package postgres

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// Secret Type Tests
// ===========================================================================

func TestSecret_String_ReturnsRedacted(t *testing.T) {
	s := Secret("super-secret-password")
	if got := s.String(); got != "[REDACTED]" {
		t.Errorf("String() = %q, want %q", got, "[REDACTED]")
	}
}

func TestSecret_GoString_ReturnsRedacted(t *testing.T) {
	s := Secret("super-secret-password")
	if got := s.GoString(); got != "[REDACTED]" {
		t.Errorf("GoString() = %q, want %q", got, "[REDACTED]")
	}
}

func TestSecret_Value_ReturnsActualValue(t *testing.T) {
	s := Secret("super-secret-password")
	if got := s.Value(); got != "super-secret-password" {
		t.Errorf("Value() = %q, want %q", got, "super-secret-password")
	}
}

func TestSecret_MarshalText_ReturnsRedacted(t *testing.T) {
	s := Secret("super-secret-password")
	data, err := s.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error: %v", err)
	}
	if got := string(data); got != "[REDACTED]" {
		t.Errorf("MarshalText() = %q, want %q", got, "[REDACTED]")
	}
}

func TestSecret_Empty(t *testing.T) {
	s := Secret("")
	if got := s.Value(); got != "" {
		t.Errorf("Value() = %q, want empty string", got)
	}
	if got := s.String(); got != "[REDACTED]" {
		t.Errorf("String() = %q, want %q for empty secret", got, "[REDACTED]")
	}
}

// ===========================================================================
// SSLMode Tests
// ===========================================================================

func TestSSLMode_String(t *testing.T) {
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
			if got := tt.mode.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSSLMode_Valid(t *testing.T) {
	validModes := []SSLMode{
		SSLModeDisable, SSLModeAllow, SSLModePrefer,
		SSLModeRequire, SSLModeVerifyCA, SSLModeVerifyFull,
	}
	for _, m := range validModes {
		t.Run(string(m), func(t *testing.T) {
			if !m.Valid() {
				t.Errorf("Valid() = false for %q, want true", m)
			}
		})
	}

	invalidModes := []SSLMode{"", "invalid", "REQUIRE", "verify_full"}
	for _, m := range invalidModes {
		t.Run("invalid_"+string(m), func(t *testing.T) {
			if m.Valid() {
				t.Errorf("Valid() = true for %q, want false", m)
			}
		})
	}
}

// ===========================================================================
// CloudProvider Tests
// ===========================================================================

func TestCloudProvider_String(t *testing.T) {
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
			if got := tt.provider.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ===========================================================================
// DefaultConfig Tests
// ===========================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Host != DefaultHost {
		t.Errorf("Host = %q, want %q", cfg.Host, DefaultHost)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("Port = %d, want %d", cfg.Port, DefaultPort)
	}
	if cfg.Database != DefaultDatabase {
		t.Errorf("Database = %q, want %q", cfg.Database, DefaultDatabase)
	}
	if cfg.User != DefaultUser {
		t.Errorf("User = %q, want %q", cfg.User, DefaultUser)
	}
	if cfg.SSLMode != SSLModeRequire {
		t.Errorf("SSLMode = %q, want %q", cfg.SSLMode, SSLModeRequire)
	}
	if cfg.MaxConns != DefaultMaxConns {
		t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, DefaultMaxConns)
	}
	if cfg.MinConns != DefaultMinConns {
		t.Errorf("MinConns = %d, want %d", cfg.MinConns, DefaultMinConns)
	}
	if cfg.MaxConnLifetime != DefaultMaxConnLifetime {
		t.Errorf("MaxConnLifetime = %v, want %v", cfg.MaxConnLifetime, DefaultMaxConnLifetime)
	}
	if cfg.MaxConnIdleTime != DefaultMaxConnIdleTime {
		t.Errorf("MaxConnIdleTime = %v, want %v", cfg.MaxConnIdleTime, DefaultMaxConnIdleTime)
	}
	if cfg.HealthCheckPeriod != DefaultHealthCheckPeriod {
		t.Errorf("HealthCheckPeriod = %v, want %v", cfg.HealthCheckPeriod, DefaultHealthCheckPeriod)
	}
	if cfg.ConnectTimeout != DefaultConnectTimeout {
		t.Errorf("ConnectTimeout = %v, want %v", cfg.ConnectTimeout, DefaultConnectTimeout)
	}
}

// ===========================================================================
// Config.Validate Tests
// ===========================================================================

func TestConfig_Validate_MinimalValid(t *testing.T) {
	cfg := Config{
		Database: "mydb",
		User:     "myuser",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	// Defaults should be applied.
	if cfg.Host != DefaultHost {
		t.Errorf("Host = %q, want default %q", cfg.Host, DefaultHost)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("Port = %d, want default %d", cfg.Port, DefaultPort)
	}
	if cfg.SSLMode != SSLModeRequire {
		t.Errorf("SSLMode = %q, want default %q", cfg.SSLMode, SSLModeRequire)
	}
	if cfg.MaxConns != DefaultMaxConns {
		t.Errorf("MaxConns = %d, want default %d", cfg.MaxConns, DefaultMaxConns)
	}
}

func TestConfig_Validate_FullySpecified(t *testing.T) {
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
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	// Specified values should be preserved (not overwritten by defaults).
	if cfg.Host != "db.example.com" {
		t.Errorf("Host = %q, want %q", cfg.Host, "db.example.com")
	}
	if cfg.Port != 5433 {
		t.Errorf("Port = %d, want %d", cfg.Port, 5433)
	}
	if cfg.MaxConns != 50 {
		t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, 50)
	}
}

func TestConfig_Validate_EmptyDatabase(t *testing.T) {
	cfg := Config{User: "myuser"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for empty database, got nil")
	}
	if !strings.Contains(err.Error(), "database must not be empty") {
		t.Errorf("error = %q, want message about empty database", err.Error())
	}
}

func TestConfig_Validate_EmptyUser(t *testing.T) {
	cfg := Config{Database: "mydb"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for empty user, got nil")
	}
	if !strings.Contains(err.Error(), "user must not be empty") {
		t.Errorf("error = %q, want message about empty user", err.Error())
	}
}

func TestConfig_Validate_InvalidPort_Negative(t *testing.T) {
	cfg := Config{Database: "mydb", User: "myuser", Port: -1}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for negative port, got nil")
	}
	if !strings.Contains(err.Error(), "port must be between") {
		t.Errorf("error = %q, want message about port range", err.Error())
	}
}

func TestConfig_Validate_InvalidPort_TooHigh(t *testing.T) {
	cfg := Config{Database: "mydb", User: "myuser", Port: 70000}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for port > 65535, got nil")
	}
	if !strings.Contains(err.Error(), "port must be between") {
		t.Errorf("error = %q, want message about port range", err.Error())
	}
}

func TestConfig_Validate_InvalidSSLMode(t *testing.T) {
	cfg := Config{
		Database: "mydb",
		User:     "myuser",
		SSLMode:  "invalid-mode",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for invalid SSL mode, got nil")
	}
	if !strings.Contains(err.Error(), "ssl_mode") {
		t.Errorf("error = %q, want message about ssl_mode", err.Error())
	}
}

func TestConfig_Validate_MaxConns_LessThan_MinConns(t *testing.T) {
	cfg := Config{
		Database: "mydb",
		User:     "myuser",
		MaxConns: 3,
		MinConns: 10,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for max_conns < min_conns, got nil")
	}
	if !strings.Contains(err.Error(), "max_conns") {
		t.Errorf("error = %q, want message about max_conns", err.Error())
	}
}

func TestConfig_Validate_NegativeMaxConns(t *testing.T) {
	cfg := Config{Database: "mydb", User: "myuser", MaxConns: -1, MinConns: 0}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for negative max_conns, got nil")
	}
	if !strings.Contains(err.Error(), "max_conns must be >= 1") {
		t.Errorf("error = %q, want message about max_conns", err.Error())
	}
}

func TestConfig_Validate_NegativeMinConns(t *testing.T) {
	cfg := Config{Database: "mydb", User: "myuser", MinConns: -1}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for negative min_conns, got nil")
	}
	if !strings.Contains(err.Error(), "min_conns must be >= 0") {
		t.Errorf("error = %q, want message about min_conns", err.Error())
	}
}

func TestConfig_Validate_NegativeConnectTimeout(t *testing.T) {
	cfg := Config{Database: "mydb", User: "myuser", ConnectTimeout: -1 * time.Second}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for negative connect_timeout, got nil")
	}
	if !strings.Contains(err.Error(), "connect_timeout must not be negative") {
		t.Errorf("error = %q, want message about connect_timeout", err.Error())
	}
}

func TestConfig_Validate_NegativeMaxConnLifetime(t *testing.T) {
	cfg := Config{Database: "mydb", User: "myuser", MaxConnLifetime: -1 * time.Hour}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for negative max_conn_lifetime, got nil")
	}
	if !strings.Contains(err.Error(), "max_conn_lifetime must not be negative") {
		t.Errorf("error = %q, want message about max_conn_lifetime", err.Error())
	}
}

func TestConfig_Validate_NegativeMaxConnIdleTime(t *testing.T) {
	cfg := Config{Database: "mydb", User: "myuser", MaxConnIdleTime: -1 * time.Minute}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for negative max_conn_idle_time, got nil")
	}
	if !strings.Contains(err.Error(), "max_conn_idle_time must not be negative") {
		t.Errorf("error = %q, want message about max_conn_idle_time", err.Error())
	}
}

func TestConfig_Validate_NegativeHealthCheckPeriod(t *testing.T) {
	cfg := Config{Database: "mydb", User: "myuser", HealthCheckPeriod: -1 * time.Second}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for negative health_check_period, got nil")
	}
	if !strings.Contains(err.Error(), "health_check_period must not be negative") {
		t.Errorf("error = %q, want message about health_check_period", err.Error())
	}
}

func TestConfig_Validate_SSLRootCert_NotFound(t *testing.T) {
	cfg := Config{
		Database:    "mydb",
		User:        "myuser",
		SSLRootCert: "/nonexistent/path/to/cert.pem",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for missing SSL root cert, got nil")
	}
	if !strings.Contains(err.Error(), "ssl_root_cert") {
		t.Errorf("error = %q, want message about ssl_root_cert", err.Error())
	}
}

func TestConfig_Validate_DefaultTimeouts(t *testing.T) {
	cfg := Config{Database: "mydb", User: "myuser"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if cfg.ConnectTimeout != DefaultConnectTimeout {
		t.Errorf("ConnectTimeout = %v, want default %v", cfg.ConnectTimeout, DefaultConnectTimeout)
	}
	if cfg.HealthCheckPeriod != DefaultHealthCheckPeriod {
		t.Errorf("HealthCheckPeriod = %v, want default %v", cfg.HealthCheckPeriod, DefaultHealthCheckPeriod)
	}
	if cfg.MaxConnLifetime != DefaultMaxConnLifetime {
		t.Errorf("MaxConnLifetime = %v, want default %v", cfg.MaxConnLifetime, DefaultMaxConnLifetime)
	}
	if cfg.MaxConnIdleTime != DefaultMaxConnIdleTime {
		t.Errorf("MaxConnIdleTime = %v, want default %v", cfg.MaxConnIdleTime, DefaultMaxConnIdleTime)
	}
}

// ===========================================================================
// Config.Validate Tests — URI Mode
// ===========================================================================

func TestConfig_Validate_URI_Valid(t *testing.T) {
	cfg := Config{URI: "postgres://user:pass@localhost:5432/mydb?sslmode=disable"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
}

func TestConfig_Validate_URI_SkipsStructuredValidation(t *testing.T) {
	// When URI is set, Database and User being empty should NOT cause an error.
	cfg := Config{URI: "postgres://user:pass@localhost:5432/mydb"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
}

func TestConfig_Validate_URI_InvalidURI(t *testing.T) {
	// A URI with an invalid control character should fail parsing.
	cfg := Config{URI: "postgres://user:pass@host:5432/db\x00"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for invalid URI, got nil")
	}
	if !strings.Contains(err.Error(), "URI is invalid") {
		t.Errorf("error = %q, want message about invalid URI", err.Error())
	}
}

func TestConfig_Validate_URI_InvalidScheme(t *testing.T) {
	cfg := Config{URI: "mysql://user:pass@host:3306/mydb"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for non-postgres URI scheme, got nil")
	}
	if !strings.Contains(err.Error(), "URI scheme must be") {
		t.Errorf("error = %q, want message about URI scheme", err.Error())
	}
}

func TestConfig_Validate_URI_NoScheme(t *testing.T) {
	cfg := Config{URI: "not-a-postgres-uri"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for URI without scheme, got nil")
	}
	if !strings.Contains(err.Error(), "URI scheme must be") {
		t.Errorf("error = %q, want message about URI scheme", err.Error())
	}
}

func TestConfig_Validate_URI_PostgresqlScheme(t *testing.T) {
	// The "postgresql://" scheme variant should also be accepted.
	cfg := Config{URI: "postgresql://user:pass@localhost:5432/mydb"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error for postgresql:// scheme: %v", err)
	}
}

func TestConfig_Validate_URI_AppliesPoolDefaults(t *testing.T) {
	cfg := Config{URI: "postgres://user:pass@localhost:5432/mydb"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if cfg.MaxConns != DefaultMaxConns {
		t.Errorf("MaxConns = %d, want default %d even in URI mode", cfg.MaxConns, DefaultMaxConns)
	}
}

// ===========================================================================
// Config.ConnectionString Tests
// ===========================================================================

func TestConfig_ConnectionString_URI_Passthrough(t *testing.T) {
	uri := "postgres://user:pass@localhost:5432/mydb?sslmode=disable"
	cfg := Config{URI: uri}
	if got := cfg.ConnectionString(); got != uri {
		t.Errorf("ConnectionString() = %q, want %q", got, uri)
	}
}

func TestConfig_ConnectionString_StructuredConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Password = Secret("testpass")

	connStr := cfg.ConnectionString()
	if !strings.HasPrefix(connStr, "postgres://") {
		t.Errorf("ConnectionString() = %q, want postgres:// prefix", connStr)
	}
	if !strings.Contains(connStr, "postgres:testpass@") {
		t.Errorf("ConnectionString() = %q, want user:password@ in URI", connStr)
	}
	if !strings.Contains(connStr, DefaultHost) {
		t.Errorf("ConnectionString() = %q, want host %q", connStr, DefaultHost)
	}
	if !strings.Contains(connStr, "5432") {
		t.Errorf("ConnectionString() = %q, want port 5432", connStr)
	}
	if !strings.Contains(connStr, "sslmode=require") {
		t.Errorf("ConnectionString() = %q, want sslmode=require", connStr)
	}
}

func TestConfig_ConnectionString_SpecialCharactersInPassword(t *testing.T) {
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
	if !strings.Contains(connStr, "postgres://") {
		t.Errorf("ConnectionString() = %q, missing postgres:// scheme", connStr)
	}
	// The password should be URL-encoded, not contain raw @ or /.
	if strings.Count(connStr, "@") != 1 {
		t.Errorf("ConnectionString() = %q, expected exactly one @ (user/host separator)", connStr)
	}
}

func TestConfig_ConnectionString_WithConnectTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Password = Secret("pass")
	cfg.ConnectTimeout = 15 * time.Second

	connStr := cfg.ConnectionString()
	if !strings.Contains(connStr, "connect_timeout=15") {
		t.Errorf("ConnectionString() = %q, want connect_timeout=15", connStr)
	}
}

// ===========================================================================
// tlsConfig Tests
// ===========================================================================

func TestConfig_tlsConfig_NoSSLRootCert(t *testing.T) {
	cfg := Config{SSLMode: SSLModeRequire}
	tlsCfg, err := cfg.tlsConfig()
	if err != nil {
		t.Fatalf("tlsConfig() error: %v", err)
	}
	if tlsCfg != nil {
		t.Error("tlsConfig() returned non-nil when SSLRootCert is empty")
	}
}

func TestConfig_tlsConfig_SSLModeDisable(t *testing.T) {
	cfg := Config{SSLMode: SSLModeDisable, SSLRootCert: "/some/cert.pem"}
	tlsCfg, err := cfg.tlsConfig()
	if err != nil {
		t.Fatalf("tlsConfig() error: %v", err)
	}
	if tlsCfg != nil {
		t.Error("tlsConfig() returned non-nil when SSLMode is disable")
	}
}

func TestConfig_tlsConfig_InvalidCertPath(t *testing.T) {
	cfg := Config{
		SSLMode:     SSLModeVerifyFull,
		SSLRootCert: "/nonexistent/cert.pem",
	}
	_, err := cfg.tlsConfig()
	if err == nil {
		t.Fatal("tlsConfig() expected error for missing cert file, got nil")
	}
}

func TestConfig_tlsConfig_InvalidCertContent(t *testing.T) {
	// Create a temp file with invalid PEM content.
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "invalid.pem")
	if err := os.WriteFile(certPath, []byte("not a valid certificate"), 0o600); err != nil {
		t.Fatalf("failed to write temp cert: %v", err)
	}

	cfg := Config{
		SSLMode:     SSLModeVerifyFull,
		SSLRootCert: certPath,
	}
	_, err := cfg.tlsConfig()
	if err == nil {
		t.Fatal("tlsConfig() expected error for invalid cert content, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("error = %q, want message about parsing failure", err.Error())
	}
}

func TestConfig_tlsConfig_VerifyFull_SetsServerName(t *testing.T) {
	// Create a temp file with a self-signed CA cert.
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "ca.pem")
	if err := os.WriteFile(certPath, testCACert, 0o600); err != nil {
		t.Fatalf("failed to write temp cert: %v", err)
	}

	cfg := Config{
		Host:        "db.example.com",
		SSLMode:     SSLModeVerifyFull,
		SSLRootCert: certPath,
	}
	tlsCfg, err := cfg.tlsConfig()
	if err != nil {
		t.Fatalf("tlsConfig() error: %v", err)
	}
	if tlsCfg == nil {
		t.Fatal("tlsConfig() returned nil")
	}
	if tlsCfg.ServerName != "db.example.com" {
		t.Errorf("ServerName = %q, want %q", tlsCfg.ServerName, "db.example.com")
	}
	if tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = true for verify-full, want false")
	}
}

func TestConfig_tlsConfig_VerifyCA_SkipsHostname(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "ca.pem")
	if err := os.WriteFile(certPath, testCACert, 0o600); err != nil {
		t.Fatalf("failed to write temp cert: %v", err)
	}

	cfg := Config{
		Host:        "db.example.com",
		SSLMode:     SSLModeVerifyCA,
		SSLRootCert: certPath,
	}
	tlsCfg, err := cfg.tlsConfig()
	if err != nil {
		t.Fatalf("tlsConfig() error: %v", err)
	}
	if tlsCfg == nil {
		t.Fatal("tlsConfig() returned nil")
	}
	if !tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false for verify-ca, want true (hostname check handled by VerifyConnection)")
	}
	if tlsCfg.VerifyConnection == nil {
		t.Error("VerifyConnection = nil for verify-ca, want custom verifier")
	}
}

func TestConfig_tlsConfig_Require_SkipsVerification(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "ca.pem")
	if err := os.WriteFile(certPath, testCACert, 0o600); err != nil {
		t.Fatalf("failed to write temp cert: %v", err)
	}

	cfg := Config{
		SSLMode:     SSLModeRequire,
		SSLRootCert: certPath,
	}
	tlsCfg, err := cfg.tlsConfig()
	if err != nil {
		t.Fatalf("tlsConfig() error: %v", err)
	}
	if tlsCfg == nil {
		t.Fatal("tlsConfig() returned nil")
	}
	if !tlsCfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false for require, want true")
	}
}

// ===========================================================================
// tlsConfig VerifyConnection Callback Tests
// ===========================================================================

// TestConfig_tlsConfig_VerifyCA_CallbackRejectsNoCerts verifies that the
// verify-ca VerifyConnection callback returns an error when the server
// presents no certificates.
func TestConfig_tlsConfig_VerifyCA_CallbackRejectsNoCerts(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "ca.pem")
	if err := os.WriteFile(certPath, testCACert, 0o600); err != nil {
		t.Fatalf("failed to write temp cert: %v", err)
	}

	cfg := Config{
		Host:        "db.example.com",
		SSLMode:     SSLModeVerifyCA,
		SSLRootCert: certPath,
	}
	tlsCfg, err := cfg.tlsConfig()
	if err != nil {
		t.Fatalf("tlsConfig() error: %v", err)
	}

	// Call the VerifyConnection callback with no peer certificates.
	cs := tls.ConnectionState{
		PeerCertificates: nil,
	}
	verifyErr := tlsCfg.VerifyConnection(cs)
	if verifyErr == nil {
		t.Error("VerifyConnection() with no certs expected error, got nil")
	}
	if !strings.Contains(verifyErr.Error(), "did not present a certificate") {
		t.Errorf("error = %q, want message about missing certificate", verifyErr.Error())
	}
}

// ===========================================================================
// truncateSQL Tests
// ===========================================================================

func TestTruncateSQL_Short(t *testing.T) {
	sql := "SELECT 1"
	if got := truncateSQL(sql); got != sql {
		t.Errorf("truncateSQL(%q) = %q, want %q", sql, got, sql)
	}
}

func TestTruncateSQL_Exact(t *testing.T) {
	sql := strings.Repeat("x", maxSQLTruncateLen)
	if got := truncateSQL(sql); got != sql {
		t.Errorf("truncateSQL() length = %d, want %d", len(got), maxSQLTruncateLen)
	}
}

func TestTruncateSQL_Long(t *testing.T) {
	sql := strings.Repeat("x", maxSQLTruncateLen+50)
	got := truncateSQL(sql)
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncateSQL() = %q, want suffix '...'", got)
	}
	if len(got) != maxSQLTruncateLen+3 {
		t.Errorf("truncateSQL() length = %d, want %d", len(got), maxSQLTruncateLen+3)
	}
}

func TestTruncateSQL_Empty(t *testing.T) {
	if got := truncateSQL(""); got != "" {
		t.Errorf("truncateSQL(\"\") = %q, want empty string", got)
	}
}

func TestTruncateSQL_MultiByte(t *testing.T) {
	// Build a string of 101 multi-byte runes (each '日' is 3 bytes in UTF-8).
	// Byte-based truncation at position 100 would land in the middle of a
	// 3-byte character, producing invalid UTF-8.
	sql := strings.Repeat("日", maxSQLTruncateLen+1)
	got := truncateSQL(sql)

	// Should truncate to exactly maxSQLTruncateLen runes + "...".
	runes := []rune(got)
	wantRuneLen := maxSQLTruncateLen + 3 // 100 runes + len("...")
	if len(runes) != wantRuneLen {
		t.Errorf("truncateSQL() rune count = %d, want %d", len(runes), wantRuneLen)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncateSQL() = %q, want suffix '...'", got)
	}

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
