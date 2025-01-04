CREATE TABLE hyperserver_contact_submission (
    name TXT,
    email TEXT NOT NULL,
    message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);