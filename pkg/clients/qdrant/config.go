package qdrant

import (
	"fmt"
	"time"
)

// maxStatementTruncateLen is the maximum length for statements recorded in
// OpenTelemetry trace spans. Statements longer than this are truncated to
// prevent sensitive data (payload content, vector values) from leaking into
// telemetry systems. The value 100 is intentionally conservative.
const maxStatementTruncateLen = 100

// Default connection and timeout settings for Kubernetes deployments.
// These values are tuned for a typical StricklySoft Cloud Platform deployment
// where Qdrant runs behind a Kubernetes Service with Linkerd mTLS.
const (
	// DefaultHost is the Kubernetes Service DNS name for the Qdrant
	// vector database in the StricklySoft Cloud Platform. This resolves
	// to the ClusterIP of the qdrant Service in the databases namespace.
	DefaultHost = "qdrant.databases.svc.cluster.local"

	// DefaultGRPCPort is the standard Qdrant gRPC port.
	DefaultGRPCPort = 6334

	// DefaultUseTLS controls whether the gRPC connection uses TLS.
	// In the StricklySoft Cloud Platform, Linkerd provides mTLS at the
	// network layer, so application-level TLS is disabled by default.
	DefaultUseTLS = false

	// DefaultHealthTimeout is the maximum time for a health check when
	// the caller's context has no deadline.
	DefaultHealthTimeout = 5 * time.Second
)

// Secret is a string type that prevents accidental logging of sensitive values
// such as API keys. Its [Secret.String] and [Secret.GoString] methods return
// a redacted placeholder. Use [Secret.Value] to retrieve the actual secret
// value.
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

// Config holds the Qdrant connection configuration. Configuration values are
// typically injected as environment variables by the External Secrets Operator
// on the StricklySoft Cloud Platform.
//
// # Kubernetes Deployment
//
// On the StricklySoft Cloud Platform, Qdrant is accessed via a Kubernetes
// Service at qdrant.databases.svc.cluster.local:6334. Linkerd provides mTLS
// at the network layer, so application-level TLS is disabled by default.
//
// # Example
//
//	cfg := qdrant.DefaultConfig()
//	cfg.APIKey = qdrant.Secret(os.Getenv("QDRANT_API_KEY"))
//	client, err := qdrant.NewClient(ctx, *cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
type Config struct {
	// Host is the Qdrant server hostname or IP address.
	// Default: "qdrant.databases.svc.cluster.local"
	// Environment variable: QDRANT_HOST
	Host string `json:"host,omitempty" env:"QDRANT_HOST"`

	// GRPCPort is the Qdrant gRPC server port.
	// Default: 6334
	// Environment variable: QDRANT_GRPC_PORT
	GRPCPort int `json:"grpc_port,omitempty" env:"QDRANT_GRPC_PORT"`

	// APIKey is the Qdrant API key for authentication. Uses the [Secret]
	// type to prevent accidental logging. Set via environment variable or
	// programmatically with [Secret] constructor.
	// Environment variable: QDRANT_API_KEY
	APIKey Secret `json:"-" env:"QDRANT_API_KEY"`

	// UseTLS controls whether the gRPC connection uses TLS.
	// Default: false (Linkerd mTLS handles encryption)
	// Environment variable: QDRANT_USE_TLS
	UseTLS bool `json:"use_tls,omitempty" env:"QDRANT_USE_TLS"`

	// HealthTimeout is the maximum time for a health check when the
	// caller's context has no deadline.
	// Default: 5s
	HealthTimeout time.Duration `json:"health_timeout,omitempty"`
}

// DefaultConfig returns a Config with default values suitable for the
// StricklySoft Cloud Platform Kubernetes deployment. Callers should override
// fields as needed before passing the config to [NewClient].
//
// Default values:
//   - Host: qdrant.databases.svc.cluster.local
//   - GRPCPort: 6334
//   - UseTLS: false
//   - HealthTimeout: 5s
func DefaultConfig() *Config {
	return &Config{
		Host:          DefaultHost,
		GRPCPort:      DefaultGRPCPort,
		UseTLS:        DefaultUseTLS,
		HealthTimeout: DefaultHealthTimeout,
	}
}

// Validate checks the configuration for invalid values and applies defaults
// for zero-valued fields. Returns the first validation error encountered,
// or nil if the configuration is valid.
//
// Validation rules:
//   - Host must not be empty
//   - GRPCPort must be between 1 and 65535
//   - HealthTimeout must not be negative
func (c *Config) Validate() error {
	if c.Host == "" {
		c.Host = DefaultHost
	}
	if c.GRPCPort == 0 {
		c.GRPCPort = DefaultGRPCPort
	}
	if c.GRPCPort < 1 || c.GRPCPort > 65535 {
		return fmt.Errorf("qdrant: config grpc_port must be between 1 and 65535, got %d", c.GRPCPort)
	}
	if c.HealthTimeout == 0 {
		c.HealthTimeout = DefaultHealthTimeout
	}
	if c.HealthTimeout < 0 {
		return fmt.Errorf("qdrant: config health_timeout must not be negative, got %v", c.HealthTimeout)
	}

	return nil
}

// GRPCAddress returns the host:port string for gRPC connections.
func (c *Config) GRPCAddress() string {
	return fmt.Sprintf("%s:%d", c.Host, c.GRPCPort)
}

// truncateStatement truncates a statement to [maxStatementTruncateLen] runes
// for safe inclusion in OpenTelemetry trace spans. Truncated statements are
// suffixed with "..." to indicate truncation. The truncation is rune-aware
// to avoid splitting multi-byte UTF-8 characters.
func truncateStatement(s string) string {
	runes := []rune(s)
	if len(runes) <= maxStatementTruncateLen {
		return s
	}
	return string(runes[:maxStatementTruncateLen]) + "..."
}
