-- name: MailCount :one
SELECT COUNT(*) FROM mail;

-- name: GetMail :one
SELECT * FROM mail
WHERE id = $1 LIMIT 1;

-- name: AddMail :exec
INSERT INTO mail (mail_id, timestamp, addr_from, addr_to, subject, body)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (mail_id) DO NOTHING;

-- name: GetMailsRequiringMessageExtraction :many
SELECT * FROM mail
where messages ->> 'messages' IS NULL;

-- name: UpdateExtractedMessages :exec
UPDATE mail
SET messages = $2, last_message_extraction = CURRENT_TIMESTAMP
WHERE id = $1;

