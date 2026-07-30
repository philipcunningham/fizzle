// A rendering failure is contained here (E5): the boundary offers
// recovery, and, while the core still responds, a last resort export
// of the current document. A crash never silently eats the session.
// The panel itself is shared, because a core that has stopped
// answering needs the same surface with different wording.
import { Component } from "react";
import type { ReactNode } from "react";

/**
 * What a crash looks like (E5): a plain sentence about what happened,
 * the technical reason folded away, and the ways on as children. The
 * display failing and the core failing differ in wording and in what
 * can still be offered, so the caller supplies both.
 */
export function CrashPanel({
  label,
  title,
  message,
  detail,
  children,
}: {
  /** The alert's accessible name: which of the two failures this is. */
  label: string;
  title: string;
  message: string;
  /** The technical reason, for a bug report rather than for reading. */
  detail?: string | undefined;
  children: ReactNode;
}) {
  return (
    <div className="crash" role="alert" aria-label={label}>
      <h2>{title}</h2>
      <p>{message}</p>
      {detail !== undefined && detail !== "" && (
        <details>
          <summary>Technical detail</summary>
          <p>{detail}</p>
        </details>
      )}
      <div className="crashactions">{children}</div>
    </div>
  );
}

interface ErrorBoundaryProps {
  /** Last resort export; wired to the core's image export. */
  onExport: () => void;
  children: ReactNode;
}

interface ErrorBoundaryState {
  failed: boolean;
  message: string;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { failed: false, message: "" };
  }

  static getDerivedStateFromError(error: unknown): ErrorBoundaryState {
    return { failed: true, message: error instanceof Error ? error.message : String(error) };
  }

  override render() {
    if (!this.state.failed) return this.props.children;
    return (
      <CrashPanel
        label="rendering failure"
        title="The display failed"
        message={`Something in the interface crashed: ${this.state.message}. The document itself lives in the core and is usually intact.`}
      >
        {/* Called with no arguments: bound straight to onClick, the
            click event would arrive as the export's continuation and
            the export would then try to call it. */}
        <button
          onClick={() => {
            this.props.onExport();
          }}
        >
          Export current document
        </button>
        <button
          onClick={() => {
            window.location.reload();
          }}
        >
          Reload
        </button>
        <button
          onClick={() => {
            this.setState({ failed: false, message: "" });
          }}
        >
          Try to continue
        </button>
      </CrashPanel>
    );
  }
}
