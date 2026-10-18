# Testing and browser diagnostics

Run commands from the repository root with the Go version in [go.mod](../go.mod). SQLite and race tests require CGO and a C compiler.

Unit tests live beside the code they exercise. HTTP integration and browser tests live in [internal/test/web](../internal/test/web) and assemble public framework APIs. Command setup and lifecycle tests stay in `cmd/web`. Shared storage contract helpers live in [internal/test](../internal/test).

Run:

```sh
go test ./...
go vet ./...
go test -race ./...
```

The [CI workflow](../.github/workflows/ci.yml) runs these checks on pushes and pull requests. Browser checks run in an independent job, so a browser failure does not prevent race checks. Both jobs must pass. See [AGENTS.md](../AGENTS.md) for working agreements and regression-test guidance.

[Container recovery and soak tests](deployment.md#start-and-test) are opt-in. Ordinary Go test runs and CI skip them. Never run failure/recovery tests alongside a soak test or against real data.

The [external-module example](../examples/external/readme.md) has a separate `go.mod`. The root suite copies it outside the repository and tests it against the current checkout. To test its pinned public dependency instead, run `go test ./...` from `examples/external`.

## Browser tests

CI tests error responses, native forms, HTMX history, redirects, retargeting, reswapping, and out-of-band notifications in headless Firefox. To run them locally:

```sh
HS_TEST_FIREFOX=firefox go test -count=1 -timeout=10m ./internal/test/web -run '^TestBrowser(SiteErrors|NativeForms|ResponseDispatch|History|ErrorReporting)$'
```

HTMX tests use the vendored asset and the layout's integrity check. Ordinary Go test runs skip browser tests unless `HS_TEST_FIREFOX` is set.

## Startup diagnostics

CI saves browser diagnostics for seven days, including launch timestamps, readiness timing, [Firefox process-launch logs](https://firefox-source-docs.mozilla.org/xpcom/logging.html), and available crash reports. On Linux, it captures thread waits, page-fault counts, and process I/O counters after successful checks and on failure. On failure, it also captures live process state and probes the test server before stopping Firefox. Whole browser profiles are not uploaded. Reports can contain synthetic test data. Set `HS_TEST_BROWSER_ARTIFACTS` to a directory to retain diagnostics locally.

To investigate startup failures, manually run CI with `browser_diagnostics` enabled. The browser job runs only `TestBrowserErrorReporting`, ten times with fresh profiles. Compare the first launch with later launches: fresh profiles do not clear the operating system's file cache.

`MAJFL` counts page faults that required storage I/O. `MINFL` counts faults that did not. The [Linux I/O counter `read_bytes`](https://www.kernel.org/doc/html/latest/filesystems/proc.html#proc-pid-io-display-the-io-accounting-fields) measures bytes fetched from storage, not bytes served from the file cache. These snapshots cover the browser parent, not every child process. High first-launch values can support an I/O hypothesis, but do not identify the slow files or explain the entire delay. If the browser sends no request and the independent probe succeeds, investigate browser startup before changing application code.

Any failed attempt keeps the run failed. Firefox has 60 seconds to report JavaScript readiness, followed by a separate 15-second deadline for checks. Diagnostic runs do not replace application browser checks on pushes and pull requests.
