// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// The community in a browser: the program's /api/community/* routes (internal/social, with the rules of
// internal/socialapi) ported to the page, so that web/social.js runs where there is no program.
// window.WebCommunity.call(route, body) answers what the Go route answers for that body: the same checks and
// refusals, the same requests to the service and the same answer. The service is this page's own origin (api/v1/
// next to the page). The parts follow the Go files, whose names head them, so that the two can be read side by side.
// What a browser does not have is answered as the program answers when it is empty: no library (nothing owned, no
// library cards, no bases of its own), no files of the program's data folder (the account and community.json are
// kept in localStorage instead), no Windows notifications, no modding basket. Each such place says so.
"use strict";
(function () {

  // ---------- Go's strings, unicode and unicode/utf8, as the rules use them ----------

  const utf8In = new TextEncoder();
  const utf8Out = new TextDecoder("utf-8", { ignoreBOM: true });

  // a string as Go holds one read from JSON: a lone surrogate is U+FFFD
  function goStr(s) {
    return s.replace(/[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/g, "\ufffd");
  }
  // len(s): its bytes in UTF-8
  function blen(s) { return utf8In.encode(s).length; }
  // utf8.RuneCountInString
  function runes(s) { let n = 0; for (const _ of s) n++; return n; }
  // unicode.IsSpace
  function isSpace(r) {
    return r === 9 || r === 10 || r === 11 || r === 12 || r === 13 || r === 32 || r === 0x85 || r === 0xa0 || r === 0x1680 ||
      r >= 0x2000 && r <= 0x200a || r === 0x2028 || r === 0x2029 || r === 0x202f || r === 0x205f || r === 0x3000;
  }
  // unicode.IsControl (C0, DEL and C1)
  function isControl(r) { return r < 0x20 || r >= 0x7f && r < 0xa0; }
  // strings.TrimSpace
  function trimSpace(s) {
    const a = [...s];
    let i = 0, j = a.length;
    while (i < j && isSpace(a[i].codePointAt(0))) i++;
    while (j > i && isSpace(a[j - 1].codePointAt(0))) j--;
    return i === 0 && j === a.length ? s : a.slice(i, j).join("");
  }
  // socialapi's asciiLower: A–Z in lower case and nothing else changed
  function asciiLower(s) { return s.replace(/[A-Z]/g, c => c.toLowerCase()); }
  // strings.ToLower, rune by rune (U+0130 is "i" and Σ is σ there, whatever follows)
  function goLower(s) {
    let out = "";
    for (const c of s) {
      const l = c === "\u0130" ? "i" : c === "\u03a3" ? "\u03c3" : c.toLowerCase();
      out += [...l].length === 1 ? l : c;
    }
    return out;
  }
  // strings.EqualFold: rune by rune under simple case folding
  function equalFold(a, b) {
    const x = [...a], y = [...b];
    if (x.length !== y.length) return false;
    const one = (s, f) => { const t = f(s); return [...t].length === 1 ? t : s; };
    for (let i = 0; i < x.length; i++) {
      const p = x[i], q = y[i];
      if (p === q) continue;
      const lp = p === "\u212a" ? "k" : p === "\u017f" ? "s" : one(p, s => s.toLowerCase());
      const lq = q === "\u212a" ? "k" : q === "\u017f" ? "s" : one(q, s => s.toLowerCase());
      if (lp === lq || one(p, s => s.toUpperCase()) === one(q, s => s.toUpperCase())) continue;
      return false;
    }
    return true;
  }
  function firstOf(...s) { for (const x of s) if (x) return x; return ""; }
  function contains(list, s) { return (list || []).includes(s); }
  function countOf(s, sub) { return s.split(sub).length - 1; }
  // a string as its UTF-8 bytes, one character a byte (what Go's url package works on), and back
  function toBytes(s) {
    const u = utf8In.encode(s);
    let out = "";
    for (let i = 0; i < u.length; i += 8192) out += String.fromCharCode.apply(null, u.subarray(i, i + 8192));
    return out;
  }
  function fromBytes(b) {
    const u = new Uint8Array(b.length);
    for (let i = 0; i < b.length; i++) u[i] = b.charCodeAt(i);
    return utf8Out.decode(u);
  }

  // ---------- Go's net/url (Go 1.24 semantics: go.mod says go 1.24) ----------
  // Bytes in, bytes out: a URL's parts are byte strings here as in Go, and urlString gives a string back.

  const ENC = { path: 1, seg: 2, host: 4, zone: 8, user: 16, query: 32, frag: 64 };
  const ENC_TABLE = new Uint8Array(256);
  (function () {
    const all = 127, set = (chars, modes) => { for (const c of chars) ENC_TABLE[c.charCodeAt(0)] = modes; };
    for (let c = 0; c < 256; c++) {
      if (c >= 48 && c <= 57 || c >= 65 && c <= 90 || c >= 97 && c <= 122) ENC_TABLE[c] = all;
    }
    const E = ENC;
    set("!", E.frag | E.zone | E.host);
    set("\"'<>[]", E.zone | E.host);
    set("$&+=", E.frag | E.user | E.zone | E.host | E.seg | E.path);
    set("()*", E.frag | E.zone | E.host);
    set(",;", E.frag | E.user | E.zone | E.host | E.path);
    set("-._~", all);
    set("/", E.frag | E.path);
    set(":", E.frag | E.zone | E.host | E.seg | E.path);
    set("?", E.frag);
    set("@", E.frag | E.seg | E.path);
  })();
  function shouldEscape(c, mode) { return (ENC_TABLE[c] & mode) === 0; }
  function ishex(c) { return c >= 48 && c <= 57 || c >= 97 && c <= 102 || c >= 65 && c <= 70; }
  function unhex(c) { return c <= 57 ? c - 48 : c <= 70 ? c - 55 : c - 87; }
  // unescape: null where Go gives an error
  function urlUnescape(s, mode) {
    let n = 0, hasPlus = false;
    for (let i = 0; i < s.length;) {
      const c = s.charCodeAt(i);
      if (c === 37) {
        n++;
        if (i + 2 >= s.length || !ishex(s.charCodeAt(i + 1)) || !ishex(s.charCodeAt(i + 2))) return null;
        if (mode === ENC.host && unhex(s.charCodeAt(i + 1)) < 8 && s.slice(i, i + 3) !== "%25") return null;
        i += 3;
      } else if (c === 43) {
        hasPlus = mode === ENC.query;
        i++;
      } else {
        if ((mode === ENC.host || mode === ENC.zone) && c < 0x80 && shouldEscape(c, mode)) return null;
        i++;
      }
    }
    if (n === 0 && !hasPlus) return s;
    let t = "";
    for (let i = 0; i < s.length; i++) {
      const c = s.charCodeAt(i);
      if (c === 37) {
        t += String.fromCharCode(unhex(s.charCodeAt(i + 1)) << 4 | unhex(s.charCodeAt(i + 2)));
        i += 2;
      } else if (c === 43) t += mode === ENC.query ? " " : "+";
      else t += s[i];
    }
    return t;
  }
  function urlEscape(s, mode) {
    let t = "";
    for (let i = 0; i < s.length; i++) {
      const c = s.charCodeAt(i);
      if (c === 32 && mode === ENC.query) t += "+";
      else if (shouldEscape(c, mode)) t += "%" + "0123456789ABCDEF"[c >> 4] + "0123456789ABCDEF"[c & 15];
      else t += s[i];
    }
    return t;
  }
  function validEncoded(s, mode) {
    for (let i = 0; i < s.length; i++) {
      if ("!$&'()*+,;=:@[]%".includes(s[i])) continue;
      if (shouldEscape(s.charCodeAt(i), mode)) return false;
    }
    return true;
  }
  // url.QueryEscape, url.PathEscape (strings in, strings out)
  function queryEscape(s) { return urlEscape(toBytes(s), ENC.query); }
  function pathEscape(s) { return urlEscape(toBytes(s), ENC.seg); }
  // url.Values{…}.Encode(): the keys sorted, each value escaped
  function valuesEncode(v) {
    const parts = [];
    for (const k of Object.keys(v).sort((a, b) => toBytes(a) < toBytes(b) ? -1 : toBytes(a) > toBytes(b) ? 1 : 0)) {
      for (const x of [].concat(v[k])) parts.push(queryEscape(k) + "=" + queryEscape(x));
    }
    return parts.join("&");
  }
  function validOptionalPort(port) {
    if (port === "") return true;
    if (port[0] !== ":") return false;
    for (const c of port.slice(1)) if (c < "0" || c > "9") return false;
    return true;
  }
  function splitHostPort(hostPort) {
    let host = hostPort, port = "";
    const colon = host.lastIndexOf(":");
    if (colon !== -1 && validOptionalPort(host.slice(colon))) { port = host.slice(colon + 1); host = host.slice(0, colon); }
    if (host.startsWith("[") && host.endsWith("]")) host = host.slice(1, -1);
    return [host, port];
  }
  // url.Parse: null where Go gives an error. (A bracketed IP literal is refused here: whatever takes the URL then
  // wants a name on the internet, which it never is.)
  function parseURL(raw) {
    raw = toBytes(raw);
    const hash = raw.indexOf("#");
    const u = parseRest(hash < 0 ? raw : raw.slice(0, hash));
    if (!u || hash < 0) return u;
    const frag = raw.slice(hash + 1);
    if (frag === "") return u;
    const f = urlUnescape(frag, ENC.frag);
    if (f === null) return null;
    u.fragment = f;
    u.rawFragment = urlEscape(f, ENC.frag) === frag ? "" : frag;
    return u;
  }
  function parseRest(raw) {
    for (let i = 0; i < raw.length; i++) { const c = raw.charCodeAt(i); if (c < 32 || c === 0x7f) return null; }
    const url = { scheme: "", opaque: "", user: false, host: "", path: "", rawPath: "", omitHost: false, forceQuery: false,
      rawQuery: "", fragment: "", rawFragment: "" };
    if (raw === "*") { url.path = "*"; return url; }
    let rest = raw;
    for (let i = 0; i < raw.length; i++) {
      const c = raw[i];
      if (c >= "a" && c <= "z" || c >= "A" && c <= "Z") continue;
      if (c >= "0" && c <= "9" || c === "+" || c === "-" || c === ".") { if (i === 0) break; continue; }
      if (c === ":") {
        if (i === 0) return null;
        url.scheme = raw.slice(0, i).toLowerCase();
        rest = raw.slice(i + 1);
      }
      break;
    }
    if (rest.endsWith("?") && countOf(rest, "?") === 1) { url.forceQuery = true; rest = rest.slice(0, -1); }
    else { const q = rest.indexOf("?"); if (q >= 0) { url.rawQuery = rest.slice(q + 1); rest = rest.slice(0, q); } }
    if (!rest.startsWith("/")) {
      if (url.scheme !== "") { url.opaque = rest; return url; }
      if (rest.split("/")[0].includes(":")) return null;
    }
    if ((url.scheme !== "" || !rest.startsWith("///")) && rest.startsWith("//")) {
      let authority = rest.slice(2);
      rest = "";
      const slash = authority.indexOf("/");
      if (slash >= 0) { rest = authority.slice(slash); authority = authority.slice(0, slash); }
      const at = authority.lastIndexOf("@");
      const host = parseHost(url.scheme, at < 0 ? authority : authority.slice(at + 1));
      if (host === null) return null;
      if (at >= 0) {
        // (userinfo: refused by every caller here; only its form decides whether Go refuses the whole URL)
        const info = authority.slice(0, at);
        if (!/^[A-Za-z0-9\-._:~!$&'()*+,;=%@]*$/.test(info)) return null;
        for (const part of info.includes(":") ? [info.slice(0, info.indexOf(":")), info.slice(info.indexOf(":") + 1)] : [info]) {
          if (urlUnescape(part, ENC.user) === null) return null;
        }
        url.user = true;
      }
      url.host = host;
    } else if (url.scheme !== "" && rest.startsWith("/")) url.omitHost = true;
    const path = urlUnescape(rest, ENC.path);
    if (path === null) return null;
    url.path = path;
    url.rawPath = urlEscape(path, ENC.path) === rest ? "" : rest;
    return url;
  }
  function parseHost(scheme, host) {
    const open = host.lastIndexOf("[");
    if (open >= 0) return null;
    const i = host.indexOf(":");
    if (i !== -1) {
      const last = host.lastIndexOf(":");
      if (!validOptionalPort(host.slice(last !== i ? last : i))) return null;
    }
    return urlUnescape(host, ENC.host);
  }
  function urlHostname(u) { return splitHostPort(u.host)[0]; }
  function urlPort(u) { return splitHostPort(u.host)[1]; }
  function escapedPath(u) {
    if (u.rawPath !== "" && validEncoded(u.rawPath, ENC.path)) {
      const p = urlUnescape(u.rawPath, ENC.path);
      if (p !== null && p === u.path) return u.rawPath;
    }
    if (u.path === "*") return "*";
    return urlEscape(u.path, ENC.path);
  }
  function escapedFragment(u) {
    if (u.rawFragment !== "" && validEncoded(u.rawFragment, ENC.frag)) {
      const f = urlUnescape(u.rawFragment, ENC.frag);
      if (f !== null && f === u.fragment) return u.rawFragment;
    }
    return urlEscape(u.fragment, ENC.frag);
  }
  // (*url.URL).String for what parseHTTPS lets through (a scheme, a host, no user, no opaque part)
  function urlString(u) {
    let s = u.scheme ? u.scheme + ":" : "";
    if (u.scheme !== "" || u.host !== "") {
      if (!(u.omitHost && u.host === "")) {
        if (u.host !== "" || u.path !== "") s += "//";
        if (u.host !== "") s += urlEscape(u.host, ENC.host);
      }
    }
    let path = escapedPath(u);
    if (u.omitHost && u.host === "" && path.startsWith("//")) { s += "%2F"; path = path.slice(1); }
    if (path !== "" && path[0] !== "/" && u.host !== "") s += "/";
    s += path;
    if (u.forceQuery || u.rawQuery !== "") s += "?" + u.rawQuery;
    if (u.fragment !== "") s += "#" + escapedFragment(u);
    return fromBytes(s);
  }
  // (*url.URL).Query().Get(k)
  function queryGet(u, k) {
    for (const pair of u.rawQuery.split("&")) {
      if (pair === "" || pair.includes(";")) continue;
      const eq = pair.indexOf("=");
      const key = urlUnescape(eq < 0 ? pair : pair.slice(0, eq), ENC.query);
      if (key === null) continue;
      const value = urlUnescape(eq < 0 ? "" : pair.slice(eq + 1), ENC.query);
      if (value === null) continue;
      if (key === toBytes(k)) return fromBytes(value);
    }
    return "";
  }

  // ---------- Go's encoding/json for the types of socialapi and social ----------
  // A Go value is a plain object holding every field of its struct (embedded structs' fields flattened, as JSON has
  // them; "$Name" says whether an embedded pointer is set). decode is json.Unmarshal into a fresh value, marshal is
  // json.Marshal: omitempty, nil slices and pointers as null, embedded fields promoted by Go's rules.

  const TYPES = {};
  // struct("Name", "json type; json type?; =Embedded; =*EmbeddedPointer"): "?" is omitempty; types are string,
  // int, bool, []T, *T, {}T (a map of string to T) and struct names
  function struct(name, spec) {
    TYPES[name] = { fields: spec.split(";").map(s => s.trim()).filter(Boolean).map(s => {
      if (s[0] === "=") return s[1] === "*" ? { embed: s.slice(2), ptr: true } : { embed: s.slice(1), ptr: false };
      const [n, t] = s.split(/\s+/);
      return t.endsWith("?") ? { name: n, type: t.slice(0, -1), omit: true } : { name: n, type: t, omit: false };
    }) };
  }
  function kindOf(t) {
    if (t.startsWith("[]")) return ["slice", t.slice(2)];
    if (t[0] === "*") return ["ptr", t.slice(1)];
    if (t.startsWith("{}")) return ["map", t.slice(2)];
    if (t === "string" || t === "int" || t === "bool") return [t];
    return ["struct", t];
  }
  // the fields JSON sees: the shallowest of each name wins; two at the same depth hide each other
  function fieldsOf(name) {
    const t = TYPES[name];
    if (!t) throw new Error("adapter: no type " + name);
    if (t.eff) return t.eff;
    const found = [];
    (function walk(n, depth, via, ord) {
      TYPES[n].fields.forEach((f, i) => {
        if (f.embed) walk(f.embed, depth + 1, f.ptr ? via.concat(f.embed) : via, ord.concat(i));
        else found.push(Object.assign({ depth, via, ord: ord.concat(i) }, f));
      });
    })(name, 0, [], []);
    const byName = new Map();
    for (const f of found) { if (!byName.has(f.name)) byName.set(f.name, []); byName.get(f.name).push(f); }
    const eff = [];
    for (const list of byName.values()) {
      const d = Math.min(...list.map(f => f.depth)), top = list.filter(f => f.depth === d);
      if (top.length === 1) eff.push(top[0]);
    }
    const cmp = (a, b) => { for (let i = 0; i < Math.min(a.length, b.length); i++) if (a[i] !== b[i]) return a[i] - b[i]; return a.length - b.length; };
    eff.sort((a, b) => cmp(a.ord, b.ord));
    t.eff = eff;
    t.exact = new Map(eff.map(f => [f.name, f]));
    t.lower = new Map(eff.map(f => [f.name.toLowerCase(), f]));
    t.ptrs = [...new Set(found.flatMap(f => f.via))];
    return eff;
  }
  function isObj(v) { return v !== null && typeof v === "object" && !Array.isArray(v); }
  function zero(t) {
    const [k, e] = kindOf(t);
    if (k === "string") return "";
    if (k === "int") return 0;
    if (k === "bool") return false;
    if (k !== "struct") return null;
    const o = {};
    for (const f of fieldsOf(e)) o[f.name] = zero(f.type);
    for (const p of TYPES[e].ptrs) o["$" + p] = false;
    return o;
  }
  // json.Unmarshal of v into a fresh value of type t; ctx.bad is set where Go would answer an error
  function decode(t, v, ctx) {
    const [k, e] = kindOf(t);
    if (v === null || v === undefined) return zero(t);
    switch (k) {
      case "string": if (typeof v === "string") return goStr(v); break;
      case "int": if (typeof v === "number" && Number.isInteger(v)) return v; break;
      case "bool": if (typeof v === "boolean") return v; break;
      case "slice": if (Array.isArray(v)) return v.map(x => decode(e, x, ctx)); break;
      case "ptr": return decode(e, v, ctx);
      case "map":
        if (isObj(v)) { const o = {}; for (const key of Object.keys(v)) o[goStr(key)] = decode(e, v[key], ctx); return o; }
        break;
      case "struct": if (isObj(v)) return decodeStruct(e, v, ctx); break;
    }
    ctx.bad = true;
    return zero(t);
  }
  function fieldFor(t, key) { return t.exact.get(key) || t.lower.get(key.toLowerCase()); }
  function decodeStruct(name, v, ctx) {
    const o = zero(name), t = TYPES[name];
    fieldsOf(name);
    // socialapi.Notice.UnmarshalJSON: "from" may be one person (older snapshots); a count of 0 is the people named
    let from;
    for (const key of Object.keys(v)) {
      const f = fieldFor(t, key);
      if (!f) continue;
      if (name === "Notice" && f.name === "from") { from = v[key]; continue; }
      for (const p of f.via) o["$" + p] = true;
      const x = v[key];
      if (x === null) {
        if (["slice", "ptr", "map"].includes(kindOf(f.type)[0])) o[f.name] = null;
        continue;
      }
      o[f.name] = decode(f.type, x, ctx);
    }
    if (name === "Notice") {
      if (isObj(from)) o.from = [decode("Person", from, ctx)];
      else if (from !== undefined) o.from = decode("[]Person", from, ctx);
      if (o.count === 0) o.count = (o.from || []).length;
    }
    return o;
  }
  function isEmpty(t, x) {
    switch (kindOf(t)[0]) {
      case "string": return x === "";
      case "int": return x === 0;
      case "bool": return x === false;
      case "slice": return x === null || x.length === 0;
      case "ptr": return x === null;
      case "map": return x === null || Object.keys(x).length === 0;
    }
    return false;
  }
  function marshal(t, v) {
    const [k, e] = kindOf(t);
    if (v === undefined) v = zero(t);
    switch (k) {
      case "string": return typeof v === "string" ? v : "";
      case "int": return typeof v === "number" ? v : 0;
      case "bool": return v === true;
      case "slice": return v === null ? null : v.map(x => marshal(e, x));
      case "ptr": return v === null ? null : marshal(e, v);
      case "map": {
        if (v === null) return null;
        const o = {};
        for (const key of Object.keys(v)) o[key] = marshal(e, v[key]);
        return o;
      }
    }
    const o = {};
    for (const f of fieldsOf(e)) {
      if (f.via.some(p => !v["$" + p])) continue;
      const x = v[f.name] === undefined ? zero(f.type) : v[f.name];
      if (f.omit && isEmpty(f.type, x)) continue;
      o[f.name] = marshal(f.type, x);
    }
    return o;
  }
  // Go's json.Marshal length (it escapes <, > and & and the line separators)
  function goJSONLen(v) {
    return blen(JSON.stringify(v).replace(/[<>&\u2028\u2029]/g, c => "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0")));
  }

  // socialapi (types.go, v1.go, groups.go, creator.go, asks.go)
  struct("Link", "kind string; url string; label string?");
  struct("Birthday", "month int; day int");
  struct("CardReq", "showcase []ShowcaseItem?; nickname string; bio string?; langs []string?; vrchat string?; links []Link?; " +
    "bases []string?; birthday *Birthday?; following []string?; avatar string?; contacts *Contacts?");
  struct("Card", "showcase []ShowcaseItem?; login string; nickname string; bio string?; langs []string?; vrchat string?; " +
    "links []Link?; bases []string?; birthday *Birthday?; following []string?; avatar string?; issue int; updated string");
  struct("CreatorEntry", "login string; nickname string; avatar string?; bases []string?; langs []string?; birthday *Birthday?; " +
    "followers int; works int; recipes int?; updated string");
  struct("Creators", "version int; updated string; creators []CreatorEntry");
  struct("ShowcaseItem", "booth_id string; name string; shop string?");
  struct("WorkAsset", "name string; url string?; shop string?");
  struct("WorkReq", "based_on string?; title string; desc string?; video string?; base string?; assets []WorkAsset?; " +
    "links []Link?; recipe string?; rights_ok bool?; cover string?; replace string?");
  struct("Work", "based_on string?; id string; author string; title string; desc string?; video string?; base string?; " +
    "assets []WorkAsset?; links []Link?; recipe string?; cover string?; issue int; created string; updated string?");
  struct("WorkEntry", "sensitive bool?; source string?; based_on string?; id string; author string; title string; cover string?; " +
    "base string?; video_kind string?; has_recipe bool?; likes int; issue int; created string; updated string?");
  struct("Works", "version int; updated string; works []WorkEntry");
  struct("CardFields", "langs []string?; vrchat string?; links []Link?; bases []string?; birthday *Birthday?; show_birthday bool; " +
    "showcase []ShowcaseItem?; contacts *Contacts?");
  struct("Contacts", "qq string?; line string?; discord string?");
  struct("Birthdays", "today string; accounts []Profile");
  struct("PublishWorkReq", "=WorkReq; pictures []string; auto_flag bool?");
  struct("PublishedWork", "=Work; sensitive bool?; source string; pictures []string?; rights_ok bool?; clones int?; own_shop bool?");
  struct("WorkPage", "works []WorkEntry; next string?");
  struct("RecipeReq", "code string; title string; note string?; base string?");
  struct("RecipeAsset", "name string; kind string?; boothId string?");
  struct("RecipeEntry", "id string; name string; note string?; base string?; author string?; login string?; links []Link?; " +
    "added string?; file string; sha256 string?; size int?; items int?; assets []RecipeAsset?; source string; account string?");
  struct("RecipePage", "recipes []RecipeEntry; next string?");
  struct("AssetCounts", "counts {}int");
  struct("AssetWorks", "works []WorkEntry");
  struct("ErrorAnswer", "error string; message string?");
  struct("ServiceInfo", "version int; synced string?; oauth []string?; github_client_id string?; push_key string?");
  struct("QRReq", "kind string; device string");
  struct("QRSession", "id string; url string; poll string?; expires string");
  struct("QRState", "state string; kind string; device string?; person *Person?; account *NewAccount?; poll string?");
  struct("QRScanReq", "device string");
  struct("QRApproveReq", "ok bool");
  struct("PushKeys", "p256dh string; auth string");
  struct("PushReq", "endpoint string; keys PushKeys; lang string; kinds []string");
  struct("PushDeleteReq", "endpoint string");
  struct("ModReq", "action string; reason string?");
  struct("TranslateReq", "post string; target string");
  struct("AITranslateReq", "=TranslateReq; provider string; model string");
  struct("Translation", "text string; source string; provider string; cached bool");
  struct("OAuthStartReq", "provider string; client string; link bool?");
  struct("OAuthStart", "id string; key string; url string; expires string");
  struct("OAuthFinishReq", "id string; key string; nickname string?; handle string?");
  struct("OAuthResult", "=NewAccount; state string");
  struct("VerifyReq", "email string; code string; nickname string; handle string?");
  struct("Account", "=*CardFields; logins []string?; moderator bool?; id string; handle string; allow_requests string; nickname string; email string?; github string?; " +
    "created string; bio string; avatar string; dm_policy string?; show_following bool; show_sensitive bool; booth string?; " +
    "counts ProfileCounts");
  struct("NewAccount", "=Account; token string; restored bool?");
  struct("Person", "id string; handle string; nickname string; github string?; avatar string?; booth string?; bot bool?");
  struct("WorkStats", "likes int; comments int");
  struct("Stats", "updated string; works {}WorkStats");
  struct("MyLikes", "works []string");
  struct("CommentReq", "text string; reply_to string?");
  struct("Comment", "id string; work string?; post string?; author Person; by_author bool?; text string; reply_to string?; created string");
  struct("Comments", "comments []Comment; next string?");
  struct("Notice", "seq int; kind string; work string?; post string?; from []Person; count int; comment string?; at string");
  struct("Notices", "notices []Notice");
  struct("ReportReq", "reason string?; sensitive bool?");
  struct("ProfileCounts", "works int; recipes int; followers int; following int; posts int; likes int");
  struct("Profile", "=Person; =CardFields; source string; read_only bool?; github_card *Card?; works []WorkEntry; " +
    "recipes []RecipeEntry; bio string; created string; counts ProfileCounts; show_following bool; following *bool?; " +
    "follows_you *bool?; blocked *bool?; can_message *bool?");
  struct("Profiles", "accounts []Profile");
  struct("Follow", "target string; github string?; account *Profile?");
  struct("Following", "following []Follow");
  struct("Picture", "id string; kind string; full PictureSize; preview PictureSize");
  struct("PictureSize", "url string; width int; height int; bytes int");
  struct("PostReq", "text string; pictures []string?; tags []string?; reply_to string?; quote_of string?; work string?; " +
    "assets []string?; sensitive bool?; group string?; poll *Poll?; ask bool?; auto_flag bool?");
  struct("Post", "=PostReq; kind string; conversation string; mentions []Person?; cover string?; replies int; reposts int; " +
    "quotes int; bookmarks int; views int; reposted bool; bookmarked bool; quote *Post?; quote_unavailable bool?; " +
    "parent_deleted bool?; deleted bool?; id string; author Person; created string; likes int; comments int; liked bool; " +
    "group_name string?; answer string?; solved bool?; featured bool?; promoted bool?; booth_items []BoothItem?");
  struct("BoothItem", "id string; title string; shop string?; price int; was int?; kind string; base string?; picture string?");
  struct("PostPage", "posts []Post; next string?");
  struct("Thread", "ancestors []Post; post Post; replies []Post; next string?");
  struct("Timeline", "items []TimelineItem; next string?");
  struct("TimelineItem", "kind string; post Post; by *Person?");
  struct("HandleAvailable", "available bool");
  struct("Search", "posts []Post?; accounts []Profile?; next string?");
  struct("Trends", "trends []Trend");
  struct("Trend", "tag string; posters int; posts int; title string?; pinned bool?");
  struct("Feed", "items []FeedItem; next string?");
  struct("FeedItem", "kind string; post *Post?; work *WorkEntry?");
  struct("MessageReq", "text string; picture string?");
  struct("Message", "id string; from string; text string; picture string?; created string; deleted bool");
  struct("MessagePage", "messages []Message; next string?");
  struct("Conversations", "conversations []Conversation");
  struct("Conversation", "account Profile; last Message; unread int");
  struct("Unread", "notices int; messages int; requests int");
  struct("BoothState", "shop string?; name string?; verified string?; pending string?; code string?; expires string?");
  struct("Group", "id string; name string; cover string?; members int; week int; joined bool?; mine bool?; moderator bool?; " +
    "moderators []Person?; pinned *Post?");
  struct("Groups", "groups []Group");
  struct("MemberPage", "accounts []Profile; next string?");
  struct("Theme", "id string; title string; line string?; tag string; start string; end string");
  struct("Themes", "current *Theme?; themes []Theme");
  struct("Poll", "options []string; days int; ends string?; closed bool?; votes []int?; total int?; voted int?");
  // social (rows.go, posts.go, timeline.go, pages.go, groups.go, clone.go, feed.go, home.go, local.go, api.go, device.go)
  struct("PostRow", "=Post; pics []string; pic string?; authorPic string?; mine bool; followed bool; share string?; boothCards []BoothCard?");
  struct("BoothCard", "id string; title string; shop string?; price int; was int?; kind string; base string?; pic string?; owned bool");
  struct("timelineWork", "=PublishedWork; pic string; owned int; total int; items []WorkItem");
  struct("timelinePost", "=PostRow; quote *timelinePost?; workCard *timelineWork?; assetItems []WorkItem?");
  struct("WorkItem", "name string; url string; boothId string; owned bool; key string?; category string?");
  struct("groupRow", "=Group; pic string?");
  struct("pagePerson", "=Profile; pic string");
  struct("CreatorRow", "=CreatorEntry; id string?; github string?; source string?; pic string?; target string?; followed bool?; " +
    "me bool?; followsMe bool?; readOnly bool?; booth string?");
  struct("WorkRow", "=WorkEntry; pic string?; nickname string; authorId string?; target string?; authorPic string?; " +
    "issueUrl string?; followed bool?; mine bool?; authorBooth string?");
  struct("WorkAssetRow", "=WorkAsset; kind string?; boothId string?; owned bool?; key string?; category string?");
  struct("RecipeRef", "id string; name string; note string?; base string?; author string?; login string?; account string?; " +
    "source string?; file string?; added string?; items int?; assets int?; links []Link?");
  struct("s.FeedItem", "key string; kind string; creator CreatorRow; at string?; new bool?; work *WorkRow?; recipe *RecipeRef?; " +
    "notice *Notice?");
  struct("s.Feed", "items []s.FeedItem; unseen int; following int; followingBases int; siteWide bool; today string; " +
    "offline string?; closed string?; noticeError string?");
  struct("HomeItem", "kind string; post *PostRow?; work *WorkRow?");
  struct("homeCursor", "t string?; s bool?; f string?; p string?; w string?");
  struct("Draft", "=CardReq; showBirthday bool?; showFollowing bool?; showShowcase bool?");
  struct("Shop", "sub string; name string");
  struct("deviceState", "status string; user_code string?; verification_uri string?; message string?");
  struct("serviceStats", "=Stats; myLikes []string");
  struct("s.Follow", "login string; at int");
  struct("BaseFollow", "base string; at int");
  struct("localFile", "version int; login string?; following []s.Follow; bases []BaseFollow?; seen []string?; birthdays string?; " +
    "notice bool?; card *Draft?; sent int?; importedId string?; noSystemNotice bool?");
  struct("accountStored", "=Account; token string; seen int");

  // json.Unmarshal(b[k], &v) on the page's body (Go's map[string]json.RawMessage): a missing key is an error, a
  // null leaves v as it was (nil for slices, pointers and maps)
  function has(b, k) { return Object.prototype.hasOwnProperty.call(b, k); }
  function unmarshal(b, k, t, start) {
    if (!has(b, k)) return { ok: false, v: start };
    if (b[k] === null) return { ok: true, v: ["slice", "ptr", "map"].includes(kindOf(t)[0]) ? null : start };
    const ctx = { bad: false }, v = decode(t, b[k], ctx);
    return { ok: !ctx.bad, v };
  }
  // the routes' str, flag and number: what is not of the kind is the zero value
  function str(b, k) { return typeof b[k] === "string" ? goStr(b[k]) : ""; }
  function flag(b, k) { return b[k] === true; }
  function number(b, k) { const v = b[k]; return typeof v === "number" && Number.isInteger(v) ? Math.max(v, 0) : 0; }

  // ---------- errors: socialapi (errors.go, v1.go) and social's serviceError ----------

  class GoError extends Error {}
  // socialapi.Error: a rule's refusal (its code's text when it gives none)
  class RuleError extends GoError {
    constructor(code, msg) { super(msg || MESSAGES[code] || code); this.code = code; }
  }
  // social.serviceError: what the service refused or could not answer
  class ServiceError extends GoError {
    constructor(code, message, status, unavailable) { super(message); this.code = code; this.status = status; this.unavailable = unavailable; }
  }
  function fail(code, msg) { return new RuleError(code, msg || ""); }
  const MESSAGES = {
    bad_request: "内容有误", bad_login: "GitHub 用户名不正确", bad_text: "内容包含无法使用的字符", too_long: "内容过长",
    bad_link: "链接格式不正确", bad_video: "视频链接需来自哔哩哔哩、YouTube、X、抖音、小红书、niconico 或 TikTok",
    bad_recipe: "装配方案的分享码无效", bad_image: "无法读取图片：请使用 JPG 或 PNG",
    rights: "附带网盘链接时，请确认你有权分享其中的内容", banned_words: "内容包含不允许使用的词语",
  };
  const SERVICE_MESSAGES = {
    oauth_expired: "登录已过期，请重新开始", unauthorized: "社区账号无效，请重新登录", bad_email: "邮箱格式不正确", bad_code: "验证码不正确或已过期",
    nickname_required: "首次登录，请填写昵称", mail_failed: "验证码邮件发送失败，请稍后重试", forbidden: "没有权限进行此操作",
    not_found: "内容不存在或已删除", rate_limited: "操作过于频繁，请稍后再试", banned: "该账号已被限制发言",
    nickname_taken: "这个昵称已被社区中的创作者使用", github_failed: "无法通过 GitHub 确认账号，请稍后重试",
    unavailable: "社区服务暂时不可用，请稍后重试", dm_not_allowed: "对方暂不接收你的私信",
    bad_picture: "图片无效，请使用符合大小限制的 JPEG 或 PNG 图片", picture_quota: "图片存储已达上限，请删除不再使用的内容后重试",
    recall_expired: "消息发送已超过 5 分钟，无法撤回",
    bad_handle: "用户名须为 4–15 位字母、数字或下划线，不能全为数字或使用保留名称", handle_taken: "该用户名已被使用，请选择其他用户名",
    bad_shop: "Booth 店铺地址不正确", booth_code_missing: "店铺页面中没有找到验证码", booth_code_expired: "验证码已过期，请重新获取",
    booth_failed: "无法读取 Booth 店铺页面，请稍后重试", not_member: "请先加入小组", poll_voted: "你已经投过票了", poll_closed: "投票已结束",
    ask_picture: "求同款需要附上至少一张图片", same_email: "这已经是当前使用的邮箱", email_taken: "该邮箱已被其他账号使用",
  };
  const has2 = (o, k) => Object.prototype.hasOwnProperty.call(o, k);
  const serviceMessage = code => has2(SERVICE_MESSAGES, code) ? SERVICE_MESSAGES[code] : "";
  const ruleMessage = code => has2(MESSAGES, code) ? MESSAGES[code] : "";

  // ---------- socialapi/rules.go ----------

  const MAX = { nickname: 24, bio: 300, bioLines: 8, title: 60, desc: 1000, descLines: 30, baseName: 24, bases: 5, langs: 5,
    links: 8, linkLabel: 24, url: 300, assets: 30, showcase: 60, assetName: 80, shopName: 40, recipe: 16 << 10, following: 2000,
    avatarB64: 64 << 10, coverB64: 128 << 10, comment: 500, commentLines: 10, report: 200, profileBio: 160, post: 1000,
    postLines: 30, postPictures: 4, postTag: 30, postAssets: 10, message: 1000, pictureBytes: 8 << 20, pollOptions: 4, pollOption: 25 };
  const RE_LOGIN = /^[a-z0-9](?:[a-z0-9]|-[a-z0-9]){0,38}$/;

  function normLogin(s) {
    const l = asciiLower(trimSpace(s));
    if (blen(l) > 39 || !RE_LOGIN.test(l)) throw fail("bad_login");
    return l;
  }
  function cleanText(s, max, multiline, lines) {
    s = trimSpace(s.replace(/\r\n/g, "\n"));
    for (const c of s) {
      const r = c.codePointAt(0);
      if (r === 10) { if (!multiline) throw fail("bad_text"); }
      else if (r === 9) { /* a tab is kept */ }
      else if (isControl(r) || r === 0xfeff || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069) throw fail("bad_text");
    }
    if (runes(s) > max) throw fail("too_long");
    if (multiline && lines > 0 && countOf(s, "\n") + 1 > lines) throw fail("too_long");
    return s;
  }

  const LINK = { boothShop: "booth_shop", boothItem: "booth_item", xianyu: "xianyu", netdisk: "netdisk", gumroad: "gumroad",
    jinxxy: "jinxxy", bilibili: "bilibili", youtube: "youtube", x: "x", pixiv: "pixiv", xiaohongshu: "xiaohongshu", douyin: "douyin",
    discord: "discord", qqGroup: "qq_group", github: "github", website: "website" };
  const PROFILE_LINKS = ["booth_shop", "xianyu", "gumroad", "jinxxy", "bilibili", "youtube", "x", "pixiv", "xiaohongshu", "douyin",
    "discord", "qq_group", "github", "website"];
  const WORK_LINKS = ["booth_shop", "booth_item", "xianyu", "netdisk", "gumroad", "jinxxy", "bilibili", "youtube", "x", "pixiv", "website"];
  const BOOTH_SYSTEM = new Set(["www", "accounts", "manage", "checkout", "api", "s", "asset", "help", "docs", "about", "pass", "admin", "static"]);
  const RE_BOOTH_SHOP_HOST = /^([a-z0-9][a-z0-9-]{0,62})\.booth\.pm$/;
  const RE_BOOTH_ITEM_PATH = /^\/(?:[a-z]{2}(?:-[a-z]{2,4})?\/)?items\/(\d{1,12})\/?$/;
  const RE_SHOP_ITEM_PATH = /^\/items\/(\d{1,12})\/?$/;
  const RE_VRCHAT_USER = /^https:\/\/vrchat\.com\/home\/user\/usr_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
  const RE_HOST_NAME = /^(?:[a-z0-9_](?:[a-z0-9_-]{0,61}[a-z0-9_])?\.)+(?:[a-z]{2,63}|xn--[a-z0-9-]{1,59})$/;

  function hostIs(h, ...names) {
    for (const n of names) {
      if (n[0] === ".") { if (h.endsWith(n) || h === n.slice(1)) return true; }
      else if (h === n) return true;
    }
    return false;
  }
  // publicHost (an address never ends in letters, so the name's form leaves IP addresses out on its own)
  function publicHost(h) {
    if (h === "" || !h.includes(".") || !RE_HOST_NAME.test(h)) return false;
    for (const s of ["localhost", ".localhost", ".local", ".internal", ".lan", ".home", ".test", ".invalid", ".example"]) {
      if (h === s.replace(/^\./, "") || h.endsWith(s)) return false;
    }
    return true;
  }
  // parseHTTPS: [url, host] or null
  function parseHTTPS(raw) {
    raw = trimSpace(raw);
    if (raw === "" || blen(raw) > MAX.url || /[ \t\r\n<>"'`\\]/.test(raw)) return null;
    const u = parseURL(raw);
    if (!u || u.scheme !== "https" || u.user || urlPort(u) !== "" || u.opaque !== "") return null;
    const h = asciiLower(urlHostname(u));
    if (!publicHost(h)) return null;
    u.host = h;
    return [u, h];
  }
  function kindOK(kind, u, h) {
    const p = escapedPath(u);
    switch (kind) {
      case "booth_shop": { const m = RE_BOOTH_SHOP_HOST.exec(h); return !!m && !BOOTH_SYSTEM.has(m[1]) && (p === "" || p === "/" || p === "/items"); }
      case "booth_item": {
        if (h === "booth.pm") return RE_BOOTH_ITEM_PATH.test(p);
        const m = RE_BOOTH_SHOP_HOST.exec(h);
        return !!m && !BOOTH_SYSTEM.has(m[1]) && RE_SHOP_ITEM_PATH.test(p);
      }
      case "xianyu": return hostIs(h, ".goofish.com", "2.taobao.com", "m.tb.cn");
      case "netdisk": return hostIs(h, "pan.baidu.com", "yun.baidu.com") && (p.startsWith("/s/") || p.startsWith("/share/init"));
      case "gumroad": return hostIs(h, ".gumroad.com");
      case "jinxxy": return hostIs(h, ".jinxxy.com");
      case "bilibili": return hostIs(h, "space.bilibili.com", "www.bilibili.com", "m.bilibili.com", "live.bilibili.com", "b23.tv");
      case "youtube": return hostIs(h, "youtube.com", "www.youtube.com", "m.youtube.com", "youtu.be");
      case "x": return hostIs(h, "x.com", "twitter.com", "www.x.com", "mobile.twitter.com", "mobile.x.com");
      case "pixiv": return hostIs(h, "www.pixiv.net", "pixiv.net", "pixiv.me");
      case "xiaohongshu": return hostIs(h, "www.xiaohongshu.com", "xiaohongshu.com", "xhslink.com");
      case "douyin": return hostIs(h, "www.douyin.com", "douyin.com", "v.douyin.com");
      case "discord": return hostIs(h, "discord.gg") || hostIs(h, "discord.com") && p.startsWith("/invite/");
      case "qq_group": return hostIs(h, "qm.qq.com");
      case "github": return hostIs(h, "github.com");
      case "website": return true;
    }
    return false;
  }
  function knownSite(h) {
    return hostIs(h, "booth.pm", ".booth.pm", ".goofish.com", "2.taobao.com", "m.tb.cn", "pan.baidu.com", "yun.baidu.com",
      ".gumroad.com", ".jinxxy.com", ".bilibili.com", "b23.tv", "youtube.com", ".youtube.com", "youtu.be", "x.com",
      ".x.com", "twitter.com", ".twitter.com", ".pixiv.net", "pixiv.me", ".xiaohongshu.com", "xhslink.com",
      ".douyin.com", "discord.gg", ".discord.com", "qm.qq.com", "github.com");
  }
  const KIND_ORDER = ["booth_item", "booth_shop", "xianyu", "netdisk", "gumroad", "jinxxy", "bilibili", "youtube", "x", "pixiv",
    "xiaohongshu", "douyin", "discord", "qq_group", "github", "website"];
  function linkKindOf(raw, allowed) {
    const p = parseHTTPS(raw);
    if (!p) return "";
    for (const k of KIND_ORDER) {
      if (contains(allowed, k) && kindOK(k, p[0], p[1])) return k === "website" && knownSite(p[1]) ? "" : k;
    }
    return "";
  }
  function clip(s, n) { const r = [...s]; return r.length <= n ? s : r.slice(0, n).join("") + "…"; }
  // CheckLink: the link with its address and label cleaned
  function checkLink(l, allowed) {
    if (!contains(allowed, l.kind)) throw fail("bad_link");
    const p = parseHTTPS(l.url);
    if (!p || !kindOK(l.kind, p[0], p[1]) || l.kind === "website" && knownSite(p[1])) throw fail("bad_link", "链接格式不正确：" + clip(l.url, 60));
    const label = cleanText(l.label, MAX.linkLabel, false, 0);
    return { kind: l.kind, url: urlString(p[0]), label };
  }
  function checkLinks(ls, allowed) {
    ls = ls || [];
    if (ls.length > MAX.links) throw fail("too_long", "链接最多 8 个");
    const out = [], seen = new Set();
    for (const l of ls) {
      const c = checkLink(l, allowed);
      if (!seen.has(c.url)) { seen.add(c.url); out.push(c); }
    }
    return out;
  }
  function hasNetdisk(ls) { return (ls || []).some(l => l.kind === "netdisk"); }
  function boothItemID(raw) {
    const p = parseHTTPS(raw);
    if (!p) return "";
    const [u, h] = p;
    if (h === "booth.pm") { const m = RE_BOOTH_ITEM_PATH.exec(escapedPath(u)); return m ? m[1] : ""; }
    const m = RE_BOOTH_SHOP_HOST.exec(h);
    if (m && !BOOTH_SYSTEM.has(m[1])) { const m2 = RE_SHOP_ITEM_PATH.exec(escapedPath(u)); if (m2) return m2[1]; }
    return "";
  }
  function checkVRChat(s) {
    s = trimSpace(s);
    if (s === "" || RE_VRCHAT_USER.test(s)) return s;
    throw fail("bad_link", "VRChat 主页链接应为 https://vrchat.com/home/user/usr_… 的形式");
  }
  function videoKind(raw) {
    const p = parseHTTPS(raw);
    if (!p) return "";
    const [u, h] = p, path = escapedPath(u);
    if (hostIs(h, "www.bilibili.com", "m.bilibili.com") && path.startsWith("/video/") || hostIs(h, "b23.tv") && path.length > 1) return "bilibili";
    if (hostIs(h, "youtube.com", "www.youtube.com", "m.youtube.com") && (path === "/watch" && queryGet(u, "v") !== "" || path.startsWith("/shorts/")) ||
      hostIs(h, "youtu.be") && path.length > 1) return "youtube";
    if (hostIs(h, "x.com", "twitter.com", "mobile.twitter.com") && path.includes("/status/")) return "x";
    if (hostIs(h, "www.douyin.com", "douyin.com") && path.startsWith("/video/") || hostIs(h, "v.douyin.com") && path.length > 1) return "douyin";
    if (hostIs(h, "www.xiaohongshu.com", "xiaohongshu.com") && (path.startsWith("/explore/") || path.startsWith("/discovery/item/")) ||
      hostIs(h, "xhslink.com") && path.length > 1) return "xiaohongshu";
    if (hostIs(h, "www.nicovideo.jp", "sp.nicovideo.jp") && path.startsWith("/watch/") || hostIs(h, "nico.ms") && path.length > 1) return "nicovideo";
    if (hostIs(h, "www.tiktok.com", "tiktok.com") && path.includes("/video/") || hostIs(h, "vt.tiktok.com", "vm.tiktok.com") && path.length > 1) return "tiktok";
    return "";
  }
  const LANGS = ["zh", "ja", "en", "ko", "other"];
  // CheckCard (not a route's own check: kept so that the rules can be held to internal/socialapi/testdata/cases.json)
  function checkCard(r) {
    const out = zero("CardReq");
    out.nickname = cleanText(r.nickname, MAX.nickname, false, 0);
    if (out.nickname === "") throw fail("bad_text", "昵称不能为空");
    out.bio = cleanText(r.bio, MAX.bio, true, MAX.bioLines);
    for (const l of r.langs || []) {
      if (contains(LANGS, l) && !contains(out.langs, l) && (out.langs || []).length < MAX.langs) out.langs = (out.langs || []).concat(l);
    }
    out.vrchat = checkVRChat(r.vrchat);
    out.links = checkLinks(r.links, PROFILE_LINKS);
    if ((r.bases || []).length > MAX.bases) throw fail("too_long", "常用素体最多 5 个");
    for (const b of r.bases || []) {
      const c = cleanText(b, MAX.baseName, false, 0);
      if (c !== "" && !contains(out.bases, c)) out.bases = (out.bases || []).concat(c);
    }
    if (r.birthday) {
      if (!dateOK(r.birthday.month, r.birthday.day)) throw fail("bad_request", "生日日期不正确");
      out.birthday = { month: r.birthday.month, day: r.birthday.day };
    }
    if ((r.following || []).length > MAX.following) throw fail("too_long", "公开的关注最多 2000 个");
    for (const f of r.following || []) {
      const l = normLogin(f);
      if (!contains(out.following, l)) out.following = (out.following || []).concat(l);
    }
    if (r.avatar !== "") {
      if (!jpegB64(r.avatar, MAX.avatarB64)) throw fail("bad_image");
      out.avatar = r.avatar;
    }
    out.showcase = checkShowcase(r.showcase);
    if (r.contacts) out.contacts = checkContacts(r.contacts);
    return out;
  }
  function checkContacts(c) {
    const out = { qq: trimSpace(c.qq || ""), line: asciiLower(trimSpace(c.line || "").replace(/^@/, "")),
      discord: asciiLower(trimSpace(c.discord || "").replace(/^@/, "")) };
    if (out.qq !== "" && !/^[1-9][0-9]{4,11}$/.test(out.qq)) throw fail("bad_contact", "QQ 号格式不正确");
    if (out.line !== "" && !/^[a-z0-9._-]{4,20}$/.test(out.line)) throw fail("bad_contact", "LINE ID 格式不正确");
    if (out.discord !== "" && !/^[a-z0-9_.]{2,32}$/.test(out.discord)) throw fail("bad_contact", "Discord 用户名格式不正确");
    if (out.qq === "" && out.line === "" && out.discord === "") return null;
    return out;
  }
  function checkShowcase(items) {
    items = items || [];
    if (items.length > MAX.showcase) throw fail("too_long", "素材展示柜最多 60 件");
    let out = null;
    const seen = new Set();
    for (const item of items) {
      if (!/^[0-9]{1,12}$/.test(item.booth_id)) throw fail("bad_request", "素材展示柜的 Booth 商品编号无效");
      if (seen.has(item.booth_id)) throw fail("bad_request", "素材展示柜中有重复的 Booth 商品");
      seen.add(item.booth_id);
      const name = cleanText(item.name, MAX.assetName, false, 0);
      if (name === "") throw fail("bad_text", "素材名称不能为空");
      const shop = cleanText(item.shop, MAX.shopName, false, 0);
      (out = out || []).push({ booth_id: item.booth_id, name, shop });
    }
    return out;
  }
  // base64.StdEncoding.DecodeString: null where Go gives an error (\r and \n are skipped, padding is required)
  function b64Decode(s) {
    s = s.replace(/[\r\n]/g, "");
    if (s.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(s)) return null;
    try {
      const bin = atob(s), out = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
      return out;
    } catch (e) { return null; }
  }
  function b64Encode(bytes) {
    let bin = "";
    for (let i = 0; i < bytes.length; i += 8192) bin += String.fromCharCode.apply(null, bytes.subarray(i, i + 8192));
    return btoa(bin);
  }
  function jpegB64(s, max) {
    if (blen(s) > max) return false;
    const b = b64Decode(s);
    return !!b && b.length > 4 && b[0] === 0xff && b[1] === 0xd8 && b[2] === 0xff;
  }
  function dateOK(m, d) {
    const days = [0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
    return m >= 1 && m <= 12 && d >= 1 && d <= days[m];
  }
  const RE_RECIPE_HEAD = /MIOVRCA-RECIPE-[0-9]{1,3}[\t\n\f\r ]*[:：]/iu;
  const RE_WORK_ID = /^(?:w[0-9]{1,9}|m[1-9][0-9]{0,17})$/;
  function workIDOK(s) { return RE_WORK_ID.test(s); }
  // CheckWork (kept, like checkCard, for the rules' cases)
  function checkWork(r) {
    const out = zero("WorkReq");
    out.rights_ok = r.rights_ok;
    if (r.based_on !== "") {
      if (!workIDOK(r.based_on) || r.based_on === r.replace) throw fail("bad_request", "参考的作品编号无效，或与当前作品相同");
      out.based_on = r.based_on;
    }
    if (r.replace !== "") {
      if (!RE_WORK_ID.test(r.replace)) throw fail("bad_request");
      out.replace = r.replace;
    }
    if (r.cover !== "") {
      if (!jpegB64(r.cover, MAX.coverB64)) throw fail("bad_image");
      out.cover = r.cover;
    }
    out.title = cleanText(r.title, MAX.title, false, 0);
    if (out.title === "") throw fail("bad_text", "请填写标题");
    out.desc = cleanText(r.desc, MAX.desc, true, MAX.descLines);
    const v = trimSpace(r.video);
    if (v !== "") {
      if (videoKind(v) === "") throw fail("bad_video");
      out.video = urlString(parseHTTPS(v)[0]);
    }
    out.base = cleanText(r.base, MAX.baseName, false, 0);
    if ((r.assets || []).length > MAX.assets) throw fail("too_long", "同款素材最多 30 件");
    for (const a of r.assets || []) {
      const c = { name: cleanText(a.name, MAX.assetName, false, 0), url: "", shop: "" };
      if (c.name === "") throw fail("bad_text", "素材名称不能为空");
      c.shop = cleanText(a.shop, MAX.shopName, false, 0);
      if (trimSpace(a.url) !== "") {
        if (linkKindOf(a.url, ["booth_item", "gumroad", "jinxxy"]) === "") throw fail("bad_link", "素材链接应为 Booth、Gumroad 或 Jinxxy 的商品页：" + clip(a.url, 60));
        c.url = urlString(parseHTTPS(a.url)[0]);
      }
      out.assets = (out.assets || []).concat([c]);
    }
    out.links = checkLinks(r.links, WORK_LINKS);
    if (hasNetdisk(out.links) && !r.rights_ok) throw fail("rights");
    const rc = trimSpace(r.recipe);
    if (rc !== "") {
      if (!recipeCodeOK(rc)) throw fail("bad_recipe");
      out.recipe = rc;
    }
    return out;
  }
  function recipeCodeOK(s) {
    if (blen(s) > MAX.recipe || !RE_RECIPE_HEAD.test(s)) return false;
    for (const c of s) { const r = c.codePointAt(0); if (isControl(r) && r !== 10 && r !== 13 && r !== 9) return false; }
    return true;
  }

  // ---------- socialapi/creator.go and groups.go ----------

  function boothShop(raw) {
    raw = goLower(trimSpace(raw));
    if (raw === "") return "";
    if (!/[/.:]/.test(raw)) raw = "https://" + raw + ".booth.pm/";
    else if (raw.startsWith("http://")) raw = "https://" + raw.slice(7);
    else if (!raw.startsWith("https://")) raw = "https://" + raw;
    const p = parseHTTPS(raw);
    if (!p || p[0].rawQuery !== "" || p[0].fragment !== "") return "";
    const m = RE_BOOTH_SHOP_HOST.exec(p[1]);
    if (!m || BOOTH_SYSTEM.has(m[1]) || m[1].endsWith("-")) return "";
    const path = escapedPath(p[0]).replace(/\/$/, "");
    if (path !== "" && path !== "/items") return "";
    return m[1];
  }
  function groupIDOK(id) { return /^b[0-9a-f]{16}$/.test(id); }
  function checkPoll(p) {
    if ((p.options || []).length < 2 || p.options.length > MAX.pollOptions) throw fail("bad_request", "投票选项需为 2–4 个");
    const out = { options: [], days: 0 };
    for (const o of p.options) {
      let clean;
      try { clean = cleanText(o, MAX.pollOption, false, 0); }
      catch (e) { if (e instanceof RuleError && e.code === "too_long") throw fail("too_long", "投票选项最多 25 个字"); throw e; }
      if (clean === "") throw fail("bad_text", "投票选项不能为空");
      for (const x of out.options) if (equalFold(x, clean)) throw fail("bad_request", "投票选项不能重复");
      out.options.push(clean);
    }
    for (const d of [1, 3, 7]) if (p.days === d) out.days = d;
    if (out.days === 0) throw fail("bad_request", "投票时长需为 1 天、3 天或 7 天");
    return out;
  }

  // ---------- browser storage: what the program keeps in files of its data folder ----------
  // Every access may throw (a private window, blocked site data): what cannot be kept there is kept in memory for the
  // page's life.

  const SESSION_KEY = "miovrca.web.session", LOCAL_KEY = "miovrca.web.local", DEVICE_KEY = "miovrca.web.device";
  const memory = {};
  function storeGet(k) {
    if (has2(memory, k)) return memory[k];
    try { return localStorage.getItem(k); } catch (e) { return null; }
  }
  function storeSet(k, v) {
    try {
      if (v === null) localStorage.removeItem(k); else localStorage.setItem(k, v);
      delete memory[k];
    } catch (e) { memory[k] = v; }
  }

  // ---------- social/account.go: the account (Go: community-account.json) ----------
  // { acc: the Account, token, seen }. A stored session that cannot be read is dropped (the program refuses to go on
  // and asks for the file to be looked at; a browser's storage cannot be looked at like that).

  function accountCopy() {
    const raw = storeGet(SESSION_KEY);
    if (!raw) return { acc: zero("Account"), token: "", seen: 0 };
    try {
      const v = JSON.parse(raw), ctx = { bad: false }, d = decode("accountStored", v, ctx);
      if (!ctx.bad && d.id !== "" && d.token !== "" && d.seen >= 0) {
        const acc = Object.assign({}, d);
        delete acc.token; delete acc.seen;
        return { acc, token: d.token, seen: d.seen };
      }
    } catch (e) { /* (dropped below) */ }
    storeSet(SESSION_KEY, null);
    return { acc: zero("Account"), token: "", seen: 0 };
  }
  // changeAccount: the answer belongs to the account that sent the request; another one now in its place is not touched
  function changeAccount(expected, f) {
    const cur = accountCopy();
    if (cur.token !== expected) throw new GoError(SERVICE_MESSAGES.unauthorized);
    const d = { acc: Object.assign({}, cur.acc), token: cur.token, seen: cur.seen };
    f(d);
    storeSet(SESSION_KEY, JSON.stringify(Object.assign(marshal("Account", d.acc), { token: d.token, seen: d.seen })));
  }
  function dropAccount(expected) {
    if (accountCopy().token === expected) storeSet(SESSION_KEY, null);
  }
  function needAccount() {
    const a = accountCopy();
    if (a.token === "") throw new ServiceError("unauthorized", SERVICE_MESSAGES.unauthorized, 401, false);
    return a;
  }
  // the account lock (Go: accountMu): routes that read the account and write it back take turns
  let accountChain = Promise.resolve();
  function withAccountLock(fn) {
    const run = accountChain.then(fn, fn);
    accountChain = run.then(() => {}, () => {});
    return run;
  }

  // ---------- social/local.go: what community.json keeps (here in localStorage) ----------
  // The follows of GitHub logins kept before accounts existed are never here: a browser starts with none.

  const SEEN_MAX = 3000, FOLLOWING_MAX = MAX.following, FEED_MAX = 200;
  function line(s, n) { return cleanText(s, n, false, 0); }
  function localLoad() {
    const raw = storeGet(LOCAL_KEY);
    let f = null;
    if (raw) {
      try { const ctx = { bad: false }; f = decode("localFile", JSON.parse(raw), ctx); if (ctx.bad) f = null; } catch (e) { f = null; }
    }
    if (!f) return Object.assign(zero("localFile"), { version: 1, following: [] });
    f.version = 1;
    try { f.login = normLogin(f.login); } catch (e) { f.login = ""; }
    const fl = [], seen = new Set();
    for (const x of f.following || []) {
      let l;
      try { l = normLogin(x.login); } catch (e) { continue; }
      if (!seen.has(l) && fl.length < FOLLOWING_MAX) { seen.add(l); fl.push({ login: l, at: x.at }); }
    }
    f.following = fl;
    const bases = [], baseSeen = new Set();
    for (const b of f.bases || []) {
      let name;
      try { name = line(b.base, MAX.baseName); } catch (e) { continue; }
      if (name !== "" && !baseSeen.has(name) && bases.length < FOLLOWING_MAX) { baseSeen.add(name); bases.push({ base: name, at: b.at }); }
    }
    f.bases = bases;
    if ((f.seen || []).length > SEEN_MAX) f.seen = f.seen.slice(f.seen.length - SEEN_MAX);
    return f;
  }
  function cloneLocal(d) {
    const c = Object.assign({}, d, { following: (d.following || []).map(x => Object.assign({}, x)),
      bases: (d.bases || []).map(x => Object.assign({}, x)), seen: (d.seen || []).slice() });
    if (d.card) c.card = JSON.parse(JSON.stringify(d.card));
    return c;
  }
  // withLocal: f on what is kept, saved when f says it changed it
  function withLocal(f) {
    const d = cloneLocal(localLoad());
    if (f(d)) storeSet(LOCAL_KEY, JSON.stringify(marshal("localFile", d)));
  }
  function localCopy() { return cloneLocal(localLoad()); }
  function followingOf(d) { const m = new Map(); for (const f of d.following) m.set(f.login, f.at); return m; }
  function markSeen(keys, birthdays) {
    withLocal(d => {
      let changed = false;
      const have = new Set(d.seen);
      for (const k of keys) {
        if (k !== "" && blen(k) <= 80 && !have.has(k)) { have.add(k); d.seen.push(k); changed = true; }
      }
      if (d.seen.length > SEEN_MAX) d.seen = d.seen.slice(d.seen.length - SEEN_MAX);
      if (birthdays !== "" && d.birthdays !== birthdays) { d.birthdays = birthdays; changed = true; }
      return changed;
    });
  }
  function followingList(d) { return d.following.slice().sort((a, b) => b.at - a.at); }
  // saveDraft: the card as filled in (Go keeps it for the video studio's end card)
  function saveDraft(dr) {
    dr = Object.assign({}, dr, { following: null });
    if (blen(dr.avatar || "") > MAX.avatarB64) throw new GoError("头像图片过大");
    if (goJSONLen(marshal("Draft", dr)) > 256 << 10) throw new GoError("名片内容过长");
    withLocal(d => { d.card = dr; return true; });
  }

  // ---------- social/service.go and service_more.go: the community service ----------
  // The service is this page's origin. A browser sets no User-Agent of its own choosing and sends no cookies here;
  // the rest of each request (method, address, headers, body, time limits, no redirects followed) is Go's.

  const BASE = (() => { try { return new URL("./", document.baseURI).href.replace(/\/+$/, ""); } catch (e) { return ""; } })();
  const SERVICE_KEEP = 60000;
  function unavailableError() { return new ServiceError("unavailable", SERVICE_MESSAGES.unavailable, 0, true); }
  // serviceNetError: a browser does not say why a request failed; a time limit reached is said as Go says a timeout
  function netError(e) {
    const why = e && e.name === "AbortError" ? "连接超时，可能需要代理" : "连接失败，可能需要代理";
    return new ServiceError("", "无法连接社区服务（" + why + "）", 0, true);
  }
  // one exchange: the status and body, with Go's checks of both (a 401 drops the account it was sent with)
  async function exchange(method, url, headers, body, timeout, token, secrets) {
    const ctl = new AbortController(), timer = setTimeout(() => ctl.abort(), timeout);
    let resp, bytes;
    try {
      resp = await fetch(url, { method, headers, body, redirect: "manual", credentials: "omit", cache: "no-store", signal: ctl.signal });
      if (resp.status === 401 && token) dropAccount(token);
      bytes = new Uint8Array(await resp.arrayBuffer());
    } catch (e) {
      throw netError(e);
    } finally { clearTimeout(timer); }
    // (a redirect is not followed, as in Go; the browser hides its status, said here as 302)
    const status = resp.type === "opaqueredirect" ? 302 : resp.status;
    if (bytes.length > 4 << 20) throw new GoError("社区服务返回的内容过大");
    if (status < 200 || status >= 300) {
      let a = zero("ErrorAnswer");
      try { a = decode("ErrorAnswer", JSON.parse(utf8Out.decode(bytes)), { bad: false }); } catch (e) { /* (not JSON) */ }
      let msg = firstOf(a.message, serviceMessage(a.error), ruleMessage(a.error), "社区服务请求失败（HTTP " + status + "）");
      for (const secret of secrets) if (secret) msg = msg.split(secret).join("***");
      throw new ServiceError(a.error, msg, status, status >= 500);
    }
    return bytes;
  }
  function decodeAnswer(t, bytes) {
    let v;
    try { v = JSON.parse(utf8Out.decode(bytes)); } catch (e) { throw new GoError("社区服务返回的内容有误"); }
    const ctx = { bad: false }, out = decode(t, v, ctx);
    if (ctx.bad) throw new GoError("社区服务返回的内容有误");
    return out;
  }
  // call: a JSON request to /api/v1/<path> (in: the request as Go marshals it; out: the type of the answer)
  async function call(method, path, token, input, outType, linkSecret, extra) {
    if (BASE === "") throw unavailableError();
    const headers = Object.assign({ Accept: "application/json" }, extra || {});
    if (input !== undefined) headers["Content-Type"] = "application/json";
    if (token) headers.Authorization = "Bearer " + token;
    if (path === "views") headers["X-MioVRCA-Device"] = viewDevice();
    const timeout = path === "auth/code" || path === "me/email/code" || path === "me/booth/check" ? 25000 : 10000;
    const bytes = await exchange(method, BASE + "/api/v1/" + path, headers, input === undefined ? undefined : JSON.stringify(input),
      timeout, token, [token, linkSecret || ""]);
    return outType ? decodeAnswer(outType, bytes) : undefined;
  }
  // raw: any request to the service (a picture's bytes, a file it names)
  async function raw(method, fullURL, token, contentType, body) {
    if (BASE === "") throw unavailableError();
    const headers = { Accept: "application/json, text/plain" };
    if (contentType) headers["Content-Type"] = contentType;
    if (token) headers.Authorization = "Bearer " + token;
    return exchange(method, fullURL, headers, body, fullURL.endsWith("/api/v1/auth/code") ? 20000 : 10000, token, [token]);
  }
  async function send(method, path, token, input, outType) {
    const b = await raw(method, BASE + "/api/v1/" + path, token, input === undefined ? "" : "application/json",
      input === undefined ? undefined : JSON.stringify(input));
    return outType ? decodeAnswer(outType, b) : undefined;
  }
  async function fetchFile(path) {
    if (BASE === "" || path === "" || !path.startsWith("/")) throw new GoError("社区服务的文件地址无效");
    return raw("GET", BASE + path, "", "", undefined);
  }

  // the service's endpoints, as service.go and service_more.go name them
  const svc = {
    getInfo: () => call("GET", "info", "", undefined, "ServiceInfo"),
    verifyCode: r => call("POST", "auth/verify", "", marshal("VerifyReq", r), "NewAccount"),
    sendCode: r => call("POST", "auth/code", "", { email: r.email }),
    logout: t => call("DELETE", "me/session", t),
    getMe: t => call("GET", "me", t, undefined, "Account"),
    deleteAccount: t => call("DELETE", "me", t),
    linkGitHub: (t, accessToken) => call("POST", "me/github", t, { access_token: accessToken }, "Account", accessToken),
    unlinkGitHub: t => call("DELETE", "me/github", t, undefined, "Account"),
    like: (t, id) => call("PUT", "works/" + pathEscape(id) + "/like", t),
    unlike: (t, id) => call("DELETE", "works/" + pathEscape(id) + "/like", t),
    getStats: () => call("GET", "stats", "", undefined, "Stats"),
    myLikes: t => call("GET", "me/likes", t, undefined, "MyLikes"),
    comments: (id, after) => call("GET", "works/" + pathEscape(id) + "/comments?after=" + queryEscape(after), "", undefined, "Comments"),
    postComment: (t, id, r) => call("POST", "works/" + pathEscape(id) + "/comments", t, marshal("CommentReq", r), "Comment"),
    deleteComment: (t, id) => call("DELETE", "comments/" + pathEscape(id), t),
    reportComment: (t, id, r) => call("POST", "comments/" + pathEscape(id) + "/report", t, marshal("ReportReq", r)),
    notices: (t, after) => call("GET", "me/notices?after=" + after, t, undefined, "Notices"),
    patchMe: (t, r) => send("PATCH", "me", t, r, "Account"),
    profile: (t, id) => send("GET", "accounts/" + pathEscape(id), t, undefined, "Profile"),
    profileByGitHub: (t, login) => send("GET", "accounts/by-github/" + pathEscape(login), t, undefined, "Profile"),
    publishWork: (t, r) => send("POST", "works", t, marshal("PublishWorkReq", r), "PublishedWork"),
    patchWork: (t, id, body) => send("PATCH", "works/" + pathEscape(id), t, body, "PublishedWork"),
    getWork: id => send("GET", "works/" + pathEscape(id), "", undefined, "PublishedWork"),
    deleteWork: (t, id) => send("DELETE", "works/" + pathEscape(id), t),
    listWorks: q => send("GET", "works?" + valuesEncode(q), "", undefined, "WorkPage"),
    createPost: (t, r) => call("POST", "posts", t, marshal("PostReq", r), "Post"),
    listPosts: (t, q) => call("GET", "posts?" + valuesEncode(q), t, undefined, "Feed"),
    getPost: (t, id) => call("GET", "posts/" + pathEscape(id), t, undefined, "Post"),
    deletePost: (t, id) => call("DELETE", "posts/" + pathEscape(id), t),
    likePost: (t, id, on) => call(on ? "PUT" : "DELETE", "posts/" + pathEscape(id) + "/like", t),
    postComments: (id, after) => call("GET", "posts/" + pathEscape(id) + "/comments?after=" + queryEscape(after), "", undefined, "Comments"),
    postPostComment: (t, id, r) => call("POST", "posts/" + pathEscape(id) + "/comments", t, marshal("CommentReq", r), "Comment"),
    publishRecipe: (t, r) => send("POST", "recipes", t, marshal("RecipeReq", r), "RecipeEntry"),
    listRecipes: q => send("GET", "recipes?" + valuesEncode(q), "", undefined, "RecipePage"),
    getRecipe: id => send("GET", "recipes/" + pathEscape(id), "", undefined, "RecipeEntry"),
    reportTarget: (t, kind, id, r) => send("POST", kind + "/" + pathEscape(id) + "/report", t, marshal("ReportReq", r)),
    follow: (t, target) => send("PUT", "follows/" + pathEscape(target), t),
    unfollow: (t, target) => send("DELETE", "follows/" + pathEscape(target), t),
    following: t => send("GET", "me/following", t, undefined, "Following"),
    importFollowing: (t, logins) => send("POST", "me/following/import", t, { logins }),
    birthdays: t => send("GET", "me/birthdays", t, undefined, "Birthdays"),
  };
  // uploadPicture: the picture's bytes as they are (Go sends them as image/jpeg, PNG too)
  async function uploadPicture(t, kind, bytes) {
    const p = decodeAnswer("Picture", await raw("POST", BASE + "/api/v1/pictures?kind=" + queryEscape(kind), t, "image/jpeg", bytes));
    if (p.id === "" || p.full.url === "") throw new GoError("社区服务返回的内容有误");
    return p;
  }

  // the caches of a serviceClient (never kept beyond the page)
  const cache = { info: null, infoErr: null, infoAt: 0, stats: null, likes: null, statsAt: 0, statsAccount: "",
    noticeAccount: "", noticeMax: 0, fresh: "", profiles: new Map() };
  async function cachedInfo(refresh) {
    if (refresh || cache.infoAt === 0 || Date.now() - cache.infoAt >= SERVICE_KEEP) {
      try { cache.info = await svc.getInfo(); cache.infoErr = null; } catch (e) { cache.info = zero("ServiceInfo"); cache.infoErr = goErr(e); }
      cache.infoAt = Date.now();
    }
    if (cache.infoErr) throw cache.infoErr;
    return cache.info;
  }
  async function cachedStats(refresh) {
    const a = accountCopy();
    if (refresh || cache.statsAt === 0 || Date.now() - cache.statsAt >= SERVICE_KEEP || cache.statsAccount !== a.acc.id) {
      const stats = await svc.getStats();
      let likes = { works: [] };
      if (a.token !== "") likes = await svc.myLikes(a.token);
      Object.assign(cache, { stats, likes, statsAccount: a.acc.id, statsAt: Date.now() });
    }
    return Object.assign({}, cache.stats, { myLikes: cache.likes.works });
  }
  function invalidateStats() { cache.statsAt = 0; }
  async function readNotices(after) {
    const a = accountCopy();
    if (a.token === "") return { notices: [] };
    const n = await svc.notices(a.token, after);
    if (cache.noticeAccount !== a.acc.id) { cache.noticeAccount = a.acc.id; cache.noticeMax = 0; }
    for (const x of n.notices || []) cache.noticeMax = Math.max(cache.noticeMax, x.seq);
    return n;
  }
  function seenNotices(seq) {
    const a = accountCopy();
    if (a.token === "") return;
    if (cache.noticeAccount !== a.acc.id || seq <= a.seen || seq > cache.noticeMax) return;
    changeAccount(a.token, d => { d.seen = Math.max(d.seen, seq); });
  }
  // refreshAccount: the account read again from the service once a page load (settings changed elsewhere)
  async function refreshAccount(a) {
    if (cache.fresh === a.acc.id || a.token === "") return;
    await withAccountLock(async () => {
      try {
        const me = await svc.getMe(a.token);
        if (me.id === a.acc.id) { changeAccount(a.token, d => { d.acc = me; }); cache.fresh = a.acc.id; }
      } catch (e) { goErr(e); }
    });
  }
  // unread: the unread counts (Go also tells new ones as Windows notifications: a browser has none of those)
  async function unread(a) {
    if (a.token === "") return zero("Unread");
    return call("GET", "me/unread", a.token, undefined, "Unread");
  }
  // cachedProfile: one public profile, kept for a minute (its refusal too)
  async function cachedProfile(id, token, refresh) {
    const p = cache.profiles.get(id);
    if (p && !refresh && Date.now() - p.at < SERVICE_KEEP) { if (p.err) throw p.err; return p.profile; }
    let profile = zero("Profile"), err = null;
    try { profile = await svc.profile(token, id); } catch (e) { err = goErr(e); }
    cache.profiles.set(id, { profile, at: Date.now(), err });
    if (err) throw err;
    return profile;
  }
  // viewDevice: a stable random ID of this browser for counting views (Go: community-device.txt; one that is not
  // 32 hex digits is replaced here, where Go asks for the file to be looked at)
  function viewDevice() {
    let id = storeGet(DEVICE_KEY) || "";
    if (!/^[0-9a-fA-F]{32}$/.test(id)) {
      const b = new Uint8Array(16);
      crypto.getRandomValues(b);
      id = Array.from(b, x => x.toString(16).padStart(2, "0")).join("");
      storeSet(DEVICE_KEY, id);
    }
    return id;
  }
  // an error as Go would hold it: what is not one of Go's (a mistake in this file) goes on up
  function goErr(e) { if (e instanceof GoError) return e; throw e; }

  // ---------- social/repo.go and read.go: the repository copy the service keeps at /repo/ ----------
  // Go reads the service's copy first and GitHub and jsDelivr after it; the page reads the service's copy only (it is
  // where the page itself comes from). What was read is kept for the page's life with Go's freshness rules (the
  // program keeps it on disk as well).

  const REPO = "CokoIya/MioVRCA-community";
  const ERR_REPO_NONE = new GoError("社区还没有开放，请稍后再来看看");
  const ERR_REPO_TOO_LARGE = new GoError("社区仓库中的文件过大，已拒绝");
  const ERR_UNREAD = new GoError("社区暂时无法读取，请稍后再试");
  class RepoNetError extends GoError {}
  function repoNetError(why) { return new RepoNetError("无法连接社区（" + why + "）"); }
  function repoSources() { return BASE === "" ? [] : [BASE + "/repo/"]; }
  const LIST_MAX = 4 << 20, FILE_MAX = 512 << 10, ROWS_MAX = 5000, LIST_FRESH = 3600e3, FILE_FRESH = 3600e3, RETRY_AFTER = 120e3;
  async function repoGet(name, max, check) {
    const srcs = repoSources();
    if (srcs.length === 0) throw ERR_REPO_NONE;
    const ctl = new AbortController(), timer = setTimeout(() => ctl.abort(), 20000);
    let bytes;
    try {
      let resp;
      try { resp = await fetch(srcs[0] + name, { credentials: "omit", cache: "no-store", signal: ctl.signal }); }
      catch (e) { throw repoNetError(e && e.name === "AbortError" ? "连接超时，可能需要代理" : "连接失败，可能需要代理"); }
      if (resp.status === 404) throw ERR_REPO_NONE;
      if (resp.status !== 200) throw repoNetError("HTTP " + resp.status);
      try { bytes = new Uint8Array(await resp.arrayBuffer()); }
      catch (e) { throw repoNetError(e && e.name === "AbortError" ? "连接超时，可能需要代理" : "连接失败，可能需要代理"); }
    } finally { clearTimeout(timer); }
    if (bytes.length > max) throw ERR_REPO_TOO_LARGE;
    check(bytes);
    return bytes;
  }
  const kept = new Map();
  // readKept: a file of the repository, the copy read when it is fresh, else read again; offline says why a copy that
  // is shown could not be read again
  async function readKept(name, max, fresh, refresh, parse) {
    if (!kept.has(name)) kept.set(name, { val: null, stamp: "", fetched: 0, tried: 0, lastErr: null, turn: Promise.resolve() });
    const k = kept.get(name);
    const run = k.turn.then(async () => {
      let offline = "";
      const old = k.val === null || k.lastErr !== null || Date.now() - k.fetched > fresh;
      if (refresh || old && Date.now() - k.tried > RETRY_AFTER) {
        let v = null, stamp = "", e = null;
        try {
          const b = await repoGet(name, max, b => parse(b));
          ({ val: v, stamp } = parse(b));
        } catch (x) { e = goErr(x); }
        if (!e) {
          if (k.val === null || stamp >= k.stamp) { k.val = v; k.stamp = stamp; }
          k.fetched = Date.now(); k.tried = 0; k.lastErr = null;
        } else if (e === ERR_REPO_NONE) {
          Object.assign(k, { val: null, stamp: "", fetched: 0, tried: Date.now(), lastErr: e });
          throw e;
        } else {
          k.tried = Date.now(); k.lastErr = e;
          if (k.val === null) throw e;
          offline = e.message;
        }
      }
      if (k.val === null) throw k.lastErr || ERR_UNREAD;
      if (k.lastErr) offline = k.lastErr.message;
      return { val: k.val, offline };
    });
    k.turn = run.then(() => {}, () => {});
    return run;
  }
  function repoJSON(b) { return JSON.parse(utf8Out.decode(b).replace(/^\ufeff/, "")); }
  function stampOK(s) { return parseTime(s) ? s : ""; }
  // time.Parse(time.RFC3339, s): [seconds, nanoseconds] or null
  function parseTime(s) {
    const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d+)?(Z|([+-])(\d{2}):(\d{2}))$/.exec(s || "");
    if (!m) return null;
    const [y, mo, d, h, mi, se] = m.slice(1, 7).map(Number);
    const days = [31, y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
    if (mo < 1 || mo > 12 || d < 1 || d > days[mo - 1] || h > 23 || mi > 59 || se > 59) return null;
    let off = 0;
    if (m[8] !== "Z") {
      if (+m[10] > 23 || +m[11] > 59) return null;
      off = (m[9] === "-" ? -1 : 1) * (+m[10] * 3600 + +m[11] * 60);
    }
    const sec = Date.UTC(y, mo - 1, d, h, mi, se) / 1000 - off;
    const ns = m[7] ? Number((m[7].slice(1) + "000000000").slice(0, 9)) : 0;
    return [sec, ns];
  }
  // a time compared with another: -1, 0, 1 (what cannot be parsed is Go's zero time)
  function timeCmp(a, b) {
    const x = parseTime(a) || [-62135596800, 0], y = parseTime(b) || [-62135596800, 0];
    return x[0] !== y[0] ? Math.sign(x[0] - y[0]) : Math.sign(x[1] - y[1]);
  }
  function strictDecode(t, v, why) {
    const ctx = { bad: false }, out = decode(t, v, ctx);
    if (ctx.bad) throw new GoError(why);
    return out;
  }
  function parseList(t, why) {
    return b => {
      let v;
      try { v = repoJSON(b); } catch (e) { throw new GoError(why); }
      return strictDecode(t, v, why);
    };
  }
  function parseCreators(b) {
    const x = parseList("Creators", "社区创作者列表的格式有误")(b);
    const keep = [], seen = new Set();
    for (const e of x.creators || []) {
      try { tidyCreator(e); } catch (err) { goErr(err); continue; }
      if (seen.has(e.login)) continue;
      seen.add(e.login);
      keep.push(e);
      if (keep.length === ROWS_MAX) break;
    }
    x.creators = keep; x.updated = stampOK(x.updated);
    return { val: x, stamp: x.updated };
  }
  function parseWorks(b) {
    const x = parseList("Works", "社区作品列表的格式有误")(b);
    const keep = [], seen = new Set();
    for (const e of x.works || []) {
      try { tidyWorkEntry(e); } catch (err) { goErr(err); continue; }
      if (seen.has(e.id)) continue;
      seen.add(e.id);
      keep.push(e);
      if (keep.length === ROWS_MAX) break;
    }
    x.works = keep; x.updated = stampOK(x.updated);
    return { val: x, stamp: x.updated };
  }
  function parseCard(login) {
    return b => {
      const c = parseList("Card", "创作者名片的格式有误")(b);
      tidyCard(c);
      if (c.login !== login) throw new GoError("创作者名片与账号不符");
      return { val: c, stamp: c.updated };
    };
  }
  function parseWork(id) {
    return b => {
      const w = parseList("Work", "作品的格式有误")(b);
      tidyWork(w);
      if (w.id !== id) throw new GoError("作品与编号不符");
      return { val: w, stamp: w.updated > w.created ? w.updated : w.created };
    };
  }
  function lineOr(s, n, otherwise) { try { return line(s, n); } catch (e) { goErr(e); return otherwise; } }
  function cleanList(l, size, n) {
    let out = null;
    for (const s of l || []) {
      let c;
      try { c = line(s, size); } catch (e) { goErr(e); continue; }
      if (c !== "" && !contains(out, c) && (out || []).length < n) (out = out || []).push(c);
    }
    return out;
  }
  function cleanLangs(l) {
    let out = null;
    for (const s of l || []) if (contains(LANGS, s) && !contains(out, s)) (out = out || []).push(s);
    return out;
  }
  // cleanLinks: the links that are right, of the kinds allowed
  function cleanLinks(ls, allowed) {
    let out = null;
    for (const l of ls || []) {
      let c;
      try { c = checkLink(l, allowed); } catch (e) { goErr(e); continue; }
      if ((out || []).length < MAX.links) (out = out || []).push(c);
    }
    return out;
  }
  function birthdayOK(b) { return b && dateOK(b.month, b.day) ? { month: b.month, day: b.day } : null; }
  function tidyCreator(e) {
    e.login = normLogin(e.login);
    const nick = lineOr(e.nickname, MAX.nickname, null);
    e.nickname = nick === null || nick === "" ? e.login : nick;
    if (e.avatar !== "avatars/" + e.login + ".jpg") e.avatar = "";
    e.bases = cleanList(e.bases, MAX.baseName, MAX.bases); e.langs = cleanLangs(e.langs); e.birthday = birthdayOK(e.birthday);
    e.followers = Math.max(e.followers, 0); e.works = Math.max(e.works, 0); e.recipes = Math.max(e.recipes, 0);
    e.updated = stampOK(e.updated);
  }
  const VIDEO_KINDS = ["bilibili", "youtube", "x", "douyin", "xiaohongshu", "nicovideo", "tiktok"];
  function tidyWorkEntry(e) {
    if (!workIDOK(e.based_on) || e.based_on === e.id) e.based_on = "";
    if (!workIDOK(e.id) || e.id !== "w" + e.issue) throw new GoError("编号无效");
    e.author = normLogin(e.author);
    const title = lineOr(e.title, MAX.title, null);
    if (title === null || title === "") throw new GoError("标题无效");
    e.title = title;
    if (e.cover !== "covers/" + e.id + ".jpg") e.cover = "";
    e.base = lineOr(e.base, MAX.baseName, "");
    if (!contains(VIDEO_KINDS, e.video_kind)) e.video_kind = "";
    e.likes = Math.max(e.likes, 0); e.created = stampOK(e.created); e.updated = stampOK(e.updated);
  }
  function tidyCard(c) {
    try { c.showcase = checkShowcase(c.showcase); } catch (e) { goErr(e); c.showcase = null; }
    c.login = normLogin(c.login);
    const nick = lineOr(c.nickname, MAX.nickname, null);
    if (nick === null || nick === "") throw new GoError("创作者名片缺少昵称");
    c.nickname = nick;
    try { c.bio = cleanText(c.bio, MAX.bio, true, MAX.bioLines); } catch (e) { goErr(e); c.bio = ""; }
    try { c.vrchat = checkVRChat(c.vrchat); } catch (e) { goErr(e); c.vrchat = ""; }
    c.langs = cleanLangs(c.langs); c.links = cleanLinks(c.links, PROFILE_LINKS);
    c.bases = cleanList(c.bases, MAX.baseName, MAX.bases); c.birthday = birthdayOK(c.birthday);
    let fl = null;
    for (const f of c.following || []) {
      let l;
      try { l = normLogin(f); } catch (e) { goErr(e); continue; }
      if (!contains(fl, l) && (fl || []).length < MAX.following) (fl = fl || []).push(l);
    }
    c.following = fl;
    if (c.avatar !== "avatars/" + c.login + ".jpg") c.avatar = "";
    c.issue = Math.max(c.issue, 0); c.updated = stampOK(c.updated);
  }
  function tidyWork(w) {
    if (!workIDOK(w.based_on) || w.based_on === w.id) w.based_on = "";
    if (!workIDOK(w.id)) throw new GoError("作品编号无效");
    w.author = normLogin(w.author);
    const title = lineOr(w.title, MAX.title, null);
    if (title === null || title === "") throw new GoError("作品缺少标题");
    w.title = title;
    try { w.desc = cleanText(w.desc, MAX.desc, true, MAX.descLines); } catch (e) { goErr(e); w.desc = ""; }
    if (videoKind(w.video) === "") w.video = "";
    w.base = lineOr(w.base, MAX.baseName, "");
    let as = null;
    for (const a of w.assets || []) {
      if ((as || []).length === MAX.assets) break;
      const name = lineOr(a.name, MAX.assetName, null);
      if (name === null || name === "") continue;
      const shop = lineOr(a.shop, MAX.shopName, "");
      const u = linkKindOf(a.url, ["booth_item", "gumroad", "jinxxy"]) !== "" ? a.url : "";
      (as = as || []).push({ name, url: u, shop });
    }
    w.assets = as; w.links = cleanLinks(w.links, WORK_LINKS);
    if (!recipeCodeOK(w.recipe)) w.recipe = "";
    if (w.cover !== "covers/" + w.id + ".jpg") w.cover = "";
    w.issue = Math.max(w.issue, 0); w.created = stampOK(w.created); w.updated = stampOK(w.updated);
  }
  function repoCreators(refresh) { return readKept("creators.json", LIST_MAX, LIST_FRESH, refresh, parseCreators); }
  function repoWorks(refresh) { return readKept("works.json", LIST_MAX, LIST_FRESH, refresh, parseWorks); }
  function repoCard(login, refresh) { return readKept("profiles/" + login + ".json", FILE_MAX, FILE_FRESH, refresh, parseCard(login)); }
  function repoWork(id, refresh) { return readKept("works/" + id + ".json", FILE_MAX, FILE_FRESH, refresh, parseWork(id)); }

  // ---------- social/rows.go: what the page is given ----------
  // A picture is the service's own address. (The program shows them through its picture cache, /cpic?u=<the same
  // address>; a page on the service's origin loads them as they are.)

  const RE_ACCOUNT_ID = /^u[0-9a-z]{12}$/;
  function accountIDOK(id) { return RE_ACCOUNT_ID.test(id); }
  function followTarget(id, login) { return id !== "" ? id : login !== "" ? "gh:" + login : ""; }
  function profileWho(p) {
    let id = p.id || "", github = p.github || "";
    if (id.startsWith("gh:")) { if (github === "") github = id.slice(3); id = ""; }
    return [id, github];
  }
  function servicePic(path) {
    if (BASE === "" || !path || !path.startsWith("/")) return "";
    if (!path.startsWith("/pic/") && !path.startsWith("/repo/")) return "";
    return BASE + path;
  }
  function profilePic(p) {
    const av = p.avatar || "";
    if (av === "") return "";
    if (av.startsWith("/")) return servicePic(av);
    if (blen(av) === 48 && servicePic("/pic/" + av + ".jpg") !== "") return servicePic("/pic/" + av + ".jpg");
    return "";
  }
  function creatorRow(p, fl, meID, meLogin) {
    const [id, github] = profileWho(p);
    const counts = p.counts || zero("ProfileCounts"), target = followTarget(id, github);
    return Object.assign(zero("CreatorRow"), { login: github, nickname: p.nickname, bases: p.bases, langs: p.langs,
      birthday: p.birthday, followers: counts.followers, works: counts.works, recipes: counts.recipes, updated: p.created,
      id, github, source: p.source, pic: profilePic(p), target, followed: !!fl.get(target), me: id !== "" && id === meID,
      followsMe: p.follows_you === true, readOnly: p.read_only, booth: p.booth });
  }
  function repoCreatorRow(c, fl, meLogin) {
    const target = followTarget("", c.login);
    return Object.assign(zero("CreatorRow"), c, { github: c.login, source: "github", target,
      pic: avatarPic(c.login, c.avatar, c.updated), followed: !!fl.get(target), me: meLogin !== "" && c.login === meLogin });
  }
  function workRow(w, author, fl, meID, meLogin) {
    let id = "", login = w.author;
    if (w.source.startsWith("service") || accountIDOK(w.author)) { id = w.author; login = ""; }
    fl = fl || new Map();
    const row = Object.assign(zero("WorkRow"), w, { pic: servicePic(w.cover), nickname: w.author, authorId: id, target: followTarget(id, login) });
    row.followed = !!fl.get(row.target);
    row.mine = id !== "" && id === meID || id === "" && meLogin !== "" && equalFold(login, meLogin);
    if (author) {
      row.nickname = author.nickname;
      row.authorPic = profilePic(author);
      row.authorBooth = author.booth || "";
      if (row.authorId === "") row.authorId = profileWho(author)[0];
      if (row.target === "") { row.target = followTarget(author.id, login); row.followed = !!fl.get(row.target); }
    } else if (login !== "") row.authorPic = avatarPic(login, "", "");
    return row;
  }
  function repoWorkRow(w, profile, byLogin, fl, meLogin) {
    const row = Object.assign(zero("WorkRow"), w, { pic: repoPic(w.cover, w.updated > w.created ? w.updated : w.created),
      nickname: w.author, issueUrl: issueURL(w.issue) });
    row.target = followTarget("", w.author);
    row.followed = !!fl.get(row.target);
    row.mine = meLogin !== "" && equalFold(w.author, meLogin);
    if (profile) {
      row.authorId = profileWho(profile)[0];
      row.nickname = profile.nickname; row.authorPic = profilePic(profile); row.authorBooth = profile.booth || "";
    } else if (byLogin.has(w.author)) {
      const c = byLogin.get(w.author);
      row.nickname = c.nickname; row.authorPic = avatarPic(c.login, c.avatar, c.updated);
    } else row.authorPic = avatarPic(w.author, "", "");
    return row;
  }
  function issueURL(n) { return n <= 0 ? "" : "https://github.com/" + REPO + "/issues/" + n; }
  function repoPic(path, v) {
    const srcs = repoSources();
    if (!path || srcs.length === 0) return "";
    let u = trimSpace(srcs[0]).replace(/\/+$/, "") + "/" + path;
    if (v) u += "?v=" + queryEscape(v);
    return u;
  }
  // avatarPic: a creator's own picture in the repository, else their GitHub avatar (loaded from GitHub as it is)
  function avatarPic(login, avatar, updated) {
    if (avatar) return repoPic(avatar, updated);
    if (!login) return "";
    return "https://avatars.githubusercontent.com/" + login + "?s=160";
  }
  // ownedBoothIDs, libraryCards: the library's Booth items and their cards. A browser has no library: none.
  function ownedBoothIDs() { return new Map(); }
  function libraryCards() { return new Map(); }
  function shopOf(l) {
    if (l.kind !== "booth_shop") return "";
    const u = parseURL(l.url);
    if (!u) return "";
    const host = goLower(fromBytes(urlHostname(u)));
    if (!host.endsWith(".booth.pm")) return "";
    const sub = host.slice(0, -".booth.pm".length);
    return sub.includes(".") ? "" : sub;
  }

  // ---------- social/community.go: the service's lists, else the repository's copy ----------

  const COMMUNITY_ROWS = 100;
  async function allWorks() {
    const q = { sort: "latest" };
    let out = [];
    for (let page = 0; page < COMMUNITY_ROWS / 20; page++) {
      const p = await svc.listWorks(q);
      out = out.concat(p.works || []);
      if (p.next === "" || out.length >= COMMUNITY_ROWS) break;
      q.after = p.next;
    }
    return out.slice(0, COMMUNITY_ROWS);
  }
  async function allRecipes() {
    let out = [], after = "";
    for (let page = 0; page < COMMUNITY_ROWS / 20; page++) {
      const q = {};
      if (after !== "") q.after = after;
      const p = await svc.listRecipes(q);
      out = out.concat(p.recipes || []);
      if (p.next === "" || out.length >= COMMUNITY_ROWS) break;
      after = p.next;
    }
    return out.slice(0, COMMUNITY_ROWS);
  }
  function addProfile(c, p) {
    if (p.id !== "" && accountIDOK(p.id)) c.profiles.set(p.id, p);
    if (p.github) c.profiles.set("gh:" + p.github, p);
    if (p.id.startsWith("gh:")) c.profiles.set(p.id, p);
  }
  // loadCommunity: the lists and the follows; refresh asks the service again
  async function loadCommunity(refresh) {
    const c = { service: false, works: null, recipes: null, profiles: new Map(), following: new Map(), birthdays: [],
      repo: new Map(), meID: "", meLogin: "", offline: "", closed: "" };
    const d = localCopy(), a = accountCopy();
    c.meID = a.acc.id; c.meLogin = a.acc.github;
    if (d.login !== "") c.meLogin = d.login;
    if (a.token !== "") {
      let f = null, err = null;
      try { f = await svc.following(a.token); } catch (e) { err = goErr(e); }
      if (!err) {
        for (const x of f.following || []) { c.following.set(x.target, true); if (x.account) addProfile(c, x.account); }
        try {
          const b = await svc.birthdays(a.token);
          for (const p of b.accounts || []) { c.birthdays.push(p); addProfile(c, p); }
        } catch (e) { goErr(e); }
      } else if (err.message !== "") {
        c.offline = err.message;
        for (const x of d.following) c.following.set("gh:" + x.login, true);
      }
    } else for (const x of d.following) c.following.set("gh:" + x.login, true);
    if (BASE !== "") {
      try { c.works = await allWorks(); c.service = true; }
      catch (e) { c.offline = firstOf(c.offline, goErr(e).message); await readRepoWorks(c, refresh); }
      try { for (const e of await allRecipes()) (c.recipes = c.recipes || []).push(recipeRefOf(e, true)); }
      catch (e) { c.offline = firstOf(c.offline, goErr(e).message); readRepoRecipes(c); }
    } else { await readRepoWorks(c, refresh); readRepoRecipes(c); }
    try { for (const e of (await repoCreators(false)).val.creators || []) c.repo.set(e.login, e); } catch (e) { goErr(e); }
    await readAuthorProfiles(c, a.token, refresh);
    return c;
  }
  async function readRepoWorks(c, refresh) {
    try { c.works = ((await repoWorks(refresh)).val.works || []).slice(); }
    catch (e) {
      goErr(e);
      if (e === ERR_REPO_NONE && c.service) c.closed = e.message;
      else c.offline = firstOf(c.offline, e.message);
    }
  }
  // readRepoRecipes: the repository's recipe list comes from the program's recipe module (internal/ai), which a
  // browser does not have: as the program without it (social.Recipes nil), there are none and nothing to say
  function readRepoRecipes() {}
  async function readAuthorProfiles(c, token, refresh) {
    if (BASE === "") return;
    const seen = new Set();
    for (const w of c.works || []) {
      if (!accountIDOK(w.author) || seen.has(w.author)) continue;
      seen.add(w.author);
      try { addProfile(c, await cachedProfile(w.author, token, refresh)); } catch (e) { goErr(e); }
    }
  }
  function recipeRefOf(e, service) {
    const r = Object.assign(zero("RecipeRef"), { id: e.id, name: e.name, note: e.note, base: e.base, author: e.author, login: e.login,
      added: e.added, items: e.items, assets: (e.assets || []).length, links: e.links, source: e.source, account: e.account, file: e.file });
    if (service && r.source === "") r.source = "service";
    return r;
  }
  function workAccountID(w) { return accountIDOK(w.author) ? w.author : ""; }
  function workLogin(w) { return accountIDOK(w.author) ? "" : w.author; }
  function creatorRows(c) {
    const byKey = new Map();
    for (const w of c.works || []) {
      const key = followTarget(workAccountID(w), workLogin(w));
      if (key === "" || byKey.has(key)) continue;
      const id = workAccountID(w);
      if (id !== "") {
        if (c.profiles.has(id)) { byKey.set(key, creatorRow(c.profiles.get(id), c.following, c.meID, c.meLogin)); continue; }
        byKey.set(key, Object.assign(zero("CreatorRow"), { id, source: "service", nickname: id, works: 1 }));
        continue;
      }
      const login = workLogin(w);
      if (c.profiles.has("gh:" + login)) { byKey.set(key, creatorRow(c.profiles.get("gh:" + login), c.following, c.meID, c.meLogin)); continue; }
      if (c.repo.has(login)) { byKey.set(key, repoCreatorRow(c.repo.get(login), c.following, c.meLogin)); continue; }
      byKey.set(key, Object.assign(zero("CreatorRow"), { github: login, source: "github", pic: avatarPic(login, "", ""),
        followed: !!c.following.get(key), login, nickname: login }));
    }
    const onService = new Set();
    for (const r of byKey.values()) if (r.github !== "") onService.add(r.github.toLowerCase());
    for (const [login, e] of c.repo) {
      if (onService.has(login.toLowerCase())) continue;
      const key = followTarget("", login);
      if (!byKey.has(key)) byKey.set(key, repoCreatorRow(e, c.following, c.meLogin));
    }
    for (const p of c.profiles.values()) {
      if (!accountIDOK(p.id) && !p.id.startsWith("gh:")) continue;
      const key = followTarget(p.id, p.github || "");
      if (key === "" || !c.following.get(key)) continue;
      if (!byKey.has(key)) byKey.set(key, creatorRow(p, c.following, c.meID, c.meLogin));
    }
    // (Go collects these from maps, so the order of rows that compare equal below is not fixed there either)
    const rows = [...byKey.values()];
    rows.sort((a, b) => a.followers !== b.followers ? b.followers - a.followers
      : a.works + a.recipes !== b.works + b.recipes ? b.works + b.recipes - (a.works + a.recipes)
        : a.updated > b.updated ? -1 : a.updated < b.updated ? 1 : 0);
    return rows;
  }
  function workRows(c) {
    const rows = [];
    for (const w of c.works || []) {
      let profile = null;
      const id = workAccountID(w);
      if (id !== "") { if (c.profiles.has(id)) profile = c.profiles.get(id); }
      else if (c.profiles.has("gh:" + w.author)) profile = c.profiles.get("gh:" + w.author);
      if (c.service) {
        const row = workRow(w, profile, c.following, c.meID, c.meLogin);
        if (!profile && !accountIDOK(w.author) && c.repo.has(w.author)) {
          const e = c.repo.get(w.author);
          row.nickname = e.nickname; row.authorPic = avatarPic(e.login, e.avatar, e.updated);
        }
        rows.push(row);
      } else rows.push(repoWorkRow(w, profile, c.repo, c.following, c.meLogin));
    }
    return stableSort(rows, (a, b) => workNewer(a, b) ? -1 : workNewer(b, a) ? 1 : 0);
  }
  function stableSort(list, cmp) { return list.map((x, i) => [x, i]).sort((a, b) => cmp(a[0], b[0]) || a[1] - b[1]).map(x => x[0]); }
  function author(c, id, login) {
    const t = followTarget(id, login);
    if (c.profiles.has(t)) return creatorRow(c.profiles.get(t), c.following, c.meID, c.meLogin);
    if (login !== "") {
      if (c.profiles.has("gh:" + login)) return creatorRow(c.profiles.get("gh:" + login), c.following, c.meID, c.meLogin);
      if (c.repo.has(login)) return repoCreatorRow(c.repo.get(login), c.following, c.meLogin);
    }
    if (id !== "") return Object.assign(zero("CreatorRow"), { id, source: "service", nickname: id });
    return Object.assign(zero("CreatorRow"), { github: login, source: "github", login, nickname: login, pic: avatarPic(login, "", "") });
  }
  function recipesOfCreator(c, id, login) {
    return (c.recipes || []).filter(r => id !== "" && r.account === id || login !== "" && equalFold(r.login, login));
  }

  // ---------- social/remix.go ----------

  function newcomers(rows, now) {
    const counts = new Map();
    for (const w of rows) counts.set(w.author, (counts.get(w.author) || 0) + 1);
    const week = Math.floor(now / 1000) - 7 * 24 * 3600, nowSec = now / 1000;
    const out = rows.filter(w => {
      const t = parseTime(w.created);
      if (!t || counts.get(w.author) !== 1) return false;
      const at = t[0] + t[1] / 1e9;
      return at >= week && at <= nowSec;
    });
    return stableSort(out, (a, b) => workNewer(a, b) ? -1 : workNewer(b, a) ? 1 : 0);
  }
  function workNewer(a, b) {
    const c = timeCmp(a.created, b.created);
    return c === 0 ? a.id > b.id : c > 0;
  }
  function remixCount(rows, login) {
    const owned = new Set();
    for (const w of rows || []) if (w.author === login) owned.add(w.id);
    return (rows || []).filter(w => w.author !== login && owned.has(w.based_on)).length;
  }

  // ---------- social/feed.go ----------

  function dayOf(t) {
    const d = new Date(t);
    return d.getFullYear() + "-" + String(d.getMonth() + 1).padStart(2, "0") + "-" + String(d.getDate()).padStart(2, "0");
  }
  const workKey = id => "work:" + id, recipeKey = id => "recipe:" + id, birthdayKey = (l, d) => "birthday:" + l + ":" + d;
  function workTarget(w) { return followTarget(workAccountID(w), workLogin(w)); }
  function recipeTarget(r) { return followTarget(r.account, r.login); }
  function buildFeed(c, d, now) {
    const f = Object.assign(zero("s.Feed"), { items: [], following: c.following.size, followingBases: d.bases.length,
      siteWide: c.following.size + d.bases.length === 0, today: dayOf(now) });
    const bases = new Set(d.bases.map(b => b.base)), seen = new Set(d.seen);
    for (const w of workRows(c)) {
      const target = workTarget(w);
      if (!c.following.get(target) && !bases.has(w.base) && !f.siteWide) continue;
      w.followed = !!c.following.get(target);
      const creator = author(c, workAccountID(w), workLogin(w));
      f.items.push(Object.assign(zero("s.FeedItem"), { key: workKey(w.id), kind: "work", creator, at: w.created,
        new: !f.siteWide && !seen.has(workKey(w.id)), work: w }));
    }
    for (const r of c.recipes || []) {
      if (!c.following.get(recipeTarget(r))) continue;
      f.items.push(Object.assign(zero("s.FeedItem"), { key: recipeKey(r.id), kind: "recipe", creator: author(c, r.account, r.login),
        at: r.added, new: !seen.has(recipeKey(r.id)), recipe: r }));
    }
    f.items = stableSort(f.items, (x, y) => {
      const a = x.at, b = y.at, da = a.split("T")[0], db = b.split("T")[0];
      if (da !== db) return da > db ? -1 : 1;
      return a > b ? -1 : a < b ? 1 : 0;
    });
    if (f.items.length > FEED_MAX) f.items = f.items.slice(0, FEED_MAX);
    const today = new Map();
    for (const p of c.birthdays) if (p.birthday) today.set(p.id, p);
    for (const target of c.following.keys()) {
      const p = c.profiles.get(target);
      if (p && p.birthday && p.show_birthday) today.set(target, p);
    }
    const bd = [], at = new Date(now);
    for (const [target, p] of today) {
      if (!p.birthday || p.birthday.month !== at.getMonth() + 1 || p.birthday.day !== at.getDate()) continue;
      bd.push(Object.assign(zero("s.FeedItem"), { key: birthdayKey(target, f.today), kind: "birthday",
        creator: creatorRow(p, c.following, c.meID, c.meLogin), at: f.today,
        new: d.birthdays !== f.today && !seen.has(birthdayKey(target, f.today)) }));
    }
    bd.sort((x, y) => x.creator.nickname < y.creator.nickname ? -1 : x.creator.nickname > y.creator.nickname ? 1 : 0);
    f.items = bd.concat(f.items);
    for (const it of f.items) if (it.new) f.unseen++;
    return f;
  }
  async function addNotices(f, c, ws) {
    const a = accountCopy();
    if (a.token === "") return;
    let n;
    try { n = await readNotices(0); } catch (e) { f.noticeError = goErr(e).message; return; }
    for (const notice of n.notices || []) {
      const w = Object.assign(zero("WorkRow"), { id: notice.work, title: notice.work });
      for (const entry of ws || []) if (entry.id === notice.work) { Object.assign(w, entry); break; }
      const fresh = notice.seq > a.seen;
      if (fresh) f.unseen++;
      const from = (notice.from || [])[0] || zero("Person");
      const creator = author(c, from.id, from.github);
      if (from.nickname !== "") creator.nickname = from.nickname;
      if (from.avatar !== "") creator.pic = profilePic(from);
      f.items.push(Object.assign(zero("s.FeedItem"), { key: "notice:" + notice.seq, kind: "notice", notice, work: w,
        at: notice.at, new: fresh, creator }));
    }
    f.items = stableSort(f.items, (x, y) => x.at > y.at ? -1 : x.at < y.at ? 1 : 0);
  }
  function keysOf(c, target) {
    const keys = [];
    for (const w of c.works || []) if (workTarget(w) === target) keys.push(workKey(w.id));
    for (const r of c.recipes || []) if (recipeTarget(r) === target) keys.push(recipeKey(r.id));
    return keys;
  }

  // ---------- social/posts.go, timeline.go, clone.go, asks.go: posts as the page shows them ----------

  const RE_POST_ID = /^[pcmg][0-9]{1,18}$/;
  function postIDOK(id) { return RE_POST_ID.test(id); }
  function shareURL(id) { return BASE === "" || !postIDOK(id) ? "" : BASE + "/p/" + id; }
  function postRow(p, me, fl) {
    const row = Object.assign({}, p, { pics: [], pic: "", authorPic: "", mine: p.author.id !== "" && p.author.id === me,
      followed: !!fl && fl.has(p.author.id), share: "" });
    if (!p.deleted) row.share = shareURL(p.id);
    for (const id of p.pictures || []) { const src = servicePic("/pic/" + id + ".jpg"); if (src !== "") row.pics.push(src); }
    if (row.pics.length > 0) row.pic = row.pics[0];
    if (p.author.avatar !== "") row.authorPic = profilePic(p.author);
    row.boothCards = boothCards(p.booth_items);
    return row;
  }
  // boothCards: the Booth items a bot's post presents, as the page shows them (none is owned: a browser has no library)
  function boothCards(items) {
    if (!items || items.length === 0) return null;
    const out = [];
    for (const it of items) {
      if (!boothIDOK(it.id)) continue;
      const c = { id: it.id, title: it.title, shop: it.shop || "", price: it.price, was: it.was || 0, kind: it.kind, base: it.base || "", pic: "", owned: false };
      if (blen(it.picture || "") === 48) c.pic = servicePic("/pic/" + it.picture + "_s.jpg");
      out.push(c);
    }
    return out;
  }
  function workEntryOf(pw) {
    return Object.assign(zero("WorkEntry"), { source: pw.source, id: pw.id, author: pw.author, title: pw.title, cover: pw.cover,
      base: pw.base, video_kind: videoKind(pw.video), has_recipe: pw.recipe !== "", issue: pw.issue, created: pw.created, updated: pw.updated });
  }
  function boothIDOK(id) { return id !== "" && boothItemID("https://booth.pm/items/" + id) === id; }
  function boothItemURL(id) { return "https://booth.pm/ja/items/" + id; }
  function workItems(assets, owned, cards) {
    const out = [], seen = new Set();
    for (const a of assets || []) {
      const id = boothItemID(a.url), key = id !== "" ? id : a.name + "\0" + a.url;
      if (seen.has(key)) continue;
      seen.add(key);
      const it = Object.assign(zero("WorkItem"), { name: a.name, url: a.url, boothId: id, owned: id !== "" && !!owned.get(id) });
      const card = cards.get(id);
      if (card && it.owned) { it.key = card.key; it.category = card.category; }
      out.push(it);
    }
    return out;
  }
  function askItems(ids, owned, cards) {
    const out = [];
    for (const id of ids || []) {
      if (!boothIDOK(id)) continue;
      const it = Object.assign(zero("WorkItem"), { url: boothItemURL(id), boothId: id, owned: !!owned.get(id) });
      const card = cards.get(id);
      if (card) { it.name = card.name; if (it.owned) { it.key = card.key; it.category = card.category; } }
      out.push(it);
    }
    return out;
  }
  // timelineRows: posts as rows, each work read once per answer, quotes two deep
  function timelineRows(a) {
    const works = new Map(), owned = ownedBoothIDs(), cards = libraryCards();
    async function row(p, depth) {
      const r = Object.assign(postRow(p, a.acc.id, null), { quote: null, workCard: null, assetItems: null });
      if (p.quote && depth < 2) r.quote = await row(p.quote, depth + 1);
      if (workIDOK(p.work)) {
        if (!works.has(p.work)) {
          let w = null;
          try {
            const pw = await svc.getWork(p.work);
            w = Object.assign({}, pw, { pic: servicePic(pw.cover), owned: 0, total: 0, items: workItems(pw.assets, owned, cards) });
            for (const it of w.items) { if (it.boothId === "") continue; w.total++; if (it.owned) w.owned++; }
          } catch (e) { goErr(e); }
          works.set(p.work, w);
        }
        r.workCard = works.get(p.work);
      }
      if ((p.assets || []).length > 0) r.assetItems = askItems(p.assets, owned, cards);
      return r;
    }
    return p => row(p, 0);
  }
  function peopleRows(profiles) { return (profiles || []).map(p => Object.assign({}, p, { pic: profilePic(p) })); }
  // libraryBases: the bases of the library's 素体 cards and their other names. A browser has no library: none.
  function libraryBases() { return [[], []]; }
  function groupRowOf(g) { return Object.assign({}, g, { pinned: null, pic: servicePic(g.cover) }); }

  // ---------- social/api.go: the pieces the routes share ----------

  function commentIDOK(id) { return id.length >= 2 && id.length <= 21 && /^c[0-9]+$/.test(id); }
  function pictureIDOK(id) { return /^[0-9a-f]{48}$/.test(id); }
  function draftOf(a) {
    // (Go reads show_birthday through the account's card fields even when it has none, which it then cannot answer;
    // false here)
    const dr = Object.assign(zero("Draft"), { showBirthday: a.$CardFields ? a.show_birthday : false, showFollowing: a.show_following });
    if (a.$CardFields) {
      Object.assign(dr, { nickname: a.nickname, bio: a.bio, langs: a.langs, vrchat: a.vrchat, links: a.links, bases: a.bases,
        birthday: a.birthday, showcase: a.showcase, contacts: a.contacts });
      dr.showShowcase = (a.showcase || []).length > 0;
    }
    return dr;
  }
  // FillKinds (send.go): each link given the kind its address is of
  function fillKinds(ls, allowed) {
    const out = [];
    for (const l of ls || []) {
      if (trimSpace(l.url) === "" && trimSpace(l.label) === "") continue;
      const c = Object.assign({}, l);
      if (c.kind === "") {
        c.kind = linkKindOf(c.url, allowed);
        if (c.kind === "") {
          const r = [...trimSpace(l.url)];
          throw fail("bad_link", "无法识别的链接：" + (r.length > 60 ? r.slice(0, 60).join("") + "…" : r.join("")));
        }
      }
      out.push(c);
    }
    return out;
  }
  // accountReq: the fields of 编辑资料 that were sent, checked (an AccountReq as Go marshals it)
  function accountReq(b) {
    const out = {};
    if (has(b, "nickname")) {
      const nick = cleanText(str(b, "nickname"), MAX.nickname, false, 0);
      if (nick === "") throw new GoError("请填写昵称");
      out.nickname = nick;
    }
    if (has(b, "bio")) out.bio = str(b, "bio");
    if (has(b, "avatar")) {
      const v = str(b, "avatar");
      if (v !== "" && !pictureIDOK(v)) throw fail("bad_picture");
      out.avatar = v;
    }
    if (has(b, "langs")) {
      const r = unmarshal(b, "langs", "[]string", null);
      if (!r.ok) throw fail("bad_request");
      out.langs = r.v;
    }
    if (has(b, "vrchat")) out.vrchat = checkVRChat(str(b, "vrchat"));
    if (has(b, "links")) {
      const r = unmarshal(b, "links", "[]Link", null);
      if (!r.ok) throw fail("bad_request");
      out.links = marshal("[]Link", fillKinds(r.v, PROFILE_LINKS));
    }
    if (has(b, "bases")) {
      const r = unmarshal(b, "bases", "[]string", null);
      if (!r.ok) throw fail("bad_request");
      out.bases = r.v;
    }
    if (has(b, "birthday") && b.birthday !== null) {
      const r = unmarshal(b, "birthday", "Birthday", zero("Birthday"));
      if (!r.ok || !dateOK(r.v.month, r.v.day)) throw fail("bad_request", "生日日期不正确");
      out.birthday = r.v;
    }
    for (const [k, key] of [["showBirthday", "show_birthday"], ["showFollowing", "show_following"]]) {
      if (has(b, k)) { const r = unmarshal(b, k, "bool", false); if (r.ok) out[key] = r.v; }
    }
    if (has(b, "showcase")) {
      const r = unmarshal(b, "showcase", "[]ShowcaseItem", null);
      if (!r.ok) throw fail("bad_request");
      out.showcase = marshal("[]ShowcaseItem", checkShowcase(r.v) || []);
    }
    if (has(b, "contacts") && b.contacts !== null) {
      const r = unmarshal(b, "contacts", "Contacts", zero("Contacts"));
      if (!r.ok) throw fail("bad_request");
      out.contacts = marshal("Contacts", checkContacts(r.v) || zero("Contacts"));
    }
    if (has(b, "auto_flag") && typeof out.avatar === "string" && out.avatar !== "" && b.auto_flag === true) out.auto_flag = true;
    // (Go's field order)
    const order = ["handle", "allow_requests", "langs", "vrchat", "links", "bases", "birthday", "show_birthday", "showcase",
      "contacts", "nickname", "bio", "avatar", "dm_policy", "show_following", "show_sensitive", "auto_flag"];
    const req = {};
    for (const k of order) if (has(out, k)) req[k] = out[k];
    return req;
  }
  function workReqOf(b) {
    const r = unmarshal(b, "work", "WorkReq", zero("WorkReq"));
    if (!r.ok) throw new GoError("作品内容有误");
    const w = r.v;
    w.links = fillKinds(w.links, WORK_LINKS);
    w.cover = ""; w.replace = "";
    return w;
  }
  // pictureBodies: the pictures sent as base64
  function pictureBodies(b) {
    let list = [];
    if (has(b, "pictures")) {
      const r = unmarshal(b, "pictures", "[]string", null);
      if (!r.ok) throw new GoError("图片内容有误");
      list = r.v || [];
    }
    if (list.length > MAX.postPictures) throw fail("too_long");
    return list.map(s => {
      const data = b64Decode(s);
      if (!data || data.length === 0 || data.length > MAX.pictureBytes) throw fail("bad_picture");
      return data;
    });
  }
  async function uploadPictures(token, pics) {
    const ids = [];
    for (const data of pics) ids.push((await uploadPicture(token, "post", data)).id);
    return ids;
  }
  function publishedAnswer(a, pw) {
    const authorP = Object.assign(zero("Profile"), { id: a.acc.id, nickname: a.acc.nickname, github: a.acc.github || "", avatar: a.acc.avatar });
    const row = workRow(Object.assign(zero("WorkEntry"), { source: pw.source, sensitive: pw.sensitive, id: pw.id, author: pw.author,
      title: pw.title, cover: pw.cover, base: pw.base, video_kind: videoKind(pw.video), has_recipe: pw.recipe !== "", issue: pw.issue,
      created: pw.created, updated: pw.updated }), authorP, null, a.acc.id, a.acc.github || "");
    row.mine = true;
    const pics = (pw.pictures || []).map(id => servicePic("/pic/" + id + ".jpg"));
    return { ok: true, work: marshal("PublishedWork", pw), row: marshal("WorkRow", row), pic: row.pic, pics };
  }
  function followTargetOf(b) {
    const raw = str(b, "target");
    if (raw === "") {
      const id = str(b, "id"), login = str(b, "login");
      if (id !== "") { if (!accountIDOK(id)) throw fail("bad_request"); return id; }
      if (login !== "") return "gh:" + normLogin(login.replace(/^gh:/, ""));
      throw fail("bad_request");
    }
    if (accountIDOK(raw)) return raw;
    if (raw.startsWith("gh:")) return "gh:" + normLogin(raw.slice(3));
    return "gh:" + normLogin(raw);
  }
  async function followingCount(token, target, on) {
    if (token === "") return 0;
    let f;
    try { f = await svc.following(token); } catch (e) { goErr(e); return 0; }
    let n = 0;
    for (const x of f.following || []) { if (x.target === target) { if (on) n++; continue; } n++; }
    return n;
  }
  async function accountTarget(token, raw) {
    if (accountIDOK(raw)) return raw;
    const login = normLogin(raw.replace(/^gh:/, ""));
    const p = await svc.profileByGitHub(token, login);
    if (!accountIDOK(p.id)) throw new GoError("该创作者还没有社区账号");
    return p.id;
  }
  function showcaseRows(items) {
    const owned = ownedBoothIDs();
    return (items || []).map(item => Object.assign(zero("WorkAssetRow"), { name: item.name, shop: item.shop,
      url: "https://booth.pm/ja/items/" + item.booth_id, kind: "booth_item", boothId: item.booth_id, owned: !!owned.get(item.booth_id) }));
  }
  function shopsOf(card) {
    const out = [];
    for (const l of card.links || []) {
      const sub = shopOf(l);
      if (sub !== "") out.push({ sub, name: l.label !== "" ? l.label : card.nickname || "" });
    }
    return out;
  }
  function shopsOfProfile(p) { return shopsOf({ links: p.links, nickname: "" }); }
  function serviceUnavailable(e) { return e instanceof ServiceError && e.unavailable; }
  // profilePage: another player's page, a service account or an older GitHub card
  async function profilePage(id, github, refresh) {
    const a = accountCopy();
    let p;
    try { p = id !== "" ? await svc.profile(a.token, id) : await svc.profileByGitHub(a.token, github); }
    catch (e) {
      goErr(e);
      if (github !== "" && (BASE === "" || serviceUnavailable(e))) return repoProfilePage(github, refresh, e);
      return { ok: false, err: e.message };
    }
    return profileAnswer(p, profilePic(p), refresh);
  }
  async function repoProfilePage(login, refresh, why) {
    let c, off = "";
    try { ({ val: c, offline: off } = await repoCard(login, refresh)); }
    catch (e) { goErr(e); return { ok: false, err: firstOf(off, why.message, e.message) }; }
    const p = Object.assign(zero("Profile"), { id: "gh:" + login, github: login, nickname: c.nickname, langs: c.langs, vrchat: c.vrchat,
      links: c.links, bases: c.bases, birthday: c.birthday, show_birthday: c.birthday !== null, showcase: c.showcase,
      source: "github", read_only: true, bio: c.bio, created: c.updated });
    const d = localCopy(), a = accountCopy(), me = firstOf(a.acc.github, d.login);
    if (me !== "" && (c.following || []).some(l => equalFold(l, me))) p.follows_you = true;
    const res = await profileAnswer(p, avatarPic(login, c.avatar, c.updated), refresh);
    if (off !== "") res.offline = off;
    return res;
  }
  async function profileAnswer(p, pic, refresh) {
    p = Object.assign({}, p, { links: cleanLinks(p.links, PROFILE_LINKS) });
    const c = await loadCommunity(refresh);
    let target = followTarget(p.id, p.github);
    if (p.id.startsWith("gh:")) target = p.id;
    let followed = !!c.following.get(target);
    if (p.following !== null) followed = p.following;
    let works = [];
    if (p.source === "service") { for (const e of p.works || []) works.push(workRow(e, p, c.following, c.meID, c.meLogin)); }
    else for (const w of c.works || []) {
      if (workAccountID(w) === "" && equalFold(workLogin(w), p.github)) works.push(workRow(w, p, c.following, c.meID, c.meLogin));
    }
    works = stableSort(works, (x, y) => workNewer(x, y) ? -1 : workNewer(y, x) ? 1 : 0);
    const recipes = p.source === "service" ? (p.recipes || []).map(e => recipeRefOf(e, true)) : recipesOfCreator(c, p.id, p.github);
    const login = p.github;
    let who = p.id;
    if (who.startsWith("gh:") || who === "") who = login;
    const me = p.id !== "" && p.id === c.meID || login !== "" && equalFold(login, c.meLogin);
    const res = { ok: true, profile: marshal("Profile", p), id: p.id, github: p.github, nickname: p.nickname, bio: p.bio, pic,
      source: p.source, readOnly: p.read_only, showcase: marshal("[]WorkAssetRow", showcaseRows(p.showcase)),
      works: marshal("[]WorkRow", works), recipes: marshal("[]RecipeRef", recipes), followed, followsMe: p.follows_you === true, me,
      canEdit: me && !p.read_only, counts: marshal("ProfileCounts", p.counts), remixCount: remixCount(c.works, who),
      following: c.following.size, shops: shopsOfProfile(p) };
    if (p.github_card) { res.githubCard = marshal("Card", p.github_card); res.githubPic = repoPic(p.github_card.avatar, p.github_card.updated); }
    res.offline = c.offline;
    res.target = target;
    return res;
  }
  // workPage: a work, its author, and its assets (none owned: no library here)
  async function workPage(id, refresh) {
    const a = accountCopy(), c = await loadCommunity(false);
    let pw = null, fromService = false;
    if (BASE !== "") {
      try { pw = await svc.getWork(id); fromService = true; }
      catch (e) {
        goErr(e);
        if (!serviceUnavailable(e) || !workIDOK(id) || id.startsWith("m")) return { ok: false, err: e.message };
      }
    }
    if (!fromService) {
      let w, off;
      try { ({ val: w, offline: off } = await repoWork(id, refresh)); }
      catch (e) { goErr(e); return { ok: false, err: e === ERR_REPO_NONE ? "社区中已没有该作品" : e.message }; }
      pw = Object.assign(zero("PublishedWork"), JSON.parse(JSON.stringify(w)), { source: "github" });
      if (off !== "") c.offline = firstOf(c.offline, off);
    }
    let authorID = "", authorLogin = pw.author;
    if (pw.source.startsWith("service") || accountIDOK(pw.author)) { authorID = pw.author; authorLogin = ""; }
    let authorRow = author(c, authorID, authorLogin);
    if (authorID !== "") {
      try { authorRow = creatorRow(await cachedProfile(authorID, a.token, refresh), c.following, c.meID, c.meLogin); } catch (e) { goErr(e); }
    }
    const owned = ownedBoothIDs(), cards = libraryCards();
    const assets = (pw.assets || []).map(x => {
      const row = Object.assign(zero("WorkAssetRow"), x, { kind: linkKindOf(x.url, ["booth_item", "gumroad", "jinxxy"]) });
      row.boothId = boothItemID(x.url);
      if (row.boothId !== "") {
        row.owned = !!owned.get(row.boothId);
        const card = cards.get(row.boothId);
        if (card && row.owned) { row.key = card.key; row.category = card.category; }
      }
      return row;
    });
    let likes = 0;
    for (const e of c.works || []) if (e.id === id) likes = e.likes;
    try { const stats = await cachedStats(false); if (stats.works && has2(stats.works, id)) likes = stats.works[id].likes; } catch (e) { goErr(e); }
    const mine = authorID !== "" && authorID === a.acc.id || authorLogin !== "" && equalFold(authorLogin, a.acc.github || "");
    const res = { ok: true, id, work: marshal("PublishedWork", pw), source: pw.source,
      pic: firstOf(servicePic(pw.cover), repoPic(pw.cover, pw.updated > pw.created ? pw.updated : pw.created)),
      author: marshal("CreatorRow", authorRow), assets: marshal("[]WorkAssetRow", assets), likes, mine, videoKind: videoKind(pw.video),
      offline: c.offline, pictures: marshal("[]string", pw.pictures) };
    res.canEdit = mine;
    res.picList = (pw.pictures || []).map(x => servicePic("/pic/" + x + ".jpg"));
    let remixes = [], original = null;
    for (const row of workRows(c)) {
      if (row.based_on === id) remixes.push(row);
      if (row.id === pw.based_on) original = row;
    }
    if (original) res.original = marshal("WorkRow", original);
    remixes = stableSort(remixes, (x, y) => workNewer(x, y) ? -1 : workNewer(y, x) ? 1 : 0);
    res.remixes = marshal("[]WorkRow", remixes);
    res.originalMissing = pw.based_on !== "" && !original;
    if (pw.source !== "service") res.issueUrl = issueURL(pw.issue);
    return res;
  }
  // ensureImported: the GitHub follows kept here go to the service once for each account (a browser keeps none, so
  // only the mark that it was done is written)
  async function ensureImported(a) {
    if (a.token === "" || a.acc.id === "") return;
    const d = localCopy();
    if (d.importedId === a.acc.id) return;
    const logins = [], seen = new Set();
    for (const f of d.following) if (f.login !== "" && !seen.has(f.login)) { seen.add(f.login); logins.push(f.login); }
    if (logins.length > 0) {
      try { await svc.importFollowing(a.token, logins); }
      catch (e) { goErr(e); if (!(e instanceof ServiceError) || e.unavailable || e.status === 0) throw e; }
    }
    withLocal(dd => { if (dd.importedId === a.acc.id) return false; dd.importedId = a.acc.id; return true; });
  }

  // ---------- the answers ----------

  // answer (registerService and the files after it): a service refusal says whether the account still holds
  function answer(r, err) {
    if (err) {
      const out = { ok: false, err: err.message };
      if (err instanceof ServiceError) { out.accountInvalid = err.status === 401; out.unavailable = err.unavailable; out.code = err.code; }
      return out;
    }
    return Object.assign(r || {}, { ok: true });
  }
  // fail (api.go's Register, discovery.go, notify.go): the refusal's text alone
  function refused(err) { return { ok: false, err: err.message }; }
  const badRequest = () => answer(null, fail("bad_request"));
  // run f; its Go error, or null
  async function attempt(f) { try { await f(); return null; } catch (e) { return goErr(e); } }
  const ROUTES = {};
  function route(name, f) { ROUTES[name] = f; }

  // ---------- social/api.go: Register ----------

  route("owned", async b => {
    // (a browser has no library and no wish list: nothing is owned or wished)
    const r = unmarshal(b, "booth_ids", "[]string", null);
    if (!r.ok || r.v === null || r.v.length > 200) return refused(fail("bad_request"));
    const owned = {}, wished = {};
    for (const id of r.v) {
      if (!boothIDOK(id)) return refused(fail("bad_request"));
      owned[id] = false; wished[id] = false;
    }
    return { ok: true, owned, wished };
  });
  // state: the 实验性 note is not shown in a browser (notice is always true); no GitHub login is typed here
  route("state", async () => {
    const d = localCopy(), a = accountCopy();
    return { ok: true, repo: REPO, login: firstOf(a.acc.github, d.login), notice: true,
      following: marshal("[]s.Follow", followingList(d)), bases: marshal("[]BaseFollow", d.bases), sent: d.sent, account: a.acc.id !== "" };
  });
  route("notice", async () => {
    withLocal(d => { const changed = !d.notice; d.notice = true; return changed; });
    return { ok: true };
  });
  route("lists", async b => {
    const c = await loadCommunity(flag(b, "refresh"));
    const works = workRows(c);
    const res = { ok: true, service: c.service, creators: marshal("[]CreatorRow", creatorRows(c)), works: marshal("[]WorkRow", works),
      following: c.following.size, newcomers: marshal("[]WorkRow", newcomers(works, Date.now())) };
    res.birthdays = marshal("[]CreatorRow", c.birthdays.map(p => creatorRow(p, c.following, c.meID, c.meLogin)));
    if (c.offline !== "") res.offline = c.offline;
    if (c.closed !== "") res.closed = c.closed;
    if (c.following.size === 0 && works.length === 0) res.empty = true;
    return res;
  });
  route("feed", async b => {
    const d = localCopy(), c = await loadCommunity(flag(b, "refresh"));
    const f = buildFeed(c, d, Date.now());
    await addNotices(f, c, c.works);
    f.offline = firstOf(c.offline, f.offline);
    f.closed = c.closed;
    return { ok: true, feed: marshal("s.Feed", f) };
  });
  route("seen", async b => {
    let keys = unmarshal(b, "keys", "[]string", null).v || [];
    if (keys.length > FEED_MAX + FOLLOWING_MAX) keys = keys.slice(0, FEED_MAX + FOLLOWING_MAX);
    let day = str(b, "birthdays");
    if (day !== "" && day !== dayOf(Date.now())) day = "";
    markSeen(keys, day);
    let seq = 0;
    for (const k of keys) {
      if (!k.startsWith("notice:")) continue;
      const tail = k.slice(7);
      const n = /^[+-]?[0-9]+$/.test(tail) && Math.abs(Number(tail)) <= Number.MAX_SAFE_INTEGER ? Number(tail) : 0;
      seq = Math.max(seq, n);
    }
    const err = await attempt(async () => seenNotices(seq));
    return err ? refused(err) : { ok: true };
  });
  route("profile", async b => {
    const id = str(b, "id");
    let github = str(b, "github");
    if (id === "" && github === "") github = str(b, "login");
    if (id !== "" && !accountIDOK(id)) return refused(fail("bad_request"));
    if (github !== "") {
      try { github = normLogin(github.replace(/^gh:/, "")); } catch (e) { return refused(goErr(e)); }
    }
    if (id === "" && github === "") return refused(fail("bad_request"));
    return profilePage(id, github, flag(b, "refresh"));
  });
  route("creator", async b => {
    let login;
    try { login = normLogin(str(b, "login")); } catch (e) { return refused(goErr(e)); }
    return profilePage("", login, flag(b, "refresh"));
  });
  route("me", async b => {
    const d = localCopy(), a = accountCopy();
    const res = { ok: true, signedIn: a.token !== "", account: null, draft: marshal("*Draft", d.card) };
    if (a.token === "") return res;
    res.account = marshal("Account", a.acc);
    res.profile = null;
    res.pic = profilePic({ avatar: a.acc.avatar });
    try {
      const p = await cachedProfile(a.acc.id, a.token, flag(b, "refresh"));
      res.profile = marshal("Profile", p); res.pic = profilePic(p); res.showcase = marshal("[]WorkAssetRow", showcaseRows(p.showcase));
      if (p.github_card) { res.githubCard = marshal("Card", p.github_card); res.githubPic = repoPic(p.github_card.avatar, p.github_card.updated); }
    } catch (e) { res.profileError = goErr(e).message; }
    return res;
  });
  route("profile/save", async b => {
    let a, req, account;
    try { a = needAccount(); req = accountReq(b); account = await svc.patchMe(a.token, req); changeAccount(a.token, d => { d.acc = account; }); }
    catch (e) { return refused(goErr(e)); }
    try { saveDraft(draftOf(account)); } catch (e) { goErr(e); }
    withLocal(d => { const changed = d.login !== (account.github || ""); d.login = account.github || ""; return changed; });
    const res = { ok: true, account: marshal("Account", account) };
    try {
      const p = await cachedProfile(account.id, a.token, true);
      Object.assign(res, { profile: marshal("Profile", p), pic: profilePic(p), showcase: marshal("[]WorkAssetRow", showcaseRows(p.showcase)) });
    } catch (e) { goErr(e); }
    return res;
  });
  // picture: an avatar, a post's picture or a message's, as base64 of its JPEG or PNG bytes
  route("picture", async b => {
    const kind = str(b, "kind");
    if (kind !== "avatar" && kind !== "post" && kind !== "message") return refused(fail("bad_request"));
    const data = b64Decode(str(b, "b64"));
    if (!data || data.length === 0 || data.length > MAX.pictureBytes) return refused(fail("bad_picture"));
    let p;
    try { const a = needAccount(); p = await uploadPicture(a.token, kind, data); } catch (e) { return refused(goErr(e)); }
    return { ok: true, picture: marshal("Picture", p), id: p.id, url: p.full.url, pic: kind === "message" ? "" : servicePic(p.full.url) };
  });
  route("me/draft", async b => {
    const r = unmarshal(b, "card", "Draft", zero("Draft"));
    if (!r.ok) return refused(new GoError("名片内容有误"));
    try { saveDraft(r.v); } catch (e) { return refused(goErr(e)); }
    return { ok: true };
  });
  route("work", async b => {
    const id = str(b, "id");
    if (!workIDOK(id)) return refused(new GoError("作品编号无效"));
    return workPage(id, flag(b, "refresh"));
  });
  route("work/publish", async b => {
    let a, pw;
    try {
      a = needAccount();
      const wr = workReqOf(b), pics = pictureBodies(b);
      if (pics.length === 0) throw new GoError("请先选择作品图片");
      const ids = await uploadPictures(a.token, pics);
      pw = await svc.publishWork(a.token, Object.assign({}, wr, { pictures: ids, auto_flag: flag(b, "auto_flag") }));
    } catch (e) { return refused(goErr(e)); }
    return publishedAnswer(a, pw);
  });
  route("work/update", async b => {
    let a, pw;
    try {
      a = needAccount();
      const id = str(b, "id");
      if (!workIDOK(id)) throw new GoError("作品编号无效");
      const wr = workReqOf(b), old = await svc.getWork(id), pics = pictureBodies(b);
      const body = { title: wr.title, desc: wr.desc, video: wr.video, base: wr.base, assets: marshal("[]WorkAsset", wr.assets),
        links: marshal("[]Link", wr.links), recipe: wr.recipe, based_on: wr.based_on, rights_ok: wr.rights_ok };
      if (pics.length > 0) {
        body.pictures = await uploadPictures(a.token, pics);
        if (flag(b, "auto_flag")) body.auto_flag = true;
      } else if (old.source === "service") body.pictures = old.pictures;
      pw = await svc.patchWork(a.token, id, body);
    } catch (e) { return refused(goErr(e)); }
    return publishedAnswer(a, pw);
  });
  route("work/delete", async b => {
    const id = str(b, "id");
    if (!workIDOK(id)) return refused(new GoError("作品编号无效"));
    try { const a = needAccount(); await svc.deleteWork(a.token, id); } catch (e) { return refused(goErr(e)); }
    return { ok: true };
  });
  route("recipes", async b => {
    const c = await loadCommunity(flag(b, "refresh"));
    const res = { ok: true, recipes: marshal("[]RecipeRef", c.recipes), service: c.service };
    if (c.offline !== "") res.offline = c.offline;
    return res;
  });
  route("recipe", async b => {
    const id = str(b, "id");
    if (id === "" || blen(id) > 40) return refused(fail("bad_request"));
    let e, text;
    try { e = await svc.getRecipe(id); text = utf8Out.decode(await fetchFile(e.file)); } catch (x) { return refused(goErr(x)); }
    return { ok: true, text, recipe: marshal("RecipeRef", recipeRefOf(e, true)) };
  });
  route("recipe/share", async b => {
    let e;
    try {
      const a = needAccount();
      e = await svc.publishRecipe(a.token, { code: str(b, "code"), title: str(b, "title"), note: str(b, "note"), base: str(b, "base") });
    } catch (x) { return refused(goErr(x)); }
    return { ok: true, recipe: marshal("RecipeRef", recipeRefOf(e, true)) };
  });
  route("report", async b => {
    try {
      const reason = cleanText(str(b, "reason"), MAX.report, true, 0);
      const kind = str(b, "kind"), id = str(b, "id"), a = needAccount();
      switch (kind) {
        case "work":
          if (!workIDOK(id)) throw new GoError("作品编号无效");
          await svc.reportTarget(a.token, "works", id, { reason });
          break;
        case "recipe":
          if (id === "" || blen(id) > 40) throw fail("bad_request");
          await svc.reportTarget(a.token, "recipes", id, { reason });
          break;
        case "profile": case "creator":
          await svc.reportTarget(a.token, "accounts", await accountTarget(a.token, id), { reason });
          break;
        case "comment":
          if (!commentIDOK(id)) throw fail("bad_request");
          await svc.reportComment(a.token, id, { reason });
          break;
        case "post":
          if (!postIDOK(id)) throw fail("bad_request");
          await svc.reportTarget(a.token, "posts", id, { reason, sensitive: flag(b, "sensitive") });
          break;
        default: throw new GoError("举报的对象无效");
      }
    } catch (e) { return refused(goErr(e)); }
    return { ok: true };
  });
  route("linkkinds", async b => {
    const urls = unmarshal(b, "urls", "[]string", null).v || [];
    const which = str(b, "for");
    const allowed = which === "profile" ? PROFILE_LINKS : which === "open" ? PROFILE_LINKS.concat(WORK_LINKS) : WORK_LINKS;
    return { ok: true, kinds: urls.slice(0, 64).map(u => linkKindOf(u, allowed)), video: videoKind(str(b, "video")) };
  });
  route("follow", async b => {
    let target, a;
    const on = flag(b, "on");
    try {
      target = followTargetOf(b);
      a = needAccount();
      if (on) await svc.follow(a.token, target); else await svc.unfollow(a.token, target);
    } catch (e) { return refused(goErr(e)); }
    const res = { ok: true, on, target };
    if (on) {
      const c = await loadCommunity(false);
      markSeen(keysOf(c, target), "");
      if (c.profiles.has(target)) res.shops = shopsOfProfile(c.profiles.get(target));
      else if (target.startsWith("gh:")) {
        try { res.shops = shopsOf((await repoCard(target.slice(3), false)).val); } catch (e) { goErr(e); }
      }
    }
    res.following = await followingCount(a.token, target, on);
    return res;
  });
  // me/login: the GitHub login as typed (older pages); kept with what community.json keeps
  route("me/login", async b => {
    let login = "";
    const s = str(b, "login");
    if (s !== "") { try { login = normLogin(s); } catch (e) { return refused(goErr(e)); } }
    withLocal(d => { const changed = d.login !== login; d.login = login; return changed; });
    return { ok: true, login };
  });

  // ---------- social/api.go: registerService ----------

  route("service", async b => {
    const r = { available: false, githubLink: false, account: null, unread: 0 };
    let infoErr = null, info = null;
    try { info = await cachedInfo(flag(b, "refresh")); } catch (e) { infoErr = goErr(e); }
    r.available = !infoErr;
    r.githubLink = !infoErr && info.github_client_id !== "";
    r.oauth = infoErr ? [] : (info.oauth || []).slice();
    if (infoErr) r.err = infoErr.message;
    let a = accountCopy();
    if (a.token !== "" && !infoErr) {
      await refreshAccount(a);
      try {
        const n = await unread(a);
        r.unread = n.notices; r.messages = n.messages; r.requests = n.requests;
      } catch (e) { r.err = goErr(e).message; }
      try { await ensureImported(a); } catch (e) { r.importError = goErr(e).message; }
    }
    a = accountCopy();
    if (a.token !== "") { r.account = marshal("Account", a.acc); r.accountPic = profilePic({ avatar: a.acc.avatar }); }
    return answer(r, null);
  });
  route("auth/code", async b => answer(null, await attempt(() => svc.sendCode({ email: str(b, "email") }))));
  route("auth/verify", b => withAccountLock(async () => {
    const a = accountCopy();
    let created = zero("NewAccount"), r = null;
    let err = await attempt(async () => {
      created = await svc.verifyCode({ email: str(b, "email"), code: str(b, "code"), nickname: str(b, "nickname"), handle: str(b, "handle") });
    });
    if (!err) err = await attempt(async () => { r = await saveSignIn(a.token, created); });
    if (err) invalidateStats();
    return answer(r, err);
  }));

  // ---------- social/qr.go: 扫码登录 ----------
  // The page keeps a code's poll secret in memory, as the program does; the device it says it is comes from the
  // browser (the program says "Windows · MioVRCA").

  const QR_DEVICE = (() => {
    const ua = navigator.userAgent || "";
    const os = /iPhone/.test(ua) ? "iPhone" : /iPad/.test(ua) || /Macintosh/.test(ua) && navigator.maxTouchPoints > 1 ? "iPad" : /Android/.test(ua) ? "Android"
      : /Windows/.test(ua) ? "Windows" : /Macintosh/.test(ua) ? "Mac" : /Linux/.test(ua) ? "Linux" : "";
    const app = /MicroMessenger/.test(ua) ? "微信" : /QQ\//.test(ua) ? "QQ" : /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox"
      : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "";
    return [os, app].filter(Boolean).join(" · ") || "浏览器";
  })();
  const qrs = new Map();
  // saveSignIn: the account an email code or a QR code signed in, kept as this browser's session
  async function saveSignIn(expected, created) {
    if (created.token === "" || created.id === "") throw new GoError("社区服务返回的内容有误");
    const acc = Object.assign({}, created);
    delete acc.token; delete acc.restored;
    stopDevice();
    await changeAccount(expected, d => { d.acc = acc; d.token = created.token; d.seen = 0; });
    withLocal(d => { const changed = d.login !== acc.github; d.login = acc.github; return changed; });
    invalidateStats();
    const out = { account: marshal("Account", acc), restored: created.restored };
    try { await ensureImported({ acc, token: created.token, seen: 0 }); } catch (e) { out.importError = goErr(e).message; }
    return out;
  }
  // qrIDOK: 16 bytes in base64url without padding, as the service makes them
  function qrIDOK(id) { return /^[A-Za-z0-9_-]{21}[AQgw]$/.test(id); }
  function keepQR(id, poll, expected) {
    const now = Date.now();
    for (const [k, q] of qrs) if (now >= q.expires) qrs.delete(k);
    // (creation, scan and approval together last at most five minutes)
    qrs.set(id, { poll, expected, expires: now + 5 * 60000 });
  }
  for (const action of ["new", "poll", "scan", "approve"]) {
    route("qr/" + action, b => withAccountLock(async () => {
      let out = null;
      const err = await attempt(async () => { out = await localQR(action, b); });
      return answer(out, err);
    }));
  }
  async function localQR(action, b) {
    const a = accountCopy();
    const id = typeof b.id === "string" ? b.id : "", kind = typeof b.kind === "string" ? b.kind : "";
    if (action !== "new" && !qrIDOK(id)) throw fail("bad_request");
    if (action === "new") {
      const q = await call("POST", "qr", kind === "handoff" ? a.token : "", marshal("QRReq", { kind, device: QR_DEVICE }), "QRSession");
      keepQR(q.id, q.poll || "", a.token);
      return { id: q.id, url: q.url, expires: q.expires };
    }
    if (action === "approve") {
      if (typeof b.ok !== "boolean") throw fail("bad_request");
      await call("POST", "qr/" + id + "/approve", a.token, marshal("QRApproveReq", { ok: b.ok }));
      return {};
    }
    let q;
    if (action === "scan") {
      q = await call("POST", "qr/" + id + "/scan", a.token, marshal("QRScanReq", { device: QR_DEVICE }), "QRState");
      if (q.poll) keepQR(id, q.poll, a.token);
    } else {
      const kept = qrs.get(id) || { poll: "", expected: "" };
      q = await call("GET", "qr/" + id, kept.poll ? "" : a.token, undefined, "QRState", "", kept.poll ? { "X-MioVRCA-QR": kept.poll } : null);
      if (q.account) {
        if (!kept.poll) throw new GoError("社区服务返回的内容有误");
        const saved = await saveSignIn(kept.expected, q.account);
        qrs.delete(id);
        return Object.assign(saved, qrView(q));
      }
    }
    return qrView(q);
  }
  function qrView(q) {
    return { state: q.state, kind: q.kind, device: q.device || "", person: q.person ? marshal("Person", q.person) : null, personPic: q.person ? profilePic(q.person) : "" };
  }

  // ---------- social/oauth.go: Google and Discord ----------
  // The program opens the browser and keeps the session's key in memory while it waits. The page goes to the provider
  // itself, so the key waits in this tab's sessionStorage until it comes back at #/oauth/<id> (web-start.js).
  function oauthKept(id) { try { return JSON.parse(sessionStorage.getItem("miovrca.oauth." + id) || "null"); } catch (e) { return null; } }
  function oauthKeep(id, v) {
    try { if (v) sessionStorage.setItem("miovrca.oauth." + id, JSON.stringify(v)); else sessionStorage.removeItem("miovrca.oauth." + id); } catch (e) { /* (a private window: the sign-in cannot come back) */ }
  }
  for (const action of ["start", "poll", "finish", "unlink"]) {
    route(action === "unlink" ? "account/logins/unlink" : "oauth/" + action, b => withAccountLock(async () => {
      let out = null;
      const err = await attempt(async () => { out = await localOAuth(action, b); });
      const r = answer(out, err);
      if (err instanceof RuleError) r.code = err.code;
      return r;
    }));
  }
  async function localOAuth(action, b) {
    const a = accountCopy();
    const id = typeof b.id === "string" ? b.id : "", provider = typeof b.provider === "string" ? b.provider : "";
    if ((action === "start" || action === "unlink") && provider !== "google" && provider !== "discord") throw fail("bad_request");
    if (action === "unlink") {
      await send("DELETE", "me/logins/" + provider, a.token);
      const logins = (a.acc.logins || []).filter(p => p !== provider);
      changeAccount(a.token, d => { d.acc.logins = logins; });
      return { logins };
    }
    if (action === "start") {
      if (has2(b, "link") && typeof b.link !== "boolean") throw fail("bad_request");
      const link = b.link === true;
      const q = await call("POST", "oauth/start", link ? a.token : "", marshal("OAuthStartReq", { provider, client: "web", link }), "OAuthStart");
      const expires = Date.parse(q.expires);
      if (!qrIDOK(q.id) || q.key === "" || !expires) throw new GoError("社区服务返回的内容有误");
      oauthKeep(q.id, { key: q.key, expected: a.token, link, provider, expires });
      location.assign(q.url); // (the page leaves for the provider)
      return { id: q.id, provider, leaving: true };
    }
    if (!qrIDOK(id)) throw fail("bad_request");
    const kept = oauthKept(id);
    if (!kept || Date.now() >= kept.expires) { oauthKeep(id, null); throw fail("oauth_expired"); }
    if (a.token !== kept.expected) { oauthKeep(id, null); throw fail("unauthorized"); }
    const req = { id, key: kept.key, nickname: "", handle: "" };
    if (action === "finish") {
      if (typeof b.nickname !== "string") throw fail("bad_request");
      req.nickname = b.nickname;
      if (has2(b, "handle")) { if (typeof b.handle !== "string") throw fail("bad_request"); req.handle = b.handle; }
    }
    let r;
    try {
      r = await call("POST", "oauth/finish", "", marshal("OAuthFinishReq", req), "OAuthResult");
    } catch (e) {
      const err = goErr(e);
      if (err instanceof ServiceError && (["oauth_expired", "oauth_denied", "oauth_no_email", "oauth_linked_elsewhere"].includes(err.code) || err.status === 503)) oauthKeep(id, null);
      throw err;
    }
    if (r.state === "pending") return { state: r.state };
    if (r.state === "new") return { state: r.state, nickname: r.nickname, handle: r.handle, email: r.email || "" };
    if (r.state === "signed_in") {
      oauthKeep(id, null);
      if (kept.link) throw new GoError("社区服务返回的内容有误");
      const created = Object.assign({}, r); delete created.state;
      const out = await saveSignIn(kept.expected, created);
      out.state = r.state;
      return out;
    }
    if (r.state === "linked") {
      oauthKeep(id, null);
      if (!kept.link) throw new GoError("社区服务返回的内容有误");
      const logins = (r.logins || []).slice();
      changeAccount(kept.expected, d => { d.acc.logins = logins; });
      return { state: r.state, logins };
    }
    throw new GoError("社区服务返回的内容有误");
  }

  // ---------- social/translate.go: 翻译帖子 ----------
  // free: the service's own translation. ai: in the program its AI service; here the player's own provider, model
  // and key (social.js keeps them in this browser as "so-translate"), through the service. When AI is not set up or
  // fails, the free translation answers, saying fallback.
  const AI_MODELS = { openai: "gpt-4o-mini", deepseek: "deepseek-chat", anthropic: "claude-haiku-5-5", gemini: "gemini-2.0-flash",
    openrouter: "openai/gpt-4o-mini", siliconflow: "Qwen/Qwen2.5-7B-Instruct", dashscope: "qwen-plus", moonshot: "moonshot-v1-8k" };
  function aiTranslateSettings() {
    try {
      const v = JSON.parse(storeGet("so-translate") || "{}");
      return AI_MODELS[v.provider] && typeof v.key === "string" && v.key ? { provider: v.provider, model: typeof v.model === "string" && v.model ? v.model : AI_MODELS[v.provider], key: v.key } : null;
    } catch (e) { return null; }
  }
  route("post/translate", async b => {
    const id = typeof b.id === "string" ? b.id : "", target = typeof b.target === "string" ? b.target : "";
    const mode = has2(b, "mode") ? b.mode : "free";
    if (!postIDOK(id) || !["zh", "en", "ja"].includes(target) || (mode !== "free" && mode !== "ai")) return Object.assign(badRequest(), { code: "bad_request" }); // (as the program says it)
    const a = accountCopy();
    let out = null;
    const err = await attempt(async () => {
      const ai = mode === "ai" ? aiTranslateSettings() : null;
      if (ai) {
        try {
          const t = await call("POST", "translate/ai", a.token, marshal("AITranslateReq", { post: id, target, provider: ai.provider, model: ai.model }),
            "Translation", "", { "X-MioVRCA-AI-Key": ai.key });
          out = { text: t.text, source: t.source, provider: t.provider };
          return;
        } catch (e) {
          const ge = goErr(e);
          if (ge instanceof ServiceError && (ge.code === "nothing_to_translate" || ge.code === "same_language" || ge.status === 404)) throw ge;
        }
      }
      const t = await call("POST", "translate", a.token, marshal("TranslateReq", { post: id, target }), "Translation");
      out = { text: t.text, source: t.source, provider: t.provider, cached: t.cached };
      if (mode === "ai") out.fallback = true;
    });
    return answer(out, err);
  });

  // ---------- social/mod.go: the community's moderators ----------
  // what the service's moderators do to a post or an account (the service checks who may)
  function registerMod(name, kind, validID) {
    route(name, async b => {
      const id = typeof b.id === "string" ? b.id : null, action = typeof b.action === "string" ? b.action : null;
      let bad = id === null || !validID(id) || action === null;
      let reason = "";
      if (has2(b, "reason")) { if (typeof b.reason !== "string") bad = true; else reason = b.reason; }
      const allowed = kind === "posts" ? ["hide", "restore", "sensitive", "unsensitive", "delete", "feature", "unfeature"].includes(action) : action === "ban" || action === "unban";
      if (bad || !allowed) return { ok: false, err: fail("bad_request").message };
      const err = await attempt(async () => {
        await send("POST", "mod/" + kind + "/" + id, needAccount().token, marshal("ModReq", { action, reason }));
      });
      if (err) return answer(null, err);
      // (posts are read directly, as for the owner's deletion; what refers to them is read again)
      invalidateStats();
      cache.profiles.clear();
      return { ok: true };
    });
  }
  registerMod("mod/post", "posts", postIDOK);
  registerMod("mod/account", "accounts", accountIDOK);

  // ---------- 推送通知: the page's own (the program has Windows notices instead) ----------
  route("push/key", async () => {
    let info = zero("ServiceInfo");
    const err = await attempt(async () => { info = await svc.getInfo(); });
    return answer({ key: info.push_key || "" }, err);
  });
  route("push/on", async b => {
    const sub = isObj(b.subscription) ? b.subscription : {}, keys = isObj(sub.keys) ? sub.keys : {};
    const kinds = Array.isArray(b.kinds) ? b.kinds.filter(k => typeof k === "string") : [];
    const err = await attempt(async () => {
      await call("PUT", "me/push", needAccount().token, marshal("PushReq", { endpoint: String(sub.endpoint || ""),
        keys: { p256dh: String(keys.p256dh || ""), auth: String(keys.auth || "") }, lang: String(b.lang || "zh"), kinds }));
    });
    return answer({}, err);
  });
  route("push/off", async b => {
    const a = accountCopy();
    const err = a.token === "" ? null : await attempt(async () => { await call("DELETE", "me/push", a.token, marshal("PushDeleteReq", { endpoint: str(b, "endpoint") })); });
    return answer({}, err);
  });
  route("account", b => withAccountLock(async () => {
    let account = zero("Account");
    const err = await attempt(async () => {
      const a = needAccount();
      const nick = cleanText(str(b, "nickname"), MAX.nickname, false, 0);
      if (nick === "") throw new GoError("请填写昵称");
      try {
        account = await svc.patchMe(a.token, { nickname: nick });
        changeAccount(a.token, d => { d.acc = account; });
      } finally { invalidateStats(); }
    });
    return answer({ account: marshal("Account", account) }, err);
  }));
  route("account/logout", () => withAccountLock(async () => {
    const a = accountCopy();
    stopDevice();
    if (a.token !== "") { try { await svc.logout(a.token); } catch (e) { goErr(e); } }
    dropAccount(a.token);
    invalidateStats();
    return answer(null, null);
  }));
  // 注销账号: the handle typed again; the service hides the account for 7 days, then deletes it
  route("account/delete", b => withAccountLock(async () => {
    const err = await attempt(async () => {
      try {
        const a = needAccount(), typed = trimSpace(str(b, "handle")).replace(/^@/, "");
        if (typed === "" || !equalFold(typed, a.acc.handle)) throw new GoError("用户名不一致");
        stopDevice();
        await svc.deleteAccount(a.token);
        dropAccount(a.token);
      } finally { invalidateStats(); }
    });
    return answer(null, err);
  }));
  route("github/start", async () => {
    let d = zero("deviceState");
    const err = await attempt(async () => { d = await startDevice(); });
    return answer({ device: marshal("deviceState", d) }, err);
  });
  route("github/poll", async () => {
    const d = Object.assign({}, device.state);
    let acc = zero("Account");
    const err = await attempt(async () => { acc = needAccount().acc; });
    return answer({ device: marshal("deviceState", d), account: marshal("Account", acc) }, err);
  });
  route("github/unlink", () => withAccountLock(async () => {
    let updated = zero("Account");
    const err = await attempt(async () => {
      const a = needAccount();
      stopDevice();
      updated = await svc.unlinkGitHub(a.token);
      changeAccount(a.token, d => { d.acc = updated; });
    });
    return answer({ account: marshal("Account", updated) }, err);
  }));
  route("like", async b => {
    const id = str(b, "id");
    if (!workIDOK(id)) return answer(null, new GoError("作品编号无效"));
    const err = await attempt(async () => {
      const a = needAccount();
      if (flag(b, "on")) await svc.like(a.token, id); else await svc.unlike(a.token, id);
    });
    if (err) return answer(null, err);
    invalidateStats();
    const r = { on: flag(b, "on") };
    try { r.stats = marshal("serviceStats", await cachedStats(false)); } catch (e) { r.statsError = goErr(e).message; }
    return answer(r, null);
  });
  route("stats", async b => {
    let stats = zero("serviceStats");
    const err = await attempt(async () => { stats = await cachedStats(flag(b, "refresh")); });
    return answer({ stats: marshal("serviceStats", stats) }, err);
  });
  // the comments of works and posts: the older program's, kept as Go keeps them
  route("comments", async b => {
    const id = str(b, "id"), after = str(b, "after");
    if (!workIDOK(id) || after !== "" && !commentIDOK(after)) return badRequest();
    let r = zero("Comments");
    const err = await attempt(async () => { r = await svc.comments(id, after); });
    return answer({ comments: marshal("[]Comment", r.comments), next: r.next }, err);
  });
  route("comment/post", async b => {
    const id = str(b, "id"), reply = str(b, "reply_to");
    if (!workIDOK(id) || reply !== "" && !commentIDOK(reply)) return badRequest();
    let c = zero("Comment");
    const err = await attempt(async () => {
      const text = cleanText(str(b, "text"), MAX.comment, true, MAX.commentLines);
      if (text === "") throw new GoError("请填写评论");
      const a = needAccount();
      c = await svc.postComment(a.token, id, { text, reply_to: reply });
      invalidateStats();
    });
    return answer({ comment: marshal("Comment", c) }, err);
  });
  route("comment/delete", async b => {
    const id = str(b, "id");
    if (!commentIDOK(id)) return badRequest();
    const err = await attempt(async () => { const a = needAccount(); await svc.deleteComment(a.token, id); invalidateStats(); });
    return answer(null, err);
  });
  route("comment/report", async b => {
    const id = str(b, "id");
    if (!commentIDOK(id)) return badRequest();
    const err = await attempt(async () => {
      const reason = cleanText(str(b, "reason"), MAX.report, true, 0);
      const a = needAccount();
      await svc.reportComment(a.token, id, { reason });
    });
    return answer(null, err);
  });
  route("notices", async b => {
    let n = null;
    const err = await attempt(async () => { n = await noticePage(b); });
    return answer(n, err);
  });
  route("notices/seen", async b => answer(null, await attempt(async () => seenNotices(number(b, "seq")))));

  // ---------- social/device.go: linking GitHub (its device flow) ----------
  // github.com answers no other site's page (and this page may only talk to its own origin): from a browser the
  // flow stops at its first request, with the same refusal the program gives when it cannot reach GitHub.

  const DEVICE_CODE_URL = "https://github.com/login/device/code", DEVICE_TOKEN_URL = "https://github.com/login/oauth/access_token";
  const device = { state: { status: "" }, run: 0, ctl: null };
  async function deviceForm(address, form, signal) {
    // (10 seconds, as httpClient gives, or until the flow is stopped)
    const ctl = new AbortController(), timer = setTimeout(() => ctl.abort(), 10000);
    const stop = () => ctl.abort();
    if (signal) signal.addEventListener("abort", stop, { once: true });
    let resp, text;
    try {
      resp = await fetch(address, { method: "POST", headers: { Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded" },
        body: valuesEncode(form), credentials: "omit", redirect: "manual", signal: ctl.signal });
      text = await resp.arrayBuffer();
    } catch (e) {
      if (ctl.signal.aborted) throw new GoError("无法连接 GitHub（连接超时，可能需要代理）");
      throw new GoError("无法连接 GitHub（连接失败，可能需要代理）");
    } finally {
      clearTimeout(timer);
      if (signal) signal.removeEventListener("abort", stop);
    }
    const s = resp.type === "opaqueredirect" ? 302 : resp.status;
    let v;
    try {
      if (text.byteLength > 65536 || s >= 500 || s < 200 || s >= 300 && s < 400) throw 0;
      v = JSON.parse(utf8Out.decode(new Uint8Array(text)));
    } catch (e) { throw new GoError("GitHub 返回的内容有误"); }
    return v;
  }
  function stopDevice() {
    if (device.ctl) device.ctl.abort();
    device.ctl = null;
    device.run++;
    device.state = { status: "idle" };
  }
  function deviceResult(run, status, message) { if (device.run === run) device.state = { status, message }; }
  function startDevice() {
    return withAccountLock(async () => {
      const a = needAccount(), info = await cachedInfo(false);
      if (info.github_client_id === "") throw new GoError("社区暂未开放 GitHub 关联");
      stopDevice();
      const v = await deviceForm(DEVICE_CODE_URL, { client_id: info.github_client_id, scope: "" });
      const ctx = { bad: false };
      const code = { device_code: decode("string", v.device_code, ctx), user_code: decode("string", v.user_code, ctx),
        verification_uri: decode("string", v.verification_uri, ctx), expires_in: decode("int", v.expires_in, ctx), interval: decode("int", v.interval, ctx) };
      if (ctx.bad || !isObj(v)) throw new GoError("GitHub 返回的内容有误");
      const u = parseURL(code.verification_uri);
      const validURI = !!u && !u.user && u.fragment === "" && u.rawQuery === "" && u.scheme === "https" && u.host === "github.com" && u.path === "/login/device";
      if (!validURI || code.device_code === "" || code.user_code === "" || code.expires_in <= 0 || code.expires_in > 3600) {
        throw new GoError("GitHub 返回的内容有误");
      }
      const run = ++device.run, ctl = new AbortController();
      device.ctl = ctl;
      device.state = { status: "pending", user_code: code.user_code, verification_uri: code.verification_uri };
      const state = Object.assign({}, device.state);
      pollDevice(run, ctl, Date.now() + code.expires_in * 1000, a.token, info.github_client_id, code.device_code, (code.interval > 0 ? code.interval : 5) * 1000);
      return state;
    });
  }
  async function pollDevice(run, ctl, deadline, token, client, code, interval) {
    const wait = ms => new Promise(done => {
      const left = deadline - Date.now();
      if (ctl.signal.aborted || left <= 0) return done(false);
      const t = setTimeout(() => done(ms <= left), Math.min(ms, left));
      ctl.signal.addEventListener("abort", () => { clearTimeout(t); done(false); }, { once: true });
    });
    while (await wait(interval)) {
      let v;
      try {
        v = await deviceForm(DEVICE_TOKEN_URL, { client_id: client, device_code: code, grant_type: "urn:ietf:params:oauth:grant-type:device_code" }, ctl.signal);
      } catch (e) {
        if (ctl.signal.aborted || Date.now() >= deadline) break;
        return deviceResult(run, "error", goErr(e).message);
      }
      const accessToken = typeof v.access_token === "string" ? v.access_token : "";
      if (accessToken !== "") {
        const err = await withAccountLock(() => attempt(async () => {
          const current = needAccount();
          if (current.token !== token) throw new GoError(SERVICE_MESSAGES.unauthorized);
          if (ctl.signal.aborted || Date.now() >= deadline) throw new GoError(Date.now() >= deadline ? "context deadline exceeded" : "context canceled");
          const acc = await svc.linkGitHub(token, accessToken);
          changeAccount(token, d => { d.acc = acc; });
        }));
        return deviceResult(run, err ? "error" : "linked", err ? err.message : "已关联 GitHub");
      }
      switch (typeof v.error === "string" ? v.error : "") {
        case "authorization_pending": break;
        case "slow_down": interval += 5000; break;
        case "expired_token": return deviceResult(run, "expired", "关联代码已过期，请重试");
        case "access_denied": return deviceResult(run, "denied", "已取消 GitHub 关联");
        default: return deviceResult(run, "error", "无法通过 GitHub 确认账号，请稍后重试");
      }
    }
    if (!ctl.signal.aborted && Date.now() >= deadline) deviceResult(run, "expired", "关联代码已过期，请重试");
  }

  // ---------- social/posts.go ----------

  function follows() { return followingOf(localCopy()); }
  route("posts", async b => {
    const feed = firstOf(str(b, "feed"), "latest");
    if (feed !== "latest" && feed !== "following" && feed !== "hot") return badRequest();
    let tag;
    try { tag = cleanText(str(b, "tag"), MAX.postTag, false, 0); } catch (e) { return answer(null, goErr(e)); }
    const authorID = str(b, "author"), after = str(b, "after");
    if (authorID !== "" && !accountIDOK(authorID) || blen(after) > 4096) return badRequest();
    const q = { feed };
    if (tag !== "") q.tag = tag;
    if (authorID !== "") q.author = authorID;
    if (after !== "") q.after = after;
    const a = accountCopy();
    if (feed === "following" && a.token === "") return answer(null, await attempt(async () => needAccount()));
    let f = zero("Feed");
    const err = await attempt(async () => { f = await svc.listPosts(a.token, q); });
    const fl = follows(), rows = [];
    for (const item of f.items || []) if (item.post) rows.push(postRow(item.post, a.acc.id, fl));
    return answer({ posts: marshal("[]PostRow", rows), next: f.next }, err);
  });
  route("post", async b => {
    const id = str(b, "id");
    if (!postIDOK(id)) return badRequest();
    const a = accountCopy();
    let p;
    try { p = await svc.getPost(a.token, id); } catch (e) { return answer(null, goErr(e)); }
    const r = { post: marshal("PostRow", postRow(p, a.acc.id, follows())) };
    if (p.work !== "" && workIDOK(p.work)) {
      try { r.work = marshal("WorkRow", workRow(workEntryOf(await svc.getWork(p.work)), null, null, a.acc.id, a.acc.github || "")); } catch (e) { goErr(e); }
    }
    return answer(r, null);
  });
  route("post/publish", async b => {
    let text;
    try { text = cleanText(str(b, "text"), MAX.post, true, MAX.postLines); } catch (e) { return answer(null, goErr(e)); }
    let pictures = null;
    if (has(b, "pictures")) {
      const r = unmarshal(b, "pictures", "[]string", null);
      if (!r.ok || (r.v || []).length > MAX.postPictures) return badRequest();
      pictures = r.v;
    }
    const seenPic = new Set();
    for (const id of pictures || []) {
      if (!pictureIDOK(id) || seenPic.has(id)) return answer(null, fail("bad_picture"));
      seenPic.add(id);
    }
    if (has(b, "tags") && b.tags !== null && !(Array.isArray(b.tags) && b.tags.length === 0)) return badRequest();
    const reply = str(b, "reply_to"), quote = str(b, "quote_of");
    if (reply !== "" && !postIDOK(reply) || quote !== "" && (!postIDOK(quote) || text === "")) return badRequest();
    const work = str(b, "work");
    if (work !== "" && !workIDOK(work)) return badRequest();
    let assets, group, poll;
    const ask = flag(b, "ask");
    try {
      assets = postAssets(b);
      if (ask) askCheck(b, pictures, reply, quote);
    } catch (e) { return answer(null, goErr(e)); }
    if (text === "" && (pictures || []).length === 0 && (reply === "" || work === "" && (assets || []).length === 0)) return answer(null, fail("bad_text"));
    try { [group, poll] = postExtras(b, pictures, reply, quote); } catch (e) { return answer(null, goErr(e)); }
    let a = accountCopy(), p = zero("Post");
    const err = await attempt(async () => {
      a = needAccount();
      p = await svc.createPost(a.token, { text, pictures, reply_to: reply, quote_of: quote, work, assets, sensitive: flag(b, "sensitive"),
        group, poll, ask, auto_flag: flag(b, "auto_flag") });
    });
    return answer({ post: marshal("PostRow", postRow(p, a.acc.id, follows())) }, err);
  });
  route("post/delete", async b => {
    const id = str(b, "id");
    if (!postIDOK(id)) return badRequest();
    return answer(null, await attempt(async () => { const a = needAccount(); await svc.deletePost(a.token, id); }));
  });
  route("post/like", async b => {
    const id = str(b, "id"), on = flag(b, "on");
    if (!postIDOK(id)) return badRequest();
    return answer({ on }, await attempt(async () => { const a = needAccount(); await svc.likePost(a.token, id, on); }));
  });
  route("post/comments", async b => {
    const id = str(b, "id"), after = str(b, "after");
    if (!postIDOK(id) || after !== "" && !commentIDOK(after)) return badRequest();
    let r = zero("Comments");
    const err = await attempt(async () => { r = await svc.postComments(id, after); });
    return answer({ comments: marshal("[]Comment", r.comments), next: r.next }, err);
  });
  route("post/comment/post", async b => {
    const id = str(b, "id"), reply = str(b, "reply_to");
    if (!postIDOK(id) || reply !== "" && !commentIDOK(reply)) return badRequest();
    let c = zero("Comment");
    const err = await attempt(async () => {
      const text = cleanText(str(b, "text"), MAX.comment, true, MAX.commentLines);
      if (text === "") throw new GoError("请填写评论");
      const a = needAccount();
      c = await svc.postPostComment(a.token, id, { text, reply_to: reply });
    });
    return answer({ comment: marshal("Comment", c) }, err);
  });

  // ---------- social/timeline.go ----------

  const RE_HANDLE = /^[A-Za-z0-9_]{4,15}$/;
  const TIMELINE_FILTERS = ["featured", "latest", "hot", "media", "works", "booth"]; // socialapi.TimelineFilters
  route("timeline", async b => {
    const tab = firstOf(str(b, "tab"), "for_you"), after = str(b, "after"), filter = str(b, "filter");
    if (tab !== "for_you" && tab !== "following" && tab !== "bases" || blen(after) > 4096 || filter !== "" && !TIMELINE_FILTERS.includes(filter)) return badRequest();
    let a = accountCopy(), err = null;
    if (tab === "following") err = await attempt(async () => { a = needAccount(); });
    const q = { tab, after }, res = {};
    if (filter !== "" && tab === "for_you") q.filter = filter; // 为你推荐's filter (the other tabs have none)
    if (tab === "bases") {
      const [names, shown] = libraryBases();
      res.bases = shown;
      if (names.length === 0) {
        Object.assign(res, { items: [], next: "", noBases: true });
        return answer(res, err);
      }
      q.base = names;
    }
    let page = zero("Timeline");
    if (!err) err = await attempt(async () => { page = await call("GET", "timeline?" + valuesEncode(q), a.token, undefined, "Timeline"); });
    const row = timelineRows(a), items = [];
    for (const it of page.items || []) items.push({ kind: it.kind, post: marshal("timelinePost", await row(it.post)), by: marshal("*Person", it.by) });
    Object.assign(res, { items, next: page.next });
    return answer(res, err);
  });
  route("thread", async b => {
    const id = str(b, "id"), after = str(b, "after");
    if (!postIDOK(id) || blen(after) > 4096) return badRequest();
    const a = accountCopy();
    let thread = zero("Thread");
    const err = await attempt(async () => { thread = await call("GET", "posts/" + id + "/thread?after=" + queryEscape(after), a.token, undefined, "Thread"); });
    const row = timelineRows(a), ancestors = [], replies = [];
    for (const p of thread.ancestors || []) ancestors.push(await row(p));
    const post = await row(thread.post);
    for (const p of thread.replies || []) replies.push(await row(p));
    return answer({ ancestors: marshal("[]timelinePost", ancestors), post: marshal("timelinePost", post),
      replies: marshal("[]timelinePost", replies), next: thread.next }, err);
  });
  for (const action of ["repost", "bookmark"]) {
    route("post/" + action, async b => {
      const id = str(b, "id"), r = unmarshal(b, "on", "bool", false);
      if (!postIDOK(id) || !r.ok) return badRequest();
      const on = r.v;
      return answer({ on }, await attempt(async () => {
        const a = needAccount();
        await call(on ? "PUT" : "DELETE", "posts/" + id + "/" + action, a.token);
      }));
    });
  }
  route("views", async b => {
    const r = unmarshal(b, "posts", "[]string", null);
    if (!r.ok || !r.v || r.v.length === 0 || r.v.length > 100) return badRequest();
    for (const id of r.v) if (!postIDOK(id)) return badRequest();
    return answer(null, await attempt(async () => { await call("POST", "views", accountCopy().token, { posts: r.v }); }));
  });
  route("handle/check", async b => {
    const handle = str(b, "handle");
    if (!RE_HANDLE.test(handle)) return badRequest();
    let available = zero("HandleAvailable");
    const err = await attempt(async () => { available = await call("GET", "handles/" + handle + "/available", "", undefined, "HandleAvailable"); });
    return answer({ available: available.available }, err);
  });
  route("handle/change", b => {
    const handle = str(b, "handle");
    if (!RE_HANDLE.test(handle)) return Promise.resolve(badRequest());
    return withAccountLock(async () => {
      let updated = zero("Account");
      const err = await attempt(async () => {
        const a = needAccount();
        updated = await svc.patchMe(a.token, { handle });
        changeAccount(a.token, d => { d.acc = updated; });
      });
      return answer({ account: marshal("Account", updated) }, err);
    });
  });
  route("handle/profile", async b => {
    const handle = str(b, "handle");
    if (!RE_HANDLE.test(handle)) return badRequest();
    let p = zero("Profile");
    const err = await attempt(async () => { p = await call("GET", "handles/" + handle, accountCopy().token, undefined, "Profile"); });
    return answer({ profile: marshal("Profile", p) }, err);
  });
  route("search", async b => {
    const kind = firstOf(str(b, "type"), "latest"), q = str(b, "q"), after = str(b, "after");
    if (kind !== "people" && kind !== "top" && kind !== "latest" && kind !== "media" || blen(q) > 4096 || blen(after) > 4096) return badRequest();
    const a = accountCopy();
    let found = zero("Search");
    const err = await attempt(async () => {
      found = await call("GET", "search?" + valuesEncode({ type: kind, q, after }), a.token, undefined, "Search");
    });
    const row = timelineRows(a), rows = [];
    for (const p of found.posts || []) rows.push(await row(p));
    return answer({ posts: marshal("[]timelinePost", rows), accounts: marshal("[]pagePerson", peopleRows(found.accounts)), next: found.next }, err);
  });
  route("bookmarks", async b => {
    const after = str(b, "after");
    if (blen(after) > 4096) return badRequest();
    let a = accountCopy(), page = zero("PostPage");
    const err = await attempt(async () => {
      a = needAccount();
      page = await call("GET", "me/bookmarks?after=" + queryEscape(after), a.token, undefined, "PostPage");
    });
    const row = timelineRows(a), rows = [];
    for (const p of page.posts || []) rows.push(await row(p));
    return answer({ posts: marshal("[]timelinePost", rows), next: page.next }, err);
  });
  route("unread", async () => {
    let u = zero("Unread");
    const err = await attempt(async () => { u = await unread(accountCopy()); });
    return answer({ notices: u.notices, messages: u.messages, requests: u.requests }, err);
  });
  route("block", async b => {
    const id = str(b, "id");
    let on = true;
    if (has(b, "on")) { const r = unmarshal(b, "on", "bool", true); if (!r.ok) return badRequest(); on = r.v; }
    if (!accountIDOK(id)) return badRequest();
    return answer(null, await attempt(async () => { const a = needAccount(); await call(on ? "PUT" : "DELETE", "blocks/" + id, a.token); }));
  });

  // ---------- social/pages.go ----------

  const RE_MESSAGE_ID = /^m[1-9][0-9]{0,18}$/;
  route("profile/posts", async b => {
    const id = str(b, "id"), filter = firstOf(str(b, "filter"), "posts"), after = str(b, "after");
    let validID = accountIDOK(id);
    if (id.startsWith("gh:")) { try { normLogin(id.slice(3)); validID = true; } catch (e) { goErr(e); validID = false; } }
    if (!validID || blen(after) > 4096 || !["posts", "replies", "media", "likes"].includes(filter)) return badRequest();
    const a = accountCopy();
    let page = zero("PostPage"), err = null;
    if (filter === "likes" && (a.acc.id === "" || a.acc.id !== id)) err = fail("forbidden");
    if (!err) err = await attempt(async () => {
      page = await call("GET", "accounts/" + pathEscape(id) + "/posts?" + valuesEncode({ filter, after }), a.token, undefined, "PostPage");
    });
    const row = timelineRows(a), rows = [];
    for (const p of page.posts || []) rows.push(await row(p));
    return answer({ posts: marshal("[]timelinePost", rows), next: page.next }, err);
  });
  route("people", async b => {
    const id = str(b, "id"), kind = str(b, "kind");
    if (kind !== "following" && kind !== "followers" && kind !== "blocks" || id !== "" && !accountIDOK(id)) return badRequest();
    let a = accountCopy(), err = null;
    const own = id === "" || id === a.acc.id;
    if (own || kind !== "following") {
      err = await attempt(async () => { a = needAccount(); if (!own) throw fail("forbidden"); });
    }
    const path = own ? "me/" + kind : "accounts/" + id + "/" + kind;
    let accounts = null;
    if (!err && kind === "following") {
      let following = zero("Following");
      err = await attempt(async () => { following = await call("GET", path, a.token, undefined, "Following"); });
      for (const f of following.following || []) {
        if (f.account) (accounts = accounts || []).push(f.account);
        else if (f.github !== "") (accounts = accounts || []).push(Object.assign(zero("Profile"), { id: "gh:" + f.github, github: f.github,
          handle: f.github, nickname: f.github, read_only: true }));
      }
    } else if (!err) {
      err = await attempt(async () => { accounts = (await call("GET", path, a.token, undefined, "Profiles")).accounts; });
    }
    return answer({ accounts: marshal("[]pagePerson", peopleRows(accounts)) }, err);
  });
  route("settings", b => {
    const req = {};
    if (has(b, "allow_requests")) {
      const r = unmarshal(b, "allow_requests", "string", "");
      if (!r.ok || r.v !== "everyone" && r.v !== "none") return Promise.resolve(badRequest());
      req.allow_requests = r.v;
    }
    for (const key of ["show_following", "show_sensitive"]) {
      if (!has(b, key)) continue;
      const r = unmarshal(b, key, "bool", false);
      if (!r.ok) return Promise.resolve(badRequest());
      req[key] = r.v;
    }
    if (Object.keys(req).length === 0) return Promise.resolve(badRequest());
    return withAccountLock(async () => {
      let updated = zero("Account");
      const err = await attempt(async () => {
        const a = needAccount();
        const body = {};
        for (const k of ["allow_requests", "show_following", "show_sensitive"]) if (has(req, k)) body[k] = req[k];
        updated = await svc.patchMe(a.token, body);
        changeAccount(a.token, d => { d.acc = updated; });
      });
      return answer({ account: marshal("Account", updated) }, err);
    });
  });
  route("trends", async () => {
    let trends = zero("Trends");
    const err = await attempt(async () => { trends = await call("GET", "trends", "", undefined, "Trends"); });
    return answer({ trends: marshal("[]Trend", trends.trends) }, err);
  });
  route("works", async b => {
    const q = {};
    for (const k of ["sort", "base", "asset", "author", "q", "after"]) {
      const v = str(b, k);
      if (blen(v) > 4096 || k === "sort" && v !== "" && v !== "latest" && v !== "hot") return badRequest();
      q[k] = v;
    }
    const a = accountCopy();
    let page = zero("WorkPage");
    const err = await attempt(async () => { page = await svc.listWorks(q); });
    const rows = [], authors = new Map();
    for (const e of page.works || []) {
      if (!authors.has(e.author)) {
        authors.set(e.author, null);
        try { authors.set(e.author, accountIDOK(e.author) ? await svc.profile(a.token, e.author) : await svc.profileByGitHub(a.token, e.author)); }
        catch (x) { goErr(x); }
      }
      rows.push(workRow(e, authors.get(e.author), null, a.acc.id, a.acc.github || ""));
    }
    return answer({ works: marshal("[]WorkRow", rows), next: page.next }, err);
  });
  route("dm", async b => {
    const box = firstOf(str(b, "box"), "inbox");
    if (box !== "inbox" && box !== "requests") return badRequest();
    let page = zero("Conversations");
    const err = await attempt(async () => { const a = needAccount(); page = await call("GET", "dm?box=" + box, a.token, undefined, "Conversations"); });
    const rows = (page.conversations || []).map(c => ({ account: marshal("pagePerson", Object.assign({}, c.account, { pic: profilePic(c.account) })),
      last: marshal("Message", c.last), unread: c.unread }));
    return answer({ conversations: rows }, err);
  });
  for (const action of ["messages", "send", "accept", "delete", "read"]) {
    route("dm/" + action, async b => {
      const id = str(b, "id"), before = str(b, "before");
      if (!accountIDOK(id) || before !== "" && !RE_MESSAGE_ID.test(before)) return badRequest();
      let method = "POST", path = "dm/" + id, input, outType;
      if (action === "messages") { method = "GET"; path += "?before=" + queryEscape(before); outType = "MessagePage"; }
      else if (action === "send") {
        let text;
        try { text = cleanText(str(b, "text"), MAX.message, true, 0); } catch (e) { goErr(e); return badRequest(); }
        const pic = str(b, "picture");
        if (text === "" && pic === "" || pic !== "" && !pictureIDOK(pic)) return badRequest();
        input = marshal("MessageReq", { text, picture: pic });
        outType = "Message";
      } else if (action === "delete") method = "DELETE";
      else path += "/" + action;
      let page = zero("MessagePage"), message = zero("Message");
      const err = await attempt(async () => {
        const a = needAccount();
        const out = await call(method, path, a.token, input, outType);
        if (outType === "MessagePage") page = out; else if (outType === "Message") message = out;
      });
      return answer({ messages: marshal("[]Message", page.messages), next: page.next, message: marshal("Message", message) }, err);
    });
  }
  for (const action of ["recall", "report"]) {
    route("dm/" + action, async b => {
      const id = str(b, "id");
      if (!RE_MESSAGE_ID.test(id)) return badRequest();
      let method = "DELETE", path = "dm/messages/" + id, input;
      if (action === "report") {
        let reason;
        try { reason = cleanText(str(b, "reason"), MAX.report, true, 0); } catch (e) { return answer(null, goErr(e)); }
        method = "POST"; path += "/report"; input = marshal("ReportReq", { reason });
      }
      return answer(null, await attempt(async () => { const a = needAccount(); await call(method, path, a.token, input); }));
    });
  }
  // dm/picture: a message's picture, which only the conversation may see, read with the account
  route("dm/picture", async b => {
    const id = str(b, "id");
    if (!pictureIDOK(id)) return badRequest();
    let data = new Uint8Array(0);
    const err = await attempt(async () => {
      const a = needAccount();
      data = await raw("GET", BASE + "/pic/" + id + "_s.jpg", a.token, "", undefined);
      if (!(data.length >= 3 && data[0] === 0xff && data[1] === 0xd8 && data[2] === 0xff)) throw fail("bad_picture");
    });
    return answer({ src: "data:image/jpeg;base64," + b64Encode(data) }, err);
  });
  async function noticePage(b) {
    const tab = firstOf(str(b, "tab"), "all");
    let after = 0;
    if (has(b, "after")) {
      const r = unmarshal(b, "after", "int", 0);
      if (!r.ok) throw fail("bad_request");
      after = r.v;
    }
    if (tab !== "all" && tab !== "mentions" || after < 0) throw fail("bad_request");
    const a = accountCopy();
    let notices = zero("Notices");
    if (a.token !== "") notices = await call("GET", "me/notices?" + valuesEncode({ tab, after: String(after) }), a.token, undefined, "Notices");
    const rows = [], row = timelineRows(a), posts = new Map();
    let highest = 0;
    for (const n of notices.notices || []) {
      highest = Math.max(highest, n.seq);
      if (!posts.has(n.post) && postIDOK(n.post)) {
        try { posts.set(n.post, await row(await svc.getPost(a.token, n.post))); } catch (e) { goErr(e); posts.set(n.post, null); }
      }
      const actors = (n.from || []).map(actor => Object.assign(zero("Profile"), actor, { pic: profilePic(actor) }));
      rows.push({ seq: n.seq, kind: n.kind, post: n.post, from: marshal("[]pagePerson", actors), count: n.count, at: n.at,
        unread: n.seq > a.seen, item: marshal("*timelinePost", posts.has(n.post) ? posts.get(n.post) : null) });
    }
    cache.noticeAccount = a.acc.id; cache.noticeMax = highest;
    if (a.token !== "" && highest > a.seen) changeAccount(a.token, d => { d.seen = Math.max(d.seen, highest); });
    return { notices: rows };
  }

  // ---------- social/home.go ----------

  route("home", async b => {
    const tab = firstOf(str(b, "tab"), "recommend"), after = str(b, "after");
    if (tab !== "recommend" && tab !== "following" && tab !== "latest" || blen(after) > 4096) return badRequest();
    const cur = Object.assign(zero("homeCursor"), { t: tab });
    if (after !== "") {
      // (Go unmarshals into the cursor as started: what the text leaves out stays)
      let v;
      try { v = JSON.parse(after); } catch (e) { return badRequest(); }
      if (v !== null && !isObj(v)) return badRequest();
      const ctx = { bad: false }, t = TYPES.homeCursor;
      fieldsOf("homeCursor");
      for (const key of Object.keys(v || {})) {
        const f = fieldFor(t, key);
        if (f && v[key] !== null) cur[f.name] = decode(f.type, v[key], ctx);
      }
      if (ctx.bad || cur.t !== tab || !cur.s) return badRequest();
    }
    const a = accountCopy();
    if (tab === "following" && a.token === "") return answer(null, await attempt(async () => needAccount()));
    const fl = follows(), byLogin = new Map();
    try { for (const e of (await repoCreators(false)).val.creators || []) byLogin.set(e.login.toLowerCase(), e); } catch (e) { goErr(e); }
    const flSet = new Map([...fl.keys()].map(k => [k, true])), items = [];
    const homeWorkRow = async w => {
      let profile = null;
      const id = workAccountID(w);
      if (id !== "") { try { profile = await cachedProfile(id, a.token, false); } catch (e) { goErr(e); } }
      const row = workRow(w, profile, flSet, a.acc.id, a.acc.github || "");
      if (!profile && !accountIDOK(w.author) && byLogin.has(w.author.toLowerCase())) {
        const e = byLogin.get(w.author.toLowerCase());
        row.nickname = e.nickname; row.authorPic = avatarPic(e.login, e.avatar, e.updated);
      }
      return row;
    };
    const next = () => !cur.s || cur.f === "" && cur.p === "" && cur.w === "" ? "" : JSON.stringify(marshal("homeCursor", cur));
    try {
      if (tab === "following") {
        if (after === "" || cur.f !== "") {
          const q = { feed: "following" };
          if (cur.f !== "") q.after = cur.f;
          const f = await svc.listPosts(a.token, q);
          for (const item of f.items || []) {
            if (item.post && item.post.kind === "work" && workIDOK(item.post.work)) {
              try { items.push({ kind: "work", work: await homeWorkRow(workEntryOf(await svc.getWork(item.post.work))) }); } catch (e) { goErr(e); }
            } else if (item.post) items.push({ kind: "post", post: postRow(item.post, a.acc.id, fl) });
            else if (item.work) items.push({ kind: "work", work: await homeWorkRow(item.work) });
          }
          cur.f = f.next;
        }
        cur.s = true;
        return answer({ items: marshal("[]HomeItem", items), next: next() }, null);
      }
      const feed = tab === "recommend" ? "hot" : "latest", sortBy = feed, first = after === "";
      if (first || cur.p !== "") {
        const q = { feed };
        if (cur.p !== "") q.after = cur.p;
        const f = await svc.listPosts(a.token, q);
        for (const item of f.items || []) if (item.post && item.post.kind !== "work") items.push({ kind: "post", post: postRow(item.post, a.acc.id, fl) });
        cur.p = f.next;
      }
      if (first || cur.w !== "") {
        const q = { sort: sortBy };
        if (cur.w !== "") q.after = cur.w;
        const p = await svc.listWorks(q);
        for (const w of p.works || []) items.push({ kind: "work", work: await homeWorkRow(w) });
        cur.w = p.next;
      }
    } catch (e) { return answer(null, goErr(e)); }
    let out = items;
    if (tab === "latest") out = homeOrder(items);
    else {
      out = [];
      let posts = 0, works = 0;
      while (out.length < items.length) {
        while (posts < items.length && items[posts].kind !== "post") posts++;
        if (posts < items.length) out.push(items[posts++]);
        while (works < items.length && items[works].kind !== "work") works++;
        if (works < items.length) out.push(items[works++]);
      }
    }
    cur.s = true;
    return answer({ items: marshal("[]HomeItem", out), next: next() }, null);
  });
  function homeOrder(items) {
    const created = it => it.post ? it.post.created : it.work ? it.work.created : "";
    const num = it => {
      const id = it.post ? it.post.id : it.work ? it.work.id : "", rest = id.replace(/^[^0-9]*/, "");
      return /^[0-9]{1,18}$/.test(rest) ? Number(rest) : 0;
    };
    return stableSort(items, (a, b) => {
      if (created(a) !== created(b)) return created(a) > created(b) ? -1 : 1;
      if (num(a) !== num(b)) return num(b) - num(a);
      return a.kind === "post" && b.kind === "work" ? -1 : b.kind === "post" && a.kind === "work" ? 1 : 0;
    });
  }

  // ---------- social/discovery.go ----------

  route("assets", async () => {
    let counts;
    try { counts = await call("GET", "assets", "", undefined, "AssetCounts"); } catch (e) { return refused(goErr(e)); }
    return { ok: true, counts: marshal("{}int", counts.counts) };
  });
  route("asset", async b => {
    const id = str(b, "booth_id");
    if (!boothIDOK(id)) return refused(fail("bad_request"));
    let result;
    try { result = await call("GET", "assets/" + id, "", undefined, "AssetWorks"); } catch (e) { return refused(goErr(e)); }
    const c = await loadCommunity(false), byID = new Map();
    for (const row of workRows(c)) byID.set(row.id, row);
    let rows = [];
    const seen = new Set();
    for (const entry of result.works || []) {
      if (!workIDOK(entry.id) || seen.has(entry.id)) continue;
      seen.add(entry.id);
      rows.push(byID.has(entry.id) ? byID.get(entry.id) : workRow(entry, null, c.following, c.meID, c.meLogin));
    }
    rows = stableSort(rows, (x, y) => -timeCmp(x.created, y.created));
    return { ok: true, works: marshal("[]WorkRow", rows) };
  });
  // base/follow: a base followed in this browser (Go: in community.json); what the repository's list has of it then
  // counts as seen
  route("base/follow", async b => {
    let base = str(b, "base");
    const on = flag(b, "on");
    try { base = line(base, MAX.baseName); if (base === "") throw fail("bad_request"); } catch (e) { return refused(goErr(e)); }
    const keys = [];
    if (on) {
      try { for (const e of (await repoWorks(false)).val.works || []) if (e.base === base) keys.push(workKey(e.id)); } catch (e) { goErr(e); }
    }
    try {
      withLocal(d => {
        const i = d.bases.findIndex(f => f.base === base);
        if (i >= 0) { if (on) return false; d.bases.splice(i, 1); return true; }
        if (!on) return false;
        if (d.bases.length >= FOLLOWING_MAX) throw new GoError("关注的素体已达上限（2000 个）");
        d.bases.push({ base, at: Math.floor(Date.now() / 1000) });
        d.seen = d.seen.concat(keys);
        if (d.seen.length > SEEN_MAX) d.seen = d.seen.slice(d.seen.length - SEEN_MAX);
        return true;
      });
    } catch (e) { return refused(goErr(e)); }
    return { ok: true, bases: marshal("[]BaseFollow", localCopy().bases) };
  });

  // ---------- social/notify.go ----------
  // 「系统通知」 is kept and answered as in the program; a browser shows no Windows notifications either way.

  route("prefs", async b => {
    let on = null;
    if (has(b, "notify")) {
      if (typeof b.notify !== "boolean") return { ok: false, err: fail("bad_request").message };
      on = b.notify;
    }
    let notify = true;
    withLocal(d => {
      const changed = on !== null && d.noSystemNotice === on;
      if (changed) d.noSystemNotice = !on;
      notify = !d.noSystemNotice;
      return changed;
    });
    return { ok: true, notify };
  });

  // ---------- social/clone.go ----------

  // work/clone: 一键克隆 fills the program's modding basket, which a browser does not have (and the clone is then not
  // told to the service either)
  route("work/clone", async b => {
    if (!workIDOK(str(b, "id"))) return badRequest();
    return { ok: false, err: "请在 MioVRCA 中使用", desktopOnly: true };
  });
  route("work/post", async b => {
    let text;
    try { text = cleanText(str(b, "text"), MAX.desc, true, MAX.descLines); } catch (e) { return answer(null, goErr(e)); }
    let pictures = null;
    if (has(b, "pictures")) {
      const r = unmarshal(b, "pictures", "[]string", null);
      if (!r.ok) return badRequest();
      pictures = r.v;
    }
    if ((pictures || []).length > MAX.postPictures) return badRequest();
    if ((pictures || []).length === 0) return answer(null, new GoError("请选择作品图片"));
    const seenPic = new Set();
    for (const id of pictures) {
      if (!pictureIDOK(id) || seenPic.has(id)) return answer(null, fail("bad_picture"));
      seenPic.add(id);
    }
    let wr;
    try { wr = workReqOf(b); } catch (e) { return answer(null, goErr(e)); }
    Object.assign(wr, { desc: text, based_on: "", video: "", links: null, rights_ok: false });
    let a = accountCopy(), pw;
    const err = await attempt(async () => {
      a = needAccount();
      pw = await svc.publishWork(a.token, Object.assign({}, wr, { pictures, auto_flag: b.auto_flag === true }));
    });
    if (err) return answer(null, err);
    const res = publishedAnswer(a, pw);
    delete res.ok;
    return answer(res, null);
  });

  // ---------- social/asks.go and groups.go: what a new post carries ----------

  function postAssets(b) {
    if (!has(b, "assets") || b.assets === null) return null;
    const r = unmarshal(b, "assets", "[]string", null);
    if (!r.ok) throw fail("bad_request");
    const ids = r.v || [];
    if (ids.length > MAX.postAssets) throw fail("too_long");
    const seen = new Set();
    for (const id of ids) {
      if (!boothIDOK(id) || seen.has(id)) throw fail("bad_request");
      seen.add(id);
    }
    return ids.length === 0 ? null : ids;
  }
  function askCheck(b, pictures, reply, quote) {
    if (reply !== "" || quote !== "" || has(b, "poll") && b.poll !== null) throw fail("bad_request");
    if ((pictures || []).length === 0) throw fail("ask_picture", SERVICE_MESSAGES.ask_picture);
  }
  function postExtras(b, pictures, reply, quote) {
    const group = str(b, "group");
    if (group !== "" && (!groupIDOK(group) || reply !== "")) throw fail("bad_request");
    if (!has(b, "poll") || b.poll === null) return [group, null];
    const r = unmarshal(b, "poll", "Poll", zero("Poll"));
    if (!r.ok || (pictures || []).length > 0 || quote !== "") throw fail("bad_request");
    return [group, checkPoll(r.v)];
  }

  // ---------- social/groups.go ----------

  route("groups", async () => {
    const [names, shown] = libraryBases(), a = accountCopy();
    let page = zero("Groups");
    const err = await attempt(async () => { page = await call("GET", "groups?" + valuesEncode({ base: names }), a.token, undefined, "Groups"); });
    return answer({ groups: marshal("[]groupRow", (page.groups || []).map(groupRowOf)), bases: shown }, err);
  });
  async function groupPage(a, id) {
    let g = zero("Group"), err = null;
    err = await attempt(async () => { g = await call("GET", "groups/" + id, a.token, undefined, "Group"); });
    const r = { group: marshal("groupRow", groupRowOf(g)), pinned: null };
    if (g.pinned) r.pinned = marshal("timelinePost", await timelineRows(a)(g.pinned));
    if (err) throw err;
    return r;
  }
  route("group", async b => {
    const id = str(b, "id");
    if (!groupIDOK(id)) return badRequest();
    let r = null;
    const err = await attempt(async () => { r = await groupPage(accountCopy(), id); });
    return answer(r, err);
  });
  route("group/posts", async b => {
    const id = str(b, "id"), after = str(b, "after");
    if (!groupIDOK(id) || blen(after) > 4096) return badRequest();
    const a = accountCopy();
    let page = zero("PostPage");
    const err = await attempt(async () => { page = await call("GET", "groups/" + id + "/posts?after=" + queryEscape(after), a.token, undefined, "PostPage"); });
    const row = timelineRows(a), rows = [];
    for (const p of page.posts || []) rows.push(await row(p));
    return answer({ posts: marshal("[]timelinePost", rows), next: page.next }, err);
  });
  route("group/members", async b => {
    const id = str(b, "id"), after = str(b, "after");
    if (!groupIDOK(id) || blen(after) > 4096) return badRequest();
    let page = zero("MemberPage");
    const err = await attempt(async () => {
      page = await call("GET", "groups/" + id + "/members?after=" + queryEscape(after), accountCopy().token, undefined, "MemberPage");
    });
    return answer({ accounts: marshal("[]pagePerson", peopleRows(page.accounts)), next: page.next }, err);
  });
  route("group/join", async b => {
    const id = str(b, "id"), r0 = unmarshal(b, "on", "bool", false);
    if (!groupIDOK(id) || !r0.ok) return badRequest();
    const on = r0.v;
    let r = null;
    const err = await attempt(async () => {
      const a = needAccount();
      await call(on ? "PUT" : "DELETE", "groups/" + id + "/join", a.token);
      r = await groupPage(a, id);
    });
    if (r) r.on = on;
    return answer(r, err);
  });
  route("group/pin", async b => {
    const id = str(b, "id"), pid = str(b, "post"), r = unmarshal(b, "on", "bool", false);
    if (!groupIDOK(id) || !r.ok || r.v && !postIDOK(pid)) return badRequest();
    const on = r.v;
    return answer({ on }, await attempt(async () => {
      const a = needAccount();
      if (on) await call("PUT", "groups/" + id + "/pin", a.token, { post: pid });
      else await call("DELETE", "groups/" + id + "/pin", a.token);
    }));
  });
  route("themes", async () => {
    let themes = zero("Themes");
    const err = await attempt(async () => { themes = await call("GET", "themes", accountCopy().token, undefined, "Themes"); });
    return answer({ current: marshal("*Theme", themes.current), themes: marshal("[]Theme", themes.themes || []) }, err);
  });
  route("poll/vote", async b => {
    const id = str(b, "id"), r = unmarshal(b, "option", "int", 0);
    if (!postIDOK(id) || !r.ok || r.v < 0 || r.v >= MAX.pollOptions) return badRequest();
    let row = null;
    const err = await attempt(async () => {
      const a = needAccount();
      const p = await call("POST", "posts/" + id + "/vote", a.token, { option: r.v }, "Post");
      row = marshal("timelinePost", await timelineRows(a)(p));
    });
    return answer({ post: row }, err);
  });

  // ---------- social/asks.go ----------

  route("asks", async b => {
    const filter = firstOf(str(b, "filter"), "all"), after = str(b, "after");
    if (filter !== "all" && filter !== "open" && filter !== "solved" || blen(after) > 4096) return badRequest();
    const a = accountCopy();
    let page = zero("PostPage");
    const err = await attempt(async () => { page = await call("GET", "asks?" + valuesEncode({ filter, after }), a.token, undefined, "PostPage"); });
    const row = timelineRows(a), rows = [];
    for (const p of page.posts || []) rows.push(await row(p));
    return answer({ posts: marshal("[]timelinePost", rows), next: page.next }, err);
  });
  route("post/answer", async b => {
    const id = str(b, "id"), reply = str(b, "reply"), r = unmarshal(b, "on", "bool", false);
    if (!postIDOK(id) || !r.ok || r.v && !postIDOK(reply)) return badRequest();
    const on = r.v;
    let row = null;
    const err = await attempt(async () => {
      const a = needAccount();
      const p = on ? await call("PUT", "posts/" + id + "/answer", a.token, { reply }, "Post")
        : await call("DELETE", "posts/" + id + "/answer", a.token, undefined, "Post");
      row = marshal("timelinePost", await timelineRows(a)(p));
    });
    return answer({ post: row, on }, err);
  });

  // ---------- social/account_more.go ----------

  function boothRoute(method, path) {
    return b => withAccountLock(async () => {
      let state = zero("BoothState");
      const err = await attempt(async () => {
        const a = needAccount();
        let input;
        if (path === "me/booth" && method === "POST") {
          const shop = boothShop(str(b, "shop"));
          if (shop === "") throw fail("bad_shop", SERVICE_MESSAGES.bad_shop);
          input = { shop };
        }
        state = await call(method, path, a.token, input, "BoothState");
        // the account learns its verified shop, and its public profile is read again next time
        cache.profiles.delete(a.acc.id);
        changeAccount(a.token, d => { d.acc = Object.assign({}, d.acc, { booth: state.shop }); });
      });
      return answer({ booth: marshal("BoothState", state) }, err);
    });
  }
  route("booth", boothRoute("GET", "me/booth"));
  route("booth/start", boothRoute("POST", "me/booth"));
  route("booth/check", boothRoute("POST", "me/booth/check"));
  route("booth/unbind", boothRoute("DELETE", "me/booth"));
  route("email/code", async b => answer(null, await attempt(async () => {
    const a = needAccount();
    await call("POST", "me/email/code", a.token, { email: str(b, "email") });
  })));
  route("email/change", b => withAccountLock(async () => {
    let updated = zero("Account");
    const err = await attempt(async () => {
      const a = needAccount();
      updated = await call("POST", "me/email", a.token, { email: str(b, "email"), code: str(b, "code") }, "Account");
      if (updated.id !== a.acc.id) throw new GoError("社区服务返回的内容有误");
      changeAccount(a.token, d => { d.acc = updated; });
    });
    return answer({ account: marshal("Account", updated) }, err);
  }));
  // account/export: the zip the service makes is downloaded by the browser (Go writes it into the folder picked),
  // under the name the program gives it; the answer names no path
  route("account/export", async () => {
    const err = await attempt(async () => {
      const a = needAccount();
      await exportDownload(a.token, a.acc.handle);
    });
    return answer({ path: "" }, err);
  });
  async function exportDownload(token, handle) {
    if (BASE === "") throw unavailableError();
    const ctl = new AbortController(), timer = setTimeout(() => ctl.abort(), 30 * 60000);
    let resp, blob;
    try {
      try {
        resp = await fetch(BASE + "/api/v1/me/export", { headers: { Accept: "application/zip, application/json", Authorization: "Bearer " + token },
          redirect: "manual", credentials: "omit", cache: "no-store", signal: ctl.signal });
      } catch (e) { throw netError(e); }
      if (resp.status === 401) dropAccount(token);
      const status = resp.type === "opaqueredirect" ? 302 : resp.status;
      if (status !== 200) {
        let a = zero("ErrorAnswer");
        try {
          const text = utf8Out.decode(new Uint8Array(await resp.arrayBuffer()).subarray(0, 64 << 10));
          a = decode("ErrorAnswer", JSON.parse(text), { bad: false });
        } catch (e) { /* (not JSON) */ }
        const msg = firstOf(a.message, serviceMessage(a.error), ruleMessage(a.error), "社区服务请求失败（HTTP " + status + "）");
        throw new ServiceError(a.error, msg.split(token).join("***"), status, status >= 500);
      }
      try { blob = await resp.blob(); } catch (e) { throw netError(e); }
    } finally { clearTimeout(timer); }
    if (blob.size > 1 << 30) throw new GoError("导出的数据过大");
    // (Go opens the zip before keeping it: here its end record has to be there)
    const tail = new Uint8Array(await blob.slice(Math.max(0, blob.size - 65557)).arrayBuffer());
    let whole = false;
    for (let i = tail.length - 22; i >= 0 && !whole; i--) whole = tail[i] === 0x50 && tail[i + 1] === 0x4b && tail[i + 2] === 5 && tail[i + 3] === 6;
    if (!whole) throw new GoError("导出的数据不完整，请重试");
    const now = new Date();
    const day = "" + now.getFullYear() + String(now.getMonth() + 1).padStart(2, "0") + String(now.getDate()).padStart(2, "0");
    const link = document.createElement("a");
    link.href = URL.createObjectURL(new Blob([blob], { type: "application/zip" }));
    link.download = "MioVRCA-" + firstOf(handle, "account") + "-" + day + ".zip";
    document.body.appendChild(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(link.href), 60000);
  }

  // ---------- the entry: what the page's api() hands over for /api/community/<route> ----------

  // the body as the program reads it: JSON, at most 4 MB (a larger one reads as an empty object there), an object
  function bodyOf(body) {
    let text;
    try { text = JSON.stringify(body === undefined ? {} : body); } catch (e) { return {}; }
    if (text === undefined || blen(text) > 4 << 20) return {};
    const v = JSON.parse(text);
    return isObj(v) ? v : {};
  }
  async function callRoute(route, body) {
    const f = has2(ROUTES, route) ? ROUTES[route] : null;
    // (the program answers a route it does not have with 404, which api() turns into this error)
    if (!f) throw new Error("程序返回错误 404（/api/community/" + route + "）");
    const r = await f(bodyOf(body));
    return JSON.parse(JSON.stringify(r));
  }

  window.WebCommunity = {
    call: callRoute,
    // the rules of internal/socialapi as ported here (test/webcom_parity.py holds them to socialapi's own cases)
    rules: { normLogin, cleanText, linkKindOf, boothItemID, videoKind, checkVRChat, checkLink, checkLinks, dateOK, workIDOK,
      checkCard, checkWork, checkContacts, checkShowcase, boothShop, checkPoll, decode: (t, v) => decode(t, v, { bad: false }), marshal,
      profileLinks: PROFILE_LINKS, workLinks: WORK_LINKS },
  };
})();
