// The preview engine (R20 to R22): exact playback-rate maths, an
// AudioContext that exists only after a gesture, envelope scheduling,
// and a release path. Tests drive a recording fake context; the plan
// says test the scheduling and the fallback, not the sound.
import { describe, expect, it } from "vitest";
import { createAudition, playbackRate } from "../src/ui/audition";

describe("playback rate maths", () => {
  it("is exact per equal temperament", () => {
    expect(playbackRate(60, 60)).toBe(1);
    expect(playbackRate(72, 60)).toBe(2);
    expect(playbackRate(48, 60)).toBe(0.5);
    expect(playbackRate(69, 60)).toBeCloseTo(Math.pow(2, 9 / 12), 12);
  });
});

interface Scheduled {
  rate: number;
  started: boolean;
  stopped: boolean;
  gains: number[];
}

function fakeContext(record: Scheduled) {
  const gainNode = {
    gain: {
      value: 1,
      setValueAtTime: (v: number) => record.gains.push(v),
      linearRampToValueAtTime: (v: number) => record.gains.push(v),
      cancelScheduledValues: () => undefined,
    },
    connect: () => undefined,
    disconnect: () => undefined,
  };
  return {
    currentTime: 0,
    state: "running" as const,
    resume: () => Promise.resolve(),
    destination: {},
    createBuffer: (_ch: number, length: number, sampleRate: number) => ({
      length,
      sampleRate,
      getChannelData: () => new Float32Array(length),
    }),
    createGain: () => gainNode,
    createBufferSource: () => ({
      buffer: null as unknown,
      playbackRate: {
        get value() {
          return record.rate;
        },
        set value(v: number) {
          record.rate = v;
        },
      },
      connect: () => undefined,
      start: () => {
        record.started = true;
      },
      stop: () => {
        record.stopped = true;
      },
      onended: null,
    }),
  };
}

describe("audition engine", () => {
  it("creates the context only on the first play, not on construction", () => {
    let created = 0;
    const record: Scheduled = { rate: 0, started: false, stopped: false, gains: [] };
    const engine = createAudition(() => {
      created += 1;
      return fakeContext(record);
    });
    expect(created).toBe(0);
    engine.play({
      pcm: new Int16Array(64),
      sampleRate: 18000,
      root: 60,
      note: 72,
      velocity: 127,
    });
    expect(created).toBe(1);
    engine.play({
      pcm: new Int16Array(64),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 64,
    });
    expect(created).toBe(1);
  });

  it("schedules the source at the exact playback rate and starts it", () => {
    const record: Scheduled = { rate: 0, started: false, stopped: false, gains: [] };
    const engine = createAudition(() => fakeContext(record));
    engine.play({
      pcm: new Int16Array(64),
      sampleRate: 18000,
      root: 60,
      note: 72,
      velocity: 127,
    });
    expect(record.rate).toBe(2);
    expect(record.started).toBe(true);
    expect(record.gains.length).toBeGreaterThan(0);
  });

  it("release stops the source", () => {
    const record: Scheduled = { rate: 0, started: false, stopped: false, gains: [] };
    const engine = createAudition(() => fakeContext(record));
    const release = engine.play({
      pcm: new Int16Array(64),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
    });
    release();
    expect(record.stopped).toBe(true);
  });

  it("a context that fails to build never throws (R22)", () => {
    const engine = createAudition(() => {
      throw new Error("no audio hardware");
    });
    const release = engine.play({
      pcm: new Int16Array(8),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 1,
    });
    release();
  });
});
