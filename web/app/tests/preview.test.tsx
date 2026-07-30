// The preview plays at the voice's current pitch (R20). The audition
// query is keyed by the audio identity alone, which is the settled
// optimisation that stops a knob turn re-decoding the PCM, so a root
// key edit never refetches the payload. Pitch therefore has to come
// from the snapshot at play time. R21's approximation licence covers
// the filter and the velocity response, not the pitch.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { openInstrumentDisk } from "./helpers";

interface Recorded {
  /** One playback rate per note started. */
  rates: number[];
  /** The sample rate each buffer was built at. */
  bufferRates: number[];
}

/** A recording stand-in for the Web Audio context jsdom lacks. */
function stubAudioContext(): Recorded {
  const recorded: Recorded = { rates: [], bufferRates: [] };
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
        stop: () => undefined,
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
