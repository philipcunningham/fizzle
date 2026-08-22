// The preview engine (R20 to R22): plays core-decoded PCM at pitch with
// the DCA envelope applied and the DCF approximated, so it's clearly a
// preview and not the hardware's sound. The AudioContext is created
// lazily on the first play, which always follows a user gesture, and
// audio failure never blocks editing.
import type { EnvelopeSnapshot } from "../boundary/contract";
import type { Scaling } from "./dca";
import { amplitude, amplitudeAt, attack, levelAt, oneShot, release, totalSeconds } from "./dca";
import { frameAt, type PlayheadPlan, type Span } from "./playhead";

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
  /**
   * The loop the chain moves to when the key comes up. Note off raises
   * the cap to loop_end (F000:1515), so the voice runs on to that loop
   * and repeats it while the envelope plays out.
   */
  releaseLoop?: { start: number; end: number };
  /**
   * Called once when the note is over, whether it played itself out or
   * a key released it. The strip stops drawing its playhead there.
   * It runs on the schedule rather than on now()'s latency corrected
   * clock, so on a slow sink it lands a few tens of milliseconds
   * before the last of the fade reaches the ear.
   */
  onEnded?: () => void;
}

/**
 * The loop window a buffer can honour, in frames, or null when it
 * cannot. Web Audio answers bounds it cannot honour by playing
 * something worse than no loop: an end at or below the start repeats
 * the whole sample, and a start past the buffer plays silence.
 */
function loopWindow(loop: Span | undefined, frames: number): Span | null {
  if (!loop) return null;
  const end = Math.min(loop.end, frames);
  if (loop.start >= frames || end <= loop.start) return null;
  return { start: loop.start, end };
}

/** The slice of AudioContext the engine touches, faked in tests. */
export interface AudioContextLike {
  currentTime: number;
  readonly outputLatency?: number;
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

/** A sounding note: how to release it, and where its playhead is. */
export interface Note {
  release: () => void;
  /** The frame the playhead sits on at a context time. */
  frameAt: (now: number) => number;
  /** False when nothing was scheduled, so there is nothing to draw. */
  sounding: boolean;
}

/** A note that never sounded still answers, so callers need no guard. */
const silent: Note = { release: () => undefined, frameAt: () => 0, sounding: false };

export interface AuditionEngine {
  /** Starts a note; the returned note releases it. Never throws. */
  play(options: PlayOptions): Note;
  /**
   * The clock the playhead reads, behind the schedule by what the
   * output stage has yet to play, so the line matches what is heard.
   */
  now(): number;
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
    now(): number {
      if (!context) return 0;
      return context.currentTime - (context.outputLatency ?? 0);
    },
    play(options: PlayOptions): Note {
      const ctx = ensure();
      if (!ctx || options.pcm.length === 0) return silent;

      const buffer = ctx.createBuffer(1, options.pcm.length, options.sampleRate);
      const channel = buffer.getChannelData(0);
      for (let i = 0; i < options.pcm.length; i++) {
        channel[i] = (options.pcm[i] ?? 0) / 32768;
      }

      const source = ctx.createBufferSource();
      source.buffer = buffer;
      const rate = playbackRate(options.note, options.root);
      source.playbackRate.value = rate;
      // Web Audio loops the buffer itself, in buffer seconds, so the
      // playback rate carries the loop along with the pitch.
      const window = loopWindow(options.loop, options.pcm.length);
      if (window) {
        source.loopStart = window.start / options.sampleRate;
        source.loopEnd = window.end / options.sampleRate;
        source.loop = true;
      }

      const now = ctx.currentTime;
      const plan: PlayheadPlan = {
        startedAt: now,
        rate,
        sampleRate: options.sampleRate,
        frames: options.pcm.length,
        window,
        releasedAt: null,
        releaseWindow: loopWindow(options.releaseLoop, options.pcm.length),
      };

      const gain = ctx.createGain();
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
        // suits: the envelope's own zero is a code above the chip's
        // mute rather than silence.
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

      let detached = false;
      const detach = () => {
        if (detached) return;
        detached = true;
        gain.disconnect();
        options.onEnded?.();
      };
      source.onended = detach;

      // A one shot finishes while the key is down, and the firmware
      // frees its slot there. Holding the source open instead leaves a
      // looping voice droning at the envelope's floor, which is a code
      // rather than silence.
      if (options.dca && oneShot(options.dca)) {
        const ends = now + attackSeconds + MUTE_SECONDS;
        gain.gain.linearRampToValueAtTime(0, ends);
        source.stop(ends + 0.01);
        setTimeout(detach, Math.ceil((attackSeconds + MUTE_SECONDS + 0.06) * 1000));
      }

      let released = false;
      const releaseNote = () => {
        if (released) return;
        released = true;
        const at = ctx.currentTime;
        // Recorded before the scheduling, so a throw below still
        // leaves the playhead honest about where the note is.
        plan.releasedAt = at;
        try {
          // The cap moves to the end loop, and Chrome carries the
          // playhead there: a window ahead traces to it, and a window
          // behind wraps into it within a render quantum.
          const onRelease = plan.releaseWindow;
          if (onRelease) {
            source.loopStart = onRelease.start / options.sampleRate;
            source.loopEnd = onRelease.end / options.sampleRate;
            source.loop = true;
          }
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
          // Stop after the fade lands. The detach runs on a timer as
          // well as on the source ending, because a one shot that
          // already played out fires no further ended event; whichever
          // comes first wins and the second call is harmless.
          const ends = tail + (releaseStages.length > 0 ? MUTE_SECONDS : 0);
          setTimeout(detach, Math.ceil((ends + 0.06) * 1000));
          source.stop(at + ends + 0.01);
        } catch {
          // A source that already ended throws on stop; harmless.
        }
      };

      return {
        release: releaseNote,
        frameAt: (when: number) => frameAt(plan, when),
        sounding: true,
      };
    },
  };
}
