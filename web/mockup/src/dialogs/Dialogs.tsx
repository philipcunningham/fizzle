// Every modal prompt in one place, driven by store.dialog: new disk
// label, WAV rate and stereo, SFZ fit or split, the placement
// prompts, extract, switch disk, and delete confirmation.

import * as Dialog from "@radix-ui/react-dialog";
import * as RadioGroup from "@radix-ui/react-radio-group";
import { useState } from "react";
import { IMAGE_SIZE, formatBytes, usedBytes } from "../data/model";
import { useOpenInstrument, useStore } from "../state/store";

export function Dialogs() {
  const { state, dispatch } = useStore();
  const d = state.dialog;
  const inst = useOpenInstrument();
  const [label, setLabel] = useState("FZ DISK 1");
  const [rate, setRate] = useState("18");
  const [stereo, setStereo] = useState("Mix");

  if (!d) return null;

  // Projected fit, shown before an import commits: the best error is
  // the one that never fires. The mock charges fixed sizes per type.
  const PLACE_BYTES: Record<string, number> = { fzb: 96_000, fzv: 44_000 };
  const wavBytes = d.kind === "wavImport" ? d.names.length * 68_000 : 0;
  const placeBytes = d.kind === "placement" && !d.fromDisk ? (PLACE_BYTES[d.ext] ?? 0) : 0;
  const freeBytes = state.doc.disk ? IMAGE_SIZE * 2 - usedBytes(state.doc.disk) : IMAGE_SIZE * 2;
  const wavFits = wavBytes <= freeBytes;

  // A dropped full dump replaces the disk's one instrument, which is
  // as destructive as closing the disk, so it carries the same
  // unexported changes guard.
  const isReplace = d.kind === "placement" && d.ext === "fzf" && !d.fromDisk;
  const replaced = inst ?? Object.values(state.doc.instruments)[0] ?? null;

  const close = () => dispatch({ type: "close-dialog" });

  return (
    <Dialog.Root open onOpenChange={(open) => !open && close()}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog">
          {d.kind === "newDisk" && (
            <>
              <Dialog.Title asChild>
                <h3>New disk</h3>
              </Dialog.Title>
              <p className="desc">
                {d.then ? "No disk is open, so the import gets one first. Just pick a label." : "A blank 1.25 MB FZ disk."}
              </p>
              <div className="row">
                <input type="text" aria-label="disk label" name="disk-label" id="disk-label" value={label} onChange={(e) => setLabel(e.target.value.toUpperCase())} />
              </div>
              <div className="buttons">
                <button className="btn" onClick={close}>
                  Cancel
                </button>
                <button className="btn solid" onClick={() => dispatch({ type: "new-disk", label, then: d.then })}>
                  Create
                </button>
              </div>
            </>
          )}

          {d.kind === "wavImport" && (
            <>
              <Dialog.Title asChild>
                <h3>Import {d.names.length} WAV{d.names.length === 1 ? "" : "s"}</h3>
              </Dialog.Title>
              <p className="desc">One answer covers the whole batch. Nothing is silently truncated.</p>
              <div className="row">
                <span style={{ width: 90, color: "var(--fz-fg-faint)" }}>sample rate</span>
                <RadioGroup.Root className="row" value={rate} onValueChange={setRate} aria-label="sample rate" name="sample-rate">
                  {["36", "18", "9"].map((r) => (
                    <label key={r} className="radio-item">
                      <RadioGroup.Item className="radio-dot" value={r} />
                      {r} kHz
                    </label>
                  ))}
                </RadioGroup.Root>
              </div>
              <div className="row">
                <span style={{ width: 90, color: "var(--fz-fg-faint)" }}>stereo</span>
                <RadioGroup.Root className="row" value={stereo} onValueChange={setStereo} aria-label="stereo handling" name="stereo">
                  {["Left", "Right", "Mix"].map((s) => (
                    <label key={s} className="radio-item">
                      <RadioGroup.Item className="radio-dot" value={s} />
                      {s}
                    </label>
                  ))}
                </RadioGroup.Root>
              </div>
              <p className="desc" style={wavFits ? undefined : { color: "var(--fz-error)" }}>
                {wavFits
                  ? `Adds about ${formatBytes(wavBytes)}; ${formatBytes(freeBytes)} free.`
                  : `Won't fit: about ${formatBytes(wavBytes)} into ${formatBytes(Math.max(0, freeBytes))} free.`}
              </p>
              <div className="buttons">
                <button className="btn" onClick={close}>
                  Cancel
                </button>
                <button
                  className="btn solid"
                  onClick={() => dispatch({ type: "import-wavs", names: d.names, rate: Number(rate), stereo })}
                >
                  Convert
                </button>
              </div>
            </>
          )}

          {d.kind === "sfzImport" && (
            <>
              <Dialog.Title asChild>
                <h3>SFZ conversion</h3>
              </Dialog.Title>
              <p className="desc">
                "{d.name}" converts with fizzle's engine. At 36 kHz it exceeds one disk, so choose how to place it.
                {Object.keys(state.doc.instruments).length > 0 && " This replaces the current instrument."}
              </p>
              {Object.keys(state.doc.instruments).length > 0 && state.doc.dirty && (
                <p className="desc" style={{ color: "var(--fz-error)" }}>
                  Unexported changes will be lost. Export first to keep them.
                </p>
              )}
              <div className="buttons" style={{ justifyContent: "flex-start", flexWrap: "wrap" }}>
                <button className="btn primary" onClick={() => dispatch({ type: "import-sfz", name: d.name, mode: "fit" })}>
                  Fit to disk (downsample)
                </button>
                <button className="btn primary" onClick={() => dispatch({ type: "import-sfz", name: d.name, mode: "split" })}>
                  Two disk split
                </button>
                <button className="btn" onClick={close}>
                  Cancel
                </button>
                {Object.keys(state.doc.instruments).length > 0 && state.doc.dirty && (
                  <button className="btn solid" onClick={() => dispatch({ type: "export" })}>
                    Export first
                  </button>
                )}
              </div>
            </>
          )}

          {d.kind === "placement" && (
            <>
              <Dialog.Title asChild>
                <h3>{isReplace ? "Replace the instrument?" : `Place ${d.name}`}</h3>
              </Dialog.Title>
              <p className="desc">
                {isReplace
                  ? `This disk holds one instrument. ${d.name} replaces ${replaced?.name ?? "it"} entirely.`
                  : d.fromDisk
                    ? "This file already lives on the disk. Choose what to do with it."
                    : "Choose where this file should land."}
              </p>
              {isReplace && state.doc.dirty && (
                <p className="desc" style={{ color: "var(--fz-error)" }}>
                  Unexported changes will be lost. Export first to keep them.
                </p>
              )}
              {placeBytes > 0 && (
                <p className="desc">
                  Adds about {formatBytes(placeBytes)}; {formatBytes(Math.max(0, freeBytes))} free.
                </p>
              )}
              {/* The replace guard mirrors the close guard's order:
                  destructive apart on the left, the back out in the
                  middle, Export first emphasised on the right. */}
              <div className="buttons" style={isReplace ? undefined : { justifyContent: "flex-start", flexWrap: "wrap" }}>
                {d.options.map((o) =>
                  o === "Delete file" ? (
                    <button
                      key={o}
                      className="btn danger"
                      onClick={() => d.fileId && dispatch({ type: "open-dialog", dialog: { kind: "confirmDelete", fileId: d.fileId, name: d.name } })}
                    >
                      Delete file
                    </button>
                  ) : (
                    <button
                      key={o}
                      className={isReplace ? "btn danger" : "btn primary"}
                      style={isReplace ? { marginRight: "auto" } : undefined}
                      onClick={() => dispatch({ type: "import-file", ext: d.ext, name: d.name, choice: o, fromDisk: d.fromDisk })}
                    >
                      {o}
                    </button>
                  ),
                )}
                <button className="btn" onClick={close}>
                  Cancel
                </button>
                {isReplace && state.doc.dirty && (
                  <button className="btn solid" onClick={() => dispatch({ type: "export" })}>
                    Export first
                  </button>
                )}
              </div>
            </>
          )}

          {d.kind === "extract" && (
            <>
              <Dialog.Title asChild>
                <h3>Export voice</h3>
              </Dialog.Title>
              <p className="desc">
                "{inst?.voices.find((v) => v.id === d.voiceId)?.name ?? "voice"}" saves through the platform dialog.
              </p>
              <div className="buttons" style={{ justifyContent: "flex-start" }}>
                <button
                  className="btn primary"
                  onClick={() => {
                    dispatch({ type: "extract", what: `${inst?.voices.find((v) => v.id === d.voiceId)?.name ?? "voice"} as .wav` });
                    close();
                  }}
                >
                  As .wav
                </button>
                <button
                  className="btn primary"
                  onClick={() => {
                    dispatch({ type: "extract", what: `${inst?.voices.find((v) => v.id === d.voiceId)?.name ?? "voice"} as .fzv` });
                    close();
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
                  ? "Closing this disk set discards edits that haven't been exported."
                  : "Opening a different disk set discards edits that haven't been exported."}
              </p>
              <div className="buttons">
                <button
                  className="btn danger"
                  style={{ marginRight: "auto" }}
                  onClick={() => {
                    dispatch({ type: "close-disk" });
                    if (d.intent === "switch") dispatch({ type: "open-seed-disk" });
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
                    dispatch({ type: "export" });
                    dispatch({ type: "close-disk" });
                    if (d.intent === "switch") dispatch({ type: "open-seed-disk" });
                  }}
                >
                  Export first
                </button>
              </div>
            </>
          )}

          {d.kind === "confirmDelete" && (
            <>
              {/* Deleting the full dump destroys the whole instrument,
                  so it names the instrument, states the loss, and
                  carries the same Export first guard as replace and
                  close. Plain files keep the short confirm. */}
              {(() => {
                const file = state.doc.disk?.files.find((f) => f.id === d.fileId);
                const delInst = file?.instrumentId ? state.doc.instruments[file.instrumentId] : null;
                return (
                  <>
                    <Dialog.Title asChild>
                      <h3>{delInst ? `Delete ${delInst.name}?` : "Delete file"}</h3>
                    </Dialog.Title>
                    <p className="desc">
                      {delInst
                        ? `This removes the instrument and all ${delInst.voices.length} of its voices from the disk (the ${d.name} file).`
                        : `Delete "${d.name}" from the disk? Destructive actions confirm first.`}
                    </p>
                    {delInst && state.doc.dirty && (
                      <p className="desc" style={{ color: "var(--fz-error)" }}>
                        Unexported changes will be lost. Export first to keep them.
                      </p>
                    )}
                    <div className="buttons">
                      <button
                        className="btn danger"
                        style={{ marginRight: "auto" }}
                        onClick={() => dispatch({ type: "delete-file", fileId: d.fileId })}
                      >
                        Delete
                      </button>
                      <button className="btn" onClick={close}>
                        Cancel
                      </button>
                      {delInst && state.doc.dirty && (
                        <button className="btn solid" onClick={() => dispatch({ type: "export" })}>
                          Export first
                        </button>
                      )}
                    </div>
                  </>
                );
              })()}
            </>
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
