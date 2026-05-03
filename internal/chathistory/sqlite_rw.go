package chathistory

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const sqliteReadAllSummariesQuery = `SELECT
	id, revision, created_at, updated_at, completed_at, status, caller_id,
	account_id, model, stream, user_input, preview, status_code, elapsed_ms,
	finish_reason, detail_revision, usage_json
	FROM chat_history ORDER BY updated_at DESC, created_at DESC`

const sqliteReadPageSummariesQuery = `SELECT
	id, revision, created_at, updated_at, completed_at, status, caller_id,
	account_id, model, stream, user_input, preview, status_code, elapsed_ms,
	finish_reason, detail_revision, usage_json
	FROM chat_history ORDER BY updated_at DESC, created_at DESC LIMIT ? OFFSET ?`

type sqliteSummaryScanner interface {
	Scan(dest ...any) error
}

func (s *sqliteStore) Snapshot() (File, error) {
	if s == nil {
		return File{}, errors.New("chat history sqlite store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return File{}, s.err
	}
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

func (s *sqliteStore) SnapshotPage(offset, limit int) (File, int, error) {
	if s == nil {
		return File{}, 0, errors.New("chat history sqlite store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return File{}, 0, s.err
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	total, err := s.countLocked()
	if err != nil {
		return File{}, 0, err
	}
	if offset > total {
		offset = total
	}
	items, err := s.readPageSummariesLocked(offset, limit)
	if err != nil {
		return File{}, 0, err
	}
	stateLimit, revision, err := s.stateMetaLocked()
	if err != nil {
		return File{}, 0, err
	}
	return File{Version: FileVersion, Limit: stateLimit, Revision: revision, Items: items}, total, nil
}

func (s *sqliteStore) Revision() (int64, error) {
	if s == nil {
		return 0, errors.New("chat history sqlite store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	_, revision, err := s.stateMetaLocked()
	return revision, err
}

func (s *sqliteStore) Enabled() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false
	}
	limit, _, err := s.stateMetaLocked()
	return err == nil && limit != DisabledLimit
}

func (s *sqliteStore) Get(id string) (Entry, error) {
	if s == nil {
		return Entry{}, errors.New("chat history sqlite store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return Entry{}, s.err
	}
	return s.getEntryLocked(strings.TrimSpace(id))
}

func (s *sqliteStore) DetailRevision(id string) (int64, error) {
	if s == nil {
		return 0, errors.New("chat history sqlite store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return 0, errors.New("history id is required")
	}
	var revision int64
	err := s.db.QueryRow(`SELECT detail_revision FROM chat_history WHERE id = ?`, id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errors.New("chat history entry not found")
	}
	if err != nil {
		return 0, fmt.Errorf("read chat history detail revision: %w", err)
	}
	return revision, nil
}

func (s *sqliteStore) stateMetaLocked() (int, int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("begin chat history sqlite meta read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	limit, err := s.metaIntLocked(tx, "limit", DefaultLimit)
	if err != nil {
		return 0, 0, err
	}
	revision, err := s.metaInt64Locked(tx, "revision", 0)
	if err != nil {
		return 0, 0, err
	}
	return limit, revision, nil
}

func (s *sqliteStore) countLocked() (int, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_history`).Scan(&total); err != nil {
		return 0, fmt.Errorf("count chat history sqlite rows: %w", err)
	}
	return total, nil
}

func (s *sqliteStore) readAllSummariesLocked() ([]SummaryEntry, error) {
	rows, err := s.db.Query(sqliteReadAllSummariesQuery)
	if err != nil {
		return nil, fmt.Errorf("read chat history sqlite index: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanSQLiteSummaries(rows)
}

func (s *sqliteStore) readPageSummariesLocked(offset, limit int) ([]SummaryEntry, error) {
	rows, err := s.db.Query(sqliteReadPageSummariesQuery, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("read chat history sqlite page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanSQLiteSummaries(rows)
}

func scanSQLiteSummaries(rows *sql.Rows) ([]SummaryEntry, error) {
	items := []SummaryEntry{}
	for rows.Next() {
		item, err := scanSQLiteSummary(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan chat history sqlite summaries: %w", err)
	}
	return items, nil
}

func scanSQLiteSummary(scanner sqliteSummaryScanner) (SummaryEntry, error) {
	var item SummaryEntry
	var stream int
	var usageJSON string
	if err := scanner.Scan(
		&item.ID,
		&item.Revision,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.CompletedAt,
		&item.Status,
		&item.CallerID,
		&item.AccountID,
		&item.Model,
		&stream,
		&item.UserInput,
		&item.Preview,
		&item.StatusCode,
		&item.ElapsedMs,
		&item.FinishReason,
		&item.DetailRevision,
		&usageJSON,
	); err != nil {
		return SummaryEntry{}, fmt.Errorf("scan chat history sqlite summary: %w", err)
	}
	item.Stream = stream != 0
	item.Usage = decodeUsageJSON(usageJSON)
	return item, nil
}

func (s *sqliteStore) getEntryLocked(id string) (Entry, error) {
	if id == "" {
		return Entry{}, errors.New("history id is required")
	}
	var detailJSON, detailEncoding string
	var detailBlob []byte
	err := s.db.QueryRow(`SELECT detail_json, detail_encoding, detail_blob FROM chat_history WHERE id = ?`, id).Scan(&detailJSON, &detailEncoding, &detailBlob)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, errors.New("chat history entry not found")
	}
	if err != nil {
		return Entry{}, fmt.Errorf("read chat history sqlite detail: %w", err)
	}
	return decodeSQLiteDetail(detailJSON, detailEncoding, detailBlob)
}
