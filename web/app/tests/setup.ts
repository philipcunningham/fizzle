// jsdom has no canvas. The waveform probes for a 2d context and shows a
// fallback when it can't get one, which is exactly what happens here;
// jsdom prints a not-implemented error anyway, and CI reads that as a
// failure. Stub the probe so the environment says plainly what it is.
HTMLCanvasElement.prototype.getContext = (() =>
  null) as typeof HTMLCanvasElement.prototype.getContext;

// jsdom implements neither PointerEvent nor pointer capture. Without
// the event class a fired drag arrives with no coordinates, because
// testing-library falls back to a plain Event that drops clientX,
// clientY, and pointerId. Without the capture methods every control
// takes its optional-call escape and the browser's path goes untested.
// Both are stubbed rather than skipped, so a test drives the code a
// browser runs.
class TestPointerEvent extends MouseEvent {
  readonly pointerId: number;
  readonly pointerType: string;
  readonly isPrimary: boolean;

  constructor(type: string, init: PointerEventInit = {}) {
    super(type, init);
    this.pointerId = init.pointerId ?? 0;
    this.pointerType = init.pointerType ?? "mouse";
    this.isPrimary = init.isPrimary ?? true;
  }
}

window.PointerEvent = TestPointerEvent as unknown as typeof PointerEvent;

// The captured ids per element, which is all hasPointerCapture needs.
const captured = new WeakMap<Element, Set<number>>();

Element.prototype.setPointerCapture = function setPointerCapture(
  this: Element,
  pointerId: number,
): void {
  const ids = captured.get(this) ?? new Set<number>();
  ids.add(pointerId);
  captured.set(this, ids);
};

Element.prototype.releasePointerCapture = function releasePointerCapture(
  this: Element,
  pointerId: number,
): void {
  captured.get(this)?.delete(pointerId);
};

Element.prototype.hasPointerCapture = function hasPointerCapture(
  this: Element,
  pointerId: number,
): boolean {
  return captured.get(this)?.has(pointerId) ?? false;
};
