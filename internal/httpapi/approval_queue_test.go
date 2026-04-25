package httpapi

import (
	"testing"
	"time"
)

func TestApprovalQueueCapacityKeepsWorstCaseWaitUnderMax(t *testing.T) {
	interval := 30 * time.Second
	jitter := 15 * time.Second
	maxWait := 3 * time.Hour

	got := approvalQueueCapacity(interval, jitter, maxWait)
	if got != 240 {
		t.Fatalf("expected capacity 240, got %d", got)
	}

	if worstCaseWait := time.Duration(got) * (interval + jitter); worstCaseWait > maxWait {
		t.Fatalf("worst-case queue wait exceeds max: %s > %s", worstCaseWait, maxWait)
	}
}
