// The DCA envelope as the FZ-1 firmware runs it. The model comes from
// reverse engineering and is recorded, cited by ROM address, in
// llm-wiki/topics/envelope-timing.md.
//
// A stage ramps from the previous stop level to its own. Each update
// indexes the rate table with the rate byte's magnitude and adds that
// to an accumulator whose high byte is the current level, so a stage
// takes |level delta| * 256 / table[rate] updates. States 3 and 7 of a
// 500 Hz service both dispatch the DCA handler at F000:2039, so those
// updates arrive 125 times a second.
import type { EnvelopeSnapshot } from "../boundary/contract";

/**
 * The 128 entry rate table at F000:0490 to F000:058F. Higher index,
 * larger step per update, so a higher rate is a faster stage. Index 0
 * is zero, which is why note on clamps an effective rate up to 1.
 */
const RATE_TABLE = [
  0x0000, 0x0003, 0x0006, 0x0009, 0x000d, 0x0010, 0x0014, 0x0018, 0x001c, 0x0021, 0x0025, 0x002a,
  0x002f, 0x0034, 0x003a, 0x0040, 0x0046, 0x004d, 0x0054, 0x005b, 0x0063, 0x006b, 0x0073, 0x007c,
  0x0085, 0x008f, 0x0099, 0x00a4, 0x00af, 0x00bb, 0x00c8, 0x00d5, 0x00e3, 0x00f1, 0x0101, 0x0111,
  0x0122, 0x0133, 0x0146, 0x015a, 0x016e, 0x0184, 0x019b, 0x01b3, 0x01cc, 0x01e7, 0x0203, 0x0220,
  0x023f, 0x025f, 0x0281, 0x02a5, 0x02cb, 0x02f2, 0x031c, 0x0348, 0x0376, 0x03a6, 0x03d9, 0x040e,
  0x0446, 0x0481, 0x04bf, 0x0501, 0x0545, 0x058d, 0x05d9, 0x0629, 0x067d, 0x06d5, 0x0731, 0x0793,
  0x07f9, 0x0865, 0x08d6, 0x094d, 0x09ca, 0x0a4d, 0x0ad7, 0x0b68, 0x0c01, 0x0ca2, 0x0d4a, 0x0dfc,
  0x0eb6, 0x0f7a, 0x1048, 0x1121, 0x1205, 0x12f4, 0x13f0, 0x14f8, 0x160f, 0x1733, 0x1867, 0x19aa,
  0x1afe, 0x1c63, 0x1dda, 0x1f65, 0x2104, 0x22b8, 0x2483, 0x2665, 0x2860, 0x2a75, 0x2ca5, 0x2ef2,
  0x315d, 0x33e8, 0x3694, 0x3964, 0x3c58, 0x3f73, 0x42b6, 0x4625, 0x49c1, 0x4d8c, 0x5188, 0x55b9,
  0x5a22, 0x5ec4, 0x63a2, 0x68c1, 0x6e23, 0x73cb, 0x79be, 0x7fff,
];

/**
 * The 96 entry table at F000:0590, which expands the top of the level
 * range on its way to the output stage. Below it the code is the level
 * plus 224; the pair spans 224 to 895, and 223 is the sentinel that
 * frees a voice slot.
 */
const EXPANSION = [
  0x0180, 0x0181, 0x0182, 0x0183, 0x0184, 0x0185, 0x0186, 0x0187, 0x0189, 0x018a, 0x018b, 0x018d,
  0x018e, 0x0190, 0x0191, 0x0193, 0x0194, 0x0196, 0x0198, 0x0199, 0x019b, 0x019d, 0x019f, 0x01a1,
  0x01a3, 0x01a5, 0x01a7, 0x01a9, 0x01ac, 0x01ae, 0x01b0, 0x01b3, 0x01b5, 0x01b8, 0x01bb, 0x01bd,
  0x01c0, 0x01c3, 0x01c6, 0x01c9, 0x01cc, 0x01d0, 0x01d3, 0x01d6, 0x01da, 0x01de, 0x01e2, 0x01e5,
  0x01e9, 0x01ee, 0x01f2, 0x01f6, 0x01fb, 0x01ff, 0x0204, 0x0209, 0x020e, 0x0213, 0x0219, 0x021e,
  0x0224, 0x022a, 0x0230, 0x0236, 0x023c, 0x0243, 0x0249, 0x0250, 0x0257, 0x025f, 0x0266, 0x026e,
  0x0276, 0x027e, 0x0287, 0x0290, 0x0299, 0x02a2, 0x02ab, 0x02b5, 0x02bf, 0x02ca, 0x02d4, 0x02df,
  0x02eb, 0x02f6, 0x0302, 0x030e, 0x031b, 0x0328, 0x0336, 0x0344, 0x0352, 0x0360, 0x036f, 0x037f,
];

/** Updates a second: 4 kHz IRQ, 500 Hz service, DCA on two states of eight. */
const UPDATES_PER_SECOND = 125;

/**
 * The output stage moves one code per 4 kHz tick (F000:0A5D), so no
 * stage crosses the code range faster than this. Traversing all 671
 * codes takes 168 ms, which is where the FZ's softness comes from.
 */
const CODES_PER_SECOND = 4000;

/** Rate byte 0x7F writes the ports directly (F000:2094), skipping the slew. */
const INSTANT_RATE = 0x7f;

/** Note on clamps an effective rate into this range (F000:12E9). */
const MIN_RATE = 1;

const FULL_LEVEL = 255;
const LINEAR_TOP = 0x9f;
const CODE_BASE = 0xe0;

/** Mirrors disk.RateDisplayToByte: the panel's 0 to 99 to a table index. */
function rateIndex(display: number): number {
  const byte = display <= 0 ? 0 : Math.floor((display * 128 + 99) / 100);
  return Math.max(MIN_RATE, Math.min(INSTANT_RATE, byte));
}

/** The code the output stage carries for a level byte (F000:214C). */
function levelCode(level: number): number {
  const h = Math.max(0, Math.min(FULL_LEVEL, Math.round(level)));
  if (h <= LINEAR_TOP) return h + CODE_BASE;
  return EXPANSION[h - (LINEAR_TOP + 1)] ?? 895;
}

/**
 * How long a stage takes to travel between two level bytes at a panel
 * rate, in seconds. The envelope's own timing, floored by what the
 * output stage can slew, except at the instant rate.
 */
export function stageSeconds(from: number, to: number, rateDisplay: number): number {
  if (from === to) return 0;
  const index = rateIndex(rateDisplay);
  if (index >= INSTANT_RATE) return 0;
  const step = RATE_TABLE[index] ?? RATE_TABLE[MIN_RATE] ?? 3;
  const ramp = (Math.abs(to - from) * 256) / step / UPDATES_PER_SECOND;
  const slew = Math.abs(levelCode(to) - levelCode(from)) / CODES_PER_SECOND;
  return Math.max(ramp, slew);
}

const STAGES = 8;

/** One stage: ramp to this level, from where the last one left off. */
export interface Segment {
  seconds: number;
  /** The stage's stop level, 0 to 1. */
  level: number;
}

/** Mirrors disk.StopDisplayToByte: the panel's 0 to 99 to a level byte. */
function stopByte(display: number): number {
  if (display <= 0) return 0;
  if (display >= 99) return FULL_LEVEL;
  return Math.floor((FULL_LEVEL * (display - 1)) / 99) + 1;
}

function clampStage(n: number): number {
  return Math.max(0, Math.min(STAGES - 1, Math.trunc(n)));
}

/**
 * The stages a held key runs, from silence to the sustain stage. The
 * last segment's level is where the note sits until it is released.
 */
export function attack(env: EnvelopeSnapshot): Segment[] {
  const sustain = clampStage(env.sustain);
  const out: Segment[] = [];
  let level = 0;
  for (let stage = 0; stage <= sustain; stage++) {
    const target = stopByte(env.stops[stage] ?? 0);
    out.push({
      seconds: stageSeconds(level, target, env.rates[stage] ?? 0),
      level: target / FULL_LEVEL,
    });
    level = target;
  }
  return out;
}

/**
 * The stages a released key runs. Ordinarily sustain plus one through
 * the end stage; a key that came up before the sustain stage was
 * reached jumps straight to the end stage and runs it alone
 * (F000:1512). The end stage always falls to silence, whatever the
 * file stores, because note on writes it that way (F000:1351).
 *
 * fromLevel is where the note actually was when the key came up, since
 * that is where the first release stage starts from.
 */
export function release(
  env: EnvelopeSnapshot,
  fromLevel: number,
  reachedSustain: boolean,
): Segment[] {
  const sustain = clampStage(env.sustain);
  const end = clampStage(env.end);
  const out: Segment[] = [];
  let level = Math.round(Math.max(0, Math.min(1, fromLevel)) * FULL_LEVEL);
  for (let stage = reachedSustain ? sustain + 1 : end; stage <= end; stage++) {
    const target = stage === end ? 0 : stopByte(env.stops[stage] ?? 0);
    out.push({
      seconds: stageSeconds(level, target, env.rates[stage] ?? 0),
      level: target / FULL_LEVEL,
    });
    level = target;
  }
  return out;
}

/** How long a run of stages takes, for scheduling what follows. */
export function totalSeconds(segments: Segment[]): number {
  return segments.reduce((total, segment) => total + segment.seconds, 0);
}
