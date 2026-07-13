// Banks and Areas tab: bank strip, area table, draggable key
// and velocity ranges with numeric entry beside, duplicate for velocity
// switching.

import { useState } from "react";
import type { Area } from "../data/model";
import { OUTPUTS, noteName } from "../data/model";
import { RangeSlider } from "../components/RangeSlider";
import { SelectControl } from "../components/SelectControl";
import { Stepper } from "../components/Stepper";
import { NoInstrument } from "./NoInstrument";
import { useOpenInstrument, useStore } from "../state/store";

export function BanksAreas() {
  const { state, dispatch } = useStore();
  const inst = useOpenInstrument();
  const [renamingBank, setRenamingBank] = useState<string | null>(null);

  if (!inst) return <NoInstrument />;

  const bank = inst.banks.find((b) => b.id === state.selectedBankId) ?? inst.banks[0];
  const area = bank?.areas.find((a) => a.id === state.selectedAreaId) ?? null;
  const voiceName = (id: string | null) => inst.voices.find((v) => v.id === id)?.name ?? "(none)";

  const editArea = (id: string, patch: (a: Area) => Area) => dispatch({ type: "edit-area", bankId: bank.id, areaId: id, patch });
  const gestureBegin = () => dispatch({ type: "gesture-begin" });
  const gestureCommit = () => dispatch({ type: "gesture-commit" });

  return (
    <div className="centered">
      <div className="row" style={{ marginBottom: 12 }}>
        {inst.banks.map((b) => (
          <button
            key={b.id}
            className={b.id === bank.id ? "btn primary" : "btn"}
            onClick={() => dispatch({ type: "select-bank", id: b.id })}
            onDoubleClick={() => setRenamingBank(b.id)}
          >
            {renamingBank === b.id ? (
              <input
                autoFocus
                defaultValue={b.name}
                aria-label="bank name" name="bank-name"
                onClick={(e) => e.stopPropagation()}
                onBlur={(e) => {
                  dispatch({ type: "rename-bank", bankId: b.id, name: e.target.value.toUpperCase().slice(0, 12) });
                  setRenamingBank(null);
                }}
                onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                style={{ width: 90 }}
              />
            ) : (
              `${b.name} (${b.areas.length})`
            )}
          </button>
        ))}
      </div>

      <div className="panel">
        <h2>
          Areas in {bank.name} ({bank.areas.length}/64)
        </h2>
        <table className="term">
          <thead>
            <tr>
              <th>Voice</th>
              <th>Key range</th>
              <th>Velocity</th>
              <th>Output</th>
              <th>MIDI ch</th>
              <th>Volume</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {bank.areas.map((a) => (
              <tr
                key={a.id}
                className={area?.id === a.id ? "selected" : ""}
                onClick={() => dispatch({ type: "select-area", id: a.id })}
                style={{ cursor: "pointer" }}
              >
                <td>{voiceName(a.voiceId)}</td>
                <td>
                  {noteName(a.keyLo)}..{noteName(a.keyHi)}
                </td>
                <td>
                  {a.velLo}..{a.velHi}
                </td>
                <td>{a.output}</td>
                <td>{a.midiChannel}</td>
                <td>{a.volume}</td>
                <td>
                  <button
                    className="btn small rowbtn"
                    aria-label="duplicate area"
                    title="duplicate for a velocity switch"
                    onClick={(e) => {
                      e.stopPropagation();
                      dispatch({ type: "duplicate-area", bankId: bank.id, areaId: a.id });
                    }}
                  >
                    Duplicate
                  </button>
                  <button
                    className="btn small danger rowbtn"
                    aria-label="delete area"
                    onClick={(e) => {
                      e.stopPropagation();
                      dispatch({ type: "delete-area", bankId: bank.id, areaId: a.id });
                    }}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="row" style={{ marginTop: 8 }}>
          <button className="btn" onClick={() => dispatch({ type: "add-area", bankId: bank.id })}>
            Add area
          </button>
        </div>
      </div>

      {area && (
        <div className="panel">
          <h2>Edit area · {voiceName(area.voiceId)}</h2>
          <div className="row" style={{ gap: 24 }}>
            <div className="field" style={{ alignItems: "flex-start" }}>
              <label>Key range (drag or type)</label>
              <RangeSlider
                lo={area.keyLo}
                hi={area.keyHi}
                format={noteName}
                onChange={(lo, hi) => editArea(area.id, (a) => ({ ...a, keyLo: lo, keyHi: hi }))}
                onGestureBegin={gestureBegin}
                onGestureCommit={gestureCommit}
              />
              <div className="row">
                <Stepper label="Low" value={area.keyLo} min={0} max={127} format={(n) => `${noteName(n)}`} onChange={(x) => editArea(area.id, (a) => ({ ...a, keyLo: Math.min(x, a.keyHi) }))} />
                <Stepper label="High" value={area.keyHi} min={0} max={127} format={(n) => `${noteName(n)}`} onChange={(x) => editArea(area.id, (a) => ({ ...a, keyHi: Math.max(x, a.keyLo) }))} />
              </div>
            </div>
            <div className="field" style={{ alignItems: "flex-start" }}>
              <label>Velocity range (drag or type)</label>
              <RangeSlider
                lo={area.velLo}
                hi={area.velHi}
                onChange={(lo, hi) => editArea(area.id, (a) => ({ ...a, velLo: lo, velHi: hi }))}
                onGestureBegin={gestureBegin}
                onGestureCommit={gestureCommit}
              />
              <div className="row">
                <Stepper label="Low" value={area.velLo} min={0} max={127} onChange={(x) => editArea(area.id, (a) => ({ ...a, velLo: Math.min(x, a.velHi) }))} />
                <Stepper label="High" value={area.velHi} min={0} max={127} onChange={(x) => editArea(area.id, (a) => ({ ...a, velHi: Math.max(x, a.velLo) }))} />
              </div>
            </div>
            <SelectControl
              label="Voice"
              value={voiceName(area.voiceId)}
              options={inst.voices.map((v) => v.name)}
              onChange={(name) => {
                const v = inst.voices.find((x) => x.name === name);
                if (v) editArea(area.id, (a) => ({ ...a, voiceId: v.id }));
              }}
            />
            <SelectControl label="Output" value={area.output} options={OUTPUTS} onChange={(o) => editArea(area.id, (a) => ({ ...a, output: o }))} />
            <Stepper label="MIDI ch" value={area.midiChannel} min={1} max={16} onChange={(x) => editArea(area.id, (a) => ({ ...a, midiChannel: x }))} />
            <Stepper label="Volume" value={area.volume} min={0} max={127} onChange={(x) => editArea(area.id, (a) => ({ ...a, volume: x }))} />
          </div>
        </div>
      )}
    </div>
  );
}
