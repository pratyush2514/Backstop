package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const alertHTTPTimeout = 5 * time.Second

// AlertEngine sends drop-detected notifications to a configured webhook.
type AlertEngine struct {
	webhookURL string
	metadata   *MetadataStore
	client     *http.Client
}

// NewAlertEngine creates an AlertEngine. If webhookURL is empty, alerts are
// written to stderr via slog only.
func NewAlertEngine(webhookURL string) *AlertEngine {
	return NewAlertEngineWithMetadata(webhookURL, nil)
}

func NewAlertEngineWithMetadata(webhookURL string, metadata *MetadataStore) *AlertEngine {
	return &AlertEngine{webhookURL: webhookURL, metadata: metadata, client: http.DefaultClient}
}

// webhookPayload is the JSON body posted to the webhook endpoint.
type webhookPayload struct {
	Text                   string `json:"text"`
	Severity               string `json:"severity"`
	EventType              string `json:"event_type"`
	Table                  string `json:"table"`
	RecoveryPointAvailable bool   `json:"recovery_point_available"`
	SnapshotAgeSeconds     int64  `json:"snapshot_age_seconds,omitempty"`
	RecommendedAction      string `json:"recommended_action"`
	SnapshotID             string `json:"snapshot_id,omitempty"`
	ManifestKey            string `json:"manifest_key,omitempty"`
	Rows                   int    `json:"rows,omitempty"`
}

// SendDropAlert fires an alert for a detected DROP TABLE event.
// If no webhook URL is configured, the event is logged to stderr.
// HTTP errors are returned so the caller can decide whether to retry.
func (a *AlertEngine) SendDropAlert(ctx context.Context, table string, manifest *SnapshotManifest) error {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	recoveryAvailable := manifest != nil
	message := fmt.Sprintf("[backstop] DROP TABLE detected: %s at %s", table, timestamp)
	recommendedAction := "Restore from a validated sidecar snapshot before allowing further writes to the affected table."
	if recoveryAvailable {
		message = fmt.Sprintf("%s. Latest recovery point: %s (%d rows)", message, manifest.SnapshotID, manifest.RowCount)
	} else {
		message = fmt.Sprintf("%s. No recovery point is available yet.", message)
		recommendedAction = "Stop writes, take a database-level backup if possible, and use PITR/native restore paths."
	}

	payload := webhookPayload{
		Text:                   message,
		Severity:               "critical",
		EventType:              "table_drop_detected",
		Table:                  table,
		RecoveryPointAvailable: recoveryAvailable,
		RecommendedAction:      recommendedAction,
	}
	if manifest != nil {
		payload.SnapshotID = manifest.SnapshotID
		payload.ManifestKey = manifest.S3ManifestKey
		payload.Rows = manifest.RowCount
		if ts, err := time.Parse(time.RFC3339, manifest.Timestamp); err == nil {
			payload.SnapshotAgeSeconds = int64(time.Since(ts).Seconds())
		}
	}
	return a.sendPayload(ctx, payload)
}

func (a *AlertEngine) SendStaleSnapshotAlert(ctx context.Context, table string, age time.Duration, maxAge time.Duration, manifest *SnapshotManifest) error {
	recoveryAvailable := manifest != nil
	payload := webhookPayload{
		Text:                   fmt.Sprintf("[backstop] Snapshot for %s is stale: age=%s max=%s", table, age.Round(time.Second), maxAge.Round(time.Second)),
		Severity:               "warning",
		EventType:              "snapshot_stale",
		Table:                  table,
		RecoveryPointAvailable: recoveryAvailable,
		SnapshotAgeSeconds:     int64(age.Seconds()),
		RecommendedAction:      "Check sidecar health, S3 reachability, and database permissions; refresh the table snapshot before destructive operations.",
	}
	if manifest != nil {
		payload.SnapshotID = manifest.SnapshotID
		payload.ManifestKey = manifest.S3ManifestKey
		payload.Rows = manifest.RowCount
	}
	return a.sendPayload(ctx, payload)
}

func (a *AlertEngine) SendSnapshotFailureAlert(ctx context.Context, table string, failures int, err error) error {
	payload := webhookPayload{
		Text:                   fmt.Sprintf("[backstop] Snapshot capture failed %d consecutive times for %s: %v", failures, table, err),
		Severity:               "critical",
		EventType:              "snapshot_capture_failed",
		Table:                  table,
		RecoveryPointAvailable: false,
		RecommendedAction:      "Investigate database access, table size limits, and S3 write permissions before allowing destructive SQL.",
	}
	return a.sendPayload(ctx, payload)
}

func (a *AlertEngine) SendStorageFailureAlert(ctx context.Context, table string, err error) error {
	payload := webhookPayload{
		Text:                   fmt.Sprintf("[backstop] Snapshot storage is unreachable for %s: %v", table, err),
		Severity:               "critical",
		EventType:              "storage_unreachable",
		Table:                  table,
		RecoveryPointAvailable: false,
		RecommendedAction:      "Restore storage connectivity and verify snapshot capture before approving destructive SQL.",
	}
	return a.sendPayload(ctx, payload)
}

func (a *AlertEngine) sendPayload(ctx context.Context, payload webhookPayload) error {
	slog.Warn("backstop alert", "event_type", payload.EventType, "table", payload.Table, "severity", payload.Severity)
	status := "logged"
	defer func() {
		a.recordAlert(ctx, payload, status)
	}()
	if a.webhookURL == "" {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		status = "failed"
		return fmt.Errorf("alert: failed to marshal payload: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, alertHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, a.webhookURL, bytes.NewReader(body))
	if err != nil {
		status = "failed"
		return fmt.Errorf("alert: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := a.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		status = "failed"
		return fmt.Errorf("alert: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "failed"
		return fmt.Errorf("alert: webhook returned non-2xx status %d", resp.StatusCode)
	}
	status = "sent"
	return nil
}

func (a *AlertEngine) recordAlert(ctx context.Context, payload webhookPayload, status string) {
	if a == nil || a.metadata == nil {
		return
	}
	if err := a.metadata.RecordAlert(ctx, payload.Severity, payload.EventType, payload.Table, status, payload); err != nil {
		slog.Error("Failed to record alert metadata", "event_type", payload.EventType, "table", payload.Table, "error", err)
	}
}
