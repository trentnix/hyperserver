# Configuration

The [loader](../config/load.go) reads the first `config.yaml` in `.`, `config`, `../config`, or `../../config`. Precedence is defaults, then file, then `HYPERSERVER_` environment values, including empty values. For example, `HYPERSERVER_MAIL_PASSWORD` overrides `mail.password`. Unknown settings and invalid values stop startup. Durations require units, except `0`.

Configuration loads once. Supplying `config.Config` directly bypasses file loading and its defaults. Commands resolve storage and TLS paths relative to the selected config file. `app.workingDirectory` overrides that base without changing the process directory.

Provider option keys keep their exact spelling in YAML and programmatic configuration. For environment overrides, use `__` between provider map entries to preserve option case and underscores:

```sh
HYPERSERVER_AUTH_ACCOUNTSTORAGE_OPTIONS__bucketName=accounts
HYPERSERVER_HTTP_SESSION_STORES__sqlitestore__sessionTable=sessions
HYPERSERVER_AUTH_SERVICES__custom__api_key=secret
```

The existing single-underscore format still addresses lowercase option names, such as `HYPERSERVER_AUTH_ACCOUNTSTORAGE_OPTIONS_CONNECTION`. Use the exact-key format for mixed-case keys. Provider names in session and auth maps remain case-insensitive. Option names must be nonempty and cannot contain dots.

The root [template](../config-template.yaml) configures the default blog. The [reference-site template](../examples/reference/config-template.yaml) documents additional settings and defaults. Only cookie-backed sessions require a [signing key](application.md#signing-keys). SQLite sessions and account tokens do not. The legacy `auth.jwtKey` setting is ignored.

## Registration and email

Registration is disabled by default. With `auth.enabled`, set `auth.registrationEnabled: true` to allow new accounts. Existing accounts can still log in and reset passwords when registration is disabled.

Verification and password reset use emailed, single-use links. If registration requires verification, startup checks delivery configuration. Failed delivery leaves the account pending verification. The user can log in to reach `/auth/request/verify` and resend the email.

Mail is sent during the request. `auth.resetMinimumResponseTime` defaults to `2s` for admitted, valid reset submissions. It delays fast responses, not slow ones, and does not cancel delivery. Set it to `0` to disable the wait. `mail.timeout` is independent. There is no durable delivery queue or automatic retry.

Authentication requires atomic session rotation and server-side revocation. The SQLite session provider supports both. Cookie-only sessions cannot provide authentication storage. Cookie-store data is signed, not encrypted, and must not contain secrets.
