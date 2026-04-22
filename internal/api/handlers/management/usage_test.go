package management

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func newUsageTestHandler(t *testing.T) (*Handler, *usage.SQLiteRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo, err := usage.NewSQLiteRepository(context.Background(), usage.SQLiteRepositoryConfig{
		Path:              filepath.Join(t.TempDir(), "usage.sqlite"),
		BatchSize:         1,
		FlushInterval:     10 * time.Millisecond,
		RetentionDays:     30,
		RetentionInterval: time.Hour,
		QueueSize:         32,
	})
	if err != nil {
		t.Fatalf("NewSQLiteRepository() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := repo.Close(ctx); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	stats := usage.NewRequestStatistics()
	stats.SetRepository(repo)
	return &Handler{
		usageStats:        stats,
		usageQueryService: usage.NewQueryService(stats),
	}, repo
}

func seedUsageHandlerEvent(t *testing.T, repo *usage.SQLiteRepository, event usage.UsageEvent) {
	t.Helper()
	if err := repo.Record(context.Background(), event); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
}

func TestGetUsageSummaryAndEvents(t *testing.T) {
	h, repo := newUsageTestHandler(t)
	now := time.Now().UTC().Truncate(time.Second)

	seedUsageHandlerEvent(t, repo, usage.UsageEvent{
		RequestedAt:   now.Add(-20 * time.Minute),
		Provider:      "codex",
		Model:         "gpt-5.4",
		APIKey:        "key-a",
		RequestMethod: "POST",
		RequestPath:   "/v1/chat/completions",
		AuthID:        "auth-a",
		AuthIndex:     "1",
		Source:        "source-a",
		LatencyMs:     1200,
		Tokens: usage.TokenStats{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})
	seedUsageHandlerEvent(t, repo, usage.UsageEvent{
		RequestedAt:   now.Add(-10 * time.Minute),
		Provider:      "codex",
		Model:         "gpt-5.4-mini",
		APIKey:        "key-b",
		RequestMethod: "POST",
		RequestPath:   "/v1/messages",
		AuthID:        "auth-b",
		AuthIndex:     "2",
		Source:        "source-b",
		LatencyMs:     800,
		Failed:        true,
		Tokens: usage.TokenStats{
			InputTokens:     5,
			OutputTokens:    5,
			CachedTokens:    2,
			ReasoningTokens: 1,
			TotalTokens:     11,
		},
	})
	seedUsageHandlerEvent(t, repo, usage.UsageEvent{
		RequestedAt:   now.Add(-5 * time.Minute),
		Provider:      "codex",
		Model:         "gpt-5.4-mini",
		APIKey:        "key-c",
		RequestMethod: "POST",
		RequestPath:   "/v1/messages",
		AuthID:        "auth-c",
		AuthIndex:     "2",
		Source:        "source-b",
		LatencyMs:     700,
		Failed:        true,
		Tokens: usage.TokenStats{
			InputTokens:  3,
			OutputTokens: 4,
			TotalTokens:  7,
		},
	})

	summaryRec := httptest.NewRecorder()
	summaryCtx, _ := gin.CreateTestContext(summaryRec)
	summaryReq := httptest.NewRequest(
		http.MethodGet,
		"/v0/management/usage/summary?start="+now.Add(-24*time.Hour).Format(time.RFC3339Nano)+"&end="+now.Format(time.RFC3339Nano),
		nil,
	)
	summaryCtx.Request = summaryReq

	h.GetUsageSummary(summaryCtx)

	if summaryRec.Code != http.StatusOK {
		t.Fatalf("summary status = %d, want %d, body=%s", summaryRec.Code, http.StatusOK, summaryRec.Body.String())
	}

	var summary usage.UsageSummary
	if err := json.Unmarshal(summaryRec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	if summary.TotalRequests != 3 || summary.SuccessCount != 1 || summary.FailureCount != 2 {
		t.Fatalf("summary counts = %+v, want total=3 success=1 failure=2", summary)
	}
	if summary.TotalTokens != 48 {
		t.Fatalf("summary total_tokens = %d, want 48", summary.TotalTokens)
	}
	if summary.RetentionDays != 30 {
		t.Fatalf("summary retention_days = %d, want 30", summary.RetentionDays)
	}

	eventsRec := httptest.NewRecorder()
	eventsCtx, _ := gin.CreateTestContext(eventsRec)
	eventsReq := httptest.NewRequest(
		http.MethodGet,
		"/v0/management/usage/events?start="+now.Add(-24*time.Hour).Format(time.RFC3339Nano)+"&end="+now.Format(time.RFC3339Nano)+"&failed=true&source=source-b&request_method=POST&request_path=/v1/messages&page=1&page_size=1",
		nil,
	)
	eventsCtx.Request = eventsReq

	h.GetUsageEvents(eventsCtx)

	if eventsRec.Code != http.StatusOK {
		t.Fatalf("events status = %d, want %d, body=%s", eventsRec.Code, http.StatusOK, eventsRec.Body.String())
	}

	var events usage.UsageEventsPage
	if err := json.Unmarshal(eventsRec.Body.Bytes(), &events); err != nil {
		t.Fatalf("unmarshal events: %v", err)
	}
	if events.TotalItems != 2 {
		t.Fatalf("events total_items = %d, want 2", events.TotalItems)
	}
	if !events.HasMore {
		t.Fatalf("events has_more = false, want true")
	}
	if len(events.Items) != 1 {
		t.Fatalf("events items len = %d, want 1", len(events.Items))
	}
	if !events.Items[0].Failed || events.Items[0].Source != "source-b" || events.Items[0].Model != "gpt-5.4-mini" {
		t.Fatalf("unexpected event item: %+v", events.Items[0])
	}
	if events.Items[0].RequestMethod != "POST" || events.Items[0].RequestPath != "/v1/messages" {
		t.Fatalf("unexpected request identity: %+v", events.Items[0])
	}
}

func TestGetUsageEventsRejectsInvalidPageSize(t *testing.T) {
	h, _ := newUsageTestHandler(t)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage/events?page_size=oops", nil)
	ctx.Request = req

	h.GetUsageEvents(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestUsageExportImport(t *testing.T) {
	_, repo := newUsageTestHandler(t)
	now := time.Now().UTC().Truncate(time.Second)

	seedUsageHandlerEvent(t, repo, usage.UsageEvent{
		RequestedAt: now.Add(-2 * time.Hour),
		Provider:    "codex",
		Model:       "gpt-5.4",
		APIKey:      "bridge-key",
		AuthIndex:   "1",
		Source:      "bridge-source",
		LatencyMs:   900,
		Tokens: usage.TokenStats{
			InputTokens:  5,
			OutputTokens: 10,
			TotalTokens:  15,
		},
	})
	seedUsageHandlerEvent(t, repo, usage.UsageEvent{
		RequestedAt: now.Add(-45 * 24 * time.Hour),
		Provider:    "codex",
		Model:       "gpt-5.4",
		APIKey:      "expired-key",
		AuthIndex:   "2",
		Source:      "expired-source",
		LatencyMs:   600,
		Tokens: usage.TokenStats{
			InputTokens:  1,
			OutputTokens: 2,
			TotalTokens:  3,
		},
	})

	importHandler, _ := newUsageTestHandler(t)
	importPayload := usageImportPayload{
		Version: 2,
		Usage: usage.StatisticsSnapshot{
			APIs: map[string]usage.APISnapshot{
				"import-key": {
					Models: map[string]usage.ModelSnapshot{
						"gpt-5.4": {
							Details: []usage.RequestDetail{
								{
									Timestamp: now.Add(-3 * time.Hour),
									LatencyMs: 1100,
									Source:    "recent-import",
									AuthIndex: "7",
									Tokens: usage.TokenStats{
										InputTokens:  10,
										OutputTokens: 20,
										TotalTokens:  30,
									},
								},
								{
									Timestamp: now.Add(-40 * 24 * time.Hour),
									LatencyMs: 2100,
									Source:    "expired-import",
									AuthIndex: "8",
									Tokens: usage.TokenStats{
										InputTokens:  1,
										OutputTokens: 2,
										TotalTokens:  3,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	body, err := json.Marshal(importPayload)
	if err != nil {
		t.Fatalf("marshal import payload: %v", err)
	}

	importRec := httptest.NewRecorder()
	importCtx, _ := gin.CreateTestContext(importRec)
	importReq := httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", bytes.NewReader(body))
	importReq.Header.Set("Content-Type", "application/json")
	importCtx.Request = importReq

	importHandler.ImportUsageStatistics(importCtx)

	if importRec.Code != http.StatusOK {
		t.Fatalf("import status = %d, want %d, body=%s", importRec.Code, http.StatusOK, importRec.Body.String())
	}

	var importResp struct {
		Added         int64     `json:"added"`
		Skipped       int64     `json:"skipped"`
		TotalRequests int64     `json:"total_requests"`
		RetentionDays int       `json:"retention_days"`
		WindowStart   time.Time `json:"window_start"`
		WindowEnd     time.Time `json:"window_end"`
	}
	if err := json.Unmarshal(importRec.Body.Bytes(), &importResp); err != nil {
		t.Fatalf("unmarshal import response: %v", err)
	}
	if importResp.Added != 1 || importResp.Skipped != 1 {
		t.Fatalf("import response = %+v, want added=1 skipped=1", importResp)
	}
	if importResp.TotalRequests != 1 {
		t.Fatalf("import total_requests = %d, want 1", importResp.TotalRequests)
	}
	if importResp.RetentionDays != 30 {
		t.Fatalf("import retention_days = %d, want 30", importResp.RetentionDays)
	}
	if importResp.WindowEnd.Before(importResp.WindowStart) {
		t.Fatalf("invalid import window: start=%s end=%s", importResp.WindowStart, importResp.WindowEnd)
	}

	exportRec := httptest.NewRecorder()
	exportCtx, _ := gin.CreateTestContext(exportRec)
	exportReq := httptest.NewRequest(http.MethodGet, "/v0/management/usage/export", nil)
	exportCtx.Request = exportReq

	importHandler.ExportUsageStatistics(exportCtx)

	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want %d, body=%s", exportRec.Code, http.StatusOK, exportRec.Body.String())
	}

	var exportResp usageExportPayload
	if err := json.Unmarshal(exportRec.Body.Bytes(), &exportResp); err != nil {
		t.Fatalf("unmarshal export response: %v", err)
	}
	if exportResp.Version != 2 {
		t.Fatalf("export version = %d, want 2", exportResp.Version)
	}
	if exportResp.RetentionDays != 30 {
		t.Fatalf("export retention_days = %d, want 30", exportResp.RetentionDays)
	}
	if exportResp.Usage.TotalRequests != 1 {
		t.Fatalf("export total_requests = %d, want 1", exportResp.Usage.TotalRequests)
	}
}
