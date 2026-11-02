// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// 工程的插件 (2.0.5): a project's packages with the newest version of their listing and, when the vendor has
// released a newer one that the listing does not have yet, of its GitHub releases; the ticked ones updated in the
// program (pluginJob). Packages put into Packages/ by hand are listed and never touched.
"use strict";

const PL = { project: "", name: "", rows: null, err: "", pre: false, pick: {}, unity: "", seenJob: null, opened: 0, timer: 0 };

function plOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "plugins"; }
function plCan(p) { return !p.embedded && !!p.newest && p.newest !== p.installed; }
// not known yet: neither its listing nor GitHub has been read (the program reads them in the background)
function plUnknown(p) { return !p.embedded && !p.listing && !p.github; }
function plChecking() { return Date.now() - PL.opened < 60000; }
function plJob() { const j = (S.data || {}).pluginJob; return j && j.project === PL.project ? j : null; }
function plRunning() { const j = plJob(); return !!(j && j.stage !== "done" && j.stage !== "failed"); }

async function plOpen(path, name) {
  Object.assign(PL, { project: path, name: name || rootLabel(path), rows: null, err: "", pick: {}, unity: "", opened: Date.now() });
  const j = plJob(); PL.seenJob = j ? j.id : null; // (an update that ended before is not shown again)
  $("#modal").dataset.kind = "plugins";
  plDraw(true); showModal();
  await plLoad();
}
async function plLoad() {
  let r;
  try { r = await api("/api/project/plugins", { project: PL.project, pre: PL.pre }); } catch (e) { r = { ok: false, err: e.message }; }
  if (!plOpenNow()) return;
  if (!r.ok) { PL.err = r.err || "无法读取插件"; return plDraw(); }
  PL.err = ""; PL.rows = r.packages || []; PL.unity = r.unity || "";
  for (const p of PL.rows) if (!(p.id in PL.pick) || !plCan(p)) PL.pick[p.id] = plCan(p);
  plDraw();
  // what is not known yet is asked for again a few times while the window is open
  clearTimeout(PL.timer);
  if (PL.rows.some(plUnknown) && plChecking()) PL.timer = setTimeout(() => { if (plOpenNow()) plLoad(); }, 4000);
}
function plRowHTML(p) {
  const can = plCan(p), busy = plRunning();
  let to;
  if (p.embedded) to = `<span class="pltag">${/^com\.miovrc\./.test(p.id) ? "MioVRCA" : "手动放入"}</span>`;
  else if (plUnknown(p)) to = `<span class="muted small">${plChecking() ? "正在检查新版本…" : "无法检查新版本"}</span>`;
  else if (can) to = `<span class="plarrow">→</span><b data-i18n="off">${esc(p.newest)}</b><span class="pltag${p.source === "github" ? " gh" : ""}">${p.source === "github" ? "GitHub" : "下载源"}</span>${p.pre ? `<span class="pltag pre">预发布</span>` : ""}`;
  else to = `<span class="muted small">已是最新版本</span>`;
  return `<label class="plrow${can ? "" : " off"}"><input type="checkbox" data-plpick="${esc(p.id)}"${PL.pick[p.id] ? " checked" : ""}${can && !busy ? "" : " disabled"}>
    <span class="plname"><b data-i18n="off">${esc(p.name || p.id)}</b><span class="muted small" data-i18n="off">${esc(p.id)}</span></span>
    <span class="plver"><span data-i18n="off">${esc(p.installed || "—")}</span>${to}</span></label>`;
}
function plJobHTML(j) {
  if (!j || j.id === PL.seenJob) return "";
  const notes = (j.notes || []).map(n => `<div class="small">${esc(n)}</div>`).join("");
  if (j.stage === "done") return `<div class="impres ok"><div>插件已更新</div>${(j.packages || []).length ? `<div class="small" data-i18n="off">${j.packages.map(esc).join("、")}</div>` : ""}${notes}</div>`;
  if (j.stage === "failed") return `<div class="impres err"><div>${esc(j.err || "更新失败")}</div>${(j.packages || []).length ? `<div class="small">已更新：<span data-i18n="off">${j.packages.map(esc).join("、")}</span></div>` : ""}${notes}</div>`;
  const pct = j.total ? Math.round(j.done / j.total * 100) : 0;
  return `<div class="impprog"><div class="t">${esc(j.msg || "正在更新插件")}</div><div class="bar wide"><i style="width:${pct}%"></i></div>${notes}</div>`;
}
function plDraw(fresh) {
  const m = $("#modal"); if (!fresh && !plOpenNow()) return;
  const rows = PL.rows, j = plJob(), busy = plRunning();
  let body;
  if (!rows && !PL.err) body = `<div class="rmbusy"><i class="spin"></i>正在读取…</div>`;
  else if (!rows) body = `<div class="small err">${esc(PL.err)}</div>`;
  else {
    const n = rows.filter(plCan).length, picked = rows.filter(p => plCan(p) && PL.pick[p.id]).length, unknown = rows.some(plUnknown);
    const head = n ? `${n} 个插件有新版本` : unknown ? (plChecking() ? "正在检查新版本…" : "部分插件无法检查新版本，请检查网络后重新打开") : "所有插件都是最新版本";
    body = `<div class="plbar"><span class="small">${head}</span><span class="spacer"></span>
        <label class="check small"><input type="checkbox" data-pl="pre"${PL.pre ? " checked" : ""}${busy ? " disabled" : ""}> 包含预发布版本</label></div>
      <div class="pllist">${rows.length ? rows.map(plRowHTML).join("") : `<div class="small muted">该工程没有通过 VCC / ALCOM 安装的插件</div>`}</div>
      <div class="small muted">下载源还没有收录、但作者已在 GitHub 发布的新版本，标为「GitHub」。手动放入 Packages 的插件不会被更改。</div>
      ${plJobHTML(j)}
      ${PL.err ? `<div class="small err">${esc(PL.err)}</div>` : ""}`;
    PL.picked = picked;
  }
  const html = `<div class="mhead">插件 · <span data-i18n="off">${esc(PL.name)}</span></div><div class="mbody plbody">${body}</div>
    <div class="mfoot"><span class="spacer"></span><button class="btn ghost" data-pl="close">关闭</button>
      <button class="btn primary" data-pl="update"${rows && PL.picked && !busy ? "" : " disabled"}>${busy ? `<i class="spin"></i>正在更新` : "更新所选"}</button></div>`;
  if (m._pl === html && !fresh) return;
  const refocus = fresh ? () => {} : focusMark(m);
  const b = $(".plbody", m), top = b ? b.scrollTop : 0;
  m._pl = html; m.innerHTML = html; refocus();
  if ($(".plbody", m)) $(".plbody", m).scrollTop = top;
}
// told by the poll: the update's progress, and the rows again once it has ended
function plLive(prev) {
  if (!plOpenNow()) return;
  const j = plJob();
  if (j && prev && prev.stage !== j.stage && (j.stage === "done" || j.stage === "failed")) { PL.pick = {}; plLoad(); return; }
  plDraw();
}
async function plDo(what, el) {
  if (what === "close") return closeModal();
  if (what === "pre") { PL.pre = el.checked; PL.rows = null; plDraw(); return plLoad(); }
  if (what === "update") {
    const ids = (PL.rows || []).filter(p => plCan(p) && PL.pick[p.id]).map(p => p.id);
    if (!ids.length || plRunning()) return;
    el.disabled = true;
    let r;
    try { r = await api("/api/project/plugins/update", { project: PL.project, ids, pre: PL.pre }); } catch (e) { r = { ok: false, err: e.message }; }
    if (!r.ok) { el.disabled = false; toast(r.err || "无法开始更新", 5000); return; }
    PL.seenJob = null; S.data.pluginJob = r.job; plDraw(); poll(true);
  }
}
document.addEventListener("click", e => {
  const b = e.target.closest("[data-pl]");
  if (b && plOpenNow() && b.tagName === "BUTTON") plDo(b.dataset.pl, b);
});
document.addEventListener("change", e => {
  if (!plOpenNow()) return;
  const t = e.target;
  if (t.dataset.plpick) { PL.pick[t.dataset.plpick] = t.checked; plDraw(); }
  else if (t.dataset.pl === "pre") plDo("pre", t);
});
