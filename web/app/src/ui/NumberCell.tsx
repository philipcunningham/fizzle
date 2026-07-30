// A numeric table cell. While the user types, the text is local; on
// blur or Enter it commits and the field falls back to the core's
// confirmed value. Input the field cannot read (empty, letters)
// reverts. Nothing is mirrored in state, so a clamp that lands on the
// value already shown still resets the text.
import { useState } from "react";

interface Props {
  label: string;
  name: string;
  value: number;
  /** Commit; the caller clamps and the core confirms. */
  onCommit: (v: number) => void;
}

export function NumberCell({ label, name, value, onCommit }: Props) {
  // null means "show the core's value"; a string means "being edited".
  const [draft, setDraft] = useState<string | null>(null);

  const commit = () => {
    const text = (draft ?? "").trim();
    setDraft(null);
    if (draft === null) return;
    if (!/^-?\d+$/.test(text)) return; // unreadable: revert
    onCommit(Number(text));
  };

  return (
    <input
      aria-label={label}
      name={name}
      value={draft ?? String(value)}
      onChange={(e) => {
        setDraft(e.target.value);
      }}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") (e.target as HTMLInputElement).blur();
      }}
    />
  );
}
