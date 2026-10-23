// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// 「扫描我的网盘」: the assets already in the player's own Baidu netdisk become netdisk cards, without downloading
// anything (pandl/panscan.go). The button sits in the 「添加网盘素材」 dialog (it opens the netdisk dialog, where a
// folder is picked) and in the netdisk dialog's footer (「扫描此文件夹」: the folder shown, the whole netdisk at its
// top). The scan's own dialog shows how far it is, cancels it, and says what it found at the end.
"use strict";

const PSCAN = { timer: 0, state: null, busy: false };

function pscanOpen() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "panscan"; }

// the buttons, put into the two dialogs whenever they are drawn
function pscanDecorate() {
  const m = $("#modal");
  if (!m.classList.contains("on")) return;
  const row = $(".pafromdisk", m);
  if (row && !$("[data-pscan]", row)) {
    row.insertAdjacentHTML("beforeend", `<button class="linkbtn" data-pscan="pick">扫描我的网盘</button>`);
  }
  if (m.dataset.kind === "pandisk" && typeof DK === "object" && !DK.login && (S.data.baidu || {}).loggedIn) {
    const foot = $(".mfoot", m);
    if (foot && !$("[data-pscan]", foot)) {
      foot.insertAdjacentHTML("afterbegin", `<button class="btn" data-pscan="start">扫描此文件夹</button>`);
    }
  }
}
new MutationObserver(pscanDecorate).observe($("#modal"), { childList: true, subtree: false, attributes: true, attributeFilter: ["class"] });

function pscanDraw() {
  if (!pscanOpen()) return;
  const s = PSCAN.state || {};
  let body = "", foot = "";
  if (s.running) {
    const where = s.dir && s.dir !== "/" ? s.dir : "我的网盘";
    body = `<div class="impprog"><div class="t" style="display:flex;gap:8px"><span class="spin"></span><span>${s.stage === "cards" ? "正在生成卡片" : `正在扫描「${esc(where)}」`}</span></div>
      <div class="small muted">已读取 ${esc(s.folders || 0)} 个文件夹</div></div>`;
    foot = `<button class="btn ghost" data-pscan="cancel">取消</button>`;
  } else if (s.stage === "done") {
    body = `<div class="impres ok"><div>找到 ${esc(s.found)} 个素材，新增 ${esc(s.added)} 个，跳过 ${esc(s.skipped)} 个</div>
      ${s.partial ? `<div class="small warnline">部分文件夹未读取（已达到扫描上限）</div>` : ""}</div>`;
    foot = `<button class="btn primary" data-m="close">关闭</button>`;
  } else if (s.stage === "failed" || s.stage === "cancelled") {
    body = `<div class="impres err"><div>${esc(s.err || "扫描失败")}</div></div>`;
    foot = s.login ? `<button class="btn primary" data-pscan="login">登录百度网盘</button><button class="btn ghost" data-m="close">关闭</button>`
      : `<button class="btn" data-pscan="again">重试</button><button class="btn ghost" data-m="close">关闭</button>`;
  } else {
    body = `<div class="muted small">正在读取…</div>`;
  }
  put($("#pscanbody"), body);
  put($("#pscanfoot"), foot);
}

function pscanShow() {
  const m = $("#modal");
  m.dataset.kind = "panscan";
  m.innerHTML = `<div class="mhead">扫描我的网盘</div><div class="mbody" id="pscanbody"></div><div class="mfoot" id="pscanfoot"></div>`;
  showModal();
  pscanDraw();
}

async function pscanPoll() {
  clearTimeout(PSCAN.timer);
  let r;
  try { r = await api("/api/pan/scan/state", {}); } catch (e) { r = null; }
  if (r && r.ok) {
    const was = PSCAN.state && PSCAN.state.running;
    PSCAN.state = r.scan;
    pscanDraw();
    if (was && !r.scan.running && r.scan.added) load(); // the new cards
  }
  if (PSCAN.state && PSCAN.state.running) PSCAN.timer = setTimeout(pscanPoll, pscanOpen() ? 700 : 3000);
}

async function pscanStart(dir) {
  if (PSCAN.busy) return;
  PSCAN.busy = true;
  let r;
  try { r = await api("/api/pan/scan/start", { dir: dir || "/" }); } catch (e) { r = { ok: false, err: e.message }; }
  PSCAN.busy = false;
  if (!r.ok && r.login) { closeModal(); toast("请先登录百度网盘"); baiduLogin(false); return; }
  // (one under way already is shown instead)
  PSCAN.state = r.ok || (r.scan && r.scan.running) ? r.scan : { stage: "failed", err: r.err || "扫描失败" };
  PSCAN.root = dir || "/";
  pscanShow();
  pscanPoll();
}

document.addEventListener("click", async e => {
  const b = e.target.closest("[data-pscan]");
  if (!b) return;
  const a = b.dataset.pscan;
  if (a === "pick") { // the netdisk dialog, to pick where the scan begins
    if (PSCAN.state && PSCAN.state.running) { pscanShow(); return pscanPoll(); }
    if (typeof openDisk === "function") return openDisk("/");
    return pscanStart("/");
  }
  if (a === "start") return pscanStart(typeof DK === "object" ? DK.dir : "/");
  if (a === "again") return pscanStart(PSCAN.root || "/");
  if (a === "cancel") { await api("/api/pan/scan/cancel", {}); return pscanPoll(); }
  if (a === "login") { closeModal(); return baiduLogin(false); }
});
