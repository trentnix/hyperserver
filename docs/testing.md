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

CI saves browser diagnostics for seven days, including launch timestamps, readiness timing, [Firefox process-launch logs](https://firefox-source-docs.mozilla.org/xpcom/logging.html), and available crash reports. On Linux, it captures thread waits, page-fault counts, and process I/O counters after successful checks and on failure. On failure, it also captures live process state and probes the test server before stopping Firefox. Whole browser profiles are not uploaded. Reports can contain synthetic test data. Set `HS_TEST_BROWSER_ARTIFACTS` to a directory to retain diagnostics locally.

To investigate startup failures, manually run CI with `browser_diagnostics` enabled. The browser job runs only `TestBrowserErrorReporting`, ten times with fresh profiles. Compare the first launch with later launches: fresh profiles do not clear the operating system's file cache.

`MAJFL` counts page faults that required storage I/O. `MINFL` counts faults that did not. The [Linux I/O counter `read_bytes`](https://www.kernel.org/doc/html/latest/filesystems/proc.html#proc-pid-io-display-the-io-accounting-fields) measures bytes fetched from storage, not bytes served from the file cache. These snapshots cover the browser parent, not every child process. High first-launch values can support an I/O hypothesis, but do not identify the slow files or explain the entire delay. If the browser sends no request and the independent probe succeeds, investigate browser startup before changing application code.

Any failed attempt keeps the run failed. Startup and check deadlines remain 30 and 15 seconds. Diagnostic runs do not replace application browser checks on pushes and pull requests.

## Startup profiling

Run the manual [Firefox startup investigation](../.github/workflows/browser-startup.yml) workflow to collect operation-level evidence. It runs three modes on separate runners, with two runners per mode. Each runner launches Firefox three times with fresh profiles. Browser version and profiler-help commands run afterward to avoid warming Firefox's libraries before the first launch.

| Mode | Evidence |
| --- | --- |
| `baseline` | Unprofiled readiness and request timing |
| `profile` | Firefox startup samples and markers in `startup.json` |
| `trace` | Timestamped system calls, file paths, and call durations in `syscalls.log` |

The probe serves a synthetic page without HyperServer services. It uses screenshot mode so Firefox exits naturally and exports its profile. Interrupting Firefox in the application test harness did not export a profile in local verification. Screenshot mode is a separate workload, not a replacement for reproducing the failure in the original browser tests.

JavaScript readiness after 30 seconds still fails. A slow probe captures process state and checks server health at that threshold, then continues observing for at most 90 seconds so Firefox can finish and export evidence. If the observation limit expires, the probe stops the launcher and its process group. Later launches still run, but cannot erase an earlier failure. Normal CI deadlines and browser installation remain unchanged.

The profile mode enables [Firefox's early and main startup profilers](https://firefox-source-docs.mozilla.org/tools/profiler/code-overview.html), requests native stacks and file-I/O markers, and checks for a complete JSON file containing samples. Open the file in Firefox Profiler and compare the main thread before the first HTTP request with later launches. Marker availability depends on the browser build. External tracing records file operations independently, but syscall durations do not measure time spent resolving memory-mapped page faults. If that distinction remains unresolved, collect a Linux page-fault trace rather than inferring disk latency from a gap between syscalls.

Profiling and tracing add overhead. Compare modes across runners and use the unprofiled measurements as the timing baseline. A fresh runner is not proof of an empty host or storage cache. Change one suspected factor only after the evidence identifies it.

To run one mode locally on Linux:

```sh
HS_TEST_FIREFOX=firefox HS_TEST_BROWSER_STARTUP=profile HS_TEST_BROWSER_ARTIFACTS="$PWD/tmp/browser-startup" go test -v -count=1 -timeout=6m ./cmd/web -run '^TestBrowserStartup$'
```

Use `baseline` or `trace` for the other modes. Trace mode requires `strace` and permission to trace its child processes. Missing tools and missing profiles fail the probe. CI limits each output file to 128 MiB and retains artifacts for seven days. Profiles can contain paths, browser startup URLs, and synthetic page data. Traces omit read buffers, write calls, and process environments. Do not run this probe against real accounts or upload personal browser profiles.
