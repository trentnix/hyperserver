# Google authentication

Google and email/password use the same local accounts and revocable sessions. The blog admits the configured first administrator and users invited by an Admin.

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

Enable email, Google, or both in configuration. A custom `Options.AuthProviders` catalog must include Google. See [authentication providers](application.md#authentication-providers).

`http.formActionOrigins` permits Google's form redirect through the application's CSP. Chromium enforces `form-action` on these redirects. The setting changes neither other CSP directives nor incoming cross-origin checks. No Google scripts or third-party browser cookies are required.

With one registered account, `/login` shows its available methods: email for a stored password, Google for a connected identity, or both. If an availability check fails, the control stays visible and login still validates credentials and admission.

Registration offers all enabled registration methods. Login offers all enabled methods when multiple users have access or invitations are pending. Removed users and canceled invitations do not affect those choices.

Forms work without JavaScript. Disabled providers cannot log in or accept callbacks. Google-only authentication needs no mail service. Global auth and session settings still apply.

Existing SQLite account databases must use the current schema before starting the application. See [account migrations](storage.md#account-schema-migrations) for the required version and upgrade commands. Fresh databases initialize automatically.

## Existing accounts

Sign in with the existing method. Open **Account** (`/auth/account`) and choose **Connect Google**. Connecting Google preserves the password and invalidates older authenticated sessions. Site Profile edits public content, not login methods.

If email authentication, mail delivery, and account storage with `user.PasswordSetupRepository` support are configured, a Google-only account can select **Add password** in Account. The site emails a single-use setup link to the account's stored address. Open the link while signed in to that account. Setting a password leaves Google connected and invalidates existing sessions. Sign in with email to try the password. Password recovery then works regardless of the original registration method.

Setup links cannot be used for password recovery, including unused links left after enrollment.

**Disconnect Google** requires an enabled password provider, a working password, and `user.ExternalIdentityUnlinker` storage support. The auth manager confirms the password. Storage atomically checks the account version, deletes the link, and revokes sessions. A concurrent password change rejects removal. Sign in with email afterward.

The site never allows removing Google when it is the only usable method. Disconnection removes the site's local association, not the Google account or its permissions at Google.

The blog places **Account** with the site navigation on mobile and ultrawide screens, and at the bottom of the page on other widths. If no enabled provider offers an account action, it shows **Sign out** instead.

Matching email addresses never link accounts automatically. A Google identity can belong to only one local account. Each local account can have one Google identity. Connect the account before disabling email if the administrator originally registered with email. To change Google accounts, add a password first, disconnect Google, then connect the new Google account.

## First-time setup

Enable `auth.registrationEnabled` for Google registration. Initial setup accepts only `adminEmail`. Later, invited addresses can register. Google creates a verified local account, signs in, and opens Site Profile. Email registration requires a separate login and any configured verification unless a valid invitation proves the address.

Google registration accepts Gmail addresses and Google Workspace addresses identified by Google's verified hosted-domain claim. For other addresses attached to Google accounts, register and verify through email first, then connect Google. Google's `email_verified` claim alone does not establish current ownership of those third-party addresses.

## Security and dependencies

The provider uses the authorization-code flow with PKCE, browser-bound state, and a nonce. Flow state expires after ten minutes. Atomic server-side session rotation permits only one callback to consume a flow. Starting another flow in the same browser replaces the previous flow.

The callback checks the token's signature, issuer, audience, expiry, subject, nonce, and verified email before accessing accounts. Google access and refresh tokens are not persisted. Logging out ends the local session, not the Google session.

Protocol handling uses `golang.org/x/oauth2` and `github.com/coreos/go-oidc/v3`, which uses `github.com/go-jose/go-jose/v4` for signatures. Their APIs stay inside the Google provider. The OIDC library lazily fetches and caches signing keys. Network requests have a ten-second timeout.

Tests use a local token endpoint and signing-key endpoint. They cover invalid proofs, replay, concurrent callbacks, registration restrictions, explicit linking, and provider selection. A real Google login still requires your Google Cloud credentials and an authorized callback URL.

See Google's [OpenID Connect documentation](https://developers.google.com/identity/openid-connect/openid-connect) for provider setup and token semantics.
