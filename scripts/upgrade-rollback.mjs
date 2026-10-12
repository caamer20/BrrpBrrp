// Upgrade and rollback compatibility rehearsal (WP-04 tail, review R2-09).
//
// scripts/upgrade-recovery.mjs rehearses the operator path with one executable. This
// one rehearses the path that crosses a version boundary, and it uses a real archived
// prior release rather than a synthetic schema fixture, because the claim under test is
// exactly the cross-version one: an installation written by the older executable must
// upgrade under the candidate, and the rollback folder must come from the older
// executable's own archive rather than from the candidate's.
//
// Handout sequence:
//   1. install with the archived prior release and write a representative dataset
//      (owner, member, categories/channels, messages, a reply, a pin, an invitation,
//      an uploaded file and its bytes)
//   2. snapshot that installation with the OLD executable — this archive is the only
//      supported rollback source — and read it with the candidate's reader
//   3. snapshot the same installation with the candidate, prove the snapshot did not
//      migrate the database, and check whether the prior reader accepts its format:
//      a newer format must be refused, while a compatible format remains readable
//   4. upgrade with the candidate, then verify and serve the upgraded installation and
//      prove every dataset row survived, including the exact attachment bytes
//   5. record what changed after the snapshot, so the data-loss boundary of a rollback
//      is described rather than hidden
//   6. restore the OLD archive into a clean folder with the OLD executable and prove
//      the snapshot's data is intact, the later writes are absent, and the candidate
//      is not asked to repair a prior-version folder
//
// What this cannot establish, and does not claim: Windows or container lifecycles, an
// physical process interruption during an upgrade, off-host recovery, capacity or
// measured RPO/RTO. Step 7 injects unfinished postconditions deterministically.
//
// Usage:
//   node scripts/upgrade-rollback.mjs [--old-release ZIP] [--old-binary PATH]
//                                     [--binary PATH] [--work-dir DIR] [--report FILE]
//                                     [--keep]
//
// Without --old-release the newest archived release for this platform under
// dist/releases is used. Exit code 0 means every check passed. The work directory is
// removed unless --keep is given, and on failure it is always kept for inspection.

import { spawn } from "node:child_process";
import { createHash, createHmac } from "node:crypto";
import {
  copyFileSync,
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  api,
  buildBinary,
  serverEnv,
  trackChild,
  untrackChild,
  createRehearsal,
  freePort,
  killLiveChildren,
  multipart,
  run,
  startServer,
  stopServer,
} from "./rehearsal-kit.mjs";
const { checks, transcript, say, check } = createRehearsal();
// The rollback rehearsal keeps a coverage table of its own beside the check list: it runs
// against two executables and has to record which of its assertions the older one could
// even satisfy.
const coverage = [];

const options = parseOptions(process.argv.slice(2));
let workDirForCleanup;

function parseOptions(argv) {
  const parsed = { keep: false };
  for (let index = 0; index < argv.length; index++) {
    const flag = argv[index];
    const value = argv[index + 1];
    if (flag === "--keep") parsed.keep = true;
    else if (
      flag === "--old-release" ||
      flag === "--old-binary" ||
      flag === "--binary" ||
      flag === "--work-dir" ||
      flag === "--report"
    ) {
      if (!value) throw new Error(`${flag} needs a value`);
      parsed[
        /--(.*)/
          .exec(flag)[1]
          .replace(/-(.)/g, (_, letter) => letter.toUpperCase())
      ] = value;
      index++;
    } else throw new Error(`unknown option ${flag}`);
  }
  return parsed;
}

// coverage records a fact about the baseline's API rather than an assertion about the
// candidate: a prior release that does not offer an endpoint cannot be asked to hold
// that kind of data, and saying so keeps the dataset honest.
function record(name, supported, detail = "") {
  coverage.push({ name, supported: Boolean(supported), detail });
  say(
    `  ${supported ? "ok   " : "skip "} ${name}${detail ? ` — ${detail}` : ""}`,
  );
}

// bytes returns the response body as it is on the wire, so an attachment can be
// compared by digest instead of through a JSON decoder that would not survive it.
async function bytes(base, route, cookie) {
  const response = await fetch(`${base}${route}`, {
    headers: { origin: base, cookie },
  });
  return {
    status: response.status,
    buffer: Buffer.from(await response.arrayBuffer()),
  };
}

// totpCode is the same RFC 6238 code an authenticator app shows for a setup key, which
// is how the rehearsal proves an enrolled factor (and its secret) survived a migration.
const BASE32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

function decodeBase32(secret) {
  let bits = 0;
  let value = 0;
  const decoded = [];
  for (const character of secret.replace(/=+$/, "").toUpperCase()) {
    const index = BASE32.indexOf(character);
    if (index === -1) throw new Error(`setup key contains ${character}`);
    value = (value << 5) | index;
    bits += 5;
    if (bits >= 8) {
      decoded.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(decoded);
}

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

function currentWindow() {
  return Math.floor(Date.now() / 1000 / 30);
}

async function waitForWindow(target) {
  while (currentWindow() < target)
    await new Promise((done) => setTimeout(done, 500));
}

// loginWithFactor signs in the way an account with a second factor does: the password
// first, then the same request with a code. An authenticator app only ever shows the
// code for the current thirty-second step, and the server accepts a step once, so a
// step that was just consumed has to age out rather than being skipped: a code from a
// later step is not something the server accepts, and producing one would be a
// rehearsal that passes for a reason the product does not offer.
async function loginWithFactor(base, username, password, secret) {
  const firstFactor = await api(base, "POST", "/api/login", {
    body: { username, password },
  });
  if (
    firstFactor.cookie ||
    firstFactor.body?.second_factor_required !== true ||
    !secret
  )
    return firstFactor;
  let last = firstFactor;
  for (let attempt = 0; attempt < 3; attempt++) {
    await waitForWindow(currentWindow() + attempt);
    last = await api(base, "POST", "/api/login", {
      body: { username, password, code: totpCode(secret) },
    });
    if (last.cookie) return last;
  }
  return last;
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

function fileDigest(file) {
  return sha256(readFileSync(file));
}

function hostPlatform() {
  const os = { darwin: "darwin", linux: "linux", win32: "windows" }[
    process.platform
  ];
  const arch = { x64: "amd64", arm64: "arm64" }[process.arch];
  if (!os || !arch)
    throw new Error(
      `this rehearsal does not know ${process.platform}/${process.arch}`,
    );
  return {
    os,
    arch,
    executable: os === "windows" ? "brrpbrrp.exe" : "brrpbrrp",
  };
}

function compareVersions(left, right) {
  const parts = (value) =>
    value
      .split(/[.-]/)
      .map((piece) => (/^\d+$/.test(piece) ? Number(piece) : piece));
  const a = parts(left);
  const b = parts(right);
  for (let index = 0; index < Math.max(a.length, b.length); index++) {
    const one = a[index];
    const other = b[index];
    if (one === other || (one === undefined && other === undefined)) continue;
    if (one === undefined) return -1;
    if (other === undefined) return 1;
    if (typeof one === "number" && typeof other === "number")
      return one < other ? -1 : 1;
    return String(one) < String(other) ? -1 : 1;
  }
  return 0;
}

// discoverOldRelease finds the newest archived release for this platform that is not
// the candidate itself: the rollback source has to be a different build, and the
// newest one below the candidate is the combination an operator would actually hold.
function discoverOldRelease(directory, candidateVersion, platform) {
  if (!existsSync(directory)) return null;
  const pattern = new RegExp(
    `^brrpbrrp-(.+)-${platform.os}-${platform.arch}\\.zip$`,
  );
  const found = [];
  for (const name of readdirSync(directory)) {
    const match = pattern.exec(name);
    if (!match) continue;
    if (match[1] === candidateVersion) continue;
    if (compareVersions(match[1], candidateVersion) > 0) continue;
    found.push({ path: path.join(directory, name), version: match[1] });
  }
  if (!found.length) return null;
  found.sort((left, right) => compareVersions(left.version, right.version));
  return found.at(-1);
}

function goToolchain() {
  const candidates = [
    process.env.BRRP_GO,
    path.resolve(".tools/go/bin/go"),
    "go",
  ].filter(Boolean);
  for (const candidate of candidates) {
    if (candidate !== "go" && !existsSync(candidate)) continue;
    return candidate;
  }
  throw new Error("no Go toolchain found; set BRRP_GO to a go executable");
}

async function buildCandidate(destination) {
  const go = goToolchain();
  const built = await run(go, [
    "build",
    "-trimpath",
    "-o",
    destination,
    "./cmd/brrpbrrp",
  ]);
  if (built.status !== 0)
    throw new Error(`building the candidate failed:\n${built.stderr}`);
}

async function versionOf(binary) {
  const result = await run(binary, ["version"]);
  return (result.stdout ?? "").trim().replace(/^BrrpBrrp\s*/, "") || "unknown";
}

async function main() {
  const platform = hostPlatform();
  const password = "correct horse battery staple";
  const attachmentText = "rollback rehearsal attachment\n";
  const workDir = options.workDir
    ? path.resolve(options.workDir)
    : mkdtempSync(path.join(tmpdir(), "brrp-rollback-"));
  workDirForCleanup = workDir;
  mkdirSync(workDir, { recursive: true });
  say(`Rehearsal workspace: ${workDir}`);
  say(`Platform: ${platform.os}/${platform.arch}`);

  let candidateBinary = options.binary
    ? path.resolve(options.binary)
    : path.join(workDir, "brrpbrrp-candidate");
  if (!options.binary || !existsSync(candidateBinary)) {
    say(`Building the candidate: ${candidateBinary}`);
    await buildCandidate(candidateBinary);
  }
  const candidateVersion = await versionOf(candidateBinary);

  let oldBinary = options.oldBinary ? path.resolve(options.oldBinary) : "";
  let oldSource = "an executable supplied with --old-binary";
  let oldSourceDigest =
    oldBinary && existsSync(oldBinary) ? fileDigest(oldBinary) : "";
  if (!oldBinary) {
    const releases = path.resolve("dist/releases");
    const archived = options.oldRelease
      ? { path: path.resolve(options.oldRelease), version: "supplied" }
      : discoverOldRelease(releases, candidateVersion, platform);
    if (!archived) {
      throw new Error(
        `no archived release for ${platform.os}/${platform.arch} below ${candidateVersion} in dist/releases; ` +
          "pass --old-release or --old-binary so the rehearsal tests a real prior executable",
      );
    }
    if (!existsSync(archived.path))
      throw new Error(`old release not found: ${archived.path}`);
    oldSource = archived.path;
    const extraction = path.join(workDir, "old-release");
    mkdirSync(extraction, { recursive: true });
    const unzip = await run("unzip", [
      "-q",
      "-o",
      archived.path,
      "-d",
      extraction,
    ]);
    if (unzip.status !== 0) {
      throw new Error(
        `could not extract ${archived.path} (${unzip.stderr?.trim() || "unzip unavailable"}); ` +
          "extract it yourself and pass --old-binary",
      );
    }
    oldBinary = path.join(extraction, platform.executable);
    if (!existsSync(oldBinary))
      throw new Error(`the archive has no ${platform.executable}`);
    oldSourceDigest = fileDigest(archived.path);
  }
  const oldVersion = await versionOf(oldBinary);
  say(`Old executable: ${oldBinary} (${oldVersion}) from ${oldSource}`);
  say(`Candidate:      ${candidateBinary} (${candidateVersion})`);
  say("");

  const oldBinaryDigest = fileDigest(oldBinary);
  const candidateDigest = fileDigest(candidateBinary);
  const rollbackDir = path.join(workDir, "rollback-source");
  const upgradeDir = path.join(workDir, "upgraded");
  const refusedDir = path.join(workDir, "candidate-archive-target");
  const restoredDir = path.join(workDir, "restored-old");
  for (const directory of [rollbackDir, upgradeDir, refusedDir, restoredDir])
    mkdirSync(directory, { recursive: true });

  say(
    "1. Install with the archived prior release and write a representative dataset",
  );
  const oldPort = await freePort();
  let server = await startServer(
    oldBinary,
    rollbackDir,
    oldPort,
    path.join(workDir, "old.log"),
  );
  const setupToken = readFileSync(
    path.join(rollbackDir, "setup-token"),
    "utf8",
  ).trim();
  const setup = await api(server.base, "POST", "/api/setup", {
    body: {
      token: setupToken,
      server_name: "Rollback rehearsal",
      username: "owner",
      password,
    },
  });
  check(
    "the prior release accepted first-time setup",
    setup.status === 201,
    `HTTP ${setup.status}`,
  );
  const ownerSession = await api(server.base, "POST", "/api/login", {
    body: { username: "owner", password },
  });
  check(
    "the prior release signs the owner in",
    ownerSession.status === 200 && ownerSession.cookie !== null,
    `HTTP ${ownerSession.status}`,
  );
  const owner = ownerSession.cookie;
  const state = await api(server.base, "GET", "/api/state", { cookie: owner });
  const channel = (state.body.channels ?? []).find(
    (entry) => entry.type === "text",
  );
  check(
    "the prior release has a text channel",
    Boolean(channel),
    channel ? `#${channel.name}` : "none",
  );

  const marker = `rollback rehearsal ${Date.now().toString(16)}`;
  const memberCreated = await api(server.base, "POST", "/api/admin/users", {
    cookie: owner,
    body: {
      username: "member",
      password: "member passphrase here",
      display_name: "Member",
    },
  });
  check(
    "the prior release created a member account",
    memberCreated.status === 201,
    `HTTP ${memberCreated.status}`,
  );
  const memberID = memberCreated.body?.id ?? "";

  const upload = multipart("note.txt", attachmentText);
  const uploaded = await api(server.base, "POST", "/api/uploads", {
    cookie: owner,
    raw: upload.body,
    contentType: upload.contentType,
  });
  check(
    "the prior release accepted an uploaded file",
    uploaded.status === 201 && uploaded.body?.id,
    `HTTP ${uploaded.status}`,
  );
  const uploadID = uploaded.body?.id ?? "";

  const first = await api(
    server.base,
    "POST",
    `/api/channels/${channel.id}/messages`,
    {
      cookie: owner,
      body: { content: marker, idempotency_key: "rollback-rehearsal-1" },
    },
  );
  check(
    "the prior release accepted a message",
    first.status === 201,
    `HTTP ${first.status}`,
  );
  const second = await api(
    server.base,
    "POST",
    `/api/channels/${channel.id}/messages`,
    {
      cookie: owner,
      body: {
        content: `${marker} with a file`,
        idempotency_key: "rollback-rehearsal-2",
        attachment_ids: [uploadID],
      },
    },
  );
  check(
    "the prior release accepted a message with an attachment",
    second.status === 201,
    `HTTP ${second.status}`,
  );

  const reply = await api(
    server.base,
    "POST",
    `/api/channels/${channel.id}/messages`,
    {
      cookie: owner,
      body: {
        content: `${marker} reply`,
        idempotency_key: "rollback-rehearsal-3",
        reply_to_id: first.body?.id ?? "",
      },
    },
  );
  record(
    "a reply is part of the dataset",
    reply.status === 201,
    `HTTP ${reply.status}`,
  );

  const pinned = await api(
    server.base,
    "PUT",
    `/api/messages/${first.body?.id ?? ""}/pin`,
    { cookie: owner, body: { pinned: true } },
  );
  record(
    "a pinned message is part of the dataset",
    pinned.status === 200,
    `HTTP ${pinned.status}`,
  );

  // A single-use invitation carrying a role is authority promised for later, so it is
  // part of what a migration must not quietly drop. The prior release decides which
  // shape it accepts.
  let invitation = await api(server.base, "POST", "/api/admin/invitations", {
    cookie: owner,
    body: { hours: 24 },
  });
  if (invitation.status !== 201) {
    invitation = await api(server.base, "POST", "/api/admin/invitations", {
      cookie: owner,
      body: { hours: 24, role_ids: [] },
    });
  }
  record(
    "an invitation is part of the dataset",
    invitation.status === 201,
    `HTTP ${invitation.status}`,
  );

  const roles = await api(server.base, "GET", "/api/roles", { cookie: owner });
  record(
    "the prior release has a role API",
    roles.status === 200,
    `HTTP ${roles.status} (roles arrive with schema 3)`,
  );

  // An enrolled second factor is a credential, and credentials have to survive a
  // migration: nothing may silently re-enroll an account or accept a factor whose
  // secret no longer verifies. The prior release allows an administrator to enroll
  // one, which is the account this rehearsal signs in as.
  let ownerFactor = "";
  const setupFactor = await api(server.base, "POST", "/api/me/mfa/setup", {
    cookie: owner,
    body: { password },
  });
  const factorSecret = setupFactor.body?.secret ?? "";
  if (setupFactor.status === 200 && factorSecret) {
    const enable = await api(server.base, "POST", "/api/me/mfa/enable", {
      cookie: owner,
      body: { code: totpCode(factorSecret) },
    });
    check(
      "the prior release enrolled a second factor",
      enable.status === 200,
      `HTTP ${enable.status}`,
    );
    if (enable.status === 200) ownerFactor = factorSecret;
  } else {
    record(
      "an enrolled second factor is part of the dataset",
      false,
      `the baseline refused enrollment with HTTP ${setupFactor.status}`,
    );
  }

  const originalDigest = sha256(attachmentText);
  // The dataset is read back rather than assumed: a write that answered 2xx but did
  // not persist would otherwise be recorded as evidence it survived a migration.
  const readBack = await api(
    server.base,
    "GET",
    `/api/channels/${channel.id}/messages`,
    { cookie: owner },
  );
  const readBackPins = await api(
    server.base,
    "GET",
    `/api/channels/${channel.id}/pins`,
    { cookie: owner },
  );
  const readBackInvitations = await api(
    server.base,
    "GET",
    "/api/admin/invitations",
    { cookie: owner },
  );
  const readBackUpload = await bytes(
    server.base,
    `/api/uploads/${uploadID}`,
    owner,
  );
  const observedMessages = Array.isArray(readBack.body) ? readBack.body : [];
  const dataset = {
    users: 2,
    channels: (state.body.channels ?? []).length,
    messages: observedMessages.length,
    replies: observedMessages.filter((message) => message.reply_count > 0)
      .length,
    pins: Array.isArray(readBackPins.body) ? readBackPins.body.length : 0,
    invitations: Array.isArray(readBackInvitations.body)
      ? readBackInvitations.body.length
      : 0,
    attachments: observedMessages.filter(
      (message) => (message.attachments ?? []).length > 0,
    ).length,
    second_factor: ownerFactor !== "",
    attachment_sha256: originalDigest,
  };
  check(
    "the prior release's dataset is what the rehearsal wrote",
    dataset.messages === 3 &&
      dataset.replies === 1 &&
      dataset.pins === 1 &&
      dataset.attachments === 1,
    JSON.stringify(dataset),
  );
  check(
    "the attachment the dataset references is readable",
    readBackUpload.status === 200 &&
      sha256(readBackUpload.buffer) === originalDigest,
    `HTTP ${readBackUpload.status}, ${readBackUpload.buffer.length} bytes`,
  );
  say(`  dataset: ${JSON.stringify(dataset)}`);
  await stopServer(server);

  say("");
  say(
    "2. Snapshot the installation with the prior release (the rollback source)",
  );
  const preflightBefore = await run(candidateBinary, [
    "preflight",
    "--data-dir",
    rollbackDir,
  ]);
  const beforeReport = JSON.parse(
    (preflightBefore.stdout ?? "{}").replace(/^[^{]*/, ""),
  );
  check(
    "the baseline schema is known before either snapshot",
    preflightBefore.status === 0 &&
      Number.isInteger(beforeReport.schema_version) &&
      beforeReport.schema_version > 0,
    `schema ${beforeReport.schema_version} of ${beforeReport.supported_schema_version}`,
  );
  const oldBackup = await run(oldBinary, [
    "backup",
    "--data-dir",
    rollbackDir,
    "--out",
    "backups/rollback.zip",
  ]);
  const rollbackArchive = path.join(rollbackDir, "backups/rollback.zip");
  check(
    "the prior release wrote an archive",
    oldBackup.status === 0 && existsSync(rollbackArchive),
    existsSync(rollbackArchive)
      ? `${statSync(rollbackArchive).size} bytes`
      : (oldBackup.stderr ?? "").trim(),
  );
  const rollbackDigest = existsSync(rollbackArchive)
    ? fileDigest(rollbackArchive)
    : "";
  const readRollback = await run(candidateBinary, [
    "verify-backup",
    "--file",
    rollbackArchive,
  ]);
  const rollbackFormat = /format (\d+)/.exec(readRollback.stdout ?? "");
  const rollbackSchema = /schema (\d+)/.exec(readRollback.stdout ?? "");
  check(
    "the candidate reads the old archive",
    readRollback.status === 0,
    `${(readRollback.stdout ?? "").split("\n")[1]?.trim() ?? (readRollback.stderr ?? "").trim()}`,
  );
  check(
    "the old archive reports its backup format",
    rollbackFormat && Number(rollbackFormat[1]) > 0,
    rollbackFormat ? `format ${rollbackFormat[1]}` : "format not reported",
  );
  check(
    "the old archive's schema metadata agrees with the baseline",
    rollbackSchema
      ? Number(rollbackSchema[1]) === beforeReport.schema_version
      : rollbackFormat?.[1] === "1",
    rollbackSchema
      ? `schema ${rollbackSchema[1]}`
      : `format 1 omits schema metadata; preflight measured schema ${beforeReport.schema_version}`,
  );

  say("");
  say(
    "3. Check the candidate snapshot's schema and prior-reader compatibility",
  );
  const candidateBackup = await run(candidateBinary, [
    "backup",
    "--data-dir",
    rollbackDir,
    "--out",
    "backups/candidate.zip",
  ]);
  const candidateArchive = path.join(rollbackDir, "backups/candidate.zip");
  check(
    "the candidate wrote an archive",
    candidateBackup.status === 0 && existsSync(candidateArchive),
    existsSync(candidateArchive)
      ? `${statSync(candidateArchive).size} bytes`
      : (candidateBackup.stderr ?? "").trim(),
  );
  const readCandidateArchive = await run(candidateBinary, [
    "verify-backup",
    "--file",
    candidateArchive,
  ]);
  const candidateHeader =
    (readCandidateArchive.stdout ?? "").split("\n")[1]?.trim() ?? "";
  const candidateFormat = /format (\d+)/.exec(candidateHeader);
  const candidateSnapshotSchema = /schema (\d+)/.exec(candidateHeader);
  check(
    "the candidate verifies its own archive and reports its format",
    readCandidateArchive.status === 0 &&
      candidateFormat &&
      Number(candidateFormat[1]) > 0,
    candidateHeader || (readCandidateArchive.stderr ?? "").trim(),
  );
  check(
    "snapshotting an un-migrated installation did not migrate it",
    candidateSnapshotSchema !== null &&
      Number(candidateSnapshotSchema[1]) === beforeReport.schema_version,
    candidateSnapshotSchema
      ? `recorded schema ${candidateSnapshotSchema[1]}`
      : "schema not reported",
  );
  const preflightAfterSnapshots = await run(candidateBinary, [
    "preflight",
    "--data-dir",
    rollbackDir,
  ]);
  const afterSnapshots = JSON.parse(
    (preflightAfterSnapshots.stdout ?? "{}").replace(/^[^{]*/, ""),
  );
  check(
    "the installation is still at the prior schema",
    preflightAfterSnapshots.status === 0 &&
      afterSnapshots.schema_version === beforeReport.schema_version &&
      afterSnapshots.schema_version < afterSnapshots.supported_schema_version,
    `schema ${afterSnapshots.schema_version} of ${afterSnapshots.supported_schema_version}, ${afterSnapshots.pending_migrations?.length ?? 0} pending`,
  );
  copyFileSync(candidateArchive, path.join(refusedDir, "candidate.zip"));
  const oldRestoreOfCandidate = await run(oldBinary, [
    "restore",
    "--data-dir",
    refusedDir,
    "--file",
    "candidate.zip",
  ]);
  const newerCandidateFormat =
    candidateFormat &&
    rollbackFormat &&
    Number(candidateFormat[1]) > Number(rollbackFormat[1]);
  if (newerCandidateFormat) {
    check(
      "the prior release refuses the candidate's newer backup format",
      oldRestoreOfCandidate.status !== 0 &&
        /unsupported backup format/i.test(
          (oldRestoreOfCandidate.stderr ?? "") +
            (oldRestoreOfCandidate.stdout ?? ""),
        ),
      (oldRestoreOfCandidate.stderr ?? "").trim().split("\n")[0] ||
        "no refusal reported",
    );
  } else {
    check(
      "the prior release restores the candidate's compatible pre-upgrade archive",
      candidateFormat &&
        rollbackFormat &&
        Number(candidateFormat[1]) === Number(rollbackFormat[1]) &&
        oldRestoreOfCandidate.status === 0,
      `exit ${oldRestoreOfCandidate.status}, formats ${rollbackFormat?.[1]} -> ${candidateFormat?.[1]}`,
    );
    const compatiblePreflight = await run(candidateBinary, [
      "preflight",
      "--data-dir",
      refusedDir,
    ]);
    const compatibleReport = JSON.parse(
      (compatiblePreflight.stdout ?? "{}").replace(/^[^{]*/, ""),
    );
    check(
      "the compatible candidate archive restores the unchanged baseline schema",
      compatiblePreflight.status === 0 &&
        compatibleReport.schema_version === beforeReport.schema_version,
      `schema ${compatibleReport.schema_version}`,
    );
    const compatibleVerify = await run(oldBinary, [
      "verify",
      "--data-dir",
      refusedDir,
    ]);
    check(
      "the prior release verifies the compatible candidate archive's restored data",
      compatibleVerify.status === 0,
      (compatibleVerify.stdout || compatibleVerify.stderr)
        .trim()
        .split("\n")
        .at(-1),
    );
  }

  say("");
  say("4. Upgrade with the candidate and prove the dataset survived");
  const upgradedPort = await freePort();
  server = await startServer(
    candidateBinary,
    rollbackDir,
    upgradedPort,
    path.join(workDir, "upgraded.log"),
  );
  await stopServer(server);
  const verify = await run(candidateBinary, [
    "verify",
    "--data-dir",
    rollbackDir,
  ]);
  const verified =
    /Schema (\d+) verified: (\d+) users, (\d+) channels, (\d+) messages, (\d+) attachments/.exec(
      verify.stdout ?? "",
    );
  check(
    "the upgraded installation verifies",
    verify.status === 0 && verified !== null,
    verified ? verified[0] : (verify.stderr ?? "").trim(),
  );
  if (verified) {
    check(
      "the upgrade reached the candidate's schema",
      Number(verified[1]) === beforeReport.supported_schema_version,
      `schema ${verified[1]}`,
    );
    check(
      "the upgrade preserved every account",
      Number(verified[2]) === dataset.users,
      `${verified[2]} users`,
    );
    check(
      "the upgrade preserved every message, including the reply",
      Number(verified[4]) >= dataset.messages,
      `${verified[4]} messages`,
    );
    check(
      "the upgrade preserved the attachment",
      Number(verified[5]) === dataset.attachments,
      `${verified[5]} attachments`,
    );
  }
  copyFileSync(rollbackArchive, path.join(upgradeDir, "unused.zip"));
  say("");

  const upgradedPort2 = await freePort();
  server = await startServer(
    candidateBinary,
    rollbackDir,
    upgradedPort2,
    path.join(workDir, "served.log"),
  );
  const upgradedLogin = await loginWithFactor(
    server.base,
    "owner",
    password,
    ownerFactor,
  );
  check(
    "the upgraded installation signs the owner in",
    upgradedLogin.status === 200 && upgradedLogin.cookie !== null,
    `HTTP ${upgradedLogin.status}`,
  );
  const upgradedCookie = upgradedLogin.cookie;
  const history = await api(
    server.base,
    "GET",
    `/api/channels/${channel.id}/messages`,
    { cookie: upgradedCookie },
  );
  const contents = (history.body ?? []).map((message) => message.content ?? "");
  check(
    "the upgraded installation serves the message",
    contents.some((text) => text.includes(marker)),
    `${contents.length} messages`,
  );
  const parent = (history.body ?? []).find(
    (message) => message.id === first.body?.id,
  );
  check(
    "the reply still belongs to its parent",
    !dataset.replies || (parent?.reply_count ?? 0) >= 1,
    `reply_count ${parent?.reply_count ?? 0}`,
  );
  check(
    "the pin survived",
    !dataset.pins || (parent?.pinned ?? false) === true,
    `pinned ${parent?.pinned}`,
  );
  const pins = await api(
    server.base,
    "GET",
    `/api/channels/${channel.id}/pins`,
    { cookie: upgradedCookie },
  );
  check(
    "the pin is listed",
    !dataset.pins || (Array.isArray(pins.body) && pins.body.length >= 1),
    `${pins.body?.length ?? 0} pins`,
  );
  const search = await api(
    server.base,
    "GET",
    `/api/search?q=${encodeURIComponent(marker)}`,
    { cookie: upgradedCookie },
  );
  check(
    "search finds the pre-upgrade message",
    Array.isArray(search.body) && search.body.length >= 1,
    `${search.body?.length ?? 0} hits`,
  );
  const download = await bytes(
    server.base,
    `/api/uploads/${uploadID}`,
    upgradedCookie,
  );
  const downloadDigest = sha256(download.buffer);
  check(
    "attachment bytes are identical after the upgrade",
    download.status === 200 && downloadDigest === originalDigest,
    `HTTP ${download.status}, sha256 ${downloadDigest.slice(0, 12)}`,
  );
  if (dataset.second_factor) {
    const firstFactor = await api(server.base, "POST", "/api/login", {
      body: { username: "owner", password },
    });
    check(
      "the upgraded installation still answers with a second-factor step",
      firstFactor.body?.second_factor_required === true,
      `HTTP ${firstFactor.status}`,
    );
    const withCode = await loginWithFactor(
      server.base,
      "owner",
      password,
      ownerFactor,
    );
    check(
      "the enrolled factor still verifies after the upgrade",
      withCode.status === 200 && withCode.cookie !== null,
      `HTTP ${withCode.status} ${withCode.bytes.toString().slice(0, 120)}`,
    );
  } else {
    say(
      "  note: the baseline did not enroll a factor, so the credential half of the upgrade is not rehearsed here",
    );
  }

  say("");
  say("5. Record what changed after the snapshot (the data-loss boundary)");
  const divergence = [];
  const lateMessage = `${marker} after the snapshot`;
  const late = await api(
    server.base,
    "POST",
    `/api/channels/${channel.id}/messages`,
    {
      cookie: upgradedCookie,
      body: { content: lateMessage, idempotency_key: "rollback-rehearsal-4" },
    },
  );
  if (late.status === 201) divergence.push("a message sent after the snapshot");
  const banned = await api(
    server.base,
    "PATCH",
    `/api/admin/users/${memberID}`,
    {
      cookie: upgradedCookie,
      body: {
        confirm: "member",
        is_banned: true,
        ban_reason: "after the snapshot",
      },
    },
  );
  if (banned.status === 200)
    divergence.push("a ban applied after the snapshot");
  check(
    "the rehearsal recorded post-snapshot changes",
    divergence.length > 0,
    divergence.join(", "),
  );
  await stopServer(server);
  const oldOnMigrated = await startServerFailure(
    oldBinary,
    rollbackDir,
    path.join(workDir, "old-on-migrated.log"),
  );
  check(
    "the prior release refuses to serve the migrated installation",
    oldOnMigrated.failed && /schema/i.test(oldOnMigrated.output),
    oldOnMigrated.output.trim().split("\n").at(-1)?.trim() ||
      "no refusal reported",
  );

  say("");
  say("6. Roll back with the prior release and prove the data-loss boundary");
  copyFileSync(rollbackArchive, path.join(restoredDir, "rollback.zip"));
  const oldRestore = await run(oldBinary, [
    "restore",
    "--data-dir",
    restoredDir,
    "--file",
    "rollback.zip",
  ]);
  check(
    "the prior release restored its own archive",
    oldRestore.status === 0,
    `exit ${oldRestore.status}`,
  );
  const restoredPreflight = await run(candidateBinary, [
    "preflight",
    "--data-dir",
    restoredDir,
  ]);
  const restoredReport = JSON.parse(
    (restoredPreflight.stdout ?? "{}").replace(/^[^{]*/, ""),
  );
  check(
    "the rollback folder is at the prior schema",
    restoredReport.schema_version === beforeReport.schema_version,
    `schema ${restoredReport.schema_version} of ${restoredReport.supported_schema_version}`,
  );
  const restoredPort = await freePort();
  server = await startServer(
    oldBinary,
    restoredDir,
    restoredPort,
    path.join(workDir, "restored.log"),
  );
  const restoredLogin = await loginWithFactor(
    server.base,
    "owner",
    password,
    ownerFactor,
  );
  check(
    "the rollback folder signs the owner in",
    restoredLogin.status === 200 && restoredLogin.cookie !== null,
    `HTTP ${restoredLogin.status}`,
  );
  const restoredCookie = restoredLogin.cookie;
  const restoredHistory = await api(
    server.base,
    "GET",
    `/api/channels/${channel.id}/messages`,
    { cookie: restoredCookie },
  );
  const restoredTexts = (restoredHistory.body ?? []).map(
    (message) => message.content ?? "",
  );
  check(
    "the rollback folder serves the snapshot's messages",
    restoredTexts.some((text) => text.includes(marker)),
    `${restoredTexts.length} messages`,
  );
  check(
    "the rollback folder does not contain the later message",
    divergence.includes("a message sent after the snapshot")
      ? !restoredTexts.includes(lateMessage)
      : true,
    "the rollback boundary is the snapshot, not the current installation",
  );
  const restoredAttachment = await bytes(
    server.base,
    `/api/uploads/${uploadID}`,
    restoredCookie,
  );
  check(
    "the rollback folder serves the same attachment bytes",
    restoredAttachment.status === 200 &&
      sha256(restoredAttachment.buffer) === originalDigest,
    `HTTP ${restoredAttachment.status}, sha256 ${sha256(restoredAttachment.buffer).slice(0, 12)}`,
  );
  const restoredUsers = await api(server.base, "GET", "/api/admin/users", {
    cookie: restoredCookie,
  });
  const restoredMember = (restoredUsers.body ?? []).find(
    (entry) => entry.username === "member",
  );
  check(
    "the rollback folder keeps the member account",
    Boolean(restoredMember),
    `HTTP ${restoredUsers.status}`,
  );
  check(
    "the rollback folder does not contain the later ban",
    divergence.includes("a ban applied after the snapshot")
      ? restoredMember?.is_banned === false
      : true,
    `is_banned ${restoredMember?.is_banned}`,
  );
  const restoredPins = await api(
    server.base,
    "GET",
    `/api/channels/${channel.id}/pins`,
    { cookie: restoredCookie },
  );
  check(
    "the rollback folder keeps the pin",
    (restoredPins.body?.length ?? 0) === dataset.pins,
    `${restoredPins.body?.length ?? 0} pins`,
  );
  const restoredInvitations = await api(
    server.base,
    "GET",
    "/api/admin/invitations",
    { cookie: restoredCookie },
  );
  check(
    "the rollback folder keeps the invitation",
    (restoredInvitations.body?.length ?? 0) === dataset.invitations,
    `${restoredInvitations.body?.length ?? 0} invitations`,
  );
  if (dataset.second_factor) {
    const restoredFactorStep = await api(server.base, "POST", "/api/login", {
      body: { username: "owner", password },
    });
    check(
      "the rollback folder still requires the enrolled factor",
      restoredFactorStep.body?.second_factor_required === true,
      `HTTP ${restoredFactorStep.status}`,
    );
    check(
      "the rollback folder accepts the factor the baseline enrolled",
      restoredLogin.cookie !== null,
      "the sign-in above used the baseline's own code",
    );
  }
  await stopServer(server);

  say("");
  say("7. An upgrade interrupted before its postconditions");
  // The state a crash between the migration and its postconditions leaves behind:
  // the schema is applied and the durable marker says the search-index rebuild and
  // verification are still owed. It is written here rather than raced, because a
  // rehearsal that depends on killing a process mid-migration passes or fails on
  // timing and would be evidence of the machine's speed rather than of the product.
  const sqlite = await run("sqlite3", ["-version"]);
  if (sqlite.status !== 0) {
    record(
      "an interrupted upgrade is repaired and verified",
      false,
      "sqlite3 is not available on this host",
    );
  } else {
    const interruptedDir = path.join(workDir, "interrupted");
    cpSync(rollbackDir, interruptedDir, { recursive: true });
    // The marker value is the JSON the server itself writes when it commits a
    // migration and still owes the postconditions.
    const upgradeState = `{"from":${beforeReport.schema_version},"to":${beforeReport.supported_schema_version}}`;
    const injected = await run("sqlite3", [
      path.join(interruptedDir, "brrp.db"),
      `INSERT INTO settings(key,value,updated_at) VALUES('upgrade-state','${upgradeState}','${new Date().toISOString()}')` +
        " ON CONFLICT(key) DO UPDATE SET value=excluded.value;",
    ]);
    check(
      "the interrupted upgrade state was injected",
      injected.status === 0,
      (injected.stderr ?? "").trim(),
    );
    const refused = await run(candidateBinary, [
      "verify",
      "--data-dir",
      interruptedDir,
    ]);
    const refusal = `${refused.stdout ?? ""}${refused.stderr ?? ""}`;
    check(
      "verification refuses an unfinished upgrade instead of reporting it as done",
      refused.status !== 0 && /has not finished/.test(refusal),
      refusal.trim().split("\n").at(-1)?.trim() || "no refusal reported",
    );
    // The search index is derived data, so an interruption can leave it stale as well as
    // owed. It is emptied here so the repair is observable: a postcondition that ran but
    // did nothing would otherwise look the same as one that worked. Counting rows is not
    // enough to see this — an external-content index has no rows of its own, so the
    // observation is a match, which is also what a search asks.
    const databaseFile = path.join(interruptedDir, "brrp.db");
    await run("sqlite3", [databaseFile, "DELETE FROM message_fts;"]);
    const damaged = await run("sqlite3", [
      databaseFile,
      `SELECT count(*) FROM message_fts WHERE message_fts MATCH 'rehearsal';`,
    ]);
    check(
      "the injected index damage is real",
      (damaged.stdout ?? "").trim() === "0",
      `${(damaged.stdout ?? "").trim()} matches for the indexed word`,
    );
    const repairPort = await freePort();
    server = await startServer(
      candidateBinary,
      interruptedDir,
      repairPort,
      path.join(workDir, "repair.log"),
    );
    const repairedLogin = await loginWithFactor(
      server.base,
      "owner",
      password,
      ownerFactor,
    );
    const repairedSearch = await api(
      server.base,
      "GET",
      `/api/search?q=${encodeURIComponent(marker)}`,
      {
        cookie: repairedLogin.cookie,
      },
    );
    check(
      "the repaired installation searches the pre-upgrade content again",
      Array.isArray(repairedSearch.body) && repairedSearch.body.length >= 1,
      `sign-in HTTP ${repairedLogin.status}, search HTTP ${repairedSearch.status}, ${Array.isArray(repairedSearch.body) ? repairedSearch.body.length : repairedSearch.bytes.toString().slice(0, 80)}`,
    );
    await stopServer(server);
    const rebuilt = await run("sqlite3", [
      databaseFile,
      `SELECT count(*) FROM message_fts WHERE message_fts MATCH 'rehearsal';`,
    ]);
    check(
      "the postconditions rebuilt the search index from the messages that survived",
      Number((rebuilt.stdout ?? "").trim()) >= dataset.messages,
      `${(rebuilt.stdout ?? "").trim()} matches after the repair`,
    );
    const repaired = await run(candidateBinary, [
      "verify",
      "--data-dir",
      interruptedDir,
    ]);
    check(
      "starting the server completes and verifies the upgrade",
      repaired.status === 0,
      (repaired.stdout ?? repaired.stderr ?? "")
        .trim()
        .split("\n")
        .at(-1)
        ?.trim(),
    );
    const remaining = await run("sqlite3", [
      databaseFile,
      "SELECT count(*) FROM settings WHERE key='upgrade-state';",
    ]);
    check(
      "the marker is cleared once the postconditions pass",
      (remaining.stdout ?? "").trim() === "0",
      `rows ${(remaining.stdout ?? "").trim()}`,
    );
    record(
      "an interrupted upgrade is repaired and verified",
      true,
      "marker state injected between the migration and its postconditions",
    );
  }

  say("");
  const failed = checks.filter((entry) => !entry.ok);
  say(
    `Rehearsal ${failed.length === 0 ? "PASS" : "FAIL"}: ${checks.length} checks, ${failed.length} failed.`,
  );
  say(
    `Compatibility: ${oldVersion} (schema ${beforeReport.schema_version}, backup format ${rollbackFormat?.[1] ?? "?"}) -> ` +
      `${candidateVersion} (schema ${beforeReport.supported_schema_version}, backup format ${candidateFormat?.[1] ?? "?"}).`,
  );
  say(
    `Rollback source: ${platform.os}/${platform.arch} archive of ${oldVersion}, sha256 ${rollbackDigest.slice(0, 16)}…`,
  );

  const summary = {
    rehearsal: "upgrade-rollback",
    passed: failed.length === 0,
    platform,
    baseline: {
      version: oldVersion,
      source: oldSource,
      source_sha256: oldSourceDigest,
      binary_sha256: oldBinaryDigest,
      schema: beforeReport.schema_version,
      backup_format: rollbackFormat ? Number(rollbackFormat[1]) : null,
    },
    candidate: {
      version: candidateVersion,
      binary_sha256: candidateDigest,
      schema: beforeReport.supported_schema_version,
      backup_format: candidateFormat ? Number(candidateFormat[1]) : null,
      pending_migrations: beforeReport.pending_migrations ?? [],
    },
    archives: {
      rollback: {
        bytes: existsSync(rollbackArchive) ? statSync(rollbackArchive).size : 0,
        sha256: rollbackDigest,
        format: rollbackFormat ? Number(rollbackFormat[1]) : null,
        schema: rollbackSchema ? Number(rollbackSchema[1]) : null,
        written_by: "the prior release, from the un-migrated installation",
      },
      candidate: {
        bytes: existsSync(candidateArchive)
          ? statSync(candidateArchive).size
          : 0,
        sha256: existsSync(candidateArchive)
          ? fileDigest(candidateArchive)
          : "",
        format: candidateFormat ? Number(candidateFormat[1]) : null,
        schema: candidateSnapshotSchema
          ? Number(candidateSnapshotSchema[1])
          : null,
        prior_reader_compatible:
          !newerCandidateFormat && oldRestoreOfCandidate.status === 0,
        written_by: "the candidate, from the same un-migrated installation",
      },
    },
    dataset,
    boundary: {
      snapshot:
        "the prior release's archive is the only supported rollback source",
      divergence,
      consequence:
        "a rollback returns the installation to the snapshot: the recorded changes above are lost, and " +
        "an in-place downgrade is refused rather than attempted",
    },
    coverage,
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

// startServerFailure starts a server that is expected to refuse, and reports what it
// said instead of throwing: a prior release meeting a newer schema is a required
// behaviour, and it must print a reason rather than start anyway.
async function startServerFailure(binary, dataDir, logFile) {
  const port = await freePort();
  const child = spawn(binary, ["serve"], {
    env: serverEnv(dataDir, port),
    stdio: ["ignore", "pipe", "pipe"],
  });
  trackChild(child);
  let output = "";
  child.stdout.on("data", (chunk) => (output += chunk));
  child.stderr.on("data", (chunk) => (output += chunk));
  const base = `http://127.0.0.1:${port}`;
  let served = false;
  for (let attempt = 0; attempt < 40; attempt++) {
    if (child.exitCode !== null || child.signalCode !== null) break;
    try {
      const response = await fetch(`${base}/api/setup/status`);
      if (response.ok) {
        served = true;
        break;
      }
    } catch {
      // still starting
    }
    await new Promise((done) => setTimeout(done, 200));
  }
  if (child.exitCode === null && child.signalCode === null)
    child.kill("SIGTERM");
  await Promise.race([
    new Promise((done) => {
      if (child.exitCode !== null || child.signalCode !== null) done();
      else child.once("exit", done);
    }),
    new Promise((done) => setTimeout(done, 5000)),
  ]);
  writeFileSync(logFile, output);
  untrackChild(child);
  return { failed: !served && child.exitCode !== 0, served, output };
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
if (!options.keep && outcome.summary.passed)
  rmSync(outcome.workDir, { recursive: true, force: true });
else say(`Workspace kept: ${outcome.workDir}`);
process.exitCode = outcome.summary.passed ? 0 : 1;
