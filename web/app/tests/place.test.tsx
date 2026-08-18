// The placement matrix in the new frame (R6, R7): classification,
// the Radix dialogs, and the routing into core calls, driven against
// the fake core.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Channel, SampleRate } from "../src/boundary/contract";
import { IMAGE_SIZE } from "../src/boundary/contract";
import { createFakeCore, fakeCalls } from "../src/core/fake";
import { App } from "../src/shell/App";
import { classifyInput, sfzCandidates } from "../src/viewstate/place";
import { openDisk, openInstrumentDisk, pickFiles, wavFixture, wavHeader } from "./helpers";

const bytes = (fill: number, length = 8) => new Uint8Array(length).fill(fill);
const named = (name: string, data: Uint8Array = bytes(1)) => ({ name, bytes: data });

describe("input classification", () => {
  it("an .sfz anywhere makes the whole set one SFZ instrument", () => {
    const placements = classifyInput([
      named("kit.sfz"),
      named("wavs/kick.wav"),
      named("wavs/snare.wav"),
    ]);
    expect(placements).toHaveLength(1);
    expect(placements[0]).toMatchObject({ kind: "sfz", sfzPath: "kit.sfz" });
  });

  // The core won't choose between two .sfz files, so the classifier
  // leaves the path unchosen and the dialog asks (R6).
  it("two .sfz files leave the path unchosen and list both", () => {
    const files = [named("Kit_alt.sfz"), named("Kit.sfz"), named("kick.wav")];
    const placements = classifyInput(files);
    expect(placements).toHaveLength(1);
    expect(placements[0]).toMatchObject({ kind: "sfz", sfzPath: "" });
    expect(sfzCandidates(files)).toEqual(["Kit.sfz", "Kit_alt.sfz"]);
  });

  it("two images classify as a pair candidate, one as an image", () => {
    expect(classifyInput([named("a.img"), named("b.img")])[0]?.kind).toBe("imagePair");
    expect(classifyInput([named("a.img")])[0]?.kind).toBe("image");
  });

  it("WAVs group into one sorted batch", () => {
    const placements = classifyInput([named("02.wav"), named("01.wav")]);
    expect(placements).toHaveLength(1);
    const wavs = placements[0];
    if (wavs?.kind !== "wavs") throw new Error("expected wavs");
    expect(wavs.files.map((f) => f.name)).toEqual(["01.wav", "02.wav"]);
  });

  it("FZ files each place; strangers report as unsupported", () => {
    const kinds = classifyInput([named("a.fzf"), named("b.fzb"), named("c.fzv"), named("d.pdf")])
      .map((p) => p.kind)
      .sort();
    expect(kinds).toEqual(["fzb", "fzf", "fzv", "unsupported"]);
  });
});

describe("placement routing", () => {
  it(".fzf with an instrument open prompts, then replaces (R7)", async () => {
    await openInstrumentDisk();
    pickFiles([new File([bytes(3)], "NEW.fzf")]);

    await screen.findByText("Replace the instrument?");
    fireEvent.click(screen.getByRole("button", { name: "Replace the instrument" }));

    await waitFor(() => {
      expect(screen.getAllByText("LOADED").length).toBeGreaterThan(0);
    });
  });

  it(".fzb with an instrument open offers the next bank slot (R7)", async () => {
    await openInstrumentDisk();
    pickFiles([new File([bytes(4)], "EXTRA.fzb")]);

    fireEvent.click(await screen.findByRole("button", { name: "Add as bank 2" }));
    fireEvent.click(await screen.findByRole("tab", { name: "Banks and Areas" }));
    await screen.findByRole("button", { name: /FZB BANK \(1\)/ });
  });

  it(".fzv joins the voice list without a prompt (R7)", async () => {
    await openInstrumentDisk();
    pickFiles([new File([bytes(5)], "HAT.fzv")]);
    await waitFor(() => {
      const statuses = screen.getAllByRole("status").map((s) => s.textContent);
      expect(statuses.join(" ")).toContain("HAT.fzv joined the voice list");
    });
    await screen.findByText("Voices (4/64)");
  });

  it("an SFZ folder converts through the dialog; fit reports the rate (R9)", async () => {
    await openInstrumentDisk();
    const sfz = new TextEncoder().encode(
      "<region> sample=wavs/kick.wav\n<region> sample=wavs/snare.wav\n",
    );
    pickFiles([
      new File([sfz], "kit.sfz"),
      new File([bytes(1)], "wavs/kick.wav"),
      new File([bytes(2)], "wavs/snare.wav"),
    ]);

    await screen.findByText("SFZ conversion");
    fireEvent.click(screen.getByRole("button", { name: "Fit to disk (downsample)" }));

    await waitFor(() => {
      const statuses = screen.getAllByRole("status").map((s) => s.textContent);
      expect(statuses.join(" ")).toContain("converted at 9000 Hz");
    });
  });

  it("an incomplete SFZ names its missing samples (R9)", async () => {
    await openInstrumentDisk();
    const sfz = new TextEncoder().encode("<region> sample=wavs/gone.wav\n");
    pickFiles([new File([sfz], "broken.sfz")]);

    await screen.findByText("SFZ conversion");
    fireEvent.click(screen.getByRole("button", { name: "Two disk split" }));

    await waitFor(() => {
      const alerts = screen.getAllByRole("alert").map((a) => a.textContent);
      expect(alerts.join(" ")).toContain("gone.wav");
    });
  });

  it("a WAV batch converts through the rate dialog (R8)", async () => {
    await openInstrumentDisk();
    pickFiles([new File([bytes(1)], "01 kick.wav"), new File([bytes(2)], "02 snare.wav")]);

    await screen.findByText("Import 2 WAVs");
    fireEvent.click(screen.getByRole("button", { name: "Convert" }));
    await screen.findByText("Voices (5/64)");
  });

  // The dialog counts every WAV in the dropped tree, so the count is
  // a promise: the folder import delivers that many voices. The core
  // converted the top level alone. The fake never did, so this pins
  // both layers to one promise rather than reproducing that gap.
  it("a nested WAV folder converts every file it counted (R8)", async () => {
    await openDisk();
    pickFiles([
      new File([wavHeader(1)], "kicks/01.wav"),
      new File([wavHeader(1)], "snares/06.wav"),
      new File([wavHeader(1)], "top.wav"),
    ]);

    await screen.findByText("Import 3 WAVs");
    fireEvent.click(screen.getByRole("button", { name: "Convert" }));
    await screen.findByText("Voices (3/64)");
  });

  it("the folder import carries the one stereo answer to the core (R8)", async () => {
    // No instrument yet, so the batch takes the folder route, where
    // the answer used to be dropped on the floor. The files are
    // stereo, which is what makes the question appear at all.
    await openDisk();
    pickFiles([
      new File([wavFixture(2, 44100, 100)], "01 kick.wav"),
      new File([wavFixture(2, 44100, 100)], "02 snare.wav"),
    ]);

    await screen.findByText("Import 2 WAVs");
    fireEvent.click(await screen.findByRole("radio", { name: "Mix" }));
    fireEvent.click(screen.getByRole("button", { name: "Convert" }));

    await waitFor(() => {
      const statuses = screen.getAllByRole("status").map((s) => s.textContent);
      expect(statuses.join(" ")).toContain("2 WAVs mapped up the keyboard");
    });
    expect(fakeCalls.wavFolderChannel).toBe("mix");
  });

  // B2: the SFZ route is what a dropped library takes, and it asks
  // the same question the WAV route does. The answer used to have
  // nowhere to go: the dialog never showed it and the boundary had no
  // parameter to carry it.
  it("the SFZ conversion carries the one stereo answer to the core", async () => {
    await openDisk();
    const sfz = new TextEncoder().encode("<region> sample=kick.wav\n");
    pickFiles([new File([sfz], "kit.sfz"), new File([wavHeader(2)], "kick.wav")]);

    await screen.findByText("SFZ conversion");
    fireEvent.click(screen.getByRole("radio", { name: "Left" }));
    fireEvent.click(screen.getByRole("button", { name: "Fit to disk (downsample)" }));

    await waitFor(() => {
      expect(fakeCalls.sfzChannel).toBe("left");
    });
  });

  // R6 lists SFZ folders as an input, and a DAW export often holds
  // more than one .sfz. The core asks which one; until the dialog
  // carried the question, nothing could answer it and Cancel was the
  // only way out.
  it("a folder holding two .sfz files asks which one to convert (R6)", async () => {
    const inner = createFakeCore();
    const asked: string[] = [];
    const core = {
      ...inner,
      importSfz: (
        files: Record<string, Uint8Array>,
        sfzPath: string,
        rate: SampleRate,
        fitToDisk: boolean,
        split: boolean,
        channel: Channel,
      ) => {
        asked.push(sfzPath);
        return inner.importSfz(files, sfzPath, rate, fitToDisk, split, channel);
      },
    };
    await openDisk(core);
    pickFiles([
      new File([new TextEncoder().encode("<region> sample=kick.wav\n")], "Kit.sfz"),
      new File([new TextEncoder().encode("<region> sample=snare.wav\n")], "Kit_alt.sfz"),
      new File([wavHeader(1)], "kick.wav"),
      new File([wavHeader(1)], "snare.wav"),
    ]);

    await screen.findByText("SFZ conversion");
    // The first is offered, so Convert always has an answer to send.
    expect(screen.getByRole("radio", { name: "Kit.sfz" }).getAttribute("aria-checked")).toBe(
      "true",
    );
    fireEvent.click(screen.getByRole("radio", { name: "Kit_alt.sfz" }));
    fireEvent.click(screen.getByRole("button", { name: "Two disk split" }));

    await waitFor(() => {
      expect(asked).toEqual(["Kit_alt.sfz"]);
    });
    // The chosen instrument is the one that arrived: Kit_alt.sfz
    // references the snare and Kit.sfz the kick.
    await screen.findByText("SNARE");
  });

  it("one .sfz asks nothing extra, and the core still gets the path", async () => {
    const inner = createFakeCore();
    const asked: string[] = [];
    const core = {
      ...inner,
      importSfz: (
        files: Record<string, Uint8Array>,
        sfzPath: string,
        rate: SampleRate,
        fitToDisk: boolean,
        split: boolean,
        channel: Channel,
      ) => {
        asked.push(sfzPath);
        return inner.importSfz(files, sfzPath, rate, fitToDisk, split, channel);
      },
    };
    await openDisk(core);
    pickFiles([
      new File([new TextEncoder().encode("<region> sample=kick.wav\n")], "Kit.sfz"),
      new File([wavHeader(1)], "kick.wav"),
    ]);

    await screen.findByText("SFZ conversion");
    expect(screen.queryByRole("radiogroup", { name: "which .sfz" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Two disk split" }));
    await waitFor(() => {
      expect(asked).toEqual(["Kit.sfz"]);
    });
  });

  it("the SFZ conversion drops the stereo question for an all mono set", async () => {
    await openDisk();
    const sfz = new TextEncoder().encode("<region> sample=kick.wav\n");
    pickFiles([new File([sfz], "kit.sfz"), new File([wavHeader(1)], "kick.wav")]);

    await screen.findByText("SFZ conversion");
    expect(screen.queryByRole("radiogroup", { name: "stereo handling" })).toBeNull();
    // A mono set still sends an answer the core accepts, because
    // parseChannel refuses anything outside the three.
    fireEvent.click(screen.getByRole("button", { name: "Two disk split" }));
    await waitFor(() => {
      expect(fakeCalls.sfzChannel).toBe("mix");
    });
  });

  it("material with no disk open asks for a disk label first (R7)", async () => {
    render(<App core={createFakeCore()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Browse" }));
    pickFiles([new File([bytes(6)], "SOLO.fzv")]);

    // The new disk dialog runs first, then the import continues.
    fireEvent.click(await screen.findByRole("button", { name: "Create" }));
    await waitFor(() => {
      const statuses = screen.getAllByRole("status").map((s) => s.textContent);
      expect(statuses.join(" ")).toContain("SOLO.fzv joined the voice list");
    });
  });

  it("an image over a dirty disk prompts to switch (R3)", async () => {
    await openInstrumentDisk();
    const field = screen.getByLabelText("loop 1 start");
    fireEvent.change(field, { target: { value: "7" } });
    fireEvent.blur(field);
    await screen.findByText("●");
    pickFiles([new File([new Uint8Array(IMAGE_SIZE)], "OTHER.img")]);

    await screen.findByText("Unexported changes");
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    await screen.findByText("[OPENED]");
  });
});

describe("split pairs in the shell (R5)", () => {
  const half = (marker: number, name: string) => {
    const data = new Uint8Array(IMAGE_SIZE);
    data[0] = marker;
    return new File([data], name);
  };

  it("two images open together as one two disk instrument", async () => {
    render(<App core={createFakeCore()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Browse" }));
    pickFiles([half(2, "b.img"), half(1, "a.img")]);
    await screen.findByText("[PAIR]");
    await screen.findByText(/two disk set/);
  });

  it("a lone half banners its missing twin, and the twin completes it", async () => {
    render(<App core={createFakeCore()} />);
    fireEvent.click(await screen.findByRole("button", { name: "Browse" }));
    pickFiles([half(1, "a.img")]);

    const banner = await screen.findByRole("alert", { name: "missing disk" });
    expect(banner.textContent).toContain("disk 2");
    expect(screen.getByRole("button", { name: "Open disk 2" }).textContent).not.toMatch(/…$/);
    fireEvent.change(screen.getByLabelText("second disk file"), {
      target: { files: [half(2, "b.img")] },
    });

    await screen.findByText("[PAIR]");
    await screen.findByText(/two disk set/);
  });
});

describe("sidebar file actions", () => {
  it("deletes the instrument through the confirm dialog", async () => {
    await openInstrumentDisk();
    const row = screen.getByRole("button", { name: /full/ });
    fireEvent.contextMenu(row);

    await screen.findByText("Delete the instrument?");
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await screen.findByText("No instrument on this disk");
  });

  it("creates a new empty instrument from the empty state (R4)", async () => {
    await openInstrumentDisk();
    fireEvent.contextMenu(screen.getByRole("button", { name: /full/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    // The empty state and the sidebar both offer it; either works.
    const buttons = await screen.findAllByRole("button", { name: "New empty instrument" });
    fireEvent.click(buttons[0] as HTMLElement);
    await screen.findByText("Voices (0/64)");
  });

  // The instrument row's Enter switches tab, and a context menu is a
  // pointer gesture, so deleting it was pointer-only (Q5).
  it("deletes a file from the keyboard as well as the pointer (Q5)", async () => {
    await openInstrumentDisk();
    const row = screen.getByRole("button", { name: /full/ });
    row.focus();
    fireEvent.keyDown(row, { key: "Delete" });

    await screen.findByText("Delete the instrument?");
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await screen.findByText("No instrument on this disk");
  });

  // R26: the core stitches a split dump back together for extractFile,
  // and until now nothing in the UI called it for the full dump.
  it("exports the instrument dump as .fzf (R26)", async () => {
    const inner = createFakeCore();
    const asked: string[] = [];
    const core = {
      ...inner,
      extractFile: (name: string) => {
        asked.push(name);
        return inner.extractFile(name);
      },
    };
    await openInstrumentDisk(core);

    fireEvent.click(screen.getByRole("button", { name: /Export instrument/ }));

    await screen.findByText(/exported OPENED\.fzf/);
    expect(asked).toEqual(["FULL-DATA-FZ"]);
  });
});
