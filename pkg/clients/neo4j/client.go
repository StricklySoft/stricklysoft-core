// Package neo4j provides a Neo4j graph database client with OpenTelemetry
// tracing and structured error handling for services running on the
// StricklySoft Cloud Platform.
//
// # Connection Management
//
// The client uses the official Neo4j Go driver v5 (neo4j.DriverWithContext)
// which provides built-in connection pooling, automatic routing, and
// managed transactions. Transient connection failures are handled
// internally by the driver's retry logic.
//
// # Configuration
//
// Create a client using [NewClient] with a [Config]:
//
//	cfg := neo4j.DefaultConfig()
//	cfg.Password = neo4j.Secret("my-password")
//	client, err := neo4j.NewClient(ctx, *cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close(ctx)
//
// For testing, use [NewFromDriver] to inject a mock driver:
//
//	client := neo4j.NewFromDriver(mockDriver, &neo4j.Config{Database: "testdb"})
//
// # OpenTelemetry Tracing
//
// All database operations (ExecuteRead, ExecuteWrite, Run, Health)
// automatically create OpenTelemetry spans with standard database semantic
// attributes (db.system, db.name, db.statement). Cypher statements are
// truncated to 100 characters in spans to prevent sensitive data leakage.
//
// # Kubernetes Integration
//
// On the StricklySoft Cloud Platform, Neo4j is accessed via a Kubernetes
// Service at neo4j.databases.svc.cluster.local:7687. Credentials are
// injected by the External Secrets Operator from Vault.
package neo4j

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// tracerName is the OpenTelemetry instrumentation scope name for this package.
// It follows the Go module path convention for OTel instrumentation libraries.
const tracerName = "github.com/StricklySoft/stricklysoft-core/pkg/clients/neo4j"

// Driver defines the interface for Neo4j driver operations.
// This interface is satisfied by [neo4j.DriverWithContext] and by mock
// implementations for unit testing. It enables dependency injection via
// [NewFromDriver] for testing without a real database.
//
// All methods follow the neo4j-go-driver v5 API signatures exactly,
// ensuring that neo4j.DriverWithContext satisfies this interface.
type Driver interface {
	// NewSession creates a new session with the provided configuration.
	NewSession(ctx context.Context, config neo4j.SessionConfig) neo4j.SessionWithContext

	// VerifyConnectivity checks that the driver can connect to the database.
	VerifyConnectivity(ctx context.Context) error

	// Close releases all driver resources.
	Close(ctx context.Context) error
}

// Client is a Neo4j client with connection pooling, OpenTelemetry tracing,
// and structured error handling. It wraps a [Driver] (typically
// neo4j.DriverWithContext) and adds cross-cutting concerns (tracing, error
// classification) transparently to all database operations.
//
// A Client is safe for concurrent use by multiple goroutines. Create one
// Client per Neo4j deployment and share it across the application.
//
// Create a Client with [NewClient] for production use, or [NewFromDriver]
// for testing with mock drivers.
type Client struct {
	driver       Driver
	config       *Config
	tracer       trace.Tracer
	databaseName string
}

// NewClient creates a new Neo4j client with connection pooling. It validates
// the configuration, creates the driver, and verifies connectivity with the
// database.
//
// The caller must call [Client.Close] when the client is no longer needed
// to release driver resources.
//
// Error codes returned:
//   - [sserr.CodeValidation]: invalid configuration
//   - [sserr.CodeUnavailableDependency]: cannot connect to the database
//   - [sserr.CodeInternalDatabase]: unexpected driver creation failure
//
// Example:
//
//	cfg := neo4j.DefaultConfig()
//	cfg.Password = neo4j.Secret(os.Getenv("NEO4J_PASSWORD"))
//	client, err := neo4j.NewClient(ctx, *cfg)
//	if err != nil {
//	    return fmt.Errorf("connecting to neo4j: %w", err)
//	}
//	defer client.Close(ctx)
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, sserr.Wrap(err, sserr.CodeValidation,
			"neo4j: invalid configuration")
	}

	uri := cfg.ConnectionURI()
	auth := neo4j.BasicAuth(cfg.Username, cfg.Password.Value(), "")

	driver, err := neo4j.NewDriverWithContext(uri, auth, func(c *config.Config) {
		c.MaxConnectionPoolSize = cfg.MaxConnectionPoolSize
		c.MaxConnectionLifetime = cfg.MaxConnectionLifetime
		c.ConnectionAcquisitionTimeout = cfg.ConnectionAcquisitionTimeout
		c.SocketConnectTimeout = cfg.ConnectTimeout
		// Encryption is controlled by the URI scheme: neo4j+s:// or bolt+s://
		// enable TLS implicitly via the driver's config.Config.TlsConfig.
	})
	if err != nil {
		return nil, sserr.Wrap(err, sserr.CodeInternalDatabase,
			"neo4j: failed to create driver")
	}

	// Verify connectivity before returning the client.
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"neo4j: failed to connect to database")
	}

	// Extract database name for span attributes. If URI is set and Database
	// is empty, attempt to parse the database name from the URI path.
	dbName := cfg.Database
	if dbName == "" && cfg.URI != "" {
		if u, parseErr := url.Parse(cfg.URI); parseErr == nil {
			dbName = strings.TrimPrefix(u.Path, "/")
		}
	}

	return &Client{
		driver:       driver,
		config:       &cfg,
		tracer:       otel.Tracer(tracerName),
		databaseName: dbName,
	}, nil
}

// NewFromDriver creates a Client with a pre-existing [Driver]. This
// constructor is intended for testing with mock drivers and for advanced
// use cases where a custom driver implementation is needed.
//
// The cfg parameter is stored but not validated; pass nil for a zero-value
// config in tests. The databaseName is used for OpenTelemetry span attributes.
//
// Example (testing):
//
//	client := neo4j.NewFromDriver(mockDriver, &neo4j.Config{Database: "testdb"})
func NewFromDriver(driver Driver, cfg *Config) *Client {
	if cfg == nil {
		cfg = &Config{}
	}
	return &Client{
		driver:       driver,
		config:       cfg,
		tracer:       otel.Tracer(tracerName),
		databaseName: cfg.Database,
	}
}

// ExecuteRead executes a Cypher query in a managed read transaction and
// returns the collected records. The driver handles automatic retries for
// transient errors.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other database errors
//
// Example:
//
//	records, err := client.ExecuteRead(ctx,
//	    "MATCH (n:User {name: $name}) RETURN n.name AS name",
//	    map[string]any{"name": "Alice"})
//	if err != nil {
//	    return err
//	}
//	for _, record := range records {
//	    name, _ := record.Get("name")
//	    fmt.Println(name)
//	}
func (c *Client) ExecuteRead(ctx context.Context, cypher string, params map[string]any) ([]*neo4j.Record, error) {
	ctx, span := c.startSpan(ctx, "ExecuteRead", cypher)

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.databaseName})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		return res.Collect(ctx)
	})
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "neo4j: read transaction failed")
	}
	records, ok := result.([]*neo4j.Record)
	if !ok {
		return nil, sserr.Wrap(
			fmt.Errorf("unexpected result type %T from read transaction", result),
			sserr.CodeInternalDatabase,
			"neo4j: read transaction returned unexpected type",
		)
	}
	return records, nil
}

// ExecuteWrite executes a Cypher query in a managed write transaction and
// returns the collected records. The driver handles automatic retries for
// transient errors.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other database errors
//
// Example:
//
//	records, err := client.ExecuteWrite(ctx,
//	    "CREATE (n:User {name: $name}) RETURN n",
//	    map[string]any{"name": "Alice"})
//	if err != nil {
//	    return err
//	}
func (c *Client) ExecuteWrite(ctx context.Context, cypher string, params map[string]any) ([]*neo4j.Record, error) {
	ctx, span := c.startSpan(ctx, "ExecuteWrite", cypher)

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.databaseName})
	defer session.Close(ctx)

	result, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		return res.Collect(ctx)
	})
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "neo4j: write transaction failed")
	}
	records, ok := result.([]*neo4j.Record)
	if !ok {
		return nil, sserr.Wrap(
			fmt.Errorf("unexpected result type %T from write transaction", result),
			sserr.CodeInternalDatabase,
			"neo4j: write transaction returned unexpected type",
		)
	}
	return records, nil
}

// Run executes a Cypher query as an auto-commit transaction and returns
// the collected records. Unlike [ExecuteRead] and [ExecuteWrite], auto-commit
// transactions are not automatically retried by the driver.
//
// Use Run for simple queries where managed transaction semantics are not
// needed (e.g., RETURN 1 AS val, schema queries).
//
// Example:
//
//	records, err := client.Run(ctx, "RETURN 1 AS val", nil)
func (c *Client) Run(ctx context.Context, cypher string, params map[string]any) ([]*neo4j.Record, error) {
	ctx, span := c.startSpan(ctx, "Run", cypher)

	session := c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.databaseName})
	defer session.Close(ctx)

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		finishSpan(span, err)
		return nil, wrapError(err, "neo4j: auto-commit query failed")
	}

	records, err := result.Collect(ctx)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "neo4j: failed to collect results")
	}
	return records, nil
}

// Session returns a raw Neo4j session for advanced use cases that are not
// covered by the Client's managed transaction methods. The caller is
// responsible for closing the session when done.
//
// Example:
//
//	session := client.Session(ctx)
//	defer session.Close(ctx)
//	// Use session directly...
func (c *Client) Session(ctx context.Context) neo4j.SessionWithContext {
	return c.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: c.databaseName})
}

// Health verifies that the database connection is alive by calling
// VerifyConnectivity on the driver. It applies [DefaultHealthTimeout] if
// the provided context has no deadline.
//
// Returns nil if the database is reachable, or a [*sserr.Error] with code
// [sserr.CodeUnavailableDependency] if the connectivity check fails.
// This method is designed for use with health check endpoints and
// readiness probes.
//
// Example:
//
//	if err := client.Health(ctx); err != nil {
//	    log.Warn("neo4j health check failed", "error", err)
//	}
func (c *Client) Health(ctx context.Context) error {
	ctx, span := c.startSpan(ctx, "Health", "VERIFY CONNECTIVITY")

	// Apply a default timeout if the caller's context has no deadline.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultHealthTimeout)
		defer cancel()
	}

	err := c.driver.VerifyConnectivity(ctx)
	finishSpan(span, err)
	if err != nil {
		return sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"neo4j: health check failed")
	}
	return nil
}

// Close releases all driver resources. After Close is called, the client
// must not be used. Close is safe to call multiple times.
//
// Close waits for all active sessions to complete. Ensure all in-flight
// operations have completed or their contexts have been canceled before
// calling Close.
func (c *Client) Close(ctx context.Context) error {
	err := c.driver.Close(ctx)
	if err != nil {
		return sserr.Wrap(err, sserr.CodeInternalDatabase,
			"neo4j: failed to close driver")
	}
	return nil
}

// Driver returns the underlying [Driver] interface. This provides access
// to the raw driver for advanced use cases that are not covered by the
// Client's methods.
//
// The returned Driver should not be closed directly; use [Client.Close]
// instead.
func (c *Client) Driver() Driver {
	return c.driver
}

// startSpan creates a new OpenTelemetry span with standard database semantic
// attributes. It follows the OpenTelemetry semantic conventions for database
// client spans: https://opentelemetry.io/docs/specs/semconv/database/
func (c *Client) startSpan(ctx context.Context, operationName, cypher string) (context.Context, trace.Span) {
	ctx, span := c.tracer.Start(ctx, "neo4j."+operationName,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	span.SetAttributes(
		attribute.String("db.system", "neo4j"),
		attribute.String("db.name", c.databaseName),
		attribute.String("db.statement", truncateStatement(cypher)),
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
// appropriate error code. It distinguishes between timeout errors and general
// database errors to enable callers to make retry decisions via
// [sserr.IsTimeout] and [sserr.IsRetryable].
//
// [context.DeadlineExceeded] is classified as [sserr.CodeTimeoutDatabase]
// (retryable). [context.Canceled] is classified as [sserr.CodeInternalDatabase]
// (not retryable) because cancellation indicates the caller abandoned the
// operation, and retrying an intentionally canceled request is wasteful.
func wrapError(err error, message string) *sserr.Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return sserr.Wrap(err, sserr.CodeTimeoutDatabase, message)
	}
	return sserr.Wrap(err, sserr.CodeInternalDatabase, message)
}
