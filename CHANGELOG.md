# Changelog

## Unreleased (last updated 2026-08-27)

- The Sample panel's rate caption sits under its value. The browser reads `18000 Hz` over `Sample rate` rather than `18000 HzSample rate`.

## v0.7.0 (2026-08-26)

- The browser core's filename now carries a content hash. The next visit picks up a deployment instead of retaining a cached core.

- The mod wheel's amp and filter depths were labelled the wrong way round. A bank with `DCF LEVEL` set on the machine read back as `DCA LEVEL`. Only the labels were wrong, so saved files are unaffected.

## v0.6.0 (2026-08-23)

- Disks saved on the FZ itself now open and edit correctly. A deleted file no longer hides the rest of the directory, and a voice in no bank is no longer dropped. An instrument the browser exports carries its voice count in a fizzle-only marker, so its bytes differ from a bare `disk get` of the same dump.

- The browser editor's preview follows the FZ's loop chain. A held key repeats the capped loop, and its release moves to the end loop. The waveform marks both. The cross-fade and the multi-loop time stay out for now.

- Parameters read and write the way the FZ front panel shows them. Tune is in cents, and LFO delay uses the panel's 0 to 127 scale. Area volume follows `AREA LEVEL`, where 127 is loudest. Values that showed a stored byte now show the machine's number.

- `fzv info` reads the way the panel reads. Tune is in cents, the LFO delay uses the panel's scale, and velocity to resonance drops the sign column the panel doesn't have. The LFO attack and resonance depth move to a line naming them as bytes no panel row reaches.

- `--lfo-attack` and `--lfo-q` are gone, and `--lfo-sync` is new. The panel derives the attack from the delay and has no resonance depth control, so fizzle offers neither. It still round trips both bytes untouched.

- Velocity to resonance is 0 to 127, not -127 to 127. The panel's row has no sign column and refuses to go below zero.

- Editing one envelope stage in the browser leaves the others alone. Saving used to rewrite all sixteen bytes through the display scale, which is lossy in both directions, so stages nobody touched could come back changed.

- An area's velocity range floors at 1, matching the panel's `MIN TOUCH` and `MAX TOUCH` rows. Zero silences the voice, and the panel never offered it.

- The sample rate is no longer editable on a recorded voice. The panel fixes it when the sample is taken.

- The waveform draws a playhead while a note sounds, so a loop is visible and not only audible. It snaps back at the loop's end under a held key, then travels on to the end loop when the key comes up.

## v0.5.0 (2026-08-18)

- The browser editor is live at [philipcunningham.github.io/fizzle](https://philipcunningham.github.io/fizzle/), with nothing to install. It runs the same Go core compiled to WebAssembly, so it writes the bytes the CLI writes. Create or open a disk, import audio, edit the instrument, preview it, and export an image for a floppy emulator. The loaded page makes no network requests. Samples and disk images never leave the machine. Desktop Chrome is the supported browser.

- Remove `fizzle studio`, the interactive terminal UI: the subcommand, its packages, and its PTY feature specs are gone. The CLI and the browser editor are the two remaining front ends. The pure container and model packages the browser editor shares live on at `pkg/container` and `pkg/model`.

- Remove the CycloneDX SBOM: `fizzle licenses --json`, `make sbom`, the `fizzle.cdx.json` release asset, and the `cyclonedx-gomod` tool are gone. `fizzle licenses` still prints the project license and the full third-party attribution the binary embeds.

- `fizzle sfz convert` and `fizzle fzv import` refuse stereo WAV input, naming the file and asking for a mono source. Both used to accept it and write a voice of double length at half the pitch, with the two channels alternating sample by sample. Convert the file to mono first. The browser editor asks which side to keep instead: left, right, or the mix of the two.

- Imported voices root at C4 instead of C5. A WAV therefore plays at its recorded pitch on C4 and matches the SFZ default. This changes the cent byte written by `fizzle fzv import` and `fizzle fzf build`. Existing voices keep their root; use `fzv edit --root` to change one.

## v0.4.0 (2026-06-24)

- Fix studio data-loss bugs and resolve the manual-QA backlog.

## v0.3.1 (2026-06-16)

- Fix studio data loss: imported voices and bank renames now persist when saving new or single-voice disks.
- Add PTY-driven feature specs (`make feature-test`) covering save/reload round-trips.

## v0.3.0 (2026-06-16)

- Harden the studio TUI: per-context help, clearer navigation and labels, action confirmations, and CLI-consistent free space.

## v0.2.1 (2026-06-14)

- Fix multi-disk handling in studio.

## v0.2.0 (2026-06-14)

- `fizzle studio` is now a workspace-oriented Bubble Tea TUI. The previous tview-based studio has been removed. studio takes a workspace directory and opens files from its Workspace browser. It defaults to the current directory and no longer accepts individual files. See [pkg/studio/README.md](pkg/studio/README.md) for features, key bindings, and testing.
- Expanded the factory-library corpus with 27 full dumps from Casio disks FL-7 through FL-12 and FL-14. The collection now covers FL-1 through FL-14, FL-A, and FL-B. Added `fzf info` snapshots for every new file. Integration assertions cover large audio areas, velocity splits, and mixed sample rates.

## v0.1.0 (2026-05-28)

- `disk add` recognises Casio FZ-1 expanded-software binaries with the standard 14-byte preamble. It writes Type-5 `Program` entries with uppercase, 12-character names.
- `make demo` builds a Casio FZ-1 scrolling-text demo program: assembles `testdata/assembly/DEMO.asm` with nasm and bakes a loadable `DEMO.img`. `make asm-tools` installs nasm via Homebrew on macOS. See `testdata/assembly/README.md` for details
- `fzv play` command for voice audio preview; native audio on macOS and Windows via oto/v3 (no external tools), `aplay`/`paplay`/`ffplay` on Linux
- `--json` output flag on `disk ls`, `fzv info`, `fzf info`, and `fzb info`
- Manage FZ series floppy disk images: create, list, add, get, and copy files
- Import mono PCM WAV files (16, 24, or 32-bit) as FZ voices at 36, 18, or 9 kHz
- Extract audio from voice files back to WAV
- Inspect voice parameters: sample rate, duration, key range, filter, envelopes, LFO, and loop points
- Build full dump files (`.fzf`) from individual voices, with key ranges, velocity splits, root keys, and generator channel assignments
- Unpack full dumps into individual voice files
- Voice map table showing keys, rate, duration, loop markers, MIDI channel, and optional root/velocity columns
- MIDI receive channel assignment per voice (`fzf midi`) for independent pitch bend and expression control per voice group
- Per-voice output assignment (`fzf output`) for routing voices to the FZ-1's individual output jacks 1-8; supports single, multiple, or all outputs
- `fzf info` shows output assignment per voice ("Out" column)
- Read real hardware FZFs including multi-bank full dumps (up to 8 banks)
- Convert SFZ instruments or directories of WAV files directly to full dumps
- SFZ `mutegroup=N` for monophonic voice groups; new notes cut the previous in the same group
- WAV SMPL chunk loop points applied to voice headers, scaled correctly when resampling
- Automatic downsampling to fit within the 1.25 MB disk limit (`--fit-to-disk`)
- Multi-disk splits preserve full quality with `--split-disks`. The hardware supports two floppy disks and up to 2 MB of sample RAM.
- `fizzle studio` terminal UI for editing a full dump or disk image. It provides three zones, live validation, keyboard navigation, undo, audition, and atomic multi-disk saves.
- `fzf edit` modifies voice parameters inside a full dump file: filter, LFO, DCA/DCF envelopes, and name, targeting a single voice by name
- `fzv edit` and `fzf edit` support voice renaming (`--name`), filter editing (`--cutoff`, `--resonance`), LFO programming, and modulation routing (`--dca-level-kf`, `--dca-rate-kf`, `--dcf-level-kf`, `--dcf-rate-kf`, `--vel-dca-kf`, `--vel-dcf-kf`)
- `fzv edit` supports DCA and DCF envelope editing with the hardware display scale. It edits sustain, end, rates, and stop levels while preserving direction bits.
- `fzf effects` command for reading and modifying the global effect block (pitch bend range, mod wheel, foot pedal, aftertouch routing)
- `fzv edit` and `fzf edit` support tuning (`--tune`), key range (`--key-low`, `--key-high`, `--root`), and playback mode (`--playback-mode`)
- `fzf unpack --bank N` for bank-selective voice extraction from multi-bank full dumps
- SFZ `tune`, `cutoff`, `resonance`, `loop_start`, `loop_end` opcodes applied during conversion
- `fzb info` command for inspecting bank dump files
- `fzf unpack --disk2` extracts voices from a 2-disk split, merging audio from both disks
- `sfz convert` accepts a directory of WAV files as input (zero-SFZ workflow for basic drum kits)
- `sfz export` converts an FZF full dump back to an SFZ instrument with WAV files, enabling round-trip workflows between hardware and DAW
- Exported `audioplayer.Player` interface and `audioplayer.TestPlayer` for testing audio playback without hardware
- Shell completion for bash, zsh, fish, and pwsh
- DCA envelope with proper note release behaviour: amplitude decays to silence on MIDI note-off, matching the hardware convention
- DCF envelope with no filtering by default: filter stays fully open through note release with no sweep
- Hardware-validated LFO programming: confirmed on real FZ-10M hardware with PAD-LFO test fixture
- Integration tests asserting on filter, envelope, and LFO struct values from all test disk images (HOOVER, STAB, TECHNO, BRASS, PAD-LFO)
- INFO/WARN/DEBUG logging via zerolog; `--debug` flag for per-file detail
- Version string embeds git commit SHA and build date (`fizzle --version`)
- Cross-compilation targets for Linux, macOS (Intel + Apple Silicon), and Windows
