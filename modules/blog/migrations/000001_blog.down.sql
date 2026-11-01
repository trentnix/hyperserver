-- Destructive: use only for disposable databases or after a verified backup.
DROP TABLE blog_profile_revisions;
DROP TABLE blog_page_revisions;
DROP TABLE blog_post_revisions;
DROP TABLE blog_page_images;
DROP TABLE blog_post_images;
-- Remove parent relationships before deleting a self-referencing page tree.
UPDATE blog_pages SET parent_id = NULL;
DROP TABLE blog_pages;
DROP TABLE blog_posts;
DROP TABLE blog_profile;
DROP TABLE blog_setup;
DROP TABLE blog_media;
