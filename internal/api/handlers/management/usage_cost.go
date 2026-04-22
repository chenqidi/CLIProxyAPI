package management

import (
	"context"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

const usageTokensPerPriceUnit = 1_000_000

var usageSnapshotModelSuffixRegex = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

var defaultUsageModelPrices = map[string]config.UsageModelPrice{
	"gpt-5.4":       {Prompt: 2.5, Completion: 15, Cache: 0.25},
	"gpt-5.3-codex": {Prompt: 1.75, Completion: 14, Cache: 0.175},
}

var usageModelPriceAliasFallbacks = map[string]string{
	"gpt-5.3-chat": "gpt-5.3-chat-latest",
}

func mergeUsageModelPrices(cfg *config.Config) map[string]config.UsageModelPrice {
	merged := make(map[string]config.UsageModelPrice, len(defaultUsageModelPrices))
	for model, price := range defaultUsageModelPrices {
		merged[model] = price
	}
	if cfg == nil {
		return merged
	}
	for model, price := range config.NormalizeUsageModelPrices(cfg.UsageModelPrices) {
		merged[model] = price
	}
	return merged
}

func getUsageModelPriceLookupKeys(model string) []string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return nil
	}

	candidates := make([]string, 0, 4)
	seen := make(map[string]struct{})
	var addCandidate func(string)
	addCandidate = func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, exists := seen[candidate]; exists {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
		if alias, ok := usageModelPriceAliasFallbacks[candidate]; ok {
			addCandidate(alias)
		}
	}

	addCandidate(trimmed)

	lowercase := strings.ToLower(trimmed)
	if lowercase != trimmed {
		addCandidate(lowercase)
	}

	snapshotBase := usageSnapshotModelSuffixRegex.ReplaceAllString(lowercase, "")
	if snapshotBase != lowercase {
		addCandidate(snapshotBase)
	}

	return candidates
}

func resolveUsageModelPrice(prices map[string]config.UsageModelPrice, model string) (config.UsageModelPrice, bool) {
	for _, key := range getUsageModelPriceLookupKeys(model) {
		if price, ok := prices[key]; ok {
			return price, true
		}
	}
	return config.UsageModelPrice{}, false
}

func calculateUsageSummaryCostFromTotals(
	totalsByModel map[string]usage.TokenStats,
	modelPrices map[string]config.UsageModelPrice,
) (*float64, bool) {
	if len(totalsByModel) == 0 || len(modelPrices) == 0 {
		return nil, false
	}

	var (
		totalCost float64
		hasPrice  bool
	)
	for model, tokens := range totalsByModel {
		price, ok := resolveUsageModelPrice(modelPrices, model)
		if !ok {
			continue
		}
		hasPrice = true

		inputTokens := max(tokens.InputTokens, 0)
		outputTokens := max(tokens.OutputTokens, 0)
		cachedTokens := max(tokens.CachedTokens, 0)
		promptTokens := max(inputTokens-cachedTokens, 0)

		totalCost += (float64(promptTokens) / usageTokensPerPriceUnit) * price.Prompt
		totalCost += (float64(cachedTokens) / usageTokensPerPriceUnit) * price.Cache
		totalCost += (float64(outputTokens) / usageTokensPerPriceUnit) * price.Completion
	}

	if !hasPrice {
		return nil, false
	}
	if math.IsNaN(totalCost) || math.IsInf(totalCost, 0) || totalCost < 0 {
		totalCost = 0
	}
	return &totalCost, true
}

func (h *Handler) usageSummaryCost(
	ctx context.Context,
	service *usage.QueryService,
	start, end *time.Time,
) (*float64, error) {
	if service == nil {
		return nil, nil
	}
	totalsByModel, err := service.TokenTotalsByModel(ctx, start, end)
	if err != nil {
		return nil, err
	}
	totalCost, _ := calculateUsageSummaryCostFromTotals(totalsByModel, mergeUsageModelPrices(h.cfg))
	return totalCost, nil
}
