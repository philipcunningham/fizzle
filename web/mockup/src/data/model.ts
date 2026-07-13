// Canned document model for the Step 0 mockup. Shapes mirror the spec's
// language (section 6): disk, file, instrument, voice, bank, area, effects.
// Nothing here is byte correct; the real model lives in the Go core.

export const IMAGE_SIZE = 1_310_720; // bytes, matches pkg/disk ImageSize
export const MAX_VOICES = 64;
export const MAX_BANKS = 8;
export const MAX_AREAS = 64;

export type RateKHz = 36 | 18 | 9;

export interface EnvStage {
  rate: number; // 0..127
  level: number; // 0..127
}

export interface Envelope {
  stages: EnvStage[]; // 8 stages
  sustainStage: number; // 0..7
  endStage: number; // 0..7
}

export interface Loop {
  start: number; // sample frames
  end: number; // sample frames
  crossfade: number; // 0..127
  time: number; // 0..127
}

export interface Voice {
  id: string;
  name: string;
  rootKey: number; // MIDI 0..127
  keyLo: number;
  keyHi: number;
  tune: number; // -64..63
  rate: RateKHz;
  playbackMode: string;
  genStart: number; // sample frames
  genEnd: number;
  cutoff: number; // 0..127
  resonance: number; // 0..127
  dcaEnv: Envelope;
  dcfEnv: Envelope;
  keyFollowDcaLevel: number;
  keyFollowDcaRate: number;
  keyFollowDcfLevel: number;
  keyFollowDcfRate: number;
  velDcaLevel: number;
  velDcaRate: number;
  velDcfLevel: number;
  velDcfRate: number;
  velDcq: number;
  lfoWave: string;
  lfoRate: number;
  lfoDelay: number;
  lfoAttack: number;
  lfoPitch: number;
  lfoAmp: number;
  lfoFilter: number;
  lfoResoDepth: number;
  loops: Loop[]; // 8
  frames: number; // sample length in frames
  sizeBytes: number;
  peakSeed: number;
}

export interface Area {
  id: string;
  voiceId: string | null;
  keyLo: number;
  keyHi: number;
  velLo: number; // 0..127
  velHi: number;
  output: string;
  midiChannel: number;
  volume: number; // 0..127
}

export interface Bank {
  id: string;
  name: string;
  areas: Area[];
}

export interface Effects {
  pitchBendRange: number; // semitones
  // matrix[controller][target], controllers: mod, foot, aftertouch;
  // targets: LFO pitch, LFO amp, LFO filter, LFO reso, DCA, DCF, DCQ.
  matrix: number[][];
}

export interface Instrument {
  id: string;
  name: string;
  voices: Voice[];
  banks: Bank[];
  effects: Effects;
}

export type FileType = "full" | "bank" | "voice";

export interface DiskFile {
  id: string;
  name: string;
  type: FileType;
  sizeBytes: number;
  instrumentId?: string;
}

export interface Disk {
  label: string;
  files: DiskFile[];
}

export interface Doc {
  disk: Disk | null;
  instruments: Record<string, Instrument>;
  openInstrumentId: string | null;
  dirty: boolean;
}

export const EFFECT_CONTROLLERS = ["Mod wheel", "Foot pedal", "Aftertouch"];
export const EFFECT_TARGETS = [
  "LFO pitch",
  "LFO amp",
  "LFO filter",
  "LFO reso",
  "DCA",
  "DCF",
  "DCQ",
];

export const PLAYBACK_MODES = ["normal", "reverse", "one shot"];
export const LFO_WAVES = ["triangle", "saw up", "saw down", "square", "random"];
export const OUTPUTS = ["mix", "out 1", "out 2", "out 3", "out 4", "out 5", "out 6", "out 7", "out 8"];

const NOTE_NAMES = ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"];

// noteName matches pkg/render NoteName: MIDI 60 renders as C4.
export function noteName(midi: number): string {
  if (midi < 0 || midi > 127) return "?";
  return `${NOTE_NAMES[midi % 12]}${Math.floor(midi / 12) - 1}`;
}

export function noteFrequency(midi: number): number {
  return 440 * Math.pow(2, (midi - 69) / 12);
}

export function formatBytes(n: number): string {
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(2)} MB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${n} B`;
}

export function usedBytes(disk: Disk): number {
  return disk.files.reduce((sum, f) => sum + f.sizeBytes, 0);
}

export function clamp(v: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, Math.round(v)));
}
