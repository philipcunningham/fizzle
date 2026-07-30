// A minimal RIFF scan for the channel count, so the import prompt can
// hide the stereo choice for mono files. Presentational only: the core
// parses the WAV for real and enforces its own rules.
export function wavChannels(bytes: Uint8Array): number | null {
  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const tag = (at: number) =>
    bytes.length >= at + 4 ? String.fromCharCode(...bytes.subarray(at, at + 4)) : "";
  if (tag(0) !== "RIFF" || tag(8) !== "WAVE") return null;
  let at = 12;
  while (at + 8 <= bytes.length) {
    const size = dv.getUint32(at + 4, true);
    if (tag(at) === "fmt ") {
      if (at + 12 > bytes.length) return null;
      const channels = dv.getUint16(at + 10, true);
      return channels > 0 ? channels : null;
    }
    // Chunks are word aligned.
    at += 8 + size + (size % 2);
  }
  return null;
}
