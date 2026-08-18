// Contract tests for the core boundary, run against the fake core.
// Slice 1 swaps in the real WASM module behind the same contract, and
// these tests keep passing unchanged against it.
import { describe, expect, it } from "vitest";
import { IMAGE_SIZE } from "../src/boundary/contract";
import { createFakeCore } from "../src/core/fake";
import { wavFixture } from "./helpers";

function image(fill: number): Uint8Array {
  return new Uint8Array(IMAGE_SIZE).fill(fill);
}

// A deep equal over a 1.4 MB image compares element by element and takes
// seconds. Report the first byte that differs instead.
function firstDifference(a: Uint8Array, b: Uint8Array): number {
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return i;
  }
  return -1;
}

describe("fake core contract", () => {
  it("starts with an empty snapshot at revision 0", async () => {
    const core = createFakeCore();
    const r = await core.snapshot();
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.value.revision).toBe(0);
    expect(r.value.disk).toBeNull();
  });

  it("creates a new disk and bumps the revision", async () => {
    const core = createFakeCore();
    const r = await core.newDisk("MY DISK");
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.value.disk?.label).toBe("MY DISK");
    expect(r.value.revision).toBe(1);
  });

  it("rejects an over-long label with an error envelope, state untouched", async () => {
    const core = createFakeCore();
    const bad = await core.newDisk("THIS LABEL IS FAR TOO LONG");
    expect(bad.ok).toBe(false);
    if (bad.ok) return;
    expect(bad.error.code).toBe("invalid-label");
    expect(bad.error.message).not.toBe("");
    const snap = await core.snapshot();
    if (!snap.ok) throw new Error("snapshot failed");
    expect(snap.value.revision).toBe(0);
    expect(snap.value.disk).toBeNull();
  });

  it("round trips an opened image byte identical", async () => {
    const core = createFakeCore();
    const bytes = image(0xa5);
    const opened = await core.openImage(bytes);
    expect(opened.ok).toBe(true);
    const out = await core.exportImage();
    expect(out.ok).toBe(true);
    if (!out.ok) return;
    expect(out.value.length).toBe(bytes.length);
    expect(firstDifference(out.value, bytes)).toBe(-1);
  });

  it("export does not alias the caller's buffer", async () => {
    const core = createFakeCore();
    const bytes = image(1);
    await core.openImage(bytes);
    bytes.fill(9);
    const out = await core.exportImage();
    if (!out.ok) throw new Error("export failed");
    expect(out.value[0]).toBe(1);
  });

  it("rejects a wrong-size image with an error envelope, never a throw", async () => {
    const core = createFakeCore();
    const r = await core.openImage(new Uint8Array(16));
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.error.code).toBe("invalid-image");
  });

  it("rejects export with no disk open", async () => {
    const core = createFakeCore();
    const r = await core.exportImage();
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.error.code).toBe("no-disk");
  });

  it("every accepted mutation carries a fresh revision", async () => {
    const core = createFakeCore();
    const a = await core.newDisk("ONE");
    const b = await core.openImage(image(2));
    if (!a.ok || !b.ok) throw new Error("setup failed");
    expect(b.value.revision).toBeGreaterThan(a.value.revision);
  });

  it("a rejected operation does not advance the revision", async () => {
    const core = createFakeCore();
    await core.newDisk("ONE");
    const before = await core.snapshot();
    await core.openImage(new Uint8Array(3));
    const after = await core.snapshot();
    if (!before.ok || !after.ok) throw new Error("snapshot failed");
    expect(after.value.revision).toBe(before.value.revision);
  });
});

describe("fake core WAV import", () => {
  async function withDisk() {
    const core = createFakeCore();
    await core.newDisk("KIT");
    return core;
  }

  function wavFile(bytes = 4000): Uint8Array {
    return new Uint8Array(bytes).fill(7);
  }

  it("adds one voice, grows capacity, advances the revision", async () => {
    const core = await withDisk();
    const before = await core.snapshot();
    const r = await core.importWav("Kick 1.wav", wavFile(), 18000, "mix");
    expect(r.ok).toBe(true);
    if (!r.ok || !before.ok) return;
    const disk = r.value.disk;
    expect(disk?.files).toHaveLength(1);
    expect(disk?.files[0]?.name).toBe("KICK 1");
    expect(disk?.files[0]?.type).toBe("voice");
    expect(disk?.usedBytes ?? 0).toBeGreaterThan(before.value.disk?.usedBytes ?? 0);
    expect(r.value.revision).toBeGreaterThan(before.value.revision);
  });

  it("rejects an import with no disk open", async () => {
    const core = createFakeCore();
    const r = await core.importWav("x.wav", wavFile(), 18000, "mix");
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.error.code).toBe("no-disk");
  });

  it("rejects an import the disk cannot hold, state untouched", async () => {
    const core = await withDisk();
    const before = await core.snapshot();
    const r = await core.importWav("HUGE.wav", wavFile(IMAGE_SIZE * 2), 18000, "mix");
    expect(r.ok).toBe(false);
    if (r.ok || !before.ok) return;
    expect(r.error.code).toBe("no-space");
    const after = await core.snapshot();
    if (!after.ok) return;
    expect(after.value.revision).toBe(before.value.revision);
  });
});

describe("fake core schema editing", () => {
  async function voiceCore() {
    const core = createFakeCore();
    await core.newDisk("KIT");
    await core.importWav("Kick.wav", new Uint8Array(2048), 18000, "mix");
    return core;
  }

  it("emits a schema with every control kind", async () => {
    const core = createFakeCore();
    const r = await core.schema();
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    const kinds = new Set(r.value.map((f) => f.kind));
    for (const kind of ["knob", "stepper", "note", "select"]) {
      expect(kinds.has(kind)).toBe(true);
    }
  });

  it("clamps numeric params to the schema range", async () => {
    const core = await voiceCore();
    const r = await core.setParamNumber("KICK", "cutoff", 900);
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.value.disk?.files[0]?.params?.["cutoff"]).toBe(127);
  });

  it("rejects an unknown field with an envelope", async () => {
    const core = await voiceCore();
    const r = await core.setParamNumber("KICK", "warp", 1);
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.error.code).toBe("invalid-field");
  });

  it("undoes and redoes edits by revision", async () => {
    const core = await voiceCore();
    await core.setParamNumber("KICK", "cutoff", 90);
    const undone = await core.undo();
    expect(undone.ok).toBe(true);
    if (!undone.ok) return;
    expect(undone.value.disk?.files[0]?.params?.["cutoff"]).not.toBe(90);
    expect(undone.value.canRedo).toBe(true);
    const redone = await core.redo();
    if (!redone.ok) return;
    expect(redone.value.disk?.files[0]?.params?.["cutoff"]).toBe(90);
  });

  it("coalesces a gesture into one undo entry", async () => {
    const core = await voiceCore();
    const start = await core.snapshot();
    if (!start.ok) return;
    const before = start.value.disk?.files[0]?.params?.["cutoff"];
    await core.beginGesture();
    await core.setParamNumber("KICK", "cutoff", 30);
    await core.setParamNumber("KICK", "cutoff", 60);
    await core.setParamNumber("KICK", "cutoff", 90);
    await core.commitGesture();
    const undone = await core.undo();
    if (!undone.ok) return;
    expect(undone.value.disk?.files[0]?.params?.["cutoff"]).toBe(before);
  });
});

describe("fake core placement matrix", () => {
  it("addVoice with no disk creates disk and mapped instrument voice", async () => {
    const core = createFakeCore();
    const r = await core.addVoice(new Uint8Array([1, 2, 3]));
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.value.disk?.label).toBe("FIZZLE");
    const inst = r.value.disk?.instrument;
    expect(inst?.voices).toHaveLength(1);
    expect(inst?.voices[0]?.referenced).toBe(true);
    expect(inst?.banks[0]?.areas).toHaveLength(1);
  });

  it("addVoice joins an open instrument's voice list mapped", async () => {
    const core = createFakeCore();
    await core.openImage(new Uint8Array(IMAGE_SIZE));
    const r = await core.addVoice(new Uint8Array([9]));
    if (!r.ok) return;
    const inst = r.value.disk?.instrument;
    expect(inst?.voices).toHaveLength(4);
    expect(inst?.voices[3]?.referenced).toBe(true);
  });

  it("loadFzf replaces the instrument, splitting when oversized", async () => {
    const core = createFakeCore();
    await core.openImage(new Uint8Array(IMAGE_SIZE));
    const small = await core.loadFzf(new Uint8Array(1024));
    if (!small.ok) return;
    expect(small.value.disk?.disks).toBe(1);
    expect(small.value.disk?.instrument?.voices[0]?.name).toBe("LOADED");
    const big = await core.loadFzf(new Uint8Array(1_400_000));
    if (!big.ok) return;
    expect(big.value.disk?.disks).toBe(2);
    const disk2 = await core.exportImageAt(1);
    expect(disk2.ok).toBe(true);
  });

  it("addBank joins at a slot or becomes the instrument", async () => {
    const core = createFakeCore();
    const created = await core.addBank(new Uint8Array([1]), 0);
    if (!created.ok) return;
    expect(created.value.disk?.instrument?.banks[0]?.name).toBe("FZB BANK");
    await core.openImage(new Uint8Array(IMAGE_SIZE));
    const appended = await core.addBank(new Uint8Array([1]), 1);
    if (!appended.ok) return;
    expect(appended.value.disk?.instrument?.banks).toHaveLength(2);
    const skipped = await core.addBank(new Uint8Array([1]), 5);
    expect(skipped.ok).toBe(false);
  });

  it("importWavToInstrument names the voice from the file", async () => {
    const core = createFakeCore();
    await core.openImage(new Uint8Array(IMAGE_SIZE));
    const r = await core.importWavToInstrument("Sub Bass.wav", new Uint8Array([1]), 18000, "mix");
    if (!r.ok) return;
    expect(r.value.disk?.instrument?.voices[3]?.name).toBe("SUB BASS");
  });
});

describe("fake core split pairs", () => {
  const disk1 = () => {
    const image = new Uint8Array(IMAGE_SIZE);
    image[0] = 1;
    return image;
  };
  const disk2 = () => {
    const image = new Uint8Array(IMAGE_SIZE);
    image[0] = 2;
    return image;
  };

  it("a lone half names its missing twin", async () => {
    const core = createFakeCore();
    const first = await core.openImage(disk1());
    if (!first.ok) return;
    expect(first.value.disk?.missingDisk).toBe(2);
    const second = await core.openImage(disk2());
    if (!second.ok) return;
    expect(second.value.disk?.missingDisk).toBe(1);
  });

  it("openImagePair accepts either order and rejects mismatches", async () => {
    for (const [a, b] of [
      [disk1(), disk2()],
      [disk2(), disk1()],
    ] as const) {
      const core = createFakeCore();
      const r = await core.openImagePair(a, b);
      expect(r.ok).toBe(true);
      if (!r.ok) continue;
      expect(r.value.disk?.disks).toBe(2);
      expect(r.value.disk?.missingDisk).toBeUndefined();
      expect(r.value.disk?.capacityBytes).toBe(2 * IMAGE_SIZE);
    }
    const core = createFakeCore();
    const bad = await core.openImagePair(disk1(), disk1());
    expect(bad.ok).toBe(false);
    if (!bad.ok) expect(bad.error.code).toBe("pair-mismatch");
  });
});

describe("fake core folder imports", () => {
  const sfzFiles = () => ({
    "kit.sfz": new TextEncoder().encode(
      "<region> sample=wavs/kick.wav\n<region> sample=wavs/snare.wav\n",
    ),
    "wavs/kick.wav": new Uint8Array([1]),
    "wavs/snare.wav": new Uint8Array([2]),
  });

  it("importSfz converts to the instrument and reports the rate", async () => {
    const core = createFakeCore();
    const r = await core.importSfz(sfzFiles(), "", 18000, false, false, "mix");
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.value.rate).toBe(18000);
    const inst = r.value.snapshot.disk?.instrument;
    expect(inst?.voices.map((v) => v.name)).toEqual(["KICK", "SNARE"]);
    expect(r.value.snapshot.disk?.disks).toBe(1);
  });

  it("importSfz split yields a pair; fit steps the rate down", async () => {
    const core = createFakeCore();
    const split = await core.importSfz(sfzFiles(), "", 18000, false, true, "mix");
    if (!split.ok) return;
    expect(split.value.snapshot.disk?.disks).toBe(2);
    const fit = await core.importSfz(sfzFiles(), "", 18000, true, false, "mix");
    if (!fit.ok) return;
    expect(fit.value.rate).toBe(9000);
  });

  it("importSfz names a missing sample (R9)", async () => {
    const core = createFakeCore();
    const files = sfzFiles();
    // @ts-expect-error deleting to simulate an incomplete pack
    delete files["wavs/snare.wav"];
    const r = await core.importSfz(files, "", 18000, false, false, "mix");
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.error.code).toBe("missing-samples");
    expect(r.error.message).toContain("snare.wav");
  });

  it("importSfz demands exactly one .sfz when unnamed", async () => {
    const core = createFakeCore();
    const none = await core.importSfz(
      { "a.wav": new Uint8Array([1]) },
      "",
      18000,
      false,
      false,
      "mix",
    );
    expect(none.ok).toBe(false);
    if (!none.ok) expect(none.error.code).toBe("no-sfz");
  });

  it("importWavFolder maps sorted WAVs up the keyboard", async () => {
    const core = createFakeCore();
    const r = await core.importWavFolder(
      {
        "02 snare.wav": new Uint8Array([2]),
        "01 kick.wav": new Uint8Array([1]),
      },
      18000,
      false,
      "mix",
    );
    if (!r.ok) return;
    const areas = r.value.snapshot.disk?.instrument?.banks[0]?.areas;
    expect(areas?.map((a) => a.keyLow)).toEqual([36, 37]);
    expect(r.value.snapshot.disk?.instrument?.voices[0]?.name).toBe("01 KICK");
  });
});

describe("fake core slot editing", () => {
  it("edits slot params, loops, envelopes, and names", async () => {
    const core = createFakeCore();
    await core.openImage(new Uint8Array(IMAGE_SIZE));

    const num = await core.setSlotParamNumber(0, "cutoff", 90);
    if (!num.ok) throw new Error(num.error.message);
    expect(num.value.disk?.instrument?.voices[0]?.params?.["cutoff"]).toBe(90);

    const opt = await core.setSlotParamOption(0, "playbackMode", "reverse");
    if (!opt.ok) throw new Error(opt.error.message);
    expect(opt.value.disk?.instrument?.voices[0]?.params?.["playbackMode"]).toBe("reverse");

    const loop = await core.setSlotLoop(1, 0, 100, 900);
    if (!loop.ok) throw new Error(loop.error.message);
    expect(loop.value.disk?.instrument?.voices[1]?.voice?.loops[0]).toMatchObject({
      start: 100,
      end: 900,
    });

    const attr = await core.setSlotLoopAttr(1, 0, 512, 700);
    if (!attr.ok) throw new Error(attr.error.message);
    expect(attr.value.disk?.instrument?.voices[1]?.voice?.loops[0]).toMatchObject({
      xf: 512,
      tm: 700,
    });

    const env = await core.setSlotEnvelope(
      0,
      "dca",
      3,
      6,
      new Array(8).fill(40) as number[],
      [99, 90, 80, 70, 60, 50, 40, 30],
    );
    if (!env.ok) throw new Error(env.error.message);
    expect(env.value.disk?.instrument?.voices[0]?.voice?.dca.sustain).toBe(3);

    const renamed = await core.renameVoiceSlot(0, "THUMP");
    if (!renamed.ok) throw new Error(renamed.error.message);
    const inst = renamed.value.disk?.instrument;
    expect(inst?.voices[0]?.name).toBe("THUMP");
    expect(inst?.banks[0]?.areas[0]?.voiceName).toBe("THUMP");

    const badSlot = await core.setSlotParamNumber(9, "cutoff", 1);
    expect(badSlot.ok).toBe(false);
  });

  it("serves slot peaks and extracts", async () => {
    const core = createFakeCore();
    await core.openImage(new Uint8Array(IMAGE_SIZE));

    const peaks = await core.slotPeaks(0, 0, 4096, 32);
    if (!peaks.ok) throw new Error(peaks.error.message);
    expect(peaks.value.length).toBe(64);

    const fzv = await core.extractVoiceSlot(1, "fzv");
    if (!fzv.ok) throw new Error(fzv.error.message);
    expect(fzv.value.name).toBe("SNARE");
    expect(fzv.value.bytes.length).toBeGreaterThan(0);

    const file = await core.extractFile("FULL-DATA-FZ");
    expect(file.ok).toBe(true);
  });
});

// R14's generation window. It stays off the schema because its bounds
// are the voice's own frame count, so the boundary carries it as a
// bespoke call beside the loops.
describe("fake core generation window", () => {
  it("takes the frames a slot call was given and reports them back", async () => {
    const core = createFakeCore();
    await core.openImage(new Uint8Array(IMAGE_SIZE));

    const r = await core.setSlotGeneration(1, 100, 900);
    if (!r.ok) throw new Error(r.error.message);
    expect(r.value.disk?.instrument?.voices[1]?.voice).toMatchObject({
      genStart: 100,
      genEnd: 900,
    });
  });

  it("clamps a slot's generation end to the voice's frame count", async () => {
    const core = createFakeCore();
    await core.openImage(new Uint8Array(IMAGE_SIZE));

    const r = await core.setSlotGeneration(1, -10, 9_999_999);
    if (!r.ok) throw new Error(r.error.message);
    const detail = r.value.disk?.instrument?.voices[1]?.voice;
    // Slot 1 holds 4352 frames in the fake.
    expect(detail).toMatchObject({ genStart: 0, genEnd: detail?.frames });
  });

  it("takes the frames a file call was given and clamps them the same way", async () => {
    const core = createFakeCore();
    await core.newDisk("KIT");
    await core.importWav("Kick.wav", new Uint8Array(2048), 18000, "mix");

    const set = await core.setGeneration("KICK", 40, 200);
    if (!set.ok) throw new Error(set.error.message);
    expect(set.value.disk?.files[0]?.voice).toMatchObject({ genStart: 40, genEnd: 200 });

    const clamped = await core.setGeneration("KICK", 40, 9_999_999);
    if (!clamped.ok) throw new Error(clamped.error.message);
    const detail = clamped.value.disk?.files[0]?.voice;
    expect(detail).toMatchObject({ genStart: 40, genEnd: detail?.frames });
  });

  it("refuses a voice it cannot find, leaving the document alone", async () => {
    const core = createFakeCore();
    await core.newDisk("KIT");
    const before = await core.snapshot();
    if (!before.ok) throw new Error(before.error.message);

    const r = await core.setGeneration("NOWHERE", 0, 10);
    expect(r.ok).toBe(false);
    const after = await core.snapshot();
    if (!after.ok) throw new Error(after.error.message);
    expect(after.value.revision).toBe(before.value.revision);
  });
});

describe("fake core document ops", () => {
  it("renames the disk and deletes files", async () => {
    const core = createFakeCore();
    await core.openImage(new Uint8Array(IMAGE_SIZE));

    const renamed = await core.renameDisk("NEW LABEL");
    if (!renamed.ok) throw new Error(renamed.error.message);
    expect(renamed.value.disk?.label).toBe("NEW LABEL");

    const deleted = await core.deleteFile("FULL-DATA-FZ");
    if (!deleted.ok) throw new Error(deleted.error.message);
    expect(deleted.value.disk?.instrument).toBeUndefined();
    expect(deleted.value.disk?.files).toHaveLength(0);

    const missing = await core.deleteFile("ABSENT");
    expect(missing.ok).toBe(false);
  });

  it("creates a new empty instrument once", async () => {
    const core = createFakeCore();
    const created = await core.newInstrument("MY SET");
    if (!created.ok) throw new Error(created.error.message);
    expect(created.value.disk?.instrument?.banks[0]?.name).toBe("MY SET");
    expect(created.value.disk?.instrument?.voices).toHaveLength(0);

    const again = await core.newInstrument("TWICE");
    expect(again.ok).toBe(false);
    if (!again.ok) expect(again.error.code).toBe("instrument-exists");
  });
});

// The estimate drives the import dialog's reactive line, so the fake
// mirrors the core's semantics: rate-scaled sizes, the sampler memory
// cap, the split verdict, and read-only behaviour.
describe("fake core import estimate", () => {
  async function withDisk() {
    const core = createFakeCore();
    await core.newDisk("KIT");
    return core;
  }

  it("scales the estimate with the chosen rate", async () => {
    const core = await withDisk();
    const files = { "pad.wav": wavFixture(1, 36000, 36000) };
    const at36 = await core.estimateImport(files, 36000, "mix");
    const at9 = await core.estimateImport(files, 9000, "mix");
    if (!at36.ok || !at9.ok) throw new Error("estimate failed");
    expect(at36.value.verdict).toBe("fits");
    expect(at36.value.seconds).toBeCloseTo(1, 2);
    expect(at9.value.seconds).toBeCloseTo(1, 2);
    expect(at9.value.bytes).toBeLessThan(at36.value.bytes);
    expect(at36.value.anyStereo).toBe(false);
    expect(at36.value.roomSeconds).toBeGreaterThan(10);
  });

  it("flags a stereo file so the dialog asks the stereo question", async () => {
    const core = await withDisk();
    const r = await core.estimateImport({ "st.wav": wavFixture(2, 44100, 1000) }, 18000, "mix");
    if (!r.ok) throw new Error(r.error.message);
    expect(r.value.anyStereo).toBe(true);
  });

  it("refuses a file over the sampler's memory and names the way out", async () => {
    const core = await withDisk();
    // 59.4 s of stereo 44.1 kHz: over the cap at 36 and 18, fits at 9.
    const files = { "long.wav": wavFixture(2, 44100, 2619540) };
    const r = await core.estimateImport(files, 36000, "mix");
    if (!r.ok) throw new Error(r.error.message);
    expect(r.value.verdict).toBe("wont-fit");
    expect(r.value.reason).toBe("sample-memory");
    expect(r.value.overCapFile).toBe("long.wav");
    expect(r.value.fileSeconds).toBeCloseTo(59.4, 1);
    expect(r.value.capSeconds).toBeCloseTo(29.1, 1);
    expect(r.value.fitsAtRates).toEqual([9000]);
  });

  it("refuses a source rate below the core's minimum", async () => {
    const core = await withDisk();
    const r = await core.estimateImport({ "slow.wav": wavFixture(1, 500, 100) }, 18000, "mix");
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.error.code).toBe("invalid-wav");
    expect(r.error.message).toContain("slow.wav");
  });

  it("splits a lone first voice too big for one disk, as the core does", async () => {
    const core = await withDisk();
    const r = await core.estimateImport({ "big.wav": wavFixture(1, 18000, 720000) }, 18000, "mix");
    if (!r.ok) throw new Error(r.error.message);
    expect(r.value.verdict).toBe("splits");
  });

  it("reports the two disk split for a join past one disk", async () => {
    const core = await withDisk();
    await core.importWavToInstrument("first.wav", wavFixture(1, 18000, 450000), 18000, "mix");
    const r = await core.estimateImport(
      { "second.wav": wavFixture(1, 18000, 450000) },
      18000,
      "mix",
    );
    if (!r.ok) throw new Error(r.error.message);
    expect(r.value.verdict).toBe("splits");
  });

  it("refuses a 65th voice with the voice-limit reason", async () => {
    const core = await withDisk();
    const files: Record<string, Uint8Array> = {};
    for (let i = 0; i <= 64; i++)
      files[`v${String(i).padStart(2, "0")}.wav`] = wavFixture(1, 18000, 100);
    const r = await core.estimateImport(files, 18000, "mix");
    if (!r.ok) throw new Error(r.error.message);
    expect(r.value.verdict).toBe("wont-fit");
    expect(r.value.reason).toBe("voice-limit");
    expect(r.value.fitsAtRates).toEqual([]);
  });

  it("refuses an unreadable file by name", async () => {
    const core = await withDisk();
    const r = await core.estimateImport(
      { "ok.wav": wavFixture(1, 18000, 100), "bad.wav": new Uint8Array([1, 2, 3]) },
      18000,
      "mix",
    );
    expect(r.ok).toBe(false);
    if (r.ok) return;
    expect(r.error.code).toBe("invalid-wav");
    expect(r.error.message).toContain("bad.wav");
  });

  it("leaves the session untouched", async () => {
    const core = await withDisk();
    const before = await core.snapshot();
    await core.estimateImport({ "kick.wav": wavFixture(1, 18000, 500) }, 18000, "mix");
    const after = await core.snapshot();
    if (!before.ok || !after.ok) throw new Error("snapshot failed");
    expect(after.value.revision).toBe(before.value.revision);
  });
});

// Parity with the real session where tests used to be able to pin
// behaviour the product refuses: the missing-disk mutation guard, the
// gesture bracket around undo and redo, and deleteArea's semantics.
describe("fake core parity guards", () => {
  it("refuses every mutation on a lone half of a split pair", async () => {
    const core = createFakeCore();
    const half = new Uint8Array(IMAGE_SIZE);
    half[0] = 1;
    const opened = await core.openImage(half);
    if (!opened.ok) throw new Error(opened.error.message);
    expect(opened.value.disk?.missingDisk).toBe(2);

    const refused = await core.renameBank(0, "NOPE");
    expect(refused.ok).toBe(false);
    if (refused.ok) return;
    expect(refused.error.code).toBe("missing-disk");

    const also = await core.setBendRange(4);
    expect(also.ok).toBe(false);
    if (also.ok) return;
    expect(also.error.code).toBe("missing-disk");

    const after = await core.snapshot();
    if (!after.ok) throw new Error("snapshot failed");
    expect(after.value.revision).toBe(opened.value.revision);
  });

  it("undo inside a gesture lands the pre-gesture state, not the one before it", async () => {
    const core = createFakeCore();
    await core.newDisk("ONE");
    await core.renameDisk("TWO");
    await core.beginGesture();
    await core.renameDisk("THREE");

    // The real session closes the open bracket before undoing, so the
    // undo returns to TWO; skipping the bracket would jump to ONE.
    const undone = await core.undo();
    if (!undone.ok) throw new Error(undone.error.message);
    expect(undone.value.disk?.label).toBe("TWO");
    expect(undone.value.canRedo).toBe(true);
  });

  it("deleteArea refuses a bank's last area and removes the freed voice", async () => {
    const core = createFakeCore();
    await core.openImage(new Uint8Array(IMAGE_SIZE));

    // The stock instrument: KICK and SNARE areas, SPARE unreferenced.
    const freed = await core.deleteArea(0, 1);
    if (!freed.ok) throw new Error(freed.error.message);
    const voices = freed.value.disk?.instrument?.voices.map((v) => v.name);
    expect(voices).not.toContain("SNARE");

    const last = await core.deleteArea(0, 0);
    expect(last.ok).toBe(false);
    if (last.ok) return;
    expect(last.error.code).toBe("last-area");
  });
});
