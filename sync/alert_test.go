package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendDropAlertIncludesRecoveryPoint(t *testing.T) {
	var payload webhookPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	alerter := NewAlertEngine(server.URL)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	alerter := NewAlertEngine(server.URL)
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

	store.RecordSnapshot(context.Background(), SnapshotManifest{
		SnapshotID:    "snap_1234abcd",
		TableName:     "users",
		Writer:        sidecarWriter,
		SnapshotScope: "table",
		RowCount:      2,
		S3ManifestKey: "backstop/snapshots/users/snap_1234abcd/manifest.json",
		Timestamp:     "2026-05-01T00:00:00Z",
	})

	var rows sql.NullInt64
	if err := store.db.QueryRow("SELECT row_count FROM snapshots WHERE snapshot_id = ?", "snap_1234abcd").Scan(&rows); err != nil {
		t.Fatalf("query snapshot metadata: %v", err)
	}
	if !rows.Valid || rows.Int64 != 2 {
		t.Fatalf("row_count = %+v", rows)
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

