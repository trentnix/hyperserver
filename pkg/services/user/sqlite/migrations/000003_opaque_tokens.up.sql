-- Previously issued JWT links are invalidated. Accounts and identities are unchanged.
DELETE FROM usertoken;
ALTER TABLE usertoken ADD COLUMN email TEXT NOT NULL DEFAULT '';
CREATE INDEX usertoken_lookup ON usertoken(token_hash, token_type);
