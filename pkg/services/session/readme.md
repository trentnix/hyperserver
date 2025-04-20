# Sessions and Session Management

Session management has been designed with the goal of being easy to use, extend, and understand. The design and implementation will look familiar if you've seen the source code of [gorilla/sessions](https://www.github.com/gorilla/sessions). It served as both inspiration and reference.

Why didn't I just use `gorilla/sessions`? Basically, I had a moment where I was tired of relying heavily on external libraries. Modern software often seems less like a cogent, coherent solution and more like a patchwork of libraries welded together. While there are benefits to this approach, it often makes projects harder to understand and maintain. It drags along dead bytes servicing unused features and unnecessary functionality. It introduces complexity, increases binary size, and expands the dependency tree. I believed a simpler implementation was possible.

This doesn't make my approach is superior. `gorilla/sessions` is more flexible, likely more secure, and generally excellent. Feel free to use it if that's what you prefer.

## Configuring Session Management and Session Storage

To configure session management, refer to the `config-template.yaml` file in the `/config` folder or review the snippet below:

```yaml
session:
    key: <your JWT encryption key goes here>
    tokenAge: "24h"
    cookieAge: "24h"
    stores:
        cookieStore:
        enabled: "true"
    sqliteStore:
        enabled: "true"
        connection: "hyperserver.db?_journal=WAL&_timeout=5000&_fk=true"
        sessionTable: "session"
    types:
        default: sqliteStore
        visit: cookieStore
```

Session-related configuration is found within the `session` section under the `http` settings.

General cookie-related settings include:

- **key**: Used for encoding and decoding JWT session data stored in cookies.
- **tokenAge**: Lifetime of the JWT token.
- **cookieAge**: Lifetime of the browser cookie used for session identification.

Two sub-sections follow: **stores** and **types**.

The **stores** subsection defines available session storage implementations and their configurations:

- **CookieStore**: Serializes session data into a JWT stored in the browser cookie. Note that browser cookies have size limitations, so large session data may require alternative storage.

- **SQLiteStore**: Serializes session data as JSON stored in a SQLite database table. This implementation could be adapted for other database systems if desired.

The **types** subsection maps session names to specific session storage implementations. Each session name should be unique within this mapping. You can create custom session storage implementations and map them here accordingly.

The `default` key specifies which session storage is used when no explicit mapping exists. In the provided example, sessions not explicitly mapped (e.g., `user`) default to `SQLiteStore`.

### Retrieving a Session

To retrieve a session, use the `Get` method from the `session` package:

```go
s, err := session.Get(r, "session-name-goes-here")
```

`Get` accepts two parameters: an `*http.Request` and a string representing the session's name. If the session exists, it returns a pointer to the existing session; otherwise, it returns a pointer to a new, empty session.

Session data is stored as strings in the `Session.Data` map:

```go
s.Data["title"] = "This is the title"
```

Retrieve session data using the same map:

```go
title = s.Data["title"]
```

Currently, only strings can be stored in sessions. To store complex data types, consider serializing them to JSON strings first.

If this limitation is a problem for you, consider using [gorilla/session](https://www.github.com/gorilla/sessions).

### Create a New Session

To explicitly create a new session (which overwrites any existing session with the same name upon saving), use the `New` method in the `session` package:

```go
s, err := session.New(r, "session-name-goes-here")
```

### Saving a Session

Persist a session across requests by saving it:

```go
err := s.Save(r, w)
```

`Save` takes an `*http.Request` and a `http.ResponseWriter`, enabling session persistence via cookies encoded as JWTs. Encoding sessions as JWTs adds a layer of security but does not ensure absolute security.

Depending on your storage options, cookies might be the primary storage method for session data. Be cautious with sensitive data, as it is generally considered insecure to store sensitive information directly in browser cookies even if it is encrypted.

You can inspect the `Session.store` property to determine which storage mechanism is being used for any given Session.

### Ending a Session

To terminate an active session and expire the associated cookie, use the `End` method:

```go
err := s.End(r, w)
```

End requires an `*http.Request` and `http.ResponseWriter` to update the browser cookie.

### Example Handler

The following example demonstrates retrieving a session, updating a counter, and saving it back:

```go
func (m *SiteModule) SessionExample(w http.ResponseWriter, r *http.Request) {
    const counterKey = "counter"

    // retrieve session
    mySession, _ := session.Get(r, "counterSession")

    // extract the counter value
    counter, _ := strconv.Atoi(mySession.Data[counterKey])
    
    // prepare the page content
    page := content.NewManagedContent(r)
    page.AddContentTemplate(homeContent)
    page.Title = fmt.Sprintf("# of My Visits: %d", counter)

    // increment the counter and save it to the session
    mySession.Data[counterKey] = fmt.Sprintf("%d", counter+1)
    mySession.Save(r, w)

    // render content
    renderErr := page.Render(w, r)
}
```

This example highlights converting integers to strings for storage, session management, and rendering output. Error handling is minimal for brevity.

## Adding a New Session Store

Currently available session stores include **CookieStore** and **SQLiteStore**.

To add a custom session store, implement the `SessionStore` interface, add any configuration to your application configuration (e.g. `config.yaml`), then update `SessionManager.getStore()` to recognize your implementation based on configuration settings.

## Session Caching

Sessions (or their pointers) are cached in the `*http.Request` context. Thus, repeated retrievals of a session won't cause multiple fetches from the session store. Upon saving, session data is stored both in the session's storage implementation and the request context.

## Future (Potential) Roadmap

There are no actual plans for what needs to be improved, but the list below are a few items off the top of my head that came to mind when I considered how session management might be improved.

- Supporting multiple JWT keys for key rotation.
- Support non-string data types in session storage
- Allowing regular expression-based mappings from session names to storage types.
- Improving configuration validation and providing detailed error messages.
- Implement a Redis-backed (or similar) session store.
- Supporting session identification methods beyond HTTP cookies.
