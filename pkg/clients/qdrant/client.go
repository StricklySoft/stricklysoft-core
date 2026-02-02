// Package qdrant provides a Qdrant vector database client with OpenTelemetry
// tracing, structured error handling, and configuration management for
// services running on the StricklySoft Cloud Platform.
//
// # Connection Management
//
// The client wraps the official Qdrant Go gRPC client, adding cross-cutting
// concerns (tracing, error classification) transparently to all vector
// database operations.
//
// # Configuration
//
// Create a client using [NewClient] with a [Config]:
//
//	cfg := qdrant.DefaultConfig()
//	cfg.APIKey = qdrant.Secret("my-api-key")
//	client, err := qdrant.NewClient(ctx, *cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
// For testing, use [NewFromVectorDB] to inject a mock:
//
//	mock := &mockVectorDB{}
//	client := qdrant.NewFromVectorDB(mock, &qdrant.Config{})
//
// # OpenTelemetry Tracing
//
// All vector database operations (CreateCollection, Upsert, Search, etc.)
// automatically create OpenTelemetry spans with standard database semantic
// attributes (db.system, db.name, db.statement). Statements are truncated
// to 100 characters in spans to prevent sensitive data leakage.
//
// # Kubernetes Integration
//
// On the StricklySoft Cloud Platform, Qdrant is accessed via a Kubernetes
// Service at qdrant.databases.svc.cluster.local:6334. Credentials are
// injected by the External Secrets Operator from Vault. Linkerd provides
// mTLS at the network layer.
package qdrant

import (
	"context"
	"errors"
	"fmt"

	pb "github.com/qdrant/go-client/qdrant"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// tracerName is the OpenTelemetry instrumentation scope name for this package.
// It follows the Go module path convention for OTel instrumentation libraries.
const tracerName = "github.com/StricklySoft/stricklysoft-core/pkg/clients/qdrant"

// VectorDB defines the interface for Qdrant vector database operations.
// This interface is satisfied by [*pb.Client] and by mock implementations
// for unit testing. It enables dependency injection via [NewFromVectorDB]
// for testing without a real Qdrant instance.
//
// All methods follow the Qdrant Go client API signatures exactly, ensuring
// that [*pb.Client] satisfies this interface without adaptation.
type VectorDB interface {
	// CreateCollection creates a new collection with the specified configuration.
	CreateCollection(ctx context.Context, req *pb.CreateCollection) error

	// DeleteCollection deletes a collection by name.
	DeleteCollection(ctx context.Context, name string) error

	// ListCollections returns the names of all existing collections.
	ListCollections(ctx context.Context) ([]string, error)

	// GetCollectionInfo returns detailed information about a collection.
	GetCollectionInfo(ctx context.Context, name string) (*pb.CollectionInfo, error)

	// Upsert inserts or updates points in a collection.
	Upsert(ctx context.Context, req *pb.UpsertPoints) (*pb.UpdateResult, error)

	// Query searches for the nearest vectors in a collection.
	Query(ctx context.Context, req *pb.QueryPoints) ([]*pb.ScoredPoint, error)

	// Get retrieves points by their IDs.
	Get(ctx context.Context, req *pb.GetPoints) ([]*pb.RetrievedPoint, error)

	// Delete removes points from a collection.
	Delete(ctx context.Context, req *pb.DeletePoints) (*pb.UpdateResult, error)

	// Scroll iterates over points in a collection with pagination.
	Scroll(ctx context.Context, req *pb.ScrollPoints) ([]*pb.RetrievedPoint, error)

	// HealthCheck verifies the Qdrant server is alive.
	HealthCheck(ctx context.Context) (*pb.HealthCheckReply, error)

	// Close releases the gRPC connection resources.
	Close() error
}

// Compile-time interface compliance check. This ensures that *pb.Client
// satisfies the VectorDB interface at compile time rather than at runtime.
var _ VectorDB = (*pb.Client)(nil)

// Client is a Qdrant vector database client with OpenTelemetry tracing and
// structured error handling. It wraps a [VectorDB] (typically [*pb.Client])
// and adds cross-cutting concerns (tracing, error classification)
// transparently to all vector database operations.
//
// A Client is safe for concurrent use by multiple goroutines. Create one
// Client per Qdrant instance and share it across the application.
//
// Create a Client with [NewClient] for production use, or [NewFromVectorDB]
// for testing with mock implementations.
type Client struct {
	vectorDB VectorDB
	config   *Config
	tracer   trace.Tracer
}

// NewClient creates a new Qdrant client with gRPC connectivity. It validates
// the configuration, establishes the gRPC connection, and verifies
// connectivity with a health check.
//
// The caller must call [Client.Close] when the client is no longer needed
// to release gRPC connection resources.
//
// Error codes returned:
//   - [sserr.CodeValidation]: invalid configuration
//   - [sserr.CodeUnavailableDependency]: cannot connect to Qdrant
//   - [sserr.CodeInternalDatabase]: unexpected client creation failure
//
// Example:
//
//	cfg := qdrant.DefaultConfig()
//	cfg.APIKey = qdrant.Secret(os.Getenv("QDRANT_API_KEY"))
//	client, err := qdrant.NewClient(ctx, *cfg)
//	if err != nil {
//	    return fmt.Errorf("connecting to qdrant: %w", err)
//	}
//	defer client.Close()
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, sserr.Wrap(err, sserr.CodeValidation,
			"qdrant: invalid configuration")
	}

	qCfg := &pb.Config{
		Host: cfg.Host,
		Port: cfg.GRPCPort,
	}

	if cfg.APIKey.Value() != "" {
		qCfg.APIKey = cfg.APIKey.Value()
	}
	if cfg.UseTLS {
		qCfg.UseTLS = true
	}

	client, err := pb.NewClient(qCfg)
	if err != nil {
		return nil, sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"qdrant: failed to create gRPC client")
	}

	// Verify connectivity before returning the client.
	healthCtx := ctx
	if _, hasDeadline := healthCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		healthCtx, cancel = context.WithTimeout(healthCtx, cfg.HealthTimeout)
		defer cancel()
	}

	if _, err := client.HealthCheck(healthCtx); err != nil {
		_ = client.Close()
		return nil, sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"qdrant: failed to connect to server")
	}

	return &Client{
		vectorDB: client,
		config:   &cfg,
		tracer:   otel.Tracer(tracerName),
	}, nil
}

// NewFromVectorDB creates a Client with a pre-existing [VectorDB]. This
// constructor is intended for testing with mock implementations and for
// advanced use cases where a custom VectorDB implementation is needed.
//
// The cfg parameter is stored but not validated; pass nil for a zero-value
// config in tests.
//
// Example (testing):
//
//	mock := &mockVectorDB{}
//	client := qdrant.NewFromVectorDB(mock, nil)
func NewFromVectorDB(vectorDB VectorDB, cfg *Config) *Client {
	if cfg == nil {
		cfg = &Config{}
	}
	return &Client{
		vectorDB: vectorDB,
		config:   cfg,
		tracer:   otel.Tracer(tracerName),
	}
}

// CreateCollection creates a new vector collection with the specified
// configuration, with OpenTelemetry tracing.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other database errors
//
// Example:
//
//	err := client.CreateCollection(ctx, &pb.CreateCollection{
//	    CollectionName: "my-collection",
//	    VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
//	        Size:     128,
//	        Distance: pb.Distance_Cosine,
//	    }),
//	})
func (c *Client) CreateCollection(ctx context.Context, req *pb.CreateCollection) error {
	ctx, span := c.startSpan(ctx, "CreateCollection",
		fmt.Sprintf("CreateCollection %s", req.GetCollectionName()),
		req.GetCollectionName())

	err := c.vectorDB.CreateCollection(ctx, req)
	finishSpan(span, err)
	if err != nil {
		return wrapError(err, "qdrant: create collection failed")
	}
	return nil
}

// DeleteCollection deletes a vector collection by name, with OpenTelemetry
// tracing.
//
// Example:
//
//	err := client.DeleteCollection(ctx, "my-collection")
func (c *Client) DeleteCollection(ctx context.Context, name string) error {
	ctx, span := c.startSpan(ctx, "DeleteCollection",
		fmt.Sprintf("DeleteCollection %s", name), name)

	err := c.vectorDB.DeleteCollection(ctx, name)
	finishSpan(span, err)
	if err != nil {
		return wrapError(err, "qdrant: delete collection failed")
	}
	return nil
}

// ListCollections returns the names of all vector collections, with
// OpenTelemetry tracing.
//
// Example:
//
//	names, err := client.ListCollections(ctx)
//	for _, name := range names {
//	    fmt.Println(name)
//	}
func (c *Client) ListCollections(ctx context.Context) ([]string, error) {
	ctx, span := c.startSpan(ctx, "ListCollections", "ListCollections", "")

	collections, err := c.vectorDB.ListCollections(ctx)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "qdrant: list collections failed")
	}
	return collections, nil
}

// CollectionInfo returns detailed information about a vector collection,
// with OpenTelemetry tracing.
//
// Example:
//
//	info, err := client.CollectionInfo(ctx, "my-collection")
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("points count: %d\n", info.GetPointsCount())
func (c *Client) CollectionInfo(ctx context.Context, name string) (*pb.CollectionInfo, error) {
	ctx, span := c.startSpan(ctx, "CollectionInfo",
		fmt.Sprintf("GetCollectionInfo %s", name), name)

	info, err := c.vectorDB.GetCollectionInfo(ctx, name)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "qdrant: get collection info failed")
	}
	return info, nil
}

// Upsert inserts or updates points in a vector collection, with
// OpenTelemetry tracing.
//
// Example:
//
//	resp, err := client.Upsert(ctx, &pb.UpsertPoints{
//	    CollectionName: "my-collection",
//	    Points: []*pb.PointStruct{
//	        {
//	            Id:      pb.NewIDNum(1),
//	            Vectors: pb.NewVectors(0.1, 0.2, 0.3, 0.4),
//	            Payload: pb.NewValueMap(map[string]any{"name": "test"}),
//	        },
//	    },
//	})
func (c *Client) Upsert(ctx context.Context, req *pb.UpsertPoints) (*pb.UpdateResult, error) {
	ctx, span := c.startSpan(ctx, "Upsert",
		fmt.Sprintf("Upsert %s (%d points)", req.GetCollectionName(), len(req.GetPoints())),
		req.GetCollectionName())

	resp, err := c.vectorDB.Upsert(ctx, req)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "qdrant: upsert failed")
	}
	return resp, nil
}

// Search searches for the nearest vectors in a collection, with
// OpenTelemetry tracing. This method wraps the underlying Query operation
// on the VectorDB interface.
//
// Example:
//
//	results, err := client.Search(ctx, &pb.QueryPoints{
//	    CollectionName: "my-collection",
//	    Query:          pb.NewQuery(0.1, 0.2, 0.3, 0.4),
//	    Limit:          pb.PtrOf(uint64(10)),
//	})
func (c *Client) Search(ctx context.Context, req *pb.QueryPoints) ([]*pb.ScoredPoint, error) {
	ctx, span := c.startSpan(ctx, "Search",
		fmt.Sprintf("Query %s", req.GetCollectionName()),
		req.GetCollectionName())

	results, err := c.vectorDB.Query(ctx, req)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "qdrant: search failed")
	}
	return results, nil
}

// Get retrieves points by their IDs from a collection, with OpenTelemetry
// tracing.
//
// Example:
//
//	points, err := client.Get(ctx, &pb.GetPoints{
//	    CollectionName: "my-collection",
//	    Ids:            []*pb.PointId{pb.NewIDNum(1), pb.NewIDNum(2)},
//	})
func (c *Client) Get(ctx context.Context, req *pb.GetPoints) ([]*pb.RetrievedPoint, error) {
	ctx, span := c.startSpan(ctx, "Get",
		fmt.Sprintf("GetPoints %s", req.GetCollectionName()),
		req.GetCollectionName())

	points, err := c.vectorDB.Get(ctx, req)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "qdrant: get points failed")
	}
	return points, nil
}

// Delete removes points from a collection, with OpenTelemetry tracing.
//
// Example:
//
//	resp, err := client.Delete(ctx, &pb.DeletePoints{
//	    CollectionName: "my-collection",
//	    Points:         pb.NewPointsSelector(pb.NewIDNum(1)),
//	})
func (c *Client) Delete(ctx context.Context, req *pb.DeletePoints) (*pb.UpdateResult, error) {
	ctx, span := c.startSpan(ctx, "Delete",
		fmt.Sprintf("Delete %s", req.GetCollectionName()),
		req.GetCollectionName())

	resp, err := c.vectorDB.Delete(ctx, req)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "qdrant: delete points failed")
	}
	return resp, nil
}

// Scroll iterates over points in a collection with pagination, with
// OpenTelemetry tracing.
//
// Example:
//
//	points, err := client.Scroll(ctx, &pb.ScrollPoints{
//	    CollectionName: "my-collection",
//	    Limit:          pb.PtrOf(uint32(100)),
//	})
func (c *Client) Scroll(ctx context.Context, req *pb.ScrollPoints) ([]*pb.RetrievedPoint, error) {
	ctx, span := c.startSpan(ctx, "Scroll",
		fmt.Sprintf("Scroll %s", req.GetCollectionName()),
		req.GetCollectionName())

	points, err := c.vectorDB.Scroll(ctx, req)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "qdrant: scroll failed")
	}
	return points, nil
}

// Health verifies that the Qdrant server is alive by executing a health
// check. It applies [DefaultHealthTimeout] if the provided context has no
// deadline.
//
// Returns nil if the server is reachable, or a [*sserr.Error] with code
// [sserr.CodeUnavailableDependency] if the health check fails. This method
// is designed for use with health check endpoints and readiness probes.
//
// Example:
//
//	if err := client.Health(ctx); err != nil {
//	    log.Warn("qdrant health check failed", "error", err)
//	}
func (c *Client) Health(ctx context.Context) error {
	ctx, span := c.startSpan(ctx, "Health", "HealthCheck", "")

	// Apply a default timeout if the caller's context has no deadline.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		timeout := DefaultHealthTimeout
		if c.config != nil && c.config.HealthTimeout > 0 {
			timeout = c.config.HealthTimeout
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	_, err := c.vectorDB.HealthCheck(ctx)
	finishSpan(span, err)
	if err != nil {
		return sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"qdrant: health check failed")
	}
	return nil
}

// Close releases the gRPC connection resources. After Close is called,
// the client must not be used. Close is safe to call multiple times.
//
// Ensure all in-flight operations have completed or their contexts have
// been canceled before calling Close.
func (c *Client) Close() error {
	return c.vectorDB.Close()
}

// VectorDB returns the underlying [VectorDB] interface. This provides access
// to the raw Qdrant client for advanced use cases not covered by the Client's
// methods.
//
// The returned VectorDB should not be closed directly; use [Client.Close]
// instead.
func (c *Client) VectorDB() VectorDB {
	return c.vectorDB
}

// startSpan creates a new OpenTelemetry span with standard database semantic
// attributes. It follows the OpenTelemetry semantic conventions for database
// client spans: https://opentelemetry.io/docs/specs/semconv/database/
func (c *Client) startSpan(ctx context.Context, operationName, statement, collectionName string) (context.Context, trace.Span) {
	ctx, span := c.tracer.Start(ctx, "qdrant."+operationName,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	attrs := []attribute.KeyValue{
		attribute.String("db.system", "qdrant"),
		attribute.String("db.statement", truncateStatement(statement)),
	}
	if collectionName != "" {
		attrs = append(attrs, attribute.String("db.name", collectionName))
	}
	span.SetAttributes(attrs...)
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
	// Check both standard context.DeadlineExceeded and gRPC's
	// DeadlineExceeded status code, because the gRPC transport layer
	// may wrap the deadline error in a status.Status rather than
	// propagating the raw context error.
	if errors.Is(err, context.DeadlineExceeded) {
		return sserr.Wrap(err, sserr.CodeTimeoutDatabase, message)
	}
	if st, ok := grpcstatus.FromError(err); ok && st.Code() == grpccodes.DeadlineExceeded {
		return sserr.Wrap(err, sserr.CodeTimeoutDatabase, message)
	}
	return sserr.Wrap(err, sserr.CodeInternalDatabase, message)
}
