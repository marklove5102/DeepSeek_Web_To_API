package metrics

import (
	"strings"
	"time"

	"DeepSeek_Web_To_API/internal/chathistory"
)

const (
	pricingSourceURL = "https://api-docs.deepseek.com/quick_start/pricing"
	pricingCurrency  = "USD"
)

var proDiscountEndsAt = time.Date(2026, 5, 31, 15, 59, 0, 0, time.UTC)

type modelPricing struct {
	InputCacheHitPerMillion  float64 `json:"input_cache_hit_per_1m"`
	InputCacheMissPerMillion float64 `json:"input_cache_miss_per_1m"`
	OutputPerMillion         float64 `json:"output_per_1m"`
}

type costBreakdown struct {
	Currency       string                  `json:"currency"`
	WindowUSD      float64                 `json:"window_usd"`
	TotalUSD       float64                 `json:"total_usd"`
	PricingSource  string                  `json:"pricing_source"`
	PricingNote    string                  `json:"pricing_note"`
	PricingByModel map[string]modelPricing `json:"pricing_by_model"`
	DiscountEndsAt string                  `json:"discount_ends_at,omitempty"`
}

func buildCostBreakdown(stats chathistory.TokenUsageStats, now time.Time) costBreakdown {
	prices := map[string]modelPricing{}
	for model := range stats.TotalByModel {
		prices[model] = priceForModel(model, now)
	}
	for model := range stats.WindowByModel {
		prices[model] = priceForModel(model, now)
	}
	if len(prices) == 0 {
		prices["deepseek-v4-flash"] = priceForModel("deepseek-v4-flash", now)
	}

	return costBreakdown{
		Currency:       pricingCurrency,
		WindowUSD:      calculateCostUSD(stats.WindowByModel, now),
		TotalUSD:       calculateCostUSD(stats.TotalByModel, now),
		PricingSource:  pricingSourceURL,
		PricingNote:    "Estimated from DeepSeek official per-1M-token pricing; input tokens without cache split are billed as cache miss.",
		PricingByModel: prices,
		DiscountEndsAt: proDiscountEndsAt.Format(time.RFC3339),
	}
}

func calculateCostUSD(byModel map[string]chathistory.TokenUsageTotals, now time.Time) float64 {
	var total float64
	for model, usage := range byModel {
		price := priceForModel(model, now)
		hit := float64(usage.CacheHitInputTokens) * price.InputCacheHitPerMillion
		miss := float64(usage.CacheMissInputTokens) * price.InputCacheMissPerMillion
		output := float64(usage.OutputTokens) * price.OutputPerMillion
		total += (hit + miss + output) / 1_000_000
	}
	return total
}

func priceForModel(model string, now time.Time) modelPricing {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(normalized, "pro") {
		if now.UTC().Before(proDiscountEndsAt) {
			return modelPricing{
				InputCacheHitPerMillion:  0.003625,
				InputCacheMissPerMillion: 0.435,
				OutputPerMillion:         0.87,
			}
		}
		return modelPricing{
			InputCacheHitPerMillion:  0.0145,
			InputCacheMissPerMillion: 1.74,
			OutputPerMillion:         3.48,
		}
	}
	return modelPricing{
		InputCacheHitPerMillion:  0.0028,
		InputCacheMissPerMillion: 0.14,
		OutputPerMillion:         0.28,
	}
}
