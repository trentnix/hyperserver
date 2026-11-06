CREATE TABLE user (
    id VARCHAR(36) PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    email_key TEXT NOT NULL UNIQUE,
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    verification_required BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    registration_auth_type VARCHAR(50),
    password VARCHAR(255),
    session_version INTEGER NOT NULL DEFAULT 1,
    role TEXT NOT NULL DEFAULT '',
    invited BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE usertoken (
    user_id VARCHAR(36),
    token_hash VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    token_type VARCHAR(255) NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, token_hash),
    FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE
);

CREATE INDEX usertoken_expiration
    ON usertoken (julianday(expires_at), user_id, token_hash);

CREATE INDEX usertoken_lookup ON usertoken(token_hash, token_type);

CREATE TABLE external_identity (
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    user_id TEXT NOT NULL,
    PRIMARY KEY (issuer, subject),
    UNIQUE (user_id, issuer),
    FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE
);
