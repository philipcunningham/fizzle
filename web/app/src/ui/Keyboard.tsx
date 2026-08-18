// The on-screen keyboard (R20): plays at pitch, velocity from click
// height, sustaining while held. Carried from the mockup's validated
// design: SVG keys, range highlight, root dot on white and black keys.
// Every key is a focusable button, so the keyboard plays from the
// keyboard (Q5): Enter or Space holds a note at a fixed velocity.
// Every colour comes from a token, because the piano is the one light
// surface in a dark app and its marks need values of their own.
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import { useState } from "react";
import { noteName } from "./notes";

export interface KeyboardProps {
  lowNote?: number;
  octaves?: number;
  highlight?: { lo: number; hi: number }[] | null;
  rootKey?: number | null;
  onNoteOn: (note: number, velocity: number) => void;
  onNoteOff: (note: number) => void;
}

const WHITE_SEMIS = [0, 2, 4, 5, 7, 9, 11];
const BLACK_SEMIS: Record<number, number> = { 0: 1, 1: 3, 3: 6, 4: 8, 5: 10 };
const WHITE_W = 22;
const WHITE_H = 84;
const BLACK_W = 13;
const BLACK_H = 52;

/** Where a key sits, so the focus ring can be drawn over it. */
interface KeyBox {
  x: number;
  width: number;
  height: number;
}

export function Keyboard({
  lowNote = 36,
  octaves = 5,
  highlight,
  rootKey,
  onNoteOn,
  onNoteOff,
}: KeyboardProps) {
  // Which key holds focus. The ring is drawn rather than outlined: an
  // outline on a full-height rect is clipped by the SVG to two
  // hairlines in the gaps between keys, and the palette's white ring
  // is 1.23 against a white key (Q5).
  const [focusNote, setFocusNote] = useState<number | null>(null);

  const whites: { note: number; x: number }[] = [];
  const blacks: { note: number; x: number }[] = [];
  for (let o = 0; o < octaves; o++) {
    WHITE_SEMIS.forEach((semi, wi) => {
      const note = lowNote + o * 12 + semi;
      if (note > 127) return;
      const x = (o * 7 + wi) * WHITE_W;
      whites.push({ note, x });
      const blackSemi = BLACK_SEMIS[wi];
      if (blackSemi !== undefined && lowNote + o * 12 + blackSemi <= 127) {
        blacks.push({ note: lowNote + o * 12 + blackSemi, x: x + WHITE_W - BLACK_W / 2 });
      }
    });
  }

  const boxes = new Map<number, KeyBox>();
  for (const { note, x } of whites) boxes.set(note, { x, width: WHITE_W - 1, height: WHITE_H });
  // Black keys overlay their neighbours, so they come second and win.
  for (const { note, x } of blacks) boxes.set(note, { x, width: BLACK_W, height: BLACK_H });

  const inRange = (n: number) => highlight?.some((r) => n >= r.lo && n <= r.hi) ?? false;

  const noteOn = (note: number, e: ReactPointerEvent<SVGRectElement>) => {
    (e.currentTarget as Partial<SVGRectElement>).setPointerCapture?.(e.pointerId);
    const rect = e.currentTarget.getBoundingClientRect();
    const frac = rect.height > 0 ? (e.clientY - rect.top) / rect.height : 1;
    onNoteOn(note, Math.max(1, Math.min(127, Math.round(frac * 127))));
  };

  const isPlayKey = (e: ReactKeyboardEvent<SVGRectElement>) => e.key === "Enter" || e.key === " ";

  const keyProps = (note: number) => ({
    role: "button",
    tabIndex: 0,
    "aria-label": `play ${noteName(note)}`,
    onPointerDown: (e: ReactPointerEvent<SVGRectElement>) => {
      noteOn(note, e);
    },
    onPointerUp: () => {
      onNoteOff(note);
    },
    onPointerCancel: () => {
      onNoteOff(note);
    },
    onKeyDown: (e: ReactKeyboardEvent<SVGRectElement>) => {
      if (isPlayKey(e) && !e.repeat) {
        e.preventDefault();
        onNoteOn(note, 100);
      }
    },
    onKeyUp: (e: ReactKeyboardEvent<SVGRectElement>) => {
      if (isPlayKey(e)) onNoteOff(note);
    },
    onFocus: () => {
      setFocusNote(note);
    },
    onBlur: () => {
      setFocusNote((n) => (n === note ? null : n));
      onNoteOff(note);
    },
  });

  const focusBox = focusNote === null ? null : (boxes.get(focusNote) ?? null);

  return (
    <svg
      className="pianokeys"
      width={whites.length * WHITE_W}
      height={WHITE_H}
      role="group"
      aria-label="on screen keyboard, velocity by click height, sustains while held"
      style={{ userSelect: "none", touchAction: "none" }}
    >
      {whites.map(({ note, x }) => (
        <g key={note}>
          <rect
            data-testid={`key-${note}`}
            x={x}
            y={0}
            width={WHITE_W - 1}
            height={WHITE_H}
            fill={inRange(note) ? "var(--fz-key-white-on)" : "var(--fz-key-white)"}
            stroke={inRange(note) ? "var(--fz-key-edge-on)" : "var(--fz-key-edge)"}
            style={{ cursor: "pointer" }}
            {...keyProps(note)}
          />
          {note === rootKey && (
            <circle
              cx={x + WHITE_W / 2}
              cy={WHITE_H - 8}
              r={3}
              // The bright amber is 1.50 against an unhighlighted white
              // key, so that one case takes the dark form.
              fill={inRange(note) ? "var(--fz-key-root)" : "var(--fz-key-root-light)"}
              style={{ pointerEvents: "none" }}
            />
          )}
          {note % 12 === 0 && (
            <text
              x={x + 3}
              y={WHITE_H - 3}
              fontSize={8}
              fill={inRange(note) ? "var(--fz-key-label-on)" : "var(--fz-key-label)"}
            >
              {noteName(note)}
            </text>
          )}
        </g>
      ))}
      {blacks.map(({ note, x }) => (
        <g key={note}>
          <rect
            data-testid={`key-${note}`}
            x={x}
            y={0}
            width={BLACK_W}
            height={BLACK_H}
            fill={inRange(note) ? "var(--fz-key-black-on)" : "var(--fz-key-black)"}
            stroke={inRange(note) ? "var(--fz-key-edge-on)" : "var(--fz-key-edge)"}
            style={{ cursor: "pointer" }}
            {...keyProps(note)}
          />
          {note === rootKey && (
            <circle
              cx={x + BLACK_W / 2}
              cy={BLACK_H - 8}
              r={3}
              fill="var(--fz-key-root)"
              style={{ pointerEvents: "none" }}
            />
          )}
        </g>
      ))}
      {focusBox && (
        // Two strokes, drawn last so they sit over the black keys and
        // inside the key's own bounds. No single colour clears 3:1 on
        // both a plain white key and a highlighted black one, so the
        // pair does it between them.
        <g data-testid="key-focus-ring" style={{ pointerEvents: "none" }}>
          <rect
            x={focusBox.x + 1}
            y={1}
            width={focusBox.width - 2}
            height={focusBox.height - 2}
            fill="none"
            stroke="var(--fz-key-focus-dark)"
            strokeWidth={2}
          />
          <rect
            x={focusBox.x + 3}
            y={3}
            width={focusBox.width - 6}
            height={focusBox.height - 6}
            fill="none"
            stroke="var(--fz-key-focus-light)"
            strokeWidth={2}
          />
        </g>
      )}
    </svg>
  );
}
