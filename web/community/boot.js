// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// Before the first paint of the community in a browser: the flag the community code reads to leave out what only
// the program can do, the language (i18n.js: what was picked here, else the browser's), and the look (theme.js
// keeps it up to date; a first visit follows the system's).
"use strict";
window.WEB = true;
window.API_TOKEN = "";
window.LANG = "?";
(function () {
  try {
    var d = document.documentElement;
    if (!localStorage.getItem("mio.theme")) localStorage.setItem("mio.theme", "system");
    var t = localStorage.getItem("mio.theme"), a = localStorage.getItem("mio.accent"), m = localStorage.getItem("mio.motion");
    if (t === "system") t = matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
    d.dataset.theme = t === "light" ? "light" : "dark";
    if (a && a !== "sakura") d.dataset.accent = a;
    d.dataset.motion = m === "reduce" || matchMedia("(prefers-reduced-motion: reduce)").matches ? "reduce" : "full";
  } catch (e) {
    document.documentElement.dataset.theme = window.matchMedia && matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }
})();
