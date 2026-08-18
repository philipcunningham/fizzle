// The rotary knob: vertical drag sweeps the value, arrow keys step it,
// the arc shows the position. Values are integers, and drags emit
// rounded ones so the core's confirmed value matches the display.
import { useRef } from "react";
import { clamp } from "./format";
import { useGestureBracket } from "./gesture";

interface Props {
  label: string;
  value: number;
  min: number;
  max: number;
  onChange: (v: number) => void;
  onGestureBegin?: () => void;
  onGestureCommit?: () => void;
}

const SWEEP_START = -135;
const SWEEP_END = 135;

function polar(cx: number, cy: number, r: number, angleDeg: number): [number, number] {
  const rad = ((angleDeg - 90) * Math.PI) / 180;
  return [cx + r * Math.cos(rad), cy + r * Math.sin(rad)];
}

function arcPath(cx: number, cy: number, r: number, a0: number, a1: number): string {
  const [x0, y0] = polar(cx, cy, r, a0);
  const [x1, y1] = polar(cx, cy, r, a1);
  const large = a1 - a0 > 180 ? 1 : 0;
  return `M ${String(x0)} ${String(y0)} A ${String(r)} ${String(r)} 0 ${String(large)} 1 ${String(x1)} ${String(y1)}`;
}

/** The step an arrow key asks for, or null for a key we don't handle. */
function arrowStep(key: string): number | null {
  if (key === "ArrowUp" || key === "ArrowRight") return 1;
  if (key === "ArrowDown" || key === "ArrowLeft") return -1;
  return null;
}

export function Knob({ label, value, min, max, onChange, onGestureBegin, onGestureCommit }: Props) {
  const start = useRef<{ y: number; value: number } | null>(null);
  const gesture = useGestureBracket(onGestureBegin, onGestureCommit);

  const span = Math.max(1, max - min);
  const frac = (value - min) / span;
  const angle = SWEEP_START + frac * (SWEEP_END - SWEEP_START);

  // Only a value the core doesn't already hold is an edit: a drag or a
  // key that runs into a rail otherwise lands a phantom undo step and
  // lights the unexported marker.
  const emit = (next: number) => {
    if (next !== value) onChange(next);
  };

  return (
    <div className="field">
      <svg
        width={44}
        height={44}
        role="slider"
        aria-label={label}
        aria-valuemin={min}
        aria-valuemax={max}
        aria-valuenow={value}
        // The drag is vertical, so a slider's default horizontal
        // reading would describe a gesture this control hasn't got.
        aria-orientation="vertical"
        tabIndex={0}
        style={{ cursor: "ns-resize", touchAction: "none" }}
        onPointerDown={(e) => {
          (e.currentTarget as Partial<SVGSVGElement>).setPointerCapture?.(e.pointerId);
          start.current = { y: e.clientY, value };
          gesture.begin();
        }}
        onPointerMove={(e) => {
          if (!start.current) return;
          const dy = start.current.y - e.clientY;
          emit(Math.round(clamp(start.current.value + (dy / 180) * span, min, max)));
        }}
        onPointerUp={() => {
          start.current = null;
          gesture.commit();
        }}
        onPointerCancel={() => {
          start.current = null;
          gesture.commit();
        }}
        onLostPointerCapture={() => {
          start.current = null;
          gesture.commit();
        }}
        // The keyboard is a first-class path (Q5), so a key auto-repeat
        // run is bracketed like a drag (R24). See useGestureBracket.
        onKeyDown={(e) => {
          const step = arrowStep(e.key);
          if (step === null) return;
          gesture.begin();
          emit(clamp(value + step, min, max));
        }}
        onKeyUp={(e) => {
          // A key released under a pointer drag ends nothing; the
          // release the drag is waiting for closes the one bracket.
          if (arrowStep(e.key) !== null && !start.current) gesture.commit();
        }}
        onBlur={() => {
          if (!start.current) gesture.commit();
        }}
      >
        <circle cx={22} cy={22} r={15} fill="var(--fz-bg-panel)" stroke="var(--fz-border)" />
        <path
          d={arcPath(22, 22, 20, SWEEP_START, SWEEP_END)}
          fill="none"
          stroke="var(--fz-border-faint)"
          strokeWidth={2.5}
        />
        {frac > 0 && (
          <path
            d={arcPath(22, 22, 20, SWEEP_START, angle)}
            fill="none"
            stroke="var(--fz-accent-bright)"
            strokeWidth={2.5}
          />
        )}
        <line
          x1={22}
          y1={22}
          x2={22 + 12 * Math.cos(((angle - 90) * Math.PI) / 180)}
          y2={22 + 12 * Math.sin(((angle - 90) * Math.PI) / 180)}
          stroke="var(--fz-accent-bright)"
          strokeWidth={2}
        />
        <circle cx={22} cy={22} r={2} fill="var(--fz-accent-bright)" />
      </svg>
      <label>{label}</label>
      <span className="value">{value}</span>
    </div>
  );
}
