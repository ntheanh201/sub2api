package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	gocache "github.com/patrickmn/go-cache"
)

const rawUsageLogModelColumn = "model"

// rawUsageLogModelColumn preserves the exact stored usage_logs.model semantics for direct filters.
// Historical rows may contain upstream/billing model values, while newer rows store requested_model.
// Requested/upstream/mapping analytics must use resolveModelDimensionExpression instead.

// usageLogSuccessFilterUL 用于把"失败请求 usage log"（tokens=0、cost=0、不计费的占位记录）
// 从统计性聚合中排除，避免污染 Dashboard / 用量拆分等指标。
//
// schema 中没有 success bool 列；新增列要做迁移，风险大；这里用 actual_cost > 0 作为代理：
// 任何成功落账的请求都会产生 actual_cost（包括 token 计费、纯图片 token 计费、按次/按图计费），
// 反之 failed-request usage log 的 actual_cost 为 0。
// 早期版本用 4 项 token 和 > 0 判定会把"按次/按图计费"与"image_output_tokens 独立计费"的纯图片
// 请求误判为失败，导致这部分请求从用量统计里消失，故改用 actual_cost。
// 配合 `FROM usage_logs ul` JOIN 查询使用。
const usageLogSuccessFilterUL = "ul.actual_cost > 0"

// usageLogEffectivePlatformExpr 用于按"有效平台"维度聚合 usage_logs：
// 优先取请求实际走的分组 platform，若分组未设置 platform 再 fallback 到 account.platform。
// Composite groups are a routing layer, so platform analytics must use the
// resolved concrete account platform instead of grouping spend under "composite".
// 配套要求查询里 LEFT JOIN groups g ON g.id = ul.group_id 与 LEFT JOIN accounts a ON a.id = ul.account_id。
const usageLogEffectivePlatformExpr = "CASE WHEN g.platform = 'composite' THEN a.platform ELSE COALESCE(NULLIF(g.platform,''), a.platform) END"

// dateFormatWhitelist 将 granularity 参数映射为 PostgreSQL TO_CHAR 格式字符串，防止外部输入直接拼入 SQL
var dateFormatWhitelist = map[string]string{
	"hour":  "YYYY-MM-DD HH24:00",
	"day":   "YYYY-MM-DD",
	"week":  "IYYY-IW",
	"month": "YYYY-MM",
}

// safeDateFormat 根据白名单获取 dateFormat，未匹配时返回默认值
func safeDateFormat(granularity string) string {
	if f, ok := dateFormatWhitelist[granularity]; ok {
		return f
	}
	return "YYYY-MM-DD"
}

// appendRawUsageLogModelWhereCondition keeps direct model filters on the raw model column for backward
// compatibility with historical rows. Requested/upstream analytics must use
// resolveModelDimensionExpression instead.
func appendRawUsageLogModelWhereCondition(conditions []string, args []any, model string) ([]string, []any) {
	if strings.TrimSpace(model) == "" {
		return conditions, args
	}
	conditions = append(conditions, fmt.Sprintf("%s = $%d", rawUsageLogModelColumn, len(args)+1))
	args = append(args, model)
	return conditions, args
}

func appendUsageLogBillingModeWhereCondition(conditions []string, args []any, billingMode string) ([]string, []any) {
	return appendUsageLogBillingModeWhereConditionWithAlias(conditions, args, billingMode, "")
}

func appendUsageLogBillingModeWhereConditionWithAlias(conditions []string, args []any, billingMode string, alias string) ([]string, []any) {
	mode := strings.TrimSpace(billingMode)
	if mode == "" {
		return conditions, args
	}
	column := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	placeholder := fmt.Sprintf("$%d", len(args)+1)
	switch service.BillingMode(mode) {
	case service.BillingModeImage:
		conditions = append(conditions, fmt.Sprintf("(%s = %s OR ((%s IS NULL OR %s = '') AND COALESCE(%s, 0) > 0))", column("billing_mode"), placeholder, column("billing_mode"), column("billing_mode"), column("image_count")))
	case service.BillingModeVideo:
		conditions = append(conditions, fmt.Sprintf("%s = %s", column("billing_mode"), placeholder))
	case service.BillingModeToken:
		conditions = append(conditions, fmt.Sprintf("(%s = %s OR ((%s IS NULL OR %s = '') AND COALESCE(%s, 0) <= 0))", column("billing_mode"), placeholder, column("billing_mode"), column("billing_mode"), column("image_count")))
	default:
		conditions = append(conditions, fmt.Sprintf("%s = %s", column("billing_mode"), placeholder))
	}
	args = append(args, mode)
	return conditions, args
}

func appendUsageLogBillingModeQueryFilter(query string, args []any, billingMode string, alias string) (string, []any) {
	conditions, args := appendUsageLogBillingModeWhereConditionWithAlias(nil, args, billingMode, alias)
	if len(conditions) == 0 {
		return query, args
	}
	return query + " AND " + conditions[0], args
}

func appendUsageLogModelWhereCondition(conditions []string, args []any, model string, source string) ([]string, []any) {
	if strings.TrimSpace(source) == "" {
		return appendRawUsageLogModelWhereCondition(conditions, args, model)
	}
	if strings.TrimSpace(model) == "" {
		return conditions, args
	}
	conditions = append(conditions, fmt.Sprintf("%s = $%d", resolveModelDimensionExpression(source), len(args)+1))
	args = append(args, model)
	return conditions, args
}

// appendRawUsageLogModelQueryFilter keeps direct model filters on the raw model column for backward
// compatibility with historical rows. Requested/upstream analytics must use
// resolveModelDimensionExpression instead.
func appendRawUsageLogModelQueryFilter(query string, args []any, model string) (string, []any) {
	if strings.TrimSpace(model) == "" {
		return query, args
	}
	query += fmt.Sprintf(" AND %s = $%d", rawUsageLogModelColumn, len(args)+1)
	args = append(args, model)
	return query, args
}

func appendUsageLogModelQueryFilter(query string, args []any, model string, source string) (string, []any) {
	if strings.TrimSpace(source) == "" {
		return appendRawUsageLogModelQueryFilter(query, args, model)
	}
	if strings.TrimSpace(model) == "" {
		return query, args
	}
	query += fmt.Sprintf(" AND %s = $%d", resolveModelDimensionExpression(source), len(args)+1)
	args = append(args, model)
	return query, args
}

type usageLogRepository struct {
	client *dbent.Client
	sql    sqlExecutor
	db     *sql.DB

	createBatchOnce     sync.Once
	createBatchCh       chan usageLogCreateRequest
	bestEffortBatchOnce sync.Once
	bestEffortBatchCh   chan usageLogBestEffortRequest
	bestEffortRecent    *gocache.Cache
}

func NewUsageLogRepository(client *dbent.Client, sqlDB *sql.DB) service.UsageLogRepository {
	return newUsageLogRepositoryWithSQL(client, sqlDB)
}

func newUsageLogRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *usageLogRepository {
	// 使用 scanSingleRow 替代 QueryRowContext，保证 ent.Tx 作为 sqlExecutor 可用。
	repo := &usageLogRepository{client: client, sql: sqlq}
	if db, ok := sqlq.(*sql.DB); ok {
		repo.db = db
	}
	repo.bestEffortRecent = gocache.New(usageLogBestEffortRecentTTL, time.Minute)
	return repo
}

func buildWhere(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

func appendRequestTypeOrStreamWhereCondition(conditions []string, args []any, requestType *int16, stream *bool) ([]string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		conditions = append(conditions, condition)
		args = append(args, conditionArgs...)
		return conditions, args
	}
	if stream != nil {
		conditions = append(conditions, fmt.Sprintf("stream = $%d", len(args)+1))
		args = append(args, *stream)
	}
	return conditions, args
}

func appendRequestTypeOrStreamQueryFilter(query string, args []any, requestType *int16, stream *bool) (string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		query += " AND " + condition
		args = append(args, conditionArgs...)
		return query, args
	}
	if stream != nil {
		query += fmt.Sprintf(" AND stream = $%d", len(args)+1)
		args = append(args, *stream)
	}
	return query, args
}

// buildRequestTypeFilterCondition 在 request_type 过滤时兼容 legacy 字段，避免历史数据漏查。
func buildRequestTypeFilterCondition(startArgIndex int, requestType int16) (string, []any) {
	return buildRequestTypeFilterConditionWithAlias(startArgIndex, requestType, "")
}

func buildRequestTypeFilterConditionWithAlias(startArgIndex int, requestType int16, alias string) (string, []any) {
	normalized := service.RequestTypeFromInt16(requestType)
	requestTypeArg := int16(normalized)
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	switch normalized {
	case service.RequestTypeSync:
		return fmt.Sprintf("(%srequest_type = $%d OR (%srequest_type = %d AND %sstream = FALSE AND %sopenai_ws_mode = FALSE))", prefix, startArgIndex, prefix, int16(service.RequestTypeUnknown), prefix, prefix), []any{requestTypeArg}
	case service.RequestTypeStream:
		return fmt.Sprintf("(%srequest_type = $%d OR (%srequest_type = %d AND %sstream = TRUE AND %sopenai_ws_mode = FALSE))", prefix, startArgIndex, prefix, int16(service.RequestTypeUnknown), prefix, prefix), []any{requestTypeArg}
	case service.RequestTypeWSV2:
		return fmt.Sprintf("(%srequest_type = $%d OR (%srequest_type = %d AND %sopenai_ws_mode = TRUE))", prefix, startArgIndex, prefix, int16(service.RequestTypeUnknown), prefix), []any{requestTypeArg}
	default:
		return fmt.Sprintf("%srequest_type = $%d", prefix, startArgIndex), []any{requestTypeArg}
	}
}

// GetUserAPIKeyLeaderboard returns aggregated usage per API key for a single user
// over a date range. Keys with no usage in the range are still returned with zeros
// so the dashboard can show idle keys.
func (r *usageLogRepository) GetUserAPIKeyLeaderboard(ctx context.Context, userID int64, startTime, endTime time.Time) ([]*usagestats.APIKeyLeaderboardRow, error) {
	if endTime.IsZero() {
		endTime = time.Now()
	}
	if startTime.IsZero() {
		startTime = endTime.AddDate(0, 0, -7)
	}

	query := `
		SELECT
			ak.id,
			ak.name,
			ak.status,
			ak.last_used_at,
			COALESCE(stats.requests, 0),
			COALESCE(stats.input_tokens, 0),
			COALESCE(stats.output_tokens, 0),
			COALESCE(stats.cache_creation_tokens, 0),
			COALESCE(stats.cache_read_tokens, 0),
			COALESCE(stats.input_cost, 0),
			COALESCE(stats.cache_read_cost, 0),
			COALESCE(stats.total_cost, 0),
			COALESCE(stats.actual_cost, 0),
			COALESCE(stats.avg_duration_ms, 0)
		FROM api_keys ak
		LEFT JOIN (
			SELECT
				api_key_id,
				COUNT(*) AS requests,
				SUM(input_tokens) AS input_tokens,
				SUM(output_tokens) AS output_tokens,
				SUM(cache_creation_tokens) AS cache_creation_tokens,
				SUM(cache_read_tokens) AS cache_read_tokens,
				SUM(input_cost) AS input_cost,
				SUM(cache_read_cost) AS cache_read_cost,
				SUM(total_cost) AS total_cost,
				SUM(actual_cost) AS actual_cost,
				AVG(COALESCE(duration_ms, 0)) AS avg_duration_ms
			FROM usage_logs
			WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
			GROUP BY api_key_id
		) stats ON stats.api_key_id = ak.id
		WHERE ak.user_id = $1 AND ak.deleted_at IS NULL
		ORDER BY COALESCE(stats.actual_cost, 0) DESC, ak.id ASC
	`

	rows, err := r.sql.QueryContext(ctx, query, userID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*usagestats.APIKeyLeaderboardRow, 0)
	for rows.Next() {
		var row usagestats.APIKeyLeaderboardRow
		var lastUsed sql.NullTime
		if err := rows.Scan(
			&row.APIKeyID,
			&row.Name,
			&row.Status,
			&lastUsed,
			&row.Requests,
			&row.InputTokens,
			&row.OutputTokens,
			&row.CacheCreationTokens,
			&row.CacheReadTokens,
			&row.InputCost,
			&row.CacheReadCost,
			&row.TotalCost,
			&row.ActualCost,
			&row.AverageDurationMs,
		); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			row.LastUsedAt = &t
		}
		row.TotalTokens = row.InputTokens + row.OutputTokens + row.CacheCreationTokens + row.CacheReadTokens
		fillCacheMetrics(&row)
		out = append(out, &row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// fillCacheMetrics computes CacheReusePct and CacheSavings on a row using its
// own aggregated input cost as the unit-rate reference. Falls back to a 9×
// multiplier on cache_read_cost when fresh-input data isn't available (matches
// Anthropic's ~10% cache pricing convention).
func fillCacheMetrics(row *usagestats.APIKeyLeaderboardRow) {
	cacheBudget := row.CacheReadTokens + row.CacheCreationTokens
	if cacheBudget > 0 {
		row.CacheReusePct = float64(row.CacheReadTokens) / float64(cacheBudget) * 100.0
	}
	switch {
	case row.InputTokens > 0 && row.CacheReadTokens > 0 && row.InputCost > 0:
		inputRate := row.InputCost / float64(row.InputTokens)
		hypothetical := inputRate * float64(row.CacheReadTokens)
		savings := hypothetical - row.CacheReadCost
		if savings > 0 {
			row.CacheSavings = savings
		}
	case row.CacheReadCost > 0:
		row.CacheSavings = row.CacheReadCost * 9
	}
}

// GetAllAPIKeysLeaderboard is the admin variant: returns aggregated usage per
// API key across ALL users in the given range, joined with the users table so
// the dashboard can show the owner of each key.
func (r *usageLogRepository) GetAllAPIKeysLeaderboard(ctx context.Context, startTime, endTime time.Time) ([]*usagestats.APIKeyLeaderboardRow, error) {
	if endTime.IsZero() {
		endTime = time.Now()
	}
	if startTime.IsZero() {
		startTime = endTime.AddDate(0, 0, -7)
	}

	query := `
		SELECT
			ak.id,
			ak.name,
			ak.status,
			ak.last_used_at,
			ak.user_id,
			COALESCE(u.email, ''),
			COALESCE(stats.requests, 0),
			COALESCE(stats.input_tokens, 0),
			COALESCE(stats.output_tokens, 0),
			COALESCE(stats.cache_creation_tokens, 0),
			COALESCE(stats.cache_read_tokens, 0),
			COALESCE(stats.input_cost, 0),
			COALESCE(stats.cache_read_cost, 0),
			COALESCE(stats.total_cost, 0),
			COALESCE(stats.actual_cost, 0),
			COALESCE(stats.avg_duration_ms, 0)
		FROM api_keys ak
		LEFT JOIN users u ON u.id = ak.user_id AND u.deleted_at IS NULL
		LEFT JOIN (
			SELECT
				api_key_id,
				COUNT(*) AS requests,
				SUM(input_tokens) AS input_tokens,
				SUM(output_tokens) AS output_tokens,
				SUM(cache_creation_tokens) AS cache_creation_tokens,
				SUM(cache_read_tokens) AS cache_read_tokens,
				SUM(input_cost) AS input_cost,
				SUM(cache_read_cost) AS cache_read_cost,
				SUM(total_cost) AS total_cost,
				SUM(actual_cost) AS actual_cost,
				AVG(COALESCE(duration_ms, 0)) AS avg_duration_ms
			FROM usage_logs
			WHERE created_at >= $1 AND created_at < $2
			GROUP BY api_key_id
		) stats ON stats.api_key_id = ak.id
		WHERE ak.deleted_at IS NULL
		ORDER BY COALESCE(stats.actual_cost, 0) DESC, ak.id ASC
	`

	rows, err := r.sql.QueryContext(ctx, query, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*usagestats.APIKeyLeaderboardRow, 0)
	for rows.Next() {
		var row usagestats.APIKeyLeaderboardRow
		var lastUsed sql.NullTime
		if err := rows.Scan(
			&row.APIKeyID,
			&row.Name,
			&row.Status,
			&lastUsed,
			&row.UserID,
			&row.UserEmail,
			&row.Requests,
			&row.InputTokens,
			&row.OutputTokens,
			&row.CacheCreationTokens,
			&row.CacheReadTokens,
			&row.InputCost,
			&row.CacheReadCost,
			&row.TotalCost,
			&row.ActualCost,
			&row.AverageDurationMs,
		); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			row.LastUsedAt = &t
		}
		row.TotalTokens = row.InputTokens + row.OutputTokens + row.CacheCreationTokens + row.CacheReadTokens
		fillCacheMetrics(&row)
		out = append(out, &row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
