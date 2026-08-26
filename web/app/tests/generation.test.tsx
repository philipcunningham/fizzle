// R14's generation window on the voice editor. It sits beside the
// loops because it is the same kind of value: a sample frame bounded by
// the voice's own length. R17 governs what the cells show, so an edit
// reads back the frame the core confirmed and a value past the voice
// comes back clamped rather than as typed.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import type { Core, InstrumentSnapshot, Snapshot } from "../src/boundary/contract";
import { IMAGE_SIZE } from "../src/boundary/contract";
import { SCENARIO_SCHEMA, createScenarioCore } from "./support/scenarioCore";
import { VoiceEditor } from "../src/screens/VoiceEditor";
import { App } from "../src/shell/App";
import { openInstrumentDisk } from "./helpers";

const noop = () => undefined;

// The editor over a core, with only the generation path wired: the
// screen renders what the core last confirmed, and nothing else in the
// test derives a frame.
function Harness({ core, first }: { core: Core; first: InstrumentSnapshot }) {
  const [instrument, setInstrument] = useState(first);
  const apply = (result: { ok: true; value: Snapshot } | { ok: false }) => {
    if (result.ok && result.value.disk?.instrument) setInstrument(result.value.disk.instrument);
  };
  return (
    <VoiceEditor
      instrument={instrument}
      schema={SCENARIO_SCHEMA}
      selectedSlot={0}
      selectedLoop={0}
      peaks={null}
      playhead={null}
      onSelectVoice={noop}
      onSelectLoop={noop}
      onSetParamNumber={noop}
      onSetParamOption={noop}
      onSetLoop={noop}
      onSetLoopAttr={noop}
      onSetLoopSelect={noop}
      onSetEnvelope={noop}
      onSetGeneration={(slot, start, end) => {
        void core.setSlotGeneration(slot, start, end).then(apply);
      }}
      onRename={noop}
      onMapVoice={noop}
      onExtract={noop}
      onGestureBegin={noop}
      onGestureCommit={noop}
    />
  );
}

async function editorOverFake() {
  const core = createScenarioCore();
  const opened = await core.openImage(new Uint8Array(IMAGE_SIZE));
  if (!opened.ok) throw new Error(opened.error.message);
  const instrument = opened.value.disk?.instrument;
  if (!instrument) throw new Error("the fake opened no instrument");
  render(<Harness core={core} first={instrument} />);
  return core;
}

describe("the generation window", () => {
  it("shows the frames the snapshot carries", async () => {
    await openInstrumentDisk();
    // Slot 0 holds 4096 frames in the fake, generated whole.
    expect((await screen.findByLabelText<HTMLInputElement>("generation start")).value).toBe("0");
    expect(screen.getByLabelText<HTMLInputElement>("generation end").value).toBe("4096");
  });

  it("reads back the frame the core confirmed after an edit", async () => {
    await editorOverFake();
    const start = await screen.findByLabelText("generation start");
    fireEvent.change(start, { target: { value: "512" } });
    fireEvent.blur(start);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("generation start").value).toBe("512");
    });
  });

  it("shows the clamped frame, not the one typed, past the voice's length", async () => {
    await editorOverFake();
    const end = await screen.findByLabelText("generation end");
    fireEvent.change(end, { target: { value: "9999999" } });
    fireEvent.blur(end);
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLInputElement>("generation end").value).toBe("4096");
    });
  });
});

// The cells are wired to the core through the shell, not just rendered:
// an optional callback the app never passes reads the core's frames and
// commits none of them back.
describe("the generation window commits through the shell", () => {
  it("sends the edited frames to the core", async () => {
    const core = createScenarioCore();
    const seen: number[][] = [];
    const watched: typeof core = {
      ...core,
      setSlotGeneration: (slot, start, end) => {
        seen.push([slot, start, end]);
        return core.setSlotGeneration(slot, start, end);
      },
    };
    render(<App core={watched} />);
    const image = new File([new Uint8Array(IMAGE_SIZE)], "TECHNO.img");
    fireEvent.click(await screen.findByRole("button", { name: "Browse" }));
    fireEvent.change(screen.getByLabelText("fz files"), { target: { files: [image] } });
    await screen.findByText("[OPENED]");

    const start = await screen.findByLabelText("generation start");
    fireEvent.change(start, { target: { value: "128" } });
    fireEvent.blur(start);

    await waitFor(() => {
      expect(seen.length).toBe(1);
    });
    expect(seen[0]?.[1]).toBe(128);
  });
});
