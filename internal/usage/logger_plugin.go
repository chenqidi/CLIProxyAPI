// Package usage provides usage tracking and logging functionality for the CLI Proxy API server.
// It includes plugins for monitoring API usage, token consumption, and other metrics
// to help with observability and billing purposes.
package usage

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

var statisticsEnabled atomic.Bool

func init() {
	statisticsEnabled.Store(true)
	coreusage.RegisterPlugin(NewLoggerPlugin())
}

// LoggerPlugin consumes runtime usage records and forwards them to the shared statistics store.
type LoggerPlugin struct {
	stats *RequestStatistics
}

// NewLoggerPlugin constructs a new logger plugin instance.
func NewLoggerPlugin() *LoggerPlugin { return &LoggerPlugin{stats: defaultRequestStatistics} }

// HandleUsage implements coreusage.Plugin.
func (p *LoggerPlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if !statisticsEnabled.Load() {
		return
	}
	if p == nil || p.stats == nil {
		return
	}
	p.stats.Record(ctx, record)
}

// SetStatisticsEnabled toggles whether usage statistics are recorded.
func SetStatisticsEnabled(enabled bool) { statisticsEnabled.Store(enabled) }

// StatisticsEnabled reports the current recording state.
func StatisticsEnabled() bool { return statisticsEnabled.Load() }

// RequestStatistics maintains compatibility aggregates while allowing a persistent repository backend.
type RequestStatistics struct {
	mu   sync.RWMutex
	repo Repository

	totalRequests int64
	successCount  int64
	failureCount  int64
	totalTokens   int64

	apis map[string]*apiStats

	requestsByDay  map[string]int64
	requestsByHour map[int]int64
	tokensByDay    map[string]int64
	tokensByHour   map[int]int64
}

// apiStats holds aggregated metrics for a single API key.
type apiStats struct {
	TotalRequests int64
	TotalTokens   int64
	Models        map[string]*modelStats
}

// modelStats holds aggregated metrics for a specific model within an API.
type modelStats struct {
	TotalRequests int64
	TotalTokens   int64
	Details       []RequestDetail
}

// RequestDetail stores the timestamp, latency, and token usage for a single request.
type RequestDetail struct {
	Timestamp           time.Time  `json:"timestamp"`
	LatencyMs           int64      `json:"latency_ms"`
	FirstTokenLatencyMs int64      `json:"first_token_latency_ms"`
	Source              string     `json:"source"`
	AuthIndex           string     `json:"auth_index"`
	Tokens              TokenStats `json:"tokens"`
	Failed              bool       `json:"failed"`
}

// TokenStats captures the token usage breakdown for a request.
type TokenStats struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

// StatisticsSnapshot represents an immutable view of the aggregated metrics.
type StatisticsSnapshot struct {
	TotalRequests int64 `json:"total_requests"`
	SuccessCount  int64 `json:"success_count"`
	FailureCount  int64 `json:"failure_count"`
	TotalTokens   int64 `json:"total_tokens"`

	APIs map[string]APISnapshot `json:"apis"`

	RequestsByDay  map[string]int64 `json:"requests_by_day"`
	RequestsByHour map[string]int64 `json:"requests_by_hour"`
	TokensByDay    map[string]int64 `json:"tokens_by_day"`
	TokensByHour   map[string]int64 `json:"tokens_by_hour"`
}

// APISnapshot summarises metrics for a single API key.
type APISnapshot struct {
	TotalRequests int64                    `json:"total_requests"`
	TotalTokens   int64                    `json:"total_tokens"`
	Models        map[string]ModelSnapshot `json:"models"`
}

// ModelSnapshot summarises metrics for a specific model.
type ModelSnapshot struct {
	TotalRequests int64           `json:"total_requests"`
	TotalTokens   int64           `json:"total_tokens"`
	Details       []RequestDetail `json:"details"`
}

var defaultRequestStatistics = NewRequestStatistics()

// GetRequestStatistics returns the shared statistics store.
func GetRequestStatistics() *RequestStatistics { return defaultRequestStatistics }

// NewRequestStatistics constructs an empty statistics store.
func NewRequestStatistics() *RequestStatistics {
	return &RequestStatistics{
		apis:           make(map[string]*apiStats),
		requestsByDay:  make(map[string]int64),
		requestsByHour: make(map[int]int64),
		tokensByDay:    make(map[string]int64),
		tokensByHour:   make(map[int]int64),
	}
}

// SetRepository switches the statistics store to a persistent backend.
func (s *RequestStatistics) SetRepository(repo Repository) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repo = repo
}

func (s *RequestStatistics) repository() Repository {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.repo
}

// Record ingests a new usage record and updates the aggregates or persistent backend.
func (s *RequestStatistics) Record(ctx context.Context, record coreusage.Record) {
	if s == nil {
		return
	}
	if !statisticsEnabled.Load() {
		return
	}

	event := NewUsageEvent(ctx, record)

	if repo := s.repository(); repo != nil {
		if err := repo.Record(context.Background(), event); err != nil {
			log.WithError(err).Warn("usage: failed to persist usage event")
		}
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordUsageEvent(event)
}

func (s *RequestStatistics) recordUsageEvent(event UsageEvent) {
	if s == nil {
		return
	}
	event = normaliseUsageEvent(event)
	totalTokens := event.Tokens.TotalTokens
	if totalTokens < 0 {
		totalTokens = 0
	}

	s.totalRequests++
	if event.Failed {
		s.failureCount++
	} else {
		s.successCount++
	}
	s.totalTokens += totalTokens

	stats, ok := s.apis[event.APIKey]
	if !ok {
		stats = &apiStats{Models: make(map[string]*modelStats)}
		s.apis[event.APIKey] = stats
	} else if stats.Models == nil {
		stats.Models = make(map[string]*modelStats)
	}

	s.updateAPIStats(stats, event.Model, RequestDetail{
		Timestamp:           event.RequestedAt,
		LatencyMs:           event.LatencyMs,
		FirstTokenLatencyMs: event.FirstTokenLatencyMs,
		Source:              event.Source,
		AuthIndex:           event.AuthIndex,
		Tokens:              event.Tokens,
		Failed:              event.Failed,
	})

	dayKey := event.RequestedAt.Format("2006-01-02")
	hourKey := event.RequestedAt.Hour()
	s.requestsByDay[dayKey]++
	s.requestsByHour[hourKey]++
	s.tokensByDay[dayKey] += totalTokens
	s.tokensByHour[hourKey] += totalTokens
}

func (s *RequestStatistics) updateAPIStats(stats *apiStats, model string, detail RequestDetail) {
	stats.TotalRequests++
	stats.TotalTokens += detail.Tokens.TotalTokens
	modelStatsValue, ok := stats.Models[model]
	if !ok {
		modelStatsValue = &modelStats{}
		stats.Models[model] = modelStatsValue
	}
	modelStatsValue.TotalRequests++
	modelStatsValue.TotalTokens += detail.Tokens.TotalTokens
	modelStatsValue.Details = append(modelStatsValue.Details, detail)
}

// Snapshot returns a copy of the aggregated metrics for external consumption.
func (s *RequestStatistics) Snapshot() StatisticsSnapshot {
	result := StatisticsSnapshot{}
	if s == nil {
		return result
	}

	if repo := s.repository(); repo != nil {
		if err := coreusage.FlushDefault(context.Background()); err != nil {
			log.WithError(err).Warn("usage: failed to flush pending usage events before snapshot")
		}
		snapshot, err := repo.Snapshot(context.Background())
		if err != nil {
			log.WithError(err).Warn("usage: failed to build snapshot from repository")
			return StatisticsSnapshot{}
		}
		return snapshot
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *RequestStatistics) snapshotLocked() StatisticsSnapshot {
	result := StatisticsSnapshot{
		TotalRequests:  s.totalRequests,
		SuccessCount:   s.successCount,
		FailureCount:   s.failureCount,
		TotalTokens:    s.totalTokens,
		APIs:           make(map[string]APISnapshot, len(s.apis)),
		RequestsByDay:  make(map[string]int64, len(s.requestsByDay)),
		RequestsByHour: make(map[string]int64, len(s.requestsByHour)),
		TokensByDay:    make(map[string]int64, len(s.tokensByDay)),
		TokensByHour:   make(map[string]int64, len(s.tokensByHour)),
	}

	for apiName, stats := range s.apis {
		apiSnapshot := APISnapshot{
			TotalRequests: stats.TotalRequests,
			TotalTokens:   stats.TotalTokens,
			Models:        make(map[string]ModelSnapshot, len(stats.Models)),
		}
		for modelName, modelStatsValue := range stats.Models {
			requestDetails := make([]RequestDetail, len(modelStatsValue.Details))
			copy(requestDetails, modelStatsValue.Details)
			apiSnapshot.Models[modelName] = ModelSnapshot{
				TotalRequests: modelStatsValue.TotalRequests,
				TotalTokens:   modelStatsValue.TotalTokens,
				Details:       requestDetails,
			}
		}
		result.APIs[apiName] = apiSnapshot
	}

	for k, v := range s.requestsByDay {
		result.RequestsByDay[k] = v
	}
	for hour, v := range s.requestsByHour {
		result.RequestsByHour[formatHour(hour)] = v
	}
	for k, v := range s.tokensByDay {
		result.TokensByDay[k] = v
	}
	for hour, v := range s.tokensByHour {
		result.TokensByHour[formatHour(hour)] = v
	}

	return result
}

// MergeResult reports how many imported request details were added or skipped.
type MergeResult struct {
	Added   int64 `json:"added"`
	Skipped int64 `json:"skipped"`
}

// MergeSnapshot merges an exported statistics snapshot into the current store.
// Existing data is preserved and duplicate request details are skipped.
func (s *RequestStatistics) MergeSnapshot(snapshot StatisticsSnapshot) MergeResult {
	result := MergeResult{}
	if s == nil {
		return result
	}

	if repo := s.repository(); repo != nil {
		merged, err := repo.ImportSnapshot(context.Background(), snapshot)
		if err != nil {
			log.WithError(err).Warn("usage: failed to import snapshot into repository")
			return MergeResult{}
		}
		return merged
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[string]struct{})
	for apiName, stats := range s.apis {
		if stats == nil {
			continue
		}
		for modelName, modelStatsValue := range stats.Models {
			if modelStatsValue == nil {
				continue
			}
			for _, detail := range modelStatsValue.Details {
				seen[dedupKey(apiName, modelName, detail)] = struct{}{}
			}
		}
	}

	for apiName, apiSnapshot := range snapshot.APIs {
		for modelName, modelSnapshot := range apiSnapshot.Models {
			for _, detail := range modelSnapshot.Details {
				event, ok := ImportedUsageEvent(apiName, modelName, detail)
				if !ok {
					continue
				}
				if _, exists := seen[event.DedupKey]; exists {
					result.Skipped++
					continue
				}
				seen[event.DedupKey] = struct{}{}
				s.recordUsageEvent(event)
				result.Added++
			}
		}
	}

	return result
}

func dedupKey(apiName, modelName string, detail RequestDetail) string {
	return buildDedupKey(apiName, modelName, detail.Timestamp, detail.Source, detail.AuthIndex, detail.Failed, detail.Tokens)
}

func resolveRequestIdentity(ctx context.Context) (string, string) {
	if ctx != nil {
		if ginCtx, ok := ctx.Value("gin").(*gin.Context); ok && ginCtx != nil {
			path := ginCtx.FullPath()
			if path == "" && ginCtx.Request != nil {
				path = ginCtx.Request.URL.Path
			}
			method := ""
			if ginCtx.Request != nil {
				method = ginCtx.Request.Method
			}
			return normaliseRequestMethod(method), normaliseRequestPath(path)
		}
	}
	return "", ""
}

func resolveAPIIdentifier(ctx context.Context, record coreusage.Record) string {
	if method, path := resolveRequestIdentity(ctx); path != "" {
		return formatRequestIdentity(method, path)
	}
	if record.Provider != "" {
		return record.Provider
	}
	return "unknown"
}

func resolveSuccess(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil {
		return true
	}
	status := ginCtx.Writer.Status()
	if status == 0 {
		return true
	}
	return status < httpStatusBadRequest
}

const httpStatusBadRequest = 400

func normaliseDetail(detail coreusage.Detail) TokenStats {
	tokens := TokenStats{
		InputTokens:     detail.InputTokens,
		OutputTokens:    detail.OutputTokens,
		ReasoningTokens: detail.ReasoningTokens,
		CachedTokens:    detail.CachedTokens,
		TotalTokens:     detail.TotalTokens,
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = detail.InputTokens + detail.OutputTokens + detail.ReasoningTokens
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = detail.InputTokens + detail.OutputTokens + detail.ReasoningTokens + detail.CachedTokens
	}
	return tokens
}

func normaliseTokenStats(tokens TokenStats) TokenStats {
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens
	}
	if tokens.TotalTokens == 0 {
		tokens.TotalTokens = tokens.InputTokens + tokens.OutputTokens + tokens.ReasoningTokens + tokens.CachedTokens
	}
	return tokens
}

func normaliseLatency(latency time.Duration) int64 {
	if latency <= 0 {
		return 0
	}
	return latency.Milliseconds()
}

func formatHour(hour int) string {
	if hour < 0 {
		hour = 0
	}
	hour = hour % 24
	return fmt.Sprintf("%02d", hour)
}
