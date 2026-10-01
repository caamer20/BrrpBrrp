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
// Usage: node scripts/check-schema-docs.mjs

import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');

// Read the number the binary writes. The registry is the source of truth and
// `SchemaVersion` is derived from it, so this cannot disagree with what ships.
const migrations = readFileSync(join(root, 'internal/db/migrations.go'), 'utf8');
const versions = [...migrations.matchAll(/\{Version:\s*(\d+),/g)].map((match) =>
  Number(match[1])
);
if (!versions.length) {
  console.error('no migrations found in internal/db/migrations.go — cannot check the schema');
  process.exit(1);
}
const schema = Math.max(...versions);

// The documents that state it, and how each one phrases it. A document that stops
// stating a schema entirely is not an error: the point is that a stated number is
// right, not that every document carries one.
const CLAIMS = [
  ['docs/next-release.md', /schema (\d+)/],
  // Anchored, because this document also states the *backup format*, whose number is
  // written the same way and is a different thing entirely. An unanchored match reads
  // "schema 24, backup format 2" as schema 2.
  ['docs/v1-implementation-plan.md', /schema (\d+), backup format/],
  ['docs/deployment.md', /current source candidate writes schema (\d+)/]
];

const problems = [];
for (const [path, pattern] of CLAIMS) {
  const text = readFileSync(join(root, path), 'utf8');
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

if (problems.length) {
  console.error(`Schema documentation is out of date (the binary writes schema ${schema}):`);
  for (const problem of problems) console.error(`  - ${problem}`);
  process.exit(1);
}
console.log(`Documentation schema: all statements match schema ${schema}.`);