// migration.go — IMAP migration assistant persistence (phase 11, migration 009).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrMigrationState = errors.New("migration job state transition not allowed")

type MigrationJob struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	MailboxID   uuid.UUID
	CreatedBy   uuid.UUID
	Source      json.RawMessage // {host,port,security,username,password_sealed}
	State       string
	Total       int
	Migrated    int
	Failed      int
	FolderMap   map[string]string
	LastError   *string
	HeartbeatAt *time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
}

type MigrationItem struct {
	ID        uuid.UUID
	JobID     uuid.UUID
	Folder    string
	UID       int64
	MessageID string
	Subject   string
	FromAddr  string
	Size      int64
	State     string
	Attempts  int
	Error     *string
}

const migrationJobSelect = `select id, org_id, mailbox_id, created_by, source, state, total_messages,
	migrated, failed, folder_map, last_error, heartbeat_at, started_at, completed_at, created_at
	from mail.migration_job`

func scanMigrationJob(row pgx.Row) (*MigrationJob, error) {
	var j MigrationJob
	var folderMap []byte
	err := row.Scan(&j.ID, &j.OrgID, &j.MailboxID, &j.CreatedBy, &j.Source, &j.State, &j.Total,
		&j.Migrated, &j.Failed, &folderMap, &j.LastError, &j.HeartbeatAt, &j.StartedAt, &j.CompletedAt, &j.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(folderMap, &j.FolderMap)
	return &j, nil
}

func CreateMigrationJob(ctx context.Context, tx pgx.Tx, j *MigrationJob) error {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	fm, _ := json.Marshal(j.FolderMap)
	_, err := tx.Exec(ctx, `insert into mail.migration_job (id, org_id, mailbox_id, created_by, source, state, folder_map)
		values ($1,$2,$3,$4,$5,$6,$7)`,
		j.ID, j.OrgID, j.MailboxID, j.CreatedBy, j.Source, j.State, fm)
	return err
}

func GetMigrationJob(ctx context.Context, q Querier, id uuid.UUID) (*MigrationJob, error) {
	return scanMigrationJob(q.QueryRow(ctx, migrationJobSelect+` where id=$1`, id))
}

func ListMigrationJobs(ctx context.Context, q Querier, orgID uuid.UUID) ([]*MigrationJob, error) {
	rows, err := q.Query(ctx, migrationJobSelect+` where org_id=$1 order by created_at desc limit 50`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*MigrationJob
	for rows.Next() {
		j, err := scanMigrationJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// TransitionMigrationJob locks the row and moves its state, enforcing the
// machine: queued → running|cancelled; running → paused|completed|failed|cancelled;
// paused → running|cancelled; terminal states are final.
func TransitionMigrationJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, allowedFrom []string, to string) error {
	var cur string
	if err := tx.QueryRow(ctx, `select state from mail.migration_job where id=$1 for update`, id).Scan(&cur); err != nil {
		return err
	}
	if !containsState(allowedFrom, cur) {
		return ErrMigrationState
	}
	_, err := tx.Exec(ctx, `update mail.migration_job set state=$2,
		started_at = case when $2='running' and started_at is null then now() else started_at end,
		completed_at = case when $2 in ('completed','failed','cancelled') then now() else completed_at end,
		heartbeat_at = case when $2='running' then now() else heartbeat_at end,
		updated_at=now()
		where id=$1`, id, to)
	return err
}

func containsState(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ClaimNextMigrationJob picks a queued/resumed job for the worker (FOR UPDATE
// SKIP LOCKED so two workers never take one job).
func ClaimNextMigrationJob(ctx context.Context, tx pgx.Tx) (*MigrationJob, error) {
	row := tx.QueryRow(ctx, `select id from mail.migration_job
		where state in ('queued','running') and (heartbeat_at is null or heartbeat_at < now() - interval '30 seconds')
		order by created_at limit 1 for update skip locked`)
	var id uuid.UUID
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	j, err := scanMigrationJob(tx.QueryRow(ctx, migrationJobSelect+` where id=$1`, id))
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `update mail.migration_job set state='running', heartbeat_at=now(),
		started_at=coalesce(started_at, now()), updated_at=now() where id=$1`, id)
	return j, err
}

func TouchMigrationHeartbeat(ctx context.Context, tx pgx.Tx, id uuid.UUID, migrated, failed int) error {
	_, err := tx.Exec(ctx, `update mail.migration_job set heartbeat_at=now(), migrated=$2, failed=$3, updated_at=now() where id=$1`,
		id, migrated, failed)
	return err
}

func SetMigrationJobTotals(ctx context.Context, tx pgx.Tx, id uuid.UUID, total int, lastErr *string) error {
	_, err := tx.Exec(ctx, `update mail.migration_job set total_messages=$2, last_error=$3, updated_at=now() where id=$1`,
		id, total, lastErr)
	return err
}

func ReplaceMigrationItems(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, items []MigrationItem) error {
	if _, err := tx.Exec(ctx, `delete from mail.migration_item where job_id=$1`, jobID); err != nil {
		return err
	}
	for _, it := range items {
		if err := CreateMigrationItem(ctx, tx, jobID, it); err != nil {
			return err
		}
	}
	return nil
}

func CreateMigrationItem(ctx context.Context, tx pgx.Tx, jobID uuid.UUID, it MigrationItem) error {
	_, err := tx.Exec(ctx, `insert into mail.migration_item (id, job_id, folder, uid, message_id, subject, from_addr, size_bytes, state)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		uuid.New(), jobID, it.Folder, it.UID, it.MessageID, it.Subject, it.FromAddr, it.Size, it.State)
	return err
}

// ClaimMigrationItem claims the next pending item for the job, bumps attempts,
// and marks it in-flight by transitioning to 'migrating' via attempts+1 and
// returning it (state stays pending until the result lands — attempts > 0 with
// state pending means in-flight).
func ClaimMigrationItem(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) (*MigrationItem, error) {
	var it MigrationItem
	var errCode *string
	err := tx.QueryRow(ctx, `update mail.migration_item set attempts=attempts+1, updated_at=now()
		where id = (select id from mail.migration_item where job_id=$1 and state='pending' order by folder, uid limit 1 for update skip locked)
		returning id, folder, uid, coalesce(message_id,''), coalesce(subject,''), coalesce(from_addr,''), size_bytes, state, attempts, error`,
		jobID).Scan(&it.ID, &it.Folder, &it.UID, &it.MessageID, &it.Subject, &it.FromAddr, &it.Size, &it.State, &it.Attempts, &errCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &it, err
}

func SetMigrationItemState(ctx context.Context, tx pgx.Tx, id uuid.UUID, state string, errText *string) error {
	_, err := tx.Exec(ctx, `update mail.migration_item set state=$2, error=$3, updated_at=now() where id=$1`, id, state, errText)
	return err
}

// ListMigrationItems: attention list (failed first) with a simple offset page.
func ListMigrationItems(ctx context.Context, q Querier, jobID uuid.UUID, state string, limit, offset int) ([]MigrationItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const itemsQ = `select id, folder, uid, coalesce(message_id,''), coalesce(subject,''), coalesce(from_addr,''),
		size_bytes, state, attempts, error from mail.migration_item where job_id=$1 and ($2='' or state=$2)
		order by case when state='failed' then 0 else 1 end, folder, uid limit $3 offset $4`
	rows, err := q.Query(ctx, itemsQ, jobID, state, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MigrationItem
	for rows.Next() {
		var it MigrationItem
		if err := rows.Scan(&it.ID, &it.Folder, &it.UID, &it.MessageID, &it.Subject, &it.FromAddr, &it.Size, &it.State, &it.Attempts, &it.Error); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
