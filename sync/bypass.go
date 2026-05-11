package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	PreventionHealthy      = "healthy"
	PreventionDegraded     = "degraded"
	PreventionRecoveryOnly = "recovery_only"
)

type BypassConfig struct {
	AllowedApplicationNames []string
	AllowedRoles            []string
	AllowedClientAddresses  []string
	AgentRoles              []string
	GatewayApplicationName  string
}

type DBActivity struct {
	PID             int
	Role            string
	ApplicationName string
	ClientAddress   string
	QueryStart      time.Time
	Query           string
}

type BypassFinding struct {
	Posture  string
	Reason   string
	Activity DBActivity
}

type BypassDetector struct {
	db     *sql.DB
	config BypassConfig
}

func NewBypassDetector(db *sql.DB, config BypassConfig) *BypassDetector {
	return &BypassDetector{db: db, config: config}
}

func (d *BypassDetector) Poll(ctx context.Context) ([]BypassFinding, error) {
	const query = `
		SELECT pid, usename, application_name, COALESCE(client_addr::text, ''), COALESCE(query_start, now()), COALESCE(query, '')
		FROM pg_stat_activity
		WHERE pid <> pg_backend_pid()
		  AND state <> 'idle'`
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var activities []DBActivity
	for rows.Next() {
		var a DBActivity
		if err := rows.Scan(&a.PID, &a.Role, &a.ApplicationName, &a.ClientAddress, &a.QueryStart, &a.Query); err != nil {
			return nil, err
		}
		activities = append(activities, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return d.Evaluate(activities), nil
}

func (d *BypassDetector) Evaluate(activities []DBActivity) []BypassFinding {
	var findings []BypassFinding
	for _, activity := range activities {
		agentRole := containsFold(d.config.AgentRoles, activity.Role)
		if len(d.config.AgentRoles) == 0 {
			agentRole = true
		}
		if !agentRole {
			continue
		}
		if d.allowed(activity) {
			continue
		}
		findings = append(findings, BypassFinding{
			Posture:  PreventionDegraded,
			Reason:   fmt.Sprintf("agent-like role %s connected outside allowed gateway posture", activity.Role),
			Activity: activity,
		})
	}
	return findings
}

func (d *BypassDetector) allowed(activity DBActivity) bool {
	if d.config.GatewayApplicationName != "" && strings.EqualFold(activity.ApplicationName, d.config.GatewayApplicationName) {
		return true
	}
	if len(d.config.AllowedApplicationNames) > 0 && !containsFold(d.config.AllowedApplicationNames, activity.ApplicationName) {
		return false
	}
	if len(d.config.AllowedRoles) > 0 && !containsFold(d.config.AllowedRoles, activity.Role) {
		return false
	}
	if len(d.config.AllowedClientAddresses) > 0 && !addressAllowed(d.config.AllowedClientAddresses, activity.ClientAddress) {
		return false
	}
	return true
}

func parseCSV(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func addressAllowed(allowed []string, candidate string) bool {
	for _, value := range allowed {
		if value == candidate {
			return true
		}
		if _, network, err := net.ParseCIDR(value); err == nil {
			ip := net.ParseIP(candidate)
			if ip != nil && network.Contains(ip) {
				return true
			}
		}
	}
	return false
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
