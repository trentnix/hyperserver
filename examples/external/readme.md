# External Go module

This greeting application has its own `go.mod` and pins a public HyperServer commit. Copy this directory anywhere outside the repository, then run from that directory:

```sh
go test ./...
go run .
```

Use Go 1.27.2 or newer. Open http://127.0.0.1:8083. The application uses embedded templates and public framework packages. It needs no configuration file, database, sessions, authentication, mail, or C compiler. `example.com/greeting` is the application's module path, not a server it contacts.

The form uses ordinary GET navigation and changes no data. This example does not load HTMX JavaScript. Exercise the server's fragment and validation responses directly:

```sh
curl -H 'HX-Request: true' 'http://127.0.0.1:8083/?name=Ada'
curl -H 'HX-Request: true' 'http://127.0.0.1:8083/?name='
```

Field validation returns 200 with error markup so HTMX can swap it normally. Malformed or oversized input receives 400 or 413. History-restoration requests receive a full page.

[main.go](main.go) owns configuration, rendering defaults, module activation, HTTP limits, and shutdown. Importing [greeting](greeting/module.go) registers its factory. This application activates its entire import catalog. Applications with optional modules should select their catalog explicitly. Templates are prepared during initialization, and request input is escaped when rendered.

Build with `go build -o greeting .`. The executable can run from an empty directory. `-port 0` selects an available loopback port. This is a local integration example, not a production setup. Access policy, secrets, persistence, TLS/proxy configuration, and deployment remain application choices.

The repository's external-module regression test copies this example to a temporary directory and replaces only that copy's HyperServer dependency with the current checkout. It builds the application and tests real HTTP responses and shutdown from an empty working directory. The example itself has no local `replace` directive. Authentication and persistent-feature examples remain separate work.
