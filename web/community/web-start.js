// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// The community in a browser, once social.js is loaded:
// - the page around it: the side bar (a drawer on a phone), the top bar and the tabs at the bottom on a phone, and
//   on a wide screen a column with search, the language and the download;
// - the address: #/ (首页), #/explore, #/notices, #/messages, #/bookmarks, #/me, #/more, #/groups, #/asks, #/works,
//   #/group/<id>, #/search/<words>, #/u/<account>, #/post/<id>, #/work/<id>. The browser's back and forward move
//   between them; a dialog takes one step of history, so on a phone back closes it;
// - a note that the page can be put on the home screen (a phone, not yet installed, not dismissed).
// The page is always the same document: everything it loads sits beside it, and the address only changes after #.
"use strict";

const WEB_NAVS = ["home", "explore", "notices", "messages", "bookmarks", "me", "more", "groups", "asks", "works", "search", "group", "edit"];
const WEB_TAB_ICON = {
  home: SO_ICON.home, explore: SO_ICON.explore, notices: SO_ICON.notices, messages: SO_ICON.messages,
  me: '<svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="4"/><path d="M4.5 20.5a7.5 7.5 0 0 1 15 0"/></svg>',
};
const R = { applying: false, modal: false, skip: 0, last: "", closing: 0 };

// ---------- the page around the community ----------
function webFrameDraw() {
  const a = SO.service?.account, n = SO.unread || {};
  const active = SO.page?.data?.me || SO.nav === "edit" ? "me" : ["works", "search", "groups", "group", "asks"].includes(SO.nav) ? "explore" : SO.nav;
  const tab = (k, t) => {
    const badge = k === "notices" ? n.notices : k === "messages" ? n.messages : 0;
    return `<button class="wtab${active === k && !SO.page ? " on" : ""}" data-so="nav" data-nav="${k}" aria-label="${t}"${active === k ? ' aria-current="page"' : ""}>${WEB_TAB_ICON[k]}${badge ? `<span class="wbadge">${esc(badge > 99 ? "99+" : badge)}</span>` : ""}</button>`;
  };
  put($("#wtabs"), tab("home", "首页") + tab("explore", "探索") + tab("notices", "通知") + tab("messages", "私信") + tab("me", "个人主页"));
  put($("#wme"), a ? soAv(SO.service?.accountPic, a.nickname, "sm") : `<span class="wmenu">${ICON.menu}</span>`);
  $("#wfab").innerHTML = SO_TOOL.pen;
  const scan = $("#wscan"); scan.hidden = !(a && soCanScan()); put(scan, `<span class="soi" data-i="scan" aria-hidden="true">${SO_UI.scan}</span>`);
  webBadge(n);
  $("#wfab").hidden = !a || !["home", "explore", "search", "group", "groups", "asks", "me"].includes(SO.nav) && !SO.page;
  document.body.classList.toggle("wout", !a);
  const join = $("#wjoin"); join.hidden = !!a || !SO.service?.available;
  if (!join.hidden && !join.firstChild) put(join, `<div class="wjoinin"><div class="wjointext"><b>别错过 VRChat 玩家的新鲜事</b><span>新作品、素材和 Booth 情报都在这里</span></div>
    <div class="wjoinbtns"><button class="btn wjoinlogin" data-so="acctopen" data-mode="login">登录</button><button class="btn wjoinsignup" data-so="acctopen" data-mode="signup">注册</button></div></div>`);
  put($("#wright"), webRightHTML());
  webSideExtras();
}
// the number on the icon of the page added to the home screen (notices and messages not yet read)
function webBadge(n) {
  const k = (n.notices || 0) + (n.messages || 0) + (n.requests || 0);
  if (webBadge.last === k || !navigator.setAppBadge) return;
  webBadge.last = k;
  (k ? navigator.setAppBadge(k) : navigator.clearAppBadge()).catch(() => {});
}
// around the side bar's own (social.js renderSocialSide): the name on top; below, the language, the look and the download
function webSideExtras() {
  const side = $("#side");
  if (!side || side.querySelector(".wsidefoot")) return;
  side.insertAdjacentHTML("afterbegin", `<button class="wsidebrand" data-so="nav" data-nav="home" aria-label="MioVRCA"><img src="icon.svg" alt=""><span data-i18n="off">MioVRCA</span></button>`);
  side.insertAdjacentHTML("beforeend", `<div class="wsidefoot">${window.langRowHTML ? langRowHTML() : ""}<div class="wsiderow"><button class="btn small ghost wtheme" data-theme-toggle aria-label="深色 / 浅色"><svg class="ico-dark" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z"/></svg><svg class="ico-light" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg></button><button class="btn small ghost" data-web="download">${ICON.download}<span>下载 MioVRCA</span></button></div></div>`);
  side.querySelector('.wsidefoot label[for="i18n_lang"]')?.insertAdjacentHTML("afterbegin", `<span class="soi" data-i="globe" aria-hidden="true">${SO_UI.globe}</span>`);
}
function webRightHTML() {
  return `<form class="sxsearch" id="w_search_form" role="search"><span class="sxsearchicon" aria-hidden="true">${ICON.search}</span><input type="search" id="w_search" value="${esc(SO.nav === "search" ? SO.q : "")}" placeholder="搜索帖子、作品、创作者" aria-label="搜索帖子、作品、创作者" enterkeyhint="search"></form>
    ${SO.service?.account || !SO.service?.available ? "" : `<div class="wcard wnew"><h3>第一次来？</h3><p>注册后就能发帖、关注创作者和收藏作品。</p>${soOAuthButtons(true)}<button class="btn primary soaccbig" data-so="acctopen" data-mode="signup" data-step="email">创建账号</button></div>`}
    ${webTrendsHTML()}<div class="wcard"><button class="btn primary wdl" data-web="download">${ICON.download}<span>下载 MioVRCA</span></button></div>
    <div class="wcard wlinks"><button class="linkbtn" data-web="discord"><span class="soi" data-i="chat" aria-hidden="true">${SO_UI.chat}</span>Discord</button><button class="linkbtn" data-web="group"><span class="soi" data-i="creators" aria-hidden="true">${SO_ICON.creators}</span>QQ 交流群</button><a class="linkbtn" href="${I18N.lang === "en" || I18N.lang === "ja" ? "/booth" : "/zh/booth"}"><span class="soi" data-i="trend" aria-hidden="true">${SO_UI.trend}</span>Booth 情报一览</a><button class="linkbtn" data-so="privacy"><span class="soi" data-i="shield" aria-hidden="true">${SO_UI.shield}</span>隐私政策</button></div>`;
}
// 趋势 beside the timeline (探索 has them all): the first five, the maintainer's pinned topics first (social.js soExploreDraw)
function webTrendsHTML() {
  const list = SO.trends?.ok ? (SO.trends.trends || []).slice(0, 5) : [];
  if (!list.length || SO.nav === "explore") return "";
  return `<div class="wcard wtrends"><h3>趋势</h3>${list.map(t => `<button class="wtrend" data-so="posttag" data-tag="${esc(t.tag)}">${t.pinned ? `<small>${SO_TOOL.pin}<span>推荐</span></small>` : ""}<b data-i18n="off">${esc(t.title || "#" + t.tag)}</b><span>${esc(`${t.posts} 篇帖子`)}</span></button>`).join("")}</div>`;
}
document.addEventListener("submit", e => {
  if (e.target.id !== "w_search_form") return;
  e.preventDefault();
  const q = $("#w_search").value.trim(); if (!q) return;
  SO.q = q; SO.nav = "search"; SO.page = null; closeDrawer();
  soSearchLoad(true);
});
document.addEventListener("click", e => {
  const w = e.target.closest("[data-web]");
  if (w && w.dataset.web === "discord") return openURL(WEB_LINKS.discord);
  if (w && w.dataset.web === "group") return openURL(WEB_LINKS.group);
  if (e.target.closest("#wme")) {
    const on = !document.body.classList.contains("wsideon");
    document.body.classList.toggle("wsideon", on); $("#wme").setAttribute("aria-expanded", String(on)); scrimSync();
    return;
  }
  // a choice in the drawer closes it
  if (document.body.classList.contains("wsideon") && e.target.closest("#side [data-so]")) closeDrawer();
});
document.addEventListener("socialunread", () => webFrameDraw());

// ---------- the address ----------
function webRoute() {
  const m = $("#modal");
  if (m.classList.contains("on")) {
    if (m.dataset.kind === "sopost" && SO.post?.id) return "#/post/" + SO.post.id;
    if (m.dataset.kind === "sowork" && SO.work?.id) return "#/work/" + SO.work.id;
  }
  if (SO.page) { const s = SO.page.src || {}; if (s.id) return "#/u/" + s.id; if (s.github) return "#/gh/" + encodeURIComponent(s.github); }
  if (SO.nav === "search" && SO.q) return "#/search/" + encodeURIComponent(SO.q);
  if (SO.nav === "group" && SO.group?.id) return "#/group/" + SO.group.id;
  return SO.nav === "home" ? "#/" : "#/" + SO.nav;
}
// after every draw: a new place is a new step of history (a dialog's own step is taken when it opens)
function webSync() {
  if (R.applying) return;
  const route = webRoute();
  if (route === R.last) return;
  const sameDialog = R.modal && $("#modal").classList.contains("on");
  if (sameDialog) history.replaceState({ web: 1, modal: 1 }, "", route);
  else { history.pushState({ web: 1 }, "", route); window.scrollTo(0, 0); }
  R.last = route;
}
function webApply(hash) {
  const qr = /^#\/qr\/([A-Za-z0-9_-]{16,64})$/.exec(hash || "");
  if (qr) { history.replaceState({ web: 1 }, "", "#/"); webApply("#/"); soQROpen(qr[1]); return; } // (once: a reload does not scan again)
  const back = /^#\/oauth\/([A-Za-z0-9_-]{16,64})$/.exec(hash || "");
  if (back) { history.replaceState({ web: 1 }, "", "#/"); webApply("#/"); soOAuthReturn(back[1]); return; } // (back from Google or Discord: adapter.js kept the key)
  const m = /^#\/(post|work|u|gh|search|group)\/(.+)$/.exec(hash || ""), n = /^#\/?([a-z]*)$/.exec(hash || "");
  const dm = /^#\/messages\/(requests|u[0-9a-z]{12})$/.exec(hash || ""); // (a message's notice: its conversation, or the requests)
  R.applying = true;
  try {
    if ($("#modal").classList.contains("on")) closeModal();
    closeDrawer(); soPopClose();
    if (m) {
      const kind = m[1], v = decodeURIComponent(m[2]);
      if (kind === "post" && /^[pcmg][0-9]{1,18}$/.test(v)) soPostOpen(v);
      else if (kind === "work" && /^(?:w[0-9]{1,9}|m[1-9][0-9]{0,17})$/.test(v)) soWorkOpen(v);
      else if (kind === "u" && /^u[0-9a-z]{12}$/.test(v)) soProfile({ id: v });
      else if (kind === "gh" && /^[A-Za-z0-9-]{1,39}$/.test(v)) soProfile({ github: v });
      else if (kind === "search") { SO.q = v.slice(0, 200); SO.nav = "search"; SO.page = null; soSearchLoad(true); }
      else if (kind === "group" && /^g[0-9a-z]{1,24}$/i.test(v)) soGroupOpen(v);
      else webNav("home");
    } else if (dm) {
      webNav("messages");
      if (dm[1] === "requests") { soDMState().box = "requests"; soDMLoad(); } else webDMOpen(dm[1]);
    } else webNav(n && WEB_NAVS.includes(n[1]) ? n[1] : "home");
  } finally {
    R.applying = false;
  }
  R.last = webRoute();
  webFrameDraw();
}
// which provider a sign-in that comes back was started with (adapter.js keeps it with the key, for this tab)
function webOAuthProvider(id) { try { return JSON.parse(sessionStorage.getItem("miovrca.oauth." + id) || "{}").provider || ""; } catch (_) { return ""; } }
// (the conversation's list fills in who it is with: social.js soDMLoad)
function webDMOpen(id) { soDMStart({ id, handle: "", nickname: "" }); }
function webNav(nav) {
  SO.asset = null; SO.page = null; SO.q = "";
  SO.nav = nav === "search" ? "explore" : nav;
  soNeed(false); soDraw(); window.scrollTo(0, 0);
}
window.addEventListener("popstate", () => {
  if (R.skip) { R.skip--; return; }
  if ($("#modal").classList.contains("on") && R.modal) {
    // back with a dialog open: the dialog closes, the page under it stays
    R.modal = false; R.applying = true;
    try { closeModal(); } finally { R.applying = false; }
    R.last = location.hash || "#/";
    if (R.last !== webRoute()) webApply(location.hash);
    return;
  }
  webApply(location.hash);
});
document.addEventListener("webmodalopen", () => {
  if (R.applying) { R.modal = false; return; }
  R.modal = true; R.last = webRoute();
  // a dialog closed and another opened in one go (登录 → 扫一扫): the new one takes over the old one's step
  if (R.closing) { clearTimeout(R.closing); R.closing = 0; history.replaceState({ web: 1, modal: 1 }, "", R.last); return; }
  history.pushState({ web: 1, modal: 1 }, "", R.last);
});
document.addEventListener("webmodalclose", () => {
  if (R.applying || !R.modal) { R.modal = false; return; }
  // closed on the page: its step of history goes too (a moment later: a dialog opened at once keeps it)
  R.modal = false;
  R.closing = setTimeout(() => {
    R.closing = 0; R.skip++; history.back();
    setTimeout(() => { R.last = webRoute(); webFrameDraw(); }, 0);
  }, 0);
});

// every draw of the community also redraws the page around it and keeps the address
const webDrawSocial = renderSocialMain;
renderSocialMain = function () { const x = $(".sxfilters")?.scrollLeft || 0; webDrawSocial(); webFiltersKeep(x); webFrameDraw(); webSync(); };
// 为你推荐's filters scroll sideways on a phone: a draw keeps where the row was, and the one chosen in sight
function webFiltersKeep(x) {
  const row = $(".sxfilters"), on = row?.querySelector(".sxfilter.on");
  if (!row || row.scrollWidth <= row.clientWidth) return;
  row.scrollLeft = x;
  if (!on) return;
  const r = row.getBoundingClientRect(), c = on.getBoundingClientRect();
  if (c.left < r.left || c.right > r.right) row.scrollLeft += c.left - r.left - (r.width - c.width) / 2;
}

// ---------- 添加到主屏幕 ----------
function webHomeTip() {
  const box = $("#whome"); if (!box) return;
  let dismissed = false;
  try { dismissed = localStorage.getItem("miovrca.web.hometip") === "1"; } catch (e) {}
  const standalone = (window.matchMedia && matchMedia("(display-mode: standalone)").matches) || navigator.standalone === true;
  const phone = /Android|iPhone|iPad|iPod/i.test(navigator.userAgent) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
  if (dismissed || standalone || !phone) { box.hidden = true; return; }
  const ios = /iPhone|iPad|iPod/i.test(navigator.userAgent) || navigator.platform === "MacIntel";
  const inApp = /MicroMessenger|QQ\/|Weibo|DingTalk/i.test(navigator.userAgent);
  const text = inApp ? "想放在桌面上？先在浏览器中打开，再点「分享」→「添加到主屏幕」"
    : ios ? "想放在桌面上？点「分享」，再点「添加到主屏幕」" : "想放在桌面上？点浏览器菜单，再点「添加到主屏幕」";
  box.innerHTML = `<span>${esc(text)}</span>${webInstall ? `<button class="btn small primary" data-web="install">添加</button>` : ""}<button class="wb" data-web="hometipclose" title="关闭" aria-label="关闭">${ICON.x}</button>`;
  box.hidden = false;
}
let webInstall = null;
window.addEventListener("beforeinstallprompt", e => { e.preventDefault(); webInstall = e; webHomeTip(); });
document.addEventListener("click", async e => {
  const b = e.target.closest("[data-web]"); if (!b) return;
  if (b.dataset.web === "hometipclose") { try { localStorage.setItem("miovrca.web.hometip", "1"); } catch (er) {} $("#whome").hidden = true; }
  if (b.dataset.web === "install" && webInstall) {
    const p = webInstall; webInstall = null;
    try { await p.prompt(); const c = await p.userChoice; if (c && c.outcome === "accepted") $("#whome").hidden = true; } catch (er) {}
  }
});

// ---------- 快速打开 ----------
// what was read last time (web.js webKept) drawn before the service answers; the answers then replace it
function webWarm() {
  const kept = (route, body) => webKept(webKeepKey(route, body));
  const service = kept("service", { refresh: false });
  if (!service?.ok || !service.account) return;
  SO.service = service; SO.serviceAt = 0;
  const unread = kept("unread", {}); if (unread?.ok) SO.unread = unread;
  const trends = kept("trends", {}); if (trends?.ok) SO.trends = trends;
  const filter = SO.timelineTab === "for_you" ? SO.timelineFilter : "", t = kept("timeline", { tab: SO.timelineTab, filter, after: "" });
  if (t?.ok) SO.timeline = { tab: SO.timelineTab, filter, items: t.items || [], next: t.next || "", busy: false, loaded: true, error: "", newer: [], kept: true };
}
async function webWarmTimeline() {
  const t = SO.timeline; if (!t?.kept) return;
  const r = await soServiceAPI("timeline", { tab: t.tab, filter: t.filter, after: "" });
  if (t !== SO.timeline || !r.ok || r.stale) return;
  t.items = r.items || []; t.next = r.next || ""; t.noBases = !!r.noBases; t.kept = false;
  soDraw();
}
// the service worker (sw.js): the page's files from this device, and notices
function webWorker() {
  if (!("serviceWorker" in navigator)) return;
  navigator.serviceWorker.register("sw.js").catch(() => {});
  navigator.serviceWorker.addEventListener("message", e => {
    const d = e.data || {};
    if (d.type === "open" && d.url) { const h = new URL(d.url).hash || "#/"; if (h !== location.hash) { history.pushState({ web: 1 }, "", h); webApply(h); } }
    if (d.type === "push") { soUnreadLoad(); if (SO.nav === "notices") soNoticesLoad(); if (SO.nav === "messages") soDMLoad(); }
  });
}

// ---------- start ----------
(function webStart() {
  const hash = location.hash || "#/";
  const dialog = /^#\/(post|work)\//.test(hash);
  webWarm();
  // a link to a post or a work opens it over 首页, so back leaves it for the page
  history.replaceState({ web: 1 }, "", dialog ? "#/" : hash);
  webApply(dialog ? "#/" : hash);
  if (dialog) { webApply(hash); R.modal = true; R.last = hash; history.pushState({ web: 1, modal: 1 }, "", hash); }
  webHomeTip();
  webWarmTimeline();
  webWorker();
  Promise.resolve(soServiceLoad()).then(soPushSync);
  soServiceAPI("trends").then(r => { if (r.ok && !r.stale || !SO.trends?.ok) { SO.trends = r; webFrameDraw(); } });
})();
