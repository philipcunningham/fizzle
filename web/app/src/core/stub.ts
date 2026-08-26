import type { Core, Snapshot } from "../boundary/contract";
import { err, ok } from "../boundary/contract";

export function emptySnapshot(overrides: Partial<Snapshot> = {}): Snapshot {
  return { revision: 0, disk: null, canUndo: false, canRedo: false, ...overrides };
}

/**
 * A programmable boundary stub for component tests. Unstaged calls return a
 * visible error instead of emulating disk, instrument, or capacity rules.
 */
export function createCoreStub(overrides: Partial<Core> = {}): Core {
  const staged: Partial<Core> = {
    snapshot: () => Promise.resolve(ok(emptySnapshot())),
    schema: () => Promise.resolve(ok([])),
    setDebug: () => Promise.resolve(ok(null)),
    ...overrides,
  };
  return new Proxy(staged, {
    get(target, property): unknown {
      const value = target[property as keyof Core];
      if (value !== undefined || typeof property !== "string") return value;
      return () =>
        Promise.resolve(err("unstaged-call", `test did not stage the core method ${property}`));
    },
  }) as Core;
}
