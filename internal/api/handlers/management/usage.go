package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

const defaultUsageRetentionDays = 30

type usageExportPayload struct {
	Version       int                      `json:"version"`
	ExportedAt    time.Time                `json:"exported_at"`
	RetentionDays int                      `json:"retention_days"`
	WindowStart   time.Time                `json:"window_start"`
	WindowEnd     time.Time                `json:"window_end"`
	Usage         usage.StatisticsSnapshot `json:"usage"`
}

type usageImportPayload struct {
	Version int                      `json:"version"`
	Usage   usage.StatisticsSnapshot `json:"usage"`
}

// GetUsageSummary returns lightweight usage summary metrics.
func (h *Handler) GetUsageSummary(c *gin.Context) {
	service := h.usageQuery()
	if service == nil {
		c.JSON(http.StatusOK, usage.UsageSummary{RetentionDays: defaultUsageRetentionDays})
		return
	}
	start, end, ok := parseUsageRange(c)
	if !ok {
		return
	}
	summary, err := service.Summary(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("usage summary query failed: %v", err)})
		return
	}
	totalCost, err := h.usageSummaryCost(c.Request.Context(), service, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("usage summary cost query failed: %v", err)})
		return
	}
	summary.TotalCost = totalCost
	c.JSON(http.StatusOK, summary)
}

// GetUsageStatus returns service-wide and grouped status-bar data.
func (h *Handler) GetUsageStatus(c *gin.Context) {
	service := h.usageQuery()
	if service == nil {
		c.JSON(http.StatusOK, usage.UsageStatusOverview{})
		return
	}
	start, end, ok := parseUsageRange(c)
	if !ok {
		return
	}
	result, err := service.Status(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("usage status query failed: %v", err)})
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetUsageHealth returns the 7x96 service health grid.
func (h *Handler) GetUsageHealth(c *gin.Context) {
	service := h.usageQuery()
	if service == nil {
		c.JSON(http.StatusOK, usage.ServiceHealthData{Rows: 7, Cols: 96})
		return
	}
	start, end, ok := parseUsageRange(c)
	if !ok {
		return
	}
	result, err := service.Health(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("usage health query failed: %v", err)})
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetUsageCharts returns chart-ready usage series grouped by model.
func (h *Handler) GetUsageCharts(c *gin.Context) {
	service := h.usageQuery()
	if service == nil {
		c.JSON(http.StatusOK, usage.UsageChartData{DataByModel: make(map[string][]float64)})
		return
	}
	start, end, ok := parseUsageRange(c)
	if !ok {
		return
	}
	hourWindow := 0
	if raw := strings.TrimSpace(c.Query("hours")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hours query parameter"})
			return
		}
		hourWindow = parsed
	}
	query := usage.UsageChartQuery{
		Start:           start,
		End:             end,
		Period:          c.DefaultQuery("period", "day"),
		Metric:          c.DefaultQuery("metric", "requests"),
		HourWindowHours: hourWindow,
	}

	metric := strings.ToLower(strings.TrimSpace(query.Metric))
	if metric == "cost" {
		tokenCharts, err := service.TokenCharts(c.Request.Context(), query)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, buildUsageCostChart(tokenCharts, mergeUsageModelPrices(h.cfg)))
		return
	}

	result, err := service.Charts(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetUsageEvents returns paginated lightweight request events.
func (h *Handler) GetUsageEvents(c *gin.Context) {
	service := h.usageQuery()
	if service == nil {
		c.JSON(http.StatusOK, usage.UsageEventsPage{})
		return
	}
	start, end, ok := parseUsageRange(c)
	if !ok {
		return
	}
	page, err := parseOptionalIntQuery(c, "page")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	pageSize, err := parseOptionalIntQuery(c, "page_size")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	failed, err := parseOptionalBoolQuery(c, "failed")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := service.Events(c.Request.Context(), usage.UsageEventsQuery{
		Start:         start,
		End:           end,
		Page:          page,
		PageSize:      pageSize,
		Model:         c.Query("model"),
		Source:        c.Query("source"),
		AuthIndex:     c.Query("auth_index"),
		RequestMethod: c.Query("request_method"),
		RequestPath:   c.Query("request_path"),
		Failed:        failed,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("usage events query failed: %v", err)})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ExportUsageStatistics returns a complete compatibility snapshot for backup/migration.
func (h *Handler) ExportUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}
	windowStart, windowEnd, retentionDays := h.currentUsageWindow(c.Request.Context())
	c.JSON(http.StatusOK, usageExportPayload{
		Version:       2,
		ExportedAt:    time.Now().UTC(),
		RetentionDays: retentionDays,
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
		Usage:         snapshot,
	})
}

// ImportUsageStatistics merges a previously exported usage snapshot into the current backend.
func (h *Handler) ImportUsageStatistics(c *gin.Context) {
	if h == nil || h.usageStats == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "usage statistics unavailable"})
		return
	}

	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var payload usageImportPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if payload.Version != 0 && payload.Version != 1 && payload.Version != 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported version"})
		return
	}

	result := h.usageStats.MergeSnapshot(payload.Usage)
	snapshot := h.usageStats.Snapshot()
	windowStart, windowEnd, retentionDays := h.currentUsageWindow(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"added":           result.Added,
		"skipped":         result.Skipped,
		"total_requests":  snapshot.TotalRequests,
		"failed_requests": snapshot.FailureCount,
		"retention_days":  retentionDays,
		"window_start":    windowStart,
		"window_end":      windowEnd,
	})
}

func (h *Handler) usageQuery() *usage.QueryService {
	if h == nil {
		return nil
	}
	if h.usageQueryService == nil {
		h.usageQueryService = usage.NewQueryService(h.usageStats)
	}
	return h.usageQueryService
}

func (h *Handler) currentUsageWindow(ctx context.Context) (time.Time, time.Time, int) {
	retentionDays := defaultUsageRetentionDays
	if service := h.usageQuery(); service != nil {
		retentionDays = service.RetentionDays()
		if summary, err := service.Summary(ctx, nil, nil); err == nil {
			return summary.WindowStart, summary.WindowEnd, summary.RetentionDays
		}
	}
	windowEnd := time.Now().UTC()
	windowStart := windowEnd.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	return windowStart, windowEnd, retentionDays
}

func parseUsageRange(c *gin.Context) (*time.Time, *time.Time, bool) {
	start, err := parseOptionalTimeQuery(c, "start")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return nil, nil, false
	}
	end, err := parseOptionalTimeQuery(c, "end")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return nil, nil, false
	}
	return start, end, true
}

func parseOptionalTimeQuery(c *gin.Context, key string) (*time.Time, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, nil
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			parsed = parsed.UTC()
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("invalid %s query parameter", key)
}

func parseOptionalIntQuery(c *gin.Context, key string) (int, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s query parameter", key)
	}
	return parsed, nil
}

func parseOptionalBoolQuery(c *gin.Context, key string) (*bool, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s query parameter", key)
	}
	return &parsed, nil
}
