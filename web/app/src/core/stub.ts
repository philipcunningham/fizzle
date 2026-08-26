import type { Core, Snapshot } from "../boundary/contract";
import { err, ok } from "../boundary/contract";
import { coreMethods } from "../boundary/protocol.generated";

export function emptySnapshot(overrides: Partial<Snapshot> = {}): Snapshot {
  return { revision: 0, disk: null, canUndo: false, canRedo: false, ...overrides };
}

/**
 * A programmable boundary stub for component tests. Unstaged calls return a
 * visible error instead of emulating disk, instrument, or capacity rules. All
 * methods are own enumerable properties, so object spread preserves the
 * fallback. It has no `then` property, so it is not a thenable.
 */
export function createCoreStub(overrides: Partial<Core> = {}): Core {
  const unstaged = Object.fromEntries(
    coreMethods.map((method) => [
      method,
      () => Promise.resolve(err("unstaged-call", `test did not stage the core method ${method}`)),
    ]),
  ) as unknown as Core;
  const staged: Core = {
    ...unstaged,
    snapshot: () => Promise.resolve(ok(emptySnapshot())),
    schema: () => Promise.resolve(ok([])),
    setDebug: () => Promise.resolve(ok(null)),
    ...overrides,
  };
  return staged;
}
