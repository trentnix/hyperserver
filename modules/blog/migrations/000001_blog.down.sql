-- Destructive: removes all blog content. Use only after a backup or for disposable data.
DROP VIEW blog_post_current;
DROP VIEW blog_page_current;
DROP VIEW blog_profile_current;
DROP TABLE blog_post_images;
DROP TABLE blog_page_images;
-- Remove parent relationships before deleting the page tree.
UPDATE blog_current_pages SET parent_id=NULL;
DROP TABLE blog_current_pages;
DROP TABLE blog_current_posts;
DROP TABLE blog_current_profile;
DROP TABLE blog_pages;
DROP TABLE blog_posts;
DROP TABLE blog_profile;
DROP TABLE blog_setup;
DROP TABLE blog_media;
