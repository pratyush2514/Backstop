package main

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// Poller snapshots tables, detects table drops, and alerts with recovery info.
type Poller struct {
	db              *sql.DB
	schema          string
	interval        time.Duration
	tracker         *DeltaTracker
	alerter         *AlertEngine
	snapshotEngine  *SnapshotEngine
	snapshotOnStart bool
	metrics         *SyncMetrics
	metadata        *MetadataStore
	bypassDetector  *BypassDetector
	maxSnapshotAge  time.Duration
	maxFailures     int
	failures        map[string]int
	staleAlerted    map[string]bool
}

// NewPoller constructs a Poller.
func NewPoller(db *sql.DB, schema string, interval time.Duration, tracker *DeltaTracker, alerter *AlertEngine, snapshotEngine *SnapshotEngine, snapshotOnStart bool) *Poller {
	return &Poller{
		db:              db,
		schema:          schema,
		interval:        interval,
		tracker:         tracker,
		alerter:         alerter,
		snapshotEngine:  snapshotEngine,
		snapshotOnStart: snapshotOnStart,
		metrics:         NewSyncMetrics(),
		maxFailures:     3,
		failures:        make(map[string]int),
		staleAlerted:    make(map[string]bool),
	}
}

func (p *Poller) ConfigureLaunchReadiness(metrics *SyncMetrics, metadata *MetadataStore, maxSnapshotAge time.Duration, maxFailures int) {
	if metrics != nil {
		p.metrics = metrics
	}
	p.metadata = metadata
	p.maxSnapshotAge = maxSnapshotAge
	if maxFailures > 0 {
		p.maxFailures = maxFailures
	}
}

func (p *Poller) ConfigureBypassDetector(detector *BypassDetector) {
	p.bypassDetector = detector
}

// Run starts the polling loop. It blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	slog.Info("Poller started", "interval", p.interval, "schema", p.schema)
	p.poll(ctx, true)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Poller stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			p.poll(ctx, false)
		}
	}
}

func (p *Poller) poll(ctx context.Context, startup bool) {
	tables, err := p.fetchTables(ctx)
	if err != nil {
		slog.Error("Failed to fetch table list from pg_catalog", "error", err)
		if p.metadata != nil {
			p.metadata.RecordHealth(ctx, "sync", "unhealthy", map[string]any{"error": err.Error()})
		}
		return
	}
	if p.metadata != nil {
		p.metadata.RecordHealth(ctx, "sync", "healthy", map[string]any{"table_count": len(tables)})
	}
	p.pollBypass(ctx)

	dropped, added := p.tracker.Update(tables)
	if len(added) > 0 {
		slog.Info("New tables detected", "tables", added)
	}

	if startup {
		if p.snapshotOnStart {
			for _, table := range tables {
				p.snapshotTable(ctx, table)
			}
		}
	} else {
		for _, table := range added {
			p.snapshotTable(ctx, table)
		}
		for _, table := range tables {
			if contains(added, table) {
				continue
			}
			p.snapshotTable(ctx, table)
		}
	}

	for _, table := range dropped {
		latest, ok := SnapshotManifest{}, false
		if p.snapshotEngine != nil {
			latest, ok = p.snapshotEngine.Latest(table)
		}
		var manifest *SnapshotManifest
		if ok {
			manifest = &latest
			slog.Warn("Table drop detected", "table", table, "recovery_point_available", true, "snapshot_id", latest.SnapshotID, "manifest", latest.S3ManifestKey)
		} else {
			slog.Warn("Table drop detected", "table", table, "recovery_point_available", false)
		}
		if alertErr := p.alerter.SendDropAlert(ctx, table, manifest); alertErr != nil {
			slog.Error("Failed to send drop alert", "table", table, "error", alertErr)
		}
		if p.metrics != nil {
			p.metrics.IncDropped()
		}
	}

	p.checkSnapshotStaleness(ctx, tables)
	slog.Debug("Poll complete", "table_count", len(tables), "dropped", len(dropped), "added", len(added))
}

func (p *Poller) pollBypass(ctx context.Context) {
	if p.bypassDetector == nil {
		if p.metrics != nil {
			p.metrics.SetPosture(PreventionRecoveryOnly)
		}
		return
	}
	findings, err := p.bypassDetector.Poll(ctx)
	if err != nil {
		slog.Error("Bypass detector failed", "error", err)
		if p.metadata != nil {
			p.metadata.RecordHealth(ctx, "prevention", PreventionRecoveryOnly, map[string]any{"error": err.Error()})
		}
		if p.metrics != nil {
			p.metrics.SetPosture(PreventionRecoveryOnly)
		}
		return
	}
	posture := PreventionHealthy
	if len(findings) > 0 {
		posture = PreventionDegraded
	}
	for _, finding := range findings {
		if p.metrics != nil {
			p.metrics.IncBypass(finding.Activity.Role, finding.Activity.ApplicationName)
		}
		if p.metadata != nil {
			p.metadata.RecordAlert(ctx, "critical", "gateway_bypass_detected", "", "logged", finding)
		}
	}
	if p.metrics != nil {
		p.metrics.SetPosture(posture)
	}
	if p.metadata != nil {
		p.metadata.RecordHealth(ctx, "prevention", posture, map[string]any{"findings": len(findings)})
	}
}

func (p *Poller) snapshotTable(ctx context.Context, table string) {
	if p.snapshotEngine == nil {
		slog.Warn("Snapshot skipped because snapshot engine is not configured", "table", table)
		return
	}
	manifest, err := p.snapshotEngine.CaptureTable(ctx, table)
	if err != nil {
		slog.Error("Snapshot failed", "table", table, "error", err)
		if p.metrics != nil {
			p.metrics.IncSnapshot("failed")
		}
		p.failures[table]++
		if p.alerter != nil && looksLikeStorageFailure(err) {
			if alertErr := p.alerter.SendStorageFailureAlert(ctx, table, err); alertErr != nil {
				slog.Error("Failed to send storage failure alert", "table", table, "error", alertErr)
			}
		}
		if p.failures[table] >= p.maxFailures && p.alerter != nil {
			if alertErr := p.alerter.SendSnapshotFailureAlert(ctx, table, p.failures[table], err); alertErr != nil {
				slog.Error("Failed to send snapshot failure alert", "table", table, "error", alertErr)
			}
		}
		return
	}
	p.failures[table] = 0
	p.staleAlerted[table] = false
	if p.metadata != nil {
		p.metadata.RecordSnapshot(ctx, manifest)
	}
	if p.metrics != nil {
		p.metrics.IncSnapshot("success")
		p.metrics.AddRows(manifest.RowCount)
		if ts, err := time.Parse(time.RFC3339, manifest.Timestamp); err == nil {
			p.metrics.MarkLatest(table, ts)
		} else {
			p.metrics.MarkLatest(table, time.Now().UTC())
		}
	}
	slog.Info("Recovery point available", "table", table, "snapshot_id", manifest.SnapshotID, "manifest", manifest.S3ManifestKey, "rows", manifest.RowCount)
}

func looksLikeStorageFailure(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	for _, marker := range []string{"s3", "putobject", "bucket", "storage", "upload", "access denied", "connection refused"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func (p *Poller) checkSnapshotStaleness(ctx context.Context, tables []string) {
	if p.maxSnapshotAge <= 0 || p.snapshotEngine == nil || p.alerter == nil {
		return
	}
	now := time.Now().UTC()
	for _, table := range tables {
		latest, ok := p.snapshotEngine.Latest(table)
		if !ok || latest.Timestamp == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, latest.Timestamp)
		if err != nil {
			continue
		}
		age := now.Sub(ts)
		if age <= p.maxSnapshotAge {
			p.staleAlerted[table] = false
			continue
		}
		if p.staleAlerted[table] {
			continue
		}
		manifest := latest
		if alertErr := p.alerter.SendStaleSnapshotAlert(ctx, table, age, p.maxSnapshotAge, &manifest); alertErr != nil {
			slog.Error("Failed to send stale snapshot alert", "table", table, "error", alertErr)
		}
		p.staleAlerted[table] = true
	}
}

// fetchTables queries pg_catalog.pg_tables for all tables in the configured schema.
func (p *Poller) fetchTables(ctx context.Context) ([]string, error) {
	const query = `
		SELECT tablename
		FROM pg_catalog.pg_tables
		WHERE schemaname = $1
		ORDER BY tablename`

	rows, err := p.db.QueryContext(ctx, query, p.schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
