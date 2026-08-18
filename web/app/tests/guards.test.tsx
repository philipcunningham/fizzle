// The document guards: the tab close warning (R3), the undo hotkey
// leaving text fields alone, and a dropped folder behaving like the
// folder picker (R6).
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createFakeCore } from "../src/core/fake";
import { App } from "../src/shell/App";
import { dropEntries, walkEntries } from "../src/shell/drop";
import { openDisk, pickFiles } from "./helpers";

/** Fires beforeunload and reports whether anything asked to stop it. */
function closeTab(): boolean {
  const e = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(e);
  return e.defaultPrevented;
}

describe("unexported changes guard the tab close (R3)", () => {
  it("stays quiet on a clean document", async () => {
    render(<App core={createFakeCore()} />);
    await screen.findByRole("button", { name: "New disk" });
    expect(closeTab()).toBe(false);
  });

  it("warns once the document is dirty", async () => {
    await openDisk();
    pickFiles([new File([new Uint8Array(64)], "kick.wav")]);
    fireEvent.click(await screen.findByRole("button", { name: "Convert" }));
    await screen.findByText(/Voices \(1\/64\)/);
    await waitFor(() => {
      expect(closeTab()).toBe(true);
    });
  });
});

describe("the undo hotkey leaves text fields alone", () => {
  /** An app whose core counts the undos the hotkey asks for. */
  async function appCountingUndos() {
    const core = createFakeCore();
    const calls = { undo: 0 };
    const counted = {
      ...core,
      undo: () => {
        calls.undo += 1;
        return core.undo();
      },
    };
    render(<App core={counted} />);
    fireEvent.click(await screen.findByRole("button", { name: "New disk" }));
    fireEvent.change(await screen.findByLabelText("disk label"), {
      target: { value: "MY DISK" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByText("[MY DISK]");
    pickFiles([new File([new Uint8Array(64)], "kick.wav")]);
    fireEvent.click(await screen.findByRole("button", { name: "Convert" }));
    await screen.findByText(/Voices \(1\/64\)/);
    return calls;
  }

  it("undoes when the focus is not in a field", async () => {
    const calls = await appCountingUndos();
    fireEvent.keyDown(document.body, { key: "z", metaKey: true });
    expect(calls.undo).toBe(1);
    await screen.findByText("No instrument on this disk");
  });

  it("does not undo the document from inside a rename field", async () => {
    const calls = await appCountingUndos();
    const row = screen.getAllByRole("row").find((r) => r.textContent.includes("KICK"));
    if (!row) throw new Error("no voice row");
    fireEvent.keyDown(row, { key: "F2" });
    const field = await screen.findByLabelText("voice name");

    fireEvent.keyDown(field, { key: "z", metaKey: true });
    // The key belonged to the field, so the document never moved.
    expect(calls.undo).toBe(0);
    expect(screen.getByText(/Voices \(1\/64\)/)).toBeTruthy();
  });
});

describe("a dropped folder walks like the picker (R6)", () => {
  // Minimal stand-ins for the entry API, which jsdom does not carry.
  const fileEntry = (name: string): FileSystemEntry =>
    ({
      name,
      isFile: true,
      isDirectory: false,
      file: (cb: (f: File) => void) => {
        cb(new File([new Uint8Array(8)], name));
      },
    }) as unknown as FileSystemEntry;

  const dirEntry = (name: string, children: FileSystemEntry[]): FileSystemEntry => {
    let handed = false;
    return {
      name,
      isFile: false,
      isDirectory: true,
      createReader: () => ({
        // The real reader hands back a batch at a time and signals the
        // end with an empty one.
        readEntries: (cb: (entries: FileSystemEntry[]) => void) => {
          cb(handed ? [] : children);
          handed = true;
        },
      }),
    } as unknown as FileSystemEntry;
  };

  // The dropped folder's own name is not in the path, because the
  // folder picker's webkitRelativePath has it stripped. Leaving it in
  // puts every WAV a level below the root the pipeline reads, and the
  // import finds no WAV files at all.
  it("walks a nested folder into the paths the picker would give", async () => {
    const tree = dirEntry("MyKit", [
      fileEntry("kit.sfz"),
      dirEntry("samples", [fileEntry("kick.wav"), fileEntry("snare.wav")]),
      fileEntry(".DS_Store"),
    ]);
    const dropped = await walkEntries([tree]);
    expect(dropped.map((d) => d.path)).toEqual([
      "kit.sfz",
      "samples/kick.wav",
      "samples/snare.wav",
    ]);
  });

  it("keeps a loose dropped file at the root", async () => {
    const dropped = await walkEntries([fileEntry("kick.wav")]);
    expect(dropped.map((d) => d.path)).toEqual(["kick.wav"]);
  });

  // A drop holding several entries is rooted at the drop point, so a
  // folder dropped beside a file keeps its name: an .sfz dragged in
  // with its samples folder references "samples/kick.wav", and that
  // path has to survive the walk.
  it("keeps a folder's name when it is dropped beside a file", async () => {
    const dropped = await walkEntries([
      fileEntry("kit.sfz"),
      dirEntry("samples", [fileEntry("kick.wav"), fileEntry("snare.wav")]),
    ]);
    expect(dropped.map((d) => d.path)).toEqual([
      "kit.sfz",
      "samples/kick.wav",
      "samples/snare.wav",
    ]);
  });

  it("keeps every folder's name when several are dropped together", async () => {
    const dropped = await walkEntries([
      dirEntry("kicks", [fileEntry("01.wav")]),
      dirEntry("snares", [fileEntry("02.wav")]),
    ]);
    expect(dropped.map((d) => d.path)).toEqual(["kicks/01.wav", "snares/02.wav"]);
  });

  it("survives a transfer whose items carry no entry API", () => {
    const transfer = { items: [{ kind: "file" }] } as unknown as DataTransfer;
    expect(dropEntries(transfer)).toEqual([]);
  });

  it("takes nothing from a transfer with no entry API", () => {
    const transfer = { items: [], files: [] } as unknown as DataTransfer;
    expect(dropEntries(transfer)).toEqual([]);
  });

  it("skips the items that are not files", () => {
    const transfer = {
      items: [{ kind: "string", webkitGetAsEntry: () => fileEntry("x.wav") }],
    } as unknown as DataTransfer;
    expect(dropEntries(transfer)).toEqual([]);
  });
});

// A real drop event through the app: the unit tests above exercise the
// walk and not the wiring, where a bad path shape still looks correct.
describe("a dropped WAV folder reaches the core the way the picker does", () => {
  it("hands the core the folder's files at its root", async () => {
    const core = createFakeCore();
    const seen: string[][] = [];
    const watched: typeof core = {
      ...core,
      importWavFolder: (files, rate, fitToDisk, channel) => {
        seen.push(Object.keys(files));
        return core.importWavFolder(files, rate, fitToDisk, channel);
      },
    };
    render(<App core={watched} />);
    fireEvent.click(await screen.findByRole("button", { name: "New disk" }));
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByText("[FZ DISK 1]");

    const wav = (name: string) =>
      ({
        name,
        isFile: true,
        isDirectory: false,
        file: (cb: (f: File) => void) => {
          cb(new File([new Uint8Array(64)], name));
        },
      }) as unknown as FileSystemEntry;
    let handed = false;
    const folder = {
      name: "MyKit",
      isFile: false,
      isDirectory: true,
      createReader: () => ({
        readEntries: (cb: (e: FileSystemEntry[]) => void) => {
          cb(handed ? [] : [wav("01 kick.wav"), wav("02 snare.wav")]);
          handed = true;
        },
      }),
    } as unknown as FileSystemEntry;

    fireEvent.drop(document.querySelector(".app") as Element, {
      dataTransfer: {
        files: [],
        items: [{ kind: "file", webkitGetAsEntry: () => folder }],
      },
    });

    const dialog = await screen.findByRole("button", { name: "Convert" });
    fireEvent.click(dialog);
    await screen.findByText(/Voices \(2\/64\)/);
    // Bare names, as the picker yields. A "MyKit/" prefix would put
    // every file below the root the conversion pipeline reads.
    expect(seen[0]).toEqual(["01 kick.wav", "02 snare.wav"]);
  });
});

// A range slider takes no typing, so it owns no undo stack. Treating
// every input alike leaves Cmd+Z dead after the waveform's zoom.
describe("the undo guard covers typing, not every input", () => {
  it("still undoes from a range slider", async () => {
    const core = createFakeCore();
    let undos = 0;
    const counted: typeof core = {
      ...core,
      undo: () => {
        undos += 1;
        return core.undo();
      },
    };
    render(<App core={counted} />);
    fireEvent.click(await screen.findByRole("button", { name: "New disk" }));
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await screen.findByText("[FZ DISK 1]");

    const range = document.createElement("input");
    range.type = "range";
    document.body.appendChild(range);
    fireEvent.keyDown(range, { key: "z", metaKey: true });
    expect(undos).toBe(1);
    range.remove();
  });
});
