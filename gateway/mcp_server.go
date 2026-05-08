package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---- JSON-RPC wire types ------------------------------------------------

// MCPRequest is the top-level JSON-RPC 2.0 request.
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// MCPResponse is the top-level JSON-RPC 2.0 response.
type MCPResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// RPCError follows the JSON-RPC 2.0 error object shape.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

// ---- Tool param structs -------------------------------------------------

type executeQueryParams struct {
	Query      string `json:"query"`
	AgentID    string `json:"agent_id"`
	DBURL      string `json:"db_url"`
	SnapshotID string `json:"snapshot_id"`
}

type analyzeQueryParams struct {
	Query   string `json:"query"`
	AgentID string `json:"agent_id"`
	DBURL   string `json:"db_url"`
}

type restoreSnapshotParams struct {
	SnapshotID string `json:"snapshot_id"`
	Table      string `json:"table"`
	AgentID    string `json:"agent_id"`
}

// ---- Risk levels --------------------------------------------------------

const (
	RiskSafe           = "SAFE"
	RiskLow            = "LOW"
	RiskHigh           = "HIGH"
	RiskImpactCritical = "IMPACT_CRITICAL"
	RiskCritical       = "CRITICAL"
)

// ---- MCP tool definitions (returned by list_tools) ----------------------

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func builtinTools() []toolDef {
	return []toolDef{
		{
			Name:        "execute_query",
			Description: "Execute SQL through backstop. SAFE reads execute immediately, HIGH and CRITICAL queries may require approval, CRITICAL table operations require recovery readiness, and DROP DATABASE/DROP SCHEMA are blocked.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":    map[string]any{"type": "string", "description": "SQL query to execute"},
					"agent_id": map[string]any{"type": "string", "description": "Stable identifier for the calling agent. If using the backstop MCP wrapper, this is normally supplied by BACKSTOP_AGENT_ID."},
					"db_url":   map[string]any{"type": "string", "description": "Optional PostgreSQL connection string when gateway was not started with --db"},
					"snapshot_id": map[string]any{
						"type":        "string",
						"description": "Latest sidecar snapshot ID required before approved CRITICAL execution when --snapshot-before-critical is enabled",
					},
				},
				"required": []string{"query", "agent_id"},
			},
		},
		{
			Name:        "analyze_query",
			Description: "Analyze SQL through backstop policy without executing it. Use before UPDATE, DELETE, DROP, ALTER, TRUNCATE, migrations, or any agent-generated SQL that might modify data.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":    map[string]any{"type": "string", "description": "SQL query to analyze without executing"},
					"agent_id": map[string]any{"type": "string", "description": "Optional stable identifier for the calling agent"},
					"db_url":   map[string]any{"type": "string", "description": "Optional PostgreSQL connection string when impact analysis needs a DB and gateway was not started with --db"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "restore_snapshot",
			Description: "Restore a previously captured backstop snapshot to the database.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"snapshot_id": map[string]any{"type": "string", "description": "Snapshot ID (snap_xxxx)"},
					"table":       map[string]any{"type": "string", "description": "Original table name"},
					"agent_id":    map[string]any{"type": "string", "description": "Identifier for the calling agent"},
				},
				"required": []string{"snapshot_id", "table", "agent_id"},
			},
		},
		{
			Name:        "list_tools",
			Description: "List all available MCP tools exposed by this gateway.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

// ---- MCPServer ----------------------------------------------------------

// MCPServer handles MCP JSON-RPC requests.
type MCPServer struct {
	approval               *ApprovalEngine
	registry               *AgentRegistry
	webhook                string // optional alert webhook URL
	db                     *sql.DB
	defaultDBURL           string
	storage                string
	snapshotBeforeCritical bool
	snapshotVerifier       SnapshotVerifier
	policy                 GatewayPolicy
	metrics                *GatewayMetrics
	environment            string
	clusterID              string
	pause                  *PauseController
	alerts                 *GatewayAlertSink
}

// NewMCPServer constructs an MCPServer wired to the given engines.
func NewMCPServer(
	approval *ApprovalEngine,
	registry *AgentRegistry,
	webhook string,
	db *sql.DB,
	defaultDBURL string,
	storage string,
	snapshotBeforeCritical bool,
	snapshotVerifier SnapshotVerifier,
) *MCPServer {
	return &MCPServer{
		approval:               approval,
		registry:               registry,
		webhook:                webhook,
		db:                     db,
		defaultDBURL:           defaultDBURL,
		storage:                storage,
		snapshotBeforeCritical: snapshotBeforeCritical,
		snapshotVerifier:       snapshotVerifier,
		policy:                 DefaultGatewayPolicy(),
		metrics:                NewGatewayMetrics(),
		environment:            "local",
		clusterID:              "local",
	}
}

// Handle processes a raw JSON-RPC request body and returns the JSON-RPC response.
func (s *MCPServer) Handle(ctx context.Context, body []byte) MCPResponse {
	var req MCPRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return errorResponse(nil, rpcParseError, "parse error: "+err.Error())
	}

	if req.Method == "" {
		return errorResponse(req.ID, rpcInvalidRequest, "missing method")
	}

	switch req.Method {
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		return errorResponse(req.ID, rpcMethodNotFound, fmt.Sprintf("unknown method: %s", req.Method))
	}
}

// handleListTools returns the MCP tool manifest.
func (s *MCPServer) handleListTools(req MCPRequest) MCPResponse {
	return okResponse(req.ID, map[string]any{"tools": builtinTools()})
}

// handleToolsCall dispatches to the named tool.
func (s *MCPServer) handleToolsCall(ctx context.Context, req MCPRequest) MCPResponse {
	// Params shape: {"name": "...", "arguments": {...}}
	var wrapper struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &wrapper); err != nil {
		return errorResponse(req.ID, rpcInvalidParams, "invalid params: "+err.Error())
	}

	switch wrapper.Name {
	case "execute_query":
		if !s.contextHasScope(ctx, "query:execute") {
			return errorResponse(req.ID, rpcInvalidRequest, "forbidden: query:execute scope is required")
		}
		return s.toolExecuteQuery(ctx, req.ID, wrapper.Arguments)
	case "analyze_query":
		if !s.contextHasScope(ctx, "query:analyze") {
			return errorResponse(req.ID, rpcInvalidRequest, "forbidden: query:analyze scope is required")
		}
		return s.toolAnalyzeQuery(ctx, req.ID, wrapper.Arguments)
	case "restore_snapshot":
		if !s.contextHasScope(ctx, "restore:prepare") {
			return errorResponse(req.ID, rpcInvalidRequest, "forbidden: restore:prepare scope is required")
		}
		return s.toolRestoreSnapshot(ctx, req.ID, wrapper.Arguments)
	case "list_tools":
		return s.handleListTools(req)
	default:
		return errorResponse(req.ID, rpcMethodNotFound, fmt.Sprintf("unknown tool: %s", wrapper.Name))
	}
}

func (s *MCPServer) contextHasScope(ctx context.Context, scope string) bool {
	principal, ok := principalFromContext(ctx)
	if !ok {
		return true
	}
	return hasScope(principal, scope)
}

// toolAnalyzeQuery implements an analyze-only MCP tool. It intentionally does
// not record approvals, execute SQL, or mutate audit history.
func (s *MCPServer) toolAnalyzeQuery(ctx context.Context, id any, rawArgs json.RawMessage) MCPResponse {
	var args analyzeQueryParams
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(id, rpcInvalidParams, "invalid arguments: "+err.Error())
	}
	if strings.TrimSpace(args.Query) == "" {
		return errorResponse(id, rpcInvalidParams, "query is required")
	}

	execArgs := executeQueryParams{
		Query:   args.Query,
		AgentID: args.AgentID,
		DBURL:   args.DBURL,
	}
	analysis := analyzeSQL(args.Query)
	impact := s.analyzeImpact(ctx, execArgs, analysis)
	analysis = applyImpactAnalysis(analysis, impact)
	decision := s.policy.Decide(analysis)
	return okResponse(id, map[string]any{
		"status":          "analyzed",
		"risk_level":      analysis.RiskLevel,
		"safety_metadata": s.safetyMetadata(analysis, decision),
		"message":         decision.Reason,
	})
}

// toolExecuteQuery implements the execute_query MCP tool.
func (s *MCPServer) toolExecuteQuery(ctx context.Context, id any, rawArgs json.RawMessage) MCPResponse {
	var args executeQueryParams
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(id, rpcInvalidParams, "invalid arguments: "+err.Error())
	}
	if args.Query == "" {
		return errorResponse(id, rpcInvalidParams, "query is required")
	}
	if args.AgentID == "" {
		return errorResponse(id, rpcInvalidParams, "agent_id is required")
	}

	analysis := analyzeSQL(args.Query)
	impact := s.analyzeImpact(ctx, args, analysis)
	analysis = applyImpactAnalysis(analysis, impact)
	risk := analysis.RiskLevel
	decision := s.policy.Decide(analysis)
	querySHA := querySHA256(args.Query)

	if s.pause != nil && s.pause.BlocksRisk(risk) {
		s.registry.RecordDetailed(args.AgentID, args.Query, risk, false, querySHA, s.environment, s.clusterID)
		s.metrics.IncQuery(risk, "blocked")
		s.metrics.IncBlock("paused")
		s.emitAlert(ctx, "critical", "execution_blocked_paused", args.AgentID, analysis, querySHA, "", args.SnapshotID, "Gateway is paused. Resume only after incident review.", "Query execution blocked by emergency pause.")
		return okResponse(id, map[string]any{
			"status":          "blocked",
			"risk_level":      risk,
			"safety_metadata": s.safetyMetadata(analysis, PolicyDecision{Action: PolicyActionBlock, Reason: "gateway is paused", RecoveryPossible: decision.RecoveryPossible}),
			"message":         "Gateway emergency pause is active; query not executed.",
		})
	}

	if s.agentQuarantined(ctx, args.AgentID, analysis) {
		s.registry.RecordDetailed(args.AgentID, args.Query, risk, false, querySHA, s.environment, s.clusterID)
		s.metrics.IncQuery(risk, "blocked")
		s.metrics.IncBlock("agent_quarantined")
		s.emitAlert(ctx, "critical", "agent_quarantined", args.AgentID, analysis, querySHA, "", args.SnapshotID, "Inspect agent prompt/tool loop and keep writes blocked for this agent.", "Agent is quarantined after repeated risky attempts.")
		return okResponse(id, map[string]any{
			"status":          "blocked",
			"risk_level":      risk,
			"safety_metadata": s.safetyMetadata(analysis, PolicyDecision{Action: PolicyActionBlock, Reason: "agent is quarantined", RecoveryPossible: decision.RecoveryPossible}),
			"message":         "Agent is temporarily quarantined after repeated risky attempts.",
		})
	}

	if decision.Action == PolicyActionBlock {
		s.registry.RecordDetailed(args.AgentID, args.Query, risk, false, querySHA, s.environment, s.clusterID)
		s.recordRiskAttempt(ctx, args.AgentID, analysis, "blocked")
		s.metrics.IncQuery(risk, "blocked")
		s.metrics.IncBlock(blockReasonLabel(analysis, decision))
		if analysis.Operation == "DROP DATABASE" || analysis.Operation == "DROP SCHEMA" {
			s.emitAlert(ctx, "critical", "blocked_"+strings.ToLower(strings.ReplaceAll(analysis.Operation, " ", "_")), args.AgentID, analysis, querySHA, "", args.SnapshotID, "Use native backup/PITR planning. Table snapshots cannot recover this operation.", decision.Reason)
		}
		return okResponse(id, map[string]any{
			"status":          "blocked",
			"risk_level":      risk,
			"safety_metadata": s.safetyMetadata(analysis, decision),
			"message":         decision.Reason,
		})
	}

	if decision.Action == PolicyActionExecute {
		s.registry.RecordDetailed(args.AgentID, args.Query, risk, true, querySHA, s.environment, s.clusterID)
		result, execErr := s.executeSQL(ctx, args, analysis)
		if execErr != nil {
			s.metrics.IncQuery(risk, "failed")
			return okResponse(id, map[string]any{
				"status":          "failed",
				"risk_level":      risk,
				"snapshot_id":     nil,
				"safety_metadata": s.safetyMetadata(analysis, decision),
				"error":           execErr.Error(),
			})
		}
		s.metrics.IncQuery(risk, "executed")
		result["status"] = "executed"
		result["risk_level"] = risk
		result["snapshot_id"] = nil
		result["safety_metadata"] = s.safetyMetadata(analysis, decision)
		return okResponse(id, result)
	}

	if decision.Action == PolicyActionApprove {
		approvalID := generateID("appr")
		var recovery SnapshotManifest
		if decision.RequiresRecovery && s.snapshotBeforeCritical {
			verified, table, verifyErr := s.verifyCriticalRecoveryPointWithAnalysis(ctx, args, analysis)
			if verifyErr != nil {
				s.registry.RecordDetailed(args.AgentID, args.Query, risk, false, querySHA, s.environment, s.clusterID)
				s.metrics.IncRecovery("failed")
				s.metrics.IncQuery(risk, "blocked")
				s.emitAlert(ctx, "critical", "recovery_readiness_failed", args.AgentID, analysis, querySHA, approvalID, args.SnapshotID, "Capture or repair a latest sidecar snapshot before approving this operation.", verifyErr.Error())
				return okResponse(id, map[string]any{
					"status":          "blocked",
					"approval_id":     approvalID,
					"risk_level":      risk,
					"snapshot_id":     nullableString(args.SnapshotID),
					"table":           table,
					"safety_metadata": s.safetyMetadata(analysis, decision),
					"message":         verifyErr.Error(),
				})
			}
			s.metrics.IncRecovery("verified")
			recovery = verified
		}
		details := ApprovalDetails{
			ID:          approvalID,
			Query:       args.Query,
			QuerySHA256: querySHA,
			AgentID:     args.AgentID,
			RiskLevel:   risk,
			Operation:   analysis.Operation,
			Schema:      analysis.Schema,
			Table:       analysis.Table,
			Environment: s.environment,
			ClusterID:   s.clusterID,
			SnapshotID:  args.SnapshotID,
		}
		s.registry.RecordApprovalRequested(ctx, details)
		s.emitAlert(ctx, "warning", "approval_requested", args.AgentID, analysis, querySHA, approvalID, args.SnapshotID, "Review exact query hash, environment, and recovery point before approving.", decision.Reason)

		// RequestApproval blocks until approved, denied, or timed out.
		resolution, err := s.approval.RequestApproval(ctx, details)

		if errors.Is(err, ErrApprovalTimeout) {
			s.registry.RecordDetailed(args.AgentID, args.Query, risk, false, querySHA, s.environment, s.clusterID)
			s.registry.RecordApprovalResolved(ctx, approvalID, "timeout", "")
			s.recordRiskAttempt(ctx, args.AgentID, analysis, "timeout")
			s.metrics.IncApproval("timeout")
			s.metrics.IncQuery(risk, "denied")
			s.emitAlert(ctx, "warning", "approval_timeout", args.AgentID, analysis, querySHA, approvalID, args.SnapshotID, "Re-submit only after confirming current query hash and recovery readiness.", "Approval request timed out; query not executed.")
			return okResponse(id, map[string]any{
				"status":          "denied",
				"approval_id":     approvalID,
				"risk_level":      risk,
				"safety_metadata": s.safetyMetadata(analysis, decision),
				"message":         "Approval request timed out; query not executed.",
			})
		}
		if err != nil {
			s.registry.RecordDetailed(args.AgentID, args.Query, risk, false, querySHA, s.environment, s.clusterID)
			s.registry.RecordApprovalResolved(ctx, approvalID, "denied", resolution.Actor)
			s.recordRiskAttempt(ctx, args.AgentID, analysis, "denied")
			s.metrics.IncApproval("denied")
			s.metrics.IncQuery(risk, "denied")
			s.emitAlert(ctx, "warning", "approval_denied", args.AgentID, analysis, querySHA, approvalID, args.SnapshotID, "Inspect repeated attempts from this agent before retrying.", err.Error())
			return okResponse(id, map[string]any{
				"status":          "denied",
				"approval_id":     approvalID,
				"risk_level":      risk,
				"safety_metadata": s.safetyMetadata(analysis, decision),
				"message":         err.Error(),
			})
		}

		if querySHA256(args.Query) != details.QuerySHA256 || s.environment != details.Environment || s.clusterID != details.ClusterID || analysis.Table != details.Table || risk != details.RiskLevel {
			s.registry.RecordDetailed(args.AgentID, args.Query, risk, false, querySHA, s.environment, s.clusterID)
			s.registry.RecordApprovalResolved(ctx, approvalID, "binding_mismatch", resolution.Actor)
			s.metrics.IncApproval("binding_mismatch")
			s.metrics.IncQuery(risk, "blocked")
			s.metrics.IncBlock("approval_binding_mismatch")
			return okResponse(id, map[string]any{
				"status":          "blocked",
				"approval_id":     approvalID,
				"risk_level":      risk,
				"safety_metadata": s.safetyMetadata(analysis, decision),
				"message":         "Approval binding mismatch; query not executed.",
			})
		}
		if s.pause != nil && s.pause.BlocksRisk(risk) {
			s.registry.RecordDetailed(args.AgentID, args.Query, risk, false, querySHA, s.environment, s.clusterID)
			s.registry.RecordApprovalResolved(ctx, approvalID, "paused", resolution.Actor)
			s.metrics.IncQuery(risk, "blocked")
			s.metrics.IncBlock("paused")
			s.emitAlert(ctx, "critical", "approved_execution_blocked_paused", args.AgentID, analysis, querySHA, approvalID, args.SnapshotID, "Gateway was paused after approval. Re-review before resuming.", "Approved query blocked by emergency pause.")
			return okResponse(id, map[string]any{
				"status":          "blocked",
				"approval_id":     approvalID,
				"risk_level":      risk,
				"safety_metadata": s.safetyMetadata(analysis, decision),
				"message":         "Gateway emergency pause became active after approval; query not executed.",
			})
		}
		s.registry.RecordDetailed(args.AgentID, args.Query, risk, resolution.Approved, querySHA, s.environment, s.clusterID)
		if !resolution.Approved {
			s.registry.RecordApprovalResolved(ctx, approvalID, "denied", resolution.Actor)
			s.recordRiskAttempt(ctx, args.AgentID, analysis, "denied")
			s.metrics.IncApproval("denied")
			s.metrics.IncQuery(risk, "denied")
			return okResponse(id, map[string]any{
				"status":          "denied",
				"approval_id":     approvalID,
				"risk_level":      risk,
				"safety_metadata": s.safetyMetadata(analysis, decision),
				"message":         "Query denied by operator.",
			})
		}
		s.registry.RecordApprovalResolved(ctx, approvalID, "approved", resolution.Actor)
		s.metrics.IncApproval("approved")

		result, execErr := s.executeSQL(ctx, args, analysis)
		if execErr != nil {
			s.metrics.IncQuery(risk, "failed")
			return okResponse(id, map[string]any{
				"status":          "failed",
				"approval_id":     approvalID,
				"risk_level":      risk,
				"snapshot_id":     nullableString(args.SnapshotID),
				"safety_metadata": s.safetyMetadata(analysis, decision),
				"error":           execErr.Error(),
			})
		}
		s.metrics.IncQuery(risk, "executed")
		result["status"] = "executed"
		result["approval_id"] = approvalID
		result["risk_level"] = risk
		result["snapshot_id"] = nullableString(args.SnapshotID)
		result["safety_metadata"] = s.safetyMetadata(analysis, decision)
		if recovery.SnapshotID != "" {
			result["recovery_point"] = map[string]any{
				"snapshot_id":  recovery.SnapshotID,
				"table":        recovery.TableName,
				"timestamp":    recovery.Timestamp,
				"row_count":    recovery.RowCount,
				"manifest_key": recovery.S3ManifestKey,
			}
		}
		return okResponse(id, result)
	}

	s.registry.RecordDetailed(args.AgentID, args.Query, risk, false, querySHA, s.environment, s.clusterID)
	s.metrics.IncQuery(risk, "blocked")
	s.metrics.IncBlock("unknown_policy_action")
	return okResponse(id, map[string]any{
		"status":          "blocked",
		"risk_level":      risk,
		"safety_metadata": s.safetyMetadata(analysis, decision),
		"message":         "policy returned an unknown action; gateway failed closed",
	})
}

func (s *MCPServer) verifyCriticalRecoveryPoint(ctx context.Context, args executeQueryParams) (SnapshotManifest, string, error) {
	return s.verifyCriticalRecoveryPointWithAnalysis(ctx, args, analyzeSQL(args.Query))
}

func applyImpactAnalysis(analysis sqlAnalysis, impact *ImpactAnalysis) sqlAnalysis {
	analysis.Impact = impact
	if impact != nil && impact.ImpactCritical {
		analysis.RiskLevel = RiskImpactCritical
		analysis.Reason = impact.Reason
		if strings.TrimSpace(analysis.Table) != "" {
			analysis.TableRecoverable = true
		}
	}
	return analysis
}

func (s *MCPServer) verifyCriticalRecoveryPointWithAnalysis(ctx context.Context, args executeQueryParams, analysis sqlAnalysis) (SnapshotManifest, string, error) {
	table, err := criticalRecoveryTableFromAnalysis(analysis)
	if err != nil {
		return SnapshotManifest{}, "", err
	}
	if strings.TrimSpace(args.SnapshotID) == "" {
		return SnapshotManifest{}, table, fmt.Errorf("approved CRITICAL query requires the latest sidecar snapshot_id before gateway execution")
	}
	if s.snapshotVerifier == nil {
		return SnapshotManifest{}, table, fmt.Errorf("gateway --storage is required to verify sidecar snapshot_id before CRITICAL execution")
	}
	manifest, err := s.snapshotVerifier.VerifyLatestSidecarSnapshot(ctx, table, args.SnapshotID)
	if err != nil {
		s.metrics.IncRecoveryReadiness("failed", "snapshot_verification")
		return SnapshotManifest{}, table, fmt.Errorf("sidecar recovery point verification failed: %w", err)
	}
	if err := s.checkRecoveryReadiness(ctx, manifest); err != nil {
		s.metrics.IncRecoveryReadiness("failed", "readiness")
		return SnapshotManifest{}, table, err
	}
	if err := s.verifyRecoveryGroup(ctx, table, manifest); err != nil {
		s.metrics.IncRecoveryReadiness("failed", "group")
		return SnapshotManifest{}, table, err
	}
	s.metrics.IncRecoveryReadiness("ok", "verified")
	return manifest, table, nil
}

func (s *MCPServer) checkRecoveryReadiness(ctx context.Context, manifest SnapshotManifest) error {
	if s.policy.MaxSnapshotAgeSeconds > 0 {
		ts, err := parseManifestTime(manifest.Timestamp)
		if err != nil {
			return fmt.Errorf("snapshot timestamp is invalid: %w", err)
		}
		age := time.Since(ts)
		maxAge := time.Duration(s.policy.MaxSnapshotAgeSeconds) * time.Second
		if age > maxAge {
			return fmt.Errorf("recovery readiness failed: snapshot age %s exceeds RPO %s", age.Round(time.Second), maxAge)
		}
	}
	if s.policy.RequireSidecarHeartbeat {
		if s.registry == nil || s.registry.metadata == nil {
			return fmt.Errorf("recovery readiness failed: metadata DB is required for sidecar heartbeat")
		}
		status, checkedAt, ok := s.registry.metadata.GetHealth(ctx, "sync")
		if !ok {
			return fmt.Errorf("recovery readiness failed: sync sidecar heartbeat is missing")
		}
		if status != "healthy" && status != "starting" {
			return fmt.Errorf("recovery readiness failed: sync sidecar status is %s", status)
		}
		maxAge := time.Duration(s.policy.MaxSidecarHeartbeatSeconds) * time.Second
		if time.Since(checkedAt) > maxAge {
			return fmt.Errorf("recovery readiness failed: sync sidecar heartbeat is stale")
		}
	}
	return nil
}

func (s *MCPServer) verifyRecoveryGroup(ctx context.Context, table string, manifest SnapshotManifest) error {
	groupName, tables := s.recoveryGroupForTable(table)
	if groupName == "" || len(tables) <= 1 {
		return nil
	}
	latest, ok := s.snapshotVerifier.(interface {
		LatestSidecarSnapshot(context.Context, string) (SnapshotManifest, error)
	})
	if !ok {
		return fmt.Errorf("recovery group %s requires a snapshot verifier that can inspect all group tables", groupName)
	}
	var missing []string
	for _, member := range tables {
		if strings.EqualFold(member, table) {
			continue
		}
		groupManifest, err := latest.LatestSidecarSnapshot(ctx, member)
		if err != nil {
			missing = append(missing, member)
			continue
		}
		if err := s.checkRecoveryReadiness(ctx, groupManifest); err != nil {
			missing = append(missing, member)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("recovery group %s is missing ready snapshots for: %s", groupName, strings.Join(missing, ", "))
	}
	_ = manifest
	return nil
}

func (s *MCPServer) recoveryGroupForTable(table string) (string, []string) {
	for name, tables := range s.policy.RecoveryGroups {
		for _, member := range tables {
			if strings.EqualFold(member, table) {
				return name, tables
			}
		}
	}
	return "", nil
}

// toolRestoreSnapshot implements the restore_snapshot MCP tool.
func (s *MCPServer) toolRestoreSnapshot(ctx context.Context, id any, rawArgs json.RawMessage) MCPResponse {
	var args restoreSnapshotParams
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errorResponse(id, rpcInvalidParams, "invalid arguments: "+err.Error())
	}
	if args.SnapshotID == "" {
		return errorResponse(id, rpcInvalidParams, "snapshot_id is required")
	}
	if args.Table == "" {
		return errorResponse(id, rpcInvalidParams, "table is required")
	}
	if args.AgentID == "" {
		return errorResponse(id, rpcInvalidParams, "agent_id is required")
	}
	if s.defaultDBURL == "" {
		return errorResponse(id, rpcInvalidParams, "gateway --db is required to build the Python restore command")
	}
	if s.storage == "" {
		return errorResponse(id, rpcInvalidParams, "gateway --storage is required to build the Python restore command")
	}

	// Gateway validates intent; actual restore is performed by the Python SDK.
	// Record in audit log as a HIGH-risk action (restore modifies data).
	s.registry.RecordDetailed(args.AgentID, fmt.Sprintf("RESTORE %s FROM %s", args.Table, args.SnapshotID), RiskHigh, true, "", s.environment, s.clusterID)

	targetTable := args.Table + "_recovered"
	command := fmt.Sprintf(
		"backstop restore --db %s --storage %s --snapshot-id %s --table %s",
		s.defaultDBURL,
		s.storage,
		args.SnapshotID,
		args.Table,
	)
	return okResponse(id, map[string]any{
		"status":          "restore_command",
		"snapshot_id":     args.SnapshotID,
		"source_table":    args.Table,
		"target_table":    targetTable,
		"restore_command": command,
		"message":         "Gateway v1 does not restore directly. Run the returned Python CLI command.",
		"recorded_at":     time.Now().UTC().Format(time.RFC3339),
		"environment":     s.environment,
		"cluster_id":      s.clusterID,
	})
}

func (s *MCPServer) executeSQL(ctx context.Context, args executeQueryParams, analysis sqlAnalysis) (map[string]any, error) {
	db := s.db
	var closeDB func() error
	if db == nil {
		if strings.TrimSpace(args.DBURL) == "" {
			return nil, errors.New("gateway has no --db configured and db_url argument is empty")
		}
		opened, err := sql.Open("postgres", args.DBURL)
		if err != nil {
			return nil, err
		}
		db = opened
		closeDB = opened.Close
	}
	if closeDB != nil {
		defer closeDB()
	}

	if returnsRows(analysis) {
		rows, err := db.QueryContext(ctx, args.Query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		count := 0
		preview := make([]map[string]any, 0, 20)
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return nil, err
			}
			if len(preview) < 20 {
				row := make(map[string]any, len(cols))
				for i, col := range cols {
					row[col] = normalizeSQLValue(values[i])
				}
				preview = append(preview, row)
			}
			count++
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return map[string]any{
			"result_type": "rows",
			"columns":     cols,
			"row_count":   count,
			"preview":     preview,
		}, nil
	}

	result, err := db.ExecContext(ctx, args.Query)
	if err != nil {
		return nil, err
	}
	rowsAffected, _ := result.RowsAffected()
	return map[string]any{
		"result_type":   "command",
		"rows_affected": rowsAffected,
	}, nil
}

func returnsRows(analysis sqlAnalysis) bool {
	return analysis.Operation == "SELECT" ||
		analysis.Operation == "SHOW" ||
		strings.HasPrefix(analysis.Operation, "EXPLAIN")
}

func normalizeSQLValue(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	default:
		return v
	}
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func (s *MCPServer) safetyMetadata(analysis sqlAnalysis, decision PolicyDecision) map[string]any {
	metadata := map[string]any{
		"risk_level":          analysis.RiskLevel,
		"operation":           analysis.Operation,
		"reason":              analysis.Reason,
		"table":               nullableString(analysis.Table),
		"schema":              nullableString(analysis.Schema),
		"table_recoverable":   analysis.TableRecoverable,
		"recovery_required":   decision.RequiresRecovery,
		"recovery_possible":   decision.RecoveryPossible,
		"policy_action":       decision.Action,
		"policy_reason":       decision.Reason,
		"requires_approval":   decision.RequiresApproval,
		"parse_error_present": analysis.ParseError != nil,
		"environment":         s.environment,
		"cluster_id":          s.clusterID,
	}
	if analysis.Impact != nil {
		metadata["estimated_affected_rows"] = analysis.Impact.EstimatedAffectedRows
		metadata["estimated_table_rows"] = analysis.Impact.EstimatedTableRows
		metadata["affected_percent"] = analysis.Impact.AffectedPercent
		metadata["protected_table"] = analysis.Impact.ProtectedTable
		metadata["protected_columns"] = analysis.Impact.ProtectedColumns
		metadata["impact_status"] = analysis.Impact.Status
	}
	if analysis.ParseError != nil {
		metadata["parse_error"] = analysis.ParseError.Error()
	}
	return metadata
}

func querySHA256(query string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(query)))
	return hex.EncodeToString(sum[:])
}

func (s *MCPServer) emitAlert(ctx context.Context, severity, eventType, agentID string, analysis sqlAnalysis, querySHA, approvalID, snapshotID, recommendedAction, message string) {
	if s.alerts == nil {
		return
	}
	s.alerts.Emit(ctx, GatewayAlertPayload{
		Severity:          severity,
		EventType:         eventType,
		Environment:       s.environment,
		ClusterID:         s.clusterID,
		AgentID:           agentID,
		RiskLevel:         analysis.RiskLevel,
		Operation:         analysis.Operation,
		Schema:            analysis.Schema,
		Table:             analysis.Table,
		QuerySHA256:       querySHA,
		ApprovalID:        approvalID,
		SnapshotID:        snapshotID,
		RecommendedAction: recommendedAction,
		Message:           message,
	})
}

func blockReasonLabel(analysis sqlAnalysis, decision PolicyDecision) string {
	if analysis.Operation != "" {
		return analysis.Operation
	}
	if decision.Reason != "" {
		return decision.Reason
	}
	return "unknown"
}

// ---- helpers ------------------------------------------------------------

func okResponse(id any, result any) MCPResponse {
	return MCPResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id any, code int, message string) MCPResponse {
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}

