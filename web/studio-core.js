// SPDX-License-Identifier: GPL-3.0-only OR PolyForm-Noncommercial-1.0.0
// Copyright (c) 2026 天川澪 (Coko_Iya). Part of MioVRCA: https://github.com/CokoIya/MioVRC_AssetManager (terms in LICENSE.md)
// the video studio's own arithmetic (1.8.1, 宣传视频): the music's beats, downbeats, sections and drop, and one frame of
// an edit (docs/宣传视频-分镜格式.md) drawn on a 2D canvas. Nothing here touches the page: studio.js hands it the
// samples, the pictures and a canvas, the node tests (test/studio_test.mjs) hand it click tracks and a context that
// writes down what is drawn. The same time always gives the same frame. In a worker it answers {rate, mono} with
// the analysis.
"use strict";
(function (root) {

// ---------- the music ----------
// decode (studio.js) → mono → onset envelope (STFT, log band energies, positive flux) → tempo (autocorrelation over
// 70–180 BPM, a prior near 120) → beats (dynamic programming, Ellis 2007) → each beat moved onto the attack it stands
// for → downbeats (the phase of 4 with the strongest onsets) → bars' loudness → sections and the drop.
const AN = { rate: 22050, n: 1024, hop: 256, minBpm: 70, maxBpm: 180, prior: 120, tight: 100 };

function fftPlan(n) {
  const bits = Math.round(Math.log2(n)), rev = new Uint32Array(n), cos = new Float64Array(n / 2), sin = new Float64Array(n / 2);
  for (let i = 0; i < n; i++) { let r = 0; for (let b = 0, v = i; b < bits; b++, v >>= 1) r = (r << 1) | (v & 1); rev[i] = r; }
  for (let i = 0; i < n / 2; i++) { cos[i] = Math.cos(2 * Math.PI * i / n); sin[i] = Math.sin(2 * Math.PI * i / n); }
  return { n, rev, cos, sin };
}
// forward transform in place
function fft(p, re, im) {
  const n = p.n, rev = p.rev, cos = p.cos, sin = p.sin;
  for (let i = 0; i < n; i++) {
    const j = rev[i];
    if (j > i) { let t = re[i]; re[i] = re[j]; re[j] = t; t = im[i]; im[i] = im[j]; im[j] = t; }
  }
  for (let size = 2; size <= n; size <<= 1) {
    const half = size >> 1, step = n / size;
    for (let i = 0; i < n; i += size) {
      for (let j = i, k = 0; j < i + half; j++, k += step) {
        const l = j + half, c = cos[k], s = sin[k];
        const tr = re[l] * c + im[l] * s, ti = im[l] * c - re[l] * s;
        re[l] = re[j] - tr; im[l] = im[j] - ti; re[j] += tr; im[j] += ti;
      }
    }
  }
}
// bands on the mel scale from 30 Hz, as ranges of bins
function bandsOf(n, sr, count) {
  const mel = f => 2595 * Math.log10(1 + f / 700), hz = m => 700 * (Math.pow(10, m / 2595) - 1);
  const top = Math.min(11000, sr / 2 - 1), m0 = mel(30), m1 = mel(top), out = [];
  let prev = 1;
  for (let b = 0; b < count; b++) {
    const lo = Math.max(prev, Math.round(hz(m0 + (m1 - m0) * b / count) * n / sr));
    const hi = Math.max(lo + 1, Math.round(hz(m0 + (m1 - m0) * (b + 1) / count) * n / sr));
    if (hi > n / 2) break;
    out.push({ lo, hi, top: hi * sr / n }); prev = hi;
  }
  return out;
}

// The onset envelope (and the same over the low bands, for the downbeats) and each frame's power. Frame f is centred
// on sample f·hop.
function onsets(x, sr) {
  const N = AN.n, H = AN.hop, half = N / 2, nf = Math.floor(x.length / H) + 1;
  const plan = fftPlan(N), win = new Float64Array(N), bands = bandsOf(N, sr, 40), B = bands.length;
  for (let i = 0; i < N; i++) win[i] = 0.5 - 0.5 * Math.cos(2 * Math.PI * i / N);
  const L = new Float32Array(nf * B), Pw = new Float32Array(nf * B), pw = new Float64Array(nf), re = new Float64Array(N), im = new Float64Array(N);
  let top = -Infinity;
  for (let f = 0; f < nf; f += 2) { // two frames in one transform: f as the real part, f + 1 as the imaginary part
    for (let i = 0; i < N; i++) {
      const s = f * H - half + i, s2 = s + H;
      re[i] = s >= 0 && s < x.length ? x[s] * win[i] : 0;
      im[i] = f + 1 < nf && s2 >= 0 && s2 < x.length ? x[s2] * win[i] : 0;
    }
    fft(plan, re, im);
    for (let b = 0; b < B; b++) {
      let pa = 0, pb = 0;
      for (let k = bands[b].lo; k < bands[b].hi; k++) {
        const nk = (N - k) % N;
        const ar = (re[k] + re[nk]) / 2, ai = (im[k] - im[nk]) / 2, br = (im[k] + im[nk]) / 2, bi = (re[nk] - re[k]) / 2;
        pa += ar * ar + ai * ai; pb += br * br + bi * bi;
      }
      const la = 10 * Math.log10(pa + 1e-12);
      L[f * B + b] = la; Pw[f * B + b] = pa; pw[f] += pa; if (la > top) top = la;
      if (f + 1 < nf) { const lb = 10 * Math.log10(pb + 1e-12); L[(f + 1) * B + b] = lb; Pw[(f + 1) * B + b] = pb; pw[f + 1] += pb; if (lb > top) top = lb; }
    }
  }
  const floor = top - 80, nlow = Math.max(1, bands.filter(b => b.top <= 200).length);
  for (let i = 0; i < L.length; i++) if (L[i] < floor) L[i] = floor;
  // env: the flux of the log energies (every onset counts, quiet ones too); low: the same below 200 Hz; loud: the flux
  // of the energies themselves (what is loud counts), for telling a tempo from its double
  const env = new Float32Array(nf), low = new Float32Array(nf), loud = new Float32Array(nf);
  for (let f = 1; f < nf; f++) {
    let s = 0, sl = 0, sp = 0;
    for (let b = 0; b < B; b++) {
      const d = L[f * B + b] - L[(f - 1) * B + b], dp = Pw[f * B + b] - Pw[(f - 1) * B + b];
      if (d > 0) { s += d; if (b < nlow) sl += d; }
      if (dp > 0) sp += dp;
    }
    env[f] = s / B; low[f] = sl / nlow; loud[f] = Math.sqrt(sp);
  }
  return { env: lift(env, sr / H), low: lift(low, sr / H), loud: lift(loud, sr / H), pw };
}
// less the local mean (a quarter of a second), never below nothing, over its standard deviation
function lift(e, fps) {
  const n = e.length, w = Math.max(1, Math.round(fps * 0.125)), sum = new Float64Array(n + 1), out = new Float32Array(n);
  for (let i = 0; i < n; i++) sum[i + 1] = sum[i] + e[i];
  let s = 0, s2 = 0;
  for (let i = 0; i < n; i++) {
    const a = Math.max(0, i - w), b = Math.min(n, i + w + 1);
    const v = Math.max(0, e[i] - (sum[b] - sum[a]) / (b - a));
    out[i] = v; s += v; s2 += v * v;
  }
  const sd = n > 1 ? Math.sqrt(Math.max(0, (s2 - s * s / n) / (n - 1))) : 0;
  if (sd > 0) for (let i = 0; i < n; i++) out[i] /= sd;
  return out;
}

// The envelope's autocorrelation, on the envelope widened to a few frames (a click is a frame or two wide, and a period
// between two whole lags would split its peak between them). A lag's worth: between whole lags by straight lines; the
// k-th multiple of a lag may be up to k/2 frames off.
function autocorr(env, maxLag) {
  const n = env.length, e = new Float64Array(n), ac = new Float64Array(maxLag + 2);
  for (let i = 0; i < n; i++) e[i] = (env[Math.max(0, i - 2)] + 4 * env[Math.max(0, i - 1)] + 6 * env[i] + 4 * env[Math.min(n - 1, i + 1)] + env[Math.min(n - 1, i + 2)]) / 16;
  for (let L = 1; L <= maxLag; L++) {
    let s = 0;
    for (let i = 0, m = n - L; i < m; i++) s += e[i] * e[i + L];
    ac[L] = s / (n - L);
  }
  return (L, k = 1) => {
    if (L >= maxLag) return 0;
    const i0 = Math.floor(L), f = L - i0;
    let m = ac[i0] * (1 - f) + ac[i0 + 1] * f;
    for (let i = Math.ceil(L - k / 2); k > 1 && i <= Math.floor(L + k / 2); i++) if (i >= 1 && i <= maxLag && ac[i] > m) m = ac[i];
    return m;
  };
}
// The beat period in envelope frames: autocorrelation, each lag scored with its double and fourfold (bars), times a
// log-normal prior (one octave) around 120 BPM.
function tempoPeriod(env, fps) {
  const n = env.length, lo = Math.floor(fps * 60 / AN.maxBpm), hi = Math.ceil(fps * 60 / AN.minBpm);
  const maxLag = Math.min(n - 2, hi * 4 + 2);
  if (maxLag <= hi) return fps * 60 / AN.prior;
  const at = autocorr(env, maxLag);
  const score = L => (at(L) + 0.5 * at(2 * L, 2) + 0.25 * at(4 * L, 4)) * Math.exp(-0.5 * Math.pow(Math.log2(fps * 60 / L / AN.prior), 2));
  let best = lo, bv = -Infinity;
  for (let L = lo; L <= hi; L++) { const v = score(L); if (v > bv) { bv = v; best = L; } }
  if (best > lo && best < hi) {
    const a = score(best - 1), c = score(best + 1), den = a - 2 * bv + c;
    if (den < 0) best += Math.max(-0.5, Math.min(0.5, 0.5 * (a - c) / den));
  }
  return best;
}

// Beats on the envelope by dynamic programming: each frame's score is its onset plus the best earlier beat at about
// one period, a step that is not one period costs tightness · log(step/period)²; within the first period a beat may
// start the run (as if the one before were before the start). Frames before the first onset start nothing. The last
// beat is the best score of the last two periods.
function trackBeats(env, per) {
  const n = env.length;
  if (n < 4 || !(per > 1)) return [];
  const gw = Math.max(1, Math.round(per)), g = [];
  for (let i = -gw; i <= gw; i++) g.push(Math.exp(-0.5 * Math.pow(i * 32 / per, 2)));
  const ls = new Float64Array(n);
  let mx = 0;
  for (let i = 0; i < n; i++) {
    let s = 0;
    for (let k = -gw; k <= gw; k++) { const j = i + k; if (j >= 0 && j < n) s += env[j] * g[k + gw]; }
    ls[i] = s; if (s > mx) mx = s;
  }
  const lo = Math.max(1, Math.round(per / 2)), hi = Math.max(lo + 1, Math.round(per * 2));
  const pen = new Float64Array(hi + 1);
  for (let d = lo; d <= hi; d++) { const z = Math.log(d / per); pen[d] = -AN.tight * z * z; }
  const cum = new Float64Array(n), back = new Int32Array(n).fill(-1);
  let first = true;
  for (let i = 0; i < n; i++) {
    // (the one before the start: free within the first period, else the cost of the shortest step past the start)
    let best = i < per ? 0 : i + 1 <= hi ? pen[i + 1] : -Infinity, arg = -1;
    for (let d = lo; d <= hi && d <= i; d++) { const v = cum[i - d] + pen[d]; if (v > best) { best = v; arg = i - d; } }
    cum[i] = ls[i] + (best > -Infinity ? best : 0);
    if (first && ls[i] < 0.01 * mx) back[i] = -1;
    else { back[i] = arg; first = false; }
  }
  let tail = n - 1;
  for (let i = n - 1, a = Math.max(0, n - Math.round(2 * per)); i >= a; i--) if (cum[i] > cum[tail]) tail = i;
  const out = [tail];
  while (back[out[out.length - 1]] >= 0) out.push(back[out[out.length - 1]]);
  return out.reverse();
}

// The attack a beat stands for: the steepest rise of the short-time energy (of the first difference: the clicks,
// not the bass) just around the frame the envelope found, when there is a clear one (≥ 6 dB in 3 ms); else the
// frame's time, a little later (the flux of a window runs ahead of the sound).
const LATE = 0.012;
function refine(x, sr, t0) {
  const W = 128, S = 32, a = Math.max(1, Math.floor((t0 - 0.03) * sr)), b = Math.min(x.length - W, Math.ceil((t0 + 0.05) * sr));
  if (b <= a) return t0 + LATE;
  const le = [];
  for (let s = a; s <= b; s += S) {
    let e = 0;
    for (let i = s; i < s + W; i++) { const d = x[i] - x[i - 1]; e += d * d; }
    le.push(10 * Math.log10(e / W + 1e-14));
  }
  let best = 6, at = -1;
  for (let k = 2; k < le.length; k++) { const d = le[k] - le[k - 2]; if (d > best) { best = d; at = k; } }
  return at < 0 ? t0 + LATE : (a + at * S + W - S) / sr;
}

// what the page needs from a song: {duration, bpm, beats (s), down (indices into beats), drop (s or null), peak (s),
// sections [{at, db}], bars (s), peaks (the waveform, 1000 maxima)}
function analyze(mono, rate) {
  if (!mono.length || !Number.isFinite(rate) || rate <= 0) throw new Error("音乐分析结果为空，请重新选择音乐");
  const d = Math.max(1, Math.round(rate / AN.rate)), sr = rate / d, len = Math.floor(mono.length / d), x = new Float32Array(len);
  for (let i = 0; i < len; i++) { let s = 0; const j = i * d; for (let k = 0; k < d; k++) s += mono[j + k]; x[i] = s / d; }
  const duration = mono.length / rate, fps = sr / AN.hop;
  const { env, low, loud, pw } = onsets(x, sr);
  let per = tempoPeriod(env, fps), frames = trackBeats(env, per);
  // twice as fast when what falls halfway between the beats is loud as well (a snare between kicks, the other clicks
  // of a metronome), not when it is only hi-hats
  if (60 * fps / (per / 2) <= AN.maxBpm + 0.5 && frames.length > 4) {
    const near = f => { let m = 0; for (let k = Math.round(f) - 2; k <= Math.round(f) + 2; k++) if (k >= 0 && k < loud.length && loud[k] > m) m = loud[k]; return m; };
    let on = 0, mid = 0;
    for (let i = 0; i + 1 < frames.length; i++) { on += near(frames[i]); mid += near((frames[i] + frames[i + 1]) / 2); }
    if (on > 0 && mid / on >= 0.25) { per /= 2; frames = trackBeats(env, per); }
  }
  let beats = frames.map(f => refine(x, sr, f * AN.hop / sr));
  // (an attack moved past the next one would turn the order round)
  beats = beats.filter((t, i) => i === 0 || t > beats[i - 1] + 0.1);
  let p = per / fps;
  // the period over the beats found: least squares of time against the beat's number, once more without the beats a
  // quarter period off the line (an onset before the music starts)
  for (let pass = 0, use = beats.map((t, i) => [i, t]); pass < 2 && use.length >= 4; pass++) {
    const n = use.length, mi = use.reduce((a, b) => a + b[0], 0) / n, mt = use.reduce((a, b) => a + b[1], 0) / n;
    let sxy = 0, sxx = 0;
    for (const [i, t] of use) { sxy += (i - mi) * (t - mt); sxx += (i - mi) * (i - mi); }
    if (!(sxy > 0)) break;
    p = sxy / sxx;
    const keep = use.filter(([i, t]) => Math.abs(mt + (i - mi) * p - t) < p / 4);
    if (keep.length === use.length) break;
    use = keep;
  }
  if (!beats.length) beats = [0];
  // the grid runs through the whole song, at the same period before the first beat found and after the last
  while (beats[0] - p >= -0.005) beats.unshift(Math.max(0, beats[0] - p));
  while (beats[beats.length - 1] + p < duration - 0.01) beats.push(beats[beats.length - 1] + p);
  beats = beats.map(t => Math.round(t * 1000) / 1000);
  const at = (e, t) => { let m = 0; const f = Math.round(t * fps); for (let k = f - 3; k <= f + 1; k++) if (k >= 0 && k < e.length && e[k] > m) m = e[k]; return m; };
  const score = [0, 0, 0, 0], cnt = [0, 0, 0, 0];
  // (by what is loud: every onset's log energy rises about as much, a loud kick's and a soft one's)
  beats.forEach((t, i) => { score[i % 4] += at(loud, t) + 0.1 * (at(env, t) + at(low, t)); cnt[i % 4]++; });
  let phase = 0;
  for (let k = 1; k < 4; k++) if (cnt[k] && score[k] / cnt[k] > score[phase] / Math.max(1, cnt[phase]) + 1e-9) phase = k;
  const down = [];
  for (let i = phase; i < beats.length; i += 4) down.push(i);
  // the bars' loudness, and where it changes: a rise of 3 dB and more between the four bars before and after is a
  // drop candidate; the biggest is the drop
  const bars = down.map(i => beats[i]), barDb = bars.map((t, j) => {
    const a = Math.max(0, Math.round(t * fps)), b = Math.min(pw.length, Math.round((j + 1 < bars.length ? bars[j + 1] : Math.min(duration, t + 4 * p)) * fps));
    let s = 0; for (let f = a; f < b; f++) s += pw[f];
    return b > a ? 10 * Math.log10(s / (b - a) + 1e-12) : -120;
  });
  const nov = bars.map((t, j) => {
    const k = Math.min(4, j, bars.length - j);
    if (k < 2) return 0;
    let a = 0, b = 0;
    for (let i = 1; i <= k; i++) { a += barDb[j - i]; b += barDb[j + i - 1]; }
    return (b - a) / k;
  });
  let drop = null, dv = 3;
  nov.forEach((v, j) => { if (v > dv + 1e-9) { dv = v; drop = bars[j]; } });
  const sections = [];
  nov.forEach((v, j) => {
    if (Math.abs(v) < 2.5) return;
    for (let k = Math.max(0, j - 2); k <= Math.min(nov.length - 1, j + 2); k++) if (Math.abs(nov[k]) > Math.abs(v)) return;
    if (sections.length && j - sections[sections.length - 1].j < 4) return;
    sections.push({ j, at: bars[j], db: Math.round(v * 10) / 10 });
  });
  let peak = bars.length ? bars[0] : 0, pv = -Infinity;
  for (let j = 0; j < bars.length; j++) {
    const k = Math.min(8, bars.length - j); if (k < Math.min(8, bars.length)) break;
    let s = 0; for (let i = 0; i < k; i++) s += barDb[j + i];
    if (s / k > pv + 1e-9) { pv = s / k; peak = bars[j]; }
  }
  return {
    v: 1, duration: Math.round(duration * 1000) / 1000, bpm: Math.round(6000 / p) / 100, beats, down, phase,
    drop, peak, bars, sections: sections.map(s => ({ at: s.at, db: s.db })), peaks: overview(mono, 1000), fps, loud,
  };
}
// The downbeats of a grid: the phase of four whose beats are loudest.
function downsOf(beats, loud, fps) {
  const at = t => { let m = 0; const f = Math.round(t * fps); for (let k = f - 3; k <= f + 1; k++) if (k >= 0 && k < loud.length && loud[k] > m) m = loud[k]; return m; };
  const score = [0, 0, 0, 0], cnt = [0, 0, 0, 0];
  beats.forEach((t, i) => { score[i % 4] += at(t); cnt[i % 4]++; });
  let phase = 0;
  for (let k = 1; k < 4; k++) if (cnt[k] && score[k] / cnt[k] > score[phase] / Math.max(1, cnt[phase]) + 1e-9) phase = k;
  const down = [];
  for (let i = phase; i < beats.length; i += 4) down.push(i);
  return { phase, down };
}
// The same song at twice (factor 2) or half (0.5) the tempo, for when the analysis took the other one: halfway beats
// added, or every other beat (the louder ones) kept; the downbeats found again. The drop and the sections stay.
function retempo(an, factor) {
  let beats;
  if (factor === 2) {
    beats = [];
    an.beats.forEach((t, i) => { beats.push(t); if (i + 1 < an.beats.length) beats.push(Math.round((t + an.beats[i + 1]) / 2 * 1000) / 1000); });
  } else {
    const loudAt = t => { let m = 0; const f = Math.round(t * an.fps); for (let k = f - 3; k <= f + 1; k++) if (k >= 0 && k < an.loud.length && an.loud[k] > m) m = an.loud[k]; return m; };
    let even = 0, odd = 0;
    an.beats.forEach((t, i) => { if (i % 2) odd += loudAt(t); else even += loudAt(t); });
    beats = an.beats.filter((t, i) => i % 2 === (odd > even ? 1 : 0));
  }
  const { phase, down } = an.loud ? downsOf(beats, an.loud, an.fps) : { phase: 0, down: beats.map((t, i) => i).filter(i => i % 4 === 0) };
  const bpm = Math.round(an.bpm * factor * 100) / 100;
  return Object.assign({}, an, { beats, down, phase, bpm, bars: down.map(i => beats[i]) });
}
function overview(mono, bins) {
  const out = new Float32Array(bins), step = mono.length / bins;
  for (let b = 0; b < bins; b++) {
    let m = 0;
    for (let i = Math.floor(b * step), e = Math.min(mono.length, Math.floor((b + 1) * step)); i < e; i++) { const v = Math.abs(mono[i]); if (v > m) m = v; }
    out[b] = m;
  }
  return out;
}
// the edit's beats for the part of the song from start to end: times from start, the downbeats' indices, the drop
function beatsFor(an, start, end) {
  const times = [], down = [], isDown = new Set(an.down);
  an.beats.forEach((t, i) => {
    if (t < start - 0.0005 || t >= end) return;
    if (isDown.has(i)) down.push(times.length);
    times.push(Math.round((t - start) * 1000) / 1000);
  });
  const drop = an.drop != null && an.drop > start && an.drop < end ? Math.round((an.drop - start) * 1000) / 1000 : null;
  return { bpm: an.bpm, times, down, drop };
}
// beats without music: a steady grid
function gridBeats(bpm, duration) {
  const p = 60 / bpm, times = [], down = [];
  for (let i = 0; i * p < duration - 0.001; i++) { if (i % 4 === 0) down.push(i); times.push(Math.round(i * p * 1000) / 1000); }
  return { bpm, times, down, drop: null };
}
// The part of a song for a video of dur seconds: from a downbeat, with the drop (else the loudest eight bars) a
// quarter of the way in (at most four bars after the start), inside the song.
function pickSegment(an, dur) {
  const total = an.duration;
  if (total <= dur + 0.05) return { start: 0, end: Math.round(total * 1000) / 1000 };
  const downs = an.down.map(i => an.beats[i]), bar = 240 / an.bpm, key = an.drop != null ? an.drop : an.peak;
  let want = key != null ? key - Math.min(dur * 0.25, 4 * bar) : 0;
  want = Math.max(0, Math.min(total - dur, want));
  let start = 0;
  for (const t of downs) if (t <= want + 0.001) start = t;
  return { start, end: Math.round((start + dur) * 1000) / 1000 };
}
// the downbeat nearest a time (for dragging the part of the song), kept inside the song
function snapDown(an, t, dur) {
  let best = 0, bd = Infinity;
  for (const i of an.down) {
    const b = an.beats[i];
    if (b + dur > an.duration + 0.05) break;
    if (Math.abs(b - t) < bd) { bd = Math.abs(b - t); best = b; }
  }
  return best;
}

// ---------- the edit, as it is drawn ----------
const STYLES = {
  fresh: { name: "日系清新", bg: { kind: "gradient", colors: ["#fdf2f8", "#e0f2fe"], angle: 135 }, accent: "#ec4899", font: "body", ink: "#1f2937",
    cap: { color: "#1f2937", box: "rgba(255,255,255,0.85)", anim: "slide_up" }, trans: ["fade", "slide"], bpm: 100 },
  cute: { name: "可爱", bg: { kind: "gradient", colors: ["#ffe4f1", "#e9d5ff"], angle: 135 }, accent: "#f472b6", font: "title", round: true, sparkles: true, ink: "#831843",
    cap: { color: "#ffffff", box: "#ec4899", anim: "pop" }, trans: ["zoom"], bpm: 110 },
  cool: { name: "酷炫", bg: { kind: "solid", colors: ["#0b0b10"] }, accent: "#22d3ee", font: "title", dark: true, ink: "#ffffff",
    cap: { color: "#ffffff", bold: true, outline: 3, anim: "zoom" }, trans: ["cut", "zoom", "whip", "flash"], bpm: 128 },
  cyber: { name: "赛博", bg: { kind: "gradient", colors: ["#0f0c29", "#302b63", "#24243e"], angle: 135 }, accent: "#f0abfc", font: "mono", dark: true, ink: "#e0f2fe",
    cap: { color: "#e0f2fe", mono: true, glow: "#f0abfc", anim: "type" }, trans: ["slide", "flash", "whip"], bpm: 128 },
  outfits: { name: "换装卡点", bg: { kind: "solid", colors: ["#111318"] }, accent: "#fbbf24", font: "title", dark: true, ink: "#ffffff",
    cap: { color: "#ffffff", bold: true, big: true, anim: "pop" }, trans: ["cut", "flash"], bpm: 128 },
  product: { name: "商品展示", bg: { kind: "gradient", colors: ["#f8fafc", "#e2e8f0"], angle: 135 }, accent: "#2563eb", font: "body", ink: "#0f172a",
    cap: { color: "#0f172a", anim: "fade" }, trans: ["fade"], bpm: 96 },
};
const LAYOUTS = ["full", "split2", "split3", "grid4", "pip", "card", "polaroid", "title", "credits", "end"];
const TRANSITIONS = ["cut", "fade", "flash", "zoom", "slide", "whip"];
const ANIMS = ["none", "fade", "pop", "slide_up", "slide_left", "zoom", "type"];
const SHAPES = ["sparkle", "frame", "bar", "circle", "heart", "star"];
const POS = ["auto", "top", "bottom", "left", "right", "center"];
const TR = 0.25, INOUT = 0.3;

// the cells of a layout: [x, y, w, h (parts of the frame), turn (degrees)] and whether it is a background
function cells(layout, w, h) {
  const P = h > w, t = 1 / 3;
  switch (layout) {
    case "split2": return P ? [[0, 0, 1, .5], [0, .5, 1, .5]] : [[0, 0, .5, 1], [.5, 0, .5, 1]];
    case "split3": return P ? [[0, 0, 1, t], [0, t, 1, t], [0, 2 * t, 1, t]] : [[0, 0, t, 1], [t, 0, t, 1], [2 * t, 0, t, 1]];
    case "grid4": return [[0, 0, .5, .5], [.5, 0, .5, .5], [0, .5, .5, .5], [.5, .5, .5, .5]];
    case "pip": return [[0, 0, 1, 1, 0, 1], P ? [.52, .70, .44, .25] : [.64, .62, .32, .32]];
    case "card": return P ? [[.08, .08, .84, .52], [.08, .64, .84, .28]] : [[.07, .12, .43, .76], [.54, .18, .40, .64]];
    case "polaroid": return P ? [[.10, .06, .62, .36, -4], [.28, .32, .62, .36, 3], [.12, .58, .62, .36, -2]]
      : [[.08, .16, .30, .62, -6], [.36, .12, .30, .62, 3], [.63, .18, .30, .62, -2]];
    case "title": return [[0, 0, 1, 1, 0, 1], [.08, .34, .84, .32]];
    case "credits": case "end": return [[0, 0, 1, 1, 0, 1]];
    default: return [[0, 0, 1, 1, 0, 1]];
  }
}
// a cell in pixels: inset by 1.2% of the short side (the gaps between pictures), except full and the backgrounds
function cellRect(layout, slot, w, h) {
  const cs = cells(layout, w, h), c = cs[Math.max(0, Math.min(cs.length - 1, slot | 0))];
  const bgCell = layout === "full" || c[5] === 1, ins = bgCell ? 0 : 0.012 * Math.min(w, h);
  return { x: c[0] * w + ins, y: c[1] * h + ins, w: c[2] * w - 2 * ins, h: c[3] * h - 2 * ins, rot: c[4] || 0, bg: bgCell };
}
const num = (v, d) => typeof v === "number" && isFinite(v) ? v : d;
const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
const kindOf = src => String(src || "").split(":")[0];
const isPic = src => ["shot", "spin", "cover", "booth", "icon", "photo"].includes(kindOf(src));

// The edit as the renderer uses it, from what the normaliser sent (or what the page holds before it answers):
// defaults filled in, beats turned into seconds, the scenes back to back. The normaliser (Go) is what decides; this
// only keeps a frame drawable while the player edits.
function prep(e0) {
  const e = JSON.parse(JSON.stringify(e0 || {}));
  e.w = Math.round(clamp(num(e.w, 1920), 360, 3840) / 2) * 2; e.h = Math.round(clamp(num(e.h, 1080), 360, 3840) / 2) * 2;
  e.fps = [24, 25, 30, 50, 60].includes(e.fps) ? e.fps : 30;
  if (!STYLES[e.style]) e.style = "fresh";
  if (!["zh", "ja", "en"].includes(e.lang)) e.lang = "zh";
  const st = STYLES[e.style];
  if (!e.bg || !["solid", "gradient", "shot"].includes(e.bg.kind)) e.bg = st.bg;
  const bt = e.beats && Array.isArray(e.beats.times) && e.beats.times.length ? e.beats : null;
  const beatT = b => {
    if (!bt) return b * 60 / ((e.beats && e.beats.bpm) || st.bpm);
    const n = bt.times.length, p = 60 / (bt.bpm || 120);
    if (b <= 0) return bt.times[0] + b * p;
    if (b < n) { const i = Math.floor(b), f = b - i; return i + 1 < n ? bt.times[i] * (1 - f) + bt.times[i + 1] * f : bt.times[i] + f * p; }
    return bt.times[n - 1] + (b - n + 1) * p;
  };
  e.beatT = beatT;
  const scenes = (Array.isArray(e.scenes) ? e.scenes : []).map((s, i) => {
    s = Object.assign({}, s);
    s.id = String(s.id || "s" + (i + 1));
    if (typeof s.at !== "number") s.at = typeof s.at_beat === "number" ? beatT(s.at_beat) : 0;
    if (typeof s.len !== "number") s.len = typeof s.len_beats === "number" && typeof s.at_beat === "number" ? beatT(s.at_beat + s.len_beats) - beatT(s.at_beat) : 2;
    if (!LAYOUTS.includes(s.layout)) s.layout = "full";
    if (!TRANSITIONS.includes(s.transition)) s.transition = "cut";
    s.layers = (Array.isArray(s.layers) ? s.layers : []).slice(0, 12).map((l, k) => Object.assign({ z: 0 }, l, { k }))
      .filter(l => l && (isPic(l.src) || l.src === "text" || kindOf(l.src) === "shape"))
      .sort((a, b) => num(a.z, 0) - num(b.z, 0) || a.k - b.k);
    return s;
  }).sort((a, b) => a.at - b.at);
  let dur = num(e.duration, 0);
  if (!(dur > 0)) dur = scenes.length ? scenes[scenes.length - 1].at + scenes[scenes.length - 1].len : 10;
  if (scenes.length) scenes[0].at = Math.min(scenes[0].at, 0);
  scenes.forEach((s, i) => { s.at = Math.max(0, s.at); s.len = Math.max(1 / e.fps, (i + 1 < scenes.length ? scenes[i + 1].at : dur) - s.at); });
  e.duration = dur; e.scenes = scenes;
  e.captions = (Array.isArray(e.captions) ? e.captions : []).map(c => {
    c = Object.assign({}, c);
    if (typeof c.at !== "number") c.at = typeof c.at_beat === "number" ? beatT(c.at_beat) : 0;
    if (typeof c.len !== "number") c.len = typeof c.len_beats === "number" && typeof c.at_beat === "number" ? beatT(c.at_beat + c.len_beats) - beatT(c.at_beat) : 2;
    if (!POS.includes(c.pos)) c.pos = "auto";
    c.text = String(c.text || "");
    return c;
  }).filter(c => c.text && c.len > 0 && c.at < dur).slice(0, 40);
  e.credits = (Array.isArray(e.credits) ? e.credits : []).slice(0, 12);
  e.end = e.end || {};
  e.mark = e.mark !== false;
  return e;
}
function sceneIndex(E, t) {
  const s = E.scenes;
  for (let i = s.length - 1; i >= 0; i--) if (t >= s[i].at - 1e-9) return i;
  return 0;
}

// ---------- drawing: animations ----------
const EASE = {
  linear: p => p, in: p => p * p, out: p => 1 - (1 - p) * (1 - p),
  inout: p => p < 0.5 ? 2 * p * p : 1 - Math.pow(-2 * p + 2, 2) / 2,
};
const backOut = p => { const c = 1.70158; return 1 + (c + 1) * Math.pow(p - 1, 3) + c * Math.pow(p - 1, 2); };
// a layer's (or a caption's) look at tl seconds into a stretch of len: alpha, scale, shift (parts of the frame), and
// how much of a text is typed
function animAt(anim, tl, len) {
  anim = anim || {};
  const ease = EASE[anim.ease] || EASE.inout, o = { a: 1, s: 1, dx: 0, dy: 0, type: 1 };
  const one = (kind, p, out) => {
    if (!kind || kind === "none" || p >= 1) return;
    const q = ease(clamp(p, 0, 1)), dir = out ? -1 : 1;
    switch (kind) {
      case "fade": o.a *= q; break;
      case "pop": o.s *= out ? 0.6 + 0.4 * q : 0.6 + 0.4 * backOut(clamp(p, 0, 1)); o.a *= Math.min(1, q * 2); break;
      case "zoom": o.s *= 1.25 - 0.25 * q; o.a *= q; break;
      case "slide_up": o.dy += dir * (1 - q) * 0.08; o.a *= q; break;
      case "slide_left": o.dx += dir * (1 - q) * 0.08; o.a *= q; break;
      case "type": if (out) o.a *= q; else o.type = q; break;
    }
  };
  one(anim.in, tl / INOUT, false);
  one(anim.out, (len - tl) / INOUT, true);
  return o;
}

// ---------- drawing: pictures ----------
// where a picture goes for a cell: fitted (cover or contain) at scale s with its point (cx, cy) at the cell's centre;
// a covering picture is kept over the whole cell
function place(iw, ih, r, fit, cx, cy, s) {
  const base = fit === "contain" ? Math.min(r.w / iw, r.h / ih) : Math.max(r.w / iw, r.h / ih);
  const k = base * s, dw = iw * k, dh = ih * k;
  let x = r.x + r.w / 2 - cx * dw, y = r.y + r.h / 2 - cy * dh;
  if (fit !== "contain" || s > 1) {
    if (dw >= r.w) x = clamp(x, r.x + r.w - dw, r.x); else x = r.x + (r.w - dw) / 2;
    if (dh >= r.h) y = clamp(y, r.y + r.h - dh, r.y); else y = r.y + (r.h - dh) / 2;
  }
  return { x, y, w: dw, h: dh };
}
// a box of a source in parts of the picture (the backend may send pixels)
function partBox(b, res) {
  if (!Array.isArray(b) || b.length < 4) return null;
  const px = b.some(v => v > 1.5);
  const W = px ? res.w || 1 : 1, H = px ? res.h || 1 : 1;
  return [b[0] / W, b[1] / H, b[2] / W, b[3] / H];
}
// a picture layer's centre and scale at progress q: its Ken Burns, else the avatar (a little up toward the face)
// a picture's fit: the edit's, else cover — contain for cut-out pictures and icons (as the normaliser fills it)
function fitOf(lay, res) { return lay.fit === "contain" || lay.fit === "cover" ? lay.fit : res && res.alpha || kindOf(lay.src) === "icon" ? "contain" : "cover"; }
function focusOf(lay, res, q) {
  const kb = lay.anim && lay.anim.kb;
  if (kb && Array.isArray(kb.from) && Array.isArray(kb.to)) {
    const f = (i, d) => num(kb.from[i], d) + (num(kb.to[i], d) - num(kb.from[i], d)) * q;
    return [clamp(f(0, .5), 0, 1), clamp(f(1, .5), 0, 1), clamp(f(2, 1), 0.2, 5)];
  }
  if (fitOf(lay, res) === "contain") return [0.5, 0.5, 1];
  const b = partBox(res.box, res), fc = partBox(res.face, res);
  if (!b) return [0.5, 0.5, 1];
  const cx = b[0] + b[2] / 2, cy = fc ? (b[1] + b[3] / 2) * 0.5 + (fc[1] + fc[3] / 2) * 0.5 : b[1] + b[3] * 0.45;
  return [clamp(cx, 0, 1), clamp(cy, 0, 1), 1];
}
function frameOf(res, q) {
  const fr = res.frames;
  if (fr && fr.length > 1) return fr[Math.min(fr.length - 1, Math.floor(clamp(q, 0, 1) * fr.length))];
  return res.img || (fr && fr[0]) || null;
}
function rrect(ctx, x, y, w, h, r) {
  r = Math.max(0, Math.min(r, w / 2, h / 2));
  ctx.beginPath();
  ctx.moveTo(x + r, y); ctx.lineTo(x + w - r, y); ctx.quadraticCurveTo(x + w, y, x + w, y + r);
  ctx.lineTo(x + w, y + h - r); ctx.quadraticCurveTo(x + w, y + h, x + w - r, y + h);
  ctx.lineTo(x + r, y + h); ctx.quadraticCurveTo(x, y + h, x, y + h - r);
  ctx.lineTo(x, y + r); ctx.quadraticCurveTo(x, y, x + r, y); ctx.closePath();
}

// ---------- drawing: text ----------
const FONT = {
  zh: '"HarmonyOS Sans SC", "MiSans", "Microsoft YaHei UI", "Microsoft YaHei", "PingFang SC", "Noto Sans CJK SC", sans-serif',
  ja: '"Yu Gothic UI", "Yu Gothic", "Meiryo UI", "Meiryo", "Hiragino Sans", "Noto Sans CJK JP", sans-serif',
  en: '"Segoe UI", "Helvetica Neue", Arial, "Microsoft YaHei UI", "Noto Sans CJK SC", sans-serif',
  mono: '"Cascadia Mono", Consolas, "JetBrains Mono", Menlo, monospace',
  round: '"M PLUS Rounded 1c", "Varela Round", "Arial Rounded MT Bold"',
};
function fontCss(kind, px, lang, st) {
  const base = FONT[lang] || FONT.zh;
  if (kind === "mono") return `500 ${px}px ${FONT.mono}, ${base}`;
  if (kind === "title") return `800 ${px}px ${st && st.round ? FONT.round + ", " : ""}${base}`;
  return `500 ${px}px ${base}`;
}
// "今天的**新衣服**" → runs [{s, em}]
function runsOf(text) {
  const out = [], parts = String(text || "").split("**");
  parts.forEach((p, i) => { if (p) out.push({ s: p, em: i % 2 === 1 }); });
  return out;
}
const CJK = /[⺀-鿿가-힯豈-﫿＀-￯　-〿]/;
const NOSTART = /^[、。，．,.!！?？：:；;）)」』】〕》〉ー～…%％]/;
// Lines of runs no wider than maxW: CJK may break between any two characters, other text between words; a closing
// mark never starts a line. Explicit line breaks are kept.
function wrap(ctx, text, font, maxW) {
  ctx.font = font;
  const toks = [];
  for (const r of runsOf(text)) {
    for (const piece of r.s.split(/(\n)/)) {
      if (piece === "\n") { toks.push({ nl: true }); continue; }
      let word = "";
      for (const ch of Array.from(piece)) {
        if (CJK.test(ch)) { if (word) { toks.push({ s: word, em: r.em }); word = ""; } toks.push({ s: ch, em: r.em }); }
        else if (ch === " ") { if (word) toks.push({ s: word, em: r.em }); toks.push({ s: " ", em: r.em, sp: true }); word = ""; }
        else word += ch;
      }
      if (word) toks.push({ s: word, em: r.em });
    }
  }
  const lines = [];
  let cur = [], cw = 0;
  const push = () => { while (cur.length && cur[cur.length - 1].sp) { cw -= cur[cur.length - 1].w; cur.pop(); } lines.push({ toks: cur, w: Math.max(0, cw) }); cur = []; cw = 0; };
  for (const t of toks) {
    if (t.nl) { push(); continue; }
    t.w = ctx.measureText(t.s).width;
    if (t.sp && !cur.length) continue;
    if (cw + t.w > maxW && cur.length && !NOSTART.test(t.s)) push();
    if (t.sp && !cur.length) continue;
    cur.push(t); cw += t.w;
  }
  if (cur.length || !lines.length) push();
  return lines;
}
// lines into rect r: font size px (shrunk until they fit), aligned; returns the lines placed
function setText(ctx, text, kind, px, lang, st, r, align) {
  let font, lines, lh;
  for (let k = 0; k < 12; k++) {
    font = fontCss(kind, px, lang, st); lines = wrap(ctx, text, font, r.w); lh = px * 1.28;
    if (lines.length * lh <= r.h * 1.02 && lines.every(l => l.w <= r.w * 1.001) || px < 8) break;
    px *= 0.9;
  }
  const total = lines.length * lh, y0 = r.y + (r.h - total) / 2;
  return { font, px, lh, lines: lines.map((l, i) => ({ toks: l.toks, w: l.w, x: align === "left" ? r.x : align === "right" ? r.x + r.w - l.w : r.x + (r.w - l.w) / 2, y: y0 + i * lh + lh / 2 })) };
}
// draws set text: each run in its colour (emphasis in the accent), as much as `type` shows
function paintText(ctx, set, color, accent, look, type) {
  ctx.font = set.font; ctx.textBaseline = "middle"; ctx.textAlign = "left";
  let total = 0;
  for (const l of set.lines) for (const t of l.toks) total += Array.from(t.s).length;
  let left = type < 1 ? Math.ceil(total * type) : Infinity;
  for (const l of set.lines) {
    // the tokens of a line in spans of one colour, each drawn at once
    const spans = [];
    for (const t of l.toks) {
      const last = spans[spans.length - 1];
      if (last && last.em === t.em) { last.s += t.s; last.w += t.w; } else spans.push({ s: t.s, em: t.em, w: t.w });
    }
    let x = l.x;
    for (const sp of spans) {
      if (left <= 0) return;
      let s = sp.s;
      const chars = Array.from(s);
      if (chars.length > left) s = chars.slice(0, left).join("");
      left -= chars.length;
      if (look && look.outline) { ctx.lineWidth = look.outline; ctx.lineJoin = "round"; ctx.strokeStyle = "#000000"; ctx.strokeText(s, x, l.y); }
      ctx.fillStyle = sp.em ? accent : color;
      ctx.fillText(s, x, l.y);
      x += sp.w;
    }
  }
}

// ---------- drawing: the frame ----------
function paintBg(ctx, R, bg) {
  const w = R.w, h = R.h;
  if (bg.kind === "shot" && bg.src && R.res[bg.src] && frameOf(R.res[bg.src], 0)) {
    const res = R.res[bg.src], key = "bg|" + bg.src + "|" + w + "x" + h;
    let c = R.cache && R.cache.get(key);
    if (!c && R.canvas) {
      c = R.canvas(w, h); const x = c.getContext("2d");
      x.fillStyle = "#000000"; x.fillRect(0, 0, w, h);
      x.filter = `blur(${Math.round(0.03 * Math.min(w, h))}px) brightness(0.72)`;
      const p = place(res.w, res.h, { x: -0.05 * w, y: -0.05 * h, w: 1.1 * w, h: 1.1 * h }, "cover", 0.5, 0.5, 1);
      x.drawImage(frameOf(res, 0), p.x, p.y, p.w, p.h); x.filter = "none";
      if (R.cache) R.cache.set(key, c);
    }
    if (c) { ctx.drawImage(c, 0, 0, w, h); return; }
  }
  const cols = Array.isArray(bg.colors) && bg.colors.length ? bg.colors : ["#111318"];
  if (bg.kind !== "gradient" || cols.length < 2) { ctx.fillStyle = cols[0]; ctx.fillRect(0, 0, w, h); return; }
  // as CSS's linear-gradient(angle): 0° up, 90° to the right
  const a = num(bg.angle, 135) * Math.PI / 180, dx = Math.sin(a), dy = -Math.cos(a), L = Math.abs(w * dx) + Math.abs(h * dy);
  const g = ctx.createLinearGradient(w / 2 - dx * L / 2, h / 2 - dy * L / 2, w / 2 + dx * L / 2, h / 2 + dy * L / 2);
  cols.slice(0, 3).forEach((c, i, all) => g.addColorStop(i / (all.length - 1), c));
  ctx.fillStyle = g; ctx.fillRect(0, 0, w, h);
}
// a stand-in where a picture is not there (yet)
function paintMissing(ctx, r, rad) {
  ctx.save(); rrect(ctx, r.x, r.y, r.w, r.h, rad); ctx.fillStyle = "rgba(128,128,140,0.22)"; ctx.fill();
  ctx.strokeStyle = "rgba(128,128,140,0.45)"; ctx.lineWidth = Math.max(1, Math.min(r.w, r.h) * 0.01); ctx.stroke(); ctx.restore();
}
function withAnim(ctx, R, r, an, fn) {
  if (an.a <= 0.001) return;
  ctx.save();
  ctx.globalAlpha *= clamp(an.a, 0, 1);
  const cx = r.x + r.w / 2, cy = r.y + r.h / 2;
  ctx.translate(cx + an.dx * R.w, cy + an.dy * R.h);
  if (r.rot) ctx.rotate(r.rot * Math.PI / 180);
  if (an.s !== 1) ctx.scale(an.s, an.s);
  ctx.translate(-cx, -cy);
  fn();
  ctx.restore();
}
function layerRect(R, sc, lay) {
  if (Array.isArray(lay.box) && lay.box.length === 4) {
    const b = lay.box.map(v => clamp(num(v, 0), -1, 2));
    return { x: b[0] * R.w, y: b[1] * R.h, w: b[2] * R.w, h: b[3] * R.h, rot: 0, bg: false, box: true };
  }
  if (lay.src === "text" && sc.layout !== "title" && lay.slot === undefined) return { x: 0.06 * R.w, y: 0.06 * R.h, w: 0.88 * R.w, h: 0.88 * R.h, rot: 0, bg: false };
  return cellRect(sc.layout, num(lay.slot, 0), R.w, R.h);
}
function paintPicture(ctx, R, sc, lay, r, tl) {
  const res = R.res[lay.src], q = clamp(tl / sc.len, 0, 1), short = Math.min(R.w, R.h);
  const rad = r.bg || r.box ? 0 : 0.008 * short, ease = EASE[(lay.anim && lay.anim.ease) || "inout"] || EASE.inout;
  if (sc.layout === "polaroid" && !r.bg && !r.box) { // a white frame, the picture inset 4%
    ctx.save(); ctx.shadowColor = "rgba(0,0,0,0.28)"; ctx.shadowBlur = 0.02 * short; ctx.shadowOffsetY = 0.006 * short;
    ctx.fillStyle = "#ffffff"; ctx.fillRect(r.x, r.y, r.w, r.h); ctx.restore();
    const ins = 0.04 * Math.min(r.w, r.h);
    r = { x: r.x + ins, y: r.y + ins, w: r.w - 2 * ins, h: r.h - 2 * ins };
  } else if (sc.layout === "pip" && !r.bg && !r.box) { // a white border 0.6% of the short side round the style's background
    const b = 0.006 * short;
    ctx.save(); ctx.shadowColor = "rgba(0,0,0,0.3)"; ctx.shadowBlur = 0.015 * short;
    rrect(ctx, r.x - b, r.y - b, r.w + 2 * b, r.h + 2 * b, rad + b); ctx.fillStyle = "#ffffff"; ctx.fill(); ctx.restore();
    ctx.save(); rrect(ctx, r.x, r.y, r.w, r.h, rad); ctx.clip(); paintBg(ctx, R, R.edit.bg); ctx.restore();
  } else if (sc.layout === "card" && !r.bg && !r.box) { // a card under the picture
    const dark = STYLES[R.edit.style].dark;
    ctx.save(); ctx.shadowColor = dark ? "rgba(0,0,0,0.5)" : "rgba(15,23,42,0.16)"; ctx.shadowBlur = 0.03 * short; ctx.shadowOffsetY = 0.008 * short;
    rrect(ctx, r.x, r.y, r.w, r.h, rad); ctx.fillStyle = dark ? "rgba(255,255,255,0.07)" : "rgba(255,255,255,0.72)"; ctx.fill(); ctx.restore();
  }
  const img = res && frameOf(res, kindOf(lay.src) === "spin" ? q : 0);
  if (!img) { paintMissing(ctx, r, rad); return; }
  const [cx, cy, s] = focusOf(lay, res, ease(q)), p = place(res.w || img.width, res.h || img.height, r, fitOf(lay, res), cx, cy, s);
  ctx.save();
  if (rad) rrect(ctx, r.x, r.y, r.w, r.h, rad); else { ctx.beginPath(); ctx.rect(r.x, r.y, r.w, r.h); }
  ctx.clip();
  ctx.drawImage(img, p.x, p.y, p.w, p.h);
  ctx.restore();
}
function paintShape(ctx, R, kind, r, color) {
  const short = Math.min(R.w, R.h), cx = r.x + r.w / 2, cy = r.y + r.h / 2, m = Math.min(r.w, r.h) / 2;
  ctx.fillStyle = color; ctx.strokeStyle = color;
  const star = (pts, inner) => {
    ctx.beginPath();
    for (let i = 0; i < pts * 2; i++) {
      const a = -Math.PI / 2 + i * Math.PI / pts, rr = i % 2 ? m * inner : m;
      if (i) ctx.lineTo(cx + Math.cos(a) * rr, cy + Math.sin(a) * rr); else ctx.moveTo(cx + Math.cos(a) * rr, cy + Math.sin(a) * rr);
    }
    ctx.closePath(); ctx.fill();
  };
  switch (kind) {
    case "sparkle": star(4, 0.28); break;
    case "star": star(5, 0.45); break;
    case "frame": ctx.lineWidth = 0.008 * short; ctx.strokeRect(r.x + ctx.lineWidth / 2, r.y + ctx.lineWidth / 2, r.w - ctx.lineWidth, r.h - ctx.lineWidth); break;
    case "bar": ctx.save(); ctx.globalAlpha *= 0.88; rrect(ctx, r.x, r.y, r.w, r.h, Math.min(r.h / 2, 0.01 * short)); ctx.fill(); ctx.restore(); break;
    case "circle": ctx.beginPath(); ctx.ellipse(cx, cy, r.w / 2, r.h / 2, 0, 0, Math.PI * 2); ctx.fill(); break;
    case "heart": {
      const s = m * 1.9, x = cx - s / 2, y = cy - s * 0.42;
      ctx.beginPath(); ctx.moveTo(cx, y + s * 0.3);
      ctx.bezierCurveTo(cx, y, x, y, x, y + s * 0.32); ctx.bezierCurveTo(x, y + s * 0.6, cx, y + s * 0.74, cx, y + s * 0.92);
      ctx.bezierCurveTo(cx, y + s * 0.74, x + s, y + s * 0.6, x + s, y + s * 0.32); ctx.bezierCurveTo(x + s, y, cx, y, cx, y + s * 0.3);
      ctx.closePath(); ctx.fill(); break;
    }
  }
}
function paintTextLayer(ctx, R, sc, lay, r, an) {
  const E = R.edit, st = STYLES[E.style], kind = ["title", "body", "mono"].includes(lay.font) ? lay.font : st.font;
  const px = clamp(num(lay.size, sc.layout === "title" ? 0.09 : 0.06), 0.02, 0.2) * R.h;
  const align = ["left", "center", "right"].includes(lay.align) ? lay.align : "center";
  const set = setText(ctx, String(lay.text || "").slice(0, 80), kind, px, E.lang, st, r, align);
  const color = typeof lay.color === "string" && lay.color ? lay.color : st.ink;
  ctx.save();
  if (E.style === "cyber") { ctx.shadowColor = st.accent; ctx.shadowBlur = set.px * 0.35; }
  else { ctx.shadowColor = st.dark || isLight(color) ? "rgba(0,0,0,0.45)" : "rgba(255,255,255,0.6)"; ctx.shadowBlur = set.px * 0.18; ctx.shadowOffsetY = set.px * 0.03; }
  paintText(ctx, set, color, st.accent, E.style === "cool" ? { outline: 3 * R.h / 1080 } : null, an.type);
  ctx.restore();
}
function isLight(c) {
  const m = /^#?([0-9a-f]{6})$/i.exec(String(c || "").trim());
  if (!m) return false;
  const n = parseInt(m[1], 16);
  return ((n >> 16) * 299 + ((n >> 8) & 255) * 587 + (n & 255) * 114) / 1000 > 150;
}
// credits: rows of picture + name + shop, six to a column (two columns when landscape); more than fit take turns
function paintCredits(ctx, R, sc, tl) {
  const E = R.edit, st = STYLES[E.style], list = E.credits || [], P = R.h > R.w;
  if (!list.length) return;
  const per = P ? 6 : 12, pages = Math.ceil(list.length / per), page = Math.min(pages - 1, Math.floor(clamp(tl / sc.len, 0, 0.9999) * pages));
  const items = list.slice(page * per, page * per + per), cols = P ? 1 : 2, short = Math.min(R.w, R.h);
  const area = P ? { x: 0.1 * R.w, y: 0.14 * R.h, w: 0.8 * R.w, h: 0.72 * R.h } : { x: 0.08 * R.w, y: 0.12 * R.h, w: 0.84 * R.w, h: 0.76 * R.h };
  const rowH = area.h / 6, colW = area.w / cols, rows = Math.min(6, Math.ceil(items.length / cols));
  const y0 = area.y + (area.h - rows * rowH) / 2;
  const pageT = sc.len / pages, pl = tl - page * pageT;
  items.forEach((c, i) => {
    const col = cols === 2 && i >= Math.ceil(items.length / 2) ? 1 : 0, row = col ? i - Math.ceil(items.length / 2) : i;
    const x = area.x + col * colW, y = y0 + row * rowH, pic = rowH * 0.8, an = animAt({ in: "slide_up", out: pages > 1 && page < pages - 1 ? "fade" : "none" }, pl - i * 0.06, pageT);
    withAnim(ctx, R, { x, y, w: colW, h: rowH }, an, () => {
      const pr = { x: x, y: y + (rowH - pic) / 2, w: pic, h: pic }, res = c.src && R.res[c.src], img = res && frameOf(res, 0);
      if (img) {
        const p = place(res.w || img.width, res.h || img.height, pr, "cover", 0.5, 0.5, 1);
        ctx.save(); rrect(ctx, pr.x, pr.y, pr.w, pr.h, 0.012 * short); ctx.clip(); ctx.drawImage(img, p.x, p.y, p.w, p.h); ctx.restore();
      } else paintMissing(ctx, pr, 0.012 * short);
      const tr = { x: x + pic + 0.025 * colW, y: pr.y, w: colW - pic - 0.06 * colW, h: pic };
      const nm = setText(ctx, String(c.name || "").replace(/\*\*/g, ""), st.font === "mono" ? "mono" : "title", rowH * 0.3, E.lang, st, { x: tr.x, y: tr.y, w: tr.w, h: pic * 0.58 }, "left");
      paintText(ctx, nm, st.ink, st.accent, null, 1);
      if (c.shop) {
        const sh = setText(ctx, String(c.shop).replace(/\*\*/g, ""), st.font === "mono" ? "mono" : "body", rowH * 0.21, E.lang, st, { x: tr.x, y: tr.y + pic * 0.56, w: tr.w, h: pic * 0.42 }, "left");
        ctx.save(); ctx.globalAlpha *= 0.72; paintText(ctx, sh, st.ink, st.accent, null, 1); ctx.restore();
      }
    });
  });
}
// the end card: creator, title, links; the QR code at the right (landscape) or the bottom (portrait)
function paintEnd(ctx, R, sc, tl) {
  const E = R.edit, st = STYLES[E.style], end = E.end || {}, P = R.h > R.w, short = Math.min(R.w, R.h);
  const qm = end.qr && R.qr ? R.qr(String(end.qr)) : null;
  const textBox = qm ? (P ? { x: 0.08 * R.w, y: 0.1 * R.h, w: 0.84 * R.w, h: 0.42 * R.h } : { x: 0.08 * R.w, y: 0.16 * R.h, w: 0.5 * R.w, h: 0.68 * R.h })
    : { x: 0.1 * R.w, y: 0.18 * R.h, w: 0.8 * R.w, h: 0.64 * R.h };
  const align = qm && !P ? "left" : "center", links = (Array.isArray(end.links) ? end.links : []).slice(0, 4).map(String);
  const parts = [];
  if (end.creator) parts.push({ s: String(end.creator), kind: st.font === "mono" ? "mono" : "title", px: 0.1, alpha: 1 });
  if (end.title) parts.push({ s: String(end.title), kind: st.font === "mono" ? "mono" : "body", px: 0.055, alpha: 0.92 });
  links.forEach(l => parts.push({ s: l, kind: "body", px: 0.034, alpha: 0.75, link: true }));
  const sizes = parts.map(p => p.px * R.h * (P ? 0.62 : 1)), gap = 0.022 * R.h, sum = sizes.reduce((a, b) => a + b * 1.3, 0) + gap * Math.max(0, parts.length - 1);
  let y = textBox.y + Math.max(0, (textBox.h - sum) / 2);
  parts.forEach((p, i) => {
    const hgt = sizes[i] * 1.3, r = { x: textBox.x, y, w: textBox.w, h: hgt }, an = animAt({ in: "slide_up", ease: "out" }, tl - i * 0.15, sc.len + 9);
    withAnim(ctx, R, r, an, () => {
      const set = setText(ctx, p.s.replace(/\*\*/g, ""), p.kind, sizes[i], p.link ? "en" : E.lang, st, r, align);
      ctx.save(); ctx.globalAlpha *= p.alpha; paintText(ctx, set, p.link ? st.accent : st.ink, st.accent, null, 1); ctx.restore();
    });
    y += hgt + gap;
  });
  if (qm) {
    const size = P ? 0.42 * R.w : 0.42 * R.h, x = P ? (R.w - size) / 2 : 0.79 * R.w - size / 2, yy = P ? 0.6 * R.h : (R.h - size) / 2;
    const an = animAt({ in: "pop" }, tl - parts.length * 0.15, sc.len + 9);
    withAnim(ctx, R, { x, y: yy, w: size, h: size }, an, () => {
      ctx.save(); ctx.shadowColor = "rgba(0,0,0,0.25)"; ctx.shadowBlur = 0.02 * short; rrect(ctx, x, yy, size, size, 0.03 * size); ctx.fillStyle = "#ffffff"; ctx.fill(); ctx.restore();
      const n = qm.n, cell = size / (n + 8), ox = x + 4 * cell, oy = yy + 4 * cell;
      ctx.fillStyle = "#111111";
      for (let rI = 0; rI < n; rI++) {
        let run = -1;
        for (let c = 0; c <= n; c++) {
          const dark = c < n && qm.dark(rI, c);
          if (dark && run < 0) run = c;
          if (!dark && run >= 0) { ctx.fillRect(ox + run * cell, oy + rI * cell, (c - run) * cell + 0.5, cell + 0.5); run = -1; }
        }
      }
    });
  }
}
// one scene, without the transitions: the background, the layers, what its layout draws itself
function paintScene(ctx, R, sc, t) {
  const tl = clamp(t - sc.at, 0, sc.len);
  paintBg(ctx, R, R.edit.bg);
  for (const lay of sc.layers) {
    const r = layerRect(R, sc, lay), an = animAt(lay.anim, tl, sc.len), kind = kindOf(lay.src);
    if (lay.src === "text") { if (lay.text) withAnim(ctx, R, r, an, () => paintTextLayer(ctx, R, sc, lay, r, an)); continue; }
    if (kind === "shape") { withAnim(ctx, R, r, an, () => paintShape(ctx, R, lay.src.slice(6), r, lay.color || STYLES[R.edit.style].accent)); continue; }
    withAnim(ctx, R, r, an, () => paintPicture(ctx, R, sc, lay, r, tl));
  }
  if (sc.layout === "credits") paintCredits(ctx, R, sc, tl);
  if (sc.layout === "end") paintEnd(ctx, R, sc, tl);
}
// a number from a text, the same each time (where the sparkles of a scene go)
function hash(s) { let h = 2166136261; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 16777619); } return (h >>> 0) / 4294967296; }
function paintSparkles(ctx, R, sc, t) {
  const short = Math.min(R.w, R.h), st = STYLES[R.edit.style];
  for (let k = 0; k < 7; k++) {
    const hx = hash(sc.id + "x" + k), hy = hash(sc.id + "y" + k), hs = hash(sc.id + "s" + k), hp = hash(sc.id + "p" + k);
    const a = 0.35 + 0.65 * Math.max(0, Math.sin(2 * Math.PI * (t * 0.9 + hp)));
    const m = (0.012 + 0.016 * hs) * short, x = (0.04 + 0.92 * hx) * R.w, y = (0.04 + 0.92 * hy) * R.h;
    ctx.save(); ctx.globalAlpha *= a; ctx.shadowColor = "#ffffff"; ctx.shadowBlur = m;
    paintShape(ctx, R, "sparkle", { x: x - m, y: y - m, w: 2 * m, h: 2 * m }, st.accent); ctx.restore();
  }
}

// ---------- drawing: captions ----------
// The main picture of a scene: its first shot or spin layer, and where its avatar and face boxes are in the frame
// (the parts that are in its cell), at time t.
function subjectOf(R, sc, t) {
  const lay = sc.layers.find(l => ["shot", "spin"].includes(kindOf(l.src)) && R.res[l.src]);
  if (!lay) return null;
  const res = R.res[lay.src], r = layerRect(R, sc, lay), q = clamp((t - sc.at) / sc.len, 0, 1);
  const ease = EASE[(lay.anim && lay.anim.ease) || "inout"] || EASE.inout, [cx, cy, s] = focusOf(lay, res, ease(q));
  const p = place(res.w || 1, res.h || 1, r, fitOf(lay, res), cx, cy, s);
  const map = b => {
    b = partBox(b, res); if (!b) return null;
    const x0 = Math.max(r.x, p.x + b[0] * p.w), y0 = Math.max(r.y, p.y + b[1] * p.h);
    const x1 = Math.min(r.x + r.w, p.x + (b[0] + b[2]) * p.w), y1 = Math.min(r.y + r.h, p.y + (b[1] + b[3]) * p.h);
    return x1 > x0 && y1 > y0 ? { x: x0, y: y0, w: x1 - x0, h: y1 - y0 } : null;
  };
  return { box: map(res.box), face: map(res.face) };
}
function capLook(R) {
  const E = R.edit, st = STYLES[E.style], short = Math.min(R.w, R.h);
  const px = (st.cap.big ? 0.075 : 0.058) * short;
  return { st, px, kind: st.cap.mono ? "mono" : st.cap.bold || st.font === "title" ? "title" : "body", pad: st.cap.box ? 0.45 * px : 0 };
}
// the rectangle a caption takes at one of the positions
function capRect(R, set, pos) {
  const P = R.h > R.w, L = capLook(R), wMax = set.w + 2 * L.pad * 1.4, hMax = set.h + 2 * L.pad;
  const mx = 0.05 * R.w, top = (P ? 0.12 : 0.07) * R.h, bottom = (P ? 0.16 : 0.07) * R.h;
  switch (pos) {
    case "top": return { x: (R.w - wMax) / 2, y: top, w: wMax, h: hMax };
    case "left": return { x: mx, y: (R.h - hMax) / 2, w: wMax, h: hMax };
    case "right": return { x: R.w - mx - wMax, y: (R.h - hMax) / 2, w: wMax, h: hMax };
    case "center": return { x: (R.w - wMax) / 2, y: (R.h - hMax) / 2, w: wMax, h: hMax };
    default: return { x: (R.w - wMax) / 2, y: R.h - bottom - hMax, w: wMax, h: hMax };
  }
}
const overlap = (a, b) => !a || !b ? 0 : Math.max(0, Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x)) * Math.max(0, Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y));
// Where an auto caption goes: nothing of the face box and as little of the avatar box as can be, preferring the
// bottom, then the top, then the side with more room (overlaps within 2% of the avatar box count the same).
function autoPos(R, sub, measure) {
  const order = ["bottom", "top"];
  if (sub && sub.box) {
    const leftRoom = sub.box.x, rightRoom = R.w - (sub.box.x + sub.box.w);
    order.push(leftRoom >= rightRoom ? "left" : "right", leftRoom >= rightRoom ? "right" : "left");
  } else return "bottom";
  const area = Math.max(1, sub.box.w * sub.box.h);
  let best = null;
  order.forEach((pos, i) => {
    const r = measure(pos), face = overlap(r, sub.face), av = Math.round(overlap(r, sub.box) / area * 50) / 50;
    const key = [face > 0 ? 1 : 0, face > 0 ? face : 0, av, i];
    if (!best || key[0] < best.key[0] || key[0] === best.key[0] && (key[1] < best.key[1] || key[1] === best.key[1] && (key[2] < best.key[2] || key[2] === best.key[2] && key[3] < best.key[3]))) best = { pos, key };
  });
  return best.pos;
}
// a caption laid out for a position: the lines and their box
function capSet(ctx, R, c, pos) {
  const L = capLook(R), P = R.h > R.w, side = pos === "left" || pos === "right";
  const maxW = (side ? (P ? 0.4 : 0.32) : 0.8) * R.w - 2 * L.pad * 1.4;
  const set = setText(ctx, c.text.slice(0, 60), L.kind, L.px, R.edit.lang, L.st, { x: 0, y: 0, w: maxW, h: L.px * 1.28 * 3 }, side ? "left" : "center");
  const w = Math.max(...set.lines.map(l => l.w), 1), h = set.lines.length * set.lh;
  return { set, w, h };
}
// where a caption is drawn: its position (auto worked out at the middle of its time within the scene it starts in)
function capPlace(ctx, R, c) {
  const pos0 = c.pos || "auto";
  if (pos0 !== "auto") return { pos: pos0, cs: capSet(ctx, R, c, pos0) };
  const key = "cap|" + c.at + "|" + c.len + "|" + c.text + "|" + R.w + "x" + R.h;
  const hit = R.cache && R.cache.get(key);
  if (hit) return hit;
  const E = R.edit, sc = E.scenes[sceneIndex(E, c.at + 1e-6)];
  const mid = Math.min(c.at + c.len / 2, sc.at + sc.len - 1e-3), sub = subjectOf(R, sc, mid);
  const pos = autoPos(R, sub, p => capRect(R, capSet(ctx, R, c, p), p)), out = { pos, cs: capSet(ctx, R, c, pos) };
  if (R.cache && R.cache.size < 2000) R.cache.set(key, out);
  return out;
}
function paintCaptions(ctx, R, t) {
  const E = R.edit, L = capLook(R), st = L.st, taken = [];
  for (const c of E.captions) {
    if (t < c.at || t >= c.at + c.len) continue;
    const { pos, cs } = capPlace(ctx, R, c), r = Object.assign({}, capRect(R, cs, pos));
    // two captions at once in one place: the later one moves on (down from the top, up from elsewhere)
    for (let k = 0; k < 4 && taken.some(o => overlap(o, r) > 0); k++) r.y += (pos === "top" ? 1 : -1) * (r.h + 0.012 * Math.min(R.w, R.h));
    taken.push(r);
    const an = animAt({ in: st.cap.anim, out: "fade", ease: "out" }, t - c.at, c.len);
    withAnim(ctx, R, r, an, () => {
      if (st.cap.box) {
        ctx.save(); ctx.shadowColor = "rgba(0,0,0,0.18)"; ctx.shadowBlur = L.px * 0.3; ctx.shadowOffsetY = L.px * 0.05;
        rrect(ctx, r.x, r.y, r.w, r.h, Math.min(r.h / 2, L.px * 0.9)); ctx.fillStyle = st.cap.box; ctx.fill(); ctx.restore();
      }
      const inner = { x: r.x + L.pad * 1.4, y: r.y + L.pad, w: r.w - 2 * L.pad * 1.4, h: r.h - 2 * L.pad };
      const align = pos === "left" || pos === "right" ? "left" : "center";
      const set = { font: cs.set.font, px: cs.set.px, lh: cs.set.lh, lines: cs.set.lines.map((l, i) => ({ toks: l.toks, w: l.w, x: align === "left" ? inner.x : inner.x + (inner.w - l.w) / 2, y: inner.y + i * cs.set.lh + cs.set.lh / 2 })) };
      ctx.save();
      if (st.cap.glow) { ctx.shadowColor = st.cap.glow; ctx.shadowBlur = L.px * 0.45; }
      else if (!st.cap.box) { ctx.shadowColor = isLight(st.cap.color) ? "rgba(0,0,0,0.55)" : "rgba(255,255,255,0.75)"; ctx.shadowBlur = L.px * 0.22; ctx.shadowOffsetY = L.px * 0.03; }
      paintText(ctx, set, st.cap.color, st.cap.box ? (st.cap.color === "#ffffff" ? "#fff7ae" : st.accent) : st.accent, st.cap.outline ? { outline: st.cap.outline * R.h / 1080 } : null, an.type);
      ctx.restore();
    });
  }
}
function paintMark(ctx, R) {
  const st = STYLES[R.edit.style], short = Math.min(R.w, R.h), px = Math.max(9, 0.019 * short);
  ctx.save(); ctx.font = `600 ${px}px ${FONT.en}`; ctx.textAlign = "right"; ctx.textBaseline = "alphabetic";
  ctx.globalAlpha *= 0.62; ctx.fillStyle = st.dark ? "#ffffff" : "#334155";
  ctx.shadowColor = st.dark ? "rgba(0,0,0,0.5)" : "rgba(255,255,255,0.6)"; ctx.shadowBlur = px * 0.3;
  ctx.fillText("Made with MioVRCA", R.w - 0.025 * R.w, R.h - 0.03 * R.h); ctx.restore();
}

// One frame of the edit at time t (seconds from the start). R: {edit (prepped), w, h, res: {src: {img | frames, w, h,
// box, face}}, qr(text) → {n, dark(r, c)}, canvas(w, h) → a canvas (for transitions and the blurred background),
// cache (a Map), noCaptions, noMark, still (no transition: a picture of a moment)}.
function draw(ctx, t, R) {
  const E = R.edit, fps = E.fps;
  t = clamp(t, 0, Math.max(0, E.duration - 1e-6));
  ctx.save();
  ctx.setTransform(1, 0, 0, 1, 0, 0); ctx.globalAlpha = 1; ctx.globalCompositeOperation = "source-over"; ctx.filter = "none";
  ctx.imageSmoothingEnabled = true; ctx.imageSmoothingQuality = "high";
  if (!E.scenes.length) { paintBg(ctx, R, E.bg); ctx.restore(); return; }
  const i = sceneIndex(E, t), sc = E.scenes[i], tl = t - sc.at, prev = i > 0 ? E.scenes[i - 1] : null;
  const tr = R.still ? "cut" : sc.transition;
  if (["fade", "zoom", "slide", "whip"].includes(tr) && tl < TR && R.canvas) {
    const A = scratch(R, 0), B = scratch(R, 1), ax = A.getContext("2d"), bx = B.getContext("2d"), p = clamp(tl / TR, 0, 1);
    if (prev) paintScene(ax, R, prev, prev.at + prev.len); else { ax.fillStyle = "#000000"; ax.fillRect(0, 0, R.w, R.h); }
    paintScene(bx, R, sc, t);
    const w = R.w, h = R.h;
    if (tr === "fade" || !prev && tr !== "slide") {
      ctx.drawImage(A, 0, 0); ctx.globalAlpha = EASE.inout(p); ctx.drawImage(B, 0, 0); ctx.globalAlpha = 1;
    } else if (tr === "zoom") {
      const q = EASE.out(p), s0 = 1 + 0.08 * q, s1 = 1 + 0.15 * (1 - q);
      ctx.drawImage(A, w / 2 - w * s0 / 2, h / 2 - h * s0 / 2, w * s0, h * s0);
      ctx.globalAlpha = Math.min(1, q * 1.4); ctx.drawImage(B, w / 2 - w * s1 / 2, h / 2 - h * s1 / 2, w * s1, h * s1); ctx.globalAlpha = 1;
    } else { // slide, whip: the new scene comes in from the right; a whip is quicker in the middle and smeared
      const q = tr === "whip" ? EASE.inout(EASE.inout(p)) : EASE.inout(p), copies = tr === "whip" ? 6 : 1, smear = tr === "whip" ? 0.12 * w * Math.sin(Math.PI * p) : 0;
      for (let k = 0; k < copies; k++) {
        const dx = copies > 1 ? (k / (copies - 1) - 0.5) * smear : 0;
        ctx.globalAlpha = 1 / (k + 1);
        ctx.drawImage(A, -q * w + dx, 0); ctx.drawImage(B, (1 - q) * w + dx, 0);
      }
      ctx.globalAlpha = 1;
    }
  } else paintScene(ctx, R, sc, t);
  if (STYLES[E.style].sparkles && !["end", "credits"].includes(sc.layout)) paintSparkles(ctx, R, sc, t);
  if (!R.noCaptions) paintCaptions(ctx, R, t);
  if (tr === "flash") { // white for two frames, then fading out over 0.2 s
    const a = tl < 2 / fps ? 1 : 1 - (tl - 2 / fps) / 0.2;
    if (a > 0) { ctx.globalAlpha = clamp(a, 0, 1); ctx.fillStyle = "#ffffff"; ctx.fillRect(0, 0, R.w, R.h); ctx.globalAlpha = 1; }
  }
  if (E.mark && !R.noMark) paintMark(ctx, R);
  ctx.restore();
}
function scratch(R, k) {
  R._scratch = R._scratch || [];
  let c = R._scratch[k];
  if (!c || c.width !== R.w || c.height !== R.h) c = R._scratch[k] = R.canvas(R.w, R.h);
  const x = c.getContext("2d");
  x.setTransform(1, 0, 0, 1, 0, 0); x.globalAlpha = 1; x.filter = "none"; x.clearRect(0, 0, R.w, R.h);
  return c;
}
// R for an edit at a size (a cover, a thumbnail: the same edit drawn at another size and shape)
function renderer(edit, res, opt) {
  const E = edit && edit.beatT ? edit : prep(edit);
  return Object.assign({ edit: E, w: E.w, h: E.h, res: res || {}, cache: new Map() }, opt || {});
}

// the scene midpoints (for the vision check and the pictures)
function midpoints(E) { return E.scenes.map(s => ({ id: s.id, t: Math.round((s.at + s.len / 2) * 1000) / 1000 })); }

const StudioCore = {
  AN, analyze, retempo, beatsFor, gridBeats, pickSegment, snapDown, overview, onsets, tempoPeriod, trackBeats,
  STYLES, LAYOUTS, TRANSITIONS, ANIMS, SHAPES, POS, TR, INOUT,
  cells, cellRect, prep, sceneIndex, animAt, place, fitOf, focusOf, partBox, wrap, setText, runsOf,
  subjectOf, autoPos, capPlace, capRect, capSet, draw, renderer, midpoints, hash, isLight, kindOf, isPic,
};
if (typeof module === "object" && module.exports) module.exports = StudioCore;
else root.StudioCore = StudioCore;
// as a worker: {id, rate, mono} in, {id, an} (or {id, err}) out
if (typeof WorkerGlobalScope !== "undefined" && root instanceof WorkerGlobalScope) {
  root.onmessage = e => {
    const d = e.data || {};
    try { root.postMessage({ id: d.id, an: analyze(d.mono, d.rate) }); } catch (err) { root.postMessage({ id: d.id, err: String(err && err.message || err) }); }
  };
}
})(typeof globalThis !== "undefined" ? globalThis : this);
