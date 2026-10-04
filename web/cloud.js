// Google Drive and Dropbox shares: netdisk cards of another service (keys gd:… / db:…). The cards, the
// drawer's netdisk section and the download queue are app.js's; this file tells them which service a card
// is on, and gives them the words and the download section that differ from a Baidu share's.
"use strict";

const CLOUD_NAME = { gd: "Google Drive", db: "Dropbox" };

// a share card's key, on any of the services
function isNetdiskKey(k) { return /^(pan|gd|db):/.test(k || ""); }
// where the share links of a text stand (-1: none of that kind); the server reads the links themselves
const CLOUD_RE = {
  gd: /(drive|docs)\.google\.com\/((a\/[^\/\s]+|u\/\d+|corp)\/)*(file\/|open\?|uc\?|drive\/|folderview\?|embeddedfolderview\?)|drive\.usercontent\.google\.com\//i,
  db: /dropbox(usercontent)?\.com\/(s|sh|scl)\//i,
  bd: /(pan|yun)\.baidu\.com\/(s\/|share\/init\?|wap\/init\?)/i,
};
function shareLinksAt(text) {
  const t = String(text || "").replace(/[！-～]/g, c => String.fromCharCode(c.charCodeAt(0) - 0xFEE0));
  return { gd: t.search(CLOUD_RE.gd), db: t.search(CLOUD_RE.db), bd: t.search(CLOUD_RE.bd) };
}
// the service a card's link field points to ("" when it is not Google Drive / Dropbox). A field that holds a
// Baidu link too is the Baidu share's, as the server keeps it (netdisk.ShareID).
function cloudSvcOf(text) {
  const at = shareLinksAt(text);
  if (at.bd >= 0) return "";
  return at.gd >= 0 && (at.db < 0 || at.gd < at.db) ? "gd" : at.db >= 0 ? "db" : "";
}
// the service a pasted text makes a card of: its first share link decides ("百度 … 提取码 … / Drive …" is
// a Baidu share), as on the server (netdisk.CloudFirst)
function cloudFirst(text) {
  const at = shareLinksAt(text);
  const svc = at.gd >= 0 && (at.db < 0 || at.gd < at.db) ? "gd" : at.db >= 0 ? "db" : "";
  return svc && at.bd >= 0 && at.bd < at[svc] ? "" : svc;
}
// the service of a card: by its key, by the share it was downloaded from, or by its share link
function cloudSvc(a) {
  if (!a) return "";
  const m = /^(gd|db):/.exec(a.fromPan || a.key || "");
  return m ? m[1] : cloudSvcOf((a.user || {}).shareUrl || "");
}
function cloudName(svc) { return CLOUD_NAME[svc] || ""; }
// the service's name for a card on Google Drive / Dropbox, zh (网盘, 百度网盘) for the others
function cloudText(a, zh) { return cloudName(cloudSvc(a)) || zh; }
// what a card stands for before its listing is read
function cloudShareLabel(a) { return cloudSvc(a) === "gd" ? "Google Drive 分享" : "Dropbox 分享"; }
// the share opens in the browser (there is no sign-in and no access code to copy)
function cloudOpenLabel(a, zh) { return cloudSvc(a) ? "在浏览器中打开" : zh; }

// the drawer's download section of a Google Drive / Dropbox card: no sign-in, no copy into a netdisk
function cloudDLSec(a) {
  if (!a.user.shareUrl || !(a.panOnly || a.fromPan)) return "";
  const svc = cloudSvc(a), name = cloudName(svc), j = panJobFor(a);
  const busy = panJobBusy(a), sel = S.panSel.size > 0 && panPickable(a);
  // a folder downloaded from the share: more of it, once parts of it are picked
  if (!a.panOnly && !sel && !(j && j.stage !== "done")) return "";
  let body = j ? panJobHTML(j) : "";
  const where = a.panOnly ? "下载到 " + esc(S.data.dlDir || "素材文件夹") : "下载到该素材所在的文件夹";
  const proxy = svc === "gd" ? "中国大陆用户需在「设置」中填写代理。" : "";
  if (!busy && sel) {
    body += `<div class="selline">${esc(panSelLine(a))}<button class="linkbtn" data-d="panselclear">取消选择</button></div>
      <div class="impform"><button class="btn primary" data-d="pandlsel">${ICON.download}<span>下载所选</span></button>${a.panOnly ? `<button class="btn ghost" data-d="pandl">下载全部</button>` : ""}</div>
      <div class="impform">${projSelect("pan_proj")}<button class="btn" data-d="pandlselimp">${ICON.cube}<span>下载所选并导入</span></button></div>
      <div class="hint2">仅下载所选内容，保留原有文件夹结构；${where}，压缩包自动解压。${proxy}</div>`;
  } else if (!busy && a.panOnly) {
    body += `<div class="impform"><button class="btn primary" data-d="pandl">${ICON.download}<span>下载到素材库</span></button></div>
      <div class="impform">${projSelect("pan_proj")}<button class="btn" data-d="pandlimp">${ICON.cube}<span>下载并导入</span></button></div>
      <div class="hint2">直接从 ${esc(name)} 下载，无需登录；${where}，压缩包自动解压。${panPickable(a) ? "如只需部分内容，可在下方「网盘内容」中勾选。" : ""}${proxy}</div>`;
  }
  return `<div class="sec imp" id="pansec"><h4>${a.panOnly ? "下载到本地" : `从 ${esc(name)} 下载`}</h4>${body}</div>`;
}

// the 「添加网盘素材」 dialog once it is open: a Google Drive / Dropbox link needs no access code and no
// sign-in, so those parts step aside while one is in the box
function cloudAddHook() {
  const text = $("#pa_text"), pwd = $("#pa_pwd"), dl = $("#pa_dl");
  if (!text) return;
  const pwdRow = pwd && pwd.closest(".row"), bdNote = $("#pa_bdnote"), note = $("#pa_cloudnote");
  const sync = () => {
    const svc = cloudFirst(text.value);
    if (pwdRow) pwdRow.hidden = !!svc;
    if (bdNote) bdNote.hidden = !!svc;
    if (dl) {
      if (svc && dl.disabled) { dl.disabled = false; dl.checked = true; }
      else if (!svc && !S.data.paneMode) { dl.disabled = true; dl.checked = false; }
    }
    if (note) note.textContent = svc ? `已识别 ${cloudName(svc)} 链接，下载无需登录` + (svc === "gd" ? "（中国大陆用户需在「设置」中填写代理）" : "") : "";
  };
  text.addEventListener("input", sync);
  sync();
}
