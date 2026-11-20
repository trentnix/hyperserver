# Social media

Choose **Add component → Social media** and paste a public post URL. The URL determines the platform. An optional description appears above the post. An optional screenshot appears while loading or if embedding fails. No API credentials are required.

The editor previews the post when the URL loses focus and previews saved components when opened. Changing the URL clears the old preview. Description edits do not reload the provider. Previewing does not save or change the component. Descriptions allow 1,000 characters, URLs 2,048 bytes, and screenshots follow the [image limits](media.md#upload-and-reuse). See [shared component behavior](components.md).

## Supported links

| Platform | Accepted links and limits |
| --- | --- |
| X | `https://x.com/{username}/status/{post-ID}`. Legacy `twitter.com` and photo/video share links work. |
| Bluesky | `https://bsky.app/profile/{handle-or-DID}/post/{record-key}`. Direct DID links support `did:plc`. Other accounts can use handle links. |
| Threads | `https://www.threads.com/@{username}/post/{code}` or `/t/{code}`. Legacy `threads.net` works. |
| Facebook | Public `/posts/` links, or `permalink.php`/`story.php` with `story_fbid` and `id`. Mobile links work. Profiles, feeds, groups, reels, and standalone video links do not. |
| Reddit | `https://www.reddit.com/r/{community}/comments/{post-ID}/{title}/`, including full comment permalinks and Unicode title slugs. |
| Mastodon | HTTPS public DNS host with `/@{username}/{post-ID}` or `/users/{username}/statuses/{post-ID}`. Saving validates the path, not the instance's software or availability. |
| Instagram | `https://www.instagram.com/p/{code}/`, `/reel/{code}/`, or `/tv/{code}/`. Stories and profiles do not work. |
| LinkedIn | `/feed/update/urn:li:activity:{post-ID}`, equivalent `share`/`ugcPost` links, or `/posts/{slug}-activity-{post-ID}-{suffix}`. LinkedIn decides which posts permit embedding. |
| TikTok | `https://www.tiktok.com/@{username}/video/{post-ID}` or `/photo/{post-ID}`. |

For shortened sharing links, open the link and copy its full destination. Rendering validates supported hosts and paths, removes tracking parameters, and constructs HTTPS embed URLs. The original input remains editable. A valid URL cannot guarantee access to private, deleted, restricted, or unavailable posts.

## Loading and fallbacks

Posts load near the viewport and contact their providers. Successful embeds replace the screenshot and viewing link. Failed or stalled loads retain the fallback and offer Retry. Without JavaScript, in feeds, and in historical previews, readers get the description, screenshot, and viewing link. Saving and server-side rendering make no provider requests. The blog does not copy post text or media.

X confirms successful rendering with a provider message. Resize alone does not confirm success. Bluesky, Threads, Facebook, and Instagram first check official oEmbed endpoints. Responses confirm availability or identity, never supply executable markup. A later availability change can still show a provider error. Requests omit credentials and referrers, reject redirects, allow at most 64 KiB, and share a 15-second deadline with iframe loading.

LinkedIn uses a scrollable 600-pixel frame because its embed reports neither height nor rendering success. Frame load hides its screenshot, but Open on LinkedIn and Retry remain available. The blog cannot distinguish a post from a login prompt or error inside that frame. TikTok keeps recovery controls until its player reports readiness, which can happen after Play. Player errors restore the fallback. Autoplay is disabled.

Provider scripts stay inside sandboxed iframes. The blog keeps `script-src 'self'` and permits only required connection and frame origins. Resize messages must match the iframe window, exact origin, and any provider callback ID. Heights are capped at 4,096 pixels, with scrolling for taller posts. Mastodon uses a same-origin wrapper restricted to the selected instance. Navigation cancels pending loads and resets cached frames.

Provider availability and embed protocols can change independently. Keep the fallback and [browser coverage](../testing.md) when changing integrations. Implementations live in [social_post_component.go](../../modules/blog/social_post_component.go) and [social-post.js](../../modules/blog/static/js/social-post.js). Saved and legacy formats are documented in [component development](component-development.md#saved-formats).
