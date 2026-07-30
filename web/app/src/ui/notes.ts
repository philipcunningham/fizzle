const NOTE_NAMES = ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"] as const;

/** MIDI note to name, matching the CLI's convention (60 is C4). */
export function noteName(midi: number): string {
  const clamped = Math.min(127, Math.max(0, Math.round(midi)));
  return `${NOTE_NAMES[clamped % 12] ?? "C"}${Math.floor(clamped / 12) - 1}`;
}

/**
 * Parses what a note field shows back into a MIDI number: a name
 * ("C#4", "db4", case insensitive), a bare number ("61"), or the
 * "C#4 (61)" form the steppers display. Returns null when the text is
 * not a note, so callers can revert instead of committing a guess.
 */
export function parseNote(text: string): number | null {
  const trimmed = text.trim();
  if (trimmed === "") return null;

  // The display form "C#4 (61)" and a bare number both end in digits
  // that are already the MIDI value.
  const parens = /\((\d{1,3})\)\s*$/.exec(trimmed);
  if (parens?.[1]) return inRange(Number(parens[1]));
  if (/^-?\d+$/.test(trimmed)) return inRange(Number(trimmed));

  const match = /^([A-Ga-g])([#b]?)(-?\d+)$/.exec(trimmed);
  if (!match) return null;
  const [, letter = "", accidental = "", octave = "0"] = match;
  const base = NOTE_NAMES.indexOf(letter.toUpperCase() as (typeof NOTE_NAMES)[number]);
  if (base < 0) return null;
  const semitone = base + (accidental === "#" ? 1 : accidental.toLowerCase() === "b" ? -1 : 0);
  return inRange((Number(octave) + 1) * 12 + semitone);
}

function inRange(n: number): number | null {
  return Number.isFinite(n) && n >= 0 && n <= 127 ? n : null;
}
