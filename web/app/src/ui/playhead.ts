// Where a sounding note's playhead is. Web Audio reports no position
// for a buffer source, so it is modelled from what note on knows. The
// fold was measured against Chrome: a window ahead of the playhead
// traces to it, and one behind wraps in on the spot.

export interface Span {
  start: number;
  end: number;
}

export interface PlayheadPlan {
  /** The context clock at note on, and the rate the note plays at. */
  startedAt: number;
  rate: number;
  sampleRate: number;
  frames: number;
  /** The window a held key repeats, or null when the note plays through. */
  window: Span | null;
  /** The context clock at the key coming up, or null while it is down. */
  releasedAt: number | null;
  releaseWindow: Span | null;
}

/** A position inside a span: linear to its end, then modulo back in. */
function fold(frame: number, span: Span | null, frames: number): number {
  if (!span || span.end <= span.start) return Math.min(frame, frames - 1);
  if (frame < span.end) return frame;
  return span.start + ((frame - span.start) % (span.end - span.start));
}

export function frameAt(plan: PlayheadPlan, now: number): number {
  const travelled = (from: number) => Math.max(0, (now - from) * plan.rate * plan.sampleRate);
  const held = fold(travelled(plan.startedAt), plan.window, plan.frames);
  if (plan.releasedAt === null) return held;
  const atRelease = fold(
    Math.max(0, (plan.releasedAt - plan.startedAt) * plan.rate * plan.sampleRate),
    plan.window,
    plan.frames,
  );
  return fold(
    atRelease + travelled(plan.releasedAt),
    plan.releaseWindow ?? plan.window,
    plan.frames,
  );
}
