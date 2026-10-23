// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// 照图配装 (1.8.0): a picture of a look in, the nearest outfit, hair and accessories of the library out — ticked
// for the line when they are in the project already, tried on in the fitting room, or opened in the library to
// be imported
"use strict";

const LM = { img: "", note: "", busy: false, err: "", res: null, seq: 0 };

function lmOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "look"; }
function lmOpen() {
  if (!AI.proj) return;
  const m = $("#modal");
  m.dataset.kind = "look";
  lmDraw(true);
  showModal();
}
// a picture made small enough to send (the long side 1280), as JPEG
function lmShrink(file) {
  return new Promise((ok, bad) => {
    const url = URL.createObjectURL(file), im = new Image();
    im.onload = () => {
      const s = Math.min(1, 1280 / Math.max(im.naturalWidth, im.naturalHeight)), c = document.createElement("canvas");
      c.width = Math.max(1, Math.round(im.naturalWidth * s)); c.height = Math.max(1, Math.round(im.naturalHeight * s));
      const g = c.getContext("2d"); g.fillStyle = "#fff"; g.fillRect(0, 0, c.width, c.height); g.drawImage(im, 0, 0, c.width, c.height);
      URL.revokeObjectURL(url); ok(c.toDataURL("image/jpeg", 0.9));
    };
    im.onerror = () => { URL.revokeObjectURL(url); bad(new Error("图片无法读取")); };
    im.src = url;
  });
}
async function lmTake(file) {
  if (!file || !/^image\//.test(file.type)) { toast("请选择一张图片", 2500); return; }
  try { LM.img = await lmShrink(file); LM.res = null; LM.err = ""; lmDraw(); } catch (e) { toast(e.message, 3000); }
}
const LM_KINDS = [["衣服", "衣服"], ["头发", "头发"], ["配饰", "配饰"]];
function lmDraw(fresh) {
  const m = $("#modal"); if (!fresh && !lmOpenNow()) return;
  const r = LM.res, d = r && r.desc;
  const drop = LM.img ? `<img class="lmimg" src="${LM.img}" alt="参考图">` : `<div class="lmdrop-t">把图片拖到这里，或按 Ctrl+V 粘贴<br><span class="muted small">别人的模型截图、Booth 商品图、照片都可以</span></div>`;
  let out = "";
  if (LM.busy) out = `<div class="rmbusy"><i class="spin"></i>AI 正在看图，并在素材库中查找…</div>`;
  else if (LM.err) out = `<div class="small err">${esc(LM.err)}</div>`;
  else if (r) {
    const chips = (d.styles || []).map(s => `<span class="rmchip">${esc(s)}</span>`).join("");
    out = `<div class="lmsum">${esc(d.summary || "")}</div>${chips ? `<div class="rmreq">${chips}</div>` : ""}`;
    for (const [k, label] of LM_KINDS) {
      const list = (r.picks || {})[k] || [];
      out += `<h5 class="lmh">${esc(label)}</h5>`;
      if (!list.length) { out += `<div class="muted small">素材库中没有接近的${esc(label)}。</div>`; continue; }
      out += `<div class="lmlist">${list.map(p => {
        const tags = [];
        if (p.worn) tags.push(`<span class="tag ok">已穿上</span>`);
        else if (p.folder) tags.push(`<span class="tag">已在工程中</span>`);
        else tags.push(`<span class="tag faint">素材库</span>`);
        if (!p.fits) tags.push(`<span class="tag warn" title="素材信息中的适配素体不包括该模型的素体">可能不适配</span>`);
        const picked = p.folder && PIPE.pick[p.folder];
        const acts = p.folder ? `${p.worn ? "" : `<button class="btn small${picked ? "" : " primary"}" data-lm="pick" data-folder="${esc(p.folder)}" data-kind="${esc(p.kind)}">${picked ? "已加入流水线" : "加入流水线"}</button><button class="btn small" data-lm="try" data-folder="${esc(p.folder)}">试穿</button>`}`
          : `<button class="btn small" data-lm="lib" data-key="${esc(p.key)}" title="在素材库中打开，可一键导入到工程">在素材库中查看</button>`;
        const a = findAsset(p.key);
        return `<div class="lmcard"><div class="lmcov">${a ? coverHTML(a) : ""}</div>
          <div class="lmtxt"><div class="lmname" data-i18n="off">${esc(p.name)}</div>${p.nameZh && p.nameZh !== p.name ? `<div class="muted small" data-i18n="off">${esc(p.nameZh)}</div>` : ""}
            <div class="lmscore" title="接近程度 ${p.score} / 5">${"●".repeat(p.score)}<span>${"●".repeat(5 - p.score)}</span></div>
            <div class="small">${esc(p.why)}</div><div class="lmtags">${tags.join("")}</div><div class="btnrow">${acts}</div></div></div>`;
      }).join("")}</div>`;
    }
  }
  const html = `<div class="mhead">照图配装</div>
    <div class="mbody lmbody"><div class="lmleft"><label class="lmdrop" id="lm_drop" tabindex="0">${drop}<input type="file" accept="image/*" id="lm_file" hidden></label>
      <textarea id="lm_note" placeholder="补充说明（选填），例如：只要衣服；想要更可爱一点" maxlength="300">${esc(LM.note)}</textarea>
      <div class="btnrow"><button class="btn primary" data-lm="go"${!LM.img || LM.busy ? " disabled" : ""}>开始配装</button>${LM.img ? `<button class="btn ghost" data-lm="clear">换一张</button>` : ""}</div>
      </div>
      <div class="lmright">${out || `<div class="muted small">结果会显示在这里。</div>`}</div></div>
    <div class="mfoot"><span class="spacer"></span><button class="btn" data-lm="close">关闭</button></div>`;
  if (m._lm === html && !fresh) return;
  const refocus = fresh ? () => {} : focusMark(m);
  m._lm = html; m.innerHTML = html; refocus();
}
async function lmGo() {
  if (LM.busy || !LM.img || !AI.proj) return;
  LM.busy = true; LM.err = ""; LM.res = null; LM.note = ($("#lm_note") || {}).value || ""; lmDraw();
  let r;
  try { r = await api("/api/lookmatch", { project: AI.proj.path, image: LM.img, note: LM.note }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  LM.busy = false;
  if (r.ok) LM.res = r; else LM.err = r.err || "未能完成照图配装";
  lmDraw();
}
document.addEventListener("click", e => {
  if (e.target.closest("[data-lookmatch]")) { lmOpen(); return; }
  if (!lmOpenNow()) return;
  const b = e.target.closest("[data-lm]");
  if (!b) return;
  const what = b.dataset.lm;
  if (what === "go") return lmGo();
  if (what === "close") return closeModal();
  if (what === "clear") { LM.img = ""; LM.res = null; LM.err = ""; return lmDraw(); }
  if (what === "pick") {
    const f = b.dataset.folder; PIPE.pick[f] = !PIPE.pick[f]; PIPE.kind[f] = b.dataset.kind; PIPE.keep.add(f);
    pipeAssetsDraw(); aiRenderLive(); return lmDraw();
  }
  if (what === "try") { const f = b.dataset.folder; closeModal(); if (window.frOpen) frOpen(AI.proj.path, { tryFolder: f }); return; }
  if (what === "lib") { const k = b.dataset.key; closeModal(); setView("lib"); setTimeout(() => renderDrawer(k), 50); }
});
document.addEventListener("change", e => {
  if (e.target.id === "lm_file" && e.target.files && e.target.files[0]) lmTake(e.target.files[0]);
});
document.addEventListener("input", e => { if (e.target.id === "lm_note") LM.note = e.target.value; });
document.addEventListener("paste", e => {
  if (!lmOpenNow()) return;
  const it = [...(e.clipboardData || {}).items || []].find(x => x.kind === "file" && /^image\//.test(x.type));
  if (it) { e.preventDefault(); lmTake(it.getAsFile()); }
});
document.addEventListener("dragover", e => { if (lmOpenNow() && e.target.closest("#lm_drop")) { e.preventDefault(); e.target.closest("#lm_drop").classList.add("over"); } });
document.addEventListener("dragleave", e => { const d = e.target.closest && e.target.closest("#lm_drop"); if (d) d.classList.remove("over"); });
document.addEventListener("drop", e => {
  if (!lmOpenNow() || !e.target.closest("#lm_drop")) return;
  e.preventDefault(); e.target.closest("#lm_drop").classList.remove("over");
  const f = [...(e.dataTransfer || {}).files || []][0]; if (f) lmTake(f);
});
