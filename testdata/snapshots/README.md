# Snapshots

These files pin JSON projections of fixtures under `testdata/corpus` and `testdata/synthetic`. Their directory structure mirrors each source path.

Refresh snapshots only after reviewing an intentional output change:

```sh
UPDATE_SNAPS=true go test ./pkg/integration/ -run TestCorpus
UPDATE_SNAPS=true go test ./pkg/integration/ -run TestSynthetic
```

Review every changed field before committing. `make test` and `make check` exercise snapshots; short tests skip them.
