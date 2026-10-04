// the avatar check-up before upload, and covers made from prefabs
"use strict";
// CK.recs: what the server last said about a project's check-up ({record, can, why}); busy / err: a run under
// way or failed; open: the detail lists the player unfolded; seen: how long each project's run record was when
// it was last idle (a run that ends is noticed by it); cov: cover status by asset; batch: the cover task
// fixBusy: the one-click fix under way (by project); fixed: this session's fixes by project and kind, as the
// server answered them; lastTex: the revert record 「恢复」 takes back next (by project: the newest texture fix
// that still can be, as the server says); rep: the report card open
const CK = { recs: {}, busy: {}, err: {}, open: new Set(), seen: {}, asked: {}, cov: {}, covBusy: {}, batch: null, batchSeen: 0, fixBusy: {}, fixed: {}, lastTex: {}, rep: null };
const RANKS = ["Excellent", "Good", "Medium", "Poor", "VeryPoor"];
const RANK_ZH = { Excellent: "极佳", Good: "良好", Medium: "中等", Poor: "较差", VeryPoor: "极差" };
const RANK_EN = { Excellent: "Excellent", Good: "Good", Medium: "Medium", Poor: "Poor", VeryPoor: "Very Poor" };
const CK_NOTE = "上传时 Modular Avatar、Avatar Optimizer（AAO）、VRCFury 会在构建阶段改动模型，实际数值会有出入（AAO 通常会减少网格、材质槽和骨骼的数量）。";

function ckNum(v) {
  if (typeof v !== "number") return v === undefined || v === null ? "" : String(v);
  return Math.abs(v) >= 10000 ? v.toLocaleString("en-US") : String(v);
}
function ckRankCls(r) { return RANKS.includes(r) ? "r" + RANKS.indexOf(r) : ""; }
// a rank as a chip: Chinese, with the name VRChat shows as its tooltip (or beside it, where it is said once)
function ckRank(r, who, withEn, tip) {
  if (!RANK_ZH[r]) return "";
  return `<span class="ckrank ${ckRankCls(r)}" title="${esc(tip || (who ? who + "：" : "") + RANK_EN[r])}">${who ? `<span class="who">${esc(who)}</span>` : ""}<span>${RANK_ZH[r]}</span>${withEn ? `<span class="en">${RANK_EN[r]}</span>` : ""}</span>`;
}
// an item's rank on PC, with Quest's in the tooltip
function ckItemRank(it) {
  return ckRank(it.rating, "", false, "PC：" + (RANK_EN[it.rating] || "") + (RANK_EN[it.ratingQuest] ? "　Quest：" + RANK_EN[it.ratingQuest] : "")) || `<span class="ckrank blank"></span>`;
}
function ckValue(it) {
  const v = it.text ? it.text : ckNum(it.value);
  return `<span class="ckval">${it.atLeast ? `<span class="pre">至少</span>` : ""}<b>${esc(v)}</b>${it.limit ? `<span class="lim">/ ${esc(ckNum(it.limit))}</span>` : ""}${it.unit ? `<span class="u">${esc(it.unit)}</span>` : ""}</span>`;
}
// how full: against the upload's limit, or against what the Poor rank allows
function ckMeter(it) {
  const max = it.limit || (it.tiers && it.tiers[3]) || 0;
  if (!max || typeof it.value !== "number") return `<span class="ckmeter none"></span>`;
  const pct = Math.max(0, Math.min(100, Math.round(it.value / max * 100)));
  const cls = it.limit ? (it.value > max ? "fail" : pct >= 85 ? "warn" : "ok") : ckRankCls(it.rating);
  return `<span class="ckmeter ${cls}" role="img" aria-label="${pct}%"><i style="width:${pct}%"></i></span>`;
}
// what the next better rank allows
function ckNext(it) {
  const i = RANKS.indexOf(it.rating);
  if (i <= 0 || !it.tiers || it.tiers[i - 1] === undefined) return "";
  return `<span class="cknext"><span>下一档</span>${ckRank(RANKS[i - 1])}<span>≤ ${esc(ckNum(it.tiers[i - 1]))}</span></span>`;
}
function ckLine(l) {
  return `<li${l.w ? ` class="w"` : ""}><span class="t" data-i18n="off">${esc(l.t)}</span>${l.m ? `<span class="m" data-i18n="off">${esc(l.m)}</span>` : ""}${l.n ? `<span class="n">${esc(l.n)}</span>` : ""}${
    l.v !== undefined ? `<span class="n"><b>${esc(ckNum(l.v))}</b>${l.u ? `<span>${esc(l.u)}</span>` : ""}</span>` : ""}${l.f ? `<span class="f"><span>可能是</span><b data-i18n="off">${esc(l.f)}</b></span>` : ""}</li>`;
}
// one item with everything it has to say: the figure, the advice, the details to unfold
function ckRow(it, where, path) {
  const id = where + ":" + it.id, lines = it.details || [];
  return `<div class="ckrow ${esc(it.level)}">
    <div class="ckline"><i class="ckmark"></i><span class="lb">${esc(it.label)}</span>${ckValue(it)}${ckMeter(it)}${ckItemRank(it)}</div>
    ${it.level === "warn" && it.group === "figure" ? ckNext(it) : ""}
    ${it.advice ? `<div class="ckadv">${esc(it.advice)}</div>` : ""}
    ${lines.length ? `<details class="ckdet" data-ckd="${esc(id)}"${CK.open.has(id) ? " open" : ""}><summary><span>${esc(it.detailsTitle || "详情")}</span><span class="n">${lines.length}</span></summary><ul>${lines.map(ckLine).join("")}</ul></details>` : ""}
    ${path ? ckFixHTML(it, path) + ckDoneHTML(path, CK_FIX_KIND[it.id], where) : ""}
  </div>`;
}

// ---------- one-click fixes ----------
// What the plugin can put right by itself (by the item's id), and what the other items need instead of a button.
const CK_FIX_KIND = { textures: "textures", "fig.lights": "lights", missing: "missing" };
const CK_FIX_ITEM = { textures: "textures", lights: "fig.lights", missing: "missing" };
const CK_FIX_LABEL = { textures: "贴图降到 2048", lights: "移除灯光", missing: "移除丢失的脚本" };
// (missing scripts are usually a plugin that is not installed: the button is not the first thing to reach for)
const CK_FIX_HINT = { textures: "改动贴图的导入设置并重新导入，可恢复", lights: "可用 Ctrl+Z 或「撤销上一步」撤销", missing: "仅在确认这些组件已不再需要时使用；若是插件未安装，请先安装插件" };
const CK_NOFIX = { materials: "需要安装对应的着色器包", params: "需要决定减少哪些开关", physbones: "需要决定移除哪些衣服或物理", menu: "需要在菜单资产中整理控件", "fig.bounds": "需要在 Unity 中找出离模型过远的物体" };
// the button under an item, when there is something to fix; the fix runs only in the Unity that has the project open
function ckFixHTML(it, path) {
  const kind = CK_FIX_KIND[it.id];
  if (!kind) return CK_NOFIX[it.id] && (it.level === "fail" || it.level === "warn") ? `<div class="cknofix"><span>无法一键处理：</span>${CK_NOFIX[it.id]}</div>` : "";
  if (!(typeof it.value === "number" && it.value > 0)) return "";
  // the 4096 px textures that are left are a package's, or have no import settings: nothing for the button to do
  if (kind === "textures" && it.fixable === 0) return `<div class="cknofix"><span>无法一键处理：</span>这些贴图不在 Assets 文件夹内，或没有可调整的导入设置</div>`;
  const st = CK.recs[path] || {}, busy = CK.fixBusy[path], can = !!st.can && !busy && !CK.busy[path];
  return `<div class="ckfix"><button class="btn small" data-ckfix="${kind}"${can ? "" : " disabled"}${!st.can && st.why ? ` title="${esc(st.why)}"` : ""}>${busy === kind ? "正在处理…" : CK_FIX_LABEL[kind]}</button><span class="hint">${CK_FIX_HINT[kind]}</span></div>`;
}
// what a fix did, in a line
function ckFixText(f) {
  if (f.reverted !== undefined) return (f.missing || []).length ? `已恢复 ${f.reverted} 张贴图的导入设置，${f.missing.length} 张未找到（可能已移动或改名），恢复记录已保留` : `已恢复 ${f.reverted} 张贴图的导入设置`;
  // read from the record in the project: the fix's own answer never arrived (stopped, or broken off half way)
  if (f.adopted) return `工程中留有一次贴图处理的恢复记录（${f.changed} 张贴图），可点击「恢复」改回原设置`;
  if (!f.changed) return "没有改动任何内容";
  // (the figure is of the textures that were changed, not the avatar's whole)
  if (f.kind === "textures") return f.memoryBefore ? `已将 ${f.changed} 张贴图的 Max Size 降到 ${f.max}，所处理贴图的显存估计由 ${f.memoryBefore} MB 降到 ${f.memoryAfter} MB` : `已将 ${f.changed} 张贴图的 Max Size 降到 ${f.max}`;
  if (f.kind === "lights") return `已移除 ${f.changed} 个灯光组件`;
  return `已移除 ${f.changed} 个丢失脚本的组件`;
}
function ckFixLine(l) {
  if (l.path) return `<li><span class="t">${esc(l.path)}</span>${l.from ? `<span class="n"><b>${esc(l.from)}</b><span>→</span><b>${esc(l.to)}</b></span>` : ""}${(l.platforms || []).length ? `<span class="f">${esc(l.platforms.join("、"))}</span>` : ""}</li>`;
  return ckLine(l);
}
// the note under the item (or, with no row for it on the page, in the 已处理 block): what was done and the way back
function ckDoneHTML(path, kind, where) {
  const f = kind && (CK.fixed[path] || {})[kind]; if (!f) return "";
  const lost = f.reverted !== undefined ? f.missing || [] : [];
  const id = where + ":fix:" + kind, lines = (f.items || []).concat((f.skipped || []).map(l => ({ t: l.t, n: l.n })), lost.map(p => ({ t: p, n: "未找到" }))), busy = !!CK.fixBusy[path];
  // 「恢复」 for as long as the server holds a texture fix that can be taken back, whatever the note says: after a
  // click that changed nothing, after a revert that could not find a texture, and for the fix before the one just taken back
  const back = kind === "textures" && CK.lastTex[path]
    ? `<button class="btn small ghost" data-ckrevert="1"${busy || !(CK.recs[path] || {}).can ? " disabled" : ""}>${CK.fixBusy[path] === "revert" ? "正在恢复…" : "恢复"}</button>`
    : f.reverted === undefined && f.undo ? `<span class="how">可用 Ctrl+Z 或「撤销上一步」撤销</span>` : "";
  return `<div class="ckdone"><span class="tag ok">${f.reverted !== undefined ? (lost.length ? "部分已恢复" : "已恢复原设置") : "已处理"}</span><span class="what">${esc(ckFixText(f))}</span><span class="when">${esc(fmtTime(f.at))}</span>${back}${
    lines.length ? `<details class="ckdet" data-ckd="${esc(id)}"${CK.open.has(id) ? " open" : ""}><summary><span>处理的内容</span><span class="n">${lines.length}</span></summary><ul>${lines.map(ckFixLine).join("")}</ul></details>` : ""}</div>`;
}
// this session's fixes whose item has no row on the page (a figure that went to the grid)
function ckFixesHTML(path, where, rowIds) {
  const done = Object.keys(CK.fixed[path] || {}).filter(k => !rowIds.has(CK_FIX_ITEM[k]));
  return done.length ? `<div class="ckfixes">${done.map(k => `<div class="ckfixrow"><span class="lb">${CK_FIX_LABEL[k]}</span>${ckDoneHTML(path, k, where)}</div>`).join("")}</div>` : "";
}
async function ckFix(path, kind) {
  const st = CK.recs[path], rec = st && st.record;
  if (!rec || !CK_FIX_LABEL[kind] || CK.fixBusy[path] || CK.busy[path]) return;
  const it = ((rec.result || {}).items || []).find(x => x.id === CK_FIX_ITEM[kind]) || {}, n = it.value || 0;
  // of the 4096 px textures, the ones the fix can lower (a package's, or one without import settings, stays as it is)
  const can = typeof it.fixable === "number" ? Math.min(it.fixable, n) : n;
  const ask = kind === "textures" ? `将把模型用到的 ${can} 张 4096 像素贴图的 Max Size 改为 2048 并重新导入，压缩设置保持不变。${can < n ? `另有 ${n - can} 张不在 Assets 文件夹内或没有可调整的导入设置，不会改动。` : ""}贴图的导入设置对整个工程生效，使用这些贴图的其他模型和场景也会随之改变。导入设置不在 Unity 的撤销记录中，处理后可点击「恢复」改回原设置。是否继续？`
    : kind === "lights" ? `将移除模型下的 ${n} 个灯光组件（Light），灯光所在的物体保留。可在 Unity 中按 Ctrl+Z 或点击「撤销上一步」撤销。是否继续？`
    : `将移除模型下 ${n} 个丢失脚本的组件。如果脚本丢失是因为插件（Modular Avatar、VRCFury 等）没有安装或编译出错，请不要移除，应先安装或修复该插件：移除后，这些组件上的设置（衣服的装配、菜单项等）在场景保存后无法找回，重新安装插件也不会恢复。保存场景前可在 Unity 中按 Ctrl+Z 或点击「撤销上一步」撤销。确认这些组件已不再需要，是否继续？`;
  if (!confirm(ask)) return;
  CK.fixBusy[path] = kind; delete CK.err[path]; ckDraw();
  let r; try { r = await api("/api/checkup/fix", { project: path, kind, avatar: rec.result.path || rec.avatar }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  delete CK.fixBusy[path];
  if (!r.ok) {
    CK.err[path] = r.err || "处理失败"; ckDraw(); toast(CK.err[path], 6000);
    if (kind === "textures") { delete CK.asked[path]; ckFetch(path); } // broken off half way: the server offers 「恢复」 from the record in the project
    return;
  }
  if (r.record) ckSet(path, r);
  // a click that changed nothing leaves the earlier note, and its 「恢复」, where they are
  if (r.fix.changed || r.fix.record) (CK.fixed[path] = CK.fixed[path] || {})[kind] = r.fix;
  if (kind === "textures" && r.fix.record) CK.lastTex[path] = r.fix.record;
  if (r.recheckErr) CK.err[path] = "已处理，但重新体检失败：" + r.recheckErr;
  ckDraw();
  toast(ckFixText(r.fix), 5000);
  if (S.view === "proj" || S.view === "pipe") loadProjects(); // the card's badge
  if (r.fix.undo && pipeActive()) aiPoll(); // 「撤销上一步」 is there for a scene fix
}
async function ckRevert(path) {
  const record = CK.lastTex[path], st = CK.recs[path], rec = st && st.record;
  if (!record || !rec || CK.fixBusy[path] || CK.busy[path]) return;
  if (!confirm("将把上次处理的贴图恢复为原来的导入设置并重新导入。是否继续？")) return;
  CK.fixBusy[path] = "revert"; delete CK.err[path]; ckDraw();
  let r; try { r = await api("/api/checkup/fix/revert", { project: path, record, avatar: rec.result.path || rec.avatar }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  delete CK.fixBusy[path];
  if (!r.ok) { CK.err[path] = r.err || "恢复失败"; ckDraw(); toast(CK.err[path], 6000); return; }
  delete CK.lastTex[path];
  if (r.record) ckSet(path, r); // (the fix before this one, when there is one, is offered next: its note and its 「恢复」)
  const rev = Object.assign({ kind: "textures", at: Math.floor(Date.now() / 1000) }, r.revert);
  // the note is the revert's when nothing is left to take back, or when a texture was not found (the record is kept: 「恢复」 stays)
  if (!CK.lastTex[path] || (rev.missing || []).length) (CK.fixed[path] = CK.fixed[path] || {}).textures = rev;
  if (r.recheckErr) CK.err[path] = "已恢复，但重新体检失败：" + r.recheckErr;
  ckDraw();
  toast(ckFixText(rev), (rev.missing || []).length ? 7000 : 4000);
  if (S.view === "proj" || S.view === "pipe") loadProjects();
}
// a figure in the two-column list
function ckCell(it) {
  return `<div class="ckfig ${esc(it.level)}"${it.advice ? ` title="${esc(it.advice)}"` : ""}><span class="lb">${esc(it.label)}</span>${ckValue(it)}${ckMeter(it)}${ckItemRank(it)}</div>`;
}
function ckCounts(rec) {
  const items = (rec.result || {}).items || [];
  return { fail: items.filter(x => x.level === "fail").length, warn: items.filter(x => x.level === "warn").length, n: items.length };
}
function ckSummary(rec) {
  const c = ckCounts(rec), ranks = (rec.result || {}).ranks || {};
  const parts = [];
  if (c.fail) parts.push(`<span class="fail"><b>${c.fail}</b><span>项需处理</span></span>`);
  if (c.warn) parts.push(`<span class="warn"><b>${c.warn}</b><span>项建议优化</span></span>`);
  parts.push(`<span class="ok">${c.fail || c.warn ? "其余通过" : "全部通过"}</span>`);
  return `<div class="cksum"><span class="cnt">${parts.join(`<i>·</i>`)}</span><span class="ranks">${ckRank(ranks.pc, "PC", true)}${ckRank(ranks.quest, "Quest", true)}</span></div>`;
}
function ckMeta(rec) {
  return `<span class="ckmeta">${rec.avatar ? `<span class="av" title="${esc(rec.avatar)}">${esc(rec.avatar)}</span>` : ""}<span>${esc(fmtTime(rec.at))}</span></span>`;
}
function ckNote(rec) {
  const r = rec.result || {};
  return `<div class="cknote"><span class="tag">构建前的数值</span><span>${CK_NOTE}</span></div>${
    r.avatar && !r.sdkCalc ? `<div class="cknote"><span class="tag">近似值</span><span>未能调用 VRChat SDK 的性能计算器，以下数值由插件自行统计，不含评级。</span></div>` : ""}`;
}
// The groups. full: the pipeline page (需处理 / 建议优化 / 参数). Otherwise the project's details: what the
// upload is bound by first, then the figures.
function ckBody(rec, where, full, path) {
  const items = (rec.result || {}).items || [];
  // a light on the avatar is a row whatever its rank: it has a fix
  const isFig = it => it.group === "figure" && !(it.id === "fig.lights" && typeof it.value === "number" && it.value > 0);
  const rowIds = new Set(items.filter(it => !isFig(it) || (full && (it.level === "fail" || it.level === "warn"))).map(it => it.id));
  const fixes = path ? ckFixesHTML(path, where, rowIds) : "";
  const zero = it => isFig(it) && it.value === 0 && it.level !== "warn" && it.level !== "fail";
  const cells = list => {
    const shown = list.filter(it => !zero(it)), zeros = list.filter(zero);
    return (shown.length ? `<div class="ckgrid">${shown.map(ckCell).join("")}</div>` : "")
      + (zeros.length ? `<div class="ckzero"><span>数值为 0：</span>${zeros.map(it => `<span>${esc(it.label)}</span>`).join(`<i>、</i>`)}</div>` : "");
  };
  const order = { fail: 0, warn: 1 };
  const rows = list => list.slice().sort((x, y) => (order[x.level] ?? 2) - (order[y.level] ?? 2)).map(it => ckRow(it, where, path)).join(""); // (the sort is stable)
  let h = fixes;
  if (full) {
    const fail = items.filter(it => it.level === "fail"), warn = items.filter(it => it.level === "warn"), rest = items.filter(it => it.level !== "fail" && it.level !== "warn");
    if (fail.length) h += `<h5 class="fail"><span>需处理</span><span class="n">${fail.length}</span></h5>${rows(fail)}`;
    if (warn.length) h += `<h5 class="warn"><span>建议优化</span><span class="n">${warn.length}</span></h5>${rows(warn)}`;
    if (rest.length) h += `<h5><span>参数</span></h5>${rows(rest.filter(it => !isFig(it)))}${cells(rest.filter(isFig))}`;
    return h;
  }
  const hard = items.filter(it => !isFig(it)), figs = items.filter(isFig);
  if (hard.length) h += `<h5><span>上传限制与外观</span></h5>${rows(hard)}`;
  if (figs.length) h += `<h5><span>性能数值</span></h5>${cells(figs)}`;
  return h;
}

// ---------- the report card: a picture of the ranks and the key figures ----------
// Drawn by the page itself at twice its 1200 × 630, in the window's colors and fonts. Every label goes through
// the dictionary, so the card reads in the window's language; names and numbers are drawn as they are.
const CK_REP_FIGS = ["fig.triangles", "fig.materials", "fig.bones", "physbones", "params", "fig.textureMemory"];
const CK_REP_NAMES = { "fig.triangles": "三角面", "fig.materials": "材质槽", "fig.bones": "骨骼", physbones: "PhysBone 组件", params: "同步参数", "fig.textureMemory": "贴图显存" };
function ckDrawCard(cv, rec, project, version) {
  const W = 1200, H = 630, c = cv.getContext("2d"), t = s => I18N.t(s), r = rec.result || {}, items = r.items || [], ranks = r.ranks || {};
  cv.width = W * 2; cv.height = H * 2; c.scale(2, 2);
  const css = getComputedStyle(document.documentElement), v = (n, d) => css.getPropertyValue(n).trim() || d;
  const BG = v("--bg", "#14151f"), PANEL = v("--panel", "#191b27"), CARD = v("--card", "#1f2231"), LINE = v("--line", "#2e3247"), TEXT = v("--text", "#eceef6");
  const MUTED = v("--muted", "#9a9eb5"), FAINT = v("--faint", "#888ca5"), ACCENT = v("--accent", "#ff5a6a"), OK = v("--ok", "#3fd0a6"), WARN = v("--warn", "#ffb547"), FAIL = "#ff8a95";
  const FONT = v("--font", "system-ui, sans-serif"), RANKC = { Excellent: OK, Good: "#9be36b", Medium: "#f0e26b", Poor: WARN, VeryPoor: FAIL };
  const font = (size, weight) => { c.font = `${weight || 400} ${size}px ${FONT}`; };
  const box = (x, y, w, h, rad, fill, stroke) => {
    c.beginPath(); c.moveTo(x + rad, y); c.arcTo(x + w, y, x + w, y + h, rad); c.arcTo(x + w, y + h, x, y + h, rad); c.arcTo(x, y + h, x, y, rad); c.arcTo(x, y, x + w, y, rad); c.closePath();
    if (fill) { c.fillStyle = fill; c.fill(); }
    if (stroke) { c.strokeStyle = stroke; c.lineWidth = 1.5; c.stroke(); }
  };
  // a text cut to a width, with … (the font is set already)
  const fit = (s, max) => { s = String(s ?? ""); if (c.measureText(s).width <= max) return s; let k = s; while (k.length > 1 && c.measureText(k + "…").width > max) k = k.slice(0, -1); return k + "…"; };
  // a word that must stay whole (a rank): the font shrinks until it fits, down to min
  const fitFont = (s, max, size, weight, min) => { for (; size > min && (font(size, weight), c.measureText(s).width > max); size -= 2); font(size, weight); };
  const text = (s, x, y, color, align) => { c.fillStyle = color; c.textAlign = align || "left"; c.fillText(s, x, y); return c.measureText(s).width; };
  c.textBaseline = "alphabetic";
  c.fillStyle = BG; c.fillRect(0, 0, W, H);
  box(28, 28, W - 56, H - 56, 22, PANEL, LINE);
  // the head: the brand, the avatar, where and when
  font(20, 700); let x = 60 + text("MioVRCA", 60, 80, ACCENT);
  font(20, 400); text("  ·  " + t("上传前体检"), x, 80, MUTED);
  font(40, 700); text(fit(r.avatar || rec.avatar || "", 680), 60, 136, TEXT);
  font(16, 400);
  const when = t(fmtTime(rec.at)), proj = fit(project, 420);
  x = 60 + text((proj ? proj + "   ·   " : "") + when, 60, 172, MUTED) + 16;
  font(13, 400); const tag = t("构建前的数值"), tw = c.measureText(tag).width;
  box(x, 156, tw + 18, 24, 12, null, LINE); text(tag, x + 9, 172, FAINT);
  // the ranks: two badges
  const badge = (bx, who, rank) => {
    const col = RANKC[rank] || FAINT;
    box(bx, 58, 186, 118, 16, CARD, (RANKC[rank] || LINE));
    font(14, 500); text(who, bx + 18, 84, FAINT);
    const zh = rank ? t(RANK_ZH[rank]) : "—", en = rank ? RANK_EN[rank] : t("未评级");
    const two = zh !== en; // the English under the Chinese; alone, the one word sits in the middle
    fitFont(zh, 150, 36, 700, 22); text(fit(zh, 150), bx + 18, two ? 132 : 142, col);
    if (two) { font(15, 500); text(fit(en, 150), bx + 18, 158, col); }
  };
  badge(754, "PC", ranks.pc); badge(954, "Quest", ranks.quest);
  // the key figures
  const by = {}; for (const it of items) by[it.id] = it;
  CK_REP_FIGS.forEach((id, i) => {
    const it = by[id], cx = 60 + (i % 3) * 366, cy = 220 + Math.floor(i / 3) * 122;
    box(cx, cy, 348, 104, 14, CARD, LINE);
    font(14, 500); text(fit(t(CK_REP_NAMES[id]), 226), cx + 18, cy + 32, MUTED);
    let val = "—", unit = "", mark = "", mcol = FAINT;
    if (it) {
      val = it.text ? it.text : ckNum(it.value);
      if (it.atLeast) val = t("至少") + " " + val;
      if (it.limit) val += " / " + ckNum(it.limit);
      unit = it.unit ? t(it.unit) : "";
      if (it.rating) { mark = t(RANK_ZH[it.rating]) + (t(RANK_ZH[it.rating]) === RANK_EN[it.rating] ? "" : " " + RANK_EN[it.rating]); mcol = RANKC[it.rating] || FAINT; }
      else if (it.level === "fail") { mark = t("超出上限"); mcol = FAIL; }
      else if (it.level === "warn") { mark = t("接近上限"); mcol = WARN; }
    }
    font(14, 400); const uw = unit ? c.measureText(unit).width + 6 : 0;
    fitFont(val, 312 - uw, 30, 700, 18); const vw = text(fit(val, 312 - uw), cx + 18, cy + 78, it && it.level === "fail" ? FAIL : TEXT);
    font(14, 400); if (unit) text(unit, cx + 18 + vw + 6, cy + 78, FAINT);
    if (mark) { font(14, 600); c.fillStyle = mcol; c.beginPath(); c.arc(cx + 330 - c.measureText(mark).width - 12, cy + 27, 4, 0, Math.PI * 2); c.fill(); text(mark, cx + 330, cy + 32, mcol, "right"); }
  });
  // what the check-up found, in pills
  const counts = ckCounts(rec), pills = [];
  if (counts.fail) pills.push([t(`${counts.fail} 项需处理`), FAIL]);
  if (counts.warn) pills.push([t(`${counts.warn} 项建议优化`), WARN]);
  pills.push([t(counts.fail || counts.warn ? "其余通过" : "全部通过"), OK]);
  font(16, 600); x = 60;
  for (const [s, col] of pills) {
    const w = c.measureText(s).width + 44;
    box(x, 476, w, 38, 19, null, col);
    c.fillStyle = col; c.beginPath(); c.arc(x + 20, 495, 5, 0, Math.PI * 2); c.fill();
    text(s, x + 32, 501, col); x += w + 12;
  }
  // the foot
  c.fillStyle = LINE; c.fillRect(60, 540, W - 120, 1);
  font(14, 400); text(t("MioVRCA 上传前体检") + " · miovrc.com/vrca", 60, 576, FAINT);
  text(version ? "MioVRCA v" + version : "MioVRCA", W - 60, 576, FAINT, "right");
}
function ckReportOpen(path) {
  const st = CK.recs[path], rec = st && st.record; if (!rec || !(rec.result || {}).avatar) return;
  const p = (S.projs || []).find(x => x.path === path) || (AI.proj && AI.proj.path === path ? AI.proj : {});
  CK.rep = { path, avatar: rec.result.avatar || rec.avatar, saved: "" };
  const m = $("#modal"); m.dataset.kind = "ckrep";
  m.innerHTML = `<div class="mhead">导出报告图片</div><div class="mbody">
    <div class="ckrep"><canvas id="ck_card" width="2400" height="1260" role="img" aria-label="体检报告"></canvas></div>
    <div class="hint2">图片含模型名、工程名、评级和主要数值，为构建前的数值。可复制后直接粘贴到群聊，或保存为文件。</div>
    <div id="ck_repsaved"></div></div>
    <div class="mfoot"><button class="btn ghost" data-m="close">关闭</button><span class="spacer"></span><button class="btn" data-ckr="copy">复制图片</button><button class="btn primary" data-ckr="save">保存图片</button></div>`;
  ckDrawCard($("#ck_card"), rec, p.name || "", (S.data || {}).version);
  showModal();
}
function ckRepSavedDraw() {
  const el = $("#ck_repsaved"), rep = CK.rep; if (!el || !rep) return;
  el.innerHTML = rep.saved ? `<div class="ckrepsaved"><span>已保存到</span><span class="p" title="${esc(rep.saved)}">${esc(rep.saved)}</span><button class="btn small" data-gopen="${esc(rep.saved)}">打开所在文件夹</button></div>` : "";
}
async function ckReportCopy() {
  const cv = $("#ck_card"); if (!cv) return;
  try {
    const blob = await new Promise((res, rej) => cv.toBlob(b => b ? res(b) : rej(new Error("no blob")), "image/png"));
    await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
    toast("已复制图片，可直接粘贴到群聊", 3000);
  } catch (e) { toast("无法复制图片，请改用「保存图片」", 5000); }
}
async function ckReportSave() {
  const cv = $("#ck_card"), rep = CK.rep; if (!cv || !rep || rep.busy) return;
  const png = cv.toDataURL("image/png");
  if (png.length > 3.5e6) { toast("图片过大，未保存", 4000); return; }
  rep.busy = true;
  let r; try { r = await api("/api/checkup/report/save", { avatar: rep.avatar, png }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  rep.busy = false;
  if (!r.ok) { toast(r.err || "保存失败", 5000); return; }
  rep.saved = r.path; ckRepSavedDraw(); toast("已保存", 2500);
}

// ---------- asking the server ----------
// The server's answer for a project. The record carries the last texture fix that can still be taken back
// (texFix): 「恢复」 is offered from it after a reload too, for a fix the AI made, and for one whose answer never
// arrived (adopted from the record in the project).
function ckSet(path, r) {
  const rec = r.record || null, tf = rec && rec.texFix && rec.texFix.record ? rec.texFix : null;
  CK.recs[path] = { record: rec, can: !!r.can, why: r.why || "", at: Date.now() };
  if (tf && CK.lastTex[path] !== tf.record) { (CK.fixed[path] = CK.fixed[path] || {}).textures = tf; CK.lastTex[path] = tf.record; }
  else if (!tf && rec && CK.lastTex[path]) { // taken back elsewhere (the AI, another window)
    delete CK.lastTex[path];
    const f = (CK.fixed[path] || {}).textures; if (f && f.reverted === undefined) delete CK.fixed[path].textures;
  }
}
async function ckFetch(path) {
  if (CK.asked[path] && Date.now() - CK.asked[path] < 800) return;
  CK.asked[path] = Date.now();
  let r; try { r = await api("/api/checkup/last", { project: path }); } catch (e) { return; }
  if (!r.ok) return;
  ckSet(path, r);
  ckDraw();
}
// quiet: the page asked (after a run of the line); what it finds is shown in the panel, not shouted
async function ckRun(path, quiet) {
  if (!path || CK.busy[path]) return;
  CK.busy[path] = true; delete CK.err[path]; ckDraw();
  let r; try { r = await api("/api/checkup/run", { project: path }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  delete CK.busy[path];
  if (r.ok) ckSet(path, r);
  else CK.err[path] = r.err || "体检失败";
  ckDraw();
  if (!r.ok) { if (!quiet) toast(CK.err[path], 6000); return; }
  const c = ckCounts(r.record), go = () => { const el = $("#ck_panel"); if (el) el.scrollIntoView({ behavior: "smooth", block: "start" }); };
  toast(c.fail ? `体检完成：${c.fail} 项需处理` : c.warn ? `体检完成：${c.warn} 项建议优化` : "体检完成：全部通过", 5000, quiet && $("#ck_panel") ? go : undefined);
  if (S.view === "proj" || S.view === "pipe") loadProjects(); // the card's badge
}

// ---------- the pipeline page: the panel under 验收 ----------
function ckPanelHTML() {
  const p = AI.proj; if (!p) return "";
  const st = CK.recs[p.path], rec = st && st.record, busy = !!CK.busy[p.path], err = CK.err[p.path];
  let h = `<div class="ckp"><div class="ckhead"><h3>上传前体检</h3>${rec ? ckMeta(rec) : ""}${busy ? `<span class="ckbusy"><i class="spin"></i><span>正在体检…</span></span>` : ""}${
    rec && rec.result && rec.result.avatar ? `<button class="btn small ghost ckrep" data-ck="report" title="把评级和主要数值画成一张图片，可复制到群聊或保存">导出报告图片</button>` : ""}</div>`;
  if (err) h += `<div class="ckerr">${esc(err)}</div>`;
  if (!rec) return h + `<div class="ckhint">${st || err ? "尚未体检。装配完成后点击「上传前体检」，可提前发现会导致上传失败或显示异常的问题，并查看模型的各项参数。" : "正在读取上次的体检结果…"}</div></div>`;
  return h + ckSummary(rec) + ckNote(rec) + ckBody(rec, "p", true, p.path) + `</div>`;
}
function ckDrawPanel() {
  const el = $("#ck_panel"); if (!el) return;
  const refocus = focusMark(el);
  if (put(el, ckPanelHTML())) refocus();
  const b = $('[data-ck="run"]');
  if (b && AI.proj) { const busy = !!CK.busy[AI.proj.path]; if (b.disabled !== busy) b.disabled = busy; }
}
// A run of the line that just ended well gets a check-up by itself; one the AI ran in it is shown as it is.
function ckAfterRun(path, fresh) {
  const did = t => fresh.some(s => s.kind === "tool" && s.tool === t && s.ok && !s.busy);
  const last = fresh[fresh.length - 1];
  if (!last || last.kind === "error" || !(did("dress") || did("build_menu") || did("place_avatar"))) return;
  if (did("checkup")) { delete CK.asked[path]; return ckFetch(path); }
  ckRun(path, true);
}

// ---------- the 工程 page: a badge on the card, and the project's details ----------
// (in the card's line of small print: the last check-up's rank and what it found to handle, and the way to the details)
function ckCardHTML(p) {
  const c = p.checkup, more = `<button class="cklink" data-ck="open" title="工程详情和模型参数">详情</button>`;
  if (!c) return more;
  const tip = `上次体检：${fmtTime(c.at)}${c.avatar ? "　" + c.avatar : ""}`;
  return `<button class="ckbadge" data-ck="open" title="${esc(tip)}">${ckRank(c.pc, "", false) || `<span class="ckrank">已体检</span>`}${c.fail ? `<span class="n fail"><b>${esc(c.fail)}</b><span>项需处理</span></span>` : ""}</button>${more}`;
}
function ckDrawerPath() { const dr = $("#drawer"); return dr.dataset.kind === "proj" && dr.classList.contains("on") ? dr.dataset.proj : ""; }
function ckOpenProj(path) {
  if (!(S.projs || []).some(x => x.path === path)) return;
  S.openKey = null; S.shopOpen = null;
  const dr = $("#drawer"); dr.dataset.kind = "proj"; dr.dataset.proj = path; dr._h = null;
  ckDrawDrawer(true); delete CK.asked[path]; ckFetch(path);
}
function ckDrawDrawer(fresh) {
  const dr = $("#drawer"), path = fresh ? dr.dataset.proj : ckDrawerPath(); if (!path) return;
  const p = (S.projs || []).find(x => x.path === path);
  if (!p) { closeDrawer(); return; }
  const st = CK.recs[path], rec = st && st.record, busy = !!CK.busy[path], err = CK.err[path], sb = STUDIO_BUSY[path];
  const cover = p.cover ? `<img src="/thumb?w=640&p=${encodeURIComponent(p.cover)}&t=${encodeURIComponent(p.coverAt)}" alt="">` : `<div class="pph">${ICON.cube}</div>`;
  const state = p.running ? `<span class="okline">已在 Unity 中打开</span>` : p.opened ? `<span>上次打开：${esc(relDay(p.opened))}</span>` : "";
  let sec = `<div class="sec ckd"><h4 class="withact"><span>模型参数</span>${rec ? ckMeta(rec) : ""}
    ${rec && rec.result && rec.result.avatar ? `<button class="h4act" data-ck="report" title="把评级和主要数值画成一张图片，可复制到群聊或保存">导出报告图片</button>` : ""}
    <button class="h4act${rec && rec.result && rec.result.avatar ? " next" : ""}" data-ck="rerun"${busy || !st || !st.can ? " disabled" : ""}${st && !st.can ? ` title="${esc(st.why)}"` : ""}>${busy ? "正在体检…" : rec ? "重新体检" : "开始体检"}</button></h4>`;
  if (err) sec += `<div class="ckerr">${esc(err)}</div>`;
  if (st && !st.can && !busy) sec += `<div class="ckwhy">${esc(st.why)}</div>`;
  if (rec) sec += ckSummary(rec) + ckNote(rec) + ckBody(rec, "d", false, path);
  else sec += `<div class="ckhint">${st ? "尚未体检。在 Unity 中打开该工程并连接插件后，可在此查看模型的各项参数，以及会导致上传失败或显示异常的问题。" : "正在读取…"}</div>`;
  sec += `</div>`;
  const html = `<div class="dhead">
      <div class="dcover pdcover">${cover}</div>
      <div style="min-width:0;flex:1">
        <h2 data-i18n="off">${esc(p.name)}</h2>
        <div class="sub">${esc(p.path)}</div>
        <div class="sub pmeta">${p.unity ? `<span class="uver${p.editor ? "" : " missing"}">Unity ${esc(p.unity)}</span>` : ""}${state}</div>
        <div class="rowbtn"><button class="btn primary" data-pa="unity"${p.running ? " disabled" : ""}>打开 Unity</button><button class="btn" data-pa="folder">文件夹</button><button class="btn" data-pa="ai">流水线</button><button class="btn" data-pa="studio"${sb ? " disabled" : ""}>${sb === "on" ? "正在准备…" : "摄影棚"}</button>${p.studio ? `<button class="btn ghost" data-pa="studio-off"${sb ? " disabled" : ""}>${sb === "off" ? "正在移除…" : "移除摄影棚"}</button>` : ""}</div>
      </div></div>
    <div class="dbody">${sec}
      <div class="sec"><h4>素材</h4>${p.assets ? `<button class="puse" data-pa="assets">使用了素材库中的 ${esc(p.assets)} 个素材</button>` : `<div class="muted small">未使用素材库中的素材</div>`}</div>
    </div>
    <div class="dfoot"><button class="btn ghost" data-ck="close">关闭</button></div>`;
  const top = fresh ? 0 : ($(".dbody", dr) || {}).scrollTop || 0, refocus = fresh ? () => {} : focusMark(dr);
  if (put(dr, html)) { $(".dbody", dr).scrollTop = top; refocus(); }
  dr.classList.add("on"); dr.setAttribute("aria-hidden", "false"); $("#scrim").classList.add("on");
}
function ckDraw() { ckDrawPanel(); ckDrawDrawer(); }

// ---------- covers from prefabs ----------
// an asset that shows no picture, one made here before, or only the preview out of its unitypackage (a render
// from Unity takes its place)
function covWants(a) {
  if (!a || !isLocal(a) || a.psd) return false;
  if (!a.cover) return true;
  let p = ""; try { p = decodeURIComponent(a.cover); } catch (e) {}
  return /covers[\\/](generated|unitypackage)[\\/]/.test(p);
}
function covFromPkg(a) { let p = ""; try { p = decodeURIComponent(a.cover || ""); } catch (e) {} return /covers[\\/]unitypackage[\\/]/.test(p); }
// the line in the asset's details panel (drawn by renderDrawer)
function covFieldHTML(a) {
  if (!covWants(a)) return "";
  const s = CK.cov[a.key], busy = !!CK.covBusy[a.key];
  if (s && s.real) return "";
  const gen = !!(s && s.generated);
  const why = !s ? "正在检查…" : s.can ? "在已打开的 Unity 工程中渲染该素材的 prefab，作为封面" : s.why;
  return `<div class="field"><label>封面</label><div class="covgen">
    <button class="btn small" data-cov="gen"${busy || !s || !s.can ? " disabled" : ""} title="${esc(why)}">${busy ? "正在生成…" : gen ? "重新生成封面" : "从 Unity 生成封面"}</button>
    ${gen && !busy ? `<button class="btn small ghost" data-cov="remove">移除</button>` : ""}
    <span class="hint">${gen ? "当前封面由 Unity 中的 prefab 生成。关联 Booth 商品或选择图片后，将改用其封面。" : s && !s.can ? esc(s.why) : covFromPkg(a) ? "生成后将替换 unitypackage 中的预览图。" : "该素材没有封面图片。"}</span></div></div>`;
}
// The line is swapped in place when only it changed (its status arrived, a run began), and the panel is told what
// it reads now: the panel is not drawn again, so what is being typed in it is not touched.
function covRedraw(key, change) {
  const a = S.openKey === key ? findAsset(key) : null, was = a ? covFieldHTML(a) : "";
  change();
  if (!a) return;
  const dr = $("#drawer"), el = $(".covgen", dr), now = covFieldHTML(a);
  if (now === was) return;
  if (!el || !was || !now || !dr._h || !dr._h.includes(was)) return renderDrawer(key, true);
  const had = el.contains(document.activeElement), box = document.createElement("div");
  box.innerHTML = now; dr._h = dr._h.replace(was, () => now);
  el.closest(".field").replaceWith(box.firstElementChild);
  if (had) { const b = $('[data-cov="gen"]', dr); if (b && !b.disabled) b.focus({ preventScroll: true }); }
}
async function covStatus(key) {
  let r; try { r = await api("/api/cover/status", { key }); } catch (e) { return; }
  if (!r.ok) return;
  covRedraw(key, () => { CK.cov[key] = Object.assign({}, r.status, { at: Date.now() }); });
}
async function covGenerate(key, remove) {
  if (CK.covBusy[key]) return;
  covRedraw(key, () => { CK.covBusy[key] = true; });
  let r; try { r = await api(remove ? "/api/cover/remove" : "/api/cover/generate", { key }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  delete CK.covBusy[key];
  if (r.status) CK.cov[key] = Object.assign({}, r.status, { at: Date.now() });
  toast(r.ok ? (remove ? "已移除生成的封面" : "封面已生成") : r.err || "生成失败", r.ok ? 2200 : 6000);
  await load();
  if (S.openKey === key) renderDrawer(key, true);
}
// the pipeline page's 素材 step: every asset of the project that has no cover
function covProgHTML() {
  const b = CK.batch, p = AI.proj;
  if (!b || !p || b.project !== p.path) return "";
  if (b.running) {
    const pct = b.total ? Math.round(b.done / b.total * 100) : 0;
    return `<div class="covprog"><span>正在生成封面</span><span class="bar wide"><i style="width:${pct}%"></i></span><span class="cnt">${esc(b.done)} / ${esc(b.total)}</span><span class="nm" title="${esc(b.now || "")}">${esc(b.now || "")}</span><button class="btn small ghost" data-cov="cancel">取消</button></div>`;
  }
  if (!b.ended || Date.now() / 1000 - b.ended > 600) return "";
  return `<div class="covprog done"><span class="${b.made ? "okline" : "muted"}"><span>已生成封面：</span><b>${esc(b.made)}</b></span>${b.failed ? `<span class="warnline"><span>未能生成：</span><b>${esc(b.failed)}</b></span>` : ""}${
    b.err ? `<span class="err">${esc(b.err)}</span>` : ""}${b.last ? `<span class="muted small last" title="${esc(b.last)}">${esc(b.last)}</span>` : ""}</div>`;
}
function covDrawProg() {
  const el = $("#cov_prog"); if (el) put(el, covProgHTML());
  const btn = $('[data-cov="batch"]'); if (btn) { const run = !!(CK.batch && CK.batch.running); if (btn.disabled !== run) btn.disabled = run; }
}
async function covBatchPoll() {
  let r; try { r = await api("/api/cover/batch/status", {}); } catch (e) { return; }
  if (!r.ok) return;
  const was = CK.batch || {};
  CK.batch = r.batch;
  covDrawProg();
  if ((r.batch.made || 0) !== (was.made || 0) || (was.running && !r.batch.running)) { CK.cov = {}; load(); } // the cards show the new covers
}
async function covBatchStart() {
  const p = AI.proj; if (!p) return;
  const r = await api("/api/cover/batch", { project: p.path });
  if (!r.ok) { toast(r.err || "无法开始", 5000); return; }
  CK.batch = r.batch; covDrawProg();
}

// ---------- events ----------
document.addEventListener("click", async e => {
  const t = e.target;
  const cb = t.closest("[data-cov]");
  if (cb) {
    const what = cb.dataset.cov;
    if (what === "batch") return covBatchStart();
    if (what === "cancel") { await api("/api/cover/batch/cancel", {}); return covBatchPoll(); }
    if (S.openKey && (what === "gen" || what === "remove")) return covGenerate(S.openKey, what === "remove");
    return;
  }
  const where = () => t.closest("#drawer") ? ckDrawerPath() : AI.proj ? AI.proj.path : ""; // which project the button is about
  const fb = t.closest("[data-ckfix]");
  if (fb) return ckFix(where(), fb.dataset.ckfix);
  if (t.closest("[data-ckrevert]")) return ckRevert(where());
  const rb = t.closest("[data-ckr]");
  if (rb) return rb.dataset.ckr === "copy" ? ckReportCopy() : ckReportSave();
  const go = t.closest("#modal [data-gopen]");
  if (go && $("#modal #ck_card")) return openPath(go.dataset.gopen); // the report dialog's own button
  const b = t.closest("[data-ck]");
  if (b) {
    const what = b.dataset.ck;
    if (what === "run") return AI.proj ? ckRun(AI.proj.path) : undefined;
    if (what === "rerun") return ckRun(ckDrawerPath());
    if (what === "report") return ckReportOpen(where());
    if (what === "close") return closeDrawer();
    if (what === "open") { const c = b.closest("[data-proj]"); if (c) ckOpenProj(c.dataset.proj); }
    return;
  }
  if (t.closest("#vPipe, #vProj, [data-pipeproj], [data-pa]")) setTimeout(ckTick, 80); // the page changed: not a second later
  // the card's picture and name lead to its details, like its 「详情」 link
  const card = t.closest(".pcard");
  if (card && !t.closest("button, a") && t.closest(".pcover, .pbody > .title")) ckOpenProj(card.dataset.proj);
});
document.addEventListener("toggle", e => {
  const d = e.target;
  if (!(d instanceof HTMLDetailsElement) || !d.dataset.ckd) return;
  if (d.open) CK.open.add(d.dataset.ckd); else CK.open.delete(d.dataset.ckd);
  // what the panel reads now is what is on the page: the next refresh leaves it alone
  const host = d.closest("#ck_panel"); if (host) host._h = ckPanelHTML();
}, true);

// Once a second: what the pages show of the check-up and of the covers is brought up to date. Nothing is asked
// of the server unless one of them is on the screen.
function ckTick() {
  if (!S.data || document.hidden) return;
  if (pipeActive()) {
    const path = AI.proj.path, card = pipeProject(path) || {}, st = CK.recs[path];
    const at = card.checkup ? card.checkup.at : 0;
    if (!st || ((st.record ? st.record.at : 0) !== at && Date.now() - st.at > 1500) || Date.now() - st.at > 15000) ckFetch(path);
    // a run of the line that has just ended
    const s = AI.sess || {}, steps = s.steps, seen = CK.seen[path];
    if (Array.isArray(steps)) { // (the session as the server gave it, not the stub the page starts with)
      const at = steps.length ? steps[steps.length - 1].at : 0;
      if (!seen) CK.seen[path] = { n: steps.length, at };
      else if (!s.busy) {
        // new steps since it was last idle; the record is capped, so a full one is told by its last step's time
        const fresh = steps.length > seen.n ? steps.slice(seen.n) : steps.length === seen.n && at > seen.at ? steps.filter(x => x.at > seen.at) : [];
        CK.seen[path] = { n: steps.length, at };
        if (fresh.length) ckAfterRun(path, fresh);
      }
    }
    if (!CK.batch || CK.batch.running || Date.now() - CK.batchSeen > 10000) { CK.batchSeen = Date.now(); covBatchPoll(); }
    ckDrawPanel(); covDrawProg();
  }
  const dp = ckDrawerPath();
  if (dp) {
    const st = CK.recs[dp], card = (S.projs || []).find(x => x.path === dp) || {};
    if (!st || Date.now() - st.at > 4000 || (card.checkup && st.record && card.checkup.at !== st.record.at)) ckFetch(dp);
    ckDrawDrawer();
  }
  const a = S.openKey && $("#drawer").classList.contains("on") ? findAsset(S.openKey) : null;
  if (a && covWants(a) && !CK.covBusy[a.key] && (!CK.cov[a.key] || Date.now() - CK.cov[a.key].at > 6000)) covStatus(a.key);
}
setInterval(ckTick, 1000);
