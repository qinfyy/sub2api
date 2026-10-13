package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"modernc.org/sqlite"
)

func isSQLiteAuditDB(db *sql.DB) bool {
	if db == nil {
		return false
	}
	_, ok := db.Driver().(*sqlite.Driver)
	return ok
}

func (r *PostgreSQLRepository) createStagingSQLite(ctx context.Context, snapshot PromptSnapshot, version int64, attempts, capacity int) (*Job, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Acquire the writer before checking capacity, including when the queue is empty.
	if _, err := tx.ExecContext(ctx, `UPDATE prompt_audit_jobs SET status=status WHERE id=-1`); err != nil {
		return nil, err
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM prompt_audit_jobs WHERE status IN ('staging','queued','processing','retry')`).Scan(&active); err != nil {
		return nil, err
	}
	if capacity <= 0 || active >= capacity {
		return nil, ErrQueueFull
	}
	if attempts <= 0 {
		attempts = 3
	}
	job, err := insertJob(ctx, tx, snapshot.Redacted(), ModeAsync, version, "staging", attempts)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return job, nil
}

func (r *PostgreSQLRepository) claimNextSQLite(ctx context.Context, now time.Time) (*Job, bool, error) {
	row := r.db.QueryRowContext(ctx, `UPDATE prompt_audit_jobs
		SET status='processing', attempts=attempts+1, claim_version=claim_version+1,
		processing_started_at=$1, updated_at=$1
		WHERE id=(SELECT id FROM prompt_audit_jobs WHERE status IN ('queued','retry')
		AND pg_rfc3339_micros(next_attempt_at)<=pg_rfc3339_micros($1)
		ORDER BY pg_rfc3339_micros(next_attempt_at),id LIMIT 1)
		RETURNING `+jobColumns("prompt_audit_jobs"), now.UTC().Format(time.RFC3339Nano))
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return job, err == nil, err
}

func (r *PostgreSQLRepository) reclaimStaleSQLite(ctx context.Context, stagingBefore, processingBefore time.Time, limit int) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE prompt_audit_jobs SET
		status=CASE WHEN status='staging' THEN 'failed' WHEN attempts<max_attempts THEN 'retry' ELSE 'failed' END,
		next_attempt_at=CASE WHEN status='processing' AND attempts<max_attempts THEN NOW() ELSE next_attempt_at END,
		processing_started_at=NULL,
		processed_at=CASE WHEN status='staging' OR attempts>=max_attempts THEN NOW() ELSE NULL END,
		last_error_code=CASE WHEN status='staging' THEN 'staging_timeout' ELSE 'processing_lease_expired' END,
		last_error_message='', updated_at=NOW()
		WHERE id IN (SELECT id FROM prompt_audit_jobs WHERE
		(status='staging' AND pg_rfc3339_micros(updated_at)<pg_rfc3339_micros($1)) OR
		(status='processing' AND pg_rfc3339_micros(processing_started_at)<pg_rfc3339_micros($2))
		ORDER BY pg_rfc3339_micros(updated_at),id LIMIT $3)`, stagingBefore.UTC(), processingBefore.UTC(), limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func auditIDList(ids []int64) string {
	raw, _ := json.Marshal(ids)
	return string(raw)
}

func deleteOrphanJobsSQLite(ctx context.Context, tx *sql.Tx, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM prompt_audit_jobs
		WHERE id IN (SELECT value FROM json_each(?)) AND status<>'processing'
		AND NOT EXISTS (SELECT 1 FROM prompt_audit_events e WHERE e.job_id=prompt_audit_jobs.id)`, auditIDList(ids))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
