#!/usr/bin/env node
// Fetch one archived prior release with a checksum the repository pins (review 8,
// R8-04). Cross-version upgrade/rollback checks must run against the REAL archived
// bytes; regenerating an "old release" from current source would prove nothing, and
// a missing archive must fail the check — never pass silently.
//
// Sources, in order:
//   --dir DIR        a local directory already holding the archive (default:
//                    BRRP_BASELINE_RELEASE_DIR or dist/releases)
//   --url-base URL   an approved HTTPS location holding release archives
//                    (default: BRRP_BASELINE_RELEASE_URL)
//
// The archive is always verified against scripts/baseline-releases.json after
// placement; a mismatch is a failure, not a warning.
//
// Usage:
//   node scripts/fetch-baseline-release.mjs --version 0.4.0-alpha.5 [--platform linux/amd64]
//                                           [--dir DIR] [--url-base URL] [--dest DIR]
//                                           [--self-test]
//
// Exit 0 prints the verified archive path. Exit 1 explains exactly what is missing
// and how an operator supplies it.

import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const options = { dir: process.env.BRRP_BASELINE_RELEASE_DIR || null, urlBase: process.env.BRRP_BASELINE_RELEASE_URL || null, dest: null };
{
  const argv = process.argv.slice(2);
  for (let index = 0; index < argv.length; index += 1) {
    const flag = argv[index];
    if (flag === "--version") options.version = argv[++index];
    else if (flag === "--platform") options.platform = argv[++index];
    else if (flag === "--dir") options.dir = argv[++index];
    else if (flag === "--url-base") options.urlBase = argv[++index];
    else if (flag === "--dest") options.dest = argv[++index];
    else if (flag === "--self-test") options.selfTest = true;
    else throw new Error(`unknown flag ${flag}`);
  }
}

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const manifest = JSON.parse(readFileSync(join(root, "scripts", "baseline-releases.json"), "utf8"));

function digestOf(file) {
  return createHash("sha256").update(readFileSync(file)).digest("hex");
}

function fail(message) {
  console.error(`baseline release unavailable: ${message}`);
  console.error(
    "Supply the archived release unchanged: place it under the directory named by --dir/BRRP_BASELINE_RELEASE_DIR, or set --url-base/BRRP_BASELINE_RELEASE_URL to an approved HTTPS host. The digest in scripts/baseline-releases.json is checked either way. Do not rebuild a stand-in from current source: a cross-version check must exercise the actual prior bytes.",
  );
  process.exit(1);
}

async function fetchBaseline(version, platform) {
  const entry = manifest.baselines?.[version];
  if (!entry) fail(`no pinned identities are recorded for ${version} (known: ${Object.keys(manifest.baselines ?? {}).join(", ")})`);
  const os = platform?.split("/")[0] ?? ({ darwin: "darwin", linux: "linux", win32: "windows" })[process.platform];
  const arch = platform?.split("/")[1] ?? process.arch;
  const name = `brrpbrrp-${version}-${os}-${arch}.zip`;
  const wanted = entry.archives?.[name];
  if (!wanted) fail(`no pinned archive ${name} is recorded for ${version}`);
  const destination = join(options.dest ?? join(root, "dist", "releases"), name);

  const local = options.dir ? join(options.dir, name) : existsSync(destination) ? destination : null;
  if (local && existsSync(local)) {
    const digest = digestOf(local);
    if (digest === wanted) {
      if (local !== destination) {
        mkdirSync(dirname(destination), { recursive: true });
        writeFileSync(destination, readFileSync(local));
      }
      console.log(`ok  ${name} — sha256 ${digest} (${statSync(destination).size} bytes, verified at ${destination})`);
      return destination;
    }
    fail(`${local} is present but its sha256 ${digest} does not match the pinned ${wanted}`);
  }
  if (!options.urlBase)
    fail(`${name} is not at ${destination} and no source is configured (--dir/BRRP_BASELINE_RELEASE_DIR, --url-base/BRRP_BASELINE_RELEASE_URL)`);
  const url = `${options.urlBase.replace(/\/$/, "")}/${name}`;
  const response = await fetch(url, { signal: AbortSignal.timeout(120000) });
  if (!response.ok) fail(`GET ${url} answered HTTP ${response.status}`);
  const bytes = Buffer.from(await response.arrayBuffer());
  const digest = createHash("sha256").update(bytes).digest("hex");
  if (digest !== wanted)
    fail(`${url} downloaded ${bytes.length} bytes with sha256 ${digest}, not the pinned ${wanted}`);
  mkdirSync(dirname(destination), { recursive: true });
  writeFileSync(destination, bytes);
  console.log(`ok  ${name} — sha256 ${digest} (fetched from ${url})`);
  return destination;
}

function selfTest() {
  const problems = [];
  const expectEq = (name, actual, wanted) => {
    if (actual !== wanted) problems.push(`${name}: ${JSON.stringify(actual)} != ${JSON.stringify(wanted)}`);
  };
  const alpha5 = manifest.baselines["0.4.0-alpha.5"];
  expectEq("every baseline records sha256-length digests", Object.values(manifest.baselines).every((entry) =>
    Object.values(entry.archives).every((digest) => /^[0-9a-f]{64}$/.test(digest))), true);
  expectEq("the packaged checkpoint names six native archives", Object.keys(alpha5.archives).length, 6);
  expectEq("the legacy baseline names six native archives", Object.keys(manifest.baselines["0.4.0-alpha.3"].archives).length, 6);
  expectEq("the pinned manifest rejects an unknown version", manifest.baselines["0.0.0-imaginary"] === undefined, true);
  if (problems.length) {
    console.error("baseline manifest self-test failed:");
    for (const problem of problems) console.error(`  - ${problem}`);
    process.exit(1);
  }
  console.log("Baseline manifest self-test passed: every pinned identity is a sha256 digest, both baselines name six platform archives, and unknown versions are refused.");
}

if (options.selfTest) {
  selfTest();
  process.exit(0);
}
if (!options.version) fail("--version is required (for example 0.4.0-alpha.5)");
process.exitCode = 0;
await fetchBaseline(options.version, options.platform);
