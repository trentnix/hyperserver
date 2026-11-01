# HTTP behavior

Use `application.New` and serve `app.Handler` for the standard middleware stack. The core and explicit middleware APIs remain available for applications that need another stack.

## Addresses, TLS, and proxies

`http.listenHost` and `http.port` select the listener. `http.publicOrigin` selects the address used in generated links. When the origin is empty, links use listener settings and the connection's TLS state. Wildcard listeners require an explicit origin. Request Host and forwarding headers never supply the link's address.

For direct HTTPS, set `http.tls.enabled`, `http.tls.certificate`, and `http.tls.key`. The listener supports TLS 1.2 or newer. Restart to reload certificates. Certificate issuance, renewal, and HTTP-to-HTTPS redirects are not automatic.

For one HTTPS-terminating proxy that connects over HTTP:

```yaml
http:
  listenHost: "127.0.0.1"
  port: 8080
  publicOrigin: "https://example.com"
  trustedProxies: ["127.0.0.1/32"]
```

Trust only the direct proxy peer and restrict backend access to it. Trusting loopback also trusts local processes. The proxy must overwrite `X-Forwarded-For` with one client IP and `X-Forwarded-Proto` with `http` or `https`, preserving the browser-facing Host and origin headers. Missing, duplicate, chained, or malformed forwarding values from trusted peers receive 400. Other peers' forwarding headers are ignored.

Verified proxy information controls client-IP limits and cookie security. `publicOrigin` alone does not enable TLS or secure cookies. For a custom HTTP stack, install [proxy verification](../pkg/requestinfo/request.go) before rate limits and session loading.

`application.New` installs the standard HTTP stack, including security headers and Go's `http.CrossOriginProtection`. Serve `app.Handler`. For explicit setup, use `middleware.Web(handler, cfg.HTTP, contentSecurityPolicy, log)` with session loading inside `handler`. Keep Host and origin headers intact through proxies. For HTTPS-only deployments, `http.hstsMaxAge` enables HSTS for direct or verified proxy HTTPS. The default `0` omits it but does not clear a browser's existing policy.


## Responses and body limits

Prepared templates reuse parsed HTML, not request data. Rebuild the application to change embedded templates or assets. See [rendering and forms](../pkg/components/readme.md).

Rendered pages and fragments default to `Cache-Control: no-store` and vary by `HX-Request` and `HX-History-Restore-Request`. Keep that policy for personalized content. Public responses can opt in through `page.Headers`. The framework does not cache rendered responses.

Redirect destinations must begin with a single `/`. Ordinary requests receive 303. HTMX requests receive 200 with `HX-Redirect`. History restoration uses full-page responses and ordinary redirects.

`routing.Routes` limits body reads to 64 KiB:

```go
routes := routing.NewRoutes(mux)
routes.Handle("POST /contact", contactHandler)
routes.HandleWithBodyLimit("POST /upload", uploadHandler, 8*routing.MiB)
routes.HandleWithoutBodyLimit("POST /stream", streamHandler)
```

Direct mux registration bypasses these limits. Handlers must check read errors before changing data and return 413 for `*http.MaxBytesError`.

[Form parsing](../pkg/components/form/request.go) separately limits decoded fields to 16 KiB and encoded queries to 64 KiB. Parser options can change field limits, not route body or query limits. JSON and multipart handlers own their parsing.

## Rate limits

Rate limits apply per client IP: the verified forwarded address when a trusted proxy is configured, otherwise the direct peer. Clients behind NAT share an allowance.

A handler policy replaces its module policy, which replaces the application default. Each registered method/path pattern has its own counter. Path parameters and query strings do not create new counters. HEAD shares its GET pattern's allowance.

```go
policy := ratelimit.Policy{Requests: 20, Window: time.Minute, MaxClients: 4096}
limited, err := routes.WithRateLimit(policy)
if err != nil {
    return err
}
limited.Handle("POST /submit", submitHandler)
limited.WithoutRateLimit().Handle("GET /stream", streamHandler)
```

Scopes copy policies. Changing a child scope does not change its parent or previously registered routes. Opting out removes the inherited route policy, not body limits or a separately attached limiter.

A shared limiter caps combined use across routes:

```go
budget, err := ratelimit.New("mail", ratelimit.Policy{
    Requests: 5, Window: time.Minute, MaxClients: 4096,
})
if err != nil {
    return err
}
routes.Handle("POST /contact", budget.Handler(contactHandler))
routes.Handle("POST /resend", budget.Handler(resendHandler))
```

Both routes consume the same five-request allowance per IP. Every attached limit must admit the request. Admission consumes an allowance even if a later limiter rejects it. Never wrap a request twice with the same limiter instance.

### Application settings

The [configuration template](../config-template.yaml) exposes independent controls:

```yaml
http:
  defaultRateLimit:
    enabled: true
    requests: 20
  sharedRateLimit:
    enabled: true
    requests: 120
```

Both default to one-minute windows and 4,096 tracked IPs per limiter. Both are disabled by default. The inherited default applies to each route. The shared budget runs before sessions and covers all requests, including assets, logout, and unmatched routes. A route override cannot bypass it.

Authentication mutations use 20 requests/minute per route. Reference-site contact and test-email routes use 10. Logout opts out of the inherited limit, but not the shared budget. Their capacities come from `auth.rateLimit.maxClients` and the optional `modules/site/config.yaml` setting `rateLimit.maxClients`. Both default to 4,096 and must be positive. HTTP default capacities do not change them. Module files have no environment overrides.

Startup warns when a route allowance exceeds an enabled shared allowance with the same window duration. Both limits remain enforced.

### Capacity and failures

When all `maxClients` slots are unexpired, new IPs receive 429, even on their first request. Existing clients retain their allowance. Expired entries are removed on later requests. Size capacity for distinct IPs within the window, not registered accounts.

Rejections are plain-text 429 responses with `Retry-After`, including HTMX requests. Request logs identify the route or shared budget and whether allowance or client capacity was exhausted. They omit submitted account values.

Each fixed window starts with the client's first admitted request. State resets on restart and is not shared across servers. Limits do not cap concurrent work or prevent distributed abuse.
