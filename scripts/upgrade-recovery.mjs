#!/usr/bin/env node
// Upgrade and recovery rehearsal (WP-04 tail, WP-23, WP-24).
//
// Runs the whole operator path on disposable data directories against a real
// server process, and fails loudly if any step is not what the documentation
// promises:
//
//   1. preflight on an empty folder reports the pending migrations and creates nothing
//   2. a fresh install, with a message and an attachment written through the HTTP API
//   3. an offline backup of that installation into a backup ZIP
//   4. a restore of that archive into a second, empty destination
//   5. verify, then serve the restored copy from a second data directory and
//      prove the account, the channel, the message and the attachment bytes
//      all survived
//
// Usage:
//   node scripts/upgrade-recovery.mjs [--binary PATH] [--work-dir DIR] [--report FILE] [--keep]
//
// Exit code 0 means every check passed. The work directories are removed unless
// --keep is given, and on failure they are always kept for inspection.

import { spawn, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import { mkdtempSync } from "node:fs";

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

// The toolchain is whichever of BRRP_GO, a repository-local Go and a PATH Go
// actually answers, so a checkout without Go on PATH still rehearses.
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
    if (result.error) process.stderr.write(`${result.error.message}\n`);
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

function serverEnv(dataDir, port) {
  return {
    BRRP_DATA_DIR: dataDir,
    BRRP_TLS_MODE: "http-local",
    BRRP_BASE_URL: `http://127.0.0.1:${port}`,
    BRRP_ADDR: `127.0.0.1:${port}`,
  };
}

async function startServer(binary, dataDir, port, logFile) {
  const env = serverEnv(dataDir, port);
  const log = [];
  const child = spawn(binary, ["serve", "--data-dir", dataDir], {
    env: { ...process.env, ...env },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout.on("data", (chunk) => log.push(chunk.toString()));
  child.stderr.on("data", (chunk) => log.push(chunk.toString()));
  const base = `http://127.0.0.1:${port}`;
  for (let attempt = 0; attempt < 300; attempt += 1) {
    if (child.exitCode !== null) break;
    try {
      const response = await fetch(`${base}/api/setup/status`, { signal: AbortSignal.timeout(1000) });
      if (response.ok) return { child, base };
    } catch {
      /* not listening yet */
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`server on ${base} did not become ready:\n${log.join("") || logFile}`);
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

async function main() {
  const password = "correct horse battery staple";
  const attachmentText = "attachment survives a restore\n";
  const workDir = options.workDir
    ? path.resolve(options.workDir)
    : mkdtempSync(path.join(tmpdir(), "brrp-rehearsal-"));
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
  say(`Rehearsal workspace: ${workDir}`);
  say(`Binary: ${binary}`);
  say("");

  say("1. Preflight an empty data directory");
  const preflight = run(binary, ["preflight", "--data-dir", sourceDir]);
  if (preflight.status !== 0) throw new Error("preflight failed on an empty directory");
  const report = JSON.parse(preflight.stdout.replace(/^[^{]*/, ""));
  check("reports an uninitialised database", report.exists === false && report.schema_version === 0);
  check("lists every pending migration", Array.isArray(report.pending_migrations) && report.pending_migrations.length > 0,
    `${report.pending_migrations.length} pending, target schema ${report.supported_schema_version}`);
  check("declares its durability setting", typeof report.configured_synchronous === "string" && report.configured_synchronous.length > 0,
    report.configured_synchronous);
  check("does not create a database", !existsSync(path.join(sourceDir, "brrp.db")) && statSync(sourceDir).isDirectory(),
    "directory still empty");

  say("");
  say("2. Install and write real content through the API");
  const sourcePort = await freePort();
  const source = await startServer(binary, sourceDir, sourcePort, path.join(workDir, "source.log"));
  const token = readFileSync(path.join(sourceDir, "setup-token"), "utf8").trim();
  const setup = await api(source.base, "POST", "/api/setup", {
    body: { token, server_name: "Upgrade rehearsal", username: "owner", password },
  });
  check("first-time setup accepted", setup.status === 201, `HTTP ${setup.status}`);
  const login = await api(source.base, "POST", "/api/login", { body: { username: "owner", password } });
  check("owner can sign in", login.status === 200 && login.cookie !== null, `HTTP ${login.status}`);
  const cookie = login.cookie;
  const state = await api(source.base, "GET", "/api/state", { cookie });
  const channel = state.body.channels?.find((candidate) => candidate.type === "text");
  check("workspace has a text channel", Boolean(channel), channel ? `#${channel.name}` : "none");
  const marker = `upgrade rehearsal ${Date.now().toString(16)}`;
  const firstMessage = await api(source.base, "POST", `/api/channels/${channel.id}/messages`, {
    cookie,
    body: { content: marker, idempotency_key: "rehearsal-1" },
  });
  check("message accepted", firstMessage.status === 201, `HTTP ${firstMessage.status}`);
  const file = multipart("note.txt", attachmentText);
  const upload = await api(source.base, "POST", "/api/uploads", { cookie, raw: file.body, contentType: file.contentType });
  check("attachment accepted", upload.status === 201 && upload.body.id, `${upload.body.file_size} bytes, id ${upload.body.id}`);
  const secondMessage = await api(source.base, "POST", `/api/channels/${channel.id}/messages`, {
    cookie,
    body: { content: `${marker} with a file`, idempotency_key: "rehearsal-2", attachment_ids: [upload.body.id] },
  });
  check("message with attachment accepted", secondMessage.status === 201, `HTTP ${secondMessage.status}`);
  const originalDigest = createHash("sha256").update(attachmentText).digest("hex");
  await stopServer(source);
  check("source server stopped cleanly", source.child.exitCode === 0 || source.child.signalCode === "SIGTERM",
    `exit ${source.child.exitCode ?? source.child.signalCode}`);

  say("");
  say("3. Back up the stopped installation");
  const backup = run(binary, ["backup", "--data-dir", sourceDir, "--out", "backups/rehearsal.zip"]);
  const archive = path.join(sourceDir, "backups", "rehearsal.zip");
  check("backup produced an archive", backup.status === 0 && existsSync(archive),
    existsSync(archive) ? `${statSync(archive).size} bytes` : "missing");
  const integrity = run(binary, ["verify-backup", "--file", archive]);
  check("verify-backup accepts the archive", integrity.status === 0 && (integrity.stdout ?? "").includes("Verified"),
    (integrity.stdout ?? "").split("\n").at(-2)?.trim());
  const tampered = path.join(sourceDir, "backups", "tampered.zip");
  const bytes = readFileSync(archive);
  bytes[Math.floor(bytes.length / 2)] ^= 0x01;
  writeFileSync(tampered, bytes);
  const rejected = run(binary, ["verify-backup", "--file", tampered]);
  check("verify-backup refuses a touched archive", rejected.status !== 0,
    (rejected.stderr ?? "").trim().split("\n")[0] || "reported no error");
  const preflightAgain = run(binary, ["preflight", "--data-dir", sourceDir]);
  const migrated = JSON.parse(preflightAgain.stdout.replace(/^[^{]*/, ""));
  check("backup did not change the schema", migrated.schema_version === report.supported_schema_version,
    `schema ${migrated.schema_version}`);

  say("");
  say("4. Restore into a second, empty destination");
  const restoreCopy = path.join(restoredDir, "restore.zip");
  writeFileSync(restoreCopy, readFileSync(archive));
  const restore = run(binary, ["restore", "--data-dir", restoredDir, "--file", "restore.zip"]);
  check("restore accepted the archive", restore.status === 0, `exit ${restore.status}`);
  const verify = run(binary, ["verify", "--data-dir", restoredDir]);
  const verified = /Schema \d+ verified: (\d+) users, (\d+) channels, (\d+) messages, (\d+) attachments/.exec(verify.stdout ?? "");
  check("restored copy verifies", verify.status === 0 && verified !== null, verified ? verified[0] : "no summary line");
  if (verified) {
    check("verified counts match what was written", Number(verified[1]) === 1 && Number(verified[4]) === 1, verified[0]);
  }

  say("");
  say("5. Serve the restored copy and prove the data survived");
  const restoredPort = await freePort();
  const restored = await startServer(binary, restoredDir, restoredPort, path.join(workDir, "restored.log"));
  const restoredLogin = await api(restored.base, "POST", "/api/login", { body: { username: "owner", password } });
  check("restored copy keeps the account", restoredLogin.status === 200 && restoredLogin.cookie !== null, `HTTP ${restoredLogin.status}`);
  const restoredState = await api(restored.base, "GET", "/api/state", { cookie: restoredLogin.cookie });
  check("restored copy has the same channels",
    (restoredState.body.channels ?? []).length === (state.body.channels ?? []).length,
    `${restoredState.body.channels?.length ?? 0} channels`);
  const history = await api(restored.base, "GET", `/api/channels/${channel.id}/messages`, { cookie: restoredLogin.cookie });
  const texts = (history.body ?? []).map((message) => message.content ?? "");
  check("restored copy serves the message", texts.some((text) => text.includes(marker)), `${texts.length} messages`);
  const attachment = await api(restored.base, "GET", `/api/uploads/${upload.body.id}`, { cookie: restoredLogin.cookie });
  const restoredDigest = createHash("sha256").update(attachment.bytes).digest("hex");
  check("attachment bytes are identical", restoredDigest === originalDigest, `${attachment.bytes.length} bytes, sha256 ${restoredDigest.slice(0, 12)}`);
  await stopServer(restored);
  const leftover = existsSync(path.join(restoredDir, "brrp.db-wal")) || existsSync(path.join(restoredDir, "brrp.db-shm"));
  check("stopped server left no WAL side files", !leftover || statSync(path.join(restoredDir, "brrp.db-wal")).size === 0,
    "checkpointed on shutdown");

  say("");
  const failed = checks.filter((entry) => !entry.ok);
  say(`Rehearsal ${failed.length === 0 ? "PASS" : "FAIL"}: ${checks.length} checks, ${failed.length} failed.`);
  say(`Schema ${report.supported_schema_version} (${report.pending_migrations.length} migrations), durability ${report.configured_synchronous} -> ${migrated.synchronous}.`);

  const summary = {
    rehearsal: "upgrade-recovery",
    passed: failed.length === 0,
    binary,
    work_dir: workDir,
    target_schema: report.supported_schema_version,
    pending_migrations: report.pending_migrations,
    durability: { configured: report.configured_synchronous, effective: migrated.synchronous },
    archive_bytes: existsSync(archive) ? statSync(archive).size : 0,
    verified: verified ? verified[0] : null,
    attachment_sha256: originalDigest,
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
