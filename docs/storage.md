# Storage providers and maintenance

Use [application setup](application.md#services-and-serving) for shared services and the [session guide](../pkg/services/session/readme.md) for session behavior. This guide covers storage contracts, SQLite, migrations, and cleanup.

## Account storage

`Options.AccountProviders` supplies the complete catalog for `auth.accountStorage.provider`. No provider is included by default. `Options.Services.Accounts` instead accepts an initialized repository without transferring ownership. [configured.AccountProviders](../pkg/services/configured/accounts.go) supplies built-in factories.

For direct setup, resolve paths first and call `user.OpenConfiguredStorage`:

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

Names are case-sensitive. Factories receive the caller's context and private option maps. Only the selected factory runs. Failed opens close partial resources. Applications own successful resources. `OpenMaintenance` must not create or migrate storage.

`configured.OpenAccounts` and `configured.NewSessionManager` are convenience helpers with built-in catalogs and optional overrides. They install no modules, middleware, or cleanup hooks. Unlike the application constructor, `configured.OpenAccounts` defaults an empty account-provider name to SQLite when auth is enabled.

SQLite accounts require a `connection` option unless using an existing pool. Relative connections use the application's resource directory. The legacy `database` setting does not select account storage. Different pools can share a file. `:memory:` creates a separate database per pool.

For a borrowed pool, supply `usersqlite.ConfiguredProvider(cfg, pool)` under the selected name. The pool keeps its original owner. Maintenance still needs a connection option pointing to the same database. For direct construction, call `sqlite.NewAccountRepository(pool)` and `Initialize(ctx)` before supplying the repository.

### Account capabilities and adapter upgrades

[`user.AccountRepository`](../pkg/services/user/account_repository.go) requires `GetByID`, `GetByEmail`, `Create`, and `Update`. Other features use optional interfaces. SQLite implements all of them.

| Capability | Required by |
| --- | --- |
| `PasswordRepository` | Password changes and email authentication. |
| `TokenRepository` | Token-backed features. |
| `PasswordResetRepository` | Email authentication. Includes `TokenRepository`. |
| `VerificationRepository` | Email authentication, including verification of existing accounts. Includes `TokenRepository`. |
| `ExternalIdentityRepository` | Google authentication. Password and verification storage are not required. |
| `PasswordSetupRepository` | Optional password enrollment. Includes `TokenRepository`. |
| `InvitationRepository` | Blog invitations when `multiUser` is enabled. Includes `TokenRepository`. |

Closing registration or disabling verification for new accounts does not clear existing verification requirements. Email authentication therefore still requires verification storage. Mail configuration is required at startup only when enabled features need it. Blog roles and setup also require user administration.

Adapters migrating from the combined interface can retain their methods. Consumers must use the appropriate capability interface. Wrappers must forward optional methods explicitly because embedding only `AccountRepository` hides them. Missing required capabilities stop startup. This interface change requires no SQLite migration.

## Storage guarantees

Replacement providers must preserve the [account](../pkg/services/user/account_repository.go) and [session](../pkg/services/session/store.go) contracts, including concurrency and failure behavior. Account security changes require version checks and atomic writes. Session encoding remains [independent of storage](../pkg/services/session/readme.md#replacing-serialization).

Use [EmailKey](../pkg/services/user/email.go) for account matching and uniqueness. It normalizes Unicode case in local parts and equivalent Unicode/Punycode domains while preserving the original address. Dots, tags, quoting, and local-part Unicode normalization remain significant. Non-transitional IDNA keeps `faß.example` and `fass.example` distinct.

Providers must distinguish rejected tokens from infrastructure failures through the [documented errors](../pkg/services/user/errors.go) and `errors.Is`. HTTP recovery handlers return 400 for rejected tokens and 500 for storage failures. Errors must not expose tokens or private provider details. Notification failures after a committed change are logged and cannot undo it.

SQLite reports success after writes complete. File-backed data survives normal close and reopen. Crash durability depends on SQLite settings and the filesystem. In-memory data does not survive closing its pool.

Factories must reject unsupported settings and release partial acquisitions. Authentication requires revocable session rotation. Maintenance requires an explicit cleanup capability.

## Testing a replacement provider

Import shared suites from your provider's `_test.go` files. They work from another Go module without application startup or registration.

| Contract | Test entry point |
| --- | --- |
| Accounts | [accounttest.Run](../pkg/services/user/accounttest/contract.go) |
| Token-only storage | [accounttest.RunTokens](../pkg/services/user/accounttest/token_storage.go), with supported purposes |
| Common sessions | [sessiontest.RunCommon](../pkg/services/session/sessiontest/contract.go) |
| Revocable sessions | [sessiontest.Run](../pkg/services/session/sessiontest/contract.go), including common checks |

Each factory must return empty, initialized storage and register cleanup with its `*testing.T`. Use temporary databases, isolated schemas, or unique namespaces. Never run contract suites on production data.

```go
func TestAccountContract(t *testing.T) {
    accounttest.Run(t, func(t *testing.T) user.AccountRepository {
        return openTestRepository(t)
    })
}
```

See the SQLite [account factory](../pkg/services/user/sqlite/contract_test.go) and [session factory](../pkg/services/session/sqlite/contract_test.go). The account suite checks advertised capabilities and their supported token purposes. Token-only providers must call `RunTokens` separately. Keep backend-specific tests for failures, migrations, expiry boundaries, timestamp precision, durability, external identities, invitations, password setup, and maintenance. Run contracts and application flows with both ordinary tests and the race detector.

## Account schema migrations

Fresh account storage bootstraps schema 1 from [embedded migrations](../pkg/services/user/sqlite/migrations/000001_accounts.up.sql) in one transaction. Existing storage must have clean version 1 in `hyperserver_account_version` and pass structural validation. Startup checks required writable columns, types, nullability, binary unique keys, and cascading token references. It does not validate every stored value, upgrade existing databases, or repair them.

Install the pinned CLI with its SQLite driver. It goes in `GOBIN` or `$(go env GOPATH)/bin`, which must be on `PATH`:

```sh
go install -tags sqlite3 github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1
migrate -path pkg/services/user/sqlite/migrations \
  -database 'sqlite3://file:///absolute/path/database.db?x-migrations-table=hyperserver_account_version&_foreign_keys=on' up
```

Before any offline migration, stop writers and cleanup jobs, prevent restarts, and verify a backup. Run only one migration process per database. The CLI does not acquire the blog lock or serialize migration processes. Keep the intended `x-migrations-table` and `_foreign_keys=on` parameters in every command.

Missing, dirty, malformed, or unsupported markers stop startup. Never change a marker to bypass schema validation. Reversing the account baseline deletes accounts, identity links, tokens, and indexes. Startup rejects the resulting empty version table until explicit `up` recreates it. Accounts, [sessions](../pkg/services/session/readme.md#sqlite-schema-migrations), and [blog content](blog/readme.md#database-migrations) have independent migrations and version tables, even in a shared database.

Failed transactional migrations roll back SQL but can leave a dirty marker. Inspect and repair the failure before using `force`, which changes metadata only. After a committed change, use forward repair or restore a compatible backup. An older binary must support the resulting schema before restarting.

Keep deployed SQL immutable. Add a sequential numbered migration pair and update the binary's required version, bootstrap, validation, and repositories together. Each binary supports one schema version. Test fresh startup, populated upgrades, downgrades, failures, and recovery compatibility.

CI installs the pinned CLI. To include account migration integration tests locally:

```sh
HS_TEST_MIGRATE=migrate go test ./pkg/services/user/sqlite -run '^TestAccountMigration' -count=1
```

An absolute CLI path also works. Without `HS_TEST_MIGRATE`, CLI tests skip. Schema-validation tests still run.

## SQLite connections and native access

The built-in providers use the [SQLite pool opener](../pkg/database/sqlite/pool.go). Each pool retains one connection, enables foreign keys, and waits up to 5,000 milliseconds for locks. `_timeout` or `_busy_timeout` changes the wait. Zero disables waiting. Disabling foreign keys is rejected. Borrowed pools retain their owner's settings.

Cancellation stops waiting for a pool connection. A SQLite lock wait can delay cancellation until the lock timeout and return a lock error instead of a context error.

Pass native clients such as `*sql.DB` through typed module constructors. If another module owns the client, publish a capability and retrieve it with `handlers.GetDependency[*sql.DB]`. See the [shared-pool test](../pkg/handlers/storage_initialization_test.go).

The creator owns client cleanup. Consumers must close rows and dedicated `*sql.Conn` values, not borrowed pools. Native writes must preserve the service's validation, token invalidation, and session revocation rules. Sharing a pool does not make separate operations one transaction.

## Expired-record cleanup

With the same configuration as `cmd/web`, run:

```sh
go run ./cmd/cleanup -batch-size 100 -timeout 10s
```

This deletes at most 100 expired records per selected server-side session store and, when auth is enabled, account-token storage. It leaves live records and accounts unchanged. Expiry checks reject records even before cleanup runs. Schedule runs externally.

The command includes built-in providers. Custom providers must also be supplied to the maintenance executable. Unknown providers and missing maintenance support return errors. Independent cleanup operations are still attempted after a failure. SQLite files and tables must already exist. Maintenance does not create storage or support in-memory databases.

For direct use, `configured.CleanupAccountTokens` and `configured.CleanupSessions` use the same built-in defaults and overrides as their setup helpers. `user.CleanupConfiguredTokens` and `session.CleanupConfiguredStores` use explicit catalogs. Factories receive cancellation and private options, open existing storage, and close acquisitions even on failure.

Cleanup can run alongside the server but does not acquire the blog lock. Pause it and wait for active runs before backups, restores, or migrations. SQLite transaction locks do not protect an operation spanning databases and uploads. See [maintenance locking](blog/backup.md#maintenance-locking).
