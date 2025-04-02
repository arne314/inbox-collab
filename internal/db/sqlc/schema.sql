CREATE TABLE fetcher (
    id TEXT PRIMARY KEY,
    uid_last INT NOT NULL DEFAULT 1,
    uid_validity INT NOT NULL DEFAULT 1
);

CREATE TABLE room (
    id TEXT PRIMARY KEY,
    name TEXT,
    name_last_update TIMESTAMP,
    overview_message_id TEXT,
    overview_message_last_update TIMESTAMP
);

CREATE TABLE thread (
    id BIGSERIAL PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    force_close BOOLEAN NOT NULL DEFAULT FALSE,
    last_message TIMESTAMP,
    matrix_id TEXT,
    matrix_room_id TEXT REFERENCES room(id) ON DELETE SET NULL ON UPDATE CASCADE
);

CREATE TABLE mail (
    id BIGSERIAL PRIMARY KEY,
    fetcher TEXT REFERENCES fetcher(id) ON DELETE SET NULL ON UPDATE CASCADE,
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
    messages_last_update TIMESTAMP,
    sorted BOOLEAN NOT NULL DEFAULT FALSE,
    reply_to BIGINT REFERENCES mail(id) ON DELETE SET NULL,
    thread BIGINT REFERENCES thread(id) ON DELETE SET NULL,
    matrix_id TEXT
);

ALTER TABLE thread ADD COLUMN first_mail BIGINT REFERENCES mail(id) ON DELETE SET NULL;
ALTER TABLE thread ADD COLUMN last_mail BIGINT REFERENCES mail(id) ON DELETE SET NULL;

