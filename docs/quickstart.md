# Standalone application

This example uses `application.New`, standard HTTP handlers, and prepared HTML templates. Ordinary navigation renders a full page. HTMX updates only the greeting. It opens no database, session, authentication, or mail service.

Use Go 1.27.4. This example also builds with `CGO_ENABLED=0`. Applications that select the built-in SQLite providers still require CGO and a C compiler.

## Create the application

Create a separate directory. Replace `FRAMEWORK_REVISION` with a tested framework commit or tag that supports explicit storage provider catalogs:

```sh
mkdir hello-hyperserver
cd hello-hyperserver
go mod init example.com/hello
go get github.com/trentnix/hyperserver@FRAMEWORK_REVISION
mkdir -p templates static
curl --fail --location https://cdn.jsdelivr.net/npm/htmx.org@4.0.2/dist/htmx.min.js --output static/htmx.min.js
```

Keep the downloaded HTMX asset with your application. This example targets HTMX 4. Pin and test framework and browser-library revisions when upgrading.

Create `config.yaml`:

```yaml
http:
  listenHost: "127.0.0.1"
  port: 8080
  readTimeout: 10s
  writeTimeout: 30s
  idleTimeout: 60s
```

Runtime configuration remains application-owned. Do not commit secrets or deployment-specific configuration.

Create `main.go`:

```go
package main

import (
    "context"
    "embed"
    "io/fs"
    "log"
    "net"
    "net/http"
    "os"
    "os/signal"
    "syscall"

    "github.com/trentnix/hyperserver/config"
    "github.com/trentnix/hyperserver/pkg/application"
    "github.com/trentnix/hyperserver/pkg/components/content"
    "github.com/trentnix/hyperserver/pkg/components/form"
    "github.com/trentnix/hyperserver/pkg/components/htmx"
    "github.com/trentnix/hyperserver/pkg/handlers"
    "github.com/trentnix/hyperserver/pkg/routing"
    "github.com/trentnix/hyperserver/pkg/server"
)

//go:embed templates static
var files embed.FS

type greetingModule struct {
    view *content.View
}

func (m *greetingModule) Init(_ context.Context, app *server.ApplicationServer) error {
    var err error
    m.view, err = content.PrepareView(app.ContentManager, "hello.greeting",
        content.TemplatePath{FS: files, Path: "templates/greeting.html"})
    return err
}

func (m *greetingModule) Routes(routes *routing.Routes) error {
    assets, err := fs.Sub(files, "static")
    if err != nil {
        return err
    }
    routes.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(assets)))
    routes.HandleFunc("GET /static/htmx-errors.js", htmx.ServeErrorScript)
    routes.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
        if err := form.Parse(r); err != nil {
            http.Error(w, err.Message, err.StatusCode)
            return
        }
        page := m.view.Content(r, r.Form.Get("name"))
        if err := page.Render(w, r); err != nil {
            page.HandleRenderError(w, r, "Unable to display the greeting", err)
        }
    })
    return routes.Err()
}

func run(ctx context.Context) error {
    cfg, err := config.GetConfig()
    if err != nil {
        return err
    }
    app, err := application.New(ctx, cfg, application.Options{
        ContentSecurityPolicy: "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'",
        Modules: func(_ context.Context, app *application.Application) ([]handlers.Descriptor, error) {
            app.ContentManager.AddPageLayout(files, "templates/page.html")
            app.ContentManager.AddHtmxLayout(files, "templates/fragment.html")
            return []handlers.Descriptor{{Name: "greeting", New: func() handlers.Handler {
                return &greetingModule{}
            }}}, nil
        },
    })
    if err != nil {
        return err
    }
    return app.Run(ctx, func(ctx context.Context) (*http.Server, net.Listener, error) {
        httpServer, err := server.NewHTTPServer(app.Config.HTTP, app.Handler)
        if err != nil {
            return nil, nil, err
        }
        listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", httpServer.Addr)
        if err == nil {
            log.Printf("Listening on %s", listener.Addr())
        }
        return httpServer, listener, err
    })
}

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    if err := run(ctx); err != nil {
        log.Fatal(err)
    }
}
```

The module factory creates fresh state. `Init` prepares templates once. `Routes` registers body-limited standard handlers. `app.Run` drains requests and closes application-owned resources when the process receives an interrupt or termination signal.

## Add templates

Create `templates/page.html`:

```gotemplate
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="htmx-config" content='{"includeIndicatorCSS":false}'>
  <title>Hello</title>
  <script src="/static/htmx.min.js" defer></script>
  <script src="/static/htmx-errors.js" defer></script>
</head>
<body>
  <h1>Hello</h1>
  <form action="/" method="get" hx-get="/" hx-target="#greeting" hx-swap="outerHTML">
    <label>Name <input name="name" value="{{.Data}}"></label>
    <button type="submit">Greet</button>
  </form>
  {{renderPartial .PartialName .}}
</body>
</html>
```

Create `templates/fragment.html`:

```gotemplate
{{renderPartial .PartialName .}}
```

Create `templates/greeting.html`:

```gotemplate
{{define "hello.greeting"}}
<p id="greeting">{{if .Data}}Hello, {{.Data}}.{{else}}Enter your name.{{end}}</p>
{{end}}
```

`html/template` escapes the name in both the input and greeting. A request receives separate response data while sharing immutable prepared templates. History restoration receives the full page.

The shared error script displays rejected HTMX requests beside the form. It requires no application-specific JavaScript.

## Run and check

```sh
go mod tidy
go run .
```

Open `http://127.0.0.1:8080` and submit a name. With JavaScript enabled, only the greeting changes. With JavaScript disabled, the ordinary GET form reloads the page and preserves the name.

Check the response modes from another terminal:

```sh
curl 'http://127.0.0.1:8080/?name=Reader'
curl -H 'HX-Request: true' 'http://127.0.0.1:8080/?name=Reader'
curl -H 'HX-Request: true' -H 'HX-History-Restore-Request: true' 'http://127.0.0.1:8080/?name=Reader'
```

The first and third responses contain the document. The second contains only the greeting. Stop the server with Ctrl+C. Continue with [application setup](application.md) and [rendering and forms](../pkg/components/readme.md) for persistence, validation, notifications, and authentication.
