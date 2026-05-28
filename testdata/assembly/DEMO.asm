; DEMO: scrolling text demo for the Casio FZ-1.
; Prints "fizzle * fizzle * ..." across line 4 of the LCD, scrolls
; right-to-left, exits on ESC. Loads as a Type-5 program from the
; Optional Software menu.
;
;   nasm -f bin DEMO.asm -o DEMO.bin
;   fizzle disk add IMAGE DEMO.bin
;
; See README.md for context. ROM-API quirks are noted at the relevant sites.

        cpu     186             ; V50 is 80186-superset; factory C used ENTER/PUSH-imm
        bits    16
        org     0x6000          ; firmware FAR-CALLs us here

; --- Constants ------------------------------------------------------------

SCRATCH         equ     0x55F6  ; one-word slot inside the FZ-1 work area
KEY_ESC         equ     21      ; front-panel ESC key code

; ROM functions (BRK 3 dispatch, cdecl, args right-to-left).
FN_MGETC        equ     6       ; mgetc() -> BX (-1 if no key queued)
FN_PRINT        equ     162     ; print(c, l, b, s) - column, line, bg, far ptr
FN_CLS          equ     163     ; cls(xs, ys, xe, ye, c) - rect-fill, see fz1os.asm:15959

LCD_W_MAX       equ     95      ; pixel width  - 1 (LCD is 96 px wide)
LCD_H_MAX       equ     63      ; pixel height - 1 (LCD is 64 px tall)

TEXT_LINE       equ     4
VIEW_W          equ     16
DELAY_OUTER     equ     1       ; ~150-220 ms per tick at ~5-8 MHz

; --- Preamble -------------------------------------------------------------
;
; Fixed 14 bytes every Type-5 program starts with. The firmware FAR-CALLs
; us at 6000h; the preamble routes main's eventual near-RET back through
; 6003 (RETF) to the firmware.
;
;   6000  E8 ?? ??     call near to main
;   6003  CB           retf back to firmware
;   6004  8F 06 F6 55  ROM-call trampoline; we don't use it but the
;   6008  CC             firmware checks the bytes match factory programs
;   6009  FF 36 F6 55
;   600D  C3
;
; NASM gotcha: use `int3` (1 byte CC), not `int 3` (2 bytes CD 03), or the
; offsets slide.

entry:
        call    main
        retf
        pop     word [SCRATCH]
        int3
        push    word [SCRATCH]
        ret

; --- main -----------------------------------------------------------------

main:
        push    cs
        pop     ds
        push    cs
        pop     es

        ; Clear the whole LCD to a known blank canvas. cls is a rect-fill
        ; cls(xs, ys, xe, ye, c) per fz1os.asm:15959. Calling it with no
        ; args (as an earlier draft did) is a silent no-op because the
        ; firmware's bounds validation rejects the garbage it reads off
        ; the stack.
        push    word 0                  ; c  = 0 (clear)
        push    word LCD_H_MAX          ; ye = 63
        push    word LCD_W_MAX          ; xe = 95
        push    word 0                  ; ys = 0
        push    word 0                  ; xs = 0
        push    word FN_CLS
        int3
        add     sp, 12

        mov     word [scroll], 0

.frame:
        push    word FN_MGETC
        int3
        add     sp, 2
        cmp     bx, KEY_ESC
        je      .exit

        ; Build a 16-char window into the cyclically extended text.
        mov     word [tmp], 0
.build:
        mov     ax, [scroll]
        add     ax, [tmp]
        xor     dx, dx
        mov     bx, TEXT_LEN
        div     bx              ; dx = (scroll + tmp) mod TEXT_LEN
        mov     bx, dx
        mov     al, [text + bx]
        mov     bx, [tmp]
        mov     [view_buffer + bx], al
        inc     word [tmp]
        cmp     word [tmp], VIEW_W
        jb      .build

        ; One print() per frame, overwriting in place. Clearing the line
        ; first showed flicker on the slow STN LCD.
        push    cs
        mov     bx, view_buffer
        push    bx
        push    word 0
        push    word TEXT_LINE
        push    word 0
        push    word FN_PRINT
        int3
        add     sp, 12

        mov     dx, DELAY_OUTER
.delay_outer:
        xor     cx, cx          ; LOOP wraps 0 to 65536
.delay_inner:
        loop    .delay_inner
        dec     dx
        jnz     .delay_outer

        inc     word [scroll]
        jmp     .frame

.exit:
        push    word 0                  ; c  = 0 (clear)
        push    word LCD_H_MAX          ; ye = 63
        push    word LCD_W_MAX          ; xe = 95
        push    word 0                  ; ys = 0
        push    word 0                  ; xs = 0
        push    word FN_CLS
        int3
        add     sp, 12
        ret                     ; falls into the preamble's RETF

; --- Data -----------------------------------------------------------------

text:
        db      "fizzle * "
text_end:
TEXT_LEN        equ     text_end - text

scroll:         dw      0
tmp:            dw      0
view_buffer:    times 17 db 0   ; 16 chars + null

        ; Pad to a 1024-byte FZ-1 cluster.
        times 1024 - ($ - $$) db 0
