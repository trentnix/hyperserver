# Reference site

This local-only application demonstrates contact forms, replaceable storage, health checks, and diagnostic routes. The [blog](../../docs/blog/readme.md) is the default application. Do not expose the reference site through a public listener, proxy, or tunnel.

From this directory, copy [config-template.yaml](config-template.yaml) to `config.yaml` only if that file does not already exist. Replace the two signing-key placeholders with different random secrets from `openssl rand -hex 32`. If authentication is enabled, set `auth.services.email.enabled: "true"`. Configure SMTP credentials for mail. Keep the listener on an explicit loopback IP.

Run from this directory so the reference site uses its own configuration and databases:

```sh
go run ../../cmd/reference
```

Use `-contact-directory ./contacts` to select the file-backed contact provider. See [storage](../../docs/storage.md) for provider requirements. The [container rehearsal](../../docs/deployment.md) also runs this application with disposable data.

Run tests from the repository root with `go test ./...` and `go vet ./...`.
