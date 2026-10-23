// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)

// Appearance: the theme (dark, light, or the system's), the accent colour and 「减少动效」. Kept on this computer; the
// first paint already has them (index.html sets the attributes before the styles load), this file changes them.

const THEME_KEEP = { theme: "mio.theme", accent: "mio.accent", motion: "mio.motion" };
const THEME_ACCENTS = [["sakura", "樱红", "#ff5a6a"], ["sky", "天蓝", "#4ea8ff"], ["mint", "薄荷", "#34d1a4"], ["violet", "紫", "#a68bff"], ["orange", "橙", "#ff9b4a"]];
const themeSystem = window.matchMedia ? matchMedia("(prefers-color-scheme: light)") : null;
const motionSystem = window.matchMedia ? matchMedia("(prefers-reduced-motion: reduce)") : null;

function themeGet(k, def) { try { return localStorage.getItem(THEME_KEEP[k]) || def; } catch (_) { return def; } }
function themeSet(k, v) { try { localStorage.setItem(THEME_KEEP[k], v); } catch (_) {} }
function themeResolved(pref) { return pref === "system" ? (themeSystem && themeSystem.matches ? "light" : "dark") : pref === "light" ? "light" : "dark"; }
function themeApply() {
  const root = document.documentElement, accent = themeGet("accent", "sakura");
  root.dataset.theme = themeResolved(themeGet("theme", "dark"));
  if (accent === "sakura") delete root.dataset.accent; else root.dataset.accent = accent;
  root.dataset.motion = themeGet("motion", "") === "reduce" || (motionSystem && motionSystem.matches) ? "reduce" : "full";
}
function themeReduced() { return document.documentElement.dataset.motion === "reduce"; }

// A change of look spreads from where it was asked for, as a circle; without the browser's view transitions, or
// with less motion, it simply changes.
function themeChange(change, from) {
  if (!document.startViewTransition || themeReduced()) { change(); return; }
  const r = from && from.getBoundingClientRect ? from.getBoundingClientRect() : null;
  const x = r ? r.left + r.width / 2 : innerWidth / 2, y = r ? r.top + r.height / 2 : innerHeight / 2;
  const end = Math.hypot(Math.max(x, innerWidth - x), Math.max(y, innerHeight - y));
  const t = document.startViewTransition(change);
  t.ready.then(() => document.documentElement.animate(
    { clipPath: [`circle(0px at ${x}px ${y}px)`, `circle(${end}px at ${x}px ${y}px)`] },
    { duration: 420, easing: "cubic-bezier(.2, .8, .2, 1)", pseudoElement: "::view-transition-new(root)" })).catch(() => {});
}
function themePick(theme, from) { themeChange(() => { themeSet("theme", theme); themeApply(); themeRowsDraw(); }, from); }
function accentPick(accent, from) { themeChange(() => { themeSet("accent", accent); themeApply(); themeRowsDraw(); }, from); }
// the button in the navigation: between dark and light (from 「跟随系统」 it goes to the other one)
function themeToggle(from) { themePick(document.documentElement.dataset.theme === "light" ? "dark" : "light", from); }

// the rows of the settings' 「外观」 section
function appearanceHTML() {
  const theme = themeGet("theme", "dark"), accent = themeGet("accent", "sakura"), reduce = themeGet("motion", "") === "reduce";
  return `<div class="row" id="s_look"><label>主题</label><div class="seg" role="radiogroup" aria-label="主题">${[["dark", "深色"], ["light", "浅色"], ["system", "跟随系统"]].map(([v, t]) =>
      `<button class="segopt${theme === v ? " on" : ""}" role="radio" aria-checked="${theme === v}" data-theme-pick="${v}">${t}</button>`).join("")}</div></div>
    <div class="row"><label>强调色</label><div class="swatches" role="radiogroup" aria-label="强调色">${THEME_ACCENTS.map(([v, t, c]) =>
      `<button class="swatch${accent === v ? " on" : ""}" role="radio" aria-checked="${accent === v}" data-accent-pick="${v}" title="${t}" aria-label="${t}" style="--sw:${c}"></button>`).join("")}</div></div>
    <div class="row"><label>动效</label><label class="check opt"><input type="checkbox" id="s_motion"${reduce ? " checked" : ""}> 减少动效</label></div>`;
}
function themeRowsDraw() {
  const box = document.getElementById("s_look"); if (!box) return;
  const theme = themeGet("theme", "dark"), accent = themeGet("accent", "sakura");
  for (const b of document.querySelectorAll("[data-theme-pick]")) { const on = b.dataset.themePick === theme; b.classList.toggle("on", on); b.setAttribute("aria-checked", String(on)); }
  for (const b of document.querySelectorAll("[data-accent-pick]")) { const on = b.dataset.accentPick === accent; b.classList.toggle("on", on); b.setAttribute("aria-checked", String(on)); }
}

document.addEventListener("click", e => {
  const t = e.target.closest("[data-theme-pick], [data-accent-pick], [data-theme-toggle]"); if (!t) return;
  if (t.dataset.themePick) themePick(t.dataset.themePick, t);
  else if (t.dataset.accentPick) accentPick(t.dataset.accentPick, t);
  else themeToggle(t);
});
document.addEventListener("change", e => {
  if (e.target.id !== "s_motion") return;
  themeSet("motion", e.target.checked ? "reduce" : ""); themeApply();
});
if (themeSystem && themeSystem.addEventListener) themeSystem.addEventListener("change", () => { if (themeGet("theme", "dark") === "system") themeChange(themeApply); });
if (motionSystem && motionSystem.addEventListener) motionSystem.addEventListener("change", themeApply);
themeApply();
