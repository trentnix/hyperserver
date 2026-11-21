# Series

A series is a shared, ordered collection of posts. It is separate from Markdown components. Each post can belong to one series. Related posts are independent links stored with the post or page where you add them.

Open **Series** above the post’s publication and save controls. If the post has no series, choose **Create a series** to enter a name or **Find a series** to search existing series. Leaving the section untouched keeps the post unassigned. The post’s save action applies post and series changes together. A new post can create or join a series with its first save.

Series search matches series names and member post titles, tags, summaries, and text. Results show the series name, post count, and matching member titles. Only series you can manage with room for another post appear. No series list loads before you search. After creating or choosing a series, search for posts and arrange their order. Every member uses the same series data.

The series editor uses the same post picker as Related posts. It supports up to 100 posts, movement buttons, and multiple selections from one search. Removing a post from the list removes its membership when saved. Saving an empty series removes the series. Authors can manage their own series and posts. Admins can manage all series.

Series edits show an unsaved indicator and share the post’s browser recovery copy. Saving waits while series details load. If you leave the series unchanged, saving the post leaves shared series data alone, even when another editor has changed its name, order, or membership.

If you edit the series and another editor changes it first, saving reports a conflict and preserves your edits. The error message includes a recovery link. This action replaces your unsaved series edits with the current saved version, clears the conflict, and brings the Series section into view. It keeps your post edits and does not save the post.

Saving applies the post, its revision and media references, and any series changes in one transaction. A validation failure or stale version for edited data saves neither edit. Changing only the series preserves the post’s version and history. Restoring a post revision does not change its series.

Readers see the series name, the current post's place among published members, previous and next links, and a collapsible list of posts that starts expanded. Previous and next links stack on mobile. Titles and URLs come from current post metadata.

Draft and scheduled posts keep their membership and order but stay out of reader navigation until published. Permanent deletion removes membership. The blog does not currently support restoring deleted posts.

`blog_collections` stores the series name, owner, and version. `blog_collection_content` stores each member's post ID and position. A unique post ID enforces one series per post. Version checks, ownership checks, and membership changes share the post save transaction.

Migration `000002_collections` adds these tables without rewriting posts. Fresh storage includes both migrations. Existing version 1 databases require an [offline migration](readme.md#database-migrations). Downgrading removes series data and preserves posts, revisions, media, accounts, and sessions. Back up before migrating.

`GET /post/series/{id}` supplies series details and post choices for an existing post. `GET /post/series` serves the same purpose for a new post. Both routes require writing permission, and the existing-post route also checks ownership. The ordinary post submission saves series changes. Series navigation renders on ordinary post pages and in feeds. There is no separate series overview post or reader route.
