// The mockup's effects screen (R19) over the real core: the pitch
// bend range in the hardware's 1/8 semitone unit, and the 3 by 7
// controller modulation matrix.
import type { EffectsSnapshot } from "../boundary/contract";
import { MatrixGrid } from "../ui/MatrixGrid";
import { Stepper } from "../ui/Stepper";

export interface EffectsScreenProps {
  effects: EffectsSnapshot;
  onSetCell: (controller: number, target: number, value: number) => void;
  onSetBend: (value: number) => void;
  onGestureBegin: () => void;
  onGestureCommit: () => void;
}

export function EffectsScreen({
  effects,
  onSetCell,
  onSetBend,
  onGestureBegin,
  onGestureCommit,
}: EffectsScreenProps) {
  return (
    <div className="centered">
      <div className="panel">
        <h2>Pitch bend</h2>
        <div className="row">
          <Stepper
            label="Bend range (1/8 semi)"
            value={effects.bendRange}
            min={0}
            max={127}
            onChange={onSetBend}
          />
        </div>
      </div>
      <div className="panel">
        <h2>Controller modulation matrix</h2>
        <MatrixGrid
          matrix={effects.matrix}
          onChange={onSetCell}
          onGestureBegin={onGestureBegin}
          onGestureCommit={onGestureCommit}
        />
      </div>
    </div>
  );
}
