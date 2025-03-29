-- name: MailCount :one
SELECT COUNT(*) FROM mail;

-- name: GetMail :one
SELECT * FROM mail
LEFT JOIN thread ON thread.id = mail.thread
WHERE mail.id = $1 LIMIT 1;

-- name: AddMail :many
INSERT INTO mail (fetcher, header_id, header_in_reply_to, header_references, timestamp, name_from, addr_from, addr_to, subject, body)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (header_id) DO NOTHING
RETURNING *;

-- name: GetMailsRequiringMessageExtraction :many
SELECT * FROM mail
WHERE messages ->> 'messages' IS NULL;

-- name: GetMailsRequiringSorting :many
SELECT * FROM mail
WHERE NOT sorted AND messages ->> 'messages' IS NOT NULL
ORDER BY timestamp;

-- name: GetReferencedThreadParent :many
SELECT * FROM mail
JOIN thread ON thread.id = mail.thread
WHERE header_id = ANY($1::text[]) AND NOT thread.force_close
ORDER BY timestamp DESC
LIMIT 1;

-- name: UpdateExtractedMessages :exec
UPDATE mail
SET messages = $2, last_message_extraction = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UpdateMailSorting :exec
UPDATE mail
SET reply_to = $3, thread = $2, sorted = TRUE
WHERE id = $1;

-- name: UpdateMailMarkSorted :exec
UPDATE mail
SET sorted = TRUE
WHERE id = $1;

-- name: AutoUpdateMailReplyTo :execrows
UPDATE mail
SET reply_to = m.id
FROM mail m
WHERE mail.thread IS NULL AND mail.header_in_reply_to = m.header_id;

-- name: AddThread :one
INSERT INTO thread (last_message, first_mail, last_mail)
VALUES (CURRENT_TIMESTAMP, $1, $1)
RETURNING *;

-- name: UpdateThreadLastMessage :exec
UPDATE thread
SET last_message = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UpdateThreadLastMail :exec
UPDATE thread
SET enabled = TRUE, last_message = GREATEST(last_message, $3), last_mail = $2
WHERE id = $1;

-- name: UpdateThreadEnabled :execrows
UPDATE thread
SET enabled = $3, force_close = COALESCE($4, force_close)
WHERE matrix_id = $1 AND matrix_room_id = $2 AND (enabled != $3 OR force_close != COALESCE($4, force_close));

-- name: AddFetcher :exec
INSERT INTO fetcher (id)
VALUES ($1);

-- name: GetFetcherState :many
SELECT * FROM fetcher
WHERE id = $1 LIMIT 1;

-- name: UpdateFetcherState :exec
UPDATE fetcher
SET uid_last = $2, uid_validity = $3
WHERE id = $1;

-- name: GetMatrixReadyThreads :many
SELECT thread.id, mail.fetcher, mail.addr_from, mail.addr_to, mail.subject, mail.name_from FROM thread
JOIN mail ON thread.first_mail = mail.id
WHERE thread.matrix_id IS NULL
ORDER BY mail.timestamp;

-- name: GetMatrixReadyMails :many
SELECT mail.*,
thread.matrix_id AS root_matrix_id, thread.matrix_room_id AS root_matrix_room_id, mail.id = thread.first_mail AS is_first
FROM mail
JOIN thread ON mail.thread = thread.id
WHERE mail.matrix_id IS NULL AND thread.matrix_id IS NOT NULL
ORDER BY mail.timestamp;

-- name: UpdateThreadMatrixIds :exec
UPDATE thread
SET matrix_id = $3, matrix_room_id = $2
WHERE id = $1;

-- name: UpdateMailMatrixId :exec
UPDATE mail
SET matrix_id = $2
WHERE id = $1;

-- name: GetOverviewThreads :many
SELECT thread.*, mail.name_from, mail.subject, mail.matrix_id AS message_id
FROM thread
JOIN mail ON mail.id = thread.first_mail
WHERE thread.enabled AND thread.matrix_room_id = ANY($1::text[]) AND thread.matrix_id IS NOT NULL
ORDER BY thread.last_message DESC;

-- name: GetRoom :one
SELECT * FROM room
WHERE id = $1 LIMIT 1;

-- name: AddRoom :exec
INSERT INTO room (id)
VALUES ($1)
ON CONFLICT (id) DO NOTHING;

-- name: UpdateRoomOverviewMessage :exec
UPDATE room
SET overview_message_id = $2, overview_message_last_update = CURRENT_TIMESTAMP
WHERE id = $1;

