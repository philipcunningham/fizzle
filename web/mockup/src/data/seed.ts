// Canned material for the mockup: a deterministic pseudo-random generator
// seeds voices, an instrument, and a disk that look plausible on screen.

import type { Area, Bank, Doc, Effects, Envelope, Instrument, Loop, Voice } from "./model";
import { clamp } from "./model";

let counter = 0;
export function nextId(prefix: string): string {
  counter += 1;
  return `${prefix}-${counter}`;
}

// Small deterministic PRNG so the mockup renders the same on every load.
function mulberry(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a += 0x6d2b79f5;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function makeEnvelope(rnd: () => number, decay: boolean): Envelope {
  const stages = Array.from({ length: 8 }, (_, i) => ({
    rate: clamp(40 + rnd() * 80, 0, 127),
    level: decay ? clamp(120 * Math.pow(0.6, i) + rnd() * 8, 0, 127) : clamp(rnd() * 127, 0, 127),
  }));
  return { stages, sustainStage: 2, endStage: 5 };
}

function makeLoops(frames: number, rnd: () => number): Loop[] {
  const loops: Loop[] = [];
  for (let i = 0; i < 8; i++) {
    const start = Math.floor(frames * (0.3 + rnd() * 0.2));
    loops.push({
      start,
      end: Math.min(frames - 1, start + Math.floor(frames * 0.25)),
      crossfade: 0,
      time: i === 0 ? 127 : 0,
    });
  }
  return loops;
}

export function makeVoice(name: string, rootKey: number, keyLo: number, keyHi: number, seed: number): Voice {
  const rnd = mulberry(seed);
  const frames = 18000 + Math.floor(rnd() * 90000);
  return {
    id: nextId("v"),
    name,
    rootKey,
    keyLo,
    keyHi,
    tune: 0,
    rate: 18,
    playbackMode: "normal",
    genStart: 0,
    genEnd: frames - 1,
    cutoff: clamp(70 + rnd() * 57, 0, 127),
    resonance: clamp(rnd() * 40, 0, 127),
    dcaEnv: makeEnvelope(rnd, true),
    dcfEnv: makeEnvelope(rnd, false),
    keyFollowDcaLevel: 64,
    keyFollowDcaRate: 64,
    keyFollowDcfLevel: 64,
    keyFollowDcfRate: 64,
    velDcaLevel: clamp(60 + rnd() * 40, 0, 127),
    velDcaRate: 64,
    velDcfLevel: clamp(40 + rnd() * 40, 0, 127),
    velDcfRate: 64,
    velDcq: 0,
    lfoWave: "triangle",
    lfoRate: clamp(30 + rnd() * 40, 0, 127),
    lfoDelay: 0,
    lfoAttack: clamp(rnd() * 30, 0, 127),
    lfoPitch: 0,
    lfoAmp: 0,
    lfoFilter: 0,
    lfoResoDepth: 0,
    loops: makeLoops(frames, rnd),
    frames,
    sizeBytes: frames * 2,
    peakSeed: seed,
  };
}

// makePeaks renders a plausible decaying waveform for wavesurfer: a
// fundamental with a detuned harmonic and a burst of attack noise, so
// zooming in reveals actual cycles rather than uniform fuzz.
export function makePeaks(seed: number, n: number): number[] {
  const rnd = mulberry(seed);
  const peaks: number[] = [];
  const cycles = 40 + Math.floor(rnd() * 40);
  const detune = 0.5 + rnd();
  for (let i = 0; i < n; i++) {
    const t = i / n;
    const envelope = Math.exp(-2.2 * t);
    const fundamental = Math.sin(2 * Math.PI * cycles * t);
    const harmonic = 0.4 * Math.sin(2 * Math.PI * cycles * 2 * detune * t);
    const noise = (rnd() * 2 - 1) * 0.35 * Math.exp(-8 * t);
    peaks.push((fundamental + harmonic + noise) * envelope * 0.7);
  }
  return peaks;
}

// snapToZeroCrossing returns the frame of the sign change nearest to the
// requested frame, searching the rendered peak data outward in both
// directions. Falls back to the raw frame when the signal never crosses.
// The real product gets sample accurate crossings from the core; this
// operates at the mockup's peak resolution.
export function snapToZeroCrossing(peaks: number[], totalFrames: number, frame: number): number {
  const n = peaks.length;
  if (n < 2 || totalFrames < 2) return clamp(frame, 0, totalFrames - 1);
  const at = clamp((frame / totalFrames) * (n - 1), 1, n - 1);
  const crossesAt = (j: number) => j >= 1 && j < n && Math.sign(peaks[j]) !== Math.sign(peaks[j - 1]);
  for (let d = 0; d < n; d++) {
    if (crossesAt(at - d)) return clamp(Math.round(((at - d) / (n - 1)) * totalFrames), 0, totalFrames - 1);
    if (crossesAt(at + d)) return clamp(Math.round(((at + d) / (n - 1)) * totalFrames), 0, totalFrames - 1);
  }
  return clamp(frame, 0, totalFrames - 1);
}

function defaultEffects(): Effects {
  const matrix = Array.from({ length: 3 }, () => Array.from({ length: 7 }, () => 0));
  matrix[0][0] = 32; // mod wheel to LFO pitch, so the grid isn't empty
  return { pitchBendRange: 2, matrix };
}

export function makeArea(voice: Voice): Area {
  return {
    id: nextId("a"),
    voiceId: voice.id,
    keyLo: voice.keyLo,
    keyHi: voice.keyHi,
    velLo: 0,
    velHi: 127,
    output: "mix",
    midiChannel: 1,
    volume: 127,
  };
}

export function makeInstrument(name: string, voiceNames: string[], seed: number): Instrument {
  const span = Math.max(1, Math.floor(61 / Math.max(1, voiceNames.length)));
  const voices = voiceNames.map((n, i) => {
    const lo = clamp(36 + i * span, 0, 127);
    const hi = clamp(lo + span - 1, 0, 127);
    return makeVoice(n, clamp(lo + 2, 0, 127), lo, hi, seed + i * 17);
  });
  const banks: Bank[] = [
    { id: nextId("b"), name: "BANK 1", areas: voices.map(makeArea) },
    { id: nextId("b"), name: "BANK 2", areas: [] },
  ];
  return { id: nextId("i"), name, voices, banks, effects: defaultEffects() };
}

export function seedDisk(): { doc: Doc; instrumentId: string } {
  const inst = makeInstrument("JUNGLISM", ["KICK 1", "SNARE 1", "HAT CL", "HAT OP", "RIDE", "BASS C2", "STAB A", "VOX CHOP"], 7);
  // One voice ships unmapped so the unreferenced marker and the Map
  // action have a target out of the box.
  inst.voices.push(makeVoice("RIM SPARE", 84, 82, 86, 555));
  const files = [
    { id: nextId("f"), name: "FULL-DATA-FZ", type: "full" as const, sizeBytes: inst.voices.reduce((s, v) => s + v.sizeBytes, 4096), instrumentId: inst.id },
    { id: nextId("f"), name: "DRUMS2.fzb", type: "bank" as const, sizeBytes: 96_000 },
    { id: nextId("f"), name: "SUBBASS.fzv", type: "voice" as const, sizeBytes: 44_000 },
  ];
  return {
    doc: {
      disk: { label: "FZ SESSION 1", files },
      instruments: { [inst.id]: inst },
      openInstrumentId: null,
      dirty: false,
    },
    instrumentId: inst.id,
  };
}

export function emptyDoc(): Doc {
  return { disk: null, instruments: {}, openInstrumentId: null, dirty: false };
}
