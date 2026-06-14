package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

func (s *Server) tryCompleteApprovalFromCatalogMatch(ctx context.Context, req *db.Request, inst providers.ReadarrInstance, username, reasonSuffix string, notify bool) (bool, error) {
	match, err := s.findCatalogMatch(ctx, req.Format, req.Title, req.Authors, req.ISBN10, req.ISBN13, "", req.ReadarrReq)
	if err == sql.ErrNoRows || match == nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if err := s.completeRequestFromCatalogMatch(ctx, req, match, inst, username, reasonSuffix, notify); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) completeRequestFromCatalogMatch(ctx context.Context, req *db.Request, match *db.ReadarrBook, inst providers.ReadarrInstance, username, reasonSuffix string, notify bool) error {
	if match == nil {
		return sql.ErrNoRows
	}

	externalStatus := match.Availability()
	reason := "already in Readarr" + reasonSuffix

	if match.ReadarrID > 0 && !match.Monitored {
		ra := providers.NewReadarrWithDB(inst, s.db.SQL())
		if _, err := ra.MonitorBooks(ctx, []int{int(match.ReadarrID)}, true); err == nil {
			if externalStatus == "" {
				externalStatus = "monitored"
			}
			reason = fmt.Sprintf("already in Readarr; monitoring enabled for id %d%s", match.ReadarrID, reasonSuffix)
		} else {
			reason = fmt.Sprintf("already in Readarr; failed to enable monitoring for id %d%s: %v", match.ReadarrID, reasonSuffix, err)
		}
	} else if externalStatus != "" {
		reason = fmt.Sprintf("already in Readarr as %s%s", externalStatus, reasonSuffix)
	}

	_ = s.db.ApproveRequest(ctx, req.ID, username)
	// Write the external status (matched id + availability) before flipping the
	// status to "queued". Observers treat "queued" as the completion signal, so
	// the matched id must already be persisted by the time that status is visible.
	_ = s.db.UpdateRequestExternalStatus(ctx, req.ID, externalStatus, match.ReadarrID, reason)
	_ = s.db.UpdateRequestStatus(ctx, req.ID, "queued", reason, username, req.ReadarrReq, match.ReadarrData)
	if cover := s.requestCoverFromPayload(req.Format, match.ReadarrData); cover != "" {
		_ = s.db.UpdateRequestCover(ctx, req.ID, cover)
	}
	if notify {
		s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)
	}
	return nil
}

// ensureReadarrMonitoredAndSearch makes sure an already-cataloged Readarr book
// is monitored and queues a Readarr search command for it. It returns the
// resulting availability label (e.g. "monitored") so callers can surface a
// status to the user.
func (s *Server) ensureReadarrMonitoredAndSearch(ctx context.Context, format string, match *db.ReadarrBook) (string, error) {
	if match == nil || match.ReadarrID <= 0 {
		return "", fmt.Errorf("no Readarr id for catalog match")
	}
	inst, ok := s.readarrInstanceForFormat(format)
	if !ok {
		return "", fmt.Errorf("no Readarr instance configured for %s", format)
	}
	ra := providers.NewReadarrWithDB(inst, s.db.SQL())
	id := int(match.ReadarrID)
	if !match.Monitored {
		if _, err := ra.MonitorBooks(ctx, []int{id}, true); err != nil {
			return "", fmt.Errorf("enable monitoring: %w", err)
		}
	}
	if _, err := ra.SearchBooks(ctx, []int{id}); err != nil {
		return "", fmt.Errorf("trigger search: %w", err)
	}
	status := match.Availability()
	if status == "" {
		status = "monitored"
	}
	return status, nil
}

func readarrStateFromResponse(body []byte) (int64, string) {
	if len(body) == 0 {
		return 0, ""
	}

	var raw any
	if json.Unmarshal(body, &raw) != nil {
		return 0, ""
	}

	parseMap := func(m map[string]any) (int64, string) {
		id := int64(inputIntValue(m, "id"))
		bookFileCount := 0
		if stats, ok := m["statistics"].(map[string]any); ok {
			bookFileCount = inputIntValue(stats, "bookFileCount")
		}
		grabbed, _ := m["grabbed"].(bool)
		monitored, _ := m["monitored"].(bool)
		switch {
		case bookFileCount > 0:
			return id, "available"
		case grabbed:
			return id, "grabbed"
		case monitored:
			return id, "monitored"
		default:
			return id, ""
		}
	}

	switch value := raw.(type) {
	case map[string]any:
		return parseMap(value)
	case []any:
		for _, item := range value {
			if m, ok := item.(map[string]any); ok {
				id, status := parseMap(m)
				if id > 0 || strings.TrimSpace(status) != "" {
					return id, status
				}
			}
		}
	}

	return 0, ""
}
