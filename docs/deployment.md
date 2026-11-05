# Local deployment and recovery

This rehearsal runs the [blog](../cmd/web) with disposable data. It uses disposable SQLite storage, Mailpit, and an Nginx HTTPS proxy. Only host loopback ports are published. Do not expose this stack through a tunnel, change those bindings, or use real account data. An independent security review remains required before public account and recovery traffic.

## Start and test

Requirements: Docker Engine with Compose on Linux, Git, OpenSSL, Bash, and `flock`. Run the tests with the Go version in `go.mod`. The containers share a network namespace so the application can keep its loopback-only listener.

From the repository root:

```sh
bash deploy/local/init.sh
docker --context default compose -p hyperserver-rehearsal -f deploy/local/compose.yaml --profile tools build
docker --context default compose -p hyperserver-rehearsal -f deploy/local/compose.yaml up -d --wait
```

Use a fresh rehearsal data directory when switching from the retired contact demo. Existing demo databases are not migrated automatically. Open https://localhost:18443 and Mailpit at http://localhost:18025. Register `admin@example.invalid`, the address in [blog.yaml](../deploy/local/blog.yaml), and verify it through Mailpit. Automated deployment tests use that address with a fixed disposable password. Do not create a different password before running them. The generated certificate is self-signed and valid for 30 days. Tests trust only that certificate. For command-line checks, use `curl --cacert deploy/local/state/tls/cert.pem https://localhost:18443/health/ready`. Do not disable certificate verification globally.

Run the destructive recovery/failure exercises only against this disposable stack, with no other writers or tests running:

```sh
HS_TEST_DEPLOYMENT=1 go test -v -count=1 -timeout=10m ./internal/test/deployment -run '^TestDeployment(Recovery|Failures|Load|Upload)$'
```

Run the optional soak separately. It is not a CI requirement:

```sh
HS_TEST_DEPLOYMENT=1 HS_TEST_SOAK=1 go test -v -count=1 -timeout=35m ./internal/test/deployment -run '^TestDeploymentSoak$'
```

`HS_SOAK_DURATION` overrides the default `30m`. Increase the Go test timeout with it. The soak sends roughly ten requests per second across authenticated and anonymous pages and fragments through HTTPS. The 30-second load test uses the same four clients without pauses. Both check failures and record container resources and mean response time. Use the soak after storage, session, rendering, or lifecycle changes. It is a stability check, not a capacity claim or proof against long-term leaks.

Stop the stack with `docker --context default compose -p hyperserver-rehearsal -f deploy/local/compose.yaml down`. Commands pin the Docker context and project so inherited settings cannot select another stack. Data and backups remain under the ignored `deploy/local/state` directory. Other applications and the repository's own `config.yaml` are untouched.

## Configuration and ownership

[Compose](../deploy/local/compose.yaml) owns process limits, mounts, ports, and shutdown grace. [config.yaml](../deploy/local/config.yaml) selects application capabilities. [Nginx](../deploy/local/nginx.conf) owns TLS and its bounded HTTP cache. Mailpit captures mail and has no relay configured. Its plaintext SMTP and permissive authentication are strictly local test settings, not production recommendations.

The app and maintenance command run as the invoking user's numeric UID/GID with dropped capabilities, no privilege escalation, and a read-only root filesystem. Each service has a 256 MiB memory limit, one CPU allowance, and a 128-process limit. Go's memory target is 192 MiB. The app has a writable 64 MiB temporary filesystem for multipart uploads. Docker logs rotate at three 10 MiB files. Nginx has a 16 MiB cache inside a 64 MiB temporary filesystem. Mailpit retains at most 100 messages. SQLite data and backups use host storage without a quota. Monitor available disk space and remove obsolete rehearsal backups yourself.

`init.sh` generates separate signing keys in `state/app.env` and a TLS key under `state/tls`, with private permissions. It preserves existing keys. Docker administrators can inspect container environment values. Use the target environment's secret-management facility for a real deployment. Never put secrets in image build arguments or commit them.

The build pins Go and container images and uses the module checksums. The image labels report version and commit. Rerun `init.sh` before building to refresh the revision. A dirty checkout is labeled `-dirty`, not presented as the exact committed tree. OS packages receive distribution updates at build time, so rebuilds are not promised to be byte-identical. Record the resulting image digest when testing or deploying.

The proxy preserves Host and origin headers and overwrites forwarding headers. The app trusts only its loopback peer. Health probes bypass sessions and rate limits. The proxy does not bypass `Cache-Control`, `Vary`, or `Set-Cookie`. Cache keys include both HTMX headers. Requests with cookies or authorization bypass the cache. Tests check an actual public-asset cache hit and uncached pages/fragments for authenticated and anonymous clients. Nginx's runtime error log is disabled because it can expose full token-bearing URLs. Its safe access log records status and duration for upstream failures. Startup checks the proxy configuration before serving.

## Health and logs

`GET /health/live` returns 204 when the process can respond. `GET /health/ready` returns 204 only after startup and while the selected checks pass. It returns a generic 503 during shutdown or a dependency failure. Both forbid caching.

The framework's [HealthHandler](../pkg/application/health.go) checks account repositories and session stores that implement `CheckHealth(context.Context) error`. The blog command requires SMTP connectivity when enabled registration requires verification. Readiness does not currently probe blog content or upload storage. A TCP probe does not prove that SMTP authentication or delivery will succeed. Custom mail senders can implement the same method. Applications can supply additional bounded checks without enabling unrelated services.

Probes are read-only and cooperative. They do not prove write permissions, available disk capacity, or future delivery. Check write and delivery failures through the operation's logs and the failure exercises too. The listener starts after initialization and closes before resource cleanup. Docker's 25-second stop grace covers the app's ten-second HTTP drain and ten-second resource cleanup budgets.

Request logs include method, path, request ID, response status, duration in nanoseconds, and aborted/hijacked flags. Status zero means no HTTP status was observed before an abort or connection hijack. Queries, headers, and bodies are not recorded. Security events record login, logout, registration, verification, and password-change/reset outcomes after the corresponding operation. They share the request ID and omit account addresses and tokens. Application/provider log messages must also avoid secrets. Metrics and tracing remain optional.

## Migrations, backup, and restore

The operator owns schema upgrades. Stop the app and any cleanup jobs, take a backup, run one migration process, verify the result, then start a compatible binary. Never run competing migrators or use `force` to conceal a failed migration. Fresh empty stores bootstrap their current schema during startup. Accounts currently require the version 1 baseline. Existing stores are validated, not automatically upgraded. See [provider migration commands](storage.md#account-schema-migrations).

Accounts and sessions share `/data/hyperserver.db`. Blog content uses `/data/blog.db`, and images use `/data/uploads`. Each schema has its own version table. The maintenance image carries the pinned `golang-migrate` CLI and account/blog SQL. Custom session table names require the provider's rendered migration files described in the [session guide](../pkg/services/session/readme.md).

```sh
bash deploy/local/storage.sh backup before-upgrade
# The app remains stopped. Apply only the intended provider's migrations.
docker --context default compose -p hyperserver-rehearsal -f deploy/local/compose.yaml run --rm --no-deps maintenance \
  migrate -path /migrations/accounts \
  -database 'sqlite3:///data/hyperserver.db?x-migrations-table=hyperserver_account_version' up
docker --context default compose -p hyperserver-rehearsal -f deploy/local/compose.yaml up -d --wait app
```

Backups contain both SQLite snapshots, an uploads archive, and signing keys needed to restore sessions. Treat all backup files as private. The script refuses to overwrite a named backup. It stops the app, uses SQLite's backup command, and never restarts the app, including after failure.

The script holds `state/maintenance.lock` to prevent overlapping script runs. After stopping the app, it also acquires `state/data/.blog-maintenance.lock`, the same file the container sees at `/data/.blog-maintenance.lock`. It holds both locks through backup or restore. A cooperating blog restart or offline command cannot access storage during that operation. If another process acquires the blog lock first, the script fails before accessing storage. Cleanup jobs, raw SQL tools, and older binaries do not honor this lock and must be stopped separately. Do not remove either lock file. See [maintenance locking](blog/backup.md#maintenance-locking).

If a script or Docker command is interrupted, confirm that no maintenance container is still running before restarting the app. Host process locks do not track work that continues inside a container after its Docker client exits.

```sh
bash deploy/local/storage.sh restore before-upgrade
docker --context default compose -p hyperserver-rehearsal -f deploy/local/compose.yaml up -d --wait app
```

Restore checks database integrity and foreign keys, then extracts and validates the uploads archive before replacing either stopped database. It restores signing keys with the snapshot. The recovery test rolls back the blog schema with the real migrator, restores the backup, and verifies account/session continuity and post data after restart. Restore works even when the uploads directory is missing. Existing uploads remain in a `state/data/restore-*/previous-uploads` directory for recovery. Remove staging directories manually after verification. Restore loses writes made after the backup. This rehearsal chooses restore rather than inventing a generic forward-repair procedure. Off-host encrypted backup storage and retention remain deployment-specific decisions.

## Dependency review

[Vulnerability CI](../.github/workflows/vulnerabilities.yml) runs `govulncheck` on pushes, pull requests, a weekly schedule, and manual dispatch. [Dependabot](../.github/dependabot.yml) proposes module, action, Dockerfile, and Compose image updates. CI builds the application and maintenance images but does not run the soak or destructive deployment tests. Review changes and require CI before merging. No workflow auto-merges updates. Repository branch protections must enforce review if required by the maintainer.

The scanner covers reachable Go code, not the container OS, Nginx, Mailpit, or vendored JavaScript. Review their advisories when updating the pinned versions and before a real deployment. Keep maintained bcrypt, OAuth, SQLite, and migration implementations. No replacement cryptography or migration engine is introduced here.
