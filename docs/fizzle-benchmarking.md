# Benchmarking

Benchmarks live next to the code they cover as `*_bench_test.go`. Cover
the SFZ to FZF conversion, per-sample audio loops, FZV/WAV encode and
decode, and disk image read/write.

## Running

```sh
make benchmark    # all Benchmark* with -benchmem across the repo
make profile      # CPU + alloc profile of the JUNGLISM end-to-end convert
```

To target a single benchmark:

```sh
go test -run=^$ -bench=BenchmarkResample -benchmem -count=3 ./pkg/fzutil/
```

`make profile` writes `cpu.prof` and `mem.prof` (both gitignored). Open
them with `go tool pprof -http=:9999 cpu.prof`.

## Comparing before and after

For one-off changes, quote the relevant `make benchmark` deltas alongside
the change. For tighter comparison, capture into files and use
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat):

```sh
make benchmark > before.txt
# ... make change ...
make benchmark > after.txt
benchstat before.txt after.txt
```

## Byte-equality constraint

`pkg/integration/integration_test.go` holds golden SHA-256 checksums over
the conversion pipeline. Performance changes must keep the output bytes
identical; `make check` verifies this.
