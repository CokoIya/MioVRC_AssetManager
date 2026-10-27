// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// The shell of the community in a browser: what the program's page (app.js) gives web/social.js, here without the
// program. The community's routes go to the service through adapter.js; what only the program can do (the library,
// the modding basket, projects, the wish list) is not offered, and its few entry points answer that it is the
// program's. Loaded before social.js; web-start.js, after it, routes the address and draws the page.
"use strict";

const $ = (s, el = document) => el.querySelector(s);
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const ICON = {
  back: '<svg viewBox="0 0 24 24"><path d="M15 5l-7 7 7 7"/></svg>',
  upd: '<svg viewBox="0 0 24 24"><path d="M20 12a8 8 0 1 1-2.34-5.66"/><path d="M20 4v5h-5"/></svg>',
  x: '<svg viewBox="0 0 24 24"><path d="M6 6l12 12M18 6 6 18"/></svg>',
  plus: '<svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg>',
  play: '<svg viewBox="0 0 24 24"><path d="M7 5v14l11-7z"/></svg>',
  search: '<svg viewBox="0 0 24 24"><circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/></svg>',
  link: '<svg viewBox="0 0 24 24"><path d="M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1 1"/><path d="M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1-1"/></svg>',
  flag: '<svg viewBox="0 0 24 24"><path d="M5 21V4M5 4h11l-2 4 2 4H5"/></svg>',
  like: '<svg viewBox="0 0 24 24"><path d="M7 10v10H4V10z"/><path d="M7 10l4-7a2 2 0 0 1 3 2l-1 5h6a2 2 0 0 1 2 2.3l-1.2 6A2 2 0 0 1 17.8 20H7"/></svg>',
  creators: '<svg viewBox="0 0 24 24"><circle cx="9" cy="8" r="3.5"/><path d="M3 20a6 6 0 0 1 12 0"/><path d="M16 4.5a3.5 3.5 0 0 1 0 7M21 20a6 6 0 0 0-3.5-5.4"/></svg>',
  ext: '<svg viewBox="0 0 24 24"><path d="M14 4h6v6M20 4l-9 9"/><path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/></svg>',
  download: '<svg viewBox="0 0 24 24"><path d="M12 4v11m0 0-4-4m4 4 4-4"/><path d="M5 19h14"/></svg>',
  menu: '<svg viewBox="0 0 24 24"><path d="M4 7h16M4 12h16M4 17h16"/></svg>',
};
// where the program and the author are
const WEB_LINKS = { site: "https://miovrc.com/vrca/", discord: "https://discord.gg/CD8NVJwBA", group: "https://qm.qq.com/q/9wErKBClA4" };

// the program's state, as much of it as the community reads: always the community, no library, no projects
const S = { view: "social", data: { assets: [], settings: {} }, projs: [], modalBack: null };
const AI = { proj: null };
let GRID = null;

// ---------- drawing ----------
function put(el, html) { if (!el) return false; if (el._h === html) return false; el._h = html; el.innerHTML = html; return true; }
function gridPut(html) { GRID = null; $("#grid").innerHTML = html; }
// the control inside el that has the focus, found again after el is redrawn (app.js focusMark)
function focusMark(el) {
  const ae = document.activeElement; let sel = "", s = null, e2 = null;
  if (!el || !ae || ae === el || !el.contains(ae)) return () => {};
  if (ae.id) sel = "#" + CSS.escape(ae.id);
  else for (const at of ae.attributes) if (at.name.startsWith("data-")) sel += `[${at.name}="${CSS.escape(at.value)}"]`;
  if (!sel) return () => {};
  try { s = ae.selectionStart; e2 = ae.selectionEnd; } catch (e) {}
  return () => {
    const n = el.querySelector(sel); if (!n || n === document.activeElement) return;
    n.focus({ preventScroll: true }); try { if (s !== null) n.setSelectionRange(s, e2); } catch (e) {}
  };
}
function fmtTime(t) {
  if (!t) return "从未";
  const d = new Date(t * 1000);
  return `${d.getMonth() + 1}月${d.getDate()}日 ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}
function toast(msg, ms = 2200, onClick) {
  const t = $("#toast"); t.textContent = msg; t.classList.add("on");
  t.classList.toggle("click", !!onClick);
  t.onclick = onClick ? () => { t.classList.remove("on"); onClick(); } : null;
  clearTimeout(toast._t); toast._t = setTimeout(() => t.classList.remove("on"), ms);
}
window.addEventListener("unhandledrejection", e => {
  const msg = (e.reason && e.reason.message) || String(e.reason || "");
  if (msg) toast("操作失败：" + msg, 4500);
});

// ---------- dialogs ----------
// Every dialog is drawn into #modal and shown through here (app.js showModal / closeModal). On a phone a dialog
// fills the screen, and the browser's back closes it (web-start.js keeps one history entry for it).
function showModal() {
  const m = $("#modal"), h = $(".mhead", m);
  const opening = !m.classList.contains("on");
  if (opening) S.modalBack = document.activeElement;
  m.classList.add("on"); m.setAttribute("aria-hidden", "false"); m.setAttribute("aria-label", h ? h.textContent.trim() : "对话框");
  scrimSync();
  if (!m.contains(document.activeElement)) m.focus({ preventScroll: true });
  if (opening) document.dispatchEvent(new Event("webmodalopen"));
}
function closeModal() {
  const m = $("#modal"), was = m.classList.contains("on");
  m.classList.remove("on"); m.setAttribute("aria-hidden", "true"); scrimSync();
  const back = S.modalBack; S.modalBack = null;
  if (was && back && back.isConnected && back !== document.body) back.focus({ preventScroll: true });
  if (was) document.dispatchEvent(new Event("webmodalclose"));
}
// on a phone a dialog fills the screen: its head gets a way back (web.css shows it there, and leaves out a foot that
// only closes)
new MutationObserver(() => {
  const h = $("#modal > .mhead");
  if (h && !h.querySelector('[data-m="close"], .wmback')) h.insertAdjacentHTML("afterbegin", `<button class="wb wmback" data-m="close" title="返回" aria-label="返回">${ICON.back}</button>`);
}).observe($("#modal"), { childList: true });
function scrimSync() {
  const on = $("#modal").classList.contains("on") || document.body.classList.contains("wsideon");
  $("#scrim").classList.toggle("on", on);
}
function closeDrawer() { document.body.classList.remove("wsideon"); const b = $("#wme"); if (b) b.setAttribute("aria-expanded", "false"); scrimSync(); }
document.addEventListener("click", e => {
  const b = e.target.closest("[data-m]");
  if (b && b.dataset.m === "close") { closeModal(); return; }
  if (e.target.id === "scrim") { if ($("#modal").classList.contains("on")) closeModal(); else closeDrawer(); }
});
// Enter in a field presses its dialog's button (the community adds its own: app.js ENTER_GOES); Tab stays in a
// dialog; Escape closes it, or the drawer
const ENTER_GOES = {};
document.addEventListener("keydown", e => {
  const modal = $("#modal").classList.contains("on");
  if (e.key === "Enter" && !e.isComposing && e.keyCode !== 229) {
    const go = ENTER_GOES[e.target.id];
    if (go) { e.preventDefault(); const b = $(go); if (b) b.click(); return; }
  }
  if (e.key === "Tab" && modal) {
    const m = $("#modal"), f = [...m.querySelectorAll("a[href], button, input, select, textarea, summary, [tabindex]")].filter(x => !x.disabled && x.tabIndex >= 0 && x.offsetParent !== null);
    const i = f.indexOf(document.activeElement);
    if (!f.length) { e.preventDefault(); m.focus(); }
    else if (e.shiftKey ? i <= 0 : i < 0 || i === f.length - 1) { e.preventDefault(); f[e.shiftKey ? f.length - 1 : 0].focus(); }
  }
  if (e.key !== "Escape") return;
  if (modal) { e.preventDefault(); closeModal(); }
  else if (document.body.classList.contains("wsideon")) closeDrawer();
});

// ---------- dropping pictures ----------
// Pictures dragged onto the page while a composer is on screen go into it wherever they are dropped (the program's
// drop.js): the one in the dialog when it is open, else the first in sight. Other files are refused, as the browser
// would otherwise leave the page to show them. A drop on a composer is its own (social.js).
function dropFiles(e) { return !!e.dataTransfer && [...e.dataTransfer.types].includes("Files") && !e.target.closest?.("[data-composer]"); }
function dropComposer(e) {
  const items = [...(e.dataTransfer?.items || [])].filter(i => i.kind === "file");
  if (!items.length || !items.every(i => /^image\//.test(i.type))) return null;
  return $("#modal.on [data-composer]") || (!$("#modal.on") && $("#grid [data-composer]")) || null;
}
document.addEventListener("dragover", e => {
  if (!dropFiles(e)) return;
  e.preventDefault(); e.dataTransfer.dropEffect = dropComposer(e) ? "copy" : "none";
});
document.addEventListener("drop", e => {
  if (!dropFiles(e)) return;
  e.preventDefault();
  const c = dropComposer(e);
  if (c) soComposeAdd(e.dataTransfer.files, c.dataset.composer);
});

// ---------- leaving the page ----------
function openURL(u) { try { window.open(u, "_blank", "noopener"); } catch (e) { toast("无法打开链接"); } }
function openWeb(u) { openURL(u); }
const CLIP = { own: "" };
function clipWrite(s) { CLIP.own = s; return navigator.clipboard.writeText(s); }
async function copyText(s, msg) {
  try { await clipWrite(s); toast(msg || "已复制"); }
  catch (e) {
    const ta = document.createElement("textarea"); ta.value = s; ta.setAttribute("readonly", ""); ta.style.position = "fixed"; ta.style.opacity = "0";
    document.body.append(ta); ta.select();
    try { document.execCommand("copy"); toast(msg || "已复制"); } catch (er) { toast("无法复制"); }
    ta.remove();
  }
}

// ---------- the program's routes ----------
// The community's go to the service (adapter.js); the few others the community calls are the program's own.
async function api(path, body) {
  if (path.startsWith("/api/community/")) {
    const route = path.slice("/api/community/".length), key = webKeepKey(route, body || {});
    let r;
    try { r = await WebCommunity.call(route, body || {}); }
    catch (e) { r = { ok: false, err: "无法连接社区服务", unavailable: true }; }
    if (r.ok && key) webKeep(key, r);
    else if (r.unavailable && key) { const kept = webKept(key); if (kept) { webOffline(true); return { ...kept, stale: true }; } }
    if (r.unavailable) webOffline(true); else if (r.ok && key) webOffline(false); // (a read of the service, not one this page answers itself)
    if (r.ok && (route === "account/logout" || route === "account/delete") || r.accountInvalid) webKeepClear();
    return r;
  }
  if (path === "/api/app-link/take") return { ok: true };
  if (path === "/api/openurl") { openURL(body && body.url); return { ok: true }; }
  return { ok: false, err: "请在 MioVRCA 中使用", desktopOnly: true };
}
// 快速打开: the last answer of each read the page makes when it opens stays in this browser (per account, the first
// page only), so the next start shows it at once (web-start.js) and a page without a connection still shows what was
// last read. An answer from the keep carries stale: true. Signing out forgets it all.
const WEB_KEEP = ["service", "stats", "unread", "timeline", "notices", "dm", "trends", "themes", "thread", "groups", "works", "asks", "settings", "profile/posts", "handle/profile", "group", "group/posts"];
function webKeepKey(route, body) {
  if (!WEB_KEEP.includes(route) || body.after || body.before || body.next || body.more) return "";
  let uid = "";
  try { uid = JSON.parse(localStorage.getItem("miovrca.web.session") || "null")?.id || ""; } catch (e) {}
  const args = Object.keys(body).filter(k => k !== "refresh").sort().map(k => k + "=" + JSON.stringify(body[k])).join("&");
  return "miovrca.web.keep:" + uid + ":" + route + (args ? "?" + args : "");
}
function webKeep(key, r) {
  try { const s = JSON.stringify(r); if (s.length < 300000) localStorage.setItem(key, s); } catch (e) {} // (full: the page works without it)
}
function webKept(key) {
  try { const s = localStorage.getItem(key); return s ? JSON.parse(s) : null; } catch (e) { return null; }
}
function webKeepClear() {
  try { for (const k of Object.keys(localStorage)) if (k.startsWith("miovrca.web.keep:")) localStorage.removeItem(k); } catch (e) {}
}
// without a connection: a line under the top says what is shown
function webOffline(on) {
  const bar = $("#woffline"); if (!bar || bar.hidden === !on) return;
  bar.hidden = !on;
}
window.addEventListener("online", () => { if (typeof soNeed === "function") soNeed(true); });
function setView() { S.view = "social"; }
function renderAll() { if (typeof soDraw === "function") soDraw(); }

// ---------- what only the program has ----------
// The library, the shops followed, the wish list, the netdisk, the recipes and projects: none in a browser. The
// community asks for them only behind controls the web page leaves out; the answers here keep it quiet if it does.
function findAsset() { return null; }
function shopOf() { return ""; }
function isGum() { return false; }
function gumPageURL() { return ""; }
function boothLinkURL(id) { return "https://booth.pm/ja/items/" + encodeURIComponent(String(id || "")); }
function followHas() { return true; } // (no 「关注店铺」 offer: shops are followed in the program)
async function followAdd() { webOnlyInApp(); return false; }
async function wishAdd() { webOnlyInApp(); return false; }
function openShopItem(id) { openURL(boothLinkURL(id)); }
function openPanAdd() { webOnlyInApp(); }
async function pickInto() { return ""; }
async function loadProjects() { S.projs = []; }
function projsToPick() { return []; }
function recipeShow() { webOnlyInApp(); }

// 「这需要 MioVRCA」: what the program does with a work (做同款, 照着装) or an asset, offered as the program itself
function webOnlyInApp(work) {
  const m = $("#modal"); m.dataset.kind = "webapp";
  const open = work ? `<a class="btn primary" href="miovrca://work/${encodeURIComponent(work)}">在 MioVRCA 中打开</a>` : "";
  m.innerHTML = `<div class="mhead sxmhead"><span>MioVRCA</span><button class="wb sxmclose" data-m="close" title="关闭" aria-label="关闭">${ICON.x}</button></div>
    <div class="mbody webapp"><p>这需要在电脑上的 MioVRCA 中完成</p></div>
    <div class="mfoot">${open}<button class="btn${open ? "" : " primary"}" data-web="download">下载 MioVRCA</button></div>`;
  showModal();
}
document.addEventListener("click", e => {
  const b = e.target.closest("[data-web]"); if (!b) return;
  if (b.dataset.web === "download") openURL(WEB_LINKS.site);
});
