package chathistory

import (
	"errors"
	"fmt"
	"time"
)

func (s *sqliteStore) TokenUsageStats(window time.Duration) (TokenUsageStats, error) {
	if s == nil {
		return TokenUsageStats{}, errors.New("chat history sqlite store is nil")
	}
	if window <= 0 {
		window = time.Minute
	}
	now := time.Now().UnixMilli()
	windowStart := now - window.Milliseconds()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return TokenUsageStats{}, s.err
	}
	stats := TokenUsageStats{
		WindowSeconds: int64(window.Seconds()),
		WindowByModel: map[string]TokenUsageTotals{},
		TotalByModel:  map[string]TokenUsageTotals{},
	}
	rows, err := s.db.Query(`SELECT model, created_at, updated_at, completed_at, usage_json FROM chat_history`)
	if err != nil {
		return TokenUsageStats{}, fmt.Errorf("read chat history sqlite token stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var model string
		var createdAt, updatedAt, completedAt int64
		var usageJSON string
		if err := rows.Scan(&model, &createdAt, &updatedAt, &completedAt, &usageJSON); err != nil {
			return TokenUsageStats{}, fmt.Errorf("scan chat history sqlite token stats: %w", err)
		}
		if createdAt >= windowStart {
			stats.WindowRequests++
		}
		usage := tokenUsageFromMap(decodeUsageJSON(usageJSON))
		if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
			continue
		}
		usage.Requests = 1
		metricModel := normalizedMetricModel(model)
		stats.Total.add(usage)
		addModelTotals(stats.TotalByModel, metricModel, usage)
		if updatedAt >= windowStart || completedAt >= windowStart {
			stats.Window.add(usage)
			addModelTotals(stats.WindowByModel, metricModel, usage)
		}
	}
	if err := rows.Err(); err != nil {
		return TokenUsageStats{}, fmt.Errorf("scan chat history sqlite token stat rows: %w", err)
	}
	return stats, nil
}

func (s *sqliteStore) OutcomeStats() (OutcomeStats, error) {
	if s == nil {
		return OutcomeStats{}, errors.New("chat history sqlite store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return OutcomeStats{}, s.err
	}
	rows, err := s.db.Query(`SELECT status, status_code, finish_reason FROM chat_history`)
	if err != nil {
		return OutcomeStats{}, fmt.Errorf("read chat history sqlite outcome stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stats := newOutcomeStats()
	for rows.Next() {
		var item SummaryEntry
		if err := rows.Scan(&item.Status, &item.StatusCode, &item.FinishReason); err != nil {
			return OutcomeStats{}, fmt.Errorf("scan chat history sqlite outcome stats: %w", err)
		}
		stats.addSummary(item)
	}
	if err := rows.Err(); err != nil {
		return OutcomeStats{}, fmt.Errorf("scan chat history sqlite outcome stat rows: %w", err)
	}
	stats.finalize()
	return stats, nil
}
