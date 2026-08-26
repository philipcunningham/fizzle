// The machine the user is building for (R27, R31). It is chosen on the
// first page, it outlives the session, and it is a fact about the
// sampler rather than an edit to the document.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { Core } from "../src/boundary/contract";
import { createScenarioCore } from "./support/scenarioCore";
import { openDisk, openInstrumentDisk, pickFiles, wavFixture } from "./helpers";

const MB = 1024 * 1024;
const KEY = "fizzle.sampleMemory";

afterEach(() => {
  localStorage.clear();
});

/** A core that records what the shell told it about the machine. */
function recordingCore() {
  const inner = createScenarioCore();
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
    // It teaches the expansion, not the badge: an FZ-1 with the card
    // fitted holds what a rack unit holds.
    expect(screen.getByText(/expansion card is fitted/)).toBeTruthy();
    // And it keeps the promise the page has always made.
    expect(screen.getByText(/Nothing leaves this machine/)).toBeTruthy();
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
    const read = localStorage.getItem.bind(localStorage);
    const write = localStorage.setItem.bind(localStorage);
    localStorage.getItem = () => {
      throw new Error("storage is off");
    };
    localStorage.setItem = () => {
      throw new Error("storage is off");
    };
    try {
      const { core, declared } = recordingCore();
      await openInstrumentDisk(core);
      // Nothing was remembered, so nothing was declared, and the
      // default stands. Only the memory of the choice is lost.
      expect(declared).toHaveLength(0);
      const reading = await screen.findByRole("status", { name: "memory free" });
      expect(reading.textContent).toContain("% memory free");

      fireEvent.click(screen.getByRole("button", { name: "Eject" }));
      const pick = await screen.findByLabelText("sampler memory");
      fireEvent.change(pick, { target: { value: String(2 * MB) } });
      // The core still hears it; only the remembering failed.
      await waitFor(() => {
        expect(declared).toContain(2 * MB);
      });
    } finally {
      localStorage.getItem = read;
      localStorage.setItem = write;
    }
  });

  // A figure from a build with different choices, or a hand edited
  // profile, must not be pushed at a core that will refuse it.
  it("ignores a remembered figure no FZ holds", async () => {
    localStorage.setItem(KEY, String(4 * MB));
    const { core, declared } = recordingCore();
    await openInstrumentDisk(core);
    await waitFor(() => {
      expect(screen.getByRole("status", { name: "memory free" })).toBeTruthy();
    });
    expect(declared).not.toContain(4 * MB);
    // jsdom always carries the unsupported browser notice, so this
    // asks only that nothing complains about the memory figure.
    const alerts = screen.queryAllByRole("alert").map((a) => a.textContent);
    expect(alerts.join(" ")).not.toMatch(/sample memory/i);
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
