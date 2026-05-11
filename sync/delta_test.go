package main

import "testing"

func TestDeltaTrackerSeedsWithoutReportingChanges(t *testing.T) {
	tracker := NewDeltaTracker()
	dropped, added := tracker.Update([]string{"users", "orders"})
	if len(dropped) != 0 || len(added) != 0 {
		t.Fatalf("first update should seed only, got dropped=%v added=%v", dropped, added)
	}
}

func TestDeltaTrackerReportsAddedAndDroppedTables(t *testing.T) {
	tracker := NewDeltaTracker()
	tracker.Update([]string{"users", "orders"})

	dropped, added := tracker.Update([]string{"users", "payments"})
	if len(dropped) != 1 || dropped[0] != "orders" {
		t.Fatalf("dropped = %v, want [orders]", dropped)
	}
	if len(added) != 1 || added[0] != "payments" {
		t.Fatalf("added = %v, want [payments]", added)
	}
}
