// Root shell: top bar, file listing, editor tabs, keyboard strip,
// status bar, journey guide, and dialogs. With a disk open the whole
// window is a drop target.

import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import { useEffect, useRef, useState } from "react";
import { auditionStart } from "./audio";
import { CapacityBar } from "./components/CapacityBar";
import { Keyboard } from "./components/Keyboard";
import { clamp, formatBytes, noteName } from "./data/model";
import { JOURNEYS } from "./data/journeys";
import { Dialogs } from "./dialogs/Dialogs";
import { routeFiles } from "./drop";
import { JourneyGuide } from "./journeys/JourneyGuide";
import { BanksAreas } from "./screens/BanksAreas";
import { EffectsScreen } from "./screens/EffectsScreen";
import { Inventory } from "./screens/Inventory";
import { StartScreen, fakeDrop } from "./screens/StartScreen";
import { VoiceEditor } from "./screens/VoiceEditor";
import { useOpenInstrument, useStore, type Tab } from "./state/store";

const TABS: { id: Tab; label: string }[] = [
  { id: "voices", label: "Voices" },
  { id: "banks", label: "Banks and Areas" },
  { id: "effects", label: "Effects" },
];

const KEYBOARD_OCTAVES = 6;
const KEYBOARD_LOW_MAX = 127 - KEYBOARD_OCTAVES * 12;

export function App() {
  const { state, dispatch } = useStore();
  const inst = useOpenInstrument();
  const [renamingDisk, setRenamingDisk] = useState(false);
  const [kbLow, setKbLow] = useState(24);
  const [filesCollapsed, setFilesCollapsed] = useState(false);
  const heldNotes = useRef(new Map<number, () => void>());

  const voice = inst?.voices.find((v) => v.id === state.selectedVoiceId) ?? inst?.voices[0] ?? null;

  // The keyboard follows what's selected: an Area (or whole bank) on the
  // Banks and Areas tab, the voice everywhere else. Audition plays the
  // focused Area's voice so what you hear matches what's lit.
  const bank = inst?.banks.find((b) => b.id === state.selectedBankId) ?? inst?.banks[0] ?? null;
  const area = bank?.areas.find((a) => a.id === state.selectedAreaId) ?? null;
  const areaVoice = area ? (inst?.voices.find((v) => v.id === area.voiceId) ?? null) : null;
  const focusVoice = state.tab === "banks" && areaVoice ? areaVoice : voice;
  const highlight =
    state.tab === "banks"
      ? area
        ? [{ lo: area.keyLo, hi: area.keyHi }]
        : (bank?.areas.map((a) => ({ lo: a.keyLo, hi: a.keyHi })) ?? null)
      : voice
        ? [{ lo: voice.keyLo, hi: voice.keyHi }]
        : null;

  // Status and success lines expire after 5 seconds; errors stay
  // until dismissed or resolved.
  useEffect(() => {
    if (!state.barMsg) return;
    const seq = state.barMsg.seq;
    const t = setTimeout(() => dispatch({ type: "clear-message", seq }), 5000);
    return () => clearTimeout(t);
  }, [state.barMsg, dispatch]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "z") {
        e.preventDefault();
        dispatch({ type: e.shiftKey ? "redo" : "undo" });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [dispatch]);

  return (
    <div
      className="app"
      onDragOver={(e) => state.doc.disk && e.preventDefault()}
      onDrop={(e) => {
        if (!state.doc.disk) return;
        e.preventDefault();
        routeFiles(e.dataTransfer, dispatch);
      }}
    >
      <header className="topbar">
        <span className="brand">
          fizzle<small>Mockup · canned data</small>
        </span>

        {state.doc.disk && (
          <>
            <span
              onDoubleClick={() => setRenamingDisk(true)}
              title="Double click to rename the disk"
              style={{ color: "var(--fz-fg-dim)", cursor: "text" }}
            >
              {renamingDisk ? (
                <input
                  autoFocus
                  defaultValue={state.doc.disk.label}
                  aria-label="disk label" name="disk-label"
                  onBlur={(e) => {
                    dispatch({ type: "rename-disk", label: e.target.value.toUpperCase().slice(0, 12) });
                    setRenamingDisk(false);
                  }}
                  onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                />
              ) : (
                `[${state.doc.disk.label}]`
              )}
            </span>
            <CapacityBar disk={state.doc.disk} alarm={state.barError !== null} />
          </>
        )}

        <span className="spacer" />

        <DropdownMenu.Root>
          <DropdownMenu.Trigger asChild>
            <button className="btn">Journeys ▾</button>
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content className="menu-content" align="end">
              {JOURNEYS.map((j) => (
                <DropdownMenu.Item key={j.id} className="menu-item" onSelect={() => dispatch({ type: "start-journey", id: j.id })}>
                  {j.title}
                </DropdownMenu.Item>
              ))}
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>

        {state.doc.disk && (
          <DropdownMenu.Root>
            <DropdownMenu.Trigger asChild>
              <button className="btn">Import ▾</button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content className="menu-content" align="end">
                <DropdownMenu.Item className="menu-item" onSelect={() => routeFiles(fakeDrop(["TOM.wav"]), dispatch)}>
                  WAV file
                </DropdownMenu.Item>
                <DropdownMenu.Item className="menu-item" onSelect={() => routeFiles(fakeDrop(["TOM.wav", "RIM.wav", "SHAKER.wav"]), dispatch)}>
                  WAV folder
                </DropdownMenu.Item>
                <DropdownMenu.Item className="menu-item" onSelect={() => routeFiles(fakeDrop(["AMBER KEYS.sfz"]), dispatch)}>
                  SFZ folder
                </DropdownMenu.Item>
                <DropdownMenu.Item className="menu-item" onSelect={() => routeFiles(fakeDrop(["STRINGS.fzf"]), dispatch)}>
                  .fzf full dump
                </DropdownMenu.Item>
                <DropdownMenu.Item className="menu-item" onSelect={() => routeFiles(fakeDrop(["PERC.fzb"]), dispatch)}>
                  .fzb bank dump
                </DropdownMenu.Item>
                <DropdownMenu.Item className="menu-item" onSelect={() => routeFiles(fakeDrop(["SUBBASS.fzv"]), dispatch)}>
                  .fzv voice dump
                </DropdownMenu.Item>
                <DropdownMenu.Item className="menu-item" onSelect={() => routeFiles(fakeDrop(["RIPPED.img"]), dispatch)}>
                  .img disk image
                </DropdownMenu.Item>
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>
        )}

        <button
          className="btn"
          onClick={() => dispatch({ type: "undo" })}
          disabled={state.past.length === 0}
          title={`Undo (${state.past.length} steps available)`}
        >
          Undo
        </button>
        <button className="btn" onClick={() => dispatch({ type: "redo" })} disabled={state.future.length === 0}>
          Redo
        </button>

        <button
          className="btn"
          onClick={() => dispatch({ type: "set-screen", screen: state.screen === "inventory" ? (state.doc.disk ? "disk" : "start") : "inventory" })}
        >
          {state.screen === "inventory" ? "Back" : "Components"}
        </button>

        {state.doc.disk && (
          <>
            {state.doc.dirty && (
              <span title="Unexported changes" style={{ color: "var(--fz-warning)" }}>
                ●
              </span>
            )}
            <button className="btn primary" onClick={() => dispatch({ type: "export" })}>
              Export
            </button>
            <button
              className="btn"
              onClick={() =>
                state.doc.dirty
                  ? dispatch({ type: "open-dialog", dialog: { kind: "switchDisk", intent: "close" } })
                  : dispatch({ type: "close-disk" })
              }
            >
              Close disk
            </button>
          </>
        )}
      </header>

      <div className="main">
        {state.screen === "inventory" ? (
          <Inventory />
        ) : state.screen === "start" || !state.doc.disk ? (
          <StartScreen />
        ) : (
          <>
            <nav className={filesCollapsed ? "sidebar collapsed" : "sidebar"} aria-label="disk files">
              {filesCollapsed ? (
                <span
                  className="raillabel"
                  role="button"
                  tabIndex={0}
                  style={{ cursor: "pointer" }}
                  onClick={() => setFilesCollapsed(false)}
                  onKeyDown={(e) => e.key === "Enter" && setFilesCollapsed(false)}
                >
                  Files ({state.doc.disk.files.length})
                </span>
              ) : (
                <>
              <h2>Disk files</h2>
              <div className="filelist">
                {state.doc.disk.files.map((f) => (
                  <button
                    key={f.id}
                    className={f.instrumentId && f.instrumentId === state.doc.openInstrumentId ? "filerow selected" : "filerow"}
                    onClick={() =>
                      f.instrumentId
                        ? dispatch({ type: "open-instrument", id: f.instrumentId })
                        : dispatch({ type: "file-actions", fileId: f.id })
                    }
                    onContextMenu={(e) => {
                      e.preventDefault();
                      dispatch({ type: "open-dialog", dialog: { kind: "confirmDelete", fileId: f.id, name: f.name } });
                    }}
                    title={f.instrumentId ? "Click to open for editing · right click to delete" : "Click for actions · right click to delete"}
                  >
                    <span className={`ftype ftype-${f.type}`}>{f.type}</span>
                    {/* The full dump row leads with the instrument's own
                        name; the fixed firmware file name reads as system
                        furniture, not as the thing the user edits. */}
                    {f.instrumentId ? (
                      <span className="fname">
                        {state.doc.instruments[f.instrumentId]?.name ?? f.name}
                        <small>{f.name}</small>
                      </span>
                    ) : (
                      <span className="fname">{f.name}</span>
                    )}
                    <span className="fsize">{formatBytes(f.sizeBytes)}</span>
                    <span className="fglyph">{f.instrumentId ? "\u25B8" : "\u22EF"}</span>
                  </button>
                ))}
              </div>
              {!state.doc.disk.files.some((f) => f.type === "full") && (
                <div style={{ padding: 8 }}>
                  <button className="btn small" onClick={() => dispatch({ type: "new-instrument" })}>
                    New empty instrument
                  </button>
                </div>
              )}
              <div style={{ padding: "2px 10px", color: "var(--fz-fg-faint)", fontSize: 11 }}>
                {state.doc.disk.files.length} files ·{" "}
                {Object.values(state.doc.instruments).reduce((n, i) => n + i.voices.length, 0)} voices
              </div>
                </>
              )}
              <button
                className="btn small ghost railtoggle"
                onClick={() => setFilesCollapsed(!filesCollapsed)}
                aria-label={filesCollapsed ? "expand file list" : "collapse file list"}
                title={filesCollapsed ? "Expand the file list" : "Collapse the file list to a rail"}
              >
                {filesCollapsed ? "»" : "« Collapse"}
              </button>
            </nav>

            <div className="content">
              <div className="tabs" role="tablist">
                {TABS.map((t) => (
                  <button
                    key={t.id}
                    role="tab"
                    aria-selected={state.tab === t.id}
                    className={state.tab === t.id ? "tab active" : "tab"}
                    onClick={() => dispatch({ type: "set-tab", tab: t.id })}
                  >
                    {t.label}
                  </button>
                ))}
                {inst && <span style={{ marginLeft: "auto", color: "var(--fz-fg-faint)", padding: "4px 8px" }}>Instrument: {inst.name}</span>}
              </div>
              <div className="tabbody">
                {state.tab === "voices" && <VoiceEditor />}
                {state.tab === "banks" && <BanksAreas />}
                {state.tab === "effects" && <EffectsScreen />}
              </div>

              {inst && (
              <div className="keyboardbar">
                <div className="field">
                  <button
                    className="btn small"
                    aria-label="octave down"
                    disabled={kbLow <= 0}
                    onClick={() => setKbLow(clamp(kbLow - 12, 0, KEYBOARD_LOW_MAX))}
                  >
                    - oct
                  </button>
                  <button
                    className="btn small"
                    aria-label="octave up"
                    disabled={kbLow >= KEYBOARD_LOW_MAX}
                    onClick={() => setKbLow(clamp(kbLow + 12, 0, KEYBOARD_LOW_MAX))}
                  >
                    + oct
                  </button>
                  <label>{noteName(kbLow)} up</label>
                  {voice && voice.keyHi < kbLow && <label style={{ color: "var(--fz-warning)" }}>range below view</label>}
                  {voice && voice.keyLo > kbLow + KEYBOARD_OCTAVES * 12 - 1 && (
                    <label style={{ color: "var(--fz-warning)" }}>range above view</label>
                  )}
                </div>
                <Keyboard
                  lowNote={kbLow}
                  octaves={KEYBOARD_OCTAVES}
                  highlight={highlight}
                  rootKey={focusVoice?.rootKey ?? null}
                  onNoteOn={(note, velocity) => {
                    if (!focusVoice) return;
                    heldNotes.current.get(note)?.();
                    const release = auditionStart(focusVoice, note, velocity);
                    if (release) heldNotes.current.set(note, release);
                    dispatch({ type: "audition", note, velocity });
                  }}
                  onNoteOff={(note) => {
                    heldNotes.current.get(note)?.();
                    heldNotes.current.delete(note);
                  }}
                />
              </div>
              )}
            </div>
          </>
        )}
        <JourneyGuide />
      </div>

      <footer className="statusbar">
        <span className="mock">Mockup</span>
        {/* The feedback channel: console-style last action lines. An
            unresolved error keeps its own slot, so later status lines
            never bury it. Each message pulses the bar once. */}
        {state.barError && (
          <span key={`e${state.barError.seq}`} className="barmsg error" role="alert">
            {state.barError.text}
            <button className="bardismiss" aria-label="dismiss error" onClick={() => dispatch({ type: "dismiss-error" })}>
              dismiss
            </button>
          </span>
        )}
        {state.barMsg && (
          <span key={`m${state.barMsg.seq}`} className={`barmsg ${state.barMsg.kind}`} role="status">
            {state.barMsg.text}
          </span>
        )}
      </footer>

      <Dialogs />
    </div>
  );
}
