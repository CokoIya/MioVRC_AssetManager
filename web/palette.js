// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)

// Ctrl+K: one box that finds anything — a page, a settings section, an action, an asset, a project — and goes there.

const PAL = { open: false, q: "", i: 0, items: [] };

function palCommands() {
  const go = v => () => { closeModal(true); setView(v); };
  const set = sec => () => { closeModal(true); openSettings(); setSecShow(sec); };
  const cmds = [
    ["素材库", "页面", go("lib")], ["我的模型", "页面", go("proj")], ["社区", "页面", go("social")],
    ["Booth", "商店", go("shop")], ["闲鱼", "商店", go("xianyu")],
    ["深色 / 浅色", "外观", () => { closeModal(true); if (typeof themeToggle === "function") themeToggle(document.querySelector("[data-theme-toggle]")); }],
    ["重新扫描", "素材库", () => { closeModal(true); const b = $("#btnScan"); if (b) b.click(); }],
    ["网盘素材", "素材库", () => { closeModal(true); openPanAdd(); }],
    ["添加工程…", "我的模型", () => { closeModal(true); addProject(); }],
    ["新建基础工程…", "我的模型", () => { closeModal(true); openNewProject(); }],
    ["新手指引", "帮助", () => { closeModal(true); if (typeof openGuide === "function") openGuide(); }],
  ];
  if (typeof ocCanStart === "function" && ocCanStart()) cmds.push(["一键创建", "改模篮", () => { closeModal(true); ocStart(); }]);
  for (const [id, t] of typeof SET_SECS !== "undefined" ? SET_SECS : []) cmds.push([t, "设置", set(id)]);
  return cmds.map(([label, kind, run]) => ({ label, kind, run }));
}
function palFind(q) {
  const k = q.trim().toLowerCase(), hit = s => (s || "").toLowerCase().includes(k);
  // a name that is the query comes first, then one that starts with it, one that has it, one whose kind has it
  const score = c => { const l = c.label.toLowerCase(); return l === k ? 0 : l.startsWith(k) ? 1 : l.includes(k) ? 2 : 3; };
  const cmds = palCommands().filter(c => !k || hit(c.label) || hit(c.kind)).map((c, i) => [score(c), i, c]).sort((x, y) => x[0] - y[0] || x[1] - y[1]).map(x => x[2]);
  if (!k) return cmds;
  const assets = (S.data.assets || []).filter(a => !a.virtual && (hit(a.name) || hit(a.zh) || hit(a.shop) || (a.bases || []).some(hit))).slice(0, 8)
    .map(a => ({ label: a.name, kind: a.category || "素材", sub: a.zh || a.shop || "", data: true, run: () => { closeModal(true); setView("lib"); renderDrawer(a.key); } }));
  const projs = (S.projs || []).filter(p => hit(p.name)).slice(0, 5)
    .map(p => ({ label: p.name, kind: "工程", data: true, run: () => { closeModal(true); setView("proj"); if (typeof ckOpenProj === "function") ckOpenProj(p.path); } }));
  return [...cmds, ...projs, ...assets];
}
function palOpen() {
  if (typeof S === "undefined" || !S.data) return;
  if (!(S.projs || []).length && typeof loadProjects === "function") loadProjects(); // projects are searchable once loaded
  const m = $("#modal"); m.dataset.kind = "palette";
  PAL.q = ""; PAL.i = 0;
  m.innerHTML = `<div class="palbox"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/></svg>
    <input id="pal_q" type="search" autocomplete="off" placeholder="搜索页面、设置、素材、工程" aria-label="搜索"></div><div class="pallist" id="pal_list" role="listbox"></div>`;
  showModal(); PAL.open = true;
  palDraw(); $("#pal_q").focus();
}
function palDraw() {
  PAL.items = palFind(PAL.q);
  PAL.i = Math.min(PAL.i, Math.max(0, PAL.items.length - 1));
  const list = $("#pal_list"); if (!list) return;
  list.innerHTML = PAL.items.length ? PAL.items.map((it, i) => `<button class="palitem${i === PAL.i ? " on" : ""}" role="option" aria-selected="${i === PAL.i}" data-pal="${i}">
      <span class="pl"${it.data ? ' data-i18n="off"' : ""}>${esc(it.label)}</span>${it.sub ? `<span class="ps" data-i18n="off">${esc(it.sub)}</span>` : ""}<span class="pk">${esc(it.kind)}</span></button>`).join("")
    : `<div class="palnone">无结果</div>`;
  const on = list.querySelector(".palitem.on"); if (on) on.scrollIntoView({ block: "nearest" });
}
function palRun(i) { const it = PAL.items[i]; PAL.open = false; if (it) it.run(); }

document.addEventListener("keydown", e => {
  if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k") {
    e.preventDefault();
    if ($("#modal").dataset.kind === "palette" && $("#modal").classList.contains("on")) { closeModal(true); return; }
    palOpen(); return;
  }
  if (!PAL.open || $("#modal").dataset.kind !== "palette") return;
  if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); const n = PAL.items.length || 1; PAL.i = (PAL.i + (e.key === "ArrowDown" ? 1 : n - 1)) % n; palDraw(); }
  else if (e.key === "Enter" && e.target.id === "pal_q") { e.preventDefault(); palRun(PAL.i); }
}, true);
document.addEventListener("input", e => { if (e.target.id === "pal_q") { PAL.q = e.target.value; PAL.i = 0; palDraw(); } });
document.addEventListener("click", e => {
  const b = e.target.closest("[data-pal]"); if (b) { palRun(+b.dataset.pal); return; }
  if (PAL.open && $("#modal").dataset.kind !== "palette") PAL.open = false;
});
