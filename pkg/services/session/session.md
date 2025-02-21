# Sessions and Session Management

In HyperServer (I hope that name holds up considering how modest the actual implementation is), session management has been designed to be easy to use, easy to extend, and easy to understand. The design and implementation will look similar if you're familiar with the source code in [gorilla/session](https://www.github.com/gorilla/sessions). It was a great inspiration and guide.

That prompts the quest, why didn't I use gorilla/mux? I was just tired of using external libraries. When you use gorilla/sessions, it uses gorilla/securecookie, and I didn't want to go down the rabbit hold. I also thought a simpler implementation was possible and I wanted to make sure I understood the implementation in detail. If you want to use gorilla/sessions, there's nothing stopping you. It's more flexible and more secure, but you may not need all that.

## Configuring Session Management and Session Storage

TBD

## Session Management

To create and use a Session, you'll need an instance of SessionManager. An instance of SessionManager is available in the ApplicationServer object at ApplicationServer.Session.

### Get a Session

To get a \*Session instance from the SessionManager, use the Get method:

    sessionManager.Get(r, "session-name-goes-here")

Get takes two parameters, an \*http.Request and a string that identifies the name of the session. The name can be used to manage the Session and any data that needs to be stored in a session.

If the Session exists, a pointer to the existing Session is returned. If the Session does not exist, a pointer to a new, empty Session is returned.

A Session has a simple interface and data can be stored in a Session in a string map called Session.Data.

    session.Data["title"] = "This is the title"

Both the map key and map values are strings. To retrieve data from a Session, just access the same map:

    title = session.Data["title"]

No other data types can be stored in a session, currently. If you need to store objects or complex data in a Session, consider serializing the data you want to store into a JSON string and saving that to the Session.

If this limitation is a deal breaker for you, consider using [gorilla/session](https://www.github.com/gorilla/sessions) instead.

### Create a New Session

To create a new Session, use the New method:

    sessionManager.New(r, "session-name-goes-here")

A pointer to a new Session is returned and, if saved, will overwrite any existing session with the same name.

### Saving a Session

To save a Session to session storage (so it will persist between requests), call Save:

    err := session.Save(r, w)

Save takes both a \*http.Request instance and a http.ResponseWriter instance as parameters. This allows the session to be written to a browser cookie that can be read from a subsequent request. The Cookie value is encoded as a [JWT](https://jwt.io/) to add some measure of security. That doesn't mean you should consider this approach to be secure.

Depending on what session storage mechanism that is used, the browser cookie may be the primary session storage mechanism. As a result, you should be careful not to store sensitive data in a Session without confirm that the Session is not writing sensitive Session data (other than, maybe, an identifier) to the browser cookie.

Since a Session has a Session.store member, you can infer which storage mechanism is being used if you want to prevent a Session from being created if, say, the CookieStore session store implementation is being used.

### Ending a Session

To end an active Session (and to expire any browser cookies) simply call the End method on a given Session:

    err := session.End(r, w)

End takes both a \*http.Request instance and a http.ResponseWriter instance as parameters so the browser cookie can be appropriately updated.

## Adding a New Session Store

TBD

## Session Roadmap

TBD