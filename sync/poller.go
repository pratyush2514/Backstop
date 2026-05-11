package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// Poller snapshots tables, detects table drops, and alerts with recovery info.
type Poller struct {
	db                *sql.DB
	schema            string
	interval          time.Duration
	tracker           *DeltaTracker
	alerter           *AlertEngine
	snapshotEngine    *SnapshotEngine
	snapshotOnStart   bool
	metrics           *SyncMetrics
	metadata          *MetadataStore
	bypassDetector    *BypassDetector
	maxSnapshotAge    time.Duration
	maxFailures       int
	failures          map[string]int
	staleAlerted      map[string]bool
	fingerprints      map[string]string
	stablePolls       map[string]int
	snapshotEveryPoll bool
	tableAllowlist    map[string]struct{}
	startupGrace      time.Duration
	minStablePolls    int
	pauseFile         string
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
		fingerprints:    make(map[string]string),
		stablePolls:     make(map[string]int),
		minStablePolls:  2,
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

func (p *Poller) ConfigureSnapshotStrategy(snapshotEveryPoll bool) {
	p.snapshotEveryPoll = snapshotEveryPoll
}

func (p *Poller) ConfigureTableAllowlist(tables []string) {
	if len(tables) == 0 {
		p.tableAllowlist = nil
		return
	}
	p.tableAllowlist = make(map[string]struct{}, len(tables))
	for _, table := range tables {
		table = strings.TrimSpace(table)
		if table != "" {
			p.tableAllowlist[table] = struct{}{}
		}
	}
}

func (p *Poller) ConfigureStability(startupGrace time.Duration, minStablePolls int, pauseFile string) {
	p.startupGrace = startupGrace
	if minStablePolls > 0 {
		p.minStablePolls = minStablePolls
	}
	p.pauseFile = strings.TrimSpace(pauseFile)
}

// Run starts the polling loop. It blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	slog.Info("Poller started", "interval", p.interval, "schema", p.schema)
	if p.metrics != nil {
		p.metrics.SetHealth("starting", map[string]any{"reason": "startup_grace", "schema": p.schema})
	}
	if p.startupGrace > 0 {
		slog.Info("Delaying first snapshot poll for startup grace period", "duration", p.startupGrace)
		select {
		case <-ctx.Done():
			return
		case <-time.After(p.startupGrace):
		}
	}
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
			if metaErr := p.metadata.RecordHealth(ctx, "sync", "unhealthy", map[string]any{"error": err.Error()}); metaErr != nil {
				slog.Error("Failed to record unhealthy sync metadata", "error", metaErr)
			}
		}
		return
	}
	tables = p.filterTables(tables)
	if p.metadata != nil {
		if metaErr := p.metadata.RecordHealth(ctx, "sync", "starting", map[string]any{"phase": "catalog_scanned", "table_count": len(tables)}); metaErr != nil {
			slog.Error("Failed to record sync metadata", "error", metaErr)
			if p.metrics != nil {
				p.metrics.SetHealth("unhealthy", map[string]any{"reason": "metadata_write_failed", "error": metaErr.Error()})
			}
			return
		}
	}
	p.pollBypass(ctx)
	if p.snapshotPaused() {
		detail := map[string]any{"reason": "snapshot_pause_file", "pause_file": p.pauseFile, "table_count": len(tables)}
		if p.metrics != nil {
			p.metrics.SetHealth("paused", detail)
		}
		if p.metadata != nil {
			if metaErr := p.metadata.RecordHealth(ctx, "sync", "paused", detail); metaErr != nil {
				slog.Error("Failed to record paused sync metadata", "error", metaErr)
			}
		}
		slog.Warn("Snapshot capture paused", "pause_file", p.pauseFile, "table_count", len(tables))
		return
	}

	dropped, added := p.tracker.Update(tables)
	if len(added) > 0 {
		slog.Info("New tables detected", "tables", added)
	}
	fingerprints, fingerprintErr := p.fetchTableFingerprints(ctx)
	if fingerprintErr != nil {
		slog.Warn("Failed to fetch table change fingerprints; snapshot retries and new tables still run", "error", fingerprintErr)
	}

	if startup {
		if p.snapshotOnStart {
			for _, table := range tables {
				if p.tableStableForSnapshot(table, fingerprints, fingerprintErr) {
					p.snapshotTable(ctx, table)
				}
			}
		}
	} else {
		for _, table := range added {
			if p.tableStableForSnapshot(table, fingerprints, fingerprintErr) {
				p.snapshotTable(ctx, table)
			}
		}
		for _, table := range tables {
			if contains(added, table) {
				continue
			}
			currentFingerprint := fingerprints[table]
			previousFingerprint := p.fingerprints[table]
			hasRecoveryPoint := false
			if p.snapshotEngine != nil {
				_, hasRecoveryPoint = p.snapshotEngine.Latest(table)
			}
			stable := p.tableStableForSnapshot(table, fingerprints, fingerprintErr)
			changed := fingerprintErr != nil || previousFingerprint == "" || currentFingerprint != previousFingerprint
			needsRetry := p.failures[table] > 0
			if p.snapshotEveryPoll || (stable && (!hasRecoveryPoint || changed || needsRetry)) {
				p.snapshotTable(ctx, table)
			} else {
				if stable && hasRecoveryPoint && !changed && !needsRetry {
					p.refreshSnapshotVerification(ctx, table)
				}
				slog.Debug("Snapshot skipped", "table", table, "stable", stable, "has_recovery_point", hasRecoveryPoint, "changed", changed, "needs_retry", needsRetry)
			}
		}
	}
	if fingerprintErr == nil {
		p.fingerprints = fingerprints
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
	p.updateRecoveryReadiness(ctx, tables)
	slog.Debug("Poll complete", "table_count", len(tables), "dropped", len(dropped), "added", len(added))
}

func (p *Poller) snapshotPaused() bool {
	if p.pauseFile == "" {
		return false
	}
	_, err := os.Stat(p.pauseFile)
	return err == nil
}

func (p *Poller) tableStableForSnapshot(table string, fingerprints map[string]string, fingerprintErr error) bool {
	if p.snapshotEveryPoll || fingerprintErr != nil {
		return true
	}
	current := fingerprints[table]
	if current == "" {
		p.stablePolls[table] = 0
		return false
	}
	if p.fingerprints[table] != "" && p.fingerprints[table] == current {
		p.stablePolls[table]++
	} else {
		p.stablePolls[table] = 0
	}
	if p.stablePolls[table] < p.minStablePolls {
		slog.Info("Snapshot deferred until table fingerprint is stable", "table", table, "stable_polls", p.stablePolls[table], "required", p.minStablePolls)
		return false
	}
	return true
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
			if metaErr := p.metadata.RecordHealth(ctx, "prevention", PreventionRecoveryOnly, map[string]any{"error": err.Error()}); metaErr != nil {
				slog.Error("Failed to record prevention metadata", "error", metaErr)
			}
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
			if metaErr := p.metadata.RecordAlert(ctx, "critical", "gateway_bypass_detected", "", "logged", finding); metaErr != nil {
				slog.Error("Failed to record bypass alert metadata", "error", metaErr)
			}
		}
	}
	if p.metrics != nil {
		p.metrics.SetPosture(posture)
	}
	if p.metadata != nil {
		if metaErr := p.metadata.RecordHealth(ctx, "prevention", posture, map[string]any{"findings": len(findings)}); metaErr != nil {
			slog.Error("Failed to record prevention metadata", "error", metaErr)
		}
	}
}

func (p *Poller) filterTables(tables []string) []string {
	if len(p.tableAllowlist) == 0 {
		return tables
	}
	filtered := make([]string, 0, len(tables))
	for _, table := range tables {
		if _, ok := p.tableAllowlist[table]; ok {
			filtered = append(filtered, table)
		}
	}
	return filtered
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
		if metaErr := p.metadata.RecordSnapshot(ctx, manifest); metaErr != nil {
			slog.Error("Snapshot metadata write failed", "table", table, "snapshot_id", manifest.SnapshotID, "error", metaErr)
			p.failures[table]++
			if p.metrics != nil {
				p.metrics.IncSnapshot("metadata_failed")
				p.metrics.SetHealth("unhealthy", map[string]any{
					"reason":      "snapshot_metadata_write_failed",
					"table":       table,
					"snapshot_id": manifest.SnapshotID,
					"error":       metaErr.Error(),
				})
			}
			return
		}
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

func (p *Poller) refreshSnapshotVerification(ctx context.Context, table string) {
	if p.snapshotEngine == nil {
		return
	}
	manifest, err := p.snapshotEngine.VerifyLatest(ctx, table)
	if err != nil {
		slog.Error("Snapshot verification refresh failed", "table", table, "error", err)
		p.failures[table]++
		if p.metrics != nil {
			p.metrics.IncSnapshot("verification_failed")
		}
		return
	}
	p.failures[table] = 0
	p.staleAlerted[table] = false
	if p.metadata != nil {
		if metaErr := p.metadata.RecordSnapshot(ctx, manifest); metaErr != nil {
			slog.Error("Snapshot verification metadata write failed", "table", table, "snapshot_id", manifest.SnapshotID, "error", metaErr)
			p.failures[table]++
			if p.metrics != nil {
				p.metrics.IncSnapshot("metadata_failed")
				p.metrics.SetHealth("unhealthy", map[string]any{
					"reason":      "snapshot_verification_metadata_write_failed",
					"table":       table,
					"snapshot_id": manifest.SnapshotID,
					"error":       metaErr.Error(),
				})
			}
			return
		}
	}
	if p.metrics != nil {
		if ts, err := time.Parse(time.RFC3339, manifest.VerifiedAt); err == nil {
			p.metrics.MarkLatest(table, ts)
		}
	}
}

func (p *Poller) updateRecoveryReadiness(ctx context.Context, tables []string) {
	missing := make([]string, 0)
	failing := make([]string, 0)
	stale := make([]string, 0)
	now := time.Now().UTC()
	for _, table := range tables {
		if p.failures[table] > 0 {
			failing = append(failing, table)
		}
		latest, ok := SnapshotManifest{}, false
		if p.snapshotEngine != nil {
			latest, ok = p.snapshotEngine.Latest(table)
		}
		if !ok || latest.Status != "valid" || latest.DataSHA256 == "" {
			missing = append(missing, table)
			continue
		}
		if p.maxSnapshotAge > 0 {
			freshnessTime := latest.VerifiedAt
			if freshnessTime == "" {
				freshnessTime = latest.Timestamp
			}
			ts, err := time.Parse(time.RFC3339, freshnessTime)
			if err != nil || now.Sub(ts) > p.maxSnapshotAge {
				stale = append(stale, table)
			}
		}
	}
	status := "healthy"
	if len(missing) > 0 || len(failing) > 0 || len(stale) > 0 {
		status = "degraded"
	}
	detail := map[string]any{
		"schema":        p.schema,
		"table_count":   len(tables),
		"missing":       missing,
		"failing":       failing,
		"stale":         stale,
		"max_age":       p.maxSnapshotAge.String(),
		"validity_rule": "all discovered tables require a valid checksummed sidecar snapshot",
	}
	if p.metrics != nil {
		p.metrics.SetHealth(status, detail)
	}
	if p.metadata != nil {
		if metaErr := p.metadata.RecordHealth(ctx, "sync", status, detail); metaErr != nil {
			slog.Error("Failed to record recovery readiness metadata", "status", status, "error", metaErr)
			if p.metrics != nil {
				p.metrics.SetHealth("unhealthy", map[string]any{"reason": "metadata_write_failed", "error": metaErr.Error()})
			}
		}
	}
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

func (p *Poller) fetchTableFingerprints(ctx context.Context) (map[string]string, error) {
	const query = `
		SELECT
			c.relname,
			COALESCE(s.n_tup_ins, 0),
			COALESCE(s.n_tup_upd, 0),
			COALESCE(s.n_tup_del, 0),
			COALESCE(s.n_live_tup, 0),
			COALESCE(s.n_dead_tup, 0)
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_catalog.pg_stat_all_tables s ON s.relid = c.oid
		WHERE n.nspname = $1
		  AND c.relkind IN ('r', 'p')
		ORDER BY c.relname`

	rows, err := p.db.QueryContext(ctx, query, p.schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fingerprints := make(map[string]string)
	for rows.Next() {
		var table string
		var inserted, updated, deleted, live, dead int64
		if err := rows.Scan(&table, &inserted, &updated, &deleted, &live, &dead); err != nil {
			return nil, err
		}
		fingerprints[table] = strings.Join([]string{
			table,
			int64String(inserted),
			int64String(updated),
			int64String(deleted),
			int64String(live),
			int64String(dead),
		}, ":")
	}
	return fingerprints, rows.Err()
}

func int64String(value int64) string {
	return strconv.FormatInt(value, 10)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
