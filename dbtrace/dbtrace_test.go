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

	_, err = tracedDB.ExecContext(ctx, "CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT);")
	if err != nil {
		t.Fatalf("ExecContext failed: %v", err)
	}

	tx, err := tracedDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO items (id, name) VALUES (?, ?);", "1", "Item A")
	if err != nil {
		t.Fatalf("Tx.ExecContext failed: %v", err)
	}

	if err = tx.Commit(); err != nil {
		t.Fatalf("Tx.Commit failed: %v", err)
	}

	row := tracedDB.QueryRowContext(ctx, "SELECT name FROM items WHERE id = ?;", "1")

	var name string
	if err = row.Scan(&name); err != nil || name != "Item A" {
		t.Fatalf("QueryRowContext Scan failed: %v (name=%s)", err, name)
	}

	rows, err := tracedDB.QueryContext(ctx, "SELECT id, name FROM items;")
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}

	if err = rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}

	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
}
