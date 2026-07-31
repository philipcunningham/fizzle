// The worker side of the core boundary: runs the Go WASM module and
// serialises calls onto it. One request in, one response out, matched
// by id; exported image bytes travel back as a transferable.
import { coreCrash, err } from "../boundary/contract";
import type { Channel, CoreResult, SchemaField, Snapshot } from "../boundary/contract";
import "./generated/wasm_exec.js";

interface FizzleCore {
  snapshot(): CoreResult<Snapshot>;
  newDisk(label: string): CoreResult<Snapshot>;
  openImage(bytes: Uint8Array): CoreResult<Snapshot>;
  importWav(
    filename: string,
    bytes: Uint8Array,
    rate: number,
    channel: string,
  ): CoreResult<Snapshot>;
  schema(): CoreResult<SchemaField[]>;
  setParamNumber(file: string, field: string, value: number): CoreResult<Snapshot>;
  setParamOption(file: string, field: string, option: string): CoreResult<Snapshot>;
  undo(): CoreResult<Snapshot>;
  redo(): CoreResult<Snapshot>;
  beginGesture(): CoreResult<Snapshot>;
  commitGesture(): CoreResult<Snapshot>;
  peaks(file: string, start: number, end: number, buckets: number): CoreResult<Uint8Array>;
  setLoop(file: string, index: number, start: number, end: number): CoreResult<Snapshot>;
  setGeneration(file: string, start: number, end: number): CoreResult<Snapshot>;
  setLoopSelect(file: string, sustain: number, release: number): CoreResult<Snapshot>;
  setEnvelope(
    file: string,
    which: string,
    sustain: number,
    end: number,
    rates: number[],
    stops: number[],
  ): CoreResult<Snapshot>;
  setAreaField(bank: number, area: number, field: string, value: number): CoreResult<Snapshot>;
  renameBank(bank: number, name: string): CoreResult<Snapshot>;
  swapAreas(bank: number, a: number, b: number): CoreResult<Snapshot>;
  deleteArea(bank: number, area: number): CoreResult<Snapshot>;
  addArea(bank: number, voiceSlot: number): CoreResult<Snapshot>;
  duplicateArea(bank: number, area: number): CoreResult<Snapshot>;
  mapVoice(voiceSlot: number): CoreResult<Snapshot>;
  setEffectCell(controller: number, target: number, value: number): CoreResult<Snapshot>;
  setBendRange(value: number): CoreResult<Snapshot>;
  auditionPCM(file: string): CoreResult<{ sampleRate: number; root: number; pcm: Uint8Array }>;
  auditionSlot(slot: number): CoreResult<{ sampleRate: number; root: number; pcm: Uint8Array }>;
  exportImage(): CoreResult<Uint8Array>;
  exportImageAt(index: number): CoreResult<Uint8Array>;
  loadFzf(bytes: Uint8Array): CoreResult<Snapshot>;
  addVoice(bytes: Uint8Array): CoreResult<Snapshot>;
  addBank(bytes: Uint8Array, slot: number): CoreResult<Snapshot>;
  importWavToInstrument(
    filename: string,
    bytes: Uint8Array,
    rate: number,
    channel: string,
  ): CoreResult<Snapshot>;
  openImagePair(a: Uint8Array, b: Uint8Array): CoreResult<Snapshot>;
  importSfz(
    files: Record<string, Uint8Array>,
    sfzPath: string,
    rate: number,
    fitToDisk: boolean,
    split: boolean,
    channel: string,
  ): CoreResult<{ snapshot: Snapshot; rate: number }>;
  importWavFolder(
    files: Record<string, Uint8Array>,
    rate: number,
    fitToDisk: boolean,
    channel: string,
  ): CoreResult<{ snapshot: Snapshot; rate: number }>;
  setDebug(debug: boolean): CoreResult<null>;
  setSlotParamNumber(slot: number, field: string, value: number): CoreResult<Snapshot>;
  setSlotParamOption(slot: number, field: string, option: string): CoreResult<Snapshot>;
  setSlotGeneration(slot: number, start: number, end: number): CoreResult<Snapshot>;
  setSlotLoop(slot: number, index: number, start: number, end: number): CoreResult<Snapshot>;
  setSlotLoopAttr(slot: number, index: number, xf: number, tm: number): CoreResult<Snapshot>;
  setSlotLoopSelect(slot: number, sustain: number, release: number): CoreResult<Snapshot>;
  setSlotEnvelope(
    slot: number,
    which: string,
    sustain: number,
    end: number,
    rates: number[],
    stops: number[],
  ): CoreResult<Snapshot>;
  renameVoiceSlot(slot: number, name: string): CoreResult<Snapshot>;
  slotPeaks(slot: number, start: number, end: number, buckets: number): CoreResult<Uint8Array>;
  renameDisk(label: string): CoreResult<Snapshot>;
  closeDisk(): CoreResult<Snapshot>;
  deleteFile(name: string): CoreResult<Snapshot>;
  newInstrument(name: string): CoreResult<Snapshot>;
  extractFile(name: string): CoreResult<Uint8Array>;
  extractVoiceSlot(slot: number, format: string): CoreResult<{ name: string; bytes: Uint8Array }>;
}

export type AreaArgs = number[];

export interface PeaksPayload {
  file: string;
  start: number;
  end: number;
  buckets: number;
}

export interface SetLoopPayload {
  file: string;
  index: number;
  start: number;
  end: number;
}

export interface SetGenerationPayload {
  file: string;
  start: number;
  end: number;
}

export interface SetLoopSelectPayload {
  file: string;
  sustain: number;
  release: number;
}

export interface SetEnvelopePayload {
  file: string;
  which: string;
  sustain: number;
  end: number;
  rates: number[];
  stops: number[];
}

export interface SetParamNumberPayload {
  file: string;
  field: string;
  value: number;
}

export interface SetParamOptionPayload {
  file: string;
  field: string;
  option: string;
}

export interface ImportWavPayload {
  filename: string;
  buffer: ArrayBuffer;
  rate: number;
  channel: string;
}

export interface BytesPayload {
  buffer: ArrayBuffer;
}

export interface AddBankPayload {
  buffer: ArrayBuffer;
  slot: number;
}

export interface PairPayload {
  a: ArrayBuffer;
  b: ArrayBuffer;
}

/**
 * Folder imports: path to buffer pairs plus the conversion options.
 * channel is the stereo answer the dialog collected; it covers every
 * file in the batch, on the SFZ route and the WAV route alike.
 */
export interface FolderPayload {
  files: Record<string, ArrayBuffer>;
  sfzPath: string;
  rate: number;
  fitToDisk: boolean;
  split: boolean;
  channel: Channel;
}

function folderFiles(files: Record<string, ArrayBuffer>): Record<string, Uint8Array> {
  const out: Record<string, Uint8Array> = {};
  for (const [name, buffer] of Object.entries(files)) {
    out[name] = new Uint8Array(buffer);
  }
  return out;
}

export interface WorkerRequest {
  id: number;
  method:
    | "snapshot"
    | "newDisk"
    | "openImage"
    | "importWav"
    | "schema"
    | "setParamNumber"
    | "setParamOption"
    | "undo"
    | "redo"
    | "beginGesture"
    | "commitGesture"
    | "peaks"
    | "setLoop"
    | "setGeneration"
    | "setLoopSelect"
    | "setEnvelope"
    | "setAreaField"
    | "renameBank"
    | "swapAreas"
    | "deleteArea"
    | "addArea"
    | "duplicateArea"
    | "mapVoice"
    | "setEffectCell"
    | "setBendRange"
    | "auditionPCM"
    | "auditionSlot"
    | "exportImage"
    | "exportImageAt"
    | "loadFzf"
    | "addVoice"
    | "addBank"
    | "importWavToInstrument"
    | "openImagePair"
    | "importSfz"
    | "importWavFolder"
    | "setDebug"
    | "setSlotParamNumber"
    | "setSlotParamOption"
    | "setSlotGeneration"
    | "setSlotLoop"
    | "setSlotLoopAttr"
    | "setSlotLoopSelect"
    | "setSlotEnvelope"
    | "renameVoiceSlot"
    | "slotPeaks"
    | "renameDisk"
    | "closeDisk"
    | "deleteFile"
    | "newInstrument"
    | "extractFile"
    | "extractVoiceSlot";
  payload?: unknown;
}

export interface SlotFieldPayload {
  slot: number;
  field: string;
  value: number;
}

export interface SlotOptionPayload {
  slot: number;
  field: string;
  option: string;
}

export interface SlotEnvelopePayload {
  slot: number;
  which: string;
  sustain: number;
  end: number;
  rates: number[];
  stops: number[];
}

export interface SlotNamePayload {
  slot: number;
  name: string;
}

export interface SlotExtractPayload {
  slot: number;
  format: string;
}

export interface WorkerResponse {
  id: number;
  result: CoreResult<unknown>;
}

// The Go module invokes onFizzleReady once fizzleCore is registered.
// A boot that never gets there (a missing or corrupt module) must fail
// every call with an envelope rather than parking it forever.
// Declared before the promise below: its executor runs at once, and it
// assigns this.
let bootFailed: (reason: Error) => void = () => undefined;

const ready = new Promise<void>((resolve, reject) => {
  (globalThis as { onFizzleReady?: () => void }).onFizzleReady = resolve;
  bootFailed = reject;
});
// A boot that fails before the first call arrives would otherwise be an
// unhandled rejection. The call itself still awaits `ready` and reports.
void ready.catch(() => undefined);

async function boot(): Promise<void> {
  const go = new Go();
  // Relative to the deployed base, not the server root: a project site
  // serves from a sub-path, and an absolute URL 404s the core there
  // while the shell loads fine.
  const response = await fetch(`${import.meta.env.BASE_URL}fizzle.wasm`);
  if (!response.ok) {
    throw new Error(`the core module did not load (HTTP ${String(response.status)})`);
  }
  let instance: WebAssembly.Instance;
  try {
    ({ instance } = await WebAssembly.instantiateStreaming(response.clone(), go.importObject));
  } catch {
    // Some servers mislabel the MIME type; fall back to a buffer.
    const buffer = await response.arrayBuffer();
    ({ instance } = await WebAssembly.instantiate(buffer, go.importObject));
  }
  // The Go program runs forever; if it returns, it aborted, and the
  // core is gone. An abort during runtime setup (a fizzle.wasm built
  // against a different wasm_exec.js, say) settles this before the
  // core ever signals ready, so fail the boot too: a call parked on
  // `ready` would otherwise wait for a core that will never answer.
  go.run(instance).then(
    () => {
      dead = new Error("the core stopped running");
      bootFailed(dead);
    },
    (reason: unknown) => {
      dead = reason instanceof Error ? reason : new Error(String(reason));
      bootFailed(dead);
    },
  );
}

// Set once the core can no longer answer.
let dead: Error | null = null;

boot().catch((reason: unknown) => {
  const error = reason instanceof Error ? reason : new Error(String(reason));
  dead = error;
  bootFailed(error);
});

function core(): FizzleCore {
  return (globalThis as unknown as { fizzleCore: FizzleCore }).fizzleCore;
}

// The return type admits undefined against the interface above: a Go
// fatal error unwinds the exported function, and the call to it then
// yields nothing at all. The caller checks.
function dispatch(request: WorkerRequest): CoreResult<unknown> | undefined {
  switch (request.method) {
    case "snapshot":
      return core().snapshot();
    case "newDisk":
      return core().newDisk(request.payload as string);
    case "openImage":
      return core().openImage(new Uint8Array(request.payload as ArrayBuffer));
    case "importWav": {
      const p = request.payload as ImportWavPayload;
      return core().importWav(p.filename, new Uint8Array(p.buffer), p.rate, p.channel);
    }
    case "schema":
      return core().schema();
    case "setParamNumber": {
      const p = request.payload as SetParamNumberPayload;
      return core().setParamNumber(p.file, p.field, p.value);
    }
    case "setParamOption": {
      const p = request.payload as SetParamOptionPayload;
      return core().setParamOption(p.file, p.field, p.option);
    }
    case "undo":
      return core().undo();
    case "redo":
      return core().redo();
    case "beginGesture":
      return core().beginGesture();
    case "commitGesture":
      return core().commitGesture();
    case "peaks": {
      const p = request.payload as PeaksPayload;
      return core().peaks(p.file, p.start, p.end, p.buckets);
    }
    case "setLoop": {
      const p = request.payload as SetLoopPayload;
      return core().setLoop(p.file, p.index, p.start, p.end);
    }
    case "setGeneration": {
      const p = request.payload as SetGenerationPayload;
      return core().setGeneration(p.file, p.start, p.end);
    }
    case "setLoopSelect": {
      const p = request.payload as SetLoopSelectPayload;
      return core().setLoopSelect(p.file, p.sustain, p.release);
    }
    case "setEnvelope": {
      const p = request.payload as SetEnvelopePayload;
      return core().setEnvelope(p.file, p.which, p.sustain, p.end, p.rates, p.stops);
    }
    case "setAreaField": {
      const p = request.payload as { bank: number; area: number; field: string; value: number };
      return core().setAreaField(p.bank, p.area, p.field, p.value);
    }
    case "renameBank": {
      const p = request.payload as { bank: number; name: string };
      return core().renameBank(p.bank, p.name);
    }
    case "swapAreas": {
      const p = request.payload as AreaArgs;
      return core().swapAreas(p[0] ?? 0, p[1] ?? 0, p[2] ?? 0);
    }
    case "deleteArea": {
      const p = request.payload as AreaArgs;
      return core().deleteArea(p[0] ?? 0, p[1] ?? 0);
    }
    case "addArea": {
      const p = request.payload as AreaArgs;
      return core().addArea(p[0] ?? 0, p[1] ?? 0);
    }
    case "duplicateArea": {
      const p = request.payload as AreaArgs;
      return core().duplicateArea(p[0] ?? 0, p[1] ?? 0);
    }
    case "mapVoice": {
      const p = request.payload as AreaArgs;
      return core().mapVoice(p[0] ?? 0);
    }
    case "setEffectCell": {
      const p = request.payload as AreaArgs;
      return core().setEffectCell(p[0] ?? 0, p[1] ?? 0, p[2] ?? 0);
    }
    case "setBendRange": {
      const p = request.payload as AreaArgs;
      return core().setBendRange(p[0] ?? 0);
    }
    case "auditionPCM":
      return core().auditionPCM(request.payload as string);
    case "auditionSlot": {
      const p = request.payload as AreaArgs;
      return core().auditionSlot(p[0] ?? 0);
    }
    case "exportImage":
      return core().exportImage();
    case "exportImageAt": {
      const p = request.payload as AreaArgs;
      return core().exportImageAt(p[0] ?? 0);
    }
    case "loadFzf": {
      const p = request.payload as BytesPayload;
      return core().loadFzf(new Uint8Array(p.buffer));
    }
    case "addVoice": {
      const p = request.payload as BytesPayload;
      return core().addVoice(new Uint8Array(p.buffer));
    }
    case "addBank": {
      const p = request.payload as AddBankPayload;
      return core().addBank(new Uint8Array(p.buffer), p.slot);
    }
    case "importWavToInstrument": {
      const p = request.payload as ImportWavPayload;
      return core().importWavToInstrument(p.filename, new Uint8Array(p.buffer), p.rate, p.channel);
    }
    case "openImagePair": {
      const p = request.payload as PairPayload;
      return core().openImagePair(new Uint8Array(p.a), new Uint8Array(p.b));
    }
    case "importSfz": {
      const p = request.payload as FolderPayload;
      return core().importSfz(
        folderFiles(p.files),
        p.sfzPath,
        p.rate,
        p.fitToDisk,
        p.split,
        p.channel,
      );
    }
    case "importWavFolder": {
      const p = request.payload as FolderPayload;
      return core().importWavFolder(folderFiles(p.files), p.rate, p.fitToDisk, p.channel);
    }
    case "setDebug":
      return core().setDebug(request.payload as boolean);
    case "setSlotParamNumber": {
      const p = request.payload as SlotFieldPayload;
      return core().setSlotParamNumber(p.slot, p.field, p.value);
    }
    case "setSlotParamOption": {
      const p = request.payload as SlotOptionPayload;
      return core().setSlotParamOption(p.slot, p.field, p.option);
    }
    case "setSlotGeneration": {
      const p = request.payload as AreaArgs;
      return core().setSlotGeneration(p[0] ?? 0, p[1] ?? 0, p[2] ?? 0);
    }
    case "setSlotLoop": {
      const p = request.payload as AreaArgs;
      return core().setSlotLoop(p[0] ?? 0, p[1] ?? 0, p[2] ?? 0, p[3] ?? 0);
    }
    case "setSlotLoopAttr": {
      const p = request.payload as AreaArgs;
      return core().setSlotLoopAttr(p[0] ?? 0, p[1] ?? 0, p[2] ?? 0, p[3] ?? 0);
    }
    case "setSlotLoopSelect": {
      const p = request.payload as AreaArgs;
      return core().setSlotLoopSelect(p[0] ?? 0, p[1] ?? 0, p[2] ?? 0);
    }
    case "setSlotEnvelope": {
      const p = request.payload as SlotEnvelopePayload;
      return core().setSlotEnvelope(p.slot, p.which, p.sustain, p.end, p.rates, p.stops);
    }
    case "renameVoiceSlot": {
      const p = request.payload as SlotNamePayload;
      return core().renameVoiceSlot(p.slot, p.name);
    }
    case "slotPeaks": {
      const p = request.payload as AreaArgs;
      return core().slotPeaks(p[0] ?? 0, p[1] ?? 0, p[2] ?? 0, p[3] ?? 1);
    }
    case "renameDisk":
      return core().renameDisk(request.payload as string);
    case "closeDisk":
      return core().closeDisk();
    case "deleteFile":
      return core().deleteFile(request.payload as string);
    case "newInstrument":
      return core().newInstrument(request.payload as string);
    case "extractFile":
      return core().extractFile(request.payload as string);
    case "extractVoiceSlot": {
      const p = request.payload as SlotExtractPayload;
      return core().extractVoiceSlot(p.slot, p.format);
    }
    default:
      // The union above rules this out; a stale bundle on one side of
      // the boundary doesn't. An unanswered call parks the caller for
      // the life of the page, so answer it.
      return err("unknown-method", `the core has no method named ${String(request.method)}`);
  }
}

self.onmessage = (event: MessageEvent<WorkerRequest>) => {
  void (async () => {
    const fatal = (reason: unknown): WorkerResponse => ({
      id: event.data.id,
      result: coreCrash(reason instanceof Error ? reason.message : String(reason)),
    });

    try {
      await ready;
    } catch (reason) {
      self.postMessage(fatal(reason));
      return;
    }
    if (dead) {
      self.postMessage(fatal(dead));
      return;
    }

    let result: CoreResult<unknown> | undefined;
    try {
      result = dispatch(event.data);
    } catch (reason) {
      // A Go abort (an exhausted heap on a huge import) takes the core
      // with it; the call reports rather than vanishing.
      dead ??= reason instanceof Error ? reason : new Error(String(reason));
      self.postMessage(fatal(dead));
      return;
    }
    if (!result) {
      // A Go fatal error unwinds through the exported function, which
      // then returns nothing. Reading the envelope below would throw
      // out of this handler, and the call would never be answered.
      dead ??= new Error("the core stopped part way through the call");
      self.postMessage(fatal(dead));
      return;
    }

    const response: WorkerResponse = { id: event.data.id, result };
    if (result.ok && result.value instanceof Uint8Array) {
      self.postMessage(response, { transfer: [result.value.buffer as ArrayBuffer] });
      return;
    }
    // Audition PCM and an extracted voice both travel inside an object;
    // transfer the buffer too, so megabytes of samples are moved rather
    // than copied.
    const inner = (result as { value?: { pcm?: Uint8Array; bytes?: Uint8Array } }).value;
    const buffer = inner?.pcm ?? inner?.bytes;
    if (result.ok && buffer instanceof Uint8Array) {
      self.postMessage(response, { transfer: [buffer.buffer as ArrayBuffer] });
      return;
    }
    self.postMessage(response);
  })();
};
