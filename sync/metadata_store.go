package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite"
)

type MetadataStore struct {
	db *sql.DB
}

func OpenMetadataStore(path string) (*MetadataStore, error) {
	if path == "" {
		return nil, nil
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &MetadataStore{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *MetadataStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *MetadataStore) init(ctx context.Context) error {
	pragmas := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
	}
	for _, pragma := range pragmas {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS snapshots (
			snapshot_id TEXT PRIMARY KEY,
			table_name TEXT NOT NULL,
			writer TEXT,
			snapshot_scope TEXT,
			row_count INTEGER,
			manifest_key TEXT,
			timestamp TEXT NOT NULL,
			payload_json TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL,
			severity TEXT NOT NULL,
			event_type TEXT NOT NULL,
			table_name TEXT,
			status TEXT NOT NULL,
			payload_json TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS health_checks (
			component TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			checked_at TEXT NOT NULL,
			detail_json TEXT
		)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *MetadataStore) RecordSnapshot(ctx context.Context, manifest SnapshotManifest) {
	if s == nil {
		return
	}
	payload, _ := json.Marshal(manifest)
	_, _ = s.db.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO snapshots (snapshot_id, table_name, writer, snapshot_scope, row_count, manifest_key, timestamp, payload_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		manifest.SnapshotID,
		manifest.TableName,
		manifest.Writer,
		manifest.SnapshotScope,
		manifest.RowCount,
		manifest.S3ManifestKey,
		manifest.Timestamp,
		string(payload),
	)
}

func (s *MetadataStore) RecordAlert(ctx context.Context, severity, eventType, table, status string, payload any) {
	if s == nil {
		return
	}
	raw, _ := json.Marshal(payload)
	_, _ = s.db.ExecContext(
		ctx,
		`INSERT INTO alerts (timestamp, severity, event_type, table_name, status, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano),
		severity,
		eventType,
		table,
		status,
		string(raw),
	)
}

func (s *MetadataStore) RecordHealth(ctx context.Context, component, status string, detail any) {
	if s == nil {
		return
	}
	raw, _ := json.Marshal(detail)
	_, _ = s.db.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO health_checks (component, status, checked_at, detail_json) VALUES (?, ?, ?, ?)`,
		component,
		status,
		time.Now().UTC().Format(time.RFC3339Nano),
		string(raw),
	)
}
