// Capacity readout: MB and percent, always visible, marks the one
// disk to two disk boundary.

import { IMAGE_SIZE, formatBytes, usedBytes } from "../data/model";
import type { Disk } from "../data/model";

// alarm echoes a standing capacity error at the control itself; the
// status bar is the record, the bar carries the alarm.
export function CapacityBar({ disk, alarm = false }: { disk: Disk; alarm?: boolean }) {
  const used = usedBytes(disk);
  const pct = (used / IMAGE_SIZE) * 100;
  const twoDisk = used > IMAGE_SIZE;
  // Danger colour as the set approaches the hard two disk ceiling.
  const over = alarm || used > IMAGE_SIZE * 2 * 0.95;
  const cls = over ? "capacity over" : twoDisk || pct > 85 ? "capacity warn" : "capacity";
  return (
    <div className={cls} role="status" aria-label="disk capacity">
      <div className="bar">
        <div className="fill" style={{ width: `${Math.min(100, pct / (twoDisk ? 2 : 1))}%` }} />
        {twoDisk && <div className="tick" title="disk 1 to disk 2 boundary" />}
      </div>
      <span className="label">
        {formatBytes(used)} · {pct.toFixed(0)}%{twoDisk ? " · two disk set" : ""}
      </span>
    </div>
  );
}
