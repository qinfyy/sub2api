package repository

import "strings"

// Keep in prepareUsageLogInsert order. SQLite uses VALUES directly: it has no
// data-modifying CTEs and cannot accept the PG per-value casts.
var sqliteUsageLogInsertColumns = []string{
	"user_id", "api_key_id", "account_id", "request_id", "model", "requested_model",
	"upstream_model", "upstream_response_model", "upstream_model_mismatch", "group_id", "subscription_id",
	"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
	"cache_creation_5m_tokens", "cache_creation_1h_tokens", "image_output_tokens", "image_output_cost",
	"image_input_tokens", "image_input_cost", "input_cost", "output_cost", "cache_creation_cost", "cache_read_cost",
	"total_cost", "actual_cost", "rate_multiplier", "account_rate_multiplier", "billing_type", "request_type",
	"stream", "openai_ws_mode", "duration_ms", "first_token_ms", "user_agent", "ip_address", "image_count",
	"image_size", "image_input_size", "image_output_size", "image_size_source", "image_size_breakdown",
	"video_count", "video_resolution", "video_duration_seconds", "service_tier", "reasoning_effort",
	"requested_reasoning_effort", "inbound_endpoint", "upstream_endpoint", "cache_ttl_overridden",
	"long_context_billing_applied", "channel_id", "model_mapping_chain", "billing_tier", "billing_mode",
	"account_stats_cost", "upstream_request_id", "session_id", "native_compaction_v2", "created_at",
}

func buildSQLiteUsageLogInsert(prepared []usageLogInsertPrepared) (string, []any) {
	var query strings.Builder
	query.WriteString("INSERT INTO usage_logs (")
	query.WriteString(strings.Join(sqliteUsageLogInsertColumns, ","))
	query.WriteString(") VALUES ")
	args := make([]any, 0, len(prepared)*len(sqliteUsageLogInsertColumns))
	row := "(" + strings.TrimSuffix(strings.Repeat("?,", len(sqliteUsageLogInsertColumns)), ",") + ")"
	for i, entry := range prepared {
		if i > 0 {
			query.WriteByte(',')
		}
		query.WriteString(row)
		start := len(args)
		args = append(args, entry.args...)
		// The SQLite baseline has NOT NULL request_id. Blank IDs are excluded
		// from the unique index, preserving PG's non-deduplicated NULL behavior.
		if entry.requestID == "" {
			args[start+3] = ""
		}
	}
	query.WriteString(" ON CONFLICT (request_id, api_key_id) WHERE request_id <> '' DO NOTHING")
	return query.String(), args
}
