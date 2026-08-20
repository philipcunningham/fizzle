/** One FZ floppy image in bytes. */
export const IMAGE_SIZE = 1_310_720;

/** The FZ directory caps disk labels at 12 characters. */
export const MAX_LABEL_LENGTH = 12;

export interface CoreError {
  code: string;
  message: string;
  /**
   * The technical reason, for diagnostics and for a bug report. The
   * user reads the message above, so that one can stay plain.
   */
  detail?: string;
  /** The offending file, voice, or field, where one exists. */
  item?: string;
  /** Set when the core can no longer answer; only a reload moves on (E5). */
  fatal?: boolean;
}

export type CoreResult<T> = { ok: true; value: T } | { ok: false; error: CoreError };

export function ok<T>(value: T): CoreResult<T> {
  return { ok: true, value };
}

export function err<T>(code: string, message: string, detail?: string): CoreResult<T> {
  // Spelled out rather than assigned: exact optional properties reject
  // a detail whose value is undefined.
  return { ok: false, error: detail === undefined ? { code, message } : { code, message, detail } };
}

/** The machine code every fatal envelope carries. */
export const CORE_UNAVAILABLE = "core-unavailable";

/** The machine code for a call the boundary couldn't deliver or read. */
export const CALL_FAILED = "call-failed";

/**
 * What the user reads when the core is gone (E5). It says what happened
 * and what to do, and it names no codes.
 */
export const CORE_CRASH_MESSAGE =
  "The core stopped responding, so the editor can't reach your document. Reload the page to start again.";

/** A core that can no longer answer; the reason rides in detail (E5). */
export function coreCrashError(detail: string): CoreError {
  return { code: CORE_UNAVAILABLE, message: CORE_CRASH_MESSAGE, detail, fatal: true };
}

/** The same crash as an envelope, for a call that has to answer one. */
export function coreCrash<T>(detail: string): CoreResult<T> {
  return { ok: false, error: coreCrashError(detail) };
}

/**
 * True when the error means the core is gone. The surface showing it
 * offers a reload rather than a retry, and shows the message alone.
 */
export function isCoreCrash(error: CoreError): boolean {
  return error.fatal === true;
}

/** Monotonic per-session token; a changed revision means changed state. */
export type Revision = number;

/** FZ sample rates in Hz. */
export type SampleRate = 36000 | 18000 | 9000;

export const SAMPLE_RATES: readonly SampleRate[] = [36000, 18000, 9000];

/** How a stereo WAV becomes the mono signal the FZ stores. */
export type Channel = "left" | "right" | "mix";

export const CHANNELS: readonly Channel[] = ["left", "right", "mix"];

export interface LoopSnapshot {
  start: number;
  end: number;
  xf: number;
  tm: number;
}

/** Rates and stops speak the hardware display scale (0 to 99). */
export interface EnvelopeSnapshot {
  sustain: number;
  end: number;
  rates: number[];
  stops: number[];
}

/**
 * The bespoke-editor surface of a voice: waveform extent, the
 * generation window, loops, envelopes. Positions are voice-relative
 * sample frames. genStart and genEnd are R14's generation window; they
 * sit outside the schema because their bounds are this voice's own
 * frame count, while a schema field declares one range for all voices.
 */
export interface VoiceDetail {
  frames: number;
  sampleRate: number;
  genStart: number;
  genEnd: number;
  loopSustain: number;
  loopRelease: number;
  loops: LoopSnapshot[];
  dca: EnvelopeSnapshot;
  dcf: EnvelopeSnapshot;
}

export interface FileSnapshot {
  name: string;
  type: string;
  sizeBytes: number;
  /** Voice files carry editable values keyed by schema field id. */
  params?: Record<string, number | string>;
  voice?: VoiceDetail;
}

/**
 * One editable parameter, declared by the core (R14). The UI renders a
 * control from this and nothing else; kinds it doesn't know degrade to
 * a labelled numeric input.
 */
export interface SchemaField {
  id: string;
  label: string;
  group: string;
  kind: string;
  min: number;
  max: number;
  options?: string[];
}

/** One key split in a bank: the fields R12 makes editable. */
export interface AreaSnapshot {
  voiceSlot: number;
  voiceName: string;
  keyLow: number;
  keyHigh: number;
  root: number;
  velLow: number;
  velHigh: number;
  /** Display scale, 1 to 16. */
  midiChannel: number;
  /** Raw gchn bitmask; outputLabel renders it. */
  output: number;
  outputLabel: string;
  volume: number;
}

export interface BankSnapshot {
  name: string;
  areas: AreaSnapshot[];
}

export interface InstrumentVoice {
  slot: number;
  name: string;
  referenced: boolean;
  /** Editable values keyed by schema field id, like a voice file's. */
  params?: Record<string, number | string>;
  /** Loops and envelopes; loop positions are voice-relative frames. */
  voice?: VoiceDetail;
  /** The voice's audio belongs to an earlier slot (a velocity switch clone). */
  sharesAudio?: boolean;
  /**
   * Identifies the samples this slot plays. It changes only when the
   * audio does, so decoded PCM and peaks cache against it instead of
   * re-fetching on every parameter edit.
   */
  audioKey?: string;
}

/** One voice's decoded audio for the preview path. */
export interface AuditionData {
  sampleRate: number;
  root: number;
  pcm: Int16Array;
}

/** Rows: mod wheel, foot pedal, aftertouch. Columns per R19. */
export interface EffectsSnapshot {
  bendRange: number;
  matrix: number[][];
}

export const EFFECT_CONTROLLERS = ["Mod wheel", "Foot pedal", "Aftertouch"] as const;
export const EFFECT_TARGETS = [
  "LFO pitch",
  "LFO amp",
  "LFO filter",
  "LFO res",
  "DCA",
  "DCF",
  "DCQ",
] as const;

/** The disk's full dump as the UI edits it (R11 to R13). */
export interface InstrumentSnapshot {
  fileName: string;
  banks: BankSnapshot[];
  voices: InstrumentVoice[];
  effects?: EffectsSnapshot;
}

export interface DiskSnapshot {
  label: string;
  usedBytes: number;
  /** What the instrument asks the sampler's memory to hold (R27). */
  audioBytes: number;
  /** What the user says their sampler has; 1 MB until they say. */
  memoryBytes: number;
  /** Covers the whole document: two disks double it (R23). */
  capacityBytes: number;
  /** 1, or 2 when a split instrument spans an image pair (R5). */
  disks: number;
  /** Set when one half of a pair was opened alone: the absent disk number. */
  missingDisk?: number;
  files: FileSnapshot[];
  instrument?: InstrumentSnapshot;
}

export interface Snapshot {
  revision: Revision;
  disk: DiskSnapshot | null;
  canUndo: boolean;
  canRedo: boolean;
  /**
   * Set by commitGesture: whether the gesture landed a history entry. A
   * press and release with no movement lands none, so it must not mark
   * the document dirty.
   */
  gestureLanded?: boolean;
}

/**
 * The coarse boundary. Every method resolves to a CoreResult and never
 * rejects, so a panic in the core can't take the UI down with it.
 */
export interface Core {
  snapshot(): Promise<CoreResult<Snapshot>>;
  newDisk(label: string): Promise<CoreResult<Snapshot>>;
  openImage(bytes: Uint8Array): Promise<CoreResult<Snapshot>>;
  schema(): Promise<CoreResult<SchemaField[]>>;
  undo(): Promise<CoreResult<Snapshot>>;
  redo(): Promise<CoreResult<Snapshot>>;
  beginGesture(): Promise<CoreResult<Snapshot>>;
  commitGesture(): Promise<CoreResult<Snapshot>>;
  setAreaField(
    bank: number,
    area: number,
    field: string,
    value: number,
  ): Promise<CoreResult<Snapshot>>;
  renameBank(bank: number, name: string): Promise<CoreResult<Snapshot>>;
  swapAreas(bank: number, a: number, b: number): Promise<CoreResult<Snapshot>>;
  deleteArea(bank: number, area: number): Promise<CoreResult<Snapshot>>;
  addArea(bank: number, voiceSlot: number): Promise<CoreResult<Snapshot>>;
  duplicateArea(bank: number, area: number): Promise<CoreResult<Snapshot>>;
  mapVoice(voiceSlot: number): Promise<CoreResult<Snapshot>>;
  setEffectCell(controller: number, target: number, value: number): Promise<CoreResult<Snapshot>>;
  setBendRange(value: number): Promise<CoreResult<Snapshot>>;
  auditionSlot(slot: number): Promise<CoreResult<AuditionData>>;
  exportImage(): Promise<CoreResult<Uint8Array>>;
  /** One image of the document: 0 is disk 1, 1 is disk 2 of a pair (R25). */
  exportImageAt(index: number): Promise<CoreResult<Uint8Array>>;
  /** Placement matrix (R7): a full dump becomes or replaces the instrument. */
  loadFzf(bytes: Uint8Array): Promise<CoreResult<Snapshot>>;
  /** Placement matrix (R7): a voice joins the instrument, mapped to a fresh area. */
  addVoice(bytes: Uint8Array): Promise<CoreResult<Snapshot>>;
  /** Placement matrix (R7): a bank dump joins at the given bank slot. */
  addBank(bytes: Uint8Array, slot: number): Promise<CoreResult<Snapshot>>;
  /** WAV conversion that grows (or creates) the instrument (R7, R8). */
  importWavToInstrument(
    filename: string,
    bytes: Uint8Array,
    rate: SampleRate,
    channel: Channel,
  ): Promise<CoreResult<Snapshot>>;
  /** Both images of a split pair, in either order (R5). */
  openImagePair(a: Uint8Array, b: Uint8Array): Promise<CoreResult<Snapshot>>;
  /**
   * SFZ conversion (R9): fit to disk or the two disk split. One channel
   * answer covers the instrument, deciding which side of every stereo
   * sample the SFZ references the FZ keeps.
   */
  importSfz(
    files: Record<string, Uint8Array>,
    sfzPath: string,
    rate: SampleRate,
    fitToDisk: boolean,
    split: boolean,
    channel: Channel,
  ): Promise<CoreResult<SFZImportResult>>;
  /**
   * A folder of WAVs maps sequentially up the keyboard (R8). One channel
   * answer covers the batch, deciding which side of every stereo file
   * the FZ keeps.
   */
  importWavFolder(
    files: Record<string, Uint8Array>,
    rate: SampleRate,
    fitToDisk: boolean,
    channel: Channel,
  ): Promise<CoreResult<SFZImportResult>>;
  /**
   * Pre-flight for the WAV import dialog: what the batch becomes at the
   * rate, and whether it lands (R6). Re-queried on every rate or channel
   * change, so implementations copy the caller's buffers rather than
   * transfer them.
   */
  estimateImport(
    files: Record<string, Uint8Array>,
    rate: SampleRate,
    channel: Channel,
  ): Promise<CoreResult<ImportEstimate>>;
  /** Core log verbosity to the console: the CLI debug flag's analogue (E4). */
  setDebug(debug: boolean): Promise<CoreResult<null>>;
  /** Slot-addressed voice editing: the instrument's voices (R14). */
  setSlotParamNumber(slot: number, field: string, value: number): Promise<CoreResult<Snapshot>>;
  setSlotParamOption(slot: number, field: string, option: string): Promise<CoreResult<Snapshot>>;
  setSlotLoop(
    slot: number,
    index: number,
    start: number,
    end: number,
  ): Promise<CoreResult<Snapshot>>;
  /**
   * R14's generation window on an instrument voice slot, in
   * voice-relative frames clamped to that slot's own frame count.
   */
  setSlotGeneration(slot: number, start: number, end: number): Promise<CoreResult<Snapshot>>;
  /** Loop cross-fade (0..1023) and multi-loop time (0..1022). */
  setSlotLoopAttr(
    slot: number,
    index: number,
    xf: number,
    tm: number,
  ): Promise<CoreResult<Snapshot>>;
  setSlotLoopSelect(slot: number, sustain: number, release: number): Promise<CoreResult<Snapshot>>;
  /** Declares the sampler's sample memory in bytes, 1 MB to 2 MB. */
  setSampleMemory(bytes: number): Promise<CoreResult<Snapshot>>;
  setSlotEnvelope(
    slot: number,
    which: "dca" | "dcf",
    sustain: number,
    end: number,
    rates: number[],
    stops: number[],
  ): Promise<CoreResult<Snapshot>>;
  renameVoiceSlot(slot: number, name: string): Promise<CoreResult<Snapshot>>;
  /** Interleaved min/max pairs for a slot's frame window (R17). */
  slotPeaks(
    slot: number,
    startFrame: number,
    endFrame: number,
    buckets: number,
  ): Promise<CoreResult<Int16Array>>;
  /** Disk label rename after open. */
  renameDisk(label: string): Promise<CoreResult<Snapshot>>;
  /** Discard the document and its history; nothing is written (N3). */
  closeDisk(): Promise<CoreResult<Snapshot>>;
  /** Directory file delete; deleting the dump deletes the instrument. */
  deleteFile(name: string): Promise<CoreResult<Snapshot>>;
  /** A new empty instrument on the open or a fresh disk (R4). */
  newInstrument(name: string): Promise<CoreResult<Snapshot>>;
  /** A copy of a directory file's bytes; a split dump comes stitched. */
  extractFile(name: string): Promise<CoreResult<Uint8Array>>;
  /** One instrument voice as .fzv or 16-bit mono .wav (R18). */
  extractVoiceSlot(slot: number, format: "fzv" | "wav"): Promise<CoreResult<ExtractedVoice>>;
}

/** An extracted voice: file bytes plus its name for the filename. */
export interface ExtractedVoice {
  name: string;
  bytes: Uint8Array;
}

/** A conversion result: the new document plus the rate actually used. */
export interface SFZImportResult {
  snapshot: Snapshot;
  rate: number;
}

/**
 * The import dialog's pre-flight answer, computed with the conversion's
 * own arithmetic. verdict names what the matching import call would do;
 * reason narrows a "wont-fit" to the constraint that bites first.
 */
export interface ImportEstimate {
  /** Document growth in bytes: audio plus the headers around it. */
  bytes: number;
  /** The batch's play time at the target rate. */
  seconds: number;
  /**
   * Play time the document still holds at the target rate within its
   * current disk count; a larger import may still land by splitting.
   */
  roomSeconds: number;
  /** What the instrument would ask the sampler to hold after this. */
  audioAfterBytes: number;
  /** What the user says their sampler has (R27). */
  memoryBytes: number;
  verdict: "fits" | "splits" | "wont-fit";
  reason: "sample-memory" | "disk-room" | "voice-limit" | "";
  /** At least one file carries a left and right to choose between. */
  anyStereo: boolean;
  /** First file over the sampler's memory at this rate, or empty. */
  overCapFile: string;
  /** That file's play time in seconds; 0 when no file is over. */
  fileSeconds: number;
  /** Longest play time the sampler's memory loads at this rate. */
  capSeconds: number;
  /** Rates at which the whole batch would be accepted. */
  fitsAtRates: number[];
}
