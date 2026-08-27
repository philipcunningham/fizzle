import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import type { Core, CoreError, CoreResult, Snapshot } from "../boundary/contract";
import { isCoreCrash } from "../boundary/contract";
import { queryKeys } from "../queries/client";
import { MEMORY_CHOICES } from "../ui/CapacityBar";

const MEMORY_KEY = "fizzle.sampleMemory";

/**
 * Owns the application-level document lifecycle around the core: snapshot
 * revision, dirty state, history gestures, fatal errors, and sampler memory.
 * Screens receive operations from this hook and never coordinate the worker.
 */
export function useDocumentSession(
  core: Core,
  onFailure: (message: string) => void,
  onSuccess: () => void,
) {
  const [revision, setRevision] = useState(0);
  const [dirty, setDirty] = useState(false);
  const [fatal, setFatal] = useState<CoreError | null>(null);
  const [memoryBytes, setMemoryBytes] = useState(MEMORY_CHOICES[0]?.bytes ?? 1024 * 1024);
  const queryClient = useQueryClient();
  // Shell messages are render callbacks, not session dependencies. Keeping the
  // latest callbacks in refs preserves boot-once behavior for inline functions.
  const onFailureRef = useRef(onFailure);
  const onSuccessRef = useRef(onSuccess);
  // Session callbacks stay current without making the boot effect repeat.
  // eslint-disable-next-line react-hooks/refs
  onFailureRef.current = onFailure;
  // Session callbacks stay current without making the boot effect repeat.
  // eslint-disable-next-line react-hooks/refs
  onSuccessRef.current = onSuccess;

  useEffect(() => {
    if (new URLSearchParams(window.location.search).get("debug") === "1") {
      void core.setDebug(true);
    }
  }, [core]);

  const snapshot = useQuery({
    queryKey: queryKeys.snapshot(revision),
    queryFn: () => core.snapshot(),
    placeholderData: keepPreviousData,
  });
  const schemaQuery = useQuery({ queryKey: queryKeys.schema(), queryFn: () => core.schema() });

  const report = useCallback((error: CoreError) => {
    if (isCoreCrash(error)) setFatal(error);
    else onFailureRef.current(error.message);
  }, []);

  const apply = useCallback(
    (result: CoreResult<Snapshot>) => {
      if (result.ok) {
        onSuccessRef.current();
        setRevision(result.value.revision);
      } else report(result.error);
      return result.ok;
    },
    [report],
  );

  const applyEdit = useCallback(
    (result: CoreResult<Snapshot>) => {
      if (apply(result)) setDirty(true);
      return result.ok;
    },
    [apply],
  );

  const setMemory = useCallback(
    (bytes: number) => {
      // Sampler memory is a machine fact, not a document edit. It outlives the
      // session in local storage; a cookie would ride every asset request.
      // Storage refusal only means the choice is not remembered.
      setMemoryBytes(bytes);
      try {
        localStorage.setItem(MEMORY_KEY, String(bytes));
      } catch {
        // A locked down profile just means the choice is not remembered.
      }
      void core.setSampleMemory(bytes).then((result) => {
        if (apply(result)) void queryClient.invalidateQueries({ queryKey: ["snapshot"] });
      });
    },
    [apply, core, queryClient],
  );

  useEffect(() => {
    let saved = 0;
    try {
      saved = Number(localStorage.getItem(MEMORY_KEY) ?? 0);
    } catch {
      saved = 0;
    }
    if (MEMORY_CHOICES.some((choice) => choice.bytes === saved)) {
      // Boot adopts persisted sampler memory into the session once.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setMemory(saved);
    }
  }, [setMemory]);

  useEffect(() => {
    if (!dirty) return;
    // Chromium acts on preventDefault alone and supplies its own wording; the
    // legacy returnValue string is deprecated.
    const onUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    window.addEventListener("beforeunload", onUnload);
    return () => {
      window.removeEventListener("beforeunload", onUnload);
    };
  }, [dirty]);

  const undo = () => {
    void core.undo().then(applyEdit);
  };
  const redo = () => {
    void core.redo().then(applyEdit);
  };
  const gestureBegin = () => {
    void core.beginGesture();
  };
  const gestureCommit = () => {
    void core.commitGesture().then((result) => {
      // A press and release with no intervening edit lands no history entry,
      // so it must not raise the unsaved dot.
      if (result.ok && result.value.gestureLanded === false) apply(result);
      else applyEdit(result);
    });
  };

  return {
    snapshot: snapshot.data?.ok ? snapshot.data.value : null,
    schema: schemaQuery.data?.ok ? schemaQuery.data.value : [],
    queryClient,
    dirty,
    setDirty,
    fatal,
    memoryBytes,
    setMemory,
    report,
    apply,
    applyEdit,
    undo,
    redo,
    gestureBegin,
    gestureCommit,
  };
}
