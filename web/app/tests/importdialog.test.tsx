// The WAV import dialog answers before it acts (R6): the size line is
// the core's own estimate and reacts to the rate, the stereo question
// appears only when there is a stereo file to answer for, an import
// that cannot land is blocked with the way out named, and a failed
// conversion reports in the dialog where the user acted rather than
// only in the footer.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Core, SFZImportResult, Snapshot } from "../src/boundary/contract";
import { err } from "../src/boundary/contract";
import { createFakeCore } from "../src/core/fake";
import { openDisk, openInstrumentDisk, pickFiles, wavFixture } from "./helpers";

function convertButton(): HTMLButtonElement {
  return screen.getByRole<HTMLButtonElement>("button", { name: "Convert" });
}

function wavFile(name: string, channels: number, rate: number, frames: number): File {
  return new File([wavFixture(channels, rate, frames)], name);
}

describe("the import dialog's size line", () => {
  it("shows the converted size and room, and reacts to the rate", async () => {
    await openDisk();
    pickFiles([wavFile("pad.wav", 1, 36000, 36000)]);

    await screen.findByText("Import 1 WAV");
    const line = await screen.findByText(/Becomes about/);
    expect(line.textContent).toContain("1.0 s");
    expect(line.textContent).toContain("room for about");
    expect(line.textContent).toContain("18 kHz");
    const before = line.textContent;

    fireEvent.click(screen.getByRole("radio", { name: "9 kHz" }));
    await waitFor(() => {
      expect(screen.getByText(/Becomes about/).textContent).not.toBe(before);
    });
    expect(screen.getByText(/Becomes about/).textContent).toContain("9 kHz");
  });

  it("keeps the batch total across several files", async () => {
    await openInstrumentDisk();
    pickFiles([wavFile("kick.wav", 1, 18000, 18000), wavFile("snare.wav", 1, 18000, 36000)]);

    await screen.findByText("Import 2 WAVs");
    const line = await screen.findByText(/Becomes about/);
    expect(line.textContent).toContain("3.0 s");
  });
});

describe("the stereo question", () => {
  it("appears only when a file is stereo", async () => {
    await openDisk();
    pickFiles([wavFile("st.wav", 2, 44100, 1000)]);
    await screen.findByText("Import 1 WAV");
    await screen.findByRole("radio", { name: "Mix" });
  });

  it("stays hidden for a mono batch", async () => {
    await openDisk();
    pickFiles([wavFile("mono.wav", 1, 18000, 1000)]);
    await screen.findByText("Import 1 WAV");
    await screen.findByText(/Becomes about/);
    expect(screen.queryByRole("radio", { name: "Mix" })).toBeNull();
  });

  it("stays hidden when the files are not readable WAVs", async () => {
    await openDisk();
    pickFiles([new File([new Uint8Array(64)], "noise.wav")]);
    await screen.findByText("Import 1 WAV");
    await screen.findByText(/not a readable WAV/);
    expect(screen.queryByRole("radio", { name: "Mix" })).toBeNull();
    // A batch the estimate cannot even read is doomed at any rate, so
    // the button says so instead of inviting a refusal loop.
    expect(convertButton().disabled).toBe(true);
  });
});

describe("an import that cannot land", () => {
  it("disables Convert over the sampler's memory and names the way out", async () => {
    await openDisk();
    // 59.4 s of stereo 44.1 kHz: over the cap at 18 (the default) and
    // 36 kHz, possible at 9 kHz.
    pickFiles([wavFile("long.wav", 2, 44100, 2619540)]);

    await screen.findByText("Import 1 WAV");
    const refusal = await screen.findByText(/more than the sampler's memory/);
    expect(refusal.textContent).toContain("59.4 s");
    expect(refusal.textContent).toContain("18 kHz");
    // The ceiling figure comes from the core as capSeconds; a fake or
    // wiring regression would render "(0.0 s max)" here.
    expect(refusal.textContent).toContain("(58.3 s max)");
    expect(refusal.textContent).toContain("9 kHz fits");
    expect(convertButton().disabled).toBe(true);

    fireEvent.click(screen.getByRole("radio", { name: "9 kHz" }));
    await screen.findByText(/Becomes about/);
    expect(screen.queryByText(/more than the sampler's memory/)).toBeNull();
    expect(convertButton().disabled).toBe(false);
  });

  it("announces a two disk split and leaves Convert enabled", async () => {
    await openDisk();
    pickFiles([wavFile("one.wav", 1, 18000, 450000), wavFile("two.wav", 1, 18000, 450000)]);

    await screen.findByText("Import 2 WAVs");
    await screen.findByText(/spreads the instrument across two disks. Export both images./);
    expect(convertButton().disabled).toBe(false);
  });
});

describe("the dialog's answers start fresh", () => {
  // The rate and stereo answers live in the shell so the estimate can
  // react to them, but they are per-import questions: a Left picked
  // for one batch must not silently apply to the next.
  it("resets rate and stereo when a new import dialog opens", async () => {
    await openDisk();
    pickFiles([wavFile("st.wav", 2, 44100, 1000)]);
    await screen.findByText("Import 1 WAV");
    fireEvent.click(await screen.findByRole("radio", { name: "Left" }));
    fireEvent.click(screen.getByRole("radio", { name: "9 kHz" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    pickFiles([wavFile("st2.wav", 2, 44100, 1000)]);
    await screen.findByText("Import 1 WAV");
    expect(
      (await screen.findByRole<HTMLInputElement>("radio", { name: "Mix" })).dataset.state,
    ).toBe("checked");
    expect(screen.getByRole<HTMLInputElement>("radio", { name: "18 kHz" }).dataset.state).toBe(
      "checked",
    );
  });
});

describe("the estimate follows the document", () => {
  it("re-estimates after the document changed, not from a stale cache", async () => {
    await openDisk();
    pickFiles([wavFile("pad.wav", 1, 18000, 18000)]);
    const first = (await screen.findByText(/room for about/)).textContent;
    fireEvent.click(convertButton());
    await screen.findByText(/Voices \(1\/64\)/);

    // The same file again: the dump grew, so the room shrank. A
    // cached answer keyed only on the files would repeat `first`.
    pickFiles([wavFile("pad.wav", 1, 18000, 18000)]);
    await screen.findByText(/room for about/);
    await waitFor(() => {
      expect(screen.getByText(/room for about/).textContent).not.toBe(first);
    });
  });

  it("keeps the refusal, and Convert disabled, while a re-estimate is in flight", async () => {
    const inner = createFakeCore();
    const gate: { release?: () => void } = {};
    const core: Core = {
      ...inner,
      estimateImport: (files, rate, channel) =>
        rate === 9000
          ? new Promise((resolve) => {
              gate.release = () => {
                void inner.estimateImport(files, rate, channel).then(resolve);
              };
            })
          : inner.estimateImport(files, rate, channel),
    };
    await openDisk(core);
    pickFiles([wavFile("long.wav", 2, 44100, 2619540)]);

    await screen.findByText(/more than the sampler's memory/);
    expect(convertButton().disabled).toBe(true);

    // Switch to the rate that fits; the answer is parked. The shown
    // verdict must not lapse into an enabled Convert while waiting.
    fireEvent.click(screen.getByRole("radio", { name: "9 kHz" }));
    expect(screen.getByText(/more than the sampler's memory/)).toBeDefined();
    expect(convertButton().disabled).toBe(true);

    gate.release?.();
    await screen.findByText(/Becomes about/);
    expect(convertButton().disabled).toBe(false);
  });
});

describe("an instrument at the voice limit", () => {
  it("names the limit and disables Convert", async () => {
    const inner = createFakeCore();
    const core: Core = {
      ...inner,
      estimateImport: async (files, rate, channel) => {
        const r = await inner.estimateImport(files, rate, channel);
        if (!r.ok) return r;
        return { ok: true, value: { ...r.value, verdict: "wont-fit", reason: "voice-limit" } };
      },
    };
    await openDisk(core);
    pickFiles([wavFile("extra.wav", 1, 18000, 100)]);

    await screen.findByText("Import 1 WAV");
    await screen.findByText(/more voices than the 64 an instrument holds/);
    expect(convertButton().disabled).toBe(true);
  });
});

describe("a conversion that fails", () => {
  it("reports in the open dialog and lets the user retry", async () => {
    const inner = createFakeCore();
    const core: Core = {
      ...inner,
      importWavToInstrument: () =>
        Promise.resolve(err<Snapshot>("invalid-wav", "the data chunk is truncated")),
    };
    await openDisk(core);
    pickFiles([wavFile("kick.wav", 1, 18000, 1000)]);

    await screen.findByText("Import 1 WAV");
    fireEvent.click(convertButton());

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("the data chunk is truncated");
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(convertButton().disabled).toBe(false);
  });

  it("reports a failed folder conversion in the open dialog", async () => {
    const inner = createFakeCore();
    const core: Core = {
      ...inner,
      importWavFolder: () =>
        Promise.resolve(err<SFZImportResult>("invalid-wav", "the batch would not convert")),
    };
    // No instrument: two or more files take the folder route.
    await openDisk(core);
    pickFiles([wavFile("kick.wav", 1, 18000, 1000), wavFile("snare.wav", 1, 18000, 1000)]);

    await screen.findByText("Import 2 WAVs");
    fireEvent.click(convertButton());

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("the batch would not convert");
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(convertButton().disabled).toBe(false);
  });

  it("stops a batch at the failing file and keeps the rest to retry", async () => {
    const inner = createFakeCore();
    let calls = 0;
    const core: Core = {
      ...inner,
      importWavToInstrument: (filename, bytes, rate, channel) => {
        calls += 1;
        return calls === 2
          ? Promise.resolve(err<Snapshot>("invalid-wav", "snare.wav went wrong"))
          : inner.importWavToInstrument(filename, bytes, rate, channel);
      },
    };
    await openInstrumentDisk(core);
    pickFiles([
      wavFile("kick.wav", 1, 18000, 1000),
      wavFile("snare.wav", 1, 18000, 1000),
      wavFile("hat.wav", 1, 18000, 1000),
    ]);

    await screen.findByText("Import 3 WAVs");
    fireEvent.click(convertButton());

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("file 2 of 3");
    expect(alert.textContent).toContain("snare.wav went wrong");
    // The batch resumes from the failure: the two unimported files
    // stay in the dialog.
    await screen.findByText("Import 2 WAVs");
  });
});

// Cancelling a running batch stops it: the chain must not keep
// mutating the document after the dialog closes, and a failure
// arriving after the cancel must not resurrect the dialog.
describe("cancelling a running conversion", () => {
  it("stops the chain and stays closed on a late failure", async () => {
    const inner = createFakeCore();
    const gates: (() => void)[] = [];
    let calls = 0;
    const core: Core = {
      ...inner,
      importWavToInstrument: (filename, bytes, rate, channel) => {
        calls += 1;
        return new Promise((resolve) => {
          gates.push(() => {
            void inner.importWavToInstrument(filename, bytes, rate, channel).then(resolve);
          });
        });
      },
    };
    await openInstrumentDisk(core);
    pickFiles([
      wavFile("one.wav", 1, 18000, 500),
      wavFile("two.wav", 1, 18000, 500),
      wavFile("three.wav", 1, 18000, 500),
    ]);
    await screen.findByText("Import 3 WAVs");
    fireEvent.click(convertButton());
    expect(calls).toBe(1);

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).toBeNull();

    // The first conversion lands after the cancel: no second call,
    // and no dialog comes back.
    gates[0]?.();
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(calls).toBe(1);
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

// A mixed selection prompts one placement at a time instead of
// overwriting earlier prompts with later ones.
describe("mixed selections queue their prompts", () => {
  it("shows the replace prompt first, then the WAV dialog", async () => {
    await openInstrumentDisk();
    pickFiles([
      new File([new Uint8Array(4096)], "other.fzf"),
      wavFile("kick.wav", 1, 18000, 500),
    ]);

    await screen.findByText("Replace the instrument?");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    await screen.findByText("Import 1 WAV");
  });
});

// After an import the app reveals what arrived: the voices tab shows
// and the newcomer is the selection (spec section 8).
describe("imports reveal what arrived", () => {
  it("selects the imported voice and returns to the voices tab", async () => {
    await openInstrumentDisk();
    fireEvent.click(screen.getByRole("tab", { name: "Effects" }));

    pickFiles([wavFile("Fresh Hit.wav", 1, 18000, 500)]);
    await screen.findByText("Import 1 WAV");
    fireEvent.click(convertButton());

    await waitFor(() => {
      expect(screen.getByRole("tab", { name: "Voices" }).getAttribute("aria-selected")).toBe(
        "true",
      );
    });
    // The snapshot refetch lands a beat after the reveal, so the row
    // assertion waits for it.
    await waitFor(() => {
      const selected = screen
        .getAllByRole("row")
        .find((r) => r.getAttribute("aria-selected") === "true" && r.textContent?.includes("HIT"));
      expect(selected?.textContent).toContain("FRESH HIT");
    });
  });
});

// Initial focus in a confirm dialog belongs to the safe action: a
// habitual Enter must never be the destructive one.
describe("confirm dialogs focus the safe action", () => {
  it("the delete confirmation arrives focused on Cancel", async () => {
    await openInstrumentDisk();
    fireEvent.contextMenu(screen.getByRole("button", { name: /full/ }));
    await screen.findByText("Delete the instrument?");
    await waitFor(() => {
      expect(document.activeElement?.textContent).toBe("Cancel");
    });
  });
});
