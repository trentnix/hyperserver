DELETE FROM usertoken WHERE token_type = 'auth-invitation';
ALTER TABLE user DROP COLUMN invited;
ALTER TABLE user DROP COLUMN role;
