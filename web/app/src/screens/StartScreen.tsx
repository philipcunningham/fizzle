// The mockup's start screen: a name, two ways in (new disk, browse),
// and a dropzone that takes everything R6 lists.
import { useState } from "react";
import { MEMORY_CHOICES } from "../ui/CapacityBar";

export interface StartScreenProps {
  onNewDisk: () => void;
  onBrowse: () => void;
  /** The machine the user builds for, declared here (R27, R31). */
  memoryBytes: number;
  onSetMemory: (bytes: number) => void;
  /** The whole transfer, so the shell can walk a dropped folder (R6). */
  onDropFiles: (transfer: DataTransfer) => void;
}

export function StartScreen({
  onNewDisk,
  onBrowse,
  onDropFiles,
  memoryBytes,
  onSetMemory,
}: StartScreenProps) {
  const [over, setOver] = useState(false);
  return (
    <div className="start">
      <h1>fizzle</h1>
      <p className="hint">FZ instruments in the browser.</p>
      <div className="actions">
        <button className="btn solid" onClick={onNewDisk}>
          New disk
        </button>
      </div>
      <div
        className={over ? "dropzone over" : "dropzone"}
        onDragOver={(e) => {
          e.preventDefault();
          setOver(true);
        }}
        onDragLeave={() => {
          setOver(false);
        }}
        onDrop={(e) => {
          e.preventDefault();
          setOver(false);
          onDropFiles(e.dataTransfer);
        }}
      >
        Drop a disk image, WAVs, an SFZ folder, or FZ files here
        <br />
        <button className="btn small" onClick={onBrowse}>
          Browse
        </button>
      </div>
      <p className="hint">
        Building for a{" "}
        <select
          className="memorypick"
          aria-label="sampler memory"
          value={memoryBytes}
          onChange={(e) => {
            onSetMemory(Number(e.target.value));
          }}
        >
          {MEMORY_CHOICES.map((c) => (
            <option key={c.bytes} value={c.bytes}>
              {c.label}
            </option>
          ))}
        </select>{" "}
        sampler. An FZ-1 has 1 MB unless the expansion card is fitted; the rack units have 2 MB.
      </p>
      <p className="hint">
        Nothing leaves this machine: your samples and disks never touch a remote server.
      </p>
    </div>
  );
}
