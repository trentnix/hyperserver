# HTMX

The HTTP fixtures embed and serve the unmodified HTMX 4.0.2 browser build at `/js/vendor/htmx/4.0.2/htmx.min.js`. Browser tests use the fixture's static-file handler. This asset is not part of the blog binary.

- Source: https://unpkg.com/htmx.org@4.0.2/dist/htmx.min.js
- License: [Zero-Clause BSD](4.0.2/LICENSE), from https://unpkg.com/htmx.org@4.0.2/LICENSE
- SHA-384 (base64): `6NzMyCdh08UZ4b4M17SJJKQLdDijx4z2IqqKLKD0Q/PzCFQLpFuk3konY0WgqbTy`

To update, verify the upstream release and download its browser build and license into a versioned directory. Preserve the asset's bytes, including its lack of a trailing newline. Compute its SHA-384 digest with `openssl dgst -sha384 -binary htmx.min.js | openssl base64 -A`. Update the layout's URL and integrity attribute, the security and browser test expectations, and the documented version. Run the Go suite and the Firefox and Chromium browser checks before removing the previous version. Do not edit the minified build.
