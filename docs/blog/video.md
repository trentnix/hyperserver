# Video

Use **Add component → Video** below a post's Markdown editor. Paste a YouTube or Vimeo URL, enter a title, and add an optional caption. Save with the post. One video component is allowed per post.

Readers see the provider's preview image and playback controls, plus an **Open on YouTube** or **Open on Vimeo** link. The player loads as it approaches the viewport. Autoplay is disabled, so playback waits for Play. The embedded player works without the blog's JavaScript. Feed entries include the title, viewing link, and caption.

## Supported URLs

YouTube accepts ordinary watch links, `youtu.be` links, Shorts, live video links, and embed URLs. Vimeo accepts ordinary video links, unlisted links with a privacy hash, and `player.vimeo.com/video/` URLs. Include the full unlisted Vimeo link so its privacy hash is retained.

YouTube's `t` and `start` query parameters and Vimeo's `#t=` fragment can set a start time. Use whole seconds or a duration such as `1m30s`. Start times must be between zero and seven days. Other playback and tracking parameters are ignored.

Provider settings still control whether a video can be embedded. Deleted, private, restricted, or domain-limited videos may require opening the provider's site. The component does not check availability when saving.

## Storage and rendering

The existing version 2 `post-components` block stores a `video` record containing a `video` object with `url`, `title`, and optional `caption`. Titles are limited to 240 characters, captions to 1,000, and URLs to 2,048 bytes. No database migration is required.

Ordering, removal, Undo, browser recovery, and revision history use the existing component flow. Search includes titles and captions. Video files stay with their provider. Saving and rendering never fetch remote video data or metadata.

The application accepts only supported provider hosts and validated video IDs. It constructs player URLs itself and never accepts embed HTML. YouTube uses `www.youtube-nocookie.com`. Vimeo uses `player.vimeo.com` with `dnt=1`. Loading the preview contacts the provider before Play. The iframe shares only the embedding origin through its referrer policy. Other page requests retain the application's existing policy.

The blog's Content Security Policy permits the two player origins in `frame-src`. External scripts remain blocked on the blog page. Autoplay stays disabled when readers return through HTMX history.

Provider references: [YouTube embeds](https://developers.google.com/youtube/player_parameters), [YouTube referrer requirements](https://developers.google.com/youtube/terms/required-minimum-functionality), [Vimeo parameters](https://help.vimeo.com/hc/en-us/articles/12426260232977-About-Player-Parameters), and [Vimeo unlisted videos](https://help.vimeo.com/hc/en-us/articles/12426470858001-Embedded-player-displays-This-video-does-not-exist-message).
