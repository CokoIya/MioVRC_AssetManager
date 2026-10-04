// Runs booth_scraper.js outside a browser: just enough of a page (elements, links, fetch) for it to read
// made-up library pages. Usage: node scraper_harness.js <booth_scraper.js>; prints one result per scenario.
const fs = require("fs");

class El {
  constructor(tag, attrs, kids) {
    this.tag = tag; this.attrs = attrs || {}; this.kids = [];
    for (const k of kids || []) { this.kids.push(k); if (k instanceof El) k.parentElement = this; }
    this.parentElement = null;
  }
  getAttribute(n) { return n in this.attrs ? this.attrs[n] : null; }
  get textContent() { return this.kids.map(k => k instanceof El ? k.textContent : k).join(" "); }
  all() { const out = []; for (const k of this.kids) if (k instanceof El) { out.push(k, ...k.all()); } return out; }
  contains(o) { return o === this || this.all().includes(o); }
  matches(sel) {
    return sel.split(",").some(s => {
      const m = s.trim().match(/^([a-z]*)((?:\.[\w-]+)*)((?:\[[^\]]+\])*)$/i);
      if (!m) return false;
      if (m[1] && m[1] !== this.tag) return false;
      for (const c of m[2].split(".").filter(Boolean)) if (!(" " + (this.attrs.class || "") + " ").includes(" " + c + " ")) return false;
      for (const a of m[3].match(/\[[^\]]+\]/g) || []) {
        const am = a.match(/^\[([\w-]+)(?:(\*?)=['"]?([^'"\]]*)['"]?)?\]$/);
        const v = this.getAttribute(am[1]);
        if (v === null) return false;
        if (am[3] !== undefined && (am[2] ? !v.includes(am[3]) : v !== am[3])) return false;
      }
      return true;
    });
  }
  querySelectorAll(sel) { return this.all().filter(e => e.matches(sel)); }
  querySelector(sel) { return this.querySelectorAll(sel)[0] || null; }
  get ownerDocument() {
    return { createTreeWalker: box => {
      const texts = []; (function walk(e) { for (const k of e.kids) k instanceof El ? walk(k) : texts.push({ nodeValue: k }); })(box);
      let i = 0; return { nextNode: () => texts[i++] || null };
    } };
  }
}

// a library page: its cards, and links to these pages
function page(cards, links) {
  const main = new El("main", {}, [
    ...cards.map(c => new El("div", { class: "card" }, [
      new El("a", { href: "https://booth.pm/ja/items/" + c.id }, [c.name]),
      new El("a", { href: "https://booth.pm/downloadables/" + c.id + "0" }, [c.name + ".zip"]),
    ])),
    ...links.map(n => new El("a", { href: "/library?page=" + n }, [String(n)])),
  ]);
  const body = new El("body", {}, [main]);
  return { body, querySelector: s => body.querySelector(s), querySelectorAll: s => body.querySelectorAll(s), documentElement: { outerHTML: "<html></html>" } };
}

const scenarios = {
  // two pages, as Booth shows them
  whole: { "/library?page=1": page([{ id: 1, name: "A" }, { id: 2, name: "B" }], [2]), "/library?page=2": page([{ id: 3, name: "C" }], [1]) },
  // the page in the middle comes back without cards
  emptyMiddle: { "/library?page=1": page([{ id: 1, name: "A" }], [2, 3]), "/library?page=2": page([], [1, 3]), "/library?page=3": page([{ id: 3, name: "C" }], [1, 2]) },
  // the second page keeps failing
  failedPage: { "/library?page=1": page([{ id: 1, name: "A" }], [2, 3]), "/library?page=2": 500, "/library?page=3": page([{ id: 3, name: "C" }], [1, 2]) },
  // a page that only repeats what was seen is not the end either
  repeats: { "/library?page=1": page([{ id: 1, name: "A" }], [2, 3]), "/library?page=2": page([{ id: 1, name: "A" }], [1, 3]), "/library?page=3": page([{ id: 3, name: "C" }], [1, 2]) },
};

(async () => {
  const src = fs.readFileSync(process.argv[2], "utf8");
  const out = {};
  for (const [name, pages] of Object.entries(scenarios)) {
    let parsed;
    globalThis.window = {};
    globalThis.location = { origin: "https://accounts.booth.pm" };
    globalThis.NodeFilter = { SHOW_TEXT: 4 };
    globalThis.setTimeout = f => setImmediate(f); // no real waiting between pages
    globalThis.DOMParser = class { parseFromString() { return parsed; } };
    globalThis.fetch = async p => {
      const pg = pages[p];
      if (typeof pg === "number") return { status: pg, ok: false, url: "https://accounts.booth.pm" + p, text: async () => "" };
      if (!pg) return { status: 404, ok: false, url: "https://accounts.booth.pm" + p, text: async () => "" };
      return { status: 200, ok: true, url: "https://accounts.booth.pm" + p, text: async () => { parsed = pg; return ""; } };
    };
    (0, eval)(src);
    await window.__vrclibRun();
    const r = window.__vrclibResult ? JSON.parse(window.__vrclibResult) : null;
    out[name] = { error: window.__vrclib.error || "", ids: r ? r.library.map(x => x.id).join(" ") : "", incomplete: r ? r.incomplete : "", downloads: r ? r.library.map(x => x.downloads.join("+")).join(" ") : "" };
  }
  console.log(JSON.stringify(out));
})();
