# Testing and browser diagnostics

Run commands from the repository root with the Go version in [go.mod](../go.mod). SQLite and race tests require CGO and a C compiler.

Tests live beside the code they exercise. Shared storage contract helpers live in [internal/test](../internal/test) and can be used by tests throughout the repository.

Run:

```sh
go test ./...
go vet ./...
go test -race ./...
```

The [CI workflow](../.github/workflows/ci.yml) runs these checks on pushes and pull requests. Browser checks run in an independent job, so a browser failure does not prevent race checks. Both jobs must pass. See [AGENTS.md](../AGENTS.md) for working agreements and regression-test guidance.

## Browser tests

CI tests error responses, native forms, HTMX history, redirects, retargeting, reswapping, and out-of-band notifications in headless Firefox. To run them locally:

```sh
HS_TEST_FIREFOX=firefox go test -count=1 -timeout=5m ./cmd/web -run '^TestBrowser(SiteErrors|NativeForms|ResponseDispatch|History|ErrorReporting)$'
```

HTMX tests use the vendored asset and the layout's integrity check. Ordinary Go test runs skip browser tests unless `HS_TEST_FIREFOX` is set.

## Startup diagnostics

CI saves browser diagnostics for seven days, including launch timestamps, readiness timing, [Firefox process-launch logs](https://firefox-source-docs.mozilla.org/xpcom/logging.html), and available crash reports. On Linux, it captures thread waits and process I/O counters after successful checks and on failure. On failure, it also captures live process state and probes the test server before stopping Firefox. Whole browser profiles are not uploaded. Reports can contain synthetic test data. Set `HS_TEST_BROWSER_ARTIFACTS` to a directory to retain diagnostics locally.

To investigate startup failures, manually run CI with `browser_diagnostics` enabled. The browser job runs only `TestBrowserErrorReporting`, ten times with fresh profiles. Compare the first launch with later launches: fresh profiles do not clear the operating system's file cache.

Any failed attempt keeps the run failed. Startup and check deadlines remain 30 and 15 seconds. Diagnostic runs do not replace application browser checks on pushes and pull requests.
