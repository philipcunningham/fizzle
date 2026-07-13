// On-screen keyboard: plays at pitch, velocity from click height,
// sustaining while held. Highlights the selected voice's key range.

import type { PointerEvent as ReactPointerEvent } from "react";
import { noteName } from "../data/model";

interface Props {
  lowNote?: number; // MIDI
  octaves?: number;
  // One or several key ranges to highlight (a voice, an Area, or a
  // whole bank's Areas).
  highlight?: { lo: number; hi: number }[] | null;
  rootKey?: number | null;
  onNoteOn: (note: number, velocity: number) => void;
  onNoteOff: (note: number) => void;
}

const WHITE_SEMIS = [0, 2, 4, 5, 7, 9, 11];
const BLACK_SEMIS: Record<number, number> = { 0: 1, 1: 3, 3: 6, 4: 8, 5: 10 };

export function Keyboard({ lowNote = 36, octaves = 5, highlight, rootKey, onNoteOn, onNoteOff }: Props) {
  const whiteW = 22;
  const whiteH = 84;
  const blackW = 13;
  const blackH = 52;
  const whites: { note: number; x: number }[] = [];
  const blacks: { note: number; x: number }[] = [];

  for (let o = 0; o < octaves; o++) {
    WHITE_SEMIS.forEach((semi, wi) => {
      const note = lowNote + o * 12 + semi;
      if (note > 127) return;
      const x = (o * 7 + wi) * whiteW;
      whites.push({ note, x });
      const blackSemi = BLACK_SEMIS[wi];
      if (blackSemi !== undefined && lowNote + o * 12 + blackSemi <= 127) {
        blacks.push({ note: lowNote + o * 12 + blackSemi, x: x + whiteW - blackW / 2 });
      }
    });
  }

  const inRange = (n: number) => highlight?.some((r) => n >= r.lo && n <= r.hi) ?? false;

  const noteOn = (note: number, e: ReactPointerEvent<SVGRectElement>) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    const rect = e.currentTarget.getBoundingClientRect();
    const frac = (e.clientY - rect.top) / rect.height;
    onNoteOn(note, Math.max(1, Math.min(127, Math.round(frac * 127))));
  };

  return (
    <svg
      width={whites.length * whiteW}
      height={whiteH}
      role="group"
      aria-label="on screen keyboard, velocity by click height, sustains while held"
      style={{ userSelect: "none", touchAction: "none" }}
    >
      {whites.map(({ note, x }) => (
        <g key={note}>
          <rect
            x={x}
            y={0}
            width={whiteW - 1}
            height={whiteH}
            fill={inRange(note) ? "#0e3535" : "#e8e8e8"}
            stroke="var(--fz-border-faint)"
            style={{ cursor: "pointer" }}
            onPointerDown={(e) => noteOn(note, e)}
            onPointerUp={() => onNoteOff(note)}
            onPointerCancel={() => onNoteOff(note)}
          />
          {note === rootKey && <circle cx={x + whiteW / 2} cy={whiteH - 8} r={3} fill="var(--fz-warning)" />}
          {note % 12 === 0 && (
            <text x={x + 3} y={whiteH - 3} fontSize={8} fill="var(--fz-fg-faint)">
              {noteName(note)}
            </text>
          )}
        </g>
      ))}
      {blacks.map(({ note, x }) => (
        <g key={note}>
          <rect
            x={x}
            y={0}
            width={blackW}
            height={blackH}
            fill={inRange(note) ? "#0a5c5c" : "#101010"}
            stroke="var(--fz-border-faint)"
            style={{ cursor: "pointer" }}
            onPointerDown={(e) => noteOn(note, e)}
            onPointerUp={() => onNoteOff(note)}
            onPointerCancel={() => onNoteOff(note)}
          />
          {note === rootKey && (
            <circle cx={x + blackW / 2} cy={blackH - 8} r={3} fill="var(--fz-warning)" style={{ pointerEvents: "none" }} />
          )}
        </g>
      ))}
    </svg>
  );
}
