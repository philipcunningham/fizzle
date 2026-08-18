// Focus must not fall to the body when the thing under it goes away
// (Q5). On the voices tab that means restarting from tab stop 0 of
// about 247. Three moments lose it: a dialog closing, a rename
// committing, and the deletion of the row that holds focus.
import { createEvent, fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Core, CoreResult, Snapshot } from "../src/boundary/contract";
import { createFakeCore } from "../src/core/fake";
import { openDisk, openInstrumentDisk } from "./helpers";

/**
 * The fake's disk carries the instrument dump alone, so a delete would
 * empty the list and hide the question. One spare file is injected
 * into every snapshot, which makes a neighbour exist to receive focus.
 */
function twoFileCore(): Core {
  const inner = createFakeCore();
  const withSpare = (result: CoreResult<Snapshot>): CoreResult<Snapshot> =>
    result.ok && result.value.disk
      ? {
          ok: true,
          value: {
            ...result.value,
            disk: {
              ...result.value.disk,
              files: [
                ...result.value.disk.files,
                { name: "SPARE.FZV", type: "voice", sizeBytes: 2048 },
              ],
            },
          },
        }
      : result;
  return {
    ...inner,
    snapshot: () => inner.snapshot().then(withSpare),
    openImage: (bytes) => inner.openImage(bytes).then(withSpare),
    deleteFile: (name) => inner.deleteFile(name).then(withSpare),
  };
}

describe("focus return (Q5)", () => {
  it("returns focus to the trigger when a dialog closes", async () => {
    await openInstrumentDisk();
    const trigger = screen.getByRole("button", { name: "export KICK" });
    trigger.focus();
    fireEvent.click(trigger);
    await screen.findByText("Export voice");

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    await waitFor(() => {
      expect(document.activeElement).toBe(trigger);
    });
  });

  it("returns focus to the disk label when the rename commits", async () => {
    await openDisk();
    const label = screen.getByRole("button", { name: /MY DISK, rename/ });
    label.focus();
    fireEvent.click(label);

    const input = await screen.findByLabelText("disk label");
    fireEvent.change(input, { target: { value: "RENAMED" } });
    fireEvent.blur(input);

    const renamed = await screen.findByRole("button", { name: /RENAMED, rename/ });
    await waitFor(() => {
      expect(document.activeElement).toBe(renamed);
    });
  });

  it("moves focus to a neighbouring row when the focused row is deleted", async () => {
    await openInstrumentDisk(twoFileCore());
    const row = screen.getByRole("button", { name: /full/ });
    row.focus();

    fireEvent.contextMenu(row);
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    await screen.findByText("No instrument on this disk");

    const spare = screen.getByRole("button", { name: /SPARE/ });
    await waitFor(() => {
      expect(document.activeElement).toBe(spare);
    });
  });
});

// Committing a rename hands focus back to the button that opened the
// field, during the keydown, so Enter's default would land there and
// reopen the rename. A control that gives focus away mid-key has to
// stop that key's default action.
describe("committing a rename does not reopen it", () => {
  it("cancels Enter so the refocused button is not activated", async () => {
    await openInstrumentDisk();
    fireEvent.click(await screen.findByRole("button", { name: /rename/ }));
    const field = await screen.findByLabelText("disk label");
    fireEvent.change(field, { target: { value: "RENAMED" } });

    const event = createEvent.keyDown(field, { key: "Enter" });
    fireEvent(field, event);
    expect(event.defaultPrevented).toBe(true);
  });
});

// The voice name is the same gesture as the disk label in another file.
// Focus on the body after committing means restarting from the first
// tab stop on the voices tab.
describe("committing a voice rename", () => {
  it("returns focus to the row it began on", async () => {
    await openInstrumentDisk();
    const row = (await screen.findAllByRole("row")).find((r) => r.textContent.includes("KICK"));
    if (!row) throw new Error("no voice row");
    row.focus();
    fireEvent.keyDown(row, { key: "F2" });
    const field = await screen.findByLabelText("voice name");
    fireEvent.change(field, { target: { value: "SNAP" } });
    fireEvent.blur(field);

    await waitFor(() => {
      expect(document.activeElement).toBe(row);
    });
  });

  it("cancels Enter so a refocused control is not activated by it", async () => {
    await openInstrumentDisk();
    const row = (await screen.findAllByRole("row")).find((r) => r.textContent.includes("KICK"));
    if (!row) throw new Error("no voice row");
    fireEvent.keyDown(row, { key: "F2" });
    const field = await screen.findByLabelText("voice name");
    const event = createEvent.keyDown(field, { key: "Enter" });
    fireEvent(field, event);
    expect(event.defaultPrevented).toBe(true);
  });
});

// The keyboard route exists and the hint never mentions it, so a
// keyboard user is told to double click (Q5).
describe("the voice list hint", () => {
  it("names the keyboard route as well as the pointer one", async () => {
    await openInstrumentDisk();
    const note = await screen.findByText(/marks a voice no Area plays yet/);
    expect(note.textContent).toMatch(/F2/);
  });
});
