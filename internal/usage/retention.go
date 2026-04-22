package usage

import "time"

const (
	defaultUsageRetentionDays     = 30
	defaultUsageBatchSize         = 128
	defaultUsageFlushInterval     = time.Second
	defaultUsageRetentionInterval = time.Hour
	defaultUsageQueueSize         = 2048
	defaultUsageBusyTimeout       = 5 * time.Second
)

func normaliseRetentionDays(days int) int {
	if days <= 0 {
		return defaultUsageRetentionDays
	}
	return days
}

func retentionCutoff(now time.Time, days int) time.Time {
	return now.UTC().Add(-time.Duration(normaliseRetentionDays(days)) * 24 * time.Hour)
}

func normaliseBatchSize(batchSize int) int {
	if batchSize <= 0 {
		return defaultUsageBatchSize
	}
	return batchSize
}

func normaliseFlushInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return defaultUsageFlushInterval
	}
	return interval
}

func normaliseRetentionInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return defaultUsageRetentionInterval
	}
	return interval
}

func normaliseQueueSize(queueSize, batchSize int) int {
	if queueSize > 0 {
		return queueSize
	}
	size := normaliseBatchSize(batchSize) * 16
	if size < 256 {
		size = 256
	}
	if size < defaultUsageQueueSize {
		return defaultUsageQueueSize
	}
	return size
}

func normaliseBusyTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultUsageBusyTimeout
	}
	return timeout
}
