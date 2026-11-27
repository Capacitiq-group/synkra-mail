// Synkra Mail service worker: PWA shell caching ONLY.
// Mail data (/api/*) is NEVER cached — a mail client that serves stale mail
// is a correctness bug, not a feature. The offline outbox lives in the app
// (localStorage + Idempotency-Key replay, src/lib/outbox.ts), not here.
const SHELL = 'synkra-shell-v1'
const SHELL_ASSETS = ['/', '/index.html', '/manifest.webmanifest', '/icon.svg']

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(SHELL).then((c) => c.addAll(SHELL_ASSETS)).then(() => self.skipWaiting())
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== SHELL).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)
  if (event.request.method !== 'GET' || url.pathname.startsWith('/api/')) return // network only
  event.respondWith(
    fetch(event.request)
      .then((res) => {
        if (res.ok && url.origin === self.location.origin) {
          const clone = res.clone()
          caches.open(SHELL).then((c) => c.put(event.request, clone))
        }
        return res
      })
      .catch(() =>
        caches.match(event.request).then(
          (hit) =>
            hit ||
            (event.request.mode === 'navigate' ? caches.match('/index.html') : undefined)
        )
      )
  )
})
