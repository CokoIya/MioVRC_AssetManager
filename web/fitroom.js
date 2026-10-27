// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// the fitting room (1.8.0): the avatar in 3D — turned with the mouse, outfits switched on and off, outfits of the
// project tried on — drawn from the binary glTF Unity writes (Glb.cs). The scene is not changed by any of it, but
// for the colours (fitlook.go) and 「部件」: parts taken off the avatar (Parts.cs: left out of the upload, put back
// with 「恢复」), and where a part comes through the body so that it shows.
"use strict";

const FR = {
  session: 0, project: "", avatar: "", wear: [], tries: [],
  on: new Map(),     // path → worn (what the player switched)
  trying: new Map(), // folder → the prefab tried on (a colour of it, or its first)
  busy: false, err: "", stat: null, again: false, timer: 0, view: null, seq: 0,
  // the colour of what is worn: whose colour panel is open, the colour version each wears, the sliders, pictures
  look: "", variant: new Map(), tint: new Map(), thumbs: new Map(), looking: false,
  // 「部件」: the side panel's tab, the plugin's list, the paths picked to be taken off (hidden in the preview until
  // then), the items unfolded, the whole clipping list shown
  tab: "wear", parts: null, partsErr: "", partsBusy: false, pend: new Set(), unfold: new Set(), clipAll: false, setting: false, fixing: "",
};

async function frOpen(project, opt) {
  const session = ++FR.session;
  clearTimeout(FR.timer); FR.timer = 0;
  FR.busy = FR.again = FR.looking = FR.partsBusy = FR.setting = false; FR.fixing = "";
  FR.thumbs = new Map();
  if (FR.view) { FR.view.stop(); FR.view.clear(); FR.view.fit = null; }
  FR.project = project; FR.avatar = ""; FR.wear = []; FR.tries = []; FR.on = new Map(); FR.trying = new Map(); FR.err = ""; FR.stat = null;
  FR.look = ""; FR.variant = new Map(); FR.tint = new Map();
  FR.tab = opt && opt.tab === "parts" ? "parts" : "wear"; FR.parts = null; FR.partsErr = ""; FR.pend = new Set(); FR.unfold = new Set(); FR.clipAll = false;
  const m = $("#modal");
  m.dataset.kind = "fit";
  m.innerHTML = `<div class="mhead">3D 试衣间<span class="frav" id="fr_av"></span></div>
    <div class="mbody frbody"><div class="frside" id="fr_side"><div class="muted small">正在读取模型…</div></div>
      <div class="frstage"><canvas id="fr_cv" tabindex="0" aria-label="3D 预览：拖动旋转，滚轮缩放"></canvas>
        <div class="frbar"><button class="btn small" data-frv="front">正面</button><button class="btn small" data-frv="back">背面</button><button class="btn small" data-frv="left">左侧</button><button class="btn small" data-frv="right">右侧</button>
          <button class="btn small" data-fr="spin" aria-pressed="false">自动旋转</button><button class="btn small ghost" data-fr="again" title="Unity 中改动了模型后，重新生成">重新生成</button></div>
        <div class="frstat" id="fr_stat"></div><div class="frwait" id="fr_wait" hidden><i class="spin"></i><span>正在生成 3D 模型…</span></div></div></div>
    <div class="mfoot"><span class="muted small">拖动旋转 · 滚轮缩放 · 双击复位</span><span class="spacer"></span><button class="btn" data-fr="close">关闭</button></div>`;
  showModal();
  if (!FR.view) FR.view = new FitView();
  FR.view.attach($("#fr_cv"));
  let r;
  try { r = await api("/api/fit/options", { project }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!frCurrent(session, project)) return;
  if (!r.ok) { $("#fr_side").innerHTML = `<div class="small err">${esc(r.err || "无法读取模型")}</div>`; return; }
  FR.avatar = r.avatar || ""; FR.wear = r.wear || []; FR.tries = r.tries || [];
  for (const w of FR.wear) { FR.on.set(w.path, !!w.active); if (w.prefab) FR.variant.set(w.path, w.prefab); }
  // opened to try something on (照图配装): it is tried on from the start
  if (opt && opt.tryFolder) FR.tries.forEach((t, i) => { if (t.folder === opt.tryFolder && !FR.trying.size) FR.trying.set(t.folder + "|" + i, t.prefab); });
  $("#fr_av").textContent = FR.avatar;
  frSide();
  frExport();
  if (FR.tab === "parts") frPartsLoad();
}
function frOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "fit"; }
// Every opening has its own session, including closing and reopening the same project.
function frCurrent(session, project) { return session === FR.session && project === FR.project && frOpenNow(); }
// the colour panel of something worn: its colour versions as pictures, then a colour of the player's own
function frLookHTML(w) {
  const t = FR.tint.get(w.path) || { hue: 0, saturation: 1, value: 1 }, cur = FR.variant.get(w.path);
  const sw = (w.colours || []).map(p => { const src = FR.thumbs.get(p); return `<button class="frsw${cur === p ? " on" : ""}" data-frlook="variant" data-frprefab="${esc(p)}" title="${esc(p.split("/").pop().replace(/\.prefab$/i, ""))}" aria-label="${esc(p.split("/").pop().replace(/\.prefab$/i, ""))}"${FR.looking ? " disabled" : ""}>${src ? `<img src="${esc(src)}" alt="">` : ""}</button>`; }).join("");
  const range = (k, label, min, max, step) => `<label class="frslider"><span>${label}</span><input type="range" min="${min}" max="${max}" step="${step}" value="${t[k]}" data-frtint="${k}"></label>`;
  return `<div class="frlook">${sw ? `<div class="frsws">${sw}</div>` : ""}
    ${range("hue", "色相", -0.5, 0.5, 0.01)}${range("saturation", "饱和度", 0, 2, 0.05)}${range("value", "明度", 0, 2, 0.05)}
    <div class="frlookbtns"><button class="btn small" data-frlook="tint"${FR.looking ? " disabled" : ""}>应用颜色</button><button class="btn small ghost" data-frlook="reset"${FR.looking ? " disabled" : ""}>恢复</button></div></div>`;
}
async function frThumbs(w) {
  const { session, project } = FR;
  if (!frCurrent(session, project)) return;
  const want = (w.colours || []).filter(p => !FR.thumbs.has(p));
  if (!want.length) return;
  let r; try { r = await api("/api/fit/thumbs", { project, prefabs: want }); } catch (e) { return; }
  if (!r.ok || !frCurrent(session, project)) return;
  for (const [p, u] of Object.entries(r.thumbs || {})) FR.thumbs.set(p, u);
  frSide();
}
async function frLook(action, prefab) {
  const { session, project } = FR;
  if (!frCurrent(session, project)) return;
  const w = FR.wear.find(x => x.path === FR.look); if (!w || FR.looking) return;
  const t = FR.tint.get(w.path) || { hue: 0, saturation: 1, value: 1 };
  FR.looking = true; frSide();
  let r; try { r = await api("/api/fit/look", Object.assign({ project, avatar: FR.avatar, outfit: w.path, action, prefab: prefab || "" }, t)); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!frCurrent(session, project)) return;
  FR.looking = false;
  if (!r.ok) { toast(r.err || "未能更改颜色", 5000); frSide(); return; }
  if (action === "variant") FR.variant.set(w.path, prefab);
  if (action === "reset") { FR.variant.set(w.path, w.prefab); FR.tint.delete(w.path); }
  if (!r.saved) toast("已更改，请在 Unity 中保存场景", 4000);
  frSide(); frExport();
}
// ---------- 部件 ----------
// where on the body a part comes through (Parts.cs: "face", or the humanoid bone nearest)
const FR_WHERE = { face: "脸部", Head: "头部", Neck: "颈部", Chest: "胸部", UpperChest: "胸部", Spine: "腰腹", Hips: "臀部",
  LeftShoulder: "左肩", RightShoulder: "右肩", LeftUpperArm: "左上臂", RightUpperArm: "右上臂", LeftLowerArm: "左前臂", RightLowerArm: "右前臂",
  LeftHand: "左手", RightHand: "右手", LeftUpperLeg: "左大腿", RightUpperLeg: "右大腿", LeftLowerLeg: "左小腿", RightLowerLeg: "右小腿", LeftFoot: "左脚", RightFoot: "右脚" };
function frWhere(w) { return FR_WHERE[w] || "身体"; }
async function frPartsLoad() {
  const { session, project } = FR;
  if (!frCurrent(session, project)) return;
  if (FR.partsBusy) return;
  FR.partsBusy = true; FR.partsErr = ""; frSide();
  let r; try { r = await api("/api/fit/parts", { project, avatar: FR.avatar }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!frCurrent(session, project)) return;
  FR.partsBusy = false;
  if (r.ok) FR.parts = r; else FR.partsErr = r.err || "无法读取部件";
  frSide();
}
// taken off now (the item or the part itself): a removed item's parts go with it
function frGone(item, part) { return item.removed || (part && part.removed); }
function frPending(item, part) { return FR.pend.has(item.path) || (part && FR.pend.has(part.path)); }
function frClipHTML() {
  const clips = ((FR.parts || {}).clipping || []).slice().sort((a, b) => (b.where === "face") - (a.where === "face") || b.shows - a.shows);
  if (!clips.length) return "";
  const shown = FR.clipAll ? clips : clips.slice(0, 5);
  const busy = FR.setting || !!FR.fixing, items = (FR.parts || {}).items || [];
  return `<h5>穿模<span class="frn">${clips.length}</span></h5>${shown.map(c => {
    const on = FR.pend.has(c.path) || FR.pend.has(c.item);
    // clothes going into the body: the body comes through them (hidden under them while worn); the rest, and
    // anything in the face (a charm of an outfit too): moved out a little
    const it = items.find(x => x.path === c.item), cloth = !!it && it.kind === "衣服" && c.where !== "face", target = cloth ? c.item : c.path;
    const fix = `<button class="btn small ghost" data-frfix="${esc(target)}" data-frkind="${cloth ? "clip" : "nudge"}" title="${cloth ? "穿着这件衣服时隐藏被盖住的身体，没有改善会自动还原" : "把它往外移几毫米，没有改善会自动还原"}"${busy ? " disabled" : ""}>${FR.fixing === target ? `<i class="spin"></i>` : ""}${cloth ? "修复" : "移开"}</button>`;
    return `<div class="frclip${c.where === "face" ? " face" : ""}${c.item === c.path ? " one" : ""}"><span class="frwhere">${esc(frWhere(c.where))}</span><span class="frcn" title="${esc(c.path)}">${esc(c.name)}</span>${c.item !== c.path ? `<span class="frci" data-i18n="off">${esc(c.item)}</span>` : ""}
      <span class="frcb">${fix}<button class="btn small${on ? " ghost" : ""}" data-frpick="${esc(c.path)}"${busy ? " disabled" : ""}>${on ? "撤销" : "移除"}</button></span></div>`;
  }).join("")}${clips.length > shown.length ? `<button class="linkbtn frmore" data-frclipall="1">更多 ${clips.length - shown.length} 项</button>` : ""}`;
}
function frPartRow(it, p) {
  const gone = frGone(it, p), clip = p.clip && p.clip.where;
  return `<div class="frsub${gone ? " gone" : ""}">${gone && !it.removed ? `<span class="frtag">已移除</span>` : `<input type="checkbox" data-frpp="${esc(p.path)}"${frPending(it, p) || gone ? "" : " checked"}${gone || FR.setting || FR.pend.has(it.path) ? " disabled" : ""} aria-label="${esc(p.name)}">`}
    <span class="frpn" title="${esc(p.path)}">${esc(p.name)}</span>${p.on || gone ? "" : `<span class="frtag">默认隐藏</span>`}${clip ? `<span class="frtag warn">穿模·${esc(frWhere(p.clip.where))}</span>` : ""}
    <span class="frtris" data-i18n="off">${esc(fmtNum(p.tris || 0))}</span>${gone && !it.removed ? `<button class="linkbtn" data-frrestore="${esc(p.path)}"${FR.setting ? " disabled" : ""}>恢复</button>` : ""}</div>`;
}
function frItemRow(it) {
  const parts = it.parts || [], clips = parts.filter(p => p.clip && p.clip.where).length, open = FR.unfold.has(it.path);
  const tris = parts.reduce((n, p) => n + (frGone(it, p) ? 0 : p.tris || 0), 0);
  const head = it.removed
    ? `<span class="frin"><span title="${esc(it.path)}">${esc(it.name)}</span></span><span class="frtag">已移除</span>`
    : `<label class="frin"><input type="checkbox" data-frpi="${esc(it.path)}"${FR.pend.has(it.path) ? "" : " checked"}${FR.setting ? " disabled" : ""}><span title="${esc(it.path)}">${esc(it.name)}</span></label>`;
  return `<div class="fritem${it.removed ? " gone" : ""}${FR.pend.has(it.path) ? " pend" : ""}"><div class="frrow">${head}${it.kind ? `<span class="frtag">${esc(it.kind)}</span>` : ""}${it.on || it.removed ? "" : `<span class="frtag">默认隐藏</span>`}${clips ? `<span class="frtag warn">穿模</span>` : ""}
      ${it.removed ? `<button class="linkbtn" data-frrestore="${esc(it.path)}"${FR.setting ? " disabled" : ""}>恢复</button>` : it.single ? `<span class="frtris" data-i18n="off">${esc(fmtNum(tris))}</span>` : `<button class="frexp" data-frexp="${esc(it.path)}" aria-expanded="${open}">${parts.length} 个部件</button>`}</div>
    ${open && !it.single ? `<div class="frsubs">${parts.map(p => frPartRow(it, p)).join("")}</div>` : ""}</div>`;
}
function frPartsHTML() {
  if (!FR.parts) return FR.partsErr ? `<div class="small err">${esc(FR.partsErr)}</div><button class="btn small" data-frparts="reload">重试</button>`
    : `<div class="muted small frloading"><i class="spin"></i>正在读取部件…</div>`;
  const items = FR.parts.items || [];
  if (!items.length) return `<div class="muted small">模型上没有可移除的部件。</div>`;
  const own = items.filter(it => it.own), base = items.filter(it => !it.own);
  return frClipHTML() + (own.length ? `<h5>穿上的</h5>${own.map(frItemRow).join("")}` : "") + (base.length ? `<h5>素体自带</h5>${base.map(frItemRow).join("")}` : "")
    + (FR.pend.size ? `<div class="frpend"><span>移除 ${FR.pend.size} 项</span><span class="spacer"></span><button class="btn small ghost" data-frpend="cancel"${FR.setting ? " disabled" : ""}>取消</button>
      <button class="btn small primary" data-frpend="apply" title="移除的部件不会上传，可随时恢复"${FR.setting ? " disabled" : ""}>${FR.setting ? `<i class="spin"></i>` : ""}移除所选</button></div>` : "");
}
// a box of an item or a part, or a 「移除」 of the clipping list: picked or unpicked (the preview follows)
function frPick(path, off) {
  const it = ((FR.parts || {}).items || []).find(x => x.path === path);
  if (off) {
    FR.pend.add(path);
    if (it) for (const p of it.parts || []) FR.pend.delete(p.path); // (the whole item: its parts go with it)
  } else FR.pend.delete(path);
  frSide(); frExport();
}
// 修复 / 移开 of the clipping list: done in Unity (one undo step each), then the list measured again
async function frFix(kind, path) {
  const { session, project } = FR;
  if (!frCurrent(session, project)) return;
  if (FR.setting || FR.fixing) return;
  FR.fixing = path; frSide();
  let r; try { r = await api("/api/fit/parts/fix", { project, avatar: FR.avatar, kind, path }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!frCurrent(session, project)) return;
  FR.fixing = "";
  if (!r.ok) { toast(r.err || "未能完成", 5000); frSide(); return; }
  toast(r.note + (r.fixed && !r.saved ? "。请在 Unity 中保存场景" : ""), 6000);
  if (r.fixed) { FR.parts = null; frPartsLoad(); frExport(); } else frSide();
}
async function frPartsSet(remove, restore) {
  const { session, project } = FR;
  if (!frCurrent(session, project)) return;
  if (FR.setting) return;
  FR.setting = true; frSide();
  let r; try { r = await api("/api/fit/parts/set", { project, avatar: FR.avatar, remove, restore }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!frCurrent(session, project)) return;
  FR.setting = false;
  if (!r.ok) { toast(r.err || "未能完成", 5000); frSide(); return; }
  if (remove.length) FR.pend = new Set();
  toast((remove.length ? `已移除 ${r.removed || 0} 项` : "已恢复") + (r.saved ? "" : "，请在 Unity 中保存场景"), 4000);
  FR.parts = null; frPartsLoad(); frExport();
}
function frSide() {
  const tabs = `<div class="seg frtabs" role="tablist"><button class="segbtn${FR.tab === "wear" ? " on" : ""}" role="tab" aria-selected="${FR.tab === "wear"}" data-frtab="wear">穿搭</button><button class="segbtn${FR.tab === "parts" ? " on" : ""}" role="tab" aria-selected="${FR.tab === "parts"}" data-frtab="parts">部件</button></div>`;
  if (FR.tab === "parts") { $("#fr_side").innerHTML = tabs + frPartsHTML(); return; }
  const off = new Set((((FR.parts || {}).items) || []).filter(it => it.removed).map(it => it.path)); // (taken off in 「部件」)
  const wear = FR.wear.length ? FR.wear.filter(w => !off.has(w.path)).map(w => `<div class="frworn${FR.look === w.path ? " open" : ""}"><div class="frrow"><label><input type="checkbox" data-frw="${esc(w.path)}"${FR.on.get(w.path) ? " checked" : ""}><span>${esc(w.name)}</span></label>${w.active ? "" : `<span class="frtag">默认隐藏</span>`}
      ${w.kind && w.kind !== "outfit" ? "" : `<button class="btn small ghost frlookbtn" data-frlookopen="${esc(w.path)}" aria-expanded="${FR.look === w.path}">颜色</button>`}</div>${FR.look === w.path ? frLookHTML(w) : ""}</div>`).join("")
    : `<div class="muted small">模型上没有用 Modular Avatar 穿上的衣服。</div>`;
  const tries = FR.tries.length ? FR.tries.map((t, i) => {
    const cur = FR.trying.get(t.folder + "|" + i), all = [t.prefab].concat(t.colours || []);
    const cols = all.length > 1 ? `<div class="frcols">${all.map((p, j) => `<button class="frcol${cur === p ? " on" : ""}" data-frt="${i}" data-frp="${esc(p)}" title="${esc(p)}">${esc(j === 0 ? "原色" : p.split("/").pop().replace(/\.prefab$/i, ""))}</button>`).join("")}</div>` : "";
    return `<div class="frtry${cur ? " on" : ""}"><label class="frrow"><input type="checkbox" data-frt="${i}"${cur ? " checked" : ""}><span>${esc(t.name)}</span><span class="frtag">${esc(t.kind)}</span></label>${cur ? cols : ""}</div>`;
  }).join("") : `<div class="muted small">工程中没有可试穿的衣服、头发或配饰（已导入、尚未穿上的）。</div>`;
  $("#fr_side").innerHTML = tabs + `<h5>身上的</h5>${wear}<h5>试穿</h5><div class="frhint">工程中已导入、还没穿上的素材，临时放到模型上看看</div>${tries}
    <div class="frnote">预览不显示物理与形态键变化。</div>`;
}
function frState() {
  const show = [], hide = [], tr = [];
  for (const w of FR.wear) {
    const on = FR.on.get(w.path);
    if (on && !w.active) show.push(w.path);
    if (!on && w.active) hide.push(w.path);
  }
  for (const p of FR.trying.values()) tr.push(p);
  for (const p of FR.pend) if (!hide.includes(p)) hide.push(p); // (picked to be taken off: seen without it first)
  return { show, hide, try: tr };
}
// what is asked for, a moment after the last change (several boxes ticked in a row make one file)
function frExport() {
  clearTimeout(FR.timer);
  const { session, project } = FR;
  FR.timer = setTimeout(() => {
    if (!frCurrent(session, project)) return;
    FR.timer = 0; frExportNow();
  }, 250);
}
async function frExportNow() {
  const { session, project } = FR;
  if (!frCurrent(session, project) || !FR.avatar) return;
  if (FR.busy) { FR.again = true; return; }
  FR.busy = true; FR.err = ""; frWait(true);
  const seq = ++FR.seq, st = frState();
  let r;
  try { r = await api("/api/fit/export", Object.assign({ project, avatar: FR.avatar }, st)); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  const current = () => frCurrent(session, project) && seq === FR.seq;
  // (an answer no longer wanted still frees the room for the next one: else every later change only waits)
  const drop = () => { if (seq === FR.seq) { FR.busy = false; frWait(false); } };
  if (!current()) return drop();
  if (r.ok) {
    try { await FR.view.load(r.url, current); } catch (e) { r = { ok: false, err: "3D 模型无法显示：" + (e && e.message || e) }; }
  }
  if (!current()) return drop();
  if (r.ok) FR.stat = r;
  FR.busy = false;
  if (!r.ok) FR.err = r.err || "未能生成 3D 模型";
  frWait(false); frStat();
  if (FR.again) { FR.again = false; frExport(); }
}
function frWait(on) { const w = $("#fr_wait"); if (w) w.hidden = !on; }
function frStat() {
  const el = $("#fr_stat"); if (!el) return;
  if (FR.err) { el.innerHTML = `<span class="err">${esc(FR.err)}</span>`; return; }
  const s = FR.stat; if (!s) { el.textContent = ""; return; }
  el.innerHTML = `${esc(fmtNum(s.triangles || 0))} 个三角面 · ${esc(s.meshes || 0)} 个网格 · ${esc(s.textures || 0)} 张贴图` +
    ((s.skipped || []).length ? ` · <span class="warnline" title="${esc(s.skipped.join("\n"))}">${esc(s.skipped.length)} 个网格未能导出</span>` : "");
}
function fmtNum(n) { return Number(n).toLocaleString("en-US"); }

document.addEventListener("change", e => {
  if (!frOpenNow()) return;
  const t = e.target;
  if (t.dataset.frw !== undefined) { FR.on.set(t.dataset.frw, t.checked); frExport(); return; }
  if (t.dataset.frpi !== undefined) { frPick(t.dataset.frpi, !t.checked); return; }
  if (t.dataset.frpp !== undefined) { frPick(t.dataset.frpp, !t.checked); return; }
  if (t.dataset.frtint !== undefined) { const v = Object.assign({ hue: 0, saturation: 1, value: 1 }, FR.tint.get(FR.look)); v[t.dataset.frtint] = +t.value; FR.tint.set(FR.look, v); return; }
  if (t.dataset.frt !== undefined && t.type === "checkbox") {
    const i = +t.dataset.frt, tr = FR.tries[i]; if (!tr) return;
    const k = tr.folder + "|" + i;
    if (t.checked) FR.trying.set(k, tr.prefab); else FR.trying.delete(k);
    frSide(); frExport();
  }
});
document.addEventListener("click", e => {
  const go = e.target.closest("[data-fitroom]");
  if (go) { if (AI.proj) frOpen(AI.proj.path); return; }
  if (!frOpenNow()) return;
  const tb = e.target.closest("[data-frtab]");
  if (tb) { FR.tab = tb.dataset.frtab; frSide(); if (FR.tab === "parts" && !FR.parts) frPartsLoad(); return; }
  const pk = e.target.closest("[data-frpick]");
  if (pk) { frPick(pk.dataset.frpick, !FR.pend.has(pk.dataset.frpick)); return; }
  const ex = e.target.closest("[data-frexp]");
  if (ex) { const p = ex.dataset.frexp; if (FR.unfold.has(p)) FR.unfold.delete(p); else FR.unfold.add(p); frSide(); return; }
  if (e.target.closest("[data-frclipall]")) { FR.clipAll = true; frSide(); return; }
  const fx = e.target.closest("[data-frfix]");
  if (fx) { frFix(fx.dataset.frkind, fx.dataset.frfix); return; }
  const rs = e.target.closest("[data-frrestore]");
  if (rs) { frPartsSet([], [rs.dataset.frrestore]); return; }
  const pd = e.target.closest("[data-frpend]");
  if (pd) { if (pd.dataset.frpend === "apply") frPartsSet([...FR.pend], []); else { FR.pend = new Set(); frSide(); frExport(); } return; }
  if (e.target.closest("[data-frparts]")) { frPartsLoad(); return; }
  const lo = e.target.closest("[data-frlookopen]");
  if (lo) { FR.look = FR.look === lo.dataset.frlookopen ? "" : lo.dataset.frlookopen; frSide(); const w = FR.wear.find(x => x.path === FR.look); if (w) frThumbs(w); return; }
  const la = e.target.closest("[data-frlook]");
  if (la) { frLook(la.dataset.frlook, la.dataset.frprefab); return; }
  const b = e.target.closest("[data-fr],[data-frv],[data-frp]");
  if (!b) return;
  if (b.dataset.frp !== undefined) {
    const i = +b.dataset.frt, tr = FR.tries[i]; if (!tr) return;
    FR.trying.set(tr.folder + "|" + i, b.dataset.frp); frSide(); frExport(); return;
  }
  if (b.dataset.frv) { FR.view.face(b.dataset.frv); return; }
  if (b.dataset.fr === "close") { FR.view.stop(); closeModal(); return; }
  if (b.dataset.fr === "again") { frExport(); return; }
  if (b.dataset.fr === "spin") { const on = FR.view.spin(); b.setAttribute("aria-pressed", on ? "true" : "false"); b.classList.toggle("on", on); }
});

// ---------- drawing a binary glTF: meshes, their main textures, unlit with a little light ----------
const FR_VS = `#version 300 es
in vec3 aPos; in vec3 aNor; in vec2 aUV;
uniform mat4 uVP;
out vec3 vNor; out vec2 vUV;
void main() { vNor = aNor; vUV = aUV; gl_Position = uVP * vec4(aPos, 1.0); }`;
const FR_FS = `#version 300 es
precision highp float;
in vec3 vNor; in vec2 vUV;
uniform sampler2D uTex; uniform vec4 uColor; uniform int uHasTex; uniform int uMode; uniform float uCut; uniform vec3 uLight;
out vec4 o;
void main() {
  vec4 c = uColor;
  if (uHasTex == 1) c *= texture(uTex, vUV);
  if (uMode == 1 && c.a < uCut) discard;
  vec3 n = normalize(gl_FrontFacing ? vNor : -vNor);
  float l = dot(n, normalize(uLight));
  float shade = mix(0.78, 1.0, smoothstep(-0.05, 0.25, l)); // two tones, as a toon shader lights it
  o = vec4(c.rgb * shade, uMode == 2 ? c.a : 1.0);
}`;

class FitView {
  constructor() { this.cv = null; this.gl = null; this.prims = []; this.yaw = 0; this.pitch = 0.08; this.dist = 3; this.target = [0, 1, 0]; this.fit = null; this.spinning = false; this.raf = 0; this.dirty = true; }
  attach(cv) {
    this.cv = cv;
    const gl = cv.getContext("webgl2", { antialias: true, alpha: true, premultipliedAlpha: false });
    if (!gl) throw new Error("WebGL2 不可用");
    if (this.gl !== gl) { this.gl = gl; this.prims = []; this.prog = this.program(); this.aniso = gl.getExtension("EXT_texture_filter_anisotropic"); }
    this.bind(); this.loop();
  }
  program() {
    const gl = this.gl, sh = (t, src) => { const s = gl.createShader(t); gl.shaderSource(s, src); gl.compileShader(s); if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) throw new Error(gl.getShaderInfoLog(s)); return s; };
    const p = gl.createProgram();
    gl.attachShader(p, sh(gl.VERTEX_SHADER, FR_VS)); gl.attachShader(p, sh(gl.FRAGMENT_SHADER, FR_FS));
    gl.bindAttribLocation(p, 0, "aPos"); gl.bindAttribLocation(p, 1, "aNor"); gl.bindAttribLocation(p, 2, "aUV");
    gl.linkProgram(p);
    if (!gl.getProgramParameter(p, gl.LINK_STATUS)) throw new Error(gl.getProgramInfoLog(p));
    const u = n => gl.getUniformLocation(p, n);
    this.u = { vp: u("uVP"), tex: u("uTex"), color: u("uColor"), has: u("uHasTex"), mode: u("uMode"), cut: u("uCut"), light: u("uLight") };
    return p;
  }
  bind() {
    const cv = this.cv;
    if (cv._fr) return;
    cv._fr = true;
    let drag = null;
    cv.addEventListener("pointerdown", e => { drag = { x: e.clientX, y: e.clientY, pan: e.button === 2 || e.shiftKey }; cv.setPointerCapture(e.pointerId); this.spinning = false; });
    cv.addEventListener("pointermove", e => {
      if (!drag) return;
      const dx = e.clientX - drag.x, dy = e.clientY - drag.y; drag.x = e.clientX; drag.y = e.clientY;
      if (drag.pan) {
        const s = this.dist * 0.0018, cy = Math.cos(this.yaw), sy = Math.sin(this.yaw);
        this.target[0] -= dx * s * cy; this.target[2] += dx * s * sy; this.target[1] += dy * s;
      } else { this.yaw -= dx * 0.008; this.pitch = Math.max(-1.45, Math.min(1.45, this.pitch + dy * 0.006)); }
      this.dirty = true;
    });
    const up = () => { drag = null; };
    cv.addEventListener("pointerup", up); cv.addEventListener("pointercancel", up);
    cv.addEventListener("contextmenu", e => e.preventDefault());
    cv.addEventListener("wheel", e => { e.preventDefault(); this.dist = Math.max(0.15, Math.min(40, this.dist * Math.exp(e.deltaY * 0.0012))); this.dirty = true; }, { passive: false });
    cv.addEventListener("dblclick", () => this.reset());
    cv.addEventListener("keydown", e => {
      const k = { ArrowLeft: [0.15, 0], ArrowRight: [-0.15, 0], ArrowUp: [0, -0.1], ArrowDown: [0, 0.1] }[e.key];
      if (k) { e.preventDefault(); this.yaw += k[0]; this.pitch = Math.max(-1.45, Math.min(1.45, this.pitch + k[1])); this.dirty = true; }
    });
  }
  loop() {
    cancelAnimationFrame(this.raf);
    const tick = () => {
      if (!this.cv || !this.cv.isConnected) { this.raf = 0; return; }
      if (this.spinning) { this.yaw += 0.006; this.dirty = true; }
      if (this.dirty) { this.dirty = false; this.draw(); }
      this.raf = requestAnimationFrame(tick);
    };
    this.raf = requestAnimationFrame(tick);
  }
  stop() { cancelAnimationFrame(this.raf); this.raf = 0; this.spinning = false; }
  spin() { this.spinning = !this.spinning; this.dirty = true; return this.spinning; }
  face(v) { this.yaw = { front: 0, back: Math.PI, left: -Math.PI / 2, right: Math.PI / 2 }[v] || 0; this.pitch = 0.08; this.spinning = false; this.dirty = true; }
  reset() {
    if (!this.fit) return;
    const [lo, hi] = this.fit, h = hi[1] - lo[1], w = Math.max(hi[0] - lo[0], hi[2] - lo[2]);
    this.target = [(lo[0] + hi[0]) / 2, lo[1] + h * 0.52, (lo[2] + hi[2]) / 2];
    this.dist = Math.max(h, w) / (2 * Math.tan(0.5 * 0.5)) * 1.12; this.yaw = 0; this.pitch = 0.08; this.dirty = true;
  }
  clear() {
    const gl = this.gl;
    for (const p of this.prims) { gl.deleteVertexArray(p.vao); }
    for (const b of this.bufs || []) gl.deleteBuffer(b);
    for (const t of this.texs || []) gl.deleteTexture(t);
    this.prims = []; this.bufs = []; this.texs = [];
  }
  async load(url, current = () => true) {
    const res = await fetch(url);
    if (!current()) return;
    if (!res.ok) throw new Error("HTTP " + res.status);
    const buf = await res.arrayBuffer();
    if (!current()) return;
    const dv = new DataView(buf);
    if (dv.getUint32(0, true) !== 0x46546c67 || dv.getUint32(4, true) !== 2) throw new Error("不是 glTF 2.0 文件");
    let off = 12, json = null, bin = null;
    while (off + 8 <= buf.byteLength) {
      const len = dv.getUint32(off, true), type = dv.getUint32(off + 4, true);
      if (type === 0x4e4f534a) json = JSON.parse(new TextDecoder().decode(new Uint8Array(buf, off + 8, len)));
      else if (type === 0x004e4942) bin = new Uint8Array(buf, off + 8, len);
      off += 8 + len;
    }
    if (!json || !bin) throw new Error("文件不完整");
    const gl = this.gl;
    const view = i => { const v = json.bufferViews[i]; return new Uint8Array(bin.buffer, bin.byteOffset + (v.byteOffset || 0), v.byteLength); };
    const sizes = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4 };
    // the textures first (decoded off the main thread), then the buffers
    const texs = await Promise.all((json.textures || []).map(async t => {
      const img = json.images[t.source], bmp = await createImageBitmap(new Blob([view(img.bufferView)], { type: img.mimeType }), { premultiplyAlpha: "none", colorSpaceConversion: "none" });
      if (!current()) { if (bmp.close) bmp.close(); return null; }
      const tex = gl.createTexture();
      gl.bindTexture(gl.TEXTURE_2D, tex);
      gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, bmp);
      gl.generateMipmap(gl.TEXTURE_2D);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      if (this.aniso) gl.texParameterf(gl.TEXTURE_2D, this.aniso.TEXTURE_MAX_ANISOTROPY_EXT, 8);
      if (bmp.close) bmp.close();
      return tex;
    }));
    if (!current()) { for (const t of texs) if (t) gl.deleteTexture(t); return; }
    this.clear();
    this.texs = texs;
    const glBuf = new Map(); // accessor → its buffer (the meshes' primitives share their vertices)
    const arrayOf = (ai, target) => {
      if (glBuf.has(ai)) return glBuf.get(ai);
      const a = json.accessors[ai], v = json.bufferViews[a.bufferView], n = a.count * sizes[a.type], bytes = n * 4;
      const data = new Uint8Array(bin.buffer, bin.byteOffset + (v.byteOffset || 0) + (a.byteOffset || 0), bytes);
      const b = gl.createBuffer(); gl.bindBuffer(target, b); gl.bufferData(target, data, gl.STATIC_DRAW);
      this.bufs.push(b); glBuf.set(ai, b);
      return b;
    };
    const srgb = x => Math.pow(Math.max(0, x), 1 / 2.2); // the factors are linear; the textures and the screen are not
    let lo = [Infinity, Infinity, Infinity], hi = [-Infinity, -Infinity, -Infinity];
    for (const node of json.nodes || []) {
      const mesh = json.meshes[node.mesh]; if (!mesh) continue;
      for (const pr of mesh.primitives) {
        const at = pr.attributes, pos = json.accessors[at.POSITION];
        if (pos.min) { for (let k = 0; k < 3; k++) { lo[k] = Math.min(lo[k], pos.min[k]); hi[k] = Math.max(hi[k], pos.max[k]); } }
        const vao = gl.createVertexArray(); gl.bindVertexArray(vao);
        [["POSITION", 0, 3], ["NORMAL", 1, 3], ["TEXCOORD_0", 2, 2]].forEach(([name, loc, size]) => {
          if (at[name] === undefined) return;
          gl.bindBuffer(gl.ARRAY_BUFFER, arrayOf(at[name], gl.ARRAY_BUFFER));
          gl.enableVertexAttribArray(loc); gl.vertexAttribPointer(loc, size, gl.FLOAT, false, 0, 0);
        });
        const ia = json.accessors[pr.indices];
        gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, arrayOf(pr.indices, gl.ELEMENT_ARRAY_BUFFER));
        gl.bindVertexArray(null);
        const m = (json.materials || [])[pr.material] || {}, pbr = m.pbrMetallicRoughness || {}, f = pbr.baseColorFactor || [1, 1, 1, 1];
        const mode = { MASK: 1, BLEND: 2 }[m.alphaMode] || 0;
        const c = pos.min ? [(pos.min[0] + pos.max[0]) / 2, (pos.min[1] + pos.max[1]) / 2, (pos.min[2] + pos.max[2]) / 2] : [0, 0, 0];
        this.prims.push({ vao, count: ia.count, color: [srgb(f[0]), srgb(f[1]), srgb(f[2]), f[3]], tex: pbr.baseColorTexture ? texs[pbr.baseColorTexture.index] : null,
          mode, cut: m.alphaCutoff === undefined ? 0.5 : m.alphaCutoff, two: !!m.doubleSided, centre: c });
      }
    }
    const first = !this.fit;
    if (isFinite(lo[0])) this.fit = [lo, hi];
    if (first) this.reset();
    this.dirty = true;
  }
  draw() {
    const gl = this.gl, cv = this.cv;
    const dpr = Math.min(2, window.devicePixelRatio || 1), w = Math.max(1, Math.round(cv.clientWidth * dpr)), h = Math.max(1, Math.round(cv.clientHeight * dpr));
    if (cv.width !== w || cv.height !== h) { cv.width = w; cv.height = h; }
    gl.viewport(0, 0, w, h);
    gl.clearColor(0, 0, 0, 0); gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
    if (!this.prims.length) return;
    const cp = Math.cos(this.pitch), eye = [this.target[0] + this.dist * cp * Math.sin(this.yaw), this.target[1] + this.dist * Math.sin(this.pitch), this.target[2] + this.dist * cp * Math.cos(this.yaw)];
    const vp = frMul(frPersp(0.5, w / h, Math.max(0.01, this.dist * 0.02), this.dist * 20), frLookAt(eye, this.target));
    gl.useProgram(this.prog);
    gl.uniformMatrix4fv(this.u.vp, false, vp);
    gl.uniform3f(this.u.light, eye[0] - this.target[0] + 0.6 * this.dist, eye[1] - this.target[1] + this.dist, eye[2] - this.target[2]);
    gl.uniform1i(this.u.tex, 0); gl.activeTexture(gl.TEXTURE0);
    gl.enable(gl.DEPTH_TEST); gl.depthFunc(gl.LEQUAL); gl.frontFace(gl.CCW);
    const one = p => {
      if (p.two) gl.disable(gl.CULL_FACE); else { gl.enable(gl.CULL_FACE); gl.cullFace(gl.BACK); }
      gl.uniform4fv(this.u.color, p.color); gl.uniform1i(this.u.mode, p.mode); gl.uniform1f(this.u.cut, p.cut);
      gl.uniform1i(this.u.has, p.tex ? 1 : 0); gl.bindTexture(gl.TEXTURE_2D, p.tex);
      gl.bindVertexArray(p.vao); gl.drawElements(gl.TRIANGLES, p.count, gl.UNSIGNED_INT, 0);
    };
    gl.disable(gl.BLEND); gl.depthMask(true);
    for (const p of this.prims) if (p.mode !== 2) one(p);
    // what blends: the farthest first, without writing depth
    const d = p => (p.centre[0] - eye[0]) ** 2 + (p.centre[1] - eye[1]) ** 2 + (p.centre[2] - eye[2]) ** 2;
    const blend = this.prims.filter(p => p.mode === 2).sort((a, b) => d(b) - d(a));
    if (blend.length) {
      gl.enable(gl.BLEND); gl.blendFuncSeparate(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA, gl.ONE, gl.ONE_MINUS_SRC_ALPHA); gl.depthMask(false);
      for (const p of blend) one(p);
      gl.depthMask(true); gl.disable(gl.BLEND);
    }
    gl.bindVertexArray(null);
  }
}
// column-major 4×4 matrices, as WebGL takes them
function frPersp(fovy, aspect, near, far) {
  const f = 1 / Math.tan(fovy / 2), nf = 1 / (near - far);
  return new Float32Array([f / aspect, 0, 0, 0, 0, f, 0, 0, 0, 0, (far + near) * nf, -1, 0, 0, 2 * far * near * nf, 0]);
}
function frLookAt(eye, at) {
  let z = [eye[0] - at[0], eye[1] - at[1], eye[2] - at[2]];
  const zl = Math.hypot(...z) || 1; z = z.map(v => v / zl);
  let x = [z[2], 0, -z[0]]; // up × z with up = (0, 1, 0)
  const xl = Math.hypot(...x) || 1; x = x.map(v => v / xl);
  const y = [z[1] * x[2] - z[2] * x[1], z[2] * x[0] - z[0] * x[2], z[0] * x[1] - z[1] * x[0]];
  return new Float32Array([x[0], y[0], z[0], 0, x[1], y[1], z[1], 0, x[2], y[2], z[2], 0,
    -(x[0] * eye[0] + x[1] * eye[1] + x[2] * eye[2]), -(y[0] * eye[0] + y[1] * eye[1] + y[2] * eye[2]), -(z[0] * eye[0] + z[1] * eye[1] + z[2] * eye[2]), 1]);
}
function frMul(a, b) {
  const o = new Float32Array(16);
  for (let c = 0; c < 4; c++) for (let r = 0; r < 4; r++) {
    let s = 0;
    for (let k = 0; k < 4; k++) s += a[k * 4 + r] * b[c * 4 + k];
    o[c * 4 + r] = s;
  }
  return o;
}
