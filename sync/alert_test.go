package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSendDropAlertIncludesRecoveryPoint(t *testing.T) {
	var payload webhookPayload
	alerter := NewAlertEngine("http://backstop.test/alert")
	alerter.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		return jsonResponse(http.StatusAccepted), nil
	})}

	manifest := &SnapshotManifest{
		SnapshotID:    "snap_1234abcd",
		RowCount:      42,
		S3ManifestKey: "backstop/snapshots/users/snap_1234abcd/manifest.json",
	}
	if err := alerter.SendDropAlert(context.Background(), "users", manifest); err != nil {
		t.Fatalf("SendDropAlert returned error: %v", err)
	}

	if !payload.RecoveryPointAvailable {
		t.Fatalf("recovery_point_available = false, want true")
	}
	if payload.SnapshotID != manifest.SnapshotID {
		t.Fatalf("snapshot_id = %q, want %q", payload.SnapshotID, manifest.SnapshotID)
	}
	if payload.ManifestKey != manifest.S3ManifestKey {
		t.Fatalf("manifest_key = %q, want %q", payload.ManifestKey, manifest.S3ManifestKey)
	}
	if payload.Rows != manifest.RowCount {
		t.Fatalf("rows = %d, want %d", payload.Rows, manifest.RowCount)
	}
}

func TestSendDropAlertWithoutRecoveryPoint(t *testing.T) {
	var payload webhookPayload
	alerter := NewAlertEngine("http://backstop.test/alert")
	alerter.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		return jsonResponse(http.StatusOK), nil
	})}

	if err := alerter.SendDropAlert(context.Background(), "users", nil); err != nil {
		t.Fatalf("SendDropAlert returned error: %v", err)
	}
	if payload.RecoveryPointAvailable {
		t.Fatalf("recovery_point_available = true, want false")
	}
	if payload.SnapshotID != "" || payload.ManifestKey != "" || payload.Rows != 0 {
		t.Fatalf("unexpected recovery payload: %+v", payload)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}
}

func TestAlertRecordsMetadata(t *testing.T) {
	store, err := OpenMetadataStore(t.TempDir() + "/backstop.db")
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	defer store.Close()

	alerter := NewAlertEngineWithMetadata("", store)
	if err := alerter.SendDropAlert(context.Background(), "users", nil); err != nil {
		t.Fatalf("SendDropAlert returned error: %v", err)
	}

	var eventType string
	if err := store.db.QueryRow("SELECT event_type FROM alerts WHERE table_name = ?", "users").Scan(&eventType); err != nil {
		t.Fatalf("query alert metadata: %v", err)
	}
	if eventType != "table_drop_detected" {
		t.Fatalf("event_type = %q", eventType)
	}
}

func TestSyncMetricsPrometheus(t *testing.T) {
	metrics := NewSyncMetrics()
	metrics.IncSnapshot("success")
	metrics.AddRows(3)
	metrics.MarkLatest("users", time.Now().Add(-2*time.Minute))
	metrics.IncDropped()

	text := metrics.Prometheus(time.Now())
	for _, want := range []string{
		`backstop_sync_snapshots_total{status="success"} 1`,
		`backstop_sync_snapshot_rows_total 3`,
		`backstop_sync_dropped_tables_total 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics missing %q:\n%s", want, text)
		}
	}
}

func TestMetadataStoreRecordsSnapshot(t *testing.T) {
	store, err := OpenMetadataStore(t.TempDir() + "/backstop.db")
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	defer store.Close()

	if err := store.RecordSnapshot(context.Background(), SnapshotManifest{
		SnapshotID:    "snap_1234abcd",
		TableName:     "users",
		Writer:        sidecarWriter,
		SnapshotScope: "table",
		RowCount:      2,
		S3ManifestKey: "backstop/snapshots/users/snap_1234abcd/manifest.json",
		Timestamp:     "2026-05-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("record snapshot: %v", err)
	}

	var rows sql.NullInt64
	if err := store.db.QueryRow("SELECT row_count FROM snapshots WHERE snapshot_id = ?", "snap_1234abcd").Scan(&rows); err != nil {
		t.Fatalf("query snapshot metadata: %v", err)
	}
	if !rows.Valid || rows.Int64 != 2 {
		t.Fatalf("row_count = %+v", rows)
	}
}

func TestMetadataStoreConcurrentWritesAreTransactional(t *testing.T) {
	store, err := OpenMetadataStore(t.TempDir() + "/backstop.db")
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	const writers = 32
	var wg sync.WaitGroup
	errs := make(chan error, writers*2)
	for i := 0; i < writers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			manifest := SnapshotManifest{
				SnapshotID:    "snap_concurrent_" + int64String(int64(i)),
				TableName:     "users",
				Writer:        sidecarWriter,
				SnapshotScope: "table",
				RowCount:      i,
				S3ManifestKey: "backstop/snapshots/users/snap_concurrent/manifest.json",
				Timestamp:     "2026-05-01T00:00:00Z",
				Status:        "valid",
			}
			if err := store.RecordSnapshot(ctx, manifest); err != nil {
				errs <- err
			}
			if err := store.RecordHealth(ctx, "sync", "healthy", map[string]any{"writer": i}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent metadata write failed: %v", err)
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM snapshots WHERE table_name = ?", "users").Scan(&count); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if count != writers {
		t.Fatalf("snapshot rows = %d, want %d", count, writers)
	}
}

func TestBypassDetectorFindsDisallowedAgentConnection(t *testing.T) {
	detector := NewBypassDetector(nil, BypassConfig{
		AgentRoles:             []string{"backstop_agent"},
		GatewayApplicationName: "backstop-gateway",
		AllowedClientAddresses: []string{"10.0.0.0/24"},
	})
	findings := detector.Evaluate([]DBActivity{
		{Role: "backstop_agent", ApplicationName: "psql", ClientAddress: "192.168.1.50"},
		{Role: "backstop_agent", ApplicationName: "backstop-gateway", ClientAddress: "192.168.1.60"},
	})
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one bypass", findings)
	}
	if findings[0].Posture != PreventionDegraded {
		t.Fatalf("posture = %s", findings[0].Posture)
	}
}

func TestSyncMetricsIncludesBypassPosture(t *testing.T) {
	metrics := NewSyncMetrics()
	metrics.IncBypass("backstop_agent", "psql")
	metrics.SetPosture(PreventionDegraded)
	text := metrics.Prometheus(time.Now())
	for _, want := range []string{
		`backstop_bypass_connections_total{role="backstop_agent",application="psql"} 1`,
		`backstop_prevention_posture{status="degraded"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics missing %q:\n%s", want, text)
		}
	}
}

func TestSyncMetricsHealthIsExplicitReadinessState(t *testing.T) {
	metrics := NewSyncMetrics()
	if got := metrics.Health().Status; got != "starting" {
		t.Fatalf("initial health = %q, want starting", got)
	}
	metrics.SetHealth("degraded", map[string]any{"missing": []string{"users"}})
	snapshot := metrics.Health()
	if snapshot.Status != "degraded" {
		t.Fatalf("health = %q, want degraded", snapshot.Status)
	}
	if !strings.Contains(string(healthJSON(snapshot)), `"status":"degraded"`) {
		t.Fatalf("health JSON missing degraded status: %s", healthJSON(snapshot))
	}
	if !strings.Contains(metrics.Prometheus(time.Now()), `backstop_sync_health{status="degraded"} 1`) {
		t.Fatalf("prometheus health gauge missing degraded state")
	}
}
