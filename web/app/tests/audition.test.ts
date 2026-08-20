// The preview engine (R20 to R22): exact playback-rate maths, an
// AudioContext that exists only after a gesture, envelope scheduling,
// and a release path. Tests drive a recording fake context; the plan
// says test the scheduling and the fallback, not the sound.
import { describe, expect, it } from "vitest";
import { createAudition, playbackRate } from "../src/ui/audition";
import { release as releaseStages, totalSeconds } from "../src/ui/dca";

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
  /** Every gain event in order, with the time it was scheduled for. */
  events: { kind: "hold" | "ramp"; value: number; time: number }[];
  loop: boolean;
  loopStart: number;
  loopEnd: number;
  stopAt: number | undefined;
}

function blank(): Scheduled {
  return {
    rate: 0,
    started: false,
    stopped: false,
    gains: [],
    events: [],
    loop: false,
    loopStart: 0,
    loopEnd: 0,
    stopAt: undefined,
  };
}

function fakeContext(record: Scheduled) {
  const gainNode = {
    gain: {
      value: 1,
      setValueAtTime: (v: number, t: number) => {
        record.gains.push(v);
        record.events.push({ kind: "hold", value: v, time: t });
      },
      linearRampToValueAtTime: (v: number, t: number) => {
        record.gains.push(v);
        record.events.push({ kind: "ramp", value: v, time: t });
      },
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
      stop: (at?: number) => {
        record.stopped = true;
        record.stopAt = at;
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

  // Handing these on would repeat the whole sample, since Web Audio
  // reads bounds that don't rise as no bounds at all.
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

  // Chrome plays a source whose loop starts past its buffer as total
  // silence, not even the first pass, so bounds are judged against the
  // buffer the engine just built rather than trusted.
  it("ignores a loop that starts past the samples it holds", () => {
    const record = blank();
    const engine = createAudition(() => fakeContext(record));
    engine.play({
      pcm: new Int16Array(4096),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
      loop: { start: 9000, end: 12000 },
    });
    expect(record.loop).toBe(false);
  });

  it("holds a loop that overruns the samples to what it has", () => {
    const record = blank();
    const engine = createAudition(() => fakeContext(record));
    engine.play({
      pcm: new Int16Array(4096),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
      loop: { start: 1000, end: 9000 },
    });
    expect(record.loop).toBe(true);
    expect(record.loopStart).toBeCloseTo(1000 / 18000, 10);
    expect(record.loopEnd).toBeCloseTo(4096 / 18000, 10);
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
        loop: false,
        loopStart: 0,
        loopEnd: 0,
        onended: null,
        connect: () => undefined,
        start: () => undefined,
        stop: (at?: number) => {
          stopArgs.push(at);
        },
      }),
    };
    const engine = createAudition(() => context);
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

// The envelope the firmware runs, through Web Audio's scheduler. The
// timing model is tested on its own in dca.test.ts; these pin that the
// engine schedules what the model gives it, and that a note stops when
// its release finishes rather than during it.
describe("the DCA envelope reaches the scheduler", () => {
  const pluck = {
    sustain: 1,
    end: 2,
    // Instant to full, then a slow fall, then a slow release.
    rates: [99, 30, 30, 50, 50, 50, 50, 50],
    stops: [99, 60, 0, 0, 0, 0, 0, 0],
  };

  // The figures are the disassembly's, not this engine's: a full sweep
  // takes 0.387 s at panel 50 and 2.301 s at panel 25. A ramp per stage
  // is a count the old approximation also produced, so the assertion is
  // when each ramp lands.
  const sweep = {
    sustain: 1,
    end: 2,
    rates: [50, 25, 50, 50, 50, 50, 50, 50],
    stops: [99, 0, 0, 0, 0, 0, 0, 0],
  };

  it("schedules each attack stage at the time the firmware takes", () => {
    const record = blank();
    const engine = createAudition(() => fakeContext(record));
    engine.play({
      pcm: new Int16Array(64),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 127,
      dca: sweep,
    });
    // Silence at note on, then a ramp per stage to the sustain point.
    expect(record.events.map((e) => e.kind)).toEqual(["hold", "ramp", "ramp"]);
    expect(record.events[0]?.time).toBe(0);
    expect(record.events[1]?.time).toBeCloseTo(0.387, 2);
    expect(record.events[2]?.time).toBeCloseTo(0.387 + 2.301, 2);
  });

  // Velocity reaches the envelope through the voice's own sensitivity
  // fields, which scale every stop at note on. Applying it a second
  // time as a master level would square the dynamics.
  it("does not scale by velocity a second time", () => {
    const flat = { levelKF: 0, rateKF: 0, velLevel: 0, velRate: 0 };
    const peak = (velocity: number) => {
      const record = blank();
      const engine = createAudition(() => fakeContext(record));
      engine.play({
        pcm: new Int16Array(64),
        sampleRate: 18000,
        root: 60,
        note: 60,
        velocity,
        dca: sweep,
        dcaFollow: flat,
      });
      return Math.max(...record.gains);
    };
    // A voice with no velocity sensitivity plays at one loudness,
    // which is what the hardware does.
    expect(peak(1)).toBeCloseTo(1, 6);
    expect(peak(127)).toBeCloseTo(1, 6);
  });

  it("holds the note until its release stages finish", () => {
    const stopArgs: (number | undefined)[] = [];
    const context = {
      currentTime: 0,
      state: "running" as const,
      resume: () => Promise.resolve(),
      destination: {},
      createBuffer: (_ch: number, length: number) => ({
        getChannelData: () => new Float32Array(length),
      }),
      createGain: () => ({
        gain: {
          value: 0.5,
          setValueAtTime: () => undefined,
          linearRampToValueAtTime: () => undefined,
          cancelScheduledValues: () => undefined,
        },
        connect: () => undefined,
        disconnect: () => undefined,
      }),
      createBufferSource: () => ({
        buffer: null,
        playbackRate: { value: 1 },
        loop: false,
        loopStart: 0,
        loopEnd: 0,
        onended: null,
        connect: () => undefined,
        start: () => undefined,
        stop: (at?: number) => {
          stopArgs.push(at);
        },
      }),
    };
    const engine = createAudition(() => context);
    const release = engine.play({
      pcm: new Int16Array(64),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
      dca: pluck,
    });
    release();
    // The source stops when the release finishes, not during it. The
    // model is tested on its own, so the assertion is that the engine
    // honours whatever it says rather than a figure copied here. This
    // context's clock never moves, so the key comes up at note on:
    // the first stage is instant, which puts the note at full, and
    // nothing has reached the sustain stage yet.
    const stages = releaseStages(pluck, 1, false);
    expect(stopArgs[0]).toBeCloseTo(totalSeconds(stages) + 0.01, 3);
    // And that is far beyond the fixed 60 ms fade it used to take.
    expect(stopArgs[0]).toBeGreaterThan(0.5);
  });

  // A context whose clock the test moves, so a key can come up partway
  // through a stage.
  function clocked(record: Scheduled, currentTime: { now: number }) {
    const base = fakeContext(record);
    return {
      ...base,
      get currentTime() {
        return currentTime.now;
      },
    };
  }

  // Web Audio's cancelScheduledValues drops the ramp in flight, so the
  // parameter's value reads back as the stop before it rather than the
  // level the note had reached. The model knows where the envelope is,
  // and that is what the release has to start from.
  it("releases from the level the envelope had reached", () => {
    const record = blank();
    const clock = { now: 0 };
    const engine = createAudition(() => clocked(record, clock));
    const stop = engine.play({
      pcm: new Int16Array(64),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 127,
      dca: sweep,
    });
    // Halfway up a stage that the firmware takes 0.387 s to climb.
    clock.now = 0.1935;
    record.events.length = 0;
    stop();
    const hold = record.events.find((e) => e.kind === "hold");
    expect(hold?.value).toBeCloseTo(0.5, 2);
  });

  // A stored rate of zero runs a stage for 174 s, and velocity rate
  // scaling can stretch an ordinary one nearly as far. The preview
  // holds a source open for the release, so it caps how long that is.
  it("caps how long a release can hold the source open", () => {
    const record = blank();
    const engine = createAudition(() => fakeContext(record));
    const stop = engine.play({
      pcm: new Int16Array(64),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 127,
      dca: {
        sustain: 0,
        end: 1,
        rates: [99, 0, 0, 0, 0, 0, 0, 0],
        stops: [99, 0, 0, 0, 0, 0, 0, 0],
      },
    });
    stop();
    expect(record.stopAt).toBeGreaterThan(1);
    expect(record.stopAt).toBeLessThanOrEqual(30.01);
  });

  it("keeps the short fade for a voice with no envelope", () => {
    const stopArgs: (number | undefined)[] = [];
    const context = {
      currentTime: 0,
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
        disconnect: () => undefined,
      }),
      createBufferSource: () => ({
        buffer: null,
        playbackRate: { value: 1 },
        loop: false,
        loopStart: 0,
        loopEnd: 0,
        onended: null,
        connect: () => undefined,
        start: () => undefined,
        stop: (at?: number) => {
          stopArgs.push(at);
        },
      }),
    };
    const engine = createAudition(() => context);
    engine.play({
      pcm: new Int16Array(64),
      sampleRate: 18000,
      root: 60,
      note: 60,
      velocity: 100,
    })();
    expect(stopArgs[0]).toBeLessThan(0.2);
  });
});
