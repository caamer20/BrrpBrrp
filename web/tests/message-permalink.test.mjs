import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

// A permalink has to resolve under *current* authority, and it has to carry a link
// rather than the message text. Both are properties of the code, and both are the kind
// of thing that silently stops being true when the surrounding code is edited.

const read = (file) => readFileSync(new URL(`../${file}`, import.meta.url), 'utf8');

test('a permalink names the message, not its content', () => {
  const workspace = read('src/lib/workspace.svelte.ts');
  // A link somebody shares is a reference. Pasting the text instead would leak a
  // private conversation to whoever receives the paste.
  assert.match(workspace, /messagePermalink\(id: string\)/);
  assert.match(workspace, /#message=/);
  // And it resolves through the same path as clicking a reply reference, which is the
  // one that re-checks access.
  assert.match(workspace, /await this\.jumpToMessage\(decodeURIComponent\(match\[1\]\)\)/);
});

test('a permalink is consumed once and the hash is cleared', () => {
  const workspace = read('src/lib/workspace.svelte.ts');
  // Without this, a reload would yank somebody back to a message they have scrolled
  // away from, every time.
  assert.match(workspace, /history\.replaceState\(null, '', location\.pathname/);
});

test('a cancelled upload is not reported as a failure', () => {
  const api = read('src/lib/api.ts');
  // The person asked for it. Telling them their upload "failed" would be telling them
  // something untrue, and a cancel button that also says "something went wrong" is a
  // button people stop trusting.
  assert.match(api, /class UploadCancelled/);
  assert.match(api, /request\.onabort = \(\) => reject\(new UploadCancelled\(\)\)/);

  const composer = read('src/lib/components/Composer.svelte');
  assert.match(composer, /error instanceof UploadCancelled/);
});

test('an upload can be stopped, and a failed one can be retried without repicking it', () => {
  const api = read('src/lib/api.ts');
  // An upload that cannot be cancelled is one somebody is trapped in: the only way out
  // is to close the tab, which costs them the draft too.
  assert.match(api, /abort: \(\) => request\.abort\(\)/);
  // And the retry keeps the file, so a person on a phone does not have to find it again.
  const composer = read('src/lib/components/Composer.svelte');
  assert.match(composer, /retryable = \{ file, name: file\.name \}/);
  assert.match(composer, /Try again/);
});
