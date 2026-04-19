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
	_ = s.db.UpdateRequestStatus(ctx, req.ID, "queued", reason, username, req.ReadarrReq, match.ReadarrData)
	_ = s.db.UpdateRequestExternalStatus(ctx, req.ID, externalStatus, match.ReadarrID, reason)
	if cover := s.requestCoverFromPayload(req.Format, match.ReadarrData); cover != "" {
		_ = s.db.UpdateRequestCover(ctx, req.ID, cover)
	}
	if notify {
		s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)
	}
	return true, nil
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
