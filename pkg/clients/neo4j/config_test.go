package neo4j

import (
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
// DefaultConfig Tests
// ===========================================================================

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	assert.Equal(t, DefaultHost, cfg.Host)
	assert.Equal(t, DefaultPort, cfg.Port)
	assert.Equal(t, DefaultScheme, cfg.Scheme)
	assert.Equal(t, DefaultDatabase, cfg.Database)
	assert.Equal(t, DefaultUsername, cfg.Username)
	assert.Equal(t, DefaultMaxConnectionPoolSize, cfg.MaxConnectionPoolSize)
	assert.Equal(t, DefaultMaxConnectionLifetime, cfg.MaxConnectionLifetime)
	assert.Equal(t, DefaultConnectionAcquisitionTimeout, cfg.ConnectionAcquisitionTimeout)
	assert.Equal(t, DefaultConnectTimeout, cfg.ConnectTimeout)
}

// ===========================================================================
// Config.Validate Tests
// ===========================================================================

func TestConfig_Validate_MinimalValid(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Database: "mydb",
		Username: "myuser",
	}
	require.NoError(t, cfg.Validate())
	// Defaults should be applied.
	assert.Equal(t, DefaultHost, cfg.Host)
	assert.Equal(t, DefaultPort, cfg.Port)
	assert.Equal(t, DefaultScheme, cfg.Scheme)
	assert.Equal(t, DefaultMaxConnectionPoolSize, cfg.MaxConnectionPoolSize)
}

func TestConfig_Validate_FullySpecified(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Host:                         "db.example.com",
		Port:                         7688,
		Scheme:                       "bolt",
		Database:                     "production",
		Username:                     "admin",
		Password:                     Secret("pass"),
		MaxConnectionPoolSize:        50,
		MaxConnectionLifetime:        2 * time.Hour,
		ConnectionAcquisitionTimeout: 30 * time.Second,
		ConnectTimeout:               5 * time.Second,
		Encrypted:                    true,
	}
	require.NoError(t, cfg.Validate())
	// Specified values should be preserved (not overwritten by defaults).
	assert.Equal(t, "db.example.com", cfg.Host)
	assert.Equal(t, 7688, cfg.Port)
	assert.Equal(t, 50, cfg.MaxConnectionPoolSize)
}

// ===========================================================================
// Config.Validate Tests - URI Mode
// ===========================================================================

func TestConfig_Validate_URI_Valid(t *testing.T) {
	t.Parallel()
	cfg := Config{
		URI:      "neo4j://localhost:7687",
		Database: "neo4j",
		Username: "neo4j",
	}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate_URI_InvalidScheme(t *testing.T) {
	t.Parallel()
	cfg := Config{
		URI:      "mysql://localhost:3306/mydb",
		Database: "neo4j",
		Username: "neo4j",
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URI scheme must be")
}

func TestConfig_Validate_URI_BoltScheme(t *testing.T) {
	t.Parallel()
	cfg := Config{
		URI:      "bolt://localhost:7687",
		Database: "neo4j",
		Username: "neo4j",
	}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate_URI_EncryptedSchemes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		uri  string
	}{
		{"neo4j+s", "neo4j+s://localhost:7687"},
		{"bolt+s", "bolt+s://localhost:7687"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := Config{
				URI:      tt.uri,
				Database: "neo4j",
				Username: "neo4j",
			}
			require.NoError(t, cfg.Validate())
		})
	}
}

func TestConfig_Validate_URI_AppliesPoolDefaults(t *testing.T) {
	t.Parallel()
	cfg := Config{
		URI:      "neo4j://localhost:7687",
		Database: "neo4j",
		Username: "neo4j",
	}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, DefaultMaxConnectionPoolSize, cfg.MaxConnectionPoolSize)
}

// ===========================================================================
// Config.Validate Tests - Structured Config Errors
// ===========================================================================

func TestConfig_Validate_EmptyDatabase(t *testing.T) {
	t.Parallel()
	cfg := Config{Username: "myuser"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database must not be empty")
}

func TestConfig_Validate_EmptyUsername(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "username must not be empty")
}

func TestConfig_Validate_InvalidPort_Negative(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", Username: "myuser", Port: -1}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port must be between")
}

func TestConfig_Validate_InvalidPort_TooHigh(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", Username: "myuser", Port: 70000}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port must be between")
}

func TestConfig_Validate_NegativePoolSize(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", Username: "myuser", MaxConnectionPoolSize: -1}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_connection_pool_size must be >= 1")
}

func TestConfig_Validate_NegativeMaxConnectionLifetime(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", Username: "myuser", MaxConnectionLifetime: -1 * time.Hour}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_connection_lifetime must not be negative")
}

func TestConfig_Validate_NegativeConnectionAcquisitionTimeout(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", Username: "myuser", ConnectionAcquisitionTimeout: -1 * time.Second}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection_acquisition_timeout must not be negative")
}

func TestConfig_Validate_NegativeConnectTimeout(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", Username: "myuser", ConnectTimeout: -1 * time.Second}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connect_timeout must not be negative")
}

func TestConfig_Validate_DefaultTimeouts(t *testing.T) {
	t.Parallel()
	cfg := Config{Database: "mydb", Username: "myuser"}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, DefaultConnectTimeout, cfg.ConnectTimeout)
	assert.Equal(t, DefaultMaxConnectionLifetime, cfg.MaxConnectionLifetime)
	assert.Equal(t, DefaultConnectionAcquisitionTimeout, cfg.ConnectionAcquisitionTimeout)
}

// ===========================================================================
// Config.ConnectionURI Tests
// ===========================================================================

func TestConfig_ConnectionURI_URIPassthrough(t *testing.T) {
	t.Parallel()
	uri := "neo4j://user@localhost:7687"
	cfg := Config{URI: uri}
	assert.Equal(t, uri, cfg.ConnectionURI())
}

func TestConfig_ConnectionURI_StructuredConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	connURI := cfg.ConnectionURI()
	assert.Equal(t, "neo4j://neo4j.databases.svc.cluster.local:7687", connURI)
}

func TestConfig_ConnectionURI_BoltScheme(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Host:   "localhost",
		Port:   7687,
		Scheme: "bolt",
	}
	connURI := cfg.ConnectionURI()
	assert.Equal(t, "bolt://localhost:7687", connURI)
}

func TestConfig_ConnectionURI_DefaultScheme(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Host: "localhost",
		Port: 7687,
	}
	connURI := cfg.ConnectionURI()
	assert.Equal(t, "neo4j://localhost:7687", connURI)
}

// ===========================================================================
// truncateStatement Tests
// ===========================================================================

func TestTruncateStatement_Short(t *testing.T) {
	t.Parallel()
	s := "MATCH (n) RETURN n"
	assert.Equal(t, s, truncateStatement(s))
}

func TestTruncateStatement_Exact(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("x", maxStatementTruncateLen)
	assert.Equal(t, s, truncateStatement(s))
}

func TestTruncateStatement_Long(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("x", maxStatementTruncateLen+50)
	got := truncateStatement(s)
	assert.True(t, strings.HasSuffix(got, "..."), "truncateStatement() = %q, want suffix '...'", got)
	assert.Equal(t, maxStatementTruncateLen+3, len(got))
}

func TestTruncateStatement_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", truncateStatement(""))
}

func TestTruncateStatement_MultiByte(t *testing.T) {
	t.Parallel()
	// Build a string of 101 multi-byte runes (each rune is 3 bytes in UTF-8).
	// Byte-based truncation at position 100 would land in the middle of a
	// 3-byte character, producing invalid UTF-8.
	s := strings.Repeat("\u65e5", maxStatementTruncateLen+1)
	got := truncateStatement(s)

	// Should truncate to exactly maxStatementTruncateLen runes + "...".
	runes := []rune(got)
	wantRuneLen := maxStatementTruncateLen + 3 // 100 runes + len("...")
	assert.Len(t, runes, wantRuneLen)
	assert.True(t, strings.HasSuffix(got, "..."), "truncateStatement() = %q, want suffix '...'", got)

	// Verify the result is valid UTF-8 (would fail if bytes were split).
	for i, r := range got {
		if r == '\uFFFD' {
			t.Errorf("truncateStatement() contains replacement character at byte %d, indicates invalid UTF-8", i)
			break
		}
	}
}
