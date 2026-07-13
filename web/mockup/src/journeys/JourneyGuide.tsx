// Docked walkthrough panel: lists the current journey's steps, ticking
// them as the matching actions happen. Steps without a matcher (hardware
// checks) offer a manual tick.

import { useEffect, useRef } from "react";
import { journeyById } from "../data/journeys";
import { useStore } from "../state/store";

export function JourneyGuide() {
  const { state, dispatch } = useStore();
  const journey = journeyById(state.journeyId);
  const seenCount = useRef(state.actionCount);

  const step = journey?.steps[state.journeyStep];

  useEffect(() => {
    if (!journey || !step?.match) return;
    if (state.actionCount === seenCount.current) return;
    seenCount.current = state.actionCount;
    if (state.lastActionFull && step.match(state.lastActionFull, state)) {
      dispatch({ type: "advance-journey" });
    }
  }, [state, journey, step, dispatch]);

  if (!journey) return null;

  return (
    <aside className="journey" aria-label="journey walkthrough">
      <h2 style={{ padding: "8px 10px 0", margin: 0, fontSize: 12, color: "var(--fz-accent-bright)" }}>{journey.title}</h2>
      <p style={{ padding: "0 10px", color: "var(--fz-fg-faint)", fontSize: 11 }}>{journey.intro}</p>
      <div className="steps">
        {journey.steps.map((st, i) => {
          const cls = i < state.journeyStep ? "step done" : i === state.journeyStep ? "step current" : "step";
          return (
            <div key={i} className={cls}>
              <span className="tick">{i < state.journeyStep ? "x" : i === state.journeyStep ? ">" : "·"}</span>
              <span style={{ flex: 1 }}>{st.text}</span>
              {i === state.journeyStep && (
                <button className="btn small" onClick={() => dispatch({ type: "advance-journey" })} aria-label="mark step done">
                  tick
                </button>
              )}
            </div>
          );
        })}
        {state.journeyStep >= journey.steps.length && (
          <p style={{ color: "var(--fz-ok)" }}>Journey complete. Note your keep / change / broken feedback in the log.</p>
        )}
      </div>
      <div className="foot">
        <button className="btn" onClick={() => dispatch({ type: "end-journey" })}>
          close walkthrough
        </button>
      </div>
    </aside>
  );
}
