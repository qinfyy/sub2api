package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// --- 模型定价 ---

func (r *channelRepository) ListModelPricing(ctx context.Context, channelID int64) ([]service.ChannelModelPricing, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, channel_id, platform, models, billing_mode, input_price, output_price, cache_write_price, cache_write_1h_price, cache_read_price, fast_multiplier, flex_multiplier, reasoning_effort_multipliers, image_input_price, image_output_price, per_request_price, time_pricing, created_at, updated_at
		 FROM channel_model_pricing WHERE channel_id = $1 ORDER BY id`, channelID,
	)
	if err != nil {
		return nil, fmt.Errorf("list model pricing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result, pricingIDs, err := scanModelPricingRows(rows, r.sqlite)
	if err != nil {
		return nil, err
	}

	if len(pricingIDs) > 0 {
		intervalMap, err := r.batchLoadIntervals(ctx, pricingIDs)
		if err != nil {
			return nil, err
		}
		for i := range result {
			result[i].Intervals = intervalMap[result[i].ID]
		}
	}

	return result, nil
}

func (r *channelRepository) CreateModelPricing(ctx context.Context, pricing *service.ChannelModelPricing) error {
	return createModelPricingExec(ctx, r.db, pricing)
}

func (r *channelRepository) UpdateModelPricing(ctx context.Context, pricing *service.ChannelModelPricing) error {
	modelsJSON, err := json.Marshal(pricing.Models)
	if err != nil {
		return fmt.Errorf("marshal models: %w", err)
	}
	reasoningEffortMultipliersJSON, err := marshalReasoningEffortMultipliers(pricing.ReasoningEffortMultipliers)
	if err != nil {
		return err
	}
	timePricingJSON, err := marshalChannelTimePricing(pricing.TimePricing)
	if err != nil {
		return err
	}
	billingMode := pricing.BillingMode
	if billingMode == "" {
		billingMode = service.BillingModeToken
	}
	result, err := r.db.ExecContext(ctx,
		`UPDATE channel_model_pricing
		 SET models = $1, billing_mode = $2, input_price = $3, output_price = $4, cache_write_price = $5, cache_write_1h_price = $6, cache_read_price = $7, fast_multiplier = $8, flex_multiplier = $9, reasoning_effort_multipliers = $10, image_input_price = $11, image_output_price = $12, per_request_price = $13, time_pricing = $14, platform = $15, updated_at = NOW()
		 WHERE id = $16`,
		modelsJSON, billingMode, pricing.InputPrice, pricing.OutputPrice, pricing.CacheWritePrice, pricing.CacheWrite1hPrice, pricing.CacheReadPrice,
		pricing.FastMultiplier, pricing.FlexMultiplier, reasoningEffortMultipliersJSON, pricing.ImageInputPrice, pricing.ImageOutputPrice, pricing.PerRequestPrice,
		timePricingJSON, pricing.Platform, pricing.ID,
	)
	if err != nil {
		return fmt.Errorf("update model pricing: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("pricing entry not found: %d", pricing.ID)
	}
	return nil
}

func (r *channelRepository) DeleteModelPricing(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM channel_model_pricing WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete model pricing: %w", err)
	}
	return nil
}

func (r *channelRepository) ReplaceModelPricing(ctx context.Context, channelID int64, pricingList []service.ChannelModelPricing) error {
	return r.runInTx(ctx, func(tx *sql.Tx) error {
		return replaceModelPricingTx(ctx, tx, channelID, pricingList)
	})
}

// --- 批量加载辅助方法 ---

// batchLoadModelPricing 批量加载多个渠道的模型定价（含区间）
func (r *channelRepository) batchLoadModelPricing(ctx context.Context, channelIDs []int64) (map[int64][]service.ChannelModelPricing, error) {
	query := `SELECT id, channel_id, platform, models, billing_mode, input_price, output_price, cache_write_price, cache_write_1h_price, cache_read_price, fast_multiplier, flex_multiplier, reasoning_effort_multipliers, image_input_price, image_output_price, per_request_price, time_pricing, created_at, updated_at
		 FROM channel_model_pricing WHERE channel_id = ANY($1) ORDER BY channel_id, id`
	args := []any{pq.Array(channelIDs)}
	if r.sqlite {
		var bindErr error
		query, args, bindErr = bindArrayArgsForSQLite(query, []any{channelIDs})
		if bindErr != nil {
			return nil, fmt.Errorf("batch load model pricing: %w", bindErr)
		}
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("batch load model pricing: %w", err)
	}
	defer func() { _ = rows.Close() }()

	allPricing, allPricingIDs, err := scanModelPricingRows(rows, r.sqlite)
	if err != nil {
		return nil, err
	}

	// 按 channelID 分组
	pricingMap := make(map[int64][]service.ChannelModelPricing, len(channelIDs))
	for _, p := range allPricing {
		pricingMap[p.ChannelID] = append(pricingMap[p.ChannelID], p)
	}

	// 批量加载所有区间
	if len(allPricingIDs) > 0 {
		intervalMap, err := r.batchLoadIntervals(ctx, allPricingIDs)
		if err != nil {
			return nil, err
		}
		for chID := range pricingMap {
			for i := range pricingMap[chID] {
				pricingMap[chID][i].Intervals = intervalMap[pricingMap[chID][i].ID]
			}
		}
	}

	return pricingMap, nil
}

// batchLoadIntervals 批量加载多个定价条目的区间
func (r *channelRepository) batchLoadIntervals(ctx context.Context, pricingIDs []int64) (map[int64][]service.PricingInterval, error) {
	query := `SELECT id, pricing_id, min_tokens, max_tokens, tier_label,
		        input_price, output_price, cache_write_price, cache_write_1h_price, cache_read_price,
		        input_multiplier, output_multiplier, cache_write_multiplier, cache_read_multiplier,
		        per_request_price, sort_order, created_at, updated_at
		 FROM channel_pricing_intervals
		 WHERE pricing_id = ANY($1) ORDER BY pricing_id, sort_order, id`
	args := []any{pq.Array(pricingIDs)}
	if r.sqlite {
		var bindErr error
		query, args, bindErr = bindArrayArgsForSQLite(query, []any{pricingIDs})
		if bindErr != nil {
			return nil, fmt.Errorf("batch load intervals: %w", bindErr)
		}
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("batch load intervals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	intervalMap := make(map[int64][]service.PricingInterval, len(pricingIDs))
	for rows.Next() {
		var iv service.PricingInterval
		if err := rows.Scan(
			&iv.ID, &iv.PricingID, &iv.MinTokens, &iv.MaxTokens, &iv.TierLabel,
			&iv.InputPrice, &iv.OutputPrice, &iv.CacheWritePrice, &iv.CacheWrite1hPrice, &iv.CacheReadPrice,
			&iv.InputMultiplier, &iv.OutputMultiplier, &iv.CacheWriteMultiplier, &iv.CacheReadMultiplier,
			&iv.PerRequestPrice, &iv.SortOrder, timeScanDest(r.sqlite, &iv.CreatedAt), timeScanDest(r.sqlite, &iv.UpdatedAt),
		); err != nil {
			return nil, fmt.Errorf("scan interval: %w", err)
		}
		intervalMap[iv.PricingID] = append(intervalMap[iv.PricingID], iv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate intervals: %w", err)
	}
	return intervalMap, nil
}

// --- 共享 scan 辅助 ---

// scanModelPricingRows 扫描 model pricing 行，返回结果列表和 ID 列表
func scanModelPricingRows(rows *sql.Rows, sqlite bool) ([]service.ChannelModelPricing, []int64, error) {
	var result []service.ChannelModelPricing
	var pricingIDs []int64
	for rows.Next() {
		var p service.ChannelModelPricing
		var modelsJSON []byte
		var timePricingJSON []byte
		var reasoningEffortMultipliersJSON []byte
		if err := rows.Scan(
			&p.ID, &p.ChannelID, &p.Platform, &modelsJSON, &p.BillingMode,
			&p.InputPrice, &p.OutputPrice, &p.CacheWritePrice, &p.CacheWrite1hPrice, &p.CacheReadPrice,
			&p.FastMultiplier, &p.FlexMultiplier, &reasoningEffortMultipliersJSON,
			&p.ImageInputPrice, &p.ImageOutputPrice, &p.PerRequestPrice, &timePricingJSON, timeScanDest(sqlite, &p.CreatedAt), timeScanDest(sqlite, &p.UpdatedAt),
		); err != nil {
			return nil, nil, fmt.Errorf("scan model pricing: %w", err)
		}
		if err := json.Unmarshal(modelsJSON, &p.Models); err != nil {
			p.Models = []string{}
		}
		multipliers, err := unmarshalReasoningEffortMultipliers(reasoningEffortMultipliersJSON)
		if err != nil {
			return nil, nil, err
		}
		p.ReasoningEffortMultipliers = multipliers
		timePricing, err := unmarshalChannelTimePricing(timePricingJSON)
		if err != nil {
			return nil, nil, err
		}
		p.TimePricing = timePricing
		pricingIDs = append(pricingIDs, p.ID)
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate model pricing: %w", err)
	}
	return result, pricingIDs, nil
}

// --- 事务内辅助方法 ---

// dbExec 是 *sql.DB 和 *sql.Tx 共享的最小 SQL 执行接口
type dbExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func sqliteNow() string {
	return "CURRENT_TIMESTAMP"
}

func sqliteJSONSlice(value []int64) string {
	if len(value) == 0 {
		return "[]"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func defaultFloatPtr(value *float64, fallback float64) *float64 {
	if value != nil {
		return value
	}
	result := fallback
	return &result
}

func decodeSQLiteInt64JSON(raw []byte) []int64 {
	if len(raw) == 0 {
		return nil
	}
	var out []int64
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func setGroupIDsTx(ctx context.Context, exec dbExec, channelID int64, groupIDs []int64, sqlite ...bool) error {
	if _, err := exec.ExecContext(ctx, `DELETE FROM channel_groups WHERE channel_id = $1`, channelID); err != nil {
		return fmt.Errorf("delete old group associations: %w", err)
	}
	if len(groupIDs) == 0 {
		return nil
	}
	if len(sqlite) > 0 && sqlite[0] {
		return insertSQLiteGroupIDs(ctx, exec, channelID, groupIDs)
	}
	_, err := exec.ExecContext(ctx,
		`INSERT INTO channel_groups (channel_id, group_id)
		 SELECT $1, unnest($2::bigint[])`,
		channelID, pq.Array(groupIDs),
	)
	if err != nil {
		return fmt.Errorf("insert group associations: %w", err)
	}
	return nil
}

func insertSQLiteGroupIDs(ctx context.Context, exec dbExec, channelID int64, groupIDs []int64) error {
	values := make([]string, 0, len(groupIDs))
	args := make([]any, 0, len(groupIDs)+1)
	args = append(args, channelID)
	for _, groupID := range groupIDs {
		args = append(args, groupID)
		values = append(values, "(?, ?)")
	}
	query := "INSERT INTO channel_groups (channel_id, group_id) VALUES " + strings.Join(values, ", ")
	if _, err := exec.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert group associations: %w", err)
	}
	return nil
}

func createModelPricingExec(ctx context.Context, exec dbExec, pricing *service.ChannelModelPricing, sqlite ...bool) error {
	modelsJSON, err := json.Marshal(pricing.Models)
	if err != nil {
		return fmt.Errorf("marshal models: %w", err)
	}
	reasoningEffortMultipliersJSON, err := marshalReasoningEffortMultipliers(pricing.ReasoningEffortMultipliers)
	if err != nil {
		return err
	}
	timePricingJSON, err := marshalChannelTimePricing(pricing.TimePricing)
	if err != nil {
		return err
	}
	billingMode := pricing.BillingMode
	if billingMode == "" {
		billingMode = service.BillingModeToken
	}
	platform := pricing.Platform
	if platform == "" {
		platform = "anthropic"
	}
	fastMultiplier := pricing.FastMultiplier
	if fastMultiplier == nil {
		value := 1.0
		fastMultiplier = &value
	}
	flexMultiplier := pricing.FlexMultiplier
	if flexMultiplier == nil {
		value := 1.0
		flexMultiplier = &value
	}
	insertSQL := `INSERT INTO channel_model_pricing (channel_id, platform, models, billing_mode, input_price, output_price, cache_write_price, cache_write_1h_price, cache_read_price, fast_multiplier, flex_multiplier, reasoning_effort_multipliers, image_input_price, image_output_price, per_request_price, time_pricing)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING id, created_at, updated_at`
	if len(sqlite) > 0 && sqlite[0] {
		isSQLite := true
		if timePricingJSON == nil {
			timePricingJSON = "{}"
		}
		result, execErr := exec.ExecContext(ctx, strings.Replace(insertSQL, " RETURNING id, created_at, updated_at", "", 1),
			pricing.ChannelID, platform, modelsJSON, billingMode,
			pricing.InputPrice, pricing.OutputPrice, pricing.CacheWritePrice, pricing.CacheWrite1hPrice, pricing.CacheReadPrice,
			fastMultiplier, flexMultiplier, reasoningEffortMultipliersJSON, pricing.ImageInputPrice, pricing.ImageOutputPrice,
			pricing.PerRequestPrice, timePricingJSON,
		)
		if execErr != nil {
			return fmt.Errorf("insert model pricing: %w", execErr)
		}
		pricing.ID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("insert model pricing id: %w", err)
		}
		if err := exec.QueryRowContext(ctx,
			`SELECT created_at, updated_at FROM channel_model_pricing WHERE id = $1`,
			pricing.ID,
		).Scan(timeScanDest(isSQLite, &pricing.CreatedAt), timeScanDest(isSQLite, &pricing.UpdatedAt)); err != nil {
			return fmt.Errorf("read model pricing timestamps: %w", err)
		}
	} else {
		err = exec.QueryRowContext(ctx, insertSQL,
			pricing.ChannelID, platform, modelsJSON, billingMode,
			pricing.InputPrice, pricing.OutputPrice, pricing.CacheWritePrice, pricing.CacheWrite1hPrice, pricing.CacheReadPrice,
			fastMultiplier, flexMultiplier, reasoningEffortMultipliersJSON, pricing.ImageInputPrice, pricing.ImageOutputPrice,
			pricing.PerRequestPrice, timePricingJSON,
		).Scan(&pricing.ID, &pricing.CreatedAt, &pricing.UpdatedAt)
	}
	if err != nil {
		return fmt.Errorf("insert model pricing: %w", err)
	}

	for i := range pricing.Intervals {
		pricing.Intervals[i].PricingID = pricing.ID
		if err := createIntervalExec(ctx, exec, &pricing.Intervals[i], sqlite...); err != nil {
			return err
		}
	}

	return nil
}

func marshalReasoningEffortMultipliers(multipliers map[string]float64) (string, error) {
	if len(multipliers) == 0 {
		return "{}", nil
	}
	data, err := json.Marshal(multipliers)
	if err != nil {
		return "", fmt.Errorf("marshal reasoning effort multipliers: %w", err)
	}
	return string(data), nil
}

func unmarshalReasoningEffortMultipliers(data []byte) (map[string]float64, error) {
	multipliers := make(map[string]float64)
	if len(data) == 0 {
		return multipliers, nil
	}
	if err := json.Unmarshal(data, &multipliers); err != nil {
		return nil, fmt.Errorf("unmarshal reasoning effort multipliers: %w", err)
	}
	if multipliers == nil {
		multipliers = make(map[string]float64)
	}
	return multipliers, nil
}

func marshalChannelTimePricing(config *service.ChannelTimePricing) (any, error) {
	if config == nil || len(config.Periods) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal time pricing: %w", err)
	}
	return string(data), nil
}

func unmarshalChannelTimePricing(data []byte) (*service.ChannelTimePricing, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var config service.ChannelTimePricing
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("unmarshal time pricing: %w", err)
	}
	return &config, nil
}

func createIntervalExec(ctx context.Context, exec dbExec, iv *service.PricingInterval, sqlite ...bool) error {
	if len(sqlite) > 0 && sqlite[0] {
		inputMultiplier := defaultFloatPtr(iv.InputMultiplier, 1)
		outputMultiplier := defaultFloatPtr(iv.OutputMultiplier, 1)
		cacheWriteMultiplier := defaultFloatPtr(iv.CacheWriteMultiplier, 1)
		cacheReadMultiplier := defaultFloatPtr(iv.CacheReadMultiplier, 1)
		result, err := exec.ExecContext(ctx,
			`INSERT INTO channel_pricing_intervals
		 (pricing_id, min_tokens, max_tokens, tier_label, input_price, output_price, cache_write_price, cache_write_1h_price, cache_read_price, input_multiplier, output_multiplier, cache_write_multiplier, cache_read_multiplier, per_request_price, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
			iv.PricingID, iv.MinTokens, iv.MaxTokens, iv.TierLabel,
			iv.InputPrice, iv.OutputPrice, iv.CacheWritePrice, iv.CacheWrite1hPrice, iv.CacheReadPrice,
			inputMultiplier, outputMultiplier, cacheWriteMultiplier, cacheReadMultiplier,
			iv.PerRequestPrice, iv.SortOrder,
		)
		if err != nil {
			return err
		}
		iv.ID, _ = result.LastInsertId()
		return nil
	}
	return exec.QueryRowContext(ctx,
		`INSERT INTO channel_pricing_intervals
		 (pricing_id, min_tokens, max_tokens, tier_label, input_price, output_price, cache_write_price, cache_write_1h_price, cache_read_price, input_multiplier, output_multiplier, cache_write_multiplier, cache_read_multiplier, per_request_price, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id, created_at, updated_at`,
		iv.PricingID, iv.MinTokens, iv.MaxTokens, iv.TierLabel,
		iv.InputPrice, iv.OutputPrice, iv.CacheWritePrice, iv.CacheWrite1hPrice, iv.CacheReadPrice,
		iv.InputMultiplier, iv.OutputMultiplier, iv.CacheWriteMultiplier, iv.CacheReadMultiplier,
		iv.PerRequestPrice, iv.SortOrder,
	).Scan(&iv.ID, &iv.CreatedAt, &iv.UpdatedAt)
}

func replaceModelPricingTx(ctx context.Context, exec dbExec, channelID int64, pricingList []service.ChannelModelPricing, sqlite ...bool) error {
	if _, err := exec.ExecContext(ctx, `DELETE FROM channel_model_pricing WHERE channel_id = $1`, channelID); err != nil {
		return fmt.Errorf("delete old model pricing: %w", err)
	}
	for i := range pricingList {
		pricingList[i].ChannelID = channelID
		if err := createModelPricingExec(ctx, exec, &pricingList[i], sqlite...); err != nil {
			return fmt.Errorf("insert model pricing: %w", err)
		}
	}
	return nil
}

// isUniqueViolation 检查 pq 唯一约束违反错误
func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr != nil {
		return pqErr.Code == "23505"
	}
	return false
}

// escapeLike 转义 LIKE/ILIKE 模式中的特殊字符
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
