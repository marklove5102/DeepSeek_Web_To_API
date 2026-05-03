package chathistory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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

func (s *sqliteStore) pruneLocked(tx *sql.Tx) error {
	limit, err := s.metaIntLocked(tx, "limit", DefaultLimit)
	if err != nil {
		return err
	}
	if limit == DisabledLimit {
		return nil
	}
	if !isAllowedLimit(limit) {
		limit = DefaultLimit
		if err := s.setMetaLocked(tx, "limit", strconv.Itoa(limit)); err != nil {
			return err
		}
	}
	_, err = tx.Exec(
		`DELETE FROM chat_history
		 WHERE id IN (
			SELECT id FROM chat_history
			ORDER BY updated_at DESC, created_at DESC
			LIMIT -1 OFFSET ?
		 )`,
		limit,
	)
	if err != nil {
		return fmt.Errorf("prune chat history sqlite rows: %w", err)
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
