// Rotary knob: drag vertically to change value. A swept arc communicates
// position in range the way the range slider's filled track does.
// Gestures are bracketed so a whole drag lands as one undo entry.

import { useRef } from "react";
import { clamp } from "../data/model";

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
  return `M ${x0} ${y0} A ${r} ${r} 0 ${large} 1 ${x1} ${y1}`;
}

export function Knob({ label, value, min, max, onChange, onGestureBegin, onGestureCommit }: Props) {
  const start = useRef<{ y: number; value: number } | null>(null);

  const frac = (value - min) / (max - min);
  const angle = SWEEP_START + frac * (SWEEP_END - SWEEP_START);

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
        tabIndex={0}
        style={{ cursor: "ns-resize", touchAction: "none" }}
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId);
          start.current = { y: e.clientY, value };
          onGestureBegin?.();
        }}
        onPointerMove={(e) => {
          if (!start.current) return;
          const dy = start.current.y - e.clientY;
          onChange(clamp(start.current.value + (dy / 180) * (max - min), min, max));
        }}
        onPointerUp={() => {
          start.current = null;
          onGestureCommit?.();
        }}
        onKeyDown={(e) => {
          if (e.key === "ArrowUp" || e.key === "ArrowRight") onChange(clamp(value + 1, min, max));
          if (e.key === "ArrowDown" || e.key === "ArrowLeft") onChange(clamp(value - 1, min, max));
        }}
      >
        <circle cx={22} cy={22} r={15} fill="var(--fz-bg-panel)" stroke="var(--fz-border)" />
        <path d={arcPath(22, 22, 20, SWEEP_START, SWEEP_END)} fill="none" stroke="var(--fz-border-faint)" strokeWidth={2.5} />
        {frac > 0 && (
          <path d={arcPath(22, 22, 20, SWEEP_START, angle)} fill="none" stroke="var(--fz-accent-bright)" strokeWidth={2.5} />
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
