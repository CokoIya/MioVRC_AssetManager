// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)

// A small menu that opens under a button (「⋯」 on cards). Its items are ordinary buttons with the data- attributes
// the page's click handlers already know; attrs go onto the menu itself, so a handler that looks for an ancestor
// (data-proj on a project card) finds it there. One menu at a time; a click elsewhere, Esc or scrolling closes it.

function popMenuClose() {
  const m = document.getElementById("popmenu");
  if (m) m.remove();
  document.querySelectorAll("[data-popopen]").forEach(b => { b.removeAttribute("data-popopen"); b.setAttribute("aria-expanded", "false"); });
}
function popMenuOpen(anchor, items, attrs) {
  const again = anchor.hasAttribute("data-popopen");
  popMenuClose();
  if (again) return; // a second click on the same button closes it
  const m = document.createElement("div");
  m.id = "popmenu"; m.className = "popmenu"; m.setAttribute("role", "menu");
  for (const [k, v] of Object.entries(attrs || {})) m.setAttribute(k, v);
  m.innerHTML = items.filter(Boolean).map(it => it === "-" ? `<hr>` :
    `<button role="menuitem" class="${it.danger ? "danger" : ""}"${Object.entries(it.data || {}).map(([k, v]) => ` data-${k}="${esc(String(v))}"`).join("")}>${it.icon || ""}<span>${esc(it.label)}</span></button>`).join("");
  document.body.appendChild(m);
  const r = anchor.getBoundingClientRect(), w = m.offsetWidth, h = m.offsetHeight;
  let x = r.right - w, y = r.bottom + 6;
  if (y + h > innerHeight - 8) y = r.top - h - 6; // no room below: above the button
  m.style.left = Math.max(8, Math.min(x, innerWidth - w - 8)) + "px";
  m.style.top = Math.max(8, y) + "px";
  anchor.setAttribute("data-popopen", ""); anchor.setAttribute("aria-expanded", "true");
  const first = m.querySelector("button"); if (first) first.focus({ preventScroll: true });
}
document.addEventListener("click", e => {
  const m = document.getElementById("popmenu");
  if (!m) return;
  // a click on an item runs it (the page's handlers see the same click) and then closes the menu
  if (!e.target.closest("[data-popopen]")) setTimeout(popMenuClose);
}, true);
document.addEventListener("keydown", e => {
  const m = document.getElementById("popmenu"); if (!m) return;
  if (e.key === "Escape") { const a = document.querySelector("[data-popopen]"); popMenuClose(); if (a) a.focus(); e.stopPropagation(); return; }
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    const bs = [...m.querySelectorAll("button")], i = bs.indexOf(document.activeElement);
    const n = bs[(i + (e.key === "ArrowDown" ? 1 : bs.length - 1)) % bs.length]; if (n) n.focus(); e.preventDefault();
  }
}, true);
window.addEventListener("resize", popMenuClose);
document.addEventListener("scroll", popMenuClose, true);
