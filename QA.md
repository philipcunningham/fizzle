# fizzle QA

This checklist covers behavior that automated assertions can't establish. Run `make check` first, then select scenarios affected by the change.

Commands use fixtures under `testdata/synthetic`. Create disposable output outside the repository:

```sh
QA=$(mktemp -d -t fizzle-qa)
```

Use `--debug` and retain stderr when a scenario fails.

## CLI QA

### CLI-01: Error messages

Run malformed and boundary inputs through every changed command. Include missing files, wrong formats, absent required arguments, conflicting flags, and values outside documented ranges.

```sh
fizzle fzv import does-not-exist.wav "$QA/out.fzv"
fizzle sfz convert --fit-to-disk --split-disks testdata/synthetic/JUNGLISM.sfz "$QA/x"
fizzle disk add does-not-exist.img README.md
fizzle fzf midi some.fzf --voice X --all --channel 1
fizzle fzv edit some.fzv --dca-rate-1 100
```

Pass when each message identifies the bad input and the accepted constraint. Usage errors exit 2, data errors exit 1, and neither path panics.

### CLI-03: Cancellation cleanup

Interrupt conversion while it is writing:

```sh
out="$QA/junglism.fzf"
fizzle sfz convert testdata/synthetic/JUNGLISM.sfz "$out" &
pid=$!
sleep 0.05
kill -INT "$pid"
wait "$pid"; echo "exit=$?"
find "$QA" -maxdepth 1 -type f -print
```

Repeat with `--split-disks`. The process must exit promptly with a nonzero status and leave no output or temporary file.

### CLI-08: Native audio

Run this scenario on macOS and Windows because automated tests replace the audio backend:

```sh
fizzle disk get testdata/synthetic/HOOVER.img HOOVER "$QA/hoover.fzv"
time fizzle fzv play "$QA/hoover.fzv"
```

Pass when playback is audible, artifact free, and exits successfully after the voice ends.

### CLI-09: WAV boundaries

Exercise an off-rate file, a one-sample file, and a WAV with an SMPL loop chunk through `fzv import`, `fzv extract`, and re-import.

```sh
ffmpeg -f lavfi -i "sine=frequency=440:duration=1:sample_rate=44100" -ac 1 -sample_fmt s16 "$QA/off-rate.wav"
fizzle fzv import "$QA/off-rate.wav" "$QA/off-rate.fzv"
fizzle fzv info "$QA/off-rate.fzv"
```

Off-rate audio must become 36 kHz. Short audio mustn't panic. Loop markers must survive extraction and re-import.

### CLI-10: Concurrent writes

Drive two processes against one image:

```sh
fizzle fzv import "testdata/synthetic/JUNGLISM Samples/808.wav" "$QA/v1.fzv"
fizzle fzv import "testdata/synthetic/JUNGLISM Samples/reese.wav" "$QA/v2.fzv"
fizzle disk new PAR "$QA/par.img"
fizzle disk add "$QA/par.img" "$QA/v1.fzv" & pid1=$!
fizzle disk add "$QA/par.img" "$QA/v2.fzv" & pid2=$!
wait "$pid1"; r1=$?
wait "$pid2"; r2=$?
fizzle disk ls "$QA/par.img"
```

Both commands must succeed, and both voices must exist. No lock or temporary file may remain.

### CLI-13: Capacity failure

Fill an image until `disk add` fails:

```sh
fizzle disk new FULL "$QA/full.img"
for i in $(seq 1 20); do
  fizzle disk add "$QA/full.img" "$QA/v1.fzv" 2>"$QA/error.log" || break
done
cat "$QA/error.log"
fizzle disk ls "$QA/full.img"
```

Pass when the error names the capacity constraint, the image remains readable, and no temporary file remains.

### CLI-14: Logging levels

```sh
fizzle --debug sfz convert testdata/synthetic/JUNGLISM.sfz "$QA/junglism.fzf" 2>"$QA/debug.log"
fizzle sfz convert --fit-to-disk testdata/synthetic/JUNGLISM.sfz "$QA/fit.fzf" 2>"$QA/fit.log"
```

Pass when debug output identifies processed inputs and downsampling warnings name the requested and selected rates.

### CLI-15: DAW export

Export a hardware FZF and load the resulting SFZ in a supported sampler or DAW:

```sh
fizzle disk get testdata/synthetic/TECHNO.img FULL-DATA-FZ "$QA/techno.fzf"
fizzle sfz export "$QA/techno.fzf" "$QA/techno-out"
fizzle sfz convert "$QA/techno-out/techno.sfz" "$QA/techno-roundtrip.fzf"
```

Pass when every voice has one playable WAV, key and velocity ranges match, pitches are correct, and conversion accepts the exported SFZ.

CUE, SYNTH, and REVERSE playback export as one-shot regions. Confirm their comments retain the source mode and their playback has no corruption.

## Hardware QA

Use a Casio FZ-1, FZ-10M, or FZ-20M. Back up the target media before copying a generated image.

### HW-01: Disk and voice round trip

```sh
fizzle disk get testdata/synthetic/HOOVER.img HOOVER "$QA/hoover.fzv"
fizzle disk new HOOVER "$QA/hoover-copy.img"
fizzle disk add "$QA/hoover-copy.img" "$QA/hoover.fzv"
```

Load the image. The sampler must show the voice and play it at the expected pitch and duration without artifacts.

### HW-02: Multi-bank dump

```sh
fizzle disk get testdata/synthetic/TECHNO.img FULL-DATA-FZ "$QA/techno.fzf"
fizzle disk new TECHNO "$QA/techno-copy.img"
fizzle disk add "$QA/techno-copy.img" "$QA/techno.fzf"
```

The sampler must load all 32 voices. Each voice must play, and METAL-BELL must retain its attack and sustain.

### HW-03: Generated voice defaults

```sh
fizzle fzv import kick.wav "$QA/kick.fzv"
fizzle disk new DEFAULTS "$QA/defaults.img"
fizzle disk add "$QA/defaults.img" "$QA/kick.fzv"
```

The voice must respond continuously from velocity 1 through 127. Note release must fade without an abrupt cut or filter sweep.

### HW-04: Split instrument

```sh
fizzle sfz convert --split-disks testdata/synthetic/JUNGLISM.sfz "$QA/junglism"
```

Load both images when prompted. Every voice must play at the expected pitch and duration without silence or artifacts.

### HW-05: MIDI channels

```sh
fizzle sfz convert testdata/synthetic/JUNGLISM.sfz "$QA/junglism.fzf"
fizzle fzf midi "$QA/junglism.fzf" --voice "AMEN 01" --voice "AMEN 02" --channel 1
fizzle fzf midi "$QA/junglism.fzf" --voice 808 --voice REESE --channel 2
fizzle disk new MIDI "$QA/midi.img"
fizzle disk add "$QA/midi.img" "$QA/junglism.fzf"
```

Channel 1 must trigger only its two assigned voices, and channel 2 must trigger only its assigned voices.

### HW-08: Filter LFO

Load `testdata/synthetic/PAD-LFO.img`. The pad must produce sine modulation on the filter at panel rate 20 and depth 50.

### HW-09: DCA envelope

```sh
fizzle fzv import pad.wav "$QA/pad.fzv"
fizzle fzv edit "$QA/pad.fzv" --dca-sustain 2 --dca-end 3 --dca-rate-1 99 --dca-stop-1 85
fizzle disk new DCA "$QA/dca.img"
fizzle disk add "$QA/dca.img" "$QA/pad.fzv"
```

The panel must show rate 99 and stop level 85. The amplitude must follow the edited stages without jumps or silence.

### HW-10: DCF envelope

```sh
fizzle fzv import pad.wav "$QA/pad.fzv"
fizzle fzv edit "$QA/pad.fzv" --cutoff 64 --dcf-rate-1 50 --dcf-stop-1 26
fizzle disk new DCF "$QA/dcf.img"
fizzle disk add "$QA/dcf.img" "$QA/pad.fzv"
```

The panel must show cutoff 64, rate 50, and stop level 26. The filter must follow the envelope through note on and release.

### HW-11: Full-dump edit isolation

```sh
fizzle fzf edit drums.fzf --voice PAD --cutoff 64 --resonance 7
fizzle disk new EDIT "$QA/edit.img"
fizzle disk add "$QA/edit.img" drums.fzf
```

The selected voice must show cutoff 64 and resonance 7. Other voices and parameters must remain unchanged.

### HW-12: Voice names

Rename a voice with `fzv edit --name` and another with `fzf edit --voice NAME --name`. Both names must match the panel without affecting playback.

### HW-13: Outputs

```sh
fizzle sfz convert --fit-to-disk testdata/synthetic/JUNGLISM.sfz "$QA/outputs.fzf"
fizzle fzf output "$QA/outputs.fzf" --voice "AMEN 01" --output 1
fizzle fzf output "$QA/outputs.fzf" --voice 808 --output 2
fizzle fzf output "$QA/outputs.fzf" --voice REESE --output 3,4
fizzle disk new OUTPUT "$QA/outputs.img"
fizzle disk add "$QA/outputs.img" "$QA/outputs.fzf"
```

Each voice must reach only its assigned physical jack. The panel and `fzf info` must show the same assignments.

## Exploratory testing

Exercise each changed workflow beyond its scripted happy path. Record the platform, hardware model, fixture, command, observed result, and debug log.
