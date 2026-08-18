// The mockup's dialogs over the real core: every prompt the placement
// matrix (R7) and the destructive flows require. Pure presentation;
// the shell owns the pending state and the core calls.
import * as Dialog from "@radix-ui/react-dialog";
import * as RadioGroup from "@radix-ui/react-radio-group";
import { useState } from "react";
import type { ImportEstimate, SampleRate } from "../boundary/contract";
import type { NamedBytes } from "../viewstate/place";
import { sfzCandidates } from "../viewstate/place";
import { formatBytes } from "../ui/format";

export type PendingDialog =
  | { kind: "newDisk"; then?: NamedBytes[] }
  | { kind: "wavImport"; files: NamedBytes[] }
  | {
      kind: "sfzImport";
      files: NamedBytes[];
      sfzPath: string;
      hasInstrument: boolean;
      /** Channels across the referenced WAVs; null when unreadable. */
      channels: number | null;
    }
  | {
      kind: "placement";
      file: NamedBytes;
      ext: "fzf" | "fzb" | "fzv";
      options: string[];
      /** Acting on a file already on the disk rather than a drop. */
      fromDisk: boolean;
    }
  | { kind: "sfzFolder"; name: string }
  | { kind: "extract"; slot: number; name: string }
  | { kind: "switchDisk"; intent: "close" | { file: NamedBytes; second?: NamedBytes } }
  | { kind: "confirmDelete"; name: string; isInstrument: boolean; voiceCount: number };

export interface DialogActions {
  onClose: () => void;
  onCreateDisk: (label: string, then?: NamedBytes[]) => void;
  onConvertWavs: (files: NamedBytes[], rate: SampleRate, channel: string) => void;
  onConvertSfz: (
    files: NamedBytes[],
    sfzPath: string,
    rate: SampleRate,
    mode: "fit" | "split",
    channel: string,
  ) => void;
  onPlacementChoice: (
    dialog: Extract<PendingDialog, { kind: "placement" }>,
    choice: string,
  ) => void;
  onExtractVoice: (slot: number, format: "fzv" | "wav") => void;
  onDeleteFile: (name: string) => void;
  onRequestDelete: (name: string) => void;
  /**
   * Export the document. `then` runs once the export completes, so a
   * guard's "Export first" carries out the action it was guarding
   * instead of leaving the user in a dialog that no longer applies.
   */
  onExport: (then?: () => void) => void;
  onCloseDisk: () => void;
  /** The lone .sfz flow: open the folder picker for its samples. */
  onPickSfzFolder: () => void;
  /** second is set when a two image pair replaces the document. */
  onSwitchTo: (file: NamedBytes, second?: NamedBytes) => void;
}

/**
 * The stereo answer: which side of a stereo file the FZ keeps. Shown
 * only when the input holds one, since a mono batch has nothing to
 * decide. Both folder imports ask it, so both ask it the same way.
 */
function StereoRow({
  channels,
  value,
  onValueChange,
}: {
  /** Channels across the batch; 1 means every file is mono. */
  channels: number | null;
  value: string;
  onValueChange: (value: string) => void;
}) {
  if (channels === 1) return null;
  return (
    <div className="row">
      <span className="radiolabel">stereo</span>
      <RadioGroup.Root
        className="row"
        value={value}
        onValueChange={onValueChange}
        aria-label="stereo handling"
        name="stereo"
      >
        {["Left", "Right", "Mix"].map((s) => (
          <label key={s} className="radio-item">
            <RadioGroup.Item className="radio-dot" value={s} />
            {s}
          </label>
        ))}
      </RadioGroup.Root>
    </div>
  );
}

/**
 * The refusal's way out: the rates the whole batch would land at,
 * phrased for the sentence's tail.
 */
function fitsTail(rates: number[]): string {
  if (rates.length === 0) return " No rate fits.";
  const labels = rates.map((r) => String(r / 1000));
  return labels.length === 1 ? ` ${labels[0]} kHz fits.` : ` ${labels.join(" or ")} kHz fit.`;
}

/**
 * The size line, from the core's estimate alone: what the batch
 * becomes, the room left at this rate, and, when the import cannot
 * land, which constraint bit and which rate is the way out.
 */
function estimateCopy(est: ImportEstimate, rateKHz: string, count: number): string {
  const s = (n: number) => n.toFixed(1);
  if (est.verdict === "wont-fit" && est.reason === "sample-memory") {
    const subject = count === 1 ? "This sample" : `"${est.overCapFile}"`;
    return (
      `${subject} is ${s(est.fileSeconds)} s, more than the sampler's memory can load ` +
      `at ${rateKHz} kHz (${s(est.capSeconds)} s max).${fitsTail(est.fitsAtRates)}`
    );
  }
  if (est.verdict === "wont-fit" && est.reason === "voice-limit") {
    return "This import needs more voices than the 64 an instrument holds.";
  }
  if (est.verdict === "wont-fit") {
    return `Not enough room on the disk at ${rateKHz} kHz.${fitsTail(est.fitsAtRates)}`;
  }
  const size = `Becomes about ${formatBytes(est.bytes)} (${s(est.seconds)} s)`;
  if (est.verdict === "splits") {
    return `${size}; spreads the instrument across two disks. Export both images.`;
  }
  return `${size}; room for about ${s(est.roomSeconds)} s more at ${rateKHz} kHz.`;
}

export function Dialogs({
  dialog,
  dirty,
  actions,
  busy = false,
  rate,
  onRateChange,
  stereo,
  onStereoChange,
  estimate = null,
  estimateError = null,
  convertError = null,
}: {
  dialog: PendingDialog;
  dirty: boolean;
  actions: DialogActions;
  /** A conversion is running; its button shows progress. */
  busy?: boolean;
  /** The rate answer, held by the shell so the estimate can react. */
  rate: string;
  onRateChange: (rate: string) => void;
  /** The stereo answer, held by the shell for the same reason. */
  stereo: string;
  onStereoChange: (stereo: string) => void;
  /** The core's answer for the WAV dialog's current files and rate. */
  estimate?: ImportEstimate | null;
  /** The estimate's refusal, when the files would not even parse. */
  estimateError?: string | null;
  /** A conversion failure, shown where the user acted (E1). */
  convertError?: string | null;
}) {
  const [label, setLabel] = useState("FZ DISK 1");

  const d = dialog;
  const close = actions.onClose;

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog" aria-describedby={undefined}>
          {d.kind === "newDisk" && (
            <>
              <Dialog.Title asChild>
                <h3>New disk</h3>
              </Dialog.Title>
              <p className="desc">
                {d.then
                  ? "No disk is open, so the import gets one first. Just pick a label."
                  : "A blank 1.25 MB FZ disk."}
              </p>
              <div className="row">
                <input
                  type="text"
                  aria-label="disk label"
                  name="disk-label"
                  id="disk-label"
                  value={label}
                  maxLength={12}
                  onChange={(e) => {
                    setLabel(e.target.value.toUpperCase());
                  }}
                />
              </div>
              <div className="buttons">
                <button className="btn" onClick={close}>
                  Cancel
                </button>
                <button
                  className="btn solid"
                  onClick={() => {
                    actions.onCreateDisk(label, d.then);
                  }}
                >
                  Create
                </button>
              </div>
            </>
          )}

          {d.kind === "wavImport" && (
            <>
              <Dialog.Title asChild>
                <h3>
                  Import {d.files.length} WAV{d.files.length === 1 ? "" : "s"}
                </h3>
              </Dialog.Title>
              <p className="desc">
                One answer covers the whole batch. Nothing is silently truncated.
              </p>
              <div className="row">
                <span className="radiolabel">sample rate</span>
                <RadioGroup.Root
                  className="row"
                  value={rate}
                  onValueChange={onRateChange}
                  aria-label="sample rate"
                  name="sample-rate"
                >
                  {["36", "18", "9"].map((r) => (
                    <label key={r} className="radio-item">
                      <RadioGroup.Item className="radio-dot" value={r} />
                      {r} kHz
                    </label>
                  ))}
                </RadioGroup.Root>
              </div>
              <StereoRow
                channels={estimate?.anyStereo ? 2 : 1}
                value={stereo}
                onValueChange={onStereoChange}
              />
              {estimate ? (
                <p className={estimate.verdict === "wont-fit" ? "desc dangertext" : "desc"}>
                  {estimateCopy(estimate, rate, d.files.length)}
                </p>
              ) : (
                estimateError !== null && <p className="desc">{estimateError}</p>
              )}
              {convertError !== null && (
                <p className="desc dangertext" role="alert">
                  {convertError}
                </p>
              )}
              <div className="buttons">
                <button className="btn" onClick={close}>
                  Cancel
                </button>
                <button
                  className="btn solid"
                  disabled={busy || estimate?.verdict === "wont-fit"}
                  aria-busy={busy || undefined}
                  onClick={() => {
                    actions.onConvertWavs(
                      d.files,
                      (Number(rate) * 1000) as SampleRate,
                      stereo.toLowerCase(),
                    );
                  }}
                >
                  {busy ? "Converting" : "Convert"}
                </button>
              </div>
            </>
          )}

          {d.kind === "sfzImport" && (
            <SfzBody
              dialog={d}
              dirty={dirty}
              actions={actions}
              busy={busy}
              rate={rate}
              setRate={onRateChange}
              stereo={stereo}
              setStereo={onStereoChange}
            />
          )}

          {d.kind === "sfzFolder" && (
            <>
              <Dialog.Title asChild>
                <h3>This SFZ needs its samples</h3>
              </Dialog.Title>
              <p className="desc">
                &quot;{d.name}&quot; lists samples fizzle cannot see yet. Pick the
                instrument&apos;s folder, or its samples folder.
              </p>
              <div className="buttons">
                <button className="btn" onClick={close}>
                  Cancel
                </button>
                <button className="btn solid" onClick={actions.onPickSfzFolder}>
                  Pick folder
                </button>
              </div>
            </>
          )}

          {d.kind === "placement" && <PlacementBody dialog={d} dirty={dirty} actions={actions} />}

          {d.kind === "extract" && (
            <>
              <Dialog.Title asChild>
                <h3>Export voice</h3>
              </Dialog.Title>
              <p className="desc">&quot;{d.name}&quot; saves through the platform dialog.</p>
              <div className="buttons" style={{ justifyContent: "flex-start" }}>
                <button
                  className="btn primary"
                  onClick={() => {
                    actions.onExtractVoice(d.slot, "wav");
                  }}
                >
                  As .wav
                </button>
                <button
                  className="btn primary"
                  onClick={() => {
                    actions.onExtractVoice(d.slot, "fzv");
                  }}
                >
                  As .fzv
                </button>
                <button className="btn" onClick={close}>
                  Cancel
                </button>
              </div>
            </>
          )}

          {d.kind === "switchDisk" && (
            <>
              <Dialog.Title asChild>
                <h3>Unexported changes</h3>
              </Dialog.Title>
              <p className="desc">
                {d.intent === "close"
                  ? "Ejecting this disk set discards edits that haven't been exported."
                  : "Opening a different disk set discards edits that haven't been exported."}
              </p>
              <div className="buttons">
                <button
                  className="btn danger"
                  style={{ marginRight: "auto" }}
                  onClick={() => {
                    if (d.intent === "close") actions.onCloseDisk();
                    else actions.onSwitchTo(d.intent.file, d.intent.second);
                  }}
                >
                  Discard
                </button>
                <button className="btn" onClick={close}>
                  Keep working
                </button>
                <button
                  className="btn solid"
                  onClick={() => {
                    actions.onExport(() => {
                      if (d.intent === "close") actions.onCloseDisk();
                      else actions.onSwitchTo(d.intent.file, d.intent.second);
                    });
                  }}
                >
                  Export first
                </button>
              </div>
            </>
          )}

          {d.kind === "confirmDelete" && (
            <>
              <Dialog.Title asChild>
                <h3>{d.isInstrument ? "Delete the instrument?" : "Delete file"}</h3>
              </Dialog.Title>
              <p className="desc">
                {d.isInstrument
                  ? `This removes the instrument and all ${String(d.voiceCount)} of its voices from the disk (the ${d.name} file).`
                  : `Delete "${d.name}" from the disk? Destructive actions confirm first.`}
              </p>
              {d.isInstrument && dirty && (
                <p className="desc dangertext">
                  Unexported changes will be lost. Export first to keep them.
                </p>
              )}
              <div className="buttons">
                <button
                  className="btn danger"
                  style={{ marginRight: "auto" }}
                  onClick={() => {
                    actions.onDeleteFile(d.name);
                  }}
                >
                  Delete
                </button>
                <button className="btn" onClick={close}>
                  Cancel
                </button>
                {d.isInstrument && dirty && (
                  <button
                    className="btn solid"
                    onClick={() => {
                      actions.onExport(() => {
                        actions.onDeleteFile(d.name);
                      });
                    }}
                  >
                    Export first
                  </button>
                )}
              </div>
            </>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/**
 * chosenSFZ resolves which .sfz the conversion runs on. The user's
 * pick wins, then the classifier's path, which is set when the folder
 * holds one .sfz. Otherwise it is the first candidate, which the
 * chooser shows selected.
 */
function chosenSFZ(choices: string[], classified: string, pick: string): string {
  if (choices.includes(pick)) return pick;
  if (classified !== "") return classified;
  return choices[0] ?? "";
}

/**
 * The SFZ conversion dialog (R6, R9): the sample rate, the stereo
 * answer, and which .sfz the folder's instrument is. A DAW export
 * folder can hold several, and the core refuses to guess between
 * them. That question belongs beside the other two, rather than
 * arriving as a refusal the user can't answer.
 */
function SfzBody({
  dialog,
  dirty,
  actions,
  busy,
  rate,
  setRate,
  stereo,
  setStereo,
}: {
  dialog: Extract<PendingDialog, { kind: "sfzImport" }>;
  dirty: boolean;
  actions: DialogActions;
  busy: boolean;
  rate: string;
  setRate: (value: string) => void;
  stereo: string;
  setStereo: (value: string) => void;
}) {
  const [pick, setPick] = useState("");
  const choices = sfzCandidates(dialog.files);
  const chosen = chosenSFZ(choices, dialog.sfzPath, pick);
  const convert = (mode: "fit" | "split") => {
    actions.onConvertSfz(
      dialog.files,
      chosen,
      (Number(rate) * 1000) as SampleRate,
      mode,
      stereo.toLowerCase(),
    );
  };
  return (
    <>
      <Dialog.Title asChild>
        <h3>SFZ conversion</h3>
      </Dialog.Title>
      <p className="desc">
        &quot;{chosen === "" ? "SFZ instrument" : chosen}&quot; converts with fizzle&apos;s engine.
        Choose the rate and how it should fit the disk set.
        {dialog.hasInstrument && " This replaces the current instrument."}
      </p>
      {dialog.hasInstrument && dirty && (
        <p className="desc dangertext">
          Unexported changes will be lost. Export first to keep them.
        </p>
      )}
      {choices.length > 1 && (
        <>
          <p className="desc">
            This folder holds {choices.length} .sfz files, so pick the instrument to convert.
          </p>
          <div className="row">
            <span className="radiolabel">instrument</span>
            <RadioGroup.Root
              className="row"
              value={chosen}
              onValueChange={setPick}
              aria-label="which .sfz"
              name="which-sfz"
            >
              {choices.map((name) => (
                <label key={name} className="radio-item">
                  <RadioGroup.Item className="radio-dot" value={name} />
                  {name}
                </label>
              ))}
            </RadioGroup.Root>
          </div>
        </>
      )}
      <div className="row">
        <span className="radiolabel">sample rate</span>
        <RadioGroup.Root
          className="row"
          value={rate}
          onValueChange={setRate}
          aria-label="target rate"
          name="target-rate"
        >
          {["36", "18", "9"].map((r) => (
            <label key={r} className="radio-item">
              <RadioGroup.Item className="radio-dot" value={r} />
              {r} kHz
            </label>
          ))}
        </RadioGroup.Root>
      </div>
      <StereoRow channels={dialog.channels} value={stereo} onValueChange={setStereo} />
      <div className="buttons" style={{ justifyContent: "flex-start", flexWrap: "wrap" }}>
        <button
          className="btn primary"
          disabled={busy}
          aria-busy={busy || undefined}
          onClick={() => {
            convert("fit");
          }}
        >
          {busy ? "Converting" : "Fit to disk (downsample)"}
        </button>
        <button
          className="btn primary"
          disabled={busy}
          onClick={() => {
            convert("split");
          }}
        >
          Two disk split
        </button>
        <button className="btn" onClick={actions.onClose}>
          Cancel
        </button>
        {dialog.hasInstrument && dirty && (
          <button
            className="btn solid"
            onClick={() => {
              actions.onExport();
            }}
          >
            Export first
          </button>
        )}
      </div>
    </>
  );
}

function PlacementBody({
  dialog,
  dirty,
  actions,
}: {
  dialog: Extract<PendingDialog, { kind: "placement" }>;
  dirty: boolean;
  actions: DialogActions;
}) {
  const isReplace = dialog.ext === "fzf" && !dialog.fromDisk;
  return (
    <>
      <Dialog.Title asChild>
        <h3>{isReplace ? "Replace the instrument?" : `Place ${dialog.file.name}`}</h3>
      </Dialog.Title>
      <p className="desc">
        {isReplace
          ? `This disk holds one instrument. ${dialog.file.name} replaces it entirely.`
          : dialog.fromDisk
            ? "This file already lives on the disk. Choose what to do with it."
            : "Choose where this file should land."}
      </p>
      {isReplace && dirty && (
        <p className="desc dangertext">
          Unexported changes will be lost. Export first to keep them.
        </p>
      )}
      <div
        className="buttons"
        style={isReplace ? undefined : { justifyContent: "flex-start", flexWrap: "wrap" }}
      >
        {dialog.options.map((o) =>
          o === "Delete file" ? (
            <button
              key={o}
              className="btn danger"
              onClick={() => {
                actions.onRequestDelete(dialog.file.name);
              }}
            >
              Delete file
            </button>
          ) : (
            <button
              key={o}
              className={isReplace ? "btn danger" : "btn primary"}
              style={isReplace ? { marginRight: "auto" } : undefined}
              onClick={() => {
                actions.onPlacementChoice(dialog, o);
              }}
            >
              {o}
            </button>
          ),
        )}
        <button className="btn" onClick={actions.onClose}>
          Cancel
        </button>
        {isReplace && dirty && (
          <button
            className="btn solid"
            onClick={() => {
              actions.onExport();
            }}
          >
            Export first
          </button>
        )}
      </div>
    </>
  );
}
