package usage

import (
	"context"
	"fmt"
	"strings"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

// UsageEvent represents a normalized usage event ready for persistence.
type UsageEvent struct {
	RequestedAt         time.Time
	Provider            string
	Model               string
	APIKey              string
	ClientIP            string
	RequestMethod       string
	RequestPath         string
	AuthID              string
	AuthIndex           string
	Source              string
	LatencyMs           int64
	FirstTokenLatencyMs int64
	Failed              bool
	Tokens              TokenStats
	DedupKey            string
}

// Repository persists usage events and can reconstruct compatibility snapshots.
type Repository interface {
	Record(ctx context.Context, event UsageEvent) error
	Snapshot(ctx context.Context) (StatisticsSnapshot, error)
	ImportSnapshot(ctx context.Context, snapshot StatisticsSnapshot) (MergeResult, error)
	Close(ctx context.Context) error
}

// NewUsageEvent converts a runtime usage record into a normalized persisted event.
func NewUsageEvent(ctx context.Context, record coreusage.Record) UsageEvent {
	timestamp := record.RequestedAt.UTC()
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	requestMethod, requestPath := resolveRequestIdentity(ctx)
	apiKey := strings.TrimSpace(record.APIKey)
	if apiKey == "" {
		apiKey = formatRequestIdentity(requestMethod, requestPath)
		if apiKey == "" {
			apiKey = resolveAPIIdentifier(ctx, record)
		}
	}

	failed := record.Failed
	if !failed {
		failed = !resolveSuccess(ctx)
	}

	event := UsageEvent{
		RequestedAt:         timestamp,
		Provider:            strings.TrimSpace(record.Provider),
		Model:               strings.TrimSpace(record.Model),
		APIKey:              apiKey,
		ClientIP:            firstNonEmpty(record.ClientIP, resolveClientIP(ctx)),
		RequestMethod:       requestMethod,
		RequestPath:         requestPath,
		AuthID:              strings.TrimSpace(record.AuthID),
		AuthIndex:           strings.TrimSpace(record.AuthIndex),
		Source:              strings.TrimSpace(record.Source),
		LatencyMs:           normaliseLatency(record.Latency),
		FirstTokenLatencyMs: normaliseLatency(record.FirstTokenLatency),
		Failed:              failed,
		Tokens:              normaliseDetail(record.Detail),
	}
	return normaliseUsageEvent(event)
}

// ImportedUsageEvent converts a compatibility snapshot detail into a normalized persisted event.
func ImportedUsageEvent(apiName, modelName string, detail RequestDetail) (UsageEvent, bool) {
	apiName = strings.TrimSpace(apiName)
	if apiName == "" {
		return UsageEvent{}, false
	}
	requestMethod, requestPath := parseCompatibilityEndpoint(apiName)

	event := UsageEvent{
		RequestedAt:         detail.Timestamp.UTC(),
		Model:               strings.TrimSpace(modelName),
		APIKey:              apiName,
		ClientIP:            strings.TrimSpace(detail.ClientIP),
		RequestMethod:       requestMethod,
		RequestPath:         requestPath,
		AuthIndex:           strings.TrimSpace(detail.AuthIndex),
		Source:              strings.TrimSpace(detail.Source),
		LatencyMs:           detail.LatencyMs,
		FirstTokenLatencyMs: detail.FirstTokenLatencyMs,
		Failed:              detail.Failed,
		Tokens:              normaliseTokenStats(detail.Tokens),
	}
	if event.RequestedAt.IsZero() {
		event.RequestedAt = time.Now().UTC()
	}
	return normaliseUsageEvent(event), true
}

func normaliseUsageEvent(event UsageEvent) UsageEvent {
	event.RequestedAt = event.RequestedAt.UTC()
	if event.RequestedAt.IsZero() {
		event.RequestedAt = time.Now().UTC()
	}

	event.Provider = strings.TrimSpace(event.Provider)
	if event.Provider == "" {
		event.Provider = "unknown"
	}

	event.Model = strings.TrimSpace(event.Model)
	if event.Model == "" {
		event.Model = "unknown"
	}

	event.APIKey = strings.TrimSpace(event.APIKey)
	if event.APIKey == "" {
		event.APIKey = "unknown"
	}
	event.ClientIP = strings.TrimSpace(event.ClientIP)
	event.RequestMethod = normaliseRequestMethod(event.RequestMethod)
	event.RequestPath = normaliseRequestPath(event.RequestPath)
	if event.RequestPath == "" {
		method, path := parseCompatibilityEndpoint(event.APIKey)
		if event.RequestMethod == "" {
			event.RequestMethod = method
		}
		event.RequestPath = path
	}

	event.AuthID = strings.TrimSpace(event.AuthID)
	event.AuthIndex = strings.TrimSpace(event.AuthIndex)
	event.Source = strings.TrimSpace(event.Source)
	if event.LatencyMs < 0 {
		event.LatencyMs = 0
	}
	if event.FirstTokenLatencyMs < 0 {
		event.FirstTokenLatencyMs = 0
	}
	if event.Tokens.TotalTokens < 0 {
		event.Tokens.TotalTokens = 0
	}
	event.Tokens = normaliseTokenStats(event.Tokens)
	event.DedupKey = buildDedupKey(event.APIKey, event.Model, event.RequestedAt, event.Source, event.AuthIndex, event.Failed, event.Tokens)
	return event
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normaliseRequestMethod(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normaliseRequestPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if idx := strings.Index(trimmed, "?"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if trimmed == "" {
		return ""
	}
	if trimmed != "/" {
		trimmed = strings.TrimRight(trimmed, "/")
		if trimmed == "" {
			return "/"
		}
	}
	return trimmed
}

func isHTTPMethod(value string) bool {
	switch normaliseRequestMethod(value) {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD":
		return true
	default:
		return false
	}
}

func formatRequestIdentity(method, path string) string {
	path = normaliseRequestPath(path)
	if path == "" {
		return ""
	}
	method = normaliseRequestMethod(method)
	if method != "" {
		return method + " " + path
	}
	return path
}

func parseCompatibilityEndpoint(value string) (string, string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ""
	}

	if strings.HasPrefix(trimmed, "/") {
		return "", normaliseRequestPath(trimmed)
	}

	parts := strings.SplitN(trimmed, " ", 2)
	if len(parts) != 2 {
		return "", ""
	}
	method := normaliseRequestMethod(parts[0])
	if !isHTTPMethod(method) {
		return "", ""
	}
	path := normaliseRequestPath(parts[1])
	if path == "" {
		return "", ""
	}
	return method, path
}

func buildLegacyEndpointKeys(method, path string) []string {
	path = normaliseRequestPath(path)
	if path == "" {
		return nil
	}
	candidates := []string{path}
	if formatted := formatRequestIdentity(method, path); formatted != "" && formatted != path {
		candidates = append([]string{formatted}, candidates...)
	}
	return candidates
}

func buildDedupKey(apiName, modelName string, timestamp time.Time, source, authIndex string, failed bool, tokens TokenStats) string {
	tokens = normaliseTokenStats(tokens)
	return fmt.Sprintf(
		"%s|%s|%s|%s|%s|%t|%d|%d|%d|%d|%d",
		strings.TrimSpace(apiName),
		strings.TrimSpace(modelName),
		timestamp.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(source),
		strings.TrimSpace(authIndex),
		failed,
		tokens.InputTokens,
		tokens.OutputTokens,
		tokens.ReasoningTokens,
		tokens.CachedTokens,
		tokens.TotalTokens,
	)
}
