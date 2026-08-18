// The fizzle shell: the mockup's validated frame over the real core.
// One direction of flow: a gesture becomes one core call, the
// returned snapshot's revision keys the query cache, and the UI
// renders from the snapshot. The document lives in the core; this
// file owns only view state (tab, selections, dialogs, status line).
import {
  QueryClientProvider,
  keepPreviousData,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import type {
  Channel,
  Core,
  CoreError,
  CoreResult,
  SampleRate,
  Snapshot,
} from "../boundary/contract";
import { IMAGE_SIZE, isCoreCrash } from "../boundary/contract";
import type { DialogActions, PendingDialog } from "../dialogs/Dialogs";
import { Dialogs } from "../dialogs/Dialogs";
import { createQueryClient, queryKeys } from "../queries/client";
import { BanksAreas } from "../screens/BanksAreas";
import { EffectsScreen } from "../screens/EffectsScreen";
import { NoInstrumentPanel } from "../screens/NoInstrumentPanel";
import { StartScreen } from "../screens/StartScreen";
import { VoiceEditor } from "../screens/VoiceEditor";
import { CapacityBar } from "../ui/CapacityBar";
import { Keyboard } from "../ui/Keyboard";
import { createAudition } from "../ui/audition";
import { clamp, formatBytes } from "../ui/format";
import { subscribeMIDI } from "../ui/midi";
import { noteName } from "../ui/notes";
import type { NamedBytes, Placement } from "../viewstate/place";
import { classifyInput, toFileMap } from "../viewstate/place";
import { matchAreas } from "../viewstate/mapping";
import { CrashPanel, ErrorBoundary } from "./ErrorBoundary";
import { dropEntries, walkEntries } from "./drop";
import { wavChannels } from "./wavinfo";

/**
 * True for a target that owns its own undo stack. The document's undo
 * hotkey must leave those alone.
 */
function isTextEntry(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  if (tag === "TEXTAREA" || tag === "SELECT") return true;
  // An input only owns an undo stack when it takes typing. A range
  // slider does not, and the waveform's zoom is one, so treating every
  // input alike left Cmd+Z dead after touching the zoom.
  return target instanceof HTMLInputElement && target.type !== "range";
}

/** A message from a thrown value, for the status bar. */
function describe(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/**
 * The widest channel count across an SFZ folder's WAVs: 1 when every
 * file is mono, so the conversion prompt can drop the stereo
 * question. Null when any file is unreadable here, which keeps the
 * question rather than guessing. The WAV import dialog asks the core
 * instead, through the estimate.
 */
function batchChannels(files: NamedBytes[]): number | null {
  return files.reduce<number | null>((worst, f) => {
    const c = wavChannels(f.bytes);
    return worst === null || c === null ? null : Math.max(worst, c);
  }, 1);
}

type Tab = "voices" | "banks" | "effects";

const TABS: { id: Tab; label: string }[] = [
  { id: "voices", label: "Voices" },
  { id: "banks", label: "Banks and Areas" },
  { id: "effects", label: "Effects" },
];

// One panel is mounted at a time, so one id serves it and the selected
// tab points at that.
const TABPANEL_ID = "fz-tabpanel";

const KEYBOARD_OCTAVES = 6;
const KEYBOARD_LOW_MAX = 127 - KEYBOARD_OCTAVES * 12;

// Desktop Chromium is the supported platform (N4); the save picker is
// its reliable tell. Elsewhere the app still runs, behind a notice.
function isSupportedBrowser(): boolean {
  return "showSaveFilePicker" in window;
}

/**
 * The boundary's last resort export (E5). It reads the document from
 * the core alone, so it still answers when the shell that owns the
 * normal export path is the thing that crashed. A split document
 * writes both images. Nothing renders to carry a message here, so a
 * refusal or a failed write leaves the crash screen's other choices.
 */
function lastResortExport(core: Core): void {
  const write = (result: CoreResult<Uint8Array>, name: string): Promise<unknown> =>
    result.ok ? saveFile(result.value, name).catch(() => null) : Promise.resolve(null);

  void core.snapshot().then((snapshot) => {
    const disk = snapshot.ok ? snapshot.value.disk : null;
    const label = disk?.label.trim() ?? "DISK";
    if (disk?.disks === 2) {
      void core
        .exportImageAt(0)
        .then((one) => write(one, `${label}-1.img`))
        .then(() => core.exportImageAt(1))
        .then((two) => write(two, `${label}-2.img`));
      return;
    }
    void core.exportImage().then((image) => write(image, `${label}.img`));
  });
}

export function App({ core }: { core: Core }) {
  const client = useMemo(() => createQueryClient(), []);
  // The boundary sits above the whole shell, so a throw in the topbar,
  // a dialog, the status bar, or the start screen is contained too
  // (E5). A second boundary inside the workspace keeps the frame alive
  // when only the sidebar or the tab body fails.
  return (
    <QueryClientProvider client={client}>
      <ErrorBoundary
        onExport={() => {
          lastResortExport(core);
        }}
      >
        <Shell core={core} />
      </ErrorBoundary>
    </QueryClientProvider>
  );
}

interface BarMsg {
  text: string;
  kind: "status" | "ok";
  seq: number;
}

function Shell({ core }: { core: Core }) {
  const [revision, setRevision] = useState(0);
  const [tab, setTab] = useState<Tab>("voices");
  const [selectedSlot, setSelectedSlot] = useState<number | null>(null);
  const [selectedBank, setSelectedBank] = useState(0);
  const [selectedArea, setSelectedArea] = useState<number | null>(null);
  const [selectedLoop, setSelectedLoop] = useState(0);
  const [dialog, setDialog] = useState<PendingDialog | null>(null);
  const [busy, setBusy] = useState(false);
  // The two conversion answers live here rather than in the dialog,
  // so the import estimate can re-query as they change.
  const [rate, setRate] = useState("18");
  const [stereo, setStereo] = useState("Mix");
  // A conversion failure, shown inside the open dialog (E1).
  const [convertError, setConvertError] = useState<string | null>(null);
  // A lone .sfz awaiting its samples. The browser cannot read the
  // files an .sfz references unless the user hands them over, so the
  // folder picked next joins this file. It survives only the hop from
  // the prompt to the picker: every other dialog open and the close
  // path clear it, so a stale .sfz cannot leak into a later import.
  const [pendingSfz, setPendingSfz] = useState<NamedBytes | null>(null);
  const [dirty, setDirty] = useState(false);
  const [barError, setBarError] = useState<{ text: string; seq: number } | null>(null);
  // Set once the core says it can no longer answer: the session is
  // over and only a reload moves on (E5).
  const [fatal, setFatal] = useState<CoreError | null>(null);
  const [barMsg, setBarMsg] = useState<BarMsg | null>(null);
  const [filesCollapsed, setFilesCollapsed] = useState(false);
  const [renamingDisk, setRenamingDisk] = useState(false);
  const [kbLow, setKbLow] = useState(24);
  const [browserNotice, setBrowserNotice] = useState(() => !isSupportedBrowser());
  const seqRef = useRef(0);

  const anyRef = useRef<HTMLInputElement>(null);
  const folderRef = useRef<HTMLInputElement>(null);
  const twinRef = useRef<HTMLInputElement>(null);

  // A dismissed OS folder picker fires cancel, not change, and React
  // declares no onCancel for inputs: the remembered .sfz must not
  // outlive the pick it was waiting on, so the listener lands by ref.
  useEffect(() => {
    const input = folderRef.current;
    if (!input) return;
    const forget = () => {
      setPendingSfz(null);
    };
    input.addEventListener("cancel", forget);
    return () => {
      input.removeEventListener("cancel", forget);
    };
  }, []);

  // The CLI debug flag's analogue (E4): ?debug=1 raises the core's
  // console log level for the session.
  useEffect(() => {
    if (new URLSearchParams(window.location.search).get("debug") === "1") {
      void core.setDebug(true);
    }
  }, [core]);

  const snapshot = useQuery({
    queryKey: queryKeys.snapshot(revision),
    queryFn: () => core.snapshot(),
    placeholderData: keepPreviousData,
  });
  const schemaQuery = useQuery({
    queryKey: queryKeys.schema(),
    queryFn: () => core.schema(),
  });
  const snap = snapshot.data?.ok ? snapshot.data.value : null;
  const disk = snap?.disk ?? null;
  const instrument = disk?.instrument ?? null;
  const schema = schemaQuery.data?.ok ? schemaQuery.data.value : [];

  const voice =
    instrument?.voices.find((v) => v.slot === selectedSlot) ?? instrument?.voices[0] ?? null;

  // The keyboard focuses the banks tab's selected area voice, else
  // the selected voice; the highlight follows the same focus.
  const bank = instrument?.banks[Math.min(selectedBank, (instrument.banks.length || 1) - 1)];
  const area = selectedArea === null ? null : (bank?.areas[selectedArea] ?? null);
  const areaVoice = area
    ? (instrument?.voices.find((v) => v.slot === area.voiceSlot) ?? null)
    : null;
  const focusVoice = tab === "banks" && areaVoice ? areaVoice : voice;
  const highlight =
    tab === "banks"
      ? area
        ? [{ lo: area.keyLow, hi: area.keyHigh }]
        : (bank?.areas.map((a) => ({ lo: a.keyLow, hi: a.keyHigh })) ?? null)
      : voice &&
          typeof voice.params?.["keyLow"] === "number" &&
          typeof voice.params["keyHigh"] === "number"
        ? [{ lo: voice.params["keyLow"], hi: voice.params["keyHigh"] }]
        : null;
  // On the banks tab both the marker and the sound come from the
  // selected area's root, the cent byte the hardware pitches by; the
  // voices tab reads the voice header's own root.
  const focusRoot =
    tab === "banks"
      ? (area?.root ?? null)
      : typeof focusVoice?.params?.["rootKey"] === "number"
        ? focusVoice.params["rootKey"]
        : null;

  // Peaks for the selected voice's waveform (R17): the full extent,
  // zoomed inside wavesurfer.
  const frames = voice?.voice?.frames ?? 0;
  // Keyed by the audio itself, not the revision: a knob turn changes
  // the document sixty times a second but never the samples, and
  // re-decoding the PCM on each one would be pure waste.
  const peaksQuery = useQuery({
    queryKey: queryKeys.peaks(0, `slot-${String(voice?.slot ?? -1)}:${voice?.audioKey ?? ""}`),
    queryFn: () => core.slotPeaks(voice?.slot ?? 0, 0, frames, 2048),
    enabled: voice !== null && frames > 0,
    placeholderData: keepPreviousData,
  });
  const peaks = peaksQuery.data?.ok ? peaksQuery.data.value : null;

  // The audition path (R20 to R22): the focus voice's PCM prefetched
  // per revision, the engine created lazily on the first gesture.
  const audition = useMemo(() => createAudition(), []);
  const queryClient = useQueryClient();
  const heldNotes = useRef(new Map<number, () => void>());
  const [auditioning, setAuditioning] = useState(false);
  const auditionQuery = useQuery({
    queryKey: ["audition", focusVoice?.slot ?? -1, focusVoice?.audioKey ?? ""],
    queryFn: () => core.auditionSlot(focusVoice?.slot ?? 0),
    enabled: focusVoice !== null,
    placeholderData: keepPreviousData,
  });
  const auditionData = auditionQuery.data?.ok ? auditionQuery.data.value : null;

  // The banks tab plays whatever the pressed key maps to, so every
  // slot the bank references prefetches into the same audition cache:
  // the first press then plays without waiting on a decode.
  useEffect(() => {
    if (tab !== "banks" || !bank) return;
    const slots = new Set(bank.areas.map((a) => a.voiceSlot));
    for (const slot of slots) {
      const slotVoice = instrument.voices.find((v) => v.slot === slot);
      void queryClient.prefetchQuery({
        queryKey: ["audition", slot, slotVoice?.audioKey ?? ""],
        queryFn: () => core.auditionSlot(slot),
      });
    }
  }, [tab, bank, instrument, queryClient, core]);

  // The import dialog's pre-flight (R6): the core's estimate for the
  // pending files at the chosen rate and stereo answer. Keyed by the
  // batch's shape and both answers, so a radio change re-asks and a
  // new drop starts fresh.
  const wavDialog = dialog?.kind === "wavImport" ? dialog : null;
  const estimateQuery = useQuery({
    // revision is part of the key: the answer reads the live document
    // (room, free sectors), so an edit must invalidate it. The cached
    // verdict of the previous key stays on screen while the new one
    // is in flight, so a shown refusal cannot lapse into an enabled
    // Convert between radio click and reply.
    queryKey: [
      "estimate",
      revision,
      wavDialog?.files.map((f) => `${f.name}:${String(f.bytes.length)}`).join("|") ?? "",
      rate,
      stereo,
    ],
    queryFn: () =>
      core.estimateImport(
        toFileMap(wavDialog?.files ?? []),
        (Number(rate) * 1000) as SampleRate,
        stereo.toLowerCase() as Channel,
      ),
    enabled: wavDialog !== null,
    placeholderData: keepPreviousData,
  });
  const estimateResult = wavDialog !== null ? (estimateQuery.data ?? null) : null;
  const estimate = estimateResult?.ok ? estimateResult.value : null;
  const estimateError =
    estimateResult !== null && !estimateResult.ok ? estimateResult.error.message : null;

  /**
   * One instrument slot's PCM through the same cache the audition
   * query fills, so a banks tab press shares payloads with the voice
   * preview instead of re-decoding.
   */
  const slotPCM = (slot: number, audioKey: string) =>
    queryClient.fetchQuery({
      queryKey: ["audition", slot, audioKey],
      queryFn: () => core.auditionSlot(slot),
    });

  const noteOn = (note: number, velocity: number) => {
    // The banks tab plays the mapping (R12): the pressed key resolves
    // through the bank's areas by key and velocity range, every match
    // sounds (the hardware layers overlaps), and each sounds at its
    // area's root, the cent byte the sampler itself pitches by. A key
    // no area covers stays silent.
    if (tab === "banks" && bank) {
      const matches = matchAreas(bank.areas, note, velocity);
      if (matches.length === 0) return;
      heldNotes.current.get(note)?.();
      const releases: (() => void)[] = [];
      let released = false;
      for (const matched of matches) {
        const slotVoice = instrument.voices.find((v) => v.slot === matched.voiceSlot);
        void slotPCM(matched.voiceSlot, slotVoice?.audioKey ?? "").then((r) => {
          if (!r.ok || released) return;
          releases.push(
            audition.play({
              pcm: r.value.pcm,
              sampleRate: slotVoice?.voice?.sampleRate ?? r.value.sampleRate,
              root: matched.root,
              note,
              velocity,
              ...(slotVoice?.voice ? { dca: slotVoice.voice.dca } : {}),
            }),
          );
        });
      }
      heldNotes.current.set(note, () => {
        released = true;
        for (const release of releases) release();
      });
      setAuditioning(true);
      return;
    }
    if (!auditionData) return;
    heldNotes.current.get(note)?.();
    // Pitch comes from the snapshot, not from the cached payload. The
    // query above is keyed by the audio identity, so an edit to the
    // root key or the rate leaves the PCM (rightly) untouched and its
    // copy of those two values stale. R20 asks for the correct pitch.
    const release = audition.play({
      pcm: auditionData.pcm,
      sampleRate: focusVoice?.voice?.sampleRate ?? auditionData.sampleRate,
      root: focusRoot ?? auditionData.root,
      note,
      velocity,
      ...(focusVoice?.voice ? { dca: focusVoice.voice.dca } : {}),
    });
    heldNotes.current.set(note, release);
    setAuditioning(true);
  };
  const noteOff = (note: number) => {
    heldNotes.current.get(note)?.();
    heldNotes.current.delete(note);
    if (heldNotes.current.size === 0) setAuditioning(false);
  };
  const noteOnRef = useRef(noteOn);
  noteOnRef.current = noteOn;
  const noteOffRef = useRef(noteOff);
  noteOffRef.current = noteOff;
  useEffect(() => {
    return subscribeMIDI({
      onNoteOn: (n, v) => {
        noteOnRef.current(n, v);
      },
      onNoteOff: (n) => {
        noteOffRef.current(n);
      },
    });
  }, []);

  // Status messages expire after five seconds, the mockup's rule.
  useEffect(() => {
    if (!barMsg) return;
    const seq = barMsg.seq;
    const t = setTimeout(() => {
      setBarMsg((m) => (m?.seq === seq ? null : m));
    }, 5000);
    return () => {
      clearTimeout(t);
    };
  }, [barMsg]);

  // Cmd or Ctrl Z undoes; with shift it redoes. The listener is on the
  // window, so it must not swallow the keys of the fields inside it:
  // Cmd+Z while renaming a voice means undo my typing, not undo the
  // document. Same rule as the table rows, one level up.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== "z") return;
      if (isTextEntry(e.target)) return;
      e.preventDefault();
      void (e.shiftKey ? core.redo() : core.undo()).then(applyEdit);
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [core]);

  // R3: closing the tab with unexported changes warns. N3 rules out
  // persistence, so the browser's own prompt is all that stands between
  // an accidental Cmd+W and the whole session.
  useEffect(() => {
    if (!dirty) return;
    // Chromium shows its own wording and acts on preventDefault alone;
    // the legacy returnValue string is deprecated and N4 scopes us to
    // desktop Chromium.
    const onUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", onUnload);
    return () => {
      window.removeEventListener("beforeunload", onUnload);
    };
  }, [dirty]);

  const say = (text: string, kind: BarMsg["kind"] = "status") => {
    seqRef.current += 1;
    setBarMsg({ text, kind, seq: seqRef.current });
  };
  const fail = (text: string) => {
    seqRef.current += 1;
    setBarError({ text, seq: seqRef.current });
  };

  /**
   * Every refused call reports through here (E1). A fatal envelope is
   * the exception: the core can no longer answer, so a dismissible
   * line in the status bar would offer a session that isn't there.
   * That one gets the crash panel and a reload (E5).
   */
  const report = (error: CoreError) => {
    if (isCoreCrash(error)) setFatal(error);
    // The message is written for the user; the machine code is for
    // the console and a bug report, not the status bar.
    else fail(error.message);
  };

  const apply = (result: CoreResult<Snapshot>) => {
    if (result.ok) {
      setBarError(null);
      setRevision(result.value.revision);
    } else {
      report(result.error);
    }
    return result.ok;
  };

  // A mutation marks the document dirty (the export guard's signal).
  const applyEdit = (result: CoreResult<Snapshot>) => {
    if (apply(result)) setDirty(true);
    return result.ok;
  };

  // Undo and redo move the document away from whatever was last
  // written, so they dirty it too. Treating them as clean let a redone
  // edit be discarded silently at close.
  const undo = () => {
    void core.undo().then(applyEdit);
  };
  const redo = () => {
    void core.redo().then(applyEdit);
  };

  // Bumped whenever the dialog closes: a conversion chain captures
  // the value at launch and stops the moment it changes, so Cancel
  // really cancels and a late failure cannot resurrect the dialog.
  const convertEpoch = useRef(0);
  // Prompts from one drop, shown one at a time: routing them all at
  // once would overwrite each dialog with the next.
  const placementQueue = useRef<Placement[]>([]);
  const promptOpened = useRef(false);

  const closeDialog = () => {
    convertEpoch.current += 1;
    setDialog(null);
    setBusy(false);
    setConvertError(null);
    setPendingSfz(null);
    drainPlacements();
  };

  // After any import the app says what arrived and selects it (the
  // spec's section 8): back to the voices tab, newest voice selected.
  const revealImport = (snap: Snapshot) => {
    setTab("voices");
    const voices = snap.disk?.instrument?.voices;
    const newest = voices?.[voices.length - 1];
    if (newest) setSelectedSlot(newest.slot);
  };

  // Focus must not fall to the body when a dialog closes (Q5). On the
  // voices tab that means starting again from tab stop 0 of about 247.
  // The shell mounts and unmounts Dialog.Root rather than driving
  // open, so Radix's own restore never lands. What had focus when the
  // dialog opened is remembered here and given it back.
  const focusReturn = useRef<HTMLElement | null>(null);

  /**
   * Every dialog opens through here, so the trigger is remembered
   * before Radix moves focus into the content. The body is no use to
   * hand back to, so it counts as nothing to restore.
   */
  const openDialog = (next: PendingDialog) => {
    promptOpened.current = true;
    // Only the first dialog of a chain records where focus came from:
    // a queued prompt opening from inside a closing dialog's handler
    // would otherwise capture that dialog's own button, which is gone
    // by the time focus wants restoring.
    if (dialog === null) {
      const active = document.activeElement;
      focusReturn.current =
        active instanceof HTMLElement && active !== document.body ? active : null;
    }
    setConvertError(null);
    setPendingSfz(null);
    // The conversion answers are per import: a rate or a Left picked
    // for an earlier batch must not silently apply to this one.
    setRate("18");
    setStereo("Mix");
    setDialog(next);
  };

  useEffect(() => {
    // After the dialog has gone, never before: a focus call while
    // Radix still traps focus inside the content bounces straight back.
    if (dialog !== null) return;
    const back = focusReturn.current;
    focusReturn.current = null;
    if (back?.isConnected) back.focus();
  }, [dialog]);

  // The rename field replaces the disk label button, so committing it
  // would leave focus on the body. Put it back where the rename began.
  const diskLabelRef = useRef<HTMLButtonElement>(null);
  const wasRenaming = useRef(false);
  useEffect(() => {
    if (wasRenaming.current && !renamingDisk) diskLabelRef.current?.focus();
    wasRenaming.current = renamingDisk;
  }, [renamingDisk]);

  // The tab strip is one tab stop, not three. The arrow keys move
  // along it and take the selection with them, and Home and End go to
  // the ends: the roving pattern the tablist role implies (Q5).
  const tablistRef = useRef<HTMLDivElement>(null);
  const onTabKeys = (e: ReactKeyboardEvent<HTMLButtonElement>) => {
    const here = TABS.findIndex((t) => t.id === tab);
    const step =
      e.key === "ArrowRight"
        ? 1
        : e.key === "ArrowLeft"
          ? -1
          : e.key === "Home" || e.key === "End"
            ? 0
            : null;
    if (step === null) return;
    const next =
      e.key === "Home"
        ? 0
        : e.key === "End"
          ? TABS.length - 1
          : (here + step + TABS.length) % TABS.length;
    e.preventDefault();
    const target = TABS[next];
    if (target) setTab(target.id);
  };
  // Selection carries focus with it, but only for someone already on
  // the strip: a click elsewhere that changes the tab must not steal it.
  useEffect(() => {
    const list = tablistRef.current;
    if (!list?.contains(document.activeElement)) return;
    list.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus();
  }, [tab]);

  // A deleted row takes focus with it. The row that fills its place is
  // where focus belongs, or the last row when the list got shorter.
  const sidebarRef = useRef<HTMLElement>(null);
  const [rowFocus, setRowFocus] = useState<{ name: string; index: number } | null>(null);
  const fileNames = (disk?.files ?? []).map((f) => f.name).join("\n");
  useEffect(() => {
    if (!rowFocus) return;
    // Wait for the delete to reach the snapshot: while the row is
    // still listed it would only take focus back.
    if (fileNames.split("\n").includes(rowFocus.name)) return;
    const rows = sidebarRef.current?.querySelectorAll<HTMLElement>("button.filerow");
    const neighbour = rows?.length ? rows[Math.min(rowFocus.index, rows.length - 1)] : null;
    // An empty list still has the sidebar's own buttons to land on.
    (neighbour ?? sidebarRef.current?.querySelector<HTMLElement>("button"))?.focus();
    setRowFocus(null);
  }, [rowFocus, fileNames]);

  const gestureBegin = () => {
    void core.beginGesture();
  };
  const gestureCommit = () => {
    void core.commitGesture().then((result) => {
      // A press and release with nothing in between lands no history
      // entry, so it must not raise the unsaved dot either.
      if (result.ok && result.value.gestureLanded === false) apply(result);
      else applyEdit(result);
    });
  };

  // ---- Export and save --------------------------------------------

  // The document is clean only once the bytes are actually written, so
  // the dirty flag and the success message wait for the save to land.
  // A cancel leaves the document dirty and says nothing was written; a
  // failed write says so. `then` (a guard's "Export first") runs only
  // on a real save: R25 says a failed export writes nothing, so it must
  // not proceed with the close, switch, or delete it was guarding.
  const exportImage = (then?: () => void) => {
    const diskLabel = disk?.label.trim() ?? "DISK";

    const landed = (outcomes: SaveOutcome[]) => {
      if (outcomes.includes("cancelled")) {
        say("export cancelled; the disk still holds unsaved changes");
        return;
      }
      setDirty(false);
      say(`exported ${diskLabel}`, "ok");
      then?.();
    };
    const writeFailed = (reason: unknown) => {
      fail(`save failed: ${reason instanceof Error ? reason.message : String(reason)}`);
    };

    if (disk && disk.disks === 2) {
      // R25: a split instrument exports as a named two image set.
      void core.exportImageAt(0).then((one) => {
        if (!one.ok) {
          report(one.error);
          return;
        }
        void core.exportImageAt(1).then((two) => {
          if (!two.ok) {
            report(two.error);
            return;
          }
          saveFile(one.value, `${diskLabel}-1.img`)
            .then(async (first) => {
              // A cancelled first half ends the export. Writing disk 2
              // alone would leave a half set on the user's disk, which
              // the sampler cannot load, and the export reports itself
              // cancelled either way.
              if (first === "cancelled") return [first];
              return [first, await saveFile(two.value, `${diskLabel}-2.img`)];
            })
            .then(landed, writeFailed);
        });
      });
      return;
    }
    void core.exportImage().then((result) => {
      if (!result.ok) {
        report(result.error);
        return;
      }
      saveFile(result.value, `${diskLabel}.img`).then((outcome) => {
        landed([outcome]);
      }, writeFailed);
    });
  };

  // R26's .fzf export: the instrument's own dump, stitched back into
  // one file by the core when it spans a pair. The voice rows offer
  // .fzv and .wav and the topbar offers the images. The whole
  // instrument had no route out at all.
  const exportInstrumentFile = () => {
    const dumpName = instrument?.fileName ?? "FULL-DATA-FZ";
    const target = `${disk?.label.trim() ?? "INSTRUMENT"}.fzf`;
    void core.extractFile(dumpName).then((r) => {
      if (!r.ok) {
        report(r.error);
        return;
      }
      saveFile(r.value, target).then(
        (outcome) => {
          say(
            outcome === "saved" ? `exported ${target}` : "export cancelled; nothing was written",
            outcome === "saved" ? "ok" : "status",
          );
        },
        (reason: unknown) => {
          fail(`save failed: ${reason instanceof Error ? reason.message : String(reason)}`);
        },
      );
    });
  };

  // ---- The placement matrix (R6, R7) ------------------------------

  const routeImage = (file: NamedBytes) => {
    if (disk === null || file.bytes.length !== IMAGE_SIZE) {
      void core.openImage(file.bytes).then((r) => {
        if (apply(r)) setDirty(false);
      });
      return;
    }
    if (disk.missingDisk) {
      // This route decides whether to prompt only after two core
      // calls, so the queue holds: draining now would open a second
      // dialog that the late prompt would then replace, losing the
      // files it carries. The drain resumes when this settles.
      promptOpened.current = true;
      // The open disk awaits its twin: try the pair, either order (R5).
      // The transport detaches what it transfers, so the attempt gets a
      // copy and the original survives for the fallback below.
      void core.exportImageAt(0).then((current) => {
        if (!current.ok) {
          report(current.error);
          drainPlacements();
          return;
        }
        void core.openImagePair(current.value, file.bytes.slice()).then((paired) => {
          if (paired.ok) {
            // The lone half's edits ride into the pair unexported, so
            // the document stays dirty.
            apply(paired);
            say("the pair is whole; editing both disks as one instrument");
            drainPlacements();
          } else {
            openDialog({ kind: "switchDisk", intent: { file } });
          }
        });
      });
      return;
    }
    if (dirty) {
      openDialog({ kind: "switchDisk", intent: { file } });
      return;
    }
    void core.openImage(file.bytes).then((r) => {
      if (apply(r)) setDirty(false);
    });
  };

  const routeOne = (placement: Placement) => {
    switch (placement.kind) {
      case "image":
        routeImage(placement.file);
        break;
      case "imagePair":
        if (dirty) {
          // Replacing the document discards unexported work, so it asks
          // first, exactly as a single image does.
          openDialog({ kind: "switchDisk", intent: { file: placement.a, second: placement.b } });
          break;
        }
        void core.openImagePair(placement.a.bytes, placement.b.bytes).then((r) => {
          if (apply(r)) setDirty(false);
        });
        break;
      case "fzf":
        if (instrument) {
          openDialog({
            kind: "placement",
            file: placement.file,
            ext: "fzf",
            options: ["Replace the instrument"],
            fromDisk: false,
          });
        } else {
          void core.loadFzf(placement.file.bytes).then((r) => {
            if (applyEdit(r)) say(`${placement.file.name} is now the instrument`);
          });
        }
        break;
      case "fzb":
        if (instrument) {
          openDialog({
            kind: "placement",
            file: placement.file,
            ext: "fzb",
            options: [
              instrument.banks.length >= 8
                ? "Replace bank 8"
                : `Add as bank ${String(instrument.banks.length + 1)}`,
            ],
            fromDisk: false,
          });
        } else {
          void core.addBank(placement.file.bytes, 0).then((r) => {
            if (applyEdit(r)) say(`${placement.file.name} placed in a new instrument`);
          });
        }
        break;
      case "fzv":
        void core.addVoice(placement.file.bytes).then((r) => {
          if (applyEdit(r)) say(`${placement.file.name} joined the voice list`);
        });
        break;
      case "wavs":
        openDialog({ kind: "wavImport", files: placement.files });
        break;
      case "sfz": {
        // A bare .sfz carries no audio of its own; without its WAVs
        // the conversion cannot start, so ask for the folder instead
        // of offering a convert that must fail. Several bare .sfz
        // files are refused outright: remembering one would silently
        // drop the rest, and the chooser cannot help without samples.
        // setPendingSfz comes after openDialog, which clears it.
        const wavs = placement.files.filter((f) => f.name.toLowerCase().endsWith(".wav"));
        const sfzs = placement.files.filter((f) => f.name.toLowerCase().endsWith(".sfz"));
        if (wavs.length === 0 && sfzs.length > 1) {
          fail(
            `${String(sfzs.length)} .sfz files arrived without their samples; import the instrument's folder instead`,
          );
          break;
        }
        const soleSfz = sfzs[0];
        if (wavs.length === 0 && soleSfz) {
          openDialog({ kind: "sfzFolder", name: soleSfz.name });
          setPendingSfz(soleSfz);
          break;
        }
        openDialog({
          kind: "sfzImport",
          files: placement.files,
          sfzPath: placement.sfzPath,
          hasInstrument: instrument !== null,
          // An SFZ folder carries its samples alongside the .sfz, so
          // the WAVs in the set are what the conversion reads.
          channels: batchChannels(wavs),
        });
        break;
      }
      case "unsupported":
        fail(`not an FZ input: ${placement.names.join(", ")}`);
        break;
    }
  };

  // Routes queued placements until one opens a dialog; the rest wait
  // for that dialog to close.
  const drainPlacements = () => {
    // routeOne sets the flag through openDialog, so the read has to
    // go through a call the narrowing cannot see past.
    const opened = () => promptOpened.current;
    while (placementQueue.current.length > 0) {
      promptOpened.current = false;
      const next = placementQueue.current.shift();
      if (next) routeOne(next);
      if (opened()) return;
    }
  };

  // hasDisk overrides the snapshot when a continuation runs before
  // the next snapshot lands (the new-disk-then-import chain).
  const routeNamed = (named: NamedBytes[], hasDisk = disk !== null) => {
    const placements = classifyInput(named);
    // Everything except a disk image needs a disk first (R7 row one).
    if (!hasDisk && !placements.some((p) => p.kind === "image" || p.kind === "imagePair")) {
      openDialog({ kind: "newDisk", then: named });
      return;
    }
    placementQueue.current.push(...placements);
    drainPlacements();
  };

  const placeFiles = (files: FileList | File[], paths?: string[]) => {
    const list = Array.from(files);
    void Promise.all(list.map(readBytes)).then(
      (buffers) => {
        routeNamed(
          list.map((file, i) => ({
            name: paths?.[i] ?? file.name,
            bytes: buffers[i] ?? new Uint8Array(),
          })),
        );
      },
      // One unreadable file used to discard the whole selection in
      // silence (E1: the failure shows where the user acted).
      (e: unknown) => {
        fail(`could not read the files: ${describe(e)}`);
      },
    );
  };

  // A dropped folder arrives as a directory entry, not its contents, so
  // it needs walking to behave like the folder picker (R6). The entry
  // list is only valid inside the event, so it is taken first and
  // walked after.
  const placeDrop = (transfer: DataTransfer) => {
    const entries = dropEntries(transfer);
    const flat = Array.from(transfer.files);
    if (entries.length === 0) {
      if (flat.length > 0) placeFiles(flat);
      return;
    }
    void walkEntries(entries).then(
      (dropped) => {
        if (dropped.length === 0) {
          fail("that folder holds nothing fizzle can import");
          return;
        }
        placeFiles(
          dropped.map((d) => d.file),
          dropped.map((d) => d.path),
        );
      },
      (e: unknown) => {
        fail(`could not read the drop: ${describe(e)}`);
      },
    );
  };

  // ---- Dialog actions ---------------------------------------------

  const dialogActions: DialogActions = {
    onClose: closeDialog,
    onCreateDisk: (label, then) => {
      void core.newDisk(label === "" ? "FZ DISK 1" : label).then((r) => {
        // Close either way: a refused label lands in the status bar,
        // which the overlay would otherwise cover (E1).
        const created = apply(r);
        closeDialog();
        if (created) {
          setDirty(false);
          if (then) routeNamed(then, true);
        }
      });
    },
    onConvertWavs: (files, sampleRate, channel) => {
      setConvertError(null);
      setBusy(true);
      const epoch = convertEpoch.current;
      if (files.length > 1 && !instrument) {
        // A batch with no instrument: the core's sequential kit (R8).
        void core
          .importWavFolder(toFileMap(files), sampleRate, false, channel as Channel)
          .then((result) => {
            if (convertEpoch.current !== epoch) return;
            setBusy(false);
            if (result.ok) {
              applyEdit({ ok: true, value: result.value.snapshot });
              closeDialog();
              say(`${String(files.length)} WAVs mapped up the keyboard`);
              revealImport(result.value.snapshot);
            } else {
              // The failure shows where the user acted (E1): in the
              // dialog, which stays open for another try. The status
              // bar carries an echo for after it closes.
              report(result.error);
              setConvertError(result.error.message);
            }
          });
        return;
      }
      const joinNext = (index: number, last: Snapshot | null) => {
        // The dialog closed underneath the chain: stop converting.
        if (convertEpoch.current !== epoch) return;
        const file = files[index];
        if (!file) {
          setBusy(false);
          closeDialog();
          say(
            files.length === 1
              ? "converted; the voice joined the instrument"
              : `${String(files.length)} WAVs joined the instrument`,
          );
          if (last) revealImport(last);
          return;
        }
        void core
          .importWavToInstrument(file.name, file.bytes, sampleRate, channel as Channel)
          .then((result) => {
            if (convertEpoch.current !== epoch) return;
            if (result.ok) {
              applyEdit(result);
              joinNext(index + 1, result.value);
              return;
            }
            // The failure shows in the open dialog (E1), and the files
            // not yet imported stay in it, so a retry resumes at the
            // failed file instead of importing the batch twice.
            applyEdit(result);
            setBusy(false);
            setConvertError(
              files.length === 1
                ? result.error.message
                : `file ${String(index + 1)} of ${String(files.length)} failed: ${result.error.message}`,
            );
            setDialog({ kind: "wavImport", files: files.slice(index) });
          });
      };
      joinNext(0, null);
    },
    onConvertSfz: (files, sfzPath, requested, mode, channel) => {
      setBusy(true);
      const epoch = convertEpoch.current;
      void core
        .importSfz(
          toFileMap(files),
          sfzPath,
          requested,
          mode === "fit",
          mode === "split",
          channel as "left" | "right" | "mix",
        )
        .then((result) => {
          // The dialog closed underneath the conversion: the document
          // change already happened in the core, but the UI must not
          // close a dialog the user opened since, bump the epoch under
          // a running batch, or yank the tab.
          if (convertEpoch.current !== epoch) return;
          setBusy(false);
          if (!result.ok) {
            closeDialog();
            report(result.error);
            return;
          }
          applyEdit({ ok: true, value: result.value.snapshot });
          closeDialog();
          say(
            result.value.rate < requested
              ? `converted at ${String(result.value.rate)} Hz to fit the disk`
              : "SFZ converted",
          );
          revealImport(result.value.snapshot);
        });
    },
    onPlacementChoice: (d, choice) => {
      const finish = (r: CoreResult<Snapshot>, msg: string) => {
        // Close either way: an error lands in the status bar, which
        // the dialog would otherwise hide (E1).
        const ok = applyEdit(r);
        closeDialog();
        if (ok) say(msg);
      };
      if (choice === "Export a copy") {
        void core.extractFile(d.file.name).then((r) => {
          if (!r.ok) {
            report(r.error);
            return;
          }
          closeDialog();
          // The message waits for the write, and a failed write says so.
          saveFile(r.value, d.file.name).then(
            (outcome) => {
              say(
                outcome === "saved"
                  ? `exported a copy of ${d.file.name}`
                  : "export cancelled; nothing was written",
              );
            },
            (reason: unknown) => {
              fail(`save failed: ${reason instanceof Error ? reason.message : String(reason)}`);
            },
          );
        });
        return;
      }
      const withBytes = (run: (bytes: Uint8Array) => void) => {
        if (!d.fromDisk) {
          run(d.file.bytes);
          return;
        }
        // Acting on a file already on the disk: fetch its bytes first.
        void core.extractFile(d.file.name).then((r) => {
          if (r.ok) run(r.value);
          else report(r.error);
        });
      };
      if (d.ext === "fzf") {
        withBytes((bytes) => {
          void core.loadFzf(bytes).then((r) => {
            finish(r, `${d.file.name} is now the instrument`);
          });
        });
      } else if (d.ext === "fzb") {
        const slot = instrument ? Math.min(instrument.banks.length, 7) : 0;
        withBytes((bytes) => {
          void core.addBank(bytes, slot).then((r) => {
            finish(r, `${d.file.name} added as bank ${String(slot + 1)}`);
          });
        });
      } else {
        withBytes((bytes) => {
          void core.addVoice(bytes).then((r) => {
            finish(r, `${d.file.name} joined the voice list`);
          });
        });
      }
    },
    onExtractVoice: (slot, format) => {
      void core.extractVoiceSlot(slot, format).then((r) => {
        if (!r.ok) {
          report(r.error);
          return;
        }
        const voiceName = r.value.name.trim();
        closeDialog();
        saveFile(r.value.bytes, `${voiceName}.${format}`).then(
          (outcome) => {
            say(
              outcome === "saved"
                ? `exported ${voiceName} as .${format}`
                : "export cancelled; nothing was written",
              outcome === "saved" ? "ok" : "status",
            );
          },
          (reason: unknown) => {
            fail(`save failed: ${reason instanceof Error ? reason.message : String(reason)}`);
          },
        );
      });
    },
    onDeleteFile: (name) => {
      const index = Math.max(0, disk?.files.findIndex((f) => f.name === name) ?? 0);
      void core.deleteFile(name).then((r) => {
        // Close either way (E1), as above: a refusal has nowhere to
        // show while the overlay is up.
        const deleted = applyEdit(r);
        closeDialog();
        if (deleted) {
          say(`deleted ${name}`);
          setRowFocus({ name, index });
        }
      });
    },
    onRequestDelete: (name) => {
      openDialog({
        kind: "confirmDelete",
        name,
        isInstrument: name === "FULL-DATA-FZ",
        voiceCount: instrument?.voices.length ?? 0,
      });
    },
    onExport: exportImage,
    onImportFiles: () => {
      closeDialog();
      anyRef.current?.click();
    },
    onImportFolder: () => {
      closeDialog();
      folderRef.current?.click();
    },
    onDropImport: (transfer) => {
      // The drop routes like the workspace drop; whatever prompt it
      // needs replaces the import dialog.
      placeDrop(transfer);
    },
    onPickSfzFolder: () => {
      // Not closeDialog: that would clear the pending .sfz the picked
      // folder is about to join.
      setDialog(null);
      setBusy(false);
      setConvertError(null);
      folderRef.current?.click();
    },
    onCloseDisk: () => {
      void core.closeDisk().then((r) => {
        const closed = apply(r);
        closeDialog();
        if (closed) setDirty(false);
      });
    },
    onSwitchTo: (file, second) => {
      const opened = second
        ? core.openImagePair(file.bytes, second.bytes)
        : core.openImage(file.bytes);
      void opened.then((r) => {
        const switched = apply(r);
        closeDialog();
        if (switched) setDirty(false);
      });
    },
  };

  const requestClose = () => {
    if (dirty) openDialog({ kind: "switchDisk", intent: "close" });
    else void core.closeDisk().then(apply);
  };

  // ---- Frame -------------------------------------------------------

  // A core that has stopped can't answer for the document, so the
  // frame around it would be a frame around nothing (E5). The message
  // is the core's own plain sentence; the reason folds away.
  if (fatal) {
    return (
      <CrashPanel
        label="core failure"
        title="The core stopped"
        message={fatal.message}
        detail={fatal.detail}
      >
        <button
          onClick={() => {
            window.location.reload();
          }}
        >
          Reload
        </button>
      </CrashPanel>
    );
  }

  return (
    <div
      className="app"
      onDragOver={(e) => {
        if (disk) e.preventDefault();
      }}
      onDrop={(e) => {
        if (!disk) return;
        e.preventDefault();
        placeDrop(e.dataTransfer);
      }}
    >
      <header className="topbar">
        <span className="brand">fizzle</span>

        {disk && (
          <>
            <span className="disklabel">
              {renamingDisk ? (
                <input
                  // eslint-disable-next-line jsx-a11y/no-autofocus -- rename begins typing immediately, the mockup's validated flow
                  autoFocus
                  defaultValue={disk.label}
                  aria-label="disk label"
                  name="disk-label"
                  maxLength={12}
                  onBlur={(e) => {
                    const label = e.target.value.toUpperCase().slice(0, 12);
                    if (label !== disk.label && label !== "") {
                      void core.renameDisk(label).then(applyEdit);
                    }
                    setRenamingDisk(false);
                  }}
                  onKeyDown={(e) => {
                    if (e.key !== "Enter") return;
                    // Committing hands focus back to the button that
                    // opened this field, and it does so during the
                    // keydown. Enter's default action would then land
                    // on that button and reopen the rename, so the key
                    // has to stop here.
                    e.preventDefault();
                    (e.target as HTMLInputElement).blur();
                  }}
                />
              ) : (
                <button
                  ref={diskLabelRef}
                  className="disklabelbutton"
                  aria-label={`disk ${disk.label.trim()}, rename`}
                  title="Rename the disk"
                  onClick={() => {
                    setRenamingDisk(true);
                  }}
                >
                  [{disk.label}]
                </button>
              )}
            </span>
            <CapacityBar usedBytes={disk.usedBytes} disks={disk.disks} alarm={barError !== null} />
          </>
        )}

        <span className="spacer" />

        {disk && (
          <button
            className="btn"
            onClick={() => {
              openDialog({ kind: "import" });
            }}
          >
            Import
          </button>
        )}

        <button className="btn" onClick={undo} disabled={!snap?.canUndo}>
          Undo
        </button>
        <button className="btn" onClick={redo} disabled={!snap?.canRedo}>
          Redo
        </button>

        {disk && (
          <>
            {dirty && (
              <span title="Unexported changes" className="dirtydot">
                ●
              </span>
            )}
            <button
              className="btn primary"
              onClick={() => {
                exportImage();
              }}
            >
              Export
            </button>
            <button className="btn" onClick={requestClose}>
              Eject
            </button>
          </>
        )}
      </header>

      <input
        ref={anyRef}
        type="file"
        accept=".img,.fzf,.fzb,.fzv,.wav,.sfz"
        multiple
        aria-label="fz files"
        hidden
        onChange={(e) => {
          if (e.target.files?.length) placeFiles(e.target.files);
          e.target.value = "";
        }}
      />
      <input
        ref={folderRef}
        type="file"
        aria-label="folder"
        hidden
        {...{ webkitdirectory: "" }}
        onChange={(e) => {
          const files = e.target.files;
          if (files?.length) {
            const list = Array.from(files);
            const sfz = pendingSfz;
            setPendingSfz(null);
            if (sfz && !list.some((f) => f.name.toLowerCase().endsWith(".sfz"))) {
              // The picked folder holds the samples alone: keep its
              // name in every path so the .sfz's references resolve,
              // and put the remembered .sfz beside it at the root.
              void Promise.all(list.map(readBytes)).then(
                (buffers) => {
                  routeNamed([
                    { name: sfz.name, bytes: sfz.bytes },
                    ...list.map((f, i) => ({
                      name: f.webkitRelativePath === "" ? f.name : f.webkitRelativePath,
                      bytes: buffers[i] ?? new Uint8Array(),
                    })),
                  ]);
                },
                // One unreadable file must not discard the selection in
                // silence (E1: the failure shows where the user acted).
                (e: unknown) => {
                  fail(`could not read the files: ${describe(e)}`);
                },
              );
            } else {
              // An instrument folder (the .sfz inside it) or an
              // ordinary folder: the normal stripped route resolves it.
              placeFiles(
                files,
                list.map((f) => relativePath(f)),
              );
            }
          }
          e.target.value = "";
        }}
      />

      {browserNotice && (
        <div className="browsernotice" role="alert" aria-label="unsupported browser">
          <span>
            fizzle is built for desktop Chromium. This browser lacks parts it relies on; saving
            falls back to downloads and some features may fail.
          </span>
          <button
            className="btn small"
            onClick={() => {
              setBrowserNotice(false);
            }}
          >
            Dismiss
          </button>
        </div>
      )}

      <div className="main">
        {!disk ? (
          <StartScreen
            onNewDisk={() => {
              openDialog({ kind: "newDisk" });
            }}
            onBrowse={() => anyRef.current?.click()}
            onDropFiles={placeDrop}
          />
        ) : (
          <ErrorBoundary onExport={exportImage}>
            <nav
              ref={sidebarRef}
              className={filesCollapsed ? "sidebar collapsed" : "sidebar"}
              aria-label="disk files"
            >
              {filesCollapsed ? (
                <button
                  className="raillabel"
                  onClick={() => {
                    setFilesCollapsed(false);
                  }}
                >
                  Files ({disk.files.length})
                </button>
              ) : (
                <>
                  <h2>Disk files</h2>
                  {disk.missingDisk ? (
                    <div className="missingdisk" role="alert" aria-label="missing disk">
                      <span>
                        This image is disk {disk.missingDisk === 2 ? 1 : 2} of a two disk
                        instrument. Open disk {disk.missingDisk} to edit the whole instrument.
                      </span>
                      <button
                        className="btn small"
                        onClick={() => {
                          twinRef.current?.click();
                        }}
                      >
                        Open disk {disk.missingDisk}
                      </button>
                      <input
                        ref={twinRef}
                        type="file"
                        accept=".img"
                        aria-label="second disk file"
                        hidden
                        onChange={(e) => {
                          if (e.target.files?.length) placeFiles(e.target.files);
                          e.target.value = "";
                        }}
                      />
                    </div>
                  ) : null}
                  <div className="filelist">
                    {disk.files.map((f) => (
                      <button
                        key={`${f.name}-${f.type}`}
                        className={f.type === "full" && instrument ? "filerow selected" : "filerow"}
                        // Which file is open was a step of hue and
                        // nothing else, and nothing at all to a reader
                        // (Q5). The stylesheet adds weight and a rule;
                        // this says the same thing to the tree.
                        aria-current={f.type === "full" && instrument ? "true" : undefined}
                        onClick={() => {
                          if (f.type === "full") {
                            setTab("voices");
                          } else {
                            openDialog({
                              kind: "placement",
                              file: { name: f.name, bytes: new Uint8Array() },
                              ext: f.type === "bank" ? "fzb" : "fzv",
                              options:
                                f.type === "bank"
                                  ? [
                                      (instrument?.banks.length ?? 0) >= 8
                                        ? "Replace bank 8"
                                        : `Add as bank ${String((instrument?.banks.length ?? 0) + 1)}`,
                                      "Export a copy",
                                      "Delete file",
                                    ]
                                  : ["Join voice list", "Export a copy", "Delete file"],
                              fromDisk: true,
                            });
                          }
                        }}
                        onContextMenu={(e) => {
                          e.preventDefault();
                          dialogActions.onRequestDelete(f.name);
                        }}
                        onKeyDown={(e) => {
                          // Delete is the keyboard's route to the same
                          // confirmation the context menu opens. Enter
                          // is already the row's own action, and a
                          // right click is a pointer gesture (Q5).
                          if (e.key === "Delete" || e.key === "Backspace") {
                            e.preventDefault();
                            dialogActions.onRequestDelete(f.name);
                          }
                        }}
                        title={
                          f.type === "full"
                            ? "Click to open for editing · Delete key or right click to remove"
                            : "Click for actions · Delete key or right click to remove"
                        }
                      >
                        <span className={`ftype ftype-${f.type}`}>{f.type}</span>
                        <span className="fname">
                          {f.type === "full" && instrument ? (
                            <>
                              {disk.label.trim()}
                              <small>{f.name}</small>
                            </>
                          ) : (
                            f.name
                          )}
                        </span>
                        <span className="fsize">{formatBytes(f.sizeBytes)}</span>
                        <span className="fglyph">{f.type === "full" ? "▸" : "⋯"}</span>
                      </button>
                    ))}
                  </div>
                  {disk.files.length > 0 && (
                    <p className="tablehint" style={{ padding: "0 10px" }}>
                      Enter acts on a file. Delete removes it.
                    </p>
                  )}
                  <div style={{ padding: 8 }}>
                    {instrument ? (
                      <button
                        className="btn small"
                        onClick={exportInstrumentFile}
                        title="Save the instrument's full dump as an .fzf file"
                      >
                        Export instrument (.fzf)
                      </button>
                    ) : (
                      <button
                        className="btn small"
                        onClick={() => {
                          void core.newInstrument(disk.label.trim()).then(applyEdit);
                        }}
                      >
                        New empty instrument
                      </button>
                    )}
                  </div>
                  <div className="filecount">
                    {disk.files.length} files · {instrument?.voices.length ?? 0} voices
                  </div>
                </>
              )}
              <button
                className="btn small ghost railtoggle"
                onClick={() => {
                  setFilesCollapsed(!filesCollapsed);
                }}
                aria-label={filesCollapsed ? "expand file list" : "collapse file list"}
              >
                {filesCollapsed ? "»" : "« Collapse"}
              </button>
            </nav>

            <div className="content">
              <div className="tabsbar">
                <div
                  ref={tablistRef}
                  className="tabs"
                  role="tablist"
                  aria-label="instrument sections"
                >
                  {TABS.map((t) => (
                    <button
                      key={t.id}
                      id={`tab-${t.id}`}
                      role="tab"
                      onKeyDown={onTabKeys}
                      aria-selected={tab === t.id}
                      // Only the panel that is mounted can be pointed
                      // at, so the attribute rides on the selected tab.
                      {...(tab === t.id ? { "aria-controls": TABPANEL_ID } : {})}
                      // One tab stop for the strip; the arrow keys move
                      // within it (Q5).
                      tabIndex={tab === t.id ? 0 : -1}
                      className={tab === t.id ? "tab active" : "tab"}
                      onClick={() => {
                        setTab(t.id);
                      }}
                    >
                      {t.label}
                    </button>
                  ))}
                </div>
                {/* Outside the tablist: the role permits tabs alone. */}
                {instrument && <span className="tabnote">Instrument: {disk.label.trim()}</span>}
              </div>
              <div
                className="tabbody"
                id={TABPANEL_ID}
                role="tabpanel"
                aria-labelledby={`tab-${tab}`}
              >
                {!instrument ? (
                  <NoInstrumentPanel
                    onNewInstrument={() => {
                      void core.newInstrument(disk.label.trim()).then(applyEdit);
                    }}
                  />
                ) : (
                  <>
                    {tab === "voices" && (
                      <VoiceEditor
                        instrument={instrument}
                        schema={schema}
                        selectedSlot={selectedSlot}
                        selectedLoop={selectedLoop}
                        peaks={peaks}
                        onSelectVoice={setSelectedSlot}
                        onSelectLoop={setSelectedLoop}
                        onSetParamNumber={(slot, field, value) => {
                          void core.setSlotParamNumber(slot, field, value).then(applyEdit);
                        }}
                        onSetParamOption={(slot, field, option) => {
                          void core.setSlotParamOption(slot, field, option).then(applyEdit);
                        }}
                        onSetLoop={(slot, index, start, end) => {
                          void core.setSlotLoop(slot, index, start, end).then(applyEdit);
                        }}
                        onSetGeneration={(slot, start, end) => {
                          void core.setSlotGeneration(slot, start, end).then(applyEdit);
                        }}
                        onSetLoopAttr={(slot, index, xf, tm) => {
                          void core.setSlotLoopAttr(slot, index, xf, tm).then(applyEdit);
                        }}
                        onSetLoopSelect={(slot, sustain, release) => {
                          void core.setSlotLoopSelect(slot, sustain, release).then(applyEdit);
                        }}
                        onSetEnvelope={(slot, which, sustain, end, rates, stops) => {
                          void core
                            .setSlotEnvelope(slot, which, sustain, end, rates, stops)
                            .then(applyEdit);
                        }}
                        onRename={(slot, name) => {
                          if (name !== "") void core.renameVoiceSlot(slot, name).then(applyEdit);
                        }}
                        onMapVoice={(slot) => {
                          void core.mapVoice(slot).then(applyEdit);
                        }}
                        onExtract={(slot, name) => {
                          openDialog({ kind: "extract", slot, name });
                        }}
                        onGestureBegin={gestureBegin}
                        onGestureCommit={gestureCommit}
                      />
                    )}
                    {tab === "banks" && (
                      <BanksAreas
                        instrument={instrument}
                        selectedBank={selectedBank}
                        selectedArea={selectedArea}
                        onSelectBank={(b) => {
                          setSelectedBank(b);
                          setSelectedArea(null);
                        }}
                        onSelectArea={setSelectedArea}
                        onRenameBank={(bankIdx, name) => {
                          if (name !== "") void core.renameBank(bankIdx, name).then(applyEdit);
                        }}
                        onSetAreaField={(bankIdx, areaIdx, field, value) => {
                          void core.setAreaField(bankIdx, areaIdx, field, value).then(applyEdit);
                        }}
                        onAddArea={(bankIdx) => {
                          void core
                            .addArea(bankIdx, instrument.voices[0]?.slot ?? 0)
                            .then(applyEdit);
                        }}
                        onDuplicateArea={(bankIdx, areaIdx) => {
                          void core.duplicateArea(bankIdx, areaIdx).then(applyEdit);
                        }}
                        onDeleteArea={(bankIdx, areaIdx) => {
                          void core.deleteArea(bankIdx, areaIdx).then(applyEdit);
                          setSelectedArea(null);
                        }}
                        onSwapAreas={(bankIdx, a, b) => {
                          void core.swapAreas(bankIdx, a, b).then(applyEdit);
                        }}
                        onGestureBegin={gestureBegin}
                        onGestureCommit={gestureCommit}
                      />
                    )}
                    {tab === "effects" && instrument.effects && (
                      <EffectsScreen
                        effects={instrument.effects}
                        onSetCell={(controller, target, value) => {
                          void core.setEffectCell(controller, target, value).then(applyEdit);
                        }}
                        onSetBend={(value) => {
                          void core.setBendRange(value).then(applyEdit);
                        }}
                        onGestureBegin={gestureBegin}
                        onGestureCommit={gestureCommit}
                      />
                    )}
                  </>
                )}
              </div>

              {instrument && focusVoice && (
                <div className="keyboardbar" data-auditioning={auditioning || undefined}>
                  <div className="field">
                    <button
                      className="btn small"
                      // The name has to contain the words on the
                      // button, or speech input can't ask for it by
                      // what it reads (WCAG 2.5.3).
                      aria-label="- oct, octave down"
                      disabled={kbLow <= 0}
                      onClick={() => {
                        setKbLow(clamp(kbLow - 12, 0, KEYBOARD_LOW_MAX));
                      }}
                    >
                      - oct
                    </button>
                    <button
                      className="btn small"
                      aria-label="+ oct, octave up"
                      disabled={kbLow >= KEYBOARD_LOW_MAX}
                      onClick={() => {
                        setKbLow(clamp(kbLow + 12, 0, KEYBOARD_LOW_MAX));
                      }}
                    >
                      + oct
                    </button>
                    <span className="kblabel">{noteName(kbLow)} up</span>
                    <span className="kblabel previewnote">preview: DCA applied, approximate</span>
                  </div>
                  <Keyboard
                    lowNote={kbLow}
                    octaves={KEYBOARD_OCTAVES}
                    highlight={highlight}
                    rootKey={focusRoot}
                    onNoteOn={noteOn}
                    onNoteOff={noteOff}
                  />
                </div>
              )}
            </div>
          </ErrorBoundary>
        )}
      </div>

      <footer className="statusbar">
        {barError && (
          <span key={`e${String(barError.seq)}`} className="barmsg error" role="alert">
            {barError.text}
            <button
              className="bardismiss"
              aria-label="dismiss error"
              onClick={() => {
                setBarError(null);
              }}
            >
              dismiss
            </button>
          </span>
        )}
        {barMsg && (
          <span key={`m${String(barMsg.seq)}`} className={`barmsg ${barMsg.kind}`} role="status">
            {barMsg.text}
          </span>
        )}
        {!barError && !barMsg && <span className="barmsg">revision {snap?.revision ?? 0}</span>}
      </footer>

      {dialog && (
        <Dialogs
          dialog={dialog}
          dirty={dirty}
          actions={dialogActions}
          busy={busy}
          rate={rate}
          onRateChange={setRate}
          stereo={stereo}
          onStereoChange={setStereo}
          estimate={estimate}
          estimateError={estimateError}
          convertError={convertError}
        />
      )}
    </div>
  );
}

// Folder pickers stamp each file with its path under the chosen root;
// fall back to the bare name where the field is empty (drag and drop,
// jsdom).
function relativePath(file: File): string {
  const path = file.webkitRelativePath;
  if (path === "") return file.name;
  const slash = path.indexOf("/");
  return slash >= 0 ? path.slice(slash + 1) : path;
}

// FileReader rather than File.arrayBuffer: identical result, and it
// exists in every environment the tests run in.
function readBytes(file: File): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      resolve(new Uint8Array(reader.result as ArrayBuffer));
    };
    reader.onerror = () => {
      reject(reader.error instanceof Error ? reader.error : new Error("file read failed"));
    };
    reader.readAsArrayBuffer(file);
  });
}

/** What a save attempt did, so the caller can tell the truth about it. */
export type SaveOutcome = "saved" | "cancelled";

/**
 * Saves through the platform picker where available, an anchor
 * download otherwise. Resolves only once the bytes are written, so the
 * caller can clear the dirty flag on the strength of a real write. A
 * user cancel resolves "cancelled" (nothing was written, and nothing
 * failed); a write that fails rejects, so the caller can say so. A
 * picker that never opens (headless, revoked permission) falls back to
 * the download rather than pretending the user cancelled.
 */
function saveFile(bytes: Uint8Array, name: string): Promise<SaveOutcome> {
  const picker = (
    window as {
      showSaveFilePicker?: (options: { suggestedName: string }) => Promise<{
        createWritable(): Promise<{ write(data: Blob): Promise<void>; close(): Promise<void> }>;
      }>;
    }
  ).showSaveFilePicker;
  const blob = new Blob([bytes.buffer as ArrayBuffer], { type: "application/octet-stream" });

  const download = (): SaveOutcome => {
    // jsdom has neither picker nor object URLs; the browser smoke
    // covers real saves, and the flow around a save must not wedge.
    try {
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = name;
      anchor.click();
      URL.revokeObjectURL(url);
    } catch {
      /* no download surface in this environment */
    }
    return "saved";
  };

  if (!picker) return Promise.resolve(download());

  const started = performance.now();
  return picker({ suggestedName: name }).then(
    // A failure inside here (a full or read-only volume) rejects the
    // returned promise, so the caller reports it rather than claiming
    // the export succeeded.
    async (handle) => {
      const writable = await handle.createWritable();
      await writable.write(blob);
      await writable.close();
      return "saved" as const;
    },
    (reason: unknown): SaveOutcome => {
      // A user cancel and a picker that never opened (headless
      // browsers) both reject with AbortError. A dialog a person
      // dismissed existed for hundreds of milliseconds; an instant
      // rejection means no dialog, so fall back to a download.
      const cancelled =
        reason instanceof DOMException &&
        reason.name === "AbortError" &&
        performance.now() - started > 250;
      if (cancelled) return "cancelled";
      return download();
    },
  );
}
