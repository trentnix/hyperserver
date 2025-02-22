# Sessions and Session Management

Session management has been designed with the intent to be easy to use, easy to extend, and easy to understand. The design and implementation will look familiar if you've seen the source code in [gorilla/session](https://www.github.com/gorilla/sessions). I used it both as an inspiration and a guide.

That prompts the question, why didn't I use gorilla/mux? The short answer is: I was just tired of using external libraries. Modern software seems like library after library is welded together. There are certainly benefits to this approach, but it can often make projects more difficult to understand. It also means that you get a lot of baggage as your depdendency tree and binary size grow with limited awareness as to why. I also thought a simpler implementation was possible and I wanted to make sure I understood the implementation in detail.

That doesn't mean I made a good decision, I just want to provide some context as to what I was thinking. If you want to use gorilla/sessions, there's nothing stopping you. It's more flexible, almost certainly more secure, and generally excellent. But you may not need all that comes with it, so do whatever make the best sense for your project.

## Configuring Session Management and Session Storage

To configure the server for session management, check out the config-template.yaml file in the /config folder or the snippet below.

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
            visit: sqliteStore
            user: sqliteStore
            cookie: cookieStore
            sqlite: sqliteStore

You should see a "session" section in the "http" section. This is where the magic happens.

General, browser cookie-related settings (including a key to encode the JWT) are at the root level and include the following settings:

- **key** - key used to encode and decode the data stored in the JWT set as the cookie value
- **tokenAge** - lifetime of the JWT token that is used
- **cookieAge** - lifetime of the browser cookie that is used to identify the session to retrieve

Two sub-sections remain: **stores** and **types**.

The **stores** sub-section contains the session storage implementations that can be used and configuration options related to those storage implementations.

A *CookieStore* implementation is available that will serialize session data to a JWT that will be saved in the requestor's browser. There is a limit on how much data can be stored on the client browser in a browser cookie. If you find yourself serializing a lot of data to your session, a different storage option is recommended.

Additionally, a *SQLiteStore* implementation is available that will serialize data to a SQLite database. The data stored is serialized to a JSON string and stored as a field in a database table. If SQLite is not preferred, this implementation could easily be modified to support other databases.

The **types** section maps a particular session name to a particular session storage implementation. Different session names can use different storage mechanisms, but each session name should only be listed once.

The key is the session name. The value is the session storage implementation to be used. If you roll your own custom session storage implementation, you'll need to map your session names accordingly.

## Session Management

To create and use a *Session*, you'll need an instance of *SessionManager*. An instance of *SessionManager* is available in the ApplicationServer object at ApplicationServer.Session.

### Get a Session

To get a \**Session* instance from the *SessionManager*, use the Get method:

    sessionManager.Get(r, "session-name-goes-here")

Get takes two parameters, an \*http.Request and a string that identifies the name of the *Session*. The name can be used to manage the Session and any data that needs to be stored in a session.

If the *Session* exists, a pointer to the existing Session is returned. If the Session does not exist, a pointer to a new, empty Session is returned.

A *Session* has a simple interface and data can be stored in a Session in a string map called Session.Data.

    session.Data["title"] = "This is the title"

Both the map key and map values are strings. To retrieve data from a *Session*, just access the same map:

    title = session.Data["title"]

No other data types can be stored in a session, currently. If you need to store objects or complex data in a Session, consider serializing the data you want to store into a JSON string and saving that to the Session.

If this limitation is a deal breaker for you, consider using [gorilla/session](https://www.github.com/gorilla/sessions) instead.

### Create a New Session

To create a new *Session*, use the New method:

    sessionManager.New(r, "session-name-goes-here")

A pointer to a new *Session* is returned and, if saved, will overwrite any existing session with the same name.

### Saving a Session

To save a *Session* to session storage (so it will persist between requests), call Save:

    err := session.Save(r, w)

Save takes both a \*http.Request instance and a http.ResponseWriter instance as parameters. This allows the session to be written to a browser cookie that can be read from a subsequent request. The Cookie value is encoded as a [JWT](https://jwt.io/) to add some measure of security. That doesn't mean you should consider this approach to be secure.

Depending on what session storage mechanism that is used, the browser cookie may be the primary session storage mechanism. As a result, you should be careful not to store sensitive data in a Session without confirm that the Session is not writing sensitive Session data (other than, maybe, an identifier) to the browser cookie.

Since a *Session* has a Session.store member, you can infer which storage mechanism is being used if you want to prevent a Session from being created if, say, the CookieStore session store implementation is being used.

### Ending a Session

To end an active *Session* (and to expire any browser cookies) simply call the End method on a given *Session*:

    err := session.End(r, w)

End takes both a \*http.Request instance and a http.ResponseWriter instance as parameters so the browser cookie can be appropriately updated.

## Adding a New Session Store

Both *CookieStore* and *SQLiteStore* are currently available implementations that can be inspected in the source code. Each implements the *SessionStore* interface, which is utilized by the *SessionManager* to create, get, and save Session instances. These implementations are fairly spartan and are intended to primarily illustrate how to implement a session store. If you want to roll your own *Session* storage implementation, you might find it easiest to copy one of those and modify as needed.

If you implement a new means to store a *Session*, you'll need to also modify the *SessionManager*.getStore() method. This evaluates the server configuration and returns the SessionStore implementation that corresponds to the provided Session name. The *SessionManager* will then be able to create an instance of your custom store when a session name is encountered that maps to your custom store (as defined in the configuration file).

## Future (Potential) Roadmap

There are no actual plans for what needs to be improved, but the list below are a few items off the top of my head that came to mind when I considered how session management might be improved.

- Support multiple JWT keys to enable key rotation
- Support a default session storage type for dynamically-named sessions
- Support regular expressions to map session names to storage types
- Improve configuration validation and provide detailed errors when session management isn't configured appropriately
- consider a Redis session store
- add support for non-HTTP cookie-based tokens
