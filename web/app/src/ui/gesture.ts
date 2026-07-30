// The gesture bracket every drag control keeps, shared so the ways a
// gesture ends are handled the same in each.
//
// Section 7 of the spec asks a continuous gesture to land in history as
// exactly one undoable step, and R24 repeats it. That makes the bracket
// a pairing problem: open once, close once, however the gesture ends.
// Both halves are idempotent, because more than one ending arrives. A
// release under capture fires pointerup and then lostpointercapture.
import { useEffect, useRef } from "react";

export interface GestureBracket {
  /** Opens the bracket. A call while one is open does nothing. */
  begin: () => void;
  /** Closes an open bracket. A call with none open does nothing. */
  commit: () => void;
}

/**
 * Tracks one control's bracket and closes it if the control leaves the
 * document mid-gesture. React's onLostPointerCapture doesn't fire when
 * the node is removed. An editor that unmounts under a held pointer
 * therefore leaves the core's bracket open. Every later edit then
 * coalesces into it, and the Undo button stays disabled while the work
 * piles up. An undo that pops the import does exactly that.
 */
export function useGestureBracket(onBegin?: () => void, onCommit?: () => void): GestureBracket {
  const open = useRef(false);
  // Refs, not captures: the unmount effect runs once, and the handler
  // it holds must be the one the last render was given.
  const beginRef = useRef(onBegin);
  beginRef.current = onBegin;
  const commitRef = useRef(onCommit);
  commitRef.current = onCommit;

  useEffect(
    () => () => {
      if (!open.current) return;
      open.current = false;
      commitRef.current?.();
    },
    [],
  );

  return {
    begin: () => {
      if (open.current) return;
      open.current = true;
      beginRef.current?.();
    },
    commit: () => {
      if (!open.current) return;
      open.current = false;
      commitRef.current?.();
    },
  };
}
