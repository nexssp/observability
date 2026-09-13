package dbtrace_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/nexssp/observability/dbtrace"
)

func TestDBTrace_ExecQueryTx(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer sqlDB.Close()

	tracedDB := dbtrace.Wrap(sqlDB)
	ctx := context.Background()

	// 1. ExecContext
	_, err = tracedDB.ExecContext(ctx, "CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT);")
	if err != nil {
		t.Fatalf("ExecContext failed: %v", err)
	}

	// 2. BeginTx & Exec
	tx, err := tracedDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO items (id, name) VALUES (?, ?);", "1", "Item A")
	if err != nil {
		t.Fatalf("Tx.ExecContext failed: %v", err)
	}
	err = tx.Commit()
	if err != nil {
		t.Fatalf("Tx.Commit failed: %v", err)
	}

	// 3. QueryRowContext & Scan
	row := tracedDB.QueryRowContext(ctx, "SELECT name FROM items WHERE id = ?;", "1")
	var name string
	err = row.Scan(&name)
	if err != nil || name != "Item A" {
		t.Fatalf("QueryRowContext Scan failed: %v (name=%s)", err, name)
	}

	// 4. QueryContext
	rows, err := tracedDB.QueryContext(ctx, "SELECT id, name FROM items;")
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
}
