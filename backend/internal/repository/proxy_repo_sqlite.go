package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbtime"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const proxySQLiteInvalidateSnapshotsSQL = `UPDATE accounts
 SET extra=json_remove(COALESCE(extra,'{}'),'$.upstream_billing_probe','$.ollama_cloud_usage_snapshot','$.opencode_go_usage_snapshot'),updated_at=NOW()
 WHERE proxy_id=$1 AND type='apikey' AND deleted_at IS NULL AND (
 json_type(extra,'$.upstream_billing_probe') IS NOT NULL AND json_type(extra,'$.upstream_billing_probe')<>'null'
 OR (platform IN (` + ollamaCloudUsagePlatformsSQL + `) AND json_type(extra,'$.ollama_cloud_usage_snapshot') IS NOT NULL AND json_type(extra,'$.ollama_cloud_usage_snapshot')<>'null')
 OR ((platform='opencode_go' OR platform IN (` + opencodeGoUsageMountPlatformsSQL + `)) AND json_type(extra,'$.opencode_go_usage_snapshot') IS NOT NULL AND json_type(extra,'$.opencode_go_usage_snapshot')<>'null')) RETURNING id`

func (r *proxyRepository) sweepOneExpiredProxySQLite(ctx context.Context, exec sqlExecutor, snapshot service.Proxy, now time.Time, target *int64, change bool) ([]int64, error) {
	result, err := exec.ExecContext(ctx, `UPDATE proxies SET status=$1,updated_at=NOW()
	 WHERE id=$2 AND deleted_at IS NULL AND status=$3 AND pg_rfc3339_micros(expires_at)<=pg_rfc3339_micros($4)
	 AND pg_rfc3339_micros(expires_at)=pg_rfc3339_micros($5) AND fallback_mode=$6 AND backup_proxy_id IS $7`,
		service.StatusExpired, snapshot.ID, service.StatusActive, now, snapshot.ExpiresAt, snapshot.FallbackMode, snapshot.BackupProxyID)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return nil, err
	}
	if !change {
		ids, err := invalidateProxyProbeSnapshots(ctx, exec, snapshot.ID, true)
		if err != nil {
			return nil, err
		}
		return nil, enqueueProxyProbeAccountChanges(ctx, exec, ids)
	}
	rows, err := exec.QueryContext(ctx, `UPDATE accounts SET proxy_id=$2,proxy_fallback_origin_id=COALESCE(proxy_fallback_origin_id,$1),
	 extra=CASE WHEN type='apikey' THEN json_remove(extra,'$.upstream_billing_probe') ELSE extra END,updated_at=NOW()
	 WHERE proxy_id=$1 AND deleted_at IS NULL RETURNING id`, snapshot.ID, target)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *proxyRepository) countExpiringSoonSQLite(ctx context.Context, now time.Time) (int64, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT expires_at,expiry_warn_days FROM proxies WHERE deleted_at IS NULL AND status=$1 AND expires_at IS NOT NULL`, service.StatusActive)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var count int64
	for rows.Next() {
		var expires time.Time
		var days int
		if err = rows.Scan(dbtime.Dest(&expires), &days); err != nil {
			return 0, err
		}
		if expires.After(now) && !expires.After(now.AddDate(0, 0, days)) {
			count++
		}
	}
	return count, rows.Err()
}
