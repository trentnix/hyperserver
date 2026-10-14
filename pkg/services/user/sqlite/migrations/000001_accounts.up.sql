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
    session_version INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE usertoken (
    user_id VARCHAR(36),
    token_hash VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    token_type VARCHAR(255) NOT NULL,
    PRIMARY KEY (user_id, token_hash),
    FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE
);

CREATE INDEX usertoken_expiration
    ON usertoken (julianday(expires_at), user_id, token_hash);
