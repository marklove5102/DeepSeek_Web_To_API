package chathistory

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"DeepSeek_Web_To_API/internal/config"
)

const sqliteDetailEncodingGzip = "gzip"
const sqliteDetailCompressionBatchSize = 100

var sqliteDetailMaxInflatedBytes int64 = 256 << 20

func encodeSQLiteDetail(item Entry) (string, string, []byte, error) {
	detailJSON, err := json.Marshal(item)
	if err != nil {
		return "", "", nil, fmt.Errorf("encode chat history sqlite detail: %w", err)
	}
	detailBlob, err := gzipSQLiteDetail(detailJSON)
	if err != nil {
		return "", "", nil, fmt.Errorf("compress chat history sqlite detail: %w", err)
	}
	return "", sqliteDetailEncodingGzip, detailBlob, nil
}

func decodeSQLiteDetail(detailJSON, detailEncoding string, detailBlob []byte) (Entry, error) {
	var raw []byte
	switch strings.ToLower(strings.TrimSpace(detailEncoding)) {
	case sqliteDetailEncodingGzip:
		inflated, err := gunzipSQLiteDetail(detailBlob)
		if err != nil {
			return Entry{}, fmt.Errorf("decompress chat history sqlite detail: %w", err)
		}
		raw = inflated
	default:
		raw = []byte(detailJSON)
	}
	var item Entry
	if err := json.Unmarshal(raw, &item); err != nil {
		return Entry{}, fmt.Errorf("decode chat history sqlite detail: %w", err)
	}
	return cloneEntry(item), nil
}

func gzipSQLiteDetail(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipSQLiteDetail(raw []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	limit := sqliteDetailMaxInflatedBytes
	if limit <= 0 {
		limit = 1
	}
	out, err := io.ReadAll(io.LimitReader(zr, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(out)) > limit {
		return nil, fmt.Errorf("inflated detail exceeds %d bytes", limit)
	}
	return out, nil
}

func (s *sqliteStore) compressExistingDetailsBatchedLocked() (int, error) {
	compressed := 0
	for {
		tx, err := s.db.Begin()
		if err != nil {
			return compressed, fmt.Errorf("begin chat history sqlite detail compression: %w", err)
		}
		rows, err := s.compressExistingDetailsBatchLocked(tx)
		if err != nil {
			_ = tx.Rollback()
			return compressed, err
		}
		if err := tx.Commit(); err != nil {
			return compressed, fmt.Errorf("commit chat history sqlite detail compression: %w", err)
		}
		if rows == 0 {
			return compressed, nil
		}
		compressed += rows
		s.checkpointAfterDetailCompression()
	}
}

func (s *sqliteStore) compressExistingDetailsBatchLocked(tx *sql.Tx) (int, error) {
	type pendingDetail struct {
		id         string
		detailJSON string
	}
	rows, err := tx.Query(
		`SELECT id, detail_json
			 FROM chat_history
			 WHERE detail_encoding = '' AND length(detail_json) > 0
			 ORDER BY updated_at DESC, created_at DESC
			 LIMIT ?`,
		sqliteDetailCompressionBatchSize,
	)
	if err != nil {
		return 0, fmt.Errorf("read uncompressed chat history sqlite details: %w", err)
	}
	pending := []pendingDetail{}
	for rows.Next() {
		var item pendingDetail
		if err := rows.Scan(&item.id, &item.detailJSON); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan uncompressed chat history sqlite detail: %w", err)
		}
		pending = append(pending, item)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close uncompressed chat history sqlite details: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("scan uncompressed chat history sqlite details: %w", err)
	}
	for _, item := range pending {
		blob, err := gzipSQLiteDetail([]byte(item.detailJSON))
		if err != nil {
			return 0, fmt.Errorf("compress existing chat history sqlite detail: %w", err)
		}
		if _, err := tx.Exec(
			`UPDATE chat_history
				 SET detail_json = '', detail_encoding = ?, detail_blob = ?
				 WHERE id = ?`,
			sqliteDetailEncodingGzip,
			blob,
			item.id,
		); err != nil {
			return 0, fmt.Errorf("write compressed chat history sqlite detail: %w", err)
		}
	}
	return len(pending), nil
}

func (s *sqliteStore) checkpointAfterDetailCompression() {
	if _, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		config.Logger.Warn("[chat_history] SQLite checkpoint after detail compression failed", "path", s.path, "error", err)
	}
}

func (s *sqliteStore) vacuumAfterDetailCompression(rows int) {
	if s == nil || s.db == nil || rows <= 0 {
		return
	}
	config.Logger.Info("[chat_history] compressed SQLite detail rows", "path", s.path, "rows", rows, "compression", sqliteDetailEncodingGzip)
	for _, stmt := range []string{
		"PRAGMA wal_checkpoint(TRUNCATE)",
		"VACUUM",
		"PRAGMA wal_checkpoint(TRUNCATE)",
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			config.Logger.Warn("[chat_history] SQLite compact after detail compression failed", "path", s.path, "statement", stmt, "error", err)
			return
		}
	}
	config.Logger.Info("[chat_history] SQLite compact after detail compression completed", "path", s.path)
}
