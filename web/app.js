"use strict";
const $ = (s, el = document) => el.querySelector(s);
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const CAT_COLOR = { "素体": "#c78bff", "衣服": "#ff7aa8", "头发": "#ffb547", "配饰": "#6fd3ff", "道具": "#9be36b", "材质": "#ff9f6b",
  "面捕": "#5ee0c5", "插件": "#8aa2ff", "动作": "#f0e26b", "音效": "#b9b2ff", "字体": "#d0d0d0", "其他": "#8b8fa6" };
const ICON = {
  folder: '<svg viewBox="0 0 24 24"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>',
  chev: '<svg viewBox="0 0 24 24"><path d="m6 9 6 6 6-6"/></svg>',
  cloud: '<svg viewBox="0 0 24 24"><path d="M7 18a4.5 4.5 0 0 1-.6-8.96A6 6 0 0 1 18 9.5a4.25 4.25 0 0 1-.5 8.5z"/></svg>',
  bag: '<svg viewBox="0 0 24 24"><path d="M5 8h14l-1 12H6z"/><path d="M9 8V6a3 3 0 0 1 6 0v2"/></svg>',
  edit: '<svg viewBox="0 0 24 24"><path d="M4 20h4L19 9l-4-4L4 16z"/><path d="m13.5 6.5 4 4"/></svg>',
  copy: '<svg viewBox="0 0 24 24"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a1 1 0 0 1 1-1h9"/></svg>',
  plus: '<svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg>',
  search: '<svg viewBox="0 0 24 24"><circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/></svg>',
  receipt: '<svg viewBox="0 0 24 24"><path d="M6 3h12v18l-3-2-3 2-3-2-3 2z"/><path d="M9 8h6M9 12h6"/></svg>',
  download: '<svg viewBox="0 0 24 24"><path d="M12 4v11m0 0-4-4m4 4 4-4"/><path d="M5 19h14"/></svg>',
  upd: '<svg viewBox="0 0 24 24"><path d="M20 12a8 8 0 1 1-2.34-5.66"/><path d="M20 4v5h-5"/></svg>',
  back: '<svg viewBox="0 0 24 24"><path d="M15 5l-7 7 7 7"/></svg>',
  fwd: '<svg viewBox="0 0 24 24"><path d="M9 5l7 7-7 7"/></svg>',
  x: '<svg viewBox="0 0 24 24"><path d="M6 6l12 12M18 6 6 18"/></svg>',
  ext: '<svg viewBox="0 0 24 24"><path d="M14 4h6v6M20 4l-9 9"/><path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/></svg>',
  cube: '<svg viewBox="0 0 24 24"><path d="M12 3 4 7.5v9L12 21l8-4.5v-9z"/><path d="M4 7.5 12 12l8-4.5M12 12v9"/></svg>',
  spark: '<svg viewBox="0 0 24 24"><path d="M11 3l1.9 5.1L18 10l-5.1 1.9L11 17l-1.9-5.1L4 10l5.1-1.9z"/><path d="M18.5 15l.9 2.1 2.1.9-2.1.9-.9 2.1-.9-2.1-2.1-.9 2.1-.9z"/></svg>',
  play: '<svg viewBox="0 0 24 24"><path d="M7 5v14l11-7z"/></svg>',
  belt: '<svg viewBox="0 0 24 24"><rect x="3" y="14" width="18" height="5" rx="2.5"/><circle cx="6" cy="16.5" r="1"/><circle cx="12" cy="16.5" r="1"/><circle cx="18" cy="16.5" r="1"/><rect x="6" y="6" width="5" height="6" rx="1"/><rect x="13" y="6" width="5" height="6" rx="1"/></svg>',
};

const S = {
  data: null, rev: 0,
  q: "", cat: "全部", style: "", recent: "", bases: new Set(), usage: "all", share: "all", root: "all", purchase: "all", showHidden: false, sort: "recent",
  openKey: null, setup: null, bs: null, desc: {}, dirty: new Set(), coverPick: null, stylePick: null, groups: new Map(), panOpen: new Set(), panClosed: new Set(), panSel: new Set(),
  view: "lib", shopOpen: null, projs: null, pq: "",
  fold: { cat: false, cloth: false, bases: false }, // side bar parts folded away
  // Booth / 闲鱼 / 百度网盘 pages inside the program: open.shop = a page covers the Booth list, open.lib =
  // a netdisk page covers the library; loaded = whose page the pane holds; back = where 「返回」 goes
  web: { open: { shop: false, lib: false }, loaded: "", st: {}, last: { booth: "", xianyu: "", pan: "" }, back: null, xq: "" },
  shop: { cats: new Set(), bases: new Set(), styles: new Set(), q: "", sort: "popular", hideBought: false, hideOwned: false, adult: false,
    page: 1, items: [], more: false, loading: false, err: "", seq: 0, allBases: false, started: false, detail: null },
};
try {
  const saved = JSON.parse(localStorage.getItem("vrclib.ui") || "{}");
  if (saved.sort) S.sort = saved.sort;
  if (saved.view === "shop" || saved.view === "xianyu" || saved.view === "proj") S.view = saved.view;
  if (saved.web) { S.web.last.xianyu = saved.web.xianyu || ""; S.web.xq = saved.web.xq || ""; }
  for (const k in S.fold) if (saved.fold && saved.fold[k]) S.fold[k] = true;
  const sh = saved.shop || {};
  for (const k of ["cats", "bases", "styles"]) if (Array.isArray(sh[k])) S.shop[k] = new Set(sh[k]);
  for (const k of ["sort", "hideBought", "hideOwned", "adult"]) if (sh[k] !== undefined) S.shop[k] = sh[k];
} catch (e) {}
function saveUI() {
  const sh = S.shop;
  try { localStorage.setItem("vrclib.ui", JSON.stringify({ sort: S.sort, view: S.view, fold: S.fold, web: { xianyu: S.web.last.xianyu, xq: S.web.xq },
    shop: { cats: [...sh.cats], bases: [...sh.bases], styles: [...sh.styles], sort: sh.sort, hideBought: sh.hideBought, hideOwned: sh.hideOwned, adult: sh.adult } })); } catch (e) {}
}

async function api(path, body) {
  const r = await fetch(path, body === undefined ? {} : {
    method: "POST", headers: { "Content-Type": "application/json", "X-Token": window.API_TOKEN }, body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error(path + " " + r.status);
  return r.json();
}

function toast(msg, ms = 2200, onClick) {
  const t = $("#toast"); t.textContent = msg; t.classList.add("on");
  t.classList.toggle("click", !!onClick);
  t.onclick = onClick ? () => { t.classList.remove("on"); onClick(); } : null;
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
  if (S.data && S.updRestart && d.version !== S.data.version) { S.updWatch = S.updRestart = false; closeModal(); toast("已更新到 " + d.version, 4000); }
  if (!S.data && (d.whatsNew || []).length && !d.setupNeeded) setTimeout(() => openWhatsNew(d.version, d.whatsNew), 500);
  else if (!S.data && d.updatedFrom && d.updatedFrom !== d.version) setTimeout(() => toast(`已更新到 ${d.version}`, 4000), 600);
  const prev = S.data ? new Map((S.data.assets || []).map(a => [a.key, a])) : null;
  const bdWas = S.data && S.data.baidu && S.data.baidu.loggedIn;
  const gumWas = S.data && S.data.gumroad && S.data.gumroad.loggedIn;
  S.data = d; S.rev = d.rev;
  if (prev && !bdWas && d.baidu && d.baidu.loggedIn) baiduLoggedIn();
  if (prev && !gumWas && d.gumroad && d.gumroad.loggedIn) gumLoggedIn();
  if (d.xyBase) { XY_BASE = d.xyBase; XY_HOME = XY_BASE + "/"; }
  S.groups = new Map();
  for (const a of d.assets || []) if (a.group) { if (!S.groups.has(a.group)) S.groups.set(a.group, []); S.groups.get(a.group).push(a); }
  renderAll();
  if (S.view === "proj") loadProjects();
  if (prev) announceChanges(prev, d.assets || []);
}
// "新增 3 个素材": things the folder watcher or a share re-read brought in while the window was open
function announceChanges(prev, list) {
  const fresh = list.filter(a => !prev.has(a.key) && a.new && isLocal(a) && !a.splitInto);
  const changed = list.filter(a => prev.has(a.key) && hasNews(a) && !hasNews(prev.get(a.key)) && !a.splitInto);
  const nf = countUnits(fresh), nc = countUnits(changed);
  if (!nf && !nc) return;
  const show = what => () => { // only those, whatever else was filtered
    S.q = ""; $("#q").value = ""; S.cat = "全部"; S.style = ""; S.bases.clear(); S.usage = S.share = S.root = "all"; S.purchase = "all";
    S.recent = what; renderSide(); renderGrid(); $(".main").scrollTop = 0;
  };
  if (nf) {
    const cats = [...new Set(fresh.map(a => a.category).filter(Boolean))].slice(0, 3).join("、");
    toast(`新增 ${nf} 个素材${cats ? `（${cats}）` : ""}${nc ? `，${nc} 个有更新` : ""}`, 7000, show("new"));
  } else toast(`${nc} 个素材有更新`, 7000, show("news"));
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
function hasNews(a) { return !!(a.panNews || a.boothNews || a.newerOnBooth || a.shareErr); }
function matches(a, skip = "") {
  if (a.splitInto) return false; // a collection share: its products have cards of their own
  if (!S.showHidden && a.hidden) return false;
  if (skip !== "recent" && S.recent === "new" && !a.new) return false;
  if (skip !== "recent" && S.recent === "news" && !hasNews(a)) return false;
  if (skip !== "purchase") {
    const p = S.purchase;
    if (p === "all" && a.virtual) return false;
    if (p === "bought" && !a.purchase) return false;
    if (p === "local" && (!a.purchase || a.virtual)) return false;
    if (p === "missing" && !a.virtual) return false;
    if (p === "other" && (a.purchase || a.virtual)) return false;
  }
  if (skip !== "cat" && S.cat !== "全部" && a.category !== S.cat) return false;
  if (skip !== "style" && skip !== "cat" && S.style) {
    const st = a.styles || [];
    if (S.style === "__none" ? st.length : !st.includes(S.style)) return false;
  }
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
// ---------- same product, several downloads ----------
// Downloads of one product (one per base body, a PSD pack …) share a.group and show as one card.
function unitKey(a) { return a.group || a.key; }
function countUnits(list) { return new Set(list.map(unitKey)).size; }
function groupAll(a) { return a.group ? (S.groups.get(a.group) || [a]) : [a]; }
// the download that stands for the product: not the PSD pack, has a picture, the biggest
function primaryOf(ms) {
  return ms.slice().sort((x, y) => (!!x.psd - !!y.psd) || (!!y.cover - !!x.cover) || (!!x.virtual - !!y.virtual) || (y.size - x.size))[0];
}
// units: the cards to show, each with the downloads that passed the filters
function unitsOf(list) {
  const m = new Map();
  for (const a of list) { const k = unitKey(a); if (!m.has(k)) m.set(k, []); m.get(k).push(a); }
  return [...m.values()].map(ms => {
    const p = primaryOf(ms);
    return { key: unitKey(p), members: ms, primary: p, all: groupAll(p),
      name: ms.length > 1 ? (p.groupName || p.name) : p.name,
      size: ms.reduce((n, a) => n + (a.size || 0), 0),
      firstSeen: Math.max(...ms.map(a => a.firstSeen || 0)), mtime: Math.max(...ms.map(a => a.mtime || 0)),
      used: ms.some(a => usedStatus(a) === "used") };
  });
}
function sorted(units) {
  const by = {
    recent: (a, b) => b.firstSeen - a.firstSeen || b.mtime - a.mtime,
    name: (a, b) => a.name.localeCompare(b.name, "zh-CN"),
    size: (a, b) => b.size - a.size,
    used: (a, b) => b.used - a.used || b.firstSeen - a.firstSeen,
  }[S.sort];
  return units.slice().sort(by);
}
function styleList() { return (S.data.styleNames || []); }

// ---------- render ----------
function renderAll() {
  applyView();
  if (S.view === "shop") { renderShopSide(); renderShopGrid(); }
  else if (S.view === "xianyu") renderXySide();
  else if (S.view === "proj") { renderProjSide(); renderProjGrid(); }
  else if (S.view === "pipe") { renderPipeSide(); renderPipeMain(); }
  else { renderSide(); renderGrid(); }
  if (webVisible()) renderWeb();
  renderStatus();
  if (S.openKey) renderDrawer(S.openKey, true);
  else if (S.shopOpen && S.shop.detail) renderShopDrawer();
}

function navItem(label, count, on, attrs, color) {
  return `<button class="navitem${on ? " on" : ""}" ${attrs}>${color ? `<span class="dot" style="background:${color}"></span>` : ""}<span>${esc(label)}</span><span class="n">${count}</span></button>`;
}

// a row with a fold button at its end; what the button folds away is drawn (or not) by the caller
function foldBtn(k, what) {
  return `<button class="foldbtn${S.fold[k] ? " folded" : ""}" data-fold="${k}" aria-expanded="${!S.fold[k]}" title="${S.fold[k] ? "展开" : "收起"}${what}">${ICON.chev}</button>`;
}
function foldRow(item, k, what) { return `<div class="navrow">${item}${foldBtn(k, what)}</div>`; }

function renderSide() {
  const d = S.data; if (!d) return;
  const all = d.assets || [];
  // counts are cards: the downloads of one product count once
  const tally = (list, keysOf) => { const m = {}; for (const a of list) for (const k of keysOf(a)) (m[k] = m[k] || new Set()).add(unitKey(a)); const out = {}; for (const k in m) out[k] = m[k].size; return out; };
  const rcb = all.filter(a => matches(a, "recent"));
  const nNew = countUnits(rcb.filter(a => a.new)), nNews = countUnits(rcb.filter(hasNews));
  let h = "";
  if (nNew || nNews || S.recent) {
    h += `<h3>动态</h3>` + (nNew || S.recent === "new" ? navItem("新加入", nNew, S.recent === "new", `data-recent="new"`, "#3fd0a6") : "") +
      (nNews || S.recent === "news" ? navItem("有更新", nNews, S.recent === "news", `data-recent="news"`, "#ffb547") : "");
  }
  const base = all.filter(a => matches(a, "cat"));
  const cc = tally(base, a => [a.category]);
  // folded: only 全部 and the category in use stay; under 衣服 only the style in use
  h += `<h3>分类</h3><div class="navfold">` + foldRow(navItem("全部", countUnits(base), S.cat === "全部", `data-cat="全部"`), "cat", "分类");
  for (const c of d.categories) if (cc[c]) {
    if (S.fold.cat && S.cat !== c) continue;
    const row = navItem(c, cc[c], S.cat === c, `data-cat="${esc(c)}"`, CAT_COLOR[c]);
    if (c !== "衣服" || S.cat !== "衣服") { h += row; continue; }
    h += foldRow(row, "cloth", "风格标签");
    const sb = all.filter(a => matches(a, "style"));
    const sc = tally(sb, a => (a.styles || []).length ? a.styles : ["__none"]);
    let sub = "";
    for (const st of styleList()) if (sc[st] && (!S.fold.cloth || S.style === st)) sub += `<button class="subitem${S.style === st ? " on" : ""}" data-style="${esc(st)}"><span>${esc(st)}</span><span class="n">${sc[st]}</span></button>`;
    if (sc.__none && (!S.fold.cloth || S.style === "__none")) sub += `<button class="subitem none${S.style === "__none" ? " on" : ""}" data-style="__none"><span>未加标签</span><span class="n">${sc.__none}</span></button>`;
    if (!S.fold.cloth) sub += `<button class="subitem edit" id="btnStyles">编辑标签…</button>`;
    if (sub) h += `<div class="subnav">${sub}</div>`;
  }
  h += `</div>`;

  const bc = tally(all.filter(a => matches(a, "bases")), a => a.bases || []);
  const bl = Object.keys(bc).sort((x, y) => bc[y] - bc[x]);
  if (bl.length) {
    const shown = S.fold.bases ? bl.filter(b => S.bases.has(b)) : bl;
    h += `<h3 class="withact">适配素体${S.fold.bases ? `<span class="foldn">${bl.length}</span>` : ""}${foldBtn("bases", "适配素体")}</h3>` +
      (shown.length ? `<div class="chips">` + shown.map(b => `<button class="chip${S.bases.has(b) ? " on" : ""}" data-base="${esc(b)}">${esc(b)}<span class="n">${bc[b]}</span></button>`).join("") + `</div>` : "");
  }

  const pb = all.filter(a => matches(a, "purchase"));
  const nU = f => countUnits(pb.filter(f));
  const syncing = d.purchaseBusy;
  const gum = d.gumroad || {}, hasBought = d.purchaseSync || gum.count;
  h += d.purchaseSync
    ? `<h3 class="withact">Booth 已购${syncing ? `<button class="h3act" id="btnSyncCancel">取消同步</button>` : `<button class="h3act" id="btnSync" title="上次同步：${esc(fmtTime(d.purchaseSync))}">重新同步</button>`}</h3>`
    : `<h3>Booth 已购</h3><div class="cta">${syncing ? `<button class="btn small" id="btnSyncCancel">取消同步</button>` : `<button class="btn small" id="btnSync">同步 Booth 已购</button>`}</div>`;
  h += gum.loggedIn || gum.count
    ? `<h3 class="withact">Gumroad 已购${gum.busy ? `<button class="h3act" id="btnGumCancel">取消同步</button>` : `<button class="h3act" id="btnGumSync" title="${gum.loggedIn ? (gum.lastSync ? "上次同步：" + esc(fmtTime(gum.lastSync)) : "读取已购") : "登录已失效，重新登录"}">${gum.loggedIn ? "重新同步" : "登录"}</button>`}</h3>`
    : `<h3>Gumroad 已购</h3><div class="cta">${gum.waiting ? `<span class="muted small">在打开的页面里登录…</span>` : `<button class="btn small" id="btnGumSync">登录并同步 Gumroad</button>`}</div>`;
  if (hasBought) {
    const local = nU(a => a.purchase && !a.virtual), miss = nU(a => a.virtual);
    const other = nU(a => !a.purchase && !a.virtual), localAll = nU(a => !a.virtual), bought = nU(a => a.purchase);
    h += navItem("本地全部", localAll, S.purchase === "all", `data-purchase="all"`) +
      navItem("已购已下载", local, S.purchase === "local", `data-purchase="local"`, "#ff5a6a") +
      navItem("已购未下载", miss, S.purchase === "missing", `data-purchase="missing"`, "#ffb547") +
      navItem("全部已购", bought, S.purchase === "bought", `data-purchase="bought"`) +
      navItem("非已购", other, S.purchase === "other", `data-purchase="other"`, "#6c7088");
  }

  const ub = all.filter(a => matches(a, "usage"));
  const pc = tally(ub, a => (a.usage || []).map(u => u.project));
  const usedN = countUnits(ub.filter(a => usedStatus(a))), unusedN = countUnits(ub.filter(a => !usedStatus(a)));
  h += `<h3>工程使用</h3>` + navItem("全部", countUnits(ub), S.usage === "all", `data-usage="all"`) +
    navItem("在用", usedN, S.usage === "used", `data-usage="used"`, "#3fd0a6") +
    navItem("未使用", unusedN, S.usage === "unused", `data-usage="unused"`, "#6c7088");
  for (const p of (d.projects || [])) if (pc[p.name]) h += navItem(p.name, pc[p.name], S.usage === "p:" + p.name, `data-usage="p:${esc(p.name)}"`);

  const sb = all.filter(a => matches(a, "share"));
  h += `<h3>网盘</h3>` + navItem("全部", countUnits(sb), S.share === "all", `data-share="all"`) +
    navItem("有链接", countUnits(sb.filter(hasShare)), S.share === "has", `data-share="has"`, "#4c8dff") +
    navItem("无链接", countUnits(sb.filter(a => !hasShare(a))), S.share === "none", `data-share="none"`);
  const po = countUnits(sb.filter(a => a.panOnly));
  if (po) h += navItem("仅网盘", po, S.share === "panonly", `data-share="panonly"`, "#7fb0ff");

  const rb = all.filter(a => matches(a, "root"));
  const rc = tally(rb, a => [...new Set((a.locations || []).map(l => l.root))]);
  h += `<h3>文件夹</h3>` + navItem("全部", countUnits(rb), S.root === "all", `data-root="all"`);
  for (const r of (d.settings.roots || [])) if (rc[r]) h += navItem(rootLabel(r), rc[r], S.root === r, `data-root="${esc(r)}" title="${esc(r)}"`);

  const hiddenN = all.filter(a => a.hidden && !a.splitInto).length;
  if (hiddenN) h += `<label class="check"><input type="checkbox" id="showHidden"${S.showHidden ? " checked" : ""}> 显示隐藏的（${hiddenN}）</label>`;
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
  if (p.source === "gumroad") { const o = (p.orders || [])[0]; return o && o.date ? o.date + " 在 Gumroad 购入" : "Gumroad 已购"; }
  const o = (p.orders || [])[0];
  return o ? (o.date ? o.date + " 购入" : "订单 #" + o.id) : (p.gift ? "收到的礼物" : "Booth 已购");
}
function boughtBadge(a) {
  if (!a.purchase) return "";
  return `<span class="badge-bought${a.virtual ? " missing" : ""}" title="${esc(orderLine(a.purchase))}">${a.virtual ? "未下载" : "已购"}</span>`;
}

function styleTags(a) {
  const st = a.styles || [];
  return st.length ? st.slice(0, 3).map(x => `<span class="stag">${esc(x)}</span>`).join("") : "";
}
// a card standing for several downloads of one product
function groupCardHTML(u) {
  const a = u.primary, ms = u.members;
  const bought = ms.find(m => m.purchase && !m.virtual) || ms.find(m => m.purchase);
  const shop = shopOf(a);
  const bases = [...new Set(ms.flatMap(m => m.bases || []))];
  const used = ms.flatMap(m => m.usage || []);
  const labels = ms.map(m => m.psd ? "PSD" : (m.variant || m.name));
  const zh = !S.data.settings.hideZh && a.nameZh ? `<div class="zh" title="${esc(a.nameZh)}">${esc(a.nameZh)}</div>` : "";
  return `<article class="card group" data-key="${esc(a.key)}" data-group="${esc(a.group)}" tabindex="0" title="${ms.length} 个版本">
    <div class="cover">${coverHTML(a)}${bought ? boughtBadge(bought) : ""}${used.length ? usedBadge({ usage: used }) : ""}
      <div class="where"><div class="path" title="${esc(labels.join("\n"))}">${esc(labels.join("、"))}</div>
        <div class="acts">
          <button class="act booth${a.boothId ? "" : " nobooth"}" data-act="booth" title="${isGum(a) ? "打开 Gumroad 商品页" : a.boothId ? "打开 Booth 商品页" : "在 Booth 上搜"}">${a.boothId ? ICON.bag : ICON.search}</button>
          <button class="act edit" data-act="edit" title="查看全部版本">${ICON.edit}<span>${ms.length} 个版本</span></button>
        </div></div>
    </div>
    <div class="body">
      <div class="title">${ms.some(m => m.new) ? `<span class="newtag">新</span>` : ""}${esc(u.name)}</div>${zh}
      <div class="sub">${esc(shop || (a.booth && a.booth.name) || rootLabel(((a.locations || [])[0] || {}).root || ""))}</div>
      ${bases.length || (a.styles || []).length ? `<div class="bases">${bases.slice(0, 6).map(b => `<span class="base">${esc(b)}</span>`).join("")}${bases.length > 6 ? `<span class="base more">+${bases.length - 6}</span>` : ""}${styleTags(a)}</div>` : ""}
      <div class="meta"><span class="cat"><i style="background:${CAT_COLOR[a.category] || "#8b8fa6"}"></i>${esc(a.category)}</span><span class="vcount">${ms.length} 个版本</span><span>${fmtSize(u.size)}</span>
        <span class="flags">${ms.some(hasNews) ? `<span class="flag upd" title="${esc(ms.filter(hasNews).map(newsTitle).join("\n"))}">${ICON.upd}</span>` : ""}${ms.some(hasShare) ? `<span class="flag pan" title="网盘">${ICON.cloud}</span>` : ""}${a.boothId ? `<span class="flag booth" title="Booth #${esc(a.boothId)}">${ICON.bag}</span>` : ""}</span></div>
    </div></article>`;
}

function cardHTML(a) {
  const loc = (a.locations || [])[0];
  const pan = hasShare(a);
  const p = a.purchase;
  const shop = (a.booth && a.booth.shop) || (p && p.shop);
  const subline = shop ? shop : (a.booth && a.booth.name && a.booth.name !== a.name ? a.booth.name : rootLabel(loc ? loc.root : ""));
  const where = a.virtual ? `${orderLine(p)}，本地还没有` : a.panOnly ? panLine(a) : shortPath(loc);
  const tip = a.virtual ? "查看并下载" : a.panOnly ? "查看内容并下载" : "打开文件夹";
  const zh = !S.data.settings.hideZh && a.nameZh ? `<div class="zh" title="${esc(a.nameZh)}">${esc(a.nameZh)}</div>` : "";
  return `<article class="card${a.hidden ? " hiddenasset" : ""}${a.virtual ? " virtual" : ""}${a.panOnly ? " panonly" : ""}" data-key="${esc(a.key)}" tabindex="0" title="${tip}">
    <div class="cover">${coverHTML(a)}${boughtBadge(a)}${a.panOnly ? `<span class="badge-pan">网盘</span>` : ""}${usedBadge(a)}${a.virtual ? dlBar(a.boothId) : a.panOnly ? panBar(a.key) : ""}
      <div class="where"><div class="path" title="${esc(loc ? loc.path : where)}">${esc(where)}</div>
        <div class="acts">
          ${a.virtual ? `<button class="act buy" data-act="dl" title="下载到素材文件夹">${ICON.download}<span>下载</span></button>`
            : a.panOnly ? (a.user.shareUrl ? `<button class="act buy" data-act="pandl" title="下载到素材库">${ICON.download}<span>下载</span></button>` : "")
            : `<button class="act" data-act="open" title="打开文件夹">${ICON.folder}</button>`}
          ${canImport(a) ? `<button class="act imp" data-act="import" title="一键导入 Unity 工程">${ICON.cube}<span>导入</span></button>` : ""}
          ${pan ? `<button class="act pan" data-act="pan" title="${a.panOnly ? "打开网盘分享（自动复制提取码）" : "打开网盘"}">${ICON.cloud}${a.panOnly ? "<span>网盘</span>" : ""}</button>` : ""}
          ${p && !a.virtual ? `<button class="act buy" data-act="buypage" title="购买页">${ICON.receipt}</button>` : ""}
          <button class="act booth${a.boothId ? "" : " nobooth"}" data-act="booth" title="${isGum(a) ? "打开 Gumroad 商品页" : a.boothId ? "打开 Booth 商品页" : "在 Booth 上搜「" + esc(a.boothQuery || a.name) + "」"}">${a.boothId ? ICON.bag : ICON.search}</button>
          <button class="act edit${canImport(a) ? " bare" : ""}" data-act="edit" title="详情">${ICON.edit}<span>详情</span></button>
        </div></div>
    </div>
    <div class="body">
      <div class="title">${a.new ? `<span class="newtag">新</span>` : ""}${esc(a.name)}</div>${zh}
      <div class="sub">${esc(a.panOnly && !shop ? "百度网盘" : subline)}</div>
      ${(a.bases || []).length || (a.styles || []).length ? `<div class="bases">${(a.bases || []).slice(0, 4).map(b => `<span class="base">${esc(b)}</span>`).join("")}${(a.bases || []).length > 4 ? `<span class="base more">+${a.bases.length - 4}</span>` : ""}${styleTags(a)}</div>` : ""}
      <div class="meta"><span class="cat"><i style="background:${CAT_COLOR[a.category] || "#8b8fa6"}"></i>${esc(a.category)}</span>${a.psd ? `<span class="psdtag" title="仅 PSD">PSD</span>` : ""}${groupAll(a).length > 1 ? `<button class="vcount link" data-act="edit">共 ${groupAll(a).length} 个版本</button>` : ""}<span>${a.virtual ? "未下载" : a.panOnly && !a.size ? "—" : fmtSize(a.size)}</span>
        <span class="flags">${hasNews(a) ? `<span class="flag upd" title="${esc(newsTitle(a))}">${ICON.upd}</span>` : ""}${pan ? `<span class="flag pan" title="网盘">${ICON.cloud}</span>` : ""}${a.boothId ? `<span class="flag booth" title="${isGum(a) ? "Gumroad" : "Booth #" + esc(a.boothId)}">${ICON.bag}</span>` : ""}</span></div>
    </div></article>`;
}

function renderGrid() {
  const d = S.data; if (!d) return;
  const list = sorted(unitsOf((d.assets || []).filter(a => matches(a))));
  const total = countUnits((d.assets || []).filter(a => (S.showHidden || !a.hidden) && !a.virtual));
  const filtered = S.q || S.cat !== "全部" || S.style || S.recent || S.bases.size || S.usage !== "all" || S.share !== "all" || S.root !== "all" || S.purchase !== "all";
  let rb = `<b>${list.length}</b><span>个${S.purchase === "missing" || S.purchase === "bought" ? "商品" : "素材"}${filtered ? `（共 ${total} 个）` : ""}</span>`;
  if (filtered) rb += `<button class="clear" id="clearFilters">清除筛选</button>`;
  if (S.purchase === "missing" && list.length) rb += `<button class="btn small" id="btnDLMissing">${ICON.download}<span>全部下载</span></button>`;
  if (S.bulk) rb = bulkBarHTML(list);
  else if (list.length && S.purchase !== "missing") rb += `<button class="btn small ghost" id="btnBulk" title="一次选中多个素材：移到别的分类、隐藏、不再收录">批量整理</button>`;
  for (const w of (d.warnings || [])) rb += `<span class="warnline">${esc(w)}</span>`;
  $("#resultbar").innerHTML = rb;
  const g = $("#grid");
  g.classList.toggle("bulk", !!S.bulk);
  if (!list.length) {
    const scanning = d.busy;
    g.innerHTML = `<div class="empty">${!(d.assets || []).some(a => !a.virtual) && S.purchase === "all"
      ? (scanning ? `<h2>正在扫描</h2><p>第一次会久一点。</p>`
        : `<h2>还没有素材</h2><p>先在设置里添加素材文件夹。</p><button class="btn primary" id="emptySettings">打开设置</button>`)
      : S.purchase === "missing" && !(d.assets || []).some(a => a.virtual) ? `<h2>已购的都下载好了</h2>`
      : `<h2>没有符合条件的素材</h2>`}</div>`;
    return;
  }
  g.innerHTML = list.map(u => u.members.length > 1 ? groupCardHTML(u) : cardHTML(u.primary)).join("");
  if (S.bulk) bulkMark();
}

// ---------- 批量整理: several cards at once ----------
// S.bulk = { keys: the picked cards (unitKey), last: the one clicked last, for Shift ranges }
function bulkUnits() { return sorted(unitsOf((S.data.assets || []).filter(a => matches(a)))); }
function bulkBarHTML(list) {
  const n = S.bulk.keys.size, dis = n ? "" : " disabled";
  return `<span class="bulkbar"><b>${n}</b><span>个已选</span>
    <button class="clear" id="bulkAll">全选这 ${list.length} 个</button>${n ? `<button class="clear" id="bulkNone">清空</button>` : ""}
    <select id="bulkCat"${dis} title="把选中的素材移到这个分类"><option value="">移到分类…</option>${S.data.categories.map(c => `<option>${esc(c)}</option>`).join("")}<option value="__auto">恢复自动分类</option></select>
    <button class="btn small" data-bulk="hide"${dis}>隐藏</button><button class="btn small" data-bulk="show"${dis}>取消隐藏</button>
    <button class="btn small" data-bulk="ignore"${dis} title="扫描时跳过这些文件夹，可以在设置里撤销">不再收录</button>
    <button class="btn small primary" id="bulkDone">完成</button></span>
    <span class="muted small">点卡片选中，按住 Shift 再点可以连选一片</span>`;
}
function bulkMark() {
  document.querySelectorAll("#grid .card").forEach(c => {
    const a = findAsset(c.dataset.key); c.classList.toggle("sel", !!a && S.bulk.keys.has(unitKey(a)));
  });
}
function bulkRefresh() { $("#resultbar").innerHTML = bulkBarHTML(bulkUnits()); bulkMark(); }
function bulkToggle(a, range) {
  const k = unitKey(a), b = S.bulk;
  if (range && b.last && b.last !== k) {
    const order = bulkUnits().map(u => unitKey(u.primary)), i = order.indexOf(b.last), j = order.indexOf(k);
    if (i >= 0 && j >= 0) { for (const x of order.slice(Math.min(i, j), Math.max(i, j) + 1)) b.keys.add(x); b.last = k; return bulkRefresh(); }
  }
  b.keys.has(k) ? b.keys.delete(k) : b.keys.add(k); b.last = k; bulkRefresh();
}
// every asset behind the picked cards (a card of several versions stands for all of them)
function bulkAssets() { return (S.data.assets || []).filter(a => S.bulk.keys.has(unitKey(a))); }
async function bulkApply(body, done) {
  const list = bulkAssets(); if (!list.length) return;
  const r = await api("/api/user/bulk", Object.assign({ keys: list.map(a => a.key) }, body));
  if (!r.ok) { toast(r.err || "没能保存", 4000); return; }
  S.bulk.keys.clear(); S.bulk.last = null;
  toast(done(r)); await load();
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
    h += `<span>上次扫描 ${fmtTime(d.lastScan)}</span>`;
    const bt = (d.tasks || []).find(t => t.name === "booth");
    if (bt && bt.msg && bt.msg.includes("代理")) h += `<span class="err">${esc(bt.msg)}</span>`;
    const dt = (d.tasks || []).find(t => t.name === "download");
    if (dt && dt.msg && dt.ended && Date.now() / 1000 - dt.ended < 600 && !d.dlNeedLogin) h += `<span class="okline">${esc(dt.msg)}</span>`;
    const pt = (d.tasks || []).find(t => t.name === "purchase");
    if (pt && pt.msg && pt.ended && Date.now() / 1000 - pt.ended < 600) h += `<span class="${pt.msg.startsWith("完成") ? "okline" : "err"}">Booth 已购：${esc(pt.msg)}</span>`;
    const gt = (d.tasks || []).find(t => t.name === "gumroad");
    if (gt && gt.msg && gt.ended && Date.now() / 1000 - gt.ended < 600) h += `<span class="${gt.msg.startsWith("完成") ? "okline" : "err"}">${gt.msg.startsWith("完成") ? "" : "Gumroad 已购："}${esc(gt.msg)}</span>`;
  }
  if (d.dlNeedLogin) h += `<span class="err">下载需要登录 Booth <button class="verbtn linkish" id="btnDLLogin">登录</button></span>`;
  if (running.some(t => t.name === "download")) h += `<button class="verbtn linkish" id="btnDLCancel">取消下载</button>`;
  if ((d.panJobs || []).some(j => j.stage === "login")) h += `<span class="err">下载网盘分享需要登录百度网盘 <button class="verbtn linkish" id="btnBdLogin">登录</button></span>`;
  if (running.some(t => t.name === "pandl")) h += `<button class="verbtn linkish" id="btnPanCancel">取消网盘下载</button>`;
  if (!running.length) {
    const pt = (d.tasks || []).find(t => t.name === "pandl"), it = (d.tasks || []).find(t => t.name === "import");
    for (const t of [pt, it]) if (t && t.msg && t.ended && Date.now() / 1000 - t.ended < 600) h += `<span class="${/完成|已导入/.test(t.msg) ? "okline" : "err"}">${esc(t.msg)}</span>`;
  }
  h += `<span class="right"><button class="verbtn" id="btnFeedback">反馈和建议</button><button class="verbtn" id="btnVersion" title="更新公告">v${esc(d.version)}</button></span>`;
  $("#status").innerHTML = h;
  renderUpdateBtn();
  $("#btnScan").disabled = !!d.busy;
  $("#btnScan").textContent = d.busy ? "正在扫描…" : "重新扫描";
}


// ---------- Booth: looking for new things ----------
// Tags instead of Japanese search words: a kind of item (several add up), base bodies and styles
// (all of them have to match). Cards say what was bought and what is in the library already.
function applyView() {
  const v = S.view, shop = v === "shop", xy = v === "xianyu", proj = v === "proj", pipe = v === "pipe";
  document.body.classList.toggle("shopview", shop);
  document.body.classList.toggle("xyview", xy);
  document.body.classList.toggle("projview", proj);
  document.body.classList.toggle("pipeview", pipe);
  $("#grid").classList.toggle("projgrid", proj);
  $("#grid").classList.toggle("pipegrid", pipe);
  for (const [id, name] of [["#vLib", "lib"], ["#vProj", "proj"], ["#vPipe", "pipe"], ["#vShop", "shop"], ["#vXy", "xianyu"]]) {
    $(id).classList.toggle("on", v === name); $(id).setAttribute("aria-selected", String(v === name));
  }
  $("#q").placeholder = shop ? "在 Booth 里搜索（可不填）" : xy ? "在闲鱼搜索，按回车" : proj || pipe ? "搜索工程" : "搜索名称、店铺、标签、路径";
  $("#sort").hidden = v !== "lib"; $("#shopSort").hidden = !shop;
  $("#btnPanAdd").hidden = shop || proj || pipe; $("#btnScan").hidden = v !== "lib";
  const web = webVisible();
  $("#webview").hidden = !web; $("#resultbar").hidden = web; $("#grid").hidden = web;
  $(".main").classList.toggle("webon", web);
}
function setView(v) {
  if (S.view === v) return;
  if (S.view === "pipe") pipeKeep();
  S.view = v; saveUI(); closeDrawer();
  $("#q").value = v === "shop" ? S.shop.q : v === "xianyu" ? S.web.xq : v === "proj" || v === "pipe" ? S.pq : S.q;
  $(".main").scrollTop = 0;
  renderAll();
  if (v === "proj") loadProjects();
  if (v === "pipe") pipeEnter();
  if (v === "shop" && !S.shop.started) shopSearch(true);
  if (S.data.paneMode && webVisible()) ensurePaneFor(v);
}
// coming back to a tab whose page the other tab (or a background login check) replaced: reopen it
async function ensurePaneFor(v) {
  const want = v === "xianyu" ? "xianyu" : v === "lib" ? "pan" : "booth";
  let st = {};
  try { st = await api("/api/pane/state", {}); } catch (e) {}
  if (S.view !== v) return;
  if (!st.open || st.kind !== want) return openWeb(S.web.last[want] || (want === "xianyu" ? XY_HOME : want === "pan" ? S.data.panWeb + "/disk/main" : boothHome()), want);
  S.web.loaded = want; S.web.st = st; placedKey = ""; renderWeb(); webPoll(true);
}
// base bodies to offer: the ones in the library first
function shopBases() {
  const names = (S.data.settings.bases || []).map(l => l.split("=")[0].trim()).filter(Boolean);
  const n = {};
  for (const a of S.data.assets || []) for (const b of a.bases || []) n[b] = (n[b] || 0) + 1;
  return names.map((b, i) => ({ b, n: n[b] || 0, i })).sort((x, y) => y.n - x.n || x.i - y.i).map(x => x.b);
}
function renderShopSide() {
  const sh = S.shop;
  const chips = (list, set, attr) => list.map(x => `<button class="chip${set.has(x) ? " on" : ""}" data-${attr}="${esc(x)}" aria-pressed="${set.has(x)}">${esc(x)}</button>`).join("");
  let bases = shopBases();
  const extra = bases.length > 14 && !sh.allBases;
  if (extra) bases = bases.slice(0, 14).concat([...sh.bases].filter(b => !bases.slice(0, 14).includes(b)));
  const mine = S.data.paneMode ? `<h3>我的 Booth</h3><div class="sidelinks two"><button class="navitem" data-bl="login"><span>登录</span></button><button class="navitem" data-bl="library"><span>已购列表</span></button>
    <button class="navitem" data-bl="cart"><span>购物车</span></button><button class="navitem" data-bl="sync"><span>同步已购</span></button></div>` : "";
  $("#side").innerHTML = `${mine}<h3>分类</h3><div class="chips">${chips(S.data.shopCats || [], sh.cats, "sc")}</div>
    <h3>适配素体</h3><div class="chips">${chips(bases, sh.bases, "sb")}${extra ? `<button class="chip more" id="shAllBases">更多…</button>` : ""}</div>
    <h3>风格</h3><div class="chips">${chips(S.data.styleNames || [], sh.styles, "ss")}</div>
    <h3>显示</h3>
    <label class="check"><input type="checkbox" data-shf="hideBought"${sh.hideBought ? " checked" : ""}> 不显示已购</label>
    <label class="check"><input type="checkbox" data-shf="hideOwned"${sh.hideOwned ? " checked" : ""}> 不显示素材库里有的</label>
    <label class="check"><input type="checkbox" data-shf="adult"${sh.adult ? " checked" : ""}> 包含成人向</label>`;
}
function shopVisible() {
  const sh = S.shop;
  return sh.items.filter(it => !(sh.hideBought && it.bought) && !(sh.hideOwned && it.owned));
}
function shopCardHTML(it) {
  return `<article class="card shopcard" data-shop="${esc(it.id)}" tabindex="0">
    <div class="cover">${it.thumb ? `<img loading="lazy" src="/rthumb?u=${encodeURIComponent(it.thumb)}" alt="">` : `<div class="ph">${esc(it.name.slice(0, 18))}</div>`}
      <div class="sbadges">${it.owned ? `<span class="badge-owned">素材库已有</span>` : ""}${it.bought ? `<span class="badge-bought">已购</span>` : ""}</div></div>
    <div class="body">
      <div class="title">${esc(it.name)}</div>
      <div class="sub">${esc(it.shop || it.shopSub || "")}</div>
      <div class="meta"><span class="cat">${esc(it.category || "")}</span><span class="price">${esc(it.price || "")}</span></div>
    </div></article>`;
}
function renderShopGrid() {
  const sh = S.shop, list = shopVisible();
  const picked = [...sh.cats, ...sh.bases, ...sh.styles];
  const hidden = sh.items.length - list.length;
  let rb = `<b>${list.length}</b><span>个商品${hidden ? `（隐藏了 ${hidden} 个）` : ""}</span>`;
  if (picked.length || sh.q) rb += `<span class="picked">${picked.map(esc).join(" + ")}${sh.q ? `${picked.length ? " + " : ""}“${esc(sh.q)}”` : ""}</span><button class="clear" id="shopClear">清除</button>`;
  $("#resultbar").innerHTML = rb;
  const g = $("#grid");
  if (!list.length) {
    g.innerHTML = `<div class="empty">${sh.loading ? `<h2>正在读取 Booth…</h2>` : sh.err ? `<h2>没能读取 Booth</h2><p>${esc(sh.err)}</p>`
      : sh.items.length ? `<h2>这一页都被筛掉了</h2>${sh.more ? `<button class="btn" id="shopMore">再看一页</button>` : ""}` : `<h2>没有找到商品</h2><p>少选几个标签试试。</p>`}</div>`;
    return;
  }
  g.innerHTML = list.map(shopCardHTML).join("") +
    (sh.more || sh.loading ? `<div class="shopmore">${sh.loading ? `<span class="muted">正在读取…</span>` : `<button class="btn" id="shopMore">加载更多</button>`}</div>` : "");
}
let shopTimer;
function shopSoon() { clearTimeout(shopTimer); shopTimer = setTimeout(() => shopSearch(true), 380); }
async function shopSearch(reset) {
  const sh = S.shop;
  sh.started = true;
  const seq = ++sh.seq;
  if (reset) { sh.page = 1; sh.items = []; sh.more = false; }
  sh.loading = true; sh.err = "";
  if (S.view === "shop") renderShopGrid();
  let r;
  try {
    r = await api("/api/shop/search", { cats: [...sh.cats], bases: [...sh.bases], styles: [...sh.styles], q: sh.q, sort: sh.sort, page: sh.page, adult: sh.adult });
  } catch (e) { r = { ok: false, err: "软件内部出错" }; }
  if (seq !== sh.seq) return; // a newer search has started
  sh.loading = false;
  if (r.ok) {
    const have = new Set(sh.items.map(x => x.id));
    sh.items = sh.items.concat((r.items || []).filter(x => !have.has(x.id)));
    sh.more = !!r.more;
  } else sh.err = r.err || "读取失败";
  if (S.view === "shop") renderShopGrid();
}
async function openShopItem(id) {
  S.openKey = null; S.shopOpen = id;
  const hit = S.shop.items.find(x => x.id === id) || { id, name: "" };
  S.shop.detail = { loading: true, hit };
  renderShopDrawer();
  let r;
  try { r = await api("/api/shop/item", { id }); } catch (e) { r = { ok: false, err: "软件内部出错" }; }
  if (S.shopOpen !== id) return;
  S.shop.detail = r.ok ? { item: r.item, hit } : { err: r.err || "读取失败", hit };
  renderShopDrawer();
}
function renderShopDrawer() {
  const dr = $("#drawer"), d = S.shop.detail || {}, it = d.item, hit = d.hit || {};
  const id = S.shopOpen, url = boothItemURL(id);
  const name = (it && it.name) || hit.name || "Booth #" + id;
  const imgs = it ? (it.images || []).slice(0, 8) : [];
  const cover = imgs[0] || hit.thumb || "";
  const bought = it ? it.bought : hit.bought, owned = it ? it.owned : hit.owned;
  let body = "";
  if (d.loading) body = `<div class="muted small">正在读取商品信息…</div>`;
  else if (d.err) body = `<div class="small err">${esc(d.err)}</div>`;
  else {
    if (imgs.length > 1) body += `<div class="sec"><div class="bimgs">${imgs.map(u => `<img loading="lazy" src="/rthumb?u=${encodeURIComponent(boothImg(u, false))}" data-big="${esc(boothImg(u, true))}" alt="">`).join("")}</div></div>`;
    if (bought) body += `<div class="sec" id="shopfiles"><h4 class="withact">已购文件${(it.files || []).length > 1 ? `<button class="h4act" data-sd="dlall">全部下载</button>` : ""}</h4>
      ${dlRows({ files: (it.files || []).map(f => f.name), dls: (it.files || []).map(f => f.id), got: (it.files || []).map(f => f.got || "") })}</div>`;
    if ((it.tags || []).length) body += `<div class="sec"><h4>标签</h4><div class="btags">${it.tags.slice(0, 24).map(t => `<span>${esc(t)}</span>`).join("")}</div></div>`;
    if (it.desc) {
      const dd = S.desc["shop:" + id] || {};
      body += `<div class="sec"><details class="bdesc" data-desc="shop:${esc(id)}" open><summary>商品说明${dd.showZh ? "（译文）" : ""}</summary>
        <div class="desc">${esc(dd.showZh && dd.zh ? dd.zh : it.desc)}</div>
        <div class="btnrow"><button class="btn small" data-sd="trans"${dd.loading ? " disabled" : ""}>${dd.loading ? "翻译中…" : dd.showZh ? "原文" : "翻译"}</button></div></details></div>`;
    }
  }
  const main = owned ? `<button class="btn primary" data-sd="lib">在素材库查看</button>`
    : bought ? `<button class="btn primary" data-sd="dlall">${ICON.download}<span style="margin-left:6px">下载</span></button>`
    : `<button class="btn primary" data-sd="booth">在 Booth 打开</button>`;
  dr.innerHTML = `<div class="dhead">
      <div class="dcover">${cover ? `<img src="/rthumb?u=${encodeURIComponent(cover)}" alt="">` : ""}</div>
      <div style="min-width:0;flex:1">
        <h2>${esc(name)}</h2>
        <div class="sub">${[it ? it.shop : hit.shop, it ? it.price : hit.price, it ? it.category : hit.category].filter(Boolean).map(esc).join("　")}</div>
        <div class="sbadges static">${owned ? `<span class="badge-owned">素材库已有</span>` : ""}${bought ? `<span class="badge-bought">已购</span>` : ""}</div>
        <div class="rowbtn">${main}${owned || bought ? `<button class="btn" data-sd="booth">Booth 商品页</button>` : ""}</div>
      </div></div>
    <div class="dbody">${body}</div>
    <div class="dfoot"><button class="btn ghost" data-sd="close">关闭</button></div>`;
  dr.dataset.kind = "shop";
  dr.classList.add("on"); dr.setAttribute("aria-hidden", "false"); $("#scrim").classList.add("on");
}

// ---------- Booth / 闲鱼 pages inside the program ----------
// One page area (S.data.paneMode "native": a second web view laid over #webhost; "window": a separate
// app window driven from the toolbar; "": no built-in browser, links go to the default browser).
let XY_BASE = "https://www.goofish.com", XY_HOME = XY_BASE + "/";
function boothHome() { return (S.data.boothWeb || "https://booth.pm") + "/ja"; }
// a Gumroad purchase or product (its id starts with "gr_"): the Booth-only things on a card do not apply
function isGum(a) { return !!a && ((a.purchase && a.purchase.source === "gumroad") || String(a.boothId || "").startsWith("gr_")); }
function gumPageURL(a) { return (a.purchase && a.purchase.productUrl) || (a.booth && a.booth.url) || (a.purchase && a.purchase.pageUrl) || ""; }
function isGumURL(u) {
  const g = (S.data && S.data.gumroad) || {};
  if (g.libraryUrl && u.startsWith(g.libraryUrl.replace(/\/library$/, "/"))) return true;
  return /^https?:\/\/([^/]*\.)?gumroad\.com(\/|$)/i.test(u);
}
function webKind(u) {
  if (S.data && S.data.xyBase && u.startsWith(S.data.xyBase)) return "xianyu";
  if (isPanURL(u)) return "pan";
  if (isGumURL(u)) return "gumroad";
  return /^https?:\/\/([^/]*\.)?(goofish\.com|taobao\.com|tmall\.com|alipay\.com|xianyu\.com)(\/|$)/i.test(u) ? "xianyu" : "booth";
}
function isPanURL(u) {
  if (S.data && [S.data.panWeb, S.data.baiduLogin].some(b => b && u.startsWith(b))) return true;
  return /^https?:\/\/([^/]*\.)?baidu\.com(\/|$)/i.test(u);
}
function webView(kind) { return kind === "xianyu" ? "xianyu" : kind === "pan" || kind === "gumroad" ? "lib" : "shop"; }
function isBoothURL(u) {
  if (S.data && [S.data.boothWeb, S.data.boothAccounts].some(b => b && u.startsWith(b))) return true;
  return /^https?:\/\/([^/]*\.)?(booth\.pm|pixiv\.net)(\/|$)/i.test(u);
}
function webVisible() { return (S.view === "shop" && S.web.open.shop) || S.view === "xianyu" || (S.view === "lib" && S.web.open.lib); }
// Booth, 闲鱼 and netdisk links open inside; anything else in the default browser
function openLink(u) { if (isBoothURL(u) || webKind(u) !== "booth") return openWeb(u); return openURL(u); }
async function openWeb(u, kind) {
  kind = kind || webKind(u);
  if (!S.data.paneMode) return openURL(u);
  const v = webView(kind), covers = v !== "xianyu"; // a page over the Booth list or over the library
  const prevBack = S.web.back;
  S.web.back = S.openKey ? { view: S.view, key: S.openKey }
    : covers && S.view !== v ? { view: S.view }
    : covers && S.web.open[v] ? prevBack : null;
  closeDrawer();
  if (covers) S.web.open[v] = true;
  S.web.loaded = kind; S.web.st = { url: u, title: "", loading: true, open: true };
  if (S.view !== v) {
    S.view = v; saveUI(); $("#q").value = v === "shop" ? S.shop.q : v === "xianyu" ? S.web.xq : S.q;
    if (v === "shop" && !S.shop.started) shopSearch(true);
  }
  renderAll();
  let r;
  try { r = await api("/api/pane/open", { url: u, kind }); } catch (e) { r = { ok: false, err: "软件内部出错" }; }
  if (!r.ok) {
    toast(r.err || "页面打不开", 4000);
    if (covers) { S.web.open[v] = false; renderAll(); }
    return;
  }
  placedKey = ""; webPoll(true);
}
// a library card's Booth button: the item in the Booth tab
function goBooth(a) {
  if (isGum(a)) { const u = gumPageURL(a); if (u) openLink(u); return; }
  S.web.open.shop = false;
  if (S.view !== "shop") setView("shop"); else { closeDrawer(); renderAll(); }
  if (a.boothId) { openShopItem(a.boothId); return; }
  S.shop.q = a.boothQuery || a.name; $("#q").value = S.shop.q; saveUI(); shopSearch(true);
}
function closeWeb() {
  const back = S.web.back; S.web.back = null; S.web.forLogin = false;
  if (S.data.paneMode === "window") api("/api/pane/act", { act: "close" }).catch(() => {});
  if (S.view === "shop" || S.view === "lib") { S.web.open[S.view] = false; S.web.loaded = S.data.paneMode === "window" ? "" : S.web.loaded; }
  if (back && back.view !== S.view) { setView(back.view); if (back.key && findAsset(back.key)) renderDrawer(back.key); return; }
  renderAll();
  if (back && back.key && findAsset(back.key)) renderDrawer(back.key);
}
function renderXySide() {
  $("#side").innerHTML = `<h3>闲鱼</h3>
    <div class="sidelinks"><button class="navitem" data-xy="home"><span>首页</span></button><button class="navitem" data-xy="im"><span>消息</span></button><button class="navitem" data-xy="login"><span>登录</span></button></div>
    <p class="sidenote">在上面的搜索框输入想找的东西，按回车搜索。登录、聊天、下单都在这里完成，登录会保存在本机。</p>
    <h3>收进素材库</h3>
    <p class="sidenote">卖家发来网盘分享后，在聊天里选中整段分享文字（链接和提取码），点页面上方的「收录网盘链接」。</p>`;
}
// ---------- 工程 ----------
async function loadProjects() {
  let r;
  try { r = await api("/api/projects", {}); } catch (e) { return; }
  if (!r.ok) return;
  const was = JSON.stringify(S.projs);
  S.projs = r.projects || [];
  if (JSON.stringify(S.projs) === was) return;
  if (S.view === "proj") { renderProjSide(); renderProjGrid(); }
  if (S.view === "pipe") { if (AI.proj) { const p = S.projs.find(x => x.path === AI.proj.path); if (p) AI.proj = p; } renderPipeSide(); }
}
function renderProjSide() {
  const ps = S.projs || [], busy = (S.data.tasks || []).some(t => t.name === "usage" && t.running);
  $("#side").innerHTML = `<h3>工程</h3>
    <div class="sidelinks"><button class="navitem" id="btnProjAdd"><span>添加工程…</span></button>
      <button class="navitem" id="btnProjUsage"${busy ? " disabled" : ""}><span>${busy ? "正在统计用到的素材…" : "重新统计用到的素材"}</span></button></div>
    <p class="sidenote">${ps.length ? `${ps.length} 个工程，` : ""}从设置里的「Unity 工程」文件夹找到。工程里的插件和模型请用 VCC 或 ALCOM 管理。</p>
    <h3>流水线</h3>
    <p class="sidenote">在卡片上点「流水线」：把导入的衣服、头发、配饰、道具装到模型上，按你定的层级生成菜单开关和图标，还能让 AI 接着做别的改模操作。</p>
    <p class="sidenote">没有工程的话，到「流水线」页一键新建基础工程。</p>
    <div class="sidelinks"><button class="navitem" id="btnNewProj"><span>新建基础工程…</span></button><button class="navitem" id="btnAICfg"><span>设置 AI 服务…</span></button></div>
    <h3>封面</h3>
    <p class="sidenote">在卡片上点「开启封面」，会往那个工程的 Packages 里放一个小插件。之后每次打开工程、保存场景，它会给场景里的模型拍一张正面照当封面，旧的自动删掉。</p>
    <p class="sidenote">插件只在 Unity 编辑器里运行，不改场景，也不会跟模型一起上传。点「关闭封面」会删掉插件和照片。</p>`;
}
function relDay(t) {
  if (!t) return "";
  const days = Math.floor((Date.now() / 1000 - t) / 86400);
  return days <= 0 ? "今天" : days === 1 ? "昨天" : days < 30 ? `${days} 天前` : fmtTime(t).split(" ")[0];
}
function projCardHTML(p) {
  const cover = p.cover ? `<img src="/thumb?w=640&p=${encodeURIComponent(p.cover)}&t=${p.coverAt}" alt="" loading="lazy">`
    : `<div class="pph">${ICON.cube}<span>${p.helper ? "打开工程或保存场景后，这里会出现模型的正面照" : "开启封面后，会自动给模型拍一张正面照"}</span></div>`;
  const ver = p.unity ? `<span class="uver${p.editor ? "" : " missing"}" title="${p.editor ? "" : esc(`这台电脑上没找到 Unity ${p.unity}`)}">Unity ${esc(p.unity)}</span>` : "";
  const opened = p.running ? `<span class="okline">Unity 里开着</span>` : p.opened ? `<span>${relDay(p.opened)}打开过</span>` : "";
  return `<article class="pcard" data-proj="${esc(p.path)}">
    <div class="pcover${p.cover ? "" : " empty"}">${cover}${p.running ? `<span class="badge-run">已打开</span>` : ""}</div>
    <div class="pbody">
      <div class="title" title="${esc(p.name)}">${esc(p.name)}</div>
      <div class="sub" title="${esc(p.path)}">${esc(p.path)}</div>
      <div class="pmeta">${ver}${opened}</div>
      <div class="pacts">
        <button class="btn small primary" data-pa="unity"${p.running ? ` disabled title="已经在 Unity 里打开了"` : ""}>${ICON.cube}<span>打开 Unity</span></button>
        <button class="btn small" data-pa="folder">${ICON.folder}<span>文件夹</span></button>
        <button class="btn small" data-pa="ai" title="到流水线页：给这个工程的模型装素材、生成菜单">${ICON.belt}<span>流水线</span></button>
        <button class="btn small ghost" data-pa="cover">${p.helper ? "关闭封面" : "开启封面"}</button>
      </div>
      ${p.assets ? `<button class="puse" data-pa="assets">用到素材库里的 ${p.assets} 个素材</button>` : `<div class="puse none">没用到素材库里的素材</div>`}
    </div></article>`;
}
function renderProjGrid() {
  const all = S.projs, q = S.pq.toLowerCase();
  const g = $("#grid");
  if (!all) { $("#resultbar").innerHTML = ""; g.innerHTML = `<div class="empty"><h2>正在读取工程</h2></div>`; return; }
  const list = all.filter(p => !q || (p.name + " " + p.path).toLowerCase().includes(q))
    .sort((a, b) => (b.running - a.running) || (b.opened || 0) - (a.opened || 0) || a.name.localeCompare(b.name));
  $("#resultbar").innerHTML = `<b>${list.length}</b><span>个工程${q ? `（共 ${all.length} 个）` : ""}</span><span class="muted small">按最近打开排序</span>`;
  g.innerHTML = list.length ? list.map(projCardHTML).join("")
    : `<div class="empty">${all.length ? `<h2>没有符合条件的工程</h2>` : `<h2>还没有 Unity 工程</h2><p>在设置里添加放工程的文件夹，或者直接添加一个工程。</p><button class="btn primary" id="btnProjAdd">添加工程…</button>`}</div>`;
}
async function addProject() {
  const p = await pickInto("选择 Unity 工程文件夹（里面有 Assets）");
  if (!p) return;
  const r = await api("/api/import/project", { path: p });
  if (!r.ok) { toast(r.err || "这不是 Unity 工程", 4000); return; }
  toast("已添加：" + r.name); await load(); loadProjects();
}
async function projAction(what, path, btn) {
  const p = (S.projs || []).find(x => x.path === path); if (!p) return;
  if (what === "folder") return openPath(p.path);
  if (what === "ai") return openAI(p.path);
  if (what === "assets") { S.usage = "p:" + p.name; setView("lib"); renderSide(); renderGrid(); return; }
  if (what === "unity") {
    btn.disabled = true;
    const r = await api("/api/project/open", { path: p.path });
    if (!r.ok) { btn.disabled = false; toast(r.err || "没能打开", 5000); return; }
    toast(`正在用 Unity ${p.unity} 打开 ${p.name}`, 3500); setTimeout(loadProjects, 4000); return;
  }
  if (what === "cover") {
    const on = !p.helper;
    if (on && !confirm(`给「${p.name}」开启封面？\n\n会在这个工程的 Packages 里放一个小插件（com.miovrc.projectcard）。打开工程、保存场景时，它给场景里的模型拍一张正面照当封面。\n只在 Unity 编辑器里运行，不改场景，也不会跟模型一起上传。`)) return;
    if (!on && !confirm(`关闭「${p.name}」的封面？会删掉插件和拍好的照片。`)) return;
    const r = await api("/api/project/cover", { path: p.path, on });
    if (!r.ok) { toast(r.err || "没能完成", 5000); return; }
    toast(on ? (r.running ? "已开启。切回 Unity，等它编译完就会拍封面" : "已开启。下次打开这个工程时会拍封面") : "已关闭封面", 4500);
    loadProjects();
  }
}
function renderWeb() {
  const mode = S.data.paneMode, host = $("#webhost");
  const key = mode + "|" + S.view + "|" + (S.web.st.open === false);
  if (host.dataset.k !== key) {
    host.dataset.k = key;
    host.innerHTML = !mode ? `<div class="webnote"><h2>没有内置浏览器</h2><p>没找到 WebView2、Edge 或 Chrome，页面会在默认浏览器里打开。</p><button class="btn" data-w="reopen">在浏览器打开闲鱼</button></div>`
      : S.web.st.open === false ? `<div class="webnote"><h2>页面已关闭</h2><button class="btn" data-w="reopen">重新打开</button></div>`
      : mode === "window" ? `<div class="webnote"><h2>页面在单独的窗口里</h2><p>这台电脑没有 WebView2，页面开在一个单独的窗口中；上面的按钮可以控制它。</p><button class="btn" data-w="front">显示窗口</button></div>`
      : `<div class="webnote muted">正在打开…</div>`;
  }
  renderWebBar();
}
function renderWebBar() {
  const st = S.web.st || {}, xy = S.view === "xianyu", pan = S.view === "lib", back = S.web.back;
  const backLabel = back ? (back.key ? "返回素材" : back.view === "lib" ? "返回素材库" : back.view === "xianyu" ? "返回闲鱼" : "返回") : pan ? "返回素材库" : "返回列表";
  const bd = S.data.baidu || {}, gm = S.data.gumroad || {};
  const html = `<button class="wb" data-w="back" title="后退"${st.back ? "" : " disabled"}>${ICON.back}</button>
    <button class="wb" data-w="forward" title="前进"${st.fwd ? "" : " disabled"}>${ICON.fwd}</button>
    <button class="wb" data-w="${st.loading ? "stop" : "reload"}" title="${st.loading ? "停止" : "刷新"}">${st.loading ? ICON.x : ICON.upd}</button>
    <div class="wtitle" title="${esc(st.url || "")}"><b>${esc(st.title || (st.loading ? "正在打开…" : st.url || ""))}</b><span>${esc(st.url || "")}</span></div>
    ${xy ? `<button class="btn small" data-w="grab" title="把聊天里选中的网盘分享收进素材库">收录网盘链接</button>`
      : pan && S.web.loaded === "gumroad" ? `<span class="wnote">${gm.loggedIn ? "Gumroad：" + esc(gm.name || "已登录") : gm.waiting ? "登录后会自动读取已购" : ""}</span>`
      : pan ? `<span class="wnote">${bd.loggedIn ? "百度网盘：" + esc(bd.name || "已登录") : bd.waiting ? "登录后会自动保存" : ""}</span>`
      : `<button class="btn small" data-w="sync"${S.data.purchaseBusy ? " disabled" : ""}>${S.data.purchaseBusy ? "正在同步…" : "同步已购"}</button>`}
    <button class="wb" data-w="copy" title="复制链接"${st.url ? "" : " disabled"}>${ICON.copy}</button>
    <button class="wb" data-w="external" title="在默认浏览器中打开"${st.url ? "" : " disabled"}>${ICON.ext}</button>
    ${!xy || back ? `<button class="btn small" data-w="close">${backLabel}</button>` : ""}`;
  const bar = $("#webbar");
  if (bar.dataset.h !== html) { bar.dataset.h = html; bar.innerHTML = html; }
}
async function webAction(w) {
  const st = S.web.st || {};
  switch (w) {
    case "close": return closeWeb();
    case "sync": return startSync();
    case "copy": try { await navigator.clipboard.writeText(st.url || ""); toast("已复制链接"); } catch (e) { toast("复制失败"); } return;
    case "reopen":
      if (!S.data.paneMode) return openURL(S.web.last.xianyu || XY_HOME);
      if (S.view === "lib" && S.web.loaded === "gumroad") return openWeb(S.web.last.gumroad || S.data.gumroad.libraryUrl, "gumroad");
      if (S.view === "lib") return openWeb(S.web.last.pan || S.data.panWeb + "/disk/main", "pan");
      return openWeb(S.view === "xianyu" ? S.web.last.xianyu || XY_HOME : S.web.last.booth || boothHome(), S.view === "xianyu" ? "xianyu" : "booth");
    case "grab": {
      const r = await api("/api/pane/act", { act: "selection" });
      const text = (r.text || "").trim();
      if (!/pan\.baidu\.com|pan\.quark\.cn|aliyundrive|alipan|123pan|lanzou/i.test(text)) { toast("先在聊天里选中网盘分享文字（链接和提取码），再点这里", 4000); return; }
      openPanAdd(text); return;
    }
  }
  const r = await api("/api/pane/act", { act: w });
  if (!r.ok && r.err) toast(r.err, 3000);
  webPoll(true);
}
let webTimer;
async function webPoll(now) {
  clearTimeout(webTimer);
  if (!S.data || !S.data.paneMode || !webVisible()) return;
  try {
    const st = await api("/api/pane/state", {});
    const was = S.web.st || {};
    if (st.open && st.url) S.web.last[S.web.loaded || (S.view === "xianyu" ? "xianyu" : S.view === "lib" ? "pan" : "booth")] = st.url;
    if (st.dl !== S.web.dl) { if (st.dl > (S.web.dl || 0)) poll(true); S.web.dl = st.dl; }
    if (S.web.loaded === "xianyu" && st.url !== was.url) saveUI();
    if (!st.open && was.open && !was.loading && S.data.paneMode === "window") { // the window was closed
      S.web.st = { open: false };
      if (S.view !== "xianyu") { S.web.open[S.view] = false; S.web.loaded = ""; renderAll(); return; }
      renderWeb();
    } else if (st.open || !was.loading) {
      if (!was.open && st.open) placedKey = "";
      S.web.st = st; renderWeb();
    }
  } catch (e) {}
  webTimer = setTimeout(webPoll, 900);
}
// keep the built-in page over #webhost; hidden while a drawer, dialog or picture viewer covers it
let placedKey = "";
function placePane() {
  if (!S.data || S.data.paneMode !== "native") return;
  const lb = $("#lightbox");
  const show = webVisible() && S.web.st.open !== false && !$("#modal").classList.contains("on") && !$("#drawer").classList.contains("on") && !(lb && lb.classList.contains("on"));
  let r = { x: 0, y: 0, w: 0, h: 0 };
  if (show) { const b = $("#webhost").getBoundingClientRect(); r = { x: b.left, y: b.top, w: b.width, h: b.height }; }
  document.body.classList.toggle("paneon", show);
  const key = [show, r.x, r.y, r.w, r.h, devicePixelRatio].join(",");
  if (key === placedKey) return;
  placedKey = key;
  api("/api/pane/place", Object.assign(r, { dpr: devicePixelRatio, show })).catch(() => { placedKey = ""; });
}
setInterval(placePane, 120);

// ---------- actions ----------
function findAsset(key) { return (S.data.assets || []).find(a => a.key === key); }
async function openPath(p) {
  const r = await api("/api/open", { path: p });
  if (!r.ok) toast(r.err || "打不开这个位置");
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
    if (u.sharePwd) await copyText(u.sharePwd, "已复制提取码 " + u.sharePwd);
    openLink(shareLink(u)); // inside, with the netdisk login, when the program has a built-in page
  } else if (u.panPath) openLink(panPathURL(u.panPath));
}
function boothItemURL(id) { return ((S.data && S.data.boothWeb) || "https://booth.pm") + "/ja/items/" + id; }
function boothSearchURL(q) { return "https://booth.pm/ja/search/" + encodeURIComponent((q || "").trim()); }
function boothURL(a) {
  if (!a.boothId) return boothSearchURL(a.boothQuery || a.name);
  return (a.user.boothUrl && a.user.boothUrl.includes(a.boothId) && a.user.boothUrl) || (a.booth && a.booth.url) || boothItemURL(a.boothId);
}
function panLine(a) {
  const l = a.pan;
  if (a.panPath) return "网盘：" + a.panPath.split("/").filter(Boolean).slice(-3).join(" / ");
  if (!l) return S.data.tasks && (S.data.tasks.find(t => t.name === "pan") || {}).running ? "读取中…" : "百度网盘分享";
  if (l.err && !l.files) return "网盘：" + l.err;
  return `百度网盘，${l.count} 个文件，${fmtSize(l.size)}`;
}
function openBuyPage(a) { if (a.purchase) openLink(a.purchase.pageUrl); }
async function startSync() {
  const mode = S.data.paneMode;
  if (mode) { // the Booth page shows up in the Booth tab
    closeModal(); closeDrawer();
    S.web.back = S.view === "shop" ? null : { view: S.view };
    S.web.open.shop = true; S.web.loaded = "booth"; S.web.st = { loading: true, title: "Booth 已购" };
    if (S.view !== "shop") { S.view = "shop"; saveUI(); $("#q").value = S.shop.q; if (!S.shop.started) shopSearch(true); }
    renderAll();
  }
  const r = await api("/api/purchases/sync", {});
  toast(!r.ok ? "正在同步" : mode === "native" ? "在下面的 Booth 页面登录后，会自动读取已购" : mode ? "在打开的窗口里登录 Booth，登录后会自动读取" : "已打开 Booth 窗口，登录后自动读取", 4000);
  S.data.purchaseBusy = true; renderStatus(); poll(true);
  if (mode) { placedKey = ""; webPoll(true); }
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
// what one entry of a share is for: its base bodies, or PSD / material / readme
function partTag(p) {
  if (!p) return "";
  if (p.kind === "psd") return `<span class="psdtag">PSD</span>`;
  if (p.kind === "material") return `<span class="ktag mat">材质</span>`;
  if (p.kind === "doc") return `<span class="ktag">说明</span>`;
  if (p.kind === "bonus") return `<span class="ktag">特典</span>`;
  return (p.bases || []).map(b => `<span class="base">${esc(b)}</span>`).join("");
}
function extTag(n) {
  const m = /\.(zip|rar|7z|unitypackage|psd|clip|png|jpe?g|gif|mp4|txt|pdf|blend|fbx|url)$/i.exec(n || "");
  return m ? `<span class="ext">${m[1].toLowerCase()}</span>` : "";
}
// pick: checkboxes to download only some of it ({set: picked paths, got: paths downloaded before})
function panTree(files, depth, prefix, parts, pick) {
  const few = (files || []).length <= 3;
  return (files || []).map(f => {
    const path = prefix + "/" + f.n;
    const tag = parts ? partTag(parts.get(path)) : "";
    let box = "", got = "";
    if (pick) {
      const on = panCov(path, pick.set), part = !on && f.d && [...pick.set].some(x => x.startsWith(path + "/"));
      box = `<input type="checkbox" class="psel${part ? " part" : ""}" data-psel="${esc(path)}"${on ? " checked" : ""} aria-label="选择 ${esc(f.n)}">`;
      if (pick.got.includes(path)) got = `<span class="ktag got">已下载</span>`;
    }
    if (!f.d) return `<div class="pf">${box}${pick ? `<i class="fi"></i>` : ""}<span class="nm">${esc(f.n)}</span>${got}${tag ? `<span class="tags">${tag}</span>` : ""}${extTag(f.n)}<span class="s">${fmtSize(f.s)}</span></div>`;
    const open = S.panOpen.has(path) || (depth === 0 && few && !S.panClosed.has(path));
    return `<details data-pp="${esc(path)}"${open ? " open" : ""}><summary>${box}${ICON.folder}<span class="nm">${esc(f.n)}</span>${got}${tag ? `<span class="tags">${tag}</span>` : ""}${f.p ? `<span class="muted small">（未列全）</span>` : ""}${pick ? `<span class="s">${fmtSize(panNodeSize(f))}</span>` : ""}</summary>
      <div class="kids">${panTree(f.c, depth + 1, path, parts, pick)}</div></details>`;
  }).join("");
}
// ---- picking parts of a netdisk card to download ----
function panCov(p, set) { for (const x of set) if (p === x || p.startsWith(x + "/")) return true; return false; }
function panNode(files, p) {
  let cur = files, node = null;
  for (const seg of p.split("/").slice(1)) { node = (cur || []).find(f => f.n === seg); if (!node) return null; cur = node.c; }
  return node;
}
function panNodeSize(f) { return f.d ? (f.c || []).reduce((n, c) => n + panNodeSize(c), 0) : (f.s || 0); }
// can parts of this card be picked? (a share with more than one file, downloadable here)
function panPickable(a) {
  const l = a.pan;
  return !!(a.user.shareUrl && l && (l.files || []).length && (a.panOnly || a.fromPan) && (l.count || 0) >= 2);
}
function togglePanSel(a, p) {
  const files = a.pan.files, set = S.panSel;
  if (panCov(p, set)) {
    if (set.has(p)) set.delete(p);
    else { // inside a picked folder: pick the rest of that folder instead
      const top = [...set].find(x => p.startsWith(x + "/"));
      set.delete(top);
      let at = top;
      for (const seg of p.slice(top.length + 1).split("/")) {
        const node = panNode(files, at);
        ((node && node.c) || []).forEach(k => { if (k.n !== seg) set.add(at + "/" + k.n); });
        at += "/" + seg;
      }
    }
  } else {
    for (const x of [...set]) if (x.startsWith(p + "/")) set.delete(x);
    set.add(p);
    // every part of a folder picked: the folder itself
    let at = p;
    while (at.lastIndexOf("/") > 0) {
      const parent = at.slice(0, at.lastIndexOf("/")), node = panNode(files, parent);
      if (!node || node.p || !(node.c || []).every(k => set.has(parent + "/" + k.n))) break;
      node.c.forEach(k => set.delete(parent + "/" + k.n)); set.add(parent); at = parent;
    }
  }
}
function panSelInfo(a) {
  let size = 0, files = 0;
  const count = f => f.d ? (f.c || []).reduce((n, c) => n + count(c), 0) : 1;
  for (const p of S.panSel) { const f = panNode(a.pan.files, p); if (f) { size += panNodeSize(f); files += count(f); } }
  return { n: S.panSel.size, size, files };
}
function panSelLine(a) {
  const i = panSelInfo(a);
  return `已选 ${i.n} 项${i.files > i.n ? `（${i.files} 个文件）` : ""}，${fmtSize(i.size)}`;
}
function panSelBar(a) {
  if (!S.panSel.size) return "";
  const busy = (j => j && !["done", "failed", "login"].includes(j.stage))(panJobFor(a));
  return `<div class="panselbar" id="panselbar"><span>${esc(panSelLine(a))}</span><span class="spacer"></span>
    ${busy ? "" : `<button class="btn small primary" data-d="pandlsel">${ICON.download}<span>下载所选</span></button>`}<button class="btn small ghost" data-d="panselclear">清除选择</button></div>`;
}
// one line on what the share holds: which base bodies, and what comes with them
function panSummary(a) {
  const ps = a.panParts || [];
  if (ps.length < 2) return "";
  const bases = [...new Set(ps.flatMap(p => p.bases || []))];
  const extra = [["psd", "PSD 源文件"], ["material", "材质包"], ["doc", "说明"], ["bonus", "特典"]].filter(([k]) => ps.some(p => p.kind === k)).map(x => x[1]);
  const n = ps.filter(p => p.kind === "variant").length;
  return `<div class="pansum">
    ${bases.length ? `<div class="r"><span class="k">适配素体</span><span class="v">${bases.map(b => `<span class="base">${esc(b)}</span>`).join("")}</span></div>` : ""}
    <div class="r"><span class="k">内容</span><span class="v">${n ? `${n} 个下载` : ""}${extra.length ? `${n ? "，另有 " : ""}${extra.join("、")}` : ""}</span></div></div>`;
}
function panSec(a) {
  const u = a.user, l = a.pan, running = taskRunning("pan");
  const got = a.panGot || [];
  const pick = panPickable(a) ? { set: S.panSel, got: got.includes("/") ? [] : got } : null;
  let list = "";
  if (u.shareUrl) {
    if (!l) list = `<div class="muted small panmsg">${running ? "读取中…" : "保存后读取文件列表"}</div>`;
    else if (l.err && !(l.files || []).length) list = `<div class="small err panmsg">读取失败：${esc(l.err)}</div>`;
    else list = `<div class="panhead"><b>${l.count} 个文件</b><span>${fmtSize(l.size)}</span>${l.truncated ? `<span class="muted small">只列出了一部分</span>` : ""}<span class="muted small right">读取于 ${esc(fmtTime(l.fetched))}</span></div>
      ${panSummary(a)}
      ${pick && !S.panSel.size ? `<div class="hint2 pickhint">只要其中一部分？勾选要下载的文件或文件夹。</div>` : ""}
      <div class="pantree${pick ? " picking" : ""}">${panTree(l.files, 0, "", new Map((a.panParts || []).map(p => [p.path, p])), pick)}</div>${pick ? panSelBar(a) : ""}${l.err ? `<div class="small err">重新读取失败：${esc(l.err)}</div>` : ""}`;
  }
  if (a.panParent) return panItemSec(a, list);
  if (a.splitInto || a.canSplit) list = `<div class="splitnote">${a.splitInto ? `已拆成 ${a.splitInto} 个素材` : `包含 ${a.canSplit} 个素材`}
      <button class="btn small" data-d="nosplit">${a.splitInto ? "合并显示" : "拆开显示"}</button></div>` + list;
  const head = `<h4 class="withact">${a.panOnly ? "网盘内容" : "网盘"}${u.shareUrl ? `<button class="h4act" data-d="panrefresh"${running ? " disabled" : ""}>${running ? "读取中…" : "重新读取"}</button>` : ""}</h4>`;
  // a share-only asset: what is in it comes first, the link fields after
  return `<div class="sec">${head}${a.panOnly ? list : ""}
      <div class="field${a.panOnly ? " gap" : ""}"><label>粘贴分享</label><input id="f_paste" placeholder="粘贴分享文字，自动识别链接和提取码"></div>
      <div class="field"><label>分享链接</label><input id="f_shareUrl" value="${esc(u.shareUrl || "")}" placeholder="https://pan.baidu.com/s/..."></div>
      <div class="field"><label>提取码</label><div class="inline"><input id="f_sharePwd" value="${esc(u.sharePwd || "")}" placeholder="可不填"><button class="btn" data-d="copypwd">复制</button></div></div>
      <div class="field"><label>网盘路径</label><div class="inline"><input id="f_panPath" value="${esc(u.panPath || "")}" placeholder="可不填"><button class="btn" data-d="openpanpath">打开</button></div></div>
      ${a.panOnly ? "" : list}
    </div>`;
}
function panItemSec(a, list) {
  const parent = findAsset(a.panParent);
  return `<div class="sec"><h4>网盘内容</h4>
    <div class="fromshare"><div><div class="muted small">来自分享</div><div class="nm">${esc(parent ? parent.name : "网盘分享")}</div>
      <div class="muted small">${esc(a.panPath)}</div></div>
      <div class="btnrow"><button class="btn small" data-d="pan">打开网盘分享</button><button class="btn small" data-d="copypwd">复制提取码</button>
      ${parent ? `<button class="btn small ghost" data-vgo="${esc(parent.key)}">整个分享</button>` : ""}</div></div>
    ${list}
    <input id="f_paste" type="hidden" value=""><input id="f_shareUrl" type="hidden" value=""><input id="f_sharePwd" type="hidden" value="${esc(a.user.sharePwd || "")}"><input id="f_panPath" type="hidden" value="">
  </div>`;
}
const BOOTH_SRC = { user: "手动关联", name: "按文件夹名里的编号识别", url: "按素材里的 Booth 链接识别", library: "来自已购记录", auto: "按名称自动匹配，请确认" };
function newsTitle(a) {
  const out = [];
  if (a.newerOnBooth) out.push(`Booth 有新版本 ${a.newerOnBooth}（本地 ${a.localVer}）`);
  if (a.boothNews) out.push("商品页有改动：" + a.boothNews);
  if (a.panNews) out.push(`分享有变化：新增 ${(a.panNews.added || []).length}，删除 ${(a.panNews.removed || []).length}`);
  if (a.shareErr) out.push("分享读取失败：" + a.shareErr);
  return out.join("\n");
}
function fileList(paths, max) {
  const names = (paths || []).map(p => p.split("/").filter(Boolean).slice(-2).join("/"));
  return names.slice(0, max).map(esc).join("、") + (names.length > max ? ` 等 ${names.length} 个` : "");
}
// changes made elsewhere: a newer download on Booth, an edited Booth page, a changed share
function newsSec(a) {
  if (!hasNews(a)) return "";
  let h = "";
  if (a.newerOnBooth) h += `<div class="news"><div class="t">Booth 有新版本 <b>${esc(a.newerOnBooth)}</b>（本地 ${esc(a.localVer)}）</div>
    <div class="btnrow">${a.newerDl ? `<button class="btn small" data-dl="${esc(a.newerDl)}">下载新版本</button>` : `<button class="btn small" data-d="buypage">打开下载页</button>`}</div></div>`;
  if (a.boothNews) h += `<div class="news"><div class="t">商品页有改动（${esc(fmtTime(a.boothNewsAt))}）：${esc(a.boothNews)}</div>
    <div class="btnrow"><button class="btn small" data-d="booth">打开商品页</button><button class="btn small ghost" data-seen="booth">知道了</button></div></div>`;
  if (a.panNews) {
    const n = a.panNews;
    h += `<div class="news"><div class="t">分享有变化（${esc(fmtTime(n.at))}）</div>
      ${(n.added || []).length ? `<div class="d">新增 ${n.added.length} 个：${fileList(n.added, 6)}</div>` : ""}
      ${(n.removed || []).length ? `<div class="d">删除 ${n.removed.length} 个：${fileList(n.removed, 4)}</div>` : ""}
      <div class="btnrow"><button class="btn small" data-d="pan">打开网盘分享</button><button class="btn small ghost" data-seen="pan">知道了</button></div></div>`;
  }
  if (a.shareErr) h += `<div class="news warn"><div class="t">分享读取失败：${esc(a.shareErr)}</div>
    <div class="d">链接可能失效了，下面是上次读到的内容。</div></div>`;
  return `<div class="sec"><h4>更新</h4>${h}</div>`;
}
// the other downloads of the same product
function variantSec(a) {
  const all = groupAll(a);
  if (all.length < 2) return "";
  const rows = all.slice().sort((x, y) => (!!x.psd - !!y.psd) || (x.variant || x.name).localeCompare(y.variant || y.name, "zh-CN")).map(m => {
    const label = m.psd ? `<span class="psdtag">PSD</span>` : (m.bases || []).length
      ? m.bases.slice(0, 4).map(b => `<span class="base">${esc(b)}</span>`).join("") + (m.bases.length > 4 ? `<span class="base more">+${m.bases.length - 4}</span>` : "")
      : `<span class="vlabel">${esc(m.variant || "通用")}</span>`;
    const where = m.virtual ? "已购，未下载" : m.panOnly ? "百度网盘" : shortPath((m.locations || [])[0]);
    const u = usedStatus(m);
    return `<div class="vrow${m.key === a.key ? " cur" : ""}">
      <div class="vl">${label}</div>
      <div class="vn"><div class="t" title="${esc(m.name)}">${esc(m.name)}</div><div class="w" title="${esc(where)}">${esc(where)}</div></div>
      <span class="vs">${m.virtual ? "未下载" : fmtSize(m.size)}</span>
      ${u ? `<span class="st ${u}">${u === "used" ? "在用" : "部分"}</span>` : ""}
      <button class="act" data-vopen="${esc(m.key)}" title="${m.virtual ? "下载页" : m.panOnly ? "网盘" : "打开文件夹"}">${m.virtual ? ICON.download : m.panOnly ? ICON.cloud : ICON.folder}</button>
      ${m.key === a.key ? `<span class="vcur">正在看</span>` : `<button class="btn small ghost" data-vgo="${esc(m.key)}">查看</button>`}
    </div>`;
  }).join("");
  return `<div class="sec"><h4>版本（${all.length}）</h4>
    <div class="vrows">${rows}</div></div>`;
}
// texture sources inside the asset
function psdSec(a) {
  if (!a.psdCount) return "";
  const root = ((a.locations || []).find(l => l.kind === "dir") || {}).path || "";
  const rel = p => root && p.toLowerCase().startsWith(root.toLowerCase()) ? p.slice(root.length).replace(/^[\\/]+/, "") : p;
  const listed = (a.psds || []).length;
  const rows = (a.psds || []).map(f => `<div class="psdrow"><span class="p" title="${esc(f.path)}">${esc(rel(f.path))}</span>
      <span class="d">${f.w ? f.w + "×" + f.h : ""}</span><span class="s">${fmtSize(f.size)}</span>
      <button class="act" data-psd="${esc(f.path)}" title="打开文件夹">${ICON.folder}</button></div>`).join("");
  const more = [];
  if (a.psdInZip) more.push(`压缩包里有 ${a.psdInZip} 个`);
  if (a.psdCount - listed - (a.psdInZip || 0) > 0) more.push(`另有 ${a.psdCount - listed - (a.psdInZip || 0)} 个没列出`);
  return `<div class="sec"><h4>PSD 源文件（${a.psdCount}）</h4>
    ${rows ? `<div class="psdrows">${rows}</div>` : ""}${more.length ? `<div class="muted small" style="margin-top:6px">${more.join("，")}</div>` : ""}</div>`;
}
// style tags for outfits: chips to toggle, and a box to add a new tag
function styleField(a) {
  const u = a.user;
  const cat = u.category || a.category;
  if (cat !== "衣服" && !(a.styles || []).length) return "";
  const picked = new Set(S.stylePick || a.styles || []);
  const auto = !u.stylesSet && !S.stylePick && (a.autoStyles || []).length;
  return `<div class="field"><label>风格标签</label><div>
      <div class="stchips">${styleList().map(st => `<button class="stchip${picked.has(st) ? " on" : ""}" data-st="${esc(st)}">${esc(st)}</button>`).join("")}
        <input id="f_newstyle" class="stnew" placeholder="＋ 新标签" maxlength="12"></div>
      ${auto || u.stylesSet || S.stylePick ? `<div class="hint">${auto ? "自动" : `<button class="linkbtn" data-d="stauto">恢复自动</button>`}</div>` : ""}
    </div></div>`;
}
function boothSec(a) {
  const b = a.booth, src = a.boothSrc;
  if (isGum(a)) return `<div class="sec"><h4>Gumroad 商品</h4><div class="bitem">
      <div class="bname">${esc((b && b.name) || a.name)}</div>
      <div class="muted small">${esc((b && b.shop) || "")}</div>
      <div class="hint bsrc">来自 Gumroad 已购记录</div>
      <div class="btnrow">${gumPageURL(a) ? `<button class="btn small" data-d="booth">打开商品页</button>` : ""}</div></div></div>`;
  let h = `<div class="sec"><h4>Booth 商品</h4>`;
  if (a.boothId) {
    const imgs = (b && b.images || []).slice(0, 6);
    h += `<div class="bitem">
      ${imgs.length ? `<div class="bimgs">${imgs.map(u => `<img loading="lazy" src="/rthumb?u=${encodeURIComponent(boothImg(u, false))}" data-big="${esc(boothImg(u, true))}" alt="">`).join("")}</div>` : ""}
      <div class="bname">${esc(b && b.name ? b.name : "Booth #" + a.boothId)}</div>
      <div class="muted small">${[b && b.shop, b && b.price, b && b.category].filter(Boolean).map(esc).join("　") || (taskRunning("booth") ? "读取中…" : "")}</div>
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
    h += `<div class="muted small" style="margin-bottom:8px">${a.user.noBooth ? "已标记为非 Booth 商品" : "未关联"}</div>` + searchPanel(a);
  }
  return h + `</div>`;
}
// Booth image urls: "/c/<size>/" picks a crop; without it the full picture is served.
function boothImg(u, big) {
  const m = /^(https:\/\/booth\.pximg\.net)\/c\/[0-9a-z_]+\//.exec(u || "");
  if (!m) return u;
  return big ? u.replace(m[0], m[1] + "/") : u.replace(m[0], m[1] + "/c/300x300_a2_g5/");
}
function showBig(list, i, direct) {
  let lb = $("#lightbox");
  if (!lb) { lb = document.createElement("div"); lb.id = "lightbox"; document.body.appendChild(lb); }
  const n = list.length; i = (i + n) % n;
  lb.innerHTML = `<img src="${direct ? esc(list[i]) : "/rthumb?u=" + encodeURIComponent(list[i])}" alt="">${n > 1 ? `<button class="lbnav prev" aria-label="上一张">‹</button><button class="lbnav next" aria-label="下一张">›</button><div class="lbcount">${i + 1} / ${n}</div>` : ""}`;
  lb.classList.add("on");
  lb.onclick = e => {
    e.stopPropagation();
    if (e.target.classList.contains("prev")) return showBig(list, i - 1, direct);
    if (e.target.classList.contains("next")) return showBig(list, i + 1, direct);
    lb.classList.remove("on");
  };
  lb.dataset.i = i; S.lb = { list, i, direct };
}
function searchPanel(a) {
  const bs = S.bs && S.bs.key === a.key ? S.bs : { key: a.key, q: a.boothQuery || a.name, hits: a.boothHits || null };
  let res = "";
  if (bs.loading) res = `<div class="muted small">搜索中…</div>`;
  else if (bs.err) res = `<div class="small err">${esc(bs.err)}</div>`;
  else if (bs.hits && !bs.hits.length) res = `<div class="muted small">没有结果</div>`;
  else if (bs.hits) res = `<div class="bhits">${bs.hits.slice(0, 12).map(h => `<div class="bhit${h.full ? " full" : ""}">
      <img loading="lazy" src="/rthumb?u=${encodeURIComponent(h.thumb || "")}" alt="">
      <div class="t" title="${esc(h.name)}">${esc(h.name)}</div>
      <div class="m">${esc(h.shop || h.shopSub || "")}${h.price ? "　" + esc(h.price) : ""}</div>
      <div class="m">${esc(h.category || "")}</div>
      <div class="hb"><button class="btn small" data-link="${esc(h.id)}">就是这个</button><button class="btn small ghost" data-url="${esc(boothItemURL(h.id))}">打开</button></div>
    </div>`).join("")}</div>`;
  return `<div class="bsearch"><input id="bs_q" value="${esc(bs.q || "")}" placeholder="商品名">
      <button class="btn small" data-d="bsearch">${ICON.search}<span>搜索</span></button>
      <button class="btn small ghost" data-d="bopen">在浏览器搜</button>
      ${!a.boothId && !a.user.noBooth ? `<button class="btn small ghost" data-d="bnone">不是 Booth 商品</button>` : ""}</div>${res}`;
}
function descBlock(a) {
  const b = a.booth, d = S.desc[a.key] || {};
  const text = d.showZh && d.zh ? d.zh : b.desc;
  return `<details class="bdesc" data-desc="${esc(a.key)}"${d.open ? " open" : ""}><summary>商品说明${d.showZh ? "（译文）" : ""}</summary>
    <div class="desc">${esc(text)}</div>
    <div class="btnrow"><button class="btn small" data-d="dtrans"${d.loading ? " disabled" : ""}>${d.loading ? "翻译中…" : d.showZh ? "原文" : "翻译"}</button></div></details>`;
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
    <button class="btn small" data-url="${esc(o.url)}">订单页</button></div>`).join("");
  if (p.source === "gumroad") return `<div class="sec" id="dlsec"><h4 class="withact">Gumroad 已购${p.variant ? "（" + esc(p.variant) + "）" : ""}${(p.dls || []).length > 1 ? `<button class="h4act" data-d="dlall">全部下载</button>` : ""}</h4>
    ${dlRows(p)}
    <div class="order"><span class="d">${esc(((p.orders || [])[0] || {}).date || "")}</span><span></span>
      <button class="btn small" data-url="${esc(p.pageUrl)}">下载页</button>${p.productUrl ? `<button class="btn small" data-url="${esc(p.productUrl)}">商品页</button>` : ""}<button class="btn small" data-url="${esc(p.libraryUrl)}">已购列表</button></div>
    ${p.matched ? `<div class="hint" style="margin-top:6px">按下载位置或文件名对应</div>` : ""}
  </div>`;
  return `<div class="sec" id="dlsec"><h4 class="withact">Booth 已购${p.gift ? "（礼物）" : ""}${(p.dls || []).length > 1 ? `<button class="h4act" data-d="dlall">全部下载</button>` : ""}</h4>
    ${dlRows(p)}
    ${orders}
    <div class="order"><span></span><button class="btn small" data-url="${esc(p.libraryUrl)}">已购列表</button></div>
    ${p.matched ? `<div class="hint" style="margin-top:6px">按文件名对应</div>` : ""}
  </div>`;
}
// ---------- one-click import into a Unity project ----------
function canImport(a) { return isLocal(a) && !a.psd && !!(a.packages || a.archives); }
function lastProject() {
  if (S.impProj !== undefined) return S.impProj;
  try { return localStorage.getItem("vrclib.project") || ""; } catch (e) { return ""; }
}
function setProject(p) { S.impProj = p; try { localStorage.setItem("vrclib.project", p); } catch (e) {} }
// the netdisk section has its own (a folder card shows the import section too)
function projSelect(id) {
  const ps = S.data.projects || [], cur = lastProject();
  const has = ps.some(p => p.path === cur);
  return `<select id="${id || "imp_proj"}" class="projsel" title="${esc(cur)}">${!cur ? `<option value="" selected>选择 Unity 工程…</option>` : ""}
    ${ps.map(p => `<option value="${esc(p.path)}"${p.path === cur ? " selected" : ""}>${esc(p.name)}</option>`).join("")}
    ${cur && !has ? `<option value="${esc(cur)}" selected>${esc(rootLabel(cur))}</option>` : ""}
    <option value="__pick">选择其他工程…</option></select>`;
}
function chosenProject(id) {
  const v = ($("#" + (id || "imp_proj")) || {}).value || "";
  if (!v || v === "__pick") { toast("先选择要导入的 Unity 工程"); return ""; }
  return v;
}
async function pickProject(sel) {
  const p = await pickInto("选择 Unity 工程文件夹（里面有 Assets）");
  if (!p) { sel.value = lastProject() || ""; return; }
  const r = await api("/api/import/project", { path: p });
  if (!r.ok) { toast(r.err || "这不是 Unity 工程", 4000); sel.value = lastProject() || ""; return; }
  setProject(r.path); await load();
}
function impJobFor(a) {
  const j = S.data.importJob; if (!j) return null;
  return j.key === a.key || (a.fromPan && j.key === a.fromPan) || (a.purchase && j.key === "purchase:" + a.purchase.id) ? j : null;
}
function impBusy() { const j = S.data.importJob; return !!(j && !["done", "failed"].includes(j.stage)); }
function impResult(j) {
  const proj = rootLabel(j.project);
  if (j.stage === "failed") {
    const tool = /7-Zip/.test(j.err || "");
    return `<div class="impres err"><div>${esc(j.err || j.msg || "导入失败")}</div>
      ${(j.failed || []).length ? `<div class="small">${j.failed.map(esc).join("<br>")}</div>` : ""}
      <div class="btnrow">${tool ? `<button class="btn small" data-url="https://www.7-zip.org/">下载 7-Zip</button>` : ""}<button class="btn small ghost" data-d="impdismiss">知道了</button></div></div>`;
  }
  const tops = (j.tops || []).slice(0, 4).join("、") + ((j.tops || []).length > 4 ? " 等" : "");
  return `<div class="impres ok"><div>已导入到 <b>${esc(proj)}</b>：${(j.imported || []).length} 个 unitypackage，${j.files} 个文件</div>
    <div class="small">${tops ? `放在 ${esc(tops)}。` : ""}Unity 开着的话，切回 Unity 就会刷新；没开就等下次打开工程。</div>
    ${j.removed ? `<div class="small">已把 ${j.removed} 个压缩包移到回收站。</div>` : ""}
    ${(j.failed || []).length ? `<div class="small warnline">没解压开：${j.failed.map(esc).join("；")}</div>` : ""}
    <div class="btnrow"><button class="btn small primary" data-aidress="${esc(j.project)}">${ICON.belt}<span>去流水线装上它</span></button><button class="btn small" data-gopen="${esc(j.project)}">打开工程文件夹</button><button class="btn small ghost" data-d="impdismiss">知道了</button></div></div>`;
}
function impProgress(j) {
  if (j.stage === "choose") {
    return `<div class="impchoose"><div>这个素材有不同素体的版本，选要导入的：</div>
      ${(j.choices || []).map((c, i) => `<label class="check"><input type="checkbox" data-ipick="${i}"${c.pick ? " checked" : ""}><span class="n">${esc(c.name)}</span>${(c.bases || []).map(b => `<span class="base">${esc(b)}</span>`).join("")}<span class="s">${fmtSize(c.size)}</span></label>`).join("")}
      <div class="btnrow"><button class="btn small primary" data-d="impgo">导入所选</button><button class="btn small ghost" data-d="impcancel">取消</button></div></div>`;
  }
  const pct = j.total ? Math.round(j.done / j.total * 100) : 0;
  return `<div class="impprog"><div class="t">${esc(j.msg || "正在准备")}</div><div class="bar wide"><i style="width:${pct}%"></i></div>
    <div class="small muted">导入到 ${esc(rootLabel(j.project))}</div></div>`;
}
function importSec(a) {
  const j = impJobFor(a);
  if (!canImport(a) && !j) return "";
  let body;
  if (j && !["done", "failed"].includes(j.stage)) body = impProgress(j);
  else {
    const other = impBusy() && !j;
    body = `${j ? impResult(j) : ""}
      <div class="impform">${projSelect()}<button class="btn primary" data-d="import"${other ? " disabled title=\"正在导入另一个素材\"" : ""}>${ICON.cube}<span>一键导入</span></button></div>
      <div class="impopts"><input id="imp_pwd" placeholder="解压密码（没有就不填）" autocomplete="off"><label class="check"><input type="checkbox" id="imp_recycle"${S.impRecycle === false ? "" : " checked"}>解压后把压缩包移到回收站</label></div>
      <div class="hint2">先解压素材里的压缩包（PSD、分卷、带密码的也行），再把 unitypackage 导入工程的 Assets。${(S.data.arcTools || []).length ? `rar、7z 和分卷用你电脑上的 ${esc(S.data.arcTools[0])} 解压。` : "电脑上没找到解压软件，rar、7z 和分卷解不开，需要先装一个（7-Zip、Bandizip、WinRAR 都行）。"}</div>`;
  }
  return `<div class="sec imp" id="impsec"><h4>导入 Unity 工程</h4>${body}</div>`;
}
async function startImport(a) {
  const project = chosenProject(); if (!project) return;
  setProject(project);
  const r = await api("/api/import/start", { key: a.key, project, pwd: ($("#imp_pwd") || {}).value || "", recycle: S.impRecycle !== false });
  if (!r.ok) { toast(r.err || "没能开始导入", 4000); return; }
  await load(); poll(true);
}
// a virtual Booth purchase: download, then import
function dlImportSec(a) {
  if (!a.virtual || !(a.purchase && (a.purchase.dls || []).length)) return "";
  const j = impJobFor(a);
  return `<div class="sec imp" id="impsec"><h4>下载并导入 Unity 工程</h4>${j && !["done", "failed"].includes(j.stage) ? impProgress(j) : (j ? impResult(j) : "")}
    <div class="impform">${projSelect()}<button class="btn" data-d="dlimport">${ICON.download}<span>下载并导入</span></button></div>
    <div class="hint2">下载完自动解压，再导入到选好的工程。</div></div>`;
}

// ---------- Baidu Netdisk downloads ----------
function panJobFor(a) { return (S.data.panJobs || []).find(j => j.key === a.key || (a.fromPan && j.key === a.fromPan)); }
function panJobBusy(a) { const j = panJobFor(a); return !!(j && !["done", "failed", "login"].includes(j.stage)); }
function panBar(key) {
  const j = (S.data.panJobs || []).find(x => x.key === key);
  if (!j || ["done", "failed", "login"].includes(j.stage)) return "";
  const pct = j.stage === "download" && j.total ? Math.floor(j.done / j.total * 100) : 0;
  return `<div class="cdl" data-panitem="${esc(key)}"><i style="width:${pct}%"></i><span>${j.stage === "download" ? pct + "%" : j.stage === "unpack" ? "解压中" : j.stage === "save" ? "转存中" : "排队中"}</span></div>`;
}
const VIP = { 0: "普通账号", 1: "会员", 2: "超级会员" };
function bdLine() {
  const b = S.data.baidu || {};
  if (!b.loggedIn) return "";
  return `<div class="bdacct"><span>百度网盘：${esc(b.name || "已登录")}${VIP[b.vip] ? `<span class="muted">（${VIP[b.vip]}）</span>` : ""}</span>
    <button class="linkbtn" data-d="bdswitch">更换账号</button><button class="linkbtn" data-d="bdlogout">退出</button></div>`;
}
function fmtSpeed(n) { return n > 0 ? fmtSize(n) + "/s" : ""; }
function panJobHTML(j) {
  const stageText = { queued: "排队中", save: "正在保存到你的网盘", download: "正在下载", unpack: "正在解压", login: "需要登录百度网盘" }[j.stage] || "";
  if (j.stage === "done") {
    return `<div class="impres ok"><div>${esc(j.msg)}</div><div class="small">${esc(j.dir || "")}</div>
      ${(j.failed || []).length ? `<div class="small warnline">没解压开：${j.failed.map(esc).join("；")}（有密码的话，在下面填密码后点「一键导入」）</div>` : ""}
      ${j.saved ? `<div class="small muted">网盘里的副本在「${esc(j.saved)}」，不需要的话可以自己在网盘里删掉。</div>` : ""}
      <div class="btnrow">${j.dir ? `<button class="btn small" data-gopen="${esc(j.dir)}">打开文件夹</button>` : ""}<button class="btn small ghost" data-d="pandismiss">知道了</button></div></div>`;
  }
  if (j.stage === "failed") {
    const captcha = /验证码/.test(j.err || "");
    return `<div class="impres err"><div>${esc(j.err || "下载失败")}</div>
      <div class="btnrow"><button class="btn small" data-d="panretry">重试</button>${captcha ? `<button class="btn small" data-d="pan">在软件里打开分享</button>` : ""}<button class="btn small ghost" data-d="pandismiss">知道了</button></div></div>`;
  }
  const pct = j.stage === "download" && j.total ? Math.floor(j.done / j.total * 100) : 0;
  const line = j.stage === "download" ? `${stageText} ${j.files + 1 > j.fileN ? j.fileN : j.files + 1}/${j.fileN}　${fmtSize(j.done)} / ${fmtSize(j.total)}　${fmtSpeed(j.speed)}` : (j.msg || stageText);
  return `<div class="impprog"><div class="t">${esc(line)}</div><div class="bar wide"><i style="width:${pct}%"></i></div>
    ${j.file && j.stage === "download" ? `<div class="small muted fn">${esc(j.file)}</div>` : ""}
    ${j.slow ? `<div class="small warnline">百度对非超级会员账号限速，大文件会很慢；开通百度网盘超级会员后会快很多。</div>` : ""}
    ${(j.paths || []).length ? `<div class="small muted">只下载所选的 ${j.paths.length} 项</div>` : ""}
    ${j.project ? `<div class="small muted">下载完会导入到 ${esc(rootLabel(j.project))}</div>` : ""}
    <div class="btnrow"><button class="btn small ghost" data-d="pancancel">取消下载</button></div></div>`;
}
function panDLSec(a) {
  if (!a.user.shareUrl || !(a.panOnly || a.fromPan)) return "";
  const b = S.data.baidu || {}, j = panJobFor(a);
  const busy = panJobBusy(a), sel = S.panSel.size > 0 && panPickable(a);
  // a folder downloaded from the netdisk: more of the share, once parts of it are picked
  if (!a.panOnly && !sel && !(j && j.stage !== "done")) return "";
  let body = "";
  if (!b.loggedIn) {
    body += `<div class="news"><div class="t">先登录百度网盘，就能在软件里直接下载。</div>
      <div class="d">登录信息加密保存在这台电脑上，不用每次登录。</div>
      <div class="btnrow"><button class="btn small primary" data-d="bdlogin">登录百度网盘</button></div></div>`;
  } else body += bdLine();
  if (j && j.stage !== "login") body += panJobHTML(j);
  const where = a.panOnly ? "下载到 " + esc(S.data.dlDir || "素材文件夹") : "下载到这个素材的文件夹";
  if (!busy && sel) {
    body += `<div class="selline">${esc(panSelLine(a))}<button class="linkbtn" data-d="panselclear">清除选择</button></div>
      <div class="impform"><button class="btn${b.loggedIn ? " primary" : ""}" data-d="pandlsel">${ICON.download}<span>下载所选</span></button>${a.panOnly ? `<button class="btn ghost" data-d="pandl">下载全部</button>` : ""}</div>
      <div class="impform">${projSelect("pan_proj")}<button class="btn" data-d="pandlselimp">${ICON.cube}<span>下载所选并导入</span></button></div>
      <div class="hint2">只转存、下载勾选的部分，文件夹结构不变；${where}，压缩包自动解压。速度取决于百度网盘会员等级，普通账号会被百度限速。</div>`;
  } else if (!busy && a.panOnly) {
    body += `<div class="impform"><button class="btn${b.loggedIn ? " primary" : ""}" data-d="pandl">${ICON.download}<span>下载到素材库</span></button></div>
      <div class="impform">${projSelect("pan_proj")}<button class="btn" data-d="pandlimp">${ICON.cube}<span>下载并导入</span></button></div>
      <div class="hint2">先存到你网盘的「MioVRCA」文件夹，再${where}，压缩包自动解压。${panPickable(a) ? "只要其中一部分的话，在下面的「网盘内容」里勾选。" : ""}速度取决于百度网盘会员等级，普通账号会被百度限速。</div>`;
  }
  return `<div class="sec imp" id="pansec"><h4>${a.panOnly ? "下载到本地" : "从网盘下载"}</h4>${body}</div>`;
}
// the folder card of a finished netdisk download keeps its note until 「知道了」
function panDoneSec(a) {
  const j = !a.panOnly && a.fromPan && panJobFor(a);
  return j && j.stage === "done" && !(S.panSel.size && panPickable(a)) ? `<div class="sec imp"><h4>网盘下载</h4>${panJobHTML(j)}</div>` : "";
}
// progress between full refreshes: the bar in place, a new stage redraws the drawer
function refreshJobs(imp, pans) {
  const d = S.data, oldImp = d.importJob || null, oldPans = d.panJobs || [];
  d.importJob = imp || null; d.panJobs = pans || [];
  renderNPLive();
  const stages = (i, ps) => JSON.stringify([i && [i.key, i.stage], ps.map(j => [j.key, j.stage])]);
  if (stages(oldImp, oldPans) !== stages(d.importJob, d.panJobs)) return load();
  document.querySelectorAll("[data-panitem]").forEach(el => { const h = panBar(el.dataset.panitem); if (h) el.outerHTML = h; else el.remove(); });
  const a = S.openKey && findAsset(S.openKey); if (!a) return;
  const ij = impJobFor(a), pj = panJobFor(a);
  const ip = $("#impsec .impprog"); if (ip && ij && ij.stage !== "choose") ip.outerHTML = impProgress(ij);
  const pp = $("#pansec .impprog"); if (pp && pj) pp.outerHTML = panJobHTML(pj);
}
// paths: only these parts of the card's file list; importTo: a project (true: the one picked in the drawer)
async function startPanDL(a, importTo, paths) {
  if (importTo === true) { importTo = chosenProject("pan_proj"); if (!importTo) return; setProject(importTo); }
  const r = await api("/api/pan/download", { key: a.fromPan || a.key, importTo: importTo || "", paths: paths || [] });
  if (!r.ok) { toast(r.err || "没能开始下载", 4000); return; }
  if (paths && paths.length) S.panSel = new Set();
  if (r.login) { toast("先登录百度网盘，登录后会自动开始下载", 4000); baiduLogin(false); return; }
  toast(paths && paths.length ? `开始下载所选的 ${paths.length} 项` : "开始下载"); await load(); poll(true);
}
async function baiduLogin(switching) {
  if (switching) { await api("/api/baidu/logout", {}); await load(); }
  if (!S.data.paneMode) { toast("这台电脑没有内置浏览器（WebView2 / Edge），没法在软件里登录百度网盘", 5000); return; }
  S.web.forLogin = true;
  openWeb(S.data.baiduLogin, "pan");
}
// ---------- Gumroad: log in inside the program, then the purchases are read ----------
function gumLogin() {
  if (!S.data.paneMode) { toast("这台电脑没有内置浏览器（WebView2 / Edge），没法在软件里登录 Gumroad", 5000); return; }
  S.web.forLogin = true;
  toast("在下面的页面里登录 Gumroad，登录后会自动读取已购", 5000);
  openWeb(S.data.gumroad.loginUrl, "gumroad");
}
function gumLoggedIn() {
  const g = S.data.gumroad || {};
  toast("Gumroad 已登录" + (g.name ? "：" + g.name : "") + "，正在读取已购", 4000);
  if (S.web.forLogin && S.view === "lib" && S.web.open.lib) closeWeb();
}
async function gumSync() {
  if (!(S.data.gumroad || {}).loggedIn) return gumLogin();
  const r = await api("/api/gumroad/sync", {});
  if (r.login) return gumLogin();
  toast(r.ok ? "开始读取 Gumroad 已购" : "正在同步"); poll(true);
}
function baiduLoggedIn() {
  const b = S.data.baidu || {};
  toast("百度网盘已登录" + (b.name ? "：" + b.name : ""), 3500);
  if (S.web.forLogin && S.view === "lib" && S.web.open.lib) closeWeb();
}

// ---------- Booth downloads ----------
function jobFor(dl) { return (S.data.downloads || []).find(j => j.id === dl); }
function jobText(j) {
  if (!j) return "";
  if (j.status === "queued") return "排队中";
  if (j.status === "running") return j.total ? `${Math.floor(j.done / j.total * 100)}%` : "下载中";
  if (j.status === "unpacking") return "解压中";
  if (j.status === "login") return "需要登录";
  if (j.status === "failed") return j.err || "失败";
  return "";
}
function dlRows(p) {
  const files = p.files || [], dls = p.dls || [];
  if (!dls.length) return `<div class="muted small">${p.source === "gumroad" ? esc(p.note || "这件商品没有可以直接下载的文件（可能只能在线看，或文件在别的网站）") + "。点「下载页」到 Gumroad 上看" : "没有读到可下载的文件，重新同步一次 Booth 已购试试。"}</div>`;
  return `<div class="dlrows">${dls.map((id, i) => {
    const j = jobFor(id), got = (j && j.status === "done" && j.path) || (p.got || [])[i];
    const busy = j && ["queued", "running", "unpacking"].includes(j.status);
    const pct = j && j.status === "running" && j.total ? Math.floor(j.done / j.total * 100) : 0;
    return `<div class="dlrow${busy ? " busy" : ""}" data-dlrow="${esc(id)}">
      <span class="fn" title="${esc(files[i] || id)}">${esc(files[i] || "文件 " + (i + 1))}</span>
      <span class="st${j && j.status === "failed" ? " err" : ""}">${busy || (j && j.status !== "done") ? esc(jobText(j)) : got ? "已下载" : ""}</span>
      ${got && !busy ? `<button class="act" data-gopen="${esc(got)}" title="打开文件夹">${ICON.folder}</button>` : ""}
      <button class="btn small" data-dl="${esc(id)}"${busy ? " disabled" : ""}>${got ? "重新下载" : "下载"}</button>
      ${busy ? `<i class="bar" style="width:${pct}%"></i>` : ""}
    </div>`;
  }).join("")}</div>`;
}
// a thin bar on a card while its files download
function dlBar(item) {
  const js = (S.data.downloads || []).filter(j => j.item === item && ["queued", "running", "unpacking"].includes(j.status));
  if (!js.length) return "";
  const r = js.find(j => j.status === "running");
  const pct = r && r.total ? Math.floor(r.done / r.total * 100) : 0;
  return `<div class="cdl" data-dlitem="${esc(item)}"><i style="width:${pct}%"></i><span>${r ? (r.status === "unpacking" ? "解压中" : pct + "%") : "排队中"}</span></div>`;
}
async function startDownload(item, ids) {
  S.dlAsked = Date.now();
  const r = await api("/api/booth/download", { item, ids: ids || [] });
  if (!r.ok) { toast(r.err || "没能开始下载", 3500); return; }
  toast(r.n ? `开始下载 ${r.n} 个文件` : "已经在下载了");
  await load(); poll(true);
}
// progress between full refreshes
function refreshDL(jobs) {
  const known = new Set((S.data.downloads || []).map(j => j.id));
  const added = jobs.filter(j => !known.has(j.id) && j.status !== "done");
  if (added.length && Date.now() - (S.dlAsked || 0) > 4000) toast(`已加入下载：${added.map(j => j.name || j.id).join("、")}`, 3500);
  const before = JSON.stringify((S.data.downloads || []).map(j => [j.id, j.status]));
  S.data.downloads = jobs;
  if (before !== JSON.stringify(jobs.map(j => [j.id, j.status]))) return load();
  document.querySelectorAll("[data-dlitem]").forEach(el => {
    const item = el.dataset.dlitem, html = dlBar(item);
    if (html) el.outerHTML = html; else el.remove();
  });
  const sec = $("#dlsec .dlrows");
  if (sec && S.openKey) { const a = findAsset(S.openKey); if (a && a.purchase) sec.outerHTML = dlRows(a.purchase); }
  if (S.shopOpen && $("#shopfiles")) renderShopDrawer();
}

function renderDrawer(key, keepScroll) {
  let a = findAsset(key);
  if (!a && key.startsWith("purchase:")) { // just downloaded: follow it to its library card
    a = (S.data.assets || []).find(x => !x.virtual && x.boothId === key.slice(9) && x.purchase);
    if (a) { key = a.key; keepScroll = false; }
  }
  if (!a && key.startsWith("pan:")) { // downloaded from the netdisk: its folder's card
    a = (S.data.assets || []).find(x => x.fromPan === key);
    if (a) { key = a.key; keepScroll = false; }
  }
  if (!a && S.drawerPan && S.drawerPan.key === key) { // more downloaded into it, and it is scanned as several cards now
    a = (S.data.assets || []).find(x => x.fromPan === S.drawerPan.pan);
    if (a) { key = a.key; keepScroll = false; S.panSelKey = key; }
  }
  S.drawerPan = a && a.fromPan ? { key: a.key, pan: a.fromPan } : null;
  if (!a) { closeDrawer(); return; }
  // the file list's picks and open folders stay while the card is left for the login page
  if (S.panSelKey !== key) { S.panSel = new Set(); S.panOpen = new Set(); S.panClosed = new Set(); S.panSelKey = key; }
  if (S.openKey !== key) { S.dirty = new Set(); S.coverPick = null; S.stylePick = null; }
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
      <div class="sub">${shopOf(a) ? esc(shopOf(a)) + "　" : ""}${b && b.price ? esc(b.price) + "　" : ""}${a.virtual ? esc(orderLine(a.purchase)) + "，未下载" : a.panOnly ? esc(panLine(a)) : `${fmtSize(a.size)}　${a.files} 个文件${a.packages ? `　${a.packages} 个 unitypackage` : ""}`}</div>
      ${b && b.err && !a.virtual ? `<div class="small err">Booth：${esc(b.err)}</div>` : ""}
      <div class="rowbtn">
        ${a.virtual ? `<button class="btn primary" data-d="dlall">${ICON.download}<span style="margin-left:6px">下载</span></button>`
          : a.panOnly ? (a.user.shareUrl && !panJobBusy(a) ? `<button class="btn primary" data-d="${S.panSel.size && panPickable(a) ? "pandlsel" : "pandl"}">${ICON.download}<span style="margin-left:6px">${S.panSel.size && panPickable(a) ? "下载所选" : "下载"}</span></button><button class="btn" data-d="pan">打开网盘分享</button>`
            : `<button class="btn primary" data-d="pan">${ICON.cloud}<span style="margin-left:6px">打开网盘分享</span></button>`)
          : `<button class="btn primary" data-d="open">${ICON.folder}<span style="margin-left:6px">打开文件夹</span></button>`}
        ${hasShare(a) && !a.panOnly ? `<button class="btn" data-d="pan">打开网盘</button>` : ""}
        ${a.purchase && !a.virtual ? `<button class="btn" data-d="buypage">购买页</button>` : ""}
        <button class="btn" data-d="booth">${isGum(a) ? "Gumroad 商品页" : a.boothId ? "Booth 商品页" : "在 Booth 搜索"}</button>
      </div>
    </div>
  </div>
  <div class="dbody">
    ${newsSec(a)}
    ${importSec(a)}${dlImportSec(a)}${panDLSec(a)}${panDoneSec(a)}
    ${variantSec(a)}
    ${a.panOnly ? "" : a.virtual ? `<div class="sec"><h4>保存位置</h4><div class="muted small">还没下载。会下载到 ${esc(S.data.dlDir || "素材文件夹")}。</div></div>`
    : `<div class="sec"><h4>保存位置</h4>
      ${(a.locations || []).map((l, i) => `<div class="loc"><span class="k">${{ dir: "文件夹", zip: "zip", rar: "rar", "7z": "7z", unitypackage: "unitypackage", file: "文件" }[l.kind] || l.kind}</span>
        <span class="p">${esc(l.path)}</span><span class="s">${fmtSize(l.size)}</span>
        <button class="act" data-loc="${i}" title="打开">${ICON.folder}</button><button class="act" data-copy="${i}" title="复制路径">${ICON.copy}</button></div>`).join("")}
    </div>`}
    ${psdSec(a)}
    ${a.purchase ? purchaseSec(a) : ""}
    ${panSec(a)}
    ${boothSec(a)}
    <div class="sec"><h4>整理</h4>
      <div class="field"><label>显示名称</label><input id="f_name" value="${esc(u.name || "")}" placeholder="${esc(a.autoName)}"></div>
      <div class="field"><label>中文名</label><input id="f_nameZh" value="${esc(u.nameZh || "")}" placeholder="${esc(a.nameZh || "自动翻译")}"></div>
      <div class="field"><label>分类</label><select id="f_cat"><option value="">自动（${esc(a.autoCategory)}）</option>${d.categories.map(c => `<option${c === u.category ? " selected" : ""}>${esc(c)}</option>`).join("")}</select></div>
      <div class="field"><label>适配素体</label><input id="f_bases" value="${esc((u.bases || a.bases || []).join(", "))}" placeholder="Plum, Chocolat"></div>
      ${u.bases ? `<div class="field"><span></span><span class="hint">自动识别：${esc((a.autoBases || []).join(", ") || "无")}</span></div>` : ""}
      ${styleField(a)}
      <div class="field"><label>标签</label><input id="f_tags" value="${esc((u.tags || []).join(", "))}" placeholder="用逗号分隔"></div>
      ${a.virtual ? `<input id="f_booth" type="hidden" value="">` : `<div class="field"><label>Booth 链接</label><input id="f_booth" value="${esc(u.boothUrl || "")}" placeholder="${isGum(a) ? "这是 Gumroad 的商品；填 Booth 链接会改成关联 Booth" : a.boothId ? "已识别 #" + esc(a.boothId) : "https://booth.pm/ja/items/..."}"></div>`}
      <div class="field"><label>备注</label><textarea id="f_notes">${esc(u.notes || "")}</textarea></div>
      ${(a.localCovers || []).length > 1 || u.cover ? `<div class="field"><label>封面</label><div class="covers">${(a.localCovers || []).map(c => `<img src="/thumb?w=160&p=${encodeURIComponent(c)}" data-cover="${esc(c)}" class="${u.cover === c ? "on" : ""}" title="${esc(c)}">`).join("")}</div></div>` : ""}
    </div>
    ${!isLocal(a) ? "" : `<div class="sec"><h4>工程使用</h4>
      ${(a.usage || []).length ? `<div class="uses">${a.usage.map(x => `<div class="use"><span class="st ${x.status}">${x.status === "used" ? "在用" : "部分"}</span><span>${esc(x.project)}</span><span class="pct">${x.matched}/${x.total} 个资源（${Math.round(x.ratio * 100)}%）</span></div>`).join("")}</div>`
        : `<div class="muted small">${a.guidCount ? "没有工程在用" : (a.packages ? "还没分析" : "没有 unitypackage，无法判断")}</div>`}
    </div>`}
    ${a.panOnly ? `<div class="sec"><h4>管理</h4><div style="display:flex;gap:8px;flex-wrap:wrap"><button class="btn" data-d="hide">${u.hidden ? "取消隐藏" : "隐藏"}</button>${a.panParent ? "" : `<button class="btn" data-d="pandelete">从素材库移除</button>`}</div></div>`
    : a.virtual ? `<div class="sec"><h4>管理</h4><button class="btn" data-d="hide">${u.hidden ? "取消隐藏" : "隐藏"}</button></div>` : `<div class="sec"><h4>识别调整</h4>
      <div class="muted small" style="margin-bottom:8px">原名：${esc(a.rawName)}${(a.hints || []).length ? `　分类文件夹：${esc(a.hints.join(" / "))}` : ""}</div>
      <div style="display:flex;gap:8px;flex-wrap:wrap">
        ${a.hasDir ? `<button class="btn" data-d="split">拆成多个素材</button>` : ""}
        <button class="btn" data-d="hide">${u.hidden ? "取消隐藏" : "隐藏"}</button>
        <button class="btn" data-d="ignore" title="扫描时跳过">不再收录</button>
        ${a.group || u.noGroup ? `<button class="btn" data-d="nogroup">${u.noGroup ? "允许合并同款" : "不和同款合并"}</button>` : ""}
        ${a.boothId && !isGum(a) ? `<button class="btn" data-d="rebooth">重新读取 Booth</button>` : ""}
      </div>
    </div>`}
  </div>
  <div class="dfoot"><button class="btn ghost" data-d="close">关闭</button><span class="spacer"></span><button class="btn primary" data-d="save">保存</button></div>`;
  for (const id in kept) { const el = document.getElementById(id); if (el) el.value = kept[id]; }
  if (S.coverPick) dr.querySelectorAll(".covers img").forEach(i => i.classList.toggle("on", i.dataset.cover === S.coverPick));
  if (focusId) { const el = document.getElementById(focusId); if (el) { el.focus(); try { if (selS !== null) el.setSelectionRange(selS, selE); } catch (e) {} } }
  dr.querySelectorAll("input.psel.part").forEach(x => { x.indeterminate = true; });
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
  S.openKey = null; S.shopOpen = null; $("#drawer").dataset.kind = ""; $("#drawer").classList.remove("on"); $("#drawer").setAttribute("aria-hidden", "true");
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
  if (S.stylePick) { u.styles = [...S.stylePick]; u.stylesSet = true; }
  return u;
}
async function saveUser(a, u, msg) {
  await api("/api/user", { key: a.key, user: u });
  S.dirty = new Set(); S.coverPick = null; S.stylePick = null; // everything typed so far has just been saved
  toast(msg || "已保存");
  await load();
}

// saving one download of a product: its category and style tags go to the other downloads too
async function saveGroup(a, u, msg) {
  const others = groupAll(a).filter(m => m.key !== a.key);
  const catChanged = (u.category || "") !== (a.user.category || "");
  const stChanged = JSON.stringify(u.styles || null) !== JSON.stringify(a.user.styles || null) || !!u.stylesSet !== !!a.user.stylesSet;
  await api("/api/user", { key: a.key, user: u });
  let n = 0;
  if (catChanged || stChanged) for (const m of others) {
    const mu = Object.assign({}, m.user);
    if (catChanged) mu.category = u.category;
    if (stChanged) { mu.styles = u.styles; mu.stylesSet = u.stylesSet; }
    await api("/api/user", { key: m.key, user: mu }); n++;
  }
  S.dirty = new Set(); S.coverPick = null; S.stylePick = null;
  toast((msg || "已保存") + (n ? `，同款 ${n} 个一起更新` : ""));
  await load();
}
async function addStyle(name) {
  name = name.replace(/[=|\n]/g, "").trim();
  if (!name) return;
  const table = (S.data.settings.styles || []).slice();
  if (!styleList().includes(name)) { table.push(name + "="); await api("/api/styles", { styles: table }); }
  const a = findAsset(S.openKey); if (!a) return;
  S.stylePick = new Set(S.stylePick || a.styles || []); S.stylePick.add(name);
  await load(); toast("已添加「" + name + "」");
}

// ---------- updates (GitHub releases) ----------
function renderUpdateBtn() {
  const d = S.data, b = $("#btnUpdate"); if (!b) return;
  const u = d.update, show = d.updateNewer && u && u.version !== d.settings.skipVersion;
  b.hidden = !show;
  if (show) b.textContent = "新版本 " + u.version;
  const t = (d.tasks || []).find(x => x.name === "update");
  if (t && t.running && $("#modal").dataset.kind === "update") renderUpdateModal();
}
// A newer version was found: the update window opens by itself, once per version each time the program runs.
// Not over something the player has open; the next tick tries again.
function announceUpdate() {
  const d = S.data; if (!d) return;
  const u = d.update;
  if (!d.updateNewer || !u || u.version === d.settings.skipVersion || S.updShown === u.version) return;
  if (S.setup || Date.now() - BOOT_AT < 2500) return; // the first-run screen and "what is new" come first
  if ($("#modal").classList.contains("on") || S.openKey || S.shopOpen) return;
  const ae = document.activeElement;
  if (ae && /^(INPUT|TEXTAREA|SELECT)$/.test(ae.tagName)) return; // not in the middle of typing
  S.updShown = u.version;
  const m = $("#modal");
  m.dataset.kind = "update"; S.upd = { info: u, newer: true };
  renderUpdateModal(); m.classList.add("on"); $("#scrim").classList.add("on");
}
const BOOT_AT = Date.now();
function fmtDate(iso) { const t = Date.parse(iso || ""); return isNaN(t) ? "" : fmtTime(t / 1000).replace(/ .*/, ""); }
async function openUpdate(force) {
  const m = $("#modal");
  m.dataset.kind = "update"; S.upd = { checking: true };
  renderUpdateModal(); m.classList.add("on"); $("#scrim").classList.add("on");
  try {
    const r = await api("/api/update/check", { force: !!force });
    S.upd = r.ok ? { info: r.update, newer: r.newer } : { err: r.err };
  } catch (e) { S.upd = { err: "检查失败" }; }
  await load();
  if ($("#modal").dataset.kind === "update") renderUpdateModal();
}
function renderUpdateModal() {
  const m = $("#modal"), d = S.data, st = S.upd || {};
  const t = (d.tasks || []).find(x => x.name === "update") || {};
  const info = st.info || d.update;
  let top = "", foot = `<button class="btn ghost" data-m="uppage">发布页</button><span class="spacer"></span><button class="btn primary" data-m="close">关闭</button>`;
  if (t.running || S.updRestart) {
    const pct = t.total ? Math.min(100, Math.round(t.done / t.total * 100)) : 0;
    top = `<p class="lead">${esc(t.msg || (S.updRestart ? "正在重启…" : "正在准备…"))}</p><div class="upbar"><i style="width:${S.updRestart ? 100 : pct}%"></i></div>
      <p class="muted small">完成后自动重启，数据不受影响。</p>`;
    foot = "";
  } else if (st.checking) {
    top = `<p class="lead">正在检查…</p>`;
  } else if (st.err) {
    top = `<p class="lead err">${esc(st.err)}</p><p class="muted small">连不上的话可以在设置里填代理，或者去发布页下载。</p>`;
  } else if (info && versionNewer(info.version, d.version)) {
    const how = info.zip ? `下载后自动替换并重启，约 ${fmtSize(info.zip.size)}` : info.setup ? "下载安装程序后打开" : "没有安装包，请去发布页下载";
    top = `<div class="uphead"><b>新版本 ${esc(info.version)}</b>${info.published ? `<span class="muted small">${esc(fmtDate(info.published))}</span>` : ""}<span class="muted small">当前 ${esc(d.version)}</span></div>
      ${info.notes ? `<div class="upnotes">${mdLite(info.notes)}</div>` : `<p class="muted small">这一版改了什么，点「发布页」可以看到。</p>`}
      <p class="muted small">${how}${t.msg && t.msg.startsWith("更新失败") ? `<br><span class="err">${esc(t.msg)}</span>` : ""}</p>`;
    foot = `<button class="btn ghost" data-m="upskip">跳过这个版本</button><button class="btn ghost" data-m="uppage">发布页</button><span class="spacer"></span>
      <button class="btn ghost" data-m="close">以后再说</button>
      ${info.zip || info.setup ? `<button class="btn primary" data-m="upgo">${info.zip ? "更新并重启" : "下载安装程序"}</button>` : ""}`;
  } else {
    top = `<p class="lead">已是最新版本 ${esc(d.version)}</p>`;
  }
  const busy = t.running || S.updRestart;
  m.innerHTML = `<div class="mhead">更新</div><div class="mbody">${top}${busy ? "" : `<h4 class="boardh">更新公告</h4>${noticeBoard()}`}</div>${foot ? `<div class="mfoot">${foot}</div>` : ""}`;
}
// the history shipped with the program (CHANGELOG.md)
function noticeBoard() {
  return `<div class="board">${(S.data.changelog || []).map(e => `<div class="note"><div class="nv">${esc(e.version)}${e.version === S.data.version ? `<span class="cur">当前版本</span>` : ""}</div>
    <ul>${(e.items || []).map(i => `<li>${esc(i)}</li>`).join("")}</ul></div>`).join("")}</div>`;
}
// release text written on GitHub: headings, "- " lists, **bold**, `code`; links show as text
function mdLite(src) {
  const inline = x => esc(x).replace(/\*\*(.+?)\*\*/g, "<b>$1</b>").replace(/`([^`]+)`/g, "<code>$1</code>").replace(/\[([^\]]+)\]\([^)\s]+\)/g, "$1");
  let out = "", list = false;
  for (const raw of String(src || "").split(/\r?\n/)) {
    const l = raw.trim(), li = /^[-*+]\s+(.*)$/.exec(l);
    if (li) { if (!list) { out += "<ul>"; list = true; } out += `<li>${inline(li[1])}</li>`; continue; }
    if (list) { out += "</ul>"; list = false; }
    if (!l) continue;
    const h = /^#{1,6}\s+(.*)$/.exec(l);
    out += h ? `<h5>${inline(h[1])}</h5>` : `<p>${inline(l)}</p>`;
  }
  return out + (list ? "</ul>" : "");
}
// once after an update: what changed since the version that was used before
function openWhatsNew(version, list) {
  const m = $("#modal");
  m.dataset.kind = "whatsnew";
  const many = list.length > 1;
  const body = list.map(e => `${many ? `<div class="wnv">${esc(e.version)}</div>` : ""}<ul class="wn">${(e.items || []).map(i => `<li>${esc(i)}</li>`).join("")}</ul>`).join("");
  m.innerHTML = `<div class="mhead">已更新到 ${esc(version)}</div><div class="mbody">${body}</div>
    <div class="mfoot"><button class="btn ghost" data-m="allnotes">全部更新公告</button><span class="spacer"></span><button class="btn primary" data-m="close">知道了</button></div>`;
  m.classList.add("on"); $("#scrim").classList.add("on");
  api("/api/whatsnew/seen", {}).catch(() => {});
}
function versionNewer(a, b) {
  const p = s => ((s || "").match(/\d+(?:\.\d+)+/) || [""])[0].split(".").map(Number);
  const x = p(a), y = p(b);
  for (let i = 0; i < Math.max(x.length, y.length); i++) { const u = x[i] || 0, v = y[i] || 0; if (u !== v) return u > v; }
  return false;
}

// ---------- settings ----------
function openSettings(focusId) {
  const s = S.data.settings; const ov = S.data.overrides || {};
  const m = $("#modal");
  const chk = (id, on, label) => `<label class="check opt"><input type="checkbox" id="${id}"${on ? " checked" : ""}> ${label}</label>`;
  m.innerHTML = `<div class="mhead">设置</div><div class="mbody">
    <div class="row"><label>素材文件夹</label><textarea id="s_roots">${esc((s.roots || []).join("\n"))}</textarea>
      <button class="btn small addbtn" data-pickto="s_roots">${ICON.folder}<span>添加…</span></button></div>
    <div class="row"><label>Unity 工程文件夹</label><textarea id="s_proj">${esc((s.projectRoots || []).join("\n"))}</textarea>
      <button class="btn small addbtn" data-pickto="s_proj">${ICON.folder}<span>添加…</span></button></div>
    <div class="row"><label>自动</label>
      ${chk("s_rescan", !s.manualRescan, "打开时重新扫描")}
      ${chk("s_watch", !s.noWatch, "有新素材时自动整理")}
      ${chk("s_auto", s.autoBooth, "读取 Booth 封面和商品名")}
      ${chk("s_match", !s.noAutoMatch, "没有封面时按名称匹配 Booth 商品")}
      ${chk("s_sync", !s.noSync, "检查网盘分享和 Booth 商品页的变化")}
      ${chk("s_upd", !s.noUpdateCheck, "检查新版本")}</div>
    <div class="row"><label>显示</label>
      ${chk("s_zh", !s.hideZh, "显示中文名")}
      <select id="s_win" class="optsel"><option value="">在窗口中打开</option><option value="tab"${s.windowMode === "tab" ? " selected" : ""}>在浏览器标签页中打开</option></select></div>
    <div class="row"><label>下载位置（Booth 已购、网盘分享）</label>
      <div class="inline"><input id="s_dldir" value="${esc(s.downloadDir || "")}" placeholder="${esc("自动：" + (S.data.dlDir || "剩余空间最大的非系统盘"))}"><button class="btn small" data-pickone="s_dldir">${ICON.folder}<span>选择…</span></button></div>
      ${chk("s_extract", !s.noExtract, "下载后自动解压")}
      ${chk("s_keepzip", s.keepZip, "解压后保留压缩包")}</div>
    <div class="row"><label>代理</label><input id="s_proxy" value="${esc(s.proxy || "")}" placeholder="留空使用系统代理，例如 127.0.0.1:7890"></div>
    <div class="row"><label>素体识别表（显示名=别名1|别名2）</label><textarea id="s_bases" style="min-height:120px">${esc((s.bases || []).join("\n"))}</textarea></div>
    <div class="row"><label>风格标签（标签名=关键词1|关键词2）</label><textarea id="s_styles" style="min-height:140px">${esc((s.styles || []).join("\n"))}</textarea></div>
    ${Object.keys(ov).length ? `<div class="row"><label>手动调整</label>${Object.entries(ov).map(([p, v]) => `<div class="ov"><span>${esc(p)}</span><span class="muted" style="flex:none">${{ split: "拆成多个", ignore: "不收录", asset: "合并为一个" }[v] || v}</span><button class="btn" data-ov="${esc(p)}">撤销</button></div>`).join("")}</div>` : ""}
    <div class="row" id="s_baidu"><label>百度网盘${(S.data.baidu || {}).loggedIn ? `<span class="muted small">　已登录：${esc(S.data.baidu.name || "")}${VIP[S.data.baidu.vip] ? "（" + VIP[S.data.baidu.vip] + "）" : ""}</span>` : `<span class="muted small">　未登录</span>`}</label>
      <div class="btnrow" style="margin-top:0">${(S.data.baidu || {}).loggedIn ? `<button class="btn small" data-m="bdswitch">更换账号</button><button class="btn small" data-m="bdlogout">退出登录</button>` : `<button class="btn small" data-m="bdlogin">登录</button>`}</div>
      <div class="muted small" style="margin-top:6px">登录后可以在软件里直接下载网盘分享。登录信息加密保存在这台电脑上。下载速度取决于账号的会员等级，普通账号会被百度限速。</div></div>
    <div class="row"><label>Booth 已购${S.data.purchaseSync ? `<span class="muted small">　已同步 ${S.data.purchaseCount} 件（${esc(fmtTime(S.data.purchaseSync))}）</span>` : ""}</label>
      <div class="btnrow" style="margin-top:0"><button class="btn small" data-m="sync">同步</button><button class="btn small" data-m="forget">退出登录</button>${S.data.purchaseSync ? `<button class="btn small" data-m="clearp">清空记录</button>` : ""}</div></div>
    <div class="row"><label>Gumroad 已购<span class="muted small">　${(S.data.gumroad || {}).loggedIn ? "已登录" + ((S.data.gumroad || {}).name ? "：" + esc(S.data.gumroad.name) : "") : "未登录"}${(S.data.gumroad || {}).count ? `，已读到 ${S.data.gumroad.count} 件` : ""}</span></label>
      <div class="btnrow" style="margin-top:0"><button class="btn small" data-m="gumsync">${(S.data.gumroad || {}).loggedIn ? "同步" : "登录"}</button>${(S.data.gumroad || {}).loggedIn ? `<button class="btn small" data-m="gumlogout">退出登录</button>` : ""}${(S.data.gumroad || {}).count ? `<button class="btn small" data-m="gumclear">清空记录</button>` : ""}</div>
      <div class="muted small" style="margin-top:6px">在软件里登录 Gumroad 后读取已购，没下载的可以直接下载。登录信息加密保存在这台电脑上，只发给 gumroad.com。</div></div>
    <div class="row"><label>流水线的 AI<span class="muted small">　${AI.cfg && AI.cfg.ready ? esc(aiProvLabel(AI.cfg)) : "还没有设置 AI 服务"}</span></label>
      <div class="btnrow" style="margin-top:0"><button class="btn small" data-m="aicfg">设置 AI 服务</button></div>
      <div class="muted small" style="margin-top:6px">「流水线」页把导入的素材装到模型上、生成菜单；设置了 AI 服务后还能让 AI 接着改模。不设置也能跑（按网格名字分组）。</div></div>
    <div class="row"><label>其他</label>
      <div class="btnrow" style="margin-top:0"><button class="btn small" data-m="checkupd">更新公告</button><button class="btn small" data-m="feedback">反馈和建议</button>${S.data.canShortcut ? `<button class="btn small" data-m="shortcut">创建桌面快捷方式</button>` : ""}</div></div>
    <div class="row muted small">数据位置：${esc(S.data.dataDir)}</div>
  </div><div class="mfoot"><button class="btn ghost" data-m="close">取消</button><button class="btn primary" data-m="save">保存</button></div>`;
  m.classList.add("on"); $("#scrim").classList.add("on");
  if (focusId && $("#" + focusId)) { $("#" + focusId).scrollIntoView({ block: "center" }); $("#" + focusId).focus(); }
}
function closeModal(force) {
  if (S.setup && !force) return; // the first-run screen has to be finished
  if ($("#modal").dataset.kind === "feedback") fbKeep();
  if ($("#modal").dataset.kind === "np") npKeep();
  if (["update", "feedback", "whatsnew", "aicfg", "np"].includes($("#modal").dataset.kind)) $("#modal").dataset.kind = "";
  $("#modal").classList.remove("wide");
  $("#modal").classList.remove("on"); if (!S.openKey) $("#scrim").classList.remove("on");
}

// ---------- feedback ----------
const FB_KINDS = ["问题反馈", "功能建议", "其他"];
function openFeedback() {
  if (!S.fb || S.fb.sent) S.fb = { kind: "问题反馈", text: "", contact: "", withInfo: true };
  const m = $("#modal"), f = S.fb;
  f.err = ""; m.dataset.kind = "feedback";
  renderFeedback(); m.classList.add("on"); $("#scrim").classList.add("on");
  $("#fb_text").focus();
  api("/api/feedback/info", {}).then(r => { f.info = r.info; f.mail = r.mail; const pre = $("#fb_infopre"); if (pre) pre.textContent = r.info; }).catch(() => {});
}
function fbKeep() {
  const f = S.fb; if (!f || !$("#fb_text")) return;
  f.text = $("#fb_text").value; f.contact = $("#fb_contact").value; f.withInfo = $("#fb_info").checked;
}
function fbMail() { return (S.fb && S.fb.mail) || "Coko_Iya@163.com"; }
function fbPlain() {
  const f = S.fb;
  return `【${f.kind}】\n${f.text.trim()}${f.contact.trim() ? "\n\n联系方式：" + f.contact.trim() : ""}${f.withInfo && f.info ? "\n\n---\n" + f.info : ""}`;
}
function renderFeedback() {
  const f = S.fb, m = $("#modal");
  let body, foot;
  const fallback = `<div class="btnrow"><button class="btn small" data-m="fbcopy">复制内容</button><button class="btn small" data-m="fbmail">用邮件软件发送</button><button class="btn small" data-m="fbaddr">复制邮箱地址</button></div>`;
  if (f.sent && f.note) {
    body = `<p class="lead">${esc(f.note)}</p>
      <div class="fberr"><p>也可以复制内容，发邮件到 ${esc(fbMail())}。</p>${fallback}</div>`;
    foot = `<span class="spacer"></span><button class="btn primary" data-m="close">好</button>`;
  } else if (f.sent) {
    body = `<p class="lead">已发送，谢谢！</p>`;
    foot = `<span class="spacer"></span><button class="btn primary" data-m="close">好</button>`;
  } else {
    body = `<div class="fbkinds" role="radiogroup">${FB_KINDS.map(k => `<button class="stchip${f.kind === k ? " on" : ""}" role="radio" aria-checked="${f.kind === k}" data-fbkind="${k}">${k}</button>`).join("")}</div>
      <div class="row"><textarea id="fb_text" maxlength="5000" placeholder="${f.kind === "功能建议" ? "想要的功能" : f.kind === "问题反馈" ? "遇到的问题，以及当时的操作" : ""}">${esc(f.text)}</textarea></div>
      <div class="row"><label>联系方式（选填）</label><input id="fb_contact" value="${esc(f.contact)}" placeholder="邮箱 / QQ / Discord"></div>
      <div class="row"><label class="check" style="padding:0"><input type="checkbox" id="fb_info"${f.withInfo ? " checked" : ""}> 附带运行信息</label>
        <details class="fbinfo"><summary>查看内容</summary><pre id="fb_infopre">${esc(f.info || "读取中…")}</pre></details></div>
      ${f.err ? `<div class="fberr"><p><b>${esc(f.err)}</b></p><p>可以复制内容，发邮件到 ${esc(fbMail())}。</p>${fallback}</div>` : ""}`;
    foot = `<span class="muted small">作者邮箱：${esc(fbMail())}</span><span class="spacer"></span><button class="btn ghost" data-m="close">取消</button>
      <button class="btn primary" data-m="fbsend"${f.sending ? " disabled" : ""}>${f.sending ? "正在发送…" : "发送"}</button>`;
  }
  m.innerHTML = `<div class="mhead">反馈和建议</div><div class="mbody">${body}</div><div class="mfoot">${foot}</div>`;
}
async function sendFeedback() {
  fbKeep(); const f = S.fb;
  if (f.text.trim().length < 4) { toast("内容太短"); $("#fb_text").focus(); return; }
  f.sending = true; f.err = ""; renderFeedback();
  let r;
  try { r = await api("/api/feedback", { kind: f.kind, text: f.text, contact: f.contact, withInfo: f.withInfo }); }
  catch (e) { r = { ok: false, err: "发送失败" }; }
  f.sending = false;
  if (r.ok) { f.sent = true; f.note = r.note || ""; }
  else if (r.input) { toast(r.err, 3500); }
  else { f.err = r.err || "发送失败"; if (r.mail) f.mail = r.mail; }
  if ($("#modal").dataset.kind === "feedback") renderFeedback();

}

// ---------- netdisk-only asset ----------
function openPanAdd(prefill) {
  const m = $("#modal");
  m.innerHTML = `<div class="mhead">添加网盘素材</div><div class="mbody">
    <div class="row"><label>分享链接或分享文字</label><textarea id="pa_text" placeholder="链接: https://pan.baidu.com/s/1xxxx 提取码: abcd"></textarea></div>
    <div class="row"><label>提取码</label><input id="pa_pwd" placeholder="可不填"></div>
    <div class="row"><label>名称</label><input id="pa_name" placeholder="可不填"></div>
    <div class="row"><label class="check"><input type="checkbox" id="pa_dl"${S.data.paneMode ? " checked" : " disabled"}> 添加后直接下载到素材库${(S.data.baidu || {}).loggedIn ? "" : "（第一次需要登录百度网盘）"}</label></div>
  </div><div class="mfoot"><button class="btn ghost" data-m="close">取消</button><button class="btn primary" data-m="panadd">添加</button></div>`;
  m.classList.add("on"); $("#scrim").classList.add("on");
  $("#pa_text").focus();
  $("#pa_text").addEventListener("input", e => { const r = parseShareText(e.target.value); if (r.pwd && !$("#pa_pwd").value) $("#pa_pwd").value = r.pwd; });
  if (prefill) { $("#pa_text").value = prefill; $("#pa_text").dispatchEvent(new Event("input")); }
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
    : `<div class="muted small cand-empty">没有找到</div>`);
  const adder = (kind, label) => `<div class="addrow"><button class="btn small" data-spick="${kind}">${ICON.folder}<span>${label}</span></button>
    <input data-spath="${kind}" placeholder="或粘贴路径后回车"></div>`;
  m.innerHTML = `<div class="mhead">选择文件夹</div><div class="mbody">
    <div class="row"><label>素材文件夹</label>${list("roots")}${adder("roots", "添加…")}</div>
    <div class="row"><label>Unity 工程（可不选）</label>${list("projects")}${adder("projects", "添加…")}</div>
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
  if (!roots.length) { toast("至少选一个素材文件夹"); return; }
  const s = Object.assign({}, S.data.settings, { roots, projectRoots: projects, setupDone: true });
  await api("/api/settings", { settings: s });
  S.setup = null; $("#modal").dataset.kind = ""; closeModal(true);
  toast("开始扫描"); await load(); poll(true);
}

// ---------- 流水线的 AI 服务 ----------
// cfg: the AI service as saved; proj: the project the panel is open for; sess: what the server says about it
const AI = { cfg: null, proj: null, sess: null, timer: 0, back: null, cfgForm: null, shown: "" };
async function aiLoadCfg() { const r = await api("/api/ai/config", {}); if (r.ok) AI.cfg = r.ai; return AI.cfg; }
function aiProvLabel(c) {
  const p = (c.providers || []).find(x => x.id === c.provider);
  return p ? `${p.label} · ${(c.profiles[p.id] || {}).model || "还没选模型"}` : "";
}
// ---------- 流水线 (the fifth tab: the AI assistant as an assembly line) ----------
const KINDS = ["素体", "衣服", "头发", "配饰", "道具", "其他"];
const PIPE = { tool: null, assets: null, pick: {}, kind: {}, extra: [], preset: null, loading: false, hier: "", noAI: false, text: "", err: "" };
function pipeActive() { return S.view === "pipe" && !!AI.proj; }
function pipeProject(path) {
  return (S.projs || []).find(x => x.path === path) || ((S.data || {}).projects || []).find(x => x.path === path);
}
// the project card's 「AI 助手」 and the import result's button land here, on that project
async function openAI(path, folders) {
  const p = pipeProject(path);
  if (!p) { toast("这个工程不在工程列表里"); return; }
  if (folders && folders.length) PIPE.preset = folders.slice();
  pipeSelect(p, true);
  if (S.view !== "pipe") setView("pipe"); else { renderPipeSide(); renderPipeMain(); pipeLoad(); }
}
function pipeKeep() {
  if (S.view !== "pipe") return;
  const h = $("#pipe_hier"), n = $("#pipe_noai"), t = $("#ai_text");
  if (h) PIPE.hier = h.value;
  if (n) PIPE.noAI = n.checked;
  if (t) PIPE.text = t.value;
}
function pipeSelect(p, quiet) {
  if (AI.proj && AI.proj.path === p.path) return;
  pipeKeep();
  AI.proj = p; AI.sess = null; PIPE.assets = null; PIPE.pick = {}; PIPE.kind = {}; PIPE.extra = []; PIPE.err = "";
  try { localStorage.setItem("vrclib.pipeproj", p.path); } catch (e) {}
  if (!quiet) { renderPipeSide(); renderPipeMain(); pipeLoad(); }
}
// coming to the tab: the last project, or the one open in Unity, or the newest
function pipeEnter() {
  if (!AI.proj) {
    let want = ""; try { want = localStorage.getItem("vrclib.pipeproj") || ""; } catch (e) {}
    const ps = (S.projs || (S.data || {}).projects || []).slice().sort((a, b) => ((b.running || 0) - (a.running || 0)) || (b.opened || 0) - (a.opened || 0) || a.name.localeCompare(b.name));
    const p = ps.find(x => x.path === want) || ps[0];
    if (p) AI.proj = p;
  }
  renderPipeSide(); renderPipeMain(); pipeLoad();
  if (!S.projs) loadProjects().then(() => { if (S.view === "pipe") { if (!AI.proj) pipeEnter(); else renderPipeSide(); } });
}
async function pipeLoad() {
  const p = AI.proj;
  PIPE.loading = true;
  const jobs = [aiLoadCfg().catch(() => {}), api("/api/pipe/toolchain", {}).then(r => { if (r.ok) PIPE.tool = r.toolchain; }).catch(() => {})];
  if (p) jobs.push(api("/api/pipe/assets", { project: p.path }).then(r => {
    if (!AI.proj || AI.proj.path !== p.path) return;
    if (!r.ok) { PIPE.err = r.err || "读不到工程"; PIPE.assets = []; return; }
    PIPE.err = ""; PIPE.assets = r.assets || []; AI.sess = Object.assign(AI.sess || {}, { kit: r.kit });
    pipeDefaults();
  }).catch(() => { PIPE.err = "软件没有回应"; PIPE.assets = []; }));
  await Promise.all(jobs);
  PIPE.loading = false;
  if (S.view !== "pipe") return;
  if (!PIPE.hier) PIPE.hier = (AI.cfg || {}).hierarchy || "";
  renderPipeSide(); renderPipeMain(); aiPoll();
}
// which rows start ticked: what was just imported, else what the library knows and is not on the avatar yet
function pipeDefaults() {
  const rows = PIPE.assets || [];
  const preset = (PIPE.preset || []).map(x => x.toLowerCase().replace(/\\/g, "/").replace(/\/+$/, ""));
  for (const a of rows) {
    if (PIPE.pick[a.folder] !== undefined) continue;
    const low = a.folder.toLowerCase();
    if (preset.length) PIPE.pick[a.folder] = preset.some(t => low === t || low.startsWith(t + "/") || t.startsWith(low + "/"));
    else PIPE.pick[a.folder] = !!a.source && !a.worn && a.kind !== "其他" && a.kind !== "素体";
  }
  if (preset.length) { // a folder the scan did not list (nothing with a prefab in it yet) is still offered
    for (const t of preset) {
      const covered = rows.some(a => { const l = a.folder.toLowerCase(); return l === t || l.startsWith(t + "/") || t.startsWith(l + "/"); });
      if (!covered && !PIPE.extra.some(x => x.folder.toLowerCase() === t)) PIPE.extra.push({ folder: t, name: t.split("/").pop(), kind: "衣服", prefabs: 0, pick: true });
    }
    PIPE.preset = null;
  }
}
function pipeRows() {
  const rows = (PIPE.assets || []).map(a => Object.assign({}, a, { kind: PIPE.kind[a.folder] || a.kind, pick: !!PIPE.pick[a.folder] }));
  for (const x of PIPE.extra) rows.push(Object.assign({}, x, { kind: PIPE.kind[x.folder] || x.kind, pick: PIPE.pick[x.folder] === undefined ? x.pick : !!PIPE.pick[x.folder], extra: true }));
  return rows;
}
function pipePicked() { return pipeRows().filter(a => a.pick && a.kind !== "其他"); }

function renderPipeSide() {
  const ps = (S.projs || (S.data || {}).projects || []).slice().sort((a, b) => ((b.running || 0) - (a.running || 0)) || (b.opened || 0) - (a.opened || 0) || a.name.localeCompare(b.name));
  const q = (S.pq || "").toLowerCase(), cur = AI.proj ? AI.proj.path : "";
  const t = PIPE.tool, c = AI.cfg || {};
  const list = ps.filter(p => !q || (p.name + " " + p.path).toLowerCase().includes(q));
  const tool = (ok, name, val, btn) => `<div class="toolrow"><i class="dot ${ok ? "ok" : "warn"}"></i><span class="k">${name}</span><span class="v">${val}</span>${btn || ""}</div>`;
  let tools = "";
  if (t) {
    tools += tool(!!t.unity, `Unity ${esc(t.unityWant)}`, t.unity ? "已安装" : "没找到", t.unity ? "" : `<button class="btn small" data-url="${esc(t.hubInstallLink)}" title="在 Unity Hub 里安装 VRChat 要求的这个版本">安装</button>`);
    tools += tool(t.alcomSeen, "ALCOM", t.alcom ? "已安装" : t.alcomSeen ? "有配置，没找到程序" : "没找到", t.alcom ? `<button class="btn small" data-pipe="launch" data-what="alcom">打开</button>` : `<button class="btn small" data-url="https://vrc-get.anatawa12.com/alcom/" title="ALCOM 是 VRChat 官方 VCC 的替代品，管理工程里的插件">下载</button>`);
    if (t.vcc) tools += tool(true, "VCC", "已安装", `<button class="btn small" data-pipe="launch" data-what="vcc">打开</button>`);
    if (!t.alcom && !t.vcc) tools += tool(false, "VCC", "没找到", "");
  } else tools = `<div class="toolrow"><i class="dot"></i><span class="k">正在检测…</span></div>`;
  $("#side").innerHTML = `<h3 class="withact">工程<span class="foldn">${ps.length ? ps.length : ""}</span></h3>
    <div class="projlist">${list.map(p => `<button class="navitem${p.path === cur ? " on" : ""}" data-pipeproj="${esc(p.path)}" title="${esc(p.path)}"><span class="dot" style="background:${p.running ? "var(--ok)" : "transparent"}"></span><span class="nm">${esc(p.name)}</span></button>`).join("")
      || `<p class="sidenote">${ps.length ? "没有符合条件的工程" : "还没有 Unity 工程。新建一个基础工程，或者在设置里添加放工程的文件夹。"}</p>`}</div>
    <div class="sidelinks"><button class="navitem strong" id="btnNewProj"><span>${ICON.plus}新建基础工程…</span></button><button class="navitem" id="btnProjAdd"><span>添加已有工程…</span></button></div>
    <h3>这台电脑</h3>
    <div class="tools">${tools}</div>
    <div class="toolrow"><i class="dot ${c.ready ? "ok" : "warn"}"></i><span class="k">AI 服务</span><span class="v" title="${c.ready ? esc(aiProvLabel(c)) : "不用 AI 也能跑流水线"}">${c.ready ? esc(aiProvLabel(c)) : "还没设置"}</span><button class="btn small" id="btnAICfg">${c.ready ? "更换" : "设置"}</button></div>
    <h3>流水线是什么</h3>
    <p class="sidenote">把素材库里导入工程的衣服、头发、配饰、道具，按类别装到模型上，生成菜单开关和图标。每一步都在 Unity 里能 Ctrl+Z 撤销，不会替你保存场景。</p>
    <p class="sidenote">没有工程的话先「新建基础工程」：自动建好 Unity 工程、装上 VRChat SDK 和常用改模插件，还能顺手把素体导进去。</p>`;
}

// the stations of the line, with what each one shows right now
function pipeStations() {
  const p = AI.proj, k = (AI.sess || {}).kit || {}, steps = (AI.sess || {}).steps || [], picked = pipePicked();
  const st = [];
  const ready = !!k.alive && !k.hint;
  st.push({ id: 1, name: "工程", dot: !p ? "" : ready ? "ok" : "warn", text: !p ? "选一个工程" : ready ? "Unity 已连接" : !k.pipeline ? "装插件、开 Unity" : k.alive ? "有事要处理" : "等 Unity 连上" });
  st.push({ id: 2, name: "素材", dot: picked.length ? "ok" : "", text: PIPE.assets === null ? "正在读取" : picked.length ? `勾了 ${picked.length} 件` : "勾选要装的" });
  const last = (tool) => { for (let i = steps.length - 1; i >= 0; i--) if (steps[i].kind === "tool" && steps[i].tool === tool) return steps[i]; return null; };
  const busy = (AI.sess || {}).busy;
  const d = last("dress") || last("place_avatar"), m = last("build_menu"), say = steps.length && steps[steps.length - 1].kind === "say";
  const state = (s, after) => !s ? (busy && after ? { dot: "run", text: "等上一站" } : { dot: "", text: "待开始" }) : s.busy ? { dot: "run", text: "进行中" } : s.ok ? { dot: "ok", text: "完成" } : { dot: "warn", text: "出错了" };
  st.push(Object.assign({ id: 3, name: "装配" }, state(d, true)));
  st.push(Object.assign({ id: 4, name: "菜单" }, state(m, true)));
  st.push({ id: 5, name: "验收", dot: m && !m.busy && m.ok && say && !busy ? "warn" : "", text: m && !m.busy && m.ok && say && !busy ? "去 Unity 看看" : "做完再来" });
  return st;
}
function pipeBeltHTML() {
  const busy = !!(AI.sess || {}).busy;
  return `<div class="belt${busy ? " moving" : ""}" role="tablist" aria-label="流水线工位">${pipeStations().map(s =>
    `<button class="station" data-station="${s.id}" role="tab"><span class="can"><i class="dot ${s.dot}"></i><b>${s.id}</b></span><span class="nm">${s.name}</span><span class="ds">${esc(s.text)}</span></button>`).join("")}</div>`;
}
function renderPipeMain() {
  const g = $("#grid"), p = AI.proj;
  $("#resultbar").innerHTML = "";
  if (!p) {
    g.innerHTML = `<div class="empty"><h2>还没有 Unity 工程</h2><p>流水线要在一个 Unity 工程里干活。没有的话，一键建一个带 VRChat SDK 和常用插件的基础工程。</p>
      <div class="btnrow center"><button class="btn primary" id="btnNewProj">${ICON.plus}<span>新建基础工程…</span></button><button class="btn" id="btnProjAdd">添加已有工程…</button></div></div>`;
    return;
  }
  const f = PIPE;
  g.innerHTML = `<div class="pipe">
    <div id="pipe_belt">${pipeBeltHTML()}</div>
    <section class="stn" id="stn1"><header><span class="no">1</span><h2>工程</h2><span class="pname" title="${esc(p.path)}">${esc(p.name)}<span class="sub">${esc(p.path)}</span></span>
      <div class="hacts"><button class="btn small" data-pipe="folder">${ICON.folder}<span>文件夹</span></button>${PIPE.tool && PIPE.tool.alcom ? `<button class="btn small" data-pipe="launch" data-what="alcom" title="管理这个工程的插件">在 ALCOM 里打开</button>` : ""}</div></header>
      <div class="aistat" id="ai_stat"></div></section>
    <section class="stn" id="stn2"><header><span class="no">2</span><h2>素材</h2><span class="hint">工程里已导入的素材，自动从 Assets 里读出来；勾上要装的，类别不对就在下拉里改。</span>
      <div class="hacts"><button class="btn small" data-pipe="rescan" title="重新读取工程里的素材">重新检测</button></div></header>
      <div id="pipe_assets">${pipeAssetsHTML()}</div>
      <div class="addrow"><input id="pipe_add" placeholder="手动添加位置：Assets/店铺名/素材名（也可以直接填 prefab）" autocomplete="off" spellcheck="false"><button class="btn small" data-pipe="add">添加</button></div></section>
    <section class="stn" id="stn3"><header><span class="no">3</span><h2>装配</h2><span class="no">4</span><h2>菜单</h2><span class="hint">按类别装到模型上，再生成菜单开关和图标。</span></header>
      <div class="howrow"><span>素体 → 场景里没有模型时放进去</span><span>衣服 → 互斥切换，可按部件开关</span><span>头发 → 单独互斥</span><span>配饰 → 开关，默认显示</span><span>道具 → 开关，默认隐藏</span></div>
      <div class="row"><label for="pipe_hier">菜单层级</label>
        <div class="inline"><input id="pipe_hier" value="${esc(f.hier || (AI.cfg || {}).hierarchy || "")}" autocomplete="off"><button class="btn small" data-ai="hierdef">恢复默认</button></div>
        <div class="hint2">用 &gt; 隔开每一层。{分类} 是衣服 / 头发 / 配饰 / 道具这一层，{素材} 是每个素材自己的一层，{开关} 是它的开关；去掉 {素材}，每个素材就只做一个总开关。也可以用自己的话写，AI 会照着排。</div></div>
      <div class="aigo"><label class="check"><input type="checkbox" id="pipe_noai"${f.noAI ? " checked" : ""}>不用 AI，按网格名字分组（快，不花钱，分得粗一些）</label>
        <button class="btn big" id="pipe_go" data-ai="dress">${ICON.play}<span>开始流水线</span></button></div></section>
    <section class="stn" id="stn5"><header><span class="no">5</span><h2>验收</h2><span class="hint">每一步会列在下面。做完后到 Unity 里检查，满意再保存。</span></header>
      <div class="ailog" id="ai_log" aria-live="polite"></div>
      <div class="aifoot"><textarea id="ai_text" placeholder="让 AI 接着做别的，例如：把鞋子开关改名叫凉鞋；给外套加一个红色配色；检查有没有丢失的材质。Ctrl+Enter 发送">${esc(f.text)}</textarea>
        <div class="aibtns" id="ai_btns"></div></div>
      <ul class="accept"><li>在 Unity 里进 Play 模式，用 Gesture Manager 把菜单点一遍：互斥是不是只留一件、开关是不是对的物体。</li><li>满意就按 Ctrl+S 保存场景；不满意按 Ctrl+Z，或者点「撤销上一步」。</li><li>上传前看 VRChat SDK 面板：同步参数不能超过 256 位，有警告就回来让 AI 减几个开关。</li></ul></section>
  </div>`;
  aiRenderLive();
}
function pipeAssetsHTML() {
  if (PIPE.err) return `<div class="aiempty err">${esc(PIPE.err)}</div>`;
  if (PIPE.assets === null) return `<div class="aiempty">正在读取工程里的素材…</div>`;
  const rows = pipeRows();
  if (!rows.length) return `<div class="aiempty">这个工程的 Assets 里还没有素材。到「素材库」里挑一件，用「一键导入」导进来，或者在下面手动填位置。</div>`;
  const k = (AI.sess || {}).kit || {};
  const sel = a => `<select data-pkind="${esc(a.folder)}" title="类别">${KINDS.map(x => `<option${x === a.kind ? " selected" : ""}>${x}</option>`).join("")}</select>`;
  return `<table class="atable"><thead><tr><th></th><th>素材</th><th>类别</th><th>在工程里的位置</th><th class="r">prefab</th><th></th></tr></thead><tbody>${rows.map(a => {
    const marks = [];
    if (a.worn) marks.push(`<span class="tag ok" title="它的 prefab 已经在模型上">已装上</span>`);
    if (a.avatar) marks.push(`<span class="tag" title="里面有整只模型的 prefab">整只模型</span>`);
    if (a.source) marks.push(`<span class="tag faint" title="${a.source === "导入" ? "这个软件导进来的" : "按文件匹配到素材库里的素材"}">${esc(a.source)}</span>`);
    if (a.extra) marks.push(`<button class="tag x" data-pipe="drop" data-folder="${esc(a.folder)}" title="去掉这一行">×</button>`);
    return `<tr class="${a.pick ? "on" : ""}${a.kind === "其他" ? " dim" : ""}"><td><input type="checkbox" data-ppick="${esc(a.folder)}"${a.pick ? " checked" : ""} aria-label="装这个"></td>
      <td class="nm" title="${esc(a.name)}">${esc(a.name)}</td><td>${sel(a)}</td><td class="path" title="${esc(a.folder)}">${esc(a.folder)}</td><td class="r muted">${a.prefabs || ""}</td><td class="marks">${marks.join("")}</td></tr>`;
  }).join("")}</tbody></table>${k.alive ? "" : `<div class="hint2">Unity 连上后，这里还会标出哪些已经装在模型上、哪些是整只模型。</div>`}`;
}
// the one thing to do next gets the coloured button: the plugins, Unity, the service, then the line itself
function aiNext() {
  const c = AI.cfg || {}, k = (AI.sess || {}).kit;
  if (!k) return "";
  if (!k.pipeline || k.pipelineOld) return "install";
  if (!k.running && k.editor) return "unity";
  if (!k.alive) return "";
  if (!c.ready && !PIPE.noAI && !($("#pipe_noai") || {}).checked) return "cfg";
  return ((AI.sess || {}).steps || []).length ? "send" : "dress";
}
function aiStatHTML() {
  const c = AI.cfg || {}, k = (AI.sess || {}).kit, next = aiNext(), pri = w => next === w ? " primary" : "";
  const row = (dot, key, val, btn) => `<div class="airow"><i class="dot ${dot}"></i><span class="k">${key}</span><span class="v">${val}</span>${btn || ""}</div>`;
  let h = "";
  if (!k) return row("", "Unity 插件", "正在检查…") + row("", "Unity", "正在检查…");
  const sk = k.skills ? `UnitySkills ${esc(k.skillsVer || "")}${k.skills === "own" ? "（工程自带）" : ""}` : "";
  h += k.pipeline
    ? row(k.pipelineOld ? "warn" : "ok", "Unity 插件", `${k.pipelineOld ? "已安装，有新版" : "已安装"}<span class="sub">　流水线插件${sk ? " + " + sk : ""}</span>`,
      `${k.pipelineOld ? `<button class="btn small${pri("install")}" data-ai="install">更新</button>` : ""}<button class="btn small ghost" data-ai="remove">移除</button>`)
    : row("warn", "Unity 插件", "这个工程还没有装", `<button class="btn small${pri("install")}" data-ai="install">安装并打开 Unity</button>`);
  if (!k.pipeline) return h + row("", "Unity", k.running ? "开着" : "没有打开", !k.running && k.editor ? `<button class="btn small" data-ai="unity">打开 Unity</button>` : "");
  const ok = k.alive && !k.hint;
  const conn = k.alive ? `已连接${k.skillsOn ? `<span class="sub">　UnitySkills 端口 ${k.port}${k.mode ? "，" + esc(k.mode) + " 模式" : ""}</span>` : `<span class="sub">　UnitySkills 服务还没启动（穿戴和菜单不受影响）</span>`}` : "";
  h += row(ok ? "ok" : "warn", "Unity", k.hint ? `${conn ? conn + "<br>" : ""}<span class="warnline">${esc(k.hint)}</span>` : conn,
    !k.running && k.editor ? `<button class="btn small${pri("unity")}" data-ai="unity">打开 Unity</button>` : "");
  if (!c.ready) h += row("warn", "AI 服务", "还没设置。不用 AI 也能跑流水线（按网格名字分组）", `<button class="btn small${pri("cfg")}" data-ai="cfg">设置</button>`);
  else {
    const vm = esc(c.eyeModel || ""), set = `<button class="btn small ghost" data-ai="cfg">设置</button>`;
    const looks = {
      main: ["ok", "AI 自己看截图", set],
      other: ["ok", `这个模型看不了图，由${c.eyeOwn ? "同一家的" : "看图模型"} ${vm} 把截图描述给它`, set],
      unknown: ["", `第一次拍照时会测一下这个模型能不能看图<span class="sub">　截图也会显示在下面的记录里</span>`, set],
      none: ["warn", "这个模型看不了图，这一家也没有现成的看图模型：截图只显示给你看。另设一个看图模型，AI 就能检查画面", set],
      off: ["", "已关闭：截图只显示给你看", set],
    }[c.looks || "unknown"];
    h += row(looks[0], "看图", looks[1], looks[2] || "");
  }
  return h;
}
function aiLogHTML() {
  const steps = (AI.sess || {}).steps || [];
  if (!steps.length) return `<div class="aiempty">流水线做的每一步会列在这里。<br>改动都能在 Unity 里按 Ctrl+Z 撤销；它不会替你保存场景。</div>`;
  const busyTool = steps.some(s => s.busy);
  return steps.map(s => {
    if (s.kind === "ask") {
      return `<div class="aistep ask${s.busy ? " open" : ""}"><div>${esc(s.text)}</div>${s.busy
        ? `<div class="btnrow"><button class="btn small primary" data-ai="allow">允许这一次</button><button class="btn small" data-ai="allowall" title="这段对话里之后的这类操作都不再问">这次对话都允许</button><button class="btn small" data-ai="deny">不允许</button></div>`
        : `<div class="out">${esc(s.out || "")}</div>`}</div>`;
    }
    if (s.kind === "tool") {
      const mark = s.busy ? `<i class="spin"></i>` : s.ok ? `<i class="mk ok">✓</i>` : `<i class="mk bad">!</i>`;
      const shots = (s.imgs || []).length ? `<span class="shots">${s.imgs.map((n, i) => `<img src="/shot?f=${encodeURIComponent(n)}" data-shot="${i}" data-shots="${esc(s.imgs.join("|"))}" alt="截图 ${i + 1}" title="点击放大" loading="lazy">`).join("")}</span>` : "";
      return `<div class="aistep tool${s.busy ? " busy" : ""}">${mark}<span><b>${esc(s.text)}</b>${s.out ? `<span class="out">　${esc(s.out)}</span>` : ""}${shots}</span></div>`;
    }
    return `<div class="aistep ${s.kind === "error" ? "err" : s.kind}">${esc(s.text)}</div>`;
  }).join("") + ((AI.sess || {}).busy && !busyTool ? `<div class="aistep tool busy"><i class="spin"></i><span class="out">AI 正在想下一步…</span></div>` : "");
}
function aiBtnsHTML() {
  const s = AI.sess || {};
  if (s.busy) return `<button class="btn" data-ai="stop">停止</button>`;
  return `<button class="btn${aiNext() === "send" ? " primary" : ""}" data-ai="send">发送</button>
    <div class="aimini">${s.changes ? `<button class="btn small ghost" data-ai="undo" title="相当于在 Unity 里按一次 Ctrl+Z">撤销上一步</button>` : ""}${(s.steps || []).length ? `<button class="btn small ghost" data-ai="reset" title="清空这段对话，AI 不再记得前面说过什么。Unity 里已经做的改动不受影响">新对话</button>` : ""}</div>`;
}
// only the parts that change are redrawn, so what the player is typing stays
function aiRenderLive() {
  if (!pipeActive() || !$("#pipe_belt")) return;
  const parts = { pipe_belt: pipeBeltHTML(), ai_stat: aiStatHTML(), ai_log: aiLogHTML(), ai_btns: aiBtnsHTML() };
  for (const id in parts) {
    const el = $("#" + id); if (!el || el.dataset.h === parts[id]) continue;
    const atEnd = id === "ai_log" && el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    el.innerHTML = parts[id]; el.dataset.h = parts[id];
    if (atEnd) el.scrollTop = el.scrollHeight;
  }
  const go = $("#pipe_go"); if (go) { go.classList.toggle("primary", aiNext() === "dress"); go.disabled = !!(AI.sess || {}).busy; }
}
// after a button did its work: draw again even if nothing in the text changed (a disabled button comes back)
function aiFresh() {
  for (const id of ["pipe_belt", "ai_stat", "ai_log", "ai_btns"]) { const el = $("#" + id); if (el) delete el.dataset.h; }
  return aiPoll();
}
async function aiPoll() {
  clearTimeout(AI.timer);
  if (!pipeActive()) return;
  const path = AI.proj.path, hadSteps = ((AI.sess || {}).steps || []).length, wasBusy = !!(AI.sess || {}).busy;
  try {
    const r = await api("/api/ai/session", { project: path });
    if (!AI.proj || AI.proj.path !== path) return;
    if (r.ok) { AI.sess = r; aiRenderLive(); }
  } catch (e) {}
  const s = AI.sess || {};
  // a run just ended: the asset list may know more now (what is on the avatar)
  if (wasBusy && !s.busy && (s.steps || []).length >= hadSteps) pipeRescan(true);
  // … and so may the 看图 row: the first look finds out whether the model reads pictures
  if (wasBusy && !s.busy) aiLoadCfg().then(() => { if (AI.proj && AI.proj.path === path) aiRenderLive(); }).catch(() => {});
  AI.timer = setTimeout(aiPoll, s.busy || !(s.kit || {}).alive ? 900 : 3000);
}
async function pipeRescan(quiet) {
  const p = AI.proj; if (!p) return;
  let r; try { r = await api("/api/pipe/assets", { project: p.path, fresh: true }); } catch (e) { return; }
  if (!AI.proj || AI.proj.path !== p.path || !r.ok) { if (!quiet && r && !r.ok) toast(r.err || "读不到工程"); return; }
  PIPE.assets = r.assets || []; AI.sess = Object.assign(AI.sess || {}, { kit: r.kit }); pipeDefaults();
  const el = $("#pipe_assets"); if (el) el.innerHTML = pipeAssetsHTML();
  aiRenderLive();
  if (!quiet) toast(`读到 ${PIPE.assets.length} 项`);
}
async function aiRun(body) {
  const r = await api("/api/ai/run", Object.assign({ project: AI.proj.path }, body));
  if (!r.ok) { toast(r.err || "没能开始", 5000); return false; }
  const log = $("#ai_log"); if (log) log.scrollTop = log.scrollHeight;
  const sec = $("#stn5"); if (sec) sec.scrollIntoView({ behavior: "smooth", block: "start" });
  aiPoll(); return true;
}
async function aiSend() {
  const ta = $("#ai_text"); if (!ta || (AI.sess || {}).busy) return;
  const text = ta.value.trim(); if (!text) { ta.focus(); return; }
  if (!AI.cfg.ready) { toast("先设置 AI 服务"); pipeKeep(); AI.back = AI.proj.path; return openAICfg(); }
  if (await aiRun({ mode: "chat", text })) { ta.value = ""; PIPE.text = ""; }
}
async function pipeStart() {
  const p = AI.proj, k = (AI.sess || {}).kit || {}, noAI = $("#pipe_noai").checked, hierarchy = $("#pipe_hier").value.trim();
  const picked = pipePicked();
  if (!picked.length) { toast("先在「素材」里勾上要装的素材", 4500); const s = $("#stn2"); if (s) s.scrollIntoView({ behavior: "smooth", block: "start" }); return; }
  if (!noAI && !AI.cfg.ready) { toast("先设置 AI 服务，或者勾上「不用 AI」"); pipeKeep(); AI.back = p.path; return openAICfg(); }
  if (!k.alive) { toast(k.hint || "Unity 还没有连上", 5000); const s = $("#stn1"); if (s) s.scrollIntoView({ behavior: "smooth", block: "start" }); return; }
  pipeKeep();
  await aiRun({ mode: "dress", assets: picked.map(a => ({ folder: a.folder, kind: a.kind, name: a.name })), hierarchy, noAI });
}
async function pipeAction(b) {
  const what = b.dataset.pipe, p = AI.proj;
  if (what === "launch") {
    b.disabled = true;
    const r = await api("/api/pipe/launch", { what: b.dataset.what });
    b.disabled = false; toast(r.ok ? "正在打开" : r.err || "没能打开", 4000); return;
  }
  if (what === "folder" && p) return openPath(p.path);
  if (what === "rescan") { b.disabled = true; await pipeRescan(false); b.disabled = false; return; }
  if (what === "add") {
    const inp = $("#pipe_add"), f = inp.value.trim().replace(/\\/g, "/").replace(/^\/+|\/+$/g, "");
    if (!f) { inp.focus(); return; }
    if (!/^Assets\//i.test(f)) { toast("位置要以 Assets/ 开头", 4000); inp.focus(); return; }
    if (pipeRows().some(a => a.folder.toLowerCase() === f.toLowerCase())) { toast("已经在列表里了"); inp.value = ""; return; }
    PIPE.extra.push({ folder: f, name: f.replace(/\.prefab$/i, "").split("/").pop(), kind: "衣服", prefabs: 0, pick: true });
    inp.value = ""; $("#pipe_assets").innerHTML = pipeAssetsHTML(); aiRenderLive(); return;
  }
  if (what === "drop") {
    PIPE.extra = PIPE.extra.filter(x => x.folder !== b.dataset.folder); delete PIPE.pick[b.dataset.folder];
    $("#pipe_assets").innerHTML = pipeAssetsHTML(); aiRenderLive(); return;
  }
}
async function aiAction(b) {
  const what = b.dataset.ai, p = AI.proj;
  if (b.dataset.aiprov) { aiCfgKeep(); AI.cfgForm.provider = b.dataset.aiprov; return openAICfg(true); }
  if (what === "cfg") { pipeKeep(); AI.back = p ? p.path : null; return openAICfg(); }
  if (what === "hierdef") { $("#pipe_hier").value = AI.cfg.defaultHierarchy; PIPE.hier = AI.cfg.defaultHierarchy; return; }
  if (what === "install") {
    const k = (AI.sess || {}).kit || {};
    if (!k.pipeline && !confirm(`给「${p.name}」安装 AI 插件？\n\n会往这个工程的 Packages 里放两个编辑器插件：\n· MioVRCA 改模流水线：放素体、穿戴素材、生成菜单和图标\n· UnitySkills ${k.skills === "own" ? "（工程里已经有，用它自己的）" : "2.8.4（开源，MIT）：让 AI 能做更多 Unity 操作"}\n\n只在 Unity 编辑器里运行，不会跟模型一起上传。装完会打开 Unity，第一次要编译一两分钟。\n随时可以在这里点「移除」。`)) return;
    b.disabled = true;
    const r = await api("/api/ai/setup", { project: p.path, on: true });
    toast(r.ok ? r.note || "已安装" : r.err || "没能安装", 6000); return aiFresh();
  }
  if (what === "remove") {
    if (!confirm(`从「${p.name}」移除 AI 插件？\n\n会删掉软件放进去的插件（工程自带的 UnitySkills 不动）。已经穿好的衣服和生成的菜单都会留着，它们只依赖 Modular Avatar。`)) return;
    const r = await api("/api/ai/setup", { project: p.path, on: false });
    toast(r.ok ? r.note : r.err || "没能移除", 5000); return aiFresh();
  }
  if (what === "unity") {
    b.disabled = true;
    const r = await api("/api/project/open", { path: p.path });
    toast(r.ok ? "正在打开 Unity，连上后这里会变成「已连接」" : r.err || "没能打开", 5000); return aiFresh();
  }
  if (what === "dress") return pipeStart();
  if (what === "send") return aiSend();
  if (what === "allow" || what === "allowall" || what === "deny") {
    b.disabled = true;
    await api("/api/ai/answer", { project: p.path, ok: what !== "deny", all: what === "allowall" }); return aiFresh();
  }
  if (what === "pick") { closeModal(); return openAI(b.dataset.path); }
  if (what === "stop") { await api("/api/ai/cancel", { project: p.path }); toast("正在停止"); return aiFresh(); }
  if (what === "undo") {
    b.disabled = true;
    const r = await api("/api/ai/undo", { project: p.path });
    toast(r.ok ? "已撤销：" + (r.undone || "上一步") : r.err || "没能撤销", 4000); return aiFresh();
  }
  if (what === "reset") {
    if (!confirm("开始新对话？AI 不再记得前面说过什么。Unity 里已经做的改动不受影响。")) return;
    await api("/api/ai/reset", { project: p.path }); return aiFresh();
  }
  // the AI service form
  if (what === "cfgclose") return aiCfgDone();
  if (b.dataset.aimodel !== undefined) { $("#ai_model").value = b.dataset.aimodel; aiCfgKeep(); return aiModelList(false); }
  if (what === "models" && !b.dataset.again && $("#ai_modelist") && !$("#ai_modelist").hidden) return aiModelList(false); // open: the button folds it away
  if (what === "models" && (AI.cfgForm.models[AI.cfgForm.provider] || []).length && !b.dataset.again) return aiModelList(true, true);
  if (what === "models" || what === "test") {
    const out = $("#ai_test"), body = aiCfgBody();
    b.disabled = true; out.className = "aitest"; out.textContent = what === "test" ? "正在连接…" : "正在读取模型…";
    let r; try { r = await api("/api/ai/" + what, body); } catch (e) { r = { ok: false, err: "软件没有回应" }; }
    b.disabled = false;
    if (!$("#ai_test")) return;
    if (!r.ok) { out.className = "aitest err"; out.textContent = r.err; return; }
    if (what === "test") { out.className = "aitest ok"; out.textContent = r.note; return; }
    AI.cfgForm.models[body.provider] = r.models;
    out.className = "aitest ok"; out.textContent = `读到 ${r.models.length} 个模型，在列表里点一个`;
    return aiModelList(true, true);
  }
  if (what === "vpreset") {
    aiCfgKeep(); const p = (AI.cfg.visionPresets || []).find(x => x.id === b.dataset.v); if (!p) return;
    Object.assign(AI.cfgForm.vision, { wire: p.wire, base: p.baseUrl, model: p.model });
    const top = $("#modal .mbody").scrollTop; await openAICfg(true); $("#modal .mbody").scrollTop = top; return;
  }
  if (what === "vmode" || what === "vwire") {
    aiCfgKeep(); AI.cfgForm.vision[what === "vmode" ? "mode" : "wire"] = b.dataset.v;
    const top = $("#modal .mbody").scrollTop; await openAICfg(true); $("#modal .mbody").scrollTop = top; return;
  }
  if (what === "vtest") {
    const out = $("#ai_vtest"), body = aiCfgBody(), x = AI.cfgForm.vision;
    body.who = b.dataset.who; body.vision = aiVisionBody(); if (x.key.trim()) body.visionKey = x.key.trim();
    b.disabled = true; out.className = "aitest"; out.textContent = "正在给它看一张写着数字的图…";
    let r; try { r = await api("/api/ai/visiontest", body); } catch (e) { r = { ok: false, err: "软件没有回应" }; }
    b.disabled = false;
    if (!$("#ai_vtest")) return;
    if (r.ok && r.ai) AI.cfg = r.ai;
    out.className = "aitest " + (r.ok && r.sees ? "ok" : "err"); out.textContent = r.ok ? r.note : r.err; return;
  }
  if (what === "cfgsave" || what === "keyclear" || what === "vkeyclear") {
    const body = aiCfgBody(), x = AI.cfgForm.vision;
    if (what === "keyclear") body.key = ""; else if (!body.key) delete body.key;
    body.vision = aiVisionBody();
    if (what === "vkeyclear") body.visionKey = ""; else if (x.key.trim()) body.visionKey = x.key.trim();
    const r = await api("/api/ai/save", body);
    if (!r.ok) { const out = $("#ai_test"); out.className = "aitest err"; out.textContent = r.err; return; }
    AI.cfg = r.ai;
    if (what === "keyclear" || what === "vkeyclear") { AI.cfgForm = null; toast("已清除保存的 Key"); return openAICfg(); }
    toast("已保存");
    if (!AI.back && AI.cfg.ready && S.view !== "pipe") { AI.cfgForm = null; return openAIPick(); } // set up from the settings: which project is it for?
    return aiCfgDone();
  }
}

// ---------- 新建基础工程 ----------
function npJob() { return (S.data || {}).newProject || null; }
function npBusy() { const j = npJob(); return !!(j && !["done", "failed"].includes(j.stage)); }
async function openNewProject() {
  const m = $("#modal");
  if (!PIPE.tool) { try { const r = await api("/api/pipe/toolchain", {}); if (r.ok) PIPE.tool = r.toolchain; } catch (e) {} }
  const t = PIPE.tool || { plugins: [], bases: [] };
  if (!PIPE.npForm) {
    const picked = {}; for (const pl of t.plugins || []) picked[pl.pkg] = !!pl.default;
    PIPE.npForm = { name: "", parent: t.defaultDir || "", base: "", plugins: picked, aiKit: true };
  }
  m.dataset.kind = "np"; m.classList.remove("wide");
  renderNP(); m.classList.add("on"); $("#scrim").classList.add("on");
  const n = $("#np_name"); if (n) n.focus();
}
function npKeep() {
  const f = PIPE.npForm; if (!f || !$("#np_name")) return;
  f.name = $("#np_name").value; f.parent = $("#np_parent").value; f.base = $("#np_base").value; f.aiKit = $("#np_kit").checked;
  for (const x of document.querySelectorAll("[data-npplug]")) f.plugins[x.dataset.npplug] = x.checked;
}
const NP_STAGES = [["prepare", "准备"], ["repos", "读取插件仓库"], ["packages", "下载插件"], ["settings", "写工程设置"], ["base", "导入素体"], ["kit", "装 AI 插件"]];
function renderNP() {
  const m = $("#modal"), t = PIPE.tool || { plugins: [], bases: [] }, f = PIPE.npForm, j = npJob();
  if (j) { // a job is running or just ended: its progress replaces the form
    const idx = NP_STAGES.findIndex(s => s[0] === j.stage);
    const list = NP_STAGES.map((s, i) => {
      const cls = j.stage === "done" || i < idx ? "done" : i === idx && j.stage !== "failed" ? "cur" : i === idx ? "bad" : "";
      return `<li class="${cls}"><i>${cls === "done" ? "✓" : cls === "cur" ? "" : cls === "bad" ? "!" : ""}</i>${s[1]}${i === idx && j.stage !== "done" && j.stage !== "failed" ? `<span class="muted">　${esc(j.msg || "")}</span>` : ""}</li>`;
    }).join("");
    const imp = S.data.importJob, choose = j.stage === "base" && imp && imp.stage === "choose";
    const pct = j.total ? Math.round(j.done / j.total * 100) : 0;
    let tail = "";
    if (j.stage === "done") tail = `<div class="impres ok"><div>工程建好了：<b>${esc(j.name)}</b></div><div class="small">${esc(j.path)}</div>
        ${(j.packages || []).length ? `<div class="small">装了 ${j.packages.length} 个包：${j.packages.map(esc).join("、")}</div>` : ""}
        ${(j.notes || []).map(n => `<div class="small${/没|失败|不了|出错|跳过/.test(n) ? " warnline" : ""}">${esc(n)}</div>`).join("")}</div>`;
    else if (j.stage === "failed") tail = `<div class="impres err"><div>${esc(j.err || "没能建好")}</div>${(j.notes || []).map(n => `<div class="small">${esc(n)}</div>`).join("")}<div class="small">已经建出来的文件留在 ${esc(j.path)}，可以删掉重来。</div></div>`;
    else if (choose) tail = `<div class="impchoose"><div>素体有不同的版本，选要导入的：</div>
        ${(imp.choices || []).map((c, i) => `<label class="check"><input type="checkbox" data-ipick="${i}"${c.pick ? " checked" : ""}><span class="n">${esc(c.name)}</span>${(c.bases || []).map(b => `<span class="base">${esc(b)}</span>`).join("")}<span class="s">${fmtSize(c.size)}</span></label>`).join("")}
        <div class="btnrow"><button class="btn small primary" data-np="impgo">导入所选</button><button class="btn small ghost" data-np="impskip">不导入素体</button></div></div>`;
    else tail = `<div class="bar wide"><i style="width:${pct}%"></i></div>`;
    m.innerHTML = `<div class="mhead">新建基础工程</div><div class="mbody"><ol class="npstages">${list}</ol>${tail}</div>
      <div class="mfoot">${j.stage === "done" ? `<button class="btn ghost" data-np="dismiss">关闭</button><button class="btn primary" data-np="goto">去流水线</button>`
        : j.stage === "failed" ? `<button class="btn ghost" data-np="dismiss">关闭</button><button class="btn" data-np="again">改一改再试</button>`
        : `<button class="btn ghost" data-np="cancel">取消</button>`}</div>`;
    return;
  }
  const unity = t.unity ? `<span class="okline">Unity ${esc(t.unityWant)} 已安装</span>` : `<span class="warnline">这台电脑没有 Unity ${esc(t.unityWant)}</span>　<button class="linkbtn" data-url="${esc(t.hubInstallLink || "")}">用 Unity Hub 安装</button>`;
  const alcom = t.alcom ? `<span class="okline">ALCOM 已安装，新工程会出现在它的列表里</span>` : t.alcomSeen ? `<span class="muted">有 ALCOM / VCC 的配置，新工程会加进它的列表</span>` : `<span class="muted">没有 ALCOM / VCC 也能建；插件直接从官方仓库下载</span>`;
  m.innerHTML = `<div class="mhead">新建基础工程</div><div class="mbody">
    <p class="lead">一键建一个 VRChat 改模工程：Unity ${esc(t.unityWant)} 的工程设置、VRChat SDK（Avatars）和常用改模插件都装好，还能顺手把素体导进去。</p>
    <div class="row"><label for="np_name">工程名</label><input id="np_name" value="${esc(f.name)}" placeholder="例如 Kaguya_Mod" autocomplete="off" spellcheck="false"></div>
    <div class="row"><label for="np_parent">放在哪个文件夹</label><div class="inline"><input id="np_parent" value="${esc(f.parent)}" placeholder="D:\\VRChat_Project" autocomplete="off" spellcheck="false"><button class="btn small" data-np="pick">选择…</button></div>
      <div class="hint2">工程会建在这个文件夹下的「工程名」子文件夹里。</div></div>
    <div class="row"><label for="np_base">素体</label><select id="np_base"><option value="">不导入（之后在素材库里一键导入）</option>${(t.bases || []).map(b => `<option value="${esc(b.key)}"${b.key === f.base ? " selected" : ""}>${esc(b.name)}${(b.bases || []).length ? "（" + esc(b.bases.join("、")) + "）" : ""}</option>`).join("")}</select>
      <div class="hint2">素材库里分类是「素体」的素材。选了就在建好工程后直接导入，之后流水线能把它放进场景。</div></div>
    <div class="row"><label>插件</label><div class="plugs">${(t.plugins || []).map(pl => `<label class="check opt"><input type="checkbox" data-npplug="${esc(pl.pkg)}"${f.plugins[pl.pkg] ? " checked" : ""}${pl.fixed ? " disabled" : ""}><span><b>${esc(pl.label)}</b><span class="muted">　${esc(pl.note || "")}</span></span></label>`).join("")}</div>
      <div class="hint2">都从各自的官方仓库下载最新稳定版（ALCOM / VCC 已经下载过的直接复用）。VRChat SDK 和 Modular Avatar 是流水线必需的。</div></div>
    <label class="check opt"><input type="checkbox" id="np_kit"${f.aiKit ? " checked" : ""}><span>建好后装上 AI 插件并打开 Unity</span></label>
    <div class="npenv">${unity}<br>${alcom}</div>
  </div><div class="mfoot"><button class="btn ghost" data-m="close">取消</button><button class="btn primary" data-np="create">创建</button></div>`;
}
function renderNPLive() { if ($("#modal").dataset.kind === "np" && $("#modal").classList.contains("on")) renderNP(); }
async function npAction(b) {
  const what = b.dataset.np, j = npJob();
  if (what === "pick") { npKeep(); const p = await pickInto("选择放工程的文件夹"); if (p) { PIPE.npForm.parent = p; renderNP(); } return; }
  if (what === "create") {
    npKeep();
    const f = PIPE.npForm, plugins = Object.keys(f.plugins).filter(k => f.plugins[k]);
    if (!f.name.trim()) { toast("给工程起个名字"); $("#np_name").focus(); return; }
    if (!f.parent.trim()) { toast("选一个放工程的文件夹"); $("#np_parent").focus(); return; }
    b.disabled = true;
    const r = await api("/api/pipe/create", { name: f.name.trim(), parent: f.parent.trim(), base: f.base, plugins, aiKit: f.aiKit });
    if (!r.ok) { b.disabled = false; toast(r.err || "没能开始", 5000); return; }
    S.data.newProject = r.job; renderNP(); poll(true); return;
  }
  if (what === "cancel") { await api("/api/pipe/create/cancel", {}); toast("正在取消"); return poll(true); }
  if (what === "impgo") {
    const cs = (S.data.importJob || {}).choices || [];
    const paths = [...document.querySelectorAll("#modal [data-ipick]")].filter(x => x.checked).map(x => (cs[+x.dataset.ipick] || {}).path).filter(Boolean);
    if (!paths.length) { toast("至少选一个"); return; }
    await api("/api/import/choose", { paths }); return poll(true);
  }
  if (what === "impskip") { await api("/api/import/dismiss", {}); return poll(true); }
  if (what === "dismiss" || what === "goto" || what === "again") {
    const path = j ? j.path : "";
    await api("/api/pipe/create/dismiss", {});
    if (what === "again") { S.data.newProject = null; return renderNP(); }
    PIPE.npForm = null; closeModal(); await load();
    if (what === "goto" && path) { await loadProjects(); openAI(path); }
    return;
  }
}
// which project the AI is to work on
function openAIPick() {
  const ps = (S.projs || (S.data || {}).projects || []).slice().sort((a, b) => ((b.running || 0) - (a.running || 0)) || (b.opened || 0) - (a.opened || 0) || a.name.localeCompare(b.name));
  if (!ps.length) { closeModal(); toast("AI 服务已保存。到「流水线」页新建或添加 Unity 工程后就能用", 6000); return; }
  const m = $("#modal");
  m.dataset.kind = "aicfg"; m.classList.remove("wide");
  m.innerHTML = `<div class="mhead">给哪个工程用 AI？</div><div class="mbody">
    <p class="lead">选一个工程：下一步到「流水线」页给它装上 AI 插件并打开 Unity，连上后就能装素材、做菜单。</p>
    <div class="aipick">${ps.map(p => `<button class="cand" data-ai="pick" data-path="${esc(p.path)}"><span class="p"><b>${esc(p.name)}</b><br><span class="muted small">${esc(p.path)}</span></span>${p.running ? `<span class="note okline">Unity 里开着</span>` : p.unity ? `<span class="note">Unity ${esc(p.unity)}</span>` : ""}</button>`).join("")}</div>
  </div><div class="mfoot"><button class="btn ghost" data-m="close">以后再说</button></div>`;
  m.classList.add("on"); $("#scrim").classList.add("on");
  if (!S.projs) loadProjects().then(() => { if ($("#modal .aipick")) openAIPick(); });
}
// The list of the service's models under the model field. all: show every model (the button was pressed);
// otherwise the ones that match what is typed.
function aiModelList(open, all) {
  const box = $("#ai_modelist"), btn = $('[data-ai="models"]'); if (!box) return;
  const f = AI.cfgForm, ms = (f.models || {})[f.provider] || [];
  if (!open || !ms.length) { box.hidden = true; if (btn) btn.setAttribute("aria-expanded", "false"); return; }
  const cur = $("#ai_model").value.trim(), q = cur.toLowerCase();
  let list = all || ms.includes(cur) ? ms : ms.filter(m => m.toLowerCase().includes(q));
  if (!list.length) list = ms;
  box.innerHTML = list.map(m => `<button class="mopt${m === cur ? " on" : ""}" data-aimodel="${esc(m)}">${esc(m)}</button>`).join("") +
    `<button class="mopt again" data-ai="models" data-again="1">重新读取列表</button>`;
  box.hidden = false; if (btn) btn.setAttribute("aria-expanded", "true");
  const on = box.querySelector(".mopt.on"); if (on) on.scrollIntoView({ block: "nearest" });
}
// what is typed in the service form, per provider, so switching back and forth loses nothing
function aiCfgKeep() {
  const f = AI.cfgForm; if (!f || !$("#ai_base")) return;
  f.vals[f.provider] = { base: $("#ai_base").value, model: $("#ai_model").value, key: $("#ai_key").value };
  if ($("#aiv_base")) Object.assign(f.vision, { base: $("#aiv_base").value, model: $("#aiv_model").value, key: $("#aiv_key").value });
}
function aiVisionBody() {
  const x = AI.cfgForm.vision;
  return { mode: x.mode, wire: x.wire, baseUrl: x.base.trim(), model: x.model.trim() };
}
function aiCfgBody() {
  aiCfgKeep();
  const f = AI.cfgForm, v = f.vals[f.provider];
  return { provider: f.provider, baseUrl: v.base.trim(), model: v.model.trim(), key: v.key.trim() };
}
// the 看图 part of the service form: who looks at the screenshots the AI takes
const VISION_MODES = [["", "自动"], ["main", "主模型自己看"], ["other", "用看图模型"], ["off", "不给 AI 看"]];
function aiVisionHTML() {
  const c = AI.cfg, x = AI.cfgForm.vision, tail = c.visionKey;
  const say = {
    "": "AI 拍照检查头像时：上面的模型能看图就把截图发给它；看不了（例如 deepseek-v4-pro）就请一个看图模型把画面描述给它——同一家有看图模型时自动用它（同一个 Key，不用填），否则用下面填的。",
    main: "截图直接发给上面的模型。它其实看不了图的话，服务会拒绝，软件就改成只把截图显示给你。",
    other: "截图总是交给下面的看图模型，由它把画面描述成文字再告诉 AI。",
    off: "截图只显示在记录里给你看，不发给任何模型。",
  }[x.mode];
  const other = x.mode === "" || x.mode === "other";
  // a provider with a saved key at the same address: its key serves the vision model too
  const host = u => { try { return new URL(u).host.toLowerCase(); } catch (e) { return ""; } };
  const same = !tail && host(x.base) ? c.providers.find(p => (c.keys || {})[p.id] && host((c.profiles[p.id] || {}).baseUrl || p.base) === host(x.base)) : null;
  const preset = (c.visionPresets || []).find(p => host(p.baseUrl) === host(x.base) && host(x.base));
  return `<div class="visionbox"><div class="row"><label>看图</label>
      <div class="seg">${VISION_MODES.map(([id, label]) => `<button class="segbtn${id === x.mode ? " on" : ""}" data-ai="vmode" data-v="${id}" aria-pressed="${id === x.mode}">${label}</button>`).join("")}</div>
      <div class="hint2">${say}</div></div>
    ${other ? `<div class="row"><label>看图模型用哪家</label><div class="seg">${(c.visionPresets || []).map(p => `<button class="segbtn${preset && preset.id === p.id ? " on" : ""}" data-ai="vpreset" data-v="${p.id}">${esc(p.label)}</button>`).join("")}</div>
      <div class="hint2">点一下填好接口地址和模型名${preset ? "：" + esc(preset.note) : ""}。也可以在下面自己填别家的。</div></div>
    <div class="row"><label>看图模型的接口</label><div class="seg">${[["openai", "ChatGPT 兼容"], ["claude", "Claude 兼容"]].map(([id, label]) => `<button class="segbtn${id === x.wire ? " on" : ""}" data-ai="vwire" data-v="${id}" aria-pressed="${id === x.wire}">${label}</button>`).join("")}</div></div>
    <div class="row"><label for="aiv_base">看图模型的接口地址</label><input id="aiv_base" value="${esc(x.base)}" placeholder="例如 https://open.bigmodel.cn/api/paas/v4" autocomplete="off" spellcheck="false"></div>
    <div class="row"><label for="aiv_key">看图模型的 API Key${tail ? `<span class="muted small">　已保存（${esc(tail)}）</span>` : ""}</label>
      <div class="inline"><input id="aiv_key" type="password" value="${esc(x.key)}" placeholder="${tail ? "留空就继续用已保存的" : same ? "不填就用「" + esc(same.label) + "」的 Key（同一家）" : "粘贴 API Key"}" autocomplete="off" spellcheck="false">${tail ? `<button class="btn small" data-ai="vkeyclear">清除</button>` : ""}</div></div>
    <div class="row"><label for="aiv_model">看图模型的模型名</label><input id="aiv_model" value="${esc(x.model)}" placeholder="例如 glm-4.6v-flash、qwen-vl-plus、kimi-k3、gpt-4o" autocomplete="off" spellcheck="false">
      <div class="hint2">填任何能看图的模型，可以和上面不是同一家；模型名以服务商的文档为准。${x.mode === "" ? "上面的模型自己能看图、或者同一家有看图模型（DeepSeek、智谱、通义、Kimi 都有）时，这几项可以不填。" : ""}</div></div>` : `<input type="hidden" id="aiv_base" value="${esc(x.base)}"><input type="hidden" id="aiv_key" value="${esc(x.key)}"><input type="hidden" id="aiv_model" value="${esc(x.model)}">`}
    ${x.mode === "off" ? "" : `<div class="btnrow">${x.mode !== "other" ? `<button class="btn small" data-ai="vtest" data-who="main">测试上面的模型能不能看图</button>` : ""}${other ? `<button class="btn small" data-ai="vtest" data-who="other">测试看图模型</button>` : ""}</div>
    <div class="aitest" id="ai_vtest" role="status"></div>`}</div>`;
}
function aiCfgDone() {
  AI.cfgForm = null;
  closeModal();
  if (AI.back) { const p = AI.back; AI.back = null; return openAI(p); } // opened from a project's line: back to it, with the new settings
}
async function openAICfg(keep) {
  if (!keep) { try { await aiLoadCfg(); } catch (e) { toast("读不到 AI 设置"); return; } AI.cfgForm = null; }
  const c = AI.cfg;
  if (!AI.cfgForm) {
    const cv = c.vision || {};
    AI.cfgForm = { provider: c.provider || "deepseek", vals: {}, models: {}, vision: { mode: cv.mode || "", wire: cv.wire === "claude" ? "claude" : "openai", base: cv.baseUrl || "", model: cv.model || "", key: "" } };
  }
  const f = AI.cfgForm, info = c.providers.find(x => x.id === f.provider), saved = c.profiles[f.provider] || {};
  const v = f.vals[f.provider] || (f.vals[f.provider] = { base: saved.baseUrl || info.base, model: saved.model || "", key: "" });
  const tail = (c.keys || {})[f.provider];
  const m = $("#modal");
  clearTimeout(AI.timer); m.dataset.kind = "aicfg"; m.classList.remove("wide");
  m.innerHTML = `<div class="mhead">AI 服务</div><div class="mbody">
    <p class="lead">流水线用你自己的 AI 账号干活，费用由服务商按用量收。API Key 加密保存在这台电脑上，只会发给下面填的接口地址。</p>
    <div class="row"><label>服务商</label><div class="seg">${c.providers.map(p => `<button class="segbtn${p.id === f.provider ? " on" : ""}" data-aiprov="${p.id}" aria-pressed="${p.id === f.provider}">${esc(p.label)}</button>`).join("")}</div>
      <div class="hint2">${esc(info.note)}</div></div>
    <div class="row"><label for="ai_base">接口地址</label><input id="ai_base" value="${esc(v.base)}" placeholder="${esc(info.base)}" autocomplete="off" spellcheck="false"></div>
    <div class="row"><label for="ai_key">API Key${tail ? `<span class="muted small">　已保存（${esc(tail)}）</span>` : ""}</label>
      <div class="inline"><input id="ai_key" type="password" value="${esc(v.key)}" placeholder="${tail ? "留空就继续用已保存的" : "粘贴 API Key"}" autocomplete="off" spellcheck="false">${tail ? `<button class="btn small" data-ai="keyclear">清除</button>` : ""}</div></div>
    <div class="row"><label for="ai_model">模型</label>
      <div class="inline"><input id="ai_model" value="${esc(v.model)}" placeholder="${esc(info.model || "点「选择模型」挑一个，或直接填模型名")}" autocomplete="off" spellcheck="false" aria-controls="ai_modelist"><button class="btn small" data-ai="models" aria-expanded="false">选择模型 ▾</button></div>
      <div class="modelist" id="ai_modelist" hidden></div>
      <div class="hint2">点「选择模型」从服务商那里读出列表再点选，也可以直接填模型名。要选支持工具调用（function calling）的对话模型。</div></div>
    ${aiVisionHTML()}
    <div class="aitest" id="ai_test" role="status"></div>
  </div><div class="mfoot"><button class="btn" data-ai="test">测试连接</button><span class="spacer"></span><button class="btn ghost" data-ai="cfgclose">取消</button><button class="btn primary" data-ai="cfgsave">保存</button></div>`;
  m.classList.add("on"); $("#scrim").classList.add("on");
}

// ---------- events ----------
document.addEventListener("click", async e => {
  const t = e.target;
  if (t.matches("[data-shot]")) return showBig(t.dataset.shots.split("|").map(n => "/shot?f=" + encodeURIComponent(n)), +t.dataset.shot, true);
  if (t.closest("#btnSync") || t.closest("#btnDLLogin")) { startSync(); return; }
  if (t.closest("#btnGumSync")) return gumSync();
  if (t.closest("#btnGumCancel")) { await api("/api/gumroad/cancel", {}); toast("正在取消"); poll(true); return; }
  if (t.closest("#btnDLCancel")) { await api("/api/booth/download/cancel", {}); toast("已取消下载"); poll(true); return; }
  if (t.closest("#btnPanCancel")) { await api("/api/pan/download/cancel", {}); toast("已取消网盘下载"); poll(true); return; }
  if (t.closest("#btnBdLogin")) { baiduLogin(false); return; }
  if (t.closest("#btnDLMissing")) {
    const list = (S.data.assets || []).filter(a => a.virtual && matches(a));
    if (!confirm(`下载这 ${list.length} 件已购商品？会下载到 ${S.data.dlDir}。`)) return;
    S.dlAsked = Date.now();
    const r = await api("/api/booth/download/missing", { items: list.map(a => a.boothId) });
    toast(r.n ? `开始下载 ${r.n} 个文件` : "没有要下载的"); poll(true); return;
  }
  if (t.closest("#vLib")) { if (S.view === "lib" && S.web.open.lib) closeWeb(); else setView("lib"); return; }
  if (t.closest("#vShop")) { if (S.view === "shop" && S.web.open.shop) closeWeb(); else setView("shop"); return; }
  if (t.closest("#vXy")) { setView("xianyu"); return; }
  if (t.closest("#vProj")) { setView("proj"); return; }
  if (t.closest("#vPipe")) { setView("pipe"); return; }
  if (t.closest("#btnNewProj")) return openNewProject();
  const npb = t.closest("[data-np]");
  if (npb) return npAction(npb);
  const pp = t.closest("[data-pipeproj]");
  if (pp) { const p = pipeProject(pp.dataset.pipeproj); if (p) pipeSelect(p); return; }
  const pb = t.closest("[data-pipe]");
  if (pb) return pipeAction(pb);
  const stb = t.closest("[data-station]");
  if (stb) { const sec = $("#stn" + (stb.dataset.station === "4" ? "3" : stb.dataset.station)); if (sec) sec.scrollIntoView({ behavior: "smooth", block: "start" }); return; }
  if (!t.closest("#modal")) {
    const ab = t.closest("[data-ai]");
    if (ab) return aiAction(ab);
    const ub = t.closest("[data-url]");
    if (ub && !t.closest(".drawer")) return /^unityhub:/.test(ub.dataset.url) ? openURL(ub.dataset.url) : openLink(ub.dataset.url);
  }
  const wb = t.closest("[data-w]");
  if (wb) { webAction(wb.dataset.w); return; }
  const xl = t.closest("[data-xy]");
  if (xl) { openWeb(xl.dataset.xy === "home" ? XY_HOME : XY_BASE + "/" + xl.dataset.xy, "xianyu"); return; }
  const bl = t.closest("[data-bl]");
  if (bl) { if (bl.dataset.bl === "sync") startSync(); else openWeb(bl.dataset.bl === "cart" ? S.data.boothWeb + "/carts" : bl.dataset.bl === "login" ? S.data.boothWeb + "/users/sign_in" : bl.dataset.bl === "library" ? S.data.boothAccounts + "/library" : boothHome(), "booth"); return; }
  const sc = t.closest(".side [data-sc], .side [data-sb], .side [data-ss]");
  if (sc && S.view === "shop") {
    const [set, v] = sc.dataset.sc !== undefined ? [S.shop.cats, sc.dataset.sc] : sc.dataset.sb !== undefined ? [S.shop.bases, sc.dataset.sb] : [S.shop.styles, sc.dataset.ss];
    set.has(v) ? set.delete(v) : set.add(v);
    if (S.web.open.shop) closeWeb();
    saveUI(); renderShopSide(); shopSoon(); return;
  }
  if (t.closest("#shAllBases")) { S.shop.allBases = true; renderShopSide(); return; }
  if (t.closest("#shopClear")) { if (S.web.open.shop) closeWeb(); const sh = S.shop; sh.cats.clear(); sh.bases.clear(); sh.styles.clear(); sh.q = ""; $("#q").value = ""; saveUI(); renderShopSide(); shopSearch(true); return; }
  if (t.closest("#shopMore")) { S.shop.page++; shopSearch(false); return; }
  const scard = t.closest(".shopcard");
  if (scard && !t.closest(".drawer")) { openShopItem(scard.dataset.shop); return; }
  const sdr = t.closest(".drawer");
  if (sdr && S.shopOpen) {
    const it = (S.shop.detail || {}).item;
    if (t.matches(".bimgs img")) { const all = [...sdr.querySelectorAll(".bimgs img")].map(i => i.dataset.big); return showBig(all, all.indexOf(t.dataset.big)); }
    const dlb = t.closest("[data-dl],[data-gopen]");
    if (dlb) { if (dlb.dataset.gopen) return openPath(dlb.dataset.gopen); await startDownload(S.shopOpen, [dlb.dataset.dl]); return openShopItem(S.shopOpen); }
    const b = t.closest("[data-sd]"); if (!b) return;
    switch (b.dataset.sd) {
      case "close": return closeDrawer();
      case "booth": return openWeb(boothItemURL(S.shopOpen), "booth");
      case "lib": { const key = it && it.ownedKey; closeDrawer(); setView("lib"); if (key) renderDrawer(key); return; }
      case "dlall": await startDownload(S.shopOpen); return openShopItem(S.shopOpen);
      case "trans": {
        const k = "shop:" + S.shopOpen, dd = S.desc[k] = S.desc[k] || {};
        if (dd.zh) { dd.showZh = !dd.showZh; return renderShopDrawer(); }
        dd.loading = true; renderShopDrawer();
        const r = await api("/api/translate", { text: it.desc });
        dd.loading = false; if (r.ok) { dd.zh = r.text; dd.showZh = true; } else toast(r.err || "翻译失败", 3500);
        return renderShopDrawer();
      }
    }
    return;
  }
  if (t.closest("#btnSyncCancel")) { await api("/api/purchases/cancel", {}); toast("正在取消"); poll(true); return; }
  if (t.closest("#btnStyles")) { openSettings("s_styles"); return; }
  const fb = t.closest(".side [data-fold]");
  if (fb) { S.fold[fb.dataset.fold] = !S.fold[fb.dataset.fold]; saveUI(); renderSide(); return; }
  const side = t.closest(".side [data-cat], .side [data-usage], .side [data-share], .side [data-root], .side [data-base], .side [data-purchase], .side [data-style], .side [data-recent]");
  if (side) {
    const ds = side.dataset;
    if (S.web.open.lib) closeWeb();
    if (ds.recent !== undefined) S.recent = S.recent === ds.recent ? "" : ds.recent;
    // a second click on the one in use goes back up: a category to 全部, a style to its category
    const again = (cur, v) => cur === v ? "all" : v;
    if (ds.cat !== undefined) { S.style = ""; S.cat = S.cat === ds.cat ? "全部" : ds.cat; }
    if (ds.style !== undefined) S.style = S.style === ds.style ? "" : ds.style;
    if (ds.usage !== undefined) S.usage = again(S.usage, ds.usage);
    if (ds.share !== undefined) S.share = again(S.share, ds.share);
    if (ds.root !== undefined) S.root = again(S.root, ds.root);
    if (ds.purchase !== undefined) S.purchase = again(S.purchase, ds.purchase);
    if (ds.base !== undefined) { S.bases.has(ds.base) ? S.bases.delete(ds.base) : S.bases.add(ds.base); }
    renderSide(); renderGrid(); $(".main").scrollTop = 0; return;
  }
  if (t.closest("#clearFilters")) { S.q = ""; $("#q").value = ""; S.cat = "全部"; S.style = ""; S.recent = ""; S.bases.clear(); S.usage = S.share = S.root = S.purchase = "all"; renderSide(); renderGrid(); return; }
  if (t.closest("#emptySettings") || t.closest("#btnSettings")) { openSettings(); return; }
  if (t.closest("#btnPanAdd")) { openPanAdd(""); return; }
  if (t.closest("#btnUpdate")) { openUpdate(false); return; }
  if (t.closest("#btnVersion")) { openUpdate(true); return; }
  if (t.closest("#btnFeedback")) { openFeedback(); return; }
  if (t.closest("#btnAICfg")) { pipeKeep(); AI.back = S.view === "pipe" && AI.proj ? AI.proj.path : null; return openAICfg(); }
  const ad = t.closest("[data-aidress]");
  if (ad) { const j = S.data.importJob; return openAI(ad.dataset.aidress, j && j.project === ad.dataset.aidress ? j.tops || [] : []); }
  const pa = t.closest("[data-pa]");
  if (pa) return projAction(pa.dataset.pa, pa.closest("[data-proj]").dataset.proj, pa);
  if (t.closest("#btnProjAdd")) return addProject();
  if (t.closest("#btnProjUsage")) { await api("/api/scan", { scan: false, usage: true, booth: false }); toast("正在统计"); poll(true); return; }
  if (t.closest("#btnScan")) { const r = await api("/api/scan", { scan: true, usage: true, booth: S.data.settings.autoBooth }); toast(r.ok ? "开始扫描" : "正在扫描"); poll(true); return; }

  if (t.closest("#btnBulk")) { S.bulk = { keys: new Set(), last: null }; closeDrawer(); return renderGrid(); }
  if (t.closest("#bulkDone")) { S.bulk = null; return renderGrid(); }
  if (t.closest("#bulkAll")) { for (const u of bulkUnits()) S.bulk.keys.add(unitKey(u.primary)); return bulkRefresh(); }
  if (t.closest("#bulkNone")) { S.bulk.keys.clear(); S.bulk.last = null; return bulkRefresh(); }
  const bk = t.closest("[data-bulk]");
  if (bk && S.bulk) {
    const n = S.bulk.keys.size, what = bk.dataset.bulk;
    if (what === "hide") return bulkApply({ hidden: true }, () => `已隐藏 ${n} 个，左边勾「显示隐藏的」可以找回来`);
    if (what === "show") return bulkApply({ hidden: false }, () => `已取消隐藏 ${n} 个`);
    if (what === "ignore") {
      const local = bulkAssets().filter(isLocal).length;
      if (!local) { toast("选中的都不是本地文件夹里的素材"); return; }
      if (!confirm(`以后扫描时跳过这 ${local} 个素材所在的文件夹？文件不会被删除，可以在设置的「手动调整」里撤销。`)) return;
      return bulkApply({ ignore: true }, r => `${r.ignored} 个不再收录，正在重新扫描`);
    }
    return;
  }

  const card = t.closest(".card");
  if (card && S.bulk && !t.closest(".drawer") && card.dataset.key) {
    const a = findAsset(card.dataset.key); if (a) bulkToggle(a, e.shiftKey);
    return;
  }
  if (card && !t.closest(".drawer") && card.dataset.key) {
    const a = findAsset(card.dataset.key); if (!a) return;
    const act = t.closest("[data-act]");
    const what = act ? act.dataset.act : card.dataset.group ? "edit" : "open";
    if (what === "open") { if (a.virtual || a.panOnly) renderDrawer(a.key); else { const l = (a.locations || [])[0]; if (l) openPath(l.path); } }
    else if (what === "dl") startDownload(a.boothId);
    else if (what === "import") { renderDrawer(a.key); const sec = $("#impsec"); if (sec) { sec.scrollIntoView({ block: "start" }); $("#imp_proj").focus(); } }
    else if (what === "pandl") {
      if (!(S.data.baidu || {}).loggedIn) { renderDrawer(a.key); const sec = $("#pansec"); if (sec) sec.scrollIntoView({ block: "start" }); toast("先登录百度网盘"); }
      else startPanDL(a, false);
    }
    else if (what === "buypage") openBuyPage(a);
    else if (what === "pan") openPan(a);
    else if (what === "booth") goBooth(a);
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
    const stc = t.closest("[data-st]");
    if (stc) {
      S.stylePick = new Set(S.stylePick || a.styles || []);
      S.stylePick.has(stc.dataset.st) ? S.stylePick.delete(stc.dataset.st) : S.stylePick.add(stc.dataset.st);
      return renderDrawer(a.key, true);
    }
    const sn = t.closest("[data-seen]");
    if (sn) { await api("/api/seen", { key: a.key, what: sn.dataset.seen }); return load(); }
    const vb = t.closest("[data-vopen],[data-vgo],[data-psd]");
    if (vb) {
      if (vb.dataset.psd) return openPath(vb.dataset.psd);
      const m = findAsset(vb.dataset.vopen || vb.dataset.vgo); if (!m) return;
      if (vb.dataset.vgo) return renderDrawer(m.key);
      if (m.virtual) return openBuyPage(m);
      if (m.panOnly) return openPan(m);
      const l = (m.locations || [])[0]; if (l) openPath(l.path); return;
    }
    const dlb = t.closest("[data-dl],[data-gopen]");
    if (dlb) {
      if (dlb.dataset.gopen) return openPath(dlb.dataset.gopen);
      return startDownload(a.boothId || (a.purchase && a.purchase.id), [dlb.dataset.dl]);
    }
    const btn = t.closest("[data-d],[data-loc],[data-copy],[data-cover],[data-url],[data-link]");
    if (!btn) return;
    if (btn.dataset.url !== undefined) return openLink(btn.dataset.url);
    if (btn.dataset.link !== undefined) {
      const u = collectUser(a); u.boothUrl = boothItemURL(btn.dataset.link); u.noBooth = false; S.bs = null;
      return saveUser(a, u, "已关联");
    }
    if (btn.dataset.loc !== undefined) return openPath(a.locations[+btn.dataset.loc].path);
    if (btn.dataset.copy !== undefined) return copyText(a.locations[+btn.dataset.copy].path, "已复制路径");
    if (btn.dataset.cover !== undefined) { const on = btn.classList.contains("on"); dr.querySelectorAll(".covers img").forEach(i => i.classList.remove("on")); if (!on) btn.classList.add("on"); S.coverPick = on ? null : btn.dataset.cover; return; }
    switch (btn.dataset.d) {
      case "close": return closeDrawer();
      case "import": return startImport(a);
      case "impgo": {
        const cs = (S.data.importJob || {}).choices || [];
        const paths = [...dr.querySelectorAll("[data-ipick]")].filter(x => x.checked).map(x => (cs[+x.dataset.ipick] || {}).path).filter(Boolean);
        if (!paths.length) { toast("至少选一个"); return; }
        await api("/api/import/choose", { paths }); return poll(true);
      }
      case "impcancel": await api("/api/import/dismiss", {}); return poll(true);
      case "impdismiss": await api("/api/import/dismiss", {}); return load();
      case "dlimport": {
        const importTo = chosenProject(); if (!importTo) return;
        setProject(importTo); S.dlAsked = Date.now();
        const r = await api("/api/booth/download", { item: a.purchase.id, ids: [], importTo });
        if (!r.ok) { toast(r.err || "没能开始下载", 3500); return; }
        toast("开始下载，下载完会自动导入"); await load(); return poll(true);
      }
      case "pandl": return startPanDL(a, "", null);
      case "pandlimp": return startPanDL(a, true, null);
      case "pandlsel": return startPanDL(a, "", [...S.panSel]);
      case "pandlselimp": return startPanDL(a, true, [...S.panSel]);
      case "panselclear": S.panSel = new Set(); return renderDrawer(a.key, true);
      case "panretry": { const j = panJobFor(a) || {}; return startPanDL(a, j.project || "", j.paths || null); }
      case "pancancel": await api("/api/pan/download/cancel", {}); toast("已取消"); return poll(true);
      case "pandismiss": await api("/api/pan/download/dismiss", { key: (panJobFor(a) || {}).key || a.key }); return load();
      case "bdlogin": return baiduLogin(false);
      case "bdswitch": return baiduLogin(true);
      case "bdlogout": if (!confirm("退出百度网盘登录？")) return;
        await api("/api/baidu/logout", {}); toast("已退出百度网盘"); return load();
      case "open": { const l = (a.locations || [])[0]; if (l) openPath(l.path); return; }
      case "pan": return openPan(a);
      case "booth": return openLink(boothURL(a));
      case "buypage": return openBuyPage(a);
      case "dlall": return startDownload(a.purchase.id);
      case "panrefresh": await api("/api/pan/refresh", { key: a.key }); toast("重新读取中"); poll(true); return;
      case "pandelete": if (!confirm("从素材库移除？网盘里的文件不受影响。")) return;
        await api("/api/user", { key: a.key, delete: true }); closeDrawer(); toast("已移除"); load(); return;
      case "bsearch": return runBoothSearch(a, ($("#bs_q").value || "").trim());
      case "bopen": return openLink(boothSearchURL(($("#bs_q") || {}).value || a.boothQuery || a.name));
      case "bchange": return runBoothSearch(a, a.boothQuery || a.name);
      case "bconfirm": { const u = collectUser(a); u.boothUrl = boothItemURL(a.boothId); u.noBooth = false; return saveUser(a, u, "已确认"); }
      case "breject": { const u = collectUser(a); u.boothUrl = ""; u.noBooth = true; S.bs = { key: a.key, q: a.boothQuery || a.name, hits: (a.boothHits || []).filter(h => h.id !== a.boothId) }; return saveUser(a, u, "已取消匹配"); }
      case "bunlink": { const u = collectUser(a); u.boothUrl = ""; u.noBooth = true; return saveUser(a, u, "已取消关联"); }
      case "bnone": { const u = collectUser(a); u.noBooth = true; return saveUser(a, u, "已标记"); }
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
      case "save": return saveGroup(a, collectUser(a));
      case "stauto": { S.stylePick = null; const u = collectUser(a); u.styles = null; u.stylesSet = false; return saveGroup(a, u, "已恢复自动"); }
      case "nosplit": { const u = collectUser(a); u.noSplit = !!a.splitInto; await saveUser(a, u, u.noSplit ? "已合并显示" : "已拆开");
        if (u.noSplit) renderDrawer(a.key); return; }
      case "nogroup": { const u = collectUser(a); u.noGroup = !a.user.noGroup; return saveUser(a, u, u.noGroup ? "已单独显示" : "已允许合并同款"); }
      case "hide": { const u = collectUser(a); u.hidden = !a.user.hidden; return saveUser(a, u, u.hidden ? "已隐藏" : "已取消隐藏"); }
      case "rebooth": { await api("/api/booth", { keys: [a.key], force: true }); toast("重新读取中"); poll(true); return; }
      case "split": case "ignore": {
        const dirLoc = (a.locations || []).find(l => l.kind === "dir") || a.locations[0];
        const verb = btn.dataset.d === "split" ? "拆成多个素材" : "以后不再收录";
        if (!confirm(`把「${dirLoc.path}」${verb}？可以在设置里撤销。`)) return;
        await api("/api/override", { path: dirLoc.path, mode: btn.dataset.d });
        closeDrawer(); toast("正在重新扫描"); poll(true); return;
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
    const po = t.closest("[data-pickone]");
    if (po) { const p = await pickInto("选择 Booth 下载位置"); if (p) $("#" + po.dataset.pickone).value = p; return; }
    const pt = t.closest("[data-pickto]");
    if (pt) {
      const p = await pickInto(pt.dataset.pickto === "s_roots" ? "选择放素材的文件夹" : "选择 Unity 工程或工程所在的文件夹");
      if (p) { const ta = $("#" + pt.dataset.pickto); ta.value = (ta.value.trim() ? ta.value.trim() + "\n" : "") + p; }
      return;
    }
    const ov = t.closest("[data-ov]");
    if (ov) { await api("/api/override", { path: ov.dataset.ov, mode: "" }); toast("已撤销，正在重新扫描"); closeModal(); poll(true); return; }
    const fk = t.closest("[data-fbkind]");
    if (fk) { fbKeep(); S.fb.kind = fk.dataset.fbkind; renderFeedback(); return; }
    const ab = t.closest("[data-aimodel], [data-ai], [data-aiprov]");
    if (ab) return aiAction(ab);
    const b = t.closest("[data-m]"); if (!b) return;
    if (b.dataset.m === "close") return closeModal();
    if (b.dataset.m === "feedback") { closeModal(); return openFeedback(); }
    if (b.dataset.m === "aicfg") { AI.back = null; return openAICfg(); }
    if (b.dataset.m === "fbsend") return sendFeedback();
    if (b.dataset.m === "fbcopy") { fbKeep(); return copyText(fbPlain(), "已复制，粘贴到邮件正文里发送就行"); }
    if (b.dataset.m === "fbaddr") return copyText(fbMail(), "已复制邮箱地址");
    if (b.dataset.m === "fbmail") {
      fbKeep(); const f = S.fb;
      return openURL(`mailto:${fbMail()}?subject=${encodeURIComponent(`[${S.data.appName || "MioVRCA"} ${S.data.version}] ${f.kind}`)}&body=${encodeURIComponent(fbPlain().slice(0, 1800))}`);
    }
    if (b.dataset.m === "uppage") { openURL((S.upd && S.upd.info && S.upd.info.url) || (S.data.update && S.data.update.url) || S.data.releases); return; }
    if (b.dataset.m === "upskip") { await api("/api/update/skip", { version: (S.data.update || {}).version || "" }); closeModal(); toast("已跳过 " + ((S.data.update || {}).version || "")); load(); return; }
    if (b.dataset.m === "upgo") {
      const r = await api("/api/update/apply", {});
      if (!r.ok) { toast(r.err || "没能开始更新", 4000); return; }
      S.updWatch = true; poll(true); renderUpdateModal(); return;
    }
    if (b.dataset.m === "checkupd") { closeModal(); return openUpdate(true); }
    if (b.dataset.m === "allnotes") { closeModal(); return openUpdate(false); }
    if (b.dataset.m === "panadd") {
      const r0 = parseShareText($("#pa_text").value);
      if (!r0.url) { toast("没有识别到 pan.baidu.com/s/ 链接"); return; }
      const r = await api("/api/pan/add", { url: r0.url, pwd: $("#pa_pwd").value.trim() || r0.pwd || "", name: $("#pa_name").value.trim() });
      if (!r.ok) { toast(r.err || "没添加成功"); return; }
      const dl = $("#pa_dl") && $("#pa_dl").checked;
      closeModal(); toast("已添加"); await load(); renderDrawer(r.key); poll(true);
      if (dl) { const a = findAsset(r.key); if (a) startPanDL(a, false); }
      return;
    }
    if (b.dataset.m === "sync") { closeModal(); return startSync(); }
    if (b.dataset.m === "bdlogin" || b.dataset.m === "bdswitch") { closeModal(); closeDrawer(); return baiduLogin(b.dataset.m === "bdswitch"); }
    if (b.dataset.m === "bdlogout") {
      if (!confirm("退出百度网盘登录？")) return;
      await api("/api/baidu/logout", {}); toast("已退出百度网盘"); await load(); return openSettings("s_baidu");
    }
    if (b.dataset.m === "shortcut") { const r = await api("/api/shortcut", {}); toast(r.ok ? "已创建" : "创建失败：" + (r.err || ""), 3500); return; }
    if (b.dataset.m === "gumsync") { closeModal(); return gumSync(); }
    if (b.dataset.m === "gumlogout" || b.dataset.m === "gumclear") {
      const clear = b.dataset.m === "gumclear";
      if (!confirm(clear ? "清空 Gumroad 已购记录并退出登录？已经下载的素材不受影响。" : "退出 Gumroad 登录？已读到的已购记录会留着。")) return;
      const r = await api("/api/gumroad/logout", { clear });
      toast(r.ok ? (clear ? "已清空并退出" : "已退出 Gumroad") : r.err || "没能退出", 3500); await load(); return openSettings();
    }
    if (b.dataset.m === "forget" || b.dataset.m === "clearp") {
      const clear = b.dataset.m === "clearp";
      if (!confirm(clear ? "清空已购记录并退出 Booth 登录？本地素材不受影响。" : "退出 Booth 登录？")) return;
      const r = await api("/api/purchases/forget", { clear });
      toast(r.ok ? (clear ? "已清空" : "已退出 Booth 登录") : (r.err || "没成功"));
      closeModal(); load(); return;
    }
    if (b.dataset.m === "save") {
      const lines = id => $(id).value.split(/\r?\n/).map(s => s.trim()).filter(Boolean);
      const s = Object.assign({}, S.data.settings, { roots: lines("#s_roots"), projectRoots: lines("#s_proj"), proxy: $("#s_proxy").value.trim(),
        autoBooth: $("#s_auto").checked, manualRescan: !$("#s_rescan").checked, bases: lines("#s_bases"),
        hideZh: !$("#s_zh").checked, noAutoMatch: !$("#s_match").checked, windowMode: $("#s_win").value, styles: lines("#s_styles"),
        noUpdateCheck: !$("#s_upd").checked, noWatch: !$("#s_watch").checked, noSync: !$("#s_sync").checked,
        downloadDir: $("#s_dldir").value.trim(), noExtract: !$("#s_extract").checked, keepZip: $("#s_keepzip").checked });
      const o = S.data.settings, same = (x, y) => JSON.stringify(x || []) === JSON.stringify(y || []);
      const noRescan = same(s.roots, o.roots) && same(s.projectRoots, o.projectRoots) && same(s.bases, o.bases) && s.autoBooth === o.autoBooth;
      await api("/api/settings", { settings: s, noRescan }); closeModal(); toast(noRescan ? "已保存" : "已保存，正在重新扫描"); noRescan ? load() : poll(true);
    }
    return;
  }
  if (t.closest("#scrim")) { closeModal(); closeDrawer(); }
});
document.addEventListener("change", e => {
  if (e.target.dataset && e.target.dataset.psel !== undefined && S.openKey) {
    const a = findAsset(S.openKey), p = e.target.dataset.psel;
    if (a && a.pan) { togglePanSel(a, p); renderDrawer(a.key, true); const el = document.querySelector(`[data-psel="${CSS.escape(p)}"]`); if (el) el.focus(); }
    return;
  }
  if (e.target.id === "imp_proj" || e.target.id === "pan_proj") { S.dirty.delete(e.target.id); if (e.target.value === "__pick") pickProject(e.target); else if (e.target.value) setProject(e.target.value); return; }
  if (e.target.id === "imp_recycle") { S.impRecycle = e.target.checked; return; }
  if (e.target.dataset && e.target.dataset.ppick !== undefined) { PIPE.pick[e.target.dataset.ppick] = e.target.checked; const tr = e.target.closest("tr"); if (tr) tr.classList.toggle("on", e.target.checked); aiRenderLive(); return; }
  if (e.target.dataset && e.target.dataset.pkind !== undefined) { PIPE.kind[e.target.dataset.pkind] = e.target.value; const tr = e.target.closest("tr"); if (tr) tr.classList.toggle("dim", e.target.value === "其他"); aiRenderLive(); return; }
  if (e.target.id === "pipe_noai") { PIPE.noAI = e.target.checked; aiRenderLive(); return; }
  if (e.target.id === "pipe_hier") { PIPE.hier = e.target.value; return; }
  if (e.target.dataset && e.target.dataset.sk && S.setup) { S.setup[e.target.dataset.sk][+e.target.dataset.si].checked = e.target.checked; return; }
  if (e.target.id === "showHidden") { S.showHidden = e.target.checked; renderSide(); renderGrid(); }
  if (e.target.id === "bulkCat" && S.bulk && e.target.value) {
    const v = e.target.value, n = S.bulk.keys.size;
    return bulkApply({ category: v === "__auto" ? "" : v }, () => v === "__auto" ? `${n} 个已恢复自动分类` : `已把 ${n} 个移到「${v}」`);
  }
  if (e.target.id === "sort") { S.sort = e.target.value; saveUI(); renderGrid(); }
  if (e.target.id === "shopSort") { S.shop.sort = e.target.value; saveUI(); shopSearch(true); }
  if (e.target.dataset && e.target.dataset.shf) { S.shop[e.target.dataset.shf] = e.target.checked; saveUI(); if (e.target.dataset.shf === "adult") shopSearch(true); else renderShopGrid(); }
});
document.addEventListener("toggle", e => {
  const d = e.target;
  if (!(d instanceof HTMLDetailsElement)) return;
  if (d.dataset.pp) { if (d.open) { S.panOpen.add(d.dataset.pp); S.panClosed.delete(d.dataset.pp); } else { S.panOpen.delete(d.dataset.pp); S.panClosed.add(d.dataset.pp); } }
  if (d.dataset.desc) { const x = S.desc[d.dataset.desc] = S.desc[d.dataset.desc] || {}; x.open = d.open; }
}, true);
document.addEventListener("keydown", e => {
  if (e.key === "Enter" && e.target.id === "bs_q" && S.openKey) { const a = findAsset(S.openKey); if (a) runBoothSearch(a, e.target.value.trim()); }
  if (e.key === "Enter" && e.target.id === "f_newstyle" && S.openKey) { e.preventDefault(); addStyle(e.target.value); S.dirty.delete("f_newstyle"); }
});
document.addEventListener("keydown", e => {
  if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && e.target.id === "ai_text") { e.preventDefault(); aiSend(); }
});
// typing in the model field narrows the list, when the service's models have been read
document.addEventListener("input", e => { if (e.target.id === "ai_model") aiModelList(true); });
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
    else if (e.key === "ArrowLeft" || e.key === "ArrowRight") showBig(S.lb.list, S.lb.i + (e.key === "ArrowLeft" ? -1 : 1), S.lb.direct);
    return;
  }
  if (e.key === "Escape") {
    if ($("#modal").classList.contains("on")) closeModal();
    else if ($("#drawer").classList.contains("on")) closeDrawer();
    else if (S.bulk && S.view === "lib") { S.bulk = null; renderGrid(); }
    else if (S.view === "shop" && S.web.open.shop) closeWeb();
  }
  if (e.key === "Enter" && document.activeElement.classList.contains("shopcard")) { openShopItem(document.activeElement.dataset.shop); return; }
  if (e.key === "Enter" && document.activeElement.classList.contains("card")) {
    const a = findAsset(document.activeElement.dataset.key); if (!a) return;
    if (S.bulk) return bulkToggle(a, e.shiftKey);
    if (document.activeElement.dataset.group) { renderDrawer(a.key); return; }
    if (a.virtual || a.panOnly) renderDrawer(a.key); else if ((a.locations || [])[0]) openPath(a.locations[0].path);
  }
});
let qTimer;
// remember which drawer fields the user has edited, so background refreshes don't overwrite them
["input", "change"].forEach(ev => $("#drawer").addEventListener(ev, e => { if (e.target.id && e.target.matches("input, textarea, select")) S.dirty.add(e.target.id); }));
$("#q").addEventListener("input", e => {
  clearTimeout(qTimer);
  if (S.view === "xianyu") { S.web.xq = e.target.value; return; }
  if (S.view === "proj") { qTimer = setTimeout(() => { S.pq = e.target.value.trim(); renderProjGrid(); }, 120); return; }
  if (S.view === "pipe") { qTimer = setTimeout(() => { S.pq = e.target.value.trim(); renderPipeSide(); }, 120); return; }
  if (S.view === "shop") { qTimer = setTimeout(() => { S.shop.q = e.target.value.trim(); shopSearch(true); }, 700); return; }
  qTimer = setTimeout(() => { S.q = e.target.value.trim(); renderSide(); renderGrid(); }, 120);
});
$("#q").addEventListener("keydown", e => {
  if (e.key !== "Enter") return;
  if (S.view === "shop") { clearTimeout(qTimer); if (S.web.open.shop) closeWeb(); S.shop.q = e.target.value.trim(); shopSearch(true); }
  if (S.view === "xianyu") { const q = e.target.value.trim(); S.web.xq = q; saveUI(); openWeb(q ? XY_BASE + "/search?q=" + encodeURIComponent(q) : XY_HOME, "xianyu"); }
});
$("#shopSort").value = S.shop.sort;
$("#sort").value = S.sort;

// ---------- polling ----------
let pollTimer;
async function poll(fast) {
  clearTimeout(pollTimer);
  try {
    const p = await api("/api/progress");
    if (S.data) {
      const flip = !!S.data.purchaseBusy !== !!p.purchaseBusy;
      S.data.tasks = p.tasks; S.data.busy = p.busy; S.data.purchaseBusy = p.purchaseBusy; S.data.dlNeedLogin = p.dlNeedLogin; renderStatus();
      if (p.downloads && JSON.stringify(p.downloads) !== JSON.stringify(S.data.downloads || [])) refreshDL(p.downloads);
      if (JSON.stringify([p.importJob || null, p.panJobs || []]) !== JSON.stringify([S.data.importJob || null, S.data.panJobs || []])) refreshJobs(p.importJob, p.panJobs);
      if (JSON.stringify(p.newProject || null) !== JSON.stringify(S.data.newProject || null)) { S.data.newProject = p.newProject || null; renderNPLive(); }
      if (flip && S.view === "lib") renderSide();
    }
    if (p.rev !== S.rev) await load();
    announceUpdate();
    if ((S.view === "proj" || S.view === "pipe") && !document.hidden) loadProjects();
    const dling = (p.tasks || []).some(t => ["download", "pandl", "import", "newproject"].includes(t.name) && t.running);
    pollTimer = setTimeout(poll, p.busy || p.purchaseBusy || fast || S.updWatch || dling ? 1000 : 5000);
  } catch (e) {
    if (S.updWatch) { S.updRestart = true; if ($("#modal").dataset.kind === "update") renderUpdateModal(); }
    pollTimer = setTimeout(poll, S.updWatch ? 1500 : 4000);
  }
}
setInterval(() => fetch("/api/ping").catch(() => {}), 15000);
load().then(() => {
  aiLoadCfg().catch(() => {});
  if (S.data.setupNeeded) openSetup();
  else if (S.data.paneMode && webVisible()) ensurePaneFor(S.view);
  if (S.view === "shop" && !S.shop.started) shopSearch(true); // the app was closed on the Booth tab
  if (S.view === "pipe") pipeEnter();
  poll();
});
