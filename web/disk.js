// The player's own Baidu netdisk: a folder (or a file) of it downloaded into the library — for what the player
// saved there from a share with Baidu's own app or page (where a captcha is Baidu's to ask and the player's to
// read). The dialog lists the netdisk's folders, and the downloads it started with their progress; the jobs
// themselves are the netdisk downloads' (key "pandisk:" and the path). Nothing in the netdisk is changed.
"use strict";

const DK = { dir: "/", list: null, err: "", login: false, loading: false, seq: 0 };
function diskJobs() { return ((S.data && S.data.panJobs) || []).filter(j => j.key.startsWith("pandisk:")); }
function diskOpen() { const m = $("#modal"); return m.classList.contains("on") && m.dataset.kind === "pandisk"; }

async function openDisk(dir) {
  const m = $("#modal");
  m.dataset.kind = "pandisk";
  if (dir) DK.dir = dir;
  diskDraw(); showModal();
  if ((S.data.baidu || {}).loggedIn) await diskLoad(DK.dir);
}
async function diskLoad(dir) {
  const seq = ++DK.seq;
  DK.loading = true; DK.err = ""; DK.login = false;
  diskDraw();
  let r;
  try { r = await api("/api/pan/disk/list", { dir }); } catch (e) { r = { ok: false, err: "程序内部错误" }; }
  if (seq !== DK.seq) return;
  DK.loading = false;
  if (r.ok) { DK.dir = r.dir; DK.list = r.entries || []; }
  else { DK.list = null; DK.err = r.err || "读取失败"; DK.login = !!r.login; if (r.dir && !r.login) DK.dir = r.dir; }
  diskDraw();
  if (r.login) load();
}
// the path, a link for each folder of it
function diskCrumbs(dir) {
  let p = "", h = `<button class="linkbtn" data-dk="cd" data-dkp="/">我的网盘</button>`;
  for (const s of dir.split("/").filter(Boolean)) { p += "/" + s; h += `<span class="muted">/</span><button class="linkbtn" data-dk="cd" data-dkp="${esc(p)}">${esc(s)}</button>`; }
  return h;
}
function diskJobHTML(j) {
  const name = j.title || j.key.slice(8);
  if (j.stage === "done") {
    return `<div class="impres ok"><div>${esc(name)}：${esc(j.msg)}</div>${j.dir ? `<div class="small">${esc(j.dir)}</div>` : ""}
      ${(j.failed || []).length ? `<div class="small warnline">解压失败：${j.failed.map(esc).join("；")}</div>` : ""}
      <div class="btnrow">${j.dir ? `<button class="btn small" data-dk="folder" data-dkp="${esc(j.dir)}">打开文件夹</button>` : ""}<button class="btn small ghost" data-dk="dismiss" data-dkk="${esc(j.key)}">关闭</button></div></div>`;
  }
  if (j.stage === "failed") {
    return `<div class="impres err"><div>${esc(name)}：${esc(j.err || "下载失败")}</div>
      <div class="btnrow"><button class="btn small" data-dk="get" data-dkp="${esc(j.key.slice(8))}">重试</button><button class="btn small ghost" data-dk="dismiss" data-dkk="${esc(j.key)}">关闭</button></div></div>`;
  }
  if (j.stage === "login") return `<div class="impres err"><div>${esc(name)}：需要登录百度网盘，登录后自动开始</div></div>`;
  const pct = panPct(j);
  const line = j.stage === "download" ? `正在下载 ${Math.min(j.files + 1, j.fileN)}/${j.fileN}　${fmtSize(j.done)} / ${fmtSize(j.total)}　${fmtSpeed(j.speed)}` : j.msg || "排队中";
  return `<div class="impprog"><div class="t">${esc(name)}：${esc(line)}</div><div class="bar wide"><i style="width:${pct}%"></i></div>
    ${j.slow ? `<div class="small warnline">百度网盘对非超级会员账号限速，大文件下载较慢。</div>` : ""}
    <div class="btnrow"><button class="btn small ghost" data-dk="cancel">取消下载</button></div></div>`;
}
// the downloads' part of the dialog: drawn again as they go on, without touching the list above it
function diskJobsDraw() {
  if (!diskOpen()) return;
  const el = $("#dkjobs"), jobs = diskJobs();
  if (el) put(el, jobs.length ? `<h4>下载任务</h4>${jobs.map(diskJobHTML).join("")}` : "");
}
function diskDraw() {
  if ($("#modal").dataset.kind !== "pandisk") return;
  const loggedIn = (S.data.baidu || {}).loggedIn && !DK.login;
  let body = `<p class="muted small">在百度网盘客户端或网页中把分享「保存到网盘」后，可以在这里选择保存的文件夹，下载到素材库并自动解压，合集包同样会拆分。不会改动网盘中的文件。</p>`;
  if (!loggedIn) {
    body += `<div class="news"><div class="t">需要先登录百度网盘。</div><div class="d">登录状态加密保存在本机。</div>
      <div class="btnrow"><button class="btn small primary" data-dk="login">登录百度网盘</button></div></div>`;
  } else {
    body += `<div class="dkcrumbs">${diskCrumbs(DK.dir)}</div>`;
    if (DK.loading) body += `<div class="dklist muted small dknote">正在读取…</div>`;
    else if (DK.err) body += `<div class="dklist dknote"><span class="err small">${esc(DK.err)}</span> <button class="linkbtn" data-dk="cd" data-dkp="${esc(DK.dir)}">重试</button></div>`;
    else if (DK.list && !DK.list.length) body += `<div class="dklist muted small dknote">此文件夹为空</div>`;
    else if (DK.list) {
      body += `<div class="dklist">${DK.list.map(e => `<div class="dkrow${e.dir ? " dir" : ""}">
        ${e.dir ? `<button class="dkname" data-dk="cd" data-dkp="${esc(e.path)}" title="打开">${ICON.folder}<span>${esc(e.name)}</span></button>`
          : `<span class="dkname" title="${esc(e.name)}"><span>${esc(e.name)}</span></span>`}
        <span class="muted small dksize">${e.dir ? "" : esc(fmtSize(e.size))}</span>
        <button class="btn small" data-dk="get" data-dkp="${esc(e.path)}">下载</button></div>`).join("")}</div>`;
    }
  }
  body += `<div id="dkjobs"></div>`;
  $("#modal").innerHTML = `<div class="mhead">从百度网盘下载</div><div class="mbody pandisk">${body}</div><div class="mfoot">
    ${loggedIn && DK.dir !== "/" && !DK.err ? `<button class="btn" data-dk="get" data-dkp="${esc(DK.dir)}">下载此文件夹</button>` : ""}<button class="btn ghost" data-m="close">关闭</button></div>`;
  diskJobsDraw();
}
async function diskAct(el) {
  const a = el.dataset.dk, p = el.dataset.dkp || "";
  if (a === "open") return openDisk();
  if (a === "cd") return diskLoad(p);
  if (a === "folder") return openPath(p);
  if (a === "login") { closeModal(); return baiduLogin(false); }
  if (a === "cancel") { await api("/api/pan/download/cancel", {}); toast("已取消网盘下载"); return poll(true); }
  if (a === "dismiss") { await api("/api/pan/download/dismiss", { key: el.dataset.dkk }); return poll(true); }
  if (a === "get") {
    let r;
    try { r = await api("/api/pan/disk/download", { path: p }); } catch (e) { r = { ok: false, err: "程序内部错误" }; }
    if (!r.ok) { toast(r.err || "无法开始下载", 4000); return; }
    toast(`开始下载「${p.split("/").filter(Boolean).pop() || p}」`);
    await load(); poll(true);
  }
}
