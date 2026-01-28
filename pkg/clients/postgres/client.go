// Package postgres provides a PostgreSQL client with connection pooling and
// OpenTelemetry tracing for services running on the StricklySoft Cloud Platform.
//
// # Connection Management
//
// The client uses pgxpool for connection pooling, automatically managing a
// pool of persistent connections. Connection retry for transient failures is
// handled internally by pgxpool — failed connections are replaced and the
// health check period keeps the pool healthy. Callers do not need to
// implement their own retry logic for connection-level errors.
//
// # Configuration
//
// Create a client using [NewClient] with a [Config]:
//
//	cfg := postgres.DefaultConfig()
//	cfg.Password = postgres.Secret("my-password")
//	client, err := postgres.NewClient(ctx, *cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
// For testing, use [NewFromPool] to inject a mock pool:
//
//	mock, _ := pgxmock.NewPool()
//	client := postgres.NewFromPool(mock, &postgres.Config{Database: "testdb"})
//
// # OpenTelemetry Tracing
//
// All database operations (Query, QueryRow, Exec, Begin, Health) automatically
// create OpenTelemetry spans with standard database semantic attributes
// (db.system, db.name, db.statement). SQL statements are truncated to 100
// characters in spans to prevent sensitive data leakage.
//
// # Kubernetes Integration
//
// On the StricklySoft Cloud Platform, PostgreSQL is accessed via a Kubernetes
// Service at postgres.databases.svc.cluster.local:5432. Credentials are
// injected by the External Secrets Operator from Vault. Linkerd provides
// mTLS at the network layer via opaque port annotation
// (config.linkerd.io/opaque-ports: "5432").
//
// # Cloud Portability
//
// The client works unchanged with cloud-managed databases (AWS RDS, Azure
// Database for PostgreSQL, GCP Cloud SQL) by configuring the appropriate
// [SSLMode] and [Config.SSLRootCert] for TLS certificate verification.
package postgres

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// tracerName is the OpenTelemetry instrumentation scope name for this package.
// It follows the Go module path convention for OTel instrumentation libraries.
const tracerName = "github.com/StricklySoft/stricklysoft-core/pkg/clients/postgres"

// Pool defines the interface for PostgreSQL connection pool operations.
// This interface is satisfied by [*pgxpool.Pool] and by mock implementations
// such as pgxmock for unit testing. It enables dependency injection via
// [NewFromPool] for testing without a real database.
//
// All methods follow the pgx v5 API signatures exactly, ensuring that
// [*pgxpool.Pool] satisfies this interface without adaptation.
type Pool interface {
	// Query executes a SQL query that returns rows.
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)

	// QueryRow executes a SQL query that returns at most one row.
	// Errors are deferred until the returned pgx.Row is scanned.
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row

	// Exec executes a SQL statement that does not return rows
	// (INSERT, UPDATE, DELETE, DDL).
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)

	// Begin starts a new database transaction.
	Begin(ctx context.Context) (pgx.Tx, error)

	// Ping verifies the connection to the database is alive.
	Ping(ctx context.Context) error

	// Close releases all pool resources. After Close is called,
	// the pool must not be used.
	Close()
}

// Compile-time interface compliance check. This ensures that *pgxpool.Pool
// satisfies the Pool interface at compile time rather than at runtime.
var _ Pool = (*pgxpool.Pool)(nil)

// Client is a PostgreSQL client with connection pooling, OpenTelemetry
// tracing, and structured error handling. It wraps a [Pool] (typically
// [*pgxpool.Pool]) and adds cross-cutting concerns (tracing, error
// classification) transparently to all database operations.
//
// A Client is safe for concurrent use by multiple goroutines. Create one
// Client per database and share it across the application.
//
// Create a Client with [NewClient] for production use, or [NewFromPool]
// for testing with mock pools.
type Client struct {
	pool         Pool
	config       *Config
	tracer       trace.Tracer
	databaseName string
}

// NewClient creates a new PostgreSQL client with connection pooling. It
// validates the configuration, establishes the connection pool, configures
// TLS if a custom CA certificate is provided, and verifies connectivity
// with a ping.
//
// The caller must call [Client.Close] when the client is no longer needed
// to release pool resources.
//
// Error codes returned:
//   - [sserr.CodeValidation]: invalid configuration
//   - [sserr.CodeInternalConfiguration]: TLS setup failure
//   - [sserr.CodeUnavailableDependency]: cannot connect to the database
//   - [sserr.CodeInternalDatabase]: unexpected pool creation failure
//
// Example:
//
//	cfg := postgres.DefaultConfig()
//	cfg.Password = postgres.Secret(os.Getenv("POSTGRES_PASSWORD"))
//	client, err := postgres.NewClient(ctx, *cfg)
//	if err != nil {
//	    return fmt.Errorf("connecting to database: %w", err)
//	}
//	defer client.Close()
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, sserr.Wrap(err, sserr.CodeValidation,
			"postgres: invalid configuration")
	}

	connStr := cfg.ConnectionString()

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, sserr.Wrap(err, sserr.CodeValidation,
			"postgres: failed to parse connection string")
	}

	// Apply pool settings from validated config.
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod

	// Apply custom TLS if a CA certificate is provided.
	tlsCfg, err := cfg.tlsConfig()
	if err != nil {
		return nil, sserr.Wrap(err, sserr.CodeInternalConfiguration,
			"postgres: failed to configure TLS")
	}
	if tlsCfg != nil {
		poolCfg.ConnConfig.TLSConfig = tlsCfg
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"postgres: failed to create connection pool")
	}

	// Verify connectivity before returning the client.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"postgres: failed to connect to database")
	}

	// Extract database name for span attributes.
	dbName := cfg.Database
	if cfg.URI != "" {
		if u, parseErr := url.Parse(cfg.URI); parseErr == nil {
			dbName = strings.TrimPrefix(u.Path, "/")
		}
	}

	return &Client{
		pool:         pool,
		config:       &cfg,
		tracer:       otel.Tracer(tracerName),
		databaseName: dbName,
	}, nil
}

// NewFromPool creates a Client with a pre-existing [Pool]. This constructor
// is intended for testing with mock pools (e.g., pgxmock) and for advanced
// use cases where a custom pool implementation is needed.
//
// The cfg parameter is stored but not validated; pass nil for a zero-value
// config in tests. The databaseName is used for OpenTelemetry span attributes.
//
// Example (testing):
//
//	mock, _ := pgxmock.NewPool()
//	client := postgres.NewFromPool(mock, nil)
//	defer mock.Close()
func NewFromPool(pool Pool, cfg *Config) *Client {
	if cfg == nil {
		cfg = &Config{}
	}
	return &Client{
		pool:         pool,
		config:       cfg,
		tracer:       otel.Tracer(tracerName),
		databaseName: cfg.Database,
	}
}

// Query executes a SQL query that returns rows, with OpenTelemetry tracing.
// The returned [pgx.Rows] must be closed by the caller when done.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other database errors
//
// Example:
//
//	rows, err := client.Query(ctx, "SELECT id, name FROM users WHERE active = $1", true)
//	if err != nil {
//	    return err
//	}
//	defer rows.Close()
//	for rows.Next() {
//	    var id int
//	    var name string
//	    if err := rows.Scan(&id, &name); err != nil {
//	        return err
//	    }
//	}
func (c *Client) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	ctx, span := c.startSpan(ctx, "Query", sql)

	rows, err := c.pool.Query(ctx, sql, args...)
	if err != nil {
		finishSpan(span, err)
		return nil, wrapError(err, "postgres: query failed")
	}
	// End span without error; row-level errors surface during iteration.
	finishSpan(span, nil)
	return rows, nil
}

// QueryRow executes a SQL query that returns at most one row, with
// OpenTelemetry tracing. The returned [pgx.Row] is never nil; errors are
// deferred until Scan() is called on the returned row.
//
// Note: Because pgx defers errors to Scan(), the tracing span for QueryRow
// cannot capture scan-time errors. The span covers only the query execution.
// To trace scan errors, check the error returned by row.Scan() and record
// it in the caller's span.
//
// Example:
//
//	var name string
//	err := client.QueryRow(ctx, "SELECT name FROM users WHERE id = $1", 42).Scan(&name)
//	if errors.Is(err, pgx.ErrNoRows) {
//	    // Handle no rows
//	}
func (c *Client) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	ctx, span := c.startSpan(ctx, "QueryRow", sql)
	defer span.End()

	return c.pool.QueryRow(ctx, sql, args...)
}

// Exec executes a SQL statement that does not return rows (INSERT, UPDATE,
// DELETE, DDL), with OpenTelemetry tracing.
//
// Returns the command tag (e.g., "INSERT 0 1") and a [*sserr.Error] on failure.
//
// Example:
//
//	tag, err := client.Exec(ctx, "DELETE FROM sessions WHERE expired_at < $1", time.Now())
//	if err != nil {
//	    return err
//	}
//	log.Printf("deleted %d expired sessions", tag.RowsAffected())
func (c *Client) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	ctx, span := c.startSpan(ctx, "Exec", sql)

	tag, err := c.pool.Exec(ctx, sql, args...)
	finishSpan(span, err)
	if err != nil {
		return tag, wrapError(err, "postgres: exec failed")
	}
	return tag, nil
}

// Begin starts a new database transaction with OpenTelemetry tracing. The
// returned [pgx.Tx] provides full transaction semantics including Commit,
// Rollback, and query methods.
//
// Callers must ensure the transaction is either committed or rolled back.
// Using defer tx.Rollback(ctx) immediately after Begin is the recommended
// pattern, as Rollback on an already-committed transaction is a no-op in pgx.
//
// Example:
//
//	tx, err := client.Begin(ctx)
//	if err != nil {
//	    return err
//	}
//	defer tx.Rollback(ctx)
//
//	_, err = tx.Exec(ctx, "INSERT INTO users (name) VALUES ($1)", "Alice")
//	if err != nil {
//	    return err
//	}
//	return tx.Commit(ctx)
func (c *Client) Begin(ctx context.Context) (pgx.Tx, error) {
	ctx, span := c.startSpan(ctx, "Begin", "BEGIN")

	tx, err := c.pool.Begin(ctx)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "postgres: begin transaction failed")
	}
	return tx, nil
}

// Health verifies that the database connection is alive by executing a ping.
// It applies [DefaultHealthTimeout] if the provided context has no deadline.
//
// Returns nil if the database is reachable, or a [*sserr.Error] with code
// [sserr.CodeUnavailableDependency] if the ping fails. This method is
// designed for use with health check endpoints and readiness probes.
//
// Example:
//
//	if err := client.Health(ctx); err != nil {
//	    log.Warn("database health check failed", "error", err)
//	}
func (c *Client) Health(ctx context.Context) error {
	ctx, span := c.startSpan(ctx, "Health", "SELECT 1")

	// Apply a default timeout if the caller's context has no deadline.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultHealthTimeout)
		defer cancel()
	}

	err := c.pool.Ping(ctx)
	finishSpan(span, err)
	if err != nil {
		return sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"postgres: health check failed")
	}
	return nil
}

// Close releases all connection pool resources. After Close is called,
// the client must not be used. Close is safe to call multiple times.
//
// Close waits for all acquired connections to be released before closing
// the pool. Ensure all in-flight queries have completed or their contexts
// have been canceled before calling Close.
func (c *Client) Close() {
	c.pool.Close()
}

// Pool returns the underlying [Pool] interface. This provides access to the
// raw connection pool for advanced use cases that are not covered by the
// Client's methods (e.g., CopyFrom, SendBatch, acquiring a raw connection).
//
// The returned Pool should not be closed directly; use [Client.Close] instead.
func (c *Client) Pool() Pool {
	return c.pool
}

// startSpan creates a new OpenTelemetry span with standard database semantic
// attributes. It follows the OpenTelemetry semantic conventions for database
// client spans: https://opentelemetry.io/docs/specs/semconv/database/
func (c *Client) startSpan(ctx context.Context, operationName, sql string) (context.Context, trace.Span) {
	ctx, span := c.tracer.Start(ctx, "postgres."+operationName,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	span.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", c.databaseName),
		attribute.String("db.statement", truncateSQL(sql)),
	)
	return ctx, span
}

// finishSpan records an error on the span (if any) and ends it. If err is
// nil, the span status is set to OK.
func finishSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

// wrapError converts a database error to a platform [*sserr.Error] with an
// appropriate error code. It distinguishes between timeout/cancellation
// errors and general database errors to enable callers to make retry
// decisions via [sserr.IsTimeout] and [sserr.IsRetryable].
func wrapError(err error, message string) *sserr.Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return sserr.Wrap(err, sserr.CodeTimeoutDatabase, message)
	}
	return sserr.Wrap(err, sserr.CodeInternalDatabase, message)
}
