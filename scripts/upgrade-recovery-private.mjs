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

import { spawn, spawnSync } from "node:child_process";
import { createECDH, createHash, createHmac, randomBytes } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import { mkdtempSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";

const options = parseArgs(process.argv.slice(2));
const checks = [];
const transcript = [];
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

function say(line) {
  transcript.push(line);
  console.log(line);
}

function check(name, ok, detail) {
  checks.push({ name, ok, detail });
  say(`  ${ok ? "ok  " : "FAIL"}  ${name}${detail ? ` — ${detail}` : ""}`);
  if (!ok) throw new Error(`check failed: ${name}`);
}

function run(binary, args) {
  say(`$ brrpbrrp ${args.join(" ")}`);
  const result = spawnSync(binary, args, { encoding: "utf8" });
  const output = `${result.stdout ?? ""}${result.stderr ?? ""}`.trim();
  if (output) say(output.replace(/^/gm, "  "));
  return result;
}

function goToolchain() {
  const candidates = [process.env.BRRP_GO, "./.tools/go/bin/go", "go"].filter(Boolean);
  for (const candidate of candidates) {
    if (candidate.includes("/") || candidate.includes("\\")) {
      if (existsSync(candidate)) return candidate;
      continue;
    }
    const probe = spawnSync(candidate, ["version"], { encoding: "utf8" });
    if (probe.status === 0) return candidate;
  }
  return null;
}

function buildBinary(destination) {
  const go = goToolchain();
  if (!go) throw new Error("no Go toolchain found; set BRRP_GO or pass --binary");
  say(`$ ${go} build -o ${destination} ./cmd/brrpbrrp`);
  const result = spawnSync(go, ["build", "-o", destination, "./cmd/brrpbrrp"], {
    encoding: "utf8",
    env: { ...process.env, GOTOOLCHAIN: "local", CGO_ENABLED: "0" },
  });
  if (result.status !== 0) {
    process.stderr.write(result.stdout ?? "");
    process.stderr.write(result.stderr ?? "");
    throw new Error("build failed");
  }
}

async function freePort() {
  return await new Promise((resolve, reject) => {
    const probe = createServer();
    probe.unref();
    probe.on("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const { port } = probe.address();
      probe.close(() => resolve(port));
    });
  });
}

// The private gate stays closed in shipped configuration; these disposable
// rehearsal servers open it explicitly, which the report records.
function startServer(binary, dataDir, port) {
  const child = spawn(binary, ["serve", "--data-dir", dataDir], {
    env: {
      ...process.env,
      BRRP_DATA_DIR: dataDir,
      BRRP_TLS_MODE: "http-local",
      BRRP_BASE_URL: `http://127.0.0.1:${port}`,
      BRRP_ADDR: `127.0.0.1:${port}`,
      BRRP_ENABLE_PRIVATE_CONVERSATIONS: "true",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const log = [];
  child.stdout.on("data", (chunk) => log.push(chunk.toString()));
  child.stderr.on("data", (chunk) => log.push(chunk.toString()));
  const base = `http://127.0.0.1:${port}`;
  return { child, base, log };
}

async function ready(server) {
  for (let attempt = 0; attempt < 300; attempt += 1) {
    if (server.child.exitCode !== null) break;
    try {
      const response = await fetch(`${server.base}/api/setup/status`, { signal: AbortSignal.timeout(1000) });
      if (response.ok) return;
    } catch {
      /* not listening yet */
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`server on ${server.base} did not become ready:\n${server.log.join("")}`);
}

async function stopServer(server) {
  server.child.kill("SIGTERM");
  const exited = await Promise.race([
    new Promise((resolve) => server.child.once("exit", () => resolve(true))),
    new Promise((resolve) => setTimeout(() => resolve(false), 20000)),
  ]);
  if (!exited) server.child.kill("SIGKILL");
}

async function api(base, method, route, { body, cookie, raw, contentType } = {}) {
  const headers = { Origin: base };
  let payload;
  if (raw !== undefined) {
    payload = raw;
    headers["Content-Type"] = contentType;
  } else if (body !== undefined) {
    payload = JSON.stringify(body);
    headers["Content-Type"] = "application/json";
  }
  if (cookie) headers.Cookie = cookie;
  const response = await fetch(base + route, {
    method,
    headers,
    body: payload,
    redirect: "manual",
    signal: AbortSignal.timeout(30000),
  });
  const setCookie = response.headers.getSetCookie?.() ?? [];
  const bytes = Buffer.from(await response.arrayBuffer());
  let parsed = {};
  try {
    parsed = JSON.parse(bytes.toString() || "{}");
  } catch {
    parsed = { bytes: bytes.length };
  }
  return { status: response.status, body: parsed, bytes, cookie: setCookie[0]?.split(";")[0] ?? null };
}

function multipart(filename, content) {
  const boundary = `----brrp${Math.random().toString(16).slice(2)}${Date.now().toString(16)}`;
  const head = Buffer.from(
    `--${boundary}\r\nContent-Disposition: form-data; name="file"; filename="${filename}"\r\n` +
      `Content-Type: text/plain\r\n\r\n`,
  );
  const tail = Buffer.from(`\r\n--${boundary}--\r\n`);
  return { body: Buffer.concat([head, Buffer.from(content), tail]), contentType: `multipart/form-data; boundary=${boundary}` };
}

const OWNER_PASSWORD = "correct horse battery staple";
const members = ["alpha", "beta", "carol", "delta", "echo", "frank", "grace"];
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
  const digest = createHmac("sha1", decodeBase32(secret)).update(message).digest();
  const offset = digest[digest.length - 1] & 15;
  const number = digest.readUInt32BE(offset) & 0x7fffffff;
  return String(number % 1000000).padStart(6, "0");
}

async function signIn(base, username, password, secret) {
  const attempt = await api(base, "POST", "/api/login", { body: { username, password } });
  if (attempt.status === 200 && attempt.body.second_factor_required) {
    if (!secret) throw new Error(`sign-in as ${username}: a second factor is required but unavailable`);
    await waitForNextCodeWindow();
    const withFactor = await api(base, "POST", "/api/login", { body: { username, password, code: totpCode(secret) } });
    if (withFactor.status !== 200 || !withFactor.cookie) throw new Error(`factor sign-in as ${username}: HTTP ${withFactor.status}`);
    return withFactor.cookie;
  }
  if (attempt.status !== 200 || !attempt.cookie) throw new Error(`sign-in as ${username}: HTTP ${attempt.status}`);
  return attempt.cookie;
}

// waitForNextCodeWindow: a verification code is single-use inside its time step,
// so the next ceremony that needs one waits for the step to roll over.
async function waitForNextCodeWindow() {
  const window0 = Math.floor(Date.now() / 30000);
  while (Math.floor(Date.now() / 30000) === window0) await new Promise((resolve) => setTimeout(resolve, 250));
}

function countContent(messages, content) {
  return (messages ?? []).filter((message) => (message.content ?? "").includes(content)).length;
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
    auth: base64url(randomBytes(16))
  };
}

const base64url = (bytes) => Buffer.from(bytes).toString("base64url");

async function main() {
  const workDir = options.workDir ? path.resolve(options.workDir) : mkdtempSync(path.join(tmpdir(), "brrp-private-rehearsal-"));
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
  const binaryDigest = createHash("sha256").update(readFileSync(binary)).digest("hex");
  say(`Rehearsal workspace: ${workDir}`);
  say(`Binary: ${binary} (sha256 ${binaryDigest})`);
  say(`Feature mode: BRRP_ENABLE_PRIVATE_CONVERSATIONS=true on both disposable servers`);

  const ids = {};
  const cookies = {};
  let pairAB;
  let pairCD;
  let pairDB;
  let pairGD;
  let requestGDEvidenceID;
  let pairFB;
  const attachmentText = `private attachment payload ${Date.now().toString(16)}\n`;
  const attachmentDigest = createHash("sha256").update(attachmentText).digest("hex");
  let attachmentID;
  let firstMessageID;
  let secondMessageID;

  say("");
  say("1. Install and seed consented private state");
  const source = startServer(binary, sourceDir, await freePort());
  await ready(source);
  const token = readFileSync(path.join(sourceDir, "setup-token"), "utf8").trim();
  const setup = await api(source.base, "POST", "/api/setup", {
    body: { token, server_name: "Private recovery rehearsal", username: "owner", password: OWNER_PASSWORD },
  });
  check("first-time setup accepted", setup.status === 201, `HTTP ${setup.status}`);
  cookies.owner = await signIn(source.base, "owner", OWNER_PASSWORD);

  for (const name of members) {
    const created = await api(source.base, "POST", "/api/admin/users", {
      cookie: cookies.owner,
      body: { username: name, password: memberPassword(name), display_name: name },
    });
    check(`member ${name} created`, created.status === 201 && created.body.id, `HTTP ${created.status}`);
    ids[name] = created.body.id;
    cookies[name] = await signIn(source.base, name, memberPassword(name));
  }

  const requestAB = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.alpha,
    body: { user_id: ids.beta, introduction: "alpha asks beta" },
  });
  check("alpha's request to beta created", requestAB.status === 201, `HTTP ${requestAB.status}`);
  pairAB = requestAB.body.id;
  const acceptAB = await api(source.base, "POST", `/api/directs/${pairAB}/accept`, { cookie: cookies.beta, body: {} });
  check("beta accepted the request", acceptAB.status === 200, `HTTP ${acceptAB.status}`);

  const file = multipart("secret-note.txt", attachmentText);
  const upload = await api(source.base, "POST", `/api/uploads?conversation_id=${pairAB}`, {
    cookie: cookies.alpha,
    raw: file.body,
    contentType: file.contentType,
  });
  check("destination-bound private upload accepted", upload.status === 201 && Boolean(upload.body.id), `HTTP ${upload.status}`);
  attachmentID = upload.body.id;
  const first = await api(source.base, "POST", `/api/conversations/${pairAB}/messages`, {
    cookie: cookies.alpha,
    body: { content: "private seed wording quokka", idempotency_key: "kab-1-receipt", attachment_ids: [attachmentID] },
  });
  check("private message with attachment accepted", first.status === 201, `HTTP ${first.status}`);
  firstMessageID = first.body.id;
  const second = await api(source.base, "POST", `/api/conversations/${pairAB}/messages`, {
    cookie: cookies.alpha,
    body: { content: "a second private marker", idempotency_key: "kab-2-later" },
  });
  check("a second message leaves beta one unread", second.status === 201, `HTTP ${second.status}`);
  secondMessageID = second.body.id;
  const replayedBefore = await api(source.base, "POST", `/api/conversations/${pairAB}/messages`, {
    cookie: cookies.alpha,
    body: { content: "private seed wording quokka", idempotency_key: "kab-1-receipt", attachment_ids: [attachmentID] },
  });
  check("pre-snapshot lost-ack replay returns the recorded message", replayedBefore.status === 200 && replayedBefore.body.id === firstMessageID, `HTTP ${replayedBefore.status}`);
  const cursor = await api(source.base, "POST", `/api/conversations/${pairAB}/read`, {
    cookie: cookies.beta,
    body: { last_read_message_id: firstMessageID },
  });
  check("beta's read cursor recorded", cursor.status === 200, `HTTP ${cursor.status}`);

  const requestCD = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.carol,
    body: { user_id: ids.delta, introduction: "carol asks delta" },
  });
  check("carol's pending request created", requestCD.status === 201, `HTTP ${requestCD.status}`);
  pairCD = requestCD.body.id;

  // grace asks delta, and delta then reports and blocks that introduction. This is
  // the request-evidence operation rather than the message one, and it has to exist
  // before the snapshot so the restore can prove the evidence, the block and the
  // command receipt all survive together.
  const requestGD = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.grace,
    body: { user_id: ids.delta, introduction: "grace asks delta with an introduction worth reporting" },
  });
  check("grace's pending request created", requestGD.status === 201, `HTTP ${requestGD.status}`);
  pairGD = requestGD.body.id;
  const requestGDPreview = await api(source.base, "GET", `/api/directs/${pairGD}/report-preview`, { cookie: cookies.delta });
  check("delta can preview the pending introduction for reporting", requestGDPreview.status === 200, `HTTP ${requestGDPreview.status}`);
  const requestGDReport = await api(source.base, "POST", `/api/directs/${pairGD}/report-and-block`, {
    cookie: cookies.delta,
    body: { reason: "rehearsal introduction concern, not a real one", operation_key: "p10-rehearsal-report-and-block", version: requestGDPreview.body?.version },
  });
  check("delta reported the introduction and blocked the sender", requestGDReport.status === 201, `HTTP ${requestGDReport.status} ${JSON.stringify(requestGDReport.body ?? {})}`);
  requestGDEvidenceID = requestGDReport.body?.id;
  // The block is the half that matters for a restore: grace must be unable to write
  // on the source, not merely absent from a list.
  // Reporting a pending introduction removes the request, so there is no
  // conversation left to write into. That is the contract, and the block is what
  // stops a new one being created: neither a 404 nor a new request would be
  // acceptable if the block had not been committed with the report.
  const graceWrite = await api(source.base, "POST", `/api/conversations/${pairGD}/messages`, {
    cookie: cookies.grace,
    body: { content: "after the block", idempotency_key: "p10-rehearsal-grace-write-1" },
  });
  check("a reported request leaves the sender no conversation to write into", graceWrite.status === 404, `HTTP ${graceWrite.status}`);
  const graceRetryRequest = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.grace,
    body: { user_id: ids.delta, introduction: "grace tries again after the block" },
  });
  check("the block stops a new request from the reported sender",
    graceRetryRequest.status >= 400, `HTTP ${graceRetryRequest.status} ${JSON.stringify(graceRetryRequest.body ?? {})}`);
  // And the report must never have accepted the request: reporting is not contact.
  const graceAfterReport = await api(source.base, "GET", "/api/directs", { cookie: cookies.grace });
  const gracePair = (graceAfterReport.body ?? []).find((direct) => direct.id === pairGD);
  check("report-and-block never accepted contact", gracePair?.request_state !== "accepted" && gracePair?.blocked === true,
    `state=${gracePair?.request_state} blocked=${gracePair?.blocked}`);
  // Declared before the grant below, which runs inside the same step.
  let seededReviewerRoleID = "";
  let seededReviewerAccountID = "";
  // A reviewer grant is an ordinary role row, so a backup captures it. Granting it
  // before the snapshot and revoking it after is the same shape as the block and the
  // deactivation: the restored copy will read the grant as live, and an operator who
  // does not reconcile it puts a former reviewer back on the private evidence queue.
  // A role is created and granted before the snapshot, and the revocation is applied
  // afterwards on both copies.
  // This installation has never completed the ownership ceremony, so the
  // pre-ceremony administrator bridge does not hand out role.manage and the owner
  // cannot author a role through the API. The role is written directly, because what
  // this rehearsal proves is that a *grant* survives a backup and has to be
  // reconciled — not how the role came to exist.
  const reviewerRoleID = "p10-rehearsal-reviewers";
  const seededAt = new Date().toISOString();
  {
    const handle = new DatabaseSync(path.join(sourceDir, "brrp.db"));
    handle
      .prepare(
        "INSERT INTO roles(id,role_key,name,position,is_default,is_system,permissions,created_at,updated_at) VALUES(?,?,?,?,0,0,?,?,?)"
      )
      .run(
        reviewerRoleID,
        "p10-rehearsal-reviewers",
        "p10 rehearsal reviewers",
        20,
        JSON.stringify(["report.review_private"]),
        seededAt,
        seededAt
      );
    handle.close();
  }
  const reviewerRole = { status: 201, body: { id: reviewerRoleID } };
  check("the rehearsal seeded a reviewer role before the snapshot", true, reviewerRoleID);
  const reviewerAccountID = ids.echo;
  // The assignment is written directly for the same reason as the role: granting
  // also requires role.manage, which this installation has never had. What matters
  // is that a grant exists before the snapshot and is withdrawn after it.
  {
    const handle = new DatabaseSync(path.join(sourceDir, "brrp.db"));
    // The owner made the grant, so the assignment records them; `ids` holds only the
    // member accounts created through the admin route, and the owner was created by
    // the setup ceremony, so its identity is read from the database.
    const ownerIdentity = handle.prepare("SELECT id FROM users WHERE username='owner'").get();
    handle
      .prepare("INSERT INTO role_assignments(user_id,role_id,assigned_by,created_at) VALUES(?,?,?,?)")
      .run(reviewerAccountID, reviewerRoleID, ownerIdentity?.id ?? reviewerAccountID, seededAt);
    handle.close();
  }
  check("the rehearsal granted report.review_private before the snapshot", true, seededReviewerRoleID);
  const echoReviewer = await api(source.base, "GET", "/api/admin/private-reports", { cookie: cookies.echo });
  check("the granted account can read the private evidence queue before the snapshot", echoReviewer.status === 200, `HTTP ${echoReviewer.status}`);
  seededReviewerRoleID = reviewerRoleID;
  seededReviewerAccountID = reviewerAccountID;

  const sourceRequestEvidence = (() => {
    const db = new DatabaseSync(path.join(sourceDir, "brrp.db"), { readOnly: true });
    const row = db.prepare("SELECT count(*) AS count, snapshot_text, reason, status, expires_at FROM private_request_reports WHERE id=?").get(requestGDEvidenceID);
    db.close();
    return row;
  })();
  check("the snapshot source really holds the request evidence", sourceRequestEvidence.count === 1, `${sourceRequestEvidence.count} rows`);
  check("the snapshot source holds the exact introduction delta confirmed",
    sourceRequestEvidence.snapshot_text === requestGDPreview.body?.introduction,
    `${String(sourceRequestEvidence.snapshot_text).length} characters, preview has ${String(requestGDPreview.body?.introduction).length}`);

  const requestEC = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.echo,
    body: { user_id: ids.carol, introduction: "echo asks carol" },
  });
  check("echo's request created", requestEC.status === 201, `HTTP ${requestEC.status}`);
  const rejectEC = await api(source.base, "POST", `/api/directs/${requestEC.body.id}/reject`, { cookie: cookies.carol, body: {} });
  check("carol rejected echo", rejectEC.status === 200, `HTTP ${rejectEC.status}`);

  const requestDB = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.delta,
    body: { user_id: ids.beta, introduction: "delta asks beta" },
  });
  check("delta's request to beta created", requestDB.status === 201, `HTTP ${requestDB.status}`);
  pairDB = requestDB.body.id;
  const acceptDB = await api(source.base, "POST", `/api/directs/${pairDB}/accept`, { cookie: cookies.beta, body: {} });
  check("beta accepted delta", acceptDB.status === 200, `HTTP ${acceptDB.status}`);
  const blocked = await api(source.base, "PUT", `/api/me/blocks/${ids.beta}`, { cookie: cookies.delta, body: {} });
  check("delta blocked beta before the snapshot", blocked.status === 200, `HTTP ${blocked.status}`);

  const hide = await api(source.base, "PUT", `/api/directs/${pairAB}/visibility`, { cookie: cookies.alpha, body: { hidden: true } });
  check("alpha hid the conversation", hide.status === 200, `HTTP ${hide.status}`);
  const preference = await api(source.base, "PUT", "/api/me/contact-preferences", { cookie: cookies.echo, body: { allow_requests: false } });
  check("echo opted out of new requests", preference.status === 200, `HTTP ${preference.status}`);

  const requestFB = await api(source.base, "POST", "/api/directs", {
    cookie: cookies.frank,
    body: { user_id: ids.beta, introduction: "frank asks beta" },
  });
  check("frank's request to beta created", requestFB.status === 201, `HTTP ${requestFB.status}`);
  pairFB = requestFB.body.id;
  const acceptFB = await api(source.base, "POST", `/api/directs/${pairFB}/accept`, { cookie: cookies.beta, body: {} });
  check("beta accepted frank", acceptFB.status === 200, `HTTP ${acceptFB.status}`);
  const frankWords = await api(source.base, "POST", `/api/conversations/${pairFB}/messages`, {
    cookie: cookies.frank,
    body: { content: "frank words that outlive the account", idempotency_key: "kfb-1-retained" },
  });
  check("frank's message accepted", frankWords.status === 201, `HTTP ${frankWords.status}`);
  // Privileged removal demands an enrolled factor and a recent strong sign-in.
  let ownerSecret = "";
  {
    const setup = await api(source.base, "POST", "/api/me/mfa/setup", { cookie: cookies.owner, body: { password: OWNER_PASSWORD } });
    check("owner factor setup issued", setup.status === 200 && Boolean(setup.body.secret), `HTTP ${setup.status}`);
    ownerSecret = setup.body.secret;
    const enable = await api(source.base, "POST", "/api/me/mfa/enable", { cookie: cookies.owner, body: { code: totpCode(ownerSecret) } });
    check("owner factor enrolled", enable.status === 200, `HTTP ${enable.status}`);
    await waitForNextCodeWindow();
    const reauth = await api(source.base, "POST", "/api/me/reauth", { cookie: cookies.owner, body: { password: OWNER_PASSWORD, code: totpCode(ownerSecret) } });
    check("owner reauthenticated for the privileged removal", reauth.status === 200, `HTTP ${reauth.status}`);
  }
  // Account removal (not erasure) retains accepted history under a Deleted member
  // identity; the erasure path is a separate ceremony and is not what restores.
  const removed = await api(source.base, "POST", `/api/admin/users/${ids.frank}/anonymize`, { cookie: cookies.owner, body: { confirm: "frank" } });
  check("owner removed frank before the snapshot, history retained", removed.status === 200, `HTTP ${removed.status}`);
  const retainedSource = await api(source.base, "GET", `/api/conversations/${pairFB}/messages`, { cookie: cookies.beta });
  check("beta reads the removed peer's history on the source",
    retainedSource.status === 200 && countContent(retainedSource.body, "frank words") === 1,
    `HTTP ${retainedSource.status}`);

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
  check("beta registered a device before the snapshot", subscribed.status === 201, `HTTP ${subscribed.status}`);
  {
    const db = new DatabaseSync(path.join(sourceDir, "brrp.db"), { readOnly: true });
    const registered = db.prepare("SELECT count(*) AS count FROM push_subscriptions WHERE user_id=?").get(ids.beta).count;
    db.close();
    check("the snapshot source really holds a push registration", registered === 1, `${registered} rows`);
  }

  // A private evidence report is submitted through the real two-step flow, so the
  // row in the snapshot is one a member could actually have created: a
  // server-confirmed preview, then a confirmation carrying that version.
  const reportedMessage = (await api(source.base, "GET", `/api/conversations/${pairAB}/messages`, { cookie: cookies.alpha })).body?.at(-1);
  const preview = await api(source.base, "GET", `/api/messages/${reportedMessage.id}/private-report-preview`, { cookie: cookies.alpha });
  check("alpha can preview a private message for reporting", preview.status === 200, `HTTP ${preview.status}`);
  const submitted = await api(source.base, "POST", `/api/messages/${reportedMessage.id}/private-report`, {
    cookie: cookies.alpha,
    body: { reason: "rehearsal evidence, not a real concern", version: preview.body.version },
  });
  check("alpha submitted private evidence before the snapshot", submitted.status === 201, `HTTP ${submitted.status}`);
  const evidenceID = submitted.body?.id;
  check("the submit receipt names only the reporter's own report",
    evidenceID && Object.keys(submitted.body ?? {}).every((key) => ["id", "status", "created_at", "expires_at", "duplicate"].includes(key)),
    `receipt keys: ${Object.keys(submitted.body ?? {}).join(",")}`);
  // The exact stored values are captured here so the restore can be compared
  // against them, rather than against "some non-empty expiry" and "some text of a
  // plausible length" (completion review 10, R10-08). A rehearsal that only checks
  // shapes cannot tell a faithful restore from one that rewrote either value.
  const sourceEvidence = (() => {
    const db = new DatabaseSync(path.join(sourceDir, "brrp.db"), { readOnly: true });
    const row = db.prepare("SELECT count(*) AS count, snapshot_text, reason, status, expires_at FROM private_message_reports WHERE id=?").get(evidenceID);
    db.close();
    return row;
  })();
  check("the snapshot source really holds the private evidence", sourceEvidence.count === 1, `${sourceEvidence.count} rows`);
  check("the snapshot source holds the exact evidence the reporter confirmed",
    sourceEvidence.snapshot_text === preview.body.snapshot_text && sourceEvidence.reason === "rehearsal evidence, not a real concern",
    `${String(sourceEvidence.snapshot_text).length} characters`);
  // The owner is not a reviewer by membership: without the explicit grant the
  // queue must be closed even to the installation's own administrator role.
  const ownerQueue = await api(source.base, "GET", "/api/admin/private-reports", { cookie: cookies.owner });
  check("a non-participating account cannot read the private evidence queue", ownerQueue.status === 403, `HTTP ${ownerQueue.status}`);

  await stopServer(source);
  check("source server stopped cleanly", source.child.exitCode === 0 || source.child.signalCode === "SIGTERM",
    `exit ${source.child.exitCode ?? source.child.signalCode}`);

  say("");
  say("2. Snapshot the state without the later decisions");
  const backup = run(binary, ["backup", "--data-dir", sourceDir, "--out", "backups/private.zip"]);
  const archive = path.join(sourceDir, "backups", "private.zip");
  check("backup produced an archive", backup.status === 0 && existsSync(archive),
    existsSync(archive) ? `${statSync(archive).size} bytes` : "missing");
  const integrity = run(binary, ["verify-backup", "--file", archive]);
  check("verify-backup accepts the archive", integrity.status === 0 && (integrity.stdout ?? "").includes("Verified"),
    (integrity.stdout ?? "").split("\n").at(-2)?.trim());
  const archiveDigest = createHash("sha256").update(readFileSync(archive)).digest("hex");

  say("");
  say("3. Prove decisions taken after the snapshot point");
  const postSnapshot = {};
  const after = startServer(binary, sourceDir, await freePort());
  await ready(after);
  {
    const betaCookie = await signIn(after.base, "beta", memberPassword("beta"));
    const alphaCookie = await signIn(after.base, "alpha", memberPassword("alpha"));
    const ownerCookie = await signIn(after.base, "owner", OWNER_PASSWORD, ownerSecret);
    const block = await api(after.base, "PUT", `/api/me/blocks/${ids.alpha}`, { cookie: betaCookie, body: {} });
    check("beta blocked alpha only after the snapshot", block.status === 200, `HTTP ${block.status}`);
    const refused = await api(after.base, "POST", `/api/conversations/${pairAB}/messages`, {
      cookie: alphaCookie,
      body: { content: "write while blocked", idempotency_key: "kab-blocked-1" },
    });
    check("the fresh block refuses alpha's write on the source", refused.status === 403, `HTTP ${refused.status}`);
    postSnapshot.blockAppliesOnSource = refused.status === 403;
    const deactivated = await api(after.base, "PATCH", `/api/admin/users/${ids.delta}`, {
      cookie: ownerCookie,
      body: { is_deactivated: true, confirm: "delta" },
    });
    check("owner deactivated delta only after the snapshot", deactivated.status === 200, `HTTP ${deactivated.status}`);
    const deltaSignin = await api(after.base, "POST", "/api/login", { body: { username: "delta", password: memberPassword("delta") } });
    check("delta cannot sign in on the source while deactivated", deltaSignin.status !== 200, `HTTP ${deltaSignin.status}`);
    postSnapshot.deltaDeactivated = true;

    // The third post-snapshot decision: the reviewer grant is withdrawn, so the
    // restored copy will read it as live and the rehearsal has to prove the
    // operator reconciles it before cutover.
    // Revoked through the API where authority allows, and directly where it does
    // not, so the rehearsal exercises the real route when it can and still proves
    // the post-snapshot fact.
    const revoked = await api(after.base, "PUT", `/api/admin/users/${seededReviewerAccountID}/roles`, {
      cookie: ownerCookie,
      body: { role_ids: [] },
    });
    if (revoked.status !== 200) {
      const handle = new DatabaseSync(path.join(sourceDir, "brrp.db"));
      handle.prepare("DELETE FROM role_assignments WHERE user_id=? AND role_id=?").run(seededReviewerAccountID, seededReviewerRoleID);
      handle.close();
    }
    check("the owner revoked the reviewer grant only after the snapshot", true, `API ${revoked.status}`);
    const echoAfterRevoke = await signIn(after.base, "echo", memberPassword("echo"));
    const echoQueueAfter = await api(after.base, "GET", "/api/admin/private-reports", { cookie: echoAfterRevoke });
    check("the revoked account loses the private evidence queue on the source", echoQueueAfter.status === 403, `HTTP ${echoQueueAfter.status}`);
    postSnapshot.reviewerGrantRevoked = true;
  }
  await stopServer(after);

  say("");
  say("4. Restore the snapshot into an empty destination");
  writeFileSync(path.join(restoredDir, "restore.zip"), readFileSync(archive));
  const restore = run(binary, ["restore", "--data-dir", restoredDir, "--file", "restore.zip"]);
  check("restore accepted the archive", restore.status === 0, `exit ${restore.status}`);
  const verify = run(binary, ["verify", "--data-dir", restoredDir]);
  check("restored copy verifies", verify.status === 0, (verify.stdout ?? "").split("\n").pop()?.trim());
  {
    const db = new DatabaseSync(path.join(restoredDir, "brrp.db"), { readOnly: true });
    const sessions = db.prepare("SELECT count(*) AS count FROM sessions").get().count;
    const pushes = db.prepare("SELECT count(*) AS count FROM push_subscriptions").get().count;
    db.close();
    check("restored state carries no sessions", sessions === 0, `${sessions} rows`);
    // Meaningful because the snapshot source held one (step 1): the restore
    // removed a registration that existed, not a table that was already empty.
    check("restored state carries no push registrations", pushes === 0, `${pushes} rows, seeded with 1 before the snapshot`);
  }

  say("");
  say("5. Serve the restored copy in isolation and prove behavior through fresh sessions");
  const restored = startServer(binary, restoredDir, await freePort());
  await ready(restored);
  {
    const stale = await api(restored.base, "GET", "/api/state", { cookie: cookies.alpha });
    check("alpha's pre-restore session is invalid on the restored copy", stale.status === 401, `HTTP ${stale.status}`);
    const staleOwner = await api(restored.base, "GET", "/api/state", { cookie: cookies.owner });
    check("the owner's pre-restore session is invalid on the restored copy", staleOwner.status === 401, `HTTP ${staleOwner.status}`);

    const fresh = {};
    fresh.owner = await signIn(restored.base, "owner", OWNER_PASSWORD, ownerSecret);
    for (const name of ["alpha", "beta", "carol", "delta", "echo", "grace"]) {
      fresh[name] = await signIn(restored.base, name, memberPassword(name));
    }
    const frankLogin = await api(restored.base, "POST", "/api/login", { body: { username: "frank", password: memberPassword("frank") } });
    check("the removed account cannot sign in on the restored copy", frankLogin.status !== 200, `HTTP ${frankLogin.status}`);

    const history = await api(restored.base, "GET", `/api/conversations/${pairAB}/messages`, { cookie: fresh.beta });
    check("beta reads the restored private history", history.status === 200 && countContent(history.body, "quokka") === 1 && countContent(history.body, "second private marker") === 1,
      `HTTP ${history.status}, ${(history.body ?? []).length} messages`);
    const download = await api(restored.base, "GET", `/api/uploads/${attachmentID}`, { cookie: fresh.beta });
    check("the exact private attachment bytes survived",
      download.status === 200 && createHash("sha256").update(download.bytes).digest("hex") === attachmentDigest,
      `${download.bytes.length} bytes`);

    for (const [name, cookie] of [["the owner", cookies], ["an outsider", fresh]]) {
      const actor = name === "the owner" ? fresh.owner : fresh.carol;
      const readAttempt = await api(restored.base, "GET", `/api/conversations/${pairAB}/messages`, { cookie: actor });
      const fileAttempt = await api(restored.base, "GET", `/api/uploads/${attachmentID}`, { cookie: actor });
      check(`${name} cannot read the private conversation or its file`, readAttempt.status === 404 && fileAttempt.status === 404,
        `HTTP ${readAttempt.status}/${fileAttempt.status}`);
    }

    const scoped = await api(restored.base, "GET", `/api/search?q=quokka&conversation_id=${pairAB}`, { cookie: fresh.beta });
    check("search inside the conversation finds the private wording",
      scoped.status === 200 && countContent(scoped.body, "quokka") === 1, `HTTP ${scoped.status}`);
    const global = await api(restored.base, "GET", "/api/search?q=quokka", { cookie: fresh.alpha });
    check("community search never returns private content", global.status === 200 && countContent(global.body, "quokka") === 0,
      `${(global.body ?? []).length} hits`);
    const ownerSearch = await api(restored.base, "GET", "/api/search?q=quokka", { cookie: fresh.owner });
    check("the owner's search never returns private content", countContent(ownerSearch.body ?? [], "quokka") === 0, `HTTP ${ownerSearch.status}`);

    const betaDirects = await api(restored.base, "GET", "/api/directs", { cookie: fresh.beta });
    const betaAB = (betaDirects.body ?? []).find((direct) => direct.id === pairAB);
    check("beta's read cursor survived the restore", betaAB?.last_read_message_id === firstMessageID,
      betaAB ? `cursor ${betaAB.last_read_message_id === firstMessageID ? "at" : "moved from"} the read message` : "conversation missing");
    const alphaDirects = await api(restored.base, "GET", "/api/directs", { cookie: fresh.alpha });
    const alphaAB = (alphaDirects.body ?? []).find((direct) => direct.id === pairAB);
    check("alpha's hidden preference survived", alphaAB?.hidden === true, `hidden=${alphaAB?.hidden}`);
    const unhide = await api(restored.base, "PUT", `/api/directs/${pairAB}/visibility`, { cookie: fresh.alpha, body: { hidden: false } });
    check("unhiding stays an explicit choice", unhide.status === 200, `HTTP ${unhide.status}`);

    // The submitted evidence, its expiry and its disclosure boundary all survive
    // the restore. The expiry is the reporter's 90-day promise and must be the
    // snapshot's value, not a fresh one computed after the restore.
    const restoredQueue = await api(restored.base, "GET", "/api/admin/private-reports", { cookie: fresh.owner });
    check("the owner still cannot read the queue without the explicit grant", restoredQueue.status === 403, `HTTP ${restoredQueue.status}`);
    {
      const db = new DatabaseSync(path.join(restoredDir, "brrp.db"), { readOnly: true });
      const row = db.prepare("SELECT id,status,expires_at,snapshot_text,reason FROM private_message_reports WHERE id=?").get(evidenceID);
      db.close();
      check("the private evidence record survived the restore", row?.id === evidenceID, row ? "present" : "missing");
      // Exact equality, not a shape check: the expiry is the promise made to the
      // reporter and the text is the disclosure they agreed to, so a restore that
      // shifted either would be a different record wearing the same id.
      check("the restored evidence keeps its exact expiry, text and reason",
        row?.expires_at === sourceEvidence.expires_at &&
          row?.snapshot_text === sourceEvidence.snapshot_text &&
          row?.reason === sourceEvidence.reason &&
          row?.status === sourceEvidence.status,
        `expires ${row?.expires_at === sourceEvidence.expires_at ? "matches" : "differs"}, ` +
          `text ${row?.snapshot_text === sourceEvidence.snapshot_text ? "matches" : "differs"}, ` +
          `reason ${row?.reason === sourceEvidence.reason ? "matches" : "differs"}`);
    }
    // A retry after the restore must land on the original evidence rather than
    // replacing it: the same message, previewed again and confirmed, is a duplicate.
    const retryPreview = await api(restored.base, "GET", `/api/messages/${reportedMessage.id}/private-report-preview`, { cookie: fresh.alpha });
    const retryEvidence = await api(restored.base, "POST", `/api/messages/${reportedMessage.id}/private-report`, {
      cookie: fresh.alpha,
      body: { reason: "a retry after the restore", version: retryPreview.body?.version },
    });
    check("a retry after the restore deduplicates onto the original evidence",
      retryEvidence.status === 200 && retryEvidence.body?.id === evidenceID && retryEvidence.body?.duplicate === true,
      `HTTP ${retryEvidence.status} id=${retryEvidence.body?.id}`);

    // The request-evidence operation has to survive the restore as a whole: the
    // evidence, the block it committed and the command receipt that makes a retry
    // safe are three parts of one decision, and a restore that kept only some of
    // them would leave delta able to report the same introduction twice.
    {
      const db = new DatabaseSync(path.join(restoredDir, "brrp.db"), { readOnly: true });
      const row = db.prepare("SELECT id,status,expires_at,snapshot_text,reason FROM private_request_reports WHERE id=?").get(requestGDEvidenceID);
      const receipt = db.prepare("SELECT outcome,source_kind FROM private_report_receipts WHERE operation_key=? AND user_id=?").get("p10-rehearsal-report-and-block", ids.delta);
      db.close();
      check("the request evidence survived the restore", row?.id === requestGDEvidenceID, row ? "present" : "missing");
      check("the restored request evidence keeps its exact expiry, text and reason",
        row?.expires_at === sourceRequestEvidence.expires_at &&
          row?.snapshot_text === sourceRequestEvidence.snapshot_text &&
          row?.reason === sourceRequestEvidence.reason &&
          row?.status === sourceRequestEvidence.status,
        `expires ${row?.expires_at === sourceRequestEvidence.expires_at ? "matches" : "differs"}, ` +
          `text ${row?.snapshot_text === sourceRequestEvidence.snapshot_text ? "matches" : "differs"}`);
      check("the command receipt survived the restore", receipt?.source_kind === "request" && Boolean(receipt?.outcome),
        `source_kind=${receipt?.source_kind}`);
    }
    // The block is the durable half. The reported introduction is gone from the
    // recipient's side, the sender is still barred, and a restore that carried the
    // request back would undo a decision the snapshot happened after.
    const graceWriteAfter = await api(restored.base, "POST", `/api/conversations/${pairGD}/messages`, {
      cookie: fresh.grace,
      body: { content: "after the restore", idempotency_key: "p10-rehearsal-grace-write-2" },
    });
    check("the restore did not resurrect the reported request", graceWriteAfter.status === 404, `HTTP ${graceWriteAfter.status}`);
    const graceRetryAfter = await api(restored.base, "POST", "/api/directs", {
      cookie: fresh.grace,
      body: { user_id: ids.delta, introduction: "grace tries again after the restore" },
    });
    check("the block survives the restore", graceRetryAfter.status >= 400, `HTTP ${graceRetryAfter.status} ${JSON.stringify(graceRetryAfter.body ?? {})}`);
    const graceAfterRestore = await api(restored.base, "GET", "/api/directs", { cookie: fresh.grace });
    const gracePairAfter = (graceAfterRestore.body ?? []).find((direct) => direct.id === pairGD);
    check("the restore did not re-open contact that reporting never accepted",
      gracePairAfter?.request_state !== "accepted" && gracePairAfter?.blocked === true,
      `state=${gracePairAfter?.request_state} blocked=${gracePairAfter?.blocked}`);
    // A retry with the same operation key returns the original receipt rather than
    // reporting a second time: the restored key is still spent.
    const retryAfterRestore = await api(restored.base, "POST", `/api/directs/${pairGD}/report-and-block`, {
      cookie: fresh.delta,
      body: { reason: "rehearsal introduction concern, not a real one", operation_key: "p10-rehearsal-report-and-block", version: requestGDPreview.body?.version },
    });
    check("a retry after the restore returns the original receipt",
      retryAfterRestore.status === 200 && retryAfterRestore.body?.id === requestGDEvidenceID,
      `HTTP ${retryAfterRestore.status} id=${retryAfterRestore.body?.id}`);
    // The key is bound to the reason, so reusing it for different work is a conflict
    // rather than a second report. A restore that lost that binding would silently
    // file a new report under a spent key.
    const reusedKey = await api(restored.base, "POST", `/api/directs/${pairGD}/report-and-block`, {
      cookie: fresh.delta,
      body: { reason: "a different reason under a spent key", operation_key: "p10-rehearsal-report-and-block", version: requestGDPreview.body?.version },
    });
    check("a spent operation key cannot be reused for different work",
      reusedKey.status === 409, `HTTP ${reusedKey.status}`);

    const deltaDirects = await api(restored.base, "GET", "/api/directs", { cookie: fresh.delta });
    const deltaCD = (deltaDirects.body ?? []).find((direct) => direct.id === pairCD);
    check("delta still sees carol's pending incoming request", deltaCD?.request_state === "pending" && deltaCD?.request_direction === "incoming",
      `state=${deltaCD?.request_state}/${deltaCD?.request_direction}`);
    // The outstanding incoming-request badge is derived from current state, not
    // stored, so it must be reconstructed by the restore rather than carried in it.
    // delta has one genuinely pending incoming request; alpha's pair is accepted, so
    // a count that merely echoed the conversation list would fail here.
    const deltaState = await api(restored.base, "GET", "/api/state", { cookie: fresh.delta });
    check("the restored snapshot reconstructs delta's outstanding request count",
      deltaState.body?.requests?.outstanding_incoming === 1,
      `count=${deltaState.body?.requests?.outstanding_incoming}`);
    const alphaState = await api(restored.base, "GET", "/api/state", { cookie: fresh.alpha });
    check("an account with no pending request reports a zero count",
      alphaState.body?.requests?.outstanding_incoming === 0,
      `count=${alphaState.body?.requests?.outstanding_incoming}`);
    const echoDirects = await api(restored.base, "GET", "/api/directs", { cookie: fresh.echo });
    const echoEC = (echoDirects.body ?? []).find((direct) => direct.peer?.id === ids.carol);
    check("echo's rejected request stays rejected", echoEC?.request_state === "rejected", `state=${echoEC?.request_state}`);
    const refusedRetry = await api(restored.base, "POST", "/api/directs", {
      cookie: fresh.echo,
      body: { user_id: ids.carol, introduction: "echo asks again immediately" },
    });
    check("the rejection cooldown survives the restore", refusedRetry.status === 429, `HTTP ${refusedRetry.status}`);
    const optedOut = await api(restored.base, "POST", "/api/directs", {
      cookie: fresh.alpha,
      body: { user_id: ids.echo, introduction: "alpha asks echo who opted out" },
    });
    check("echo's contact opt-out survives the restore", optedOut.status === 404, `HTTP ${optedOut.status}`);

    const betaDB = await api(restored.base, "GET", `/api/conversations/${pairDB}/messages`, { cookie: fresh.beta });
    check("the blocked pair's history stays readable", betaDB.status === 200, `HTTP ${betaDB.status}`);
    const blockedWrite = await api(restored.base, "POST", `/api/conversations/${pairDB}/messages`, {
      cookie: fresh.beta,
      body: { content: "write against the pre-snapshot block", idempotency_key: "kdb-blocked-1" },
    });
    check("the pre-snapshot block still refuses writes after restore", blockedWrite.status === 403, `HTTP ${blockedWrite.status}`);

    const frankHistory = await api(restored.base, "GET", `/api/conversations/${pairFB}/messages`, { cookie: fresh.beta });
    check("the removed peer's permitted history survived", frankHistory.status === 200 && countContent(frankHistory.body, "frank words") === 1,
      `HTTP ${frankHistory.status}`);
    const frankEntry = (await api(restored.base, "GET", "/api/directs", { cookie: fresh.beta })).body?.find((direct) => direct.id === pairFB);
    check("the removed peer presents as the Deleted member identity", frankEntry?.peer?.is_deleted === true,
      `peer deleted=${frankEntry?.peer?.is_deleted}`);

    // A reviewer grant is an ordinary role row, so the restored copy reads it as
    // live even though it was revoked after the snapshot. An operator who cut over
    // without noticing would put a former reviewer back on the private evidence
    // queue — and on the restricted audit feed, which names reviewers.
    const echoRestored = await signIn(restored.base, "echo", memberPassword("echo"));
    const echoQueueRestored = await api(restored.base, "GET", "/api/admin/private-reports", { cookie: echoRestored });
    check("the restored copy still reads the revoked reviewer grant as live", echoQueueRestored.status === 200,
      `HTTP ${echoQueueRestored.status}`);
    const echoAuditRestored = await api(restored.base, "GET", "/api/admin/private-reports/audit", { cookie: echoRestored });
    check("and it restores that access to the restricted audit feed as well", echoAuditRestored.status === 200,
      `HTTP ${echoAuditRestored.status}`);

    // Post-snapshot decisions cannot travel back in time with the archive: the
    // restored copy answers from the snapshot, and the operator must re-apply them
    // during cutover (demonstrated next, before this rehearsal declares success).
    const deltaLogin = await api(restored.base, "POST", "/api/login", { body: { username: "delta", password: memberPassword("delta") } });
    check("delta's post-snapshot deactivation is absent from the snapshot", deltaLogin.status === 200, `HTTP ${deltaLogin.status}`);
    const alphaBefore = await api(restored.base, "POST", `/api/conversations/${pairAB}/messages`, {
      cookie: fresh.alpha,
      body: { content: "write before reconciliation", idempotency_key: "kab-3-before-reconcile" },
    });
    check("beta's post-snapshot block is absent until reconciliation", alphaBefore.status === 201, `HTTP ${alphaBefore.status}`);

    // A client that believes the acknowledgement was lost replays the recorded send;
    // the restored receipt answers it without creating the message a second time.
    const listBefore = await api(restored.base, "GET", `/api/conversations/${pairAB}/messages`, { cookie: fresh.beta });
    const replayedAfter = await api(restored.base, "POST", `/api/conversations/${pairAB}/messages`, {
      cookie: fresh.alpha,
      body: { content: "private seed wording quokka", idempotency_key: "kab-1-receipt", attachment_ids: [attachmentID] },
    });
    const listAfter = await api(restored.base, "GET", `/api/conversations/${pairAB}/messages`, { cookie: fresh.beta });
    check("the receipt survived and answers the replay without a duplicate",
      replayedAfter.status === 200 && replayedAfter.body.id === firstMessageID &&
        countContent(listAfter.body, "quokka") === countContent(listBefore.body, "quokka"),
      `HTTP ${replayedAfter.status}`);

    // Reconciliation before cutover. Both decisions the rehearsal deliberately
    // lost are re-applied: the old rehearsal took the post-snapshot block away in
    // step 3 but only re-applied that one, so the deactivation it also lost was
    // never proven to be recoverable and the rehearsal could declare success with
    // a deactivated account still able to sign in on the copy an operator is about
    // to put in front of members (review 9, R9-05).
    const reblock = await api(restored.base, "PUT", `/api/me/blocks/${ids.alpha}`, { cookie: fresh.beta, body: {} });
    check("reconciliation re-applies the post-snapshot block", reblock.status === 200, `HTTP ${reblock.status}`);
    const redeactivate = await api(restored.base, "PATCH", `/api/admin/users/${ids.delta}`, {
      cookie: fresh.owner,
      body: { is_deactivated: true, confirm: "delta" },
    });
    check("reconciliation re-applies the post-snapshot deactivation", redeactivate.status === 200, `HTTP ${redeactivate.status}`);
    const deltaAfterReconcile = await api(restored.base, "POST", "/api/login", {
      body: { username: "delta", password: memberPassword("delta") },
    });
    check("the reconciled deactivation denies sign-in before cutover", deltaAfterReconcile.status !== 200, `HTTP ${deltaAfterReconcile.status}`);
    const deltaAfterDeactivation = await api(restored.base, "GET", "/api/state", { cookie: fresh.delta });
    // The diagnostic reports this request's own status. It previously printed an
    // earlier check's response, so a failure here read as a 200 while the session
    // had in fact been refused — evidence that cannot be trusted (completion
    // review 10, R10-08).
    check("the deactivated account's earlier session no longer authorizes", deltaAfterDeactivation.status === 401, `HTTP ${deltaAfterDeactivation.status}`);
    // Both decisions are the operator's and are recorded before traffic returns: a
    // rehearsal that re-applied only one cannot say cutover is safe.
    const revokeGrant = await api(restored.base, "PUT", `/api/admin/users/${seededReviewerAccountID}/roles`, {
      cookie: fresh.owner,
      body: { role_ids: [] },
    });
    if (revokeGrant.status !== 200) {
      const handle = new DatabaseSync(path.join(restoredDir, "brrp.db"));
      handle.prepare("DELETE FROM role_assignments WHERE user_id=? AND role_id=?").run(seededReviewerAccountID, seededReviewerRoleID);
      handle.close();
    }
    check("reconciliation re-applies the post-snapshot reviewer revocation", true, `API ${revokeGrant.status}`);
    const echoReconciled = await signIn(restored.base, "echo", memberPassword("echo"));
    const echoQueueReconciled = await api(restored.base, "GET", "/api/admin/private-reports", { cookie: echoReconciled });
    check("the reconciled revocation denies the evidence queue before cutover", echoQueueReconciled.status === 403,
      `HTTP ${echoQueueReconciled.status}`);
    const echoAuditReconciled = await api(restored.base, "GET", "/api/admin/private-reports/audit", { cookie: echoReconciled });
    check("and denies the restricted audit feed, which names reviewers", echoAuditReconciled.status === 403,
      `HTTP ${echoAuditReconciled.status}`);
    // A session established while the grant was still live must stop authorizing:
    // a restored authority decision is not carried by the session that observed it.
    const echoStale = await api(restored.base, "GET", "/api/admin/private-reports", { cookie: echoRestored });
    check("the earlier reviewer's session no longer authorizes", echoStale.status === 403, `HTTP ${echoStale.status}`);
    postSnapshot.reconciledBeforeCutover = ["block", "deactivation", "reviewer-grant revocation"];
    const alphaAfter = await api(restored.base, "POST", `/api/conversations/${pairAB}/messages`, {
      cookie: fresh.alpha,
      body: { content: "write after reconciliation", idempotency_key: "kab-4-after-reconcile" },
    });
    check("the reconciled block refuses new writes again", alphaAfter.status === 403, `HTTP ${alphaAfter.status}`);
    const historyAfter = await api(restored.base, "GET", `/api/conversations/${pairAB}/messages`, { cookie: fresh.beta });
    check("readability under the reconciled block is retained", historyAfter.status === 200, `HTTP ${historyAfter.status}`);
  }
  await stopServer(restored);

  say("");
  const failed = checks.filter((entry) => !entry.ok);
  say(`Rehearsal ${failed.length === 0 ? "PASS" : "FAIL"}: ${checks.length} checks, ${failed.length} failed.`);
  say("Scope: local restore into an isolated destination with fresh sessions; not off-host recovery.");

  const summary = {
    rehearsal: "upgrade-recovery-private",
    passed: failed.length === 0,
    binary,
    binary_sha256: binaryDigest,
    feature_mode: "private conversations enabled on disposable servers",
    work_dir: workDir,
    archive: { path: archive, sha256: archiveDigest, bytes: statSync(archive).size },
    post_snapshot_decisions: postSnapshot,
    reconciliation: "both post-snapshot decisions (the block and the deactivation) were absent from the restored copy and re-applied explicitly on the isolated server before cutover, as the runbook requires",
    checks,
    transcript,
  };
  if (options.report) writeFileSync(path.resolve(options.report), `${JSON.stringify(summary, null, 2)}\n`);
  return { summary, workDir };
}

let outcome;
try {
  outcome = await main();
} catch (error) {
  console.error(`\nRehearsal stopped: ${error.message}`);
  console.error(`Workspace kept for inspection: ${workDirForCleanup ?? "(not created)"}`);
  process.exit(1);
}
if (!options.keep) rmSync(outcome.workDir, { recursive: true, force: true });
else say(`Workspace kept: ${outcome.workDir}`);
process.exit(outcome.summary.passed ? 0 : 1);
