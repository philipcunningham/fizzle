# Casio FZ Series Disk and Voice Format

Primary reference: [casio-fz1-data-structures.pdf](casio-fz1-data-structures.pdf) (T. Sasaki, Casio R&D, March 1987). A clean machine-transcribed copy lives at [casio-fz1-data-structures.md](casio-fz1-data-structures.md), useful for grep and side-by-side reading. The PDF is the authority where they disagree.

This document describes both the spec layout and the hardware-observed deviations. Where the two disagree, fizzle follows what real hardware actually writes (verified against the test fixtures in `testdata/`).

## Conventions inherited from the V50 CPU

The FZ-1 CPU is a NEC µPD70216 (V50), a 16-bit microprocessor that runs the V20/V30 (8086-compatible) instruction set. The on-chip peripherals (DMA controller, serial unit (SCU), timer (TCU), interrupt controller (ICU)) handle floppy I/O, MIDI, and the front panel.

Three V50 conventions show up directly in the file format:

**Endianness is little-endian.** All multi-byte integers in FZF/FZV/disk-image structures store the low byte at the lower address. This is the 8086 convention the V50 inherits.

**The spec's type sizes are HP 64000 / 70116 cross-compiler conventions**, not standard C:

- `long` = 4 bytes
- `int` = 2 bytes
- `short` = **1 byte**

A reader interpreting `struct voicedata` with modern C semantics (where `short` is typically 2 bytes) would shift every offset past `loop_sus` (0x12) and misread the entire header. The spec's offset comments are the source of truth.

**16-bit fields are aligned on even offsets** within every FZ structure. The V50 has a 16-bit data bus; word access at an even address completes in one bus cycle, at an odd address it takes two. Casio's engineers laid the structs out so every `long` and `int` starts on an even byte offset (`wavst` at 0x00, `loop` at 0x10, `dcp` at 0x74, `lfo_delay` at 0x9c, etc.). Single-byte `short` fields are packed wherever convenient. fizzle reads at the same documented offsets; no alignment translation is needed.

**"Word Address" in the spec means a 16-bit-word index**, not a byte offset. Wave-address fields (`wavst`, `waved`, `genst`, `gened`) and loop-address fields (`loopst[]`, `looped[]`) hold sample indices into the 16-bit PCM stream. To convert to a byte offset within the audio area, multiply by 2 (or by `disk.BytesPerSample`). This matches what fizzle's code does throughout.

---

## Disk Image

The FZ series formats 3.5" HD disks as:

- Double-sided, 80 tracks, 8 sectors per track, 1024 bytes per sector
- 1280 logical sectors total, 1,310,720 bytes
- Physical sector interleave: 3:1

Logical sector address: `loc = (track * 16) + (side * 8) + (sector - 1)`

Sectors 0 and 1 are reserved. Sectors 2-1279 hold file data (1,278 usable sectors, ~1.25 MB).

### Sector 0: Head

| Offset | Size | Field |
|--------|------|-------|
| 0x000 | 12 | Disk name (ASCII, space-padded) |
| 0x00c | 2 | Password (ASCII, space-padded; only 2 bytes before the name tag) |
| 0x00e | 1 | Disk name tag (always 0x02) |
| 0x010 | 12 | Disk name copy |
| 0x080 | 896 | CAT bitmap |

The CAT (Cluster Allocation Table) is a bitmap where each bit represents one logical sector. Bit set = allocated. Bit 0 of byte 0 corresponds to sector 0. During formatting, bytes from offset 0x120 to end of sector are set to 0xff to mark beyond-physical sectors as allocated.

### Sector 1: Directory

Up to 64 entries, 16 bytes each:

| Offset | Size | Field |
|--------|------|-------|
| 0 | 12 | File name (ASCII, space-padded) |
| 12 | 1 | File type (see below) |
| 13 | 1 | Disk number (0 = first disk, 1 = second, 2 = third, ...) |
| 14 | 2 | DIS sector location (logical sector index) |

An entry is empty if byte 0 is null. File types:

| Value | Type |
|-------|------|
| 0 | Full Dump |
| 1 | Voice |
| 2 | Bank |
| 3 | Effect |
| 4 | Sequence |
| 5 | Program |

**Full dumps must use the name `FULL-DATA-FZ`.** The firmware identifies full dump files by this exact name. Any other name causes the sampler to treat the file as a multi-disk save and prompt "Next disk?".

### Data Information Sector (DIS) / "File Head"

The spec calls this the "file head" (Section 1-4 of the source PDF). It is one 1024-byte sector that contains the file's extent map plus three section-size counts (`vn`, `bn`, `wn`).

Per the spec, the layout is:

| Offset | Size | Field |
|--------|------|-------|
| 0x000 | 256 | dBP area: up to 64 extent entries of `[ss, es]` (uint16 each) |
| 0x100 | 2 | vn: voice count |
| 0x102 | 2 | bn: bank count |
| 0x104 | 2 | wn: wave block count |
| 0x106 | 762 | Unused |

Each directory entry points to a DIS / file-head sector. The extent entries describe the file's sector chain, the counts describe section sizes within the chain.

**Real hardware deviates in two ways:**

1. **Extent area extends to offset 0x3F9** (1018 bytes, allowing up to ~254 extents) rather than ending at 0x100. fizzle reads the extent list until the first `[0, 0]` pair or until offset 0x3FA, whichever comes first.

2. **Counts are written at the end of the sector**, in the order `bn vn wn` rather than the spec's `vn bn wn`:

   | Offset | Field |
   |--------|-------|
   | 0x3FA | bn (bank count, uint16) |
   | 0x3FC | vn (voice count, uint16) |
   | 0x3FE | wn (wave sector count, uint16) |

fizzle reads from the hardware offsets. The spec layout is documented for reference; it is not what the FZ-1 firmware emits.

**The DIS sector is itself the start of the file's sector chain.** Specifically, the DIS sector index is `ss` of the first extent; file content (bank sector(s), voice headers, audio) begins at `ss + 1`. The DIS is included in the chain but is skipped when reading content. This matches what hobbyist tools like Jacob Vosmaer's `fz1` infer.

**Standalone FZF files lose the DIS.** When fizzle's `disk get` extracts a file from a disk image, it copies only the content sectors, not the DIS. Standalone `.fzf` files therefore have no top-level `vn` / `bn` / `wn`. Section sizes must be inferred from the content itself (see [Voice-area sizing](#voice-area-sizing) below).

---

## FZF: Full Dump File

A full dump contains one or more bank sectors, a voice parameter area, and a raw PCM audio area, concatenated in that order.

### Bank Sector(s)

One 1024-byte sector per bank, up to 8 banks. Each bank sector is `struct bankdata` from the spec:

| Offset | Size | Field |
|--------|------|-------|
| 0x000 | 2 | `bstep`: number of key splits (or voices) the bank uses; 0-64 |
| 0x002 | 64 | `hwid[64]`: key high limit per key split |
| 0x042 | 64 | `lwid[64]`: key low limit per key split |
| 0x082 | 64 | `htch[64]`: velocity high limit per key split |
| 0x0c2 | 64 | `ltch[64]`: velocity low limit per key split |
| 0x102 | 64 | `cent[64]`: root key per key split |
| 0x142 | 64 | `mchn[64]`: MIDI receive channel (0-15) per key split |
| 0x182 | 64 | `gchn[64]`: generator channel bitmask per key split |
| 0x1c2 | 64 | `bvol[64]`: output level per key split (0-127) |
| 0x202 | 128 | `vp[64]`: voice slot index per key split (uint16 each); see below |
| 0x282 | 14 | Bank name (12 ASCII + 2 null bytes) |
| 0x290 | 4 | Multi-disk wave total (uint32): total wave sectors across all disks; 0 for single-disk dumps. Written by `fizzle` so `diskadd` can set the correct DIS `wn` on disk 1. |
| 0x3c0 | 24 | Effect parameters (`struct efectdata`) |

Key and velocity values use MIDI note numbers (0-127). All per-key-split arrays (`hwid`, `lwid`, …, `vp`) are indexed by key split number (0 to `bstep - 1`), not by voice slot number. The mapping is `vp[]` (see below).

**`bstep` is per-bank and counts key splits, not voices.** Spec wording (section 2-2): *"Denotes the current number of key splits or the number of voices which the bank uses and takes a number among 0 thru 64. The number 0 denotes that the current bank is not yet defined."* In the simplest case (a single-bank kit where every key split uses a distinct voice) `bstep` happens to equal the file's voice count, which is why the two are sometimes used interchangeably. They are not the same field, and for most real dumps they diverge (see [the sizing note below](#bstep-key-splits-vs-voices)).

**`vp[i]` (offset 0x202, uint16) maps key split `i` to a voice slot index.** When parameters are dumped from internal memory to disk or over MIDI/External Port (spec section 2-2), the entries hold a voice number (0-63), not an in-memory address. Multiple key splits can share a voice via `vp`: for instance, an open and closed hi-hat that should mute each other can both point at the same voice slot in `vp`, then use distinct `gchn` bits to enforce the mute group. Factory drum kits use this heavily; `Drums.fzf` from the FL-4 factory disk has `bstep = 61` with `vp[]` referencing only 19 distinct voice slots (max index 23).

**Note: the `mchn` array is at 0x142, not 0x104.** Writing to 0x104 corrupts `cent[2]` onward, zeroing all root keys past voice 2 and causing severe pitch errors on hardware.

**`gchn` (generator channel bitmask, labelled "OUTPUT" on the front panel):**

Each of the 8 voice generators feeds an individual output jack (1-8) on the
back panel. The `gchn` byte is a bitmask: bit 0 = output 1, bit 7 = output 8.
The hardware front panel displays 8 positions, each shown as a filled circle
(assigned) or a dot (inactive).

- `0xff`: all 8 outputs active (round-robin across generators)
- Single bit (e.g. `0x01`): one output, monophonic. New notes on this output cut off the previous note. Voices sharing the same single-bit value mute each other.
- Multiple bits (e.g. `0x05` = outputs 1 and 3): limited polyphony across the selected outputs.

This maps naturally to the SFZ `mutegroup=N` opcode: each mutegroup is assigned a separate output during SFZ conversion.

**Effect data** at 0x3c0 is `struct effectdata` (24 bytes). Each field is 1 byte; the spec defines them as `short` in the C struct but its prose clarifies "the size of every factor is all 1 byte" (section 2-3).

| Offset | Field | Description |
|--------|-------|-------------|
| 0x00 | `bend` | Pitch-bend depth in 1/8-semitone units (0-127) |
| 0x01 | `mvol` | Master volume. **Unused; spec says "normally 0 is placed".** |
| 0x02 | `suss` | Sustain switch. **Unused; spec says "normally 0 is placed".** |
| 0x03 | `mod_lfp` | Mod wheel to LFO pitch (0-127) |
| 0x04 | `mod_lfa` | Mod wheel to LFO amp |
| 0x05 | `mod_lff` | Mod wheel to LFO filter |
| 0x06 | `mod_lfq` | Mod wheel to LFO resonance (Q) |
| 0x07 | `mod_dcf` | Mod wheel to filter offset |
| 0x08 | `mod_dca` | Mod wheel to amp offset |
| 0x09 | `mod_dcq` | Mod wheel to resonance offset |
| 0x0a | `fot_lfp` | Foot pedal to LFO pitch |
| 0x0b | `fot_lfa` | Foot pedal to LFO amp |
| 0x0c | `fot_lff` | Foot pedal to LFO filter |
| 0x0d | `fot_lfq` | Foot pedal to LFO Q |
| 0x0e | `fot_dca` | Foot pedal to amp offset |
| 0x0f | `fot_dcf` | Foot pedal to filter offset |
| 0x10 | `fot_dcq` | Foot pedal to resonance offset |
| 0x11 | `aft_lfp` | Aftertouch to LFO pitch |
| 0x12 | `aft_lfa` | Aftertouch to LFO amp |
| 0x13 | `aft_lff` | Aftertouch to LFO filter |
| 0x14 | `aft_lfq` | Aftertouch to LFO Q |
| 0x15 | `aft_dca` | Aftertouch to amp offset |
| 0x16 | `aft_dcf` | Aftertouch to filter offset |
| 0x17 | `aft_dcq` | Aftertouch to resonance offset |

Three controllers (mod wheel, foot pedal, aftertouch) by seven LFO/DC targets each, plus pitch-bend and the two unused legacy fields = 24 bytes. fizzle exposes four representative fields via `fzf effects`: `bend`, `mod_lfp`, `fot_dca`, `aft_lfp`. The other 17 controller-routing fields aren't surfaced by the current CLI but are preserved on round-trip.

Working defaults confirmed against real hardware:

```
18 00 00 0f 00 00 00 00  00 00 00 00 00 00 40 00
00 08 00 00 00 00 00 00
```

`bend=24` (pitch bend range ~3 semitones), `mod_lfp=15`, `fot_dca=64`, `aft_lfp=8`.

### Voice Parameter Area

`ceil(vn / 4)` sectors, each holding 4 voice headers packed at 256-byte intervals. Only the first 192 bytes of each 256-byte slot are used (the voice header). The remaining 64 bytes are unused.

Voice `i` is at byte offset `(i / 4) * 1024 + (i % 4) * 256` within the voice area.

Sample pointer fields in the voice headers are **cumulative absolute addresses** relative to the start of the audio area. Voice 0 has `wavst=0`, voice 1 has `wavst` equal to the sector-aligned byte count of voice 0's audio divided by 2, and so on.

#### Voice-area sizing

Per spec (section 1-5, "Full Data ext=0" diagram), the voice parameter area is `(vn + 3) / 4` sectors long, where `vn` is the file's voice count read from the DIS / file head.

**Standalone `.fzf` files do not carry `vn`**: the file head is discarded by `disk get`. `vn` must be inferred from the file content.

A naive inference is "use `bstep` from bank 0", which fizzle did until [the realisation that `bstep` is a per-bank key-split count, not a file-level voice count](#bstep-key-splits-vs-voices). Files where a single bank uses more key splits than distinct voices (`vp[]` repeats) overshoot the real voice area, and the parser reads audio bytes as if they were voice headers. `Drums.fzf` from the FL-4 factory disk is the canonical example: `bstep = 61`, real `vn ≈ 28`, audio mistaken for headers at slots 28-60.

The robust inference: walk voice slots from 0 upward and accept slots whose 192-byte header passes a plausibility check (rate index in {0,1,2}, wave pointers monotonic, playback mode known, name printable or padded), stopping at the first slot that fails. The count of accepted slots is the inferred `vn`.

### Audio Area

Raw 16-bit mono PCM, one block per voice. Each block is padded to a 1024-byte sector boundary. Samples are little-endian int16, upper byte at the higher address.

The three sample rates are encoded as an index: `0` = 36 kHz, `1` = 18 kHz, `2` = 9 kHz.

---

## FZV: Voice File

A voice file is one 1024-byte header sector followed by the raw audio. The header is `struct voicedata` from the spec, 192 bytes total (0xC0), zero-padded to 1024 bytes.

### Voice Header (struct voicedata)

| Offset | Type | Field | Notes |
|--------|------|-------|-------|
| 0x00 | long | `wavst` | Wave start address (samples from audio area start) |
| 0x04 | long | `waved` | Wave end address (samples; exclusive) |
| 0x08 | long | `genst` | Generator start: first sample the sampler plays on note-on |
| 0x0c | long | `gened` | Generator end: last sample played; loop points reference this as the tail |
| 0x10 | int | `loop` | Playback mode (see below) |
| 0x12 | short | `loop_sus` | Sustain loop number (0-8; 8 = no loop) |
| 0x13 | short | `loop_end` | Loop end number |
| 0x14 | long[8] | `loopst[8]` | Loop start addresses |
| 0x34 | long[8] | `looped[8]` | Loop end addresses |
| 0x54 | int[8] | `loopxf[8]` | Loop crossfade times |
| 0x64 | uint[8] | `looptm[8]` | Loop times |
| 0x74 | int | `dcp` | Pitch (1/256 semitone units; transpose = dcp/256) |
| 0x76 | short | `dcf` | Filter cutoff offset (0-127) |
| 0x77 | short | `dcq` | Filter resonance offset (full byte 0-127; see Real-World Findings) |
| 0x78 | short | `dca_sus` | DCA envelope sustain point (0-7) |
| 0x79 | short | `dca_end` | DCA envelope end point (0-7) |
| 0x7a | short[8] | `dca_rate[8]` | DCA rates; MSB=0 rising, MSB=1 falling, low 7 bits = value |
| 0x82 | ushort[8] | `dca_stop[8]` | DCA stop levels (0-255) |
| 0x8a | short | `dcf_sus` | DCF envelope sustain point |
| 0x8b | short | `dcf_end` | DCF envelope end point |
| 0x8c | short[8] | `dcf_rate[8]` | DCF rates |
| 0x94 | ushort[8] | `dcf_stop[8]` | DCF stop levels |
| 0x9c | uint | `lfo_delay` | LFO delay before onset, in 2ms units (2 bytes) |
| 0x9e | ushort | `lfo_name` | LFO waveform (0-5); MSB = phase sync on/off |
| 0x9f | ushort | `lfo_atck` | LFO attack rate |
| 0xa0 | short | `lfo_rate` | LFO frequency |
| 0xa1 | short | `lfo_dcp` | LFO pitch depth |
| 0xa2 | short | `lfo_dca` | LFO amplitude depth |
| 0xa3 | short | `lfo_dcf` | LFO filter depth |
| 0xa4 | short | `lfo_dcq` | LFO resonance depth |
| 0xa5 | short | `vel_dcq_kf` | Velocity to resonance |
| 0xa6 | short | `dca_kf` | Key follow to amplitude |
| 0xa7 | short | `dca_rs` | Rate scaling for amplitude envelope |
| 0xa8 | short | `dcf_kf` | Key follow to filter |
| 0xa9 | short | `dcf_ns` (spec: `dcf_rs`) | Note rate scaling for filter |
| 0xaa | short | `vel_dca_kf` | Velocity to amplitude (signed; positive = louder at higher velocity) |
| 0xab | short | `vel_dca_rs` | Velocity to amplitude envelope rate |
| 0xac | short | `vel_dcf_kf` | Velocity to filter |
| 0xad | short | `vel_dcf_rs` | Velocity to filter envelope rate |
| 0xae | ushort | `hwid` | Highest MIDI key (voice range high) |
| 0xaf | ushort | `lwid` | Lowest MIDI key (voice range low) |
| 0xb0 | ushort | `cent` | Root key (MIDI note number) |
| 0xb1 | ushort | `samp` | Sample rate index (0/1/2 = 36k/18k/9kHz) |
| 0xb2 | char[14] | `name` | Voice name, 12 ASCII bytes + 2 null bytes |

Loop mode values (`loop` at 0x10):

| Value | Mode |
|-------|------|
| 0x0000 | No sound (voice slot undefined) |
| 0x01D7 | Normal |
| 0x101D | Reverse |
| 0x2014 | Cue |
| 0x0013 | Synthesized |

### Loop Notes

For sustain loops, set `loop_sus=0` (sustain on loop 1) and `loop_end=7` (hold indefinitely). Set `loopst[0]` and `looped[0]` to the loop start and end sample addresses. The remaining loop slots (`loopst[1..7]`, `looped[1..7]`) should be set to `gened`.

For one-shot samples, set `loop_sus=8` (no sustain loop). All `loopst`/`looped` entries should be set to `gened`.

`genst` and `gened` define the playback window within the wave. Set `genst=wavst` and `gened=waved` to play the full sample. Loop point addresses in unused slots are conventionally set to `gened` (the end of the sample).

Loop point addresses in the FZF are cumulative (relative to the combined audio area start). When reading loop points from a WAV SMPL chunk, scale them to the target sample rate: `loop_fz = round(loop_wav * target_rate / source_rate)`.

**Auxiliary loop fields per spec (section 2-1):**

- `loopxf` ("cross-fade time") takes a value in 0..1023. `0` means non-cross-fade looping.
- `looptm` ("loop time") takes a value in 1..1022, in 16 ms units (16 ms to ~16 s).
- The upper 8 bits of each `loopst` entry are "loop fine" (0..255).
- The MSB of each `looped` entry encodes the loop pattern: `1` = Skip, `0` = Trace.

### Envelope Notes

The DCA and DCF envelopes each have 8 stages (0-7). `dca_sus` sets which stage the envelope holds at during key-held; `dca_end` sets the final stage. On note-on, the envelope runs from stage 0 toward `stop[0]` at `rate[0]`, advancing through stages until it reaches `sus`. On note release (MIDI note-off), the envelope resumes from stage `sus+1` and runs through to `end`.

With `dca_sus=0` and `dca_end=7`, the envelope holds at stage 0 during sustain. On note release, it traverses stages 1-7. For one-shot samples, this means stages 1-7 control the release behaviour.

**DCA default envelope:** Stage 0 opens at max rate (`dca_rate[0]=127`) to full level (`dca_stop[0]=255`). Stages 1-7 decay to zero at rate 0xC0 (falling, magnitude 64) with `dca_stop[1..7]=0`. This produces instant attack, full level during sustain, and a moderate amplitude release on note-off. The hardware uses this pattern for voices with no envelope shaping.

**DCF default (no filtering):** Set `dcf=127` (max offset) with `dcf_rate[0]=127`, `dcf_rate[1..7]=0`, and all `dcf_stop` values at 255. The high offset ensures the filter is fully open regardless of envelope state. The inert release stages (`rate=0`) prevent any filter sweep on note release. The `dcf` field is an offset added to the DCF envelope output; `dcf=127` biases the filter above its audible range so the envelope has no perceptible effect.

**`vel_dca_kf` should be non-zero.** With `vel_dca_kf=0` all notes play at identical volume regardless of MIDI velocity, so velocity has no effect at all. A value of 80 gives normal velocity response matching real hardware samples.

### Voice Name

The name field is 14 bytes. The first 12 are the display name (ASCII, space-padded). Bytes 12 and 13 must be null. The name at 0xb2 is also used by the disk format to identify voice files: a printable 12-byte string here indicates a voice file; its absence indicates a full dump.

---

## Hardware Display Scale

The FZ-1/FZ-10M front panel displays envelope rates and stop levels using a 0-99 scale rather than the raw byte values stored in the voice header. The mapping between display values and byte values:

**Rates** (magnitude 0-127 to display 0-99):
- Forward: `display = (magnitude * 100) >> 7` (integer shift, equivalent to `floor(magnitude * 100 / 128)`)
- Reverse: `magnitude = ceil(display * 128 / 100)` (smallest magnitude that maps back to the given display value)
- The sign bit (bit 7) indicates direction: 0 = rising, 1 = falling.

**Stop levels** (byte 0-255 to display 0-99):
- Forward: `display = ceil(byte * 99 / 255)` (0 maps to 0; all other values round up)
- Reverse: `byte = floor(255 * (display - 1) / 99) + 1` (smallest byte that maps back to the given display value; 0 maps to 0)

Confirmed against BRASS1 D3 1 from hardware: DCA rate byte 127 displays as 99, DCA stop byte 218 displays as 85.

**Key follow / rate scaling** (signed byte -127..+127 to display -15..+15):
- Forward: `display = clamp(int8(byte) / 8, -15, +15)` (two's-complement reinterpretation, integer division by 8). The low end is clamped because `-128/8 = -16`, one step below the displayable range.
- Reverse: `byte = uint8(int8(display * 8))` (multiply by 8 and store as two's-complement).
- Applies to `dca_kf` (0xa6), `dca_rs` (0xa7), `dcf_kf` (0xa8), `dcf_rs` (0xa9).
- Validated against FZ-10M hardware using calibration disk images with byte values 0, 1, 4, 8, 15, 64, 127, 128.
- Implementation: `disk.KFByteToDisplay` / `disk.KFDisplayToByte` in `pkg/disk/voice.go`.

**Velocity sensitivity (`vel_dca_kf`, `vel_dca_rs`, `vel_dcf_kf`, `vel_dcf_rs`, `vel_dcq_kf`).** These signed-byte fields almost certainly have a narrower hardware display range too (the FZ-1 front panel does not show signed values in the -127..+127 range anywhere else), but the exact mapping has not been characterised against real hardware yet. fizzle currently exposes the raw signed-byte range in the CLI and TUI. Calibration is open; when it lands, it should be added here alongside the KF/RS mapping above.

---

## Real-World Findings

A few things that differ from the spec or are not documented:

**Multiple bank sectors.** Real hardware full dumps can contain up to 8 bank sectors (one per bank). The spec describes a single bank layout but the sampler saves the entire bank set into a single full dump. The voice area follows all bank sectors. Code reading FZFs must count consecutive valid bank sectors before locating the voice area.

**DIS sector included as ss0.** The sampler includes the DIS sector itself as the start of the first extent in the file's sector chain. File data starts at `ss0 + 1`. The tail counts (`bn vn wn`) in the DIS must correctly reflect the file structure; wrong counts cause the sampler to misparse the file.

**`bn vn wn` order.** The spec documents the tail count order as `vn bn wn` but the hardware writes and reads `bn vn wn`.

**Padding in the voice area.** Audio blocks are padded to 1024-byte sector boundaries in the FZF, but `waved` stores the unpadded sample count. The next voice's audio starts at the padded boundary. When unpacking, the audio start for voice `i` must be read from `wavst` in its header, not reconstructed from `waved` deltas, because the delta does not equal the padded block size.

**`midiChan` offset.** The spec's `struct bankdata` lists the arrays in order: `hwid`, `lwid`, `htch`, `ltch`, `cent`, `mchn`, `gchn`, `bvol`, `vp`. With 64 entries per array at 1 byte each (except `vp` at 2 bytes), `mchn` is at `0x142`, not `0x104`. Writing to `0x104` corrupts the `cent` array from voice 3 onwards, causing severe pitch errors on hardware.

**`vel_dca_kf` should be non-zero.** With `vel_dca_kf=0` velocity has no effect on amplitude and all notes play at identical volume regardless of how hard they are struck. A value of 80 gives normal velocity response matching real hardware samples.

**`gchn` (generator channel) controls polyphony.** `0xff` = all 8 generators active (polyphonic). A single-bit value like `0x01` assigns one generator (monophonic: new note cuts previous). Multiple voices with the same single-bit gchn value mute each other. This maps naturally to the SFZ `mutegroup=N` opcode (as exported by Renoise).

**`dcq` uses the full byte, not the upper 4 bits.** The spec describes the resonance offset as a 4-bit field in the upper nibble of byte 0x77 (section 2-1: *"takes a number among 0 - 127; however, notice that the effective bit number is upper 4 bits"*). In practice the FZ-10M reads the entire byte (0-127 range): writing the low nibble changes audible resonance, and the front panel display (0-127 scale) responds to single-byte increments. Confirmed by `pkg/voiceedit` round-trip tests where the byte written by `--resonance N` is read back unchanged via `fzv info` and matches the front-panel display.

<a id="bstep-key-splits-vs-voices"></a>
**`bstep` is per-bank and counts key splits, not file-level voices.** Spec section 2-2 gives `bstep` as "the current number of key splits or the number of voices which the bank uses". The two coincide only in the simplest case: a single-bank dump where every key split uses a distinct voice. Bank 0's `bstep` diverges from the file's voice count `vn` in two independent ways, and across the bundled corpus it equals `vn` for only 24 of the 235 full dumps:

1. **Multi-bank dumps** (210 of the 235 corpus files; up to 8 banks) carry per-bank `bstep` values. Bank 0's `bstep` counts only bank 0's key splits and ignores voices that live in later banks, so it undercounts. `Orchestra.fzf` (FL-2, 4 banks) carries `bstep = 2` in bank 0 but holds 8 voices.
2. **Shared-voice kits** point several key splits at one voice via `vp[]`, so the key-split count exceeds the distinct-voice count. `Drums.fzf` (FL-4) carries `bstep = 61` in bank 0 against 24 voices total; the FL-7 and FL-8 sound-effect disks show the same pattern (`Animals.fzf`, single bank, `bstep = 29`, 8 voices).

Parsers must size the voice area by `vn` (from the file head) when available, or by walking and validating slots when the file head is absent (the standalone-`.fzf` case after `disk get`). The walk uses the sum of every bank's `bstep` as a safe upper bound, then stops at the first slot whose bytes do not form a plausible voice header; the validation trims the overshoot that `vp[]` sharing introduces (the summed bound itself equals `vn` for only 80 of the 235 corpus files, so the trim is essential). Sizing by bank 0's `bstep` alone reads garbage on the multi-bank and shared-voice cases. fizzle implements this in `fzutil.CountAllVoices` and `fzutil.InferVoiceCount`.

---

## Multi-Disk Full Dumps

The spec (section 1-3) states: "The FZ-1 allows you to save/load waveform data uninterruptedly on 2 pieces of floppy disk. The higher byte for ext takes 0 as value for the first floppy disk and does 1 for the second disk."

2-disk splits have been confirmed by testing on real FZ-10M hardware. 2 disks is the hardware maximum: the FZ series has 2 MB of sample RAM, and the spec only describes 2-disk saves for exactly this reason. Loading a third disk would overflow RAM.

**Disk 1** (`disknum=0`):
- Bank sector with `bstep` = **total voice count across the full instrument** (e.g. 25 voices, not just the voices whose audio fits on disk 1). This is the critical field: the sampler compares it against the audio it receives and detects the shortfall.
- Voice parameter area (`struct voicedata`) for **all voices** including those on later disks. The sampler reads envelopes, loop points, and tuning from disk 1 for the entire instrument.
- Audio for as many voices as fit within the 1.25 MB capacity.
- DIS tail `wn` = **total** wave sectors across all disks, not just disk 1. This is stored in the bank sector at offset `0x290` so `diskadd` can write the correct value. The sampler uses this to know more audio is coming.

**Disk 2** (`disknum=1`):
- **Pure audio continuation.** No bank sector, no voice headers. The sampler appends disk 2's data directly into sample RAM after disk 1's audio.
- DIS tail `bn`, `vn`, `wn` are **identical to disk 1's values** (both reflect the total instrument size).

This was confirmed by saving a 2-disk instrument from real FZ-10M hardware and inspecting the binary output. The hardware writes disk 2 as raw audio overflow; the sampler reads all instrument metadata (bank sector, voice headers) from disk 1.

Key points:
- **Disk 1 must contain the full instrument voice count in its bank sector.** If it only contains the voices whose audio fits on disk 1, the sampler considers loading complete and never asks for the next disk.
- **Disk 2 is not a self-contained FZF.** It contains only audio data that continues from where disk 1's audio was truncated at the disk boundary.
- Voice header pointers on disk 1 use **absolute word addresses** (as described in the spec) that span both disks. A voice whose audio starts on disk 2 will have `wavst` pointing past disk 1's local audio area into the RAM region that disk 2's audio will occupy.
- Both disks use the filename `FULL-DATA-FZ`. The `disknum` field in the directory entry (`0` for disk 1, `1` for disk 2) distinguishes them.
- `fizzle sfz convert --split-disks` produces disk images (`.img`) directly, handling the split and DIS sector creation internally.
