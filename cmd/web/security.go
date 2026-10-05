package main

// The reference application's policy allows its existing HTMX and font sources.
// Other applications must select their own policy for their assets and features.
const referenceContentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self' https://unpkg.com/htmx.org@2.0.1; " +
	"style-src 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; " +
	"img-src 'self'; connect-src 'self'; base-uri 'none'; object-src 'none'; " +
	"form-action 'self'; frame-ancestors 'none'"
