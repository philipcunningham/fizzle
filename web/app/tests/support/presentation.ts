import type { Snapshot } from "../../src/boundary/contract";
import { IMAGE_SIZE, ok } from "../../src/boundary/contract";
import { createCoreStub, emptySnapshot } from "../../src/core/stub";

export const diskSnapshot = (overrides: Partial<Snapshot> = {}): Snapshot =>
  emptySnapshot({
    revision: 1,
    disk: {
      label: "MY DISK",
      usedBytes: 4096,
      audioBytes: 2048,
      memoryBytes: 1024 * 1024,
      capacityBytes: IMAGE_SIZE,
      disks: 1,
      files: [],
    },
    ...overrides,
  });

export const instrumentSnapshot = (overrides: Partial<Snapshot> = {}): Snapshot => {
  const snapshot = diskSnapshot();
  if (!snapshot.disk) throw new Error("presentation fixture lost its disk");
  return {
    ...snapshot,
    disk: {
      ...snapshot.disk,
      files: [
        { name: "FULL-DATA-FZ", type: "full", sizeBytes: 4096 },
        { name: "SPARE.FZV", type: "voice", sizeBytes: 2048 },
      ],
      instrument: {
        fileName: "FULL-DATA-FZ",
        banks: [
          {
            name: "BANK A",
            areas: [
              {
                voiceSlot: 0,
                voiceName: "KICK",
                keyLow: 0,
                keyHigh: 127,
                root: 60,
                velLow: 1,
                velHigh: 127,
                midiChannel: 1,
                output: 255,
                outputLabel: "all",
                volume: 0,
              },
            ],
          },
        ],
        voices: [{ slot: 0, name: "KICK", referenced: true }],
      },
    },
    ...overrides,
  };
};

export function presentationCore(snapshot: Snapshot = instrumentSnapshot()) {
  return createCoreStub({
    snapshot: () => Promise.resolve(ok(snapshot)),
    newDisk: () => Promise.resolve(ok(diskSnapshot())),
    openImage: () => Promise.resolve(ok(snapshot)),
    closeDisk: () => Promise.resolve(ok(emptySnapshot({ revision: snapshot.revision + 1 }))),
    exportImage: () => Promise.resolve(ok(new Uint8Array([1, 2, 3]))),
    extractFile: () => Promise.resolve(ok(new Uint8Array([1, 2, 3]))),
    setSampleMemory: (bytes) =>
      Promise.resolve(
        ok({
          ...snapshot,
          revision: snapshot.revision + 1,
          disk: snapshot.disk ? { ...snapshot.disk, memoryBytes: bytes } : null,
        }),
      ),
  });
}
