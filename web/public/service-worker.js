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
    self.registration.showNotification(notification.title || 'BrrpBrrp', {
      body: notification.body || 'You were mentioned.',
      icon: '/icons/icon-192.png',
      badge: '/icons/icon-192.png',
      tag: notification.message_id,
      data: { url: notification.url || '/' }
    })
  );
});
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
