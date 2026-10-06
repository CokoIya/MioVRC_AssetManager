// the wish list with price watching, the followed shops, and other shops
"use strict";

// ---------- 愿望单 ----------
// Booth items the player means to buy. The program looks at their prices once a day (booth/wishlist.go); this
// is the list in the Booth tab, the button on a search result and in the details panel, and the notice of a
// lower price. What it shows comes from /api/stores/state, asked for again whenever the state's revision moves.
const W = { on: false, items: [], byKey: new Map(), unseen: 0, watch: true, paused: false, busy: false, sig: "",
  sort: "drop", paste: "", fresh: new Set(), histOpen: new Set(), adding: false };
// Jinxxy lives in the 闲鱼 tab (a sixth tab does not fit the header at 720 px): site = whose pages that tab shows
const JX = { site: "xianyu", base: "https://jinxxy.com", search: "", q: { xianyu: "", jinxxy: "" }, files: [], told: null, saved: "", fsig: "", lsig: "" };
const WISH_ICON = '<svg viewBox="0 0 24 24"><path d="M12 20.5s-7.5-4.6-7.5-10.2A4.3 4.3 0 0 1 12 7.6a4.3 4.3 0 0 1 7.5 2.7c0 5.6-7.5 10.2-7.5 10.2z"/></svg>';
try {
  const saved = JSON.parse(localStorage.getItem("vrclib.stores") || "{}");
  if (saved.site === "jinxxy") JX.site = "jinxxy";
  if (["drop", "added", "price"].includes(saved.sort)) W.sort = saved.sort;
  if (saved.q) { JX.q.xianyu = String(saved.q.xianyu || ""); JX.q.jinxxy = String(saved.q.jinxxy || ""); }
  if (typeof saved.jxLast === "string" && webKind(saved.jxLast) !== "xianyu") { S.web.last.jinxxy = saved.jxLast; JX.saved = saved.jxLast; }
  // S.web.xq is the query of the site the tab shows; app.js restored 闲鱼's
  if (JX.site === "jinxxy") { JX.q.xianyu = S.web.xq; S.web.xq = JX.q.jinxxy; if (S.view === "xianyu") $("#q").value = S.web.xq; }
} catch (e) {}
function storesSave() {
  JX.q[JX.site] = S.web.xq; JX.saved = S.web.last.jinxxy || "";
  try { localStorage.setItem("vrclib.stores", JSON.stringify({ site: JX.site, sort: W.sort, q: JX.q, jxLast: JX.saved })); } catch (e) {}
}

// called by load() after every state refresh; the first answer is waited for (the tab may open on Jinxxy)
let storesFirst = null;
function storesLoaded() {
  const p = storesFetch().catch(e => console.error(e)); // (a fault here must not look like a lost connection to load()'s caller)
  if (!storesFirst) { storesFirst = p; return p; }
}
async function storesFetch() {
  let r;
  try { r = await api("/api/stores/state", {}); } catch (e) { return; }
  if (!r || !r.ok) return;
  if (r.jinxxy) { JX.base = r.jinxxy.base; JX.search = r.jinxxy.search; }
  jxFiles(r.files || []);
  const changed = wishTake(r.wish || {}), fchanged = followTake(r.follow || {});
  if (!(changed || fchanged) || S.view !== "shop") return;
  renderShopSide(); renderShopGrid();
  if (S.shopOpen && S.shop.detail) renderShopDrawer();
}
function wishTake(w) {
  const sig = JSON.stringify(w), changed = sig !== W.sig;
  W.sig = sig; W.items = w.items || []; W.byKey = new Map(W.items.map(x => [x.key, x]));
  W.unseen = w.unseen || 0; W.watch = w.watch !== false; W.paused = !!w.paused; W.busy = !!w.busy;
  wishAnnounce();
  return changed;
}
// a lower price is told once: a notice that leads to the list (the entry in the side bar keeps its dot until then)
function wishAnnounce() {
  const news = W.items.filter(x => x.change && !x.change.told && (x.change.kind === "drop" || x.change.kind === "free"));
  if (!news.length) return;
  const at = {};
  for (const x of news) { at[x.key] = x.change.at; x.change.told = true; }
  api("/api/wish/told", { at }).catch(() => {});
  const x = news[0], c = x.change;
  toast(news.length > 1 ? "愿望单中有多件商品降价" : (c.kind === "free" ? "愿望单商品变为免费：" : "愿望单商品降价：") + x.title + "　" + c.old + " → " + c.new, 9000, wishOpen);
}
function wishHas(id) { return W.byKey.has("booth:" + id); }
// asked by renderShopGrid(); the header's sort box is for the Booth results, so it goes while the list is shown
function wishOn() { $("#shopSort").hidden = W.on; return W.on; }
// told by shopSearch(): a search or a filter shows the Booth results again
function wishLeave() { if (!W.on) return; W.on = false; if (S.view === "shop") renderShopSide(); }
function wishSeen() {
  for (const x of W.items) if (x.unseen) { W.fresh.add(x.key); x.unseen = false; }
  W.unseen = 0;
  api("/api/wish/seen", {}).catch(() => {});
}
function wishOpen() {
  if (S.view !== "shop") setView("shop"); // (starts the first Booth search, which leaves the list: so before W.on)
  W.on = true; W.fresh = new Set(); F.on = false;
  if (S.web.open.shop) closeWeb();
  closeDrawer(); sideOpen(false);
  renderShopSide(); renderShopGrid(); $(".main").scrollTop = 0;
}
function wishClose() { W.on = false; renderShopSide(); renderShopGrid(); $(".main").scrollTop = 0; }

// the entry at the top of the Booth tab's side bar
function wishSideHTML() {
  return `<div class="sidelinks wlnav"><button class="navitem${W.on ? " on" : ""}" data-wl="nav" aria-pressed="${W.on}">${WISH_ICON}<span class="nm">愿望单</span>${W.unseen ? `<span class="wldot" title="有新的价格变化"></span>` : ""}<span class="n">${esc(W.items.length)}</span></button></div>`;
}
// the other shops, at the end of it
function storesSideHTML() {
  return `<h3>其他店铺</h3><div class="sidelinks two"><button class="navitem" data-jx="go" data-site="xianyu"><span>闲鱼</span></button><button class="navitem" data-jx="go" data-site="jinxxy"><span>Jinxxy</span></button></div>`;
}
// on a search result
function wishCardBtn(it) {
  const on = wishHas(it.id);
  if (it.bought && !on) return "";
  const tip = on ? "已在愿望单，点击移除" : "加入愿望单";
  return `<button class="wlbtn${on ? " on" : ""}" data-wl="toggle" data-id="${esc(it.id)}" aria-pressed="${on}" title="${tip}" aria-label="${tip}">${WISH_ICON}</button>`;
}
// in the details panel
function wishDrawerBtn(id, bought) {
  const on = wishHas(id);
  if (bought && !on) return "";
  return `<button class="btn wltoggle${on ? " on" : ""}" data-wl="toggle" data-id="${esc(id)}" aria-pressed="${on}" title="${on ? "点击从愿望单移除" : "加入后每天检查一次价格"}">${WISH_ICON}<span>${on ? "已在愿望单" : "加入愿望单"}</span></button>`;
}

const WISH_KIND = { drop: "降价", free: "变为免费", rise: "涨价", off: "不可购买", gone: "已下架", back: "恢复上架" };
function wishSorted() {
  const by = {
    added: (a, b) => b.added - a.added,
    price: (a, b) => (a.priceNum < 0) - (b.priceNum < 0) || a.priceNum - b.priceNum || b.added - a.added,
    drop: (a, b) => a.pct - b.pct || b.added - a.added,
  }[W.sort];
  return W.items.slice().sort(by);
}
function wishState(x) {
  if (x.bought) return `<span class="wltag bought">已购</span>`;
  if (x.state === "gone") return `<span class="wltag gone">已下架</span>`;
  if (x.state === "off") return `<span class="wltag off">不可购买</span>`;
  if (x.priceNum === 0) return `<span class="wltag ok">免费</span>`;
  return `<span class="wltag">在售</span>`;
}
function wishRowHTML(x) {
  const c = x.change, hist = x.hist || [];
  const change = c ? `<div class="wlchange ${esc(c.kind)}${W.fresh.has(x.key) ? " fresh" : ""}"><span class="k">${esc(WISH_KIND[c.kind] || "")}</span>${c.var ? `<span class="var">${esc(c.var)}</span>` : ""}${
    c.old && c.new && c.old !== c.new ? `<span class="v">${esc(c.old)} → ${esc(c.new)}</span>` : ""}<span class="t">${esc(fmtTime(c.at))}</span></div>` : "";
  // a variation's own price record is its tooltip (one line a change)
  const vtip = v => (v.off ? ["缺货"] : []).concat((v.hist || []).length > 1 ? v.hist.slice().reverse().map(h => fmtTime(h.at) + "　" + h.price) : []).join("\n");
  const vars = (x.vars || []).length ? `<div class="wlvars">${x.vars.map(v => `<span${v.off ? ` class="out"` : ""} title="${esc(vtip(v))}">${esc(v.name || "默认")}<b>${esc(v.price)}</b></span>`).join("")}</div>` : "";
  const histHTML = hist.length > 1 ? `<details class="wlhist" data-wlhist="${esc(x.key)}"${W.histOpen.has(x.key) ? " open" : ""}><summary>价格记录</summary>${
    hist.slice().reverse().map(h => `<div><span class="t">${esc(fmtTime(h.at))}</span><b>${esc(h.price)}</b>${h.state ? `<span class="s">${h.state === "gone" ? "已下架" : "不可购买"}</span>` : ""}</div>`).join("")}</details>` : "";
  const diff = x.diffDir ? `<div class="wldiff ${x.diffDir < 0 ? "down" : "up"}"><span class="lb">较加入时</span><b>${x.diffDir < 0 ? "−" : "+"}${esc(x.diff)}</b></div>`
    : x.first ? `<div class="wldiff"><span class="lb">较加入时</span><b>无变化</b></div>` : "";
  const low = x.low && x.low !== x.price ? `<div class="wllow"><span class="lb">最低</span><b>${esc(x.low)}</b></div>` : "";
  return `<article class="wlrow${x.bought || x.state ? " dim" : ""}" data-wlkey="${esc(x.key)}">
    <button class="wlthumb" data-wl="detail" data-id="${esc(x.id)}" title="查看商品详情" aria-label="查看商品详情">${x.thumb ? `<img loading="lazy" decoding="async" src="/rthumb?u=${encodeURIComponent(x.thumb)}" alt="">` : ""}</button>
    <div class="wlmain">
      <button class="wltitle" data-wl="detail" data-id="${esc(x.id)}" data-i18n="off">${esc(x.title || "Booth #" + x.id)}</button>
      <div class="wlsub"><span>${esc(x.shop || "")}</span><span class="t"><span>加入于</span> ${esc(fmtTime(x.added))}</span></div>
      ${vars}${change}
      ${x.note ? `<div class="wlnote"><span class="lb">备注</span><span data-i18n="off">${esc(x.note)}</span></div>` : ""}
      ${x.err ? `<div class="wlerr small err"><span>价格检查失败</span>　${esc(x.err)}</div>` : ""}
      ${histHTML}
    </div>
    <div class="wlprice" title="${x.checked ? "上次检查：" + esc(fmtTime(x.checked)) : "尚未检查"}">
      <div class="now"><b>${esc(x.price || "—")}</b>${x.from ? `<span class="from">起</span>` : ""}</div>${diff}${low}
    </div>
    <div class="wlstate">${wishState(x)}</div>
    <div class="wlacts">
      <button class="btn small" data-wl="open" data-id="${esc(x.id)}">在 Booth 中打开</button>
      <button class="wb" data-wl="note" data-id="${esc(x.id)}" title="编辑备注" aria-label="编辑备注">${ICON.edit}</button>
      <button class="wb" data-wl="remove" data-id="${esc(x.id)}" title="从愿望单移除" aria-label="从愿望单移除">${ICON.x}</button>
    </div></article>`;
}
// drawn in place of the Booth results. The paste field and the list are separate parts: a refresh in the
// background redraws the list only when it changed, and never the field being typed into.
function renderWish() {
  if (W.unseen && !S.web.open.shop) { wishSeen(); renderShopSide(); } // the list is in view: what changed is seen
  const n = W.items.length;
  const opt = (v, label) => `<option value="${v}"${W.sort === v ? " selected" : ""}>${label}</option>`;
  put($("#resultbar"), `<b>${esc(n)}</b><span>件愿望单商品</span>
    <span class="wlbar"><label class="wlsort">排序<select id="wl_sort">${opt("drop", "降幅")}${opt("added", "加入时间")}${opt("price", "价格")}</select></label>
    <label class="check"><input type="checkbox" id="wl_watch"${W.watch ? " checked" : ""}> 每天检查价格</label>
    <button class="btn small" data-wl="check">立即检查</button>
    <button class="btn small" data-wl="nav">返回 Booth 商品</button></span>`);
  // (changed in place: a round that starts or ends must not redraw the sort box under the pointer)
  const cb = $('#resultbar [data-wl="check"]'), label = W.busy ? "正在检查…" : "立即检查";
  cb.disabled = W.busy || !n;
  if (cb.textContent !== label) cb.textContent = label;
  if (!$("#wl")) {
    gridPut(`<div class="wl" id="wl"><div class="wladd"><input id="wl_paste" autocomplete="off" placeholder="粘贴 Booth 商品链接或商品编号" aria-label="Booth 商品链接或商品编号"><button class="btn" data-wl="add">加入愿望单</button></div>
      <div id="wl_top"></div><div id="wl_list"></div><p class="wlfoot">价格取自 Booth 商品页，按店铺所用货币显示，未作换算。</p></div>`);
    $("#wl_paste").value = W.paste;
  }
  put($("#wl_top"), W.paused ? `<p class="wlpause">Booth 暂时限制了访问，价格检查将稍后自动继续。</p>` : !W.watch && n ? `<p class="wlpause">已关闭每天的价格检查，价格不会自动更新。</p>` : "");
  const list = $("#wl_list");
  if (!n) { put(list, `<div class="empty"><h2>愿望单为空</h2><p>在此收藏想购买的 Booth 商品，软件每天检查一次价格，降价时提醒。</p></div>`); return; }
  // row by row: one item's price being looked at does not redraw the others (nor the one a button is being pressed in)
  const rows = wishSorted().map(x => [x.key, wishRowHTML(x)]), kids = [...list.children], refocus = focusMark(list);
  if (kids.length === rows.length && kids.every((k, i) => k.dataset.wlkey === rows[i][0])) {
    rows.forEach(([, html], i) => {
      if (kids[i]._h === html) return;
      const t = document.createElement("template"); t.innerHTML = html;
      const row = t.content.firstElementChild; row._h = html; kids[i].replaceWith(row);
    });
  } else {
    list._h = null; list.innerHTML = rows.map(r => r[1]).join("");
    [...list.children].forEach((k, i) => { k._h = rows[i][1]; });
  }
  refocus();
}
async function wishAdd(body) {
  if (W.adding) return false;
  W.adding = true;
  let r;
  try { r = await api("/api/wish/add", body); } catch (e) { r = { ok: false, err: "程序内部错误" }; } finally { W.adding = false; }
  if (!r.ok) { toast(r.err || "加入愿望单失败", 4000); return false; }
  toast("已加入愿望单");
  await storesFetch();
  return true;
}
async function wishRemove(key) {
  await api("/api/wish/remove", { key });
  toast("已从愿望单移除");
  await storesFetch();
}
// what the window already shows of an item goes along, so it can be listed even when Booth does not answer
function wishSeed(id) {
  const d = S.shopOpen === id ? (S.shop.detail || {}) : {}, it = d.item, hit = S.shop.items.find(x => x.id === id) || d.hit || {};
  return { id, title: (it && it.name) || hit.name || "", shop: (it && it.shop) || hit.shop || hit.shopSub || "",
    thumb: hit.thumb || (it && (it.images || [])[0]) || "", price: (it && it.price) || hit.price || "" };
}
function wishNoteOpen(key) {
  const x = W.byKey.get(key); if (!x) return;
  const m = $("#modal");
  m.dataset.kind = "wlnote";
  m.innerHTML = `<div class="mhead">编辑备注</div><div class="mbody">
    <p class="lead">${esc(x.title)}</p>
    <div class="row"><label for="wl_note">备注</label><textarea id="wl_note" maxlength="500" placeholder="例如：需要搭配的素体、打算购买的款式">${esc(x.note)}</textarea></div>
  </div><div class="mfoot"><button class="btn ghost" data-m="close">取消</button><button class="btn primary" data-wl="notesave" data-key="${esc(key)}">保存</button></div>`;
  showModal();
  $("#wl_note").focus();
}
async function wishAction(b) {
  const row = b.closest("[data-wlkey]"), key = row ? row.dataset.wlkey : "", id = b.dataset.id || "";
  switch (b.dataset.wl) {
    case "nav": return W.on && S.view === "shop" && !S.web.open.shop ? wishClose() : wishOpen();
    case "toggle": {
      const card = b.classList.contains("wlbtn");
      if (wishHas(id)) await wishRemove("booth:" + id); else await wishAdd(wishSeed(id));
      // the results were drawn again with the button in its new state: the focus goes back onto it
      const nb = card ? $(`.shopcard .wlbtn[data-id="${CSS.escape(id)}"]`) : null;
      if (nb && document.activeElement === document.body) nb.focus({ preventScroll: true });
      return;
    }
    case "add": {
      const text = $("#wl_paste").value.trim();
      if (!text) { $("#wl_paste").focus(); return; }
      b.disabled = true;
      const ok = await wishAdd({ text });
      b.disabled = false;
      if (ok && $("#wl_paste")) { W.paste = ""; $("#wl_paste").value = ""; }
      return;
    }
    case "detail": return openShopItem(id);
    case "open": return openWeb(boothItemURL(id), "booth");
    case "note": return wishNoteOpen(key);
    case "notesave": {
      const r = await api("/api/wish/note", { key: b.dataset.key, note: $("#wl_note").value });
      if (!r.ok) { toast(r.err || "保存失败", 3500); return; }
      closeModal(); toast("已保存备注"); return storesFetch();
    }
    case "remove": {
      const x = W.byKey.get(key);
      if (!x || !confirm("从愿望单移除该商品？其价格记录将一并删除。\n\n" + x.title)) return;
      return wishRemove(key);
    }
    case "check": {
      const r = await api("/api/wish/check", {});
      if (!r.ok) { toast(r.err || "检查失败", 4000); return; }
      toast(r.n ? "正在检查价格，完成后自动更新" : "所有商品均已在一小时内检查过", 3500);
      return storesFetch();
    }
  }
}

// ---------- 关注的店铺 ----------
// Booth shops whose new items the player wants to hear of. The program reads each shop's item list once a day
// (booth/follow.go); this is the list of shops in the Booth tab, a shop's new arrivals, the button in the details
// panel, and the notice of a new item. What it shows comes with /api/stores/state.
const F = { on: false, open: "", shops: [], bySub: new Map(), unseen: 0, watch: true, paused: false, busy: false, sig: "", paste: "", fresh: new Set(), adding: false };
const FOLLOW_ICON = '<svg viewBox="0 0 24 24"><path d="M4 9.5 5.2 5h13.6L20 9.5"/><path d="M4 9.5a2.7 2.7 0 0 0 5.3 0 2.7 2.7 0 0 0 5.4 0 2.7 2.7 0 0 0 5.3 0"/><path d="M5.5 12v8h13v-8"/><path d="M10 20v-5h4v5"/></svg>';
function followTake(f) {
  const sig = JSON.stringify(f), changed = sig !== F.sig;
  F.sig = sig; F.shops = f.shops || []; F.bySub = new Map(F.shops.map(x => [x.sub, x]));
  F.unseen = f.unseen || 0; F.watch = f.watch !== false; F.paused = !!f.paused; F.busy = !!f.busy;
  if (F.open && !F.bySub.has(F.open)) F.open = ""; // unfollowed meanwhile
  followAnnounce();
  return changed;
}
// a new item is told once: a notice that leads to the shop (the entry in the side bar keeps its dot until the list is opened)
function followAnnounce() {
  const news = [];
  for (const s of F.shops) for (const x of s.news || []) if (!x.told) news.push({ s, x });
  if (!news.length) return;
  api("/api/follow/told", { keys: news.map(n => n.x.key) }).catch(() => {});
  for (const n of news) n.x.told = true;
  const one = news[0], shops = new Set(news.map(n => n.s.sub));
  toast(news.length > 1 ? (shops.size > 1 ? `关注的店铺有 ${news.length} 件新商品` : `店铺「${one.s.name}」有 ${news.length} 件新商品`) : `店铺「${one.s.name}」上新：${one.x.title}`,
    9000, () => followOpen(shops.size > 1 ? "" : one.s.sub));
}
function followHas(sub) { return F.bySub.has(sub); }
// the shop an item's address names ("https://komado.booth.pm/" → komado; the stand-in the tests use has /shop/<sub>/)
function followSubOf(u) {
  u = String(u || "");
  const m = /^https?:\/\/([a-z0-9][a-z0-9-]*)\.booth\.pm(?:[/?#:]|$)/i.exec(u) || (S.data && S.data.boothWeb && u.startsWith(S.data.boothWeb + "/shop/") ? /\/shop\/([a-z0-9-]+)(?:[/?#]|$)/i.exec(u) : null);
  const sub = m ? m[1].toLowerCase() : "";
  return ["www", "accounts", "manage", "api", "asset", "booth", "static", "help", "checkout"].includes(sub) ? "" : sub;
}
// asked by renderShopGrid() after wishOn(): the shops in place of the results
function followOn() { if (!W.on) $("#shopSort").hidden = F.on; return F.on; }
// told by shopSearch(): a search or a filter shows the Booth results again
function followLeave() { if (!F.on) return; F.on = false; if (S.view === "shop") renderShopSide(); }
function followSeen() {
  for (const s of F.shops) for (const x of s.news || []) if (x.unseen) { F.fresh.add(x.key); x.unseen = false; }
  F.unseen = 0;
  api("/api/follow/seen", {}).catch(() => {});
}
// the list of shops, or one shop's new arrivals
function followOpen(sub) {
  if (S.view !== "shop") setView("shop");
  F.on = true; F.open = sub && F.bySub.has(sub) ? sub : ""; W.on = false;
  if (S.web.open.shop) closeWeb();
  closeDrawer(); sideOpen(false);
  renderShopSide(); renderShopGrid(); $(".main").scrollTop = 0;
}
function followClose() { F.on = false; F.open = ""; renderShopSide(); renderShopGrid(); $(".main").scrollTop = 0; }

// the entry in the Booth tab's side bar, under the wish list's
function followSideHTML() {
  return `<div class="sidelinks wlnav flnav"><button class="navitem${F.on ? " on" : ""}" data-fl="nav" aria-pressed="${F.on}">${FOLLOW_ICON}<span class="nm">关注的店铺</span>${F.unseen ? `<span class="wldot" title="有新商品"></span>` : ""}<span class="n">${esc(F.shops.length)}</span></button></div>`;
}
// in the details panel: the item's shop
function followDrawerBtn(it, hit) {
  const sub = followSubOf(it && it.shopUrl) || (hit && hit.shopSub ? followSubOf("https://" + hit.shopSub + ".booth.pm/") : "");
  if (!sub) return "";
  const on = followHas(sub), name = (it && it.shop) || (hit && hit.shop) || sub;
  return `<button class="btn fltoggle${on ? " on" : ""}" data-fl="toggle" data-sub="${esc(sub)}" data-name="${esc(name)}" aria-pressed="${on}" title="${on ? "点击取消关注" : "关注后每天检查一次店铺上新"}">${FOLLOW_ICON}<span>${on ? "已关注" : "关注店铺"}</span></button>`;
}
function followShopURL(s) { return s.url || "https://" + s.sub + ".booth.pm/"; }
function followRowHTML(s) {
  const err = s.err ? `<div class="small err"><span>上新检查失败</span>　${esc(s.err)}</div>` : "";
  return `<article class="flrow" data-flsub="${esc(s.sub)}">
    <button class="flicon" data-fl="open" data-sub="${esc(s.sub)}" title="查看上新" aria-label="查看上新">${s.icon ? `<img loading="lazy" decoding="async" src="/rthumb?u=${encodeURIComponent(s.icon)}" alt="">` : FOLLOW_ICON}</button>
    <div class="flmain">
      <button class="wltitle" data-fl="open" data-sub="${esc(s.sub)}" data-i18n="off">${esc(s.name || s.sub)}</button>
      <div class="wlsub"><span data-i18n="off">${esc(s.sub)}.booth.pm</span><span class="t" title="程序在该店铺的商品列表中见过的商品数（每次只读取列表第一页），不是店铺的商品总数"><span>已记录</span> <span class="num">${esc(s.items)}</span> <span>件商品</span></span><span class="t">${s.checked ? `<span>上次检查</span> ${esc(fmtTime(s.checked))}` : `<span>尚未检查</span>`}</span>${s.watch ? "" : `<span class="t">已关闭上新检查</span>`}</div>
      ${s.note ? `<div class="wlnote"><span class="lb">备注</span><span data-i18n="off">${esc(s.note)}</span></div>` : ""}${err}
    </div>
    <div class="flnew">${s.unread ? `<button class="flbadge" data-fl="open" data-sub="${esc(s.sub)}"><span>上新</span> ${esc(s.unread)}</button>` : ""}</div>
    <div class="wlacts">
      <button class="btn small" data-fl="shop" data-sub="${esc(s.sub)}">在 Booth 中打开</button>
      <button class="wb" data-fl="note" data-sub="${esc(s.sub)}" title="编辑备注" aria-label="编辑备注">${ICON.edit}</button>
      <button class="wb" data-fl="remove" data-sub="${esc(s.sub)}" title="取消关注" aria-label="取消关注">${ICON.x}</button>
    </div></article>`;
}
function followCardHTML(s, x) {
  const on = wishHas(x.id), fresh = !x.read || F.fresh.has(x.key);
  return `<article class="card flcard${fresh ? " fresh" : ""}" data-flitem="${esc(x.id)}" tabindex="0">
    <div class="cover">${x.thumb ? `<img loading="lazy" decoding="async" src="/rthumb?u=${encodeURIComponent(x.thumb)}" alt="">` : `<div class="ph">${esc((x.title || "").slice(0, 18))}</div>`}
      <div class="sbadges">${!x.read ? `<span class="badge-bought">新</span>` : ""}${x.adult ? `<span class="badge-owned">成人向</span>` : ""}</div>
      <button class="wlbtn${on ? " on" : ""}" data-fl="wish" data-id="${esc(x.id)}" aria-pressed="${on}" title="${on ? "已在愿望单，点击移除" : "加入愿望单"}" aria-label="${on ? "已在愿望单，点击移除" : "加入愿望单"}">${WISH_ICON}</button></div>
    <div class="body">
      <div class="title" data-i18n="off">${esc(x.title || "Booth #" + x.id)}</div>
      <div class="sub" data-i18n="off">${esc(s.name || s.sub)}</div>
      <div class="meta"><span class="cat"><span>上新于</span> ${esc(fmtTime(x.at))}</span><span class="price" data-i18n="off">${esc(x.price || "")}</span></div>
    </div></article>`;
}
// drawn in place of the Booth results: the shops, or one shop's new arrivals. The paste field and the list are
// separate parts: a refresh in the background redraws the list only when it changed, and never the field.
function renderFollow() {
  if (F.unseen && !S.web.open.shop) { followSeen(); renderShopSide(); } // the list is in view: what arrived is seen
  const s = F.open ? F.bySub.get(F.open) : null;
  if (s) return renderFollowShop(s);
  const n = F.shops.length;
  put($("#resultbar"), `<b>${esc(n)}</b><span>家关注的店铺</span>
    <span class="wlbar"><label class="check"><input type="checkbox" id="fl_watch"${F.watch ? " checked" : ""}> 每天检查上新</label>
    <button class="btn small" data-fl="check">立即检查</button>
    <button class="btn small" data-fl="nav">返回 Booth 商品</button></span>`);
  // (changed in place: a round that starts or ends must not redraw the bar under the pointer)
  const cb = $('#resultbar [data-fl="check"]'), label = F.busy ? "正在检查…" : "立即检查";
  cb.disabled = F.busy || !n;
  if (cb.textContent !== label) cb.textContent = label;
  if (!$("#fl")) {
    gridPut(`<div class="wl fl" id="fl"><div class="wladd"><input id="fl_paste" autocomplete="off" placeholder="粘贴 Booth 店铺链接或商品链接" aria-label="Booth 店铺链接或商品链接"><button class="btn" data-fl="add">关注店铺</button></div>
      <div id="fl_top"></div><div id="fl_list"></div><p class="wlfoot">商品名称与价格取自店铺页面，按店铺所用货币显示，未作换算。</p></div>`);
    $("#fl_paste").value = F.paste;
  }
  put($("#fl_top"), F.paused ? `<p class="wlpause">Booth 暂时限制了访问，上新检查将稍后自动继续。</p>` : !F.watch && n ? `<p class="wlpause">已关闭每天的上新检查，不会再提醒新商品。</p>` : "");
  const list = $("#fl_list");
  if (!n) { put(list, `<div class="empty"><h2>尚未关注店铺</h2><p>关注 Booth 店铺后，软件每天查看一次店铺的商品列表，有新商品时提醒。</p></div>`); return; }
  // row by row: one shop being looked at does not redraw the others (nor the one a button is being pressed in)
  const rows = F.shops.map(x => [x.sub, followRowHTML(x)]), kids = [...list.children], refocus = focusMark(list);
  if (kids.length === rows.length && kids.every((k, i) => k.dataset.flsub === rows[i][0])) {
    rows.forEach(([, html], i) => {
      if (kids[i]._h === html) return;
      const t = document.createElement("template"); t.innerHTML = html;
      const row = t.content.firstElementChild; row._h = html; kids[i].replaceWith(row);
    });
  } else {
    list._h = null; list.innerHTML = rows.map(r => r[1]).join("");
    [...list.children].forEach((k, i) => { k._h = rows[i][1]; });
  }
  refocus();
}
function renderFollowShop(s) {
  const news = s.news || [], unread = s.unread || 0;
  put($("#resultbar"), `<b>${esc(news.length)}</b><span>件新商品</span>${unread ? `<span class="picked"><span>未读</span> ${esc(unread)}</span>` : ""}
    <span class="wlbar"><label class="check"><input type="checkbox" id="fl_swatch"${s.watch ? " checked" : ""}> 检查此店铺的上新</label>
    <button class="btn small" data-fl="readall" data-sub="${esc(s.sub)}"${unread ? "" : " disabled"}>全部标为已读</button>
    <button class="btn small" data-fl="list">返回店铺列表</button></span>`);
  if (!$("#flshop") || $("#flshop").dataset.sub !== s.sub) {
    gridPut(`<div class="wl fl" id="flshop" data-sub="${esc(s.sub)}"><div id="fl_head"></div><div id="fl_top"></div><div class="grid flnews" id="fl_news"></div><p class="wlfoot">商品名称与价格取自店铺页面，按店铺所用货币显示，未作换算。</p></div>`);
  }
  put($("#fl_head"), `<div class="flhead">
    <div class="flicon big">${s.icon ? `<img src="/rthumb?u=${encodeURIComponent(s.icon)}" alt="">` : FOLLOW_ICON}</div>
    <div class="flinfo">
      <h2 data-i18n="off">${esc(s.name || s.sub)}</h2>
      <div class="wlsub"><span data-i18n="off">${esc(s.sub)}.booth.pm</span><span class="t" title="程序在该店铺的商品列表中见过的商品数（每次只读取列表第一页），不是店铺的商品总数"><span>已记录</span> <span class="num">${esc(s.items)}</span> <span>件商品</span></span><span class="t"><span>关注于</span> ${esc(fmtTime(s.added))}</span><span class="t">${s.checked ? `<span>上次检查</span> ${esc(fmtTime(s.checked))}` : `<span>尚未检查</span>`}</span></div>
      ${s.note ? `<div class="wlnote"><span class="lb">备注</span><span data-i18n="off">${esc(s.note)}</span></div>` : ""}
      ${s.err ? `<div class="small err"><span>上新检查失败</span>　${esc(s.err)}</div>` : ""}
      <div class="btnrow"><button class="btn small" data-fl="shop" data-sub="${esc(s.sub)}">在 Booth 中打开</button><button class="btn small" data-fl="note" data-sub="${esc(s.sub)}">${ICON.edit}<span>编辑备注</span></button><button class="btn small" data-fl="remove" data-sub="${esc(s.sub)}">${ICON.x}<span>取消关注</span></button></div>
    </div></div>`);
  put($("#fl_top"), F.paused ? `<p class="wlpause">Booth 暂时限制了访问，上新检查将稍后自动继续。</p>` : !s.watch ? `<p class="wlpause">已关闭此店铺的上新检查。</p>` : !F.watch ? `<p class="wlpause">已关闭每天的上新检查，不会再提醒新商品。</p>` : "");
  const grid = $("#fl_news");
  if (!news.length) { grid.classList.add("none"); put(grid, `<div class="empty"><h2>暂无上新</h2><p>关注后出现的新商品会显示在这里，每天检查一次。</p></div>`); return; }
  grid.classList.remove("none");
  const refocus = focusMark(grid);
  if (put(grid, news.map(x => followCardHTML(s, x)).join(""))) refocus();
}
async function followAdd(body) {
  if (F.adding) return false;
  F.adding = true;
  let r;
  try { r = await api("/api/follow/add", body); } catch (e) { r = { ok: false, err: "程序内部错误" }; } finally { F.adding = false; }
  if (!r.ok) { toast(r.err || "关注店铺失败", 4000); return false; }
  toast(`已关注店铺「${r.name || r.sub}」`);
  await storesFetch();
  return true;
}
async function followRemove(sub) {
  await api("/api/follow/remove", { sub });
  toast("已取消关注");
  await storesFetch();
}
function followNoteOpen(sub) {
  const s = F.bySub.get(sub); if (!s) return;
  const m = $("#modal");
  m.dataset.kind = "flnote";
  m.innerHTML = `<div class="mhead">编辑备注</div><div class="mbody">
    <p class="lead" data-i18n="off">${esc(s.name || s.sub)}</p>
    <div class="row"><label for="fl_note">备注</label><textarea id="fl_note" maxlength="500" placeholder="例如：常买的品类、关注的原因">${esc(s.note)}</textarea></div>
  </div><div class="mfoot"><button class="btn ghost" data-m="close">取消</button><button class="btn primary" data-fl="notesave" data-sub="${esc(sub)}">保存</button></div>`;
  showModal();
  $("#fl_note").focus();
}
async function followAction(b) {
  const sub = b.dataset.sub || "", id = b.dataset.id || "";
  switch (b.dataset.fl) {
    case "nav": return F.on && S.view === "shop" && !S.web.open.shop ? followClose() : followOpen("");
    case "open": return followOpen(sub);
    case "list": F.open = ""; renderShopGrid(); $(".main").scrollTop = 0; return;
    case "toggle": {
      if (followHas(sub)) await followRemove(sub); else await followAdd({ sub, name: b.dataset.name || "" });
      // the panel was drawn again with the button in its new state: the focus goes back onto it
      const nb = $(`#drawer [data-fl="toggle"]`);
      if (nb && document.activeElement === document.body) nb.focus({ preventScroll: true });
      return;
    }
    case "add": {
      const text = $("#fl_paste").value.trim();
      if (!text) { $("#fl_paste").focus(); return; }
      b.disabled = true;
      const ok = await followAdd({ text });
      b.disabled = false;
      if (ok && $("#fl_paste")) { F.paste = ""; $("#fl_paste").value = ""; }
      return;
    }
    case "shop": { const s = F.bySub.get(sub); return s ? openWeb(followShopURL(s), "booth") : undefined; }
    case "wish": {
      const s = F.bySub.get(F.open), x = s && (s.news || []).find(n => n.id === id);
      if (wishHas(id)) await wishRemove("booth:" + id);
      else if (x) await wishAdd({ id, title: x.title, shop: s.name || s.sub, thumb: x.thumb, price: x.price });
      const nb = $(`.flcard[data-flitem="${CSS.escape(id)}"] .wlbtn`);
      if (nb && document.activeElement === document.body) nb.focus({ preventScroll: true });
      return;
    }
    case "note": return followNoteOpen(sub);
    case "notesave": {
      const r = await api("/api/follow/note", { sub, note: $("#fl_note").value });
      if (!r.ok) { toast(r.err || "保存失败", 3500); return; }
      closeModal(); toast("已保存备注"); return storesFetch();
    }
    case "remove": {
      const s = F.bySub.get(sub);
      if (!s || !confirm("取消关注该店铺？其上新记录将一并删除。\n\n" + (s.name || s.sub))) return;
      if (F.open === sub) F.open = "";
      return followRemove(sub);
    }
    case "readall": {
      await api("/api/follow/read", { sub });
      F.fresh = new Set();
      return storesFetch();
    }
    case "check": {
      const r = await api("/api/follow/check", {});
      if (!r.ok) { toast(r.err || "检查失败", 4000); return; }
      toast(r.n ? "正在检查上新，完成后自动更新" : "所有店铺均已在一小时内检查过", 3500);
      return storesFetch();
    }
  }
}

// ---------- Jinxxy ----------
// A marketplace many VRChat creators outside Japan sell on. It has no interface for buyers and its pages could
// not be read without an account, so it is browsed as it is, inside the page area, in the 闲鱼 tab. The search
// box goes to Bing kept to jinxxy.com: the market's own search has no address a query can be put into.
const JX_CATS = [["全部商品", "/market/browse"], ["免费商品", "/market/browse?max_price=0"], ["模型", "/market/avatars"], ["衣服", "/market/clothing"],
  ["配饰与道具", "/market/avatar-props"], ["材质与贴图", "/market/materials"], ["粒子与着色器", "/market/shaders"], ["脚本与工具", "/market/scripts"],
  ["世界", "/market/worlds"], ["世界素材", "/market/world-assets"]];
function xySite() { return JX.site; }
function jxOn() { return S.view === "xianyu" && JX.site === "jinxxy"; }
function jxHome() { return JX.base + "/market/browse"; }
function xyPlaceholder() { return JX.site === "jinxxy" ? "搜索 Jinxxy，按 Enter 确认" : "搜索闲鱼，按 Enter 确认"; }
function isJxURL(u) { return (!!JX.base && (u === JX.base || u.startsWith(JX.base + "/"))) || /^https?:\/\/([^/]*\.)?jinxxy\.com(\/|$)/i.test(u); }
// asked by webKind(): whose page an address is ("" = not one of this file's)
function storeKind(u) { return isJxURL(u) ? "jinxxy" : ""; }
// the 闲鱼 tab goes over to the other site: each keeps its own search words
function jxSetSite(site) {
  if (site === JX.site) return;
  JX.q[JX.site] = S.web.xq; JX.site = site; S.web.xq = JX.q[site] || "";
  if (S.view === "xianyu") { $("#q").value = S.web.xq; $("#webhost").dataset.k = ""; }
  storesSave(); saveUI();
}
// told by openWeb() whose page is about to open
function storeOpening(kind) { if (kind === "xianyu" || kind === "jinxxy") jxSetSite(kind); }
function jxGo(site) {
  const same = S.view === "xianyu" && JX.site === site;
  jxSetSite(site);
  if (S.view !== "xianyu") return setView("xianyu");
  if (same && S.web.st.open) return;
  renderAll();
  if (site === "xianyu") return xyEnter(); // (its own view, or the default browser: app.js)
  if (!S.data.paneMode) return;
  return openWeb(lastPage(site) || jxHome(), site);
}
function jxOpen(u) { return S.data.paneMode ? openWeb(u, "jinxxy") : openURL(u); }
function jxReopen() { return jxOpen(lastPage("jinxxy") || jxHome()); }
function jxSearch(q) {
  S.web.xq = q; storesSave(); saveUI();
  return jxOpen(q && JX.search ? JX.search + encodeURIComponent(q) : jxHome());
}
// the files the page area's browser has downloaded for the library; one that has arrived is told once
function jxFiles(files) {
  const first = !JX.told;
  if (first) JX.told = new Set();
  for (const f of files) {
    if (f.status !== "done" && f.status !== "failed") continue;
    if (!JX.told.has(f.id) && !first) toast(f.status === "done" ? "已下载并收录到素材库：" + f.name : f.foreign ? "其他网站页面发起的下载不会收录，已取消：" + f.name : "下载未能收录：" + f.name, 6000);
    JX.told.add(f.id);
  }
  const sig = JSON.stringify(files);
  if (sig === JX.fsig) return;
  JX.fsig = sig; JX.files = files;
  if (jxOn()) renderJxSide();
}
function jxFileStatus(f) {
  if (f.status === "running") return f.total > 0 ? "下载中 " + Math.min(100, Math.round(f.done / f.total * 100)) + "%" : "下载中";
  return { saving: "正在收录", done: "已收录", failed: "失败" }[f.status] || "";
}
function xySwitchHTML() {
  const chip = (site, label) => `<button class="chip${JX.site === site ? " on" : ""}" data-jx="go" data-site="${site}" aria-pressed="${JX.site === site}">${label}</button>`;
  return `<h3>店铺</h3><div class="chips">${chip("xianyu", "闲鱼")}${chip("jinxxy", "Jinxxy")}</div>`;
}
function renderJxSide() {
  const link = (label, path) => `<button class="navitem" data-jx="open" data-path="${esc(path)}"><span>${esc(label)}</span></button>`;
  // live numbers come with the page area's own state, asked for every second while it is in view
  const live = new Map(((S.web.st || {}).files || []).map(f => [f.id, f]));
  const files = JX.files.map(f => live.get(f.id) || f).slice(-5).reverse();
  put($("#side"), `${xySwitchHTML()}
    <h3>Jinxxy</h3><div class="sidelinks two">${link("首页", "/")}${link("登录", "/login")}</div>
    <h3>商品分类</h3><div class="sidelinks two">${JX_CATS.map(c => link(c[0], c[1])).join("")}</div>
    ${files.length ? `<h3>下载记录</h3><div class="jxfiles">${files.map(f => `<div class="jxfile"><span class="fn" title="${esc(f.name)}">${esc(f.name || "下载中")}</span>${
      f.status === "done" && f.path ? `<button class="h4act" data-jx="reveal" data-path="${esc(f.path)}">打开位置</button>` : `<span class="st${f.status === "failed" ? " err" : ""}" title="${esc(f.err || "")}">${esc(jxFileStatus(f))}</span>`}</div>`).join("")}</div>` : ""}
    <h3>使用说明</h3>
    ${S.data.paneMode ? `<p class="sidenote">Jinxxy 是海外 VRChat 创作者常用的商品市场。登录、购买和下载均在此页面内完成，登录状态保存在本机。</p>
    <p class="sidenote">请使用邮箱密码或 Discord 登录；通过邮件链接登录时，链接会在默认浏览器中打开，无法登录到此页面。</p>
    <p class="sidenote">顶部搜索框通过必应（Bing）在 jinxxy.com 范围内搜索；也可以直接使用页面内的搜索框。</p>
    <p class="sidenote">前往其他网站（Payhip、Gumroad 等）的商品链接将在默认浏览器中打开。</p>
    <h3>下载</h3>
    <p class="sidenote">登录后点击页面右上角的「My Purchases」进入已购列表。在此页面下载的文件将保存到软件的下载文件夹，并自动解压、收录到素材库。只收录 Jinxxy 页面发起的下载，在搜索结果等其他网站的页面上发起的下载会被取消。如下载后未出现在「下载记录」中，文件已由浏览器保存到系统的「下载」文件夹。</p>`
    : `<p class="sidenote">Jinxxy 是海外 VRChat 创作者常用的商品市场。本机没有内置浏览器，Jinxxy 页面在默认浏览器中打开，下载的文件由浏览器保存。</p>
    <p class="sidenote">顶部搜索框通过必应（Bing）在 jinxxy.com 范围内搜索。</p>`}
    <h3>愿望单</h3>
    <p class="sidenote">愿望单与降价提醒目前仅支持 Booth 商品；Jinxxy 商品可使用网站自带的 Wishlist。</p>`);
}
// in the page area's toolbar, where 闲鱼 has 「收录网盘链接」
function jxBarHTML() {
  if ((S.web.last.jinxxy || "") !== JX.saved) storesSave(); // where the page is now is where the tab opens next time
  const files = (S.web.st || {}).files || [], run = files.filter(f => f.status === "running" || f.status === "saving");
  const sig = JSON.stringify(files);
  if (sig !== JX.lsig) { JX.lsig = sig; setTimeout(() => { if (jxOn()) renderJxSide(); }, 0); }
  return run.length ? `<span class="wnote">${esc(run[0].name || "")}　${esc(jxFileStatus(run[0]))}</span>` : "";
}
function jxAction(b) {
  switch (b.dataset.jx) {
    case "go": return jxGo(b.dataset.site);
    case "open": return jxOpen(JX.base + b.dataset.path);
    case "reveal": return openPath(b.dataset.path);
  }
}

// ---------- events ----------
// Taken before app.js's own handlers (in the capturing phase), and kept from them: a click on the wish button
// of a card must not open the card.
document.addEventListener("click", e => {
  const b = e.target.closest ? e.target.closest("[data-wl], [data-jx], [data-fl]") : null;
  if (!b) { // a new arrival's card itself: the item in the Booth page
    const c = e.target.closest ? e.target.closest(".flcard[data-flitem]") : null;
    if (!c) return;
    e.stopPropagation(); e.preventDefault();
    openWeb(boothItemURL(c.dataset.flitem), "booth").catch(err => toast("操作失败：" + ((err && err.message) || err), 4500));
    return;
  }
  if (b.disabled) return;
  e.stopPropagation(); e.preventDefault();
  if (document.body.classList.contains("sideopen") && b.closest("#side .navitem")) sideOpen(false);
  Promise.resolve(b.dataset.wl ? wishAction(b) : b.dataset.fl ? followAction(b) : jxAction(b)).catch(err => toast("操作失败：" + ((err && err.message) || err), 4500));
}, true);
document.addEventListener("contextmenu", e => {
  const c = e.target.closest ? e.target.closest(".flcard[data-flitem]") : null;
  if (!c) return;
  e.stopPropagation(); e.preventDefault();
  openWeb(boothItemURL(c.dataset.flitem), "booth");
}, true);
document.addEventListener("keydown", e => {
  if (e.key !== "Enter" || e.isComposing || e.keyCode === 229) return;
  const id = e.target.id;
  if (id === "wl_paste") { e.preventDefault(); e.stopPropagation(); const b = $('[data-wl="add"]'); if (b) b.click(); return; }
  if (id === "wl_note" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); e.stopPropagation(); const b = $('#modal [data-wl="notesave"]'); if (b) b.click(); return; }
  if (id === "fl_paste") { e.preventDefault(); e.stopPropagation(); const b = $('[data-fl="add"]'); if (b) b.click(); return; }
  if (id === "fl_note" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); e.stopPropagation(); const b = $('#modal [data-fl="notesave"]'); if (b) b.click(); return; }
  if (e.target.classList && e.target.classList.contains("flcard") && e.target.dataset.flitem) { e.preventDefault(); e.stopPropagation(); openWeb(boothItemURL(e.target.dataset.flitem), "booth"); return; }
  if (id === "q" && jxOn()) { e.stopPropagation(); jxSearch(e.target.value.trim()); }
}, true);
document.addEventListener("input", e => { if (e.target.id === "wl_paste") W.paste = e.target.value; if (e.target.id === "fl_paste") F.paste = e.target.value; }, true);
document.addEventListener("change", e => {
  if (e.target.id === "wl_sort") { W.sort = e.target.value; storesSave(); renderWish(); }
  if (e.target.id === "wl_watch") {
    const on = e.target.checked; W.watch = on;
    api("/api/wish/watch", { on }).then(() => { toast(on ? "已开启每天的价格检查" : "已关闭每天的价格检查"); return storesFetch(); }).catch(() => {});
  }
  if (e.target.id === "fl_watch") {
    const on = e.target.checked; F.watch = on;
    api("/api/follow/watch", { on }).then(() => { toast(on ? "已开启每天的上新检查" : "已关闭每天的上新检查"); return storesFetch(); }).catch(() => {});
  }
  if (e.target.id === "fl_swatch" && F.open) {
    const on = e.target.checked, s = F.bySub.get(F.open); if (s) s.watch = on;
    api("/api/follow/watch", { sub: F.open, on }).then(() => { toast(on ? "已开启此店铺的上新检查" : "已关闭此店铺的上新检查"); return storesFetch(); }).catch(() => {});
  }
}, true);
document.addEventListener("toggle", e => {
  const d = e.target;
  if (d instanceof HTMLDetailsElement && d.dataset.wlhist) { if (d.open) W.histOpen.add(d.dataset.wlhist); else W.histOpen.delete(d.dataset.wlhist); }
}, true);
// the state arrived before this file was read: draw what the page left out, and ask for this file's part
if (S.data) { renderAll(); storesLoaded(); }
