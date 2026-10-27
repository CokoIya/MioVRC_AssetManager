// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// 社区: the community window (internal/social). Creators, works and recipes come from the community service; an
// email account publishes posts and follows creators. Explore retains works, recipes and the waterfall.
"use strict";
// SO_WEB: the community in a browser (web/community/): what only the program can do (the library, the modding basket,
// projects, recipes, the wish list, Windows notifications, choosing a folder) is left out, and explanations with it.
const SO_WEB = !!window.WEB;
// Protocol launches are held by the local server until the shell and first-run screens are ready.
function socialOpenLink(link) {
  if (!link || !(link.kind === "post" && /^[pcmg][0-9]{1,18}$/.test(link.id) ||
      link.kind === "work" && /^(?:w[0-9]{1,9}|m[1-9][0-9]{0,17})$/.test(link.id))) return;
  closeDrawer(); setView("social");
  if (link.kind === "post") soPostOpen(link.id); else soWorkOpen(link.id);
}
let soLinkBusy = false;
setInterval(async () => {
  if (SO_WEB || soLinkBusy || !S.data || S.data.setupNeeded || $("#guide")?.open || $("#modal.on[data-kind='whatsnew']")) return;
  soLinkBusy = true;
  try {
    const hash = /^#miovrca:\/\/(post|work)\/([^/]+)$/.exec(location.hash);
    if (hash) {
      history.replaceState(null, "", location.pathname + location.search);
      socialOpenLink({kind: hash[1], id: hash[2]});
    } else {
      const r = await api("/api/app-link/take", {});
      if (r.link) socialOpenLink(r.link);
    }
  } catch (e) { /* A restarting server will answer the next poll. */ }
  finally { soLinkBusy = false; }
}, 1000);
// nav: the view in the tab; q: the header's search box; base/sort: the works' filters; st: local state; lists, feed,
// com, me: what each view shows; page: another player's page; work: the work open in its dialog; wd: the work being
// published or edited; home: the profile form; follows: follow targets as last read.
const SO = { nav: "home", q: "", base: "", sort: "latest", recipeOnly: false, videoOnly: false, basesMore: false, asset: null,
  st: null, lists: null, feed: null, com: null, me: null, page: null, work: null, wd: null, home: null, follows: {},
  posts: null, postTag: "", post: null, compose: null,
  timeline: null, timelineTab: "for_you", drafts: {}, unread: {}, notices: null, noticeTab: "all", searchType: "top",
  trends: null, worksPage: null, settings: null, dm: null, session: 0,
  htab: "recommend", hfeed: null, revealed: new Set(), prefs: null, groups: null, group: null, themes: null, asks: null, askFilter: "all",
  busy: {}, err: {}, service: null, stats: null, serviceAt: 0, serviceLoad: null, device: null, report: null, offer: null };
const SO_AVATAR = { w: 256, h: 256, max: 512 << 10 };
const SO_POST_MAX = 1600;
const SO_LANGS = [["zh", "中文"], ["ja", "日语"], ["en", "英语"], ["ko", "韩语"], ["other", "其他"]];
const SO_VIDEO = { bilibili: "哔哩哔哩", youtube: "YouTube", x: "X", douyin: "抖音", xiaohongshu: "小红书", nicovideo: "niconico", tiktok: "TikTok" };
const SO_KIND = { booth_shop: "Booth 店铺", booth_item: "Booth 商品", xianyu: "闲鱼", netdisk: "百度网盘", gumroad: "Gumroad", jinxxy: "Jinxxy",
  bilibili: "哔哩哔哩", youtube: "YouTube", x: "X", pixiv: "pixiv", xiaohongshu: "小红书", douyin: "抖音", discord: "Discord", qq_group: "QQ 群",
  github: "GitHub", website: "网站" };
// the composer's and the side bar's own (a picture, a pen, a work card)
const SO_TOOL = {
  camera: '<svg viewBox="0 0 24 24"><path d="M4 8h3l2-3h6l2 3h3v11H4z"/><circle cx="12" cy="13" r="3.5"/></svg>',
  smile: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M8.5 14.5c1.8 2 5.2 2 7 0"/><path d="M9 9.5h.01M15 9.5h.01"/></svg>',
  picture: '<svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="16" rx="3"/><circle cx="9" cy="10" r="2"/><path d="m21 16-5-5-9 9"/></svg>',
  pen: '<svg viewBox="0 0 24 24"><path d="M4 20h4L19 9l-4-4L4 16z"/><path d="m13.5 6.5 4 4"/></svg>',
  work: '<svg viewBox="0 0 24 24"><rect x="4" y="3" width="16" height="18" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/></svg>',
  poll: '<svg viewBox="0 0 24 24"><path d="M5 20V11M12 20V4M19 20v-6"/></svg>',
  pin: '<svg viewBox="0 0 24 24"><path d="M9 4h6l-1 5 3 3v2H7v-2l3-3z"/><path d="M12 14v6"/></svg>',
  ask: '<svg viewBox="0 0 24 24"><path d="M10 6a2 2 0 1 1 3 1.7c-.6.4-1 .8-1 1.4v.4"/><path d="m12 9.5-9 7h18z"/></svg>',
  booth: '<svg viewBox="0 0 24 24"><path d="M5 8h14l-1 12H6z"/><path d="M9 8V6a3 3 0 0 1 6 0v2"/></svg>',
};
const SO_ICON = {
  explore: '<svg viewBox="0 0 24 24"><circle cx="10" cy="10" r="7"/><path d="m15 15 6 6"/></svg>',
  notices: '<svg viewBox="0 0 24 24"><path d="M5 17h14l-2-3V9a5 5 0 0 0-10 0v5zM10 21h4"/></svg>',
  messages: '<svg viewBox="0 0 24 24"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="m3 6 9 7 9-7"/></svg>',
  bookmarks: '<svg viewBox="0 0 24 24"><path d="M6 3h12v18l-6-4-6 4z"/></svg>',
  more: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M7 12h1m3 0h1m3 0h1"/></svg>',
  home: '<svg viewBox="0 0 24 24"><path d="m3 11 9-7 9 7"/><path d="M5 10v10h14V10"/><path d="M10 20v-6h4v6"/></svg>',
  feed: '<svg viewBox="0 0 24 24"><path d="M4 6h16M4 12h10M4 18h7"/><circle cx="18" cy="16" r="3"/></svg>',
  posts: '<svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="m5 18 5-5 3 3 2-2 4 4"/></svg>',
  works: '<svg viewBox="0 0 24 24"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="m10 9 5 3-5 3z"/></svg>',
  creators: '<svg viewBox="0 0 24 24"><circle cx="9" cy="8" r="3.5"/><path d="M3 20a6 6 0 0 1 12 0"/><path d="M16 4.5a3.5 3.5 0 0 1 0 7M21 20a6 6 0 0 0-3.5-5.4"/></svg>',
  recipes: '<svg viewBox="0 0 24 24"><path d="M6 3h9l4 4v14H6z"/><path d="M14 3v5h5M9 13h7M9 17h5"/></svg>',
  me: '<svg viewBox="0 0 24 24"><rect x="3" y="5" width="18" height="14" rx="2"/><circle cx="9" cy="11" r="2.2"/><path d="M5.8 16a3.4 3.4 0 0 1 6.4 0M14 10h4M14 13.5h3"/></svg>',
  like: '<svg viewBox="0 0 24 24"><path d="M7 10v10H4V10z"/><path d="M7 10l4-7a2 2 0 0 1 3 2l-1 5h6a2 2 0 0 1 2 2.3l-1.2 6A2 2 0 0 1 17.8 20H7"/></svg>',
  link: '<svg viewBox="0 0 24 24"><path d="M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1 1"/><path d="M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1-1"/></svg>',
  flag: '<svg viewBox="0 0 24 24"><path d="M5 21V4M5 4h11l-2 4 2 4H5"/></svg>',
  cake: '<svg viewBox="0 0 24 24"><path d="M4 21h16v-8H4zM4 16c2 1.5 4 1.5 6 0s4-1.5 6 0 4 1.5 4 0"/><path d="M12 13V9M12 6.5V6"/></svg>',
};
// a post's actions as X draws them: the icon alone (its name in title and aria-label) with the count beside it;
// also the profile's tabs, the notices' kinds and 为你推荐's filters
const SO_ACT = {
  featured: '<svg viewBox="0 0 24 24"><path class="mv" d="m12 3.5 2.6 5.3 5.9.9-4.3 4.1 1 5.8L12 16.9l-5.2 2.7 1-5.8-4.3-4.1 5.9-.9z"/></svg>',
  reply: '<svg viewBox="0 0 24 24"><path d="M20 11.5c0 4-3.6 7-8 7-1 0-2-.2-2.9-.5L4 20l1.4-3.6A6.7 6.7 0 0 1 4 11.5c0-4 3.6-7 8-7s8 3 8 7z"/></svg>',
  repost: '<svg viewBox="0 0 24 24"><path d="M4 11V8a2 2 0 0 1 2-2h12"/><path d="m15 3 3 3-3 3"/><path d="M20 13v3a2 2 0 0 1-2 2H6"/><path d="m9 21-3-3 3-3"/></svg>',
  like: '<svg viewBox="0 0 24 24"><path d="M12 20.3 4.6 13a4.7 4.7 0 0 1 6.7-6.6l.7.7.7-.7a4.7 4.7 0 0 1 6.7 6.6z"/></svg>',
  views: '<svg viewBox="0 0 24 24"><path d="M5 20v-6M10 20V9M15 20V4M20 20v-9"/></svg>',
  bookmark: '<svg viewBox="0 0 24 24"><path d="M6.5 4h11v16.5L12 16.6l-5.5 3.9z"/></svg>',
  share: '<svg viewBox="0 0 24 24"><path d="M12 15V4"/><path d="m7.5 8.5 4.5-4.5 4.5 4.5"/><path d="M5 13v5a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-5"/></svg>',
  posts: '<svg viewBox="0 0 24 24"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 9h8M8 12.5h8M8 16h5"/></svg>',
  showcase: '<svg viewBox="0 0 24 24"><path d="M3.5 5h17v4h-17zM5 9v11h14V9"/><path d="M10 13h4"/></svg>',
  mention: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3.6"/><path d="M15.6 12v1.4a2.6 2.6 0 0 0 5.2 0V12A8.8 8.8 0 1 0 17 19.2"/></svg>',
  quote: '<svg viewBox="0 0 24 24"><path d="M10 7H6v5h4zM18 7h-4v5h4z"/><path d="M10 12c0 3-1.4 4.8-4 5.5M18 12c0 3-1.4 4.8-4 5.5"/></svg>',
  follow: '<svg viewBox="0 0 24 24"><circle cx="10" cy="8" r="3.5"/><path d="M4 20a6 6 0 0 1 12 0"/><path d="M19 8v6M16 11h6"/></svg>',
  answer: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="m8 12.2 2.8 2.8L16 9.5"/></svg>',
  spark: '<svg viewBox="0 0 24 24"><path d="M12 3.5 13.8 9l5.7 1.5-5.7 1.6L12 17.5l-1.8-5.4-5.7-1.6L10.2 9z"/><path d="M18.5 16.5l.7 2 2 .7-2 .7-.7 2-.7-2-2-.7 2-.7z"/></svg>',
  clock: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/></svg>',
  hot: '<svg viewBox="0 0 24 24"><path d="M12 21c-3.6 0-6.5-2.6-6.5-6.2 0-3 2-4.8 3.4-6.6.5 1.6 1.4 2.6 2.5 3.1-.2-3.4 1.4-6 3.9-7.8-.2 2.8.9 4.6 2.4 6.3 1.2 1.4 1.8 3 1.8 5 0 3.6-3.1 6.2-7.5 6.2z"/></svg>',
  bot: '<svg viewBox="0 0 24 24"><rect x="5" y="8" width="14" height="11" rx="3"/><path d="M12 8V5M9.5 13h.01M14.5 13h.01M9.5 16.2h5"/><circle cx="12" cy="4" r="1"/></svg>',
};
// the icons of the other buttons (soIconize puts them in); the parts an animation moves have a class of their own
const SO_UI = {
  send: '<svg viewBox="0 0 24 24"><g class="mv"><path d="M20.5 3.5 10 14"/><path d="M20.5 3.5 14 20.5l-4-6.5-6.5-4z"/></g></svg>',
  plus: '<svg viewBox="0 0 24 24"><path class="mv" d="M12 5v14M5 12h14"/></svg>',
  join: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path class="mv" d="M12 8v8M8 12h8"/></svg>',
  joined: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path class="dr" pathLength="1" d="m8 12.3 2.8 2.8L16 9.6"/></svg>',
  clone: '<svg viewBox="0 0 24 24"><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/><g class="mv"><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M14 11.5v5M11.5 14h5"/></g></svg>',
  copy: '<svg viewBox="0 0 24 24"><path d="M5 15V6a2 2 0 0 1 2-2h8"/><rect class="mv" x="9" y="8" width="11" height="12" rx="2"/></svg>',
  following: '<svg viewBox="0 0 24 24"><circle cx="10" cy="8" r="3.5"/><path d="M4 20a6 6 0 0 1 12 0"/><path class="dr" pathLength="1" d="m16 11 2 2 4-4"/></svg>',
  dots: '<svg viewBox="0 0 24 24" class="fill"><circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/></svg>',
  edit: '<svg viewBox="0 0 24 24"><g class="mv"><path d="M4 20h4L19 9l-4-4L4 16z"/><path d="m13.5 6.5 4 4"/></g></svg>',
  mail: '<svg viewBox="0 0 24 24"><rect x="3" y="5" width="18" height="14" rx="2"/><path class="mv" d="m3 6 9 7 9-7"/></svg>',
  shield: '<svg viewBox="0 0 24 24"><path d="M12 3 5 6v5c0 4.5 3 8 7 10 4-2 7-5.5 7-10V6z"/><path class="dr" pathLength="1" d="m9 12 2.2 2.2L15.5 10"/></svg>',
  download: '<svg viewBox="0 0 24 24"><g class="mv"><path d="M12 4v11"/><path d="m7 10 5 5 5-5"/></g><path d="M5 20h14"/></svg>',
  logout: '<svg viewBox="0 0 24 24"><path d="M10 4H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4"/><g class="mv"><path d="m15 8 4 4-4 4"/><path d="M19 12H9"/></g></svg>',
  login: '<svg viewBox="0 0 24 24"><path d="M14 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4"/><g class="mv"><path d="m10 8 4 4-4 4"/><path d="M14 12H4"/></g></svg>',
  trash: '<svg viewBox="0 0 24 24"><g class="lid"><path d="M4 7h16"/><path d="M9.5 7V4.5h5V7"/></g><path d="M6 7l1 13h10l1-13"/><path d="M10 11v5.5M14 11v5.5"/></svg>',
  checks: '<svg viewBox="0 0 24 24"><path class="dr" pathLength="1" d="m2.5 12.5 4.5 4.5 8.5-8.5"/><path class="dr d2" pathLength="1" d="m11.5 16 1 1 9-9"/></svg>',
  check: '<svg viewBox="0 0 24 24"><path class="dr" pathLength="1" d="m5 12.5 4.5 4.5L19 7.5"/></svg>',
  inbox: '<svg viewBox="0 0 24 24"><path d="M4 13l2.5-8h11L20 13v6H4z"/><path class="mv" d="M4 13h4.5l1.5 2.5h4l1.5-2.5H20"/></svg>',
  block: '<svg viewBox="0 0 24 24"><g class="mv"><circle cx="12" cy="12" r="9"/><path d="m5.7 5.7 12.6 12.6"/></g></svg>',
  x: '<svg viewBox="0 0 24 24"><path class="mv" d="M6 6l12 12M18 6 6 18"/></svg>',
  chat: '<svg viewBox="0 0 24 24"><g class="mv"><path d="M4 5h16v11H9.5L4 20z"/><path d="M8.5 10.5h.01M12 10.5h.01M15.5 10.5h.01"/></g></svg>',
  globe: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><g class="mv"><path d="M3 12h18"/><path d="M12 3c2.5 2.6 3.8 5.6 3.8 9s-1.3 6.4-3.8 9c-2.5-2.6-3.8-5.6-3.8-9S9.5 5.6 12 3z"/></g></svg>',
  trend: '<svg viewBox="0 0 24 24"><g class="mv"><path d="m3 17 6-6 4 4 8-8"/><path d="M15 7h6v6"/></g></svg>',
  video: '<svg viewBox="0 0 24 24"><rect x="3" y="6" width="13" height="12" rx="2"/><path class="mv" d="m16 10 5-3v10l-5-3z"/></svg>',
  person: '<svg viewBox="0 0 24 24"><g class="mv"><circle cx="12" cy="8" r="4"/><path d="M4.5 21a7.5 7.5 0 0 1 15 0"/></g></svg>',
  list: '<svg viewBox="0 0 24 24"><path d="M8.5 6H20M8.5 12H20M8.5 18H20"/><path class="mv" d="M4 6h.01M4 12h.01M4 18h.01"/></svg>',
  help: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><g class="mv"><path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .8-1 1.5v.3"/><path d="M12 16.8h.01"/></g></svg>',
  more: '<svg viewBox="0 0 24 24"><path class="mv" d="m6 9 6 6 6-6"/></svg>',
  prev: '<svg viewBox="0 0 24 24"><path class="mv" d="m15 6-6 6 6 6"/></svg>',
  next: '<svg viewBox="0 0 24 24"><path class="mv" d="m9 6 6 6-6 6"/></svg>',
  back: '<svg viewBox="0 0 24 24"><g class="mv"><path d="M19 12H5"/><path d="m11 6-6 6 6 6"/></g></svg>',
  eye: '<svg viewBox="0 0 24 24"><g class="mv"><path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="3"/></g></svg>',
  eyeoff: '<svg viewBox="0 0 24 24"><path d="M2.5 12S6 5.5 12 5.5c2 0 3.7.7 5.1 1.7M21.5 12s-3.5 6.5-9.5 6.5c-2 0-3.7-.7-5.1-1.7"/><path d="M9.9 9.9a3 3 0 0 0 4.2 4.2"/><path class="mv" d="M4 4l16 16"/></svg>',
  undo: '<svg viewBox="0 0 24 24"><g class="mv"><path d="M9 14 4 9l5-5"/><path d="M4 9h11a5 5 0 0 1 0 10h-3"/></g></svg>',
  refresh: '<svg viewBox="0 0 24 24"><g class="mv"><path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 4v7h-7"/></g></svg>',
  star: '<svg viewBox="0 0 24 24"><path class="mv" d="m12 3.5 2.6 5.3 5.9.9-4.3 4.1 1 5.8L12 16.9l-5.2 2.7 1-5.8-4.3-4.1 5.9-.9z"/></svg>',
  folder: '<svg viewBox="0 0 24 24"><path class="mv" d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>',
  library: '<svg viewBox="0 0 24 24"><path d="M5 4h4v16H5zM10 4h4v16h-4z"/><path class="mv" d="m15.2 5.3 3.7-1 3.6 14.6-3.7 1z"/></svg>',
  unlink: '<svg viewBox="0 0 24 24"><path d="M10 14a4 4 0 0 0 5.66 0l3-3a4 4 0 0 0-5.66-5.66l-1 1"/><path d="M14 10a4 4 0 0 0-5.66 0l-3 3a4 4 0 0 0 5.66 5.66l1-1"/><path class="mv" d="M4 4l16 16"/></svg>',
  qr: '<svg viewBox="0 0 24 24"><rect x="3.5" y="3.5" width="6.5" height="6.5" rx="1"/><rect x="14" y="3.5" width="6.5" height="6.5" rx="1"/><rect x="3.5" y="14" width="6.5" height="6.5" rx="1"/><path class="mv" d="M14 14h2.5v2.5H14zM20.5 14v.01M14 20.5h.01M17.5 17.5h3v3"/></svg>',
  scan: '<svg viewBox="0 0 24 24"><path d="M4 8V5a1 1 0 0 1 1-1h3M16 4h3a1 1 0 0 1 1 1v3M20 16v3a1 1 0 0 1-1 1h-3M8 20H5a1 1 0 0 1-1-1v-3"/><path class="mv" d="M4 12h16"/></svg>',
  phone: '<svg viewBox="0 0 24 24"><g class="mv"><rect x="6.5" y="2.5" width="11" height="19" rx="2.5"/><path d="M11 18h2"/></g></svg>',
  bell: '<svg viewBox="0 0 24 24"><g class="mv"><path d="M5 17h14l-2-3V9a5 5 0 0 0-10 0v5z"/><path d="M10 21h4"/></g></svg>',
};
function soIconSVG(k) { return SO_UI[k] || SO_ACT[k] || SO_TOOL[k] || SO_ICON[k] || ""; }
// What each button of the community shows, as X puts an icon on almost everything: by data-so, or by data-so and
// its data-nav, -tab, -box, -sort or -kind ("nav:edit"). Two names: the first when off, the second when on (.on or
// aria-pressed). Rows that are a person, a post, a work or a tag stay as they are.
const SO_BTN = {
  xpublish: "send", dmsend: "send", wsend: "send", reportsend: "flag", xthemejoin: "join", groupjoin: ["join", "joined"], xthemeoff: "x",
  webinapp: "clone", xclone: "clone", wclone: "clone", xclonebase: "clone", wclonebase: "clone", xalong: "clone", remix: "clone", cloneapply: "check", cloneundo: "undo",
  xcredits: "copy", credits: "copy", copycontact: "copy", boothcopy: "copy", xcopy: "link", url: "link", githubstart: "link", githubunlink: "unlink", oauthlink: "link", oauthunlink: "unlink", boothunbind: "unlink",
  follow: ["follow", "following"], basefollow: ["follow", "following"], followshop: ["follow", "following"], dmstart: "mail",
  xmenu: "dots", pagemenu: "dots", dmmore: "dots", xmore: "dots", xprev: "prev", xnext: "next",
  dmaccept: "check", dmblock: "block", xblock: "block", dmdelete: "trash", xdelete: "trash", wremove: "trash", dmrecall: "undo", dmreport: "flag", xreport: "flag", report: "flag",
  "dmbox:requests": "inbox", "dmbox:inbox": "back", pageunblock: "undo", xpin: "pin", xunpin: "pin",
  "nav:edit": "edit", wedit: "edit", handle: "edit", contacts: "edit", emailchange: "mail", emailcode: "mail", accountcode: "mail", boothstart: "shield", boothcheck: "check",
  accountexport: "download", accountlogout: "logout", accountdelete: "trash", accountdeletego: "trash", accountsave: "login", account: "login", accountcancel: "x",
  emailsave: "check", contactssave: "check", homesave: "check", xhandlesave: "check", xhandlelater: "x", xcomplete: "check", reportcancel: "x", nooffer: "x",
  noticesread: "checks", refresh: "refresh", xretry: "refresh", homeretry: "refresh", xthreadretry: "refresh",
  xthreadmore: "more", xsearchmore: "more", worksnext: "more", groupmembersmore: "more", basesmore: "more", pageworkmore: "more", showcaseall: "check", showcasenone: "x",
  groupback: "back", back: "back", xviewerback: "back", groupmembers: "creators", xreveal: "eye", askmark: "answer",
  linkadd: "plus", xpolladd: "plus", askboothadd: "plus", assetadd: "plus", newwork: "plus", xnew: "plus",
  linkdel: "x", askboothdel: "x", askworkdrop: "x", xworkdrop: "x", xpolldrop: "x", xpolldel: "x", assetdel: "x", assetclear: "x", avclear: "x", bdclear: "x", basedclear: "x", dmpicclear: "x",
  wish: "star", wishmissing: "star", showcasewish: "star", shareimage: "share", recipeshare: "share", recipesharego: "share", recipe: "recipes", wrecipe: "recipes", xrecipe: "recipes", assetrecipe: "recipes",
  assetlib: "library", assetproject: "folder", ask: "ask", booth: "booth",
  "sort:latest": "clock", "sort:popular": "hot", recipefilter: "recipes", videofilter: "video",
  "nav:explore": "trend", "nav:groups": "creators", "nav:asks": "ask", "nav:works": "work", "nav:images": "posts", "nav:creators": "creators", "nav:recipes": "recipes",
  "htab:recommend": "spark", "htab:following": "creators", "htab:latest": "clock", "m:close": "x",
  "link:booth_item": "booth", "link:booth_shop": "booth", link: "link", asset: "work",
  xstafffeature: "star", xstaffunfeature: "star", xtranslate: "globe", xtranslatehide: "x", xstaffhide: "eyeoff", xstaffsens: "eye", xstaffunsens: "eye", xstaffdelete: "trash", pagestaffban: "block",
  qrhandoff: "qr", qrscan: "scan", qrrefresh: "refresh", qrapprove: "login", qrdeny: "x", qrsignin: "login",
};
function soIconOne(b) {
  // (a dialog's own buttons only in the community's dialogs: the program's other dialogs stay as they are)
  const so = b.dataset.so || (b.dataset.m && (SO_WEB || /^s[ox]/.test(b.closest("#modal")?.dataset.kind || "")) && "m:" + b.dataset.m);
  if (!so || b.querySelector(":scope > .soi, svg, img, canvas, .soav, .octh") || b.matches(".sxitem, .solink")) return; // (an item's row, a profile's link)
  const k = SO_BTN[so + ":" + (b.dataset.nav || b.dataset.tab || b.dataset.box || b.dataset.sort || b.dataset.kind || "")] || SO_BTN[so];
  const [off, on] = Array.isArray(k) ? k : [k];
  if (!off || !soIconSVG(off)) return;
  const text = b.textContent.trim();
  if (/^[⋯…‹›×✕]$/.test(text)) { if (!b.getAttribute("aria-label")) b.setAttribute("aria-label", b.title || { "‹": "上一张", "›": "下一张", "×": "关闭", "✕": "关闭" }[text] || "更多"); b.textContent = ""; b.classList.add("soiconly"); }
  else if (!text) b.classList.add("soiconly");
  b.insertAdjacentHTML("afterbegin", `<span class="soi${on ? " sotwo" : ""}" data-i="${off}" aria-hidden="true">${on ? `<span class="soff">${soIconSVG(off)}</span><span class="son" data-i="${on}">${soIconSVG(on)}</span>` : soIconSVG(off)}</span>`);
}
function soIconize(root) {
  if (root.dataset?.so || root.dataset?.m) soIconOne(root);
  for (const b of root.querySelectorAll?.("[data-so], [data-m]") || []) soIconOne(b);
}
// pressing a button plays its icon's move (the send arrow flies, the bin's lid lifts, the check draws itself, …)
function soIconPop(b) {
  const i = b.querySelector(":scope > .soi, :scope > .sxtabicon, :scope > svg, :scope > .ico");
  if (!i || b.closest(".sxactions")) return;
  i.classList.remove("pop"); void i.offsetWidth; i.classList.add("pop");
  i.addEventListener("animationend", () => i.classList.remove("pop"), { once: true });
}
if (typeof MutationObserver !== "undefined") {
  soIconize(document.body);
  new MutationObserver(ms => {
    for (const m of ms) {
      if (m.target.nodeType === 1 && (m.target.dataset.so || m.target.dataset.m)) soIconOne(m.target);
      for (const n of m.addedNodes) if (n.nodeType === 1) soIconize(n);
    }
  }).observe(document.body, { childList: true, subtree: true });
}
document.addEventListener("pointerdown", e => { const b = e.target.closest("button, [role=button], [role=tab]"); if (b && !b.disabled) soIconPop(b); }, true);

// ---------- reading ----------
async function soLoad(what, refresh) {
  if (SO.busy[what]) return;
  SO.busy[what] = true; SO.err[what] = "";
  soDraw();
  const path = { lists: "/api/community/lists", feed: "/api/community/feed", me: "/api/community/me", com: "/api/community/recipes", st: "/api/community/state" }[what];
  let r;
  try { r = await api(path, { refresh: !!refresh }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  SO.busy[what] = false;
  if (!r.ok) SO.err[what] = r.err || "无法读取社区";
  else if (what === "feed") { SO.feed = r.feed; soDot(); }
  else SO[what] = r;
  if (what === "lists" && r.ok) soRememberFollows(r);
  soDraw();
}
function soRememberFollows(r) {
  for (const c of r.creators || []) if (c.target) SO.follows[c.target] = !!c.followed;
  for (const w of r.works || []) if (w.target) SO.follows[w.target] = !!w.followed;
}
// what the view in sight needs (read once; 「刷新」 reads it again)
function soNeed(refresh) {
  soServiceLoad(refresh);
  if (!SO.st || refresh) soLoad("st");
  const nav = SO.nav;
  if (nav === "home" && (!SO.timeline || refresh)) soTimelineLoad(false, refresh);
  if (nav === "home" || nav === "search") soThemesLoad(refresh);
  if (nav === "groups") soGroupsLoad();
  if (nav === "asks") soAsksLoad(false);
  if (nav === "group" && SO.group && (!SO.group.data || refresh)) soGroupLoad();
  if (nav === "images") { if (!SO.hfeed || refresh) soLoadHome(false, refresh); soLoad("lists", refresh); }
  if (nav === "explore") { soTrendsLoad(); soLoad("lists", refresh); }
  if (nav === "works") soWorksLoad(true);
  if (["works", "creators", "edit"].includes(nav)) soLoad("lists", refresh);
  if (nav === "recipes") { soLoad("com", refresh); soLoad("lists", refresh); }
  if (nav === "edit") soLoad("me", refresh);
  if (nav === "me") soOwnProfile(refresh);
  if (nav === "more") soSettingsLoad();
  if (nav === "messages") { soDMLoad(); soPollSoon(); }
  if (nav === "notices") soNoticesLoad();
  if (nav === "bookmarks") soSearchLoad(true);
  if (SO.page && refresh && nav !== "me") soProfile(SO.page.src, true);
}
function socialEnter() { soNeed(false); }
function socialPlaceholder() { return "搜索帖子、作品、创作者"; }
function socialSearch(q) {
  SO.q = q || "";
  if (S.view !== "social") return;
  if (["home", "search", "explore", "groups", "group", "asks"].includes(SO.nav)) { SO.nav = "search"; clearTimeout(soSearchTimer); soSearchTimer = setTimeout(() => soSearchLoad(true), 300); }
  else if (SO.nav === "works") { clearTimeout(soSearchTimer); soSearchTimer = setTimeout(() => soWorksLoad(true), 300); }
  else renderSocialMain();
}
let soSearchTimer;
function soDraw() { if (S.view === "social") { renderSocialSide(); renderSocialMain(); } }
let soDotLast = 0;
function soDot() {
  const b = $("#vSocial"), n = socialUnreadTotal();
  if (b) { b.classList.toggle("sodot", n > 0); b.title = "社区"; }
  if (n !== soDotLast) {
    soDotLast = n;
    document.dispatchEvent(new CustomEvent("socialunread", {detail: {total: n, notices: SO.unread.notices || 0, messages: SO.unread.messages || 0}}));
  }
}
// The unread notifications and private messages (message requests included) of the signed-in account, for the
// shell's community button; "socialunread" on document says when it changes (HOOKS_community_c34.md).
function socialUnreadTotal() { return SO.service?.account ? (SO.unread.notices || 0) + (SO.unread.messages || 0) : 0; }
window.socialUnreadTotal = socialUnreadTotal;
// A click on the community's Windows notification (cmd/miovrca/tray_windows.go): the page it names.
function socialFromNotice(nav) {
  SO.asset = null; SO.page = null; SO.nav = nav === "messages" ? "messages" : "notices"; SO.q = "";
  closeModal(); if (S.view !== "social") setView("social");
  soNeed(false); soDraw(); soPollSoon();
}
window.socialFromNotice = socialFromNotice;
// The header releases text before its tabs sit closer together.
function soTabFit() {
  const top = document.querySelector("header.top"), tab = $("#vSocial");
  if (!top || !tab) return;
  const views = tab.parentNode, over = () => top.scrollWidth - top.clientWidth > 0;
  let lvl = 0;
  tab.dataset.fit = "0"; views.classList.remove("sotight");
  while (lvl < 3 && over()) { lvl++; tab.dataset.fit = String(lvl); if (lvl === 3) views.classList.add("sotight"); }
}
(() => {
  const top = document.querySelector("header.top");
  if (!top || typeof ResizeObserver === "undefined") return;
  let raf = 0;
  const ro = new ResizeObserver(() => { if (!raf) raf = requestAnimationFrame(() => { raf = 0; soTabFit(); }); });
  ro.observe(top);
  for (const el of top.children) ro.observe(el);
})();
function soWords(q) { return String(q || "").toLowerCase().split(/\s+/).filter(Boolean); }
function soHit(words, ...hay) { const h = hay.join("\n").toLowerCase(); return words.every(w => h.includes(w)); }
function soMe() { return (SO.me && SO.me.account && SO.me.account.github) || (SO.service && SO.service.account && SO.service.account.github) || (SO.st && SO.st.login) || ""; }
function soAccountID() { return (SO.me && SO.me.account && SO.me.account.id) || (SO.service && SO.service.account && SO.service.account.id) || ""; }
function soFollowing(login) {
  if (!login) return false;
  const target = /^u[0-9a-z]{12}$/.test(login) ? login : "gh:" + login;
  if (target in SO.follows) return SO.follows[target];
  if (SO.lists && (SO.lists.creators || []).some(c => c.github === login && c.followed)) return true;
  return false;
}

// ---------- small parts ----------
function soAv(pic, name, cls) {
  const ch = Array.from(String(name || "?"))[0] || "?";
  return `<span class="soav${cls ? " " + cls : ""}">${pic ? `<img src="${esc(pic)}" alt="" loading="lazy" decoding="async">` : `<i data-i18n="off">${esc(ch)}</i>`}</span>`;
}
function soWhen(s) { const t = Date.parse(s || ""); return isNaN(t) ? esc(s || "") : esc(fmtTime(Math.floor(t / 1000))); }
function soCreatorBtn(login, name, pic, plain) {
  if (!login) return name ? `<span data-i18n="off">${esc(name)}</span>` : "";
  return `<button class="sochip${plain ? " plain" : ""}" data-so="profile" data-github="${esc(login)}" title="${esc("@" + login)}">${plain ? "" : soAv(pic, name || login, "xs")}<span data-i18n="off">${esc(name || login)}</span></button>`;
}
function soProfileBtn(id, github, name, pic, plain, booth) {
  if (!id && !github) return name ? `<span data-i18n="off">${esc(name)}</span>` : "";
  const who = id ? `data-id="${esc(id)}"` : `data-github="${esc(github)}"`;
  const title = github ? "@" + github : "";
  return `<button class="sochip${plain ? " plain" : ""}" data-so="profile" ${who} title="${esc(title)}">${plain ? "" : soAv(pic, name || github, "xs")}<span data-i18n="off">${esc(name || github || id)}</span></button>${soVerified(booth)}`;
}
// the mark of an account that proved it owns a Booth shop (the service read a code on the shop's page)
function soVerified(shop) {
  return shop ? `<span class="soverified" title="${esc(`已认证的 Booth 店铺：${shop}.booth.pm`)}" aria-label="已认证"><svg viewBox="0 0 24 24"><path d="m6 12 4 4 8-8"/></svg></span>` : "";
}
function socialLinksHTML(links) {
  if (!links || !links.length) return "";
  return `<div class="solinks">${links.map(l => `<button class="btn small solink" data-so="link" data-kind="${esc(l.kind)}" data-href="${esc(l.url)}" data-label="${esc(l.label || "")}" title="${esc(l.url)}">${SO_ICON.link}${l.label ? `<span data-i18n="off">${esc(l.label)}</span>` : `<span>${esc(SO_KIND[l.kind] || SO_KIND.website)}</span>`}</button>`).join("")}</div>`;
}
// QQ / LINE / Discord on a profile: a click copies the ID (they are what the player typed, not verified)
function soContactsHTML(c) {
  return [["QQ", c?.qq], ["LINE", c?.line], ["Discord", c?.discord]].filter(x => x[1])
    .map(([k, v]) => `<button class="btn small sxcontactbtn" data-so="copycontact" data-text="${esc(v)}" title="复制"><span class="sxcontactk">${k}</span><span data-i18n="off">${esc(v)}</span></button>`).join("");
}
function soInside() { if (S.data && S.data.paneMode) closeModal(); }
async function soOpenLink(kind, url, label, btn) {
  let u;
  try { u = new URL(url); } catch (e) { toast("链接格式不正确"); return; }
  if (u.protocol !== "https:" || u.username || u.password || u.port) { toast("链接格式不正确"); return; }
  const r = await api("/api/community/linkkinds", { urls: [url], for: "open" });
  kind = r.ok && r.kinds && r.kinds[0];
  if (!kind) { toast("链接格式不正确"); return; }
  if (kind === "booth_shop" || kind === "booth_item") { soInside(); return openWeb(url, "booth"); }
  if (kind === "netdisk") return SO_WEB ? openURL(url) : soPop(btn, [["netadd", "收录到素材库"], ["netopen", "在网盘中打开"]], { url, label });
  return openURL(url);
}
function soPop(btn, items, data) {
  soPopClose();
  const p = document.createElement("div");
  p.className = "sopop"; p.id = "so_pop"; p._data = data;
  p.innerHTML = items.map(([k, t]) => k === "sep" ? `<div class="sopopsep" role="separator"></div>` : `<button class="navitem" data-so="${k}">${esc(t)}</button>`).join("");
  document.body.append(p);
  const r = btn.getBoundingClientRect(), w = p.offsetWidth;
  p.style.left = Math.max(8, Math.min(r.left, innerWidth - w - 8)) + "px";
  p.style.top = (r.bottom + 4 + p.offsetHeight > innerHeight ? r.top - p.offsetHeight - 4 : r.bottom + 4) + "px";
  const f = $("button", p); if (f) f.focus();
}
function soPopClose() { const p = $("#so_pop"); if (p) p.remove(); }
function soPopAct(what) {
  const p = $("#so_pop"), d = p && p._data; soPopClose(); if (!d) return;
  closeModal();
  if (what === "netadd") return openPanAdd(d.url + (d.label ? " " + d.label : ""));
  return openWeb(d.url, "pan");
}

// ---------- side bar ----------
function renderSocialSide() {
  const a = SO.service?.account;
  const active = SO.page?.data?.me || SO.nav === "edit" ? "me" : ["works", "creators", "recipes", "search", "images", "groups", "group", "asks"].includes(SO.nav) ? "explore" : SO.nav;
  const selected = k => active === k && (!SO.page || SO.page.data?.me && k === "me");
  const icons = {explore: "⌕", notices: "♧", messages: "✉", bookmarks: "♧", more: "⋯"};
  const item = (k, t) => `<button class="navitem${selected(k) ? " on" : ""}" data-so="nav" data-nav="${k}" aria-pressed="${selected(k)}">${SO_ICON[k] || `<span class="sxnavicon" aria-hidden="true">${icons[k]}</span>`}<span class="nm">${t}</span>${SO.unread[k] ? `<span class="sxbadge">${esc(SO.unread[k])}</span>` : ""}</button>`;
  const account = a ? `${soAv(SO.service?.accountPic, a.nickname)}<span><b data-i18n="off">${esc(a.nickname)}</b><small data-i18n="off">@${esc(a.handle || "")}</small></span>` : `<span>登录社区</span>`;
  if (SO_WEB && !a) return put($("#side"), `<div class="sidelinks sonav">${item("home", "首页")}${item("explore", "探索")}${item("more", "更多")}</div>`); // (signed out in a browser, as X: the bar at the bottom offers 注册 and 登录)
  put($("#side"), `<div class="sidelinks sonav">${item("home", "首页")}${item("explore", "探索")}${item("notices", "通知")}${item("messages", "私信")}${item("bookmarks", "书签")}${item("me", "个人主页")}${item("more", "更多")}</div>
    <div class="soside"><button class="btn primary sxpublish" data-so="compose">${SO_TOOL.pen}<span>发帖</span></button><button class="btn sxnewwork" data-so="newwork">${SO_TOOL.work}<span>发布作品</span></button><button class="btn sxnewwork" data-so="ask">${SO_TOOL.ask}<span>求同款</span></button></div>
    ${SO.nav === "works" ? soBaseChips() : ""}<button class="sxaccount" data-so="${a ? "nav" : "account"}" data-nav="more">${account}</button>`);
}
function renderSocialMain() {
  const keep = focusMark($(".main"));
  if (SO.page) soPageDraw();
  else if (SO.nav === "home") soTimelineDraw();
  else if (SO.nav === "explore") soExploreDraw();
  else if (SO.nav === "groups") soGroupsDraw();
  else if (SO.nav === "group") soGroupDraw();
  else if (SO.nav === "asks") soAsksDraw();
  else if (SO.nav === "images") soHfDraw();
  else if (SO.nav === "works") soWorksDraw();
  else if (SO.nav === "creators") soCreatorsDraw();
  else if (SO.nav === "recipes") soRecipesDraw();
  else if (SO.nav === "edit") soHomeDraw();
  else if (SO.nav === "me") { soBar(null); gridPut(soEmpty(SO.service?.account ? "正在读取…" : "登录社区", soAccountHTML())); }
  else if (["search", "bookmarks"].includes(SO.nav)) soSearchDraw();
  else if (SO.nav === "notices") soNoticesDraw();
  else if (SO.nav === "more") soSettingsDraw();
  else if (SO.nav === "messages") soDMDraw();
  keep(); soViewsWatch();
  for (const t of document.querySelectorAll("#grid [data-composer] textarea")) if (t.value) soComposeGrow(t);
}
function soBar(count, what, extra) {
  const L = SO.lists, off = L && L.offline ? soOffline(L.offline, true) : "";
  const busy = SO.busy.lists || SO.busy.feed || SO.busy.com || SO.busy.me || SO.busy.home || SO.busy.postpic || SO.posts?.busy;
  put($("#resultbar"), `${["explore", "images", "works", "creators", "recipes", "search", "groups", "asks"].includes(SO.nav) && !SO.page ? soExploreNav() : ""}${count === null ? "" : `<b>${esc(count)}</b><span>${what}</span>`}${extra || ""}${off}<span class="spacer"></span><button class="btn small ghost" data-so="refresh"${busy ? " disabled" : ""}>${busy ? `<i class="spin"></i>` : ICON.upd}<span>刷新</span></button>`);
}
function soOffline(why, kept) {
  return `<span class="warnc small"><span>${kept ? "暂时无法更新，显示的是之前读取的内容" : "暂时无法更新"}</span><span data-i18n="off">（</span><span>${esc(why)}</span><span data-i18n="off">）</span></span>`;
}
function soEmpty(h, act) { return `<div class="empty"><h2>${h}</h2>${act || ""}</div>`; }
function soWait(what) {
  if (SO.err[what]) return soEmpty("无法读取社区", `<p class="warnc small">${esc(SO.err[what])}</p><button class="btn" data-so="refresh">重试</button>`);
  return `<div class="empty"><div class="rmbusy"><i class="spin"></i>正在读取…</div></div>`;
}
function soClosed(L) { return L && L.closed ? soEmpty("社区还没有开放") : ""; }

function soWorkCard(w) {
  const v = SO_VIDEO[w.video_kind];
  const followed = w.target in SO.follows ? SO.follows[w.target] : w.followed;
  return `<article class="card socard" data-so="work" data-id="${esc(w.id)}" tabindex="0">
    <div class="socover">${w.pic ? soSensWrap(soWorkMedia(w), `<img src="${esc(w.pic)}" alt="" loading="lazy" decoding="async">`) : `<div class="ph" data-i18n="off">${esc((w.title || "").slice(0, 16))}</div>`}
      <div class="sbadges">${v ? `<span class="sobadge">${ICON.play}<span>${esc(v)}</span></span>` : ""}${w.has_recipe ? `<span class="sobadge">附方案</span>` : ""}</div></div>
    <div class="body"><div class="title" data-i18n="off">${esc(w.title)}</div>
      <div class="soby">${soProfileBtn(w.authorId, w.author, w.nickname, w.authorPic, false, w.authorBooth)}${followed ? `<span class="tag faint">已关注</span>` : ""}</div>
      <div class="meta">${w.base ? `<span class="base" data-i18n="off">${esc(w.base)}</span>` : ""}<span class="solikes" title="点赞">${SO_ICON.like}<span class="num">${esc(soLikes(w.id, w.likes))}</span></span></div></div>
  </article>`;
}
function soWorksDraw() {
  const L = SO.asset ? SO.asset.data : SO.worksPage || SO.lists;
  if (!L) { soBar(null); gridPut(soWait("lists")); return; }
  if (!L.ok) { soBar(null); gridPut(soEmpty("无法读取社区", `<p class="warnc small">${esc(L.err || "")}</p>`)); return; }
  const list = soFilteredWorks(L.works || []);
  const title = SO.asset ? `<span class="soassettitle">${esc(`用到「${SO.asset.name}」的作品`)}</span><button class="btn small ghost" data-so="assetclear">返回</button>` : "";
  soBar(list.length, "个作品", title + soWorkControls(false));
  gridPut(soClosed(L) || (list.length ? `<div class="sogrid works">${list.map(soWorkCard).join("")}</div>`
    : soEmpty((L.works || []).length ? "没有符合条件的作品" : "还没有作品", `<button class="btn primary" data-so="newwork">发布作品</button>`)) + (L.next ? `<button class="btn sxmore" data-so="worksnext">加载更多</button>` : ""));
}
function soCreatorCard(c) {
  const target = c.target || (c.id || ("gh:" + (c.github || c.login)));
  const on = target in SO.follows ? SO.follows[target] : !!c.followed;
  return `<article class="socreator" data-so="profile" ${c.id ? `data-id="${esc(c.id)}"` : `data-github="${esc(c.github || c.login)}"`} tabindex="0">
    ${soAv(c.pic, c.nickname, "lg")}
    <div class="nm" data-i18n="off">${esc(c.nickname)}</div>${c.github ? `<div class="sub" data-i18n="off">@${esc(c.github)}</div>` : ""}
    ${(c.bases || []).length ? `<div class="bases">${c.bases.map(b => `<span class="base" data-i18n="off">${esc(b)}</span>`).join("")}</div>` : ""}
    <div class="meta"><span>${esc(c.followers || 0)} 位关注</span><span>${esc(c.works || 0)} 个作品</span>${c.recipes ? `<span>${esc(c.recipes)} 个方案</span>` : ""}</div>
    ${c.me ? `<span class="tag faint">我</span>` : `<button class="btn small sofollow${on ? " on" : ""}" data-so="follow" data-target="${esc(target)}" data-on="${!on}" aria-pressed="${on}">${on ? "已关注" : "关注"}</button>`}
  </article>`;
}
function soCreatorsDraw() {
  const L = SO.lists;
  if (!L) { soBar(null); gridPut(soWait("lists")); return; }
  const words = soWords(SO.q);
  const list = (L.creators || []).filter(c => soHit(words, c.nickname, c.github, c.login, (c.bases || []).join(" ")));
  soBar(list.length, "位创作者");
  gridPut(soClosed(L) || (list.length ? `<div class="sogrid creators">${list.map(soCreatorCard).join("")}</div>`
    : soEmpty((L.creators || []).length ? "没有符合条件的创作者" : "还没有创作者")));
}

// Explore keeps the existing library browsing and waterfall.
function soExploreNav() {
  return `<div class="sxexplore">${[["explore", "趋势"], ["groups", "小组"], ["asks", "求同款"], ["works", "作品"], ...SO_WEB ? [] : [["images", "图片"], ["creators", "创作者"], ["recipes", "装配方案"]]].map(([key, label]) => `<button class="chip${SO.nav === key ? " on" : ""}" data-so="nav" data-nav="${key}">${label}</button>`).join("")}</div>`;
}
// ---------- 首页 ----------
const SO_HTABS = [["recommend", "推荐"], ["following", "关注"], ["latest", "最新"]];
const SO_HSKEL = [236, 304, 208, 268, 232, 320, 244, 288];
let soHfObserver = null;

function soDiscoveryHTML() {
  const birthdays = (SO.lists?.birthdays || []).map(c => `<div>${soProfileBtn(c.id, c.github || c.login, c.nickname, c.pic, false, c.booth)} <span>今天生日</span></div>`).join("");
  const newcomers = SO.lists?.newcomers || [];
  const shops = typeof F === "undefined" ? [] : (F.shops || []).flatMap(s => (s.news || []).map(x => ({s, x}))).sort((a, b) => (b.x.at || 0) - (a.x.at || 0)).slice(0, 12);
  return `${birthdays ? `<section id="sx_birthdays">${birthdays}</section>` : ""}${newcomers.length ? `<section id="so_newcomers"><h4 class="soh">新人作品</h4><div class="sogrid works">${newcomers.map(soWorkCard).join("")}</div></section>` : ""}${shops.length ? `<section><h4 class="soh">关注的店铺上新</h4>${shops.map(({s, x}) => `<button class="btn" data-so="booth" data-id="${esc(x.id)}"><span data-i18n="off">${esc(x.title || x.id)} · ${esc(s.name || s.sub)}</span></button>`).join("")}</section>` : ""}`;
}
function soHfKey(it) { return it.kind + ":" + (it.kind === "work" ? it.work?.id : it.post?.id); }
async function soLoadHome(more, refresh) {
  if (!SO.hfeed) SO.hfeed = { items: [], next: "", tab: SO.htab, busy: false, loaded: false, error: "" };
  const H = SO.hfeed;
  if (H.busy || more && !H.next) return;
  H.busy = true; H.error = ""; SO.busy.home = true;
  if (!more) H.loaded = false;
  soDraw();
  let r;
  try { r = await api("/api/community/home", { tab: H.tab, after: more ? H.next : "" }); }
  catch (e) { r = { ok: false, err: "程序无响应" }; }
  H.busy = false;
  if (SO.hfeed === H) SO.busy.home = false;
  if (SO.hfeed !== H) return;
  if (!r.ok) {
    // a following tab with no account has nothing to show, not a failure to report
    if (r.accountInvalid && H.tab === "following") { H.loaded = true; H.next = ""; soDraw(); return; }
    H.error = r.err || "无法读取社区"; soDraw(); return;
  }
  const seen = new Set(H.items.map(soHfKey));
  for (const it of r.items || []) if (!seen.has(soHfKey(it))) H.items.push(it);
  H.next = r.next || ""; H.loaded = true;
  soDraw();
}
function soHfTab(tab) {
  if (!tab || tab === SO.htab) return;
  SO.htab = tab; SO.hfeed = null;
  soNeed(false); soDraw();
  const m = $(".main"); if (m) m.scrollTop = 0;
}
function soHfCover(pic) {
  return pic ? `<div class="sohcover"><img src="${esc(pic)}" alt="" loading="lazy" decoding="async"></div>` : "";
}
function soHfLikeHTML(kind, id, on, count) {
  return `<button class="sohlike${on ? " on" : ""}" data-so="homelike" data-kind="${kind}" data-id="${esc(id)}" aria-pressed="${!!on}" title="点赞">${SO_ICON.like}<span class="num">${esc(count || 0)}</span></button>`;
}
function soHfCard(it) {
  const foot = (pic, name, tag, like) => `<div class="sohfoot">${soAv(pic, name, "xs")}<span class="sohnm" data-i18n="off">${esc(name || "")}</span>${tag}${like}</div>`;
  if (it.kind === "work") {
    const w = it.work || {};
    return `<article class="sohcard" data-so="work" data-kind="work" data-id="${esc(w.id)}" tabindex="0">
      ${soSensWrap(soWorkMedia(w), soHfCover(w.pic), "card")}<div class="sohbody"><div class="sohtitle" data-i18n="off">${esc(w.title || "")}</div>
      ${foot(w.authorPic, w.nickname, `<span class="sohtag">作品</span>`, soHfLikeHTML("work", w.id, soLiked(w.id), soLikes(w.id, w.likes)))}</div></article>`;
  }
  const p = it.post || {};
  const line = String(p.text || "").split("\n")[0];
  return `<article class="sohcard" data-so="post" data-kind="post" data-id="${esc(p.id)}" tabindex="0">
    ${soSensWrap(p, soHfCover(p.pic || (p.pics || [])[0]), "card")}<div class="sohbody"><div class="sohtitle" data-i18n="off">${esc(line)}</div>
    ${foot(p.authorPic, p.author?.nickname, "", soHfLikeHTML("post", p.id, p.liked, p.likes))}</div></article>`;
}
function soHfSkeleton() {
  return `<div class="sohgrid">${SO_HSKEL.map(h => `<div class="sohskeleton" style="height:${h}px" aria-hidden="true"></div>`).join("")}</div>`;
}
function soHfWatch() {
  if (soHfObserver) { soHfObserver.disconnect(); soHfObserver = null; }
  const el = $("#so_home_more");
  if (!el) return;
  if (typeof IntersectionObserver === "undefined") { el.classList.add("on"); return; }
  soHfObserver = new IntersectionObserver(entries => {
    if (!entries.some(x => x.isIntersecting)) return;
    // the first callback can arrive before the covers have their height, when the sentinel still sits
    // inside the viewport of a page that is really taller; look again once the pictures are laid out
    setTimeout(() => {
      if (!el.isConnected || SO.hfeed?.busy) return;
      const m = $(".main");
      if (!m) return;
      const r = el.getBoundingClientRect(), box = m.getBoundingClientRect();
      if (r.top <= box.bottom + 240 && r.bottom >= box.top) soLoadHome(true);
    }, 500);
  }, { root: $(".main"), rootMargin: "240px" });
  soHfObserver.observe(el);
}
function soHfDraw() {
  const H = SO.hfeed;
  const tabs = `<div class="sohtabs" role="tablist">${SO_HTABS.map(([k, t]) =>
    `<button class="chip sohtab${SO.htab === k ? " on" : ""}" role="tab" aria-selected="${SO.htab === k}" data-so="htab" data-tab="${k}">${t}</button>`).join("")}</div>`;
  soBar(H && H.loaded ? H.items.length : null, "", tabs);
  if (!H || !H.loaded && !H.error) { gridPut(`<div class="sowrap sohwrap">${soHfSkeleton()}</div>`); return; }
  let body;
  if (H.error && !H.items.length) body = soEmpty("无法读取社区", `<p class="warnc small">${esc(H.error)}</p><button class="btn" data-so="homeretry">重试</button>`);
  else if (!H.items.length && H.tab === "following") body = soEmpty("还没有关注任何人", `<button class="btn primary" data-so="htab" data-tab="recommend">去看看推荐</button>`);
  else if (!H.items.length) body = soEmpty("暂无内容");
  else {
    const more = H.next ? `<div class="sohmore" id="so_home_more">${H.busy ? `<i class="spin"></i>` : ""}</div>` : "";
    const end = H.loaded && !H.next ? `<p class="sohend">没有更多了</p>` : "";
    body = `<div class="sohgrid">${H.items.map(soHfCard).join("")}</div>${more}${end}`;
  }
  gridPut(`<div class="sowrap sohwrap">${soDiscoveryHTML()}${body}${H.error && H.items.length ? `<p class="warnc small">${esc(H.error)}</p>` : ""}</div>`);
  soHfWatch();
}
async function soHfLike(btn) {
  const kind = btn.dataset.kind, id = btn.dataset.id;
  const key = "homelike:" + id;
  if (SO.busy[key]) return;
  await soServiceLoad(false);
  if (!SO.service?.account) { soAccountDialog(() => soHfLike(btn)); return; }
  const on = btn.getAttribute("aria-pressed") !== "true";
  SO.busy[key] = true; btn.disabled = true;
  const r = kind === "post"
    ? await soServiceAPI("post/like", { id, on })
    : await soServiceAPI("like", { id, on });
  SO.busy[key] = false; btn.disabled = false;
  if (!r.ok) { soServiceError(r); return; }
  const item = (SO.hfeed?.items || []).find(x => soHfKey(x) === kind + ":" + id);
  let count;
  if (kind === "post") {
    if (item?.post) {
      item.post.liked = on;
      item.post.likes = Math.max(0, (item.post.likes || 0) + (on ? 1 : -1));
      count = item.post.likes;
    }
  } else if (item?.work) {
    item.work.likes = Math.max(0, (item.work.likes || 0) + (on ? 1 : -1));
    if (r.stats) SO.stats = r.stats;
    else {
      if (!SO.stats) SO.stats = { works: {}, myLikes: [] };
      SO.stats.myLikes = (SO.stats.myLikes || []).filter(x => x !== id);
      if (on) SO.stats.myLikes.push(id);
      if (SO.stats.works?.[id]) SO.stats.works[id].likes = Math.max(0, SO.stats.works[id].likes + (on ? 1 : -1));
    }
    SO.serviceAt = 0;
    count = soLikes(id, item.work.likes);
  }
  if (count === undefined) count = Math.max(0, parseInt(btn.querySelector(".num")?.textContent || "0", 10) + (on ? 1 : -1));
  btn.classList.toggle("on", on);
  btn.setAttribute("aria-pressed", String(on));
  const num = btn.querySelector(".num"); if (num) num.textContent = String(count);
}

// ---------- 装配方案 ----------
function soRecipesDraw() {
  const c = SO.com;
  if (!c) { soBar(null); gridPut(soWait("com")); return; }
  const words = soWords(SO.q);
  const rows = (c.recipes || []).filter(e => soHit(words, e.name, e.note, e.base, e.author, e.login, (e.links || []).map(l => l.label).join(" ")));
  soBar(rows.length, "个方案", c.offline ? soOffline(c.offline) : `<button class="btn small" data-so="recipeshare">分享方案</button>`);
  gridPut(rows.length ? `<div class="sowrap"><div class="rcplist sorecipes">${rows.slice(0, 200).map(soRecipeRow).join("")}</div></div>`
    : soEmpty((c.recipes || []).length ? "没有符合条件的方案" : "还没有方案", `<button class="btn primary" data-so="recipeshare">分享方案</button>`));
}
function soRecipeRow(e) {
  const n = e.items || 0, meta = [];
  if (e.base) meta.push(`<span data-i18n="off">${esc(e.base)}</span>`);
  if (n) meta.push(`<span>${esc(n)} 项素材</span>`);
  if (e.added) meta.push(`<span>${esc(e.added)}</span>`);
  const mine = e.account && e.account === soAccountID() || e.login && soMe() && e.login.toLowerCase() === soMe().toLowerCase();
  return `<div class="rcp" data-cid="${esc(e.id)}">
    <div class="rcpinfo"><div class="nm" data-i18n="off">${esc(e.name)}</div>
      <div class="meta">${e.account ? soProfileBtn(e.account, e.login, e.author || e.login) : e.login ? soCreatorBtn(e.login, e.author || e.login) : e.author ? `<span data-i18n="off">${esc(e.author)}</span>` : ""}${meta.join(" · ")}</div>
      ${e.note ? `<div class="note" data-i18n="off">${esc(e.note)}</div>` : ""}${socialLinksHTML(e.links)}</div>
    <div class="rcpacts"><button class="btn small primary" data-so="recipe" data-id="${esc(e.id)}">查看</button>${!mine ? `<button class="btn small ghost" data-so="report" data-kind="recipe" data-id="${esc(e.id)}" data-name="${esc(e.name)}" title="举报" aria-label="举报">${SO_ICON.flag}</button>` : ""}</div></div>`;
}
async function soRecipeView(id) {
  let r; try { r = await api("/api/community/recipe", { id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!r.ok && (r.unavailable || (SO.com?.recipes || []).some(e => e.id === id && e.source === "github"))) {
    try { r = await api("/api/recipe/community/get", { id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  }
  if (!r.ok) { toast(r.err || "无法读取该方案", 5000); return; }
  if (SO.com) RCP.com = { rows: SO.com.recipes };
  RCP.imp = { text: r.text, recipe: r.recipe, needs: r.needs, community: id };
  recipeShow("import");
}
function soRecipeShare(id) {
  const m = $("#modal");
  SO.share = { id: id || "", title: "", note: "", base: "", busy: false };
  m.dataset.kind = "soreport";
  const fill = async () => {
    const s = SO.share; if (!s || !s.id) return;
    let r; try { r = await api("/api/recipe/get", { id: s.id }); } catch (e) { r = { ok: false }; }
    if (SO.share !== s || !s.id) return;
    if (r.ok) { s.title = r.recipe.name || ""; s.note = r.recipe.note || ""; s.base = (r.recipe.base && r.recipe.base.name) || ""; }
    draw();
  };
  const draw = () => {
    const s = SO.share, list = (typeof RCP !== "undefined" && RCP.list) || [];
    m.innerHTML = `<div class="mhead">分享方案</div><div class="mbody soform">
      <div class="row"><label for="sr_pick">方案</label><select id="sr_pick"><option value="">选择方案</option>${list.map(r => `<option value="${esc(r.id)}"${r.id === s.id ? " selected" : ""} data-i18n="off">${esc(r.name)}</option>`).join("")}</select></div>
      <div class="row"><label for="sr_title">名称</label><input id="sr_title" maxlength="60" value="${esc(s.title)}"></div>
      <div class="row"><label for="sr_note">说明（选填）</label><textarea id="sr_note" maxlength="300">${esc(s.note)}</textarea></div>
      <div class="row"><label for="sr_base">素体（选填）</label><input id="sr_base" maxlength="24" value="${esc(s.base)}"></div>
      <p class="warnc small" id="sr_error" role="status"></p></div>
    <div class="mfoot"><button class="btn" data-m="close">取消</button><button class="btn primary" data-so="recipesharego"${!s.id ? " disabled" : ""}>分享</button></div>`;
    showModal();
    $("#sr_pick").onchange = ev => { s.id = ev.target.value; draw(); fill(); };
  };
  draw();
  if (id) fill();
  if (typeof RCP !== "undefined" && RCP.list === null && typeof recipeLoad === "function") recipeLoad().then(draw);
}
async function soRecipeShareGo(btn) {
  const s = SO.share; if (!s || !s.id || s.busy) return;
  s.title = $("#sr_title").value.trim(); s.note = $("#sr_note").value.trim(); s.base = $("#sr_base").value.trim();
  if (!s.title) { $("#sr_title").focus(); return; }
  btn.disabled = true; s.busy = true;
  let code;
  try { const c = await api("/api/recipe/code", { id: s.id }); code = c.ok ? c.code : ""; } catch (e) { code = ""; }
  if (!code) { s.busy = false; btn.disabled = false; $("#sr_error").textContent = "无法读取该方案"; return; }
  const r = await soServiceAPI("recipe/share", { code, title: s.title, note: s.note, base: s.base });
  s.busy = false;
  if (!r.ok) { btn.disabled = false; $("#sr_error").textContent = r.err || "无法分享"; return; }
  closeModal(); toast("已分享"); SO.com = null; if (SO.nav === "recipes") soLoad("com", true);
}

// ---------- a player's page ----------
async function soProfile(src, refresh) {
  const previous = SO.page && JSON.stringify(SO.page.src) === JSON.stringify(src) ? SO.page : null;
  SO.page = {src: src || {}, data: previous?.data, tab: previous?.tab || "posts", posts: null};
  const page = SO.page;
  closeModal(); if (S.view !== "social") setView("social");
  soDraw(); $(".main").scrollTop = 0;
  let r; try { r = await api("/api/community/profile", Object.assign({ refresh: !!refresh }, src)); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (SO.page !== page) return;
  SO.page.data = r;
  soDraw();
  if (r.ok) {
    if (r.target) SO.follows[r.target] = !!r.followed;
    if (r.showcase?.length) {
      try {
        const own = await api("/api/community/owned", { booth_ids: r.showcase.map(a => a.boothId) });
        if (own.ok) for (const a of r.showcase) { a.owned = !!own.owned[a.boothId]; a.wished = !!own.wished[a.boothId]; }
      } catch (e) {}
    }
  }
  if (SO.page === page) {
    if (r.readOnly && page.tab === "posts") page.tab = "works";
    if (r.ok) await soProfilePosts();
    soDraw();
  }
}
function soCreator(login, refresh) { return soProfile({ github: String(login || "").replace(/^gh:/, "") }, refresh); }
function soProfileFrom(el) {
  const src = el.dataset.id ? { id: el.dataset.id } : { github: el.dataset.github || el.dataset.login };
  return soProfile(src);
}
function soCardHead(c, pic, r, acts) {
  const langs = (c.langs || []).map(l => (SO_LANGS.find(x => x[0] === l) || [l, l])[1]);
  const bd = c.birthday && c.show_birthday !== false ? `<span>${esc(`生日 ${c.birthday.month} 月 ${c.birthday.day} 日`)}</span>` : "";
  return `<div class="sohead">${soAv(pic, c.nickname, "xl")}<div class="sohmain">
    <h2 data-i18n="off">${esc(c.nickname || c.github || "")}</h2>
    <div class="sub">${c.github ? `<span data-i18n="off">@${esc(c.github)}</span>` : ""}${langs.length ? `<span>${langs.map(esc).join(" · ")}</span>` : ""}${bd}</div>
    ${(c.bases || []).length ? `<div class="bases">${c.bases.map(b => `<span class="base" data-i18n="off">${esc(b)}</span>`).join("")}</div>` : ""}
    ${c.bio ? `<div class="sobio" data-i18n="off">${esc(c.bio)}</div>` : ""}
    <div class="btnrow">${acts || ""}${c.vrchat ? `<button class="btn small" data-so="url" data-href="${esc(c.vrchat)}">VRChat 主页</button>` : ""}</div>
    ${socialLinksHTML(c.links)}</div></div>`;
}
function soFollowBtn(target, followed, followsMe) {
  return `<button class="btn small sofollow${followed ? " on" : " primary"}" data-so="follow" data-target="${esc(target)}" data-on="${!followed}" aria-pressed="${followed}"><span class="sofollowlabel">${followed ? "正在关注" : followsMe ? "回关" : "关注"}</span>${followed ? `<span class="sofollowhover">取消关注</span>` : ""}</button>`;
}
function soPageDraw() {
  const p = SO.page, r = p.data;
  put($("#resultbar"), `<button class="btn small ghost" data-so="back">${ICON.back}<span>返回</span></button><span class="spacer"></span><button class="btn small ghost" data-so="refresh">${ICON.upd}<span>刷新</span></button>`);
  if (!r) { gridPut(soEmpty("正在读取…")); return; }
  if (!r.ok) { gridPut(soEmpty("无法读取该页面", `<p class="warnc small">${esc(r.err || "")}</p>`)); return; }
  const c = r.profile, target = r.target, blocked = !!c.blocked;
  const tabs = r.readOnly ? [["works", "作品", "work"]] : [["posts", "帖子", "posts"], ["replies", "回复", "reply"], ["media", "媒体", "picture"], ...(r.me ? [["likes", "喜欢", "like"]] : []), ["works", "作品", "work"]];
  if (r.showcase?.length) tabs.push(["showcase", "展示柜", "showcase"]);
  if (!tabs.some(([k]) => k === p.tab)) p.tab = tabs[0][0];
  const acts = r.canEdit ? `<button class="btn" data-so="nav" data-nav="edit">编辑资料</button>` : `${!blocked ? soFollowBtn(target, r.followed, r.followsMe) : ""}${!r.readOnly ? `<button class="btn" data-so="dmstart"${blocked ? " disabled" : ""}>私信</button>` : ""}<button class="btn" data-so="pagemenu" aria-label="更多">⋯</button>`;
  const counts = c.counts || {}, count = (kind, label, allowed) => `<${allowed ? 'button' : 'span'} class="sxcount"${allowed ? ` data-so="pagepeople" data-kind="${kind}"` : ""}><b>${esc(counts[kind] || 0)}</b> <span>${label}</span></${allowed ? 'button' : 'span'}>`;
  const joined = new Date(c.created), date = isNaN(joined) ? "" : joined.toLocaleDateString(I18N.lang || "zh-CN", {year: "numeric", month: "long"});
  const offer = SO.offer?.target === target ? SO.offer.shops.filter(s => !followHas(s.sub)) : [];
  const shop = offer.length ? `<div class="sooffer">${offer.map(s => `<span>${esc(`同时关注 TA 的 Booth 店铺「${s.name}」？`)}</span><button class="btn small" data-so="followshop" data-sub="${esc(s.sub)}" data-name="${esc(s.name)}">关注店铺</button>`).join("")}<button class="wb" data-so="nooffer" title="不用了" aria-label="不用了">${ICON.x}</button></div>` : "";
  let content;
  if (p.tab === "showcase") content = soShowcaseHTML(r.showcase, true);
  else if (p.tab === "works") {
    const works = r.works || [], recipes = r.recipes || [];
    content = `<div class="sxprofileworks"><div class="sxworknumbers"><span>${esc(works.length)} 个作品</span><span>${esc(`获赞 ${works.reduce((n,w) => n + soLikes(w.id, w.likes), 0)}`)}</span></div><p class="soremixcount">${esc(`被做同款 ${r.remixCount || 0} 次`)}</p>${r.readOnly && p.posts?.loaded ? soPostListHTML(p.posts, false, "pagepostmore") : works.length ? `<div class="sogrid works">${works.map(soWorkCard).join("")}</div>` : soEmpty("还没有作品")}${p.workNext ? `<button class="btn" data-so="pageworkmore">加载更多</button>` : ""}<h4 class="soh">装配方案 <span class="n">${esc(recipes.length)}</span></h4>${recipes.map(x => `<div class="rcp"><div class="rcpinfo"><div class="nm" data-i18n="off">${esc(x.name)}</div><div class="meta">${x.base ? `<span data-i18n="off">${esc(x.base)}</span> · ` : ""}<span>${esc(x.items || 0)} 项素材</span></div>${socialLinksHTML(x.links)}</div><div class="rcpacts"><button class="btn small" data-so="recipe" data-id="${esc(x.id)}">查看</button></div></div>`).join("") || `<p class="muted small">还没有方案</p>`}</div>`;
  } else content = soPostListHTML(p.posts, p.tab === "media", "pagepostmore");
  gridPut(`<div class="sxtimeline sopage sxprofile">${r.offline ? `<p>${soOffline(r.offline)}</p>` : ""}<div class="sxbanner"></div><div class="sxprofilehead"><div class="sxprofiletop">${soAv(r.pic, c.nickname, "xl")}<div class="btnrow">${acts}</div></div><h2><span data-i18n="off">${esc(c.nickname)}</span>${soVerified(c.booth)}${soBotMark(c)}</h2><div class="sxhandle" data-i18n="off">@${esc(c.handle || c.github || "")}</div>${blocked ? `<p>已拉黑</p>` : ""}<div class="sobio sxtext" data-i18n="off">${soPostText(c.bio || "")}</div><div class="sub">${(c.langs || []).map(l => `<span>${esc((SO_LANGS.find(x => x[0] === l) || [l,l])[1])}</span>`).join(" · ")}${c.birthday && c.show_birthday !== false ? `<span>${esc(`生日 ${c.birthday.month} 月 ${c.birthday.day} 日`)}</span>` : ""}${date ? `<span>${esc(`加入于 ${date}`)}</span>` : ""}</div><div class="bases">${(c.bases || []).map(b => `<span class="base" data-i18n="off">${esc(b)}</span>`).join("")}</div><div class="sxprofilelinks">${c.vrchat ? `<button class="btn small" data-so="url" data-href="${esc(c.vrchat)}">VRChat 主页</button>` : ""}${c.booth ? `<button class="btn small" data-so="url" data-href="${esc(`https://${c.booth}.booth.pm/`)}">Booth 店铺</button>` : ""}${socialLinksHTML(c.links)}${soContactsHTML(c.contacts)}</div><div class="socreatornumbers">${count("following", "正在关注", !r.readOnly && (r.me || c.show_following))} · ${count("followers", "关注者", !r.readOnly && r.me)}</div>${shop}</div>${soTabs(tabs, p.tab, "pagetab")}${content}</div>`);
}
async function soFollow(target, on, btn) {
  if (!target) return;
  if (!SO.service?.account) { soAccountDialog(() => soFollow(target, on)); return; }
  if (btn) btn.disabled = true;
  let r; try { r = await api("/api/community/follow", { target, on }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (btn) btn.disabled = false;
  if (!r.ok) { soServiceError(r); return; }
  SO.follows[target] = on;
  if (on) SO.offer = (r.shops || []).length ? { target, shops: r.shops } : null;
  else if (SO.offer && SO.offer.target === target) SO.offer = null;
  for (const w of (SO.lists && SO.lists.works) || []) if (w.target === target) w.followed = on;
  for (const c of (SO.lists && SO.lists.creators) || []) if ((c.target || "") === target) c.followed = on;
  if (SO.page?.data?.ok && (SO.page.data.target === target || (SO.page.data.github && "gh:" + SO.page.data.github === target))) SO.page.data.followed = on;
  const post = SO.post?.data?.ok ? SO.post.data.post : null;
  if (post && soPostTarget(post) === target) post.followed = on;
  for (const v of (SO.posts && SO.posts.items) || []) if (soPostTarget(v) === target) v.followed = on;
  toast(on ? "已关注" : "已取消关注");
  SO.feed = null;
  if (SO.nav === "feed" && !SO.page) soLoad("feed");
  if ($("#modal")?.dataset.kind === "sopost") { soPostDraw(); return; }
  if (SO.offer && SO.offer.shops.some(s => !followHas(s.sub)) && (!SO.page || SO.page.data?.target !== target)) {
    await soProfile(/^u[0-9a-z]{12}$/.test(target) ? { id: target } : { github: target.replace(/^gh:/, "") }, false);
    return;
  }
  if (SO.page) soProfile(SO.page.src);
}

// ---------- 我的主页 ----------
function soHomeDraft() {
  if (SO.home) return SO.home;
  const me = SO.me || {}, p = me.profile || me.account || {}, d = me.draft || {};
  const src = me.signedIn ? p : d;
  SO.home = {
    nickname: src.nickname || "", bio: src.bio || "", langs: (src.langs || []).slice(), vrchat: src.vrchat || "",
    bases: (src.bases || []).join("，"), links: (src.links || []).map(l => ({ url: l.url, label: l.label || "", kind: l.kind })),
    month: src.birthday ? src.birthday.month : 0, day: src.birthday ? src.birthday.day : 0,
    showBirthday: !!(src.show_birthday ?? src.showBirthday), showFollowing: !!(src.show_following ?? src.showFollowing),
    showcase: (src.showcase || []).map(a => ({ booth_id: a.booth_id || a.boothId, name: a.name, shop: a.shop || "" })),
    avatar: p.avatar || "", pic: me.pic || "", uploaded: "", busy: false, showcaseQuery: "",
  };
  return SO.home;
}
function soHomeRead() {
  const c = SO.home; if (!c || !$("#sh_nick")) return c;
  c.nickname = $("#sh_nick").value; c.bio = $("#sh_bio").value; c.vrchat = $("#sh_vrc").value.trim(); c.bases = $("#sh_bases").value;
  c.langs = [...document.querySelectorAll("#sh_langs input:checked")].map(i => i.value);
  c.month = +$("#sh_month").value || 0; c.day = +$("#sh_day").value || 0;
  c.showBirthday = !!(c.month && c.day);
  const old = c.links; c.links = soLinkRead("sh_links").map((l, i) => Object.assign(l, { kind: old[i] && old[i].url === l.url ? old[i].kind : "" }));
  return c;
}
function soHomeReq(c) {
  const req = { nickname: c.nickname.trim(), bio: c.bio.trim(), langs: c.langs, vrchat: soNormURL(c.vrchat),
    bases: c.bases.split(/[,，、\n]/).map(s => s.trim()).filter(Boolean).slice(0, 20),
    links: c.links.filter(l => l.url).map(l => ({ url: l.url, label: l.label })), avatar: c.avatar || "",
    showBirthday: !!(c.month && c.day), showFollowing: c.showFollowing,
    showcase: c.showcase.map(a => ({ booth_id: a.booth_id, name: a.name, shop: a.shop || "" })) };
  if (c.month && c.day) req.birthday = { month: c.month, day: c.day };
  if (c.uploaded && c.avatarFlag) req.auto_flag = true; // (the new avatar, until it is saved)
  return req;
}
function soHomeDraw() {
  const me = SO.me;
  soBar(null);
  if (!me) { gridPut(soWait("me")); return; }
  if (!SO.service?.account) {
    gridPut(`<div class="sowrap">${soAccountHTML()}<div class="empty"><button class="btn primary" data-so="account">登录社区</button></div></div>`);
    return;
  }
  const c = soHomeDraft(), days = c.month ? [31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][c.month - 1] : 31;
  const sel = (id, n, v, first, label) => `<select id="${id}" aria-label="${label}"><option value="0">${first}</option>${Array.from({ length: n }, (_, i) => `<option value="${i + 1}"${v === i + 1 ? " selected" : ""}>${i + 1}</option>`).join("")}</select>`;
  const count = (id, v, max) => `<span class="sxlen" id="${id}_n">${Array.from(v || "").length}/${max}</span>`;
  const mine = ((SO.lists && SO.lists.works) || []).filter(w => w.mine);
  gridPut(`<div class="sxtimeline sxedit">
    <div class="sxedittop"><button class="btn small ghost" data-so="nav" data-nav="me">${ICON.back || ""}<span>返回</span></button><h2>编辑资料</h2><span class="spacer"></span><button class="btn primary" data-so="homesave"${c.busy ? " disabled" : ""}>保存</button></div>
    <div class="sxbanner"></div>
    <div class="sxeditav"><button class="sxavedit" data-so="avpick" title="更换头像" aria-label="更换头像">${soAv(c.uploaded || c.avatar ? (c.uploaded || c.pic) : me.pic, c.nickname, "xl")}<span class="sxavcam">${SO_TOOL.camera}</span></button>
      ${c.avatar ? `<button class="btn small ghost" data-so="avclear">移除头像</button>` : ""}</div>
    <div class="soform sxeditform" id="sh_form">
      <div class="sxfield"><div class="sxfieldk"><label for="sh_nick">昵称</label>${count("sh_nick", c.nickname, 24)}</div><input id="sh_nick" maxlength="24" value="${esc(c.nickname)}" autocomplete="off"></div>
      <div class="sxfield"><div class="sxfieldk"><span>用户名</span></div><div class="sxfieldline"><span data-i18n="off">@${esc(SO.service.account.handle || "")}</span><button class="btn small" data-so="handle">更改</button></div></div>
      <div class="sxfield"><div class="sxfieldk"><label for="sh_bio">简介</label>${count("sh_bio", c.bio, 160)}</div><textarea id="sh_bio" maxlength="160" rows="3" placeholder="介绍一下自己">${esc(c.bio)}</textarea></div>
      <div class="sxfield"><div class="sxfieldk"><span>语言</span></div><div class="chips solangs sxchecks" id="sh_langs">${SO_LANGS.map(([k, t]) => `<label class="check"><input type="checkbox" value="${k}"${c.langs.includes(k) ? " checked" : ""}> ${esc(t)}</label>`).join("")}</div></div>
      <div class="sxfield"><div class="sxfieldk"><label for="sh_bases">常用素体</label></div><input id="sh_bases" value="${esc(c.bases)}" placeholder="Plum，Chocolat" autocomplete="off"></div>
      <div class="sxfield"><div class="sxfieldk"><label for="sh_vrc">VRChat 主页</label></div><input id="sh_vrc" value="${esc(c.vrchat)}" placeholder="vrchat.com/home/user/usr_…" spellcheck="false" autocomplete="off"></div>
      <div class="sxfield"><div class="sxfieldk"><span>链接</span></div>${soLinkRows("sh_links", c.links, 8)}</div>
      <div class="sxfield"><div class="sxfieldk"><span>生日</span></div><div class="sobd">${sel("sh_month", 12, c.month, "月", "月")}${sel("sh_day", days, c.day, "日", "日")}${c.month || c.day ? `<button class="btn small ghost" data-so="bdclear">清除</button>` : ""}</div></div>
      <div class="sxfield"${SO_WEB ? " hidden" : ""}><div class="sxfieldk"><span>素材展示柜</span></div>
        <div id="sh_showcase_picker"><input id="sh_showcase_q" value="${esc(c.showcaseQuery || "")}" placeholder="搜索有 Booth 商品编号的素材" autocomplete="off">
          <div class="btnrow"><button class="btn small" data-so="showcaseall">全选</button><button class="btn small" data-so="showcasenone">全不选</button><span class="muted small" id="sh_showcase_count">${esc(`已选择 ${c.showcase.length}/60 件`)}</span></div>
          <div class="solibrows" id="sh_showcase_rows">${soShowcaseChoices(c)}</div></div></div>
    </div>
    <h4 class="soh sxeditworks">我的作品 <span class="n">${esc(mine.length)}</span><span class="spacer"></span><button class="btn small" data-so="newwork">${ICON.plus}<span>发布作品</span></button></h4>
    ${mine.length ? `<div class="sogrid works sxeditworkgrid">${mine.map(soWorkCard).join("")}</div>` : `<p class="muted small sxeditnone">还没有作品</p>`}</div>`);
}
function soHomePreview(c) {
  const req = soHomeReq(c);
  return soCardHead({ nickname: req.nickname || "（昵称）", github: soMe(), bio: req.bio, langs: req.langs, bases: req.bases, vrchat: req.vrchat,
    birthday: req.showBirthday && req.birthday ? req.birthday : null, links: c.links.filter(l => l.url && l.kind) },
    c.uploaded || (c.avatar ? c.pic : ""), null, "")
    + soShowcaseHTML(req.showcase.map(a => ({ ...a, boothId: a.booth_id })), false);
}
function soHomePreviewDraw() { const p = $("#sh_preview"); if (p && SO.home) put(p, soHomePreview(SO.home)); }
async function soHomeSave(quiet) {
  const c = soHomeRead(); if (!c || c.busy) return false;
  if (!c.nickname.trim()) { toast("请填写昵称"); $("#sh_nick")?.focus(); return false; }
  c.busy = true;
  const r = await soServiceAPI("profile/save", soHomeReq(c));
  c.busy = false;
  if (!r.ok) { soServiceError(r); return false; }
  if (r.account) { if (SO.me) SO.me.account = r.account; if (SO.service) SO.service.account = r.account; }
  if (r.profile && SO.me) { SO.me.profile = r.profile; SO.me.pic = r.pic; }
  c.avatar = (r.account && r.account.avatar) || c.uploaded || c.avatar; c.uploaded = ""; c.pic = r.pic || c.pic;
  SO.lists = null; SO.feed = null;
  if (!quiet) toast("已保存");
  soDraw();
  return true;
}
function soShowcaseHTML(items, compare) {
  if (!items || !items.length) return "";
  const missing = items.filter(a => !a.owned);
  const rows = (list, wish) => list.map(a => `<li><span class="soshowname" data-i18n="off">${esc(a.name)}</span><span class="muted small" data-i18n="off">${esc(a.shop || "")}</span>
    <button class="btn small ghost" data-so="link" data-kind="booth_item" data-href="${esc(boothLinkURL(a.boothId))}">打开 Booth</button>
    ${wish ? `<button class="btn small" data-so="showcasewish" data-id="${esc(a.boothId)}"${a.wished ? " disabled" : ""}>${a.wished ? "已在愿望单" : "加入愿望单"}</button>` : ""}</li>`).join("");
  return `<section class="soshowcase"><h4 class="soh">素材展示柜</h4>${compare ? `<p class="socommon">${esc(`你们都有 ${items.length - missing.length} 件相同的素材`)}</p>
    <h5 class="somissing">${esc(`TA 有、你没有的 ${missing.length} 件`)}</h5><ul class="soshowitems somissingitems">${rows(missing, true)}</ul><h5>全部素材</h5>` : ""}
    <ul class="soshowitems soallitems">${rows(items, false)}</ul></section>`;
}
function soShowcaseLibrary(c) {
  const all = new Map();
  for (const a of S.data?.assets || []) {
    const id = String(a.boothId || "");
    if (!/^[0-9]{1,12}$/.test(id) || all.has(id)) continue;
    const x = soAssetOf(a);
    all.set(id, { booth_id: id, name: Array.from(x.name).slice(0, 80).join(""), shop: Array.from(x.shop || "").slice(0, 40).join("") });
  }
  for (const a of c.showcase) if (!all.has(a.booth_id)) all.set(a.booth_id, a);
  return [...all.values()].filter(a => soHit(soWords(c.showcaseQuery), a.name, a.shop, a.booth_id));
}
function soShowcaseChoices(c) {
  return soShowcaseLibrary(c).map(a => `<label class="check"><input type="checkbox" class="so_showcase_item" value="${esc(a.booth_id)}"${c.showcase.some(x => x.booth_id === a.booth_id) ? " checked" : ""}>
    <span data-i18n="off">${esc(a.name)}</span><span class="muted small" data-i18n="off">${esc(a.shop || "")}</span></label>`).join("") || `<p class="muted small">没有符合条件的素材</p>`;
}
function soShowcaseRefresh() {
  const c = SO.home;
  put($("#sh_showcase_rows"), soShowcaseChoices(c));
  put($("#sh_showcase_count"), esc(`已选择 ${c.showcase.length}/60 件`));
  soHomePreviewDraw();
}
async function soShowcaseWish(btn) {
  const page = SO.page, a = page?.data?.showcase?.find(a => a.boothId === btn.dataset.id);
  if (!a || a.wished || a.owned) return;
  btn.disabled = true;
  try {
    const own = await api("/api/community/owned", { booth_ids: [a.boothId] });
    if (!own.ok) throw new Error(own.err || "操作失败");
    a.owned = !!own.owned[a.boothId]; a.wished = !!own.wished[a.boothId];
    if (!a.owned && !a.wished) {
      const r = await api("/api/wish/add", { id: a.boothId, title: a.name, shop: a.shop || "" });
      if (!r.ok) throw new Error(r.err || "操作失败");
      a.wished = true;
      if (typeof storesFetch === "function") storesFetch();
    }
  } catch (e) { toast(e.message || "程序无响应", 5000); }
  finally { if (SO.page === page) soPageDraw(); }
}

// ---------- discovery controls ----------
function soFilteredWorks(works, now = Date.now()) {
  const words = soWords(SO.q), age = w => (now - Date.parse(w.created)) / 86400000;
  const score = w => soLikes(w.id, w.likes) / (Math.max(0, age(w)) + 1);
  return works.filter(w => (!SO.base || w.base === SO.base) && (!SO.recipeOnly || w.has_recipe) && (!SO.videoOnly || w.video_kind)
    && (SO.sort !== "popular" || age(w) >= 0 && age(w) <= 30) && soHit(words, w.title, w.nickname, w.author, w.base))
    .sort((a, b) => (SO.sort === "popular" ? score(b) - score(a) : 0) || (Date.parse(b.created) || 0) - (Date.parse(a.created) || 0) || a.id.localeCompare(b.id));
}
function soWorkControls(siteWide) {
  const followed = (SO.st?.bases || []).some(b => b.base === SO.base);
  return `<div class="soworkcontrols"><button class="chip${SO.sort === "latest" ? " on" : ""}" data-so="sort" data-sort="latest" aria-pressed="${SO.sort === "latest"}">${siteWide ? "全站最新" : "最新"}</button>
    <button class="chip${SO.sort === "popular" ? " on" : ""}" data-so="sort" data-sort="popular" aria-pressed="${SO.sort === "popular"}">本周热门</button>
    <button class="chip${SO.recipeOnly ? " on" : ""}" data-so="recipefilter" aria-pressed="${SO.recipeOnly}">有方案</button>
    <button class="chip${SO.videoOnly ? " on" : ""}" data-so="videofilter" aria-pressed="${SO.videoOnly}">有视频</button>
    ${SO.base ? `<button class="clear" data-so="base" data-base="">${esc(`素体：${SO.base}`)} ×</button><button class="btn small" data-so="basefollow" aria-pressed="${followed}">${followed ? "取消关注这个素体" : "关注这个素体"}</button>` : ""}</div>`;
}
function soBaseChips() {
  const count = new Map();
  for (const w of SO.lists?.works || []) if (w.base) count.set(w.base, (count.get(w.base) || 0) + 1);
  const list = [...count.keys()].sort((a, b) => count.get(b) - count.get(a) || a.localeCompare(b));
  if (!list.length) return "";
  const visible = SO.basesMore ? list : list.slice(0, 12);
  return `<h3>素体</h3><div class="chips sobases">${visible.map(b => `<button class="chip${SO.base === b ? " on" : ""}" data-so="base" data-base="${esc(b)}" aria-pressed="${SO.base === b}" data-i18n="off">${esc(b)}<span class="n">${count.get(b)}</span></button>`).join("")}
    ${list.length > 12 ? `<button class="chip" data-so="basesmore">${SO.basesMore ? "收起" : "更多"}</button>` : ""}</div>`;
}
async function soFollowBase(btn) {
  const base = SO.base, on = !(SO.st?.bases || []).some(b => b.base === base);
  btn.disabled = true;
  try {
    const r = await api("/api/community/base/follow", { base, on });
    if (!r.ok) { toast(r.err || "操作失败"); return; }
    if (!SO.st) SO.st = {};
    SO.st.bases = r.bases; SO.feed = null;
    toast(on ? "已关注" : "已取消关注"); soDraw();
  } catch (e) { toast("程序无响应"); } finally { btn.disabled = false; }
}
async function socialByAsset(id, name) {
  const current = { id, name: name || id, data: null };
  SO.asset = current; SO.nav = "works"; SO.page = null; SO.base = ""; SO.q = "";
  SO.sort = "latest"; SO.recipeOnly = SO.videoOnly = false;
  if ($("#q")) $("#q").value = "";
  closeModal(); if (S.view !== "social") setView("social"); soDraw();
  let r;
  try { r = await api("/api/community/asset", { booth_id: id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (SO.asset !== current) return;
  current.data = r; soDraw();
}
async function socialAssetLink(element, id, name) {
  element.replaceChildren();
  try {
    const r = await api("/api/community/assets", {}), n = r.ok && r.counts?.[id];
    if (!n || !element.isConnected) return;
    const b = document.createElement("button"); b.className = "linkbtn"; b.dataset.i18n = "off";
    b.textContent = I18N.t(`社区里有 ${n} 个作品用到它`);
    b.addEventListener("click", () => socialByAsset(id, name)); element.replaceChildren(b);
  } catch (e) {}
}
async function soOwned(current) {
  const assets = current?.data?.assets || [], booth_ids = [...new Set(assets.map(a => a.boothId).filter(Boolean))];
  const r = await api("/api/community/owned", { booth_ids });
  if (!r.ok) throw new Error(r.err || "操作失败");
  for (const a of assets) if (a.boothId) { a.owned = !!r.owned[a.boothId]; a.wished = !!r.wished[a.boothId]; }
}
async function soWishMissing() {
  const current = SO.work;
  if (!current?.data?.ok || current.wishing) return;
  current.wishing = true; soWorkDraw();
  let added = 0;
  try {
    await soOwned(current);
    const seen = new Set();
    for (const a of current.data.assets || []) {
      if (!a.boothId || a.owned || a.wished || seen.has(a.boothId)) continue;
      seen.add(a.boothId);
      const r = await api("/api/wish/add", { id: a.boothId, title: a.name, shop: a.shop || "" });
      if (!r.ok) throw new Error(r.err || "操作失败");
      a.wished = true; added++;
    }
    await soOwned(current);
  } catch (e) { toast(e.message || "程序无响应", 5000); }
  finally {
    current.wishing = false;
    if (typeof storesFetch === "function") storesFetch();
    if (SO.work === current && $("#modal").dataset.kind === "sowork") soWorkDraw();
    toast(`已将 ${added} 件素材加入愿望单`);
  }
}
async function soFollowAlong() {
  const current = SO.work;
  if (!current?.data?.work?.recipe) return;
  try {
    await soOwned(current);
    if (SO.work !== current) return;
    const missing = current.data.assets.filter(a => a.boothId && !a.owned);
    if (missing.length && !confirm("以下素材尚未收录，仍要导入方案吗？\n" + missing.map(a => "・" + a.name).join("\n"))) return;
    return soRecipeFromCode(current.data.work.recipe);
  } catch (e) { toast(e.message || "程序无响应", 5000); }
}
// a recipe's code that came with a work
async function soRecipeFromCode(code) {
  let r; try { r = await api("/api/recipe/import", { text: code, save: false }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!r.ok) { toast(r.err || "无法读取该方案", 5000); return; }
  RCP.imp = { text: code, recipe: r.recipe, needs: r.needs };
  recipeShow("import");
}
function soCredits(w) {
  return [w.title || "", I18N.t("使用素材"), ...(w.assets || []).map(a => "・" + (a.name || "") + (a.shop ? "（" + a.shop + "）" : "") + (a.url ? " " + a.url : "")), "#VRChat #MioVRCA"].join("\n");
}
function soCanvasText(ctx, text, x, y, width, lines, height) {
  const chars = Array.from(String(text || ""));
  for (let row = 0; row < lines && chars.length; row++) {
    let line = "";
    while (chars.length && ctx.measureText(line + chars[0] + (row === lines - 1 ? "…" : "")).width <= width) line += chars.shift();
    if (row === lines - 1 && chars.length) line += "…";
    ctx.fillText(line, x, y + row * height);
  }
}
async function soShareCanvas(r) {
  const w = r.work, canvas = document.createElement("canvas"); canvas.width = 1080; canvas.height = 1350;
  const c = canvas.getContext("2d");
  c.fillStyle = "#f5f2f7"; c.fillRect(0, 0, 1080, 1350);
  c.fillStyle = "#e6e0ed"; c.fillRect(48, 48, 984, 550);
  if (r.pic) {
    const img = new Image();
    await new Promise((resolve, reject) => { img.onload = resolve; img.onerror = () => reject(new Error("无法载入封面，请重试")); img.src = r.pic; });
    const scale = Math.min(984 / img.naturalWidth, 550 / img.naturalHeight), width = img.naturalWidth * scale, height = img.naturalHeight * scale;
    c.drawImage(img, 48 + (984 - width) / 2, 48 + (550 - height) / 2, width, height);
  }
  c.fillStyle = "#292234"; c.font = 'bold 48px "Segoe UI", "Yu Gothic", "Microsoft YaHei", sans-serif';
  soCanvasText(c, w.title, 48, 660, 984, 2, 60);
  c.font = '32px "Segoe UI", "Yu Gothic", "Microsoft YaHei", sans-serif';
  soCanvasText(c, r.author?.nickname || w.author, 48, 785, 984, 1, 40);
  soCanvasText(c, w.base ? I18N.t(`素体：${w.base}`) : "", 48, 835, 984, 1, 40);
  c.fillStyle = "#675571"; c.fillText(I18N.t("使用素材"), 48, 900);
  const assets = w.assets || [];
  assets.slice(0, 6).forEach((a, i) => soCanvasText(c, "・" + a.name, 48, 949 + i * 40, 984, 1, 40));
  if (assets.length > 6) c.fillText(I18N.t(`等 ${assets.length} 件`), 48, 1200);
  c.fillStyle = "#766d7d"; c.font = '28px "Segoe UI", "Yu Gothic", "Microsoft YaHei", sans-serif';
  soCanvasText(c, I18N.t(`MioVRCA 社区 · ${w.id}`), 48, 1295, 984, 1, 36);
  return canvas;
}
async function soShareImage() {
  const current = SO.work;
  if (!current?.data?.ok || current.sharing) return;
  current.sharing = true; soWorkDraw();
  if (SO_WEB) {
    try {
      const canvas = await soShareCanvas(current.data);
      const blob = await new Promise(resolve => canvas.toBlob(resolve, "image/png"));
      if (!blob) throw new Error("无法保存");
      const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = `MioVRCA_${current.id}.png`;
      document.body.append(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(a.href), 4000);
      toast("分享图已保存");
    } catch (e) { toast(e.message || "无法保存", 5000); }
    finally { current.sharing = false; if (SO.work === current && $("#modal").dataset.kind === "sowork") soWorkDraw(); }
    return;
  }
  let save;
  try {
    const folder = await api("/api/studio/save/folder", { kind: "image" });
    if (folder.cancelled) return;
    if (!folder.ok) throw new Error(folder.err || "无法保存");
    const canvas = await soShareCanvas(current.data);
    const blob = await new Promise(resolve => canvas.toBlob(resolve, "image/png"));
    if (!blob) throw new Error("无法保存");
    const bytes = new Uint8Array(await blob.arrayBuffer());
    save = await api("/api/studio/save/begin", { kind: "cover", name: `MioVRCA_${current.id}.png`, size: bytes.length, folder: folder.path });
    if (!save.ok) throw new Error(save.err || "无法保存");
    const chunk = Math.max(1, Math.min(save.chunk || 8 * 1024 * 1024, 8 * 1024 * 1024));
    for (let off = 0; off < bytes.length;) {
      const end = Math.min(off + chunk, bytes.length);
      const response = await fetch(`/api/studio/save/chunk?id=${encodeURIComponent(save.id)}&off=${off}`, { method: "POST",
        headers: { "X-Token": window.API_TOKEN, "Content-Type": "application/octet-stream" }, body: bytes.subarray(off, end) });
      const r = await response.json();
      if (!response.ok || !r.ok || r.got !== end) throw new Error(r.err || "保存中断");
      off = r.got;
    }
    const r = await api("/api/studio/save/end", { id: save.id });
    if (!r.ok) throw new Error(r.err || "保存失败");
    save = null; toast("分享图已保存");
  } catch (e) { toast(e.message || "程序无响应", 5000); }
  finally {
    if (save?.ok) await api("/api/studio/save/end", { id: save.id, cancel: true }).catch(() => {});
    current.sharing = false;
    if (SO.work === current && $("#modal").dataset.kind === "sowork") soWorkDraw();
  }
}

// ---------- a work ----------
async function soWorkOpen(id) {
  SO.work = { id, data: null };
  const current = SO.work;
  soWorkDraw(); showModal();
  let r; try { r = await api("/api/community/work", { id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (SO.work !== current) return;
  SO.work.data = r;
  if (r.ok) { try { await soOwned(current); } catch (e) { toast(e.message || "程序无响应", 5000); } }
  if (SO.work !== current) return;
  if ($("#modal").dataset.kind === "sowork") soWorkDraw();
  await soServiceLoad(false);

}
function soWorkDraw() {
  const m = $("#modal"), x = SO.work, r = x && x.data;
  m.dataset.kind = "sowork";
  if (!r || !r.ok) {
    m.innerHTML = `<div class="mhead">作品</div><div class="mbody">${r ? `<div class="aiempty err">${esc(r.err || "无法读取该作品")}</div>` : `<div class="rmbusy"><i class="spin"></i>正在读取…</div>`}</div><div class="mfoot"><button class="btn primary" data-m="close">关闭</button></div>`;
    return;
  }
  const w = r.work, a = r.author || {}, v = SO_VIDEO[r.videoKind];
  const row = s => {
    const marks = [], acts = [];
    if (SO_WEB) { /* (no library to compare with) */ }
    else if (s.owned) marks.push(`<span class="tag ok">已有</span>`);
    else if (s.boothId) {
      marks.push(`<span class="tag faint">未有</span>`);
      const on = s.wished || typeof wishHas === "function" && wishHas(s.boothId);
      acts.push(`<button class="btn small${on ? " on" : ""}" data-so="wish" data-id="${esc(s.boothId)}" data-name="${esc(s.name)}" data-shop="${esc(s.shop || "")}"${on ? " disabled" : ""}>${on ? "已在愿望单" : "加入愿望单"}</button>`);
    }
    if (s.boothId) acts.push(`<button class="btn small ghost" data-so="asset" data-id="${esc(s.boothId)}" data-name="${esc(s.name)}">相关作品</button>`);
    if (s.url) acts.push(`<button class="btn small ghost" data-so="link" data-kind="${esc(s.kind || "website")}" data-href="${esc(s.url)}" title="${esc(s.url)}">${s.kind === "booth_item" ? "打开 Booth" : "打开"}</button>`);
    return `<tr><td class="nm" title="${esc(s.name)}" data-i18n="off">${esc(s.name)}</td><td class="muted small" data-i18n="off">${esc(s.shop || "")}</td><td class="marks">${marks.join("")}</td><td class="r">${acts.join("")}</td></tr>`;
  };
  const matched = (r.assets || []).filter(s => s.boothId), own = matched.filter(s => s.owned).length;
  m.innerHTML = `<div class="mhead">作品</div><div class="mbody sowork">
    <div class="socoverbig">${r.pic ? soSensWrap(soWorkMedia(w), `<img src="${esc(r.pic)}" alt="">`) : `<div class="ph" data-i18n="off">${esc(w.title)}</div>`}</div>
    <h2 data-i18n="off">${esc(w.title)}</h2>
    <div class="soline">${soProfileBtn(a.id, a.github || a.login, a.nickname, a.pic, false, a.booth)}${w.own_shop ? `<span class="tag sxownshop">作者本人</span>` : ""}${w.base ? `<span class="base" data-i18n="off">${esc(w.base)}</span>` : ""}<span class="muted small">${soWhen(w.created)}</span><span class="solikes">${SO_ICON.like}<span class="num">${esc(soLikes(w.id, r.likes))}</span></span>${w.clones > 0 ? `<span class="muted small sxclones">${esc(`${w.clones} 人做了同款`)}</span>` : ""}</div>
    <div class="btnrow">${w.video ? `<button class="btn small primary" data-so="url" data-href="${esc(w.video)}">${ICON.play}<span>打开视频</span>${v ? `<span class="sovsite">${esc(v)}</span>` : ""}</button>` : ""}
      ${soLikeButton(w.id)}
      <button class="btn small" data-so="remix">我也做了</button>
      <button class="btn small" data-so="credits">复制使用素材</button><button class="btn small" data-so="shareimage"${x.sharing ? " disabled" : ""}>保存分享图</button>
      ${w.recipe ? SO_WEB ? `<button class="btn small primary" data-so="webinapp" data-work="${esc(w.id)}">一键克隆</button>` : `<button class="btn small primary" data-so="wclone">一键克隆</button><button class="btn small" data-so="wclonebase">换素体克隆</button><button class="btn small" data-so="wrecipe">照着装</button>` : ""}
      ${r.canEdit ? `<button class="btn small" data-so="wedit">编辑</button><button class="btn small ghost" data-so="wremove">删除</button>` : `<button class="btn small ghost" data-so="report" data-kind="work" data-id="${esc(w.id)}" data-name="${esc(w.title)}">${SO_ICON.flag}<span>举报</span></button>`}</div>
    ${r.original ? `<div class="soreference"><button class="linkbtn" data-so="work" data-id="${esc(r.original.id)}">${esc(`参考了「${r.original.title}」`)}</button>${soProfileBtn(r.original.authorId, r.original.author, r.original.nickname)}</div>` : r.originalMissing ? `<p class="muted soreference">参考的作品已删除</p>` : ""}
    ${w.desc ? `<div class="sobio" data-i18n="off">${esc(w.desc)}</div>` : ""}
    ${(r.assets || []).length ? `<h5 class="lmh">同款素材</h5>${SO_WEB ? "" : `<p class="soowned">${esc(`已拥有 ${own}/${matched.length}`)}</p><button class="btn small" data-so="wishmissing"${own === matched.length || x.wishing ? " disabled" : ""}>把缺的加入愿望单</button>`}<table class="atable"><tbody>${r.assets.map(row).join("")}</tbody></table>` : ""}
    ${(w.links || []).length ? `<h5 class="lmh">相关链接</h5>${socialLinksHTML(w.links)}` : ""}
    ${r.offline ? `<p>${soOffline(r.offline)}</p>` : ""}
    ${r.remixes?.length ? `<section id="so_remixes"><h5 class="lmh">做同款的作品</h5><div class="sogrid works">${r.remixes.map(soWorkCard).join("")}</div></section>` : ""}
    <div class="sxworkthread"><button class="btn" data-so="post" data-id="${esc(w.id.replace(/^w/, "g"))}"${!SO.service?.available ? " disabled" : ""}>查看回复</button>${!SO.service?.available ? `<p>社区服务暂时不可用</p>` : ""}</div>
  </div><div class="mfoot"><span class="muted small" data-i18n="off">${esc(w.id)}</span><span class="spacer"></span><button class="btn primary" data-m="close">关闭</button></div>`;
}
async function soLike() {
  const x = SO.work; if (!x?.data || SO.busy.like) return;
  await soServiceLoad(false);
  if (!SO.service?.account) { soAccountDialog(() => soLike()); return; }
  const on = !soLiked(x.id);
  SO.busy.like = true; soWorkDraw();
  const r = await soServiceAPI("like", { id: x.id, on });
  SO.busy.like = false;
  if (!r.ok) { soServiceError(r); return; }
  if (r.stats) SO.stats = r.stats;
  else {
    if (!SO.stats) SO.stats = { works: {}, myLikes: [] };
    SO.stats.myLikes = (SO.stats.myLikes || []).filter(id => id !== x.id);
    if (on) SO.stats.myLikes.push(x.id);
    if (SO.stats.works?.[x.id]) SO.stats.works[x.id].likes = Math.max(0, SO.stats.works[x.id].likes + (on ? 1 : -1));
    if (r.statsError) toast(r.statsError, 5000);
    SO.serviceAt = 0;
  }
  soDraw(); if (SO.work === x && $("#modal")?.dataset.kind === "sowork") soWorkDraw();
}
function soReport(kind, id, name) {
  if (!SO.service?.account) { soAccountDialog(() => soReport(kind, id, name)); return; }
  const m = $("#modal"), open = m.classList.contains("on") ? m.dataset.kind : "";
  SO.report = { kind, id, name, back: open === "sowork" || open === "sopost" ? open : "" };
  m.dataset.kind = "soreport";
  m.innerHTML = `<div class="mhead">举报</div><div class="mbody soform">
    ${kind === "post" ? `<label class="check"><input type="checkbox" id="so_report_sensitive"> 敏感内容</label>` : ""}
    <div class="row"><label for="so_report_reason">原因（选填）</label><textarea id="so_report_reason" maxlength="200"></textarea></div>
    <p class="warnc small" id="so_report_error" role="status"></p></div>
    <div class="mfoot"><button class="btn" data-so="reportcancel">取消</button><button class="btn primary" data-so="reportsend" data-id="${esc(id)}">提交</button></div>`;
  showModal();
}
async function soReportSend(btn) {
  const r0 = SO.report; if (!r0) return;
  const reason = $("#so_report_reason").value.trim();
  btn.disabled = true;
  const r = r0.kind === "message" ? await soServiceAPI("dm/report", {id: r0.id, reason}) : r0.kind === "comment"
    ? await soServiceAPI("comment/report", { id: r0.id, reason })
    : await soServiceAPI("report", { kind: r0.kind, id: r0.id, reason, sensitive: r0.kind === "post" && !!$("#so_report_sensitive")?.checked });
  btn.disabled = false;
  if (!r.ok) { soServiceError(r); return; }
  const back = r0.back;
  SO.report = null; closeModal(); toast("已提交");
  if (back === "sowork" && SO.work?.data) { soWorkDraw(); showModal(); }
  else if (back === "sopost" && SO.post?.data?.ok) { soPostDraw(); showModal(); }
}
async function soRemoveWork() {
  const w = SO.work && SO.work.data && SO.work.data.work; if (!w) return;
  if (!confirm(`删除作品「${w.title}」？`)) return;
  let r; try { r = await api("/api/community/work/delete", { id: w.id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!r.ok) { soServiceError(r); return; }
  closeModal(); toast("已删除"); SO.lists = null; SO.me = null;
  if (SO.page) soProfile(SO.page.src, true); else soNeed(true);
}

// ---------- the service account ----------
function soLikes(id, fallback) { return SO.service?.available && SO.stats?.works?.[id] ? SO.stats.works[id].likes : fallback || 0; }
function soLiked(id) { return !!(SO.service?.account && SO.stats?.myLikes?.includes(id)); }
function soLikeButton(id) {
  const available = SO.service?.available, on = available && soLiked(id);
  return `<button class="btn small${on ? " on" : ""}" data-so="like" aria-pressed="${!!on}"${SO.busy.like ? " disabled" : ""}>${SO_ICON.like}<span>${on ? "已点赞" : "点赞"}</span></button>`;
}
async function soServiceAPI(path, body) {
  let r;
  try { r = await api("/api/community/" + path, body || {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (r.accountInvalid && SO.service?.account) { SO.service.account = null; SO.stats = null; soSessionReset(); }
  if (r.unavailable && SO.service) { SO.service.available = false; SO.stats = null; SO.serviceAt = 0; }
  return r;
}
async function soServiceLoad(refresh) {
  if (SO.serviceLoad) return SO.serviceLoad;
  if (!refresh && SO.service && Date.now() - SO.serviceAt < 60000) return;
  SO.busy.service = true;
  SO.serviceLoad = (async () => {
    const previous = SO.service;
    const r = await soServiceAPI("service", { refresh: !!refresh });
    SO.service = r.ok ? r : { available: false, account: null, err: r.err };
    SO.serviceAt = Date.now();
    if (SO.service.account) try { localStorage.setItem("miovrca.signed", "1"); } catch (_) {} // (signed in before 注册 / 登录 came: 登录 opens first)
    if (previous && previous.account?.id !== SO.service.account?.id) {
      SO.feed = null; SO.me = null; SO.home = null; soSessionReset();
      if (SO.nav === "images" && S.view === "social") soLoadHome(false, true);
    }
    if (SO.service.available) {
      const stats = await soServiceAPI("stats", { refresh: !!refresh });
      SO.stats = stats.ok ? stats.stats : null;
    } else SO.stats = null;
    await soUnreadLoad();
    if (S.view === "social" && SO.nav === "home" && !SO.timeline) soTimelineLoad();
    soDot();
    if (document.querySelector("#grid")) soDraw();
    if (SO.work?.data && $("#modal")?.dataset.kind === "sowork") soWorkDraw();
    if (SO.post?.data?.ok && $("#modal")?.dataset.kind === "sopost") soPostDraw();
  })();
  try { await SO.serviceLoad; } finally { SO.serviceLoad = null; SO.busy.service = false; }
}
function soServiceError(r) {
  toast(r.err || "操作失败", 5000);
  if (r.accountInvalid) { SO.me = null; SO.home = null; soAccountDialog(); }
  else if (SO.work?.data && $("#modal")?.dataset.kind === "sowork") soWorkDraw();
  else if (SO.post?.data?.ok && $("#modal")?.dataset.kind === "sopost") soPostDraw();
}
function soAccountHTML() {
  const s = SO.service, a = s?.account, d = SO.device;
  if (!a) return `<section class="soaccount soform" id="so_account"><div class="btnrow"><button class="btn primary" data-so="acctopen" data-mode="signup"${!s?.available ? " disabled" : ""}>注册</button><button class="btn" data-so="acctopen" data-mode="login"${!s?.available ? " disabled" : ""}>登录</button></div>
    ${s?.err ? `<p class="warnc small" role="status">${esc(s.err)}</p>` : !s?.available ? `<p class="warnc small">社区服务暂时不可用</p>` : ""}</section>`;
  const off = !s.available ? " disabled" : "", c = a.contacts || {};
  const contacts = [["QQ", c.qq], ["LINE", c.line], ["Discord", c.discord]].filter(x => x[1])
    .map(([k, v]) => `<span class="sxcontact"><span class="sxcontactk">${k}</span><span data-i18n="off">${esc(v)}</span></span>`).join("");
  const github = a.github ? `<button class="btn small" data-so="githubunlink"${off}>取消关联</button>`
    : s.githubLink ? `<button class="btn small" data-so="githubstart"${off || (d?.status === "pending" ? " disabled" : "")}>关联</button>` : `<span class="muted small">暂未开放</span>`;
  return `<section class="sxset" id="so_account">
    ${soSetHead("person", "账号")}
    ${soSetRow("昵称", `<b data-i18n="off">${esc(a.nickname)}</b>`, `<button class="btn small" data-so="nav" data-nav="edit">编辑资料</button>`)}
    ${soSetRow("用户名", `<span data-i18n="off">@${esc(a.handle || "")}</span>`, `<button class="btn small" data-so="handle">更改</button>`)}
    ${soSetRow("邮箱", `<span class="soemail" data-i18n="off">${esc(a.email || "")}</span>`, `<button class="btn small" data-so="emailchange"${off}>更换邮箱</button>`)}
    ${soCanScan() ? "" : soSetRow("在手机上登录", "", `<button class="btn small" data-so="qrhandoff"${off}>显示二维码</button>`)}
    ${soSetHead("link", "关联账号")}
    ${SO_WEB ? "" : soSetRow("GitHub", a.github ? `<b data-i18n="off">@${esc(a.github)}</b>` : `<span class="muted">未关联</span>`, github)}
    ${soOAuthList().map(p => (a.logins || []).includes(p) ? soSetRow(SO_OAUTH[p][0], `<span>已关联</span>`, `<button class="btn small" data-so="oauthunlink" data-provider="${p}"${off}>取消关联</button>`)
      : soSetRow(SO_OAUTH[p][0], `<span class="muted">未关联</span>`, `<button class="btn small" data-so="oauthlink" data-provider="${p}"${off}>关联</button>`)).join("")}
    <div id="so_device">${d?.status === "pending" ? `<code class="sodevicecode" data-i18n="off">${esc(d.user_code)}</code>` : d?.message ? `<p class="small">${esc(d.message)}</p>` : ""}</div>
    ${soSetRow("QQ / LINE / Discord", contacts || `<span class="muted">未填写</span>`, `<button class="btn small" data-so="contacts"${off}>编辑</button>`)}
  </section>`;
}
// a row of the settings: what it is, what it is now (a note under it), what to do about it
function soSetRow(label, value, actions, note) {
  return `<div class="sxsetrow"><div class="sxsetk">${label}</div><div class="sxsetv">${value}${note && !SO_WEB ? `<small>${esc(note)}</small>` : ""}</div><div class="sxseta">${actions || ""}</div></div>`;
}
// a switch of the settings: the label (a note under it) on the left, the switch on the right
function soSwitchRow(id, label, on, note, disabled) {
  return `<label class="sxsetrow sxswitchrow" for="${id}"><div class="sxsetv">${esc(label)}${note && !SO_WEB ? `<small>${esc(note)}</small>` : ""}</div><div class="sxseta"><input type="checkbox" class="sxswitch" id="${id}"${on ? " checked" : ""}${disabled ? " disabled" : ""}></div></label>`;
}
function soAccountDraw() { const el = $("#so_account"); if (el) el.outerHTML = soAccountHTML(); }
// ---------- 注册 / 登录 ----------
// As X: a window to join (加入 MioVRCA) and one to sign in (登录 MioVRCA). A device that never signed in opens the
// first, one that did the second; each links to the other. Both go by an emailed code, or by Google or Discord where
// the service offers them; a new address goes on to 设置资料. Each step redraws the window from SO.acct.
const SO_PRIVACY = "https://miovrc.net/privacy.html";
const soMailCooldowns = new Map();
let soMailTimer = 0;
const SO_OAUTH = {
  google: ["Google", '<svg viewBox="0 0 48 48" aria-hidden="true"><path style="fill:#ea4335" d="M24 9.5c3.5 0 6.7 1.2 9.2 3.6l6.9-6.9C35.9 2.4 30.5 0 24 0 14.6 0 6.5 5.4 2.6 13.2l8 6.2C12.4 13.7 17.7 9.5 24 9.5z"/><path style="fill:#4285f4" d="M47 24.6c0-1.6-.2-3.1-.4-4.6H24v9h12.9c-.6 3-2.3 5.5-4.8 7.2l7.7 6c4.5-4.2 7.1-10.4 7.1-17.6z"/><path style="fill:#fbbc05" d="M10.5 28.6c-.5-1.5-.8-3-.8-4.6s.3-3.1.8-4.6l-8-6.2C.9 16.5 0 20.1 0 24s.9 7.5 2.6 10.8z"/><path style="fill:#34a853" d="M24 48c6.5 0 11.9-2.1 15.9-5.8l-7.7-6c-2.2 1.5-4.9 2.3-8.2 2.3-6.3 0-11.6-4.2-13.5-9.9l-8 6.2C6.5 42.6 14.6 48 24 48z"/></svg>'],
  discord: ["Discord", '<svg viewBox="0 0 24 24" aria-hidden="true"><path style="fill:#5865f2" d="M20.3 4.4A19.8 19.8 0 0 0 15.4 2.9l-.7 1.3a18.3 18.3 0 0 0-5.5 0l-.6-1.3a19.7 19.7 0 0 0-4.9 1.5C.5 9-.3 13.6.1 18.1a19.9 19.9 0 0 0 6 3l1.3-2a13 13 0 0 1-1.9-.9l.4-.3c3.9 1.8 8.2 1.8 12 0l.4.3c-.6.4-1.2.7-1.9.9l1.3 2a19.8 19.8 0 0 0 6-3c.5-5.2-.8-9.7-3.5-13.7zM8 15.3c-1.2 0-2.2-1.1-2.2-2.4s1-2.4 2.2-2.4 2.2 1.1 2.2 2.4-1 2.4-2.2 2.4zm8 0c-1.2 0-2.2-1.1-2.2-2.4s1-2.4 2.2-2.4 2.2 1.1 2.2 2.4-1 2.4-2.2 2.4z"/></svg>'],
};
// has this device been signed in before (then 登录 opens first)
function soKnown() { try { return localStorage.getItem("miovrca.signed") === "1"; } catch (_) { return false; } }
function soOAuthList() { return (SO.service?.oauth || []).filter(p => SO_OAUTH[p]); }
function soOAuthButtons(signup) {
  return soOAuthList().map(p => `<button class="btn soaccbig soaccprov" data-so="oauthstart" data-provider="${p}">${SO_OAUTH[p][1]}<span>${signup ? `使用 ${SO_OAUTH[p][0]} 注册` : `使用 ${SO_OAUTH[p][0]} 登录`}</span></button>`).join("");
}
function soMailCountdown() {
  clearTimeout(soMailTimer);
  const btn = $("#so_send_code"), A = SO.acct;
  if (!btn || !A) return;
  const seconds = Math.max(0, Math.ceil(((soMailCooldowns.get(A.email) || 0) - Date.now()) / 1000));
  btn.disabled = !!seconds || btn.dataset.sending === "1";
  btn.textContent = seconds ? `重新发送（${seconds} 秒）` : "重新发送验证码";
  if (seconds) soMailTimer = setTimeout(soMailCountdown, 250);
}
// mode: "signup" or "login" (else by soKnown); step: where it opens ("start", "email" for 创建账号)
function soAccountDialog(next, mode, step) {
  SO.accountNext = typeof next === "function" ? next : null;
  const open = $("#modal")?.classList.contains("on") ? $("#modal").dataset.kind : "";
  const back = open === "sowork" && SO.work || open === "sopost" && SO.post || open === "sopostedit" && SO.compose;
  if (open !== "soaccount") SO.accountReturn = back ? open : "";
  SO.oauth = null;
  SO.acct = { mode: mode === "signup" || mode === "login" ? mode : soKnown() ? "login" : "signup", step: step || "start", email: "", code: "", nickname: "", handle: "", oauth: null };
  soAcctDraw();
}
function soAcctGo(step, mode) {
  const A = SO.acct; if (!A) return;
  if (mode) A.mode = mode;
  if (step === "start" || step === "email") { A.oauth = null; SO.oauth = null; }
  A.step = step; soAcctDraw();
}
// 返回: one step back (a code goes back to its address, 设置资料 to its code or to the start)
function soAcctBack() {
  const A = SO.acct; if (!A) return;
  if (A.step === "profile" && !A.oauth) return soAcctGo("code");
  if (A.step === "code" && A.mode === "signup") return soAcctGo("email");
  soAcctGo("start");
}
function soAcctDraw() {
  const A = SO.acct, m = $("#modal"); if (!A) return;
  clearTimeout(soMailTimer);
  const signup = A.mode === "signup", start = A.step === "start" || A.step === "wait";
  const qr = A.step === "start" && !signup && !(soCanScan() || innerWidth <= 760);
  if (!qr) soQRStop();
  const providers = soOAuthButtons(signup), or = providers ? `<div class="soaccor"><span>或</span></div>` : "";
  const no = signup ? { email: 1, code: 2, profile: 3 }[A.step] : 0;
  const title = A.step === "email" ? "创建账号" : A.step === "code" ? "输入邮箱验证码" : A.step === "profile" ? "设置资料" : signup ? "加入 MioVRCA" : "登录 MioVRCA";
  const email = `<div class="row"><label for="so_email">邮箱</label><input id="so_email" type="email" maxlength="254" autocomplete="email" value="${esc(A.email)}"></div>`;
  let body;
  if (A.step === "wait") body = `<div class="soaccwait"><span class="spin"></span><p>请在浏览器中完成登录</p></div><button class="btn soaccbig" data-so="acctback">取消</button>`;
  else if (A.step === "start" && signup) body = `${providers}${or}<button class="btn primary soaccbig" data-so="acctcreate">创建账号</button>
    <p class="soacclegal">注册即表示你同意<button class="linkbtn" data-so="privacy">隐私政策</button></p>
    <p class="soaccswitch">已有账号？<button class="linkbtn" data-so="acctlogin">登录</button></p>`;
  else if (A.step === "start") body = `${providers}${or}${email}<button class="btn primary soaccbig" data-so="accountcode">下一步</button>
    ${soCanScan() ? `<button class="btn soaccbig soqrscanbtn" data-so="qrscan">扫码登录</button>` : ""}
    <p class="soaccswitch">还没有账号？<button class="linkbtn" data-so="acctsignup">注册</button></p>`;
  else if (A.step === "email") body = `${email}<button class="btn primary soaccbig" data-so="accountcode">下一步</button>`;
  else if (A.step === "code") body = `<p class="soacclead">${esc(`验证码已发送至 ${A.email}`)}</p><p class="small muted">几分钟内没收到，请查看垃圾邮件。</p>
    <div class="row"><label for="so_code">验证码</label><input id="so_code" inputmode="numeric" maxlength="6" autocomplete="one-time-code" value="${esc(A.code)}"></div>
    <button class="btn primary soaccbig" data-so="accountsave">${signup ? "下一步" : "登录"}</button><button class="linkbtn soaccresend" id="so_send_code" data-so="accountcode">重新发送验证码</button>`;
  else body = `<p class="soacclead">${A.oauth ? esc(`使用 ${SO_OAUTH[A.oauth.provider]?.[0] || ""} 账号（${A.email}）注册`) : signup ? "昵称可以随时修改，用户名用于 @ 和个人主页地址。" : "该邮箱还没有账号，设置昵称和用户名即可完成注册。"}</p>
    <div class="row" id="so_nickname_row"><label for="so_nickname">昵称</label><input id="so_nickname" maxlength="24" autocomplete="off" value="${esc(A.nickname)}"></div>
    <div class="row" id="so_handle_row"><label for="so_handle">用户名</label><div class="sxat"><span aria-hidden="true">@</span><input id="so_handle" maxlength="15" autocomplete="off" spellcheck="false" placeholder="4–15 位字母、数字或下划线" value="${esc(A.handle)}"></div>
      <p class="small muted" id="so_handle_status" role="status"></p></div>
    <button class="btn primary soaccbig" data-so="accountsave">注册</button>`;
  m.dataset.kind = "soaccount";
  m.classList.toggle("withqr", qr);
  m.innerHTML = `<div class="mhead soacchead">${start ? `<button class="soaccnav wmback" data-so="accountcancel" aria-label="关闭">${SO_UI.x}</button>` : `<button class="soaccnav wmback" data-so="acctback" aria-label="返回">${SO_UI.back}</button>`}
      <span class="soaccmark" aria-hidden="true">${SO_WEB ? `<img src="icon.svg" alt="">` : ""}</span>${no ? `<small class="soaccstep">${esc(`第 ${no} 步，共 3 步`)}</small>` : ""}</div>
    <div class="mbody soaccountbody"><div class="soform soacc"><h2>${title}</h2>${body}<p class="warnc small" id="so_account_error" role="status"></p></div>${qr ? `<div class="soqrside" id="so_qr_login"></div>` : ""}</div>`;
  showModal(); m.setAttribute("aria-label", title);
  const first = $("#so_code") || $("#so_nickname") || $("#so_email") || $(".soaccbig", m);
  if (first) first.focus();
  if (A.step === "profile" && $("#so_handle").value) soSignHandleCheck();
  if (qr) soQRStart("login", $("#so_qr_login"));
  soMailCountdown();
}
function soAccountClose() {
  clearTimeout(soMailTimer); clearTimeout(SO.oauthTimer); SO.accountNext = null; SO.acct = null; SO.oauth = null; soQRStop(); $("#modal")?.classList.remove("withqr");
  const back = SO.accountReturn; SO.accountReturn = "";
  if (back === "sowork" && SO.work?.data) { soWorkDraw(); showModal(); }
  else if (back === "sopost" && SO.post?.data?.ok) { soPostDraw(); showModal(); }
  else if (back === "sopostedit" && SO.compose) { soComposeDraw(); showModal(); }
  else closeModal();
}
// 下一步 after an address (the code is mailed, then the code step) and 重新发送验证码
async function soAccountCode(btn) {
  const A = SO.acct, input = $("#so_email"), error = $("#so_account_error");
  if (!A) return;
  if (input) {
    const email = input.value.trim().toLowerCase();
    if (!email || !input.checkValidity()) { input.reportValidity(); input.focus(); return; }
    A.email = email;
  }
  // (a code went to this address a moment ago: that one is typed)
  if ((soMailCooldowns.get(A.email) || 0) > Date.now() && A.step !== "code") return soAcctGo("code");
  btn.disabled = true; btn.dataset.sending = "1"; error.textContent = "";
  const r = await soServiceAPI("auth/code", { email: A.email });
  btn.dataset.sending = ""; btn.disabled = false;
  if (SO.acct !== A || !btn.isConnected) return;
  if (!r.ok) { error.textContent = r.err; soMailCountdown(); return; }
  soMailCooldowns.set(A.email, Date.now() + 60000);
  if (A.step !== "code") return soAcctGo("code");
  toast("验证码已重新发送"); soMailCountdown(); $("#so_code")?.focus();
}
// 注册时的用户名: checked as it is typed (the service decides when the account is made)
let soSignHandleTimer = 0;
async function soSignHandleCheck() {
  const input = $("#so_handle"), status = $("#so_handle_status"); if (!input || !status) return;
  const v = input.value.trim().replace(/^@/, "");
  if (!v) { status.className = "small muted"; status.textContent = ""; input.dataset.ok = ""; return; }
  if (!/^[A-Za-z0-9_]{4,15}$/.test(v) || /^[0-9]+$/.test(v)) { status.className = "small warnc"; status.textContent = "4–15 位字母、数字或下划线，不能全为数字"; input.dataset.ok = "0"; return; }
  const r = await soServiceAPI("handle/check", {handle: v});
  if (!input.isConnected || input.value.trim().replace(/^@/, "") !== v) return;
  input.dataset.ok = r.ok && r.available ? "1" : "0";
  status.className = "small " + (r.ok && r.available ? "okline" : "warnc");
  status.textContent = !r.ok ? r.err : r.available ? "可以使用" : "已被使用，请换一个";
}
// the code step's 下一步 / 登录 (an address with an account signs in; a new one goes on to 设置资料) and 注册
async function soAccountSave(btn) {
  const A = SO.acct, error = $("#so_account_error"); if (!A) return;
  let nickname = "", handle = "";
  if (A.step === "code") {
    A.code = $("#so_code").value.trim();
    if (!A.code) { $("#so_code").focus(); return; }
  } else if (A.step === "profile") {
    A.nickname = nickname = $("#so_nickname").value.trim(); A.handle = handle = $("#so_handle").value.trim().replace(/^@/, "");
    if (!nickname) { $("#so_nickname").focus(); return; }
    if (!handle || $("#so_handle").dataset.ok === "0") { $("#so_handle").focus(); if (!handle) $("#so_handle_status").textContent = "请选择用户名"; return; }
  } else return;
  btn.disabled = true; error.textContent = "";
  const r = A.oauth ? await soServiceAPI("oauth/finish", { id: A.oauth.id, nickname, handle })
    : await soServiceAPI("auth/verify", { email: A.email, code: A.code, nickname, handle });
  if (SO.acct !== A || !btn.isConnected) return;
  btn.disabled = false;
  if (!r.ok) {
    if (r.code === "nickname_required") return soAcctGo("profile");
    if (r.code === "bad_code" && A.step === "profile") { soAcctGo("code"); $("#so_account_error").textContent = r.err; return; }
    if (r.code === "oauth_expired") { soAcctGo("start"); $("#so_account_error").textContent = r.err; return; }
    error.textContent = r.err;
    if (r.code === "handle_taken" || r.code === "bad_handle") { $("#so_handle").dataset.ok = "0"; $("#so_handle").focus(); }
    return;
  }
  if (handle) try { localStorage.setItem("so-handle:" + r.account.id, "1"); } catch (_) {} // (chosen here: no second question)
  if (A.mode === "signup" && A.step === "code") toast("该邮箱已有账号，已为你登录", 5000);
  await soSignedIn(r);
}
// Google / Discord: the page goes to the provider and comes back at #/oauth/<id> (web-start.js); the program opens
// the browser and waits here, asking every 2 seconds. 关联 in the settings goes the same way for a signed-in account.
async function soOAuthStart(btn, link) {
  const provider = btn.dataset.provider, A = SO.acct;
  btn.disabled = true;
  const r = await soServiceAPI("oauth/start", { provider, link: !!link });
  btn.disabled = false;
  if (!r.ok) { if (A && $("#so_account_error")) $("#so_account_error").textContent = r.err; else soServiceError(r); return; }
  if (SO_WEB && r.leaving) { try { sessionStorage.setItem("miovrca.oauthui." + r.id, JSON.stringify({ mode: A?.mode || "", link: !!link })); } catch (_) {} return; } // (on its way to the provider; the window it left is opened again on return)
  SO.oauth = { id: r.id, provider, link: !!link };
  if (A) { A.oauth = SO.oauth; A.step = "wait"; soAcctDraw(); }
  else toast("请在浏览器中完成关联", 5000);
  SO.oauthTimer = setTimeout(() => soOAuthPoll(r.id), 2000);
}
async function soOAuthPoll(id) {
  clearTimeout(SO.oauthTimer);
  const o = SO.oauth; if (!o || o.id !== id) return;
  const r = await soServiceAPI("oauth/poll", { id });
  if (SO.oauth !== o) return;
  if (r.ok && r.state === "pending") { SO.oauthTimer = setTimeout(() => soOAuthPoll(id), 2000); return; }
  SO.oauth = null;
  const name = SO_OAUTH[o.provider]?.[0] || "";
  if (!r.ok) {
    if (!SO.acct && o.mode) soAccountDialog(null, o.mode);
    if (SO.acct) { soAcctGo("start"); $("#so_account_error").textContent = r.err; } else toast(r.err, 5000);
    return;
  }
  if (r.state === "linked") {
    if (SO.service?.account) SO.service.account.logins = r.logins || [];
    soAccountDraw(); return toast(`已关联 ${name}`);
  }
  if (r.state === "new") {
    if (!SO.acct) soAccountDialog(null, "signup");
    Object.assign(SO.acct, { mode: "signup", step: "profile", email: r.email || "", nickname: r.nickname || "", handle: r.handle || "", oauth: o });
    return soAcctDraw();
  }
  if (r.state === "signed_in") { if (!SO.acct) SO.acct = { mode: "login" }; return soSignedIn(r); }
}
// back from the provider in the browser (#/oauth/<id>, web-start.js)
function soOAuthReturn(id) {
  let ui = {};
  try { ui = JSON.parse(sessionStorage.getItem("miovrca.oauthui." + id) || "{}"); sessionStorage.removeItem("miovrca.oauthui." + id); } catch (_) {}
  SO.oauth = { id, provider: (SO_WEB && webOAuthProvider(id)) || "", mode: ui.mode || "", link: !!ui.link };
  soOAuthPoll(id);
}
async function soOAuthUnlink(btn) {
  const provider = btn.dataset.provider;
  btn.disabled = true;
  const r = await soServiceAPI("account/logins/unlink", { provider });
  btn.disabled = false;
  if (!r.ok) return soServiceError(r);
  if (SO.service?.account) SO.service.account.logins = (SO.service.account.logins || []).filter(p => p !== provider);
  soAccountDraw(); toast(`已取消关联 ${SO_OAUTH[provider]?.[0] || ""}`);
}
// signed in (by email or by a QR code): the community starts over as this account
async function soSignedIn(r) {
  if (!SO.service) SO.service = { available: true, account: null };
  try { localStorage.setItem("miovrca.signed", "1"); } catch (_) {} // (this device opens 登录 from now on)
  SO.service.account = r.account; SO.serviceAt = 0; SO.feed = null; SO.me = null; SO.home = null; SO.lists = null; SO.posts = null; SO.timeline = null;
  soSessionReset();
  if (r.restored) toast("已取消注销", 5000);
  const next = SO.accountNext; soAccountClose(); soAccountDraw();
  soServiceLoad(true);
  soLoad("me", true); soLoad("lists", true);


  await soHandleOpen(next, true);
}
async function soAccountLogout(btn) {
  btn.disabled = true;
  const r = await soServiceAPI("account/logout");
  if (!r.ok) { btn.disabled = false; soServiceError(r); return; }
  if (SO_WEB) soPushSub().then(sub => sub?.unsubscribe()).catch(() => {}); // (the service ended it with the session)
  SO.service.account = null; SO.service.unread = 0; SO.device = null; SO.stats = null; SO.feed = null; SO.me = null; SO.home = null; SO.lists = null; SO.posts = null; SO.post = null;
  soSessionReset(); SO.drafts = {}; SO.compose = null;
  soDraw(); soDot();
  if (SO.nav === "home") soTimelineLoad();
}
// ---------- 管理 (the community's moderators) ----------
// An account the service names a moderator (its config, not a group's moderators) finds the moderation of a post and
// of an account in their menus. Hiding, deleting and banning ask first; everything is logged on the service and can
// be undone in the admin pages (but a deletion).
function soStaff() { return !!SO.service?.account?.moderator; }
function soStaffPostMenu(p) {
  if (!soStaff() || p.author?.bot) return [];
  return [["sep"], [p.featured ? "xstaffunfeature" : "xstafffeature", p.featured ? "取消精选" : "设为精选"], ["xstaffhide", "隐藏"], [p.sensitive ? "xstaffunsens" : "xstaffsens", p.sensitive ? "取消敏感标记" : "标记为敏感内容"], ["xstaffdelete", "删除"]];
}
function soStaffProfileMenu(r) {
  const a = r.profile || {};
  if (!soStaff() || r.readOnly || r.me || a.bot || !/^u[0-9a-z]{12}$/.test(a.id || "") || a.id === SO.service.account.id) return [];
  return [["sep"], ["pagestaffban", "封禁"]];
}
// a question over the page, as X asks before deleting: the answer, and the reason when one is asked for
function soAsk({ title, text, ok, danger, reason }) {
  return new Promise(done => {
    document.querySelector(".soask")?.remove();
    const el = document.createElement("div");
    el.className = "soask";
    el.innerHTML = `<div class="soaskbox" role="alertdialog" aria-modal="true" aria-labelledby="so_ask_t"><b id="so_ask_t">${esc(title)}</b>${text ? `<p>${esc(text)}</p>` : ""}
      ${reason ? `<input class="soaskreason" maxlength="100" placeholder="${esc(reason)}">` : ""}
      <div class="soaskbtns"><button class="btn ${danger ? "sxdangerfill" : "primary"}" data-ask="ok">${esc(ok)}</button><button class="btn" data-ask="no">取消</button></div></div>`;
    document.body.append(el);
    const end = v => { el.remove(); done(v); };
    el.addEventListener("click", e => {
      const a = e.target.closest("[data-ask]")?.dataset.ask;
      if (a === "ok") end({ ok: true, reason: el.querySelector(".soaskreason")?.value.trim() || "" });
      else if (a === "no" || e.target === el) end({ ok: false });
    });
    el.addEventListener("keydown", e => { if (e.key === "Escape") { e.stopPropagation(); end({ ok: false }); } });
    requestAnimationFrame(() => el.classList.add("on"));
    (el.querySelector(".soaskreason") || el.querySelector('[data-ask="no"]')).focus();
  });
}
async function soStaffPost(p, action) {
  const ask = { hide: ["隐藏这条帖子？", "其他人将看不到它，可以在后台恢复", "隐藏"], delete: ["删除这条帖子？", "删除后无法恢复", "删除"] }[action];
  if (ask && !(await soAsk({ title: ask[0], text: ask[1], ok: ask[2], danger: true })).ok) return;
  const r = await soServiceAPI("mod/post", { id: p.id, action });
  if (!r.ok) return soServiceError(r);
  if (ask) { soPostGone(p.id); return toast(action === "hide" ? "已隐藏" : "已删除"); }
  if (action === "feature" || action === "unfeature") { soPostFlag(p.id, "featured", action === "feature"); return toast(action === "feature" ? "已设为精选" : "已取消精选"); }
  soPostFlag(p.id, "sensitive", action === "sensitive");
  toast(action === "sensitive" ? "已标记为敏感内容" : "已取消敏感标记");
}
async function soStaffBan(a) {
  const ask = await soAsk({ title: `封禁 @${a.handle}？`, text: "封禁后无法发帖、回复和私信，可以在后台解封", ok: "封禁", danger: true, reason: "原因（可不填）" });
  if (!ask.ok) return;
  const r = await soServiceAPI("mod/account", { id: a.id, action: "ban", reason: ask.reason });
  if (!r.ok) return soServiceError(r);
  toast("已封禁");
  if (SO.page) soProfile(SO.page.src, true);
}
// a post hidden or deleted by a moderator leaves every list in sight
function soPostGone(id) {
  const keep = list => Array.isArray(list) ? list.filter(x => x?.id !== id && x?.post?.id !== id) : list;
  if (SO.timeline) SO.timeline.items = keep(SO.timeline.items);
  for (const holder of [SO.posts, SO.asks, SO.page?.posts, SO.group?.posts]) if (holder?.items) holder.items = keep(holder.items);
  if (SO.post?.data?.replies) SO.post.data.replies = keep(SO.post.data.replies);
  if (SO.post?.id === id) { closeModal(); SO.post = null; } else if (SO.post?.data?.ok && $("#modal")?.dataset.kind === "sopost") soPostDraw();
  soDraw();
}
function soPostFlag(id, key, value) {
  const set = x => { if (!x) return; if (x.id === id) x[key] = value; if (x.quote) set(x.quote); };
  for (const list of soPostLists()) for (const x of list || []) set(x);
  if (SO.post?.data?.ok && $("#modal")?.dataset.kind === "sopost") soPostDraw();
  soDraw();
}

// ---------- 翻译帖子 ----------
// As X: under a post written in another language than the page's, 「翻译帖子」; the translation shows under the text,
// naming the language and the service, until 隐藏翻译. The service translates for free (its own services, one after
// another, the result kept for everyone); 「AI 翻译」 in the settings uses the player's AI instead (the program's AI
// service, or in a browser the player's own API key), and falls back to the free one when it fails.
SO.trans = new Map();
const SO_LANG_FROM = { zh: "翻译自中文", en: "翻译自英语", ja: "翻译自日语", ko: "翻译自韩语", other: "翻译自其他语言" };
const SO_TRANS_BY = { deepl: "DeepL", microsoft: "Microsoft", mymemory: "MyMemory", ai: "AI" };
const SO_AI_PROVIDERS = [["openai", "OpenAI", "gpt-4o-mini"], ["deepseek", "DeepSeek", "deepseek-chat"], ["anthropic", "Claude", "claude-haiku-5-5"],
  ["gemini", "Gemini", "gemini-2.0-flash"], ["openrouter", "OpenRouter", "openai/gpt-4o-mini"], ["siliconflow", "硅基流动", "Qwen/Qwen2.5-7B-Instruct"],
  ["dashscope", "阿里云百炼", "qwen-plus"], ["moonshot", "Moonshot", "moonshot-v1-8k"]];
// the language a text is written in, by its letters (what the service decides by too): "" when nothing is left once
// links, @handles and #tags are taken out
function soTextLang(text) {
  const t = String(text || "").replace(/https?:\/\/\S+/g, "").replace(/[@#][\p{L}\p{N}_]+/gu, "");
  const kana = (t.match(/[\u3040-\u30ff]/g) || []).length, hangul = (t.match(/[\uac00-\ud7af]/g) || []).length;
  const han = (t.match(/\p{Script=Han}/gu) || []).length, latin = (t.match(/[A-Za-z]/g) || []).length;
  if (kana) return "ja";
  if (hangul) return "ko";
  if (han) return "zh";
  if (latin >= 2) return "en";
  return (t.match(/\p{L}/gu) || []).length >= 2 ? "other" : "";
}
function soUILang() { return I18N.lang === "en" || I18N.lang === "ja" ? I18N.lang : "zh"; }
function soTransSettings() {
  try { return Object.assign({ mode: "free", provider: "deepseek", model: "", key: "" }, JSON.parse(localStorage.getItem("so-translate") || "{}")); }
  catch (_) { return { mode: "free", provider: "deepseek", model: "", key: "" }; }
}
function soTransSave(v) { try { localStorage.setItem("so-translate", JSON.stringify(v)); } catch (_) {} }
function soTranslateHTML(p) {
  const lang = soTextLang(p.text);
  if (!lang || lang === soUILang()) return "";
  return `<div class="sxtrans" data-trans="${esc(p.id)}">${soTranslateInner(p.id)}</div>`;
}
function soTranslateInner(id) {
  const t = SO.trans.get(id);
  if (!t) return `<button class="linkbtn sxtranslink" data-so="xtranslate" data-id="${esc(id)}">翻译帖子</button>`;
  if (t.busy) return `<p class="sxtransby"><span class="spin"></span><span>正在翻译…</span></p>`;
  if (t.err) return `<p class="sxtransby warnc"><span>${esc(t.err)}</span><button class="linkbtn" data-so="xtranslate" data-id="${esc(id)}">重试</button></p>${t.hint ? `<p class="sxtransby"><span>${esc(t.hint)}</span></p>` : ""}`;
  return `<p class="sxtransby"><span>${esc(SO_LANG_FROM[t.source] || SO_LANG_FROM.other)}</span><span aria-hidden="true">·</span><span data-i18n="off">${esc(SO_TRANS_BY[t.provider] || t.provider || "")}</span><button class="linkbtn" data-so="xtranslatehide" data-id="${esc(id)}">隐藏翻译</button></p>
    ${t.fallback ? `<p class="sxtransby"><span>AI 翻译失败，已改用免费翻译</span></p>` : ""}<div class="sxtext sxtranstext" data-i18n="off">${soPostText(t.text || "")}</div>`;
}
function soTranslateDraw(id) { for (const el of document.querySelectorAll(`.sxtrans[data-trans="${CSS.escape(id)}"]`)) put(el, soTranslateInner(id)); }
async function soTranslate(id) {
  if (SO.trans.get(id)?.busy) return;
  const mode = soTransSettings().mode === "ai" ? "ai" : "free";
  SO.trans.set(id, { busy: true }); soTranslateDraw(id);
  const r = await soServiceAPI("post/translate", { id, target: soUILang(), mode });
  SO.trans.set(id, r.ok ? { text: r.text, source: r.source, provider: r.provider, fallback: !!r.fallback }
    : { err: r.err || "翻译失败，请稍后重试", hint: mode === "free" && r.code === "translate_failed" ? "可以在「设置」中改用 AI 翻译" : "" });
  soTranslateDraw(id);
}
// 设置 → 翻译: the service, and in a browser the player's own AI (its key stays in this browser)
function soTransSetHTML() {
  const v = soTransSettings(), ai = v.mode === "ai", prov = SO_AI_PROVIDERS.find(x => x[0] === v.provider) || SO_AI_PROVIDERS[1];
  const mode = `<select id="so_trans_mode" aria-label="翻译服务"><option value="free"${ai ? "" : " selected"}>免费翻译</option><option value="ai"${ai ? " selected" : ""}>AI 翻译</option></select>`;
  let more = "";
  if (ai && SO_WEB) more = `<div class="sxtransai soform">
      <div class="row"><label for="so_trans_provider">服务商</label><select id="so_trans_provider">${SO_AI_PROVIDERS.map(([k, n]) => `<option value="${k}"${k === prov[0] ? " selected" : ""} data-i18n="off">${esc(n)}</option>`).join("")}</select></div>
      <div class="row"><label for="so_trans_model">模型</label><input id="so_trans_model" maxlength="100" autocomplete="off" spellcheck="false" value="${esc(v.model || prov[2])}"></div>
      <div class="row"><label for="so_trans_key">API Key</label><input id="so_trans_key" type="password" maxlength="300" autocomplete="off" spellcheck="false" value="${esc(v.key || "")}"></div>
      <p class="small muted">API Key 只保存在此浏览器中，翻译时经由 MioVRCA 服务器转发，不会被保存。AI 翻译失败时改用免费翻译。</p></div>`;
  else if (ai) more = `<div class="sxtransai"><p class="small muted">使用「AI 服务」中的设置。AI 翻译失败时改用免费翻译。</p><button class="btn small" data-so="transai">AI 服务</button></div>`;
  return `<section class="sxset">${soSetHead("globe", "帖子翻译")}${soSetRow("翻译服务", "", mode)}${more}</section>`;
}
function soAICfgOpen() { if (typeof openAICfg === "function") openAICfg(); }
document.addEventListener("change", e => {
  const id = e.target.id;
  if (!["so_trans_mode", "so_trans_provider", "so_trans_model", "so_trans_key"].includes(id)) return;
  const v = soTransSettings();
  if (id === "so_trans_mode") v.mode = e.target.value === "ai" ? "ai" : "free";
  if (id === "so_trans_provider") { v.provider = e.target.value; v.model = (SO_AI_PROVIDERS.find(x => x[0] === v.provider) || [])[2] || ""; }
  if (id === "so_trans_model") v.model = e.target.value.trim();
  if (id === "so_trans_key") v.key = e.target.value.trim();
  soTransSave(v);
  if (id === "so_trans_mode" || id === "so_trans_provider") soDraw();
});
// ---------- 推送通知 (the page in a browser) ----------
// The service's notices on this phone or computer through the browser's push service; sw.js shows them and opens
// the page at what they are about. On an iPhone or iPad only once the page is on the home screen. Which kinds come
// is kept in this browser and sent with the subscription; signing out ends the subscription on the service.
const SO_PUSH_KINDS = [["reply", "回复"], ["mention", "提及"], ["quote", "引用"], ["repost", "转帖"], ["like", "喜欢"], ["follow", "新关注者"], ["dm", "私信"], ["featured", "精选"]];
function soPushSupport() {
  const ios = /iPhone|iPad|iPod/.test(navigator.userAgent) || navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1;
  const standalone = matchMedia("(display-mode: standalone)").matches || navigator.standalone === true;
  if (!("serviceWorker" in navigator) || !("PushManager" in window) || !("Notification" in window)) return ios && !standalone ? "home" : "none";
  return Notification.permission === "denied" ? "denied" : "ok";
}
function soPushKinds() {
  try { const v = JSON.parse(localStorage.getItem("miovrca.web.pushkinds") || "null"); if (Array.isArray(v)) return v; } catch (e) {}
  return SO_PUSH_KINDS.map(k => k[0]);
}
// (the worker may never be ready, e.g. where the browser keeps none: then no subscription after three seconds)
async function soPushSub() {
  const reg = await Promise.race([navigator.serviceWorker.ready, new Promise(ok => setTimeout(ok, 3000, null))]).catch(() => null);
  try { return reg ? await reg.pushManager.getSubscription() : null; } catch (e) { return null; }
}
function soPushHTML() {
  const support = soPushSupport(), on = !!SO.push?.on, kinds = soPushKinds();
  const note = { home: "添加到主屏幕后才能开启", none: "此浏览器不支持推送通知", denied: "通知权限已被关闭，请在浏览器设置中允许" }[support];
  return `${soSwitchRow("sx_push", "推送通知", on && support === "ok", "", support !== "ok" || !SO.push)}
    ${note ? `<p class="sxsetnote">${note}</p>` : ""}
    <div id="sx_push_kinds"${on && support === "ok" ? "" : " hidden"}>${SO_PUSH_KINDS.map(([k, t]) => soSwitchRow("sx_push_" + k, t, kinds.includes(k))).join("")}</div>
    <p class="sxsetstatus" id="sx_push_status" role="status"></p>`;
}
// what this browser has: a subscription and the permission (the switch waits for it)
async function soPushLoad() {
  const sub = soPushSupport() === "ok" && Notification.permission === "granted" ? await soPushSub() : null;
  SO.push = { on: !!sub };
  if (SO.nav === "more") soDraw();
  return sub;
}
async function soPushSwitch(box) {
  const status = $("#sx_push_status");
  box.disabled = true; status.textContent = "";
  const ok = box.checked ? await soPushOn() : await soPushOff();
  box.disabled = false;
  if (!ok.ok) { box.checked = !box.checked; status.textContent = ok.err; return; }
  SO.push = { on: box.checked };
  $("#sx_push_kinds").hidden = !box.checked;
}
// on: the permission first (asked while the click lasts), then the browser's subscription, then the service
async function soPushOn() {
  if (Notification.permission !== "granted" && await Notification.requestPermission() !== "granted") return { ok: false, err: "未允许通知" };
  const key = await soServiceAPI("push/key");
  if (!key.ok || !key.key) return { ok: false, err: key.err || "暂时无法开启推送通知" };
  let sub;
  try {
    const reg = await Promise.race([navigator.serviceWorker.ready, new Promise((ok, bad) => setTimeout(bad, 10000))]);
    const raw = atob(key.key.replace(/-/g, "+").replace(/_/g, "/"));
    sub = await reg.pushManager.getSubscription() || await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: Uint8Array.from(raw, c => c.charCodeAt(0)) });
  } catch (e) { return { ok: false, err: "暂时无法开启推送通知" }; }
  return soPushSend(sub);
}
function soPushSend(sub) {
  const kinds = soPushKinds();
  return soServiceAPI("push/on", { subscription: sub.toJSON(), lang: window.I18N?.lang || "zh", kinds: kinds.includes("reply") ? [...kinds, "answer"] : kinds });
}
async function soPushOff() {
  const sub = await soPushSub();
  if (sub) { await soServiceAPI("push/off", { endpoint: sub.endpoint }); try { await sub.unsubscribe(); } catch (e) {} }
  return { ok: true };
}
async function soPushKindsSave() {
  const kinds = SO_PUSH_KINDS.map(k => k[0]).filter(k => $("#sx_push_" + k)?.checked);
  try { localStorage.setItem("miovrca.web.pushkinds", JSON.stringify(kinds)); } catch (e) {}
  const sub = await soPushSub(); if (!sub) return;
  const r = await soPushSend(sub);
  const status = $("#sx_push_status"); if (status) status.textContent = r.ok ? "已保存" : r.err;
}
// on each start, signed in with a subscription: the service gets it again (a new endpoint, the language)
async function soPushSync() {
  if (!SO_WEB || !SO.service?.account || soPushSupport() !== "ok" || Notification.permission !== "granted") return;
  const sub = await soPushLoad(); if (sub) soPushSend(sub);
}
// 通知: the offer to turn them on, until it is taken or closed
function soPushOffer() {
  if (!SO_WEB || !SO.service?.account || SO.push?.on || soPushSupport() !== "ok" || Notification.permission !== "default") return "";
  try { if (localStorage.getItem("miovrca.web.pushoffer") === "0") return ""; } catch (e) {}
  return `<div class="sxpushoffer"><span class="soi" data-i="bell" aria-hidden="true">${SO_UI.bell}</span><span>开启推送通知，有人回复或私信时提醒你</span>
    <button class="btn small primary" data-so="pushon">开启</button><button class="wb" data-so="pushofferclose" aria-label="关闭">${SO_UI.x}</button></div>`;
}

// ---------- 扫码登录 ----------
// A QR code signs a device in to the account another of the player's devices is signed in to, and the signed-in one
// always approves (internal/socialsrv qr.go):
// - login: this device is not signed in and shows the code beside the email form; a signed-in phone scans it
//   (扫一扫, or its camera opening the page at #/qr/<id>) and approves there;
// - handoff: this device is signed in and shows the code (设置 →「在手机上登录」); a phone scans it, then this device
//   approves it.
// A code lasts two minutes; the page asks every two seconds what became of it.
const SOQR = { id: "", kind: "", timer: 0, box: null, url: "", state: "" };
function soCanScan() { return SO_WEB && !!navigator.mediaDevices?.getUserMedia && matchMedia("(pointer: coarse)").matches; }
function soQRSVG(text) {
  qrcode.stringToBytes = qrcode.stringToBytesFuncs["UTF-8"];
  const q = qrcode(0, "M"); q.addData(text); q.make();
  return q.createSvgTag({ cellSize: 4, margin: 3, scalable: true });
}
function soNeedScript(src, global) {
  if (window[global]) return Promise.resolve(window[global]);
  return new Promise((ok, bad) => {
    const el = document.createElement("script"); el.src = src; el.async = true;
    el.onload = () => window[global] ? ok(window[global]) : bad(new Error("无法载入 " + src));
    el.onerror = () => { el.remove(); bad(new Error("无法载入 " + src)); };
    document.head.append(el);
  });
}
function soQRStop() { clearTimeout(SOQR.timer); SOQR.id = ""; SOQR.box = null; }
async function soQRStart(kind, box) {
  soQRStop();
  SOQR.kind = kind; SOQR.box = box; SOQR.state = "";
  put(box, `<div class="soqr"><div class="soqrcode"><span class="spin"></span></div></div><p class="soqrcap">${kind === "login" ? "用手机扫码登录" : "用手机相机扫描二维码"}</p>`);
  const [r] = await Promise.all([soServiceAPI("qr/new", { kind }), soNeedScript("vendor/qrcode.js", "qrcode").catch(() => null)]);
  if (SOQR.box !== box || !box.isConnected) return;
  if (!r.ok || !window.qrcode) return soQRDraw(box, "error", r.err || "无法显示二维码");
  SOQR.id = r.id; SOQR.url = r.url;
  soQRDraw(box, "waiting");
  soQRPoll(r.id, box);
}
function soQRDraw(box, state, text) {
  SOQR.state = state;
  const over = { scanned: [SO_UI.check, "已扫描，请在手机上确认"], denied: [SO_UI.x, "已取消"], expired: [SO_UI.refresh, "二维码已过期"],
    approved: [SO_UI.check, "已登录"], error: [SO_UI.x, text || "无法显示二维码"] }[state];
  const again = state === "denied" || state === "expired" || state === "error";
  put(box, `<div class="soqr${over ? " dim" : ""}"><div class="soqrcode">${SOQR.url && window.qrcode ? soQRSVG(SOQR.url) : ""}</div>${over ? `<div class="soqrover"><span class="soi" data-i="check" aria-hidden="true">${over[0]}</span><b>${over[1]}</b>${again ? `<button class="btn small" data-so="qrrefresh">刷新</button>` : ""}</div>` : ""}</div>
    <p class="soqrcap">${SOQR.kind === "login" ? "用手机扫码登录" : "用手机相机扫描二维码"}</p>`);
}
async function soQRPoll(id, box) {
  clearTimeout(SOQR.timer);
  if (SOQR.id !== id || !box.isConnected) return;
  const r = await soServiceAPI("qr/poll", { id });
  if (SOQR.id !== id || !box.isConnected) return;
  if (!r.ok) {
    if (r.code === "qr_expired") { SOQR.id = ""; return soQRDraw(box, "expired"); }
    SOQR.timer = setTimeout(() => soQRPoll(id, box), 4000); return; // (the connection: try again)
  }
  if (r.account) { SOQR.id = ""; soQRDraw(box, "approved"); return soSignedIn(r); }
  if (r.state === "denied") { SOQR.id = ""; return soQRDraw(box, "denied"); }
  if (SOQR.kind === "handoff" && r.state === "scanned") return soQRAsk(id, box, r);
  if (r.state === "approved" && SOQR.kind === "handoff") { SOQR.id = ""; return soQRDone(box); }
  if (r.state !== SOQR.state) soQRDraw(box, r.state === "scanned" ? "scanned" : "waiting");
  SOQR.timer = setTimeout(() => soQRPoll(id, box), 2000);
}
// handoff: a phone scanned this device's code; it signs in once this device says so
function soQRAsk(id, box, r) {
  SOQR.state = "ask";
  put(box, `<div class="soqrask"><span class="soqrdevice"><span class="soi" data-i="phone" aria-hidden="true">${SO_UI.phone}</span></span>
    <b>${esc(r.device || "手机")}</b><p>请求登录你的账号</p>
    <div class="soqrbtns"><button class="btn" data-so="qrdeny" data-id="${esc(id)}">拒绝</button><button class="btn primary" data-so="qrapprove" data-id="${esc(id)}">登录</button></div></div>`);
}
function soQRDone(box) {
  put(box, `<div class="soqrask"><span class="soqrdevice ok"><span class="soi pop" data-i="check" aria-hidden="true">${SO_UI.check}</span></span><b>已在手机上登录</b></div>`);
}
async function soQRAnswer(btn, ok) {
  const id = btn.dataset.id, box = btn.closest("#so_qr_handoff, .soqrscanned");
  btn.disabled = true;
  const r = await soServiceAPI("qr/approve", { id, ok });
  if (!r.ok) { btn.disabled = false; return toast(r.err, 4000); }
  if (box?.id === "so_qr_handoff") { SOQR.id = ""; if (ok) soQRDone(box); else soQRDraw(box, "denied"); return; }
  closeModal(); toast(ok ? "已登录" : "已取消");
}
// 设置 →「在手机上登录」: this account's code for a phone
function soQRHandoff() {
  if (!SO.service?.account) return soAccountDialog(soQRHandoff);
  const m = $("#modal"); m.dataset.kind = "sxqr";
  m.innerHTML = `<div class="mhead">在手机上登录</div><div class="mbody"><div class="soqrside" id="so_qr_handoff"></div></div>
    <div class="mfoot"><button class="btn" data-m="close">关闭</button></div>`;
  showModal(); soQRStart("handoff", $("#so_qr_handoff"));
}
// a phone opened a code (扫一扫, or the camera opening #/qr/<id>): sign the computer in, or sign in here
async function soQROpen(id) {
  if (!/^[A-Za-z0-9_-]{16,64}$/.test(id || "")) return toast("这不是 MioVRCA 的登录二维码", 4000);
  const m = $("#modal"); m.dataset.kind = "sxqr";
  m.innerHTML = `<div class="mhead">扫码登录</div><div class="mbody soqrscanned"><span class="spin"></span></div>`;
  showModal();
  const r = await soServiceAPI("qr/scan", { id });
  const box = $("#modal .soqrscanned"); if (!box) return;
  if (!r.ok) {
    if (r.code === "unauthorized" || r.accountInvalid) return put(box, `<div class="soqrask"><span class="soqrdevice"><span class="soi" data-i="login" aria-hidden="true">${SO_UI.login}</span></span><b>请先登录，再扫码</b>
      <div class="soqrbtns"><button class="btn primary" data-so="qrsignin" data-id="${esc(id)}">登录</button></div></div>`);
    return put(box, `<div class="soqrask"><span class="soqrdevice"><span class="soi" data-i="x" aria-hidden="true">${SO_UI.x}</span></span><b>${esc(r.err)}</b></div>`);
  }
  if (r.kind === "login") {
    const a = SO.service?.account;
    return put(box, `<div class="soqrask"><span class="soqrdevice"><span class="soi" data-i="login" aria-hidden="true">${SO_UI.login}</span></span>
      <b>在 ${esc(r.device || "电脑")} 上登录？</b>${a ? `<p class="soqrwho">${soAv(SO.service.accountPic, a.nickname, "xs")}<span data-i18n="off">${esc(a.nickname)} @${esc(a.handle)}</span></p>` : ""}
      <div class="soqrbtns"><button class="btn" data-so="qrdeny" data-id="${esc(id)}">取消</button><button class="btn primary" data-so="qrapprove" data-id="${esc(id)}">登录</button></div></div>`);
  }
  const p = r.person || {};
  put(box, `<div class="soqrask"><span class="soqrdevice"><span class="spin"></span></span><b>请在电脑上确认</b>
    <p class="soqrwho">${soAv(r.personPic, p.nickname, "xs")}<span data-i18n="off">${esc(p.nickname || "")} @${esc(p.handle || "")}</span></p></div>`);
  SOQR.kind = "take"; SOQR.id = id; SOQR.box = box;
  const wait = async () => {
    if (SOQR.id !== id || !box.isConnected) return;
    const q = await soServiceAPI("qr/poll", { id });
    if (SOQR.id !== id || !box.isConnected) return;
    if (q.ok && q.account) { SOQR.id = ""; closeModal(); toast("已登录"); return soSignedIn(q); }
    if (q.ok && q.state === "denied" || q.code === "qr_expired") { SOQR.id = ""; return put(box, `<div class="soqrask"><span class="soqrdevice"><span class="soi" data-i="x" aria-hidden="true">${SO_UI.x}</span></span><b>${q.ok ? "已拒绝" : "二维码已过期"}</b></div>`); }
    SOQR.timer = setTimeout(wait, 2000);
  };
  wait();
}
// 扫一扫 (a phone's camera in the page): BarcodeDetector where the browser has it, else jsQR (vendor/jsQR.js)
const SOSCAN = { stream: null, timer: 0 };
async function soQRScan() {
  const m = $("#modal"); m.dataset.kind = "sxscan";
  m.innerHTML = `<div class="mhead">扫一扫</div><div class="mbody soscan"><video id="so_scan_video" playsinline muted></video><span class="soscanframe" aria-hidden="true"></span>
    <p class="soscantip" id="so_scan_tip">将二维码放入框内</p></div>`;
  showModal();
  const video = $("#so_scan_video"), tip = $("#so_scan_tip");
  try {
    SOSCAN.stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" }, audio: false });
  } catch (e) {
    tip.textContent = e && e.name === "NotAllowedError" ? "请允许使用相机" : "无法使用相机"; return;
  }
  if (!soScanOpen()) return soScanStop();
  video.srcObject = SOSCAN.stream; await video.play().catch(() => {});
  let detect;
  if ("BarcodeDetector" in window && (await BarcodeDetector.getSupportedFormats?.() || []).includes("qr_code")) {
    const d = new BarcodeDetector({ formats: ["qr_code"] });
    detect = async () => (await d.detect(video))[0]?.rawValue || "";
  } else {
    try { await soNeedScript("vendor/jsQR.js", "jsQR"); } catch (e) { tip.textContent = "无法使用相机"; return; }
    const c = document.createElement("canvas"), g = c.getContext("2d", { willReadFrequently: true });
    detect = async () => {
      const w = video.videoWidth, h = video.videoHeight; if (!w || !h) return "";
      const k = Math.min(1, 640 / Math.max(w, h)); c.width = Math.round(w * k); c.height = Math.round(h * k);
      g.drawImage(video, 0, 0, c.width, c.height);
      return jsQR(g.getImageData(0, 0, c.width, c.height).data, c.width, c.height, { inversionAttempts: "dontInvert" })?.data || "";
    };
  }
  const tick = async () => {
    if (!soScanOpen() || !SOSCAN.stream) return soScanStop();
    let text = ""; try { text = await detect(); } catch (e) {}
    const id = /#\/qr\/([A-Za-z0-9_-]{16,64})$/.exec(text)?.[1];
    if (id) { soScanStop(); return soQROpen(id); }
    if (text) tip.textContent = "这不是 MioVRCA 的登录二维码";
    SOSCAN.timer = setTimeout(tick, 250);
  };
  tick();
}
// (closed by its way back, or by anything that put another dialog in its place: the camera goes off)
function soScanOpen() { return !!$("#modal.on[data-kind='sxscan'] #so_scan_video"); }
function soScanStop() {
  clearTimeout(SOSCAN.timer);
  if (SOSCAN.stream) for (const t of SOSCAN.stream.getTracks()) t.stop();
  SOSCAN.stream = null;
}
// 注销账号: confirmed by typing the handle. The service hides the account at once and deletes it after 7 days;
// signing in again before then cancels it.
function soAccountDelete() {
  const a = SO.service?.account;
  if (!a) return;
  const m = $("#modal");
  m.dataset.kind = "sodelete";
  m.innerHTML = `<div class="mhead">注销账号</div><div class="mbody soform">
    <p>注销后账号立即隐藏，7 天内重新登录可取消。7 天后帖子、回复、图片和发出的私信将被删除，用户名在 30 天后释放。</p>
    <label for="so_delete_handle">${esc(`输入 @${a.handle} 确认`)}</label><input id="so_delete_handle" autocomplete="off" spellcheck="false">
    <p class="warnc small" id="so_delete_error" role="status"></p></div>
    <div class="mfoot"><button class="btn" data-m="close">取消</button><button class="btn primary" data-so="accountdeletego">注销账号</button></div>`;
  showModal(); $("#so_delete_handle").focus();
}
async function soAccountDeleteGo(btn) {
  const a = SO.service?.account, input = $("#so_delete_handle"), error = $("#so_delete_error");
  if (!a || !input) return;
  const typed = input.value.trim().replace(/^@/, "");
  if (!typed || typed.toLowerCase() !== String(a.handle || "").toLowerCase()) { error.textContent = "用户名不一致"; input.focus(); return; }
  btn.disabled = true;
  const r = await soServiceAPI("account/delete", { handle: typed });
  if (!r.ok) { btn.disabled = false; if (r.accountInvalid) { closeModal(); soServiceError(r); } else error.textContent = r.err || "操作失败"; return; }
  closeModal();
  if (SO_WEB) soPushSub().then(sub => sub?.unsubscribe()).catch(() => {});
  SO.service.account = null; SO.service.unread = 0; SO.device = null; SO.stats = null; SO.feed = null; SO.me = null; SO.home = null; SO.posts = null; SO.post = null;
  soSessionReset(); SO.drafts = {}; SO.compose = null;
  toast("账号已注销，7 天内重新登录可取消", 5000);
  await soServiceLoad(true); soLoad("me", true);
}
// 导出我的数据: a zip made by the service, saved into the folder the player picks
async function soAccountExport(btn) {
  const dir = SO_WEB ? "" : await pickInto("选择导出位置");
  if (!dir && !SO_WEB) return;
  const status = $("#sx_export_status");
  btn.disabled = true;
  if (status) status.textContent = "正在导出…";
  const r = await soServiceAPI("account/export", { folder: dir });
  btn.disabled = false;
  if (!r.ok) { if (status) status.textContent = r.err || "操作失败"; if (r.accountInvalid) soServiceError(r); return; }
  if (status) put(status, r.path ? `<span>已导出：</span><span data-i18n="off">${esc(r.path)}</span>` : `<span>已导出</span>`);
  toast("已导出");
}
// Booth 店铺认证: the service gives a code, the shop's owner puts it into the shop's profile, the service reads it there
function soBoothHTML() {
  const r = SO.booth;
  if (!r) return `<p class="muted">正在读取…</p>`;
  if (!r.ok) return `<p class="warnc">${esc(r.err || "")}</p>`;
  const s = r.booth || {}, shop = s.pending || s.shop || "";
  return `${s.shop ? `<div class="btnrow"><span class="sxboothshop"><span data-i18n="off">${esc(s.name || s.shop)}</span>${soVerified(s.shop)}</span><button class="btn small" data-so="url" data-href="${esc(`https://${s.shop}.booth.pm/`)}"><span data-i18n="off">${esc(s.shop)}.booth.pm</span></button><button class="btn small ghost" data-so="boothunbind">解除绑定</button></div>` : ""}
    ${s.pending ? `<div class="btnrow" title="${esc(`将验证码填入「${s.pending}.booth.pm」的店铺简介，然后点击「检查」`)}"><code class="sxboothcode" data-i18n="off">${esc(s.code)}</code><button class="btn small" data-so="boothcopy">复制</button><button class="btn small primary" data-so="boothcheck">检查</button></div>` : ""}
    <label for="sx_booth_shop">店铺地址</label><div class="btnrow"><input id="sx_booth_shop" data-i18n="off" placeholder="https://xxx.booth.pm/" value="${esc(shop ? `https://${shop}.booth.pm/` : "")}"><button class="btn small" data-so="boothstart">获取验证码</button></div>
    <p class="warnc small" id="sx_booth_status" role="status">${esc(SO.boothStatus || "")}</p>`;
}
function soBoothDraw() { const el = $("#sx_booth"); if (el) put(el, soBoothHTML()); }
async function soBoothAct(kind, btn) {
  const shop = $("#sx_booth_shop")?.value.trim() || "";
  if (kind === "start" && !shop) { $("#sx_booth_shop")?.focus(); return; }
  btn.disabled = true;
  const r = await soServiceAPI("booth/" + kind, kind === "start" ? { shop } : {});
  btn.disabled = false;
  if (!r.ok) {
    if (r.accountInvalid) return soServiceError(r);
    SO.boothStatus = r.err || "操作失败"; // (kept: a redraw of the page shows it again)
    const status = $("#sx_booth_status"); if (status) status.textContent = SO.boothStatus;
    return;
  }
  SO.boothStatus = "";
  SO.booth = r;
  if (SO.service?.account) SO.service.account.booth = r.booth?.shop || "";
  soBoothDraw();
  if (kind === "check") toast("已认证");
  if (kind === "unbind") toast("已解除绑定");
}
async function soGitHubStart(btn) {
  btn.disabled = true;
  const r = await soServiceAPI("github/start");
  if (!r.ok) { btn.disabled = false; soServiceError(r); return; }
  SO.device = r.device;
  await openURL(r.device.verification_uri);
  soAccountDraw(); soGitHubPoll();
}
async function soGitHubPoll() {
  const d = SO.device; if (!d || d.status !== "pending") return;
  const r = await soServiceAPI("github/poll");
  if (SO.device !== d) return;
  if (!r.ok) { SO.device = { status: "error", message: r.err }; soAccountDraw(); soServiceError(r); return; }
  SO.device = r.device; if (SO.service) SO.service.account = r.account;
  soAccountDraw();
  if (r.device.status === "pending") setTimeout(soGitHubPoll, 1000);
  else {
    SO.feed = null; SO.me = null; SO.home = null; SO.posts = null; SO.serviceAt = 0;
    if (r.device.message) toast(r.device.message, 5000);


  }
}
async function soGitHubUnlink(btn) {
  btn.disabled = true;
  const r = await soServiceAPI("github/unlink");
  btn.disabled = false;
  if (!r.ok) { soServiceError(r); return; }
  SO.service.account = r.account; SO.device = null; SO.me = null; SO.home = null; soAccountDraw();
}

// ---------- pictures ----------
function soDataURL(blob) {
  return new Promise((ok, bad) => { const f = new FileReader(); f.onload = () => ok(f.result); f.onerror = () => bad(f.error); f.readAsDataURL(blob); });
}
async function soPicture(blob, size) {
  let img;
  try { img = await createImageBitmap(blob); } catch (e) { throw new Error("无法读取图片：请使用 JPG、PNG 或 WebP"); }
  const cv = document.createElement("canvas"); cv.width = size.w; cv.height = size.h;
  const g = cv.getContext("2d"), want = size.w / size.h;
  let sw = img.width, sh = img.height;
  if (sw / sh > want) sw = Math.round(sh * want); else sh = Math.round(sw / want);
  g.imageSmoothingQuality = "high";
  g.fillStyle = "#fff"; g.fillRect(0, 0, size.w, size.h);
  g.drawImage(img, Math.round((img.width - sw) / 2), Math.round((img.height - sh) / 2), sw, sh, 0, 0, size.w, size.h);
  if (img.close) img.close();
  for (const q of [0.9, 0.82, 0.74, 0.66, 0.58, 0.5]) {
    const b = await new Promise(ok => cv.toBlob(ok, "image/jpeg", q));
    if (b && b.size <= size.max) { const url = await soDataURL(b); return { b64: url.slice(url.indexOf(",") + 1), url }; }
  }
  throw new Error("图片压缩后仍然过大，请换一张");
}
async function soPostPicture(blob) {
  let img;
  try { img = await createImageBitmap(blob); } catch (e) { throw new Error("无法读取图片：请使用 JPG、PNG 或 WebP"); }
  const scale = Math.min(1, SO_POST_MAX / Math.max(img.width, img.height));
  const w = Math.max(1, Math.round(img.width * scale)), h = Math.max(1, Math.round(img.height * scale));
  const cv = document.createElement("canvas"); cv.width = w; cv.height = h;
  const g = cv.getContext("2d"); g.imageSmoothingQuality = "high";
  g.fillStyle = "#fff"; g.fillRect(0, 0, w, h); g.drawImage(img, 0, 0, w, h);
  if (img.close) img.close();
  for (const q of [0.9, 0.82, 0.74, 0.66, 0.58]) {
    const b = await new Promise(ok => cv.toBlob(ok, "image/jpeg", q));
    if (b && b.size <= 4 << 20) { const url = await soDataURL(b); return { b64: url.slice(url.indexOf(",") + 1), url }; }
  }
  throw new Error("图片压缩后仍然过大，请换一张");
}
// 裁剪头像: the picture under a round frame — drag to move it, the wheel or the slider to zoom — resolved with the
// square under the frame (512 px, a PNG blob), or null when cancelled
function soCropOpen(file) {
  return new Promise(async resolve => {
    let img;
    try { img = await createImageBitmap(file); } catch (e) { toast("无法读取图片：请使用 JPG、PNG 或 WebP", 5000); resolve(null); return; }
    const m = $("#modal"), V = 360;
    m.dataset.kind = "sxcrop";
    m.innerHTML = `<div class="mhead">裁剪头像</div><div class="mbody sxcrop"><div class="sxcropstage"><canvas width="${V * 2}" height="${V * 2}"></canvas><div class="sxcropring"></div></div>
      <div class="sxcropzoom"><span aria-hidden="true">－</span><input type="range" min="1" max="4" step="0.01" value="1" aria-label="缩放"><span aria-hidden="true">＋</span></div></div>
      <div class="mfoot"><button class="btn" data-crop="no">取消</button><button class="btn primary" data-crop="ok">确定</button></div>`;
    showModal();
    const cv = $("canvas", m), g = cv.getContext("2d"), range = $(".sxcropzoom input", m);
    const base = Math.max(V / img.width, V / img.height); // (zoom 1: the picture just covers the frame)
    let zoom = 1, x = 0, y = 0, drag = null;
    const clamp = () => { const w = img.width * base * zoom, h = img.height * base * zoom; x = Math.min(0, Math.max(V - w, x)); y = Math.min(0, Math.max(V - h, y)); };
    x = (V - img.width * base) / 2; y = (V - img.height * base) / 2;
    const draw = () => { clamp(); g.setTransform(2, 0, 0, 2, 0, 0); g.clearRect(0, 0, V, V); g.imageSmoothingQuality = "high"; g.drawImage(img, x, y, img.width * base * zoom, img.height * base * zoom); };
    const zoomTo = (z, cx = V / 2, cy = V / 2) => { z = Math.min(4, Math.max(1, z)); const k = z / zoom; x = cx - (cx - x) * k; y = cy - (cy - y) * k; zoom = z; range.value = String(z); draw(); };
    cv.addEventListener("pointerdown", e => { drag = { px: e.clientX, py: e.clientY, x, y }; cv.setPointerCapture(e.pointerId); });
    cv.addEventListener("pointermove", e => { if (!drag) return; const r = cv.getBoundingClientRect(), s2 = V / r.width; x = drag.x + (e.clientX - drag.px) * s2; y = drag.y + (e.clientY - drag.py) * s2; draw(); });
    cv.addEventListener("pointerup", () => { drag = null; });
    cv.addEventListener("wheel", e => { e.preventDefault(); const r = cv.getBoundingClientRect(), s2 = V / r.width; zoomTo(zoom * (e.deltaY < 0 ? 1.08 : 1 / 1.08), (e.clientX - r.left) * s2, (e.clientY - r.top) * s2); }, { passive: false });
    range.addEventListener("input", () => zoomTo(+range.value));
    const done = async ok => {
      m.removeEventListener("click", click);
      let blob = null;
      if (ok) {
        const out = document.createElement("canvas"); out.width = out.height = 512;
        const o = out.getContext("2d"), k = 512 / V; o.imageSmoothingQuality = "high";
        o.drawImage(img, x * k, y * k, img.width * base * zoom * k, img.height * base * zoom * k);
        blob = await new Promise(r => out.toBlob(r, "image/png"));
      }
      if (img.close) img.close();
      closeModal();
      resolve(blob);
    };
    const click = e => { const b = e.target.closest("[data-crop],[data-m=close]"); if (b) { e.stopPropagation(); done(b.dataset.crop === "ok"); } };
    m.addEventListener("click", click);
    draw();
  });
}
function soPickImage(multiple) {
  return new Promise(ok => {
    const i = document.createElement("input"); i.type = "file"; i.accept = "image/png,image/jpeg,image/webp"; i.multiple = !!multiple;
    i.onchange = () => ok(i.files ? [...i.files] : []); i.click();
  });
}

// ---------- link rows (shared with web/recipe.js) ----------
function soLinkRows(id, rows, max) {
  return `<div class="solinkrows" id="${id}">${rows.map((l, i) => `<div class="solinkrow" data-i="${i}">
      <input class="solu" value="${esc(l.url)}" placeholder="https://…" aria-label="链接地址" spellcheck="false" autocomplete="off">
      <input class="soll" value="${esc(l.label || "")}" placeholder="名称（选填）" maxlength="24" aria-label="链接名称">
      <span class="tag${l.kind ? " ok" : l.url ? " warn" : ""}">${esc(l.kind ? SO_KIND[l.kind] || l.kind : l.url ? "无法识别" : "")}</span>
      <button class="wb" data-so="linkdel" data-for="${id}" data-i="${i}" title="删除" aria-label="删除">${ICON.x}</button></div>`).join("")}
    ${rows.length < max ? `<button class="btn small ghost" data-so="linkadd" data-for="${id}">${ICON.plus}<span>添加链接</span></button>` : ""}</div>`;
}
function soLinkRead(id) {
  return [...document.querySelectorAll(`#${id} .solinkrow`)].map(r => ({ url: soNormURL($(".solu", r).value), label: $(".soll", r).value.trim() }));
}
// an address as a player types it — 「example.com」, 「www.x.com/abc」, 「http://…」 — as the https address it is
function soNormURL(v) {
  v = String(v || "").trim();
  if (!v) return "";
  if (/^http:\/\//i.test(v)) v = "https://" + v.slice(7);
  else if (!/^[a-z][a-z0-9+.-]*:/i.test(v) && /^[^\s/]+\.[^\s/]{2,}(\/\S*)?$/.test(v)) v = "https://" + v.replace(/^\/+/, "");
  return v;
}
let soKindTimer = 0;
function soKinds(id, forWhat, rows, then) {
  clearTimeout(soKindTimer);
  soKindTimer = setTimeout(async () => {
    let r; try { r = await api("/api/community/linkkinds", { urls: rows.map(l => l.url), for: forWhat }); } catch (e) { return; }
    if (!r.ok) return;
    rows.forEach((l, i) => { l.kind = r.kinds[i] || ""; });
    const tags = document.querySelectorAll(`#${id} .solinkrow .tag`);
    rows.forEach((l, i) => { const t = tags[i]; if (!t) return; t.className = "tag" + (l.kind ? " ok" : l.url ? " warn" : ""); t.textContent = l.kind ? SO_KIND[l.kind] || l.kind : l.url ? "无法识别" : ""; if (window.I18N) I18N.apply && I18N.apply(t); });
    if (then) then(r);
  }, 300);
}

// ---------- 发布作品 / 编辑作品 ----------
async function socialNewWork(init) {
  init = init || {};
  SO.wd = { id: "", title: init.title || "", desc: init.desc || "", video: init.video || "", base: init.base || "", based_on: init.based_on || "",
    assets: (init.assets || []).map(a => ({ ...a })), links: [], recipe: "", rights: false, vkind: "",
    pictures: [], existing: [], replacing: false, busy: false };
  if (!SO.lists) await soLoad("lists");
  if (init.coverBlob) {
    try { const check = nsfwCheck(init.coverBlob), p = await soPostPicture(init.coverBlob); p.flagged = !!(await check)?.flagged; SO.wd.pictures.push(p); } catch (e) { toast(e.message, 5000); }
  }
  soWorkEdit();
}
window.socialNewWork = socialNewWork;
async function soWorkEditOpen() {
  const r = SO.work && SO.work.data; if (!r || !r.work) return;
  const w = r.work;
  if (!SO.lists) await soLoad("lists");
  const existing = (r.picList || []).map((url, i) => ({ id: (w.pictures || [])[i] || "", url }));
  SO.wd = { id: w.id, title: w.title, desc: w.desc || "", video: w.video || "", base: w.base || "", based_on: w.based_on || "",
    assets: (w.assets || []).map(a => Object.assign({}, a)), links: (w.links || []).map(l => Object.assign({}, l)),
    recipe: "", recipeCode: w.recipe || "", rights: !!w.rights_ok, vkind: r.videoKind || "",
    pictures: [], existing, replacing: false, busy: false };
  soWorkEdit();
}
function soWdRead() {
  const d = SO.wd; if (!d || !$("#sw_title")) return d;
  d.title = $("#sw_title").value; d.desc = $("#sw_desc").value; d.video = $("#sw_video").value.trim(); d.base = $("#sw_base").value.trim();
  d.recipe = $("#sw_recipe") ? $("#sw_recipe").value : d.recipe; d.rights = !!($("#sw_rights") && $("#sw_rights").checked);
  const old = d.links; d.links = soLinkRead("sw_links").map((l, i) => Object.assign(l, { kind: old[i] && old[i].url === l.url ? old[i].kind : "" }));
  d.assets = [...document.querySelectorAll("#sw_assets .soasset")].map((row, i) => Object.assign(d.assets[i] || {}, { name: $(".soan", row).value.trim(), shop: $(".soas", row).value.trim(), url: $(".soau", row).value.trim() }));
  return d;
}
function soWorkPictures() {
  const d = SO.wd;
  const pics = d.replacing ? d.pictures : (d.pictures.length ? d.pictures : d.existing.map(p => ({ id: p.id, url: p.url, old: true })));
  return `<div class="sopicpick sxworkpicgrid">${pics.map((p, i) => `<span class="sopic${i === 0 ? " cover" : ""}"><img src="${esc(p.url)}" alt="">
      ${i === 0 ? `<span class="tag ok">封面</span>` : ""}<button class="wb" data-so="picdel" data-i="${i}" title="移除" aria-label="移除">${ICON.x}</button></span>`).join("")}${pics.length < 9 ? `<button class="sxpicadd" data-so="picpick" aria-label="选择图片">${SO_TOOL.picture}<span>${pics.length ? "添加图片" : "选择图片…"}</span></button>` : ""}</div>
    ${pics.some(p => p.flagged) ? `<p class="small sxsensnote" role="status">图片可能含敏感内容，将由管理员复核</p>` : ""}`;
}
function soWorkEdit() {
  const d = SO.wd, m = $("#modal");
  if (typeof RCP !== "undefined" && RCP.list === null && typeof recipeLoad === "function") recipeLoad().then(() => { if (m.dataset.kind === "sowedit") { soWdRead(); soWorkEdit(); } });
  const recipes = (typeof RCP !== "undefined" && RCP.list) || [];
  const net = d.links.some(l => l.kind === "netdisk" || /pan\.baidu\.com|yun\.baidu\.com/i.test(l.url));
  m.dataset.kind = "sowedit";
  m.innerHTML = `<div class="mhead sxmhead"><span>${d.id ? "更新作品" : "发布作品"}</span><button class="wb sxmclose" data-m="close" title="关闭" aria-label="关闭">${ICON.x}</button></div><div class="mbody soform sxworkform">
    <div class="sxworkpics"><div class="sxfieldk"><span>图片</span></div><div id="sw_pics">${soWorkPictures()}</div></div>
    <div class="sxworkfields">
    <div class="row"><label for="sw_title">标题</label><input id="sw_title" maxlength="60" value="${esc(d.title)}" autocomplete="off" placeholder="给作品起个名字"></div>
    <div class="row"><label for="sw_video">视频链接（选填）</label><div class="addrow"><input id="sw_video" value="${esc(d.video)}" spellcheck="false" autocomplete="off"><span class="tag${d.vkind ? " ok" : d.video ? " warn" : ""}" id="sw_vkind">${esc(d.vkind ? SO_VIDEO[d.vkind] : d.video ? "无法识别" : "")}</span></div></div>
    <div class="row"><label for="sw_desc">说明（选填）</label><textarea id="sw_desc" maxlength="1000">${esc(d.desc)}</textarea></div>
    <div class="row"><label for="sw_base">素体（选填）</label><input id="sw_base" maxlength="24" value="${esc(d.base)}" autocomplete="off"></div>
    <div class="row"><label for="sw_based_q">参考的作品（选填）</label><div id="sw_based_selected">${soBasedSelected()}</div>
      <input id="sw_based_q" value="${esc(d.basedQuery || "")}" placeholder="按标题或作品编号搜索" autocomplete="off"><div class="solibrows" id="sw_based_results">${soBasedChoices()}</div></div>
    <div class="row"><label>同款素材（选填）</label><div id="sw_assets">${d.assets.map((a, i) => `<div class="soasset" data-i="${i}"><input class="soan" value="${esc(a.name)}" placeholder="名称" maxlength="80" aria-label="素材名称"><input class="soas" value="${esc(a.shop || "")}" placeholder="店铺（选填）" maxlength="40" aria-label="店铺"><input class="soau" value="${esc(a.url || "")}" placeholder="商品页（选填）" spellcheck="false" aria-label="商品页"><button class="wb" data-so="assetdel" data-i="${i}" title="删除" aria-label="删除">${ICON.x}</button></div>`).join("")}</div>
      <div class="btnrow">${d.assets.length < 30 ? `<button class="btn small" data-so="assetlib">从素材库添加…</button><button class="btn small" data-so="assetproject">从工程添加…</button><button class="btn small ghost" data-so="assetadd">${ICON.plus}<span>手动添加</span></button>` : ""}</div>
      <div id="sw_libpick"></div></div>
    <div class="row"><label>链接（选填）</label>${soLinkRows("sw_links", d.links, 8)}</div>
    <label class="check${net ? "" : " sohide"}" id="sw_rightsrow"><input type="checkbox" id="sw_rights"${d.rights ? " checked" : ""}> 网盘链接中的内容是我自己的，或我有权分享</label>
    <div class="row"${SO_WEB ? " hidden" : ""}><label for="sw_recipe">附带装配方案（选填）</label><select id="sw_recipe"><option value="">${d.recipeCode ? "保留原来附带的方案" : "不附带"}</option>${recipes.map(r => `<option value="${esc(r.id)}"${d.recipe === r.id ? " selected" : ""} data-i18n="off">${esc(r.name)}</option>`).join("")}${d.recipeCode ? `<option value="-"${d.recipe === "-" ? " selected" : ""}>不再附带</option>` : ""}</select>
      ${recipes.length ? `<div class="btnrow"><button class="btn small ghost" data-so="assetrecipe">把方案中的素材加入同款素材</button></div>` : ""}</div>
  </div></div><div class="mfoot"><button class="btn ghost" data-m="close">取消</button><button class="btn primary" data-so="wsend"${d.busy ? " disabled" : ""}>${d.id ? "保存" : "发布"}</button></div>`;
  showModal();
}
function soLibPick(q) {
  const box = $("#sw_libpick"); if (!box) return;
  const words = soWords(q), all = ((S.data && S.data.assets) || []).filter(a => a.boothId || isGum(a));
  const hits = all.filter(a => soHit(words, a.name, shopOf(a), a.boothId)).slice(0, 30);
  put(box, `<div class="solib"><input id="sw_libq" value="${esc(q || "")}" placeholder="搜索素材库" autocomplete="off" aria-label="搜索素材库">
    <div class="solibrows">${hits.map(a => `<button class="navitem" data-so="assetpick" data-key="${esc(a.key)}"><span class="nm" data-i18n="off">${esc(a.name)}</span><span class="n" data-i18n="off">${esc(shopOf(a))}</span></button>`).join("") || `<div class="muted small">没有符合条件的素材</div>`}</div></div>`);
}
function soAssetOf(a) {
  const url = isGum(a) ? gumPageURL(a) : boothLinkURL(a.boothId);
  return { name: (a.booth && a.booth.name) || a.name, shop: shopOf(a), url };
}
function soBasedSelected() {
  const d = SO.wd, w = SO.lists?.works?.find(w => w.id === d.based_on);
  return d.based_on ? `<span data-i18n="off">${esc(w ? w.title + " · " + w.id : d.based_on)}</span> <button class="btn small ghost" data-so="basedclear">清除</button>` : "";
}
function soBasedChoices() {
  const d = SO.wd;
  return (SO.lists?.works || []).filter(w => w.id !== d.id && soHit(soWords(d.basedQuery), w.title, w.id)).slice(0, 30)
    .map(w => `<button class="navitem" data-so="basedpick" data-id="${esc(w.id)}"><span data-i18n="off">${esc(w.title)} · ${esc(w.id)}</span></button>`).join("") || `<p class="muted small">没有符合条件的作品</p>`;
}
async function soProjectPick(path) {
  const d = soWdRead(), box = $("#sw_libpick"); if (!d || !box) return;
  if (!S.projs) await loadProjects();
  if (SO.wd !== d || !box.isConnected) return;
  const ps = projsToPick("");
  path = path || (AI.proj && AI.proj.path) || (ps[0] && ps[0].path);
  const p = ps.find(p => p.path === path);
  if (!p) { put(box, `<p class="muted small">没有可用的工程</p>`); return; }
  const select = `<select id="sw_project" aria-label="选择工程">${ps.map(p => `<option value="${esc(p.path)}"${p.path === path ? " selected" : ""} data-i18n="off">${esc(p.name)}</option>`).join("")}</select>`;
  d.projectPath = path;
  put(box, `${select}<p class="muted small">正在读取…</p>`);
  let r; try { r = await api("/api/pipe/assets", { project: path }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (SO.wd !== d || d.projectPath !== path || !box.isConnected) return;
  if (!r.ok) { put(box, `${select}<p class="warnc small">${esc(r.err || "无法读取工程")}</p>`); return; }
  d.projectAssets = (r.assets || []).map(a => {
    const lib = a.key && findAsset(a.key);
    if (lib && (lib.boothId || isGum(lib))) return soAssetOf(lib);
    const id = /^booth:(\d{1,12})$/.exec(a.key || "");
    return { name: a.name, shop: "", url: id ? boothLinkURL(id[1]) : "" };
  });
  put(box, `${select}<div class="solibrows">${d.projectAssets.map((a, i) => `<button class="navitem" data-so="assetprojectpick" data-i="${i}"><span class="nm" data-i18n="off">${esc(a.name)}</span><span class="n" data-i18n="off">${esc(a.shop)}</span></button>`).join("") || `<p class="muted small">没有可添加的素材</p>`}</div>`);
}
async function soAssetsFromRecipe() {
  const id = $("#sw_recipe") && $("#sw_recipe").value;
  if (!id || id === "-") { toast("请先选择要附带的方案"); return; }
  let r; try { r = await api("/api/recipe/get", { id }); } catch (e) { r = { ok: false }; }
  if (!r.ok) { toast(r.err || "无法读取该方案", 4000); return; }
  const d = soWdRead(); let n = 0;
  for (const a of r.recipe.assets || []) {
    const url = a.url || (a.boothId && !String(a.boothId).startsWith("gr_") ? boothLinkURL(a.boothId) : "");
    if (d.assets.length >= 30 || d.assets.some(x => x.name === a.name || (url && x.url === url))) continue;
    d.assets.push({ name: a.name, shop: "", url }); n++;
  }
  toast(n ? `已加入 ${n} 件素材` : "方案中的素材都已在列表中");
  soWorkEdit();
}
async function soWorkSend(btn) {
  const d = soWdRead();
  if (!d.title.trim()) { toast("请填写标题"); $("#sw_title").focus(); return; }
  if (!d.id && !d.pictures.length) { toast("请选择作品图片"); return; }
  const req = { title: d.title.trim(), desc: d.desc.trim(), video: d.video, base: d.base, assets: d.assets.filter(a => a.name),
    links: d.links.filter(l => l.url).map(l => ({ url: l.url, label: l.label })), rights_ok: d.rights, based_on: d.based_on };
  if (d.recipe && d.recipe !== "-") { const c = await api("/api/recipe/code", { id: d.recipe }); if (!c.ok) { toast(c.err || "无法读取该方案", 4000); return; } req.recipe = c.code; }
  else if (!d.recipe && d.recipeCode) req.recipe = d.recipeCode;
  const pictures = d.replacing ? d.pictures.map(p => p.b64) : d.pictures.map(p => p.b64);
  btn.disabled = true; d.busy = true;
  let r;
  // a new picture the check found likely sensitive (web/nsfw.js): the work goes to the admin as one automatic report
  const flagged = pictures.length > 0 && d.pictures.some(p => p.flagged);
  try { r = await api(d.id ? "/api/community/work/update" : "/api/community/work/publish", { id: d.id, work: req, pictures, auto_flag: flagged }); }
  catch (e) { r = { ok: false, err: "程序无响应" }; }
  d.busy = false; btn.disabled = false;
  if (!r.ok) { soServiceError(r); return; }
  closeModal(); toast(d.id ? "已保存" : "已发布"); SO.lists = null; SO.me = null;
  if (SO.page) soProfile(SO.page.src, true); else soNeed(true);
}

// ---------- what is clicked ----------
async function soAction(b, e) {
  const what = b.dataset.so;
  if (what.startsWith("x")) return soXAction(b, e);
  if (what.startsWith("group")) return soGroupAction(b, e);
  if (what.startsWith("ask")) return soAskAction(b, e);
  if (what.startsWith("page") || what.startsWith("dm") || ["noticetab", "searchtab", "worksnext"].includes(what)) return soPageAction(b, e);
  switch (what) {
    case "nav": SO.asset = null; SO.nav = b.dataset.nav; SO.page = null; SO.q = ""; if ($("#q") && S.view === "social") $("#q").value = ""; closeModal(); if (S.view !== "social") setView("social"); soNeed(false); soDraw(); $(".main").scrollTop = 0; return;
    case "refresh": if (SO.asset) return socialByAsset(SO.asset.id, SO.asset.name); return soNeed(true);
    case "htab": return soHfTab(b.dataset.tab);
    case "homeretry": SO.hfeed = null; return soLoadHome(false, true);
    case "homelike": e.stopPropagation(); return soHfLike(b);
    case "base": SO.base = SO.base === b.dataset.base ? "" : b.dataset.base; SO.nav = "works"; SO.asset = null; return soWorksLoad(true);
    case "basesmore": SO.basesMore = !SO.basesMore; return soDraw();
    case "basefollow": return soFollowBase(b);
    case "sort": SO.sort = b.dataset.sort; return soWorksLoad(true);
    case "recipefilter": SO.recipeOnly = !SO.recipeOnly; return soDraw();
    case "videofilter": SO.videoOnly = !SO.videoOnly; return soDraw();
    case "asset": return socialByAsset(b.dataset.id, b.dataset.name);
    case "assetclear": SO.asset = null; return soWorksLoad(true);
    case "wishmissing": return soWishMissing();
    case "credits": return copyText(soCredits(SO.work.data.work), "使用素材已复制");
    case "shareimage": return soShareImage();
    case "work": return soWorkOpen(b.dataset.id);
    case "post": return soPostOpen(b.dataset.id);
    case "compose": return soComposeOpen();
    case "handle": return soHandleOpen();
    case "emailchange": return soEmailOpen();
    case "emailcode": return soEmailCode(b);
    case "emailsave": return soEmailSave(b);
    case "contacts": return soContactsOpen();
    case "emoji": return soEmojiOpen(b);
    case "noticesread": return soNoticesReadAll(b);
    case "copycontact": return copyText(b.dataset.text, "已复制");
    case "contactssave": return soContactsSave(b);
    case "posttag": return soPostTagOpen(b.dataset.tag);
    case "profile": case "creator": e.stopPropagation(); return soProfileFrom(b);
    case "back": SO.page = null; return soDraw();
    case "follow": e.stopPropagation(); return soFollow(b.dataset.target, b.dataset.on === "true", b);
    case "followshop": { const ok = await followAdd({ sub: b.dataset.sub, name: b.dataset.name }); if (ok) soDraw(); return; }
    case "nooffer": SO.offer = null; return soDraw();
    case "link": e.stopPropagation(); return soOpenLink(b.dataset.kind, b.dataset.href, b.dataset.label, b);
    case "netadd": case "netopen": return soPopAct(what);
    case "url": return soOpenLink("", b.dataset.href, "", b);
    case "booth": soInside(); return openShopItem(b.dataset.id);
    case "recipe": return soRecipeView(b.dataset.id);
    case "recipeshare": return soRecipeShare();
    case "recipesharego": return soRecipeShareGo(b);
    case "wrecipe": return soFollowAlong();
    case "wclone": case "wclonebase": { const w = soCloneWorkOf(SO.work?.data); if (!w) return; return what === "wclone" ? soClone(w) : soCloneBase(w); }
    case "clonepick": { const w = SO.cloneWork; SO.cloneWork = null; if (w) return soClone(w, b.dataset.key); return; }
    case "cloneapply": return soCloneApply();
    case "cloneundo": { const undo = SO.cloneUndo; SO.cloneUndo = null; closeModal(); if (undo) undo(); return; }
    case "like": return soLike();
    case "account": return soAccountDialog();
    case "accountsave": return soAccountSave(b);
    case "accountcode": return soAccountCode(b);
    case "accountlogout": return soAccountLogout(b);
    case "accountcancel": return soAccountClose();
    case "acctback": return soAcctBack();
    case "privacy": return openURL(SO_PRIVACY);
    case "transai": return soAICfgOpen();
    case "acctcreate": return soAcctGo("email");
    case "acctlogin": return soAcctGo("start", "login");
    case "acctsignup": return soAcctGo("start", "signup");
    case "acctopen": return soAccountDialog(null, b.dataset.mode, b.dataset.step);
    case "oauthstart": return soOAuthStart(b);
    case "oauthlink": return soOAuthStart(b, true);
    case "oauthunlink": return soOAuthUnlink(b);
    case "qrrefresh": { const box = b.closest(".soqrside"); if (box) soQRStart(SOQR.kind === "handoff" ? "handoff" : "login", box); return; }
    case "qrapprove": case "qrdeny": return soQRAnswer(b, what === "qrapprove");
    case "qrhandoff": return soQRHandoff();
    case "pushon": { const r = await soPushOn(); if (!r.ok) return toast(r.err, 4000); SO.push = { on: true }; toast("推送通知已开启"); return soDraw(); }
    case "pushofferclose": try { localStorage.setItem("miovrca.web.pushoffer", "0"); } catch (er) {} return soDraw();
    case "qrscan": soAccountClose(); return soQRScan();
    case "qrsignin": { const id = b.dataset.id; return soAccountDialog(() => soQROpen(id)); }
    case "accountdelete": return soAccountDelete();
    case "accountdeletego": return soAccountDeleteGo(b);
    case "accountexport": return soAccountExport(b);
    case "boothstart": case "boothcheck": case "boothunbind": return soBoothAct(what.slice(5), b);
    case "boothcopy": return copyText(SO.booth?.booth?.code || "", "已复制");
    case "githubstart": return soGitHubStart(b);
    case "githubunlink": return soGitHubUnlink(b);
    case "reportsend": return soReportSend(b);
    case "reportcancel": {
      const back = SO.report?.back; SO.report = null;
      if (back === "sowork" && SO.work?.data) { soWorkDraw(); showModal(); }
      else if (back === "sopost" && SO.post?.data?.ok) { soPostDraw(); showModal(); }
      else closeModal();
      return;
    }
    case "report": return soReport(b.dataset.kind, b.dataset.id, b.dataset.name);
    case "wedit": return soWorkEditOpen();
    case "wremove": return soRemoveWork();
    case "wish": {
      const id = b.dataset.id; b.disabled = true;
      const ok = await wishAdd({ id, title: b.dataset.name, shop: b.dataset.shop });
      if (ok && $("#modal").dataset.kind === "sowork") { await soOwned(SO.work); soWorkDraw(); }
      else if (ok && $("#modal").dataset.kind === "soclonepreview") { b.textContent = "已在愿望单"; b.classList.add("on"); }
      else b.disabled = false;
      return;
    }
    case "webinapp": return webOnlyInApp(b.dataset.work);
    case "newwork": return socialNewWork({});
    case "remix": { const w = SO.work?.data?.work; if (w) return socialNewWork({ based_on: w.id, base: w.base, assets: w.assets }); return; }
    case "basedpick": case "basedclear": {
      const d = soWdRead();
      if (what === "basedpick" && (!SO.lists?.works?.some(w => w.id === b.dataset.id) || b.dataset.id === d.id)) return;
      d.based_on = what === "basedpick" ? b.dataset.id : "";
      put($("#sw_based_selected"), soBasedSelected()); return;
    }
    case "showcasewish": return soShowcaseWish(b);
    case "showcaseall": case "showcasenone": {
      const c = soHomeRead(), hits = soShowcaseLibrary(c), ids = new Set(hits.map(a => a.booth_id));
      const next = what === "showcasenone" ? c.showcase.filter(a => !ids.has(a.booth_id)) : [...c.showcase, ...hits.filter(a => !c.showcase.some(x => x.booth_id === a.booth_id))];
      if (next.length > 60) { toast("素材展示柜最多 60 件"); return; }
      c.showcase = next; return soShowcaseRefresh();
    }
    case "avpick": {
      const files = await soPickImage(false); if (!files.length) return;
      try {
        const cropped = await soCropOpen(files[0]); if (!cropped) return;
        const check = nsfwCheck(cropped); // (a new avatar the check flags is saved with auto_flag: web/nsfw.js)
        const p = await soPicture(cropped, SO_AVATAR);
        const r = await api("/api/community/picture", { kind: "avatar", b64: p.b64 });
        if (!r.ok) { soServiceError(r); return; }
        const c = soHomeRead(); if (!c) return;
        c.uploaded = p.url; c.avatar = r.id; c.avatarFlag = !!(await check)?.flagged; soHomeDraw();
        if (await soHomeSave(false) && c.avatarFlag) toast("图片可能含敏感内容，将由管理员复核", 5000);
      } catch (x) { toast(x.message, 5000); }
      return;
    }
    case "bdclear": { const c = soHomeRead(); if (!c) return; c.month = c.day = 0; soHomeDraw(); return; }
    case "avclear": {
      const c = soHomeRead(); if (!c) return;
      c.uploaded = ""; c.avatar = ""; c.avatarFlag = false; soHomeDraw(); soHomeSave(false); return;
    }
    case "picpick": {
      const files = await soPickImage(true);
      if (!files.length) return;
      const d = soWdRead(); if (!d) return;
      try {
        const pics = [];
        for (const f of files.slice(0, 9)) { const check = nsfwCheck(f), p = await soPostPicture(f); p.flagged = !!(await check)?.flagged; pics.push(p); }
        if (pics.length) { d.pictures = pics; d.replacing = true; }
        soWorkEdit();
      } catch (x) { toast(x.message, 5000); }
      return;
    }
    case "picdel": {
      const d = soWdRead(); if (!d) return;
      if (d.replacing) d.pictures.splice(+b.dataset.i, 1); else d.existing.splice(+b.dataset.i, 1);
      soWorkEdit(); return;
    }
    case "homesave": return soHomeSave(false);
    case "linkadd": case "linkdel": {
      const id = b.dataset.for, holder = id === "sh_links" ? soHomeRead() : id === "rcp_links" ? recipeMetaRead() : soWdRead();
      if (!holder) return;
      if (what === "linkadd") holder.links.push({ url: "", label: "", kind: "" }); else holder.links.splice(+b.dataset.i, 1);
      if (id === "sh_links") soHomeDraw(); else if (id === "rcp_links") recipeRender(); else soWorkEdit();
      const rows = document.querySelectorAll(`#${id} .solinkrow .solu`); if (what === "linkadd" && rows.length) rows[rows.length - 1].focus();
      return;
    }
    case "assetadd": soWdRead().assets.push({ name: "", shop: "", url: "" }); soWorkEdit(); { const r = document.querySelectorAll("#sw_assets .soan"); if (r.length) r[r.length - 1].focus(); } return;
    case "assetdel": soWdRead().assets.splice(+b.dataset.i, 1); return soWorkEdit();
    case "assetlib": soWdRead(); soLibPick(""); { const q = $("#sw_libq"); if (q) q.focus(); } return;
    case "assetproject": return soProjectPick("");
    case "assetprojectpick": {
      const d = soWdRead(), a = d && (d.projectAssets || [])[+b.dataset.i];
      if (!a || d.assets.length >= 30) return;
      if (!d.assets.some(x => a.url ? x.url === a.url : x.name === a.name)) d.assets.push(Object.assign({}, a));
      const path = d.projectPath; soWorkEdit(); return soProjectPick(path);
    }
    case "assetpick": {
      const a = findAsset(b.dataset.key), d = soWdRead(); if (!a || !d || d.assets.length >= 30) return;
      const x = soAssetOf(a);
      if (!d.assets.some(y => y.url === x.url)) d.assets.push(x);
      const q = $("#sw_libq") ? $("#sw_libq").value : "";
      soWorkEdit(); soLibPick(q); return;
    }
    case "assetrecipe": return soAssetsFromRecipe();
    case "wsend": return soWorkSend(b);
  }
}
document.addEventListener("click", e => {
  const pop = $("#so_pop");
  if (pop && !pop.contains(e.target)) soPopClose();
  const b = e.target.closest("[data-so]");
  if (!b || b.disabled) return;
  Promise.resolve(soAction(b, e)).catch(err => toast("操作失败：" + ((err && err.message) || err), 4500));
});
document.addEventListener("keydown", e => {
  if (e.key === "Escape" && $("#so_pop")) { soPopClose(); e.stopPropagation(); return; }
  if ((e.key === "Enter" || e.key === " ") && e.target.matches && e.target.matches(".socard[data-so], .socreator[data-so], .sofrow[data-so], .sopostcard[data-so], .sohcard[data-so], .sxpost[data-so], .sxgroup[data-so]")) { e.preventDefault(); e.target.click(); }
}, true);
document.addEventListener("input", e => {
  const t = e.target;
  if (t.id === "sw_based_q") { SO.wd.basedQuery = t.value; put($("#sw_based_results"), soBasedChoices()); return; }
  if (t.id === "sh_showcase_q") { SO.home.showcaseQuery = t.value; put($("#sh_showcase_rows"), soShowcaseChoices(SO.home)); return; }
  if (t.closest && t.closest("#sh_form")) {
    if (t.id === "sh_month") {
      const c = soHomeRead(), days = [31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][(c.month || 1) - 1];
      c.day = Math.min(c.day, days); soHomeDraw(); return;
    }
    if (t.id === "sh_nick" || t.id === "sh_bio") { const n = $(`#${t.id}_n`); if (n) n.textContent = `${Array.from(t.value).length}/${t.maxLength}`; }
    soHomeRead(); soHomePreviewDraw();
    if (t.closest("#sh_links")) soKinds("sh_links", "profile", SO.home.links, () => soHomePreviewDraw());
    return;
  }
  if (t.id === "sw_libq") return soLibPick(t.value);
  if (t.closest && t.closest("#sw_links")) {
    const d = soWdRead();
    soKinds("sw_links", "work", d.links, () => { $("#sw_rightsrow").classList.toggle("sohide", !d.links.some(l => l.kind === "netdisk")); });
    return;
  }
  if (t.id === "sw_video") {
    const d = soWdRead();
    clearTimeout(soKindTimer);
    soKindTimer = setTimeout(async () => {
      let r; try { r = await api("/api/community/linkkinds", { urls: [], video: d.video }); } catch (x) { return; }
      d.vkind = r.video || ""; const tag = $("#sw_vkind");
      if (tag) { tag.className = "tag" + (d.vkind ? " ok" : d.video ? " warn" : ""); tag.textContent = d.vkind ? SO_VIDEO[d.vkind] : d.video ? "无法识别" : ""; }
    }, 300);
  }
});
ENTER_GOES.so_nickname = '[data-so="accountsave"]';
ENTER_GOES.so_handle = '[data-so="accountsave"]';
ENTER_GOES.so_code = '[data-so="accountsave"]';
ENTER_GOES.so_email = '[data-so="accountcode"]';
document.addEventListener("change", e => {
  const t = e.target;
  if (t.matches && t.matches(".solu, #sh_vrc")) { const v = soNormURL(t.value); if (v !== t.value) t.value = v; } // (what was typed, as the address it is)
  if (t.classList.contains("so_showcase_item")) {
    const c = SO.home, item = soShowcaseLibrary(c).find(a => a.booth_id === t.value);
    if (t.checked && item && !c.showcase.some(a => a.booth_id === t.value)) {
      if (c.showcase.length >= 60) { t.checked = false; toast("素材展示柜最多 60 件"); return; }
      c.showcase.push({ ...item });
    } else if (!t.checked) c.showcase = c.showcase.filter(a => a.booth_id !== t.value);
    soShowcaseRefresh(); return;
  }
  if (e.target.id === "sw_project") soProjectPick(e.target.value).catch(() => toast("无法读取工程"));
  if (e.target.closest && e.target.closest("#sh_form")) { soHomeRead(); soHomePreviewDraw(); }
});
setTimeout(() => soServiceLoad(false), 20000);

// Timeline rows are shared by home, search, bookmarks, quotes and threads.
const soPostCache = new Map();
function soSessionReset() {
  SO.timeline = null; SO.hfeed = null; SO.posts = null; SO.unread = {}; SO.notices = null; SO.dm = null; SO.settings = null; SO.booth = null; SO.page = null; SO.session++;
  SO.groups = null; if (SO.group) SO.group = {id: SO.group.id, back: SO.group.back, data: null, posts: null}; SO.asks = null;
  soPostCache.clear(); soViewPending.clear(); soViewSent.clear();
}
function soTimelineKey(it) { return `${it.kind}:${it.post.id}:${it.by?.id || ""}`; }
async function soTimelineLoad(more = false, refresh = false) {
  const filter = SO.timelineTab === "for_you" ? SO.timelineFilter : "";
  if (!SO.timeline || refresh) SO.timeline = {tab: SO.timelineTab, filter, items: [], next: "", busy: false, loaded: false, error: "", newer: []};
  const t = SO.timeline;
  if (t.busy || more && !t.next) return;
  t.busy = true; t.error = ""; soDraw();
  const r = await soServiceAPI("timeline", {tab: t.tab, filter: t.filter, after: more ? t.next : ""});
  t.busy = false;
  if (t !== SO.timeline) return;
  if (!r.ok) t.error = r.err;
  else {
    const seen = new Set(t.items.map(soTimelineKey));
    for (const it of r.items || []) if (!seen.has(soTimelineKey(it))) { seen.add(soTimelineKey(it)); t.items.push(it); }
    t.next = r.next || ""; t.loaded = true; t.noBases = !!r.noBases;
  }
  soDraw();
}
async function soTimelineCheck() {
  const t = SO.timeline;
  if (S.view !== "social" || SO.nav !== "home" || document.hidden || !t?.loaded || t.busy) return;
  const r = await soServiceAPI("timeline", {tab: t.tab, filter: t.filter});
  if (!r.ok || t !== SO.timeline) return;
  const seen = new Set(t.items.map(soTimelineKey));
  t.newer = (r.items || []).filter(it => !seen.has(soTimelineKey(it)));
  const bar = $("#sx_new");
  if (bar) { bar.hidden = !t.newer.length; bar.textContent = `显示 ${t.newer.length} 条新帖子`; }
}
setInterval(soTimelineCheck, 60000);
const soPageObservers = new Map();
function soPageWatch(selector, load) {
  const key = selector === "#sx_thread_more" ? "thread" : "main";
  soPageObservers.get(key)?.disconnect(); soPageObservers.delete(key);
  const el = $(selector);
  if (!el || typeof IntersectionObserver === "undefined") return;
  const observer = new IntersectionObserver(es => {
    if (es.some(e => e.isIntersecting) && el.isConnected) load();
  }, {rootMargin: "180px"});
  soPageObservers.set(key, observer); observer.observe(el);
}
// 「适配你的素体」: the posts whose work is made on one of the library's bases (internal/social/clone.go)
const SO_TTABS = [["for_you", "为你推荐", "spark"], ["following", "正在关注", "creators"], ["bases", "适配你的素体", "person"]].filter(([k]) => !SO_WEB || k !== "bases"); // (the bases are the library's)
// 为你推荐's filters (the service's timeline filter): the ranking, or only the newest, the most liked of the week,
// posts with pictures, works, or the posts that carry Booth items (Booth 情报)
const SO_TFILTERS = [["", "推荐", "spark"], ["featured", "精选", "featured"], ["latest", "最新发布", "clock"], ["hot", "热门", "hot"], ["media", "图片", "picture"], ["works", "作品", "work"], ["booth", "Booth 情报", "booth"]];
try { SO.timelineFilter = localStorage.getItem("so-for-you") || ""; } catch (e) { SO.timelineFilter = ""; }
if (!SO_TFILTERS.some(([k]) => k === SO.timelineFilter)) SO.timelineFilter = "";
function soTimelineFilters() {
  const icon = k => SO_ACT[k] || SO_TOOL[k] || "";
  return `<div class="sxfilters" role="radiogroup" aria-label="筛选">${SO_TFILTERS.map(([k, label, i]) => `<button class="sxfilter${SO.timelineFilter === k ? " on" : ""}" role="radio" aria-checked="${SO.timelineFilter === k}" data-so="xfilter" data-filter="${k}">${icon(i)}<span>${label}</span></button>`).join("")}</div>`;
}
function soTimelineDraw() {
  const t = SO.timeline;
  soBar(null, "", soTabs(SO_TTABS, SO.timelineTab, "xtab"));
  const rows = (t?.items || []).map(it => `${it.kind === "repost" ? `<div class="sxreposted">${SO_ACT.repost}<span data-i18n="off">${esc(it.by?.nickname || "")}</span> <span>转帖了</span></div>` : ""}${soPostCard(it.post)}`).join("");
  gridPut(`<div class="sxtimeline">${SO.timelineTab === "for_you" ? soTimelineFilters() + soThemeHTML(true) : ""}${SO_WEB && !SO.service?.account ? "" : soComposerHTML("home")}<button class="sxnew" id="sx_new" data-so="xnew"${t?.newer?.length ? "" : " hidden"}>${esc(`显示 ${t?.newer?.length || 0} 条新帖子`)}</button>
    ${t?.error ? `<p class="warnc">${esc(t.error)}</p><button class="btn" data-so="xretry">重试</button>` : ""}
    ${rows || (t?.loaded ? soEmpty(t.noBases ? "素材库中还没有素体" : "暂无内容") : !t?.error ? `<p class="muted">正在读取…</p>` : "")}
    ${t?.next ? `<button class="btn sxmore" id="sx_more" data-so="xmore"${t.busy ? " disabled" : ""}>加载更多</button>` : t?.loaded ? `<p class="sohend">没有更多了</p>` : ""}</div>`);
  soPageWatch("#sx_more", () => soTimelineLoad(true));
}
function soRelative(s) {
  const ms = Date.now() - Date.parse(s), n = Math.max(0, Math.floor(ms / 60000));
  if (!Number.isFinite(ms)) return "";
  return n < 1 ? "刚刚" : n < 60 ? `${n} 分钟前` : n < 1440 ? `${Math.floor(n / 60)} 小时前` : `${Math.floor(n / 1440)} 天前`;
}
// a post's share address (https://<the service>/p/<id>, or miovrca://post/<id>): the post's ID, else ""
const SO_SHARE_HOSTS = new Set(["api.miovrc.com"]);
function soSharedID(value) {
  let u; try { u = new URL(value); } catch (_) { return ""; }
  const m = u.protocol === "miovrca:" ? /^\/\/post\/([pcmg][0-9]{1,18})$/.exec(u.pathname || "") || /^post\/([pcmg][0-9]{1,18})$/.exec((u.host || "") + u.pathname)
    : u.protocol === "https:" && SO_SHARE_HOSTS.has(u.host) && !u.search ? /^\/p\/([pcmg][0-9]{1,18})\/?$/.exec(u.pathname) : null;
  return m ? m[1] : "";
}
function soPostText(text, mentions = []) {
  const re = /https:\/\/[^\s<>"'`]+|miovrca:\/\/post\/[pcmg][0-9]{1,18}|@[A-Za-z0-9_]{4,15}|#[\p{L}\p{N}_]{1,30}/gu;
  let html = "", end = 0;
  for (const match of String(text || "").matchAll(re)) {
    const value = match[0], at = match.index;
    html += esc(text.slice(end, at)); end = at + value.length;
    const boundary = at === 0 || !/[\p{L}\p{N}_/:@#]/u.test(text[at - 1]);
    if (!boundary) { html += esc(value); continue; }
    if (soSharedID(value)) html += `<button class="linkbtn" data-so="xopenshared" data-id="${esc(soSharedID(value))}">${esc(value)}</button>`;
    else if (value.startsWith("miovrca://")) html += esc(value);
    else if (value.startsWith("https://")) {
      let safe = false;
      try { const u = new URL(value); safe = u.protocol === "https:" && !u.username && !u.password && !u.port; } catch (_) {}
      html += safe ? `<button class="linkbtn" data-so="url" data-href="${esc(value)}">${esc(value)}</button>` : esc(value);
    } else if (value[0] === "@") {
      const person = mentions.find(p => p.handle.toLowerCase() === value.slice(1).toLowerCase());
      html += `<button class="linkbtn" data-so="xmention" data-handle="${esc(value.slice(1))}" data-id="${esc(person?.id || "")}">${esc(value)}</button>`;
    } else html += `<button class="linkbtn" data-so="posttag" data-tag="${esc(value.slice(1))}">${esc(value)}</button>`;
  }
  return html + esc(String(text || "").slice(end));
}
// A sensitive post's pictures are blurred behind 「敏感内容」 and 「显示」 until they are shown for that post, or the
// account shows them all (「总是显示敏感内容」); its text, avatars and banners are not affected.
function soHideMedia(p) { return !!p?.sensitive && !SO.service?.account?.show_sensitive && !SO.revealed.has(p.id); }
function soWorkMedia(w) { return { id: (w.id || "").replace(/^w/, "g"), sensitive: w.sensitive }; }
function soSensCover(id) { return `<span>敏感内容</span><button class="btn small" data-so="xreveal" data-id="${esc(id)}">显示</button>`; }
// soSensWrap: the pictures' html, blurred and out of reach (inert) under the cover when they are to be hidden
function soSensWrap(p, html, cls = "") {
  if (!html || !soHideMedia(p)) return html;
  return `<div class="sxsens${cls ? " " + cls : ""}" data-sens="${esc(p.id)}"><div class="sxsensblur" inert>${html}</div><div class="sxsenscover">${soSensCover(p.id)}</div></div>`;
}
// soReveal: the post's pictures shown where they are, without drawing the list again
function soReveal(id) {
  SO.revealed.add(id);
  for (const el of document.querySelectorAll(".sxsens")) {
    if (el.dataset.sens !== id) continue;
    const inner = el.querySelector(":scope > .sxsensblur");
    if (inner) el.replaceWith(...inner.childNodes); else el.remove();
  }
  for (const img of document.querySelectorAll("img.sxsensimg")) if (img.dataset.sensimg === id) img.classList.remove("sxsensimg");
}
// a work card's 「已拥有 n/m」 (its Booth items the library has), each missing item by its name (its Booth page), and
// 「N 人做了同款」. items: [{name, url, owned}], as timeline.go and the work page give them.
function soWorkHave(owned, total, items, clones) {
  if (SO_WEB) return clones > 0 ? `<p class="sxclones">${esc(`${clones} 人做了同款`)}</p>` : "";
  const missing = (items || []).filter(x => x.boothId && !x.owned && x.url);
  return `${total ? `<p class="sxowned">${esc(`已拥有 ${owned}/${total}`)}</p>` : ""}${missing.length ? `<p class="sxmissing"><span>缺少</span>${missing.map(x => `<button class="linkbtn" data-so="link" data-kind="booth_item" data-href="${esc(x.url)}" title="${esc(x.url)}" data-i18n="off">${esc(x.name)}</button>`).join("")}</p>` : ""}${clones > 0 ? `<p class="sxclones">${esc(`${clones} 人做了同款`)}</p>` : ""}`;
}
function soPostCard(p, large = false, embedded = false, author = "", ask = null) {
  if (p.deleted) return `<div class="sxdeleted" data-id="${esc(p.id)}">此帖已删除</div>`;
  soPostCache.set(p.id, p);
  if (p.share) try { SO_SHARE_HOSTS.add(new URL(p.share).host); } catch (_) {}
  const a = p.author || {}, pics = (p.pics || []).slice(0, p.kind === "work" ? 9 : 4), w = p.workCard, hide = soHideMedia(p);
  const workMedia = w?.sensitive ? soWorkMedia(w) : p, hideWork = soHideMedia(workMedia);
  const action = (key, icon, label, count, on) => `<button class="sxact a-${icon}" data-so="x${key}" data-id="${esc(p.id)}" title="${label}" aria-label="${label}"${on === undefined ? "" : ` aria-pressed="${!!on}"`}><span class="sxi">${SO_ACT[icon]}</span>${count === undefined ? "" : `<span class="num">${soCountHTML(count)}</span>`}</button>`;
  // (the work's cover sits in a button, so it is blurred alone; 「敏感内容」 and 「显示」 follow the button when the
  // post has no pictures of its own to carry them)
  const cover = w?.pic ? `<img src="${esc(w.pic)}" alt="" loading="lazy"${hideWork ? ` class="sxsensimg" data-sensimg="${esc(workMedia.id)}"` : ""}>` : "";
  const coverNote = hideWork && w?.pic && (!pics.length || !hide || workMedia.id !== p.id) ? `<div class="sxsens sxsensbar" data-sens="${esc(workMedia.id)}">${soSensCover(workMedia.id)}</div>` : "";
  const work = w ? `<section class="sxwork"><button class="sxworkhead" data-so="work" data-id="${esc(w.id)}">${cover}<span><b data-i18n="off">${esc(w.title)}</b><small data-i18n="off">${esc(w.base || "")}</small>${w.own_shop ? `<span class="tag sxownshop">作者本人</span>` : ""}</span></button>${coverNote}${soWorkHave(w.owned, w.total, w.items, w.clones)}<div class="btnrow">${w.recipe ? SO_WEB ? `<button class="btn small primary" data-so="webinapp" data-work="${esc(w.id)}">一键克隆</button>` : `<button class="btn small primary" data-so="xclone" data-id="${esc(p.id)}">一键克隆</button><button class="btn small" data-so="xclonebase" data-id="${esc(p.id)}">换素体克隆</button><button class="btn small" data-so="xalong" data-id="${esc(p.id)}">照着装</button><button class="btn small" data-so="xrecipe" data-id="${esc(p.id)}">装配方案</button>` : ""}<button class="btn small" data-so="xcredits" data-id="${esc(p.id)}">复制使用素材</button></div></section>` : "";
  const quote = p.quote ? `<div class="sxquote">${soPostCard(p.quote, false, true)}</div>` : p.quote_of || p.quote_unavailable ? `<div class="sxquote sxdeleted">此帖已不可用</div>` : !embedded ? soSharedCard(p) : "";
  const group = p.group && p.group_name && !(SO.nav === "group" && !SO.page && SO.group?.id === p.group) ? `<button class="sxingroup" data-so="groupopen" data-id="${esc(p.group)}">${SO_ICON.creators}<span data-i18n="off">${esc(p.group_name)}</span></button>` : "";
  return `<article class="sxpost${large ? " large" : ""}${embedded ? " embedded" : ""}${p.featured && !embedded ? " featured" : ""}" data-so="post" data-id="${esc(p.id)}" data-view-post="${esc(p.id)}" tabindex="0">
    ${p.featured && !embedded ? `<div class="sxfeatured">${SO_ACT.featured}<span>精选</span></div>` : ""}${group}${p.parent_deleted ? `<div class="sxdeleted">此帖已删除</div>` : ""}<div class="sxposthead">${soProfileBtn(a.id?.startsWith("gh:") ? "" : a.id, a.github || (a.id?.startsWith("gh:") ? a.id.slice(3) : ""), a.nickname, p.authorPic, false, a.booth)}${soBotMark(a)}<span class="sxhandle" data-i18n="off">@${esc(a.handle || a.github || "")}</span><span class="sxwhen">· <span>${esc(soRelative(p.created))}</span></span>${p.promoted ? `<span class="sxad">推广</span>` : ""}${!embedded ? `<button class="wb sxmenu" data-so="xmenu" data-id="${esc(p.id)}" title="更多" aria-label="更多">⋯</button>` : ""}</div>
    ${author && a.id === author ? `<span class="sxauthor">作者</span>` : ""}${soAskMarksHTML(p, ask)}<div class="sxtext" data-i18n="off">${soPostText(p.text || "", p.mentions)}</div>
    ${embedded ? "" : soTranslateHTML(p)}${soPollHTML(p, embedded)}
    ${soSensWrap(p, pics.length ? `<div class="sxmedia n${Math.min(pics.length, 4)}">${pics.slice(0, 4).map((src, i) => `<button data-so="xpicture" data-id="${esc(p.id)}" data-i="${i}" aria-label="${i + 1}"><img src="${esc(src)}" alt="" loading="lazy" decoding="async"></button>`).join("")}</div>` : "")}
    ${soBoothCardsHTML(p)}${soAssetItemsHTML(p)}${work}${quote}${soAnswerButtonHTML(p, ask, embedded)}${large ? `<time class="sxdate" data-i18n="off" datetime="${esc(p.created)}">${esc(new Date(p.created).toLocaleString())}</time>${!p.mine && soPostTarget(p) ? soFollowBtn(soPostTarget(p), SO.follows[soPostTarget(p)] ?? p.followed, false) : ""}` : ""}
    ${!embedded ? `<div class="sxactions">${action("reply", "reply", "回复", p.replies)}${action("repostmenu", "repost", "转帖", p.reposts, p.reposted)}${action("like", "like", "喜欢", p.likes, p.liked)}<span class="sxact a-views" title="浏览" aria-label="浏览"><span class="sxi">${SO_ACT.views}</span><span class="num sxviews">${soCountHTML(p.views)}</span></span><span class="sxacttail">${action("bookmark", "bookmark", "书签", undefined, p.bookmarked)}${action("share", "share", "分享")}</span></div>` : ""}</article>`;
}
// a count as X shows it (1.2万 / 1.2K / 3.4M where the interface is not Chinese), each digit a cell the number rolls in
function soCount(n) {
  n = Math.max(0, +n || 0);
  if (n < 10000) return String(n);
  const zh = !I18N.lang || I18N.lang === "zh", d = (v, u) => `${v >= 100 ? Math.round(v) : Math.round(v * 10) / 10}${u}`;
  if (zh) return n < 1e8 ? d(n / 1e4, "万") : d(n / 1e8, "亿");
  return n < 1e6 ? d(n / 1e3, "K") : d(n / 1e6, "M");
}
function soCountHTML(n) { return n ? `<span class="numv" data-i18n="off">${esc(soCount(n))}</span>` : `<span class="numv"></span>`; }
// the new count rolls in from below (up) or from above, the old one rolls out the other way
function soCountRoll(num, n, up) {
  const old = $(".numv", num), text = n ? soCount(n) : "";
  if (old && old.textContent === text) return;
  const next = document.createElement("span"); next.className = "numv in " + (up ? "up" : "down"); next.dataset.i18n = "off"; next.textContent = text;
  // (the old number leaves as a picture of itself, ::before, so the count's text is the new number alone at once)
  if (old) { old.className = "numv out " + (up ? "up" : "down"); old.dataset.v = old.textContent; old.textContent = ""; setTimeout(() => old.remove(), 360); }
  num.append(next);
}
// a bot account of the service (Booth 情报, promotions): its mark beside the name
function soBotMark(a) { return a?.bot ? `<span class="sxbot" title="机器人账号" aria-label="机器人账号">${SO_ACT.bot}</span>` : ""; }
// Booth items a post carries (the service's Booth 情报 and promotions): picture, name, shop, price and what kind of
// news it is; 「已拥有」 for those in the library. A click opens the item's page.
const SO_BOOTH_KIND = { new: "新作", sale: "打折", free: "限免", preorder: "预售" };
function soBoothCardsHTML(p) {
  const items = (p.boothCards || p.booth_items || []).filter(x => x && x.id);
  if (!items.length) return "";
  const owned = new Set(((S.data && S.data.assets) || []).map(a => a.boothId).filter(Boolean));
  const yen = n => "¥" + Number(n).toLocaleString("ja-JP");
  return `<div class="sxbooth${items.length === 1 ? " one" : ""}">${items.map(x => {
    const off = x.was > x.price && x.price >= 0 ? Math.round((1 - x.price / x.was) * 100) : 0, mine = x.owned || owned.has(String(x.id));
    return `<button class="sxbitem" data-so="booth" data-id="${esc(x.id)}" title="${esc(x.title || "")}"><span class="sxbpic">${x.pic ? `<img src="${esc(x.pic)}" alt="" loading="lazy" decoding="async">` : SO_TOOL.booth}${SO_BOOTH_KIND[x.kind] ? `<span class="sxbkind k-${esc(x.kind)}">${SO_BOOTH_KIND[x.kind]}</span>` : ""}</span>
      <span class="sxbbody"><b data-i18n="off">${esc(x.title || "#" + x.id)}</b>${x.shop ? `<small data-i18n="off">${esc(x.shop)}</small>` : ""}<span class="sxbprice">${x.price === 0 ? `<span class="free">免费</span>` : x.price > 0 ? `<span data-i18n="off">${yen(x.price)}</span>` : ""}${off ? `<s data-i18n="off">${yen(x.was)}</s><span class="off" data-i18n="off">-${off}%</span>` : ""}</span>
      ${x.base || mine ? `<span class="sxbmeta">${x.base ? `<span class="base" data-i18n="off">${esc(x.base)}</span>` : ""}${mine ? `<span class="tag ok">已拥有</span>` : ""}</span>` : ""}</span></button>`;
  }).join("")}</div>`;
}
// a post as a small card: who, the first lines, the first picture (or the work's cover); a click opens it
function soPostMini(q) {
  if (q.deleted) return `<div class="sxmini sxdeleted">此帖已删除</div>`;
  const a = q.author || {}, pic = (q.pics || [])[0] || q.workCard?.pic || "", hide = soHideMedia(q);
  return `<button class="sxmini" data-so="xopenshared" data-id="${esc(q.id)}"><span class="sxminibody"><span class="sxminihead">${soAv(q.authorPic, a.nickname, "xs")}<b data-i18n="off">${esc(a.nickname || "")}</b>${soBotMark(a)}<span class="sxhandle" data-i18n="off">@${esc(a.handle || a.github || "")}</span></span>
    <span class="sxminitext" data-i18n="off">${esc(q.text || q.workCard?.title || "")}</span></span>${pic ? `<img src="${esc(pic)}" alt=""${hide ? ' class="sxsensimg"' : ""} loading="lazy">` : ""}</button>`;
}
// a post that shares another post's address shows that post as a card (fetched once, then kept with the others)
const soSharedWant = new Set();
function soSharedCard(p) {
  const id = [...String(p.text || "").matchAll(/https:\/\/[^\s<>"'`]+|miovrca:\/\/post\/[pcmg][0-9]{1,18}/g)].map(m => soSharedID(m[0])).find(x => x && x !== p.id);
  if (!id) return "";
  const q = soPostCache.get(id);
  if (q) return soPostMini(q);
  if (!soSharedWant.has(id)) {
    soSharedWant.add(id);
    soServiceAPI("thread", {id}).then(r => { const post = r.ok && (r.post || r.ancestors?.[r.ancestors.length - 1]); if (post && post.id === id) { soPostCache.set(id, post); soDraw(); } });
  }
  return `<button class="sxmini sxsharewait" data-so="xopenshared" data-id="${esc(id)}">${SO_ICON.link || ""}<span>MioVRCA 帖子</span></button>`;
}
async function soPostOpen(id) {
  SO.post = {id, data: null, busy: false};
  SO.drafts.thread = soNewDraft({reply_to: id});
  soPostDraw(); showModal();
  await soThreadLoad(false);
}
async function soThreadLoad(more) {
  const x = SO.post;
  if (!x || x.busy || more && !x.data?.next) return;
  x.busy = true;
  const r = await soServiceAPI("thread", {id: x.id, after: more ? x.data.next : ""});
  x.busy = false;
  if (SO.post !== x) return;
  if (more && r.ok && x.data?.ok) {
    const ids = new Set(x.data.replies.map(p => p.id));
    x.data.replies.push(...r.replies.filter(p => !ids.has(p.id))); x.data.next = r.next;
  } else x.data = r;
  if (r.ok) { SO.drafts.thread.target = r.post; soViewQueue(r.post.id); }
  if ($("#modal")?.dataset.kind === "sopost") soPostDraw();
}
function soPostDraw() {
  const m = $("#modal"), r = SO.post?.data;
  m.dataset.kind = "sopost";
  m.innerHTML = `<div class="mhead">帖子</div><div class="mbody sxthread">${!r ? `<p>正在读取…</p>` : !r.ok ? `<p class="warnc">${esc(r.err)}</p><button class="btn" data-so="xthreadretry">重试</button>` : `${(r.ancestors || []).map(p => soPostCard(p)).join("")}${soPostCard({...r.post, parent_deleted: r.post.parent_deleted && !(r.ancestors || []).some(p => p.deleted)}, true)}${soComposerHTML("thread")}${(r.replies || []).map(p => soPostCard(p, false, false, r.post.author?.id, r.post.ask ? r.post : null)).join("")}${r.next ? `<button class="btn" data-so="xthreadmore" id="sx_thread_more">加载更多</button>` : ""}`}</div><div class="mfoot"><button class="btn primary" data-m="close">关闭</button></div>`;
  soViewsWatch(); soPageWatch("#sx_thread_more", () => soThreadLoad(true));
}
async function soSearchLoad(reset = false) {
  const nav = SO.nav;
  if (reset || !SO.posts) SO.posts = {items: [], accounts: [], next: "", busy: false, loaded: false, q: SO.q, nav};
  const p = SO.posts;
  if (p.busy) return;
  p.busy = true;
  const r = await soServiceAPI(nav === "bookmarks" ? "bookmarks" : "search", {type: SO.searchType, q: p.q, after: reset ? "" : p.next});
  p.busy = false;
  if (p !== SO.posts) return;
  if (r.ok) { const ids = new Set(p.items.map(x => x.id)); p.items.push(...(r.posts || []).filter(x => !ids.has(x.id))); p.accounts.push(...(r.accounts || [])); p.next = r.next; p.loaded = true; p.error = ""; }
  else p.error = r.err;
  soDraw();
}
function soSearchDraw() {
  soBar(null); const p = SO.posts;
  gridPut(`<div class="sxtimeline">${SO.nav === "bookmarks" ? `<h2 class="sxpagetitle">书签</h2>` : soSearchBox() + soThemesForTag() + soTabs([["top", "热门", "hot"], ["latest", "最新", "clock"], ["people", "用户", "person"], ["media", "媒体", "posts"]], SO.searchType, "searchtab")}${SO.nav === "search" && SO.searchType === "people" ? `${(p?.accounts || []).map(soPersonRow).join("") || soEmpty(p?.loaded ? "暂无内容" : "正在读取…")}${p?.error ? `<p class="warnc">${esc(p.error)}</p>` : ""}${p?.next ? `<button class="btn" data-so="xsearchmore">加载更多</button>` : ""}` : soPostListHTML(p, SO.nav === "search" && SO.searchType === "media", "xsearchmore")}</div>`);
}
function soPostTagOpen(tag) { SO.q = "#" + tag; SO.searchType = "top"; SO.nav = "search"; SO.page = null; closeModal(); if ($("#q")) $("#q").value = SO.q; soThemesLoad(false); soSearchLoad(true); }
async function soUnreadLoad() {
  const session = SO.session, r = await soServiceAPI("unread");
  if (session !== SO.session) return;
  if (r.ok) { SO.unread = r; soPollLast = Date.now(); soDot(); if (S.view === "social") renderSocialSide(); }
}
async function soNoticesLoad() {
  const tab = SO.noticeTab, session = SO.session;
  const r = await soServiceAPI("notices", {tab});
  if (session !== SO.session || tab !== SO.noticeTab) return;
  SO.notices = r; await soUnreadLoad(); soDraw();
}
// each kind of notice with its icon on the left, as X shows them (a like's heart, a repost's arrows, a follow's
// person); a reply, a quote or a mention is the post itself under a line that says which it is
const SO_NOTICE = {like: ["like", "喜欢了你的帖子"], repost: ["repost", "转帖了你的帖子"], quote: ["quote", "引用了你的帖子"], reply: ["reply", "回复了你的帖子"],
  mention: ["mention", "提到了你"], follow: ["follow", "关注了你"], answer: ["answer", "将你的回复设为答案"], featured: ["featured", "你的帖子被选为精选"]};
function soNoticesDraw() {
  const r = SO.notices;
  soBar(null);
  const rows = (r?.notices || []).map(n => {
    const [icon, label] = SO_NOTICE[n.kind] || SO_NOTICE.mention;
    const actors = n.from || [], first = actors[0], extra = Math.max(0, (n.count || actors.length) - 1);
    const mark = `<span class="sxnicon k-${esc(icon)}" aria-hidden="true">${SO_ACT[icon]}</span>`;
    const who = !first ? `<span>${label}</span>` : `<b data-i18n="off">${esc(first?.nickname || "")}</b>${extra ? ` <span>${esc(`和另外 ${extra} 人`)}</span>` : ""} <span>${label}</span>`;
    if (["quote", "reply", "mention"].includes(n.kind) && n.item)
      return `<section class="sxnotice withpost${n.unread ? " unread" : ""}" data-kind="${esc(n.kind)}" data-post="${esc(n.post || "")}" data-seq="${esc(n.seq)}"><div class="sxnoticeline">${mark}<span>${who}</span></div>${soPostCard(n.item)}</section>`;
    const group = `<div class="sxactors">${actors.map(a => soProfileBtn(a.id?.startsWith("gh:") ? "" : a.id, a.github, a.nickname, a.pic)).join("")}</div><div class="sxnoticewho">${who}</div>`;
    return `<section class="sxnotice${n.unread ? " unread" : ""}" data-kind="${esc(n.kind)}" data-post="${esc(n.post || "")}" data-seq="${esc(n.seq)}">${mark}<div class="sxnoticebody">${group}${n.item ? `<button class="sxnoticepost" data-so="post" data-id="${esc(n.post)}"><span class="sxtext" data-i18n="off">${esc(n.item.text || n.item.workCard?.title || "")}</span></button>` : ""}</div></section>`;
  }).join("");
  const unread = (r?.notices || []).some(n => n.unread) || (SO.unread.notices || 0) > 0;
  gridPut(`<div class="sxtimeline">${soPushOffer()}<div class="sxpagehead"><h2>通知</h2><button class="btn small" data-so="noticesread"${unread ? "" : " disabled"}>全部已读</button></div>${soTabs([["all", "全部", "notices"], ["mentions", "提及", "mention"]], SO.noticeTab, "noticetab")}${r?.ok ? rows || soEmpty("暂无通知") : soEmpty(esc(r?.err || "正在读取…"))}</div>`);
}
// 全部已读: every notice up to the newest counts as seen on this account (the newest comes first in 「全部」)
async function soNoticesReadAll(btn) {
  btn.disabled = true;
  const latest = await soServiceAPI("notices", {tab: "all"});
  const seq = Math.max(0, ...((latest.ok && latest.notices) || SO.notices?.notices || []).map(n => +n.seq || 0));
  const r = await soServiceAPI("notices/seen", {seq});
  if (!r.ok) { btn.disabled = false; soServiceError(r); return; }
  for (const n of SO.notices?.notices || []) n.unread = false;
  SO.unread.notices = 0; soDot(); await soUnreadLoad(); soDraw();
}

// Each composer owns a draft so opening a quote never consumes the inline draft.
function soNewDraft(extra = {}) { return {text: "", pictures: [], error: "", busy: false, drag: -1, ...extra}; }
function soDraft(key) { return SO.drafts[key] || (SO.drafts[key] = soNewDraft()); }
async function soComposeOpen(mode = "", target = null, extra = {}) {
  if (!SO.service?.account) { soAccountDialog(() => soComposeOpen(mode, target, extra)); return; }
  SO.compose = SO.drafts.dialog = soNewDraft({target, reply_to: mode === "reply" ? target?.id : "", quote_of: mode === "quote" ? target?.id : "", ask: mode === "ask", ...extra});
  if (!SO.lists) await soLoad("lists");
  soComposeDraw(); showModal();
  const t = $("#sc_dialog_text"); if (t) soComposeGrow(t);
  if (t && extra.text) { t.focus(); t.setSelectionRange(t.value.length, t.value.length); }
}
function soComposerHTML(key) {
  const c = soDraft(key), count = Array.from(c.text).length, pending = c.pictures.some(p => p.pending);
  const hint = c.ask ? "这套是什么，怎么搭" : c.reply_to ? "发布你的回复" : "有什么新鲜事？", answering = soAnswering(c);
  return `<section class="sxcomposer socomposeform${answering ? " answering" : ""}" data-composer="${key}">
    ${c.reply_to && c.target ? `<p class="sxreply"><span>回复</span> <span data-i18n="off">@${esc(c.target.author?.handle || "")}</span></p>` : ""}
    <textarea id="sc_${key}_text" rows="${key === "dialog" ? 5 : 2}" placeholder="${hint}" aria-label="${hint}">${esc(c.text)}</textarea>
    <div class="sxpeople" id="sc_${key}_people" role="listbox"></div>
    ${c.quote_of && c.target ? `<div class="sxquote">${soPostCard(c.target, false, true)}</div>` : ""}
    <div class="sopicpick" id="sc_${key}_pics">${soComposePicsHTML(c)}</div>
    ${c.poll ? soPollEditHTML(c) : ""}${answering ? soAnswerDraftHTML(c, key) : ""}
    ${key === "dialog" && c.workDraft ? soWorkDraftHTML(c.workDraft) : key === "dialog" && !c.reply_to && !c.quote_of && !c.ask ? `<label class="sxworkpick" for="sc_dialog_work"><span>关联作品</span><select id="sc_dialog_work"><option value="">不关联</option>${(SO.lists?.works || []).map(w => `<option value="${esc(w.id)}"${c.work === w.id ? " selected" : ""} data-i18n="off">${esc(w.title)}</option>`).join("")}</select></label>` : ""}
    <p class="warnc small" id="sc_${key}_error" role="status">${esc(c.error)}</p>
    ${soComposeNote(c) ? `<p class="small sxsensnote" id="sc_${key}_note" role="status">${soComposeNote(c)}</p>` : ""}
    <div class="sxtools"><button class="sxtool" data-so="xpick" title="添加图片" aria-label="添加图片"${c.pictures.length >= 4 || c.busy || c.poll ? " disabled" : ""}>${SO_TOOL.picture}<span data-i18n="off">${c.pictures.length}/4</span></button><button class="sxtool" data-so="emoji" data-for="sc_${key}_text" title="插入表情" aria-label="插入表情"${c.busy ? " disabled" : ""}>${SO_TOOL.smile}</button>${answering ? soAnswerToolsHTML(c) : ""}${!c.reply_to && !c.quote_of && !c.workDraft && !c.ask ? `<button class="sxtool" data-so="xpoll" title="投票" aria-label="投票"${c.pictures.length || c.busy || c.poll ? " disabled" : ""}>${SO_TOOL.poll}</button>` : ""}${c.pictures.length && !c.workDraft ? `<button class="chip sxsenstoggle${c.sensitive ? " on" : ""}" data-so="xsensitive" aria-pressed="${!!c.sensitive}">敏感内容</button>` : ""}<span class="spacer"></span><span id="sc_${key}_count" data-i18n="off" class="sxcnt${count > 1000 ? " over" : count > 900 ? " near" : ""}">${count}/1000</span><button class="btn primary sxsend" data-so="xpublish"${c.busy || pending || count > 1000 ? " disabled" : ""}>${c.reply_to ? "回复" : "发布"}</button></div></section>`;
}
// The pictures under the text, as X lays them out (one large, two side by side, three or four in a grid), each with
// its own 「移除」 always in sight; a picture still uploading shows at once, dimmed, and can be removed as well
function soComposePicsHTML(c) {
  const n = c.pictures.length;
  if (!n) return "";
  return `<div class="sxpicgrid n${n}">${c.pictures.map((p, i) => `<span class="sopic${p.pending ? " pending" : ""}" draggable="${n > 1}" data-i="${i}"><img src="${esc(p.url)}" alt="" draggable="false">${p.pending ? `<i class="spin" aria-hidden="true"></i>` : ""}<button class="sxpicx" data-so="xpicdel" data-i="${i}" title="移除" aria-label="移除">${ICON.x}</button>${n > 1 ? `<span class="sopicacts">${i ? `<button class="wb" data-so="xpicmove" data-i="${i}" data-dir="-1" title="前移" aria-label="前移">‹</button>` : ""}${i < n - 1 ? `<button class="wb" data-so="xpicmove" data-i="${i}" data-dir="1" title="后移" aria-label="后移">›</button>` : ""}</span>` : ""}</span>`).join("")}</div>`;
}
function soComposeDraw(key = "dialog") {
  if (key === "dialog") {
    const m = $("#modal"), keep = focusMark(m); m.dataset.kind = "sopostedit";
    m.innerHTML = `<div class="mhead sxmhead"><span>${soDraft(key).ask ? "求同款" : "发帖"}</span><button class="wb sxmclose" data-m="close" title="关闭" aria-label="关闭">${ICON.x}</button></div><div class="mbody">${soComposerHTML(key)}</div>`;
    keep();
  } else { const el = $(`[data-composer="${key}"]`); if (el) { const keep = focusMark(el.parentNode); el.outerHTML = soComposerHTML(key); keep(); } }
  const t = $(`#sc_${key}_text`); if (t) soComposeGrow(t);
}
// the box grows with what is typed, up to about twelve lines (then it scrolls)
function soComposeGrow(t) {
  t.style.height = "auto";
  const h = t.scrollHeight; // (0 while it is not in sight: then its rows decide)
  t.style.height = h ? Math.min(h + 2, 340) + "px" : "";
}
function soComposeRelease(c) { for (const p of c?.pictures || []) if (p.url?.startsWith("blob:")) URL.revokeObjectURL(p.url); }
function soComposeError(key) { const el = $(`#sc_${key}_error`); if (el) el.textContent = soDraft(key).error; }
// The picture check (web/nsfw.js) marks each picture it finds likely sensitive (p.flagged). A flagged picture ticks
// 「敏感内容」 once, which the author may untick; when no flagged picture is left, a tick the check made goes again.
// Left unticked, or with a work card (works have no mark), the post is sent with auto_flag: one automatic report
// that lists it for the admin (internal/socialsrv, socialapi.AutoFlagReason).
function soComposeFlag(c) {
  const flagged = c.pictures.some(p => p.flagged);
  if (flagged && !c.flagged && !c.workDraft && !c.sensitive) c.sensitive = c.autoTick = true;
  else if (!flagged && c.flagged && c.autoTick) c.sensitive = c.autoTick = false;
  c.flagged = flagged;
}
function soComposeNote(c) {
  if (!c.pictures.some(p => p.flagged)) return "";
  return c.sensitive && !c.workDraft ? "图片可能含敏感内容，已标记为敏感" : "图片可能含敏感内容，将由管理员复核";
}
// Each picture shows at once (the file itself) and is then uploaded and checked on its own, one after another; the
// composer stays usable meanwhile — a picture can be removed, moved or another added before its upload is done, and
// one removed by then is not kept. 「发布」 waits until none is pending.
async function soComposeAdd(files, key = "dialog") {
  const c = soDraft(key);
  if (c.busy) return;
  if (!SO.service?.account) { soAccountDialog(() => soComposeAdd(files, key)); return; }
  files = [...files].filter(f => /^image\//.test(f.type));
  if (!files.length) return;
  const room = Math.max(0, 4 - c.pictures.length);
  const added = files.slice(0, room).map(file => ({url: URL.createObjectURL(file), pending: true, file}));
  c.error = files.length > room ? "最多 4 张图片" : "";
  c.pictures.push(...added); soComposeDraw(key);
  for (const pic of added) {
    if (!c.pictures.includes(pic)) continue; // (removed before its turn)
    try {
      const check = nsfwCheck(pic.file); // (alongside the upload; the first picture loads the check)
      const prepared = await soPostPicture(pic.file);
      const r = c.pictures.includes(pic) ? await soServiceAPI("picture", {kind: "post", b64: prepared.b64}) : null;
      if (r && !r.ok) throw new Error(r.err);
      if (r) { pic.id = r.id; pic.flagged = !!(await check)?.flagged; }
    } catch (e) {
      c.error = e.message || "无法读取图片";
      const i = c.pictures.indexOf(pic); if (i >= 0) { c.pictures.splice(i, 1); URL.revokeObjectURL(pic.url); }
    } finally { pic.pending = false; delete pic.file; }
    soComposeFlag(c);
    // (the composer's window may have closed, or shows something else by now)
    if (soDraft(key) === c && (key !== "dialog" || $("#modal.on")?.dataset.kind === "sopostedit")) soComposeDraw(key);
  }
}
async function soComposeSend(key) {
  const c = soDraft(key);
  if (c.busy) return;
  if (!SO.service?.account) { soAccountDialog(() => soComposeSend(key)); return; }
  const text = c.text.trim(), wd = c.workDraft, title = wd ? String(wd.title || "").trim() : "";
  const poll = c.poll && !wd ? {options: c.poll.options.map(o => o.trim()), days: c.poll.days} : null;
  const answer = soAnswering(c) ? {work: c.answerWork?.id || "", assets: (c.assets || []).map(x => x.id)} : null;
  c.error = c.pictures.some(p => p.pending) ? "图片正在上传，请稍候" : poll && soPollError(poll, text) || (wd && !title ? "请填写标题" : wd && !c.pictures.length ? "请选择作品图片" : c.ask && !c.pictures.length ? "求同款需要附上至少一张图片"
    : !wd && !text && (!c.pictures.length || c.quote_of) && !(answer && (answer.work || answer.assets.length)) ? "请填写内容或添加图片" : Array.from(text).length > 1000 ? "内容最多 1000 个字" : text.split("\n").length > 30 ? "内容最多 30 行" : "");
  if (c.error) { soComposeError(key); return; }
  c.busy = true; soComposeDraw(key);
  // with a work card (发布到社区 of a one-click creation) the post is a work: its text the work's description
  const flagged = c.pictures.some(p => p.flagged);
  const r = wd ? await soServiceAPI("work/post", {text, pictures: c.pictures.map(p => p.id), work: {title, base: wd.base || "", assets: wd.assets || [], recipe: wd.recipe || ""}, auto_flag: flagged})
    : await soServiceAPI("post/publish", {text, pictures: c.pictures.map(p => p.id), reply_to: c.reply_to || "", quote_of: c.quote_of || "", work: answer?.work || (c.ask ? "" : c.work || ""), assets: answer?.assets || [], sensitive: !!c.sensitive && c.pictures.length > 0, group: key === "group" ? SO.group?.id || "" : "", poll, ask: !!c.ask, auto_flag: flagged && !c.sensitive});
  c.busy = false;
  if (!r.ok) { c.error = r.err; soComposeDraw(key); return; }
  if (wd) { SO.lists = null; SO.me = null; }
  soComposeRelease(c);
  SO.drafts[key] = soNewDraft({reply_to: c.reply_to, target: c.target});
  if (key !== "dialog") soComposeDraw(key);
  if (key === "dialog") { SO.compose = null; closeModal(); }
  toast("已发布");
  if (c.reply_to && SO.post?.id === c.reply_to) { await soThreadLoad(false); if (key === "dialog") { soPostDraw(); showModal(); } }
  if (key === "group") await soGroupPosts(false);
  if (c.ask && SO.asks) await soAsksLoad(false);
  if (SO.timeline) await soTimelineLoad(false, true);
  soComposeDraw(key === "dialog" ? "home" : key);
}
// ---------- 发布到社区, 一键克隆 and 换素体克隆 ----------
// The composer's work card: a one-click creation's title (editable), base, assets and 装配方案, posted as a work.
function soWorkDraftHTML(d) {
  return `<section class="sxwork sxworkdraft"><input id="sc_dialog_wtitle" value="${esc(d.title || "")}" maxlength="60" placeholder="作品标题" aria-label="作品标题" autocomplete="off">
    <p>${d.base ? `<span data-i18n="off">${esc(d.base)}</span> · ` : ""}<span>${esc(`${(d.assets || []).length} 项素材`)}</span>${d.recipe ? ` · <span>附方案</span>` : ""}</p>
    <button class="wb sxworkdrop" data-so="xworkdrop" title="移除" aria-label="移除">${ICON.x}</button></section>`;
}
document.addEventListener("input", e => {
  if (e.target.id !== "sc_dialog_wtitle") return;
  const d = soDraft("dialog").workDraft; if (d) d.title = e.target.value;
});
// 「发布到社区」 of a finished one-click job (web/oneclick.js, HOOKS_community_c12.md): the composer opens with the
// job's pictures (its front picture first) and its work card (/api/oneclick/share, internal/ai/oneclick_share.go).
async function socialPublishOneClick(job) {
  if (!job || job.state !== "done") return;
  await soServiceLoad(false);
  if (!SO.service?.account) { soAccountDialog(() => socialPublishOneClick(job)); return; }
  let r; try { r = await api("/api/oneclick/share", { id: job.id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!r.ok) { toast(r.err || "操作失败", 5000); return; }
  const s = r.share || {};
  $("#modal").classList.remove("ocwin");
  SO.compose = SO.drafts.dialog = soNewDraft({ workDraft: { title: s.title || "", base: s.base || "", assets: s.assets || [], recipe: s.recipe || "" } });
  soComposeDraw(); showModal();
  // the result's pictures go in as pictures picked by hand do (shown at once, uploaded one by one, removable)
  const blobs = (s.pictures || []).slice(0, 4).map(b64 => new Blob([Uint8Array.from(atob(b64), ch => ch.charCodeAt(0))], { type: "image/jpeg" }));
  await soComposeAdd(blobs, "dialog");
}
window.socialPublishOneClick = socialPublishOneClick;

// the work dialog's work as a work card (the shape timeline.go gives)
function soCloneWorkOf(r) {
  if (!r?.ok || !r.work) return null;
  const items = (r.assets || []).map(a => ({ name: a.name, url: a.url, boothId: a.boothId, owned: !!a.owned, key: a.key || "", category: a.category || "" }));
  return { id: r.work.id, recipe: r.work.recipe || "", base: r.work.base || "", items };
}
// the basket group of a library card: by its category, else by the recipe's kind of it
const SO_CLONE_KINDS = { 皮肤: "skins", 表情: "expressions", 互动: "plugins", 面捕: "plugins", 光影: "plugins" };
function soCloneGroup(a, kind) { return (typeof ocGroupOf === "function" && ocGroupOf(a.category)) || (typeof ocGroupOf === "function" && ocGroupOf(kind)) || SO_CLONE_KINDS[kind] || ""; }
function soBoothOf(raw) {
  try {
    const u = new URL(raw), m = /^\/(?:[a-z]{2}\/)?items\/([1-9][0-9]*)\/?$/.exec(u.pathname);
    return u.protocol === "https:" && !u.username && !u.password && !u.port && (u.hostname === "booth.pm" || u.hostname.endsWith(".booth.pm")) && m ? m[1] : "";
  } catch (_) { return ""; }
}
// A clone is previewed before the basket or clone count changes. Recipe-local keys and names are not proof
// of ownership: both the work and its recipe are matched by Booth ID against this library.
async function soClone(w, base) {
  if (!w || SO.cloning || typeof OC === "undefined" || typeof ocChanged !== "function") return;
  SO.cloning = true;
  try {
    const candidates = (w.items || []).map(x => ({ ...x })), notes = [];
    let named = w.base || "";
    if (w.recipe) {
      let r; try { r = await api("/api/recipe/import", { text: w.recipe, save: false }); } catch (e) { r = { ok: false }; }
      if (r.ok) {
        named = r.recipe?.base?.name || named;
        for (const a of r.recipe?.assets || []) candidates.push({ name: a.name, url: a.url || "", boothId: a.boothId || soBoothOf(a.url), kind: a.kind });
      } else notes.push("方案无法读取，仅使用作品列出的素材");
    }
    const rows = [], items = [], seen = new Map(), library = S.data?.assets || [];
    let b = base ? findAsset(base) : null;
    for (const x of candidates) {
      const id = /^[1-9][0-9]*$/.test(x.boothId || "") ? x.boothId : soBoothOf(x.url);
      const key = id || `${x.name || ""}\n${x.url || ""}`;
      if (seen.has(key)) { if (!seen.get(key).kind) seen.get(key).kind = x.kind; continue; }
      const row = { name: x.name, boothId: id, url: id ? (soBoothOf(x.url) === id ? x.url : `https://booth.pm/items/${id}`) : "", kind: x.kind };
      seen.set(key, row); rows.push(row);
    }
    for (const row of rows) {
      if (!row.boothId) { row.note = "无 Booth 编号，无法匹配"; continue; }
      const matches = library.filter(a => a.boothId === row.boothId);
      const rank = a => (!a.virtual && !a.panOnly ? 2 : 0) + (!a.hidden ? 1 : 0);
      const a = matches.sort((a, b) => rank(b) - rank(a))[0];
      row.owned = !!a;
      if (!a) continue;
      const group = soCloneGroup(a, row.kind), g = group && ocGroup(group);
      if (!g) { row.note = "无法加入改模篮"; continue; }
      if (group === "base") { if (!b) b = a; continue; }
      if (items.filter(i => i.group === group).length >= g[2]) { row.note = "超出改模篮数量上限"; continue; }
      items.push({ key: a.key, name: a.name, group, face: a.category === "面捕" });
    }
    if (b) items.unshift({ key: b.key, name: b.name, group: "base", face: false });
    if (!b) rows.push({ name: named || "素体", note: "未匹配到作品素体，无法还原原作" });
    else if (base) rows.push({ name: b.name, owned: true, note: "使用所选素体，外观与适配可能不同" });
    notes.push("方案不包含材质参数、BlendShape 数值等设置，无法完整还原外观");
    notes.push("这里只加入素材，方案中的菜单和开关设置需另行应用");
    SO.clonePlan = { w, items, missing: rows.filter(x => x.boothId && !x.owned) };
    const m = $("#modal"); m.dataset.kind = "soclonepreview";
    m.innerHTML = `<div class="mhead">做同款前确认</div><div class="mbody"><ul class="socclonepreview">${rows.map(x => {
      const wished = x.boothId && typeof wishHas === "function" && wishHas(x.boothId);
      return `<li><span class="nm" data-i18n="off">${esc(x.name)}</span><span class="tag ${x.owned ? "ok" : "faint"}">${x.owned ? "素材库中已有" : x.boothId ? "缺少" : "无法还原"}</span>${x.note ? `<span class="muted small">${esc(x.note)}</span>` : ""}${x.boothId && !x.owned ? `<button class="btn small ghost" data-so="link" data-kind="booth_item" data-href="${esc(x.url)}">打开商品页</button><button class="btn small${wished ? " on" : ""}" data-so="wish" data-id="${esc(x.boothId)}" data-name="${esc(x.name)}"${wished ? " disabled" : ""}>${wished ? "已在愿望单" : "加入愿望单"}</button>` : ""}</li>`;
    }).join("")}${notes.map(n => `<li class="muted small">${esc(n)}</li>`).join("")}</ul></div><div class="mfoot"><button class="btn" data-m="close">取消</button><button class="btn primary" data-so="cloneapply"${items.length ? "" : " disabled"}>用已有素材继续</button></div>`;
    showModal();
  } finally { SO.cloning = false; }
}
function soCloneApply() {
  const plan = SO.clonePlan;
  if (!plan || $("#modal").dataset.kind !== "soclonepreview") return;
  SO.clonePlan = null;
  const { w, items, missing } = plan, was = OC.items;
  closeModal(); if (S.view !== "lib") setView("lib");
  ocRedraw(was.map(i => i.key).concat(items.map(i => i.key)));
  OC.items = items; OC.open = true; ocChanged();
  const undo = () => { ocRedraw(OC.items.map(i => i.key).concat(was.map(i => i.key))); OC.items = was; ocChanged(); };
  if (SO.service?.account && w.id) soServiceAPI("work/clone", { id: w.id }).then(r => { if (r.ok) soCloneCount(w.id, r.clones); });
  if (missing.length) soCloneMissing(missing, undo);
  else toast("已放入改模篮，点此撤销", 5000, undo);
}
function soCloneMissing(missing, undo) {
  const m = $("#modal"); m.dataset.kind = "soclone"; SO.cloneUndo = undo;
  m.innerHTML = `<div class="mhead">已放入改模篮</div><div class="mbody"><p>${esc(`缺少 ${missing.length} 件`)}</p><ul class="socmissing">${missing.map(x => `<li><span data-i18n="off">${esc(x.name)}</span>${x.url ? `<button class="btn small ghost" data-so="link" data-kind="booth_item" data-href="${esc(x.url)}" title="${esc(x.url)}">打开 Booth</button>` : ""}</li>`).join("")}</ul></div>
    <div class="mfoot"><button class="btn ghost" data-so="cloneundo">撤销</button><button class="btn primary" data-m="close">关闭</button></div>`;
  showModal();
}
// 换素体克隆: one of the library's bases first
function soCloneBase(w) {
  if (!w) return;
  const bases = ((S.data && S.data.assets) || []).filter(a => a.category === "素体" && !a.hidden);
  if (!bases.length) { toast("素材库中还没有素体", 4000); return; }
  SO.cloneWork = w;
  const m = $("#modal"); m.dataset.kind = "soclonebase";
  m.innerHTML = `<div class="mhead">选择素体</div><div class="mbody"><div class="soclonebases">${bases.map(a => `<button class="navitem" data-so="clonepick" data-key="${esc(a.key)}">${a.cover ? `<img src="${esc(a.cover)}" alt="" loading="lazy">` : `<span class="octh" data-i18n="off">${esc(Array.from(a.name || "?")[0])}</span>`}<span class="nm" data-i18n="off">${esc(a.name)}</span></button>`).join("")}</div></div>
    <div class="mfoot"><button class="btn" data-m="close">取消</button></div>`;
  showModal();
}
// the count after a clone, wherever the work's card is kept
function soCloneCount(id, n) {
  for (const p of soPostCache.values()) if (p.workCard && p.workCard.id === id) p.workCard.clones = n;
  if (SO.work?.data?.work?.id === id) SO.work.data.work.clones = n;
}

let soMentionTimer;
async function soMentionSearch(key, text, pos) {
  const c = soDraft(key), match = text.slice(0, pos).match(/(?:^|\s)@([A-Za-z0-9_]*)$/);
  const box = $(`#sc_${key}_people`);
  if (!box) return;
  box.replaceChildren();
  if (!match) return;
  const r = await soServiceAPI("search", {type: "people", q: match[1]});
  const area = $(`#sc_${key}_text`);
  if (!r.ok || !area || c.text !== text || area.selectionStart !== pos) return;
  c.mention = {start: pos - match[1].length - 1, end: pos};
  put(box, (r.accounts || []).slice(0, 6).map(a => `<button role="option" data-so="xcomplete" data-handle="${esc(a.handle)}">${soAv(a.pic, a.nickname, "xs")}<b data-i18n="off">${esc(a.nickname)}</b><span class="sxhandle" data-i18n="off">@${esc(a.handle)}</span></button>`).join(""));
}

async function soXAction(b, e) {
  e.stopPropagation();
  const id = b.dataset.id, p = soPostCache.get(id), key = b.closest("[data-composer]")?.dataset.composer || "dialog", c = soDraft(key);
  switch (b.dataset.so) {
    case "xtab": SO.timelineTab = b.dataset.tab; return soTimelineLoad(false, true);
    case "xfilter": SO.timelineFilter = b.dataset.filter || ""; try { localStorage.setItem("so-for-you", SO.timelineFilter); } catch (x) {} $(".main").scrollTop = 0; return soTimelineLoad(false, true);
    case "xretry": case "xnew": return soTimelineLoad(false, true);
    case "xtranslate": return soTranslate(id);
    case "xtranslatehide": SO.trans.delete(id); return soTranslateDraw(id);
    case "xmore": return soTimelineLoad(true);
    case "xthreadmore": return soThreadLoad(true);
    case "xthreadretry": return soThreadLoad(false);
    case "xsearchmore": return soSearchLoad();
    case "xreply": return soComposeOpen("reply", p);
    case "xquote": { const q = $("#so_pop")?._data; soPopClose(); return soComposeOpen("quote", q); }
    case "xrepostmenu": return soPop(b, [["xrepost", p.reposted ? "撤销转帖" : "转帖"], ["xquote", "引用"]], p);
    case "xmenu": return soPop(b, [...(soCanPin(p, b) ? [SO.group.data.pinned?.id === p.id ? ["xunpin", "取消置顶"] : ["xpin", "置顶"]] : []), ...(p.mine ? [["xdelete", "删除"]] : [["xreport", "举报"], ...(p.author?.id?.startsWith("u") ? [["xblock", `拉黑 @${p.author.handle}`]] : [])]), ["xcopy", "复制链接文字"], ...soStaffPostMenu(p)], p);
    case "xstaffhide": case "xstaffsens": case "xstaffunsens": case "xstaffdelete": case "xstafffeature": case "xstaffunfeature": {
      const q = $("#so_pop")?._data; soPopClose();
      if (q) return soStaffPost(q, {xstaffhide: "hide", xstaffsens: "sensitive", xstaffunsens: "unsensitive", xstaffdelete: "delete", xstafffeature: "feature", xstaffunfeature: "unfeature"}[b.dataset.so]);
      return;
    }
    case "xpin": case "xunpin": { const q = $("#so_pop")?._data; soPopClose(); if (q) return soGroupPin(q, b.dataset.so === "xpin"); return; }
    case "xvote": return soVote(p, +b.dataset.i);
    case "xpoll": if (!c.busy && !c.pictures.length && !c.poll) { c.poll = {options: ["", ""], days: 1}; soComposeDraw(key); $(`[data-composer="${key}"] .sxpollinput`)?.focus(); } return;
    case "xpolladd": if (c.poll && c.poll.options.length < 4) { c.poll.options.push(""); soComposeDraw(key); const all = document.querySelectorAll(`[data-composer="${key}"] .sxpollinput`); all[all.length - 1]?.focus(); } return;
    case "xpolldel": if (c.poll && c.poll.options.length > 2) { c.poll.options.splice(+b.dataset.i, 1); soComposeDraw(key); } return;
    case "xpolldrop": c.poll = null; soComposeDraw(key); return;
    case "xthemejoin": return soComposeOpen("", null, {text: "#" + b.dataset.tag + " "});
    case "xthemeoff": try { localStorage.setItem("so-theme-off:" + b.dataset.id, "1"); } catch (x) {} return soDraw();
    case "xcopy": { const q = $("#so_pop")?._data; soPopClose(); if (q) copyText(soPostShare(q), "已复制"); return; }
    case "xshare": b.classList.remove("pop"); void b.offsetWidth; b.classList.add("pop"); return p.share ? copyText(p.share, "链接已复制") : copyText(soPostShare(p), "已复制");
    case "xdelete": { const q = $("#so_pop")?._data; soPopClose(); if (!q || !confirm("删除帖子？")) return; const r = await soServiceAPI("post/delete", {id: q.id}); if (!r.ok) return soServiceError(r); if (SO.post?.id === q.id) { closeModal(); SO.post = null; } if (SO.nav === "group" && SO.group) await soGroupLoad(); await soTimelineLoad(false, true); return; }
    case "xreport": { const q = $("#so_pop")?._data; soPopClose(); if (q) soReport("post", q.id, q.text?.slice(0, 20)); return; }
    case "xblock": { const q = $("#so_pop")?._data; soPopClose(); if (q) return soBlock(q.author.id, true); return; }
    case "xlike": case "xbookmark": return soPostToggle(p, b.dataset.so.slice(1));
    case "xrepost": { const q = $("#so_pop")?._data; soPopClose(); return soPostToggle(q, "repost"); }
    case "xopenshared": e.stopPropagation(); return soPostOpen(b.dataset.id); // (a shared post's address: the post itself, here)
    case "xmention": { const r = id ? {ok: true, profile: {id}} : await soServiceAPI("handle/profile", {handle: b.dataset.handle}); if (!r.ok) return soServiceError(r); closeModal(); return soProfile({id: r.profile.id}); }
    case "xpicture": return soPictureView(p, +b.dataset.i);
    case "xreveal": return soReveal(id);
    case "xsensitive": {
      c.sensitive = !c.sensitive; c.autoTick = false; b.classList.toggle("on", c.sensitive); b.setAttribute("aria-pressed", String(c.sensitive));
      const note = $(`#sc_${key}_note`); if (note) note.textContent = soComposeNote(c);
      return;
    }
    case "xprev": return soPictureView(SO.viewer.post, SO.viewer.index - 1);
    case "xnext": return soPictureView(SO.viewer.post, SO.viewer.index + 1);
    case "xviewerback": if (SO.viewer.back === "sopost") soPostDraw(); else closeModal(); return;
    case "xcredits": return copyText(soCredits(p.workCard), "使用素材已复制");
    case "xrecipe": return soRecipeFromCode(p.workCard.recipe);
    case "xalong": await soWorkOpen(p.workCard.id); return soFollowAlong();
    case "xclone": return soClone(p.workCard);
    case "xclonebase": return soCloneBase(p.workCard);
    case "xworkdrop": if (!c.busy) { c.workDraft = null; c.flagged = false; soComposeFlag(c); soComposeDraw(key); } return;
    case "xpick": { const files = await soPickImage(true); return soComposeAdd(files, key); }
    case "xpicdel": {
      if (c.busy) return;
      const [gone] = c.pictures.splice(+b.dataset.i, 1);
      if (gone?.url?.startsWith("blob:")) URL.revokeObjectURL(gone.url);
      if (c.error === "最多 4 张图片") c.error = "";
      soComposeFlag(c); soComposeDraw(key); return;
    }
    case "xpicmove": { if (c.busy) return; const from = +b.dataset.i, to = from + +b.dataset.dir; if (to >= 0 && to < c.pictures.length) { [c.pictures[from], c.pictures[to]] = [c.pictures[to], c.pictures[from]]; soComposeDraw(key); } return; }
    case "xpublish": return soComposeSend(key);
    case "xcomplete": { const m = c.mention; if (!m) return; c.text = c.text.slice(0, m.start) + "@" + b.dataset.handle + " " + c.text.slice(m.end); soComposeDraw(key); const a = $(`#sc_${key}_text`); a.focus(); a.setSelectionRange(m.start + b.dataset.handle.length + 2, m.start + b.dataset.handle.length + 2); return; }
    case "xhandlesave": return soHandleSave(b);
    case "xhandlelater": return soHandleClose();
  }
}
// every list of posts the views keep (one post may be in several)
function soPostLists() {
  return [SO.timeline?.items?.map(it => it.post), SO.posts?.items, SO.asks?.items, SO.page?.posts?.items, SO.post?.data?.ancestors, SO.post?.data?.replies, [SO.post?.data?.post], SO.group?.posts?.items, [SO.group?.data?.pinned], [...soPostCache.values()]];
}
function soPostTarget(p) { return p?.author?.id || (p?.author?.github ? "gh:" + p.author.github : ""); }
// 「分享」 copies the post's web page on the service (p.share); 「复制链接文字」 copies it as plain text
function soPostShare(p) { return `${p.author?.nickname || ""} (@${p.author?.handle || ""})\n${p.text || ""}\n— MioVRCA`; }
async function soPostToggle(p, kind) {
  if (!p || SO.busy[kind + p.id]) return;
  if (!SO.service?.account) { soAccountDialog(() => soPostToggle(p, kind)); return; }
  const flag = {like: "liked", repost: "reposted", bookmark: "bookmarked"}[kind], count = {like: "likes", repost: "reposts", bookmark: "bookmarks"}[kind], on = !p[flag];
  const buttons = () => document.querySelectorAll(`[data-so="x${kind === "repost" ? "repostmenu" : kind}"][data-id="${p.id}"]`);
  // shown at once, as X does (the heart fills and bursts, the count rolls), and put back if the service refuses
  const show = (state, n, animate) => {
    p[flag] = state; p[count] = n;
    const update = x => { if (!x) return; if (x.id === p.id) { x[flag] = state; x[count] = n; } if (x.quote) update(x.quote); };
    for (const list of soPostLists()) for (const x of list || []) update(x);
    for (const el of buttons()) {
      el.setAttribute("aria-pressed", String(state));
      const num = $(".num", el); if (num) soCountRoll(num, n, state);
      el.classList.remove("pop"); if (animate && state) { void el.offsetWidth; el.classList.add("pop"); }
    }
  };
  const before = p[count] || 0;
  SO.busy[kind + p.id] = true;
  show(on, Math.max(0, before + (on ? 1 : -1)), true);
  const r = await soServiceAPI("post/" + kind, {id: p.id, on});
  SO.busy[kind + p.id] = false;
  if (!r.ok) { show(!on, before, false); return soServiceError(r); }
  if (kind === "bookmark") toast(on ? "已加入书签" : "已移出书签");
  if (kind === "bookmark" && !on && SO.nav === "bookmarks" && SO.posts) { SO.posts.items = SO.posts.items.filter(x => x.id !== p.id); soDraw(); }
}
function soPictureView(p, i) {
  const back = $("#modal").dataset.kind === "sxviewer" ? SO.viewer.back : $("#modal").classList.contains("on") ? $("#modal").dataset.kind : "";
  i = Math.max(0, Math.min(p.pics.length - 1, i)); SO.viewer = {post: p, index: i, back};
  const m = $("#modal"); m.dataset.kind = "sxviewer";
  m.innerHTML = `<div class="mhead"><span data-i18n="off">${i + 1}/${p.pics.length}</span></div><div class="mbody sxviewer"><button class="btn" data-so="xprev" aria-label="上一张"${i === 0 ? " disabled" : ""}>‹</button><img src="${esc(p.pics[i])}" alt=""><button class="btn" data-so="xnext" aria-label="下一张"${i === p.pics.length - 1 ? " disabled" : ""}>›</button></div><div class="mfoot"><button class="btn" data-so="xviewerback">返回</button></div>`; showModal();
}

let soHandleTimer;
async function soHandleOpen(next, first = false) {
  const a = SO.service?.account;
  if (!a) return soAccountDialog(() => soHandleOpen(next, first));
  const done = localStorage.getItem("so-handle:" + a.id);
  if (first && done) { if (next) await next(); return; }
  SO.handleNext = next;
  SO.handleReturn = $("#modal.on")?.dataset.kind || "";
  const m = $("#modal"); m.dataset.kind = "sxhandle";
  m.innerHTML = `<div class="mhead">选择用户名</div><div class="mbody soform"><label for="sx_handle">用户名</label><input id="sx_handle" maxlength="15" value="${esc(a.handle || "")}" placeholder="4–15 位字母、数字或下划线" autocomplete="off"><p id="sx_handle_status" role="status"></p></div><div class="mfoot"><button class="btn" data-so="xhandlelater">稍后</button><button class="btn primary" data-so="xhandlesave" disabled>保存</button></div>`;
  showModal(); await soHandleCheck();
}
async function soHandleCheck() {
  const input = $("#sx_handle"); if (!input) return;
  const value = input.value, button = $('[data-so="xhandlesave"]'); button.disabled = true;
  const r = /^[A-Za-z0-9_]{4,15}$/.test(value) ? await soServiceAPI("handle/check", {handle: value}) : {ok: true, available: false};
  if (!input.isConnected || input.value !== value) return;
  const own = value.toLowerCase() === SO.service?.account?.handle?.toLowerCase();
  $("#sx_handle_status").textContent = !r.ok ? r.err : own || r.available ? "用户名可用" : "用户名不可用";
  button.disabled = !r.ok || !own && !r.available;
}
async function soHandleSave(button) {
  button.disabled = true;
  const r = await soServiceAPI("handle/change", {handle: $("#sx_handle").value});
  if (!r.ok) { $("#sx_handle_status").textContent = r.err; button.disabled = false; return; }
  SO.service.account = r.account; if (SO.me) SO.me.account = r.account;
  localStorage.setItem("so-handle:" + r.account.id, "1");
  soDraw(); await soHandleClose();
}
async function soHandleClose() {
  const next = SO.handleNext, back = SO.handleReturn; SO.handleNext = null; SO.handleReturn = "";
  if (back === "sowork" && SO.work?.data) { soWorkDraw(); showModal(); }
  else if (back === "sopost" && SO.post?.data) { soPostDraw(); showModal(); }
  else closeModal();
  if (next) await next();
}

document.addEventListener("input", e => {
  if (e.target.id === "so_handle") { clearTimeout(soSignHandleTimer); soSignHandleTimer = setTimeout(soSignHandleCheck, 300); return; }
  if (e.target.id === "sx_handle") { clearTimeout(soHandleTimer); $('[data-so="xhandlesave"]').disabled = true; soHandleTimer = setTimeout(soHandleCheck, 250); return; }
  const key = e.target.closest("[data-composer]")?.dataset.composer;
  if (!key || e.target.tagName !== "TEXTAREA") return;
  const c = soDraft(key); c.text = e.target.value; soComposeGrow(e.target);
  const n = Array.from(c.text).length, el = $(`#sc_${key}_count`); el.textContent = `${n}/1000`; el.classList.toggle("warnc", n > 1000);
  $('[data-so="xpublish"]', e.target.closest("[data-composer]")).disabled = c.busy || n > 1000 || c.pictures.some(p => p.pending);
  clearTimeout(soMentionTimer); soMentionTimer = setTimeout(() => soMentionSearch(key, c.text, e.target.selectionStart), 250);
});
document.addEventListener("change", e => { if (e.target.id === "sc_dialog_work") soDraft("dialog").work = e.target.value; });
document.addEventListener("paste", e => {
  const key = e.target.closest("[data-composer]")?.dataset.composer;
  if (key && e.clipboardData?.files?.length) { e.preventDefault(); soComposeAdd(e.clipboardData.files, key); }
});
document.addEventListener("dragstart", e => {
  const pic = e.target.closest("[data-composer] .sopic"), form = pic?.closest("[data-composer]");
  if (form) { soDraft(form.dataset.composer).drag = +pic.dataset.i; e.dataTransfer?.setData("text/plain", pic.dataset.i); }
});
document.addEventListener("dragover", e => { if (e.target.closest("[data-composer]")) e.preventDefault(); });
document.addEventListener("drop", e => {
  const form = e.target.closest("[data-composer]"); if (!form) return;
  e.preventDefault(); const key = form.dataset.composer, c = soDraft(key);
  if (c.busy) return;
  if (c.drag >= 0) { const at = e.target.closest(".sopic"), to = at ? +at.dataset.i : c.pictures.length - 1; const pic = c.pictures.splice(c.drag, 1)[0]; c.drag = -1; if (pic) c.pictures.splice(to, 0, pic); soComposeDraw(key); }
  else if (e.dataTransfer?.files?.length) soComposeAdd(e.dataTransfer.files, key);
});
document.addEventListener("dragend", () => { for (const c of Object.values(SO.drafts)) c.drag = -1; });
document.addEventListener("keydown", e => {
  if ($("#modal.on")?.dataset.kind !== "sxviewer" || !["ArrowLeft", "ArrowRight"].includes(e.key)) return;
  e.preventDefault(); soPictureView(SO.viewer.post, SO.viewer.index + (e.key === "ArrowLeft" ? -1 : 1));
});

const soViewPending = new Set(), soViewSent = new Map(), soViewTimers = new Map();
let soViewObserver, soViewBusy = false;
function soViewQueue(id) { if (Date.now() - (soViewSent.get(id) || 0) >= 86400000 && soViewPending.size < 1000) soViewPending.add(id); }
function soViewsWatch() {
  soViewObserver?.disconnect(); for (const timer of soViewTimers.values()) clearTimeout(timer); soViewTimers.clear();
  if (typeof IntersectionObserver === "undefined" || document.hidden || S.view !== "social") return;
  soViewObserver = new IntersectionObserver(entries => {
    for (const e of entries) {
      clearTimeout(soViewTimers.get(e.target)); soViewTimers.delete(e.target);
      if (e.isIntersecting && e.intersectionRatio >= .5) soViewTimers.set(e.target, setTimeout(() => {
        if (e.target.isConnected && !document.hidden && S.view === "social") soViewQueue(e.target.dataset.viewPost);
        soViewTimers.delete(e.target);
      }, 1000));
    }
  }, {threshold: [.5]});
  const modal = $("#modal.on"), root = modal || $("#grid");
  for (const el of root?.querySelectorAll("[data-view-post]") || []) soViewObserver.observe(el);
}
async function soViewsFlush() {
  if (soViewBusy || !soViewPending.size) return;
  soViewBusy = true; const ids = [...soViewPending].slice(0, 100);
  try { const r = await soServiceAPI("views", {posts: ids}); if (r.ok) for (const id of ids) { soViewPending.delete(id); soViewSent.set(id, Date.now()); } }
  finally { soViewBusy = false; }
}
setInterval(soViewsFlush, 5000);
document.addEventListener("visibilitychange", soViewsWatch);
if (typeof MutationObserver !== "undefined" && $("#modal")) new MutationObserver(soViewsWatch).observe($("#modal"), {attributes: true, attributeFilter: ["class"]});

// Profile, discovery and private messages share the same account session.
// tabs, each [key, label] or [key, label, icon] (an icon of SO_ACT or SO_TOOL, before the label)
function soTabs(tabs, selected, action) {
  const icon = k => k ? `<span class="sxtabicon" aria-hidden="true">${soIconSVG(k)}</span>` : "";
  return `<div class="sxtabs${tabs.some(t => t[2]) ? " withicons" : ""}" role="tablist">${tabs.map(([key, label, i]) => `<button role="tab" aria-selected="${selected === key}" data-so="${action}" data-tab="${key}">${icon(i)}<span>${label}</span></button>`).join("")}</div>`;
}
function soPersonRow(a, action = "profile") {
  const github = a.id?.startsWith("gh:") ? a.id.slice(3) : a.github;
  return `<div class="sxperson">${soProfileBtn(github && a.id?.startsWith("gh:") ? "" : a.id, github, a.nickname, a.pic, false, a.booth)}${soBotMark(a)}<span class="sxhandle" data-i18n="off">@${esc(a.handle || github || "")}</span>${action === "unblock" ? `<button class="btn small" data-so="pageunblock" data-id="${esc(a.id)}">取消拉黑</button>` : ""}</div>`;
}
function soPostListHTML(p, media, more) {
  const items = p?.items || [];
  for (const x of items) soPostCache.set(x.id, x);
  const rows = media ? `<div class="sxmediagrid">${items.flatMap(x => (x.pics || []).map((src, i) => soSensWrap(x, `<button data-so="xpicture" data-id="${esc(x.id)}" data-i="${i}"><img src="${esc(src)}" alt="" loading="lazy"></button>`, "tile"))).join("")}</div>` : items.map(x => soPostCard(x)).join("");
  return `${p?.error ? `<p class="warnc">${esc(p.error)}</p><button class="btn" data-so="${more}">重试</button>` : ""}${items.length ? rows : soEmpty(p?.loaded ? "暂无内容" : "正在读取…")}${p?.next ? `<button class="btn sxmore" data-so="${more}"${p.busy ? " disabled" : ""}>加载更多</button>` : ""}`;
}
async function soOwnProfile(refresh) {
  await soServiceLoad(false);
  if (SO.nav !== "me" || !SO.service?.account) return;
  return soProfile({id: SO.service.account.id}, refresh);
}
async function soProfilePosts(more = false) {
  const page = SO.page;
  if (!page?.data?.ok || !(page.data.readOnly && page.tab === "works") && !["posts", "replies", "media", "likes"].includes(page.tab)) return;
  if (!more || !page.posts) page.posts = {items: [], next: "", loaded: false};
  const p = page.posts, tab = page.tab;
  if (p.busy || more && p.loaded && !p.next) return;
  p.busy = true;
  const r = await soServiceAPI("profile/posts", {id: page.data.id, filter: page.data.readOnly ? "posts" : tab, after: more ? p.next : ""});
  p.busy = false;
  if (SO.page !== page || page.posts !== p) return;
  if (r.ok) { const ids = new Set(p.items.map(x => x.id)); p.items.push(...(r.posts || []).filter(x => !ids.has(x.id))); p.next = r.next; p.loaded = true; p.error = ""; }
  else p.error = r.err;
  soDraw();
}
async function soProfileWorks(more = false) {
  const page = SO.page;
  if (!page?.data?.ok) return;
  const r = await soServiceAPI("works", {author: page.data.id, after: more ? page.workNext : ""});
  if (SO.page !== page) return;
  if (r.ok) {
    const old = more ? page.data.works : [], ids = new Set(old.map(w => w.id));
    page.data.works = [...old, ...(r.works || []).filter(w => !ids.has(w.id))]; page.workNext = r.next;
  }
  soDraw();
}
async function soWorksLoad(reset = false) {
  const key = JSON.stringify([SO.base, SO.sort, SO.q]);
  if (reset || !SO.worksPage) SO.worksPage = {key, works: [], next: "", ok: true};
  const p = SO.worksPage;
  if (p.busy || !reset && !p.next) return;
  p.busy = true; SO.busy.works = true;
  const r = await soServiceAPI("works", {base: SO.base, sort: SO.sort === "popular" ? "hot" : SO.sort, q: SO.q, after: reset ? "" : p.next});
  p.busy = false;
  if (p !== SO.worksPage) return;
  SO.busy.works = false;
  if (r.ok) { const ids = new Set(p.works.map(w => w.id)); p.works.push(...(r.works || []).filter(w => !ids.has(w.id))); p.next = r.next; }
  else { SO.worksPage = null; }
  soDraw();
}
function soSearchBox() {
  return `<form class="sxsearch" id="sx_search_form" role="search"><span class="sxsearchicon" aria-hidden="true">${ICON.search || ""}</span><input type="search" id="sx_search" value="${esc(SO.q)}" placeholder="搜索帖子、作品、创作者" aria-label="搜索帖子、作品、创作者" enterkeyhint="search"></form>`;
}
async function soTrendsLoad() {
  SO.trends = await soServiceAPI("trends"); soDraw();
}
function soExploreDraw() {
  soBar(null);
  const r = SO.trends;
  // (a topic the community's maintainer picked has a title of its own, and comes first while it is pinned)
  const trend = t => `<button class="sxtrend${t.pinned ? " pinned" : ""}" data-so="posttag" data-tag="${esc(t.tag)}">${t.pinned ? `<small class="sxtrendpin">${SO_TOOL.pin}<span>推荐</span></small>` : ""}<b data-i18n="off">${esc(t.title || "#" + t.tag)}</b><span>${t.title ? `<span data-i18n="off">#${esc(t.tag)}</span> · ` : ""}<span>${esc(`${t.posts} 篇帖子`)}</span></span></button>`;
  gridPut(`<div class="sxtimeline sxexplorepage">${soSearchBox()}<h2 class="sxpagetitle">趋势</h2>${r?.ok ? (r.trends || []).map(trend).join("") || soEmpty("暂无内容") : soEmpty(esc(r?.err || "正在读取…"))}${soDiscoveryHTML()}</div>`);
}
async function soSettingsLoad() {
  const session = SO.session;
  const [r, prefs, booth] = SO.service?.account ? await Promise.all([soServiceAPI("people", {kind: "blocks"}), soServiceAPI("prefs"), soServiceAPI("booth")]) : [null, null, null];
  if (session !== SO.session) return;
  SO.settings = r; SO.prefs = prefs?.ok ? prefs : SO.prefs; SO.booth = booth; soDraw();
  if (SO_WEB) soPushLoad();
}
// a section of 设置 with its icon before the name, as X's settings list has
function soSetHead(icon, text) { return `<h3 class="sxsethead"><span class="soi" data-i="${icon}" aria-hidden="true">${soIconSVG(icon)}</span><span>${text}</span></h3>`; }
function soSettingsDraw() {
  soBar(null);
  const a = SO.service?.account, blocks = SO.settings;
  if (!a) { gridPut(`<div class="sxtimeline sxsettings"><h2 class="sxpagetitle">设置</h2>${soAccountHTML()}</div>`); return; }
  gridPut(`<div class="sxtimeline sxsettings"><h2 class="sxpagetitle">设置</h2>${soAccountHTML()}
    <section class="sxset">${soSetHead("booth", "Booth 店铺认证")}<div class="soform" id="sx_booth">${soBoothHTML()}</div></section>
    <section class="sxset">${soSetHead("shield", "隐私")}
      ${soSetRow(`<label for="sx_requests">消息请求</label>`, "", `<select id="sx_requests"><option value="everyone"${a.allow_requests !== "none" ? " selected" : ""}>所有人</option><option value="none"${a.allow_requests === "none" ? " selected" : ""}>不接收</option></select>`)}
      ${soSwitchRow("sx_following", "公开我的关注列表", a.show_following)}
      ${soSwitchRow("sx_sensitive", "总是显示敏感内容", a.show_sensitive)}
      <p class="sxsetstatus" id="sx_setting_status" role="status"></p></section>
    ${soTransSetHTML()}
    ${SO_WEB ? `<section class="sxset">${soSetHead("bell", "通知")}${soPushHTML()}</section>` : `<section class="sxset">${soSetHead("bell", "通知")}
      ${soSwitchRow("sx_notify", "系统通知", SO.prefs?.notify !== false, "", !SO.prefs)}
      <p class="sxsetstatus" id="sx_notify_status" role="status"></p></section>`}
    <section class="sxset">${soSetHead("download", "数据")}
      ${soSetRow("导出我的数据", "", `<button class="btn small" data-so="accountexport">导出</button>`)}
      <p class="sxsetstatus" id="sx_export_status" role="status"></p></section>
    <section class="sxset">${soSetHead("block", "已拉黑的账号")}${blocks?.ok ? blocks.accounts.map(p => soPersonRow(p, "unblock")).join("") || `<p class="sxsetnote">暂无内容</p>` : `<p class="warnc">${esc(blocks?.err || "正在读取…")}</p>`}</section>
    <section class="sxset sxsetend"><button class="btn" data-so="accountlogout">退出登录</button><button class="btn sxdanger" data-so="accountdelete"${!SO.service.available ? " disabled" : ""}>注销账号</button></section>
  </div>`);
}
// 更换邮箱: a code sent to the new address, then the change (the old address stops signing in)
function soEmailOpen() {
  const a = SO.service?.account; if (!a) return soAccountDialog(soEmailOpen);
  const m = $("#modal"); m.dataset.kind = "sxemail";
  m.innerHTML = `<div class="mhead">更换邮箱</div><div class="mbody soform">
    <p class="muted small"><span>当前邮箱</span> <span data-i18n="off">${esc(a.email || "")}</span></p>
    <label for="sx_email_new">新邮箱</label><div class="somailcode"><input id="sx_email_new" type="email" autocomplete="email" maxlength="254"><button class="btn" data-so="emailcode">发送验证码</button></div>
    <label for="sx_email_code">验证码</label><input id="sx_email_code" inputmode="numeric" maxlength="6" autocomplete="one-time-code" placeholder="6 位数字">
    <p class="small" id="sx_email_status" role="status"></p></div>
    <div class="mfoot"><button class="btn" data-m="close">取消</button><button class="btn primary" data-so="emailsave">更换</button></div>`;
  showModal(); $("#sx_email_new").focus();
}
async function soEmailCode(btn) {
  const email = $("#sx_email_new").value.trim(), status = $("#sx_email_status");
  if (!email) { $("#sx_email_new").focus(); return; }
  btn.disabled = true; status.textContent = "正在发送…";
  const r = await soServiceAPI("email/code", {email});
  if (!r.ok) { btn.disabled = false; status.textContent = r.err; return; }
  status.textContent = "验证码已发送，10 分钟内有效。几分钟内没收到，请查看垃圾邮件";
  let n = 60; const tick = () => { if (!btn.isConnected) return; btn.textContent = n ? `重新发送（${n} 秒）` : "发送验证码"; btn.disabled = n > 0; if (n--) setTimeout(tick, 1000); }; tick();
  $("#sx_email_code").focus();
}
async function soEmailSave(btn) {
  const email = $("#sx_email_new").value.trim(), code = $("#sx_email_code").value.trim(), status = $("#sx_email_status");
  if (!email || !/^[0-9]{6}$/.test(code)) { status.textContent = "请填写新邮箱和 6 位验证码"; return; }
  btn.disabled = true;
  const r = await soServiceAPI("email/change", {email, code});
  btn.disabled = false;
  if (!r.ok) { status.textContent = r.err; return; }
  SO.service.account = r.account; if (SO.me) SO.me.account = r.account;
  closeModal(); toast("邮箱已更换"); soDraw();
}
// QQ / LINE / Discord: shown on the profile for others to reach the player (not verified)
function soContactsOpen() {
  const a = SO.service?.account; if (!a) return soAccountDialog(soContactsOpen);
  const c = a.contacts || {}, m = $("#modal"); m.dataset.kind = "sxcontacts";
  const row = (id, label, value, hint, mode) => `<label for="${id}">${label}</label><input id="${id}" value="${esc(value || "")}" placeholder="${esc(hint)}" autocomplete="off"${mode ? ` inputmode="${mode}"` : ""} data-i18n="off">`;
  m.innerHTML = `<div class="mhead">QQ / LINE / Discord</div><div class="mbody soform">
    ${row("sx_qq", "QQ 号", c.qq, "例如 123456789", "numeric")}${row("sx_line", "LINE ID", c.line, "例如 mio_vrc")}${row("sx_discord", "Discord 用户名", c.discord, "例如 mio.vrc")}
    <p class="small warnc" id="sx_contacts_status" role="status"></p></div>
    <div class="mfoot"><button class="btn" data-m="close">取消</button><button class="btn primary" data-so="contactssave">保存</button></div>`;
  showModal(); $("#sx_qq").focus();
}
async function soContactsSave(btn) {
  const contacts = {qq: $("#sx_qq").value.trim(), line: $("#sx_line").value.trim().replace(/^@/, ""), discord: $("#sx_discord").value.trim().replace(/^@/, "")};
  btn.disabled = true;
  const r = await soServiceAPI("profile/save", {contacts});
  btn.disabled = false;
  if (!r.ok) { $("#sx_contacts_status").textContent = r.err; return; }
  SO.service.account = r.account; if (SO.me) SO.me = null;
  closeModal(); toast("已保存"); soDraw();
}
async function soSettingSave() {
  const a = SO.service?.account, session = SO.session;
  if (!a) return;
  const request = $("#sx_requests"), following = $("#sx_following"), sensitive = $("#sx_sensitive"), status = $("#sx_setting_status");
  request.disabled = following.disabled = sensitive.disabled = true;
  const r = await soServiceAPI("settings", {allow_requests: request.value, show_following: following.checked, show_sensitive: sensitive.checked});
  if (session !== SO.session) return;
  if (r.ok) { SO.service.account = r.account; if (SO.me) SO.me.account = r.account; SO.home = null; status.textContent = "已保存"; }
  else { request.value = a.allow_requests || "everyone"; following.checked = !!a.show_following; sensitive.checked = !!a.show_sensitive; status.textContent = r.err; }
  request.disabled = following.disabled = sensitive.disabled = false;
}
// 「系统通知」 is this computer's (community.json), not the account's
async function soPrefsSave() {
  const box = $("#sx_notify"), status = $("#sx_notify_status");
  if (!box) return;
  box.disabled = true;
  const r = await soServiceAPI("prefs", {notify: box.checked});
  if (r.ok) { SO.prefs = r; status.textContent = "已保存"; }
  else { box.checked = SO.prefs?.notify !== false; status.textContent = r.err; }
  box.disabled = false;
}
async function soBlock(id, on) {
  const r = await soServiceAPI("block", {id, on});
  if (!r.ok) return soServiceError(r);
  const src = SO.page?.src;
  SO.follows[id] = false; SO.offer = null; SO.lists = null; SO.hfeed = null; SO.posts = null; SO.timeline = null;
  SO.notices = null; SO.dm = null; SO.post = null; soPostCache.clear(); closeModal();
  await soUnreadLoad();
  if (src) await soProfile(src, true);
  else soNeed(true);
  soDraw();
}
async function soPeopleOpen(kind) {
  const p = SO.page?.data;
  if (!p) return;
  const m = $("#modal"), session = SO.session;
  m.dataset.kind = "sxpeople";
  m.innerHTML = `<div class="mhead">${kind === "following" ? "正在关注" : "关注者"}</div><div class="mbody">正在读取…</div><div class="mfoot"><button class="btn" data-m="close">关闭</button></div>`; showModal();
  const r = await soServiceAPI("people", {id: p.id, kind});
  if (SO.session !== session || m.dataset.kind !== "sxpeople" || !m.classList.contains("on")) return;
  put($(".mbody", m), r.ok ? r.accounts.map(a => soPersonRow(a)).join("") || soEmpty("暂无内容") : `<p class="warnc">${esc(r.err)}</p>`);
}
function soDMState() {
  return SO.dm || (SO.dm = {box: "inbox", inbox: [], requests: [], loaded: false, chat: null});
}
async function soDMLoad() {
  if (!SO.service?.account) { soDraw(); return; }
  const d = soDMState();
  if (d.busy) return;
  d.busy = true; SO.busy.dm = true;
  const [inbox, requests] = await Promise.all([soServiceAPI("dm", {box: "inbox"}), soServiceAPI("dm", {box: "requests"})]);
  d.busy = false; SO.busy.dm = false;
  if (SO.dm !== d) return;
  if (inbox.ok && requests.ok) { d.inbox = inbox.conversations; d.requests = requests.conversations; d.loaded = true; d.error = ""; soDMAt = Date.now(); }
  else d.error = inbox.err || requests.err;
  if (d.chat) {
    const current = [...d.inbox, ...d.requests].find(c => c.account.id === d.chat.account.id);
    if (current) d.chat.account = current.account;
    d.chat.request = d.requests.some(c => c.account.id === d.chat.account.id);
  }
  soDraw();
}
async function soDMStart(account) {
  if (!SO.service?.account) return soAccountDialog(() => soDMStart(account));
  SO.nav = "messages"; SO.page = null; closeModal();
  const d = soDMState();
  const chat = d.chat = {account, items: [], next: "", loaded: false, request: false, draft: "", picture: null, busy: false, error: ""};
  soDraw(); await soDMLoad();
  if (SO.dm !== d || d.chat !== chat) return;
  return soDMRead();
}
async function soDMRead(more = false) {
  const d = SO.dm, c = d?.chat;
  if (!c || c.loading || more && !c.next) return;
  const log = $("#sx_dm_log"), top = log?.scrollTop || 0, height = log?.scrollHeight || 0;
  const bottom = !more && (!c.loaded || !log || height - top - log.clientHeight < 80);
  c.loading = true;
  const r = await soServiceAPI("dm/messages", {id: c.account.id, before: more ? c.next : ""});
  if (SO.dm !== d || d.chat !== c) return;
  if (r.ok) {
    const items = new Map(c.items.map(m => [m.id, m]));
    for (const m of r.messages || []) items.set(m.id, m);
    c.items = [...items.values()].sort((a,b) => a.id.length - b.id.length || a.id.localeCompare(b.id));
    if (more || !c.loaded) c.next = r.next;
    c.loaded = true; c.error = "";
    const pictures = new Set(c.items.filter(m => !m.deleted).map(m => m.picture));
    for (const id of Object.keys(c.pictures || {})) if (!pictures.has(id)) delete c.pictures[id];
    const read = await soServiceAPI("dm/read", {id: c.account.id});
    if (!read.ok) c.error = read.err;
    else for (const row of [...d.inbox, ...d.requests]) if (row.account.id === c.account.id) row.unread = 0;
    await soUnreadLoad();
  } else c.error = r.err;
  if (SO.dm !== d || d.chat !== c) return;
  c.loading = false;
  soDraw();
  await soDMImages(c);
  if (SO.dm !== d || d.chat !== c) return;
  const current = $("#sx_dm_log");
  if (current) current.scrollTop = bottom ? current.scrollHeight : more ? top + current.scrollHeight - height : top;
}
async function soDMImages(c) {
  for (const m of c.items) {
    if (!m.picture || m.deleted || c.pictures?.[m.picture]) continue;
    const r = await soServiceAPI("dm/picture", {id: m.picture});
    if (SO.dm?.chat !== c) return;
    if (r.ok) {
      (c.pictures || (c.pictures = {}))[m.picture] = r.src;
      const img = document.querySelector(`[data-dmpic="${m.picture}"]`);
      if (img) { img.src = r.src; await img.decode().catch(() => {}); img.hidden = false; }
    }
  }
  await Promise.allSettled([...document.querySelectorAll("#sx_dm_log img[src]")].map(img => img.decode()));
}
function soDMDraw() {
  const top = $("#sx_dm_log")?.scrollTop;
  soBar(null);
  if (!SO.service?.account) { gridPut(`<div class="sxtimeline">${soAccountHTML()}</div>`); return; }
  const d = soDMState(), c = d.chat, own = soAccountID();
  const list = d[d.box] || [];
  const requests = `<button class="sxdmrequests" data-so="dmbox" data-box="${d.box === "requests" ? "inbox" : "requests"}">${d.box === "requests" ? "私信" : "消息请求"} <span class="sxbadge">${d.requests.length}</span></button>`;
  const rows = list.map(r => `<button class="sxdmrow${c?.account.id === r.account.id ? " selected" : ""}" data-so="dmopen" data-id="${esc(r.account.id)}">${soAv(r.account.pic, r.account.nickname)}<span class="sxdmsummary"><b data-i18n="off">${esc(r.account.nickname)}</b><span class="sxhandle" data-i18n="off">@${esc(r.account.handle)}</span><span class="sxdmlast"${r.last.deleted ? "" : ' data-i18n="off"'}>${r.last.deleted ? "消息已删除" : esc(r.last.text || "")}${r.last.picture ? `<span>图片</span>` : ""}</span></span><time>${esc(soRelative(r.last.created))}</time>${r.unread ? `<i class="sxdmunread" aria-label="未读"></i>` : ""}</button>`).join("");
  let chat = soEmpty("选择一条私信");
  if (c) {
    let day = "";
    const bubbles = c.items.map(m => {
      const date = new Date(m.created).toLocaleDateString(), heading = date !== day ? `<div class="sxdmday" data-i18n="off">${esc(date)}</div>` : ""; day = date;
      const mine = m.from === own, recall = mine && !m.deleted && Date.now() - Date.parse(m.created) <= 300000;
      return `${heading}<article class="sxdmbubble${mine ? " mine" : ""}" data-id="${esc(m.id)}"><div class="sxdmtext"${m.deleted ? "" : ' data-i18n="off"'}>${m.deleted ? esc("消息已删除") : soPostText(m.text)}</div>${m.deleted ? "" : soSharedCard({text: m.text, id: ""})}${m.picture && !m.deleted ? `<img data-dmpic="${esc(m.picture)}"${c.pictures?.[m.picture] ? ` src="${esc(c.pictures[m.picture])}"` : " hidden"} alt="图片">` : ""}<div class="sxdmmeta"><time data-i18n="off">${esc(new Date(m.created).toLocaleTimeString([], {hour: "2-digit", minute: "2-digit"}))}</time>${recall ? `<button class="linkbtn" data-so="dmrecall" data-id="${esc(m.id)}">撤回</button>` : !mine && !m.deleted ? `<button class="linkbtn" data-so="dmreport" data-id="${esc(m.id)}">举报</button>` : ""}</div></article>`;
    }).join("");
    const request = c.request ? `<div class="sxdmrequestnote"><div class="btnrow"><button class="btn primary" data-so="dmaccept">接受</button><button class="btn" data-so="dmdelete">删除</button><button class="btn" data-so="dmblock">拉黑</button></div></div>` : "";
    chat = `<div class="sxdmhead">${soProfileBtn(c.account.id, c.account.github, c.account.nickname, c.account.pic, false, c.account.booth)}<span class="sxhandle" data-i18n="off">@${esc(c.account.handle)}</span><button class="btn small" data-so="dmdelete">删除</button></div><div class="sxdmlog" id="sx_dm_log">${c.next ? `<button class="btn" data-so="dmmore">加载更多</button>` : ""}${bubbles || soEmpty(c.loaded ? "暂无消息" : "正在读取…")}</div>${request}<div class="sxdmcompose"><p class="warnc" id="sx_dm_error" role="status">${esc(c.error || "")}</p>${c.picture ? `<div class="sxdmattachment"><img src="${esc(c.picture.src)}" alt="图片"><button class="btn small" data-so="dmpicclear">移除</button></div>` : ""}<div class="sxdmbox"><button class="sxtool" data-so="dmpick" title="图片" aria-label="图片"${c.busy || c.picture ? " disabled" : ""}>${SO_TOOL.picture}</button><button class="sxtool" data-so="emoji" data-for="sx_dm_text" title="插入表情" aria-label="插入表情"${c.busy ? " disabled" : ""}>${SO_TOOL.smile}</button><textarea id="sx_dm_text" rows="1" placeholder="发送私信（Enter 发送，Shift+Enter 换行）"${c.busy ? " disabled" : ""}>${esc(c.draft)}</textarea><span class="sxcnt" id="sx_dm_count">${Array.from(c.draft).length}/1000</span><button class="btn primary sxsend" data-so="dmsend"${c.busy || c.account.blocked ? " disabled" : ""}>发送</button></div></div>`;
  }
  gridPut(`<div class="sxdm"><section class="sxdmlist"><h2>${d.box === "requests" ? "消息请求" : "私信"}</h2>${requests}${d.error ? `<p class="warnc">${esc(d.error)}</p>` : ""}${rows || soEmpty(d.loaded ? "暂无消息" : "正在读取…")}</section><section class="sxdmchat">${chat}</section></div>`);
  if (top !== undefined && $("#sx_dm_log")) $("#sx_dm_log").scrollTop = top;
}
async function soDMSend() {
  const d = SO.dm, c = d?.chat;
  if (!c || c.busy) return;
  c.draft = $("#sx_dm_text")?.value ?? c.draft;
  if (Array.from(c.draft).length > 1000) { c.error = "私信最多 1000 字"; soDraw(); return; }
  if (!c.draft.trim() && !c.picture) return;
  c.busy = true; c.error = ""; soDraw();
  const r = await soServiceAPI("dm/send", {id: c.account.id, text: c.draft, picture: c.picture?.id || ""});
  c.busy = false;
  if (SO.dm !== d || d.chat !== c) return;
  if (!r.ok) { c.error = r.code === "dm_not_allowed" ? "对方暂不接收你的私信" : r.err; soDraw(); return; }
  if (c.picture && r.message?.picture === c.picture.id) (c.pictures || (c.pictures = {}))[c.picture.id] = c.picture.src;
  c.draft = ""; c.picture = null;
  await soDMLoad(); await soDMRead(); $("#sx_dm_text")?.focus();
}
async function soDMPick() {
  const c = SO.dm?.chat;
  if (!c || c.busy || c.picture) return;
  const files = await soPickImage(false);
  if (!files.length || SO.dm?.chat !== c) return;
  c.busy = true;
  try {
    const pic = await soPostPicture(files[0]), r = await soServiceAPI("picture", {kind: "message", b64: pic.b64});
    if (SO.dm?.chat !== c) return;
    if (r.ok) c.picture = {id: r.id, src: pic.url}; else c.error = r.err;
  } catch (e) { c.error = e.message; }
  finally { c.busy = false; soDraw(); }
}
async function soPageAction(b, e) {
  e.stopPropagation(); const what = b.dataset.so, d = SO.dm, c = d?.chat;
  switch (what) {
    case "pagetab": SO.page.tab = b.dataset.tab; soDraw(); return SO.page.tab === "works" && !SO.page.data.readOnly ? soProfileWorks() : soProfilePosts();
    case "pagepostmore": return soProfilePosts(true);
    case "pageworkmore": return soProfileWorks(true);
    case "pagepeople": return soPeopleOpen(b.dataset.kind);
    case "pagemenu": { const r = SO.page.data; return soPop(b, [["pagereport", "举报"], ...(!r.readOnly ? [[r.profile.blocked ? "pageunblock" : "pageblock", r.profile.blocked ? "取消拉黑" : "拉黑"]] : []), ["pagecopy", "复制链接文字"], ...soStaffProfileMenu(r)], r); }
    case "pagestaffban": { const r = $("#so_pop")?._data; soPopClose(); if (r) return soStaffBan(r.profile); return; }
    case "pagereport": { const r = $("#so_pop")?._data; soPopClose(); if (r) soReport("profile", r.target, r.nickname); return; }
    case "pagecopy": { const r = $("#so_pop")?._data; soPopClose(); if (r) copyText(`${r.nickname} (@${r.profile.handle || r.github}) — MioVRCA`, "已复制"); return; }
    case "pageblock": case "pageunblock": { const id = b.dataset.id || $("#so_pop")?._data?.id; soPopClose(); if (id) return soBlock(id, what === "pageblock"); return; }
    case "noticetab": SO.noticeTab = b.dataset.tab; SO.notices = null; soDraw(); return soNoticesLoad();
    case "searchtab": SO.searchType = b.dataset.tab; return soSearchLoad(true);
    case "worksnext": return soWorksLoad();
    case "dmstart": return soDMStart({...SO.page.data.profile, pic: SO.page.data.pic});
    case "dmbox": d.box = b.dataset.box; d.chat = null; soDraw(); return soDMLoad();
    case "dmopen": { const row = [...d.inbox, ...d.requests].find(r => r.account.id === b.dataset.id); if (row) return soDMStart(row.account); return; }
    case "dmmore": return soDMRead(true);
    case "dmsend": return soDMSend();
    case "dmpick": return soDMPick();
    case "dmpicclear": if (c && !c.busy) { c.picture = null; soDraw(); } return;
    case "dmreport": return soReport("message", b.dataset.id, "");
    case "dmblock": if (c) return soBlock(c.account.id, true); return;
    case "dmrecall": { const r = await soServiceAPI("dm/recall", {id: b.dataset.id}); if (!r.ok) { c.error = r.err; soDraw(); return; } await soDMLoad(); return soDMRead(); }
    case "dmaccept": case "dmdelete": {
      if (!c) return;
      const r = await soServiceAPI(what === "dmaccept" ? "dm/accept" : "dm/delete", {id: c.account.id});
      if (!r.ok) { c.error = r.err; soDraw(); return; }
      if (what === "dmdelete") d.chat = null; else { c.request = false; d.box = "inbox"; }
      await soDMLoad(); await soUnreadLoad(); return;
    }
  }
}
document.addEventListener("submit", e => {
  if (e.target.id !== "sx_search_form") return;
  e.preventDefault(); SO.q = $("#sx_search").value; SO.nav = "search"; SO.page = null;
  if ($("#q")) $("#q").value = SO.q;
  soSearchLoad(true);
});
document.addEventListener("input", e => {
  if (e.target.id !== "sx_dm_text" || !SO.dm?.chat) return;
  SO.dm.chat.draft = e.target.value;
  const count = Array.from(e.target.value).length;
  $("#sx_dm_count").textContent = `${count}/1000`; $("#sx_dm_count").classList.toggle("warnc", count > 1000);
});
document.addEventListener("change", e => {
  if (["sx_requests", "sx_following", "sx_sensitive"].includes(e.target.id)) soSettingSave();
  if (e.target.id === "sx_notify") soPrefsSave();
  if (e.target.id === "sx_push") soPushSwitch(e.target);
  if (e.target.id?.startsWith("sx_push_")) soPushKindsSave();
});
document.addEventListener("keydown", e => {
  if (e.target.id === "sx_dm_text" && e.key === "Enter" && !e.shiftKey && !e.isComposing) { e.preventDefault(); e.stopImmediatePropagation(); soDMSend(); }
}, true);
// The unread counts are read every half minute while an account is signed in (and not at all without one); the
// program's Windows notifications come from these readings (internal/social/notify.go). With the private messages
// in sight they are read every five seconds, and the conversations again only when a message came in or half a
// minute has passed.
let soPollTimer = 0, soPollDue = 0, soPollLast = 0, soDMAt = 0;
document.addEventListener("scroll", e => {
  if (e.target?.id !== "sx_dm_log" || e.target.scrollTop > 60) return;
  const c = SO.dm?.chat; if (c && c.next && !c.loading) soDMRead(true);
}, true);
// the message box grows with what is typed (up to six lines); Enter sends, Shift+Enter starts a new line
document.addEventListener("input", e => { if (e.target.id === "sx_dm_text") soGrow(e.target); });
document.addEventListener("keydown", e => {
  if (e.target.id !== "sx_dm_text" || e.key !== "Enter" || e.shiftKey || e.isComposing || e.keyCode === 229) return;
  e.preventDefault(); soDMSend();
});
function soGrow(t) { t.style.height = "auto"; t.style.height = Math.min(t.scrollHeight + 2, 150) + "px"; }

// 表情: a grid of common emoji under the button; a click puts one in at the cursor of the box it belongs to
const SO_EMOJI = ("😀 😃 😄 😁 😆 😅 😂 🤣 🥲 😊 😇 🙂 😉 😌 😍 🥰 😘 😗 😋 😛 😜 🤪 😝 🤗 🤭 🤫 🤔 😐 😑 😶 🙄 😏 😮 😯 😲 😳 🥺 😢 😭 😤 😠 😡 🤯 😱 😨 😰 😥 😓 🥱 😴 🤤 😪 😵 🤐 🥴 🤢 😷 🤒 🤕 🤑 🤠 😎 🤓 🧐 😈 👻 💀 👽 🤖 🎃 😺 😸 😹 😻 😼 😽 🙀 😿 😾 " +
  "👍 👎 👌 ✌️ 🤞 🤟 🤘 👏 🙌 🙏 🤝 💪 👋 🫶 ❤️ 🧡 💛 💚 💙 💜 🖤 🤍 💕 💞 💓 💗 💖 💘 💝 💯 ✨ 🌟 ⭐ 🔥 🎉 🎊 🎁 🎀 🌸 🌷 🌹 🍀 🌈 ☀️ 🌙 ⚡ ❄️ 🍰 🍓 🍑 🍡 🍵 ☕ 🎮 🎧 🎤 🎨 📷 👗 👘 👑 💄 🐱 🐶 🐰 🦊 🐻 🐼 🐧 🦄").split(" ");
function soEmojiOpen(btn) {
  const target = $("#" + btn.dataset.for); if (!target) return;
  let pop = $("#sx_emoji");
  if (pop && pop.dataset.for === btn.dataset.for) { pop.remove(); return; }
  pop?.remove();
  pop = document.createElement("div"); pop.id = "sx_emoji"; pop.className = "sxemoji"; pop.dataset.for = btn.dataset.for; pop.setAttribute("role", "dialog"); pop.setAttribute("aria-label", "插入表情");
  pop.innerHTML = SO_EMOJI.map(x => `<button type="button" data-emoji="${x}" data-i18n="off">${x}</button>`).join("");
  document.body.append(pop);
  const r = btn.getBoundingClientRect(), w = pop.offsetWidth, h = pop.offsetHeight;
  pop.style.left = Math.max(8, Math.min(innerWidth - w - 8, r.left)) + "px";
  pop.style.top = (r.top - h - 8 > 8 ? r.top - h - 8 : r.bottom + 8) + "px";
}
document.addEventListener("mousedown", e => {
  const pop = $("#sx_emoji"); if (!pop) return;
  const b = e.target.closest("[data-emoji]");
  if (b && pop.contains(b)) {
    e.preventDefault(); // (the box keeps its cursor)
    const t = $("#" + pop.dataset.for); if (!t) return;
    const at = t.selectionStart ?? t.value.length, end = t.selectionEnd ?? at;
    t.focus(); t.setRangeText(b.dataset.emoji, at, end, "end"); t.dispatchEvent(new Event("input", { bubbles: true }));
    return;
  }
  if (!pop.contains(e.target) && !e.target.closest('[data-so="emoji"]')) pop.remove();
});
function soDMInSight() { return S.view === "social" && SO.nav === "messages" && !SO.page && !document.hidden; }
function soPollWait() { return soDMInSight() ? 5000 : 30000; }
// soPollSoon: the next reading, counted from the last one (soUnreadLoad's too), never later than it already was
function soPollSoon() {
  const due = Math.max(Date.now(), soPollLast + soPollWait());
  if (soPollTimer && soPollDue <= due) return;
  clearTimeout(soPollTimer);
  soPollDue = due; soPollTimer = setTimeout(soPoll, due - Date.now());
}
async function soPoll() {
  soPollTimer = 0;
  try {
    if (Date.now() - soPollLast < soPollWait() - 50) return; // (the private messages left the screen meanwhile)
    soPollLast = Date.now();
    if (!SO.service?.account) return;
    const before = SO.unread.messages || 0;
    await soUnreadLoad();
    if (soDMInSight() && SO.dm && ((SO.unread.messages || 0) > before || Date.now() - soDMAt >= 30000)) {
      await soDMLoad();
      if (SO.dm?.chat && !SO.dm.chat.busy) await soDMRead();
    }
  } finally { soPollSoon(); }
}
soPollSoon();
document.addEventListener("visibilitychange", () => { if (!document.hidden) soPollSoon(); });

// ---------- 小组, 每周主题 and 投票 ----------
// 小组: each base the community's works use has a group (internal/social/groups.go); the library's bases first.
async function soGroupsLoad() {
  if (SO.busy.groups) return;
  const session = SO.session;
  SO.busy.groups = true;
  const r = await soServiceAPI("groups");
  SO.busy.groups = false;
  if (session !== SO.session) return;
  SO.groups = r; soDraw();
}
function soJoinBtn(g) {
  return `<button class="btn small sofollow sxjoin${g.joined ? " on" : " primary"}" data-so="groupjoin" data-id="${esc(g.id)}" data-on="${!g.joined}" aria-pressed="${!!g.joined}"><span class="sofollowlabel">${g.joined ? "已加入" : "加入"}</span>${g.joined ? `<span class="sofollowhover">退出</span>` : ""}</button>`;
}
function soGroupNumbers(g) { return `<span>${esc(`${g.members || 0} 位成员`)}</span><span>${esc(`本周 ${g.week || 0} 帖`)}</span>`; }
function soGroupRow(g) {
  return `<div class="sxgroup" data-so="groupopen" data-id="${esc(g.id)}" tabindex="0"><span class="sxgroupcover">${g.pic ? `<img src="${esc(g.pic)}" alt="" loading="lazy">` : `<i data-i18n="off">${esc(Array.from(g.name || "?")[0])}</i>`}</span><span class="sxgroupinfo"><b data-i18n="off">${esc(g.name)}</b><small>${soGroupNumbers(g)}</small></span>${soJoinBtn(g)}</div>`;
}
function soGroupsDraw() {
  soBar(null);
  const r = SO.groups, list = r?.groups || [], mine = list.filter(g => g.mine), rest = list.filter(g => !g.mine);
  if (!r && !SO.busy.groups) setTimeout(() => { if (!SO.groups) soGroupsLoad(); }); // (after a sign-in, for instance)
  const body = !r ? soEmpty("正在读取…") : !r.ok ? soEmpty("无法读取社区", `<p class="warnc small">${esc(r.err || "")}</p><button class="btn" data-so="refresh">重试</button>`)
    : !list.length ? soEmpty("还没有小组")
    : `${mine.length ? `<h3 class="soh sxgrouphead">我的素体</h3>${mine.map(soGroupRow).join("")}` : ""}${rest.length ? `<h3 class="soh sxgrouphead">热门小组</h3>${rest.map(soGroupRow).join("")}` : ""}`;
  gridPut(`<div class="sxtimeline sxgroups">${body}</div>`);
}
async function soGroupOpen(id) {
  const back = SO.nav === "group" ? SO.group?.back : SO.nav;
  if (SO.group?.id !== id) { SO.group = {id, data: null, posts: null}; SO.drafts.group = soNewDraft(); }
  SO.group.back = back; SO.page = null; SO.nav = "group"; closeModal(); if (S.view !== "social") setView("social");
  soDraw(); $(".main").scrollTop = 0;
  await soGroupLoad();
}
async function soGroupLoad() {
  const x = SO.group;
  if (!x || SO.busy.group) return;
  SO.busy.group = true;
  const r = await soServiceAPI("group", {id: x.id});
  SO.busy.group = false;
  if (SO.group !== x) return;
  x.data = r; soDraw();
  if (r.ok) await soGroupPosts(false);
}
async function soGroupPosts(more) {
  const x = SO.group;
  if (!x?.data?.ok) return;
  if (!more || !x.posts) x.posts = {items: [], next: "", loaded: false, busy: false};
  const p = x.posts;
  if (p.busy || more && !p.next) return;
  p.busy = true;
  const r = await soServiceAPI("group/posts", {id: x.id, after: more ? p.next : ""});
  p.busy = false;
  if (SO.group !== x || x.posts !== p) return;
  if (r.ok) { const ids = new Set(p.items.map(y => y.id)); p.items.push(...(r.posts || []).filter(y => !ids.has(y.id))); p.next = r.next || ""; p.loaded = true; p.error = ""; }
  else p.error = r.err;
  soDraw();
}
function soGroupDraw() {
  const x = SO.group, r = x?.data;
  put($("#resultbar"), `<button class="btn small ghost" data-so="groupback">${ICON.back}<span>返回</span></button><span class="spacer"></span><button class="btn small ghost" data-so="refresh"${SO.busy.group ? " disabled" : ""}>${ICON.upd}<span>刷新</span></button>`);
  if (!r) { gridPut(soEmpty("正在读取…")); if (!SO.busy.group) setTimeout(() => { if (SO.group === x && !x.data) soGroupLoad(); }); return; }
  if (!r.ok) { gridPut(soEmpty("无法读取该页面", `<p class="warnc small">${esc(r.err || "")}</p>`)); return; }
  const g = r.group, mods = g.moderators || [], pinned = r.pinned;
  const list = x.posts ? {...x.posts, items: x.posts.items.filter(p => p.id !== pinned?.id)} : null;
  gridPut(`<div class="sxtimeline sxgrouppage"><div class="sxbanner sxgroupbanner">${g.pic ? `<img src="${esc(g.pic)}" alt="">` : ""}</div><div class="sxprofilehead"><div class="sxgrouptop"><h2 data-i18n="off">${esc(g.name)}</h2>${soJoinBtn(g)}</div>
    <div class="sub"><button class="sxcount" data-so="groupmembers">${esc(`${g.members || 0} 位成员`)}</button><span>${esc(`本周 ${g.week || 0} 帖`)}</span></div>
    ${mods.length ? `<div class="sub sxmods"><span>版主</span>${mods.map(m => soProfileBtn(m.id, m.github, m.nickname, "", true, m.booth)).join("")}</div>` : ""}</div>
    ${g.joined ? soComposerHTML("group") : ""}${pinned ? `<div class="sxpinned"><p class="sxpinmark">${SO_TOOL.pin}<span>已置顶</span></p>${soPostCard(pinned)}</div>` : ""}${soPostListHTML(list, false, "groupmore")}</div>`);
  soPageWatch("#grid .sxgrouppage .sxmore", () => soGroupPosts(true));
}
async function soGroupJoin(id, on, btn) {
  if (!SO.service?.account) { soAccountDialog(() => soGroupJoin(id, on)); return; }
  if (btn) btn.disabled = true;
  const r = await soServiceAPI("group/join", {id, on});
  if (btn) btn.disabled = false;
  if (!r.ok) return soServiceError(r);
  const g = r.group || {};
  for (const y of SO.groups?.groups || []) if (y.id === id) Object.assign(y, {joined: !!g.joined, members: g.members});
  if (SO.group?.id === id && SO.group.data?.ok) Object.assign(SO.group.data.group, {joined: !!g.joined, members: g.members});
  toast(on ? "已加入" : "已退出");
  soDraw();
}
async function soGroupMembers(more) {
  const x = SO.group, m = $("#modal");
  if (!x?.data?.ok) return;
  if (!more) {
    x.members = {items: [], next: ""};
    m.dataset.kind = "sxmembers";
    m.innerHTML = `<div class="mhead">成员</div><div class="mbody">正在读取…</div><div class="mfoot"><button class="btn" data-m="close">关闭</button></div>`; showModal();
  }
  const list = x.members;
  if (!list || more && !list.next) return;
  const r = await soServiceAPI("group/members", {id: x.id, after: more ? list.next : ""});
  if (SO.group !== x || x.members !== list || m.dataset.kind !== "sxmembers" || !m.classList.contains("on")) return;
  if (r.ok) { list.items.push(...(r.accounts || [])); list.next = r.next || ""; }
  put($(".mbody", m), r.ok ? (list.items.map(a => soPersonRow(a)).join("") || soEmpty("暂无内容")) + (list.next ? `<button class="btn" data-so="groupmembersmore">加载更多</button>` : "") : `<p class="warnc">${esc(r.err)}</p>`);
}
// a moderator's 置顶: one post of the group's own, on the group's page
function soCanPin(p, b) { return SO.nav === "group" && !SO.page && !!SO.group?.data?.group?.moderator && !!b.closest(".sxgrouppage") && !p.reply_to; }
async function soGroupPin(p, on) {
  const x = SO.group;
  if (!x) return;
  const r = await soServiceAPI("group/pin", {id: x.id, post: p.id, on});
  if (!r.ok) return soServiceError(r);
  toast(on ? "已置顶" : "已取消置顶");
  await soGroupLoad();
}
async function soGroupAction(b, e) {
  switch (b.dataset.so) {
    case "groupopen": e.stopPropagation(); return soGroupOpen(b.dataset.id);
    case "groupjoin": e.stopPropagation(); return soGroupJoin(b.dataset.id, b.dataset.on === "true", b);
    case "groupback": SO.page = null; SO.nav = SO.group?.back && SO.group.back !== "group" ? SO.group.back : "groups"; soNeed(false); soDraw(); $(".main").scrollTop = 0; return;
    case "groupmore": return soGroupPosts(true);
    case "groupmembers": return soGroupMembers(false);
    case "groupmembersmore": return soGroupMembers(true);
  }
}
// 每周主题: the theme of the week as a banner above 为你推荐 (closed for that week on this computer), and the past
// themes on their hashtag's search page; 「参加」 opens the composer with the hashtag
async function soThemesLoad(refresh) {
  if (SO.busy.themes || SO.themes?.ok && !refresh) return;
  SO.busy.themes = true;
  const r = await soServiceAPI("themes");
  SO.busy.themes = false;
  SO.themes = r; soDraw();
}
function soThemeOff(id) { try { return localStorage.getItem("so-theme-off:" + id) === "1"; } catch (e) { return false; } }
function soThemeHTML(closable) {
  const t = SO.themes?.ok && SO.themes.current;
  if (!t || closable && soThemeOff(t.id)) return "";
  return `<div class="sxtheme"><span class="sxthemelabel">本周主题</span><span class="sxthemetext"><b data-i18n="off">${esc(t.title)}</b></span><button class="linkbtn" data-so="posttag" data-tag="${esc(t.tag)}" data-i18n="off">#${esc(t.tag)}</button><button class="btn small primary" data-so="xthemejoin" data-tag="${esc(t.tag)}">参加</button>${closable ? `<button class="wb" data-so="xthemeoff" data-id="${esc(t.id)}" title="关闭" aria-label="关闭">${ICON.x}</button>` : ""}</div>`;
}
function soThemesForTag() {
  const q = SO.nav === "search" ? String(SO.q || "").trim() : "", all = SO.themes?.ok ? SO.themes.themes || [] : [];
  const tag = q.startsWith("#") ? q.slice(1).toLowerCase() : "";
  if (!tag || !all.some(t => t.tag.toLowerCase() === tag)) return "";
  const current = SO.themes.current, past = all.filter(t => t.id !== current?.id);
  const day = s => new Date(s).toLocaleDateString(I18N.lang || "zh-CN");
  const row = t => `<button class="sxthemerow${t.tag.toLowerCase() === tag ? " on" : ""}" data-so="posttag" data-tag="${esc(t.tag)}"><b data-i18n="off">${esc(t.title)}</b><span data-i18n="off">#${esc(t.tag)}</span><time data-i18n="off">${esc(day(t.start))} – ${esc(day(Date.parse(t.end) - 1000))}</time></button>`;
  return `<section class="sxthemes">${current?.tag.toLowerCase() === tag ? soThemeHTML(false) : ""}${past.length ? `<h3 class="soh">往期主题</h3>${past.map(row).join("")}` : ""}</section>`;
}
// 投票: options to vote on, or the results (percentages and the total) once voted, for the author and when it ended
function soPollLeft(v) {
  const ms = Date.parse(v.ends) - Date.now();
  if (v.closed || !(ms > 0)) return "已结束";
  const m = Math.ceil(ms / 60000);
  return m >= 1440 ? `剩余 ${Math.floor(m / 1440)} 天` : m >= 60 ? `剩余 ${Math.floor(m / 60)} 小时` : `剩余 ${m} 分钟`;
}
function soPollHTML(p, embedded) {
  const v = p.poll;
  if (!v?.options?.length) return "";
  const total = v.total || 0, meta = `<p class="sxpollmeta"><span>${esc(`${total} 票`)}</span> · <span>${esc(soPollLeft(v))}</span></p>`;
  if (v.votes) {
    const top = Math.max(0, ...v.votes);
    return `<div class="sxpoll">${v.options.map((o, i) => {
      const n = v.votes[i] || 0, pct = total ? Math.round(n * 100 / total) : 0, mine = v.voted === i + 1;
      return `<div class="sxpollbar${n && n === top ? " lead" : ""}${mine ? " mine" : ""}" style="--pct:${pct}%"><span class="sxpolllabel" data-i18n="off">${esc(o)}</span>${mine ? `<span class="sxpollmine" title="已投">✓</span>` : ""}<span class="sxpollpct" data-i18n="off">${pct}%</span></div>`;
    }).join("")}${meta}</div>`;
  }
  return `<div class="sxpoll">${v.options.map((o, i) => embedded ? `<span class="sxpollopt" data-i18n="off">${esc(o)}</span>` : `<button class="sxpollopt" data-so="xvote" data-id="${esc(p.id)}" data-i="${i}" data-i18n="off">${esc(o)}</button>`).join("")}${meta}</div>`;
}
async function soVote(p, i) {
  if (!p || SO.busy["vote" + p.id]) return;
  if (!SO.service?.account) { soAccountDialog(() => soVote(p, i)); return; }
  SO.busy["vote" + p.id] = true;
  const r = await soServiceAPI("poll/vote", {id: p.id, option: i});
  SO.busy["vote" + p.id] = false;
  if (!r.ok) return soServiceError(r);
  const poll = r.post?.poll, update = x => { if (!x) return; if (x.id === p.id && poll) x.poll = poll; if (x.quote) update(x.quote); };
  for (const list of soPostLists()) for (const x of list || []) update(x);
  soDraw();
  if ($("#modal")?.dataset.kind === "sopost" && SO.post?.data?.ok) soPostDraw();
}
function soPollEditHTML(c) {
  const v = c.poll;
  return `<div class="sxpolledit">${v.options.map((o, i) => `<div class="sxpollrow"><input class="sxpollinput" data-i="${i}" maxlength="25" value="${esc(o)}" placeholder="${esc(`选项 ${i + 1}`)}" aria-label="${esc(`选项 ${i + 1}`)}" autocomplete="off">${i >= 2 ? `<button class="wb" data-so="xpolldel" data-i="${i}" title="移除" aria-label="移除">${ICON.x}</button>` : ""}</div>`).join("")}
    <div class="sxpolltools">${v.options.length < 4 ? `<button class="btn small" data-so="xpolladd">添加选项</button>` : ""}<label><span>时长</span><select class="sxpolldays" aria-label="时长">${[1, 3, 7].map(d => `<option value="${d}"${v.days === d ? " selected" : ""}>${esc(`${d} 天`)}</option>`).join("")}</select></label><span class="spacer"></span><button class="btn small ghost" data-so="xpolldrop">移除投票</button></div></div>`;
}
function soPollError(poll, text) {
  const options = poll.options;
  return !text ? "请填写投票的问题" : options.some(o => !o) ? "投票选项不能为空" : options.some(o => Array.from(o).length > 25) ? "投票选项最多 25 个字"
    : new Set(options.map(o => o.toLowerCase())).size !== options.length ? "投票选项不能重复" : "";
}
document.addEventListener("input", e => {
  if (!e.target.classList?.contains("sxpollinput")) return;
  const key = e.target.closest("[data-composer]")?.dataset.composer, c = key && soDraft(key);
  if (c?.poll) c.poll.options[+e.target.dataset.i] = e.target.value;
});
document.addEventListener("change", e => {
  if (!e.target.classList?.contains("sxpolldays")) return;
  const key = e.target.closest("[data-composer]")?.dataset.composer, c = key && soDraft(key);
  if (c?.poll) c.poll.days = +e.target.value;
});

// ---------- 求同款 ----------
// An ask (「求同款」 beside 「发帖」) is a post with pictures asking what an outfit is; replies answer it with a work card
// (「用作品回答」) or Booth items (「附上 Booth 商品」), and the asker marks the one that solved it (「设为答案」), which
// then leads the thread as 「就是这套」 (internal/social/asks.go). 探索 「求同款」 lists them, the open ones first.
const SO_ATABS = [["all", "全部", "list"], ["open", "待解决", "help"], ["solved", "已解决", "answer"]];
// soAnswering: the draft is a direct reply to an ask, which may answer it with a work card or Booth items
function soAnswering(c) { return !!(c?.reply_to && c.target?.ask && c.target.id === c.reply_to); }
function soAskMarksHTML(p, ask) {
  const marks = [];
  if (p.ask) marks.push(`<span class="sxasklabel">${SO_TOOL.ask}<span>求同款</span></span>`, p.solved ? `<span class="sxsolved">已解决</span>` : "");
  if (ask?.answer && ask.answer === p.id) marks.push(`<span class="sxthisone">就是这套</span>`);
  return marks.length ? `<div class="sxaskmarks">${marks.join("")}</div>` : "";
}
// a post's Booth items, each its Booth page, and 「已拥有」 for those the library has
function soAssetItemsHTML(p) {
  const items = (p.assetItems || []).filter(x => x.boothId);
  if (!items.length) return "";
  return `<div class="sxitems"><span class="sxitemshead">Booth 商品</span>${items.map(x => `<button class="sxitem" data-so="link" data-kind="booth_item" data-href="${esc(x.url)}" title="${esc(x.url)}"><span data-i18n="off">${esc(x.name || "#" + x.boothId)}</span>${x.owned ? `<span class="tag ok">已拥有</span>` : ""}</button>`).join("")}</div>`;
}
// 「设为答案」 / 「取消答案」 under each direct reply, for the asker
function soAnswerButtonHTML(p, ask, embedded) {
  if (!ask?.mine || embedded || p.deleted || p.reply_to !== ask.id) return "";
  const on = ask.answer === p.id;
  return `<div class="sxanswerrow"><button class="btn small" data-so="askmark" data-id="${esc(ask.id)}" data-reply="${esc(p.id)}" data-on="${!on}">${on ? "取消答案" : "设为答案"}</button></div>`;
}
function soAnswerToolsHTML(c) {
  const tool = (what, panel, icon, label) => `<button class="sxtool${c.askPanel === panel ? " on" : ""}" data-so="${what}" aria-pressed="${c.askPanel === panel}"${c.busy ? " disabled" : ""}>${icon}<span>${label}</span></button>`;
  return tool("askworkopen", "work", SO_TOOL.work, "用作品回答") + tool("askboothopen", "booth", SO_TOOL.booth, "附上 Booth 商品");
}
// what the answer carries (its work card, its Booth items) and the picker that is open
function soAnswerDraftHTML(c, key) {
  const items = c.assets || [], w = c.answerWork;
  const picked = `${w ? `<div class="sxanswerwork">${SO_TOOL.work}<span class="sxanswertitle"><b data-i18n="off">${esc(w.title)}</b>${w.base ? `<small data-i18n="off">${esc(w.base)}</small>` : ""}</span><button class="wb" data-so="askworkdrop" title="移除" aria-label="移除">${ICON.x}</button></div>` : ""}${items.length ? `<div class="sxansweritems">${items.map((x, i) => `<span class="sxansweritem"><span data-i18n="off">${esc(x.name || "#" + x.id)}</span><button class="wb" data-so="askboothdel" data-i="${i}" title="移除" aria-label="移除">${ICON.x}</button></span>`).join("")}</div>` : ""}`;
  const panel = c.askPanel === "work" ? `<div class="sxanswerpick"><input id="sc_${key}_workq" class="sxanswerq" value="${esc(c.workQuery || "")}" placeholder="按标题或作品编号搜索" aria-label="按标题或作品编号搜索" autocomplete="off"><div class="solibrows" id="sc_${key}_works">${soAnswerWorkChoices(c)}</div></div>`
    : c.askPanel === "booth" ? `<div class="sxanswerpick sxanswerbooth"><input id="sc_${key}_booth" class="sxanswerbq" placeholder="Booth 商品地址或编号" aria-label="Booth 商品地址或编号" spellcheck="false" autocomplete="off"><button class="btn small" data-so="askboothadd">添加</button></div>` : "";
  return picked || panel ? `<div class="sxanswerdraft">${picked}${panel}</div>` : "";
}
// the community's works to answer with, the player's own first (the works the publishing dialog picks from)
function soAnswerWorkChoices(c) {
  if (!SO.lists?.works) return `<p class="muted small">正在读取…</p>`;
  const words = soWords(c.workQuery), hits = SO.lists.works.filter(w => soHit(words, w.title, w.id, w.base || ""));
  hits.sort((a, b) => (b.mine ? 1 : 0) - (a.mine ? 1 : 0));
  return hits.slice(0, 20).map(w => `<button class="navitem" data-so="askworkpick" data-id="${esc(w.id)}">${w.pic ? `<img src="${esc(w.pic)}" alt="" loading="lazy"${soHideMedia(soWorkMedia(w)) ? ` class="sxsensimg" data-sensimg="${esc(soWorkMedia(w).id)}"` : ""}>` : ""}<span class="nm" data-i18n="off">${esc(w.title)}</span><span class="n" data-i18n="off">${esc(w.base || "")}</span></button>`).join("") || `<p class="muted small">没有符合条件的作品</p>`;
}
// soBoothItemID: the item a Booth address (booth.pm/ja/items/123, shop.booth.pm/items/123), booth:123 or a number names
function soBoothItemID(raw) {
  const s = String(raw || "").trim();
  const m = /^(?:booth:|#)?(\d{1,12})$/i.exec(s) || /^https?:\/\/(?:[a-z0-9-]+\.)?booth\.pm\/(?:[a-z]{2}(?:-[a-z]{2,4})?\/)?items\/(\d{1,12})\/?(?:[?#]\S*)?$/i.exec(s);
  return m ? m[1] : "";
}
function soAnswerBoothAdd(key) {
  const c = soDraft(key), id = soBoothItemID($(`#sc_${key}_booth`)?.value);
  c.assets = c.assets || [];
  if (!id) { c.error = "无法识别 Booth 商品，请填写商品地址或编号"; soComposeError(key); return; }
  if (!c.assets.some(x => x.id === id)) {
    if (c.assets.length >= 10) { c.error = "最多 10 件商品"; soComposeError(key); return; }
    const lib = ((S.data && S.data.assets) || []).find(a => a.boothId === id);
    c.assets.push({id, name: lib ? (lib.booth && lib.booth.name) || lib.name : ""});
  }
  c.error = ""; soComposeDraw(key); $(`#sc_${key}_booth`)?.focus();
}
async function soAnswerMark(askId, replyId, on, btn) {
  if (!SO.service?.account) { soAccountDialog(() => soAnswerMark(askId, replyId, on)); return; }
  if (SO.busy["answer" + askId]) return;
  SO.busy["answer" + askId] = true; if (btn) btn.disabled = true;
  const r = await soServiceAPI("post/answer", {id: askId, reply: replyId, on});
  SO.busy["answer" + askId] = false;
  if (!r.ok) { if (btn) btn.disabled = false; return soServiceError(r); }
  const answer = r.post?.answer || "", solved = !!r.post?.solved;
  for (const list of soPostLists()) for (const x of list || []) if (x?.id === askId) { x.answer = answer; x.solved = solved; }
  toast(on ? "已设为答案" : "已取消答案");
  if (SO.post?.id === askId) await soThreadLoad(false);
  soDraw();
}
async function soAsksLoad(more) {
  if (!more || !SO.asks || SO.asks.filter !== SO.askFilter) SO.asks = {filter: SO.askFilter, items: [], next: "", loaded: false, busy: false};
  const p = SO.asks;
  if (p.busy || more && !p.next) return;
  p.busy = true; SO.busy.asks = true; soDraw();
  const r = await soServiceAPI("asks", {filter: p.filter, after: more ? p.next : ""});
  p.busy = false; SO.busy.asks = !!SO.asks?.busy;
  if (SO.asks !== p) return;
  if (r.ok) { const ids = new Set(p.items.map(x => x.id)); p.items.push(...(r.posts || []).filter(x => !ids.has(x.id))); p.next = r.next || ""; p.loaded = true; p.error = ""; }
  else p.error = r.err;
  soDraw();
}
function soAsksDraw() {
  soBar(null);
  if (!SO.asks && !SO.busy.asks) setTimeout(() => { if (!SO.asks) soAsksLoad(false); }); // (after a sign-in, for instance)
  gridPut(`<div class="sxtimeline sxasks">${soTabs(SO_ATABS, SO.askFilter, "askstab")}${soPostListHTML(SO.asks, false, "asksmore")}</div>`);
  soPageWatch("#grid .sxasks .sxmore", () => soAsksLoad(true));
}
async function soAskAction(b, e) {
  const key = b.closest("[data-composer]")?.dataset.composer || "dialog", c = soDraft(key);
  switch (b.dataset.so) {
    case "ask": return soComposeOpen("ask");
    case "askstab": SO.askFilter = b.dataset.tab; return soAsksLoad(false);
    case "asksmore": return soAsksLoad(true);
    case "askmark": e.stopPropagation(); return soAnswerMark(b.dataset.id, b.dataset.reply, b.dataset.on === "true", b);
    case "askworkopen": case "askboothopen": {
      const panel = b.dataset.so === "askworkopen" ? "work" : "booth";
      c.askPanel = c.askPanel === panel ? "" : panel; soComposeDraw(key);
      $(`#sc_${key}_${panel === "work" ? "workq" : "booth"}`)?.focus();
      if (panel === "work" && !SO.lists) { await soLoad("lists"); if (c.askPanel === "work") put($(`#sc_${key}_works`), soAnswerWorkChoices(c)); }
      return;
    }
    case "askworkpick": {
      const w = SO.lists?.works?.find(x => x.id === b.dataset.id); if (!w || c.busy) return;
      c.answerWork = {id: w.id, title: w.title, base: w.base || ""}; c.askPanel = ""; return soComposeDraw(key);
    }
    case "askworkdrop": if (!c.busy) { c.answerWork = null; soComposeDraw(key); } return;
    case "askboothadd": if (!c.busy) soAnswerBoothAdd(key); return;
    case "askboothdel": if (!c.busy) { (c.assets || []).splice(+b.dataset.i, 1); soComposeDraw(key); } return;
  }
}
document.addEventListener("input", e => {
  if (!e.target.classList?.contains("sxanswerq")) return;
  const key = e.target.closest("[data-composer]")?.dataset.composer, c = key && soDraft(key);
  if (c) { c.workQuery = e.target.value; put($(`#sc_${key}_works`), soAnswerWorkChoices(c)); }
});
document.addEventListener("keydown", e => {
  if (e.key !== "Enter" || !e.target.classList?.contains("sxanswerbq")) return;
  e.preventDefault();
  const key = e.target.closest("[data-composer]")?.dataset.composer;
  if (key && !soDraft(key).busy) soAnswerBoothAdd(key);
});
