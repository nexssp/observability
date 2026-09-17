package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	"github.com/nexssp/kernel/action"
	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/actionhook"
	"github.com/nexssp/observability/dbtrace"
	"github.com/nexssp/observability/httptrace"
)

type Clients struct {
	DB     *dbtrace.DB
	Client *http.Client
}

func main() {
	provider, shutdown, _ := obs.Auto()
	defer func() { _ = shutdown(context.Background()) }()

	// Initialize SQL database and wrap with OpenTelemetry tracer
	rawDB, _ := sql.Open("sqlite", ":memory:")
	defer rawDB.Close()
	_, _ = rawDB.ExecContext(context.Background(), "CREATE TABLE orders (id INTEGER, total REAL); INSERT INTO orders VALUES (9, 29.99);")

	tracedDB := dbtrace.Wrap(rawDB)

	// Wrap HTTP Client for outbound request tracing
	tracedClient := httptrace.WrapClient(&http.Client{Timeout: 3 * time.Second})

	clients := &Clients{DB: tracedDB, Client: tracedClient}

	// Define action which implicitly propagates the tracing context
	getInvoice := action.New("invoice.get", func(ctx context.Context, id int) (string, error) {
		// The dbtrace driver automatically links this query to the action trace
		var total float64
		err := clients.DB.QueryRowContext(ctx, "SELECT total FROM orders WHERE id = ?", id).Scan(&total)
		if err != nil {
			return "", err
		}

		// The httptrace client injects W3C Trace headers so downstream APIs continue the trace
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/nexssp/kernel", http.NoBody)
		resp, err := clients.Client.Do(req)
		if err != nil {
			return "", fmt.Errorf("outbound request failed: %w", err)
		}
		defer resp.Body.Close()

		return fmt.Sprintf("Invoice total: %.2f (Outbound Status: %d)", total, resp.StatusCode), nil
	}).
		AnyHook(actionhook.New(provider)).
		Build()

	fmt.Println("Executing transaction...")
	result, err := getInvoice.Do(context.Background(), 9)
	if err != nil {
		fmt.Println("Execution error:", err)
		return
	}
	fmt.Println("Result:", result)
}
