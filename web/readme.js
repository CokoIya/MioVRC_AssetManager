// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// an asset's instructions (1.8.0): the readmes, PDF manuals, pages and pictures that came with it, and the steps
// the AI made of them — who does each: the pipeline, the AI, the player — in the details panel
"use strict";

const RM = {
  info: new Map(), // asset key → what /api/readme says (files, steps)
  loading: new Set(), busy: new Set(), err: new Map(),
};
const RM_KIND = { auto: ["流水线自动完成", "auto"], ai: ["AI 可完成", "ai"], manual: ["需手动", "manual"], info: ["备注", "info"] };

function rmFileName(f) { return String(f.name || "").split("/").slice(-1)[0]; }
// the details panel's section: nothing until the files are known, nothing when there are none
function readmeSec(a) {
  if (!isLocal(a) || a.virtual || a.panOnly) return "";
  const r = RM.info.get(a.key);
  if (!r || Date.now() - (r._t || 0) > 60000) rmLoad(a.key); // (looked at again when the panel is opened later)
  if (!r) return "";
  if (!(r.files || []).length) return "";
  const busy = RM.busy.has(a.key), err = RM.err.get(a.key), has = (r.steps || []).length > 0;
  const files = r.files.map(f => `<button class="rmfile" data-gopen="${esc(f.path)}" title="${esc(f.entry ? f.path + " › " + f.entry : f.path)}">${esc({ pdf: "PDF", html: "网页", image: "图片", text: "文本" }[f.kind] || f.kind)}<span>${esc(rmFileName(f))}</span></button>`).join("");
  let body = `<div class="rmfiles">${files}</div>`;
  if (busy) body += `<div class="rmbusy"><i class="spin"></i>AI 正在阅读说明书…</div>`;
  else if (err) body += `<div class="small err">${esc(err)}</div>`;
  if (has) {
    body += r.summary ? `<div class="rmsum">${esc(r.summary)}</div>` : "";
    body += `<ol class="rmsteps">${r.steps.map(s => { const k = RM_KIND[s.kind] || RM_KIND.manual; return `<li class="${k[1]}"><span class="rmk">${esc(k[0])}</span><span class="t">${esc(s.text)}</span></li>`; }).join("")}</ol>`;
    if ((r.requires || []).length) body += `<div class="rmreq"><span class="k">前置要求</span>${r.requires.map(x => `<span class="rmchip">${esc(x)}</span>`).join("")}</div>`;
    if ((r.unread || []).length) body += `<div class="small muted">未能读取：${esc(r.unread.join("；"))}</div>`;
    if (r.stale) body += `<div class="small warnline" style="margin-top:8px">说明书文件有变化，以上为旧版的整理结果。</div>`;
    body += `<div class="hint2">由 ${esc(r.model || "AI")} 整理${r.at ? "于 " + esc(fmtTime(r.at)) : ""}。标为「AI 可完成」的步骤，装配时可交给 AI 执行；运行流水线时也会自动阅读。</div>`;
  } else if (!busy) {
    body += ``;
  }
  const btn = busy ? "" : `<button class="h4act" data-rm="read">${has ? (r.stale ? "重新整理（文件有变化）" : "重新整理") : "AI 整理成步骤"}</button>`;
  return `<div class="sec" id="rmsec"><h4 class="withact">说明书<span class="rmn">${r.files.length}</span>${btn}</h4>${body}</div>`;
}
async function rmLoad(key) {
  if (RM.loading.has(key)) return;
  RM.loading.add(key);
  try {
    const r = await api("/api/readme", { key });
    if (r.ok) RM.info.set(key, Object.assign(r.readme, { _t: Date.now() }));
  } catch (e) {} finally { RM.loading.delete(key); }
  rmDrawLive(key);
}
async function rmRead(key, again) {
  if (RM.busy.has(key)) return;
  RM.busy.add(key); RM.err.delete(key); rmDrawLive(key);
  let r;
  try { r = await api("/api/readme", { key, read: true, again: !!again }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  RM.busy.delete(key);
  if (r.readme) RM.info.set(key, Object.assign(r.readme, { _t: Date.now() }));
  if (!r.ok) RM.err.set(key, r.err || "未能整理说明书");
  else toast("说明书已整理成步骤", 2200);
  rmDrawLive(key);
}
// the section of the open asset swapped in place (the rest of the panel, what is typed in it, stays)
function rmDrawLive(key) {
  const a = S.openKey === key ? findAsset(key) : null;
  if (!a) return;
  const sec = $("#rmsec"), html = readmeSec(a);
  if (sec && html) { if (sec._h !== html) { const re = focusMark(sec); sec.outerHTML = html; const now = $("#rmsec"); if (now) { now._h = html; re(); } } }
  else if (!!sec !== !!html) renderDrawer(key, true);
}
document.addEventListener("click", e => {
  const b = e.target.closest("[data-rm]");
  if (!b || !S.openKey) return;
  if (b.dataset.rm === "read") { const r = RM.info.get(S.openKey); rmRead(S.openKey, r && (r.steps || []).length > 0); }
});
