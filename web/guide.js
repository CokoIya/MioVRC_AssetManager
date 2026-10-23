// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
"use strict";

// The web pane hosts shop sessions, not application windows. This dialog has its own focus and lifecycle.
const GUIDE_PAGES = [
  ["软件能做什么", "整理素材、装配模型，再分享到社区。", "从素材库开始，按需要进入工程和流水线。",
    '<rect x="32" y="32" width="256" height="136" rx="16"/><path d="M32 64h256M104 64v104"/><rect x="128" y="88" width="56" height="56" rx="8"/><path d="M208 96h48m-48 24h32"/>'],
  ["素材库", "添加素材文件夹，按素体、类别或标签查找。", "选中素材后，可导入 Unity 工程。",
    '<path d="M40 72V48h88l20 24h132v96H40Z"/><rect x="64" y="96" width="40" height="48" rx="8"/><rect x="132" y="96" width="40" height="48" rx="8"/><rect x="200" y="96" width="40" height="48" rx="8"/>'],
  ["一键改模", "在「流水线」选择工程和素材，运行装配与菜单生成。", "完成后在 Unity 中检查模型和菜单。",
    '<path d="M48 100h224"/><circle cx="56" cy="100" r="24"/><circle cx="160" cy="100" r="24"/><circle cx="264" cy="100" r="24"/><path d="m44 100 8 8 16-16m80 8 8 8 16-16m80 8 8 8 16-16"/>'],
  ["社区", "浏览作品、交流搭配，收藏想用的素材。", "分享素材链接和装配方案。",
    '<rect x="40" y="40" width="152" height="96" rx="16"/><path d="m72 136-16 24v-24m144-64h64a16 16 0 0 1 16 16v72l-24-16h-48"/><circle cx="80" cy="88" r="4"/><circle cx="116" cy="88" r="4"/><circle cx="152" cy="88" r="4"/>'],
  ["上传到 VRChat", "在 Unity 打开 VRChat SDK，登录后进入 Builder。", "选中模型，填写名称与缩略图，再点 Build & Publish。",
    '<rect x="48" y="32" width="224" height="136" rx="16"/><path d="M48 64h224m-112 80V88m-24 24 24-24 24 24M120 184h80"/>']
];
let guidePage = 0, guideAfter = null, guideBack = null;

async function guideFirstRun() {
  try {
    const r = await api("/api/guide", {});
    if (!r.ok) toast(r.err);
    if (r.ok && !r.seen) { openGuide(() => { if (S.data.setupNeeded) openSetup(); }); return; }
  } catch (e) { toast("无法读取新手指引设置"); }
  if (S.data.setupNeeded) openSetup();
}
function openGuide(after) {
  const g = $("#guide");
  if (g.open) return;
  guidePage = 0; guideAfter = typeof after === "function" ? after : null;
  guideBack = document.activeElement;
  guideDraw(); g.showModal(); $("[data-guide='next']", g).focus();
  api("/api/guide", { seen: true }).then(r => { if (!r.ok) toast(r.err); }).catch(() => toast("无法保存新手指引设置"));
}
function guideDraw() {
  const p = GUIDE_PAGES[guidePage], g = $("#guide");
  g.innerHTML = `<header class="guide-head"><span>新手指引</span><button class="btn icon ghost" data-guide="close" aria-label="关闭">×</button></header>
    <div class="guide-page" aria-live="polite"><svg viewBox="0 0 320 200" aria-hidden="true">${p[3]}</svg>
      <h1 id="guide-title">${p[0]}</h1><p>${p[1]}</p><p>${p[2]}</p></div>
    <footer class="guide-foot"><button class="btn ghost" data-guide="prev"${guidePage ? "" : " disabled"}>上一步</button>
      <nav class="guide-dots" aria-label="新手指引">${GUIDE_PAGES.map((v, i) => `<button data-guide="page" data-page="${i}" aria-label="${v[0]}"${i === guidePage ? ' aria-current="step"' : ""}></button>`).join("")}</nav>
      <button class="btn primary" data-guide="next">${guidePage === GUIDE_PAGES.length - 1 ? "开始使用" : "下一步"}</button></footer>`;
}
$("#guide").addEventListener("click", e => {
  const b = e.target.closest("[data-guide]"); if (!b) return;
  const act = b.dataset.guide;
  if (act === "close" || (act === "next" && guidePage === GUIDE_PAGES.length - 1)) { $("#guide").close(); return; }
  guidePage = act === "page" ? Number(b.dataset.page) : guidePage + (act === "prev" ? -1 : 1);
  guideDraw();
  const next = act === "page" ? `[data-page="${guidePage}"]` : `[data-guide="${act === "prev" && !guidePage ? "next" : act}"]`;
  $(next, $("#guide")).focus();
});
$("#guide").addEventListener("close", () => {
  if ($("#guide").open) return;
  const after = guideAfter; guideAfter = null;
  if (guideBack && guideBack.isConnected) guideBack.focus({ preventScroll: true });
  guideBack = null;
  if (after) after();
});
$("#guide").addEventListener("keydown", e => {
  e.stopPropagation();
  const g = $("#guide"), buttons = [...g.querySelectorAll("button:not(:disabled)")];
  if (e.key === "Tab") {
    const first = buttons[0], last = buttons[buttons.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }
});
document.addEventListener("click", e => { if (e.target.closest("[data-open-guide]")) openGuide(); });
