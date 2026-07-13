// Effects tab: pitch bend range plus the full 3 by 7 controller
// modulation matrix, including the two parameters some prior tools hide
// (LFO attack lives in the voice LFO group; LFO reso is a matrix column).

import { Stepper } from "../components/Stepper";
import { MatrixGrid } from "../components/MatrixGrid";
import { NoInstrument } from "./NoInstrument";
import { useOpenInstrument, useStore } from "../state/store";

export function EffectsScreen() {
  const { dispatch } = useStore();
  const inst = useOpenInstrument();

  if (!inst) return <NoInstrument />;

  return (
    <div className="centered">
      <div className="panel">
        <h2>Pitch bend</h2>
        <div className="row">
          <Stepper
            label="Bend range (semitones)"
            value={inst.effects.pitchBendRange}
            min={0}
            max={12}
            onChange={(v) => dispatch({ type: "edit-bend", value: v })}
          />
        </div>
      </div>
      <div className="panel">
        <h2>Controller modulation matrix</h2>
        <MatrixGrid
          matrix={inst.effects.matrix}
          onChange={(controller, target, value) => dispatch({ type: "edit-effects", controller, target, value })}
          onGestureBegin={() => dispatch({ type: "gesture-begin" })}
          onGestureCommit={() => dispatch({ type: "gesture-commit" })}
        />
      </div>
    </div>
  );
}
