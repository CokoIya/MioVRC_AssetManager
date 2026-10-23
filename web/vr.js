// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// 不摘头显改模 (1.8.0): what the player says in VR comes from miovrct (speech recognition, the chatbox) to /vr/ask,
// the AI changes the avatar in Unity, has it built and sent to the game and looks at it with VRChat's camera; the
// replies go back to miovrct, or straight into the chatbox
"use strict";

const VR = { d: null, cfg: null, busy: "", err: "", shown: false };

function vrOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "vr"; }
function vrSettingsRow() {
  return `<div class="row" id="s_vr"><label>不摘头显改模</label>
    <div class="btnrow" style="margin-top:0"><button class="btn small" data-vr="open">设置不摘头显改模…</button></div>
    </div>`;
}
async function vrOpen() {
  Object.assign(VR, { d: null, cfg: null, busy: "", err: "", shown: false });
  $("#modal").dataset.kind = "vr";
  vrDraw(true); showModal();
  let r;
  try { r = await api("/api/vr/config", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!vrOpenNow()) return;
  if (!r.ok) VR.err = r.err || "无法读取"; else { VR.d = r; VR.cfg = Object.assign({}, r.cfg); }
  vrDraw();
}
function vrDraw(fresh) {
  const m = $("#modal"); if (!fresh && !vrOpenNow()) return;
  const d = VR.d, c = VR.cfg;
  let body;
  if (!d && !VR.err) body = `<div class="rmbusy"><i class="spin"></i>正在读取…</div>`;
  else if (!d) body = `<div class="small err">${esc(VR.err)}</div>`;
  else {
    const url = `http://127.0.0.1:${location.port || 80}${d.endpoint}`;
    const opts = [`<option value="">${esc("自动：最近在流水线里用过的工程")}</option>`].concat((d.projects || []).map(p => `<option value="${esc(p.path)}"${p.path === c.project ? " selected" : ""} data-i18n="off">${esc(p.name)}</option>`)).join("");
    const tok = c.token ? (VR.shown ? c.token : "•".repeat(12)) : "（保存后生成）";
    body = `
      <label class="check mcpon"><input type="checkbox" id="vr_on"${c.on ? " checked" : ""}> 开启不摘头显改模</label>
      <div class="row"><label for="vr_proj">改哪个工程</label><select id="vr_proj">${opts}</select>${d.now ? `<div class="small muted mcpsub">${esc(`现在会改：${d.now}`)}</div>` : ""}</div>
      <h5 class="lmh">回复发到哪里</h5>
      <div class="row"><label for="vr_reply">miovrct 接收回复的地址</label><input id="vr_reply" value="${esc(c.replyUrl || "")}" placeholder="例如 http://127.0.0.1:端口/miovrca（在 miovrct 中查看）" spellcheck="false"></div>
      <label class="check"><input type="checkbox" id="vr_chatbox"${c.chatbox ? " checked" : ""}> 没有 miovrct 时，直接把回复发到 VRChat 聊天框（OSC）</label>
      <div class="small muted mcpsub">用 miovrct 时不要勾选：两边都往聊天框发会互相覆盖。</div>
      <h5 class="lmh">游戏里</h5>
      <label class="check"><input type="checkbox" id="vr_build"${c.autoBuild ? " checked" : ""}> AI 每次改完模型都自动 Build &amp; Test</label>
      <div class="small muted mcpsub">先在 Other → Local test avatars 中换上测试头像。</div>
      <div class="vrgrid"><div class="row"><label for="vr_port">VRChat 的 OSC 端口</label><input id="vr_port" value="${esc(String(c.oscPort || 9000))}" inputmode="numeric"></div>
        <div class="row"><label for="vr_photo">照片文件夹（选填）</label><input id="vr_photo" value="${esc(c.photoDir || "")}" placeholder="不填：VRChat 的设置或 图片\\VRChat" spellcheck="false"></div></div>
      <div class="small muted mcpsub">请在 Action Menu → Options → OSC 启用 OSC。</div>
      <h5 class="lmh">给 miovrct 的接入信息</h5>
      <div class="mcpcode"><pre id="vr_url" data-i18n="off">${esc(url)}</pre><button class="btn small" data-vr="copy" data-of="vr_url">复制</button></div>
      <div class="mcpcode"><pre id="vr_tok" data-i18n="off">${esc(tok)}</pre><button class="btn small" data-vr="show"${c.token ? "" : " disabled"}>${VR.shown ? "隐藏" : "显示"}</button><button class="btn small" data-vr="copytok"${c.token ? "" : " disabled"}>复制</button><button class="btn small ghost" data-vr="newtok"${c.token ? "" : " disabled"}>重新生成</button></div>

      ${VR.err ? `<div class="small err">${esc(VR.err)}</div>` : ""}`;
  }
  const html = `<div class="mhead">不摘头显改模</div><div class="mbody mcpbody">${body}</div>
    <div class="mfoot"><button class="btn" data-vr="test"${!d || VR.busy ? " disabled" : ""}>发送测试回复</button><span class="spacer"></span><button class="btn ghost" data-vr="close">取消</button><button class="btn primary" data-vr="save"${!d || VR.busy ? " disabled" : ""}>保存</button></div>`;
  if (m._vr === html && !fresh) return;
  const refocus = fresh ? () => {} : focusMark(m);
  const b = $(".mcpbody", m), top = b ? b.scrollTop : 0;
  m._vr = html; m.innerHTML = html; refocus();
  if ($(".mcpbody", m)) $(".mcpbody", m).scrollTop = top;
}
// what the form says
function vrRead() {
  const c = VR.cfg, v = id => ($("#" + id) || {}).value || "";
  const chk = id => !!($("#" + id) || {}).checked;
  return Object.assign({}, c, { on: chk("vr_on"), project: v("vr_proj"), replyUrl: v("vr_reply").trim(), chatbox: chk("vr_chatbox"), autoBuild: chk("vr_build"),
    oscPort: parseInt(v("vr_port"), 10) || 9000, photoDir: v("vr_photo").trim() });
}
async function vrSave(newToken) {
  VR.busy = "save"; VR.err = ""; VR.cfg = vrRead(); vrDraw();
  let r;
  try { r = await api("/api/vr/save", { cfg: VR.cfg, newToken: !!newToken }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  VR.busy = "";
  if (!r.ok) { VR.err = r.err || "未能保存"; vrDraw(); return false; }
  VR.d = r; VR.cfg = Object.assign({}, r.cfg);
  vrDraw();
  return true;
}
async function vrDo(what, el) {
  if (what === "open") return vrOpen();
  if (what === "close") return closeModal();
  if (what === "copy") { const p = $("#" + el.dataset.of); if (p) copyText(p.textContent); return; }
  if (what === "copytok") { if (VR.cfg && VR.cfg.token) copyText(VR.cfg.token); return; }
  if (what === "show") { VR.cfg = vrRead(); VR.shown = !VR.shown; return vrDraw(); }
  if (VR.busy) return;
  if (what === "save") { if (await vrSave()) { toast("已保存", 2000); closeModal(); } return; }
  if (what === "newtok") {
    if (!confirm("重新生成令牌？miovrct 里填写的旧令牌将失效，需要重新填写。")) return;
    if (await vrSave(true)) toast("已生成新令牌", 2000);
    return;
  }
  if (what === "test") {
    if (!(await vrSave())) return;
    let r;
    try { r = await api("/api/vr/test", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    if (!r.ok) { VR.err = r.err || "未能发送"; return vrDraw(); }
    toast("已发送测试回复：请在 miovrct 或 VRChat 的聊天框中查看", 3500);
  }
}
document.addEventListener("click", e => {
  const b = e.target.closest("button[data-vr]");
  if (b && (vrOpenNow() || b.dataset.vr === "open")) vrDo(b.dataset.vr, b);
});
