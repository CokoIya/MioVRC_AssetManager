// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// 高速下载 (2.0.5): netdisk downloads through Baidu's open platform. The player scans a code with the Baidu app
// once; the program keeps the sign-in (never this page). Its state is read when the settings open and polled only
// while the code is on screen.
"use strict";

const BDO = { state: "", vip: -1, err: "", code: null, timer: 0, run: 0 };

function bdoOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "bdopen"; }
// the settings' row, under the netdisk sign-in
function bdoRowHTML() {
  const on = BDO.state === "done";
  return `<div class="row" id="s_bdopen"><label>高速下载<span class="muted small">　${on ? "已开启" : BDO.state ? "未开启" : ""}</span></label>
    <div class="btnrow" style="margin-top:0"><button class="btn small" data-bdo="start">${on ? "重新授权" : "开启高速下载"}</button>${on ? `<button class="btn small" data-bdo="signout">关闭高速下载</button>` : ""}</div>
    <div class="small muted">用百度网盘 App 扫码授权一次，之后网盘下载更快（超级会员）。转存仍使用上面登录的账号，两边请用同一个账号。</div>
    ${BDO.err && !bdoOpenNow() ? `<div class="small warnline">${esc(BDO.err)}</div>` : ""}</div>`;
}
function bdoRowDraw() { const r = $("#s_bdopen"); if (r) r.outerHTML = bdoRowHTML(); }
async function bdoRead() {
  let r;
  try { r = await api("/api/pan/open/poll", {}); } catch (e) { return; }
  BDO.state = r.state || ""; BDO.vip = r.vip; BDO.err = r.state === "error" ? r.err || "" : "";
  bdoRowDraw();
}
function bdoDraw() {
  const m = $("#modal"); if (!bdoOpenNow()) return;
  const c = BDO.code, st = BDO.state;
  let body;
  if (st === "done") body = `<div class="bdodone"><span class="qvdot ok"></span>高速下载已开启${BDO.vip === 2 ? "" : `<div class="small muted">当前账号不是超级会员，下载仍会受百度限速。</div>`}</div>`;
  else if (st === "error") body = `<div class="small err">${esc(BDO.err || "授权失败")}</div>`;
  else if (st === "expired" || !c) body = `<div class="small muted">${c ? "二维码已过期，请重新获取。" : "正在获取二维码…"}</div>`;
  else body = `<div class="bdoqr"><img src="${esc(c.qrcode_url)}" alt="" width="200" height="200">
      <div><div>用百度网盘 App 扫描二维码，确认授权。</div>
        <div class="small muted">或在浏览器打开授权页面，输入授权码：</div>
        <div class="bdocode" data-i18n="off">${esc(c.user_code)}</div>
        <button class="btn small" data-url="${esc(c.verification_url)}">打开授权页面</button>
        <div class="small muted"><i class="spin"></i>等待授权…</div></div></div>`;
  const html = `<div class="mhead">开启高速下载</div><div class="mbody">${body}</div>
    <div class="mfoot"><span class="spacer"></span>${st === "expired" || st === "error" ? `<button class="btn" data-bdo="start">重新获取</button>` : ""}<button class="btn ${st === "done" ? "primary" : "ghost"}" data-bdo="close">${st === "done" ? "完成" : "关闭"}</button></div>`;
  if (m._bdo === html) return;
  m._bdo = html; m.innerHTML = html;
}
// waits for the player's approval: asks every few seconds while the code is shown, stops when the window closes
function bdoWait(run) {
  clearTimeout(BDO.timer);
  BDO.timer = setTimeout(async () => {
    if (run !== BDO.run || !bdoOpenNow() || BDO.state !== "pending") return;
    let r;
    try { r = await api("/api/pan/open/poll", {}); } catch (e) { return bdoWait(run); }
    if (run !== BDO.run) return;
    BDO.state = r.state || "error"; BDO.vip = r.vip; BDO.err = r.err || "";
    bdoDraw(); bdoRowDraw();
    if (BDO.state === "pending") bdoWait(run);
    else if (BDO.state === "done") toast("高速下载已开启", 2500);
  }, Math.max(5, (BDO.code && BDO.code.interval) || 5) * 1000);
}
async function bdoDo(what) {
  if (what === "close") { BDO.run++; clearTimeout(BDO.timer); return closeModal(); }
  if (what === "signout") {
    BDO.run++;
    try { await api("/api/pan/open/signout", {}); } catch (e) { }
    Object.assign(BDO, { state: "expired", vip: -1, err: "", code: null });
    toast("已关闭高速下载", 2000); return bdoRowDraw();
  }
  if (what === "start") {
    const run = ++BDO.run;
    Object.assign(BDO, { state: "pending", err: "", code: null });
    $("#modal").dataset.kind = "bdopen"; $("#modal")._bdo = "";
    showModal(); bdoDraw();
    let r;
    try { r = await api("/api/pan/open/start", {}); } catch (e) { r = { state: "error", err: e.message }; }
    if (run !== BDO.run) return;
    if (r.err || !r.user_code) Object.assign(BDO, { state: "error", err: r.err || "无法获取二维码", code: null });
    else BDO.code = r;
    bdoDraw();
    if (BDO.state === "pending") bdoWait(run);
  }
}
// on a download's line: where it comes from, and the way to more speed for a 超级会员 still on the web path
function bdoJobHTML(j) {
  const fast = j.source === "open";
  return `${fast ? `<span class="pltag gh">高速下载</span>` : ""}${j.note ? `<div class="small warnline">${esc(j.note)}${!fast && j.vip === "超级会员" ? ` <button class="linkbtn" data-bdo="start">开启高速下载</button>` : ""}</div>` : ""}`;
}
document.addEventListener("click", e => {
  const b = e.target.closest("[data-bdo]");
  if (b) bdoDo(b.dataset.bdo);
});
