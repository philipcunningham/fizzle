// The controller modulation matrix (R19): a 3 by 7 grid of
// drag-and-arrow cells, lit by value.
import { useRef } from "react";
import { EFFECT_CONTROLLERS, EFFECT_TARGETS } from "../boundary/contract";
import { clamp } from "./format";
import { useGestureBracket } from "./gesture";

interface Props {
  matrix: number[][];
  onChange: (controller: number, target: number, value: number) => void;
  onGestureBegin?: () => void;
  onGestureCommit?: () => void;
}

export function MatrixGrid({ matrix, onChange, onGestureBegin, onGestureCommit }: Props) {
  const drag = useRef<{ r: number; c: number; y: number; v: number } | null>(null);
  const gesture = useGestureBracket(onGestureBegin, onGestureCommit);

  return (
    <table className="term matrix" aria-label="effects modulation matrix">
      <thead>
        <tr>
          <th>Controller</th>
          {EFFECT_TARGETS.map((t) => (
            <th key={t}>{t}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {EFFECT_CONTROLLERS.map((ctl, r) => (
          <tr key={ctl}>
            <td className="matrixrow">{ctl}</td>
            {EFFECT_TARGETS.map((target, c) => {
              const v = matrix[r]?.[c] ?? 0;
              // Only a value the core doesn't already hold is an edit:
              // a drag into a rail or a click on a zero cell otherwise
              // lands a phantom undo step and lights the dirty marker.
              const emit = (next: number) => {
                if (next !== v) onChange(r, c, next);
              };
              return (
                <td key={target}>
                  <div
                    role="spinbutton"
                    aria-label={`${ctl} to ${target}`}
                    aria-valuenow={v}
                    aria-valuemin={0}
                    aria-valuemax={127}
                    tabIndex={0}
                    className="matrixcell"
                    // The lit border is a box-shadow, not an outline: an
                    // inline outline beats the stylesheet's
                    // :focus-visible ring and hides keyboard focus (Q5).
                    style={{
                      color: v > 0 ? "var(--fz-fg)" : "var(--fz-fg-faint)",
                      background:
                        v > 0
                          ? `rgba(0, 139, 139, ${String(0.15 + (v / 127) * 0.5)})`
                          : "transparent",
                      boxShadow: v > 0 ? "inset 0 0 0 1px var(--fz-accent)" : "none",
                    }}
                    onPointerDown={(e) => {
                      drag.current = { r, c, y: e.clientY, v };
                      (e.currentTarget as Partial<HTMLDivElement>).setPointerCapture?.(e.pointerId);
                      gesture.begin();
                    }}
                    onPointerMove={(e) => {
                      if (!drag.current || drag.current.r !== r || drag.current.c !== c) return;
                      emit(clamp(drag.current.v + (drag.current.y - e.clientY), 0, 127));
                    }}
                    onPointerUp={() => {
                      drag.current = null;
                      gesture.commit();
                    }}
                    onPointerCancel={() => {
                      drag.current = null;
                      gesture.commit();
                    }}
                    onLostPointerCapture={() => {
                      drag.current = null;
                      gesture.commit();
                    }}
                    onDoubleClick={() => {
                      emit(0);
                    }}
                    // The keyboard is a first-class path (Q5), so a key
                    // auto-repeat run is bracketed like a drag (R24).
                    // See useGestureBracket for why.
                    onKeyDown={(e) => {
                      if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
                      gesture.begin();
                      emit(clamp(v + (e.key === "ArrowUp" ? 1 : -1), 0, 127));
                    }}
                    onKeyUp={(e) => {
                      // A key released under a pointer drag ends
                      // nothing; the release the drag is waiting for
                      // closes the one bracket.
                      if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
                      if (!drag.current) gesture.commit();
                    }}
                    onBlur={() => {
                      if (!drag.current) gesture.commit();
                    }}
                  >
                    {v}
                  </div>
                </td>
              );
            })}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
