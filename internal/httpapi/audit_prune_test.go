package httpapi

import (
	"context"
	"testing"
)

func TestPruneAuditEvents(t *testing.T) {
	s := newServerForTest(t)

	// First verify retention 0 (default) is a no-op
	cfg := s.settings.Get()
	if cfg.Audit.RetentionDays != 0 {
		t.Fatal("expected default retention 0")
	}
	s.pruneAuditEvents(context.Background()) // should not panic

	// Set a retention policy and confirm it runs
	cfg.Audit.RetentionDays = 90
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	s.pruneAuditEvents(context.Background()) // should prune (table is empty, so 0 rows)

	// Also test with an audit event present to cover the prune branch
	_ = s.db.InsertAuditEvent(context.Background(), "testuser", "test_action", nil, "test details")
	s.pruneAuditEvents(context.Background()) // retention 90 on fresh event = no-op
}
