// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.28.0
// source: queries.sql

package db

import (
	"context"

	db "github.com/arne314/inbox-collab/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

const addFetcher = `-- name: AddFetcher :exec
INSERT INTO fetcher (id)
VALUES ($1)
`

func (q *Queries) AddFetcher(ctx context.Context, id string) error {
	_, err := q.db.Exec(ctx, addFetcher, id)
	return err
}

const addMail = `-- name: AddMail :many
INSERT INTO mail (header_id, header_in_reply_to, header_references, timestamp, name_from, addr_from, addr_to, subject, body)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (header_id) DO NOTHING
RETURNING id, header_id, header_in_reply_to, header_references, timestamp, name_from, addr_from, addr_to, subject, body, messages, last_message_extraction, reply_to, thread, matrix_id
`

type AddMailParams struct {
	HeaderID         string
	HeaderInReplyTo  pgtype.Text
	HeaderReferences []string
	Timestamp        pgtype.Timestamp
	NameFrom         pgtype.Text
	AddrFrom         pgtype.Text
	AddrTo           []string
	Subject          string
	Body             *pgtype.Text
}

func (q *Queries) AddMail(ctx context.Context, arg AddMailParams) ([]*Mail, error) {
	rows, err := q.db.Query(ctx, addMail,
		arg.HeaderID,
		arg.HeaderInReplyTo,
		arg.HeaderReferences,
		arg.Timestamp,
		arg.NameFrom,
		arg.AddrFrom,
		arg.AddrTo,
		arg.Subject,
		arg.Body,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*Mail
	for rows.Next() {
		var i Mail
		if err := rows.Scan(
			&i.ID,
			&i.HeaderID,
			&i.HeaderInReplyTo,
			&i.HeaderReferences,
			&i.Timestamp,
			&i.NameFrom,
			&i.AddrFrom,
			&i.AddrTo,
			&i.Subject,
			&i.Body,
			&i.Messages,
			&i.LastMessageExtraction,
			&i.ReplyTo,
			&i.Thread,
			&i.MatrixID,
		); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const addThread = `-- name: AddThread :one
INSERT INTO thread (enabled, last_message, first_mail, last_mail)
VALUES (true, CURRENT_TIMESTAMP, $1, $1)
RETURNING id, enabled, last_message, matrix_id, first_mail, last_mail
`

func (q *Queries) AddThread(ctx context.Context, firstMail pgtype.Int8) (*Thread, error) {
	row := q.db.QueryRow(ctx, addThread, firstMail)
	var i Thread
	err := row.Scan(
		&i.ID,
		&i.Enabled,
		&i.LastMessage,
		&i.MatrixID,
		&i.FirstMail,
		&i.LastMail,
	)
	return &i, err
}

const autoUpdateMailReplyTo = `-- name: AutoUpdateMailReplyTo :execrows
UPDATE mail
SET reply_to = m.id
FROM mail m
WHERE mail.thread IS NULL AND mail.header_in_reply_to = m.header_id
`

func (q *Queries) AutoUpdateMailReplyTo(ctx context.Context) (int64, error) {
	result, err := q.db.Exec(ctx, autoUpdateMailReplyTo)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getFetcherState = `-- name: GetFetcherState :many
SELECT id, uid_last, uid_validity FROM fetcher
WHERE id = $1 LIMIT 1
`

func (q *Queries) GetFetcherState(ctx context.Context, id string) ([]*Fetcher, error) {
	rows, err := q.db.Query(ctx, getFetcherState, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*Fetcher
	for rows.Next() {
		var i Fetcher
		if err := rows.Scan(&i.ID, &i.UidLast, &i.UidValidity); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getMail = `-- name: GetMail :one
SELECT id, header_id, header_in_reply_to, header_references, timestamp, name_from, addr_from, addr_to, subject, body, messages, last_message_extraction, reply_to, thread, matrix_id FROM mail
WHERE id = $1 LIMIT 1
`

func (q *Queries) GetMail(ctx context.Context, id int64) (*Mail, error) {
	row := q.db.QueryRow(ctx, getMail, id)
	var i Mail
	err := row.Scan(
		&i.ID,
		&i.HeaderID,
		&i.HeaderInReplyTo,
		&i.HeaderReferences,
		&i.Timestamp,
		&i.NameFrom,
		&i.AddrFrom,
		&i.AddrTo,
		&i.Subject,
		&i.Body,
		&i.Messages,
		&i.LastMessageExtraction,
		&i.ReplyTo,
		&i.Thread,
		&i.MatrixID,
	)
	return &i, err
}

const getMailsRequiringMessageExtraction = `-- name: GetMailsRequiringMessageExtraction :many
SELECT id, header_id, header_in_reply_to, header_references, timestamp, name_from, addr_from, addr_to, subject, body, messages, last_message_extraction, reply_to, thread, matrix_id FROM mail
WHERE messages ->> 'messages' IS NULL
`

func (q *Queries) GetMailsRequiringMessageExtraction(ctx context.Context) ([]*Mail, error) {
	rows, err := q.db.Query(ctx, getMailsRequiringMessageExtraction)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*Mail
	for rows.Next() {
		var i Mail
		if err := rows.Scan(
			&i.ID,
			&i.HeaderID,
			&i.HeaderInReplyTo,
			&i.HeaderReferences,
			&i.Timestamp,
			&i.NameFrom,
			&i.AddrFrom,
			&i.AddrTo,
			&i.Subject,
			&i.Body,
			&i.Messages,
			&i.LastMessageExtraction,
			&i.ReplyTo,
			&i.Thread,
			&i.MatrixID,
		); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getMailsRequiringSorting = `-- name: GetMailsRequiringSorting :many
SELECT id, header_id, header_in_reply_to, header_references, timestamp, name_from, addr_from, addr_to, subject, body, messages, last_message_extraction, reply_to, thread, matrix_id FROM mail
WHERE thread IS NULL AND messages ->> 'messages' IS NOT NULL
ORDER BY timestamp
`

func (q *Queries) GetMailsRequiringSorting(ctx context.Context) ([]*Mail, error) {
	rows, err := q.db.Query(ctx, getMailsRequiringSorting)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*Mail
	for rows.Next() {
		var i Mail
		if err := rows.Scan(
			&i.ID,
			&i.HeaderID,
			&i.HeaderInReplyTo,
			&i.HeaderReferences,
			&i.Timestamp,
			&i.NameFrom,
			&i.AddrFrom,
			&i.AddrTo,
			&i.Subject,
			&i.Body,
			&i.Messages,
			&i.LastMessageExtraction,
			&i.ReplyTo,
			&i.Thread,
			&i.MatrixID,
		); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getMatrixReadyMails = `-- name: GetMatrixReadyMails :many
SELECT mail.id, mail.header_id, mail.header_in_reply_to, mail.header_references, mail.timestamp, mail.name_from, mail.addr_from, mail.addr_to, mail.subject, mail.body, mail.messages, mail.last_message_extraction, mail.reply_to, mail.thread, mail.matrix_id, thread.matrix_id AS root_matrix_id, mail.id = thread.first_mail AS is_first FROM mail
JOIN thread ON mail.thread = thread.id
WHERE mail.matrix_id IS NULL AND thread.matrix_id IS NOT NULL
ORDER BY mail.timestamp
`

type GetMatrixReadyMailsRow struct {
	ID                    int64
	HeaderID              string
	HeaderInReplyTo       pgtype.Text
	HeaderReferences      []string
	Timestamp             pgtype.Timestamp
	NameFrom              pgtype.Text
	AddrFrom              pgtype.Text
	AddrTo                []string
	Subject               string
	Body                  *pgtype.Text
	Messages              *db.ExtractedMessages
	LastMessageExtraction pgtype.Timestamp
	ReplyTo               pgtype.Int8
	Thread                pgtype.Int8
	MatrixID              pgtype.Text
	RootMatrixID          pgtype.Text
	IsFirst               bool
}

func (q *Queries) GetMatrixReadyMails(ctx context.Context) ([]*GetMatrixReadyMailsRow, error) {
	rows, err := q.db.Query(ctx, getMatrixReadyMails)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*GetMatrixReadyMailsRow
	for rows.Next() {
		var i GetMatrixReadyMailsRow
		if err := rows.Scan(
			&i.ID,
			&i.HeaderID,
			&i.HeaderInReplyTo,
			&i.HeaderReferences,
			&i.Timestamp,
			&i.NameFrom,
			&i.AddrFrom,
			&i.AddrTo,
			&i.Subject,
			&i.Body,
			&i.Messages,
			&i.LastMessageExtraction,
			&i.ReplyTo,
			&i.Thread,
			&i.MatrixID,
			&i.RootMatrixID,
			&i.IsFirst,
		); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getMatrixReadyThreads = `-- name: GetMatrixReadyThreads :many
SELECT thread.id, mail.subject, mail.name_from FROM thread
JOIN mail ON thread.first_mail = mail.id
WHERE thread.matrix_id IS NULL
ORDER BY mail.timestamp
`

type GetMatrixReadyThreadsRow struct {
	ID       int64
	Subject  string
	NameFrom pgtype.Text
}

func (q *Queries) GetMatrixReadyThreads(ctx context.Context) ([]*GetMatrixReadyThreadsRow, error) {
	rows, err := q.db.Query(ctx, getMatrixReadyThreads)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*GetMatrixReadyThreadsRow
	for rows.Next() {
		var i GetMatrixReadyThreadsRow
		if err := rows.Scan(&i.ID, &i.Subject, &i.NameFrom); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getReferencedThreadParent = `-- name: GetReferencedThreadParent :many
SELECT id, header_id, header_in_reply_to, header_references, timestamp, name_from, addr_from, addr_to, subject, body, messages, last_message_extraction, reply_to, thread, matrix_id FROM mail
WHERE thread IS NOT NULL AND header_id = ANY($1::text[])
ORDER BY timestamp DESC
LIMIT 1
`

func (q *Queries) GetReferencedThreadParent(ctx context.Context, dollar_1 []string) ([]*Mail, error) {
	rows, err := q.db.Query(ctx, getReferencedThreadParent, dollar_1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*Mail
	for rows.Next() {
		var i Mail
		if err := rows.Scan(
			&i.ID,
			&i.HeaderID,
			&i.HeaderInReplyTo,
			&i.HeaderReferences,
			&i.Timestamp,
			&i.NameFrom,
			&i.AddrFrom,
			&i.AddrTo,
			&i.Subject,
			&i.Body,
			&i.Messages,
			&i.LastMessageExtraction,
			&i.ReplyTo,
			&i.Thread,
			&i.MatrixID,
		); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const mailCount = `-- name: MailCount :one
SELECT COUNT(*) FROM mail
`

func (q *Queries) MailCount(ctx context.Context) (int64, error) {
	row := q.db.QueryRow(ctx, mailCount)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const updateExtractedMessages = `-- name: UpdateExtractedMessages :exec
UPDATE mail
SET messages = $2, last_message_extraction = CURRENT_TIMESTAMP
WHERE id = $1
`

type UpdateExtractedMessagesParams struct {
	ID       int64
	Messages *db.ExtractedMessages
}

func (q *Queries) UpdateExtractedMessages(ctx context.Context, arg UpdateExtractedMessagesParams) error {
	_, err := q.db.Exec(ctx, updateExtractedMessages, arg.ID, arg.Messages)
	return err
}

const updateFetcherState = `-- name: UpdateFetcherState :exec
UPDATE fetcher
SET uid_last = $2, uid_validity = $3
WHERE id = $1
`

type UpdateFetcherStateParams struct {
	ID          string
	UidLast     int32
	UidValidity int32
}

func (q *Queries) UpdateFetcherState(ctx context.Context, arg UpdateFetcherStateParams) error {
	_, err := q.db.Exec(ctx, updateFetcherState, arg.ID, arg.UidLast, arg.UidValidity)
	return err
}

const updateMailMatrixId = `-- name: UpdateMailMatrixId :exec
UPDATE mail
SET matrix_id = $2
WHERE id = $1
`

type UpdateMailMatrixIdParams struct {
	ID       int64
	MatrixID pgtype.Text
}

func (q *Queries) UpdateMailMatrixId(ctx context.Context, arg UpdateMailMatrixIdParams) error {
	_, err := q.db.Exec(ctx, updateMailMatrixId, arg.ID, arg.MatrixID)
	return err
}

const updateMailSorting = `-- name: UpdateMailSorting :exec
UPDATE mail
SET reply_to = $3, thread = $2
WHERE id = $1
`

type UpdateMailSortingParams struct {
	ID      int64
	Thread  pgtype.Int8
	ReplyTo pgtype.Int8
}

func (q *Queries) UpdateMailSorting(ctx context.Context, arg UpdateMailSortingParams) error {
	_, err := q.db.Exec(ctx, updateMailSorting, arg.ID, arg.Thread, arg.ReplyTo)
	return err
}

const updateThreadLastMail = `-- name: UpdateThreadLastMail :exec
UPDATE thread
SET enabled = true, last_message = GREATEST(last_message, $3), last_mail = $2
WHERE id = $1
`

type UpdateThreadLastMailParams struct {
	ID          int64
	LastMail    pgtype.Int8
	LastMessage pgtype.Timestamp
}

func (q *Queries) UpdateThreadLastMail(ctx context.Context, arg UpdateThreadLastMailParams) error {
	_, err := q.db.Exec(ctx, updateThreadLastMail, arg.ID, arg.LastMail, arg.LastMessage)
	return err
}

const updateThreadLastMessage = `-- name: UpdateThreadLastMessage :exec
UPDATE thread
SET last_message = CURRENT_TIMESTAMP
WHERE id = $1
`

func (q *Queries) UpdateThreadLastMessage(ctx context.Context, id int64) error {
	_, err := q.db.Exec(ctx, updateThreadLastMessage, id)
	return err
}

const updateThreadMatrixId = `-- name: UpdateThreadMatrixId :exec
UPDATE thread
SET matrix_id = $2
WHERE id = $1
`

type UpdateThreadMatrixIdParams struct {
	ID       int64
	MatrixID pgtype.Text
}

func (q *Queries) UpdateThreadMatrixId(ctx context.Context, arg UpdateThreadMatrixIdParams) error {
	_, err := q.db.Exec(ctx, updateThreadMatrixId, arg.ID, arg.MatrixID)
	return err
}
