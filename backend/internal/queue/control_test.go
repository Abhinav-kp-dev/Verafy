package queue

import "testing"

// River's JobDeleteManyParams.First panics above 10,000 — this guards against
// ever regressing PurgeQueued back to a single unbounded call (it crashed the
// whole process the first time, recovered only by Go's stdlib per-request
// recover, not by anything in our own code).
func TestPurgeQueuedPageSizeNeverExceedsRiverCap(t *testing.T) {
	if riverDeleteManyMaxCount > 10000 {
		t.Fatalf("riverDeleteManyMaxCount = %d exceeds River's hard cap of 10000 and will panic", riverDeleteManyMaxCount)
	}
}
