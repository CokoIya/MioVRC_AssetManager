// What the AI's answers took and cost: a line in the pipeline's status (this conversation, and today), and a
// window with every model used — today, this month, since counting began — at the prices of the price list
// (DeepSeek's own; for the others, what the player enters), and what is left on a DeepSeek or Kimi account.
// The amounts are estimates: the services' own bills are what counts.
"use strict";

const AIU = { r: null, bal: null, balErr: "", balBusy: false, edit: null, seq: 0, ev: null, evTimer: 0 };

function fmtTok(n) {
  n = n || 0;
  if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e8 ? 0 : n >= 1e7 ? 1 : 2) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e5 ? 0 : 1) + "k";
  return String(n);
}
function fmtMoney1(cur, v) {
  const s = cur === "USD" ? "$" : "¥";
  return s + (v >= 100 ? v.toFixed(0) : v >= 1 ? v.toFixed(2) : v >= 0.01 ? v.toFixed(3) : v > 0 ? v.toFixed(4) : "0");
}
function fmtMoney(cost) { return Object.keys(cost || {}).sort().map(k => fmtMoney1(k, cost[k])).join(" + "); }
// the share of the input the service read from its cache (cheaper): "" when it said nothing about its cache
function hitRate(u) {
  u = u || {};
  const all = (u.in || 0) + (u.hit || 0) + (u.write || 0);
  return all > 0 && u.hit > 0 ? Math.round(u.hit / all * 100) + "%" : "";
}
// a usage in short: the tokens, and about what they cost (or that a model has no price to count them at, or that
// they are on the player's ChatGPT plan)
function usageShort(u) {
  u = u || {};
  const m = fmtMoney(u.cost), hr = hitRate(u);
  const plan = u.plan ? ` <span class="muted small">${u.plan >= u.tokens ? "ChatGPT 套餐" : `<span>其中 ChatGPT 套餐</span> ${fmtTok(u.plan)}`}</span>` : "";
  return `<b>${fmtTok(u.tokens)}</b> tokens${m ? ` <span class="muted">≈ ${m}</span>` : ""}${plan}${u.unpriced && u.tokens ? ` <span class="muted small">${m || u.plan ? "（部分模型未设置价格）" : "（未设置价格）"}</span>` : ""}${hr ? ` <span class="muted small"><span>缓存命中</span> ${hr}</span>` : ""}`;
}
// the pipeline's status line
function aiUsageRow(row) {
  const u = (AI.sess || {}).usage;
  if (!u) return "";
  const pk = u.peak ? `<span class="sub warnline"><span>DeepSeek 当前为高峰时段，价格为平时的 2 倍</span> <span>（北京时间 ${esc(u.peak.until)} 后恢复）</span></span>` : "";
  return row("", "用量", `<span>本次对话</span> ${usageShort(u.conv)}<span class="sub"><span>今日</span> ${usageShort(u.today)}</span>${pk}`,
    `<button class="btn small ghost" data-aiu="open">明细</button>`);
}
// under 「运行流水线」: what a run took on average lately, with this model
function aiEstimateHTML() {
  const u = (AI.sess || {}).usage, e = u && u.estimate, c = AI.cfg || {};
  if (!e || !c.ready || PIPE.noAI || ($("#pipe_noai") || {}).checked) return "";
  const m = fmtMoney(e.cost);
  return `<span>预计用量</span> <b>${fmtTok(e.tokens)}</b> tokens${m ? ` ≈ ${m}` : ""} <span class="muted">（<span>按最近</span> ${e.runs} <span>次运行的平均</span>）</span>`;
}

async function aiUsageOpen() {
  const m = $("#modal");
  m.dataset.kind = "aiusage";
  AIU.edit = null; AIU.bal = null; AIU.balErr = "";
  m.innerHTML = `<div class="mhead">AI 用量与花费</div><div class="mbody"><div class="muted">正在读取…</div></div>`;
  showModal();
  await aiUsageLoad();
  if (AIU.r && AIU.r.balance) aiUsageBalance();
}
async function aiUsageLoad() {
  const seq = ++AIU.seq;
  let r;
  try { r = await api("/api/ai/usage", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (seq !== AIU.seq) return;
  if (!r.ok) { toast(r.err || "无法读取用量", 4000); return; }
  AIU.r = r; aiUsageDraw();
  aiEvalLoad();
}
// ---------- the evaluation set: the samples of the runs, asked again of the model set up now ----------
async function aiEvalLoad(path) {
  let r;
  try { r = await api(path || "/api/ai/eval", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!r.ok) { toast(r.err || "操作失败", 5000); if (path) return; }
  else AIU.ev = r;
  if (!aiUsageOpenNow()) return;
  aiUsageDraw();
  clearTimeout(AIU.evTimer);
  if (((AIU.ev || {}).runs || []).some(x => x.running)) AIU.evTimer = setTimeout(() => aiEvalLoad(), 1500);
}
function aiEvalHTML() {
  const ev = AIU.ev; if (!ev) return "";
  const runs = ev.runs || [], now = runs.find(x => x.running);
  const avg = (x, f) => x.done ? f(x) : "—";
  const pct = (n, x) => x.done ? Math.round(n / x.done * 100) + "%" : "—";
  const rows = runs.map(x => `<tr><td>${esc(new Date(x.at * 1000).toLocaleString())}${x.stopped ? `<div class="sub">已停止</div>` : ""}</td>
    <td><div data-i18n="off">${esc(x.model)}</div><div class="sub">${esc(x.label)}</div></td>
    <td class="r">${x.done}/${x.samples}</td><td class="r">${pct(x.first, x)}</td><td class="r">${pct(x.fixed, x)}</td><td class="r">${pct(x.failed + x.errors, x)}</td>
    <td class="r">${avg(x, x => fmtTok(Math.round(x.tokens / x.done)))}</td><td class="r">${avg(x, x => fmtMoney(Object.fromEntries(Object.entries(x.cost || {}).map(([k, v]) => [k, v / x.done]))) || "—")}</td>
    <td class="r">${avg(x, x => (x.seconds / x.done).toFixed(1) + " s")}</td></tr>`).join("");
  return `<div class="aieval"><div class="t"><b>评测</b> <span class="muted small"><span>已保存样本</span> ${ev.samples}</span></div>
    <div class="hint2">每次用 AI 运行流水线时，自动保存发给 AI 的菜单请求（不含截图和 API Key），最多保留 60 个。评测用当前的 AI 服务和模型重新请求这些样本，比较方案的通过率、用量、花费和用时；只请求 AI，不改动任何工程。</div>
    <div class="btnrow">${now ? `<span class="muted"><span>正在评测</span> ${now.done}/${now.samples}…</span><button class="btn small" data-aiu="evalstop">停止</button>`
      : `<button class="btn small" data-aiu="eval"${ev.samples ? "" : " disabled"}>用当前模型评测</button>`}<button class="btn small ghost" data-aiu="evalopen">打开样本文件夹</button>${ev.samples && !now ? `<button class="btn small ghost" data-aiu="evalclear">清除样本</button>` : ""}</div>
    ${runs.length ? `<table class="atable evtable"><thead><tr><th>时间</th><th>模型</th><th class="r">样本</th><th class="r">直接通过</th><th class="r">修改后通过</th><th class="r">未通过</th><th class="r">平均 tokens</th><th class="r">平均花费</th><th class="r">平均用时</th></tr></thead><tbody>${rows}</tbody></table>` : ""}</div>`;
}
async function aiUsageBalance() {
  AIU.balBusy = true; aiUsageDraw();
  let r;
  try { r = await api("/api/ai/balance", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  AIU.balBusy = false;
  if (r.ok) { AIU.bal = r.balance; AIU.balErr = ""; } else { AIU.bal = null; AIU.balErr = r.err || "查询失败"; }
  aiUsageDraw();
}
function aiUsageOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "aiusage"; }
function aiUsageCell(u) {
  if (!u || !u.tokens) return `<span class="muted">—</span>`;
  const m = fmtMoney(u.cost), hr = hitRate(u);
  return `<b>${fmtTok(u.tokens)}</b><div class="sub">${m ? "≈ " + m : u.plan && !u.unpriced ? `<span>ChatGPT 套餐</span>` : `<span>未设置价格</span>`}</div>${hr ? `<div class="sub"><span>缓存命中</span> ${hr}</div>` : ""}`;
}
function aiPriceHTML(p) {
  if (!p) return `<span class="muted">未设置价格</span>`;
  const s = v => fmtMoney1(p.currency, v).replace(/\.?0+$/, "");
  return `<span class="pr"><span>输入（缓存未命中）</span> ${s(p.in)}</span><span class="pr"><span>输入（缓存命中）</span> ${s(p.hit)}</span><span class="pr"><span>输出</span> ${s(p.out)}</span>${p.peak ? `<span class="pr muted"><span>高峰时段</span> ×${p.peak}</span>` : ""}`;
}
function aiUsageDraw() {
  if (!aiUsageOpenNow() || !AIU.r) return;
  const r = AIU.r, ms = r.models || [];
  let bal = "";
  if (r.provider === "chatgpt") { // the plan's own usage and limits are ChatGPT's to show
    bal = `<div class="aibal"><span class="k">ChatGPT 套餐</span><span class="muted small">套餐的用量和上限以 ChatGPT 中的显示为准</span><button class="linkbtn" data-link="gptusage">管理用量</button></div>`;
  } else if (r.balance) {
    const b = AIU.bal;
    bal = `<div class="aibal"><span class="k">账户余额</span>${AIU.balBusy ? `<span class="muted">正在查询…</span>`
      : b ? `<b>${fmtMoney1(b.currency, b.total)}</b><span class="muted small"><span>${r.provider === "kimi" ? "其中代金券" : "其中赠送"}</span> ${fmtMoney1(b.currency, b.granted)}<span>${r.provider === "kimi" ? "，现金" : "，充值"}</span> ${fmtMoney1(b.currency, b.paid)}</span>${b.usable ? "" : `<span class="err small">余额不足，无法调用</span>`}`
      : `<span class="err small">${esc(AIU.balErr || "未查询")}</span>`}
      <button class="linkbtn" data-aiu="bal"${AIU.balBusy ? " disabled" : ""}>刷新</button></div>`;
  }
  const rows = ms.map((m, i) => `<tr${m.provider === r.provider && m.model === r.model ? ` class="cur"` : ""}>
    <td><div data-i18n="off">${esc(m.model)}</div><div class="sub">${esc(m.label)}${m.provider === r.provider && m.model === r.model ? `<span>（当前使用）</span>` : ""}</div></td>
    <td class="r">${aiUsageCell(m.today)}</td><td class="r">${aiUsageCell(m.month)}</td><td class="r">${aiUsageCell(m.all)}</td>
    <td class="prices">${m.plan ? `<span class="muted">ChatGPT 套餐额度</span><div class="sub"><button class="linkbtn" data-link="gptusage">管理用量</button></div>`
      : `${aiPriceHTML(m.price)}<div class="sub">${m.price ? (m.builtin ? `<span>官方价格</span>` : `<span>自定义价格</span>`) : ""}<button class="linkbtn" data-aiu="edit" data-i="${i}">${m.price ? "修改" : "设置价格"}</button></div>`}</td></tr>`).join("");
  const since = r.since ? new Date(r.since * 1000).toLocaleDateString() : "";
  $("#modal").innerHTML = `<div class="mhead">AI 用量与花费</div><div class="mbody aiuse">
    ${bal}
    ${ms.length ? `<table class="atable"><thead><tr><th>模型</th><th class="r">今日</th><th class="r">本月</th><th class="r">累计</th><th>价格（每百万 tokens）</th></tr></thead><tbody>${rows}</tbody></table>`
      : `<div class="aiempty">尚无用量记录。使用 AI 运行流水线后，将在这里按模型统计。</div>`}
    ${aiPriceFormHTML()}
    <div class="aibudget"><label for="aiu_budget"><span>单次运行上限</span></label><input id="aiu_budget" type="number" min="0" step="0.1" value="${r.budget ? +(r.budget / 1e6).toFixed(2) : 0}" inputmode="decimal"><span><span>百万 tokens</span></span>
      <button class="btn small" data-aiu="budget">保存</button><span class="hint2"><span>一次运行用到上限时先暂停，询问是否继续；0 表示不限。</span></span></div>
    ${aiEvalHTML()}
    <p class="hint">金额按上表价格估算，仅供参考，以服务商的账单为准。DeepSeek 在北京时间工作日 9:00–12:00、14:00–18:00 为高峰时段，价格为其余时段的 2 倍，法定节假日也按高峰估算。其他服务商的价格请按其官网填写。</p>
    ${ms.some(m => m.plan) ? `<p class="hint">「ChatGPT 账号」的用量计入你的 ChatGPT 套餐，不另外计费；此处的 tokens 仅为本软件的统计。</p>` : ""}
    ${r.pricesNote ? `<p class="hint">${esc(r.pricesNote)}</p>` : ""}
    ${since ? `<p class="hint"><span>统计开始于</span> ${esc(since)}</p>` : ""}
  </div><div class="mfoot">${ms.length ? `<button class="btn ghost" data-aiu="clear">清零统计</button>` : ""}<span class="spacer"></span><button class="btn" data-m="close">关闭</button></div>`;
}
// the price of one model, as the player gives it (per million tokens)
function aiPriceFormHTML() {
  const m = AIU.edit != null && (AIU.r.models || [])[AIU.edit];
  if (!m) return "";
  const p = m.price || { currency: m.provider === "openai" || m.provider === "claude" ? "USD" : "CNY", in: "", hit: "", out: "" };
  const num = (id, label, v) => `<label class="pf"><span>${label}</span><input id="${id}" type="number" min="0" step="any" value="${v === "" ? "" : esc(String(v))}" inputmode="decimal"></label>`;
  return `<div class="priceform"><div class="t"><span>设置价格</span> <b data-i18n="off">${esc(m.model)}</b></div>
    <div class="pfrow"><label class="pf"><span>币种</span><select id="pr_cur"><option value="CNY"${p.currency === "CNY" ? " selected" : ""}>人民币（¥）</option><option value="USD"${p.currency === "USD" ? " selected" : ""}>美元（$）</option></select></label>
      ${num("pr_in", "输入（缓存未命中）", p.in)}${num("pr_hit", "输入（缓存命中）", p.hit)}${num("pr_out", "输出", p.out)}</div>
    <div class="hint2"><span>每百万 tokens 的价格。缓存命中的输入不填时按 0 计算。</span>${m.price && m.price.peak ? `<span>高峰时段的倍数保持不变。</span>` : ""}</div>
    <div class="btnrow"><button class="btn small primary" data-aiu="psave">保存价格</button>${m.price && !m.builtin ? `<button class="btn small" data-aiu="pdel">${m.provider === "deepseek" ? "恢复官方价格" : "清除价格"}</button>` : ""}<button class="btn small ghost" data-aiu="pcancel">取消</button></div></div>`;
}
async function aiUsageAct(el) {
  const a = el.dataset.aiu;
  if (a === "open") return aiUsageOpen();
  if (a === "bal") return aiUsageBalance();
  if (a === "edit") { AIU.edit = +el.dataset.i; aiUsageDraw(); const f = $("#pr_in"); if (f) f.focus(); return; }
  if (a === "pcancel") { AIU.edit = null; return aiUsageDraw(); }
  if (a === "psave" || a === "pdel") {
    const m = (AIU.r.models || [])[AIU.edit]; if (!m) return;
    let price = null;
    if (a === "psave") {
      const v = id => { const s = ($("#" + id) || {}).value || ""; return s.trim() === "" ? 0 : Number(s); };
      price = { currency: $("#pr_cur").value, in: v("pr_in"), hit: v("pr_hit"), out: v("pr_out") };
      if ([price.in, price.hit, price.out].some(x => !isFinite(x) || x < 0)) { toast("价格须为不小于 0 的数字"); return; }
    }
    let r;
    try { r = await api("/api/ai/usage/price", { provider: m.provider, model: m.model, price }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    if (!r.ok) { toast(r.err || "保存失败", 4000); return; }
    AIU.r = r; AIU.edit = null; aiUsageDraw(); toast(price ? "已保存价格" : "已清除自定义价格"); aiPoll();
    return;
  }
  if (a === "budget") {
    const v = Number(($("#aiu_budget") || {}).value || 0);
    if (!isFinite(v) || v < 0) { toast("上限须为不小于 0 的数字"); return; }
    let r;
    try { r = await api("/api/ai/usage/budget", { tokens: Math.round(v * 1e6) }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    if (!r.ok) { toast(r.err || "保存失败", 4000); return; }
    AIU.r = r; aiUsageDraw(); toast(v ? "已保存上限" : "已取消上限");
    return;
  }
  if (a === "eval") return aiEvalLoad("/api/ai/eval/run");
  if (a === "evalstop") return aiEvalLoad("/api/ai/eval/stop");
  if (a === "evalopen") return aiEvalLoad("/api/ai/eval/open");
  if (a === "evalclear") {
    if (!confirm("清除全部评测样本？\n\n已有的评测结果会保留。")) return;
    return aiEvalLoad("/api/ai/eval/clear");
  }
  if (a === "clear") {
    if (!confirm("清零全部用量统计？\n\n已设置的价格会保留。服务商账户中的余额和账单不受影响。")) return;
    let r;
    try { r = await api("/api/ai/usage/clear", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    if (!r.ok) { toast(r.err || "操作失败", 4000); return; }
    AIU.r = r; AIU.edit = null; aiUsageDraw(); toast("已清零"); aiPoll();
  }
}
