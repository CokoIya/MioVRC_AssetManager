// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// for a player starting from nothing: well-known bases to start with while the library has none (the modding basket
// and the checklist), 「适配我的素体」 in the Booth side bar, a base bought from those recommendations put into the
// basket once it is in the library, the checklist on 「我的模型」, 「撤销这次改动」 on a one-click result, and the
// glossary marks (a dotted underline, one plain sentence on hover or focus)
"use strict";

// Well-known bases: the name Booth knows each by (its search word), the base table's name (internal/core
// DefaultSettings) and the Booth item's number where the program's own fixtures confirm it; the others open a Booth
// search for the name among 素体.
const NB_BASES = [
  { ja: "桔梗", base: "Kikyo", id: "3681787" },
  { ja: "セレスティア", base: "Selestia" },
  { ja: "マヌカ", base: "Manuka" },
  { ja: "森羅", base: "Shinra" },
  { ja: "ラスク", base: "Rusk" },
  { ja: "ミルティナ", base: "Milltina" },
  { ja: "竜胆", base: "Rindo" },
  { ja: "舞夜", base: "Maya" },
  { ja: "萌", base: "Moe" },
  { ja: "カリン", base: "Karin" },
];
// kept: the checklist's state ({dismissed, upload, see}); intent: the recommendations opened ([{id, base, ja, at}])
const NB_KEEP = "mio.newbie", NB_INTENT = "mio.newbie.intent", NB_INTENT_DAYS = 60;
// rev: the library state looked at last; job: the last one-click job (undefined until asked); undo: the plan the
// undo dialog shows ({id, plan}), busy while it runs
const NB = { rev: -1, job: undefined, jobAt: 0, undo: null, undoBusy: false };

function nbAssets() { return (typeof S !== "undefined" && S.data && S.data.assets) || []; }
function nbHasBase() { return nbAssets().some(a => a.category === "素体"); }
// the player's bases: those of the library's 素体 cards (bought ones not downloaded yet included), the most first
function nbMyBases() {
  const n = new Map();
  for (const a of nbAssets()) if (a.category === "素体") for (const b of a.bases || []) n.set(b, (n.get(b) || 0) + 1);
  return [...n].sort((x, y) => y[1] - x[1]).map(x => x[0]);
}

// ---------- bases to start with ----------
function nbBasesHTML() {
  return `<ul class="nbbases">${NB_BASES.map((b, i) => `<li><button class="nbbase" data-nb="base" data-i="${i}"><b data-i18n="off">${esc(b.ja)}</b><span data-i18n="off">${esc(b.base)}</span></button></li>`).join("")}</ul>`;
}
// in the basket while no base is picked and the library has none (oneclick.js ocPanelHTML)
function nbNoBaseHTML() { return `<div class="nbnobase"><p class="nbhead">还没有素体</p>${nbBasesHTML()}</div>`; }
// the bases to start with for an item made for these (ocbooth.js): the well-known ones among them, and the others
// by their name (a Booth search among 素体); all ten when it names none
function nbBasesFor(bases) {
  const low = s => String(s || "").toLowerCase(), want = (bases || []).map(low);
  const known = NB_BASES.map((b, i) => [b, i]).filter(([b]) => want.includes(low(b.base)) || want.includes(low(b.ja)));
  const rest = (bases || []).filter(n => !NB_BASES.some(b => low(b.base) === low(n) || low(b.ja) === low(n)));
  if (!known.length && !rest.length) return nbBasesHTML();
  return `<ul class="nbbases">${known.map(([b, i]) => `<li><button class="nbbase" data-nb="base" data-i="${i}"><b data-i18n="off">${esc(b.ja)}</b><span data-i18n="off">${esc(b.base)}</span></button></li>`).join("")}${
    rest.map(n => `<li><button class="nbbase" data-nb="basename" data-base="${esc(n)}"><b data-i18n="off">${esc(n)}</b></button></li>`).join("")}</ul>`;
}
// a base known by its name only: a Booth search for it among 素体, remembered like a recommendation
function nbOpenBaseName(name) {
  if (!name) return;
  nbIntentAdd({ base: name, ja: name });
  const sh = S.shop; sh.cats = new Set(["素体"]); sh.bases.clear(); sh.styles.clear();
  goBooth({ name, boothQuery: name });
}
function nbBasesOpen() {
  const m = $("#modal"); m.dataset.kind = "nbbases"; m.classList.remove("ocwin");
  put(m, `<div class="mhead">还没有素体</div><div class="mbody">${nbBasesHTML()}</div><div class="mfoot"><button class="btn" data-nb="close">关闭</button></div>`);
  showModal();
}
// a recommendation opened: the item's page in the in-app shop, or a Booth search among 素体; remembered, so the base
// goes into the basket once it is bought and in the library
function nbOpenBase(i) {
  const b = NB_BASES[i]; if (!b) return;
  nbIntentAdd(b);
  const m = $("#modal");
  if (m.classList.contains("on") && m.dataset.kind === "nbbases") { m.dataset.kind = ""; closeModal(true); }
  if (b.id) return goBooth({ boothId: b.id, name: b.ja });
  const sh = S.shop; sh.cats = new Set(["素体"]); sh.bases.clear(); sh.styles.clear();
  goBooth({ name: b.ja, boothQuery: b.ja });
}

// ---------- a recommended base, once bought and in the library, into the basket ----------
function nbIntents() {
  try {
    const v = JSON.parse(localStorage.getItem(NB_INTENT) || "[]");
    return Array.isArray(v) ? v.filter(x => x && x.base && Date.now() - (x.at || 0) < NB_INTENT_DAYS * 864e5) : [];
  } catch (_) { return []; }
}
function nbIntentSave(list) { try { if (list.length) localStorage.setItem(NB_INTENT, JSON.stringify(list)); else localStorage.removeItem(NB_INTENT); } catch (_) {} }
function nbIntentAdd(b) { nbIntentSave(nbIntents().filter(x => x.base !== b.base).concat({ id: b.id || "", base: b.base, ja: b.ja, at: Date.now() })); }
// the card a recommendation became: its Booth item, or (from a search) a base of that name
function nbIntentHit(x, a) {
  if (x.id) return String(a.boothId || "") === x.id;
  if (a.category !== "素体") return false;
  const name = String(a.name || "").toLowerCase();
  return (a.bases || []).includes(x.base) || name.includes(x.base.toLowerCase()) || (!!x.ja && name.includes(x.ja.toLowerCase()));
}
// a card on disk (downloaded and imported, not a purchase still on Booth) of a kind the basket takes
function nbIntentCheck() {
  if (typeof oneclickToggle !== "function" || typeof ocGroupOf !== "function") return;
  let list = nbIntents(); if (!list.length) return;
  let added = 0;
  for (const a of nbAssets()) {
    if (!isLocal(a) || !ocGroupOf(a.category)) continue;
    const x = list.find(x => nbIntentHit(x, a)); if (!x) continue;
    list = list.filter(y => y !== x);
    if (!ocHas(a.key)) { oneclickToggle(a); if (ocHas(a.key)) added++; }
  }
  nbIntentSave(list);
  if (added) toast("已加入改模篮", 4000);
}

// ---------- 「适配我的素体」 in the Booth side bar (app.js renderShopSide) ----------
function nbMineOn(mine) { const on = S.shop.bases; return mine.length > 0 && on.size === mine.length && mine.every(b => on.has(b)); }
function nbMineChip() {
  const mine = nbMyBases(); if (!mine.length) return "";
  const on = nbMineOn(mine);
  return `<button class="chip nbmine${on ? " on" : ""}" data-nb="mybases" aria-pressed="${on}">适配我的素体</button>`;
}
// on: what fits the player's bases, so not other bases, nor the words of a recommendation's search
function nbMineToggle() {
  const mine = nbMyBases(); if (!mine.length) return;
  const on = !nbMineOn(mine);
  S.shop.bases = new Set(on ? mine : []);
  if (on) {
    S.shop.cats.delete("素体");
    if (NB_BASES.some(b => b.ja === S.shop.q)) { S.shop.q = ""; if (S.view === "shop") $("#q").value = ""; }
  }
  if (S.web.open.shop) closeWeb();
  saveUI(); renderShopSide(); shopSearch(true);
}

// ---------- the checklist on 「我的模型」 ----------
// key, step, what its button says
const NB_STEPS = [["base", "选素体", "去选择"], ["dress", "穿衣服", "去搭配"], ["upload", "上传", "上传方法"], ["see", "在 VRChat 看到自己", "打开 VRChat 网站"]];
const NB_SEE_URL = "https://vrchat.com/home/avatars";
function nbKept() { try { return JSON.parse(localStorage.getItem(NB_KEEP) || "{}") || {}; } catch (_) { return {}; } }
function nbKeep(k, v) { const o = nbKept(); o[k] = v; try { localStorage.setItem(NB_KEEP, JSON.stringify(o)); } catch (_) {} }
// each step from what is there: a base in the library; a finished one-click creation, or clothes used in a project;
// an upload from the one-click result (else ticked by the player, who may upload otherwise); seeing yourself, by
// the player
function nbSteps() {
  const kept = nbKept(), j = NB.job;
  return {
    base: nbHasBase(),
    dress: !!(j && j.state === "done" && !j.undone) || nbAssets().some(a => a.category === "衣服" && (a.usage || []).length > 0),
    upload: !!kept.upload || !!(j && j.result && j.result.uploaded),
    see: !!kept.see,
  };
}
function nbCheckHTML() {
  const st = nbSteps(), n = NB_STEPS.filter(s => st[s[0]]).length;
  return `<section class="nbcheck" aria-label="新手任务"><header><b>新手任务</b><span class="muted small">${n} / ${NB_STEPS.length}</span><span class="grow"></span>
    ${NB.job ? `<button class="btn small" data-nb="last">上次创建</button>` : ""}<button class="btn small ghost" data-nb="dismiss">不再显示</button></header>
    <ol>${NB_STEPS.map(([k, name, go], i) => {
      const done = st[k], manual = k === "upload" || k === "see";
      return `<li class="nbstep${done ? " done" : ""}" data-step="${k}"><i aria-hidden="true">${done ? "✓" : i + 1}</i><span class="nm">${name}</span>${done ? `<span class="nbvh">已完成</span>`
        : `<span class="acts"><button class="btn small" data-nb="go" data-step="${k}">${go}</button>${manual ? `<button class="btn small ghost" data-nb="mark" data-step="${k}">已完成</button>` : ""}</span>`}</li>`;
    }).join("")}</ol></section>`;
}
// The card sits above the project cards while 「我的模型」 is in view and it has not been put away.
function nbCheckSync() {
  const grid = $("#grid"); if (!grid) return;
  let el = $("#nb_check");
  if (!(typeof S !== "undefined" && S.data && S.view === "proj" && !nbKept().dismissed)) { if (el) el.hidden = true; return; }
  if (!el) { el = document.createElement("div"); el.id = "nb_check"; grid.before(el); }
  el.hidden = false;
  if (NB.job === undefined || Date.now() - NB.jobAt > 15000) nbJobFetch();
  put(el, nbCheckHTML());
}
async function nbJobFetch() {
  NB.jobAt = Date.now();
  try {
    const r = typeof ocStatus === "function" ? await ocStatus() : null;
    if (r && r.ok) { NB.job = r.job || null; nbCheckSync(); }
  } catch (_) {}
}
function nbGo(step) {
  if (step === "base") return nbBasesOpen();
  if (step === "dress") { // the library, at the bases while the basket has none, else at the clothes
    const hasBase = typeof OC !== "undefined" && OC.items.some(i => i.group === "base");
    setView("lib"); S.cat = hasBase ? "衣服" : "素体"; renderSide(); renderGrid(); return;
  }
  if (step === "upload") { if (typeof openGuide === "function") { openGuide(); if (typeof guideDraw === "function") { guidePage = GUIDE_PAGES.length - 1; guideDraw(); } } return; }
  if (step === "see") return openURL(NB_SEE_URL);
}

// ---------- 「撤销这次改动」 on a finished one-click creation (the result's button: HOOKS_newbie_a67.md) ----------
async function ocUndoAsk(j) {
  j = j || (typeof OC !== "undefined" ? OC.job : null);
  if (!j || NB.undoBusy) return;
  let r; try { r = await api("/api/oneclick/undo", { id: j.id, dry: true }); } catch (e) { r = { ok: false, err: e.message }; }
  if (!r.ok) { toast(r.err || "无法撤销", 5000); return; }
  NB.undo = { id: j.id, plan: r.plan || {} };
  ocUndoDraw();
}
// one dialog saying exactly what goes
function ocUndoHTML(p) {
  const rows = [];
  if (p.kind === "project") rows.push(`<li><span>工程文件夹</span><b data-i18n="off">${esc(p.name)}</b><span class="path" data-i18n="off">${esc(p.project)}</span></li>`);
  else {
    for (const f of p.folders || []) rows.push(`<li><span>所在文件夹</span><b data-i18n="off">${esc(f)}</b></li>`);
    if (p.scene) rows.push(`<li><span>场景</span><b data-i18n="off">${esc(p.scene)}</b></li>`);
  }
  return `<div class="nbundo">${p.avatar ? `<p>模型「${esc(p.avatar)}」将还原到创建前，现在的模型保留并隐藏</p>` : ""}
    ${p.kind === "existing" ? `<p>${esc(`将移除 ${p.count || 0} 个文件（含 .meta）`)}</p><p>${esc(`保留 ${p.keptCount || 0} 个后来修改的文件`)}</p>${(p.kept || []).length ? `<ul>${p.kept.map(f => `<li data-i18n="off">${esc(f)}</li>`).join("")}</ul>` : ""}` : ""}
    ${rows.length ? `<p>${p.kind === "project" ? "以下内容将移到回收站，并从「我的模型」中移除：" : "以下内容将移到回收站："}</p><ul>${rows.join("")}</ul>` : ""}
    ${p.needUnity ? `<p class="warnc">请先在 Unity 中打开该工程</p>` : ""}</div>`;
}
function ocUndoDraw() {
  const u = NB.undo, m = $("#modal"); if (!u) return;
  m.dataset.kind = "ocundo"; m.classList.remove("ocwin"); // (its width is its own: nothing stays behind when Escape closes it)
  put(m, `<div class="mhead">撤销这次改动</div><div class="mbody">${ocUndoHTML(u.plan)}</div>
    <div class="mfoot"><button class="btn" data-nb="undoback">取消</button><button class="btn primary" data-nb="undogo"${NB.undoBusy || u.plan.needUnity ? " disabled" : ""}>${NB.undoBusy ? "正在撤销…" : "撤销"}</button></div>`);
  showModal();
}
async function ocUndoGo() {
  const u = NB.undo; if (!u || NB.undoBusy) return;
  NB.undoBusy = true; ocUndoDraw();
  let r; try { r = await api("/api/oneclick/undo", { id: u.id }); } catch (e) { r = { ok: false, err: e.message }; }
  NB.undoBusy = false;
  if (!r.ok) { toast(r.err || "无法撤销", 6000); ocUndoDraw(); return; }
  NB.undo = null;
  if (r.job) { NB.job = r.job; if (typeof OC !== "undefined") OC.job = r.job; }
  const m = $("#modal"); m.dataset.kind = ""; closeModal(true);
  toast("已撤销这次改动", 4000);
  if (u.plan.kind === "project") { if (typeof load === "function") load().catch(() => {}); if (S.view === "proj") loadProjects(); }
  if (typeof ocDraw === "function") ocDraw();
  nbCheckSync();
}
function ocUndoBack() {
  NB.undo = null;
  if (typeof ocJobOpen === "function" && typeof OC !== "undefined" && OC.job) ocJobOpen(); else closeModal(true);
}

// ---------- the glossary ----------
const NB_TERMS = {
  physbone: "VRChat 的物理组件，让头发、裙摆、尾巴随动作摆动。",
  ma: "把衣服和配饰装到模型上的插件，不改动原文件，上传时自动合并。",
  vrcfury: "与 Modular Avatar 类似的装配插件，很多开关和互动类素材用它安装。",
  aao: "上传时合并网格、去掉用不到的部分，让模型更轻的插件。",
  base: "衣服、头发等素材所适配的基础模型。",
  prefab: "Unity 中预先做好的物体，放进场景即可使用。",
  menu: "在 VRChat 中打开的圆形菜单，用来切换衣服、表情等。",
  params: "菜单开关在 VRChat 中同步所占的数据，每个模型最多 256 位，超出无法上传。",
  faces: "模型由三角面组成，面数越多越耗性能。",
  slots: "模型上的每个材质占一个槽，槽越多越耗性能。",
  quest: "给 Quest 和手机玩家看的版本，限制比 PC 版严格。",
  rank: "VRChat 按面数、材质槽等给模型的性能评级，PC 和 Quest 各有一套标准。",
  alcom: "管理 VRChat 工程和插件的工具，可代替 VRChat Creator Companion。",
};
// the check-up's labels that are such terms (checkup.js)
const NB_TERM_OF = { "同步参数": "params", "PhysBone 组件": "physbone", "PhysBone 影响的骨骼": "physbone", "PhysBone 碰撞体": "physbone", "PhysBone 碰撞检测": "physbone",
  "三角面": "faces", "材质槽": "slots", "菜单单页的控件": "menu" };
// a term marked: html is the label as it is shown (escaped), whole, so its translation is the label's
function nbTerm(key, html) { return NB_TERMS[key] ? `<span class="term" data-term="${key}" tabindex="0">${html}</span>` : html; }
function nbTermLabel(label) { const k = NB_TERM_OF[label]; return k ? nbTerm(k, esc(label)) : esc(label); }
// One tooltip for every term (and for any element with data-term, such as a button): shown on hover and on keyboard
// focus, described by aria-describedby, gone with the pointer, the focus or Escape.
let nbTip = null, nbTipFor = null;
function nbTipShow(el) {
  const text = NB_TERMS[el.dataset.term]; if (!text) return;
  if (!nbTip) { nbTip = document.createElement("div"); nbTip.id = "nb_tip"; nbTip.className = "nbtip"; nbTip.setAttribute("role", "tooltip"); nbTip.hidden = true; document.body.append(nbTip); }
  if (nbTipFor && nbTipFor !== el) nbTipFor.removeAttribute("aria-describedby");
  nbTipFor = el; nbTip.textContent = text; nbTip.hidden = false;
  el.setAttribute("aria-describedby", "nb_tip");
  const r = el.getBoundingClientRect(), w = nbTip.offsetWidth, h = nbTip.offsetHeight;
  let y = r.bottom + 6;
  if (y + h > innerHeight - 8) y = Math.max(8, r.top - h - 6);
  nbTip.style.left = Math.max(8, Math.min(r.left, innerWidth - w - 8)) + "px"; nbTip.style.top = y + "px";
}
function nbTipHide(el) {
  if (!nbTip || (el && el !== nbTipFor)) return;
  nbTip.hidden = true;
  if (nbTipFor) nbTipFor.removeAttribute("aria-describedby");
  nbTipFor = null;
}
const nbTermAt = e => e.target && e.target.closest ? e.target.closest("[data-term]") : null;
document.addEventListener("mouseover", e => { const t = nbTermAt(e); if (t && t !== nbTipFor) nbTipShow(t); });
document.addEventListener("mouseout", e => { const t = nbTermAt(e); if (t && !(e.relatedTarget && t.contains(e.relatedTarget)) && document.activeElement !== t) nbTipHide(t); });
document.addEventListener("focusin", e => { const t = nbTermAt(e); if (t) nbTipShow(t); });
document.addEventListener("focusout", e => { const t = nbTermAt(e); if (t) nbTipHide(t); });
document.addEventListener("keydown", e => { if (e.key === "Escape") nbTipHide(); }, true);
// a scroll moves the tooltip with a focused term (focus scrolls it into view) and takes it away from a hovered one
document.addEventListener("scroll", () => { if (nbTipFor && document.activeElement === nbTipFor) nbTipShow(nbTipFor); else nbTipHide(); }, true);

document.addEventListener("click", e => {
  const b = e.target.closest("[data-nb]"); if (!b) return;
  switch (b.dataset.nb) {
    case "base": if (b.closest("#modal")?.dataset.kind === "ocbooth") closeModal(true); return nbOpenBase(+b.dataset.i);
    case "basename": if (b.closest("#modal")?.dataset.kind === "ocbooth") closeModal(true); return nbOpenBaseName(b.dataset.base);
    case "bases": return nbBasesOpen();
    case "close": $("#modal").dataset.kind = ""; return closeModal(true);
    case "mybases": return nbMineToggle();
    case "go": return nbGo(b.dataset.step);
    case "last": if (NB.job) { OC.job = NB.job; return ocJobOpen(); } return;
    case "mark": nbKeep(b.dataset.step, true); return nbCheckSync();
    case "dismiss":
      nbKeep("dismissed", true); nbCheckSync();
      return toast("已隐藏「新手任务」，点此撤销", 5000, () => { nbKeep("dismissed", false); nbCheckSync(); });
    case "ocundo": return ocUndoAsk();
    case "undogo": return ocUndoGo();
    case "undoback": return ocUndoBack();
  }
});
// 「我的模型」 coming into view (app.js applyView sets the page's class), and the library changing under it
new MutationObserver(nbCheckSync).observe(document.body, { attributes: true, attributeFilter: ["class"] });
setInterval(() => {
  if (nbTipFor && !nbTipFor.isConnected) nbTipHide();
  if (typeof S === "undefined" || !S.data || S.rev === NB.rev) return;
  NB.rev = S.rev;
  nbIntentCheck(); nbCheckSync();
}, 1000);
