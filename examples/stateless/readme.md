# Stateless greeting application

This small application uses HyperServer's module initialization, routing, and rendering without a database, sessions, authentication, or mail. It does not load configuration files or environment settings.

Run from the repository root:

```sh
go run ./examples/stateless
```

Open http://127.0.0.1:8082. The name form uses an ordinary GET request. Change the application name or port with `-name "My greeting app" -port 8083`.

The same route returns an HTML fragment when the request carries `HX-Request: true`:

```sh
curl -H 'HX-Request: true' 'http://127.0.0.1:8082/?name=Ada'
```

The example includes no JavaScript or HTMX download. The fragment response demonstrates the server-side behavior. It does not make the form interactive without a page reload.

## Composition

- [app.go](app.go) selects the greeting module explicitly and registers its shutdown hook. Other modules imported into the same process do not become active in this application.
- [pages/module.go](pages/module.go) self-registers a factory and owns its routes and embedded templates. Each application gets a fresh module and content manager.
- [main.go](main.go) owns the loopback listener and HTTP shutdown. Interrupting the process drains requests before application cleanup. `-shutdown-timeout` defaults to `10s`. If draining fails, the command closes connections and exits without closing services beneath active handlers.

The templates are embedded. A built executable can run from an empty directory without the repository beside it:

```sh
go build -o ./tmp/stateless ./examples/stateless
```

The tests run the greeting application beside the reference site's module in one process, check independent rendering and shutdown, and verify that stateless requests activate no optional services or cookies. The reference-site test uses its own SQLite database. Broader replacement-provider tests remain separate work.

This is a local composition example, not a production deployment template. Its use of the shared application container does not remove optional service packages from the Go dependency graph.
