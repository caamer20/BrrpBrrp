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
  // The notice store the worker admits through. Keyed exactly as the worker writes it,
  // so the test can prove that identification and retention use the same prefix.
  const notices = new Map();
  const shown = [];
  const next = (callback) => {
    Promise.resolve().then(callback);
  };
  const store = () => ({
    get(key) {
      const request = {};
      next(() => {
        request.result = notices.get(String(key));
        request.onsuccess?.();
      });
      return request;
    },
    put(value, key) {
      notices.set(String(key), value);
      const request = {};
      next(() => request.onsuccess?.());
      return request;
    },
    delete(key) {
      notices.delete(String(key));
      const request = {};
      next(() => request.onsuccess?.());
      return request;
    },
    getAllKeys() {
      const request = { result: [...notices.keys()] };
      next(() => request.onsuccess?.());
      return request;
    }
  });
  const indexedDB = {
    open() {
      const request = {};
      next(() => {
        request.result = {
          createObjectStore() {},
          transaction: () => ({ objectStore: store, onerror: null, oncomplete: null })
        };
        request.onupgradeneeded?.();
        request.onsuccess?.();
      });
      return request;
    }
  };
  const context = {
    URL,
    Response,
    Request: class extends Request {
      constructor(url, options) {
        super(new URL(url, origin), options);
      }
    },
    caches,
    indexedDB,
    fetch: async () => {
      throw new Error('offline');
    },
    self: {
      location: { origin },
      addEventListener: (event, fn) => listeners.set(event, fn),
      skipWaiting: async () => {
        skipped++;
      },
      clients: { claim: async () => {} },
      registration: {
        showNotification: async (title, options) => {
          shown.push({ title, ...options });
        }
      }
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
    notices,
    shown,
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

test('one event is one notice across a push and an open page', async () => {
  const value = worker();
  await value.dispatch('push', {
    data: { json: () => ({ title: 'server', body: 'mentioned you', message_id: 'm-1' }) }
  });
  assert.equal(value.shown.length, 1, 'the push was not shown');
  // The open page sends its notice through the worker, because the worker owns
  // admission for both paths. A second context, or a push racing the page, must not
  // produce an operating-system banner beside the one already shown.
  await value.dispatch('message', {
    data: {
      type: 'BRRP_NOTIFY',
      notification: { title: 'server', body: 'mentioned you', tag: 'm-1', url: '/?x' }
    }
  });
  assert.equal(value.shown.length, 1, 'the same event was shown twice');
  // A different event still shows.
  await value.dispatch('push', {
    data: { json: () => ({ title: 'server', body: 'mentioned you', message_id: 'm-2' }) }
  });
  assert.equal(value.shown.length, 2, 'a new event was suppressed');
});

test('notice retention writes and sweeps the same prefix, and stays bounded', async () => {
  const value = worker();
  await value.dispatch('push', {
    data: { json: () => ({ title: 'server', body: 'one', message_id: 'm-1' }) }
  });
  assert.ok(value.notices.has('brrp.notified.m-1'), 'the notice stamp used another prefix');

  // An old stamp is swept when the next notice is admitted, so the store holds a day
  // of events and not the life of the installation.
  value.notices.set('brrp.notified.m-1', Date.now() - 25 * 60 * 60 * 1000);
  await value.dispatch('push', {
    data: { json: () => ({ title: 'server', body: 'two', message_id: 'm-2' }) }
  });
  assert.equal(value.notices.has('brrp.notified.m-1'), false, 'an expired notice was retained');
  assert.ok(value.notices.has('brrp.notified.m-2'));
  // A key outside the prefix is not the worker's to remove.
  value.notices.set('unrelated', 1);

  // The count bound: a very busy day cannot grow the store without limit. Entry stamps
  // are seeded directly because a real sweep of that many notices is O(n^2).
  for (let index = 0; index < 510; index++)
    value.notices.set(`brrp.notified.seed-${index}`, Date.now() - index * 1000);
  await value.dispatch('push', {
    data: { json: () => ({ title: 'server', body: 'three', message_id: 'm-3' }) }
  });
  const noticeKeys = [...value.notices.keys()].filter((key) => key.startsWith('brrp.notified.'));
  assert.ok(noticeKeys.length <= 500, `the notice store kept ${noticeKeys.length} entries`);
  assert.equal(value.notices.has('unrelated'), true, 'the sweep removed a foreign key');
  // The newest keys survive the sweep: the bound drops the oldest, not the latest.
  assert.ok(value.notices.has('brrp.notified.m-3'));
  assert.ok(value.notices.has('brrp.notified.seed-0'));
  assert.equal(value.notices.has('brrp.notified.seed-509'), false);
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
