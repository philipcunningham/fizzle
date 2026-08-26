import { describe, expect, it } from "vitest";
import type { Core } from "../src/boundary/contract";
import { ok } from "../src/boundary/contract";
import { createCoreStub, emptySnapshot } from "../src/core/stub";

describe("programmable core stub", () => {
  it("reports an unstaged capability instead of emulating it", async () => {
    const result = await createCoreStub().newDisk("TEST");
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.code).toBe("unstaged-call");
    expect(result.error.message).toContain("newDisk");
  });

  it("uses the exact staged result", async () => {
    const expected = emptySnapshot({ revision: 7 });
    const result = await createCoreStub({
      snapshot: () => Promise.resolve(ok(expected)),
    }).snapshot();
    expect(result).toEqual(ok(expected));
  });

  it("is not thenable", async () => {
    const core = createCoreStub();
    expect(await Promise.resolve(core)).toBe(core);
  });

  it("keeps unstaged methods when spread", async () => {
    const copy: Core = { ...createCoreStub() };
    const result = await copy.newDisk("TEST");
    expect(result.ok).toBe(false);
    if (result.ok) return;
    expect(result.error.code).toBe("unstaged-call");
    expect(result.error.message).toContain("newDisk");
  });
});
