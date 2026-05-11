package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type GatewayAlertPayload struct {
	Severity          string `json:"severity"`
	EventType         string `json:"event_type"`
	Environment       string `json:"environment"`
	ClusterID         string `json:"cluster_id"`
	AgentID           string `json:"agent_id,omitempty"`
	RiskLevel         string `json:"risk_level,omitempty"`
	Operation         string `json:"operation,omitempty"`
	Schema            string `json:"schema,omitempty"`
	Table             string `json:"table,omitempty"`
	QuerySHA256       string `json:"query_sha256,omitempty"`
	ApprovalID        string `json:"approval_id,omitempty"`
	SnapshotID        string `json:"snapshot_id,omitempty"`
	RecommendedAction string `json:"recommended_action"`
	Timestamp         string `json:"timestamp"`
	Message           string `json:"message,omitempty"`
}

type GatewayAlertSink struct {
	webhook    string
	metadata   *MetadataStore
	metrics    *GatewayMetrics
	httpClient *http.Client
}

func NewGatewayAlertSink(webhook string, metadata *MetadataStore, metrics *GatewayMetrics) *GatewayAlertSink {
	return &GatewayAlertSink{
		webhook:  strings.TrimSpace(webhook),
		metadata: metadata,
		metrics:  metrics,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (s *GatewayAlertSink) Emit(ctx context.Context, payload GatewayAlertPayload) {
	if s == nil {
		return
	}
	if payload.Timestamp == "" {
		payload.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	status := "recorded"
	if s.webhook != "" {
		status = "sent"
		raw, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhook, bytes.NewReader(raw))
		if err != nil {
			status = "failed"
		} else {
			req.Header.Set("Content-Type", "application/json")
			resp, err := s.httpClient.Do(req)
			if err != nil {
				status = "failed"
			} else {
				_ = resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					status = fmt.Sprintf("failed_http_%d", resp.StatusCode)
				}
			}
		}
	}
	if s.metrics != nil {
		s.metrics.IncAlert(payload.EventType, status)
	}
	if s.metadata != nil {
		if err := s.metadata.RecordAlert(ctx, payload.Severity, payload.EventType, payload.Table, status, payload); err != nil {
			if s.metrics != nil {
				s.metrics.IncAlert(payload.EventType, "metadata_failed")
			}
		}
	}
}
