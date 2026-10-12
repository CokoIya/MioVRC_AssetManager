// What the AI's answers took and cost: a line in the pipeline's status (this conversation, and today), and a
// window with every model used — today, this month, since counting began — at the prices of the price list
// (DeepSeek's own; for the others, what the player enters), and what is left on a DeepSeek or Kimi account.
// The amounts are estimates: the services' own bills are what counts.
"use strict";

const AIU = { r: null, bal: null, balErr: "", balBusy: false, edit: null, seq: 0 };

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
// a usage in short: the tokens, and about what they cost (or that a model has no price to count them at)
function usageShort(u) {
  u = u || {};
  const m = fmtMoney(u.cost);
  return `<b>${fmtTok(u.tokens)}</b> tokens${m ? ` <span class="muted">≈ ${m}</span>` : ""}${u.unpriced && u.tokens ? ` <span class="muted small">${m ? "（部分模型未设置价格）" : "（未设置价格）"}</span>` : ""}`;
}
// the pipeline's status line
function aiUsageRow(row) {
  const u = (AI.sess || {}).usage;
  if (!u) return "";
  return row("", "用量", `<span>本次对话</span> ${usageShort(u.conv)}<span class="sub"><span>今日</span> ${usageShort(u.today)}</span>`,
    `<button class="btn small ghost" data-aiu="open">明细</button>`);
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
  const m = fmtMoney(u.cost);
  return `<b>${fmtTok(u.tokens)}</b><div class="sub">${m ? "≈ " + m : `<span>未设置价格</span>`}</div>`;
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
  if (r.balance) {
    const b = AIU.bal;
    bal = `<div class="aibal"><span class="k">账户余额</span>${AIU.balBusy ? `<span class="muted">正在查询…</span>`
      : b ? `<b>${fmtMoney1(b.currency, b.total)}</b><span class="muted small"><span>${r.provider === "kimi" ? "其中代金券" : "其中赠送"}</span> ${fmtMoney1(b.currency, b.granted)}<span>${r.provider === "kimi" ? "，现金" : "，充值"}</span> ${fmtMoney1(b.currency, b.paid)}</span>${b.usable ? "" : `<span class="err small">余额不足，无法调用</span>`}`
      : `<span class="err small">${esc(AIU.balErr || "未查询")}</span>`}
      <button class="linkbtn" data-aiu="bal"${AIU.balBusy ? " disabled" : ""}>刷新</button></div>`;
  }
  const rows = ms.map((m, i) => `<tr${m.provider === r.provider && m.model === r.model ? ` class="cur"` : ""}>
    <td><div data-i18n="off">${esc(m.model)}</div><div class="sub">${esc(m.label)}${m.provider === r.provider && m.model === r.model ? `<span>（当前使用）</span>` : ""}</div></td>
    <td class="r">${aiUsageCell(m.today)}</td><td class="r">${aiUsageCell(m.month)}</td><td class="r">${aiUsageCell(m.all)}</td>
    <td class="prices">${aiPriceHTML(m.price)}<div class="sub">${m.price ? (m.builtin ? `<span>官方价格</span>` : `<span>自定义价格</span>`) : ""}<button class="linkbtn" data-aiu="edit" data-i="${i}">${m.price ? "修改" : "设置价格"}</button></div></td></tr>`).join("");
  const since = r.since ? new Date(r.since * 1000).toLocaleDateString() : "";
  $("#modal").innerHTML = `<div class="mhead">AI 用量与花费</div><div class="mbody aiuse">
    ${bal}
    ${ms.length ? `<table class="atable"><thead><tr><th>模型</th><th class="r">今日</th><th class="r">本月</th><th class="r">累计</th><th>价格（每百万 tokens）</th></tr></thead><tbody>${rows}</tbody></table>`
      : `<div class="aiempty">尚无用量记录。使用 AI 运行流水线后，将在这里按模型统计。</div>`}
    ${aiPriceFormHTML()}
    <p class="hint">金额按上表价格估算，仅供参考，以服务商的账单为准。DeepSeek 在北京时间工作日 9:00–12:00、14:00–18:00 为高峰时段，价格为其余时段的 2 倍，法定节假日也按高峰估算。其他服务商的价格请按其官网填写。</p>
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
  if (a === "clear") {
    if (!confirm("清零全部用量统计？\n\n已设置的价格会保留。服务商账户中的余额和账单不受影响。")) return;
    let r;
    try { r = await api("/api/ai/usage/clear", {}); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    if (!r.ok) { toast(r.err || "操作失败", 4000); return; }
    AIU.r = r; AIU.edit = null; aiUsageDraw(); toast("已清零"); aiPoll();
  }
}
