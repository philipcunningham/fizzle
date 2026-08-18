// The preview plays at the voice's current pitch (R20). The audition
// query is keyed by audio identity alone, so a knob turn never
// re-decodes the PCM and a root key edit never refetches the payload.
// Pitch therefore comes from the snapshot at play time. R21's
// approximation licence covers the filter and velocity, not the pitch.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Core } from "../src/boundary/contract";
import { createFakeCore } from "../src/core/fake";
import { openInstrumentDisk } from "./helpers";

interface Recorded {
  /** One playback rate per note started. */
  rates: number[];
  /** The sample rate each buffer was built at. */
  bufferRates: number[];
  /** One entry per source stopped: the release path ran. */
  stops: number[];
}

/** A recording stand-in for the Web Audio context jsdom lacks. */
function stubAudioContext(): Recorded {
  const recorded: Recorded = { rates: [], bufferRates: [], stops: [] };
  vi.stubGlobal(
    "AudioContext",
    vi.fn(() => ({
      currentTime: 0,
      state: "running",
      resume: () => Promise.resolve(),
      destination: {},
      createBuffer: (_channels: number, length: number, sampleRate: number) => {
        recorded.bufferRates.push(sampleRate);
        return { getChannelData: () => new Float32Array(length) };
      },
      createGain: () => ({
        gain: {
          value: 1,
          setValueAtTime: () => undefined,
          linearRampToValueAtTime: () => undefined,
          cancelScheduledValues: () => undefined,
        },
        connect: () => undefined,
        disconnect: () => undefined,
      }),
      createBufferSource: () => ({
        buffer: null,
        playbackRate: {
          get value() {
            return recorded.rates[recorded.rates.length - 1] ?? 0;
          },
          set value(rate: number) {
            recorded.rates.push(rate);
          },
        },
        connect: () => undefined,
        start: () => undefined,
        stop: () => {
          recorded.stops.push(1);
        },
      }),
    })),
  );
  return recorded;
}

function playMiddleC() {
  const key = screen.getByTestId("key-60");
  fireEvent.pointerDown(key, { pointerId: 1, clientY: 0 });
  fireEvent.pointerUp(key, { pointerId: 1 });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("preview pitch (R20)", () => {
  it("follows a root key edit instead of the cached payload's root", async () => {
    const recorded = stubAudioContext();
    await openInstrumentDisk();

    // The fake's voices sit at root C4, so C4 plays back unshifted.
    // The PCM arrives asynchronously, so the first press waits for it;
    // the retry stops on the press that lands.
    await waitFor(() => {
      playMiddleC();
      expect(recorded.rates).toEqual([1]);
    });

    // Root up an octave. No wave pointer moves, so the audition query
    // holds its cached PCM and the root that came with it.
    const root = screen.getByLabelText("Root");
    fireEvent.change(root, { target: { value: "72" } });
    fireEvent.blur(root);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("Root").value).toContain("(72)");
    });

    // C4 is now an octave below the root, so it plays at half speed.
    playMiddleC();
    expect(recorded.rates.length).toBe(2);
    expect(recorded.rates[1]).toBeCloseTo(0.5, 10);
  });
});

// The banks tab keyboard plays the mapping (the key's area resolves
// the slot, the area's root sets the pitch), where the voices tab
// plays the one selected voice across the whole keyboard.
describe("the banks tab keyboard plays the key mapping", () => {
  function slotRecordingCore() {
    const inner = createFakeCore();
    const slots: number[] = [];
    const core: Core = {
      ...inner,
      auditionSlot: (slot) => {
        slots.push(slot);
        return inner.auditionSlot(slot);
      },
    };
    return { core, slots };
  }

  /** Presses and holds: the release follows once playback lands. */
  async function holdKey(note: number, recorded: Recorded) {
    const key = screen.getByTestId(`key-${String(note)}`);
    fireEvent.pointerDown(key, { pointerId: 1, clientY: 0 });
    await waitFor(() => {
      expect(recorded.rates.length).toBeGreaterThanOrEqual(1);
    });
    fireEvent.pointerUp(key, { pointerId: 1 });
  }

  async function openBanksTab(core: Core) {
    await openInstrumentDisk(core);
    fireEvent.click(screen.getByRole("tab", { name: "Banks and Areas" }));
    await screen.findByRole("table", { name: "areas" });
  }

  it("prefetches and plays the slot the key maps to", async () => {
    const recorded = stubAudioContext();
    const { core, slots } = slotRecordingCore();
    await openBanksTab(core);

    // The bank's mapped slots prefetch on the tab, so a press plays
    // without waiting on a fetch. SNARE is slot 1; nothing maps the
    // unreferenced SPARE voice, so its slot is never fetched.
    await waitFor(() => {
      expect(slots).toContain(1);
    });
    expect(slots).not.toContain(2);

    // Move SNARE's area root down an octave first: the voice header
    // stays at 60, so the rate proves the area mapping played, not
    // the voices tab's focus-voice fallback.
    fireEvent.click(within(screen.getByRole("table", { name: "areas" })).getByText("SNARE"));
    await screen.findByText(/Edit area · SNARE/);
    const root = screen.getByLabelText("Root");
    fireEvent.change(root, { target: { value: "48" } });
    fireEvent.blur(root);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("Root").value).toBe("C3");
    });

    await holdKey(70, recorded);
    expect(recorded.rates.at(-1)).toBeCloseTo(Math.pow(2, 22 / 12), 10);
    // The release stopped what the press started.
    expect(recorded.stops.length).toBeGreaterThanOrEqual(1);
  });

  it("a tap released before the sound arrives stays silent", async () => {
    const recorded = stubAudioContext();
    const inner = createFakeCore();
    const gate: { open?: () => void } = {};
    const gated: Core = {
      ...inner,
      auditionSlot: (slot) =>
        new Promise((resolve) => {
          const prior = gate.open;
          gate.open = () => {
            prior?.();
            void inner.auditionSlot(slot).then(resolve);
          };
        }),
    };
    await openBanksTab(gated);

    const key = screen.getByTestId("key-70");
    fireEvent.pointerDown(key, { pointerId: 1, clientY: 0 });
    fireEvent.pointerUp(key, { pointerId: 1 });
    gate.open?.();
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(recorded.rates.length).toBe(0);
  });

  it("a key no area covers stays silent and never lights the bar", async () => {
    const recorded = stubAudioContext();
    const { core } = slotRecordingCore();
    await openBanksTab(core);

    fireEvent.click(screen.getByRole("button", { name: "delete area 2" }));
    await waitFor(() => {
      const table = screen.getByRole("table", { name: "areas" });
      expect(within(table).getAllByRole("row")).toHaveLength(2);
    });

    const key = screen.getByTestId("key-70");
    fireEvent.pointerDown(key, { pointerId: 1, clientY: 0 });
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(recorded.rates.length).toBe(0);
    expect(document.querySelector("[data-auditioning]")).toBeNull();
    fireEvent.pointerUp(key, { pointerId: 1 });
  });

  it("layers every area that covers the key, and releases them all", async () => {
    const recorded = stubAudioContext();
    const { core, slots } = slotRecordingCore();
    await openBanksTab(core);

    fireEvent.click(screen.getByRole("button", { name: "duplicate area 1" }));
    await waitFor(() => {
      const table = screen.getByRole("table", { name: "areas" });
      expect(within(table).getAllByRole("row")).toHaveLength(4);
    });
    await waitFor(() => {
      expect(slots).toContain(0);
    });

    const key = screen.getByTestId("key-40");
    fireEvent.pointerDown(key, { pointerId: 1, clientY: 0 });
    await waitFor(() => {
      expect(recorded.rates.length).toBeGreaterThanOrEqual(2);
    });
    fireEvent.pointerUp(key, { pointerId: 1 });
    expect(recorded.stops.length).toBeGreaterThanOrEqual(2);
  });

  it("pitches from the area's root, not the voice's", async () => {
    const recorded = stubAudioContext();
    const { core } = slotRecordingCore();
    await openBanksTab(core);

    // Raise KICK's area root an octave; the voice header stays at 60.
    fireEvent.click(within(screen.getByRole("table", { name: "areas" })).getByText("KICK"));
    await screen.findByText(/Edit area · KICK/);
    const root = screen.getByLabelText("Root");
    fireEvent.change(root, { target: { value: "72" } });
    fireEvent.blur(root);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("Root").value).toBe("C5");
    });

    // C3 sits inside KICK's key range, two octaves below the area's
    // new root, so it plays at quarter speed; the voice header's own
    // root is still C4 and would have given half.
    await holdKey(48, recorded);
    expect(recorded.rates.at(-1)).toBeCloseTo(0.25, 10);
  });
});
