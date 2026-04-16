package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type ReadarrBook struct {
	ID               int64           `json:"id"`
	SourceKind       string          `json:"sourceKind"`
	ReadarrID        int64           `json:"readarrId"`
	Title            string          `json:"title"`
	AuthorName       string          `json:"authorName"`
	ISBN10           string          `json:"isbn10"`
	ISBN13           string          `json:"isbn13"`
	ASIN             string          `json:"asin"`
	ForeignBookID    string          `json:"foreignBookId"`
	ForeignEditionID string          `json:"foreignEditionId"`
	Monitored        bool            `json:"monitored"`
	Grabbed          bool            `json:"grabbed"`
	BookFileCount    int             `json:"bookFileCount"`
	ReadarrData      json.RawMessage `json:"readarrData,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

type ReadarrMatchQuery struct {
	SourceKind       string
	Title            string
	Authors          []string
	ISBN10           string
	ISBN13           string
	ASIN             string
	ForeignBookID    string
	ForeignEditionID string
}

func (b ReadarrBook) Availability() string {
	switch {
	case b.BookFileCount > 0:
		return "available"
	case b.Grabbed:
		return "grabbed"
	case b.Monitored:
		return "monitored"
	default:
		return "present"
	}
}

func (d *DB) ReplaceReadarrBooks(ctx context.Context, sourceKind string, books []ReadarrBook) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM readarr_books WHERE source_kind=?`, strings.ToLower(strings.TrimSpace(sourceKind))); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO readarr_books
(source_kind, readarr_id, title, author_name, isbn13, isbn10, asin, foreign_book_id, foreign_edition_id, monitored, grabbed, book_file_count, readarr_data, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, book := range books {
		_, err := stmt.ExecContext(
			ctx,
			strings.ToLower(strings.TrimSpace(book.SourceKind)),
			book.ReadarrID,
			book.Title,
			strings.ToLower(strings.TrimSpace(book.AuthorName)),
			book.ISBN13,
			book.ISBN10,
			book.ASIN,
			book.ForeignBookID,
			book.ForeignEditionID,
			boolToInt(book.Monitored),
			boolToInt(book.Grabbed),
			book.BookFileCount,
			bytesOrNil(book.ReadarrData),
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (d *DB) CountReadarrBooks(ctx context.Context, sourceKind string) (int, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT COUNT(1) FROM readarr_books WHERE source_kind=?`, strings.ToLower(strings.TrimSpace(sourceKind)))
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (d *DB) FindReadarrBookMatch(ctx context.Context, q ReadarrMatchQuery) (*ReadarrBook, error) {
	kind := strings.ToLower(strings.TrimSpace(q.SourceKind))
	lookups := []struct {
		field string
		value string
	}{
		{"foreign_edition_id", strings.TrimSpace(q.ForeignEditionID)},
		{"foreign_book_id", strings.TrimSpace(q.ForeignBookID)},
		{"isbn13", strings.TrimSpace(q.ISBN13)},
		{"isbn10", strings.TrimSpace(q.ISBN10)},
		{"asin", strings.TrimSpace(q.ASIN)},
	}
	for _, lookup := range lookups {
		if lookup.value == "" {
			continue
		}
		row := d.sql.QueryRowContext(ctx, `
SELECT id, source_kind, readarr_id, title, author_name, isbn10, isbn13, asin, foreign_book_id, foreign_edition_id, monitored, grabbed, book_file_count, readarr_data, created_at, updated_at
FROM readarr_books
WHERE source_kind=? AND `+lookup.field+`=?
ORDER BY book_file_count DESC, monitored DESC, grabbed DESC, readarr_id DESC
LIMIT 1`, kind, lookup.value)
		if book, err := scanReadarrBook(row); err == nil {
			return book, nil
		} else if err != sql.ErrNoRows {
			return nil, err
		}
	}

	title := strings.ToLower(strings.TrimSpace(q.Title))
	author := ""
	if len(q.Authors) > 0 {
		author = strings.ToLower(strings.TrimSpace(q.Authors[0]))
	}
	if title != "" {
		row := d.sql.QueryRowContext(ctx, `
SELECT id, source_kind, readarr_id, title, author_name, isbn10, isbn13, asin, foreign_book_id, foreign_edition_id, monitored, grabbed, book_file_count, readarr_data, created_at, updated_at
FROM readarr_books
WHERE source_kind=? AND lower(title)=? AND (?='' OR author_name=?)
ORDER BY book_file_count DESC, monitored DESC, grabbed DESC, readarr_id DESC
LIMIT 1`, kind, title, author, author)
		if book, err := scanReadarrBook(row); err == nil {
			return book, nil
		} else if err != sql.ErrNoRows {
			return nil, err
		}
	}

	return nil, sql.ErrNoRows
}

func (d *DB) UpdateRequestExternalStatus(ctx context.Context, id int64, externalStatus string, readarrID int64, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.sql.ExecContext(ctx, `
UPDATE requests
SET external_status=?, matched_readarr_id=?, status_reason=CASE WHEN ?<>'' THEN ? ELSE status_reason END, updated_at=?
WHERE id=?`,
		strings.TrimSpace(externalStatus),
		readarrID,
		strings.TrimSpace(reason),
		strings.TrimSpace(reason),
		now,
		id,
	)
	return err
}

func scanReadarrBook(scanner interface{ Scan(dest ...any) error }) (*ReadarrBook, error) {
	var book ReadarrBook
	var title string
	var authorName string
	var monitored int
	var grabbed int
	var readarrData sql.NullString
	var created string
	var updated string
	if err := scanner.Scan(
		&book.ID,
		&book.SourceKind,
		&book.ReadarrID,
		&title,
		&authorName,
		&book.ISBN10,
		&book.ISBN13,
		&book.ASIN,
		&book.ForeignBookID,
		&book.ForeignEditionID,
		&monitored,
		&grabbed,
		&book.BookFileCount,
		&readarrData,
		&created,
		&updated,
	); err != nil {
		return nil, err
	}
	book.Title = title
	book.AuthorName = authorName
	book.Monitored = monitored == 1
	book.Grabbed = grabbed == 1
	if readarrData.Valid {
		book.ReadarrData = json.RawMessage(readarrData.String)
	}
	book.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	book.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &book, nil
}
