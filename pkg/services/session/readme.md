# Sessions

The `session` package defines session values, storage contracts, and request handling. Optional providers implement [cookie](cookie/store.go) and [SQLite](sqlite/store.go) storage. Attach a `SessionManager` to each request before using `session.Get` or `session.New`. See [config-template.yaml](../../../config-template.yaml) for `http.session` configuration.

## Storage and limits

`CookieStore` puts session data in a signed JWT cookie. Signing prevents modification but does not encrypt the contents. Do not store passwords, recovery tokens, credentials, or other secrets in cookie sessions.

`SQLiteStore` stores session data in SQLite. Its signed cookie contains a session ID and token metadata, not session data. Authentication requires a store that supports revocable rotation, such as SQLite. Cookie-only sessions cannot revoke captured cookies.

Both stores default to JSON encoded in URL-safe base64. Each store can select a different [Serializer](serializer.go). `Session.Data` still accepts JSON-compatible values. The JSON serializer restores numbers as `json.Number` to preserve integer precision. Go structs and concrete numeric types are not preserved automatically. Use `DecodeValue` to read values into the expected type. That helper uses JSON conversion independently of the storage format.

- `MaxSessionDataSize` limits serialized data to 64 KiB before base64 encoding. Reads reject oversized data before calling the serializer. Saves and rotations reject oversized serializer output before writing storage.
- `MaxCookieSize` limits each cookie to 4 KiB, including its name and outgoing attributes. JWT and base64 overhead reduce the space available for cookie-store data.
- Invalid or oversized data returns an error. A missing or expired session returns a new, empty session.

These limits apply to each session, not the total size of an HTTP request. Incoming request-header limits are a separate server concern.

## Storage guarantees

Providers serve concurrent requests, but each request owns its mutable session data. SQLite saves replace the entire stored payload. If two requests load the same session and save changes, the later write replaces the earlier data. There is no merge or version check. Do not use session read-modify-write for reliable counters or balances. Cookie-only saves likewise depend on which response the browser applies last.

SQLite creates use unique IDs and reject collisions without replacing existing data. Saving a deleted session fails. Reads reject expired sessions even before cleanup. Saves preserve the original expiration and cannot revive an expired session. Rotation creates a new lifetime and revokes the previous ID atomically. Only one concurrent rotation of a stored session can succeed.

SQLite issues cookies only after successful writes. Persistent database files survive normal close and reopen. Crash durability depends on SQLite settings and the filesystem. In-memory databases do not survive closing their connections. Cookie-only storage has no server-side persistence or revocation. Its data lasts only as long as the browser retains a valid cookie. Neither provider can guarantee delivery of a response cookie.

Storage I/O must use the request context. Cancellation after a completed write does not undo that write. Authentication rejects providers without `RotatingStore` during initialization. Custom implementations must satisfy its revocation and concurrency rules, not merely implement a method with that name.

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

Save before writing response headers or a body. Simple string values can also be read directly with a checked type assertion: `title, ok := s.Data["title"].(string)`.

## Rotation and logout

`s.Rotate(w, r, data)` replaces a SQLite session with a new ID and data in one transaction. Failure leaves the stored session valid and issues no cookie. Cookie-only stores do not support revocable rotation.

`s.End(w, r)` revokes a SQLite session and expires its browser cookie. For cookie-only sessions, it only expires the browser cookie. After successful logout, request-local session data is cleared. Ending an already deleted SQLite session is safe.

The [cleanup command](../../../cmd/cleanup) removes expired SQLite sessions and account tokens in bounded batches. Expiration checks reject expired records regardless of whether cleanup has run.

## Ownership and extension

Session contracts live in `session`. The built-in implementations live in [`session/sqlite`](sqlite/store.go) and [`session/cookie`](cookie/store.go), following the same layout as `user` and `user/sqlite`. The reference application's [setup](../../../cmd/internal/setup/sessions.go) selects configured providers. Other applications supply factories to `session.NewSessionManager` directly. The manager copies session mappings and initializes each selected provider once. Requests reuse those providers. Unselected providers do not open storage. Failed setup returns an error and closes acquired resources.

Each SQLite store owns its pool and prepares its table and expiration index in one transaction. Its pool uses one connection to preserve `:memory:` databases and serialize session writes. Separate managers use separate pools, even when configured for the same file. They share session records only if configured for the same database and table.

Built-in constructors accept `cookie.Options` or `sqlite.Options`, not application configuration. Application setup selects enabled providers and resolves SQLite paths before construction. Constructors reject missing or placeholder signing keys and lifetimes shorter than one second. Successful constructors return enabled stores. SQLite also requires a connection string and table name.

The reference application's [session setup](../../../cmd/web/sessions.go) supplies the manager after resolving its working directory and before initializing modules. Setup has a ten-second timeout, bounded by the caller's context. Setup registers cleanup for managers it creates. A supplied manager keeps its existing owner unless setup explicitly transfers ownership with `ownSessions`. The core has no session-manager field.

Register owned managers with `app.OnShutdown` before their consumers initialize. Shutdown hooks close consumers before session stores after requests drain. Give each resource one owner. A closed manager must not be reused.

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

`openMySessionStore` must accept `context.Context` and return `(session.SessionStore, error)`. It must release partial resources on failure. The manager owns successful results, closes them in reverse initialization order, and closes each selected provider only once. Provider names are case-insensitive. Session names are case-sensitive. A named selection overrides `default`. Unknown selections and ambiguous provider names fail before any factory runs.

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

The default serializer reads the existing JSON format unchanged. A replacement must explicitly read existing data or the application must invalidate those sessions before switching formats. There is no automatic format detection or migration. A replacement can read the previous format and write its new format when a session is next saved. SQLite cookies contain only session identifiers, so changing the stored payload format does not require changing those cookies.
