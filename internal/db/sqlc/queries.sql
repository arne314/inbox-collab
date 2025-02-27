-- name: MailCount :one
SELECT COUNT(*) FROM mail;

-- name: GetMail :one
SELECT * FROM mail
WHERE id = $1 LIMIT 1;

-- name: AddMail :exec
INSERT INTO mail (header_id, header_in_reply_to, header_references, timestamp, name_from, addr_from, addr_to, subject, body)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (header_id) DO NOTHING;

-- name: GetMailsRequiringMessageExtraction :many
SELECT * FROM mail
where messages ->> 'messages' IS NULL;

-- name: UpdateExtractedMessages :exec
UPDATE mail
SET messages = $2, last_message_extraction = CURRENT_TIMESTAMP
WHERE id = $1;

