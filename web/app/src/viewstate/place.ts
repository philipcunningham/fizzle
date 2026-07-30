// The placement matrix's first half: classify whatever arrived (drop
// or picker, one file or a folder) into the actions R7 defines. The
// second half, context routing, lives in the shell, which knows
// whether a disk or instrument is open.

export interface NamedBytes {
  /** Slash separated path relative to the drop or folder root. */
  name: string;
  bytes: Uint8Array;
}

export type Placement =
  | { kind: "image"; file: NamedBytes }
  | { kind: "imagePair"; a: NamedBytes; b: NamedBytes }
  | { kind: "fzf"; file: NamedBytes }
  | { kind: "fzb"; file: NamedBytes }
  | { kind: "fzv"; file: NamedBytes }
  | { kind: "wavs"; files: NamedBytes[] }
  | { kind: "sfz"; files: NamedBytes[]; sfzPath: string }
  | { kind: "unsupported"; names: string[] };

function ext(name: string): string {
  const dot = name.lastIndexOf(".");
  return dot < 0 ? "" : name.slice(dot + 1).toLowerCase();
}

/**
 * sfzCandidates lists the .sfz files a selection holds, sorted by
 * path, so the classifier and the SFZ dialog agree on what counts as
 * one and offer them in a stable order.
 */
export function sfzCandidates(files: NamedBytes[]): string[] {
  return files
    .filter((f) => ext(f.name) === "sfz")
    .map((f) => f.name)
    .sort();
}

/**
 * classifyInput turns a selection into placements. An .sfz anywhere in
 * the selection makes the whole set one SFZ instrument (its WAVs are
 * the referenced samples). Two images are a split pair candidate.
 * WAVs group into one batch so a folder gets one rate answer (R8).
 *
 * An sfz placement carries an empty sfzPath when the selection holds
 * more than one .sfz: the core won't guess which is the instrument,
 * and the dialog asks before the conversion runs (R6).
 */
export function classifyInput(files: NamedBytes[]): Placement[] {
  const sfzs = sfzCandidates(files);
  if (sfzs.length > 0) {
    return [{ kind: "sfz", files, sfzPath: sfzs.length === 1 ? (sfzs[0] ?? "") : "" }];
  }

  const placements: Placement[] = [];
  const images = files.filter((f) => ext(f.name) === "img");
  const first = images[0];
  const second = images[1];
  if (first && second && images.length === 2) {
    placements.push({ kind: "imagePair", a: first, b: second });
  } else {
    for (const image of images) placements.push({ kind: "image", file: image });
  }

  for (const file of files) {
    const e = ext(file.name);
    if (e === "fzf" || e === "fzb" || e === "fzv") {
      placements.push({ kind: e, file });
    }
  }

  const wavs = files.filter((f) => ext(f.name) === "wav");
  if (wavs.length > 0) {
    placements.push({ kind: "wavs", files: [...wavs].sort((a, b) => (a.name < b.name ? -1 : 1)) });
  }

  const known = new Set(["img", "fzf", "fzb", "fzv", "wav"]);
  const unsupported = files.filter((f) => !known.has(ext(f.name))).map((f) => f.name);
  if (unsupported.length > 0) {
    placements.push({ kind: "unsupported", names: unsupported });
  }
  return placements;
}

/** The boundary's folder shape: root-relative slash paths to bytes. */
export function toFileMap(files: NamedBytes[]): Record<string, Uint8Array> {
  const out: Record<string, Uint8Array> = {};
  for (const f of files) out[f.name] = f.bytes;
  return out;
}
