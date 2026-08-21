// Which of a voice's eight loops repeats, and when. Two designations
// drive one chain: note on caps it at min(loop_sus, loop_end)
// (F000:122B), and note off raises the cap to loop_end (F000:1515).
// An index of 8 names no loop, and an end at or below its start is
// the shape a one shot import writes rather than a loop.
import type { LoopSnapshot, VoiceDetail } from "../boundary/contract";

function namedLoop(detail: VoiceDetail | undefined, index: number): LoopSnapshot | undefined {
  const loop = detail?.loops[index];
  if (!loop || loop.end <= loop.start) return undefined;
  return loop;
}

/** The index note on caps the chain at. */
function cap(detail: VoiceDetail): number {
  return Math.min(detail.loopSustain, detail.loopRelease);
}

export function sustainLoop(detail: VoiceDetail | undefined): LoopSnapshot | undefined {
  return detail ? namedLoop(detail, cap(detail)) : undefined;
}

export function releaseLoop(detail: VoiceDetail | undefined): LoopSnapshot | undefined {
  return detail ? namedLoop(detail, detail.loopRelease) : undefined;
}

/** The same rules, asked of one loop: the waveform marks the drawn one. */
export function isSustainLoop(detail: VoiceDetail | undefined, index: number): boolean {
  return detail !== undefined && cap(detail) === index && sustainLoop(detail) !== undefined;
}

export function isReleaseLoop(detail: VoiceDetail | undefined, index: number): boolean {
  return detail?.loopRelease === index && releaseLoop(detail) !== undefined;
}
