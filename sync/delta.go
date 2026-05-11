package main

import "sync"

// DeltaTracker tracks which tables exist in memory and detects changes between polls.
type DeltaTracker struct {
	mu     sync.Mutex
	known  map[string]struct{}
	seeded bool
}

// NewDeltaTracker creates an empty DeltaTracker.
func NewDeltaTracker() *DeltaTracker {
	return &DeltaTracker{
		known: make(map[string]struct{}),
	}
}

// Update receives the current set of table names.
// On the very first call it seeds the baseline and returns no changes,
// so that tables that already existed before the daemon started are not
// misreported as dropped.
//
// Returns:
//   - dropped: tables present in the previous snapshot but absent now.
//   - added:   tables absent in the previous snapshot but present now.
func (d *DeltaTracker) Update(tables []string) (dropped []string, added []string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	current := make(map[string]struct{}, len(tables))
	for _, t := range tables {
		current[t] = struct{}{}
	}

	if !d.seeded {
		// First call — establish baseline, report nothing.
		d.known = current
		d.seeded = true
		return nil, nil
	}

	// Tables in known but not in current → dropped.
	for t := range d.known {
		if _, ok := current[t]; !ok {
			dropped = append(dropped, t)
		}
	}

	// Tables in current but not in known → added.
	for t := range current {
		if _, ok := d.known[t]; !ok {
			added = append(added, t)
		}
	}

	d.known = current
	return dropped, added
}

// KnownTables returns a snapshot of the currently tracked table set.
func (d *DeltaTracker) KnownTables() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	tables := make([]string, 0, len(d.known))
	for t := range d.known {
		tables = append(tables, t)
	}
	return tables
}
