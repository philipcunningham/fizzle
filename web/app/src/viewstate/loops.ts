// Which loop a voice repeats while a key is held. Pure, so the
// format's rule stays testable on its own: loopSustain indexes the
// loops and an index past the last one (8) means the voice names none,
// and a loop whose end sits at or below its start is the shape a one
// shot import writes rather than a loop the sampler would repeat.
import type { LoopSnapshot, VoiceDetail } from "../boundary/contract";

export function sustainLoop(detail: VoiceDetail | undefined): LoopSnapshot | undefined {
  if (!detail) return undefined;
  const loop = detail.loops[detail.loopSustain];
  if (!loop || loop.end <= loop.start) return undefined;
  return loop;
}

/** The same rule, asked of one loop: the waveform marks the drawn one. */
export function isSustainLoop(detail: VoiceDetail | undefined, index: number): boolean {
  return detail?.loopSustain === index && sustainLoop(detail) !== undefined;
}
