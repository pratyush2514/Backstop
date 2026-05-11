package main

import (
	"context"
	"log"
	"time"
)

func (s *MCPServer) agentQuarantined(ctx context.Context, agentID string, analysis sqlAnalysis) bool {
	if s.registry == nil || s.registry.metadata == nil {
		return false
	}
	state, ok := s.registry.metadata.GetAgentState(ctx, agentID)
	if !ok || state.QuarantineUntil == nil {
		return false
	}
	if time.Now().UTC().After(*state.QuarantineUntil) {
		return false
	}
	return s.policy.QuarantineBlocksSafeQueries || analysis.RiskLevel != RiskSafe
}

func (s *MCPServer) recordRiskAttempt(ctx context.Context, agentID string, analysis sqlAnalysis, reason string) {
	if analysis.RiskLevel == RiskSafe || s.registry == nil || s.registry.metadata == nil {
		return
	}
	now := time.Now().UTC()
	window := time.Duration(s.policy.DangerousRetryWindowSeconds) * time.Second
	quarantineDuration := time.Duration(s.policy.QuarantineDurationSeconds) * time.Second
	state, ok := s.registry.metadata.GetAgentState(ctx, agentID)
	if !ok || now.Sub(state.WindowStartedAt) > window {
		state = AgentState{
			AgentID:         agentID,
			RiskyAttempts:   0,
			WindowStartedAt: now,
		}
	}
	state.RiskyAttempts++
	state.LastReason = reason
	state.LastTable = analysis.Table
	if state.RiskyAttempts >= s.policy.MaxBlockedAttemptsPerWindow {
		until := now.Add(quarantineDuration)
		newQuarantine := state.QuarantineUntil == nil || state.QuarantineUntil.Before(now)
		state.QuarantineUntil = &until
		if newQuarantine {
			s.metrics.IncAgentQuarantine()
			s.emitAlert(ctx, "critical", "agent_quarantined", agentID, analysis, "", "", "", "Inspect agent behavior and deny similar destructive retries.", "Agent quarantined after repeated risky attempts.")
		}
	}
	if err := s.registry.metadata.SaveAgentState(ctx, state); err != nil {
		log.Printf("backstop-gateway: failed to persist agent state: %v", err)
		s.metrics.IncBlock("metadata_write_failed")
		return
	}
	s.metrics.IncRiskyAttempt(analysis.RiskLevel)
}
