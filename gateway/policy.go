package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	PolicyActionExecute = "execute"
	PolicyActionApprove = "approve"
	PolicyActionBlock   = "block"
)

type GatewayPolicy struct {
	RequireApprovalForRisks           []string            `json:"require_approval_for_risks"`
	BlockOperations                   []string            `json:"block_operations"`
	RequireRecoveryForCritical        bool                `json:"require_recovery_for_critical"`
	BlockUnknownOrParseFailure        bool                `json:"block_unknown_or_parse_failure"`
	BlockUnrecoverableOperations      bool                `json:"block_unrecoverable_operations"`
	MaxSnapshotAgeSeconds             int                 `json:"max_snapshot_age_seconds"`
	RequireSidecarHeartbeat           bool                `json:"require_sidecar_heartbeat"`
	MaxSidecarHeartbeatSeconds        int                 `json:"max_sidecar_heartbeat_seconds"`
	RequireObjectChecksum             bool                `json:"require_object_checksum"`
	ImpactAnalysisEnabled             bool                `json:"impact_analysis_enabled"`
	MaxWriteRowsWithoutCritical       int64               `json:"max_write_rows_without_critical"`
	MaxWritePercentWithoutCritical    float64             `json:"max_write_percent_without_critical"`
	ProtectedTables                   []string            `json:"protected_tables"`
	ProtectedColumns                  map[string][]string `json:"protected_columns"`
	UnknownImpactAction               string              `json:"unknown_impact_action"`
	RecoveryGroups                    map[string][]string `json:"recovery_groups"`
	ForeignKeyGroupingEnabled         bool                `json:"foreign_key_grouping_enabled"`
	CascadeRiskRequiresGroupReadiness bool                `json:"cascade_risk_requires_group_readiness"`
	MaxBlockedAttemptsPerWindow       int                 `json:"max_blocked_attempts_per_window"`
	QuarantineDurationSeconds         int                 `json:"quarantine_duration_seconds"`
	DangerousRetryWindowSeconds       int                 `json:"dangerous_retry_window_seconds"`
	QuarantineBlocksSafeQueries       bool                `json:"quarantine_blocks_safe_queries"`
}

type PolicyDecision struct {
	Action           string `json:"policy_action"`
	Reason           string `json:"policy_reason"`
	RequiresApproval bool   `json:"requires_approval"`
	RequiresRecovery bool   `json:"recovery_required"`
	RecoveryPossible bool   `json:"recovery_possible"`
}

func DefaultGatewayPolicy() GatewayPolicy {
	return GatewayPolicy{
		RequireApprovalForRisks: []string{RiskHigh, RiskImpactCritical, RiskCritical},
		BlockOperations: []string{
			"DROP DATABASE",
			"DROP SCHEMA",
		},
		RequireRecoveryForCritical:     true,
		BlockUnknownOrParseFailure:     true,
		BlockUnrecoverableOperations:   true,
		MaxSnapshotAgeSeconds:          300,
		RequireSidecarHeartbeat:        true,
		MaxSidecarHeartbeatSeconds:     120,
		ImpactAnalysisEnabled:          true,
		MaxWriteRowsWithoutCritical:    1000,
		MaxWritePercentWithoutCritical: 50,
		UnknownImpactAction:            PolicyActionApprove,
		RecoveryGroups:                 map[string][]string{},
		ProtectedColumns:               map[string][]string{},
		MaxBlockedAttemptsPerWindow:    3,
		QuarantineDurationSeconds:      1800,
		DangerousRetryWindowSeconds:    600,
	}
}

func LoadGatewayPolicy(path string) (GatewayPolicy, error) {
	policy := DefaultGatewayPolicy()
	if strings.TrimSpace(path) == "" {
		return policy, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return GatewayPolicy{}, err
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		return GatewayPolicy{}, fmt.Errorf("parse policy file: %w", err)
	}
	if len(policy.RequireApprovalForRisks) == 0 {
		policy.RequireApprovalForRisks = DefaultGatewayPolicy().RequireApprovalForRisks
	}
	if len(policy.BlockOperations) == 0 {
		policy.BlockOperations = DefaultGatewayPolicy().BlockOperations
	}
	defaults := DefaultGatewayPolicy()
	if policy.MaxSnapshotAgeSeconds == 0 {
		policy.MaxSnapshotAgeSeconds = defaults.MaxSnapshotAgeSeconds
	}
	if policy.MaxSidecarHeartbeatSeconds == 0 {
		policy.MaxSidecarHeartbeatSeconds = defaults.MaxSidecarHeartbeatSeconds
	}
	if policy.MaxWriteRowsWithoutCritical == 0 {
		policy.MaxWriteRowsWithoutCritical = defaults.MaxWriteRowsWithoutCritical
	}
	if policy.MaxWritePercentWithoutCritical == 0 {
		policy.MaxWritePercentWithoutCritical = defaults.MaxWritePercentWithoutCritical
	}
	if policy.UnknownImpactAction == "" {
		policy.UnknownImpactAction = defaults.UnknownImpactAction
	}
	if policy.ProtectedColumns == nil {
		policy.ProtectedColumns = defaults.ProtectedColumns
	}
	if policy.RecoveryGroups == nil {
		policy.RecoveryGroups = defaults.RecoveryGroups
	}
	if policy.MaxBlockedAttemptsPerWindow == 0 {
		policy.MaxBlockedAttemptsPerWindow = defaults.MaxBlockedAttemptsPerWindow
	}
	if policy.QuarantineDurationSeconds == 0 {
		policy.QuarantineDurationSeconds = defaults.QuarantineDurationSeconds
	}
	if policy.DangerousRetryWindowSeconds == 0 {
		policy.DangerousRetryWindowSeconds = defaults.DangerousRetryWindowSeconds
	}
	return policy, nil
}

func (p GatewayPolicy) Decide(analysis sqlAnalysis) PolicyDecision {
	recoveryPossible := (analysis.RiskLevel == RiskCritical || analysis.RiskLevel == RiskImpactCritical) && analysis.TableRecoverable && strings.TrimSpace(analysis.Table) != ""

	if p.blocksOperation(analysis.Operation) {
		return PolicyDecision{
			Action:           PolicyActionBlock,
			Reason:           analysis.Operation + " is blocked by policy",
			RequiresApproval: false,
			RequiresRecovery: false,
			RecoveryPossible: recoveryPossible,
		}
	}

	if p.BlockUnknownOrParseFailure && (analysis.Operation == "PARSE_FAILURE" || analysis.Operation == "UNKNOWN" || strings.HasPrefix(analysis.Operation, "*")) {
		return PolicyDecision{
			Action:           PolicyActionBlock,
			Reason:           "unknown or unparsable SQL is blocked by policy",
			RequiresApproval: false,
			RequiresRecovery: false,
			RecoveryPossible: false,
		}
	}

	if (analysis.RiskLevel == RiskCritical || analysis.RiskLevel == RiskImpactCritical) && p.BlockUnrecoverableOperations && !recoveryPossible {
		return PolicyDecision{
			Action:           PolicyActionBlock,
			Reason:           "critical SQL has no single table recovery point",
			RequiresApproval: false,
			RequiresRecovery: false,
			RecoveryPossible: false,
		}
	}

	if p.requiresApproval(analysis.RiskLevel) {
		return PolicyDecision{
			Action:           PolicyActionApprove,
			Reason:           analysis.RiskLevel + " SQL requires approval",
			RequiresApproval: true,
			RequiresRecovery: (analysis.RiskLevel == RiskCritical || analysis.RiskLevel == RiskImpactCritical) && p.RequireRecoveryForCritical,
			RecoveryPossible: recoveryPossible,
		}
	}

	return PolicyDecision{
		Action:           PolicyActionExecute,
		Reason:           analysis.RiskLevel + " SQL may execute immediately",
		RequiresApproval: false,
		RequiresRecovery: false,
		RecoveryPossible: recoveryPossible,
	}
}

func (p GatewayPolicy) requiresApproval(risk string) bool {
	for _, value := range p.RequireApprovalForRisks {
		if strings.EqualFold(value, risk) {
			return true
		}
	}
	return false
}

func (p GatewayPolicy) blocksOperation(operation string) bool {
	for _, value := range p.BlockOperations {
		if strings.EqualFold(value, operation) {
			return true
		}
	}
	return false
}
