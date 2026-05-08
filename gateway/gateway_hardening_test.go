package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAgentRegistryPersistsAuditLog(t *testing.T) {
	path := t.TempDir() + "/audit.jsonl"

	registry := NewAgentRegistryWithAuditLog(path)
	registry.Record("agent-1", "DROP TABLE users", RiskCritical, true)

	reloaded := NewAgentRegistryWithAuditLog(path)
	entries := reloaded.GetHistory("agent-1")
	if len(entries) != 1 {
		t.Fatalf("loaded entries = %d, want 1", len(entries))
	}
	if entries[0].Query != "DROP TABLE users" || entries[0].RiskLevel != RiskCritical || !entries[0].Approved {
		t.Fatalf("unexpected loaded entry: %+v", entries[0])
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat audit log: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("audit log should not be empty")
	}
}

func TestRequireAuth(t *testing.T) {
	protected := requireAuth("secret", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	for _, tc := range []struct {
		name       string
		headerName string
		headerVal  string
		wantStatus int
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized},
		{name: "bad bearer", headerName: "Authorization", headerVal: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "good bearer", headerName: "Authorization", headerVal: "Bearer secret", wantStatus: http.StatusNoContent},
		{name: "good backstop token", headerName: "X-Backstop-Token", headerVal: "secret", wantStatus: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/audit", nil)
			if tc.headerName != "" {
				req.Header.Set(tc.headerName, tc.headerVal)
			}
			rr := httptest.NewRecorder()
			protected(rr, req)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rr.Code, tc.wantStatus)
			}
		})
	}
}

func TestRequireAuthDisabled(t *testing.T) {
	handler := requireAuth("", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	rr := httptest.NewRecorder()
	handler(rr, httptest.NewRequest(http.MethodGet, "/audit", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

func TestValidateAuthConfigRequiresTokenByDefault(t *testing.T) {
	if err := validateAuthConfig(true, false, "", ""); err == nil {
		t.Fatal("expected missing auth token to fail startup validation")
	}
	if err := validateAuthConfig(true, false, "", "tokens.json"); err != nil {
		t.Fatalf("token file should satisfy auth config: %v", err)
	}
	if err := validateAuthConfig(true, true, "", ""); err != nil {
		t.Fatalf("explicit insecure mode should allow missing token: %v", err)
	}
}

func TestScopedTokenFileEnforcesScopes(t *testing.T) {
	path := t.TempDir() + "/tokens.json"
	if err := os.WriteFile(path, []byte(`{"tokens":[{"name":"agent","token":"agent-token","scopes":["query:execute"]},{"name":"operator","token":"operator-token","scopes":["approval:read"]}]}`), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	auth, err := NewTokenAuthenticator("", path, false)
	if err != nil {
		t.Fatalf("load auth: %v", err)
	}
	handler := requireScopedAuth(auth, nil, nil, "approval:read", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	for _, tc := range []struct {
		name       string
		token      string
		wantStatus int
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized},
		{name: "wrong scope", token: "agent-token", wantStatus: http.StatusForbidden},
		{name: "right scope", token: "operator-token", wantStatus: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/pending", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rr := httptest.NewRecorder()
			handler(rr, req)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rr.Code, tc.wantStatus)
			}
		})
	}
}

func TestApprovalMetadataStoresBindingFields(t *testing.T) {
	store, err := OpenMetadataStore(t.TempDir() + "/backstop.db")
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	defer store.Close()
	registry := NewAgentRegistryWithStores("", store)
	details := ApprovalDetails{
		ID:          "appr_1",
		Query:       "DROP TABLE users",
		QuerySHA256: querySHA256("DROP TABLE users"),
		AgentID:     "agent-1",
		RiskLevel:   RiskCritical,
		Operation:   "DROP TABLE",
		Table:       "users",
		Environment: "prod",
		ClusterID:   "cluster-a",
		SnapshotID:  "snap_1",
	}
	registry.RecordApprovalRequested(context.Background(), details)
	registry.RecordApprovalResolved(context.Background(), details.ID, "approved", "operator")

	rows, err := store.QueryRows(context.Background(), "approvals", map[string]string{"approval_id": details.ID})
	if err != nil {
		t.Fatalf("query approvals: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	for key, want := range map[string]any{
		"query_sha256": details.QuerySHA256,
		"environment":  details.Environment,
		"cluster_id":   details.ClusterID,
		"snapshot_id":  details.SnapshotID,
		"resolved_by":  "operator",
	} {
		if row[key] != want {
			t.Fatalf("%s = %v, want %v; row=%+v", key, row[key], want, row)
		}
	}
}

func TestEmergencyPauseBlocksWritesButAllowsSafeByDefault(t *testing.T) {
	server := NewMCPServer(NewApprovalEngine(time.Second), NewAgentRegistry(), "", nil, "", "", false, nil)
	pause, err := NewPauseController("", true, false)
	if err != nil {
		t.Fatalf("pause controller: %v", err)
	}
	server.pause = pause

	blocked := server.toolExecuteQuery(context.Background(), 1, []byte(`{"query":"DELETE FROM users WHERE id=1","agent_id":"agent-1"}`))
	result := blocked.Result.(map[string]any)
	if result["status"] != "blocked" {
		t.Fatalf("write status = %v, want blocked", result["status"])
	}

	safe := server.toolExecuteQuery(context.Background(), 2, []byte(`{"query":"SELECT 1","agent_id":"agent-1"}`))
	if safe.Error == nil {
		result := safe.Result.(map[string]any)
		if result["status"] == "blocked" {
			t.Fatalf("SAFE query should not be pause-blocked by default: %+v", result)
		}
	}
}

func TestPauseHandlersRequireAdminScope(t *testing.T) {
	auth, err := NewTokenAuthenticator("", "", false)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	auth.tokens = []tokenEntry{{Name: "agent", Token: "agent-token", Scopes: []string{"query:execute"}}, {Name: "admin", Token: "admin-token", Scopes: []string{"admin:*"}}}
	pause, _ := NewPauseController("", false, false)
	handler := requireScopedAuth(auth, nil, nil, "admin:*", makePauseHandler(pause, nil, "local", "local", true, NewGatewayMetrics()))

	req := httptest.NewRequest(http.MethodPost, "/admin/pause", bytes.NewReader([]byte(`{"reason":"test"}`)))
	req.Header.Set("Authorization", "Bearer agent-token")
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("agent status = %d, want forbidden", rr.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/pause", bytes.NewReader([]byte(`{"reason":"test"}`)))
	req.Header.Set("Authorization", "Bearer admin-token")
	rr = httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "emergency_paused" {
		t.Fatalf("status = %v", body["status"])
	}
}

func TestDefaultPolicyRequiresApprovalAndBlocksUnrecoverable(t *testing.T) {
	policy := DefaultGatewayPolicy()
	high := policy.Decide(analyzeSQL("INSERT INTO users(id) VALUES (1)"))
	if high.Action != PolicyActionApprove || !high.RequiresApproval {
		t.Fatalf("HIGH policy = %+v, want approval", high)
	}
	dropDB := policy.Decide(analyzeSQL("DROP DATABASE prod"))
	if dropDB.Action != PolicyActionBlock {
		t.Fatalf("DROP DATABASE policy = %+v, want block", dropDB)
	}
}

func TestMetadataStorePersistsAuditAcrossRestart(t *testing.T) {
	path := t.TempDir() + "/backstop.db"
	store, err := OpenMetadataStore(path)
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	registry := NewAgentRegistryWithStores("", store)
	registry.Record("agent-1", "DROP TABLE users", RiskCritical, false)
	if err := store.Close(); err != nil {
		t.Fatalf("close metadata: %v", err)
	}

	reopened, err := OpenMetadataStore(path)
	if err != nil {
		t.Fatalf("reopen metadata: %v", err)
	}
	defer reopened.Close()
	rows, err := reopened.QueryAudit(context.Background(), "agent-1", RiskCritical)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if len(rows) != 1 || rows[0]["query"] != "DROP TABLE users" {
		t.Fatalf("unexpected metadata rows: %+v", rows)
	}
}

func TestGatewayMetricsPrometheus(t *testing.T) {
	metrics := NewGatewayMetrics()
	metrics.IncQuery(RiskSafe, "executed")
	metrics.IncApproval("approved")
	metrics.IncBlock("DROP DATABASE")
	text := metrics.Prometheus()
	for _, want := range []string{
		`backstop_gateway_queries_total{risk="SAFE",status="executed"} 1`,
		`backstop_gateway_approvals_total{decision="approved"} 1`,
		`backstop_gateway_blocks_total{reason="DROP DATABASE"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("metrics missing %q:\n%s", want, text)
		}
	}
}

func TestAuditScrubsSecrets(t *testing.T) {
	registry := NewAgentRegistry()
	registry.Record("agent-1", "ALTER USER app PASSWORD 'secret'; SELECT 'postgres://u:p@host/db'", RiskHigh, true)
	entry := registry.AllEntries()[0]
	if strings.Contains(entry.Query, "secret") || strings.Contains(entry.Query, ":p@") {
		t.Fatalf("query was not scrubbed: %s", entry.Query)
	}
}

func TestProtectedColumnPromotesImpactCritical(t *testing.T) {
	server := NewMCPServer(NewApprovalEngine(0), NewAgentRegistry(), "", nil, "", "", false, nil)
	server.policy.ProtectedColumns = map[string][]string{"users": []string{"email"}}

	analysis := analyzeSQL("UPDATE users SET email = NULL WHERE id = 1")
	impact := server.analyzeImpact(context.Background(), executeQueryParams{Query: "UPDATE users SET email = NULL WHERE id = 1"}, analysis)
	if impact == nil || !impact.ImpactCritical {
		t.Fatalf("impact = %+v, want critical", impact)
	}
	if len(impact.ProtectedColumns) != 1 || impact.ProtectedColumns[0] != "email" {
		t.Fatalf("protected columns = %+v", impact.ProtectedColumns)
	}
}

func TestAgentQuarantinePersistsInMetadata(t *testing.T) {
	store, err := OpenMetadataStore(t.TempDir() + "/backstop.db")
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	defer store.Close()
	server := NewMCPServer(NewApprovalEngine(0), NewAgentRegistryWithStores("", store), "", nil, "", "", false, nil)
	server.policy.MaxBlockedAttemptsPerWindow = 2
	server.policy.DangerousRetryWindowSeconds = 60
	server.policy.QuarantineDurationSeconds = 60

	analysis := analyzeSQL("DROP TABLE users")
	server.recordRiskAttempt(context.Background(), "agent-1", analysis, "blocked")
	if server.agentQuarantined(context.Background(), "agent-1", analysis) {
		t.Fatal("agent should not be quarantined after first attempt")
	}
	server.recordRiskAttempt(context.Background(), "agent-1", analysis, "blocked")
	if !server.agentQuarantined(context.Background(), "agent-1", analysis) {
		t.Fatal("agent should be quarantined after repeated risky attempts")
	}
}

func TestRecoveryReadinessBlocksStaleSnapshot(t *testing.T) {
	server := NewMCPServer(NewApprovalEngine(0), NewAgentRegistry(), "", nil, "", "", false, nil)
	server.policy.RequireSidecarHeartbeat = false
	server.policy.MaxSnapshotAgeSeconds = 1
	err := server.checkRecoveryReadiness(context.Background(), SnapshotManifest{
		SnapshotID: "snap_old",
		Timestamp:  time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	})
	if err == nil {
		t.Fatal("expected stale snapshot to fail readiness")
	}
}

