#!/usr/bin/env node
// The full-disk rehearsal (T-DATA-01's owed half).
//
// The disk guard's *logic* is proven with an injected free-space function, because a
// refusal proven by consuming a real disk is a test nobody can run. What that leaves
// unproven is the thing an operator actually experiences: a data directory on a volume
// that really has no room, an upload really refused, the server really still serving, and
// the space freed by a deletion really available again.
//
// This rehearse creates a small filesystem of its own (a sparse disk image on macOS, a
// tmpfs-free loop-mounted image on Linux) and runs a real server with its data directory
// inside it, so "the disk is full" is a fact about the filesystem rather than a number
// passed into a function.
//
// Usage:
//   node scripts/full-disk-rehearsal.mjs [--binary PATH] [--work-dir DIR] [--report FILE] [--keep]
//
// Exit code 0 means every check passed. On a platform where a small filesystem cannot be
// created without root, the rehearsal says so and exits 0 with a recorded skip rather than
// pretending to have run.

import { randomUUID } from "node:crypto";
import { spawnSync } from "node:child_process";
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
  multipart,
  run,
  startServer,
  stopServer,
} from "./rehearsal-kit.mjs";
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
 * A small filesystem, mounted at `mountPoint`.
 *
 * macOS: a sparse disk image attached with `hdiutil`, which needs no root. Linux: an image
 * formatted with `mkfs.ext4` and mounted through a loop device, which needs root — so the
 * rehearsal asks for a *user* mount there and reports a skip when it cannot have one.
 * Anywhere else it reports a skip. The size is deliberately far below the product's own
 * minimum headroom, so the guard engages as soon as the server looks at it.
 */
export function createSmallFilesystem(workDir, sizeMb = 48, name = "small") {
  const image = path.join(workDir, `${name}.dmg`);
  const mountPoint = path.join(workDir, name);
  mkdirSync(mountPoint, { recursive: true });
  const platform = process.platform;
  if (platform === "darwin") {
    const created = spawnSync(
      "hdiutil",
      [
        "create",
        "-size",
        `${sizeMb}m`,
        "-fs",
        "HFS+",
        "-volname",
        "brrp-small",
        image,
      ],
      { encoding: "utf8" },
    );
    if (created.status !== 0)
      return {
        ok: false,
        reason: `hdiutil create failed: ${created.stderr.trim()}`,
      };
    const attached = spawnSync(
      "hdiutil",
      ["attach", image, "-mountpoint", mountPoint, "-nobrowse"],
      { encoding: "utf8" },
    );
    if (attached.status !== 0)
      return {
        ok: false,
        reason: `hdiutil attach failed: ${attached.stderr.trim()}`,
      };
    return {
      ok: true,
      mountPoint,
      detach: () =>
        spawnSync("hdiutil", ["detach", mountPoint, "-force"], {
          encoding: "utf8",
        }),
    };
  }
  if (platform === "linux") {
    const create = spawnSync("truncate", ["-s", `${sizeMb}M`, image], {
      encoding: "utf8",
    });
    if (create.status !== 0)
      return { ok: false, reason: "truncate is unavailable" };
    const format = spawnSync("mkfs.ext4", ["-q", "-F", image], {
      encoding: "utf8",
    });
    if (format.status !== 0)
      return { ok: false, reason: `mkfs.ext4 failed: ${format.stderr.trim()}` };
    const mount = spawnSync("mount", ["-o", "loop", image, mountPoint], {
      encoding: "utf8",
    });
    if (mount.status !== 0)
      return {
        ok: false,
        reason: `mount -o loop needs privileges: ${mount.stderr.trim()}`,
      };
    return {
      ok: true,
      mountPoint,
      detach: () => spawnSync("umount", [mountPoint], { encoding: "utf8" }),
    };
  }
  return { ok: false, reason: `no small-filesystem recipe for ${platform}` };
}

/** How much room the filesystem has, in bytes, as the operating system sees it. */
function freeBytes(dir) {
  const result = spawnSync("df", ["-k", dir], { encoding: "utf8" });
  const line = (result.stdout ?? "").trim().split("\n").at(-1) ?? "";
  const columns = line.split(/\s+/);
  const available = Number(columns[3]);
  return Number.isFinite(available) ? available * 1024 : -1;
}

async function main() {
  const password = "correct horse battery staple";
  const workDir = options.workDir
    ? path.resolve(options.workDir)
    : mkdtempSync(path.join(tmpdir(), "brrp-full-disk-"));
  workDirForCleanup = workDir;
  if (options.workDir) mkdirSync(workDir, { recursive: true });

  let binary = options.binary && path.resolve(options.binary);
  if (!binary || !existsSync(binary)) {
    binary = options.binary
      ? path.resolve(options.binary)
      : path.join(workDir, "brrpbrrp-rehearsal");
    buildBinary(binary);
  }

  const filesystem = createSmallFilesystem(workDir, 48, "small");
  if (!filesystem.ok) {
    say(`Full-disk rehearsal skipped: ${filesystem.reason}`);
    const summary = {
      rehearsal: "full-disk",
      passed: true,
      skipped: filesystem.reason,
      checks: [],
      transcript,
    };
    if (options.report)
      writeFileSync(
        path.resolve(options.report),
        `${JSON.stringify(summary, null, 2)}\n`,
      );
    return { summary, workDir };
  }

  const dataDir = path.join(filesystem.mountPoint, "data");
  mkdirSync(dataDir, { recursive: true });
  say(`Rehearsal workspace: ${workDir}`);
  say(
    `Data directory: ${dataDir} on a ${Math.round(freeBytes(dataDir) / 1048576)} MB filesystem`,
  );

  const port = await freePort();
  const server = await startServer(
    binary,
    dataDir,
    port,
    path.join(workDir, "server.log"),
  );
  const token = readFileSync(path.join(dataDir, "setup-token"), "utf8").trim();
  const setup = await api(server.base, "POST", "/api/setup", {
    body: {
      token,
      server_name: "Full disk rehearsal",
      username: "owner",
      password,
    },
  });
  check(
    "setup succeeds on the small filesystem",
    setup.status === 201,
    `HTTP ${setup.status}`,
  );
  const login = await api(server.base, "POST", "/api/login", {
    body: { username: "owner", password },
  });
  check("sign-in succeeds", login.status === 200, `HTTP ${login.status}`);
  const cookie = login.cookie;
  const state = await api(server.base, "GET", "/api/state", { cookie });
  const channel = state.body.channels?.find(
    (candidate) => candidate.type === "text",
  );
  check(
    "the workspace is usable",
    Boolean(channel),
    channel ? `#${channel.name}` : "no channel",
  );

  // An upload that must be refused: the volume has far less room than the product's own
  // disk-guard headroom, so the refusal is the guard working rather than the filesystem
  // failing a write half way.
  const before = freeBytes(dataDir);
  const big = multipart("big.bin", Buffer.alloc(6 * 1024 * 1024, 7));
  const refused = await api(server.base, "POST", "/api/uploads", {
    cookie,
    raw: big.body,
    contentType: big.contentType,
  });
  check(
    "an upload that would fill the disk is refused with a reason",
    refused.status >= 400 &&
      typeof refused.body?.error === "string" &&
      refused.body.error.length > 10,
    `HTTP ${refused.status} ${String(refused.body?.error).slice(0, 80)}`,
  );
  const after = freeBytes(dataDir);
  check(
    "the refused upload did not consume the volume",
    after >= before - 1048576,
    `${Math.round(before / 1024)} KB free before, ${Math.round(after / 1024)} KB after`,
  );

  // The server is still serving: a guard that protects the disk by breaking the product is
  // not a guard, and this is the assertion that says so.
  const message = await api(
    server.base,
    "POST",
    `/api/channels/${channel.id}/messages`,
    {
      cookie,
      body: {
        content: "the server still works on a full disk",
        idempotency_key: randomUUID(),
      },
    },
  );
  check(
    "a message is still accepted",
    message.status === 201,
    `HTTP ${message.status}`,
  );
  const history = await api(
    server.base,
    "GET",
    `/api/channels/${channel.id}/messages`,
    { cookie },
  );
  check(
    "the message is readable back",
    (history.body ?? []).some((entry) =>
      (entry.content ?? "").includes("still works on a full disk"),
    ),
    `${(history.body ?? []).length} messages`,
  );
  const health = await api(server.base, "GET", "/api/admin/health", { cookie });
  check(
    "the health endpoint still answers",
    health.status === 200,
    `HTTP ${health.status}`,
  );

  // The refusal names the headroom it keeps rather than failing vaguely: that sentence is
  // the difference between an operator who can act and one who cannot.
  check(
    "the refusal names the headroom the server keeps",
    /keeps \d+ bytes free/.test(String(refused.body?.error ?? "")),
    String(refused.body?.error ?? "").slice(0, 120),
  );

  await stopServer(server);
  check(
    "the server stops cleanly on a full disk",
    server.child.exitCode === 0 || server.child.signalCode === "SIGTERM",
    `exit ${server.child.exitCode ?? server.child.signalCode}`,
  );
  filesystem.detach();

  // The control, on a second volume of its own: one *larger than the headroom the server
  // reserves* accepts a small upload. Without it this rehearsal would only show that
  // uploads are refused on a tiny volume — which a product that refused every upload would
  // also show — and the assertion that matters is that the guard refuses for the reason it
  // states.
  const roomy = createSmallFilesystem(workDir, 640, "roomy");
  if (!roomy.ok) {
    say(
      `  note: the control volume could not be created (${roomy.reason}), so the acceptance half is not rehearsed here`,
    );
  } else {
    const roomyDir = path.join(roomy.mountPoint, "data");
    mkdirSync(roomyDir, { recursive: true });
    const roomyServer = await startServer(
      binary,
      roomyDir,
      await freePort(),
      path.join(workDir, "roomy.log"),
    );
    const roomyToken = readFileSync(
      path.join(roomyDir, "setup-token"),
      "utf8",
    ).trim();
    const roomySetup = await api(roomyServer.base, "POST", "/api/setup", {
      body: {
        token: roomyToken,
        server_name: "Room to spare",
        username: "owner",
        password,
      },
    });
    check(
      "the control volume installs",
      roomySetup.status === 201,
      `HTTP ${roomySetup.status}`,
    );
    const roomyLogin = await api(roomyServer.base, "POST", "/api/login", {
      body: { username: "owner", password },
    });
    const small = multipart("small.txt", "a few bytes that fit");
    const accepted = await api(roomyServer.base, "POST", "/api/uploads", {
      cookie: roomyLogin.cookie,
      raw: small.body,
      contentType: small.contentType,
    });
    check(
      "an upload is accepted where there is room for it",
      accepted.status === 201,
      `HTTP ${accepted.status} with ${Math.round(freeBytes(roomyDir) / 1048576)} MB free`,
    );
    await stopServer(roomyServer);
    roomy.detach();
  }

  say("");
  say(
    `Rehearsal ${checks.every((entry) => entry.ok) ? "PASS" : "FAIL"}: ${checks.length} checks.`,
  );
  const summary = {
    rehearsal: "full-disk",
    passed: checks.every((entry) => entry.ok),
    binary,
    work_dir: workDir,
    filesystem_mb: Math.round(
      statSync(path.join(workDir, "small.dmg")).size / 1048576,
    ),
    control_mb: existsSync(path.join(workDir, "roomy.dmg"))
      ? Math.round(statSync(path.join(workDir, "roomy.dmg")).size / 1048576)
      : 0,
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
