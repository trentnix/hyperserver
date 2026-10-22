# Blog example

A personal Markdown blog with a responsive profile sidebar, post archive, About and Projects pages, RSS, image uploads, and an administrator editor. Authentication pages use the blog's layout and styles.

## Run locally

From this directory:

```sh
cp config-template.yaml config.yaml
cp modules/blog/config-template.yaml modules/blog/config.yaml
openssl rand -hex 32
openssl rand -hex 32
```

Put the two different generated keys in `http.session.jwtKey` and `auth.jwtKey`. Both `config.yaml` files and SQLite databases are ignored by Git. Keep any existing local configuration instead of overwriting it.

Start a local SMTP inbox if one is not already running:

```sh
docker run --rm --name blog-mailpit -p 127.0.0.1:1025:1025 -p 127.0.0.1:8025:8025 -e MP_SMTP_AUTH_ACCEPT_ANY=1 -e MP_SMTP_AUTH_ALLOW_INSECURE=1 axllent/mailpit
```

Then start the blog:

```sh
GOWORK=off go run .
```

Open <http://127.0.0.1:8086/auth/register/email> and register `trentnix@gmail.com`. Other addresses cannot register or log in. Open the verification message in <http://127.0.0.1:8025>, confirm the address, and log in. The **New post** link appears for the verified administrator. Mailpit captures mail locally and does not deliver it to Gmail. Stop the server with Ctrl+C.

Set `auth.registrationEnabled: false` after creating the administrator if desired. Login, recovery, and verification remain available. For deployment, configure real SMTP delivery, independent random signing keys, HTTPS, and `http.publicOrigin`.

## Structure

`application.go` selects the blog and authentication modules, the response layout, and the blog's email provider. HyperServer opens configured account and session services and owns their lifetime. `internal/blog` owns the administrator rule, templates, handlers, and its SQLite post/image repository. The module loads `modules/blog/config.yaml` through the framework. Storage paths are relative to the application configuration directory.

EasyMDE supplies the editor toolbar and upload controls. The server validates all submissions and renders preview and published Markdown with Goldmark. Raw HTML is disabled. Uploaded images remain stored when posts are deleted. This prototype does not import old blog databases.

```sh
GOWORK=off go test ./...
GOWORK=off go vet ./...
HS_TEST_FIREFOX=firefox GOWORK=off go test ./internal/blog -run '^TestBrowser' -count=1
```
