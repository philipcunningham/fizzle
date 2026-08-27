# fizzle

`fizzle` prepares floppy disk images for Casio FZ-1, FZ-10M, and FZ-20M samplers. It converts WAV files and SFZ instruments into FZ voices, full dumps, and disk images.

Use the [browser editor](https://philipcunningham.github.io/fizzle/) or install the CLI. See the [manual](docs/fizzle-manual.md) for workflows and the generated [CLI reference](docs/cli-reference.md) for every command and flag.

> [!WARNING]
> fizzle is alpha software tested primarily on an FZ-10M and macOS. Back up instruments, disks, and samples before editing them.

## Requirements

- A Casio FZ series sampler
- A floppy emulator that accepts `.img` files, or a drive that writes compatible 3.5 inch HD floppies

## Install

Install with Go 1.26 or later:

```sh
go install github.com/philipcunningham/fizzle/cmd/fizzle@latest
```

Build from source:

```sh
git clone https://github.com/philipcunningham/fizzle
cd fizzle
make build
make install
fizzle --version
```

Set `PREFIX` to change the default `/usr/local` installation prefix. The Makefile also provides `linux`, `darwin-arm64`, `darwin-amd64`, and `windows` cross-build targets.

`fizzle completion bash|zsh|fish|pwsh` emits a completion script for the selected shell.

## CLI quickstart

Convert an SFZ instrument, inspect it, and add it to a new disk image:

```sh
fizzle sfz convert --fit-to-disk mydrums.sfz mydrums.fzf
fizzle fzf info mydrums.fzf
fizzle disk new "My Drums" mydrums.img
fizzle disk add mydrums.img mydrums.fzf
fizzle disk ls mydrums.img
```

Passing a WAV directory instead assigns its files sequentially from C2. `fizzle sfz export mydrums.fzf ./my-instrument/` exports an FZF as SFZ and WAV files.

FZ audio is mono at 36 kHz, 18 kHz, or 9 kHz. A floppy holds 1.25 MB. Use `--fit-to-disk` to reduce sample rates or `--split-disks` to use two images.

## Browser editor

The [browser editor](https://philipcunningham.github.io/fizzle/) creates and edits disk images locally. It makes no network requests after loading, but closing the tab discards unexported work.

Desktop Chrome is the supported browser. To run the editor from source:

```sh
make wasm
cd web/app
npm install
npm run dev
```

See [web/app/README.md](web/app/README.md) for front end development and [docs/architecture.md](docs/architecture.md) for system boundaries.

## References

The implementation draws on [Casio FZ-1 Data Structures](llm-wiki/sources/casio-fz1-data-structures.pdf), [fztoolkit](http://www.buchty.net/casio/), and [Jacob Vosmaer's FZ-1 research](https://github.com/jacobvosmaer/fz1). Verified format findings live in the [knowledge wiki](llm-wiki/index.md).

## License

[MIT](LICENSE)
