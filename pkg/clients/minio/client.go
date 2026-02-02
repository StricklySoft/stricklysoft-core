// Package minio provides a MinIO S3-compatible object storage client with
// OpenTelemetry tracing, structured error handling, and configuration
// management for services running on the StricklySoft Cloud Platform.
//
// # Connection Management
//
// The MinIO client uses stateless HTTP connections. Unlike database clients,
// there is no connection pool to manage. The client is safe for concurrent
// use by multiple goroutines.
//
// # Configuration
//
// Create a client using [NewClient] with a [Config]:
//
//	cfg := minio.DefaultConfig()
//	cfg.AccessKey = "my-access-key"
//	cfg.SecretKey = minio.Secret("my-secret-key")
//	client, err := minio.NewClient(ctx, *cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
// For testing, use [NewFromStore] to inject a mock store:
//
//	mock := &mockObjectStore{}
//	client := minio.NewFromStore(mock, &minio.Config{})
//
// # OpenTelemetry Tracing
//
// All object storage operations (PutObject, GetObject, RemoveObject, etc.)
// automatically create OpenTelemetry spans with standard database semantic
// attributes (db.system, db.name, db.statement). Operation descriptions are
// truncated to 100 characters in spans to prevent sensitive data leakage.
//
// # Kubernetes Integration
//
// On the StricklySoft Cloud Platform, MinIO is accessed via a Kubernetes
// Service at minio.databases.svc.cluster.local:9000. Credentials are
// injected by the External Secrets Operator from Vault. Linkerd provides
// mTLS at the network layer.
package minio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// tracerName is the OpenTelemetry instrumentation scope name for this package.
// It follows the Go module path convention for OTel instrumentation libraries.
const tracerName = "github.com/StricklySoft/stricklysoft-core/pkg/clients/minio"

// ObjectStore defines the interface for MinIO object storage operations.
// This interface is satisfied by [*minio.Client] and by mock implementations
// for unit testing. It enables dependency injection via [NewFromStore] for
// testing without a real MinIO server.
//
// All methods follow the minio-go v7 API signatures exactly, ensuring that
// [*minio.Client] satisfies this interface without adaptation.
type ObjectStore interface {
	// PutObject uploads an object to a bucket.
	PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error)

	// GetObject retrieves an object from a bucket. The returned *minio.Object
	// implements io.ReadCloser and must be closed by the caller.
	GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) (*minio.Object, error)

	// RemoveObject deletes an object from a bucket.
	RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error

	// StatObject retrieves metadata about an object without downloading it.
	StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error)

	// ListObjects returns a channel of objects in a bucket matching the
	// provided options (prefix, recursive, etc.).
	ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo

	// BucketExists checks whether a bucket exists on the server.
	BucketExists(ctx context.Context, bucketName string) (bool, error)

	// MakeBucket creates a new bucket with the given name and options.
	MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error

	// RemoveBucket deletes an empty bucket.
	RemoveBucket(ctx context.Context, bucketName string) error

	// PresignedGetObject generates a presigned URL for downloading an object.
	PresignedGetObject(ctx context.Context, bucketName, objectName string, expires time.Duration, reqParams url.Values) (*url.URL, error)

	// PresignedPutObject generates a presigned URL for uploading an object.
	PresignedPutObject(ctx context.Context, bucketName, objectName string, expires time.Duration) (*url.URL, error)
}

// Compile-time interface compliance check. This ensures that *minio.Client
// satisfies the ObjectStore interface at compile time rather than at runtime.
var _ ObjectStore = (*minio.Client)(nil)

// Client is a MinIO object storage client with OpenTelemetry tracing and
// structured error handling. It wraps an [ObjectStore] (typically
// [*minio.Client]) and adds cross-cutting concerns (tracing, error
// classification) transparently to all storage operations.
//
// A Client is safe for concurrent use by multiple goroutines. Create one
// Client per MinIO endpoint and share it across the application.
//
// Create a Client with [NewClient] for production use, or [NewFromStore]
// for testing with mock stores.
type Client struct {
	store  ObjectStore
	config *Config
	tracer trace.Tracer
}

// NewClient creates a new MinIO client. It validates the configuration,
// creates the underlying minio.Client, and verifies connectivity by
// calling BucketExists on a health-check probe bucket.
//
// The caller should call [Client.Close] when the client is no longer needed
// (though Close is a no-op for MinIO since the client is stateless HTTP).
//
// Error codes returned:
//   - [sserr.CodeValidation]: invalid configuration
//   - [sserr.CodeUnavailableDependency]: cannot connect to MinIO
//   - [sserr.CodeInternalDatabase]: unexpected client creation failure
//
// Example:
//
//	cfg := minio.DefaultConfig()
//	cfg.AccessKey = os.Getenv("MINIO_ACCESS_KEY")
//	cfg.SecretKey = minio.Secret(os.Getenv("MINIO_SECRET_KEY"))
//	client, err := minio.NewClient(ctx, *cfg)
//	if err != nil {
//	    return fmt.Errorf("connecting to minio: %w", err)
//	}
//	defer client.Close()
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, sserr.Wrap(err, sserr.CodeValidation,
			"minio: invalid configuration")
	}

	minioClient, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey.Value(), ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, sserr.Wrap(err, sserr.CodeInternalDatabase,
			"minio: failed to create client")
	}

	// Verify connectivity by probing with BucketExists. The bucket does
	// not need to exist; a successful API call (even returning false)
	// confirms that the MinIO server is reachable and credentials are valid.
	healthBucket := cfg.HealthBucket
	if healthBucket == "" {
		healthBucket = "health-check-probe"
	}
	if _, err := minioClient.BucketExists(ctx, healthBucket); err != nil {
		return nil, sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"minio: failed to connect to server")
	}

	return &Client{
		store:  minioClient,
		config: &cfg,
		tracer: otel.Tracer(tracerName),
	}, nil
}

// NewFromStore creates a Client with a pre-existing [ObjectStore]. This
// constructor is intended for testing with mock stores and for advanced
// use cases where a custom store implementation is needed.
//
// The cfg parameter is stored but not validated; pass nil for a zero-value
// config in tests.
//
// Example (testing):
//
//	mock := &mockObjectStore{}
//	client := minio.NewFromStore(mock, nil)
func NewFromStore(store ObjectStore, cfg *Config) *Client {
	if cfg == nil {
		cfg = &Config{}
	}
	return &Client{
		store:  store,
		config: cfg,
		tracer: otel.Tracer(tracerName),
	}
}

// PutObject uploads an object to a bucket, with OpenTelemetry tracing.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other storage errors
func (c *Client) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	ctx, span := c.startSpan(ctx, "PutObject", bucketName, fmt.Sprintf("PUT %s/%s", bucketName, objectName))

	info, err := c.store.PutObject(ctx, bucketName, objectName, reader, objectSize, opts)
	finishSpan(span, err)
	if err != nil {
		return info, wrapError(err, "minio: put object failed")
	}
	return info, nil
}

// GetObject retrieves an object from a bucket, with OpenTelemetry tracing.
// The returned [*minio.Object] implements io.ReadCloser and must be closed
// by the caller when done.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other storage errors
func (c *Client) GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) (*minio.Object, error) {
	ctx, span := c.startSpan(ctx, "GetObject", bucketName, fmt.Sprintf("GET %s/%s", bucketName, objectName))

	obj, err := c.store.GetObject(ctx, bucketName, objectName, opts)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "minio: get object failed")
	}
	return obj, nil
}

// RemoveObject deletes an object from a bucket, with OpenTelemetry tracing.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other storage errors
func (c *Client) RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error {
	ctx, span := c.startSpan(ctx, "RemoveObject", bucketName, fmt.Sprintf("DELETE %s/%s", bucketName, objectName))

	err := c.store.RemoveObject(ctx, bucketName, objectName, opts)
	finishSpan(span, err)
	if err != nil {
		return wrapError(err, "minio: remove object failed")
	}
	return nil
}

// StatObject retrieves metadata about an object without downloading it,
// with OpenTelemetry tracing.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other storage errors
func (c *Client) StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	ctx, span := c.startSpan(ctx, "StatObject", bucketName, fmt.Sprintf("STAT %s/%s", bucketName, objectName))

	info, err := c.store.StatObject(ctx, bucketName, objectName, opts)
	finishSpan(span, err)
	if err != nil {
		return info, wrapError(err, "minio: stat object failed")
	}
	return info, nil
}

// ListObjects returns a channel of objects in a bucket matching the provided
// options, with OpenTelemetry tracing. The caller should drain the channel
// to completion.
func (c *Client) ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	ctx, span := c.startSpan(ctx, "ListObjects", bucketName, fmt.Sprintf("LIST %s prefix=%s", bucketName, opts.Prefix))
	defer span.End()

	return c.store.ListObjects(ctx, bucketName, opts)
}

// BucketExists checks whether a bucket exists on the server, with
// OpenTelemetry tracing.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other storage errors
func (c *Client) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	ctx, span := c.startSpan(ctx, "BucketExists", bucketName, fmt.Sprintf("HEAD %s", bucketName))

	exists, err := c.store.BucketExists(ctx, bucketName)
	finishSpan(span, err)
	if err != nil {
		return false, wrapError(err, "minio: bucket exists check failed")
	}
	return exists, nil
}

// MakeBucket creates a new bucket with the given name and options, with
// OpenTelemetry tracing.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other storage errors
func (c *Client) MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error {
	ctx, span := c.startSpan(ctx, "MakeBucket", bucketName, fmt.Sprintf("MAKE %s", bucketName))

	err := c.store.MakeBucket(ctx, bucketName, opts)
	finishSpan(span, err)
	if err != nil {
		return wrapError(err, "minio: make bucket failed")
	}
	return nil
}

// RemoveBucket deletes an empty bucket, with OpenTelemetry tracing.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other storage errors
func (c *Client) RemoveBucket(ctx context.Context, bucketName string) error {
	ctx, span := c.startSpan(ctx, "RemoveBucket", bucketName, fmt.Sprintf("REMOVE %s", bucketName))

	err := c.store.RemoveBucket(ctx, bucketName)
	finishSpan(span, err)
	if err != nil {
		return wrapError(err, "minio: remove bucket failed")
	}
	return nil
}

// PresignedGetObject generates a presigned URL for downloading an object,
// with OpenTelemetry tracing.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other storage errors
func (c *Client) PresignedGetObject(ctx context.Context, bucketName, objectName string, expires time.Duration, reqParams url.Values) (*url.URL, error) {
	ctx, span := c.startSpan(ctx, "PresignedGetObject", bucketName, fmt.Sprintf("PRESIGN GET %s/%s", bucketName, objectName))

	u, err := c.store.PresignedGetObject(ctx, bucketName, objectName, expires, reqParams)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "minio: presigned get object failed")
	}
	return u, nil
}

// PresignedPutObject generates a presigned URL for uploading an object,
// with OpenTelemetry tracing.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other storage errors
func (c *Client) PresignedPutObject(ctx context.Context, bucketName, objectName string, expires time.Duration) (*url.URL, error) {
	ctx, span := c.startSpan(ctx, "PresignedPutObject", bucketName, fmt.Sprintf("PRESIGN PUT %s/%s", bucketName, objectName))

	u, err := c.store.PresignedPutObject(ctx, bucketName, objectName, expires)
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "minio: presigned put object failed")
	}
	return u, nil
}

// Health verifies that the MinIO server is reachable by calling BucketExists.
// The bucket does not need to exist; a successful API call confirms
// connectivity. It applies [DefaultHealthTimeout] if the provided context
// has no deadline.
//
// Returns nil if MinIO is reachable, or a [*sserr.Error] with code
// [sserr.CodeUnavailableDependency] if the probe fails. This method is
// designed for use with health check endpoints and readiness probes.
//
// Example:
//
//	if err := client.Health(ctx); err != nil {
//	    log.Warn("minio health check failed", "error", err)
//	}
func (c *Client) Health(ctx context.Context) error {
	ctx, span := c.startSpan(ctx, "Health", "", "BucketExists health-check-probe")

	// Apply a default timeout if the caller's context has no deadline.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultHealthTimeout)
		defer cancel()
	}

	healthBucket := c.config.HealthBucket
	if healthBucket == "" {
		healthBucket = "health-check-probe"
	}

	_, err := c.store.BucketExists(ctx, healthBucket)
	finishSpan(span, err)
	if err != nil {
		return sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"minio: health check failed")
	}
	return nil
}

// Close is a no-op for the MinIO client. Unlike database clients with
// connection pools, the MinIO client uses stateless HTTP connections that
// do not require explicit cleanup. This method is provided for interface
// consistency with other client packages in the SDK.
//
// Close is safe to call multiple times.
func (c *Client) Close() {
	// No-op: MinIO client uses stateless HTTP connections.
	// There is no connection pool or persistent state to release.
}

// Store returns the underlying [ObjectStore] interface. This provides access
// to the raw MinIO client for advanced use cases that are not covered by the
// Client's methods.
//
// The returned ObjectStore should not be used to bypass tracing or error
// handling unless there is a specific reason to do so.
func (c *Client) Store() ObjectStore {
	return c.store
}

// startSpan creates a new OpenTelemetry span with standard database semantic
// attributes. It follows the OpenTelemetry semantic conventions for database
// client spans: https://opentelemetry.io/docs/specs/semconv/database/
func (c *Client) startSpan(ctx context.Context, operationName, bucketName, statement string) (context.Context, trace.Span) {
	ctx, span := c.tracer.Start(ctx, "minio."+operationName,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	span.SetAttributes(
		attribute.String("db.system", "minio"),
		attribute.String("db.name", bucketName),
		attribute.String("db.statement", truncateStatement(statement)),
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

// wrapError converts a storage error to a platform [*sserr.Error] with an
// appropriate error code. It distinguishes between timeout errors and general
// storage errors to enable callers to make retry decisions via
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
