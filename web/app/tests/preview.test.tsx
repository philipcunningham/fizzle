// The preview plays at the voice's current pitch (R20). The audition
// query is keyed by audio identity alone, so a knob turn never
// re-decodes the PCM and a root key edit never refetches the payload.
// Pitch therefore comes from the snapshot at play time. R21's
// approximation licence covers the filter and velocity, not the pitch.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Core } from "../src/boundary/contract";
import { createFakeCore } from "../src/core/fake";
import { commitField, openInstrumentDisk } from "./helpers";

interface Recorded {
  /** One playback rate per note started. */
  rates: number[];
  /** The sample rate each buffer was built at. */
  bufferRates: number[];
  /** One entry per source stopped: the release path ran. */
  stops: number[];
  /**
   * One entry per note started, in the same order as the rates: the
   * loop bounds in buffer seconds, or null for a source playing
   * straight through.
   */
  loops: ({ start: number; end: number } | null)[];
  /** One entry per source stopped: the loop bounds at release, or null. */
  releaseLoops: ({ start: number; end: number } | null)[];
  /** Every gain value scheduled, in order, across all sources. */
  gains: number[];
}

/** A recording stand-in for the Web Audio context jsdom lacks. */
function stubAudioContext(): Recorded {
  const recorded: Recorded = {
    rates: [],
    bufferRates: [],
    stops: [],
    loops: [],
    releaseLoops: [],
    gains: [],
  };
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
          setValueAtTime: (v: number) => recorded.gains.push(v),
          linearRampToValueAtTime: (v: number) => recorded.gains.push(v),
          exponentialRampToValueAtTime: (v: number) => recorded.gains.push(v),
          cancelScheduledValues: () => undefined,
        },
        connect: () => undefined,
        disconnect: () => undefined,
      }),
      createBufferSource: () => {
        // Per source, so a layered press records one entry each.
        const own = { loop: false, loopStart: 0, loopEnd: 0 };
        return {
          buffer: null,
          playbackRate: {
            get value() {
              return recorded.rates[recorded.rates.length - 1] ?? 0;
            },
            set value(rate: number) {
              recorded.rates.push(rate);
            },
          },
          get loop() {
            return own.loop;
          },
          set loop(on: boolean) {
            own.loop = on;
          },
          get loopStart() {
            return own.loopStart;
          },
          set loopStart(at: number) {
            own.loopStart = at;
          },
          get loopEnd() {
            return own.loopEnd;
          },
          set loopEnd(at: number) {
            own.loopEnd = at;
          },
          connect: () => undefined,
          start: () => {
            recorded.loops.push(own.loop ? { start: own.loopStart, end: own.loopEnd } : null);
          },
          stop: () => {
            recorded.stops.push(1);
            recorded.releaseLoops.push(
              own.loop ? { start: own.loopStart, end: own.loopEnd } : null,
            );
          },
        };
      },
    })),
  );
  return recorded;
}

function playMiddleC() {
  const key = screen.getByTestId("key-60");
  fireEvent.pointerDown(key, { pointerId: 1, clientY: 0 });
  fireEvent.pointerUp(key, { pointerId: 1 });
}

/**
 * A press at a chosen velocity. The keyboard reads velocity off click
 * height, and jsdom gives every element a zero height, so the key gets
 * a box before it is pressed.
 */
function playAt(velocity: number) {
  const key = screen.getByTestId("key-60");
  const height = 127;
  key.getBoundingClientRect = () =>
    ({ top: 0, left: 0, right: 20, bottom: height, width: 20, height, x: 0, y: 0 }) as DOMRect;
  fireEvent.pointerDown(key, { pointerId: 1, clientY: velocity });
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

// A held key repeats the loop the voice names as its sustain loop,
// which is what the sampler does. The sustain loop itself is a Radix
// select these tests can't drive, so it goes through the core; the
// field edits that follow refetch the snapshot carrying it, and the
// browser smoke covers the select.
describe("the preview repeats the sustain loop", () => {
  it("loops between the named loop's bounds, in buffer seconds", async () => {
    const recorded = stubAudioContext();
    const core = createFakeCore();
    await openInstrumentDisk(core);
    await core.setSlotLoopSelect(0, 0, 8);
    await commitField("loop 1 start", "1000");
    await commitField("loop 1 end", "3000");

    // The PCM arrives asynchronously, so the first press waits for it.
    await waitFor(() => {
      playMiddleC();
      expect(recorded.rates.length).toBeGreaterThan(0);
    });
    const loop = recorded.loops.at(-1);
    // KICK's voice runs at 18 kHz in the fake.
    expect(loop?.start).toBeCloseTo(1000 / 18000, 10);
    expect(loop?.end).toBeCloseTo(3000 / 18000, 10);
  });

  it("plays a voice that names no sustain loop straight through", async () => {
    const recorded = stubAudioContext();
    await openInstrumentDisk();
    await waitFor(() => {
      playMiddleC();
      expect(recorded.rates.length).toBeGreaterThan(0);
    });
    expect(recorded.loops.at(-1)).toBeNull();
  });

  // Naming a loop is one action and giving it width is another, so a
  // voice can name the loop an import parked at the generation end.
  it("plays straight through when the named loop has no width", async () => {
    const recorded = stubAudioContext();
    const core = createFakeCore();
    await openInstrumentDisk(core);
    await core.setSlotLoopSelect(0, 0, 8);
    // Loop 2 carries the edit, so loop 1 keeps the width it imported
    // with. The refetch it forces brings the designation with it.
    await commitField("loop 2 start", "100");
    await waitFor(() => {
      playMiddleC();
      expect(recorded.rates.length).toBeGreaterThan(0);
    });
    expect(recorded.loops.at(-1)).toBeNull();
    expect(screen.queryByText(/repeats while held/)).toBeNull();
  });

  it("follows a loop edit instead of the cached payload", async () => {
    const recorded = stubAudioContext();
    const core = createFakeCore();
    await openInstrumentDisk(core);
    await core.setSlotLoopSelect(0, 0, 8);
    await commitField("loop 1 start", "1000");
    await commitField("loop 1 end", "3000");
    await waitFor(() => {
      playMiddleC();
      expect(recorded.rates.length).toBeGreaterThan(0);
    });

    // No wave pointer moves, so the audition query holds its cached
    // PCM: the new bounds have to come from the snapshot at play time.
    await commitField("loop 1 end", "2000");
    playMiddleC();
    expect(recorded.loops.at(-1)?.end).toBeCloseTo(2000 / 18000, 10);
  });
});

// Switching voices leaves the previous voice's PCM on screen while the
// new one decodes. Playing it would pair one voice's samples with
// another's loop, and a loop past the shorter buffer plays nothing at
// all, so the press waits.
describe("the preview never plays one voice through another's payload", () => {
  it("stays silent until the switched-to voice's samples arrive", async () => {
    const recorded = stubAudioContext();
    const inner = createFakeCore();
    let open: (() => void) | null = null;
    const core: Core = {
      ...inner,
      auditionSlot: (slot) =>
        slot === 1
          ? new Promise((resolve) => {
              open = () => {
                void inner.auditionSlot(slot).then(resolve);
              };
            })
          : inner.auditionSlot(slot),
    };
    await openInstrumentDisk(core);
    await waitFor(() => {
      playMiddleC();
      expect(recorded.rates.length).toBeGreaterThan(0);
    });
    const before = recorded.rates.length;

    fireEvent.click((await screen.findAllByText("SNARE"))[0] as HTMLElement);
    await screen.findByText(/4,352 frames/);
    playMiddleC();
    expect(recorded.rates.length).toBe(before);

    (open as (() => void) | null)?.();
    await waitFor(() => {
      playMiddleC();
      expect(recorded.rates.length).toBeGreaterThan(before);
    });
  });
});

// A sustain loop has no natural end, so a note the page abandons would
// sound until the tab closed. The pointer up that would have released
// it goes wherever the focus went.
describe("a held note survives nothing", () => {
  /** Presses and holds middle C, waiting for the sound to land. */
  async function holdMiddleC(recorded: Recorded) {
    const key = screen.getByTestId("key-60");
    await waitFor(() => {
      fireEvent.pointerDown(key, { pointerId: 1, clientY: 0 });
      expect(recorded.rates.length).toBeGreaterThan(0);
    });
  }

  it("releases what is held when the page is hidden", async () => {
    const recorded = stubAudioContext();
    await openInstrumentDisk();
    await holdMiddleC(recorded);
    const before = recorded.stops.length;

    Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true });
    try {
      fireEvent(document, new Event("visibilitychange"));
    } finally {
      Reflect.deleteProperty(document, "visibilityState");
    }
    expect(recorded.stops.length).toBeGreaterThan(before);
    expect(document.querySelector("[data-auditioning]")).toBeNull();
  });

  // The release lives in a closure the keyboard's own pointer up
  // calls. Take the keyboard away mid-press and that pointer up lands
  // on an element that no longer exists.
  it("releases what is held when the keyboard goes away", async () => {
    const recorded = stubAudioContext();
    await openInstrumentDisk();
    await holdMiddleC(recorded);
    const before = recorded.stops.length;

    fireEvent.click(screen.getByRole("button", { name: "Eject" }));
    await screen.findByRole("button", { name: "New disk" });
    expect(recorded.stops.length).toBeGreaterThan(before);
  });

  // A MIDI note belongs to the device holding it, and the device
  // reaches a window nobody is looking at, so focus moving elsewhere
  // is not a note off. The page going away still ends it.
  it("keeps a MIDI note through a focus change, and ends it on the way out", async () => {
    const recorded = stubAudioContext();
    let send: ((data: number[]) => void) | null = null;
    const input = {
      onmidimessage: null as ((e: { data: Uint8Array }) => void) | null,
    };
    Object.defineProperty(navigator, "requestMIDIAccess", {
      value: () => Promise.resolve({ inputs: new Map([["in", input]]), onstatechange: null }),
      configurable: true,
    });
    try {
      await openInstrumentDisk();
      await waitFor(() => {
        expect(input.onmidimessage).not.toBeNull();
      });
      send = (data) => {
        input.onmidimessage?.({ data: new Uint8Array(data) });
      };
      await waitFor(() => {
        send?.([0x90, 60, 100]);
        expect(recorded.rates.length).toBeGreaterThan(0);
      });
      const before = recorded.stops.length;

      fireEvent.blur(window);
      expect(recorded.stops.length).toBe(before);
      expect(document.querySelector("[data-auditioning]")).not.toBeNull();

      Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true });
      try {
        fireEvent(document, new Event("visibilitychange"));
      } finally {
        Reflect.deleteProperty(document, "visibilityState");
      }
      expect(recorded.stops.length).toBeGreaterThan(before);
    } finally {
      Reflect.deleteProperty(navigator, "requestMIDIAccess");
    }
  });

  it("marks the strip with a playhead while a key sounds, and clears it at note off", async () => {
    const recorded = stubAudioContext();
    await openInstrumentDisk();
    expect(document.querySelector(".waveform-wrap[data-playhead]")).toBeNull();

    await holdMiddleC(recorded);
    await waitFor(() => {
      expect(document.querySelector(".waveform-wrap[data-playhead]")).not.toBeNull();
    });

    fireEvent.pointerUp(screen.getByTestId("key-60"), { pointerId: 1 });
    await waitFor(() => {
      expect(document.querySelector(".waveform-wrap[data-playhead]")).toBeNull();
    });
  });

  it("releases what is held when the window loses focus", async () => {
    const recorded = stubAudioContext();
    await openInstrumentDisk();
    await holdMiddleC(recorded);
    const before = recorded.stops.length;

    fireEvent.blur(window);
    expect(recorded.stops.length).toBeGreaterThan(before);
    expect(document.querySelector("[data-auditioning]")).toBeNull();
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

  it("loops each sounding slot's own sustain loop, not the selected voice's", async () => {
    const recorded = stubAudioContext();
    const core = createFakeCore();
    await openInstrumentDisk(core);
    await core.setSlotLoopSelect(0, 0, 8);
    await core.setSlotLoopSelect(1, 0, 8);

    // KICK (slot 0) is selected first; SNARE (slot 1) gets bounds the
    // assertion can tell apart from it.
    await commitField("loop 1 start", "100");
    await commitField("loop 1 end", "200");
    fireEvent.click((await screen.findAllByText("SNARE"))[0] as HTMLElement);
    await screen.findByText(/4,352 frames/);
    await commitField("loop 1 start", "300");
    await commitField("loop 1 end", "900");

    fireEvent.click(screen.getByRole("tab", { name: "Banks and Areas" }));
    await screen.findByRole("table", { name: "areas" });
    // Selecting KICK's area focuses slot 0, while key 70 sounds SNARE:
    // a preview reading the focus voice would play 100 to 200.
    fireEvent.click(within(screen.getByRole("table", { name: "areas" })).getByText("KICK"));
    await screen.findByText(/Edit area · KICK/);

    await holdKey(70, recorded);
    const loop = recorded.loops.at(-1);
    expect(loop?.start).toBeCloseTo(300 / 18000, 10);
    expect(loop?.end).toBeCloseTo(900 / 18000, 10);
  });

  it("layers two slots and gives each its own loop", async () => {
    const recorded = stubAudioContext();
    const core = createFakeCore();
    await openInstrumentDisk(core);
    // KICK repeats; the duplicate that layers over it doesn't.
    await core.setSlotLoopSelect(0, 0, 8);
    await commitField("loop 1 start", "100");
    await commitField("loop 1 end", "900");

    fireEvent.click(screen.getByRole("tab", { name: "Banks and Areas" }));
    await screen.findByRole("table", { name: "areas" });
    fireEvent.click(screen.getByRole("button", { name: "duplicate area 1" }));
    await waitFor(() => {
      const table = screen.getByRole("table", { name: "areas" });
      expect(within(table).getAllByRole("row")).toHaveLength(4);
    });

    const key = screen.getByTestId("key-40");
    fireEvent.pointerDown(key, { pointerId: 1, clientY: 0 });
    await waitFor(() => {
      expect(recorded.loops.length).toBeGreaterThanOrEqual(2);
    });
    fireEvent.pointerUp(key, { pointerId: 1 });

    const sounded = recorded.loops.slice(-2);
    expect(sounded.filter((l) => l === null)).toHaveLength(1);
    const looped = sounded.find((l) => l !== null);
    expect(looped?.start).toBeCloseTo(100 / 18000, 10);
    expect(looped?.end).toBeCloseTo(900 / 18000, 10);
  });

  it("names a release loop for the sounding slot, not the selected voice's", async () => {
    const recorded = stubAudioContext();
    const core = createFakeCore();
    await openInstrumentDisk(core);
    await core.setSlotLoopSelect(0, 8, 0);
    await core.setSlotLoopSelect(1, 8, 0);

    // KICK (slot 0) is selected first; SNARE (slot 1) gets bounds the
    // assertion can tell apart from it.
    await commitField("loop 1 start", "100");
    await commitField("loop 1 end", "200");
    fireEvent.click((await screen.findAllByText("SNARE"))[0] as HTMLElement);
    await screen.findByText(/4,352 frames/);
    await commitField("loop 1 start", "400");
    await commitField("loop 1 end", "900");

    fireEvent.click(screen.getByRole("tab", { name: "Banks and Areas" }));
    await screen.findByRole("table", { name: "areas" });
    // Selecting KICK's area focuses slot 0, while key 70 sounds SNARE:
    // a preview reading the focus voice would move to 100 to 200 at
    // release instead.
    fireEvent.click(within(screen.getByRole("table", { name: "areas" })).getByText("KICK"));
    await screen.findByText(/Edit area · KICK/);

    await holdKey(70, recorded);
    const loop = recorded.releaseLoops.at(-1);
    expect(loop?.start).toBeCloseTo(400 / 18000, 10);
    expect(loop?.end).toBeCloseTo(900 / 18000, 10);
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

describe("the release loop reaches the preview", () => {
  it("moves the window to the release loop at key up", async () => {
    const recorded = stubAudioContext();
    const core = createFakeCore();
    await openInstrumentDisk(core);
    // Name loop 1 the sustain loop and loop 2 the release loop, with
    // distinct bounds. A sustain loop naming none (8) would let note
    // on's own cap at min(loop_sus, loop_end) (F000:122B) land on the
    // release loop already, so the window at release would match the
    // one note on set and prove nothing moved.
    await core.setSlotLoopSelect(0, 0, 1);
    await commitField("loop 1 start", "500");
    await commitField("loop 1 end", "1200");
    await commitField("loop 2 start", "2000");
    await commitField("loop 2 end", "3500");

    await waitFor(() => {
      playMiddleC();
      expect(recorded.rates.length).toBeGreaterThan(0);
    });
    // Note on holds the sustain loop. KICK's voice runs at 18 kHz in
    // the fake.
    const held = recorded.loops.at(-1);
    expect(held?.start).toBeCloseTo(500 / 18000, 10);
    expect(held?.end).toBeCloseTo(1200 / 18000, 10);
    // Key up moves the window to the release loop, distinct from the
    // one note on set: the move is real, not the same window twice.
    const loop = recorded.releaseLoops.at(-1);
    expect(loop?.start).toBeCloseTo(2000 / 18000, 10);
    expect(loop?.end).toBeCloseTo(3500 / 18000, 10);
  });
});

// The four envelope follow fields reach the engine by name, read out
// of the schema parameters at play time. A name that drifts from the
// core's would leave every voice playing unbent, silently, with every
// other test still green: the smoke presses at full velocity where a
// scaled envelope and an unscaled one agree.
describe("the envelope follows the press", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("plays quieter for a soft press once velocity sensitivity is on", async () => {
    const recorded = stubAudioContext();
    await openInstrumentDisk();

    const peakOf = async (velocity: number) => {
      recorded.gains.length = 0;
      await waitFor(() => {
        playAt(velocity);
        expect(recorded.gains.length).toBeGreaterThan(0);
      });
      return Math.max(...recorded.gains);
    };

    // An imported voice carries the hardware's own default
    // sensitivity, so the press already bends the envelope.
    expect(await peakOf(20)).toBeLessThan(await peakOf(120));

    // Turned off, it cannot: the press reaches the envelope only
    // through this field.
    await commitField("To amplitude", "0");
    expect(await peakOf(20)).toBeCloseTo(await peakOf(120), 6);

    // And turned up again it bends further than the default did.
    await commitField("To amplitude", "120");
    const soft = await peakOf(20);
    const hard = await peakOf(120);
    expect(soft).toBeLessThan(hard);
  });
});
