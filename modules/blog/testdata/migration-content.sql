-- Populate every table, including historical URL bases and cross-module references.
-- Deferred pointers allow identities and versions to be created in one transaction.
BEGIN;
INSERT INTO blog_current_posts(id,version_id,slug,published,draft,version) VALUES
 ('live',4,'live',1700000000,0,4),
 ('draft',5,'draft',0,1,7),
 ('scheduled',6,'scheduled',4102444800,0,2);
INSERT INTO blog_posts(id,owner_id,title,summary,markdown,preview_image,recorded,tags,content_path) VALUES
 (1,'live','Earlier post','Old summary','![Old](../../images/old.png)','',1600000000000000000,'older','/post/s/old-slug'),
 (2,'draft','Earlier draft','','Earlier body','private.png',1600000000000000000,'older','/post/s/draft'),
 (3,'scheduled','Earlier scheduled','','Earlier body','',1600000000000000000,'older','/post/s/scheduled'),
 (4,'live','Published post','Public summary','![Public](/images/public.png)','public.png',1700000000000000000,'public, example','/post/s/live'),
 (5,'draft','Draft post','Private summary','![Private](/images/private.png)','private.png',1700000000000000000,'private','/post/s/draft'),
 (6,'scheduled','Scheduled post','Future summary','Future content','',1700000000000000000,'future','/post/s/scheduled');

INSERT INTO blog_current_pages(id,version_id,slug,published,parent_id,children_display,display_order,updated,ever_published,version) VALUES
 ('parent',4,'projects',1,NULL,'blocks',10,1700000000000000000,1,3),
 ('child',5,'example',1,'parent','hidden',20,1700000000000000001,1,5),
 ('private-page',6,'private-page',0,'parent','list',NULL,1700000000000000002,1,8);
INSERT INTO blog_pages(id,owner_id,title,summary,markdown,preview_image,recorded,tags,content_path) VALUES
 (1,'parent','Earlier Projects','','Earlier body','',1600000000000000000,'older','/projects'),
 (2,'child','Earlier Example','','![Old](images/old.png)','',1600000000000000000,'older','/about'),
 (3,'private-page','Earlier Private','','Earlier body','private.png',1600000000000000000,'older','/page/private-page'),
 (4,'parent','Projects','Project previews','Parent content','',1700000000000000000,'projects','/projects'),
 (5,'child','Example','A project','![Preview](/images/public.png)','public.png',1700000000000000000,'example','/page/example'),
 (6,'private-page','Private page','Not public','Private content','private.png',1700000000000000000,'private','/page/private-page');

INSERT INTO blog_current_profile(id,version_id,version) VALUES(1,2,6);
INSERT INTO blog_profile(id,owner_id,name,bio,image_url,favicon_url,background_url,recorded) VALUES
 (1,1,'Earlier Blog','Earlier bio','/images/older-portrait.png','','',1600000000000000000),
 (2,1,'Example Blog','A test profile','/images/portrait.png','/images/icon.png','/images/background.png',1700000000000000000);
INSERT INTO blog_setup(email,complete) VALUES('admin@example.invalid',1);
INSERT INTO blog_media(id,filename,label,version,tags) VALUES
 ('public.png','public-original.png','Public image',3,'public, example'),
 ('private.png','private-original.png','Private image',2,'private'),
 ('unused.png','unused-original.png','Unused image',1,'unused');

INSERT INTO blog_post_images(version_id,url) VALUES
 (1,'/images/old.png'),(2,'/images/private.png'),(4,'/images/public.png'),(5,'/images/private.png');
INSERT INTO blog_page_images(version_id,url) VALUES
 (2,'/images/old.png'),(3,'/images/private.png'),(5,'/images/public.png'),(6,'/images/private.png');

CREATE TABLE other_version(version INTEGER NOT NULL, dirty BOOLEAN NOT NULL);
INSERT INTO other_version VALUES(73,0);
CREATE TABLE other_bookmarks(id INTEGER PRIMARY KEY, post_id TEXT REFERENCES blog_current_posts(id) ON DELETE RESTRICT);
INSERT INTO other_bookmarks VALUES(1,'live');
COMMIT;
