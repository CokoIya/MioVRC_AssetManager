// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)

// Archives and unitypackages dropped anywhere onto the window go into the library (/api/import/drop keeps them in
// the download folder and has the library pick them up). The community's composer and 照图配装 take their own
// drops (images) and are left alone.

const DROP = { depth: 0 };
const DROP_KINDS = /\.(zip|7z|rar|unitypackage)$/i;

function dropHasFiles(e) { return !!e.dataTransfer && [...e.dataTransfer.types].includes("Files"); }
// pictures dragged in while a community composer is on screen are that composer's, wherever they are dropped: the
// one in the window when it is open, else the first in sight (the browser tells the kinds before the drop)
function dropComposer(e) {
  const items = [...(e.dataTransfer?.items || [])].filter(i => i.kind === "file");
  if (!items.length || !items.every(i => /^image\//.test(i.type)) || typeof soComposeAdd !== "function") return null;
  return document.querySelector("#modal.on [data-composer]") || (!document.querySelector("#modal.on") && document.querySelector("#grid [data-composer]")) || null;
}
function dropElsewhere(e) { return !!e.target.closest && !!e.target.closest("[data-composer], #lm_drop") || !!dropComposer(e); }
function dropShow(on) {
  let z = document.getElementById("dropzone");
  if (on && !z) {
    z = document.createElement("div"); z.id = "dropzone"; z.setAttribute("aria-hidden", "true");
    z.innerHTML = `<div><svg viewBox="0 0 24 24"><path d="M12 4v11M7 10l5 5 5-5"/><path d="M4 18v2h16v-2"/></svg><b>松开即可导入素材</b></div>`;
    document.body.appendChild(z);
  }
  if (z) z.classList.toggle("on", on);
}
document.addEventListener("dragenter", e => { if (!dropHasFiles(e) || dropElsewhere(e)) return; DROP.depth++; dropShow(true); });
document.addEventListener("dragleave", e => { if (!dropHasFiles(e)) return; if (--DROP.depth <= 0) { DROP.depth = 0; dropShow(false); } });
document.addEventListener("dragover", e => {
  if (!dropHasFiles(e)) return;
  if (dropElsewhere(e) && !(dropComposer(e) && !e.target.closest?.("[data-composer], #lm_drop"))) return;
  e.preventDefault(); e.dataTransfer.dropEffect = "copy";
});
document.addEventListener("drop", async e => {
  const composer = dropHasFiles(e) && !e.target.closest?.("[data-composer], #lm_drop") ? dropComposer(e) : null;
  if (composer) { e.preventDefault(); DROP.depth = 0; dropShow(false); soComposeAdd(e.dataTransfer.files, composer.dataset.composer); return; }
  if (!dropHasFiles(e) || dropElsewhere(e)) return;
  e.preventDefault(); DROP.depth = 0; dropShow(false);
  const files = [...e.dataTransfer.files];
  const take = files.filter(f => DROP_KINDS.test(f.name));
  if (!take.length) { toast("只能拖入压缩包或 unitypackage", 3500); return; }
  for (const f of take) {
    toast(`正在导入「${f.name}」`, 60000);
    let r;
    try {
      const res = await fetch("/api/import/drop?name=" + encodeURIComponent(f.name), { method: "POST", headers: { "X-Token": window.API_TOKEN }, body: f });
      r = await res.json();
    } catch (err) { r = { ok: false, err: "无法连接到程序" }; }
    if (!r.ok) { toast(r.err || "导入失败", 5000); return; }
  }
  toast(take.length > 1 ? `已导入 ${take.length} 个文件，正在收录` : `已导入「${take[0].name}」，正在收录`, 4000);
  if (S.view !== "lib") setView("lib");
  if (typeof poll === "function") poll(true);
});
