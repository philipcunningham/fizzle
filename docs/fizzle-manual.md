# fizzle manual

This is the reference for `fizzle`, a desktop tool for the Casio FZ-1, FZ-10M, and FZ-20M samplers. Each chapter covers a concept of the instrument and the fizzle commands that operate on it, with the matching front-panel menu path on the sampler whenever one exists.

For a copy-paste quickstart, see [README.md](../README.md). For byte-level details about the binary format, see [casio-fz1-format.md](casio-fz1-format.md).

Front-panel menu paths in this document are written as `MENU/SUBMENU/PAGE`.

---

## Contents

- [Preface](#preface)
- [Concepts](#concepts)
- [The FZ-1 at a glance](#the-fz-1-at-a-glance)
- [Working with disk images (`disk`)](#working-with-disk-images-disk)
- [Working with voices (`fzv`)](#working-with-voices-fzv)
- [Working with full dumps (`fzf`)](#working-with-full-dumps-fzf)
- [Voice parameters in depth](#voice-parameters-in-depth)
  - [DCA envelope](#dca-envelope)
  - [DCF envelope and filter](#dcf-envelope-and-filter)
  - [LFO](#lfo)
  - [Modulation routing](#modulation-routing)
  - [Loops and playback modes](#loops-and-playback-modes)
  - [Tuning and voice naming](#tuning-and-voice-naming)
- [Outputs and polyphony](#outputs-and-polyphony)
- [MIDI and Area Mode](#midi-and-area-mode)
- [Converting from SFZ](#converting-from-sfz)
- [The interactive studio](#the-interactive-studio)
- [Quickstart walkthroughs](#quickstart-walkthroughs)
- [Glossary](#glossary)
- [Command index](#command-index)
- [Appendices](#appendices)

---

## Preface

`fizzle` is a command-line tool. It prepares disk images, voice files, and full dumps that the sampler reads from a floppy or floppy emulator (such as a Gotek). It does not emulate the FZ-1; the hardware is still required.

This manual is the authoritative description of what fizzle does, what each flag means, and where on the sampler each parameter shows up. The README is the on-ramp; this is the reference.

**Conventions used here:**

- `MENU/SUBMENU/PAGE` indicates a front-panel navigation path on the sampler. For example, `MODIFY/VOICE EDIT/CREATE VOICE/LFO SET` is the LFO SET page reached through MODIFY then VOICE EDIT then CREATE VOICE.
- A boxed "On the FZ" callout appears alongside each parameter that has a documented hardware location.
- Flag tables list every option with its valid range, default, and what it controls.
- Sample-byte values where they appear are the bytes written to disk. Display values are what the sampler's front panel shows. The mapping is documented in the [Display values vs stored bytes](#display-values-vs-stored-bytes) section of Concepts, with formulae in [casio-fz1-format.md](casio-fz1-format.md#hardware-display-scale).
- Where a voice parameter has no documented hardware location, this manual says so explicitly.

---

## Concepts

The minimum vocabulary needed to read the rest of this document.

### Voice

A single sample plus the parameters that govern how the sampler plays it: key range, root key, sample rate, DCA envelope, DCF envelope and filter, LFO, loop points, modulation routing, name. A voice file on disk has the extension `.fzv`.

### Full dump

Up to 64 voices packed together with a bank mapping over the top. The mapping determines which keys trigger which voice, what each voice's MIDI receive channel is, what physical output it routes to, and what its output level is. A full dump on disk has the extension `.fzf`. The firmware identifies full dumps by the on-disk name `FULL-DATA-FZ`.

### Bank

A logical grouping of voices inside a full dump. Most dumps have one bank. Real hardware dumps may contain up to 8. `fizzle` reads multi-bank dumps; use `fzf unpack --bank N` to extract a single bank's voices.

### Disk image

A `.img` file that mirrors the byte layout of an FZ-series 3.5" floppy: 1,310,720 bytes, 1,280 logical sectors, 1.25 MB usable. Copy a disk image to a Gotek or floppy emulator (or write to a real floppy with `dd`) and the sampler reads it like an original disk.

### Sample rate

The sampler runs at one of three sample rates: 36 kHz, 18 kHz, or 9 kHz. Higher rates sound better; lower rates fit more audio per disk and use less of the sampler's 2 MB sample RAM. Voices store a rate index (0, 1, or 2) in their header.

### Generator

The FZ-1 has 8 voice generators. Each note-on engages one generator. When all 8 are busy, the oldest note is stolen. Each generator also has its own physical output jack on the back panel, labelled 1 through 8. Generator assignment is controlled per voice via a bitmask (`gchn`), labelled OUTPUT on the front panel.

### Polyphony

The maximum number of notes that can sound at once. The hardware is 8-note polyphonic. A voice's polyphony in practice is limited by how many generators it is assigned to: a voice with all 8 generators is fully polyphonic, a voice with one generator is monophonic.

### Mute group

Two voices sharing the same single-generator output mute each other: a new note on either voice cuts off whatever the other was playing. This is the FZ-1's mechanism for things like hat chokes and monophonic basses. SFZ's `mutegroup=N` opcode translates to this assignment during `sfz convert`.

### Area Mode

When voices in a full dump have different MIDI receive channels, the sampler operates multitimbrally: each voice listens on its own channel, and pitch bend, expression, and other CCs affect only the voices on the matching channel. The sampler calls this Area Mode.

> **On the FZ:** `MAIN MENU/EFFECT/MIDI/MIDI FUNCTION` and set `RECEIVE` to `AREA` (default is `BASIC`).

Set per-voice channels with [`fzf midi`](#fzf-midi).

### Display values vs stored bytes

The sampler's front panel shows envelope rates and stop levels on a 0-to-99 scale. The actual bytes in the file are 0-to-127 (rates) or 0-to-255 (stop levels). `fizzle fzv edit --dca-rate-1 99` takes the display value: what you type matches what the panel shows. fizzle does the byte conversion internally. The conversion formulae are documented in [casio-fz1-format.md](casio-fz1-format.md#hardware-display-scale).

---

## The FZ-1 at a glance

The sampler's top-level mode hierarchy, abbreviated. fizzle commands map back to these locations throughout the rest of this document.

- **MAIN MENU**: top-level. Entry point for playing back voices, choosing banks, configuring effects and MIDI.
  - `EFFECT/MIDI`: pitch bend range, modulation routing, MIDI receive mode (BASIC or AREA), MIDI channels.
- **MODIFY**: editing of voices, banks, and effects.
  - `VOICE EDIT`: per-voice parameters.
    - `DEFINE VOICE/VOICE NAME`: 12-character name.
    - `KEYBOARD SET`: low key, original key, high key.
    - `CREATE VOICE`: the synthesis parameters.
      - `DCA ENVELOPE`: 8-stage amplitude envelope.
      - `DCF ENVELOPE`: 8-stage filter envelope, plus cutoff and resonance.
      - `LFO SET`: LFO type, sync, delay, rate, and depths.
      - `VELOCITY SENS`: velocity-to-level, velocity-to-rate, velocity-to-resonance.
      - `LOOP SET`: loop points, crossfade, loop times.
      - `TUNE/MEM READ`: cent tuning and sound type selection.
  - `BANK EDIT/CREATE BANK`: per-voice bank settings inside a full dump (key range, velocity range, volume, MIDI channel, output assignment).
  - `EFFECT/MIDI/BEND RANGE`, `MOD WHEEL`, `AFTER TOUCH`: depths for performance-controller routing into the synthesis engine.

The actual button labels and screen contents are described in the FZ-1 owner's manual and the Casio R&D structure document at [casio-fz1-data-structures.pdf](casio-fz1-data-structures.pdf).

---

## Working with disk images (`disk`)

A disk image is a `.img` file. `fizzle` creates, lists, and modifies these files; copy the result to a Gotek (or real floppy) and the sampler treats it like a manufactured disk.

The `disk` family does not have a direct front-panel analogue beyond the sampler's own DIR / COPY / SAVE / LOAD menus. The closest equivalent operations on the device are the disk save/load functions accessible from the main menu after inserting a floppy.

### `fizzle disk new`

```
fizzle disk new LABEL IMAGE
```

Create a blank, formatted 1.25 MB disk image. `LABEL` is the 12-character name shown on the sampler when the disk is loaded. `IMAGE` is the `.img` file path to write.

If `IMAGE` already exists, fizzle logs a warning and overwrites it.

### `fizzle disk ls`

```
fizzle disk ls IMAGE [--json]
```

List the disk label, all files on the disk, and the free space remaining.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--json` | bool | false | Emit JSON instead of a formatted table. |

### `fizzle disk add`

```
fizzle disk add [--disk-num N] IMAGE FILE
```

Add a voice (`.fzv`), full dump (`.fzf`), or expanded-software program (`.bin`) file to a disk image. The file type is detected automatically from the contents of `FILE`. Program binaries are recognised by the standard 14-byte FZ-1 preamble; their on-disk directory name is derived from the input filename basename (uppercased, truncated to 12 characters).

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--disk-num` | uint | 1 | Which disk in a multi-disk set (1 for the first or only disk, 2 for the second). |

### `fizzle disk get`

```
fizzle disk get IMAGE NAME OUTPUT
```

Extract a file from a disk image by its on-disk name. `NAME` is matched case-insensitively. Use `disk ls` first to see what names are present. Full dumps are always named `FULL-DATA-FZ`.

### `fizzle disk copy`

```
fizzle disk copy SRC-IMAGE NAME DEST-IMAGE
```

Copy a named file from one disk image to another. Equivalent to `disk get` followed by `disk add` but in one step.

---

## Working with voices (`fzv`)

A voice file (`.fzv`) holds a single sample plus its synthesis parameters. The hardware equivalent operations all live under `MODIFY/VOICE EDIT` on the device.

### `fzv info`

```
fizzle fzv info FZV [--json]
```

Show the parameters stored in a voice file: rate, length, duration, key range, root key, filter, DCA and DCF envelopes, LFO, loop configuration.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--json` | bool | false | Emit JSON instead of formatted text. |

### `fzv import`

```
fizzle fzv import [--rate N] WAV FZV
```

Convert a mono PCM WAV file (16, 24, or 32-bit) into a voice file. The WAV is resampled to the target rate if it does not already match. Stereo WAVs are rejected; the FZ-1 is mono.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--rate` | int | 36000 | Target sample rate. Must be 36000, 18000, or 9000. |

The resulting voice has its sound type set to Normal (one of the five playback modes the FZ supports; see [Loops and playback modes](#loops-and-playback-modes)). The DCA envelope is set to instant attack at full level and a moderate release; the DCF is set fully open. Velocity sensitivity is set to a usable default so notes respond to MIDI velocity. The voice's MIDI key range is C2 to C7 with root C5; change this with `fzv edit --key-low`, `--key-high`, and `--root`.

### `fzv extract`

```
fizzle fzv extract FZV WAV
```

Decode the audio out of a voice file as a 16-bit mono WAV. Only the generator range (`genst` to `gened`) is exported, matching what the sampler plays on note-on.

### `fzv play`

```
fizzle fzv play FZV
```

Play the voice's audio once through the system audio device. Uses native audio on macOS and Windows; on Linux, requires `aplay`, `paplay`, or `ffplay` to be installed.

### `fzv edit`

```
fizzle fzv edit FZV [flags]
```

Modify voice parameters in place. Only specified flags are changed; everything else is preserved. See [Voice parameters in depth](#voice-parameters-in-depth) for what each flag controls and where the parameter lives on the sampler.

---

## Working with full dumps (`fzf`)

A full dump (`.fzf`) packs up to 64 voices with a bank mapping. It loads onto the sampler in a single operation.

### `fzf info`

```
fizzle fzf info FZF [--json]
```

Show the voice map: index, name, root note, key range, MIDI channel, output assignment, velocity range, sample rate, duration, loop markers.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--json` | bool | false | Emit JSON. |

### `fzf build`

```
fizzle fzf build OUTPUT VOICE [VOICE ...]
```

Pack one or more voice files into a full dump. Up to 64 voices. The order matters: each voice keeps its own key range, but voices appear in the file in the order given.

### `fzf unpack`

```
fizzle fzf unpack FZF OUTPUTDIR
fizzle fzf unpack DISK1.img --disk2 DISK2.img OUTPUTDIR
fizzle fzf unpack FZF --bank N OUTPUTDIR
```

Extract all voices from a full dump into individual `.fzv` files. The output directory is created if it does not exist. Voice files are named after the voice slot name; duplicate names get a numeric suffix.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--disk2` | string | (none) | Path to the disk-2 `.img` for a 2-disk split. When set, the first positional argument is treated as the disk-1 `.img`. |
| `--bank` | int | 0 | Extract only voices from the given bank (1-based). 0 means all banks. Only meaningful for multi-bank dumps. |

### `fzf midi`

```
fizzle fzf midi FZF --voice NAME --channel N
fizzle fzf midi FZF --all --channel N
```

Set the MIDI receive channel for one or more voices. Use `fzf info` first to see voice names. Voice matching is case-insensitive.

> **On the FZ:** the per-voice MIDI channel lives at `MODIFY/BANK EDIT/CREATE BANK` (the `MidiCh` field). For the sampler to actually route per-voice, enable Area Mode at `MAIN MENU/EFFECT/MIDI/MIDI FUNCTION` by setting `RECEIVE = AREA`. See [MIDI and Area Mode](#midi-and-area-mode).

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--voice` | string (repeatable) | (none) | Voice name to target. Repeatable for several voices in one call. |
| `--all` | bool | false | Target every voice. Mutually exclusive with `--voice`. |
| `--channel` | int | (required) | MIDI receive channel, 1 to 16. |

### `fzf output`

```
fizzle fzf output FZF --voice NAME --output VALUE
fizzle fzf output FZF --all --output VALUE
```

Set the output (generator channel) assignment for one or more voices. Values are output numbers 1 to 8, a comma-separated list (e.g. `1,3,5`), or `all`.

> **On the FZ:** the per-voice output assignment lives at `MODIFY/BANK EDIT/CREATE BANK` (the `Output assignment` field). The front-panel display labels these 8 positions OUTPUT; a filled circle means assigned, a dot means inactive.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--voice` | string (repeatable) | (none) | Voice name to target. |
| `--all` | bool | false | Target every voice. Mutually exclusive with `--voice`. |
| `--output` | string | (required) | `1`-`8` (single output), `1,3` (multiple), or `all` (every output). |

A voice on a single output is monophonic on that output. Voices sharing the same single output mute each other (the mute-group mechanism; see [Mute group](#mute-group) and [Outputs and polyphony](#outputs-and-polyphony)).

### `fzf effects`

```
fizzle fzf effects FZF
fizzle fzf effects FZF [--bend N] [--mod-lfp N] [--foot-dca N] [--aftertouch-lfp N]
```

View or modify the global effect block: how performance controllers (pitch bend, mod wheel, foot pedal, aftertouch) route to the synthesis engine. The block is per-bank; fizzle targets bank 0 (the first and usually only bank). With no flags, the current values are printed.

> **On the FZ:** `MODIFY/EFFECT/MIDI/BEND RANGE`, `MOD WHEEL`, `AFTER TOUCH`. The FZ-1 also accepts a Casio VP-2 foot pedal; FZ-10M/FZ-20M have sustain pedal only.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--bend` | int | (unchanged) | Pitch-bend range in 1/8-semitone units. 24 = 3 semitones, 48 = 6. Range 0 to 127. |
| `--mod-lfp` | int | (unchanged) | Mod wheel to LFO pitch depth. Range 0 to 127. |
| `--foot-dca` | int | (unchanged) | Foot pedal to DCA (volume) depth. Range 0 to 127. |
| `--aftertouch-lfp` | int | (unchanged) | Aftertouch to LFO pitch depth. Range 0 to 127. |

Out-of-range values for the three depth fields are not currently independently verified on hardware; the defaults (`bend=24`, `mod-lfp=15`, `foot-dca=64`, `aftertouch-lfp=8`) are confirmed to load and play correctly.

### `fzf edit`

```
fizzle fzf edit FZF --voice NAME [flags]
```

Modify a voice inside a full dump. The voice is identified by `--voice` (case-insensitive). Only specified flags are changed; everything else is preserved. The same flag set as [`fzv edit`](#fzv-edit) applies. See [Voice parameters in depth](#voice-parameters-in-depth) for what each flag does.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--voice` | string | (required) | Target voice name. |

---

## Working with bank dumps (`fzb`)

A bank dump (`.fzb`) holds a bank sector and its voice headers but no audio. Bank dumps are uncommon in practice; most users only encounter the full dump (`.fzf`) which carries audio as well. See the [glossary entry](#glossary) for the distinction.

### `fzb info`

```
fizzle fzb info FZB
fizzle fzb info FZB --json
```

Show the voice map of a bank dump: each voice's name, key range, root note, MIDI channel, and generator (output) assignment. With `--json`, the same data is emitted as structured JSON for scripting.

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--json` | bool | false | Emit JSON instead of the table. |

There is currently no `fzb build`, `fzb unpack`, or `fzb edit`: bank dumps are inspect-only in fizzle.

---

## Voice parameters in depth

This is the heart of the reference. Each subsection describes one cluster of parameters: what it does, the front-panel page on the sampler, and the fizzle flags that change it.

### DCA envelope

The DCA (Digitally Controlled Amplifier) envelope controls amplitude over time. It has 8 stages, each with a rate (how fast the envelope moves through that stage) and a stop level (where the stage finishes). A sustain point and an end point determine where the envelope holds when a key is held and where it ends after release.

> **On the FZ:** `MODIFY/VOICE EDIT/CREATE VOICE/DCA ENVELOPE`. The front panel displays rates and stop levels on a 0-to-99 scale.

Stages 0 through `sustain` run on note-on. The envelope holds at `sustain` while the key is held. On note release, stages `sustain+1` through `end` run.

**Flags on `fzv edit` / `fzf edit`:**

| Flag | Range | Notes |
|------|-------|-------|
| `--dca-sustain` | 0-7 | Stage number to hold at while the key is down. |
| `--dca-end` | 0-7 | Final stage to traverse on release. |
| `--dca-rate-N` (N=1..8) | 0-99 | Rate for stage N, on the hardware display scale. The envelope direction sign bit is preserved automatically. |
| `--dca-stop-N` (N=1..8) | 0-99 | Stop level for stage N, on the hardware display scale. |

The display values map to internal byte values (0-127 for rates, 0-255 for stop levels) automatically. Values you type match what the sampler's front panel shows.

### DCF envelope and filter

The DCF (Digitally Controlled Filter) is the filter envelope plus a fixed cutoff offset and resonance. The envelope has the same 8-stage structure as the DCA. The cutoff offset and resonance are constants applied to the filter regardless of envelope state.

> **On the FZ:** `MODIFY/VOICE EDIT/CREATE VOICE/DCF ENVELOPE`. Cutoff and resonance are on the same page as the envelope.

**Flags:**

| Flag | Range | Notes |
|------|-------|-------|
| `--cutoff` | 0-127 | Cutoff offset added to the filter envelope output. 127 means fully open (no audible filtering). |
| `--resonance` | 0-127 | Filter resonance. Older Casio documentation describes resonance as the upper nibble only; in practice the hardware uses the entire byte. See [casio-fz1-format.md](casio-fz1-format.md#real-world-findings) for the verification detail. |
| `--dcf-sustain` | 0-7 | Sustain stage for the filter envelope. |
| `--dcf-end` | 0-7 | End stage for the filter envelope. |
| `--dcf-rate-N` (N=1..8) | 0-99 | Stage rate, display scale. |
| `--dcf-stop-N` (N=1..8) | 0-99 | Stage stop level, display scale. |

### LFO

A single low-frequency oscillator that can modulate pitch, amplitude, filter cutoff, and filter resonance. Choose a waveform, a rate, a delay, and per-destination depths.

> **On the FZ:** `MODIFY/VOICE EDIT/CREATE VOICE/LFO SET`. The page lists Type, Sync, Delay, Rate, and depth settings.

**Flags:**

| Flag | Range / values | Notes |
|------|----------------|-------|
| `--lfo-wave` | `sine`, `saw-up`, `saw-down`, `triangle`, `rectangle`, `random` | LFO waveform. The sampler labels these Sin, AscSaw, DecSaw, Tri, Square, Rand. |
| `--lfo-rate` | 0-127 | Oscillation rate. |
| `--lfo-delay` | 0-65535 | Delay between note-on and LFO onset, in 2ms units. |
| `--lfo-pitch` | 0-127 | Depth of LFO into voice pitch. |
| `--lfo-amp` | 0-127 | Depth of LFO into amplitude (DCA). |
| `--lfo-filter` | 0-127 | Depth of LFO into filter cutoff (DCF). |
| `--lfo-attack` | 0-127 | LFO attack rate. Not visible on the FZ front panel and not described in the owner's manual. |
| `--lfo-q` | 0-127 | Depth of LFO into filter resonance. Not visible on the FZ front panel and not described in the owner's manual. |

`--lfo-wave` carries a phase-sync flag internally (bit 7 of the byte that names the waveform). fizzle does not currently expose phase sync as a CLI flag.

### Modulation routing

Per-voice routing of MIDI key and velocity to envelope behaviour and other parameters.

> **On the FZ:** velocity sensitivity lives at `MODIFY/VOICE EDIT/CREATE VOICE/VELOCITY SENS`. Key-follow parameters are part of the envelope pages.

**Flags:**

| Flag | Range | Notes |
|------|-------|-------|
| `--dca-level-kf` | -15 to +15 | Key follow into amplitude level. Positive values brighten higher keys. |
| `--dca-rate-kf` | -15 to +15 | Key follow into amplitude envelope rate. |
| `--dcf-level-kf` | -15 to +15 | Key follow into filter cutoff. |
| `--dcf-rate-kf` | -15 to +15 | Key follow into filter envelope rate. |
| `--vel-dca-kf` | 0-127 | Velocity into amplitude. A value of zero means velocity does not affect volume. fizzle's defaults set this to 80 to give normal velocity response. |
| `--vel-dcf-kf` | 0-127 | Velocity into filter. |

### Loops and playback modes

Voices have 8 loop slots and a playback mode that determines whether and how loops engage.

> **On the FZ:** loop points live at `MODIFY/VOICE EDIT/CREATE VOICE/LOOP SET`. The page lists Loop Stage (1 to 8), Next (TRACE or SKIP), Start offset, End offset, Cross Time, and Loop Time. The right and left arrow keys on the sampler navigate between stages. Sound type lives at `MODIFY/VOICE EDIT/CREATE VOICE/TUNE/MEM READ`.

**Playback modes** the sampler supports, stored in the `loop` field of the voice header:

| Mode | Notes |
|------|-------|
| Normal | Standard playback with loops if configured. fizzle's default for imported WAVs. |
| Reverse | Plays the sample backwards. Looping is disabled in this mode. |
| Cue | Looping is enabled; used for sustained sounds. |
| Synth | Synthesised playback; looping disabled. On the sampler, Synth-mode voices display pitch offset by minus six semitones; correction requires equal-and-opposite tuning. |
| No sound | The slot is silent. Used for unused voice slots. |

Playback mode can be changed with `fzv edit --playback-mode` (normal, reverse, cue, synth). Per-loop editing (individual loop start/end/crossfade) is not currently exposed through CLI flags. Loops carried in WAV SMPL chunks are imported automatically by `fzv import`; the loop points are scaled correctly when resampling. SFZ `loop_start` and `loop_end` opcodes override WAV loop points during conversion.

### Tuning and voice naming

> **On the FZ:** voice names are set at `MODIFY/VOICE EDIT/DEFINE VOICE/VOICE NAME`. Cent tuning lives at `MODIFY/VOICE EDIT/CREATE VOICE/TUNE/MEM READ`. The sampler displays cent tuning on a -100 to +100 scale; the internal byte covers a wider range.

**Flags:**

| Flag | Notes |
|------|-------|
| `--name` | New voice name, up to 12 ASCII characters. Names are space-padded to 12 bytes when stored. |

Tuning can be changed with `fzv edit --tune` (in DCP units: 1/256 semitone). Tuning is set to zero at import time by default. The SFZ `tune` opcode (in cents) is applied during conversion.

### Keyboard range

The sampler stores three MIDI note numbers per voice: a low key, an original (root) key, and a high key.

> **On the FZ:** `MODIFY/VOICE EDIT/KEYBOARD SET`.

In a standalone voice (`.fzv`), the range applies to the voice itself. Inside a full dump, key range lives in the bank mapping (`MODIFY/BANK EDIT/CREATE BANK`) and overrides the voice's own range. Use `fzv edit --key-low`, `--key-high`, and `--root` to change the voice-level range. For full dumps, key ranges come from the source SFZ or WAV-directory layout and can be inspected with `fzf info`.

---

## Outputs and polyphony

The FZ-1 has 8 voice generators. Each generator drives a physical output jack (1 through 8) on the back panel and contributes one note of polyphony. Generator assignment is per voice and is stored as an 8-bit bitmask (`gchn`): bit 0 corresponds to output 1, bit 7 to output 8.

> **On the FZ:** per-voice output assignment lives at `MODIFY/BANK EDIT/CREATE BANK` (the `Output assignment` field). The front-panel display has an OUTPUT row of 8 positions; filled circle means assigned, dot means inactive.

A voice with all 8 generators (`0xff`) plays back round-robin across all generators: fully polyphonic, with stolen notes when more than 8 are sounding. A voice with one generator is monophonic on that output: a new note cuts the previous. A voice with several generators (e.g. outputs 1, 3, and 5) has limited polyphony across just those outputs.

**Mute groups.** Two voices sharing the same single-generator assignment mute each other. This is how the FZ-1 implements hat chokes (closed and open hat both routed to output 1), monophonic basses (one output, never overlapping), and the SFZ `mutegroup=N` opcode. `sfz convert` allocates one generator per distinct `mutegroup` value during conversion (see [Converting from SFZ](#converting-from-sfz)).

Set assignment with [`fzf output`](#fzf-output).

---

## MIDI and Area Mode

Each voice in a full dump has a MIDI receive channel. When all voices share a channel, the sampler responds the way a single-channel synth would. When channels differ, the sampler operates multitimbrally: each voice listens on its own channel, and pitch bend, expression, mod wheel, and other CCs affect only the voices on the matching channel.

> **On the FZ:** the per-voice channel lives at `MODIFY/BANK EDIT/CREATE BANK` (the `MidiCh` field). The multitimbral routing must be enabled separately at `MAIN MENU/EFFECT/MIDI/MIDI FUNCTION` by setting `RECEIVE` from `BASIC` (default) to `AREA`. The sampler calls this Area Mode.

Set per-voice channels with [`fzf midi`](#fzf-midi).

**Performance controllers.** The FZ-1 supports four performance-controller sources: pitch bend, mod wheel, foot controller (Casio VP-2), and aftertouch. Each can be routed to LFO pitch, LFO amplitude, LFO filter, LFO resonance (Q), DCA level, DCF level, and DCF Q. Depths are global to the full dump (one set per bank), not per voice.

> **On the FZ:** `MODIFY/EFFECT/MIDI/BEND RANGE` (or `MOD WHEEL` or `AFTER TOUCH`). The FZ-1 keyboard accepts the Casio VP-2 foot pedal and sustain; FZ-10M and FZ-20M rack models support sustain pedal only.

Use `fzf effects` to view or modify the performance-controller routing. The defaults are sensible (about 3 semitones of bend, light mod wheel, moderate foot pedal, light aftertouch). See [`fzf effects`](#fzf-effects) for flags.

---

## Converting from SFZ

SFZ is an open sampler format widely supported by DAWs and sample libraries. `fizzle sfz convert` translates SFZ regions and their referenced WAV files into a full dump.

```
fizzle sfz convert [--rate N] [--fit-to-disk] [--split-disks] SFZ-OR-DIR OUTPUT
```

Pass either an SFZ file or a directory of WAV files. With a directory, the WAVs are sorted alphabetically and assigned to sequential MIDI keys starting at C2 (note 36).

| Flag | Type | Default | Notes |
|------|------|---------|-------|
| `--rate` | int | 36000 | Target sample rate. Must be 36000, 18000, or 9000. |
| `--fit-to-disk` | bool | false | If the output would exceed 1.25 MB, step the sample rate down through the ladder (36 to 18 to 9 kHz) until it fits. `--rate` sets the ceiling. |
| `--split-disks` | bool | false | Split the output across two 1.25 MB disks. Produces `OUTPUT-1.img` and `OUTPUT-2.img` directly (not `.fzf` files). The sampler loads disk 1 and prompts for disk 2 to complete the load. Mutually exclusive with `--fit-to-disk`. |

**SFZ opcode coverage.** `sfz convert` translates the following opcodes:

| SFZ opcode | Translates to |
|------------|---------------|
| `sample` | The WAV file referenced by this region |
| `lokey` | Voice low key |
| `hikey` | Voice high key |
| `key` | Shorthand: sets `lokey`, `hikey`, and `pitch_keycenter` to the same value |
| `pitch_keycenter` | Voice root key |
| `lovel` | Velocity low |
| `hivel` | Velocity high |
| `transpose` | Semitone transpose (written to the DCP pitch field) |
| `tune` | Fine tuning in cents (applied as DCP offset during conversion) |
| `mutegroup` | A single shared generator output (see [Mute group](#mute-group) and [Outputs and polyphony](#outputs-and-polyphony)) |
| `loop_mode` | `one_shot` disables sustain looping; other values are parsed but not fully mapped |
| `cutoff` | Filter cutoff offset (0 to 127; overrides the default fully-open filter) |
| `resonance` | Filter resonance (0 to 127) |
| `loop_start` | Loop start sample index (overrides WAV SMPL chunk loop points) |
| `loop_end` | Loop end sample index (overrides WAV SMPL chunk loop points) |

Opcodes outside this set are read but not applied; unsupported features are reported as warnings during conversion.

**Examples:**

```sh
# Convert an SFZ
fizzle sfz convert drums.sfz drums.fzf

# Convert a directory of WAVs assigned to sequential keys
fizzle sfz convert ./samples/ mykit.fzf

# Fit a large instrument onto one floppy
fizzle sfz convert --fit-to-disk junglism.sfz junglism.fzf

# Split a large instrument across two floppies
fizzle sfz convert --rate 36000 --split-disks junglism.sfz junglism
```

Long conversions respect Ctrl+C. The command exits cleanly without leaving a half-written file.

---

## The interactive studio

```
fizzle studio [DIRECTORY]
```

A workspace-oriented terminal editor for FZ-1 / FZ-10M / FZ-20M sound material. DIRECTORY points at the workspace folder containing `.img` / `.fzf` / `.fzv` / `.wav` files. Omitting DIRECTORY uses the current working directory. Individual files are opened from the Workspace browser inside the TUI.

```sh
fizzle studio                  # workspace = cwd
fizzle studio ~/fz-library     # workspace = a directory
```

### The four spaces

studio organises around the user's task, not the FZ's data hierarchy. Four spaces are stacked vertically, navigated with `SHIFT+up` / `SHIFT+down` (or Emacs `Ctrl-P` / `Ctrl-N`):

- **Workspace.** File browser over a directory of `.img` / `.fzf` / `.fzv` / `.wav` files. Each file is dispatched by extension: `.img` and `.fzf` open as the in-focus container, `.fzv` adds to the Pool, `.wav` adds to the Pool (with a stereo channel prompt when needed).
- **Pool.** A session-level basket of voices accumulated from the Workspace, the in-focus container's banks, or external samples. Voices persist across container switches.
- **Layout.** The in-focus container's banks and Areas. Up to 8 banks × 64 Areas each. `Ctrl+D` duplicates an Area for velocity multi-switching; `a` opens the per-Area editor (key range, velocity range, root note, audio output, volume, MIDI channel); `f` opens the per-bank effects editor (bend depth + 3×7 controller modulation matrix).
- **Sound.** Voice-scoped editing of the currently selected Area. A 2D grid of subsystems by cells: DCA, DCF, LFO, Sample, Loops. The leftmost cell of each row is a braille-rendered visualisation (envelope curve, LFO waveform, sample waveform); subsequent cells are field editors.

### Key bindings

| Key | Action |
|-----|--------|
| `SHIFT+up` / `SHIFT+down` | Move between spaces |
| Arrow keys | Cursor navigation within a space |
| `Enter` | Drill into the focused item |
| `Space` | Audition the focused voice (second press stops) |
| `Tab` / `Shift+Tab` | Move focus between fields in the current cell |
| `Ctrl+S` | Save changes to disk |
| `Ctrl+Z` / `Ctrl+Y` | Undo / redo |
| `Ctrl+C` / `Ctrl+V` | Copy / paste between compatible cells |
| `Ctrl+D` | Duplicate the focused Area in Layout |
| `Ctrl+E` / `c` | Extract the focused Area's voice into the Pool |
| `i` | Import a Pool voice into the selected Area |
| `m` | Move (two-press swap) Areas within a bank |
| `a` | Edit the focused Area (key range, velocity, root, output, volume, MIDI channel) |
| `f` | Edit the focused bank's effects (bend + modulation matrix) |
| `r` / `F2` | Rename the focused name field |
| `Delete` | Destructive action on the focused item (always confirms) |
| `?` | Contextual help |
| `Ctrl+Q` | Quit (prompts if there are unsaved changes) |
| `Esc` | Dismiss the topmost modal |

### Autosave and crash recovery

While a container is dirty, studio writes a `{name}.bak` snapshot next to the source file every 30 seconds. A successful save deletes the snapshot. On open, if a `.bak` exists that's newer than its named container, studio offers Recover (load the snapshot, mark dirty) or Discard (delete the `.bak`).

For the full specification, including the per-cell field layout, the snapshot-test discipline, and the user-journey contracts, see [`pkg/studio/README.md`](../pkg/studio/README.md).

---

## Quickstart walkthroughs

End-to-end examples. Use them as a guided introduction or as templates.

### Your first voice

You have one WAV file and want to hear it on the sampler.

```sh
# 1. Convert the WAV into a voice file
fizzle fzv import kick.wav kick.fzv

# 2. Create a blank disk image
fizzle disk new "FIRST KICK" first-kick.img

# 3. Put the voice on the disk
fizzle disk add first-kick.img kick.fzv

# 4. Confirm
fizzle disk ls first-kick.img
```

Copy `first-kick.img` to the Gotek (or a real floppy). On the sampler, load the disk and the voice will appear as `KICK`, playing at original pitch on the root key (C5, MIDI 72).

### Build a drum kit

You have a folder of WAVs.

```sh
# 1. Convert the directory to a full dump
fizzle sfz convert --fit-to-disk ./samples/ mykit.fzf

# 2. See what you got
fizzle fzf info mykit.fzf

# 3. Build the disk
fizzle disk new "MY KIT" mykit.img
fizzle disk add mykit.img mykit.fzf
```

WAVs are assigned to sequential keys from C2. `--fit-to-disk` steps the rate down (36 kHz to 18 to 9) if the output would overflow the 1.25 MB disk.

To make a group of hats monophonic (a new hat cancels the old hat), write a small SFZ and use `mutegroup=N`:

```sfz
<region>
sample=hat-closed.wav lokey=42 hikey=42 pitch_keycenter=42
mutegroup=1

<region>
sample=hat-open.wav lokey=46 hikey=46 pitch_keycenter=46
mutegroup=1
```

Both hats share a single generator; a new note on either cuts the previous one.

### Round-trip a hardware dump

You have a `.img` of a disk saved on a real FZ-1 and want to tweak one voice.

```sh
# 1. Pull the full dump off the disk image
fizzle disk get original.img FULL-DATA-FZ original.fzf

# 2. Inspect it
fizzle fzf info original.fzf

# 3. Edit one voice's filter cutoff
fizzle fzf edit original.fzf --voice "REESE" --cutoff 80

# 4. Drop the modified dump onto a fresh disk
fizzle disk new "REESE EDIT" reese-edit.img
fizzle disk add reese-edit.img original.fzf
```

Full dumps are always named `FULL-DATA-FZ` on disk; that name is how the firmware recognises them. `fzf edit` modifies the file in place. Only flags you specify change; the rest is preserved exactly.

---

## Glossary

**Area Mode.** Multitimbral MIDI routing on the FZ-1. Each voice in a full dump listens on its own MIDI channel. Enabled at `MAIN MENU/EFFECT/MIDI/MIDI FUNCTION` by setting `RECEIVE = AREA`.

**Bank.** A logical grouping of voices within a full dump. Most dumps have one bank; hardware dumps may have up to 8.

**bvol.** Bank-sector field holding the per-voice output level (0 to 127). Not currently editable through fizzle.

**Cue.** A playback mode in the sampler. Looping is enabled; suited to sustained sounds.

**DCA.** Digitally Controlled Amplifier. The amplitude envelope and its associated parameters.

**DCF.** Digitally Controlled Filter. The filter envelope, plus cutoff and resonance.

**DCP.** Pitch-control field in the voice header (`dcp`). Stores transpose in 1/256-semitone units.

**Display value.** The 0-to-99 number shown on the sampler's front panel for envelope rates and stop levels. Maps to internal bytes (0-127 for rates, 0-255 for levels) via the formulae in [casio-fz1-format.md](casio-fz1-format.md#hardware-display-scale).

**End point.** The last envelope stage traversed during note release.

**Full dump.** A file (`.fzf`) containing up to 64 voices, one or more bank sectors, and the raw audio for every voice. Named `FULL-DATA-FZ` on disk.

**FZB.** A bank dump file (file-type byte 2 on disk). Holds bank parameters and voice headers without audio. Use `fzb info` to inspect.

**FZF.** A full dump file. See Full dump.

**FZV.** A voice file. Holds one sample plus its synthesis parameters.

**gchn.** Generator channel bitmask. Per-voice byte in the bank sector; each bit assigns the voice to one of the 8 voice generators. Labelled OUTPUT on the front panel.

**gened.** Generator end. Last sample address played on note-on.

**genst.** Generator start. First sample address played on note-on.

**LFO.** Low Frequency Oscillator. A single per-voice modulator that can drive pitch, amplitude, filter cutoff, and filter resonance.

**Loop sustain (`loop_sus`).** Loop number that the voice sustains on while a key is held. A value of 8 means no sustain loop.

**Mute group.** A set of voices assigned to the same single output. New notes on any voice in the group cut off the others.

**Normal.** Standard playback mode. fizzle's default for imported WAVs.

**OUTPUT.** The front-panel label for the 8 generator-channel bits.

**Polyphony.** Maximum simultaneous notes. The FZ-1 is 8-voice polyphonic.

**Root key.** The MIDI note at which the sample plays back at its original pitch. Stored as `cent` in the voice header.

**Sample rate.** One of 36 kHz, 18 kHz, or 9 kHz on the FZ. Stored as an index (0, 1, or 2) in the voice header.

**Sustain point.** The envelope stage that holds while a key is held down.

**Voice.** A single sample plus its synthesis parameters. Stored in `.fzv` files or as one slot within a `.fzf` full dump.

---

## Command index

Every fizzle subcommand and flag, alphabetical.

### Top-level

- `--debug`: enable debug logging (global flag).
- `fizzle --version`: print the version.
- `fizzle completion bash|zsh|fish|pwsh`: emit shell completion script.

### `disk`

- [`disk add`](#fizzle-disk-add): `--disk-num`.
- [`disk copy`](#fizzle-disk-copy).
- [`disk get`](#fizzle-disk-get).
- [`disk ls`](#fizzle-disk-ls): `--json`.
- [`disk new`](#fizzle-disk-new).

### `fzv`

- [`fzv edit`](#fzv-edit): every flag from [Voice parameters in depth](#voice-parameters-in-depth).
- [`fzv extract`](#fzv-extract).
- [`fzv import`](#fzv-import): `--rate`.
- [`fzv info`](#fzv-info): `--json`.
- [`fzv play`](#fzv-play).

### `fzf`

- [`fzf build`](#fzf-build).
- [`fzf edit`](#fzf-edit): `--voice`, plus every flag from [Voice parameters in depth](#voice-parameters-in-depth).
- [`fzf effects`](#fzf-effects): `--bend`, `--mod-lfp`, `--foot-dca`, `--aftertouch-lfp`.
- [`fzf info`](#fzf-info): `--json`.
- [`fzf midi`](#fzf-midi): `--voice`, `--all`, `--channel`.
- [`fzf output`](#fzf-output): `--voice`, `--all`, `--output`.
- [`fzf unpack`](#fzf-unpack): `--disk2`, `--bank`.

### `fzb`

- [`fzb info`](#fzb-info): `--json`.

### `sfz`

- [`sfz convert`](#converting-from-sfz): `--rate`, `--fit-to-disk`, `--split-disks`.

### `studio`

- [`studio`](#the-interactive-studio).

### Voice parameter flags (used by `fzv edit` and `fzf edit`)

| Flag | Section |
|------|---------|
| `--cutoff` | [DCF envelope and filter](#dcf-envelope-and-filter) |
| `--dca-end` | [DCA envelope](#dca-envelope) |
| `--key-high` | [Keyboard range](#keyboard-range) |
| `--key-low` | [Keyboard range](#keyboard-range) |
| `--dca-level-kf` | [Modulation routing](#modulation-routing) |
| `--dca-rate-kf` | [Modulation routing](#modulation-routing) |
| `--dca-rate-N` (N=1..8) | [DCA envelope](#dca-envelope) |
| `--dca-stop-N` (N=1..8) | [DCA envelope](#dca-envelope) |
| `--dca-sustain` | [DCA envelope](#dca-envelope) |
| `--dcf-end` | [DCF envelope and filter](#dcf-envelope-and-filter) |
| `--dcf-level-kf` | [Modulation routing](#modulation-routing) |
| `--dcf-rate-kf` | [Modulation routing](#modulation-routing) |
| `--dcf-rate-N` (N=1..8) | [DCF envelope and filter](#dcf-envelope-and-filter) |
| `--dcf-stop-N` (N=1..8) | [DCF envelope and filter](#dcf-envelope-and-filter) |
| `--dcf-sustain` | [DCF envelope and filter](#dcf-envelope-and-filter) |
| `--lfo-amp` | [LFO](#lfo) |
| `--lfo-attack` | [LFO](#lfo) |
| `--lfo-delay` | [LFO](#lfo) |
| `--lfo-filter` | [LFO](#lfo) |
| `--lfo-pitch` | [LFO](#lfo) |
| `--lfo-q` | [LFO](#lfo) |
| `--lfo-rate` | [LFO](#lfo) |
| `--lfo-wave` | [LFO](#lfo) |
| `--name` | [Tuning and voice naming](#tuning-and-voice-naming) |
| `--playback-mode` | [Loops and playback modes](#loops-and-playback-modes) |
| `--resonance` | [DCF envelope and filter](#dcf-envelope-and-filter) |
| `--root` | [Keyboard range](#keyboard-range) |
| `--tune` | [Tuning and voice naming](#tuning-and-voice-naming) |
| `--vel-dca-kf` | [Modulation routing](#modulation-routing) |
| `--vel-dcf-kf` | [Modulation routing](#modulation-routing) |

---

## Appendices

### Display value formulae

The conversion between front-panel display values (0 to 99) and stored bytes is documented in [casio-fz1-format.md](casio-fz1-format.md#hardware-display-scale). fizzle does the conversion internally so flags accept the display value.

### Where the manual disagrees with the spec

A small number of corrections against the original Casio R&D specification are documented in [casio-fz1-format.md](casio-fz1-format.md#real-world-findings). The most consequential is the `mchn` (MIDI channel) array offset: the spec lists it at 0x104, but the hardware actually reads it at 0x142. fizzle writes 0x142.
