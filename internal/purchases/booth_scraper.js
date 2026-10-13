// Runs inside the user's own logged-in accounts.booth.pm page (same origin), reads their
// library / gifts / orders pages (and, when asked, the items they marked スキ！) and leaves the result in
// window.__vrclibResult. Parsing is structural (links + text) rather than tied to Booth's generated CSS classes.
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
  // the same for a list Booth's own pages read as JSON
  async function getJSON(path) {
    for (let i = 0; i < 4; i++) {
      let r;
      try { r = await fetch(path, { credentials: "include", cache: "no-store", headers: { Accept: "application/json" } }); }
      catch (e) { await sleep(1500 * (i + 1)); continue; }
      try { if (/sign_in|\/login/.test(new URL(r.url).pathname)) throw new Error("LOGIN"); } catch (e) { if (e.message === "LOGIN") throw e; }
      if (r.status === 404) return null;
      // (the pages before it were read in this same login: a refusal here is not taken for a lost login)
      if (r.status === 401 || r.status === 403) throw new Error("Booth 拒绝读取スキ列表（HTTP " + r.status + "）");
      if (r.status === 429 || r.status >= 500) { await sleep(3000 * (i + 1)); continue; }
      if (!r.ok) throw new Error("HTTP " + r.status + " " + path);
      try { return await r.json(); } catch (e) { throw new Error("返回的不是 JSON：" + path); }
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

  // The highest page number the page's own links lead to (0 when it has none).
  function lastPage(doc) {
    let max = 0;
    doc.querySelectorAll("a[href*='page=']").forEach(a => {
      const m = (a.getAttribute("href") || "").match(/[?&]page=(\d+)/);
      if (m && +m[1] > max) max = +m[1];
    });
    return max;
  }

  // What keeps the lists from being whole; with any of it, nothing kept from earlier syncs is dropped.
  const gaps = [];
  const gap = s => { if (gaps.length < 8) gaps.push(s); };

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
    const what = gift ? "礼物列表" : "已购列表";
    let pages = 1, empty = 0;
    for (let page = 1; page <= 300; page++) {
      P.stage = gift ? "gifts" : "library"; P.page = page;
      let doc;
      try { doc = await get(path + "?page=" + page); }
      catch (e) {
        if (e.message === "LOGIN" || (page === 1 && !gift)) throw e;
        gap(what + "第 " + page + " 页未能加载"); // what the other pages give is still worth keeping
        if (++empty >= 3 || page >= pages) break;
        continue;
      }
      if (!doc) {
        if (page > 1 && page <= pages) gap(what + "第 " + page + " 页不存在");
        break;
      }
      if (page === 1 && !gift) firstHTML = doc.documentElement.outerHTML.slice(0, 400000);
      pages = Math.max(pages, lastPage(doc));
      let cards = 0;
      for (const [id, el] of containers(doc, ITEM)) {
        cards++;
        if (!items.has(id)) items.set(id, parseItem(id, el, gift));
      }
      P.items = base + items.size;
      if (page >= pages) break;
      if (!cards) {
        // a page without cards before the last one Booth shows: a page that did not render, not the end
        gap(what + "第 " + page + " 页没有内容");
        if (++empty >= 3) break;
      }
      await sleep(400);
    }
    return [...items.values()];
  }

  async function scrapeOrders(known) {
    const orders = new Map();
    let pages = 1, empty = 0;
    for (let page = 1; page <= 300; page++) {
      P.stage = "orders"; P.page = page;
      const doc = await get("/orders?page=" + page);
      if (!doc) break;
      pages = Math.max(pages, lastPage(doc));
      let added = 0;
      for (const [oid, el] of containers(doc, ORDER)) {
        if (orders.has(oid)) continue;
        added++;
        orders.set(oid, { id: oid, date: fmtDate(norm(el.textContent).match(DATE)), items: [...idSet(el, ITEM)].filter(x => known.has(x)) });
      }
      P.orders = orders.size;
      if (page >= pages || (!added && ++empty >= 3)) break;
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

  // The items the player marked スキ！, across all their スキリスト: the list Booth's own page of them
  // (/wish_lists) reads, a page at a time — {items: [{id, name, price: "¥ 3,000~", url, shop: {name, url},
  // thumbnail_image_urls: [...], ...}], pagination: {current_page, next_page, total_pages, total_count}}.
  async function scrapeLikes() {
    const items = new Map();
    let total = 0;
    for (let page = 1; page <= 250; page++) {
      P.stage = "likes"; P.page = page;
      const j = await getJSON("/wish_list_name_items.json?page=" + page);
      if (!j && page === 1) throw new Error("未找到スキ列表");
      if (!j) break;
      if (!Array.isArray(j.items) || !j.pagination || typeof j.pagination !== "object") throw new Error("スキ列表的格式无法识别");
      for (const x of j.items) {
        const id = String((x && x.id) || "");
        if (!/^\d{1,12}$/.test(id) || items.has(id)) continue;
        const shop = x.shop || {}, thumbs = Array.isArray(x.thumbnail_image_urls) ? x.thumbnail_image_urls : [];
        items.set(id, { id, name: norm(x.name), price: norm(x.price), shop: norm(shop.name), shopUrl: String(shop.url || ""), thumb: String(thumbs[0] || "") });
      }
      total = Math.max(total, +j.pagination.total_count || 0);
      P.likes = items.size;
      if (!j.pagination.next_page || items.size >= 5000) break;
      await sleep(400);
    }
    return { likes: [...items.values()], likesTotal: Math.max(total, items.size) };
  }

  window.__vrclibRun = async opts => {
    opts = opts || {};
    Object.assign(P, { stage: "start", page: 0, items: 0, orders: 0, ordersTotal: 0, orderDone: 0, likes: 0, done: false, error: "", orderError: "" });
    window.__vrclibResult = null;
    gaps.length = 0;
    try {
      const library = await scrapeLibrary("/library", false, 0);
      const gifts = await scrapeLibrary("/library/gifts", true, library.length);
      const known = new Set([...library, ...gifts].map(x => x.id));
      let orders = [];
      if (known.size) {
        try { orders = await scrapeOrders(known); }
        catch (e) { if (e.message === "LOGIN") throw e; P.orderError = String(e.message || e); }
      }
      // a list that cannot be read does not cost the purchases read above
      let likes = { likesRead: false };
      if (opts.likes) {
        try { likes = Object.assign({ likesRead: true }, await scrapeLikes()); }
        catch (e) { likes = { likesRead: false, likesError: String((e && e.message) || e) === "LOGIN" ? "未登录" : String((e && e.message) || e) }; }
      }
      window.__vrclibResult = JSON.stringify(Object.assign({ library, gifts, orders, orderError: P.orderError, incomplete: gaps.join("；"), debug: known.size ? "" : firstHTML }, likes));
      P.stage = "done";
    } catch (e) {
      P.error = String((e && e.message) || e);
    }
    P.done = true;
  };
})();
