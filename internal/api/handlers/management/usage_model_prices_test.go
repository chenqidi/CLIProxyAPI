package management

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestPutUsageModelPricesPersistsNormalizedEntries(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("usage-statistics-enabled: true\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	h := NewHandler(cfg, configPath, nil)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(
		http.MethodPut,
		"/v0/management/usage-model-prices",
		strings.NewReader(`{
			"gpt-5.4": {"prompt": 3, "completion": 18, "cache": -1},
			"  ": {"prompt": 1, "completion": 2, "cache": 3}
		}`),
	)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.PutUsageModelPrices(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	got := h.cfg.UsageModelPrices
	if len(got) != 1 {
		t.Fatalf("expected 1 saved entry, got %d", len(got))
	}

	price, ok := got["gpt-5.4"]
	if !ok {
		t.Fatalf("expected gpt-5.4 to be saved")
	}
	if price.Prompt != 3 {
		t.Fatalf("expected prompt 3, got %v", price.Prompt)
	}
	if price.Completion != 18 {
		t.Fatalf("expected completion 18, got %v", price.Completion)
	}
	if price.Cache != 3 {
		t.Fatalf("expected cache fallback to prompt 3, got %v", price.Cache)
	}

	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	reloadedPrice, ok := reloaded.UsageModelPrices["gpt-5.4"]
	if !ok {
		t.Fatalf("expected reloaded config to contain gpt-5.4")
	}
	if reloadedPrice.Prompt != 3 || reloadedPrice.Completion != 18 || reloadedPrice.Cache != 3 {
		t.Fatalf("unexpected reloaded price: %+v", reloadedPrice)
	}
}

func TestPutUsageModelPricesRejectsInvalidBody(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("usage-statistics-enabled: true\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	h := NewHandler(cfg, configPath, nil)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(
		http.MethodPut,
		"/v0/management/usage-model-prices",
		strings.NewReader(`{"usage-model-prices": "invalid"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.PutUsageModelPrices(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPutUsagePriceSelectedModelPersistsNormalizedValue(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("usage-statistics-enabled: true\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	h := NewHandler(cfg, configPath, nil)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(
		http.MethodPut,
		"/v0/management/usage-price-selected-model",
		strings.NewReader(`{"value":"  gpt-5.4-mini  "}`),
	)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.PutUsagePriceSelectedModel(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	if got := h.cfg.UsagePriceSelectedModel; got != "gpt-5.4-mini" {
		t.Fatalf("expected selected model gpt-5.4-mini, got %q", got)
	}

	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got := reloaded.UsagePriceSelectedModel; got != "gpt-5.4-mini" {
		t.Fatalf("expected reloaded selected model gpt-5.4-mini, got %q", got)
	}
}

func TestPutUsagePriceSelectedModelFallsBackToDefault(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("usage-statistics-enabled: true\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	h := NewHandler(cfg, configPath, nil)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(
		http.MethodPut,
		"/v0/management/usage-price-selected-model",
		strings.NewReader(`{"value":"   "}`),
	)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	h.PutUsagePriceSelectedModel(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	if got := h.cfg.UsagePriceSelectedModel; got != config.DefaultUsagePriceModel {
		t.Fatalf("expected selected model %q, got %q", config.DefaultUsagePriceModel, got)
	}
}
