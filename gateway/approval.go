package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrApprovalDenied is returned when an approval request is explicitly denied.
var ErrApprovalDenied = errors.New("approval denied by operator")

// ErrApprovalTimeout is returned when the approval window expires.
var ErrApprovalTimeout = errors.New("approval timed out — auto-denied")

// ErrApprovalNotFound is returned when Approve/Deny targets an unknown ID.
var ErrApprovalNotFound = errors.New("approval ID not found")

// pendingApproval holds the channel through which a decision is signalled.
type pendingApproval struct {
	details   ApprovalDetails
	createdAt time.Time
	decision  chan ApprovalResolution
}

// ApprovalRequest is the JSON-serialisable view of a pending approval.
type ApprovalRequest struct {
	ID          string    `json:"id"`
	Query       string    `json:"query"`
	QuerySHA256 string    `json:"query_sha256"`
	AgentID     string    `json:"agent_id"`
	RiskLevel   string    `json:"risk_level"`
	Operation   string    `json:"operation"`
	Schema      string    `json:"schema,omitempty"`
	Table       string    `json:"table,omitempty"`
	Environment string    `json:"environment"`
	ClusterID   string    `json:"cluster_id"`
	SnapshotID  string    `json:"snapshot_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type ApprovalDetails struct {
	ID          string
	Query       string
	QuerySHA256 string
	AgentID     string
	RiskLevel   string
	Operation   string
	Schema      string
	Table       string
	Environment string
	ClusterID   string
	SnapshotID  string
}

type ApprovalResolution struct {
	Approved bool
	Actor    string
}

// ApprovalEngine manages human-in-the-loop approval workflows.
// All methods are safe for concurrent use.
type ApprovalEngine struct {
	timeout time.Duration
	pending sync.Map // map[string]*pendingApproval
}

// NewApprovalEngine creates an ApprovalEngine with the given per-request timeout.
func NewApprovalEngine(timeout time.Duration) *ApprovalEngine {
	return &ApprovalEngine{timeout: timeout}
}

// RequestApproval creates a pending approval record and blocks until an operator
// approves or denies, or the timeout expires (auto-deny).
// The provided ctx can also cancel the wait (e.g. on server shutdown).
func (e *ApprovalEngine) RequestApproval(ctx context.Context, details ApprovalDetails) (ApprovalResolution, error) {
	if details.ID == "" {
		return ApprovalResolution{}, fmt.Errorf("approval id is required")
	}
	pa := &pendingApproval{
		details:   details,
		createdAt: time.Now().UTC(),
		decision:  make(chan ApprovalResolution, 1),
	}

	e.pending.Store(details.ID, pa)
	defer e.pending.Delete(details.ID)

	timer := time.NewTimer(e.timeout)
	defer timer.Stop()

	select {
	case resolution := <-pa.decision:
		if !resolution.Approved {
			return resolution, ErrApprovalDenied
		}
		return resolution, nil

	case <-timer.C:
		return ApprovalResolution{}, ErrApprovalTimeout

	case <-ctx.Done():
		return ApprovalResolution{}, fmt.Errorf("approval cancelled: %w", ctx.Err())
	}
}

// Approve signals that the operator approved the request with the given ID.
func (e *ApprovalEngine) Approve(id, actor string) error {
	return e.signal(id, ApprovalResolution{Approved: true, Actor: actor})
}

// Deny signals that the operator denied the request with the given ID.
func (e *ApprovalEngine) Deny(id, actor string) error {
	return e.signal(id, ApprovalResolution{Approved: false, Actor: actor})
}

// signal sends a decision on the pending approval's channel.
func (e *ApprovalEngine) signal(id string, resolution ApprovalResolution) error {
	val, ok := e.pending.Load(id)
	if !ok {
		return ErrApprovalNotFound
	}
	pa := val.(*pendingApproval)
	// Non-blocking send: if the channel already has a value the request already
	// resolved (timeout/cancel). This prevents a goroutine leak.
	select {
	case pa.decision <- resolution:
	default:
	}
	return nil
}

// PendingList returns a snapshot of all currently pending approvals.
func (e *ApprovalEngine) PendingList() []ApprovalRequest {
	var result []ApprovalRequest
	e.pending.Range(func(_, val any) bool {
		pa := val.(*pendingApproval)
		details := pa.details
		result = append(result, ApprovalRequest{
			ID:          details.ID,
			Query:       details.Query,
			QuerySHA256: details.QuerySHA256,
			AgentID:     details.AgentID,
			RiskLevel:   details.RiskLevel,
			Operation:   details.Operation,
			Schema:      details.Schema,
			Table:       details.Table,
			Environment: details.Environment,
			ClusterID:   details.ClusterID,
			SnapshotID:  details.SnapshotID,
			CreatedAt:   pa.createdAt,
		})
		return true
	})
	return result
}
