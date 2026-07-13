// The mockup's stand-in for the core-emitted parameter schema (spec
// section 7, "Generated, not hand written"). The voice editor renders
// controls from these declarations; nothing in the editor hard-codes a
// field. In the real product the Go core emits this.

import type { Voice } from "./model";
import { LFO_WAVES, PLAYBACK_MODES } from "./model";

export type ControlKind = "knob" | "stepper" | "select";

export interface Field {
  id: string;
  label: string;
  group: string;
  kind: ControlKind;
  min?: number;
  max?: number;
  options?: string[];
  get: (v: Voice) => number | string;
  set: (v: Voice, value: number | string) => Voice;
}

function num(
  id: string,
  label: string,
  group: string,
  kind: ControlKind,
  min: number,
  max: number,
  get: (v: Voice) => number,
  set: (v: Voice, value: number) => Voice,
): Field {
  return { id, label, group, kind, min, max, get, set: (v, value) => set(v, Number(value)) };
}

function sel(
  id: string,
  label: string,
  group: string,
  options: string[],
  get: (v: Voice) => string,
  set: (v: Voice, value: string) => Voice,
): Field {
  return { id, label, group, kind: "select", options, get, set: (v, value) => set(v, String(value)) };
}

export const GROUPS = [
  "Identity and mapping",
  "Sample",
  "Filter",
  "Key follow",
  "Velocity sensitivity",
  "LFO",
];

// The DCA/DCF envelopes and loops get bespoke editors; every other
// group is schema-rendered below.
export const FIELDS: Field[] = [
  num("tune", "tune", "Identity and mapping", "knob", -64, 63, (v) => v.tune, (v, x) => ({ ...v, tune: x })),
  num("rootKey", "root key", "Identity and mapping", "stepper", 0, 127, (v) => v.rootKey, (v, x) => ({ ...v, rootKey: x })),
  num("keyLo", "key low", "Identity and mapping", "stepper", 0, 127, (v) => v.keyLo, (v, x) => ({ ...v, keyLo: x })),
  num("keyHi", "key high", "Identity and mapping", "stepper", 0, 127, (v) => v.keyHi, (v, x) => ({ ...v, keyHi: x })),

  sel("rate", "sample rate", "Sample", ["36", "18", "9"], (v) => String(v.rate), (v, x) => ({ ...v, rate: Number(x) as Voice["rate"] })),
  sel("playbackMode", "playback", "Sample", PLAYBACK_MODES, (v) => v.playbackMode, (v, x) => ({ ...v, playbackMode: x })),
  num("genStart", "gen start", "Sample", "stepper", 0, 999_999, (v) => v.genStart, (v, x) => ({ ...v, genStart: x })),
  num("genEnd", "gen end", "Sample", "stepper", 0, 999_999, (v) => v.genEnd, (v, x) => ({ ...v, genEnd: x })),

  num("cutoff", "DCF cutoff", "Filter", "knob", 0, 127, (v) => v.cutoff, (v, x) => ({ ...v, cutoff: x })),
  num("resonance", "DCQ reso", "Filter", "knob", 0, 127, (v) => v.resonance, (v, x) => ({ ...v, resonance: x })),

  num("kfDcaLevel", "DCA level", "Key follow", "knob", 0, 127, (v) => v.keyFollowDcaLevel, (v, x) => ({ ...v, keyFollowDcaLevel: x })),
  num("kfDcaRate", "DCA rate", "Key follow", "knob", 0, 127, (v) => v.keyFollowDcaRate, (v, x) => ({ ...v, keyFollowDcaRate: x })),
  num("kfDcfLevel", "DCF level", "Key follow", "knob", 0, 127, (v) => v.keyFollowDcfLevel, (v, x) => ({ ...v, keyFollowDcfLevel: x })),
  num("kfDcfRate", "DCF rate", "Key follow", "knob", 0, 127, (v) => v.keyFollowDcfRate, (v, x) => ({ ...v, keyFollowDcfRate: x })),

  num("velDcaLevel", "DCA level", "Velocity sensitivity", "knob", 0, 127, (v) => v.velDcaLevel, (v, x) => ({ ...v, velDcaLevel: x })),
  num("velDcaRate", "DCA rate", "Velocity sensitivity", "knob", 0, 127, (v) => v.velDcaRate, (v, x) => ({ ...v, velDcaRate: x })),
  num("velDcfLevel", "DCF level", "Velocity sensitivity", "knob", 0, 127, (v) => v.velDcfLevel, (v, x) => ({ ...v, velDcfLevel: x })),
  num("velDcfRate", "DCF rate", "Velocity sensitivity", "knob", 0, 127, (v) => v.velDcfRate, (v, x) => ({ ...v, velDcfRate: x })),
  num("velDcq", "DCQ", "Velocity sensitivity", "knob", 0, 127, (v) => v.velDcq, (v, x) => ({ ...v, velDcq: x })),

  num("lfoRate", "rate", "LFO", "knob", 0, 127, (v) => v.lfoRate, (v, x) => ({ ...v, lfoRate: x })),
  num("lfoDelay", "delay", "LFO", "knob", 0, 127, (v) => v.lfoDelay, (v, x) => ({ ...v, lfoDelay: x })),
  num("lfoAttack", "attack", "LFO", "knob", 0, 127, (v) => v.lfoAttack, (v, x) => ({ ...v, lfoAttack: x })),
  num("lfoPitch", "pitch", "LFO", "knob", 0, 127, (v) => v.lfoPitch, (v, x) => ({ ...v, lfoPitch: x })),
  num("lfoAmp", "amp", "LFO", "knob", 0, 127, (v) => v.lfoAmp, (v, x) => ({ ...v, lfoAmp: x })),
  num("lfoFilter", "filter", "LFO", "knob", 0, 127, (v) => v.lfoFilter, (v, x) => ({ ...v, lfoFilter: x })),
  num("lfoResoDepth", "reso depth", "LFO", "knob", 0, 127, (v) => v.lfoResoDepth, (v, x) => ({ ...v, lfoResoDepth: x })),
  sel("lfoWave", "wave", "LFO", LFO_WAVES, (v) => v.lfoWave, (v, x) => ({ ...v, lfoWave: x })),

];

export function fieldsForGroup(group: string): Field[] {
  return FIELDS.filter((f) => f.group === group);
}
