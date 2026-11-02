// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// 外部 AI 接入 (1.8.0): MioVRCA's tools for Claude Code, Codex, DeepSeek Harness and other MCP clients — turned on
// here, with what to add to each of them and a skill that tells their AI how to use MioVRCA
"use strict";

const MC = { info: null, busy: "", err: "", dshPath: "", claude: null, claudeRes: null };

function mcpOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "mcp"; }
function mcpSettingsRow() {
  return `<div class="row" id="s_mcp"><label>外部 AI 接入（MCP）</label>
    <div class="btnrow" style="margin-top:0"><button class="btn small" data-mcp="open">设置外部 AI 接入…</button></div>
    </div>`;
}
async function mcpOpen() {
  Object.assign(MC, { info: null, busy: "", err: "", dshPath: "", claude: null, claudeRes: null });
  $("#modal").dataset.kind = "mcp";
  mcpDraw(true); showModal();
  await mcpLoad();
}
async function mcpLoad() {
  let r;
  try { r = await api("/api/mcp/info", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!mcpOpenNow()) return;
  if (!r.ok) MC.err = r.err || "无法读取"; else MC.info = r;
  mcpDraw();
  mcpClaudeState();
}
// whether Claude Code is on this PC and already has MioVRCA (read from its config; nothing is run)
async function mcpClaudeState() {
  let r = null;
  try { r = await api("/api/mcp/claude/state", {}); } catch (e) { }
  if (!mcpOpenNow()) return;
  MC.claude = r; mcpDraw();
}
function mcpClaudeHTML(c) {
  const st = MC.claude, res = MC.claudeRes, busy = MC.busy === "claude";
  let line = "";
  if (busy) line = `<span class="small muted"><i class="spin"></i>正在接入 Claude Code…</span>`;
  else if (res) line = `<span class="qvdot ${res.ok && res.connected ? "ok" : "no"}"></span><span class="small">${res.ok && res.connected ? "Claude Code · 已连接" : "Claude Code · 连接失败"}</span>`;
  else if (st && !st.found) line = `<span class="small muted">未找到 Claude Code，请先安装</span>`;
  else if (st && st.added) line = `<span class="qvdot ${st.current ? "ok" : "wait"}"></span><span class="small">${st.current ? "Claude Code 已接入" : "Claude Code 接入需要更新"}</span>`;
  // the CLI's own lines, shown as they are; the program's known messages among them translated
  const out = res && !(res.ok && res.connected) && res.output
    ? `<pre class="mcpout" data-i18n="off">${esc(res.output.split("\n").map(l => window.I18N && I18N.t ? I18N.t(l) : l).join("\n"))}</pre>` : "";
  return `<div class="mcpclaude"><button class="btn small primary" data-mcp="claude"${!st || !st.found || MC.busy ? " disabled" : ""}>接入 Claude Code</button>${line}</div>${out}
      <div class="small muted mcpsub">也可以在终端（PowerShell 或命令提示符）中运行一次：</div>${mcpCode("mcp_claude", c.claude)}`;
}
function mcpCode(id, text) {
  return `<div class="mcpcode"><pre id="${id}" data-i18n="off">${esc(text)}</pre><button class="btn small" data-mcp="copy" data-of="${id}">复制</button></div>`;
}
function mcpDraw(fresh) {
  const m = $("#modal"); if (!fresh && !mcpOpenNow()) return;
  const c = MC.info;
  let body;
  if (!c && !MC.err) body = `<div class="rmbusy"><i class="spin"></i>正在读取…</div>`;
  else if (!c) body = `<div class="small err">${esc(MC.err)}</div>`;
  else {
    const skills = (c.skills || []).map(s => `<div class="mcpskill"><span class="qvdot ${s.current ? "ok" : s.there ? "wait" : "no"}"></span><span>${esc(s.for)}</span><span class="muted small" data-i18n="off">${esc(s.path)}</span><span class="muted small">${s.current ? "已安装" : s.there ? "有新版本" : "未安装"}</span></div>`).join("");
    body = `
      <label class="check mcpon"><input type="checkbox" data-mcp="on"${c.on ? " checked" : ""}${MC.busy ? " disabled" : ""}> 允许外部 AI 使用 MioVRCA</label>
      ${c.on ? "" : ``}
      <h5 class="lmh">Claude Code</h5>${mcpClaudeHTML(c)}
      <h5 class="lmh">Codex</h5><div class="small muted mcpsub">${esc(`把下面几行加入 ${c.codexFile}（没有就新建）。导入、流水线、验收可能要好几分钟，所以把工具超时设长：`)}</div>${mcpCode("mcp_codex", c.codex)}
      <h5 class="lmh">DeepSeek Harness</h5><div class="small muted mcpsub">保存后用 dsh web --patch 加载。</div>${mcpCode("mcp_dsh", c.dsh)}
      <div class="btnrow"><button class="btn small" data-mcp="dsh"${MC.busy ? " disabled" : ""}>保存为文件</button>${MC.dshPath ? `<span class="small muted" data-i18n="off">dsh web --patch "${esc(MC.dshPath)}"</span>` : ""}</div>
      <h5 class="lmh">其他 MCP 客户端</h5>${mcpCode("mcp_json", c.json)}
      <h5 class="lmh">技能</h5>
      <div class="mcpskills">${skills}</div><div class="btnrow"><button class="btn small primary" data-mcp="skill"${MC.busy ? " disabled" : ""}>${MC.busy === "skill" ? `<i class="spin"></i>` : ""}安装技能</button></div>
      ${MC.err ? `<div class="small err">${esc(MC.err)}</div>` : ""}`;
  }
  const html = `<div class="mhead">外部 AI 接入（MCP）</div><div class="mbody mcpbody">${body}</div>
    <div class="mfoot"><span class="spacer"></span><button class="btn" data-mcp="close">关闭</button></div>`;
  if (m._mcp === html && !fresh) return;
  const refocus = fresh ? () => {} : focusMark(m);
  const b = $(".mcpbody", m), top = b ? b.scrollTop : 0;
  m._mcp = html; m.innerHTML = html; refocus();
  if ($(".mcpbody", m)) $(".mcpbody", m).scrollTop = top;
}
async function mcpDo(what, el) {
  if (what === "open") return mcpOpen();
  if (what === "close") return closeModal();
  if (what === "copy") { const p = $("#" + el.dataset.of); if (p) copyText(p.textContent); return; }
  if (MC.busy) return;
  if (what === "claude") {
    MC.busy = "claude"; MC.claudeRes = null; MC.err = ""; mcpDraw();
    let r;
    try { r = await api("/api/mcp/claude/add", {}); } catch (e) { r = { ok: false, connected: false, output: e.message }; }
    MC.busy = ""; MC.claudeRes = r;
    if (r.ok && r.connected) toast("Claude Code · 已连接", 2500);
    return mcpClaudeState();
  }
  if (what === "on") {
    MC.busy = "on"; MC.err = ""; mcpDraw();
    let r;
    try { r = await api("/api/mcp/setup", { on: el.checked }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    MC.busy = "";
    if (!r.ok) MC.err = r.err || "未能保存";
    return mcpLoad();
  }
  if (what === "skill" || what === "dsh") {
    MC.busy = what; MC.err = ""; mcpDraw();
    let r;
    try { r = await api(what === "skill" ? "/api/mcp/skill" : "/api/mcp/dsh", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    MC.busy = "";
    if (!r.ok) { MC.err = r.err || "未能保存"; return mcpDraw(); }
    if (what === "dsh") { MC.dshPath = r.path; toast("已保存", 2000); return mcpDraw(); }
    toast("已安装技能", 2000);
    return mcpLoad();
  }
}
document.addEventListener("click", e => {
  const b = e.target.closest("button[data-mcp]");
  if (b && (mcpOpenNow() || b.dataset.mcp === "open")) mcpDo(b.dataset.mcp, b);
});
document.addEventListener("change", e => {
  if (e.target.dataset && e.target.dataset.mcp === "on" && mcpOpenNow()) mcpDo("on", e.target);
});
