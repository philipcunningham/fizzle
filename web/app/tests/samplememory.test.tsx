// The machine the user is building for (R27, R31). It is chosen on the
// first page, it outlives the session, and it is a fact about the
// sampler rather than an edit to the document.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { Core } from "../src/boundary/contract";
import { createFakeCore } from "../src/core/fake";
import { openDisk, openInstrumentDisk, pickFiles, wavFixture } from "./helpers";

const MB = 1024 * 1024;
const KEY = "fizzle.sampleMemory";

afterEach(() => {
  localStorage.clear();
});

/** A core that records what the shell told it about the machine. */
function recordingCore() {
  const inner = createFakeCore();
  const declared: number[] = [];
  const core: Core = {
    ...inner,
    setSampleMemory: (bytes) => {
      declared.push(bytes);
      return inner.setSampleMemory(bytes);
    },
  };
  return { core, declared };
}

describe("declaring the sampler's memory", () => {
  it("states the assumption on the first page", async () => {
    const { core } = recordingCore();
    await openDisk(core);
    fireEvent.click(screen.getByRole("button", { name: "Eject" }));
    const pick = await screen.findByLabelText<HTMLSelectElement>("sampler memory");
    expect(pick.value).toBe(String(MB));
    expect(screen.getByText(/FZ-1 shipped with 1 MB/)).toBeTruthy();
  });

  it("tells the core, and remembers for next time", async () => {
    const { core, declared } = recordingCore();
    await openDisk(core);
    fireEvent.click(screen.getByRole("button", { name: "Eject" }));
    const pick = await screen.findByLabelText("sampler memory");
    fireEvent.change(pick, { target: { value: String(2 * MB) } });

    await waitFor(() => {
      expect(declared).toContain(2 * MB);
    });
    expect(localStorage.getItem(KEY)).toBe(String(2 * MB));
  });

  it("adopts the remembered machine at boot, before anything is asked of it", async () => {
    localStorage.setItem(KEY, String(2 * MB));
    const { core, declared } = recordingCore();
    await openInstrumentDisk(core);
    await waitFor(() => {
      expect(declared).toContain(2 * MB);
    });
    await waitFor(() => {
      expect(screen.getByRole("status", { name: "memory free" }).textContent).toContain("%");
    });
  });

  it("carries on with the default when storage refuses", async () => {
    const original = localStorage.getItem.bind(localStorage);
    localStorage.getItem = () => {
      throw new Error("storage is off");
    };
    try {
      const { core } = recordingCore();
      await openInstrumentDisk(core);
      // The reading is still there; only the memory of the choice is lost.
      expect(screen.getByRole("status", { name: "memory free" })).toBeTruthy();
    } finally {
      localStorage.getItem = original;
    }
  });

  // The reading is the core's answer, and the core keeps the revision
  // the snapshot query is keyed by, so the shell has to re-read it.
  // 1.2 MB of audio fills a stock machine and fits an expanded one.
  async function importBigWav() {
    pickFiles([new File([wavFixture(1, 18000, 600000)], "big.wav")]);
    await screen.findByText(/Import 1 WAV/);
    fireEvent.click(screen.getByRole("button", { name: "Convert" }));
  }

  /** The memory reading's percentage, exactly. */
  async function memoryFree(): Promise<string> {
    const el = await screen.findByRole("status", { name: "memory free" });
    return /(\d+)% memory free/.exec(el.textContent)?.[1] ?? "";
  }

  it("leaves a stock machine with nothing free", async () => {
    const { core } = recordingCore();
    await openDisk(core);
    await importBigWav();
    await waitFor(async () => {
      expect(await memoryFree()).toBe("0");
    });
  });

  it("gives the same instrument room once the expansion is declared", async () => {
    localStorage.setItem(KEY, String(2 * MB));
    const { core } = recordingCore();
    await openDisk(core);
    await importBigWav();
    await waitFor(async () => {
      expect(Number(await memoryFree())).toBeGreaterThan(30);
    });
  });
});
