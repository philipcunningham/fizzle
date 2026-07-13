// Effects matrix: 3 controllers by 7 targets, data driven, every cell
// editable. Cells are draggable steppers; double click zeroes a cell.

import { useRef } from "react";
import { EFFECT_CONTROLLERS, EFFECT_TARGETS, clamp } from "../data/model";

interface Props {
  matrix: number[][];
  onChange: (controller: number, target: number, value: number) => void;
  onGestureBegin?: () => void;
  onGestureCommit?: () => void;
}

export function MatrixGrid({ matrix, onChange, onGestureBegin, onGestureCommit }: Props) {
  const drag = useRef<{ r: number; c: number; y: number; v: number } | null>(null);

  return (
    <table className="term matrix">
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
            <td style={{ color: "var(--fz-accent-bright)" }}>{ctl}</td>
            {EFFECT_TARGETS.map((_, c) => {
              const v = matrix[r][c];
              return (
                <td key={c}>
                  <div
                    role="spinbutton"
                    aria-label={`${ctl} to ${EFFECT_TARGETS[c]}`}
                    aria-valuenow={v}
                    aria-valuemin={0}
                    aria-valuemax={127}
                    tabIndex={0}
                    style={{
                      cursor: "ns-resize",
                      textAlign: "center",
                      padding: "4px 6px",
                      borderRadius: 3,
                      touchAction: "none",
                      color: v > 0 ? "var(--fz-fg)" : "#5a5a5a",
                      background: v > 0 ? `rgba(0, 139, 139, ${0.15 + (v / 127) * 0.5})` : "transparent",
                      outline: v > 0 ? "1px solid var(--fz-accent)" : "none",
                      outlineOffset: -1,
                    }}
                    onPointerDown={(e) => {
                      drag.current = { r, c, y: e.clientY, v };
                      e.currentTarget.setPointerCapture(e.pointerId);
                      onGestureBegin?.();
                    }}
                    onPointerMove={(e) => {
                      if (!drag.current || drag.current.r !== r || drag.current.c !== c) return;
                      onChange(r, c, clamp(drag.current.v + (drag.current.y - e.clientY), 0, 127));
                    }}
                    onPointerUp={() => {
                      drag.current = null;
                      onGestureCommit?.();
                    }}
                    onDoubleClick={() => onChange(r, c, 0)}
                    onKeyDown={(e) => {
                      if (e.key === "ArrowUp") onChange(r, c, clamp(v + 1, 0, 127));
                      if (e.key === "ArrowDown") onChange(r, c, clamp(v - 1, 0, 127));
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
