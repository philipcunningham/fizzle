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
 *
 * A verified Go copy shipped in this repo before, in the studio TUI,
 * and survives in git history at pkg/studio/widgets/envelopevisual/.
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

/** Mirrors disk.KFDisplayToByte: the panel's -15 to 15 to a byte. */
function kfByte(display: number): number {
  return display * 8;
}

/** Mirrors disk.RateDisplayToByte: the panel's 0 to 99 to a rate byte. */
function rateByte(display: number): number {
  return display <= 0 ? 0 : Math.floor((display * 128 + 99) / 100);
}

/** The same, clamped as note on clamps an effective rate (F000:12E9). */
function rateIndex(display: number): number {
  return Math.max(MIN_RATE, Math.min(INSTANT_RATE, rateByte(display)));
}

/** The code the output stage carries for a level byte (F000:214C). */
function levelCode(level: number): number {
  const h = Math.max(0, Math.min(FULL_LEVEL, Math.round(level)));
  if (h <= LINEAR_TOP) return h + CODE_BASE;
  return EXPANSION[h - (LINEAR_TOP + 1)] ?? 895;
}

/**
 * The DCA is an analog amplifier inside the filter chip: MB87186 in
 * the FZ-20M service manual, which the FZ-1 parts list calls FM-1 and
 * fits four of, two channels each for eight voices. Sample data
 * reaches it through two 16 bit converters; the envelope never
 * touches the samples.
 *
 * The chip's control word is a gain word and an amplitude word, which
 * the firmware drives as one monotone number: it steps the pair by
 * one on the slew and by four on the fade a voice ends with, straight
 * across the boundary between the two registers. That fade cuts out a
 * quarter of the way up the range, which would click under a law
 * linear in amplitude, so the code is dB-like.
 *
 * Which levels are loud relative to which is therefore the firmware's
 * own map, below, and it is exact. How many dB the range covers is
 * not. The chip's documented 0 to -87.75 dB over the full 10 bit word
 * would put 57.6 dB across the codes the firmware uses, which spreads
 * one velocity sensitive voice over 46 dB and leaves ordinary playing
 * inaudible. No gain table for the chip has been found, so the span
 * below is tuned by ear until a measurement settles it.
 */
const SPAN_DB = 36;

/** The quietest and loudest codes the firmware writes. */
const QUIETEST_CODE = 224;
const LOUDEST_CODE = 895;
const DB_PER_CODE = SPAN_DB / (LOUDEST_CODE - QUIETEST_CODE);

/**
 * What a level sounds like, as a linear amplitude. A stop of zero is
 * code 224 rather than silence, which is far enough down to be
 * inaudible; the chip's own mute is a control word of zero, which the
 * firmware writes only when it frees the voice.
 */
export function amplitude(level: number): number {
  const code = levelCode(Math.max(0, Math.min(1, level)) * FULL_LEVEL);
  return Math.pow(10, ((code - LOUDEST_CODE) * DB_PER_CODE) / 20);
}

/**
 * How long a stage takes to travel between two level bytes at a panel
 * rate, in seconds. The envelope's own timing, floored by what the
 * output stage can slew, except at the instant rate.
 */
export function stageSeconds(from: number, to: number, rateDisplay: number): number {
  return stageSecondsByte(from, to, rateIndex(rateDisplay));
}

/** The same, from the rate byte the firmware actually steps with. */
function stageSecondsByte(from: number, to: number, rateByte: number): number {
  if (from === to) return 0;
  const index = Math.max(MIN_RATE, Math.min(INSTANT_RATE, rateByte));
  if (index >= INSTANT_RATE) return 0;
  const step = RATE_TABLE[index] ?? RATE_TABLE[MIN_RATE] ?? 3;
  const ramp = (Math.abs(to - from) * 256) / step / UPDATES_PER_SECOND;
  const slew = Math.abs(levelCode(to) - levelCode(from)) / CODES_PER_SECOND;
  return Math.max(ramp, slew);
}

const STAGES = 8;

/**
 * One stage: ramp to this level, from where the last one left off.
 *
 * The level is the envelope's own level as a fraction of full scale,
 * and the preview plays it as amplitude directly. That last step is a
 * choice rather than a reproduction: the firmware maps a level to an
 * output code (see levelCode), and how that code becomes loudness is
 * unmeasured. Only the two lines that divide by FULL_LEVEL below make
 * it, so a measurement can replace the law in one place.
 */
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
 * How a press and the keyboard position bend the envelope. Every field
 * is what the voice header carries, in the units the schema surfaces.
 */
export interface Scaling {
  /** The press, 1 to 127. */
  velocity: number;
  /** The note played, and the voice's own centre. */
  note: number;
  centre: number;
  /**
   * dcaLevelKF and dcaRateKF, the keyboard follows, on the panel's
   * -15 to 15. The firmware multiplies by the stored byte, which is
   * eight times that (disk.KFDisplayToByte).
   */
  levelKF: number;
  rateKF: number;
  /** velDcaKF and velDcaRS, the velocity follows, -127 to 127. */
  velLevel: number;
  velRate: number;
}

/**
 * The rates and stops a note actually runs, in bytes. Scaling is
 * applied once at note on (F000:12B4 to F000:135E): the two KF fields
 * follow the keyboard, and the two velocity fields follow the press.
 * A sensitivity of zero is a no op whatever the velocity, which is
 * what the subtraction of the sensitivity itself achieves.
 */
function effective(env: EnvelopeSnapshot, scale?: Scaling): { rates: number[]; stops: number[] } {
  const rates: number[] = [];
  const stops: number[] = [];
  // Velocity enters saturated (F000:125A), so even the lightest press
  // carries some weight.
  const velocity = scale ? Math.min(Math.max(scale.velocity, 0) + 0x10, 0x7f) : 0;
  const fromCentre = scale ? scale.note - scale.centre : 0;
  for (let stage = 0; stage < STAGES; stage++) {
    // Unclamped here: the firmware clamps the effective rate, after
    // the scaling terms are in (F000:12E9).
    let rate = rateByte(env.rates[stage] ?? 0);
    let stop = stopByte(env.stops[stage] ?? 0);
    if (scale) {
      rate +=
        ((fromCentre * kfByte(scale.rateKF)) >> 7) +
        ((((velocity * scale.velRate * 2) >> 8) + 1 - Math.max(0, scale.velRate)) >> 1);
      stop +=
        ((fromCentre * kfByte(scale.levelKF)) >> 4) +
        2 * (((velocity * scale.velLevel * 2) >> 8) - Math.max(0, scale.velLevel));
    }
    rates.push(Math.max(MIN_RATE, Math.min(INSTANT_RATE, rate)));
    stops.push(Math.max(0, Math.min(FULL_LEVEL, stop)));
  }
  return { rates, stops };
}

/**
 * The stages a held key runs. Note on caps the run at the lower of
 * the sustain and end stages (F000:123B), so the last segment's level
 * is where the note sits until it is released, and a sustain stage at
 * or past the end stage never holds anything at all: the voice runs
 * to its end stage, which note on forces to silence (F000:1351), and
 * frees its own slot with the key still down. Sustain at or past end
 * is how the format spells a one shot, and most factory voices are
 * written that way.
 */
export function attack(env: EnvelopeSnapshot, scale?: Scaling): Segment[] {
  const end = clampStage(env.end);
  const last = Math.min(clampStage(env.sustain), end);
  const { rates, stops } = effective(env, scale);
  const out: Segment[] = [];
  let level = 0;
  for (let stage = 0; stage <= last; stage++) {
    const target = stage === end ? 0 : (stops[stage] ?? 0);
    out.push({
      seconds: stageSecondsByte(level, target, rates[stage] ?? MIN_RATE),
      level: target / FULL_LEVEL,
    });
    level = target;
  }
  return out;
}

/**
 * The stages a released key runs. Note off compares the stage counter
 * with the sustain stage (F000:1525): a counter past it, which is the
 * parked state a held note sits in, leaves the counter alone, so
 * sustain plus one through the end stage run. A counter that has not
 * passed it, which covers every stage still ramping including the
 * sustain stage itself, is forced to the end stage, so that stage
 * runs alone and the ones between are skipped. reachedSustain is the
 * caller's answer to which happened.
 *
 * A voice whose sustain sits at or past its end stage never parks, so
 * it has nothing left to run once its attack has finished.
 *
 * The end stage always falls to silence, whatever the file stores,
 * because note on writes it that way (F000:1351). The stages before
 * it keep their own stops, so a release can rise before it falls.
 *
 * fromLevel is where the note actually was when the key came up, since
 * that is where the first release stage starts from.
 */
export function release(
  env: EnvelopeSnapshot,
  fromLevel: number,
  reachedSustain: boolean,
  scale?: Scaling,
): Segment[] {
  const sustain = clampStage(env.sustain);
  const end = clampStage(env.end);
  const { rates, stops } = effective(env, scale);
  const out: Segment[] = [];
  let level = Math.round(Math.max(0, Math.min(1, fromLevel)) * FULL_LEVEL);
  for (let stage = reachedSustain ? sustain + 1 : end; stage <= end; stage++) {
    const target = stage === end ? 0 : (stops[stage] ?? 0);
    out.push({
      seconds: stageSecondsByte(level, target, rates[stage] ?? MIN_RATE),
      level: target / FULL_LEVEL,
    });
    level = target;
  }
  return out;
}

/**
 * Where a run of stages sits `elapsed` seconds in, interpolating along
 * the stage it is in. A key released mid stage has to release from
 * here: Web Audio's own parameter reads back the stop before the ramp
 * in flight, not the level the note reached.
 */
export function levelAt(segments: Segment[], elapsed: number): number {
  let level = 0;
  let t = 0;
  for (const segment of segments) {
    const stops = t + segment.seconds;
    if (elapsed >= stops) {
      level = segment.level;
      t = stops;
      continue;
    }
    const travelled = segment.seconds > 0 ? (elapsed - t) / segment.seconds : 1;
    return level + (segment.level - level) * Math.max(0, travelled);
  }
  return level;
}

/** How long a run of stages takes, for scheduling what follows. */
export function totalSeconds(segments: Segment[]): number {
  return segments.reduce((total, segment) => total + segment.seconds, 0);
}
