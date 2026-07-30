// The shell frame over the fake core: start screen, the new disk
// dialog, the workspace frame, errors in the status bar, the close
// guard, and the disk rename.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { openDisk, openInstrumentDisk, pickFiles } from "./helpers";

describe("shell frame", () => {
  it("starts on the start screen and creates a disk through the dialog", async () => {
    await openDisk();
    expect(screen.getByRole("navigation", { name: "disk files" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Voices" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Export" })).toBeTruthy();
  });

  it("opens an image with an instrument and renders the voice list", async () => {
    await openInstrumentDisk();
    await screen.findByText("Voices (3/64)");
    expect(screen.getAllByText("KICK").length).toBeGreaterThan(0);
  });

  it("shows core errors in the status bar with a dismiss (E1)", async () => {
    await openInstrumentDisk();
    pickFiles([new File([new Uint8Array(16)], "bad.img")]);
    await waitFor(() => {
      const alerts = screen.getAllByRole("alert").map((a) => a.textContent);
      expect(alerts.join(" ")).toContain("invalid-image");
    });
    fireEvent.click(screen.getByRole("button", { name: "dismiss error" }));
    await waitFor(() => {
      const alerts = screen.queryAllByRole("alert").map((a) => a.textContent);
      expect(alerts.join(" ")).not.toContain("invalid-image");
    });
  });

  it("close disk returns to the start screen when clean", async () => {
    await openDisk();
    fireEvent.click(screen.getByRole("button", { name: "Close disk" }));
    await screen.findByRole("button", { name: "New disk" });
    expect(screen.queryByRole("tab", { name: "Voices" })).toBeNull();
  });

  it("close disk guards unexported changes", async () => {
    await openInstrumentDisk();
    const field = screen.getByLabelText("loop 1 start");
    fireEvent.change(field, { target: { value: "5" } });
    fireEvent.blur(field);
    await screen.findByText("●");
    fireEvent.click(screen.getByRole("button", { name: "Close disk" }));
    await screen.findByText("Unexported changes");
    fireEvent.click(screen.getByRole("button", { name: "Keep working" }));
    expect(screen.getByRole("tab", { name: "Voices" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Close disk" }));
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));
    await screen.findByRole("button", { name: "New disk" });
  });

  it("renames the disk from the topbar label", async () => {
    await openDisk();
    // The label is a button, so the rename is reachable by keyboard,
    // not only by a double click.
    fireEvent.click(await screen.findByRole("button", { name: /MY DISK, rename/ }));
    const input = await screen.findByLabelText("disk label");
    fireEvent.change(input, { target: { value: "RENAMED" } });
    fireEvent.blur(input);
    await screen.findByRole("button", { name: /RENAMED, rename/ });
  });
});
