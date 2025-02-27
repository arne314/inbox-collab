CREATE TABLE thread (
    id BIGSERIAL PRIMARY KEY,
    enabled BOOLEAN,
    last_message TIMESTAMP
);

CREATE TABLE mail (
    id BIGSERIAL PRIMARY KEY,
    header_id TEXT UNIQUE NOT NULL,
    header_in_reply_to TEXT,
    header_references TEXT[],
    timestamp TIMESTAMP NOT NULL,
    name_from TEXT,
    addr_from TEXT,
    addr_to TEXT[],
    subject TEXT NOT NULL,
    body TEXT,
    messages JSONB,
    last_message_extraction TIMESTAMP,
    reply_to BIGINT REFERENCES mail(id) ON DELETE SET NULL,
    thread BIGINT REFERENCES thread(id) ON DELETE SET NULL
);

ALTER TABLE thread ADD COLUMN first_mail BIGINT REFERENCES mail(id) ON DELETE SET NULL;
ALTER TABLE thread ADD COLUMN last_mail BIGINT REFERENCES mail(id) ON DELETE SET NULL;

