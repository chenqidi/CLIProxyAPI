package registry

import "testing"

func newValidStaticModelsCatalog(prefix string) *staticModelsJSON {
	return &staticModelsJSON{
		Claude:      []*ModelInfo{{ID: prefix + "claude", Type: "claude"}},
		Gemini:      []*ModelInfo{{ID: prefix + "gemini", Type: "gemini"}},
		Vertex:      []*ModelInfo{{ID: prefix + "vertex", Type: "vertex"}},
		GeminiCLI:   []*ModelInfo{{ID: prefix + "gemini-cli", Type: "gemini-cli"}},
		AIStudio:    []*ModelInfo{{ID: prefix + "aistudio", Type: "aistudio"}},
		CodexFree:   []*ModelInfo{{ID: prefix + "codex-free", Type: "codex"}},
		CodexTeam:   []*ModelInfo{{ID: prefix + "codex-team", Type: "codex"}},
		CodexPlus:   []*ModelInfo{{ID: prefix + "codex-plus", Type: "codex"}},
		CodexPro:    []*ModelInfo{{ID: prefix + "codex-pro", Type: "codex"}},
		Qwen:        []*ModelInfo{{ID: prefix + "qwen", Type: "qwen"}},
		IFlow:       []*ModelInfo{{ID: prefix + "iflow", Type: "iflow"}},
		Kimi:        []*ModelInfo{{ID: prefix + "kimi", Type: "kimi"}},
		Antigravity: []*ModelInfo{{ID: prefix + "antigravity", Type: "antigravity"}},
	}
}

func hasFallbackSection(fallbacks []modelCatalogFallback, section string) bool {
	for _, fallback := range fallbacks {
		if fallback.section == section {
			return true
		}
	}
	return false
}

func TestMergeModelsCatalogWithFallbackKeepsCurrentForMissingSections(t *testing.T) {
	current := newValidStaticModelsCatalog("current-")
	remote := newValidStaticModelsCatalog("remote-")
	remote.Qwen = nil
	remote.IFlow = nil

	merged, fallbacks, err := mergeModelsCatalogWithFallback(current, remote)
	if err != nil {
		t.Fatalf("mergeModelsCatalogWithFallback() error = %v", err)
	}
	if err := validateModelsCatalog(merged); err != nil {
		t.Fatalf("merged catalog should validate, got %v", err)
	}

	if got := merged.Gemini[0].ID; got != "remote-gemini" {
		t.Fatalf("expected remote gemini to replace current, got %q", got)
	}
	if got := merged.Qwen[0].ID; got != "current-qwen" {
		t.Fatalf("expected current qwen fallback, got %q", got)
	}
	if got := merged.IFlow[0].ID; got != "current-iflow" {
		t.Fatalf("expected current iflow fallback, got %q", got)
	}
	if len(fallbacks) != 2 {
		t.Fatalf("expected 2 fallbacks, got %d", len(fallbacks))
	}
	if !hasFallbackSection(fallbacks, "qwen") {
		t.Fatalf("expected qwen fallback, got %+v", fallbacks)
	}
	if !hasFallbackSection(fallbacks, "iflow") {
		t.Fatalf("expected iflow fallback, got %+v", fallbacks)
	}
}

func TestMergeModelsCatalogWithFallbackKeepsCurrentForInvalidRemoteSection(t *testing.T) {
	current := newValidStaticModelsCatalog("current-")
	remote := newValidStaticModelsCatalog("remote-")
	remote.Qwen = []*ModelInfo{{ID: "", Type: "qwen"}}

	merged, fallbacks, err := mergeModelsCatalogWithFallback(current, remote)
	if err != nil {
		t.Fatalf("mergeModelsCatalogWithFallback() error = %v", err)
	}
	if err := validateModelsCatalog(merged); err != nil {
		t.Fatalf("merged catalog should validate, got %v", err)
	}

	if got := merged.Claude[0].ID; got != "remote-claude" {
		t.Fatalf("expected valid remote section to remain updated, got %q", got)
	}
	if got := merged.Qwen[0].ID; got != "current-qwen" {
		t.Fatalf("expected current qwen fallback for invalid remote section, got %q", got)
	}
	if len(fallbacks) != 1 {
		t.Fatalf("expected 1 fallback, got %d", len(fallbacks))
	}
	if !hasFallbackSection(fallbacks, "qwen") {
		t.Fatalf("expected qwen fallback, got %+v", fallbacks)
	}
}
