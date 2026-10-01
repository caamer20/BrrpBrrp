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
//
// The same channel carries a foreground notice from an open page. The worker owns
// notification admission for both paths, so a page notice and a push arriving for the
// same event claim the same key and only one of them shows — which is what stops the
// operating system banner a member sees while the tab is already open, and what stops
// two open tabs from each showing one.
self.addEventListener('message', (event) => {
  if (event.data?.type === 'ACTIVATE_UPDATE') {
    event.waitUntil(self.skipWaiting());
    return;
  }
  if (event.data?.type === 'BRRP_NOTIFY') {
    event.waitUntil(showNotificationOnce(event.data.notification));
  }
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
  event.waitUntil(showNotificationOnce(notification));
});

// One admission point for every notice, from a push or from an open page.
//
// The event's own identity is `tag` (falling back to `message_id`, which is what the
// mention payload carries); it is what the operating system also uses to replace a
// notice rather than stack it. Admission is one IndexedDB transaction: it reads the
// stored stamp and writes the new one together, so two contexts racing the same event
// cannot both decide to show it. A read-then-show-then-remember sequence could, and
// did — one notice per tab, plus another for the push.
async function showNotificationOnce(notification) {
  const tag = notification?.tag || notification?.message_id || '';
  if (tag) {
    try {
      if (!(await claimNotice(noticeKey(tag)))) return;
    } catch {
      // A storage failure must not swallow a notification: the notice is worth
      // showing twice, and a missing key only risks that.
    }
  }
  await self.registration.showNotification(notification?.title || 'BrrpBrrp', {
    body: notification?.body || 'You were mentioned.',
    icon: '/icons/icon-192.png',
    badge: '/icons/icon-192.png',
    // A tag is what makes a second notice *replace* the first rather than stack
    // beside it, and what lets the same event on two devices collapse to one.
    ...(tag ? { tag } : {}),
    data: { url: notification?.url || '/' }
  });
  if (tag) {
    try {
      await sweepNotices();
    } catch {
      // Recorded-or-not does not change what was already shown.
    }
  }
}

// The retention is a day because a catch-up notice for something that happened
// yesterday is still worth seeing, and one from last week is not: a member who has
// been away should find recent activity, not a week of stale ones. The count bound is
// the second half: a very busy day cannot grow the store without limit, and the
// oldest keys go first.
const NOTIFY_RETENTION_MS = 24 * 60 * 60 * 1000;
const NOTIFY_MAX = 500;
// One prefix for writing and for cleanup. They used to differ — keys were written as
// `brrp.notified.…` and swept by a `brrp.notify.…` filter — so nothing was ever
// reclaimed and the store grew for the life of the installation.
const NOTIFY_PREFIX = 'brrp.notified.';
const noticeKey = (tag) => `${NOTIFY_PREFIX}${tag}`;

// claimNotice atomically takes one event. It resolves true when this context may show
// the notice and false when another context already did.
async function claimNotice(key) {
  const store = await openStore();
  return new Promise((resolve, reject) => {
    const tx = store.transaction('seen', 'readwrite');
    const object = tx.objectStore('seen');
    const read = object.get(key);
    read.onerror = () => reject(read.error);
    read.onsuccess = () => {
      const at = read.result;
      if (typeof at === 'number' && Date.now() - at < NOTIFY_RETENTION_MS) {
        resolve(false);
        return;
      }
      const write = object.put(Date.now(), key);
      write.onerror = () => reject(write.error);
      write.onsuccess = () => resolve(true);
    };
    tx.onerror = () => reject(tx.error);
  });
}

async function sweepNotices() {
  const keys = (await listStore()).filter((name) => name.startsWith(NOTIFY_PREFIX));
  const stamped = [];
  for (const name of keys) {
    const at = await readStore(name);
    if (typeof at !== 'number' || Date.now() - at >= NOTIFY_RETENTION_MS) await deleteStore(name);
    else stamped.push({ name, at });
  }
  stamped.sort((left, right) => left.at - right.at);
  for (const entry of stamped.slice(0, Math.max(0, stamped.length - NOTIFY_MAX)))
    await deleteStore(entry.name);
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
