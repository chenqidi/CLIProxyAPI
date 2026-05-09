package usage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func newTestSQLiteRepository(t *testing.T) *SQLiteRepository {
	t.Helper()
	repo, err := NewSQLiteRepository(context.Background(), SQLiteRepositoryConfig{
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
	return repo
}

func TestSQLiteRepositorySnapshotAndRetention(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC().Truncate(time.Millisecond)

	recent := UsageEvent{
		RequestedAt:         now,
		Provider:            "codex",
		Model:               "gpt-5.4",
		APIKey:              "test-key",
		AuthID:              "auth-1",
		AuthIndex:           "0",
		Source:              "user@example.com",
		LatencyMs:           1500,
		FirstTokenLatencyMs: 420,
		Tokens: TokenStats{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	}
	expired := recent
	expired.RequestedAt = now.Add(-31 * 24 * time.Hour)
	expired.Source = "expired@example.com"

	if err := repo.Record(context.Background(), recent); err != nil {
		t.Fatalf("Record(recent) error = %v", err)
	}
	if err := repo.Record(context.Background(), expired); err != nil {
		t.Fatalf("Record(expired) error = %v", err)
	}
	if err := repo.flush(context.Background()); err != nil {
		t.Fatalf("flush() error = %v", err)
	}
	if err := repo.pruneExpired(context.Background(), now); err != nil {
		t.Fatalf("pruneExpired() error = %v", err)
	}

	snapshot, err := repo.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want 1", snapshot.TotalRequests)
	}
	if snapshot.TotalTokens != 30 {
		t.Fatalf("TotalTokens = %d, want 30", snapshot.TotalTokens)
	}
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].Source != "user@example.com" {
		t.Fatalf("detail source = %q, want %q", details[0].Source, "user@example.com")
	}
	if details[0].LatencyMs != 1500 {
		t.Fatalf("detail latency = %d, want 1500", details[0].LatencyMs)
	}
	if details[0].FirstTokenLatencyMs != 420 {
		t.Fatalf("detail first_token_latency_ms = %d, want 420", details[0].FirstTokenLatencyMs)
	}
}

func TestSQLiteRepositoryImportSnapshotDeduplicatesAgainstExistingRows(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	timestamp := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	snapshot := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 2500,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}

	result, err := repo.ImportSnapshot(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("ImportSnapshot(first) error = %v", err)
	}
	if result.Added != 1 || result.Skipped != 0 {
		t.Fatalf("first import result = %+v, want added=1 skipped=0", result)
	}

	result, err = repo.ImportSnapshot(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("ImportSnapshot(second) error = %v", err)
	}
	if result.Added != 0 || result.Skipped != 1 {
		t.Fatalf("second import result = %+v, want added=0 skipped=1", result)
	}

	compatSnapshot, err := repo.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	details := compatSnapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
}

func TestSQLiteRepositoryImportSnapshotSkipsExpiredRows(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{
							{
								Timestamp: now.Add(-2 * time.Hour),
								LatencyMs: 1200,
								Source:    "recent@example.com",
								AuthIndex: "0",
								Tokens: TokenStats{
									InputTokens:  10,
									OutputTokens: 20,
									TotalTokens:  30,
								},
							},
							{
								Timestamp: now.Add(-45 * 24 * time.Hour),
								LatencyMs: 2400,
								Source:    "expired@example.com",
								AuthIndex: "1",
								Tokens: TokenStats{
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
	}

	result, err := repo.ImportSnapshot(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("ImportSnapshot() error = %v", err)
	}
	if result.Added != 1 || result.Skipped != 1 {
		t.Fatalf("import result = %+v, want added=1 skipped=1", result)
	}

	compatSnapshot, err := repo.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if compatSnapshot.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want 1", compatSnapshot.TotalRequests)
	}
	details := compatSnapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].Source != "recent@example.com" {
		t.Fatalf("detail source = %q, want %q", details[0].Source, "recent@example.com")
	}
}

func TestLoggerPluginWritesToRepositoryWithCancelledContext(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	stats := NewRequestStatistics()
	stats.SetRepository(repo)
	plugin := &LoggerPlugin{stats: stats}

	SetStatisticsEnabled(true)
	t.Cleanup(func() { SetStatisticsEnabled(true) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plugin.HandleUsage(ctx, coreusage.Record{
		APIKey:            "cancelled-key",
		Model:             "gpt-5.4",
		RequestedAt:       time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
		Latency:           1500 * time.Millisecond,
		FirstTokenLatency: 250 * time.Millisecond,
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	snapshot := stats.Snapshot()
	if snapshot.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want 1", snapshot.TotalRequests)
	}
	details := snapshot.APIs["cancelled-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
}

func TestLoggerPluginWritesToRepository(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	stats := NewRequestStatistics()
	stats.SetRepository(repo)
	plugin := &LoggerPlugin{stats: stats}

	SetStatisticsEnabled(true)
	t.Cleanup(func() { SetStatisticsEnabled(true) })

	plugin.HandleUsage(context.Background(), coreusage.Record{
		APIKey:            "plugin-key",
		Model:             "gpt-5.4",
		RequestedAt:       time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
		Latency:           1500 * time.Millisecond,
		FirstTokenLatency: 250 * time.Millisecond,
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	snapshot := stats.Snapshot()
	details := snapshot.APIs["plugin-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].LatencyMs != 1500 {
		t.Fatalf("latency_ms = %d, want 1500", details[0].LatencyMs)
	}
	if details[0].FirstTokenLatencyMs != 250 {
		t.Fatalf("first_token_latency_ms = %d, want 250", details[0].FirstTokenLatencyMs)
	}
}
