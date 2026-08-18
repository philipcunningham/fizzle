// The envelope editor over the core's envelope shape (R16): a draggable
// stage graph and a numeric grid, always in sync, with sustain and end
// designations visible and editable on both. Rates and stops speak the
// hardware display scale (0 to 99); a higher rate is a faster stage, so
// the graph spaces stages by (100 - rate).
import { useRef } from "react";
import type { EnvelopeSnapshot } from "../boundary/contract";
import { NumberCell } from "./NumberCell";
import { clamp } from "./format";
import { useGestureBracket } from "./gesture";

interface Props {
  envelope: EnvelopeSnapshot;
  label: string;
  onChange: (sustain: number, end: number, rates: number[], stops: number[]) => void;
  onGestureBegin?: () => void;
  onGestureCommit?: () => void;
  compact?: boolean;
}

const W = 1000;
const H = 170;
const PAD = 10;
const MAX = 99;

interface DragStart {
  i: number;
  x: number;
  rate: number;
}

export function EnvelopeEditor({
  envelope,
  label,
  onChange,
  onGestureBegin,
  onGestureCommit,
  compact,
}: Props) {
  const drag = useRef<DragStart | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const gesture = useGestureBracket(onGestureBegin, onGestureCommit);

  const { sustain, end, rates, stops } = envelope;
  const active = rates.slice(0, end + 1);
  const totalRate = active.reduce((s, r) => s + (MAX + 1 - r), 0);

  const xs: number[] = [PAD];
  let acc = 0;
  active.forEach((r) => {
    acc += MAX + 1 - r;
    xs.push(PAD + (acc / Math.max(1, totalRate)) * (W - 2 * PAD));
  });
  const toY = (level: number) => H - PAD - (level / MAX) * (H - 2 * PAD);

  // Only a stage the core doesn't already hold is an edit: a drag into
  // a rail otherwise lands a phantom undo step and lights the marker.
  const setStage = (i: number, rate: number, stop: number) => {
    const nextRate = Math.round(clamp(rate, 0, MAX));
    const nextStop = Math.round(clamp(stop, 0, MAX));
    if (nextRate === rates[i] && nextStop === stops[i]) return;
    const nextRates = rates.map((r, ri) => (ri === i ? nextRate : r));
    const nextStops = stops.map((s, si) => (si === i ? nextStop : s));
    onChange(sustain, end, nextRates, nextStops);
  };

  const slug = label.toLowerCase().replace(/\s+/g, "-");

  return (
    <div>
      <div className="row" style={{ justifyContent: "space-between" }}>
        <span className="envtitle">{label}</span>
        <span className="envmeta">
          Sustain S{sustain + 1} · end S{end + 1}
        </span>
      </div>
      <div className="row" style={{ alignItems: "flex-start", gap: 16 }}>
        <svg
          ref={svgRef}
          viewBox={`0 0 ${String(W)} ${String(H)}`}
          role="group"
          aria-label={`${label} envelope graph`}
          className="envgraph"
          style={{ touchAction: "none" }}
          onPointerMove={(e) => {
            const d = drag.current;
            const svg = svgRef.current;
            if (!d || !svg) return;
            const rect = svg.getBoundingClientRect();
            const px = (e.clientX - rect.left) * (W / rect.width);
            const py = (e.clientY - rect.top) * (H / rect.height);
            const stop = clamp(((H - PAD - py) / (H - 2 * PAD)) * MAX, 0, MAX);
            const rate = clamp(d.rate - (px - d.x) * 0.6, 0, MAX);
            setStage(d.i, rate, stop);
          }}
          // A pointer that leaves the window, a release under capture,
          // and an editor that unmounts mid-drag all end the gesture.
          // The bracket closes once whichever of them arrives.
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
        >
          <polyline
            points={
              `${String(PAD)},${String(toY(0))} ` +
              active
                .map((_, i) => `${String(xs[i + 1] ?? PAD)},${String(toY(stops[i] ?? 0))}`)
                .join(" ")
            }
            fill="none"
            stroke="var(--fz-accent)"
            strokeWidth={1.5}
          />
          {active.map((_, i) => (
            <g key={i}>
              {i === sustain && (
                <>
                  <line
                    x1={xs[i + 1]}
                    y1={PAD}
                    x2={xs[i + 1]}
                    y2={H - PAD}
                    stroke="var(--fz-warning)"
                    strokeDasharray="3 3"
                  />
                  <text
                    x={(xs[i + 1] ?? 0) + 4}
                    y={PAD + 12}
                    fontSize={11}
                    fill="var(--fz-warning)"
                  >
                    S
                  </text>
                </>
              )}
              {i === end && (
                <text
                  x={(xs[i + 1] ?? 0) - 12}
                  y={toY(stops[i] ?? 0) - 8}
                  fontSize={11}
                  fill="var(--fz-error)"
                >
                  E
                </text>
              )}
              <circle
                cx={xs[i + 1]}
                cy={toY(stops[i] ?? 0)}
                r={5}
                fill={i === end ? "var(--fz-error)" : "var(--fz-bg-raised)"}
                stroke="var(--fz-accent-bright)"
                style={{ cursor: "grab" }}
                data-testid={`${slug}-node-${String(i + 1)}`}
                onPointerDown={(e) => {
                  const svg = e.currentTarget.ownerSVGElement;
                  if (!svg) return;
                  const rect = svg.getBoundingClientRect();
                  drag.current = {
                    i,
                    x: (e.clientX - rect.left) * (W / Math.max(1, rect.width)),
                    rate: rates[i] ?? 0,
                  };
                  (svg as Partial<SVGSVGElement>).setPointerCapture?.(e.pointerId);
                  gesture.begin();
                }}
              >
                <title>{`S${String(i + 1)} rate ${String(rates[i] ?? 0)} level ${String(stops[i] ?? 0)}`}</title>
              </circle>
            </g>
          ))}
        </svg>
        {!compact && (
          <table className="term envgrid">
            <thead>
              <tr>
                <th>Stage</th>
                {rates.map((_, i) => (
                  <th key={i}>S{i + 1}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>Rate</td>
                {rates.map((r, i) => (
                  <td key={i}>
                    <NumberCell
                      label={`${label} stage ${String(i + 1)} rate`}
                      name={`${slug}-s${String(i + 1)}-rate`}
                      value={r}
                      onCommit={(n) => {
                        setStage(i, n, stops[i] ?? 0);
                      }}
                    />
                  </td>
                ))}
              </tr>
              <tr>
                <td>Level</td>
                {stops.map((s, i) => (
                  <td key={i}>
                    <NumberCell
                      label={`${label} stage ${String(i + 1)} level`}
                      name={`${slug}-s${String(i + 1)}-level`}
                      value={s}
                      onCommit={(n) => {
                        setStage(i, rates[i] ?? 0, n);
                      }}
                    />
                  </td>
                ))}
              </tr>
              <tr>
                <td>Mark</td>
                {rates.map((_, i) => (
                  <td key={i}>
                    <button
                      className={`btn small ghost${i === sustain ? " marked-sus" : ""}`}
                      onClick={() => {
                        onChange(i, end, [...rates], [...stops]);
                      }}
                      aria-label={`${label} set sustain stage ${String(i + 1)}`}
                    >
                      Sus
                    </button>
                    <button
                      className={`btn small ghost${i === end ? " marked-end" : ""}`}
                      onClick={() => {
                        onChange(sustain, i, [...rates], [...stops]);
                      }}
                      aria-label={`${label} set end stage ${String(i + 1)}`}
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
