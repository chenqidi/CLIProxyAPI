package management

import (
	"math"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func TestDefaultUsageModelPricesIncludeGPT55(t *testing.T) {
	prices := mergeUsageModelPrices(nil)
	price, ok := resolveUsageModelPrice(prices, "gpt-5.5")
	if !ok {
		t.Fatal("expected default price for gpt-5.5")
	}
	if price.Prompt != 5 || price.Completion != 30 || price.Cache != 0.5 {
		t.Fatalf("unexpected gpt-5.5 default price: %+v", price)
	}
}

func TestCalculateUsageTokenCostForGPT55(t *testing.T) {
	price, ok := resolveUsageModelPrice(mergeUsageModelPrices(nil), "gpt-5.5-2026-04-23")
	if !ok {
		t.Fatal("expected snapshot model to resolve gpt-5.5 default price")
	}

	got := calculateUsageTokenCost(usage.TokenStats{
		InputTokens:  1_000_000,
		CachedTokens: 250_000,
		OutputTokens: 100_000,
	}, price)
	want := 6.875
	if diff := math.Abs(got - want); diff > 1e-12 {
		t.Fatalf("gpt-5.5 cost = %.12f, want %.12f (diff %.12f)", got, want, diff)
	}
}
