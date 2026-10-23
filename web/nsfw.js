// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// The check of a picture before it goes to the community (a post's pictures, a work's, the avatar: web/social.js):
// NSFWJS's MobileNetV2 model (web/vendor/nsfwjs/) run on TensorFlow.js (web/vendor/tf.es2017.min.js), both loaded
// the first time a picture is checked. The model gives five probabilities; a picture over the bars below is likely
// sensitive. Whatever fails (the script, the model, the backend, a picture it cannot read, a check that takes too
// long) leaves the picture unchecked: posting never stops for the check, and the failure goes to the console only.
"use strict";

// the model's outputs in their order (NSFWJS 4.4.0, src/nsfw_classes.ts), and its input's side
const NSFW_CLASSES = ["Drawing", "Hentai", "Neutral", "Porn", "Sexy"];
const NSFW_SIZE = 224;
// Likely sensitive: Porn + Hentai ≥ 0.85, or Sexy ≥ 0.85. Most pictures here are anime-style avatars, which the
// model tends to put between Drawing and Hentai, and outfits that show skin (swimsuits, short skirts), which it
// puts towards Sexy: that is the community's normal content, so both bars sit well above where such pictures
// usually land, and few are flagged that should not be. (On the 118 Booth covers of a real library, 0.6 flagged
// two ordinary outfits — leggings at 0.81, a sweater at 0.65 — besides two skin materials shown on bare bodies at
// 0.96 and above; 0.85 keeps only those two.) Porn and Hentai are added up because the model splits
// explicit pictures between the two (a render of an avatar can go either way); Sexy alone (suggestive, not
// explicit) needs a clearer answer. Borderline pictures it misses are left to the players' reports, and a flag
// only ticks 「敏感内容」: unticked, it reaches the admin as one report, never a mark.
const NSFW_EXPLICIT = 0.85, NSFW_SEXY = 0.85;
const NSFW_WAIT = 15000; // the longest a check (the first load included) is waited for
let nsfwModel = null; // the model's promise from the first check on; a failed load stays failed until the page reloads

function nsfwFlagged(s) { return s.Porn + s.Hentai >= NSFW_EXPLICIT || s.Sexy >= NSFW_SEXY; }

function nsfwScript(src) {
  if (window.tf) return Promise.resolve();
  return new Promise((ok, bad) => {
    const s = document.createElement("script");
    s.src = src; s.async = true;
    s.onload = () => window.tf ? ok() : bad(new Error("no tf in " + src));
    s.onerror = () => bad(new Error("cannot load " + src));
    document.head.append(s);
  });
}

// TensorFlow.js and the model, once (WebGL where there is a GPU for it, else the CPU); a first prediction on
// zeros gets the backend's programs ready, as NSFWJS's load does
function nsfwLoad() {
  if (!nsfwModel) nsfwModel = (async () => {
    await nsfwScript("vendor/tf.es2017.min.js");
    const model = await tf.loadLayersModel("vendor/nsfwjs/model.json");
    const warm = tf.tidy(() => model.predict(tf.zeros([1, NSFW_SIZE, NSFW_SIZE, 3])));
    await warm.data(); warm.dispose();
    return model;
  })();
  return nsfwModel;
}

// the five probabilities of a picture (a Blob or File) as {Drawing, Hentai, Neutral, Porn, Sexy}. NSFWJS scales
// the pixels to 0..1 and squashes the picture to 224×224; here the canvas does the squashing (over white, as the
// uploads are flattened), which keeps a large picture's tensors small.
async function nsfwScores(blob) {
  const model = await nsfwLoad();
  const img = await createImageBitmap(blob);
  const cv = document.createElement("canvas"); cv.width = cv.height = NSFW_SIZE;
  const g = cv.getContext("2d", { willReadFrequently: true }); g.imageSmoothingQuality = "high";
  g.fillStyle = "#fff"; g.fillRect(0, 0, NSFW_SIZE, NSFW_SIZE); g.drawImage(img, 0, 0, NSFW_SIZE, NSFW_SIZE);
  if (img.close) img.close();
  const pixels = g.getImageData(0, 0, NSFW_SIZE, NSFW_SIZE);
  const out = tf.tidy(() => model.predict(tf.browser.fromPixels(pixels).toFloat().div(255).reshape([1, NSFW_SIZE, NSFW_SIZE, 3])));
  const p = await out.data(); out.dispose();
  return Object.fromEntries(NSFW_CLASSES.map((c, i) => [c, p[i]]));
}

// {flagged, scores} for a picture, or null when it could not be checked. A page under test sets
// window.nsfwTestScores (the picture → the five probabilities, or a promise of them) in place of the model.
async function nsfwCheck(blob) {
  let timer = 0;
  try {
    const run = typeof window.nsfwTestScores === "function" ? Promise.resolve(window.nsfwTestScores(blob)) : nsfwScores(blob);
    const late = new Promise((_, bad) => { timer = setTimeout(() => bad(new Error("no answer in " + NSFW_WAIT + " ms")), NSFW_WAIT); });
    const scores = await Promise.race([run, late]);
    return { flagged: nsfwFlagged(scores), scores };
  } catch (e) {
    console.warn("picture check skipped:", e);
    return null;
  } finally { clearTimeout(timer); }
}
