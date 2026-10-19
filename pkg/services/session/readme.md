# Sessions

The `session` package defines session values, storage contracts, and request handling. Optional providers implement [cookie](cookie/store.go) and [SQLite](sqlite/store.go) storage. `application.New` attaches the configured `SessionManager` through `app.Handler`. With lower-level APIs, attach the manager before using `session.Get` or `session.New`. See [config-template.yaml](../../../config-template.yaml) for `http.session` configuration.

## Storage and limits

`CookieStore` puts session data in a signed JWT cookie. Signing prevents modification but does not encrypt the contents. Do not store passwords, recovery tokens, credentials, or other secrets in cookie sessions.

`SQLiteStore` stores session data in SQLite. Its signed cookie contains a session ID and token metadata, not session data. Authentication requires a store that supports revocable rotation, such as SQLite. Cookie-only sessions cannot revoke captured cookies.

Both providers bind signed cookies to the session name. Renamed cookies and older cookies without a signed name are rejected. After upgrading from the unnamed format, clear the browser's HyperServer cookies and sign in again. This does not change stored payload or database schema versions.

Both stores default to versioned JSON encoded in URL-safe base64. Each store can select a different [Serializer](serializer.go). `Session.Data` still accepts JSON-compatible values. The JSON serializer restores numbers as `json.Number` to preserve integer precision. Go structs and concrete numeric types are not preserved automatically. Use `DecodeValue` to read values into the expected type. That helper uses JSON conversion independently of the storage format.

- `MaxSessionDataSize` limits serialized data to 64 KiB before base64 encoding. Reads reject oversized data before calling the serializer. Saves and rotations reject oversized serializer output before writing storage.
- `MaxCookieSize` limits each cookie to 4 KiB, including its name and outgoing attributes. JWT and base64 overhead reduce the space available for cookie-store data.
- Invalid or oversized data returns an error. A missing or expired session returns a new, empty session.

These limits apply to each session, not the total size of an HTTP request. Incoming request-header limits are a separate server concern.

## Storage guarantees

Providers serve concurrent requests, but each request owns its mutable session data. SQLite saves replace the entire stored payload. If two requests load the same session and save changes, the later write replaces the earlier data. There is no merge or stale-write check. Do not use session read-modify-write for reliable counters or balances. Cookie-only saves likewise depend on which response the browser applies last.

SQLite creates use unique IDs and reject collisions without replacing existing data. Saving a deleted session fails. Reads reject expired sessions even before cleanup. Saves preserve the original expiration and cannot revive an expired session. Rotation creates a new lifetime and revokes the previous ID atomically. Only one concurrent rotation of a stored session can succeed.

SQLite issues cookies only after successful writes. Persistent database files survive normal close and reopen. Crash durability depends on SQLite settings and the filesystem. In-memory databases do not survive closing their connections. Cookie-only storage has no server-side persistence or revocation. Its data lasts only as long as the browser retains a valid cookie. Neither provider can guarantee delivery of a response cookie.

Storage I/O must use the request context. Cancellation after a completed write does not undo that write. Authentication rejects providers without `RotatingStore` during initialization. Custom implementations must satisfy its revocation and concurrency rules, not merely implement a method with that name.

`session.New` calls the selected store's `New` method and caches only a successful result. `Session.End` creates an unsaved replacement before revoking the old session. If creation or revocation fails, the request-local session remains unchanged. A successful revocation installs the provider's replacement and clears the old data.

## Reading and saving

The following handler increments a counter. `DecodeValue` leaves the destination unchanged when the key is absent and returns an error if the value cannot be decoded into the requested type.

```go
func countVisits(w http.ResponseWriter, r *http.Request) {
    s, err := session.Get(r, "visits")
    if err != nil {
        http.Error(w, "Unable to load session", http.StatusInternalServerError)
        return
    }

    var count int
    if err := s.DecodeValue("count", &count); err != nil {
        http.Error(w, "Invalid counter", http.StatusInternalServerError)
        return
    }
    s.Data["count"] = count + 1
    if err := s.Save(w, r); err != nil {
        http.Error(w, "Unable to save session", http.StatusInternalServerError)
        return
    }

    fmt.Fprintf(w, "Visit %d", count+1)
}
```

Save before writing response headers or a body. A successful save sets `IsNew` to false and publishes `ExpiresAt`. Later saves preserve expiry. A failed save leaves the session unchanged and issues no cookie. Success does not guarantee browser receipt. Simple string values can also be read directly with a checked type assertion: `title, ok := s.Data["title"].(string)`.

## Rotation and logout

`s.Rotate(w, r, data)` replaces a SQLite session with a new ID and data in one transaction. Failure leaves the stored session valid and issues no cookie. Cookie-only stores do not support revocable rotation.

`s.End(w, r)` revokes a SQLite session and expires its browser cookie. For cookie-only sessions, it only expires the browser cookie. After successful logout, request-local session data is cleared. Ending an already deleted SQLite session is safe.

The [cleanup command](../../../cmd/cleanup) removes expired SQLite sessions and account tokens in bounded batches. Expiration checks reject expired records regardless of whether cleanup has run.

## Ownership and extension

`application.New` supplies the built-in providers and exposes the configured manager as `app.Sessions`. Use `Options.SessionProviders` to add or replace providers. For lower-level setup, choose the available providers explicitly:

```go
providers := map[string]session.ConfiguredProvider{
    "cookiestore": cookie.ConfiguredProvider(*cfg),
    "sqlitestore": sqlite.ConfiguredProvider(*cfg),
}
manager, err := session.NewConfiguredManager(ctx, cfg, providers)
```

`http.session.types` selects from those providers. Provider names are case-insensitive. `NewConfiguredManager` validates configuration and opens each selected provider once. The manager owns the stores and must be closed after requests stop. Failed setup closes acquired resources. For programmatic setup without configuration, use `NewSessionManager` with store factories directly.

Custom providers supply `ConfiguredProvider` callbacks. Validation performs no I/O, and callbacks receive private option maps. `CleanupConfiguredStores` uses the same selection with maintenance factories, without requiring signing keys or initializing schema. [configured.SessionProviders](../configured/sessions.go) supplies the built-in catalog used by startup and the cleanup command.

Each SQLite store owns its pool and prepares its table, expiration index, and version metadata in one transaction. Its pool uses one connection to preserve `:memory:` databases and serialize session writes. Session-store transactions use `BEGIN IMMEDIATE`, overriding `_txlock`, so concurrent initializations acquire the write lock before inspecting storage. Separate managers use separate pools, even when configured for the same file. They share session records only if configured for the same database and table.

Startup checks writable `id` and `session` columns with text affinity and `expires_at`, `created_at`, and `updated_at` columns declared as `DATE`, `DATETIME`, or `TIMESTAMP`. The table must have a non-partial unique key on `id` alone with binary comparison. Nullable declarations remain supported. Each configured table requires clean schema version 1. Setup does not alter existing columns, repair indexes, or validate stored values. The selected serializer checks payload formats when sessions are read.

### SQLite schema migrations

First startup executes the [initial migration](sqlite/migrations/000001_sessions.up.sql), substitutes the configured table and index names, and records version 1. Version metadata uses `hyperserver_session_version_<table>`. For example, `sessions` uses `hyperserver_session_version_sessions`. Different tables have independent versions. SQLite's identifier case rules apply to both table and metadata names.

Later migrations run explicitly with the [separately installed CLI](../../../docs/storage.md#account-schema-migrations). For the default `sessions` table, run from the repository root:

```sh
migrate -path pkg/services/session/sqlite/migrations \
  -database 'sqlite3://file:///absolute/path/database.db?x-migrations-table=hyperserver_session_version_sessions&_foreign_keys=on' up
```

For a custom table, make a deployment-local copy of the migration files. Change the quoted `"sessions"` and `"sessions_expiration"` identifiers to your table and index names in the up/down SQL. Set `x-migrations-table` to `hyperserver_session_version_<table>`. If the metadata name needs SQL quoting, include the double quotes in the query parameter value and URL-encode that value. Stop writers and run only one migration process per database at a time.

Missing, malformed, dirty, or unsupported versions stop startup without changing storage. Recreate disposable unversioned development databases. After `down 1`, startup rejects the empty version marker until an explicit `up` recreates the schema.

Built-in constructors accept `cookie.Options` or `sqlite.Options`, not application configuration. Application setup selects enabled providers and resolves SQLite paths before construction. Constructors reject signing keys shorter than 32 bytes, placeholders, and lifetimes shorter than one second. Successful constructors return enabled stores. SQLite also requires a connection string and table name. See [signing-key generation](../../../docs/application.md#signing-keys).

[application.New](../../application/application.go) resolves paths and creates the session manager before initializing modules. Services and modules share `app.initializationTimeout`, defaulting to ten seconds. The constructor registers cleanup for managers it creates. Managers supplied through `Options.Services.Sessions` remain caller-owned.

With lower-level setup, register owned managers with `app.OnShutdown` before their consumers initialize. Shutdown hooks close consumers before session stores after requests drain. Give each resource one owner. A closed manager must not be reused.

Retrieved sessions are cached in the request context. Do not share their mutable data across concurrent requests. Set manager mappings before serving requests, not while requests are active.

Custom stores implement `SessionStore`. Stores supporting revocable rotation also implement `RotatingStore`. Providers with owned resources implement `io.Closer`. Supply factories directly without importing the built-in providers:

```go
manager, err := session.NewSessionManager(ctx,
    map[string]string{"default": "custom", "auth-user-session": "custom"},
    map[string]session.StoreFactory{"custom": openMySessionStore},
)
if err != nil {
    return err
}
app.OnShutdown("sessions", func(context.Context) error { return manager.Close() })
```

Pass the same manager to auth factories and `authmiddleware.LoadSessionManagement(accounts, manager)`, or use `session.WithSessionManager(manager)` when authentication is not needed. Register cleanup only if this application owns the manager.

`openMySessionStore` must accept `context.Context` and return `(session.SessionStore, error)`. Success means the store is ready for use. If the store is unavailable, the factory must release partial resources and return an error. The manager owns successful results, closes them in reverse initialization order, and closes each selected provider only once. Provider names are case-insensitive. Session names are case-sensitive.

A named selection overrides `default`. Unknown selections and ambiguous provider names fail before any factory runs.

Providers can use `NewSession` to create an unsaved value. Pass the same serializer to `EncodedData` and `LoadSession`. A nil serializer selects JSON. Providers remain responsible for identity and expiration checks.

Maintenance uses the same provider names and session mappings, but separate factories that open existing storage without creating or migrating it:

```go
count, err := session.CleanupExpired(ctx, sessionTypes,
    map[string]session.MaintenanceFactory{"custom": openMySessionMaintenance},
    500,
)
```

`openMySessionMaintenance` returns `(session.ExpiredSessionMaintenance, func() error, error)`. Cleanup calls the returned close function even if opening fails. Return a nil maintenance value when no cleanup is needed, as with cookie-only sessions. A missing or nil factory means maintenance is unsupported. Aliases share one batch, unselected factories are not called, and independent providers are still attempted after a failure.

## Replacing serialization

Set a built-in store's `Serializer` in its factory, before serving requests. For example, supply this factory to `session.NewSessionManager`:

```go
openSessions := func(ctx context.Context) (session.SessionStore, error) {
    store, err := sqlite.NewSQLiteStore(ctx, sqlite.Options{
        Connection:     sessionDatabasePath,
        Table:          "sessions",
        SigningKey:     sessionSigningKey,
        TokenLifetime:  24 * time.Hour,
        CookieLifetime: 24 * time.Hour,
    })
    if err != nil {
        return nil, err
    }
    store.Serializer = mySerializer
    return store, nil
}
```

`CookieStore` supports the same field. Nil selects `session.JSONSerializer{}`. Serializer selection belongs to application setup, not a process-wide registry or YAML type name. Do not change it while requests are active.

A serializer implements `Encode(map[string]any) ([]byte, error)` and `Decode([]byte) (map[string]any, error)`. Implementations must be safe for concurrent calls, preserve numeric precision, return independent maps, and reject malformed data. They must also bound their own allocations and decoding work. The byte limit alone cannot prevent a custom decoder from expanding a small input into a large object.

Base64 framing, cookie signing, expiration, revocation, and size limits stay outside serialization. Neither selecting a serializer nor encoding data encrypts it. Decode failures return errors rather than new sessions. Encode failures issue no cookie and leave stored data unchanged.

The default payload is `[1,{"key":"value"}]` wrapped in padded URL-safe base64. The array contains the payload version and the application's data object. Object keys, including `version` and `data`, belong to the application. Unknown versions and malformed payloads return errors. Payload versions are independent of database schema versions. SQLite stores the payload in its session table. Cookie-only sessions carry it in signed claims.

The default reader also accepts unversioned JSON objects. Reading leaves storage unchanged. A successful save writes version 1 without changing the session ID or expiration. The four-byte version wrapper counts toward existing limits. If a rewrite exceeds the payload or cookie limit, saving fails without replacing the stored data or issuing a cookie. Reduce the session's data or invalidate the session before retrying. Applications that cannot read version 1 must invalidate affected sessions before rolling back to an older serializer.

A custom serializer owns its format and compatibility rules. The stores do not add a version wrapper to custom output. A replacement must explicitly read existing data or the application must invalidate those sessions before switching formats. SQLite cookies contain session identifiers rather than stored payloads, so changing the stored payload format does not require changing those cookies.
