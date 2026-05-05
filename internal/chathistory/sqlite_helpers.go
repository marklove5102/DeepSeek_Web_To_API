package chathistory

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	sqliteMetaPrunedTotal        = "pruned_total"
	sqliteMetaPrunedSuccess      = "pruned_success"
	sqliteMetaPrunedFailed       = "pruned_failed"
	sqliteMetaPrunedActive       = "pruned_active"
	sqliteMetaPrunedNeutral      = "pruned_neutral"
	sqliteMetaPrunedExcludedRate = "pruned_excluded_from_failure_rate"
	sqliteMetaPrunedTokenTotal   = "pruned_token_total_json"
	sqliteMetaPrunedTokenByModel = "pruned_token_total_by_model_json"
)

func encodeUsageJSON(usage map[string]any) (string, error) {
	if len(usage) == 0 {
		return "", nil
	}
	payload, err := json.Marshal(usage)
	if err != nil {
		return "", fmt.Errorf("encode chat history sqlite usage: %w", err)
	}
	return string(payload), nil
}

func decodeUsageJSON(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var usage map[string]any
	if err := json.Unmarshal([]byte(raw), &usage); err != nil {
		return nil
	}
	return usage
}

func boolToSQLiteInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *sqliteStore) bumpRevisionAtLeastLocked(tx *sql.Tx, revision int64) error {
	current, err := s.metaInt64Locked(tx, "revision", 0)
	if err != nil {
		return err
	}
	if revision <= current {
		return nil
	}
	return s.setMetaLocked(tx, "revision", strconv.FormatInt(revision, 10))
}

func (s *sqliteStore) pruneLocked(tx *sql.Tx) (int, error) {
	limit, err := s.metaIntLocked(tx, "limit", DefaultLimit)
	if err != nil {
		return 0, err
	}
	if limit == DisabledLimit {
		return 0, nil
	}
	if !isAllowedLimit(limit) {
		limit = DefaultLimit
		if err := s.setMetaLocked(tx, "limit", strconv.Itoa(limit)); err != nil {
			return 0, err
		}
	}
	total, err := s.countTxLocked(tx)
	if err != nil {
		return 0, err
	}
	retain := retainedHistoryCountAfterPrune(total, limit)
	if retain < 0 || total <= retain {
		return 0, nil
	}
	if err := s.rollupPrunedMetricsLocked(tx, retain); err != nil {
		return 0, err
	}
	_, err = tx.Exec(
		`DELETE FROM chat_history
		 WHERE id IN (
			SELECT id FROM chat_history
			ORDER BY updated_at DESC, created_at DESC
			LIMIT -1 OFFSET ?
		 )`,
		retain,
	)
	if err != nil {
		return 0, fmt.Errorf("prune chat history sqlite rows: %w", err)
	}
	return total - retain, nil
}

func (s *sqliteStore) countTxLocked(tx *sql.Tx) (int, error) {
	var total int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM chat_history`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count chat history sqlite rows: %w", err)
	}
	return total, nil
}

func (s *sqliteStore) rollupPrunedMetricsLocked(tx *sql.Tx, retain int) error {
	outcome, tokenTotal, tokenByModel, err := s.prunedMetricsLocked(tx, retain)
	if err != nil {
		return err
	}
	if outcome.Total <= 0 {
		return nil
	}
	for _, item := range []struct {
		key   string
		delta int64
	}{
		{sqliteMetaPrunedTotal, outcome.Total},
		{sqliteMetaPrunedSuccess, outcome.Success},
		{sqliteMetaPrunedFailed, outcome.Failed},
		{sqliteMetaPrunedActive, outcome.Active},
		{sqliteMetaPrunedNeutral, outcome.Neutral},
		{sqliteMetaPrunedExcludedRate, outcome.ExcludedFromFailureRate},
	} {
		if err := s.addMetaInt64Locked(tx, item.key, item.delta); err != nil {
			return err
		}
	}
	return s.addTokenRollupLocked(tx, tokenTotal, tokenByModel)
}

func (s *sqliteStore) prunedMetricsLocked(tx *sql.Tx, retain int) (OutcomeStats, TokenUsageTotals, map[string]TokenUsageTotals, error) {
	outcome := newOutcomeStats()
	tokenTotal := TokenUsageTotals{}
	tokenByModel := map[string]TokenUsageTotals{}
	rows, err := tx.Query(
		`SELECT status, status_code, finish_reason, model, usage_json
		 FROM chat_history
		 ORDER BY updated_at DESC, created_at DESC
		 LIMIT -1 OFFSET ?`,
		retain,
	)
	if err != nil {
		return OutcomeStats{}, TokenUsageTotals{}, nil, fmt.Errorf("read pruned chat history metrics: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item SummaryEntry
		var model, usageJSON string
		if err := rows.Scan(&item.Status, &item.StatusCode, &item.FinishReason, &model, &usageJSON); err != nil {
			return OutcomeStats{}, TokenUsageTotals{}, nil, fmt.Errorf("scan pruned chat history metrics: %w", err)
		}
		outcome.addSummary(item)
		usage := tokenUsageFromMap(decodeUsageJSON(usageJSON))
		if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
			continue
		}
		usage.Requests = 1
		tokenTotal.add(usage)
		addModelTotals(tokenByModel, normalizedMetricModel(model), usage)
	}
	if err := rows.Err(); err != nil {
		return OutcomeStats{}, TokenUsageTotals{}, nil, fmt.Errorf("scan pruned chat history metric rows: %w", err)
	}
	return outcome, tokenTotal, tokenByModel, nil
}

func (s *sqliteStore) addMetaInt64Locked(tx *sql.Tx, key string, delta int64) error {
	if delta == 0 {
		return nil
	}
	current, err := s.metaInt64Locked(tx, key, 0)
	if err != nil {
		return err
	}
	return s.setMetaLocked(tx, key, strconv.FormatInt(current+delta, 10))
}

func (s *sqliteStore) addTokenRollupLocked(tx *sql.Tx, totalDelta TokenUsageTotals, byModelDelta map[string]TokenUsageTotals) error {
	if totalDelta.Requests <= 0 && len(byModelDelta) == 0 {
		return nil
	}
	// Maintain the legacy chat_history_meta rollup alongside the dedicated
	// token stats DB so the two never drift; legacy data continues to work
	// even when the dedicated DB is disabled.
	total, byModel, err := s.tokenRollupFromLegacyMetaLocked(tx)
	if err != nil {
		return err
	}
	total.add(totalDelta)
	for model, usage := range byModelDelta {
		addModelTotals(byModel, model, usage)
	}
	if err := s.setMetaJSONLocked(tx, sqliteMetaPrunedTokenTotal, total); err != nil {
		return err
	}
	if err := s.setMetaJSONLocked(tx, sqliteMetaPrunedTokenByModel, byModel); err != nil {
		return err
	}
	if s.tokenStats != nil {
		if err := s.tokenStats.addRollup(totalDelta, byModelDelta); err != nil {
			return fmt.Errorf("mirror token rollup to dedicated store: %w", err)
		}
	}
	return nil
}

// tokenRollupLocked returns the persisted pruned rollup. When the dedicated
// token stats DB is available it is preferred; otherwise we fall back to the
// legacy keys in chat_history_meta. Both paths return zero-value totals when
// no rollup has been written yet.
func (s *sqliteStore) tokenRollupLocked(tx *sql.Tx) (TokenUsageTotals, map[string]TokenUsageTotals, error) {
	if s.tokenStats != nil {
		total, byModel, err := s.tokenStats.readRollup()
		if err == nil {
			if byModel == nil {
				byModel = map[string]TokenUsageTotals{}
			}
			return total, byModel, nil
		}
	}
	return s.tokenRollupFromLegacyMetaLocked(tx)
}

func (s *sqliteStore) tokenRollupFromLegacyMetaLocked(tx *sql.Tx) (TokenUsageTotals, map[string]TokenUsageTotals, error) {
	total := TokenUsageTotals{}
	byModel := map[string]TokenUsageTotals{}
	if err := s.metaJSONLocked(tx, sqliteMetaPrunedTokenTotal, &total); err != nil {
		return TokenUsageTotals{}, nil, err
	}
	if err := s.metaJSONLocked(tx, sqliteMetaPrunedTokenByModel, &byModel); err != nil {
		return TokenUsageTotals{}, nil, err
	}
	if byModel == nil {
		byModel = map[string]TokenUsageTotals{}
	}
	return total, byModel, nil
}

func (s *sqliteStore) metaJSONLocked(tx *sql.Tx, key string, target any) error {
	raw, err := s.metaLocked(tx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode chat history sqlite meta %s: %w", key, err)
	}
	return nil
}

func (s *sqliteStore) setMetaJSONLocked(tx *sql.Tx, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode chat history sqlite meta %s: %w", key, err)
	}
	return s.setMetaLocked(tx, key, string(payload))
}

func (s *sqliteStore) clearPrunedMetricsLocked(tx *sql.Tx) error {
	for _, key := range []string{
		sqliteMetaPrunedTotal,
		sqliteMetaPrunedSuccess,
		sqliteMetaPrunedFailed,
		sqliteMetaPrunedActive,
		sqliteMetaPrunedNeutral,
		sqliteMetaPrunedExcludedRate,
		sqliteMetaPrunedTokenTotal,
		sqliteMetaPrunedTokenByModel,
	} {
		if _, err := tx.Exec(`DELETE FROM chat_history_meta WHERE key = ?`, key); err != nil {
			return fmt.Errorf("clear chat history sqlite meta %s: %w", key, err)
		}
	}
	if s.tokenStats != nil {
		if err := s.tokenStats.clearRollup(); err != nil {
			return fmt.Errorf("clear dedicated token stats rollup: %w", err)
		}
	}
	return nil
}

func (s *sqliteStore) snapshotLocked() (File, error) {
	limit, revision, err := s.stateMetaLocked()
	if err != nil {
		return File{}, err
	}
	items, err := s.readAllSummariesLocked()
	if err != nil {
		return File{}, err
	}
	return File{Version: FileVersion, Limit: limit, Revision: revision, Items: items}, nil
}
