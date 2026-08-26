// E5's other half: a core that can no longer answer. A fatal envelope
// in the status bar would read as a raw machine code with no
// explanation and no way on, so a crash gets the panel, the plain
// sentence, and a reload instead. An ordinary refusal keeps its status
// bar line.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Core, Snapshot } from "../src/boundary/contract";
import { CORE_CRASH_MESSAGE, CORE_UNAVAILABLE, coreCrash } from "../src/boundary/contract";
import { createScenarioCore } from "./support/scenarioCore";
import { createCoreStub } from "../src/core/stub";
import { App } from "../src/shell/App";
import { openInstrumentDisk, pickFiles } from "./helpers";

describe("a fatal envelope (E5)", () => {
  it("shows the plain sentence and a reload, never the code", async () => {
    const core = createCoreStub({
      newDisk: () => Promise.resolve(coreCrash<Snapshot>("the worker exited")),
    });
    render(<App core={core} />);

    fireEvent.click(await screen.findByRole("button", { name: "New disk" }));
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    const panel = await screen.findByRole("alert", { name: "core failure" });
    expect(panel.textContent).toContain(CORE_CRASH_MESSAGE);
    expect(panel.textContent).not.toContain(CORE_UNAVAILABLE);
    expect(screen.getByRole("button", { name: "Reload" })).toBeTruthy();
  });

  it("keeps the technical reason to hand, and reaches every call site", async () => {
    const inner = createScenarioCore();
    const core: Core = {
      ...inner,
      extractFile: () => Promise.resolve(coreCrash<Uint8Array>("postMessage on a dead worker")),
    };
    await openInstrumentDisk(core);

    // A site that reports outside apply(), so the reporter is shared
    // rather than bolted onto one path.
    fireEvent.click(screen.getByRole("button", { name: /Export instrument/ }));

    const panel = await screen.findByRole("alert", { name: "core failure" });
    expect(panel.textContent).toContain(CORE_CRASH_MESSAGE);
    expect(panel.textContent).toContain("postMessage on a dead worker");
  });

  it("leaves an ordinary refusal in the status bar", async () => {
    await openInstrumentDisk();
    pickFiles([new File([new Uint8Array(16)], "bad.img")]);

    await screen.findByText(/an FZ image is/);
    expect(screen.queryByRole("alert", { name: "core failure" })).toBeNull();
  });
});
