-- Populate every blog table with distinct values so migration tests detect
-- lost content, publication changes, reset edit versions, and broken references.
INSERT INTO blog_posts(id,slug,title,summary,markdown,published,draft,preview_image,version,tags) VALUES
 ('live','live','Published post','Public summary','![Public](/images/public.png)',1700000000,0,'/images/public.png',4,'public, example'),
 ('draft','draft','Draft post','Private summary','![Private](/images/private.png)',0,1,'/images/private.png',7,'private'),
 ('scheduled','scheduled','Scheduled post','Future summary','Future content',4102444800,0,'',2,'future');

INSERT INTO blog_pages(id,slug,title,markdown,published,parent_id,summary,preview_image,children_display,display_order,updated,ever_published,version,tags) VALUES
 ('parent','projects','Projects','Parent content',1,NULL,'Project previews','','blocks',10,1700000000000000000,1,3,'projects'),
 ('child','example','Example','![Preview](/images/public.png)',1,'parent','A project','/images/public.png','hidden',20,1700000000000000001,1,5,'example'),
 ('private-page','private-page','Private page','Private content',0,'parent','Not public','/images/private.png','list',NULL,1700000000000000002,1,8,'private');

INSERT INTO blog_profile(id,name,bio,image_url,favicon_url,background_url,version) VALUES
 (1,'Example Blog','A test profile','/images/portrait.png','/images/icon.png','/images/background.png',6);
INSERT INTO blog_setup(email,complete) VALUES('admin@example.invalid',1);
INSERT INTO blog_media(id,filename,label,version,tags) VALUES
 ('public.png','public-original.png','Public image',3,'public, example'),
 ('private.png','private-original.png','Private image',2,'private'),
 ('unused.png','unused-original.png','Unused image',1,'unused');

INSERT INTO blog_post_images(owner_id,url) VALUES
 ('live','/images/public.png'),('draft','/images/private.png');
INSERT INTO blog_page_images(owner_id,url) VALUES
 ('child','/images/public.png'),('private-page','/images/private.png');

INSERT INTO blog_post_revisions(owner_id,title,summary,markdown,preview_image,recorded,tags)
 SELECT id,'Earlier ' || title,summary,'Earlier body',preview_image,1600000000000000000,'older' FROM blog_posts ORDER BY id;
INSERT INTO blog_post_revisions(owner_id,title,summary,markdown,preview_image,recorded,tags)
 SELECT id,title,summary,markdown,preview_image,1700000000000000000,tags FROM blog_posts ORDER BY id;
INSERT INTO blog_page_revisions(owner_id,title,summary,markdown,preview_image,recorded,tags)
 SELECT id,'Earlier ' || title,summary,'Earlier body',preview_image,1600000000000000000,'older' FROM blog_pages ORDER BY id;
INSERT INTO blog_page_revisions(owner_id,title,summary,markdown,preview_image,recorded,tags)
 SELECT id,title,summary,markdown,preview_image,1700000000000000000,tags FROM blog_pages ORDER BY id;
INSERT INTO blog_profile_revisions(owner_id,name,bio,image_url,favicon_url,background_url,recorded) VALUES
 (1,'Earlier Blog','Earlier bio','/images/older-portrait.png','','',1600000000000000000),
 (1,'Example Blog','A test profile','/images/portrait.png','/images/icon.png','/images/background.png',1700000000000000000);

-- Other modules can share this database without sharing migration history.
CREATE TABLE other_version(version INTEGER NOT NULL, dirty BOOLEAN NOT NULL);
INSERT INTO other_version VALUES(73,0);
CREATE TABLE other_records(id TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO other_records VALUES('kept','Unrelated module data');
