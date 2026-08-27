---
type: finding
title: Audio blocks are sector-padded but waved stores the unpadded count
description: Read each voice's audio start from its own wavst; reconstructing from waved deltas misses the padding.
tags: [fzf, audio, parsing]
updated: 2026-08-27
sources:
  - llm-wiki/sources/casio-fz1-data-structures.md sections 1-5, 2-1
status: confirmed-hardware
---

# Audio blocks are sector-padded but waved stores the unpadded count

Each voice's PCM block in an FZF is padded to a 1024-byte sector boundary, while `waved` stores the unpadded sample count. The next voice's audio starts at the padded boundary, so the `waved` delta doesn't equal the padded block size.

Wave-address fields (`wavst`, `waved`, `genst`, `gened`, loop addresses) are cumulative 16-bit-word indices into the combined audio area. Voice 0 has `wavst = 0`; voice 1's `wavst` is voice 0's sector-aligned byte count divided by 2. Multiply by 2 for a byte offset.

When unpacking, read voice `i`'s audio start from its own `wavst` (`pkg/voiceunpack`, `pkg/voiceextract`); never reconstruct it from `waved` deltas. On 2-disk dumps the same cumulative indices on disk 1 point past its local audio into RAM that disk 2 fills. See [multi-disk-dumps](../topics/multi-disk-dumps.md).
