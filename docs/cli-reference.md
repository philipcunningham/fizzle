# fizzle CLI reference

This file is generated from the command metadata used by the executable. Don't edit it by hand. Regenerate it with:

```sh
UPDATE_CLI_REFERENCE=true go test ./cmd/fizzle -run TestCLIReferenceIsCurrent
```

## `fizzle`

FZ series sampler disk and voice tool

Usage: `fizzle`

Flags:

- `--debug`: enable debug logging

## `fizzle disk`

manage FZ series floppy disk images

Usage: `fizzle disk`

## `fizzle disk add`

add a voice (.fzv) or full dump (.fzf) file to a disk image

Usage: `fizzle disk add IMAGE FILE`

Flags:

- `--disk-num`: which disk this is in a 2-disk split (1 = first/only disk, 2 = second disk)

Details and examples:

```text
Copy a voice or full dump file onto a disk image so the sampler can load it.
The file type is detected automatically from the file contents.

   IMAGE       the .img disk image file to add the file to
   FILE        the .fzv voice file or .fzf full dump file to copy onto the disk

   --disk-num  which disk this is in a 2-disk split: 1 for the first disk
               (default), 2 for the second disk

Example:
   fizzle disk add mydrums.img kick.fzv
   fizzle disk add --disk-num 2 jungle-2.img jungle-2.fzf
```

## `fizzle disk copy`

copy a named file from one disk image to another

Usage: `fizzle disk copy SRC-IMAGE NAME DEST-IMAGE`

Details and examples:

```text
Copy a file from one disk image to another in a single step.
Equivalent to 'disk get' followed by 'disk add'.

   SRC-IMAGE   the .img disk image file to copy from
   NAME        the name of the file on the source disk (case-insensitive)
   DEST-IMAGE  the .img disk image file to copy into

Example:
   fizzle disk copy library.img HOOVER mydrums.img
```

## `fizzle disk get`

extract a named file from a disk image

Usage: `fizzle disk get IMAGE NAME OUTPUT`

Details and examples:

```text
Extract a file from a disk image by its name on the disk.
Use 'fizzle disk ls' to see what files are on a disk and what they are named.
The name is matched case-insensitively.

   IMAGE   the .img disk image file to read from
   NAME    the name of the file as it appears on the disk (e.g. KICK)
   OUTPUT  the file to write on your computer (e.g. kick.fzv)

Example:
   fizzle disk get mydrums.img KICK kick.fzv
```

## `fizzle disk ls`

list the contents of a disk image

Usage: `fizzle disk ls IMAGE`

Flags:

- `--json`: output as JSON

Details and examples:

```text
List the disk label and all files stored in a disk image file.

   IMAGE  the .img disk image file to inspect

   --json  output as JSON instead of a table

Example:
   fizzle disk ls mydrums.img
   fizzle disk ls --json mydrums.img
```

## `fizzle disk new`

create a blank formatted disk image

Usage: `fizzle disk new LABEL IMAGE`

Details and examples:

```text
Create a blank 1.25 MB FZ series disk image file.

   LABEL  the name displayed on the sampler when this disk is loaded,
          up to 12 characters (e.g. "My Drums")
   IMAGE  the .img file to create on your computer. Copy this to a USB
          stick or floppy emulator to use with the sampler

Example:
   fizzle disk new "My Drums" mydrums.img
```

## `fizzle fzb`

work with FZ series bank dump files (.fzb)

Usage: `fizzle fzb`

## `fizzle fzb info`

show the voice map of a bank dump file

Usage: `fizzle fzb info FZB`

Flags:

- `--json`: output as JSON

## `fizzle fzf`

work with FZ series full dump files (.fzf)

Usage: `fizzle fzf`

## `fizzle fzf build`

pack individual voice files (.fzv) into a full dump (.fzf)

Usage: `fizzle fzf build OUTPUT VOICE [VOICE...]`

Details and examples:

```text
Pack one or more voice files into a single full dump file (.fzf).

A full dump loads all voices in one operation on the sampler, which is much
more convenient than loading voices one at a time. Once built, use
'fizzle disk add' to copy the dump onto a disk image.

   OUTPUT  the .fzf full dump file to create (up to 64 voices)
   VOICE   one or more .fzv voice files to pack in

Example:
   fizzle fzf build drums.fzf kick.fzv snare.fzv hihat.fzv
```

## `fizzle fzf edit`

modify voice parameters in a full dump file

Usage: `fizzle fzf edit FZF`

Flags:

- `--cutoff`: filter cutoff offset (0-127)
- `--dca-end`: dca envelope end point (0-7)
- `--dca-level-kf`: DCA level KF (-15 to +15)
- `--dca-rate-1`: dca rate for stage 1 (0 to 99)
- `--dca-rate-2`: dca rate for stage 2 (0 to 99)
- `--dca-rate-3`: dca rate for stage 3 (0 to 99)
- `--dca-rate-4`: dca rate for stage 4 (0 to 99)
- `--dca-rate-5`: dca rate for stage 5 (0 to 99)
- `--dca-rate-6`: dca rate for stage 6 (0 to 99)
- `--dca-rate-7`: dca rate for stage 7 (0 to 99)
- `--dca-rate-8`: dca rate for stage 8 (0 to 99)
- `--dca-rate-kf`: DCA rate KF (-15 to +15)
- `--dca-stop-1`: dca level for stage 1 (0 to 99)
- `--dca-stop-2`: dca level for stage 2 (0 to 99)
- `--dca-stop-3`: dca level for stage 3 (0 to 99)
- `--dca-stop-4`: dca level for stage 4 (0 to 99)
- `--dca-stop-5`: dca level for stage 5 (0 to 99)
- `--dca-stop-6`: dca level for stage 6 (0 to 99)
- `--dca-stop-7`: dca level for stage 7 (0 to 99)
- `--dca-stop-8`: dca level for stage 8 (0 to 99)
- `--dca-sustain`: dca envelope sustain point (0-7)
- `--dcf-end`: dcf envelope end point (0-7)
- `--dcf-level-kf`: DCF level KF (-15 to +15)
- `--dcf-rate-1`: dcf rate for stage 1 (0 to 99)
- `--dcf-rate-2`: dcf rate for stage 2 (0 to 99)
- `--dcf-rate-3`: dcf rate for stage 3 (0 to 99)
- `--dcf-rate-4`: dcf rate for stage 4 (0 to 99)
- `--dcf-rate-5`: dcf rate for stage 5 (0 to 99)
- `--dcf-rate-6`: dcf rate for stage 6 (0 to 99)
- `--dcf-rate-7`: dcf rate for stage 7 (0 to 99)
- `--dcf-rate-8`: dcf rate for stage 8 (0 to 99)
- `--dcf-rate-kf`: DCF rate KF (-15 to +15)
- `--dcf-stop-1`: dcf level for stage 1 (0 to 99)
- `--dcf-stop-2`: dcf level for stage 2 (0 to 99)
- `--dcf-stop-3`: dcf level for stage 3 (0 to 99)
- `--dcf-stop-4`: dcf level for stage 4 (0 to 99)
- `--dcf-stop-5`: dcf level for stage 5 (0 to 99)
- `--dcf-stop-6`: dcf level for stage 6 (0 to 99)
- `--dcf-stop-7`: dcf level for stage 7 (0 to 99)
- `--dcf-stop-8`: dcf level for stage 8 (0 to 99)
- `--dcf-sustain`: dcf envelope sustain point (0-7)
- `--key-high`: highest MIDI note (0-127)
- `--key-low`: lowest MIDI note (0-127)
- `--lfo-amp`: LFO amplitude depth (0-127)
- `--lfo-delay`: LFO delay on the panel's scale (0-127); the same row sets the attack
- `--lfo-filter`: LFO filter depth (0-127)
- `--lfo-pitch`: LFO pitch depth (0-127)
- `--lfo-rate`: LFO rate (0-127)
- `--lfo-sync`: LFO phase sync: on, off
- `--lfo-wave`: LFO waveform: sine, saw-up, saw-down, triangle, rectangle, random
- `--name`: voice name (max 12 characters)
- `--playback-mode`: playback mode: normal, reverse, cue, synth
- `--resonance`: filter resonance (0-127)
- `--root`: root key MIDI note (0-127)
- `--tune`: voice tuning in cents, as the panel shows it (-100 to +100)
- `--vel-dca-kf`: velocity to amplitude (-127 to +127)
- `--vel-dca-rs`: initial-touch amp rate scale (-127 to +127)
- `--vel-dcf-kf`: velocity to filter (-127 to +127)
- `--vel-dcf-rs`: initial-touch DCF rate scale (-127 to +127)
- `--vel-dcq-kf`: initial-touch DCQ follow (0 to 127)
- `--voice`: voice name to target (exact, case-insensitive)

Details and examples:

```text
Modify parameters of a voice inside an FZF full dump file in place.
The voice is identified by --voice (case-insensitive, as shown in 'fzf info').
Only the specified flags are changed; all other parameters are preserved.

   FZF  the .fzf full dump file to modify

Example:
   fizzle fzf edit drums.fzf --voice "PAD" --lfo-wave sine --lfo-rate 25
   fizzle fzf edit drums.fzf --voice "KICK" --cutoff 64 --resonance 7
```

## `fizzle fzf effects`

view or set the global effect parameters (bend, mod, foot, aftertouch)

Usage: `fizzle fzf effects FZF`

Flags:

- `--aftertouch-dca`: aftertouch to amp offset (0-127)
- `--aftertouch-dcf`: aftertouch to filter offset (0-127)
- `--aftertouch-dcq`: aftertouch to resonance offset (0-127)
- `--aftertouch-lfa`: aftertouch to LFO amp depth (0-127)
- `--aftertouch-lff`: aftertouch to LFO filter depth (0-127)
- `--aftertouch-lfp`: aftertouch to LFO pitch depth (0-127)
- `--aftertouch-lfq`: aftertouch to LFO resonance depth (0-127)
- `--bend`: pitch bend range in 1/8-semitone units (0-127)
- `--foot-dca`: foot pedal to amp offset (volume) (0-127)
- `--foot-dcf`: foot pedal to filter offset (0-127)
- `--foot-dcq`: foot pedal to resonance offset (0-127)
- `--foot-lfa`: foot pedal to LFO amp depth (0-127)
- `--foot-lff`: foot pedal to LFO filter depth (0-127)
- `--foot-lfp`: foot pedal to LFO pitch depth (0-127)
- `--foot-lfq`: foot pedal to LFO resonance depth (0-127)
- `--mod-dca`: mod wheel to amp offset (0-127)
- `--mod-dcf`: mod wheel to filter offset (0-127)
- `--mod-dcq`: mod wheel to resonance offset (0-127)
- `--mod-lfa`: mod wheel to LFO amp depth (0-127)
- `--mod-lff`: mod wheel to LFO filter depth (0-127)
- `--mod-lfp`: mod wheel to LFO pitch depth (0-127)
- `--mod-lfq`: mod wheel to LFO resonance depth (0-127)

Details and examples:

```text
View or modify the global effect block in a full dump file.

The effect block controls how performance controllers are routed to the
synthesis engine. Three controllers (mod wheel, foot pedal, aftertouch)
each route to seven targets: LFO pitch/amp/filter/resonance and amp/
filter/resonance offset. Plus the global pitch bend range.

The --bend flag is in 1/8-semitone units, so 24 = 3 semitones and
48 = 6 semitones.

With no flags, the current effect parameters are displayed.

   FZF  the .fzf full dump file to inspect or modify (modified in place)

Example:
   fizzle fzf effects drums.fzf --bend 48
   fizzle fzf effects drums.fzf --mod-lfa 30 --aftertouch-dcf 20
```

## `fizzle fzf info`

show the voice map of a full dump file

Usage: `fizzle fzf info FZF`

Flags:

- `--json`: output as JSON

Details and examples:

```text
Display all voices in a full dump file as a table showing each voice's
name, key range, sample rate, and duration. Root key and velocity columns
appear only when they carry useful information. Voices with sustain loops
are marked in the duration column.

This gives you a complete map of the instrument.

   FZF  the .fzf full dump file to inspect

   --json  output as JSON instead of a table

Example:
   fizzle fzf info drums.fzf
   fizzle fzf info --json drums.fzf
```

## `fizzle fzf midi`

set the MIDI receive channel for one or more voices

Usage: `fizzle fzf midi FZF`

Flags:

- `--all`: target all voices
- `--channel`: MIDI receive channel (1-16)
- `--voice`: voice name to target (exact, case-insensitive, repeatable)

Details and examples:

```text
Set the MIDI receive channel for voices in a full dump file.

The FZ-1 responds to note-on/off events on each voice's assigned channel,
allowing independent pitch bend and expression per voice group. For example,
assign your bass voice to channel 2 and send pitch bend only on channel 2
to bend the bass without affecting the drums.

Use 'fzf info' to see voice names before running this command.

   FZF           the .fzf full dump file to modify (modified in place)

   --voice NAME  voice name to target, exactly as shown in 'fzf info'
                 (case-insensitive, repeatable for multiple voices)
   --all         target all voices (use with --channel 1 to reset)
   --channel N   MIDI receive channel to assign (1-16, required)

Example:
   fizzle fzf midi drums.fzf --voice "REESE" --channel 2
   fizzle fzf midi drums.fzf --voice "808" --voice "REESE" --channel 2
   fizzle fzf midi drums.fzf --all --channel 1
```

## `fizzle fzf output`

set the output (generator channel) for one or more voices

Usage: `fizzle fzf output FZF`

Flags:

- `--all`: target all voices
- `--output`: output assignment: 1-8, comma-separated, or 'all'
- `--voice`: voice name to target (exact, case-insensitive, repeatable)

Details and examples:

```text
Set the output assignment for voices in a full dump file.

The FZ-1 has 8 voice generators, each feeding an individual output jack
(1-8) on the back panel. Assigning a voice to a single output makes it
monophonic on that output: a new note cuts the previous one. Voices
sharing the same output mute each other. Assigning multiple outputs
gives limited polyphony across those outputs. 'all' enables all 8
outputs.

Use 'fzf info' to see voice names and current output assignments.

   FZF            the .fzf full dump file to modify (modified in place)

   --voice NAME   voice name to target, exactly as shown in 'fzf info'
                  (case-insensitive, repeatable for multiple voices)
   --all          target all voices
   --output VAL   output assignment: 1-8 (single), 1,3,5 (multiple),
                  or 'all' (all 8 outputs). Required.

Example:
   fizzle fzf output drums.fzf --voice "REESE" --output 2
   fizzle fzf output drums.fzf --voice "PAD" --output 1,3,5
   fizzle fzf output drums.fzf --all --output all
```

## `fizzle fzf unpack`

extract individual voice files (.fzv) from a full dump (.fzf)

Usage: `fizzle fzf unpack FZF OUTPUTDIR`

Flags:

- `--bank`: extract only voices from the given bank (1-based; 0 means all banks)
- `--disk2`: disk 2 image path for multi-disk unpack

Details and examples:

```text
Extract all voices from a full dump file into individual .fzv files,
one file per voice, each named after the voice as stored in the dump.

   FZF        the .fzf full dump file to unpack (or a disk 1 .img when using --disk2)
   OUTPUTDIR  the directory to write the .fzv files into (created if it does not exist)

   --disk2    path to the disk 2 .img file for a multi-disk full dump; the first
              argument is treated as the disk 1 .img path and voices are extracted
              with audio from both disks

Example:
   fizzle fzf unpack drums.fzf ./voices/
   fizzle fzf unpack JUNGLISM-1.img --disk2 JUNGLISM-2.img ./voices/
```

## `fizzle fzv`

work with FZ series voice files (.fzv)

Usage: `fizzle fzv`

## `fizzle fzv edit`

modify voice parameters in an FZV file

Usage: `fizzle fzv edit FZV`

Flags:

- `--cutoff`: filter cutoff offset (0-127)
- `--dca-end`: dca envelope end point (0-7)
- `--dca-level-kf`: DCA level KF (-15 to +15)
- `--dca-rate-1`: dca rate for stage 1 (0 to 99)
- `--dca-rate-2`: dca rate for stage 2 (0 to 99)
- `--dca-rate-3`: dca rate for stage 3 (0 to 99)
- `--dca-rate-4`: dca rate for stage 4 (0 to 99)
- `--dca-rate-5`: dca rate for stage 5 (0 to 99)
- `--dca-rate-6`: dca rate for stage 6 (0 to 99)
- `--dca-rate-7`: dca rate for stage 7 (0 to 99)
- `--dca-rate-8`: dca rate for stage 8 (0 to 99)
- `--dca-rate-kf`: DCA rate KF (-15 to +15)
- `--dca-stop-1`: dca level for stage 1 (0 to 99)
- `--dca-stop-2`: dca level for stage 2 (0 to 99)
- `--dca-stop-3`: dca level for stage 3 (0 to 99)
- `--dca-stop-4`: dca level for stage 4 (0 to 99)
- `--dca-stop-5`: dca level for stage 5 (0 to 99)
- `--dca-stop-6`: dca level for stage 6 (0 to 99)
- `--dca-stop-7`: dca level for stage 7 (0 to 99)
- `--dca-stop-8`: dca level for stage 8 (0 to 99)
- `--dca-sustain`: dca envelope sustain point (0-7)
- `--dcf-end`: dcf envelope end point (0-7)
- `--dcf-level-kf`: DCF level KF (-15 to +15)
- `--dcf-rate-1`: dcf rate for stage 1 (0 to 99)
- `--dcf-rate-2`: dcf rate for stage 2 (0 to 99)
- `--dcf-rate-3`: dcf rate for stage 3 (0 to 99)
- `--dcf-rate-4`: dcf rate for stage 4 (0 to 99)
- `--dcf-rate-5`: dcf rate for stage 5 (0 to 99)
- `--dcf-rate-6`: dcf rate for stage 6 (0 to 99)
- `--dcf-rate-7`: dcf rate for stage 7 (0 to 99)
- `--dcf-rate-8`: dcf rate for stage 8 (0 to 99)
- `--dcf-rate-kf`: DCF rate KF (-15 to +15)
- `--dcf-stop-1`: dcf level for stage 1 (0 to 99)
- `--dcf-stop-2`: dcf level for stage 2 (0 to 99)
- `--dcf-stop-3`: dcf level for stage 3 (0 to 99)
- `--dcf-stop-4`: dcf level for stage 4 (0 to 99)
- `--dcf-stop-5`: dcf level for stage 5 (0 to 99)
- `--dcf-stop-6`: dcf level for stage 6 (0 to 99)
- `--dcf-stop-7`: dcf level for stage 7 (0 to 99)
- `--dcf-stop-8`: dcf level for stage 8 (0 to 99)
- `--dcf-sustain`: dcf envelope sustain point (0-7)
- `--key-high`: highest MIDI note (0-127)
- `--key-low`: lowest MIDI note (0-127)
- `--lfo-amp`: LFO amplitude depth (0-127)
- `--lfo-delay`: LFO delay on the panel's scale (0-127); the same row sets the attack
- `--lfo-filter`: LFO filter depth (0-127)
- `--lfo-pitch`: LFO pitch depth (0-127)
- `--lfo-rate`: LFO rate (0-127)
- `--lfo-sync`: LFO phase sync: on, off
- `--lfo-wave`: LFO waveform: sine, saw-up, saw-down, triangle, rectangle, random
- `--name`: voice name (max 12 characters)
- `--playback-mode`: playback mode: normal, reverse, cue, synth
- `--resonance`: filter resonance (0-127)
- `--root`: root key MIDI note (0-127)
- `--tune`: voice tuning in cents, as the panel shows it (-100 to +100)
- `--vel-dca-kf`: velocity to amplitude (-127 to +127)
- `--vel-dca-rs`: initial-touch amp rate scale (-127 to +127)
- `--vel-dcf-kf`: velocity to filter (-127 to +127)
- `--vel-dcf-rs`: initial-touch DCF rate scale (-127 to +127)
- `--vel-dcq-kf`: initial-touch DCQ follow (0 to 127)

Details and examples:

```text
Modify parameters of an FZ voice file in place. Only the specified
flags are changed; all other parameters are preserved.

   FZV  the .fzv voice file to modify

Example:
   fizzle fzv edit pad.fzv --lfo-wave sine --lfo-rate 25 --lfo-filter 50
   fizzle fzv edit kick.fzv --cutoff 64 --resonance 7
   fizzle fzv edit pad.fzv --name "MY PAD"
   fizzle fzv edit pad.fzv --dca-sustain 2 --dca-end 3
   fizzle fzv edit pad.fzv --dca-rate-1 99 --dca-stop-1 85
```

## `fizzle fzv extract`

extract audio from a voice file as a WAV file

Usage: `fizzle fzv extract FZV WAV`

Details and examples:

```text
Extract the PCM audio from an FZ voice file and write it as a standard
16-bit mono WAV file that any audio software can open.

   FZV  the .fzv voice file to read (from the sampler or a disk image)
   WAV  the .wav file to write on your computer

Example:
   fizzle fzv extract kick.fzv kick.wav
```

## `fizzle fzv import`

convert a WAV file into an FZ voice file (.fzv)

Usage: `fizzle fzv import WAV FZV`

Flags:

- `--rate`: target sample rate: 36000, 18000, or 9000

Details and examples:

```text
Convert a 16-bit mono WAV file into an FZ voice file that can be loaded
onto the sampler. The WAV is resampled to the target rate if needed.

The sampler supports three sample rates. Higher rates use more memory but
sound better. 36000 Hz is the highest quality and the default.

   WAV    the WAV file to convert (16, 24, or 32-bit mono PCM)
   FZV    the .fzv voice file to write. Use 'fizzle disk add' to put it on a disk

   --rate  sample rate to encode at: 36000, 18000, or 9000 Hz (default: 36000)

Example:
   fizzle fzv import kick.wav kick.fzv
   fizzle fzv import --rate 18000 kick.wav kick.fzv
```

## `fizzle fzv info`

show details about a voice file

Usage: `fizzle fzv info FZV`

Flags:

- `--json`: output as JSON

Details and examples:

```text
Display the parameters stored in a voice file: sample rate, length,
duration, key range, root key, envelope settings, and loop configuration.

   FZV  the .fzv voice file to inspect

   --json  output as JSON instead of formatted text

Example:
   fizzle fzv info kick.fzv
   fizzle fzv info --json kick.fzv
```

## `fizzle fzv play`

play the audio from a voice file through system speakers

Usage: `fizzle fzv play FZV`

Details and examples:

```text
Play the audio from an FZ voice file through the system audio device.
Plays only the generator range (genst to gened), matching what the
FZ hardware plays on note-on. Uses native audio on macOS and Windows;
on Linux, requires aplay, paplay, or ffplay.

   FZV  the .fzv voice file to play

Example:
   fizzle fzv play kick.fzv
```

## `fizzle licenses`

show the project license and third-party attribution

Usage: `fizzle licenses`

Details and examples:

```text
Print the project license followed by the full text of every
third-party dependency's license. The output satisfies the
attribution clauses of the permissive licenses in the dependency
graph (MIT, BSD, Apache-2.0).

Example:
   fizzle licenses
```

## `fizzle sfz`

convert SFZ instruments to FZ series format

Usage: `fizzle sfz`

## `fizzle sfz convert`

convert an SFZ instrument or WAV directory into an FZ series full dump (.fzf)

Usage: `fizzle sfz convert SFZ-OR-DIR OUTPUT`

Flags:

- `--fit-to-disk`: automatically reduce sample rate if needed to fit on a floppy disk
- `--rate`: sample rate: 36000, 18000, or 9000
- `--split-disks`: split across 2 floppy disk images; OUTPUT is the filename prefix (produces PREFIX-1.img and PREFIX-2.img)

Details and examples:

```text
Convert an SFZ instrument file or a directory of WAV files into a full dump.

When given an .sfz file, each region becomes one voice with its key range,
velocity range, and root key preserved. WAV files are read automatically.

When given a directory, all .wav files in the directory are loaded in
alphabetical order and assigned to sequential keys starting at C2 (MIDI 36).
This is the zero-SFZ workflow for simple drum kits.

Unsupported SFZ features are reported as warnings but do not stop conversion.
The output can then be added to a disk image with 'fizzle disk add'.

   SFZ-OR-DIR  the .sfz instrument file or directory of WAV files to convert
   OUTPUT      the .fzf full dump file to write

   --rate         sample rate: 36000, 18000, or 9000 Hz (default: 36000)
   --fit-to-disk  step down the sample rate automatically if the output would
                  not fit on a single 1.25 MB floppy disk; --rate sets the
                  ceiling (never upsampled, may downsample to 18000 or 9000)
   --split-disks  split across 2 floppy disk images (the FZ series maximum,
                   limited by its 2 MB of sample RAM); OUTPUT becomes the
                   filename prefix and produces OUTPUT-1.img and OUTPUT-2.img
                   ready for a Gotek or floppy emulator.
                   Cannot be used with --fit-to-disk.

Example:
   fizzle sfz convert drums.sfz drums.fzf
   fizzle sfz convert --fit-to-disk jungle.sfz jungle.fzf
   fizzle sfz convert --rate 36000 --split-disks jungle.sfz JUNGLISM
   fizzle sfz convert ./my-samples/ mykit.fzf
```

## `fizzle sfz export`

export a full dump as an SFZ instrument with WAV files

Usage: `fizzle sfz export FZF OUTPUT_DIR`

Flags:

- `--name`: SFZ filename (without extension)

Details and examples:

```text
Export all voices from a full dump file as an SFZ instrument.
Each voice is extracted as a WAV file and an SFZ file is generated
mapping them to their original key ranges, velocities, and synthesis
parameters.

   FZF         the .fzf full dump file to export
   OUTPUT_DIR  directory to write the .sfz and .wav files to

   --name NAME  name for the SFZ file (default: derived from FZF filename)

Example:
   fizzle sfz export drums.fzf ./my-instrument/
   fizzle sfz export --name mykit drums.fzf ./output/
```
