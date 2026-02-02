// Package redis provides a Redis client with OpenTelemetry tracing, structured
// error handling, and configuration management for services running on the
// StricklySoft Cloud Platform.
//
// # Connection Management
//
// The client wraps go-redis (github.com/redis/go-redis/v9) and adds
// cross-cutting concerns (tracing, error classification) transparently to
// all Redis operations. Connection pooling, reconnection, and retry are
// handled internally by go-redis.
//
// # Configuration
//
// Create a client using [NewClient] with a [Config]:
//
//	cfg := redis.DefaultConfig()
//	cfg.Password = redis.Secret("my-password")
//	client, err := redis.NewClient(ctx, *cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
// For testing, use [NewFromClient] to inject a mock:
//
//	mock := &mockCmdable{}
//	client := redis.NewFromClient(mock, &redis.Config{DB: 0})
//
// # OpenTelemetry Tracing
//
// All Redis operations (Set, Get, Del, HSet, etc.) automatically create
// OpenTelemetry spans with standard database semantic attributes
// (db.system, db.redis.database_index, db.statement). Statements are
// truncated to 100 characters in spans to prevent sensitive data leakage.
//
// # Kubernetes Integration
//
// On the StricklySoft Cloud Platform, Redis is accessed via a Kubernetes
// Service at redis.databases.svc.cluster.local:6379. Credentials are
// injected by the External Secrets Operator from Vault.
package redis

import (
	"fmt"
	"net/url"
	"time"
)

// maxStatementTruncateLen is the maximum length for Redis command statements
// recorded in OpenTelemetry trace spans. Statements longer than this are
// truncated to prevent sensitive data (key values, PII) from leaking into
// telemetry systems. The value 100 is intentionally conservative.
const maxStatementTruncateLen = 100

// Default connection pool and timeout settings for Kubernetes deployments.
// These values are tuned for a typical StricklySoft Cloud Platform deployment
// where Redis runs behind a Kubernetes Service.
const (
	// DefaultHost is the Kubernetes Service DNS name for the Redis
	// database in the StricklySoft Cloud Platform. This resolves to the
	// ClusterIP of the redis Service in the databases namespace.
	DefaultHost = "redis.databases.svc.cluster.local"

	// DefaultPort is the standard Redis port.
	DefaultPort = 6379

	// DefaultDB is the default Redis database index. Redis supports
	// databases numbered 0-15 by default.
	DefaultDB = 0

	// DefaultPoolSize is the maximum number of connections in the pool.
	// This value balances connection availability against Redis resource
	// consumption.
	DefaultPoolSize = 25

	// DefaultMinIdleConns is the minimum number of idle connections
	// maintained in the pool. Keeping idle connections avoids the latency
	// of establishing new connections for burst traffic.
	DefaultMinIdleConns = 5

	// DefaultMaxRetries is the maximum number of retries before giving
	// up on a command. Set to 3 to handle transient network failures.
	DefaultMaxRetries = 3

	// DefaultDialTimeout is the maximum time to wait when establishing
	// a new connection to Redis.
	DefaultDialTimeout = 10 * time.Second

	// DefaultReadTimeout is the maximum time to wait for a read response
	// from Redis.
	DefaultReadTimeout = 5 * time.Second

	// DefaultWriteTimeout is the maximum time to wait for a write to
	// complete on the Redis connection.
	DefaultWriteTimeout = 5 * time.Second

	// DefaultHealthTimeout is the maximum time for a health check ping
	// when the caller's context has no deadline.
	DefaultHealthTimeout = 5 * time.Second
)

// Secret is a string type that prevents accidental logging of sensitive values
// such as Redis passwords. Its [Secret.String] and [Secret.GoString]
// methods return a redacted placeholder. Use [Secret.Value] to retrieve the
// actual secret value.
//
// Security: This type provides defense-in-depth against credential leakage
// in log output, error messages, and serialized configuration. It does NOT
// provide encryption at rest; use a secret manager (e.g., Vault via External
// Secrets Operator) for secret storage.
type Secret string

// redacted is the placeholder string returned by Secret's string methods.
const redacted = "[REDACTED]"

// String returns "[REDACTED]" to prevent accidental logging of the secret.
func (s Secret) String() string {
	return redacted
}

// GoString returns "[REDACTED]" for fmt.Sprintf("%#v", secret) safety.
func (s Secret) GoString() string {
	return redacted
}

// Value returns the actual secret string. Handle the returned value with
// care; avoid logging, serializing, or storing it in plaintext.
func (s Secret) Value() string {
	return string(s)
}

// MarshalText implements encoding.TextMarshaler, returning "[REDACTED]" to
// prevent the secret from appearing in JSON, YAML, or other text-based
// serialization formats.
func (s Secret) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}

// Config holds the Redis connection configuration. It supports both
// URI-based and structured configuration. When [Config.URI] is set, it takes
// precedence over individual fields (Host, Port, DB, Password).
//
// For Kubernetes deployments on the StricklySoft Cloud Platform, configuration
// values are typically injected as environment variables by the External
// Secrets Operator. The env struct tags document the expected environment
// variable names for each field.
//
// # URI-Based Configuration
//
//	cfg := &redis.Config{URI: "redis://:password@localhost:6379/0"}
//	client, err := redis.NewClient(ctx, *cfg)
//
// # Structured Configuration
//
//	cfg := redis.DefaultConfig()
//	cfg.Host = "redis.example.com"
//	cfg.Password = redis.Secret("my-password")
//	client, err := redis.NewClient(ctx, *cfg)
type Config struct {
	// URI is a Redis connection string (e.g.,
	// "redis://:password@host:6379/0" or "rediss://:password@host:6379/0").
	// When set, Host, Port, DB, and Password are ignored.
	// Supports both "redis://" and "rediss://" (TLS) schemes.
	// Environment variable: REDIS_URI
	URI string `json:"uri,omitempty" env:"REDIS_URI"`

	// Host is the Redis server hostname or IP address.
	// Default: "redis.databases.svc.cluster.local"
	// Environment variable: REDIS_HOST
	Host string `json:"host,omitempty" env:"REDIS_HOST"`

	// Port is the Redis server port.
	// Default: 6379
	// Environment variable: REDIS_PORT
	Port int `json:"port,omitempty" env:"REDIS_PORT"`

	// DB is the Redis database index (0-15 by default).
	// Default: 0
	// Environment variable: REDIS_DB
	DB int `json:"db" env:"REDIS_DB"`

	// Password is the Redis password. Uses the [Secret] type to
	// prevent accidental logging. Set via environment variable or
	// programmatically with [Secret] constructor.
	// Environment variable: REDIS_PASSWORD
	Password Secret `json:"-" env:"REDIS_PASSWORD"`

	// PoolSize is the maximum number of connections in the pool.
	// Default: 25
	// Environment variable: REDIS_POOL_SIZE
	PoolSize int `json:"pool_size,omitempty" env:"REDIS_POOL_SIZE"`

	// MinIdleConns is the minimum number of idle connections maintained
	// in the pool. Keeping idle connections avoids connection
	// establishment latency.
	// Default: 5
	// Environment variable: REDIS_MIN_IDLE_CONNS
	MinIdleConns int `json:"min_idle_conns,omitempty" env:"REDIS_MIN_IDLE_CONNS"`

	// MaxRetries is the maximum number of retries before giving up on
	// a command. Set to -1 to disable retries.
	// Default: 3
	// Environment variable: REDIS_MAX_RETRIES
	MaxRetries int `json:"max_retries,omitempty" env:"REDIS_MAX_RETRIES"`

	// DialTimeout is the maximum time to wait when establishing a new
	// connection to Redis.
	// Default: 10s
	// Environment variable: REDIS_DIAL_TIMEOUT
	DialTimeout time.Duration `json:"dial_timeout,omitempty" env:"REDIS_DIAL_TIMEOUT"`

	// ReadTimeout is the maximum time to wait for a read response from
	// Redis.
	// Default: 5s
	// Environment variable: REDIS_READ_TIMEOUT
	ReadTimeout time.Duration `json:"read_timeout,omitempty" env:"REDIS_READ_TIMEOUT"`

	// WriteTimeout is the maximum time to wait for a write to complete
	// on the Redis connection.
	// Default: 5s
	// Environment variable: REDIS_WRITE_TIMEOUT
	WriteTimeout time.Duration `json:"write_timeout,omitempty" env:"REDIS_WRITE_TIMEOUT"`

	// TLSEnabled indicates whether to use TLS for the Redis connection.
	// When URI is set with "rediss://" scheme, TLS is enabled automatically.
	// Default: false
	// Environment variable: REDIS_TLS_ENABLED
	TLSEnabled bool `json:"tls_enabled,omitempty" env:"REDIS_TLS_ENABLED"`
}

// DefaultConfig returns a Config with default values suitable for the
// StricklySoft Cloud Platform Kubernetes deployment. Callers should override
// fields as needed before passing the config to [NewClient].
//
// Default values:
//   - Host: redis.databases.svc.cluster.local
//   - Port: 6379
//   - DB: 0
//   - PoolSize: 25, MinIdleConns: 5
//   - MaxRetries: 3
//   - DialTimeout: 10s, ReadTimeout: 5s, WriteTimeout: 5s
func DefaultConfig() *Config {
	return &Config{
		Host:         DefaultHost,
		Port:         DefaultPort,
		DB:           DefaultDB,
		PoolSize:     DefaultPoolSize,
		MinIdleConns: DefaultMinIdleConns,
		MaxRetries:   DefaultMaxRetries,
		DialTimeout:  DefaultDialTimeout,
		ReadTimeout:  DefaultReadTimeout,
		WriteTimeout: DefaultWriteTimeout,
	}
}

// Validate checks the configuration for invalid values and applies defaults
// for zero-valued fields. Returns the first validation error encountered,
// or nil if the configuration is valid.
//
// When [Config.URI] is set, structured fields (Host, Port, DB) are not
// validated because the URI takes precedence. Pool and timeout defaults
// are always applied when zero.
//
// Validation rules:
//   - URI (if set) must have redis:// or rediss:// scheme
//   - Port must be between 1 and 65535
//   - PoolSize must be >= 1
//   - MinIdleConns must be >= 0
//   - Duration fields must not be negative
func (c *Config) Validate() error {
	// Apply pool and timeout defaults regardless of URI vs structured.
	c.applyDefaults()

	if c.URI != "" {
		// URI-based config: validate that the URI is parseable and uses
		// a recognized Redis scheme.
		u, err := url.Parse(c.URI)
		if err != nil {
			return fmt.Errorf("redis: config URI is invalid: %w", err)
		}
		if u.Scheme != "redis" && u.Scheme != "rediss" {
			return fmt.Errorf("redis: config URI scheme must be redis:// or rediss://, got %q", u.Scheme)
		}
		return nil
	}

	// Structured config validation.
	if c.Host == "" {
		c.Host = DefaultHost
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("redis: config port must be between 1 and 65535, got %d", c.Port)
	}
	if c.PoolSize < 1 {
		return fmt.Errorf("redis: config pool_size must be >= 1, got %d", c.PoolSize)
	}
	if c.MinIdleConns < 0 {
		return fmt.Errorf("redis: config min_idle_conns must be >= 0, got %d", c.MinIdleConns)
	}
	if c.PoolSize < c.MinIdleConns {
		return fmt.Errorf("redis: config pool_size (%d) must be >= min_idle_conns (%d)", c.PoolSize, c.MinIdleConns)
	}
	if c.DialTimeout < 0 {
		return fmt.Errorf("redis: config dial_timeout must not be negative, got %v", c.DialTimeout)
	}
	if c.ReadTimeout < 0 {
		return fmt.Errorf("redis: config read_timeout must not be negative, got %v", c.ReadTimeout)
	}
	if c.WriteTimeout < 0 {
		return fmt.Errorf("redis: config write_timeout must not be negative, got %v", c.WriteTimeout)
	}

	return nil
}

// applyDefaults sets default values for zero-valued pool and timeout fields.
func (c *Config) applyDefaults() {
	if c.PoolSize == 0 {
		c.PoolSize = DefaultPoolSize
	}
	if c.MinIdleConns == 0 {
		c.MinIdleConns = DefaultMinIdleConns
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = DefaultMaxRetries
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = DefaultDialTimeout
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = DefaultReadTimeout
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = DefaultWriteTimeout
	}
}

// truncateStatement truncates a Redis command statement to
// [maxStatementTruncateLen] runes for safe inclusion in OpenTelemetry
// trace spans. Truncated statements are suffixed with "..." to indicate
// truncation. The truncation is rune-aware to avoid splitting multi-byte
// UTF-8 characters.
func truncateStatement(s string) string {
	runes := []rune(s)
	if len(runes) <= maxStatementTruncateLen {
		return s
	}
	return string(runes[:maxStatementTruncateLen]) + "..."
}
