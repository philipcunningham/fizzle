// The main-thread side of the core boundary: implements the Core
// contract by RPC to the worker running the Go WASM module. Image
// bytes cross as transferables, so openImage detaches the caller's
// buffer rather than copying it.
import { CALL_FAILED, coreCrashError, err } from "../boundary/contract";
import type {
  AuditionData,
  Channel,
  Core,
  CoreError,
  CoreResult,
  ImportEstimate,
  SFZImportResult,
  SchemaField,
  Snapshot,
} from "../boundary/contract";
import type {
  AddBankPayload,
  BytesPayload,
  FolderPayload,
  ImportWavPayload,
  PairPayload,
  WorkerRequest,
  WorkerResponse,
} from "./worker";

export function createWasmCore(): Core {
  const worker = new Worker(new URL("./worker.ts", import.meta.url), { type: "module" });
  let nextId = 1;
  const pending = new Map<number, (result: CoreResult<unknown>) => void>();

  worker.onmessage = (event: MessageEvent<WorkerResponse>) => {
    const resolve = pending.get(event.data.id);
    if (!resolve) return;
    pending.delete(event.data.id);
    resolve(event.data.result);
  };

  // Set once the worker can no longer answer. Without it, a dead worker
  // takes the calls in flight with it and parks every later one.
  let dead: CoreError | null = null;

  function settleAll(result: CoreResult<never>) {
    for (const resolve of pending.values()) {
      resolve(result);
    }
    pending.clear();
  }

  worker.onerror = (event) => {
    // A worker script that throws at module scope arrives here and
    // never sends anything again, so latch it.
    dead = coreCrashError(event.message || "the core worker stopped");
    settleAll({ ok: false, error: dead });
  };

  worker.onmessageerror = () => {
    // A reply that fails structured clone arrives with no data, so no
    // id matches it: answer everything in flight rather than park it.
    // The worker still lives, so nothing latches.
    settleAll(err<never>(CALL_FAILED, "The core's reply couldn't be read. Try that action again."));
  };

  function call<T>(
    method: WorkerRequest["method"],
    payload?: unknown,
    transfer: Transferable[] = [],
  ): Promise<CoreResult<T>> {
    if (dead) return Promise.resolve<CoreResult<T>>({ ok: false, error: dead });
    return new Promise((resolve) => {
      const id = nextId++;
      // One entry per id; the response narrows back to T here only.
      pending.set(id, resolve as (result: CoreResult<unknown>) => void);
      const request: WorkerRequest = { id, method, payload };
      try {
        worker.postMessage(request, transfer);
      } catch (reason) {
        // Re-sending an already transferred buffer throws DataCloneError
        // here. Section 9 says every method resolves, so answer the call
        // and drop the entry it would otherwise leave behind.
        pending.delete(id);
        resolve(
          err<T>(
            CALL_FAILED,
            "The core didn't receive that request. Try that action again.",
            reason instanceof Error ? reason.message : String(reason),
          ),
        );
      }
    });
  }

  function auditionCall(
    method: WorkerRequest["method"],
    payload: unknown,
  ): Promise<CoreResult<AuditionData>> {
    return call<{
      sampleRate: number;
      root: number;
      pcm: { buffer: ArrayBuffer; byteOffset: number; byteLength: number };
    }>(method, payload).then((result) =>
      result.ok
        ? {
            ok: true as const,
            value: {
              sampleRate: result.value.sampleRate,
              root: result.value.root,
              pcm: new Int16Array(
                result.value.pcm.buffer,
                result.value.pcm.byteOffset,
                result.value.pcm.byteLength / 2,
              ),
            },
          }
        : result,
    );
  }

  return {
    snapshot: () => call<Snapshot>("snapshot"),
    newDisk: (label) => call<Snapshot>("newDisk", label),
    openImage: (bytes) => {
      const buffer = wholeBytes(bytes);
      return call<Snapshot>("openImage", buffer, [buffer]);
    },
    schema: () => call<SchemaField[]>("schema"),
    undo: () => call<Snapshot>("undo"),
    redo: () => call<Snapshot>("redo"),
    beginGesture: () => call<Snapshot>("beginGesture"),
    commitGesture: () => call<Snapshot>("commitGesture"),
    setAreaField: (bank, area, field, value) =>
      call<Snapshot>("setAreaField", { bank, area, field, value }),
    renameBank: (bank, name) => call<Snapshot>("renameBank", { bank, name }),
    swapAreas: (bank, a, b) => call<Snapshot>("swapAreas", [bank, a, b]),
    deleteArea: (bank, area) => call<Snapshot>("deleteArea", [bank, area]),
    addArea: (bank, voiceSlot) => call<Snapshot>("addArea", [bank, voiceSlot]),
    duplicateArea: (bank, area) => call<Snapshot>("duplicateArea", [bank, area]),
    mapVoice: (voiceSlot) => call<Snapshot>("mapVoice", [voiceSlot]),
    setEffectCell: (controller, target, value) =>
      call<Snapshot>("setEffectCell", [controller, target, value]),
    setBendRange: (value) => call<Snapshot>("setBendRange", [value]),
    auditionSlot: (slot) => auditionCall("auditionSlot", [slot]),
    exportImage: () => {
      return call<{ [index: number]: number; length: number }>("exportImage").then((result) =>
        result.ok ? { ok: true as const, value: coerceBytes(result.value) } : result,
      );
    },
    exportImageAt: (index) => {
      return call<{ [index: number]: number; length: number }>("exportImageAt", [index]).then(
        (result) => (result.ok ? { ok: true as const, value: coerceBytes(result.value) } : result),
      );
    },
    loadFzf: (bytes) => {
      const payload: BytesPayload = { buffer: wholeBytes(bytes) };
      return call<Snapshot>("loadFzf", payload, [payload.buffer]);
    },
    addVoice: (bytes) => {
      const payload: BytesPayload = { buffer: wholeBytes(bytes) };
      return call<Snapshot>("addVoice", payload, [payload.buffer]);
    },
    addBank: (bytes, slot) => {
      const payload: AddBankPayload = { buffer: wholeBytes(bytes), slot };
      return call<Snapshot>("addBank", payload, [payload.buffer]);
    },
    importWavToInstrument: (filename, bytes, rate, channel) => {
      // A copy crosses: the dialog keeps its bytes for the estimate and
      // for a retry, so the transfer must not detach the caller's.
      const payload: ImportWavPayload = {
        filename,
        buffer: bytes.slice().buffer,
        rate,
        channel,
      };
      return call<Snapshot>("importWavToInstrument", payload, [payload.buffer]);
    },
    openImagePair: (a, b) => {
      const payload: PairPayload = { a: wholeBytes(a), b: wholeBytes(b) };
      return call<Snapshot>("openImagePair", payload, [payload.a, payload.b]);
    },
    importSfz: (files, sfzPath, rate, fitToDisk, split, channel) => {
      const payload = folderPayload(files, sfzPath, rate, fitToDisk, split, channel);
      return call<SFZImportResult>("importSfz", payload, Object.values(payload.files));
    },
    importWavFolder: (files, rate, fitToDisk, channel) => {
      const payload = folderPayload(files, "", rate, fitToDisk, false, channel);
      return call<SFZImportResult>("importWavFolder", payload, Object.values(payload.files));
    },
    estimateImport: (files, rate, channel) => {
      const payload = folderPayload(files, "", rate, false, false, channel);
      // No transfer list: the buffers clone, staying usable for the
      // next radio change's re-estimate and the conversion itself.
      return call<ImportEstimate>("estimateImport", {
        files: payload.files,
        rate,
        channel,
      });
    },
    setDebug: (debug) => call<null>("setDebug", debug),
    setSlotParamNumber: (slot, field, value) =>
      call<Snapshot>("setSlotParamNumber", { slot, field, value }),
    setSlotParamOption: (slot, field, option) =>
      call<Snapshot>("setSlotParamOption", { slot, field, option }),
    setSlotGeneration: (slot, start, end) =>
      call<Snapshot>("setSlotGeneration", [slot, start, end]),
    setSlotLoop: (slot, index, start, end) =>
      call<Snapshot>("setSlotLoop", [slot, index, start, end]),
    setSlotLoopAttr: (slot, index, xf, tm) =>
      call<Snapshot>("setSlotLoopAttr", [slot, index, xf, tm]),
    setSlotLoopSelect: (slot, sustain, release) =>
      call<Snapshot>("setSlotLoopSelect", [slot, sustain, release]),
    setSampleMemory: (bytes) => call<Snapshot>("setSampleMemory", [bytes]),
    setSlotEnvelope: (slot, which, sustain, end, rates, stops) =>
      call<Snapshot>("setSlotEnvelope", { slot, which, sustain, end, rates, stops }),
    renameVoiceSlot: (slot, name) => call<Snapshot>("renameVoiceSlot", { slot, name }),
    slotPeaks: (slot, start, end, buckets) => {
      return call<{ buffer: ArrayBuffer; byteOffset: number; byteLength: number }>("slotPeaks", [
        slot,
        start,
        end,
        buckets,
      ]).then((result) =>
        result.ok
          ? {
              ok: true as const,
              value: new Int16Array(
                result.value.buffer,
                result.value.byteOffset,
                result.value.byteLength / 2,
              ),
            }
          : result,
      );
    },
    renameDisk: (label) => call<Snapshot>("renameDisk", label),
    closeDisk: () => call<Snapshot>("closeDisk"),
    deleteFile: (name) => call<Snapshot>("deleteFile", name),
    newInstrument: (name) => call<Snapshot>("newInstrument", name),
    extractFile: (name) => {
      return call<{ [index: number]: number; length: number }>("extractFile", name).then(
        (result) => (result.ok ? { ok: true as const, value: coerceBytes(result.value) } : result),
      );
    },
    extractVoiceSlot: (slot, format) => {
      return call<{ name: string; bytes: { [index: number]: number; length: number } }>(
        "extractVoiceSlot",
        { slot, format },
      ).then((result) =>
        result.ok
          ? {
              ok: true as const,
              value: { name: result.value.name, bytes: coerceBytes(result.value.bytes) },
            }
          : result,
      );
    },
  };
}

// Folder imports transfer a copy of every file's buffer: the transfer
// is for speed, and the caller's bytes stay usable for a retry.
function folderPayload(
  files: Record<string, Uint8Array>,
  sfzPath: string,
  rate: number,
  fitToDisk: boolean,
  split: boolean,
  channel: Channel,
): FolderPayload {
  const buffers: Record<string, ArrayBuffer> = {};
  for (const [name, bytes] of Object.entries(files)) {
    buffers[name] = bytes.slice().buffer;
  }
  return { files: buffers, sfzPath, rate, fitToDisk, split, channel };
}

// A view narrower than its backing buffer sends only its own bytes:
// posting the raw buffer ships unrelated data and the wrong length.
function wholeBytes(bytes: Uint8Array): ArrayBuffer {
  return bytes.byteOffset === 0 && bytes.byteLength === bytes.buffer.byteLength
    ? (bytes.buffer as ArrayBuffer)
    : bytes.slice().buffer;
}

// Structured clone hands the transferred buffer back as a Uint8Array,
// but only if the worker sent one; coerce defensively.
function coerceBytes(value: { [index: number]: number; length: number }): Uint8Array {
  return value instanceof Uint8Array
    ? value
    : Uint8Array.from({ length: value.length }, (_, i) => value[i] ?? 0);
}
