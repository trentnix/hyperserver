# Google authentication

Google authentication is a provider beside email/password, not a separate account system. Both use the framework's local accounts and revocable browser sessions. The blog keeps its configured administrator restriction.

## Configure

1. Create an OAuth web application in Google Cloud. Configure its consent screen and obtain a client ID and client secret.
2. Register the exact callback URL: `http.publicOrigin` followed by `/auth/callback/google`. Use HTTPS outside loopback development.
3. Keep credentials in the ignored runtime configuration, not in source control. Enable the desired providers:

```yaml
http:
  publicOrigin: https://blog.example.com
  formActionOrigins: [https://accounts.google.com]
auth:
  services:
    email:
      enabled: "true"
    google:
      enabled: "true"
      clientID: YOUR_CLIENT_ID
      clientSecret: YOUR_CLIENT_SECRET
```

The default application catalog includes both registered providers. No changes to `cmd/web/application.go` are needed to enable email, Google, or both. The blog supplies one `Options.AuthPolicy` for its administrator restriction and onboarding behavior. The framework passes that policy to each provider. Applications using an explicit `Options.AuthProviders` catalog must include Google to make it available.

Set `http.formActionOrigins` to permit Google's form redirect destination. The framework adds these exact origins to the application's CSP `form-action` directive without changing other directives or inbound CSRF checks. The blog does not hard-code Google origins. Chromium browsers apply this restriction to form redirects. No Google scripts or third-party browser cookies are required.

`/login` normally shows all configured login methods. With exactly one registered account, the blog shows only methods available to that account: email for a stored password, Google for a connected Google identity, or both. If a provider's availability check fails, its control remains visible. Submitted login methods still work if presentation lookups fail. Credential validation and admission remain mandatory. Registration still offers all configured registration methods. Multiple-account sites and account storage without `user.SingleAccountReader` keep all configured login choices. The forms work without JavaScript. A disabled provider has no usable login or callback. Google-only authentication does not require a mail service. Existing global authentication and session settings still apply.

Existing SQLite account databases must use the current schema before starting the application. See [account migrations](storage.md#account-schema-migrations) for the required version and upgrade commands. Fresh databases initialize automatically.

## Existing accounts

Log in using the existing method. Open **Account** (`/auth/account`) and select **Google login**. If Google is not connected, select **Connect Google account**. The callback links Google's identity to the authenticated local account. Connecting Google preserves the password and invalidates older authenticated sessions. Already-connected accounts show their connection status without offering to connect again. Site Profile only edits public site content.

Matching email addresses never link accounts automatically. A Google identity can belong to only one local account. Each local account can have one Google identity. Connect the account before disabling email if the administrator originally registered with email. Changing Google accounts or disconnecting an identity is not included yet.

## First-time setup

The wizard offers the enabled registration providers. Google registration requires `auth.registrationEnabled`. The blog accepts only its configured `adminEmail`. Registration creates a verified local account, then asks the administrator to log in. The first successful login opens Site Profile. The public site opens after registration verifies the account, before that first login.

Google registration accepts Gmail addresses and Google Workspace addresses identified by Google's verified hosted-domain claim. For other addresses attached to Google accounts, register and verify through email first, then connect Google. Google's `email_verified` claim alone does not establish current ownership of those third-party addresses.

## Security and dependencies

The provider uses the authorization-code flow with PKCE, browser-bound state, and a nonce. Flow state expires after ten minutes. Atomic server-side session rotation permits only one callback to consume a flow. Starting another flow in the same browser replaces the previous flow. The callback checks the token's signature, issuer, audience, expiry, subject, nonce, and verified email before accessing accounts. Google access and refresh tokens are not persisted. Logging out ends the local session, not the Google session.

Protocol handling uses `golang.org/x/oauth2` and `github.com/coreos/go-oidc/v3`. The latter uses `github.com/go-jose/go-jose/v4` for signature verification. These maintained protocol libraries keep cryptographic parsing out of application code. Only the Google provider depends on their APIs, so replacing them does not change account storage or blog policy. Signing keys are fetched lazily and cached by the OIDC library. Network requests have a ten-second timeout.

Tests use a local token endpoint and signing-key endpoint. They cover invalid proofs, replay, concurrent callbacks, registration restrictions, explicit linking, and provider selection. A real Google login still requires your Google Cloud credentials and an authorized callback URL.

See Google's [OpenID Connect documentation](https://developers.google.com/identity/openid-connect/openid-connect) for provider setup and token semantics.
