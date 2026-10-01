"use strict";
const $ = (s, el = document) => el.querySelector(s);
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const CAT_COLOR = { "素体": "#c78bff", "衣服": "#ff7aa8", "头发": "#ffb547", "配饰": "#6fd3ff", "道具": "#9be36b", "材质": "#ff9f6b",
  "面捕": "#5ee0c5", "插件": "#8aa2ff", "动作": "#f0e26b", "音效": "#b9b2ff", "字体": "#d0d0d0", "其他": "#8b8fa6" };
const ICON = {
  folder: '<svg viewBox="0 0 24 24"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>',
  cloud: '<svg viewBox="0 0 24 24"><path d="M7 18a4.5 4.5 0 0 1-.6-8.96A6 6 0 0 1 18 9.5a4.25 4.25 0 0 1-.5 8.5z"/></svg>',
  bag: '<svg viewBox="0 0 24 24"><path d="M5 8h14l-1 12H6z"/><path d="M9 8V6a3 3 0 0 1 6 0v2"/></svg>',
  edit: '<svg viewBox="0 0 24 24"><path d="M4 20h4L19 9l-4-4L4 16z"/><path d="m13.5 6.5 4 4"/></svg>',
  copy: '<svg viewBox="0 0 24 24"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a1 1 0 0 1 1-1h9"/></svg>',
  plus: '<svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg>',
  search: '<svg viewBox="0 0 24 24"><circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/></svg>',
  receipt: '<svg viewBox="0 0 24 24"><path d="M6 3h12v18l-3-2-3 2-3-2-3 2z"/><path d="M9 8h6M9 12h6"/></svg>',
  download: '<svg viewBox="0 0 24 24"><path d="M12 4v11m0 0-4-4m4 4 4-4"/><path d="M5 19h14"/></svg>',
};

const S = {
  data: null, rev: 0,
  q: "", cat: "全部", bases: new Set(), usage: "all", share: "all", root: "all", purchase: "all", showHidden: false, sort: "recent",
  openKey: null, setup: null, bs: null, desc: {}, dirty: new Set(), coverPick: null, panOpen: new Set(), panClosed: new Set(),
};
try { const saved = JSON.parse(localStorage.getItem("vrclib.ui") || "{}"); if (saved.sort) S.sort = saved.sort; } catch (e) {}

async function api(path, body) {
  const r = await fetch(path, body === undefined ? {} : {
    method: "POST", headers: { "Content-Type": "application/json", "X-Token": window.API_TOKEN }, body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error(path + " " + r.status);
  return r.json();
}

function toast(msg, ms = 2200) {
  const t = $("#toast"); t.textContent = msg; t.classList.add("on");
  clearTimeout(toast._t); toast._t = setTimeout(() => t.classList.remove("on"), ms);
}

function fmtSize(n) {
  if (!n) return "—";
  const u = ["B", "KB", "MB", "GB", "TB"]; let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return (n >= 100 || i === 0 ? n.toFixed(0) : n.toFixed(1)) + " " + u[i];
}
function fmtTime(t) {
  if (!t) return "从未";
  const d = new Date(t * 1000);
  return `${d.getMonth() + 1}月${d.getDate()}日 ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}
function shortPath(l) {
  if (!l) return "";
  const sep = l.path.includes("\\") ? "\\" : "/";
  const root = l.root || "";
  let rel = l.path.startsWith(root) ? l.path.slice(root.length).replace(/^[\\/]+/, "") : l.path;
  return rootLabel(root) + (rel ? sep + rel : "");
}
function rootLabel(p) { const parts = String(p).split(/[\\/]/).filter(Boolean); return parts[parts.length - 1] || p; }

async function load() {
  const d = await api("/api/state");
  S.data = d; S.rev = d.rev;
  renderAll();
}

// ---------- filtering ----------
function hasShare(a) { return !!(a.user.shareUrl || a.user.panPath); }
function panNames(a) {
  if (!a.pan || !a.pan.files) return "";
  if (a._panNames !== undefined) return a._panNames;
  const out = []; const walk = fs => { for (const f of fs || []) { if (out.length > 300) return; out.push(f.n); if (f.c) walk(f.c); } };
  walk(a.pan.files); return (a._panNames = out.join(" "));
}
function isLocal(a) { return !a.virtual && !a.panOnly; }
function usedStatus(a) { return (a.usage || []).some(u => u.status === "used") ? "used" : (a.usage || []).length ? "partial" : ""; }
function matches(a, skip = "") {
  if (!S.showHidden && a.hidden) return false;
  if (skip !== "purchase") {
    const p = S.purchase;
    if (p === "all" && a.virtual) return false;
    if (p === "bought" && !a.purchase) return false;
    if (p === "local" && (!a.purchase || a.virtual)) return false;
    if (p === "missing" && !a.virtual) return false;
    if (p === "other" && (a.purchase || a.virtual)) return false;
  }
  if (skip !== "cat" && S.cat !== "全部" && a.category !== S.cat) return false;
  if (skip !== "bases" && S.bases.size && !(a.bases || []).some(b => S.bases.has(b))) return false;
  if (skip !== "usage" && S.usage !== "all") {
    if (S.usage === "used" && !usedStatus(a)) return false;
    if (S.usage === "unused" && usedStatus(a)) return false;
    if (S.usage.startsWith("p:") && !(a.usage || []).some(u => u.project === S.usage.slice(2))) return false;
  }
  if (skip !== "share" && S.share !== "all") {
    if (S.share === "has" && !hasShare(a)) return false;
    if (S.share === "none" && hasShare(a)) return false;
    if (S.share === "panonly" && !a.panOnly) return false;
  }
  if (skip !== "root" && S.root !== "all" && !(a.locations || []).some(l => l.root === S.root)) return false;
  if (S.q) {
    const hay = [a.name, a.autoName, a.rawName, a.category, (a.bases || []).join(" "), (a.tags || []).join(" "), a.user.notes,
      a.booth && a.booth.name, a.booth && a.booth.shop, a.boothId, (a.hints || []).join(" "), (a.locations || []).map(l => l.path).join(" "),
      a.purchase && a.purchase.name, a.purchase && a.purchase.shop, a.purchase && (a.purchase.files || []).join(" "),
      a.nameZh, a.booth && (a.booth.tags || []).join(" "), panNames(a)]
      .join(" ").toLowerCase();
    for (const tok of S.q.toLowerCase().split(/\s+/).filter(Boolean)) if (!hay.includes(tok)) return false;
  }
  return true;
}
function sorted(list) {
  const by = {
    recent: (a, b) => (b.firstSeen || 0) - (a.firstSeen || 0) || (b.mtime - a.mtime),
    name: (a, b) => a.name.localeCompare(b.name, "zh-CN"),
    size: (a, b) => b.size - a.size,
    used: (a, b) => (usedStatus(b) === "used") - (usedStatus(a) === "used") || (b.firstSeen || 0) - (a.firstSeen || 0),
  }[S.sort];
  return list.slice().sort(by);
}

// ---------- render ----------
function renderAll() { renderSide(); renderGrid(); renderStatus(); if (S.openKey) renderDrawer(S.openKey, true); }

function navItem(label, count, on, attrs, color) {
  return `<button class="navitem${on ? " on" : ""}" ${attrs}>${color ? `<span class="dot" style="background:${color}"></span>` : ""}<span>${esc(label)}</span><span class="n">${count}</span></button>`;
}

function renderSide() {
  const d = S.data; if (!d) return;
  const all = d.assets || [];
  const base = all.filter(a => matches(a, "cat"));
  const cc = {}; base.forEach(a => cc[a.category] = (cc[a.category] || 0) + 1);
  let h = `<h3>分类</h3>` + navItem("全部", base.length, S.cat === "全部", `data-cat="全部"`);
  for (const c of d.categories) if (cc[c]) h += navItem(c, cc[c], S.cat === c, `data-cat="${esc(c)}"`, CAT_COLOR[c]);

  const bc = {}; all.filter(a => matches(a, "bases")).forEach(a => (a.bases || []).forEach(b => bc[b] = (bc[b] || 0) + 1));
  const bl = Object.keys(bc).sort((x, y) => bc[y] - bc[x]);
  if (bl.length) h += `<h3>适配素体</h3><div class="chips">` + bl.map(b => `<button class="chip${S.bases.has(b) ? " on" : ""}" data-base="${esc(b)}">${esc(b)}<span class="n">${bc[b]}</span></button>`).join("") + `</div>`;

  const pb = all.filter(a => matches(a, "purchase"));
  const syncing = d.purchaseBusy;
  if (!d.purchaseSync) {
    h += `<h3>Booth 已购</h3><div class="cta"><p>登录一次 Booth，列出所有买过的商品，并标出哪些已经下载到本地。</p>
      ${syncing ? `<button class="btn small" id="btnSyncCancel">取消同步</button>` : `<button class="btn small" id="btnSync">同步 Booth 已购</button>`}</div>`;
  } else {
    const local = pb.filter(a => a.purchase && !a.virtual).length, miss = pb.filter(a => a.virtual).length;
    const other = pb.filter(a => !a.purchase && !a.virtual).length;
    h += `<h3 class="withact">Booth 已购${syncing ? `<button class="h3act" id="btnSyncCancel">取消同步</button>` : `<button class="h3act" id="btnSync" title="上次同步：${esc(fmtTime(d.purchaseSync))}">重新同步</button>`}</h3>` +
      navItem("本地全部素材", local + other, S.purchase === "all", `data-purchase="all"`) +
      navItem("已购，本地有", local, S.purchase === "local", `data-purchase="local"`, "#ff5a6a") +
      navItem("已购，还没下载", miss, S.purchase === "missing", `data-purchase="missing"`, "#ffb547") +
      navItem("全部已购", local + miss, S.purchase === "bought", `data-purchase="bought"`) +
      navItem("不是 Booth 已购", other, S.purchase === "other", `data-purchase="other"`, "#6c7088");
  }

  const ub = all.filter(a => matches(a, "usage"));
  const pc = {}; let usedN = 0;
  ub.forEach(a => { if (usedStatus(a)) usedN++; (a.usage || []).forEach(u => pc[u.project] = (pc[u.project] || 0) + 1); });
  h += `<h3>工程使用</h3>` + navItem("全部", ub.length, S.usage === "all", `data-usage="all"`) +
    navItem("工程里用到的", usedN, S.usage === "used", `data-usage="used"`, "#3fd0a6") +
    navItem("没被用到的", ub.length - usedN, S.usage === "unused", `data-usage="unused"`, "#6c7088");
  for (const p of (d.projects || [])) if (pc[p.name]) h += navItem(p.name, pc[p.name], S.usage === "p:" + p.name, `data-usage="p:${esc(p.name)}"`);

  const sb = all.filter(a => matches(a, "share")); const sh = sb.filter(hasShare).length;
  h += `<h3>网盘</h3>` + navItem("全部", sb.length, S.share === "all", `data-share="all"`) +
    navItem("已填网盘链接", sh, S.share === "has", `data-share="has"`, "#4c8dff") +
    navItem("还没填", sb.length - sh, S.share === "none", `data-share="none"`);
  const po = sb.filter(a => a.panOnly).length;
  if (po) h += navItem("只在网盘里", po, S.share === "panonly", `data-share="panonly"`, "#7fb0ff");

  const rb = all.filter(a => matches(a, "root")); const rc = {};
  rb.forEach(a => new Set((a.locations || []).map(l => l.root)).forEach(r => rc[r] = (rc[r] || 0) + 1));
  h += `<h3>来源文件夹</h3>` + navItem("全部", rb.length, S.root === "all", `data-root="all"`);
  for (const r of (d.settings.roots || [])) if (rc[r]) h += navItem(rootLabel(r), rc[r], S.root === r, `data-root="${esc(r)}" title="${esc(r)}"`);

  const hiddenN = all.filter(a => a.hidden).length;
  if (hiddenN) h += `<label class="check"><input type="checkbox" id="showHidden"${S.showHidden ? " checked" : ""}> 显示已隐藏的 ${hiddenN} 个</label>`;
  $("#side").innerHTML = h;
}

function coverHTML(a, big) {
  const src = big ? a.coverBig : a.cover;
  const ph = `<div class="ph" style="background:linear-gradient(135deg, ${CAT_COLOR[a.category] || "#8b8fa6"}22, transparent 70%)">${esc(a.name.slice(0, 18))}</div>`;
  return src ? `<img loading="lazy" src="${esc(src)}" alt="" onerror="this.replaceWith(document.createRange().createContextualFragment(this.dataset.ph))" data-ph="${esc(ph)}">` : ph;
}

function usedBadge(a) {
  const us = a.usage || []; if (!us.length) return "";
  const used = us.filter(u => u.status === "used");
  const list = used.length ? used : us;
  const extra = list.length > 1 ? ` +${list.length - 1}` : "";
  return `<span class="badge-used${used.length ? "" : " partial"}" title="${esc(us.map(u => `${u.project}：${Math.round(u.ratio * 100)}%`).join("\n"))}">${used.length ? "在用" : "部分"}：${esc(list[0].project)}${extra}</span>`;
}

function orderLine(p) {
  const o = (p.orders || [])[0];
  return o ? (o.date ? o.date + " 购入" : "订单 #" + o.id) : (p.gift ? "收到的礼物" : "Booth 已购");
}
function boughtBadge(a) {
  if (!a.purchase) return "";
  return `<span class="badge-bought${a.virtual ? " missing" : ""}" title="${esc(orderLine(a.purchase))}">${a.virtual ? "未下载" : "已购"}</span>`;
}

function cardHTML(a) {
  const loc = (a.locations || [])[0];
  const pan = hasShare(a);
  const p = a.purchase;
  const shop = (a.booth && a.booth.shop) || (p && p.shop);
  const subline = shop ? shop : (a.booth && a.booth.name && a.booth.name !== a.name ? a.booth.name : rootLabel(loc ? loc.root : ""));
  const where = a.virtual ? `${orderLine(p)}，本地还没有` : a.panOnly ? panLine(a) : shortPath(loc);
  const tip = a.virtual ? "点击：打开 Booth 下载页" : a.panOnly ? "点击：打开网盘分享（提取码会自动复制）" : "点击：在资源管理器中打开";
  const zh = !S.data.settings.hideZh && a.nameZh ? `<div class="zh" title="${esc(a.nameZh)}">${esc(a.nameZh)}</div>` : "";
  return `<article class="card${a.hidden ? " hiddenasset" : ""}${a.virtual ? " virtual" : ""}${a.panOnly ? " panonly" : ""}" data-key="${esc(a.key)}" tabindex="0" title="${tip}">
    <div class="cover">${coverHTML(a)}${boughtBadge(a)}${a.panOnly ? `<span class="badge-pan">网盘</span>` : ""}${usedBadge(a)}
      <div class="where"><div class="path" title="${esc(loc ? loc.path : where)}">${esc(where)}</div>
        <div class="acts">
          ${a.virtual ? `<button class="act buy" data-act="buypage" title="打开 Booth 下载页（订单页）">${ICON.download}<span>下载页</span></button>`
            : a.panOnly ? "" : `<button class="act" data-act="open" title="打开所在位置">${ICON.folder}</button>`}
          ${pan ? `<button class="act pan" data-act="pan" title="打开网盘">${ICON.cloud}${a.panOnly ? "<span>网盘</span>" : ""}</button>` : ""}
          ${p && !a.virtual ? `<button class="act buy" data-act="buypage" title="Booth 购买页（可重新下载）">${ICON.receipt}</button>` : ""}
          <button class="act booth${a.boothId ? "" : " search"}" data-act="booth" title="${a.boothId ? "打开 Booth 商品页" : "在 Booth 上搜「" + esc(a.boothQuery || a.name) + "」"}">${a.boothId ? ICON.bag : ICON.search}</button>
          <button class="act edit" data-act="edit" title="详情和编辑">${ICON.edit}<span>详情</span></button>
        </div></div>
    </div>
    <div class="body">
      <div class="title">${esc(a.name)}</div>${zh}
      <div class="sub">${esc(a.panOnly && !shop ? "百度网盘" : subline)}</div>
      ${(a.bases || []).length ? `<div class="bases">${a.bases.slice(0, 4).map(b => `<span class="base">${esc(b)}</span>`).join("")}</div>` : ""}
      <div class="meta"><span class="cat"><i style="background:${CAT_COLOR[a.category] || "#8b8fa6"}"></i>${esc(a.category)}</span><span>${a.virtual ? "未下载" : a.panOnly && !a.size ? "—" : fmtSize(a.size)}</span>
        <span class="flags">${pan ? `<span class="flag pan" title="已填网盘链接">${ICON.cloud}</span>` : ""}${a.boothId ? `<span class="flag booth" title="Booth #${esc(a.boothId)}">${ICON.bag}</span>` : ""}</span></div>
    </div></article>`;
}

function renderGrid() {
  const d = S.data; if (!d) return;
  const list = sorted((d.assets || []).filter(a => matches(a)));
  const total = (d.assets || []).filter(a => (S.showHidden || !a.hidden) && !a.virtual).length;
  const filtered = S.q || S.cat !== "全部" || S.bases.size || S.usage !== "all" || S.share !== "all" || S.root !== "all" || S.purchase !== "all";
  let rb = `<b>${list.length}</b><span>个${S.purchase === "missing" || S.purchase === "bought" ? "商品" : "素材"}${filtered ? `（本地共 ${total} 个）` : ""}</span>`;
  if (filtered) rb += `<button class="clear" id="clearFilters">清除筛选</button>`;
  for (const w of (d.warnings || [])) rb += `<span class="warnline">${esc(w)}</span>`;
  $("#resultbar").innerHTML = rb;
  const g = $("#grid");
  if (!list.length) {
    const scanning = d.busy;
    g.innerHTML = `<div class="empty">${!(d.assets || []).some(a => !a.virtual) && S.purchase === "all"
      ? (scanning ? `<h2>正在扫描素材文件夹</h2><p>第一次扫描要读完所有 unitypackage，可能需要几分钟。卡片会在文件夹扫完后先出现。</p>`
        : `<h2>还没有素材</h2><p>在设置里添加放素材的文件夹，然后点「重新扫描」。</p><button class="btn primary" id="emptySettings">打开设置</button>`)
      : `<h2>没有符合条件的素材</h2><p>换个关键词，或者清除筛选。</p>`}</div>`;
    return;
  }
  g.innerHTML = list.map(cardHTML).join("");
}

function renderStatus() {
  const d = S.data; if (!d) return;
  const running = (d.tasks || []).filter(t => t.running);
  let h = "";
  if (running.length) {
    for (const t of running) {
      const pct = t.total ? Math.min(100, Math.round(t.done / t.total * 100)) : 0;
      h += `<span class="task">${esc(t.label)}<span class="bar"><i style="width:${pct}%"></i></span>${t.total ? `${t.done}/${t.total}` : ""} <span class="muted">${esc(t.msg || "")}</span></span>`;
    }
  } else {
    h += `<span>上次扫描 ${fmtTime(d.lastScan)}</span><span>工程使用分析 ${fmtTime(d.lastUsage)}</span>`;
    const bt = (d.tasks || []).find(t => t.name === "booth");
    if (bt && bt.msg && bt.msg.includes("代理")) h += `<span class="err">${esc(bt.msg)}</span>`;
    const pt = (d.tasks || []).find(t => t.name === "purchase");
    if (pt && pt.msg && pt.ended && Date.now() / 1000 - pt.ended < 600) h += `<span class="${pt.msg.startsWith("完成") ? "okline" : "err"}">Booth 已购：${esc(pt.msg)}</span>`;
  }
  h += `<span class="right">${(d.projects || []).length} 个 Unity 工程 · v${esc(d.version)}</span>`;
  $("#status").innerHTML = h;
  $("#btnScan").disabled = !!d.busy;
  $("#btnScan").textContent = d.busy ? "正在扫描…" : "重新扫描";
}

// ---------- actions ----------
function findAsset(key) { return (S.data.assets || []).find(a => a.key === key); }
async function openPath(p) {
  const r = await api("/api/open", { path: p });
  toast(r.ok ? "已打开：" + p : (r.err || "打不开这个位置"));
}
function shareLink(u) {
  let url = (u.shareUrl || "").trim();
  if (url && u.sharePwd && !/[?&]pwd=/.test(url)) url += (url.includes("?") ? "&" : "?") + "pwd=" + encodeURIComponent(u.sharePwd.trim());
  return url;
}
function panPathURL(p) { return "https://pan.baidu.com/disk/main#/index?category=all&path=" + encodeURIComponent(p.trim()); }
async function openURL(u) { const r = await api("/api/openurl", { url: u }); if (!r.ok) toast(r.err || "打不开链接"); }
async function copyText(s, msg) {
  try { await navigator.clipboard.writeText(s); toast(msg || "已复制"); }
  catch (e) { const ta = document.createElement("textarea"); ta.value = s; document.body.append(ta); ta.select(); document.execCommand("copy"); ta.remove(); toast(msg || "已复制"); }
}
async function openPan(a) {
  const u = a.user;
  if (u.shareUrl) {
    if (u.sharePwd) await copyText(u.sharePwd, "提取码 " + u.sharePwd + " 已复制，正在打开网盘");
    openURL(shareLink(u));
  } else if (u.panPath) openURL(panPathURL(u.panPath));
}
function boothItemURL(id) { return "https://booth.pm/ja/items/" + id; }
function boothSearchURL(q) { return "https://booth.pm/ja/search/" + encodeURIComponent((q || "").trim()); }
function boothURL(a) {
  if (!a.boothId) return boothSearchURL(a.boothQuery || a.name);
  return (a.user.boothUrl && a.user.boothUrl.includes(a.boothId) && a.user.boothUrl) || (a.booth && a.booth.url) || boothItemURL(a.boothId);
}
function panLine(a) {
  const l = a.pan;
  if (!l) return S.data.tasks && (S.data.tasks.find(t => t.name === "pan") || {}).running ? "正在读取网盘分享…" : "百度网盘分享";
  if (l.err && !l.files) return "网盘：" + l.err;
  return `百度网盘，${l.count} 个文件，${fmtSize(l.size)}`;
}
function openBuyPage(a) { if (a.purchase) openURL(a.purchase.pageUrl); }
async function startSync() {
  const r = await api("/api/purchases/sync", {});
  toast(r.ok ? "已打开 Booth 窗口：没登录的话在里面登录一次，之后会自动读取" : "正在同步中，请稍候", 5000);
  S.data.purchaseBusy = true; renderSide(); poll(true);
}
async function pickInto(title) {
  const r = await api("/api/pickfolder", { title });
  if (r.ok) return r.path;
  if (!r.cancelled) toast(r.err || "打不开选择窗口", 4000);
  return "";
}

// ---------- drawer ----------
function parseShareText(t) {
  const out = {};
  const m = t.match(/https?:\/\/pan\.baidu\.com\/s\/[A-Za-z0-9_\-]+(\?pwd=[A-Za-z0-9]+)?/) || t.match(/https?:\/\/[^\s]+/);
  if (m) out.url = m[0];
  const p = t.match(/(?:提取码|密码|pwd)[:：=\s]*([A-Za-z0-9]{4})/i) || (out.url || "").match(/[?&]pwd=([A-Za-z0-9]+)/);
  if (p) out.pwd = p[1];
  return out;
}

function taskRunning(name) { return !!((S.data.tasks || []).find(t => t.name === name && t.running)); }
function panTree(files, depth, prefix) {
  const few = (files || []).length <= 3;
  return (files || []).map(f => {
    if (!f.d) return `<div class="pf"><span>${esc(f.n)}</span><span class="s">${fmtSize(f.s)}</span></div>`;
    const path = prefix + "/" + f.n;
    const open = S.panOpen.has(path) || (depth === 0 && few && !S.panClosed.has(path));
    return `<details data-pp="${esc(path)}"${open ? " open" : ""}><summary>${ICON.folder}<span>${esc(f.n)}</span>${f.p ? `<span class="muted small">（没有全部列出）</span>` : ""}</summary>
      <div class="kids">${panTree(f.c, depth + 1, path)}</div></details>`;
  }).join("");
}
function panSec(a) {
  const u = a.user, l = a.pan, running = taskRunning("pan");
  let list = "";
  if (u.shareUrl) {
    if (!l) list = `<div class="muted small panmsg">${running ? "正在读取分享里的文件…" : "保存后会自动读取分享里的文件。"}</div>`;
    else if (l.err && !(l.files || []).length) list = `<div class="small err panmsg">读取失败：${esc(l.err)}</div>`;
    else list = `<div class="panhead"><b>${l.count} 个文件</b><span>${fmtSize(l.size)}</span>${l.truncated ? `<span class="muted small">文件太多，只列出了一部分</span>` : ""}<span class="muted small right">读取于 ${esc(fmtTime(l.fetched))}</span></div>
      <div class="pantree">${panTree(l.files, 0, "")}</div>${l.err ? `<div class="small err">上次重新读取失败：${esc(l.err)}</div>` : ""}`;
  }
  return `<div class="sec"><h4 class="withact">网盘${u.shareUrl ? `<button class="h4act" data-d="panrefresh"${running ? " disabled" : ""}>${running ? "读取中…" : "重新读取"}</button>` : ""}</h4>
      <div class="field"><label>粘贴分享</label><input id="f_paste" placeholder="把百度网盘的分享文字整段粘贴到这里，自动识别链接和提取码"></div>
      <div class="field"><label>分享链接</label><input id="f_shareUrl" value="${esc(u.shareUrl || "")}" placeholder="https://pan.baidu.com/s/..."></div>
      <div class="field"><label>提取码</label><div class="inline"><input id="f_sharePwd" value="${esc(u.sharePwd || "")}" placeholder="没有提取码就留空"><button class="btn" data-d="copypwd">复制</button></div></div>
      <div class="field"><label>网盘路径</label><div class="inline"><input id="f_panPath" value="${esc(u.panPath || "")}" placeholder="/VRChat素材/衣服/...（可不填）"><button class="btn" data-d="openpanpath">打开</button></div></div>
      ${list}
    </div>`;
}
const BOOTH_SRC = { user: "你手动关联的", name: "从文件夹名里的商品编号识别", url: "从素材里的 Booth 快捷方式识别", library: "来自 Booth 已购记录", auto: "按名称自动匹配的，可能不准，请确认一下" };
function boothSec(a) {
  const b = a.booth, src = a.boothSrc;
  let h = `<div class="sec"><h4>Booth 商品</h4>`;
  if (a.boothId) {
    const imgs = (b && b.images || []).slice(0, 6);
    h += `<div class="bitem">
      ${imgs.length ? `<div class="bimgs">${imgs.map(u => `<img loading="lazy" src="/rthumb?u=${encodeURIComponent(boothImg(u, false))}" data-big="${esc(boothImg(u, true))}" alt="" title="点击放大">`).join("")}</div>` : ""}
      <div class="bname">${esc(b && b.name ? b.name : "Booth #" + a.boothId)}</div>
      <div class="muted small">${[b && b.shop, b && b.price, b && b.category].filter(Boolean).map(esc).join("　") || (taskRunning("booth") ? "正在读取商品信息…" : "")}</div>
      ${b && (b.tags || []).length ? `<div class="btags">${b.tags.slice(0, 14).map(t => `<span>${esc(t)}</span>`).join("")}</div>` : ""}
      ${BOOTH_SRC[src] ? `<div class="hint bsrc${src === "auto" ? " warn" : ""}">${esc(BOOTH_SRC[src])}</div>` : ""}
      <div class="btnrow">
        <button class="btn small" data-d="booth">打开商品页</button>
        ${src === "auto" ? `<button class="btn small" data-d="bconfirm">是这个</button><button class="btn small" data-d="breject">不是这个</button>` : ""}
        <button class="btn small" data-d="bchange">换一个</button>
        ${src === "user" ? `<button class="btn small ghost" data-d="bunlink">取消关联</button>` : ""}
      </div></div>
      ${b && b.desc ? descBlock(a) : ""}
      ${S.bs && S.bs.key === a.key ? searchPanel(a) : ""}`;
  } else {
    h += `<div class="muted small" style="margin-bottom:8px">${a.user.noBooth ? "已标记为不是 Booth 商品。需要的话也可以搜索关联。" : "还没关联 Booth 商品。挑对的那个点「就是这个」，会补上封面、商品说明和中文名。"}</div>` + searchPanel(a);
  }
  return h + `</div>`;
}
// Booth image urls: "/c/<size>/" picks a crop; without it the full picture is served.
function boothImg(u, big) {
  const m = /^(https:\/\/booth\.pximg\.net)\/c\/[0-9a-z_]+\//.exec(u || "");
  if (!m) return u;
  return big ? u.replace(m[0], m[1] + "/") : u.replace(m[0], m[1] + "/c/300x300_a2_g5/");
}
function showBig(list, i) {
  let lb = $("#lightbox");
  if (!lb) { lb = document.createElement("div"); lb.id = "lightbox"; document.body.appendChild(lb); }
  const n = list.length; i = (i + n) % n;
  lb.innerHTML = `<img src="/rthumb?u=${encodeURIComponent(list[i])}" alt="">${n > 1 ? `<button class="lbnav prev" aria-label="上一张">‹</button><button class="lbnav next" aria-label="下一张">›</button><div class="lbcount">${i + 1} / ${n}</div>` : ""}`;
  lb.classList.add("on");
  lb.onclick = e => {
    e.stopPropagation();
    if (e.target.classList.contains("prev")) return showBig(list, i - 1);
    if (e.target.classList.contains("next")) return showBig(list, i + 1);
    lb.classList.remove("on");
  };
  lb.dataset.i = i; S.lb = { list, i };
}
function searchPanel(a) {
  const bs = S.bs && S.bs.key === a.key ? S.bs : { key: a.key, q: a.boothQuery || a.name, hits: a.boothHits || null };
  let res = "";
  if (bs.loading) res = `<div class="muted small">正在搜索 Booth…</div>`;
  else if (bs.err) res = `<div class="small err">${esc(bs.err)}</div>`;
  else if (bs.hits && !bs.hits.length) res = `<div class="muted small">没搜到，换个关键词试试。</div>`;
  else if (bs.hits) res = `<div class="bhits">${bs.hits.slice(0, 12).map(h => `<div class="bhit${h.full ? " full" : ""}">
      <img loading="lazy" src="/rthumb?u=${encodeURIComponent(h.thumb || "")}" alt="">
      <div class="t" title="${esc(h.name)}">${esc(h.name)}</div>
      <div class="m">${esc(h.shop || h.shopSub || "")}${h.price ? "　" + esc(h.price) : ""}</div>
      <div class="m">${esc(h.category || "")}</div>
      <div class="hb"><button class="btn small" data-link="${esc(h.id)}">就是这个</button><button class="btn small ghost" data-url="${esc(boothItemURL(h.id))}">看看</button></div>
    </div>`).join("")}</div>`;
  return `<div class="bsearch"><input id="bs_q" value="${esc(bs.q || "")}" placeholder="搜索 Booth 商品名">
      <button class="btn small" data-d="bsearch">${ICON.search}<span>搜索</span></button>
      <button class="btn small ghost" data-d="bopen">在浏览器里搜</button>
      ${!a.boothId && !a.user.noBooth ? `<button class="btn small ghost" data-d="bnone">不是 Booth 商品</button>` : ""}</div>${res}`;
}
function descBlock(a) {
  const b = a.booth, d = S.desc[a.key] || {};
  const text = d.showZh && d.zh ? d.zh : b.desc;
  return `<details class="bdesc" data-desc="${esc(a.key)}"${d.open ? " open" : ""}><summary>商品说明${d.showZh ? "（中文翻译）" : ""}</summary>
    <div class="desc">${esc(text)}</div>
    <div class="btnrow"><button class="btn small" data-d="dtrans"${d.loading ? " disabled" : ""}>${d.loading ? "翻译中…" : d.showZh ? "看原文" : "翻译成中文"}</button></div></details>`;
}
async function runBoothSearch(a, q) {
  S.bs = { key: a.key, q, loading: true, hits: null };
  if (S.openKey === a.key) renderDrawer(a.key, true);
  try {
    const r = await api("/api/boothsearch", { q, name: a.autoName || a.name, cat: a.category });
    S.bs = { key: a.key, q, hits: r.ok ? r.hits : null, err: r.ok ? "" : r.err };
  } catch (e) { S.bs = { key: a.key, q, err: "搜索失败" }; }
  if (S.openKey === a.key) renderDrawer(a.key, true);
}
function shopOf(a) { return (a.booth && a.booth.shop) || (a.purchase && a.purchase.shop) || ""; }
function purchaseSec(a) {
  const p = a.purchase;
  const orders = (p.orders || []).map(o => `<div class="order"><span class="d">${esc(o.date || "日期未知")}</span><span class="muted">订单 #${esc(o.id)}</span>
    <button class="btn small" data-url="${esc(o.url)}">打开订单页</button></div>`).join("");
  const files = (p.files || []).filter(Boolean);
  return `<div class="sec"><h4>Booth 已购${p.gift ? "（礼物）" : ""}</h4>
    ${orders || `<div class="muted small">没读到订单号，可以在已购列表里找到它。</div>`}
    <div class="order"><span class="muted small">所有已购商品都能在 Booth 的「ライブラリ」页重新下载。</span><button class="btn small" data-url="${esc(p.libraryUrl)}">打开已购列表</button></div>
    ${files.length ? `<div class="files">${files.map(f => `<span class="file">${esc(f)}</span>`).join("")}</div>` : ""}
    ${p.matched ? `<div class="hint" style="margin-top:6px">这个素材是按下载文件名和 Booth 已购对上的。</div>` : ""}
  </div>`;
}

function renderDrawer(key, keepScroll) {
  const a = findAsset(key); if (!a) { closeDrawer(); return; }
  if (S.openKey !== key) { S.panOpen = new Set(); S.panClosed = new Set(); S.dirty = new Set(); S.coverPick = null; }
  const u = a.user; const d = S.data;
  const dr = $("#drawer"); const scroll = keepScroll ? ($(".dbody", dr) || {}).scrollTop : 0;
  const kept = {}; let focusId = null, selS = null, selE = null;
  if (keepScroll && S.openKey === key) {
    // keep only what the user has typed and not saved yet; other fields show the saved value
    dr.querySelectorAll("input[id], textarea[id], select[id]").forEach(el => { if (el.type !== "hidden" && S.dirty.has(el.id)) kept[el.id] = el.value; });
    const ae = document.activeElement;
    if (ae && dr.contains(ae) && ae.id) { focusId = ae.id; try { selS = ae.selectionStart; selE = ae.selectionEnd; } catch (e) {} }
  }
  const b = a.booth;
  const catOpts = d.categories.map(c => `<option${c === a.category ? " selected" : ""}>${esc(c)}</option>`).join("");
  dr.innerHTML = `
  <div class="dhead">
    <div class="dcover">${coverHTML(a, true)}</div>
    <div style="min-width:0;flex:1">
      <h2>${esc(a.name)}</h2>
      ${!S.data.settings.hideZh && a.nameZh ? `<div class="zh big">${esc(a.nameZh)}</div>` : ""}
      ${b && b.name && b.name !== a.name ? `<div class="sub">${esc(b.name)}</div>` : ""}
      <div class="sub">${shopOf(a) ? esc(shopOf(a)) + "　" : ""}${b && b.price ? esc(b.price) + "　" : ""}${a.virtual ? esc(orderLine(a.purchase)) + "，本地还没有" : a.panOnly ? esc(panLine(a)) : `${fmtSize(a.size)}　${a.files} 个文件${a.packages ? `　${a.packages} 个 unitypackage` : ""}`}</div>
      ${b && b.err && !a.virtual ? `<div class="small err">Booth：${esc(b.err)}</div>` : ""}
      <div class="rowbtn">
        ${a.virtual ? `<button class="btn primary" data-d="buypage">${ICON.download}<span style="margin-left:6px">打开下载页</span></button>`
          : a.panOnly ? `<button class="btn primary" data-d="pan">${ICON.cloud}<span style="margin-left:6px">打开网盘分享</span></button>`
          : `<button class="btn primary" data-d="open">${ICON.folder}<span style="margin-left:6px">打开所在位置</span></button>`}
        ${hasShare(a) && !a.panOnly ? `<button class="btn" data-d="pan">打开网盘</button>` : ""}
        ${a.purchase && !a.virtual ? `<button class="btn" data-d="buypage">Booth 购买页</button>` : ""}
        <button class="btn" data-d="booth">${a.boothId ? "Booth 商品页" : "在 Booth 搜索"}</button>
      </div>
    </div>
  </div>
  <div class="dbody">
    ${a.panOnly ? "" : a.virtual ? `<div class="sec"><h4>保存位置</h4><div class="muted small">素材文件夹里还没有它。下载后放进素材文件夹，重新扫描就会自动对上（按 Booth 商品编号或下载文件名）。</div></div>`
    : `<div class="sec"><h4>保存位置</h4>
      ${(a.locations || []).map((l, i) => `<div class="loc"><span class="k">${{ dir: "文件夹", zip: "zip", rar: "rar", "7z": "7z", unitypackage: "unitypackage", file: "文件" }[l.kind] || l.kind}</span>
        <span class="p">${esc(l.path)}</span><span class="s">${fmtSize(l.size)}</span>
        <button class="act" data-loc="${i}" title="打开">${ICON.folder}</button><button class="act" data-copy="${i}" title="复制路径">${ICON.copy}</button></div>`).join("")}
    </div>`}
    ${a.purchase ? purchaseSec(a) : ""}
    ${panSec(a)}
    ${boothSec(a)}
    <div class="sec"><h4>整理</h4>
      <div class="field"><label>显示名称</label><input id="f_name" value="${esc(u.name || "")}" placeholder="${esc(a.autoName)}"></div>
      <div class="field"><label>中文名</label><input id="f_nameZh" value="${esc(u.nameZh || "")}" placeholder="${esc(a.nameZh || "自动翻译（没有就是不需要翻译）")}"></div>
      <div class="field"><label>分类</label><select id="f_cat"><option value="">自动（${esc(a.autoCategory)}）</option>${d.categories.map(c => `<option${c === u.category ? " selected" : ""}>${esc(c)}</option>`).join("")}</select></div>
      <div class="field"><label>适配素体</label><input id="f_bases" value="${esc((u.bases || a.bases || []).join(", "))}" placeholder="Plum, Chocolat"></div>
      ${u.bases ? `<div class="field"><span></span><span class="hint">自动识别：${esc((a.autoBases || []).join(", ") || "无")}</span></div>` : ""}
      <div class="field"><label>标签</label><input id="f_tags" value="${esc((u.tags || []).join(", "))}" placeholder="用逗号分隔"></div>
      ${a.virtual ? `<input id="f_booth" type="hidden" value="">` : `<div class="field"><label>Booth 链接</label><input id="f_booth" value="${esc(u.boothUrl || "")}" placeholder="${a.boothId ? "已自动识别 #" + esc(a.boothId) : "https://booth.pm/ja/items/..."}"></div>`}
      <div class="field"><label>备注</label><textarea id="f_notes" placeholder="版本、用法、注意事项…">${esc(u.notes || "")}</textarea></div>
      ${(a.localCovers || []).length > 1 || u.cover ? `<div class="field"><label>封面</label><div class="covers">${(a.localCovers || []).map(c => `<img src="/thumb?w=160&p=${encodeURIComponent(c)}" data-cover="${esc(c)}" class="${u.cover === c ? "on" : ""}" title="${esc(c)}">`).join("")}</div></div>` : ""}
    </div>
    ${!isLocal(a) ? "" : `<div class="sec"><h4>工程使用</h4>
      ${(a.usage || []).length ? `<div class="uses">${a.usage.map(x => `<div class="use"><span class="st ${x.status}">${x.status === "used" ? "在用" : "部分"}</span><span>${esc(x.project)}</span><span class="pct">${x.matched}/${x.total} 个资源（${Math.round(x.ratio * 100)}%）</span></div>`).join("")}</div>`
        : `<div class="muted small">${a.guidCount ? "扫描过的工程里都没有用到它。" : (a.packages ? "还没分析，扫描完成后会显示。" : "这个素材里没有 unitypackage，无法判断哪些工程在用。")}</div>`}
    </div>`}
    ${a.panOnly ? `<div class="sec"><h4>管理</h4><div style="display:flex;gap:8px;flex-wrap:wrap"><button class="btn" data-d="hide">${u.hidden ? "取消隐藏" : "隐藏"}</button><button class="btn" data-d="pandelete">从素材库删除（网盘里的文件不受影响）</button></div></div>`
    : a.virtual ? `<div class="sec"><h4>不想看到它？</h4><button class="btn" data-d="hide">${u.hidden ? "取消隐藏" : "隐藏"}</button></div>` : `<div class="sec"><h4>识别不对？</h4>
      <div class="muted small" style="margin-bottom:8px">原始名称：${esc(a.rawName)}${(a.hints || []).length ? `　所在分类文件夹：${esc(a.hints.join(" / "))}` : ""}</div>
      <div style="display:flex;gap:8px;flex-wrap:wrap">
        ${a.hasDir ? `<button class="btn" data-d="split" title="这个文件夹其实装着好几个不同的素材">拆成多个素材</button>` : ""}
        <button class="btn" data-d="hide">${u.hidden ? "取消隐藏" : "隐藏"}</button>
        <button class="btn" data-d="ignore" title="以后扫描都跳过这个位置">不再收录</button>
        ${a.boothId ? `<button class="btn" data-d="rebooth">重新抓取 Booth 信息</button>` : ""}
      </div>
    </div>`}
  </div>
  <div class="dfoot"><button class="btn ghost" data-d="close">关闭</button><span class="spacer"></span><button class="btn primary" data-d="save">保存</button></div>`;
  for (const id in kept) { const el = document.getElementById(id); if (el) el.value = kept[id]; }
  if (S.coverPick) dr.querySelectorAll(".covers img").forEach(i => i.classList.toggle("on", i.dataset.cover === S.coverPick));
  if (focusId) { const el = document.getElementById(focusId); if (el) { el.focus(); try { if (selS !== null) el.setSelectionRange(selS, selE); } catch (e) {} } }
  if (keepScroll) $(".dbody", dr).scrollTop = scroll;
  S.openKey = key;
  if (!a.boothId && a.boothQuery && (!S.bs || S.bs.key !== a.key) && !(a.boothHits || []).length && !a.user.noBooth) runBoothSearch(a, a.boothQuery);
  dr.classList.add("on"); dr.setAttribute("aria-hidden", "false"); $("#scrim").classList.add("on");
  $("#f_paste").addEventListener("input", e => {
    const r = parseShareText(e.target.value);
    if (r.url) { $("#f_shareUrl").value = r.url.replace(/[?&]pwd=[A-Za-z0-9]+/, ""); S.dirty.add("f_shareUrl"); }
    if (r.pwd) { $("#f_sharePwd").value = r.pwd; S.dirty.add("f_sharePwd"); }
    if (r.url || r.pwd) toast("已识别分享链接" + (r.pwd ? "和提取码" : ""));
  });
}
function closeDrawer() {
  S.openKey = null; $("#drawer").classList.remove("on"); $("#drawer").setAttribute("aria-hidden", "true");
  if (!$("#modal").classList.contains("on")) $("#scrim").classList.remove("on");
}
function collectUser(a) {
  const split = s => s.split(/[,，、;；]/).map(x => x.trim()).filter(Boolean);
  const u = Object.assign({}, a.user);
  u.name = $("#f_name").value.trim();
  u.nameZh = $("#f_nameZh").value.trim();
  u.category = $("#f_cat").value;
  const bases = split($("#f_bases").value);
  const autoB = (a.autoBases || []).join(",");
  u.bases = (bases.join(",") === autoB && !a.user.bases) ? null : bases;
  u.tags = split($("#f_tags").value);
  u.boothUrl = $("#f_booth").value.trim();
  u.notes = $("#f_notes").value;
  u.shareUrl = $("#f_shareUrl").value.trim();
  u.sharePwd = $("#f_sharePwd").value.trim();
  u.panPath = $("#f_panPath").value.trim();
  const sel = $(".covers img.on"); u.cover = sel ? sel.dataset.cover : (a.user.cover || "");
  return u;
}
async function saveUser(a, u, msg) {
  await api("/api/user", { key: a.key, user: u });
  S.dirty = new Set(); S.coverPick = null; // everything typed so far has just been saved
  toast(msg || "已保存");
  await load();
}

// ---------- settings ----------
function openSettings() {
  const s = S.data.settings; const ov = S.data.overrides || {};
  const m = $("#modal");
  m.innerHTML = `<div class="mhead">设置</div><div class="mbody">
    <div class="row"><label>素材文件夹（一行一个）</label><textarea id="s_roots">${esc((s.roots || []).join("\n"))}</textarea>
      <button class="btn small addbtn" data-pickto="s_roots">${ICON.folder}<span>添加文件夹…</span></button></div>
    <div class="row"><label>Unity 工程所在文件夹（一行一个，用来判断哪些工程在用某个素材）</label><textarea id="s_proj">${esc((s.projectRoots || []).join("\n"))}</textarea>
      <button class="btn small addbtn" data-pickto="s_proj">${ICON.folder}<span>添加文件夹…</span></button></div>
    <div class="row"><label class="check" style="padding:0"><input type="checkbox" id="s_rescan"${s.manualRescan ? "" : " checked"}> 每次打开时在后台重新扫描</label></div>
    <div class="row"><label class="check" style="padding:0"><input type="checkbox" id="s_zh"${s.hideZh ? "" : " checked"}> 名称下面显示简体中文翻译</label></div>
    <div class="row"><label class="check" style="padding:0"><input type="checkbox" id="s_match"${s.noAutoMatch ? "" : " checked"}> 没有封面的素材，自动按名称在 Booth 上找（名字对得上才会关联）</label></div>
    <div class="row"><label>界面打开方式</label>
      <select id="s_win"><option value="">软件自己的窗口（推荐）</option><option value="tab"${s.windowMode === "tab" ? " selected" : ""}>默认浏览器里的标签页</option></select>
      <div class="muted small" style="margin-top:4px">商品页、订单页、网盘链接都用你的默认浏览器（${esc(S.data.defaultBrowser || "未知")}）打开。下次启动生效。</div></div>
    <div class="row"><label>访问 Booth 用的代理（留空则用系统代理，例如 127.0.0.1:7890）</label><input id="s_proxy" value="${esc(s.proxy || "")}"></div>
    <div class="row"><label class="check" style="padding:0"><input type="checkbox" id="s_auto"${s.autoBooth ? " checked" : ""}> 扫描后自动从 Booth 抓封面和商品名</label></div>
    <div class="row"><label>素体识别表（一行一个：显示名=别名1|别名2）</label><textarea id="s_bases" style="min-height:120px">${esc((s.bases || []).join("\n"))}</textarea></div>
    ${Object.keys(ov).length ? `<div class="row"><label>手动调整过的识别</label>${Object.entries(ov).map(([p, v]) => `<div class="ov"><span>${esc(p)}</span><span class="muted" style="flex:none">${{ split: "拆成多个", ignore: "不收录", asset: "合并为一个" }[v] || v}</span><button class="btn" data-ov="${esc(p)}">撤销</button></div>`).join("")}</div>` : ""}
    <div class="row"><label>Booth 已购</label>
      <div class="muted small" style="margin-bottom:8px">${S.data.purchaseSync ? `已同步 ${S.data.purchaseCount} 件，上次 ${esc(fmtTime(S.data.purchaseSync))}。` : "还没同步过。"}登录只保存在本软件自己的浏览器窗口里，不会读你平时浏览器的账号。</div>
      <div class="btnrow"><button class="btn small" data-m="sync">同步 Booth 已购</button><button class="btn small" data-m="forget">退出 Booth 登录</button>${S.data.purchaseSync ? `<button class="btn small" data-m="clearp">清空已购记录</button>` : ""}</div></div>
    ${S.data.canShortcut ? `<div class="row"><label>快捷方式</label><button class="btn small" data-m="shortcut">在桌面创建快捷方式</button></div>` : ""}
    <div class="row muted small">数据保存在：${esc(S.data.dataDir)}</div>
  </div><div class="mfoot"><button class="btn ghost" data-m="close">取消</button><button class="btn primary" data-m="save">保存并重新扫描</button></div>`;
  m.classList.add("on"); $("#scrim").classList.add("on");
}
function closeModal(force) {
  if (S.setup && !force) return; // the first-run screen has to be finished
  $("#modal").classList.remove("on"); if (!S.openKey) $("#scrim").classList.remove("on");
}

// ---------- netdisk-only asset ----------
function openPanAdd() {
  const m = $("#modal");
  m.innerHTML = `<div class="mhead">添加网盘素材</div><div class="mbody">
    <p class="lead">只存在百度网盘里、电脑上没有的素材，也能放进素材库。填好分享链接后，软件会读出分享里有哪些文件。</p>
    <div class="row"><label>分享链接（可以把分享文字整段粘贴进来）</label><textarea id="pa_text" placeholder="链接: https://pan.baidu.com/s/1xxxx 提取码: abcd"></textarea></div>
    <div class="row"><label>提取码（没有就留空）</label><input id="pa_pwd" placeholder="4 位，可以不填"></div>
    <div class="row"><label>名称（可以不填，默认用分享里的文件夹名）</label><input id="pa_name"></div>
  </div><div class="mfoot"><button class="btn ghost" data-m="close">取消</button><button class="btn primary" data-m="panadd">添加</button></div>`;
  m.classList.add("on"); $("#scrim").classList.add("on");
  $("#pa_text").focus();
  $("#pa_text").addEventListener("input", e => { const r = parseShareText(e.target.value); if (r.pwd && !$("#pa_pwd").value) $("#pa_pwd").value = r.pwd; });
}

// ---------- first run ----------
async function openSetup() {
  let det = { roots: [], projects: [] };
  try { det = await api("/api/detect"); } catch (e) {}
  S.setup = { roots: det.roots || [], projects: det.projects || [] };
  renderSetup();
}
function renderSetup() {
  const st = S.setup; const m = $("#modal");
  const list = kind => (st[kind].length ? st[kind].map((c, i) => `<label class="cand"><input type="checkbox" data-sk="${kind}" data-si="${i}"${c.checked ? " checked" : ""}>
      <span class="p">${esc(c.path)}</span>${c.note ? `<span class="note">${esc(c.note)}</span>` : ""}</label>`).join("")
    : `<div class="muted small cand-empty">没有自动找到，用下面的按钮选择，或者粘贴路径。</div>`);
  const adder = (kind, label) => `<div class="addrow"><button class="btn small" data-spick="${kind}">${ICON.folder}<span>${label}</span></button>
    <input data-spath="${kind}" placeholder="或者粘贴路径后按回车"></div>`;
  m.innerHTML = `<div class="mhead">开始之前，选一下文件夹</div><div class="mbody">
    <p class="lead">素材库会扫描这些文件夹，把每个素材做成一张卡片。点卡片就能打开它所在的位置。</p>
    <div class="row"><label>放素材的文件夹（百度网盘下载、解压出来的素材等）</label>${list("roots")}${adder("roots", "添加素材文件夹…")}</div>
    <div class="row"><label>Unity 工程（可以不选。选了能看出哪个工程用到了哪个素材）</label>${list("projects")}${adder("projects", "添加工程或工程所在文件夹…")}</div>
    <p class="muted small">以后可以在右上角的设置里改。</p>
  </div><div class="mfoot"><button class="btn primary" data-setup="go">开始扫描</button></div>`;
  m.dataset.kind = "setup"; m.classList.add("on"); $("#scrim").classList.add("on");
}
function setupAdd(kind, path) {
  path = (path || "").trim().replace(/^"|"$/g, ""); if (!path) return;
  const list = S.setup[kind];
  const ex = list.find(c => c.path.toLowerCase() === path.toLowerCase());
  if (ex) ex.checked = true; else list.push({ path, note: "", checked: true });
  renderSetup();
}
async function finishSetup() {
  const roots = S.setup.roots.filter(c => c.checked).map(c => c.path);
  const projects = S.setup.projects.filter(c => c.checked).map(c => c.path);
  if (!roots.length) { toast("至少选一个放素材的文件夹"); return; }
  const s = Object.assign({}, S.data.settings, { roots, projectRoots: projects, setupDone: true });
  await api("/api/settings", { settings: s });
  S.setup = null; $("#modal").dataset.kind = ""; closeModal(true);
  toast("开始扫描，第一次会久一点"); await load(); poll(true);
}

// ---------- events ----------
document.addEventListener("click", async e => {
  const t = e.target;
  if (t.closest("#btnSync")) { startSync(); return; }
  if (t.closest("#btnSyncCancel")) { await api("/api/purchases/cancel", {}); toast("正在取消，Booth 窗口会自动关闭"); poll(true); return; }
  const side = t.closest(".side [data-cat], .side [data-usage], .side [data-share], .side [data-root], .side [data-base], .side [data-purchase]");
  if (side) {
    const ds = side.dataset;
    if (ds.cat !== undefined) S.cat = ds.cat;
    if (ds.usage !== undefined) S.usage = ds.usage;
    if (ds.share !== undefined) S.share = ds.share;
    if (ds.root !== undefined) S.root = ds.root;
    if (ds.purchase !== undefined) S.purchase = ds.purchase;
    if (ds.base !== undefined) { S.bases.has(ds.base) ? S.bases.delete(ds.base) : S.bases.add(ds.base); }
    renderSide(); renderGrid(); $(".main").scrollTop = 0; return;
  }
  if (t.closest("#clearFilters")) { S.q = ""; $("#q").value = ""; S.cat = "全部"; S.bases.clear(); S.usage = S.share = S.root = S.purchase = "all"; renderSide(); renderGrid(); return; }
  if (t.closest("#emptySettings") || t.closest("#btnSettings")) { openSettings(); return; }
  if (t.closest("#btnPanAdd")) { openPanAdd(); return; }
  if (t.closest("#btnScan")) { const r = await api("/api/scan", { scan: true, usage: true, booth: S.data.settings.autoBooth }); toast(r.ok ? "开始重新扫描" : "正在扫描中，请稍候"); poll(true); return; }

  const card = t.closest(".card");
  if (card && !t.closest(".drawer")) {
    const a = findAsset(card.dataset.key); if (!a) return;
    const act = t.closest("[data-act]");
    const what = act ? act.dataset.act : "open";
    if (what === "open") { if (a.virtual) openBuyPage(a); else if (a.panOnly) openPan(a); else { const l = (a.locations || [])[0]; if (l) openPath(l.path); } }
    else if (what === "buypage") openBuyPage(a);
    else if (what === "pan") openPan(a);
    else if (what === "booth") openURL(boothURL(a));
    else if (what === "edit") renderDrawer(a.key);
    return;
  }

  const dr = t.closest(".drawer");
  if (dr && S.openKey) {
    const a = findAsset(S.openKey); if (!a) return;
    if (t.matches(".bimgs img")) {
      const all = [...dr.querySelectorAll(".bimgs img")].map(i => i.dataset.big);
      return showBig(all, all.indexOf(t.dataset.big));
    }
    const btn = t.closest("[data-d],[data-loc],[data-copy],[data-cover],[data-url],[data-link]");
    if (!btn) return;
    if (btn.dataset.url !== undefined) return openURL(btn.dataset.url);
    if (btn.dataset.link !== undefined) {
      const u = collectUser(a); u.boothUrl = boothItemURL(btn.dataset.link); u.noBooth = false; S.bs = null;
      return saveUser(a, u, "已关联，正在读取商品信息");
    }
    if (btn.dataset.loc !== undefined) return openPath(a.locations[+btn.dataset.loc].path);
    if (btn.dataset.copy !== undefined) return copyText(a.locations[+btn.dataset.copy].path, "已复制路径");
    if (btn.dataset.cover !== undefined) { const on = btn.classList.contains("on"); dr.querySelectorAll(".covers img").forEach(i => i.classList.remove("on")); if (!on) btn.classList.add("on"); S.coverPick = on ? null : btn.dataset.cover; return; }
    switch (btn.dataset.d) {
      case "close": return closeDrawer();
      case "open": { const l = (a.locations || [])[0]; if (l) openPath(l.path); return; }
      case "pan": return openPan(a);
      case "booth": return openURL(boothURL(a));
      case "buypage": return openBuyPage(a);
      case "panrefresh": await api("/api/pan/refresh", { key: a.key }); toast("正在重新读取网盘分享"); poll(true); return;
      case "pandelete": if (!confirm("从素材库删除这个网盘素材？网盘里的文件不受影响。")) return;
        await api("/api/user", { key: a.key, delete: true }); closeDrawer(); toast("已删除"); load(); return;
      case "bsearch": return runBoothSearch(a, ($("#bs_q").value || "").trim());
      case "bopen": return openURL(boothSearchURL(($("#bs_q") || {}).value || a.boothQuery || a.name));
      case "bchange": return runBoothSearch(a, a.boothQuery || a.name);
      case "bconfirm": { const u = collectUser(a); u.boothUrl = boothItemURL(a.boothId); u.noBooth = false; return saveUser(a, u, "已确认"); }
      case "breject": { const u = collectUser(a); u.boothUrl = ""; u.noBooth = true; S.bs = { key: a.key, q: a.boothQuery || a.name, hits: (a.boothHits || []).filter(h => h.id !== a.boothId) }; return saveUser(a, u, "已取消自动匹配，可以在下面挑对的"); }
      case "bunlink": { const u = collectUser(a); u.boothUrl = ""; u.noBooth = true; return saveUser(a, u, "已取消关联"); }
      case "bnone": { const u = collectUser(a); u.noBooth = true; return saveUser(a, u, "已标记为不是 Booth 商品"); }
      case "dtrans": {
        const dd = S.desc[a.key] = S.desc[a.key] || {}; dd.open = true;
        if (dd.zh) { dd.showZh = !dd.showZh; return renderDrawer(a.key, true); }
        dd.loading = true; renderDrawer(a.key, true);
        const r = await api("/api/translate", { text: a.booth.desc });
        dd.loading = false; if (r.ok) { dd.zh = r.text; dd.showZh = true; } else toast(r.err || "翻译失败", 3500);
        return renderDrawer(a.key, true);
      }
      case "copypwd": { const v = $("#f_sharePwd").value.trim(); if (v) copyText(v, "提取码已复制"); return; }
      case "openpanpath": { const v = $("#f_panPath").value.trim(); if (v) openURL(panPathURL(v)); return; }
      case "save": return saveUser(a, collectUser(a));
      case "hide": { const u = collectUser(a); u.hidden = !a.user.hidden; return saveUser(a, u, u.hidden ? "已隐藏" : "已取消隐藏"); }
      case "rebooth": { await api("/api/booth", { keys: [a.key], force: true }); toast("正在重新抓取 Booth 信息"); poll(true); return; }
      case "split": case "ignore": {
        const dirLoc = (a.locations || []).find(l => l.kind === "dir") || a.locations[0];
        const verb = btn.dataset.d === "split" ? "拆成多个素材" : "以后不再收录";
        if (!confirm(`把「${dirLoc.path}」${verb}？可以在设置里撤销。`)) return;
        await api("/api/override", { path: dirLoc.path, mode: btn.dataset.d });
        closeDrawer(); toast("已记录，正在重新扫描"); poll(true); return;
      }
    }
    return;
  }

  const m = t.closest(".modal");
  if (m && S.setup) {
    const pk = t.closest("[data-spick]");
    if (pk) { const p = await pickInto(pk.dataset.spick === "roots" ? "选择放素材的文件夹" : "选择 Unity 工程或工程所在的文件夹"); if (p) setupAdd(pk.dataset.spick, p); return; }
    if (t.closest("[data-setup]")) { finishSetup(); return; }
    return;
  }
  if (m) {
    const pt = t.closest("[data-pickto]");
    if (pt) {
      const p = await pickInto(pt.dataset.pickto === "s_roots" ? "选择放素材的文件夹" : "选择 Unity 工程或工程所在的文件夹");
      if (p) { const ta = $("#" + pt.dataset.pickto); ta.value = (ta.value.trim() ? ta.value.trim() + "\n" : "") + p; }
      return;
    }
    const ov = t.closest("[data-ov]");
    if (ov) { await api("/api/override", { path: ov.dataset.ov, mode: "" }); toast("已撤销，正在重新扫描"); closeModal(); poll(true); return; }
    const b = t.closest("[data-m]"); if (!b) return;
    if (b.dataset.m === "close") return closeModal();
    if (b.dataset.m === "panadd") {
      const r0 = parseShareText($("#pa_text").value);
      if (!r0.url) { toast("没认出分享链接，请粘贴 pan.baidu.com/s/ 开头的链接"); return; }
      const r = await api("/api/pan/add", { url: r0.url, pwd: $("#pa_pwd").value.trim() || r0.pwd || "", name: $("#pa_name").value.trim() });
      if (!r.ok) { toast(r.err || "没添加成功"); return; }
      closeModal(); toast("已添加，正在读取分享里的文件"); await load(); renderDrawer(r.key); poll(true); return;
    }
    if (b.dataset.m === "sync") { closeModal(); return startSync(); }
    if (b.dataset.m === "shortcut") { const r = await api("/api/shortcut", {}); toast(r.ok ? "已在桌面创建快捷方式" : "没创建成功：" + (r.err || ""), 3500); return; }
    if (b.dataset.m === "forget" || b.dataset.m === "clearp") {
      const clear = b.dataset.m === "clearp";
      if (!confirm(clear ? "清空已购记录并退出 Booth 登录？本地素材不受影响。" : "退出 Booth 登录？下次同步需要重新登录。")) return;
      const r = await api("/api/purchases/forget", { clear });
      toast(r.ok ? (clear ? "已清空" : "已退出 Booth 登录") : (r.err || "没成功"));
      closeModal(); load(); return;
    }
    if (b.dataset.m === "save") {
      const lines = id => $(id).value.split(/\r?\n/).map(s => s.trim()).filter(Boolean);
      const s = Object.assign({}, S.data.settings, { roots: lines("#s_roots"), projectRoots: lines("#s_proj"), proxy: $("#s_proxy").value.trim(),
        autoBooth: $("#s_auto").checked, manualRescan: !$("#s_rescan").checked, bases: lines("#s_bases"),
        hideZh: !$("#s_zh").checked, noAutoMatch: !$("#s_match").checked, windowMode: $("#s_win").value });
      await api("/api/settings", { settings: s }); closeModal(); toast("设置已保存，正在重新扫描"); poll(true);
    }
    return;
  }
  if (t.closest("#scrim")) { closeModal(); closeDrawer(); }
});
document.addEventListener("change", e => {
  if (e.target.dataset && e.target.dataset.sk && S.setup) { S.setup[e.target.dataset.sk][+e.target.dataset.si].checked = e.target.checked; return; }
  if (e.target.id === "showHidden") { S.showHidden = e.target.checked; renderSide(); renderGrid(); }
  if (e.target.id === "sort") { S.sort = e.target.value; try { localStorage.setItem("vrclib.ui", JSON.stringify({ sort: S.sort })); } catch (x) {} renderGrid(); }
});
document.addEventListener("toggle", e => {
  const d = e.target;
  if (!(d instanceof HTMLDetailsElement)) return;
  if (d.dataset.pp) { if (d.open) { S.panOpen.add(d.dataset.pp); S.panClosed.delete(d.dataset.pp); } else { S.panOpen.delete(d.dataset.pp); S.panClosed.add(d.dataset.pp); } }
  if (d.dataset.desc) { const x = S.desc[d.dataset.desc] = S.desc[d.dataset.desc] || {}; x.open = d.open; }
}, true);
document.addEventListener("keydown", e => {
  if (e.key === "Enter" && e.target.id === "bs_q" && S.openKey) { const a = findAsset(S.openKey); if (a) runBoothSearch(a, e.target.value.trim()); }
});
document.addEventListener("contextmenu", e => {
  const card = e.target.closest(".card"); if (!card) return;
  e.preventDefault(); renderDrawer(card.dataset.key);
});
document.addEventListener("keydown", e => {
  if (e.key === "Enter" && e.target.dataset && e.target.dataset.spath && S.setup) { setupAdd(e.target.dataset.spath, e.target.value); return; }
  if (e.key === "/" && document.activeElement.tagName !== "INPUT" && document.activeElement.tagName !== "TEXTAREA") { e.preventDefault(); $("#q").focus(); }
  const lb = $("#lightbox");
  if (lb && lb.classList.contains("on")) {
    if (e.key === "Escape") lb.classList.remove("on");
    else if (e.key === "ArrowLeft" || e.key === "ArrowRight") showBig(S.lb.list, S.lb.i + (e.key === "ArrowLeft" ? -1 : 1));
    return;
  }
  if (e.key === "Escape") { if ($("#modal").classList.contains("on")) closeModal(); else closeDrawer(); }
  if (e.key === "Enter" && document.activeElement.classList.contains("card")) {
    const a = findAsset(document.activeElement.dataset.key); if (!a) return;
    if (a.virtual) openBuyPage(a); else if (a.panOnly) openPan(a); else if ((a.locations || [])[0]) openPath(a.locations[0].path);
  }
});
let qTimer;
// remember which drawer fields the user has edited, so background refreshes don't overwrite them
["input", "change"].forEach(ev => $("#drawer").addEventListener(ev, e => { if (e.target.id && e.target.matches("input, textarea, select")) S.dirty.add(e.target.id); }));
$("#q").addEventListener("input", e => { clearTimeout(qTimer); qTimer = setTimeout(() => { S.q = e.target.value.trim(); renderSide(); renderGrid(); }, 120); });
$("#sort").value = S.sort;

// ---------- polling ----------
let pollTimer;
async function poll(fast) {
  clearTimeout(pollTimer);
  try {
    const p = await api("/api/progress");
    if (S.data) {
      const flip = !!S.data.purchaseBusy !== !!p.purchaseBusy;
      S.data.tasks = p.tasks; S.data.busy = p.busy; S.data.purchaseBusy = p.purchaseBusy; renderStatus();
      if (flip) renderSide();
    }
    if (p.rev !== S.rev) await load();
    pollTimer = setTimeout(poll, p.busy || p.purchaseBusy || fast ? 1200 : 5000);
  } catch (e) { pollTimer = setTimeout(poll, 4000); }
}
setInterval(() => fetch("/api/ping").catch(() => {}), 15000);
load().then(() => { if (S.data.setupNeeded) openSetup(); poll(); });
