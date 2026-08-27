# DEMO program

`DEMO` is a Type 5 Optional Program for the FZ-1. It scrolls text across the LCD and exits when ESC is pressed.

| File | Purpose |
|---|---|
| `DEMO.asm` | Annotated NEC V50 assembly source |
| `DEMO.bin` | 1,024-byte payload used by integration tests |
| `DEMO.img` | Disk image containing the program |

Rebuild the tracked fixtures with:

```sh
make asm-tools
make demo
```

The rebuild must leave `testdata/assembly` unchanged. Standard validation doesn't require NASM.

`DEMO.asm` documents the verified ROM calls and instruction encoding. Hardware confirms its behavior.
