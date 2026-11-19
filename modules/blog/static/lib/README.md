# Bundled browser libraries

- HTMX 4.0.2: https://cdn.jsdelivr.net/npm/htmx.org@4.0.2/dist/htmx.min.js. License in `htmx/LICENSE`.
- EasyMDE 2.21.0: `dist/easymde.min.js` and `dist/easymde.min.css` from the published npm package. License in `easymde/LICENSE`.
- PDF.js 6.5.438: `pdfjs-dist` from Mozilla's official npm distribution. Minified browser/worker modules, character maps, standard fonts, and JavaScript image decoders are in `pdfjs/`, with their licenses. Presentation mode loads these local assets only when needed. WebAssembly and PDF scripting are not enabled.
- Lucide link, image-upload, and image-library icons: SVGs in `lucide/`, with their license. The source revision is recorded in [blog.css](../css/blog.css).

The editor does not download Font Awesome or spell-checking dictionaries. Blog CSS supplies toolbar labels and icons. Fonts are [bundled locally](../fonts/README.md).

The [scheduled dependency check](../../../../.github/workflows/browser-dependencies.yml) compares HTMX, EasyMDE, and PDF.js versions with stable npm releases. Review updates and run [browser checks](../../../../docs/testing.md#browser-tests) before replacing vendored assets.
