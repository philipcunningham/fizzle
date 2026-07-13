// The seven journeys as guided click-throughs. A step is
// done when its `match` sees the action that proves it, or when the
// user ticks it by hand (steps with no matcher, like hardware checks).

import type { Action, State } from "../state/store";

export interface Step {
  text: string;
  match?: (action: Action, state: State) => boolean;
}

export interface Journey {
  id: string;
  title: string;
  intro: string;
  steps: Step[];
}

export const JOURNEYS: Journey[] = [
  {
    id: "J1",
    title: "J1. WAV folder to playable disk",
    intro: "From a folder of drum hits to an exported image, no disk open at the start.",
    steps: [
      { text: "Close any open disk (top right), then use 'Import WAV folder' on the start screen or drag .wav files in.", match: (_a, s) => s.dialog?.kind === "newDisk" },
      { text: "Name the new disk when asked; the app creates it rather than failing.", match: (a) => a.type === "new-disk" },
      { text: "Answer sample rate and stereo handling once for the whole batch.", match: (a) => a.type === "import-wavs" },
      { text: "Rename the instrument or a voice (double click the name in the voice table).", match: (a) => a.type === "rename-voice" || a.type === "rename-disk" },
      { text: "Drag a key range on the Banks and Areas tab.", match: (a) => a.type === "edit-area" },
      { text: "Tighten a loop point: on the Voices tab, drag an edge of the amber loop region, or click the waveform to move the nearest edge.", match: (a) => a.type === "edit-voice" },
      { text: "Shape the DCA envelope by dragging its stage nodes.", match: (a) => a.type === "edit-voice" },
      { text: "Audition from the on screen keyboard.", match: (a) => a.type === "audition" },
      { text: "Watch the capacity readout, then export the disk image.", match: (a) => a.type === "export" },
      { text: "On hardware: load the image on the FZ through a Gotek and play it. Tick when done." },
    ],
  },
  {
    id: "J2",
    title: "J2. DAW instrument to the FZ",
    intro: "An SFZ export lands mapped and named; oversized material offers fit or split.",
    steps: [
      { text: "Use the import menu's SFZ entry, or drag an .sfz folder in.", match: (_a, s) => s.dialog?.kind === "sfzImport" },
      { text: "The instrument is oversized: choose fit to disk or the two disk split.", match: (a) => a.type === "import-sfz" },
      { text: "Tweak a velocity layer or tuning on the mapped instrument.", match: (a) => a.type === "edit-area" || a.type === "edit-voice" },
      { text: "Audition, then export.", match: (a) => a.type === "export" },
    ],
  },
  {
    id: "J3",
    title: "J3. Rework an old disk",
    intro: "Open a ripped .img, fix a loop, rebalance two Areas, save.",
    steps: [
      { text: "Open the corpus image: Browse in the drop zone, or drag an .img in.", match: (a) => a.type === "open-seed-disk" },
      { text: "Open the instrument (the full dump) from the file listing.", match: (a) => a.type === "open-instrument" },
      { text: "Fix the loop that always clicked: on the Voices tab, drag an edge of the amber loop region on the waveform, or click the waveform to move the nearest loop edge.", match: (a) => a.type === "edit-voice" },
      { text: "Rebalance two Areas: adjust volume on the Banks and Areas tab.", match: (a) => a.type === "edit-area" },
      { text: "Export, over the original or as a new image.", match: (a) => a.type === "export" },
    ],
  },
  {
    id: "J4",
    title: "J4. Bring FZ files together",
    intro: "Any .fzf, .fzb, or .fzv does something sensible in every context.",
    steps: [
      { text: "With a disk open, import a .fzv (import menu): it joins the voice list.", match: (a) => a.type === "import-file" && a.ext === "fzv" },
      { text: "Map the unreferenced voice in one action from the voice table.", match: (a) => a.type === "map-voice" },
      { text: "Import a .fzb: it lands as a bank, prompting for the slot.", match: (a) => a.type === "import-file" && a.ext === "fzb" },
      { text: "Import a .fzf and choose whether to open it or just add it to the disk.", match: (a) => a.type === "import-file" && a.ext === "fzf" },
    ],
  },
  {
    id: "J5",
    title: "J5. Grow an instrument over time",
    intro: "Drop more material into an open instrument at any point.",
    steps: [
      { text: "With an instrument open, import more WAVs (import menu or drag in).", match: (a) => a.type === "import-wavs" },
      { text: "Watch capacity update immediately; the new voices take the next free key range.", match: (_a, s) => s.doc.dirty },
      { text: "Map or adjust the new voices, then export.", match: (a) => a.type === "export" },
    ],
  },
  {
    id: "J6",
    title: "J6. Rescue and round trip",
    intro: "Pull a voice out of any disk as .wav or .fzv.",
    steps: [
      { text: "Open a disk and an instrument.", match: (a) => a.type === "open-instrument" },
      { text: "Export a voice as .wav or .fzv from the voice table.", match: (a) => a.type === "extract" },
      { text: "Note: SFZ export back to the DAW is planned, not mocked. Tick to finish." },
    ],
  },
  {
    id: "J7",
    title: "J7. Play it before you burn it",
    intro: "Audition at pitch across the key range, clearly labelled a preview.",
    steps: [
      { text: "Select a voice, then click keys across its range on the keyboard; click height sets velocity.", match: (a) => a.type === "audition" },
      { text: "Shape the DCA envelope and hear the preview change.", match: (a) => a.type === "edit-voice" },
    ],
  },
];

export function journeyById(id: string | null): Journey | null {
  return JOURNEYS.find((j) => j.id === id) ?? null;
}
