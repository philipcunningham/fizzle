// The mockup's Banks and Areas screen (R11 to R13) over the real
// core: a bank strip with inline rename, the areas table with
// duplicate (the velocity switch), delete, and reorder, and the edit
// panel with draggable ranges beside numeric entry (R12).
import { useEffect, useRef, useState } from "react";
import type { InstrumentSnapshot } from "../boundary/contract";
import { RangeSlider } from "../ui/RangeSlider";
import { SelectControl } from "../ui/SelectControl";
import { Stepper } from "../ui/Stepper";
import { noteName, parseNote } from "../ui/notes";

export interface BanksAreasProps {
  instrument: InstrumentSnapshot;
  selectedBank: number;
  selectedArea: number | null;
  onSelectBank: (bank: number) => void;
  onSelectArea: (area: number) => void;
  onRenameBank: (bank: number, name: string) => void;
  onSetAreaField: (bank: number, area: number, field: string, value: number) => void;
  onAddArea: (bank: number) => void;
  onDuplicateArea: (bank: number, area: number) => void;
  onDeleteArea: (bank: number, area: number) => void;
  onSwapAreas: (bank: number, a: number, b: number) => void;
  onGestureBegin: () => void;
  onGestureCommit: () => void;
}

// The FZ routes an area to the mix or a single output; the byte is a
// generator bitmask, so combined masks from other tools surface as
// their raw label.
const OUTPUT_CHOICES: { label: string; value: number }[] = [
  { label: "all", value: 255 },
  ...Array.from({ length: 8 }, (_, i) => ({ label: `out ${String(i + 1)}`, value: 1 << i })),
];

export function BanksAreas(props: BanksAreasProps) {
  const { instrument, selectedBank, selectedArea } = props;
  const [renamingBank, setRenamingBank] = useState<number | null>(null);

  // The rename field replaces the bank button rather than sitting
  // inside it: HTML forbids a control inside a button, and while it was
  // nested the button's accessible name was whatever had been typed.
  // Committing therefore unmounts the field, so focus has to be handed
  // back or it falls to the body (Q5).
  const bankButtons = useRef(new Map<number, HTMLButtonElement>());
  const wasRenaming = useRef<number | null>(null);
  useEffect(() => {
    if (wasRenaming.current !== null && renamingBank === null) {
      bankButtons.current.get(wasRenaming.current)?.focus();
    }
    wasRenaming.current = renamingBank;
  }, [renamingBank]);

  const bankIdx = Math.min(selectedBank, instrument.banks.length - 1);
  const bank = instrument.banks[bankIdx];
  if (!bank) return null;
  const area = selectedArea === null ? null : (bank.areas[selectedArea] ?? null);

  const set = (field: string, value: number) => {
    if (selectedArea !== null) props.onSetAreaField(bankIdx, selectedArea, field, value);
  };

  const outputOptions = area
    ? OUTPUT_CHOICES.some((c) => c.value === area.output)
      ? OUTPUT_CHOICES.map((c) => c.label)
      : [...OUTPUT_CHOICES.map((c) => c.label), area.outputLabel]
    : [];

  return (
    <div className="centered">
      <div className="row" style={{ marginBottom: 12 }}>
        {instrument.banks.map((b, i) =>
          renamingBank === i ? (
            <input
              key={`bank-${String(i)}`}
              // eslint-disable-next-line jsx-a11y/no-autofocus -- rename begins typing immediately, the mockup's validated flow
              autoFocus
              defaultValue={b.name}
              aria-label="bank name"
              name="bank-name"
              onBlur={(e) => {
                props.onRenameBank(i, e.target.value.toUpperCase().slice(0, 12));
                setRenamingBank(null);
              }}
              onKeyDown={(e) => {
                if (e.key !== "Enter") return;
                // Committing hands focus back to the button that opened
                // this field, and it does so during the keydown. Enter's
                // default action would then land on that button.
                e.preventDefault();
                (e.target as HTMLInputElement).blur();
              }}
              style={{ width: 90 }}
            />
          ) : (
            <button
              key={`bank-${String(i)}`}
              ref={(node) => {
                if (node) bankButtons.current.set(i, node);
                else bankButtons.current.delete(i);
              }}
              className={i === bankIdx ? "btn primary" : "btn"}
              onClick={() => {
                props.onSelectBank(i);
              }}
              onDoubleClick={() => {
                setRenamingBank(i);
              }}
              onKeyDown={(e) => {
                // Only when the button itself holds focus, the rule the
                // table rows below already keep.
                if (e.target !== e.currentTarget) return;
                if (e.key === "F2") {
                  e.preventDefault();
                  setRenamingBank(i);
                }
              }}
            >
              {`${b.name} (${String(b.areas.length)})`}
            </button>
          ),
        )}
      </div>

      <div className="panel">
        <h2>
          Areas in {bank.name} ({bank.areas.length}/64)
        </h2>
        <table className="term" aria-label="areas">
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
            {bank.areas.map((a, i) => (
              <tr
                key={`area-${String(i)}`}
                className={selectedArea === i ? "selected" : ""}
                // Selecting a row is the only way to open the area
                // editor, so it must work without a pointer (Q5).
                tabIndex={0}
                aria-selected={selectedArea === i}
                onClick={() => {
                  props.onSelectArea(i);
                }}
                onKeyDown={(e) => {
                  // Only when the row itself holds focus. Duplicate,
                  // Delete, and the move buttons sit inside the row;
                  // their Enter must not be cancelled here.
                  if (e.target !== e.currentTarget) return;
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    props.onSelectArea(i);
                  }
                }}
                style={{ cursor: "pointer" }}
              >
                <td>{a.voiceName}</td>
                <td>
                  {noteName(a.keyLow)}..{noteName(a.keyHigh)}
                </td>
                <td>
                  {a.velLow}..{a.velHigh}
                </td>
                <td>{a.outputLabel}</td>
                <td>{a.midiChannel}</td>
                <td>{a.volume}</td>
                <td>
                  <button
                    className="btn small rowbtn"
                    aria-label={`move area ${String(i + 1)} up`}
                    disabled={i === 0}
                    onClick={(e) => {
                      e.stopPropagation();
                      props.onSwapAreas(bankIdx, i, i - 1);
                    }}
                  >
                    ↑
                  </button>
                  <button
                    className="btn small rowbtn"
                    aria-label={`move area ${String(i + 1)} down`}
                    disabled={i === bank.areas.length - 1}
                    onClick={(e) => {
                      e.stopPropagation();
                      props.onSwapAreas(bankIdx, i, i + 1);
                    }}
                  >
                    ↓
                  </button>
                  <button
                    className="btn small rowbtn"
                    aria-label={`duplicate area ${String(i + 1)}`}
                    title="duplicate for a velocity switch"
                    onClick={(e) => {
                      e.stopPropagation();
                      props.onDuplicateArea(bankIdx, i);
                    }}
                  >
                    Duplicate
                  </button>
                  <button
                    className="btn small danger rowbtn"
                    aria-label={`delete area ${String(i + 1)}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      props.onDeleteArea(bankIdx, i);
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
          <button
            className="btn"
            disabled={instrument.voices.length === 0}
            onClick={() => {
              props.onAddArea(bankIdx);
            }}
          >
            Add area
          </button>
        </div>
      </div>

      {area && selectedArea !== null && (
        <div className="panel">
          <h2>Edit area · {area.voiceName}</h2>
          <div className="row" style={{ gap: 24 }}>
            <div className="field" style={{ alignItems: "flex-start" }}>
              <span className="fieldcaption">Key range (drag or type)</span>
              <RangeSlider
                label={`area ${String(selectedArea + 1)} key range`}
                lo={area.keyLow}
                hi={area.keyHigh}
                format={noteName}
                onChange={(lo, hi) => {
                  if (lo !== area.keyLow) set("keyLow", lo);
                  if (hi !== area.keyHigh) set("keyHigh", hi);
                }}
                onGestureBegin={props.onGestureBegin}
                onGestureCommit={props.onGestureCommit}
              />
              <div className="row">
                <Stepper
                  label="Key low"
                  value={area.keyLow}
                  min={0}
                  max={127}
                  format={noteName}
                  parse={parseNote}
                  onChange={(x) => {
                    set("keyLow", Math.min(x, area.keyHigh));
                  }}
                />
                <Stepper
                  label="Key high"
                  value={area.keyHigh}
                  min={0}
                  max={127}
                  format={noteName}
                  parse={parseNote}
                  onChange={(x) => {
                    set("keyHigh", Math.max(x, area.keyLow));
                  }}
                />
              </div>
            </div>
            <div className="field" style={{ alignItems: "flex-start" }}>
              <span className="fieldcaption">Velocity range (drag or type)</span>
              <RangeSlider
                label={`area ${String(selectedArea + 1)} velocity range`}
                lo={area.velLow}
                hi={area.velHigh}
                onChange={(lo, hi) => {
                  if (lo !== area.velLow) set("velLow", lo);
                  if (hi !== area.velHigh) set("velHigh", hi);
                }}
                onGestureBegin={props.onGestureBegin}
                onGestureCommit={props.onGestureCommit}
              />
              <div className="row">
                <Stepper
                  label="Vel low"
                  value={area.velLow}
                  min={0}
                  max={127}
                  onChange={(x) => {
                    set("velLow", Math.min(x, area.velHigh));
                  }}
                />
                <Stepper
                  label="Vel high"
                  value={area.velHigh}
                  min={0}
                  max={127}
                  onChange={(x) => {
                    set("velHigh", Math.max(x, area.velLow));
                  }}
                />
              </div>
            </div>
            <SelectControl
              label="Voice"
              value={`${String(area.voiceSlot + 1)} · ${area.voiceName}`}
              options={instrument.voices.map((v) => `${String(v.slot + 1)} · ${v.name}`)}
              onChange={(choice) => {
                const slot = Number(choice.split(" · ")[0]) - 1;
                if (!Number.isNaN(slot)) set("voiceSlot", slot);
              }}
            />
            <SelectControl
              label="Output"
              value={OUTPUT_CHOICES.find((c) => c.value === area.output)?.label ?? area.outputLabel}
              options={outputOptions}
              onChange={(choice) => {
                const found = OUTPUT_CHOICES.find((c) => c.label === choice);
                if (found) set("output", found.value);
              }}
            />
            <Stepper
              label="MIDI ch"
              value={area.midiChannel}
              min={1}
              max={16}
              onChange={(x) => {
                set("midiChannel", x);
              }}
            />
            <Stepper
              label="Volume"
              value={area.volume}
              min={0}
              max={127}
              onChange={(x) => {
                set("volume", x);
              }}
            />
            <Stepper
              label="Root"
              value={area.root}
              min={0}
              max={127}
              format={noteName}
              parse={parseNote}
              onChange={(x) => {
                set("root", x);
              }}
            />
          </div>
        </div>
      )}
    </div>
  );
}
