// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)

// 从 Booth 商品页生成搭配 (internal/ai/oneclick_booth.go): a Booth item's address or number pasted into the modding
// basket (or anywhere in the library), or 「用它搭配」 on a library card's details or a wish-list entry, goes into the
// basket with a base — the basket's, else a base of the library it is made for, else the player picks one of the
// well-known bases (newbie.js). The window says whether the player has the item (「未购买」: its page, the wish list)
// and lists the community's works that use it, each with 「一键克隆」 (social.js).
// 素材版本过时提醒: an item of the basket with a later version among its Booth downloads says so, with 「下载新版本」
// (the library's download path), and the basket is estimated again once it is in; a plugin the one-click job did not
// put on because it does not support the base's version offers its later version, then 「重新应用」
// (internal/ai/oneclick_reapply.go).
"use strict";

// item: what the server said of the Booth item shown (placed: what happened in the basket); works: the community's
// works that use it (null while asked); dl: downloads begun here, followed until they end; after: until when a change
// of the library may be the version just downloaded; paste: what is typed into the basket's field
const OCB = { text: "", item: null, busy: false, err: "", works: null, worksErr: "", seq: 0, dl: new Set(), after: 0, rev: -1, paste: "",
  reapplying: false, watch: 0 };
const OCB_ICON = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 8h14l-1.2 12.2a1 1 0 0 1-1 .8H7.2a1 1 0 0 1-1-.8z"/><path d="M9 11v5M12 11v5M15 11v5"/></svg>';

// ---------- the entry points ----------

// 「用它搭配」: on a library card's details (app.js) and a wish-list entry (stores.js); icon: the grid's small button
function ocbButton(id, small) {
  return id ? `<button class="btn${small ? " small" : ""}" data-ocb="open" data-id="${esc(id)}">用它搭配</button>` : "";
}
function ocbIconButton(id) {
  return id ? `<button class="wb" data-ocb="open" data-id="${esc(id)}" title="用它搭配" aria-label="用它搭配">${OCB_ICON}</button>` : "";
}
// the field at the top of the open basket (oneclick.js ocPanelHTML); what is typed stays through redraws (ocbKeep)
function ocbPasteHTML() {
  return `<div class="ocbpaste"><input id="ocb_paste" autocomplete="off" placeholder="粘贴 Booth 商品链接或商品编号" aria-label="Booth 商品链接或商品编号"><button class="btn small" data-ocb="paste">生成搭配</button></div>`;
}
// called by ocDraw around its redraw: the field keeps its text, its focus and where the caret was
function ocbKeep(el) {
  const refocus = typeof focusMark === "function" ? focusMark(el) : () => {};
  return () => {
    const i = document.getElementById("ocb_paste");
    if (i && i.value !== OCB.paste) i.value = OCB.paste;
    refocus();
  };
}

// ---------- the window ----------

function ocbOpen(text) {
  const m = $("#modal");
  OCB.text = String(text || "").trim(); OCB.item = null; OCB.err = ""; OCB.works = null; OCB.worksErr = ""; OCB.busy = false;
  m.dataset.kind = "ocbooth"; m.classList.remove("ocwin");
  m._h = ""; m.innerHTML = `<div class="mhead">从 Booth 商品页生成搭配</div>
    <div class="mbody"><div class="ocbq"><input id="ocb_q" autocomplete="off" placeholder="粘贴 Booth 商品链接或商品编号" aria-label="Booth 商品链接或商品编号"><button class="btn" data-ocb="read">读取</button></div>
      <div id="ocb_body"></div></div>
    <div class="mfoot" id="ocb_foot"></div>`;
  $("#ocb_q").value = OCB.text;
  ocbDraw(); showModal();
  if (OCB.text) ocbRead(); else $("#ocb_q").focus();
}
function ocbBasketBase() { return typeof OC !== "undefined" ? (OC.items.find(i => i.group === "base") || {}).key || "" : ""; }
async function ocbRead() {
  const text = OCB.text.trim(); if (!text) return;
  const seq = ++OCB.seq;
  OCB.busy = true; OCB.err = ""; OCB.item = null; OCB.works = null; OCB.worksErr = ""; ocbDraw();
  let r; try { r = await api("/api/oneclick/booth", { text, base: ocbBasketBase() }); } catch (e) { r = { ok: false, err: e.message }; }
  if (seq !== OCB.seq) return; // another item was asked for meanwhile
  OCB.busy = false;
  if (!r.ok) { OCB.err = r.err || "无法读取"; return ocbDraw(); }
  OCB.item = r.item;
  ocbPlace(r.item);
  ocbDraw();
  ocbWorks(r.item, seq);
}
// the community's works that use it (/api/community/asset, social/discovery.go)
async function ocbWorks(it, seq) {
  if (typeof soWorkCard !== "function") { OCB.works = []; return ocbDraw(); }
  let r; try { r = await api("/api/community/asset", { booth_id: it.id }); } catch (e) { r = { ok: false, err: e.message }; }
  if (seq !== OCB.seq) return;
  OCB.works = r.ok ? r.works || [] : []; OCB.worksErr = r.ok ? "" : r.err || "无法读取社区";
  ocbDraw();
}

// Into the basket: the item in its group (a base in place of the basket's; a base not bought goes in once it is in
// the library, as a recommended one does), with the library's base it is made for when the basket has none.
// Undone from the toast.
function ocbPlace(it) {
  if (typeof OC === "undefined" || typeof ocChanged !== "function") return;
  const was = OC.items.slice(), add = [];
  const g = it.group && ocGroup(it.group);
  if (!g) { it.placed = "none"; return; }
  if (it.group === "base" && !it.owned) {
    if (typeof nbIntentAdd === "function") nbIntentAdd({ id: it.id, base: (it.bases || [])[0] || it.name, ja: it.name });
    it.placed = "later"; return;
  }
  if (ocHas(it.key)) it.placed = "in";
  else if (it.group === "base") { OC.items = OC.items.filter(i => i.group !== "base"); add.push({ key: it.key, name: it.name, group: "base", face: false }); it.placed = "in"; }
  else if (OC.items.filter(i => i.group === it.group).length >= g[2]) it.placed = "full";
  else if (it.face && OC.items.some(i => i.face)) it.placed = "face";
  else { add.push({ key: it.key, name: it.name, group: it.group, face: !!it.face }); it.placed = "in"; }
  if (it.placed === "in" && it.baseFrom === "library" && !OC.items.some(i => i.group === "base") && !ocHas(it.base)) {
    add.unshift({ key: it.base, name: it.baseName || it.base, group: "base", face: false });
  }
  if (!add.length && OC.items.length === was.length) return;
  ocRedraw(was.map(i => i.key).concat(add.map(i => i.key)));
  OC.items = OC.items.concat(add); OC.open = true; ocChanged();
  toast("已放入改模篮，点此撤销", 5000, () => { ocRedraw(OC.items.map(i => i.key).concat(was.map(i => i.key))); OC.items = was; ocChanged(); if (OCB.item === it) { it.placed = "undone"; ocbDraw(); } });
}

function ocbDLBusy(dl) { return ((typeof S !== "undefined" && S.data && S.data.downloads) || []).some(j => j.id === dl && ["queued", "running", "unpacking"].includes(j.status)); }
// what the item is to the player: in the library, bought and not downloaded, or not bought (its page, the wish list)
function ocbOwnHTML(it) {
  if (it.owned === "library") return `<span class="tag ok">已在素材库中</span>`;
  if (it.owned === "bought") return `<span class="tag">已购，未下载</span><button class="btn small" data-ocb="dlall" data-item="${esc(it.id)}">下载</button>`;
  const wished = typeof wishHas === "function" && wishHas(it.id);
  return `<span class="tag bad">未购买</span><button class="btn small" data-ocb="page" data-id="${esc(it.id)}">打开商品页</button>
    ${typeof wishAdd === "function" ? `<button class="btn small${wished ? " on" : ""}" data-ocb="wish"${wished ? " disabled" : ""}>${wished ? "已在愿望单" : "加入愿望单"}</button>` : ""}`;
}
// a later version among its Booth downloads than the one on disk, with its download (the library's)
function ocbNewerHTML(item, newer, local, dl, line) {
  if (!newer || !dl || !item) return "";
  const busy = ocbDLBusy(dl);
  return `<div class="ocbnews"><span>${esc(line || `Booth 有新版本 ${newer}（本地 ${local || "—"}）`)}</span>
    <button class="btn small" data-ocb="dl" data-item="${esc(item)}" data-dl="${esc(dl)}"${busy ? " disabled" : ""}>${busy ? "正在下载" : "下载新版本"}</button></div>`;
}
function ocbBasketHTML(it) {
  switch (it.placed) {
    case "none": return `<p class="ochint">改模篮不接受该类素材</p>`;
    case "later": return `<p class="ochint">购买并下载后将自动放入改模篮</p>`;
    case "full": { const g = ocGroup(it.group); return `<p class="warnc">${esc(`「${g[1]}」最多 ${g[2]} 个`)}</p>`; }
    case "face": return `<p class="warnc">面捕最多 1 个</p>`;
    case "undone": return "";
  }
  const base = OC.items.find(i => i.group === "base");
  if (it.group !== "base" && !base) {
    return `<p class="ochint">已放入改模篮</p><div class="ocbbase"><p class="nbhead">选择素体</p>${typeof nbBasesFor === "function" ? nbBasesFor(it.bases) : ""}</div>`;
  }
  return `<p class="ochint">已放入改模篮${base && it.group !== "base" ? `<span class="ocbsep">素体</span><b data-i18n="off">${esc(base.name)}</b>` : ""}</p>`;
}
function ocbWorksHTML(it) {
  if (OCB.works === null) return `<div class="ocbworks"><h5 class="lmh">相关作品</h5><div class="rmbusy"><i class="spin"></i>正在读取…</div></div>`;
  if (OCB.worksErr) return `<div class="ocbworks"><h5 class="lmh">相关作品</h5><p class="muted small">${esc(OCB.worksErr)}</p></div>`;
  if (!OCB.works.length) return `<div class="ocbworks"><h5 class="lmh">相关作品</h5><p class="muted small">还没有作品</p></div>`;
  const list = OCB.works.slice(0, 6);
  return `<div class="ocbworks"><h5 class="lmh">相关作品</h5><div class="ocbwlist">${list.map(w => `<div class="ocbwork">
      <button class="ocbwpic" data-so="work" data-id="${esc(w.id)}" aria-label="${esc(w.title)}">${w.pic ? `<img src="${esc(w.pic)}" alt="" loading="lazy">` : `<span data-i18n="off">${esc((w.title || "").slice(0, 8))}</span>`}</button>
      <div class="ocbwinfo"><button class="linkbtn" data-so="work" data-id="${esc(w.id)}" data-i18n="off">${esc(w.title)}</button><span class="muted small" data-i18n="off">${esc([w.nickname || w.author, w.base].filter(Boolean).join(" · "))}</span></div>
      ${w.has_recipe ? `<button class="btn small" data-ocb="clone" data-id="${esc(w.id)}">一键克隆</button>` : ""}</div>`).join("")}</div>
    ${OCB.works.length > list.length ? `<button class="btn small ghost" data-ocb="more">更多</button>` : ""}</div>`;
}
function ocbDraw() {
  const m = $("#modal"), body = $("#ocb_body"), foot = $("#ocb_foot");
  if (m.dataset.kind !== "ocbooth" || !body) return;
  const it = OCB.item;
  let html = "";
  if (OCB.busy) html = `<div class="rmbusy"><i class="spin"></i>正在读取…</div>`;
  else if (OCB.err) html = `<p class="warnc">${esc(OCB.err)}</p>`;
  else if (it) {
    const pic = it.cover || (it.image ? "/rthumb?u=" + encodeURIComponent(it.image) : "");
    html = `<div class="ocbitem"><div class="ocbcover">${pic ? `<img src="${esc(pic)}" alt="" loading="lazy">` : ""}</div>
      <div class="ocbinfo"><div class="ocbname" data-i18n="off">${esc(it.name)}</div>
        ${it.shop || it.price ? `<div class="muted small" data-i18n="off">${esc([it.shop, it.price].filter(Boolean).join("　"))}</div>` : ""}
        <div class="ocbtags">${it.category ? `<span class="tag">${esc(it.category)}</span>` : ""}${(it.bases || []).length ? `<span class="muted small">适配素体</span>${it.bases.map(b => `<span class="base" data-i18n="off">${esc(b)}</span>`).join("")}` : ""}</div>
        <div class="ocbown">${ocbOwnHTML(it)}</div>
        ${ocbNewerHTML(it.id, it.newer, it.localVer, it.newerDl)}</div></div>
      ${ocbBasketHTML(it)}${ocbWorksHTML(it)}`;
  }
  put(body, html);
  if (foot) put(foot, `${it && it.placed === "in" ? `<button class="btn" data-ocb="basket">查看改模篮</button>` : ""}<button class="btn primary" data-ocb="close">关闭</button>`);
}

// 一键克隆 of a work in the list: the work as its own window reads it, then social.js's clone
async function ocbClone(id) {
  if (typeof soClone !== "function" || typeof soCloneWorkOf !== "function") return;
  let r; try { r = await api("/api/community/work", { id }); } catch (e) { r = { ok: false, err: "程序无响应" }; }
  if (!r.ok) { toast(r.err || "无法读取该作品", 4000); return; }
  if (typeof soOwned === "function") { try { await soOwned({ data: r }); } catch (e) {} }
  return soClone(soCloneWorkOf(r));
}

// ---------- the basket: items by their Booth number, later versions ----------

// the library's card of a Booth item: files on disk first, then a netdisk share, then a purchase not downloaded
function ocbCard(id) {
  const rank = a => a.virtual ? 2 : a.panOnly ? 1 : 0;
  return ((S.data && S.data.assets) || []).filter(a => String(a.boothId || "") === id && !a.splitInto).sort((a, b) => rank(a) - rank(b))[0] || null;
}
// An item put in by its Booth number ("booth:<id>"), or as a purchase not downloaded ("purchase:<id>"), is replaced
// by its card once the library has one with files (called before each estimate). true: the basket changed.
function ocbSwap() {
  if (typeof OC === "undefined" || typeof S === "undefined" || !S.data) return false;
  let changed = false;
  for (const it of OC.items) {
    const m = /^(booth|purchase):(\d{1,12})$/.exec(it.key); if (!m) continue;
    const have = findAsset(it.key), best = ocbCard(m[2]);
    if (!best || best.key === it.key || (have && best.virtual)) continue;
    ocRedraw([it.key, best.key]); it.key = best.key; it.name = best.name; changed = true;
  }
  if (!changed) return false;
  const seen = new Set();
  OC.items = OC.items.filter(i => !seen.has(i.key) && seen.add(i.key));
  ocSave();
  if (typeof renderGrid === "function") renderGrid(); // (the cards' basket marks)
  return true;
}
// below an item of the basket (oneclick.js ocPanelHTML): not bought (its page, the wish list), bought and not
// downloaded (its download), a later version on Booth
function ocbItemHTML(i, x) {
  if (!x) return "";
  let html = "";
  if (x.source === "booth_not_owned" && x.boothId) {
    const wished = typeof wishHas === "function" && wishHas(x.boothId);
    html += `<div class="ocbnews"><button class="btn small" data-ocb="page" data-id="${esc(x.boothId)}">打开商品页</button>${typeof wishAdd === "function"
      ? `<button class="btn small" data-ocb="wish" data-id="${esc(x.boothId)}" data-name="${esc(x.name)}"${wished ? " disabled" : ""}>${wished ? "已在愿望单" : "加入愿望单"}</button>` : ""}</div>`;
  } else if (x.source === "booth_not_downloaded" && x.boothId) {
    html += `<div class="ocbnews"><button class="btn small" data-ocb="dlall" data-item="${esc(x.boothId)}">下载</button></div>`;
  }
  return html + ocbNewerHTML(x.boothId, x.newer, x.localVer, x.newerDl);
}

// ---------- the one-click result: a plugin's later version, 「重新应用」 ----------

// below a plugin of the result (oneclick.js ocPluginsHTML)
function ocbPluginHTML(p) {
  if (p.fresh) return `<div class="ocbnews"><button class="btn small primary" data-ocb="reapply" data-plugin="${esc(p.id)}"${OCB.reapplying ? " disabled" : ""}>重新应用</button></div>`;
  if (p.state === "skipped" && p.newer) return ocbNewerHTML(p.boothId, p.newer, p.localVer, p.newerDl, "Booth 有新版本，可能已支持该素体版本");
  return "";
}
async function ocbReapply(plugin) {
  const j = typeof OC !== "undefined" && OC.job; if (!j || OCB.reapplying) return;
  OCB.reapplying = true; ocJobDraw();
  let r; try { r = await api("/api/oneclick/reapply", { id: j.id, plugin }); } catch (e) { r = { ok: false, err: e.message }; }
  OCB.reapplying = false;
  if (!r.ok) { toast(r.err || "操作失败", 5000); ocJobDraw(); return; }
  OC.job = r.job; ocJobDraw(); ocbFollow();
}
// the job's plugins while one is applied again (the window's own polling has stopped: the job is done)
function ocbFollow() {
  clearTimeout(OCB.watch);
  OCB.watch = setTimeout(async () => {
    let r; try { r = await ocStatus(); } catch (_) { return ocbFollow(); }
    if (r.ok && r.job && OC.job && r.job.id === OC.job.id) { OC.job = r.job; ocJobDraw(); }
    if (OC.job && ((OC.job.result || {}).plugins || []).some(p => p.state === "applying")) ocbFollow();
  }, 1500);
}
// the result again from the program, when what the library holds may have changed it
async function ocbJobAgain() {
  const m = $("#modal"); if (m.dataset.kind !== "oneclick" || !OC.job || OC.job.state !== "done") return;
  let r; try { r = await ocStatus(); } catch (_) { return; }
  if (r.ok && r.job && OC.job && r.job.id === OC.job.id) { OC.job = r.job; ocJobDraw(); }
}

// ---------- what goes on ----------

document.addEventListener("click", async e => {
  const b = e.target.closest("[data-ocb]"); if (!b) return;
  const it = OCB.item;
  switch (b.dataset.ocb) {
    case "open": return ocbOpen(b.dataset.id);
    case "paste": {
      const v = (OCB.paste || "").trim(), i = $("#ocb_paste");
      if (!v) { if (i) i.focus(); return; }
      OCB.paste = ""; if (i) i.value = "";
      return ocbOpen(v);
    }
    case "read": OCB.text = $("#ocb_q").value; return ocbRead();
    case "close": closeModal(true); return;
    case "basket":
      closeModal(true);
      if (S.view !== "lib") setView("lib");
      OC.open = true; ocDraw();
      { const d = $("#oc_dock"); if (d) d.scrollIntoView({ block: "end" }); }
      return;
    case "page": closeModal(true); return openLink(boothItemURL(b.dataset.id));
    case "wish": {
      const id = b.dataset.id || (it && it.id); if (!id) return;
      const seed = it && it.id === id ? { id, title: it.name, shop: it.shop || "", thumb: it.image || "", price: it.price || "" } : { id, title: b.dataset.name || "" };
      b.disabled = true;
      await wishAdd(seed);
      ocbDraw(); if (typeof ocDraw === "function") ocDraw();
      return;
    }
    case "dlall": await startDownload(b.dataset.item); return ocbDraw();
    case "dl":
      b.disabled = true;
      await startDownload(b.dataset.item, [b.dataset.dl]);
      OCB.dl.add(b.dataset.dl); // (followed from here: the window's list of downloads has been read again)
      ocbDraw(); if (typeof ocDraw === "function") ocDraw(); if ($("#modal").dataset.kind === "oneclick") ocJobDraw();
      return;
    case "clone": return ocbClone(b.dataset.id);
    case "more": if (it && typeof socialByAsset === "function") return socialByAsset(it.id, it.name); return;
    case "reapply": return ocbReapply(b.dataset.plugin);
  }
});
document.addEventListener("input", e => { if (e.target.id === "ocb_paste") OCB.paste = e.target.value; });
document.addEventListener("keydown", e => {
  if (e.key !== "Enter" || e.isComposing) return;
  if (e.target.id === "ocb_q") { e.preventDefault(); OCB.text = e.target.value; ocbRead(); }
  else if (e.target.id === "ocb_paste" && OCB.paste.trim()) { e.preventDefault(); const v = OCB.paste.trim(); OCB.paste = ""; e.target.value = ""; ocbOpen(v); }
});
// a Booth item's address pasted anywhere in the library but a text field: the window for it
document.addEventListener("paste", e => {
  const t = e.target;
  if (typeof S === "undefined" || S.view !== "lib" || $("#modal").classList.contains("on")) return;
  if (t && (t.closest("input, textarea, select, [contenteditable]") || t.isContentEditable)) return;
  const text = (e.clipboardData && e.clipboardData.getData("text")) || "";
  if (typeof boothNum !== "function" || !boothNum(text.trim())) return;
  e.preventDefault(); ocbOpen(text.trim());
});
// Downloads begun here: once one ends, the library's next changes may be its version — the basket is estimated
// again and an open result asked for again; items by their Booth number become their cards.
setInterval(() => {
  if (typeof S === "undefined" || !S.data || typeof OC === "undefined") return;
  let ended = false;
  for (const dl of [...OCB.dl]) if (!ocbDLBusy(dl)) { OCB.dl.delete(dl); ended = true; }
  if (ended) OCB.after = Date.now() + 180000;
  const moved = S.rev !== OCB.rev; OCB.rev = S.rev;
  if (!ended && !moved) return;
  const swapped = moved && ocbSwap();
  const fresh = ended || (moved && Date.now() < OCB.after);
  if (swapped) ocChanged();
  else if (fresh && OC.items.some(i => i.group === "base") && !ocActive(OC.job)) ocEstimate();
  else if (moved && OCB.dl.size) ocDraw(); // (a download's button)
  if (fresh) ocbJobAgain();
  if (moved && $("#modal").dataset.kind === "ocbooth") ocbDraw();
}, 1000);
