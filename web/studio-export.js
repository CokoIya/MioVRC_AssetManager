// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// the video studio's export (1.8.1): every frame drawn by StudioCore.draw, encoded with WebCodecs — H.264 and AAC into
// MP4 (mp4-muxer) where the computer can, else VP9 (or VP8) and Opus into WebM (webm-muxer) — and the pictures (a
// cover, a VRChat thumbnail, Booth images) drawn the same way. It needs nothing of the page: studio.js saves what it
// makes, test/studio_export_check.py runs it on a page of its own.
"use strict";
(function (root) {
const SC = root.StudioCore;

// the scripts of web/vendor/ (next to this one), loaded once when they are first needed
const here = typeof document !== "undefined" && document.currentScript ? document.currentScript.src.replace(/[^/]*$/, "") : "";
const loading = {};
function need(src, global) {
  if (root[global]) return Promise.resolve(root[global]);
  if (!loading[src]) loading[src] = new Promise((ok, bad) => {
    const s = document.createElement("script");
    s.src = here + src; s.async = true;
    s.onload = () => root[global] ? ok(root[global]) : bad(new Error(src));
    s.onerror = () => { delete loading[src]; bad(new Error("无法载入 " + src)); };
    document.head.append(s);
  });
  return loading[src];
}

// H.264 High at the smallest level the size and frame rate fit (4.0 for 1080p30: avc1.640028; 4.2 for 1080p60)
function avcCodec(w, h, fps) {
  const mbs = Math.ceil(w / 16) * Math.ceil(h / 16), rate = mbs * fps;
  for (const [lv, fs, mbps] of [[0x28, 8192, 245760], [0x2a, 8704, 522240], [0x32, 22080, 589824], [0x33, 36864, 983040], [0x34, 36864, 2073600]])
    if (mbs <= fs && rate <= mbps) return "avc1.6400" + lv.toString(16);
  return "avc1.640034";
}
function vp9Codec(w, h, fps) {
  const px = w * h, rate = px * fps;
  const lv = px <= 2228224 ? (rate <= 83558400 ? "40" : "41") : px <= 8912896 ? (rate <= 311951360 ? "50" : "51") : "60";
  return "vp09.00." + lv + ".08";
}
// bits per second: 12 Mbit/s (high) or 8 (standard) for 1080p30, growing with the pixels a second; VP9 needs less
function bitrate(w, h, fps, quality, vp) {
  const base = quality === "standard" ? 8e6 : 12e6, k = Math.pow(w * h * fps / (1920 * 1080 * 30), 0.75);
  return Math.round(Math.max(2e6, Math.min(40e6, base * k * (vp ? 0.75 : 1))));
}
async function supported(Kind, cfg) {
  try { const r = await Kind.isConfigSupported(cfg); return !!(r && r.supported); } catch (e) { return false; }
}
// What this computer can make: {container, mime, ext, video (encoder config), audio (config or null), label}.
// prefer: "auto" (MP4 when both H.264 and AAC can be encoded, else WebM), "mp4" or "webm"; "mp4-vp9" (VP9 and Opus
// in MP4) lets the tests run the MP4 muxer where there is no H.264 encoder (Chromium on Linux).
async function pick(w, h, fps, withAudio, prefer, quality) {
  if (typeof VideoEncoder === "undefined") throw new Error("此环境不支持视频编码（WebCodecs）");
  const A = typeof AudioEncoder !== "undefined" ? AudioEncoder : null;
  const aac = { codec: "mp4a.40.2", sampleRate: 48000, numberOfChannels: 2, bitrate: 192000 };
  const opus = { codec: "opus", sampleRate: 48000, numberOfChannels: 2, bitrate: 160000 };
  if (prefer === "mp4-vp9") {
    const cfg = { codec: vp9Codec(w, h, fps), width: w, height: h, bitrate: bitrate(w, h, fps, quality, true), framerate: fps, latencyMode: "quality" };
    if (!await supported(VideoEncoder, cfg)) throw new Error("无法编码 VP9");
    return { container: "mp4", mp4codec: "vp9", mime: "video/mp4", ext: "mp4", video: cfg, audio: withAudio ? opus : null, label: "MP4 (VP9 + Opus)" };
  }
  const avc = { codec: avcCodec(w, h, fps), width: w, height: h, bitrate: bitrate(w, h, fps, quality), framerate: fps, latencyMode: "quality", avc: { format: "avc" } };
  if (prefer !== "webm" && await supported(VideoEncoder, avc)) {
    const okAac = withAudio && A ? await supported(A, aac) : false;
    if (!withAudio || okAac) return { container: "mp4", mime: "video/mp4", ext: "mp4", video: avc, audio: withAudio ? aac : null, label: "MP4 (H.264" + (withAudio ? " + AAC" : "") + ")" };
    if (prefer === "mp4" && A && await supported(A, opus)) return { container: "mp4", mime: "video/mp4", ext: "mp4", video: avc, audio: opus, label: "MP4 (H.264 + Opus)" };
  }
  if (prefer === "mp4") throw new Error("无法编码 H.264");
  const okOpus = withAudio && A ? await supported(A, opus) : false;
  if (withAudio && !okOpus) throw new Error("无法编码音频");
  for (const v of [{ codec: vp9Codec(w, h, fps), name: "VP9", mux: "V_VP9" }, { codec: "vp8", name: "VP8", mux: "V_VP8" }]) {
    const cfg = { codec: v.codec, width: w, height: h, bitrate: bitrate(w, h, fps, quality, true), framerate: fps, latencyMode: "quality" };
    if (await supported(VideoEncoder, cfg)) return { container: "webm", mime: "video/webm", ext: "webm", video: cfg, mux: v.mux, audio: withAudio ? opus : null, label: "WebM (" + v.name + (withAudio ? " + Opus" : "") + ")" };
  }
  throw new Error("此电脑无法编码视频");
}

// The music of the video: the part from music.start to music.end at its gain, fading out over music.fade seconds at
// the end, as 48 kHz stereo.
async function renderAudio(buffer, music, rate = 48000) {
  const dur = Math.max(0.05, music.end - music.start), frames = Math.ceil(dur * rate);
  const ctx = new OfflineAudioContext(2, frames, rate), src = ctx.createBufferSource(), g = ctx.createGain();
  const gain = Math.max(0, Math.min(2, typeof music.gain === "number" ? music.gain : 1)), fade = Math.max(0, Math.min(dur, music.fade || 0));
  src.buffer = buffer;
  g.gain.setValueAtTime(gain, 0);
  if (fade > 0) { g.gain.setValueAtTime(gain, dur - fade); g.gain.linearRampToValueAtTime(0, dur); }
  src.connect(g); g.connect(ctx.destination);
  src.start(0, Math.max(0, music.start), dur);
  return ctx.startRendering();
}

// a turn of the event loop that background throttling does not stretch to a second (timers it does)
const chan = typeof MessageChannel !== "undefined" ? new MessageChannel() : null, waiting = [];
if (chan) chan.port1.onmessage = () => { const f = waiting.shift(); if (f) f(); };
const tick = () => new Promise(r => { if (chan) { waiting.push(r); chan.port2.postMessage(0); } else setTimeout(r, 0); });
// wait while more than max frames are queued in the encoder
async function drain(enc, max, failed) {
  while (enc.encodeQueueSize > max && !failed()) {
    if ("ondequeue" in enc) await new Promise(r => { const t = setTimeout(r, 1000); enc.addEventListener("dequeue", () => { clearTimeout(t); r(); }, { once: true }); });
    else await tick();
  }
}
class Cancelled extends Error { constructor() { super("已取消"); this.cancelled = true; } }

// Encodes the whole video. o: {edit (StudioCore.prep'd), res, qr, audio (an AudioBuffer of the music, or null), music,
// quality, prefer, signal ({cancelled}), onProgress({stage, done, total})}. Answers {bytes, mime, ext, label, frames,
// seconds, maxQueue}.
async function run(o) {
  const E = o.edit, w = E.w, h = E.h, fps = E.fps, total = Math.max(1, Math.round(E.duration * fps)), t0 = Date.now();
  const sig = o.signal || {}, prog = o.onProgress || (() => {});
  const plan = await pick(w, h, fps, !!(o.audio && o.music), o.prefer || "auto", o.quality);
  const M = plan.container === "mp4" ? await need("vendor/mp4-muxer.js", "Mp4Muxer") : await need("vendor/webm-muxer.js", "WebMMuxer");
  let pcm = null;
  if (plan.audio) { prog({ stage: "audio", done: 0, total }); pcm = await renderAudio(o.audio, o.music, 48000); }
  if (sig.cancelled) throw new Cancelled();
  const mux = plan.container === "mp4"
    ? new M.Muxer(Object.assign({ target: new M.ArrayBufferTarget(), video: { codec: plan.mp4codec || "avc", width: w, height: h, frameRate: fps }, fastStart: "in-memory", firstTimestampBehavior: "offset" },
      plan.audio ? { audio: { codec: plan.audio.codec === "opus" ? "opus" : "aac", numberOfChannels: 2, sampleRate: 48000 } } : {}))
    : new M.Muxer(Object.assign({ target: new M.ArrayBufferTarget(), video: { codec: plan.mux, width: w, height: h, frameRate: fps }, firstTimestampBehavior: "offset" },
      plan.audio ? { audio: { codec: "A_OPUS", numberOfChannels: 2, sampleRate: 48000 } } : {}));
  let err = null;
  const failed = () => !!err;
  const venc = new VideoEncoder({ output: (c, m) => { try { mux.addVideoChunk(c, m); } catch (e) { err = err || e; } }, error: e => { err = err || e; } });
  venc.configure(plan.video);
  let aenc = null;
  const close = () => { for (const e of [venc, aenc]) try { if (e && e.state !== "closed") e.close(); } catch (x) {} };
  try {
    if (pcm) { // the music first, in pieces of 4096 samples (small next to the frames)
      aenc = new AudioEncoder({ output: (c, m) => { try { mux.addAudioChunk(c, m); } catch (e) { err = err || e; } }, error: e => { err = err || e; } });
      aenc.configure(plan.audio);
      const L = pcm.getChannelData(0), R = pcm.numberOfChannels > 1 ? pcm.getChannelData(1) : L, N = 4096;
      for (let i = 0; i < pcm.length; i += N) {
        const n = Math.min(N, pcm.length - i), data = new Float32Array(n * 2);
        data.set(L.subarray(i, i + n), 0); data.set(R.subarray(i, i + n), n);
        const ad = new AudioData({ format: "f32-planar", sampleRate: 48000, numberOfFrames: n, numberOfChannels: 2, timestamp: Math.round(i * 1e6 / 48000), data });
        aenc.encode(ad); ad.close();
        await drain(aenc, 16, failed);
        if (err) throw err;
        if (sig.cancelled) throw new Cancelled();
      }
    }
    const canvas = new OffscreenCanvas(w, h), ctx = canvas.getContext("2d", { alpha: false });
    const R = SC.renderer(E, o.res, { qr: o.qr, canvas: (cw, ch) => new OffscreenCanvas(cw, ch) });
    let maxQueue = 0;
    for (let i = 0; i < total; i++) {
      if (sig.cancelled) throw new Cancelled();
      if (err) throw err;
      SC.draw(ctx, i / fps, R);
      const vf = new VideoFrame(canvas, { timestamp: Math.round(i * 1e6 / fps), duration: Math.round(1e6 / fps) });
      venc.encode(vf, { keyFrame: i % (2 * fps) === 0 });
      vf.close();
      maxQueue = Math.max(maxQueue, venc.encodeQueueSize);
      await drain(venc, 4, failed);
      if (i % 3 === 2) await tick();
      prog({ stage: "video", done: i + 1, total });
    }
    prog({ stage: "finish", done: total, total });
    await venc.flush();
    if (aenc) await aenc.flush();
    if (err) throw err;
    mux.finalize();
    close();
    const bytes = new Uint8Array(mux.target.buffer);
    return { bytes, mime: plan.mime, ext: plan.ext, label: plan.label, codec: plan.video.codec, frames: total, seconds: (Date.now() - t0) / 1000, maxQueue };
  } catch (e) {
    close();
    throw e;
  }
}

// One picture of the edit: drawn at time t at its own size (or w × h), as a Blob (JPEG or PNG).
async function picture(edit, res, qr, opt) {
  const E = edit.beatT ? edit : SC.prep(edit), w = opt.w || E.w, h = opt.h || E.h;
  const c = new OffscreenCanvas(w, h), x = c.getContext("2d", { alpha: false });
  const R = SC.renderer(E, res, { w, h, qr, still: true, noCaptions: !!opt.noCaptions, noMark: !!opt.noMark, canvas: (cw, ch) => new OffscreenCanvas(cw, ch) });
  SC.draw(x, opt.t || 0, R);
  return c.convertToBlob({ type: opt.type || "image/jpeg", quality: opt.quality || 0.92 });
}

// base64 of some bytes (the browser's own encoder, through a data: address)
function b64(bytes) {
  return new Promise((ok, bad) => {
    const fr = new FileReader();
    fr.onload = () => { const s = String(fr.result); ok(s.slice(s.indexOf(",") + 1)); };
    fr.onerror = () => bad(fr.error || new Error("base64"));
    fr.readAsDataURL(new Blob([bytes]));
  });
}

root.StudioExport = { need, pick, run, renderAudio, picture, b64, avcCodec, vp9Codec, bitrate, tick };
})(typeof globalThis !== "undefined" ? globalThis : this);
