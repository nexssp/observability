// Package dbtrace provides OpenTelemetry tracing wrappers for database/sql operations.
package dbtrace

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("nexss/dbtrace")

// DB wraps *sql.DB with OpenTelemetry tracing.
type DB struct {
	db *sql.DB
}

// Wrap creates a traced DB wrapper.
func Wrap(db *sql.DB) *DB { return &DB{db: db} }

// DB exposes underlying *sql.DB.
func (d *DB) DB() *sql.DB { return d.db }

// Close closes the underlying database connection.
func (d *DB) Close() error {
	if d.db == nil {
		return nil
	}

	return d.db.Close()
}

// QueryContext traces QueryContext calls. Caller owns the returned *sql.Rows.
func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	ctx, span := tracer.Start(ctx, "db.query",
		trace.WithAttributes(attribute.String("db.statement", query)),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()

	rows, err := d.db.QueryContext(ctx, query, args...) //nolint:sqlclosecheck // rows returned to caller for closing
	span.SetAttributes(attribute.Int64("db.duration_ms", time.Since(start).Milliseconds()))

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, fmt.Errorf("db query context: %w", err)
	}

	return rows, nil
}

// ExecContext traces ExecContext calls.
func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	ctx, span := tracer.Start(ctx, "db.exec",
		trace.WithAttributes(attribute.String("db.statement", query)),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()

	res, err := d.db.ExecContext(ctx, query, args...)
	span.SetAttributes(attribute.Int64("db.duration_ms", time.Since(start).Milliseconds()))

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, fmt.Errorf("db exec context: %w", err)
	}

	return res, nil
}

// QueryRowContext traces QueryRowContext calls.
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *Row {
	ctx, span := tracer.Start(ctx, "db.query_row",
		trace.WithAttributes(attribute.String("db.statement", query)),
		trace.WithSpanKind(trace.SpanKindClient),
	)

	row := d.db.QueryRowContext(ctx, query, args...)

	return &Row{Row: row, span: span}
}

// PrepareContext prepares a traced statement.
func (d *DB) PrepareContext(ctx context.Context, query string) (*Stmt, error) {
	s, err := d.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("db prepare context: %w", err)
	}

	return &Stmt{stmt: s, query: query}, nil
}

// BeginTx starts a traced transaction.
func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	ctx, span := tracer.Start(ctx, "db.begin",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	t, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, fmt.Errorf("db begin tx: %w", err)
	}

	return &Tx{tx: t, ctx: ctx}, nil
}

// Stmt represents a prepared traced database statement.
type Stmt struct {
	stmt  *sql.Stmt
	query string
}

// ExecContext executes a prepared statement with tracing.
func (s *Stmt) ExecContext(ctx context.Context, args ...any) (sql.Result, error) {
	ctx, span := tracer.Start(ctx, "db.stmt.exec",
		trace.WithAttributes(attribute.String("db.statement", s.query)),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	res, err := s.stmt.ExecContext(ctx, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, fmt.Errorf("stmt exec context: %w", err)
	}

	return res, nil
}

// QueryContext queries a prepared statement with tracing. Caller owns the returned *sql.Rows.
func (s *Stmt) QueryContext(ctx context.Context, args ...any) (*sql.Rows, error) {
	ctx, span := tracer.Start(ctx, "db.stmt.query",
		trace.WithAttributes(attribute.String("db.statement", s.query)),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	rows, err := s.stmt.QueryContext(ctx, args...) //nolint:sqlclosecheck // rows returned to caller for closing
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, fmt.Errorf("stmt query context: %w", err)
	}

	return rows, nil
}

// QueryRowContext queries a single row from a prepared statement with tracing.
func (s *Stmt) QueryRowContext(ctx context.Context, args ...any) *Row {
	ctx, span := tracer.Start(ctx, "db.stmt.query_row",
		trace.WithAttributes(attribute.String("db.statement", s.query)),
		trace.WithSpanKind(trace.SpanKindClient),
	)

	row := s.stmt.QueryRowContext(ctx, args...)

	return &Row{Row: row, span: span}
}

// Close closes the underlying prepared statement.
func (s *Stmt) Close() error {
	err := s.stmt.Close()
	if err != nil {
		return fmt.Errorf("stmt close: %w", err)
	}

	return nil
}

// Row wraps *sql.Row to terminate tracing span upon Scan.
type Row struct {
	*sql.Row
	span trace.Span
}

// Scan scans row fields and completes trace span.
func (r *Row) Scan(dest ...any) error {
	defer r.span.End()

	err := r.Row.Scan(dest...)
	if err != nil {
		r.span.RecordError(err)
		r.span.SetStatus(codes.Error, err.Error())

		return fmt.Errorf("row scan: %w", err)
	}

	r.span.SetStatus(codes.Ok, "")

	return nil
}

// Tx wraps *sql.Tx with tracing.
type Tx struct {
	tx  *sql.Tx
	ctx context.Context
}

// ExecContext executes statement within transaction.
func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	ctx, span := tracer.Start(ctx, "db.tx.exec",
		trace.WithAttributes(attribute.String("db.statement", query)),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	res, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, fmt.Errorf("tx exec context: %w", err)
	}

	return res, nil
}

// QueryContext executes query within transaction. Caller owns the returned *sql.Rows.
func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	ctx, span := tracer.Start(ctx, "db.tx.query",
		trace.WithAttributes(attribute.String("db.statement", query)),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	rows, err := t.tx.QueryContext(ctx, query, args...) //nolint:sqlclosecheck // rows returned to caller for closing
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return nil, fmt.Errorf("tx query context: %w", err)
	}

	return rows, nil
}

// QueryRowContext executes single-row query within transaction.
func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *Row {
	ctx, span := tracer.Start(ctx, "db.tx.query_row",
		trace.WithAttributes(attribute.String("db.statement", query)),
		trace.WithSpanKind(trace.SpanKindClient),
	)

	row := t.tx.QueryRowContext(ctx, query, args...)

	return &Row{Row: row, span: span}
}

// Commit commits transaction with tracing.
func (t *Tx) Commit() error {
	ctx := t.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	_, span := tracer.Start(ctx, "db.tx.commit", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	err := t.tx.Commit()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return fmt.Errorf("tx commit: %w", err)
	}

	return nil
}

// Rollback rolls back transaction with tracing.
func (t *Tx) Rollback() error {
	ctx := t.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	_, span := tracer.Start(ctx, "db.tx.rollback", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	err := t.tx.Rollback()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())

		return fmt.Errorf("tx rollback: %w", err)
	}

	return nil
}

// Tx exposes underlying *sql.Tx.
func (t *Tx) Tx() *sql.Tx { return t.tx }
