-- Unversioned schema produced by 1502fba, before versioned migrations.
-- Keep this fixture unchanged when adding future migrations.
CREATE TABLE IF NOT EXISTS blog_posts (
 id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL,
 summary TEXT NOT NULL, markdown TEXT NOT NULL, published INTEGER NOT NULL,
 draft INTEGER NOT NULL DEFAULT 0 CHECK (draft IN (0, 1)),
 preview_image TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS blog_pages (
 id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL,
 markdown TEXT NOT NULL, published INTEGER NOT NULL CHECK (published IN (0, 1)),
 parent_id TEXT REFERENCES blog_pages(id) ON DELETE RESTRICT,
 summary TEXT NOT NULL, preview_image TEXT NOT NULL,
 children_display TEXT NOT NULL CHECK (children_display IN ('hidden', 'list', 'blocks')),
 display_order INTEGER CHECK (display_order BETWEEN 0 AND 1000000), updated INTEGER NOT NULL,
 ever_published INTEGER NOT NULL DEFAULT 0 CHECK (ever_published IN (0, 1))
);
CREATE INDEX IF NOT EXISTS blog_pages_parent ON blog_pages(parent_id);
CREATE TRIGGER IF NOT EXISTS blog_pages_no_cycle BEFORE UPDATE OF parent_id ON blog_pages
WHEN NEW.parent_id IS NOT NULL BEGIN
 SELECT RAISE(ABORT, 'page parent cycle') WHERE NEW.id IN (
  WITH RECURSIVE ancestors(id, parent_id) AS (
   SELECT id, parent_id FROM blog_pages WHERE id=NEW.parent_id
   UNION ALL SELECT p.id, p.parent_id FROM blog_pages p JOIN ancestors a ON p.id=a.parent_id
  ) SELECT id FROM ancestors
 );
END;
CREATE TABLE IF NOT EXISTS blog_profile (
 id INTEGER PRIMARY KEY CHECK (id=1), name TEXT NOT NULL, bio TEXT NOT NULL,
 image_url TEXT NOT NULL, favicon_url TEXT NOT NULL, background_url TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS blog_setup (
 email TEXT PRIMARY KEY, complete INTEGER NOT NULL CHECK (complete IN (0, 1))
);
CREATE TABLE IF NOT EXISTS blog_media (
 id TEXT PRIMARY KEY, filename TEXT NOT NULL, label TEXT NOT NULL,
 version INTEGER NOT NULL CHECK (version>0)
);
CREATE TABLE blog_post_images (
 owner_id TEXT NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
 url TEXT NOT NULL, PRIMARY KEY(owner_id, url)
);
CREATE INDEX blog_post_images_url ON blog_post_images(url);
CREATE TABLE blog_post_revisions (
 id INTEGER PRIMARY KEY AUTOINCREMENT, owner_id TEXT NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
 title TEXT NOT NULL, summary TEXT NOT NULL, markdown TEXT NOT NULL, preview_image TEXT NOT NULL,
 recorded INTEGER NOT NULL, tags TEXT NOT NULL DEFAULT ''
);
CREATE INDEX blog_post_revisions_owner ON blog_post_revisions(owner_id, id);

CREATE TABLE blog_page_images (
 owner_id TEXT NOT NULL REFERENCES blog_pages(id) ON DELETE CASCADE,
 url TEXT NOT NULL, PRIMARY KEY(owner_id, url)
);
CREATE INDEX blog_page_images_url ON blog_page_images(url);
CREATE TABLE blog_page_revisions (
 id INTEGER PRIMARY KEY AUTOINCREMENT, owner_id TEXT NOT NULL REFERENCES blog_pages(id) ON DELETE CASCADE,
 title TEXT NOT NULL, summary TEXT NOT NULL, markdown TEXT NOT NULL, preview_image TEXT NOT NULL,
 recorded INTEGER NOT NULL, tags TEXT NOT NULL DEFAULT ''
);
CREATE INDEX blog_page_revisions_owner ON blog_page_revisions(owner_id, id);
CREATE TABLE blog_profile_revisions (
 id INTEGER PRIMARY KEY AUTOINCREMENT, owner_id INTEGER NOT NULL REFERENCES blog_profile(id) ON DELETE CASCADE,
 name TEXT NOT NULL, bio TEXT NOT NULL, image_url TEXT NOT NULL, favicon_url TEXT NOT NULL,
 background_url TEXT NOT NULL, recorded INTEGER NOT NULL
);

ALTER TABLE blog_posts ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE blog_pages ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE blog_profile ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE blog_posts ADD COLUMN tags TEXT NOT NULL DEFAULT '';
ALTER TABLE blog_pages ADD COLUMN tags TEXT NOT NULL DEFAULT '';
ALTER TABLE blog_media ADD COLUMN tags TEXT NOT NULL DEFAULT '';
