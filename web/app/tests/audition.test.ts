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
  loop: boolean;
  loopStart: number;
  loopEnd: number;
}

function blank(): Scheduled {
  return {
    rate: 0,
    started: false,
    stopped: false,
    gains: [],
    loop: false,
    loopStart: 0,
    loopEnd: 0,
  };
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
      get loop() {
        return record.loop;
      },
      set loop(v: boolean) {
        record.loop = v;
      },
      get loopStart() {
        return record.loopStart;
      },
      set loopStart(v: number) {
        record.loopStart = v;
      },
      get loopEnd() {
        return record.loopEnd;
      },
      set loopEnd(v: number) {
        record.loopEnd = v;
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
    const record = blank();
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
    const record = blank();
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
    const record = blank();
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

// A voice that names a sustain loop repeats it for as long as the key
// is held, which is what the sampler does. Loop positions arrive in
// voice-relative frames; Web Audio wants seconds into the buffer.
describe("the sustain loop repeats while the key is held", () => {
  it("hands Web Audio the loop bounds in seconds", () => {
    const record = blank();
    const engine = createAudition(() => fakeContext(record));
    engine.play({
      pcm: new Int16Array(18000),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
      loop: { start: 4000, end: 12000 },
    });
    expect(record.loop).toBe(true);
    expect(record.loopStart).toBeCloseTo(4000 / 18000, 10);
    expect(record.loopEnd).toBeCloseTo(12000 / 18000, 10);
  });

  it("plays a voice with no sustain loop straight through", () => {
    const record = blank();
    const engine = createAudition(() => fakeContext(record));
    engine.play({
      pcm: new Int16Array(18000),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
    });
    expect(record.loop).toBe(false);
  });

  // A freshly imported one shot carries start equal to end, so this is
  // the common shape rather than a corner case.
  it("ignores a loop whose end sits at or below its start", () => {
    const record = blank();
    const engine = createAudition(() => fakeContext(record));
    engine.play({
      pcm: new Int16Array(18000),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
      loop: { start: 9000, end: 9000 },
    });
    expect(record.loop).toBe(false);
    engine.play({
      pcm: new Int16Array(18000),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
      loop: { start: 9000, end: 200 },
    });
    expect(record.loop).toBe(false);
  });

  // Looping doesn't change the release: the fade still ends the note,
  // and a source left looping would sound forever.
  it("still stops on release", () => {
    const record = blank();
    const engine = createAudition(() => fakeContext(record));
    const release = engine.play({
      pcm: new Int16Array(18000),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
      loop: { start: 4000, end: 12000 },
    });
    release();
    expect(record.stopped).toBe(true);
  });
});

// The release schedules a fade and stops after it: an immediate stop
// would cut the audio at the current gain and click on every release.
describe("release fades before stopping", () => {
  it("stops the source after the fade window, not now", () => {
    const stopArgs: (number | undefined)[] = [];
    let disconnected = 0;
    const context = {
      currentTime: 1,
      state: "running" as const,
      resume: () => Promise.resolve(),
      destination: {},
      createBuffer: (_ch: number, length: number) => ({
        getChannelData: () => new Float32Array(length),
      }),
      createGain: () => ({
        gain: {
          value: 1,
          setValueAtTime: () => undefined,
          linearRampToValueAtTime: () => undefined,
          cancelScheduledValues: () => undefined,
        },
        connect: () => undefined,
        disconnect: () => {
          disconnected += 1;
        },
      }),
      createBufferSource: () => ({
        buffer: null as unknown,
        playbackRate: { value: 1 },
        connect: () => undefined,
        start: () => undefined,
        stop: (at?: number) => {
          stopArgs.push(at);
        },
      }),
    };
    const engine = createAudition(() => context as never);
    const release = engine.play({
      pcm: new Int16Array(64),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
    });
    release();
    expect(stopArgs).toHaveLength(1);
    expect(stopArgs[0]).toBeGreaterThanOrEqual(1.05);
    // The gain detaches when the source ends, never synchronously:
    // an immediate disconnect silences the fade it just scheduled.
    expect(disconnected).toBe(0);
  });
});
