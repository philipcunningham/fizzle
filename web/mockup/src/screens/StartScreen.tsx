// Start screen: new disk plus a drop zone that accepts every
// supported type, with Browse beneath it as the picker path. The import
// buttons inside the drop zone simulate the drops the walkthrough needs.

import { useState } from "react";
import { routeFiles } from "../drop";
import { useStore } from "../state/store";

export function StartScreen() {
  const { dispatch } = useStore();
  const [over, setOver] = useState(false);

  return (
    <div className="start">
      <h1>fizzle</h1>
      <p className="hint">A Casio FZ disk editor in the browser · interactive mockup, canned data</p>
      <div className="actions">
        <button className="btn primary" onClick={() => dispatch({ type: "open-dialog", dialog: { kind: "newDisk" } })}>
          New disk
        </button>
      </div>
      <div
        className={over ? "dropzone over" : "dropzone"}
        onDragOver={(e) => {
          e.preventDefault();
          setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setOver(false);
          routeFiles(e.dataTransfer, dispatch);
        }}
      >
        Drop .img (one or a pair), .fzf, .fzb, .fzv, .wav, or an SFZ folder here
        <div className="row" style={{ marginTop: 16, justifyContent: "center" }}>
          <button className="btn" onClick={() => routeFiles(fakeDrop(["KICK.wav", "SNARE.wav", "HAT.wav", "CLAP.wav"]), dispatch)}>
            Import WAV folder
          </button>
          <button className="btn" onClick={() => routeFiles(fakeDrop(["AMBER KEYS.sfz"]), dispatch)}>
            Import SFZ folder
          </button>
          <button className="btn" onClick={() => routeFiles(fakeDrop(["STRINGS.fzf"]), dispatch)}>
            Import .fzf file
          </button>
        </div>
      </div>
      <button className="btn" onClick={() => dispatch({ type: "open-seed-disk" })}>
        Browse
      </button>
      <p className="hint">The Journeys menu (top bar) walks each spec journey, J1 to J7</p>
    </div>
  );
}

// fakeDrop builds the name list a real DataTransfer would carry.
export function fakeDrop(names: string[]): { names: string[] } {
  return { names };
}
