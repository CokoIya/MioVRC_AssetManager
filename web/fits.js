// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// 社区实测数据 (1.8.0): what the check found of outfits on base bodies, kept here and put to use the next time;
// given to the community through GitHub, and what the community measured shown on an asset's card
"use strict";

// list: what /api/fits/list said (null: not read yet); asset: what is known of a shop item's fits, by its number
// sent: how many the page opened last holds (asked whether it was sent: only then are they marked as shared)
const FT = { list: null, busy: "", err: "", asset: new Map(), loading: new Set(), sent: 0 };

function fitsOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "fits"; }
function fitsSettingsRow() {
  return `<div class="row" id="s_fits"><label>社区实测数据</label>
    <div class="btnrow" style="margin-top:0"><button class="btn small" data-fits="open">查看和分享…</button></div>
    </div>`;
}
async function fitsOpen() {
  Object.assign(FT, { list: null, busy: "", err: "", sent: 0 });
  $("#modal").dataset.kind = "fits";
  fitsDraw(true); showModal();
  await fitsLoad();
}
async function fitsLoad() {
  let r;
  try { r = await api("/api/fits/list", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!fitsOpenNow()) return;
  if (r.ok) FT.list = r; else FT.err = r.err || "无法读取";
  fitsDraw();
}
const FT_HINT = { author: "作者说明", local: "本机记录", community: "社区实测" };
function fitsResult(r) {
  if (!r.ok) return `<span class="warnc">${esc(`仍有 ${r.after} 个顶点穿出`)}</span>`;
  return r.clip ? `<span class="okc">${esc(`已修好（${r.clip} 个顶点）`)}</span>` : `<span class="okc">没有穿模</span>`;
}
function fitsHow(r) {
  const parts = [];
  if ((r.shapes || []).length) parts.push(`<span>穿着时设置</span> <span data-i18n="off">${esc(r.shapes.join("、"))}</span>`);
  if (r.cutter) parts.push(`<span>裁掉被盖住的身体</span>`);
  if (r.hint && FT_HINT[r.hint]) parts.push(`<span class="tag faint">${esc(FT_HINT[r.hint])}</span>`);
  return parts.join("<br>") || "—";
}
function fitsDraw(fresh) {
  const m = $("#modal"); if (!fresh && !fitsOpenNow()) return;
  const x = FT.list;
  let body;
  if (!x && !FT.err) body = `<div class="rmbusy"><i class="spin"></i>正在读取…</div>`;
  else if (!x) body = `<div class="small err">${esc(FT.err)}</div>`;
  else {
    const recs = x.records || [];
    const rows = recs.map(r => `<tr><td class="nm" title="${esc(r.asset.name)}"><span data-i18n="off">${esc(r.asset.name)}</span>${r.prefab && r.prefab !== r.asset.name ? `<div class="muted small" data-i18n="off">${esc(r.prefab)}</div>` : ""}</td>
      <td data-i18n="off">${esc(r.base)}</td><td>${fitsResult(r)}</td><td class="how">${fitsHow(r)}</td>
      <td>${r.shared ? `<span class="tag ok">已分享</span>` : r.asset.boothId ? `<span class="tag">未分享</span>` : `<span class="tag faint" title="没有 Booth 商品编号的记录不分享（其他玩家无法确认是同一件衣服）">仅本机</span>`}</td>
      <td class="r"><button class="btn small ghost" data-fits="forget" data-key="${esc(r.key)}" title="删除这条记录">删除</button></td></tr>`).join("");
    const c = x.community;
    body = `
      ${c ? `<div class="small muted">${esc(`已读取社区实测数据：${c.outfits} 件衣服`)}${c.updated ? `<span>（${esc(c.updated)}）</span>` : ""}</div>` : ""}
      ${recs.length ? `<table class="atable fitstable"><thead><tr><th>衣服</th><th>素体</th><th>结果</th><th>做法</th><th>状态</th><th></th></tr></thead><tbody>${rows}</tbody></table>`
        : `<div class="aiempty">还没有记录。运行流水线并完成验收后，这里会列出每件衣服的结果。</div>`}
      <h5 class="lmh">分享给社区</h5>

      <div class="btnrow"><button class="btn small primary" data-fits="submit"${x.shareable && !FT.busy ? "" : " disabled"}>${esc(`在 GitHub 上分享（${x.shareable} 条）`)}</button>${recs.length ? `<button class="btn small ghost" data-fits="clear"${FT.busy ? " disabled" : ""}>清空记录</button>` : ""}</div>
      ${FT.sent ? `<div class="fitsent"><span>提交后点击「已提交」。</span>
        <div class="btnrow"><button class="btn small primary" data-fits="sent"${FT.busy ? " disabled" : ""}>已提交</button><button class="btn small ghost" data-fits="notsent">没有提交</button></div></div>` : ""}
      ${FT.err ? `<div class="small err">${esc(FT.err)}</div>` : ""}`;
  }
  const html = `<div class="mhead">社区实测数据</div><div class="mbody mcpbody">${body}</div>
    <div class="mfoot"><span class="spacer"></span><button class="btn" data-fits="close">关闭</button></div>`;
  if (m._ft === html && !fresh) return;
  const refocus = fresh ? () => {} : focusMark(m);
  const b = $(".mcpbody", m), top = b ? b.scrollTop : 0;
  m._ft = html; m.innerHTML = html; refocus();
  if ($(".mcpbody", m)) $(".mcpbody", m).scrollTop = top;
}
async function fitsDo(what, el) {
  if (what === "open") return fitsOpen();
  if (what === "close") return closeModal();
  if (FT.busy) return;
  if (what === "submit") {
    FT.busy = "submit"; FT.err = ""; fitsDraw();
    let r;
    try { r = await api("/api/fits/submit", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    FT.busy = "";
    if (!r.ok) { FT.err = r.err || "无法分享"; return fitsDraw(); }
    const say = r.long ? "分享码较长，已复制：请在打开的页面中粘贴到「实测数据分享码」一栏" : "已打开提交页面：登录 GitHub 后点「Create」提交";
    if (r.long) await copyText(r.code, say);
    openLink(r.url);
    toast(say, 9000);
    FT.sent = r.n || 1;
    return fitsLoad();
  }
  if (what === "notsent") { FT.sent = 0; return fitsDraw(); }
  if (what === "sent") {
    FT.busy = "sent"; FT.err = ""; fitsDraw();
    let r;
    try { r = await api("/api/fits/sent", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    FT.busy = "";
    if (!r.ok) { FT.err = r.err || "未能保存"; return fitsDraw(); }
    FT.sent = 0;
    return fitsLoad();
  }
  if (what === "forget" || what === "clear") {
    if (what === "clear" && !confirm("清空本机的全部实测记录？已分享给社区的不受影响。")) return;
    FT.busy = what; fitsDraw();
    let r;
    try { r = await api("/api/fits/forget", what === "clear" ? { all: true } : { key: el.dataset.key }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    FT.busy = "";
    if (!r.ok) { FT.err = r.err || "未能删除"; return fitsDraw(); }
    FT.asset.clear();
    return fitsLoad();
  }
}
document.addEventListener("click", e => {
  const b = e.target.closest("button[data-fits]");
  if (b && (fitsOpenNow() || b.dataset.fits === "open")) fitsDo(b.dataset.fits, b);
});

// ---------- on an asset's card ----------
function fitsSec(a) {
  if (!a || !a.boothId || isGum(a)) return "";
  const x = FT.asset.get(a.boothId);
  if (!x || Date.now() - (x._t || 0) > 60000) fitsAssetLoad(a.boothId, a.key);
  if (!x) return "";
  const c = x.community || [], l = x.local || [];
  if (!c.length && !l.length) return "";
  const rows = c.slice(0, 8).map(f => `<div class="fitrow"><span class="fitbase" data-i18n="off">${esc(f.base)}</span><span class="muted small" data-i18n="off">${esc(f.prefab)}</span>
      <span>${esc(`社区实测 ${f.n} 次，${f.ok} 次最终没有穿模`)}</span>${(f.hint || []).length ? `<span class="muted small"><span>常用：穿着时设置</span> <span data-i18n="off">${esc(f.hint.join("、"))}</span></span>` : ""}</div>`).join("")
    + l.slice(0, 6).map(r => `<div class="fitrow"><span class="fitbase" data-i18n="off">${esc(r.base)}</span><span class="muted small" data-i18n="off">${esc(r.prefab || "")}</span><span class="tag faint">本机</span>${fitsResult(r)}</div>`).join("");
  return `<div class="sec" id="fitsec"><h4>实测数据</h4>${rows}
    </div>`;
}
async function fitsAssetLoad(boothId, key) {
  if (FT.loading.has(boothId)) return;
  FT.loading.add(boothId);
  // (asked again a minute later, also when it failed: a failure must not ask again at once, and again…)
  let fits = null;
  try { const r = await api("/api/fits/asset", { boothId }); if (r.ok) fits = r.fits; } catch (e) {}
  FT.loading.delete(boothId);
  FT.asset.set(boothId, Object.assign(fits || FT.asset.get(boothId) || { community: [], local: [] }, { _t: Date.now() }));
  // the section of the open asset, swapped in place
  const a = S.openKey === key ? findAsset(key) : null;
  if (!a) return;
  const sec = $("#fitsec"), html = fitsSec(a);
  if (sec && html) { if (sec._h !== html) { sec.outerHTML = html; const now = $("#fitsec"); if (now) now._h = html; } }
  else if (!!sec !== !!html) renderDrawer(key, true);
}
