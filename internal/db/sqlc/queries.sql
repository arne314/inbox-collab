-- name: MailCount :one
SELECT COUNT(*) FROM mail;

-- name: GetMail :one
SELECT * FROM mail
WHERE id = $1 LIMIT 1;

-- name: AddMail :exec
INSERT INTO mail (mail_id, date, addr_from, addr_to, body)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (mail_id) DO NOTHING;

