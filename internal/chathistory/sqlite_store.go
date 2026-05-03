package chathistory

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteStore struct {
	mu              sync.Mutex
	path            string
	legacyPath      string
	legacyDetailDir string
	db              *sql.DB
	err             error
}

type sqliteColumnInfo struct {
	name string
	ddl  string
}

func newSQLiteStore(path, legacyPath string) (*sqliteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("chat history sqlite path is required")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create chat history sqlite dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open chat history sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &sqliteStore{
		path:            path,
		legacyPath:      strings.TrimSpace(legacyPath),
		legacyDetailDir: strings.TrimSpace(legacyPath) + ".d",
		db:              db,
	}
	store.err = store.init()
	if store.err != nil {
		_ = db.Close()
		return nil, store.err
	}
	return store, nil
}

func (s *sqliteStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *sqliteStore) Err() error {
	if s == nil {
		return errors.New("chat history sqlite store is nil")
	}
	return s.err
}

func (s *sqliteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *sqliteStore) init() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("configure chat history sqlite: %w", err)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin chat history sqlite init: %w", err)
	}
	if err := s.initSchemaLocked(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.initMetaLocked(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.importLegacyIfEmptyLocked(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.stopUnfinishedLocked(tx, "server restarted before request completed"); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.pruneLocked(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit chat history sqlite init: %w", err)
	}
	compressedRows, err := s.compressExistingDetailsBatchedLocked()
	if err != nil {
		return err
	}
	if compressedRows > 0 {
		s.vacuumAfterDetailCompression(compressedRows)
	}
	return nil
}

func (s *sqliteStore) initSchemaLocked(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS chat_history_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS chat_history (
			id TEXT PRIMARY KEY,
			revision INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			completed_at INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			caller_id TEXT NOT NULL DEFAULT '',
			account_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			stream INTEGER NOT NULL DEFAULT 0,
			user_input TEXT NOT NULL DEFAULT '',
			preview TEXT NOT NULL DEFAULT '',
			status_code INTEGER NOT NULL DEFAULT 0,
			elapsed_ms INTEGER NOT NULL DEFAULT 0,
			finish_reason TEXT NOT NULL DEFAULT '',
			detail_revision INTEGER NOT NULL,
			usage_json TEXT NOT NULL DEFAULT '',
			detail_json TEXT NOT NULL DEFAULT '',
			detail_encoding TEXT NOT NULL DEFAULT '',
			detail_blob BLOB NOT NULL DEFAULT X''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_history_updated_at ON chat_history(updated_at DESC, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_history_status ON chat_history(status)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_history_account_id ON chat_history(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_history_caller_id ON chat_history(caller_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_history_model ON chat_history(model)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("init chat history sqlite schema: %w", err)
		}
	}
	for _, column := range []sqliteColumnInfo{
		{name: "detail_encoding", ddl: `ALTER TABLE chat_history ADD COLUMN detail_encoding TEXT NOT NULL DEFAULT ''`},
		{name: "detail_blob", ddl: `ALTER TABLE chat_history ADD COLUMN detail_blob BLOB NOT NULL DEFAULT X''`},
	} {
		if err := ensureChatHistoryColumnLocked(tx, column); err != nil {
			return err
		}
	}
	return nil
}

func ensureChatHistoryColumnLocked(tx *sql.Tx, column sqliteColumnInfo) error {
	rows, err := tx.Query(`PRAGMA table_info(chat_history)`)
	if err != nil {
		return fmt.Errorf("inspect chat history sqlite schema: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan chat history sqlite schema: %w", err)
		}
		if strings.EqualFold(strings.TrimSpace(name), column.name) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan chat history sqlite schema rows: %w", err)
	}
	if _, err := tx.Exec(column.ddl); err != nil {
		return fmt.Errorf("migrate chat history sqlite schema: %w", err)
	}
	return nil
}

func (s *sqliteStore) initMetaLocked(tx *sql.Tx) error {
	if err := s.setMetaLocked(tx, "version", strconv.Itoa(FileVersion)); err != nil {
		return err
	}
	limit, err := s.metaIntLocked(tx, "limit", DefaultLimit)
	if err != nil {
		return err
	}
	if !isAllowedLimit(limit) {
		limit = DefaultLimit
	}
	if err := s.setMetaLocked(tx, "limit", strconv.Itoa(limit)); err != nil {
		return err
	}
	revision, err := s.metaInt64Locked(tx, "revision", 0)
	if err != nil {
		return err
	}
	return s.setMetaLocked(tx, "revision", strconv.FormatInt(revision, 10))
}

func (s *sqliteStore) metaIntLocked(tx *sql.Tx, key string, fallback int) (int, error) {
	raw, err := s.metaLocked(tx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback, nil
	}
	return n, nil
}

func (s *sqliteStore) metaInt64Locked(tx *sql.Tx, key string, fallback int64) (int64, error) {
	raw, err := s.metaLocked(tx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return fallback, nil
	}
	return n, nil
}

func (s *sqliteStore) metaLocked(tx *sql.Tx, key string) (string, error) {
	var value string
	err := tx.QueryRow(`SELECT value FROM chat_history_meta WHERE key = ?`, key).Scan(&value)
	return value, err
}

func (s *sqliteStore) setMetaLocked(tx *sql.Tx, key, value string) error {
	_, err := tx.Exec(
		`INSERT INTO chat_history_meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key,
		value,
	)
	if err != nil {
		return fmt.Errorf("write chat history sqlite meta: %w", err)
	}
	return nil
}

func (s *sqliteStore) nextRevisionLocked(tx *sql.Tx) (int64, error) {
	current, err := s.metaInt64Locked(tx, "revision", 0)
	if err != nil {
		return 0, err
	}
	next := time.Now().UnixNano()
	if next <= current {
		next = current + 1
	}
	if err := s.setMetaLocked(tx, "revision", strconv.FormatInt(next, 10)); err != nil {
		return 0, err
	}
	return next, nil
}
