package main

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"os"
	"regexp"
	"sync"
	"time"
)

// AuditEntry represents a single audited query event for an agent.
type AuditEntry struct {
	AgentID     string    `json:"agent_id"`
	Query       string    `json:"query"`
	QuerySHA256 string    `json:"query_sha256,omitempty"`
	RiskLevel   string    `json:"risk_level"`
	Approved    bool      `json:"approved"`
	Environment string    `json:"environment,omitempty"`
	ClusterID   string    `json:"cluster_id,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// AgentRegistry is an append-only, thread-safe audit log.
type AgentRegistry struct {
	mu        sync.RWMutex
	entries   []AuditEntry
	auditPath string
	metadata  *MetadataStore
}

// NewAgentRegistry creates an empty AgentRegistry.
func NewAgentRegistry() *AgentRegistry {
	return NewAgentRegistryWithAuditLog("")
}

// NewAgentRegistryWithAuditLog creates a registry backed by an optional JSONL file.
func NewAgentRegistryWithAuditLog(auditPath string) *AgentRegistry {
	return NewAgentRegistryWithStores(auditPath, nil)
}

// NewAgentRegistryWithStores creates a registry backed by optional JSONL and SQLite metadata stores.
func NewAgentRegistryWithStores(auditPath string, metadata *MetadataStore) *AgentRegistry {
	registry := &AgentRegistry{
		entries:   make([]AuditEntry, 0),
		auditPath: auditPath,
		metadata:  metadata,
	}
	if auditPath != "" {
		if err := registry.loadAuditLog(); err != nil {
			log.Printf("backstop-gateway: failed to load audit log %s: %v", auditPath, err)
		}
	}
	return registry
}

func (r *AgentRegistry) loadAuditLog() error {
	file, err := os.Open(r.auditPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	loaded := make([]AuditEntry, 0)
	for scanner.Scan() {
		var entry AuditEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			log.Printf("backstop-gateway: skipping malformed audit entry: %v", err)
			continue
		}
		loaded = append(loaded, entry)
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	r.entries = append(r.entries, loaded...)
	return nil
}

func (r *AgentRegistry) appendAuditLog(entry AuditEntry) error {
	file, err := os.OpenFile(r.auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	return enc.Encode(entry)
}

// Record appends an audit entry for the given agent.
func (r *AgentRegistry) Record(agentID, query, riskLevel string, approved bool) {
	r.RecordDetailed(agentID, query, riskLevel, approved, "", "", "")
}

func (r *AgentRegistry) RecordDetailed(agentID, query, riskLevel string, approved bool, querySHA256, environment, clusterID string) {
	safeQuery := scrubSecrets(query)
	entry := AuditEntry{
		AgentID:     agentID,
		Query:       safeQuery,
		QuerySHA256: querySHA256,
		RiskLevel:   riskLevel,
		Approved:    approved,
		Environment: environment,
		ClusterID:   clusterID,
		Timestamp:   time.Now().UTC(),
	}
	r.mu.Lock()
	r.entries = append(r.entries, entry)
	if r.auditPath != "" {
		if err := r.appendAuditLog(entry); err != nil {
			log.Printf("backstop-gateway: failed to append audit log %s: %v", r.auditPath, err)
		}
	}
	if r.metadata != nil {
		r.metadata.RecordAudit(context.Background(), entry)
	}
	r.mu.Unlock()
}

func (r *AgentRegistry) RecordApprovalRequested(ctx context.Context, details ApprovalDetails) {
	if r == nil || r.metadata == nil {
		return
	}
	details.Query = scrubSecrets(details.Query)
	r.metadata.RecordApprovalRequested(ctx, details)
}

func (r *AgentRegistry) RecordApprovalResolved(ctx context.Context, approvalID, status, actor string) {
	if r == nil || r.metadata == nil {
		return
	}
	r.metadata.RecordApprovalResolved(ctx, approvalID, status, actor)
}

// GetHistory returns all audit entries for the given agentID.
// Returns an empty slice if the agent has no history.
func (r *AgentRegistry) GetHistory(agentID string) []AuditEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []AuditEntry
	for _, e := range r.entries {
		if e.AgentID == agentID {
			result = append(result, e)
		}
	}
	if result == nil {
		return []AuditEntry{}
	}
	return result
}

// AllEntries returns every audit entry regardless of agent.
func (r *AgentRegistry) AllEntries() []AuditEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]AuditEntry, len(r.entries))
	copy(out, r.entries)
	return out
}

func scrubSecrets(value string) string {
	replacements := []struct {
		pattern *regexp.Regexp
		value   string
	}{
		{regexp.MustCompile(`://([^:\s/]+):([^@\s]+)@`), `://$1:***@`},
		{regexp.MustCompile(`(?i)(password\s*)'[^']*'`), `${1}'***'`},
		{regexp.MustCompile(`(?i)(password\s*=\s*)'[^']*'`), `${1}'***'`},
		{regexp.MustCompile(`(?i)(password\s*=\s*)\S+`), `${1}***`},
		{regexp.MustCompile(`(?i)(aws_secret_access_key\s*=\s*)\S+`), `${1}***`},
		{regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)\S+`), `${1}***`},
	}
	out := value
	for _, replacement := range replacements {
		out = replacement.pattern.ReplaceAllString(out, replacement.value)
	}
	return out
}

