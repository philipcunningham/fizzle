// Drop routing: reads file names from a real DataTransfer (or a simulated
// one) and routes them by extension through the placement matrix. The
// mockup never reads file contents; names alone drive the canned flows.

import type { Dispatch } from "react";
import type { Action } from "./state/store";

interface NamedFiles {
  names: string[];
}

function namesFrom(input: DataTransfer | NamedFiles): string[] {
  if ("names" in input) return input.names;
  return Array.from(input.files).map((f) => f.name);
}

export function routeFiles(input: DataTransfer | NamedFiles, dispatch: Dispatch<Action>): void {
  const names = namesFrom(input);
  if (names.length === 0) return;
  // An SFZ instrument arrives as one .sfz plus its .wav samples; the
  // .sfz decides the route or every SFZ folder would import as WAVs.
  const sfz = names.find((n) => n.toLowerCase().endsWith(".sfz"));
  if (sfz) {
    dispatch({ type: "route-import", ext: "sfz", names: [sfz] });
    return;
  }
  const wavs = names.filter((n) => n.toLowerCase().endsWith(".wav"));
  if (wavs.length > 0) {
    dispatch({ type: "route-import", ext: "wav", names: wavs });
    return;
  }
  dispatch({ type: "route-import", ext: (names[0].split(".").pop() ?? "").toLowerCase(), names });
}
