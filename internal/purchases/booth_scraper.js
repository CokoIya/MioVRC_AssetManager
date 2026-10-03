// Runs inside the user's own logged-in accounts.booth.pm page (same origin), reads their
// library / gifts / orders pages and leaves the result in window.__vrclibResult.
// Parsing is structural (links + text) rather than tied to Booth's generated CSS classes.
(() => {
  const P = window.__vrclib = window.__vrclib || {};
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const ITEM = /booth\.pm(?:\/[\w-]+)?\/items\/(\d+)/i;
  const DL = /\/downloadables\/(\d+)/;
  const ORDER = /accounts\.booth\.pm\/orders\/(\d+)|^[^?#]*\/orders\/(\d+)(?:[?#]|$)/;
  const FILE = /\.(zip|rar|7z|unitypackage|blend|fbx|psd|clip|pdf|png|jpe?g|txt|ttf|otf|vrca|mp4|wav|mp3|gz|exe)$/i;
  const DATE = /(20\d{2})\s*[\/\-年.]\s*(\d{1,2})\s*[\/\-月.]\s*(\d{1,2})/;
  const norm = s => String(s || "").replace(/\s+/g, " ").trim();
  const abs = h => { try { return new URL(h, location.origin + "/").href; } catch (e) { return ""; } };
  const hrefOf = el => el.getAttribute("href") || el.getAttribute("data-href") || "";
  const idOf = (h, re) => { const m = abs(h).match(re); return m ? (m[1] || m[2]) : ""; };
  const fmtDate = m => m ? `${m[1]}-${m[2].padStart(2, "0")}-${m[3].padStart(2, "0")}` : "";
  let firstHTML = "";

  async function get(path) {
    for (let i = 0; i < 4; i++) {
      let r;
      try { r = await fetch(path, { credentials: "include", cache: "no-store" }); }
      catch (e) { await sleep(1500 * (i + 1)); continue; }
      try { if (/sign_in|\/login/.test(new URL(r.url).pathname)) throw new Error("LOGIN"); } catch (e) { if (e.message === "LOGIN") throw e; }
      if (r.status === 404) return null;
      if (r.status === 401 || r.status === 403) throw new Error("LOGIN");
      if (r.status === 429 || r.status >= 500) { await sleep(3000 * (i + 1)); continue; }
      if (!r.ok) throw new Error("HTTP " + r.status + " " + path);
      const html = await r.text();
      return new DOMParser().parseFromString(html, "text/html");
    }
    throw new Error("网络不稳定，页面加载失败：" + path);
  }

  function idSet(root, re) {
    const s = new Set();
    root.querySelectorAll("a[href]").forEach(a => { const id = idOf(a.getAttribute("href"), re); if (id) s.add(id); });
    return s;
  }

  // For every link matching re: the largest ancestor that still contains exactly one distinct id.
  function containers(doc, re) {
    const scope = doc.querySelector("main") || doc.body;
    const out = new Map();
    scope.querySelectorAll("a[href]").forEach(a => {
      const id = idOf(a.getAttribute("href"), re); if (!id) return;
      let best = a;
      for (let el = a.parentElement; el && el !== scope.parentElement; el = el.parentElement) {
        const ids = idSet(el, re);
        if (ids.size !== 1 || !ids.has(id)) break;
        best = el;
      }
      const prev = out.get(id);
      if (!prev || best.contains(prev)) out.set(id, best);
    });
    return out;
  }

  function hasNext(doc, page) {
    return [...doc.querySelectorAll("a[href*='page=']")].some(a => {
      const m = (a.getAttribute("href") || "").match(/[?&]page=(\d+)/);
      return m && +m[1] > page;
    });
  }

  function fileNameNear(dl, card) {
    const label = norm(dl.getAttribute("data-label"));
    if (FILE.test(label)) return label;
    let box = dl;
    const dlId = idOf(hrefOf(dl), DL);
    for (let e = dl.parentElement; e && e !== card; e = e.parentElement) {
      const ids = new Set([...e.querySelectorAll("[href*='downloadables'],[data-href*='downloadables']")].map(x => idOf(hrefOf(x), DL)));
      if (ids.size > 1 || (ids.size === 1 && !ids.has(dlId))) break;
      box = e;
    }
    const w = box.ownerDocument.createTreeWalker(box, NodeFilter.SHOW_TEXT);
    for (let n = w.nextNode(); n; n = w.nextNode()) {
      const s = norm(n.nodeValue);
      if (s.length < 240 && FILE.test(s)) return s;
    }
    const t = box.querySelector(".text-14");
    return t ? norm(t.textContent) : "";
  }

  function parseItem(id, el, gift) {
    const it = { id, name: "", shop: "", shopUrl: "", thumb: "", files: [], downloads: [], gift };
    const t = el.querySelector(".text-text-default.font-bold, [class*='font-bold'][class*='break-all']");
    if (t) it.name = norm(t.textContent);
    if (!it.name) el.querySelectorAll("a[href]").forEach(a => {
      if (idOf(a.getAttribute("href"), ITEM) === id) { const s = norm(a.textContent); if (s.length > it.name.length) it.name = s; }
    });
    const img = el.querySelector("img.l-library-item-thumbnail") || el.querySelector("img[src*='pximg']") || el.querySelector("img");
    if (img) {
      it.thumb = abs(img.getAttribute("src") || img.getAttribute("data-src") || "");
      if (!it.name) it.name = norm(img.getAttribute("alt"));
    }
    el.querySelectorAll("a[href]").forEach(a => {
      const h = abs(a.getAttribute("href"));
      if (!it.shopUrl && /^https?:\/\/[\w-]+\.booth\.pm\/?(\?.*)?$/.test(h) && !/^https?:\/\/(accounts|www)\./.test(h)) {
        it.shop = norm(a.textContent); it.shopUrl = h.split("?")[0];
      }
    });
    if (!it.shop) { const c = el.querySelector(".text-14.text-text-gray600, [class*='text-gray600']"); if (c) it.shop = norm(c.textContent); }
    const seen = new Set();
    el.querySelectorAll("[href*='downloadables'],[data-href*='downloadables']").forEach(d => {
      const dl = idOf(hrefOf(d), DL); if (!dl || seen.has(dl)) return;
      seen.add(dl);
      it.downloads.push(dl);
      it.files.push(fileNameNear(d, el));
    });
    return it;
  }

  async function scrapeLibrary(path, gift, base) {
    const items = new Map();
    for (let page = 1; page <= 300; page++) {
      P.stage = gift ? "gifts" : "library"; P.page = page;
      const doc = await get(path + "?page=" + page);
      if (!doc) break;
      if (page === 1 && !gift) firstHTML = doc.documentElement.outerHTML.slice(0, 400000);
      let added = 0;
      for (const [id, el] of containers(doc, ITEM)) {
        if (items.has(id)) continue;
        items.set(id, parseItem(id, el, gift)); added++;
      }
      P.items = base + items.size;
      if (!added || !hasNext(doc, page)) break;
      await sleep(400);
    }
    return [...items.values()];
  }

  async function scrapeOrders(known) {
    const orders = new Map();
    for (let page = 1; page <= 300; page++) {
      P.stage = "orders"; P.page = page;
      const doc = await get("/orders?page=" + page);
      if (!doc) break;
      let added = 0;
      for (const [oid, el] of containers(doc, ORDER)) {
        if (orders.has(oid)) continue;
        added++;
        orders.set(oid, { id: oid, date: fmtDate(norm(el.textContent).match(DATE)), items: [...idSet(el, ITEM)].filter(x => known.has(x)) });
      }
      P.orders = orders.size;
      if (!added || !hasNext(doc, page)) break;
      await sleep(400);
    }
    // only open order pages whose items the list did not show
    const todo = [...orders.values()].filter(o => !o.items.length);
    P.ordersTotal = todo.length; P.orderDone = 0;
    for (const o of todo) {
      P.stage = "orderDetail";
      const doc = await get("/orders/" + o.id);
      if (doc) {
        const scope = doc.querySelector("main") || doc.body;
        o.items = [...idSet(scope, ITEM)].filter(x => known.has(x));
        if (!o.date) o.date = fmtDate(norm(scope.textContent).match(DATE));
      }
      P.orderDone++;
      await sleep(350);
    }
    return [...orders.values()];
  }

  window.__vrclibRun = async () => {
    Object.assign(P, { stage: "start", page: 0, items: 0, orders: 0, ordersTotal: 0, orderDone: 0, done: false, error: "", orderError: "" });
    window.__vrclibResult = null;
    try {
      const library = await scrapeLibrary("/library", false, 0);
      const gifts = await scrapeLibrary("/library/gifts", true, library.length);
      const known = new Set([...library, ...gifts].map(x => x.id));
      let orders = [];
      if (known.size) {
        try { orders = await scrapeOrders(known); }
        catch (e) { if (e.message === "LOGIN") throw e; P.orderError = String(e.message || e); }
      }
      window.__vrclibResult = JSON.stringify({ library, gifts, orders, orderError: P.orderError, debug: known.size ? "" : firstHTML });
      P.stage = "done";
    } catch (e) {
      P.error = String((e && e.message) || e);
    }
    P.done = true;
  };
})();
