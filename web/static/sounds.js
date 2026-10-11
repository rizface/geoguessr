// Every sound in the game is synthesized with the Web Audio API.
// No audio files, nothing to license, nothing to download.

const STORAGE_KEY = "geoguessr.muted";

class SoundEngine {
  constructor() {
    this.ctx = null;
    this.master = null;
    this.noise = null;
    this.muted = readMuted();
  }

  // Browsers only allow audio after a user gesture, so call this from
  // the Start button's click handler.
  unlock() {
    if (!this.ctx) {
      const AC = window.AudioContext || window.webkitAudioContext;
      if (!AC) return;
      this.ctx = new AC();
      this.master = this.ctx.createGain();
      this.master.gain.value = this.muted ? 0 : 0.6;
      // A gentle compressor keeps stacked sounds from clipping.
      const comp = this.ctx.createDynamicsCompressor();
      comp.threshold.value = -12;
      comp.ratio.value = 4;
      this.master.connect(comp).connect(this.ctx.destination);
      this.noise = makeNoiseBuffer(this.ctx);
    }
    if (this.ctx.state === "suspended") this.ctx.resume();
  }

  setMuted(m) {
    this.muted = m;
    try { localStorage.setItem(STORAGE_KEY, m ? "1" : "0"); } catch {}
    if (this.master) {
      this.master.gain.setTargetAtTime(m ? 0 : 0.6, this.ctx.currentTime, 0.02);
    }
  }

  toggleMute() {
    this.setMuted(!this.muted);
    return this.muted;
  }

  get ready() {
    return !!this.ctx && !this.muted;
  }

  // ---- building blocks ----------------------------------------------

  tone({ freq, to, type = "sine", start = 0, dur = 0.15, gain = 0.3, attack = 0.005 }) {
    const t = this.ctx.currentTime + start;
    const osc = this.ctx.createOscillator();
    const g = this.ctx.createGain();
    osc.type = type;
    osc.frequency.setValueAtTime(freq, t);
    if (to) osc.frequency.exponentialRampToValueAtTime(to, t + dur);
    g.gain.setValueAtTime(0.0001, t);
    g.gain.exponentialRampToValueAtTime(gain, t + attack);
    g.gain.exponentialRampToValueAtTime(0.0001, t + dur);
    osc.connect(g).connect(this.master);
    osc.start(t);
    osc.stop(t + dur + 0.02);
  }

  noiseBurst({ start = 0, dur = 0.4, from = 400, to = 2400, q = 1.2, gain = 0.25 }) {
    const t = this.ctx.currentTime + start;
    const src = this.ctx.createBufferSource();
    src.buffer = this.noise;
    const bp = this.ctx.createBiquadFilter();
    bp.type = "bandpass";
    bp.Q.value = q;
    bp.frequency.setValueAtTime(from, t);
    bp.frequency.exponentialRampToValueAtTime(to, t + dur);
    const g = this.ctx.createGain();
    g.gain.setValueAtTime(0.0001, t);
    g.gain.exponentialRampToValueAtTime(gain, t + dur * 0.35);
    g.gain.exponentialRampToValueAtTime(0.0001, t + dur);
    src.connect(bp).connect(g).connect(this.master);
    src.start(t);
    src.stop(t + dur + 0.05);
  }

  // ---- game sounds ---------------------------------------------------

  // Soft click for buttons.
  click() {
    if (!this.ready) return;
    this.tone({ freq: 1800, to: 900, type: "triangle", dur: 0.04, gain: 0.12 });
  }

  // Dropping a pin on the guess map.
  pin() {
    if (!this.ready) return;
    this.tone({ freq: 520, to: 1040, type: "sine", dur: 0.09, gain: 0.35 });
    this.tone({ freq: 2080, type: "sine", start: 0.05, dur: 0.06, gain: 0.08 });
  }

  // Submitting a guess: a whoosh as the line flies to the answer.
  whoosh() {
    if (!this.ready) return;
    this.noiseBurst({ dur: 0.55, from: 300, to: 3200, q: 1.4, gain: 0.3 });
    this.tone({ freq: 180, to: 90, type: "sine", start: 0.05, dur: 0.4, gain: 0.12 });
  }

  // Score reveal: happier and higher the closer you were.
  score(points) {
    if (!this.ready) return;
    const ratio = points / 5000;
    if (ratio < 0.1) {
      // A sad little "womp".
      this.tone({ freq: 220, to: 160, type: "triangle", dur: 0.25, gain: 0.25 });
      this.tone({ freq: 165, to: 110, type: "triangle", start: 0.22, dur: 0.45, gain: 0.25 });
      return;
    }
    // Rising arpeggio: more notes and a brighter key for better scores.
    const scale = [0, 4, 7, 12, 16]; // major chord tones
    const notes = 1 + Math.round(ratio * 4);
    const root = 392 + ratio * 130; // G4 → ~C5
    for (let i = 0; i < notes; i++) {
      const f = root * Math.pow(2, scale[i] / 12);
      this.tone({ freq: f, type: "triangle", start: i * 0.085, dur: 0.22, gain: 0.22 });
    }
  }

  // Bang on (under ~25 m): a sparkly jingle.
  perfect() {
    if (!this.ready) return;
    const melody = [523.25, 659.25, 783.99, 1046.5, 1318.5];
    melody.forEach((f, i) => {
      this.tone({ freq: f, type: "square", start: i * 0.07, dur: 0.18, gain: 0.09 });
      this.tone({ freq: f * 2, type: "sine", start: i * 0.07 + 0.02, dur: 0.12, gain: 0.05 });
    });
    for (let i = 0; i < 6; i++) {
      this.tone({ freq: 2000 + Math.random() * 2500, type: "sine", start: 0.4 + i * 0.05, dur: 0.08, gain: 0.05 });
    }
  }

  // Timer tick for the last seconds; urgent ticks are higher.
  tick(urgent = false) {
    if (!this.ready) return;
    this.tone({ freq: urgent ? 1500 : 1100, type: "square", dur: 0.03, gain: urgent ? 0.1 : 0.06 });
  }

  // Time ran out.
  timeUp() {
    if (!this.ready) return;
    this.tone({ freq: 440, to: 220, type: "sawtooth", dur: 0.5, gain: 0.12 });
    this.tone({ freq: 445, to: 222, type: "sawtooth", dur: 0.5, gain: 0.08 });
  }

  // End of game fanfare; grander for better totals.
  fanfare(ratio) {
    if (!this.ready) return;
    const good = ratio >= 0.5;
    const seq = good
      ? [[392, 0], [392, 0.12], [392, 0.24], [523.25, 0.38], [659.25, 0.62], [783.99, 0.86]]
      : [[392, 0], [349.23, 0.18], [329.63, 0.36], [392, 0.6]];
    seq.forEach(([f, t], i) => {
      const last = i === seq.length - 1;
      this.tone({ freq: f, type: "sawtooth", start: t, dur: last ? 0.9 : 0.16, gain: 0.08 });
      this.tone({ freq: f, type: "triangle", start: t, dur: last ? 0.9 : 0.16, gain: 0.18 });
    });
    if (good) {
      const end = 0.86;
      [783.99 / 2, 783.99 * 1.25].forEach((f) =>
        this.tone({ freq: f, type: "triangle", start: end, dur: 0.9, gain: 0.1 })
      );
      this.noiseBurst({ start: end, dur: 0.8, from: 6000, to: 9000, q: 0.7, gain: 0.05 });
    }
  }
}

function makeNoiseBuffer(ctx) {
  const len = ctx.sampleRate * 1;
  const buf = ctx.createBuffer(1, len, ctx.sampleRate);
  const data = buf.getChannelData(0);
  for (let i = 0; i < len; i++) data[i] = Math.random() * 2 - 1;
  return buf;
}

function readMuted() {
  try { return localStorage.getItem(STORAGE_KEY) === "1"; } catch { return false; }
}

export const sounds = new SoundEngine();
