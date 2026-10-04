// Loads web/checkup.js outside a browser and plays the one-click fixes of the check-up panel against canned
// answers of the server. Prints what the panel showed, as JSON, for TestCheckupPanelFixes.
//   node checkup_harness.js <path to web/checkup.js>
const fs = require("fs"), vm = require("vm");
const src = fs.readFileSync(process.argv[2], "utf8");
const answers = [], asks = [], toasts = [], calls = [];
let polls = 0;
const ctx = {
  console, Date, Math, JSON, Object, Set, Map, Array, String, Promise, Error,
  document: { addEventListener() {}, hidden: false, documentElement: {} },
  setInterval() {}, setTimeout,
  esc: s => String(s ?? ""), fmtTime: t => "T" + t, toast: m => { toasts.push(m); }, confirm: m => { asks.push(m); return true; },
  S: { view: "pipe", projs: [] }, AI: { proj: null }, loadProjects() {}, $: () => ({ dataset: {}, classList: { contains: () => false } }), put: () => false, focusMark: () => () => {},
  pipeActive: () => true, aiPoll: () => { polls++; },
  api: async (path, body) => { calls.push(path + " " + (body.record || body.kind || "")); return answers.shift() || { ok: false, err: "no answer" }; },
  I18N: { t: s => s }, ICON: {},
};
vm.createContext(ctx);
vm.runInContext(src + "\n;globalThis.T = { CK, ckFix, ckRevert, ckSet, ckDoneHTML, ckFixHTML, ckFixText };", ctx);
const { CK, ckFix, ckRevert, ckSet, ckDoneHTML, ckFixHTML, ckFixText } = ctx.T;
const P = "C:/proj", REC = n => "UserSettings/MioVRCA/fixes/R" + n + ".json";
const tex = (n, fixable) => Object.assign({ id: "textures", group: "look", label: "4096 像素以上的贴图", level: n ? "warn" : "ok", value: n }, fixable === undefined ? {} : { fixable });
const item = (id, n) => ({ id, group: id === "missing" ? "limit" : "figure", label: id, level: n ? "fail" : "ok", value: n });
const rec = (items, texFix) => ({ project: P, at: 1, avatar: "Kaguya", result: { avatar: "Kaguya", path: "Kaguya", items }, texFix });
const fixOf = (n, changed) => ({ kind: "textures", max: 2048, changed, record: REC(n), items: [{ path: "Assets/t" + n + ".png", from: 4096, to: 2048 }], memoryBefore: 42.7, memoryAfter: 10.7, at: 10 * n });
const text = h => h.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
const note = () => { const h = ckDoneHTML(P, "textures", "p"); return { revert: /data-ckrevert/.test(h), text: text(h) }; };
const out = {};
(async () => {
  // ---- the questions, and the button under an item
  ckSet(P, { ok: true, record: rec([tex(3, 2), item("fig.lights", 2), item("missing", 4)]), can: true });
  for (const kind of ["textures", "lights", "missing"]) {
    answers.push({ ok: false, err: "x" });
    await ckFix(P, kind);
    out["ask_" + kind] = asks.pop();
  }
  out.btnTextures = ckFixHTML(tex(3, 2), P);
  out.btnNothingToLower = ckFixHTML(tex(1, 0), P);
  out.btnOlderPlugin = ckFixHTML(tex(1), P); // no count from the plugin: the button as before
  out.btnMissing = text(ckFixHTML(item("missing", 4), P));
  // ---- a second click that changes nothing keeps the first fix's note and its 「恢复」
  const fix1 = fixOf(1, 2);
  answers.push({ ok: true, record: rec([tex(1)], fix1), can: true, fix: fix1 });
  await ckFix(P, "textures");
  out.first = note(); out.firstToast = toasts[toasts.length - 1];
  answers.push({ ok: true, record: rec([tex(1)], fix1), can: true, fix: { kind: "textures", max: 2048, changed: 0, items: [], skipped: [{ t: "Packages/x/big.png", n: "不在 Assets 文件夹内" }], at: 20 } });
  await ckFix(P, "textures");
  out.second = note(); out.secondToast = toasts[toasts.length - 1];
  ckSet(P, { ok: true, record: rec([tex(1)], fix1), can: true }); // the page asks the server again
  out.afterPoll = note();
  // ---- a second fix that changes something: taking it back offers the first one again
  const fix2 = fixOf(2, 1);
  answers.push({ ok: true, record: rec([tex(0)], fix2), can: true, fix: fix2 });
  await ckFix(P, "textures");
  out.third = note();
  answers.push({ ok: true, record: rec([tex(1)], fix1), can: true, revert: { kind: "textures", reverted: 1, items: [] } });
  await ckRevert(P);
  out.afterRevert2 = note(); out.revert2Asked = calls[calls.length - 1]; out.revert2Toast = toasts[toasts.length - 1];
  // ---- a texture that was moved since: the revert says which, the record is kept and 「恢复」 stays
  answers.push({ ok: true, record: rec([tex(2)], fix1), can: true, revert: { kind: "textures", reverted: 1, items: [{ path: "Assets/t1.png", to: 4096 }], missing: ["Assets/moved.png"] } });
  await ckRevert(P);
  out.afterMissing = note(); out.missingAsked = calls[calls.length - 1];
  ckSet(P, { ok: true, record: rec([tex(2)], fix1), can: true });
  out.afterMissingPoll = note();
  // ---- everything put back: nothing more to take back
  answers.push({ ok: true, record: rec([tex(3)]), can: true, revert: { kind: "textures", reverted: 2, items: [], missing: [] } });
  await ckRevert(P);
  out.afterRevert1 = note();
  // ---- a fix whose answer never arrived: the server found its record in the project
  ckSet(P, { ok: true, record: rec([tex(0)], { kind: "textures", max: 2048, changed: 3, record: REC(9), items: [{ path: "Assets/a.png", from: 4096, to: 2048 }], adopted: true, at: 90 }), can: true });
  out.adopted = note();
  // ---- a scene fix: the pipeline page asks for the session again (「撤销上一步」), a fix that changed nothing leaves no note
  const before = polls;
  answers.push({ ok: true, record: rec([item("fig.lights", 0)]), can: true, fix: { kind: "lights", changed: 2, items: [], undo: "MioVRCA 移除灯光", at: 30 } });
  await ckFix(P, "lights");
  out.lightsPolled = polls - before;
  out.lightsNote = text(ckDoneHTML(P, "lights", "p"));
  out.revertText = ckFixText({ reverted: 3 });
  console.log(JSON.stringify(out));
})().catch(e => { console.log(JSON.stringify({ error: String(e && e.stack || e) })); });
