-- Current baseline. Earlier development layouts are unsupported.
CREATE TABLE blog_current_posts (
 id TEXT PRIMARY KEY, version_id INTEGER NOT NULL,
 slug TEXT NOT NULL UNIQUE, published INTEGER NOT NULL,
 draft INTEGER NOT NULL DEFAULT 0 CHECK (draft IN (0, 1)),
 version INTEGER NOT NULL DEFAULT 1,
 author_id TEXT NOT NULL DEFAULT '',
 FOREIGN KEY (id, version_id) REFERENCES blog_posts(owner_id, id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX blog_posts_author ON blog_current_posts(author_id);
CREATE TABLE blog_posts (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 owner_id TEXT NOT NULL REFERENCES blog_current_posts(id) ON DELETE CASCADE,
 title TEXT NOT NULL, summary TEXT NOT NULL, markdown TEXT NOT NULL,
 preview_image TEXT NOT NULL, tags TEXT NOT NULL DEFAULT '', recorded INTEGER NOT NULL, content_path TEXT NOT NULL,
 UNIQUE (owner_id, id)
);
CREATE TABLE blog_current_pages (
 id TEXT PRIMARY KEY, version_id INTEGER NOT NULL,
 slug TEXT NOT NULL UNIQUE, published INTEGER NOT NULL CHECK (published IN (0, 1)),
 parent_id TEXT REFERENCES blog_current_pages(id) ON DELETE RESTRICT,
 children_display TEXT NOT NULL CHECK (children_display IN ('hidden', 'list', 'blocks')),
 display_order INTEGER CHECK (display_order BETWEEN 0 AND 1000000), updated INTEGER NOT NULL,
 ever_published INTEGER NOT NULL DEFAULT 0 CHECK (ever_published IN (0, 1)),
 version INTEGER NOT NULL DEFAULT 1,
 FOREIGN KEY (id, version_id) REFERENCES blog_pages(owner_id, id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX blog_pages_parent ON blog_current_pages(parent_id);
CREATE TRIGGER blog_pages_no_cycle BEFORE UPDATE OF parent_id ON blog_current_pages
WHEN NEW.parent_id IS NOT NULL BEGIN
 SELECT RAISE(ABORT, 'page parent cycle') WHERE NEW.id IN (
  WITH RECURSIVE ancestors(id, parent_id) AS (
   SELECT id, parent_id FROM blog_current_pages WHERE id=NEW.parent_id
   UNION ALL SELECT p.id, p.parent_id FROM blog_current_pages p JOIN ancestors a ON p.id=a.parent_id
  ) SELECT id FROM ancestors
 );
END;
CREATE TABLE blog_pages (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 owner_id TEXT NOT NULL REFERENCES blog_current_pages(id) ON DELETE CASCADE,
 title TEXT NOT NULL, summary TEXT NOT NULL, markdown TEXT NOT NULL,
 preview_image TEXT NOT NULL, tags TEXT NOT NULL DEFAULT '', recorded INTEGER NOT NULL, content_path TEXT NOT NULL,
 UNIQUE (owner_id, id)
);
CREATE TABLE blog_current_profile (
 id INTEGER PRIMARY KEY CHECK (id=1), version_id INTEGER NOT NULL,
 version INTEGER NOT NULL DEFAULT 1,
 FOREIGN KEY (id, version_id) REFERENCES blog_profile(owner_id, id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE blog_profile (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 owner_id INTEGER NOT NULL REFERENCES blog_current_profile(id) ON DELETE CASCADE,
 name TEXT NOT NULL, bio TEXT NOT NULL, image_url TEXT NOT NULL,
 favicon_url TEXT NOT NULL, background_url TEXT NOT NULL, recorded INTEGER NOT NULL,
 UNIQUE (owner_id, id)
);
CREATE TABLE blog_post_images (
 version_id INTEGER NOT NULL REFERENCES blog_posts(id) ON DELETE CASCADE,
 url TEXT NOT NULL, PRIMARY KEY(version_id, url)
);
CREATE INDEX blog_post_images_url ON blog_post_images(url, version_id);
CREATE TABLE blog_page_images (
 version_id INTEGER NOT NULL REFERENCES blog_pages(id) ON DELETE CASCADE,
 url TEXT NOT NULL, PRIMARY KEY(version_id, url)
);
CREATE INDEX blog_page_images_url ON blog_page_images(url, version_id);
CREATE TABLE blog_setup (
 email TEXT PRIMARY KEY, complete INTEGER NOT NULL CHECK (complete IN (0, 1))
);
CREATE TABLE blog_media (
 id TEXT PRIMARY KEY, filename TEXT NOT NULL, label TEXT NOT NULL,
 version INTEGER NOT NULL CHECK (version>0), tags TEXT NOT NULL DEFAULT '',
 uploaded_by TEXT NOT NULL DEFAULT ''
);

-- Read-only joins keep current-content queries independent of history.
CREATE VIEW blog_post_current AS
 SELECT c.rowid AS rowid, c.id, c.version_id, c.slug, p.title, p.summary, p.markdown,
 c.published, c.draft, p.preview_image, c.version, p.tags, c.author_id
 FROM blog_current_posts c JOIN blog_posts p ON p.id=c.version_id AND p.owner_id=c.id;
CREATE VIEW blog_page_current AS
 SELECT c.id, c.version_id, c.slug, p.title, p.markdown, c.published, c.parent_id,
 p.summary, p.preview_image, c.children_display, c.display_order, c.updated,
 c.ever_published, c.version, p.tags
 FROM blog_current_pages c JOIN blog_pages p ON p.id=c.version_id AND p.owner_id=c.id;
CREATE VIEW blog_profile_current AS
 SELECT c.id, c.version_id, p.name, p.bio, p.image_url, p.favicon_url, p.background_url, c.version
 FROM blog_current_profile c JOIN blog_profile p ON p.id=c.version_id AND p.owner_id=c.id;
