const BUILD = '__BRRP_BUILD__';
const PRECACHE = /*__BRRP_PRECACHE__*/ [];
const CACHE = `brrpbrrp-shell-${BUILD}`;
const RUNTIME = 'brrpbrrp-runtime-assets';

self.addEventListener('install', (event) => {
  event.waitUntil(
    (async () => {
      try {
        const cache = await caches.open(CACHE);
        await cache.addAll(PRECACHE.map((url) => new Request(url, { cache: 'reload' })));
        const page = await cache.match('/');
        // A deployment that changes mid-install must not produce a mixed offline shell.
        if (!(await page?.text())?.includes(`name="brrp-build" content="${BUILD}"`))
          throw new Error('The app changed during installation.');
      } catch (error) {
        await caches.delete(CACHE);
        throw error;
      }
    })()
  );
});

// Existing pages choose when to refresh. A first install activates normally.
self.addEventListener('message', (event) => {
  if (event.data?.type === 'ACTIVATE_UPDATE') event.waitUntil(self.skipWaiting());
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      const names = await caches.keys();
      const previous = names
        .filter((name) => name.startsWith('brrpbrrp-shell-') && name !== CACHE)
        .at(-1);
      await Promise.all(
        names
          .filter(
            (name) => name.startsWith('brrpbrrp-shell-') && name !== CACHE && name !== previous
          )
          .map((name) => caches.delete(name))
      );
      await self.clients.claim();
    })()
  );
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);
  if (event.request.method !== 'GET' || url.origin !== self.location.origin) return;
  // Private conversations, uploads and API responses never enter a browser cache.
  if (
    url.pathname.startsWith('/api/') ||
    url.pathname === '/ws' ||
    url.pathname === '/app-version.json'
  )
    return;
  if (event.request.mode === 'navigate') {
    event.respondWith(
      (async () => {
        try {
          const response = await fetch(event.request);
          if (response.ok && response.headers.get('content-type')?.includes('text/html')) {
            const page = await response.clone().text();
            if (page.includes(`name="brrp-build" content="${BUILD}"`)) {
              try {
                await (await caches.open(CACHE)).put('/', response.clone());
              } catch {
                /* Online use still works when storage is full. */
              }
            }
          }
          return response;
        } catch {
          return (
            (await (await caches.open(CACHE)).match('/')) ||
            new Response('Your server is offline. Please reconnect.', {
              status: 503,
              headers: { 'Content-Type': 'text/plain' }
            })
          );
        }
      })()
    );
    return;
  }
  if (url.pathname.startsWith('/assets/') || PRECACHE.includes(url.pathname)) {
    event.respondWith(
      (async () => {
        const saved = await caches.match(event.request);
        if (saved) return saved;
        const response = await fetch(event.request);
        if (response.ok && url.pathname.startsWith('/assets/')) {
          try {
            const cache = await caches.open(RUNTIME);
            await cache.put(event.request, response.clone());
            const keys = await cache.keys();
            for (const old of keys.slice(0, -32)) await cache.delete(old);
          } catch {
            /* Caching is optional while online. */
          }
        }
        return response;
      })()
    );
  }
});

self.addEventListener('push', (event) => {
  let notification;
  try {
    notification = event.data?.json();
  } catch {
    return;
  }
  if (!notification) return;
  event.waitUntil(
    (async () => {
      // The server supplies `tag`, and the previous code read `message_id` — a field
      // the payload does not have. Every notification therefore arrived with
      // `tag: undefined`, which is not deduplication at all: two devices, or one
      // device told twice about the same event, produced two notices.
      const tag = notification.tag || notification.message_id || '';
      // Retained across tabs and the worker, per account, for a day. Server-side
      // uniqueness is one row per source event; it is not one notice per device, and
      // it cannot know about a second tab that already showed the notice.
      const key = `brrp.notified.${notification.account_id || ''}.${tag}`;
      if (tag) {
        try {
          const seen = await seenRecently(key);
          if (seen) return;
        } catch {
          // A storage failure must not swallow a notification: the notice is worth
          // showing twice, and a missing key only risks that.
        }
      }
      await self.registration.showNotification(notification.title || 'BrrpBrrp', {
        body: notification.body || 'You were mentioned.',
        icon: '/icons/icon-192.png',
        badge: '/icons/icon-192.png',
        // A tag is what makes a second notice *replace* the first rather than stack
        // beside it, and what lets the same event on two devices collapse to one.
        ...(tag ? { tag } : {}),
        data: { url: notification.url || '/' }
      });
      if (tag) {
        try {
          await remember(key);
        } catch {
          // Recorded-or-not does not change what was already shown.
        }
      }
    })()
  );
});

// The retention is a day because a catch-up notice for something that happened
// yesterday is still worth seeing, and one from last week is not: a member who has
// been away should find recent activity, not a week of stale ones.
const NOTIFY_RETENTION_MS = 24 * 60 * 60 * 1000;
const NOTIFY_PREFIX = 'brrp.notify.';

async function seenRecently(key) {
  const at = await readStore(key);
  return typeof at === 'number' ? Date.now() - at < NOTIFY_RETENTION_MS : false;
}

async function remember(key) {
  await writeStore(key, Date.now());
  // A bounded sweep rather than an expiry the browser may or may not honour.
  const keys = (await listStore()).filter((name) => name.startsWith(NOTIFY_PREFIX));
  for (const name of keys) {
    const at = await readStore(name);
    if (typeof at !== 'number' || Date.now() - at >= NOTIFY_RETENTION_MS) await deleteStore(name);
  }
}

// Storage is best-effort: a browser that refuses IndexedDB still gets notifications,
// which is the right trade. Blocking storage is best-effort in the other direction.
function openStore() {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open('brrp-notifications', 1);
    request.onupgradeneeded = () => request.result.createObjectStore('seen');
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
    request.onblocked = () => reject(new Error('notification store blocked'));
  });
}
async function readStore(key) {
  const store = await openStore();
  return new Promise((resolve, reject) => {
    const request = store.transaction('seen').objectStore('seen').get(key);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}
async function writeStore(key, value) {
  const store = await openStore();
  return new Promise((resolve, reject) => {
    const request = store.transaction('seen', 'readwrite').objectStore('seen').put(value, key);
    request.onsuccess = () => resolve();
    request.onerror = () => reject(request.error);
  });
}
async function deleteStore(key) {
  const store = await openStore();
  return new Promise((resolve, reject) => {
    const request = store.transaction('seen', 'readwrite').objectStore('seen').delete(key);
    request.onsuccess = () => resolve();
    request.onerror = () => reject(request.error);
  });
}
async function listStore() {
  const store = await openStore();
  return new Promise((resolve, reject) => {
    const keys = [];
    const request = store.transaction('seen').objectStore('seen').getAllKeys();
    request.onsuccess = () => {
      for (const key of request.result) keys.push(String(key));
      resolve(keys);
    };
    request.onerror = () => reject(request.error);
  });
}
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = new URL(event.notification.data?.url || '/', self.location.origin);
  if (target.origin !== self.location.origin) return;
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(async (windows) => {
      const existing = windows.find((window) => new URL(window.url).origin === target.origin);
      if (existing) {
        await existing.navigate(target.href);
        return existing.focus();
      }
      return self.clients.openWindow(target.href);
    })
  );
});
