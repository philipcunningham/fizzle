# DEMO program

`DEMO` is a horizontal text scroller for the Casio FZ-1 sampler, shipped
as a Type-5 "Optional Program" file. It loads from the FZ-1's Optional
Software menu, prints `fizzle * fizzle * ...` across the LCD, and exits
cleanly when ESC is pressed.

The artefacts here also serve as the fixture for the
`TestCLIDiskAddProgramRoundTrip` integration test, exercising the
`fizzle disk add` Type-5 detection path end to end.

## Files

| File        | Size     | Description                                                       |
|-------------|----------|-------------------------------------------------------------------|
| `DEMO.asm`  | ~5 KB    | Annotated NEC V50 (8086-superset) assembly source.                |
| `DEMO.bin`  | 1024 B   | Assembled payload, ready to be added to a disk as a Type-5 entry. |
| `DEMO.img`  | 1.25 MB  | Fresh FZ-1 disk image with `DEMO` as the only Program file.       |

`DEMO.bin` and `DEMO.img` are checked in so the integration test runs
in CI without `nasm` installed. nasm is deterministic, so re-running
`make demo` should leave both files byte-identical to what is
committed; a clean `git status testdata/assembly/` after rebuild is
the intended state.

## Building

```
make asm-tools   # one-time, brew install nasm (macOS)
make demo        # nasm + fizzle disk new + fizzle disk add + fizzle disk ls
```

`asm-tools` is separate from `tools` so the standard build/test
workflow stays Homebrew-free. `make demo` depends on `make build`, so
it always uses a freshly compiled `fizzle` binary.

## Notes on the V50 ROM API

Reverse-engineered from the factory `OPT_SOFTWARE.img` (CKMIDI, CKPORT,
etc.) and cross-checked against Rainer Buchty's firmware disassembly.
Confirmed on real FZ-1 hardware. The relevant findings, recorded in
detail in the comments of `DEMO.asm`:

| Discovery                          | Detail                                                                                          |
|------------------------------------|-------------------------------------------------------------------------------------------------|
| Standard 14-byte preamble          | `E8 ?? ?? CB 8F 06 ?? ?? CC FF 36 ?? ?? C3`; every Program starts with this.                    |
| `int3` vs `int 3` in NASM          | The 1-byte breakpoint opcode `CC` is `int3`. NASM emits 2 bytes for `int 3`.                    |
| `cls` (function 163) is a rect-fill | `cls(xs, ys, xe, ye, c)`. Calling it with no args is a silent no-op (bounds validation rejects). |
| Anti-flicker trick                 | One full-line `print` per frame overwrites in place; cells never go blank.                      |
