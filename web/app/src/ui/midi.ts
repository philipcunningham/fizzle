// Web MIDI (R22): a controller works where present, and absence
// degrades silently. Note on and off from any input reach the same
// handlers as the on-screen keyboard.
export interface MIDIHandlers {
  onNoteOn: (note: number, velocity: number) => void;
  onNoteOff: (note: number) => void;
}

interface MIDIInputLike {
  onmidimessage: ((event: { data: Uint8Array }) => void) | null;
}

interface MIDIAccessLike {
  inputs: { values(): Iterable<MIDIInputLike> };
  onstatechange: (() => void) | null;
}

/**
 * Subscribes to every MIDI input. Returns a cleanup function. When
 * Web MIDI is absent or permission fails, nothing happens and nothing
 * throws.
 */
export function subscribeMIDI(
  handlers: MIDIHandlers,
  request?: () => Promise<MIDIAccessLike>,
): () => void {
  const requester =
    request ??
    (
      navigator as unknown as { requestMIDIAccess?: () => Promise<MIDIAccessLike> }
    ).requestMIDIAccess?.bind(navigator);
  if (!requester) return () => undefined;

  let cancelled = false;
  const attached: MIDIInputLike[] = [];

  const attach = (access: MIDIAccessLike) => {
    for (const input of access.inputs.values()) {
      input.onmidimessage = (event) => {
        const [status = 0, note = 0, velocity = 0] = event.data;
        const kind = status & 0xf0;
        if (kind === 0x90 && velocity > 0) handlers.onNoteOn(note, velocity);
        else if (kind === 0x80 || (kind === 0x90 && velocity === 0)) handlers.onNoteOff(note);
      };
      attached.push(input);
    }
  };

  requester().then(
    (access) => {
      if (cancelled) return;
      attach(access);
      access.onstatechange = () => {
        if (!cancelled) attach(access);
      };
    },
    () => undefined, // permission denied degrades silently
  );

  return () => {
    cancelled = true;
    for (const input of attached) input.onmidimessage = null;
  };
}
