package httpapi

import (
	"context"
	"fmt"
	"strings"
)

func (s *Server) recoverProcessingApprovals(ctx context.Context) {
	if s.needsSetup() {
		return
	}
	requests, err := s.db.ListRequestsByStatus(ctxOrBackground(ctx), "processing", 0)
	if err != nil {
		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: failed to recover processing approvals: %v\n", err)
		}
		return
	}
	for i := range requests {
		req := requests[i]
		inst, ok := s.readarrInstanceForFormat(req.Format)
		if !ok {
			_ = s.db.UpdateRequestStatus(ctxOrBackground(ctx), req.ID, "error", "readarr not configured; could not restore queued approval after restart", "system", nil, nil)
			continue
		}
		username := strings.TrimSpace(req.ApproverEmail)
		if username == "" {
			username = "system"
		}
		if err := s.enqueueAsyncApproval(req.ID, &req, inst, username); err != nil {
			_ = s.db.UpdateRequestStatus(ctxOrBackground(ctx), req.ID, "error", err.Error(), "system", nil, nil)
		}
	}
}
