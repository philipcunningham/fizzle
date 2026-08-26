// The accessibility semantics Q5 asks for: state in the accessibility
// tree rather than in hue alone, a focus indicator that survives the
// surface it lands on, one tab stop per composite widget, and an
// accessible name that contains the words on the control (WCAG 2.5.3).
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Core, CoreResult, Snapshot } from "../src/boundary/contract";
import { createScenarioCore } from "./support/scenarioCore";
import { Keyboard } from "../src/ui/Keyboard";
import { Knob } from "../src/ui/Knob";
import { RangeSlider } from "../src/ui/RangeSlider";
import { noteName } from "../src/ui/notes";
import { openInstrumentDisk } from "./helpers";

/**
 * The fake's disk carries the instrument dump alone, leaving no second
 * row to prove the open marker isn't on everything, so one spare file
 * is injected into every snapshot.
 */
function twoFileCore(): Core {
  const inner = createScenarioCore();
  const withSpare = (result: CoreResult<Snapshot>): CoreResult<Snapshot> =>
    result.ok && result.value.disk
      ? {
          ok: true,
          value: {
            ...result.value,
            disk: {
              ...result.value.disk,
              files: [
                ...result.value.disk.files,
                { name: "SPARE.FZV", type: "voice", sizeBytes: 2048 },
              ],
            },
          },
        }
      : result;
  return {
    ...inner,
    snapshot: () => inner.snapshot().then(withSpare),
    openImage: (bytes) => inner.openImage(bytes).then(withSpare),
  };
}

describe("which file is open (Q5, WCAG 1.4.1)", () => {
  it("puts the open state in the accessibility tree, not only in the hue", async () => {
    await openInstrumentDisk(twoFileCore());
    // The piano keys are buttons too, so match the class rather than
    // the role alone.
    const rows = screen.getAllByRole("button").filter((b) => b.classList.contains("filerow"));
    expect(rows.length).toBe(2);

    const current = screen.getAllByRole("button", { current: true });
    expect(current.length).toBe(1);
    expect(current[0]?.textContent).toContain("OPENED");
    expect(
      rows.find((r) => r.textContent.includes("SPARE.FZV"))?.getAttribute("aria-current"),
    ).toBe(null);
  });
});

describe("the piano key focus ring (Q5)", () => {
  it("draws a two tone ring inside the focused key, where nothing clips it", () => {
    render(
      <Keyboard lowNote={36} octaves={2} onNoteOn={() => undefined} onNoteOff={() => undefined} />,
    );
    expect(screen.queryByTestId("key-focus-ring")).toBeNull();

    const key = screen.getByTestId("key-36");
    fireEvent.focus(key);
    const ring = screen.getByTestId("key-focus-ring");

    // Two strokes: no single colour clears 3:1 on both a white key and
    // a highlighted black one, so one of the pair always reads.
    const strokes = Array.from(ring.querySelectorAll("rect")).map((r) => r.getAttribute("stroke"));
    expect(strokes).toEqual(["var(--fz-key-focus-dark)", "var(--fz-key-focus-light)"]);

    // Drawn inside the key: an outline on a full-height rect is clipped
    // by the SVG to two hairlines in the gaps between keys.
    const keyX = Number(key.getAttribute("x"));
    const keyW = Number(key.getAttribute("width"));
    const outer = ring.querySelector("rect");
    const ringX = Number(outer?.getAttribute("x"));
    const ringW = Number(outer?.getAttribute("width"));
    expect(ringX).toBeGreaterThanOrEqual(keyX);
    expect(ringX + ringW).toBeLessThanOrEqual(keyX + keyW);

    fireEvent.blur(key);
    expect(screen.queryByTestId("key-focus-ring")).toBeNull();
  });
});

describe("the tab strip (Q5)", () => {
  it("names a tabpanel for the selected tab and points the tab at it", async () => {
    await openInstrumentDisk();
    const selected = screen.getByRole("tab", { selected: true });
    const panel = screen.getByRole("tabpanel");
    expect(selected.id).not.toBe("");
    expect(panel.id).not.toBe("");
    expect(selected.getAttribute("aria-controls")).toBe(panel.id);
    expect(panel.getAttribute("aria-labelledby")).toBe(selected.id);
  });

  it("holds nothing but tabs inside the tablist", async () => {
    await openInstrumentDisk();
    const list = screen.getByRole("tablist");
    const strays = Array.from(list.children)
      .filter((child) => child.getAttribute("role") !== "tab")
      .map((child) => child.outerHTML);
    expect(strays).toEqual([]);
  });

  it("moves along the strip with the arrow keys from a single tab stop", async () => {
    await openInstrumentDisk();
    const voices = screen.getByRole("tab", { name: "Voices" });
    voices.focus();
    fireEvent.keyDown(voices, { key: "ArrowRight" });

    const banks = await screen.findByRole("tab", { name: "Banks and Areas" });
    await waitFor(() => {
      expect(banks.getAttribute("aria-selected")).toBe("true");
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(banks);
    });
    // One stop for the whole strip: the unselected tabs are skipped.
    expect(screen.getByRole("tab", { name: "Voices" }).tabIndex).toBe(-1);
    expect(banks.tabIndex).toBe(0);
  });
});

describe("RangeSlider semantics (Q5)", () => {
  it("is a group of two handles rather than a slider wrapping sliders", () => {
    render(<RangeSlider label="key range" lo={10} hi={20} onChange={() => undefined} />);
    const group = screen.getByRole("group", { name: "key range" });
    expect(within(group).getAllByRole("slider").length).toBe(2);
    // A slider is a leaf role, so nothing may nest inside one.
    for (const handle of within(group).getAllByRole("slider")) {
      expect(handle.querySelector("[role]")).toBeNull();
    }
  });

  it("speaks each handle the way the label reads it", () => {
    render(
      <RangeSlider
        label="key range"
        lo={37}
        hi={49}
        format={noteName}
        onChange={() => undefined}
      />,
    );
    expect(
      screen.getByRole("slider", { name: "key range low" }).getAttribute("aria-valuetext"),
    ).toBe(noteName(37));
    expect(
      screen.getByRole("slider", { name: "key range high" }).getAttribute("aria-valuetext"),
    ).toBe(noteName(49));
  });
});

describe("Knob semantics (Q5)", () => {
  it("reports the vertical orientation its drag actually has", () => {
    render(<Knob label="Cutoff" value={5} min={0} max={10} onChange={() => undefined} />);
    expect(screen.getByRole("slider", { name: "Cutoff" }).getAttribute("aria-orientation")).toBe(
      "vertical",
    );
  });
});

describe("the bank rename field (Q5)", () => {
  async function openBanksTab() {
    await openInstrumentDisk();
    fireEvent.click(screen.getByRole("tab", { name: "Banks and Areas" }));
    await screen.findByRole("table", { name: "areas" });
  }

  it("is not a child of the bank button, and does not become its name", async () => {
    await openBanksTab();
    fireEvent.doubleClick(screen.getByRole("button", { name: /BANK A \(2\)/ }));
    const field = await screen.findByLabelText("bank name");
    // HTML forbids interactive content inside a button, and nesting the
    // field makes the button's name the field's live value.
    expect(field.closest("button")).toBeNull();
  });

  it("returns focus to the bank button when the rename commits", async () => {
    await openBanksTab();
    fireEvent.doubleClick(screen.getByRole("button", { name: /BANK A \(2\)/ }));
    const field = await screen.findByLabelText("bank name");
    fireEvent.change(field, { target: { value: "DRUMS" } });
    fireEvent.blur(field);

    const renamed = await screen.findByRole("button", { name: /DRUMS \(2\)/ });
    await waitFor(() => {
      expect(document.activeElement).toBe(renamed);
    });
  });
});

describe("accessible names carry the visible label (WCAG 2.5.3)", () => {
  it("names the octave buttons with the words printed on them", async () => {
    await openInstrumentDisk();
    const octaves = screen
      .getAllByRole("button")
      .filter((b) => b.textContent === "- oct" || b.textContent === "+ oct");
    expect(octaves.length).toBe(2);
    for (const button of octaves) {
      const visible = button.textContent.toLowerCase();
      const name = (button.getAttribute("aria-label") ?? button.textContent).toLowerCase();
      expect(name).toContain(visible);
    }
  });
});
