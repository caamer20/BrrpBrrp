import test from 'node:test';
import assert from 'node:assert/strict';
import ts from 'typescript';
import { readFile } from 'node:fs/promises';
import { storage } from '../src/lib/storage.ts';

const source = await readFile(new URL('../src/lib/api.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 }
}).outputText;
const { api, APIError } = await import(
  'data:text/javascript;base64,' + Buffer.from(compiled).toString('base64')
);

test('network failures become useful connection errors', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => {
    throw new TypeError('Failed to fetch');
  });
  await assert.rejects(api('/state'), /Check your connection/);
});

test('non-JSON success cannot be mistaken for valid application data', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => new Response('<html>Proxy error</html>'));
  await assert.rejects(api('/state'), /unreadable response/);
});

test('API failures preserve status and the server explanation', async (t) => {
  t.mock.method(globalThis, 'fetch', async () =>
    Response.json({ error: 'Please sign in again.' }, { status: 401 })
  );
  await assert.rejects(
    api('/state'),
    (error) =>
      error instanceof APIError && error.status === 401 && error.message === 'Please sign in again.'
  );
});

test('a stalled request times out and offers retry', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  t.mock.method(
    globalThis,
    'fetch',
    (_url, options) =>
      new Promise((_resolve, reject) => {
        options.signal.addEventListener('abort', () =>
          reject(new DOMException('Aborted', 'AbortError'))
        );
      })
  );
  const response = api('/state');
  const check = assert.rejects(response, /took too long/);
  t.mock.timers.tick(30000);
  await check;
});

test('blocked storage does not prevent startup, preference changes, or sign-out', () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    get() {
      throw new DOMException('Blocked', 'SecurityError');
    }
  });
  try {
    assert.equal(storage.getItem('brrp.theme'), null);
    assert.doesNotThrow(() => storage.setItem('brrp.theme', 'light'));
    assert.doesNotThrow(() => storage.removeItem('brrp.drafts.me'));
  } finally {
    if (original) Object.defineProperty(globalThis, 'localStorage', original);
    else delete globalThis.localStorage;
  }
});

test('large backups can opt out of the ordinary request timeout', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  let finish;
  t.mock.method(
    globalThis,
    'fetch',
    (_url, options) =>
      new Promise((resolve, reject) => {
        finish = resolve;
        options.signal.addEventListener('abort', () =>
          reject(new DOMException('Aborted', 'AbortError'))
        );
      })
  );
  const backup = api('/admin/backup', 'POST', {}, { timeoutMs: 0 });
  t.mock.timers.tick(300000);
  finish(Response.json({ url: '/backup.zip' }));
  assert.deepEqual(await backup, { url: '/backup.zip' });
});
