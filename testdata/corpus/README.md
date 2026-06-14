# Casio FZ-1 corpus

Real-world FZ-1 sampler files used as test fixtures, downloaded from
https://zine.r-massive.com/casio-fz-sampler-archive/.

## Archives

| Directory                                    | Contents                                                   |
|----------------------------------------------|------------------------------------------------------------|
| `casio-fz-1-factory-library/`                | Official Casio factory disks (FL-1 through FL-14, plus FL-A and FL-B). |
| `casio-fz-1-shareware-library-fzf-format/`   | Shareware archive, CASIO001..CASIO142.                     |
| `casio-fz1-soundwaves/`                      | Casio Soundwaves Library, organised by instrument family.  |

Directory names were lowercased and hyphenated on import (so "Casio FZ-1
Factory Library" becomes `casio-fz-1-factory-library/`). Filenames keep
their original case for the extension (`.FZF` / `.FZV`); spaces in the
original filenames were replaced with hyphens, with runs collapsed to a
single hyphen. PDFs, JPGs, and TXT catalog listings that accompanied the
original downloads were stripped. Only playable FZ-1 binaries remain.

## File-type corrections

Nineteen files in the shareware archive were originally distributed with a
`.FZF` extension but are actually FZV (single-voice) files, not FZF (full
dumps). They were renamed from `.FZF` to `.FZV` on import so the tooling
routes them to the correct command (`fizzle fzv info`).
