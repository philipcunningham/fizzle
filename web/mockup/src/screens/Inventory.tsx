// Component inventory: live stories for the leaf controls, one of the kept
// Step 0 artifacts. Each control runs on local state, independent of the
// document.

import { useRef, useState } from "react";
import { EnvelopeEditor } from "../components/EnvelopeEditor";
import { Keyboard } from "../components/Keyboard";
import { Knob } from "../components/Knob";
import { MatrixGrid } from "../components/MatrixGrid";
import { RangeSlider } from "../components/RangeSlider";
import type { Envelope } from "../data/model";
import { noteName } from "../data/model";
import { auditionStart } from "../audio";
import { makeVoice } from "../data/seed";

const demoVoice = makeVoice("DEMO", 60, 48, 72, 4242);

export function Inventory() {
  const [knob, setKnob] = useState(64);
  const heldNotes = useRef(new Map<number, () => void>());
  const [range, setRange] = useState({ lo: 48, hi: 72 });
  const [env, setEnv] = useState<Envelope>(demoVoice.dcaEnv);
  const [matrix, setMatrix] = useState<number[][]>([
    [32, 0, 0, 0, 0, 0, 0],
    [0, 0, 0, 0, 0, 0, 0],
    [0, 0, 0, 0, 0, 12, 0],
  ]);

  return (
    <div className="inventory">
      <p style={{ color: "var(--fz-fg-faint)" }}>
        Leaf control stories, kept from Step 0. Every control is keyboard operable and uses the terminal tokens.
      </p>

      <section>
        <h3>Knob</h3>
        <Knob label="Cutoff" value={knob} min={0} max={127} onChange={setKnob} />
      </section>

      <section>
        <h3>Range slider</h3>
        <RangeSlider lo={range.lo} hi={range.hi} format={noteName} onChange={(lo, hi) => setRange({ lo, hi })} />
        <p style={{ color: "var(--fz-fg-dim)" }}>
          {noteName(range.lo)} to {noteName(range.hi)}
        </p>
      </section>

      <section>
        <h3>Envelope editor</h3>
        <EnvelopeEditor label="DCA envelope" envelope={env} onChange={setEnv} />
      </section>

      <section>
        <h3>Matrix grid</h3>
        <MatrixGrid
          matrix={matrix}
          onChange={(r, c, v) => setMatrix((m) => m.map((row, ri) => row.map((cell, ci) => (ri === r && ci === c ? v : cell))))}
        />
      </section>

      <section>
        <h3>Keyboard</h3>
        <Keyboard
          highlight={[range]}
          rootKey={60}
          onNoteOn={(note, velocity) => {
            heldNotes.current.get(note)?.();
            const release = auditionStart(demoVoice, note, velocity);
            if (release) heldNotes.current.set(note, release);
          }}
          onNoteOff={(note) => {
            heldNotes.current.get(note)?.();
            heldNotes.current.delete(note);
          }}
        />
      </section>
    </div>
  );
}
