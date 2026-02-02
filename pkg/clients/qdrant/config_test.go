package qdrant

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
	s := Secret("super-secret-api-key")
	assert.Equal(t, "[REDACTED]", s.String())
}

func TestSecret_GoString_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-api-key")
	assert.Equal(t, "[REDACTED]", s.GoString())
}

func TestSecret_Value_ReturnsActualValue(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-api-key")
	assert.Equal(t, "super-secret-api-key", s.Value())
}

func TestSecret_MarshalText_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-api-key")
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
	assert.Equal(t, DefaultGRPCPort, cfg.GRPCPort)
	assert.Equal(t, DefaultUseTLS, cfg.UseTLS)
	assert.Equal(t, DefaultHealthTimeout, cfg.HealthTimeout)
}

// ===========================================================================
// Config.Validate Tests
// ===========================================================================

func TestConfig_Validate_MinimalValid(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Host: "localhost",
	}
	require.NoError(t, cfg.Validate())
	// Defaults should be applied for zero-valued fields.
	assert.Equal(t, DefaultGRPCPort, cfg.GRPCPort)
	assert.Equal(t, DefaultHealthTimeout, cfg.HealthTimeout)
}

func TestConfig_Validate_FullySpecified(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Host:          "qdrant.example.com",
		GRPCPort:      6335,
		APIKey:        Secret("test-key"),
		UseTLS:        true,
		HealthTimeout: 10 * time.Second,
	}
	require.NoError(t, cfg.Validate())
	// Specified values should be preserved (not overwritten by defaults).
	assert.Equal(t, "qdrant.example.com", cfg.Host)
	assert.Equal(t, 6335, cfg.GRPCPort)
	assert.Equal(t, 10*time.Second, cfg.HealthTimeout)
}

func TestConfig_Validate_EmptyHost(t *testing.T) {
	t.Parallel()
	cfg := Config{}
	require.NoError(t, cfg.Validate())
	// Empty host should default to DefaultHost.
	assert.Equal(t, DefaultHost, cfg.Host)
}

func TestConfig_Validate_InvalidPort_Negative(t *testing.T) {
	t.Parallel()
	cfg := Config{Host: "localhost", GRPCPort: -1}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "grpc_port must be between")
}

func TestConfig_Validate_InvalidPort_TooHigh(t *testing.T) {
	t.Parallel()
	cfg := Config{Host: "localhost", GRPCPort: 70000}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "grpc_port must be between")
}

func TestConfig_Validate_NegativeHealthTimeout(t *testing.T) {
	t.Parallel()
	cfg := Config{Host: "localhost", HealthTimeout: -1 * time.Second}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "health_timeout must not be negative")
}

// ===========================================================================
// Config.GRPCAddress Tests
// ===========================================================================

func TestConfig_GRPCAddress(t *testing.T) {
	t.Parallel()
	cfg := Config{Host: "qdrant.example.com", GRPCPort: 6334}
	assert.Equal(t, "qdrant.example.com:6334", cfg.GRPCAddress())
}

func TestConfig_GRPCAddress_DefaultValues(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	assert.Equal(t, "qdrant.databases.svc.cluster.local:6334", cfg.GRPCAddress())
}

// ===========================================================================
// truncateStatement Tests
// ===========================================================================

func TestTruncateStatement_Short(t *testing.T) {
	t.Parallel()
	stmt := "Upsert my_collection (3 points)"
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
