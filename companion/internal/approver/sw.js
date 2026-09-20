// Fylane approver: the service worker exists so the page can be installed as
// a home-screen app. It caches nothing on purpose — a prompt must never be
// served from a cache, and the page itself is small enough to fetch.
self.addEventListener("install", function () { self.skipWaiting(); });
self.addEventListener("activate", function (e) { e.waitUntil(self.clients.claim()); });
