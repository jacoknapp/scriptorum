package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type Request struct {
	ID             int64           `json:"id"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	RequesterEmail string          `json:"requesterEmail"`
	Title          string          `json:"title"`
	Authors        []string        `json:"authors"`
	ISBN10         string          `json:"isbn10"`
	ISBN13         string          `json:"isbn13"`
	Format         string          `json:"format"`
	Status         string          `json:"status"`
	StatusReason   string          `json:"statusReason"`
	ApproverEmail  string          `json:"approverEmail"`
	ApprovedAt     *time.Time      `json:"approvedAt,omitempty"`
	ReadarrReq     json.RawMessage `json:"readarrRequest,omitempty"`
	ReadarrResp    json.RawMessage `json:"readarrResponse,omitempty"`
}

func (d *DB) CreateRequest(ctx context.Context, r *Request) (int64, error) {
	now := time.Now().UTC()
	r.CreatedAt, r.UpdatedAt = now, now
	authorsJSON, _ := json.Marshal(r.Authors)
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO requests
(created_at, updated_at, requester_email, title, authors, isbn10, isbn13, format, status, status_reason, readarr_request, readarr_response)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		strings.ToLower(r.RequesterEmail), r.Title, string(authorsJSON),
		r.ISBN10, r.ISBN13, r.Format, r.Status, r.StatusReason,
		bytesOrNil(r.ReadarrReq), bytesOrNil(r.ReadarrResp),
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (d *DB) UpdateRequestStatus(ctx context.Context, id int64, status, reason, actor string, readarrReq, readarrResp []byte) error {
	now := time.Now().UTC()
	_, err := d.sql.ExecContext(ctx, `
UPDATE requests
SET status=?, status_reason=?, approver_email=COALESCE(approver_email, ?), updated_at=?, readarr_request=COALESCE(?, readarr_request), readarr_response=COALESCE(?, readarr_response)
WHERE id=?`,
		status, reason, strings.ToLower(actor), now.Format(time.RFC3339Nano), bytesOrNil(readarrReq), bytesOrNil(readarrResp), id,
	)
	return err
}

func bytesOrNil(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func (d *DB) ApproveRequest(ctx context.Context, id int64, actor string) error {
	now := time.Now().UTC()
	_, err := d.sql.ExecContext(ctx, `
UPDATE requests
SET status='approved', approved_at=?, approver_email=?, updated_at=?
WHERE id=?`,
		now.Format(time.RFC3339Nano), strings.ToLower(actor), now.Format(time.RFC3339Nano), id,
	)
	return err
}

func (d *DB) GetRequest(ctx context.Context, id int64) (*Request, error) {
	row := d.sql.QueryRowContext(ctx, `
SELECT id, created_at, updated_at, requester_email, title, authors, isbn10, isbn13, format, status, status_reason, approver_email, approved_at, readarr_request, readarr_response
FROM requests WHERE id=?`, id)
	var rr Request
	var created, updated, approved sql.NullString
	var authorsStr sql.NullString
	var approver sql.NullString
	var readarrReqStr, readarrRespStr sql.NullString
	if err := row.Scan(&rr.ID, &created, &updated, &rr.RequesterEmail, &rr.Title, &authorsStr, &rr.ISBN10, &rr.ISBN13, &rr.Format, &rr.Status, &rr.StatusReason, &approver, &approved, &readarrReqStr, &readarrRespStr); err != nil {
		return nil, err
	}
	if approver.Valid {
		rr.ApproverEmail = approver.String
	}
	if readarrReqStr.Valid {
		rr.ReadarrReq = json.RawMessage([]byte(readarrReqStr.String))
	}
	if readarrRespStr.Valid {
		rr.ReadarrResp = json.RawMessage([]byte(readarrRespStr.String))
	}
	rr.CreatedAt, _ = time.Parse(time.RFC3339Nano, created.String)
	rr.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated.String)
	if approved.Valid {
		t, _ := time.Parse(time.RFC3339Nano, approved.String)
		rr.ApprovedAt = &t
	}
	if authorsStr.Valid && authorsStr.String != "" {
		_ = json.Unmarshal([]byte(authorsStr.String), &rr.Authors)
	}
	return &rr, nil
}

func (d *DB) ListRequests(ctx context.Context, mine string, limit int) ([]Request, error) {
	if limit <= 0 {
		limit = 200
	}
	var rows *sql.Rows
	var err error
	if mine != "" {
		rows, err = d.sql.QueryContext(ctx, `
SELECT id, created_at, updated_at, requester_email, title, authors, isbn10, isbn13, format, status, status_reason, approver_email, approved_at, readarr_request, readarr_response
FROM requests
WHERE requester_email=?
ORDER BY id DESC LIMIT ?`, strings.ToLower(mine), limit)
	} else {
		rows, err = d.sql.QueryContext(ctx, `
SELECT id, created_at, updated_at, requester_email, title, authors, isbn10, isbn13, format, status, status_reason, approver_email, approved_at, readarr_request, readarr_response
FROM requests
ORDER BY id DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Request
	for rows.Next() {
		var rr Request
		var created, updated, approved sql.NullString
		var authorsStr sql.NullString
		var approver sql.NullString
		var readarrReqStr, readarrRespStr sql.NullString
		if err := rows.Scan(&rr.ID, &created, &updated, &rr.RequesterEmail, &rr.Title, &authorsStr, &rr.ISBN10, &rr.ISBN13, &rr.Format, &rr.Status, &rr.StatusReason, &approver, &approved, &readarrReqStr, &readarrRespStr); err != nil {
			return nil, err
		}
		if approver.Valid {
			rr.ApproverEmail = approver.String
		}
		if readarrReqStr.Valid {
			rr.ReadarrReq = json.RawMessage([]byte(readarrReqStr.String))
		}
		if readarrRespStr.Valid {
			rr.ReadarrResp = json.RawMessage([]byte(readarrRespStr.String))
		}
		rr.CreatedAt, _ = time.Parse(time.RFC3339Nano, created.String)
		rr.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated.String)
		if approved.Valid {
			t, _ := time.Parse(time.RFC3339Nano, approved.String)
			rr.ApprovedAt = &t
		}
		if authorsStr.Valid && authorsStr.String != "" {
			_ = json.Unmarshal([]byte(authorsStr.String), &rr.Authors)
		}
		out = append(out, rr)
	}
	return out, nil
}

func (d *DB) DeleteRequest(ctx context.Context, id int64) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM requests WHERE id=?`, id)
	return err
}

func (d *DB) AddAudit(ctx context.Context, actor, event string, reqID int64, details string) error {
	now := time.Now().UTC()
	return d.Exec(ctx, `
INSERT INTO audit_events (ts, actor_email, event_type, request_id, details)
VALUES (?, ?, ?, ?, ?)`,
		now.Format(time.RFC3339Nano), strings.ToLower(actor), event, reqID, details,
	)
}

// Users
type User struct {
	ID       int64
	Username string
	Hash     string
	IsAdmin  bool
	Created  time.Time
}

func (d *DB) CreateUser(ctx context.Context, username, passwordHash string, isAdmin bool) (int64, error) {
	now := time.Now().UTC()
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO users (created_at, username, password_hash, is_admin)
VALUES (?, ?, ?, ?)`, now.Format(time.RFC3339Nano), strings.ToLower(username), passwordHash, boolToInt(isAdmin))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

func (d *DB) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT id, created_at, username, password_hash, is_admin FROM users WHERE username=?`, strings.ToLower(username))
	var u User
	var created string
	var isAdminInt int
	if err := row.Scan(&u.ID, &created, &u.Username, &u.Hash, &isAdminInt); err != nil {
		return nil, err
	}
	u.IsAdmin = isAdminInt == 1
	u.Created, _ = time.Parse(time.RFC3339Nano, created)
	return &u, nil
}

func (d *DB) CountAdmins(ctx context.Context) (int, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT COUNT(1) FROM users WHERE is_admin=1`)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (d *DB) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, created_at, username, password_hash, is_admin FROM users ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var created string
		var isAdminInt int
		if err := rows.Scan(&u.ID, &created, &u.Username, &u.Hash, &isAdminInt); err != nil {
			return nil, err
		}
		u.Created, _ = time.Parse(time.RFC3339Nano, created)
		u.IsAdmin = isAdminInt == 1
		out = append(out, u)
	}
	return out, nil
}

func (d *DB) DeleteUser(ctx context.Context, id int64) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	return err
}

func (d *DB) SetUserAdmin(ctx context.Context, id int64, isAdmin bool) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE users SET is_admin=? WHERE id=?`, boolToInt(isAdmin), id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
