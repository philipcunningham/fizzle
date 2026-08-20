import type {
  AreaSnapshot,
  AuditionData,
  Channel,
  Core,
  CoreResult,
  FileSnapshot,
  ImportEstimate,
  InstrumentSnapshot,
  InstrumentVoice,
  SFZImportResult,
  SampleRate,
  SchemaField,
  Snapshot,
  VoiceDetail,
} from "../boundary/contract";
import {
  CHANNELS,
  IMAGE_SIZE,
  MAX_LABEL_LENGTH,
  SAMPLE_RATES,
  err,
  ok,
} from "../boundary/contract";

// The estimate's capacity model, scaled-down mirrors of the core's:
// the sampler's sample memory in samples, the dump one disk holds,
// and the empty instrument a first voice brings with it.
const FAKE_SAMPLE_CAP = 1_048_576;
const FAKE_DUMP_MAX = IMAGE_SIZE - 4096;
const FAKE_DUMP_BASE = 1024;

// Mirrors fzutil.VoiceName: basename, extension stripped, uppercased,
// runs of other characters collapsing to single spaces, 12 chars.
export function voiceName(filename: string): string {
  const stem = (filename.split("/").pop() ?? filename).replace(/\.[^.]*$/, "").toUpperCase();
  const collapsed = stem
    .replace(/[^A-Z0-9]+/g, " ")
    .trim()
    .slice(0, 12)
    .trim();
  return collapsed === "" ? "VOICE" : collapsed;
}

// A miniature of the real schema: one field per control kind, plus a
// kind no registry knows, so the numeric fallback stays tested.
export const FAKE_SCHEMA: SchemaField[] = [
  {
    id: "playbackMode",
    label: "Playback",
    group: "Sample",
    kind: "select",
    min: 0,
    max: 0,
    options: ["normal", "reverse", "cue", "synth"],
  },
  {
    // The core declares this one too, with the same id, label, and
    // options (from disk.SampleRates), so a UI test sees the real field.
    id: "sampleRate",
    label: "Sample rate (Hz)",
    group: "Sample",
    kind: "select",
    min: 0,
    max: 0,
    options: ["36000", "18000", "9000"],
  },
  {
    id: "tune",
    label: "Tune (1/256 semi)",
    group: "Identity and mapping",
    kind: "stepper",
    min: -32768,
    max: 32767,
  },
  { id: "rootKey", label: "Root", group: "Identity and mapping", kind: "note", min: 0, max: 127 },
  { id: "keyLow", label: "Key low", group: "Identity and mapping", kind: "note", min: 0, max: 127 },
  {
    id: "keyHigh",
    label: "Key high",
    group: "Identity and mapping",
    kind: "note",
    min: 0,
    max: 127,
  },
  { id: "cutoff", label: "Cutoff", group: "Filter", kind: "knob", min: 0, max: 127 },
  { id: "wobble", label: "Wobble", group: "Filter", kind: "hyper-dial", min: 0, max: 99 },
];

function defaultParams(): Record<string, number | string> {
  return {
    playbackMode: "normal",
    sampleRate: "18000",
    tune: 0,
    rootKey: 60,
    keyLow: 36,
    keyHigh: 96,
    cutoff: 127,
    wobble: 0,
  };
}

function defaultVoiceDetail(frames: number): VoiceDetail {
  return {
    frames,
    sampleRate: 18000,
    genStart: 0,
    genEnd: frames,
    loopSustain: 8,
    loopRelease: 8,
    // What an import writes: every loop parked at the generation end
    // with no width, and no loop named. A fake that handed out full
    // width loops would hide the shape the editor actually meets.
    loops: Array.from({ length: 8 }, () => ({ start: frames, end: frames, xf: 0, tm: 0 })),
    dca: {
      sustain: 7,
      end: 7,
      rates: new Array<number>(8).fill(50),
      stops: new Array<number>(8).fill(99),
    },
    dcf: {
      sustain: 7,
      end: 7,
      rates: new Array<number>(8).fill(50),
      stops: new Array<number>(8).fill(99),
    },
  };
}

function cloneDetail(d: VoiceDetail): VoiceDetail {
  return {
    ...d,
    loops: d.loops.map((l) => ({ ...l })),
    dca: { ...d.dca, rates: [...d.dca.rates], stops: [...d.dca.stops] },
    dcf: { ...d.dcf, rates: [...d.dcf.rates], stops: [...d.dcf.stops] },
  };
}

const clampNum = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

/**
 * Mirrors webcore.clampGeneration. The FZ spec requires wavst <= genst
 * <= gened <= waved, so the end clamps up to the start rather than
 * inverting the pair, and neither escapes the voice's own frames.
 */
function clampGeneration(frames: number, start: number, end: number): [number, number] {
  const lo = clampNum(start, 0, frames);
  return [lo, clampNum(end, lo, frames)];
}

interface FakeState {
  label: string | null;
  bytes: Uint8Array | null;
  /** Disk 2 of a split pair; null for one disk documents. */
  bytes2: Uint8Array | null;
  files: FileSnapshot[];
  used: number;
  instrument: InstrumentSnapshot | null;
  /** Mirrors DiskSnapshot.missingDisk for lone halves of a pair. */
  missingDisk: number;
}

function fakeArea(
  voiceSlot: number,
  voiceName: string,
  overrides?: Partial<AreaSnapshot>,
): AreaSnapshot {
  return {
    voiceSlot,
    voiceName,
    keyLow: 0,
    keyHigh: 127,
    root: 60,
    velLow: 1,
    velHigh: 127,
    midiChannel: 1,
    output: 255,
    outputLabel: "all",
    volume: 0,
    ...overrides,
  };
}

// An enriched instrument voice, params and detail included: the shape
// the real snapshot carries per slot.
function fakeVoice(slot: number, name: string, referenced: boolean): InstrumentVoice {
  const frames = 4096 + slot * 256;
  return {
    slot,
    name,
    referenced,
    params: defaultParams(),
    voice: defaultVoiceDetail(frames),
    audioKey: `${String(slot * 100_000)}:${String(slot * 100_000 + frames)}`,
  };
}

// The instrument an opened image carries: one bank, two areas, and a
// third voice nothing references, so the R13 map action has work.
function fakeInstrument(): InstrumentSnapshot {
  return {
    effects: {
      bendRange: 16,
      matrix: Array.from({ length: 3 }, () => new Array<number>(7).fill(0)),
    },
    fileName: "FULL-DATA-FZ",
    banks: [
      {
        name: "BANK A",
        areas: [
          fakeArea(0, "KICK", { keyHigh: 59 }),
          fakeArea(1, "SNARE", { keyLow: 60, velLow: 64 }),
        ],
      },
    ],
    voices: [fakeVoice(0, "KICK", true), fakeVoice(1, "SNARE", true), fakeVoice(2, "SPARE", false)],
  };
}

function cloneInstrument(inst: InstrumentSnapshot): InstrumentSnapshot {
  const copy: InstrumentSnapshot = {
    fileName: inst.fileName,
    banks: inst.banks.map((b) => ({ name: b.name, areas: b.areas.map((a) => ({ ...a })) })),
    voices: inst.voices.map((v) => {
      const voice: InstrumentVoice = { slot: v.slot, name: v.name, referenced: v.referenced };
      if (v.params) voice.params = { ...v.params };
      if (v.voice) voice.voice = cloneDetail(v.voice);
      if (v.sharesAudio) voice.sharesAudio = true;
      if (v.audioKey !== undefined) voice.audioKey = v.audioKey;
      return voice;
    }),
  };
  if (inst.effects) {
    copy.effects = {
      bendRange: inst.effects.bendRange,
      matrix: inst.effects.matrix.map((row) => [...row]),
    };
  }
  return copy;
}

function refreshReferenced(inst: InstrumentSnapshot): void {
  const used = new Set(inst.banks.flatMap((b) => b.areas.map((a) => a.voiceSlot)));
  for (const v of inst.voices) v.referenced = used.has(v.slot);
}

// The core decodes a voice's whole wave, so the payload holds one
// sample per declared frame. A fixed length here would let a test pin
// loop bounds the buffer cannot reach.
function fakeAudition(frames: number, sampleRate: number): AuditionData {
  const pcm = new Int16Array(frames);
  for (let i = 0; i < pcm.length; i++) pcm[i] = Math.round(12000 * Math.sin(i / 6));
  return { sampleRate, root: 60, pcm };
}

// Mirrors the core's historyCap, which R24 floors at 100. A fake that
// caps lower would pass a UI test on a depth the real core rejects.
const HISTORY_CAP = 100;

/** Documents larger than one disk's payload split across a pair. */
export const FAKE_SPLIT_THRESHOLD = 1_300_000;

function emptyState(): FakeState {
  return {
    label: null,
    bytes: null,
    bytes2: null,
    files: [],
    used: 0,
    instrument: null,
    missingDisk: 0,
  };
}

/**
 * The fake classifies opened images by their first byte, so drop and
 * pair tests can stage split halves: 1 marks disk 1 of a pair (disk 2
 * missing), 2 marks a lone disk 2. Anything else opens whole.
 */
function fakeMissingDisk(image: Uint8Array): number {
  if (image[0] === 1) return 2;
  if (image[0] === 2) return 1;
  return 0;
}

/**
 * Arguments the fake received that its snapshot cannot show. The stereo
 * answer is one: it changes the samples the real core writes, and the
 * fake has no samples, so a test asserts it here. createFakeCore clears
 * it, so each test starts from a blank record.
 */
export const fakeCalls: { wavFolderChannel: Channel | null; sfzChannel: Channel | null } = {
  wavFolderChannel: null,
  sfzChannel: null,
};

export function createFakeCore(): Core {
  let revision = 0;
  // The core's default: the machine Casio sold most of.
  let memoryBytes = 1024 * 1024;
  let state: FakeState = emptyState();
  fakeCalls.wavFolderChannel = null;
  fakeCalls.sfzChannel = null;
  let past: FakeState[] = [];
  let future: FakeState[] = [];
  let inGesture = false;
  let gestureBase: FakeState | null = null;

  const clone = (s: FakeState): FakeState => ({
    label: s.label,
    bytes: s.bytes,
    bytes2: s.bytes2,
    files: s.files.map((f) => {
      const copy: FileSnapshot = { name: f.name, type: f.type, sizeBytes: f.sizeBytes };
      if (f.params) copy.params = { ...f.params };
      if (f.voice) copy.voice = cloneDetail(f.voice);
      return copy;
    }),
    used: s.used,
    instrument: s.instrument ? cloneInstrument(s.instrument) : null,
    missingDisk: s.missingDisk,
  });

  const snap = (): Snapshot => ({
    revision,
    canUndo: past.length > 0,
    canRedo: future.length > 0,
    disk:
      state.label === null
        ? null
        : {
            label: state.label,
            usedBytes: state.used,
            // A clone shares an earlier slot's samples, so it costs the
            // sampler nothing and is not counted twice.
            audioBytes: (state.instrument?.voices ?? []).reduce(
              (sum, v) => sum + (v.sharesAudio ? 0 : (v.voice?.frames ?? 0) * 2),
              0,
            ),
            memoryBytes,
            capacityBytes: (state.bytes2 ? 2 : 1) * IMAGE_SIZE,
            disks: state.bytes2 ? 2 : 1,
            ...(state.missingDisk ? { missingDisk: state.missingDisk } : {}),
            files: state.files.map((f) => ({ ...f })),
            ...(state.instrument ? { instrument: cloneInstrument(state.instrument) } : {}),
          },
  });

  // The real session's history semantics: gestures coalesce to one
  // entry, redo clears, and the empty pre-disk state never joins.
  const mutate = (next: FakeState): Snapshot => {
    const prev = state;
    if (prev.label !== null) {
      if (inGesture) {
        gestureBase ??= prev;
      } else {
        past = [...past, prev].slice(-HISTORY_CAP);
      }
    }
    future = [];
    state = next;
    revision += 1;
    return snap();
  };

  // The fake's adoptPair: every accepted mutation lands here, so the
  // missing-disk refusal sits after each method's own argument
  // validation, as the real session orders them.
  const commit = (next: FakeState): CoreResult<Snapshot> => {
    const guard = missingGuard();
    if (guard) return guard;
    return ok(mutate(next));
  };

  // Mirrors Session.checkWholeDocument: every mutation on a lone half
  // of a split pair refuses without touching the document. Opens, undo,
  // redo, and the gesture brackets stay allowed.
  const missingGuard = (): CoreResult<never> | null =>
    state.missingDisk
      ? err(
          "missing-disk",
          `disk ${String(state.missingDisk)} of this split instrument is missing; open it alongside this one to edit the instrument`,
        )
      : null;

  // Mirrors Session.endGesture: an undo or redo inside a drag first
  // lands the pre-gesture document, or the timeline inverts.
  const endGesture = () => {
    if (inGesture && gestureBase !== null) {
      past = [...past, gestureBase].slice(-HISTORY_CAP);
      gestureBase = null;
    }
  };

  return {
    snapshot(): Promise<CoreResult<Snapshot>> {
      return Promise.resolve(ok(snap()));
    },

    newDisk(newLabel: string): Promise<CoreResult<Snapshot>> {
      if (newLabel.length === 0 || newLabel.length > MAX_LABEL_LENGTH) {
        return Promise.resolve(
          err("invalid-label", `label must be 1 to ${MAX_LABEL_LENGTH} characters`),
        );
      }
      return Promise.resolve(ok(mutate({ ...emptyState(), label: newLabel })));
    },

    openImage(image: Uint8Array): Promise<CoreResult<Snapshot>> {
      if (image.length !== IMAGE_SIZE) {
        return Promise.resolve(
          err("invalid-image", `an FZ image is ${IMAGE_SIZE} bytes, got ${image.length}`),
        );
      }
      return Promise.resolve(
        ok(
          mutate({
            ...emptyState(),
            label: "OPENED",
            bytes: image.slice(),
            files: [{ name: "FULL-DATA-FZ", type: "full", sizeBytes: IMAGE_SIZE - 4096 }],
            used: IMAGE_SIZE,
            instrument: fakeInstrument(),
            missingDisk: fakeMissingDisk(image),
          }),
        ),
      );
    },

    schema(): Promise<CoreResult<SchemaField[]>> {
      return Promise.resolve(ok(FAKE_SCHEMA.map((f) => ({ ...f }))));
    },

    undo(): Promise<CoreResult<Snapshot>> {
      endGesture();
      const prev = past[past.length - 1];
      if (!prev) {
        return Promise.resolve(err("nothing-to-undo", "nothing to undo"));
      }
      past = past.slice(0, -1);
      future = [...future, state];
      state = prev;
      revision += 1;
      return Promise.resolve(ok(snap()));
    },

    redo(): Promise<CoreResult<Snapshot>> {
      endGesture();
      const next = future[future.length - 1];
      if (!next) {
        return Promise.resolve(err("nothing-to-redo", "nothing to redo"));
      }
      future = future.slice(0, -1);
      past = [...past, state].slice(-HISTORY_CAP);
      state = next;
      revision += 1;
      return Promise.resolve(ok(snap()));
    },

    beginGesture(): Promise<CoreResult<Snapshot>> {
      if (!inGesture) {
        inGesture = true;
        gestureBase = null;
      }
      return Promise.resolve(ok(snap()));
    },

    commitGesture(): Promise<CoreResult<Snapshot>> {
      let landed = false;
      if (inGesture) {
        inGesture = false;
        if (gestureBase) {
          past = [...past, gestureBase].slice(-HISTORY_CAP);
          gestureBase = null;
          landed = true;
          // The landed entry is new state, as in the real session.
          revision += 1;
        }
      }
      return Promise.resolve(ok({ ...snap(), gestureLanded: landed }));
    },

    setAreaField(bank: number, area: number, field: string, value: number) {
      const next = clone(state);
      const target = next.instrument?.banks[bank]?.areas[area];
      if (!next.instrument || !target) {
        return Promise.resolve(err("invalid-value", "no such area"));
      }
      const clamp127 = (v: number) => clampNum(v, 0, 127);
      switch (field) {
        case "keyLow":
          target.keyLow = clamp127(value);
          break;
        case "keyHigh":
          target.keyHigh = clamp127(value);
          break;
        case "root":
          target.root = clamp127(value);
          break;
        case "velLow":
          target.velLow = clamp127(value);
          break;
        case "velHigh":
          target.velHigh = clamp127(value);
          break;
        case "volume":
          target.volume = clamp127(value);
          break;
        case "midiChannel":
          target.midiChannel = clampNum(value, 1, 16);
          break;
        case "output":
          target.output = clampNum(value, 0, 255);
          target.outputLabel = target.output === 255 ? "all" : String(target.output);
          break;
        case "voiceSlot": {
          const voice = next.instrument.voices.find((v) => v.slot === value);
          if (!voice) return Promise.resolve(err("invalid-value", "no such voice slot"));
          target.voiceSlot = voice.slot;
          target.voiceName = voice.name;
          break;
        }
        default:
          return Promise.resolve(err("invalid-field", `${field} is not an area field`));
      }
      refreshReferenced(next.instrument);
      return Promise.resolve(commit(next));
    },

    renameBank(bank: number, name: string) {
      if (name.length === 0 || name.length > 12) {
        return Promise.resolve(err("invalid-value", "bank name must be 1 to 12 characters"));
      }
      const next = clone(state);
      const target = next.instrument?.banks[bank];
      if (!target) return Promise.resolve(err("invalid-value", "no such bank"));
      target.name = name;
      return Promise.resolve(commit(next));
    },

    swapAreas(bank: number, a: number, b: number) {
      const next = clone(state);
      const areas = next.instrument?.banks[bank]?.areas;
      const left = areas?.[a];
      const right = areas?.[b];
      if (!areas || !left || !right) {
        return Promise.resolve(err("invalid-value", "no such area"));
      }
      areas[a] = right;
      areas[b] = left;
      return Promise.resolve(commit(next));
    },

    deleteArea(bank: number, area: number) {
      const next = clone(state);
      const bankSnap = next.instrument?.banks[bank];
      if (!next.instrument || !bankSnap?.areas[area]) {
        return Promise.resolve(err("invalid-value", "no such area"));
      }
      // The real core refuses a bank's last area and removes a voice
      // no remaining area plays.
      if (bankSnap.areas.length === 1) {
        return Promise.resolve(
          err("last-area", "a bank keeps at least one area; delete the bank instead"),
        );
      }
      const freedSlot = bankSnap.areas[area].voiceSlot;
      bankSnap.areas.splice(area, 1);
      const stillPlayed = next.instrument.banks.some((b) =>
        b.areas.some((a) => a.voiceSlot === freedSlot),
      );
      if (!stillPlayed) {
        // The core sizes the voice area from the banks, so a freed slot
        // leaves and every slot above it shifts down, with the areas
        // that play them renumbered to match.
        next.instrument.voices = next.instrument.voices
          .filter((v) => v.slot !== freedSlot)
          .map((v) => (v.slot > freedSlot ? { ...v, slot: v.slot - 1 } : v));
        for (const b of next.instrument.banks) {
          for (const a of b.areas) {
            if (a.voiceSlot > freedSlot) a.voiceSlot -= 1;
          }
        }
      }
      refreshReferenced(next.instrument);
      return Promise.resolve(commit(next));
    },

    addArea(bank: number, voiceSlot: number) {
      const next = clone(state);
      const bankSnap = next.instrument?.banks[bank];
      const voice = next.instrument?.voices.find((v) => v.slot === voiceSlot);
      if (!next.instrument || !bankSnap || !voice) {
        return Promise.resolve(err("invalid-value", "no such bank or voice"));
      }
      bankSnap.areas.push(fakeArea(voice.slot, voice.name));
      refreshReferenced(next.instrument);
      return Promise.resolve(commit(next));
    },

    duplicateArea(bank: number, area: number) {
      const next = clone(state);
      const bankSnap = next.instrument?.banks[bank];
      const src = bankSnap?.areas[area];
      if (!next.instrument || !bankSnap || !src) {
        return Promise.resolve(err("invalid-value", "no such area"));
      }
      const newSlot = Math.max(...next.instrument.voices.map((v) => v.slot)) + 1;
      // The duplicate shares the source's audio: no extra bytes.
      const duplicate = fakeVoice(newSlot, src.voiceName, true);
      duplicate.sharesAudio = true;
      next.instrument.voices.push(duplicate);
      bankSnap.areas.push({ ...src, voiceSlot: newSlot });
      refreshReferenced(next.instrument);
      return Promise.resolve(commit(next));
    },

    setEffectCell(controller: number, target: number, value: number) {
      const next = clone(state);
      const row = next.instrument?.effects?.matrix[controller];
      if (!next.instrument?.effects || !row || target < 0 || target > 6) {
        return Promise.resolve(err("invalid-value", "no such cell"));
      }
      row[target] = clampNum(value, 0, 127);
      return Promise.resolve(commit(next));
    },

    setBendRange(value: number) {
      const next = clone(state);
      if (!next.instrument?.effects) {
        return Promise.resolve(err("invalid-value", "no effects block"));
      }
      next.instrument.effects.bendRange = clampNum(value, 0, 127);
      return Promise.resolve(commit(next));
    },

    mapVoice(voiceSlot: number) {
      const next = clone(state);
      const voice = next.instrument?.voices.find((v) => v.slot === voiceSlot);
      const bankSnap = next.instrument?.banks[0];
      if (!next.instrument || !voice || !bankSnap) {
        return Promise.resolve(err("invalid-value", "no such voice"));
      }
      bankSnap.areas.push(fakeArea(voice.slot, voice.name));
      refreshReferenced(next.instrument);
      return Promise.resolve(commit(next));
    },

    auditionSlot(slot: number): Promise<CoreResult<AuditionData>> {
      const voice = state.instrument?.voices.find((v) => v.slot === slot);
      if (!voice) return Promise.resolve(err("invalid-value", `no voice slot ${slot}`));
      return Promise.resolve(
        ok(fakeAudition(voice.voice?.frames ?? 1024, voice.voice?.sampleRate ?? 18000)),
      );
    },

    exportImage(): Promise<CoreResult<Uint8Array>> {
      if (state.label === null) {
        return Promise.resolve(err("no-disk", "no disk is open"));
      }
      return Promise.resolve(ok(state.bytes ? state.bytes.slice() : new Uint8Array(IMAGE_SIZE)));
    },

    exportImageAt(index: number): Promise<CoreResult<Uint8Array>> {
      if (state.label === null) {
        return Promise.resolve(err("no-disk", "no disk is open"));
      }
      if (index === 0) {
        return Promise.resolve(ok(state.bytes ? state.bytes.slice() : new Uint8Array(IMAGE_SIZE)));
      }
      if (index === 1) {
        if (!state.bytes2) {
          return Promise.resolve(err("not-found", "the document is one disk; there is no disk 2"));
        }
        return Promise.resolve(ok(state.bytes2.slice()));
      }
      return Promise.resolve(err("invalid-value", `disk index must be 0 or 1, got ${index}`));
    },

    loadFzf(bytes: Uint8Array): Promise<CoreResult<Snapshot>> {
      if (bytes.length === 0) {
        return Promise.resolve(err("invalid-fzf", "empty dump"));
      }
      const split = bytes.length > FAKE_SPLIT_THRESHOLD;
      const next = clone(state);
      next.label ??= "FIZZLE";
      next.missingDisk = 0;
      next.bytes2 = split ? new Uint8Array(IMAGE_SIZE) : null;
      next.instrument = {
        fileName: "FULL-DATA-FZ",
        banks: [{ name: "BANK A", areas: [fakeArea(0, "LOADED")] }],
        voices: [{ slot: 0, name: "LOADED", referenced: true }],
      };
      next.files = [{ name: "FULL-DATA-FZ", type: "full", sizeBytes: bytes.length }];
      return Promise.resolve(commit(next));
    },

    addVoice(bytes: Uint8Array): Promise<CoreResult<Snapshot>> {
      if (bytes.length === 0) {
        return Promise.resolve(err("not-a-voice", "empty voice file"));
      }
      const next = clone(state);
      next.label ??= "FIZZLE";
      return Promise.resolve(ok(mutate(joinVoice(next, `VOICE ${voiceCount(next) + 1}`))));
    },

    addBank(bytes: Uint8Array, slot: number): Promise<CoreResult<Snapshot>> {
      if (bytes.length === 0) {
        return Promise.resolve(err("invalid-fzb", "empty bank file"));
      }
      const next = clone(state);
      next.label ??= "FIZZLE";
      if (!next.instrument) {
        next.instrument = {
          fileName: "FULL-DATA-FZ",
          banks: [{ name: "FZB BANK", areas: [fakeArea(0, "FZB VOICE")] }],
          voices: [{ slot: 0, name: "FZB VOICE", referenced: true }],
        };
        return Promise.resolve(commit(next));
      }
      if (slot < 0 || slot > next.instrument.banks.length || slot >= 8) {
        return Promise.resolve(err("invalid-value", `bank slot ${slot} out of range`));
      }
      const joined = { name: "FZB BANK", areas: [fakeArea(0, "FZB VOICE")] };
      if (slot === next.instrument.banks.length) {
        next.instrument.banks.push(joined);
      } else {
        next.instrument.banks[slot] = joined;
      }
      refreshReferenced(next.instrument);
      return Promise.resolve(commit(next));
    },

    importWavToInstrument(
      filename: string,
      wav: Uint8Array,
      rate: SampleRate,
      _channel: Channel,
    ): Promise<CoreResult<Snapshot>> {
      if (wav.length === 0) {
        return Promise.resolve(err("invalid-wav", "empty WAV"));
      }
      const next = clone(state);
      next.label ??= "FIZZLE";
      // The dump grows by the converted audio, so a later estimate
      // sees the room this import used up, as the core's would.
      const shape = wavShape(wav);
      const grown = shape ? convertedBytes(shape, rate) : wav.length;
      joinVoice(next, voiceName(filename), { frames: Math.round(grown / 2), rate });
      const dump = next.files.find((f) => f.type === "full");
      if (dump) dump.sizeBytes += grown;
      next.used += grown;
      return Promise.resolve(commit(next));
    },

    estimateImport(
      files: Record<string, Uint8Array>,
      rate: SampleRate,
      channel: Channel,
    ): Promise<CoreResult<ImportEstimate>> {
      if (!CHANNELS.includes(channel)) {
        return Promise.resolve(
          err("invalid-channel", `channel ${channel} is not left, right, or mix`),
        );
      }
      const names = Object.keys(files).sort();
      if (names.length === 0) {
        return Promise.resolve(err("invalid-value", "no files to estimate"));
      }
      // The real core refuses the estimate on a lone half of a pair,
      // because the import it describes would be refused too.
      const missing = missingGuard();
      if (missing) return Promise.resolve(missing);
      const shapes: { name: string; channels: number; rate: number; frames: number }[] = [];
      for (const name of names) {
        const shape = wavShape(files[name] ?? new Uint8Array());
        if (!shape) {
          return Promise.resolve(err("invalid-wav", `${name}: not a readable WAV`));
        }
        // Mirrors fzutil.MinSampleRate: the core refuses rates a real
        // recording never carries before doing any arithmetic on them.
        if (shape.rate < 1000) {
          return Promise.resolve(
            err(
              "invalid-wav",
              `${name}: sample rate ${String(shape.rate)} Hz is below minimum 1000 Hz`,
            ),
          );
        }
        shapes.push({ name, ...shape });
      }
      const dump = state.files.find((f) => f.type === "full");
      const dumpLen = dump?.sizeBytes ?? 0;
      const disks = state.bytes2 ? 2 : 1;
      const estimateAt = (target: number): ImportEstimate => {
        const base: ImportEstimate = {
          bytes: 0,
          seconds: 0,
          roomSeconds: 0,
          audioAfterBytes: 0,
          memoryBytes,
          verdict: "fits",
          reason: "",
          anyStereo: shapes.some((s) => s.channels >= 2),
          overCapFile: "",
          fileSeconds: 0,
          capSeconds: 0,
          fitsAtRates: [],
        };
        let audio = 0;
        for (const s of shapes) {
          const samples = Math.round((s.frames * target) / s.rate);
          if (samples > FAKE_SAMPLE_CAP) {
            return {
              ...base,
              verdict: "wont-fit",
              reason: "sample-memory",
              overCapFile: s.name,
              fileSeconds: s.frames / s.rate,
              capSeconds: FAKE_SAMPLE_CAP / target,
            };
          }
          audio += samples * 2;
          base.seconds += samples / target;
        }
        const creating = state.instrument === null;
        const slots = (state.instrument?.voices.length ?? 0) + shapes.length;
        if (slots > 64) {
          return { ...base, verdict: "wont-fit", reason: "voice-limit" };
        }
        base.bytes = audio + (creating ? FAKE_DUMP_BASE : 0);
        const newLen = dumpLen + base.bytes;
        // Every import path re-splits as its size dictates, the lone
        // first voice included, matching the core.
        if (newLen <= FAKE_DUMP_MAX) {
          base.verdict = "fits";
        } else if (newLen <= 2 * FAKE_DUMP_MAX && newLen <= 2 * 1024 * 1024) {
          base.verdict = "splits";
        } else {
          base.verdict = "wont-fit";
          base.reason = "disk-room";
        }
        return base;
      };
      const est = estimateAt(rate);
      // The instrument's own audio, plus what this import adds.
      const held = (state.instrument?.voices ?? []).reduce(
        (sum, v) => sum + (v.sharesAudio ? 0 : (v.voice?.frames ?? 0) * 2),
        0,
      );
      est.audioAfterBytes = held + est.bytes;
      est.roomSeconds =
        Math.min(disks * FAKE_DUMP_MAX - dumpLen, Math.max(0, memoryBytes - held)) / 2 / rate;
      if (est.verdict === "wont-fit") {
        est.fitsAtRates = SAMPLE_RATES.filter((r) => estimateAt(r).verdict !== "wont-fit").map(
          (r) => r,
        );
      }
      return Promise.resolve(ok(est));
    },

    openImagePair(a: Uint8Array, b: Uint8Array): Promise<CoreResult<Snapshot>> {
      for (const image of [a, b]) {
        if (image.length !== IMAGE_SIZE) {
          return Promise.resolve(
            err("invalid-image", `an FZ image is ${IMAGE_SIZE} bytes, got ${image.length}`),
          );
        }
      }
      const nums = [fakeMissingDisk(a), fakeMissingDisk(b)].sort();
      if (!(nums[0] === 1 && nums[1] === 2)) {
        return Promise.resolve(err("pair-mismatch", "the images are not disks 1 and 2 of a set"));
      }
      const disk1 = fakeMissingDisk(a) === 2 ? a : b;
      const disk2 = disk1 === a ? b : a;
      return Promise.resolve(
        ok(
          mutate({
            ...emptyState(),
            label: "PAIR",
            bytes: disk1.slice(),
            bytes2: disk2.slice(),
            files: [{ name: "FULL-DATA-FZ", type: "full", sizeBytes: 2 * (IMAGE_SIZE - 4096) }],
            used: 2 * IMAGE_SIZE,
            instrument: fakeInstrument(),
          }),
        ),
      );
    },

    importSfz(
      files: Record<string, Uint8Array>,
      sfzPath: string,
      rate: SampleRate,
      fitToDisk: boolean,
      split: boolean,
      channel: Channel,
    ): Promise<CoreResult<SFZImportResult>> {
      fakeCalls.sfzChannel = channel;
      if (fitToDisk && split) {
        return Promise.resolve(
          err("invalid-value", "fit to disk and the two disk split are alternatives; choose one"),
        );
      }
      const sfzNames = Object.keys(files).filter((n) => n.toLowerCase().endsWith(".sfz"));
      let sfz = sfzPath;
      if (sfz === "") {
        if (sfzNames.length === 0) {
          return Promise.resolve(err("no-sfz", "the folder holds no .sfz file"));
        }
        if (sfzNames.length > 1) {
          return Promise.resolve(
            err("invalid-value", `the folder holds ${sfzNames.length} .sfz files; name one`),
          );
        }
        sfz = sfzNames[0] ?? "";
      }
      const text = new TextDecoder().decode(files[sfz] ?? new Uint8Array());
      const referenced = [...text.matchAll(/sample=([^\s]+)/g)].map((m) => m[1] ?? "");
      for (const sample of referenced) {
        if (!(sample in files)) {
          return Promise.resolve(
            err("missing-samples", `region (${sample.split("/").pop() ?? sample}): file missing`),
          );
        }
      }
      const next = clone(state);
      next.label ??= "FIZZLE";
      next.missingDisk = 0;
      next.bytes2 = split ? new Uint8Array(IMAGE_SIZE) : null;
      const voices = referenced.map((sample, i) => ({
        slot: i,
        name: voiceName(sample),
        referenced: true,
      }));
      next.instrument = {
        fileName: "FULL-DATA-FZ",
        banks: [
          {
            name: "SFZ BANK",
            areas: voices.map((v, i) =>
              fakeArea(v.slot, v.name, { keyLow: 36 + i, keyHigh: 36 + i, root: 36 + i }),
            ),
          },
        ],
        voices,
      };
      next.files = [{ name: "FULL-DATA-FZ", type: "full", sizeBytes: 8192 }];
      const guard = missingGuard();
      if (guard) return Promise.resolve(guard);
      const snapshot = mutate(next);
      return Promise.resolve(ok({ snapshot, rate: fitToDisk ? 9000 : rate }));
    },

    importWavFolder(
      files: Record<string, Uint8Array>,
      rate: SampleRate,
      fitToDisk: boolean,
      channel: Channel,
    ): Promise<CoreResult<SFZImportResult>> {
      fakeCalls.wavFolderChannel = channel;
      const names = Object.keys(files)
        .filter((n) => n.toLowerCase().endsWith(".wav"))
        .sort();
      if (names.length === 0) {
        return Promise.resolve(err("invalid-sfz", "no WAV files found"));
      }
      const next = clone(state);
      next.label ??= "FIZZLE";
      next.missingDisk = 0;
      next.bytes2 = null;
      const voices = names.map((name, i) => ({ slot: i, name: voiceName(name), referenced: true }));
      next.instrument = {
        fileName: "FULL-DATA-FZ",
        banks: [
          {
            name: "WAV KIT",
            areas: voices.map((v, i) =>
              fakeArea(v.slot, v.name, { keyLow: 36 + i, keyHigh: 36 + i, root: 36 + i }),
            ),
          },
        ],
        voices,
      };
      next.files = [{ name: "FULL-DATA-FZ", type: "full", sizeBytes: 8192 }];
      const guard = missingGuard();
      if (guard) return Promise.resolve(guard);
      const snapshot = mutate(next);
      return Promise.resolve(ok({ snapshot, rate: fitToDisk ? 9000 : rate }));
    },

    setDebug(_debug: boolean): Promise<CoreResult<null>> {
      return Promise.resolve(ok(null));
    },

    setSlotParamNumber(slot: number, field: string, value: number) {
      const spec = FAKE_SCHEMA.find((f) => f.id === field);
      if (!spec || spec.kind === "select") {
        return Promise.resolve(err("invalid-field", `${field} is not a numeric schema field`));
      }
      const next = clone(state);
      const voice = next.instrument?.voices.find((v) => v.slot === slot);
      if (!voice?.params) {
        return Promise.resolve(err("invalid-value", `voice slot ${slot} out of range`));
      }
      voice.params[field] = Math.min(spec.max, Math.max(spec.min, value));
      return Promise.resolve(commit(next));
    },

    setSlotParamOption(slot: number, field: string, option: string) {
      const spec = FAKE_SCHEMA.find((f) => f.id === field);
      if (!spec || spec.kind !== "select") {
        return Promise.resolve(err("invalid-field", `${field} is not a select schema field`));
      }
      if (!spec.options?.includes(option)) {
        return Promise.resolve(err("invalid-value", `unknown option ${option}`));
      }
      const next = clone(state);
      const voice = next.instrument?.voices.find((v) => v.slot === slot);
      if (!voice?.params) {
        return Promise.resolve(err("invalid-value", `voice slot ${slot} out of range`));
      }
      voice.params[field] = option;
      return Promise.resolve(commit(next));
    },

    setSlotLoop(slot: number, index: number, start: number, end: number) {
      const next = clone(state);
      const detail = next.instrument?.voices.find((v) => v.slot === slot)?.voice;
      const loop = detail?.loops[index];
      if (!detail || !loop) {
        return Promise.resolve(err("invalid-value", "no such slot or loop"));
      }
      loop.start = clampNum(start, 0, detail.frames - 1);
      loop.end = clampNum(end, loop.start + 1, detail.frames);
      return Promise.resolve(commit(next));
    },

    setSlotGeneration(slot: number, start: number, end: number) {
      const next = clone(state);
      const detail = next.instrument?.voices.find((v) => v.slot === slot)?.voice;
      if (!detail) {
        return Promise.resolve(err("invalid-value", `voice slot ${slot} out of range`));
      }
      const [lo, hi] = clampGeneration(detail.frames, start, end);
      detail.genStart = lo;
      detail.genEnd = hi;
      return Promise.resolve(commit(next));
    },

    setSlotLoopAttr(slot: number, index: number, xf: number, tm: number) {
      const next = clone(state);
      const loop = next.instrument?.voices.find((v) => v.slot === slot)?.voice?.loops[index];
      if (!loop) {
        return Promise.resolve(err("invalid-value", "no such slot or loop"));
      }
      loop.xf = clampNum(xf, 0, 1023);
      loop.tm = clampNum(tm, 0, 1022);
      return Promise.resolve(commit(next));
    },

    setSampleMemory(bytes: number) {
      if (bytes < 1024 * 1024 || bytes > 2 * 1024 * 1024) {
        return Promise.resolve(
          err(
            "invalid-value",
            `sample memory ${String(bytes)} is outside the 1 MB to 2 MB an FZ holds`,
          ),
        );
      }
      // The machine is not the document: no revision, no history.
      memoryBytes = bytes;
      return Promise.resolve(ok(snap()));
    },

    setSlotLoopSelect(slot: number, sustain: number, release: number) {
      const next = clone(state);
      const detail = next.instrument?.voices.find((v) => v.slot === slot)?.voice;
      if (!detail) {
        return Promise.resolve(err("invalid-value", `voice slot ${slot} out of range`));
      }
      detail.loopSustain = clampNum(sustain, 0, 8);
      detail.loopRelease = clampNum(release, 0, 8);
      return Promise.resolve(commit(next));
    },

    setSlotEnvelope(
      slot: number,
      which: "dca" | "dcf",
      sustain: number,
      end: number,
      rates: number[],
      stops: number[],
    ) {
      if (rates.length !== 8 || stops.length !== 8) {
        return Promise.resolve(err("invalid-value", "envelopes carry 8 stages"));
      }
      const next = clone(state);
      const detail = next.instrument?.voices.find((v) => v.slot === slot)?.voice;
      if (!detail) {
        return Promise.resolve(err("invalid-value", `voice slot ${slot} out of range`));
      }
      detail[which] = {
        sustain: clampNum(sustain, 0, 7),
        end: clampNum(end, 0, 7),
        rates: rates.map((v) => clampNum(v, 0, 99)),
        stops: stops.map((v) => clampNum(v, 0, 99)),
      };
      return Promise.resolve(commit(next));
    },

    renameVoiceSlot(slot: number, name: string) {
      if (name.length === 0 || name.length > 12) {
        return Promise.resolve(err("invalid-value", "voice name must be 1 to 12 characters"));
      }
      const next = clone(state);
      const voice = next.instrument?.voices.find((v) => v.slot === slot);
      if (!next.instrument || !voice) {
        return Promise.resolve(err("invalid-value", `voice slot ${slot} out of range`));
      }
      voice.name = name;
      for (const bank of next.instrument.banks) {
        for (const area of bank.areas) {
          if (area.voiceSlot === slot) area.voiceName = name;
        }
      }
      return Promise.resolve(commit(next));
    },

    slotPeaks(
      slot: number,
      startFrame: number,
      endFrame: number,
      buckets: number,
    ): Promise<CoreResult<Int16Array>> {
      const detail = state.instrument?.voices.find((v) => v.slot === slot)?.voice;
      if (!detail) {
        return Promise.resolve(err("invalid-value", `voice slot ${slot} out of range`));
      }
      if (buckets <= 0) {
        return Promise.resolve(err("invalid-value", "buckets must be positive"));
      }
      const out = new Int16Array(buckets * 2);
      const span = Math.max(1, endFrame - startFrame);
      for (let b = 0; b < buckets; b++) {
        const frame = startFrame + (b * span) / buckets;
        const amp = Math.round(
          20000 * Math.exp(-frame / detail.frames) * Math.abs(Math.sin(frame / 40 + slot)),
        );
        out[b * 2] = -amp;
        out[b * 2 + 1] = amp;
      }
      return Promise.resolve(ok(out));
    },

    closeDisk() {
      past = [];
      future = [];
      state = emptyState();
      revision += 1;
      return Promise.resolve(ok(snap()));
    },

    renameDisk(label: string) {
      if (state.label === null) {
        return Promise.resolve(err("no-disk", "no disk is open"));
      }
      if (label.length === 0 || label.length > MAX_LABEL_LENGTH) {
        return Promise.resolve(
          err("invalid-value", `disk label must be 1 to ${MAX_LABEL_LENGTH} characters`),
        );
      }
      const next = clone(state);
      next.label = label;
      return Promise.resolve(commit(next));
    },

    deleteFile(name: string) {
      if (state.label === null) {
        return Promise.resolve(err("no-disk", "no disk is open"));
      }
      const next = clone(state);
      const index = next.files.findIndex((f) => f.name === name);
      if (index < 0) {
        return Promise.resolve(err("not-found", `no file named ${name}`));
      }
      const [removed] = next.files.splice(index, 1);
      next.used -= removed?.sizeBytes ?? 0;
      if (name === "FULL-DATA-FZ") {
        next.instrument = null;
        next.bytes2 = null;
      }
      return Promise.resolve(commit(next));
    },

    newInstrument(name: string) {
      if (state.instrument) {
        return Promise.resolve(err("instrument-exists", "the disk already has an instrument"));
      }
      if (/[^ -~]/.test(name)) {
        return Promise.resolve(
          err("invalid-value", "instrument name contains a non-ASCII character"),
        );
      }
      const next = clone(state);
      next.label ??= "FIZZLE";
      next.instrument = {
        fileName: "FULL-DATA-FZ",
        banks: [{ name: name === "" ? "NEW INST" : name.slice(0, 12), areas: [] }],
        voices: [],
      };
      next.files = [
        ...next.files.filter((f) => f.name !== "FULL-DATA-FZ"),
        { name: "FULL-DATA-FZ", type: "full", sizeBytes: 2048 },
      ];
      return Promise.resolve(commit(next));
    },

    extractFile(name: string): Promise<CoreResult<Uint8Array>> {
      const file = state.files.find((f) => f.name === name);
      if (!file) {
        return Promise.resolve(err("not-found", `no file named ${name}`));
      }
      return Promise.resolve(ok(new Uint8Array(Math.min(file.sizeBytes, 4096)).fill(7)));
    },

    extractVoiceSlot(
      slot: number,
      format: "fzv" | "wav",
    ): Promise<CoreResult<{ name: string; bytes: Uint8Array }>> {
      const voice = state.instrument?.voices.find((v) => v.slot === slot);
      if (!voice) {
        return Promise.resolve(err("invalid-value", `voice slot ${slot} out of range`));
      }
      const bytes = new Uint8Array(1024).fill(format === "wav" ? 1 : 2);
      return Promise.resolve(ok({ name: voice.name, bytes }));
    },
  };
}

/** How many voices the document holds, instrument or none. */
function voiceCount(s: FakeState): number {
  return s.instrument?.voices.length ?? 0;
}

/**
 * A header scan for the shape the estimate needs: channels, source
 * rate, and frame count, all from the declared sizes. Mirrors the
 * core's read of the same fields; null for anything unparseable.
 */
function wavShape(bytes: Uint8Array): { channels: number; rate: number; frames: number } | null {
  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const tag = (at: number) =>
    bytes.length >= at + 4 ? String.fromCharCode(...bytes.subarray(at, at + 4)) : "";
  if (tag(0) !== "RIFF" || tag(8) !== "WAVE") return null;
  let at = 12;
  let channels = 0;
  let rate = 0;
  let dataBytes = -1;
  while (at + 8 <= bytes.length) {
    const size = dv.getUint32(at + 4, true);
    if (tag(at) === "fmt " && at + 26 <= bytes.length) {
      channels = dv.getUint16(at + 10, true);
      rate = dv.getUint32(at + 12, true);
    }
    if (tag(at) === "data") dataBytes = size;
    at += 8 + size + (size % 2);
  }
  if (channels < 1 || rate < 1 || dataBytes < 0) return null;
  return { channels, rate, frames: Math.floor(dataBytes / (2 * channels)) };
}

/** Mono audio bytes a file becomes at the target rate. */
function convertedBytes(shape: { rate: number; frames: number }, rate: number): number {
  return Math.round((shape.frames * rate) / shape.rate) * 2;
}

/**
 * Lands a voice the way the core does: appended to the voice list and
 * mapped to a fresh area (membership is reference on the FZ format),
 * creating the instrument when the document has none.
 */
function joinVoice(
  next: FakeState,
  name: string,
  audio?: { frames: number; rate: number },
): FakeState {
  // The core's imported voice carries its own audio, and the memory
  // reading is the sum of it, so a voice landed without detail here
  // would let a test pin a figure the product never reports.
  const detail = audio
    ? { ...defaultVoiceDetail(audio.frames), sampleRate: audio.rate }
    : undefined;
  if (!next.instrument) {
    next.instrument = {
      fileName: "FULL-DATA-FZ",
      banks: [{ name: "BANK A", areas: [fakeArea(0, name)] }],
      voices: [{ slot: 0, name, referenced: true, ...(detail ? { voice: detail } : {}) }],
    };
    next.files = [{ name: "FULL-DATA-FZ", type: "full", sizeBytes: 4096 }];
    return next;
  }
  const slot = next.instrument.voices.length;
  next.instrument.voices.push({
    slot,
    name,
    referenced: true,
    ...(detail ? { voice: detail } : {}),
  });
  const bank = next.instrument.banks[0];
  if (bank) bank.areas.push(fakeArea(slot, name));
  refreshReferenced(next.instrument);
  return next;
}
