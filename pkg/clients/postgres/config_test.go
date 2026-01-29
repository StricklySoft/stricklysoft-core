package postgres

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// Secret Type Tests
// ===========================================================================

func TestSecret_String_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-password")
	assert.Equal(t, "[REDACTED]", s.String())
}

func TestSecret_GoString_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-password")
	assert.Equal(t, "[REDACTED]", s.GoString())
}

func TestSecret_Value_ReturnsActualValue(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-password")
	assert.Equal(t, "super-secret-password", s.Value())
}

func TestSecret_MarshalText_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-password")
	data, err := s.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "[REDACTED]", string(data))
}

func TestSecret_Empty(t *testing.T) {
	t.Parallel()
	s := Secret("")
	assert.Equal(t, "", s.Value())
	assert.Equal(t, "[REDACTED]", s.String())
}

// ===========================================================================
// SSLMode Tests
// ===========================================================================

func TestSSLMode_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode SSLMode
		want string
	}{
		{SSLModeDisable, "disable"},
		{SSLModeAllow, "allow"},
		{SSLModePrefer, "prefer"},
		{SSLModeRequire, "require"},
		{SSLModeVerifyCA, "verify-ca"},
		{SSLModeVerifyFull, "verify-full"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.mode.String())
		})
	}
}

func TestSSLMode_Valid(t *testing.T) {
	t.Parallel()
	validModes := []SSLMode{
		SSLModeDisable, SSLModeAllow, SSLModePrefer,
		SSLModeRequire, SSLModeVerifyCA, SSLModeVerifyFull,
	}
	for _, m := range validModes {
		t.Run(string(m), func(t *testing.T) {
			t.Parallel()
			assert.True(t, m.Valid(), "Valid() = false for %q, want true", m)
		})
	}

	invalidModes := []SSLMode{"", "invalid", "REQUIRE", "verify_full"}
	for _, m := range invalidModes {
		t.Run("invalid_"+string(m), func(t *testing.T) {
			t.Parallel()
			assert.False(t, m.Valid(), "Valid() = true for %q, want false", m)
		})
	}
}

// ===========================================================================
// CloudProvider Tests
// ===========================================================================

func TestCloudProvider_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		provider CloudProvider
		want     string
	}{
		{CloudProviderNone, ""},
		{CloudProviderAWS, "aws"},
		{CloudProviderAzure, "azure"},
		{CloudProviderGCP, "gcp"},
	}
	for _, tt := range tests {
		name := tt.want
		if name == "" {
			name = "none"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.provider.String())
		})
	}
}

// ===========================================================================
// DefaultConfig Tests
// ===========================================================================

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	assert.Equal(t, DefaultHost, cfg.Host)
	assert.Equal(t, DefaultPort, cfg.Port)
	assert.Equal(t, DefaultDatabase, cfg.Database)
	assert.Equal(t, DefaultUser, cfg.User)
	assert.Equal(t, SSLModeRequire, cfg.SSLMode)
	assert.Equal(t, DefaultMaxConns, cfg.MaxConns)
	assert.Equal(t, DefaultMinConns, cfg.MinConns)
	assert.Equal(t, DefaultMaxConnLifetime, cfg.MaxConnLifetime)
	assert.Equal(t, DefaultMaxConnIdleTime, cfg.MaxConnIdleTime)
	assert.Equal(t, DefaultHealthCheckPeriod, cfg.HealthCheckPeriod)
	assert.Equal(t, DefaultConnectTimeout, cfg.ConnectTimeout)
}

// ===========================================================================
// Config.Validate Tests
// ===========================================================================

func TestConfig_Validate_MinimalValid(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Database: "mydb",
		User:     "myuser",
	}
	require.NoError(t, cfg.Validate())
	// Defaults should be applied.
	assert.Equal(t, DefaultHost, cfg.Host)
	assert.Equal(t, DefaultPort, cfg.Port)
	assert.Equal(t, SSLModeRequire, cfg.SSLMode)
	assert.Equal(t, DefaultMaxConns, cfg.MaxConns)
}

func TestConfig_Validate_FullySpecified(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Host:              "db.example.com",
		Port:              5433,
		Database:          "production",
		User:              "admin",
		Password:          Secret("pass"),
		SSLMode:           SSLModeVerifyFull,
		MaxConns:          50,
		MinConns:          10,
		MaxConnLifetime:   2 * time.Hour,
		MaxConnIdleTime:   time.Hour,
		HealthCheckPeriod: 30 * time.Second,
		ConnectTimeout:    5 * time.Second,
		CloudProvider:     CloudProviderAWS,
	}
	require.NoError(t, cfg.Validate())
	// Specified values should be preserved (not overwritten by defaults).
	assert.Equal(t, "db.example.com", cfg.Host)
	assert.Equal(t, 5433, cfg.Port)
	assert.Equal(t, int32(50), cfg.MaxConns)
}

func TestConfig_Validate_EmptyDatabase(t *testing.T) {
	t.Parallel()
	cfg := Config{User: "myuser"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database must not be empty")
}

func TestConfig_Validate_EmptyUser(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user must not be empty")
}

func TestConfig_Validate_InvalidPort_Negative(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", User: "myuser", Port: -1}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port must be between")
}

func TestConfig_Validate_InvalidPort_TooHigh(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", User: "myuser", Port: 70000}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port must be between")
}

func TestConfig_Validate_InvalidSSLMode(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Database: "mydb",
		User:     "myuser",
		SSLMode:  "invalid-mode",
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssl_mode")
}

func TestConfig_Validate_MaxConns_LessThan_MinConns(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Database: "mydb",
		User:     "myuser",
		MaxConns: 3,
		MinConns: 10,
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_conns")
}

func TestConfig_Validate_NegativeMaxConns(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", User: "myuser", MaxConns: -1, MinConns: 0}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_conns must be >= 1")
}

func TestConfig_Validate_NegativeMinConns(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", User: "myuser", MinConns: -1}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "min_conns must be >= 0")
}

func TestConfig_Validate_NegativeConnectTimeout(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", User: "myuser", ConnectTimeout: -1 * time.Second}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect_timeout must not be negative")
}

func TestConfig_Validate_NegativeMaxConnLifetime(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", User: "myuser", MaxConnLifetime: -1 * time.Hour}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_conn_lifetime must not be negative")
}

func TestConfig_Validate_NegativeMaxConnIdleTime(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", User: "myuser", MaxConnIdleTime: -1 * time.Minute}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_conn_idle_time must not be negative")
}

func TestConfig_Validate_NegativeHealthCheckPeriod(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", User: "myuser", HealthCheckPeriod: -1 * time.Second}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "health_check_period must not be negative")
}

func TestConfig_Validate_SSLRootCert_NotFound(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Database:    "mydb",
		User:        "myuser",
		SSLRootCert: "/nonexistent/path/to/cert.pem",
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssl_root_cert")
}

func TestConfig_Validate_DefaultTimeouts(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", User: "myuser"}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, DefaultConnectTimeout, cfg.ConnectTimeout)
	assert.Equal(t, DefaultHealthCheckPeriod, cfg.HealthCheckPeriod)
	assert.Equal(t, DefaultMaxConnLifetime, cfg.MaxConnLifetime)
	assert.Equal(t, DefaultMaxConnIdleTime, cfg.MaxConnIdleTime)
}

// ===========================================================================
// Config.Validate Tests — URI Mode
// ===========================================================================

func TestConfig_Validate_URI_Valid(t *testing.T) {
	t.Parallel()
	cfg := Config{URI: "postgres://user:pass@localhost:5432/mydb?sslmode=disable"}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate_URI_SkipsStructuredValidation(t *testing.T) {
	t.Parallel()
	// When URI is set, Database and User being empty should NOT cause an error.
	cfg := Config{URI: "postgres://user:pass@localhost:5432/mydb"}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate_URI_InvalidURI(t *testing.T) {
	t.Parallel()
	// A URI with an invalid control character should fail parsing.
	cfg := Config{URI: "postgres://user:pass@host:5432/db\x00"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URI is invalid")
}

func TestConfig_Validate_URI_InvalidScheme(t *testing.T) {
	t.Parallel()
	cfg := Config{URI: "mysql://user:pass@host:3306/mydb"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URI scheme must be")
}

func TestConfig_Validate_URI_NoScheme(t *testing.T) {
	t.Parallel()
	cfg := Config{URI: "not-a-postgres-uri"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URI scheme must be")
}

func TestConfig_Validate_URI_PostgresqlScheme(t *testing.T) {
	t.Parallel()
	// The "postgresql://" scheme variant should also be accepted.
	cfg := Config{URI: "postgresql://user:pass@localhost:5432/mydb"}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate_URI_AppliesPoolDefaults(t *testing.T) {
	t.Parallel()
	cfg := Config{URI: "postgres://user:pass@localhost:5432/mydb"}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, DefaultMaxConns, cfg.MaxConns)
}

// ===========================================================================
// Config.ConnectionString Tests
// ===========================================================================

func TestConfig_ConnectionString_URI_Passthrough(t *testing.T) {
	t.Parallel()
	uri := "postgres://user:pass@localhost:5432/mydb?sslmode=disable"
	cfg := Config{URI: uri}
	assert.Equal(t, uri, cfg.ConnectionString())
}

func TestConfig_ConnectionString_StructuredConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.Password = Secret("testpass")

	connStr := cfg.ConnectionString()
	assert.True(t, strings.HasPrefix(connStr, "postgres://"), "ConnectionString() = %q, want postgres:// prefix", connStr)
	assert.Contains(t, connStr, "postgres:testpass@")
	assert.Contains(t, connStr, DefaultHost)
	assert.Contains(t, connStr, "5432")
	assert.Contains(t, connStr, "sslmode=require")
}

func TestConfig_ConnectionString_SpecialCharactersInPassword(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Host:     "localhost",
		Port:     5432,
		Database: "testdb",
		User:     "user@domain",
		Password: Secret("p@ss:w0rd/special"),
		SSLMode:  SSLModeDisable,
	}
	connStr := cfg.ConnectionString()
	// The connection string should be a valid URL with encoded special chars.
	assert.Contains(t, connStr, "postgres://")
	// The password should be URL-encoded, not contain raw @ or /.
	assert.Equal(t, 1, strings.Count(connStr, "@"), "ConnectionString() = %q, expected exactly one @ (user/host separator)", connStr)
}

func TestConfig_ConnectionString_WithConnectTimeout(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.Password = Secret("pass")
	cfg.ConnectTimeout = 15 * time.Second

	connStr := cfg.ConnectionString()
	assert.Contains(t, connStr, "connect_timeout=15")
}

// ===========================================================================
// tlsConfig Tests
// ===========================================================================

func TestConfig_tlsConfig_NoSSLRootCert(t *testing.T) {
	t.Parallel()
	cfg := Config{SSLMode: SSLModeRequire}
	tlsCfg, err := cfg.tlsConfig()
	require.NoError(t, err)
	assert.Nil(t, tlsCfg)
}

func TestConfig_tlsConfig_SSLModeDisable(t *testing.T) {
	t.Parallel()
	cfg := Config{SSLMode: SSLModeDisable, SSLRootCert: "/some/cert.pem"}
	tlsCfg, err := cfg.tlsConfig()
	require.NoError(t, err)
	assert.Nil(t, tlsCfg)
}

func TestConfig_tlsConfig_InvalidCertPath(t *testing.T) {
	t.Parallel()
	cfg := Config{
		SSLMode:     SSLModeVerifyFull,
		SSLRootCert: "/nonexistent/cert.pem",
	}
	_, err := cfg.tlsConfig()
	require.Error(t, err)
}

func TestConfig_tlsConfig_InvalidCertContent(t *testing.T) {
	t.Parallel()
	// Create a temp file with invalid PEM content.
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "invalid.pem")
	err := os.WriteFile(certPath, []byte("not a valid certificate"), 0o600)
	require.NoError(t, err)

	cfg := Config{
		SSLMode:     SSLModeVerifyFull,
		SSLRootCert: certPath,
	}
	_, err = cfg.tlsConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestConfig_tlsConfig_VerifyFull_SetsServerName(t *testing.T) {
	t.Parallel()
	// Create a temp file with a self-signed CA cert.
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "ca.pem")
	err := os.WriteFile(certPath, testCACert, 0o600)
	require.NoError(t, err)

	cfg := Config{
		Host:        "db.example.com",
		SSLMode:     SSLModeVerifyFull,
		SSLRootCert: certPath,
	}
	tlsCfg, err := cfg.tlsConfig()
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)
	assert.Equal(t, "db.example.com", tlsCfg.ServerName)
	assert.False(t, tlsCfg.InsecureSkipVerify, "InsecureSkipVerify = true for verify-full, want false")
}

func TestConfig_tlsConfig_VerifyCA_SkipsHostname(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "ca.pem")
	err := os.WriteFile(certPath, testCACert, 0o600)
	require.NoError(t, err)

	cfg := Config{
		Host:        "db.example.com",
		SSLMode:     SSLModeVerifyCA,
		SSLRootCert: certPath,
	}
	tlsCfg, err := cfg.tlsConfig()
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)
	assert.True(t, tlsCfg.InsecureSkipVerify, "InsecureSkipVerify = false for verify-ca, want true (hostname check handled by VerifyConnection)")
	assert.NotNil(t, tlsCfg.VerifyConnection, "VerifyConnection = nil for verify-ca, want custom verifier")
}

func TestConfig_tlsConfig_Require_SkipsVerification(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "ca.pem")
	err := os.WriteFile(certPath, testCACert, 0o600)
	require.NoError(t, err)

	cfg := Config{
		SSLMode:     SSLModeRequire,
		SSLRootCert: certPath,
	}
	tlsCfg, err := cfg.tlsConfig()
	require.NoError(t, err)
	require.NotNil(t, tlsCfg)
	assert.True(t, tlsCfg.InsecureSkipVerify, "InsecureSkipVerify = false for require, want true")
}

// ===========================================================================
// tlsConfig VerifyConnection Callback Tests
// ===========================================================================

// TestConfig_tlsConfig_VerifyCA_CallbackRejectsNoCerts verifies that the
// verify-ca VerifyConnection callback returns an error when the server
// presents no certificates.
func TestConfig_tlsConfig_VerifyCA_CallbackRejectsNoCerts(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "ca.pem")
	err := os.WriteFile(certPath, testCACert, 0o600)
	require.NoError(t, err)

	cfg := Config{
		Host:        "db.example.com",
		SSLMode:     SSLModeVerifyCA,
		SSLRootCert: certPath,
	}
	tlsCfg, err := cfg.tlsConfig()
	require.NoError(t, err)

	// Call the VerifyConnection callback with no peer certificates.
	cs := tls.ConnectionState{
		PeerCertificates: nil,
	}
	verifyErr := tlsCfg.VerifyConnection(cs)
	require.Error(t, verifyErr)
	assert.Contains(t, verifyErr.Error(), "did not present a certificate")
}

// ===========================================================================
// truncateSQL Tests
// ===========================================================================

func TestTruncateSQL_Short(t *testing.T) {
	t.Parallel()
	sql := "SELECT 1"
	assert.Equal(t, sql, truncateSQL(sql))
}

func TestTruncateSQL_Exact(t *testing.T) {
	t.Parallel()
	sql := strings.Repeat("x", maxSQLTruncateLen)
	assert.Equal(t, sql, truncateSQL(sql))
}

func TestTruncateSQL_Long(t *testing.T) {
	t.Parallel()
	sql := strings.Repeat("x", maxSQLTruncateLen+50)
	got := truncateSQL(sql)
	assert.True(t, strings.HasSuffix(got, "..."), "truncateSQL() = %q, want suffix '...'", got)
	assert.Equal(t, maxSQLTruncateLen+3, len(got))
}

func TestTruncateSQL_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", truncateSQL(""))
}

func TestTruncateSQL_MultiByte(t *testing.T) {
	t.Parallel()
	// Build a string of 101 multi-byte runes (each '日' is 3 bytes in UTF-8).
	// Byte-based truncation at position 100 would land in the middle of a
	// 3-byte character, producing invalid UTF-8.
	sql := strings.Repeat("日", maxSQLTruncateLen+1)
	got := truncateSQL(sql)

	// Should truncate to exactly maxSQLTruncateLen runes + "...".
	runes := []rune(got)
	wantRuneLen := maxSQLTruncateLen + 3 // 100 runes + len("...")
	assert.Len(t, runes, wantRuneLen)
	assert.True(t, strings.HasSuffix(got, "..."), "truncateSQL() = %q, want suffix '...'", got)

	// Verify the result is valid UTF-8 (would fail if bytes were split).
	for i, r := range got {
		if r == '\uFFFD' {
			t.Errorf("truncateSQL() contains replacement character at byte %d, indicates invalid UTF-8", i)
			break
		}
	}
}

// ===========================================================================
// Test Fixtures
// ===========================================================================

// testCACert is a self-signed CA certificate for testing TLS configuration.
// It is NOT used for actual TLS connections; it only tests that the config
// correctly loads and parses PEM certificates. Generated with:
//
//	openssl req -x509 -newkey rsa:2048 -keyout /dev/null -out cert.pem \
//	    -days 365 -nodes -subj "/CN=localhost"
//
//nolint:lll
var testCACert = []byte(`-----BEGIN CERTIFICATE-----
MIIDCTCCAfGgAwIBAgIUbwwzDXoTi0Qj9fJEticuUSPDZtQwDQYJKoZIhvcNAQEL
BQAwFDESMBAGA1UEAwwJbG9jYWxob3N0MB4XDTI2MDEyODIzMDk1NVoXDTI3MDEy
ODIzMDk1NVowFDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEArhSGA+iIfKylWNa2tgCw6uIKJ+pS2Sb93vxfrsQD9wtB
wo6HAFJkokmfDSR/xZP210NEhnof5PKdh3lYLYmTsDgKs80UThqQwFAhLqIr8fI+
HDYitf6gWcg+bZkqN8itWUsg7ENIL8T9/W/8xcLfcQU0olHCdKh2QBiA/fFngL1U
Yjp9efsc+susuGd7apdglKTUxanMtYqIMC2L98VNzgojU4AKIqQ55pHJZp9sZB85
ke13svWM++gGzOVB3MvyajTpds0l97agJmbnKv1CKYhwaXnvrD59MN9CUoT2WdY1
5ewrj+RM56dUHMIMt9QciEbC2kWszxvvQMvd9VAqJQIDAQABo1MwUTAdBgNVHQ4E
FgQU8ziFa9bcY9vWaMDkQv+uutIDPBwwHwYDVR0jBBgwFoAU8ziFa9bcY9vWaMDk
Qv+uutIDPBwwDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEARmsp
DSwMdRQtgU6eKYj+h/tUhTeMv4tMXjpLJ4djOy+B0unBKCokAj3KIokkSWuzp5Ho
FT2riCtkmenVmTfTmE/NdDEOc5B7KBwiJZX+kymCiwPlwAhb61sS4KosjRrRrNwE
XMCJkYc4xx4ozqv9MmzPpSTtk7qeCVmt3+qlFoCtQSBAGGgp1hWZgUrRjWV3s8ci
nZy0zaDEw+T8JOYEOoLnMcWF/9Ca0AqyvpFYGvJHuZ42dpF9lNk85AgsVgy7bhWQ
q87tveJzka635nGa2aISjJRI7b5TNTi38m7Ps9lNsXuI647o2TJZDsd662LS4wf3
TJ4l41jvKEXiCdgpsQ==
-----END CERTIFICATE-----
`)
