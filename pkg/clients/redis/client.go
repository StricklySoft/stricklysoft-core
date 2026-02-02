package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// tracerName is the OpenTelemetry instrumentation scope name for this package.
// It follows the Go module path convention for OTel instrumentation libraries.
const tracerName = "github.com/StricklySoft/stricklysoft-core/pkg/clients/redis"

// Cmdable defines the interface for Redis command operations. This interface
// is satisfied by [*redis.Client] and by mock implementations for unit
// testing. It enables dependency injection via [NewFromClient] for testing
// without a real Redis instance.
//
// The interface is intentionally narrow, exposing only the operations that
// the [Client] wraps with tracing and error handling.
type Cmdable interface {
	// Set sets the string value of a key with an optional expiration.
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd

	// Get returns the string value of a key.
	Get(ctx context.Context, key string) *redis.StringCmd

	// Del deletes one or more keys.
	Del(ctx context.Context, keys ...string) *redis.IntCmd

	// Exists returns the number of keys that exist from the specified keys.
	Exists(ctx context.Context, keys ...string) *redis.IntCmd

	// Expire sets an expiration on a key.
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd

	// TTL returns the remaining time to live of a key.
	TTL(ctx context.Context, key string) *redis.DurationCmd

	// Incr increments the integer value of a key by one.
	Incr(ctx context.Context, key string) *redis.IntCmd

	// Decr decrements the integer value of a key by one.
	Decr(ctx context.Context, key string) *redis.IntCmd

	// HSet sets field-value pairs in a hash stored at key.
	HSet(ctx context.Context, key string, values ...interface{}) *redis.IntCmd

	// HGet returns the value of a field in a hash.
	HGet(ctx context.Context, key, field string) *redis.StringCmd

	// HGetAll returns all fields and values in a hash.
	HGetAll(ctx context.Context, key string) *redis.MapStringStringCmd

	// HDel deletes one or more fields from a hash.
	HDel(ctx context.Context, key string, fields ...string) *redis.IntCmd

	// LPush prepends one or more values to a list.
	LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd

	// RPush appends one or more values to a list.
	RPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd

	// LRange returns a range of elements from a list.
	LRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd

	// LLen returns the length of a list.
	LLen(ctx context.Context, key string) *redis.IntCmd

	// SAdd adds one or more members to a set.
	SAdd(ctx context.Context, key string, members ...interface{}) *redis.IntCmd

	// SMembers returns all members of a set.
	SMembers(ctx context.Context, key string) *redis.StringSliceCmd

	// SIsMember determines if a value is a member of a set.
	SIsMember(ctx context.Context, key string, member interface{}) *redis.BoolCmd

	// SRem removes one or more members from a set.
	SRem(ctx context.Context, key string, members ...interface{}) *redis.IntCmd

	// Ping pings the Redis server.
	Ping(ctx context.Context) *redis.StatusCmd

	// Close closes the client connection.
	Close() error
}

// Compile-time interface compliance check. This ensures that *redis.Client
// satisfies the Cmdable interface at compile time rather than at runtime.
var _ Cmdable = (*redis.Client)(nil)

// Client is a Redis client with OpenTelemetry tracing and structured error
// handling. It wraps a [Cmdable] (typically [*redis.Client]) and adds
// cross-cutting concerns (tracing, error classification) transparently to
// all Redis operations.
//
// A Client is safe for concurrent use by multiple goroutines. Create one
// Client per Redis instance and share it across the application.
//
// Create a Client with [NewClient] for production use, or [NewFromClient]
// for testing with mock implementations.
type Client struct {
	cmdable Cmdable
	config  *Config
	tracer  trace.Tracer
	dbIndex int
}

// NewClient creates a new Redis client with connection pooling. It validates
// the configuration, creates a go-redis client with the appropriate options,
// and verifies connectivity with a ping.
//
// The caller must call [Client.Close] when the client is no longer needed
// to release connection resources.
//
// Error codes returned:
//   - [sserr.CodeValidation]: invalid configuration
//   - [sserr.CodeUnavailableDependency]: cannot connect to Redis
//
// Example:
//
//	cfg := redis.DefaultConfig()
//	cfg.Password = redis.Secret(os.Getenv("REDIS_PASSWORD"))
//	client, err := redis.NewClient(ctx, *cfg)
//	if err != nil {
//	    return fmt.Errorf("connecting to redis: %w", err)
//	}
//	defer client.Close()
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, sserr.Wrap(err, sserr.CodeValidation,
			"redis: invalid configuration")
	}

	var opts *redis.Options
	if cfg.URI != "" {
		var err error
		opts, err = redis.ParseURL(cfg.URI)
		if err != nil {
			return nil, sserr.Wrap(err, sserr.CodeValidation,
				"redis: failed to parse connection URI")
		}
		// Apply pool settings from config to parsed options.
		opts.PoolSize = cfg.PoolSize
		opts.MinIdleConns = cfg.MinIdleConns
		opts.MaxRetries = cfg.MaxRetries
		if cfg.DialTimeout > 0 {
			opts.DialTimeout = cfg.DialTimeout
		}
		if cfg.ReadTimeout > 0 {
			opts.ReadTimeout = cfg.ReadTimeout
		}
		if cfg.WriteTimeout > 0 {
			opts.WriteTimeout = cfg.WriteTimeout
		}
	} else {
		opts = &redis.Options{
			Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
			Password:     cfg.Password.Value(),
			DB:           cfg.DB,
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
			MaxRetries:   cfg.MaxRetries,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
		}
		if cfg.TLSEnabled {
			opts.TLSConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
		}
	}

	rdb := redis.NewClient(opts)

	// Verify connectivity before returning the client.
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"redis: failed to connect to server")
	}

	dbIndex := cfg.DB
	if cfg.URI != "" {
		dbIndex = opts.DB
	}

	return &Client{
		cmdable: rdb,
		config:  &cfg,
		tracer:  otel.Tracer(tracerName),
		dbIndex: dbIndex,
	}, nil
}

// NewFromClient creates a Client with a pre-existing [Cmdable]. This
// constructor is intended for testing with mock implementations and for
// advanced use cases where a custom client implementation is needed.
//
// The cfg parameter is stored but not validated; pass nil for a zero-value
// config in tests.
//
// Example (testing):
//
//	mock := &mockCmdable{}
//	client := redis.NewFromClient(mock, nil)
func NewFromClient(cmdable Cmdable, cfg *Config) *Client {
	if cfg == nil {
		cfg = &Config{}
	}
	return &Client{
		cmdable: cmdable,
		config:  cfg,
		tracer:  otel.Tracer(tracerName),
		dbIndex: cfg.DB,
	}
}

// Set sets the string value of a key with an optional expiration, with
// OpenTelemetry tracing.
//
// All errors are wrapped as [*sserr.Error] with an appropriate error code:
//   - [sserr.CodeTimeoutDatabase] if the context deadline is exceeded
//   - [sserr.CodeInternalDatabase] for all other Redis errors
//
// Example:
//
//	err := client.Set(ctx, "user:123", "Alice", 10*time.Minute)
func (c *Client) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	ctx, span := c.startSpan(ctx, "Set", fmt.Sprintf("SET %s", key))
	err := c.cmdable.Set(ctx, key, value, expiration).Err()
	finishSpan(span, err)
	if err != nil {
		return wrapError(err, "redis: set failed")
	}
	return nil
}

// Get returns the string value of a key, with OpenTelemetry tracing.
// Returns [redis.Nil] error when the key does not exist.
//
// Example:
//
//	val, err := client.Get(ctx, "user:123")
//	if errors.Is(err, redis.Nil) {
//	    // key does not exist
//	}
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	ctx, span := c.startSpan(ctx, "Get", fmt.Sprintf("GET %s", key))
	val, err := c.cmdable.Get(ctx, key).Result()
	finishSpan(span, err)
	if err != nil {
		return "", wrapError(err, "redis: get failed")
	}
	return val, nil
}

// Del deletes one or more keys and returns the number of keys that were
// removed, with OpenTelemetry tracing.
//
// Example:
//
//	deleted, err := client.Del(ctx, "key1", "key2")
func (c *Client) Del(ctx context.Context, keys ...string) (int64, error) {
	ctx, span := c.startSpan(ctx, "Del", fmt.Sprintf("DEL %v", keys))
	val, err := c.cmdable.Del(ctx, keys...).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: del failed")
	}
	return val, nil
}

// Exists returns the number of specified keys that exist, with
// OpenTelemetry tracing.
//
// Example:
//
//	count, err := client.Exists(ctx, "key1", "key2")
func (c *Client) Exists(ctx context.Context, keys ...string) (int64, error) {
	ctx, span := c.startSpan(ctx, "Exists", fmt.Sprintf("EXISTS %v", keys))
	val, err := c.cmdable.Exists(ctx, keys...).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: exists failed")
	}
	return val, nil
}

// Expire sets an expiration on a key and returns true if the timeout was
// set successfully, with OpenTelemetry tracing.
//
// Example:
//
//	ok, err := client.Expire(ctx, "session:abc", 30*time.Minute)
func (c *Client) Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	ctx, span := c.startSpan(ctx, "Expire", fmt.Sprintf("EXPIRE %s %v", key, expiration))
	val, err := c.cmdable.Expire(ctx, key, expiration).Result()
	finishSpan(span, err)
	if err != nil {
		return false, wrapError(err, "redis: expire failed")
	}
	return val, nil
}

// TTL returns the remaining time to live of a key, with OpenTelemetry
// tracing. Returns -1 if the key exists but has no associated expiration,
// and -2 if the key does not exist.
//
// Example:
//
//	ttl, err := client.TTL(ctx, "session:abc")
func (c *Client) TTL(ctx context.Context, key string) (time.Duration, error) {
	ctx, span := c.startSpan(ctx, "TTL", fmt.Sprintf("TTL %s", key))
	val, err := c.cmdable.TTL(ctx, key).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: ttl failed")
	}
	return val, nil
}

// Incr increments the integer value of a key by one and returns the new
// value, with OpenTelemetry tracing.
//
// Example:
//
//	newVal, err := client.Incr(ctx, "counter")
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	ctx, span := c.startSpan(ctx, "Incr", fmt.Sprintf("INCR %s", key))
	val, err := c.cmdable.Incr(ctx, key).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: incr failed")
	}
	return val, nil
}

// Decr decrements the integer value of a key by one and returns the new
// value, with OpenTelemetry tracing.
//
// Example:
//
//	newVal, err := client.Decr(ctx, "counter")
func (c *Client) Decr(ctx context.Context, key string) (int64, error) {
	ctx, span := c.startSpan(ctx, "Decr", fmt.Sprintf("DECR %s", key))
	val, err := c.cmdable.Decr(ctx, key).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: decr failed")
	}
	return val, nil
}

// HSet sets field-value pairs in a hash stored at key and returns the
// number of fields added, with OpenTelemetry tracing.
//
// Example:
//
//	added, err := client.HSet(ctx, "user:123", "name", "Alice", "age", "30")
func (c *Client) HSet(ctx context.Context, key string, values ...interface{}) (int64, error) {
	ctx, span := c.startSpan(ctx, "HSet", fmt.Sprintf("HSET %s", key))
	val, err := c.cmdable.HSet(ctx, key, values...).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: hset failed")
	}
	return val, nil
}

// HGet returns the value of a field in a hash stored at key, with
// OpenTelemetry tracing. Returns [redis.Nil] error when the field or
// key does not exist.
//
// Example:
//
//	name, err := client.HGet(ctx, "user:123", "name")
func (c *Client) HGet(ctx context.Context, key, field string) (string, error) {
	ctx, span := c.startSpan(ctx, "HGet", fmt.Sprintf("HGET %s %s", key, field))
	val, err := c.cmdable.HGet(ctx, key, field).Result()
	finishSpan(span, err)
	if err != nil {
		return "", wrapError(err, "redis: hget failed")
	}
	return val, nil
}

// HGetAll returns all fields and values in a hash stored at key, with
// OpenTelemetry tracing. Returns an empty map if the key does not exist.
//
// Example:
//
//	fields, err := client.HGetAll(ctx, "user:123")
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	ctx, span := c.startSpan(ctx, "HGetAll", fmt.Sprintf("HGETALL %s", key))
	val, err := c.cmdable.HGetAll(ctx, key).Result()
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "redis: hgetall failed")
	}
	return val, nil
}

// HDel deletes one or more fields from a hash stored at key and returns
// the number of fields removed, with OpenTelemetry tracing.
//
// Example:
//
//	removed, err := client.HDel(ctx, "user:123", "age")
func (c *Client) HDel(ctx context.Context, key string, fields ...string) (int64, error) {
	ctx, span := c.startSpan(ctx, "HDel", fmt.Sprintf("HDEL %s %v", key, fields))
	val, err := c.cmdable.HDel(ctx, key, fields...).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: hdel failed")
	}
	return val, nil
}

// LPush prepends one or more values to a list stored at key and returns
// the length of the list after the push, with OpenTelemetry tracing.
//
// Example:
//
//	length, err := client.LPush(ctx, "queue", "task1", "task2")
func (c *Client) LPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	ctx, span := c.startSpan(ctx, "LPush", fmt.Sprintf("LPUSH %s", key))
	val, err := c.cmdable.LPush(ctx, key, values...).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: lpush failed")
	}
	return val, nil
}

// RPush appends one or more values to a list stored at key and returns
// the length of the list after the push, with OpenTelemetry tracing.
//
// Example:
//
//	length, err := client.RPush(ctx, "queue", "task1", "task2")
func (c *Client) RPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	ctx, span := c.startSpan(ctx, "RPush", fmt.Sprintf("RPUSH %s", key))
	val, err := c.cmdable.RPush(ctx, key, values...).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: rpush failed")
	}
	return val, nil
}

// LRange returns a range of elements from a list stored at key, with
// OpenTelemetry tracing. The offsets start and stop are zero-based indexes.
// Use 0 and -1 to get all elements.
//
// Example:
//
//	items, err := client.LRange(ctx, "queue", 0, -1)
func (c *Client) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	ctx, span := c.startSpan(ctx, "LRange", fmt.Sprintf("LRANGE %s %d %d", key, start, stop))
	val, err := c.cmdable.LRange(ctx, key, start, stop).Result()
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "redis: lrange failed")
	}
	return val, nil
}

// LLen returns the length of a list stored at key, with OpenTelemetry
// tracing.
//
// Example:
//
//	length, err := client.LLen(ctx, "queue")
func (c *Client) LLen(ctx context.Context, key string) (int64, error) {
	ctx, span := c.startSpan(ctx, "LLen", fmt.Sprintf("LLEN %s", key))
	val, err := c.cmdable.LLen(ctx, key).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: llen failed")
	}
	return val, nil
}

// SAdd adds one or more members to a set stored at key and returns the
// number of members added (not including members already present), with
// OpenTelemetry tracing.
//
// Example:
//
//	added, err := client.SAdd(ctx, "tags", "go", "redis")
func (c *Client) SAdd(ctx context.Context, key string, members ...interface{}) (int64, error) {
	ctx, span := c.startSpan(ctx, "SAdd", fmt.Sprintf("SADD %s", key))
	val, err := c.cmdable.SAdd(ctx, key, members...).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: sadd failed")
	}
	return val, nil
}

// SMembers returns all members of a set stored at key, with OpenTelemetry
// tracing.
//
// Example:
//
//	members, err := client.SMembers(ctx, "tags")
func (c *Client) SMembers(ctx context.Context, key string) ([]string, error) {
	ctx, span := c.startSpan(ctx, "SMembers", fmt.Sprintf("SMEMBERS %s", key))
	val, err := c.cmdable.SMembers(ctx, key).Result()
	finishSpan(span, err)
	if err != nil {
		return nil, wrapError(err, "redis: smembers failed")
	}
	return val, nil
}

// SIsMember determines if a value is a member of a set stored at key,
// with OpenTelemetry tracing.
//
// Example:
//
//	isMember, err := client.SIsMember(ctx, "tags", "go")
func (c *Client) SIsMember(ctx context.Context, key string, member interface{}) (bool, error) {
	ctx, span := c.startSpan(ctx, "SIsMember", fmt.Sprintf("SISMEMBER %s", key))
	val, err := c.cmdable.SIsMember(ctx, key, member).Result()
	finishSpan(span, err)
	if err != nil {
		return false, wrapError(err, "redis: sismember failed")
	}
	return val, nil
}

// SRem removes one or more members from a set stored at key and returns
// the number of members removed, with OpenTelemetry tracing.
//
// Example:
//
//	removed, err := client.SRem(ctx, "tags", "redis")
func (c *Client) SRem(ctx context.Context, key string, members ...interface{}) (int64, error) {
	ctx, span := c.startSpan(ctx, "SRem", fmt.Sprintf("SREM %s", key))
	val, err := c.cmdable.SRem(ctx, key, members...).Result()
	finishSpan(span, err)
	if err != nil {
		return 0, wrapError(err, "redis: srem failed")
	}
	return val, nil
}

// Health verifies that the Redis connection is alive by executing a ping.
// It applies [DefaultHealthTimeout] if the provided context has no deadline.
//
// Returns nil if Redis is reachable, or a [*sserr.Error] with code
// [sserr.CodeUnavailableDependency] if the ping fails. This method is
// designed for use with health check endpoints and readiness probes.
//
// Example:
//
//	if err := client.Health(ctx); err != nil {
//	    log.Warn("redis health check failed", "error", err)
//	}
func (c *Client) Health(ctx context.Context) error {
	ctx, span := c.startSpan(ctx, "Health", "PING")

	// Apply a default timeout if the caller's context has no deadline.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultHealthTimeout)
		defer cancel()
	}

	err := c.cmdable.Ping(ctx).Err()
	finishSpan(span, err)
	if err != nil {
		return sserr.Wrap(err, sserr.CodeUnavailableDependency,
			"redis: health check failed")
	}
	return nil
}

// Close releases all connection resources. After Close is called,
// the client must not be used. Close is safe to call multiple times.
func (c *Client) Close() error {
	return c.cmdable.Close()
}

// Client returns the underlying [Cmdable] interface. This provides access
// to the raw Redis client for advanced use cases that are not covered by
// the Client's methods.
//
// The returned Cmdable should not be closed directly; use [Client.Close]
// instead.
func (c *Client) Client() Cmdable {
	return c.cmdable
}

// startSpan creates a new OpenTelemetry span with standard database semantic
// attributes. It follows the OpenTelemetry semantic conventions for database
// client spans: https://opentelemetry.io/docs/specs/semconv/database/
func (c *Client) startSpan(ctx context.Context, operationName, statement string) (context.Context, trace.Span) {
	ctx, span := c.tracer.Start(ctx, "redis."+operationName,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	span.SetAttributes(
		attribute.String("db.system", "redis"),
		attribute.Int("db.redis.database_index", c.dbIndex),
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

// wrapError converts a Redis error to a platform [*sserr.Error] with an
// appropriate error code. It distinguishes between timeout errors and general
// Redis errors to enable callers to make retry decisions via
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
