package httpapi

import (
	"context"
	"fmt"
)

// auditLog records a best-effort audit trail entry. Failures are swallowed
// (logged) rather than surfaced, since audit logging must never block the
// action it's recording.
func (s *Server) auditLog(ctx context.Context, actorEmail, eventType string, requestID *int64, details string) {
	if err := s.db.InsertAuditEvent(ctx, actorEmail, eventType, requestID, details); err != nil {
		fmt.Printf("audit: failed to record %s for %s: %v\n", eventType, actorEmail, err)
	}
}
