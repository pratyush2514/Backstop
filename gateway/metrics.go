package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type GatewayMetrics struct {
	mu                    sync.Mutex
	queries               map[string]int64
	approvals             map[string]int64
	blocks                map[string]int64
	recoveryVerifications map[string]int64
	recoveryReadiness     map[string]int64
	agentRiskyAttempts    map[string]int64
	agentQuarantines      int64
	alerts                map[string]int64
	pauseState            int64
}

func NewGatewayMetrics() *GatewayMetrics {
	return &GatewayMetrics{
		queries:               make(map[string]int64),
		approvals:             make(map[string]int64),
		blocks:                make(map[string]int64),
		recoveryVerifications: make(map[string]int64),
		recoveryReadiness:     make(map[string]int64),
		agentRiskyAttempts:    make(map[string]int64),
		alerts:                make(map[string]int64),
	}
}

func (m *GatewayMetrics) IncQuery(risk, status string) {
	if m == nil {
		return
	}
	m.inc(m.queries, risk+"|"+status)
}

func (m *GatewayMetrics) IncApproval(decision string) {
	if m == nil {
		return
	}
	m.inc(m.approvals, decision)
}

func (m *GatewayMetrics) IncBlock(reason string) {
	if m == nil {
		return
	}
	m.inc(m.blocks, reason)
}

func (m *GatewayMetrics) IncRecovery(status string) {
	if m == nil {
		return
	}
	m.inc(m.recoveryVerifications, status)
}

func (m *GatewayMetrics) IncRecoveryReadiness(status, reason string) {
	if m == nil {
		return
	}
	m.inc(m.recoveryReadiness, status+"|"+reason)
}

func (m *GatewayMetrics) IncRiskyAttempt(risk string) {
	if m == nil {
		return
	}
	m.inc(m.agentRiskyAttempts, risk)
}

func (m *GatewayMetrics) IncAgentQuarantine() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agentQuarantines++
}

func (m *GatewayMetrics) IncAlert(eventType, status string) {
	if m == nil {
		return
	}
	m.inc(m.alerts, eventType+"|"+status)
}

func (m *GatewayMetrics) SetPauseState(paused bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if paused {
		m.pauseState = 1
		return
	}
	m.pauseState = 0
}

func (m *GatewayMetrics) inc(values map[string]int64, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	values[key]++
}

func (m *GatewayMetrics) Prometheus() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder
	b.WriteString("# TYPE backstop_gateway_queries_total counter\n")
	for _, key := range sortedMetricKeys(m.queries) {
		parts := strings.SplitN(key, "|", 2)
		risk, status := parts[0], ""
		if len(parts) == 2 {
			status = parts[1]
		}
		fmt.Fprintf(&b, "backstop_gateway_queries_total{risk=%q,status=%q} %d\n", risk, status, m.queries[key])
	}
	b.WriteString("# TYPE backstop_gateway_approvals_total counter\n")
	for _, key := range sortedMetricKeys(m.approvals) {
		fmt.Fprintf(&b, "backstop_gateway_approvals_total{decision=%q} %d\n", key, m.approvals[key])
	}
	b.WriteString("# TYPE backstop_gateway_blocks_total counter\n")
	for _, key := range sortedMetricKeys(m.blocks) {
		fmt.Fprintf(&b, "backstop_gateway_blocks_total{reason=%q} %d\n", key, m.blocks[key])
	}
	b.WriteString("# TYPE backstop_gateway_recovery_verifications_total counter\n")
	for _, key := range sortedMetricKeys(m.recoveryVerifications) {
		fmt.Fprintf(&b, "backstop_gateway_recovery_verifications_total{status=%q} %d\n", key, m.recoveryVerifications[key])
	}
	b.WriteString("# TYPE backstop_recovery_readiness_total counter\n")
	for _, key := range sortedMetricKeys(m.recoveryReadiness) {
		parts := strings.SplitN(key, "|", 2)
		status, reason := parts[0], ""
		if len(parts) == 2 {
			reason = parts[1]
		}
		fmt.Fprintf(&b, "backstop_recovery_readiness_total{status=%q,reason=%q} %d\n", status, reason, m.recoveryReadiness[key])
	}
	b.WriteString("# TYPE backstop_agent_risky_attempts_total counter\n")
	for _, key := range sortedMetricKeys(m.agentRiskyAttempts) {
		fmt.Fprintf(&b, "backstop_agent_risky_attempts_total{risk=%q} %d\n", key, m.agentRiskyAttempts[key])
	}
	b.WriteString("# TYPE backstop_agent_quarantines_total counter\n")
	fmt.Fprintf(&b, "backstop_agent_quarantines_total %d\n", m.agentQuarantines)
	b.WriteString("# TYPE backstop_gateway_alerts_total counter\n")
	for _, key := range sortedMetricKeys(m.alerts) {
		parts := strings.SplitN(key, "|", 2)
		eventType, status := parts[0], ""
		if len(parts) == 2 {
			status = parts[1]
		}
		fmt.Fprintf(&b, "backstop_gateway_alerts_total{event_type=%q,status=%q} %d\n", eventType, status, m.alerts[key])
	}
	b.WriteString("# TYPE backstop_gateway_pause_state gauge\n")
	fmt.Fprintf(&b, "backstop_gateway_pause_state %d\n", m.pauseState)
	return b.String()
}

func sortedMetricKeys(values map[string]int64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
