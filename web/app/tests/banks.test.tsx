// The Banks and Areas screen (R11 to R13) over the fake core: the
// bank strip, the areas table with duplicate, delete, and reorder,
// and the edit area panel.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { openInstrumentDisk } from "./helpers";

async function openBanksTab() {
  await openInstrumentDisk();
  fireEvent.click(screen.getByRole("tab", { name: "Banks and Areas" }));
  await screen.findByRole("table", { name: "areas" });
}

describe("banks and areas", () => {
  it("renders the areas from the instrument", async () => {
    await openBanksTab();
    const table = screen.getByRole("table", { name: "areas" });
    const rows = within(table).getAllByRole("row");
    // Header plus the fake's two areas.
    expect(rows.length).toBe(3);
    expect(table.textContent).toContain("KICK");
    expect(table.textContent).toContain("SNARE");
  });

  it("edits an area's velocity through the edit panel steppers", async () => {
    await openBanksTab();
    const table = screen.getByRole("table", { name: "areas" });
    fireEvent.click(within(table).getByText("KICK"));
    await screen.findByText(/Edit area · KICK/);

    const field = screen.getByLabelText("Vel high");
    fireEvent.change(field, { target: { value: "64" } });
    fireEvent.blur(field);
    await waitFor(() => {
      const refreshed = screen.getByRole("table", { name: "areas" });
      expect(refreshed.textContent).toContain("1..64");
    });
  });

  it("duplicates an area: the velocity switch grows the table (R11)", async () => {
    await openBanksTab();
    fireEvent.click(screen.getByRole("button", { name: "duplicate area 1" }));
    await waitFor(() => {
      const table = screen.getByRole("table", { name: "areas" });
      expect(within(table).getAllByRole("row").length).toBe(4);
    });
  });

  it("deletes and reorders areas (R11)", async () => {
    await openBanksTab();
    fireEvent.click(screen.getByRole("button", { name: "move area 2 up" }));
    await waitFor(() => {
      const rows = within(screen.getByRole("table", { name: "areas" })).getAllByRole("row");
      expect(rows[1]?.textContent).toContain("SNARE");
    });
    fireEvent.click(screen.getByRole("button", { name: "delete area 1" }));
    await waitFor(() => {
      const rows = within(screen.getByRole("table", { name: "areas" })).getAllByRole("row");
      expect(rows.length).toBe(2);
    });
  });

  it("adds an area for the first voice", async () => {
    await openBanksTab();
    fireEvent.click(screen.getByRole("button", { name: "Add area" }));
    await waitFor(() => {
      const rows = within(screen.getByRole("table", { name: "areas" })).getAllByRole("row");
      expect(rows.length).toBe(4);
    });
  });

  it("renames the bank by double click on the strip", async () => {
    await openBanksTab();
    fireEvent.doubleClick(screen.getByRole("button", { name: /BANK A \(2\)/ }));
    const input = await screen.findByLabelText("bank name");
    fireEvent.change(input, { target: { value: "DRUMS" } });
    fireEvent.blur(input);
    await screen.findByRole("button", { name: /DRUMS \(2\)/ });
  });
});
