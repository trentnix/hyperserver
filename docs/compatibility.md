# Compatibility

HyperServer is in `v0` development. No public framework interface is frozen. Applications must pin a version or commit and test upgrades. A documented extension point is supported at that revision, not a promise that its shape will stay unchanged.

## What can change

Public APIs, configuration, HTTP behavior, and stored formats can change during `v0`, including between commits. A breaking change must document the affected contract and the required application changes in the same change. Published release notes must identify those changes and link to upgrade instructions.

If an old Go API is retained during a transition, its documentation must include a `Deprecated:` comment naming the replacement. There is no guaranteed deprecation period during `v0`. An API can be removed directly when maintaining both versions would add unnecessary complexity. Security fixes do not wait for a deprecation period.

Storage compatibility is separate from source compatibility. A format change must state whether to migrate, invalidate disposable data, or recreate example storage. It must not silently discard durable application data. Follow the provider's [migration procedures](storage.md) and [session format rules](../pkg/services/session/readme.md#replacing-serialization).

## Login entry point

The auth module now owns `GET /login` and `POST /login`. Applications must remove their own `/login` handlers and point sign-in links at `/login`. Old `GET /auth/login` and enabled-provider `GET /auth/login/{authType}` URLs redirect there. Provider-specific login POST routes no longer exist. Callbacks, registration, password recovery, and stored data are unchanged.

Custom providers must return `auth.LoginResult` from `Login` instead of `*user.User`. Return `LoginResult{Account: account}` on success, `LoginResult{Form: markup}` for validation errors, or an empty result after writing a response. Replace `GetLogin` and `GetLoginForm` page handlers with `LoginForm(*http.Request) (template.HTML, error)` for inline controls or `GetLoginButton()` for a provider button. Forms must post to `/login` with a hidden `provider` field. Inline providers must link their alternative control to `/login?provider=<AuthType>`. The shared page now preserves alternatives, so providers must remove calls to `auth.LoginAlternatives`. See [provider composition](application.md#catalog-selection).

## Stabilization candidates

Module registration, lifecycle hooks, and `MailSender` are candidates for stabilization. The [blog](../modules/blog) uses framework setup without reference-site wiring, but shares the framework's Go module. Test a separately pinned external application before promising stable interfaces.

Keep account and session provider interfaces experimental while testing another backend. SQLite coverage alone does not establish that their contracts fit PostgreSQL or other storage platforms. Feature repositories remain application-owned unless reuse demonstrates a framework need.

A future stable release must identify its supported public surface and compatibility policy explicitly. Example internals and packages under `internal` are not reusable framework contracts.
