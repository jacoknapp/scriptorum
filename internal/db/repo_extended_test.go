package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func openMigratedDB(t *testing.T) *DB {
	t.Helper()

	db, err := Open(filepath.Join(t.TempDir(), "scriptorum.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestRequestAndUserRepositoryFlows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openMigratedDB(t)

	reqID, err := db.CreateRequest(ctx, &Request{
		RequesterEmail:   "Reader@Example.com",
		Title:            "The Long Way",
		Authors:          []string{"Becky Chambers"},
		ISBN10:           "1234567890",
		ISBN13:           "9781234567897",
		Format:           "ebook",
		Status:           "pending",
		StatusReason:     "queued",
		ExternalStatus:   "wanted",
		MatchedReadarrID: 41,
		CoverURL:         " https://covers.example/initial.jpg ",
		ReadarrReq:       json.RawMessage(`{"request":true}`),
		ReadarrResp:      json.RawMessage(`{"response":true}`),
	})
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	got, err := db.GetRequest(ctx, reqID)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}
	if got.RequesterEmail != "reader@example.com" {
		t.Fatalf("requester email = %q", got.RequesterEmail)
	}
	if got.MatchedReadarrID != 41 || got.ExternalStatus != "wanted" {
		t.Fatalf("unexpected readarr metadata: %+v", got)
	}
	if string(got.ReadarrReq) != `{"request":true}` || string(got.ReadarrResp) != `{"response":true}` {
		t.Fatalf("unexpected request payloads: %+v", got)
	}

	if err := db.UpdateRequestStatus(ctx, reqID, "processing", "sent", "ADMIN@EXAMPLE.COM", nil, []byte(`{"queued":true}`)); err != nil {
		t.Fatalf("UpdateRequestStatus: %v", err)
	}
	if err := db.ApproveRequest(ctx, reqID, "ADMIN@EXAMPLE.COM"); err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	if err := db.UpdateRequestCover(ctx, reqID, " https://covers.example/final.jpg "); err != nil {
		t.Fatalf("UpdateRequestCover: %v", err)
	}
	if err := db.UpdateRequestExternalStatus(ctx, reqID, "available", 99, "matched in catalog"); err != nil {
		t.Fatalf("UpdateRequestExternalStatus: %v", err)
	}

	got, err = db.GetRequest(ctx, reqID)
	if err != nil {
		t.Fatalf("GetRequest after updates: %v", err)
	}
	if got.Status != "approved" || got.ApproverEmail != "admin@example.com" || got.ApprovedAt == nil {
		t.Fatalf("approval fields not persisted: %+v", got)
	}
	if got.CoverURL != "https://covers.example/final.jpg" {
		t.Fatalf("cover URL = %q", got.CoverURL)
	}
	if got.ExternalStatus != "available" || got.MatchedReadarrID != 99 || got.StatusReason != "matched in catalog" {
		t.Fatalf("unexpected external status update: %+v", got)
	}
	if string(got.ReadarrResp) != `{"queued":true}` {
		t.Fatalf("readarr response = %s", string(got.ReadarrResp))
	}

	page, err := db.ListRequestsPage(ctx, "READER@EXAMPLE.COM", 0)
	if err != nil {
		t.Fatalf("ListRequestsPage: %v", err)
	}
	if len(page) != 1 || !page[0].HasReadarrReq {
		t.Fatalf("unexpected paged results: %+v", page)
	}

	listed, err := db.ListRequests(ctx, "", 0)
	if err != nil {
		t.Fatalf("ListRequests: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != reqID {
		t.Fatalf("unexpected listed requests: %+v", listed)
	}

	if err := db.DeclineRequest(ctx, reqID, "OTHERADMIN@example.com"); err != nil {
		t.Fatalf("DeclineRequest: %v", err)
	}
	got, err = db.GetRequest(ctx, reqID)
	if err != nil {
		t.Fatalf("GetRequest after decline: %v", err)
	}
	if got.Status != "declined" || got.ApproverEmail != "otheradmin@example.com" {
		t.Fatalf("decline fields not persisted: %+v", got)
	}

	if _, err := db.CreateRequest(ctx, &Request{
		RequesterEmail: "other@example.com",
		Title:          "Second",
		Format:         "audiobook",
		Status:         "pending",
	}); err != nil {
		t.Fatalf("CreateRequest second: %v", err)
	}

	if err := db.DeleteRequest(ctx, reqID); err != nil {
		t.Fatalf("DeleteRequest: %v", err)
	}
	if _, err := db.GetRequest(ctx, reqID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetRequest after delete error = %v", err)
	}

	if err := db.DeleteAllRequests(ctx); err != nil {
		t.Fatalf("DeleteAllRequests: %v", err)
	}
	listed, err = db.ListRequests(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListRequests after delete all: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("expected empty requests list, got %d", len(listed))
	}

	adminID, err := db.CreateUser(ctx, "AdminUser", "hash-1", true, false)
	if err != nil {
		t.Fatalf("CreateUser admin: %v", err)
	}
	memberID, err := db.CreateUser(ctx, "MemberUser", "hash-2", false, true)
	if err != nil {
		t.Fatalf("CreateUser member: %v", err)
	}

	admin, err := db.GetUserByUsername(ctx, "ADMINUSER")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if !admin.IsAdmin || admin.AutoApprove || admin.Username != "adminuser" {
		t.Fatalf("unexpected admin user: %+v", admin)
	}

	adminCount, err := db.CountAdmins(ctx)
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if adminCount != 1 {
		t.Fatalf("admin count = %d", adminCount)
	}

	users, err := db.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	if err := db.SetUserAdmin(ctx, memberID, true); err != nil {
		t.Fatalf("SetUserAdmin: %v", err)
	}
	if err := db.SetUserAutoApprove(ctx, adminID, true); err != nil {
		t.Fatalf("SetUserAutoApprove: %v", err)
	}
	if err := db.UpdateUserPassword(ctx, adminID, "hash-3"); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}

	admin, err = db.GetUserByUsername(ctx, "adminuser")
	if err != nil {
		t.Fatalf("GetUserByUsername after updates: %v", err)
	}
	if !admin.AutoApprove || admin.Hash != "hash-3" {
		t.Fatalf("updated admin fields not persisted: %+v", admin)
	}

	if err := db.DeleteUser(ctx, memberID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := db.GetUserByUsername(ctx, "memberuser"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetUserByUsername after delete error = %v", err)
	}

	if boolToInt(true) != 1 || boolToInt(false) != 0 {
		t.Fatalf("boolToInt returned unexpected values")
	}
	if bytesOrNil(nil) != nil || bytesOrNil([]byte{}) != nil {
		t.Fatalf("bytesOrNil should return nil for empty input")
	}
	if got := bytesOrNil([]byte("payload")); got != "payload" {
		t.Fatalf("bytesOrNil = %v", got)
	}
}

func TestReadarrCatalogRepositoryFlows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openMigratedDB(t)

	books := []ReadarrBook{
		{
			SourceKind:       " Ebook ",
			ReadarrID:        101,
			Title:            "Dune",
			AuthorName:       "Frank Herbert",
			ISBN10:           "0441172717",
			ISBN13:           "9780441172719",
			ForeignBookID:    "book-1",
			ForeignEditionID: "edition-1",
			Monitored:        true,
			ReadarrData:      json.RawMessage(`{"id":101}`),
		},
		{
			SourceKind:       "ebook",
			ReadarrID:        102,
			Title:            "Dune",
			AuthorName:       "Frank Herbert",
			ASIN:             "B00B7NPRY8",
			ForeignBookID:    "book-2",
			ForeignEditionID: "edition-2",
			Grabbed:          true,
		},
		{
			SourceKind:       "ebook",
			ReadarrID:        103,
			Title:            "Dune",
			AuthorName:       "Frank Herbert",
			ISBN13:           "9780441013593",
			ForeignBookID:    "book-3",
			ForeignEditionID: "edition-3",
			BookFileCount:    1,
		},
	}
	if err := db.ReplaceReadarrBooks(ctx, "ebook", books); err != nil {
		t.Fatalf("ReplaceReadarrBooks: %v", err)
	}

	count, err := db.CountReadarrBooks(ctx, " EBOOK ")
	if err != nil {
		t.Fatalf("CountReadarrBooks: %v", err)
	}
	if count != len(books) {
		t.Fatalf("count = %d", count)
	}

	byEdition, err := db.FindReadarrBookMatch(ctx, ReadarrMatchQuery{
		SourceKind:       "ebook",
		ForeignEditionID: "edition-1",
	})
	if err != nil || byEdition.ReadarrID != 101 {
		t.Fatalf("FindReadarrBookMatch by edition: book=%+v err=%v", byEdition, err)
	}

	byBook, err := db.FindReadarrBookMatch(ctx, ReadarrMatchQuery{
		SourceKind:    "ebook",
		ForeignBookID: "book-2",
	})
	if err != nil || byBook.ReadarrID != 102 {
		t.Fatalf("FindReadarrBookMatch by book: book=%+v err=%v", byBook, err)
	}

	byISBN13, err := db.FindReadarrBookMatch(ctx, ReadarrMatchQuery{
		SourceKind: "ebook",
		ISBN13:     "9780441013593",
	})
	if err != nil || byISBN13.ReadarrID != 103 {
		t.Fatalf("FindReadarrBookMatch by ISBN13: book=%+v err=%v", byISBN13, err)
	}

	byISBN10, err := db.FindReadarrBookMatch(ctx, ReadarrMatchQuery{
		SourceKind: "ebook",
		ISBN10:     "0441172717",
	})
	if err != nil || byISBN10.ReadarrID != 101 {
		t.Fatalf("FindReadarrBookMatch by ISBN10: book=%+v err=%v", byISBN10, err)
	}

	byASIN, err := db.FindReadarrBookMatch(ctx, ReadarrMatchQuery{
		SourceKind: "ebook",
		ASIN:       "B00B7NPRY8",
	})
	if err != nil || byASIN.ReadarrID != 102 {
		t.Fatalf("FindReadarrBookMatch by ASIN: book=%+v err=%v", byASIN, err)
	}

	byTitle, err := db.FindReadarrBookMatch(ctx, ReadarrMatchQuery{
		SourceKind: "ebook",
		Title:      " dUnE ",
		Authors:    []string{" frank herbert "},
	})
	if err != nil || byTitle.ReadarrID != 103 {
		t.Fatalf("FindReadarrBookMatch by title: book=%+v err=%v", byTitle, err)
	}

	ids, err := db.ListReadarrBooksByIDs(ctx, "ebook", []int64{103, 0, 101, 103, -1})
	if err != nil {
		t.Fatalf("ListReadarrBooksByIDs: %v", err)
	}
	if len(ids) != 2 || ids[101].ReadarrID != 101 || ids[103].ReadarrID != 103 {
		t.Fatalf("unexpected ID map: %+v", ids)
	}

	empty, err := db.ListReadarrBooksByIDs(ctx, "ebook", nil)
	if err != nil {
		t.Fatalf("ListReadarrBooksByIDs empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty ID map, got %+v", empty)
	}

	if _, err := db.FindReadarrBookMatch(ctx, ReadarrMatchQuery{SourceKind: "ebook", Title: "missing"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}

	if got := (ReadarrBook{}).Availability(); got != "" {
		t.Fatalf("Availability empty = %q", got)
	}
	if got := (ReadarrBook{Monitored: true}).Availability(); got != "monitored" {
		t.Fatalf("Availability monitored = %q", got)
	}
	if got := (ReadarrBook{Grabbed: true}).Availability(); got != "grabbed" {
		t.Fatalf("Availability grabbed = %q", got)
	}
	if got := (ReadarrBook{BookFileCount: 1, Grabbed: true, Monitored: true}).Availability(); got != "available" {
		t.Fatalf("Availability available = %q", got)
	}
}
