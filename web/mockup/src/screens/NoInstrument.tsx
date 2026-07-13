// Actionable empty state for tabs that need an open instrument: offers
// the disk's openable full dumps and a new instrument, instead of a
// stranded instruction.

import { useStore } from "../state/store";

export function NoInstrument() {
  const { state, dispatch } = useStore();
  const fulls = state.doc.disk?.files.filter((f) => f.instrumentId) ?? [];
  return (
    <div className="empty" style={{ flexDirection: "column", gap: 14, minHeight: "100%" }}>
      <p style={{ margin: 0 }}>No instrument is open.</p>
      <div className="row" style={{ justifyContent: "center" }}>
        {fulls.map((f) => (
          <button
            key={f.id}
            className="btn primary"
            onClick={() => f.instrumentId && dispatch({ type: "open-instrument", id: f.instrumentId })}
          >
            Open {(f.instrumentId && state.doc.instruments[f.instrumentId]?.name) || f.name}
          </button>
        ))}
        {/* One full dump per disk: creating only offers when none
            exists, matching the sidebar. */}
        {fulls.length === 0 && (
          <button className="btn" onClick={() => dispatch({ type: "new-instrument" })}>
            New empty instrument
          </button>
        )}
      </div>
    </div>
  );
}
