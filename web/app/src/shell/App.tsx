import { QueryClientProvider } from "@tanstack/react-query";
import { useMemo } from "react";
import type { Core } from "../boundary/contract";
import { createQueryClient } from "../queries/client";
import { EditorShell } from "./EditorShell";
import { ErrorBoundary } from "./ErrorBoundary";
import { exportLastResort } from "./fileio";

/** Application composition root. Workflows and document state live below it. */
export function App({ core }: { core: Core }) {
  const client = useMemo(() => createQueryClient(), []);
  return (
    <QueryClientProvider client={client}>
      <ErrorBoundary
        onExport={() => {
          exportLastResort(core);
        }}
      >
        <EditorShell core={core} />
      </ErrorBoundary>
    </QueryClientProvider>
  );
}
