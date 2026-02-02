package neo4j

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

// maxStatementTruncateLen is the maximum length for Cypher statements recorded
// in OpenTelemetry trace spans. Statements longer than this are truncated to
// prevent sensitive data (property values, PII) from leaking into telemetry
// systems. The value 100 is intentionally conservative.
const maxStatementTruncateLen = 100

// Default connection pool and timeout settings for Kubernetes deployments.
// These values are tuned for a typical StricklySoft Cloud Platform deployment
// where Neo4j runs behind a Kubernetes Service with Linkerd mTLS.
const (
	// DefaultHost is the Kubernetes Service DNS name for the Neo4j
	// database in the StricklySoft Cloud Platform. This resolves to the
	// ClusterIP of the neo4j Service in the databases namespace.
	DefaultHost = "neo4j.databases.svc.cluster.local"

	// DefaultPort is the standard Neo4j Bolt protocol port.
	DefaultPort = 7687

	// DefaultScheme is the default URI scheme for Neo4j connections.
	// The "neo4j" scheme uses the Bolt protocol with routing support.
	DefaultScheme = "neo4j"

	// DefaultDatabase is the default Neo4j database name.
	DefaultDatabase = "neo4j"

	// DefaultUsername is the default Neo4j user for platform agents.
	DefaultUsername = "neo4j"

	// DefaultMaxConnectionPoolSize is the maximum number of connections
	// in the pool. This value balances connection availability against
	// database resource consumption.
	DefaultMaxConnectionPoolSize = 100

	// DefaultMaxConnectionLifetime is the maximum lifetime of a connection
	// before it is closed and replaced. This prevents connections from
	// becoming stale after DNS changes or load balancer reconfigurations.
	DefaultMaxConnectionLifetime = time.Hour

	// DefaultConnectionAcquisitionTimeout is the maximum time to wait
	// when acquiring a connection from the pool.
	DefaultConnectionAcquisitionTimeout = time.Minute

	// DefaultConnectTimeout is the maximum time to wait when establishing
	// a new connection to the database.
	DefaultConnectTimeout = 10 * time.Second

	// DefaultHealthTimeout is the maximum time for a health check when
	// the caller's context has no deadline.
	DefaultHealthTimeout = 5 * time.Second
)

// Secret is a string type that prevents accidental logging of sensitive values
// such as database passwords. Its [Secret.String] and [Secret.GoString]
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

// Config holds the Neo4j connection configuration. It supports both
// URI-based and structured configuration. When [Config.URI] is set, it takes
// precedence over individual fields (Host, Port, Scheme, Database, Username,
// Password).
//
// For Kubernetes deployments on the StricklySoft Cloud Platform, configuration
// values are typically injected as environment variables by the External
// Secrets Operator. The env struct tags document the expected environment
// variable names for each field.
//
// # Example
//
//	cfg := neo4j.DefaultConfig()
//	cfg.Password = neo4j.Secret("my-password")
//	client, err := neo4j.NewClient(ctx, *cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close(ctx)
type Config struct {
	// URI is a Neo4j connection URI (e.g.,
	// "neo4j://host:7687" or "bolt://host:7687").
	// When set, Host, Port, and Scheme are ignored.
	// Supports "neo4j://", "neo4j+s://", "bolt://", and "bolt+s://" schemes.
	// Environment variable: NEO4J_URI
	URI string `json:"uri,omitempty" env:"NEO4J_URI"`

	// Host is the Neo4j server hostname or IP address.
	// Default: "neo4j.databases.svc.cluster.local"
	// Environment variable: NEO4J_HOST
	Host string `json:"host,omitempty" env:"NEO4J_HOST"`

	// Port is the Neo4j Bolt protocol port.
	// Default: 7687
	// Environment variable: NEO4J_PORT
	Port int `json:"port,omitempty" env:"NEO4J_PORT"`

	// Scheme is the URI scheme for the Neo4j connection.
	// Default: "neo4j"
	// Environment variable: NEO4J_SCHEME
	Scheme string `json:"scheme,omitempty" env:"NEO4J_SCHEME"`

	// Database is the name of the Neo4j database to connect to.
	// Default: "neo4j"
	// Environment variable: NEO4J_DATABASE
	Database string `json:"database" env:"NEO4J_DATABASE"`

	// Username is the Neo4j user for authentication.
	// Default: "neo4j"
	// Environment variable: NEO4J_USERNAME
	Username string `json:"username" env:"NEO4J_USERNAME"`

	// Password is the Neo4j password. Uses the [Secret] type to
	// prevent accidental logging. Set via environment variable or
	// programmatically with [Secret] constructor.
	// Environment variable: NEO4J_PASSWORD
	Password Secret `json:"-" env:"NEO4J_PASSWORD"`

	// MaxConnectionPoolSize is the maximum number of connections in the pool.
	// Default: 100
	// Environment variable: NEO4J_MAX_CONNECTION_POOL_SIZE
	MaxConnectionPoolSize int `json:"max_connection_pool_size,omitempty" env:"NEO4J_MAX_CONNECTION_POOL_SIZE"`

	// MaxConnectionLifetime is the maximum lifetime of a connection before
	// it is closed and replaced. This prevents stale connections after DNS
	// changes.
	// Default: 1h
	// Environment variable: NEO4J_MAX_CONNECTION_LIFETIME
	MaxConnectionLifetime time.Duration `json:"max_connection_lifetime,omitempty" env:"NEO4J_MAX_CONNECTION_LIFETIME"`

	// ConnectionAcquisitionTimeout is the maximum time to wait when
	// acquiring a connection from the pool.
	// Default: 1m
	// Environment variable: NEO4J_CONNECTION_ACQUISITION_TIMEOUT
	ConnectionAcquisitionTimeout time.Duration `json:"connection_acquisition_timeout,omitempty" env:"NEO4J_CONNECTION_ACQUISITION_TIMEOUT"`

	// ConnectTimeout is the maximum time to wait when establishing a new
	// connection to the database.
	// Default: 10s
	// Environment variable: NEO4J_CONNECT_TIMEOUT
	ConnectTimeout time.Duration `json:"connect_timeout,omitempty" env:"NEO4J_CONNECT_TIMEOUT"`

	// Encrypted enables TLS encryption for the connection. When using
	// neo4j+s:// or bolt+s:// URI schemes, encryption is implicit.
	// Environment variable: NEO4J_ENCRYPTED
	Encrypted bool `json:"encrypted,omitempty" env:"NEO4J_ENCRYPTED"`
}

// validSchemes is the set of recognized Neo4j URI schemes.
var validSchemes = map[string]bool{
	"neo4j":   true,
	"neo4j+s": true,
	"bolt":    true,
	"bolt+s":  true,
}

// DefaultConfig returns a Config with default values suitable for the
// StricklySoft Cloud Platform Kubernetes deployment. Callers should override
// fields as needed before passing the config to [NewClient].
//
// Default values:
//   - Host: neo4j.databases.svc.cluster.local
//   - Port: 7687
//   - Scheme: neo4j
//   - Database: neo4j
//   - Username: neo4j
//   - MaxConnectionPoolSize: 100
//   - MaxConnectionLifetime: 1h
//   - ConnectionAcquisitionTimeout: 1m
//   - ConnectTimeout: 10s
func DefaultConfig() *Config {
	return &Config{
		Host:                         DefaultHost,
		Port:                         DefaultPort,
		Scheme:                       DefaultScheme,
		Database:                     DefaultDatabase,
		Username:                     DefaultUsername,
		MaxConnectionPoolSize:        DefaultMaxConnectionPoolSize,
		MaxConnectionLifetime:        DefaultMaxConnectionLifetime,
		ConnectionAcquisitionTimeout: DefaultConnectionAcquisitionTimeout,
		ConnectTimeout:               DefaultConnectTimeout,
	}
}

// Validate checks the configuration for invalid values and applies defaults
// for zero-valued fields. Returns the first validation error encountered,
// or nil if the configuration is valid.
//
// When [Config.URI] is set, structured fields (Host, Port, Scheme) are not
// validated because the URI takes precedence. Pool settings defaults are
// always applied when zero.
//
// Validation rules for structured config:
//   - Database must not be empty
//   - Username must not be empty
//   - Port must be between 1 and 65535
//   - MaxConnectionPoolSize must be >= 1
//   - Duration fields must not be negative
func (c *Config) Validate() error {
	// Apply pool and timeout defaults regardless of URI vs structured.
	c.applyPoolDefaults()

	if c.URI != "" {
		// URI-based config: validate that the URI is parseable and uses
		// a recognized Neo4j scheme.
		u, err := url.Parse(c.URI)
		if err != nil {
			return fmt.Errorf("neo4j: config URI is invalid: %w", err)
		}
		if !validSchemes[u.Scheme] {
			return fmt.Errorf("neo4j: config URI scheme must be neo4j://, neo4j+s://, bolt://, or bolt+s://, got %q", u.Scheme)
		}
		// Database and Username must still be valid even in URI mode.
		if c.Database == "" {
			c.Database = DefaultDatabase
		}
		if c.Username == "" {
			c.Username = DefaultUsername
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
		return fmt.Errorf("neo4j: config port must be between 1 and 65535, got %d", c.Port)
	}
	if c.Scheme == "" {
		c.Scheme = DefaultScheme
	}
	if c.Database == "" {
		return errors.New("neo4j: config database must not be empty")
	}
	if c.Username == "" {
		return errors.New("neo4j: config username must not be empty")
	}
	if c.MaxConnectionPoolSize < 1 {
		return fmt.Errorf("neo4j: config max_connection_pool_size must be >= 1, got %d", c.MaxConnectionPoolSize)
	}
	if c.MaxConnectionLifetime < 0 {
		return fmt.Errorf("neo4j: config max_connection_lifetime must not be negative, got %v", c.MaxConnectionLifetime)
	}
	if c.ConnectionAcquisitionTimeout < 0 {
		return fmt.Errorf("neo4j: config connection_acquisition_timeout must not be negative, got %v", c.ConnectionAcquisitionTimeout)
	}
	if c.ConnectTimeout < 0 {
		return fmt.Errorf("neo4j: config connect_timeout must not be negative, got %v", c.ConnectTimeout)
	}

	return nil
}

// applyPoolDefaults sets default values for zero-valued pool and timeout fields.
func (c *Config) applyPoolDefaults() {
	if c.MaxConnectionPoolSize == 0 {
		c.MaxConnectionPoolSize = DefaultMaxConnectionPoolSize
	}
	if c.MaxConnectionLifetime == 0 {
		c.MaxConnectionLifetime = DefaultMaxConnectionLifetime
	}
	if c.ConnectionAcquisitionTimeout == 0 {
		c.ConnectionAcquisitionTimeout = DefaultConnectionAcquisitionTimeout
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = DefaultConnectTimeout
	}
}

// ConnectionURI builds a Neo4j connection URI from the structured
// configuration fields. If [Config.URI] is set, it is returned directly.
//
// The returned string does not contain the password; authentication is
// handled separately via neo4j.BasicAuth when creating the driver.
func (c *Config) ConnectionURI() string {
	if c.URI != "" {
		return c.URI
	}
	scheme := c.Scheme
	if scheme == "" {
		scheme = DefaultScheme
	}
	return fmt.Sprintf("%s://%s:%d", scheme, c.Host, c.Port)
}

// truncateStatement truncates a Cypher statement to [maxStatementTruncateLen]
// runes for safe inclusion in OpenTelemetry trace spans. Truncated statements
// are suffixed with "..." to indicate truncation. The truncation is rune-aware
// to avoid splitting multi-byte UTF-8 characters.
func truncateStatement(s string) string {
	runes := []rune(s)
	if len(runes) <= maxStatementTruncateLen {
		return s
	}
	return string(runes[:maxStatementTruncateLen]) + "..."
}
