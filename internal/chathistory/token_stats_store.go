package chathistory

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// tokenStatsStore is a dedicated SQLite database that tracks token usage
// rollups separately from chat_history.sqlite.
//
// Splitting persistence achieves two goals:
//  1. Token aggregates are not lost when chat history rows are pruned or the
//     chat history database is wiped/rotated for privacy reasons.
//  2. The aggregate query no longer competes with chat history transactions
//     for the chat_history.sqlite write lock.
//
// Schema is intentionally minimal:
//
//	CREATE TABLE token_rollup (
//	  model TEXT PRIMARY KEY,         -- empty string == grand-total row
//	  requests INTEGER,
//	  input_tokens INTEGER,
//	  output_tokens INTEGER,
//	  cache_hit_input_tokens INTEGER,
//	  cache_miss_input_tokens INTEGER,
//	  total_tokens INTEGER,
//	  updated_at INTEGER
//	)
//
//	CREATE TABLE token_meta (key TEXT PRIMARY KEY, value TEXT)
//
// `token_meta` carries a `migrated_from_chat_history` flag so the one-time
// migration from chat_history_meta runs only once.
type tokenStatsStore struct {
	mu   sync.Mutex
	path string
	db   *sql.DB
	err  error
}

const (
	tokenStatsMetaMigratedFromCH = "migrated_from_chat_history"
)

func newTokenStatsStore(path string) (*tokenStatsStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("token stats sqlite path is required")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create token stats sqlite dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open token stats sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &tokenStatsStore{path: path, db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *tokenStatsStore) init() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("configure token stats sqlite: %w", err)
		}
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS token_rollup (
			model TEXT PRIMARY KEY,
			requests INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cache_hit_input_tokens INTEGER NOT NULL DEFAULT 0,
			cache_miss_input_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS token_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init token stats sqlite schema: %w", err)
		}
	}
	return nil
}

func (s *tokenStatsStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *tokenStatsStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *tokenStatsStore) addRollup(totalDelta TokenUsageTotals, byModelDelta map[string]TokenUsageTotals) error {
	if s == nil || s.db == nil {
		return errors.New("token stats sqlite store is nil")
	}
	if totalDelta.Requests <= 0 && len(byModelDelta) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin token stats rollup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if totalDelta.Requests > 0 {
		if err := s.upsertRollupLocked(tx, "", totalDelta); err != nil {
			return err
		}
	}
	for model, delta := range byModelDelta {
		if err := s.upsertRollupLocked(tx, model, delta); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit token stats rollup: %w", err)
	}
	return nil
}

func (s *tokenStatsStore) upsertRollupLocked(tx *sql.Tx, model string, delta TokenUsageTotals) error {
	now := time.Now().UnixMilli()
	_, err := tx.Exec(
		`INSERT INTO token_rollup(
			model, requests, input_tokens, output_tokens,
			cache_hit_input_tokens, cache_miss_input_tokens, total_tokens, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(model) DO UPDATE SET
			requests = requests + excluded.requests,
			input_tokens = input_tokens + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			cache_hit_input_tokens = cache_hit_input_tokens + excluded.cache_hit_input_tokens,
			cache_miss_input_tokens = cache_miss_input_tokens + excluded.cache_miss_input_tokens,
			total_tokens = total_tokens + excluded.total_tokens,
			updated_at = excluded.updated_at`,
		model,
		delta.Requests,
		delta.InputTokens,
		delta.OutputTokens,
		delta.CacheHitInputTokens,
		delta.CacheMissInputTokens,
		delta.TotalTokens,
		now,
	)
	if err != nil {
		return fmt.Errorf("write token stats rollup row: %w", err)
	}
	return nil
}

func (s *tokenStatsStore) readRollup() (TokenUsageTotals, map[string]TokenUsageTotals, error) {
	if s == nil || s.db == nil {
		return TokenUsageTotals{}, nil, errors.New("token stats sqlite store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(
		`SELECT model, requests, input_tokens, output_tokens,
		        cache_hit_input_tokens, cache_miss_input_tokens, total_tokens
		 FROM token_rollup`,
	)
	if err != nil {
		return TokenUsageTotals{}, nil, fmt.Errorf("read token stats rollup: %w", err)
	}
	defer func() { _ = rows.Close() }()
	total := TokenUsageTotals{}
	byModel := map[string]TokenUsageTotals{}
	for rows.Next() {
		var model string
		var t TokenUsageTotals
		if err := rows.Scan(
			&model,
			&t.Requests,
			&t.InputTokens,
			&t.OutputTokens,
			&t.CacheHitInputTokens,
			&t.CacheMissInputTokens,
			&t.TotalTokens,
		); err != nil {
			return TokenUsageTotals{}, nil, fmt.Errorf("scan token stats rollup row: %w", err)
		}
		if model == "" {
			total = t
			continue
		}
		byModel[model] = t
	}
	if err := rows.Err(); err != nil {
		return TokenUsageTotals{}, nil, fmt.Errorf("scan token stats rollup rows: %w", err)
	}
	return total, byModel, nil
}

func (s *tokenStatsStore) clearRollup() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM token_rollup`); err != nil {
		return fmt.Errorf("clear token stats rollup: %w", err)
	}
	return nil
}

func (s *tokenStatsStore) hasMigrated() (bool, error) {
	value, err := s.metaValue(tokenStatsMetaMigratedFromCH)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(value) == "1", nil
}

func (s *tokenStatsStore) markMigrated() error {
	return s.setMeta(tokenStatsMetaMigratedFromCH, "1")
}

func (s *tokenStatsStore) metaValue(key string) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("token stats sqlite store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var value string
	err := s.db.QueryRow(`SELECT value FROM token_meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read token stats meta: %w", err)
	}
	return value, nil
}

func (s *tokenStatsStore) setMeta(key, value string) error {
	if s == nil || s.db == nil {
		return errors.New("token stats sqlite store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO token_meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key,
		value,
	)
	if err != nil {
		return fmt.Errorf("write token stats meta: %w", err)
	}
	return nil
}

// migrateLegacyRollupOnce copies the historical pruned token aggregates that
// were previously stored in chat_history_meta into the dedicated database.
// Idempotent: runs only the first time it is called per database file.
func (s *tokenStatsStore) migrateLegacyRollupOnce(legacyTotal TokenUsageTotals, legacyByModel map[string]TokenUsageTotals) error {
	migrated, err := s.hasMigrated()
	if err != nil {
		return err
	}
	if migrated {
		return nil
	}
	if err := s.addRollup(legacyTotal, legacyByModel); err != nil {
		return err
	}
	return s.markMigrated()
}

// jsonOrEmpty is a tiny helper for tests / debug output.
func (s *tokenStatsStore) jsonOrEmpty() string {
	total, byModel, err := s.readRollup()
	if err != nil {
		return ""
	}
	payload := map[string]any{
		"total":    total,
		"by_model": byModel,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(b)
}
