// The always-visible capacity readout (R23), fed from the core
// snapshot. Two ceilings a user can hit and they are not the same
// measurement: image bytes against a floppy, and the instrument's audio
// against the machine they declared (R29). Both count down what is
// left, so neither ever reads past 100 and full is always zero.
import { IMAGE_SIZE } from "../boundary/contract";
import { formatBytes } from "./format";

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
  /** Free percentages at or below which the reading warns and alarms. */
  warnAt: number;
  overAt: number;
  /** Drawn where disk 1 ends on a two disk set. */
  tick?: boolean;
}

/** One reading: the bar fills as the space goes, the figure counts down. */
function Meter({
  used,
  capacity,
  what,
  alarm = false,
  note = "",
  warnAt,
  overAt,
  tick = false,
}: MeterProps) {
  const spent = capacity > 0 ? (used / capacity) * 100 : 0;
  // Rounded once, so the colour and the figure can't disagree: a
  // hair above nothing used to read zero in amber.
  const free = Math.round(Math.max(0, 100 - spent));
  const cls =
    alarm || free <= overAt ? "capacity over" : free <= warnAt ? "capacity warn" : "capacity";
  return (
    // Named for a screen reader to seek out, but not a live region:
    // capacity moves on every edit, and the status bar already
    // announces what the user just did.
    <div className={cls} role="status" aria-live="off" aria-label={`${what} free`}>
      <div className="bar" aria-hidden="true">
        <div className="fill" style={{ width: `${String(Math.min(100, spent))}%` }} />
        {tick && <div className="tick" title="disk 1 to disk 2 boundary" />}
      </div>
      <span className="label">
        {formatBytes(used)} · {free}% {what} free{note}
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
      {/* The disk keeps the thresholds it has always had: amber on
          sight for a two disk set or a disk over 85 percent spent, and
          red before the last bytes go. */}
      <Meter
        used={usedBytes}
        capacity={capacity}
        what="disk"
        alarm={alarm}
        note={twoDisk ? " · two disk set" : ""}
        warnAt={twoDisk ? 100 : 15}
        overAt={5}
        tick={twoDisk}
      />
      {/* The memory has no spare: an instrument either loads or it
          doesn't, so red waits for the last of it. */}
      <Meter used={audioBytes} capacity={memoryBytes} what="memory" warnAt={15} overAt={0} />
    </div>
  );
}
