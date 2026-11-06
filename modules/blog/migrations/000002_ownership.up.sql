ALTER TABLE blog_current_posts ADD COLUMN author_id TEXT NOT NULL DEFAULT '';
ALTER TABLE blog_media ADD COLUMN uploaded_by TEXT NOT NULL DEFAULT '';
CREATE INDEX blog_posts_author ON blog_current_posts(author_id);

DROP VIEW blog_post_current;
CREATE VIEW blog_post_current AS
 SELECT c.rowid AS rowid, c.id, c.version_id, c.slug, p.title, p.summary, p.markdown,
 c.published, c.draft, p.preview_image, c.version, p.tags, c.author_id
 FROM blog_current_posts c JOIN blog_posts p ON p.id=c.version_id AND p.owner_id=c.id;
