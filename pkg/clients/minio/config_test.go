package minio

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// Secret Type Tests
// ===========================================================================

func TestSecret_String_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-key")
	assert.Equal(t, "[REDACTED]", s.String())
}

func TestSecret_GoString_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-key")
	assert.Equal(t, "[REDACTED]", s.GoString())
}

func TestSecret_Value_ReturnsActualValue(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-key")
	assert.Equal(t, "super-secret-key", s.Value())
}

func TestSecret_MarshalText_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-key")
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

	assert.Equal(t, DefaultEndpoint, cfg.Endpoint)
	assert.Equal(t, DefaultRegion, cfg.Region)
	assert.Equal(t, DefaultUseSSL, cfg.UseSSL)
	assert.Equal(t, "", cfg.AccessKey)
	assert.Equal(t, Secret(""), cfg.SecretKey)
	assert.Equal(t, "", cfg.HealthBucket)
}

// ===========================================================================
// Config.Validate Tests
// ===========================================================================

func TestConfig_Validate_MinimalValid(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Endpoint:  "localhost:9000",
		AccessKey: "myaccesskey",
	}
	require.NoError(t, cfg.Validate())
	// Default region should be applied.
	assert.Equal(t, DefaultRegion, cfg.Region)
}

func TestConfig_Validate_FullySpecified(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Endpoint:     "minio.example.com:9000",
		AccessKey:    "admin",
		SecretKey:    Secret("secretpass"),
		Region:       "eu-west-1",
		UseSSL:       true,
		HealthBucket: "my-health-bucket",
	}
	require.NoError(t, cfg.Validate())
	// Specified values should be preserved (not overwritten by defaults).
	assert.Equal(t, "minio.example.com:9000", cfg.Endpoint)
	assert.Equal(t, "eu-west-1", cfg.Region)
	assert.True(t, cfg.UseSSL)
}

func TestConfig_Validate_EmptyEndpoint(t *testing.T) {
	t.Parallel()
	cfg := Config{AccessKey: "mykey"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint must not be empty")
}

func TestConfig_Validate_EmptyAccessKey(t *testing.T) {
	t.Parallel()
	cfg := Config{Endpoint: "localhost:9000"}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_key must not be empty")
}

func TestConfig_Validate_DefaultRegion(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Endpoint:  "localhost:9000",
		AccessKey: "mykey",
	}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, DefaultRegion, cfg.Region)
}

// ===========================================================================
// truncateStatement Tests
// ===========================================================================

func TestTruncateStatement_Short(t *testing.T) {
	t.Parallel()
	stmt := "PUT mybucket/mykey"
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
	stmt := strings.Repeat("日", maxStatementTruncateLen+1)
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
