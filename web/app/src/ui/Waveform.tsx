// The waveform (R17) over real core peaks: wavesurfer draws the strip,
// the selected loop is a draggable region, edges snap to the nearest
// zero-crossing bucket, and the numeric fields beside always show the
// frames the core confirmed. jsdom has no canvas, so the component
// degrades to nothing there and the loops table keeps the flow testable.
import { useEffect, useRef, useState } from "react";
import WaveSurfer from "wavesurfer.js";
import RegionsPlugin from "wavesurfer.js/dist/plugins/regions.js";
import type { LoopSnapshot } from "../boundary/contract";
import { NumberCell } from "./NumberCell";
import { clamp } from "./format";

interface Props {
  /** Remount key: the voice identity (slot or file). */
  voiceKey: string;
  frames: number;
  peaks: Int16Array | null;
  loopIndex: number;
  loop: LoopSnapshot;
  /** The drawn loop is the one the voice repeats while a key is held. */
  sustain: boolean;
  onSetLoop: (start: number, end: number) => void;
  onGestureBegin?: () => void;
  onGestureCommit?: () => void;
}

// One synthetic second regardless of frame count keeps the maths in
// frame space: frame f sits at time f/frames.
const DURATION = 1;

// The one region the strip draws, in its two states: amber while it is
// just the loop under edit, green once it is the loop that repeats.
// Hue can't be what carries that, since the two grounds sit close
// together for a red green viewer, so the caption says it in words and
// the fill is the glance level hint.
const LOOP_FILL = "rgba(255, 176, 0, 0.35)";
const SUSTAIN_FILL = "rgba(51, 209, 122, 0.35)";

// Moves a frame to the centre of the nearest peak bucket that crosses
// zero, searching at most 5 percent of the span: a snap assists, it
// never teleports.
function snapToZero(peaks: Int16Array, frames: number, frame: number): number {
  const buckets = peaks.length / 2;
  if (buckets < 1 || frames < 1) return frame;
  const home = Math.round((frame / frames) * buckets);
  const reach = Math.max(1, Math.floor(buckets * 0.05));
  for (let d = 0; d <= reach; d++) {
    for (const b of d === 0 ? [home] : [home - d, home + d]) {
      if (b < 0 || b >= buckets) continue;
      const min = peaks[b * 2] ?? 0;
      const max = peaks[b * 2 + 1] ?? 0;
      if (min <= 0 && max >= 0) {
        return clamp(Math.round(((b + 0.5) / buckets) * frames), 0, frames);
      }
    }
  }
  return frame;
}

export function Waveform({
  voiceKey,
  frames,
  peaks,
  loopIndex,
  loop,
  sustain,
  onSetLoop,
  onGestureBegin,
  onGestureCommit,
}: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const wsRef = useRef<WaveSurfer | null>(null);
  const regionRef = useRef<ReturnType<RegionsPlugin["addRegion"]> | null>(null);
  // Rebuilds the region from scratch: the mount effect owns it, the
  // loop effect calls it when the region has to change shape.
  const remakeRegionRef = useRef<((start: number, end: number) => void) | null>(null);
  const draggingRef = useRef(false);
  const [zoom, setZoom] = useState(1);
  const [failed, setFailed] = useState(false);
  const [live, setLive] = useState<{ start: number; end: number } | null>(null);

  // Refs, not captures: the mount effect outlives renders, and a drag
  // handler holding a stale onSetLoop wrote the wrong loop.
  const loopRef = useRef(loop);
  loopRef.current = loop;
  const onSetLoopRef = useRef(onSetLoop);
  onSetLoopRef.current = onSetLoop;
  const framesRef = useRef(frames);
  framesRef.current = frames;
  const peaksRef = useRef(peaks);
  peaksRef.current = peaks;
  const sustainRef = useRef(sustain);
  sustainRef.current = sustain;
  const zoomRef = useRef(zoom);
  zoomRef.current = zoom;
  const onGestureCommitRef = useRef(onGestureCommit);
  onGestureCommitRef.current = onGestureCommit;

  useEffect(() => {
    const container = containerRef.current;
    if (!container || !peaks || peaks.length === 0) return;
    let context: unknown = null;
    try {
      context = document.createElement("canvas").getContext("2d");
    } catch {
      context = null;
    }
    if (!context) {
      setFailed(true);
      return;
    }

    const normalised = new Float32Array(peaks.length);
    for (let i = 0; i < peaks.length; i++) {
      normalised[i] = (peaks[i] ?? 0) / 32768;
    }

    let ws: WaveSurfer;
    const regions = RegionsPlugin.create();
    try {
      ws = WaveSurfer.create({
        container,
        height: 110,
        waveColor: "#008b8b",
        progressColor: "#008b8b",
        cursorWidth: 0,
        interact: true,
        autoScroll: false,
        autoCenter: false,
        peaks: [normalised],
        normalize: true,
        duration: DURATION,
        plugins: [regions],
      });
    } catch {
      setFailed(true);
      return;
    }
    wsRef.current = ws;

    // wavesurfer's regions plugin decides marker-versus-region styling
    // once, in the element it builds: a region whose start equals its
    // end gets a bare left border and no resize handles, and setOptions
    // repairs neither when it is later widened. A freshly imported
    // voice's loop 1 does start equal to its end, so widening it left
    // an invisible loop with undraggable edges (R17). Hence a function
    // the loop effect can call again whenever the shape changes.
    const makeRegion = (start: number, end: number) => {
      const region = regions.addRegion({
        start,
        end,
        color: sustainRef.current ? SUSTAIN_FILL : LOOP_FILL,
        drag: true,
        resize: true,
      });
      regionRef.current = region;

      region.on("update", () => {
        if (!draggingRef.current) {
          draggingRef.current = true;
          onGestureBegin?.();
        }
        setLive({
          start: Math.round(region.start * framesRef.current),
          end: Math.round(region.end * framesRef.current),
        });
      });
      region.on("update-end", () => {
        draggingRef.current = false;
        setLive(null);
        const f = framesRef.current;
        const cur = loopRef.current;
        const p = peaksRef.current;
        const rawStart = Math.round(region.start * f);
        const rawEnd = Math.round(region.end * f);
        const startMoved = Math.abs(rawStart - cur.start) > 0;
        const endMoved = Math.abs(rawEnd - cur.end) > 0;
        const start2 = startMoved && p ? snapToZero(p, f, rawStart) : rawStart;
        const end2 = endMoved && p ? snapToZero(p, f, rawEnd) : rawEnd;
        onSetLoopRef.current(start2, end2);
        onGestureCommit?.();
      });
    };
    remakeRegionRef.current = makeRegion;

    // A region added before decode clamps to zero width permanently;
    // it must be created on ready.
    ws.once("ready", () => {
      makeRegion(
        loopRef.current.start / framesRef.current,
        loopRef.current.end / framesRef.current,
      );

      // The zoom effect below runs while the new instance is still
      // decoding, where a zoom call throws. Here the audio is decoded.
      if (zoomRef.current !== 1) {
        ws.zoom(zoomRef.current * (containerRef.current?.clientWidth ?? 600));
      }
    });

    ws.on("click", (relX) => {
      const f = framesRef.current;
      const p = peaksRef.current;
      const raw = Math.round(relX * f);
      const frame = p ? snapToZero(p, f, raw) : raw;
      const cur = loopRef.current;
      if (Math.abs(frame - cur.start) <= Math.abs(frame - cur.end)) {
        onSetLoopRef.current(Math.min(frame, cur.end - 1), cur.end);
      } else {
        onSetLoopRef.current(cur.start, Math.max(frame, cur.start + 1));
      }
    });

    return () => {
      // A drag that never released (a voice change or a peaks rebuild
      // mid-drag) leaves the core's gesture bracket open, folding every
      // later edit into it.
      if (draggingRef.current) {
        draggingRef.current = false;
        onGestureCommitRef.current?.();
      }
      ws.destroy();
      wsRef.current = null;
      regionRef.current = null;
      remakeRegionRef.current = null;
    };
    // Remounts per voice and per peak payload; loop moves apply to the
    // existing region below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voiceKey, peaks]);

  // Loop moves from outside (the numeric fields, undo) reposition the
  // region without a remount. Crossing between a zero-width loop and a
  // real one rebuilds it instead: see makeRegion above for why.
  useEffect(() => {
    if (draggingRef.current) return;
    const ws = wsRef.current;
    if (!ws || ws.getDuration() === 0) return;
    const region = regionRef.current;
    if (!region) return;
    const start = loop.start / frames;
    const end = loop.end / frames;
    if ((region.start === region.end) !== (start === end)) {
      region.remove();
      regionRef.current = null;
      remakeRegionRef.current?.(start, end);
      return;
    }
    region.setOptions({ start, end });
  }, [loop.start, loop.end, frames, loopIndex]);

  // The designation moves without the loop moving (another loop takes
  // it, or this one gains it), so the fill follows on its own. A new
  // colour through setOptions leaves the drawn region exactly as it
  // was, the same repaint gap widening hits, so this rebuilds at the
  // bounds the region already holds.
  useEffect(() => {
    if (draggingRef.current) return;
    const region = regionRef.current;
    if (!region) return;
    const { start, end } = region;
    region.remove();
    regionRef.current = null;
    remakeRegionRef.current?.(start, end);
  }, [sustain]);

  // Re-applied on remount as well as on change: a new peaks payload
  // rebuilds wavesurfer, and the strip would otherwise snap back to 1x
  // while the slider still read 8x.
  useEffect(() => {
    const ws = wsRef.current;
    // Zoom before decode throws "No audio loaded"; the default view
    // needs no zoom call anyway.
    if (!ws || ws.getDuration() === 0 || zoom === 1) return;
    ws.zoom(zoom * (containerRef.current?.clientWidth ?? 600));
  }, [zoom, voiceKey, peaks]);

  const shownStart = live?.start ?? loop.start;
  const shownEnd = live?.end ?? loop.end;

  // Without a canvas (jsdom) the strip disappears; the numeric loop
  // fields keep working, and the browser smoke covers the drawing.
  return (
    <div className="waveform-wrap">
      {!failed && <div ref={containerRef} className="waveform" data-testid="waveform" />}
      <div className="loopnums">
        <span className="loopname">Loop {loopIndex + 1}</span>
        {sustain && <span className="loopsustain">sustain · repeats while held</span>}
        <span>
          Start{" "}
          <NumberCell
            label="loop start frame"
            name="loop-start"
            value={shownStart}
            onCommit={(n) => {
              onSetLoop(clamp(n, 0, shownEnd - 1), shownEnd);
            }}
          />
        </span>
        <span>
          End{" "}
          <NumberCell
            label="loop end frame"
            name="loop-end"
            value={shownEnd}
            onCommit={(n) => {
              onSetLoop(shownStart, clamp(n, shownStart + 1, frames));
            }}
          />
        </span>
        <span className="loopmeta">
          {frames.toLocaleString("en-US")} frames · snap: zero crossing
        </span>
        <span className="loopzoom">
          Zoom {zoom}x{" "}
          <input
            type="range"
            min={1}
            max={32}
            step={1}
            value={zoom}
            aria-label="waveform zoom"
            name="waveform-zoom"
            onChange={(e) => {
              setZoom(Number(e.target.value));
            }}
          />
        </span>
      </div>
    </div>
  );
}
