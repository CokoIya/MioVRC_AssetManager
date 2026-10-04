// library tools: updates carried into projects, tidying, moving the library to another computer
"use strict";

const LT = {
  notes: new Map(), // asset key → what is newer for it (from /api/libtools/updates)
  jobs: [],         // updates under way and their results
  loaded: false,    // the notes have been read at least once
  busy: false, again: false, timer: 0,
  dlg: null,        // 「下载并更新到工程」: { key, picked: Set of project paths, recycle, job (id once started), ipick }
  tidy: { tab: "dup", st: null, sel: { dup: new Set(), arc: new Set() }, open: new Set(), minSize: 1 << 20, shown: { dup: 30, arc: 60 }, confirm: null, note: null, timer: 0 },
  move: null,       // 迁移: { step, ... } of the dialog that is open
};

// ---------- what is newer ----------
// (an asset stays under 「有更新」 while its update runs, so the card does not vanish from under the player)
function hasUpd(a) { return !!a.upd || !!a.updBusy; }
// the filters of this file: 「有更新」, and the assets no base body was found for (from 「适配对照」)
function ltMatch(a) {
  if (S.recent === "upd") return hasUpd(a);
  if (S.recent === "nobase") return !(a.bases || []).length;
  return true;
}
function ltRecentNav(nUpd) {
  return (nUpd || S.recent === "upd" ? navItem("有更新", nUpd, S.recent === "upd", `data-recent="upd"`, "#ffb547") : "") +
    (S.recent === "nobase" ? navItem("未识别适配素体", "", true, `data-recent="nobase"`, "#6c7088") : "");
}
function ltUpdTag(a) {
  const n = Array.isArray(a) ? (a.find(hasUpd) || {}).upd : a.upd;
  return n ? `<span class="updtag" title="${esc(ltNoteLines(n).map(l => l[0] + "：" + l[1]).join("\n"))}">有更新</span>` : "";
}
function ltOver(j) { return ["done", "failed", "cancelled"].includes(j.stage); }
function ltJobFor(a) { return LT.jobs.find(j => j.key === a.key); }
function ltSize(n) { return n ? fmtSize(n) : "0 B"; }
function ltNames(list, max) {
  const names = (list || []).map(p => String(p).split("/").filter(Boolean).slice(-2).join("/"));
  return names.slice(0, max).join("、") + (names.length > max ? " …" : "");
}
// what a note says, as label and value pairs
function ltNoteLines(n) {
  const out = [];
  if (n.version) { out.push(["新版本", n.version]); if (n.have) out.push(["本地版本", n.have]); }
  if ((n.added || []).length) out.push(["新增文件", ltNames(n.added, 6), n.added.length]);
  if ((n.replaced || []).length) out.push(["已替换文件", ltNames(n.replaced, 6), n.replaced.length]);
  if ((n.changed || []).length) out.push(["新增或已替换的文件", ltNames(n.changed, 6), n.changed.length]);
  if ((n.removed || []).length) out.push(["已移除文件", ltNames(n.removed, 4), n.removed.length]);
  if (n.at) out.push(["发现时间", fmtTime(n.at)]);
  return out;
}
function ltNoteTitle(n) { return n.source === "pan" ? "网盘分享中有新文件" : n.gumroad ? "Gumroad 已购文件有更新" : "Booth 已购文件有更新"; }
function ltKV(lines) {
  return `<div class="updkv">${lines.map(l => `<div class="r"><span class="k">${esc(l[0])}</span><span class="v">${esc(l[1])}${l[2] > 1 ? `<span class="n">${esc(l[2])}</span>` : ""}</span></div>`).join("")}</div>`;
}
const LT_PST = { waiting: "等待中", running: "正在更新", choose: "需要选择版本", done: "已更新", failed: "失败", skipped: "未导入", cancelled: "已取消" };
function ltProjRows(j) {
  return (j.projects || []).map(p => `<div class="updproj ${esc(p.status)}"><span class="pn" title="${esc(p.path)}">${esc(p.name)}</span>
    <span class="st ${esc(p.status)}">${esc(LT_PST[p.status] || p.status)}</span>
    ${p.status === "done" ? `<span class="num">unitypackage<b>${esc(p.pkgs)}</b></span><span class="num">文件<b>${esc(p.files)}</b></span>` : p.msg && p.msg !== LT_PST[p.status] ? `<span class="msg" title="${esc(p.msg)}">${esc(p.msg)}</span>` : ""}
    ${p.status === "done" && p.kept ? `<span class="msg" title="${esc((p.keptPkgs || []).join("、"))}">已安装的插件（Packages）未覆盖</span>` : ""}</div>`).join("");
}
// one update: its progress, or how it ended. inDlg: drawn in the dialog (the choice of versions is made there)
function ltJobHTML(j, inDlg) {
  const id = esc(j.id), rows = (j.projects || []).length ? `<div class="updprojs">${ltProjRows(j)}</div>` : "";
  if (!ltOver(j)) {
    const pct = j.stage === "download" && j.total ? Math.floor(j.done / j.total * 100) : 0;
    const login = j.login ? `<div class="small warnline">${esc(j.msg)}</div><div class="btnrow"><button class="btn small primary" data-lt="login" data-login="${esc(j.login)}">${j.login === "baidu" ? "登录百度网盘" : j.login === "gumroad" ? "登录 Gumroad" : "登录 Booth"}</button></div>` : "";
    const choose = (j.projects || []).some(p => p.status === "choose");
    return `<div class="updjob" data-job="${id}"><div class="impprog"><div class="t">${esc(j.login ? "下载已暂停" : j.msg || "正在准备…")}</div>
      ${j.stage === "import" ? "" : `<div class="bar wide"><i style="width:${pct}%"></i></div>${j.total ? `<div class="small muted">${fmtSize(j.done)} / ${fmtSize(j.total)}</div>` : ""}`}</div>${login}${rows}
      ${choose && inDlg ? ltChooseHTML() : ""}
      <div class="btnrow">${choose && !inDlg ? `<button class="btn small primary" data-lt="updshow" data-job="${id}">选择要导入的版本…</button>` : ""}
        ${inDlg ? "" : `<button class="btn small" data-lt="updshow" data-job="${id}">查看进度</button>`}<button class="btn small ghost" data-lt="updcancel" data-job="${id}">取消更新</button></div></div>`;
  }
  const failedN = (j.projects || []).filter(p => p.status === "failed").length, doneN = (j.projects || []).filter(p => p.status === "done").length;
  const recycled = (j.recycled || []).length;
  let head;
  if (j.stage === "failed") head = `<div>${esc(j.err || "更新失败")}</div>`;
  else if (j.stage === "cancelled") head = `<div>更新已取消</div>${(j.packages || []).length || j.folder ? `<div class="small">新版本已下载，未完成的工程未更新。</div>` : ""}`;
  else head = `<div>${failedN ? "新版本已下载，部分工程更新失败" : !(j.projects || []).length ? "新版本已下载" : doneN < j.projects.length ? "新版本已下载，部分工程未更新" : "新版本已下载，工程已更新"}</div>`;
  return `<div class="updjob" data-job="${id}"><div class="impres ${j.stage === "done" && !failedN ? "ok" : "err"}">${head}
      ${j.folder ? `<div class="small">${esc(j.folder)}</div>` : ""}
      ${doneN ? `<div class="small">若 Unity 已打开，切换到 Unity 窗口即可刷新；否则将在下次打开工程时生效。</div>` : ""}
      ${recycled ? `<div class="small">旧版本已移至回收站：${j.recycled.map(o => esc(o.path)).join("、")}</div>` : ""}
      ${j.recycleErr ? `<div class="small warnline">旧版本未移至回收站：${esc(j.recycleErr)}</div>` : ""}
      ${j.recycle && !recycled && !j.recycleErr && j.stage !== "failed" ? `<div class="small">有工程未更新成功，旧版本已保留。</div>` : ""}</div>${rows}
    <div class="btnrow">${j.folder ? `<button class="btn small" data-gopen="${esc(j.folder)}">打开文件夹</button>` : ""}
      ${failedN && (j.packages || []).length ? `<button class="btn small" data-lt="updretry" data-job="${id}">重试失败的工程</button>` : ""}
      ${inDlg ? "" : `<button class="btn small ghost" data-lt="upddismiss" data-job="${id}">关闭</button>`}</div></div>`;
}
// the packages an import asks about (several base bodies): ticked as the import suggests, until the player changes them
function ltChooseHTML() {
  const ij = S.data.importJob, d = LT.dlg || {};
  if (!ij || ij.stage !== "choose") return "";
  if (d.ipickFor !== ij.id) { d.ipickFor = ij.id; d.ipick = new Set((ij.choices || []).map((c, i) => c.pick ? i : -1).filter(i => i >= 0)); }
  return `<div class="impchoose updchoose"><div>该素材包含多个素体版本，请选择要导入的版本：</div><div class="num">目标工程<b>${esc(rootLabel(ij.project))}</b></div>
    ${(ij.choices || []).map((c, i) => `<label class="check"><input type="checkbox" data-ltipick="${i}"${d.ipick.has(i) ? " checked" : ""}><span class="n">${esc(c.name)}</span>${(c.bases || []).map(b => `<span class="base">${esc(b)}</span>`).join("")}<span class="s">${fmtSize(c.size)}</span></label>`).join("")}
    <div class="btnrow"><button class="btn small primary" data-lt="updpick">导入所选</button><button class="btn small ghost" data-lt="updskip">跳过该工程</button></div></div>`;
}
// the details panel: what is newer for this asset, how to get it, and the update that is running or has ended
function ltUpdSec(a) {
  const n = a.upd, j = ltJobFor(a);
  if (!n && !j) return "";
  const busy = j && !ltOver(j);
  let h = "";
  if (n) {
    h += `<div class="news"><div class="t">${esc(ltNoteTitle(n))}</div>${ltKV(ltNoteLines(n))}
      ${busy ? "" : `<div class="btnrow">${n.canDl ? `<button class="btn small primary" data-lt="upddl">${ICON.download}<span>下载新版</span></button><button class="btn small" data-lt="upddlg">${ICON.cube}<span>下载并更新到工程</span></button>`
        : `<button class="btn small" data-d="pan">打开网盘分享</button>`}<button class="btn small ghost" data-lt="updseen">标为已是最新</button></div>
      ${n.canDl ? "" : `<div class="hint2">该网盘分享为手动关联，无法自动下载，请在网盘中下载新文件。</div>`}`}</div>`;
  }
  if (j) h += ltJobHTML(j, false);
  return `<div class="sec upd" id="updsec"><h4>素材更新${n ? `<span class="updtag">有更新</span>` : ""}</h4>${h}</div>`;
}

// the notes go onto the cards' assets; true when a card reads differently now
function ltApply() {
  let changed = false;
  const busy = new Set(LT.jobs.filter(j => !ltOver(j)).map(j => j.key));
  for (const a of (S.data && S.data.assets) || []) {
    const n = LT.notes.get(a.key) || null, sig = (n ? JSON.stringify(n) : "") + (busy.has(a.key) ? "+" : "");
    if ((a._updSig || "") !== sig) { a.upd = n; a.updBusy = busy.has(a.key); a._updSig = sig; a._card = null; changed = true; }
    if (!LT.loaded || !isLocal(a)) continue;
    // for what is on disk the notes say what is newer: the earlier notices of the same thing give way
    if (a.newerOnBooth) { a.newerOnBooth = ""; a.newerDl = ""; a._card = null; changed = true; }
    if (a.panNews && (n || !(a.pan && a.pan.truncated))) { a.panNews = null; a._card = null; changed = true; }
  }
  return changed;
}
function ltOnLoad() { ltApply(); ltRefresh(); }
async function ltRefresh() {
  if (LT.busy) { LT.again = true; return; }
  LT.busy = true;
  try {
    const r = await api("/api/libtools/updates", {});
    if (r.ok) {
      const was = LT.notes, first = !LT.loaded;
      LT.notes = new Map(Object.entries(r.notes || {})); LT.loaded = true;
      const jobsWas = JSON.stringify(LT.jobs);
      LT.jobs = r.jobs || [];
      const fresh = first ? 0 : [...LT.notes.keys()].filter(k => !was.has(k)).length;
      const changed = ltApply();
      if (changed && S.view === "lib") { renderSide(); renderGrid(); }
      if (changed || first || jobsWas !== JSON.stringify(LT.jobs)) ltDrawLive();
      if (fresh) toast(`${fresh} 个素材有更新`, 7000, () => {
        if (S.view !== "lib") setView("lib");
        S.q = ""; $("#q").value = ""; S.cat = "全部"; S.style = ""; S.bases.clear(); S.usage = S.share = S.root = "all"; S.purchase = "all";
        S.recent = "upd"; renderSide(); renderGrid(); $(".main").scrollTop = 0;
      });
    }
  } catch (e) {} finally { LT.busy = false; }
  if (LT.again) { LT.again = false; return ltRefresh(); }
  clearTimeout(LT.timer);
  if (LT.jobs.some(j => !ltOver(j))) LT.timer = setTimeout(ltRefresh, 1000); // progress of an update under way
}
// the update of the open asset, in the details panel and in the dialog, without drawing the rest again
function ltDrawLive() {
  const a = S.openKey && findAsset(S.openKey);
  if (a) {
    const sec = $("#updsec"), html = ltUpdSec(a);
    if (sec && html) { if (sec._h !== html) { const re = focusMark(sec); sec.outerHTML = html; const now = $("#updsec"); if (now) { now._h = html; re(); } } }
    else if (!!sec !== !!html) renderDrawer(a.key, true);
  }
  if (ltModal("updrun")) ltDrawUpdRun();
}
// is one of this file's dialogs showing? (whatever opens over it replaces its content, and the marker with it)
function ltModal(kind) { const m = $("#modal"); return m.classList.contains("on") && !!m.querySelector(`[data-ltm="${kind}"]`); }
function ltShow(kind, head, body, foot, wide) {
  const m = $("#modal"), same = ltModal(kind), top = same ? ($(".mbody", m) || {}).scrollTop || 0 : 0;
  const html = `<div class="mhead">${head}</div><div class="mbody ltm${wide ? " ltwide" : ""}" data-ltm="${kind}">${body}</div>${foot ? `<div class="mfoot">${foot}</div>` : ""}`;
  if (same && m._lt === html) return;
  const refocus = same ? focusMark(m) : () => {};
  m._lt = html; m.innerHTML = html; m.dataset.kind = "lt";
  if (same) { $(".mbody", m).scrollTop = top; refocus(); } else showModal();
}

// ---------- 「下载并更新到工程」 ----------
function ltOpenUpdDlg(key) {
  const a = findAsset(key); if (!a || !a.upd) return;
  const used = new Set((a.usage || []).map(u => u.project));
  LT.dlg = { key, picked: new Set((S.data.projects || []).filter(p => used.has(p.name)).map(p => p.path)), recycle: false, job: null };
  ltDrawUpdDlg();
}
function ltDrawUpdDlg() {
  const d = LT.dlg, a = d && findAsset(d.key);
  if (!a || !a.upd) { if (ltModal("upd")) closeModal(); return; }
  const n = a.upd, ps = S.data.projects || [];
  const st = new Map((a.usage || []).map(u => [u.project, u.status]));
  const list = ps.slice().sort((x, y) => (st.has(y.name) - st.has(x.name)) || x.name.localeCompare(y.name, "zh-CN"));
  const old = n.old || [], oldSize = old.reduce((s, o) => s + (o.size || 0), 0);
  const body = `<div class="updhead"><b>${esc(a.name)}</b><span class="muted small">${esc(ltNoteTitle(n))}</span></div>${ltKV(ltNoteLines(n))}
    <h4 class="boardh">要更新的工程</h4>
    ${list.length ? `<div class="updpick">${list.map(p => `<label class="cand"><input type="checkbox" data-ltproj="${esc(p.path)}"${d.picked.has(p.path) ? " checked" : ""}>
      <span class="p"><b>${esc(p.name)}</b><span class="muted small">${esc(p.path)}</span></span>
      ${st.has(p.name) ? `<span class="tag ok">${st.get(p.name) === "used" ? "使用中" : "部分使用"}</span>` : ""}</label>`).join("")}</div>`
      : `<div class="muted small">尚未添加 Unity 工程，可在设置中添加工程文件夹。</div>`}
    <div class="hint2">已默认勾选正在使用该素材的工程。将依次下载新版本、解压并导入所选工程：工程中已有的文件原位更新，已安装的插件（Packages）不会被覆盖。未勾选工程时仅下载新版本。</div>
    ${old.length ? `<h4 class="boardh">旧版本</h4>
      <label class="check opt"><input type="checkbox" id="lt_recycle"${d.recycle ? " checked" : ""}> 全部更新成功后，将旧版本移至回收站</label>
      <div class="ltfiles">${old.map(o => `<div class="ltfile"><span class="p">${esc(o.path)}</span><span class="s">${fmtSize(o.size)}</span></div>`).join("")}</div>
      <div class="hint2">未勾选时保留旧版本。勾选后将移至回收站的内容如上所列，可释放空间：<b>${fmtSize(oldSize)}</b></div>`
      : n.source === "pan" ? `<div class="hint2">网盘分享中的新文件将下载到该素材原有的文件夹；同名的旧文件会被新文件替换，其余文件保持不变。</div>`
      : `<div class="hint2">新文件将下载到下载位置中该商品的文件夹，原有文件保持不变。</div>`}`;
  ltShow("upd", "下载并更新到工程", body, `<button class="btn ghost" data-m="close">取消</button><span class="spacer"></span><button class="btn primary" data-lt="updstart" id="lt_updgo">${d.picked.size ? "开始更新" : "仅下载新版"}</button>`);
}
async function ltStartUpdate(key, projects, recycle) {
  let r;
  try { r = await api("/api/libtools/update/start", { key, projects, recycle }); } catch (e) { r = { ok: false, err: e.message }; }
  if (!r.ok) { toast(r.err || "无法开始更新", 4500); return null; }
  S.dlAsked = Date.now(); // (the download it queues is this update's own: no second note about it)
  await ltRefresh(); poll(true);
  return r.job;
}
function ltDrawUpdRun() {
  const d = LT.dlg, j = d && LT.jobs.find(x => x.id === d.job);
  if (!j) { if (ltModal("updrun")) closeModal(); return; }
  const over = ltOver(j);
  ltShow("updrun", "下载并更新到工程", `<div class="updhead"><b>${esc(j.name)}</b></div>${ltJobHTML(j, true)}`,
    over ? `<span class="spacer"></span><button class="btn primary" data-m="close">关闭</button>`
      : `<span class="muted small">关闭此窗口后更新仍在后台进行，可在素材详情中查看。</span><span class="spacer"></span><button class="btn" data-m="close">后台运行</button>`);
}

// ---------- 整理 ----------
function ltBarBtn() { return `<button class="btn small ghost" id="btnTidy" title="查找重复文件和可清理的压缩包，查看占用空间和适配对照">整理</button>`; }
const LT_TABS = [["dup", "重复文件"], ["arc", "可清理的压缩包"], ["space", "占用空间"], ["compat", "适配对照"], ["cover", "封面"]];
async function ltOpenTidy(tab) {
  const t = LT.tidy;
  if (tab) t.tab = tab;
  t.confirm = null;
  ltDrawTidy();
  await ltTidyLoad();
  if (t.tab === "cover") pkBatchLoad();
}
async function ltTidyLoad() {
  const t = LT.tidy;
  let r;
  const was = t.st, seq = k => (was && was[k][k] ? was[k][k].seq : 0);
  try { r = await api("/api/libtools/tidy/status", { have: { dup: seq("dup"), arc: seq("arc") } }); } catch (e) { return; }
  if (!r.ok || t.st !== was) return;
  // (a result this window holds is not sent again)
  for (const k of ["dup", "arc"]) if ((r.same || {})[k]) r.tidy[k][k] = was[k][k];
  t.st = r.tidy; t.dlDir = r.dlDir;
  // what is ticked stays only while it is still listed
  for (const k of ["dup", "arc"]) {
    const have = new Set(k === "dup" ? ((t.st.dup.dup || {}).groups || []).flatMap(g => g.files.map(f => f.path)) : ((t.st.arc.arc || {}).items || []).map(i => i.main));
    for (const p of [...t.sel[k]]) if (!have.has(p)) t.sel[k].delete(p);
  }
  const running = t.st.dup.run.running || t.st.arc.run.running;
  const flip = !was || was.dup.run.running !== t.st.dup.run.running || was.arc.run.running !== t.st.arc.run.running || !!was.dup.dup !== !!t.st.dup.dup || !!was.arc.arc !== !!t.st.arc.arc;
  if (ltModal("tidy")) {
    if (flip) ltDrawTidy();
    else { const el = $("#lt_prog"), p = t.st[t.tab]; if (el && p && p.run.running) el.outerHTML = ltTidyProg(p.run); }
  }
  clearTimeout(t.timer);
  if (running && ltModal("tidy")) t.timer = setTimeout(ltTidyLoad, 700);
}
function ltTidyProg(run) {
  const pct = run.total ? Math.min(100, Math.floor(run.done / run.total * 100)) : 0;
  return `<div class="impprog" id="lt_prog"><div class="t">${esc(run.msg || "正在扫描…")}</div><div class="bar wide"><i style="width:${pct}%"></i></div></div>`;
}
function ltRel(p, root) {
  const sep = p.includes("\\") ? "\\" : "/";
  return root && p.toLowerCase().startsWith(root.toLowerCase()) ? rootLabel(root) + sep + p.slice(root.length).replace(/^[\\/]+/, "") : p;
}
function ltAssetLink(key, name) { return key ? `<button class="linkbtn ltasset" data-lt="goasset" data-key="${esc(key)}" title="查看素材详情">${esc(name || key)}</button>` : `<span class="muted">未归入素材</span>`; }
function ltWarn(list) { return (list || []).map(w => `<div class="small warnline">${esc(w)}</div>`).join(""); }
function ltScanBar(what, part, label, extra) {
  const run = part.run || {}, has = what === "dup" ? part.dup : part.arc;
  if (run.running) return `${ltTidyProg(run)}<div class="btnrow"><button class="btn small ghost" data-lt="tidycancel" data-what="${what}">取消扫描</button></div>`;
  return `<div class="ltscan"><button class="btn small${has ? "" : " primary"}" data-lt="tidyscan" data-what="${what}">${has ? "重新扫描" : esc(label)}</button>${extra || ""}
    ${has ? `<span class="num">上次扫描<b>${esc(fmtTime(has.at))}</b></span>` : ""}
    ${part.stale ? `<span class="small warnline">素材库在上次扫描后已有变化，请重新扫描。</span>` : ""}
    ${run.err ? `<span class="small err">${esc(run.err)}</span>` : run.msg === "已取消" ? `<span class="muted small">扫描已取消</span>` : ""}</div>`;
}
function ltDupWaste(g) { return g.size * (g.files.length - 1); }
// a group of very many copies shows its first few: the kept one, what is ticked, then the others
const LT_DUP_ROWS = 8;
function ltDupFirst(g) {
  if (g.files.length <= LT_DUP_ROWS) return g.files;
  const sel = LT.tidy.sel.dup, first = g.files.filter(f => f.keep || sel.has(f.path));
  return first.concat(g.files.filter(f => !first.includes(f))).slice(0, Math.max(LT_DUP_ROWS, first.length));
}
// the copies the suggestion would let go: every one but the kept one, where no other asset loses a file by it
function ltSuggest(g) { return g.cross || g.inner ? [] : g.files.filter(f => !f.keep).map(f => f.path); }
function ltDupTab() {
  const t = LT.tidy, part = t.st ? t.st.dup : { run: {} }, r = part.dup;
  const sizes = [[1 << 20, "1 MB 以上"], [100 << 10, "100 KB 以上"], [1, "全部文件"]];
  let h = ltScanBar("dup", part, "扫描重复文件", `<select id="lt_min" class="ltsel" title="参与比较的最小文件大小">${sizes.map(([v, l]) => `<option value="${v}"${t.minSize === v ? " selected" : ""}>${l}</option>`).join("")}</select>`);
  h += `<div class="hint2">在素材文件夹和下载位置中查找内容完全相同的文件：先比较大小，再比较文件片段，仅对仍然相同的文件读取全部内容。Unity 工程内的文件不参与比较。</div>
    <div class="hint2">标有「分属不同素材」或「同一素材内」的组，其中的每一份可能都被对应的素材需要，不会按建议勾选，请确认后手动勾选。</div>`;
  if (!r) return h;
  h += `<div class="ltsum"><span>重复文件组<b>${esc(r.total)}</b></span><span>可释放空间<b>${ltSize(r.waste)}</b></span><span>已检查文件<b>${esc(r.files)}</b></span></div>${ltWarn(r.warnings)}`;
  if (t.note) h += ltDoneNote(t.note);
  if (!r.groups.length) return h + `<div class="empty small"><h2>未发现重复文件</h2></div>`;
  const shown = r.groups.slice(0, t.shown.dup);
  h += `<div class="ltgroups">${shown.map((g, gi) => `<div class="ltgroup" data-g="${gi}">
    <div class="gh"><span class="num">单个大小<b>${fmtSize(g.size)}</b></span><span class="num">份数<b>${g.files.length}</b></span><span class="num">可释放<b>${fmtSize(ltDupWaste(g))}</b></span>
      ${g.cross ? `<span class="tag warn" title="这些文件分别属于不同的素材，移除后对应素材将缺少该文件">分属不同素材</span>`
        : g.inner ? `<span class="tag warn" title="这些文件位于同一素材的文件夹内，该素材可能需要其中的每一份">同一素材内</span>` : `<button class="linkbtn" data-lt="dupsuggest" data-g="${gi}">按建议勾选</button>`}</div>
    ${(t.open.has(gi) ? g.files : ltDupFirst(g)).map(f => `<label class="ltrow"><input type="checkbox" data-ltdup="${esc(f.path)}"${t.sel.dup.has(f.path) ? " checked" : ""}>
      <span class="p" title="${esc(f.path)}">${esc(ltRel(f.path, f.root))}</span>
      <span class="a">${ltAssetLink(f.asset, f.assetName)}</span>
      <span class="tags">${f.used ? `<span class="tag ok">使用中</span>` : ""}${f.noted ? `<span class="tag">有备注</span>` : ""}${f.keep ? `<span class="tag keep">建议保留</span>` : ""}</span>
      <button class="act" data-gopen="${esc(f.path)}" title="打开所在文件夹">${ICON.folder}</button></label>`).join("")}
    ${!t.open.has(gi) && g.files.length > LT_DUP_ROWS ? `<div class="gmore"><button class="linkbtn" data-lt="dupopen" data-g="${gi}">显示该组的全部文件</button></div>` : ""}</div>`).join("")}</div>`;
  if (r.groups.length > shown.length) h += `<div class="btnrow center"><button class="btn small" data-lt="tidymore" data-what="dup">显示更多</button><span class="num">已显示<b>${shown.length} / ${r.groups.length}</b></span></div>`;
  if (r.total > r.groups.length) h += `<div class="muted small">重复文件组较多，仅列出可释放空间最多的部分。</div>`;
  return h;
}
function ltArcTab() {
  const t = LT.tidy, part = t.st ? t.st.arc : { run: {} }, r = part.arc;
  let h = ltScanBar("arc", part, "扫描压缩包");
  h += `<div class="hint2">查找内容已解压到旁边或素材文件夹中的压缩包。仅列出已核对的压缩包：按文件名和大小，将压缩包内的文件与已解压的文件逐一比对。分卷压缩包按一项处理。</div>`;
  if (!r) return h;
  h += `<div class="ltsum"><span>可清理的压缩包<b>${esc(r.items.length)}</b></span><span>可释放空间<b>${ltSize(r.free)}</b></span><span>已检查压缩包<b>${esc(r.archives)}</b></span>${r.unreadable ? `<span>无法读取<b>${esc(r.unreadable)}</b></span>` : ""}</div>${ltWarn(r.warnings)}`;
  if (r.unreadable) h += `<div class="small muted">无法读取内容的压缩包未列出：rar、7z 和分卷压缩包需安装 7-Zip，加密压缩包无法核对。</div>`;
  if (t.note) h += ltDoneNote(t.note);
  if (!r.items.length) return h + `<div class="empty small"><h2>未发现可清理的压缩包</h2></div>`;
  const shown = r.items.slice(0, t.shown.arc);
  h += `<div class="ltgroups">${shown.map(it => `<label class="ltrow arc"><input type="checkbox" data-ltarc="${esc(it.main)}"${t.sel.arc.has(it.main) ? " checked" : ""}>
      <span class="p" title="${esc(it.parts.map(p => p.path).join("\n"))}">${esc(ltRel(it.main, it.root))}${it.parts.length > 1 ? `<span class="tag">分卷<b>${it.parts.length}</b></span>` : ""}
        <span class="sub" title="${esc(it.folder)}"><span>已解压到</span><span class="to">${esc(ltRel(it.folder, it.root))}</span><span>已核对文件</span><b${it.checked < it.entries ? ` title="压缩包内另有 ${esc(it.entries - it.checked)} 个由系统或压缩软件附带的文件（__MACOSX、Thumbs.db 等），不参与核对"` : ""}>${esc(it.checked)} / ${esc(it.entries)}</b></span></span>
      <span class="a">${ltAssetLink(it.asset, it.assetName)}</span><span class="s">${fmtSize(it.size)}</span>
      <button class="act" data-gopen="${esc(it.main)}" title="打开所在文件夹">${ICON.folder}</button></label>`).join("")}</div>`;
  if (r.items.length > shown.length) h += `<div class="btnrow center"><button class="btn small" data-lt="tidymore" data-what="arc">显示更多</button></div>`;
  return h;
}
function ltDoneNote(n) {
  return `<div class="impres ${n.failed.length ? "err" : "ok"}"><div>已移至回收站</div><div class="ltsum inres"><span>文件<b>${esc(n.removed)}</b></span><span>已释放空间<b>${ltSize(n.freed)}</b></span></div>
    ${n.failed.length ? `<div class="small warnline">以下文件未移动：</div>${n.failed.slice(0, 8).map(f => `<div class="small">${esc(f.path)}：${esc(f.err)}</div>`).join("")}` : ""}</div>`;
}
// the assets a table counts: what the library shows with no filter set
function ltCounted() { return (S.data.assets || []).filter(a => !a.splitInto && !a.virtual && (S.showHidden || !a.hidden)); }
function ltSpaceTab() {
  const list = ltCounted().filter(isLocal);
  const total = a => (a.locations || []).reduce((s, l) => s + (l.size || 0), 0);
  const roots = new Map(), cats = new Map();
  for (const a of list) {
    for (const l of a.locations || []) roots.set(l.root, (roots.get(l.root) || 0) + (l.size || 0));
    const c = cats.get(a.category) || { size: 0, units: new Set() }; c.size += total(a); c.units.add(unitKey(a)); cats.set(a.category, c);
  }
  const all = [...roots.values()].reduce((s, v) => s + v, 0);
  const bars = (rows, max) => rows.map(r => `<div class="ltbar"><span class="k" title="${esc(r.title || r.label)}">${r.dot ? `<i style="background:${r.dot}"></i>` : ""}${esc(r.label)}</span>
    <span class="b"><i style="width:${max ? Math.max(1, Math.round(r.size / max * 100)) : 0}%"></i></span><span class="s">${fmtSize(r.size)}</span>${r.n !== undefined ? `<span class="n">${esc(r.n)} 个</span>` : ""}</div>`).join("");
  const rr = (S.data.settings.roots || []).filter(r => roots.has(r)).map(r => ({ label: rootLabel(r), title: r, size: roots.get(r) })).sort((x, y) => y.size - x.size);
  const cr = [...cats.entries()].map(([c, v]) => ({ label: c, size: v.size, n: v.units.size, dot: CAT_COLOR[c] || "#8b8fa6" })).sort((x, y) => y.size - x.size);
  const top = list.map(a => ({ a, size: total(a) })).sort((x, y) => y.size - x.size).slice(0, 50);
  return `<div class="ltsum"><span>素材库占用空间<b>${ltSize(all)}</b></span><span>素材<b>${countUnits(list)}</b></span></div>
    <div class="hint2">按素材库中记录的大小统计，重新扫描后更新。</div>
    <h4 class="boardh">按文件夹</h4><div class="ltbars">${bars(rr, rr.length ? rr[0].size : 0) || `<div class="muted small">暂无素材</div>`}</div>
    <h4 class="boardh">按分类</h4><div class="ltbars">${bars(cr, cr.length ? cr[0].size : 0)}</div>
    <h4 class="boardh">占用空间最大的素材</h4>
    <div class="lttop">${top.map((x, i) => `<div class="ltrow rank"><span class="i">${i + 1}</span><span class="p">${ltAssetLink(x.a.key, x.a.name)}</span>
      <span class="a"><span class="cat"><i style="background:${CAT_COLOR[x.a.category] || "#8b8fa6"}"></i>${esc(x.a.category)}</span></span><span class="s">${fmtSize(x.size)}</span></div>`).join("")}</div>`;
}
function ltCompatTab() {
  const list = ltCounted();
  const cell = new Map(), byBase = new Map(), byCat = new Map(), none = new Map();
  const add = (m, k, a) => { let s = m.get(k); if (!s) m.set(k, s = new Set()); s.add(unitKey(a)); };
  for (const a of list) {
    const bs = a.bases || [];
    if (!bs.length) { add(none, a.category, a); add(none, "", a); continue; }
    for (const b of bs) { add(cell, b + "\n" + a.category, a); add(byBase, b, a); }
    add(byCat, a.category, a);
  }
  const cats = (S.data.categories || []).filter(c => byCat.has(c) || none.has(c));
  const bases = [...byBase.keys()].sort((x, y) => byBase.get(y).size - byBase.get(x).size || x.localeCompare(y));
  const n = s => s ? s.size : 0;
  const td = (count, attrs) => count ? `<td><button class="ltcell" ${attrs}>${count}</button></td>` : `<td class="zero">·</td>`;
  if (!bases.length && !n(none.get(""))) return `<div class="empty small"><h2>暂无素材</h2></div>`;
  return `<div class="hint2">各素体在每个分类下的素材数量，来自素材的「适配素体」信息。点击数字可在素材库中查看对应的素材。</div>
    <div class="lttablewrap"><table class="lttable"><thead><tr><th>适配素体</th>${cats.map(c => `<th><i style="background:${CAT_COLOR[c] || "#8b8fa6"}"></i>${esc(c)}</th>`).join("")}<th>合计</th></tr></thead>
    <tbody>${bases.map(b => `<tr><th title="${esc(b)}">${esc(b)}</th>${cats.map(c => td(n(cell.get(b + "\n" + c)), `data-lt="compat" data-base="${esc(b)}" data-cat="${esc(c)}"`)).join("")}${td(n(byBase.get(b)), `data-lt="compat" data-base="${esc(b)}" data-cat=""`)}</tr>`).join("")}
    <tr class="none"><th>未识别适配素体</th>${cats.map(c => td(n(none.get(c)), `data-lt="compat" data-base="" data-cat="${esc(c)}"`)).join("")}${td(n(none.get("")), `data-lt="compat" data-base="" data-cat=""`)}</tr></tbody></table></div>
    <div class="ltsum"><span>未识别适配素体的素材<b>${n(none.get(""))}</b></span></div>
    <div class="muted small">插件、音效等通用素材通常不区分素体。</div>`;
}
function ltSelInfo(what) {
  const t = LT.tidy, sel = t.sel[what]; let size = 0, files = 0;
  if (what === "dup") for (const g of ((t.st && t.st.dup.dup) || { groups: [] }).groups) for (const f of g.files) if (sel.has(f.path)) { size += g.size; files++; }
  if (what === "arc") for (const it of ((t.st && t.st.arc.arc) || { items: [] }).items) if (sel.has(it.main)) { size += it.size; files += it.parts.length; }
  return { n: sel.size, size, files };
}
function ltTidyFoot() {
  const t = LT.tidy, what = t.tab;
  if (t.confirm) return `<button class="btn ghost" data-lt="tidyback">返回</button><span class="spacer"></span><button class="btn primary" data-lt="tidygo" id="lt_tidygo"${t.busy ? " disabled" : ""}>${t.busy ? "正在移动…" : "确认移至回收站"}</button>`;
  if (what !== "dup" && what !== "arc") return `<span class="spacer"></span><button class="btn primary" data-m="close">关闭</button>`;
  const i = ltSelInfo(what), has = what === "dup" ? t.st && t.st.dup.dup && t.st.dup.dup.groups.length : t.st && t.st.arc.arc && t.st.arc.arc.items.length;
  return `<span class="ltselinfo" id="lt_selinfo"><span>已选文件<b>${i.files}</b></span><span>可释放空间<b>${ltSize(i.size)}</b></span></span>
    ${has ? (what === "dup" ? `<button class="btn small ghost" data-lt="dupsuggestall" title="在可按建议处理的各组中，勾选除建议保留以外的文件">按建议勾选</button>` : `<button class="btn small ghost" data-lt="arcall">全选</button>`) : ""}
    ${i.n ? `<button class="btn small ghost" data-lt="tidyclear">清除勾选</button>` : ""}
    <span class="spacer"></span><button class="btn ghost" data-m="close">关闭</button><button class="btn primary" data-lt="tidyask"${i.n ? "" : " disabled"}>移至回收站…</button>`;
}
function ltConfirmBody() {
  const t = LT.tidy, c = t.confirm;
  return `<p class="lead">以下文件将移至回收站。</p>
    <div class="ltsum"><span>文件<b>${c.files.length}</b></span><span>可释放空间<b>${fmtSize(c.size)}</b></span></div>
    <div class="ltfiles tall">${c.files.map(f => `<div class="ltfile"><span class="p">${esc(f.path)}</span><span class="s">${fmtSize(f.size)}</span></div>`).join("")}</div>
    <div class="hint2">文件不会被永久删除，可在 Windows 回收站中还原。扫描后发生变化的文件不会被移动。</div>`;
}
function ltDrawTidy() {
  const t = LT.tidy;
  const tabs = `<div class="seg lttabs">${LT_TABS.map(([k, l]) => `<button class="segbtn${t.tab === k ? " on" : ""}" data-lt="tidytab" data-tab="${k}"${t.confirm ? " disabled" : ""}>${l}</button>`).join("")}</div>`;
  const body = t.confirm ? ltConfirmBody() : !t.st && (t.tab === "dup" || t.tab === "arc") ? `<div class="muted small">正在加载…</div>`
    : t.tab === "dup" ? ltDupTab() : t.tab === "arc" ? ltArcTab() : t.tab === "space" ? ltSpaceTab() : t.tab === "cover" ? pkTab() : ltCompatTab();
  ltShow("tidy", "整理", tabs + `<div class="lttab" data-tab="${t.confirm ? "confirm" : t.tab}">${body}</div>`, ltTidyFoot(), true);
}
function ltTidyFootDraw() { const f = $("#modal .mfoot"); if (f && ltModal("tidy")) { f.innerHTML = ltTidyFoot(); $("#modal")._lt = null; } }
function ltGoAsset(key) {
  closeModal();
  if (S.view !== "lib") setView("lib");
  if (findAsset(key)) renderDrawer(key); else toast("未找到该素材，请重新扫描后重试", 3500);
}

// ---------- 迁移 ----------
function ltSettingsRow() {
  return `<div class="row" id="s_move"><label>迁移到另一台电脑</label>
    <div class="btnrow" style="margin-top:0"><button class="btn small" data-lt="exportopen">导出素材库…</button><button class="btn small" data-lt="importopen">导入素材库…</button><button class="btn small ghost" data-lt="moveopen">备份与待映射文件夹</button></div>
    <div class="muted small" style="margin-top:6px">将素材库数据（素材列表、备注、标签、封面、已购和网盘记录等）导出为一个 zip 文件，在另一台电脑上导入。素材文件本身不包含在内，需自行复制。</div></div>
    <div class="row" id="s_ae"><label>Avatar Explorer</label>
    <div class="btnrow" style="margin-top:0"><button class="btn small" data-lt="aeopen">导入素材信息…</button></div>
    <div class="muted small" style="margin-top:6px">使用过 Avatar Explorer 的玩家，可将其中记录的素材信息（Booth 商品、分类、适配素体、标签、备注、封面）补充到本素材库。扫描时会自动跳过 Avatar Explorer 的数据库和备份，只收录其中的素材文件夹。</div></div>
    <div class="row" id="s_ka"><label>KonoAsset</label>
    <div class="btnrow" style="margin-top:0"><button class="btn small" data-lt="kaopen">导入素材信息…</button></div>
    <div class="muted small" style="margin-top:6px">使用过 KonoAsset 的玩家，可将其中记录的素材信息（Booth 商品、分类、适配素体、标签、备注、封面）补充到本素材库。扫描时会自动跳过 KonoAsset 的数据库和图片，只收录其中的素材文件夹。</div></div>`;
}
// ---------- another asset manager's library (Avatar Explorer, KonoAsset) ----------
// (one dialog for both: what it says of the manager, by LT.ae.src)
const LT_MGR = {
  ae: { api: "ae", title: "导入 Avatar Explorer 的素材信息", lead: "将 Avatar Explorer 中记录的素材信息补充到本素材库。只填写此处尚未设置的内容，不会修改 Avatar Explorer 的数据。",
    finding: "正在查找 Avatar Explorer 的数据文件夹…", none: "未在素材文件夹和默认位置找到 Avatar Explorer 的数据。", which: "请选择 Avatar Explorer 的程序文件夹（旧版，包含 Datas）或数据文件夹（V2，包含 database）。",
    folder: "Avatar Explorer 的文件夹", choose: "选择 Avatar Explorer 的文件夹", addRoot: "将 Avatar Explorer 的素材文件夹加入素材库" },
  ka: { api: "ka", title: "导入 KonoAsset 的素材信息", lead: "将 KonoAsset 中记录的素材信息补充到本素材库。只填写此处尚未设置的内容，不会修改 KonoAsset 的数据。",
    finding: "正在查找 KonoAsset 的数据文件夹…", none: "未在素材文件夹和默认位置找到 KonoAsset 的数据。", which: "请选择 KonoAsset 的数据文件夹（包含 metadata 和 data）。",
    folder: "KonoAsset 的文件夹", choose: "选择 KonoAsset 的文件夹", addRoot: "将 KonoAsset 的素材文件夹加入素材库" },
};
async function ltOpenAE(src) {
  LT.ae = { src: src || "ae", found: null, dir: "", pv: null, err: "", busy: false, res: null };
  const ae = LT.ae;
  ltDrawAE();
  try { const r = await api(`/api/libtools/${LT_MGR[ae.src].api}/find`, {}); if (LT.ae === ae) { ae.found = r.found || []; if (ae.found.length === 1) { ae.pv = ae.found[0]; ae.dir = ae.pv.dir; } } } catch (e) { if (LT.ae === ae) ae.found = []; }
  if (LT.ae === ae && ltModal("ae")) ltDrawAE();
}
async function ltAEPick(dir) {
  const ae = LT.ae; if (!ae || !dir) return;
  ae.dir = dir; ae.pv = null; ae.err = ""; ae.res = null;
  let r; try { r = await api(`/api/libtools/${LT_MGR[ae.src].api}/preview`, { dir }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (LT.ae !== ae) return;
  if (r.ok) ae.pv = r.preview; else ae.err = r.err || "无法读取该文件夹";
  ltDrawAE();
}
function ltDrawAE() {
  const ae = LT.ae; if (!ae) return;
  const m = LT_MGR[ae.src];
  let body = `<p class="lead">${m.lead}</p>`, foot = "";
  if (ae.res) {
    const r = ae.res;
    body += `<div class="impres ok"><div>导入完成</div>
      <div class="ltsum inres"><span>补充信息的素材<b>${esc(r.matched)}</b></span><span>关联 Booth 商品<b>${esc(r.booth)}</b></span><span>分类<b>${esc(r.category)}</b></span><span>适配素体<b>${esc(r.bases)}</b></span><span>标签<b>${esc(r.tags)}</b></span><span>备注<b>${esc(r.notes)}</b></span><span>封面<b>${esc(r.covers)}</b></span></div>
      ${r.addRoot ? `<div class="small">已将以下文件夹加入素材库，正在扫描；扫描完成后会自动补充其中素材的信息。</div><div class="small" data-i18n="off">${esc(r.addRoot)}</div>` : ""}
      ${r.skipped && !r.addRoot ? `<div class="ltsum inres"><span>尚未收录、未作处理的素材<b>${esc(r.skipped)}</b></span></div>` : ""}</div>`;
    foot = `<span class="spacer"></span><button class="btn primary" data-m="close">完成</button>`;
  } else {
    if (ae.found === null) body += `<div class="muted">${m.finding}</div>`;
    else if (ae.found.length > 1) body += `<div class="row"><label>已找到的数据文件夹</label>${ae.found.map(f => `<label class="check"><input type="radio" name="lt_ae" data-ltae="${esc(f.dir)}"${f.dir === ae.dir ? " checked" : ""}><span data-i18n="off">${esc(f.dir)}</span></label>`).join("")}</div>`;
    else if (!ae.found.length && !ae.dir) body += `<div class="ltnote"><div>${m.none}</div><div>${m.which}</div></div>`;
    body += `<div class="row"><label>数据文件夹</label><div class="inline"><input id="lt_ae_dir" value="${esc(ae.dir)}" placeholder="${m.folder}" autocomplete="off" spellcheck="false"><button class="btn small" data-lt="aepick">选择…</button><button class="btn small" data-lt="aeread">读取</button></div></div>`;
    if (ae.err) body += `<div class="impres err">${esc(ae.err)}</div>`;
    const pv = ae.pv;
    if (pv) {
      body += `<div class="ltsum">${pv.version ? `<span>版本<b>${pv.version === 2 ? "V2" : "V1"}</b></span>` : ""}<span>记录的素材<b>${esc(pv.items)}</b></span><span>已在素材库中<b>${esc(pv.matched)}</b></span><span>不在素材文件夹内<b>${esc(pv.outside)}</b></span><span>文件夹已不存在<b>${esc(pv.missing)}</b></span></div>`;
      if (pv.addRoot) body += `<label class="check"><input type="checkbox" id="lt_ae_root" checked>${m.addRoot}</label><div class="muted small" data-i18n="off" style="margin:2px 0 0 24px">${esc(pv.addRoot)}</div><div class="muted small" style="margin:2px 0 0 24px">加入后会扫描该文件夹，并自动补充其中素材的信息。尚未收录的素材：<b>${esc(pv.inFolder)}</b></div>`;
      else if (pv.outside) body += `<div class="muted small">不在素材文件夹内的素材不会处理；如需收录，请先在「设置」中添加其所在的文件夹。</div>`;
    }
    foot = `<button class="btn ghost" data-m="close">取消</button><span class="spacer"></span><button class="btn primary" data-lt="aego"${pv && !ae.busy && (pv.matched || pv.addRoot) ? "" : " disabled"}>${ae.busy ? "正在导入…" : "导入"}</button>`;
  }
  ltShow("ae", m.title, body, foot);
}

// ---------- covers from the previews inside unitypackages ----------
// PK.st: what the server last said about an asset's cover from its packages (by key); busy: an extraction under
// way; batch: the run over the library (「整理」→「封面」), and when it was last asked for
const PK = { st: {}, busy: {}, batch: null, batchAt: 0 };
// an asset whose packages could give it a picture: it shows none, or one taken from them already
function pkWants(a) { return !!a && isLocal(a) && !a.psd && !!(a.packages || a.archives) && (!a.cover || !!a.coverPkg); }
// the line in the asset's details panel, under 「从 Unity 生成封面」 (the two read as one group; the label is
// drawn by the first of them that shows)
function pkFieldHTML(a) {
  if (!pkWants(a)) return "";
  const s = PK.st[a.key], busy = !!PK.busy[a.key], has = !!a.coverPkg;
  const entry = has && s && s.cover && s.cover.entry ? s.cover.entry.split("/").pop() : "";
  const hint = has ? `当前封面来自 unitypackage 中的预览图${entry ? `（${esc(entry)}）` : ""}。关联 Booth 商品、选择图片或从 Unity 生成封面后，将改用其封面。`
    : s && s.busy ? "正在后台读取该素材的 unitypackage…" : s && s.failed ? `上次未在 unitypackage 中找到可用的预览图${s.failed === "unitypackage 中没有可用的预览图" ? "" : `（${esc(s.failed)}）`}。` : "读取 unitypackage 内由 Unity 生成的预览图，作为封面。";
  return `<div class="field pkcov">${covFieldHTML(a) ? `<span></span>` : `<label>封面</label>`}<div class="covgen">
    <button class="btn small" data-lt="pkextract"${busy || (s && s.busy) ? " disabled" : ""} title="读取 unitypackage 内由 Unity 生成的预览图（prefab 的渲染图、贴图的缩略图），作为封面">${busy ? "正在提取…" : has ? "重新提取封面" : "从 unitypackage 提取封面"}</button>
    ${has && !busy ? `<button class="btn small ghost" data-lt="pkremove">移除</button>` : ""}
    <span class="hint">${hint}</span></div></div>`;
}
// The line is swapped in place when only it changed (its status arrived, a run began), and the panel is told what
// it reads now: the panel is not drawn again, so what is being typed in it is not touched (like covRedraw).
function pkRedraw(key, change) {
  const a = S.openKey === key ? findAsset(key) : null, was = a ? pkFieldHTML(a) : "";
  change();
  if (!a) return;
  const dr = $("#drawer"), el = $(".pkcov", dr), now = pkFieldHTML(a);
  if (now === was) return;
  if (!el || !was || !now || !dr._h || !dr._h.includes(was)) return renderDrawer(key, true);
  const had = el.contains(document.activeElement), box = document.createElement("div");
  box.innerHTML = now; dr._h = dr._h.replace(was, () => now);
  el.replaceWith(box.firstElementChild);
  if (had) { const b = $('.pkcov [data-lt="pkextract"]', dr); if (b && !b.disabled) b.focus({ preventScroll: true }); }
}
async function pkStatus(key) {
  let r; try { r = await api("/api/libtools/pkgcover/status", { key }); } catch (e) { return; }
  if (!r.ok) return;
  PK.batch = r.batch || null; PK.batchAt = Date.now();
  pkRedraw(key, () => { PK.st[key] = Object.assign({}, r.status, { at: Date.now() }); });
}
async function pkExtract(key, remove) {
  if (PK.busy[key]) return;
  pkRedraw(key, () => { PK.busy[key] = true; });
  let r; try { r = await api(remove ? "/api/libtools/pkgcover/remove" : "/api/libtools/pkgcover/extract", { key }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  delete PK.busy[key];
  if (r.status) PK.st[key] = Object.assign({}, r.status, { at: Date.now() });
  toast(r.ok ? (remove ? "已移除来自 unitypackage 的封面" : "封面已提取") : r.err || "未能提取封面", r.ok ? 2200 : 6000);
  await load();
  if (S.openKey === key) renderDrawer(key, true);
}
// 「整理」→「封面」: the run over every asset that shows no picture
function pkTab() {
  const list = ltCounted().filter(isLocal), none = list.filter(a => !a.cover), can = none.filter(pkWants), from = list.filter(a => a.coverPkg);
  const b = PK.batch;
  return `<div class="hint2">unitypackage 内带有 Unity 生成的预览图（prefab 的渲染图、贴图的缩略图）。没有封面的素材会在每次扫描后自动从中提取封面；此处可立即为全部素材提取，并重试此前未找到预览图的素材。</div>
    <div class="hint2">来自 unitypackage 的封面排在最后：关联 Booth 商品、选择图片或从 Unity 生成封面后，将改用其封面。在素材详情中可以移除它，或重新提取。</div>
    <div class="ltsum"><span>没有封面的素材<b>${esc(none.length)}</b></span><span>其中带 unitypackage 的<b>${esc(can.length)}</b></span><span>封面来自 unitypackage<b>${esc(from.length)}</b></span></div>
    <div id="lt_pkprog">${pkProgHTML()}</div>
    <div class="btnrow"><button class="btn small primary" data-lt="pkbatch"${(b && b.running) || !can.length ? " disabled" : ""}>为没有封面的素材提取封面</button>${!can.length ? `<span class="muted small">没有封面的素材中，没有可读取的 unitypackage。</span>` : ""}</div>`;
}
function pkProgHTML() {
  const b = PK.batch; if (!b) return "";
  if (b.running) {
    const pct = b.total ? Math.round(b.done / b.total * 100) : 0;
    return `<div class="covprog"><span>正在提取封面</span><span class="bar wide"><i style="width:${pct}%"></i></span><span class="cnt">${esc(b.done)} / ${esc(b.total)}</span><span class="nm" title="${esc(b.now || "")}">${esc(b.now || "")}</span><button class="btn small ghost" data-lt="pkcancel">取消</button></div>`;
  }
  if (!b.ended || Date.now() / 1000 - b.ended > 600) return "";
  return `<div class="covprog done"><span class="${b.made ? "okline" : "muted"}"><span>已提取封面：</span><b>${esc(b.made)}</b></span>${b.failed ? `<span class="warnline"><span>未找到预览图：</span><b>${esc(b.failed)}</b></span>` : ""}${
    b.err ? `<span class="err">${esc(b.err)}</span>` : ""}${b.last ? `<span class="muted small last" title="${esc(b.last)}">${esc(b.last)}</span>` : ""}</div>`;
}
function pkTabOpen() { return ltModal("tidy") && LT.tidy.tab === "cover" && !LT.tidy.confirm; }
async function pkBatchLoad() {
  let r; try { r = await api("/api/libtools/pkgcover/status", { key: "" }); } catch (e) { return; }
  if (!r.ok) return;
  const was = PK.batch || {};
  PK.batch = r.batch || null; PK.batchAt = Date.now();
  if (!pkTabOpen()) return;
  const b = PK.batch || {};
  // the whole tab when the run began or ended (the counts and the button change), its progress line otherwise
  if (!!was.running !== !!b.running || (was.ended || 0) !== (b.ended || 0)) ltDrawTidy();
  else { const el = $("#lt_pkprog"); if (el && put(el, pkProgHTML())) $("#modal")._lt = null; }
}
async function pkBatchStart() {
  const r = await api("/api/libtools/pkgcover/batch", {});
  if (!r.ok) { toast(r.err || "无法开始", 5000); return; }
  PK.batch = r.batch; PK.batchAt = Date.now();
  if (pkTabOpen()) ltDrawTidy();
}
// Once a second: the details panel's line and the 「封面」 tab are brought up to date. Nothing is asked of the
// server unless one of them is on the screen.
setInterval(() => {
  if (!S.data || document.hidden) return;
  const a = S.openKey && $("#drawer").classList.contains("on") ? findAsset(S.openKey) : null;
  if (a && pkWants(a) && !PK.busy[a.key] && (!PK.st[a.key] || Date.now() - PK.st[a.key].at > 6000)) pkStatus(a.key);
  if (pkTabOpen() && Date.now() - PK.batchAt > (PK.batch && PK.batch.running ? 900 : 5000)) pkBatchLoad();
}, 1000);
// the first-run screen: a library moved from another computer is taken in instead of starting a new one
function ltSetupBtn() { return `<button class="btn ghost" data-lt="setupimport" title="导入在另一台电脑上导出的素材库">导入素材库…</button><span class="spacer"></span>`; }
const LT_SECRET = "Booth、Gumroad、百度网盘的登录状态和 AI 服务的 API Key 已针对本机的 Windows 账户加密，无法迁移，不会导出；在另一台电脑上需重新登录并重新填写 API Key。AI 服务的地址与模型、代理、下载位置和工程体检记录属于本机设置，同样不会导出，导入时也不会改动本机的这些设置。";
async function ltMoveState() { try { const r = await api("/api/libtools/transfer/state", {}); return r.ok ? r.transfer : null; } catch (e) { return null; } }
function ltOpenExport() { LT.move = { step: "export", dir: "", run: null }; ltDrawMove(); }
function ltOpenImport() { LT.move = { step: "pick", path: "", info: null, mode: "merge", busy: false }; ltDrawMove(); const el = $("#lt_imp_path"); if (el) el.focus(); }
async function ltOpenPending() { LT.move = { step: "state", state: null }; ltDrawMove(); LT.move.state = await ltMoveState(); if (LT.move && LT.move.step === "state") ltDrawMove(); }
function ltRunHTML(run) {
  const pct = run.total ? Math.floor(run.done / run.total * 100) : 0;
  return `<div class="impprog"><div class="t">${esc(run.msg || "正在准备…")}</div><div class="bar wide"><i style="width:${pct}%"></i></div></div>`;
}
function ltMapRows(list, what) {
  return list.map((r, i) => `<div class="ltmap"><div class="old"><span class="p" title="${esc(r.path)}">${esc(r.path)}</span>
      <span class="num">${what === "root" ? "素材" : "工程"}<b>${esc(r.assets)}</b></span>${r.exists ? `<span class="tag ok">本机存在相同路径</span>` : ""}</div>
    <div class="inline"><input data-ltmap="${what}" data-i="${i}" value="${esc(r.to !== undefined ? r.to : r.suggest || "")}" placeholder="${what === "root" ? "留空则暂不映射，该文件夹的素材将等待映射" : "留空则不导入该文件夹下的工程"}" aria-label="${esc(r.path)} 在本机的位置">
      <button class="btn small" data-lt="mappick" data-what="${what}" data-i="${i}">${ICON.folder}<span>选择…</span></button></div></div>`).join("");
}
function ltDrawMove() {
  const mv = LT.move; if (!mv) return;
  let head = "导入素材库", body = "", foot = "";
  if (mv.step === "export") {
    head = "导出素材库";
    const run = mv.run;
    body = `<p class="lead">将素材库数据导出为一个 zip 文件，用于迁移到另一台电脑。</p>
      <div class="ltnote"><div><b>包含</b>素材列表、备注、标签和分类调整，Booth 封面，已购记录和网盘分享记录，以及配方、愿望单等数据文件。</div>
        <div><b>不包含</b>素材文件本身、Unity 工程、缩略图缓存和日志。</div><div>${LT_SECRET}</div></div>
      ${run && run.running ? ltRunHTML(run) : run && run.path ? `<div class="impres ok"><div>导出完成</div><div class="small">${esc(run.path)}</div>
          <div class="ltsum inres"><span>素材<b>${esc(run.assets)}</b></span><span>数据文件<b>${esc(run.files)}</b></span><span>文件大小<b>${fmtSize(run.size)}</b></span></div>
          <div class="btnrow"><button class="btn small" data-gopen="${esc(run.path)}">打开所在文件夹</button></div></div>`
        : `${run && run.err ? `<div class="impres err">${esc(run.err)}</div>` : ""}<div class="row"><label>保存到</label>
          <div class="inline"><input id="lt_exp_dir" value="${esc(mv.dir)}" placeholder="选择保存导出文件的文件夹"><button class="btn small" data-lt="exppick">${ICON.folder}<span>选择…</span></button></div></div>`}`;
    foot = `<span class="spacer"></span><button class="btn ghost" data-m="close">${run && run.path ? "关闭" : "取消"}</button>${run && (run.running || run.path) ? "" : `<button class="btn primary" data-lt="expgo" id="lt_expgo">导出</button>`}`;
  } else if (mv.step === "pick") {
    body = `<p class="lead">选择在另一台电脑上导出的素材库文件（zip）。</p>
      <div class="row"><label>导出文件</label><div class="inline"><input id="lt_imp_path" value="${esc(mv.path)}" placeholder="例如 D:\\MioVRCA-library-20260101-120000.zip"><button class="btn small" data-lt="imppick">${ICON.folder}<span>选择文件…</span></button></div></div>
      ${mv.err ? `<div class="impres err">${esc(mv.err)}</div>` : ""}
      <div class="hint2">导入前会自动备份当前素材库，导入后可以撤销。</div>`;
    foot = `<span class="spacer"></span>${mv.fromSetup ? `<button class="btn ghost" data-lt="setupback">返回</button>` : `<button class="btn ghost" data-m="close">取消</button>`}<button class="btn primary" data-lt="impnext" id="lt_impnext"${mv.busy ? " disabled" : ""}>${mv.busy ? "正在读取…" : "下一步"}</button>`;
  } else if (mv.step === "map") {
    const info = mv.info;
    body = `<div class="ltsum"><span>素材<b>${esc(info.assets)}</b></span><span>备注与设置<b>${esc(info.notes)}</b></span><span>已购记录<b>${esc(info.purchases)}</b></span><span>网盘分享<b>${esc(info.shares)}</b></span><span>数据文件<b>${esc(info.dataFiles)}</b></span></div>
      <div class="ltsum"><span>导出时间<b>${esc(fmtTime(info.exported))}</b></span><span>软件版本<b>${esc(info.version)}</b></span><span>文件大小<b>${fmtSize(info.size)}</b></span></div>
      ${info.kind === "import-backup" ? `<div class="small warnline">这是导入前自动生成的备份文件。</div>` : ""}
      <h4 class="boardh">素材文件夹在本机的位置</h4>
      ${info.roots.length ? ltMapRows(info.roots, "root") : `<div class="muted small">导出文件中没有素材文件夹。</div>`}
      <div class="hint2">请为导出文件中的每个素材文件夹指定本机上存放相同内容的文件夹；素材的备注、标签、封面和使用情况会随之对应到新位置。暂未复制过来的文件夹可以留空，稍后在「备份与待映射文件夹」中指定。</div>
      ${info.projectRoots.length ? `<h4 class="boardh">Unity 工程文件夹在本机的位置（选填）</h4>${ltMapRows(info.projectRoots, "proj")}` : ""}
      <h4 class="boardh">导入方式</h4>
      <label class="cand"><input type="radio" name="lt_mode" value="merge"${mv.mode === "merge" ? " checked" : ""}><span class="p"><b>合并</b><span class="muted small">保留当前素材库，加入导出文件中的内容。同一素材的备注以当前素材库为准，为空时采用导出文件中的内容；标签合并。</span></span></label>
      <label class="cand"><input type="radio" name="lt_mode" value="replace"${mv.mode === "replace" ? " checked" : ""}><span class="p"><b>替换</b><span class="muted small">以导出文件中的内容替换当前素材库和数据文件。</span></span></label>
      <div class="ltnote"><div>导入前会自动备份当前素材库（数据文件夹中带日期的 zip 文件），导入后可以撤销。</div><div>${LT_SECRET}</div></div>
      ${mv.err ? `<div class="impres err">${esc(mv.err)}</div>` : ""}`;
    foot = `<button class="btn ghost" data-lt="impback"${mv.busy ? " disabled" : ""}>返回上一步</button><span class="spacer"></span>${mv.fromSetup ? "" : `<button class="btn ghost" data-m="close">取消</button>`}<button class="btn primary" data-lt="impgo"${mv.busy ? " disabled" : ""}>${mv.busy ? "正在导入…" : "开始导入"}</button>`;
  } else if (mv.step === "done") {
    const r = mv.result;
    head = "导入完成";
    body = `<div class="impres ok"><div>${r.mode === "replace" ? "已用导出文件中的内容替换当前素材库" : "已将导出文件中的内容合并到当前素材库"}，正在重新扫描素材文件夹。</div>
        <div class="ltsum inres"><span>已导入素材<b>${esc(r.assets)}</b></span><span>等待映射的素材<b>${esc(r.waiting)}</b></span><span>备注与设置<b>${esc(r.notes)}</b></span><span>数据文件<b>${esc(r.dataFiles)}</b></span>${r.keptFiles ? `<span>本机已有、保持不变的数据文件<b>${esc(r.keptFiles)}</b></span>` : ""}</div></div>
      ${(r.pending || []).length ? `<div class="ltnote"><div><b>以下文件夹尚未映射</b></div><div>其中的素材将在指定本机位置后加入素材库。</div>${r.pending.map(p => `<div class="small">${esc(p)}</div>`).join("")}</div>` : ""}
      <div class="ltnote"><div>导入前的素材库已备份至：</div><div class="small">${esc(r.backup)}</div><div>如需恢复到导入前的状态，可点击「撤销本次导入」。</div><div>${LT_SECRET}</div></div>`;
    foot = `<button class="btn ghost" data-lt="impundo">撤销本次导入</button><span class="spacer"></span>${(r.pending || []).length ? `<button class="btn" data-lt="moveopen">指定待映射的文件夹</button>` : ""}<button class="btn primary" data-m="close">完成</button>`;
  } else if (mv.step === "state") {
    head = "备份与待映射文件夹";
    const s = mv.state;
    body = !s ? `<div class="muted small">正在加载…</div>` : `<h4 class="boardh first">等待映射的文件夹</h4>
      ${s.pending.length ? s.pending.map((p, i) => `<div class="ltmap"><div class="old"><span class="p" title="${esc(p.root)}">${esc(p.root)}</span><span class="num">素材<b>${esc(p.assets)}</b></span></div>
          <div class="inline"><input data-ltpend="${i}" value="${esc((mv.to || {})[p.root] || "")}" placeholder="该文件夹在本机的位置" aria-label="${esc(p.root)} 在本机的位置"><button class="btn small" data-lt="pendpick" data-i="${i}">${ICON.folder}<span>选择…</span></button>
          <button class="btn small primary" data-lt="pendmap" data-i="${i}">映射</button><button class="btn small ghost" data-lt="penddrop" data-i="${i}">放弃</button></div></div>`).join("")
        : `<div class="muted small">没有等待映射的文件夹。</div>`}
      <h4 class="boardh">导入前的备份</h4>
      ${s.undo ? `<div class="ltnote"><div>上次导入前的素材库已备份，可恢复到导入前的状态。</div>${s.from ? `<div class="small muted">导入的文件：${esc(s.from)}</div>` : ""}<div class="btnrow"><button class="btn small" data-lt="impundo">撤销上次导入</button></div></div>` : ""}
      ${s.backups.length ? `<div class="ltfiles">${s.backups.map(b => `<div class="ltfile"><span class="p">${esc(b.path)}</span><span class="s">${esc(fmtTime(b.at))}</span><span class="s">${fmtSize(b.size)}</span></div>`).join("")}</div>
        <div class="btnrow"><button class="btn small" data-gopen="${esc(s.backups[0].path)}">打开所在文件夹</button></div><div class="hint2">备份文件不会自动删除，不再需要时可自行删除。</div>` : `<div class="muted small">暂无备份。</div>`}`;
    foot = `<span class="spacer"></span><button class="btn primary" data-m="close">关闭</button>`;
  }
  ltShow("move", head, body, foot);
}
// what is typed in the mapping fields is kept on the rows, so drawing the dialog again does not lose it
function ltKeepMap() {
  const mv = LT.move; if (!mv) return;
  document.querySelectorAll("#modal [data-ltmap]").forEach(el => { const l = el.dataset.ltmap === "root" ? mv.info.roots : mv.info.projectRoots; if (l[+el.dataset.i]) l[+el.dataset.i].to = el.value.trim(); });
  document.querySelectorAll("#modal [data-ltpend]").forEach(el => { const p = mv.state && mv.state.pending[+el.dataset.ltpend]; if (p) (mv.to = mv.to || {})[p.root] = el.value.trim(); });
  const ep = $("#lt_exp_dir"); if (ep) mv.dir = ep.value.trim();
  const ip = $("#lt_imp_path"); if (ip) mv.path = ip.value.trim();
  const mode = $("#modal input[name=lt_mode]:checked"); if (mode) mv.mode = mode.value;
}
async function ltExportWatch() {
  const mv = LT.move;
  for (;;) {
    const s = await ltMoveState();
    if (!LT.move || LT.move !== mv || !s) return;
    mv.run = s.run;
    if (ltModal("move")) ltDrawMove();
    if (!s.run.running) return;
    await new Promise(r => setTimeout(r, 500));
  }
}
async function ltImportNext() {
  const mv = LT.move; ltKeepMap();
  if (!mv.path) { toast("请先选择导出文件"); return; }
  mv.busy = true; mv.err = ""; ltDrawMove();
  let r;
  try { r = await api("/api/libtools/import/inspect", { path: mv.path }); } catch (e) { r = { ok: false, err: e.message }; }
  if (LT.move !== mv) return;
  mv.busy = false;
  if (!r.ok) { mv.err = r.err || "无法读取导出文件"; ltDrawMove(); return; }
  mv.info = r.info; mv.step = "map"; mv.err = "";
  ltDrawMove();
}
async function ltImportGo() {
  const mv = LT.move; ltKeepMap();
  const roots = {}, projects = {};
  for (const r of mv.info.roots) roots[r.path] = r.to !== undefined ? r.to : r.suggest || "";
  for (const r of mv.info.projectRoots) projects[r.path] = r.to !== undefined ? r.to : r.suggest || "";
  const unmapped = mv.info.roots.filter(r => !roots[r.path]).length;
  if (mv.mode === "replace" && !confirm("用导出文件中的内容替换当前素材库？\n\n当前素材库会先备份，导入后可以撤销。")) return;
  if (unmapped && !confirm("有素材文件夹尚未指定本机位置，其中的素材将等待映射，暂不出现在素材库中。是否继续？")) return;
  mv.busy = true; mv.err = ""; ltDrawMove();
  let r;
  try { r = await api("/api/libtools/import/apply", { path: mv.path, roots, projects, mode: mv.mode }); } catch (e) { r = { ok: false, err: e.message }; }
  if (LT.move !== mv) return;
  mv.busy = false;
  if (!r.ok) { mv.err = r.err || "导入失败"; ltDrawMove(); return; }
  if (mv.fromSetup) S.setup = null; // there is a library now
  mv.step = "done"; mv.result = r.result;
  ltDrawMove(); await load();
  // the language picked on the first-run screen is this computer's: an import does not bring one
  if (window.I18N && I18N.fresh && (S.data.settings.lang || "") !== I18N.lang) { try { await api("/api/settings", { settings: Object.assign({}, S.data.settings, { lang: I18N.lang }), noRescan: true }); } catch (e) {} }
  poll(true);
}

// ---------- events ----------
Object.assign(ENTER_GOES, { lt_exp_dir: '#modal [data-lt="expgo"]', lt_imp_path: '#modal [data-lt="impnext"]', lt_ae_dir: '#modal [data-lt="aeread"]' });
document.addEventListener("change", e => { const r = e.target.closest && e.target.closest("[data-ltae]"); if (r && r.checked) ltAEPick(r.dataset.ltae); });
document.addEventListener("click", async e => {
  const t = e.target;
  if (t.closest("#btnTidy")) return ltOpenTidy();
  const b = t.closest("[data-lt]");
  // a file or folder shown in one of this file's dialogs: opened where it is
  const go = !b && t.closest("#modal .ltm [data-gopen], #modal .mfoot [data-gopen]");
  if (go && $("#modal .ltm")) { e.preventDefault(); return openPath(go.dataset.gopen); }
  if (!b) return;
  const act = b.dataset.lt, a = S.openKey && findAsset(S.openKey);
  switch (act) {
    // updates
    case "upddl": if (a) { b.disabled = true; await ltStartUpdate(a.key, [], false); } return;
    case "upddlg": if (a) ltOpenUpdDlg(a.key); return;
    case "updseen": if (a) { await api("/api/libtools/update/seen", { key: a.key }); toast("已标为最新"); await ltRefresh(); await load(); } return;
    case "updstart": {
      const d = LT.dlg; if (!d) return;
      b.disabled = true;
      const j = await ltStartUpdate(d.key, [...d.picked], d.recycle);
      if (!j) { b.disabled = false; return; }
      d.job = j.id; ltDrawUpdRun(); return;
    }
    case "updshow": { LT.dlg = Object.assign(LT.dlg || {}, { job: b.dataset.job }); ltDrawUpdRun(); return; }
    case "updcancel": await api("/api/libtools/update/cancel", { id: b.dataset.job }); toast("正在取消"); return ltRefresh();
    case "upddismiss": await api("/api/libtools/update/dismiss", { id: b.dataset.job }); if (ltModal("updrun")) closeModal(); await ltRefresh(); return load();
    case "updretry": {
      const r = await api("/api/libtools/update/retry", { id: b.dataset.job });
      if (!r.ok) { toast(r.err || "无法重试", 4000); return; }
      if (LT.dlg) LT.dlg.job = r.job.id;
      await ltRefresh(); poll(true); return;
    }
    case "updpick": {
      const cs = (S.data.importJob || {}).choices || [], d = LT.dlg || {};
      const paths = [...(d.ipick || [])].map(i => (cs[i] || {}).path).filter(Boolean);
      if (!paths.length) { toast("请至少选择一项"); return; }
      await api("/api/import/choose", { paths }); return poll(true);
    }
    case "updskip": await api("/api/import/dismiss", {}); return poll(true);
    case "login": closeModal(); if (b.dataset.login === "baidu") return baiduLogin(false); if (b.dataset.login === "gumroad") return gumLogin(); return startSync();
    // tidying
    case "goasset": return ltGoAsset(b.dataset.key);
    case "tidytab": LT.tidy.tab = b.dataset.tab; LT.tidy.note = null; ltDrawTidy(); $("#modal .mbody").scrollTop = 0; if (LT.tidy.tab === "cover") pkBatchLoad(); return ltTidyLoad();
    // covers from unitypackages
    case "pkextract": if (a) pkExtract(a.key, false); return;
    case "pkremove": if (a) pkExtract(a.key, true); return;
    case "pkbatch": return pkBatchStart();
    case "pkcancel": await api("/api/libtools/pkgcover/batch/cancel", {}); toast("正在取消"); return pkBatchLoad();
    case "tidyscan": {
      const what = b.dataset.what, t = LT.tidy;
      const ms = $("#lt_min"); if (ms) t.minSize = +ms.value;
      t.note = null; t.sel[what].clear(); t.open.clear(); t.shown = { dup: 30, arc: 60 };
      const r = await api("/api/libtools/tidy/scan", { what, minSize: t.minSize });
      if (!r.ok) toast(r.err || "无法开始扫描", 3500);
      return ltTidyLoad();
    }
    case "tidycancel": await api("/api/libtools/tidy/cancel", { what: b.dataset.what }); return ltTidyLoad();
    case "dupopen": LT.tidy.open.add(+b.dataset.g); return ltDrawTidy();
    case "tidymore": LT.tidy.shown[b.dataset.what] += b.dataset.what === "dup" ? 30 : 60; return ltDrawTidy();
    case "dupsuggest": case "dupsuggestall": {
      const t = LT.tidy, gs = t.st.dup.dup.groups, list = act === "dupsuggest" ? [gs[+b.dataset.g]] : gs;
      let n = 0;
      for (const g of list) { for (const f of g.files) t.sel.dup.delete(f.path); for (const p of ltSuggest(g)) { t.sel.dup.add(p); n++; } }
      if (!n) toast("没有可按建议勾选的文件，其余各组请手动勾选", 4000);
      return ltDrawTidy();
    }
    case "arcall": for (const it of LT.tidy.st.arc.arc.items) LT.tidy.sel.arc.add(it.main); return ltDrawTidy();
    case "tidyclear": LT.tidy.sel[LT.tidy.tab].clear(); return ltDrawTidy();
    case "tidyask": {
      const t = LT.tidy, what = t.tab, files = [];
      if (what === "dup") for (const g of t.st.dup.dup.groups) for (const f of g.files) if (t.sel.dup.has(f.path)) files.push({ path: f.path, size: g.size });
      if (what === "arc") for (const it of t.st.arc.arc.items) if (t.sel.arc.has(it.main)) for (const p of it.parts) files.push({ path: p.path, size: p.size });
      if (!files.length) return;
      t.confirm = { what, files, size: files.reduce((s, f) => s + f.size, 0), picks: [...t.sel[what]], seq: (what === "dup" ? t.st.dup.dup : t.st.arc.arc).seq };
      ltDrawTidy(); $("#modal .mbody").scrollTop = 0; return;
    }
    case "tidyback": LT.tidy.confirm = null; return ltDrawTidy();
    case "tidygo": {
      const t = LT.tidy, c = t.confirm; if (!c || t.busy) return;
      t.busy = true; ltTidyFootDraw();
      let r;
      try { r = await api("/api/libtools/tidy/recycle", { what: c.what, paths: c.picks, seq: c.seq }); } catch (e2) { r = { ok: false, err: e2.message }; }
      t.busy = false; t.confirm = null;
      if (!r.ok) { toast(r.err || "无法移至回收站", 5000); ltDrawTidy(); return ltTidyLoad(); }
      t.sel[c.what].clear();
      t.note = { removed: r.result.removed, freed: r.result.freed, failed: r.result.failed || [] };
      t.st = null; ltDrawTidy(); await ltTidyLoad(); ltDrawTidy(); poll(true); return;
    }
    case "compat": {
      const base = b.dataset.base, cat = b.dataset.cat;
      closeModal();
      if (S.view !== "lib") setView("lib");
      if (S.web.open.lib) closeWeb();
      S.q = ""; $("#q").value = ""; S.style = ""; S.usage = S.share = S.root = "all"; S.purchase = "all";
      S.cat = cat || "全部"; S.bases.clear(); S.recent = "";
      if (base) S.bases.add(base); else S.recent = "nobase";
      renderSide(); renderGrid(); $(".main").scrollTop = 0; return;
    }
    // moving the library
    case "aeopen": closeModal(); return ltOpenAE("ae");
    case "kaopen": closeModal(); return ltOpenAE("ka");
    case "aepick": { const p = LT.ae && await pickInto(LT_MGR[LT.ae.src].choose); if (p) await ltAEPick(p); return; }
    case "aeread": { const el = $("#lt_ae_dir"); if (el && el.value.trim()) await ltAEPick(el.value.trim()); return; }
    case "aego": {
      const ae = LT.ae; if (!ae || !ae.pv || ae.busy) return;
      const root = $("#lt_ae_root");
      ae.busy = true; ltDrawAE();
      try {
        const r = await api(`/api/libtools/${LT_MGR[ae.src].api}/apply`, { dir: ae.pv.dir, addRoot: !!(root && root.checked) });
        if (r.ok) ae.res = r.result; else ae.err = r.err || "导入失败";
      } catch (e) { ae.err = "程序无响应"; } finally { ae.busy = false; }
      ltDrawAE(); if (ae.res) load();
      return;
    }
    case "exportopen": closeModal(); return ltOpenExport();
    case "importopen": closeModal(); return ltOpenImport();
    // (the first-run screen is not over until a library is there: Esc does not close the import either)
    case "setupimport": ltOpenImport(); LT.move.fromSetup = true; return ltDrawMove();
    case "setupback": LT.move = null; return openSetup();
    case "moveopen": closeModal(); return ltOpenPending();
    case "exppick": { const p = await pickInto("选择保存导出文件的文件夹"); if (p && LT.move) { LT.move.dir = p; const el = $("#lt_exp_dir"); if (el) el.value = p; } return; }
    case "expgo": {
      const mv = LT.move; ltKeepMap();
      if (!mv.dir) { toast("请先选择保存导出文件的文件夹"); return; }
      const r = await api("/api/libtools/export", { dir: mv.dir });
      if (!r.ok) { mv.run = { err: r.err || "无法开始导出" }; return ltDrawMove(); }
      mv.run = { running: true, msg: "正在准备…" }; ltDrawMove(); return ltExportWatch();
    }
    case "imppick": {
      const r = await api("/api/libtools/pickfile", {});
      if (r.ok && LT.move) { LT.move.path = r.path; const el = $("#lt_imp_path"); if (el) el.value = r.path; }
      else if (!r.cancelled) toast(r.err || "无法打开文件选择窗口，请直接粘贴文件路径", 4500);
      return;
    }
    case "impnext": return ltImportNext();
    case "impback": ltKeepMap(); LT.move.step = "pick"; LT.move.err = ""; return ltDrawMove();
    case "impgo": return ltImportGo();
    case "mappick": {
      const p = await pickInto(b.dataset.what === "root" ? "选择该素材文件夹在本机的位置" : "选择该工程文件夹在本机的位置");
      if (p) { const el = document.querySelector(`#modal [data-ltmap="${b.dataset.what}"][data-i="${b.dataset.i}"]`); if (el) el.value = p; }
      return;
    }
    case "impundo": {
      if (!confirm("撤销上次导入？素材库将恢复到导入前的状态，导入后所做的修改不会保留。")) return;
      const r = await api("/api/libtools/import/undo", {});
      if (!r.ok) { toast(r.err || "无法撤销", 5000); return; }
      closeModal(); toast("已恢复到导入前的素材库", 4000); await load(); poll(true);
      if (S.data.setupNeeded && !S.setup) openSetup(); // undone on a computer that had no library yet
      return;
    }
    case "pendpick": { const p = await pickInto("选择该文件夹在本机的位置"); if (p) { const el = document.querySelector(`#modal [data-ltpend="${b.dataset.i}"]`); if (el) el.value = p; } return; }
    case "pendmap": case "penddrop": {
      const mv = LT.move; ltKeepMap();
      const p = mv.state.pending[+b.dataset.i]; if (!p) return;
      let r;
      if (act === "pendmap") {
        const to = (mv.to || {})[p.root];
        if (!to) { toast("请先指定该文件夹在本机的位置"); return; }
        r = await api("/api/libtools/import/maproot", { root: p.root, to });
        if (r.ok) toast(`已加入 ${r.assets} 个素材，正在重新扫描`, 4000);
      } else {
        if (!confirm(`放弃「${p.root}」中等待映射的 ${p.assets} 个素材？这些素材的记录将不再保留（导入前的备份和导出文件不受影响）。`)) return;
        r = await api("/api/libtools/import/droproot", { root: p.root });
      }
      if (!r.ok) { toast(r.err || "操作失败", 4500); return; }
      mv.state = await ltMoveState(); ltDrawMove(); await load(); poll(true); return;
    }
  }
});
// Enter in a waiting folder's field maps that folder
document.addEventListener("keydown", e => {
  const ds = e.target.dataset || {};
  if (e.key !== "Enter" || e.isComposing || e.keyCode === 229 || ds.ltpend === undefined) return;
  e.preventDefault();
  const b = document.querySelector(`#modal [data-lt="pendmap"][data-i="${CSS.escape(ds.ltpend)}"]`); if (b) b.click();
});
document.addEventListener("change", e => {
  const t = e.target, ds = t.dataset || {};
  if (ds.ltproj !== undefined && LT.dlg) {
    t.checked ? LT.dlg.picked.add(ds.ltproj) : LT.dlg.picked.delete(ds.ltproj);
    const go = $("#lt_updgo"); if (go) go.textContent = LT.dlg.picked.size ? "开始更新" : "仅下载新版";
    $("#modal")._lt = null; return;
  }
  if (t.id === "lt_recycle" && LT.dlg) { LT.dlg.recycle = t.checked; $("#modal")._lt = null; return; }
  if (ds.ltipick !== undefined && LT.dlg && LT.dlg.ipick) { t.checked ? LT.dlg.ipick.add(+ds.ltipick) : LT.dlg.ipick.delete(+ds.ltipick); $("#modal")._lt = null; return; }
  if (t.id === "lt_min") { LT.tidy.minSize = +t.value; return; }
  if (ds.ltdup !== undefined) {
    const tt = LT.tidy, sel = tt.sel.dup, g = tt.st.dup.dup.groups.find(x => x.files.some(f => f.path === ds.ltdup));
    if (t.checked && g && g.files.every(f => f.path === ds.ltdup || sel.has(f.path))) { t.checked = false; toast("每组相同的文件至少需保留一份", 3500); return; }
    t.checked ? sel.add(ds.ltdup) : sel.delete(ds.ltdup);
    return ltTidyFootDraw();
  }
  if (ds.ltarc !== undefined) { t.checked ? LT.tidy.sel.arc.add(ds.ltarc) : LT.tidy.sel.arc.delete(ds.ltarc); return ltTidyFootDraw(); }
  if (t.name === "lt_mode" && LT.move) { LT.move.mode = t.value; $("#modal")._lt = null; }
});
