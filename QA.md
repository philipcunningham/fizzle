# fizzle QA

This file holds the QA scenarios that the automated test suite cannot
cover by itself. Run the automated suite first:

```sh
make check              # unit + package integration tests, fmt, vet, lint
make integration-test   # CLI binary-executing integration tests
```

Once those are green, work through the relevant scenarios below.

The file is split into two sections:

- **CLI QA** (`CLI-01` to `CLI-14`) is software-level testing that an
  operator or agent can perform from a terminal with the `fizzle`
  binary and the fixtures in `testdata/synthetic/`. These scenarios cover
  exploratory testing, ergonomics, cross-command consistency, and
  failure modes that are hard to encode as automated assertions.
- **Hardware QA** (`HW-01` to `HW-13`) requires a real Casio FZ-1,
  FZ-10M, or FZ-20M and cannot be automated.

Run with `--debug` for verbose output when investigating failures.

Test assets in `testdata/synthetic/`: `HOOVER.img`, `STAB.img`, `TECHNO.img`,
`BRASS.img`, `PAD-LFO.img`, `JUNGLISM.sfz` plus `JUNGLISM Samples/`.

---

# CLI QA

Scenarios that exercise the compiled `fizzle` binary against the
fixtures and check behaviour the automated suite does not assert on.

Use a scratch directory for outputs:

```sh
QA=$(mktemp -d -t fizzle-qa)
cd "$QA"
```

---

## CLI-01: Error message ergonomics

**Why CLI-level QA:** Automated tests assert exit codes and rough
substrings. They do not grade whether the message tells the user what
went wrong, what limit was hit, or what to try instead.

**Scenario:** Drive each command with malformed or boundary inputs and
read the error like a first-time user would.

**Steps:**

```sh
# fzv import
fizzle fzv import does-not-exist.wav out.fzv
fizzle fzv import README.md out.fzv                   # not a WAV
fizzle fzv import --rate 12345 testdata/synthetic/HOOVER.img out.fzv

# sfz convert
echo 'not valid sfz' > bad.sfz
fizzle sfz convert bad.sfz bad.fzf
fizzle sfz convert does-not-exist.sfz bad.fzf
fizzle sfz convert --rate 12345 testdata/synthetic/JUNGLISM.sfz x.fzf
fizzle sfz convert --fit-to-disk --split-disks testdata/synthetic/JUNGLISM.sfz x

# disk
fizzle disk new "x" .                                 # IMAGE is a directory
fizzle disk new
fizzle disk ls does-not-exist.img
fizzle disk add does-not-exist.img README.md
fizzle disk get testdata/synthetic/HOOVER.img UNKNOWN out.fzv   # name not on disk

# fzf midi / output
fizzle fzf midi some.fzf                              # no flags
fizzle fzf midi some.fzf --voice X --all --channel 1  # mutually exclusive
fizzle fzf midi some.fzf --voice X --channel 0
fizzle fzf midi some.fzf --voice X --channel 17
fizzle fzf output some.fzf --voice X --output 9
fizzle fzf output some.fzf --voice X --output stereo

# fzv edit / fzf edit
fizzle fzv edit testdata/synthetic/HOOVER.img                   # not a voice file
fizzle fzv edit some.fzv                              # no flags
fizzle fzv edit some.fzv --dca-rate-1 100             # display max is 99
fizzle fzv edit some.fzv --key-low 200
```

**Pass criteria:**

- Every command exits non-zero (CLI usage errors should be exit 2, data
  errors exit 1).
- Each error message names the offending input or flag, the limit or
  expected value, and ideally a fix.
- Package prefixes (`fzfmidi:`, `voiceedit:`) are reserved for internal
  errors. User-facing usage errors should not leak them.
- Truly broken arguments do not produce a stack trace or panic.

---

## CLI-02: Shell completion validity

**Why CLI-level QA:** The README advertises completion for bash, zsh,
fish, and pwsh, but nothing automated parses the generated output.

**Steps:**

```sh
for shell in bash zsh fish pwsh; do
  echo "--- $shell ---"
  fizzle completion "$shell" > "completion.$shell" || echo "FAILED: $shell"
  wc -l "completion.$shell"
done

# Bash and zsh: syntax check
bash -n completion.bash && echo bash syntax OK
zsh -n completion.zsh && echo zsh syntax OK

# fish: load and check that 'fizzle <TAB>' would list subcommands
fish -c 'source completion.fish; complete -C "fizzle "' | head
```

**Pass criteria:**

- Each shell produces non-empty output and exit 0.
- `bash -n` and `zsh -n` parse the output without errors.
- The fish completion lists the top-level subcommands (`disk`, `fzv`,
  `fzf`, `fzb`, `sfz`, `studio`).

---

## CLI-03: SIGINT / cancellation cleanup

**Why CLI-level QA:** The README claims "Long conversions respect
Ctrl+C: cancel a running `sfz convert` and the command exits cleanly
without leaving a half-written file." Nothing automated proves this.

**Steps:**

```sh
# Start a slow convert, signal it, check for leftover temp files.
out="$QA/junglism.fzf"
fizzle sfz convert testdata/synthetic/JUNGLISM.sfz "$out" &
pid=$!
sleep 0.05
kill -INT $pid
wait $pid; echo "exit=$?"
ls "$QA" | grep -E 'junglism\.fzf|\.tmp' || echo "no leftover files"

# Repeat with --split-disks
fizzle sfz convert --split-disks testdata/synthetic/JUNGLISM.sfz "$QA/SPLIT" &
pid=$!
sleep 0.05
kill -INT $pid
wait $pid; echo "exit=$?"
ls "$QA" | grep -E 'SPLIT-[12]\.img|\.tmp' || echo "no leftover files"
```

**Pass criteria:**

- The process exits within a few hundred milliseconds of SIGINT.
- No `.fzf`, `.img`, or `.tmp*` files are left in the scratch
  directory.
- Exit status is non-zero (interrupted), but stderr is not a stack
  trace.

---

## CLI-04: Help text and examples

**Why CLI-level QA:** Examples in `--help` and in the manual rot
silently when flags or args change. No automated test runs them.

**Steps:**

```sh
# Print --help for every command and subcommand
fizzle --help
for cmd in disk fzv fzf fzb sfz studio; do
  fizzle "$cmd" --help
done
for sub in "disk new" "disk ls" "disk add" "disk get" "disk copy" \
           "fzv info" "fzv import" "fzv extract" "fzv play" "fzv edit" \
           "fzf info" "fzf build" "fzf unpack" "fzf midi" "fzf output" \
           "fzf effects" "fzf edit" "fzb info" "sfz convert"; do
  fizzle $sub --help > /dev/null && echo "ok: $sub" || echo "FAILED: $sub"
done

# Take one example from each command's --help and run it against a fixture
```

**Pass criteria:**

- Every command and subcommand returns `--help` without error.
- Every flag listed in `docs/fizzle-manual.md` appears in `--help`.
- The example blocks in `--help` are still syntactically valid (paths
  may need substitution, but flag combinations should still parse).

---

## CLI-05: Operational round-trips

**Why CLI-level QA:** Byte-level round-trips are fuzzed. Round-trips
that involve multiple subcommands chained through the filesystem are
not.

**Steps:**

```sh
# Round-trip 1: sfz convert -> fzf unpack -> fzf build
fizzle sfz convert --fit-to-disk testdata/synthetic/JUNGLISM.sfz a.fzf
fizzle fzf unpack a.fzf voices/
fizzle fzf build b.fzf voices/*.fzv
fizzle fzf info a.fzf > a.info
fizzle fzf info b.fzf > b.info
diff a.info b.info        # voice set should match (order may differ)

# Round-trip 2: disk new -> add -> get -> diff
fizzle fzv import "testdata/synthetic/JUNGLISM Samples/808.wav" 808.fzv
fizzle disk new "RT" rt.img
fizzle disk add rt.img 808.fzv
fizzle disk get rt.img 808 808-out.fzv
cmp 808.fzv 808-out.fzv && echo "round-trip OK"

# Round-trip 3: fzv import -> fzv edit (many flags) -> fzv info verify
fizzle fzv import "testdata/synthetic/JUNGLISM Samples/reese.wav" pad.fzv
fizzle fzv edit pad.fzv --name "MY PAD" --cutoff 80 --resonance 7 \
       --tune 100 --key-low 36 --key-high 96 --root 60 \
       --lfo-wave sine --lfo-rate 30
fizzle fzv info --json pad.fzv | jq '.name,.dcf.cutoff,.dcf.resonance,.tune,.lfo'

# Round-trip 4: fzf build -> fzf edit voice -> fzf unpack -> verify
fizzle fzf build kit.fzf 808.fzv pad.fzv
fizzle fzf edit kit.fzf --voice "MY PAD" --cutoff 64
fizzle fzf unpack kit.fzf unpacked/
fizzle fzv info --json unpacked/MY\ PAD.fzv | jq '.dcf.cutoff'   # expect 64
```

**Pass criteria:**

- Round-trip 1: same voice count, names, and durations on both sides.
- Round-trip 2: extracted voice is byte-identical to the original.
- Round-trip 3: every edited field reads back at the new value.
- Round-trip 4: the per-voice edit propagates through unpack to the
  individual voice file.

---

## CLI-06: Multi-bank `fzf edit`

**Why CLI-level QA:** `fzf edit` is tested against single-bank dumps.
Behaviour on a multi-bank dump (TECHNO has 8 bank sectors) is not
exercised at the CLI level.

**Steps:**

```sh
fizzle disk get testdata/synthetic/TECHNO.img FULL-DATA-FZ techno.fzf
fizzle fzf info techno.fzf | head -20 > before.info
fizzle fzf edit techno.fzf --voice "METAL-BELL" --cutoff 80 --resonance 5
fizzle fzf info techno.fzf | head -20 > after.info
diff before.info after.info
fizzle fzf unpack techno.fzf out/
fizzle fzv info --json "out/METAL-BELL.fzv" | jq '.dcf.cutoff,.dcf.resonance'
```

**Pass criteria:**

- The edit applies to the named voice only.
- No other voice's parameters change between before and after.
- After unpack, the extracted voice carries the new cutoff and
  resonance values.

---

## CLI-07: Studio non-TTY failure

**Why CLI-level QA:** Running an interactive TUI under a pipe or in
CI should fail with a clear message rather than hang or crash.

**Steps:**

```sh
fizzle studio testdata/synthetic/HOOVER.img < /dev/null 2>&1 | head -5
echo "exit=$?"
fizzle studio testdata/synthetic/HOOVER.img < /dev/null > /tmp/out 2>&1
echo "exit=$?"
```

**Pass criteria:**

- The process exits within a few seconds.
- The error message names the cause (no TTY).
- No stack trace.

---

## CLI-08: `fzv play` on the native-audio platform

**Why CLI-level QA:** Automated tests inject a `TestPlayer`. On
darwin and windows the real `oto/v3` backend is selected; that path
is not exercised in `make check`.

**Steps:**

```sh
fizzle disk get testdata/synthetic/HOOVER.img HOOVER hoover.fzv
# Short voice; should play and exit cleanly.
time fizzle fzv play hoover.fzv
echo "exit=$?"
```

**Pass criteria:**

- Exit 0.
- Wall-clock duration is roughly the voice's audible length plus the
  500 ms USB-DAC lead-in.
- On a workstation, audio is actually heard (judgement call;
  unverifiable in headless CI).

---

## CLI-09: WAV edge cases via `fzv import`

**Why CLI-level QA:** The fixtures all use 36 kHz or 18 kHz mono PCM
with no SMPL chunk. Off-rate, very short, and looped WAVs are not
exercised end-to-end at the CLI level.

**Steps:**

```sh
# Off-rate input (resample path)
ffmpeg -f lavfi -i "sine=frequency=440:duration=1:sample_rate=44100" \
       -ac 1 -sample_fmt s16 sine-44k.wav 2>/dev/null
fizzle fzv import sine-44k.wav sine.fzv
fizzle fzv info sine.fzv | grep "Sample rate"   # expect 36000 Hz

# Very short input
ffmpeg -f lavfi -i "sine=frequency=440:duration=0.001:sample_rate=36000" \
       -ac 1 -sample_fmt s16 tiny.wav 2>/dev/null
fizzle fzv import tiny.wav tiny.fzv
fizzle fzv info tiny.fzv | grep -E "Samples|Duration"

# WAV with SMPL chunk loop points (use any of the JUNGLISM Samples)
fizzle fzv import "testdata/synthetic/JUNGLISM Samples/reese.wav" loop.fzv
fizzle fzv info loop.fzv | grep -iE "loop|playback"
fizzle fzv extract loop.fzv loop.wav
# Re-import and verify loop carried through (SMPL chunk survives the round-trip).
```

**Pass criteria:**

- 44.1 kHz input resamples cleanly to 36 kHz.
- A 1-sample WAV imports without panic; `fzv info` shows the right
  count.
- A looped WAV retains its loop markers on the FZV side (visible in
  `fzv info`); after extract, the WAV has a SMPL chunk; after
  re-import, the FZV still has loop markers.

---

## CLI-10: Concurrent disk operations

**Why CLI-level QA:** `fileutil.WithFileLock` is unit-tested with the
real filesystem but not driven from two separate processes. The
cross-process serialisation guarantee is otherwise unverified.

**Steps:**

```sh
fizzle fzv import "testdata/synthetic/JUNGLISM Samples/808.wav" v1.fzv
fizzle fzv import "testdata/synthetic/JUNGLISM Samples/reese.wav" v2.fzv
fizzle disk new "PAR" par.img

# Launch two adds in parallel
fizzle disk add par.img v1.fzv &
pid1=$!
fizzle disk add par.img v2.fzv &
pid2=$!
wait $pid1; r1=$?
wait $pid2; r2=$?
echo "exit codes: $r1 $r2"
fizzle disk ls par.img
```

**Pass criteria:**

- Both processes exit 0.
- The final image contains both voices (no lost write).
- No `.lock` file is left behind.
- The two processes did not both report success against an empty disk
  (one of them must have seen the other's write).

---

## CLI-11: `--json` schema stability

**Why CLI-level QA:** Each `--json` flag is unit-tested independently.
There is no single place that lists the documented schema and
asserts the binary still emits it.

**Steps:**

```sh
fizzle disk ls --json testdata/synthetic/HOOVER.img | jq 'keys'
fizzle fzv info --json hoover.fzv | jq 'keys'
fizzle fzf info --json techno.fzf | jq 'keys'
fizzle fzb info --json some.fzb | jq 'keys'
```

**Pass criteria:**

- Every command with a `--json` flag emits valid JSON.
- The top-level keys match `docs/fizzle-manual.md`.
- All numeric fields are JSON numbers; all names are JSON strings.
- The schema is stable across re-runs of the same input.

---

## CLI-12: Cross-command consistency

**Why CLI-level QA:** The same voice viewed through different commands
should agree. Renderers can drift independently and pass their own
tests.

**Steps:**

```sh
fizzle sfz convert --fit-to-disk testdata/synthetic/JUNGLISM.sfz junglism.fzf
fizzle disk new "JNGL" jungle.img
fizzle disk add jungle.img junglism.fzf
fizzle disk ls jungle.img
fizzle fzf info junglism.fzf | head -8
fizzle fzf unpack junglism.fzf voices/
fizzle fzv info "voices/AMEN 01.fzv" | head -6
```

For each viewing path, the voice name "AMEN 01", its sample rate, its
duration, and its key range should agree.

**Pass criteria:**

- Names match exactly across `disk ls`, `fzf info`, and `fzv info`.
- Sample rate is the same in `fzf info` and in `fzv info` on the
  unpacked voice.
- Duration agrees within rounding (the FZF table shows seconds to 3
  decimal places).

---

## CLI-13: Disk-full handling

**Why CLI-level QA:** Boundary-at-capacity is fuzzed at the byte
level, but the user-facing behaviour of "the disk filled up while I
was adding files" is not.

**Steps:**

```sh
fizzle disk new "FULL" full.img
# Add the same voice repeatedly until the disk is full.
for i in $(seq 1 20); do
  fizzle disk add full.img v1.fzv 2>err.log || { cat err.log; break; }
done
fizzle disk ls full.img | tail -3
```

**Pass criteria:**

- The eventual error names the constraint ("no space", a numeric limit,
  or similar).
- The image remains parseable by `disk ls` after the failed add.
- No `.tmp` file is left behind by the failed write.

---

## CLI-14: Debug logging and warnings

**Why CLI-level QA:** `--debug` is documented as showing per-file
detail. The presence and shape of WARN lines on the fit-to-disk path
is part of the user contract.

**Steps:**

```sh
fizzle --debug sfz convert testdata/synthetic/JUNGLISM.sfz junglism.fzf 2>debug.log
grep -c '^DEBUG' debug.log
grep -c '^WARN' debug.log
head -20 debug.log

# fit-to-disk should emit a WARN naming the rate it picked
fizzle sfz convert --fit-to-disk testdata/synthetic/JUNGLISM.sfz fit.fzf 2>fit.log
grep -i 'downsampling\|capacity\|fit' fit.log
```

**Pass criteria:**

- `--debug` emits DEBUG lines per region or per voice; the count
  scales with the input.
- `--fit-to-disk` emits a WARN when it has to downsample, naming both
  the requested and selected rate.

---

## CLI-15: `sfz export` round-trip into a DAW

**Why CLI-level QA:** The `sfz export` command produces an SFZ + WAV
bundle intended to load in Renoise, Bitwig, or any other SFZ-aware
DAW. Automated tests verify that the exported SFZ can be re-converted
by `sfz convert`, but nothing automated proves the bundle actually
loads in a DAW or that the audio plays back correctly. The export
also has a known limitation around playback modes (CUE, SYNTH,
REVERSE collapse to NORMAL on the SFZ side) that an operator should
confirm matches expectations for their workflow.

**Scenario:** Export a real hardware FZF, load the result in a DAW,
and confirm key ranges, velocities, and audio fidelity.

**Steps:**

```sh
# Export an FZF you already have on a disk.
fizzle disk get testdata/synthetic/TECHNO.img FULL-DATA-FZ techno.fzf
fizzle sfz export techno.fzf ./techno-out/

# Inspect the produced SFZ.
ls ./techno-out/
head -40 ./techno-out/techno.sfz

# Round-trip the export back through sfz convert and diff key fields.
fizzle sfz convert ./techno-out/techno.sfz techno-rt.fzf
fizzle fzf info techno.fzf    > /tmp/orig.info
fizzle fzf info techno-rt.fzf > /tmp/rt.info
diff /tmp/orig.info /tmp/rt.info

# Load techno-out/ into a DAW that supports SFZ (Renoise, Bitwig,
# sforzando, etc.) and audition each voice.
```

**Pass criteria:**

- `sfz export` produces one `.sfz` and one `.wav` per voice in the
  source FZF.
- The SFZ file references each WAV via a `sample=` line and lists
  `lokey`, `hikey`, `pitch_keycenter` for every region.
- `// Voice N:`, `// DCA:`, `// DCF:`, `// LFO:`, `// Playback:`
  comment lines appear above each region.
- `sfz convert` accepts the exported SFZ without error.
- `diff` of `fzf info` between the original and round-tripped FZFs
  shows the same voice count, names, key ranges, MIDI channels, and
  outputs (durations and exact rate are allowed to differ because
  `sfz convert` may resample).
- In the DAW: each voice plays at the correct pitch on its assigned
  key, audio is audibly clean (no clicks or wrong notes), and the
  velocity range matches the source.
- A voice whose source FZF used CUE, SYNTH, or REVERSE playback is
  represented as `loop_mode=one_shot` (with the original mode in the
  `// Playback:` comment); the DAW will play it as a one-shot.

---

## CLI-16: FZF to SFZ to FZF round-trip

**Why CLI-level QA:** A package test covers this round-trip at the
data level but on synthetic fixtures. Running it against real
hardware FZFs exposes whatever drift exists in the export and
re-conversion paths against the actual voice headers the sampler
writes.

**Scenario:** Export a hardware FZF as SFZ + WAVs, re-convert that
SFZ to a fresh FZF, and confirm every voice slot survives in the
same order with the same key range, MIDI channel, output, sample
rate, and approximate duration.

**Steps:**

```sh
QA=$(mktemp -d -t fzf-sfz-rt)

# 1. Export TECHNO (32 voices, multi-bank).
fizzle disk get testdata/synthetic/TECHNO.img FULL-DATA-FZ "$QA/techno.fzf"
fizzle sfz export "$QA/techno.fzf" "$QA/techno-out"

# 2. Re-convert.
fizzle sfz convert "$QA/techno-out/techno.sfz" "$QA/techno-rt.fzf"

# 3. Compare per-voice metadata.
fizzle fzf info --json "$QA/techno.fzf" \
  | jq '.voices[] | {name, key_low, key_high, midi_channel, output}' \
  > "$QA/orig.json"
fizzle fzf info --json "$QA/techno-rt.fzf" \
  | jq '.voices[] | {name, key_low, key_high, midi_channel, output}' \
  > "$QA/rt.json"
diff "$QA/orig.json" "$QA/rt.json"

# 4. Repeat against BRASS (13 voices, key-range mapping).
fizzle disk get testdata/synthetic/BRASS.img FULL-DATA-FZ "$QA/brass.fzf"
fizzle sfz export "$QA/brass.fzf" "$QA/brass-out"
fizzle sfz convert "$QA/brass-out/brass.sfz" "$QA/brass-rt.fzf"
fizzle fzf info --json "$QA/brass.fzf" \
  | jq '.voices[] | {name, key_low, key_high}'
fizzle fzf info --json "$QA/brass-rt.fzf" \
  | jq '.voices[] | {name, key_low, key_high}'
```

**Pass criteria:**

- Every step exits 0.
- TECHNO's exported directory contains exactly 32 `.wav` files plus
  one `.sfz`. BRASS's contains 13 + 1.
- Voice order in the re-converted FZF matches the original FZF's
  slot order.
- `key_low`, `key_high`, `midi_channel`, and `output` are identical
  across all 32 TECHNO voices and all 13 BRASS voices.
- Voice names match modulo the documented `fzutil.VoiceName`
  normalisation (hyphens and other non-alphanumerics collapse to
  spaces; consecutive whitespace collapses to a single space).
  TECHNO's `METAL-BELL` legitimately becomes `METAL BELL`,
  `BASS DRUM  2` becomes `BASS DRUM 2`.

---

## CLI-17: SFZ to FZF to SFZ round-trip

**Why CLI-level QA:** The complementary round-trip: take an SFZ
authored in a DAW, push it through fizzle into the FZ format, then
pull it back out. Catches loss of fidelity in either direction
against a known SFZ source-of-truth.

**Scenario:** Convert `JUNGLISM.sfz` to FZF, export the FZF back to
SFZ + WAVs, and check that the region count and sample references
hold.

**Steps:**

```sh
QA=$(mktemp -d -t sfz-fzf-rt)

# 1. Convert SFZ to FZF (with downsample to fit).
fizzle sfz convert --fit-to-disk testdata/synthetic/JUNGLISM.sfz "$QA/junglism.fzf"

# 2. Export back to SFZ + WAVs.
fizzle sfz export "$QA/junglism.fzf" "$QA/junglism-out"

# 3. Compare counts and first/last voice names.
echo "regions: $(grep -c '<region>' "$QA/junglism-out/junglism.sfz")"
echo "wavs:    $(ls "$QA/junglism-out"/*.wav | wc -l)"

# 4. Re-convert the exported SFZ to verify it round-trips through
#    the converter cleanly.
fizzle sfz convert "$QA/junglism-out/junglism.sfz" "$QA/junglism-rt.fzf"
fizzle fzf info --json "$QA/junglism-rt.fzf" \
  | jq '.voice_count'
```

**Pass criteria:**

- All steps exit 0.
- The exported SFZ contains exactly 28 `<region>` blocks and 28
  `.wav` files (matching JUNGLISM's 28 source samples).
- Each `<region>` has a `sample=` reference, a `lokey/hikey/pitch_keycenter`
  triplet, and is preceded by the documented `// Voice N:`,
  `// DCA:`, `// DCF:`, `// LFO:`, `// Modulation:`, `// Playback:`
  comment block.
- The re-converted FZF reports `voice_count == 28`.
- Each WAV file is playable (open in any audio editor or run
  `fizzle fzv import` against it to confirm).

---

# Hardware QA

Scenarios that require a real Casio FZ-1, FZ-10M, or FZ-20M sampler
and cannot be automated.

---

## HW-01: Extract a file from a real FZ disk image and load on hardware

**Automated counterparts:** `TestCLIDiskLs`, `TestCLIDiskAddAndGet`,
`TestCLIRoundTrip`, `TestGoldenDiskLs` (software-level validation).

**Scenario:** User has a disk image from real hardware and wants to get a voice
off it, then load it back onto the sampler.

**Steps:**
```sh
fizzle disk ls testdata/synthetic/HOOVER.img
fizzle disk get testdata/synthetic/HOOVER.img HOOVER hoover.fzv
fizzle fzv info hoover.fzv
fizzle disk new "HOOVER" hoover-copy.img
fizzle disk add hoover-copy.img hoover.fzv
```

Copy `hoover-copy.img` to a USB floppy emulator or real floppy disk and load
on the sampler.

**Pass criteria:**
- Sampler loads the disk without error
- Voice plays correctly at the expected pitch and duration
- No audible artifacts or silence

---

## HW-02: Real hardware FZF: multi-bank dump (TECHNO.img)

**Automated counterparts:** `TestCLIDiskLs/TECHNO`, `TestTechnoFZFUnpack`,
`TestTechnoVoiceHeaderSanity`, `TestTechnoRoundTripExtract`.

**Scenario:** Load a real hardware multi-bank full dump on the sampler.

**Steps:**
```sh
fizzle disk ls testdata/synthetic/TECHNO.img
fizzle disk get testdata/synthetic/TECHNO.img FULL-DATA-FZ techno.fzf
fizzle fzf info techno.fzf
fizzle disk new "Techno" techno-copy.img
fizzle disk add techno-copy.img techno.fzf
```

Copy `techno-copy.img` to a USB floppy emulator and load on the sampler.

**Pass criteria:**
- Sampler loads all 32 voices without error
- Each voice plays at the correct pitch
- No voices are silent (envelope bug regression)
- METAL-BELL plays with correct attack and sustain

---

## HW-03: Velocity sensitivity on hardware

**Automated counterparts:** `TestEnvelopeDefaultsMatchHardware` (DCA defaults
ensure velocity response).

**Scenario:** Verify velocity affects amplitude on generated voices when played
on the sampler.

**Steps:**
```sh
fizzle fzv import kick.wav kick.fzv
fizzle disk new "Drums" drums.img
fizzle disk add drums.img kick.fzv
```

Copy `drums.img` to the sampler.

**Pass criteria:**
- Playing at velocity 1 vs 127 produces audibly different volume levels
- Velocity curve feels natural (not on/off)

---

## HW-04: Multi-disk split loading on hardware

**Automated counterparts:** `TestCLISfzConvertSplitDisks`,
`TestCLIMultiDiskUnpack`, `TestMultiDiskBankSectorInvariant`,
`TestMultiDiskUnpackBothDisks`.

**Scenario:** Verify that a multi-disk SFZ conversion loads correctly when split
across two floppy disks.

**Steps:**
```sh
fizzle sfz convert --split-disks JUNGLISM.sfz junglism
# produces junglism-1.img and junglism-2.img (ready to copy to Gotek/floppy)
```

Copy both images to USB floppy emulators. Load `junglism-1.img` on the sampler;
when prompted, load `junglism-2.img`.

**Pass criteria:**
- Sampler loads both disks without error
- All voices play correctly at the expected pitch and duration
- No audible artifacts or silence

---

## HW-05: MIDI channel assignment on hardware

**Automated counterparts:** `TestCLIFzfMidi`, `TestFZFMidiEndToEnd`.

**Scenario:** Verify that MIDI channel assignments route correctly on the
sampler.

**Steps:**
```sh
fizzle sfz convert testdata/synthetic/JUNGLISM.sfz junglism.fzf
fizzle fzf midi junglism.fzf --voice "AMEN 01" --voice "AMEN 02" --channel 1
fizzle fzf midi junglism.fzf --voice "808" --voice "REESE" --channel 2
fizzle disk new "MIDI" midi.img
fizzle disk add midi.img junglism.fzf
```

Copy `midi.img` to the sampler.

**Pass criteria:**
- Notes on MIDI channel 1 trigger only channel-1 voices
- Notes on MIDI channel 2 trigger only channel-2 voices
- No voices respond on unassigned channels

---

## HW-06: Filter and envelope defaults on hardware

**Automated counterparts:** `TestEnvelopeDefaultsMatchHardware`,
`TestHooverVoiceParameters` (DCA/DCF default validation).

**Scenario:** Verify that generated voices play without audible filtering and
that note-off produces a clean amplitude release with no filter sweep.

**Steps:**
```sh
fizzle fzv import kick.wav kick.fzv
fizzle fzv info kick.fzv
fizzle disk new "ENV TEST" env-test.img
fizzle disk add env-test.img kick.fzv
```

Copy `env-test.img` to the sampler.

**Pass criteria:**
- Voice plays without audible high-frequency rolloff or darkening
- MIDI note-off produces a clean amplitude fade to silence (no abrupt cut)
- No filter sweep is audible on note release
- Velocity affects amplitude (soft keys quieter, hard keys louder)
- `fzv info` shows no "Filter:" line (default is hidden)

---

## HW-07: Studio edit round-trip

**Automated counterparts:** the model + widget tests under
`pkg/studio/{model,widgets/*}/widget_test.go` cover field commit,
undo/redo, focus cycling, and bank-site fan-out at the model layer.
Hardware QA verifies the edits are audible on a real FZ-1 / FZ-10M.

**Scenario:** Open an image in the studio, edit one voice across DCA /
DCF / LFO / global effect, rename a bank, save, reload, and verify the
edits persist and play correctly on hardware.

**Setup:**
```sh
cp testdata/synthetic/HOOVER.img /tmp/qa-studio.img
fizzle studio /tmp/qa-studio.img
```

**Edits to perform inside the studio:**

1. Navigation: press `2` to switch to a bank tab; press `Alt+1` / `Alt+2`
   / `Alt+3` to cycle the lower detail panels; press `Tab` to walk
   forward through fields; press `Shift+Tab` to cycle panes.
2. Voice Details: pick any voice and change Cutoff, Resonance, DCA
   Level KF, and LFO Rate to distinct non-default values.
3. Bank tab: rename the bank to something with mixed case (e.g.
   `Test Edit`). Confirm the displayed value preserves case after Tab
   out (no auto-upper-case).
4. Bank tab: for the voice you edited in step 2, change its area
   Volume and MIDI Channel.
5. Loop Details: select that voice. Change Sustain loop to a different
   stage.
6. Global Effect: change Bend Range to a distinct value.
7. Optional: press `Ctrl+Z` once and `Ctrl+Y` once to confirm
   undo/redo round-trips.
8. Press `Ctrl+S`, confirm the save modal. The status line should
   report the save; the header's `[modified]` indicator should clear.
9. Quit with `Ctrl+Q` and re-launch on the same file.

**Pass criteria (on-screen, after reload):**

- Every edit from steps 2 to 6 is visible and unchanged in the studio
  after the relaunch.
- Bank name preserves the typed mixed case.
- The `[modified]` indicator is absent on relaunch.

**Pass criteria (on hardware):**

- Copy `/tmp/qa-studio.img` to the floppy / Gotek and load on the
  FZ-1. The edited voice plays with the new filter, envelope, LFO,
  and global-effect characteristics. The bank shows the renamed
  name on the front panel.
- F14 sanity: the voice's key range as reported by the FZ-1 matches
  what was edited (this is the bank-site fan-out for key-range edits;
  without it, hardware ignores the edit even though the file looks
  correct).

**Failure modes to record:**

- Field appears edited in the studio after save but does not affect
  hardware playback: indicates a missing bank-site sync or wrong
  storage offset.
- Field reverts after save then relaunch: indicates the model's
  Apply path is not committing through to the saved bytes.
- Save modal does not appear, or the studio hangs on Ctrl+S: file
  lock contention or modal stack regression.

---

## HW-08: LFO on filter (PAD-LFO.img)

**Automated counterparts:** `TestPadLFOVoiceParameters`,
`TestPadLFOImageChecksum`, `TestCLIPadLFOVoiceInfo`.

**Scenario:** Verify LFO sine wave modulation on the filter plays correctly
on hardware.

**Steps:**
```sh
fizzle disk ls testdata/synthetic/PAD-LFO.img
fizzle disk get testdata/synthetic/PAD-LFO.img FULL-DATA-FZ padlfo.fzf
fizzle fzf info padlfo.fzf
```

Copy `PAD-LFO.img` to a USB floppy emulator and load on the sampler.

**Pass criteria:**
- Load PAD-LFO.img on sampler, verify LFO sine wave on filter at rate 20, depth 50
- Filter sweeps audibly with the LFO
- No audible artifacts or unexpected behaviour

---

## HW-09: DCA envelope editing on hardware

**Automated counterparts:** `TestCLIFzvEditDCA`, `TestEditDCAEnvelopeRoundTrip`,
`TestBrassHardwareDisplayValues`.

**Scenario:** Verify that DCA envelope edits produce the expected amplitude
behaviour on the sampler.

**Steps:**
```sh
fizzle fzv import pad.wav pad.fzv
fizzle fzv edit pad.fzv --dca-sustain 2 --dca-end 3
fizzle fzv edit pad.fzv --dca-rate-1 99 --dca-stop-1 85
fizzle fzv info pad.fzv
fizzle disk new "DCA TEST" dca-test.img
fizzle disk add dca-test.img pad.fzv
```

Copy `dca-test.img` to the sampler.

**Pass criteria:**
- Voice plays with the edited envelope shape (sustain at stage 2, end at stage 3)
- Stage 1 rate displays as 99 and stop level displays as 85 on the sampler front panel
- Amplitude behaviour matches the configured envelope (no unexpected jumps or silence)

---

## HW-10: DCF envelope editing on hardware

**Automated counterparts:** `TestCLIFzvEditDCF`, `TestEditDCFEnvelopeRoundTrip`,
`TestBrassHardwareDisplayValues`.

**Scenario:** Verify that DCF envelope edits produce audible filter changes
on the sampler.

**Steps:**
```sh
fizzle fzv import pad.wav pad.fzv
fizzle fzv edit pad.fzv --cutoff 64 --dcf-rate-1 50 --dcf-stop-1 26
fizzle fzv info pad.fzv
fizzle disk new "DCF TEST" dcf-test.img
fizzle disk add dcf-test.img pad.fzv
```

Copy `dcf-test.img` to the sampler.

**Pass criteria:**
- Voice plays with audible filter movement matching the edited DCF envelope
- Stage 1 rate displays as 50 and stop level displays as 26 on the sampler front panel
- Filter behaviour is consistent across note-on and note-off

---

## HW-11: fzf edit round-trip

**Automated counterparts:** `TestCLIFzfEdit`, `TestEditFZFVoiceRoundTrip`,
`TestEditVoiceInImageRoundTrip`.

**Scenario:** Verify that voice parameter edits inside a full dump file
produce the expected values on the sampler.

**Steps:**
```sh
fizzle fzf edit drums.fzf --voice PAD --cutoff 64 --resonance 7
fizzle fzf info drums.fzf
fizzle disk new "EDIT TEST" edit-test.img
fizzle disk add edit-test.img drums.fzf
```

Copy `edit-test.img` to a USB floppy emulator and load on the sampler.
Navigate to the edited voice.

**Pass criteria:**
- Front-panel values match the CLI edits
- Cutoff and resonance display as 64 and 7 on the sampler
- No other voice parameters are altered

---

## HW-12: Voice name editing on hardware

**Automated counterparts:** `TestCLIFzvEditName`,
`TestEditFZFVoiceNameRoundTrip`.

**Scenario:** Verify that renaming a voice via `fzv edit --name` or
`fzf edit --voice ... --name` produces the correct name on the sampler front
panel.

**Steps:**
```sh
fizzle fzv import pad.wav pad.fzv
fizzle fzv edit pad.fzv --name "MY PAD"
fizzle fzv info pad.fzv
fizzle disk new "NAME TEST" name-test.img
fizzle disk add name-test.img pad.fzv
```

Copy `name-test.img` to the sampler.

**Pass criteria:**
- The sampler front panel displays "MY PAD" as the voice name
- Voice plays correctly (rename did not corrupt other parameters)

---

## HW-13: Output assignment on hardware

**Automated counterparts:** `TestCLIFzfOutput`, `TestSetSingleVoiceOutput`,
`TestSetMultipleOutputs`, `TestSetAllVoices`.

**Scenario:** Verify that output assignments route audio to the correct
physical output jacks on the sampler.

**Steps:**
```sh
fizzle sfz convert --fit-to-disk testdata/synthetic/JUNGLISM.sfz test-output.fzf
fizzle fzf output test-output.fzf --voice "AMEN 01" --output 1
fizzle fzf output test-output.fzf --voice "808" --output 2
fizzle fzf output test-output.fzf --voice "REESE" --output 3,4
fizzle fzf output test-output.fzf --voice "PAD 1" --output all
fizzle fzf info test-output.fzf
fizzle disk new "OUTPUT" output-test.img
fizzle disk add output-test.img test-output.fzf
```

Copy `output-test.img` to the sampler.

**Pass criteria:**
- AMEN 01 audio comes from output jack 1 only (front panel shows filled circle in position 1)
- 808 audio comes from output jack 2 only
- REESE audio comes from output jacks 3 and 4 (front panel shows filled circles in positions 3 and 4)
- PAD 1 plays across all outputs (front panel shows all 8 circles filled)
- `fzf info` shows Out column: `1`, `2`, `3,4`, `all` for the respective voices

---

## Exploratory testing

Before a release, consider manually testing any new features or changed
workflows that are not yet covered by the CLI QA or hardware QA above.
Use `--debug` to inspect log output during exploratory sessions.
