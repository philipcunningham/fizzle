// Voices tab: voice table on the left (rename, extract, map), the selected
// voice's editors on the right. Parameter groups render from the schema;
// waveform and envelopes get bespoke editors.

import { useState } from "react";
import type { Voice } from "../data/model";
import { clamp, formatBytes, noteName } from "../data/model";
import { GROUPS, fieldsForGroup } from "../data/schema";
import { EnvelopeEditor } from "../components/EnvelopeEditor";
import { Knob } from "../components/Knob";
import { SelectControl } from "../components/SelectControl";
import { Stepper } from "../components/Stepper";
import { Waveform } from "../components/Waveform";
import { NoInstrument } from "./NoInstrument";
import { useOpenInstrument, useStore } from "../state/store";

export function VoiceEditor() {
  const { state, dispatch } = useStore();
  const inst = useOpenInstrument();
  const [renaming, setRenaming] = useState<string | null>(null);
  const [activeLoop, setActiveLoop] = useState(0);

  if (!inst) return <NoInstrument />;

  const voice = inst.voices.find((v) => v.id === state.selectedVoiceId) ?? inst.voices[0] ?? null;
  const referenced = new Set(inst.banks.flatMap((b) => b.areas.map((a) => a.voiceId)));

  const edit = (patch: (v: Voice) => Voice) => {
    if (voice) dispatch({ type: "edit-voice", voiceId: voice.id, patch });
  };
  const gestureBegin = () => dispatch({ type: "gesture-begin" });
  const gestureCommit = () => dispatch({ type: "gesture-commit" });

  const setLoopField = (i: number, field: "start" | "end" | "crossfade" | "time", raw: string) => {
    const n = Number(raw);
    if (Number.isNaN(n)) return;
    edit((v) => ({
      ...v,
      loops: v.loops.map((l, li) => {
        if (li !== i) return l;
        if (field === "start") return { ...l, start: clamp(n, 0, l.end - 1) };
        if (field === "end") return { ...l, end: clamp(n, l.start + 1, v.frames - 1) };
        return { ...l, [field]: clamp(n, 0, 127) };
      }),
    }));
  };

  const groupPanel = (group: string) => {
    if (!voice) return null;
    return (
      <div className="panel" key={group}>
        <h2>{group}</h2>
        <div className="row">
          {fieldsForGroup(group).map((f) => {
            const value = f.get(voice);
            if (f.kind === "select") {
              return (
                <SelectControl
                  key={f.id}
                  label={f.label}
                  value={String(value)}
                  options={f.options ?? []}
                  onChange={(x) => edit((v) => f.set(v, x))}
                />
              );
            }
            if (f.kind === "stepper") {
              const isKey = f.id === "rootKey" || f.id === "keyLo" || f.id === "keyHi";
              return (
                <Stepper
                  key={f.id}
                  label={f.label}
                  value={Number(value)}
                  min={f.min ?? 0}
                  max={f.max ?? 127}
                  step={(f.max ?? 127) > 999 ? 100 : 1}
                  format={isKey ? (n) => `${noteName(n)} (${n})` : undefined}
                  onChange={(x) => edit((v) => f.set(v, x))}
                />
              );
            }
            return (
              <Knob
                key={f.id}
                label={f.label}
                value={Number(value)}
                min={f.min ?? 0}
                max={f.max ?? 127}
                onChange={(x) => edit((v) => f.set(v, x))}
                onGestureBegin={gestureBegin}
                onGestureCommit={gestureCommit}
              />
            );
          })}
        </div>
      </div>
    );
  };

  return (
    <div className="voicegrid">
      <div className="panel">
        <h2>Voices ({inst.voices.length}/64)</h2>
        <table className="term wide">
          <thead>
            <tr>
              <th>Name</th>
              <th>Range</th>
              <th>Size</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {inst.voices.map((v) => (
              <tr
                key={v.id}
                className={voice?.id === v.id ? "selected" : ""}
                onClick={() => dispatch({ type: "select-voice", id: v.id })}
                style={{ cursor: "pointer" }}
              >
                <td onDoubleClick={() => setRenaming(v.id)}>
                  {renaming === v.id ? (
                    <input
                      autoFocus
                      defaultValue={v.name}
                      aria-label="voice name" name="voice-name"
                      onBlur={(e) => {
                        dispatch({ type: "rename-voice", voiceId: v.id, name: e.target.value.toUpperCase().slice(0, 12) });
                        setRenaming(null);
                      }}
                      onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                    />
                  ) : (
                    <>
                      {v.name}
                      {!referenced.has(v.id) && (
                        <span title="No Area references this voice" style={{ color: "var(--fz-warning)", fontWeight: 700, fontSize: 14 }}>
                          {" "}
                          ∘
                        </span>
                      )}
                    </>
                  )}
                </td>
                <td>
                  {noteName(v.keyLo)}..{noteName(v.keyHi)}
                </td>
                <td>{formatBytes(v.sizeBytes)}</td>
                <td>
                  <button
                    className="btn small rowbtn"
                    aria-label={`export ${v.name}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      dispatch({ type: "open-dialog", dialog: { kind: "extract", voiceId: v.id } });
                    }}
                  >
                    Export
                  </button>
                  {!referenced.has(v.id) && (
                    <button
                      className="btn small rowbtn"
                      aria-label={`map ${v.name}`}
                      onClick={(e) => {
                        e.stopPropagation();
                        dispatch({ type: "map-voice", voiceId: v.id });
                      }}
                    >
                      Map
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {inst.voices.length > 0 && (
          <p style={{ color: "var(--fz-fg-faint)", fontSize: 10 }}>∘ marks a voice no Area plays yet. Double click a name to rename.</p>
        )}
      </div>

      {voice ? (
        <div style={{ minWidth: 0 }}>
          {groupPanel("Sample")}
          <div className="panel">
            <Waveform
              voice={voice}
              loopIndex={activeLoop}
              onLoopChange={(start, end) =>
                edit((v) => ({ ...v, loops: v.loops.map((l, i) => (i === activeLoop ? { ...l, start, end } : l)) }))
              }
            />
          </div>

          <div className="masonry">
            <div className="panel">
              <h2>Loops</h2>
              <table className="term wide">
                <thead>
                  <tr>
                    <th>Loop</th>
                    <th>Start</th>
                    <th>End</th>
                    <th>Crossfade</th>
                    <th>Time</th>
                  </tr>
                </thead>
                <tbody>
                  {voice.loops.map((l, i) => (
                    <tr key={i} className={i === activeLoop ? "selected" : ""} onClick={() => setActiveLoop(i)} style={{ cursor: "pointer" }}>
                      <td>{i + 1}</td>
                      <td>
                        <input aria-label={`loop ${i + 1} start`} name={`loop-${i + 1}-start`} value={l.start} onChange={(e) => setLoopField(i, "start", e.target.value)} />
                      </td>
                      <td>
                        <input aria-label={`loop ${i + 1} end`} name={`loop-${i + 1}-end`} value={l.end} onChange={(e) => setLoopField(i, "end", e.target.value)} />
                      </td>
                      <td>
                        <input aria-label={`loop ${i + 1} crossfade`} name={`loop-${i + 1}-crossfade`} value={l.crossfade} onChange={(e) => setLoopField(i, "crossfade", e.target.value)} />
                      </td>
                      <td>
                        <input aria-label={`loop ${i + 1} time`} name={`loop-${i + 1}-time`} value={l.time} onChange={(e) => setLoopField(i, "time", e.target.value)} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="panel">
              <EnvelopeEditor
                label="DCA envelope"
                envelope={voice.dcaEnv}
                onChange={(dcaEnv) => edit((v) => ({ ...v, dcaEnv }))}
                onGestureBegin={gestureBegin}
                onGestureCommit={gestureCommit}
              />
            </div>
            <div className="panel">
              <EnvelopeEditor
                label="DCF envelope"
                envelope={voice.dcfEnv}
                onChange={(dcfEnv) => edit((v) => ({ ...v, dcfEnv }))}
                onGestureBegin={gestureBegin}
                onGestureCommit={gestureCommit}
              />
            </div>
          {GROUPS.filter((g) => g !== "Sample").map(groupPanel)}
          </div>
        </div>
      ) : (
        <p className="empty">No voices yet. Import WAVs from the import menu.</p>
      )}
    </div>
  );
}
