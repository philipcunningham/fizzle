// Envelope editor: a graph with 8 draggable stage nodes and a numeric
// grid, always in sync. Sustain and end designations are visible on the
// graph and editable in the grid.

import { useRef } from "react";
import type { Envelope } from "../data/model";
import { clamp } from "../data/model";

interface Props {
  envelope: Envelope;
  label: string;
  onChange: (e: Envelope) => void;
  onGestureBegin?: () => void;
  onGestureCommit?: () => void;
  compact?: boolean;
}

// viewBox coordinates; the svg scales to fill its container's width.
const W = 1000;
const H = 170;
const PAD = 10;

interface DragStart {
  i: number;
  x: number;
  rate: number;
}

export function EnvelopeEditor({ envelope, label, onChange, onGestureBegin, onGestureCommit, compact }: Props) {
  const drag = useRef<DragStart | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);

  const active = envelope.stages.slice(0, envelope.endStage + 1);
  const totalRate = active.reduce((s, st) => s + (128 - st.rate), 0);

  // X positions: cumulative "time" per stage (slower rate = wider step).
  const xs: number[] = [PAD];
  let acc = 0;
  active.forEach((st) => {
    acc += 128 - st.rate;
    xs.push(PAD + (acc / totalRate) * (W - 2 * PAD));
  });
  const toY = (level: number) => H - PAD - (level / 127) * (H - 2 * PAD);

  const setStage = (i: number, rate: number, level: number) => {
    const stages = envelope.stages.map((st, si) => (si === i ? { rate: clamp(rate, 0, 127), level: clamp(level, 0, 127) } : st));
    onChange({ ...envelope, stages });
  };

  return (
    <div>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <span style={{ color: "var(--fz-accent-bright)", fontSize: 11 }}>{label}</span>
        <span style={{ color: "var(--fz-fg-faint)", fontSize: 10 }}>
          Sustain S{envelope.sustainStage + 1} · end S{envelope.endStage + 1}
        </span>
      </div>
      <div className="row" style={{ alignItems: "flex-start", gap: 16 }}>
        <svg
        ref={svgRef}
        viewBox={`0 0 ${W} ${H}`}
        role="group"
        aria-label={`${label} envelope graph`}
        style={{
          width: "100%",
          height: "auto",
          display: "block",
          background: "var(--fz-bg)",
          border: "1px solid var(--fz-border-faint)",
          borderRadius: 3,
          touchAction: "none",
        }}
        onPointerMove={(e) => {
          const d = drag.current;
          if (!d) return;
          const rect = svgRef.current!.getBoundingClientRect();
          // Pointer positions map into viewBox coordinates, since the svg
          // scales to its container. Vertical position maps straight to
          // level; horizontal movement adjusts the stage rate relative to
          // where the drag started, so the node never jumps.
          const px = (e.clientX - rect.left) * (W / rect.width);
          const py = (e.clientY - rect.top) * (H / rect.height);
          const level = clamp(((H - PAD - py) / (H - 2 * PAD)) * 127, 0, 127);
          const rate = clamp(d.rate - (px - d.x) * 0.6, 0, 127);
          setStage(d.i, rate, level);
        }}
        onPointerUp={() => {
          drag.current = null;
          onGestureCommit?.();
        }}
      >
        <polyline
          points={`${PAD},${toY(0)} ` + active.map((st, i) => `${xs[i + 1]},${toY(st.level)}`).join(" ")}
          fill="none"
          stroke="var(--fz-accent)"
          strokeWidth={1.5}
        />
        {active.map((st, i) => (
          <g key={i}>
            {i === envelope.sustainStage && (
              <>
                <line x1={xs[i + 1]} y1={PAD} x2={xs[i + 1]} y2={H - PAD} stroke="var(--fz-warning)" strokeDasharray="3 3" />
                <text x={xs[i + 1] + 4} y={PAD + 12} fontSize={11} fill="var(--fz-warning)">
                  S
                </text>
              </>
            )}
            {i === envelope.endStage && (
              <text x={xs[i + 1] - 12} y={toY(st.level) - 8} fontSize={11} fill="var(--fz-error)">
                E
              </text>
            )}
            <circle
              cx={xs[i + 1]}
              cy={toY(st.level)}
              r={5}
              fill={i === envelope.endStage ? "var(--fz-error)" : "var(--fz-bg-raised)"}
              stroke="var(--fz-accent-bright)"
              style={{ cursor: "grab" }}
              onPointerDown={(e) => {
                const svg = e.currentTarget.ownerSVGElement as SVGSVGElement;
                const rect = svg.getBoundingClientRect();
                drag.current = { i, x: (e.clientX - rect.left) * (W / rect.width), rate: st.rate };
                svg.setPointerCapture(e.pointerId);
                onGestureBegin?.();
              }}
            >
              <title>{`S${i + 1} rate ${st.rate} level ${st.level}`}</title>
            </circle>
          </g>
        ))}
      </svg>
      {!compact && (
        <table className="term envgrid">
          <thead>
            <tr>
              <th>Stage</th>
              {envelope.stages.map((_, i) => (
                <th key={i}>S{i + 1}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>Rate</td>
              {envelope.stages.map((st, i) => (
                <td key={i}>
                  <input
                    aria-label={`stage ${i + 1} rate`} name={`${label.toLowerCase().replace(/\s+/g, "-")}-s${i + 1}-rate`}
                    value={st.rate}
                    onChange={(e) => setStage(i, Number(e.target.value) || 0, st.level)}
                  />
                </td>
              ))}
            </tr>
            <tr>
              <td>Level</td>
              {envelope.stages.map((st, i) => (
                <td key={i}>
                  <input
                    aria-label={`stage ${i + 1} level`} name={`${label.toLowerCase().replace(/\s+/g, "-")}-s${i + 1}-level`}
                    value={st.level}
                    onChange={(e) => setStage(i, st.rate, Number(e.target.value) || 0)}
                  />
                </td>
              ))}
            </tr>
            <tr>
              <td>Mark</td>
              {envelope.stages.map((_, i) => (
                <td key={i}>
                  <button
                    className="btn small ghost"
                    style={
                      i === envelope.sustainStage
                        ? { color: "var(--fz-warning)", borderColor: "var(--fz-warning)", background: "rgba(255, 176, 0, 0.12)" }
                        : undefined
                    }
                    onClick={() => onChange({ ...envelope, sustainStage: i })}
                    aria-label={`set sustain stage ${i + 1}`}
                  >
                    Sus
                  </button>
                  <button
                    className="btn small ghost"
                    style={
                      i === envelope.endStage
                        ? { color: "var(--fz-error)", borderColor: "var(--fz-error)", background: "rgba(255, 64, 64, 0.12)" }
                        : undefined
                    }
                    onClick={() => onChange({ ...envelope, endStage: i })}
                    aria-label={`set end stage ${i + 1}`}
                  >
                    End
                  </button>
                </td>
              ))}
            </tr>
          </tbody>
        </table>
      )}
      </div>
    </div>
  );
}
