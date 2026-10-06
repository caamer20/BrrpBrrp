#!/usr/bin/env node
// Static audit: every BRRP_* variable the server reads must be documented where an
// operator looks for one.
//
// The defect class this exists for: the server reads a key that neither example file
// mentions nor the deployment guide's table lists. The setting works, and nobody knows it
// exists. `.env.example` is what a new installation copies and edits, and the table in
// `docs/deployment.md` is what an operator reads when something behaves unexpectedly, so a
// key missing from either is unreachable in practice even though it is reachable in the
// code. Its sibling, `check-compose-env.mjs`, asks the other half of the same question:
// whether the container actually receives the keys it is told about.
//
// A key counts as documented when its name appears in the file at all, commented out or
// not. Most of `.env.example` is commented optional settings by design, and a line reading
// `# BRRP_LOG_FILE=logs/server.log` is exactly the documentation this asks for.
//
// Usage: node scripts/check-env-coverage.mjs [root]

import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { serverKeys } from "./env-keys.mjs";

const destinations = [".env.example", "docs/deployment.md"];

function mentions(path, key) {
  if (!existsSync(path)) return false;
  return new RegExp(`\\b${key}\\b`).test(readFileSync(path, "utf8"));
}

function audit(root) {
  const keys = serverKeys(root);
  const problems = [];
  for (const destination of destinations) {
    if (!existsSync(join(root, destination))) {
      problems.push(`${destination} is not present, so it documents nothing`);
      continue;
    }
  }
  for (const key of [...keys].sort()) {
    for (const destination of destinations) {
      if (mentions(join(root, destination), key)) continue;
      problems.push(`${destination} does not mention ${key}`);
    }
  }
  return { problems, keys };
}

// --- Self-test --------------------------------------------------------------
//
// The audit is only worth running if it catches the defect it was written for, so
// `--self-test` builds disposable fixtures: a tree whose server reads a key neither file
// mentions (must fail once per destination), the same tree with the key added as a comment
// (must pass, because that is how the example file is written), and a tree missing one of
// the destinations entirely (must fail on the file, not on every key inside it).
if (process.argv.includes("--self-test")) {
  const script = fileURLToPath(import.meta.url);
  const repository = dirname(dirname(script));
  const workspace = mkdtempSync(join(tmpdir(), "brrpbrrp-env-coverage-"));
  let failures = 0;
  try {
    const fixture = (name, envExample, deployment) => {
      const root = join(workspace, name);
      mkdirSync(join(root, "internal", "app"), { recursive: true });
      mkdirSync(join(root, "docs"), { recursive: true });
      writeFileSync(
        join(root, "internal", "app", "config.go"),
        [
          "package app",
          "",
          "func load() {",
          '\tenv("BRRP_DATA_DIR", \"./data\")',
          '\tos.Getenv("BRRP_UNPASSED")',
          "}",
          "",
        ].join("\n"),
      );
      writeFileSync(
        join(root, "internal", "app", "config_test.go"),
        'package app\n\nvar harness = "BRRP_EXPORT_LOAD"\n',
      );
      if (envExample !== null) writeFileSync(join(root, ".env.example"), envExample);
      if (deployment !== null) {
        writeFileSync(join(root, "docs", "deployment.md"), deployment);
      }
      return root;
    };

    const base = "BRRP_DATA_DIR=./data\n";
    const guide = "| `BRRP_DATA_DIR` | the data folder |\n";

    const broken = fixture("broken", base, guide);
    const brokenRun = audit(broken);
    if (brokenRun.problems.length !== 2) {
      console.error(
        `self-test: expected two complaints, got ${brokenRun.problems.length}: ${brokenRun.problems.join("; ")}`,
      );
      failures += 1;
    }
    for (const destination of destinations) {
      if (!brokenRun.problems.some((problem) => problem.includes(destination))) {
        console.error(`self-test: no complaint named ${destination}`);
        failures += 1;
      }
    }
    if (brokenRun.keys.has("BRRP_EXPORT_LOAD")) {
      console.error("self-test: a key read only by a test file was treated as the server's");
      failures += 1;
    }

    const repaired = fixture(
      "repaired",
      `${base}# BRRP_UNPASSED=\n`,
      `${guide}| \`BRRP_UNPASSED\` | a documented optional key |\n`,
    );
    const repairedRun = audit(repaired);
    if (repairedRun.problems.length > 0) {
      console.error(
        `self-test: the repaired fixture was rejected: ${repairedRun.problems.join("; ")}`,
      );
      failures += 1;
    }

    const absent = fixture("absent", base, null);
    const absentRun = audit(absent);
    if (!absentRun.problems.some((problem) => problem.includes("deployment.md"))) {
      console.error("self-test: a missing destination file was not reported");
      failures += 1;
    }

    if (failures > 0) {
      console.error(`Environment coverage audit self-test failed (${failures} problem(s)).`);
      process.exit(1);
    }
    console.log(
      "Environment coverage audit self-test passed: an undocumented key was reported once per destination, a commented example satisfied it, and a missing document was reported as itself.",
    );
  } finally {
    rmSync(workspace, { recursive: true, force: true });
  }
  process.exit(0);
}

// --- Result -----------------------------------------------------------------

const root = process.argv[2] || ".";
const { problems, keys } = audit(root);

if (problems.length > 0) {
  console.error(`Environment coverage audit failed (${problems.length} problem(s)):`);
  for (const problem of problems) console.error(`  - ${problem}`);
  console.error(
    "Document each key in `.env.example` (a commented line is enough) and in the table in docs/deployment.md, or remove the read.",
  );
  process.exit(1);
}
console.log(
  `Environment coverage audit passed: all ${keys.size} keys the server reads appear in ${destinations.join(" and ")}.`,
);
console.log(
  "This is a static audit of two documents. It does not read a configuration or start a server.",
);
