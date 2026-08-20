// The voice editor: the voices table (rename, export, map,
// unreferenced marker), schema-driven group panels, the waveform with
// the active loop, the loops table with cross-fade and time, and both
// envelopes. Every edit is a slot addressed core call, and the values
// shown are the core's confirmed ones.
import { useEffect, useRef, useState } from "react";
import type { InstrumentSnapshot, SchemaField } from "../boundary/contract";
import { EnvelopeEditor } from "../ui/EnvelopeEditor";
import { Knob } from "../ui/Knob";
import { NumberCell } from "../ui/NumberCell";
import { SelectControl } from "../ui/SelectControl";
import { Stepper } from "../ui/Stepper";
import { Waveform } from "../ui/Waveform";
import { clamp, formatBytes } from "../ui/format";
import { noteName, parseNote } from "../ui/notes";
import { isSustainLoop } from "../viewstate/loops";

export interface VoiceEditorProps {
  instrument: InstrumentSnapshot;
  schema: SchemaField[];
  selectedSlot: number | null;
  selectedLoop: number;
  /** Peaks for the selected slot; null while loading or under jsdom. */
  peaks: Int16Array | null;
  onSelectVoice: (slot: number) => void;
  onSelectLoop: (index: number) => void;
  onSetParamNumber: (slot: number, field: string, value: number) => void;
  onSetParamOption: (slot: number, field: string, option: string) => void;
  onSetLoop: (slot: number, index: number, start: number, end: number) => void;
  /**
   * R14's generation window, in sample frames. Optional: the cells read
   * the core's window whether or not a caller takes their commits.
   */
  onSetGeneration: (slot: number, start: number, end: number) => void;
  onSetLoopAttr: (slot: number, index: number, xf: number, tm: number) => void;
  onSetLoopSelect: (slot: number, sustain: number, release: number) => void;
  onSetEnvelope: (
    slot: number,
    which: "dca" | "dcf",
    sustain: number,
    end: number,
    rates: number[],
    stops: number[],
  ) => void;
  onRename: (slot: number, name: string) => void;
  onMapVoice: (slot: number) => void;
  onExtract: (slot: number, name: string) => void;
  onGestureBegin: () => void;
  onGestureCommit: () => void;
}

const LOOP_NONE = 8;
const loopChoices = ["1", "2", "3", "4", "5", "6", "7", "8", "none"];

export function VoiceEditor(props: VoiceEditorProps) {
  const { instrument, schema, selectedSlot } = props;
  const [renaming, setRenaming] = useState<number | null>(null);
  // The field replaces the name inside the row, so committing would
  // leave focus on the body. Put it back on the row it began from.
  const renameRow = useRef<HTMLTableRowElement | null>(null);
  const wasRenaming = useRef<number | null>(null);
  useEffect(() => {
    if (wasRenaming.current !== null && renaming === null) renameRow.current?.focus();
    wasRenaming.current = renaming;
  }, [renaming]);

  const voice =
    instrument.voices.find((v) => v.slot === selectedSlot) ?? instrument.voices[0] ?? null;
  const detail = voice?.voice ?? null;
  const params = voice?.params ?? null;
  const groups = [...new Set(schema.map((f) => f.group))];

  const groupPanel = (group: string) => {
    if (!voice || !params) return null;
    return (
      <div className="panel" key={group}>
        <h2>{group}</h2>
        <div className="row">
          {schema
            .filter((f) => f.group === group)
            .map((f) => {
              const value = params[f.id] ?? 0;
              if (f.kind === "select") {
                // A real voice can carry a mode the schema doesn't list
                // (the factory library's normal_variant, say). Show it
                // rather than a blank control the user can't read.
                const options = f.options ?? [];
                const shown = options.includes(String(value))
                  ? options
                  : [...options, String(value)];
                return (
                  <SelectControl
                    key={f.id}
                    label={f.label}
                    value={String(value)}
                    options={shown}
                    onChange={(x) => {
                      props.onSetParamOption(voice.slot, f.id, x);
                    }}
                  />
                );
              }
              if (f.kind === "note") {
                return (
                  <Stepper
                    key={f.id}
                    label={f.label}
                    value={Number(value)}
                    min={f.min}
                    max={f.max}
                    format={(n) => `${noteName(n)} (${String(n)})`}
                    parse={parseNote}
                    onChange={(x) => {
                      props.onSetParamNumber(voice.slot, f.id, x);
                    }}
                  />
                );
              }
              if (f.kind === "knob") {
                return (
                  <Knob
                    key={f.id}
                    label={f.label}
                    value={Number(value)}
                    min={f.min}
                    max={f.max}
                    onChange={(x) => {
                      props.onSetParamNumber(voice.slot, f.id, x);
                    }}
                    onGestureBegin={props.onGestureBegin}
                    onGestureCommit={props.onGestureCommit}
                  />
                );
              }
              // Steppers, and the numeric fallback for kinds this UI
              // does not know (E2: the schema is the core's).
              return (
                <Stepper
                  key={f.id}
                  label={f.label}
                  value={Number(value)}
                  min={f.min}
                  max={f.max}
                  step={f.max - f.min > 999 ? 100 : 1}
                  onChange={(x) => {
                    props.onSetParamNumber(voice.slot, f.id, x);
                  }}
                />
              );
            })}
        </div>
      </div>
    );
  };

  const activeLoop = detail?.loops[props.selectedLoop] ?? null;

  return (
    <div className="voicegrid">
      <div className="panel">
        <h2>Voices ({instrument.voices.length}/64)</h2>
        <table className="term wide" aria-label="instrument voices">
          <thead>
            <tr>
              <th>Name</th>
              <th>Range</th>
              <th>Size</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {instrument.voices.map((v) => (
              <tr
                key={v.slot}
                className={voice?.slot === v.slot ? "selected" : ""}
                // Rows are the only way to change the edited voice, so
                // they must be reachable without a pointer (Q5).
                tabIndex={0}
                aria-selected={voice?.slot === v.slot}
                onClick={() => {
                  props.onSelectVoice(v.slot);
                }}
                onKeyDown={(e) => {
                  // Only when the row itself holds focus: a key pressed
                  // on a button or the rename input inside it bubbles
                  // up here, and cancelling the default would stop the
                  // button activating or a space reaching the field.
                  if (e.target !== e.currentTarget) return;
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    props.onSelectVoice(v.slot);
                  }
                  // Rename without a pointer: double click has no
                  // keyboard equivalent (Q5).
                  if (e.key === "F2") {
                    renameRow.current = e.currentTarget;
                    e.preventDefault();
                    setRenaming(v.slot);
                  }
                }}
                style={{ cursor: "pointer" }}
              >
                <td
                  onDoubleClick={(e) => {
                    renameRow.current = e.currentTarget.closest("tr");
                    setRenaming(v.slot);
                  }}
                >
                  {renaming === v.slot ? (
                    <input
                      // eslint-disable-next-line jsx-a11y/no-autofocus -- rename begins typing immediately, the mockup's validated flow
                      autoFocus
                      defaultValue={v.name}
                      aria-label="voice name"
                      name="voice-name"
                      onBlur={(e) => {
                        props.onRename(v.slot, e.target.value.toUpperCase().slice(0, 12));
                        setRenaming(null);
                      }}
                      onKeyDown={(e) => {
                        if (e.key !== "Enter") return;
                        // Committing hands focus back to the row during
                        // this keydown, so Enter's default action would
                        // land there. The key stops here.
                        e.preventDefault();
                        (e.target as HTMLInputElement).blur();
                      }}
                    />
                  ) : (
                    <>
                      {v.name}
                      {!v.referenced && (
                        <span className="unrefmark" title="No Area references this voice">
                          {" "}
                          ∘
                        </span>
                      )}
                    </>
                  )}
                </td>
                <td>
                  {typeof v.params?.["keyLow"] === "number" &&
                  typeof v.params["keyHigh"] === "number"
                    ? `${noteName(v.params["keyLow"])}..${noteName(v.params["keyHigh"])}`
                    : "–"}
                </td>
                <td title={v.sharesAudio ? "shares its audio with another voice" : undefined}>
                  {v.sharesAudio ? "shared" : v.voice ? formatBytes(v.voice.frames * 2) : "–"}
                </td>
                <td>
                  <button
                    className="btn small rowbtn"
                    aria-label={`export ${v.name}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      props.onExtract(v.slot, v.name);
                    }}
                  >
                    Export
                  </button>
                  {!v.referenced && (
                    <button
                      className="btn small rowbtn"
                      aria-label={`map ${v.name}`}
                      onClick={(e) => {
                        e.stopPropagation();
                        props.onMapVoice(v.slot);
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
        {instrument.voices.length > 0 && (
          <p className="tablehint">
            ∘ marks a voice no Area plays yet. Rename with F2, or a double click.
          </p>
        )}
      </div>

      {voice && detail ? (
        <div style={{ minWidth: 0 }}>
          {groups[0] !== undefined && groupPanel(groups[0])}
          <div className="panel">
            <Waveform
              voiceKey={`slot-${String(voice.slot)}`}
              frames={detail.frames}
              peaks={props.peaks}
              loopIndex={props.selectedLoop}
              loop={activeLoop ?? { start: 0, end: detail.frames, xf: 0, tm: 0 }}
              sustain={isSustainLoop(detail, props.selectedLoop)}
              onSetLoop={(start, end) => {
                props.onSetLoop(voice.slot, props.selectedLoop, start, end);
              }}
              onGestureBegin={props.onGestureBegin}
              onGestureCommit={props.onGestureCommit}
            />
          </div>

          <div className="masonry">
            <div className="panel">
              <h2>Generation</h2>
              <table className="term wide" aria-label="generation window">
                <thead>
                  <tr>
                    <th>Start</th>
                    <th>End</th>
                  </tr>
                </thead>
                <tbody>
                  <tr>
                    <td>
                      <NumberCell
                        label="generation start"
                        name="generation-start"
                        value={detail.genStart}
                        onCommit={(n) => {
                          props.onSetGeneration(
                            voice.slot,
                            clamp(n, 0, detail.genEnd),
                            detail.genEnd,
                          );
                        }}
                      />
                    </td>
                    <td>
                      <NumberCell
                        label="generation end"
                        name="generation-end"
                        value={detail.genEnd}
                        onCommit={(n) => {
                          props.onSetGeneration(
                            voice.slot,
                            detail.genStart,
                            clamp(n, detail.genStart, detail.frames),
                          );
                        }}
                      />
                    </td>
                  </tr>
                </tbody>
              </table>
              <p className="tablehint">
                Sample frames the FZ plays back, within this voice&apos;s {detail.frames}. Each cell
                shows the frame the core confirmed.
              </p>
            </div>

            <div className="panel">
              <h2>Loops</h2>
              <table className="term wide" aria-label="loops">
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
                  {detail.loops.map((l, i) => (
                    <tr
                      key={`loop-${String(i)}`}
                      className={i === props.selectedLoop ? "selected" : ""}
                      tabIndex={0}
                      aria-selected={i === props.selectedLoop}
                      onClick={() => {
                        props.onSelectLoop(i);
                      }}
                      onKeyDown={(e) => {
                        // The row's own keys only: a space typed into a
                        // number cell in this row must reach the cell.
                        if (e.target !== e.currentTarget) return;
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          props.onSelectLoop(i);
                        }
                      }}
                      style={{ cursor: "pointer" }}
                    >
                      <td>{i + 1}</td>
                      <td>
                        <NumberCell
                          label={`loop ${String(i + 1)} start`}
                          name={`loop-${String(i + 1)}-start`}
                          value={l.start}
                          onCommit={(n) => {
                            props.onSetLoop(voice.slot, i, clamp(n, 0, l.end - 1), l.end);
                          }}
                        />
                      </td>
                      <td>
                        <NumberCell
                          label={`loop ${String(i + 1)} end`}
                          name={`loop-${String(i + 1)}-end`}
                          value={l.end}
                          onCommit={(n) => {
                            props.onSetLoop(
                              voice.slot,
                              i,
                              l.start,
                              clamp(n, l.start + 1, detail.frames),
                            );
                          }}
                        />
                      </td>
                      <td>
                        <NumberCell
                          label={`loop ${String(i + 1)} crossfade`}
                          name={`loop-${String(i + 1)}-crossfade`}
                          value={l.xf}
                          onCommit={(n) => {
                            props.onSetLoopAttr(voice.slot, i, clamp(n, 0, 1023), l.tm);
                          }}
                        />
                      </td>
                      <td>
                        <NumberCell
                          label={`loop ${String(i + 1)} time`}
                          name={`loop-${String(i + 1)}-time`}
                          value={l.tm}
                          onCommit={(n) => {
                            props.onSetLoopAttr(voice.slot, i, l.xf, clamp(n, 0, 1022));
                          }}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <div className="row">
                <SelectControl
                  label="Sustain loop"
                  value={loopChoices[clamp(detail.loopSustain, 0, LOOP_NONE)] ?? "none"}
                  options={loopChoices}
                  onChange={(x) => {
                    props.onSetLoopSelect(
                      voice.slot,
                      x === "none" ? LOOP_NONE : Number(x) - 1,
                      detail.loopRelease,
                    );
                  }}
                />
                <SelectControl
                  label="Release loop"
                  value={loopChoices[clamp(detail.loopRelease, 0, LOOP_NONE)] ?? "none"}
                  options={loopChoices}
                  onChange={(x) => {
                    props.onSetLoopSelect(
                      voice.slot,
                      detail.loopSustain,
                      x === "none" ? LOOP_NONE : Number(x) - 1,
                    );
                  }}
                />
              </div>
            </div>

            <div className="panel">
              <EnvelopeEditor
                label="DCA envelope"
                envelope={detail.dca}
                onChange={(sustain, end, rates, stops) => {
                  props.onSetEnvelope(voice.slot, "dca", sustain, end, rates, stops);
                }}
                onGestureBegin={props.onGestureBegin}
                onGestureCommit={props.onGestureCommit}
              />
            </div>
            <div className="panel">
              <EnvelopeEditor
                label="DCF envelope"
                envelope={detail.dcf}
                onChange={(sustain, end, rates, stops) => {
                  props.onSetEnvelope(voice.slot, "dcf", sustain, end, rates, stops);
                }}
                onGestureBegin={props.onGestureBegin}
                onGestureCommit={props.onGestureCommit}
              />
            </div>
            {groups.slice(1).map(groupPanel)}
          </div>
        </div>
      ) : (
        <p className="empty">No voices yet. Import WAVs to fill the instrument.</p>
      )}
    </div>
  );
}
