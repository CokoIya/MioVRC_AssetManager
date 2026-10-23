// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// 「ChatGPT 账号」: the player's ChatGPT plan in place of an API key (OpenAI's Sign in with ChatGPT). Signing in
// happens in the player's own browser, on OpenAI's page. This file draws the sign-in in the AI service dialog and
// follows one under way, shows the note that the plan is in use (once, after the first sign-in), the 「正在使用
// ChatGPT 套餐」 line under the AI's input box, and the way out of the plan's usage limit (「管理用量」 first). The
// dialog, the pipeline and the usage window are app.js's and aiusage.js's; the server side is internal/ai/chatgpt.go.
"use strict";

LINKS.gptusage = "https://chatgpt.com/settings/usage";
LINKS.gpthelp = "https://help.openai.com/";

const GPT = { timer: 0 };

function gptOn(c) { return ((c || AI.cfg || {}).provider) === "chatgpt"; }
function gptState() { return (AI.cfg || {}).chatgpt || {}; }
function gptCfgOpen() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "aicfg" && !!$("#gpt_card"); }

// 「正在使用 ChatGPT 套餐」, with the way to ChatGPT's usage settings
function gptUseHTML() {
  return `<span class="gptuse"><i class="dot ok"></i><span>正在使用 ChatGPT 套餐</span><button class="linkbtn" data-link="gptusage">管理用量</button></span>`;
}

// the dialog's part for 「ChatGPT 账号」, in place of the address and the key
function gptCardHTML() {
  const g = gptState();
  let body;
  if (g.pending) {
    body = `<div class="gptwait"><i class="spin"></i><span>请在浏览器中登录 ChatGPT 并完成授权…</span></div>
      <div class="btnrow"><button class="btn small" data-ai="gptreopen">重新打开登录页面</button><button class="btn small ghost" data-ai="gptcancel">取消登录</button></div>`;
  } else if (g.signedIn) {
    body = `<div class="gptacct"><span>已登录：</span><b data-i18n="off">${esc(g.email || "ChatGPT")}</b><button class="btn small ghost" data-ai="gptout">退出登录</button></div>` +
      (g.plan ? `<div class="gptnow">${gptUseHTML()}<button class="linkbtn" data-ai="gptconsent" title="在 OpenAI 的页面中重新确认本软件对套餐额度的使用">重新授权</button></div>`
        : `<div class="warnline">该登录未授权本软件使用你的 ChatGPT 套餐额度。</div><div class="btnrow"><button class="btn gpt" data-ai="gptconsent">启用套餐额度</button></div>`);
  } else {
    // the accounts signed in here before: the last one first, each with its own registration at OpenAI
    const accts = g.accounts || [], last = accts[0];
    body = `<div class="btnrow"><button class="btn gpt" data-ai="gptin"${last ? ` data-acct="${esc(last.id)}"` : ""}>使用 ChatGPT 继续</button>${last ? `<button class="linkbtn" data-ai="gptother">使用其他账号</button>` : ""}</div>` +
      (last ? `<div class="hint2"><span>上次登录：</span><span data-i18n="off">${esc(last.email)}</span></div>` : "") +
      (accts.length > 1 ? `<div class="hint2 gptaccts"><span>此前登录过的账号：</span>${accts.slice(1).map(a => `<button class="linkbtn" data-ai="gptin" data-acct="${esc(a.id)}" data-i18n="off">${esc(a.email || a.id)}</button>`).join("")}</div>` : "");
  }
  return `<div class="gptcard" id="gpt_card"><h4>使用你的 ChatGPT 套餐</h4>
    <p class="hint2">无需 API Key：AI 请求计入你的 ChatGPT 套餐用量，可在 ChatGPT 的设置中查看和限制。MioVRCA 本身免费，不收取任何费用。<button class="linkbtn" data-link="gpthelp">了解哪些套餐可用</button></p>
    ${body}${g.err && !g.pending ? `<div class="aitest err">${esc(g.err)}</div>` : ""}${g.unread ? `<div class="aitest err">ChatGPT 登录文件暂时无法读取，请稍后重试</div>` : ""}</div>`;
}

// the note shown once, after the first sign-in that lets the program use the plan
function gptWelcomeDue() { const g = gptState(); return gptOn({ provider: (AI.cfgForm || {}).provider }) && g.signedIn && g.plan && g.welcome; }
function gptWelcome() {
  const m = $("#modal");
  m.dataset.kind = "aicfg";
  m.innerHTML = `<div class="mhead">你正在使用 ChatGPT 套餐</div><div class="mbody">

  </div><div class="mfoot"><button class="btn ghost" data-link="gptusage">管理用量</button><span class="spacer"></span><button class="btn primary" data-ai="gptok">知道了</button></div>`;
  showModal();
}

// the dialog drawn again where it was scrolled to (the sign-in moved on)
async function gptRedraw() {
  if (!$("#modal").classList.contains("on") || $("#modal").dataset.kind !== "aicfg" || !AI.cfgForm) return;
  const body = $("#modal .mbody"), top = body ? body.scrollTop : 0;
  if (gptWelcomeDue()) return gptWelcome();
  await openAICfg(true);
  const b2 = $("#modal .mbody"); if (b2) b2.scrollTop = top;
}

// a sign-in under way: followed until it ends, then the dialog shows how it went
function gptFollow() {
  clearTimeout(GPT.timer);
  GPT.timer = setTimeout(async () => {
    let r;
    try { r = await api("/api/ai/config", {}); } catch (e) { return gptFollow(); }
    if (r.ok) AI.cfg = r.ai;
    const g = gptState();
    if (g.pending) { if (gptCfgOpen()) gptCardDraw(); return gptFollow(); }
    if (!gptCfgOpen()) return;
    await gptRedraw();
    if (g.signedIn && g.plan && !gptWelcomeDue()) gptModelsSoon();
  }, 1000);
}
// only the card, while it waits (what is typed in the rest of the dialog stays)
function gptCardDraw() {
  const el = $("#gpt_card"); if (!el) return;
  const h = gptCardHTML();
  if (el._h === h) return;
  el.outerHTML = h;
  const now = $("#gpt_card"); if (now) now._h = h;
}
// signed in and no model chosen yet: the account's models are listed to choose from
function gptModelsSoon() {
  const f = AI.cfgForm, inp = $("#ai_model");
  if (!f || f.provider !== "chatgpt" || !inp || inp.value.trim()) return;
  aiAction({ dataset: { ai: "models" }, disabled: false });
}

async function gptAction(b) {
  const what = b.dataset.ai;
  if (what === "gptin" || what === "gptother" || what === "gptconsent") {
    aiCfgKeep();
    b.disabled = true;
    let r;
    try { r = await api("/api/ai/chatgpt/signin", { mode: what === "gptother" ? "other" : what === "gptconsent" ? "consent" : "", account: b.dataset.acct || "" }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    if (!r.ok) { b.disabled = false; toast(r.err || "无法开始登录", 5000); return; }
    AI.cfg = r.ai; await gptRedraw(); gptFollow(); return;
  }
  if (what === "gptcancel") {
    let r; try { r = await api("/api/ai/chatgpt/cancel", {}); } catch (e) { r = { ok: false }; }
    if (r.ok) AI.cfg = r.ai;
    return gptRedraw();
  }
  if (what === "gptreopen") {
    let r; try { r = await api("/api/ai/chatgpt/reopen", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    if (!r.ok) toast(r.err || "无法打开浏览器", 4000);
    return;
  }
  if (what === "gptout") {
    if (!confirm("退出 ChatGPT 账号？\n\n将在 OpenAI 撤销本软件的授权，并删除保存在本机的登录凭据。")) return;
    b.disabled = true;
    let r; try { r = await api("/api/ai/chatgpt/signout", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    if (!r.ok) { b.disabled = false; toast(r.err || "退出失败", 5000); return; }
    AI.cfg = r.ai;
    if (AI.cfgForm) { delete AI.cfgForm.models.chatgpt; if (AI.cfgForm.names) delete AI.cfgForm.names.chatgpt; }
    toast(r.revoked ? "已退出 ChatGPT 账号" : "已退出登录，但未能确认已在 OpenAI 撤销授权。可在 ChatGPT 的设置中断开本软件", r.revoked ? 3000 : 9000);
    return gptRedraw();
  }
  if (what === "gptok") { // the note that the plan is in use was read
    try { const r = await api("/api/ai/chatgpt/welcomed", {}); if (r.ok) AI.cfg = r.ai; } catch (e) {}
    if (!AI.cfgForm) return closeModal();
    await openAICfg(true);
    gptModelsSoon();
  }
}

// ---------- the pipeline ----------

// under the AI's input box, while the plan is what the AI uses
function gptComposerHTML() {
  const c = AI.cfg || {};
  return gptOn(c) && c.ready ? gptUseHTML() : "";
}
// the status rows: which account
function gptStatRow(row) {
  const c = AI.cfg || {}, g = c.chatgpt || {};
  if (!gptOn(c) || !c.ready) return "";
  return row("ok", "AI 服务", `<span>ChatGPT 账号</span><span class="sub" data-i18n="off">　${esc(g.email || "")}</span>`, `<span class="gptrow">${gptUseHTML()}</span>`);
}
// the dialog's 「测试连接」 / 「选择模型」 meeting the plan's limit: 「管理用量」 next to what was said
function gptTestErr(out, r) {
  if (r.code !== "usage") return false;
  out.className = "aitest err";
  out.innerHTML = `${esc(r.err)} <button class="btn small primary" data-link="gptusage">管理用量</button>`;
  return true;
}
// an error step with what the player can do about it: the plan's usage limit (「管理用量」 first), or the
// sign-in to see to
function gptStepHTML(s) {
  if (s.code === "usage") {
    return `<div class="aistep err gptlimit"><b>ChatGPT：已达到用量上限</b><div class="t">${esc(s.text)}</div>
      <div class="btnrow"><button class="btn small primary" data-link="gptusage">管理用量</button></div></div>`;
  }
  if (s.code === "signin" || s.code === "plan") {
    return `<div class="aistep err">${esc(s.text)}<div class="btnrow"><button class="btn small" data-ai="cfg">打开「AI 服务」</button></div></div>`;
  }
  return "";
}
