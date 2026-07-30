// The empty state behind the editor tabs when the disk carries no
// full dump: one obvious way forward (R4).
export function NoInstrumentPanel({ onNewInstrument }: { onNewInstrument: () => void }) {
  return (
    <div className="centered">
      <div className="panel">
        <h2>No instrument on this disk</h2>
        <p className="hint">
          An instrument is the disk&apos;s full dump: banks, areas, and voices the sampler loads in
          one go. Import WAVs, an SFZ, or FZ files to create one, or start empty.
        </p>
        <button className="btn primary" onClick={onNewInstrument}>
          New empty instrument
        </button>
      </div>
    </div>
  );
}
