// Area resolution for the banks tab keyboard: which of a bank's
// areas sound for a pressed key. Pure, so the hardware rule stays
// testable on its own: an area sounds when the note sits in its key
// range and the velocity in its velocity range, edges included, and
// every matching area sounds at once, which is how the FZ layers key
// and velocity overlaps.
import type { AreaSnapshot } from "../boundary/contract";

export function matchAreas(areas: AreaSnapshot[], note: number, velocity: number): AreaSnapshot[] {
  return areas.filter(
    (a) => note >= a.keyLow && note <= a.keyHigh && velocity >= a.velLow && velocity <= a.velHigh,
  );
}
