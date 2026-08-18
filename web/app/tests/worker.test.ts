// The boundary must never leave a call unsettled. Section 9 promises
// that every method resolves an envelope and that none of them reject,
// so these tests hold both halves to it. The worker answers a call it
// can't dispatch, and the main thread answers calls made after the
// worker dies, calls it can't post, and replies it can't read.
import { afterEach, describe, expect, it, vi } from "vitest";
import { isCoreCrash } from "../src/boundary/contract";
import { createWasmCore } from "../src/core/wasm";
import type { Core } from "../src/boundary/contract";
import type { ImportWavPayload, WorkerRequest, WorkerResponse } from "../src/core/worker";

const UNSETTLED = "unsettled";

// A hung call is the defect under test, so race every promise against a
// deadline and let the assertion read the sentinel.
async function settle<T>(promise: Promise<T>): Promise<T | typeof UNSETTLED> {
  return await Promise.race([
    promise,
    new Promise<typeof UNSETTLED>((resolve) => {
      setTimeout(() => {
        resolve(UNSETTLED);
      }, 50);
    }),
  ]);
}

type Handler<E> = ((event: E) => void) | null;

// Stands in for the Worker the main thread half constructs. It records
// what it was sent and can be told to throw from postMessage, which is
// what a detached buffer does in the browser.
class FakeWorker {
  static last: FakeWorker | null = null;
  onmessage: Handler<MessageEvent<WorkerResponse>> = null;
  onerror: Handler<ErrorEvent> = null;
  onmessageerror: Handler<MessageEvent<unknown>> = null;
  readonly sent: WorkerRequest[] = [];
  readonly transfers: Transferable[][] = [];
  postThrows: Error | null = null;

  constructor() {
    FakeWorker.last = this;
  }

  postMessage(request: WorkerRequest, transfer: Transferable[] = []): void {
    if (this.postThrows) throw this.postThrows;
    this.sent.push(request);
    this.transfers.push(transfer);
  }
}

function newCore(): Core {
  FakeWorker.last = null;
  vi.stubGlobal("Worker", FakeWorker);
  return createWasmCore();
}

function activeWorker(): FakeWorker {
  const worker = FakeWorker.last;
  if (!worker) throw new Error("createWasmCore constructed no worker");
  return worker;
}

function lastRequest(worker: FakeWorker): WorkerRequest {
  const request = worker.sent.at(-1);
  if (!request) throw new Error("nothing reached the worker");
  return request;
}

function reply(worker: FakeWorker, response: WorkerResponse): void {
  worker.onmessage?.(new MessageEvent<WorkerResponse>("message", { data: response }));
}

describe("core boundary, main thread half", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("answers a call made after the worker dies", async () => {
    const core = newCore();
    const worker = activeWorker();

    // A worker script that throws at module scope reaches the page as
    // an error event; nothing else ever arrives from it.
    worker.onerror?.(new ErrorEvent("error", { message: "boom at module scope" }));

    const result = await settle(core.snapshot());
    expect(result).not.toBe(UNSETTLED);
    expect(result).toMatchObject({ ok: false, error: { code: "core-unavailable" } });
  });

  it("explains a core crash in plain words and marks it for reload", async () => {
    const core = newCore();
    const worker = activeWorker();
    worker.onerror?.(new ErrorEvent("error", { message: "boom at module scope" }));

    const result = await settle(core.snapshot());
    if (result === UNSETTLED || result.ok) throw new Error("expected a failure envelope");
    expect(isCoreCrash(result.error)).toBe(true);
    expect(result.error.message).toMatch(/reload/i);
    expect(result.error.message).not.toContain(result.error.code);
    expect(result.error.detail).toContain("boom at module scope");
  });

  it("answers the calls in flight when a reply can't be read", async () => {
    const core = newCore();
    const worker = activeWorker();
    const pending = core.snapshot();

    // A reply that fails structured clone arrives with no data, so no
    // id matches it.
    worker.onmessageerror?.(new MessageEvent<unknown>("messageerror", { data: null }));

    const result = await settle(pending);
    expect(result).not.toBe(UNSETTLED);
    expect(result).toMatchObject({ ok: false, error: { code: "call-failed" } });
  });

  it("resolves an envelope when the request can't be posted", async () => {
    const core = newCore();
    const worker = activeWorker();

    // Re-sending an already transferred buffer, which is what a retry
    // after a failed open does.
    worker.postThrows = new Error("ArrayBuffer at index 0 is already detached");

    const result = await settle(core.openImage(new Uint8Array(4)));
    expect(result).not.toBe(UNSETTLED);
    expect(result).toMatchObject({ ok: false, error: { code: "call-failed" } });

    // A request that never left isn't a dead core, so the next call
    // still reaches the worker and still gets its answer.
    worker.postThrows = null;
    const next = core.snapshot();
    reply(worker, { id: lastRequest(worker).id, result: { ok: true, value: null } });
    await expect(settle(next)).resolves.toMatchObject({ ok: true });
  });

  // R14's generation window is a bespoke call, like the loops, so the
  // boundary has to carry it explicitly on both halves.
  it("posts the generation window with the frames it was given", () => {
    const core = newCore();
    const worker = activeWorker();

    void core.setSlotGeneration(2, 100, 900);
    expect(lastRequest(worker)).toMatchObject({
      method: "setSlotGeneration",
      payload: [2, 100, 900],
    });
  });

  // A subarray view must send only its own bytes: posting the backing
  // buffer would ship unrelated data and the wrong image length.
  it("normalises subarray views to their own bytes", () => {
    const core = newCore();
    const worker = activeWorker();
    const backing = new Uint8Array(64);
    const view = backing.subarray(8, 24);

    void core.openImage(view);
    const posted = lastRequest(worker).payload as ArrayBuffer;
    expect(posted.byteLength).toBe(16);
  });

  // The dialog re-estimates on every radio change and then converts
  // the same bytes, so the estimate must copy its buffers across the
  // boundary; a transfer would detach them and starve the next call.
  it("posts the estimate without transferring the file buffers", () => {
    const core = newCore();
    const worker = activeWorker();
    const bytes = new Uint8Array([1, 2, 3, 4]);

    void core.estimateImport({ "kick.wav": bytes }, 36000, "mix");
    expect(lastRequest(worker)).toMatchObject({
      method: "estimateImport",
      payload: { rate: 36000, channel: "mix" },
    });
    expect(worker.transfers.at(-1)).toEqual([]);
  });

  // The dialog keeps its file bytes after Convert: the estimate reads
  // them again and a failed batch retries from the failed file. The
  // import calls transfer for speed, so what they transfer has to be
  // a copy, never the caller's own buffer.
  it("imports transfer a copy, leaving the caller's buffers usable", () => {
    const core = newCore();
    const worker = activeWorker();
    const bytes = new Uint8Array([1, 2, 3, 4]);

    void core.importWavToInstrument("kick.wav", bytes, 36000, "mix");
    const single = worker.transfers.at(-1) ?? [];
    expect(single).toHaveLength(1);
    expect(single[0]).not.toBe(bytes.buffer);
    const posted = lastRequest(worker).payload as ImportWavPayload;
    expect(Array.from(new Uint8Array(posted.buffer))).toEqual([1, 2, 3, 4]);

    void core.importWavFolder({ "kick.wav": bytes }, 36000, false, "mix");
    const batch = worker.transfers.at(-1) ?? [];
    expect(batch).toHaveLength(1);
    expect(batch[0]).not.toBe(bytes.buffer);
  });
});

describe("core worker", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("answers core-unavailable when the module fails to boot", async () => {
    const posted: unknown[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new Error("the core module did not load"))),
    );
    vi.stubGlobal("postMessage", (message: unknown) => posted.push(message));

    vi.resetModules();
    await import("../src/core/worker");
    const onmessage = self.onmessage as unknown as (event: { data: unknown }) => void;
    expect(onmessage).toBeTypeOf("function");

    onmessage({ data: { id: 7, method: "snapshot" } });

    await vi.waitFor(() => {
      expect(posted).toHaveLength(1);
    });
    expect(posted[0]).toMatchObject({
      id: 7,
      result: { ok: false, error: { code: "core-unavailable" } },
    });
  });

  it("answers core-unavailable when a call comes back empty", async () => {
    // A Go fatal error, an exhausted heap on a big import say, unwinds
    // the exported function and leaves it returning nothing.
    const posted = await bootedWorker({ importWavToInstrument: () => undefined });
    const onmessage = self.onmessage as unknown as (event: { data: unknown }) => void;

    onmessage({
      data: {
        id: 11,
        method: "importWavToInstrument",
        payload: { filename: "a.wav", buffer: new ArrayBuffer(4), rate: 18000, channel: "mix" },
      },
    });

    await vi.waitFor(() => {
      expect(posted).toHaveLength(1);
    });
    expect(posted[0]).toMatchObject({
      id: 11,
      result: { ok: false, error: { code: "core-unavailable" } },
    });
  });

  it("answers an unknown method rather than parking it", async () => {
    const posted = await bootedWorker({});
    const onmessage = self.onmessage as unknown as (event: { data: unknown }) => void;

    onmessage({ data: { id: 13, method: "teleport" } });

    await vi.waitFor(() => {
      expect(posted).toHaveLength(1);
    });
    expect(posted[0]).toMatchObject({ id: 13, result: { ok: false } });
  });

  it("dispatches the generation window to the core with the arguments given", async () => {
    const calls: unknown[][] = [];
    const record =
      (name: string) =>
      (...args: unknown[]) => {
        calls.push([name, ...args]);
        return { ok: true, value: null };
      };
    const posted = await bootedWorker({
      setSlotGeneration: record("setSlotGeneration"),
    });
    const onmessage = self.onmessage as unknown as (event: { data: unknown }) => void;

    onmessage({ data: { id: 22, method: "setSlotGeneration", payload: [2, 100, 900] } });

    await vi.waitFor(() => {
      expect(posted).toHaveLength(1);
    });
    expect(calls).toEqual([["setSlotGeneration", 2, 100, 900]]);
  });
});

// Loads a fresh worker module with the given core registered and the
// ready signal fired. The fetch never settles, so the boot neither
// succeeds nor fails and the fake core is what answers.
async function bootedWorker(core: Record<string, () => unknown>): Promise<unknown[]> {
  const posted: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(() => new Promise(() => undefined)),
  );
  vi.stubGlobal("postMessage", (message: unknown) => posted.push(message));
  vi.stubGlobal("fizzleCore", core);

  vi.resetModules();
  await import("../src/core/worker");
  const signal = (globalThis as { onFizzleReady?: () => void }).onFizzleReady;
  signal?.();
  return posted;
}
