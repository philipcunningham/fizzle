// Audition preview for the mockup: a WebAudio oscillator with the DCA
// envelope approximated. Clearly not the hardware's sound; it exists
// so audition has something to hear. Starts only from a user
// gesture. A note sustains while held and releases on note off.

import type { Voice } from "./data/model";
import { noteFrequency } from "./data/model";

let ctx: AudioContext | null = null;

function audioContext(): AudioContext | null {
  try {
    ctx = ctx ?? new AudioContext();
    if (ctx.state === "suspended") void ctx.resume();
    return ctx;
  } catch {
    return null; // audio failure never blocks editing
  }
}

// auditionStart begins a held note and returns its release function, or
// null when audio is unavailable.
export function auditionStart(voice: Voice, midiNote: number, velocity: number): (() => void) | null {
  const ac = audioContext();
  if (!ac) return null;

  const now = ac.currentTime;
  const osc = ac.createOscillator();
  const gain = ac.createGain();
  const filter = ac.createBiquadFilter();

  osc.type = "sawtooth";
  osc.frequency.value = noteFrequency(midiNote) * Math.pow(2, voice.tune / 768);

  filter.type = "lowpass";
  filter.frequency.value = 200 + (voice.cutoff / 127) * 8000;
  filter.Q.value = (voice.resonance / 127) * 12;

  const peak = 0.25 * (0.3 + 0.7 * (velocity / 127));
  const first = voice.dcaEnv.stages[0];
  const attack = 0.005 + (1 - first.rate / 127) * 0.2;
  const sustainLevel = Math.max(0.002, (voice.dcaEnv.stages[voice.dcaEnv.sustainStage]?.level ?? 64) / 127) * peak;

  gain.gain.setValueAtTime(0.0001, now);
  gain.gain.exponentialRampToValueAtTime(Math.max(0.001, peak), now + attack);
  // Settle toward the sustain stage's level and hold there while held.
  gain.gain.setTargetAtTime(sustainLevel, now + attack, 0.25);

  osc.connect(filter).connect(gain).connect(ac.destination);
  osc.start(now);

  let released = false;
  return () => {
    if (released) return;
    released = true;
    const t = ac.currentTime;
    const endStage = voice.dcaEnv.stages[voice.dcaEnv.endStage];
    const release = 0.05 + (1 - (endStage?.rate ?? 64) / 127) * 0.6;
    gain.gain.cancelScheduledValues(t);
    gain.gain.setTargetAtTime(0.0001, t, release / 3);
    osc.stop(t + release + 0.3);
  };
}
