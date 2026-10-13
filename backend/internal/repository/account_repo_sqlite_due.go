package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type sqliteDueUsageQuery struct {
	eligibleSQL      string
	snapshotKey      string
	autoRefreshKey   string
	sessionKey       string
	minFetchInterval time.Duration
}

// listDueSQLiteUsageAccounts is the SQLite counterpart of the activity-driven
// Ollama/OpenCode due queries. It intentionally keeps the same due rules as
// service.ollamaCloudUsageAutoRefreshDueAt and
// service.openCodeGoUsageAutoRefreshDueAt, but evaluates them numerically so
// SQLite does not need PostgreSQL jsonpath or interval syntax.
func (r *accountRepository) listDueSQLiteUsageAccounts(
	ctx context.Context,
	now time.Time,
	debounce, maxWait time.Duration,
	limit int,
	query sqliteDueUsageQuery,
) ([]service.Account, error) {
	if limit <= 0 {
		return []service.Account{}, nil
	}
	if r == nil || r.sql == nil {
		return nil, fmt.Errorf("account repository SQL executor not configured")
	}
	if query.eligibleSQL == "" || query.snapshotKey == "" || query.autoRefreshKey == "" {
		return nil, fmt.Errorf("sqlite due usage query is missing required configuration")
	}
	if debounce <= 0 {
		debounce = time.Minute
	}
	if maxWait <= 0 {
		maxWait = time.Hour
	}
	if query.minFetchInterval <= 0 {
		query.minFetchInterval = time.Minute
	}

	autoRefreshPath := "$." + query.autoRefreshKey
	sessionFilter := ""
	if query.sessionKey != "" {
		sessionFilter = " AND jsonb_typeof(extra -> '" + query.sessionKey + "') = 'string'"
	}

	rows, err := r.sql.QueryContext(ctx, `
		WITH eligible AS (
			SELECT id,
				credentials ->> 'api_key' AS api_key,
				last_used_at,
				extra -> '`+query.snapshotKey+`' AS snapshot
			FROM accounts
			WHERE deleted_at IS NULL
				AND status = 'active'
				AND `+query.eligibleSQL+`
				`+sessionFilter+`
				AND json_type(extra, '`+autoRefreshPath+`') = 'true'
		), group_activity AS (
			SELECT credentials ->> 'api_key' AS api_key,
				MAX(last_used_at) AS group_last_used_at
			FROM accounts
			WHERE deleted_at IS NULL
				AND `+query.eligibleSQL+`
				AND jsonb_typeof(credentials -> 'api_key') = 'string'
			GROUP BY credentials ->> 'api_key'
		), joined AS (
			SELECT e.id, e.api_key, e.snapshot, g.group_last_used_at,
				json_extract(e.snapshot, '$.status') AS status,
				json_extract(e.snapshot, '$.fetched_at') AS fetched_at,
				json_extract(e.snapshot, '$.last_attempt_at') AS last_attempt_at,
				json_extract(e.snapshot, '$.next_refresh_at') AS next_refresh_at
			FROM eligible e
			JOIN group_activity g ON g.api_key = e.api_key
		), parsed AS (
			SELECT id, api_key, snapshot, group_last_used_at, status,
				pg_rfc3339_micros(fetched_at) AS parsed_fetched_at,
				pg_rfc3339_micros(last_attempt_at) AS parsed_last_attempt_at,
				pg_rfc3339_micros(next_refresh_at) AS parsed_next_refresh_at,
				CASE WHEN group_last_used_at IS NOT NULL
					THEN CAST(unixepoch(group_last_used_at) * 1000000 AS INTEGER) ELSE NULL END AS group_last_used_micros
			FROM joined
		), activity AS (
			SELECT *,
				CASE
					WHEN status = 'ok'
						AND parsed_fetched_at IS NOT NULL
						AND group_last_used_micros IS NOT NULL
						AND group_last_used_micros > parsed_fetched_at
					THEN least(
						group_last_used_micros + ?,
						parsed_fetched_at + ?
					)
					WHEN status IN ('failed', 'unauthorized')
						AND parsed_last_attempt_at IS NOT NULL
						AND group_last_used_micros IS NOT NULL
						AND group_last_used_micros > parsed_last_attempt_at
					THEN least(
						group_last_used_micros + ?,
						parsed_last_attempt_at + ?
					)
					ELSE NULL
				END AS activity_due_at
			FROM parsed
		), timed AS (
			SELECT *,
				CASE
					WHEN status = 'ok' AND activity_due_at IS NOT NULL
					THEN greatest(activity_due_at, parsed_fetched_at + ?)
					WHEN status IN ('failed', 'unauthorized') AND activity_due_at IS NOT NULL
					THEN greatest(activity_due_at, COALESCE(parsed_next_refresh_at, activity_due_at))
					ELSE NULL
				END AS final_due_at
			FROM activity
		), candidates AS (
			SELECT *,
				CASE
					WHEN snapshot IS NULL OR json_type(snapshot) = 'null' OR status IS NULL
						OR status NOT IN ('ok', 'failed', 'unauthorized') THEN 0
					WHEN status = 'ok' AND parsed_fetched_at IS NULL THEN 0
					WHEN status IN ('failed', 'unauthorized') AND parsed_last_attempt_at IS NULL THEN 0
					WHEN final_due_at IS NOT NULL AND ? >= final_due_at THEN 1
					ELSE NULL
				END AS due_class,
				final_due_at AS due_at
			FROM timed
		), ranked AS (
			SELECT id, api_key, group_last_used_at, due_class, due_at,
				row_number() OVER (
					PARTITION BY api_key
					ORDER BY due_class, due_at, id
				) AS group_rank
			FROM candidates
			WHERE due_class IS NOT NULL
		)
		SELECT id, group_last_used_at
		FROM ranked
		WHERE group_rank = 1
		ORDER BY due_class, due_at, id
		LIMIT ?
	`,
		debounce.Microseconds(),
		maxWait.Microseconds(),
		debounce.Microseconds(),
		maxWait.Microseconds(),
		query.minFetchInterval.Microseconds(),
		now.UTC().UnixMicro(),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type dueRow struct {
		id            int64
		groupLastUsed *time.Time
	}
	rowsOut := make([]dueRow, 0, limit)
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var row dueRow
		var rawGroupLastUsed any
		if err := rows.Scan(&row.id, &rawGroupLastUsed); err != nil {
			return nil, err
		}
		if rawGroupLastUsed != nil {
			parsed, ok := sqliteTimeValue(rawGroupLastUsed)
			if !ok {
				return nil, fmt.Errorf("parse sqlite group_last_used_at %T", rawGroupLastUsed)
			}
			row.groupLastUsed = &parsed
		}
		rowsOut = append(rowsOut, row)
		ids = append(ids, row.id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	accounts, err := r.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*service.Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			byID[account.ID] = account
		}
	}
	result := make([]service.Account, 0, len(rowsOut))
	for _, row := range rowsOut {
		account := byID[row.id]
		if account == nil {
			continue
		}
		if row.groupLastUsed != nil {
			ts := row.groupLastUsed.UTC()
			account.LastUsedAt = &ts
		} else {
			account.LastUsedAt = nil
		}
		result = append(result, *account)
	}
	return result, nil
}
