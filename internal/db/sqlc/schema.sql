CREATE TABLE thread (
    id BIGSERIAL PRIMARY KEY,
    enabled BOOLEAN,
    last_message TIMESTAMP
);

CREATE TABLE mail (
    id BIGSERIAL PRIMARY KEY,
    mail_id VARCHAR(320) UNIQUE NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    addr_from TEXT,
    addr_to TEXT,
    subject TEXT NOT NULL,
    body TEXT,
    messages JSONB,
    last_message_extraction TIMESTAMP,
    reply_to BIGINT REFERENCES mail(id) ON DELETE SET NULL,
    thread BIGINT REFERENCES thread(id) ON DELETE SET NULL
);

ALTER TABLE thread ADD COLUMN first_mail BIGINT REFERENCES mail(id) ON DELETE SET NULL;
ALTER TABLE thread ADD COLUMN last_mail BIGINT REFERENCES mail(id) ON DELETE SET NULL;

