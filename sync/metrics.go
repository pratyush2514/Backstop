package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type SyncMetrics struct {
	mu           sync.Mutex
	snapshots    map[string]int64
	snapshotRows int64
	dropped      int64
	latest       map[string]time.Time
	bypass       map[string]int64
	posture      string
}

func NewSyncMetrics() *SyncMetrics {
	return &SyncMetrics{
		snapshots: make(map[string]int64),
		latest:    make(map[string]time.Time),
		bypass:    make(map[string]int64),
		posture:   PreventionHealthy,
	}
}

func (m *SyncMetrics) IncSnapshot(status string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshots[status]++
}

func (m *SyncMetrics) AddRows(rows int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshotRows += int64(rows)
}

func (m *SyncMetrics) MarkLatest(table string, ts time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latest[table] = ts
}

func (m *SyncMetrics) IncDropped() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropped++
}

func (m *SyncMetrics) IncBypass(role, application string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bypass[role+"|"+application]++
}

func (m *SyncMetrics) SetPosture(posture string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.posture = posture
}

func (m *SyncMetrics) Prometheus(now time.Time) string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder
	b.WriteString("# TYPE backstop_sync_snapshots_total counter\n")
	for _, status := range sortedCounterKeys(m.snapshots) {
		fmt.Fprintf(&b, "backstop_sync_snapshots_total{status=%q} %d\n", status, m.snapshots[status])
	}
	b.WriteString("# TYPE backstop_sync_snapshot_rows_total counter\n")
	fmt.Fprintf(&b, "backstop_sync_snapshot_rows_total %d\n", m.snapshotRows)
	b.WriteString("# TYPE backstop_sync_latest_snapshot_age_seconds gauge\n")
	for _, table := range sortedTimeKeys(m.latest) {
		age := now.Sub(m.latest[table]).Seconds()
		if age < 0 {
			age = 0
		}
		fmt.Fprintf(&b, "backstop_sync_latest_snapshot_age_seconds{table=%q} %.0f\n", table, age)
	}
	b.WriteString("# TYPE backstop_sync_dropped_tables_total counter\n")
	fmt.Fprintf(&b, "backstop_sync_dropped_tables_total %d\n", m.dropped)
	b.WriteString("# TYPE backstop_bypass_connections_total counter\n")
	for _, key := range sortedCounterKeys(m.bypass) {
		parts := strings.SplitN(key, "|", 2)
		role, application := parts[0], ""
		if len(parts) == 2 {
			application = parts[1]
		}
		fmt.Fprintf(&b, "backstop_bypass_connections_total{role=%q,application=%q} %d\n", role, application, m.bypass[key])
	}
	b.WriteString("# TYPE backstop_prevention_posture gauge\n")
	for _, posture := range []string{PreventionHealthy, PreventionDegraded, PreventionRecoveryOnly} {
		value := 0
		if m.posture == posture {
			value = 1
		}
		fmt.Fprintf(&b, "backstop_prevention_posture{status=%q} %d\n", posture, value)
	}
	return b.String()
}

func sortedCounterKeys(values map[string]int64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedTimeKeys(values map[string]time.Time) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

