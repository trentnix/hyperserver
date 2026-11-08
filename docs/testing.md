# Testing and browser diagnostics

Run commands from the repository root with the Go version in [go.mod](../go.mod). SQLite and race tests require CGO and a C compiler.

Unit tests live beside the code they exercise. Framework HTTP integration and browser tests live in [internal/test/web](../internal/test/web) and assemble public APIs. Blog tests live in [modules/blog](../modules/blog). Command setup tests stay in `cmd/web`. Shared account and session contract suites live beside their service contracts.

Run:

```sh
go test ./...
go vet ./...
go test -race ./...
```

The [CI workflow](../.github/workflows/ci.yml) runs these checks on pushes and pull requests. Browser checks run in an independent job, so a browser failure does not prevent race checks. All required jobs must pass. See [AGENTS.md](../AGENTS.md) for working agreements and regression-test guidance.

[Container recovery and soak tests](deployment.md#start-and-test) are opt-in. Ordinary Go test runs and CI skip them. Never run failure/recovery tests alongside a soak test or against real data.

CI also builds and tests the core without CGO. Dependency checks verify that the core does not import optional storage, authentication, session, or mail implementations. Compatibility with a separately pinned external application is not established by those checks.

The root commands include the blog. For blog browser checks, run:

```sh
HS_TEST_FIREFOX=firefox go test ./modules/blog -run '^TestBrowser' -count=1 -timeout=15m
```

Blog backup tests use isolated temporary data, including an application restart after manual recovery. They never delete the local blog's data.

## Framework fixtures

- [HTTP fixtures](../internal/test/web/module_fixture_test.go) retain ordinary and HTMX forms, error responses, notifications, authenticated navigation, and optional authentication. Templates and browser assets live under `internal/test/web/testdata`.
- [Provider replacement](../internal/test/web/provider_replacement_test.go) injects account, session, mail, and form-storage doubles through public APIs. [Independent storage](../internal/test/web/storage_test.go) checks failure isolation.
- [Application tests](../pkg/application) cover optional services, module selection, startup failure cleanup, readiness, and borrowed-resource ownership.
- [Lifecycle integration tests](../pkg/application/run_drain_test.go) cover HTTP/TLS draining, drain deadlines, and resource cleanup. [Shared-pool tests](../pkg/handlers/storage_initialization_test.go) cover dependency order and native queries.
- Account and session packages cover provider contracts and migration behavior. Set `HS_TEST_MIGRATE` to the pinned migration CLI to include its integration tests. See [storage maintenance](storage.md#account-schema-migrations).

## Browser tests

CI tests error responses, native forms, HTMX history, redirects, retargeting, reswapping, and out-of-band notifications in headless Firefox. To run them locally:

```sh
HS_TEST_FIREFOX=firefox go test -count=1 -timeout=10m ./internal/test/web -run '^TestBrowser(SiteErrors|NativeForms|ResponseDispatch|History|ErrorReporting)$'
```

HTMX tests use the vendored asset and the layout's integrity check. Ordinary Go test runs skip browser tests unless `HS_TEST_FIREFOX` is set.

### Blog accessibility

`TestBrowserAccessibility` checks skip-link focus, focus after HTMX navigation, form control names and help references, Markdown Tab handling, and image chooser focus at 320, 1100, and 3440 pixels. The wider browser suite checks responsive layouts, search focus, revision navigation, confirmations, scheduling, and reduced-motion notifications. These tests use isolated data and never reset the running blog.

Run the focused checks from the repository root:

```sh
HS_TEST_FIREFOX=firefox go test ./modules/blog -run '^TestBrowserAccessibility$' -count=1
```

Automated checks do not establish accessibility compliance. Before release, also check:

- Keyboard-only navigation with Tab, Shift+Tab, Enter, Space, arrow keys, and Escape. Include the editor, menus, scheduling controls, and dialogs.
- A screen reader with forms, validation errors, search results, and history previews. Confirm that labels and updates make sense when spoken.
- Browser zoom at 200% and 400%, increased text size, and high-contrast settings. Controls and focus indicators must remain visible.
- Safari on iOS and Chrome on Android with the on-screen keyboard open. Desktop tests at mobile widths do not verify touch behavior or mobile browser chrome.

## Startup diagnostics

CI retains browser startup logs and available crash reports for seven days, but does not upload whole profiles. Set `HS_TEST_BROWSER_ARTIFACTS` to a directory to retain diagnostics locally. Reports can contain synthetic test data.

To investigate startup failures, manually run CI with `browser_diagnostics` enabled. The browser job runs only `TestBrowserErrorReporting`, ten times with fresh profiles. Compare the first launch with later launches: fresh profiles do not clear the operating system's file cache.

If Firefox sends no request and the independent server probe succeeds, investigate browser startup before changing application code.

Any failed attempt keeps the run failed. Firefox has 60 seconds to report JavaScript readiness, followed by a separate 15-second deadline for checks. Diagnostic runs do not replace application browser checks on pushes and pull requests.
