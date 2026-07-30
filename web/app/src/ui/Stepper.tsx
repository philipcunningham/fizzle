// The mockup's stepper: minus, a typed value, plus; shift steps by 10.
// The text is local while the user types and commits on blur or Enter,
// so the core's confirmed value is what the field shows afterwards.
// Input the field cannot parse (empty, letters, a note name in a plain
// numeric field) reverts rather than committing a guess.
import { useId, useState } from "react";
import { clamp } from "./format";

interface Props {
  label: string;
  value: number;
  min: number;
  max: number;
  step?: number;
  /** Renders the value for display (e.g. a note name). */
  format?: (v: number) => string;
  /** Reads the display form back; returns null when it is not valid. */
  parse?: (text: string) => number | null;
  onChange: (v: number) => void;
}

/** The default reading: a plain integer, nothing else. */
function parseInteger(text: string): number | null {
  const trimmed = text.trim();
  if (!/^-?\d+$/.test(trimmed)) return null;
  const n = Number(trimmed);
  return Number.isFinite(n) ? n : null;
}

export function Stepper({ label, value, min, max, step = 1, format, parse, onChange }: Props) {
  const id = useId();
  // null means "show the core's value"; a string means "being edited".
  // Nothing is mirrored in state, so a clamp (or an undo) that lands on
  // the value already shown still resets the field.
  const [draft, setDraft] = useState<string | null>(null);

  const move = (dir: 1 | -1, shift: boolean) => {
    onChange(clamp(value + dir * step * (shift ? 10 : 1), min, max));
  };

  const commit = () => {
    if (draft === null) return;
    const parsed = (parse ?? parseInteger)(draft);
    setDraft(null);
    if (parsed === null) return; // unreadable: revert to the core's value
    onChange(clamp(parsed, min, max));
  };

  return (
    <div className="field">
      <div className="row" style={{ gap: 4 }}>
        <button
          className="btn small"
          aria-label={`decrease ${label}`}
          onClick={(e) => {
            move(-1, e.shiftKey);
          }}
        >
          -
        </button>
        <input
          id={id}
          name={id}
          aria-label={label}
          className="stepperinput"
          value={draft ?? (format ? format(value) : String(value))}
          onChange={(e) => {
            setDraft(e.target.value);
          }}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") (e.target as HTMLInputElement).blur();
          }}
        />
        <button
          className="btn small"
          aria-label={`increase ${label}`}
          onClick={(e) => {
            move(1, e.shiftKey);
          }}
        >
          +
        </button>
      </div>
      <label htmlFor={id}>{label}</label>
    </div>
  );
}
