package store

// Idempotency-Key replay store (build register §1.9): the first response to a
// keyed request is stored per user + key and replayed on retry, so the webmail
// can double-submit safely.

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"synkra.email/mail/internal/model"
)

func GetIdempotentResponse(ctx context.Context, q Querier, userID uuid.UUID, key, endpoint string) (*model.Idempotency, error) {
	row := q.QueryRow(ctx, `select id, user_id, key, endpoint, request_hash, status, response, created_at
		from mail.idempotency where user_id=$1 and key=$2 and endpoint=$3`, userID, key, endpoint)
	var r model.Idempotency
	err := row.Scan(&r.ID, &r.UserID, &r.Key, &r.Endpoint, &r.RequestHash, &r.Status, &r.Response, &r.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &r, nil
}

// InsertIdempotentResponse stores the response of the first keyed request. A
// concurrent retry with the same key loses the race silently (on conflict do
// nothing) — that retry already replayed or will replay the stored response.
func InsertIdempotentResponse(ctx context.Context, tx pgx.Tx, r *model.Idempotency) error {
	_, err := tx.Exec(ctx, `insert into mail.idempotency (id, user_id, key, endpoint, request_hash, status, response)
		values ($1,$2,$3,$4,$5,$6,$7) on conflict (user_id, key) do nothing`,
		r.ID, r.UserID, r.Key, r.Endpoint, r.RequestHash, r.Status, r.Response)
	return err
}
