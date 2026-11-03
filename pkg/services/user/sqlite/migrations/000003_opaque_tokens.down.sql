-- Opaque links cannot be used by the older binary either.
DELETE FROM usertoken;
DROP INDEX usertoken_lookup;
ALTER TABLE usertoken DROP COLUMN email;
