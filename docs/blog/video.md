# Video

Use **Add component → Video** below a post's Markdown editor. Paste a YouTube or Vimeo URL. The title and caption are optional. Save with the post. One video component is allowed per post.

Choose **Fetch details** to copy the video's title and available description into the title and caption fields. You can edit or clear either field. **Refresh details** asks before replacing existing text. Missing source fields and failed requests leave your text intact. Source descriptions may be shortened by the provider.

Readers see the provider's preview image and playback controls. The player loads as it approaches the viewport. Autoplay is disabled, so playback waits for Play. The embedded player works without the blog's JavaScript. Feed entries replace the player with a viewing link and include any title or caption.

## Supported URLs

YouTube accepts ordinary watch links, `youtu.be` links, Shorts, live video links, and embed URLs. Vimeo accepts ordinary video links, unlisted links with a privacy hash, and `player.vimeo.com/video/` URLs. Include the full unlisted Vimeo link so its privacy hash is retained.

YouTube's `t` and `start` query parameters and Vimeo's `#t=` fragment can set a start time. Use whole seconds or a duration such as `1m30s`. Start times must be between zero and seven days. Other playback and tracking parameters are ignored.

Provider settings still control whether a video can be embedded. Deleted, private, restricted, or domain-limited videos may require opening the provider's site. The component does not check availability when saving.

## Storage and rendering

The existing version 2 `post-components` block stores a `video` record containing a `video` object with `url` and optional `title` and `caption`. Titles are limited to 240 characters, captions to 1,000, and URLs to 2,048 bytes. If the title is blank, the player uses an accessibility label such as “YouTube video” and omits the title heading. No database migration is required.

Ordering, removal, Undo, browser recovery, and revision history use the existing component flow. Search includes titles and captions. Video files stay with their provider. Metadata is fetched only when the author requests it and saved with the post. Saving and rendering never fetch remote video data or metadata.

Video details share the link preview's authenticated endpoint protections: public destinations, checked DNS addresses, three redirects, a five-second timeout, two concurrent requests, and ten requests per author per minute. The fetcher reads at most 1 MiB of decoded HTML and stops at the end of the metadata section. It imports text only and ignores embed markup and thumbnails. Both title and caption remain optional if metadata is unavailable.

The application accepts only supported provider hosts and validated video IDs. It constructs player URLs itself and never accepts embed HTML. YouTube uses `www.youtube-nocookie.com`. Vimeo uses `player.vimeo.com` with `dnt=1`. Loading the preview contacts the provider before Play. The iframe shares only the embedding origin through its referrer policy. Other page requests retain the application's existing policy.

The blog's Content Security Policy permits the two player origins in `frame-src`. External scripts remain blocked on the blog page. Autoplay stays disabled when readers return through HTMX history.

Provider references: [YouTube embeds](https://developers.google.com/youtube/player_parameters), [YouTube referrer requirements](https://developers.google.com/youtube/terms/required-minimum-functionality), [Vimeo parameters](https://help.vimeo.com/hc/en-us/articles/12426260232977-About-Player-Parameters), and [Vimeo unlisted videos](https://help.vimeo.com/hc/en-us/articles/12426470858001-Embedded-player-displays-This-video-does-not-exist-message).
