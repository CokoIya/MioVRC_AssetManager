// 合集包: a card that holds a collection of products — a seller's share that is one archive with the archives
// of many products in it, downloaded and unpacked into one folder. One-click import would put every one of
// them into the Unity project, so such a card says what it is (a.bundle: how many products; a.bundlePacked:
// some of them are still inside archives) and has no one-click import. There are two ways out of it: split it
// into a card for each product, or say that it is one asset after all. The cards, the drawer and the import
// job are app.js's; this file gives them the tag, the note, and what the note's two buttons do.
"use strict";

// gone: the card that was just split, until the scan has run; told: the job whose end has been said;
// quiet: until when the notice of a split is up
const BUNDLE = { gone: "", told: 0, quiet: 0 };

// a notice about a split. While it is up, the library's own 「新增 N 个素材」 is left out (app.js: load): the
// cards that arrive are the ones this notice is about, and it would take its place before it is read.
function bundleToast(msg, ms) { BUNDLE.quiet = Date.now() + ms; toast(msg, ms); }
function bundleQuiet() { return Date.now() < BUNDLE.quiet; }

// the tag on a card
function bundleTag(a) {
  if (!isLocal(a) || !(a.bundle >= 2)) return "";
  return `<b class="bundletag" title="合集包：包含 ${esc(a.bundle)} 个素材">合集 · ${esc(a.bundle)}</b>`;
}

// the note above the import form of such a card (app.js: importSec)
function bundleNote(a) {
  return `<div class="news bundlenote"><div class="t">合集包：包含 ${esc(a.bundle)} 个素材</div>
    <div class="d">一键导入会把其中的全部素材导入工程，因此已停用。请先拆分为独立的素材卡片，再分别导入。${a.bundlePacked ? "拆分时会解压其中的压缩包。" : ""}</div>
    <div class="btnrow"><button class="btn small primary" data-d="bundlesplit">拆分为 ${esc(a.bundle)} 个素材</button><button class="btn small" data-d="bundleone" title="这是一个素材：恢复一键导入，不再提示">作为单个素材</button></div></div>`;
}

// 「拆分为 N 个素材」: where the products lie in folders, the folders are marked and the library is scanned
// again; where some are still packed, a job unpacks them first — it is shown where an import's progress is,
// with the password and the Recycle Bin box of the import form
async function bundleSplit(a) {
  const r = await api("/api/bundle/split", { key: a.key, pwd: ($("#imp_pwd") || {}).value || "", recycle: S.impRecycle !== false });
  if (!r.ok) { toast(r.err || "无法拆分", 4000); return; }
  BUNDLE.gone = a.key;
  if (!r.job) bundleToast(`已拆分为 ${r.n} 个素材，正在重新扫描`, 4000);
  await load(); poll(true);
}

// 「作为单个素材」: the folder (or, of a card that is only an archive, the archive) is kept as one asset
async function bundleOne(a) {
  const locs = a.locations || [], dirs = locs.filter(l => l.kind === "dir");
  for (const l of dirs.length ? dirs : locs) await api("/api/override", { path: l.path, mode: "asset" });
  toast("已设为单个素材，正在重新扫描", 4000);
  await load(); poll(true);
}

document.addEventListener("click", async e => {
  const b = e.target.closest("#drawer [data-d=bundlesplit], #drawer [data-d=bundleone]");
  const a = b && !b.disabled && S.openKey && findAsset(S.openKey);
  if (!a) return;
  b.disabled = true; // (the drawer is drawn again when the answer is there)
  try { await (b.dataset.d === "bundlesplit" ? bundleSplit(a) : bundleOne(a)); }
  finally { b.disabled = false; }
});

// After each reload of the library, before it is drawn (app.js: load).
// A job that ended by splitting a collection — one that was asked to, or an import that found one once it had
// unpacked the asset — says so in a notice and is put away: the card its result would be shown on is gone
// with the scan. A split that failed stays on its card, as a failed import does: what it says is what to do.
// The details panel of a card that was split closes once the card is gone; left open, it would go over to
// one of the products (as it does for a download that became several cards).
async function bundleLoaded() {
  const j = S.data.importJob;
  if (j && ["done", "failed"].includes(j.stage) && (j.split || j.bundle >= 2)) {
    if (BUNDLE.told !== j.id) {
      BUNDLE.told = j.id;
      bundleToast(j.err || j.msg, 5000);
      if (j.bundle >= 2 && j.key && !isNetdiskKey(j.key) && !j.key.startsWith("purchase:")) BUNDLE.gone = j.key;
    }
    if (j.stage === "done" || j.bundle >= 2) {
      S.data.importJob = null;
      try { await api("/api/import/dismiss", {}); } catch (e) {} // (asked again at the next reload)
    }
  }
  if (!BUNDLE.gone) return;
  if (!S.byKey.has(BUNDLE.gone)) { if (S.openKey === BUNDLE.gone) closeDrawer(); BUNDLE.gone = ""; }
  else if (!S.data.busy) BUNDLE.gone = ""; // scanned, and still there: it was no collection after all
}

// The settings' 「手动调整」 list: of the marks of one collection only the outermost is listed — its 恢复 takes
// the ones below it along, and the collection is one card again.
function bundleOverrides(ov) {
  const tops = Object.keys(ov).filter(p => ov[p] === "bundle");
  const under = (p, top) => p.length > top.length + 1 && p.startsWith(top) && /[\\/]/.test(p[top.length]);
  return Object.entries(ov).filter(([p, v]) => v !== "bundle" || !tops.some(top => under(p, top)));
}
