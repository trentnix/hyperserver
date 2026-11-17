# Related posts

Add Related posts from a post's component picker. Type a title to find matching posts. Results stay open after selection so you can add several posts from one search. Added posts keep their place in the results and show a checkmark. Click outside, press Escape, or clear the search to close the results.

Select up to 12 posts and arrange the links with the movement buttons. An optional heading replaces “Related posts.” Save with the post.

The component appears only on its source post. Selecting a target does not change that target or create reciprocal links. Related posts use the shared component ordering, removal, Undo, unsaved indicators, draft recovery, and revision history.

Readers see current titles and URLs for published targets. Draft, scheduled, and deleted targets remain referenced but disappear from the displayed list. Republishing a target restores its link in the same position. If no targets are public, the component disappears. Feeds and historical previews follow the same publication rules.

Authors can select public posts and private posts they can edit. Private titles are hidden from other authors. Existing references remain saved when a target becomes unavailable. New missing or inaccessible targets, duplicate selections, self-links, and empty components fail validation.

The component stores stable post IDs and an optional heading in the existing `post-components` Markdown block. It requires no database migration. One writer-only endpoint, `GET /post/related`, supplies editor choices. Saving uses the normal post handlers. Reader rendering resolves selected IDs in a bounded metadata query without loading target bodies. Pure Markdown rendering performs no lookup and omits unresolved links.
