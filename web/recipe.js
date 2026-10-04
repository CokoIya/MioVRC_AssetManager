// outfitting recipes: save a pipeline run, apply it elsewhere, export and import
"use strict";
// list: the saved recipes (null: not read yet); rev: the state it was read at; ready: the project (readyFor)
// has a run that can be saved; sig: the session as it was when that was last asked; view: what the dialog
// shows (list, save, rename, preview, import); form: what is typed in it, kept over a redraw
const RCP = { list: null, rev: -1, ready: false, readyFor: "", sig: "", loading: false, view: "list", cur: null, pv: null, form: { name: "", note: "" }, imp: null, busy: false };
const RCP_MAX = 512 * 1024;

async function recipeLoad() {
  if (RCP.loading) { RCP.again = true; return; }
  RCP.loading = true; RCP.again = false;
  const path = AI.proj ? AI.proj.path : "";
  try {
    const r = await api("/api/recipe/list", { project: path });
    if (r.ok) { RCP.list = r.recipes || []; RCP.ready = !!r.ready; RCP.readyFor = path; }
  } catch (e) {} finally { RCP.loading = false; RCP.rev = S.rev; if (RCP.list === null) RCP.list = []; } // (a failed read is not tried again until the state changes)
  aiRenderLive();
  recipeListDraw();
  if (RCP.again) recipeLoad(); // asked again meanwhile: what was read may be of before
}
// the list in the open dialog, drawn again only when it reads differently: what has the focus keeps it, and
// how far the dialog is scrolled stays
function recipeListDraw() {
  const el = $("#rcp_list"); if (!el) return;
  const refocus = focusMark(el);
  if (put(el, recipeListHTML())) refocus();
}
// (by what is in the dialog, not by a mark on it: another dialog opened later leaves no trace of this one)
function recipeOpen() { const m = $("#modal"); return m.classList.contains("on") && !!$(".rcpfoot", m); }

// ---------- on the pipeline page ----------
// the button on the 装配 station (a live part of the page: the count follows the list)
function recipeActsHTML() {
  if ((RCP.list === null || RCP.rev !== S.rev) && !RCP.loading) setTimeout(recipeLoad, 0); // saved or imported elsewhere: read again
  const n = (RCP.list || []).length;
  return `<button class="btn small" data-recipe="open" title="将装配结果保存为方案，在其他工程或素体上一键应用，或导出给他人">装配方案${n ? `<span class="rcpn">${esc(n)}</span>` : ""}</button>`;
}
// a run of this project has put something on the avatar or built a menu, and has ended: it can be kept as a
// recipe. The program knows (what was undone is not in it); it is asked again whenever the session moved on.
function recipeCanSave() {
  const s = AI.sess || {}, p = AI.proj ? AI.proj.path : "";
  const sig = [p, s.changes || 0, (s.steps || []).length, !!s.busy].join("|");
  if (sig !== RCP.sig) { RCP.sig = sig; if (!s.busy) setTimeout(recipeLoad, 0); }
  return !s.busy && RCP.ready && RCP.readyFor === p;
}
function recipeSaveHTML() {
  return recipeCanSave() ? `<button class="btn small ghost" data-recipe="save" title="将本次装配的素材和生成的菜单保存为方案">保存为方案</button>` : "";
}

// ---------- the dialog ----------
function recipeKeep() {
  const n = $("#rcp_name"), t = $("#rcp_note");
  if (n) RCP.form.name = n.value;
  if (t) RCP.form.note = t.value;
}
function recipeShow(view) {
  recipeKeep();
  RCP.view = view;
  $("#modal").dataset.kind = "recipe";
  recipeRender(); showModal();
  const f = $("#rcp_name") || $("#modal .mfoot .btn.primary:not([disabled])");
  if (f) { f.focus({ preventScroll: true }); if (f.select) f.select(); }
}
function recipeDate(t) {
  if (!t) return "";
  const d = new Date(t * 1000), p = n => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}
function recipeMeta(r) {
  const parts = [];
  if (r.base) parts.push(`素体 ${esc(r.base)}`);
  parts.push(`${esc(+r.assets || 0)} 项素材`, `${esc(+r.items || 0)} 项菜单`);
  if (r.created) parts.push(esc(recipeDate(r.created)));
  return parts.join(" · ");
}
function recipeListHTML() {
  const list = RCP.list;
  if (list === null) return `<div class="aiempty">正在读取…</div>`;
  if (!list.length) return `<div class="aiempty">暂无方案。<br>流水线运行完成后，可在「验收」步骤点击「保存为方案」，也可导入他人分享的方案文件。</div>`;
  return `<div class="rcplist">${list.map(r => `<div class="rcp" data-rid="${esc(r.id)}">
      <div class="rcpinfo"><div class="nm" title="${esc(r.name)}"><span data-i18n="off">${esc(r.name)}</span>${r.imported ? `<span class="tag faint" title="由方案文件导入">导入</span>` : ""}</div>
        <div class="meta">${recipeMeta(r)}</div>${r.note ? `<div class="note" data-i18n="off">${esc(r.note)}</div>` : ""}</div>
      <div class="rcpacts"><button class="btn small primary" data-recipe="apply" data-id="${esc(r.id)}">应用</button><button class="btn small" data-recipe="rename" data-id="${esc(r.id)}">重命名</button><button class="btn small" data-recipe="export" data-id="${esc(r.id)}">导出</button><button class="btn small ghost" data-recipe="delete" data-id="${esc(r.id)}">删除</button></div>
    </div>`).join("")}</div>`;
}
const RCP_STATUS = { dress: ["", "将装配"], worn: ["ok", "已在模型上"], missing: ["warn", "未导入"] };
function recipePreviewHTML() {
  const pv = RCP.pv, p = AI.proj;
  if (!pv) return `<div class="aiempty">正在检查工程中的素材…</div>`;
  const rows = pv.rows || [];
  const row = a => {
    const st = RCP_STATUS[a.status] || ["", a.status], marks = [], acts = [];
    marks.push(`<span class="tag ${esc(st[0])}">${esc(st[1])}</span>`);
    if (a.otherBase) marks.push(`<span class="tag warn" title="该版本为其他素体制作，仍会装配，可能不合身">为 ${esc(a.otherBase)} 制作</span>`);
    if (a.via === "version") marks.push(`<span class="tag faint" title="${esc(a.resolved)}">已改用当前素体的版本</span>`);
    if (a.via === "library") marks.push(`<span class="tag faint" title="${esc(a.resolved)}">按素材库记录找到</span>`);
    if (a.status === "missing" && a.libKey) acts.push(`<button class="btn small" data-recipe="libimport" data-key="${esc(a.libKey)}" title="在素材库中打开该素材，使用「一键导入」导入到当前工程">一键导入</button>`);
    if (a.status === "missing" && a.link) acts.push(`<button class="btn small ghost" data-recipe="link" data-href="${esc(a.link)}" title="${esc(a.link)}">商品页</button>`);
    return `<tr class="${a.status === "missing" ? "dim" : ""}"><td class="nm" title="${esc(a.name)}">${esc(a.name)}</td><td>${esc(a.kind)}</td><td class="marks">${marks.join("")}</td><td class="r">${acts.join("")}</td></tr>`;
  };
  const menu = pv.items ? (pv.dropped ? `方案共 ${esc(+pv.items)} 项菜单，其中 ${esc(+pv.dropped)} 项因所需素材未导入而不会生成。` : `方案共 ${esc(+pv.items)} 项菜单，将按保存时的结构生成。`) : "该方案不含菜单。";
  return `<div class="rcphead"><b>${esc(pv.recipe.name)}</b><span class="meta">${recipeMeta(pv.recipe)}</span></div>
    <div class="rcptarget">应用到工程「${esc(p ? p.name : "")}」${pv.avatar ? `中的模型「${esc(pv.avatar)}」` : ""}</div>
    ${(pv.notes || []).map(n => `<div class="rcpnote">${esc(n)}</div>`).join("")}
    <table class="atable rcptable"><thead><tr><th>素材</th><th>类别</th><th>状态</th><th></th></tr></thead><tbody>${rows.map(row).join("")}</tbody></table>
    <div class="hint2">${menu}应用过程不使用 AI，每一步均可通过「撤销上一步」撤销，场景不会自动保存。</div>
    ${pv.why ? `<div class="aitest err" role="status">${esc(pv.why)}</div>` : ""}`;
}
function recipeImportHTML() {
  const x = RCP.imp; if (!x) return "";
  const r = x.recipe, needs = x.needs || [];
  let items = 0; for (const m of r.menus || []) items += (m.items || []).length;
  return `<div class="rcphead"><b>${esc(r.name)}</b><span class="meta">${recipeMeta({ base: (r.base || {}).name, assets: (r.assets || []).length, items, created: r.created })}</span></div>
    ${r.note ? `<div class="rcpnote plain" data-i18n="off">${esc(r.note)}</div>` : ""}
    <div class="row"><label>所需素材</label>
    <table class="atable rcptable"><tbody>${needs.map(n => `<tr><td class="nm" title="${esc(n.name)}">${esc(n.name)}</td><td>${esc(n.kind)}${n.version ? `<span class="muted small">　${esc(n.version)} 版本</span>` : ""}</td>
      <td class="marks">${n.libKey ? `<span class="tag ok">素材库中已有</span>` : `<span class="tag">素材库中没有</span>`}</td>
      <td class="r">${n.link ? `<button class="btn small ghost" data-recipe="link" data-href="${esc(n.link)}" title="${esc(n.link)}">商品页</button>` : ""}</td></tr>`).join("") || `<tr><td class="muted">该方案不含素材</td></tr>`}</tbody></table></div>
    <div class="hint2">方案只记录素材在工程中的位置和菜单结构，不包含素材文件。导入后，在装有这些素材的工程中即可应用。</div>`;
}
function recipeFormHTML(lead) {
  const f = RCP.form;
  return `<p class="lead">${lead}</p>
    <div class="row"><label for="rcp_name">名称</label><input id="rcp_name" maxlength="60" value="${esc(f.name)}" placeholder="例如：日常三套 + 樱发" autocomplete="off" spellcheck="false"></div>
    <div class="row"><label for="rcp_note">备注（选填）</label><textarea id="rcp_note" maxlength="500" placeholder="例如：适用的素体、需要注意的事项">${esc(f.note)}</textarea></div>`;
}
function recipeRender() {
  const m = $("#modal"), v = RCP.view, dis = RCP.busy ? " disabled" : "";
  let head = "装配方案", body, foot;
  if (v === "save") {
    head = "保存为方案";
    body = recipeFormHTML("保存本次流水线装配的素材和生成的菜单。方案只记录素材在工程中的位置、素材库信息和菜单结构，不包含素材文件和本机路径。");
    foot = `<button class="btn ghost" data-recipe="open">方案列表</button><span class="spacer"></span><button class="btn ghost" data-recipe="close">取消</button><button class="btn primary" data-recipe="savego"${dis}>保存</button>`;
  } else if (v === "rename") {
    head = "重命名方案";
    body = recipeFormHTML("修改方案的名称和备注。");
    foot = `<span class="spacer"></span><button class="btn ghost" data-recipe="open">返回</button><button class="btn primary" data-recipe="renamego"${dis}>保存</button>`;
  } else if (v === "preview") {
    head = "应用方案";
    body = recipePreviewHTML();
    const ok = RCP.pv && RCP.pv.canApply;
    foot = `<button class="btn ghost" data-recipe="open">返回</button><button class="btn ghost" data-recipe="recheck"${RCP.pv ? "" : " disabled"}>重新检查</button><span class="spacer"></span><button class="btn primary" data-recipe="applygo"${ok && !RCP.busy ? "" : " disabled"}>应用到当前工程</button>`;
  } else if (v === "import") {
    head = "导入方案";
    body = recipeImportHTML();
    foot = `<span class="spacer"></span><button class="btn ghost" data-recipe="open">取消</button><button class="btn primary" data-recipe="importgo"${dis}>导入</button>`;
  } else {
    body = `<p class="lead">方案记录一次流水线装配的素材和菜单，可在其他工程或其他素体上直接应用，也可导出为文件分享。</p><div id="rcp_list"></div>`;
    foot = `<button class="btn" data-recipe="import">导入方案…</button>${recipeCanSave() ? `<button class="btn" data-recipe="save">保存当前结果为方案</button>` : ""}<span class="spacer"></span><button class="btn primary" data-recipe="close">关闭</button>
      <input type="file" id="rcp_file" accept=".json,application/json" hidden>`;
  }
  m.innerHTML = `<div class="mhead">${head}</div><div class="mbody">${body}</div><div class="mfoot rcpfoot">${foot}</div>`;
  recipeListDraw();
}
function recipeById(id) { return (RCP.list || []).find(r => r.id === id); }
async function recipePreview(id) {
  const p = AI.proj; if (!p) { toast("请先选择工程"); return; }
  RCP.cur = id; RCP.pv = null; recipeShow("preview");
  let r; try { r = await api("/api/recipe/preview", { id, project: p.path }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!recipeOpen() || RCP.view !== "preview" || RCP.cur !== id) return;
  if (!r.ok) { toast(r.err || "无法检查该方案", 4500); return recipeShow("list"); }
  RCP.pv = r.preview; recipeRender();
  const go = $('#modal [data-recipe="applygo"]:not([disabled])'); if (go) go.focus({ preventScroll: true });
}
async function recipeAction(b) {
  const what = b.dataset.recipe, id = b.dataset.id, p = AI.proj;
  if (what === "close") return closeModal();
  if (what === "open") {
    const was = RCP.view;
    recipeShow("list");
    if (was === "rename") RCP.form = { name: "", note: "" }; // what was typed for another recipe is not a new one's name
    return recipeLoad();
  }
  if (what === "link") {
    const u = b.dataset.href;
    if (S.data.paneMode && (isBoothURL(u) || webKind(u) !== "booth")) closeModal(); // a page inside the program would open behind the dialog
    return openLink(u);
  }
  if (what === "save") { if (p) recipeShow("save"); return; }
  if (what === "savego" || what === "renamego") {
    recipeKeep();
    const name = RCP.form.name.trim();
    if (!name) { toast("请填写方案名称"); const n = $("#rcp_name"); if (n) n.focus(); return; }
    if (RCP.busy) return; // Enter and the button: not twice
    RCP.busy = true; b.disabled = true; let r;
    try { r = await api(what === "savego" ? "/api/recipe/save" : "/api/recipe/rename", what === "savego" ? { project: p.path, name, note: RCP.form.note } : { id: RCP.cur, name, note: RCP.form.note }); }
    catch (e) { r = { ok: false, err: "程序无响应" }; }
    finally { RCP.busy = false; }
    if (!r.ok) { toast(r.err || "保存失败", 4500); if (recipeOpen()) recipeRender(); return; }
    toast(what === "savego" ? "已保存为方案" : "已保存");
    RCP.form = { name: "", note: "" };
    if (recipeOpen()) recipeShow("list");
    return recipeLoad();
  }
  if (what === "rename") {
    const r = recipeById(id); if (!r) return;
    RCP.cur = id; RCP.view = "rename"; RCP.form = { name: r.name, note: r.note || "" };
    recipeRender();
    const n = $("#rcp_name"); n.focus(); n.select(); return;
  }
  if (what === "delete") {
    const r = recipeById(id); if (!r) return;
    if (!confirm(`删除方案「${r.name}」？已装配到工程中的内容不受影响。`)) return;
    const res = await api("/api/recipe/delete", { id });
    toast(res.ok ? "已删除" : res.err || "删除失败", 3500);
    return recipeLoad();
  }
  if (what === "export") {
    const dir = await pickInto("选择方案文件的保存位置"); if (!dir) return;
    const res = await api("/api/recipe/export", { id, dir });
    if (!res.ok) { toast(res.err || "导出失败", 4500); return; }
    toast("已导出：" + res.file + "（点击打开所在文件夹）", 7000, () => openPath(dir)); return;
  }
  if (what === "apply") return recipePreview(id);
  if (what === "recheck") return recipePreview(RCP.cur);
  if (what === "applygo") {
    if (!p || RCP.busy) return;
    RCP.busy = true; b.disabled = true; let r;
    try { r = await api("/api/recipe/apply", { id: RCP.cur, project: p.path }); } catch (e) { r = { ok: false, err: "程序无响应" }; } finally { RCP.busy = false; }
    if (!r.ok) { toast(r.err || "无法应用该方案", 5000); if (recipeOpen()) recipeRender(); return; }
    closeModal();
    const log = $("#ai_log"); if (log) log.scrollTop = log.scrollHeight;
    const sec = $("#stn5"); if (sec) sec.scrollIntoView({ behavior: "smooth", block: "start" });
    return aiPoll();
  }
  if (what === "libimport") {
    // the library's card for it, at its 「一键导入」, with this project as the target
    const a = findAsset(b.dataset.key);
    if (!a) { toast("素材库中未找到该素材，可能已被移除"); return; }
    if (p) setProject(p.path);
    closeModal(); setView("lib"); renderDrawer(a.key);
    const sec = $("#impsec"); if (sec) { sec.scrollIntoView({ block: "start" }); const sel = $("#imp_proj"); if (sel) sel.focus(); }
    return;
  }
  if (what === "import") { const f = $("#rcp_file"); if (f) { f.value = ""; f.click(); } return; }
  if (what === "importgo") {
    if (!RCP.imp || RCP.busy) return;
    RCP.busy = true; b.disabled = true; let r;
    try { r = await api("/api/recipe/import", { text: RCP.imp.text, save: true }); } catch (e) { r = { ok: false, err: "程序无响应" }; } finally { RCP.busy = false; }
    if (!r.ok) { toast(r.err || "导入失败", 5000); if (recipeOpen()) recipeRender(); return; }
    RCP.imp = null; toast("已导入方案「" + r.recipe.name + "」", 3500);
    if (recipeOpen()) recipeShow("list");
    return recipeLoad();
  }
}
// a file somebody else made: read as text, looked at by the program before anything of it is shown
async function recipePickFile(input) {
  const f = input.files && input.files[0]; if (!f) return;
  if (f.size > RCP_MAX) { toast("文件过大，不是有效的装配方案", 4500); return; }
  let text = "";
  try { text = await f.text(); } catch (e) { toast("无法读取该文件", 4000); return; }
  let r; try { r = await api("/api/recipe/import", { text, save: false }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!r.ok) { toast(r.err || "无法读取该文件", 6000); return; }
  RCP.imp = { text, recipe: r.recipe, needs: r.needs };
  if (recipeOpen()) recipeShow("import");
}
document.addEventListener("click", e => {
  const b = e.target.closest("[data-recipe]");
  if (b) recipeAction(b);
});
document.addEventListener("change", e => { if (e.target.id === "rcp_file") recipePickFile(e.target); });
document.addEventListener("input", e => { if (e.target.id === "rcp_name" || e.target.id === "rcp_note") recipeKeep(); });
// Enter in the name field saves, as the dialog's main button does
ENTER_GOES.rcp_name = '#modal .mfoot .btn.primary';
// the page may have been drawn before this file was read: its live parts are drawn again with what is here
if (typeof aiRenderLive === "function") aiRenderLive();
