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

CI also builds and tests the core without CGO. Dependency checks verify that the core does not import optional storage, authentication, session, or mail implementations. CI tests the blog's separate module against the local checkout. Compatibility with a separately pinned external application is not established by those checks.

The root commands do not include the blog. Run its tests separately:

```sh
cd examples/blog
GOWORK=off go test ./...
GOWORK=off go vet ./...
GOWORK=off go test -race ./...
```

For blog browser checks, set `HS_TEST_FIREFOX=firefox` and run `GOWORK=off go test ./internal/blog -run '^TestBrowser' -count=1` from that directory. Blog backup tests use isolated temporary data, including an application restart after manual recovery. They never delete the local blog's data.

## Browser tests

CI tests error responses, native forms, HTMX history, redirects, retargeting, reswapping, and out-of-band notifications in headless Firefox. To run them locally:

```sh
HS_TEST_FIREFOX=firefox go test -count=1 -timeout=10m ./internal/test/web -run '^TestBrowser(SiteErrors|NativeForms|ResponseDispatch|History|ErrorReporting)$'
```

HTMX tests use the vendored asset and the layout's integrity check. Ordinary Go test runs skip browser tests unless `HS_TEST_FIREFOX` is set.

### Blog accessibility

`TestBrowserAccessibility` checks skip-link focus, focus after HTMX navigation, form control names and help references, Markdown Tab handling, and image chooser focus at 320, 1100, and 3440 pixels. The wider browser suite checks responsive layouts, search focus, revision navigation, confirmations, scheduling, and reduced-motion notifications. These tests use isolated data and never reset the running blog.

Run the focused checks from `examples/blog`:

```sh
GOWORK=off HS_TEST_FIREFOX=firefox go test ./internal/blog -run '^TestBrowserAccessibility$' -count=1
```

Automated checks do not establish accessibility compliance. Before release, also check:

- Keyboard-only navigation with Tab, Shift+Tab, Enter, Space, arrow keys, and Escape. Include the editor, menus, scheduling controls, and dialogs.
- A screen reader with forms, validation errors, search results, and history previews. Confirm that labels and updates make sense when spoken.
- Browser zoom at 200% and 400%, increased text size, and high-contrast settings. Controls and focus indicators must remain visible.
- Safari on iOS and Chrome on Android with the on-screen keyboard open. Desktop tests at mobile widths do not verify touch behavior or mobile browser chrome.

## Startup diagnostics

CI saves browser diagnostics for seven days, including launch timestamps, readiness timing, [Firefox process-launch logs](https://firefox-source-docs.mozilla.org/xpcom/logging.html), and available crash reports. On Linux, it captures thread waits, page-fault counts, and process I/O counters after successful checks and on failure. On failure, it also captures live process state and probes the test server before stopping Firefox. Whole browser profiles are not uploaded. Reports can contain synthetic test data. Set `HS_TEST_BROWSER_ARTIFACTS` to a directory to retain diagnostics locally.

To investigate startup failures, manually run CI with `browser_diagnostics` enabled. The browser job runs only `TestBrowserErrorReporting`, ten times with fresh profiles. Compare the first launch with later launches: fresh profiles do not clear the operating system's file cache.

`MAJFL` counts page faults that required storage I/O. `MINFL` counts faults that did not. The [Linux I/O counter `read_bytes`](https://www.kernel.org/doc/html/latest/filesystems/proc.html#proc-pid-io-display-the-io-accounting-fields) measures bytes fetched from storage, not bytes served from the file cache. These snapshots cover the browser parent, not every child process. High first-launch values can support an I/O hypothesis, but do not identify the slow files or explain the entire delay. If the browser sends no request and the independent probe succeeds, investigate browser startup before changing application code.

Any failed attempt keeps the run failed. Firefox has 60 seconds to report JavaScript readiness, followed by a separate 15-second deadline for checks. Diagnostic runs do not replace application browser checks on pushes and pull requests.
