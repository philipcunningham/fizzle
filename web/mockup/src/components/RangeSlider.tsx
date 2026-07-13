// Draggable range picker for key and velocity ranges: a 0..127 strip
// with two handles. Numeric entry always sits beside it in the caller.

import { useRef } from "react";
import { clamp } from "../data/model";

interface Props {
  lo: number;
  hi: number;
  min?: number;
  max?: number;
  width?: number;
  format?: (v: number) => string;
  onChange: (lo: number, hi: number) => void;
  onGestureBegin?: () => void;
  onGestureCommit?: () => void;
}

export function RangeSlider({ lo, hi, min = 0, max = 127, width = 220, format, onChange, onGestureBegin, onGestureCommit }: Props) {
  const drag = useRef<"lo" | "hi" | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const h = 22;

  const toX = (v: number) => ((v - min) / (max - min)) * (width - 12) + 6;
  const toV = (x: number) => clamp(min + ((x - 6) / (width - 12)) * (max - min), min, max);

  const fmt = format ?? String;

  return (
    <svg
      ref={svgRef}
      width={width}
      height={h}
      role="slider"
      aria-label="range slider"
      aria-valuetext={`${fmt(lo)} to ${fmt(hi)}`}
      style={{ touchAction: "none" }}
      onPointerDown={(e) => {
        const rect = svgRef.current!.getBoundingClientRect();
        const x = e.clientX - rect.left;
        drag.current = Math.abs(x - toX(lo)) <= Math.abs(x - toX(hi)) ? "lo" : "hi";
        (e.currentTarget as SVGSVGElement).setPointerCapture(e.pointerId);
        onGestureBegin?.();
      }}
      onPointerMove={(e) => {
        if (!drag.current) return;
        const rect = svgRef.current!.getBoundingClientRect();
        const v = toV(e.clientX - rect.left);
        if (drag.current === "lo") onChange(Math.min(v, hi), hi);
        else onChange(lo, Math.max(v, lo));
      }}
      onPointerUp={() => {
        drag.current = null;
        onGestureCommit?.();
      }}
    >
      <rect x={0} y={h / 2 - 3} width={width} height={6} fill="var(--fz-bg-panel)" stroke="var(--fz-border-faint)" />
      <rect x={toX(lo)} y={h / 2 - 3} width={Math.max(1, toX(hi) - toX(lo))} height={6} fill="var(--fz-accent)" />
      <circle cx={toX(lo)} cy={h / 2} r={6} fill="var(--fz-bg)" stroke="var(--fz-accent-bright)" style={{ cursor: "ew-resize" }} />
      <circle cx={toX(hi)} cy={h / 2} r={6} fill="var(--fz-bg)" stroke="var(--fz-accent-bright)" style={{ cursor: "ew-resize" }} />
    </svg>
  );
}
