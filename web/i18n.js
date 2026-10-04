// the interface in other languages
"use strict";
// The sources stay Chinese: the templates, the program's messages and the Unity plugin's all reach the page as
// Chinese text. When another language is picked, what is shown is rewritten on its way into the page: every text
// node and the attributes people read (title, placeholder, aria-label, alt, a button's value) are looked up in
// that language's dictionary (i18n-en.js, i18n-ja.js) before the page is painted, and so is what confirm() asks.
// What the dictionary does not know stays as it is, which is how names, paths and notes are left alone.
// In Chinese none of this runs.
//
// A dictionary is I18N.add(lang, { exact: { 中文: text }, patterns: [[中文 with {slots}, text with {slots}]] }).
// Slots in a pattern: {x} any text, {#n} a number, {@t} a time as fmtTime writes it. In its text: {x} is what was
// caught, itself looked up; {x|raw} as it is; {x|lc} with a small first letter; {x|list} a 、 list, each part
// looked up; {n|s} "s" unless 1;
// {n|pl:one:many}; {_prev|…} the number in the element just before (for a sentence a <b> cuts in two).
// A key starting with "re:" is a regular expression, its groups are {1} {2} …; it is tried before the others.
// Of the patterns that match, the one with the most literal text wins.
// A text made of sentences (。), of pieces set apart by a wide space or by " · ", goes piece by piece when every
// piece is known. What no entry matches is tried in parts: those pieces again, 「label：rest」, clauses (；),
// a trailing （…）, and 、 or ， lists whose parts are all known. So a sentence needs one entry, whole, without
// its 。, and a new text is added by adding entries to the two dictionaries, nothing else.
//
// Not looked at: what is typed (inputs, a textarea's text, contenteditable), <pre>, svg, and whatever a template
// marks data-i18n="off" (the element and all in it) or data-i18n="text" (its own text and tooltip, not its
// children); a few classes that only ever hold data are listed in OFF below.
(() => {
  const NAMES = { "": "简体中文", en: "English", ja: "日本語" };
  const given = typeof window.LANG === "string" && window.LANG !== "__LANG__" ? window.LANG : "";
  // "?": a fresh install, nothing chosen yet — what was picked on the first-run screen, else the system's language
  function firstRun() {
    let v = null; try { v = localStorage.getItem("vrclib.lang"); } catch (e) {}
    if (v === "zh") return "";
    if (v === "en" || v === "ja") return v;
    const l = (navigator.language || "").toLowerCase();
    return l.startsWith("zh") ? "" : l.startsWith("ja") ? "ja" : "en";
  }
  const lang = given === "?" ? firstRun() : NAMES[given] ? given : "";
  const I = window.I18N = { lang, fresh: given === "?", names: NAMES, on: false, t: s => s, add, misses: null };

  // the language box: at the top of the settings and of the first-run screen, named in all three languages
  window.langRowHTML = () => `<div class="row" data-i18n="off"><label for="i18n_lang">语言 / Language / 言語</label><select id="i18n_lang">${
    Object.keys(NAMES).map(k => `<option value="${k}"${k === lang ? " selected" : ""}>${NAMES[k]}</option>`).join("")}</select></div>`;
  document.addEventListener("change", async e => {
    if (e.target.id !== "i18n_lang") return;
    const v = e.target.value;
    if (v === lang) return;
    if (I.fresh) { try { localStorage.setItem("vrclib.lang", v || "zh"); } catch (er) {} } // saved with the folders, when the first-run screen is done
    else await api("/api/settings", { settings: Object.assign({}, S.data.settings, { lang: v }), noRescan: true });
    location.reload();
  });
  if (!lang) return;

  document.documentElement.lang = lang;
  document.write(`<link rel="stylesheet" href="i18n.css"><script src="i18n-${lang}.js"><\/script>`);

  const JA = lang === "ja";
  const EXACT = new Map(), ANY = [], BY = new Map(), CACHE = new Map();
  const CJK = /[\u3000-\u303f\u3400-\u9fff\uff00-\uffef]/;
  const STOP = JA ? "。" : ". ", COLON = JA ? "：" : ": ", SEMI = JA ? "；" : "; ", COMMA = JA ? "、" : ", ";
  const OPEN = JA ? "（" : " (", CLOSE = JA ? "）" : ")", WIDE = JA ? "\u3000" : "\u2003";
  const STRONG = [["。", STOP], ["\u3000", WIDE], [" · ", " · "]]; // what divides a text into pieces before anything else
  const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
  const TIME = /^(\d{1,2})月(\d{1,2})日(?: (\d\d:\d\d))?$/;
  const SLOT = { "": "([\\s\\S]+?)", "#": "(-?\\d+(?:[.,]\\d+)*)", "@": "(\\d{1,2}月\\d{1,2}日(?: \\d\\d:\\d\\d)?|从未)" };
  const norm = s => s.replace(/[^\S\u3000]+/g, " ").replace(/ ?\u3000 ?/g, "\u3000").trim(); // (a wide space divides, see STRONG)
  const RAW = new Map(); // texts as the page had them, by what they came to
  let nPat = 0, PREV = null, fleeting = false;

  function add(l, d) {
    if (l !== lang) return;
    for (const k in d.exact || {}) { const key = norm(k); if (key.length > 1 && key.endsWith("。")) EXACT.set(key.slice(0, -1), d.exact[k].replace(/[.。]$/, "")); else EXACT.set(key, d.exact[k]); }
    for (const [k, v] of d.patterns || []) addPattern(k, v);
    if (!I.on) start();
  }
  // A pattern is tried only on text that holds the first two characters of its longest literal piece (BY), so a
  // miss costs a few map lookups however many patterns there are.
  function addPattern(key, out) {
    const p = { out, i: nPat++, lit: 0, names: [], kinds: [], prev: out.indexOf("{_prev") >= 0 };
    if (key.startsWith("re:")) { p.re = new RegExp(key.slice(3)); p.lit = 1e3; ANY.push(p); return; } // (written out in full: tried first)
    key = norm(key);
    if (key.endsWith("。")) { key = key.slice(0, -1); p.out = out.replace(/[.。]$/, ""); }
    let src = "", best = "";
    key.split(/(\{[#@]?\w+\})/).forEach((part, n) => {
      if (n % 2) { const kind = /[#@]/.test(part[1]) ? part[1] : ""; p.names.push(part.slice(kind ? 2 : 1, -1)); p.kinds.push(kind); src += SLOT[kind]; return; }
      src += part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      const lit = part.trim(); p.lit += lit.length; if (lit.length > best.length) best = lit;
    });
    p.re = new RegExp("^" + src + "$");
    if (!best) { ANY.push(p); return; }
    const a = best.slice(0, 2);
    if (!BY.has(a)) BY.set(a, []);
    BY.get(a).push(p);
  }

  function fmtWhen(v) {
    const m = TIME.exec(v);
    if (!m) return EXACT.get(v) ?? v;
    return JA ? v : `${MONTHS[m[1] - 1] || m[1]} ${m[2]}${m[3] ? ", " + m[3] : ""}`;
  }
  const sub = (v, depth) => CJK.test(v) ? one(norm(v), depth + 1) ?? v : v;
  function fill(p, m, depth) {
    if (p.prev) fleeting = true;
    return p.out.replace(/\{(\w+)(?:\|([^}]*))?\}/g, (all, name, how) => {
      let v, kind = "";
      if (name === "_prev") v = PREV ? PREV() : "";
      else { const i = p.names.length ? p.names.indexOf(name) : +name - 1; if (i < 0 || m[i + 1] === undefined) return all; v = m[i + 1]; kind = p.kinds[i] || ""; }
      if (how === "raw") return v;
      if (how === "s") return +String(v).replace(/,/g, "") === 1 ? "" : "s";
      if (how && how.startsWith("pl:")) { const f = how.split(":"); return +String(v).replace(/,/g, "") === 1 ? f[1] : f[2]; }
      if (how === "list") return v.split("、").map(x => sub(x.trim(), depth)).join(COMMA);
      if (how === "lc") { v = sub(v, depth); return /^[A-Z][a-z]/.test(v) ? v[0].toLowerCase() + v.slice(1) : v; }
      return kind === "#" ? v : kind === "@" ? fmtWhen(v) : sub(v, depth);
    });
  }
  function viaPatterns(k, depth) {
    if (k.length > 400) return null;
    let cands = ANY;
    for (let i = 0; i < k.length; i++) {
      const a = BY.get(k.substr(i, 2)), b = BY.get(k[i]);
      if (a || b) { if (cands === ANY) cands = ANY.slice(); if (a) cands.push(...a); if (b) cands.push(...b); }
    }
    if (cands.length > 1) cands = [...new Set(cands)].sort((x, y) => y.lit - x.lit || x.i - y.i); // the one that says most first
    for (const p of cands) { const m = p.re.exec(k); if (m) return fill(p, m, depth); }
    return null;
  }
  // each part looked up by itself; null when none of them is known (all: unless every one is)
  function parts(list, sep, depth, all) {
    let hit = false; const out = [];
    for (const x of list) {
      const k = x.trim(); if (!k) continue;
      const r = CJK.test(k) ? one(k, depth + 1, !all) : null;
      if (r !== null) hit = true; else if (all && CJK.test(k)) return null;
      out.push(r ?? k);
    }
    return hit ? out.join(sep) : null;
  }
  // what follows a known label and is not known itself: names, which keep their words and take our commas
  const names = (v, depth) => v.indexOf("、") < 0 || /[，。；：]/.test(v) ? v : v.split("、").map(x => sub(x.trim(), depth)).join(COMMA);
  function viaParts(k, depth) {
    if (depth > 4) return null;
    let m;
    if (k.indexOf("。") >= 0) return parts(k.split("。"), STOP, depth);
    if ((m = /^([^：；，。]{1,24})：([\s\S]*)$/.exec(k))) {
      const head = CJK.test(m[1]) ? one(m[1].trim(), depth + 1) : m[1].trim(), rest = m[2].trim();
      if (head !== null) return head + (rest ? COLON + (CJK.test(rest) ? one(rest, depth + 1, true) ?? names(rest, depth) : rest) : COLON.trimEnd());
    }
    if (k.indexOf("；") >= 0) return parts(k.split("；"), SEMI, depth);
    if (k.indexOf("\u3000") >= 0) return parts(k.split("\u3000"), WIDE, depth);
    if (k.indexOf(" · ") >= 0) return parts(k.split(" · "), " · ", depth);
    if (k.indexOf(" + ") >= 0) return parts(k.split(" + "), " + ", depth);
    if (k.indexOf("、") >= 0) return parts(k.split("、"), COMMA, depth, true);
    if ((m = /^([\s\S]*?)（([^（）]*)）$/.exec(k))) {
      const head = m[1] && CJK.test(m[1]) ? one(m[1].trim(), depth + 1) : m[1].trim();
      if (head !== null) return head + OPEN + sub(m[2], depth) + CLOSE;
    }
    if (k.indexOf("，") >= 0) return parts(k.split("，"), COMMA, depth, true);
    return null;
  }
  // one line, its whitespace collapsed: the translation, or null. told: a miss is worth noting (debugging)
  function one(k, depth, told) {
    depth = depth || 0;
    let r = EXACT.get(k);
    if (r !== undefined) return r;
    if ((r = CACHE.get(k)) !== undefined) return r;
    if (depth > 6) return null;
    const end = k.length > 1 && k.endsWith("。"), body = end ? k.slice(0, -1) : k, was = fleeting;
    fleeting = false;
    r = end ? EXACT.get(body) : undefined;
    const flat = body.indexOf("\u3000") < 0 ? body : body.replace(/\u3000/g, " "); // as an entry would write it
    if (r === undefined && flat !== body) r = EXACT.get(flat);
    // pieces that are each known go one by one: a pattern must not swallow several of them into one capture
    for (const [sep, out] of STRONG) if (r === undefined && body.indexOf(sep) >= 0) r = parts(body.split(sep), out, depth, true) ?? undefined;
    if (r === undefined) r = TIME.test(flat) ? fmtWhen(flat) : viaPatterns(flat, depth) ?? viaParts(body, depth);
    if (end && r !== null && !/[.!?。！？…:：]$/.test(r)) r += STOP.trimEnd();
    if (!fleeting) { if (CACHE.size > 8000) CACHE.clear(); CACHE.set(k, r); }
    fleeting = fleeting || was;
    if (r === null && I.misses && (!depth || told)) I.misses.set(k, (I.misses.get(k) || 0) + 1);
    return r;
  }
  // a text as the page has it (lines, indentation): null when nothing in it is known
  function tr(s) {
    if (!CJK.test(s)) return null;
    if (s.indexOf("\n") < 0) {
      let r = RAW.get(s);
      if (r !== undefined) return r; // (a grid says the same few things thousands of times)
      const k = norm(s); if (!k) return null;
      const was = fleeting; fleeting = false;
      r = one(k);
      if (r !== null && /^\s|\s$/.test(s)) r = s.match(/^\s*/)[0] + r + s.match(/\s*$/)[0];
      if (!fleeting) { if (RAW.size > 8000) RAW.clear(); RAW.set(s, r); }
      fleeting = fleeting || was;
      return r;
    }
    let hit = false;
    const out = s.split("\n").map(line => { const r = tr(line); if (r === null) return line; hit = true; return r; });
    return hit ? out.join("\n") : null;
  }
  I.t = s => (typeof s === "string" && tr(s)) || s;

  // ---------- the page ----------
  const ATTRS = ["title", "placeholder", "aria-label", "alt"];
  const SKIP = { SCRIPT: 1, STYLE: 1, TEXTAREA: 1, PRE: 1, svg: 1 };
  // classes that only ever hold data. First group: nothing inside is looked at; second: their own text is not
  const OFF = /(?:^|\s)(?:(desc|upnotes|wn)|(zh|bname|wltitle|base|av|pname|mopt|ph))(?:\s|$)/;
  const TEXTS = new WeakMap(), ATS = new WeakMap(); // what was written here, so it is not taken for Chinese again
  // 0: translate, 1: its own text is data, 2: all of it is
  function offOf(el) {
    if (SKIP[el.nodeName]) return 2;
    const c = el.className;
    let o = 0;
    if (c) { if (typeof c !== "string") return 2; const m = OFF.exec(c); if (m) { if (m[1]) return 2; o = 1; } }
    const d = el.getAttribute("data-i18n");
    return d ? (d === "off" ? 2 : 1) : o;
  }
  function inOff(el) { for (; el && el.nodeType === 1; el = el.parentNode) if (offOf(el) === 2) return true; return false; }
  I.skips = n => { const p = n.nodeType === 3 ? n.parentNode : n; return !p || p.nodeType !== 1 || (n === p && p.nodeName === "TEXTAREA" ? inOff(p.parentNode) : offOf(p) > 0 || inOff(p)); }; // (asked by test/i18n_crawl.py)
  function text(n) {
    const s = n.nodeValue;
    if (!s || !CJK.test(s) || TEXTS.get(n) === s) return;
    PREV = () => { const p = n.previousElementSibling || (n.parentNode && n.parentNode.previousElementSibling); return p ? p.textContent.trim() : ""; };
    const r = tr(s); PREV = null;
    if (r === null || r === s) return;
    const p = n.parentNode;
    // an option without a value answers with its text: it goes on answering with what the program wrote
    if (p && p.nodeName === "OPTION" && !p.hasAttribute("value")) p.setAttribute("value", p.text);
    n.nodeValue = r; TEXTS.set(n, r);
  }
  // one attribute; live: asked only once something Chinese is found, whether this element is ours to translate
  function attr(el, name, live) {
    const v = el.getAttribute(name);
    if (!v || !CJK.test(v)) return;
    let done = ATS.get(el);
    if ((done && done[name] === v) || (live && !live(el))) return;
    const r = tr(v); if (r === null || r === v) return;
    if (!done) ATS.set(el, done = {});
    done[name] = r; el.setAttribute(name, r);
  }
  function attrs(el, live) {
    attr(el, "title", live); attr(el, "placeholder", live); attr(el, "aria-label", live); attr(el, "alt", live);
    if (el.nodeName === "INPUT" && /^(button|submit|reset)$/.test(el.type)) attr(el, "value", live);
  }
  // An element and all in it. The browser is asked for the text nodes and for the elements with a tooltip or a
  // hint, so that nothing else is touched: going through every element of 5,000 cards takes twice as long.
  const HAVE = "[title],[placeholder],[aria-label],[alt],input[type=button],input[type=submit],input[type=reset]";
  function walk(root) {
    const top = offOf(root);
    if (top === 2) { if (root.nodeName === "TEXTAREA") attrs(root); return; } // (what is typed is the player's, the hint in it is ours)
    const st = new Map([[root, top]]); // offOf, with what is above taken in: asked of few elements, and once each
    const state = el => { let v = st.get(el); if (v === undefined) { v = offOf(el); if (v !== 2 && state(el.parentNode) === 2) v = 2; st.set(el, v); } return v; };
    // (where the text is data, the tooltip is the same data in full)
    const live = el => el.nodeName === "TEXTAREA" ? state(el.parentNode) !== 2 : !state(el);
    if (!top) attrs(root);
    for (const el of root.querySelectorAll(HAVE)) attrs(el, live);
    const w = document.createTreeWalker(root, 4); // text nodes
    for (let n = w.nextNode(); n; n = w.nextNode()) { const v = n.nodeValue; if (v && CJK.test(v) && !state(n.parentNode)) text(n); }
  }
  function seen(recs) {
    let last = null, lastOff = false; // (a grid's cards arrive one record each, all under the same element)
    for (const r of recs) {
      const t = r.target;
      if (r.type === "characterData") { const p = t.parentNode; if (p && p.nodeType === 1 && !p.isContentEditable && !offOf(p) && !inOff(p.parentNode)) text(t); continue; }
      const box = t.nodeName === "TEXTAREA";
      if (t.nodeType !== 1) continue;
      if (t !== last) { last = t; lastOff = inOff(box ? t.parentNode : t); }
      if (lastOff) continue;
      if (r.type === "attributes") { if (box || !offOf(t)) attrs(t); continue; }
      if (box) continue;
      const own = !offOf(t) && !t.isContentEditable;
      if (r.addedNodes.length > 20) { walk(t); continue; } // a grid filled at once: one pass (what was there before has nothing Chinese left)
      for (const n of r.addedNodes) {
        if (!n.isConnected) continue;
        if (n.nodeType === 1) walk(n); else if (n.nodeType === 3 && own) text(n);
      }
    }
    mo.takeRecords(); // what was just written here is not news
  }
  const mo = new MutationObserver(seen);
  function start() {
    I.on = true;
    try { if (localStorage.getItem("vrclib.i18n.debug")) I.misses = new Map(); } catch (e) {}
    walk(document.documentElement);
    mo.observe(document, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ATTRS.concat("value") });
    for (const k of ["alert", "confirm", "prompt"]) { const f = window[k]; window[k] = function (m, ...a) { return f.call(window, I.t(String(m ?? "")), ...a); }; }
  }
})();
