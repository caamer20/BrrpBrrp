// Upgrading a real schema-13 installation to the current schema and proving the
// data survived exactly.
//
//   node scripts/upgrade-schema13-private.mjs --make-seed PATH --old-binary PATH
//   node scripts/upgrade-schema13-private.mjs --seed-dir PATH --binary PATH
//
// The seed directory must have been produced by scripts/seed-schema13-private.mjs
// against an actual archived schema-13 executable. This script copies it, migrates
// the copy with the current binary, and compares every preserved value rather than
// checking that rows still exist: a migration that rewrote an expiry, truncated a
// snapshot or altered a request state would leave the same row count.
//
// The backup format is deliberately unchanged by the migration, so a rollback to
// the old executable's own archive remains the supported path. That is proved
// separately by scripts/upgrade-rollback.mjs, which is the script that owns
// cross-version behaviour.

import { DatabaseSync } from 'node:sqlite';
import { execFileSync } from 'node:child_process';
import { cpSync, existsSync, readFileSync, rmSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const args = process.argv.slice(2);
const option = (flag, fallback) => {
  const index = args.indexOf(flag);
  return index < 0 ? fallback : args[index + 1];
};
const makeSeed = option('--make-seed', process.env.MAKE_SEED);
const oldBinary = option('--old-binary', process.env.OLD_BINARY);
const seedDir = option('--seed-dir', process.env.SEED_DIR);
const binary = option('--binary', 'dist/brrpbrrp');

// --make-seed builds a real schema-13 data directory by running a *preserved prior
// executable* against an empty one, then creating a direct conversation and a pending
// request through its own HTTP surface.
//
// A fixture assembled by writing SQL into a current database would not be a schema-13
// database at all: it would be a current one with old-looking rows, and the rehearsal
// would pass while proving nothing about the upgrade. The executable is what makes the
// fixture real, which is why this takes a binary rather than a schema number.
//
// The binary must actually BE a schema-13 release. Given the current one, the seed it
// writes is a current database, and the rehearsal's own first check says so — which is
// the tool doing its job: a seed that is not the version under test is rejected rather
// than accepted as a lookalike.
if (makeSeed) {
  if (!oldBinary || !existsSync(oldBinary)) {
    console.error('usage: --make-seed PATH --old-binary PATH');
    process.exit(2);
  }
  const { makeSchema13Seed } = await import('./schema13-seed.mjs');
  await makeSchema13Seed({ seedDir: makeSeed, oldBinary, cwd: dirname(fileURLToPath(import.meta.url)) });
  process.exit(0);
}

if (!seedDir || !existsSync(join(seedDir, 'brrp.db'))) {
  console.error('usage: --make-seed PATH --old-binary PATH | --seed-dir PATH --binary PATH');
  process.exit(2);
}

let failures = 0;
const check = (label, condition, detail = '') => {
  if (condition) console.log(`  ok    ${label}${detail ? ` — ${detail}` : ''}`);
  else {
    console.log(`  FAIL  ${label}${detail ? ` — ${detail}` : ''}`);
    failures += 1;
  }
};
const run = (dir, ...command) =>
  execFileSync(binary, [...command, '--data-dir', dir], { encoding: 'utf8' });

// A read-only handle, so a probe can never be the thing that changes the data.
const read = (dir, sql) => {
  const db = new DatabaseSync(join(dir, 'brrp.db'), { readOnly: true });
  try {
    return db.prepare(sql).all();
  } finally {
    db.close();
  }
};

const PRESERVED = {
  evidence: `SELECT id,snapshot_text,reason,status,created_at,updated_at,expires_at FROM private_message_reports ORDER BY id`,
  requests:
    'SELECT conversation_id,state,recipient_id,requester_id,introduction,requested_at,retry_after FROM direct_requests ORDER BY conversation_id',
  messages: 'SELECT id,conversation_id,content,created_at FROM messages ORDER BY id',
  blocks: 'SELECT blocker_id,blocked_id,created_at FROM user_blocks ORDER BY blocker_id,blocked_id'
};

const work = `${seedDir}-upgraded`;
rmSync(work, { recursive: true, force: true });
cpSync(seedDir, work, { recursive: true });

console.log(`1. The seed really is schema 13`);
const before = read(work, 'SELECT version FROM schema_migrations ORDER BY version');
const versions = before.map((row) => row.version);
check('the seed has 13 applied migrations', versions.at(-1) === 13, `highest is ${versions.at(-1)}`);
check('the seed is still the schema-13 snapshot', versions.at(-1) === 13, `highest is ${versions.at(-1)}`);
// Proof that the column does not exist yet: querying it raises, so the schema
// itself is inspected rather than the rows. This is the state migration 14 exists
// to change, and the P10 rehearsal skipped it by upgrading from schema 12.
const requestColumns = read(work, 'SELECT name FROM pragma_table_info(\'direct_requests\')').map((row) => row.name);
check('the request table has no attempt identity yet', !requestColumns.includes('attempt_id'), requestColumns.join(','));

const before_ = {};
for (const [name, sql] of Object.entries(PRESERVED)) before_[name] = read(work, sql);
check('the seed really holds private evidence', before_.evidence.length > 0, `${before_.evidence.length} row(s)`);
check(
  'the seed really holds a pending request',
  before_.requests.some((row) => row.state === 'pending'),
  `${before_.requests.length} request row(s)`
);

console.log('\n2. Migrate with the current binary');
// The registry this build carries, read from the source rather than assumed. A tool
// that hard-codes "one more migration" stops describing the product the moment a
// second one exists, and then rejects a healthy upgrade.
function registryVersions() {
  // The registry sits beside this script, so it is located from the script rather
  // than from the working directory, which the harness changes.
  const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
  const source = readFileSync(join(repoRoot, 'internal', 'db', 'migrations.go'), 'utf8');
  return [...source.matchAll(/\{Version: (\d+),/g)]
    .map((match) => Number(match[1]))
    .sort((a, b) => a - b);
}

const migrated = run(work, 'migrate');
console.log(`  ${migrated.trim().split('\n').at(-1)}`);
const verified = run(work, 'verify');
console.log(`  ${verified.trim()}`);
const after = read(work, 'SELECT version FROM schema_migrations ORDER BY version').map((row) => row.version);
// The pending set is whatever the source registry says it is, not a single migration.
// The tool was written when exactly one was outstanding and rejected every valid
// result after that, which made a healthy upgrade look like a broken one
// (completion review 13, V13-10).
const expected = registryVersions().filter((version) => version > versions.at(-1));
check(
  'every migration this build carries has been applied',
  expected.every((version) => after.includes(version)),
  `expected ${expected.join(',') || 'none'} beyond ${versions.at(-1)}`
);
check(
  'no migration was skipped',
  after.length === versions.length + expected.length,
  `${versions.length} -> ${after.length}`
);
check('no earlier migration was re-applied or reordered',
  after.filter((version) => version <= versions.at(-1)).join(',') === versions.join(','),
  after.join(','));

console.log('\n3. Every preserved value is identical, not merely present');
for (const [name, sql] of Object.entries(PRESERVED)) {
  const now = read(work, sql);
  check(
    `${name} survived byte for byte`,
    JSON.stringify(now) === JSON.stringify(before_[name]),
    `${before_[name].length} row(s)`
  );
}

console.log('\n4. The attempt identity the migration added is usable');
const attempts = read(work, 'SELECT conversation_id,state,attempt_id FROM direct_requests ORDER BY conversation_id');
check('every request has an attempt identity', attempts.every((row) => row.attempt_id && row.attempt_id !== ''));
check('attempt identities are distinct per row', new Set(attempts.map((row) => row.attempt_id)).size === attempts.length);
const pending = attempts.filter((row) => row.state === 'pending');
check('the pending request has one', pending.length === 1 && pending[0].attempt_id !== '', pending[0]?.attempt_id ?? 'none');
check(
  'the new evidence tables exist',
  read(work, "SELECT name FROM sqlite_master WHERE type='table' AND name IN ('private_request_reports','private_report_receipts')").length === 2
);

console.log('\n5. The backup format is unchanged, so the old rollback path still works');
// The archive has to live inside the data directory: the executable refuses to
// write outside it, which is a deliberate guard rather than an obstacle.
const archive = join(work, 'backups', 'schema13-upgrade.zip');
run(work, 'backup', '--out', 'backups/schema13-upgrade.zip');
const manifest = run(work, 'verify-backup', '--file', archive);
check('an archive taken after the upgrade verifies', manifest.includes('Verified'), manifest.trim().split('\n').at(-1));
check('the format did not change', /format 2/.test(manifest), 'a changed format would break the old executable\'s own restore path');

rmSync(work, { recursive: true, force: true });
if (failures > 0) {
  console.log(`\nRehearsal FAILED: ${failures} check(s).`);
  process.exit(1);
}
console.log('\nRehearsal PASS: schema 13 private data upgraded to the current schema with every value preserved.');
console.log('Scope: a local upgrade and its postconditions. Cross-version rollback is scripts/upgrade-rollback.mjs.');
