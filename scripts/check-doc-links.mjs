#!/usr/bin/env node
// Checks that every relative markdown link in the documents a reader actually opens
// resolves to a file that exists.
//
// A broken link in a README is not a cosmetic defect: it is the one place a person
// who has not seen the repository is told where to go next, and a link to a document
// that was renamed sends them nowhere. The failure names the document and the target,
// because "a link is broken" is not something anybody can act on.
//
// Usage: node scripts/check-doc-links.mjs [--list]

import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');

// The documents a reader is sent to. A link inside a historical record is not this
// gate's business: those records are kept for what they decided, and a renamed file
// referenced from one of them is history rather than a route into the product.
const DOCUMENTS = [
  'README.md',
  'docs/next-release.md',
  'docs/community-guide.md',
  'docs/installation.md',
  'docs/deployment.md',
  'docs/host-qualification.md',
  'docs/v1-progress.md',
  'docs/production-readiness.md',
  ...['0.4.0-alpha.7', '0.4.0-alpha.6'].map(
    (release) => `docs/releases/${release}.md`
  )
];

// A relative link: no scheme, no host, no fragment-only target. Absolute and
// protocol links are somebody else's problem.
const LINK = /\]\(([^)\s]+?)(?:\s+"[^"]*")?\)/g;

let failures = 0;
for (const document of DOCUMENTS) {
  if (!existsSync(join(root, document))) {
    console.log(`  FAIL  ${document} is listed here but does not exist`);
    failures += 1;
    continue;
  }
  const text = readFileSync(join(root, document), 'utf8');
  for (const [, target] of text.matchAll(LINK)) {
    if (/^(https?:|mailto:|#)/.test(target)) continue;
    // A link's target is relative to the document that contains it.
    const withoutFragment = target.split('#')[0].split('?')[0];
    if (!withoutFragment) continue;
    const resolved = resolve(dirname(join(root, document)), withoutFragment);
    if (!existsSync(resolved)) {
      console.log(`  FAIL  ${document} links to ${target}, which does not exist`);
      failures += 1;
    }
  }
}

if (failures) {
  console.log(`\n${failures} broken documentation link(s).`);
  process.exit(1);
}
console.log(`Documentation links: all resolve (${DOCUMENTS.length} documents).`);
