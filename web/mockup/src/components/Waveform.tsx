// Waveform with loop handles: wavesurfer.js renders canned peaks;
// loop 1's start and end drag as a region, and a click moves the nearest
// loop edge. Zoom is a fit multiplier so the canvas never renders narrower
// than its container (a sub-fit zoom made fillParent fight the scrollbar
// and flicker). The numeric readout shows sample frames; in the real
// product those come back confirmed by the core.

import { useEffect, useMemo, useRef, useState } from "react";
import WaveSurfer from "wavesurfer.js";
import RegionsPlugin from "wavesurfer.js/plugins/regions";
import type { Voice } from "../data/model";
import { clamp } from "../data/model";
import { makePeaks, snapToZeroCrossing } from "../data/seed";

type Region = ReturnType<RegionsPlugin["addRegion"]>;

interface Props {
  voice: Voice;
  loopIndex: number;
  onLoopChange: (startFrame: number, endFrame: number) => void;
}

export function Waveform({ voice, loopIndex, onLoopChange }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const wsRef = useRef<WaveSurfer | null>(null);
  const regionRef = useRef<Region | null>(null);
  const draggingRef = useRef(false);
  const zoomApplied = useRef(false);
  const [zoomMult, setZoomMult] = useState(1);
  const [live, setLive] = useState<{ start: number; end: number } | null>(null);

  const sampleRate = voice.rate * 1000;
  const duration = voice.frames / sampleRate;
  const loop = voice.loops[loopIndex];

  // Latest loop values and callback for the wavesurfer event closures:
  // the mount effect runs once per voice, so a captured onLoopChange
  // would keep writing into whichever loop was active at mount.
  const loopRef = useRef({ start: loop.start, end: loop.end });
  loopRef.current = { start: loop.start, end: loop.end };
  const onLoopChangeRef = useRef(onLoopChange);
  onLoopChangeRef.current = onLoopChange;

  // Zoom anchors on the edge touched last; a fresh loop starts on start.
  const lastEdgeRef = useRef<"start" | "end">("start");
  useEffect(() => {
    lastEdgeRef.current = "start";
  }, [loopIndex, voice.id]);

  const peaks = useMemo(() => makePeaks(voice.peakSeed, 4096), [voice.peakSeed]);

  // Snap drags and clicks to the nearest sign change in the rendered
  // waveform; the confirmed frame comes back the way the core's would.
  const snap = (f: number) => snapToZeroCrossing(peaks, voice.frames, f);

  useEffect(() => {
    if (!containerRef.current) return;
    const regions = RegionsPlugin.create();
    const ws = WaveSurfer.create({
      container: containerRef.current,
      height: 110,
      waveColor: "#008b8b",
      progressColor: "#008b8b",
      cursorWidth: 0,
      interact: true,
      autoScroll: false,
      autoCenter: false,
      peaks: [peaks],
      duration,
      plugins: [regions],
    });
    wsRef.current = ws;

    // Regions added before decode clamp against a zero duration and
    // collapse into markers, so the loop region waits for 'ready'.
    ws.once("ready", () => {
      const region = regions.addRegion({
        start: loopRef.current.start / sampleRate,
        end: loopRef.current.end / sampleRate,
        color: "rgba(255, 176, 0, 0.35)",
        drag: true,
        resize: true,
      });
      regionRef.current = region;

      region.on("update", () => {
        draggingRef.current = true;
        setLive({ start: Math.round(region.start * sampleRate), end: Math.round(region.end * sampleRate) });
      });
      region.on("update-end", () => {
        draggingRef.current = false;
        setLive(null);
        // Snap only the edge that actually moved; re-snapping the other
        // edge surprises whoever placed it deliberately.
        const cur = loopRef.current;
        const rawStart = region.start * sampleRate;
        const rawEnd = region.end * sampleRate;
        const startMoved = Math.abs(rawStart - cur.start) > 0.5;
        const endMoved = Math.abs(rawEnd - cur.end) > 0.5;
        if (startMoved !== endMoved) lastEdgeRef.current = startMoved ? "start" : "end";
        const start = startMoved ? snap(rawStart) : cur.start;
        const end = endMoved ? snap(rawEnd) : cur.end;
        if (start !== cur.start || end !== cur.end) {
          onLoopChangeRef.current(clamp(start, 0, voice.frames - 2), clamp(end, start + 2, voice.frames - 1));
        }
      });
    });

    // Click to move the nearest loop edge to the clicked frame.
    ws.on("click", (relX) => {
      const frame = snap(relX * voice.frames);
      const cur = loopRef.current;
      if (Math.abs(frame - cur.start) <= Math.abs(frame - cur.end)) {
        lastEdgeRef.current = "start";
        onLoopChangeRef.current(Math.min(frame, cur.end - 2), cur.end);
      } else {
        lastEdgeRef.current = "end";
        onLoopChangeRef.current(cur.start, Math.max(frame, cur.start + 2));
      }
    });

    return () => {
      ws.destroy();
      wsRef.current = null;
      regionRef.current = null;
    };
    // Rebuild per voice and per rate: duration is derived from the rate,
    // so a rate edit needs a fresh decode or every frame conversion skews.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voice.id, voice.rate]);

  // Keep the region in step with the document (numeric entry, click
  // placement, loop selection, undo) without fighting an in-flight drag.
  // Skipped until wavesurfer has decoded: setOptions clamps against a zero
  // duration and would collapse the region into a marker at frame 0.
  useEffect(() => {
    if (draggingRef.current) return;
    const ws = wsRef.current;
    const el = containerRef.current;
    if (!ws || ws.getDuration() === 0) return;
    regionRef.current?.setOptions({ start: loop.start / sampleRate, end: loop.end / sampleRate });
    // When zoomed, keep the edited loop in view: numeric entry, click
    // placement, or undo can otherwise push both edges off screen. The
    // last touched edge takes priority.
    if (el && zoomApplied.current && zoomMult > 1) {
      const pxPerSec = (el.clientWidth / duration) * zoomMult;
      const winDur = el.clientWidth / pxPerSec;
      const winStart = ws.getScroll() / pxPerSec;
      const s0 = loop.start / sampleRate;
      const e0 = loop.end / sampleRate;
      const first = lastEdgeRef.current === "end" ? e0 : s0;
      const second = lastEdgeRef.current === "end" ? s0 : e0;
      if (first < winStart || first > winStart + winDur) {
        ws.setScrollTime(Math.max(0, first - winDur / 2));
      } else if (second < winStart || second > winStart + winDur) {
        ws.setScrollTime(Math.max(0, second - winDur / 2));
      }
    }
  }, [loop.start, loop.end, sampleRate, loopIndex, zoomMult, duration]);

  useEffect(() => {
    const el = containerRef.current;
    const ws = wsRef.current;
    if (!el || !ws) return;
    if (zoomMult === 1 && !zoomApplied.current) return;
    zoomApplied.current = true;
    const pxPerSec = (el.clientWidth / duration) * zoomMult;
    ws.zoom(pxPerSec);
    // Anchor the view on the loop edge touched last (start by default) so
    // zooming dives into the point being worked on. Deferred a frame: the
    // zoom re-render resets the scroll position.
    const anchored = lastEdgeRef.current === "end" ? loopRef.current.end : loopRef.current.start;
    const center = anchored / sampleRate;
    const id = window.setTimeout(() => {
      ws.setScrollTime(Math.max(0, center - el.clientWidth / pxPerSec / 2));
    }, 60);
    return () => window.clearTimeout(id);
  }, [zoomMult, duration, sampleRate]);

  const shownStart = live?.start ?? loop.start;
  const shownEnd = live?.end ?? loop.end;

  return (
    <div className="waveform-wrap">
      <div ref={containerRef} />
      <div className="loopnums">
        <span style={{ color: "var(--fz-accent-bright)" }}>Loop {loopIndex + 1}</span>
        <span>
          Start{" "}
          <input
            aria-label="loop start frame" name="loop-start"
            value={shownStart}
            onChange={(e) => {
              const n = Number(e.target.value);
              if (Number.isNaN(n)) return;
              lastEdgeRef.current = "start";
              onLoopChange(clamp(n, 0, shownEnd - 1), shownEnd);
            }}
          />
        </span>
        <span>
          End{" "}
          <input
            aria-label="loop end frame" name="loop-end"
            value={shownEnd}
            onChange={(e) => {
              const n = Number(e.target.value);
              if (Number.isNaN(n)) return;
              lastEdgeRef.current = "end";
              onLoopChange(shownStart, clamp(n, shownStart + 1, voice.frames - 1));
            }}
          />
        </span>
        <span style={{ color: "var(--fz-fg-faint)" }}>
          {voice.frames} frames at {voice.rate} kHz · snap: zero crossing
        </span>
        <span style={{ marginLeft: "auto" }}>
          Zoom {zoomMult}x{" "}
          <input
            type="range"
            min={1}
            max={16}
            step={1}
            value={zoomMult}
            aria-label="waveform zoom" name="waveform-zoom"
            onChange={(e) => setZoomMult(Number(e.target.value))}
          />
        </span>
      </div>
    </div>
  );
}
