// The query layer serves a worker-as-server: nothing refetches on its
// own, freshness is driven entirely by the revision token in the key.
import { describe, expect, it } from "vitest";
import { createQueryClient, queryKeys } from "../src/queries/client";

describe("query key factory", () => {
  it("keys snapshots by revision so a new revision is a new key", () => {
    expect(queryKeys.snapshot(1)).not.toEqual(queryKeys.snapshot(2));
    expect(queryKeys.snapshot(3)).toEqual(queryKeys.snapshot(3));
  });

  it("namespaces keys so different resources never collide", () => {
    const keys = [queryKeys.snapshot(1), queryKeys.schema(), queryKeys.peaks(1, "v1")];
    const flat = keys.map((k) => JSON.stringify(k));
    expect(new Set(flat).size).toBe(keys.length);
  });

  it("keys peaks by revision and voice", () => {
    expect(queryKeys.peaks(1, "v1")).not.toEqual(queryKeys.peaks(1, "v2"));
    expect(queryKeys.peaks(1, "v1")).not.toEqual(queryKeys.peaks(2, "v1"));
  });
});

describe("query client defaults", () => {
  const defaults = createQueryClient().getDefaultOptions().queries;

  it("never refetches automatically", () => {
    expect(defaults?.refetchOnWindowFocus).toBe(false);
    expect(defaults?.refetchOnReconnect).toBe(false);
    expect(defaults?.refetchOnMount).toBe(false);
  });

  it("treats results as fresh forever; the revision key invalidates", () => {
    expect(defaults?.staleTime).toBe(Infinity);
  });

  it("does not retry; the core is local and deterministic", () => {
    expect(defaults?.retry).toBe(false);
  });
});
