// Upgrading a real schema-13 installation to the current schema and proving the
// data survived exactly.
//
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
import { cpSync, existsSync, rmSync } from 'node:fs';
import { join } from 'node:path';

const args = process.argv.slice(2);
const option = (flag, fallback) => {
  const index = args.indexOf(flag);
  return index < 0 ? fallback : args[index + 1];
};
const seedDir = option('--seed-dir', process.env.SEED_DIR);
const binary = option('--binary', 'dist/brrpbrrp');
if (!seedDir || !existsSync(join(seedDir, 'brrp.db'))) {
  console.error('usage: --seed-dir PATH --binary PATH (seed-dir is a schema-13 data directory)');
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
check('migration 14 has not been applied', !versions.includes(14));
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
const migrated = run(work, 'migrate');
console.log(`  ${migrated.trim().split('\n').at(-1)}`);
const verified = run(work, 'verify');
console.log(`  ${verified.trim()}`);
const after = read(work, 'SELECT version FROM schema_migrations ORDER BY version').map((row) => row.version);
check('migration 14 is now applied', after.includes(14), `${after.length} migrations`);
check('no earlier migration was re-applied', after.length === versions.length + 1);

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
