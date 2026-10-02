#!/usr/bin/env node
// Checks that the schema number the documents state is the schema this binary writes.
//
// Three documents tell an operator what schema the current release writes, and all
// three were wrong: deployment.md said 13, next-release.md said 21, and the schema was
// 24. A reader planning an update reads those to decide whether a backup taken now can
// be restored afterwards, and a stale number there is not a typo — it is the basis of a
// rollback decision that has already gone wrong before anyone starts.
//
// So the number is read from the code rather than trusted from prose. `SchemaVersion` is
// derived from the migration registry, so there is one answer and it cannot be wrong.
//
// The gate must pass on a fresh clone: `docs/next-release.md` is a deliberately
// untracked operator note, so it is checked when it exists and skipped when it does
// not — while the tracked documents that state a schema are required, because a
// tracked document that stopped stating one would have silently left the gate checking
// less than it says it does.
//
// Usage: node scripts/check-schema-docs.mjs [--self-test]

import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');

// The documents that state the schema, how each one phrases it, and whether a clone is
// expected to contain it. A document that stops stating a schema entirely is not an
// error: the point is that a stated number is right, not that every document carries
// one — but a *required* document that has gone missing is an error, because the gate
// would otherwise check nothing where it claims to check something.
const CLAIMS = [
  // Anchored, because this document also states the *backup format*, whose number is
  // written the same way and is a different thing entirely. An unanchored match reads
  // "schema 24, backup format 2" as schema 2.
  ['docs/v1-implementation-plan.md', /schema (\d+), backup format/, { required: true }],
  ['docs/deployment.md', /current source candidate writes schema (\d+)/, { required: true }],
  // An operator note: present in a working checkout, absent from a fresh clone.
  ['docs/next-release.md', /schema (\d+)/, { required: false }]
];

// schemaNumber reads the schema the binary writes from the migration registry, which is
// the source of truth `SchemaVersion` is derived from, so this cannot disagree with what
// ships.
export function schemaNumber(tree) {
  const migrations = readFileSync(join(tree, 'internal/db/migrations.go'), 'utf8');
  const versions = [...migrations.matchAll(/\{Version:\s*(\d+),/g)].map((match) => Number(match[1]));
  if (!versions.length) return null;
  return Math.max(...versions);
}

// checkClaims returns one message per problem, reading the documents under `tree`.
export function checkClaims(tree, schema, claims = CLAIMS) {
  const problems = [];
  for (const [path, pattern, options] of claims) {
    if (!existsSync(join(tree, path))) {
      if (options?.required === false) continue;
      problems.push(`${path} does not exist, so the schema it states cannot be checked`);
      continue;
    }
    const text = readFileSync(join(tree, path), 'utf8');
    const match = pattern.exec(text);
    if (!match) {
      problems.push(`${path} states no schema number this check can verify`);
      continue;
    }
    const stated = Number(match[1]);
    if (stated !== schema) {
      problems.push(`${path} states schema ${stated}, but the binary writes schema ${schema}`);
    }
  }
  return problems;
}

function selfTest() {
  const tree = mkdtempSync(join(tmpdir(), 'brrp-schema-docs-'));
  let failures = 0;
  const expect = (name, condition) => {
    console.log(`  ${condition ? 'ok  ' : 'FAIL'}  ${name}`);
    if (!condition) failures += 1;
  };
  try {
    writeFileSync(
      join(tree, 'plan.md'),
      'The current source candidate writes schema 25, backup format 2.\n'
    );
    writeFileSync(join(tree, 'deployment.md'), 'The current source candidate writes schema 25.\n');
    const claims = [
      ['plan.md', /schema (\d+), backup format/, { required: true }],
      ['deployment.md', /schema (\d+)/, { required: true }],
      ['next-release.md', /schema (\d+)/, { required: false }]
    ];

    // A fresh clone: the optional note is absent and that is not a problem.
    expect('a tree without the untracked note passes', checkClaims(tree, 25, claims).length === 0);

    // A required document that states the wrong number still fails.
    writeFileSync(join(tree, 'deployment.md'), 'The current source candidate writes schema 24.\n');
    expect(
      'a wrong number in a required document fails',
      checkClaims(tree, 25, claims).some((problem) => problem.includes('schema 24'))
    );

    // A required document that is missing fails rather than being skipped.
    rmSync(join(tree, 'deployment.md'));
    expect(
      'a missing required document fails',
      checkClaims(tree, 25, claims).some((problem) => problem.includes('does not exist'))
    );

    // A tracked document that stopped stating a number fails: otherwise the gate would
    // pass while checking nothing.
    writeFileSync(join(tree, 'deployment.md'), 'Schema version is documented elsewhere.\n');
    expect(
      'a required document with no statement fails',
      checkClaims(tree, 25, claims).some((problem) => problem.includes('states no schema'))
    );

    // When the optional note is present, its number is checked like anybody else's.
    writeFileSync(join(tree, 'next-release.md'), 'Alpha, schema 21.\n');
    expect(
      'a wrong number in the present note fails',
      checkClaims(tree, 25, claims).some((problem) => problem.includes('next-release.md'))
    );
  } finally {
    rmSync(tree, { recursive: true, force: true });
  }
  if (failures) {
    console.log(`\n${failures} schema-documentation self-test failure(s).`);
    process.exit(1);
  }
  console.log('Schema-documentation checks: self-test passed (5 cases).');
}

function main() {
  const schema = schemaNumber(root);
  if (schema === null) {
    console.error('no migrations found in internal/db/migrations.go — cannot check the schema');
    process.exit(1);
  }
  const problems = checkClaims(root, schema);
  if (problems.length) {
    console.error(`Schema documentation is out of date (the binary writes schema ${schema}):`);
    for (const problem of problems) console.error(`  - ${problem}`);
    process.exit(1);
  }
  console.log(`Documentation schema: all statements match schema ${schema}.`);
}

if (process.argv.includes('--self-test')) {
  selfTest();
} else {
  main();
}
