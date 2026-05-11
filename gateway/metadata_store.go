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
		`CREATE TABLE IF NOT EXISTS audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			query TEXT NOT NULL,
			risk_level TEXT NOT NULL,
			approved INTEGER NOT NULL,
			query_sha256 TEXT,
			environment TEXT,
			cluster_id TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS approvals (
			approval_id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			query TEXT NOT NULL,
			risk_level TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			resolved_at TEXT,
			query_sha256 TEXT,
			operation TEXT,
			schema_name TEXT,
			table_name TEXT,
			environment TEXT,
			cluster_id TEXT,
			snapshot_id TEXT,
			resolved_by TEXT
		)`,
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
		`CREATE TABLE IF NOT EXISTS native_backups (
			backup_id TEXT PRIMARY KEY,
			backup_type TEXT NOT NULL,
			db_name TEXT,
			cluster_id TEXT,
			timestamp TEXT NOT NULL,
			payload_json TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS health_checks (
			component TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			checked_at TEXT NOT NULL,
			detail_json TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS agent_state (
			agent_id TEXT PRIMARY KEY,
			risky_attempts INTEGER NOT NULL,
			window_started_at TEXT NOT NULL,
			quarantine_until TEXT,
			last_reason TEXT,
			last_table TEXT,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn(ctx, "audit_events", "query_sha256", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "audit_events", "environment", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "audit_events", "cluster_id", "TEXT"); err != nil {
		return err
	}
	for _, col := range []struct {
		name string
		typ  string
	}{
		{"query_sha256", "TEXT"},
		{"operation", "TEXT"},
		{"schema_name", "TEXT"},
		{"table_name", "TEXT"},
		{"environment", "TEXT"},
		{"cluster_id", "TEXT"},
		{"snapshot_id", "TEXT"},
		{"resolved_by", "TEXT"},
	} {
		if err := s.ensureColumn(ctx, "approvals", col.name, col.typ); err != nil {
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
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, typ))
	return err
}

type AgentState struct {
	AgentID         string
	RiskyAttempts   int
	WindowStartedAt time.Time
	QuarantineUntil *time.Time
	LastReason      string
	LastTable       string
}

func (s *MetadataStore) GetAgentState(ctx context.Context, agentID string) (AgentState, bool) {
	if s == nil {
		return AgentState{}, false
	}
	var state AgentState
	var window, quarantine sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT agent_id, risky_attempts, window_started_at, quarantine_until, last_reason, last_table FROM agent_state WHERE agent_id = ?`, agentID).
		Scan(&state.AgentID, &state.RiskyAttempts, &window, &quarantine, &state.LastReason, &state.LastTable)
	if err != nil {
		return AgentState{}, false
	}
	if window.Valid {
		state.WindowStartedAt, _ = time.Parse(time.RFC3339Nano, window.String)
	}
	if quarantine.Valid && quarantine.String != "" {
		ts, err := time.Parse(time.RFC3339Nano, quarantine.String)
		if err == nil {
			state.QuarantineUntil = &ts
		}
	}
	return state, true
}

func (s *MetadataStore) SaveAgentState(ctx context.Context, state AgentState) error {
	if s == nil {
		return nil
	}
	quarantine := ""
	if state.QuarantineUntil != nil {
		quarantine = state.QuarantineUntil.Format(time.RFC3339Nano)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO agent_state (agent_id, risky_attempts, window_started_at, quarantine_until, last_reason, last_table, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			state.AgentID,
			state.RiskyAttempts,
			state.WindowStartedAt.Format(time.RFC3339Nano),
			quarantine,
			state.LastReason,
			state.LastTable,
			time.Now().UTC().Format(time.RFC3339Nano),
		)
		return err
	})
}

func (s *MetadataStore) RecordAudit(ctx context.Context, entry AuditEntry) error {
	if s == nil {
		return nil
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO audit_events (timestamp, agent_id, query, risk_level, approved, query_sha256, environment, cluster_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.Timestamp.Format(time.RFC3339Nano),
			entry.AgentID,
			entry.Query,
			entry.RiskLevel,
			boolInt(entry.Approved),
			entry.QuerySHA256,
			entry.Environment,
			entry.ClusterID,
		)
		return err
	})
}

func (s *MetadataStore) RecordApprovalRequested(ctx context.Context, details ApprovalDetails) error {
	if s == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO approvals
			 (approval_id, agent_id, query, risk_level, status, created_at, query_sha256, operation, schema_name, table_name, environment, cluster_id, snapshot_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			details.ID,
			details.AgentID,
			details.Query,
			details.RiskLevel,
			"pending",
			now,
			details.QuerySHA256,
			details.Operation,
			details.Schema,
			details.Table,
			details.Environment,
			details.ClusterID,
			details.SnapshotID,
		)
		return err
	})
}

func (s *MetadataStore) RecordApprovalResolved(ctx context.Context, approvalID, status, actor string) error {
	if s == nil {
		return nil
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			`UPDATE approvals SET status = ?, resolved_at = ?, resolved_by = ? WHERE approval_id = ?`,
			status,
			time.Now().UTC().Format(time.RFC3339Nano),
			actor,
			approvalID,
		)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("approval metadata update affected %d rows for approval_id=%s", rows, approvalID)
		}
		return nil
	})
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
		SnapshotID:      snapshotID,
		TableName:       table,
		S3ManifestKey:   manifestKey,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Status:          "quarantined",
		ValidationError: reason,
		Writer:          sidecarWriter,
		SnapshotScope:   "table",
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

func (s *MetadataStore) QueryRows(ctx context.Context, table string, filters map[string]string) ([]map[string]any, error) {
	if s == nil {
		return []map[string]any{}, nil
	}
	query := "SELECT * FROM " + table
	args := []any{}
	clauses := []string{}
	for column, value := range filters {
		if value == "" {
			continue
		}
		clauses = append(clauses, column+" = ?")
		args = append(args, value)
	}
	if len(clauses) > 0 {
		query += " WHERE " + joinAnd(clauses)
	}
	query += " ORDER BY 1 DESC LIMIT 500"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return rowsToMaps(rows)
}

func (s *MetadataStore) QueryAudit(ctx context.Context, agentID, risk string) ([]map[string]any, error) {
	return s.queryControlledRows(ctx, "audit_events", "timestamp", map[string]string{
		"agent_id":   agentID,
		"risk_level": risk,
	})
}

func (s *MetadataStore) QuerySnapshots(ctx context.Context, table string, latestValidOnly bool) ([]map[string]any, error) {
	if !latestValidOnly {
		return s.queryControlledRows(ctx, "snapshots", "timestamp", map[string]string{
			"table_name": table,
		})
	}
	if s == nil {
		return []map[string]any{}, nil
	}
	query := `SELECT * FROM snapshots s WHERE status = 'valid'`
	args := []any{}
	if table != "" {
		query += ` AND s.table_name = ?`
		args = append(args, table)
	}
	query += ` AND s.timestamp = (SELECT MAX(s2.timestamp) FROM snapshots s2 WHERE s2.table_name = s.table_name AND s2.status = 'valid') ORDER BY s.timestamp DESC LIMIT 500`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return rowsToMaps(rows)
}

func (s *MetadataStore) ValidateSnapshotRecord(ctx context.Context, manifest SnapshotManifest) error {
	if s == nil {
		return fmt.Errorf("metadata store is unavailable")
	}
	var tableName, manifestKey, status, payloadJSON string
	err := s.db.QueryRowContext(ctx, `SELECT table_name, manifest_key, status, payload_json FROM snapshots WHERE snapshot_id = ?`, manifest.SnapshotID).
		Scan(&tableName, &manifestKey, &status, &payloadJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("metadata state is ambiguous: snapshot %s is missing from SQLite metadata", manifest.SnapshotID)
		}
		return fmt.Errorf("metadata snapshot lookup failed for %s: %w", manifest.SnapshotID, err)
	}
	if status != "valid" {
		return fmt.Errorf("metadata snapshot %s is not valid: status=%s", manifest.SnapshotID, status)
	}
	if tableName != manifest.TableName {
		return fmt.Errorf("metadata snapshot table mismatch: metadata=%s manifest=%s", tableName, manifest.TableName)
	}
	if manifestKey != manifest.S3ManifestKey {
		return fmt.Errorf("metadata snapshot manifest_key mismatch: metadata=%s manifest=%s", manifestKey, manifest.S3ManifestKey)
	}
	var recorded SnapshotManifest
	if err := json.Unmarshal([]byte(payloadJSON), &recorded); err != nil {
		return fmt.Errorf("metadata snapshot payload is corrupt for %s: %w", manifest.SnapshotID, err)
	}
	if recorded.SnapshotID != manifest.SnapshotID ||
		recorded.TableName != manifest.TableName ||
		recorded.S3ManifestKey != manifest.S3ManifestKey ||
		recorded.S3DataKey != manifest.S3DataKey ||
		recorded.DataSHA256 != manifest.DataSHA256 ||
		recorded.Status != "valid" {
		return fmt.Errorf("metadata snapshot payload does not match verified manifest for %s", manifest.SnapshotID)
	}
	return nil
}

func (s *MetadataStore) QueryAlerts(ctx context.Context) ([]map[string]any, error) {
	return s.queryControlledRows(ctx, "alerts", "timestamp", nil)
}

func (s *MetadataStore) QueryHealth(ctx context.Context) ([]map[string]any, error) {
	return s.queryControlledRows(ctx, "health_checks", "checked_at", nil)
}

func (s *MetadataStore) GetHealth(ctx context.Context, component string) (string, time.Time, bool) {
	if s == nil {
		return "", time.Time{}, false
	}
	var status, checked string
	err := s.db.QueryRowContext(ctx, `SELECT status, checked_at FROM health_checks WHERE component = ?`, component).Scan(&status, &checked)
	if err != nil {
		return "", time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, checked)
	if err != nil {
		return status, time.Time{}, false
	}
	return status, ts, true
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

func (s *MetadataStore) queryControlledRows(ctx context.Context, table, orderBy string, filters map[string]string) ([]map[string]any, error) {
	if s == nil {
		return []map[string]any{}, nil
	}
	query := fmt.Sprintf("SELECT * FROM %s", table)
	args := []any{}
	clauses := []string{}
	for column, value := range filters {
		if value == "" {
			continue
		}
		clauses = append(clauses, column+" = ?")
		args = append(args, value)
	}
	if len(clauses) > 0 {
		query += " WHERE " + joinAnd(clauses)
	}
	query += fmt.Sprintf(" ORDER BY %s DESC LIMIT 500", orderBy)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return rowsToMaps(rows)
}

func rowsToMaps(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, col := range cols {
			switch v := values[i].(type) {
			case []byte:
				row[col] = string(v)
			default:
				row[col] = v
			}
		}
		out = append(out, row)
	}
	if out == nil {
		return []map[string]any{}, nil
	}
	return out, rows.Err()
}

func joinAnd(values []string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += " AND "
		}
		result += value
	}
	return result
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
