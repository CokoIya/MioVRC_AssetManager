// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// 作者工具 (1.8.0): a miovrca.json for an asset of one's own — made from what its folder holds, edited here and
// written into the folder, to go into the package with it. The line reads it when a player puts the asset on.
"use strict";

// project, folder: what is chosen; folders: the project's asset folders (null: not read yet); d: the draft the
// program made (or the file there); m: what the form says (bases with their blendshapes as text, parts with their
// meshes as text); saved: where the file was written
const AU = { project: "", folders: null, folder: "", d: null, m: null, busy: "", err: "", saved: "" };
const AU_KIND_EN = { 素体: "base", 衣服: "outfit", 头发: "hair", 皮肤: "skin", 表情: "expression", 配饰: "accessory", 道具: "prop", 光影: "lighting", 互动: "gimmick", 面捕: "face-tracking" };

function authorOpenNow() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "author"; }
function authorSettingsRow() {
  return `<div class="row" id="s_author"><label>作者工具</label>
    <div class="btnrow" style="margin-top:0"><button class="btn small" data-author="open">为素材生成 miovrca.json…</button></div>
    </div>`;
}
async function authorOpen() {
  const ps = S.data.projects || [];
  const cur = AI.proj ? AI.proj.path : (typeof lastProject === "function" ? lastProject() : "");
  Object.assign(AU, { project: ps.some(p => p.path === cur) ? cur : (ps[0] || {}).path || "", folders: null, folder: "", d: null, m: null, busy: "", err: "", saved: "" });
  $("#modal").dataset.kind = "author";
  authorDraw(true); showModal();
  if (AU.project) await authorFolders();
}
async function authorFolders() {
  AU.folders = null; AU.folder = ""; AU.d = null; AU.m = null; AU.err = ""; AU.saved = "";
  authorDraw();
  let r;
  try { r = await api("/api/author/folders", { project: AU.project }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!authorOpenNow()) return;
  if (r.ok) AU.folders = r.folders || []; else { AU.folders = []; AU.err = r.err || "无法读取"; }
  authorDraw();
}
async function authorDraft() {
  AU.d = null; AU.m = null; AU.err = ""; AU.saved = "";
  if (!AU.folder) return authorDraw();
  AU.busy = "draft"; authorDraw();
  let r;
  try { r = await api("/api/author/draft", { project: AU.project, folder: AU.folder }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  AU.busy = "";
  if (!authorOpenNow()) return;
  if (!r.ok) { AU.err = r.err || "无法读取"; return authorDraw(); }
  AU.d = r.draft;
  const m = r.draft.manifest || {};
  AU.m = { name: m.name || "", kind: m.kind || "", author: m.author || "", url: m.url || "", notes: m.notes || "",
    bases: (m.bases || []).map(b => ({ name: b.name || "", prefab: b.prefab || "", quest: b.quest || "",
      shapes: Object.entries(b.shapes || {}).map(([k, v]) => `${k}=${v}`).join(", ") })),
    parts: (m.parts || []).map(p => ({ label: p.label || "", meshes: (p.meshes || []).join(", ") })) };
  authorDraw();
}
function authorPrefabOpts(sel, empty) {
  const d = AU.d, list = d ? d.prefabs || [] : [];
  return (empty ? `<option value="">${esc(empty)}</option>` : "") + list.map(p => `<option value="${esc(p)}"${p === sel ? " selected" : ""} data-i18n="off">${esc(p)}</option>`).join("")
    + (sel && !list.includes(sel) ? `<option value="${esc(sel)}" selected data-i18n="off">${esc(sel)}</option>` : "");
}
function authorFormHTML() {
  const d = AU.d, m = AU.m;
  const kinds = [`<option value="">${esc("未指定")}</option>`].concat(Object.keys(AU_KIND_EN).map(k => `<option value="${esc(k)}"${k === m.kind ? " selected" : ""}>${esc(k)}</option>`)).join("");
  const bases = m.bases.map((b, i) => `<tr><td><input data-au="bname" data-i="${i}" value="${esc(b.name)}" placeholder="Kaguya" spellcheck="false"></td>
      <td><select data-au="bprefab" data-i="${i}">${authorPrefabOpts(b.prefab, "请选择")}</select></td>
      <td><input data-au="bshapes" data-i="${i}" value="${esc(b.shapes)}" placeholder="Shrink_Chest=100" spellcheck="false"></td>
      <td><select data-au="bquest" data-i="${i}">${authorPrefabOpts(b.quest, "无")}</select></td>
      <td class="r"><button class="btn small ghost" data-author="delbase" data-i="${i}">删除</button></td></tr>`).join("");
  const parts = m.parts.map((p, i) => `<tr><td><input data-au="plabel" data-i="${i}" value="${esc(p.label)}" placeholder="外套" spellcheck="false"></td>
      <td><input data-au="pmeshes" data-i="${i}" value="${esc(p.meshes)}" placeholder="Jacket, Ribbon" spellcheck="false"></td>
      <td class="r"><button class="btn small ghost" data-author="delpart" data-i="${i}">删除</button></td></tr>`).join("");
  const first = m.bases[0] && (d.meshes || {})[m.bases[0].prefab];
  return `${d.err ? `<div class="small err">${esc(d.err)}</div>` : ""}
    <div class="small muted">${d.exists ? esc(`文件夹中已有 miovrca.json，下面是它的内容。`) : esc("下面是按文件夹内容生成的草稿，请核对后保存。")}${d.unity ? "" : `<span class="warnc">${esc("Unity 未打开，无法读取 prefab 的网格名：部件需要手动填写。")}</span>`}</div>
    <div class="augrid">
      <div class="row"><label for="au_name">名称</label><input id="au_name" data-au="name" value="${esc(m.name)}" spellcheck="false"></div>
      <div class="row"><label for="au_kind">类别</label><select id="au_kind" data-au="kind">${kinds}</select></div>
      <div class="row"><label for="au_author">作者</label><input id="au_author" data-au="author" value="${esc(m.author)}" spellcheck="false"></div>
      <div class="row"><label for="au_url">商品页（选填）</label><input id="au_url" data-au="url" value="${esc(m.url)}" placeholder="https://booth.pm/ja/items/…" spellcheck="false"></div>
    </div>
    <h5 class="lmh">各素体的版本</h5>
    <div class="small muted mcpsub">形态键格式：名称=数值，多个用逗号分隔。</div>
    ${bases ? `<table class="atable autable"><thead><tr><th>素体</th><th>prefab</th><th>穿着时的形态键</th><th>Quest 版</th><th></th></tr></thead><tbody>${bases}</tbody></table>` : ""}
    <div class="btnrow"><button class="btn small" data-author="addbase">添加素体版本</button></div>
    <h5 class="lmh">部件</h5>
    <div class="small muted mcpsub">每行一个部件，网格名用逗号分隔。</div>
    ${parts ? `<table class="atable autable"><thead><tr><th>名称</th><th>包含的网格</th><th></th></tr></thead><tbody>${parts}</tbody></table>` : ""}
    <div class="btnrow"><button class="btn small" data-author="addpart">添加部件</button></div>
    ${first && first.length ? `<div class="small muted mcpsub"><span>该 prefab 的网格：</span><span data-i18n="off">${esc(first.join("、"))}</span></div>` : ""}
    <div class="row"><label for="au_notes">备注（选填）</label><textarea id="au_notes" data-au="notes" maxlength="1000">${esc(m.notes)}</textarea></div>
    ${AU.saved ? `<div class="ausaved" role="status">${esc(`已保存到 ${AU.saved}。打包素材（unitypackage 或 zip）时请把它一起放进去。`)}</div>` : ""}`;
}
function authorDraw(fresh) {
  const m = $("#modal"); if (!fresh && !authorOpenNow()) return;
  const ps = S.data.projects || [];
  let body = `
    <div class="augrid"><div class="row"><label for="au_proj">所在工程</label><select id="au_proj">${ps.length ? "" : `<option value="">${esc("请先在「工程」页添加工程")}</option>`}${ps.map(p => `<option value="${esc(p.path)}"${p.path === AU.project ? " selected" : ""} data-i18n="off">${esc(p.name)}</option>`).join("")}</select></div>
    <div class="row"><label for="au_folder">素材所在文件夹</label><select id="au_folder"${AU.folders ? "" : " disabled"}>${AU.folders ? `<option value="">${esc("请选择")}</option>` + AU.folders.map(f => `<option value="${esc(f.folder)}"${f.folder === AU.folder ? " selected" : ""} data-i18n="off">${esc((f.has ? "✓ " : "") + f.name + " — " + f.folder)}</option>`).join("") : `<option>${esc("正在读取…")}</option>`}</select></div></div>`;
  if (AU.busy === "draft") body += `<div class="rmbusy"><i class="spin"></i>正在读取文件夹…</div>`;
  else if (AU.m) body += authorFormHTML();
  if (AU.err) body += `<div class="small err" role="status">${esc(AU.err)}</div>`;
  const can = !!AU.m && !AU.busy;
  const html = `<div class="mhead">作者工具：素材说明文件</div><div class="mbody mcpbody">${body}</div>
    <div class="mfoot"><button class="btn" data-author="copy"${can ? "" : " disabled"}>复制 JSON</button><span class="spacer"></span><button class="btn ghost" data-author="close">关闭</button><button class="btn primary" data-author="save"${can ? "" : " disabled"}>保存到素材文件夹</button></div>`;
  if (m._au === html && !fresh) return;
  const refocus = fresh ? () => {} : focusMark(m);
  const b = $(".mcpbody", m), top = b ? b.scrollTop : 0;
  m._au = html; m.innerHTML = html; refocus();
  if ($(".mcpbody", m)) $(".mcpbody", m).scrollTop = top;
}
// the form as a manifest; a line that cannot be read says so
function authorManifest() {
  const m = AU.m, out = { format: "miovrca-asset", schema: 1 };
  if (m.name.trim()) out.name = m.name.trim();
  if (m.kind) out.kind = AU_KIND_EN[m.kind] || m.kind;
  if (m.author.trim()) out.author = m.author.trim();
  if (m.url.trim()) out.url = m.url.trim();
  out.bases = [];
  for (const b of m.bases) {
    if (!b.name.trim() && !b.prefab) continue;
    const x = { name: b.name.trim(), prefab: b.prefab };
    const shapes = {};
    for (const part of b.shapes.split(/[,，、;；\n]+/)) {
      const t = part.trim(); if (!t) continue;
      const mm = t.match(/^(.+?)\s*[=:：]\s*(-?\d+(?:\.\d+)?)$/);
      if (mm) shapes[mm[1].trim()] = parseFloat(mm[2]);
      else if (/[=:：]/.test(t)) throw new Error(`形态键「${t}」的写法有误，应为 形态键名=数值`);
      else shapes[t] = 100;
    }
    if (Object.keys(shapes).length) x.shapes = shapes;
    if (b.quest) x.quest = b.quest;
    out.bases.push(x);
  }
  out.parts = m.parts.filter(p => p.label.trim() || p.meshes.trim()).map(p => ({ label: p.label.trim(), meshes: p.meshes.split(/[,，、;；\n]+/).map(s => s.trim()).filter(Boolean) }));
  if (m.notes.trim()) out.notes = m.notes.trim();
  if (!out.bases.length) delete out.bases;
  if (!out.parts.length) delete out.parts;
  return out;
}
async function authorDo(what, el) {
  if (what === "open") return authorOpen();
  if (what === "close") return closeModal();
  if (AU.busy || !AU.m) return;
  const i = +(el.dataset.i || 0);
  if (what === "addbase") { AU.m.bases.push({ name: "", prefab: "", quest: "", shapes: "" }); AU.saved = ""; return authorDraw(); }
  if (what === "addpart") { AU.m.parts.push({ label: "", meshes: "" }); AU.saved = ""; return authorDraw(); }
  if (what === "delbase") { AU.m.bases.splice(i, 1); AU.saved = ""; return authorDraw(); }
  if (what === "delpart") { AU.m.parts.splice(i, 1); AU.saved = ""; return authorDraw(); }
  let man;
  try { man = authorManifest(); } catch (e) { AU.err = e.message; return authorDraw(); }
  AU.err = "";
  if (what === "copy") { copyText(JSON.stringify(man, null, 2) + "\n", "已复制 JSON"); return authorDraw(); }
  if (what === "save") {
    AU.busy = "save"; AU.saved = ""; authorDraw();
    let r;
    try { r = await api("/api/author/save", { project: AU.project, folder: AU.folder, manifest: man }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
    AU.busy = "";
    if (!r.ok) { AU.err = r.err || "未能保存"; return authorDraw(); }
    AU.saved = r.file;
    const f = (AU.folders || []).find(x => x.folder === AU.folder); if (f) f.has = true;
    toast("已保存", 2000);
    return authorDraw();
  }
}
document.addEventListener("click", e => {
  const b = e.target.closest("button[data-author]");
  if (b && (authorOpenNow() || b.dataset.author === "open")) authorDo(b.dataset.author, b);
});
document.addEventListener("change", e => {
  if (!authorOpenNow()) return;
  if (e.target.id === "au_proj") { AU.project = e.target.value; return authorFolders(); }
  if (e.target.id === "au_folder") { AU.folder = e.target.value; return authorDraft(); }
  authorKeep(e.target);
});
document.addEventListener("input", e => { if (authorOpenNow()) authorKeep(e.target); });
// what is typed or picked, kept in the form's state (the dialog is drawn again only when rows come or go)
function authorKeep(t) {
  const k = t.dataset && t.dataset.au; if (!k || !AU.m) return;
  const i = +(t.dataset.i || 0), v = t.value;
  const set = { name: () => AU.m.name = v, kind: () => AU.m.kind = v, author: () => AU.m.author = v, url: () => AU.m.url = v, notes: () => AU.m.notes = v,
    bname: () => AU.m.bases[i].name = v, bprefab: () => AU.m.bases[i].prefab = v, bshapes: () => AU.m.bases[i].shapes = v, bquest: () => AU.m.bases[i].quest = v,
    plabel: () => AU.m.parts[i].label = v, pmeshes: () => AU.m.parts[i].meshes = v }[k];
  if (set) { set(); if (AU.saved) { AU.saved = ""; authorDraw(); } }
}
