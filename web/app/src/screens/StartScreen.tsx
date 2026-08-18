// The mockup's start screen: a name, two ways in (new disk, browse),
// and a dropzone that takes everything R6 lists.
import { useState } from "react";

export interface StartScreenProps {
  onNewDisk: () => void;
  onBrowse: () => void;
  /** The whole transfer, so the shell can walk a dropped folder (R6). */
  onDropFiles: (transfer: DataTransfer) => void;
}

export function StartScreen({ onNewDisk, onBrowse, onDropFiles }: StartScreenProps) {
  const [over, setOver] = useState(false);
  return (
    <div className="start">
      <h1>fizzle</h1>
      <p className="hint">FZ instruments in the browser, byte exact.</p>
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
      <p className="hint">Nothing leaves this machine. Export writes the disk image.</p>
    </div>
  );
}
