// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// the Quest (Android) version (1.8.0): VRCQuestTools put into the project, the avatar set up for it, the PhysBones,
// colliders and contacts the Android version keeps, and the ranks it would get — the PC version is not changed
"use strict";

const QV = { project: "", st: null, busy: "", err: "", keep: null, drop: null, timer: 0, open: false };
const QV_RANK = { Excellent: "Excellent", Good: "Good", Medium: "Medium", Poor: "Poor", VeryPoor: "Very Poor" };

function qvOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "quest"; }
async function qvOpen(project) {
  Object.assign(QV, { project, st: null, busy: "", err: "", keep: null, drop: null });
  $("#modal").dataset.kind = "quest";
  qvDraw(true); showModal();
  await qvLoad();
}
async function qvLoad() {
  clearTimeout(QV.timer);
  let r;
  try { r = await api("/api/quest/status", { project: QV.project }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!qvOpenNow()) return;
  if (!r.ok) { QV.err = r.err || "无法读取"; return qvDraw(); }
  QV.st = r; QV.err = "";
  const u = r.unity;
  if (u && u.dynamics && QV.keep === null) QV.keep = new Set(u.dynamics.filter(d => d.keep).map(d => d.id));
  if (u && u.hidden && QV.drop === null) QV.drop = u.hidden.some(h => h.dropped);
  qvDraw();
  const j = r.job;
  if (j && (j.stage === "download" || j.stage === "import")) QV.timer = setTimeout(qvLoad, 1500);
}
function qvRank(r) { return r ? `<span class="qvrank r-${esc(r)}">${esc(QV_RANK[r] || r)}</span>` : ""; }
// what the choice adds up to, against what Android allows
function qvTotals(u) {
  const d = u.dynamics || [], k = QV.keep || new Set(), lim = u.limits || {};
  const bones = d.filter(x => x.kind === "physbone" && k.has(x.id) && !x.gone), cols = new Set(d.filter(x => x.kind === "collider" && k.has(x.id) && !x.gone).map(x => x.id));
  const t = {
    physBones: bones.length,
    transforms: bones.reduce((a, b) => a + (b.transforms || 0), 0),
    colliders: cols.size,
    checks: bones.reduce((a, b) => a + (b.perCollider || 0) * (b.colliders || []).filter(c => cols.has(c)).length, 0),
    contacts: d.filter(x => x.kind === "contact" && k.has(x.id) && !x.gone).length,
  };
  const over = Object.keys(t).filter(n => lim[n] !== undefined && t[n] > lim[n]);
  return { t, lim, over };
}
const QV_TOT = [["physBones", "PhysBone"], ["transforms", "受影响的骨骼"], ["colliders", "碰撞体"], ["checks", "碰撞检测"], ["contacts", "Contact"]];
function qvRow(ok, label, state, act) {
  return `<div class="qvrow"><span class="qvdot ${ok === true ? "ok" : ok === false ? "no" : "wait"}"></span><span class="qvlabel">${label}</span><span class="qvstate">${state}</span>${act ? `<span class="qvact">${act}</span>` : ""}</div>`;
}
function qvReadyHTML(r) {
  const u = r.unity, j = r.job, rows = [];
  // VRCQuestTools
  let vqt;
  if (j && (j.stage === "download" || j.stage === "import")) vqt = qvRow(null, "VRCQuestTools", `<i class="spin"></i>${esc(j.msg || "正在安装…")}`);
  else if (r.installed) {
    const loaded = !u || u.vqt;
    vqt = qvRow(loaded ? true : null, "VRCQuestTools", `${esc(r.installed)}${loaded ? "" : `<span class="muted small">（Unity 还没有加载：请切换到 Unity 窗口等待编译完成）</span>`}`);
  } else vqt = qvRow(false, "VRCQuestTools", `未安装`, `<button class="btn small primary" data-qv="install"${QV.busy ? " disabled" : ""}>安装</button>`);
  rows.push(vqt);
  if (j && j.stage === "failed") rows.push(`<div class="small err qvsub">${esc(j.err || "安装失败")}</div>`);
  else if (j && j.stage === "done" && j.note) rows.push(`<div class="small warn qvsub">${esc(j.note)}</div>`);
  // the Android module of the project's Unity
  const am = u ? (u.android ? "yes" : "no") : r.androidModule;
  if (am === "yes") rows.push(qvRow(true, "Android 构建模块", "已安装"));
  else if (am === "no") rows.push(qvRow(false, "Android 构建模块", `未安装`, r.hub ? `<button class="btn small" data-qv="hub">打开 Unity Hub</button>` : "")
    + `<div class="small muted qvsub">${esc(`在 Unity Hub 的「安装」页中，为 Unity ${r.unityVersion || ""} 点「添加模块」，勾选 Android Build Support（连同其中的 OpenJDK 和 Android SDK & NDK Tools）。安装完需要重启 Unity。`)}</div>`);
  else rows.push(qvRow(null, "Android 构建模块", `<span class="muted">未找到该工程使用的 Unity ${esc(r.unityVersion || "")}，无法确认</span>`));
  // the avatar
  if (!u) {
    const why = r.oldPlugin ? "该工程的 Unity 插件为旧版，不支持Quest 版设置：请在流水线页点击「更新」，待 Unity 编译完成后重试" : r.unityErr || "在 Unity 中打开工程后，可以设置模型并查看安卓版的评级预估。";
    rows.push(qvRow(null, "模型设置", `<span class="muted">${esc(why)}</span>`));
  } else if (!u.avatar) rows.push(qvRow(false, "模型设置", "场景中没有模型"));
  else if (u.setUp) rows.push(qvRow(true, "模型设置", esc(`已设置：${u.avatar}`)));
  else rows.push(qvRow(false, "模型设置", esc(`未设置：${u.avatar}`), `<button class="btn small primary" data-qv="setup"${!u.vqt || QV.busy ? " disabled" : ""} title="${u.vqt ? "在模型上加 VRCQuestTools 的设置组件，并按下面的选择标记安卓版去掉的物理" : "请先安装 VRCQuestTools"}">${QV.busy === "setup" ? `<i class="spin"></i>` : ""}一键设置</button>`));
  return `<div class="qvready">${rows.join("")}</div>`;
}
function qvFiguresHTML(u) {
  const f = u.figures;
  if (!f) return `<div class="muted small">该 SDK 没有性能计算器，无法估算。</div>`;
  const rows = (f.items || []).map(it => {
    const tiers = it.tiers ? `<span class="muted small">Medium ≤ ${it.tiers[2]} · Poor ≤ ${it.tiers[3]}</span>` : "";
    return `<tr><td>${esc(it.label)}</td><td class="num">${esc(it.text || String(it.value))}${it.unit ? " " + esc(it.unit) : ""}</td><td>${qvRank(it.rating)}</td><td>${tiers}</td></tr>`;
  }).join("");
  const vp = f.overall === "VeryPoor" || f.overall === "Poor";
  return `<div class="qvover"><span>安卓版预估</span>${qvRank(f.overall)}</div>
    ${vp ? `<div class="small warn qvnote">安卓默认只显示 Medium 及以上的模型：Quest 玩家要在你的名片上点「显示模型」才看得到（其余时候显示为备用模型）。大多数日系模型的安卓版都是这样。</div>` : ""}
    <table class="qvtab"><tbody>${rows}</tbody></table>
    <div class="small muted qvnote">性能为估算值，请以上传时的 SDK 结果为准。</div>`;
}
function qvDynHTML(u) {
  const d = (u.dynamics || []).filter(x => !x.gone), k = QV.keep || new Set();
  const { t, lim, over } = qvTotals(u);
  const tot = QV_TOT.map(([n, label]) => `<span class="qvtot${over.includes(n) ? " over" : ""}">${esc(label)} <b>${t[n]}</b> / ${lim[n]}</span>`).join("");
  const item = x => {
    const extra = x.kind === "physbone" ? `<span class="frtag">${x.transforms} 根骨骼${x.colliders && x.colliders.length ? `，${x.colliders.length} 个碰撞体` : ""}</span>` : "";
    return `<label class="frrow${x.active ? "" : " qvoff"}"><input type="checkbox" data-qvk="${esc(x.id)}"${k.has(x.id) ? " checked" : ""}><span data-i18n="off">${esc(x.path || "（模型根物体）")}</span>${x.active ? "" : `<span class="frtag">默认隐藏</span>`}${extra}</label>`;
  };
  const group = (kind, title) => {
    const l = d.filter(x => x.kind === kind);
    if (!l.length) return "";
    const n = l.filter(x => k.has(x.id)).length;
    return `<details class="qvgrp"${kind === "physbone" ? " open" : ""}><summary>${esc(title)}：保留 ${n} / ${l.length}</summary>${l.map(item).join("")}</details>`;
  };
  const hidden = u.hidden || [];
  const dropRow = hidden.length ? `<label class="check qvdrop"><input type="checkbox" data-qv="drop"${QV.drop ? " checked" : ""}>${esc(`安卓版不带默认隐藏的衣服（${hidden.map(h => h.name).join("、")}）`)}</label>
    <div class="small muted qvsub">能大幅减少面数和材质；这些衣服的菜单开关在安卓版里不起作用。</div>` : "";
  return `<div class="qvtots">${tot}</div>
    ${over.length ? `<div class="small err qvnote">超过安卓的上限：VRChat 会去掉这一类的全部组件。请再少留一些。</div>` : ""}
    ${group("physbone", "PhysBone")}${group("collider", "PhysBone 碰撞体")}${group("contact", "Contact")}
    ${dropRow}
    <div class="btnrow"><button class="btn small" data-qv="auto">按程序建议</button><button class="btn small primary" data-qv="apply"${!u.vqt || QV.busy || over.length ? " disabled" : ""}>${QV.busy === "apply" ? `<i class="spin"></i>` : ""}应用到模型</button>
      <span class="small muted">${u.chosen === "marks" ? "显示的是模型上已有的标记" : "显示的是程序的建议（头发、裙子、尾巴、耳朵优先）"}</span></div>`;
}
function qvDraw(fresh) {
  const m = $("#modal"); if (!fresh && !qvOpenNow()) return;
  const r = QV.st;
  let body;
  if (!r && !QV.err) body = `<div class="rmbusy"><i class="spin"></i>正在读取…</div>`;
  else if (!r) body = `<div class="small err">${esc(QV.err)}</div>`;
  else {
    const u = r.unity;
    body = `
      <h5 class="lmh">准备</h5>${qvReadyHTML(r)}
      ${u && u.avatar ? `<h5 class="lmh">安卓版保留的物理</h5>${qvDynHTML(u)}<h5 class="lmh">评级预估</h5>${qvFiguresHTML(u)}` : ""}
      <h5 class="lmh">上传</h5><ol class="qvsteps"><li>先在 VRChat SDK 面板重新上传一次 PC 版：让 PC 和安卓版的 PhysBone 同步。</li><li>在 SDK 面板的 Selected Platform 中选 Android。第一次切换要重新导入全部素材，大工程可能需要十几分钟。</li><li>点 Build and Publish：VRCQuestTools 在构建时自动转换。</li><li>上传完把平台切回 Windows，再继续改模。</li></ol>
      ${QV.err ? `<div class="small err">${esc(QV.err)}</div>` : ""}`;
  }
  const html = `<div class="mhead">Quest（安卓）版</div><div class="mbody qvbody">${body}</div>
    <div class="mfoot"><span class="muted small">设置可在 Unity 中按 Ctrl+Z 撤销，也可在「时光机」中回到设置前</span><span class="spacer"></span><button class="btn" data-qv="reload">刷新</button><button class="btn" data-qv="close">关闭</button></div>`;
  if (m._qv === html && !fresh) return;
  const refocus = fresh ? () => {} : focusMark(m);
  const b = $(".qvbody", m), top = b ? b.scrollTop : 0;
  const opened = [...m.querySelectorAll("details.qvgrp")].map(x => x.open);
  m._qv = html; m.innerHTML = html; refocus();
  [...m.querySelectorAll("details.qvgrp")].forEach((x, i) => { if (opened[i] !== undefined) x.open = opened[i]; });
  if ($(".qvbody", m)) $(".qvbody", m).scrollTop = top;
}
async function qvDo(what) {
  if (what === "close") return closeModal();
  if (what === "reload") { QV.keep = null; QV.drop = null; return qvLoad(); }
  if (QV.busy) return;
  if (what === "hub") { const r = await api("/api/quest/hub", {}); if (!r.ok) toast(r.err || "无法打开 Unity Hub", 3000); return; }
  if (what === "install") {
    if (!confirm("将 VRCQuestTools（kurotu 制作，MIT 许可）安装到该工程？\n\n会从它的 VPM 仓库下载，放进工程的 Packages 文件夹并写入 vpm-manifest.json（和在 ALCOM 中添加插件一样）。Unity 打开着的话会接着导入并编译一两分钟。")) return;
    QV.busy = "install"; QV.err = ""; qvDraw();
    let r;
    try { r = await api("/api/quest/install", { project: QV.project }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    QV.busy = "";
    if (!r.ok) { QV.err = r.err || "未能安装"; return qvDraw(); }
    return qvLoad();
  }
  if (what === "auto") {
    const u = QV.st && QV.st.unity; if (!u) return;
    QV.keep = null; QV.drop = null;
    // the program's choice again: asked of the plugin as it would choose without marks
    QV.busy = "auto"; qvDraw();
    let r;
    try { r = await api("/api/quest/status", { project: QV.project, auto: true }); } catch (e) { r = { ok: false }; }
    QV.busy = "";
    if (r.ok && r.unity) { QV.st = r; QV.keep = new Set(r.unity.dynamics.filter(d => d.keep).map(d => d.id)); QV.drop = (r.unity.hidden || []).some(h => h.dropped); }
    return qvDraw();
  }
  if (what === "setup" || what === "apply") {
    const body = { project: QV.project, dropHidden: !!QV.drop };
    if (what === "apply" && QV.keep) body.keep = [...QV.keep];
    QV.busy = what; QV.err = ""; qvDraw();
    let r;
    try { r = await api("/api/quest/setup", body); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    QV.busy = "";
    if (!r.ok) { QV.err = r.err || "未能设置"; return qvDraw(); }
    toast(r.added && r.added.length ? `已设置，添加了 ${r.added.length} 个组件` : "已应用到模型", 2500);
    QV.st = Object.assign({}, QV.st, { unity: r });
    QV.keep = new Set((r.dynamics || []).filter(d => d.keep).map(d => d.id)); QV.drop = (r.hidden || []).some(h => h.dropped);
    if (window.aiPoll && pipeActive()) aiPoll();
    return qvDraw();
  }
}
document.addEventListener("click", e => {
  if (e.target.closest("[data-quest]")) { if (AI.proj) qvOpen(AI.proj.path); return; }
  if (!qvOpenNow()) return;
  const b = e.target.closest("button[data-qv]");
  if (b) qvDo(b.dataset.qv);
});
document.addEventListener("change", e => {
  if (!qvOpenNow()) return;
  const t = e.target;
  if (t.dataset.qvk) { if (!QV.keep) QV.keep = new Set(); if (t.checked) QV.keep.add(t.dataset.qvk); else QV.keep.delete(t.dataset.qvk); qvDraw(); }
  if (t.dataset.qv === "drop") { QV.drop = t.checked; qvDraw(); }
});
