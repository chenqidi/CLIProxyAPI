package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigOptional_UsageStatisticsConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	configYAML := []byte(`
usage-statistics-enabled: true
usage-statistics:
  sqlite-path: "  ./data/usage.sqlite  "
  retention-days: 0
  batch-size: -5
  flush-interval-ms: 0
`)
	if err := os.WriteFile(configPath, configYAML, 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}

	if !cfg.UsageStatisticsEnabled {
		t.Fatal("UsageStatisticsEnabled = false, want true")
	}
	if got := cfg.UsageStatistics.SQLitePath; got != "./data/usage.sqlite" {
		t.Fatalf("SQLitePath = %q, want %q", got, "./data/usage.sqlite")
	}
	if got := cfg.UsageStatistics.RetentionDays; got != DefaultUsageStatisticsRetentionDays {
		t.Fatalf("RetentionDays = %d, want %d", got, DefaultUsageStatisticsRetentionDays)
	}
	if got := cfg.UsageStatistics.BatchSize; got != DefaultUsageStatisticsBatchSize {
		t.Fatalf("BatchSize = %d, want %d", got, DefaultUsageStatisticsBatchSize)
	}
	if got := cfg.UsageStatistics.FlushIntervalMs; got != DefaultUsageStatisticsFlushIntervalMS {
		t.Fatalf("FlushIntervalMs = %d, want %d", got, DefaultUsageStatisticsFlushIntervalMS)
	}
}
