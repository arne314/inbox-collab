-- name: MailCount :one
SELECT COUNT(*) FROM mail;

-- name: GetMail :one
SELECT * FROM mail
WHERE id = $1 LIMIT 1;

-- name: AddMail :many
INSERT INTO mail (header_id, header_in_reply_to, header_references, timestamp, name_from, addr_from, addr_to, subject, body)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (header_id) DO NOTHING
RETURNING *;

-- name: GetMailsRequiringMessageExtraction :many
SELECT * FROM mail
WHERE messages ->> 'messages' IS NULL;

-- name: GetMailsRequiringSorting :many
SELECT * FROM mail
WHERE thread IS NULL AND messages ->> 'messages' IS NOT NULL
ORDER BY timestamp;

-- name: GetReferencedThreadParent :many
SELECT * FROM mail
WHERE thread IS NOT NULL AND header_id = ANY($1::text[])
ORDER BY timestamp DESC
LIMIT 1;

-- name: UpdateExtractedMessages :exec
UPDATE mail
SET messages = $2, last_message_extraction = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UpdateMailSorting :exec
UPDATE mail
SET reply_to = $3, thread = $2
WHERE id = $1;

-- name: AutoUpdateMailReplyTo :execrows
UPDATE mail
SET reply_to = m.id
FROM mail m
WHERE mail.thread IS NULL AND mail.header_in_reply_to = m.header_id;

-- name: AddThread :one
INSERT INTO thread (enabled, last_message, first_mail, last_mail)
VALUES (true, CURRENT_TIMESTAMP, $1, $1)
RETURNING *;

-- name: UpdateThreadLastMessage :exec
UPDATE thread
SET last_message = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: UpdateThreadLastMail :exec
UPDATE thread
SET enabled = true, last_message = GREATEST(last_message, $3), last_mail = $2
WHERE id = $1;

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
SELECT thread.id, mail.subject FROM thread
JOIN mail ON thread.first_mail = mail.id
WHERE thread.matrix_id IS NULL;

-- name: GetMatrixReadyMails :many
SELECT mail.*, thread.matrix_id AS root_matrix_id FROM mail
JOIN thread ON mail.thread = thread.id
WHERE mail.matrix_id IS NULL AND thread.matrix_id IS NOT NULL
ORDER BY mail.timestamp;

-- name: UpdateThreadMatrixId :exec
UPDATE thread
SET matrix_id = $2
WHERE id = $1;

-- name: UpdateMailMatrixId :exec
UPDATE mail
SET matrix_id = $2
WHERE id = $1;

