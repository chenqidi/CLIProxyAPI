package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func registerAuthFileRecord(
	t *testing.T,
	manager *coreauth.Manager,
	authDir, fileName, authID, provider string,
) *coreauth.Auth {
	t.Helper()

	path := filepath.Join(authDir, fileName)
	if err := os.WriteFile(path, []byte(`{"type":"`+provider+`"}`), 0o600); err != nil {
		t.Fatalf("failed to write auth fixture %s: %v", fileName, err)
	}

	record := &coreauth.Auth{
		ID:       authID,
		FileName: fileName,
		Provider: provider,
		Attributes: map[string]string{
			"path": path,
		},
	}
	if _, errRegister := manager.Register(context.Background(), record); errRegister != nil {
		t.Fatalf("failed to register auth record %s: %v", authID, errRegister)
	}
	return record
}

func decodeAuthFilesPayload(t *testing.T, body []byte) map[string]map[string]any {
	t.Helper()

	var payload struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("failed to decode auth files payload: %v", err)
	}

	byName := make(map[string]map[string]any, len(payload.Files))
	for _, entry := range payload.Files {
		name, _ := entry["name"].(string)
		if name == "" {
			continue
		}
		byName[name] = entry
	}
	return byName
}

func TestListAuthFilesMarksCurrentActiveFromLatestSuccessfulHit(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	h, repo := newUsageTestHandler(t)
	authDir := t.TempDir()
	manager := coreauth.NewManager(nil, nil, nil)
	h.cfg = &config.Config{AuthDir: authDir}
	h.authManager = manager

	authA := registerAuthFileRecord(t, manager, authDir, "codex-a.json", "codex-auth-a", "codex")
	authB := registerAuthFileRecord(t, manager, authDir, "codex-b.json", "codex-auth-b", "codex")
	authC := registerAuthFileRecord(t, manager, authDir, "claude-a.json", "claude-auth-a", "claude")

	now := time.Now().UTC().Truncate(time.Second)
	seedUsageHandlerEvent(t, repo, usage.UsageEvent{
		RequestedAt: now.Add(-8 * time.Minute),
		Provider:    "codex",
		AuthID:      authA.ID,
	})
	seedUsageHandlerEvent(t, repo, usage.UsageEvent{
		RequestedAt: now.Add(-2 * time.Minute),
		Provider:    "codex",
		AuthID:      authB.ID,
	})
	seedUsageHandlerEvent(t, repo, usage.UsageEvent{
		RequestedAt: now.Add(-1 * time.Minute),
		Provider:    "codex",
		AuthID:      authA.ID,
		Failed:      true,
	})
	seedUsageHandlerEvent(t, repo, usage.UsageEvent{
		RequestedAt: now.Add(-3 * time.Minute),
		Provider:    "claude",
		AuthID:      authC.ID,
	})

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)

	h.ListAuthFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	entries := decodeAuthFilesPayload(t, rec.Body.Bytes())
	if got, ok := entries["codex-a.json"]["current_active"].(bool); ok && got {
		t.Fatalf("expected codex-a.json to not be current_active")
	}
	if got, ok := entries["codex-b.json"]["current_active"].(bool); !ok || !got {
		t.Fatalf("expected codex-b.json to be current_active, got %#v", entries["codex-b.json"]["current_active"])
	}
	if got, ok := entries["claude-a.json"]["current_active"].(bool); !ok || !got {
		t.Fatalf("expected claude-a.json to be current_active, got %#v", entries["claude-a.json"]["current_active"])
	}
}

func TestListAuthFilesDoesNotMarkStaleHitAsCurrentActive(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	h, repo := newUsageTestHandler(t)
	authDir := t.TempDir()
	manager := coreauth.NewManager(nil, nil, nil)
	h.cfg = &config.Config{AuthDir: authDir}
	h.authManager = manager

	auth := registerAuthFileRecord(t, manager, authDir, "codex-a.json", "codex-auth-a", "codex")
	seedUsageHandlerEvent(t, repo, usage.UsageEvent{
		RequestedAt: time.Now().UTC().Add(-currentAuthActiveTTL - time.Minute),
		Provider:    "codex",
		AuthID:      auth.ID,
	})

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)

	h.ListAuthFiles(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	entries := decodeAuthFilesPayload(t, rec.Body.Bytes())
	if got, exists := entries["codex-a.json"]["current_active"]; exists {
		t.Fatalf("expected codex-a.json to have no current_active flag, got %#v", got)
	}
}
