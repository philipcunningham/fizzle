import { useId } from "react";
import { clamp } from "../data/model";

interface Props {
  label: string;
  value: number;
  min: number;
  max: number;
  // Base increment; Shift multiplies it by ten for coarse moves.
  step?: number;
  format?: (v: number) => string;
  onChange: (v: number) => void;
}

export function Stepper({ label, value, min, max, step = 1, format, onChange }: Props) {
  const id = useId();
  const move = (dir: 1 | -1, shift: boolean) => onChange(clamp(value + dir * step * (shift ? 10 : 1), min, max));
  return (
    <div className="field">
      <div className="row" style={{ gap: 4 }}>
        <button className="btn small" aria-label={`decrease ${label}`} onClick={(e) => move(-1, e.shiftKey)}>
          -
        </button>
        <input
          id={id}
          name={id}
          value={format ? format(value) : String(value)}
          onChange={(e) => {
            const n = Number(e.target.value.replace(/[^\d-]/g, ""));
            if (!Number.isNaN(n)) onChange(clamp(n, min, max));
          }}
          style={{
            width: 64,
            textAlign: "center",
            background: "var(--fz-bg)",
            border: "1px solid var(--fz-border-faint)",
            color: "var(--fz-fg)",
            borderRadius: "var(--fz-radius)",
            padding: "1px 2px",
          }}
        />
        <button className="btn small" aria-label={`increase ${label}`} onClick={(e) => move(1, e.shiftKey)}>
          +
        </button>
      </div>
      <label htmlFor={id}>{label}</label>
    </div>
  );
}
