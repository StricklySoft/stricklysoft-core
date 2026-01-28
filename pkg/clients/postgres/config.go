package postgres

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"
)

// maxSQLTruncateLen is the maximum length for SQL statements recorded in
// OpenTelemetry trace spans. Statements longer than this are truncated to
// prevent sensitive data (column values, PII) from leaking into telemetry
// systems. The value 100 is intentionally conservative.
const maxSQLTruncateLen = 100

// Default connection pool and timeout settings for Kubernetes deployments.
// These values are tuned for a typical StricklySoft Cloud Platform deployment
// where PostgreSQL runs behind a Kubernetes Service with Linkerd mTLS.
const (
	// DefaultHost is the Kubernetes Service DNS name for the PostgreSQL
	// database in the StricklySoft Cloud Platform. This resolves to the
	// ClusterIP of the postgres Service in the databases namespace.
	DefaultHost = "postgres.databases.svc.cluster.local"

	// DefaultPort is the standard PostgreSQL port.
	DefaultPort = 5432

	// DefaultDatabase is the default database name for the StricklySoft
	// Cloud Platform.
	DefaultDatabase = "stricklysoft"

	// DefaultUser is the default PostgreSQL user for platform agents.
	DefaultUser = "postgres"

	// DefaultMaxConns is the maximum number of connections in the pool.
	// This value balances connection availability against database resource
	// consumption. Each PostgreSQL connection uses approximately 10 MB of
	// server memory.
	DefaultMaxConns int32 = 25

	// DefaultMinConns is the minimum number of idle connections maintained
	// in the pool. Keeping idle connections avoids the latency of
	// establishing new connections for burst traffic.
	DefaultMinConns int32 = 5

	// DefaultMaxConnLifetime is the maximum lifetime of a connection before
	// it is closed and replaced. This prevents connections from becoming
	// stale after DNS changes or load balancer reconfigurations.
	DefaultMaxConnLifetime = time.Hour

	// DefaultMaxConnIdleTime is the maximum time a connection can remain
	// idle before being closed. This releases resources from idle
	// connections during low-traffic periods.
	DefaultMaxConnIdleTime = 30 * time.Minute

	// DefaultHealthCheckPeriod is the interval between automatic health
	// checks on idle connections. Failed connections are removed from the
	// pool and replaced.
	DefaultHealthCheckPeriod = time.Minute

	// DefaultConnectTimeout is the maximum time to wait when establishing
	// a new connection to the database.
	DefaultConnectTimeout = 10 * time.Second

	// DefaultHealthTimeout is the maximum time for a health check ping
	// when the caller's context has no deadline.
	DefaultHealthTimeout = 5 * time.Second
)

// SSLMode represents the SSL/TLS connection mode for PostgreSQL.
// It maps directly to the PostgreSQL sslmode connection parameter.
//
// For the StricklySoft Cloud Platform on-premise deployment, Linkerd provides
// mTLS at the network layer, so application-level SSL can use [SSLModeDisable]
// or [SSLModeRequire]. For cloud-managed databases (AWS RDS, Azure Database,
// GCP Cloud SQL), use [SSLModeVerifyCA] or [SSLModeVerifyFull] with a custom
// CA certificate.
type SSLMode string

const (
	// SSLModeDisable disables SSL entirely. Use only when Linkerd mTLS or
	// another transport-layer encryption mechanism is active.
	SSLModeDisable SSLMode = "disable"

	// SSLModeAllow attempts SSL but falls back to an unencrypted connection.
	SSLModeAllow SSLMode = "allow"

	// SSLModePrefer attempts SSL first, falls back to unencrypted if the
	// server does not support SSL.
	SSLModePrefer SSLMode = "prefer"

	// SSLModeRequire requires SSL but does not verify the server certificate.
	// This is the default for enterprise deployments where certificate
	// management is handled externally (e.g., Linkerd, cloud provider).
	SSLModeRequire SSLMode = "require"

	// SSLModeVerifyCA requires SSL and verifies the server certificate
	// against a trusted CA. Use with [Config.SSLRootCert] to specify the
	// CA certificate file.
	SSLModeVerifyCA SSLMode = "verify-ca"

	// SSLModeVerifyFull requires SSL and verifies both the server certificate
	// chain and the server hostname. This is the most secure mode and is
	// recommended for cloud-managed databases.
	SSLModeVerifyFull SSLMode = "verify-full"
)

// String returns the string representation of the SSL mode.
func (m SSLMode) String() string {
	return string(m)
}

// Valid reports whether the SSL mode is one of the recognized values.
func (m SSLMode) Valid() bool {
	switch m {
	case SSLModeDisable, SSLModeAllow, SSLModePrefer,
		SSLModeRequire, SSLModeVerifyCA, SSLModeVerifyFull:
		return true
	default:
		return false
	}
}

// CloudProvider identifies the cloud platform hosting the PostgreSQL database.
// This field is informational and does not change client behavior. The actual
// differences between providers are handled by [Config.SSLMode] and
// [Config.SSLRootCert].
type CloudProvider string

const (
	// CloudProviderNone indicates an on-premise or self-managed deployment.
	CloudProviderNone CloudProvider = ""

	// CloudProviderAWS indicates AWS RDS PostgreSQL.
	// Requires SSL with the AWS RDS CA certificate.
	CloudProviderAWS CloudProvider = "aws"

	// CloudProviderAzure indicates Azure Database for PostgreSQL.
	// Requires SSL; the username format is "user@server".
	CloudProviderAzure CloudProvider = "azure"

	// CloudProviderGCP indicates GCP Cloud SQL for PostgreSQL.
	// Can use Cloud SQL Auth Proxy (sslmode=disable) or direct SSL.
	CloudProviderGCP CloudProvider = "gcp"
)

// String returns the string representation of the cloud provider.
func (p CloudProvider) String() string {
	return string(p)
}

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

// Config holds the PostgreSQL connection configuration. It supports both
// URI-based and structured configuration. When [Config.URI] is set, it takes
// precedence over individual fields (Host, Port, Database, User, Password).
//
// For Kubernetes deployments on the StricklySoft Cloud Platform, configuration
// values are typically injected as environment variables by the External
// Secrets Operator. The env struct tags document the expected environment
// variable names for each field.
//
// # Cloud Provider Examples
//
// AWS RDS:
//
//	cfg := postgres.DefaultConfig()
//	cfg.Host = "mydb.xxx.us-east-1.rds.amazonaws.com"
//	cfg.SSLMode = postgres.SSLModeVerifyFull
//	cfg.SSLRootCert = "/etc/ssl/certs/rds-ca-2019-root.pem"
//	cfg.CloudProvider = postgres.CloudProviderAWS
//
// Azure Database for PostgreSQL:
//
//	cfg := postgres.DefaultConfig()
//	cfg.Host = "mydb.postgres.database.azure.com"
//	cfg.User = "myuser@mydb"
//	cfg.SSLMode = postgres.SSLModeRequire
//	cfg.CloudProvider = postgres.CloudProviderAzure
//
// GCP Cloud SQL (via Auth Proxy):
//
//	cfg := postgres.DefaultConfig()
//	cfg.Host = "127.0.0.1"
//	cfg.SSLMode = postgres.SSLModeDisable
//	cfg.CloudProvider = postgres.CloudProviderGCP
type Config struct {
	// URI is a PostgreSQL connection string (e.g.,
	// "postgres://user:pass@host:5432/db?sslmode=require").
	// When set, Host, Port, Database, User, and Password are ignored.
	// Supports both "postgres://" and "postgresql://" schemes.
	// Environment variable: POSTGRES_URI
	URI string `json:"uri,omitempty" env:"POSTGRES_URI"`

	// Host is the PostgreSQL server hostname or IP address.
	// Default: "postgres.databases.svc.cluster.local"
	// Environment variable: POSTGRES_HOST
	Host string `json:"host,omitempty" env:"POSTGRES_HOST"`

	// Port is the PostgreSQL server port.
	// Default: 5432
	// Environment variable: POSTGRES_PORT
	Port int `json:"port,omitempty" env:"POSTGRES_PORT"`

	// Database is the name of the database to connect to.
	// Default: "stricklysoft"
	// Environment variable: POSTGRES_DATABASE
	Database string `json:"database" env:"POSTGRES_DATABASE"`

	// User is the PostgreSQL user for authentication.
	// Default: "postgres"
	// Environment variable: POSTGRES_USER
	User string `json:"user" env:"POSTGRES_USER"`

	// Password is the PostgreSQL password. Uses the [Secret] type to
	// prevent accidental logging. Set via environment variable or
	// programmatically with [Secret] constructor.
	// Environment variable: POSTGRES_PASSWORD
	Password Secret `json:"-" env:"POSTGRES_PASSWORD"`

	// SSLMode controls the SSL/TLS connection mode.
	// Default: SSLModeRequire
	// Environment variable: POSTGRES_SSLMODE
	SSLMode SSLMode `json:"ssl_mode,omitempty" env:"POSTGRES_SSLMODE"`

	// SSLRootCert is the file path to a PEM-encoded CA certificate for
	// TLS verification. Required when SSLMode is verify-ca or verify-full
	// with cloud databases (AWS RDS, Azure Database, GCP Cloud SQL).
	// Environment variable: POSTGRES_SSL_ROOT_CERT
	SSLRootCert string `json:"ssl_root_cert,omitempty" env:"POSTGRES_SSL_ROOT_CERT"`

	// MaxConns is the maximum number of connections in the pool.
	// Default: 25
	// Environment variable: POSTGRES_MAX_CONNS
	MaxConns int32 `json:"max_conns,omitempty" env:"POSTGRES_MAX_CONNS"`

	// MinConns is the minimum number of idle connections maintained in the
	// pool. Keeping idle connections avoids connection establishment latency.
	// Default: 5
	// Environment variable: POSTGRES_MIN_CONNS
	MinConns int32 `json:"min_conns,omitempty" env:"POSTGRES_MIN_CONNS"`

	// MaxConnLifetime is the maximum lifetime of a connection before it is
	// closed and replaced. This prevents stale connections after DNS changes.
	// Default: 1h
	// Environment variable: POSTGRES_MAX_CONN_LIFETIME
	MaxConnLifetime time.Duration `json:"max_conn_lifetime,omitempty" env:"POSTGRES_MAX_CONN_LIFETIME"`

	// MaxConnIdleTime is the maximum time a connection can remain idle
	// before being closed to free server resources.
	// Default: 30m
	// Environment variable: POSTGRES_MAX_CONN_IDLE_TIME
	MaxConnIdleTime time.Duration `json:"max_conn_idle_time,omitempty" env:"POSTGRES_MAX_CONN_IDLE_TIME"`

	// HealthCheckPeriod is the interval between automatic health checks on
	// idle connections. Failed connections are removed and replaced.
	// Default: 1m
	// Environment variable: POSTGRES_HEALTH_CHECK_PERIOD
	HealthCheckPeriod time.Duration `json:"health_check_period,omitempty" env:"POSTGRES_HEALTH_CHECK_PERIOD"`

	// ConnectTimeout is the maximum time to wait when establishing a new
	// connection to the database.
	// Default: 10s
	// Environment variable: POSTGRES_CONNECT_TIMEOUT
	ConnectTimeout time.Duration `json:"connect_timeout,omitempty" env:"POSTGRES_CONNECT_TIMEOUT"`

	// CloudProvider identifies the cloud platform hosting the database.
	// This is informational and does not change client behavior.
	// Environment variable: POSTGRES_CLOUD_PROVIDER
	CloudProvider CloudProvider `json:"cloud_provider,omitempty" env:"POSTGRES_CLOUD_PROVIDER"`
}

// DefaultConfig returns a Config with default values suitable for the
// StricklySoft Cloud Platform Kubernetes deployment. Callers should override
// fields as needed before passing the config to [NewClient].
//
// Default values:
//   - Host: postgres.databases.svc.cluster.local
//   - Port: 5432
//   - Database: stricklysoft
//   - User: postgres
//   - SSLMode: require
//   - MaxConns: 25, MinConns: 5
//   - MaxConnLifetime: 1h, MaxConnIdleTime: 30m
//   - HealthCheckPeriod: 1m, ConnectTimeout: 10s
func DefaultConfig() *Config {
	return &Config{
		Host:              DefaultHost,
		Port:              DefaultPort,
		Database:          DefaultDatabase,
		User:              DefaultUser,
		SSLMode:           SSLModeRequire,
		MaxConns:          DefaultMaxConns,
		MinConns:          DefaultMinConns,
		MaxConnLifetime:   DefaultMaxConnLifetime,
		MaxConnIdleTime:   DefaultMaxConnIdleTime,
		HealthCheckPeriod: DefaultHealthCheckPeriod,
		ConnectTimeout:    DefaultConnectTimeout,
	}
}

// Validate checks the configuration for invalid values and applies defaults
// for zero-valued fields. Returns the first validation error encountered,
// or nil if the configuration is valid.
//
// When [Config.URI] is set, structured fields (Host, Port, Database, User)
// are not validated because the URI takes precedence. Pool settings defaults
// are always applied when zero.
//
// Validation rules for structured config:
//   - Database must not be empty
//   - User must not be empty
//   - Port must be between 1 and 65535
//   - SSLMode must be a recognized value
//   - SSLRootCert (if set) must be a readable file
//   - MaxConns must be >= MinConns
func (c *Config) Validate() error {
	// Apply pool and timeout defaults regardless of URI vs structured.
	c.applyPoolDefaults()

	if c.URI != "" {
		// URI-based config: only validate the URI is parseable.
		_, err := url.Parse(c.URI)
		if err != nil {
			return fmt.Errorf("postgres: config URI is invalid: %w", err)
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
		return fmt.Errorf("postgres: config port must be between 1 and 65535, got %d", c.Port)
	}
	if c.Database == "" {
		return errors.New("postgres: config database must not be empty")
	}
	if c.User == "" {
		return errors.New("postgres: config user must not be empty")
	}
	if c.SSLMode == "" {
		c.SSLMode = SSLModeRequire
	}
	if !c.SSLMode.Valid() {
		return fmt.Errorf("postgres: config ssl_mode %q is not valid", c.SSLMode)
	}
	if c.SSLRootCert != "" {
		if _, err := os.Stat(c.SSLRootCert); err != nil {
			return fmt.Errorf("postgres: config ssl_root_cert %q is not accessible: %w", c.SSLRootCert, err)
		}
	}
	if c.MaxConns < c.MinConns {
		return fmt.Errorf("postgres: config max_conns (%d) must be >= min_conns (%d)", c.MaxConns, c.MinConns)
	}

	return nil
}

// applyPoolDefaults sets default values for zero-valued pool and timeout fields.
func (c *Config) applyPoolDefaults() {
	if c.MaxConns == 0 {
		c.MaxConns = DefaultMaxConns
	}
	if c.MinConns == 0 {
		c.MinConns = DefaultMinConns
	}
	if c.MaxConnLifetime == 0 {
		c.MaxConnLifetime = DefaultMaxConnLifetime
	}
	if c.MaxConnIdleTime == 0 {
		c.MaxConnIdleTime = DefaultMaxConnIdleTime
	}
	if c.HealthCheckPeriod == 0 {
		c.HealthCheckPeriod = DefaultHealthCheckPeriod
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = DefaultConnectTimeout
	}
}

// ConnectionString builds a PostgreSQL connection string from the structured
// configuration fields. If [Config.URI] is set, it is returned directly.
//
// The returned string contains the password in cleartext. Handle with care
// and avoid logging.
func (c *Config) ConnectionString() string {
	if c.URI != "" {
		return c.URI
	}

	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password.Value()),
		Host:   fmt.Sprintf("%s:%d", c.Host, c.Port),
		Path:   c.Database,
	}

	q := u.Query()
	if c.SSLMode != "" {
		q.Set("sslmode", string(c.SSLMode))
	}
	if c.ConnectTimeout > 0 {
		q.Set("connect_timeout", fmt.Sprintf("%d", int(c.ConnectTimeout.Seconds())))
	}
	u.RawQuery = q.Encode()

	return u.String()
}

// tlsConfig builds a *tls.Config for custom CA certificate verification.
// Returns nil if no custom CA certificate is configured, allowing pgx to
// handle TLS via the sslmode connection string parameter.
//
// This is used for cloud databases (AWS RDS, Azure Database, GCP Cloud SQL)
// that require the client to trust a specific CA certificate not present in
// the system certificate pool.
//
// TLS behavior by SSL mode:
//   - verify-full: Verifies certificate chain AND server hostname
//   - verify-ca: Verifies certificate chain only (hostname not checked)
//   - require/prefer/allow: TLS enabled but no certificate verification
//   - disable: No TLS (returns nil)
func (c *Config) tlsConfig() (*tls.Config, error) {
	if c.SSLRootCert == "" || c.SSLMode == SSLModeDisable {
		return nil, nil
	}

	caCert, err := os.ReadFile(c.SSLRootCert)
	if err != nil {
		return nil, fmt.Errorf("postgres: failed to read CA certificate %q: %w", c.SSLRootCert, err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("postgres: failed to parse CA certificate from %q", c.SSLRootCert)
	}

	tlsCfg := &tls.Config{
		RootCAs:    caCertPool,
		MinVersion: tls.VersionTLS12,
	}

	switch c.SSLMode {
	case SSLModeVerifyFull:
		// Full verification: check certificate chain AND hostname.
		tlsCfg.ServerName = c.Host
	case SSLModeVerifyCA:
		// Verify certificate chain but NOT hostname. Go's TLS library
		// verifies hostname by default when InsecureSkipVerify is false,
		// so we skip the automatic hostname check and verify the cert
		// chain manually via VerifyConnection.
		rootCAs := caCertPool
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("postgres: server did not present a certificate")
			}
			opts := x509.VerifyOptions{
				Roots:         rootCAs,
				Intermediates: x509.NewCertPool(),
			}
			for _, cert := range cs.PeerCertificates[1:] {
				opts.Intermediates.AddCert(cert)
			}
			_, err := cs.PeerCertificates[0].Verify(opts)
			return err
		}
	default:
		// require/prefer/allow: TLS enabled but no certificate verification.
		tlsCfg.InsecureSkipVerify = true
	}

	return tlsCfg, nil
}

// truncateSQL truncates a SQL statement to [maxSQLTruncateLen] characters
// for safe inclusion in OpenTelemetry trace spans. Truncated statements are
// suffixed with "..." to indicate truncation.
func truncateSQL(sql string) string {
	if len(sql) <= maxSQLTruncateLen {
		return sql
	}
	return sql[:maxSQLTruncateLen] + "..."
}
