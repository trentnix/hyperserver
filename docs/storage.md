# Storage providers and maintenance

The [README](../readme.md#account-storage) shows account setup. This guide covers provider guarantees, SQLite migrations, and cleanup. For session APIs, see the [session guide](../pkg/services/session/readme.md).

## Account storage

The reference application's `auth.accountStorage.provider` defaults to `sqlite`, using the application-owned `database.connection`. SQLite accepts no `auth.accountStorage.options`. Unknown names stop startup when authentication is enabled. The web and cleanup commands share [provider selection](../cmd/internal/setup/accounts.go), separate from login providers under `auth.services`.

Account models and contracts live in [`user`](../pkg/services/user), with SQLite support in [`user/sqlite`](../pkg/services/user/sqlite). See the [README](../readme.md#account-storage) for construction and initialization. Register repository-owned cleanup before initializing consumers. A supplied pool keeps its existing owner.

Initialization checks writable columns, supported types, required `NOT NULL` declarations, and unique keys for account IDs, normalized email keys, and account/token pairs. Keys must cover every row and use binary comparison. Tokens must reference `user(id)` through `user_id` with `ON DELETE CASCADE`. These checks do not validate every stored value. See [schema versions and migrations](#account-schema-migrations) for startup's version checks.

## Storage guarantees

Replacement providers must preserve the [account](../pkg/services/user/account_repository.go), [session](../pkg/services/session/store.go), and [contact](../modules/site/site.go) guarantees, not just their method signatures. Account security changes use version checks and atomic writes. Contact creation inserts a new submission on each call, so retries can create duplicates. Sessions support [serializer replacement](../pkg/services/session/readme.md#replacing-serialization) independently of storage, with JSON as the default.

Account matching and uniqueness use [EmailKey](../pkg/services/user/email.go). Unicode case-insensitive local parts and equivalent Unicode/Punycode domains share a key. The original address is preserved for display and delivery. Dots, tags, local-part quoting, and local-part Unicode normalization remain significant. Non-transitional IDNA keeps `faß.example` and `fass.example` distinct.

SQLite writes complete before reporting success. File-backed data survives normal close and reopen. Crash durability depends on SQLite settings and the filesystem. In-memory databases are temporary.

The sample file contact provider publishes complete files without overwriting records, but does not sync them to disk or promise power-loss durability. Its filesystem calls cannot be interrupted by context cancellation. Applications needing stronger guarantees must select another provider.

Factories must reject unsupported settings and release partially acquired resources. Authentication rejects session providers without revocable rotation, and maintenance rejects missing cleanup capabilities. Interface checks cannot prove a custom provider's behavior. Its tests must establish the required guarantees. See [session behavior](../pkg/services/session/readme.md#storage-guarantees) for concurrent saves and expiration.

Accounts, contacts, and each configured SQLite session table have independent schema versions. Default session payloads have a separate format version and support reading unversioned JSON objects. See [session serialization](../pkg/services/session/readme.md#replacing-serialization) for compatibility and size limits.

## Account schema migrations

Accounts use the separately installed [golang-migrate v4.20.1 CLI](https://github.com/golang-migrate/migrate/releases/tag/v4.20.1). Migrations are plain SQL: [up](../pkg/services/user/sqlite/migrations/000001_accounts.up.sql) creates version 1 and [down](../pkg/services/user/sqlite/migrations/000001_accounts.down.sql) removes its tables. On first startup, the application creates the SQLite file if needed. If no account storage objects exist, initialization runs the embedded up file and records version 1 in one transaction. Fresh databases need no migration executable or manual setup.

Existing account storage must record version 1 with `dirty = false` in `hyperserver_account_version`. Missing, malformed, dirty, or unsupported versions stop startup without changes. Startup never deletes storage or runs later migrations. Unversioned account databases have no supported upgrade path. Recreate disposable development databases rather than adding version metadata by hand.

Install the pinned CLI with its SQLite driver, outside the application's dependency graph. The executable goes in `GOBIN`, or `$(go env GOPATH)/bin` if `GOBIN` is unset. With that directory on `PATH`, run from the repository root against a database with no account tables:

```sh
go install -tags sqlite3 github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1
migrate -path pkg/services/user/sqlite/migrations \
  -database 'sqlite3://file:///absolute/path/database.db?x-migrations-table=hyperserver_account_version&_foreign_keys=on' up
```

Before deployment migrations, stop affected writers, including cleanup jobs, and back up the data. Run one migration process per database at a time because the SQLite driver does not coordinate migration-process locks. Keep the account-specific version-table setting in every command. Validate the result before restarting consumers.

Use `down 1` to reverse version 1. After down, startup rejects the empty version table until an explicit `up` recreates the schema. Failed transactional migrations must roll back. Inspect and repair the database before using `force` to correct its version marker. `force` does not run or undo SQL.

After a committed change, use forward repair or restore a compatible backup. Restart an older application only if it supports the resulting format.

The CLI adds no dependencies to this Go module, but its own dependencies still need maintenance. Only the SQLite provider knows its version-table format. Replacing the tool would require converting that metadata, not account records.

CI installs the pinned CLI and tests bootstrap compatibility, up/down, independent version tables, failed-migration rollback, and explicit repair. To run those tests locally:

```sh
HS_TEST_MIGRATE=migrate go test ./pkg/services/user/sqlite -run '^TestAccountMigration' -count=1
```

The command fails if `migrate` is not on `PATH`. You can also set `HS_TEST_MIGRATE` to an absolute executable path. Without `HS_TEST_MIGRATE`, CLI integration tests skip. Schema and version-validation tests still run.

## Contact and session migrations

SQLite contact startup checks writable columns, supported types, required email values (`NOT NULL`), and a non-partial unique submission-ID key with binary comparison. Incompatible tables stop site initialization. Setup does not repair existing tables or validate stored records.

Contact storage follows the same first-start and version-check policy as accounts, using [contact migrations](../modules/site/database/migrations/000001_contacts.up.sql) and `hyperserver_contact_version`. To run its migrations explicitly:

```sh
migrate -path modules/site/database/migrations \
  -database 'sqlite3://file:///absolute/path/database.db?x-migrations-table=hyperserver_contact_version&_foreign_keys=on' up
```

See [session migrations](../pkg/services/session/readme.md#sqlite-schema-migrations) for per-table versions and custom table names. These providers can share a database without sharing version markers. Run each provider's migrations separately.

## SQLite connections

The reference application and built-in session and maintenance providers use the [SQLite pool opener](../pkg/database/sqlite/pool.go). Each pool allows one open connection and retains it while idle. Every new or replacement connection enables foreign keys and waits up to 5000 milliseconds for locks. Set `_timeout` or `_busy_timeout` in the connection string to change that wait. Zero disables waiting. Disabling foreign keys is rejected.

Cancellation stops a wait for a free pool connection. During a SQLite lock wait, cancellation can be delayed until the lock timeout and return a lock error instead of a context error. Caller-supplied pools retain their owner's settings.

## Direct backend access

Keep native clients in application setup and pass them to modules that need them. A pool shared with repositories can also serve a reporting query:

```go
var accountCount int
if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM user").Scan(&accountCount); err != nil {
    return err
}
```

Use a typed constructor parameter such as `*sql.DB` for application-supplied clients. If another module supplies the client, publish it as a capability, declare the requirement, and retrieve it with `handlers.GetDependency[*sql.DB](d, "sqlite-pool")`. The [shared-pool test](../pkg/handlers/storage_initialization_test.go) covers native queries alongside account and contact repositories. Other backends can use their own client types without adding a framework interface.

Application setup owns the clients it supplies and closes them after modules stop. Modules must not close supplied clients. A module that creates its own client owns its cleanup. Modules must still close query results and any dedicated `*sql.Conn` they acquire. Closing a dedicated connection returns it to the pool without closing the pool.

Direct access couples that code to the backend. Direct writes must preserve the affected service's rules, including validation, token invalidation, and session revocation. Sharing a pool does not make separate operations one transaction.

## Provider selection

The reference application's [shared setup](../cmd/internal/setup) is private to packages under `cmd`. It supports named factories. In the web command, register replacements before initialization:

```go
accounts := setup.NewAccountStorage(app.Config)
if err := accounts.RegisterProvider("company", setup.AccountProvider{
    Open: openCompanyAccounts,
    OpenMaintenance: openCompanyTokenMaintenance, // Optional. Opens existing storage only.
}); err != nil {
    return err
}
if err := initializeAccounts(ctx, app, accounts); err != nil {
    return err
}
```

This `cmd/internal/setup` helper uses configuration, not a server instance. Provider factories receive the startup context and a copy of the configured options. `accounts.Open(ctx, pool)` returns an initialized repository and its optional cleanup function. Failed opens release partial acquisitions. The web command's [initializeAccounts](../cmd/web/accounts.go) supplies the repository and registers its cleanup. Cleanup runs after consumers stop and before the shared pool closes.

Select the registered name and its options in configuration:

```yaml
auth:
  accountStorage:
    provider: company
    options:
      connection: your-provider-connection
```

Reference provider registrations belong to one setup instance. Names are case-sensitive, and `sqlite` is reserved. Supplying `AccountRepository` on the reference application's [setup struct](../cmd/web/application.go) bypasses selection and initialization. The reference application ships only SQLite. Its shared database and session setup remain separate from account-provider selection.

`OpenMaintenance` returns [TokenMaintenance](../pkg/services/user/account_repository.go), an optional close function, and an error. It must not create or migrate tables. Each batch closes acquired resources, including on failure. Missing maintenance capabilities return errors rather than skipping cleanup.

The cleanup command calls `setup.CleanupAccountTokens(ctx, cfg, limit)` without constructing a server or `AccountStorage`. Provider selection is shared with web startup. An account setup's `CleanupTokens` method uses its custom registrations through the same maintenance implementation. Applications already holding a repository with `TokenMaintenance` can call `DeleteExpiredTokens` directly.

## Expired-record cleanup

Run `go run ./cmd/cleanup -batch-size 100 -timeout 10s` with the same configuration as `cmd/web`. Both commands resolve storage paths from that configuration. Each run deletes at most 100 expired rows from each selected server-side session store and, when authentication is enabled, account-token storage. Live records and accounts are unchanged.

Providers own maintenance setup and pool handling. SQLite files and tables must already exist. Cleanup does not create missing files or support in-memory databases.

The supplied command includes the built-in providers. A custom account provider must also be registered in the application's maintenance executable. Unknown providers and missing maintenance capabilities produce an error without falling back to SQLite. Independent session cleanup still runs.

Schedule the command externally for regular cleanup. The server does not run cleanup automatically. The command reports deleted counts and exits with an error if either cleanup fails. Expiration checks still reject expired sessions and tokens before cleanup runs.
