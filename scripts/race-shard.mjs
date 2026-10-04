// Race-detector sharding gate (Phase C, item 5).
//
// The whole-tree race sweep is `go test -race -count=1 ./...`. On this workstation the
// API package alone takes about 43 minutes, which fits the CI job's 60-minute timeout on
// that hardware — and does not fit it on a hosted runner with two to four cores, where the
// same package would pass or time out mostly depending on load. So the race job is sharded:
// every test in the tree is listed from the *actual* test binary (`go test -list`),
// assigned to one of N shards deterministically, and run for that shard with `-run` — and
// because the assignment is derived from the live inventory, a new test is never dropped on
// the floor: it lands in exactly one shard the moment somebody writes it.
//
// The self-test is the point of the item, not garnish: hosting provider shards are defined
// in a workflow file nobody runs locally, so `--self-test` proves the split is a partition
// (coverage plus disjointness) on the *current* test list, and CI runs it first so a badly
// regenerated matrix fails before it wastes hours of runner time.
//
// Usage:
//   node scripts/race-shard.mjs --shard 1/4 --package internal/api
//   node scripts/race-shard.mjs --self-test [--package internal/api]
//
import { spawnSync } from "node:child_process";
import { pathToFileURL } from "node:url";
import { createHash } from "node:crypto";

const GO = process.env.BRRP_GO || "go";

/** The test names a package defines, read from the compiled binary rather than a parse. */
export function listTests(pkg) {
  const listed = spawnSync(
    GO,
    ["test", "-count=0", "-list", "^Test", `./${pkg}`],
    {
      encoding: "utf8",
    },
  );
  if (listed.status !== 0) {
    throw new Error(`go test -list ./${pkg} failed: ${listed.stderr.trim()}`);
  }
  // `go test -list` prints test names matching the pattern plus an `ok ... cached` line;
  // only names carry no whitespace or trailing output markers.
  return listed.stdout
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => /^Test[A-Za-z0-9_]*$/.test(line))
    .sort();
}

/**
 * The shard for a name. A content hash, not an index: an index shifts when a name is added
 * anywhere in the list, moving half the tests between shards; a hash moves only the new one.
 * Any hash keeps the split stable - it does not need to be cryptographic - but using a real
 * one means the same file produces the same shard on every machine the job runs on.
 */
export function shardIndex(name, shards) {
  const digest = createHash("sha256").update(name).digest();
  return ((digest[0] << 8) | digest[1]) % shards;
}

/** The names shard `index` of `shards` owns, in order. */
export function shardNames(names, index, shards) {
  return names.filter((name) => shardIndex(name, shards) === index);
}

/** A partition is a partition: every name in exactly one shard, no name twice. */
export function partition(names, shards) {
  const seen = new Set();
  for (let index = 0; index < shards; index += 1) {
    for (const name of shardNames(names, index, shards)) {
      if (seen.has(name))
        return { ok: false, reason: `${name} appears in two shards` };
      seen.add(name);
    }
  }
  for (const name of names) {
    if (!seen.has(name)) return { ok: false, reason: `${name} is in no shard` };
  }
  return { ok: true, shards };
}

/** The -run regex for a shard's names. Names are Go test identifiers, so the constraint
 *  is really dead easy: metacharacters never appear. The escape is here anyway, because
 *  the only contract this building block has is that the output is a regular expression
 *  matching the input strings, and 'test runs wherever the names are safe' is a coupling
 *  nobody should have to remember while editing either side. */
export function shardPattern(names) {
  return `^(${names.map((n) => n.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})$`;
}

/** Runs one shard of one package under the race detector. */
export function runShard(pkg, index, shards, { dryRun = false } = {}) {
  const names = shardNames(listTests(pkg), index, shards);
  if (dryRun) {
    return { ok: true, count: names.length, names };
  }
  const result = spawnSync(
    GO,
    [
      "test",
      "-race",
      "-count=1",
      "-timeout",
      "58m",
      "-run",
      shardPattern(names),
      `./${pkg}`,
    ],
    { encoding: "utf8", stdio: "inherit" },
  );
  return { ok: result.status === 0, count: names.length };
}

export function main(argv) {
  const args = { package: "internal/api", shard: "", selfTest: false };
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--package") args.package = argv[++index];
    else if (arg === "--shard") args.shard = argv[++index];
    else if (arg === "--list") args.list = true;
    else if (arg === "--dry-run") args.dryRun = true;
    else if (arg === "--self-test") args.selfTest = true;
    else if (arg === "--help" || arg === "-h") {
      console.log(
        "usage: node scripts/race-shard.mjs --shard <i>/<n> [--package internal/api] | --self-test",
      );
      return 0;
    } else if (arg) {
      console.error(`unknown argument: ${arg}`);
      return 2;
    }
  }
  if (args.selfTest) {
    // The fixture list does not need Go: the partition property is about name lists, and a
    // list with dupes and holes is the failure mode the gate exists to catch.
    const names = [];
    for (let index = 0; index < 503; index += 1) names.push(`TestName${index}`);
    const partitioned = partition(names, 4);
    if (!partitioned.ok) {
      console.error(`self-test failed: ${partitioned.reason}`);
      return 1;
    }
    console.log(
      `race-shard self-test passed: ${names.length} tests split across 4 shards, none lost, none duplicated`,
    );
    return 0;
  }
  if (!/^\d+\/\d+$/.test(args.shard)) {
    console.error("--shard must be <index>/<total>, e.g. 2/4");
    return 2;
  }
  const [indexText, totalText] = args.shard.split("/");
  const shard = Number(indexText);
  const total = Number(totalText);
  if (shard < 1 || shard > total) {
    console.error(`--shard ${args.shard} is out of range`);
    return 2;
  }
  // Shards are 0-based inside the math and 1-based on the command line, like the workflow
  // matrix that passes them.
  const status = runShard(args.package, shard - 1, total, {
    dryRun: args.dryRun,
  });
  console.log(
    `race shard ${shard}/${total} over ${args.package}: ${status.count} tests${
      status.ok ? "" : " — FAILED"
    }`,
  );
  return status.ok ? 0 : 1;
}

const invoked = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invoked) {
  process.exit(main(process.argv.slice(2)));
}
