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
import { createHash, createHmac } from "node:crypto";
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
const members = ["alpha", "beta", "carol", "delta", "echo", "frank"];
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
    check("restored state carries no push registrations", pushes === 0, `${pushes} rows`);
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
    for (const name of ["alpha", "beta", "carol", "delta", "echo"]) {
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

    const deltaDirects = await api(restored.base, "GET", "/api/directs", { cookie: fresh.delta });
    const deltaCD = (deltaDirects.body ?? []).find((direct) => direct.id === pairCD);
    check("delta still sees carol's pending incoming request", deltaCD?.request_state === "pending" && deltaCD?.request_direction === "incoming",
      `state=${deltaCD?.request_state}/${deltaCD?.request_direction}`);
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

    // Reconciliation before cutover: the operator re-applies the recorded decision.
    const reblock = await api(restored.base, "PUT", `/api/me/blocks/${ids.alpha}`, { cookie: fresh.beta, body: {} });
    check("reconciliation re-applies the post-snapshot block", reblock.status === 200, `HTTP ${reblock.status}`);
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
    reconciliation: "the post-snapshot block was absent from the restored copy and re-applied explicitly before cutover, as the runbook requires",
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
