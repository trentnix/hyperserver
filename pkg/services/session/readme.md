# Sessions

HyperServer provides cookie and SQLite session stores. Attach a `SessionManager` to each request before using `session.Get` or `session.New`. See [config-template.yaml](../../../config-template.yaml) for `http.session` configuration.

## Storage and limits

`CookieStore` puts session data in a signed JWT cookie. Signing prevents modification but does not encrypt the contents. Do not store passwords, recovery tokens, credentials, or other secrets in cookie sessions.

`SQLiteStore` stores session data in SQLite. Its signed cookie contains a session ID and token metadata, not session data. Authentication requires SQLite sessions because cookie-only sessions cannot revoke captured cookies.

Both stores use JSON encoded in URL-safe base64. Only that format is supported. `Session.Data` accepts JSON-compatible values. Loaded numbers use `json.Number` to preserve integer precision. Go structs and concrete numeric types are not preserved automatically. Use `DecodeValue` to read those values into the expected type.

- `MaxSessionDataSize` limits JSON data to 64 KiB before base64 encoding. Reads reject oversized data before JSON decoding. Saves and rotations reject oversized data before writing storage.
- `MaxCookieSize` limits each cookie to 4 KiB, including its name and outgoing attributes. JWT and base64 overhead reduce the space available for cookie-store data.
- Invalid or oversized data returns an error. A missing or expired session returns a new, empty session.

These limits apply to each session, not the total size of an HTTP request. Incoming request-header limits are a separate server concern.

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

Create a manager with `NewSessionManager(ctx, cfg)` before serving requests. It validates configuration, copies session mappings, and initializes each selected provider once. Requests reuse those providers. Unselected providers do not open storage. Failed setup returns an error, closes acquired resources, and allows a fresh construction attempt.

Each SQLite store owns its pool and prepares its table and expiration index in one transaction. Its pool uses one connection to preserve `:memory:` databases and serialize session writes. Separate managers use separate pools, even when configured for the same file. They share session records only if configured for the same database and table.

The reference application calls `ApplicationServer.InitializeSessions(ctx)` after resolving its working directory and before initializing modules. Setup has a ten-second timeout, bounded by the caller's context. `ApplicationServer.Shutdown()` closes session providers after HTTP requests drain. If you construct a manager or SQLite store directly, call its `Close()` after its consumers stop. A closed manager must not be reused.

Retrieved sessions are cached in the request context. Do not share their mutable data across concurrent requests. Set manager mappings before serving requests, not while requests are active.

Custom stores implement `SessionStore`. Stores supporting revocable rotation also implement `RotatingStore`. Providers with owned resources implement `io.Closer`. Provider selection currently requires wiring the implementation into `NewSessionManager`. Replaceable serialization remains [roadmap](../../../roadmap.md) work.
