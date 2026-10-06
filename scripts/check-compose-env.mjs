#!/usr/bin/env node
// Static audit: every BRRP_* variable the server reads must be reachable from Compose,
// or be listed below as deliberately not passed.
//
// The defect class this exists for: `compose.yaml` passes a fixed list of variables into
// the container. A setting an operator writes into `.env.docker` for a key that list
// omits does nothing at all — no error, no log line, just a feature that silently stays
// off on one of the two required deployments. That is invisible to every other check,
// because both files are individually correct.
//
// It reads the keys from the source (non-test Go files under `internal/` and `cmd/`) and
// compares them with the `environment:` blocks of the Compose files. It deliberately does
// not check the reverse direction: `BRRP_BIND_IP`, `BRRP_WEB_PORT`, `BRRP_MEDIA_BIND_IP`,
// `BRRP_TURN_PORT` and `BRRP_BACKUP_HOST_DIR` are Compose's own variables, read by
// `docker compose` rather than by the server, and flagging them would make the audit noise.
//
// Usage: node scripts/check-compose-env.mjs [root]

import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { composeKeys, serverKeys } from "./env-keys.mjs";

// Keys the server reads that Compose deliberately does not pass, each with the reason it
// is absent. Adding a key here is a statement that its value belongs to the container
// layout rather than to the operator's `.env.docker`; a key that is passed anyway is
// fine, so this list can only ever be too long, never mask a missing pass-through.
const deliberatelyFixed = new Map([
  [
    "BRRP_DB_FILE",
    "the container keeps the database under the data volume (BRRP_DATA_DIR), not at a host path",
  ],
  [
    "BRRP_UPLOAD_DIR",
    "the container keeps uploads under the data volume (BRRP_DATA_DIR)",
  ],
  [
    "BRRP_SETUP_TOKEN_FILE",
    "the container keeps the setup token under the data volume (BRRP_DATA_DIR)",
  ],
  [
    "BRRP_LOG_FILE",
    "the container logs to its logging driver, not to a file inside the volume",
  ],
]);

const composeFiles = ["compose.yaml", "compose.turn.yaml", "compose.backup.yaml"];

function audit(root) {
  const passed = composeKeys(root, composeFiles);
  const read = serverKeys(root);
  const problems = [];
  for (const key of [...read].sort()) {
    if (passed.has(key) || deliberatelyFixed.has(key)) continue;
    problems.push(
      `the server reads ${key}, but no Compose file passes it and it is not listed as deliberately fixed`,
    );
  }
  return { problems, passed, read };
}

// --- Self-test --------------------------------------------------------------
//
// The audit is only worth running if it catches the defect it was written for, so
// `--self-test` runs it against disposable fixtures: a tree whose server reads a key no
// Compose file passes (must fail, naming it), the same tree with the pass-through added
// (must pass), and a tree whose only unpassed key is on the deliberate list (must pass).
if (process.argv.includes("--self-test")) {
  const script = fileURLToPath(import.meta.url);
  const repository = dirname(dirname(script));
  const workspace = mkdtempSync(join(tmpdir(), "brrpbrrp-compose-env-"));
  let failures = 0;
  try {
    const fixture = (name, composeEnvironment) => {
      const root = join(workspace, name);
      mkdirSync(join(root, "internal", "app"), { recursive: true });
      writeFileSync(
        join(root, "internal", "app", "config.go"),
        [
          "package app",
          "",
          'func load() {',
          '\tenv("BRRP_DATA_DIR", "./data")',
          '\tos.Getenv("BRRP_UNPASSED")',
          '\tos.Getenv("BRRP_LOG_FILE")',
          "}",
          "",
        ].join("\n"),
      );
      // A test file's keys belong to a harness, not to an operator; they must be ignored.
      writeFileSync(
        join(root, "internal", "app", "config_test.go"),
        'package app\n\nvar harness = "BRRP_EXPORT_LOAD"\n',
      );
      writeFileSync(
        join(root, "compose.yaml"),
        [
          "services:",
          "  brrpbrrp:",
          "    environment:",
          "      BRRP_DATA_DIR: /data",
          ...composeEnvironment,
          "",
        ].join("\n"),
      );
      return root;
    };

    const broken = fixture("broken", []);
    const brokenRun = audit(broken);
    if (brokenRun.problems.length !== 1) {
      console.error(
        `self-test: expected one complaint, got ${brokenRun.problems.length}: ${brokenRun.problems.join("; ")}`,
      );
      failures += 1;
    } else if (!brokenRun.problems[0].includes("BRRP_UNPASSED")) {
      console.error(
        `self-test: the complaint did not name the unpassed key: ${brokenRun.problems[0]}`,
      );
      failures += 1;
    }
    if (!brokenRun.passed.has("BRRP_DATA_DIR")) {
      console.error(
        "self-test: a key the Compose file passes was not recognised as passed",
      );
      failures += 1;
    }
    if (brokenRun.read.has("BRRP_EXPORT_LOAD")) {
      console.error("self-test: a key read only by a test file was treated as the server's");
      failures += 1;
    }

    const repaired = fixture("repaired", [
      "      BRRP_UNPASSED: ${BRRP_UNPASSED:-}",
    ]);
    const repairedRun = audit(repaired);
    if (repairedRun.problems.length > 0) {
      console.error(
        `self-test: the repaired fixture was rejected: ${repairedRun.problems.join("; ")}`,
      );
      failures += 1;
    }

    // A tree whose only unpassed key is on the deliberate list must pass. That is what
    // keeps the list a statement about the container layout rather than a way to silence a
    // real omission: it can only ever excuse the four keys it names.
    const intentional = join(workspace, "intentional");
    mkdirSync(join(intentional, "internal", "app"), { recursive: true });
    writeFileSync(
      join(intentional, "internal", "app", "config.go"),
      'package app\n\nfunc load() { os.Getenv("BRRP_LOG_FILE") }\n',
    );
    writeFileSync(
      join(intentional, "compose.yaml"),
      "services:\n  brrpbrrp:\n    environment:\n      BRRP_DATA_DIR: /data\n",
    );
    const intentionalRun = audit(intentional);
    if (intentionalRun.problems.length > 0) {
      console.error(
        `self-test: a deliberately fixed key was reported: ${intentionalRun.problems.join("; ")}`,
      );
      failures += 1;
    }

    if (failures > 0) {
      console.error(`Compose environment audit self-test failed (${failures} problem(s)).`);
      process.exit(1);
    }
    console.log(
      "Compose environment audit self-test passed: an unpassed key was reported with its name, a repaired tree passed, and deliberate and harness keys were ignored.",
    );
  } finally {
    rmSync(workspace, { recursive: true, force: true });
  }
  process.exit(0);
}

// --- Result -----------------------------------------------------------------

const root = process.argv[2] || ".";
const { problems, passed, read } = audit(root);

if (problems.length > 0) {
  console.error(`Compose environment audit failed (${problems.length} problem(s)):`);
  for (const problem of problems) console.error(`  - ${problem}`);
  console.error(
    "Pass each one through as ${NAME:-} in compose.yaml, or add it to deliberatelyFixed with the reason it is not an operator's setting.",
  );
  process.exit(1);
}
console.log(
  `Compose environment audit passed: ${read.size} keys the server reads are each passed by a Compose file or listed as deliberately fixed (${passed.size} passed, ${deliberatelyFixed.size} fixed by the container layout).`,
);
console.log(
  "This is a static audit of the Compose files; it does not build an image or start a container.",
);
