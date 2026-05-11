package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
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
			payload_json TEXT,
			status TEXT NOT NULL DEFAULT 'valid',
			validation_error TEXT
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
	if err := s.ensureColumn(ctx, "snapshots", "status", "TEXT NOT NULL DEFAULT 'valid'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "snapshots", "validation_error", "TEXT"); err != nil {
		return err
	}
	return nil
}

func (s *MetadataStore) ensureColumn(ctx context.Context, table, column, typ string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+typ)
	return err
}

func (s *MetadataStore) RecordSnapshot(ctx context.Context, manifest SnapshotManifest) error {
	if s == nil {
		return nil
	}
	status := manifest.Status
	if status == "" {
		status = "valid"
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal snapshot metadata: %w", err)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO snapshots (snapshot_id, table_name, writer, snapshot_scope, row_count, manifest_key, timestamp, payload_json, status, validation_error)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			manifest.SnapshotID,
			manifest.TableName,
			manifest.Writer,
			manifest.SnapshotScope,
			manifest.RowCount,
			manifest.S3ManifestKey,
			manifest.Timestamp,
			string(payload),
			status,
			manifest.ValidationError,
		)
		return err
	})
}

func (s *MetadataStore) RecordInvalidSnapshot(ctx context.Context, manifest SnapshotManifest, validationErr string) error {
	if s == nil {
		return nil
	}
	manifest.Status = "invalid"
	manifest.ValidationError = validationErr
	if manifest.Timestamp == "" {
		manifest.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	return s.RecordSnapshot(ctx, manifest)
}

func (s *MetadataStore) QuarantineManifest(ctx context.Context, snapshotID, table, manifestKey, reason string) error {
	if s == nil {
		return nil
	}
	if snapshotID == "" {
		snapshotID = "unknown_" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	manifest := SnapshotManifest{
		SnapshotID:       snapshotID,
		TableName:        table,
		S3ManifestKey:    manifestKey,
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		Status:           "quarantined",
		ValidationError:  reason,
		Writer:           sidecarWriter,
		SnapshotScope:    "table",
		FKConstraints:    []string{},
		Indexes:          []string{},
		CheckConstraints: []string{},
	}
	return s.RecordSnapshot(ctx, manifest)
}

func (s *MetadataStore) RecordAlert(ctx context.Context, severity, eventType, table, status string, payload any) error {
	if s == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal alert metadata: %w", err)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO alerts (timestamp, severity, event_type, table_name, status, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
			time.Now().UTC().Format(time.RFC3339Nano),
			severity,
			eventType,
			table,
			status,
			string(raw),
		)
		return err
	})
}

func (s *MetadataStore) RecordHealth(ctx context.Context, component, status string, detail any) error {
	if s == nil {
		return nil
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal health metadata: %w", err)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO health_checks (component, status, checked_at, detail_json) VALUES (?, ?, ?, ?)`,
			component,
			status,
			time.Now().UTC().Format(time.RFC3339Nano),
			string(raw),
		)
		return err
	})
}

func (s *MetadataStore) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return nil
}
