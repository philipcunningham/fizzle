# llm-wiki log

Append only, newest last. Entry format: [AGENTS.md](AGENTS.md).

## [2026-07-11] scaffold | llm-wiki bootstrapped

Added the schema, `index.md`, `map.md`, `project.md`, 8 findings pages,
5 topic pages, and 6 source pages, distilled from
`docs/casio-fz1-format.md` (deleted, citations repointed). Moved the
Casio spec (md and pdf) verbatim into `llm-wiki/sources/`.

## [2026-08-18] ingest | studio TUI removal

fizzle removed the studio TUI; `pkg/studio/container` and
`pkg/studio/model` moved to `pkg/container` and `pkg/model`. Repointed
code anchors in map.md, project.md, firmware.md, corpus.md,
display-scales.md, and envelope-timing.md. The verified rate-table Go
copy now lives only in git history; the ROM at F000:0490 is the source.

## [2026-08-20] ingest | Sample memory per machine

An FZ-1 owner reported `NO MEMORY SPACE` on a 1,188,000 byte
instrument, which the wiki's 2 MB figure couldn't explain. Added
[sample-memory](topics/sample-memory.md). The FZ-1 shipped with 1 MB
and the rack units with 2 MB, and the probe at `F000:07D4` discovers
which at power on. Corrected multi-disk-dumps.md, which derived the two
disk cap from a series wide 2 MB; the cap comes from the `disknum` bit.
One open question logged: what a load does to resident memory.

## [2026-08-20] ingest | Envelope timing, corrected and extended

The page's tick figure was wrong by a factor of three. It derived 25 ms
from a stale 6.4 ms IRQ estimate. The chain is a 4 kHz IRQ, a 500 Hz
voice service, and 125 Hz per DCA stage, so a stage steps every 8 ms.
Added the slew limit, the instant rate at panel 99, and the level to
code mapping through the table at F000:0590. Added the note on scaling
and the release rules. The code to loudness law stays open.

## [2026-08-20] ingest | Envelope timing, the stage walk

Note on caps the run at the lower of the sustain and end stages. A
sustain at or past the end never holds, and the voice terminates with
the key still down. Note off skips a mid stage release to the end
stage, where a parked note runs the stages between.

## [2026-08-20] ingest | Envelope timing, the note on scaling

Each scaling term now carries its arithmetic, its ROM address, and its
shift kind, plus the `bvol` subtraction on stops. Corrected the slew
citation to F000:0A49. Split the level to code map at F000:214C from
the composition at F000:20BD, and recorded that velocity never reaches
that routine.

## [2026-08-20] ingest | The DCA is an analog amplifier, and its law is dB

Service manuals settled the open question the page carried. The DCA
sits inside the filter chip, downstream of two converters, and covers
0 to -87.75 dB. Its gain word and amplitude word are driven as one
monotone number, which the slew and the fade prove. The per step
figure and the select pin tie stay open.

## [2026-08-20] ingest | Service manuals, and what they don't settle

New source page for the manuals and books. The manuals establish the
DCA part and its control words. The books corroborate the one shot
envelope, the forced end stage, and the instant rate. Three open
questions added to envelope timing: the competing law, the wide
velocity span, and the stored stage direction.

## [2026-08-21] ingest | Multi-loop playback

The eight loops play as a chain in numerical order, with sustain and
end as roles inside it, per the spec and the FZ book.
Added [multi-loops](topics/multi-loops.md) and the looptm finding.
The spec says 16 ms steps where the book's figure shows repeat
counts. Map row for loops updated.

## [2026-08-21] lint | After the multi-loop ingest

Frontmatter complete on all pages; no orphans or duplicates. Map
drift fixed: the source list gains casio-service-manuals. Staleness
flagged on nine pages dated 2026-07-11 whose cited packages moved on
2026-08-18, all refactors; re-verify on next ingest. Twelve open
questions collected across six pages.

## [2026-08-21] ingest | Multi-loop pages hardened

An adversarial pass over the manuals found the book's Experiment 12
contradicting the looptm reconciliation, so the finding now leans
repeat count. The end loop credit widens to both envelopes, and the
spec's four bit loop fine comment joins casio-spec known errors.

## [2026-08-21] ingest | The panel and the file part ways on loop roles

A second review caught the roles section speaking panel where the
file stores loop_sus and loop_end; both pages now draw the line. The
manual's screens show END at step 11 and SUS on the next loop
screens, so the designations are panel positions, not looptm values.

## [2026-08-21] ingest | The chain runs backward when the loops do

A plan review surfaced Experiment 12's observations. The book chains
a phrase's words in reverse, so a next loop can sit behind the one
before it and the jump runs backward. Added to multi-loops, read
from the book directly.

## [2026-08-21] ingest | The loop chain runs on one cap byte

Note on caps the chain at `min(loop_sus, loop_end)` and note off raises
the cap to `loop_end`. The advance runs only below the cap, so the
sustain hold and the repeating end loop are one rule. multi-loops
moves to confirmed-firmware.

## [2026-08-21] ingest | looptm is a duration, not a count

The ROM's counter ticks at 62.5 Hz, so a unit is 16 ms, which is the
spec's own figure. The finding had leaned repeat count on the FZ
book's caption. It moves from suspect to confirmed-firmware.

## [2026-08-21] ingest | The 1024 on end loops is an authoring marker

The advance halts at the cap, so an end loop's timer never runs and its
`looptm` is never read. The corpus writes 1024 on all 2,554 of them,
and playback consults none of it.

## [2026-08-21] ingest | The loop timer runs only while its loop sounds

Note on parks the timer, expiry parks it, and F000:251F parks it again;
a negative timer never expires. The running value comes from F000:2033.
What those two run under stays untraced, and both pages say so.

## [2026-08-21] schema | Pages state the present, the log carries history

Added the rule to the page section: a page never narrates its own
history. Three ingests in one day had pages arguing with their past
readings, which costs every later reader a detour.

## [2026-08-21] lint | Log entry size, third recurrence

Two entries ran past the two to five line body again. Split per
operation rather than amending the rule: an ingest touching several
pages is several operations, and the size limit is what forces that.

## [2026-08-21] ingest | Preview follows the loop cap at key up

The implementation note in multi-loops.md fell behind this branch.
The browser preview now moves the Web Audio loop window to the end
loop at key up, matching the cap raise at F000:1515. Corrected the
sentence claiming the preview only repeats the sustain loop.

## [2026-08-22] ingest | Panel display scales, read off an emulator

Recorded four mappings in display-scales.md. Tune in cents, the delay
and attack pair the DELAY row writes together, the unsigned velocity
resonance row, and the inverted area level. Each came from driving the
FZ-1 firmware under an emulator and reading the bytes back.

## [2026-08-22] ingest | Two static bounds readings reversed

The velocity RESONANCE and velocity DCF RATE rows were both recorded
wrong, from misjudging the bounds table's record phase by one. A
misaligned record still decodes into plausible bounds. The open
question calling the velocity fields uncalibrated is gone.

## [2026-08-23] ingest | Casio's manuals confirm the panel ranges

The owner's manual and the FZ-1 and FZ-10M book confirm six emulator
readings independently, the unsigned velocity resonance row included.
Neither manual discusses stored bytes, so they settle what a row
displays and leave every byte mapping where it was.

## [2026-08-23] ingest | MAX TOUCH and MIN TOUCH floor at 001

The owner's manual prints both velocity sliders as 127 over 001, where
AREA LEVEL beside them floors at 000. A velocity of zero silences the
voice. fizzle's area setter allowed it and now clamps to the panel's
floor.

## [2026-08-23] ingest | Deleted directory entries leave blank slots

New finding from a user disk a real FZ-1 saved, now the PREY.img fixture under testdata/synthetic. Deletion zeroes the first name byte and leaves the gap, so live entries follow blank slots. Mapped the directory row to the new page.

## [2026-08-23] ingest | DIS vn is the authority for voice-area sizing

voice-area-sizing.md now carries the PREY evidence. Summed bstep can undercount vn (a voice in no bank), and stale slots past vn defeat the walk's other bound. Recorded the new fizzle plumbing and an open question on wavst monotonicity.

## [2026-08-23] lint | Post-ingest pass

Frontmatter complete on both touched pages. No stale citations: the pages touched today carry today's date, and no older page cites the code paths this branch changed. directory-blank-slots is linked from the map, so no orphan. No duplicate scope.

## [2026-08-23] ingest | The DIS vn is trusted upward only

Adversarial review over the branch: TECHNO.img carries vn 30 with 32 live voices. A vn at or below the walk is an undercount, and the walk wins. voice-area-sizing.md records the direction rule and the new 0x294 voice-count marker for standalone exports.

## [2026-08-23] ingest | The gap read tolerance is inference

directory-blank-slots.md now scopes its claim: the bytes prove the firmware writes gaps, not that it reads past one. An open question asks for the firmware trace. Recorded the printable-name gate and the fixture's unconfirmed audio redistribution terms.

## [2026-08-23] lint | Correction: multi-disk-dumps was stale

The 2026-08-23 post-ingest pass claimed no older page cited the changed code paths. multi-disk-dumps.md cites pkg/disk and pkg/diskadd and was dated 2026-08-20. Its citations re-verified today and the page bumped, with the 0x294 marker cross-reference added.

## [2026-08-23] ingest | The trusted-upward rule's blind spot is named

voice-area-sizing.md records the accepted trade. Stale slots that walk as plausible past a low vn list as voices, and an edit restamps the tail. A test pins the TECHNO restamp. The CLI readers now honour the 0x294 marker through ReadFZF.
