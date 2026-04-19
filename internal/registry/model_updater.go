package registry

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	modelsFetchTimeout    = 30 * time.Second
	modelsRefreshInterval = 3 * time.Hour
)

var modelsURLs = []string{
	"https://raw.githubusercontent.com/router-for-me/models/refs/heads/main/models.json",
	"https://models.router-for.me/models.json",
}

//go:embed models/models.json
var embeddedModelsJSON []byte

type modelStore struct {
	mu   sync.RWMutex
	data *staticModelsJSON
}

type modelCatalogSection struct {
	name     string
	provider string
	get      func(*staticModelsJSON) []*ModelInfo
	set      func(*staticModelsJSON, []*ModelInfo)
}

type modelCatalogFallback struct {
	section string
	err     error
}

var staticModelCatalogSections = []modelCatalogSection{
	{
		name:     "claude",
		provider: "claude",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.Claude
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.Claude = models
		},
	},
	{
		name:     "gemini",
		provider: "gemini",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.Gemini
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.Gemini = models
		},
	},
	{
		name:     "vertex",
		provider: "vertex",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.Vertex
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.Vertex = models
		},
	},
	{
		name:     "gemini-cli",
		provider: "gemini-cli",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.GeminiCLI
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.GeminiCLI = models
		},
	},
	{
		name:     "aistudio",
		provider: "aistudio",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.AIStudio
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.AIStudio = models
		},
	},
	{
		name:     "codex-free",
		provider: "codex",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.CodexFree
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.CodexFree = models
		},
	},
	{
		name:     "codex-team",
		provider: "codex",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.CodexTeam
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.CodexTeam = models
		},
	},
	{
		name:     "codex-plus",
		provider: "codex",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.CodexPlus
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.CodexPlus = models
		},
	},
	{
		name:     "codex-pro",
		provider: "codex",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.CodexPro
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.CodexPro = models
		},
	},
	{
		name:     "qwen",
		provider: "qwen",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.Qwen
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.Qwen = models
		},
	},
	{
		name:     "iflow",
		provider: "iflow",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.IFlow
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.IFlow = models
		},
	},
	{
		name:     "kimi",
		provider: "kimi",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.Kimi
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.Kimi = models
		},
	},
	{
		name:     "antigravity",
		provider: "antigravity",
		get: func(data *staticModelsJSON) []*ModelInfo {
			if data == nil {
				return nil
			}
			return data.Antigravity
		},
		set: func(data *staticModelsJSON, models []*ModelInfo) {
			data.Antigravity = models
		},
	},
}

var modelsCatalogStore = &modelStore{}

var updaterOnce sync.Once

// ModelRefreshCallback is invoked when startup or periodic model refresh detects changes.
// changedProviders contains the provider names whose model definitions changed.
type ModelRefreshCallback func(changedProviders []string)

var (
	refreshCallbackMu     sync.Mutex
	refreshCallback       ModelRefreshCallback
	pendingRefreshChanges []string
)

// SetModelRefreshCallback registers a callback that is invoked when startup or
// periodic model refresh detects changes. Only one callback is supported;
// subsequent calls replace the previous callback.
func SetModelRefreshCallback(cb ModelRefreshCallback) {
	refreshCallbackMu.Lock()
	refreshCallback = cb
	var pending []string
	if cb != nil && len(pendingRefreshChanges) > 0 {
		pending = append([]string(nil), pendingRefreshChanges...)
		pendingRefreshChanges = nil
	}
	refreshCallbackMu.Unlock()

	if cb != nil && len(pending) > 0 {
		cb(pending)
	}
}

func init() {
	// Load embedded data as fallback on startup.
	if err := loadModelsFromBytes(embeddedModelsJSON, "embed"); err != nil {
		panic(fmt.Sprintf("registry: failed to parse embedded models.json: %v", err))
	}
}

// StartModelsUpdater starts a background updater that fetches models
// immediately on startup and then refreshes the model catalog every 3 hours.
// Safe to call multiple times; only one updater will run.
func StartModelsUpdater(ctx context.Context) {
	updaterOnce.Do(func() {
		go runModelsUpdater(ctx)
	})
}

func runModelsUpdater(ctx context.Context) {
	tryStartupRefresh(ctx)
	periodicRefresh(ctx)
}

func periodicRefresh(ctx context.Context) {
	ticker := time.NewTicker(modelsRefreshInterval)
	defer ticker.Stop()
	log.Infof("periodic model refresh started (interval=%s)", modelsRefreshInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tryPeriodicRefresh(ctx)
		}
	}
}

// tryPeriodicRefresh fetches models from remote, compares with the current
// catalog, and notifies the registered callback if any provider changed.
func tryPeriodicRefresh(ctx context.Context) {
	tryRefreshModels(ctx, "periodic model refresh")
}

// tryStartupRefresh fetches models from remote in the background during
// process startup. It uses the same change detection as periodic refresh so
// existing auth registrations can be updated after the callback is registered.
func tryStartupRefresh(ctx context.Context) {
	tryRefreshModels(ctx, "startup model refresh")
}

func tryRefreshModels(ctx context.Context, label string) {
	oldData := getModels()

	parsed, url := fetchModelsFromRemote(ctx)
	if parsed == nil {
		log.Warnf("%s: fetch failed from all URLs, keeping current data", label)
		return
	}

	// Detect changes before updating store.
	changed := detectChangedProviders(oldData, parsed)

	// Update store with new data regardless.
	modelsCatalogStore.mu.Lock()
	modelsCatalogStore.data = parsed
	modelsCatalogStore.mu.Unlock()

	if len(changed) == 0 {
		log.Infof("%s completed from %s, no changes detected", label, url)
		return
	}

	log.Infof("%s completed from %s, changes detected for providers: %v", label, url, changed)
	notifyModelRefresh(changed)
}

// fetchModelsFromRemote tries all remote URLs and returns the parsed model catalog
// along with the URL it was fetched from. Returns (nil, "") if all fetches fail.
func fetchModelsFromRemote(ctx context.Context) (*staticModelsJSON, string) {
	client := &http.Client{Timeout: modelsFetchTimeout}
	current := getModels()
	for _, url := range modelsURLs {
		reqCtx, cancel := context.WithTimeout(ctx, modelsFetchTimeout)
		req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
		if err != nil {
			cancel()
			log.Debugf("models fetch request creation failed for %s: %v", url, err)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			log.Debugf("models fetch failed from %s: %v", url, err)
			continue
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			cancel()
			log.Debugf("models fetch returned %d from %s", resp.StatusCode, url)
			continue
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()

		if err != nil {
			log.Debugf("models fetch read error from %s: %v", url, err)
			continue
		}

		var parsed staticModelsJSON
		if err := json.Unmarshal(data, &parsed); err != nil {
			log.Warnf("models parse failed from %s: %v", url, err)
			continue
		}

		merged, fallbacks, err := mergeModelsCatalogWithFallback(current, &parsed)
		if err != nil {
			log.Warnf("models validate failed from %s: %v", url, err)
			continue
		}
		for _, fallback := range fallbacks {
			log.Warnf("models refresh from %s: keeping current %s section: %v", url, fallback.section, fallback.err)
		}

		return merged, url
	}
	return nil, ""
}

func mergeModelsCatalogWithFallback(current, remote *staticModelsJSON) (*staticModelsJSON, []modelCatalogFallback, error) {
	if remote == nil {
		return nil, nil, fmt.Errorf("catalog is nil")
	}

	merged := &staticModelsJSON{}
	fallbacks := make([]modelCatalogFallback, 0)
	for _, section := range staticModelCatalogSections {
		remoteModels := cloneModelInfos(section.get(remote))
		if err := validateModelSection(section.name, remoteModels); err == nil {
			section.set(merged, remoteModels)
			continue
		} else {
			currentModels := cloneModelInfos(section.get(current))
			if len(currentModels) == 0 {
				return nil, nil, fmt.Errorf("%s: no fallback available after remote validation failure: %w", section.name, err)
			}
			section.set(merged, currentModels)
			fallbacks = append(fallbacks, modelCatalogFallback{section: section.name, err: err})
		}
	}

	if err := validateModelsCatalog(merged); err != nil {
		return nil, fallbacks, err
	}
	return merged, fallbacks, nil
}

// detectChangedProviders compares two model catalogs and returns provider names
// whose model definitions differ. Codex tiers (free/team/plus/pro) are grouped
// under a single "codex" provider.
func detectChangedProviders(oldData, newData *staticModelsJSON) []string {
	if oldData == nil || newData == nil {
		return nil
	}

	seen := make(map[string]bool, len(staticModelCatalogSections))
	var changed []string
	for _, section := range staticModelCatalogSections {
		if seen[section.provider] {
			continue
		}
		if modelSectionChanged(section.get(oldData), section.get(newData)) {
			changed = append(changed, section.provider)
			seen[section.provider] = true
		}
	}
	return changed
}

// modelSectionChanged reports whether two model slices differ.
func modelSectionChanged(a, b []*ModelInfo) bool {
	if len(a) != len(b) {
		return true
	}
	if len(a) == 0 {
		return false
	}
	aj, err1 := json.Marshal(a)
	bj, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return true
	}
	return string(aj) != string(bj)
}

func notifyModelRefresh(changedProviders []string) {
	if len(changedProviders) == 0 {
		return
	}

	refreshCallbackMu.Lock()
	cb := refreshCallback
	if cb == nil {
		pendingRefreshChanges = mergeProviderNames(pendingRefreshChanges, changedProviders)
		refreshCallbackMu.Unlock()
		return
	}
	refreshCallbackMu.Unlock()
	cb(changedProviders)
}

func mergeProviderNames(existing, incoming []string) []string {
	if len(incoming) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	merged := make([]string, 0, len(existing)+len(incoming))
	for _, provider := range existing {
		name := strings.ToLower(strings.TrimSpace(provider))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	for _, provider := range incoming {
		name := strings.ToLower(strings.TrimSpace(provider))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	return merged
}

func loadModelsFromBytes(data []byte, source string) error {
	var parsed staticModelsJSON
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("%s: decode models catalog: %w", source, err)
	}
	if err := validateModelsCatalog(&parsed); err != nil {
		return fmt.Errorf("%s: validate models catalog: %w", source, err)
	}

	modelsCatalogStore.mu.Lock()
	modelsCatalogStore.data = &parsed
	modelsCatalogStore.mu.Unlock()
	return nil
}

func getModels() *staticModelsJSON {
	modelsCatalogStore.mu.RLock()
	defer modelsCatalogStore.mu.RUnlock()
	return modelsCatalogStore.data
}

func validateModelsCatalog(data *staticModelsJSON) error {
	if data == nil {
		return fmt.Errorf("catalog is nil")
	}

	for _, section := range staticModelCatalogSections {
		if err := validateModelSection(section.name, section.get(data)); err != nil {
			return err
		}
	}
	return nil
}

func validateModelSection(section string, models []*ModelInfo) error {
	if len(models) == 0 {
		return fmt.Errorf("%s section is empty", section)
	}

	seen := make(map[string]struct{}, len(models))
	for i, model := range models {
		if model == nil {
			return fmt.Errorf("%s[%d] is null", section, i)
		}
		modelID := strings.TrimSpace(model.ID)
		if modelID == "" {
			return fmt.Errorf("%s[%d] has empty id", section, i)
		}
		if _, exists := seen[modelID]; exists {
			return fmt.Errorf("%s contains duplicate model id %q", section, modelID)
		}
		seen[modelID] = struct{}{}
	}
	return nil
}
