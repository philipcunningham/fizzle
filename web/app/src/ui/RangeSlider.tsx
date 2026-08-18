// The dual-handle range slider: pointer drags move the nearer handle,
// arrow keys nudge the focused one.
import { type KeyboardEvent, useRef } from "react";
import { clamp } from "./format";
import { useGestureBracket } from "./gesture";

interface Props {
  label: string;
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

/** The step an arrow key asks for, or null for a key we don't handle. */
function arrowStep(key: string): number | null {
  if (key === "ArrowRight" || key === "ArrowUp") return 1;
  if (key === "ArrowLeft" || key === "ArrowDown") return -1;
  return null;
}

export function RangeSlider({
  label,
  lo,
  hi,
  min = 0,
  max = 127,
  width = 220,
  format,
  onChange,
  onGestureBegin,
  onGestureCommit,
}: Props) {
  const drag = useRef<"lo" | "hi" | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const gesture = useGestureBracket(onGestureBegin, onGestureCommit);
  const h = 22;

  const toX = (v: number) => ((v - min) / Math.max(1, max - min)) * (width - 12) + 6;
  const toV = (x: number) =>
    Math.round(clamp(min + ((x - 6) / (width - 12)) * (max - min), min, max));

  // Only a handle whose display differs from its number needs spelling
  // out: velocity 64 already reads 64, while key 37 shows C#2, and the
  // value a reader hears has to be the value on screen (Q5).
  const speak = (v: number) => (format ? format(v) : undefined);

  const nudge = (which: "lo" | "hi", delta: number) => {
    if (which === "lo") onChange(clamp(Math.min(lo + delta, hi), min, max), hi);
    else onChange(lo, clamp(Math.max(hi + delta, lo), min, max));
  };

  // The keyboard is a first-class path (Q5), so a key auto-repeat run
  // is bracketed like a drag (R24). See useGestureBracket for why.
  const keyDown = (which: "lo" | "hi") => (e: KeyboardEvent<SVGCircleElement>) => {
    const step = arrowStep(e.key);
    if (step === null) return;
    gesture.begin();
    nudge(which, step);
  };
  const keyUp = (e: KeyboardEvent<SVGCircleElement>) => {
    // A key released under a pointer drag ends nothing; the release the
    // drag is waiting for closes the one bracket.
    if (arrowStep(e.key) !== null && !drag.current) gesture.commit();
  };
  const blur = () => {
    if (!drag.current) gesture.commit();
  };

  return (
    <svg
      ref={svgRef}
      width={width}
      height={h}
      // A slider is a leaf role, so the rail can't be one and hold two
      // handles that are. The rail names the pair; each handle carries
      // its own value (Q5).
      role="group"
      aria-label={label}
      style={{ touchAction: "none" }}
      onPointerDown={(e) => {
        const rect = svgRef.current?.getBoundingClientRect();
        if (!rect) return;
        const x = e.clientX - rect.left;
        drag.current = Math.abs(x - toX(lo)) <= Math.abs(x - toX(hi)) ? "lo" : "hi";
        (e.currentTarget as Partial<SVGSVGElement>).setPointerCapture?.(e.pointerId);
        gesture.begin();
      }}
      onPointerMove={(e) => {
        if (!drag.current) return;
        const rect = svgRef.current?.getBoundingClientRect();
        if (!rect) return;
        const v = toV(e.clientX - rect.left);
        if (drag.current === "lo") onChange(Math.min(v, hi), hi);
        else onChange(lo, Math.max(v, lo));
      }}
      onPointerUp={() => {
        drag.current = null;
        gesture.commit();
      }}
      // A pointer that leaves the window, or a control that unmounts
      // mid-drag, otherwise leaves the bracket open and swallows every
      // later edit's history entry.
      onPointerCancel={() => {
        drag.current = null;
        gesture.commit();
      }}
      onLostPointerCapture={() => {
        drag.current = null;
        gesture.commit();
      }}
    >
      <rect
        x={0}
        y={h / 2 - 3}
        width={width}
        height={6}
        fill="var(--fz-bg-panel)"
        stroke="var(--fz-border-faint)"
      />
      <rect
        x={toX(lo)}
        y={h / 2 - 3}
        width={Math.max(1, toX(hi) - toX(lo))}
        height={6}
        fill="var(--fz-accent)"
      />
      <circle
        cx={toX(lo)}
        cy={h / 2}
        r={6}
        fill="var(--fz-bg)"
        stroke="var(--fz-accent-bright)"
        style={{ cursor: "ew-resize" }}
        role="slider"
        aria-label={`${label} low`}
        aria-valuenow={lo}
        aria-valuemin={min}
        aria-valuemax={max}
        aria-valuetext={speak(lo)}
        tabIndex={0}
        onKeyDown={keyDown("lo")}
        onKeyUp={keyUp}
        onBlur={blur}
      />
      <circle
        cx={toX(hi)}
        cy={h / 2}
        r={6}
        fill="var(--fz-bg)"
        stroke="var(--fz-accent-bright)"
        style={{ cursor: "ew-resize" }}
        role="slider"
        aria-label={`${label} high`}
        aria-valuenow={hi}
        aria-valuemin={min}
        aria-valuemax={max}
        aria-valuetext={speak(hi)}
        tabIndex={0}
        onKeyDown={keyDown("hi")}
        onKeyUp={keyUp}
        onBlur={blur}
      />
    </svg>
  );
}
