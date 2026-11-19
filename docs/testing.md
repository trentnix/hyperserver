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

CI also builds the core and `application.New` without CGO. The [stateless fixture](../internal/test/stateless/application_test.go) covers full pages, HTMX fragments, history restoration, and HTML escaping without storage. Dependency checks keep storage implementations out of the application constructor and optional services out of the core. Compatibility with a separately pinned external application is not established by those checks.

The root commands include the blog. For blog browser checks, run:

```sh
HS_TEST_FIREFOX=firefox go test ./modules/blog -run '^TestBrowser' -count=1 -timeout=20m
```

Blog backup tests use isolated temporary data, including an application restart after manual recovery. They never delete the local blog's data.

## Framework fixtures

- [HTTP fixtures](../internal/test/web/module_fixture_test.go) retain ordinary and HTMX forms, error responses, notifications, authenticated navigation, and optional authentication. Templates and browser assets live under `internal/test/web/testdata`.
- [Provider replacement](../internal/test/web/provider_replacement_test.go) injects account, session, mail, and form-storage doubles through public APIs. [Independent storage](../internal/test/web/storage_test.go) checks failure isolation.
- [Application tests](../pkg/application) cover optional services, module selection, startup failure cleanup, readiness, and borrowed-resource ownership.
- [Lifecycle integration tests](../pkg/application/run_drain_test.go) cover HTTP/TLS draining, drain deadlines, and resource cleanup. [Shared-pool tests](../pkg/handlers/storage_initialization_test.go) cover dependency order and native queries.
- Account and session packages cover provider contracts and migration behavior. Set `HS_TEST_MIGRATE` to the pinned migration CLI to include its integration tests. See [storage maintenance](storage.md#account-schema-migrations).

## Browser tests

Blog browser tests declare scripts, starting URLs, and setup through `browserScenario` in [browser_test.go](../modules/blog/browser_test.go). [browser_scenarios_test.go](../modules/blog/browser_scenarios_test.go) seeds data and prepares scripts. [browser_fixture_test.go](../modules/blog/browser_fixture_test.go) shares Firefox startup, HTTP endpoints, reporting, and cleanup. Add scenario requirements explicitly. Keep scenario logic out of the shared runner.

CI tests error responses, native forms, HTMX history, redirects, retargeting, reswapping, and out-of-band notifications in headless Firefox. To run them locally:

```sh
HS_TEST_FIREFOX=firefox go test -count=1 -timeout=10m ./internal/test/web -run '^TestBrowser(SiteErrors|NativeForms|ResponseDispatch|History|ErrorReporting)$'
```

An independent Chromium job runs the shared error, form, response, navigation, and history scenarios. Use Chrome or Chromium with an isolated temporary profile:

```sh
HS_TEST_CHROMIUM=google-chrome go test -count=1 -timeout=5m ./internal/test/web -run '^TestBrowser(SiteErrors|ResponseDispatch|History)$'
```

If both browser settings are present, the shared runner uses Firefox. Clear `HS_TEST_FIREFOX` before selecting Chromium. Blog browser scenarios remain Firefox-only.

HTMX tests use the vendored asset and the layout's integrity check. Ordinary Go test runs skip browser scenarios unless a supported browser setting is present.

The scheduled [browser dependency check](../.github/workflows/browser-dependencies.yml) compares HTMX, EasyMDE, and PDF.js versions in the [library inventory](../modules/blog/static/lib/README.md) with upstream releases. A newer release fails the check for review. It does not update assets automatically. Update both HTMX copies, the fixture URL and integrity hash, and browser version assertions together. Run the Go suite and both browser engines after updating HTMX. Keep PDF.js browser and worker modules on the same version and run the PDF browser checks after updating them.

### Blog accessibility

`TestBrowserAccessibility` checks skip-link focus, focus after HTMX navigation, form control names and help references, Markdown Tab handling, and image chooser focus at 320, 768, 1100, and 3440 pixels. The wider browser suite checks responsive layouts, search focus, revision navigation, confirmations, scheduling, and reduced-motion notifications. These tests use isolated data and never reset the running blog.

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

Any failed attempt keeps the run failed. Both suites allow 60 seconds for JavaScript startup. Framework checks then have 15 seconds. Blog checks have 60 seconds. Diagnostic runs do not replace application browser checks on pushes and pull requests.
