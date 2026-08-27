# Casio FZ-1 corpus

This directory contains the hardware corpus used by fizzle's parser, layout, mutation, and snapshot tests.

Fixtures originate from the [Casio FZ sampler archive](https://zine.r-massive.com/casio-fz-sampler-archive/).

| Directory | Contents |
|---|---|
| `casio-fz-1-factory-library/` | Casio factory disks |
| `casio-fz-1-shareware-library-fzf-format/` | CASIO001 through CASIO142 |
| `casio-fz1-soundwaves/` | Casio Soundwaves library |

`../layout-manifest.json` records provenance, evidence, parse context, file count, authority, and layout digest for each collection.

Keep these binary fixtures unchanged. Add new material only with its provenance and manifest record.

## Reviewing a layout change

Capture records before and after changing a parser:

```sh
scratch=$(mktemp -d)
UPDATE_LAYOUTS=true LAYOUT_SCRATCH_DIR="$scratch/before" \
  go test ./pkg/integration -run TestStandaloneCorpusLayoutManifest -count=1 -v

# Apply the parser change, then capture again.
UPDATE_LAYOUTS=true LAYOUT_SCRATCH_DIR="$scratch/after" \
  go test ./pkg/integration -run TestStandaloneCorpusLayoutManifest -count=1 -v

diff -ru "$scratch/before" "$scratch/after"
```

Review every changed fixture and field before copying new `layoutDigest` values into the manifest.
