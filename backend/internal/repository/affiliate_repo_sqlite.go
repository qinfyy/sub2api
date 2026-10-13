package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

func thawFrozenQuotaSQLite(ctx context.Context, client *dbent.Client, userID int64) (float64, error) {
	// The write also takes SQLite's transaction lock before a balance transfer reads its quota.
	rows, err := client.QueryContext(ctx, `UPDATE user_affiliate_ledger SET frozen_until=NULL,updated_at=NOW()
	 WHERE user_id=$1 AND frozen_until IS NOT NULL AND pg_rfc3339_micros(frozen_until)<=pg_rfc3339_micros(NOW()) RETURNING amount`, userID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var thawed float64
	for rows.Next() {
		var amount float64
		if err = rows.Scan(&amount); err != nil {
			return 0, err
		}
		thawed += amount
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	if thawed > 0 {
		_, err = client.ExecContext(ctx, `UPDATE user_affiliates SET aff_quota=aff_quota+$1,
		 aff_frozen_quota=max(aff_frozen_quota-$1,0),updated_at=NOW() WHERE user_id=$2`, thawed, userID)
	}
	return thawed, err
}
