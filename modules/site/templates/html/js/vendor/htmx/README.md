# HTMX

The site embeds and serves the unmodified HTMX 2.0.11 browser build at `/js/vendor/htmx/2.0.11/htmx.min.js`. Browser tests use that same asset through the site's static-file handler.

- Source: https://unpkg.com/htmx.org@2.0.11/dist/htmx.min.js
- License: [Zero-Clause BSD](2.0.11/LICENSE), from https://unpkg.com/htmx.org@2.0.11/LICENSE
- SHA-384 (base64): `2OatzQy1H+Zd/IIrjr1TcuDGqLXeHhbooAyJY1KdQMKnr4LZ22k31GBLdYKHmVjg`

To update, verify the upstream release and download its browser build and license into a versioned directory. Preserve the asset's bytes, including its lack of a trailing newline. Compute its SHA-384 digest with `openssl dgst -sha384 -binary htmx.min.js | openssl base64 -A`. Update the layout's URL and integrity attribute, the security and browser test expectations, and the documented version. Run the Go suite and the Firefox browser tests before removing the previous version. Do not edit the minified build.
