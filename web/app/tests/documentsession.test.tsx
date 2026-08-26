import { QueryClientProvider } from "@tanstack/react-query";
import { act, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ok } from "../src/boundary/contract";
import { createCoreStub, emptySnapshot } from "../src/core/stub";
import { createQueryClient } from "../src/queries/client";
import { useDocumentSession } from "../src/shell/useDocumentSession";

afterEach(() => {
  localStorage.clear();
});

describe("document session boot", () => {
  it("does not restart when inline callback identities change", async () => {
    localStorage.setItem("fizzle.sampleMemory", String(2 * 1024 * 1024));
    let declarations = 0;
    const core = createCoreStub({
      setSampleMemory: () => {
        declarations += 1;
        return Promise.resolve(ok(emptySnapshot({ revision: declarations })));
      },
    });
    const client = createQueryClient();

    function Probe() {
      useDocumentSession(
        core,
        () => undefined,
        () => undefined,
      );
      return null;
    }

    render(
      <QueryClientProvider client={client}>
        <Probe />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(declarations).toBe(1);
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 25));
    });
    expect(declarations).toBe(1);
  });
});
