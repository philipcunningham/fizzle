// Pointer drags on the controls, the gap the ship review findings name
// under "What the suites don't assert": no test pressed, moved, and
// released a pointer on any control, so every gesture fix was unpinned.
// What each drag control owes the core is the same contract, so the
// tests are the same shape. A drag opens
// the gesture bracket once and closes it exactly once, whatever ends it:
// a release, a cancel, a lost capture, or the control leaving the
// document mid-drag. A run of auto-repeat arrow keys is one bracket too,
// because R24 makes a continuous gesture one undo step. And a press that
// changes no value, the drag that runs into a rail, writes nothing.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { EnvelopeSnapshot } from "../src/boundary/contract";
import { EnvelopeEditor } from "../src/ui/EnvelopeEditor";
import { Knob } from "../src/ui/Knob";
import { MatrixGrid } from "../src/ui/MatrixGrid";
import { RangeSlider } from "../src/ui/RangeSlider";
import { openInstrumentDisk } from "./helpers";

/** Counts the brackets a control opens and closes. */
function bracketSpy() {
  const calls = { begins: 0, commits: 0 };
  const handlers = {
    onGestureBegin: () => {
      calls.begins += 1;
    },
    onGestureCommit: () => {
      calls.commits += 1;
    },
  };
  return { calls, handlers };
}

/** jsdom lays nothing out, so a control that maps pixels needs a box. */
function stubBox(el: Element, width: number, height: number) {
  vi.spyOn(el, "getBoundingClientRect").mockReturnValue({
    x: 0,
    y: 0,
    width,
    height,
    top: 0,
    right: width,
    bottom: height,
    left: 0,
    toJSON: () => ({}),
  });
}

function emptyMatrix(): number[][] {
  return Array.from({ length: 3 }, () => new Array<number>(7).fill(0));
}

const ENV: EnvelopeSnapshot = {
  sustain: 2,
  end: 5,
  rates: [80, 70, 60, 50, 40, 30, 20, 10],
  stops: [99, 90, 80, 70, 60, 50, 40, 30],
};

describe("Knob pointer drag", () => {
  it("captures the pointer and brackets press, move, and release as one gesture", () => {
    const capture = vi.spyOn(Element.prototype, "setPointerCapture");
    const g = bracketSpy();
    const values: number[] = [];
    render(
      <Knob
        label="Cutoff"
        value={100}
        min={0}
        max={127}
        onChange={(v) => values.push(v)}
        {...g.handlers}
      />,
    );
    const knob = screen.getByRole("slider", { name: "Cutoff" });

    fireEvent.pointerMove(knob, { pointerId: 7, clientY: 190 });
    expect(values).toEqual([]); // a move with no press moves nothing

    fireEvent.pointerDown(knob, { pointerId: 7, clientY: 200 });
    fireEvent.pointerMove(knob, { pointerId: 7, clientY: 190 });
    fireEvent.pointerMove(knob, { pointerId: 7, clientY: 160 });
    fireEvent.pointerUp(knob, { pointerId: 7 });

    // The pointer is captured, so a drag that leaves the 44 pixel
    // square keeps steering the knob.
    expect(capture).toHaveBeenCalledWith(7);
    // Up is a rise: 10 pixels of 180 sweeps the 127 wide span by 7.
    expect(values).toEqual([107, 127]);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });

    // Released. Later moves are not part of anything.
    fireEvent.pointerMove(knob, { pointerId: 7, clientY: 100 });
    expect(values).toEqual([107, 127]);
    capture.mockRestore();
  });

  it("closes the bracket on a cancelled pointer", () => {
    const g = bracketSpy();
    render(
      <Knob label="Cutoff" value={100} min={0} max={127} onChange={() => 0} {...g.handlers} />,
    );
    const knob = screen.getByRole("slider", { name: "Cutoff" });
    fireEvent.pointerDown(knob, { pointerId: 1, clientY: 200 });
    fireEvent.pointerCancel(knob, { pointerId: 1 });
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("closes the bracket when the pointer capture is lost", () => {
    const g = bracketSpy();
    render(
      <Knob label="Cutoff" value={100} min={0} max={127} onChange={() => 0} {...g.handlers} />,
    );
    const knob = screen.getByRole("slider", { name: "Cutoff" });
    fireEvent.pointerDown(knob, { pointerId: 1, clientY: 200 });
    fireEvent.lostPointerCapture(knob, { pointerId: 1 });
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("closes the bracket when the knob unmounts mid-drag", () => {
    const g = bracketSpy();
    const { unmount } = render(
      <Knob label="Cutoff" value={100} min={0} max={127} onChange={() => 0} {...g.handlers} />,
    );
    const knob = screen.getByRole("slider", { name: "Cutoff" });
    fireEvent.pointerDown(knob, { pointerId: 1, clientY: 200 });
    fireEvent.pointerMove(knob, { pointerId: 1, clientY: 180 });
    unmount();
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("does not close a bracket the drag already closed", () => {
    const g = bracketSpy();
    const { unmount } = render(
      <Knob label="Cutoff" value={100} min={0} max={127} onChange={() => 0} {...g.handlers} />,
    );
    const knob = screen.getByRole("slider", { name: "Cutoff" });
    fireEvent.pointerDown(knob, { pointerId: 1, clientY: 200 });
    fireEvent.pointerUp(knob, { pointerId: 1 });
    fireEvent.pointerCancel(knob, { pointerId: 1 });
    unmount();
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("writes nothing when the drag runs into a rail", () => {
    const g = bracketSpy();
    const values: number[] = [];
    render(
      <Knob
        label="Cutoff"
        value={127}
        min={0}
        max={127}
        onChange={(v) => values.push(v)}
        {...g.handlers}
      />,
    );
    const knob = screen.getByRole("slider", { name: "Cutoff" });
    fireEvent.pointerDown(knob, { pointerId: 1, clientY: 200 });
    fireEvent.pointerMove(knob, { pointerId: 1, clientY: 150 });
    fireEvent.pointerUp(knob, { pointerId: 1 });
    // The value the core already holds is not an edit.
    expect(values).toEqual([]);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("brackets a run of auto-repeat arrow keys as one gesture", () => {
    const g = bracketSpy();
    const values: number[] = [];
    render(
      <Knob
        label="Cutoff"
        value={100}
        min={0}
        max={127}
        onChange={(v) => values.push(v)}
        {...g.handlers}
      />,
    );
    const knob = screen.getByRole("slider", { name: "Cutoff" });
    fireEvent.keyDown(knob, { key: "ArrowDown" });
    fireEvent.keyDown(knob, { key: "ArrowDown", repeat: true });
    fireEvent.keyDown(knob, { key: "ArrowDown", repeat: true });
    fireEvent.keyUp(knob, { key: "ArrowDown" });
    // The key keeps working; the run lands one undo step (R24).
    expect(values).toEqual([99, 99, 99]);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("closes a held arrow key's bracket when the knob unmounts or blurs", () => {
    const g = bracketSpy();
    const { unmount } = render(
      <Knob label="Cutoff" value={100} min={0} max={127} onChange={() => 0} {...g.handlers} />,
    );
    const knob = screen.getByRole("slider", { name: "Cutoff" });
    fireEvent.keyDown(knob, { key: "ArrowUp" });
    fireEvent.blur(knob);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });

    fireEvent.keyDown(knob, { key: "ArrowUp" });
    unmount();
    expect(g.calls).toEqual({ begins: 2, commits: 2 });
  });

  it("keeps the drag's bracket open when a key is released mid-drag", () => {
    const g = bracketSpy();
    render(
      <Knob label="Cutoff" value={100} min={0} max={127} onChange={() => 0} {...g.handlers} />,
    );
    const knob = screen.getByRole("slider", { name: "Cutoff" });
    fireEvent.pointerDown(knob, { pointerId: 1, clientY: 200 });
    fireEvent.keyDown(knob, { key: "ArrowUp" });
    fireEvent.keyUp(knob, { key: "ArrowUp" });
    expect(g.calls).toEqual({ begins: 1, commits: 0 });
    fireEvent.pointerUp(knob, { pointerId: 1 });
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });
});

describe("RangeSlider pointer drag", () => {
  it("drags the nearer handle and brackets the drag as one gesture", () => {
    const capture = vi.spyOn(Element.prototype, "setPointerCapture");
    const g = bracketSpy();
    const calls: [number, number][] = [];
    render(
      <RangeSlider
        label="key range"
        lo={10}
        hi={20}
        onChange={(lo, hi) => calls.push([lo, hi])}
        {...g.handlers}
      />,
    );
    const rail = screen.getByRole("group", { name: "key range" });
    // 220 wide over 0 to 127: the low handle sits near x 22.
    fireEvent.pointerDown(rail, { pointerId: 3, clientX: 20 });
    fireEvent.pointerMove(rail, { pointerId: 3, clientX: 30 });
    fireEvent.pointerMove(rail, { pointerId: 3, clientX: 35 });
    fireEvent.pointerUp(rail, { pointerId: 3 });

    expect(capture).toHaveBeenCalledWith(3);
    expect(calls).toEqual([
      [15, 20],
      [18, 20],
    ]);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
    capture.mockRestore();
  });

  it("picks the high handle when the press lands nearer to it", () => {
    const g = bracketSpy();
    const calls: [number, number][] = [];
    render(
      <RangeSlider
        label="key range"
        lo={10}
        hi={20}
        onChange={(lo, hi) => calls.push([lo, hi])}
        {...g.handlers}
      />,
    );
    const rail = screen.getByRole("group", { name: "key range" });
    fireEvent.pointerDown(rail, { pointerId: 1, clientX: 100 });
    fireEvent.pointerMove(rail, { pointerId: 1, clientX: 60 });
    fireEvent.pointerCancel(rail, { pointerId: 1 });
    expect(calls).toEqual([[10, 33]]);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("closes the bracket when the slider unmounts mid-drag", () => {
    const g = bracketSpy();
    const { unmount } = render(
      <RangeSlider label="key range" lo={10} hi={20} onChange={() => 0} {...g.handlers} />,
    );
    const rail = screen.getByRole("group", { name: "key range" });
    fireEvent.pointerDown(rail, { pointerId: 1, clientX: 20 });
    fireEvent.pointerMove(rail, { pointerId: 1, clientX: 30 });
    unmount();
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("brackets a run of auto-repeat arrow keys on a handle", () => {
    const g = bracketSpy();
    const calls: [number, number][] = [];
    render(
      <RangeSlider
        label="key range"
        lo={10}
        hi={20}
        onChange={(lo, hi) => calls.push([lo, hi])}
        {...g.handlers}
      />,
    );
    const low = screen.getByRole("slider", { name: "key range low" });
    fireEvent.keyDown(low, { key: "ArrowRight" });
    fireEvent.keyDown(low, { key: "ArrowRight", repeat: true });
    fireEvent.keyUp(low, { key: "ArrowRight" });
    expect(calls).toEqual([
      [11, 20],
      [11, 20],
    ]);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });
});

describe("MatrixGrid pointer drag", () => {
  it("captures the pointer and brackets a cell drag as one gesture", () => {
    const capture = vi.spyOn(Element.prototype, "setPointerCapture");
    const g = bracketSpy();
    const calls: [number, number, number][] = [];
    render(
      <MatrixGrid
        matrix={emptyMatrix()}
        onChange={(r, c, v) => calls.push([r, c, v])}
        {...g.handlers}
      />,
    );
    const cell = screen.getByRole("spinbutton", { name: "Mod wheel to LFO pitch" });
    fireEvent.pointerDown(cell, { pointerId: 5, clientY: 100 });
    fireEvent.pointerMove(cell, { pointerId: 5, clientY: 60 });
    fireEvent.pointerMove(cell, { pointerId: 5, clientY: 30 });
    fireEvent.pointerUp(cell, { pointerId: 5 });

    expect(capture).toHaveBeenCalledWith(5);
    // A pixel up is a unit of the 0 to 127 cell.
    expect(calls).toEqual([
      [0, 0, 40],
      [0, 0, 70],
    ]);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
    capture.mockRestore();
  });

  it("closes the bracket when the grid unmounts mid-drag", () => {
    const g = bracketSpy();
    const { unmount } = render(
      <MatrixGrid matrix={emptyMatrix()} onChange={() => 0} {...g.handlers} />,
    );
    const cell = screen.getByRole("spinbutton", { name: "Mod wheel to LFO pitch" });
    fireEvent.pointerDown(cell, { pointerId: 1, clientY: 100 });
    fireEvent.pointerMove(cell, { pointerId: 1, clientY: 60 });
    unmount();
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("writes nothing when a drag or a double click leaves the cell alone", () => {
    const g = bracketSpy();
    const calls: [number, number, number][] = [];
    const matrix = emptyMatrix();
    matrix[0]?.splice(0, 1, 127);
    render(
      <MatrixGrid matrix={matrix} onChange={(r, c, v) => calls.push([r, c, v])} {...g.handlers} />,
    );
    const lit = screen.getByRole("spinbutton", { name: "Mod wheel to LFO pitch" });
    fireEvent.pointerDown(lit, { pointerId: 1, clientY: 100 });
    fireEvent.pointerMove(lit, { pointerId: 1, clientY: 50 });
    fireEvent.pointerUp(lit, { pointerId: 1 });
    // Double clicking a cell that already reads zero clears nothing.
    fireEvent.dblClick(screen.getByRole("spinbutton", { name: "Aftertouch to DCQ" }));
    expect(calls).toEqual([]);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("brackets a run of auto-repeat arrow keys on a cell", () => {
    const g = bracketSpy();
    const calls: [number, number, number][] = [];
    render(
      <MatrixGrid
        matrix={emptyMatrix()}
        onChange={(r, c, v) => calls.push([r, c, v])}
        {...g.handlers}
      />,
    );
    const cell = screen.getByRole("spinbutton", { name: "Foot pedal to DCA" });
    fireEvent.keyDown(cell, { key: "ArrowUp" });
    fireEvent.keyDown(cell, { key: "ArrowUp", repeat: true });
    fireEvent.keyDown(cell, { key: "ArrowUp", repeat: true });
    fireEvent.keyUp(cell, { key: "ArrowUp" });
    expect(calls).toEqual([
      [1, 4, 1],
      [1, 4, 1],
      [1, 4, 1],
    ]);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });
});

describe("EnvelopeEditor stage drag", () => {
  it("drags a stage node and brackets the drag as one gesture", () => {
    const g = bracketSpy();
    const calls: EnvelopeSnapshot[] = [];
    render(
      <EnvelopeEditor
        envelope={ENV}
        label="DCA"
        onChange={(sustain, end, rates, stops) => calls.push({ sustain, end, rates, stops })}
        {...g.handlers}
      />,
    );
    const graph = screen.getByRole("group", { name: "DCA envelope graph" });
    stubBox(graph, 1000, 170);
    const node = screen.getByTestId("dca-node-1");

    fireEvent.pointerDown(node, { pointerId: 2, clientX: 200, clientY: 20 });
    fireEvent.pointerMove(graph, { pointerId: 2, clientX: 260, clientY: 80 });
    fireEvent.pointerUp(graph, { pointerId: 2 });

    // Right is slower: 60 pixels take stage 1's rate from 80 to 44.
    expect(calls[0]?.rates[0]).toBe(44);
    expect(calls[0]?.stops[0]).toBe(53);
    expect(calls).toHaveLength(1);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("closes the bracket when the editor unmounts mid-drag", () => {
    const g = bracketSpy();
    const { unmount } = render(
      <EnvelopeEditor envelope={ENV} label="DCA" onChange={() => 0} {...g.handlers} />,
    );
    const graph = screen.getByRole("group", { name: "DCA envelope graph" });
    stubBox(graph, 1000, 170);
    fireEvent.pointerDown(screen.getByTestId("dca-node-1"), {
      pointerId: 1,
      clientX: 200,
      clientY: 20,
    });
    fireEvent.pointerMove(graph, { pointerId: 1, clientX: 260, clientY: 80 });
    unmount();
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });

  it("writes nothing when the stage lands where it already was", () => {
    const g = bracketSpy();
    const calls: EnvelopeSnapshot[] = [];
    // Stage 1 sits on both rails: rate 0 and the top level.
    const railed: EnvelopeSnapshot = {
      ...ENV,
      rates: [0, ...ENV.rates.slice(1)],
      stops: [...ENV.stops],
    };
    render(
      <EnvelopeEditor
        envelope={railed}
        label="DCA"
        onChange={(sustain, end, rates, stops) => calls.push({ sustain, end, rates, stops })}
        {...g.handlers}
      />,
    );
    const graph = screen.getByRole("group", { name: "DCA envelope graph" });
    stubBox(graph, 1000, 170);
    fireEvent.pointerDown(screen.getByTestId("dca-node-1"), {
      pointerId: 1,
      clientX: 100,
      clientY: 5,
    });
    fireEvent.pointerMove(graph, { pointerId: 1, clientX: 200, clientY: 5 });
    fireEvent.pointerUp(graph, { pointerId: 1 });
    expect(calls).toEqual([]);
    expect(g.calls).toEqual({ begins: 1, commits: 1 });
  });
});

describe("the editor that unmounts mid-drag", () => {
  it("leaves the drag undoable and the history usable", async () => {
    await openInstrumentDisk();
    const cutoff = await screen.findByRole("slider", { name: "Cutoff" });
    fireEvent.pointerDown(cutoff, { pointerId: 1, clientY: 200 });
    fireEvent.pointerMove(cutoff, { pointerId: 1, clientY: 220 });
    await waitFor(() => {
      expect(screen.getByRole("slider", { name: "Cutoff" }).getAttribute("aria-valuenow")).toBe(
        "113",
      );
    });

    // The tab switch takes the voice editor out of the document with
    // the pointer still down, which is what a mid-drag undo does when
    // it pops the import.
    fireEvent.click(screen.getByRole("tab", { name: "Effects" }));
    await screen.findByText("Controller modulation matrix");

    const bend = screen.getByLabelText("Bend range (1/8 semi)");
    fireEvent.change(bend, { target: { value: "24" } });
    fireEvent.blur(bend);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("Bend range (1/8 semi)").value).toBe("24");
    });

    // A stale bracket swallows this edit and every later one.
    const undo = screen.getByRole<HTMLButtonElement>("button", { name: "Undo" });
    expect(undo.disabled).toBe(false);
    fireEvent.click(undo);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("Bend range (1/8 semi)").value).toBe("16");
    });

    // The drag is a step of its own, so one more undo returns it.
    fireEvent.click(screen.getByRole("tab", { name: "Voices" }));
    expect(screen.getByRole("slider", { name: "Cutoff" }).getAttribute("aria-valuenow")).toBe(
      "113",
    );
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    await waitFor(() => {
      expect(screen.getByRole("slider", { name: "Cutoff" }).getAttribute("aria-valuenow")).toBe(
        "127",
      );
    });
  });

  it("lands one undo step for a held arrow key, not one per repeat", async () => {
    await openInstrumentDisk();
    const cutoff = await screen.findByRole("slider", { name: "Cutoff" });
    // Auto-repeat outruns the core, so every repeat of the run reads
    // the same confirmed value. The point is the history, not the value.
    fireEvent.keyDown(cutoff, { key: "ArrowDown" });
    fireEvent.keyDown(cutoff, { key: "ArrowDown", repeat: true });
    fireEvent.keyDown(cutoff, { key: "ArrowDown", repeat: true });
    fireEvent.keyDown(cutoff, { key: "ArrowDown", repeat: true });
    fireEvent.keyUp(cutoff, { key: "ArrowDown" });
    await waitFor(() => {
      expect(screen.getByRole("slider", { name: "Cutoff" }).getAttribute("aria-valuenow")).toBe(
        "126",
      );
    });

    // An unrelated edit, then two undos: one for the edit, one for the
    // whole held key.
    const tune = screen.getByLabelText("Tune (cents)");
    fireEvent.change(tune, { target: { value: "5" } });
    fireEvent.blur(tune);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("Tune (cents)").value).toBe("5");
    });
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("Tune (cents)").value).toBe("0");
    });
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    await waitFor(() => {
      expect(screen.getByRole("slider", { name: "Cutoff" }).getAttribute("aria-valuenow")).toBe(
        "127",
      );
    });
  });
});
