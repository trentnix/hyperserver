# Performance checks

Measure the workload you intend to deploy before optimizing. Framework benchmarks use HTTP fixtures, not a running blog or network server. Keep local results and profiles in the ignored `tmp/` directory rather than committing machine-specific output.

## Framework regression checks

Run from the repository root with the Go version in [go.mod](../go.mod), CGO, and a C compiler:

```bash
set -euo pipefail
mkdir -p tmp/performance
export GOTOOLCHAIN=local GOMAXPROCS=2 CGO_ENABLED=1 GOGC=100 GOMEMLIMIT=off
{
  go version
  go test -run '^$' -bench 'Benchmark(RenderTemplates|Middleware|Session|Framework)' \
    -benchmem -benchtime=500ms -count=5 \
    ./pkg/components/content ./pkg/services/middleware ./pkg/services/session ./internal/test/web
} | tee tmp/performance/results.txt
go build -trimpath -ldflags='-s -w' -o tmp/performance/web ./cmd/web
go run ./internal/test/perfcheck -results tmp/performance/results.txt -binary tmp/performance/web
HS_TEST_PERFORMANCE=1 go test -v -count=5 ./internal/test/web -run '^TestFrameworkHeapBudget$'
```

[CI](../.github/workflows/ci.yml) runs these allocation, retained-heap, and binary-size checks. [performance-budgets.json](performance-budgets.json) contains the enforced limits and required environment. The checker requires at least five samples and compares medians. It rejects missing results, failed runs, and mismatched Go versions, operating systems, architectures, or parallelism.

CI does not enforce latency budgets. Use `perfcheck -timing` only on the recorded CPU with comparable storage and no competing workload. Review the workload and environment before changing a budget. These checks are regression guards, not production capacity guarantees.

## Blog search

Search scans current metadata and text. It is not a full-text index. SQLite checks publication visibility on every public search and orders metadata without sorting Markdown bodies. Results prefer title, tags, summary, then body matches.

The repository caches normalized bodies by content type, ID, and saved version. Metadata and uncached bodies are read in one transaction. The cache holds at most 64 MiB of text and IDs and 16,384 entries, plus map and list overhead.

Run the benchmarks with the environment above:

```sh
go test ./modules/blog -run '^$' -bench '^BenchmarkSearch$/posts=/public/(title|body|unicode|miss)/workers=1$' -benchmem -benchtime=500ms -count=5
go test ./modules/blog -run '^$' -bench '^BenchmarkSearch$/posts=/public/miss/workers=(4|16)$' -benchmem -benchtime=64x -count=5
go test ./modules/blog -run '^$' -bench '^BenchmarkSearch$/posts=/(admin|cold)/' -benchmem -benchtime=500ms -count=5
go test ./modules/blog -run '^$' -bench '^BenchmarkSearchOverflow$' -benchmem -benchtime=10x -count=5
```

Fixtures use temporary SQLite files, a one-connection pool, varied Markdown bodies, drafts, scheduled posts, nested pages, and saved revisions. Regular cases warm the cache first. Cold cases clear it before every search. The overflow case uses 20,000 posts and 2,000 pages, exceeding the cache budget.

Public benchmarks apply the handler's two-second deadline but bypass HTTP rendering and per-client admission. `ns/op` measures throughput-normalized cost. Percentiles include connection-pool waiting and timeouts. Allocation counts measure Go heap traffic, not SQLite's native memory. Short runs do not establish reliable tail latency.

Before deployment, measure mixed reading, writing, and search with cold and warm caches on the target host. Include collections larger than the cache. Search still scans uncached content and shares database capacity with other operations.

## Profiling

After creating `tmp/performance`, capture a profile of the workload you want to improve:

```sh
go test ./internal/test/web -run '^$' -bench '^BenchmarkFrameworkLoad$' -benchtime=5s \
  -cpuprofile="$PWD/tmp/performance/cpu.pprof" \
  -memprofile="$PWD/tmp/performance/heap.pprof" \
  -o tmp/performance/web.test
go tool pprof -top tmp/performance/web.test tmp/performance/cpu.pprof
go tool pprof -top -alloc_space tmp/performance/web.test tmp/performance/heap.pprof
```

Profiles can include fixture setup and benchmark calibration even when benchmark timers exclude them. Record the commit, toolchain, machine, storage, settings, and workload when comparing results. Investigate the measured bottleneck before adding a cache, dependency, or abstraction.
