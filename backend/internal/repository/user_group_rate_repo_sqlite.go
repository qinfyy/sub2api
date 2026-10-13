package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (r *userGroupRateRepository) sqliteRateTx(ctx context.Context, change func(*sql.Tx) error) error {
	db, ok := r.sql.(*sql.DB)
	if !ok { return fmt.Errorf("SQLite group rates require a database connection") }
	tx, err := db.BeginTx(ctx, nil)
	if err != nil { return err }
	defer tx.Rollback()
	if err := change(tx); err != nil { return err }
	return tx.Commit()
}

func (r *userGroupRateRepository) syncSQLiteUserRates(ctx context.Context, userID int64, rates map[int64]*float64) error {
	return r.sqliteRateTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC()
		if len(rates) == 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE user_group_rate_multipliers SET rate_multiplier=NULL, updated_at=? WHERE user_id=?`, now, userID); err != nil { return err }
		}
		for groupID, rate := range rates {
			if rate == nil {
				if _, err := tx.ExecContext(ctx, `UPDATE user_group_rate_multipliers SET rate_multiplier=NULL, updated_at=? WHERE user_id=? AND group_id=?`, now, userID, groupID); err != nil { return err }
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO user_group_rate_multipliers(user_id,group_id,rate_multiplier,created_at,updated_at)
			VALUES(?,?,?,?,?) ON CONFLICT(user_id,group_id) DO UPDATE SET rate_multiplier=excluded.rate_multiplier,updated_at=excluded.updated_at`, userID, groupID, *rate, now, now); err != nil { return err }
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM user_group_rate_multipliers WHERE user_id=? AND rate_multiplier IS NULL AND rpm_override IS NULL`, userID)
		return err
	})
}

func (r *userGroupRateRepository) syncSQLiteGroupValues(ctx context.Context, groupID int64, column string, values map[int64]any) error {
	if column != "rate_multiplier" && column != "rpm_override" { return fmt.Errorf("invalid group rate column") }
	return r.sqliteRateTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `UPDATE user_group_rate_multipliers SET `+column+`=NULL,updated_at=? WHERE group_id=?`, now, groupID); err != nil { return err }
		for userID, value := range values {
			if value == nil { continue }
			if _, err := tx.ExecContext(ctx, `INSERT INTO user_group_rate_multipliers(user_id,group_id,`+column+`,created_at,updated_at)
			VALUES(?,?,?,?,?) ON CONFLICT(user_id,group_id) DO UPDATE SET `+column+`=excluded.`+column+`,updated_at=excluded.updated_at`, userID, groupID, value, now, now); err != nil { return err }
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM user_group_rate_multipliers WHERE group_id=? AND rate_multiplier IS NULL AND rpm_override IS NULL`, groupID)
		return err
	})
}
