// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)

// Pictures in what the player says to the AI (/api/ai/run images): pasted or dropped into 「向 AI 下达指令」 and shown
// above it until they are sent. A large picture is made smaller here first (the long side at most 1600 px), so that
// it stays within the 5 MB a picture may have.

const AIIMG = { list: [] }; // data URLs, in the order they were added
const AIIMG_MAX = 4, AIIMG_SIDE = 1600, AIIMG_BYTES = 4.5 * 1024 * 1024;

function aiImgsHTML() {
  if (!AIIMG.list.length) return `<div class="aiimgs" id="ai_imgs" hidden></div>`;
  return `<div class="aiimgs" id="ai_imgs">${AIIMG.list.map((u, i) => `<span class="aiimg"><img src="${u}" alt=""><button class="aiimgx" data-aiimgx="${i}" title="移除">${ICON.x}</button></span>`).join("")}
    <span class="muted small">${AIIMG.list.length} 张图片将随指令发送</span></div>`;
}
function aiImgsDraw() { const el = document.getElementById("ai_imgs"); if (el) el.outerHTML = aiImgsHTML(); }
function aiImgsTake() { return AIIMG.list.slice(); }
function aiImgsClear() { AIIMG.list = []; aiImgsDraw(); }

// the picture as a data URL, made smaller when it is large; PNG stays PNG while it is small enough (screenshots keep
// their text sharp), anything else that has to be made smaller becomes JPEG
function aiImgShrink(file) {
  return new Promise((done, fail) => {
    const url = URL.createObjectURL(file), img = new Image();
    img.onload = () => {
      URL.revokeObjectURL(url);
      const k = Math.min(1, AIIMG_SIDE / Math.max(img.naturalWidth, img.naturalHeight));
      if (k === 1 && file.size <= AIIMG_BYTES && file.type !== "image/gif") {
        const fr = new FileReader(); fr.onload = () => done(fr.result); fr.onerror = fail; fr.readAsDataURL(file); return;
      }
      const c = document.createElement("canvas");
      c.width = Math.max(1, Math.round(img.naturalWidth * k)); c.height = Math.max(1, Math.round(img.naturalHeight * k));
      const g = c.getContext("2d"); g.fillStyle = "#fff"; g.fillRect(0, 0, c.width, c.height); g.drawImage(img, 0, 0, c.width, c.height);
      let out = file.type === "image/png" ? c.toDataURL("image/png") : "";
      if (!out || out.length * 0.75 > AIIMG_BYTES) out = c.toDataURL("image/jpeg", 0.9);
      done(out);
    };
    img.onerror = () => { URL.revokeObjectURL(url); fail(new Error("unreadable")); };
    img.src = url;
  });
}

async function aiImgAdd(files) {
  for (const f of files) {
    if (AIIMG.list.length >= AIIMG_MAX) { toast("最多附带 4 张图片"); break; }
    if (!/^image\/(png|jpeg|webp|gif)$/.test(f.type)) { toast("无法识别的图片格式"); continue; }
    try { AIIMG.list.push(await aiImgShrink(f)); } catch (e) { toast("无法读取这张图片"); }
  }
  aiImgsDraw();
}

const aiImgFiles = list => [...(list || [])].filter(f => f && /^image\//.test(f.type));
document.addEventListener("paste", e => {
  if (!e.target || e.target.id !== "ai_text") return;
  const files = [...((e.clipboardData || {}).items || [])].filter(x => x.kind === "file" && /^image\//.test(x.type)).map(x => x.getAsFile());
  if (!files.length) return; // (text is pasted as text)
  e.preventDefault(); aiImgAdd(files);
});
document.addEventListener("dragover", e => {
  const box = e.target.closest && e.target.closest(".aifoot");
  if (box && [...((e.dataTransfer || {}).items || [])].some(x => x.kind === "file")) { e.preventDefault(); box.classList.add("over"); }
});
document.addEventListener("dragleave", e => { const box = e.target.closest && e.target.closest(".aifoot"); if (box) box.classList.remove("over"); });
document.addEventListener("drop", e => {
  const box = e.target.closest && e.target.closest(".aifoot");
  if (!box) return;
  const files = aiImgFiles((e.dataTransfer || {}).files);
  box.classList.remove("over");
  if (!files.length) return;
  e.preventDefault(); aiImgAdd(files);
});
document.addEventListener("click", e => {
  const x = e.target.closest("[data-aiimgx]");
  if (!x) return;
  AIIMG.list.splice(Number(x.dataset.aiimgx), 1); aiImgsDraw();
});
