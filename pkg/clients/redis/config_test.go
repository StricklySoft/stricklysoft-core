package redis

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
	assert.Equal(t, DefaultDB, cfg.DB)
	assert.Equal(t, DefaultPoolSize, cfg.PoolSize)
	assert.Equal(t, DefaultMinIdleConns, cfg.MinIdleConns)
	assert.Equal(t, DefaultMaxRetries, cfg.MaxRetries)
	assert.Equal(t, DefaultDialTimeout, cfg.DialTimeout)
	assert.Equal(t, DefaultReadTimeout, cfg.ReadTimeout)
	assert.Equal(t, DefaultWriteTimeout, cfg.WriteTimeout)
}

// ===========================================================================
// Config.Validate Tests
// ===========================================================================

func TestConfig_Validate_MinimalValid(t *testing.T) {
	t.Parallel()
	cfg := Config{}
	require.NoError(t, cfg.Validate())
	// Defaults should be applied.
	assert.Equal(t, DefaultHost, cfg.Host)
	assert.Equal(t, DefaultPort, cfg.Port)
	assert.Equal(t, DefaultPoolSize, cfg.PoolSize)
}

func TestConfig_Validate_FullySpecified(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Host:         "redis.example.com",
		Port:         6380,
		DB:           3,
		Password:     Secret("pass"),
		PoolSize:     50,
		MinIdleConns: 10,
		MaxRetries:   5,
		DialTimeout:  15 * time.Second,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		TLSEnabled:   true,
	}
	require.NoError(t, cfg.Validate())
	// Specified values should be preserved (not overwritten by defaults).
	assert.Equal(t, "redis.example.com", cfg.Host)
	assert.Equal(t, 6380, cfg.Port)
	assert.Equal(t, 50, cfg.PoolSize)
}

func TestConfig_Validate_InvalidPort_Negative(t *testing.T) {
	t.Parallel()
	cfg := Config{Port: -1}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port must be between")
}

func TestConfig_Validate_InvalidPort_TooHigh(t *testing.T) {
	t.Parallel()
	cfg := Config{Port: 70000}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port must be between")
}

func TestConfig_Validate_NegativePoolSize(t *testing.T) {
	t.Parallel()
	cfg := Config{PoolSize: -1, MinIdleConns: 0}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pool_size must be >= 1")
}

func TestConfig_Validate_NegativeMinIdleConns(t *testing.T) {
	t.Parallel()
	cfg := Config{MinIdleConns: -1}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "min_idle_conns must be >= 0")
}

func TestConfig_Validate_NegativeDialTimeout(t *testing.T) {
	t.Parallel()
	cfg := Config{DialTimeout: -1 * time.Second}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dial_timeout must not be negative")
}

func TestConfig_Validate_NegativeReadTimeout(t *testing.T) {
	t.Parallel()
	cfg := Config{ReadTimeout: -1 * time.Second}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read_timeout must not be negative")
}

func TestConfig_Validate_NegativeWriteTimeout(t *testing.T) {
	t.Parallel()
	cfg := Config{WriteTimeout: -1 * time.Second}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write_timeout must not be negative")
}

func TestConfig_Validate_DefaultTimeouts(t *testing.T) {
	t.Parallel()
	cfg := Config{}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, DefaultDialTimeout, cfg.DialTimeout)
	assert.Equal(t, DefaultReadTimeout, cfg.ReadTimeout)
	assert.Equal(t, DefaultWriteTimeout, cfg.WriteTimeout)
}

// ===========================================================================
// Config.Validate Tests - URI Mode
// ===========================================================================

func TestConfig_Validate_URI_Valid(t *testing.T) {
	t.Parallel()
	cfg := Config{URI: "redis://localhost:6379/0"}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate_URI_InvalidScheme(t *testing.T) {
	t.Parallel()
	cfg := Config{URI: "mysql://localhost:3306/mydb"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URI scheme must be")
}

func TestConfig_Validate_URI_RedissScheme(t *testing.T) {
	t.Parallel()
	// The "rediss://" scheme variant (TLS) should also be accepted.
	cfg := Config{URI: "rediss://:password@localhost:6379/0"}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate_URI_SkipsStructuredValidation(t *testing.T) {
	t.Parallel()
	// When URI is set, structured fields being zero-valued should NOT cause an error.
	cfg := Config{URI: "redis://localhost:6379/0"}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate_URI_AppliesPoolDefaults(t *testing.T) {
	t.Parallel()
	cfg := Config{URI: "redis://localhost:6379/0"}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, DefaultPoolSize, cfg.PoolSize)
}

func TestConfig_Validate_URI_NoScheme(t *testing.T) {
	t.Parallel()
	cfg := Config{URI: "not-a-redis-uri"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URI scheme must be")
}

// ===========================================================================
// truncateStatement Tests
// ===========================================================================

func TestTruncateStatement_Short(t *testing.T) {
	t.Parallel()
	stmt := "SET mykey"
	assert.Equal(t, stmt, truncateStatement(stmt))
}

func TestTruncateStatement_Exact(t *testing.T) {
	t.Parallel()
	stmt := strings.Repeat("x", maxStatementTruncateLen)
	assert.Equal(t, stmt, truncateStatement(stmt))
}

func TestTruncateStatement_Long(t *testing.T) {
	t.Parallel()
	stmt := strings.Repeat("x", maxStatementTruncateLen+50)
	got := truncateStatement(stmt)
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
	stmt := strings.Repeat("\u65e5", maxStatementTruncateLen+1)
	got := truncateStatement(stmt)

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
