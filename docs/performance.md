# Performance checks

These benchmarks measure the local reference application, not deployment capacity. Run them before adding a cache or changing a storage or rendering policy.

## Workload and baseline

The [raw baseline](performance-baseline.txt) records five 500 ms samples per benchmark on October 15, 2026, based on `4f86ae4` plus the benchmark changes. Environment: Go 1.27.2, linux/amd64 (`GOAMD64=v1`), Linux Mint 22.3, AMD Ryzen 9 9950X3D, `GOMAXPROCS=2`, CGO enabled with GCC, and temporary SQLite files on local ext4/NVMe storage. No race detector or coverage instrumentation was enabled.

| Workload | Median time/op | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Prepared page template | 1.79 µs | 1,120 | 31 |
| Prepared fragment template | 1.26 µs | 864 | 24 |
| Security and request logging middleware | 1.75 µs | 3,024 | 40 |
| Cookie session read | 7.28 µs | 9,398 | 77 |
| SQLite session read | 12.45 µs | 9,472 | 121 |
| SQLite session save | 13.10 ms | 7,532 | 77 |
| Anonymous page / fragment | 13.66 / 10.26 µs | 27,723 / 24,547 | 159 / 120 |
| Authenticated page / fragment | 32.96 / 30.05 µs | 32,299 / 29,111 | 313 / 288 |
| Mixed concurrent requests | 19.86 µs | 28,458 | 220 |

Rendering compares per-render parsing with prepared templates using a layout, partial, and escaped input. Middleware measures a bare handler and a chain containing request logging, security headers, proxy handling, HSTS, and cross-origin protection. Session benchmarks save one 256-byte string, then measure reads, request-local reuse, and saves. SQLite writes use the provider's default durability settings. Their timing depends heavily on the filesystem.

Reference benchmarks serve `GET /` through the actual application handler with isolated SQLite accounts and sessions. They cover pages and fragments for anonymous and authenticated users. The concurrent workload splits those four cases evenly across two workers. Setup, password hashing, and schema preparation are outside benchmark timers. Requests are cloned and responses recorded inside the timers. Logging formats JSON into `io.Discard`. Mail is fake and is not exercised. The results exclude sockets, TLS, browser work, and external log delivery. Concurrent ns/op measures throughput-normalized cost, not a latency percentile.

The stripped reference binary measured 14,508,568 bytes. Across five retained-heap checks, 10,000 mixed requests changed the live Go heap by −51,856 to +26,304 bytes after warm-up and garbage collection. These checks do not measure SQLite's C allocations, RSS, or a long-running deployment.

## Run the checks

Run from the repository root with the Go version in `go.mod`. Use an otherwise idle machine for timing comparisons. Keep raw output in the ignored `tmp` directory.

```bash
set -euo pipefail
mkdir -p tmp/performance
export GOTOOLCHAIN=local GOMAXPROCS=2 CGO_ENABLED=1 GOGC=100 GOMEMLIMIT=off
{
  go version
  go test -run '^$' -bench 'Benchmark(RenderTemplates|Middleware|Session|Reference)' \
    -benchmem -benchtime=500ms -count=5 \
    ./pkg/components/content ./pkg/services/middleware ./pkg/services/session ./cmd/web
} | tee tmp/performance/results.txt
go build -trimpath -ldflags='-s -w' -o tmp/performance/web ./cmd/web
go run ./internal/test/perfcheck -results tmp/performance/results.txt -binary tmp/performance/web
HS_TEST_PERFORMANCE=1 go test -v -count=5 ./cmd/web -run '^TestReferenceHeapBudget$'
```

[Budgets](performance-budgets.json) allow 25% above baseline medians for allocations and bytes allocated, rounded up. At least five samples are required. The checker reports each range and uses the median so one noisy sample cannot fail the run. It rejects missing benchmarks, malformed metrics, failed runs, and mismatched Go version, OS, architecture, or parallelism. CI checks these budgets, a stripped binary limit of 18,000,000 bytes, and a retained-heap growth limit of 1 MiB after 10,000 requests.

Latency budgets allow 50% above the baseline median. Add `-timing` to `perfcheck` only on comparable storage and otherwise idle hardware. The checker requires the recorded CPU model, but cannot verify equivalent storage, CPU scheduling, or background load. CI does not enforce timing. Investigate a failure before changing a budget. Remeasure and review the baseline when the toolchain, workload, or intended behavior changes. These are coarse regression guards, not service-level objectives.

## Profile before optimizing

```bash
go test ./cmd/web -run '^$' -bench '^BenchmarkReferenceLoad$' -benchtime=5s \
  -cpuprofile="$PWD/tmp/performance/cpu.pprof" \
  -memprofile="$PWD/tmp/performance/heap.pprof" \
  -o "$PWD/tmp/performance/web.test"
go tool pprof -top tmp/performance/web.test tmp/performance/cpu.pprof
go tool pprof -top -alloc_space tmp/performance/web.test tmp/performance/heap.pprof
```

Use the same environment as the benchmark command. Profiles include fixture setup and benchmark calibration even though benchmark timers exclude setup. The local five-second mixed-load profile showed SQLite/CGO calls, allocation/garbage collection, and rendering work. It did not establish an expensive reusable application result that needs caching. The local Go 1.27.2 installation lacked `go tool pprof`, so its profiles were inspected using `cmd/pprof` from the already-installed Go 1.27.1 toolchain. No project dependency was added.

## Cache decision

Do not add a general cache yet. Parsed templates are already reused, and sessions are reused within a request. The reference workload does not demonstrate another consumer with a measurable cache benefit and a defined freshness policy. Adding account caching would also require preserving prompt session revocation and account changes. No runtime optimization or cache-related API change is part of this baseline work.

Defer the bounded cache, no-op implementation, cache hit/miss/eviction/error metrics, and cache integration contracts together. When a consumer demonstrates a need, define its keys, user or tenant isolation, expiration, invalidation, concurrent misses, and failure behavior before implementation. Test mutations and failures against cached reads. Distributed caching remains deferred.

The local application sends `Cache-Control: no-store` for rendered pages and fragments and varies by `HX-Request` and `HX-History-Restore-Request`. Tests cover separate users, account changes and revoked sessions, public page/fragment opt-in, history restores, errors, and static-asset separation. There is no configured local proxy/CDN cache to test. Before deployment, verify that the actual cache honors these headers, includes both HTMX headers in cache selection for public content, and never shares personalized responses. Header tests alone do not verify a deployment cache.
