// E1: a failure shows where the user acted. A dialog action that acts
// only inside `if (apply(r))` leaves a refusal standing over a status
// bar line the overlay covers, with no reason shown, so these actions
// (onConvertSfz, onPlacementChoice) close first and report after. The
// WAV conversion dialog is the exception: it stays open and reports
// inline, covered by importdialog.test.tsx.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Core, Snapshot } from "../src/boundary/contract";
import { IMAGE_SIZE, err } from "../src/boundary/contract";
import { createFakeCore } from "../src/core/fake";
import { createCoreStub } from "../src/core/stub";
import { App } from "../src/shell/App";
import { openInstrumentDisk, pickFiles } from "./helpers";

/** Dirty the open document, which is what the two guards react to. */
async function dirtyEdit() {
  const field = screen.getByLabelText("loop 1 start");
  fireEvent.change(field, { target: { value: "5" } });
  fireEvent.blur(field);
  await screen.findByText("●");
}

describe("a refused dialog action (E1)", () => {
  it("closes the new disk dialog and shows why", async () => {
    const core = createCoreStub({
      newDisk: () =>
        Promise.resolve(err<Snapshot>("invalid-label", "an FZ disk label is ASCII only")),
    });
    render(<App core={core} />);

    fireEvent.click(await screen.findByRole("button", { name: "New disk" }));
    fireEvent.change(await screen.findByLabelText("disk label"), {
      target: { value: "MŸ DISK" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await screen.findByText(/ASCII only/);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("closes the delete confirmation and shows why", async () => {
    const inner = createFakeCore();
    const core: Core = {
      ...inner,
      deleteFile: (name) => Promise.resolve(err<Snapshot>("in-use", `${name} is open elsewhere`)),
    };
    await openInstrumentDisk(core);

    fireEvent.contextMenu(screen.getByRole("button", { name: /full/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));

    await screen.findByText(/open elsewhere/);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("closes the unexported changes guard when the close fails", async () => {
    const inner = createFakeCore();
    const core: Core = {
      ...inner,
      closeDisk: () => Promise.resolve(err<Snapshot>("busy", "a conversion is still running")),
    };
    await openInstrumentDisk(core);
    await dirtyEdit();

    fireEvent.click(screen.getByRole("button", { name: "Eject" }));
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));

    await screen.findByText(/conversion is still running/);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("closes the switch guard when the replacement image is refused", async () => {
    const inner = createFakeCore();
    let opens = 0;
    const core: Core = {
      ...inner,
      openImage: (bytes) => {
        opens += 1;
        // The first open is the one under test's setup; the switch is
        // the second, and that is the one the core refuses.
        return opens > 1
          ? Promise.resolve(err<Snapshot>("invalid-image", "that image is not an FZ disk"))
          : inner.openImage(bytes);
      },
    };
    await openInstrumentDisk(core);
    await dirtyEdit();

    pickFiles([new File([new Uint8Array(IMAGE_SIZE)], "OTHER.img")]);
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));

    await screen.findByText(/not an FZ disk/);
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
