// The preview engine (R20 to R22): plays core-decoded PCM at pitch with
// the DCA envelope applied and the DCF approximated, so it's clearly a
// preview and not the hardware's sound. The AudioContext is created
// lazily on the first play, which always follows a user gesture, and
// audio failure never blocks editing.
import type { EnvelopeSnapshot } from "../boundary/contract";

/** Equal temperament: exact, and unit tested (the plan asks). */
export function playbackRate(note: number, root: number): number {
  return Math.pow(2, (note - root) / 12);
}

export interface PlayOptions {
  pcm: Int16Array;
  sampleRate: number;
  root: number;
  note: number;
  velocity: number;
  dca?: EnvelopeSnapshot;
  cutoff?: number;
}

/** The slice of AudioContext the engine touches, faked in tests. */
export interface AudioContextLike {
  currentTime: number;
  state: string;
  resume(): Promise<void>;
  destination: unknown;
  createBuffer(
    channels: number,
    length: number,
    sampleRate: number,
  ): { getChannelData(channel: number): Float32Array };
  createGain(): {
    gain: {
      value: number;
      setValueAtTime(value: number, time: number): void;
      linearRampToValueAtTime(value: number, time: number): void;
      cancelScheduledValues(time: number): void;
    };
    connect(node: unknown): void;
    disconnect(): void;
  };
  createBufferSource(): {
    buffer: unknown;
    playbackRate: { value: number };
    onended: ((ev: Event) => void) | null;
    connect(node: unknown): void;
    start(): void;
    stop(at?: number): void;
  };
}

export interface AuditionEngine {
  /** Starts a note; the returned function releases it. Never throws. */
  play(options: PlayOptions): () => void;
}

// Envelope display units to preview seconds: a fast rate (99) moves in
// about 10 ms, a slow one (0) in about 2 s. Approximate on purpose.
function stageSeconds(rate: number): number {
  return 0.01 + ((99 - rate) / 99) * 2;
}

export function createAudition(
  createContext: () => AudioContextLike = () => new AudioContext(),
): AuditionEngine {
  let context: AudioContextLike | null = null;
  let failed = false;

  const ensure = (): AudioContextLike | null => {
    if (failed) return null;
    if (!context) {
      try {
        context = createContext();
      } catch {
        // R22: audio failure never blocks editing.
        failed = true;
        return null;
      }
    }
    if (context.state === "suspended") void context.resume();
    return context;
  };

  return {
    play(options: PlayOptions): () => void {
      const ctx = ensure();
      if (!ctx || options.pcm.length === 0) return () => undefined;

      const buffer = ctx.createBuffer(1, options.pcm.length, options.sampleRate);
      const channel = buffer.getChannelData(0);
      for (let i = 0; i < options.pcm.length; i++) {
        channel[i] = (options.pcm[i] ?? 0) / 32768;
      }

      const source = ctx.createBufferSource();
      source.buffer = buffer;
      source.playbackRate.value = playbackRate(options.note, options.root);

      const gain = ctx.createGain();
      const level = Math.max(0.05, options.velocity / 127);
      const now = ctx.currentTime;
      gain.gain.setValueAtTime(0, now);
      if (options.dca) {
        // Walk the stages to the sustain point, scaling stops by
        // velocity; hold there until release.
        let t = now;
        const sustain = Math.min(options.dca.sustain, options.dca.stops.length - 1);
        for (let stage = 0; stage <= sustain; stage++) {
          t += stageSeconds(options.dca.rates[stage] ?? 50);
          const stop = ((options.dca.stops[stage] ?? 99) / 99) * level;
          gain.gain.linearRampToValueAtTime(stop, t);
        }
      } else {
        gain.gain.linearRampToValueAtTime(level, now + 0.005);
      }

      source.connect(gain);
      gain.connect(ctx.destination);
      source.start();

      let released = false;
      return () => {
        if (released) return;
        released = true;
        try {
          const at = ctx.currentTime;
          gain.gain.cancelScheduledValues(at);
          gain.gain.setValueAtTime(gain.gain.value, at);
          gain.gain.linearRampToValueAtTime(0, at + 0.05);
          // Stop after the fade lands, and detach only once the source
          // ends: an immediate stop cuts at the current gain and clicks
          // on every release. A one shot that already played out fires
          // no further ended event, so the detach also runs on a timer;
          // whichever comes first wins and the second call is harmless.
          let detached = false;
          const detach = () => {
            if (detached) return;
            detached = true;
            gain.disconnect();
          };
          source.onended = detach;
          setTimeout(detach, 100);
          source.stop(at + 0.06);
        } catch {
          // A source that already ended throws on stop; harmless.
        }
      };
    },
  };
}
