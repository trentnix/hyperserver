CREATE TABLE external_identity (
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    user_id TEXT NOT NULL,
    PRIMARY KEY (issuer, subject),
    UNIQUE (user_id, issuer),
    FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE
);
