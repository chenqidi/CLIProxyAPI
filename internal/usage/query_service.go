package usage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

const (
	defaultStatusBlockCount     = 20
	defaultStatusBlockDuration  = 10 * time.Minute
	defaultHealthRows           = 7
	defaultHealthCols           = 96
	defaultHealthBlockDuration  = 15 * time.Minute
	defaultHourChartWindowHours = 24
	defaultDayChartWindowDays   = 30
	defaultEventsPage           = 1
	defaultEventsPageSize       = 100
	maxEventsPageSize           = 500
	rateWindowDuration          = 30 * time.Minute
)

// StatusBlockState is the state of a status bar block.
type StatusBlockState string

const (
	StatusBlockIdle    StatusBlockState = "idle"
	StatusBlockSuccess StatusBlockState = "success"
	StatusBlockFailure StatusBlockState = "failure"
	StatusBlockMixed   StatusBlockState = "mixed"
)

// TimeWindow represents the effective query window after retention clamping.
type TimeWindow struct {
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
}

// UsageSummary is the lightweight summary payload for the management usage dashboard.
type UsageSummary struct {
	TimeWindow
	TotalRequests     int64    `json:"total_requests"`
	SuccessCount      int64    `json:"success_count"`
	FailureCount      int64    `json:"failure_count"`
	TotalTokens       int64    `json:"total_tokens"`
	TotalCost         *float64 `json:"total_cost,omitempty"`
	CachedTokens      int64    `json:"cached_tokens"`
	ReasoningTokens   int64    `json:"reasoning_tokens"`
	DistinctSources   int64    `json:"distinct_sources"`
	DistinctAuthIndex int64    `json:"distinct_auth_indexes"`
	DistinctModels    int64    `json:"distinct_models"`
	RequestsLast30m   int64    `json:"requests_last_30m"`
	TokensLast30m     int64    `json:"tokens_last_30m"`
	RPM30m            float64  `json:"rpm_30m"`
	TPM30m            float64  `json:"tpm_30m"`
	RetentionDays     int      `json:"retention_days"`
}

// StatusBlockDetail captures success/failure counts for a single status block.
type StatusBlockDetail struct {
	Success     int64   `json:"success"`
	Failure     int64   `json:"failure"`
	Rate        float64 `json:"rate"`
	StartTimeMs int64   `json:"start_time_ms"`
	EndTimeMs   int64   `json:"end_time_ms"`
}

// StatusBarData matches the status-bar semantics currently derived in the management UI.
type StatusBarData struct {
	Blocks       []StatusBlockState  `json:"blocks"`
	BlockDetails []StatusBlockDetail `json:"block_details"`
	SuccessRate  float64             `json:"success_rate"`
	TotalSuccess int64               `json:"total_success"`
	TotalFailure int64               `json:"total_failure"`
}

// UsageStatusOverview provides service-wide and grouped status-bar data.
type UsageStatusOverview struct {
	TimeWindow
	Service     StatusBarData            `json:"service"`
	BySource    map[string]StatusBarData `json:"by_source"`
	ByAuthIndex map[string]StatusBarData `json:"by_auth_index"`
}

// ServiceHealthData represents the 7x96 15-minute health grid used by the management UI.
type ServiceHealthData struct {
	TimeWindow
	Blocks       []StatusBlockState  `json:"blocks"`
	BlockDetails []StatusBlockDetail `json:"block_details"`
	SuccessRate  float64             `json:"success_rate"`
	TotalSuccess int64               `json:"total_success"`
	TotalFailure int64               `json:"total_failure"`
	Rows         int                 `json:"rows"`
	Cols         int                 `json:"cols"`
}

// UsageChartQuery customizes chart aggregation.
type UsageChartQuery struct {
	Start           *time.Time
	End             *time.Time
	Period          string
	Metric          string
	HourWindowHours int
}

// UsageChartData is a lightweight chart payload grouped by model.
type UsageChartData struct {
	TimeWindow
	Period      string               `json:"period"`
	Metric      string               `json:"metric"`
	Labels      []string             `json:"labels"`
	DataByModel map[string][]float64 `json:"data_by_model"`
}

// UsageTokenChartData returns bucketed token stats grouped by model.
type UsageTokenChartData struct {
	TimeWindow
	Period      string                  `json:"period"`
	Labels      []string                `json:"labels"`
	DataByModel map[string][]TokenStats `json:"data_by_model"`
}

// UsageEventsQuery filters and paginates recent request events.
type UsageEventsQuery struct {
	Start         *time.Time
	End           *time.Time
	Page          int
	PageSize      int
	SkipTotal     bool
	Model         string
	Source        string
	AuthIndex     string
	RequestMethod string
	RequestPath   string
	Failed        *bool
}

// UsageEventItem is the lightweight event row returned to the management UI.
type UsageEventItem struct {
	Timestamp           time.Time  `json:"timestamp"`
	Provider            string     `json:"provider"`
	Model               string     `json:"model"`
	APIKey              string     `json:"api_key"`
	ClientIP            string     `json:"client_ip"`
	RequestMethod       string     `json:"request_method"`
	RequestPath         string     `json:"request_path"`
	AuthID              string     `json:"auth_id"`
	AuthIndex           string     `json:"auth_index"`
	Source              string     `json:"source"`
	LatencyMs           int64      `json:"latency_ms"`
	FirstTokenLatencyMs int64      `json:"first_token_latency_ms"`
	Failed              bool       `json:"failed"`
	Tokens              TokenStats `json:"tokens"`
}

// UsageEventsPage returns paginated request events.
type UsageEventsPage struct {
	TimeWindow
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalItems int64            `json:"total_items"`
	TotalExact bool             `json:"total_exact"`
	HasMore    bool             `json:"has_more"`
	Items      []UsageEventItem `json:"items"`
}

// CurrentAuthHit describes the latest successful auth hit observed for a provider.
type CurrentAuthHit struct {
	Timestamp time.Time `json:"timestamp"`
	Provider  string    `json:"provider"`
	AuthID    string    `json:"auth_id"`
	AuthIndex string    `json:"auth_index"`
}

// QueryService provides lightweight usage queries backed by SQLite.
type QueryService struct {
	stats *RequestStatistics
}

// NewQueryService creates a new usage query service.
func NewQueryService(stats *RequestStatistics) *QueryService {
	return &QueryService{stats: stats}
}

// RetentionDays returns the effective usage retention window.
func (s *QueryService) RetentionDays() int {
	if repo := s.sqliteRepo(); repo != nil && repo.retentionDays > 0 {
		return repo.retentionDays
	}
	return defaultUsageRetentionDays
}

// Summary returns the lightweight summary payload for the requested time window.
func (s *QueryService) Summary(ctx context.Context, start, end *time.Time) (UsageSummary, error) {
	summary := UsageSummary{RetentionDays: s.RetentionDays()}
	repo, window, ok, err := s.repoAndWindow(ctx, start, end, time.Duration(s.RetentionDays())*24*time.Hour)
	if err != nil {
		return summary, err
	}
	summary.TimeWindow = window
	if !ok {
		return summary, nil
	}

	row := repo.db.QueryRowContext(ctx, `SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN failed != 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(cached_tokens), 0),
		COALESCE(SUM(reasoning_tokens), 0),
		COUNT(DISTINCT NULLIF(source, '')),
		COUNT(DISTINCT NULLIF(auth_index, '')),
		COUNT(DISTINCT NULLIF(model, ''))
	FROM usage_events
	WHERE requested_at_ns >= ? AND requested_at_ns <= ?`, window.WindowStart.UnixNano(), window.WindowEnd.UnixNano())
	if err := row.Scan(
		&summary.TotalRequests,
		&summary.SuccessCount,
		&summary.FailureCount,
		&summary.TotalTokens,
		&summary.CachedTokens,
		&summary.ReasoningTokens,
		&summary.DistinctSources,
		&summary.DistinctAuthIndex,
		&summary.DistinctModels,
	); err != nil {
		return summary, fmt.Errorf("usage query summary: %w", err)
	}

	rateStart := window.WindowEnd.Add(-rateWindowDuration)
	if rateStart.Before(window.WindowStart) {
		rateStart = window.WindowStart
	}
	rateRow := repo.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(total_tokens), 0)
	FROM usage_events
	WHERE requested_at_ns >= ? AND requested_at_ns <= ?`, rateStart.UnixNano(), window.WindowEnd.UnixNano())
	if err := rateRow.Scan(&summary.RequestsLast30m, &summary.TokensLast30m); err != nil {
		return summary, fmt.Errorf("usage query summary rates: %w", err)
	}
	minutes := window.WindowEnd.Sub(rateStart).Minutes()
	if minutes > 0 {
		summary.RPM30m = float64(summary.RequestsLast30m) / minutes
		summary.TPM30m = float64(summary.TokensLast30m) / minutes
	}
	return summary, nil
}

// TokenTotalsByModel returns aggregated token totals grouped by model for the requested time window.
func (s *QueryService) TokenTotalsByModel(ctx context.Context, start, end *time.Time) (map[string]TokenStats, error) {
	repo, window, ok, err := s.repoAndWindow(ctx, start, end, time.Duration(s.RetentionDays())*24*time.Hour)
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string]TokenStats{}, nil
	}

	rows, err := repo.db.QueryContext(ctx, `SELECT
		model,
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(reasoning_tokens), 0),
		COALESCE(SUM(cached_tokens), 0),
		COALESCE(SUM(total_tokens), 0)
	FROM usage_events
	WHERE requested_at_ns >= ? AND requested_at_ns <= ?
	GROUP BY model`,
		window.WindowStart.UnixNano(),
		window.WindowEnd.UnixNano(),
	)
	if err != nil {
		return nil, fmt.Errorf("usage query token totals by model: %w", err)
	}
	defer rows.Close()

	totals := make(map[string]TokenStats)
	for rows.Next() {
		var (
			model string
			stats TokenStats
		)
		if err := rows.Scan(
			&model,
			&stats.InputTokens,
			&stats.OutputTokens,
			&stats.ReasoningTokens,
			&stats.CachedTokens,
			&stats.TotalTokens,
		); err != nil {
			return nil, fmt.Errorf("usage query token totals by model scan: %w", err)
		}
		totals[strings.TrimSpace(model)] = normaliseTokenStats(stats)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage query token totals by model iterate: %w", err)
	}
	return totals, nil
}

// Status returns service-wide and grouped status-bar data for the requested time window.
func (s *QueryService) Status(ctx context.Context, start, end *time.Time) (UsageStatusOverview, error) {
	overview := UsageStatusOverview{
		BySource:    make(map[string]StatusBarData),
		ByAuthIndex: make(map[string]StatusBarData),
	}
	repo, window, ok, err := s.repoAndWindow(ctx, start, end, defaultStatusBlockCount*defaultStatusBlockDuration)
	if err != nil {
		return overview, err
	}
	overview.TimeWindow = window
	overview.Service = newStatusBarData(defaultStatusBlockCount, window.WindowStart, defaultStatusBlockDuration)
	if !ok {
		return overview, nil
	}

	rows, err := repo.db.QueryContext(ctx, `SELECT
		MIN(CAST((requested_at_ns - ?) / ? AS INTEGER), ?) AS bucket_idx,
		source,
		auth_index,
		COALESCE(SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END), 0) AS success_count,
		COALESCE(SUM(CASE WHEN failed != 0 THEN 1 ELSE 0 END), 0) AS failure_count
	FROM usage_events
	WHERE requested_at_ns >= ? AND requested_at_ns <= ?
	GROUP BY bucket_idx, source, auth_index
	ORDER BY bucket_idx ASC`,
		window.WindowStart.UnixNano(),
		defaultStatusBlockDuration.Nanoseconds(),
		defaultStatusBlockCount-1,
		window.WindowStart.UnixNano(),
		window.WindowEnd.UnixNano(),
	)
	if err != nil {
		return overview, fmt.Errorf("usage query status: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			bucketIdx    int
			source       string
			authIndex    string
			successCount int64
			failureCount int64
		)
		if err := rows.Scan(&bucketIdx, &source, &authIndex, &successCount, &failureCount); err != nil {
			return overview, fmt.Errorf("usage query status scan: %w", err)
		}
		applyStatusCounts(&overview.Service, bucketIdx, successCount, failureCount)

		source = strings.TrimSpace(source)
		if source != "" {
			data := overview.BySource[source]
			if len(data.Blocks) == 0 {
				data = newStatusBarData(defaultStatusBlockCount, window.WindowStart, defaultStatusBlockDuration)
			}
			applyStatusCounts(&data, bucketIdx, successCount, failureCount)
			overview.BySource[source] = data
		}

		authIndex = strings.TrimSpace(authIndex)
		if authIndex != "" {
			data := overview.ByAuthIndex[authIndex]
			if len(data.Blocks) == 0 {
				data = newStatusBarData(defaultStatusBlockCount, window.WindowStart, defaultStatusBlockDuration)
			}
			applyStatusCounts(&data, bucketIdx, successCount, failureCount)
			overview.ByAuthIndex[authIndex] = data
		}
	}
	if err := rows.Err(); err != nil {
		return overview, fmt.Errorf("usage query status iterate: %w", err)
	}
	finalizeStatusBar(&overview.Service)
	for key, data := range overview.BySource {
		finalizeStatusBar(&data)
		overview.BySource[key] = data
	}
	for key, data := range overview.ByAuthIndex {
		finalizeStatusBar(&data)
		overview.ByAuthIndex[key] = data
	}
	return overview, nil
}

// Health returns the service health grid for the requested time window.
func (s *QueryService) Health(ctx context.Context, start, end *time.Time) (ServiceHealthData, error) {
	blockCount := defaultHealthRows * defaultHealthCols
	health := ServiceHealthData{
		Rows:         defaultHealthRows,
		Cols:         defaultHealthCols,
		Blocks:       make([]StatusBlockState, blockCount),
		BlockDetails: make([]StatusBlockDetail, blockCount),
	}
	repo, window, ok, err := s.repoAndWindow(ctx, start, end, time.Duration(blockCount)*defaultHealthBlockDuration)
	if err != nil {
		return health, err
	}
	health.TimeWindow = window
	seedStatusDetails(health.BlockDetails, blockCount, window.WindowStart, defaultHealthBlockDuration)
	if !ok {
		for i := range health.Blocks {
			health.Blocks[i] = StatusBlockIdle
		}
		return health, nil
	}

	rows, err := repo.db.QueryContext(ctx, `SELECT
		MIN(CAST((requested_at_ns - ?) / ? AS INTEGER), ?) AS bucket_idx,
		COALESCE(SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END), 0) AS success_count,
		COALESCE(SUM(CASE WHEN failed != 0 THEN 1 ELSE 0 END), 0) AS failure_count
	FROM usage_events
	WHERE requested_at_ns >= ? AND requested_at_ns <= ?
	GROUP BY bucket_idx
	ORDER BY bucket_idx ASC`,
		window.WindowStart.UnixNano(),
		defaultHealthBlockDuration.Nanoseconds(),
		blockCount-1,
		window.WindowStart.UnixNano(),
		window.WindowEnd.UnixNano(),
	)
	if err != nil {
		return health, fmt.Errorf("usage query health: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			bucketIdx    int
			successCount int64
			failureCount int64
		)
		if err := rows.Scan(&bucketIdx, &successCount, &failureCount); err != nil {
			return health, fmt.Errorf("usage query health scan: %w", err)
		}
		if bucketIdx < 0 || bucketIdx >= len(health.BlockDetails) {
			continue
		}
		health.BlockDetails[bucketIdx].Success = successCount
		health.BlockDetails[bucketIdx].Failure = failureCount
		health.TotalSuccess += successCount
		health.TotalFailure += failureCount
	}
	if err := rows.Err(); err != nil {
		return health, fmt.Errorf("usage query health iterate: %w", err)
	}

	finalizeHealth(&health)
	return health, nil
}

// Charts returns chart-ready time-series data grouped by model.
func (s *QueryService) Charts(ctx context.Context, query UsageChartQuery) (UsageChartData, error) {
	metric := strings.ToLower(strings.TrimSpace(query.Metric))
	if metric == "" {
		metric = "requests"
	}
	if metric != "requests" && metric != "tokens" {
		return UsageChartData{}, fmt.Errorf("unsupported chart metric %q", metric)
	}

	result := UsageChartData{Metric: metric, DataByModel: make(map[string][]float64)}
	repo, window, normalizedPeriod, step, labels, ok, err := s.prepareChartWindow(ctx, query)
	if err != nil {
		return result, err
	}
	result.Period = normalizedPeriod
	result.TimeWindow = window
	result.Labels = labels
	if !ok || len(result.Labels) == 0 {
		return result, nil
	}

	valueExpr := "COUNT(*)"
	if metric == "tokens" {
		valueExpr = "COALESCE(SUM(total_tokens), 0)"
	}
	rows, err := repo.db.QueryContext(ctx, fmt.Sprintf(`SELECT
		MIN(CAST((requested_at_ns - ?) / ? AS INTEGER), ?) AS bucket_idx,
		model,
		%s AS metric_value
	FROM usage_events
	WHERE requested_at_ns >= ? AND requested_at_ns <= ?
	GROUP BY bucket_idx, model
	ORDER BY bucket_idx ASC, model ASC`, valueExpr),
		window.WindowStart.UnixNano(),
		step.Nanoseconds(),
		len(result.Labels)-1,
		window.WindowStart.UnixNano(),
		window.WindowEnd.UnixNano(),
	)
	if err != nil {
		return result, fmt.Errorf("usage query charts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			bucketIdx int
			model     string
			value     int64
		)
		if err := rows.Scan(&bucketIdx, &model, &value); err != nil {
			return result, fmt.Errorf("usage query charts scan: %w", err)
		}
		model = strings.TrimSpace(model)
		if model == "" {
			model = "unknown"
		}
		series, ok := result.DataByModel[model]
		if !ok {
			series = make([]float64, len(result.Labels))
		}
		if bucketIdx >= 0 && bucketIdx < len(series) {
			series[bucketIdx] = float64(value)
		}
		result.DataByModel[model] = series
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("usage query charts iterate: %w", err)
	}
	return result, nil
}

// TokenCharts returns bucketed token stats grouped by model for the requested chart window.
func (s *QueryService) TokenCharts(ctx context.Context, query UsageChartQuery) (UsageTokenChartData, error) {
	result := UsageTokenChartData{DataByModel: make(map[string][]TokenStats)}
	repo, window, period, step, labels, ok, err := s.prepareChartWindow(ctx, query)
	if err != nil {
		return result, err
	}
	result.Period = period
	result.TimeWindow = window
	result.Labels = labels
	if !ok || len(result.Labels) == 0 {
		return result, nil
	}

	rows, err := repo.db.QueryContext(ctx, `SELECT
		MIN(CAST((requested_at_ns - ?) / ? AS INTEGER), ?) AS bucket_idx,
		model,
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(reasoning_tokens), 0),
		COALESCE(SUM(cached_tokens), 0),
		COALESCE(SUM(total_tokens), 0)
	FROM usage_events
	WHERE requested_at_ns >= ? AND requested_at_ns <= ?
	GROUP BY bucket_idx, model
	ORDER BY bucket_idx ASC, model ASC`,
		window.WindowStart.UnixNano(),
		step.Nanoseconds(),
		len(result.Labels)-1,
		window.WindowStart.UnixNano(),
		window.WindowEnd.UnixNano(),
	)
	if err != nil {
		return result, fmt.Errorf("usage query token charts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			bucketIdx int
			model     string
			stats     TokenStats
		)
		if err := rows.Scan(
			&bucketIdx,
			&model,
			&stats.InputTokens,
			&stats.OutputTokens,
			&stats.ReasoningTokens,
			&stats.CachedTokens,
			&stats.TotalTokens,
		); err != nil {
			return result, fmt.Errorf("usage query token charts scan: %w", err)
		}
		model = strings.TrimSpace(model)
		if model == "" {
			model = "unknown"
		}
		series, ok := result.DataByModel[model]
		if !ok {
			series = make([]TokenStats, len(result.Labels))
		}
		if bucketIdx >= 0 && bucketIdx < len(series) {
			series[bucketIdx] = normaliseTokenStats(stats)
		}
		result.DataByModel[model] = series
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("usage query token charts iterate: %w", err)
	}
	return result, nil
}

// Events returns paginated lightweight request events.
func (s *QueryService) Events(ctx context.Context, query UsageEventsQuery) (UsageEventsPage, error) {
	result := UsageEventsPage{}
	repo, window, ok, err := s.repoAndWindow(ctx, query.Start, query.End, time.Duration(s.RetentionDays())*24*time.Hour)
	if err != nil {
		return result, err
	}
	result.TimeWindow = window
	result.Page = normalisePositiveInt(query.Page, defaultEventsPage)
	result.PageSize = normalisePageSize(query.PageSize)
	if !ok {
		return result, nil
	}

	clauses := []string{"requested_at_ns >= ?", "requested_at_ns <= ?"}
	args := []any{window.WindowStart.UnixNano(), window.WindowEnd.UnixNano()}
	if model := strings.TrimSpace(query.Model); model != "" {
		clauses = append(clauses, "model = ?")
		args = append(args, model)
	}
	if source := strings.TrimSpace(query.Source); source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, source)
	}
	if authIndex := strings.TrimSpace(query.AuthIndex); authIndex != "" {
		clauses = append(clauses, "auth_index = ?")
		args = append(args, authIndex)
	}
	if requestMethod := normaliseRequestMethod(query.RequestMethod); requestMethod != "" {
		clauses = append(clauses, "(request_method = ? OR api_key LIKE ?)")
		args = append(args, requestMethod, requestMethod+" %")
	}
	if requestPath := normaliseRequestPath(query.RequestPath); requestPath != "" {
		pathClauses := []string{"request_path = ?"}
		pathArgs := []any{requestPath}
		for _, legacyKey := range buildLegacyEndpointKeys(query.RequestMethod, requestPath) {
			pathClauses = append(pathClauses, "api_key = ?")
			pathArgs = append(pathArgs, legacyKey)
		}
		clauses = append(clauses, "("+strings.Join(pathClauses, " OR ")+")")
		args = append(args, pathArgs...)
	}
	if query.Failed != nil {
		clauses = append(clauses, "failed = ?")
		if *query.Failed {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	whereClause := strings.Join(clauses, " AND ")

	result.TotalExact = !query.SkipTotal
	if !query.SkipTotal {
		countRow := repo.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM usage_events WHERE %s`, whereClause), args...)
		if err := countRow.Scan(&result.TotalItems); err != nil {
			return result, fmt.Errorf("usage query events count: %w", err)
		}
		if result.TotalItems == 0 {
			return result, nil
		}
	}

	offset := (result.Page - 1) * result.PageSize
	limit := result.PageSize
	if query.SkipTotal {
		limit++
	}
	itemArgs := append(append([]any{}, args...), limit, offset)
	rows, err := repo.db.QueryContext(ctx, fmt.Sprintf(`SELECT
		requested_at_ns,
		provider,
		model,
		api_key,
		client_ip,
		request_method,
		request_path,
		auth_id,
		auth_index,
		source,
		latency_ms,
		first_token_latency_ms,
		failed,
		input_tokens,
		output_tokens,
		reasoning_tokens,
		cached_tokens,
		total_tokens
	FROM usage_events
	WHERE %s
	ORDER BY requested_at_ns DESC, id DESC
	LIMIT ? OFFSET ?`, whereClause), itemArgs...)
	if err != nil {
		return result, fmt.Errorf("usage query events: %w", err)
	}
	defer rows.Close()

	loadedRows := 0
	for rows.Next() {
		var (
			requestedAtNS       int64
			provider            string
			model               string
			apiKey              string
			clientIP            string
			requestMethod       string
			requestPath         string
			authID              string
			authIndex           string
			source              string
			latencyMs           int64
			firstTokenLatencyMs int64
			failedInt           int
			inputTokens         int64
			outputTokens        int64
			reasoningTokens     int64
			cachedTokens        int64
			totalTokens         int64
		)
		if err := rows.Scan(
			&requestedAtNS,
			&provider,
			&model,
			&apiKey,
			&clientIP,
			&requestMethod,
			&requestPath,
			&authID,
			&authIndex,
			&source,
			&latencyMs,
			&firstTokenLatencyMs,
			&failedInt,
			&inputTokens,
			&outputTokens,
			&reasoningTokens,
			&cachedTokens,
			&totalTokens,
		); err != nil {
			return result, fmt.Errorf("usage query events scan: %w", err)
		}
		requestMethod = normaliseRequestMethod(requestMethod)
		requestPath = normaliseRequestPath(requestPath)
		if requestPath == "" {
			legacyMethod, legacyPath := parseCompatibilityEndpoint(apiKey)
			if requestMethod == "" {
				requestMethod = legacyMethod
			}
			requestPath = legacyPath
		}
		loadedRows++
		if query.SkipTotal && loadedRows > result.PageSize {
			result.HasMore = true
			continue
		}
		result.Items = append(result.Items, UsageEventItem{
			Timestamp:           time.Unix(0, requestedAtNS).UTC(),
			Provider:            strings.TrimSpace(provider),
			Model:               strings.TrimSpace(model),
			APIKey:              strings.TrimSpace(apiKey),
			ClientIP:            strings.TrimSpace(clientIP),
			RequestMethod:       requestMethod,
			RequestPath:         requestPath,
			AuthID:              strings.TrimSpace(authID),
			AuthIndex:           strings.TrimSpace(authIndex),
			Source:              strings.TrimSpace(source),
			LatencyMs:           latencyMs,
			FirstTokenLatencyMs: firstTokenLatencyMs,
			Failed:              failedInt != 0,
			Tokens: normaliseTokenStats(TokenStats{
				InputTokens:     inputTokens,
				OutputTokens:    outputTokens,
				ReasoningTokens: reasoningTokens,
				CachedTokens:    cachedTokens,
				TotalTokens:     totalTokens,
			}),
		})
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("usage query events iterate: %w", err)
	}
	if query.SkipTotal {
		result.TotalItems = int64(offset + len(result.Items))
		return result, nil
	}
	result.HasMore = int64(offset+len(result.Items)) < result.TotalItems
	return result, nil
}

// LatestSuccessfulAuthByProvider returns the latest successful auth hit observed
// for each provider within the requested time window.
func (s *QueryService) LatestSuccessfulAuthByProvider(
	ctx context.Context,
	start, end *time.Time,
) (map[string]CurrentAuthHit, error) {
	result := make(map[string]CurrentAuthHit)

	repo, window, ok, err := s.repoAndWindow(
		ctx,
		start,
		end,
		time.Duration(s.RetentionDays())*24*time.Hour,
	)
	if err != nil {
		return result, err
	}
	if !ok {
		return result, nil
	}

	rows, err := repo.db.QueryContext(ctx, `SELECT
		e.requested_at_ns,
		LOWER(TRIM(e.provider)) AS provider_key,
		e.auth_id,
		e.auth_index
	FROM usage_events e
	WHERE e.failed = 0
		AND e.requested_at_ns >= ? AND e.requested_at_ns <= ?
		AND LOWER(TRIM(e.provider)) != ''
		AND e.id = (
			SELECT e2.id
			FROM usage_events e2
			WHERE LOWER(TRIM(e2.provider)) = LOWER(TRIM(e.provider))
				AND e2.failed = 0
				AND e2.requested_at_ns >= ? AND e2.requested_at_ns <= ?
			ORDER BY e2.requested_at_ns DESC, e2.id DESC
			LIMIT 1
		)
	ORDER BY e.requested_at_ns DESC, e.id DESC`,
		window.WindowStart.UnixNano(),
		window.WindowEnd.UnixNano(),
		window.WindowStart.UnixNano(),
		window.WindowEnd.UnixNano(),
	)
	if err != nil {
		return result, fmt.Errorf("usage query latest successful auth by provider: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			requestedAtNS int64
			provider      string
			authID        string
			authIndex     string
		)
		if err := rows.Scan(&requestedAtNS, &provider, &authID, &authIndex); err != nil {
			return result, fmt.Errorf("usage query latest successful auth by provider scan: %w", err)
		}

		providerKey := strings.ToLower(strings.TrimSpace(provider))
		if providerKey == "" {
			continue
		}
		if _, exists := result[providerKey]; exists {
			continue
		}
		result[providerKey] = CurrentAuthHit{
			Timestamp: time.Unix(0, requestedAtNS).UTC(),
			Provider:  providerKey,
			AuthID:    strings.TrimSpace(authID),
			AuthIndex: strings.TrimSpace(authIndex),
		}
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("usage query latest successful auth by provider iterate: %w", err)
	}

	return result, nil
}

func (s *QueryService) repoAndWindow(ctx context.Context, start, end *time.Time, defaultDuration time.Duration) (*SQLiteRepository, TimeWindow, bool, error) {
	window := s.resolveWindow(start, end, defaultDuration)
	repo := s.sqliteRepo()
	if repo == nil {
		return nil, window, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := coreusage.FlushDefault(ctx); err != nil {
		return nil, window, false, fmt.Errorf("usage manager flush: %w", err)
	}
	if err := repo.flush(ctx); err != nil {
		return nil, window, false, err
	}
	if window.WindowEnd.Before(window.WindowStart) {
		return repo, window, false, nil
	}
	return repo, window, true, nil
}

func (s *QueryService) sqliteRepo() *SQLiteRepository {
	if s == nil || s.stats == nil {
		return nil
	}
	repo, _ := s.stats.repository().(*SQLiteRepository)
	return repo
}

func (s *QueryService) resolveWindow(start, end *time.Time, defaultDuration time.Duration) TimeWindow {
	now := time.Now().UTC()
	windowEnd := now
	if end != nil && !end.IsZero() {
		windowEnd = end.UTC()
	}
	if windowEnd.After(now) {
		windowEnd = now
	}

	windowStart := retentionCutoff(now, s.RetentionDays())
	if start != nil && !start.IsZero() {
		windowStart = start.UTC()
	} else if defaultDuration > 0 {
		windowStart = windowEnd.Add(-defaultDuration)
	}

	retentionStart := retentionCutoff(now, s.RetentionDays())
	if windowStart.Before(retentionStart) {
		windowStart = retentionStart
	}
	return TimeWindow{WindowStart: windowStart, WindowEnd: windowEnd}
}

func (s *QueryService) prepareChartWindow(
	ctx context.Context,
	query UsageChartQuery,
) (*SQLiteRepository, TimeWindow, string, time.Duration, []string, bool, error) {
	period, defaultDuration, step, labelFormat, err := resolveUsageChartPeriod(query)
	if err != nil {
		return nil, TimeWindow{}, "", 0, nil, false, err
	}

	repo, window, ok, err := s.repoAndWindow(ctx, query.Start, query.End, defaultDuration)
	if err != nil {
		return nil, TimeWindow{}, "", 0, nil, false, err
	}

	labels := buildTimeLabels(window.WindowStart, window.WindowEnd, step, labelFormat)
	return repo, window, period, step, labels, ok, nil
}

func resolveUsageChartPeriod(query UsageChartQuery) (string, time.Duration, time.Duration, string, error) {
	period := strings.ToLower(strings.TrimSpace(query.Period))
	if period == "" {
		period = "day"
	}

	switch period {
	case "hour":
		hours := query.HourWindowHours
		if hours <= 0 {
			hours = defaultHourChartWindowHours
		}
		if hours > 24*31 {
			hours = 24 * 31
		}
		return period, time.Duration(hours) * time.Hour, time.Hour, "2006-01-02T15:00:00Z", nil
	case "day":
		return period, time.Duration(defaultDayChartWindowDays) * 24 * time.Hour, 24 * time.Hour, "2006-01-02", nil
	default:
		return "", 0, 0, "", fmt.Errorf("unsupported chart period %q", period)
	}
}

func newStatusBarData(blockCount int, windowStart time.Time, blockDuration time.Duration) StatusBarData {
	data := StatusBarData{
		Blocks:       make([]StatusBlockState, blockCount),
		BlockDetails: make([]StatusBlockDetail, blockCount),
	}
	seedStatusDetails(data.BlockDetails, blockCount, windowStart, blockDuration)
	for i := range data.Blocks {
		data.Blocks[i] = StatusBlockIdle
	}
	return data
}

func seedStatusDetails(details []StatusBlockDetail, blockCount int, windowStart time.Time, blockDuration time.Duration) {
	for i := 0; i < blockCount && i < len(details); i++ {
		startTime := windowStart.Add(time.Duration(i) * blockDuration)
		details[i] = StatusBlockDetail{
			StartTimeMs: startTime.UnixMilli(),
			EndTimeMs:   startTime.Add(blockDuration).UnixMilli(),
			Rate:        -1,
		}
	}
}

func applyStatusCounts(data *StatusBarData, bucketIdx int, successCount, failureCount int64) {
	if data == nil || bucketIdx < 0 || bucketIdx >= len(data.BlockDetails) {
		return
	}
	data.BlockDetails[bucketIdx].Success += successCount
	data.BlockDetails[bucketIdx].Failure += failureCount
	data.TotalSuccess += successCount
	data.TotalFailure += failureCount
}

func finalizeStatusBar(data *StatusBarData) {
	if data == nil {
		return
	}
	for i := range data.BlockDetails {
		detail := &data.BlockDetails[i]
		total := detail.Success + detail.Failure
		switch {
		case total == 0:
			data.Blocks[i] = StatusBlockIdle
			detail.Rate = -1
		case detail.Failure == 0:
			data.Blocks[i] = StatusBlockSuccess
			detail.Rate = 1
		case detail.Success == 0:
			data.Blocks[i] = StatusBlockFailure
			detail.Rate = 0
		default:
			data.Blocks[i] = StatusBlockMixed
			detail.Rate = float64(detail.Success) / float64(total)
		}
	}
	total := data.TotalSuccess + data.TotalFailure
	if total > 0 {
		data.SuccessRate = (float64(data.TotalSuccess) / float64(total)) * 100
		return
	}
	data.SuccessRate = 100
}

func finalizeHealth(health *ServiceHealthData) {
	if health == nil {
		return
	}
	for i := range health.BlockDetails {
		detail := &health.BlockDetails[i]
		total := detail.Success + detail.Failure
		switch {
		case total == 0:
			health.Blocks[i] = StatusBlockIdle
			detail.Rate = -1
		case detail.Failure == 0:
			health.Blocks[i] = StatusBlockSuccess
			detail.Rate = 1
		case detail.Success == 0:
			health.Blocks[i] = StatusBlockFailure
			detail.Rate = 0
		default:
			health.Blocks[i] = StatusBlockMixed
			detail.Rate = float64(detail.Success) / float64(total)
		}
	}
	total := health.TotalSuccess + health.TotalFailure
	if total > 0 {
		health.SuccessRate = (float64(health.TotalSuccess) / float64(total)) * 100
		return
	}
	health.SuccessRate = 100
}

func buildTimeLabels(start, end time.Time, step time.Duration, format string) []string {
	if step <= 0 || end.Before(start) {
		return nil
	}
	labels := make([]string, 0)
	current := start.UTC()
	for !current.After(end.UTC()) {
		labels = append(labels, current.Format(format))
		current = current.Add(step)
	}
	return labels
}

func normalisePositiveInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func normalisePageSize(pageSize int) int {
	pageSize = normalisePositiveInt(pageSize, defaultEventsPageSize)
	if pageSize > maxEventsPageSize {
		return maxEventsPageSize
	}
	return pageSize
}

// SortedKeys returns a stable sorted list of map keys for query results.
func SortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

var _ = sql.ErrNoRows
