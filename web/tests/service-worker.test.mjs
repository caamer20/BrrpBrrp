import test from 'node:test';
import assert from 'node:assert/strict';
import vm from 'node:vm';
import { readFile } from 'node:fs/promises';
const template = await readFile(new URL('../public/service-worker.js', import.meta.url), 'utf8');
const origin = 'https://brrp.test';
function worker({ fail = false } = {}) {
  const listeners = new Map(),
    stores = new Map();
  let skipped = 0;
  const key = (value) => new URL(typeof value === 'string' ? value : value.url, origin).href;
  const caches = {
    async open(name) {
      if (!stores.has(name)) stores.set(name, new Map());
      const data = stores.get(name);
      return {
        async addAll(requests) {
          for (const request of requests) {
            if (fail && key(request).includes('/assets/')) throw new Error('offline');
            data.set(
              key(request),
              new Response(
                key(request) === origin + '/'
                  ? '<meta name="brrp-build" content="test-build">'
                  : 'asset'
              )
            );
          }
        },
        async match(request) {
          return data.get(key(request))?.clone();
        },
        async put(request, response) {
          data.set(key(request), response.clone());
        },
        async keys() {
          return [...data.keys()].map((url) => new Request(url));
        },
        async delete(request) {
          return data.delete(key(request));
        }
      };
    },
    async keys() {
      return [...stores.keys()];
    },
    async delete(name) {
      return stores.delete(name);
    },
    async match(request) {
      for (const data of stores.values()) {
        const response = data.get(key(request));
        if (response) return response.clone();
      }
    }
  };
  const source = template
    .replace('__BRRP_BUILD__', 'test-build')
    .replace(
      '/*__BRRP_PRECACHE__*/ []',
      JSON.stringify(['/', '/assets/main.js', '/assets/main.css', '/manifest.webmanifest'])
    );
  const context = {
    URL,
    Response,
    Request: class extends Request {
      constructor(url, options) {
        super(new URL(url, origin), options);
      }
    },
    caches,
    fetch: async () => {
      throw new Error('offline');
    },
    self: {
      location: { origin },
      addEventListener: (event, fn) => listeners.set(event, fn),
      skipWaiting: async () => {
        skipped++;
      },
      clients: { claim: async () => {} }
    }
  };
  vm.runInNewContext(source, context);
  async function dispatch(name, event = {}) {
    let work;
    listeners.get(name)({
      ...event,
      waitUntil: (promise) => {
        work = promise;
      }
    });
    await work;
  }
  return {
    stores,
    caches,
    dispatch,
    listeners,
    get skipped() {
      return skipped;
    }
  };
}

test('first install caches startup scripts/styles and opens offline', async () => {
  const value = worker();
  await value.dispatch('install');
  assert.equal(value.skipped, 0, 'install must not force an update');
  let response;
  value.listeners.get('fetch')({
    request: { url: origin + '/', method: 'GET', mode: 'navigate' },
    respondWith: (promise) => {
      response = promise;
    }
  });
  assert.match(await (await response).text(), /test-build/);
  assert.ok(await value.caches.match('/assets/main.js'));
  assert.ok(await value.caches.match('/assets/main.css'));
});

test('a partial installation is discarded and activation requires an explicit message', async () => {
  const failed = worker({ fail: true });
  await assert.rejects(failed.dispatch('install'), /offline/);
  assert.equal(failed.stores.size, 0);
  const value = worker();
  await value.dispatch('install');
  await value.dispatch('message', { data: { type: 'unrelated' } });
  assert.equal(value.skipped, 0);
  await value.dispatch('message', { data: { type: 'ACTIVATE_UPDATE' } });
  assert.equal(value.skipped, 1);
});

test('private API/upload requests bypass the service worker cache', async () => {
  const value = worker();
  let intercepted = false;
  for (const path of ['/api/state', '/api/uploads/private?preview=1', '/ws', '/app-version.json'])
    value.listeners.get('fetch')({
      request: { url: origin + path, method: 'GET', mode: 'navigate' },
      respondWith() {
        intercepted = true;
      }
    });
  assert.equal(intercepted, false);
});

test('activation keeps one previous shell for existing tabs and removes older versions', async () => {
  const value = worker();
  await value.caches.open('brrpbrrp-shell-oldest');
  await value.caches.open('brrpbrrp-shell-previous');
  await value.dispatch('install');
  await value.dispatch('activate');
  assert.deepEqual(await value.caches.keys(), [
    'brrpbrrp-shell-previous',
    'brrpbrrp-shell-test-build'
  ]);
});
