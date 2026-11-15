# Link previews

Use **Add component → Link preview** below a post's Markdown editor. Enter an HTTP or HTTPS URL and choose **Fetch preview**. Edit the title, description, and site name. Upload or choose an image, hide it with **Show image**, or remove it. Save the post to apply changes.

Fetching is optional. If a site blocks requests or has no useful metadata, enter the card details yourself. Refresh asks before replacing existing details. If you edit while a request runs, your edits take precedence over the response.

Readers see a linked card with the destination domain, title, description, and optional image. The destination domain comes from the saved URL. Site metadata cannot replace that URL. Cards use ordinary navigation and the same rounded container as other components.

## Fetching

`POST /post/link-preview` requires an Admin or Poster session and the application's same-origin protection. Each author can fetch ten previews per minute. Up to two requests run at once. The allowance stores at most 1,024 active author windows and resets after a minute or an application restart.

Each request has a five-second deadline covering the page and thumbnail. HTML is limited to 1 MiB after decompression. The fetcher follows at most three redirects. It accepts standard HTTP and HTTPS ports, checks every resolved address before connecting, and connects directly to a checked IP. Private, loopback, link-local, multicast, documentation, and other reserved destinations are blocked, including through redirects and image URLs. Proxies, cookies, and credentials are not forwarded. HTTPS redirects cannot downgrade to HTTP.

Metadata uses [Open Graph](https://ogp.me/) titles and descriptions, with HTML title and description fallbacks. Pages must use UTF-8. Other text encodings require manual card details. Relative image URLs resolve against the fetched page URL. Embed HTML, `og:url`, and document base URLs are ignored. Sites that require JavaScript or login may need manual card details. Metadata is treated as text, never executable markup. Network restrictions follow the [OWASP SSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html).

## Images and saved data

Imported images use the existing PNG, JPEG, and GIF pipeline: at most 8 MiB and 20 megapixels, with generated display and thumbnail copies. The importer owns the upload. Existing ownership and permitted-reuse rules still apply. If an image cannot be imported, the title and description remain usable.

The existing version 2 `post-components` block stores a `link-preview` record with a `link` object. Fields are `url`, `title`, optional `description`, optional `siteName`, optional local `imageID`, and optional `hideImage`. One preview is allowed per post. Titles are limited to 240 characters, descriptions to 1,000, site names to 200, and URLs to 2,048 bytes. No database migration is required.

Ordering, removal, Undo, browser recovery, and revision history preserve the card. Hidden images retain their saved references so they remain available to show again. Removing the image clears that reference from the current revision. Historical references follow the usual cleanup rules. Draft and scheduled thumbnails stay private until publication. Readers, feeds, search, and revision previews never fetch remote metadata. Search includes the saved title, description, and site name. Feeds show the saved card and any visible local image.
