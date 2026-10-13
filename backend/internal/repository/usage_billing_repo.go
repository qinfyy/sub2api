package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func nowSQLiteUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func parseSQLiteTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("parse sqlite time %q", raw)
}

func nowSQLiteDayStart(now time.Time) string {
	return now.UTC().Truncate(24 * time.Hour).Format(time.RFC3339Nano)
}

type usageBillingRepository struct {
	db     *sql.DB
	sqlite bool
}

func NewUsageBillingRepository(_ *dbent.Client, sqlDB *sql.DB) service.UsageBillingRepository {
	return &usageBillingRepository{db: sqlDB, sqlite: isSQLiteDB(sqlDB)}
}

func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (_ *service.UsageBillingApplyResult, err error) {
	if cmd == nil {
		return &service.UsageBillingApplyResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.UsageBillingApplyResult{Applied: false}, nil
	}

	result := &service.UsageBillingApplyResult{Applied: true}
	if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) claimUsageBillingKey(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	return r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
}

func (r *usageBillingRepository) claimUsageBillingRequest(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	if r.sqlite {
		return claimUsageBillingRequestSQLite(ctx, tx, requestID, apiKeyID, requestFingerprint)
	}
	return claimUsageBillingRequestPostgres(ctx, tx, requestID, apiKeyID, requestFingerprint)
}

func claimUsageBillingRequestPostgres(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES ($1, $2, $3)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id
	`, requestID, apiKeyID, requestFingerprint).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = $1 AND api_key_id = $2
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&archivedFingerprint)
	if err == nil {
		if strings.TrimSpace(archivedFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func claimUsageBillingRequestSQLite(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES (?, ?, ?)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
	`, requestID, apiKeyID, requestFingerprint)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = ? AND api_key_id = ?
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}

	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = ? AND api_key_id = ?
	`, requestID, apiKeyID).Scan(&archivedFingerprint)
	if err == nil {
		if strings.TrimSpace(archivedFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) ReserveBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, r.reserveUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, r.captureUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) ReleaseBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, r.releaseUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) applyBatchImageBalanceHold(
	ctx context.Context,
	cmd *service.BatchImageBalanceHoldCommand,
	apply func(context.Context, *sql.Tx, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error),
) (_ *service.BatchImageBalanceHoldResult, err error) {
	if cmd == nil {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.BatchImageBalanceHoldResult{Applied: false}, nil
	}

	result, err := apply(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &service.BatchImageBalanceHoldResult{}
	}
	result.Applied = true

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) applyUsageBillingEffects(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	if cmd.SubscriptionCost > 0 && cmd.SubscriptionID != nil {
		if err := incrementUsageBillingSubscription(ctx, tx, *cmd.SubscriptionID, cmd.SubscriptionCost); err != nil {
			return err
		}
	}

	if cmd.BalanceCost > 0 {
		newBalance, sufficient, err := r.deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost)
		if err != nil {
			return err
		}
		result.NewBalance = &newBalance
		result.BalanceOverdrafted = !sufficient
	}

	// Key 已不存在时跳过其自身的额度/限速计数，其余结算项不受影响。
	if cmd.APIKeyQuotaCost > 0 {
		exhausted, err := r.incrementUsageBillingAPIKeyQuota(ctx, tx, cmd.APIKeyID, cmd.APIKeyQuotaCost)
		if err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
		result.APIKeyQuotaExhausted = exhausted
	}

	if cmd.APIKeyRateLimitCost > 0 {
		if err := r.incrementUsageBillingAPIKeyRateLimit(ctx, tx, cmd.APIKeyID, cmd.APIKeyRateLimitCost); err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
	}

	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		quotaState, err := r.incrementUsageBillingAccountQuota(ctx, tx, cmd.AccountID, cmd.AccountQuotaCost)
		if err != nil {
			return err
		}
		result.QuotaState = quotaState
	}

	return nil
}

func incrementUsageBillingSubscription(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`
	res, err := tx.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

func (r *usageBillingRepository) deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	if r.sqlite {
		return deductUsageBillingBalanceSQLite(ctx, tx, userID, amount)
	}
	return deductUsageBillingBalancePostgres(ctx, tx, userID, amount)
}

// deductUsageBillingBalance retains the PostgreSQL-only entry point used by
// repository unit tests and any package-internal caller that is known to be PG.
func deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	return deductUsageBillingBalancePostgres(ctx, tx, userID, amount)
}

func deductUsageBillingBalancePostgres(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if err == nil {
		return newBalance, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, service.ErrUserNotFound
	}
	if err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func deductUsageBillingBalanceSQLite(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE users
		SET balance = balance - ?,
			updated_at = ?
		WHERE id = ? AND deleted_at IS NULL AND balance >= ?
	`, amount, nowSQLiteUTC(), userID, amount)
	if err != nil {
		return 0, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if affected > 0 {
		var newBalance float64
		if err := tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = ?`, userID).Scan(&newBalance); err != nil {
			return 0, false, err
		}
		return newBalance, true, nil
	}

	res, err = tx.ExecContext(ctx, `
		UPDATE users
		SET balance = balance - ?,
			updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, amount, nowSQLiteUTC(), userID)
	if err != nil {
		return 0, false, err
	}
	affected, err = res.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if affected == 0 {
		return 0, false, service.ErrUserNotFound
	}
	var newBalance float64
	if err := tx.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = ?`, userID).Scan(&newBalance); err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func (r *usageBillingRepository) reserveUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r.sqlite {
		return reserveUsageBillingBatchImageBalanceSQLite(ctx, tx, cmd)
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			frozen_balance = COALESCE(frozen_balance, 0) + $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, service.ErrBatchImageInsufficientBalance
}

func reserveUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return (&usageBillingRepository{}).reserveUsageBillingBatchImageBalance(ctx, tx, cmd)
}

func reserveUsageBillingBatchImageBalanceSQLite(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE users
		SET balance = balance - ?,
			frozen_balance = COALESCE(frozen_balance, 0) + ?,
			updated_at = ?
		WHERE id = ? AND deleted_at IS NULL AND balance >= ?
	`, cmd.HoldAmount, cmd.HoldAmount, nowSQLiteUTC(), cmd.UserID, cmd.HoldAmount)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected > 0 {
		var balance, frozen float64
		if err := tx.QueryRowContext(ctx, `SELECT balance, COALESCE(frozen_balance, 0) FROM users WHERE id = ?`, cmd.UserID).Scan(&balance, &frozen); err != nil {
			return nil, err
		}
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	exists, err := userExistsForBillingSQLite(ctx, tx, cmd.UserID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, service.ErrBatchImageInsufficientBalance
}

func (r *usageBillingRepository) captureUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 && cmd.ActualAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if cmd.ActualAmount-cmd.HoldAmount > 0.00000001 {
		return nil, service.ErrBatchImageSettlementCostExceedsHold
	}
	if r.sqlite {
		return captureUsageBillingBatchImageBalanceSQLite(ctx, tx, cmd)
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance
				+ CASE WHEN $1 > $2 THEN $1 - $2 ELSE 0 END
				- CASE WHEN $2 > $1 THEN $2 - $1 ELSE 0 END,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.ActualAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

func captureUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return (&usageBillingRepository{}).captureUsageBillingBatchImageBalance(ctx, tx, cmd)
}

func captureUsageBillingBatchImageBalanceSQLite(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE users
		SET balance = balance
				+ CASE WHEN ? > ? THEN ? - ? ELSE 0 END
				- CASE WHEN ? > ? THEN ? - ? ELSE 0 END,
			frozen_balance = COALESCE(frozen_balance, 0) - ?,
			updated_at = ?
		WHERE id = ? AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= ?
	`,
		cmd.HoldAmount, cmd.ActualAmount, cmd.HoldAmount, cmd.ActualAmount,
		cmd.ActualAmount, cmd.HoldAmount, cmd.ActualAmount, cmd.HoldAmount,
		cmd.HoldAmount, nowSQLiteUTC(), cmd.UserID, cmd.HoldAmount,
	)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected > 0 {
		var balance, frozen float64
		if err := tx.QueryRowContext(ctx, `SELECT balance, COALESCE(frozen_balance, 0) FROM users WHERE id = ?`, cmd.UserID).Scan(&balance, &frozen); err != nil {
			return nil, err
		}
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	exists, err := userExistsForBillingSQLite(ctx, tx, cmd.UserID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

func (r *usageBillingRepository) releaseUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	// 释放前校验该 job 确实预留过 hold（hold request id 已被 claim），
	// 防止从未成功冻结的 job 触发"幻影释放"，从其他用户的冻结资金池中凭空生成余额。
	held, heldErr := batchImageHoldClaimExists(ctx, tx, service.BatchImageHoldRequestID(cmd.BatchID), cmd.APIKeyID)
	if heldErr != nil {
		return nil, heldErr
	}
	if !held {
		logger.LegacyPrintf("repository.usage_billing", "[BatchImage] release skipped, hold was never reserved: batch=%s", cmd.BatchID)
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r.sqlite {
		return releaseUsageBillingBatchImageBalanceSQLite(ctx, tx, cmd)
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance + $1,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

func releaseUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return (&usageBillingRepository{}).releaseUsageBillingBatchImageBalance(ctx, tx, cmd)
}

func releaseUsageBillingBatchImageBalanceSQLite(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE users
		SET balance = balance + ?,
			frozen_balance = COALESCE(frozen_balance, 0) - ?,
			updated_at = ?
		WHERE id = ? AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= ?
	`, cmd.HoldAmount, cmd.HoldAmount, nowSQLiteUTC(), cmd.UserID, cmd.HoldAmount)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected > 0 {
		var balance, frozen float64
		if err := tx.QueryRowContext(ctx, `SELECT balance, COALESCE(frozen_balance, 0) FROM users WHERE id = ?`, cmd.UserID).Scan(&balance, &frozen); err != nil {
			return nil, err
		}
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	exists, err := userExistsForBillingSQLite(ctx, tx, cmd.UserID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

// batchImageHoldClaimExists 检查 hold request id 是否已在 dedup（或归档）表中被 claim，
// 即该 batch 的冻结操作确实成功提交过。
func batchImageHoldClaimExists(ctx context.Context, tx *sql.Tx, holdRequestID string, apiKeyID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func userExistsForBilling(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func userExistsForBillingSQLite(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE id = ? AND deleted_at IS NULL
	`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) incrementUsageBillingAPIKeyQuota(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	if r.sqlite {
		return incrementUsageBillingAPIKeyQuotaSQLite(ctx, tx, apiKeyID, amount)
	}
	var exhausted bool
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + $1,
			status = CASE
				WHEN quota > 0
					AND status = $3
					AND quota_used < quota
					AND quota_used + $1 >= quota
				THEN $4
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING quota > 0 AND quota_used >= quota AND quota_used - $1 < quota
	`, amount, apiKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).Scan(&exhausted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return false, err
	}
	return exhausted, nil
}

func incrementUsageBillingAPIKeyQuotaSQLite(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + ?,
			status = CASE
				WHEN quota > 0
					AND status = ?
					AND quota_used < quota
					AND quota_used + ? >= quota
				THEN ?
				ELSE status
			END,
			updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, amount, service.StatusAPIKeyActive, amount, service.StatusAPIKeyQuotaExhausted, nowSQLiteUTC(), apiKeyID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, service.ErrAPIKeyNotFound
	}
	var exhausted bool
	if err := tx.QueryRowContext(ctx, `
		SELECT quota > 0 AND quota_used >= quota AND quota_used - ? < quota
		FROM api_keys
		WHERE id = ?
	`, amount, apiKeyID).Scan(&exhausted); err != nil {
		return false, err
	}
	return exhausted, nil
}

func (r *usageBillingRepository) incrementUsageBillingAPIKeyRateLimit(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64) error {
	if r.sqlite {
		return incrementUsageBillingAPIKeyRateLimitSQLite(ctx, tx, apiKeyID, cost)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW() THEN $1 ELSE usage_5h + $1 END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW() THEN $1 ELSE usage_1d + $1 END,
			usage_7d = CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW() THEN $1 ELSE usage_7d + $1 END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, cost, apiKeyID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

func incrementUsageBillingAPIKeyRateLimitSQLite(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64) error {
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND julianday(window_5h_start, '+5 hours') <= julianday(?) THEN ? ELSE usage_5h + ? END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND julianday(window_1d_start, '+24 hours') <= julianday(?) THEN ? ELSE usage_1d + ? END,
			usage_7d = CASE WHEN window_7d_start IS NOT NULL AND julianday(window_7d_start, '+7 days') <= julianday(?) THEN ? ELSE usage_7d + ? END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR julianday(window_5h_start, '+5 hours') <= julianday(?) THEN ? ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR julianday(window_1d_start, '+24 hours') <= julianday(?) THEN ? ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR julianday(window_7d_start, '+7 days') <= julianday(?) THEN ? ELSE window_7d_start END,
			updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`,
		nowSQLiteUTC(), cost, cost,
		nowSQLiteUTC(), cost, cost,
		nowSQLiteUTC(), cost, cost,
		nowSQLiteUTC(), nowSQLiteUTC(),
		nowSQLiteUTC(), nowSQLiteDayStart(now),
		nowSQLiteUTC(), nowSQLiteDayStart(now),
		nowSQLiteUTC(), apiKeyID,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

func (r *usageBillingRepository) incrementUsageBillingAccountQuota(ctx context.Context, tx *sql.Tx, accountID int64, amount float64) (*service.AccountQuotaState, error) {
	if r.sqlite {
		return incrementUsageBillingAccountQuotaSQLite(ctx, tx, accountID, amount)
	}
	rows, err := tx.QueryContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN `+dailyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN `+dailyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_daily_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_weekly_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
		), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0),
			COALESCE((extra->>'quota_daily_used')::numeric, 0),
			COALESCE((extra->>'quota_daily_limit')::numeric, 0),
			COALESCE((extra->>'quota_weekly_used')::numeric, 0),
			COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`,
		amount, accountID)
	if err != nil {
		return nil, err
	}

	var state service.AccountQuotaState
	if rows.Next() {
		if err := rows.Scan(
			&state.TotalUsed, &state.TotalLimit,
			&state.DailyUsed, &state.DailyLimit,
			&state.WeeklyUsed, &state.WeeklyLimit,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
	} else {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		return nil, service.ErrAccountNotFound
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// 必须在执行下一条 SQL 前显式关闭 rows：pq 驱动在同一连接上
	// 不允许前一条查询的结果集未耗尽时启动新查询，否则会返回
	// "unexpected Parse response" 错误。
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// 任意维度额度在本次递增中从"未超"跨越到"已超"时，必须刷新调度快照，
	// 否则 Redis 中缓存的 Account 仍显示旧的 used 值，后续请求会继续选中本账号，
	// 最终观察到 daily_used / weekly_used 大幅超过配置的 limit。
	// 对于日/周额度，即使本次触发了周期重置（pre=0、post=amount），
	// 判定式 (post-amount) < limit 同样成立，逻辑与总额度保持一致。
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return &state, nil
}

func incrementUsageBillingAccountQuotaSQLite(ctx context.Context, tx *sql.Tx, accountID int64, amount float64) (*service.AccountQuotaState, error) {
	var rawExtra string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(extra, '{}')
		FROM accounts
		WHERE id = ? AND deleted_at IS NULL
	`, accountID).Scan(&rawExtra); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrAccountNotFound
		}
		return nil, err
	}

	extra := map[string]any{}
	if strings.TrimSpace(rawExtra) != "" {
		if err := json.Unmarshal([]byte(rawExtra), &extra); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	nowText := now.UTC().Format(time.RFC3339)

	totalUsed := quotaExtraFloat(extra, "quota_used") + amount
	extra["quota_used"] = totalUsed

	dailyLimit := quotaExtraFloat(extra, "quota_daily_limit")
	dailyUsed := quotaExtraFloat(extra, "quota_daily_used")
	if dailyLimit > 0 {
		if quotaWindowExpired(extra, "quota_daily_reset_mode", "quota_daily_reset_at", "quota_daily_start", 24*time.Hour, now) {
			dailyUsed = amount
			extra["quota_daily_start"] = nowText
			if quotaMode(extra, "quota_daily_reset_mode") == "fixed" {
				extra["quota_daily_reset_at"] = nextQuotaResetAt(extra, "quota_daily", now).Format(time.RFC3339)
			}
		} else {
			dailyUsed += amount
		}
		extra["quota_daily_used"] = dailyUsed
	}

	weeklyLimit := quotaExtraFloat(extra, "quota_weekly_limit")
	weeklyUsed := quotaExtraFloat(extra, "quota_weekly_used")
	if weeklyLimit > 0 {
		if quotaWindowExpired(extra, "quota_weekly_reset_mode", "quota_weekly_reset_at", "quota_weekly_start", 7*24*time.Hour, now) {
			weeklyUsed = amount
			extra["quota_weekly_start"] = nowText
			if quotaMode(extra, "quota_weekly_reset_mode") == "fixed" {
				extra["quota_weekly_reset_at"] = nextQuotaResetAt(extra, "quota_weekly", now).Format(time.RFC3339)
			}
		} else {
			weeklyUsed += amount
		}
		extra["quota_weekly_used"] = weeklyUsed
	}

	encoded, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE accounts
		SET extra = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, string(encoded), nowText, accountID)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, service.ErrAccountNotFound
	}

	state := &service.AccountQuotaState{
		TotalUsed:   totalUsed,
		TotalLimit:  quotaExtraFloat(extra, "quota_limit"),
		DailyUsed:   dailyUsed,
		DailyLimit:  dailyLimit,
		WeeklyUsed:  weeklyUsed,
		WeeklyLimit: weeklyLimit,
	}
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return state, nil
}

func quotaExtraFloat(extra map[string]any, key string) float64 {
	switch v := extra[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &f); err == nil {
			return f
		}
	}
	return 0
}

func quotaMode(extra map[string]any, key string) string {
	mode, _ := extra[key].(string)
	if mode == "" {
		return "rolling"
	}
	return mode
}

func quotaWindowExpired(extra map[string]any, modeKey, resetAtKey, startKey string, window time.Duration, now time.Time) bool {
	if quotaMode(extra, modeKey) == "fixed" {
		resetAt, err := time.Parse(time.RFC3339, strings.TrimSpace(quotaExtraString(extra, resetAtKey)))
		if err != nil {
			return true
		}
		return !now.Before(resetAt)
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(quotaExtraString(extra, startKey)))
	if err != nil {
		return true
	}
	return !now.Before(start.Add(window))
}

func quotaExtraString(extra map[string]any, key string) string {
	v, _ := extra[key].(string)
	return v
}

func nextQuotaResetAt(extra map[string]any, prefix string, now time.Time) time.Time {
	tzName := quotaExtraString(extra, "quota_reset_timezone")
	if tzName == "" {
		tzName = "UTC"
	}
	tz, err := time.LoadLocation(tzName)
	if err != nil {
		tz = time.UTC
	}
	hour := int(quotaExtraFloat(extra, prefix+"_reset_hour"))
	if hour < 0 || hour > 23 {
		hour = 0
	}
	local := now.In(tz)
	reset := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, tz)
	if !now.Before(reset) {
		reset = reset.AddDate(0, 0, 1)
	}
	if strings.HasSuffix(prefix, "weekly") {
		day := int(quotaExtraFloat(extra, "quota_weekly_reset_day"))
		if day < 0 || day > 6 {
			day = 1
		}
		daysForward := (day - int(reset.Weekday()) + 7) % 7
		if daysForward == 0 && !now.Before(reset) {
			daysForward = 7
		}
		reset = reset.AddDate(0, 0, daysForward)
	}
	return reset.UTC()
}
