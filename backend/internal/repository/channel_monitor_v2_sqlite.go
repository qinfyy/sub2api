package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbtime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// Callers only pass []string or []int64, both of which always marshal to JSON.
func channelMonitorV2ArrayCondition(column string, index int, values any, sqlite bool) (string, any) {
	if !sqlite {
		return fmt.Sprintf("%s = ANY($%d)", column, index), pq.Array(values)
	}
	encoded, _ := json.Marshal(values)
	return fmt.Sprintf("%s IN (SELECT value FROM json_each($%d))", column, index), string(encoded)
}

const channelMonitorV2SQLiteConfigColumns = `version,enabled,refresh_interval_seconds,platforms,group_ids,
 COALESCE(ignored_error_categories,'[]'),COALESCE(health_thresholds,'{}'),updated_at,updated_by`

func scanChannelMonitorV2SQLiteConfig(row *sql.Row) (*service.ChannelMonitorV2Config, error) {
	var cfg service.ChannelMonitorV2Config
	var platforms, groups, ignored, thresholds []byte
	if err := row.Scan(&cfg.Version, &cfg.Enabled, &cfg.RefreshIntervalSeconds, &platforms, &groups,
		&ignored, &thresholds, dbtime.Dest(&cfg.UpdatedAt), &cfg.UpdatedBy); err != nil {
		return nil, err
	}
	cfg.HealthThresholds = service.DefaultChannelMonitorV2HealthThresholds()
	for _, pair := range []struct {
		raw    []byte
		target any
	}{
		{platforms, &cfg.Platforms}, {groups, &cfg.GroupIDs}, {ignored, &cfg.IgnoredErrorCategories}, {thresholds, &cfg.HealthThresholds},
	} {
		if err := json.Unmarshal(pair.raw, pair.target); err != nil {
			return nil, fmt.Errorf("decode channel monitor v2 config: %w", err)
		}
	}
	if cfg.IgnoredErrorCategories == nil {
		cfg.IgnoredErrorCategories = []string{}
	}
	cfg.HealthThresholds = service.NormalizeChannelMonitorV2HealthThresholds(cfg.HealthThresholds)
	return &cfg, nil
}

func (r *channelMonitorV2Repository) getConfigSQLite(ctx context.Context) (*service.ChannelMonitorV2Config, error) {
	return scanChannelMonitorV2SQLiteConfig(r.db.QueryRowContext(ctx, `SELECT `+channelMonitorV2SQLiteConfigColumns+` FROM channel_monitor_v2_config WHERE id=1`))
}

func (r *channelMonitorV2Repository) updateConfigSQLite(ctx context.Context, cfg service.ChannelMonitorV2Config, expected int) (*service.ChannelMonitorV2Config, error) {
	if cfg.GroupIDs == nil {
		cfg.GroupIDs = []int64{}
	}
	if cfg.IgnoredErrorCategories == nil {
		cfg.IgnoredErrorCategories = []string{}
	}
	cfg.HealthThresholds = service.NormalizeChannelMonitorV2HealthThresholds(cfg.HealthThresholds)
	args := []any{cfg.Enabled, cfg.RefreshIntervalSeconds}
	for _, value := range []any{cfg.Platforms, cfg.GroupIDs, cfg.IgnoredErrorCategories, cfg.HealthThresholds} {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		args = append(args, string(encoded))
	}
	args = append(args, cfg.UpdatedBy, expected)
	updated, err := scanChannelMonitorV2SQLiteConfig(r.db.QueryRowContext(ctx, `UPDATE channel_monitor_v2_config
	 SET version=version+1,enabled=$1,refresh_interval_seconds=$2,platforms=$3,group_ids=$4,
	 ignored_error_categories=$5,health_thresholds=$6,updated_by=$7,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
	 WHERE id=1 AND version=$8 RETURNING `+channelMonitorV2SQLiteConfigColumns, args...))
	if err == sql.ErrNoRows {
		return nil, service.ErrChannelMonitorV2ConfigConflict
	}
	return updated, err
}
