package usage

import (
	"context"
	"testing"
	"time"
)

func seedUsageEvent(t *testing.T, repo *SQLiteRepository, event UsageEvent) {
	t.Helper()
	if err := repo.Record(context.Background(), event); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
}

func TestQueryServiceSummaryChartsAndEvents(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	stats := NewRequestStatistics()
	stats.SetRepository(repo)
	service := NewQueryService(stats)

	now := time.Now().UTC().Truncate(time.Minute)
	seedUsageEvent(t, repo, UsageEvent{
		RequestedAt:   now.Add(-20 * time.Minute),
		Provider:      "codex",
		Model:         "gpt-5.4",
		APIKey:        "key-a",
		RequestMethod: "POST",
		RequestPath:   "/v1/chat/completions",
		AuthIndex:     "1",
		Source:        "source-a",
		LatencyMs:     1200,
		Tokens:        TokenStats{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
	})
	seedUsageEvent(t, repo, UsageEvent{
		RequestedAt:   now.Add(-10 * time.Minute),
		Provider:      "codex",
		Model:         "gpt-5.4-mini",
		APIKey:        "key-b",
		RequestMethod: "POST",
		RequestPath:   "/v1/messages",
		AuthIndex:     "2",
		Source:        "source-b",
		Failed:        true,
		LatencyMs:     800,
		Tokens:        TokenStats{InputTokens: 5, OutputTokens: 5, CachedTokens: 2, ReasoningTokens: 1, TotalTokens: 11},
	})
	seedUsageEvent(t, repo, UsageEvent{
		RequestedAt:   now.Add(-26 * time.Hour),
		Provider:      "codex",
		Model:         "gpt-5.4",
		APIKey:        "key-a",
		RequestMethod: "POST",
		RequestPath:   "/v1/chat/completions",
		AuthIndex:     "1",
		Source:        "source-a",
		LatencyMs:     600,
		Tokens:        TokenStats{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
	})

	start := now.Add(-48 * time.Hour)
	end := now
	summary, err := service.Summary(context.Background(), &start, &end)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.TotalRequests != 3 {
		t.Fatalf("TotalRequests = %d, want 3", summary.TotalRequests)
	}
	if summary.SuccessCount != 2 || summary.FailureCount != 1 {
		t.Fatalf("success/failure = %d/%d, want 2/1", summary.SuccessCount, summary.FailureCount)
	}
	if summary.TotalTokens != 44 {
		t.Fatalf("TotalTokens = %d, want 44", summary.TotalTokens)
	}
	if summary.CachedTokens != 2 || summary.ReasoningTokens != 1 {
		t.Fatalf("cached/reasoning = %d/%d, want 2/1", summary.CachedTokens, summary.ReasoningTokens)
	}
	if summary.DistinctSources != 2 || summary.DistinctAuthIndex != 2 || summary.DistinctModels != 2 {
		t.Fatalf("distinct counts = %+v, want sources=2 auth=2 models=2", summary)
	}
	if summary.RequestsLast30m != 2 || summary.TokensLast30m != 41 {
		t.Fatalf("last30m requests/tokens = %d/%d, want 2/41", summary.RequestsLast30m, summary.TokensLast30m)
	}

	chartStart := now.Add(-47 * time.Hour)
	chart, err := service.Charts(context.Background(), UsageChartQuery{
		Start:  &chartStart,
		End:    &end,
		Period: "day",
		Metric: "tokens",
	})
	if err != nil {
		t.Fatalf("Charts() error = %v", err)
	}
	if chart.Period != "day" || chart.Metric != "tokens" {
		t.Fatalf("chart meta = %+v", chart)
	}
	if len(chart.Labels) == 0 {
		t.Fatal("expected non-empty chart labels")
	}
	if got := len(chart.DataByModel["gpt-5.4"]); got != len(chart.Labels) {
		t.Fatalf("gpt-5.4 series len = %d, want %d", got, len(chart.Labels))
	}

	tokenChart, err := service.TokenCharts(context.Background(), UsageChartQuery{
		Start:  &chartStart,
		End:    &end,
		Period: "day",
	})
	if err != nil {
		t.Fatalf("TokenCharts() error = %v", err)
	}
	if tokenChart.Period != "day" {
		t.Fatalf("tokenChart period = %q, want %q", tokenChart.Period, "day")
	}
	if got := len(tokenChart.DataByModel["gpt-5.4"]); got != len(tokenChart.Labels) {
		t.Fatalf("token chart gpt-5.4 series len = %d, want %d", got, len(tokenChart.Labels))
	}
	var gpt54Total int64
	for _, bucket := range tokenChart.DataByModel["gpt-5.4"] {
		gpt54Total += bucket.TotalTokens
	}
	if gpt54Total != 33 {
		t.Fatalf("token chart gpt-5.4 total_tokens = %d, want 33", gpt54Total)
	}

	failed := true
	events, err := service.Events(context.Background(), UsageEventsQuery{
		Start:    &start,
		End:      &end,
		Page:     1,
		PageSize: 10,
		Failed:   &failed,
	})
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if events.TotalItems != 1 {
		t.Fatalf("TotalItems = %d, want 1", events.TotalItems)
	}
	if len(events.Items) != 1 || !events.Items[0].Failed {
		t.Fatalf("events items = %+v, want single failed event", events.Items)
	}
	if events.Items[0].Source != "source-b" {
		t.Fatalf("event source = %q, want %q", events.Items[0].Source, "source-b")
	}
	if events.Items[0].RequestMethod != "POST" || events.Items[0].RequestPath != "/v1/messages" {
		t.Fatalf("event request identity = %+v, want POST /v1/messages", events.Items[0])
	}
}

func TestQueryServiceEventsSupportsLegacyEndpointFallback(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	stats := NewRequestStatistics()
	stats.SetRepository(repo)
	service := NewQueryService(stats)

	now := time.Now().UTC().Truncate(time.Second)
	legacyTimestamp := now.Add(-5 * time.Minute)
	_, err := repo.db.ExecContext(context.Background(), `INSERT INTO usage_events (
		requested_at_ns,
		provider,
		model,
		api_key,
		request_method,
		request_path,
		auth_id,
		auth_index,
		source,
		latency_ms,
		failed,
		input_tokens,
		output_tokens,
		reasoning_tokens,
		cached_tokens,
		total_tokens,
		dedup_key
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		legacyTimestamp.UnixNano(),
		"codex",
		"gpt-5.4",
		"POST /v1/responses",
		"",
		"",
		"auth-legacy",
		"9",
		"legacy-source",
		640,
		0,
		4,
		6,
		0,
		0,
		10,
		"legacy-dedup",
	)
	if err != nil {
		t.Fatalf("insert legacy usage event: %v", err)
	}

	start := now.Add(-24 * time.Hour)
	end := now
	events, err := service.Events(context.Background(), UsageEventsQuery{
		Start:         &start,
		End:           &end,
		Page:          1,
		PageSize:      10,
		RequestMethod: "POST",
		RequestPath:   "/v1/responses",
	})
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if events.TotalItems != 1 || len(events.Items) != 1 {
		t.Fatalf("events = %+v, want single legacy event", events)
	}
	if events.Items[0].RequestMethod != "POST" || events.Items[0].RequestPath != "/v1/responses" {
		t.Fatalf("legacy request identity = %+v, want POST /v1/responses", events.Items[0])
	}
}

func TestQueryServiceStatusAndHealth(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	stats := NewRequestStatistics()
	stats.SetRepository(repo)
	service := NewQueryService(stats)

	now := time.Now().UTC().Truncate(time.Minute)
	seedUsageEvent(t, repo, UsageEvent{
		RequestedAt: now.Add(-5 * time.Minute),
		Model:       "gpt-5.4",
		APIKey:      "key-a",
		AuthIndex:   "7",
		Source:      "source-a",
		Tokens:      TokenStats{TotalTokens: 10},
	})
	seedUsageEvent(t, repo, UsageEvent{
		RequestedAt: now.Add(-15 * time.Minute),
		Model:       "gpt-5.4",
		APIKey:      "key-a",
		AuthIndex:   "7",
		Source:      "source-a",
		Failed:      true,
		Tokens:      TokenStats{TotalTokens: 5},
	})
	seedUsageEvent(t, repo, UsageEvent{
		RequestedAt: now.Add(-2 * time.Hour),
		Model:       "gpt-5.4-mini",
		APIKey:      "key-b",
		AuthIndex:   "9",
		Source:      "source-b",
		Tokens:      TokenStats{TotalTokens: 3},
	})

	statusStart := now.Add(-200 * time.Minute)
	status, err := service.Status(context.Background(), &statusStart, &now)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if len(status.Service.Blocks) != defaultStatusBlockCount {
		t.Fatalf("service blocks len = %d, want %d", len(status.Service.Blocks), defaultStatusBlockCount)
	}
	if status.Service.TotalSuccess != 2 || status.Service.TotalFailure != 1 {
		t.Fatalf("service totals = %+v, want success=2 failure=1", status.Service)
	}
	if status.BySource["source-a"].TotalSuccess != 1 || status.BySource["source-a"].TotalFailure != 1 {
		t.Fatalf("source-a status = %+v, want success=1 failure=1", status.BySource["source-a"])
	}
	if status.ByAuthIndex["7"].TotalSuccess != 1 || status.ByAuthIndex["7"].TotalFailure != 1 {
		t.Fatalf("auth 7 status = %+v, want success=1 failure=1", status.ByAuthIndex["7"])
	}

	healthStart := now.Add(-(time.Duration(defaultHealthRows*defaultHealthCols) * defaultHealthBlockDuration))
	health, err := service.Health(context.Background(), &healthStart, &now)
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if len(health.Blocks) != defaultHealthRows*defaultHealthCols {
		t.Fatalf("health blocks len = %d, want %d", len(health.Blocks), defaultHealthRows*defaultHealthCols)
	}
	if health.TotalSuccess != 3-1 || health.TotalFailure != 1 {
		t.Fatalf("health totals = success %d failure %d, want 2/1", health.TotalSuccess, health.TotalFailure)
	}
	if health.Rows != defaultHealthRows || health.Cols != defaultHealthCols {
		t.Fatalf("health rows/cols = %d/%d, want %d/%d", health.Rows, health.Cols, defaultHealthRows, defaultHealthCols)
	}
}
