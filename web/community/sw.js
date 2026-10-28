// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// The community page's service worker: the page from this phone or computer at once, and the service's notices.
// The service writes its build into VERSION as it serves this file, so every new build installs again:
// - the page's own files are kept per build and answered from here first (a new build fetches them anew, then the
//   old ones go);
// - pictures (/pic/, and the pictures under /repo/) are kept in a cache of their own, the newest 400;
// - nothing of /api/ is kept here (web.js keeps the last answers the page needs);
// - a push from the service shows its notice; a click on one opens the page at what it is about.
"use strict";
const VERSION = "__MIOVRCA_WEB_VERSION__";
const PAGE = "page-" + VERSION, PICS = "pics", PICS_MAX = 400;
const DEV = VERSION.startsWith("__"); // (served as it is, without a build: the page's files always from the network)
const SHELL = ["./", "boot.js", "app.css", "social.css", "web.css", "i18n.js", "theme.js", "adapter.js", "web.js", "nsfw.js", "social.js",
  "web-start.js", "manifest.webmanifest", "icon.svg", "icon-180.png", "icon-192.png", "badge-72.png"];

self.addEventListener("install", e => {
  e.waitUntil((DEV ? Promise.resolve() : caches.open(PAGE).then(c => c.addAll(SHELL.map(u => new Request(u, { cache: "no-cache" }))))).then(() => self.skipWaiting()));
});
self.addEventListener("activate", e => {
  e.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(k => k.startsWith("page-") && k !== PAGE).map(k => caches.delete(k))))
    .then(() => self.clients.claim()));
});

self.addEventListener("fetch", e => {
  const r = e.request, url = new URL(r.url);
  if (r.method !== "GET" || url.origin !== self.location.origin) return;
  const path = url.pathname;
  if (path.startsWith("/api/") || path.startsWith("/admin") || path.startsWith("/p/") || path === "/sw.js" || path === "/privacy.html" ||
    path === "/booth" || path.startsWith("/booth/") || path.startsWith("/zh/") || path === "/sitemap.xml" || path === "/robots.txt") return; // (pages of their own: the policy, Booth 情报's pages)
  if (path.startsWith("/pic/") || path.startsWith("/repo/") && /\.(jpe?g|png|webp|gif)$/i.test(path)) { e.respondWith(picture(r)); return; }
  if (path.startsWith("/repo/") || DEV) return;
  if (r.mode === "navigate") { e.respondWith(fromPage(new Request("./"), r)); return; }
  e.respondWith(fromPage(r, r));
});
// the page's files: this build's copy, else the network (and kept for next time)
async function fromPage(key, r) {
  const cache = await caches.open(PAGE);
  const hit = await cache.match(key, { ignoreSearch: true });
  if (hit) return hit;
  const res = await fetch(r);
  if (res.ok && res.type === "basic") cache.put(key, res.clone());
  return res;
}
async function picture(r) {
  const cache = await caches.open(PICS);
  const hit = await cache.match(r);
  if (hit) return hit;
  const res = await fetch(r);
  if (res.ok && res.type === "basic") {
    await cache.put(r, res.clone());
    const keys = await cache.keys();
    for (const k of keys.slice(0, Math.max(0, keys.length - PICS_MAX))) await cache.delete(k); // (oldest first)
  }
  return res;
}

// ---------- notices ----------
// the service's payload: {title, body, url, tag, kind, badge}
self.addEventListener("push", e => {
  let d = {};
  try { d = e.data ? e.data.json() : {}; } catch (er) { d = { title: e.data ? e.data.text() : "" }; }
  const title = d.title || "MioVRCA";
  const shown = self.registration.showNotification(title, {
    body: d.body || "", tag: d.tag || undefined, renotify: !!d.tag, icon: "icon-192.png", badge: "badge-72.png",
    data: { url: d.url || "/#/notices" }, timestamp: Date.now(),
  });
  const badge = typeof d.badge === "number" && self.navigator.setAppBadge ? (d.badge > 0 ? self.navigator.setAppBadge(d.badge) : self.navigator.clearAppBadge()) : null;
  const tell = self.clients.matchAll({ type: "window" }).then(list => list.forEach(c => c.postMessage({ type: "push", kind: d.kind || "" })));
  e.waitUntil(Promise.all([shown, Promise.resolve(badge).catch(() => {}), tell]));
});
self.addEventListener("notificationclick", e => {
  e.notification.close();
  const target = new URL((e.notification.data && e.notification.data.url) || "/#/notices", self.location.origin).href;
  e.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true }).then(list => {
    const page = list.find(c => new URL(c.url).origin === self.location.origin);
    if (page) { page.postMessage({ type: "open", url: target }); return page.focus(); }
    return self.clients.openWindow(target);
  }));
});
