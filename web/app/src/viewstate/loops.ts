// Which loop a voice repeats while a key is held. Pure, so the
// format's rule stays testable on its own. Note on caps the chain at
// min(loop_sus, loop_end) (F000:122B), so the loop that holds is the
// one named at that cap, not loopSustain alone; loopRelease can name
// it instead when loopSustain is 8 (none). An index past the last
// loop (8) means the voice names none there, and a loop whose end
// sits at or below its start is the shape a one shot import writes
// rather than a loop the sampler would repeat.
import type { LoopSnapshot, VoiceDetail } from "../boundary/contract";

export function sustainLoop(detail: VoiceDetail | undefined): LoopSnapshot | undefined {
  if (!detail) return undefined;
  const cap = Math.min(detail.loopSustain, detail.loopRelease);
  const loop = detail.loops[cap];
  if (!loop || loop.end <= loop.start) return undefined;
  return loop;
}

/** The same rule, asked of one loop: the waveform marks the drawn one. */
export function isSustainLoop(detail: VoiceDetail | undefined, index: number): boolean {
  if (!detail) return false;
  const cap = Math.min(detail.loopSustain, detail.loopRelease);
  return cap === index && sustainLoop(detail) !== undefined;
}

/**
 * Which loop a voice moves to when the key comes up. Note off raises
 * the chain's cap to loop_end (F000:1515), so the chain runs on to
 * that loop and repeats it. The rules match sustainLoop's: an index
 * past the last loop means none, and a loop whose end sits at or
 * below its start is the shape a one shot import writes.
 */
export function releaseLoop(detail: VoiceDetail | undefined): LoopSnapshot | undefined {
  if (!detail) return undefined;
  const loop = detail.loops[detail.loopRelease];
  if (!loop || loop.end <= loop.start) return undefined;
  return loop;
}

/** The same rule, asked of one loop: the waveform marks the drawn one. */
export function isReleaseLoop(detail: VoiceDetail | undefined, index: number): boolean {
  return detail?.loopRelease === index && releaseLoop(detail) !== undefined;
}
