# Storage providers and maintenance

Use [application.New](application.md#services-and-serving) for configured account and session setup. This guide covers provider contracts, explicit setup, SQLite migrations, and cleanup. For session APIs, see the [session guide](../pkg/services/session/readme.md).

## Account storage

[`configured.OpenAccounts`](../pkg/services/configured/accounts.go) opens `auth.accountStorage` when authentication is enabled. An empty provider name selects SQLite, which requires `auth.accountStorage.options.connection`. The returned cleanup function closes owned storage. [`configured.NewSessionManager`](../pkg/services/configured/sessions.go) opens the stores selected by `http.session.types` and returns a manager the caller must close. Both accept additional provider factories that can replace built-in names. Resolve configuration paths before calling these helpers. Neither helper installs modules, middleware, or shutdown hooks.

For accounts that share an existing SQLite pool, supply `user/sqlite.ConfiguredProvider(cfg, pool)` as the `"sqlite"` factory. The pool keeps its existing owner. Runtime setup needs no connection option in that case. If using maintenance, set the option to the same database. Without an override, account setup opens and owns its pool.

SQLite account connections use the config directory or `app.workingDirectory` for relative paths. Missing connections and unsupported options stop startup. Accounts do not read the legacy `database` setting. Modules choose their own storage and can share a database file without sharing a pool. `:memory:` creates a separate database per pool.

Account models and contracts live in [`user`](../pkg/services/user), with SQLite support in [`user/sqlite`](../pkg/services/user/sqlite). For explicit setup, call `sqlite.NewAccountRepository(pool)` and then `Initialize(ctx)` before supplying it to consumers. A supplied pool keeps its existing owner.

Initialization checks writable columns, supported types, required `NOT NULL` declarations, and unique keys for account IDs, normalized email keys, and account/token pairs. Keys must cover every row and use binary comparison. Tokens must reference `user(id)` through `user_id` with `ON DELETE CASCADE`. These checks do not validate every stored value. See [schema versions and migrations](#account-schema-migrations) for startup's version checks.

## Storage guarantees

Replacement providers must preserve the [account](../pkg/services/user/account_repository.go) and [session](../pkg/services/session/store.go) guarantees, not just their method signatures. Account security changes use version checks and atomic writes. Sessions support [serializer replacement](../pkg/services/session/readme.md#replacing-serialization) independently of storage, with JSON as the default.

Account providers must distinguish rejected recovery tokens from storage failures using the [documented errors](../pkg/services/user/errors.go) and `errors.Is`. Reset-link validation and token redemption return HTTP 400 for rejected tokens and HTTP 500 for infrastructure failures. Error responses and diagnostics omit token values and private provider details. Notification or redirect-session cleanup failures after a committed account change or login are logged without undoing the successful operation.

Account matching and uniqueness use [EmailKey](../pkg/services/user/email.go). Unicode case-insensitive local parts and equivalent Unicode/Punycode domains share a key. The original address is preserved for display and delivery. Dots, tags, local-part quoting, and local-part Unicode normalization remain significant. Non-transitional IDNA keeps `faß.example` and `fass.example` distinct.

SQLite writes complete before reporting success. File-backed data survives normal close and reopen. Crash durability depends on SQLite settings and the filesystem. In-memory databases are temporary.

Factories must reject unsupported settings and release partially acquired resources. Authentication rejects session providers without revocable rotation, and maintenance rejects missing cleanup capabilities. Interface checks cannot prove a custom provider's behavior. Its tests must establish the required guarantees. See [session behavior](../pkg/services/session/readme.md#storage-guarantees) for concurrent saves and expiration.

Accounts and each configured SQLite session table have independent schema versions. Default session payloads have a separate format version and support reading unversioned JSON objects. See [session serialization](../pkg/services/session/readme.md#replacing-serialization) for compatibility and size limits.

## Testing a replacement provider

Import the shared suites from your provider's `_test.go` files. They work from another Go module and require no registration or application startup.

| Contract | Test package and entry point |
| --- | --- |
| Account storage | [`pkg/services/user/accounttest.Run`](../pkg/services/user/accounttest/contract.go) |
| Common session behavior | [`pkg/services/session/sessiontest.RunCommon`](../pkg/services/session/sessiontest/contract.go) |
| Server-side sessions with revocable rotation | [`pkg/services/session/sessiontest.Run`](../pkg/services/session/sessiontest/contract.go), which also runs the common suite |

Pass a factory that creates empty, initialized storage for each call and registers cleanup with the supplied `*testing.T`. Use temporary databases, isolated schemas, or unique namespaces. Never point the suites at production data. Providers must support the concurrent operations exercised by their suite.

For example, call the account suite from your provider package:

```go
func TestAccountContract(t *testing.T) {
    accounttest.Run(t, func(t *testing.T) user.AccountRepository {
        return openTestRepository(t)
    })
}
```

Your `openTestRepository` helper owns backend setup and cleanup. See the [SQLite account test](../pkg/services/user/sqlite/contract_test.go) and [session test](../pkg/services/session/sqlite/contract_test.go) for complete factories.

These suites check shared behavior, not every provider capability. Keep tests for backend failures, schema migrations, exact expiration boundaries, timestamp precision, durability, and optional interfaces with the provider. Test external identity storage separately when supporting external authentication. Run the suites with `go test` and `go test -race` alongside application integration tests. The test APIs follow the framework's [experimental compatibility policy](compatibility.md).

## Account schema migrations

Accounts use the separately installed [golang-migrate v4.20.1 CLI](https://github.com/golang-migrate/migrate/releases/tag/v4.20.1). The [version 1 baseline](../pkg/services/user/sqlite/migrations/000001_accounts.up.sql) creates accounts, external identity links, recipient-bound tokens, and token indexes. Version 2 adds an application-defined role and an invitation flag. Invitations reuse recipient-bound tokens. On first startup, the application creates the SQLite file if needed. If no account storage objects exist, initialization discovers the embedded numbered `.up.sql` files, applies them in order, and records the required version in one transaction. Fresh databases need no migration executable or manual setup.

Each binary supports exactly one account schema version, declared by `accountSchemaVersion`. To change the schema, add the next zero-padded, sequential migration pair and update the required version and repository code together. Startup does not select between schema implementations or upgrade existing storage. The bootstrap runner requires the embedded migrations to end at the declared version.

Existing account storage must match the current schema and record version 2 with `dirty = false` in `hyperserver_account_version`. Missing, malformed, dirty, or unsupported versions stop startup without changes. Startup never deletes storage or migrates an existing database. Back up before an offline migration. Do not change version metadata merely to bypass startup checks.

Install the pinned CLI with its SQLite driver, outside the application's dependency graph. The executable goes in `GOBIN`, or `$(go env GOPATH)/bin` if `GOBIN` is unset. With that directory on `PATH`, run from the repository root against an empty or supported versioned account database:

```sh
go install -tags sqlite3 github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1
migrate -path pkg/services/user/sqlite/migrations \
  -database 'sqlite3://file:///absolute/path/database.db?x-migrations-table=hyperserver_account_version&_foreign_keys=on' up
```

Before deployment migrations, stop affected writers, including cleanup jobs, and back up the data. Run one migration process per database at a time because the SQLite driver does not coordinate migration-process locks. Keep the account-specific version-table setting in every command. Validate the result before restarting consumers.

Reversing version 2 removes roles, invitation flags, and invitation tokens but preserves credentials and external identities. Reversing the baseline deletes all accounts, external identity links, tokens, and their indexes. Back up before downgrading. Session and blog schemas have separate version tables and remain unchanged. After the baseline is reversed, startup rejects the empty version table until an explicit `up` recreates the schema. Failed transactional migrations must roll back. Inspect and repair the database before using `force` to correct its version marker. `force` does not run or undo SQL.

After a committed change, use forward repair or restore a compatible backup. Restart an older application only if it supports the resulting format.

CI installs the pinned CLI and tests bootstrap compatibility, up/down, independent version tables, failed-migration rollback, and explicit repair. To run those tests locally:

```sh
HS_TEST_MIGRATE=migrate go test ./pkg/services/user/sqlite -run '^TestAccountMigration' -count=1
```

The command fails if `migrate` is not on `PATH`. You can also set `HS_TEST_MIGRATE` to an absolute executable path. Without `HS_TEST_MIGRATE`, CLI integration tests skip. Schema and version-validation tests still run.

## Session migrations

See [session migrations](../pkg/services/session/readme.md#sqlite-schema-migrations) for per-table versions and custom table names. These providers can share a database without sharing version markers. Run each provider's migrations separately.

## SQLite connections

The blog and built-in session and maintenance providers use the [SQLite pool opener](../pkg/database/sqlite/pool.go). Each pool allows one open connection and retains it while idle. Every new or replacement connection enables foreign keys and waits up to 5000 milliseconds for locks. Set `_timeout` or `_busy_timeout` in the connection string to change that wait. Zero disables waiting. Disabling foreign keys is rejected.

Cancellation stops a wait for a free pool connection. During a SQLite lock wait, cancellation can be delayed until the lock timeout and return a lock error instead of a context error. Caller-supplied pools retain their owner's settings.

## Direct backend access

Keep native clients in application setup and pass them to modules that need them. A pool shared with repositories can also serve a reporting query:

```go
var accountCount int
if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM user").Scan(&accountCount); err != nil {
    return err
}
```

Use a typed constructor parameter such as `*sql.DB` for application-supplied clients. If another module supplies the client, publish it as a capability, declare the requirement, and retrieve it with `handlers.GetDependency[*sql.DB](d, "sqlite-pool")`. The [shared-pool test](../pkg/handlers/storage_initialization_test.go) covers native queries alongside an account repository and a test-only consumer table. Other backends can use their own client types without adding a framework interface.

Application setup owns the clients it supplies and closes them after modules stop. Modules must not close supplied clients. A module that creates its own client owns its cleanup. Modules must still close query results and any dedicated `*sql.Conn` they acquire. Closing a dedicated connection returns it to the pool without closing the pool.

Direct access couples that code to the backend. Direct writes must preserve the affected service's rules, including validation, token invalidation, and session revocation. Sharing a pool does not make separate operations one transaction.

## Provider selection

Applications can use `user.OpenConfiguredStorage` with an explicit map of available adapters. Selection and failure cleanup belong to the library. The application owns successful resources and decides when to close them:

```go
providers := map[string]user.StorageProvider{
    "company": {
        Open: openCompanyAccounts,
        OpenMaintenance: openCompanyTokenMaintenance, // Optional. Opens existing storage only.
    },
}
repository, closeStorage, err := user.OpenConfiguredStorage(ctx, cfg.Auth.AccountStorage, providers)
if err != nil {
    return err
}
// Supply repository to consumers. Call closeStorage, if non-nil, after they stop.
```

Factories receive the caller's context and a private copy of the configured options. The library has no default provider. For SQLite, `usersqlite.ConfiguredProvider(*cfg, pool)` wraps an application-owned pool without closing it. Direct repository constructors remain available for applications that do not need configuration-driven selection.

Select the registered name and its options in configuration:

```yaml
auth:
  accountStorage:
    provider: company
    options:
      connection: your-provider-connection
```

Provider names are case-sensitive. `application.Options.AccountProviders` adds or replaces factories. Supplying `Options.Services.Accounts` bypasses selection and initialization without transferring ownership. [`configured.AccountProviders`](../pkg/services/configured/accounts.go) supplies the built-in catalog for startup and maintenance. Pass custom factories to both operations when replacing a provider.

`OpenMaintenance` returns [TokenMaintenance](../pkg/services/user/account_repository.go), an optional close function, and an error. It must not create or migrate tables. Each batch closes acquired resources, including on failure. Missing maintenance capabilities return errors rather than skipping cleanup.

`configured.CleanupAccountTokens(ctx, cfg, limit, additional)` uses the same defaults and provider overrides as `configured.OpenAccounts`, without opening a runtime repository. Disabled authentication skips cleanup. For an explicit catalog without built-in defaults, use `user.CleanupConfiguredTokens`. Applications already holding a repository with `TokenMaintenance` can call `DeleteExpiredTokens` directly.

`configured.CleanupSessions(ctx, cfg, limit, additional)` uses the same defaults and provider overrides as `configured.NewSessionManager`. It removes up to `limit` expired sessions from each selected store without initializing runtime stores or schema. For an explicit catalog without built-in defaults, use `session.CleanupConfiguredStores`. Both configured cleanup helpers close the storage they open and use the caller's context for cancellation and deadlines.

## Expired-record cleanup

Run `go run ./cmd/cleanup -batch-size 100 -timeout 10s` with the same configuration as `cmd/web`. Both commands resolve storage paths from that configuration. Each run deletes at most 100 expired rows from each selected server-side session store and, when authentication is enabled, account-token storage. Live records and accounts are unchanged.

Providers own maintenance setup and pool handling. SQLite files and tables must already exist. Cleanup does not create missing files or support in-memory databases.

The supplied command includes the built-in providers. Custom account and session providers must also be supplied to the application's maintenance executable. Unknown providers and missing maintenance capabilities produce an error without falling back to SQLite. Independent session cleanup still runs.

Schedule the command externally for regular cleanup. The server does not run cleanup automatically. The command reports deleted counts and exits with an error if either cleanup fails. Expiration checks still reject expired sessions and tokens before cleanup runs.

Cleanup can run alongside the server, but it does not acquire the blog's process lock. Pause scheduled cleanup and wait for active runs to finish before backup, restore, or migrations. SQLite locking protects individual transactions, not an offline operation spanning databases and uploads. See [maintenance locking](blog/backup.md#maintenance-locking).
