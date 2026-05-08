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

func (s *MetadataStore) SaveAgentState(ctx context.Context, state AgentState) {
	if s == nil {
		return
	}
	quarantine := ""
	if state.QuarantineUntil != nil {
		quarantine = state.QuarantineUntil.Format(time.RFC3339Nano)
	}
	_, _ = s.db.ExecContext(
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
}

func (s *MetadataStore) RecordAudit(ctx context.Context, entry AuditEntry) {
	if s == nil {
		return
	}
	_, _ = s.db.ExecContext(
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
}

func (s *MetadataStore) RecordApprovalRequested(ctx context.Context, details ApprovalDetails) {
	if s == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = s.db.ExecContext(
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
}

func (s *MetadataStore) RecordApprovalResolved(ctx context.Context, approvalID, status, actor string) {
	if s == nil {
		return
	}
	_, _ = s.db.ExecContext(
		ctx,
		`UPDATE approvals SET status = ?, resolved_at = ?, resolved_by = ? WHERE approval_id = ?`,
		status,
		time.Now().UTC().Format(time.RFC3339Nano),
		actor,
		approvalID,
	)
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

func (s *MetadataStore) QuerySnapshots(ctx context.Context, table string) ([]map[string]any, error) {
	return s.queryControlledRows(ctx, "snapshots", "timestamp", map[string]string{
		"table_name": table,
	})
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
