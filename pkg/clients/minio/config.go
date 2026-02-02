package minio

import (
	"errors"
	"time"
)

// maxStatementTruncateLen is the maximum length for operation descriptions
// recorded in OpenTelemetry trace spans. Statements longer than this are
// truncated to prevent sensitive data (object keys, bucket names containing
// PII) from leaking into telemetry systems. The value 100 is intentionally
// conservative.
const maxStatementTruncateLen = 100

// Default configuration settings for Kubernetes deployments. These values
// are tuned for a typical StricklySoft Cloud Platform deployment where
// MinIO runs behind a Kubernetes Service with Linkerd mTLS.
const (
	// DefaultEndpoint is the Kubernetes Service DNS name for the MinIO
	// server in the StricklySoft Cloud Platform. This resolves to the
	// ClusterIP of the MinIO Service in the databases namespace.
	DefaultEndpoint = "minio.databases.svc.cluster.local:9000"

	// DefaultRegion is the default S3 region for MinIO. MinIO supports
	// region configuration for S3 API compatibility.
	DefaultRegion = "us-east-1"

	// DefaultUseSSL disables application-level TLS by default because
	// Linkerd provides mTLS at the network layer in the StricklySoft
	// Cloud Platform. For direct internet-facing MinIO deployments,
	// set UseSSL to true.
	DefaultUseSSL = false

	// DefaultHealthTimeout is the maximum time for a health check probe
	// when the caller's context has no deadline.
	DefaultHealthTimeout = 5 * time.Second
)

// Secret is a string type that prevents accidental logging of sensitive values
// such as MinIO secret keys. Its [Secret.String] and [Secret.GoString]
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

// Config holds the MinIO object storage connection configuration. Configuration
// values are typically injected as environment variables by the External
// Secrets Operator on the StricklySoft Cloud Platform.
//
// # Kubernetes Example
//
//	cfg := minio.DefaultConfig()
//	cfg.AccessKey = os.Getenv("MINIO_ACCESS_KEY")
//	cfg.SecretKey = minio.Secret(os.Getenv("MINIO_SECRET_KEY"))
//	client, err := minio.NewClient(ctx, *cfg)
type Config struct {
	// Endpoint is the MinIO server hostname and port (e.g.,
	// "minio.databases.svc.cluster.local:9000").
	// Default: "minio.databases.svc.cluster.local:9000"
	// Environment variable: MINIO_ENDPOINT
	Endpoint string `json:"endpoint,omitempty" env:"MINIO_ENDPOINT"`

	// AccessKey is the MinIO access key for authentication.
	// Environment variable: MINIO_ACCESS_KEY
	AccessKey string `json:"access_key,omitempty" env:"MINIO_ACCESS_KEY"`

	// SecretKey is the MinIO secret key. Uses the [Secret] type to
	// prevent accidental logging. Set via environment variable or
	// programmatically with [Secret] constructor.
	// Environment variable: MINIO_SECRET_KEY
	SecretKey Secret `json:"-" env:"MINIO_SECRET_KEY"`

	// Region is the S3 region for the MinIO server.
	// Default: "us-east-1"
	// Environment variable: MINIO_REGION
	Region string `json:"region,omitempty" env:"MINIO_REGION"`

	// UseSSL enables TLS for the connection to MinIO.
	// Default: false (Linkerd provides mTLS)
	// Environment variable: MINIO_USE_SSL
	UseSSL bool `json:"use_ssl,omitempty" env:"MINIO_USE_SSL"`

	// HealthBucket is the bucket name used for health checks. When empty,
	// the health check uses a probe bucket name ("health-check-probe")
	// and calls BucketExists which tests connectivity without requiring
	// the bucket to actually exist.
	// Environment variable: MINIO_HEALTH_BUCKET
	HealthBucket string `json:"health_bucket,omitempty" env:"MINIO_HEALTH_BUCKET"`
}

// DefaultConfig returns a Config with default values suitable for the
// StricklySoft Cloud Platform Kubernetes deployment. Callers should override
// fields as needed before passing the config to [NewClient].
//
// Default values:
//   - Endpoint: minio.databases.svc.cluster.local:9000
//   - Region: us-east-1
//   - UseSSL: false
func DefaultConfig() *Config {
	return &Config{
		Endpoint: DefaultEndpoint,
		Region:   DefaultRegion,
		UseSSL:   DefaultUseSSL,
	}
}

// Validate checks the configuration for invalid values and applies defaults
// for zero-valued fields. Returns the first validation error encountered,
// or nil if the configuration is valid.
//
// Validation rules:
//   - Endpoint must not be empty
//   - AccessKey must not be empty
//   - Region defaults to "us-east-1" if empty
func (c *Config) Validate() error {
	if c.Endpoint == "" {
		return errors.New("minio: config endpoint must not be empty")
	}
	if c.AccessKey == "" {
		return errors.New("minio: config access_key must not be empty")
	}
	if c.Region == "" {
		c.Region = DefaultRegion
	}
	return nil
}

// truncateStatement truncates an operation description to
// [maxStatementTruncateLen] runes for safe inclusion in OpenTelemetry trace
// spans. Truncated statements are suffixed with "..." to indicate truncation.
// The truncation is rune-aware to avoid splitting multi-byte UTF-8 characters.
func truncateStatement(s string) string {
	runes := []rune(s)
	if len(runes) <= maxStatementTruncateLen {
		return s
	}
	return string(runes[:maxStatementTruncateLen]) + "..."
}
