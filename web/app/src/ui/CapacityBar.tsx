// The always-visible capacity readout (R23), fed from the core
// snapshot: used bytes, whole-document capacity, and whether the
// document already spans two disks.
import { IMAGE_SIZE } from "../boundary/contract";
import { formatBytes } from "./format";

interface Props {
  usedBytes: number;
  disks: number;
  alarm?: boolean;
}

export function CapacityBar({ usedBytes, disks, alarm = false }: Props) {
  const twoDisk = disks === 2;
  // The percentage is of what the document actually holds. A two disk
  // set is two images, so 1.7 MB across the pair reads 69 percent, not
  // the 138 percent a one disk denominator would claim.
  const capacity = IMAGE_SIZE * (twoDisk ? 2 : 1);
  const pct = (usedBytes / capacity) * 100;
  const over = alarm || pct > 95;
  const cls = over ? "capacity over" : twoDisk || pct > 85 ? "capacity warn" : "capacity";
  return (
    <div className={cls} role="status" aria-label="disk capacity">
      <div className="bar">
        <div className="fill" style={{ width: `${String(Math.min(100, pct))}%` }} />
        {twoDisk && <div className="tick" title="disk 1 to disk 2 boundary" />}
      </div>
      <span className="label">
        {formatBytes(usedBytes)} · {pct.toFixed(0)}%{twoDisk ? " · two disk set" : ""}
      </span>
    </div>
  );
}
