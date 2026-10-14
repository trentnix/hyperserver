CREATE TABLE "sessions" (
    id VARCHAR(36) PRIMARY KEY,
    session TEXT,
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX "sessions_expiration" ON "sessions" (julianday(expires_at), id);
