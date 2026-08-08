// Service worker: cache-busting only — forces iOS/Safari to re-fetch
// index.html on every navigation, discovering new hashed JS/CSS assets.
// No offline caching, no push notifications, no wife-hate.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', () => self.clients.claim());
self.addEventListener('fetch', e => {
  // Only intercept navigation requests (page loads) — let all other
  // requests (JS, CSS, API calls) pass through normally.
  if (e.request.mode === 'navigate') {
    e.respondWith(fetch(e.request));
  }
});
