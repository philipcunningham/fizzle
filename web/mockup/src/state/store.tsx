// Mockup state store: document state plus view state in one reducer.
// Undo/redo and the revision token demonstrate the behaviour; the
// real history lives in the core later.

import { createContext, useContext, useReducer, type Dispatch, type ReactNode } from "react";
import type { Area, Bank, Doc, Instrument, Voice } from "../data/model";
import { IMAGE_SIZE, clamp, noteName, usedBytes } from "../data/model";
import { emptyDoc, makeArea, makeInstrument, makeVoice, nextId, seedDisk } from "../data/seed";

export type DialogKind =
  | { kind: "newDisk"; then?: PendingImport }
  | { kind: "wavImport"; names: string[] }
  | { kind: "sfzImport"; name: string; oversized: boolean }
  | { kind: "placement"; ext: string; name: string; options: string[]; fromDisk?: boolean; fileId?: string }
  | { kind: "extract"; voiceId: string }
  | { kind: "switchDisk"; intent: "close" | "switch" }
  | { kind: "confirmDelete"; fileId: string; name: string };

export interface PendingImport {
  ext: string;
  names: string[];
}

export type Tab = "voices" | "banks" | "effects";

// One line of feedback in the status bar, the app's only message
// channel. "ok" and "status" lines live in barMsg; each new one
// replaces the last, console style. Errors live in barError so a
// later status line can't bury an unresolved error; they clear on
// dismissal or when a later action resolves them. seq keys the
// one-shot pulse animation.
export interface BarMessage {
  text: string;
  seq: number;
}

export interface State {
  doc: Doc;
  past: Doc[];
  future: Doc[];
  revision: number;
  screen: "start" | "disk" | "inventory";
  tab: Tab;
  selectedVoiceId: string | null;
  selectedBankId: string | null;
  selectedAreaId: string | null;
  dialog: DialogKind | null;
  journeyId: string | null;
  journeyStep: number;
  gestureBase: Doc | null;
  lastActionFull: Action | null;
  actionCount: number;
  barMsg: (BarMessage & { kind: "ok" | "status" }) | null;
  barError: BarMessage | null;
  barSeq: number;
}

export type Action =
  | { type: "new-disk"; label: string; then?: PendingImport }
  | { type: "open-seed-disk" }
  | { type: "close-disk" }
  | { type: "open-instrument"; id: string }
  | { type: "new-instrument" }
  | { type: "import-wavs"; names: string[]; rate: number; stereo: string }
  | { type: "import-sfz"; name: string; mode: "fit" | "split" | "plain" }
  | { type: "import-file"; ext: string; name: string; choice: string; fromDisk?: boolean }
  | { type: "file-actions"; fileId: string }
  | { type: "route-import"; ext: string; names: string[] }
  | { type: "edit-voice"; voiceId: string; patch: (v: Voice) => Voice }
  | { type: "rename-voice"; voiceId: string; name: string }
  | { type: "edit-effects"; controller: number; target: number; value: number }
  | { type: "edit-bend"; value: number }
  | { type: "edit-area"; bankId: string; areaId: string; patch: (a: Area) => Area }
  | { type: "add-area"; bankId: string }
  | { type: "duplicate-area"; bankId: string; areaId: string }
  | { type: "delete-area"; bankId: string; areaId: string }
  | { type: "rename-bank"; bankId: string; name: string }
  | { type: "map-voice"; voiceId: string }
  | { type: "rename-disk"; label: string }
  | { type: "delete-file"; fileId: string }
  | { type: "extract"; what: string }
  | { type: "export" }
  | { type: "audition"; note: number; velocity: number }
  | { type: "gesture-begin" }
  | { type: "gesture-commit" }
  | { type: "undo" }
  | { type: "redo" }
  | { type: "select-voice"; id: string | null }
  | { type: "select-bank"; id: string }
  | { type: "select-area"; id: string | null }
  | { type: "set-tab"; tab: Tab }
  | { type: "set-screen"; screen: State["screen"] }
  | { type: "open-dialog"; dialog: DialogKind }
  | { type: "close-dialog" }
  | { type: "start-journey"; id: string }
  | { type: "advance-journey" }
  | { type: "end-journey" }
  | { type: "dismiss-error" }
  | { type: "clear-message"; seq: number };

export function initialState(): State {
  return {
    doc: emptyDoc(),
    past: [],
    future: [],
    revision: 1,
    screen: "start",
    tab: "voices",
    selectedVoiceId: null,
    selectedBankId: null,
    selectedAreaId: null,
    dialog: null,
    journeyId: null,
    journeyStep: 0,
    gestureBase: null,
    lastActionFull: null,
    actionCount: 0,
    barMsg: null,
    barError: null,
    barSeq: 0,
  };
}

// say posts a routine or success line: the "last action" record. Only
// actions the user can't already see get one; a visible state change is
// its own confirmation.
function say(s: State, kind: "ok" | "status", text: string): State {
  return { ...s, barMsg: { kind, text, seq: s.barSeq + 1 }, barSeq: s.barSeq + 1 };
}

// fail posts an error that outlives later status lines until dismissed
// or resolved.
function fail(s: State, text: string): State {
  return { ...s, barError: { text, seq: s.barSeq + 1 }, barSeq: s.barSeq + 1 };
}

// resolveError clears the standing error once an action addresses it:
// a successful import, an export, or a file deletion that frees space.
function resolveError(s: State): State {
  return s.barError ? { ...s, barError: null } : s;
}

// mutate applies a document change with undo bookkeeping and a fresh
// revision token. During a drag gesture the base snapshot is pushed once.
function mutate(s: State, doc: Doc): State {
  const dirty = { ...doc, dirty: true };
  if (s.gestureBase) {
    return { ...s, doc: dirty, future: [], revision: s.revision + 1 };
  }
  return { ...s, doc: dirty, past: [...s.past, s.doc], future: [], revision: s.revision + 1 };
}

function patchInstrument(doc: Doc, id: string, patch: (i: Instrument) => Instrument): Doc {
  const inst = doc.instruments[id];
  if (!inst) return doc;
  return { ...doc, instruments: { ...doc.instruments, [id]: patch(inst) } };
}

function openInst(s: State): Instrument | null {
  return s.doc.openInstrumentId ? (s.doc.instruments[s.doc.openInstrumentId] ?? null) : null;
}

function nextFreeRange(inst: Instrument): { lo: number; hi: number } {
  const hi = inst.voices.reduce((m, v) => Math.max(m, v.keyHi), 35);
  const lo = clamp(hi + 1, 0, 122);
  return { lo, hi: clamp(lo + 5, 0, 127) };
}

function importWavsIntoDoc(doc: Doc, names: string[], rate: number): Doc {
  // WAVs land in the disk's one instrument, opening or creating it.
  let instId = doc.openInstrumentId ?? Object.values(doc.instruments)[0]?.id ?? null;
  let next = instId ? { ...doc, openInstrumentId: instId } : doc;
  if (!instId) {
    // The "KIT" suffix keeps the instrument's name distinct from its
    // first voice, which otherwise reads identically.
    const base = names[0]?.replace(/\.\w+$/, "").toUpperCase().slice(0, 8) || "NEW";
    const inst = makeInstrument(`${base} KIT`, [], 21);
    instId = inst.id;
    next = replaceFullDump(next, inst, 4096);
  }
  return patchInstrument(next, instId, (inst) => {
    let voices = inst.voices;
    let banks = inst.banks;
    names.forEach((n, i) => {
      const { lo, hi } = nextFreeRange({ ...inst, voices });
      const v = makeVoice(n.replace(/\.wav$/i, "").toUpperCase().slice(0, 12), clamp(lo + 2, 0, 127), lo, hi, 100 + voices.length * 13 + i);
      v.rate = rate as Voice["rate"];
      voices = [...voices, v];
      banks = banks.map((b, bi) => (bi === 0 ? { ...b, areas: [...b.areas, makeArea(v)] } : b));
    });
    return { ...inst, voices, banks };
  });
}

// grow adds bytes to the file backing the given instrument, falling back
// to the first file. Growing the wrong instrument's file misreports where
// imported material landed.
function grow(doc: Doc, bytes: number, instrumentId?: string | null): Doc {
  if (!doc.disk) return doc;
  const idx = instrumentId ? doc.disk.files.findIndex((f) => f.instrumentId === instrumentId) : 0;
  const at = idx >= 0 ? idx : 0;
  const files = doc.disk.files.map((f, i) => (i === at ? { ...f, sizeBytes: f.sizeBytes + bytes } : f));
  return { ...doc, disk: { ...doc.disk, files } };
}

// The firmware stores a disk's full dump under one fixed name; any other
// name makes the sampler misidentify the file (pkg/disk FullDumpName).
// A disk therefore holds at most one full dump: the instrument.
const FULL_DUMP_NAME = "FULL-DATA-FZ";

// replaceFullDump installs inst as the disk's one instrument, replacing
// any existing full dump entry.
function replaceFullDump(doc: Doc, inst: Instrument, sizeBytes: number): Doc {
  const others = (doc.disk?.files ?? []).filter((f) => f.type !== "full");
  const files = [...others, { id: nextId("f"), name: FULL_DUMP_NAME, type: "full" as const, sizeBytes, instrumentId: inst.id }];
  return {
    ...doc,
    disk: doc.disk ? { ...doc.disk, files } : doc.disk,
    instruments: { [inst.id]: inst },
    openInstrumentId: inst.id,
  };
}

export function reducer(state: State, action: Action): State {
  let s = reduce(state, action);
  if (s !== state) {
    s = { ...s, lastActionFull: action, actionCount: state.actionCount + 1 };
  }
  return s;
}

function reduce(s: State, action: Action): State {
  switch (action.type) {
    case "new-disk": {
      const doc: Doc = { disk: { label: action.label, files: [] }, instruments: {}, openInstrumentId: null, dirty: true };
      let next: State = { ...mutate(s, doc), screen: "disk", dialog: null };
      if (action.then) {
        next = routeImport(next, action.then.ext, action.then.names);
      }
      return next;
    }
    case "open-seed-disk": {
      // Mockup stand-in for the file picker: opens the canned corpus image.
      const seeded = seedImport();
      const opened: State = { ...s, doc: seeded, past: [], future: [], revision: s.revision + 1, screen: "disk", dialog: null };
      return say(opened, "status", `disk opened: ${seeded.disk?.label ?? ""}`);
    }
    case "close-disk":
      // Keep any running journey: the guide survives the trip back to
      // the start screen.
      return {
        ...initialState(),
        journeyId: s.journeyId,
        journeyStep: s.journeyStep,
        revision: s.revision + 1,
      };
    case "open-instrument": {
      const doc = { ...s.doc, openInstrumentId: action.id };
      const inst = doc.instruments[action.id];
      return {
        ...s,
        doc,
        tab: "voices",
        selectedVoiceId: inst?.voices[0]?.id ?? null,
        selectedBankId: inst?.banks[0]?.id ?? null,
        revision: s.revision + 1,
      };
    }
    case "new-instrument": {
      if (!s.doc.disk) return s;
      // One full dump per disk: creating is only possible when none exists.
      if (s.doc.disk.files.some((f) => f.type === "full")) return s;
      const inst = makeInstrument("NEW INST", [], 55);
      const doc = replaceFullDump(s.doc, inst, 4096);
      return { ...mutate(s, doc), selectedBankId: inst.banks[0].id };
    }
    case "import-wavs": {
      const bytes = action.names.length * 68_000;
      if (s.doc.disk && usedBytes(s.doc.disk) + bytes > IMAGE_SIZE * 2) {
        const overKB = Math.ceil((usedBytes(s.doc.disk) + bytes - IMAGE_SIZE * 2) / 1024);
        // Phrased as the failed action, not a standing condition: the
        // error clears on the next successful action, and the lasting
        // truth lives in the capacity readout.
        return fail({ ...s, dialog: null }, `import rejected: ${overKB} KB over the two disk ceiling`);
      }
      let doc = importWavsIntoDoc(s.doc, action.names, action.rate);
      doc = grow(doc, bytes, doc.openInstrumentId);
      const inst = doc.openInstrumentId ? doc.instruments[doc.openInstrumentId] : null;
      let next = mutate(s, doc);
      next = {
        ...next,
        dialog: null,
        selectedVoiceId: inst?.voices[inst.voices.length - 1]?.id ?? s.selectedVoiceId,
      };
      const what = action.names.length === 1 ? action.names[0] : `${action.names.length} WAVs`;
      // Auto-mapping runs off the top of the keyboard once earlier
      // voices reach the ceiling; further voices pile onto the same
      // final range. Piling is legal but unplayable, so say so.
      const piled = inst ? inst.voices.filter((v) => v.keyLo === 122 && v.keyHi === 127).length : 0;
      if (piled > 1) {
        return say(resolveError(next), "status", `imported ${what}; keyboard full, ${piled} voices share ${noteName(122)}..${noteName(127)}`);
      }
      return say(resolveError(next), "ok", `imported ${what}`);
    }
    case "import-sfz": {
      const inst = makeInstrument(action.name.toUpperCase().replace(/\.SFZ$/, ""), ["KICK", "SNARE", "HATS", "BASS", "PAD LO", "PAD HI", "LEAD", "FX"], 33);
      let doc: Doc = s.doc.disk ? s.doc : { ...s.doc, disk: { label: inst.name, files: [] } };
      doc = replaceFullDump(doc, inst, action.mode === "fit" ? 900_000 : 1_500_000);
      const next: State = { ...mutate(s, doc), screen: "disk", dialog: null, selectedVoiceId: inst.voices[0].id, selectedBankId: inst.banks[0].id };
      return say(resolveError(next), "ok", `converted ${action.name}`);
    }
    case "import-file": {
      return routeChoice(s, action.ext, action.name, action.choice, action.fromDisk);
    }
    case "file-actions": {
      // A bank or voice file already on the disk gets the verbs the spec
      // promises: add to the instrument, export a copy, or delete.
      const f = s.doc.disk?.files.find((x) => x.id === action.fileId);
      if (!f) return s;
      const ext = (f.name.split(".").pop() ?? "").toLowerCase();
      const inst = openInst(s) ?? Object.values(s.doc.instruments)[0] ?? null;
      const add = inst
        ? ext === "fzb"
          ? `Add as bank ${inst.banks.length + 1} of ${inst.name}`
          : `Add to ${inst.name}`
        : "Add to a new instrument";
      const options = [add, "Export a copy", "Delete file"];
      return { ...s, dialog: { kind: "placement", ext, name: f.name, options, fromDisk: true, fileId: f.id } };
    }
    case "route-import":
      return routeImport(s, action.ext, action.names);
    case "edit-voice": {
      const instId = s.doc.openInstrumentId;
      if (!instId) return s;
      const doc = patchInstrument(s.doc, instId, (inst) => ({
        ...inst,
        voices: inst.voices.map((v) => (v.id === action.voiceId ? action.patch(v) : v)),
      }));
      return mutate(s, doc);
    }
    case "rename-voice": {
      const instId = s.doc.openInstrumentId;
      if (!instId) return s;
      const doc = patchInstrument(s.doc, instId, (inst) => ({
        ...inst,
        voices: inst.voices.map((v) => (v.id === action.voiceId ? { ...v, name: action.name } : v)),
      }));
      return mutate(s, doc);
    }
    case "edit-effects": {
      const instId = s.doc.openInstrumentId;
      if (!instId) return s;
      const doc = patchInstrument(s.doc, instId, (inst) => {
        const matrix = inst.effects.matrix.map((row, r) =>
          row.map((cell, c) => (r === action.controller && c === action.target ? clamp(action.value, 0, 127) : cell)),
        );
        return { ...inst, effects: { ...inst.effects, matrix } };
      });
      return mutate(s, doc);
    }
    case "edit-bend": {
      const instId = s.doc.openInstrumentId;
      if (!instId) return s;
      const doc = patchInstrument(s.doc, instId, (inst) => ({
        ...inst,
        effects: { ...inst.effects, pitchBendRange: clamp(action.value, 0, 12) },
      }));
      return mutate(s, doc);
    }
    case "edit-area": {
      const doc = patchBank(s, action.bankId, (b) => ({
        ...b,
        areas: b.areas.map((a) => (a.id === action.areaId ? action.patch(a) : a)),
      }));
      return doc ? mutate(s, doc) : s;
    }
    case "add-area": {
      const inst = openInst(s);
      if (!inst) return s;
      const v = inst.voices[0];
      if (!v) return s;
      const area = makeArea(v);
      const doc = patchBank(s, action.bankId, (b) => ({ ...b, areas: [...b.areas, area] }));
      return doc ? { ...mutate(s, doc), selectedAreaId: area.id } : s;
    }
    case "duplicate-area": {
      let dupId: string | null = null;
      const doc = patchBank(s, action.bankId, (b) => {
        const idx = b.areas.findIndex((a) => a.id === action.areaId);
        if (idx < 0) return b;
        const src = b.areas[idx];
        // Complementary velocity split: the duplicate takes the lower
        // half and the original keeps the upper half, so the two layers
        // never fight over the same velocities. The pair sits together.
        const mid = Math.floor((src.velLo + src.velHi) / 2);
        const dup = { ...src, id: nextId("a"), velHi: mid };
        const upper = { ...src, velLo: Math.min(mid + 1, src.velHi) };
        dupId = dup.id;
        return { ...b, areas: [...b.areas.slice(0, idx), dup, upper, ...b.areas.slice(idx + 1)] };
      });
      if (!doc) return s;
      return { ...mutate(s, doc), selectedAreaId: dupId ?? s.selectedAreaId };
    }
    case "delete-area": {
      const doc = patchBank(s, action.bankId, (b) => ({ ...b, areas: b.areas.filter((a) => a.id !== action.areaId) }));
      return doc ? { ...mutate(s, doc), selectedAreaId: null } : s;
    }
    case "rename-bank": {
      const doc = patchBank(s, action.bankId, (b) => ({ ...b, name: action.name }));
      return doc ? mutate(s, doc) : s;
    }
    case "map-voice": {
      const inst = openInst(s);
      if (!inst) return s;
      const v = inst.voices.find((x) => x.id === action.voiceId);
      if (!v) return s;
      const { lo, hi } = nextFreeRange(inst);
      const area = { ...makeArea(v), keyLo: lo, keyHi: hi };
      const doc = patchBank(s, inst.banks[0].id, (b) => ({ ...b, areas: [...b.areas, area] }));
      if (!doc) return s;
      // The new Area lands on the other tab, out of sight, so the bar
      // records where the voice went; a clamped range gets called out.
      const overlaps = inst.voices.some((x) => x.keyHi >= lo);
      const where = `${v.name} mapped to ${noteName(lo)}..${noteName(hi)} in ${inst.banks[0].name}`;
      return say(mutate(s, doc), "status", overlaps ? `${where}; keyboard full, ranges overlap` : where);
    }
    case "rename-disk": {
      if (!s.doc.disk) return s;
      return mutate(s, { ...s.doc, disk: { ...s.doc.disk, label: action.label } });
    }
    case "delete-file": {
      if (!s.doc.disk) return s;
      const target = s.doc.disk.files.find((f) => f.id === action.fileId);
      let doc: Doc = { ...s.doc, disk: { ...s.doc.disk, files: s.doc.disk.files.filter((f) => f.id !== action.fileId) } };
      if (target?.instrumentId) {
        // Deleting the full dump deletes the instrument it backs.
        const { [target.instrumentId]: _gone, ...rest } = doc.instruments;
        doc = {
          ...doc,
          instruments: rest,
          openInstrumentId: doc.openInstrumentId === target.instrumentId ? null : doc.openInstrumentId,
        };
      }
      // The bar is also the record: deletion posts a line, and
      // deleting the full dump names the instrument it took.
      const gone = target?.instrumentId ? (s.doc.instruments[target.instrumentId]?.name ?? target.name) : target?.name;
      const next = resolveError({ ...mutate(s, doc), dialog: null });
      return gone ? say(next, "status", `deleted ${gone}`) : next;
    }
    case "extract":
      // Fresh object so the journey guide sees the action.
      return say({ ...s }, "ok", `extracted ${action.what}`);
    case "export": {
      if (!s.doc.disk) return s;
      const next = { ...s, doc: { ...s.doc, dirty: false } };
      return say(resolveError(next), "ok", `exported ${s.doc.disk.label}.img`);
    }
    case "audition":
      // Fresh object so the journey guide sees the action and advances.
      return { ...s };
    case "gesture-begin":
      return s.gestureBase ? s : { ...s, gestureBase: s.doc };
    case "gesture-commit": {
      if (!s.gestureBase) return s;
      const changed = s.gestureBase !== s.doc;
      return {
        ...s,
        gestureBase: null,
        past: changed ? [...s.past, s.gestureBase] : s.past,
      };
    }
    case "undo": {
      const prev = s.past[s.past.length - 1];
      if (!prev) return s;
      return { ...s, doc: prev, past: s.past.slice(0, -1), future: [s.doc, ...s.future], revision: s.revision + 1 };
    }
    case "redo": {
      const nxt = s.future[0];
      if (!nxt) return s;
      return { ...s, doc: nxt, past: [...s.past, s.doc], future: s.future.slice(1), revision: s.revision + 1 };
    }
    case "select-voice":
      return { ...s, selectedVoiceId: action.id };
    case "select-bank":
      return { ...s, selectedBankId: action.id, selectedAreaId: null };
    case "select-area":
      return { ...s, selectedAreaId: action.id };
    case "set-tab":
      return { ...s, tab: action.tab };
    case "set-screen":
      return { ...s, screen: action.screen };
    case "open-dialog":
      return { ...s, dialog: action.dialog };
    case "close-dialog":
      return { ...s, dialog: null };
    case "start-journey":
      return { ...s, journeyId: action.id, journeyStep: 0 };
    case "advance-journey":
      return { ...s, journeyStep: s.journeyStep + 1 };
    case "end-journey":
      return { ...s, journeyId: null, journeyStep: 0 };
    case "dismiss-error":
      return s.barError ? { ...s, barError: null } : s;
    case "clear-message":
      // Status and success lines expire after 5 seconds (the timer
      // lives in the shell). The seq guard keeps a stale timer from
      // killing a newer message. Errors never expire.
      return s.barMsg?.seq === action.seq ? { ...s, barMsg: null } : s;
    default:
      return s;
  }
}

function patchBank(s: State, bankId: string, patch: (b: Bank) => Bank): Doc | null {
  const instId = s.doc.openInstrumentId;
  if (!instId) return null;
  return patchInstrument(s.doc, instId, (inst) => ({
    ...inst,
    banks: inst.banks.map((b) => (b.id === bankId ? patch(b) : b)),
  }));
}

// routeImport applies the placement matrix for a fresh drop or pick.
export function routeImport(s: State, ext: string, names: string[]): State {
  const hasDisk = s.doc.disk !== null;

  if (!hasDisk && ext !== "img") {
    // Never fails for want of a disk; ask for a label, then continue.
    return { ...s, dialog: { kind: "newDisk", then: { ext, names } } };
  }
  const theInst = openInst(s) ?? Object.values(s.doc.instruments)[0] ?? null;

  switch (ext) {
    case "img":
      // The prompt is scoped to unexported changes.
      if (hasDisk && s.doc.dirty) return { ...s, dialog: { kind: "switchDisk", intent: "switch" } };
      return reduce(s, { type: "open-seed-disk" });
    case "wav":
      return { ...s, dialog: { kind: "wavImport", names } };
    case "sfz":
      return { ...s, dialog: { kind: "sfzImport", name: names[0] ?? "INSTRUMENT.sfz", oversized: true } };
    case "fzf": {
      // One full dump per disk: an incoming dump replaces the instrument.
      if (s.doc.disk?.files.some((f) => f.type === "full")) {
        return { ...s, dialog: { kind: "placement", ext, name: names[0] ?? "DUMP.fzf", options: ["Replace the instrument"] } };
      }
      return routeChoice(s, ext, names[0] ?? "DUMP.fzf", "Replace the instrument");
    }
    case "fzb": {
      if (theInst) {
        return {
          ...s,
          dialog: {
            kind: "placement",
            ext,
            name: names[0] ?? "BANK.fzb",
            options: [`Add as bank ${theInst.banks.length + 1} of ${theInst.name}`],
          },
        };
      }
      return routeChoice(s, ext, names[0] ?? "BANK.fzb", "Add to a new instrument");
    }
    case "fzv":
      if (theInst) return routeChoice(s, ext, names[0] ?? "VOICE.fzv", "Join voice list");
      return routeChoice(s, ext, names[0] ?? "VOICE.fzv", "Add to a new instrument");
    default:
      return s;
  }
}

// targetInstrument resolves the disk's one instrument, opening it if it
// isn't already, so added material lands where the user then looks.
function targetInstrument(base: State): { state: State; inst: Instrument | null } {
  const open = openInst(base);
  if (open) return { state: base, inst: open };
  const only = Object.values(base.doc.instruments)[0] ?? null;
  if (only) return { state: { ...base, doc: { ...base.doc, openInstrumentId: only.id } }, inst: only };
  return { state: base, inst: null };
}

// routeChoice applies a placement decision. fromDisk marks material that
// already lives on the disk: it adds to instruments without growing any
// file, since nothing new lands on the disk.
function routeChoice(s: State, ext: string, name: string, choice: string, fromDisk = false): State {
  const base = { ...s, dialog: null } as State;
  if (choice === "Export a copy") {
    return say(base, "ok", `exported a copy of ${name}`);
  }
  switch (ext) {
    case "fzf": {
      // Replaces the disk's one full dump; the display name comes from
      // the dropped file, the on disk name is always FULL-DATA-FZ.
      const inst = makeInstrument(name.replace(/\.fzf$/i, "").toUpperCase(), ["ONE", "TWO", "THREE", "FOUR"], 77);
      const doc = replaceFullDump(base.doc, inst, 240_000);
      const next: State = { ...mutate(base, doc), tab: "voices", selectedVoiceId: inst.voices[0].id, selectedBankId: inst.banks[0].id };
      return say(next, "status", `${inst.name} is now the instrument`);
    }
    case "fzb": {
      const { state: with_, inst } = targetInstrument(base);
      if (inst && choice !== "Add to a new instrument") {
        const doc = patchInstrument(with_.doc, inst.id, (i) => ({
          ...i,
          banks: [...i.banks, { id: nextId("b"), name: name.replace(/\.fzb$/i, "").toUpperCase(), areas: [] }],
        }));
        const grown = fromDisk ? doc : grow(doc, 96_000, inst.id);
        return say(mutate(with_, grown), "status", `${name} added as bank ${inst.banks.length + 1} of ${inst.name}`);
      }
      const ni = makeInstrument(name.replace(/\.fzb$/i, "").toUpperCase(), ["A", "B"], 88);
      return say(mutate(base, replaceFullDump(base.doc, ni, 96_000)), "status", `${name} placed in a new instrument`);
    }
    case "fzv": {
      const { state: with_, inst } = targetInstrument(base);
      if (inst && choice !== "Add to a new instrument") {
        const { lo, hi } = nextFreeRange(inst);
        const v = makeVoice(name.replace(/\.fzv$/i, "").toUpperCase().slice(0, 12), clamp(lo + 2, 0, 127), lo, hi, 99);
        const doc = patchInstrument(with_.doc, inst.id, (i) => ({ ...i, voices: [...i.voices, v] }));
        let next = mutate(with_, fromDisk ? doc : grow(doc, v.sizeBytes, inst.id));
        next = { ...next, selectedVoiceId: v.id };
        return say(next, "status", `${v.name} joined the voice list`);
      }
      const ni = makeInstrument(name.replace(/\.fzv$/i, "").toUpperCase(), [name.replace(/\.fzv$/i, "").toUpperCase()], 66);
      return say(mutate(base, replaceFullDump(base.doc, ni, 44_000)), "status", `${name} placed in a new instrument`);
    }
    default:
      return base;
  }
}

function seedImport(): Doc {
  return seedDisk().doc;
}

const StoreCtx = createContext<{ state: State; dispatch: Dispatch<Action> } | null>(null);

export function StoreProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, undefined, initialState);
  return <StoreCtx.Provider value={{ state, dispatch }}>{children}</StoreCtx.Provider>;
}

export function useStore() {
  const ctx = useContext(StoreCtx);
  if (!ctx) throw new Error("useStore outside provider");
  return ctx;
}

export function useOpenInstrument(): Instrument | null {
  const { state } = useStore();
  return state.doc.openInstrumentId ? (state.doc.instruments[state.doc.openInstrumentId] ?? null) : null;
}
