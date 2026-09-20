// Fylane approver: the service worker exists so the page can be installed as
// a home-screen app and woken by a push. It caches nothing on purpose — a
// prompt must never be served from a cache, and the page itself is small
// enough to fetch.
self.addEventListener("install", function () { self.skipWaiting(); });
self.addEventListener("activate", function (e) { e.waitUntil(self.clients.claim()); });

// A push says only that something is waiting; the payload is a fixed marker
// and is not even read. What was asked is fetched by the page, sealed to
// this phone, once it opens.
self.addEventListener("push", function (e) {
  var zh = (self.navigator.language || "").toLowerCase().indexOf("zh") === 0;
  e.waitUntil(self.registration.showNotification("Fylane", {
    body: zh ? "有一条待审批" : "An approval is waiting",
    tag: "fylane-approval",
    data: { url: "/approver" }
  }));
});
self.addEventListener("notificationclick", function (e) {
  e.notification.close();
  e.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true }).then(function (list) {
    for (var i = 0; i < list.length; i++) {
      if ("focus" in list[i]) return list[i].focus();
    }
    return self.clients.openWindow("/approver");
  }));
});
