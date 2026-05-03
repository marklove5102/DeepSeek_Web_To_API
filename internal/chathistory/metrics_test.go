package chathistory

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTokenUsageStatsAggregatesTotalsAndWindow(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "chat_history.json"))

	oldEntry, err := store.Start(StartParams{Model: "deepseek-v4-flash", UserInput: "old"})
	if err != nil {
		t.Fatalf("start old entry failed: %v", err)
	}
	if _, err := store.Update(oldEntry.ID, UpdateParams{
		Status:    "success",
		Usage:     map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		Completed: true,
	}); err != nil {
		t.Fatalf("update old entry failed: %v", err)
	}

	store.mu.Lock()
	oldSummary, oldIndex, ok := store.findSummaryLocked(oldEntry.ID)
	if !ok {
		store.mu.Unlock()
		t.Fatal("old summary not found")
	}
	oldSummary.CreatedAt -= int64(2 * time.Minute / time.Millisecond)
	oldSummary.UpdatedAt -= int64(2 * time.Minute / time.Millisecond)
	oldSummary.CompletedAt = oldSummary.UpdatedAt
	store.state.Items[oldIndex] = oldSummary
	store.mu.Unlock()

	newEntry, err := store.Start(StartParams{Model: "deepseek-v4-pro", UserInput: "new"})
	if err != nil {
		t.Fatalf("start new entry failed: %v", err)
	}
	if _, err := store.Update(newEntry.ID, UpdateParams{
		Status: "success",
		Usage: map[string]any{
			"input_tokens":            20,
			"output_tokens":           10,
			"total_tokens":            30,
			"input_cache_hit_tokens":  4,
			"input_cache_miss_tokens": 16,
		},
		Completed: true,
	}); err != nil {
		t.Fatalf("update new entry failed: %v", err)
	}

	stats, err := store.TokenUsageStats(time.Minute)
	if err != nil {
		t.Fatalf("token usage stats failed: %v", err)
	}
	if stats.WindowRequests != 1 {
		t.Fatalf("expected one window request, got %d", stats.WindowRequests)
	}
	if stats.Window.TotalTokens != 30 || stats.Total.TotalTokens != 45 {
		t.Fatalf("unexpected token totals: %#v", stats)
	}
	if stats.Window.CacheHitInputTokens != 4 || stats.Window.CacheMissInputTokens != 16 {
		t.Fatalf("unexpected cache split: %#v", stats.Window)
	}
	if stats.TotalByModel["deepseek-v4-flash"].TotalTokens != 15 {
		t.Fatalf("expected flash model total, got %#v", stats.TotalByModel)
	}
	if stats.WindowByModel["deepseek-v4-pro"].TotalTokens != 30 {
		t.Fatalf("expected pro window total, got %#v", stats.WindowByModel)
	}
}

func TestTokenUsageStatsDefaultsPromptInputToCacheMiss(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "chat_history.json"))
	entry, err := store.Start(StartParams{UserInput: "hello"})
	if err != nil {
		t.Fatalf("start entry failed: %v", err)
	}
	if _, err := store.Update(entry.ID, UpdateParams{
		Status:    "success",
		Usage:     map[string]any{"prompt_tokens": "12", "completion_tokens": float64(8)},
		Completed: true,
	}); err != nil {
		t.Fatalf("update entry failed: %v", err)
	}

	stats, err := store.TokenUsageStats(time.Minute)
	if err != nil {
		t.Fatalf("token usage stats failed: %v", err)
	}
	if stats.Total.InputTokens != 12 || stats.Total.OutputTokens != 8 || stats.Total.TotalTokens != 20 {
		t.Fatalf("unexpected parsed totals: %#v", stats.Total)
	}
	if stats.Total.CacheMissInputTokens != 12 {
		t.Fatalf("expected prompt tokens to default to cache miss, got %#v", stats.Total)
	}
}

func TestOutcomeStatsExcludesConfiguredUserSideStatuses(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "chat_history.json"))
	cases := []struct {
		status       string
		statusCode   int
		finishReason string
	}{
		{status: "success", statusCode: 200},
		{status: "error", statusCode: 500},
		{status: "error", statusCode: 502},
		{status: "error", statusCode: 401},
		{status: "stopped", statusCode: 200, finishReason: "context_cancelled"},
		{status: "queued"},
	}
	for _, tc := range cases {
		entry, err := store.Start(StartParams{UserInput: tc.status})
		if err != nil {
			t.Fatalf("start %s failed: %v", tc.status, err)
		}
		if _, err := store.Update(entry.ID, UpdateParams{
			Status:       tc.status,
			StatusCode:   tc.statusCode,
			FinishReason: tc.finishReason,
			Completed:    tc.status != "queued",
		}); err != nil {
			t.Fatalf("update %s failed: %v", tc.status, err)
		}
	}

	stats, err := store.OutcomeStats()
	if err != nil {
		t.Fatalf("outcome stats failed: %v", err)
	}
	if stats.Total != 6 || stats.Success != 1 || stats.Failed != 1 || stats.ExcludedFromFailureRate != 2 || stats.Neutral != 1 || stats.Active != 1 {
		t.Fatalf("unexpected outcome stats: %#v", stats)
	}
	if stats.EligibleTotal != 2 || stats.SuccessRate != 50 {
		t.Fatalf("unexpected success-rate denominator: %#v", stats)
	}
	if !IsFailureRateExcludedStatusCode(524) || IsFailureRateExcludedStatusCode(500) {
		t.Fatalf("unexpected excluded status-code set")
	}
}
