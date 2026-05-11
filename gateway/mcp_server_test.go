package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClassifySQL(t *testing.T) {
	cases := map[string]string{
		"SELECT * FROM users":                    RiskSafe,
		"SHOW search_path":                       RiskSafe,
		"EXPLAIN SELECT * FROM users":            RiskSafe,
		"BEGIN":                                  RiskSafe,
		"COMMIT":                                 RiskSafe,
		"ROLLBACK":                               RiskSafe,
		"SET search_path = public":               RiskSafe,
		"INSERT INTO users(id) VALUES (1)":       RiskHigh,
		"DELETE FROM users WHERE id=1":           RiskHigh,
		"UPDATE users SET name = 'x' WHERE id=1": RiskHigh,
		"DELETE FROM users":                      RiskCritical,
		"UPDATE users SET name = 'x'":            RiskCritical,
		"DROP TABLE users":                       RiskCritical,
		"TRUNCATE users":                         RiskCritical,
		"DROP DATABASE prod":                     RiskCritical,
		"DROP SCHEMA public":                     RiskCritical,
	}
	for query, want := range cases {
		if got := classifySQL(query); got != want {
			t.Fatalf("classifySQL(%q) = %s, want %s", query, got, want)
		}
	}
}

func TestClassifySQLUsesPostgresAST(t *testing.T) {
	cases := map[string]string{
		"SELECT 'DROP TABLE users'":                                  RiskSafe,
		"DELETE FROM users WHERE note = 'no WHERE trick'":            RiskHigh,
		"DELETE FROM users /* WHERE id = 1 */":                       RiskCritical,
		"/* DROP TABLE users */ SELECT 1":                            RiskSafe,
		"SELECT 1; DROP TABLE users":                                 RiskCritical,
		"this is not sql !@#$":                                       RiskCritical,
		"VACUUM users":                                               RiskCritical,
		"EXPLAIN ANALYZE DELETE FROM users":                          RiskCritical,
		"EXPLAIN ANALYZE DELETE FROM users WHERE id = 1":             RiskHigh,
		"EXPLAIN SELECT 'DROP TABLE users' FROM pg_catalog.pg_class": RiskSafe,
	}
	for query, want := range cases {
		if got := classifySQL(query); got != want {
			t.Fatalf("classifySQL(%q) = %s, want %s", query, got, want)
		}
	}
}

func TestAnalyzeSQLPreservesSafeStatementOperation(t *testing.T) {
	analysis := analyzeSQL("/* DROP TABLE users */ SELECT 'DROP TABLE users' AS msg")
	if analysis.RiskLevel != RiskSafe {
		t.Fatalf("risk = %s, want %s", analysis.RiskLevel, RiskSafe)
	}
	if analysis.Operation != "SELECT" {
		t.Fatalf("operation = %s, want SELECT", analysis.Operation)
	}
	if !returnsRows(analysis) {
		t.Fatal("SELECT analysis should route through QueryContext")
	}
}

func TestAnalyzeQueryToolDoesNotExecuteOrAudit(t *testing.T) {
	registry := NewAgentRegistry()
	server := NewMCPServer(
		NewApprovalEngine(time.Second),
		registry,
		"",
		nil,
		"",
		"",
		true,
		nil,
	)

	resp := server.toolAnalyzeQuery(context.Background(), 1, []byte(`{"query":"DROP DATABASE prod","agent_id":"agent-1"}`))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", resp.Result)
	}
	if result["status"] != "analyzed" {
		t.Fatalf("status = %v, want analyzed", result["status"])
	}
	if result["risk_level"] != RiskCritical {
		t.Fatalf("risk = %v, want %s", result["risk_level"], RiskCritical)
	}
	if got := len(registry.AllEntries()); got != 0 {
		t.Fatalf("analyze_query should not write audit entries; got %d", got)
	}
}

func TestExecuteQueryRejectsWhitespaceOnlySQL(t *testing.T) {
	server := NewMCPServer(NewApprovalEngine(time.Second), NewAgentRegistry(), "", nil, "", "", false, nil)
	resp := server.toolExecuteQuery(context.Background(), 1, []byte(`{"query":"   ","agent_id":"agent-1"}`))
	if resp.Error == nil {
		t.Fatalf("expected validation error, got %+v", resp.Result)
	}
	if !strings.Contains(resp.Error.Message, "query is required") {
		t.Fatalf("error = %+v", resp.Error)
	}
}

func TestImpactCriticalWriteKeepsTableRecoverable(t *testing.T) {
	analysis := applyImpactAnalysis(
		analyzeSQL("DELETE FROM users WHERE id > 0"),
		&ImpactAnalysis{
			Status:         "estimated",
			Reason:         "write impact exceeds configured thresholds",
			ImpactCritical: true,
		},
	)
	if analysis.RiskLevel != RiskImpactCritical {
		t.Fatalf("risk = %s, want %s", analysis.RiskLevel, RiskImpactCritical)
	}
	if !analysis.TableRecoverable {
		t.Fatal("impact-critical single-table write should remain table recoverable")
	}
	table, err := criticalRecoveryTableFromAnalysis(analysis)
	if err != nil {
		t.Fatalf("criticalRecoveryTableFromAnalysis returned error: %v", err)
	}
	if table != "users" {
		t.Fatalf("table = %q, want users", table)
	}
	if decision := DefaultGatewayPolicy().Decide(analysis); decision.Action != PolicyActionApprove || !decision.RequiresRecovery {
		t.Fatalf("policy decision = %+v, want approval with recovery", decision)
	}
}

func TestRestoreSnapshotReturnsSecretSafeRestorePlan(t *testing.T) {
	server := NewMCPServer(
		NewApprovalEngine(time.Second),
		NewAgentRegistry(),
		"",
		nil,
		"postgresql://postgres:password@localhost:5433/testdb",
		"s3://backstop-test@http://localhost:9000",
		true,
		nil,
	)
	args := []byte(`{"snapshot_id":"snap_1234abcd","table":"users","agent_id":"agent-1"}`)

	resp := server.toolRestoreSnapshot(context.Background(), 1, args)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", resp.Result)
	}
	command, ok := result["restore_command"].(string)
	if !ok {
		t.Fatalf("restore_command missing from result: %+v", result)
	}
	if strings.Contains(command, "postgres:password") || strings.Contains(command, "localhost:5433") {
		t.Fatalf("restore command leaked database URL: %q", command)
	}
	for _, want := range []string{
		"backstop restore",
		"--db \"$BACKSTOP_RESTORE_DB\"",
		"s3://backstop-test@http://localhost:9000",
		"snap_1234abcd",
		"users",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("restore command %q missing %q", command, want)
		}
	}
}

type fakeSnapshotVerifier struct {
	manifest SnapshotManifest
	err      error
}

func (f fakeSnapshotVerifier) VerifyLatestSidecarSnapshot(ctx context.Context, table string, snapshotID string) (SnapshotManifest, error) {
	if f.err != nil {
		return SnapshotManifest{}, f.err
	}
	manifest := f.manifest
	manifest.TableName = table
	manifest.SnapshotID = snapshotID
	return manifest, nil
}

func TestVerifyCriticalRecoveryPointRequiresLatestSidecarSnapshot(t *testing.T) {
	server := NewMCPServer(
		NewApprovalEngine(time.Second),
		NewAgentRegistry(),
		"",
		nil,
		"",
		"s3://backstop-test@http://localhost:9000",
		true,
		fakeSnapshotVerifier{
			manifest: SnapshotManifest{
				Writer:        sidecarWriter,
				Timestamp:     "2026-05-01T00:00:00Z",
				RowCount:      2,
				S3ManifestKey: "backstop/snapshots/users/snap_1234abcd/manifest.json",
				SnapshotScope: "table",
			},
		},
	)
	server.policy.MaxSnapshotAgeSeconds = 0
	server.policy.RequireSidecarHeartbeat = false

	manifest, table, err := server.verifyCriticalRecoveryPoint(
		context.Background(),
		executeQueryParams{
			Query:      "DROP TABLE users",
			AgentID:    "agent-1",
			SnapshotID: "snap_1234abcd",
		},
	)
	if err != nil {
		t.Fatalf("verifyCriticalRecoveryPoint returned error: %v", err)
	}
	if table != "users" {
		t.Fatalf("table = %q, want users", table)
	}
	if manifest.SnapshotID != "snap_1234abcd" {
		t.Fatalf("snapshot_id = %q, want snap_1234abcd", manifest.SnapshotID)
	}
}

func TestVerifyCriticalRecoveryPointBlocksUnrecoverableDropDatabase(t *testing.T) {
	server := NewMCPServer(
		NewApprovalEngine(time.Second),
		NewAgentRegistry(),
		"",
		nil,
		"",
		"s3://backstop-test@http://localhost:9000",
		true,
		fakeSnapshotVerifier{},
	)
	server.policy.MaxSnapshotAgeSeconds = 0
	server.policy.RequireSidecarHeartbeat = false

	_, _, err := server.verifyCriticalRecoveryPoint(
		context.Background(),
		executeQueryParams{
			Query:      "DROP DATABASE prod",
			AgentID:    "agent-1",
			SnapshotID: "snap_1234abcd",
		},
	)
	if err == nil {
		t.Fatal("expected unrecoverable DROP DATABASE to be blocked")
	}
}

func TestToolsListDoesNotRequireDBURLArgument(t *testing.T) {
	tools := builtinTools()
	var execute toolDef
	for _, tool := range tools {
		if tool.Name == "execute_query" {
			execute = tool
			break
		}
	}
	raw, err := json.Marshal(execute.InputSchema["required"])
	if err != nil {
		t.Fatalf("marshal required schema: %v", err)
	}
	if strings.Contains(string(raw), "db_url") {
		t.Fatalf("execute_query should not require db_url when gateway can be configured with --db: %s", raw)
	}
}
