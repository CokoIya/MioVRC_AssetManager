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

// a page of the list of スキ！ (/wish_list_name_items.json), in the shape the live site gives
function likes(ids, next, total) {
  return { json: { items: ids.map(id => ({ id, name: "Like " + id, price: "¥ 1,000~", is_adult: false, category: { name: { ja: "3D衣装" } }, shop: { name: "Shop " + id, url: "https://s" + id + ".booth.pm/" },
    url: "https://booth.pm/ja/items/" + id, thumbnail_image_urls: ["https://booth.pximg.net/" + id + ".jpg"], tracking_data: { product_price: 1000 } })),
    pagination: { current_page: 1, prev_page: null, next_page: next, limit_value: 24, total_pages: 2, total_count: total } } };
}
const whole = { "/library?page=1": page([{ id: 1, name: "A" }, { id: 2, name: "B" }], [2]), "/library?page=2": page([{ id: 3, name: "C" }], [1]) };

const scenarios = {
  // two pages, as Booth shows them
  whole,
  // the page in the middle comes back without cards
  emptyMiddle: { "/library?page=1": page([{ id: 1, name: "A" }], [2, 3]), "/library?page=2": page([], [1, 3]), "/library?page=3": page([{ id: 3, name: "C" }], [1, 2]) },
  // the second page keeps failing
  failedPage: { "/library?page=1": page([{ id: 1, name: "A" }], [2, 3]), "/library?page=2": 500, "/library?page=3": page([{ id: 3, name: "C" }], [1, 2]) },
  // a page that only repeats what was seen is not the end either
  repeats: { "/library?page=1": page([{ id: 1, name: "A" }], [2, 3]), "/library?page=2": page([{ id: 1, name: "A" }], [1, 3]), "/library?page=3": page([{ id: 3, name: "C" }], [1, 2]) },
  // the items marked スキ！, two pages of them (one twice), when the wish list wants them
  likes: Object.assign({ _opts: { likes: true }, "/wish_list_name_items.json?page=1": likes([11, 12], 2, 3), "/wish_list_name_items.json?page=2": likes([12, 13], null, 3) }, whole),
  // …not asked for: not read
  noLikes: Object.assign({ _opts: {}, "/wish_list_name_items.json?page=1": likes([11], null, 1) }, whole),
  // …a list in another shape, or a page that is not JSON: said, and the purchases are kept
  likesShape: Object.assign({ _opts: { likes: true }, "/wish_list_name_items.json?page=1": { json: { item_ids: [], wishlists_counts: {} } } }, whole),
  likesHTML: Object.assign({ _opts: { likes: true }, "/wish_list_name_items.json?page=1": page([], []) }, whole),
  // …refused: said as such, not as a lost login
  likes403: Object.assign({ _opts: { likes: true }, "/wish_list_name_items.json?page=1": 403 }, whole),
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
    const asked = [];
    globalThis.fetch = async p => {
      const pg = pages[p];
      asked.push(p);
      const notJSON = async () => { throw new SyntaxError("Unexpected token <"); };
      if (typeof pg === "number") return { status: pg, ok: false, url: "https://accounts.booth.pm" + p, text: async () => "", json: notJSON };
      if (!pg) return { status: 404, ok: false, url: "https://accounts.booth.pm" + p, text: async () => "", json: notJSON };
      if (pg.json) return { status: 200, ok: true, url: "https://accounts.booth.pm" + p, text: async () => JSON.stringify(pg.json), json: async () => pg.json };
      return { status: 200, ok: true, url: "https://accounts.booth.pm" + p, text: async () => { parsed = pg; return ""; }, json: notJSON };
    };
    (0, eval)(src);
    await window.__vrclibRun(pages._opts);
    const r = window.__vrclibResult ? JSON.parse(window.__vrclibResult) : null;
    out[name] = { error: window.__vrclib.error || "", ids: r ? r.library.map(x => x.id).join(" ") : "", incomplete: r ? r.incomplete : "", downloads: r ? r.library.map(x => x.downloads.join("+")).join(" ") : "",
      likes: r && r.likes ? r.likes.map(x => [x.id, x.name, x.price, x.shop, x.shopUrl, x.thumb].join("|")).join(" ") : "", likesTotal: r ? r.likesTotal || 0 : 0,
      likesRead: !!(r && r.likesRead), likesError: r ? r.likesError || "" : "", askedLikes: asked.filter(x => x.includes("wish_list")).length };
  }
  console.log(JSON.stringify(out));
})();
