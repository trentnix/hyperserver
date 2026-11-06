DROP VIEW blog_post_current;
DROP INDEX blog_posts_author;
ALTER TABLE blog_current_posts DROP COLUMN author_id;
ALTER TABLE blog_media DROP COLUMN uploaded_by;
CREATE VIEW blog_post_current AS
 SELECT c.rowid AS rowid, c.id, c.version_id, c.slug, p.title, p.summary, p.markdown,
 c.published, c.draft, p.preview_image, c.version, p.tags
 FROM blog_current_posts c JOIN blog_posts p ON p.id=c.version_id AND p.owner_id=c.id;
