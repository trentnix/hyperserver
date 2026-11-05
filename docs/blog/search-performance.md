# Search performance

Search preserves literal substring matching, Unicode case folding and normalization, title-first ranking, and current publication visibility. A bounded cache of normalized bodies removes repeated body reads and conversion. Search still scans current metadata and matching text. It is not a full-text index.

## Implementation

Each search reads metadata and saved versions in one SQLite transaction. It loads a Markdown body only when title, tags, and summary do not match and the cached body version is missing or outdated. The body read uses the same transaction, so a concurrent edit cannot mix metadata and body versions. Edits and restores increment the saved version. Publication time and page ancestry are checked in SQLite on every public search, even when an administrator previously cached private content.

The repository-owned cache stores normalized bodies, not query results, visibility, or rendered HTML. It retains at most 64 MiB of body text and IDs and at most 16,384 entries. Map and list overhead is additional. New bodies enter at the eviction end of the list, so a full scan larger than the cache does not displace every cached body before reuse. Hits move entries to the front. Every 32nd insertion also moves to the front, allowing new content to replace unused old entries over repeated scans.

A version mismatch expires an entry without a timer or an application-wide invalidation hook. Database replacement and manual changes outside the repository contract require stopping and restarting the application.

Normalization reuses buffers within each search. SQLite orders metadata without including Markdown bodies in the sort. Public search retains the first 50 matches per rank and returns at most 50 results overall. The SQLite pool size, schema, and dependencies are unchanged.

## Workload

[BenchmarkSearch](../../modules/blog/search_benchmark_test.go) uses temporary SQLite files with WAL and the application's one-connection pool. Fixtures contain 100, 1,000, or 10,000 posts, plus one page per ten posts. Ten percent of posts are drafts and five percent are scheduled. Pages include drafts and children of draft parents. Two saved revisions per post increase database size but are not searched.

Bodies vary from roughly 1.2 to 7 KiB and include prose, line breaks, and accented text. Queries cover frequent title matches, sparse body matches, Unicode folding (`STRASSE` matching `Straße`), and no matches. Administration benchmarks cover post and page body searches. Public results remain capped at 50 while counting all matches.

Concurrent cases issue no-match searches from 4 or 16 workers against the same pool, with the public search handler's two-second deadline. Each worker starts another request after its previous request finishes. These closed-loop stress tests bypass HTTP rendering and per-client admission. They measure admitted storage work, not complete HTTP latency or one rate-limited client's behavior. Ordinary reads and writes are not mixed into the workload.

Fixture creation, warm-up, latency-array allocation, and percentile calculation are outside benchmark timing. The regular cases retain the normalized-body cache. The cold cases clear that cache before every operation, including measured operations. Filesystem caches remain warm. Measured work includes queries, matching, result collection, worker coordination, and public-query deadline setup. Allocations measure Go heap traffic, not retained heap or SQLite's native memory. `cached-MiB` reports retained text and IDs, excluding cache overhead. `ns/op` is throughput-normalized time. Percentiles measure each operation's elapsed time, including pool waiting and deadline failures.

## Reproduce

Run from the repository root on an otherwise idle machine:

```sh
export GOMAXPROCS=2 GOGC=100 GOMEMLIMIT=off CGO_ENABLED=1 GOTOOLCHAIN=local
go test ./modules/blog -run '^$' -bench '^BenchmarkSearch$/posts=/public/(title|body|unicode|miss)/workers=1$' -benchmem -benchtime=500ms -count=3
go test ./modules/blog -run '^$' -bench '^BenchmarkSearch$/posts=/admin/' -benchmem -benchtime=500ms -count=3
go test ./modules/blog -run '^$' -bench '^BenchmarkSearch$/posts=/public/miss/workers=(4|16)$' -benchmem -benchtime=64x -count=3
go test ./modules/blog -run '^$' -bench '^BenchmarkSearch$/posts=/cold/' -benchmem -benchtime=500ms -count=3
go test ./modules/blog -run '^$' -bench '^BenchmarkSearchOverflow$' -benchmem -benchtime=10x -count=3
```

Concurrent samples use 64 operations so every requested worker can participate. Three samples provide a local comparison, not a production latency guarantee. Some large serial cases contain only two operations per sample. Their tail percentiles are not statistically useful. Increase the duration and operation count for capacity planning.

## Original measurements

The [raw results](search-benchmark-baseline.txt) were collected on November 5, 2026 with Go 1.27.3, linux/amd64, Linux 7.0.0-42-generic, an AMD Ryzen 9 9950X3D, and temporary files on local ext4/NVMe storage. `GOMAXPROCS=2`, CGO was enabled, and no race detector or coverage instrumentation was active. The source was `71b7471` plus the benchmark in this change. Filesystem and SQLite caches were warm. These measurements do not describe the deployment host.

The following values are medians across three samples. Serial public searches show approximately linear scan cost for body matches and misses:

| Posts | Title match | Body match | No match | Go allocations per miss |
| ---: | ---: | ---: | ---: | ---: |
| 100 | 0.23 ms | 2.64 ms | 2.67 ms | 2.1 MiB |
| 1,000 | 3.50 ms | 30.07 ms | 30.67 ms | 23.4 MiB |
| 10,000 | 54.14 ms | 320.67 ms | 321.80 ms | 234.9 MiB |

Unicode-match timings were close to body-match timings. Administration post searches took 2.83, 32.60, and 330.92 ms at these sizes. Administration page searches took 0.16, 2.88, and 32.59 ms for the corresponding 10, 100, and 1,000 pages.

Concurrent no-match searches expose pool contention and deadline failures:

| Posts | Workers | p95 elapsed | Mean pool wait per operation | Timed out |
| ---: | ---: | ---: | ---: | ---: |
| 100 | 4 | 24 ms | 8 ms | 0% |
| 100 | 16 | 98 ms | 35 ms | 0% |
| 1,000 | 4 | 272 ms | 87 ms | 0% |
| 1,000 | 16 | 1,121 ms | 399 ms | 0% |
| 10,000 | 4 | 2,003 ms | 841 ms | 12.5% |
| 10,000 | 16 | 2,003 ms | 1,568 ms | 65.6% |

At 10,000 posts, increasing concurrency caused requests to expire before completing the scan. The lower `ns/op` and allocation counts in those samples reflect aborted work, not faster successful searches. The 16-worker timeout rate ranged from 64.1% to 67.2% across samples.

## After optimization

A CPU and allocation profile of the original 10,000-post no-match benchmark attributed about 80% of allocated bytes to case-folding string transformations. Most remaining bytes came from SQLite-to-Go string copies. Reusable buffers alone reduced allocation but left repeated body reads and conversion on the request path. The version-aware body cache addresses both costs without changing matching semantics or introducing a migration.

The [optimized results](search-benchmark-optimized.txt) use the same machine, Go settings, fixtures, and commands on the same date. Values below are medians across three samples. The source was `71b7471` plus the initial search optimization, before the cache admission and SQL ordering changes described below.

| Posts | Warm title match | Warm body match | Warm no match | Go allocations per warm miss | Cached public bodies |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 0.14 ms | 0.25 ms | 0.27 ms | 26 KiB | 0.35 MiB |
| 1,000 | 1.35 ms | 2.76 ms | 2.96 ms | 229 KiB | 3.82 MiB |
| 10,000 | 24.05 ms | 41.38 ms | 43.53 ms | 2.20 MiB | 38.35 MiB |

At 10,000 posts, warm no-match allocation fell by about 99% and elapsed time fell by about 86%. Unicode-match timings remain close to body matches. Administration post searches took 0.26, 2.94, and 34.09 ms. Page searches took 0.03, 0.29, and 3.23 ms. Caching all administrator-visible post and page bodies retained 45.14 MiB in the largest fixture.

| Posts | Workers | Optimized p95 elapsed | Mean pool wait per operation | Timed out |
| ---: | ---: | ---: | ---: | ---: |
| 100 | 4 | 2 ms | 1 ms | 0% |
| 100 | 16 | 10 ms | 4 ms | 0% |
| 1,000 | 4 | 24 ms | 8 ms | 0% |
| 1,000 | 16 | 104 ms | 38 ms | 0% |
| 10,000 | 4 | 347 ms | 126 ms | 0% |
| 10,000 | 16 | 1,600 ms | 544 ms | 4.7% |

The 10,000-post, 16-worker timeout rate ranged from 0% to 4.7%. The remaining failures include waiting for the shared connection. The workload still exceeds comfortable capacity under sustained pressure, even though allocation and throughput improved substantially.

Cold no-match searches took 2.68, 29.52, and 296.10 ms and allocated 0.95, 9.50, and 94.22 MiB. The largest cold search builds the shared 38.35 MiB cache as well as reading and normalizing bodies. The cold cost occurs after restart, edits, or eviction, not once per new query string. These results do not establish capacity for unbounded collections or cold concurrent startup traffic.

## Over-capacity scans

Review found that inserting every new body at the front of the eviction list caused repeated full scans to evict cached bodies before reuse. A sequential scan of 16,385 entries produced zero hits on its second pass. The revised admission policy preserves most cached bodies while allowing the retained set to change. Regression tests cover both capacity limits and replacement of unused content.

`BenchmarkSearchOverflow` uses the same fixture distribution with 20,000 posts and 2,000 pages. Public bodies exceed the 64 MiB cache budget. The following measurements use the same machine and environment as above, with one worker, one warm-up scan, ten measured searches per sample, and three samples. Both versions use SQL metadata ordering. A temporary Go overlay restored front insertion for the comparison without changing repository files. Values are medians across samples, collected on November 5, 2026.

| Cache admission | Time per search | Go allocations per search | Retained text and IDs |
| --- | ---: | ---: | ---: |
| Every insertion at front | 634.01 ms | 185.39 MiB | 64.00 MiB |
| Revised admission | 197.98 ms | 35.35 MiB | 64.00 MiB |

The cache limit is unchanged. Uncached bodies still require reads and normalization, so larger collections remain more expensive than collections that fit entirely in memory. These serial samples do not establish concurrent capacity.

The same ten-operation, three-sample check at 10,000 posts measured 24.69 ms for title matches and 45.36 ms for warm misses after switching to SQL metadata ordering. Those results remain close to the initial optimization while removing the Go comparator and sorted-insertion code.

Before supporting substantially larger collections or sustained high search traffic, measure mixed reading, writing, and search on the deployment host. An indexed substring-search design may then be warranted. Token-based full-text search would change semantics and must not silently replace substring matching.
