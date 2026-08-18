// The placement matrix in the new frame (R6, R7): classification,
// the Radix dialogs, and the routing into core calls, driven against
// the fake core.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Channel, Core, SampleRate } from "../src/boundary/contract";
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
    // A WAV rides along so the batch passes the lone-.sfz gate; the
    // referenced sample is still absent, which is the case under test.
    pickFiles([new File([sfz], "broken.sfz"), new File([wavHeader(1)], "other.wav")]);

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

// A bare .sfz is only the instrument's recipe: the browser cannot
// read the samples it references unasked, so the shell asks for the
// folder instead of offering a conversion that must fail.
describe("a lone .sfz asks for its samples", () => {
  const PENDING_SFZ_TEXT = "<region> sample=JUNGLISM Samples/amen 01.wav\n";
  const FOLDER_SFZ_TEXT = "<region> sample=JUNGLISM Samples/amen 01.wav folder copy\n";

  function sfzFile(name = "JUNGLISM.sfz"): File {
    return new File([PENDING_SFZ_TEXT], name);
  }

  function folderFile(relativePath: string): File {
    const name = relativePath.split("/").pop() ?? relativePath;
    const content = name.toLowerCase().endsWith(".sfz")
      ? new TextEncoder().encode(FOLDER_SFZ_TEXT)
      : wavFixture(1, 18000, 100);
    const file = new File([content], name);
    Object.defineProperty(file, "webkitRelativePath", { value: relativePath });
    return file;
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function pickFolder(files: File[]) {
    fireEvent.change(screen.getByLabelText("folder"), { target: { files } });
  }

  /** A core that records the paths and sizes each conversion got. */
  function recordingCore() {
    const inner = createFakeCore();
    const sfzCallKeys: string[][] = [];
    const sfzCallSizes: Record<string, number>[] = [];
    const core: Core = {
      ...inner,
      importSfz: (files, sfzPath, rate, fit, split, channel) => {
        sfzCallKeys.push(Object.keys(files).sort());
        sfzCallSizes.push(Object.fromEntries(Object.entries(files).map(([k, v]) => [k, v.length])));
        return inner.importSfz(files, sfzPath, rate, fit, split, channel);
      },
    };
    return { core, sfzCallKeys, sfzCallSizes };
  }

  it("prompts for the folder instead of opening the conversion", async () => {
    await openDisk();
    pickFiles([sfzFile()]);
    await screen.findByText("This SFZ needs its samples");
    expect(screen.queryByText("SFZ conversion")).toBeNull();
  });

  it("joins the remembered .sfz to a picked samples folder, paths intact", async () => {
    const { core, sfzCallKeys } = recordingCore();
    await openDisk(core);
    pickFiles([sfzFile()]);
    fireEvent.click(await screen.findByRole("button", { name: "Pick folder" }));
    pickFolder([
      folderFile("JUNGLISM Samples/amen 01.wav"),
      folderFile("JUNGLISM Samples/amen 02.wav"),
    ]);

    await screen.findByText("SFZ conversion");
    fireEvent.click(screen.getByRole("button", { name: "Fit to disk (downsample)" }));
    await waitFor(() => {
      expect(sfzCallKeys).toHaveLength(1);
    });
    expect(sfzCallKeys[0]).toEqual([
      "JUNGLISM Samples/amen 01.wav",
      "JUNGLISM Samples/amen 02.wav",
      "JUNGLISM.sfz",
    ]);
  });

  it("routes a picked instrument folder the normal stripped way", async () => {
    const { core, sfzCallKeys, sfzCallSizes } = recordingCore();
    await openDisk(core);
    pickFiles([sfzFile()]);
    fireEvent.click(await screen.findByRole("button", { name: "Pick folder" }));
    pickFolder([
      folderFile("JUNGLISM/JUNGLISM.sfz"),
      folderFile("JUNGLISM/JUNGLISM Samples/amen 01.wav"),
    ]);

    await screen.findByText("SFZ conversion");
    fireEvent.click(screen.getByRole("button", { name: "Fit to disk (downsample)" }));
    await waitFor(() => {
      expect(sfzCallKeys).toHaveLength(1);
    });
    expect(sfzCallKeys[0]).toEqual(["JUNGLISM Samples/amen 01.wav", "JUNGLISM.sfz"]);
    // The folder's own .sfz converts, not the remembered one: the two
    // share a name, so the byte length is what tells them apart.
    expect(sfzCallSizes[0]?.["JUNGLISM.sfz"]).toBe(FOLDER_SFZ_TEXT.length);
  });

  it("forgets the .sfz when the OS picker is dismissed", async () => {
    const { core, sfzCallKeys } = recordingCore();
    await openDisk(core);
    pickFiles([sfzFile()]);
    fireEvent.click(await screen.findByRole("button", { name: "Pick folder" }));
    // A dismissed native picker fires cancel on the input, not change.
    fireEvent(screen.getByLabelText("folder"), new Event("cancel"));

    pickFolder([folderFile("drums/kick.wav")]);
    await screen.findByText("Import 1 WAV");
    expect(sfzCallKeys).toHaveLength(0);
  });

  it("refuses several bare .sfz files and names the way out", async () => {
    await openDisk();
    pickFiles([sfzFile("LEAD.sfz"), sfzFile("PAD.sfz")]);
    await waitFor(() => {
      const alerts = screen.getAllByRole("alert").map((a) => a.textContent);
      expect(alerts.join(" ")).toContain("import the instrument's folder");
    });
    expect(screen.queryByText("This SFZ needs its samples")).toBeNull();
  });

  it("prompts for a lone .sfz that arrives by drop", async () => {
    await openDisk();
    const entry = {
      name: "JUNGLISM.sfz",
      isFile: true,
      isDirectory: false,
      file: (cb: (f: File) => void) => {
        cb(new File([PENDING_SFZ_TEXT], "JUNGLISM.sfz"));
      },
    } as unknown as FileSystemEntry;
    const transfer = {
      items: [{ kind: "file", webkitGetAsEntry: () => entry }],
      files: [],
    } as unknown as DataTransfer;
    const app = document.querySelector(".app");
    if (!app) throw new Error("no app surface to drop on");
    fireEvent.drop(app, { dataTransfer: transfer });
    await screen.findByText("This SFZ needs its samples");
  });

  it("reports a folder whose files cannot be read instead of dropping it", async () => {
    const { core, sfzCallKeys } = recordingCore();
    await openDisk(core);
    pickFiles([sfzFile()]);
    fireEvent.click(await screen.findByRole("button", { name: "Pick folder" }));

    vi.stubGlobal(
      "FileReader",
      class {
        onerror: ((e: unknown) => void) | null = null;
        onload: (() => void) | null = null;
        error = new Error("unreadable");
        readAsArrayBuffer() {
          setTimeout(() => this.onerror?.(new Event("error")), 0);
        }
      },
    );
    pickFolder([folderFile("JUNGLISM Samples/amen 01.wav")]);
    await waitFor(() => {
      const alerts = screen.getAllByRole("alert").map((a) => a.textContent);
      expect(alerts.join(" ")).toContain("could not read the files");
    });
    expect(sfzCallKeys).toHaveLength(0);
  });

  it("forgets the .sfz on cancel, so a later folder import stays a WAV import", async () => {
    const { core, sfzCallKeys } = recordingCore();
    await openDisk(core);
    pickFiles([sfzFile()]);
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    pickFolder([folderFile("drums/kick.wav")]);

    await screen.findByText("Import 1 WAV");
    expect(sfzCallKeys).toHaveLength(0);
    expect(screen.queryByText("SFZ conversion")).toBeNull();
  });
});
