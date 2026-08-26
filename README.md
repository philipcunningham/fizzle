# fizzle

`fizzle` loads samples onto **Casio FZ series samplers** (FZ-1, FZ-10M, FZ-20M) via floppy disk images.

The FZ series are 16-bit samplers from the late 1980s, still used in jungle and experimental music for their distinctive sound. fizzle turns modern material (WAV files, SFZ instruments) into the voices, full dumps, and disk images the sampler reads. It doesn't emulate an FZ. You bring the sampler and a floppy emulator; fizzle prepares what goes on the disk.

fizzle has two front ends over the same core: a command-line tool and a [browser editor](#browser-editor). The editor is live at [philipcunningham.github.io/fizzle](https://philipcunningham.github.io/fizzle/). The releases carry the CLI.

[docs/fizzle-manual.md](docs/fizzle-manual.md) is the full reference. It covers every flag and where each parameter lives on the sampler. This README is the quickstart.

> [!IMPORTANT]
> **Status: alpha.** `fizzle` is young and under active development. Validation so far is hands-on: my own daily workflow on a Casio FZ-10M. The full FZ series and a wide spread of setups remain untested. Development and testing happen on macOS, so the Linux and Windows builds are unexercised on their own platforms. Expect the occasional rough edge, and treat results on unfamiliar material with healthy curiosity.
>
> fizzle writes the disk images your sampler loads. **Keep backups of your instruments, disks, and samples** before destructive edits, and try a converted disk on non-essential data first. Reports of what worked, what didn't, and on which hardware are what help fizzle mature.

---

## What you need

- A **Casio FZ-1, FZ-10M, or FZ-20M** sampler
- A floppy emulator that accepts `.img` disk image files
- A way to copy the `.img` file to a USB stick (any file copy works)

---

## Install

**With Go installed** (requires Go 1.26+):

```sh
go install github.com/philipcunningham/fizzle/cmd/fizzle@latest
```

**Build from source:**

```sh
git clone https://github.com/philipcunningham/fizzle
cd fizzle
make build
make install                    # copies to /usr/local/bin
# make install PREFIX=~/.local  # or install to a custom location
fizzle --version                # verify the install
```

Or build for a specific platform:

```sh
make linux          # fizzle-linux-amd64
make darwin-arm64   # fizzle-darwin-arm64 (Apple Silicon)
make darwin-amd64   # fizzle-darwin-amd64 (Intel Mac)
make windows        # fizzle-windows-amd64.exe
```

**Shell completion** (bash, zsh, fish, pwsh):

```sh
source <(fizzle completion bash)   # bash: add to .bashrc
source <(fizzle completion zsh)    # zsh: add to .zshrc
fizzle completion fish > ~/.config/fish/completions/fizzle.fish  # fish
```

---

## CLI quickstart

Turn a folder of WAVs (or an SFZ instrument) into a disk image the sampler can load.

```sh
# 1. Convert an SFZ instrument to a full dump.
#    --fit-to-disk drops the sample rate when the material doesn't fit a floppy.
fizzle sfz convert --fit-to-disk mydrums.sfz mydrums.fzf

# 2. Check the key assignments and durations.
fizzle fzf info mydrums.fzf

# 3. Create a blank 1.25 MB disk image.
fizzle disk new "My Drums" mydrums.img

# 4. Put the dump on the disk, then verify it.
fizzle disk add mydrums.img mydrums.fzf
fizzle disk ls mydrums.img

# 5. Copy mydrums.img to your floppy emulator and load it on the sampler.
```

Pass a directory of WAVs instead of an SFZ file and each WAV takes a sequential key from C2 upward. To go the other way, `fizzle sfz export mydrums.fzf ./my-instrument/` writes an SFZ instrument plus its WAVs.

Long conversions respect Ctrl+C. Cancel a running `sfz convert` and the command exits cleanly, with no half-written file left behind. Progress goes to stderr at INFO level. Add `--debug` for per-file detail:

```sh
fizzle --debug sfz convert junglism.sfz junglism.fzf
```

The manual's [Quickstart walkthroughs](docs/fizzle-manual.md#quickstart-walkthroughs) chapter goes further. It covers importing a single WAV, editing a voice in place, splitting a large instrument across two floppies, and round-tripping a hardware FZF.

The generated [CLI reference](docs/cli-reference.md) lists every command, argument, and flag from the same metadata used by the executable.

---

## Browser editor

**[philipcunningham.github.io/fizzle](https://philipcunningham.github.io/fizzle/)** opens the editor. Nothing to install.

It is the same fizzle core compiled to WebAssembly, behind a React front end. Create a disk or open an `.img` file, import WAVs or an SFZ instrument as voices, edit them, then export the image for your floppy emulator. Voices preview in the tab through Web Audio.

**Your files stay on your machine.** The page loads its assets and then makes no network requests, so your samples and disk images never reach a server. There is no account, no upload, and no telemetry. Closing the tab discards the open disk, so export before you leave.

**Use Chrome.** Desktop Chromium browsers are the supported target, and Chrome is where fizzle is tested. Other browsers and mobile are out of scope, and the editor says so when it can't rely on what it needs.

To run it from source, against your own build of the core:

```sh
make wasm          # build the browser core
cd web/app
npm install
npm run dev        # then open the printed localhost URL
```

For the front end layout and its checks see [web/app/README.md](web/app/README.md).
The [architecture guide](docs/architecture.md) records domain ownership,
dependency rules, and their executable evidence.

---

## Good to know

- **Mono only.** The FZ series records and plays mono; the hardware has no stereo path. The CLI rejects stereo WAVs, and the browser editor asks which side to keep.
- **Three sample rates.** 36 kHz, 18 kHz, and 9 kHz. Higher rates sound better and cost more memory and disk space.
- **1.25 MB per floppy.** Large libraries at 36 kHz overflow it. `--fit-to-disk` downsamples to fit, and `--split-disks` spreads one instrument over two disks.
- **`.fzv` holds one voice, `.fzf` up to 64.** A full dump also carries the bank mapping that decides which keys play which samples, so `.fzf` is what you usually want on the disk.
- **Real floppies work too.** Write the image to a 3.5" HD floppy with `dd if=mydrums.img of=/dev/rdisk2 bs=512` on macOS or Linux. The FZ-1 uses a standard IBM-compatible format.
- **Hardware disks read back.** fizzle reads real FZF files, multi-bank full dumps included (up to 8 banks). `disk get` extracts a file from a disk image, and `fzf unpack` splits it into voice files.

---

## Acknowledgements

The [Casio FZ-1 Data Structures](llm-wiki/sources/casio-fz1-data-structures.pdf) document (T. Sasaki, Casio R&D, 1987) is the primary reference for the disk and voice formats implemented here. Corrections and real-world findings distilled from it live in the [llm-wiki](llm-wiki/index.md).

Rainer Buchty's [fztoolkit](http://www.buchty.net/casio/) (2000) is a set of C utilities for reading and writing FZ-1 disks directly from a Linux floppy drive. Its `voice_data` and `bank_data` struct layouts were a useful cross-check against the spec while implementing fizzle's voice and bank parsers. His firmware disassembly also informed the V50 ROM API notes in `testdata/assembly/DEMO.asm`.

[Jacob Vosmaer's fz1 project](https://github.com/jacobvosmaer/fz1) and his [write-up on FZ-1 disk images](https://blog.jacobvosmaer.nl/0057-fz-1-images/) (2025) were valuable references. They settled the file head layout number correction and the heuristic for reconstructing layout numbers from FZF files found online.

The [Undecyclenate FZ Editor and Librarian](https://undecyclenate.neocities.org/manual) is prior art that scratched a similar itch on Windows XP. Its manual was a useful reference while structuring `docs/fizzle-manual.md`.

## License

[MIT](LICENSE)
