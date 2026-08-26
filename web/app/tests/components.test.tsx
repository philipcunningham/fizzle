// The pure controls: driven by props and callbacks, exercised the way
// the screens use them.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CapacityBar } from "../src/ui/CapacityBar";
import { EnvelopeEditor } from "../src/ui/EnvelopeEditor";
import { Knob } from "../src/ui/Knob";
import { MatrixGrid } from "../src/ui/MatrixGrid";
import { RangeSlider } from "../src/ui/RangeSlider";
import { SelectControl } from "../src/ui/SelectControl";
import { Stepper } from "../src/ui/Stepper";
import { Waveform } from "../src/ui/Waveform";
import { noteName, parseNote } from "../src/ui/notes";

describe("Knob", () => {
  it("steps by arrow key and clamps", () => {
    const values: number[] = [];
    render(<Knob label="Cutoff" value={127} min={0} max={127} onChange={(v) => values.push(v)} />);
    const knob = screen.getByRole("slider", { name: "Cutoff" });
    // Up at the rail writes nothing: the value the core already holds
    // is not an edit, and writing it lands a phantom undo step.
    fireEvent.keyDown(knob, { key: "ArrowUp" });
    fireEvent.keyDown(knob, { key: "ArrowDown" });
    expect(values).toEqual([126]);
    expect(knob.getAttribute("aria-valuenow")).toBe("127");
  });
});

describe("Stepper", () => {
  it("steps, shift-steps, and accepts typed values", () => {
    const values: number[] = [];
    render(<Stepper label="volume" value={50} min={0} max={99} onChange={(v) => values.push(v)} />);
    fireEvent.click(screen.getByRole("button", { name: "increase volume" }));
    fireEvent.click(screen.getByRole("button", { name: "decrease volume" }), { shiftKey: true });
    const field = screen.getByLabelText("volume");
    fireEvent.change(field, { target: { value: "88" } });
    fireEvent.blur(field);
    expect(values).toEqual([51, 40, 88]);
  });
});

describe("RangeSlider", () => {
  it("nudges each handle by keyboard within bounds", () => {
    const calls: [number, number][] = [];
    render(
      <RangeSlider label="key range" lo={10} hi={20} onChange={(lo, hi) => calls.push([lo, hi])} />,
    );
    fireEvent.keyDown(screen.getByRole("slider", { name: "key range low" }), {
      key: "ArrowRight",
    });
    fireEvent.keyDown(screen.getByRole("slider", { name: "key range high" }), {
      key: "ArrowLeft",
    });
    expect(calls).toEqual([
      [11, 20],
      [10, 19],
    ]);
  });
});

describe("SelectControl", () => {
  it("renders the labelled trigger with the current value", () => {
    render(
      <SelectControl
        label="Playback"
        value="normal"
        options={["normal", "reverse"]}
        onChange={() => 0}
      />,
    );
    const trigger = screen.getByRole("combobox", { name: "Playback" });
    expect(trigger.textContent).toContain("normal");
  });
});

const MB = 1024 * 1024;

describe("CapacityBar", () => {
  it("counts down the disk's free space, and knows a two disk set", () => {
    render(<CapacityBar usedBytes={655_360} disks={1} audioBytes={0} memoryBytes={MB} />);
    const bar = screen.getByRole("status", { name: "disk free" });
    expect(bar.textContent).toContain("50%");
    expect(bar.textContent).toContain("640.0 KB used · 50% disk free");
    expect(bar.textContent).not.toContain("two disk");

    render(<CapacityBar usedBytes={1_500_000} disks={2} audioBytes={0} memoryBytes={MB} />);
    const two = screen.getAllByRole("status", { name: "disk free" })[1];
    expect(two?.textContent).toContain("two disk set");
  });

  // The sampler's memory is the second ceiling a user can hit (R29),
  // measured against the machine they declared rather than the floppy.
  it("counts down the sampler's memory beside the disk", () => {
    render(<CapacityBar usedBytes={0} disks={1} audioBytes={MB / 2} memoryBytes={MB} />);
    const mem = screen.getByRole("status", { name: "memory free" });
    expect(mem.textContent).toContain("50% memory free");
  });

  // Nothing reads past 100 and nothing reads below zero: an instrument
  // twice the size of the memory leaves none of it, not minus 100.
  it("floors free space at nothing rather than going negative", () => {
    render(<CapacityBar usedBytes={0} disks={1} audioBytes={MB * 2} memoryBytes={MB} />);
    const mem = screen.getByRole("status", { name: "memory free" });
    expect(mem.textContent).toContain("0% memory free");
    expect(mem.className).toContain("over");
  });

  // Three things the disk reading has always done, which the second
  // reading must not cost it.
  it("keeps the disk reading's own warnings", () => {
    // A two disk set is worth noticing on sight, whatever its fill.
    render(<CapacityBar usedBytes={100} disks={2} audioBytes={0} memoryBytes={MB} />);
    expect(screen.getByRole("status", { name: "disk free" }).className).toContain("warn");

    // Red arrives before the last byte goes, not with it.
    render(<CapacityBar usedBytes={1_300_000} disks={1} audioBytes={0} memoryBytes={MB} />);
    const tight = screen.getAllByRole("status", { name: "disk free" })[1];
    expect(tight?.className).toContain("over");
  });

  it("marks where disk 1 ends on a two disk set", () => {
    const { container } = render(
      <CapacityBar usedBytes={1_800_000} disks={2} audioBytes={0} memoryBytes={MB} />,
    );
    expect(container.querySelector(".tick")).not.toBeNull();
  });

  it("stays quiet while the memory has room", () => {
    render(<CapacityBar usedBytes={0} disks={1} audioBytes={MB / 4} memoryBytes={MB} />);
    expect(screen.getByRole("status", { name: "memory free" }).className).not.toContain("over");
  });

  // A disk with no instrument asks the sampler for nothing, so all of
  // its memory is available.
  it("reports the whole machine free with no instrument", () => {
    render(<CapacityBar usedBytes={0} disks={1} audioBytes={0} memoryBytes={MB} />);
    expect(screen.getByRole("status", { name: "memory free" }).textContent).toContain(
      "100% memory free",
    );
  });
});

describe("MatrixGrid", () => {
  it("names the effects table for assistive technology", () => {
    render(<MatrixGrid matrix={[]} onChange={() => undefined} />);
    expect(screen.getByRole("table", { name: "effects modulation matrix" })).toBeTruthy();
  });

  it("edits cells by arrow key and zeroes on double click", () => {
    const calls: [number, number, number][] = [];
    const matrix = Array.from({ length: 3 }, () => new Array<number>(7).fill(0));
    matrix[0]?.splice(0, 1, 32);
    render(<MatrixGrid matrix={matrix} onChange={(r, c, v) => calls.push([r, c, v])} />);
    const cell = screen.getByRole("spinbutton", { name: "Mod wheel to LFO pitch" });
    fireEvent.keyDown(cell, { key: "ArrowUp" });
    fireEvent.dblClick(cell);
    // Down at zero writes nothing: the value the core already holds is
    // not an edit, and writing it lands a phantom undo step.
    const other = screen.getByRole("spinbutton", { name: "Aftertouch to DCQ" });
    fireEvent.keyDown(other, { key: "ArrowDown" });
    expect(calls).toEqual([
      [0, 0, 33],
      [0, 0, 0],
    ]);
  });
});

describe("EnvelopeEditor", () => {
  const env = {
    sustain: 2,
    end: 5,
    rates: [80, 70, 60, 50, 40, 30, 20, 10],
    stops: [99, 90, 80, 70, 60, 50, 40, 30],
  };

  it("edits a stage rate from the grid in display scale", () => {
    const calls: { sustain: number; end: number; rates: number[]; stops: number[] }[] = [];
    render(
      <EnvelopeEditor
        envelope={env}
        label="DCA"
        onChange={(sustain, end, rates, stops) => calls.push({ sustain, end, rates, stops })}
      />,
    );
    const rate = screen.getByLabelText("DCA stage 2 rate");
    fireEvent.change(rate, { target: { value: "65" } });
    fireEvent.blur(rate);
    expect(calls[0]?.rates[1]).toBe(65);
    // Typed values clamp to the display scale. Stage 2 sits at 90, so
    // the clamped 99 is a real change; a stage already at 99 would be
    // suppressed as the phantom write it is.
    const level = screen.getByLabelText("DCA stage 2 level");
    fireEvent.change(level, { target: { value: "500" } });
    fireEvent.blur(level);
    expect(calls[1]?.stops[1]).toBe(99);
  });

  it("moves the sustain and end designations", () => {
    const calls: [number, number][] = [];
    render(
      <EnvelopeEditor
        envelope={env}
        label="DCF"
        onChange={(sustain, end) => calls.push([sustain, end])}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "DCF set sustain stage 4" }));
    fireEvent.click(screen.getByRole("button", { name: "DCF set end stage 7" }));
    expect(calls).toEqual([
      [3, 5],
      [2, 6],
    ]);
  });
});

describe("Waveform", () => {
  it("keeps the numeric loop fields working without a canvas (jsdom)", () => {
    const calls: [number, number][] = [];
    render(
      <Waveform
        voiceKey="slot-0"
        frames={2000}
        peaks={new Int16Array(64)}
        loopIndex={0}
        loop={{ start: 100, end: 900, xf: 0, tm: 0 }}
        sustain={false}
        release={false}
        onSetLoop={(s, e) => calls.push([s, e])}
      />,
    );
    const s = screen.getByLabelText("loop start frame");
    fireEvent.change(s, { target: { value: "150" } });
    fireEvent.blur(s);
    const e = screen.getByLabelText("loop end frame");
    fireEvent.change(e, { target: { value: "9999" } });
    fireEvent.blur(e);
    expect(calls).toEqual([
      [150, 900],
      [100, 2000],
    ]);
  });
});

describe("field commit discipline (QA fixes)", () => {
  it("Stepper reads back note names and reverts unreadable input", () => {
    const values: number[] = [];
    render(
      <Stepper
        label="Key high"
        value={66}
        min={0}
        max={127}
        format={noteName}
        parse={parseNote}
        onChange={(v) => values.push(v)}
      />,
    );
    const field = screen.getByLabelText<HTMLInputElement>("Key high");
    expect(field.value).toBe("F#4");

    // A note name commits as its MIDI number, not as the stray digit.
    fireEvent.change(field, { target: { value: "G4" } });
    fireEvent.blur(field);
    expect(values).toEqual([67]);

    // Empty and non-note text revert; nothing is committed.
    fireEvent.change(field, { target: { value: "" } });
    fireEvent.blur(field);
    fireEvent.change(field, { target: { value: "zzz" } });
    fireEvent.blur(field);
    expect(values).toEqual([67]);
    expect(screen.getByLabelText<HTMLInputElement>("Key high").value).toBe("F#4");
  });

  it("a clamp back to the shown value still resets the field", () => {
    // The core confirms 127; the field must not keep the typed 999.
    render(<Stepper label="Cutoff" value={127} min={0} max={127} onChange={() => undefined} />);
    const field = screen.getByLabelText<HTMLInputElement>("Cutoff");
    fireEvent.change(field, { target: { value: "999" } });
    fireEvent.blur(field);
    expect(screen.getByLabelText<HTMLInputElement>("Cutoff").value).toBe("127");
  });

  it("CapacityBar reads a two disk set against the pair, not one disk", () => {
    render(
      <CapacityBar usedBytes={1_800_000} disks={2} audioBytes={0} memoryBytes={1024 * 1024} />,
    );
    const bar = screen.getByRole("status", { name: "disk free" });
    // 1.8 MB of a 2.5 MB pair leaves 31 percent. Measured against one
    // disk it would be spent twice over, and floor at nothing.
    expect(bar.textContent).toContain("31%");
    expect(bar.textContent).not.toContain("0%");
  });

  it("a lit matrix cell keeps the focus outline available", () => {
    const matrix = Array.from({ length: 3 }, () => new Array<number>(7).fill(0));
    matrix[0]?.splice(0, 1, 64);
    render(<MatrixGrid matrix={matrix} onChange={() => undefined} />);
    const cell = screen.getByRole("spinbutton", { name: "Mod wheel to LFO pitch" });
    // The value border is a shadow, so the stylesheet's focus outline
    // is not overridden by an inline one.
    expect(cell.style.boxShadow).not.toBe("");
    expect(cell.style.outline).toBe("");
  });
});
