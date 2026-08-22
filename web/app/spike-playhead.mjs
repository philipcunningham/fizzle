// Throwaway spike: the audition playhead, end to end. The sample is
// LOOPDEMO's, the loop rules are the app's, and the cursor is
// wavesurfer's own, driven by the modelled position.
import WaveSurfer from "wavesurfer.js";
import RegionsPlugin from "wavesurfer.js/dist/plugins/regions.js";

const RATE = 18000;
const TICK = 3600;
const REGION = 18000;
const LOW = [TICK, TICK + REGION];
const MID = [LOW[1], LOW[1] + REGION];
const HIGH = [MID[1], MID[1] + REGION];
const FRAMES = HIGH[1] + 3600;

// LOOPDEMO's sample: a tick, a low sine, a mid sawtooth, a high pulsing
// tone, and a falling tail.
const pcm = new Float32Array(FRAMES);
for (let i = 0; i < TICK; i++) {
  pcm[i] = 0.9 * Math.exp(-i / (0.025 * RATE)) * Math.sin((2 * Math.PI * 1800 * i) / RATE);
}
for (let i = 0; i < REGION; i++) {
  pcm[LOW[0] + i] = 0.85 * Math.sin((2 * Math.PI * 180 * i) / RATE);
  let saw = 0;
  for (let h = 1; h <= 12; h++) saw += Math.sin((2 * Math.PI * 360 * h * i) / RATE) / h;
  pcm[MID[0] + i] = (0.55 * saw) / 1.6;
  const pulse = RATE / 6;
  const on = pulse / 2;
  const edge = 120;
  const phase = i % pulse;
  let gate = 0;
  if (phase < edge) gate = 0.5 - 0.5 * Math.cos((Math.PI * phase) / edge);
  else if (phase < on - edge) gate = 1;
  else if (phase < on) gate = 0.5 + 0.5 * Math.cos((Math.PI * (phase - (on - edge))) / edge);
  pcm[HIGH[0] + i] = 0.9 * gate * Math.sin((2 * Math.PI * 720 * i) / RATE);
}
let sweep = 0;
for (let i = 0; i < 3600; i++) {
  const frac = i / 3600;
  sweep += (2 * Math.PI * (720 - 540 * frac)) / RATE;
  pcm[HIGH[1] + i] = 0.7 * (1 - frac) * Math.sin(sweep);
}

const peaks = new Float32Array(2048);
const bucket = Math.floor(FRAMES / peaks.length);
for (let i = 0; i < peaks.length; i++) {
  let peak = 0;
  for (let j = 0; j < bucket; j++) peak = Math.max(peak, Math.abs(pcm[i * bucket + j] ?? 0));
  peaks[i] = peak;
}

const regions = RegionsPlugin.create();
const ws = WaveSurfer.create({
  container: document.getElementById("strip"),
  height: 120,
  waveColor: "#008b8b",
  progressColor: "#008b8b",
  cursorWidth: 0,
  cursorColor: "#f0883e",
  interact: false,
  autoScroll: false,
  autoCenter: false,
  peaks: [peaks],
  normalize: true,
  duration: 1,
  plugins: [regions],
});

ws.once("ready", () => {
  regions.addRegion({
    start: LOW[0] / FRAMES,
    end: LOW[1] / FRAMES,
    color: "rgba(51, 209, 122, 0.35)",
    drag: false,
    resize: false,
  });
  regions.addRegion({
    start: HIGH[0] / FRAMES,
    end: HIGH[1] / FRAMES,
    color: "rgba(122, 162, 255, 0.35)",
    drag: false,
    resize: false,
  });
});

// The position model. A window repeats: linear until its end, then the
// modulo folds back into it. The key coming up swaps the window, which
// is a trace forward or a wrap backward, exactly as Chrome does it.
const fold = (frame, [start, end]) =>
  frame < end ? frame : start + ((frame - start) % (end - start));

let note = null;

const frameAt = (now) => {
  if (!note) return null;
  const heard = now - (ctx.outputLatency || 0);
  if (note.releasedAt === null) {
    return fold((heard - note.startedAt) * RATE, LOW);
  }
  const atRelease = fold((note.releasedAt - note.startedAt) * RATE, LOW);
  return fold(atRelease + (heard - note.releasedAt) * RATE, HIGH);
};

const ctx = new AudioContext();
const buffer = ctx.createBuffer(1, FRAMES, RATE);
buffer.getChannelData(0).set(pcm);

const read = document.getElementById("read");
const hold = document.getElementById("hold");

const tick = () => {
  if (!note) return;
  const frame = frameAt(ctx.currentTime);
  ws.setTime(Math.max(0, Math.min(FRAMES - 1, frame)) / FRAMES);
  const inWindow = note.releasedAt === null ? LOW : HIGH;
  read.textContent =
    `frame ${String(Math.round(frame)).padStart(6)}  of ${FRAMES}\n` +
    `window ${inWindow[0]} to ${inWindow[1]}   ${note.releasedAt === null ? "held" : "released"}\n` +
    `output latency ${Math.round((ctx.outputLatency || 0) * 1000)} ms`;
  requestAnimationFrame(tick);
};

const start = async () => {
  if (ctx.state === "suspended") await ctx.resume();
  if (note) return;
  const source = ctx.createBufferSource();
  source.buffer = buffer;
  source.loopStart = LOW[0] / RATE;
  source.loopEnd = LOW[1] / RATE;
  source.loop = true;
  const gain = ctx.createGain();
  gain.gain.value = 0.25;
  source.connect(gain);
  gain.connect(ctx.destination);
  source.start();
  note = { source, gain, startedAt: ctx.currentTime, releasedAt: null };
  ws.setOptions({ cursorWidth: 2 });
  requestAnimationFrame(tick);
};

const release = () => {
  if (!note || note.releasedAt !== null) return;
  const at = ctx.currentTime;
  note.source.loopStart = HIGH[0] / RATE;
  note.source.loopEnd = HIGH[1] / RATE;
  note.releasedAt = at;
  // A tail long enough to hear the end loop repeat, as LOOPDEMO has.
  note.gain.gain.setValueAtTime(0.25, at);
  note.gain.gain.linearRampToValueAtTime(0, at + 2.5);
  note.source.stop(at + 2.6);
  const ending = note;
  setTimeout(() => {
    if (note !== ending) return;
    note = null;
    ws.setOptions({ cursorWidth: 0 });
  }, 2700);
};

hold.addEventListener("pointerdown", start);
hold.addEventListener("pointerup", release);
hold.addEventListener("pointerleave", release);
window.__spike = { start, release, frameAt, ws, LOW, HIGH, FRAMES };
