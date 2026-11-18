CREATE TABLE blog_collections (
 id TEXT PRIMARY KEY, title TEXT NOT NULL,
 author_id TEXT NOT NULL,
 version INTEGER NOT NULL DEFAULT 1 CHECK (version>0)
);
CREATE TABLE blog_collection_content (
 collection_id TEXT NOT NULL REFERENCES blog_collections(id) ON DELETE CASCADE,
 post_id TEXT NOT NULL PRIMARY KEY REFERENCES blog_current_posts(id) ON DELETE CASCADE,
 position INTEGER NOT NULL CHECK (position>=0),
 UNIQUE (collection_id, position)
);
