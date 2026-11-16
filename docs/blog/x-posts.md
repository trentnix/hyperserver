# X posts

Use **Add component → X post** below a post's Markdown editor. Paste a public X post URL or an old `twitter.com` share link. The editor shows any fallback image below the URL, followed by the optional description and image controls. The description appears above the embedded post. Save the post to apply changes. One X post component is allowed per post.

Upload or choose an optional fallback image, such as a screenshot. It appears while the embed loads and remains visible if loading fails. Once the embed renders, the screenshot hides. The screenshot links to the original post. It stays saved for future visits and remains visible without JavaScript, in feeds, and in historical previews. Images use the site's PNG, JPEG, and GIF limits: 8 MiB and 20 megapixels.

The post and its media load automatically when the component comes into view. Loading connects to X. If loading fails, readers can choose **Retry X post**. **Open on X** stays visible until the embed renders successfully. The link also works without JavaScript and when embedding is blocked or unavailable. X controls the embedded content and its availability. The blog does not automatically copy post text or media from X.

## Storage and editing

The existing version 2 `post-components` block stores an `x-post` record with an `xPost` object. Fields are `url`, optional `description`, and optional `imageID`. URLs allow 2,048 bytes. Descriptions allow 1,000 characters. Image permissions, cleanup protection, and revision history use the shared media paths. No database migration or X API credentials are required.

The component uses shared ordering, removal, Undo, unsaved indicators, browser recovery, and revision history. An added empty component must be completed or removed before Save. An image alone does not complete the component. A component-only post is allowed. Search includes the description. Feeds and historical previews keep the description, any fallback image, and a canonical viewing link without interactive controls. Page editing does not expose the component yet.

## Embedding

The application validates supported X and Twitter hosts and extracts a numeric post ID. Share parameters and photo/video suffixes do not reach the embed URL. The saved URL remains editable. Saving, reader rendering, search, and feeds make no server-side requests to X.

When the component comes into view, the browser loads `https://platform.twitter.com/embed/Tweet.html`, the provider-hosted iframe used by X's widget. The iframe requests `dnt=true`, hides the surrounding conversation, and disables autoplay through its permissions policy. X's scripts run on the provider origin. The blog's script policy remains limited to its own assets. The iframe sandbox permits its scripts, origin, and viewing links while blocking navigation of the blog page.

Resize and unavailable-post messages must come from the iframe's window and exact provider origin. The reader uses X's own card without an outer gray surface. Heights round up with a pixel of clearance. Scrolling stays off unless content reaches the 4,096-pixel height limit, avoiding X's forced scrollbar when the post fits. A resize adjusts the frame but does not confirm success. Loading completes only when X reports that the post rendered. Failed or stalled loading offers Retry while retaining Open on X. Returning through the browser cache reloads the component when it becomes visible. Browsers without viewport observation show a Load X post button.

The iframe URL and resize messages are provider interfaces rather than a versioned API contract. Changes at X can require an integration update. Keep the static fallback and browser tests when changing the integration.

X documents embedded photos and videos in its [embedding guide](https://help.x.com/en/using-x/how-to-embed-a-post). Its [website integration privacy notice](https://help.x.com/en/x-for-websites-ads-info-and-privacy) describes information received when readers load an embed.
