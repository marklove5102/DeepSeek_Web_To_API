// Package safetystore persists the safety policy lists (banned words, banned
// regexes, jailbreak patterns, blocked / allowed IPs and CIDRs, blocked
// conversation IDs) in dedicated SQLite databases — one for words and one for
// network identifiers. The legacy config.SafetyConfig still carries these
// lists so existing exports and tests continue to work, but at runtime the
// stores in this package are the source of truth: requestguard reads from the
// stores, admin writes go into the stores, and a one-time startup migration
// copies any pre-existing config.SafetyConfig list contents into the stores
// the first time they are opened.
package safetystore

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Word kind constants used in the banned_entries.kind column.
const (
	KindContent   = "content"
	KindRegex     = "regex"
	KindJailbreak = "jailbreak"
)

const metaMigratedFromConfig = "migrated_from_config"

// WordsStore owns data/safety_words.sqlite. It stores three kinds of entries:
// literal banned content (substring match), banned regular expressions, and
// jailbreak patterns. Each (kind, value) pair is unique.
type WordsStore struct {
	mu   sync.Mutex
	path string
	db   *sql.DB
}

func NewWordsStore(path string) (*WordsStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("safety words sqlite path is required")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create safety words dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open safety words sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &WordsStore{path: path, db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *WordsStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *WordsStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *WordsStore) init() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		`CREATE TABLE IF NOT EXISTS banned_entries (
			kind       TEXT NOT NULL,
			value      TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (kind, value)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_banned_kind ON banned_entries(kind)`,
		`CREATE TABLE IF NOT EXISTS words_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init safety words sqlite: %w", err)
		}
	}
	return nil
}

// List returns every value of the given kind, sorted alphabetically for
// stable diffs and predictable display.
func (s *WordsStore) List(kind string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("safety words store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT value FROM banned_entries WHERE kind = ? ORDER BY value`, kind)
	if err != nil {
		return nil, fmt.Errorf("list safety words: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]string, 0, 16)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan safety word: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan safety word rows: %w", err)
	}
	return out, nil
}

// ReplaceKind atomically replaces the entire set of entries of the given kind
// with the provided values (deduplicated, blanks dropped). This matches the
// admin-save semantics where the user submits a complete list.
func (s *WordsStore) ReplaceKind(kind string, values []string) error {
	if s == nil || s.db == nil {
		return errors.New("safety words store is nil")
	}
	clean := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		clean = append(clean, v)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin safety words replace: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM banned_entries WHERE kind = ?`, kind); err != nil {
		return fmt.Errorf("clear safety words kind: %w", err)
	}
	now := time.Now().UnixMilli()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO banned_entries(kind, value, created_at) VALUES(?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare safety words insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, v := range clean {
		if _, err := stmt.Exec(kind, v, now); err != nil {
			return fmt.Errorf("insert safety word: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit safety words replace: %w", err)
	}
	return nil
}

func (s *WordsStore) hasMigrated() (bool, error) {
	if s == nil || s.db == nil {
		return false, errors.New("safety words store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var v string
	err := s.db.QueryRow(`SELECT value FROM words_meta WHERE key = ?`, metaMigratedFromConfig).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read safety words meta: %w", err)
	}
	return strings.TrimSpace(v) == "1", nil
}

func (s *WordsStore) markMigrated() error {
	if s == nil || s.db == nil {
		return errors.New("safety words store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO words_meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		metaMigratedFromConfig, "1",
	)
	if err != nil {
		return fmt.Errorf("write safety words meta: %w", err)
	}
	return nil
}

// MigrateLegacyOnce copies the supplied legacy lists into the store iff the
// dedicated database has never been migrated before. It is idempotent and
// safe to call on every startup.
func (s *WordsStore) MigrateLegacyOnce(content, regex, jailbreak []string) error {
	if s == nil {
		return nil
	}
	migrated, err := s.hasMigrated()
	if err != nil {
		return err
	}
	if migrated {
		return nil
	}
	if err := s.ReplaceKind(KindContent, content); err != nil {
		return err
	}
	if err := s.ReplaceKind(KindRegex, regex); err != nil {
		return err
	}
	if err := s.ReplaceKind(KindJailbreak, jailbreak); err != nil {
		return err
	}
	return s.markMigrated()
}

// Snapshot returns the three lists in one call.
func (s *WordsStore) Snapshot() (content, regex, jailbreak []string, err error) {
	if content, err = s.List(KindContent); err != nil {
		return nil, nil, nil, err
	}
	if regex, err = s.List(KindRegex); err != nil {
		return nil, nil, nil, err
	}
	jailbreak, err = s.List(KindJailbreak)
	return content, regex, jailbreak, err
}

// IPsStore owns data/safety_ips.sqlite. It maintains three separate tables:
// blocked_ips (deny list with IP or CIDR), allowed_ips (allow list reserved
// for future use) and blocked_conversation_ids (deny list keyed by the
// caller-supplied conversation identifier).
type IPsStore struct {
	mu   sync.Mutex
	path string
	db   *sql.DB
}

const (
	ipsTableBlocked   = "blocked_ips"
	ipsTableAllowed   = "allowed_ips"
	ipsTableConvBlock = "blocked_conversation_ids"
)

func NewIPsStore(path string) (*IPsStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("safety ips sqlite path is required")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create safety ips dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open safety ips sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &IPsStore{path: path, db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *IPsStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *IPsStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *IPsStore) init() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		`CREATE TABLE IF NOT EXISTS blocked_ips (
			raw        TEXT PRIMARY KEY,
			note       TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS allowed_ips (
			raw        TEXT PRIMARY KEY,
			note       TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS blocked_conversation_ids (
			id         TEXT PRIMARY KEY,
			note       TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ips_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init safety ips sqlite: %w", err)
		}
	}
	return nil
}

func (s *IPsStore) listTable(table, col string) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("safety ips store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(fmt.Sprintf(`SELECT %s FROM %s ORDER BY %s`, col, table, col))
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]string, 0, 16)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan %s: %w", table, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan %s rows: %w", table, err)
	}
	return out, nil
}

func (s *IPsStore) BlockedIPs() ([]string, error) {
	return s.listTable(ipsTableBlocked, "raw")
}

func (s *IPsStore) AllowedIPs() ([]string, error) {
	return s.listTable(ipsTableAllowed, "raw")
}

func (s *IPsStore) BlockedConversationIDs() ([]string, error) {
	return s.listTable(ipsTableConvBlock, "id")
}

func (s *IPsStore) replaceTable(table, col string, values []string) error {
	if s == nil || s.db == nil {
		return errors.New("safety ips store is nil")
	}
	clean := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		clean = append(clean, v)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin %s replace: %w", table, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s`, table)); err != nil {
		return fmt.Errorf("clear %s: %w", table, err)
	}
	stmt, err := tx.Prepare(fmt.Sprintf(`INSERT OR IGNORE INTO %s(%s, created_at) VALUES(?, ?)`, table, col))
	if err != nil {
		return fmt.Errorf("prepare %s insert: %w", table, err)
	}
	defer func() { _ = stmt.Close() }()
	now := time.Now().UnixMilli()
	for _, v := range clean {
		if _, err := stmt.Exec(v, now); err != nil {
			return fmt.Errorf("insert into %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s replace: %w", table, err)
	}
	return nil
}

func (s *IPsStore) ReplaceBlockedIPs(values []string) error {
	return s.replaceTable(ipsTableBlocked, "raw", values)
}

func (s *IPsStore) ReplaceAllowedIPs(values []string) error {
	return s.replaceTable(ipsTableAllowed, "raw", values)
}

func (s *IPsStore) ReplaceBlockedConversationIDs(values []string) error {
	return s.replaceTable(ipsTableConvBlock, "id", values)
}

func (s *IPsStore) hasMigrated() (bool, error) {
	if s == nil || s.db == nil {
		return false, errors.New("safety ips store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var v string
	err := s.db.QueryRow(`SELECT value FROM ips_meta WHERE key = ?`, metaMigratedFromConfig).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read safety ips meta: %w", err)
	}
	return strings.TrimSpace(v) == "1", nil
}

func (s *IPsStore) markMigrated() error {
	if s == nil || s.db == nil {
		return errors.New("safety ips store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO ips_meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		metaMigratedFromConfig, "1",
	)
	if err != nil {
		return fmt.Errorf("write safety ips meta: %w", err)
	}
	return nil
}

// MigrateLegacyOnce performs the equivalent of WordsStore.MigrateLegacyOnce
// for blocked IPs, allowed IPs (always nil today, reserved) and blocked
// conversation ids.
func (s *IPsStore) MigrateLegacyOnce(blockedIPs, allowedIPs, blockedConv []string) error {
	if s == nil {
		return nil
	}
	migrated, err := s.hasMigrated()
	if err != nil {
		return err
	}
	if migrated {
		return nil
	}
	if err := s.ReplaceBlockedIPs(blockedIPs); err != nil {
		return err
	}
	if err := s.ReplaceAllowedIPs(allowedIPs); err != nil {
		return err
	}
	if err := s.ReplaceBlockedConversationIDs(blockedConv); err != nil {
		return err
	}
	return s.markMigrated()
}

// Snapshot returns the three IP lists in one call.
func (s *IPsStore) Snapshot() (blocked, allowed, blockedConv []string, err error) {
	if blocked, err = s.BlockedIPs(); err != nil {
		return nil, nil, nil, err
	}
	if allowed, err = s.AllowedIPs(); err != nil {
		return nil, nil, nil, err
	}
	blockedConv, err = s.BlockedConversationIDs()
	return blocked, allowed, blockedConv, err
}
