// The WAV import dialog answers before it acts (R6): the size line is
// the core's own estimate and reacts to the rate, the stereo question
// appears only when there is a stereo file to answer for, an import
// that cannot land is blocked with the way out named, and a failed
// conversion reports in the dialog where the user acted rather than
// only in the footer.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Core, Snapshot } from "../src/boundary/contract";
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
    // The estimate is advisory; the core stays the authority on
    // whether the bytes convert.
    expect(convertButton().disabled).toBe(false);
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
    await screen.findByText(/two disks/);
    expect(screen.getByText(/export both/i).textContent).toBeTruthy();
    expect(convertButton().disabled).toBe(false);
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
