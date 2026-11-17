# Social media

Use **Add component → Social media**. Paste a public X, Bluesky, Threads, or Facebook post URL. The component detects the platform. No platform selector or extra inputs are required. An optional description appears above the embed. Upload or choose an optional screenshot as a fallback. Save the post to apply changes.

One Social media component can be added to a post. If Undo restores an older component over its replacement, the replacement stays available through its own Undo. Changing its URL can switch platforms while keeping the description and screenshot. No API credentials, provider account, or database migration is required. Added empty components must be completed or removed before Save. Descriptions allow 1,000 characters. Screenshots use the existing image limits: PNG, JPEG, or GIF, up to 8 MiB and 20 megapixels.

## Supported links

- X: `https://x.com/{username}/status/{post-ID}`. Legacy `twitter.com` links and photo/video share links also work. See [X embedding behavior](x-posts.md).
- Bluesky: `https://bsky.app/profile/{handle-or-DID}/post/{record-key}`. Direct DID links support `did:plc`. Ordinary handle links also work for other accounts resolved by Bluesky.
- Threads: `https://www.threads.com/@{username}/post/{code}` or `https://www.threads.com/t/{code}`. Legacy `threads.net` links also work.
- Facebook: `https://www.facebook.com/{username-or-ID}/posts/{post-ID}` or `https://www.facebook.com/permalink.php?story_fbid={post-ID}&id={profile-ID}`. Mobile links and equivalent `story.php` links also work. For shortened sharing links, open the post and copy its full address first. Profiles, feeds, groups, reels, and standalone video links are not supported.

The application validates supported hosts and post paths. Rendering drops tracking parameters and uses an HTTPS viewing link. The original input remains editable. A valid URL does not guarantee that a provider will permit embedding. Private, deleted, restricted, or unavailable posts can fail to load.

## Reader behavior

Posts load when readers reach them. Loading contacts the provider. Each provider's own card appears left aligned without an outer gray container. The screenshot and viewing link remain available while loading. A completed embed replaces them. Loading failures retain the screenshot and viewing link and offer **Retry**. Feeds, historical previews, and browsers without JavaScript show the description, screenshot, and viewing link.

For Bluesky, Threads, and Facebook, the browser checks the provider's official oEmbed endpoint before creating its iframe. Unavailable posts and request limits leave the local fallback. An availability change between the check and rendering can still produce the provider's own unavailable-post notice. These providers report frame heights rather than a separate success event. The blog cannot inspect their cross-origin contents.

The endpoints are `https://embed.bsky.app/oembed`, `https://graph.threads.com/oembed`, and `https://graph.facebook.com/v27.0/oembed_post`. Requests omit credentials and referrers, reject redirects, limit responses to 64 KiB, and share a 15-second loading deadline with the iframe. Provider HTML is never inserted into the page. Bluesky supplies a validated post identity. Meta responses only confirm availability. The blog constructs each iframe destination itself.

Provider scripts run inside provider-hosted sandboxed iframes. The page keeps `script-src 'self'`. Its connection and frame policies allow only the required provider origins. Height messages must come from the exact origin and iframe window. Bluesky and Facebook messages must also match the frame's callback ID. Heights are bounded at 4,096 pixels, with scrolling enabled for taller posts. Cards scale to fit narrow content widths without clipping. Navigation cleanup aborts pending requests and resets cached frames.

## Storage

The version 2 `post-components` block stores a `social-post` record with a `socialPost` payload. The payload contains `url`, optional `description`, and optional `imageID`. The URL determines the provider. Authored JSON cannot select a different provider.

Existing `x-post`, `bluesky-post`, `threads-post`, and `facebook-post` records keep their original envelope keys. They open in the same Social media editor and can also change platforms. Legacy records remain supported for rendering, feeds, history, and recovered drafts. They are not separate choices in Add component. Saving a new Social media component beside a legacy record fails validation and preserves both inputs. Older envelopes containing multiple provider-specific records remain readable and editable.

Shared code handles ordering, removal, Undo, unsaved indicators, browser recovery, validation errors, image authorization, history, and cleanup references. Search uses the authored description. Saving, reader rendering on the server, feeds, and reference extraction make no provider requests. The blog does not store fetched post text or media. Page editing does not expose these components yet.

See [Bluesky's embed documentation](https://bsky.network/docs/oembed/), [Meta's Threads integration](https://github.com/facebook/meta-embeds-for-wordpress), and [Facebook's oEmbed documentation](https://developers.facebook.com/docs/graph-api/reference/oembed-post/). Provider availability and iframe protocols can change independently of the blog.
