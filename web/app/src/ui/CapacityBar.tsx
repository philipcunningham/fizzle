// The always-visible capacity readout (R23), fed from the core
// snapshot. Two ceilings a user can hit and they are not the same
// measurement: image bytes against a floppy, and the instrument's audio
// against the machine they declared (R29). Both count down what is
// left, so neither ever reads past 100 and full is always zero.
import { IMAGE_SIZE } from "../boundary/contract";

/**
 * What an FZ can hold. The FZ-1 shipped with 1 MB and reaches 2 MB with
 * Casio's expansion card; the rack units shipped with 2 MB. Nothing
 * holds more: only five bits of the wave memory bank register reach
 * memory, which is 32 banks of 64 KB.
 */
export const MEMORY_CHOICES = [
  { bytes: 1024 * 1024, label: "1 MB" },
  { bytes: 2 * 1024 * 1024, label: "2 MB" },
];

interface Props {
  usedBytes: number;
  disks: number;
  alarm?: boolean;
  /** What the instrument asks the sampler's memory to hold. */
  audioBytes: number;
  /** What the user says their sampler has. */
  memoryBytes: number;
}

interface MeterProps {
  used: number;
  capacity: number;
  /** The noun in the label and the accessible name. */
  what: string;
  alarm?: boolean;
  note?: string;
}

/** One reading: the bar fills as the space goes, the figure counts down. */
function Meter({ used, capacity, what, alarm = false, note = "" }: MeterProps) {
  const spent = capacity > 0 ? (used / capacity) * 100 : 0;
  const free = Math.max(0, 100 - spent);
  const cls = alarm || free === 0 ? "capacity over" : free < 15 ? "capacity warn" : "capacity";
  return (
    <div className={cls} role="status" aria-label={`${what} free`}>
      <div className="bar">
        <div className="fill" style={{ width: `${String(Math.min(100, spent))}%` }} />
      </div>
      <span className="label">
        {free.toFixed(0)}% {what} free{note}
      </span>
    </div>
  );
}

export function CapacityBar({ usedBytes, disks, alarm = false, audioBytes, memoryBytes }: Props) {
  const twoDisk = disks === 2;
  // The percentage is of what the document actually holds. A two disk
  // set is two images, so 1.7 MB across the pair reads 69 percent, not
  // the 138 percent a one disk denominator would claim.
  const capacity = IMAGE_SIZE * (twoDisk ? 2 : 1);
  return (
    <div className="capacitystack">
      <Meter
        used={usedBytes}
        capacity={capacity}
        what="disk"
        alarm={alarm}
        note={twoDisk ? " · two disk set" : ""}
      />
      <Meter used={audioBytes} capacity={memoryBytes} what="memory" />
    </div>
  );
}
