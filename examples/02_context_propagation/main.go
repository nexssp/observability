package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
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
	os.Exit(run())
}

func run() int {
	provider, shutdown, err := obs.Auto()
	if err != nil {
		log.Printf("initialize observability: %v", err)
		return 1
	}
	defer func() {
		if shutdownErr := shutdown(context.Background()); shutdownErr != nil {
			log.Printf("shutdown observability: %v", shutdownErr)
		}
	}()

	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Printf("open sqlite: %v", err)
		return 1
	}
	defer rawDB.Close()
	if _, execErr := rawDB.ExecContext(context.Background(), "CREATE TABLE orders (id INTEGER, total REAL); INSERT INTO orders VALUES (9, 29.99);"); execErr != nil {
		log.Printf("seed sqlite: %v", execErr)
		return 1
	}

	tracedDB := dbtrace.Wrap(rawDB)
	tracedClient := httptrace.WrapClient(&http.Client{Timeout: 3 * time.Second})

	clients := &Clients{DB: tracedDB, Client: tracedClient}

	getInvoice := action.New("invoice.get", func(ctx context.Context, id int) (string, error) {
		var total float64
		if scanErr := clients.DB.QueryRowContext(ctx, "SELECT total FROM orders WHERE id = ?", id).Scan(&total); scanErr != nil {
			return "", scanErr
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/nexssp/kernel", http.NoBody)
		if reqErr != nil {
			return "", fmt.Errorf("build request: %w", reqErr)
		}
		resp, doErr := clients.Client.Do(req)
		if doErr != nil {
			return "", fmt.Errorf("outbound request failed: %w", doErr)
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
		return 1
	}
	fmt.Println("Result:", result)
	return 0
}
