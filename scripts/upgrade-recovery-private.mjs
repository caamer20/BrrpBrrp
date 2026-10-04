#!/usr/bin/env node
// Private-state upgrade and recovery rehearsal (review 8, R8-05; WP-09–12, WP-23).
//
// Runs the operator path on disposable data with the private-conversation gate
// explicitly opened on temporary installations, and fails loudly when the restored
// copy does not keep its promises:
//
//   1. a fresh install seeds consented private state through the real API: an
//      accepted conversation carrying attachment bytes, a send receipt and read
//      cursors; a pending request; a rejected request inside its cooldown; a blocked
//      pair; a hidden-conversation preference; a contact opt-out; and an accepted
//      peer whose account is removed, whose history must survive as a Deleted member
//   2. the installation is stopped and backed up, and the archive is verified
//   3. decisions taken only AFTER the snapshot (a block, a deactivation) are applied
//      to the source and proven live there, so the rehearsal can prove a restore
//      cannot carry them back in time
//   4. the archive is restored into an empty destination and verified; the restored
//      database must contain no sessions or push registrations
//   5. the restored copy is served in isolation and asserted through freshly
//      authenticated API sessions: participants read history and the exact private
//      attachment bytes, outsiders (including the owner) cannot, cursors and private
//      search/hide/contact preferences survive, the pre-snapshot block still refuses
//      writes, the post-snapshot block is absent until reconciliation applies it
//      again, and a lost-acknowledgement replay returns its receipt without
//      recreating the message
//
// This is a local one-machine rehearsal, not off-host recovery acceptance.
//
// Usage:
//   node scripts/upgrade-recovery-private.mjs [--binary PATH] [--work-dir DIR] [--report FILE] [--keep]
//
// Exit code 0 means every check passed. The work directories are removed unless
// --keep is given, and on failure they are always kept for inspection.

import { createECDH, createHash, createHmac, randomBytes } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
  cpSync,
} from "node:fs";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { DatabaseSync } from "node:sqlite";
import {
  api,
  buildBinary,
  createRehearsal,
  freePort,
  installTeardown,
  killLiveChildren,
  multipart,
  run,
  startServer,
  stopServer,
} from "./rehearsal-kit.mjs";

installTeardown((code) => process.exit(code));
const { checks, transcript, say, check } = createRehearsal();

const options = parseArgs(process.argv.slice(2));
let workDirForCleanup = null;

function parseArgs(argv) {
  const parsed = { keep: false, binary: null, workDir: null, report: null };
  for (let index = 0; index < argv.length; index += 1) {
    const flag = argv[index];
    if (flag === "--keep") parsed.keep = true;
    else if (flag === "--binary") parsed.binary = argv[++index];
    else if (flag === "--work-dir") parsed.workDir = argv[++index];
    else if (flag === "--report") parsed.report = argv[++index];
    else throw new Error(`unknown flag ${flag}`);
  }
  return parsed;
}

// The gate is opened on these temporary installations, and the sign-in throttle is raised
// because every credential check this rehearsal makes is a real sign-in from one loopback
// address: the production limit of twenty attempts per five minutes is for real clients,
// not for a disposable fixture whose purpose is repeated authentication.
const PRIVATE_ENV = {
  BRRP_ENABLE_PRIVATE_CONVERSATIONS: "true",
  BRRP_LOGIN_ATTEMPTS: "2000",
};

const OWNER_PASSWORD = "correct horse battery staple";
const rotatedEchoPassword = "a rotated echo passphrase";
const members = [
  "alpha",
  "beta",
  "carol",
  "delta",
  "echo",
  "frank",
  "grace",
  "ivy",
];
const memberPassword = (name) => `${name} practice passphrase`;

const BASE32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

function decodeBase32(secret) {
  let bits = 0;
  let value = 0;
  const bytes = [];
  for (const character of secret.replace(/=+$/, "").toUpperCase()) {
    const index = BASE32.indexOf(character);
    if (index === -1) throw new Error(`setup key contains ${character}`);
    value = (value << 5) | (index >>> 0);
    bits += 5;
    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(bytes);
}

// totpCode is the RFC 6238 code an authenticator would show for the setup key the
// server generated: SHA-1, six digits, thirty-second steps.
function totpCode(secret, at = Date.now()) {
  const message = Buffer.alloc(8);
  message.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)));
  const digest = createHmac("sha1", decodeBase32(secret))
    .update(message)
    .digest();
  const offset = digest[digest.length - 1] & 15;
  const number = digest.readUInt32BE(offset) & 0x7fffffff;
  return String(number % 1000000).padStart(6, "0");
}

async function signIn(base, username, password, secret) {
  const attempt = await api(base, "POST", "/api/login", {
    body: { username, password },
  });
  if (attempt.status === 200 && attempt.body.second_factor_required) {
    if (!secret)
      throw new Error(
        `sign-in as ${username}: a second factor is required but unavailable`,
      );
    await waitForNextCodeWindow();
    const withFactor = await api(base, "POST", "/api/login", {
      body: { username, password, code: totpCode(secret) },
    });
    if (withFactor.status !== 200 || !withFactor.cookie)
      throw new Error(
        `factor sign-in as ${username}: HTTP ${withFactor.status}`,
      );
    return withFactor.cookie;
  }
  if (attempt.status !== 200 || !attempt.cookie)
    throw new Error(`sign-in as ${username}: HTTP ${attempt.status}`);
  return attempt.cookie;
}

// waitForNextCodeWindow: a verification code is single-use inside its time step,
// so the next ceremony that needs one waits for the step to roll over.
async function waitForNextCodeWindow() {
  const window0 = Math.floor(Date.now() / 30000);
  while (Math.floor(Date.now() / 30000) === window0)
    await new Promise((resolve) => setTimeout(resolve, 250));
}

function countContent(messages, content) {
  return (messages ?? []).filter((message) =>
    (message.content ?? "").includes(content),
  ).length;
}

// deviceKeys produces the key material the push subscription endpoint validates:
// an uncompressed P-256 public key (65 bytes) that the server can unmarshal back
// to a curve point, and the 16-byte auth secret. A fixed key pair is enough — it
// is a disposable fixture row, never used to encrypt anything — and fixing it
// makes a failure reproducible.
function deviceKeys() {
  const uncompressed = createECDH("prime256v1").generateKeys();
  return {
    p256dh: base64url(uncompressed),
    auth: base64url(randomBytes(16)),
  };
}

const base64url = (bytes) => Buffer.from(bytes).toString("base64url");

async function main() {
  const workDir = options.workDir
    ? path.resolve(options.workDir)
    : mkdtempSync(path.join(tmpdir(), "brrp-private-rehearsal-"));
  workDirForCleanup = workDir;
  if (options.workDir) mkdirSync(workDir, { recursive: true });
  const sourceDir = path.join(workDir, "source");
  const restoredDir = path.join(workDir, "restored");
  mkdirSync(sourceDir, { recursive: true });
  mkdirSync(restoredDir, { recursive: true });

  let binary = options.binary && path.resolve(options.binary);
  if (!binary || !existsSync(binary)) {
    if (options.binary) {
      binary = path.resolve(options.binary);
      buildBinary(binary);
    } else {
      binary = path.join(workDir, "brrpbrrp-rehearsal");
      buildBinary(binary);
    }
  }
  const binaryDigest = createHash("sha256")
    .update(readFileSync(binary))
    .digest("hex");
  say(`Rehearsal workspace: ${workDir}`);
  say(`Binary: ${binary} (sha256 ${binaryDigest})`);
  say(
    `Feature mode: BRRP_ENABLE_PRIVATE_CONVERSATIONS=true on both disposable servers`,
  );

  const ids = {};
  const cookies = {};
  let pairAB;
  let pairCD;
  let pairDB;
  let pairGD;
  let requestGDEvidenceID;
  let pairFB;
  const attachmentText = `private attachment payload ${Date.now().toString(16)}\n`;
  const attachmentDigest = createHash("sha256")
    .update(attachmentText)
    .digest("hex");
  let attachmentID;
  let firstMessageID;
  let secondMessageID;

  say("");
  say("1. Install and seed consented private state");
  // The kit's startServer waits for readiness itself, which is why these awaited twice
  // before: a promise that resolves to a running server is the only shape that cannot be
  // used before it is listening.
  const source = await startServer(
    binary,
    sourceDir,
    await freePort(),
    null,
    PRIVATE_ENV,
  );
  const token = readFileSync(
    path.join(sourceDir, "setup-token"),
    "utf8",
  ).trim();
  const setup = await api(source.base, "POST", "/api/setup", {
    body: {
      token,
      server_name: "Private recovery rehearsal",
      username: "owner",
      password: OWNER_PASSWORD,
    },
  });
  check(
    "first-time setup accepted",
    setup.status === 201,
    `HTTP ${setup.status}`,
  );
  cookies.owner = await signIn(source.base, "owner", OWNER_PASSWORD);

  for (const name of members) {
    const created = await api(source.base, "POST", "/api/admin/users", {
      cookie: cookies.owner,
      body: {
        username: name,
        password: memberPassword(name),
        display_name: name,
      },
    });
    check(
      `member ${name} created`,
      created.status === 201 && created.body.id,
      `HTTP ${created.status}`,
    );
    ids[name] = created.body.id;
    cookies[name] = await signIn(source.base, name, memberPassword(name));
  }

  const requestAB = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.alpha,
    body: { user_id: ids.beta, introduction: "alpha asks beta" },
  });
  check(
    "alpha's request to beta created",
    requestAB.status === 201,
    `HTTP ${requestAB.status}`,
  );
  pairAB = requestAB.body.id;
  const acceptAB = await api(
    source.base,
    "POST",
    `/api/directs/${pairAB}/accept`,
    { cookie: cookies.beta, body: {} },
  );
  check(
    "beta accepted the request",
    acceptAB.status === 200,
    `HTTP ${acceptAB.status}`,
  );

  const file = multipart("secret-note.txt", attachmentText);
  const upload = await api(
    source.base,
    "POST",
    `/api/uploads?conversation_id=${pairAB}`,
    {
      cookie: cookies.alpha,
      raw: file.body,
      contentType: file.contentType,
    },
  );
  check(
    "destination-bound private upload accepted",
    upload.status === 201 && Boolean(upload.body.id),
    `HTTP ${upload.status}`,
  );
  attachmentID = upload.body.id;
  const first = await api(
    source.base,
    "POST",
    `/api/conversations/${pairAB}/messages`,
    {
      cookie: cookies.alpha,
      body: {
        content: "private seed wording quokka",
        idempotency_key: "kab-1-receipt",
        attachment_ids: [attachmentID],
      },
    },
  );
  check(
    "private message with attachment accepted",
    first.status === 201,
    `HTTP ${first.status}`,
  );
  firstMessageID = first.body.id;
  const second = await api(
    source.base,
    "POST",
    `/api/conversations/${pairAB}/messages`,
    {
      cookie: cookies.alpha,
      body: {
        content: "a second private marker",
        idempotency_key: "kab-2-later",
      },
    },
  );
  check(
    "a second message leaves beta one unread",
    second.status === 201,
    `HTTP ${second.status}`,
  );
  secondMessageID = second.body.id;
  const replayedBefore = await api(
    source.base,
    "POST",
    `/api/conversations/${pairAB}/messages`,
    {
      cookie: cookies.alpha,
      body: {
        content: "private seed wording quokka",
        idempotency_key: "kab-1-receipt",
        attachment_ids: [attachmentID],
      },
    },
  );
  check(
    "pre-snapshot lost-ack replay returns the recorded message",
    replayedBefore.status === 200 && replayedBefore.body.id === firstMessageID,
    `HTTP ${replayedBefore.status}`,
  );
  const cursor = await api(
    source.base,
    "POST",
    `/api/conversations/${pairAB}/read`,
    {
      cookie: cookies.beta,
      body: { last_read_message_id: firstMessageID },
    },
  );
  check(
    "beta's read cursor recorded",
    cursor.status === 200,
    `HTTP ${cursor.status}`,
  );

  // Groups, threads, activity and exports. All four are recoverable state the product
  // promises a member, and a rehearsal that restores direct messages while losing a
  // group or a thread proves less than the restore actually delivers. Each is created
  // through its own supported route — no direct writes — so the rehearsal also
  // demonstrates that the state is reachable the way a member would reach it.
  //
  // The group needs the private feature, which the harness already enables, and a
  // second member who accepts an invitation. Both happen before the snapshot.
  const groupCreate = await api(source.base, "POST", "/api/groups", {
    cookie: cookies.alpha,
    body: { name: "rehearsal group" },
  });
  check(
    "a group created through its own route",
    groupCreate.status === 201 && Boolean(groupCreate.body.id),
    `HTTP ${groupCreate.status}`,
  );
  const groupID = groupCreate.body.id;
  const groupInvite = await api(
    source.base,
    "POST",
    `/api/groups/${groupID}/invite`,
    {
      cookie: cookies.alpha,
      body: { user_id: ids.beta },
    },
  );
  check(
    "the second member invited to the group",
    groupInvite.status === 201,
    `HTTP ${groupInvite.status}`,
  );
  const invitations = await api(source.base, "GET", "/api/group-invitations", {
    cookie: cookies.beta,
  });
  const pendingInvite = (invitations.body?.invitations ?? []).find(
    (row) => row.group_id === groupID && row.state === "pending",
  );
  check(
    "the invitation is listed for its recipient",
    Boolean(pendingInvite),
    `${(invitations.body?.invitations ?? []).length} pending`,
  );
  const groupAccept = await api(
    source.base,
    "POST",
    `/api/group-invitations/${pendingInvite.id}/accept`,
    {
      cookie: cookies.beta,
      body: {},
    },
  );
  check(
    "the invitation accepted through the supported route",
    groupAccept.status === 200,
    `HTTP ${groupAccept.status}`,
  );
  const groupMessage = await api(
    source.base,
    "POST",
    `/api/conversations/${groupID}/messages`,
    {
      cookie: cookies.alpha,
      body: { content: "group marker narwhal", idempotency_key: "kab-group-1" },
    },
  );
  check(
    "a group message accepted",
    groupMessage.status === 201,
    `HTTP ${groupMessage.status}`,
  );
  const groupMessageID = groupMessage.body.id;

  // A named thread inside that group: the root, the thread, and a reply. Threads are
  // community state with their own rows, and restoring the conversation without the
  // thread would leave the reply reachable only by id.
  // Threads are a *community* feature and live on channels, so the group's own
  // conversation id is not a valid thread parent. The rehearsal therefore starts the
  // thread on the community channel instead, which is where a member would start one.
  const channelList = await api(source.base, "GET", "/api/channels", {
    cookie: cookies.alpha,
  });
  const textChannel = (channelList.body ?? []).find(
    (row) => row.type === "text",
  );
  check(
    "a readable community channel is available for a thread",
    Boolean(textChannel),
    `${(channelList.body ?? []).length} channel(s)`,
  );
  const channelRoot = await api(
    source.base,
    "POST",
    `/api/channels/${textChannel.id}/messages`,
    {
      cookie: cookies.alpha,
      body: {
        content: "thread root pangolin",
        idempotency_key: "kab-thread-root",
      },
    },
  );
  check(
    "a channel message to root the thread",
    channelRoot.status === 201,
    `HTTP ${channelRoot.status}`,
  );
  const threadStart = await api(
    source.base,
    "POST",
    `/api/channels/${textChannel.id}/threads`,
    {
      cookie: cookies.alpha,
      body: { root_message_id: channelRoot.body.id, title: "rehearsal thread" },
    },
  );
  check(
    "a named thread started through the channel route",
    threadStart.status === 201 && Boolean(threadStart.body.id),
    `HTTP ${threadStart.status}`,
  );
  const threadID = threadStart.body.id;
  const threadChannelID = textChannel.id;
  const threadReply = await api(
    source.base,
    "POST",
    `/api/threads/${threadID}/messages`,
    {
      cookie: cookies.beta,
      body: { content: "thread reply okapi", idempotency_key: "kab-thread-1" },
    },
  );
  check(
    "a reply accepted into the thread",
    threadReply.status === 201,
    `HTTP ${threadReply.status}`,
  );
  const threadFollow = await api(
    source.base,
    "POST",
    `/api/threads/${threadID}/follow`,
    {
      cookie: cookies.beta,
      body: {},
    },
  );
  check(
    "a member follows the thread",
    threadFollow.status === 200 ||
      threadFollow.status === 201 ||
      threadFollow.status === 204,
    `HTTP ${threadFollow.status}`,
  );
  // The reader's own position in the thread is durable state with its own table, and a
  // restore that dropped it would re-offer replies a member already read. Alpha never
  // opens the thread, so the pair of assertions below can tell "the cursor survived"
  // from "everyone is at zero".
  const threadRead = await api(
    source.base,
    "POST",
    `/api/threads/${threadID}/read`,
    {
      cookie: cookies.beta,
      body: { last_read_message_id: threadReply.body.id },
    },
  );
  check(
    "a member's thread read position recorded before the snapshot",
    threadRead.status === 200,
    `HTTP ${threadRead.status}`,
  );
  // A second invitation stays pending across the snapshot: an invitation is durable
  // state that is neither membership nor history, and a restore that resolved or
  // dropped it would change who is in the group.
  const groupInviteCarol = await api(
    source.base,
    "POST",
    `/api/groups/${groupID}/invite`,
    {
      cookie: cookies.alpha,
      body: { user_id: ids.carol },
    },
  );
  check(
    "a second, still-pending invitation exists before the snapshot",
    groupInviteCarol.status === 201,
    `HTTP ${groupInviteCarol.status}`,
  );

  // Activity: the group message and the thread reply are things that happened *to* beta,
  // so beta's own feed has rows and the acknowledgment cursor is durable state.
  const activityBefore = await api(source.base, "GET", "/api/me/activity", {
    cookie: cookies.beta,
  });
  check(
    "the recipient has an activity feed before the snapshot",
    activityBefore.status === 200 &&
      (activityBefore.body?.activity ?? []).length > 0,
    `HTTP ${activityBefore.status}, ${(activityBefore.body?.activity ?? []).length} row(s)`,
  );
  // Acknowledgment and interruption preferences are account-owned durable state. A
  // restore that carried the rows but not the acknowledgment would resurrect count
  // badges, and one that dropped a mute would interrupt people who asked it not to.
  const activityNewest = (activityBefore.body?.activity ?? [])[0]?.id;
  const unacknowledgedBeforeAck =
    activityBefore.body?.unacknowledged_count ?? 0;
  const activityAck = await api(
    source.base,
    "POST",
    "/api/me/activity/acknowledge",
    {
      cookie: cookies.beta,
      body: { before: activityNewest },
    },
  );
  check(
    "the recipient acknowledged activity before the snapshot",
    activityAck.status === 200 &&
      activityAck.body?.unacknowledged_count < unacknowledgedBeforeAck,
    `HTTP ${activityAck.status}, ${unacknowledgedBeforeAck} -> ${activityAck.body?.unacknowledged_count}`,
  );
  const interruptionPreference = await api(
    source.base,
    "PUT",
    "/api/me/notification-preferences",
    {
      cookie: cookies.beta,
      body: { kind: "group_message", muted: true },
    },
  );
  check(
    "a notification preference set before the snapshot",
    interruptionPreference.status === 200,
    `HTTP ${interruptionPreference.status}`,
  );

  // A member data export. The file is on disk rather than in a row, so a rehearsal that
  // restores the row without the file proves nothing about the copy a member was told
  // was waiting. The build runs after the request commits, so the fixture waits for
  // the row to reach `ready` and captures its identity and metadata — the assertions
  // after the restore compare against this, not against a row that merely exists
  // (review 17, V17-05).
  const exportRequest = await api(source.base, "POST", "/api/me/export", {
    cookie: cookies.beta,
    body: { operation_key: "kab-export-1" },
  });
  check(
    "a member export requested before the snapshot",
    exportRequest.status === 200 ||
      exportRequest.status === 201 ||
      exportRequest.status === 202,
    `HTTP ${exportRequest.status}`,
  );
  const exportID = exportRequest.body?.id ?? "";
  let exportRequested = null;
  for (let attempt = 0; attempt < 100 && !exportRequested; attempt++) {
    const listing = await api(source.base, "GET", "/api/me/export", {
      cookie: cookies.beta,
    });
    const record = (Array.isArray(listing.body) ? listing.body : []).find(
      (row) => row.id === exportID,
    );
    if (record?.state === "ready") exportRequested = record;
    else await new Promise((done) => setTimeout(done, 100));
  }
  check(
    "the requested export is ready before the snapshot",
    exportRequested !== null,
    exportRequested
      ? `${exportRequested.message_count} messages, ${exportRequested.conversation_count} conversations`
      : `record ${exportID} never reached ready`,
  );

  const requestCD = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.carol,
    body: { user_id: ids.delta, introduction: "carol asks delta" },
  });
  check(
    "carol's pending request created",
    requestCD.status === 201,
    `HTTP ${requestCD.status}`,
  );
  pairCD = requestCD.body.id;

  // grace asks delta, and delta then reports and blocks that introduction. This is
  // the request-evidence operation rather than the message one, and it has to exist
  // before the snapshot so the restore can prove the evidence, the block and the
  // command receipt all survive together.
  const requestGD = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.grace,
    body: {
      user_id: ids.delta,
      introduction: "grace asks delta with an introduction worth reporting",
    },
  });
  check(
    "grace's pending request created",
    requestGD.status === 201,
    `HTTP ${requestGD.status}`,
  );
  pairGD = requestGD.body.id;
  const requestGDPreview = await api(
    source.base,
    "GET",
    `/api/directs/${pairGD}/report-preview`,
    { cookie: cookies.delta },
  );
  check(
    "delta can preview the pending introduction for reporting",
    requestGDPreview.status === 200,
    `HTTP ${requestGDPreview.status}`,
  );
  const requestGDReport = await api(
    source.base,
    "POST",
    `/api/directs/${pairGD}/report-and-block`,
    {
      cookie: cookies.delta,
      body: {
        reason: "rehearsal introduction concern, not a real one",
        operation_key: "p10-rehearsal-report-and-block",
        version: requestGDPreview.body?.version,
      },
    },
  );
  check(
    "delta reported the introduction and blocked the sender",
    requestGDReport.status === 201,
    `HTTP ${requestGDReport.status} ${JSON.stringify(requestGDReport.body ?? {})}`,
  );
  requestGDEvidenceID = requestGDReport.body?.id;
  // The block is the half that matters for a restore: grace must be unable to write
  // on the source, not merely absent from a list.
  // Reporting a pending introduction removes the request, so there is no
  // conversation left to write into. That is the contract, and the block is what
  // stops a new one being created: neither a 404 nor a new request would be
  // acceptable if the block had not been committed with the report.
  const graceWrite = await api(
    source.base,
    "POST",
    `/api/conversations/${pairGD}/messages`,
    {
      cookie: cookies.grace,
      body: {
        content: "after the block",
        idempotency_key: "p10-rehearsal-grace-write-1",
      },
    },
  );
  check(
    "a reported request leaves the sender no conversation to write into",
    graceWrite.status === 404,
    `HTTP ${graceWrite.status}`,
  );
  const graceRetryRequest = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.grace,
    body: {
      user_id: ids.delta,
      introduction: "grace tries again after the block",
    },
  });
  check(
    "the block stops a new request from the reported sender",
    graceRetryRequest.status >= 400,
    `HTTP ${graceRetryRequest.status} ${JSON.stringify(graceRetryRequest.body ?? {})}`,
  );
  // And the report must never have accepted the request: reporting is not contact.
  const graceAfterReport = await api(source.base, "GET", "/api/directs", {
    cookie: cookies.grace,
  });
  const gracePair = (graceAfterReport.body ?? []).find(
    (direct) => direct.id === pairGD,
  );
  check(
    "report-and-block never accepted contact",
    gracePair?.request_state !== "accepted" && gracePair?.blocked === true,
    `state=${gracePair?.request_state} blocked=${gracePair?.blocked}`,
  );
  // Declared here and assigned where the fixture has completed the ownership
  // ceremony and enrolled the owner's factor — the grant is authored through the real
  // role routes there, because writing the rows directly proved nothing about the
  // product and recorded success unconditionally (review 17, V17-05).
  let seededReviewerRoleID = "";
  let seededReviewerAccountID = "";

  const sourceRequestEvidence = (() => {
    const db = new DatabaseSync(path.join(sourceDir, "brrp.db"), {
      readOnly: true,
    });
    const row = db
      .prepare(
        "SELECT count(*) AS count, snapshot_text, reason, status, expires_at FROM private_request_reports WHERE id=?",
      )
      .get(requestGDEvidenceID);
    db.close();
    return row;
  })();
  check(
    "the snapshot source really holds the request evidence",
    sourceRequestEvidence.count === 1,
    `${sourceRequestEvidence.count} rows`,
  );
  check(
    "the snapshot source holds the exact introduction delta confirmed",
    sourceRequestEvidence.snapshot_text === requestGDPreview.body?.introduction,
    `${String(sourceRequestEvidence.snapshot_text).length} characters, preview has ${String(requestGDPreview.body?.introduction).length}`,
  );

  const requestEC = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.echo,
    body: { user_id: ids.carol, introduction: "echo asks carol" },
  });
  check(
    "echo's request created",
    requestEC.status === 201,
    `HTTP ${requestEC.status}`,
  );
  const rejectEC = await api(
    source.base,
    "POST",
    `/api/directs/${requestEC.body.id}/reject`,
    { cookie: cookies.carol, body: {} },
  );
  check(
    "carol rejected echo",
    rejectEC.status === 200,
    `HTTP ${rejectEC.status}`,
  );

  const requestDB = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.delta,
    body: { user_id: ids.beta, introduction: "delta asks beta" },
  });
  check(
    "delta's request to beta created",
    requestDB.status === 201,
    `HTTP ${requestDB.status}`,
  );
  pairDB = requestDB.body.id;
  const acceptDB = await api(
    source.base,
    "POST",
    `/api/directs/${pairDB}/accept`,
    { cookie: cookies.beta, body: {} },
  );
  check(
    "beta accepted delta",
    acceptDB.status === 200,
    `HTTP ${acceptDB.status}`,
  );
  const blocked = await api(source.base, "PUT", `/api/me/blocks/${ids.beta}`, {
    cookie: cookies.delta,
    body: {},
  });
  check(
    "delta blocked beta before the snapshot",
    blocked.status === 200,
    `HTTP ${blocked.status}`,
  );

  const hide = await api(
    source.base,
    "PUT",
    `/api/directs/${pairAB}/visibility`,
    { cookie: cookies.alpha, body: { hidden: true } },
  );
  check(
    "alpha hid the conversation",
    hide.status === 200,
    `HTTP ${hide.status}`,
  );
  const preference = await api(
    source.base,
    "PUT",
    "/api/me/contact-preferences",
    { cookie: cookies.echo, body: { allow_requests: false } },
  );
  check(
    "echo opted out of new requests",
    preference.status === 200,
    `HTTP ${preference.status}`,
  );

  const requestFB = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.frank,
    body: { user_id: ids.beta, introduction: "frank asks beta" },
  });
  check(
    "frank's request to beta created",
    requestFB.status === 201,
    `HTTP ${requestFB.status}`,
  );
  pairFB = requestFB.body.id;
  const acceptFB = await api(
    source.base,
    "POST",
    `/api/directs/${pairFB}/accept`,
    { cookie: cookies.beta, body: {} },
  );
  check(
    "beta accepted frank",
    acceptFB.status === 200,
    `HTTP ${acceptFB.status}`,
  );
  const frankWords = await api(
    source.base,
    "POST",
    `/api/conversations/${pairFB}/messages`,
    {
      cookie: cookies.frank,
      body: {
        content: "frank words that outlive the account",
        idempotency_key: "kfb-1-retained",
      },
    },
  );
  check(
    "frank's message accepted",
    frankWords.status === 201,
    `HTTP ${frankWords.status}`,
  );
  // Privileged removal demands an enrolled factor and a recent strong sign-in.
  let ownerSecret = "";
  {
    const setup = await api(source.base, "POST", "/api/me/mfa/setup", {
      cookie: cookies.owner,
      body: { password: OWNER_PASSWORD },
    });
    check(
      "owner factor setup issued",
      setup.status === 200 && Boolean(setup.body.secret),
      `HTTP ${setup.status}`,
    );
    ownerSecret = setup.body.secret;
    const enable = await api(source.base, "POST", "/api/me/mfa/enable", {
      cookie: cookies.owner,
      body: { code: totpCode(ownerSecret) },
    });
    check(
      "owner factor enrolled",
      enable.status === 200,
      `HTTP ${enable.status}`,
    );
    await waitForNextCodeWindow();
    const reauth = await api(source.base, "POST", "/api/me/reauth", {
      cookie: cookies.owner,
      body: { password: OWNER_PASSWORD, code: totpCode(ownerSecret) },
    });
    check(
      "owner reauthenticated for the privileged removal",
      reauth.status === 200,
      `HTTP ${reauth.status}`,
    );
  }
  // A reviewer grant is an ordinary role row, so a backup captures it. Granting it
  // before the snapshot and revoking it after is the same shape as the block and the
  // deactivation: the restored copy will read the grant as live, and an operator who
  // does not reconcile it puts a former reviewer back on the private evidence queue.
  //
  // Everything below goes through the supported surfaces. The disposable fixture
  // completes the ownership ceremony on the account whose factor was just enrolled,
  // then creates the role and makes the assignment through the role routes — the
  // earlier version wrote both rows with SQL and recorded success unconditionally,
  // which proved nothing about the product and would have passed with an empty seed
  // (review 17, V17-05).
  {
    // The pre-ceremony administrator bridge deliberately does not include the
    // private-review capability: reading submitted private evidence is not community
    // administration, and an administrator who has not been given the reviewer role
    // must not be able to read it. This is checked before the ceremony below, because
    // the ceremony changes the answer — the owner tier holds every capability, by
    // design — and the two facts together are what the policy actually is.
    const ownerQueueBeforeCeremony = await api(
      source.base,
      "GET",
      "/api/admin/private-reports",
      {
        cookie: cookies.owner,
      },
    );
    check(
      "the pre-ceremony administrator cannot read the private evidence queue without the role",
      ownerQueueBeforeCeremony.status === 403,
      `HTTP ${ownerQueueBeforeCeremony.status}`,
    );
    const ceremony = await api(source.base, "POST", "/api/owner", {
      cookie: cookies.owner,
      body: { username: "owner", confirm: "owner" },
    });
    check(
      "the fixture completes the ownership ceremony",
      ceremony.status === 200,
      `HTTP ${ceremony.status} ${JSON.stringify(ceremony.body ?? {})}`,
    );
    const createdRole = await api(source.base, "POST", "/api/roles", {
      cookie: cookies.owner,
      body: {
        name: "p10 rehearsal reviewers",
        position: 20,
        permissions: ["report.review_private", "audit.read"],
      },
    });
    check(
      "a reviewer role authored through the role route",
      createdRole.status === 201 && Boolean(createdRole.body.id),
      `HTTP ${createdRole.status} ${JSON.stringify(createdRole.body ?? {})}`,
    );
    seededReviewerRoleID = createdRole.body.id;
    seededReviewerAccountID = ids.echo;
    // The reviewer role is *added* to what echo already holds. Assignments replace
    // the set, and an account whose only role is the reviewer role loses every member
    // capability it had — including message.send, which this fixture's later checks
    // exercise. The earlier SQL seed inserted an assignment row beside echo's existing
    // one and so never exposed this; the real route did, immediately.
    const summaryBefore = await api(
      source.base,
      "GET",
      "/api/admin/accounts/summary",
      { cookie: cookies.owner },
    );
    const echoRow = (summaryBefore.body ?? []).find(
      (row) => row.username === "echo",
    );
    check(
      "the fixture knows the reviewer's existing roles",
      Array.isArray(echoRow?.role_ids) && echoRow.role_ids.length >= 1,
      `${(echoRow?.role_ids ?? []).length} role(s)`,
    );
    const granted = await api(
      source.base,
      "PUT",
      `/api/admin/users/${seededReviewerAccountID}/roles`,
      {
        cookie: cookies.owner,
        body: {
          role_ids: [...(echoRow?.role_ids ?? []), seededReviewerRoleID],
        },
      },
    );
    check(
      "the reviewer grant made through the assignment route",
      granted.status === 200,
      `HTTP ${granted.status} ${JSON.stringify(granted.body ?? {})}`,
    );
    // The grant is checked against what the granted account resolves, not against the
    // write's status: the capability is the thing the restore has to carry, and the
    // member capability must still be there beside it.
    const echoState = await api(source.base, "GET", "/api/state", {
      cookie: cookies.echo,
    });
    const echoCapabilities = echoState.body?.authorization?.capabilities ?? [];
    check(
      "the granted account resolves report.review_private before the snapshot",
      echoCapabilities.includes("report.review_private"),
      `capabilities=${echoCapabilities.length}`,
    );
    check(
      "the grant did not take the account's member capabilities away",
      echoCapabilities.includes("message.send"),
      `capabilities=${echoCapabilities.join(",")}`,
    );
    const echoReviewer = await api(
      source.base,
      "GET",
      "/api/admin/private-reports",
      { cookie: cookies.echo },
    );
    check(
      "the granted account can read the private evidence queue before the snapshot",
      echoReviewer.status === 200,
      `HTTP ${echoReviewer.status}`,
    );
  }
  // Account removal (not erasure) retains accepted history under a Deleted member
  // identity; the erasure path is a separate ceremony and is not what restores.
  const removed = await api(
    source.base,
    "POST",
    `/api/admin/users/${ids.frank}/anonymize`,
    { cookie: cookies.owner, body: { confirm: "frank" } },
  );
  check(
    "owner removed frank before the snapshot, history retained",
    removed.status === 200,
    `HTTP ${removed.status}`,
  );
  const retainedSource = await api(
    source.base,
    "GET",
    `/api/conversations/${pairFB}/messages`,
    { cookie: cookies.beta },
  );
  check(
    "beta reads the removed peer's history on the source",
    retainedSource.status === 200 &&
      countContent(retainedSource.body, "frank words") === 1,
    `HTTP ${retainedSource.status}`,
  );

  // A real device registration is seeded before the snapshot. The old rehearsal
  // asserted that the restored copy held zero push registrations without ever
  // proving the source held one, so the assertion could not tell a correct
  // restore from a database that never had one (review 9, R9-05).
  //
  // It is registered through the real API, which validates the endpoint host and
  // both keys, so the row is one a browser could actually have created. Nothing
  // is ever delivered: a stored registration is only read when a mention fires
  // against it, and this fixture never sends one.
  //
  // The endpoint is example.com, reserved for documentation by RFC 2606 and never
  // a push service. It has to be a resolvable public HTTPS host because the server
  // refuses an endpoint whose host does not resolve to a public address; a
  // deliberately unroutable host would be rejected by validation, and the
  // rehearsal would then prove nothing about a registration that can exist.
  const pushEndpoint = "https://example.com/brrpbrrp-rehearsal-device";
  const subscribed = await api(source.base, "POST", "/api/push/subscribe", {
    cookie: cookies.beta,
    body: { endpoint: pushEndpoint, keys: deviceKeys() },
  });
  check(
    "beta registered a device before the snapshot",
    subscribed.status === 201,
    `HTTP ${subscribed.status}`,
  );
  {
    const db = new DatabaseSync(path.join(sourceDir, "brrp.db"), {
      readOnly: true,
    });
    const registered = db
      .prepare(
        "SELECT count(*) AS count FROM push_subscriptions WHERE user_id=?",
      )
      .get(ids.beta).count;
    db.close();
    check(
      "the snapshot source really holds a push registration",
      registered === 1,
      `${registered} rows`,
    );
  }

  // A private evidence report is submitted through the real two-step flow, so the
  // row in the snapshot is one a member could actually have created: a
  // server-confirmed preview, then a confirmation carrying that version.
  const reportedMessage = (
    await api(source.base, "GET", `/api/conversations/${pairAB}/messages`, {
      cookie: cookies.alpha,
    })
  ).body?.at(-1);
  const preview = await api(
    source.base,
    "GET",
    `/api/messages/${reportedMessage.id}/private-report-preview`,
    { cookie: cookies.alpha },
  );
  check(
    "alpha can preview a private message for reporting",
    preview.status === 200,
    `HTTP ${preview.status}`,
  );
  const submitted = await api(
    source.base,
    "POST",
    `/api/messages/${reportedMessage.id}/private-report`,
    {
      cookie: cookies.alpha,
      body: {
        reason: "rehearsal evidence, not a real concern",
        version: preview.body.version,
      },
    },
  );
  check(
    "alpha submitted private evidence before the snapshot",
    submitted.status === 201,
    `HTTP ${submitted.status}`,
  );
  const evidenceID = submitted.body?.id;
  check(
    "the submit receipt names only the reporter's own report",
    evidenceID &&
      Object.keys(submitted.body ?? {}).every((key) =>
        ["id", "status", "created_at", "expires_at", "duplicate"].includes(key),
      ),
    `receipt keys: ${Object.keys(submitted.body ?? {}).join(",")}`,
  );
  // The exact stored values are captured here so the restore can be compared
  // against them, rather than against "some non-empty expiry" and "some text of a
  // plausible length" (completion review 10, R10-08). A rehearsal that only checks
  // shapes cannot tell a faithful restore from one that rewrote either value.
  const sourceEvidence = (() => {
    const db = new DatabaseSync(path.join(sourceDir, "brrp.db"), {
      readOnly: true,
    });
    const row = db
      .prepare(
        "SELECT count(*) AS count, snapshot_text, reason, status, expires_at FROM private_message_reports WHERE id=?",
      )
      .get(evidenceID);
    db.close();
    return row;
  })();
  check(
    "the snapshot source really holds the private evidence",
    sourceEvidence.count === 1,
    `${sourceEvidence.count} rows`,
  );
  check(
    "the snapshot source holds the exact evidence the reporter confirmed",
    sourceEvidence.snapshot_text === preview.body.snapshot_text &&
      sourceEvidence.reason === "rehearsal evidence, not a real concern",
    `${String(sourceEvidence.snapshot_text).length} characters`,
  );
  // A plain member with no reviewer grant cannot read the queue. The owner is
  // deliberately not used here any more: the fixture completed the ownership
  // ceremony, and the owner tier holds every capability by design — the
  // pre-ceremony denial is asserted where the ceremony happens.
  const outsiderQueue = await api(
    source.base,
    "GET",
    "/api/admin/private-reports",
    { cookie: cookies.carol },
  );
  check(
    "a non-participating account cannot read the private evidence queue",
    outsiderQueue.status === 403,
    `HTTP ${outsiderQueue.status}`,
  );
  // The acknowledgment state as the snapshot will carry it. Activity created after the
  // acknowledgment (later requests in this fixture) is legitimately still unseen, so
  // the restored copy must carry *this* number rather than zero — the ack's own effect
  // is asserted where it happened, against the count it reduced.
  const activityAtSnapshot = await api(source.base, "GET", "/api/me/activity", {
    cookie: cookies.beta,
  });
  const unacknowledgedAtSnapshot =
    activityAtSnapshot.body?.unacknowledged_count;
  check(
    "the snapshot's unacknowledged count is a real value",
    Number.isInteger(unacknowledgedAtSnapshot),
    `${unacknowledgedAtSnapshot}`,
  );

  await stopServer(source);
  check(
    "source server stopped cleanly",
    source.child.exitCode === 0 || source.child.signalCode === "SIGTERM",
    `exit ${source.child.exitCode ?? source.child.signalCode}`,
  );

  say("");
  say("2. Snapshot the state without the later decisions");
  const backup = run(binary, [
    "backup",
    "--data-dir",
    sourceDir,
    "--out",
    "backups/private.zip",
  ]);
  const archive = path.join(sourceDir, "backups", "private.zip");
  check(
    "backup produced an archive",
    backup.status === 0 && existsSync(archive),
    existsSync(archive) ? `${statSync(archive).size} bytes` : "missing",
  );
  const integrity = run(binary, ["verify-backup", "--file", archive]);
  check(
    "verify-backup accepts the archive",
    integrity.status === 0 && (integrity.stdout ?? "").includes("Verified"),
    (integrity.stdout ?? "").split("\n").at(-2)?.trim(),
  );
  const archiveDigest = createHash("sha256")
    .update(readFileSync(archive))
    .digest("hex");

  say("");
  say("3. Prove decisions taken after the snapshot point");
  const postSnapshot = {};
  const after = await startServer(
    binary,
    sourceDir,
    await freePort(),
    null,
    PRIVATE_ENV,
  );
  {
    const betaCookie = await signIn(after.base, "beta", memberPassword("beta"));
    const alphaCookie = await signIn(
      after.base,
      "alpha",
      memberPassword("alpha"),
    );
    const ownerCookie = await signIn(
      after.base,
      "owner",
      OWNER_PASSWORD,
      ownerSecret,
    );
    const block = await api(after.base, "PUT", `/api/me/blocks/${ids.alpha}`, {
      cookie: betaCookie,
      body: {},
    });
    check(
      "beta blocked alpha only after the snapshot",
      block.status === 200,
      `HTTP ${block.status}`,
    );
    const refused = await api(
      after.base,
      "POST",
      `/api/conversations/${pairAB}/messages`,
      {
        cookie: alphaCookie,
        body: {
          content: "write while blocked",
          idempotency_key: "kab-blocked-1",
        },
      },
    );
    check(
      "the fresh block refuses alpha's write on the source",
      refused.status === 403,
      `HTTP ${refused.status}`,
    );
    postSnapshot.blockAppliesOnSource = refused.status === 403;
    const deactivated = await api(
      after.base,
      "PATCH",
      `/api/admin/users/${ids.delta}`,
      {
        cookie: ownerCookie,
        body: { is_deactivated: true, confirm: "delta" },
      },
    );
    check(
      "owner deactivated delta only after the snapshot",
      deactivated.status === 200,
      `HTTP ${deactivated.status}`,
    );
    const deltaSignin = await api(after.base, "POST", "/api/login", {
      body: { username: "delta", password: memberPassword("delta") },
    });
    check(
      "delta cannot sign in on the source while deactivated",
      deltaSignin.status !== 200,
      `HTTP ${deltaSignin.status}`,
    );
    postSnapshot.deltaDeactivated = true;

    // The third post-snapshot decision: the reviewer grant is withdrawn, so the
    // restored copy will read it as live and the rehearsal has to prove the
    // operator reconciles it before cutover.
    // Revoked through the revoke path only. There is no SQL fallback: a fallback
    // that mutates the rows and then records success is a rehearsal that cannot fail,
    // and this decision is exactly what the restored copy will disagree with
    // (review 17, V17-05).
    const revoked = await api(
      after.base,
      "PUT",
      `/api/admin/users/${seededReviewerAccountID}/roles`,
      {
        cookie: ownerCookie,
        body: { role_ids: [] },
      },
    );
    check(
      "the owner revoked the reviewer grant only after the snapshot",
      revoked.status === 200,
      `HTTP ${revoked.status} ${JSON.stringify(revoked.body ?? {})}`,
    );
    const echoAfterRevoke = await signIn(
      after.base,
      "echo",
      memberPassword("echo"),
    );
    const echoQueueAfter = await api(
      after.base,
      "GET",
      "/api/admin/private-reports",
      { cookie: echoAfterRevoke },
    );
    check(
      "the revoked account loses the private evidence queue on the source",
      echoQueueAfter.status === 403,
      `HTTP ${echoQueueAfter.status}`,
    );
    postSnapshot.reviewerGrantRevoked = true;

    // Three more decisions taken after the snapshot, each with its own reconciliation
    // before cutover: a ban, a rotated credential and an erasure. A rehearsal that
    // proved only blocks and deactivations would leave the operator to discover these
    // on the restored host with real people in front of it.
    const banned = await api(
      after.base,
      "PATCH",
      `/api/admin/users/${ids.carol}`,
      {
        cookie: ownerCookie,
        body: { is_banned: true, confirm: "carol" },
      },
    );
    check(
      "owner banned carol only after the snapshot",
      banned.status === 200,
      `HTTP ${banned.status}`,
    );
    const carolOnSource = await api(after.base, "POST", "/api/login", {
      body: { username: "carol", password: memberPassword("carol") },
    });
    check(
      "carol cannot sign in on the source while banned",
      carolOnSource.status !== 200,
      `HTTP ${carolOnSource.status}`,
    );
    postSnapshot.carolBanned = true;

    const echoForRotation = echoAfterRevoke;
    const rotated = await api(after.base, "POST", "/api/me/password", {
      cookie: echoForRotation,
      body: {
        current_password: memberPassword("echo"),
        new_password: rotatedEchoPassword,
      },
    });
    check(
      "echo rotated their password only after the snapshot",
      rotated.status === 200,
      `HTTP ${rotated.status}`,
    );
    const oldStillWorks = await api(after.base, "POST", "/api/login", {
      body: { username: "echo", password: memberPassword("echo") },
    });
    check(
      "the rotated account's old password no longer signs in on the source",
      oldStillWorks.status !== 200,
      `HTTP ${oldStillWorks.status}`,
    );
    postSnapshot.echoPasswordRotated = true;

    const erased = await api(
      after.base,
      "DELETE",
      `/api/admin/users/${ids.ivy}`,
      {
        cookie: ownerCookie,
        body: { confirm: "ivy" },
      },
    );
    check(
      "owner erased ivy only after the snapshot",
      erased.status === 200,
      `HTTP ${erased.status} ${JSON.stringify(erased.body ?? {})}`,
    );
    const ivyOnSource = await api(after.base, "POST", "/api/login", {
      body: { username: "ivy", password: memberPassword("ivy") },
    });
    check(
      "the erased account cannot sign in on the source",
      ivyOnSource.status !== 200,
      `HTTP ${ivyOnSource.status}`,
    );
    postSnapshot.ivyErased = true;
  }
  await stopServer(after);

  say("");
  say("4. Restore the snapshot into an empty destination");
  writeFileSync(path.join(restoredDir, "restore.zip"), readFileSync(archive));
  const restore = run(binary, [
    "restore",
    "--data-dir",
    restoredDir,
    "--file",
    "restore.zip",
  ]);
  check(
    "restore accepted the archive",
    restore.status === 0,
    `exit ${restore.status}`,
  );
  const verify = run(binary, ["verify", "--data-dir", restoredDir]);
  check(
    "restored copy verifies",
    verify.status === 0,
    (verify.stdout ?? "").split("\n").pop()?.trim(),
  );
  {
    const db = new DatabaseSync(path.join(restoredDir, "brrp.db"), {
      readOnly: true,
    });
    const sessions = db
      .prepare("SELECT count(*) AS count FROM sessions")
      .get().count;
    const pushes = db
      .prepare("SELECT count(*) AS count FROM push_subscriptions")
      .get().count;
    db.close();
    check(
      "restored state carries no sessions",
      sessions === 0,
      `${sessions} rows`,
    );
    // Meaningful because the snapshot source held one (step 1): the restore
    // removed a registration that existed, not a table that was already empty.
    check(
      "restored state carries no push registrations",
      pushes === 0,
      `${pushes} rows, seeded with 1 before the snapshot`,
    );
  }

  say("");
  say(
    "5. Serve the restored copy in isolation and prove behavior through fresh sessions",
  );
  const restored = await startServer(
    binary,
    restoredDir,
    await freePort(),
    null,
    PRIVATE_ENV,
  );
  {
    const stale = await api(restored.base, "GET", "/api/state", {
      cookie: cookies.alpha,
    });
    check(
      "alpha's pre-restore session is invalid on the restored copy",
      stale.status === 401,
      `HTTP ${stale.status}`,
    );
    const staleOwner = await api(restored.base, "GET", "/api/state", {
      cookie: cookies.owner,
    });
    check(
      "the owner's pre-restore session is invalid on the restored copy",
      staleOwner.status === 401,
      `HTTP ${staleOwner.status}`,
    );

    const fresh = {};
    fresh.owner = await signIn(
      restored.base,
      "owner",
      OWNER_PASSWORD,
      ownerSecret,
    );
    for (const name of ["alpha", "beta", "carol", "delta", "echo", "grace"]) {
      fresh[name] = await signIn(restored.base, name, memberPassword(name));
    }
    const frankLogin = await api(restored.base, "POST", "/api/login", {
      body: { username: "frank", password: memberPassword("frank") },
    });
    check(
      "the removed account cannot sign in on the restored copy",
      frankLogin.status !== 200,
      `HTTP ${frankLogin.status}`,
    );

    const history = await api(
      restored.base,
      "GET",
      `/api/conversations/${pairAB}/messages`,
      { cookie: fresh.beta },
    );
    check(
      "beta reads the restored private history",
      history.status === 200 &&
        countContent(history.body, "quokka") === 1 &&
        countContent(history.body, "second private marker") === 1,
      `HTTP ${history.status}, ${(history.body ?? []).length} messages`,
    );
    const download = await api(
      restored.base,
      "GET",
      `/api/uploads/${attachmentID}`,
      { cookie: fresh.beta },
    );
    check(
      "the exact private attachment bytes survived",
      download.status === 200 &&
        createHash("sha256").update(download.bytes).digest("hex") ===
          attachmentDigest,
      `${download.bytes.length} bytes`,
    );

    // The restored copy has to carry the state that is not a private message: a group,
    // its thread, the thread's reply and follow, the activity those produced, and a
    // member export. A rehearsal that restores direct messages and loses these proves
    // less than the restore actually delivers — and each is verified through its own
    // route, so a restored row nobody can reach would fail here rather than pass as a
    // row that exists.
    const groupRestored = await api(
      restored.base,
      "GET",
      `/api/conversations/${groupID}/messages`,
      { cookie: fresh.beta },
    );
    check(
      "the accepted member reads the restored group history",
      groupRestored.status === 200 &&
        countContent(groupRestored.body, "group marker narwhal") === 1,
      `HTTP ${groupRestored.status}, ${(groupRestored.body ?? []).length} messages`,
    );
    const groupOutsider = await api(
      restored.base,
      "GET",
      `/api/conversations/${groupID}/messages`,
      { cookie: fresh.carol },
    );
    check(
      "an account that never joined the restored group cannot read it",
      groupOutsider.status === 404,
      `HTTP ${groupOutsider.status}`,
    );

    const threadRestored = await api(
      restored.base,
      "GET",
      `/api/threads/${threadID}/messages`,
      { cookie: fresh.alpha },
    );
    check(
      "the named thread and its reply survived the restore",
      threadRestored.status === 200 &&
        countContent(threadRestored.body, "thread reply okapi") === 1,
      `HTTP ${threadRestored.status}, ${(threadRestored.body ?? []).length} entries`,
    );
    const threadList = await api(
      restored.base,
      "GET",
      `/api/channels/${threadChannelID}/threads`,
      { cookie: fresh.beta },
    );
    check(
      "the thread is still listed on its channel after the restore",
      threadList.status === 200 &&
        JSON.stringify(threadList.body ?? {}).includes(threadID),
      `HTTP ${threadList.status}`,
    );

    const activityRestored = await api(
      restored.base,
      "GET",
      "/api/me/activity",
      { cookie: fresh.beta },
    );
    check(
      "the recipient's activity feed survived the restore",
      activityRestored.status === 200 &&
        (activityRestored.body?.activity ?? []).length > 0,
      `HTTP ${activityRestored.status}, ${(activityRestored.body?.activity ?? []).length} row(s)`,
    );
    check(
      "the restored activity still names the conversation it is about",
      JSON.stringify(activityRestored.body ?? {}).includes(groupID),
      "no restored row names the group",
    );
    // Acknowledgment is account-owned state: the rows came back, and so must the fact
    // that the member already saw them — otherwise the restore resurrects badges for
    // an account that had cleared them.
    check(
      "the acknowledgment survived the restore",
      activityRestored.body?.unacknowledged_count === unacknowledgedAtSnapshot,
      `unacknowledged=${activityRestored.body?.unacknowledged_count}, snapshot had ${unacknowledgedAtSnapshot}`,
    );
    const preferencesRestored = await api(
      restored.base,
      "GET",
      "/api/me/notification-preferences",
      { cookie: fresh.beta },
    );
    const groupPreference = (preferencesRestored.body?.preferences ?? []).find(
      (row) => row.kind === "group_message",
    );
    check(
      "the interruption preference survived the restore",
      groupPreference?.muted === true,
      `muted=${groupPreference?.muted}`,
    );

    // The thread's own durable state: the follower and the read position survive, so
    // the member who read the thread is not offered it as unread again, and the
    // member who never opened it still is.
    const betaThreadList = await api(
      restored.base,
      "GET",
      `/api/channels/${threadChannelID}/threads`,
      { cookie: fresh.beta },
    );
    const betaThread = (
      Array.isArray(betaThreadList.body) ? betaThreadList.body : []
    ).find((row) => row.id === threadID);
    check(
      "the follower's subscription survived the restore",
      betaThread?.following === true,
      `following=${betaThread?.following}`,
    );
    check(
      "the reader's thread position survived the restore",
      betaThread?.unread === 0,
      `unread=${betaThread?.unread}`,
    );
    // A member who neither started nor read the thread: still unread, still not
    // following. Alpha is deliberately not used — the starter keeps their own follow by
    // design, so alpha would report following=true for a reason unrelated to the read
    // position.
    const carolThreadList = await api(
      restored.base,
      "GET",
      `/api/channels/${threadChannelID}/threads`,
      { cookie: fresh.carol },
    );
    const carolThread = (
      Array.isArray(carolThreadList.body) ? carolThreadList.body : []
    ).find((row) => row.id === threadID);
    check(
      "a member who never read the thread still sees it unread",
      carolThread?.following === false && carolThread?.unread >= 1,
      `following=${carolThread?.following} unread=${carolThread?.unread}`,
    );

    // Group authority and membership, exactly: the creator is still the owner, the
    // accepted member is still a member, and the invitation that was still pending is
    // still pending rather than resolved by the restore.
    const membersRestored = await api(
      restored.base,
      "GET",
      `/api/groups/${groupID}/members`,
      { cookie: fresh.alpha },
    );
    const memberRows = membersRestored.body?.members ?? [];
    const ownerRow = memberRows.find((row) => row.id === ids.alpha);
    const acceptedRow = memberRows.find((row) => row.id === ids.beta);
    check(
      "the restored group keeps its owner and its accepted membership",
      membersRestored.status === 200 &&
        ownerRow?.is_owner === true &&
        acceptedRow?.state === "accepted" &&
        memberRows.length === 2,
      `HTTP ${membersRestored.status}, ${memberRows.length} member row(s)`,
    );
    const carolInvitations = await api(
      restored.base,
      "GET",
      "/api/group-invitations",
      { cookie: fresh.carol },
    );
    const stillPending = (carolInvitations.body?.invitations ?? []).find(
      (row) => row.group_id === groupID && row.state === "pending",
    );
    check(
      "the pending invitation is still pending after the restore",
      Boolean(stillPending),
      `${(carolInvitations.body?.invitations ?? []).length} invitation(s) for carol`,
    );

    // The export is the one piece of member state the archive does not carry: the
    // backup holds the database and the uploaded files, not the produced export.
    // That is a deliberate boundary — an export is a copy of the server's data, so
    // keeping it in every backup would mean every backup holds a second copy of
    // everything — and the contract for the resulting row is a 410 saying the file is
    // gone rather than a 404 that looks like it never existed.
    //
    // The list route answers with a raw array of records. An earlier version read
    // `body.exports`, which is always undefined, so both assertions below were
    // vacuous: an empty list satisfied "not another's" and an empty list satisfied
    // "survives" too (review 17, V17-05).
    const exportList = await api(restored.base, "GET", "/api/me/export", {
      cookie: fresh.beta,
    });
    const exportRows = Array.isArray(exportList.body) ? exportList.body : [];
    const restoredExport = exportRows.find((row) => row.id === exportID);
    check(
      "the member's export history survives the restore with its identity",
      exportList.status === 200 && Boolean(restoredExport),
      `HTTP ${exportList.status}, ${exportRows.length} record(s), wanted ${exportID}`,
    );
    check(
      "the restored export keeps its recorded metadata",
      restoredExport?.created_at === exportRequested.created_at &&
        restoredExport?.message_count === exportRequested.message_count &&
        restoredExport?.conversation_count ===
          exportRequested.conversation_count,
      restoredExport
        ? `created ${restoredExport.created_at === exportRequested.created_at ? "matches" : "differs"}, counts ${restoredExport.message_count}/${restoredExport.conversation_count}`
        : "record missing",
    );
    const exportOwner = await api(restored.base, "GET", "/api/me/export", {
      cookie: fresh.carol,
    });
    const otherExports = Array.isArray(exportOwner.body)
      ? exportOwner.body
      : [];
    check(
      "one account's exports are not another's",
      otherExports.length === 0 && exportRows.length > 0,
      `${otherExports.length} record(s) visible to an account that requested none, ${exportRows.length} to the requester`,
    );
    // The file itself is not in the archive, so the download must say exactly that:
    // 410 rather than a 404 that would read as "you never asked".
    const missingDownload = await api(
      restored.base,
      "GET",
      `/api/me/export/${exportID}`,
      { cookie: fresh.beta },
    );
    check(
      "the restored export row reports its missing file as gone, not as absent",
      missingDownload.status === 410,
      `HTTP ${missingDownload.status} ${JSON.stringify(missingDownload.body ?? {})}`,
    );

    for (const [name, cookie] of [
      ["the owner", cookies],
      ["an outsider", fresh],
    ]) {
      const actor = name === "the owner" ? fresh.owner : fresh.carol;
      const readAttempt = await api(
        restored.base,
        "GET",
        `/api/conversations/${pairAB}/messages`,
        { cookie: actor },
      );
      const fileAttempt = await api(
        restored.base,
        "GET",
        `/api/uploads/${attachmentID}`,
        { cookie: actor },
      );
      check(
        `${name} cannot read the private conversation or its file`,
        readAttempt.status === 404 && fileAttempt.status === 404,
        `HTTP ${readAttempt.status}/${fileAttempt.status}`,
      );
    }

    const scoped = await api(
      restored.base,
      "GET",
      `/api/search?q=quokka&conversation_id=${pairAB}`,
      { cookie: fresh.beta },
    );
    check(
      "search inside the conversation finds the private wording",
      scoped.status === 200 && countContent(scoped.body, "quokka") === 1,
      `HTTP ${scoped.status}`,
    );
    const global = await api(restored.base, "GET", "/api/search?q=quokka", {
      cookie: fresh.alpha,
    });
    check(
      "community search never returns private content",
      global.status === 200 && countContent(global.body, "quokka") === 0,
      `${(global.body ?? []).length} hits`,
    );
    const ownerSearch = await api(
      restored.base,
      "GET",
      "/api/search?q=quokka",
      { cookie: fresh.owner },
    );
    check(
      "the owner's search never returns private content",
      countContent(ownerSearch.body ?? [], "quokka") === 0,
      `HTTP ${ownerSearch.status}`,
    );

    const betaDirects = await api(restored.base, "GET", "/api/directs", {
      cookie: fresh.beta,
    });
    const betaAB = (betaDirects.body ?? []).find(
      (direct) => direct.id === pairAB,
    );
    check(
      "beta's read cursor survived the restore",
      betaAB?.last_read_message_id === firstMessageID,
      betaAB
        ? `cursor ${betaAB.last_read_message_id === firstMessageID ? "at" : "moved from"} the read message`
        : "conversation missing",
    );
    const alphaDirects = await api(restored.base, "GET", "/api/directs", {
      cookie: fresh.alpha,
    });
    const alphaAB = (alphaDirects.body ?? []).find(
      (direct) => direct.id === pairAB,
    );
    check(
      "alpha's hidden preference survived",
      alphaAB?.hidden === true,
      `hidden=${alphaAB?.hidden}`,
    );
    const unhide = await api(
      restored.base,
      "PUT",
      `/api/directs/${pairAB}/visibility`,
      { cookie: fresh.alpha, body: { hidden: false } },
    );
    check(
      "unhiding stays an explicit choice",
      unhide.status === 200,
      `HTTP ${unhide.status}`,
    );

    // The submitted evidence, its expiry and its disclosure boundary all survive
    // the restore. The expiry is the reporter's 90-day promise and must be the
    // snapshot's value, not a fresh one computed after the restore.
    //
    // The caller here is a plain member with no reviewer grant: the queue is opened
    // by the grant, not by being in the community. The owner is deliberately not used
    // — the fixture completed the ownership ceremony, and the owner tier holds every
    // capability by design (the pre-ceremony denial is asserted where the ceremony
    // happens).
    const restoredQueue = await api(
      restored.base,
      "GET",
      "/api/admin/private-reports",
      { cookie: fresh.carol },
    );
    check(
      "a member without the explicit grant cannot read the queue after the restore",
      restoredQueue.status === 403,
      `HTTP ${restoredQueue.status}`,
    );
    {
      const db = new DatabaseSync(path.join(restoredDir, "brrp.db"), {
        readOnly: true,
      });
      const row = db
        .prepare(
          "SELECT id,status,expires_at,snapshot_text,reason FROM private_message_reports WHERE id=?",
        )
        .get(evidenceID);
      db.close();
      check(
        "the private evidence record survived the restore",
        row?.id === evidenceID,
        row ? "present" : "missing",
      );
      // Exact equality, not a shape check: the expiry is the promise made to the
      // reporter and the text is the disclosure they agreed to, so a restore that
      // shifted either would be a different record wearing the same id.
      check(
        "the restored evidence keeps its exact expiry, text and reason",
        row?.expires_at === sourceEvidence.expires_at &&
          row?.snapshot_text === sourceEvidence.snapshot_text &&
          row?.reason === sourceEvidence.reason &&
          row?.status === sourceEvidence.status,
        `expires ${row?.expires_at === sourceEvidence.expires_at ? "matches" : "differs"}, ` +
          `text ${row?.snapshot_text === sourceEvidence.snapshot_text ? "matches" : "differs"}, ` +
          `reason ${row?.reason === sourceEvidence.reason ? "matches" : "differs"}`,
      );
    }
    // A retry after the restore must land on the original evidence rather than
    // replacing it: the same message, previewed again and confirmed, is a duplicate.
    const retryPreview = await api(
      restored.base,
      "GET",
      `/api/messages/${reportedMessage.id}/private-report-preview`,
      { cookie: fresh.alpha },
    );
    const retryEvidence = await api(
      restored.base,
      "POST",
      `/api/messages/${reportedMessage.id}/private-report`,
      {
        cookie: fresh.alpha,
        body: {
          reason: "a retry after the restore",
          version: retryPreview.body?.version,
        },
      },
    );
    check(
      "a retry after the restore deduplicates onto the original evidence",
      retryEvidence.status === 200 &&
        retryEvidence.body?.id === evidenceID &&
        retryEvidence.body?.duplicate === true,
      `HTTP ${retryEvidence.status} id=${retryEvidence.body?.id}`,
    );

    // The request-evidence operation has to survive the restore as a whole: the
    // evidence, the block it committed and the command receipt that makes a retry
    // safe are three parts of one decision, and a restore that kept only some of
    // them would leave delta able to report the same introduction twice.
    {
      const db = new DatabaseSync(path.join(restoredDir, "brrp.db"), {
        readOnly: true,
      });
      const row = db
        .prepare(
          "SELECT id,status,expires_at,snapshot_text,reason FROM private_request_reports WHERE id=?",
        )
        .get(requestGDEvidenceID);
      const receipt = db
        .prepare(
          "SELECT outcome,source_kind FROM private_report_receipts WHERE operation_key=? AND user_id=?",
        )
        .get("p10-rehearsal-report-and-block", ids.delta);
      db.close();
      check(
        "the request evidence survived the restore",
        row?.id === requestGDEvidenceID,
        row ? "present" : "missing",
      );
      check(
        "the restored request evidence keeps its exact expiry, text and reason",
        row?.expires_at === sourceRequestEvidence.expires_at &&
          row?.snapshot_text === sourceRequestEvidence.snapshot_text &&
          row?.reason === sourceRequestEvidence.reason &&
          row?.status === sourceRequestEvidence.status,
        `expires ${row?.expires_at === sourceRequestEvidence.expires_at ? "matches" : "differs"}, ` +
          `text ${row?.snapshot_text === sourceRequestEvidence.snapshot_text ? "matches" : "differs"}`,
      );
      check(
        "the command receipt survived the restore",
        receipt?.source_kind === "request" && Boolean(receipt?.outcome),
        `source_kind=${receipt?.source_kind}`,
      );
    }
    // The block is the durable half. The reported introduction is gone from the
    // recipient's side, the sender is still barred, and a restore that carried the
    // request back would undo a decision the snapshot happened after.
    const graceWriteAfter = await api(
      restored.base,
      "POST",
      `/api/conversations/${pairGD}/messages`,
      {
        cookie: fresh.grace,
        body: {
          content: "after the restore",
          idempotency_key: "p10-rehearsal-grace-write-2",
        },
      },
    );
    check(
      "the restore did not resurrect the reported request",
      graceWriteAfter.status === 404,
      `HTTP ${graceWriteAfter.status}`,
    );
    const graceRetryAfter = await api(restored.base, "POST", "/api/directs", {
      cookie: fresh.grace,
      body: {
        user_id: ids.delta,
        introduction: "grace tries again after the restore",
      },
    });
    check(
      "the block survives the restore",
      graceRetryAfter.status >= 400,
      `HTTP ${graceRetryAfter.status} ${JSON.stringify(graceRetryAfter.body ?? {})}`,
    );
    const graceAfterRestore = await api(restored.base, "GET", "/api/directs", {
      cookie: fresh.grace,
    });
    const gracePairAfter = (graceAfterRestore.body ?? []).find(
      (direct) => direct.id === pairGD,
    );
    check(
      "the restore did not re-open contact that reporting never accepted",
      gracePairAfter?.request_state !== "accepted" &&
        gracePairAfter?.blocked === true,
      `state=${gracePairAfter?.request_state} blocked=${gracePairAfter?.blocked}`,
    );
    // A retry with the same operation key returns the original receipt rather than
    // reporting a second time: the restored key is still spent.
    const retryAfterRestore = await api(
      restored.base,
      "POST",
      `/api/directs/${pairGD}/report-and-block`,
      {
        cookie: fresh.delta,
        body: {
          reason: "rehearsal introduction concern, not a real one",
          operation_key: "p10-rehearsal-report-and-block",
          version: requestGDPreview.body?.version,
        },
      },
    );
    check(
      "a retry after the restore returns the original receipt",
      retryAfterRestore.status === 200 &&
        retryAfterRestore.body?.id === requestGDEvidenceID,
      `HTTP ${retryAfterRestore.status} id=${retryAfterRestore.body?.id}`,
    );
    // The key is bound to the reason, so reusing it for different work is a conflict
    // rather than a second report. A restore that lost that binding would silently
    // file a new report under a spent key.
    const reusedKey = await api(
      restored.base,
      "POST",
      `/api/directs/${pairGD}/report-and-block`,
      {
        cookie: fresh.delta,
        body: {
          reason: "a different reason under a spent key",
          operation_key: "p10-rehearsal-report-and-block",
          version: requestGDPreview.body?.version,
        },
      },
    );
    check(
      "a spent operation key cannot be reused for different work",
      reusedKey.status === 409,
      `HTTP ${reusedKey.status}`,
    );

    const deltaDirects = await api(restored.base, "GET", "/api/directs", {
      cookie: fresh.delta,
    });
    const deltaCD = (deltaDirects.body ?? []).find(
      (direct) => direct.id === pairCD,
    );
    check(
      "delta still sees carol's pending incoming request",
      deltaCD?.request_state === "pending" &&
        deltaCD?.request_direction === "incoming",
      `state=${deltaCD?.request_state}/${deltaCD?.request_direction}`,
    );
    // The outstanding incoming-request badge is derived from current state, not
    // stored, so it must be reconstructed by the restore rather than carried in it.
    // delta has one genuinely pending incoming request; alpha's pair is accepted, so
    // a count that merely echoed the conversation list would fail here.
    const deltaState = await api(restored.base, "GET", "/api/state", {
      cookie: fresh.delta,
    });
    check(
      "the restored snapshot reconstructs delta's outstanding request count",
      deltaState.body?.requests?.outstanding_incoming === 1,
      `count=${deltaState.body?.requests?.outstanding_incoming}`,
    );
    const alphaState = await api(restored.base, "GET", "/api/state", {
      cookie: fresh.alpha,
    });
    check(
      "an account with no pending request reports a zero count",
      alphaState.body?.requests?.outstanding_incoming === 0,
      `count=${alphaState.body?.requests?.outstanding_incoming}`,
    );
    const echoDirects = await api(restored.base, "GET", "/api/directs", {
      cookie: fresh.echo,
    });
    const echoEC = (echoDirects.body ?? []).find(
      (direct) => direct.peer?.id === ids.carol,
    );
    check(
      "echo's rejected request stays rejected",
      echoEC?.request_state === "rejected",
      `state=${echoEC?.request_state}`,
    );
    const refusedRetry = await api(restored.base, "POST", "/api/directs", {
      cookie: fresh.echo,
      body: { user_id: ids.carol, introduction: "echo asks again immediately" },
    });
    check(
      "the rejection cooldown survives the restore",
      refusedRetry.status === 429,
      `HTTP ${refusedRetry.status}`,
    );
    const optedOut = await api(restored.base, "POST", "/api/directs", {
      cookie: fresh.alpha,
      body: {
        user_id: ids.echo,
        introduction: "alpha asks echo who opted out",
      },
    });
    check(
      "echo's contact opt-out survives the restore",
      optedOut.status === 404,
      `HTTP ${optedOut.status}`,
    );

    const betaDB = await api(
      restored.base,
      "GET",
      `/api/conversations/${pairDB}/messages`,
      { cookie: fresh.beta },
    );
    check(
      "the blocked pair's history stays readable",
      betaDB.status === 200,
      `HTTP ${betaDB.status}`,
    );
    const blockedWrite = await api(
      restored.base,
      "POST",
      `/api/conversations/${pairDB}/messages`,
      {
        cookie: fresh.beta,
        body: {
          content: "write against the pre-snapshot block",
          idempotency_key: "kdb-blocked-1",
        },
      },
    );
    check(
      "the pre-snapshot block still refuses writes after restore",
      blockedWrite.status === 403,
      `HTTP ${blockedWrite.status}`,
    );

    const frankHistory = await api(
      restored.base,
      "GET",
      `/api/conversations/${pairFB}/messages`,
      { cookie: fresh.beta },
    );
    check(
      "the removed peer's permitted history survived",
      frankHistory.status === 200 &&
        countContent(frankHistory.body, "frank words") === 1,
      `HTTP ${frankHistory.status}`,
    );
    const frankEntry = (
      await api(restored.base, "GET", "/api/directs", { cookie: fresh.beta })
    ).body?.find((direct) => direct.id === pairFB);
    check(
      "the removed peer presents as the Deleted member identity",
      frankEntry?.peer?.is_deleted === true,
      `peer deleted=${frankEntry?.peer?.is_deleted}`,
    );

    // A reviewer grant is an ordinary role row, so the restored copy reads it as
    // live even though it was revoked after the snapshot. An operator who cut over
    // without noticing would put a former reviewer back on the private evidence
    // queue — and on the restricted audit feed, which names reviewers.
    const echoRestored = await signIn(
      restored.base,
      "echo",
      memberPassword("echo"),
    );
    const echoQueueRestored = await api(
      restored.base,
      "GET",
      "/api/admin/private-reports",
      { cookie: echoRestored },
    );
    check(
      "the restored copy still reads the revoked reviewer grant as live",
      echoQueueRestored.status === 200,
      `HTTP ${echoQueueRestored.status}`,
    );
    // The restricted audit feed is gated on audit.read rather than on the evidence
    // capability: a grant made to see submitted text is not a grant to read an audit
    // trail (completion review 13, V13-02). The rehearsal's reviewer holds both, so
    // the restored grant is still visible here.
    const echoAuditRestored = await api(
      restored.base,
      "GET",
      "/api/admin/private-reports/audit",
      { cookie: echoRestored },
    );
    check(
      "and it restores that access to the restricted audit feed as well",
      echoAuditRestored.status === 200,
      `HTTP ${echoAuditRestored.status}`,
    );

    // Post-snapshot decisions cannot travel back in time with the archive: the
    // restored copy answers from the snapshot, and the operator must re-apply them
    // during cutover (demonstrated next, before this rehearsal declares success).
    const deltaLogin = await api(restored.base, "POST", "/api/login", {
      body: { username: "delta", password: memberPassword("delta") },
    });
    check(
      "delta's post-snapshot deactivation is absent from the snapshot",
      deltaLogin.status === 200,
      `HTTP ${deltaLogin.status}`,
    );
    const carolLoginRestored = await api(restored.base, "POST", "/api/login", {
      body: { username: "carol", password: memberPassword("carol") },
    });
    check(
      "carol's post-snapshot ban is absent from the snapshot",
      carolLoginRestored.status === 200,
      `HTTP ${carolLoginRestored.status}`,
    );
    const echoOldRestored = await api(restored.base, "POST", "/api/login", {
      body: { username: "echo", password: memberPassword("echo") },
    });
    check(
      "the snapshot still carries the pre-rotation credential",
      echoOldRestored.status === 200,
      `HTTP ${echoOldRestored.status}`,
    );
    const echoNewRestored = await api(restored.base, "POST", "/api/login", {
      body: { username: "echo", password: rotatedEchoPassword },
    });
    check(
      "the snapshot does not carry the post-snapshot credential",
      echoNewRestored.status !== 200,
      `HTTP ${echoNewRestored.status}`,
    );
    const ivyRestored = await api(restored.base, "POST", "/api/login", {
      body: { username: "ivy", password: memberPassword("ivy") },
    });
    check(
      "the account erased after the snapshot is still present in it",
      ivyRestored.status === 200,
      `HTTP ${ivyRestored.status}`,
    );
    const alphaBefore = await api(
      restored.base,
      "POST",
      `/api/conversations/${pairAB}/messages`,
      {
        cookie: fresh.alpha,
        body: {
          content: "write before reconciliation",
          idempotency_key: "kab-3-before-reconcile",
        },
      },
    );
    check(
      "beta's post-snapshot block is absent until reconciliation",
      alphaBefore.status === 201,
      `HTTP ${alphaBefore.status}`,
    );

    // A client that believes the acknowledgement was lost replays the recorded send;
    // the restored receipt answers it without creating the message a second time.
    const listBefore = await api(
      restored.base,
      "GET",
      `/api/conversations/${pairAB}/messages`,
      { cookie: fresh.beta },
    );
    const replayedAfter = await api(
      restored.base,
      "POST",
      `/api/conversations/${pairAB}/messages`,
      {
        cookie: fresh.alpha,
        body: {
          content: "private seed wording quokka",
          idempotency_key: "kab-1-receipt",
          attachment_ids: [attachmentID],
        },
      },
    );
    const listAfter = await api(
      restored.base,
      "GET",
      `/api/conversations/${pairAB}/messages`,
      { cookie: fresh.beta },
    );
    check(
      "the receipt survived and answers the replay without a duplicate",
      replayedAfter.status === 200 &&
        replayedAfter.body.id === firstMessageID &&
        countContent(listAfter.body, "quokka") ===
          countContent(listBefore.body, "quokka"),
      `HTTP ${replayedAfter.status}`,
    );

    // Reconciliation before cutover. Both decisions the rehearsal deliberately
    // lost are re-applied: the old rehearsal took the post-snapshot block away in
    // step 3 but only re-applied that one, so the deactivation it also lost was
    // never proven to be recoverable and the rehearsal could declare success with
    // a deactivated account still able to sign in on the copy an operator is about
    // to put in front of members (review 9, R9-05).
    const reblock = await api(
      restored.base,
      "PUT",
      `/api/me/blocks/${ids.alpha}`,
      { cookie: fresh.beta, body: {} },
    );
    check(
      "reconciliation re-applies the post-snapshot block",
      reblock.status === 200,
      `HTTP ${reblock.status}`,
    );
    const redeactivate = await api(
      restored.base,
      "PATCH",
      `/api/admin/users/${ids.delta}`,
      {
        cookie: fresh.owner,
        body: { is_deactivated: true, confirm: "delta" },
      },
    );
    check(
      "reconciliation re-applies the post-snapshot deactivation",
      redeactivate.status === 200,
      `HTTP ${redeactivate.status}`,
    );
    const deltaAfterReconcile = await api(restored.base, "POST", "/api/login", {
      body: { username: "delta", password: memberPassword("delta") },
    });
    check(
      "the reconciled deactivation denies sign-in before cutover",
      deltaAfterReconcile.status !== 200,
      `HTTP ${deltaAfterReconcile.status}`,
    );
    const deltaAfterDeactivation = await api(
      restored.base,
      "GET",
      "/api/state",
      { cookie: fresh.delta },
    );
    // The diagnostic reports this request's own status. It previously printed an
    // earlier check's response, so a failure here read as a 200 while the session
    // had in fact been refused — evidence that cannot be trusted (completion
    // review 10, R10-08).
    check(
      "the deactivated account's earlier session no longer authorizes",
      deltaAfterDeactivation.status === 401,
      `HTTP ${deltaAfterDeactivation.status}`,
    );
    // Both decisions are the operator's and are recorded before traffic returns: a
    // rehearsal that re-applied only one cannot say cutover is safe. The revocation
    // goes through the same route the operator would use — no SQL fallback, and no
    // unconditional success (review 17, V17-05).
    const revokeGrant = await api(
      restored.base,
      "PUT",
      `/api/admin/users/${seededReviewerAccountID}/roles`,
      {
        cookie: fresh.owner,
        body: { role_ids: [] },
      },
    );
    check(
      "reconciliation re-applies the post-snapshot reviewer revocation",
      revokeGrant.status === 200,
      `HTTP ${revokeGrant.status} ${JSON.stringify(revokeGrant.body ?? {})}`,
    );
    const echoReconciled = await signIn(
      restored.base,
      "echo",
      memberPassword("echo"),
    );
    const echoQueueReconciled = await api(
      restored.base,
      "GET",
      "/api/admin/private-reports",
      { cookie: echoReconciled },
    );
    check(
      "the reconciled revocation denies the evidence queue before cutover",
      echoQueueReconciled.status === 403,
      `HTTP ${echoQueueReconciled.status}`,
    );
    const echoAuditReconciled = await api(
      restored.base,
      "GET",
      "/api/admin/private-reports/audit",
      { cookie: echoReconciled },
    );
    check(
      "and denies the restricted audit feed, which names reviewers",
      echoAuditReconciled.status === 403,
      `HTTP ${echoAuditReconciled.status}`,
    );
    // A session established while the grant was still live must stop authorizing:
    // a restored authority decision is not carried by the session that observed it.
    const echoStale = await api(
      restored.base,
      "GET",
      "/api/admin/private-reports",
      { cookie: echoRestored },
    );
    check(
      "the earlier reviewer's session no longer authorizes",
      echoStale.status === 403,
      `HTTP ${echoStale.status}`,
    );
    // The ban, the rotated credential and the erasure, re-applied the same way.
    const rebanned = await api(
      restored.base,
      "PATCH",
      `/api/admin/users/${ids.carol}`,
      {
        cookie: fresh.owner,
        body: { is_banned: true, confirm: "carol" },
      },
    );
    check(
      "reconciliation re-applies the post-snapshot ban",
      rebanned.status === 200,
      `HTTP ${rebanned.status}`,
    );
    const carolAfterBan = await api(restored.base, "POST", "/api/login", {
      body: { username: "carol", password: memberPassword("carol") },
    });
    check(
      "the reconciled ban denies sign-in before cutover",
      carolAfterBan.status !== 200,
      `HTTP ${carolAfterBan.status}`,
    );
    const echoForReconcile = await signIn(
      restored.base,
      "echo",
      memberPassword("echo"),
    );
    const rerotate = await api(restored.base, "POST", "/api/me/password", {
      cookie: echoForReconcile,
      body: {
        current_password: memberPassword("echo"),
        new_password: rotatedEchoPassword,
      },
    });
    check(
      "reconciliation re-applies the post-snapshot password rotation",
      rerotate.status === 200,
      `HTTP ${rerotate.status}`,
    );
    const newCredential = await api(restored.base, "POST", "/api/login", {
      body: { username: "echo", password: rotatedEchoPassword },
    });
    check(
      "the reconciled rotation admits the new credential",
      newCredential.status === 200,
      `HTTP ${newCredential.status}`,
    );
    const reErased = await api(
      restored.base,
      "DELETE",
      `/api/admin/users/${ids.ivy}`,
      {
        cookie: fresh.owner,
        body: { confirm: "ivy" },
      },
    );
    check(
      "reconciliation re-applies the post-snapshot erasure",
      reErased.status === 200,
      `HTTP ${reErased.status}`,
    );
    const ivyAfterErasure = await api(restored.base, "POST", "/api/login", {
      body: { username: "ivy", password: memberPassword("ivy") },
    });
    check(
      "the reconciled erasure denies the erased account before cutover",
      ivyAfterErasure.status !== 200,
      `HTTP ${ivyAfterErasure.status}`,
    );
    postSnapshot.reconciledBeforeCutover = [
      "block",
      "deactivation",
      "reviewer-grant revocation",
      "ban",
      "password rotation",
      "erasure",
    ];
    // What the artifact cannot carry is stated rather than implied: sessions are
    // deliberately not in a backup, so every pre-restore session is replaced by fresh
    // sign-ins (asserted above), and nothing else in this fixture is unreconstructable
    // — every post-snapshot decision was re-applied through its own route before
    // cutover.
    postSnapshot.unreconstructable = [
      "sessions: deliberately not carried in the archive, replaced by fresh sign-ins",
    ];
    const alphaAfter = await api(
      restored.base,
      "POST",
      `/api/conversations/${pairAB}/messages`,
      {
        cookie: fresh.alpha,
        body: {
          content: "write after reconciliation",
          idempotency_key: "kab-4-after-reconcile",
        },
      },
    );
    check(
      "the reconciled block refuses new writes again",
      alphaAfter.status === 403,
      `HTTP ${alphaAfter.status}`,
    );
    const historyAfter = await api(
      restored.base,
      "GET",
      `/api/conversations/${pairAB}/messages`,
      { cookie: fresh.beta },
    );
    check(
      "readability under the reconciled block is retained",
      historyAfter.status === 200,
      `HTTP ${historyAfter.status}`,
    );
  }
  await stopServer(restored);

  say("");
  say("6. The comparisons can fail (negative control)");
  // The assertions above are evidence only if loss makes them fail. A copy of the
  // restored database has the thread follow, the thread read position, the group
  // membership and the group-message preference damaged, and the same comparisons the
  // run made must report exactly those tables as mismatched — a rehearsal whose checks
  // cannot fail would pass for a restore that dropped the state (review 17, V17-05).
  {
    const readRows = (dir, sql, ...args) => {
      const db = new DatabaseSync(path.join(dir, "brrp.db"), {
        readOnly: true,
      });
      try {
        return db.prepare(sql).all(...args);
      } finally {
        db.close();
      }
    };
    const PRESERVED = {
      thread_follows:
        "SELECT user_id,thread_id,created_at FROM thread_follows ORDER BY user_id,thread_id",
      thread_reads:
        "SELECT user_id,thread_id,last_read_message_id FROM thread_reads ORDER BY user_id,thread_id",
      group_members:
        "SELECT conversation_id,user_id,state FROM conversation_members WHERE conversation_id=? ORDER BY user_id",
      group_preferences:
        "SELECT user_id,kind,muted,show_sender FROM private_notification_preferences ORDER BY user_id,kind",
    };
    const beforeControl = Object.fromEntries(
      Object.entries(PRESERVED).map(([name, sql]) => [
        name,
        name === "group_members"
          ? readRows(restoredDir, sql, groupID)
          : readRows(restoredDir, sql),
      ]),
    );
    check(
      "the negative control has rows to damage",
      beforeControl.thread_follows.length >= 1 &&
        beforeControl.thread_reads.length >= 1 &&
        beforeControl.group_members.length === 2 &&
        beforeControl.group_preferences.some(
          (row) => row.kind === "group_message",
        ),
      `follows=${beforeControl.thread_follows.length} reads=${beforeControl.thread_reads.length} ` +
        `members=${beforeControl.group_members.length} prefs=${beforeControl.group_preferences.length}`,
    );
    const damaged = `${restoredDir}-damaged`;
    rmSync(damaged, { recursive: true, force: true });
    cpSync(restoredDir, damaged, { recursive: true });
    {
      const db = new DatabaseSync(path.join(damaged, "brrp.db"));
      try {
        db.exec(
          "DELETE FROM thread_follows WHERE rowid IN (SELECT rowid FROM thread_follows LIMIT 1)",
        );
        db.exec(
          "DELETE FROM thread_reads WHERE rowid IN (SELECT rowid FROM thread_reads LIMIT 1)",
        );
        db.exec(
          "UPDATE private_notification_preferences SET muted=0 WHERE kind='group_message'",
        );
      } finally {
        db.close();
      }
    }
    const mismatched = Object.entries(PRESERVED)
      .filter(([name, sql]) => {
        const now =
          name === "group_members"
            ? readRows(damaged, sql, groupID)
            : readRows(damaged, sql);
        return JSON.stringify(now) !== JSON.stringify(beforeControl[name]);
      })
      .map(([name]) => name);
    check(
      "damaged thread and preference state is reported as a mismatch",
      mismatched.length === 3 &&
        mismatched.includes("thread_follows") &&
        mismatched.includes("thread_reads") &&
        mismatched.includes("group_preferences"),
      mismatched.join(",") || "none",
    );
    rmSync(damaged, { recursive: true, force: true });
  }

  say("");
  const failed = checks.filter((entry) => !entry.ok);
  say(
    `Rehearsal ${failed.length === 0 ? "PASS" : "FAIL"}: ${checks.length} checks, ${failed.length} failed.`,
  );
  say(
    "Scope: local restore into an isolated destination with fresh sessions; not off-host recovery.",
  );

  const summary = {
    rehearsal: "upgrade-recovery-private",
    passed: failed.length === 0,
    binary,
    binary_sha256: binaryDigest,
    feature_mode: "private conversations enabled on disposable servers",
    work_dir: workDir,
    archive: {
      path: archive,
      sha256: archiveDigest,
      bytes: statSync(archive).size,
    },
    post_snapshot_decisions: postSnapshot,
    reconciliation:
      "every post-snapshot decision (block, deactivation, reviewer-grant revocation, ban, password rotation and erasure) was absent from the restored copy and re-applied explicitly on the isolated server before cutover, as the runbook requires",
    checks,
    transcript,
  };
  if (options.report)
    writeFileSync(
      path.resolve(options.report),
      `${JSON.stringify(summary, null, 2)}\n`,
    );
  return { summary, workDir };
}

let outcome;
try {
  outcome = await main();
} catch (error) {
  console.error(`\nRehearsal stopped: ${error.message}`);
  console.error(
    `Workspace kept for inspection: ${workDirForCleanup ?? "(not created)"}`,
  );
  killLiveChildren();
  process.exit(1);
}
if (!options.keep) rmSync(outcome.workDir, { recursive: true, force: true });
else say(`Workspace kept: ${outcome.workDir}`);
process.exit(outcome.summary.passed ? 0 : 1);
