// The on-screen keyboard (R20) and the Web MIDI subscription (R22).
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Keyboard } from "../src/ui/Keyboard";
import { subscribeMIDI } from "../src/ui/midi";

describe("on-screen keyboard", () => {
  it("plays a note with velocity from click height and releases", () => {
    const events: [string, number, number?][] = [];
    render(
      <Keyboard
        lowNote={36}
        octaves={2}
        onNoteOn={(n, v) => events.push(["on", n, v])}
        onNoteOff={(n) => events.push(["off", n])}
      />,
    );
    const key = screen.getByTestId("key-48");
    fireEvent.pointerDown(key, { clientY: 0, pointerId: 1 });
    fireEvent.pointerUp(key, { pointerId: 1 });
    expect(events[0]?.[0]).toBe("on");
    expect(events[0]?.[1]).toBe(48);
    const velocity = events[0]?.[2] ?? 0;
    expect(velocity).toBeGreaterThanOrEqual(1);
    expect(velocity).toBeLessThanOrEqual(127);
    expect(events[1]).toEqual(["off", 48]);
  });

  it("highlights the given range and marks the root", () => {
    render(
      <Keyboard
        lowNote={36}
        octaves={2}
        highlight={[{ lo: 40, hi: 50 }]}
        rootKey={43}
        onNoteOn={() => undefined}
        onNoteOff={() => undefined}
      />,
    );
    // The fills are tokens now, so the assertion reads which token a
    // key took rather than a literal the stylesheet no longer owns.
    expect(screen.getByTestId("key-43").getAttribute("fill")).toBe("var(--fz-key-white-on)");
    expect(screen.getByTestId("key-36").getAttribute("fill")).toBe("var(--fz-key-white)");
  });
});

describe("web MIDI", () => {
  it("routes note on and off from an input", async () => {
    const events: [string, number][] = [];
    let handler: ((e: { data: Uint8Array }) => void) | null = null;
    const input = {
      set onmidimessage(fn: ((e: { data: Uint8Array }) => void) | null) {
        handler = fn;
      },
      get onmidimessage() {
        return handler;
      },
    };
    const access = { inputs: new Map([["in", input]]), onstatechange: null };
    const cleanup = subscribeMIDI(
      {
        onNoteOn: (n) => events.push(["on", n]),
        onNoteOff: (n) => events.push(["off", n]),
      },
      () => Promise.resolve(access),
    );
    await Promise.resolve();
    input.onmidimessage?.({ data: new Uint8Array([0x90, 60, 100]) });
    input.onmidimessage?.({ data: new Uint8Array([0x80, 60, 0]) });
    input.onmidimessage?.({ data: new Uint8Array([0x90, 62, 0]) }); // running-status off
    expect(events).toEqual([
      ["on", 60],
      ["off", 60],
      ["off", 62],
    ]);
    cleanup();
    expect(input.onmidimessage).toBeNull();
  });

  it("absence degrades silently (R22)", () => {
    const cleanup = subscribeMIDI({
      onNoteOn: () => undefined,
      onNoteOff: () => undefined,
    });
    cleanup();
  });
});
