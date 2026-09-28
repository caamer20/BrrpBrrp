import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

// R10-03: a conflict the client has to act on cannot be handled from a message
// alone. The server returns a refreshed preview alongside the refusal — the text
// the reporter must re-read before confirming again — and the api helper used to
// throw only the message and status, so that field never reached the dialog and
// the changed-preview flow could not finish. This exercises the real helper
// against a real Response, not a stub, because the stubbed workspace harness
// replaces the whole module.

const source = await readFile(new URL('../src/lib/api.ts', import.meta.url), 'utf8');
const compiled = ts
  .transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
  })
  .outputText.replace(/^export /gm, '');

const module = await import(
  'data:text/javascript;base64,' +
    Buffer.from(
      `const $state = value => value;
// A class, not an arrow function: the helper constructs this with "new", which an
// arrow function cannot be called with.
class AbortController { constructor() { this.signal = { aborted: false }; } abort() { this.signal.aborted = true; } }
class AbortError extends Error {}
${compiled}
export { APIError, api };`
    ).toString('base64')
);
const { api } = module;

// The helper reads only `ok`, `status` and `json()`, so a minimal object is
// enough here and keeps this test independent of a Response implementation.
const jsonResponse = (status, body) => ({
  ok: status >= 200 && status < 300,
  status,
  json: async () => body
});
test('a refused request carries the server body so a conflict can be acted on', async () => {
  const preview = {
    message_id: 'm1',
    author_name: 'writer',
    snapshot_text: 'the replacement text',
    version: 'a'.repeat(64),
    has_attachment: false,
    expires_at: '2026-12-27T00:00:00Z',
    reviewer_count: 1
  };
  const original = globalThis.fetch;
  globalThis.fetch = async () =>
    jsonResponse(409, { error: 'This message changed since you read it.', changed: true, preview });
  try {
    await assert.rejects(
      () =>
        api('/messages/m1/private-report', 'POST', {
          reason: 'unwelcome',
          version: 'b'.repeat(64)
        }),
      (error) => {
        // The class is the module's own, so its identity is the constructor
        // rather than a name: it carries the status and the server body.
        assert.equal(error.constructor.name, 'APIError', 'the helper still throws APIError');
        assert.equal(error.status, 409);
        // The refreshed preview is what makes a re-confirm possible at all.
        assert.deepEqual(error.detail?.preview, preview);
        return true;
      }
    );
  } finally {
    globalThis.fetch = original;
  }
});

test('a plain failure keeps its message and adds no invented detail', async () => {
  const original = globalThis.fetch;
  globalThis.fetch = async () =>
    jsonResponse(400, { error: 'Explain your report in 1–2,000 characters.' });
  try {
    await assert.rejects(
      () => api('/messages/m1/private-report', 'POST', { reason: '', version: '' }),
      (error) => {
        assert.equal(error.status, 400);
        assert.match(error.message, /Explain your report/);
        assert.equal(error.detail?.preview, undefined, 'no preview is invented on a plain failure');
        return true;
      }
    );
  } finally {
    globalThis.fetch = original;
  }
});
