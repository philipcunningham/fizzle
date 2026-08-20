// The preview engine (R20 to R22): plays core-decoded PCM at pitch with
// the DCA envelope applied and the DCF approximated, so it's clearly a
// preview and not the hardware's sound. The AudioContext is created
// lazily on the first play, which always follows a user gesture, and
// audio failure never blocks editing.
import type { EnvelopeSnapshot } from "../boundary/contract";
import type { Scaling } from "./dca";
import { amplitude, amplitudeAt, attack, levelAt, release, totalSeconds } from "./dca";

/**
 * The longest release the preview holds a source open for. The
 * envelope model is faithful past this; the scheduler is not.
 */
const MAX_TAIL_SECONDS = 30;

/**
 * The fade the preview adds after an envelope finishes. The envelope's
 * own floor is a code well above silence, and the hardware mutes from
 * there by slewing the output code to a sentinel below the range,
 * which frees the voice. Cutting the source at the floor instead
 * clicks on the end of every note.
 */
const MUTE_SECONDS = 0.008;

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
  /**
   * The four voice fields that bend the envelope by the press and the
   * key. Absent means the envelope runs as stored.
   */
  dcaFollow?: Pick<Scaling, "levelKF" | "rateKF" | "velLevel" | "velRate">;
  cutoff?: number;
  /**
   * The sustain loop, in voice-relative frames. Present only when the
   * voice names one, and it repeats for as long as the note is held.
   */
  loop?: { start: number; end: number };
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
      exponentialRampToValueAtTime(value: number, time: number): void;
      cancelScheduledValues(time: number): void;
    };
    connect(node: unknown): void;
    disconnect(): void;
  };
  createBufferSource(): {
    buffer: unknown;
    playbackRate: { value: number };
    loop: boolean;
    loopStart: number;
    loopEnd: number;
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
      // Web Audio loops the buffer itself, in buffer seconds, so the
      // playback rate carries the loop along with the pitch. The bounds
      // are judged against the buffer just built rather than trusted,
      // because Web Audio answers a loop it can't honour by playing
      // something worse than no loop: an end at or below the start
      // repeats the whole sample, and a start past the buffer plays
      // total silence, first pass included.
      if (options.loop) {
        const end = Math.min(options.loop.end, options.pcm.length);
        if (options.loop.start < options.pcm.length && end > options.loop.start) {
          source.loopStart = options.loop.start / options.sampleRate;
          source.loopEnd = end / options.sampleRate;
          source.loop = true;
        }
      }

      const gain = ctx.createGain();
      const now = ctx.currentTime;
      const scaling: Scaling | undefined =
        options.dca && options.dcaFollow
          ? {
              velocity: options.velocity,
              note: options.note,
              // The key follow's reference. On the voices tab this is
              // the voice's own root; on the banks tab it is the
              // area's, and the two are separate editable fields.
              centre: options.root,
              ...options.dcaFollow,
            }
          : undefined;
      // Velocity reaches a voice through its own sensitivity fields,
      // which scale every stop at note on. Applying it again here as a
      // master level would square the dynamics, so it only stands in
      // when there is no envelope to carry it.
      const level = scaling ? 1 : Math.max(0.05, options.velocity / 127);
      const attackStages = options.dca ? attack(options.dca, scaling) : [];
      const attackSeconds = totalSeconds(attackStages);
      if (options.dca) {
        // The chip's amplifier is calibrated in dB, so a ramp that is
        // exponential in amplitude is the linear one the firmware's
        // accumulator runs. It also cannot start from silence, which
        // suits: the envelope's own zero is a code 57 dB down rather
        // than the chip's mute.
        gain.gain.setValueAtTime(amplitude(0) * level, now);
        let t = now;
        for (const stage of attackStages) {
          t += stage.seconds;
          gain.gain.exponentialRampToValueAtTime(
            amplitude(stage.level) * level,
            Math.max(t, now + 0.001),
          );
        }
      } else {
        gain.gain.setValueAtTime(0, now);
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
          // Where the envelope had got to, from the model rather than
          // from the parameter: cancelScheduledValues drops the ramp in
          // flight, so a read back here gives the stop before it.
          // The level is what the release stages are timed from; the
          // loudness is where the scheduled ramp had actually got to.
          // Reading the level's line for both makes the note step as
          // the key comes up, because the two lines only meet at a
          // stage's ends.
          const elapsed = at - now;
          const from = options.dca ? levelAt(attackStages, elapsed) : 1;
          const heard = options.dca ? amplitudeAt(attackStages, elapsed) : 1;
          gain.gain.cancelScheduledValues(at);
          gain.gain.setValueAtTime(heard * level, at);
          // A key that comes up before the envelope reached its sustain
          // stage runs the end stage alone, so the engine has to know
          // which happened.
          const reached = at - now >= attackSeconds;
          const releaseStages = options.dca ? release(options.dca, from, reached, scaling) : [];
          let t = at;
          for (const stage of releaseStages) {
            t += stage.seconds;
            gain.gain.exponentialRampToValueAtTime(
              amplitude(stage.level) * level,
              Math.max(t, at + 0.001),
            );
          }
          // A voice with no release stages keeps the short fade, which
          // is also what stops the click on a voice with no envelope.
          // The cap is the preview's own: a stage stored at a rate of
          // zero runs for 174 s, and holding a source open that long
          // for every soft press is not what a preview is for.
          const tail =
            releaseStages.length > 0
              ? Math.min(totalSeconds(releaseStages), MAX_TAIL_SECONDS)
              : 0.05;
          if (releaseStages.length === 0) {
            gain.gain.linearRampToValueAtTime(0, at + 0.05);
          } else {
            gain.gain.linearRampToValueAtTime(0, at + tail + MUTE_SECONDS);
          }
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
          const ends = tail + (releaseStages.length > 0 ? MUTE_SECONDS : 0);
          setTimeout(detach, Math.ceil((ends + 0.06) * 1000));
          source.stop(at + ends + 0.01);
        } catch {
          // A source that already ended throws on stop; harmless.
        }
      };
    },
  };
}
