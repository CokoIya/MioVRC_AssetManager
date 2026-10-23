// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// the project's time machine (1.8.0): the avatar as it was before each run of the line and of the AI (and whenever
// the player keeps one), with a picture of it — put back with a click (the avatar of now switched off beside it,
// nothing deleted, one undo step in Unity), and what was imported since moved to the Recycle Bin if the player wants
"use strict";

const TM = { project: "", shots: [], dir: "", ready: true, sel: "", diff: null, diffFor: "", busy: "", err: "", done: null, label: "", drop: {}, loaded: false };

function tmOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "tm"; }
async function tmOpen(project) {
  Object.assign(TM, { project, shots: [], sel: "", diff: null, diffFor: "", busy: "", err: "", done: null, label: "", drop: {}, loaded: false });
  $("#modal").dataset.kind = "tm";
  tmDraw(true); showModal();
  await tmList();
}
async function tmList() {
  let r;
  try { r = await api("/api/tm/list", { project: TM.project }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!tmOpenNow()) return;
  TM.loaded = true;
  if (!r.ok) { TM.err = r.err || "无法读取时光机"; return tmDraw(); }
  TM.shots = r.shots || []; TM.dir = r.dir || ""; TM.ready = r.ready !== false;
  if (!TM.shots.some(s => s.id === TM.sel)) { TM.sel = TM.shots.length ? TM.shots[0].id : ""; TM.diff = null; TM.diffFor = ""; }
  tmDraw(); tmDiff();
}
// how the avatar of now differs from the moment picked
async function tmDiff() {
  const id = TM.sel;
  if (!id || TM.diffFor === id) return;
  TM.diffFor = id; TM.diff = null; tmDraw();
  let r;
  try { r = await api("/api/tm/diff", { project: TM.project, id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!tmOpenNow() || TM.sel !== id) return;
  TM.diff = r; tmDraw();
}
function tmPic(s, cls) {
  return s.pic ? `<img class="${cls}" src="/tmpic?d=${encodeURIComponent(TM.dir)}&f=${encodeURIComponent(s.pic)}" alt="" loading="lazy">` : `<div class="${cls} tmnopic">无图</div>`;
}
function tmItemHTML(s) {
  return `<button class="tmitem${s.id === TM.sel ? " on" : ""}" data-tmsel="${esc(s.id)}">${tmPic(s, "tmthumb")}
    <span class="tmitxt"><span class="tmlabel"${s.auto ? "" : ` data-i18n="off"`}>${esc(s.label || "")}</span>${s.said ? `<span class="tmsaid small" data-i18n="off">${esc(s.said)}</span>` : ""}<span class="muted small">${esc(fmtTime(s.at))}</span>
      <span class="tmtags"><span class="tag${s.auto ? " faint" : ""}">${s.auto ? "自动" : "手动"}</span><span class="muted small" data-i18n="off">${esc(s.avatar)}</span></span></span></button>`;
}
function tmListHTML(names, off) {
  return `<ul class="tmnames">${names.map(n => `<li data-i18n="off"${off ? ` class="tmoff"` : ""}>${esc(n)}</li>`).join("")}</ul>`;
}
function tmDetailHTML() {
  const s = TM.shots.find(x => x.id === TM.sel);
  if (!s) return `<div class="muted small">选择左侧的一个时刻。</div>`;
  const on = (s.children || []).filter(c => !c.startsWith("-")), off = (s.children || []).filter(c => c.startsWith("-")).map(c => c.slice(1));
  let diff = `<div class="rmbusy"><i class="spin"></i>正在和场景中现在的模型比较…</div>`;
  const d = TM.diff;
  if (d && !d.ok) diff = `<div class="small err">${esc(d.err || "无法比较")}</div>`;
  else if (d) {
    const rows = [];
    if (!d.found) rows.push(`<div class="small warn">${esc(`场景中现在没有名为「${s.avatar}」的模型：还原时会换下场景中正在显示的模型（场景中没有模型时直接放入）。`)}</div>`);
    if ((d.added || []).length) rows.push(`<div class="tmdrow"><span class="k">之后新加的</span>${tmListHTML(d.added)}</div>`);
    if ((d.removed || []).length) rows.push(`<div class="tmdrow"><span class="k">之后去掉的</span>${tmListHTML(d.removed)}</div>`);
    if (d.menuMore || d.menuLess) rows.push(`<div class="tmdrow"><span class="k">菜单</span><span>${d.menuMore ? `之后多了 ${d.menuMore} 项` : ""}${d.menuMore && d.menuLess ? "，" : ""}${d.menuLess ? `少了 ${d.menuLess} 项` : ""}</span></div>`);
    if ((d.imported || []).length) rows.push(`<div class="tmdrow"><span class="k">之后导入的文件夹</span>${tmListHTML(d.imported)}</div>`);
    diff = rows.length ? rows.join("") : `<div class="muted small">物体和菜单未变化；未比较材质与形态键。</div>`;
  }
  let done = "";
  if (TM.done && TM.done.id === s.id) {
    const imp = TM.done.imported || [];
    const said = TM.done.before ? `已回到这一刻：场景中现在是当时的「${TM.done.avatar}」，原来的模型已改名为「${TM.done.before}」并隐藏，没有删除。在 Unity 中按 Ctrl+Z 可撤销；确认无误后再保存场景。`
      : `已回到这一刻：当时的「${TM.done.avatar}」已放回场景。在 Unity 中按 Ctrl+Z 可撤销；确认无误后再保存场景。`;
    done = `<div class="tmdone"><div>${esc(said)}</div>
      ${imp.length ? `<div class="tmimp"><div class="small">移至回收站后，「还原前」的模型可能缺少文件。</div>
        ${imp.map(f => `<label class="frrow"><input type="checkbox" data-tmdrop="${esc(f)}"${TM.drop[f] ? " checked" : ""}><span data-i18n="off">${esc(f)}</span></label>`).join("")}
        <div class="btnrow"><button class="btn small" data-tm="recycle"${Object.values(TM.drop).some(Boolean) && !TM.busy ? "" : " disabled"}>移到回收站</button></div></div>` : ""}</div>`;
  }
  return `<div class="tmbig">${tmPic(s, "tmpic")}</div>
    <div class="tmhead"><span class="tmlabel"${s.auto ? "" : ` data-i18n="off"`}>${esc(s.label || "")}</span><span class="muted small"><span>${esc(fmtTime(s.at))}</span> · <span data-i18n="off">${esc(s.avatar)}</span></span></div>
    ${s.said ? `<div class="tmsaid" data-i18n="off">${esc(s.said)}</div>` : ""}
    ${done}
    <h5 class="lmh">当时身上</h5>${on.length ? tmListHTML(on) : `<div class="muted small">没有子物体。</div>`}${off.length ? `<div class="small muted">默认隐藏：</div>${tmListHTML(off, true)}` : ""}
    <div class="small muted">菜单 ${(s.menu || []).length} 项</div>
    <h5 class="lmh">和现在相比</h5>${diff}
    <div class="btnrow tmacts"><button class="btn primary" data-tm="restore"${TM.busy || !TM.ready ? " disabled" : ""}>${TM.busy === "restore" ? `<i class="spin"></i>` : ""}回到这一刻</button><button class="btn ghost" data-tm="delete"${TM.busy ? " disabled" : ""}>删除这个快照</button></div>
    `;
}
function tmDraw(fresh) {
  const m = $("#modal"); if (!fresh && !tmOpenNow()) return;
  let body;
  if (!TM.loaded) body = `<div class="rmbusy"><i class="spin"></i>正在读取…</div>`;
  else if (TM.err && !TM.shots.length) body = `<div class="small err">${esc(TM.err)}</div>`;
  else {
    const old = TM.ready ? "" : `<div class="small warn tmold">该工程的 Unity 插件为旧版，不支持工程时光机：请在流水线页点击「更新」，待 Unity 编译完成后重试</div>`;
    const list = TM.shots.length ? TM.shots.map(tmItemHTML).join("")
      : `<div class="muted small tmempty">暂无记录</div>`;
    body = `${old}<div class="tmbody"><div class="tmlist">${list}</div><div class="tmdetail">${TM.shots.length ? tmDetailHTML() : ""}</div></div>
      ${TM.err ? `<div class="small err">${esc(TM.err)}</div>` : ""}`;
  }
  const html = `<div class="mhead">工程时光机</div><div class="mbody tmwrap">${body}</div>
    <div class="mfoot"><input id="tm_label" placeholder="备注（选填），例如：换发型前" maxlength="60" value="${esc(TM.label)}"${TM.ready ? "" : " disabled"}>
      <button class="btn" data-tm="save"${TM.busy || !TM.ready ? " disabled" : ""}>${TM.busy === "save" ? `<i class="spin"></i>` : ""}保存当前状态</button><span class="spacer"></span><button class="btn" data-tm="close">关闭</button></div>`;
  if (m._tm === html && !fresh) return;
  const refocus = fresh ? () => {} : focusMark(m);
  const sc = $(".tmlist", m), top = sc ? sc.scrollTop : 0, dt = $(".tmdetail", m), dtop = dt ? dt.scrollTop : 0;
  m._tm = html; m.innerHTML = html; refocus();
  if ($(".tmlist", m)) $(".tmlist", m).scrollTop = top;
  if ($(".tmdetail", m)) $(".tmdetail", m).scrollTop = dtop;
}
async function tmDo(what) {
  const s = TM.shots.find(x => x.id === TM.sel);
  if (TM.busy) return;
  if (what === "save") {
    TM.busy = "save"; TM.err = ""; tmDraw();
    let r;
    try { r = await api("/api/tm/save", { project: TM.project, label: TM.label }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    TM.busy = "";
    if (!r.ok) { TM.err = r.err || "未能保存"; return tmDraw(); }
    TM.label = ""; TM.sel = r.shot.id; TM.done = null; TM.diffFor = ""; toast("已记下当前的模型", 2000);
    return tmList();
  }
  if (!s) return;
  if (what === "restore") {
    if (!confirm(`回到 ${fmtTime(s.at)} 的模型？\n\n当时的模型将放回场景，现在的模型改名为「（还原前）」并隐藏，不删除任何东西。可在 Unity 中按 Ctrl+Z 撤销。`)) return;
    TM.busy = "restore"; TM.err = ""; tmDraw();
    let r;
    try { r = await api("/api/tm/restore", { project: TM.project, id: s.id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    TM.busy = "";
    if (!r.ok) { TM.err = r.err || "未能还原"; return tmDraw(); }
    const imp = (TM.diff && TM.diff.ok && TM.diff.imported) || [];
    TM.done = { id: s.id, avatar: r.avatar || s.avatar, before: r.before || "", imported: imp }; TM.drop = {};
    TM.diffFor = ""; tmDraw(); tmDiff();
    if (window.aiPoll && pipeActive()) aiPoll();
    return;
  }
  if (what === "delete") {
    if (!confirm(`删除 ${fmtTime(s.at)} 的快照？它在工程中保存的模型将移到回收站。`)) return;
    TM.busy = "delete"; TM.err = ""; tmDraw();
    let r;
    try { r = await api("/api/tm/delete", { project: TM.project, id: s.id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    TM.busy = "";
    if (!r.ok) { TM.err = r.err || "未能删除"; return tmDraw(); }
    if (TM.done && TM.done.id === s.id) TM.done = null;
    TM.sel = ""; return tmList();
  }
  if (what === "recycle") {
    const folders = Object.keys(TM.drop).filter(f => TM.drop[f]);
    if (!folders.length || !confirm(`将这 ${folders.length} 个文件夹移到回收站？\n\n${folders.join("\n")}\n\n需要时可从回收站还原。`)) return;
    TM.busy = "recycle"; TM.err = ""; tmDraw();
    let r;
    try { r = await api("/api/tm/recycle", { project: TM.project, folders }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    TM.busy = "";
    if (!r.ok) { TM.err = r.err || "未能移到回收站"; return tmDraw(); }
    if (TM.done) TM.done.imported = TM.done.imported.filter(f => !folders.includes(f));
    TM.drop = {}; toast("已移到回收站", 2000);
    TM.diffFor = ""; tmDraw(); tmDiff();
  }
}
document.addEventListener("click", e => {
  const o = e.target.closest("[data-timemachine]");
  if (o) { if (AI.proj) tmOpen(AI.proj.path); return; }
  if (!tmOpenNow()) return;
  const it = e.target.closest("[data-tmsel]");
  if (it) { if (TM.sel !== it.dataset.tmsel) { TM.sel = it.dataset.tmsel; TM.err = ""; tmDraw(); tmDiff(); } return; }
  const b = e.target.closest("[data-tm]");
  if (!b) return;
  if (b.dataset.tm === "close") return closeModal();
  tmDo(b.dataset.tm);
});
document.addEventListener("change", e => {
  const t = e.target;
  if (t.dataset && t.dataset.tmdrop !== undefined && tmOpenNow()) { TM.drop[t.dataset.tmdrop] = t.checked; tmDraw(); }
});
document.addEventListener("input", e => { if (e.target.id === "tm_label") TM.label = e.target.value; });
document.addEventListener("keydown", e => {
  if (e.target.id === "tm_label" && e.key === "Enter" && tmOpenNow()) { e.preventDefault(); tmDo("save"); }
});
