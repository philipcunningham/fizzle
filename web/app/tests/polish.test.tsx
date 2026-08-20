// The shell's polish gates: the unsupported browser notice, dismissible
// errors where the user acted (E1), keyboard operability of the
// on-screen keyboard (Q5), and the rendering error boundary (E5).
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Core, CoreResult, Snapshot } from "../src/boundary/contract";
import { IMAGE_SIZE } from "../src/boundary/contract";
import { createFakeCore } from "../src/core/fake";
import { App } from "../src/shell/App";
import { ErrorBoundary } from "../src/shell/ErrorBoundary";
import { Keyboard } from "../src/ui/Keyboard";
import { openDisk, openInstrumentDisk, pickFiles } from "./helpers";

describe("unsupported browser notice", () => {
  it("appears where the save picker is missing and dismisses", async () => {
    render(<App core={createFakeCore()} />);
    const notice = await screen.findByRole("alert", { name: "unsupported browser" });
    expect(notice.textContent).toContain("Chromium");
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByRole("alert", { name: "unsupported browser" })).toBeNull();
  });
});

describe("dismissible errors (E1)", () => {
  it("an operation failure shows in the status bar and dismisses", async () => {
    render(<App core={createFakeCore()} />);
    // A bad image through the picker: the core rejects with an envelope.
    fireEvent.click(await screen.findByRole("button", { name: "Browse" }));
    fireEvent.change(screen.getByLabelText("fz files"), {
      target: { files: [new File([new Uint8Array(16)], "bad.img")] },
    });

    const dismiss = await screen.findByRole("button", { name: "dismiss error" });
    fireEvent.click(dismiss);
    expect(screen.queryByRole("button", { name: "dismiss error" })).toBeNull();
  });
});

describe("keyboard operability (Q5)", () => {
  it("a key plays from Enter and releases on key up", () => {
    const events: [string, number][] = [];
    render(
      <Keyboard
        lowNote={36}
        octaves={2}
        onNoteOn={(n) => events.push(["on", n])}
        onNoteOff={(n) => events.push(["off", n])}
      />,
    );
    const key = screen.getByRole("button", { name: "play C3" });
    fireEvent.keyDown(key, { key: "Enter" });
    fireEvent.keyUp(key, { key: "Enter" });
    expect(events).toEqual([
      ["on", 48],
      ["off", 48],
    ]);
  });

  it("every key is focusable", () => {
    render(<Keyboard lowNote={36} octaves={1} onNoteOn={() => 0} onNoteOff={() => 0} />);
    const key = screen.getByTestId("key-37");
    expect(key.getAttribute("tabindex")).toBe("0");
    expect(key.getAttribute("role")).toBe("button");
  });
});

describe("rendering error boundary (E5)", () => {
  function Bomb(): never {
    throw new Error("kaboom");
  }

  it("contains a crash and offers recovery plus a last resort export", () => {
    let exported = 0;
    render(
      <ErrorBoundary
        onExport={() => {
          exported++;
        }}
      >
        <Bomb />
      </ErrorBoundary>,
    );
    const alert = screen.getByRole("alert", { name: "rendering failure" });
    expect(alert.textContent).toContain("kaboom");
    fireEvent.click(screen.getByRole("button", { name: "Export current document" }));
    expect(exported).toBe(1);
    expect(screen.getByRole("button", { name: "Reload" })).toBeTruthy();
  });

  /**
   * A core whose revision is an object rather than a number. The status
   * bar prints the revision, so the shell throws while rendering,
   * outside the sidebar and the tab body. It stands in for any failure
   * in the topbar, the bar, a dialog, or the start screen: all are the
   * shell's own output, so only a boundary above it contains them.
   */
  function crashingStatusBarCore(): { core: Core; exports: () => number } {
    const inner = createFakeCore();
    let exports = 0;
    const poison = (result: CoreResult<Snapshot>): CoreResult<Snapshot> =>
      result.ok && result.value.disk
        ? { ok: true, value: { ...result.value, revision: {} as unknown as number } }
        : result;
    const core: Core = {
      ...inner,
      snapshot: () => inner.snapshot().then(poison),
      openImage: (bytes) => inner.openImage(bytes).then(poison),
      exportImage: () => {
        exports += 1;
        return inner.exportImage();
      },
    };
    return { core, exports: () => exports };
  }

  it("contains a crash outside the sidebar and tab body, and still exports", async () => {
    const { core, exports } = crashingStatusBarCore();
    render(<App core={core} />);
    fireEvent.click(await screen.findByRole("button", { name: "Browse" }));
    pickFiles([new File([new Uint8Array(IMAGE_SIZE)], "TECHNO.img")]);

    const alert = await screen.findByRole("alert", { name: "rendering failure" });
    expect(alert.textContent).toContain("Objects are not valid as a React child");

    // E5's last resort: the core still answers, so the document is
    // still exportable from the crash screen.
    fireEvent.click(screen.getByRole("button", { name: "Export current document" }));
    await waitFor(() => {
      expect(exports()).toBe(1);
    });
  });
});

describe("QA fixes in the shell", () => {
  it("Export first exports, then completes the close it guarded", async () => {
    await openInstrumentDisk();
    // Dirty the document.
    const rate = await screen.findByLabelText("DCA envelope stage 1 rate");
    fireEvent.change(rate, { target: { value: "12" } });
    fireEvent.blur(rate);
    await screen.findByText("●");

    fireEvent.click(screen.getByRole("button", { name: "Eject" }));
    fireEvent.click(await screen.findByRole("button", { name: "Export first" }));

    // The guard is gone and the disk is closed, not left in a stale dialog.
    await screen.findByRole("button", { name: "New disk" });
    expect(screen.queryByText("Unexported changes")).toBeNull();
  });

  it("voice and area rows are keyboard selectable (Q5)", async () => {
    await openInstrumentDisk();
    const voiceRows = screen.getAllByRole("row");
    const snare = voiceRows.find((r) => r.textContent.includes("SNARE"));
    if (!snare) throw new Error("no SNARE row");
    expect(snare.getAttribute("tabindex")).toBe("0");
    fireEvent.keyDown(snare, { key: "Enter" });
    await waitFor(() => {
      expect(snare.getAttribute("aria-selected")).toBe("true");
    });

    // The area editor opens from the keyboard too.
    fireEvent.click(screen.getByRole("tab", { name: "Banks and Areas" }));
    const areaRow = (await screen.findAllByRole("row")).find((r) => r.textContent.includes("KICK"));
    if (!areaRow) throw new Error("no area row");
    fireEvent.keyDown(areaRow, { key: "Enter" });
    await screen.findByText(/Edit area/);
  });

  it("a duplicated voice reports shared audio rather than double size", async () => {
    await openInstrumentDisk();
    fireEvent.click(screen.getByRole("tab", { name: "Banks and Areas" }));
    fireEvent.click(await screen.findByRole("button", { name: "duplicate area 1" }));
    fireEvent.click(screen.getByRole("tab", { name: "Voices" }));
    await waitFor(() => {
      expect(screen.getAllByText("shared").length).toBe(1);
    });
  });
});

// Menu and button labels state the action plainly; the trailing
// ellipsis affordance is not this UI's idiom.
describe("labels carry no trailing ellipsis", () => {
  it("the browse button states the action plainly", async () => {
    render(<App core={createFakeCore()} />);
    const browse = await screen.findByRole("button", { name: "Browse" });
    expect(browse.textContent).not.toMatch(/…$/);
  });

  it("the import surface states its actions plainly", async () => {
    await openDisk();
    fireEvent.click(screen.getByRole("button", { name: "Import" }));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain("Drop a disk image");
    // One way in, as on the first page: a browser cannot offer files
    // and folders in one picker, and a folder still arrives by drag.
    expect(within(dialog).getByRole("button", { name: "Browse" })).toBeDefined();
    expect(within(dialog).queryByRole("button", { name: "Choose a folder" })).toBeNull();
    for (const button of within(dialog).getAllByRole("button")) {
      expect(button.textContent).not.toMatch(/…$/);
    }
  });
});
