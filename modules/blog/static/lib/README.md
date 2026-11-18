# Bundled browser libraries

- HTMX 4.0.2: https://cdn.jsdelivr.net/npm/htmx.org@4.0.2/dist/htmx.min.js. License in `htmx/LICENSE`.
- EasyMDE 2.21.0: `dist/easymde.min.js` and `dist/easymde.min.css` from the published npm package. License in `easymde/LICENSE`.
- Lucide link, image-upload, and image-library icons: SVGs in `lucide/`, with their license. The source revision is recorded in [blog.css](../css/blog.css).

The editor does not download Font Awesome or spell-checking dictionaries. Blog CSS supplies toolbar labels and icons. Fonts are [bundled locally](../fonts/README.md).

The [scheduled dependency check](../../../../.github/workflows/browser-dependencies.yml) compares HTMX and EasyMDE versions with stable npm releases. Review updates and run [browser checks](../../../../docs/testing.md#browser-tests) before replacing vendored assets.
