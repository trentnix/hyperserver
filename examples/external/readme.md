# External Go module

This application has its own `go.mod` and pins a public HyperServer commit. It serves a greeting form and persistent notes using public framework packages. Copy this directory outside the repository, then run from that directory:

```sh
go test ./...
go run .
```

Use Go 1.27.2 or newer. The SQLite provider and its tests require CGO and a C compiler. Open http://127.0.0.1:8083/notes. The default provider stores notes as JSON files in `notes-data`. To use SQLite instead:

```sh
go run . -notes-store sqlite -notes-path ./notes.db
```

`-notes-path` selects the file directory or SQLite database. Relative paths use the working directory. Switching providers does not transfer existing data. Each provider can reopen its own data after a restart.

This is a loopback-only example with shared, unauthenticated notes. Do not use it for sensitive data or expose it publicly. It needs no configuration file, sessions, or mail. `example.com/greeting` is the application's Go module path, not a server it contacts.

## Storage and module setup

[main.go](main.go) owns configuration, rendering defaults, module activation, HTTP limits, and shutdown. [storage.go](storage.go) selects the notes provider. The application closes the SQLite pool after requests drain. The file provider keeps no handles open between calls.

The [notes repository interface](notes/notes.go) has two operations: create a note and list recent notes. [The handlers](notes/module.go) use the same interface with either provider. Notes are append-only, limited to 1,000 Unicode code points, and displayed newest first, up to 100 at a time. Both providers reject duplicate IDs rather than overwriting notes.

Importing each module registers its factory. Setup supplies the notes repository before activating the import catalog. Applications with optional modules should select their catalog explicitly. Templates are embedded and prepared during initialization. Request input and stored notes are escaped when rendered.

The file provider writes each JSON record inside a temporary directory, then renames the directory to `<id>.note`. It requires atomic directory renames, not hard links. Existing nonempty record directories cannot be overwritten. Keep storage private through directory permissions or mount settings. Older `<id>.json` records remain readable, but stop old application processes before using the new writer.

Listings scan records in bounded batches and report corrupt records as errors. This provider is intended for small local datasets and does not guarantee durability across power loss. The SQLite provider creates its initial table and index. Neither provider includes schema upgrades or data migration tooling.

## HTTP behavior

The forms work with ordinary browser navigation. This example does not load HTMX JavaScript. Exercise fragment responses and redirects directly:

```sh
curl -H 'HX-Request: true' 'http://127.0.0.1:8083/?name=Ada'
curl -H 'HX-Request: true' http://127.0.0.1:8083/notes
curl -i -H 'HX-Request: true' -d 'text=First note' http://127.0.0.1:8083/notes
curl -i -H 'HX-Request: true' -d 'text=' http://127.0.0.1:8083/notes
```

A successful POST returns 303 for ordinary navigation or 200 with `HX-Redirect: /notes` for HTMX. Field validation returns 200 with error markup. Malformed or oversized input receives 400 or 413. History-restoration requests receive a full page. Cross-origin writes are rejected, and responses use `Cache-Control: no-store`.

Build with `go build -o greeting .`. Embedded templates let the executable run from any directory. Set `-notes-path` to keep storage at a fixed location. `-port 0` selects an available loopback port.

The tests run the same repository contract and HTTP workflow against both providers, including concurrent writes, storage failures, and reopening saved notes. The repository's external-module regression test copies this example to a temporary directory and replaces only that copy's HyperServer dependency with the current checkout. The example itself has no local `replace` directive.
