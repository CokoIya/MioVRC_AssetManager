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
  upd: '<svg viewBox="0 0 24 24"><path d="M20 12a8 8 0 1 1-2.34-5.66"/><path d="M20 4v5h-5"/></svg>',
};

const S = {
  data: null, rev: 0,
  q: "", cat: "全部", style: "", recent: "", bases: new Set(), usage: "all", share: "all", root: "all", purchase: "all", showHidden: false, sort: "recent",
  openKey: null, setup: null, bs: null, desc: {}, dirty: new Set(), coverPick: null, stylePick: null, groups: new Map(), panOpen: new Set(), panClosed: new Set(),
};
try { const saved = JSON.parse(localStorage.getItem("vrclib.ui") || "{}"); if (saved.sort) S.sort = saved.sort; } catch (e) {}

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
  S.data = d; S.rev = d.rev;
  S.groups = new Map();
  for (const a of d.assets || []) if (a.group) { if (!S.groups.has(a.group)) S.groups.set(a.group, []); S.groups.get(a.group).push(a); }
  renderAll();
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
function renderAll() { renderSide(); renderGrid(); renderStatus(); if (S.openKey) renderDrawer(S.openKey, true); }

function navItem(label, count, on, attrs, color) {
  return `<button class="navitem${on ? " on" : ""}" ${attrs}>${color ? `<span class="dot" style="background:${color}"></span>` : ""}<span>${esc(label)}</span><span class="n">${count}</span></button>`;
}

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
  h += `<h3>分类</h3>` + navItem("全部", countUnits(base), S.cat === "全部", `data-cat="全部"`);
  for (const c of d.categories) if (cc[c]) {
    h += navItem(c, cc[c], S.cat === c, `data-cat="${esc(c)}"`, CAT_COLOR[c]);
    if (c === "衣服" && S.cat === "衣服") {
      const sb = all.filter(a => matches(a, "style"));
      const sc = tally(sb, a => (a.styles || []).length ? a.styles : ["__none"]);
      let sub = "";
      for (const st of styleList()) if (sc[st]) sub += `<button class="subitem${S.style === st ? " on" : ""}" data-style="${esc(st)}"><span>${esc(st)}</span><span class="n">${sc[st]}</span></button>`;
      if (sc.__none) sub += `<button class="subitem none${S.style === "__none" ? " on" : ""}" data-style="__none"><span>未加标签</span><span class="n">${sc.__none}</span></button>`;
      h += `<div class="subnav">${sub}<button class="subitem edit" id="btnStyles">编辑标签…</button></div>`;
    }
  }

  const bc = tally(all.filter(a => matches(a, "bases")), a => a.bases || []);
  const bl = Object.keys(bc).sort((x, y) => bc[y] - bc[x]);
  if (bl.length) h += `<h3>适配素体</h3><div class="chips">` + bl.map(b => `<button class="chip${S.bases.has(b) ? " on" : ""}" data-base="${esc(b)}">${esc(b)}<span class="n">${bc[b]}</span></button>`).join("") + `</div>`;

  const pb = all.filter(a => matches(a, "purchase"));
  const nU = f => countUnits(pb.filter(f));
  const syncing = d.purchaseBusy;
  if (!d.purchaseSync) {
    h += `<h3>Booth 已购</h3><div class="cta">
      ${syncing ? `<button class="btn small" id="btnSyncCancel">取消同步</button>` : `<button class="btn small" id="btnSync">同步 Booth 已购</button>`}</div>`;
  } else {
    const local = nU(a => a.purchase && !a.virtual), miss = nU(a => a.virtual);
    const other = nU(a => !a.purchase && !a.virtual), localAll = nU(a => !a.virtual), bought = nU(a => a.purchase);
    h += `<h3 class="withact">Booth 已购${syncing ? `<button class="h3act" id="btnSyncCancel">取消同步</button>` : `<button class="h3act" id="btnSync" title="上次同步：${esc(fmtTime(d.purchaseSync))}">重新同步</button>`}</h3>` +
      navItem("本地全部", localAll, S.purchase === "all", `data-purchase="all"`) +
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
          <button class="act booth${a.boothId ? "" : " nobooth"}" data-act="booth" title="${a.boothId ? "打开 Booth 商品页" : "在 Booth 上搜"}">${a.boothId ? ICON.bag : ICON.search}</button>
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
  const tip = a.virtual ? "打开下载页" : a.panOnly ? "打开网盘分享（自动复制提取码）" : "打开文件夹";
  const zh = !S.data.settings.hideZh && a.nameZh ? `<div class="zh" title="${esc(a.nameZh)}">${esc(a.nameZh)}</div>` : "";
  return `<article class="card${a.hidden ? " hiddenasset" : ""}${a.virtual ? " virtual" : ""}${a.panOnly ? " panonly" : ""}" data-key="${esc(a.key)}" tabindex="0" title="${tip}">
    <div class="cover">${coverHTML(a)}${boughtBadge(a)}${a.panOnly ? `<span class="badge-pan">网盘</span>` : ""}${usedBadge(a)}
      <div class="where"><div class="path" title="${esc(loc ? loc.path : where)}">${esc(where)}</div>
        <div class="acts">
          ${a.virtual ? `<button class="act buy" data-act="buypage" title="下载页">${ICON.download}<span>下载页</span></button>`
            : a.panOnly ? "" : `<button class="act" data-act="open" title="打开文件夹">${ICON.folder}</button>`}
          ${pan ? `<button class="act pan" data-act="pan" title="打开网盘">${ICON.cloud}${a.panOnly ? "<span>网盘</span>" : ""}</button>` : ""}
          ${p && !a.virtual ? `<button class="act buy" data-act="buypage" title="购买页">${ICON.receipt}</button>` : ""}
          <button class="act booth${a.boothId ? "" : " nobooth"}" data-act="booth" title="${a.boothId ? "打开 Booth 商品页" : "在 Booth 上搜「" + esc(a.boothQuery || a.name) + "」"}">${a.boothId ? ICON.bag : ICON.search}</button>
          <button class="act edit" data-act="edit" title="详情">${ICON.edit}<span>详情</span></button>
        </div></div>
    </div>
    <div class="body">
      <div class="title">${a.new ? `<span class="newtag">新</span>` : ""}${esc(a.name)}</div>${zh}
      <div class="sub">${esc(a.panOnly && !shop ? "百度网盘" : subline)}</div>
      ${(a.bases || []).length || (a.styles || []).length ? `<div class="bases">${(a.bases || []).slice(0, 4).map(b => `<span class="base">${esc(b)}</span>`).join("")}${(a.bases || []).length > 4 ? `<span class="base more">+${a.bases.length - 4}</span>` : ""}${styleTags(a)}</div>` : ""}
      <div class="meta"><span class="cat"><i style="background:${CAT_COLOR[a.category] || "#8b8fa6"}"></i>${esc(a.category)}</span>${a.psd ? `<span class="psdtag" title="仅 PSD">PSD</span>` : ""}${groupAll(a).length > 1 ? `<button class="vcount link" data-act="edit">共 ${groupAll(a).length} 个版本</button>` : ""}<span>${a.virtual ? "未下载" : a.panOnly && !a.size ? "—" : fmtSize(a.size)}</span>
        <span class="flags">${hasNews(a) ? `<span class="flag upd" title="${esc(newsTitle(a))}">${ICON.upd}</span>` : ""}${pan ? `<span class="flag pan" title="网盘">${ICON.cloud}</span>` : ""}${a.boothId ? `<span class="flag booth" title="Booth #${esc(a.boothId)}">${ICON.bag}</span>` : ""}</span></div>
    </div></article>`;
}

function renderGrid() {
  const d = S.data; if (!d) return;
  const list = sorted(unitsOf((d.assets || []).filter(a => matches(a))));
  const total = countUnits((d.assets || []).filter(a => (S.showHidden || !a.hidden) && !a.virtual));
  const filtered = S.q || S.cat !== "全部" || S.style || S.recent || S.bases.size || S.usage !== "all" || S.share !== "all" || S.root !== "all" || S.purchase !== "all";
  let rb = `<b>${list.length}</b><span>个${S.purchase === "missing" || S.purchase === "bought" ? "商品" : "素材"}${filtered ? `（共 ${total} 个）` : ""}</span>`;
  if (filtered) rb += `<button class="clear" id="clearFilters">清除筛选</button>`;
  for (const w of (d.warnings || [])) rb += `<span class="warnline">${esc(w)}</span>`;
  $("#resultbar").innerHTML = rb;
  const g = $("#grid");
  if (!list.length) {
    const scanning = d.busy;
    g.innerHTML = `<div class="empty">${!(d.assets || []).some(a => !a.virtual) && S.purchase === "all"
      ? (scanning ? `<h2>正在扫描</h2><p>第一次会久一点。</p>`
        : `<h2>还没有素材</h2><p>先在设置里添加素材文件夹。</p><button class="btn primary" id="emptySettings">打开设置</button>`)
      : `<h2>没有符合条件的素材</h2>`}</div>`;
    return;
  }
  g.innerHTML = list.map(u => u.members.length > 1 ? groupCardHTML(u) : cardHTML(u.primary)).join("");
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
    const pt = (d.tasks || []).find(t => t.name === "purchase");
    if (pt && pt.msg && pt.ended && Date.now() / 1000 - pt.ended < 600) h += `<span class="${pt.msg.startsWith("完成") ? "okline" : "err"}">Booth 已购：${esc(pt.msg)}</span>`;
  }
  h += `<span class="right"><button class="verbtn" id="btnFeedback">反馈和建议</button><button class="verbtn" id="btnVersion" title="更新公告">v${esc(d.version)}</button></span>`;
  $("#status").innerHTML = h;
  renderUpdateBtn();
  $("#btnScan").disabled = !!d.busy;
  $("#btnScan").textContent = d.busy ? "正在扫描…" : "重新扫描";
}

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
  if (a.panPath) return "网盘：" + a.panPath.split("/").filter(Boolean).slice(-3).join(" / ");
  if (!l) return S.data.tasks && (S.data.tasks.find(t => t.name === "pan") || {}).running ? "读取中…" : "百度网盘分享";
  if (l.err && !l.files) return "网盘：" + l.err;
  return `百度网盘，${l.count} 个文件，${fmtSize(l.size)}`;
}
function openBuyPage(a) { if (a.purchase) openURL(a.purchase.pageUrl); }
async function startSync() {
  const r = await api("/api/purchases/sync", {});
  toast(r.ok ? "已打开 Booth 窗口，登录后自动读取" : "正在同步", 4000);
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
function panTree(files, depth, prefix, parts) {
  const few = (files || []).length <= 3;
  return (files || []).map(f => {
    const path = prefix + "/" + f.n;
    const tag = parts ? partTag(parts.get(path)) : "";
    if (!f.d) return `<div class="pf"><span class="nm">${esc(f.n)}</span>${tag ? `<span class="tags">${tag}</span>` : ""}${extTag(f.n)}<span class="s">${fmtSize(f.s)}</span></div>`;
    const open = S.panOpen.has(path) || (depth === 0 && few && !S.panClosed.has(path));
    return `<details data-pp="${esc(path)}"${open ? " open" : ""}><summary>${ICON.folder}<span class="nm">${esc(f.n)}</span>${tag ? `<span class="tags">${tag}</span>` : ""}${f.p ? `<span class="muted small">（未列全）</span>` : ""}</summary>
      <div class="kids">${panTree(f.c, depth + 1, path, parts)}</div></details>`;
  }).join("");
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
  let list = "";
  if (u.shareUrl) {
    if (!l) list = `<div class="muted small panmsg">${running ? "读取中…" : "保存后读取文件列表"}</div>`;
    else if (l.err && !(l.files || []).length) list = `<div class="small err panmsg">读取失败：${esc(l.err)}</div>`;
    else list = `<div class="panhead"><b>${l.count} 个文件</b><span>${fmtSize(l.size)}</span>${l.truncated ? `<span class="muted small">只列出了一部分</span>` : ""}<span class="muted small right">读取于 ${esc(fmtTime(l.fetched))}</span></div>
      ${panSummary(a)}
      <div class="pantree">${panTree(l.files, 0, "", new Map((a.panParts || []).map(p => [p.path, p])))}</div>${l.err ? `<div class="small err">重新读取失败：${esc(l.err)}</div>` : ""}`;
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
    <div class="btnrow"><button class="btn small" data-d="buypage">打开下载页</button></div></div>`;
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
    <button class="btn small" data-url="${esc(o.url)}">打开订单页</button></div>`).join("");
  const files = (p.files || []).filter(Boolean);
  return `<div class="sec"><h4>Booth 已购${p.gift ? "（礼物）" : ""}</h4>
    ${orders || `<div class="muted small">没有订单号</div>`}
    <div class="order"><span></span><button class="btn small" data-url="${esc(p.libraryUrl)}">已购列表</button></div>
    ${files.length ? `<div class="files">${files.map(f => `<span class="file">${esc(f)}</span>`).join("")}</div>` : ""}
    ${p.matched ? `<div class="hint" style="margin-top:6px">按文件名对应</div>` : ""}
  </div>`;
}

function renderDrawer(key, keepScroll) {
  const a = findAsset(key); if (!a) { closeDrawer(); return; }
  if (S.openKey !== key) { S.panOpen = new Set(); S.panClosed = new Set(); S.dirty = new Set(); S.coverPick = null; S.stylePick = null; }
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
        ${a.virtual ? `<button class="btn primary" data-d="buypage">${ICON.download}<span style="margin-left:6px">打开下载页</span></button>`
          : a.panOnly ? `<button class="btn primary" data-d="pan">${ICON.cloud}<span style="margin-left:6px">打开网盘分享</span></button>`
          : `<button class="btn primary" data-d="open">${ICON.folder}<span style="margin-left:6px">打开文件夹</span></button>`}
        ${hasShare(a) && !a.panOnly ? `<button class="btn" data-d="pan">打开网盘</button>` : ""}
        ${a.purchase && !a.virtual ? `<button class="btn" data-d="buypage">购买页</button>` : ""}
        <button class="btn" data-d="booth">${a.boothId ? "Booth 商品页" : "在 Booth 搜索"}</button>
      </div>
    </div>
  </div>
  <div class="dbody">
    ${newsSec(a)}
    ${variantSec(a)}
    ${a.panOnly ? "" : a.virtual ? `<div class="sec"><h4>保存位置</h4><div class="muted small">还没下载。放进素材文件夹后会自动对上。</div></div>`
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
      ${a.virtual ? `<input id="f_booth" type="hidden" value="">` : `<div class="field"><label>Booth 链接</label><input id="f_booth" value="${esc(u.boothUrl || "")}" placeholder="${a.boothId ? "已识别 #" + esc(a.boothId) : "https://booth.pm/ja/items/..."}"></div>`}
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
        ${a.boothId ? `<button class="btn" data-d="rebooth">重新读取 Booth</button>` : ""}
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
      ${info.notes ? `<div class="upnotes">${mdLite(info.notes)}</div>` : ""}
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
    <div class="row"><label>代理</label><input id="s_proxy" value="${esc(s.proxy || "")}" placeholder="留空使用系统代理，例如 127.0.0.1:7890"></div>
    <div class="row"><label>素体识别表（显示名=别名1|别名2）</label><textarea id="s_bases" style="min-height:120px">${esc((s.bases || []).join("\n"))}</textarea></div>
    <div class="row"><label>风格标签（标签名=关键词1|关键词2）</label><textarea id="s_styles" style="min-height:140px">${esc((s.styles || []).join("\n"))}</textarea></div>
    ${Object.keys(ov).length ? `<div class="row"><label>手动调整</label>${Object.entries(ov).map(([p, v]) => `<div class="ov"><span>${esc(p)}</span><span class="muted" style="flex:none">${{ split: "拆成多个", ignore: "不收录", asset: "合并为一个" }[v] || v}</span><button class="btn" data-ov="${esc(p)}">撤销</button></div>`).join("")}</div>` : ""}
    <div class="row"><label>Booth 已购${S.data.purchaseSync ? `<span class="muted small">　已同步 ${S.data.purchaseCount} 件（${esc(fmtTime(S.data.purchaseSync))}）</span>` : ""}</label>
      <div class="btnrow" style="margin-top:0"><button class="btn small" data-m="sync">同步</button><button class="btn small" data-m="forget">退出登录</button>${S.data.purchaseSync ? `<button class="btn small" data-m="clearp">清空记录</button>` : ""}</div></div>
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
  if (["update", "feedback", "whatsnew"].includes($("#modal").dataset.kind)) $("#modal").dataset.kind = "";
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
function openPanAdd() {
  const m = $("#modal");
  m.innerHTML = `<div class="mhead">添加网盘素材</div><div class="mbody">
    <div class="row"><label>分享链接或分享文字</label><textarea id="pa_text" placeholder="链接: https://pan.baidu.com/s/1xxxx 提取码: abcd"></textarea></div>
    <div class="row"><label>提取码</label><input id="pa_pwd" placeholder="可不填"></div>
    <div class="row"><label>名称</label><input id="pa_name" placeholder="可不填"></div>
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

// ---------- events ----------
document.addEventListener("click", async e => {
  const t = e.target;
  if (t.closest("#btnSync")) { startSync(); return; }
  if (t.closest("#btnSyncCancel")) { await api("/api/purchases/cancel", {}); toast("正在取消"); poll(true); return; }
  if (t.closest("#btnStyles")) { openSettings("s_styles"); return; }
  const side = t.closest(".side [data-cat], .side [data-usage], .side [data-share], .side [data-root], .side [data-base], .side [data-purchase], .side [data-style], .side [data-recent]");
  if (side) {
    const ds = side.dataset;
    if (ds.recent !== undefined) S.recent = S.recent === ds.recent ? "" : ds.recent;
    if (ds.cat !== undefined) { if (S.cat !== ds.cat) S.style = ""; S.cat = ds.cat; }
    if (ds.style !== undefined) S.style = S.style === ds.style ? "" : ds.style;
    if (ds.usage !== undefined) S.usage = ds.usage;
    if (ds.share !== undefined) S.share = ds.share;
    if (ds.root !== undefined) S.root = ds.root;
    if (ds.purchase !== undefined) S.purchase = ds.purchase;
    if (ds.base !== undefined) { S.bases.has(ds.base) ? S.bases.delete(ds.base) : S.bases.add(ds.base); }
    renderSide(); renderGrid(); $(".main").scrollTop = 0; return;
  }
  if (t.closest("#clearFilters")) { S.q = ""; $("#q").value = ""; S.cat = "全部"; S.style = ""; S.recent = ""; S.bases.clear(); S.usage = S.share = S.root = S.purchase = "all"; renderSide(); renderGrid(); return; }
  if (t.closest("#emptySettings") || t.closest("#btnSettings")) { openSettings(); return; }
  if (t.closest("#btnPanAdd")) { openPanAdd(); return; }
  if (t.closest("#btnUpdate")) { openUpdate(false); return; }
  if (t.closest("#btnVersion")) { openUpdate(true); return; }
  if (t.closest("#btnFeedback")) { openFeedback(); return; }
  if (t.closest("#btnScan")) { const r = await api("/api/scan", { scan: true, usage: true, booth: S.data.settings.autoBooth }); toast(r.ok ? "开始扫描" : "正在扫描"); poll(true); return; }

  const card = t.closest(".card");
  if (card && !t.closest(".drawer")) {
    const a = findAsset(card.dataset.key); if (!a) return;
    const act = t.closest("[data-act]");
    const what = act ? act.dataset.act : card.dataset.group ? "edit" : "open";
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
    const btn = t.closest("[data-d],[data-loc],[data-copy],[data-cover],[data-url],[data-link]");
    if (!btn) return;
    if (btn.dataset.url !== undefined) return openURL(btn.dataset.url);
    if (btn.dataset.link !== undefined) {
      const u = collectUser(a); u.boothUrl = boothItemURL(btn.dataset.link); u.noBooth = false; S.bs = null;
      return saveUser(a, u, "已关联");
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
      case "panrefresh": await api("/api/pan/refresh", { key: a.key }); toast("重新读取中"); poll(true); return;
      case "pandelete": if (!confirm("从素材库移除？网盘里的文件不受影响。")) return;
        await api("/api/user", { key: a.key, delete: true }); closeDrawer(); toast("已移除"); load(); return;
      case "bsearch": return runBoothSearch(a, ($("#bs_q").value || "").trim());
      case "bopen": return openURL(boothSearchURL(($("#bs_q") || {}).value || a.boothQuery || a.name));
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
    const b = t.closest("[data-m]"); if (!b) return;
    if (b.dataset.m === "close") return closeModal();
    if (b.dataset.m === "feedback") { closeModal(); return openFeedback(); }
    if (b.dataset.m === "fbsend") return sendFeedback();
    if (b.dataset.m === "fbcopy") { fbKeep(); return copyText(fbPlain(), "已复制，粘贴到邮件正文里发送就行"); }
    if (b.dataset.m === "fbaddr") return copyText(fbMail(), "已复制邮箱地址");
    if (b.dataset.m === "fbmail") {
      fbKeep(); const f = S.fb;
      return openURL(`mailto:${fbMail()}?subject=${encodeURIComponent(`[VRC素材库 ${S.data.version}] ${f.kind}`)}&body=${encodeURIComponent(fbPlain().slice(0, 1800))}`);
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
      closeModal(); toast("已添加"); await load(); renderDrawer(r.key); poll(true); return;
    }
    if (b.dataset.m === "sync") { closeModal(); return startSync(); }
    if (b.dataset.m === "shortcut") { const r = await api("/api/shortcut", {}); toast(r.ok ? "已创建" : "创建失败：" + (r.err || ""), 3500); return; }
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
        noUpdateCheck: !$("#s_upd").checked, noWatch: !$("#s_watch").checked, noSync: !$("#s_sync").checked });
      const o = S.data.settings, same = (x, y) => JSON.stringify(x || []) === JSON.stringify(y || []);
      const noRescan = same(s.roots, o.roots) && same(s.projectRoots, o.projectRoots) && same(s.bases, o.bases) && s.autoBooth === o.autoBooth;
      await api("/api/settings", { settings: s, noRescan }); closeModal(); toast(noRescan ? "已保存" : "已保存，正在重新扫描"); noRescan ? load() : poll(true);
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
  if (e.key === "Enter" && e.target.id === "f_newstyle" && S.openKey) { e.preventDefault(); addStyle(e.target.value); S.dirty.delete("f_newstyle"); }
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
    if (document.activeElement.dataset.group) { renderDrawer(a.key); return; }
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
    pollTimer = setTimeout(poll, p.busy || p.purchaseBusy || fast || S.updWatch ? 1200 : 5000);
  } catch (e) {
    if (S.updWatch) { S.updRestart = true; if ($("#modal").dataset.kind === "update") renderUpdateModal(); }
    pollTimer = setTimeout(poll, S.updWatch ? 1500 : 4000);
  }
}
setInterval(() => fetch("/api/ping").catch(() => {}), 15000);
load().then(() => { if (S.data.setupNeeded) openSetup(); poll(); });
