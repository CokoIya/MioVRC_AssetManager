// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)

// 「自定义分类」 in 设置 → 素材: the player's own categories (internal/server/catapi.go). Each change is saved at once,
// whatever else in the settings is still unsaved; the cards move into one from their details or 「批量整理」.

function customCatsHTML(s) {
  const list = s.customCats || [];
  const chips = list.map(c => `<span class="ccat" data-ccname="${esc(c)}"><span class="ccn">${esc(c)}</span>`
    + `<button class="ccbtn" data-ccat="rename" title="重命名">${ICON.edit}</button>`
    + `<button class="ccbtn" data-ccat="remove" title="删除分类">${ICON.x}</button></span>`).join("");
  return `<div class="row ccrow"><label>自定义分类</label>
    <div class="ccats" id="s_ccats">${chips || `<span class="muted small">暂无</span>`}</div>
    <div class="inline"><input id="s_ccat_new" maxlength="12" placeholder="新分类名称"><button class="btn small" data-ccat="add">添加</button></div>
    <div class="muted small">在素材详情或「批量整理」中把素材移入；删除分类后，其中的素材恢复自动分类。</div></div>`;
}

// what the server answered: the settings and the page's category list follow it, and the row is drawn again
async function customCatsDone(r, msg) {
  if (!r.ok) { toast(r.err || "保存失败", 4000); return false; }
  if (r.categories) {
    S.data.categories = r.categories;
    S.data.settings.customCats = r.categories.filter(c => !(c in CAT_COLOR)); // (CAT_COLOR names every built-in one)
  }
  const row = $("#s_ccats") && $("#s_ccats").closest(".ccrow");
  if (row) row.outerHTML = customCatsHTML(S.data.settings);
  if (msg) toast(msg);
  await load();
  return true;
}

async function customCatsAdd() {
  const inp = $("#s_ccat_new");
  const name = inp ? inp.value.trim() : "";
  if (!name) { toast("分类名称不能为空"); return; }
  const r = await api("/api/categories/add", { name });
  if (await customCatsDone(r, `已添加分类「${name}」`)) { const i = $("#s_ccat_new"); if (i) i.focus(); }
}

function customCatsRename(chip) {
  const old = chip.dataset.ccname;
  chip.innerHTML = `<input class="ccedit" maxlength="12" value="${esc(old)}">`;
  const inp = chip.querySelector("input");
  inp.focus(); inp.select();
  let done = false;
  const finish = async ok => {
    if (done) return; done = true;
    const to = inp.value.trim();
    if (!ok || !to || to === old) { const row = chip.closest(".ccrow"); if (row) row.outerHTML = customCatsHTML(S.data.settings); return; }
    const r = await api("/api/categories/rename", { name: old, to });
    if (!await customCatsDone(r, r.changed ? `已重命名为「${to}」，${r.changed} 个素材随之更改` : `已重命名为「${to}」`)) {
      const row = chip.closest(".ccrow"); if (row) row.outerHTML = customCatsHTML(S.data.settings);
    }
  };
  inp.addEventListener("keydown", e => {
    if (e.key === "Enter") { e.preventDefault(); finish(true); }
    else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); finish(false); }
  });
  inp.addEventListener("blur", () => finish(true));
}

async function customCatsRemove(name) {
  if (!confirm(`删除分类「${name}」？其中的素材将恢复自动分类。`)) return;
  const r = await api("/api/categories/remove", { name });
  await customCatsDone(r, r.changed ? `已删除分类「${name}」，${r.changed} 个素材已恢复自动分类` : `已删除分类「${name}」`);
}

document.addEventListener("click", e => {
  const b = e.target.closest("[data-ccat]");
  if (!b) return;
  const what = b.dataset.ccat;
  if (what === "add") return customCatsAdd();
  const chip = b.closest("[data-ccname]");
  if (!chip) return;
  if (what === "rename") return customCatsRename(chip);
  if (what === "remove") return customCatsRemove(chip.dataset.ccname);
});
document.addEventListener("keydown", e => {
  if (e.key === "Enter" && e.target && e.target.id === "s_ccat_new") { e.preventDefault(); customCatsAdd(); }
});
