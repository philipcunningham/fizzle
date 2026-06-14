# Snapshots

Checked-in snapshots of the JSON output produced by running `fizzle`'s
"read" commands over the bundled test fixtures. Each snapshot is one
expected JSON document; the matching test parses the fixture, marshals
the result, and asserts byte equality.

## Layout

| Directory     | Source fixtures              | Tests                                                                 |
|---------------|------------------------------|-----------------------------------------------------------------------|
| `corpus/`     | `testdata/corpus/`           | `fzf info` / `fzv info` per file (254 snapshots).                     |
| `synthetic/`  | `testdata/synthetic/`        | `disk ls` per image, `fzf info`/`fzv info` per disk entry, `sfz` parse of JUNGLISM.sfz. |

Subdirectories under each mirror the source fixture's path. A snapshot's
filename keeps the source filename plus a per-command suffix
(`.fzf-info`, `.fzv-info`, `.disk-ls`, `.sfz-parse`), then a `_1.snap.json`
tail inserted by [go-snaps](https://github.com/gkampitakis/go-snaps).

## Workflow

Snapshots are powered by go-snaps's `MatchStandaloneJSON` with a custom
`Dir` / `Filename` / `Ext`. Each fixture runs as its own subtest, so a
mismatch points the failure at the offending source path.

Refresh after an intentional output change:

```
UPDATE_SNAPS=true go test ./pkg/integration/ -run TestCorpus
UPDATE_SNAPS=true go test ./pkg/integration/ -run TestSynthetic
```

Review the diff, commit. The snapshot tests are gated by `skipShort` so
they don't slow down `go test -short`, but `make test` and `make check`
exercise them on every run.
