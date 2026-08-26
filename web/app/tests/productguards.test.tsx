import { QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Core, Snapshot } from "../src/boundary/contract";
import { CORE_CRASH_MESSAGE, CORE_UNAVAILABLE, coreCrash, ok } from "../src/boundary/contract";
import { createCoreStub, emptySnapshot } from "../src/core/stub";
import { createQueryClient } from "../src/queries/client";
import { App } from "../src/shell/App";
import { useDocumentSession } from "../src/shell/useDocumentSession";
import { instrumentSnapshot, presentationCore } from "./support/presentation";

const MEMORY_KEY = "fizzle.sampleMemory";

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

function closeTab(): boolean {
  const event = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
}

function sessionHarness(core: Core) {
  const client = createQueryClient();
  const wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return renderHook(
    () =>
      useDocumentSession(
        core,
        () => undefined,
        () => undefined,
      ),
    {
      wrapper,
    },
  );
}

describe("fatal core failures", () => {
  it("shows a recovery panel and never exposes the machine code", async () => {
    const core = createCoreStub({
      newDisk: () => Promise.resolve(coreCrash<Snapshot>("worker exited")),
    });
    render(<App core={core} />);
    fireEvent.click(await screen.findByRole("button", { name: "New disk" }));
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    const panel = await screen.findByRole("alert", { name: "core failure" });
    expect(panel.textContent).toContain(CORE_CRASH_MESSAGE);
    expect(panel.textContent).not.toContain(CORE_UNAVAILABLE);
    expect(screen.getByRole("button", { name: "Reload" })).toBeTruthy();
  });
});

describe("unexported changes guard the tab", () => {
  it("is quiet while clean and prevents navigation after an edit", async () => {
    const { result } = sessionHarness(createCoreStub());
    expect(closeTab()).toBe(false);
    act(() => {
      result.current.applyEdit(ok(emptySnapshot({ revision: 1 })));
    });
    await waitFor(() => {
      expect(result.current.dirty).toBe(true);
    });
    expect(closeTab()).toBe(true);
  });

  it("becomes dirty again when undo changes a just-exported document", async () => {
    const core = createCoreStub({
      undo: () => Promise.resolve(ok(emptySnapshot({ revision: 2 }))),
    });
    const { result } = sessionHarness(core);
    act(() => {
      result.current.setDirty(false);
      result.current.undo();
    });
    await waitFor(() => {
      expect(result.current.dirty).toBe(true);
    });
    expect(closeTab()).toBe(true);
  });
});

describe("sampler-memory persistence", () => {
  it("ignores a remembered size no supported FZ holds", async () => {
    localStorage.setItem(MEMORY_KEY, String(4 * 1024 * 1024));
    const declarations: number[] = [];
    const core = createCoreStub({
      setSampleMemory: (bytes) => {
        declarations.push(bytes);
        return Promise.resolve(ok(emptySnapshot()));
      },
    });
    sessionHarness(core);
    await act(async () => Promise.resolve());
    expect(declarations).not.toContain(4 * 1024 * 1024);
  });
});

describe("dialog focus return", () => {
  it("returns focus to the control that opened the dialog", async () => {
    render(<App core={presentationCore()} />);
    const trigger = await screen.findByRole("button", { name: "export KICK" });
    trigger.focus();
    fireEvent.click(trigger);
    await screen.findByText("Export voice");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(document.activeElement).toBe(trigger);
    });
  });
});

describe("open-file accessibility state", () => {
  it("marks exactly one file current instead of relying on colour", async () => {
    render(<App core={presentationCore()} />);
    await waitFor(() => {
      expect(document.querySelectorAll("button.filerow")).toHaveLength(2);
    });
    const rows = Array.from(document.querySelectorAll("button.filerow"));
    const current = screen.getAllByRole("button", { current: true });
    expect(current).toHaveLength(1);
    expect(current[0]?.textContent).toContain("MY DISK");
    expect(
      rows.find((row) => row.textContent.includes("SPARE.FZV"))?.getAttribute("aria-current"),
    ).toBeNull();
  });
});

describe("unexported changes accessibility state", () => {
  it("announces the dirty state instead of exposing only a coloured dot", async () => {
    render(<App core={presentationCore(instrumentSnapshot())} />);
    fireEvent.click(await screen.findByRole("button", { name: "disk MY DISK, rename" }));
    const label = await screen.findByRole("textbox", { name: "disk label" });
    fireEvent.change(label, { target: { value: "NEW NAME" } });
    fireEvent.blur(label);
    expect(await screen.findByRole("status", { name: "Unexported changes" })).toBeTruthy();
  });
});
