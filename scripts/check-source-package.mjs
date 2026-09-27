#!/usr/bin/env node
// Validate what the distributable source package promises, without building it.
//
// The archive's contents come from `go run scripts/release.go -source-list`, so this
// check reads the same list the packager writes. It answers the questions a consumer
// of a clean extraction would ask, and that previous releases got wrong (completion
// review F-15):
//
//   1. every listed path exists, so the package cannot advertise a file it lacks;
//   2. every relative documentation link in a packaged document resolves inside the
//      package, so no shipped doc points at a file the consumer does not have;
//   3. every script the Makefile invokes is packaged, so `make rehearsal`,
//      `make check` and `make docker-check` work from the extraction;
//   4. everything the Docker build context requires is packaged, so the image can be
//      built from the extraction;
//   5. the operator-specific documents stay out, so a public package does not carry
//      the maintainer's hosts and session notes;
//   6. the documents a native package ships do not link outside that package.
//
// Paths are handled in two forms on purpose. The package list is always an archive
// path with forward slashes, whatever host produced it, while reading and stating
// files uses the platform's own separators. Mixing the two is how a Windows check
// silently looks for a file called "scripts\release.go".
//
// Usage: node scripts/check-source-package.mjs [--list|--self-test]

import { existsSync, readFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { dirname, join, posix, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

// archivePath normalizes one entry of the packager's list into the archive's own
// form. The packager prints forward slashes; a list produced by an older build, a
// hand-edited manifest or a differently configured host may carry the native
// separator, which must be normalized rather than treated as part of a file name.
function archivePath(line) {
  return posix.normalize(line.trim().replaceAll('\\', '/'));
}

// diskPath resolves an archive path against a package root on this host, so a root
// that is a Windows drive path or contains spaces is joined with the native
// separator instead of being concatenated as text.
function diskPath(root, archive) {
  return join(root, ...archive.split('/'));
}

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const go = process.env.BRRP_GO || 'go';

// packageListEntries parses a packager list: blank entries — including the
// trailing newline every command prints — are dropped BEFORE normalization, so an
// empty line cannot normalize into a phantom `.` entry (review 8, R8-08).
function packageListEntries(stdout) {
  return stdout
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map(archivePath);
}

// readPackageList runs one list flag of the packager, so the check validates the
// same contents the archive is built from rather than a second copy of the rules.
function readPackageList(args) {
  const result = spawnSync(go, ['run', 'scripts/release.go', ...args], { encoding: 'utf8', maxBuffer: 32 << 20 });
  if (result.status !== 0) {
    console.error(`could not read the package list (${args.join(' ')}): ${result.error?.message || result.stderr || result.stdout}`);
    process.exit(1);
  }
  const paths = packageListEntries(result.stdout);
  if (paths.length === 0) {
    console.error(`the package list (${args.join(' ')}) is empty`);
    process.exit(1);
  }
  return new Set(paths);
}

// selfTest exercises the two path forms without reading the repository: a Windows
// drive path with spaces, a native separator inside a list entry and a nested
// archive path. It runs on any host, and is the regression for the non-portable
// handling review R2-10 found.
function selfTest() {
  const problems = [];
  const expect = (name, actual, wanted) => {
    if (actual !== wanted) problems.push(`${name}: ${JSON.stringify(actual)} != ${JSON.stringify(wanted)}`);
  };
  expect('a native separator becomes an archive separator', archivePath('scripts\\release.go'), 'scripts/release.go');
  expect('a noisy entry is trimmed and normalized', archivePath('  docs//adr/0001-a.md  '), 'docs/adr/0001-a.md');
  // R8-08: filtering runs before normalization; the trailing newline a command
  // always prints must not become a phantom `.` entry that inflates the count.
  expect('empty input yields no entries', packageListEntries('').length, 0);
  expect('a trailing blank line adds no entry', packageListEntries('docs/a.md\n').join('|'), 'docs/a.md');
  expect('blank-only input yields no entries', packageListEntries('\n \n\t\n').length, 0);
  expect('a Windows path with a space stays one entry', packageListEntries('C:\\build\\Brrp Brrp\\scripts\\release.go\n').join('|'), 'C:/build/Brrp Brrp/scripts/release.go');
  expect('an absolute drive path is not mistaken for an archive path', archivePath('C:\\build\\BrrpBrrp'), 'C:/build/BrrpBrrp');
  expect('a root with spaces joins with the native separator', diskPath('C:\\Users\\Release Package\\BrrpBrrp', 'scripts/release.go'), ['C:\\Users\\Release Package\\BrrpBrrp', 'scripts', 'release.go'].join(sep));
  expect('a nested archive path keeps its own separator in archive form', posix.dirname('docs/adr/0002-x.md'), 'docs/adr');
  expect('a link target with no directory resolves to the root', posix.normalize(posix.join('.', 'LICENSE')), 'LICENSE');
  if (problems.length) {
    console.error('Path-handling self-test failed:');
    for (const problem of problems) console.error(`  - ${problem}`);
    process.exit(1);
  }
  console.log('Path-handling self-test passed: archive paths use forward slashes, filesystem paths use the host separator, and a root with spaces or a drive letter joins correctly.');
}

if (process.argv.includes('--self-test')) {
  selfTest();
  process.exit(0);
}

const contents = readPackageList(['-source-list']);
if (contents.size === 0) {
  console.error('the source-package list is empty');
  process.exit(1);
}
if (process.argv.includes('--list')) {
  console.log([...contents].sort().join('\n'));
  process.exit(0);
}

const problems = [];
const fail = (message) => problems.push(message);

// linkTargets returns the relative link targets of a document, skipping links inside
// quoted text: a fenced or inline code span is an example, not a promise.
function linkTargets(path) {
  const text = readFileSync(diskPath(root, path), 'utf8').replace(/`[^`]*`/g, '');
  const targets = [];
  for (const [, target] of text.matchAll(/\[[^\]]*\]\(([^)]+)\)/g)) {
    const clean = target.split('#')[0].split('?')[0].trim();
    if (!clean || /^[a-z][a-z0-9+.-]*:/i.test(clean) || clean.startsWith('/')) continue;
    targets.push(clean);
  }
  return targets;
}
const packaged = (path) => {
  const normalized = posix.normalize(path);
  if (contents.has(normalized)) return true;
  // A link may address a directory; it resolves when anything beneath it is packaged.
  const prefix = normalized.endsWith('/') ? normalized : `${normalized}/`;
  return [...contents].some((entry) => entry.startsWith(prefix));
};

// 1 — a listed path must exist.
for (const path of contents) {
  if (!existsSync(diskPath(root, path))) fail(`the package lists ${path}, which does not exist`);
}

// 2 — packaged documents must not link outside the package.
function checkLinks(path, contains, label) {
  const directory = posix.dirname(path);
  for (const target of linkTargets(path)) {
    // A link may address a directory ("adr/"); it resolves when anything beneath it
    // is packaged. The trailing slash is removed first, or the prefix would be
    // "docs/adr//" and match nothing.
    const resolved = posix.normalize(posix.join(directory, target)).replace(/\/+$/, '');
    if (resolved.startsWith('..')) {
      fail(`${path} links to ${target}, which leaves the ${label}`);
      continue;
    }
    const found = contains.has(resolved)
      ? true
      : [...contains].some((entry) => entry.startsWith(`${resolved}/`));
    if (!found) fail(`${path} links to ${target}, which the ${label} does not contain`);
  }
}
for (const path of contents) {
  if (path.endsWith('.md')) checkLinks(path, contents, 'source package');
}

// 6 — the documents a native package ships are a different set, so they are checked
// against that set: a shipped document that links to a document the native package
// does not carry is a broken link in the consumer's hands, even though the source
// package happens to contain the target.
const nativeDocs = readPackageList(['-native-list', 'windows']);
for (const path of nativeDocs) {
  if (path.endsWith('.md')) checkLinks(path, nativeDocs, 'native package');
}

// 3 — every script the Makefile runs must be packaged, which is the defect the
// completion review found with the rehearsal target.
const makefile = readFileSync(diskPath(root, 'Makefile'), 'utf8');
for (const [, script] of makefile.matchAll(/(scripts\/[\w.-]+\.(?:mjs|go|sh))/g)) {
  if (!packaged(script)) fail(`the Makefile runs ${script}, which the source package does not contain`);
}
for (const [line] of makefile.matchAll(/^\t.*$/gm)) {
  for (const [, script] of line.matchAll(/(scripts\/[\w.-]+\.(?:mjs|go|sh))/g)) {
    if (!packaged(script)) fail(`the Makefile target "${line.trim()}" runs ${script}, which is not packaged`);
  }
}

// 4 — the Docker build context must be buildable from the extraction.
const dockerignore = readFileSync(diskPath(root, '.dockerignore'), 'utf8');
for (const rule of dockerignore.split('\n')) {
  const trimmed = rule.trim();
  if (!trimmed.startsWith('!')) continue;
  const path = trimmed.slice(1);
  if (path.endsWith('/**')) {
    // A whole tree: one packaged file beneath it is enough.
    const prefix = `${path.slice(0, -3)}/`;
    if (![...contents].some((entry) => entry.startsWith(prefix))) {
      fail(`.dockerignore admits ${path}, but the source package contains nothing beneath it`);
    }
    continue;
  }
  if (path.includes('*')) {
    const directory = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '.';
    const suffix = path.split('/').pop().replace(/\*/g, '');
    const matched = [...contents].some(
      (entry) => (directory === '.' || entry.startsWith(`${directory}/`)) && entry.endsWith(suffix) && !entry.slice(directory === '.' ? 0 : directory.length + 1).includes('/')
    );
    if (!matched) fail(`.dockerignore admits ${path}, but no such file is packaged`);
    continue;
  }
  if (!packaged(path)) fail(`.dockerignore admits ${path}, which the source package does not contain`);
}

// 5 — the operator's own hosts and session notes stay out.
for (const path of ['docs/next-release.md', 'docs/tailscale-setup-legacy.md', 'docs/windows-startup-fix.md']) {
  if (contents.has(path)) fail(`${path} records operator-specific hosts and must not be packaged`);
}

if (problems.length > 0) {
  console.error(`Source package check failed (${problems.length} problem(s)):`);
  for (const problem of problems) console.error(`  - ${problem}`);
  process.exit(1);
}
console.log(`Source package check passed: ${contents.size} files, ${nativeDocs.size} native package documents, every documented local link resolved in both sets, every Makefile script and .dockerignore input packaged, and the operator-specific notes excluded.`);
console.log(`This validates the package contents, not an extracted build; the archive itself is produced by "make release".`);
