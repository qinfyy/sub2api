package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/imagequeueentry"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type memoryBatchImageQueue struct {
	client *dbent.Client
	core   *memoryCacheCore
}

func NewMemoryBatchImageQueue(core *memoryCacheCore, db *sql.DB) service.BatchImageQueue {
	q := &memoryBatchImageQueue{core: memoryAssertCore(core)}
	if db != nil {
		name := dialect.SQLite
		if isPostgresDB(db) {
			name = dialect.Postgres
		}
		q.client = dbent.NewClient(dbent.Driver(entsql.OpenDB(name, db)))
	}
	return q
}

var _ service.BatchImageQueue = (*memoryBatchImageQueue)(nil)

func (q *memoryBatchImageQueue) validate(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.client == nil {
		return errors.New("durable image queue is unavailable")
	}
	if !service.IsValidBatchImageID(id) {
		return service.ErrInvalidBatchImageQueuePayload
	}
	return nil
}
func (q *memoryBatchImageQueue) Enqueue(ctx context.Context, id string) error {
	if err := q.validate(ctx, id); err != nil {
		return err
	}
	r, err := clientFromContext(ctx, q.client).ExecContext(ctx, `INSERT INTO memory_batch_queue(batch_id,state,available_at) VALUES($1, 'ready', $2) ON CONFLICT(batch_id) DO NOTHING`, id, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n == 0 {
		return service.ErrBatchImageAlreadyQueued
	}
	return err
}
func (q *memoryBatchImageQueue) Reserve(ctx context.Context, timeout time.Duration) (service.ReservedBatchImageJob, error) {
	if q.client == nil {
		return service.ReservedBatchImageJob{}, errors.New("durable image queue is unavailable")
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return service.ReservedBatchImageJob{}, err
		}
		receipt, err := newBatchImageLockToken()
		if err != nil {
			return service.ReservedBatchImageJob{}, err
		}
		job, err := q.claim(ctx, receipt)
		if err == nil {
			return job, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return job, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return job, service.ErrBatchImageQueueEmpty
		}
		wait := 100 * time.Millisecond
		if remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return job, ctx.Err()
		case <-timer.C:
		}
	}
}
func (q *memoryBatchImageQueue) claim(ctx context.Context, receipt string) (service.ReservedBatchImageJob, error) {
	client := clientFromContext(ctx, q.client)
	selectReady := `SELECT batch_id FROM memory_batch_queue WHERE state='ready' ORDER BY available_at,batch_id LIMIT 1`
	if client.Driver().Dialect() == dialect.Postgres {
		selectReady += ` FOR UPDATE SKIP LOCKED`
	}
	rows, err := client.QueryContext(ctx, `UPDATE memory_batch_queue SET state='active', heartbeat_at=$1, receipt=$2 WHERE batch_id=(`+selectReady+`) AND state='ready' RETURNING batch_id,receipt`, time.Now().UnixMilli(), receipt)
	var job service.ReservedBatchImageJob
	if err != nil {
		return job, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return job, err
		}
		return job, sql.ErrNoRows
	}
	if err := rows.Scan(&job.BatchID, &job.Receipt); err != nil {
		return job, err
	}
	if rows.Next() {
		return job, errors.New("multiple jobs returned for one queue claim")
	}
	return job, rows.Err()
}

func queueClaimCount(n int, err error) error {
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrBatchImageLockNotAcquired
	}
	return err
}
func (q *memoryBatchImageQueue) RequeueAfter(ctx context.Context, id string, delay time.Duration) error {
	if err := q.validate(ctx, id); err != nil {
		return err
	}
	receipt := service.BatchImageReservationReceipt(ctx, id)
	if receipt == "" {
		return service.ErrBatchImageLockNotAcquired
	}
	state := imagequeueentry.StateReady
	if delay > 0 {
		state = imagequeueentry.StateDelayed
	}
	return queueClaimCount(clientFromContext(ctx, q.client).ImageQueueEntry.Update().
		Where(imagequeueentry.IDEQ(id), imagequeueentry.StateEQ(imagequeueentry.StateActive), imagequeueentry.ReceiptEQ(receipt)).
		SetState(state).SetAvailableAt(time.Now().Add(delay).UnixMilli()).SetHeartbeatAt(0).SetReceipt("").Save(ctx))
}
func (q *memoryBatchImageQueue) Ack(ctx context.Context, id string) error {
	if err := q.validate(ctx, id); err != nil {
		return err
	}
	receipt := service.BatchImageReservationReceipt(ctx, id)
	if receipt == "" {
		return service.ErrBatchImageLockNotAcquired
	}
	return queueClaimCount(clientFromContext(ctx, q.client).ImageQueueEntry.Delete().
		Where(imagequeueentry.IDEQ(id), imagequeueentry.StateEQ(imagequeueentry.StateActive), imagequeueentry.ReceiptEQ(receipt)).Exec(ctx))
}
func (q *memoryBatchImageQueue) Heartbeat(ctx context.Context, id string) error {
	if err := q.validate(ctx, id); err != nil {
		return err
	}
	receipt := service.BatchImageReservationReceipt(ctx, id)
	if receipt == "" {
		return service.ErrBatchImageLockNotAcquired
	}
	return queueClaimCount(clientFromContext(ctx, q.client).ImageQueueEntry.Update().
		Where(imagequeueentry.IDEQ(id), imagequeueentry.StateEQ(imagequeueentry.StateActive), imagequeueentry.ReceiptEQ(receipt)).
		SetHeartbeatAt(time.Now().UnixMilli()).Save(ctx))
}
func (q *memoryBatchImageQueue) MoveDueDelayedToReady(ctx context.Context, limit int) (int, error) {
	if q.client == nil {
		return 0, errors.New("durable image queue is unavailable")
	}
	if limit <= 0 {
		limit = 100
	}
	r, err := clientFromContext(ctx, q.client).ExecContext(ctx, `UPDATE memory_batch_queue SET state='ready' WHERE state='delayed' AND available_at<=$1 AND batch_id IN (SELECT batch_id FROM memory_batch_queue WHERE state='delayed' AND available_at<=$1 ORDER BY available_at,batch_id LIMIT $2)`, time.Now().UnixMilli(), limit)
	if err != nil {
		return 0, err
	}
	n, err := r.RowsAffected()
	return int(n), err
}
func (q *memoryBatchImageQueue) RecoverStaleActive(ctx context.Context, stale time.Duration, limit int) (int, error) {
	if q.client == nil {
		return 0, errors.New("durable image queue is unavailable")
	}
	if stale <= 0 {
		return 0, service.ErrInvalidBatchImageQueuePayload
	}
	if limit <= 0 {
		limit = 100
	}
	r, err := clientFromContext(ctx, q.client).ExecContext(ctx, `UPDATE memory_batch_queue SET state='ready',available_at=$1,receipt='' WHERE state='active' AND heartbeat_at<=$2 AND batch_id IN (SELECT batch_id FROM memory_batch_queue WHERE state='active' AND heartbeat_at<=$2 ORDER BY heartbeat_at,batch_id LIMIT $3)`, time.Now().UnixMilli(), time.Now().Add(-stale).UnixMilli(), limit)
	if err != nil {
		return 0, err
	}
	n, err := r.RowsAffected()
	return int(n), err
}
func (q *memoryBatchImageQueue) TryAcquireJobLock(ctx context.Context, id string, ttl time.Duration) (service.BatchImageJobLock, bool, error) {
	if err := q.validate(ctx, id); err != nil {
		return nil, false, err
	}
	if ttl <= 0 {
		ttl = defaultBatchImageJobLockTTL
	}
	token, err := newBatchImageLockToken()
	if err != nil {
		return nil, false, err
	}
	key := "batch-image:" + id
	c := memoryLeaderLockCache{q.core}
	ok, err := c.TryAcquireLeaderLock(ctx, key, token, ttl)
	if err != nil || !ok {
		return nil, false, err
	}
	return &memoryBatchImageJobLock{core: q.core, key: key, token: token}, true, nil
}

type memoryBatchImageJobLock struct {
	core       *memoryCacheCore
	key, token string
}

func (l *memoryBatchImageJobLock) Release(ctx context.Context) error {
	c := memoryLeaderLockCache{l.core}
	return c.ReleaseLeaderLock(ctx, l.key, l.token)
}
func (l *memoryBatchImageJobLock) Refresh(ctx context.Context, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = defaultBatchImageJobLockTTL
	}
	l.core.mu.Lock()
	defer l.core.mu.Unlock()
	e := l.core.entries["leader:"+l.key]
	if e == nil || l.core.expiredLocked(e) || e.value != l.token {
		return service.ErrBatchImageLockNotAcquired
	}
	e.expiry = l.core.now().Add(ttl)
	return nil
}

var _ service.BatchImageJobLockRefresher = (*memoryBatchImageJobLock)(nil)

type memoryBatchImageDownloadLimiter struct {
	core *memoryCacheCore
	max  int
	ttl  time.Duration
}

func NewMemoryBatchImageDownloadLimiter(core *memoryCacheCore, cfg *config.Config) service.BatchImageDownloadLimiter {
	l := &memoryBatchImageDownloadLimiter{memoryAssertCore(core), defaultBatchImageDownloadConcurrency, defaultBatchImageDownloadActiveTTL}
	if cfg != nil {
		if cfg.BatchImage.MaxDownloadConcurrencyPerUser > 0 {
			l.max = cfg.BatchImage.MaxDownloadConcurrencyPerUser
		}
		if cfg.BatchImage.MaxDownloadDurationSeconds > 0 {
			l.ttl = time.Duration(cfg.BatchImage.MaxDownloadDurationSeconds) * time.Second
		}
	}
	return l
}
func (l *memoryBatchImageDownloadLimiter) Acquire(ctx context.Context, userID, kind string) (service.BatchImageDownloadPermit, error) {
	id, err := newBatchImageLockToken()
	if err != nil {
		return nil, err
	}
	key := defaultBatchImageDownloadActivePrefix + userID
	err = changeMemoryMembers(ctx, l.core, key, func(v memoryMembers) error {
		if len(v) >= l.max {
			return service.ErrBatchImageDownloadLimited
		}
		v[id] = l.core.now().Add(l.ttl).UnixNano()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &memoryBatchImageDownloadPermit{l.core, key, id}, nil
}

type memoryBatchImageDownloadPermit struct {
	core    *memoryCacheCore
	key, id string
}

func (p *memoryBatchImageDownloadPermit) Release(ctx context.Context) error {
	return changeMemoryMembers(ctx, p.core, p.key, func(v memoryMembers) error { delete(v, p.id); return nil })
}
