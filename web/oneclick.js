// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)

// The modding basket: in the library the player picks one base and the assets to put on it, sees what each item
// costs in synced parameters and whether it fits, then one click makes a new project with everything dressed and a
// menu built (/api/oneclick/*, internal/ai/oneclick_*.go). The basket stays on this computer between sessions.

// key, title, how many, the library categories that land in it
const OC_GROUPS = [
  ["base", "素体", 1, ["素体"]],
  ["clothes", "衣服", 8, ["衣服"]],
  ["hair", "头发", 3, ["头发"]],
  ["skins", "皮肤 / 材质", 4, ["材质"]],
  ["accessories", "配饰", 12, ["配饰"]],
  ["props", "道具", 8, ["道具"]],
  ["expressions", "表情", 1, []],
  ["plugins", "功能插件", 4, ["插件", "面捕", "动作"]],
];
const OC_KEEP = "mio.oneclick.basket", OC_DIR = "mio.oneclick.dir", OC_TARGET = "mio.oneclick.target";
// dir: where projects go, one folder for every creation (ALCOM's default until the player picks another); target:
// a project of the player's own to put the avatar into instead of a new one ("" = a new project);
// tool: what the computer has (/api/pipe/toolchain); toolJob: a download and installation under way
const OC = { items: [], est: null, estErr: "", seq: 0, timer: 0, open: false, job: null, poll: 0, sending: false, dir: "", tool: null, toolJob: null, toolPoll: 0,
  up: null }; // 上传到 VRChat from the result: {name, release, state, err, poll}

function ocGroup(key) { return OC_GROUPS.find(g => g[0] === key); }
function ocGroupOf(cat) { const g = OC_GROUPS.find(g => g[3].includes(cat)); return g ? g[0] : ""; }
function ocLoad() {
  try { const v = JSON.parse(localStorage.getItem(OC_KEEP) || "[]"); if (Array.isArray(v)) OC.items = v.filter(i => i && i.key && ocGroup(i.group)); } catch (_) { OC.items = []; }
  try { OC.dir = localStorage.getItem(OC_DIR) || ""; OC.target = localStorage.getItem(OC_TARGET) || ""; } catch (_) {}
}
function ocSave() { try { localStorage.setItem(OC_KEEP, JSON.stringify(OC.items)); } catch (_) {} }
function ocHas(key) { return OC.items.some(i => i.key === key); }

// the round button on a library card (app.js cardHTML)
function oneclickPickHTML(a) {
  if (!a || !ocGroupOf(a.category)) return "";
  const on = ocHas(a.key);
  return `<button class="ocpick${on ? " on" : ""}" data-act="ocpick" aria-pressed="${on}" title="${on ? "移出改模篮" : "加入改模篮"}">${on ? "✓" : "+"}</button>`;
}

// library cards keep their html until the asset changes (app.js unitHTML); the basket's mark is not part of the
// asset, so the cards whose mark changes are drawn again
function ocRedraw(keys) {
  for (const k of keys) { const a = typeof findAsset === "function" ? findAsset(k) : null; if (a) a._card = null; }
}
function oneclickToggle(a) {
  ocRedraw(OC.items.map(i => i.key).concat(a.key));
  if (ocHas(a.key)) { OC.items = OC.items.filter(i => i.key !== a.key); return ocChanged(); }
  const group = ocGroupOf(a.category); if (!group) return;
  const g = ocGroup(group), face = a.category === "面捕";
  if (group === "base") OC.items = OC.items.filter(i => i.group !== "base");
  else if (OC.items.filter(i => i.group === group).length >= g[2]) { toast(`「${g[1]}」最多 ${g[2]} 个`, 3000); return; }
  if (face && OC.items.some(i => i.face)) { toast("面捕最多 1 个", 3000); return; }
  OC.items.push({ key: a.key, name: a.name, group, face });
  ocChanged();
}
function ocMove(key, group) {
  const it = OC.items.find(i => i.key === key), g = ocGroup(group); if (!it || !g || it.group === group) return;
  if (group === "base") OC.items = OC.items.filter(i => i.group !== "base");
  else if (OC.items.filter(i => i.group === group).length >= g[2]) { toast(`「${g[1]}」最多 ${g[2]} 个`, 3000); ocDraw(); return; }
  it.group = group; ocChanged();
}
function ocChanged() {
  ocSave(); ocDraw();
  if (typeof renderGrid === "function") renderGrid();
  clearTimeout(OC.timer); OC.timer = setTimeout(ocEstimate, 250);
}

function ocBasket() {
  const b = { base: { id: "" } };
  for (const [key] of OC_GROUPS) if (key !== "base") b[key] = [];
  for (const i of OC.items) {
    const item = i.face ? { id: i.key, faceTracking: true } : { id: i.key };
    if (i.group === "base") b.base = item; else b[i.group].push(item);
  }
  return b;
}
async function ocEstimate() {
  if (typeof ocbSwap === "function") ocbSwap(); // items by their Booth number, in the library by now (ocbooth.js)
  const seq = ++OC.seq;
  if (!OC.items.some(i => i.group === "base")) { OC.est = null; OC.estErr = ""; return ocDraw(); }
  let r; // (where the job would go, for the room on that drive: internal/ai/oneclick_space.go)
  try { r = await api("/api/oneclick/estimate", Object.assign(ocBasket(), OC.target ? { target: OC.target } : { parent: ocDir() })); } catch (e) { r = { ok: false, err: e.message }; }
  if (seq !== OC.seq) return; // a newer basket is on its way
  OC.est = r.ok ? r.estimate : null; OC.estErr = r.ok ? "" : (r.err || "无法计算");
  ocDraw();
}

function ocEstItem(key) { return OC.est ? (OC.est.items || []).find(x => x.id === key) : null; }
function ocCompat(x) {
  if (!x) return "";
  const cls = x.compatibility === "incompatible" ? "bad" : x.compatibility === "unknown" ? "" : "ok";
  return `<span class="tag ${cls}"${x.reason ? ` title="${esc(x.reason)}"` : ""}>${esc(x.compatibilityLabel || "")}</span>`;
}
// job states (oneclick_job.go): running, need_player, stopping; paused (the program restarted mid-run), failed,
// stopped, done
function ocActive(j) { return !!j && (["running", "need_player", "stopping", "paused"].includes(j.state) || ocQuestBusy(j)); }
function ocCanStart() { return !!(OC.est && OC.est.canStart && !ocActive(OC.job)); }

// a term with its sentence on hover and focus (newbie.js), the label alone without it
function ocTerm(key, html) { return typeof nbTerm === "function" ? nbTerm(key, html) : html; }
// no base picked while the library has none: the bases to start with (newbie.js)
function ocNoBase() { return !OC.items.some(i => i.group === "base") && typeof nbHasBase === "function" && !nbHasBase(); }
function ocBarHTML() {
  const counts = OC_GROUPS.map(g => [g[1], OC.items.filter(i => i.group === g[0]).length]).filter(c => c[1]);
  const total = OC.est ? OC.est.total : null, limit = OC.est ? OC.est.limit || 256 : 256, over = total !== null && total > limit && !OC.est.compress;
  // the picked assets as small covers (the counts by category in the tooltip)
  const thumbs = OC.items.slice(0, 7).map(i => {
    const a = typeof findAsset === "function" ? findAsset(i.key) : null;
    return a && a.cover ? `<img class="octh" src="${esc(a.cover)}" alt="" title="${esc(i.name)}" loading="lazy">` : `<span class="octh" title="${esc(i.name)}" data-i18n="off">${esc((i.name || "?").slice(0, 1))}</span>`;
  }).join("") + (OC.items.length > 7 ? `<span class="octh more">+${OC.items.length - 7}</span>` : "");
  return `<div class="ocbar"><b>改模篮</b><span class="octhumbs" title="${esc(counts.map(([t, n]) => `${t} ${n}`).join(" · "))}">${thumbs}</span>
    <span class="ocbits${over ? " over" : ""}">${ocTerm("params", total === null ? "参数 —" : `参数 ${total} / ${limit}`)}</span>
    <span class="grow"></span>${ocNoBase() ? `<button class="btn small ghost" data-nb="bases">还没有素体</button>` : ""}${OC.job ? `<button class="btn small" data-oc="progress">${ocActive(OC.job) ? "查看进度" : "上次创建"}</button>` : ""}
    <button class="btn small ghost" data-oc="toggle" aria-expanded="${OC.open}">${OC.open ? "收起" : "展开"}</button>
    <button class="btn small primary" data-oc="start"${ocCanStart() ? "" : " disabled"}>一键创建</button></div>`;
}
function ocPanelHTML() {
  const e = OC.est, lines = [];
  if (!OC.items.some(i => i.group === "base")) lines.push(ocNoBase() ? nbNoBaseHTML() : `<p class="ochint">先选一个素体</p>`);
  if (OC.estErr) lines.push(`<p class="warnc">${esc(OC.estErr)}</p>`);
  if (e && (e.items || []).some(x => x.source === "netdisk")) lines.push(`<p class="ochint">包含网盘文件，选择后创建会自动下载并解压导入</p>`);
  if (e && e.total > (e.limit || 256) && !e.compress) lines.push(`<p class="warnc">参数超过 ${e.limit || 256} 位，请移除几项</p>`);
  for (const c of (e && e.conflicts) || []) lines.push(`<p class="warnc">${esc(c)}</p>`);
  for (const w of (e && e.warnings) || []) lines.push(`<p class="ochint">${esc(w)}</p>`);
  const groups = OC_GROUPS.map(([key, title]) => {
    const items = OC.items.filter(i => i.group === key);
    if (!items.length) return "";
    const bits = e && e.categoryBits ? e.categoryBits[key] : undefined;
    return `<section class="ocgroup"><h4>${key === "base" ? ocTerm("base", esc(title)) : esc(title)}${bits !== undefined && key !== "base" ? `<span class="muted small">${bits} 位</span>` : ""}${key === "base" && e ? `<span class="muted small">自带 ${e.baseBits} 位</span>` : ""}</h4>
      ${items.map(i => {
        const x = ocEstItem(i.key);
        return `<div class="ocitem" data-key="${esc(i.key)}"><span class="nm" data-i18n="off" title="${esc(i.name)}">${esc(i.name)}</span>${ocCompat(x)}
          ${x && key !== "base" ? `<span class="bits">${x.bits} 位${x.guessed ? "（估算）" : ""}</span>` : ""}
          ${x && x.note ? `<span class="ochint small">${esc(x.note)}</span>` : ""}
          <select data-oc="move" aria-label="类别">${OC_GROUPS.map(([k, t]) => `<option value="${k}"${k === key ? " selected" : ""}>${esc(t)}</option>`).join("")}</select>
          <button class="btn icon ghost" data-oc="remove" aria-label="移除" title="移除">×</button>${typeof ocbItemHTML === "function" ? ocbItemHTML(i, x) : ""}</div>`;
      }).join("")}</section>`;
  }).join("");
  const where = OC.target ? OC.target : ocDir();
  return `<div class="ocpanel">${typeof ocbPasteHTML === "function" ? ocbPasteHTML() : ""}${lines.join("")}${groups}<div class="btnrow"><select data-oc="where" aria-label="创建到"><option value="new"${OC.target ? "" : " selected"}>新建工程</option><option value="existing"${OC.target ? " selected" : ""}>放进已有工程</option></select>
    <span class="ocloc" data-i18n="off" title="${esc(where)}">${esc(where || "—")}</span><button class="btn small ghost" data-oc="dir">更改</button><span class="grow"></span><button class="btn small ghost" data-oc="clear">清空</button></div></div>`;
}
function ocDraw() {
  const grid = $("#grid"); if (!grid) return;
  if (OC.items.length && !OC.tool && !OC.toolAsked) { OC.toolAsked = true; ocTools().then(() => ocDraw()); }
  let el = $("#oc_dock");
  if (!el) { el = document.createElement("div"); el.id = "oc_dock"; el.className = "ocdock"; grid.after(el); }
  if (!OC.items.length && !OC.job) { el.hidden = true; el.innerHTML = ""; el._h = ""; return; }
  if (S.view !== "lib") { el.hidden = true; return; } // (the library's: other pages would have it over their own content)
  el.hidden = false;
  const keep = typeof ocbKeep === "function" ? ocbKeep(el) : null; // (the Booth address being typed in, ocbooth.js)
  put(el, ocBarHTML() + (OC.open ? ocPanelHTML() : ""));
  if (keep) keep();
}

// ---------- the creation window ----------

// one-click creation needs Unity Hub (the player signs in there) and the Unity VRChat asks for; ALCOM manages
// the project afterwards. What is missing is offered in 「准备环境」 before anything starts.
async function ocTools() {
  try { const r = await api("/api/pipe/toolchain", {}); if (r.ok) { OC.tool = r.toolchain; OC.toolJob = r.job || OC.toolJob; } } catch (_) {}
  return OC.tool;
}
function ocDir() { return OC.dir || (OC.tool && OC.tool.defaultDir) || ""; }
function ocWhere() { return OC.target || ocDir(); }
function ocReady(t) { return !!(t && t.unity && t.hub && ocWhere()); }
function ocMissing(t) { return !t || !t.unity || !t.hub || !t.license || !(t.alcom || t.vcc) || !ocWhere(); }
// a new project in the one project folder, or the player's own project
function ocStartBody() { return OC.target ? { basket: ocBasket(), target: OC.target } : { basket: ocBasket(), parent: ocDir() }; }
async function ocStart() {
  if (!ocCanStart() || OC.sending) return;
  OC.sending = true;
  const t = await ocTools();
  OC.sending = false;
  if (ocMissing(t)) return ocPrepOpen();
  return ocGo();
}
async function ocGo() {
  if (OC.sending) return;
  OC.sending = true; let r;
  try { r = await api("/api/oneclick/start", ocStartBody()); } catch (e) { r = { ok: false, err: e.message }; }
  OC.sending = false;
  if (!r.ok) { toast(r.err || "无法开始创建", 5000); return; }
  OC.job = r.job; ocJobOpen();
}

// ---------- 「准备环境」 ----------

const OC_TOOLS = [["hub", "Unity Hub", "选择 Unity Hub 的安装位置"], ["unity", "Unity", "选择 Unity 的安装位置"], ["alcom", "ALCOM", "选择 ALCOM 的安装位置"]];
function ocPrepOpen() {
  const m = $("#modal"); m.dataset.kind = "ocprep"; m.classList.add("ocwin");
  ocPrepDraw(); showModal();
  if (OC.toolJob && !["done", "failed"].includes(OC.toolJob.stage)) ocToolWatch();
}
function ocPrepRow(ok, name, state, action) {
  return `<li class="ocprow${ok ? " ok" : ""}"><i aria-hidden="true"></i><span class="nm">${name}</span><span class="st">${state}</span><span class="ocact">${ok ? "" : action}</span></li>`;
}
function ocPrepDraw() {
  const m = $("#modal"); if (m.dataset.kind !== "ocprep") return;
  const t = OC.tool || {}, j = OC.toolJob, busy = !!(j && !["done", "failed"].includes(j.stage));
  const install = tool => `<button class="btn small" data-oc="install" data-tool="${tool}"${busy ? " disabled" : ""}>下载安装</button>`;
  const rows = [
    ocPrepRow(!!t.hub, "Unity Hub", t.hub ? "已安装" : "未安装", install("hub")),
    ocPrepRow(!!t.unity, `Unity <span data-i18n="off">${esc(t.unityWant || "")}</span>`, t.unity ? "已安装" : "未安装",
      install("unity") + (t.hub && t.hubInstallLink ? `<button class="btn small ghost" data-url="${esc(t.hubInstallLink)}">用 Unity Hub 安装</button>` : "")),
    t.hub ? ocPrepRow(!!t.license, "Unity 账号", t.license ? "已登录" : "未登录", `<button class="btn small" data-oc="hub">登录 Unity Hub</button>`) : "",
    ocPrepRow(!!(t.alcom || t.vcc), ocTerm("alcom", "ALCOM"), t.alcom ? "已安装" : t.vcc ? "VCC" : "未安装", install("alcom")),
    ocPrepRow(!!ocWhere(), OC.target ? "已有工程" : "工程位置", ocWhere() ? `<span data-i18n="off">${esc(ocWhere())}</span>` : "未选择", ""),
  ];
  let progress = "";
  if (j) {
    const pct = j.total > 0 ? Math.min(100, Math.floor(j.got * 100 / j.total)) : 0;
    const size = j.stage === "download" && j.total > 0 ? ` <span class="muted small">${(j.got / 1048576).toFixed(0)} / ${(j.total / 1048576).toFixed(0)} MB</span>` : "";
    progress = busy ? `<div class="ocjobline"><span>${esc(j.msg)}</span>${size}<button class="btn small ghost" data-oc="toolcancel">取消</button>
      ${j.stage === "download" ? `<span class="ocprog"><i style="width:${pct}%"></i></span>` : ""}</div>` : j.stage === "failed" ? `<p class="warnc">${esc(j.err || j.msg)}</p>` : "";
    if (busy && j.warn) progress += `<p class="ochint">${esc(j.warn)}</p>`; // the drive is left nearly full
  }
  put(m, `<div class="mhead">准备环境</div>
    <div class="mbody"><ul class="ocprep">${rows.join("")}</ul>${progress}</div>
    <div class="mfoot"><button class="btn" data-oc="dir">更改工程位置</button><button class="btn" data-oc="recheck">重新检测</button>
      <button class="btn primary" data-oc="go"${ocReady(t) && !busy ? "" : " disabled"}>开始创建</button></div>`);
}
async function ocPickDir(title, initial) {
  const r = await api("/api/pickfolder", { title: window.I18N ? I18N.t(title) : title, initial: initial || "" });
  if (r.ok) return r.path;
  if (!r.cancelled) toast(r.err || "无法打开文件夹选择窗口", 4000);
  return "";
}
async function ocInstall(tool) {
  const def = OC_TOOLS.find(x => x[0] === tool); if (!def) return;
  const dir = await ocPickDir(def[2], (OC.tool && OC.tool.installDirs && OC.tool.installDirs[tool]) || "");
  if (!dir) return;
  let r; try { r = await api("/api/tools/install", { tool, dir }); } catch (e) { r = { ok: false, err: e.message }; }
  if (!r.ok) { toast(r.err || "无法开始安装", 5000); return; }
  OC.toolJob = r.job; ocPrepDraw(); ocToolWatch();
}
function ocToolWatch() {
  clearInterval(OC.toolPoll);
  OC.toolPoll = setInterval(async () => {
    let r; try { r = await api("/api/tools/job", {}); } catch (_) { return; }
    OC.toolJob = r.job;
    if (!r.job || ["done", "failed"].includes(r.job.stage)) { clearInterval(OC.toolPoll); await ocTools(); }
    ocPrepDraw();
  }, 1000);
}
async function ocChooseDir() {
  if (OC.target) return ocChooseTarget();
  const dir = await ocPickDir("选择工程保存位置", ocDir());
  if (!dir) return;
  OC.dir = dir;
  try { localStorage.setItem(OC_DIR, dir); } catch (_) {}
  ocDraw(); ocPrepDraw(); ocEstimate();
}
// a project of the player's own; the server checks that it is a Unity 2022.3 project when the creation starts
async function ocChooseTarget() {
  const dir = await ocPickDir("选择要放入的 Unity 工程", OC.target || ocDir());
  if (dir) return ocSetTarget(dir);
  const s = document.querySelector("select[data-oc='where']"); // cancelled: the choice stays as it was
  if (s) s.value = OC.target ? "existing" : "new";
}
function ocSetTarget(dir) {
  OC.target = dir;
  try { localStorage.setItem(OC_TARGET, dir); } catch (_) {}
  ocDraw(); ocPrepDraw(); ocEstimate();
}
// the job as the program has it: a GET, which needs the page's token too (api() sends it with a POST only; without
// it the program answers 403 and the page reloads itself)
async function ocStatus() {
  const r = await fetch("/api/oneclick/status", { headers: { "X-Token": window.API_TOKEN } });
  if (!r.ok) throw new Error("status " + r.status);
  return r.json();
}
function ocJobOpen() {
  if (OC.job && OC.job.result && OC.job.result.upload && (!OC.up || OC.up.id !== OC.job.id)) ocUploadOpen(OC.job);
  const m = $("#modal"); m.dataset.kind = "oneclick"; m.classList.add("ocwin");
  m._h = ""; // (another window may have filled the modal since the job was last drawn the same)
  ocJobDraw(); showModal();
  clearInterval(OC.poll); OC.poll = setInterval(ocPoll, 1500);
  if (OC.job && OC.job.result && OC.job.result.uploaded) ocQuestCheck(OC.job, true);
}
async function ocPoll() {
  const m = $("#modal");
  if (!m.classList.contains("on") || m.dataset.kind !== "oneclick") { clearInterval(OC.poll); m.classList.remove("ocwin"); return; }
  let r; try { r = await ocStatus(); } catch (_) { return; }
  if (r.ok && r.job) { OC.job = r.job; ocJobDraw(); }
  if (OC.job && ["done", "stopped", "failed"].includes(OC.job.state) && !ocQuestBusy(OC.job) && !(OC.quest && OC.quest.sending)) clearInterval(OC.poll);
  if (OC.job && OC.job.state === "failed" && OC.job.report) ocReportRecheck(OC.job.id);
}
function ocStepHTML(s) {
  const pct = s.status === "running" && s.progress > 0 ? `<span class="ocprog"><i style="width:${Math.max(0, Math.min(100, s.progress))}%"></i></span>` : "";
  return `<li class="ocstep ${esc(s.status)}"><i aria-hidden="true"></i><span class="nm">${esc(s.name)}</span>${pct}${s.message ? `<span class="msg">${esc(s.message)}</span>` : ""}${s.status === "failed" ? ocActionHTML(s.action) : ""}</li>`;
}
const OC_ACTIONS = { update_plugin: "更新插件", reconnect: "重新连接", open_unity: "打开 Unity", install_android: "安装 Android 模块", open_quest: "打开 Quest 页", signin_sdk: "打开 SDK 面板" };
function ocActionHTML(action) {
  return Object.hasOwn(OC_ACTIONS, action) ? `<button class="btn small ocaction" data-oc="action" data-action="${action}"${OC.fixBusy ? " disabled" : ""}>${OC_ACTIONS[action]}</button>` : "";
}
async function ocAction(j, action) {
  // Android 模块: a large download whose licences the player accepts by installing it
  if (action === "install_android" && !confirm("将下载约 1.7 GB 并安装 Unity 的 Android 模块，安装即表示同意 Unity 和 Android SDK 的许可协议。继续吗？")) return;
  if (!j || OC.fixBusy || !Object.hasOwn(OC_ACTIONS, action)) return;
  if (action === "open_quest") { $("#modal").classList.remove("ocwin"); return qvOpen(j.project); }
  OC.fixBusy = true; OC.fix = null; ocJobDraw();
  let r; try { r = await api("/api/oneclick/action", { id: j.id, action }); } catch (e) { r = { ok: false, err: e.message }; }
  OC.fixBusy = false;
  if (OC.job && OC.job.id === j.id) {
    if (!r.ok) OC.fix = { id: j.id, err: r.err || "操作未完成", action: r.action };
    ocJobDraw();
  }
  if (r.ok) toast(r.note || "操作已完成，请重试", 5000);
}
function ocNeedHTML(n) {
  let body = "";
  if (n.kind === "packages") body = (n.choices || []).map(c => `<label class="check"><input type="checkbox" data-ocpkg value="${esc(c.path)}"${c.pick ? " checked" : ""}> <span data-i18n="off">${esc(c.name || c.path)}</span>${c.note ? ` <span class="muted small">${esc(c.note)}</span>` : ""}</label>`).join("");
  else if (n.kind === "prefab") body = (n.prefabs || []).map((p, i) => `<label class="check"><input type="radio" name="ocprefab" data-ocprefab value="${esc(p)}"${i === 0 ? " checked" : ""}> <span data-i18n="off">${esc(p)}</span></label>`).join("");
  else if (n.kind === "original") body = ocMenuHTML(n);
  const label = n.kind === "original" ? "移除勾选的菜单" : n.kind === "packages" || n.kind === "prefab" ? "使用所选" : "已完成，继续";
  return `<div class="ocneed"><p>${esc(n.message)}</p>${n.kind === "packages" ? pickAllHTML((n.choices || []).length) : ""}${body ? `<div class="occhoices">${body}</div>` : ""}
    <div class="btnrow">${n.kind === "original" ? `<button class="btn small ghost" data-oc="keepall">全部保留</button>` : ""}<button class="btn small primary" data-oc="reply">${label}</button></div></div>`;
}
// The base's original menus grouped by top-level menu. A group stands for its first entries; it opens to the level
// below (the entries of a single top-level submenu, or the first entries themselves). Ticks live in OC.msel, so a
// redraw of the window keeps them.
function ocMenuGroups(menus) {
  const by = new Map(), depth = m => (m.path || []).length || 1;
  for (const m of menus || []) { const k = (m.path && m.path[0]) || m.name; if (!by.has(k)) by.set(k, []); by.get(k).push(m); }
  return [...by].map(([name, list]) => {
    const d = Math.min(...list.map(depth)), roots = list.filter(m => depth(m) === d);
    const subs = roots.length === 1 && d === 1 ? list.filter(m => depth(m) === 2) : roots;
    return { name, roots, subs, bits: roots.reduce((s, m) => s + (m.bits || 0), 0), fixed: roots.some(m => m.editable === false) };
  });
}
function ocMenuSel(n) {
  if (!OC.msel || OC.msel.id !== n.id) OC.msel = { id: n.id, g: new Set(ocMenuGroups(n.menus).filter(g => !g.fixed && g.roots.every(m => m.suggested)).map(g => g.name)), s: new Set(), open: new Set() };
  return OC.msel;
}
function ocMenuHTML(n) {
  const sel = ocMenuSel(n);
  return ocMenuGroups(n.menus).map(g => {
    const on = sel.g.has(g.name), open = sel.open.has(g.name), more = g.subs.length > 1 || (g.subs.length === 1 && g.subs[0] !== g.roots[0]);
    return `<div class="ocmg"><label class="check"><input type="checkbox" data-ocmg="${esc(g.name)}"${on ? " checked" : ""}${g.fixed ? " disabled" : ""}> <span data-i18n="off">${esc(g.name)}</span></label>
      <span class="muted small">${g.bits} 位</span>${more ? `<button class="btn small ghost" data-oc="mopen" data-g="${esc(g.name)}">${open ? "收起" : "展开"}</button>` : ""}</div>
      ${open && more ? `<div class="ocmsubs">${g.subs.map(m => `<label class="check"><input type="checkbox" data-ocms="${esc(m.id)}" data-g="${esc(g.name)}"${on || sel.s.has(m.id) ? " checked" : ""}${m.editable === false ? " disabled" : ""}> <span data-i18n="off">${esc(m.name)}</span> <span class="muted small">${m.bits} 位</span></label>`).join("")}</div>` : ""}`;
  }).join("");
}
function ocMenuTick(input) {
  const n = OC.job && OC.job.need; if (!n || n.kind !== "original") return;
  const sel = ocMenuSel(n), g = ocMenuGroups(n.menus).find(x => x.name === input.dataset.g || x.name === input.dataset.ocmg); if (!g) return;
  const editable = g.subs.filter(m => m.editable !== false).map(m => m.id);
  g.subs.forEach(m => sel.s.delete(m.id));
  if (input.dataset.ocmg !== undefined) { if (input.checked) sel.g.add(g.name); else sel.g.delete(g.name); }
  else {
    const ticked = new Set(sel.g.has(g.name) ? editable : [...document.querySelectorAll(`[data-ocms][data-g="${CSS.escape(g.name)}"]:checked`)].map(x => x.dataset.ocms));
    if (input.checked) ticked.add(input.dataset.ocms); else ticked.delete(input.dataset.ocms);
    sel.g.delete(g.name);
    if (!g.fixed && editable.length && editable.every(id => ticked.has(id))) sel.g.add(g.name); else ticked.forEach(id => sel.s.add(id));
  }
  ocJobDraw();
}
function ocMenuRemove() {
  const n = OC.job.need, sel = ocMenuSel(n);
  return ocMenuGroups(n.menus).flatMap(g => sel.g.has(g.name) ? g.roots.map(m => m.id) : g.subs.filter(m => sel.s.has(m.id)).map(m => m.id));
}
// the result: pictures, figures, the first notes (the rest behind 「展开」) and a few of the names left untranslated
function ocResultHTML(j) {
  const r = j.result || {}, perf = Object.entries(r.performance || {}), notes = r.warnings || [], left = r.untranslated || [];
  const rank = v => (typeof RANK_ZH === "object" && RANK_ZH[v]) || v, platform = k => ({ pc: "PC", quest: "Quest" })[k] || k;
  return `<div class="ocresult">
    ${(r.images || []).length ? `<div class="ocshots">${r.images.slice(0, 4).map(n => `<img src="/shot?f=${encodeURIComponent(n)}" alt="">`).join("")}</div>` : ""}
    <div class="ocfacts"><span>${ocTerm("params", `参数 ${r.parameterBits}${r.parameterEstimate ? "（估算）" : ""} / 256${r.parameterBitsRaw > r.parameterBits ? `（压缩前 ${r.parameterBitsRaw}）` : ""}`)}</span>${perf.map(([k, v]) => `<span>${ocTerm("rank", `${esc(platform(k))}：${esc(rank(v))}`)}</span>`).join("")}</div>
    ${ocPluginsHTML(r.plugins || [])}
    ${OC.up && OC.up.id === j.id ? ocUploadHTML(j) : ""}
    ${r.uploaded && !(OC.up && OC.up.id === j.id && OC.up.state === "done") ? `<div class="ocquest"><div class="btnrow">${ocQuestButton(j)}</div>${ocQuestLine(j)}</div>` : ""}
    ${(OC.notesOpen ? notes : notes.slice(0, 3)).map(w => `<p class="ochint">${esc(w)}</p>`).join("")}
    ${notes.length > 3 ? `<button class="btn small ghost" data-oc="notes">${OC.notesOpen ? "收起" : "展开"}</button>` : ""}
    ${left.length ? `<p class="ochint">未翻译：<span data-i18n="off">${esc(left.slice(0, 6).join("、"))}</span>${left.length > 6 ? ` 等 ${left.length} 项` : ""}</p>` : ""}
    <div class="btnrow">${typeof frOpen === "function" ? `<button class="btn small" data-oc="fit">3D 预览</button><button class="btn small" data-oc="parts">部件与穿模</button>` : ""}${typeof openAI === "function" ? `<button class="btn small" data-oc="askai" title="在 AI 助手中询问这个模型的问题">问 AI</button>` : ""}<button class="btn small ghost" data-oc="folder">打开工程文件夹</button>${OC.tool && OC.tool.alcom ? `<button class="btn small ghost" data-oc="alcom">在 ALCOM 中打开</button>` : ""}${typeof socialPublishOneClick === "function" ? `<button class="btn small" data-oc="publish">发布到社区</button>` : ""}${j.undone ? `<span class="muted small">已撤销</span>` : `<button class="btn small ghost" data-nb="ocundo">撤销这次改动</button>`}<button class="btn small primary" data-oc="upload"${j.undone ? " disabled" : ""}>上传到 VRChat</button></div></div>`;
}
// 上传到 VRChat: the VRChat SDK in the job's Unity builds the avatar and uploads it to the player's account
// (oneclick_upload.go); the player signs in once in the SDK's panel and confirms there that they own the rights
const OC_UP_STATE = { opening: "已在 Unity 中打开 VRChat SDK 面板，登录后再次点击「上传」", building: "正在构建并上传，Unity 弹出版权确认时请点击 OK",
  busy: "Unity 正在进行另一项构建，请稍后再试", other: "其他构建已结束，可以上传", done: "已上传到你的 VRChat 账号" };
// the account may not upload yet: VRChat allows it from the trust rank New User (blue) on
const OC_UP_NOPERM = "该 VRChat 账号暂时不能上传模型：信任等级需达到 New User（蓝色）。在 VRChat 中多玩一段时间后会自动提升，届时再上传";
const OC_UP_AGREEMENT = "请切换到 Unity，在 VRChat SDK 面板中确认版权声明（Copyright ownership agreement）后点击「OK」";
function ocUploadHTML(j) {
  const u = OC.up, going = u.state === "building" || u.state === "busy" || u.sending;
  return `<div class="ocupload">
    <label class="ocufield"><span>名称</span><input id="oc_up_name" maxlength="64" value="${esc(u.name)}"${going ? " disabled" : ""}></label>
    <div class="seg ocurel" role="radiogroup" aria-label="公开范围"><button class="segbtn${u.release !== "public" ? " on" : ""}" role="radio" aria-checked="${u.release !== "public"}" data-ocup="private"${going ? " disabled" : ""}>仅自己可见</button><button class="segbtn${u.release === "public" ? " on" : ""}" role="radio" aria-checked="${u.release === "public"}" data-ocup="public"${going ? " disabled" : ""}>公开</button></div>
    <div class="btnrow">${u.state === "done" ? `<button class="btn small" data-oc="uploadweb">在网页上查看</button>${ocQuestButton(j)}` : `<button class="btn small primary" data-oc="uploadgo"${going ? " disabled" : ""}>${going ? `<i class="spin"></i>` : ""}上传</button>`}<button class="btn small ghost" data-oc="uploadclose">关闭</button></div>
    ${u.state === "done" ? ocQuestLine(j) : ""}${ocActionHTML(u.action)}
    ${u.state === "failed" ? `<p class="warnc">${esc("上传失败：" + (u.err || ""))}</p>` : u.state === "noperm" ? `<p class="warnc">${esc(OC_UP_NOPERM)}</p>` : OC_UP_STATE[u.state] ? `<p class="ochint">${esc(u.agreement ? OC_UP_AGREEMENT : OC_UP_STATE[u.state])}</p>` : ""}</div>`;
}
// Quest readiness comes from the existing page's status; progress belongs to the saved one-click result.
const OC_QUEST_STATE = { checking: "正在检查 Quest 上传条件", pc: "正在更新 PC 版", switching: "正在切换到 Android 平台，第一次需要重新导入全部素材，可能需要十几分钟",
  building: "正在构建并上传 Quest 版", back: "正在切换回 Windows 平台", done: "Quest 版已上传" };
function ocQuestBusy(j) { return !!(j && j.result && j.result.quest && ["checking", "pc", "switching", "building", "back"].includes(j.result.quest.state)); }
function ocQuestButton(j) {
  const q = OC.quest && OC.quest.id === j.id ? OC.quest : {}, busy = !!j.undone || ocQuestBusy(j) || q.sending;
  return `<button class="btn small" data-oc="${q.ready ? "questupload" : "quest"}"${busy ? " disabled" : ""}>${q.ready ? "上传 Quest 版" : "Quest 版"}</button>`;
}
function ocQuestLine(j) {
  const q = j.result && j.result.quest, local = OC.quest && OC.quest.id === j.id ? OC.quest : {};
  const err = local.err || (q && q.state === "failed" ? q.error : "");
  const line = err || (local.sending ? OC_QUEST_STATE.checking : q && (q.agreement ? OC_UP_AGREEMENT : OC_QUEST_STATE[q.state]));
  return (line ? `<p class="ocquestline ${err ? "warnc" : "ochint"}">${esc(line)}</p>` : "") + ocActionHTML(local.action || (q && q.action));
}
async function ocQuestCheck(j, refresh = false) {
  if (!j.result || !j.result.uploaded || ocQuestBusy(j)) return;
  if (OC.quest && OC.quest.id === j.id && (!refresh || OC.quest.loading || OC.quest.sending)) return;
  const q = OC.quest = { id: j.id, ready: false, loading: true };
  let r; try { r = await api("/api/quest/status", { project: j.project }); } catch (_) { r = {}; }
  if (OC.quest !== q || !OC.job || OC.job.id !== j.id) return;
  q.loading = false;
  const u = r.ok && r.unity;
  q.ready = !!(u && u.vqt && u.ndmf && u.setUp && u.android);
  if ($("#modal").dataset.kind === "oneclick") ocJobDraw();
}
async function ocQuestUpload(j) {
  const q = OC.quest; if (!q || q.id !== j.id || !q.ready || q.sending || ocQuestBusy(j)) return;
  q.sending = true; q.err = ""; q.action = ""; ocJobDraw();
  clearInterval(OC.poll); OC.poll = setInterval(ocPoll, 1500);
  let r; try { r = await api("/api/oneclick/quest-upload", { id: j.id }); } catch (_) { r = { ok: false, err: "程序无响应" }; }
  q.sending = false;
  if (!OC.job || OC.job.id !== j.id) return;
  if (r.ok) OC.job = r.job;
  else { q.ready = false; q.err = r.err || "上传 Quest 版失败，请在 Unity 中检查后重试"; q.action = r.action; }
  if ($("#modal").dataset.kind === "oneclick") ocJobDraw();
}
function ocUploadOpen(j) {
  if (j.undone) return;
  if (OC.up && OC.up.id === j.id) { ocJobDraw(); return; }
  if (OC.up) clearTimeout(OC.up.poll);
  const saved = (j.result && j.result.upload) || {};
  OC.up = { id: j.id, name: (j.avatar || j.name || "").slice(0, 64), release: "private", state: saved.state || "", err: saved.error || "", action: saved.action, op: saved.op, agreement: !!saved.agreement, poll: 0 };
  if (["building", "busy"].includes(OC.up.state)) ocUploadPoll(j);
  ocJobDraw();
}
async function ocUploadGo(j) {
  const u = OC.up; if (!u || u.sending || ["building", "busy"].includes(u.state)) return;
  const el = $("#oc_up_name"); if (el) u.name = el.value.trim();
  u.sending = true; u.state = ""; u.err = ""; u.action = ""; u.agreement = false; ocJobDraw();
  let r; try { r = await api("/api/oneclick/upload", { id: j.id, name: u.name, release: u.release }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  u.sending = false;
  if (OC.up !== u || !OC.job || OC.job.id !== j.id) return;
  if (!r.ok) { u.state = "failed"; u.err = r.err || ""; u.action = r.action; ocJobDraw(); return; }
  u.state = r.upload.state; u.err = r.upload.error || ""; u.agreement = !!r.upload.agreement; u.action = r.upload.action; u.op = r.upload.op;
  OC.job.result.upload = r.upload;
  ocJobDraw();
  if (u.state === "building" || u.state === "busy") ocUploadPoll(j);
}
function ocUploadPoll(j) {
  clearTimeout(OC.up.poll);
  OC.up.poll = setTimeout(async () => {
    const u = OC.up; if (!u || u.id !== j.id) return;
    let r; try { r = await api("/api/oneclick/upload/status", { id: j.id }); } catch (e) { r = { ok: false }; }
    if (OC.up !== u || !OC.job || OC.job.id !== j.id) return;
    if (r.ok) { u.state = r.upload.state === "idle" ? "" : r.upload.state; u.err = r.upload.error || ""; u.agreement = !!r.upload.agreement; u.action = r.upload.action; u.op = r.upload.op; OC.job.result.upload = r.upload; }
    else if (r.action) { u.state = "failed"; u.err = r.err || ""; u.action = r.action; u.agreement = false; }
    if (u.state === "done" && OC.job && OC.job.result) OC.job.result.uploaded = Date.now() / 1000 | 0;
    if ($("#modal").dataset.kind === "oneclick") ocJobDraw();
    if (u.state === "building" || u.state === "busy") ocUploadPoll(j);
  }, 2000);
}
// the plugins of the job: whether the build has them, and those to finish in their own tool in Unity
const OC_PLUGIN = { ok: ["已生效", "ok"], unknown: ["已安装", ""], partial: ["部分生效", "warn"], failed: ["未生效", "err"], setup: ["待设置", "warn"], skipped: ["未安装", "err"], placed: ["已安装", ""],
  applying: ["正在重新应用", ""] }; // (applying: 「重新应用」 on a later version, ocbooth.js)
function ocPluginsHTML(list) {
  if (!list.length) return "";
  return `<div class="ocplugins">${list.map(p => { const [label, cls] = OC_PLUGIN[p.state] || ["", ""]; return `<div class="ocplugin">
    <span class="ocpstate ${cls}">${esc(label)}</span><span class="ocpname" data-i18n="off">${esc(p.name || p.id)}</span>${p.note ? `<span class="ocpnote">${esc(p.note)}</span>` : ""}
    ${p.state === "setup" ? `<button class="btn small" data-oc="tool" data-plugin="${esc(p.id)}" title="${esc(p.menu || "")}">打开设置工具</button>` : ""}${typeof ocbPluginHTML === "function" ? ocbPluginHTML(p) : ""}</div>`; }).join("")}</div>`;
}
async function ocOpenTool(j, plugin) {
  let r; try { r = await api("/api/oneclick/tool", { id: j.id, plugin }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  toast(r.ok ? "已在 Unity 中打开设置工具，模型已选中" : (r.err || "未能打开设置工具"), r.ok ? 4000 : 6000);
}
// 「问 AI」: the AI assistant (app.js openAI) on the job's project with a first question in its input, for the player
// to change or send; the AI reads the job with its oneclick_result tool. Without an AI service set up, sending it
// asks for one, as the assistant always does.
const OC_ASK = "请查看这次一键改模的结果和上传前体检，说明这个模型还有哪些问题、该怎么处理";
async function ocAskAI(j) {
  clearInterval(OC.poll); $("#modal").classList.remove("ocwin"); closeModal(true);
  const same = p => (p.path || "").replace(/\\/g, "/").toLowerCase() === j.project.replace(/\\/g, "/").toLowerCase();
  if (!(S.projs || []).some(same)) await loadProjects(); // (a new project is listed once the list is read again)
  const p = (S.projs || []).find(same);
  await openAI(p ? p.path : j.project);
  if (!AI.proj || !same(AI.proj)) return;
  const q = window.I18N ? I18N.t(OC_ASK) : OC_ASK, ta = $("#ai_text");
  PIPE.text = q;
  if (ta) { ta.value = q; ta.focus(); }
}
function ocJobDraw() {
  const m = $("#modal"), j = OC.job; if (!j || m.dataset.kind !== "oneclick") return;
  if (j.result && j.result.uploaded) ocQuestCheck(j);
  const running = ["running", "need_player", "stopping"].includes(j.state);
  const head = { done: "创建完成", stopped: "已停止", failed: "创建未完成", paused: "已暂停", need_player: "需要你确认" }[j.state] || "正在创建";
  const again = j.state === "paused" ? "继续" : "重试";
  const html = `<div class="mhead"><span>${esc(head)}</span><span class="muted small" data-i18n="off">${esc(j.name || "")}</span></div>
    <div class="mbody"><ol class="ocsteps">${(j.steps || []).map(ocStepHTML).join("")}</ol>
      ${j.state === "need_player" && j.need ? ocNeedHTML(j.need) : ""}
      ${j.error ? `<p class="warnc">${esc(j.error)}</p>` : ""}
      ${OC.fix && OC.fix.id === j.id ? `<p class="warnc">${esc(OC.fix.err)}</p>${ocActionHTML(OC.fix.action)}` : ""}
      ${j.state === "done" ? ocResultHTML(j) : ""}</div>
    <div class="mfoot">${j.state === "failed" ? ocReportHTML(j) : ""}${running ? `<button class="btn" data-oc="stop">停止</button>` : ""}${!running && j.state !== "done" ? `<button class="btn" data-oc="retry">${again}</button>` : ""}<button class="btn" data-oc="close">${running ? "在后台继续" : "关闭"}</button></div>`;
  put(m, html);
}
// 匿名失败报告 (oneclick_report.go): a failed run sends its own when the player switched it on in the settings;
// otherwise it is sent from here, once
const OC_REPORT_NOTE = "一键创建失败时，把失败的步骤和错误信息发给作者，不含账号、文件路径和工程名。";
function ocReportHTML(j) {
  const what = j.report ? `<span class="muted small ocreport" title="${esc(OC_REPORT_NOTE)}">已发送失败报告</span>`
    : `<button class="btn ghost ocreport" data-oc="report" title="${esc(OC_REPORT_NOTE)}"${OC.reporting ? " disabled" : ""}>${OC.reporting ? `<i class="spin"></i>` : ""}发送这次的失败报告</button>`;
  return what + `<span class="spacer"></span>`; // (in the window's foot, left of 「重试」)
}
async function ocReport(j) {
  if (OC.reporting) return;
  OC.reporting = true; ocJobDraw();
  let r; try { r = await api("/api/oneclick/report", { id: j.id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  OC.reporting = false;
  if (r.job && OC.job && r.job.id === OC.job.id) OC.job = r.job;
  ocJobDraw();
  toast(r.ok ? "已发送失败报告" : (r.err || "失败报告未能发送"), r.ok ? 3000 : 5000);
}
// a report sent by itself comes off the job again when it could not go (after at most 10 seconds): looked at once more
function ocReportRecheck(id) {
  clearTimeout(OC.recheck);
  OC.recheck = setTimeout(async () => {
    let r; try { r = await ocStatus(); } catch (_) { return; }
    if (r.ok && r.job && OC.job && r.job.id === id && OC.job.id === id && r.job.state === "failed") { OC.job = r.job; ocJobDraw(); }
  }, 12000);
}
async function ocReply(extra) {
  const j = OC.job; if (!j || !j.need || OC.sending) return;
  OC.sending = true; let r;
  try { r = await api("/api/oneclick/start", { resume: j.id, reply: Object.assign({ needId: j.need.id, confirmed: true }, extra) }); } catch (e) { r = { ok: false, err: e.message }; }
  OC.sending = false;
  if (!r.ok) { toast(r.err || "无法继续", 5000); return; }
  OC.job = r.job; ocJobDraw();
  clearInterval(OC.poll); OC.poll = setInterval(ocPoll, 1500);
}

document.addEventListener("click", async e => {
  const rel = e.target.closest("[data-ocup]");
  if (rel && OC.up) { const el = $("#oc_up_name"); if (el) OC.up.name = el.value; OC.up.release = rel.dataset.ocup; ocJobDraw(); return; }
  const b = e.target.closest("[data-oc]"); if (!b) return;
  const what = b.dataset.oc, j = OC.job;
  switch (what) {
    case "toggle": OC.open = !OC.open; return ocDraw();
    case "clear": {
      const was = OC.items; ocRedraw(was.map(i => i.key)); OC.items = []; OC.est = null; ocChanged();
      return toast(`已清空改模篮，点此撤销`, 5000, () => { OC.items = was; ocRedraw(was.map(i => i.key)); ocChanged(); });
    }
    case "remove": {
      const k = b.closest(".ocitem")?.dataset.key, at = OC.items.findIndex(i => i.key === k), it = OC.items[at]; if (!it) return;
      ocRedraw([k]); OC.items = OC.items.filter(i => i.key !== k); ocChanged();
      return toast(`已移出「${it.name}」，点此撤销`, 5000, () => { if (ocHas(k)) return; OC.items.splice(Math.min(at, OC.items.length), 0, it); ocRedraw([k]); ocChanged(); });
    }
    case "start": return ocStart();
    case "progress": return ocJobOpen();
    case "action": return ocAction(j, b.dataset.action);
    case "stop": { if (!j) return; try { const r = await api("/api/oneclick/stop", { id: j.id }); if (r.ok) { OC.job = r.job; ocJobDraw(); } else toast(r.err || "无法停止"); } catch (err) { toast(err.message); } return; }
    case "retry": {
      if (!j) return; let r;
      try { r = await api("/api/oneclick/start", { resume: j.id }); } catch (err) { r = { ok: false, err: err.message }; }
      if (!r.ok) { toast(r.err || "无法重试", 5000); return; }
      OC.job = r.job; ocJobDraw(); clearInterval(OC.poll); OC.poll = setInterval(ocPoll, 1500); return;
    }
    case "close": clearInterval(OC.poll); $("#modal").classList.remove("ocwin"); return closeModal(true);
    case "reply": {
      const n = j && j.need; if (!n) return;
      if (n.kind === "packages") return ocReply({ packages: [...document.querySelectorAll("[data-ocpkg]:checked")].map(x => x.value) });
      if (n.kind === "prefab") { const p = document.querySelector("[data-ocprefab]:checked"); return ocReply({ prefab: p ? p.value : "" }); }
      if (n.kind === "original") return ocReply({ remove: ocMenuRemove() });
      return ocReply({});
    }
    case "keepall": return ocReply({ remove: [] });
    case "notes": OC.notesOpen = !OC.notesOpen; return ocJobDraw();
    case "dir": return ocChooseDir();
    case "install": return ocInstall(b.dataset.tool);
    case "toolcancel": try { await api("/api/tools/cancel", {}); } catch (_) {} return;
    case "recheck": await ocTools(); return ocPrepDraw();
    case "go": if (!ocReady(OC.tool)) return; closeModal(true); $("#modal").classList.remove("ocwin"); return ocGo();
    case "hub": case "alcom": { let r; try { r = await api("/api/pipe/launch", { what: what }); } catch (e) { r = { ok: false, err: e.message }; } if (!r.ok) toast(r.err || "无法打开", 4000); return; }
    case "mopen": { const sel = j && j.need && ocMenuSel(j.need), g = b.dataset.g; if (!sel) return; if (sel.open.has(g)) sel.open.delete(g); else sel.open.add(g); return ocJobDraw(); }
    case "folder": if (j && j.project) openPath(j.project); return;
    case "fit": if (j && j.project) { $("#modal").classList.remove("ocwin"); frOpen(j.project); } return; // the fitting room, on the avatar just made
    case "tool": if (j) ocOpenTool(j, b.dataset.plugin); return; // a plugin's own setup tool in Unity
    case "askai": if (j && j.project) ocAskAI(j); return;
    case "parts": if (j && j.project) { $("#modal").classList.remove("ocwin"); frOpen(j.project, { tab: "parts" }); } return; // … at its parts and clipping
    case "upload": if (j) ocUploadOpen(j); return;
    case "uploadgo": if (j) ocUploadGo(j); return;
    case "uploadclose": if (OC.up) clearTimeout(OC.up.poll); OC.up = null; ocJobDraw(); return;
    case "uploadweb": openWeb("https://vrchat.com/home/avatars"); return;
    case "quest": if (typeof qvOpen === "function" && OC.job) qvOpen(OC.job.project); return; // (the Quest page: its checks, its setup, the steps to upload)
    case "questupload": if (j) ocQuestUpload(j); return;
    case "publish": if (j && typeof socialPublishOneClick === "function") socialPublishOneClick(j); return; // the post composer with the job's pictures and its 装配方案 (social.js)
    case "report": if (j) ocReport(j); return; // 「发送这次的失败报告」
  }
});
document.addEventListener("change", e => {
  if (e.target.matches("[data-ocmg], [data-ocms]")) return ocMenuTick(e.target);
  if (e.target.matches("select[data-oc='where']")) return e.target.value === "existing" ? ocChooseTarget() : ocSetTarget("");
  const s = e.target.closest("select[data-oc='move']"); if (!s) return;
  ocMove(s.closest(".ocitem")?.dataset.key, s.value);
});

ocLoad();
// The last result and recoverable failures stay reachable after the program or the page restarts.
ocStatus().then(r => { if (r.ok) { OC.job = r.job || null; ocDraw(); } }).catch(() => {});
if (OC.items.length) setTimeout(ocEstimate, 0);
