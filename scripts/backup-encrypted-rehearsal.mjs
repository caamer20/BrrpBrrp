#!/usr/bin/env node
// Encrypted off-host backup rehearsal (ENG-15).
//
// The deployment documentation tells an operator to keep an encrypted copy of the backup
// somewhere that is not the server, and it names the commands. A procedure nobody has run
// is a procedure that does not work, so this rehearsal runs exactly those steps against a
// real installation and then restores a second, empty data directory from the encrypted
// copy alone:
//
//   1. install, and write a message and an attachment through the API
//   2. back up the stopped installation, and verify the archive
//   3. copy the archive to a separate directory, encrypt it there with the documented
//      tooling, and **delete the plaintext from that directory**
//   4. decrypt it in the destination using only the documented commands, restore, verify
//   5. serve the restored copy and prove the account, the message and the attachment bytes
//      survived
//
// Which tool encrypts is whichever of the documented ones is installed — age first, then
// gpg, then openssl — and the report records which one ran, because "the procedure works
// with the tool that was on this machine" is the claim being made.
//
// Usage:
//   node scripts/backup-encrypted-rehearsal.mjs [--binary PATH] [--work-dir DIR] [--report FILE] [--keep]
//
// Exit code 0 means every check passed. The work directories are removed unless --keep is
// given, and on failure they are always kept for inspection.

import { spawnSync } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  api,
  buildBinary,
  createRehearsal,
  freePort,
  installTeardown,
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

/**
 * Chooses the encryption tool the documentation names, in its documented order.
 *
 * age is first because it is the one an operator can hand a *public* key to: the server
 * holds something that can only encrypt. gpg is next because it is what most machines
 * already have, and it is driven here through a keyring of its own so the rehearsal never
 * touches the operator's real one. openssl is last: it is everywhere, but its `enc`
 * command has no authenticated mode, so the archive's own manifest is what verifies the
 * plaintext after decryption — which the documentation says, and the rehearsal then
 * demonstrates rather than assumes.
 */
export function chooseTool() {
  const probes = [
    { name: "age", args: ["--version"] },
    { name: "gpg", args: ["--version"] },
    { name: "openssl", args: ["version"] },
  ];
  for (const probe of probes) {
    const result = spawnSync(probe.name, probe.args, { encoding: "utf8" });
    if (result.status === 0) return probe.name;
  }
  return null;
}

/**
 * Creates the key material for the chosen tool, in a directory that stands in for the
 * operator's own custody: the rehearsal never puts it anywhere the server can read.
 *
 * The key is generated rather than written by hand, because a made-up recipient is a
 * rehearsal that would fail on a machine that actually has the tool — which is the
 * opposite of what this exists to prove.
 */
export function prepareKey(tool, keyDir) {
  const identity = path.join(keyDir, "identity.txt");
  if (tool === "age") {
    const generated = spawnSync("age-keygen", ["-o", identity], {
      encoding: "utf8",
    });
    if (generated.status !== 0)
      throw new Error(`age-keygen failed: ${generated.stderr ?? ""}`);
    const publicKey = /public key: (age1[a-z0-9]+)/.exec(
      `${generated.stdout ?? ""}${generated.stderr ?? ""}`,
    );
    if (!publicKey) throw new Error("age-keygen printed no public key");
    const recipient = path.join(keyDir, "recipient.txt");
    writeFileSync(recipient, `${publicKey[1]}\n`);
    return {
      recipient,
      identity,
      env: {},
      kind: "age recipient (public key on the server, identity with the operator)",
    };
  }
  if (tool === "gpg") {
    const home = path.join(keyDir, "gnupg");
    mkdirSync(home, { recursive: true, mode: 0o700 });
    const env = { GNUPGHOME: home };
    const generated = spawnSync(
      "gpg",
      [
        "--batch",
        "--passphrase",
        "",
        "--quick-generate-key",
        "BrrpBrrp Rehearsal <rehearsal@example.invalid>",
        "default",
        "default",
        "never",
      ],
      { encoding: "utf8", env: { ...process.env, ...env } },
    );
    if (generated.status !== 0)
      throw new Error(`gpg key generation failed: ${generated.stderr ?? ""}`);
    return {
      recipient: "BrrpBrrp Rehearsal <rehearsal@example.invalid>",
      identity,
      env,
      kind: "gpg recipient (keyring with the operator)",
    };
  }
  const passphrase = path.join(keyDir, "passphrase.txt");
  writeFileSync(passphrase, `${randomBytes(32).toString("base64")}\n`);
  return {
    recipient: passphrase,
    identity: passphrase,
    env: {},
    kind: "openssl passphrase (file held with the operator)",
  };
}

/** The documented commands, per tool. Both directions, so the rehearsal and the document
 * cannot drift into two different procedures. */
export function encryptionCommands(tool, { plaintext, encrypted, key }) {
  switch (tool) {
    case "age":
      return {
        encrypt: [
          "age",
          [
            "--encrypt",
            "--recipients-file",
            key.recipient,
            "--output",
            encrypted,
            plaintext,
          ],
        ],
        decrypt: [
          "age",
          [
            "--decrypt",
            "--identity",
            key.identity,
            "--output",
            plaintext,
            encrypted,
          ],
        ],
      };
    case "gpg":
      return {
        encrypt: [
          "gpg",
          [
            "--batch",
            "--yes",
            "--trust-model",
            "always",
            "--recipient",
            key.recipient,
            "--output",
            encrypted,
            "--encrypt",
            plaintext,
          ],
        ],
        decrypt: [
          "gpg",
          ["--batch", "--yes", "--output", plaintext, "--decrypt", encrypted],
        ],
      };
    default:
      return {
        encrypt: [
          "openssl",
          [
            "enc",
            "-aes-256-cbc",
            "-pbkdf2",
            "-iter",
            "600000",
            "-salt",
            "-in",
            plaintext,
            "-out",
            encrypted,
            "-pass",
            `file:${key.recipient}`,
          ],
        ],
        decrypt: [
          "openssl",
          [
            "enc",
            "-d",
            "-aes-256-cbc",
            "-pbkdf2",
            "-iter",
            "600000",
            "-in",
            encrypted,
            "-out",
            plaintext,
            "-pass",
            `file:${key.recipient}`,
          ],
        ],
      };
  }
}

function runTool(command, args, env = {}) {
  say(
    `$ ${command} ${args.map((arg) => (arg.includes(" ") ? JSON.stringify(arg) : arg)).join(" ")}`,
  );
  const result = spawnSync(command, args, {
    encoding: "utf8",
    env: { ...process.env, ...env },
  });
  const output = `${result.stdout ?? ""}${result.stderr ?? ""}`.trim();
  if (output) say(output.replace(/^/gm, "  "));
  return result;
}

async function main() {
  const password = "correct horse battery staple";
  const attachmentText =
    "an attachment that must survive an encrypted round trip\n";
  const workDir = options.workDir
    ? path.resolve(options.workDir)
    : mkdtempSync(path.join(tmpdir(), "brrp-encrypted-"));
  workDirForCleanup = workDir;
  if (options.workDir) mkdirSync(workDir, { recursive: true });
  const sourceDir = path.join(workDir, "source");
  const restoredDir = path.join(workDir, "restored");
  // The off-host directory stands in for the separate disk or object store, and the key
  // directory stands in for the operator's own custody: the rehearsal never writes the key
  // anywhere the server can read.
  const offHostDir = path.join(workDir, "off-host");
  const keyDir = path.join(workDir, "operator-keys");
  for (const directory of [sourceDir, restoredDir, offHostDir, keyDir])
    mkdirSync(directory, { recursive: true });

  let binary = options.binary && path.resolve(options.binary);
  if (!binary || !existsSync(binary)) {
    binary = options.binary
      ? path.resolve(options.binary)
      : path.join(workDir, "brrpbrrp-rehearsal");
    buildBinary(binary);
  }
  say(`Rehearsal workspace: ${workDir}`);
  say(`Binary: ${binary}`);

  const tool = chooseTool();
  check(
    "a documented encryption tool is installed",
    tool !== null,
    tool ?? "none of age, gpg, openssl",
  );
  const key = prepareKey(tool, keyDir);
  say(`Key material: ${key.kind}`);

  say("");
  say("1. Install and write real content through the API");
  const sourcePort = await freePort();
  const source = await startServer(
    binary,
    sourceDir,
    sourcePort,
    path.join(workDir, "source.log"),
  );
  const token = readFileSync(
    path.join(sourceDir, "setup-token"),
    "utf8",
  ).trim();
  const setup = await api(source.base, "POST", "/api/setup", {
    body: {
      token,
      server_name: "Encrypted backup rehearsal",
      username: "owner",
      password,
    },
  });
  check(
    "first-time setup accepted",
    setup.status === 201,
    `HTTP ${setup.status}`,
  );
  const login = await api(source.base, "POST", "/api/login", {
    body: { username: "owner", password },
  });
  check(
    "owner can sign in",
    login.status === 200 && login.cookie !== null,
    `HTTP ${login.status}`,
  );
  const cookie = login.cookie;
  const state = await api(source.base, "GET", "/api/state", { cookie });
  const channel = state.body.channels?.find(
    (candidate) => candidate.type === "text",
  );
  check(
    "workspace has a text channel",
    Boolean(channel),
    channel ? `#${channel.name}` : "none",
  );
  const marker = `encrypted rehearsal ${Date.now().toString(16)}`;
  const message = await api(
    source.base,
    "POST",
    `/api/channels/${channel.id}/messages`,
    {
      cookie,
      body: { content: marker, idempotency_key: "encrypted-1" },
    },
  );
  check("message accepted", message.status === 201, `HTTP ${message.status}`);
  const file = multipart("note.txt", attachmentText);
  const upload = await api(source.base, "POST", "/api/uploads", {
    cookie,
    raw: file.body,
    contentType: file.contentType,
  });
  check(
    "attachment accepted",
    upload.status === 201 && upload.body.id,
    `${upload.body.file_size} bytes`,
  );
  const originalDigest = createHash("sha256")
    .update(attachmentText)
    .digest("hex");
  await stopServer(source);

  say("");
  say("2. Back up the stopped installation");
  const backup = run(binary, [
    "backup",
    "--data-dir",
    sourceDir,
    "--out",
    "backups/rehearsal.zip",
  ]);
  const archive = path.join(sourceDir, "backups", "rehearsal.zip");
  check(
    "backup produced an archive",
    backup.status === 0 && existsSync(archive),
    existsSync(archive) ? `${statSync(archive).size} bytes` : "missing",
  );
  const integrity = run(binary, ["verify-backup", "--file", archive]);
  check(
    "verify-backup accepts the archive",
    integrity.status === 0,
    (integrity.stdout ?? "").split("\n").at(-2)?.trim(),
  );

  say("");
  say(
    "3. Copy it off the host, encrypt it there, and remove the plaintext copy",
  );
  const plaintextCopy = path.join(offHostDir, "rehearsal.zip");
  const encryptedCopy = path.join(
    offHostDir,
    tool === "openssl" ? "rehearsal.zip.enc" : `rehearsal.zip.${tool}`,
  );
  writeFileSync(plaintextCopy, readFileSync(archive));
  const commands = encryptionCommands(tool, {
    plaintext: plaintextCopy,
    encrypted: encryptedCopy,
    key,
  });
  const encrypted = runTool(commands.encrypt[0], commands.encrypt[1], key.env);
  check(
    "the documented encrypt command succeeds",
    encrypted.status === 0,
    `exit ${encrypted.status}`,
  );
  check(
    "an encrypted copy exists",
    existsSync(encryptedCopy),
    existsSync(encryptedCopy)
      ? `${statSync(encryptedCopy).size} bytes`
      : "missing",
  );
  // The plaintext leaves the off-host directory: what is left there is the encrypted copy,
  // which is the whole point of the step. A rehearsal that restored from a leftover
  // plaintext archive would pass while the procedure failed.
  rmSync(plaintextCopy);
  check(
    "no plaintext copy remains off the host",
    !existsSync(plaintextCopy),
    `${offHostDir} holds only the encrypted archive`,
  );

  say("");
  say(
    "4. Decrypt and restore into a second, empty destination, using only those commands",
  );
  const decrypted = path.join(restoredDir, "restore.zip");
  // `--file` is resolved inside the data directory (app.Contained), so the archive is
  // decrypted straight into the destination: the plaintext never exists anywhere else.
  const decryptionCommands = encryptionCommands(tool, {
    plaintext: decrypted,
    encrypted: encryptedCopy,
    key,
  });
  const decryption = runTool(
    decryptionCommands.decrypt[0],
    decryptionCommands.decrypt[1],
    key.env,
  );
  check(
    "the documented decrypt command succeeds",
    decryption.status === 0,
    `exit ${decryption.status}`,
  );
  const restoredDigestOfArchive = createHash("sha256")
    .update(readFileSync(decrypted))
    .digest("hex");
  const originalDigestOfArchive = createHash("sha256")
    .update(readFileSync(archive))
    .digest("hex");
  check(
    "the decrypted archive is byte-identical to the original",
    restoredDigestOfArchive === originalDigestOfArchive,
    `sha256 ${restoredDigestOfArchive.slice(0, 12)}`,
  );
  const restore = run(binary, [
    "restore",
    "--data-dir",
    restoredDir,
    "--file",
    "restore.zip",
  ]);
  check(
    "restore accepted the decrypted archive",
    restore.status === 0,
    `exit ${restore.status}`,
  );
  const verify = run(binary, ["verify", "--data-dir", restoredDir]);
  const verified =
    /Schema \d+ verified: (\d+) users, (\d+) channels, (\d+) messages, (\d+) attachments/.exec(
      verify.stdout ?? "",
    );
  check(
    "restored copy verifies",
    verify.status === 0 && verified !== null,
    verified ? verified[0] : "no summary line",
  );

  say("");
  say("5. Serve the restored copy and prove the data survived");
  const restoredPort = await freePort();
  const restored = await startServer(
    binary,
    restoredDir,
    restoredPort,
    path.join(workDir, "restored.log"),
  );
  const restoredLogin = await api(restored.base, "POST", "/api/login", {
    body: { username: "owner", password },
  });
  check(
    "restored copy keeps the account",
    restoredLogin.status === 200,
    `HTTP ${restoredLogin.status}`,
  );
  const history = await api(
    restored.base,
    "GET",
    `/api/channels/${channel.id}/messages`,
    { cookie: restoredLogin.cookie },
  );
  check(
    "restored copy serves the message",
    (history.body ?? []).some((entry) =>
      (entry.content ?? "").includes(marker),
    ),
    `${(history.body ?? []).length} messages`,
  );
  const attachment = await api(
    restored.base,
    "GET",
    `/api/uploads/${upload.body.id}`,
    { cookie: restoredLogin.cookie },
  );
  const digest = createHash("sha256").update(attachment.bytes).digest("hex");
  check(
    "attachment bytes are identical",
    digest === originalDigest,
    `${attachment.bytes.length} bytes, sha256 ${digest.slice(0, 12)}`,
  );
  await stopServer(restored);

  say("");
  say(
    `Rehearsal ${checks.every((entry) => entry.ok) ? "PASS" : "FAIL"}: ${checks.length} checks.`,
  );
  say(`Encryption tool exercised: ${tool} (${key.kind}).`);
  const summary = {
    rehearsal: "backup-encrypted",
    passed: checks.every((entry) => entry.ok),
    binary,
    work_dir: workDir,
    tool,
    key_kind: key.kind,
    archive_bytes: existsSync(archive) ? statSync(archive).size : 0,
    encrypted_bytes: existsSync(encryptedCopy)
      ? statSync(encryptedCopy).size
      : 0,
    attachment_sha256: originalDigest,
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
  process.exit(1);
}
if (!options.keep) rmSync(outcome.workDir, { recursive: true, force: true });
else say(`Workspace kept: ${outcome.workDir}`);
process.exit(outcome.summary.passed ? 0 : 1);
