// Synkra Mail service worker.
// Strategy:
//  - App shell (HTML, JS, CSS, icons): stale-while-revalidate.
//  - API GETs to /api/v1/me, /mailboxes, /folders, /storage: network-first,
//    fall back to cache when offline. Message bodies are NOT cached — they
//    are large, high-churn and potentially sensitive; offline read of cached
//    bodies is handled in-app via IndexedDB for the mailbox the user opted in.
//  - All other API calls: network only, no caching (mutations, events).
//  - Push notifications: /api/v1/push/subscribe registered from the app.

const VERSION = 'v1.0.0';
const SHELL_CACHE = `synkra-shell-${VERSION}`;
const API_CACHE = `synkra-api-${VERSION}`;
const SHELL_ASSETS = ['/', '/index.html', '/manifest.webmanifest'];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(SHELL_CACHE).then((c) => c.addAll(SHELL_ASSETS)).then(() => self.skipWaiting()),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys
          .filter((k) => k !== SHELL_CACHE && k !== API_CACHE)
          .map((k) => caches.delete(k)),
      ),
    ).then(() => self.clients.claim()),
  );
});

const CACHEABLE_API = [/^\/api\/v1\/me$/, /^\/api\/v1\/mailboxes$/, /^\/api\/v1\/mailboxes\/[^/]+\/folders$/, /^\/api\/v1\/storage$/];

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;

  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  if (url.pathname.startsWith('/api/')) {
    if (!CACHEABLE_API.some((re) => re.test(url.pathname))) return;
    event.respondWith(networkFirst(req));
    return;
  }

  if (req.mode === 'navigate' || url.pathname.endsWith('.js') || url.pathname.endsWith('.css') || url.pathname.endsWith('.svg') || url.pathname.endsWith('.png')) {
    event.respondWith(staleWhileRevalidate(req));
  }
});

async function staleWhileRevalidate(req) {
  const cache = await caches.open(SHELL_CACHE);
  const cached = await cache.match(req);
  const fetchPromise = fetch(req).then((res) => {
    if (res.ok) cache.put(req, res.clone());
    return res;
  }).catch(() => cached);
  return cached || fetchPromise;
}

async function networkFirst(req) {
  const cache = await caches.open(API_CACHE);
  try {
    const res = await fetch(req);
    if (res.ok) cache.put(req, res.clone());
    return res;
  } catch {
    const cached = await cache.match(req);
    if (cached) return cached;
    return new Response(JSON.stringify({ error: { code: 'offline', message: 'Offline' } }), {
      status: 503,
      headers: { 'Content-Type': 'application/json' },
    });
  }
}

self.addEventListener('push', (event) => {
  let payload = { title: 'Synkra Mail', body: 'New message', tag: 'synkra' };
  try {
    if (event.data) payload = { ...payload, ...event.data.json() };
  } catch { /* ignore malformed */ }
  event.waitUntil(
    self.registration.showNotification(payload.title, {
      body: payload.body,
      tag: payload.tag,
      icon: '/icons/icon-192.png',
      badge: '/icons/icon-192.png',
      data: payload.data || {},
    }),
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const url = event.notification.data?.url || '/';
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
      for (const c of clients) {
        if ('focus' in c) { c.navigate(url); return c.focus(); }
      }
      return self.clients.openWindow(url);
    }),
  );
});
