CREATE TABLE IF NOT EXISTS hyperserver_contact_submission (
    id VARCHAR(36) PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    name TEXT,
    message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
