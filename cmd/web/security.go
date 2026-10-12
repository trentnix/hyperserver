package main

// The reference application's policy allows local assets and Google Fonts.
// Other applications must select their own policy for their assets and features.
const referenceContentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; " +
	"img-src 'self'; connect-src 'self'; base-uri 'none'; object-src 'none'; " +
	"form-action 'self'; frame-ancestors 'none'"
