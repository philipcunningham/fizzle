# studio

`studio` is the Bubble Tea TUI for editing FZ-1 / FZ-10M / FZ-20M
sound material on disk.

## Launch

```sh
fizzle studio                   # use the current directory as workspace
fizzle studio ~/fz-library      # use a directory as workspace
```

`DIRECTORY` points at the workspace folder containing the `.img` /
`.fzf` / `.fzv` / `.wav` files studio will list and act on.
Omitting `DIRECTORY` uses the current working directory.
Individual files are opened from the in-TUI Workspace browser,
never from the CLI: studio is workspace-oriented by design.

Minimum terminal: 140 columns by 30 rows. Below the floor studio
suspends rendering and shows a centred resize hint until the
terminal grows.

## The four spaces

studio organises around the user's task, not the FZ's hardware
data hierarchy. The four spaces are arranged vertically from
broadest scope at the top to narrowest at the bottom:

| Space     | Scope                                                  | Reachable                                |
|-----------|--------------------------------------------------------|------------------------------------------|
| Workspace | A directory of `.img` / `.fzf` / `.fzv` / `.wav` files | Always                                   |
| Pool      | A persistent collection of voices ready for assembly   | Always                                   |
| Layout    | The in-focus disk: banks and Areas                     | Always                                   |
| Sound     | The currently selected voice, edited in detail         | Only when an Area is selected in Layout  |

`SHIFT+up` / `SHIFT+down` (or `Ctrl-P` / `Ctrl-N` for Emacs users)
move between adjacent spaces. Inside Sound, the same gestures move
between cells of Sound's 2D grid. A minimap in the top-right
corner of every space shows where you are.

### Workspace

A file browser. `.img` and `.fzf` files open as the in-focus
container (transitioning into Layout); `.fzv` files add to the
Pool; `.wav` files add to the Pool as well, with a stereo channel
prompt (Left / Right / Mix / Cancel) when the source is stereo.

### Pool

A workspace-level collection of voices the user accumulates while
browsing. Every entry is an FZ voice in the same internal
representation regardless of source (`.fzv`, `.wav`, or extracted
from a disk's bank). Entries are immutable: editing operations
target the Area's voice instance, not the pool entry.

The pool persists across container focus changes within a session
but is not persisted to disk. Pool entries the user wants to keep
must be assigned to an Area or exported via the fizzle CLI before
quitting.

### Layout

Layout edits the in-focus container. A container holds up to 8
banks; each bank holds up to 64 Areas. Each Area maps a voice to a
key range, a velocity range, an audio output assignment, a MIDI
channel, and a per-Area volume.

The `Ctrl-D` Duplicate gesture is the foundation of velocity
multi-switching: duplicate an Area, narrow the duplicate's
velocity range to a band adjacent to the source's, end up with two
Areas that play the same voice with different per-Area parameters
depending on key velocity. Duplicate Areas share the source's
audio data: the FZ voice header is cloned but the wave area is
not duplicated.

### Sound

Sound is voice-scoped editing. Entered by pressing `Enter` on a
selected Area in Layout, or by the spatial gesture from Layout.
Internally a 2D grid of subsystems by cells: DCA, DCF, LFO,
Sample, Loops rows; per-row cells expose visual representation,
stage editors (DCA/DCF), shape and depths (LFO), name / rate /
gen / root / tune / playback (Sample), and 8 loop cells (Loops).

## Key bindings

| Binding                   | Action                                                                  |
|---------------------------|-------------------------------------------------------------------------|
| `Ctrl-S`                  | Save the in-focus container. Save-as flow if untitled.                  |
| `Ctrl-Q`                  | Quit. Prompts to save if any container is dirty.                        |
| `Ctrl-Z`                  | Undo.                                                                   |
| `Ctrl-Y` / `Ctrl-Shift-Z` | Redo.                                                                   |
| `SHIFT+?`                 | Open the Help modal.                                                    |
| `Esc`                     | Dismiss the topmost modal; cancel rename.                               |
| `Space`                   | Audition the selected voice (one-shot, no envelope or filter shaping).  |
| `Enter`                   | Drill into the focused item; commit rename.                             |
| `Tab` / `Shift+Tab`       | Move focus to next / previous field within the current cell.            |
| `F2` / `r`                | Rename the focused name field.                                          |
| `Delete`                  | Destructive action on the focused item (confirms before acting).        |
| `Ctrl-D`                  | Duplicate the focused Area in Layout.                                   |
| `Ctrl-E` / `c`            | Extract the focused Area's voice to the Pool.                           |
| `i`                       | Import a Pool voice into the selected Area.                             |
| `m`                       | Move (two-press swap) the selected Area with another in the same bank.  |
| `a`                       | Edit the focused Area's key/velocity range and per-Area config.         |
| `f`                       | Edit the focused bank's bend + 3×7 controller modulation matrix.        |

## User workflows

These are the contracts studio guarantees. Each one is encoded as
an executable journey test in `pkg/studio/app/journey_test.go`.

1. **Open and edit.** Load a file (`.img` or `.fzf`), navigate to a
   voice's DCF cutoff, change the value, save. The edit lands at
   the right byte and survives reload; for `.img` the file remains
   exactly 1,310,720 bytes (FZ-1 floppy size).

2. **Compose new.** Launch untitled, populate the Pool from `.fzv`
   files plus a stereo `.wav` (the stereo channel modal fires for
   Left / Right / Mix / Cancel), assign each pool entry to a fresh
   Area in a new bank, save-as. The pool persists across the
   save; reload shows the assigned Areas.

3. **Sound-sculpt.** Drill into a voice; set a DCA stage to SUS
   and another to END; set an LFO depth; set DCF cutoff; save;
   reload. Every edit persists and the voice still parses as a
   real FZ-1 header (the SUS/END pointers stay in range 0..7).

4. **Layer (velocity multi-switch).** Select an Area; Ctrl-D
   duplicates it (a new voice slot, header cloned, audio shared);
   narrow the source's velocity range and expand the duplicate's
   to the complementary band; save; reload. Edits to either voice
   are independent of the other.

5. **Don't lose work.** Three sub-flows:
   - **Switch-while-dirty.** Opening a different container while
     the current one has unsaved edits opens a confirm modal
     (Save and switch / Discard / Cancel).
   - **Autosave.** A 30-second tick on a dirty container writes a
     single `{name}.bak` snapshot next to the source file (one
     `.bak` per container; each tick overwrites the previous).
   - **Recovery.** On open, if a `.bak` exists alongside the loaded
     file with a mod-time newer than the file itself, a confirm
     modal offers Recover (load the snapshot, mark dirty) or
     Discard (delete the `.bak`).

## Editing model

studio is always editing an in-focus container. There is no "no
container" state.

| Trigger                       | Behaviour                                                                                            |
|-------------------------------|------------------------------------------------------------------------------------------------------|
| Launch with no file argument  | An untitled in-memory container with 8 empty banks is created.                                       |
| Opening a file from Workspace | Switches the in-focus container. Unsaved edits on the previous container trigger the save-confirm flow. |
| Saving a named container      | Writes in place. Successful save deletes any autosave `.bak`.                                        |
| Saving an untitled container  | Save-as: prompts for a filename in the workspace directory. Default extension is `.img`.             |
| Quitting with unsaved edits   | Prompts to save, discard, or cancel.                                                                 |

Only one container is in focus at a time. The voice pool persists
across container switches; the undo / redo stacks are per-container
and survive switching away and back as long as the container has
not been closed.

## Package layout

```
pkg/studio/
├── app/                Top-level tea.Model orchestration. Routes
│                        actions to spaces, manages the modal stack
│                        (confirm, help, save-as, area editor,
│                        effects editor), runs save / autosave /
│                        recovery, hosts the journey tests.
├── audio/              Audition path: decode a voice's samples and
│                        play at a chosen MIDI pitch via oto, one
│                        in-flight playback at a time. Owner-
│                        identity guard for the natural-completion-
│                        vs-restart race.
├── clock/              The tea.Tick seam. Production wires
│                        tea.Tick; tests inject a fakeClock that
│                        records ticks and fires them on demand.
├── loader/             Reads .img / .fzf into a model.Model plus a
│                        ContainerInfo summary the App displays.
├── model/              In-memory container bytes plus the undo /
│                        redo stacks and dirty flag.
├── nav/                Navigation Action enum plus the keymap that
│                        produces actions from key events.
├── spaces/             One sub-package per space: workspace, pool,
│                        layout, sound. Each owns its own Apply
│                        method and View.
├── theme/              The lipgloss palette. Every widget imports
│                        from here.
└── widgets/            Reusable UI pieces: minimap, status,
                        toast, hint, help, confirm, areaeditor,
                        effectseditor, envelopevisual, lfovisual,
                        samplevisual, topbar.
```

CLI wiring: `cmd/fizzle/studio_cmd.go` registers
`fizzle studio [DIRECTORY]` and dispatches to
`pkg/studio/app.Run`.

## Testing

studio's tests sit at the three layers fizzle uses everywhere else
(see [CONTRIBUTING.md](../../CONTRIBUTING.md#test-layers)), with
two studio-specific surfaces.

**Layer 1: unit tests.** Per-package tests covering individual
field patchers, validation helpers, navigation clamping, encode /
decode math. Property tests via `rapid` for state-machine
invariants in `widgets/areaeditor`, `widgets/effectseditor`,
`model`, and `spaces/sound`. The headline property is "every patch
returned by every editor field keeps `disk.IsActiveOrEmptyVoiceSlot`
true". That is the load-bearing data-integrity invariant.

**Layer 2: package integration tests.** Round-trip tests that load
a real fixture, drive edits through `Update`, `Save`, reload, and
compare bytes. Lives in `app/save_test.go`,
`spaces/sound/roundtrip_test.go`, and `app/journey_test.go` (the
end-to-end user journeys).

**Layer 3: corpus-driven appearance net.** `app/corpus_test.go`
walks every fixture in `testdata/corpus/` and `testdata/synthetic/`
(232 files), loads each, runs a fixed navigation script
(Workspace, Pool, Layout-banks, Layout-areas, Sound), and asserts
(no panic, loader determinism, view non-empty, no rendered line
exceeds terminal width + slack). Two curated snapshots gate
appearance regressions: `Drums.fzf` Layout bank list at 140x30 and
the too-small hint at 139x29.

**Fuzz.** `loader/fuzz_test.go` drives `LoadContainer` with
arbitrary bytes saved under .fzf / .img / .txt extensions. The
contract is "never panic."

### Snapshot review discipline

Visual snapshots (especially anything containing braille / dense
ASCII fingerprints such as envelope, LFO, sample waveform) are not
eyeball-reviewable. When one of these changes:

- **Don't blind-rebless.** `UPDATE_SNAPS=true make test` rewrites
  every snapshot in the repo; running it without thinking turns
  the snapshot layer into noise.
- **Explain the diff in the PR description.** One line is enough.
  If you can't explain it, the change is unintentional and the
  test is catching a real regression.
- **Layout / list snapshots are reviewable.** Read the diff
  normally.

### Refresh procedure

```sh
UPDATE_SNAPS=true go test ./pkg/studio/...
```

Then `git diff testdata/snapshots/ pkg/studio/**/__snapshots__/`
and inspect every changed file. If a diff is unexpected, revert
the change and investigate before re-running.

### Determinism preconditions

For snapshot tests to be refresh-stable, all of these must hold:

1. Fixed terminal size via explicit `WindowSizeMsg`, never
   ambient.
2. ANSI codes stripped from the rendered output (the `stripANSI`
   helper in `app/snapshot_test.go`).
3. Tempdir paths stabilised (the `stabilize` / `renderView`
   helpers).
4. No timestamps in any rendered surface.
5. All randomness seeded.
6. No map-iteration-order dependence in any View.

### Deferred prerequisites

These are blockers for future snapshot work, not for the current
corpus + curated tier:

- **Animation "jump to settled" test hook.** Transitions don't
  exist yet. When they're added, build with a test hook that
  jumps any in-flight animation to its settled state.
- **LFO LCG seed.** Before snapshotting any LFO-visual cell that
  renders the random-waveform variant, the LCG must be seedable
  and the test must seed it.
- **App body width overhead.** `corpus_test.go` allows
  `viewWidthSlack = 2` cells past the requested terminal width
  because the body composition consistently overflows by 2 cells.
  Drive that slack to zero by fixing the body layout to honour
  width exactly.
