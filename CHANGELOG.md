# Changelog

## Unreleased (last updated 2026-06-14)

- `fizzle studio` is now a workspace-oriented Bubble Tea TUI; the previous tview-based studio has been removed. studio takes a workspace directory (defaulting to the current working directory) and opens files from its Workspace browser; individual files are no longer passed on the command line. See [pkg/studio/README.md](pkg/studio/README.md) for the feature set, key bindings, and testing approach.
- Expanded the factory-library test corpus with Casio sound disks FL-7 through FL-12 and FL-14 (27 full dumps), completing FL-1 through FL-14 plus FL-A and FL-B. Adds `fzf info` snapshot coverage for all new files and integration assertions exercising multi-disk-sized audio areas, velocity-split kits, and dumps that mix sample rates within a single file.

## v0.1.0 (2026-05-28)

- `disk add` recognises Casio FZ-1 expanded-software binaries (`.bin` files starting with the standard 14-byte program preamble) and writes them as Type-5 "Program" directory entries; the on-disk name is derived from the input filename basename (uppercased, truncated to 12 chars)
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
- Multi-disk splits for large instruments at full quality (`--split-disks`); splits across 2 floppy disks (the FZ series hardware maximum, limited by 2 MB of sample RAM)
- `fizzle studio` interactive terminal UI for editing a single full dump or disk image. Three-zone layout (header, voice + bank tabs, Voice Details / Loop Details / Global Effect panels), live char-by-char field validation, Tab / Shift+Tab navigation, undo/redo, Space-key audition, atomic save with multi-disk companion patching
- `fzf edit` modifies voice parameters inside a full dump file: filter, LFO, DCA/DCF envelopes, and name, targeting a single voice by name
- `fzv edit` and `fzf edit` support voice renaming (`--name`), filter editing (`--cutoff`, `--resonance`), LFO programming, and modulation routing (`--dca-level-kf`, `--dca-rate-kf`, `--dcf-level-kf`, `--dcf-rate-kf`, `--vel-dca-kf`, `--vel-dcf-kf`)
- `fzv edit` supports DCA and DCF envelope editing: sustain point, end point, per-stage rates (0 to 99) and per-stage stop levels (0 to 99), using the hardware display scale. The envelope direction sign bit is preserved automatically.
- `fzf effects` command for reading and modifying the global effect block (pitch bend range, mod wheel, foot pedal, aftertouch routing)
- `fzv edit` and `fzf edit` support tuning (`--tune`), key range (`--key-low`, `--key-high`, `--root`), and playback mode (`--playback-mode`)
- `fzf unpack --bank N` for bank-selective voice extraction from multi-bank full dumps
- SFZ `tune`, `cutoff`, `resonance`, `loop_start`, `loop_end` opcodes applied during conversion
- `fzb info` command for inspecting bank dump files
- `fzf unpack --disk2` extracts voices from a 2-disk split, merging audio from both disks
- `sfz convert` accepts a directory of WAV files as input (zero-SFZ workflow for simple drum kits)
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
