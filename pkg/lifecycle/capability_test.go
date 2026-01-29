package lifecycle

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// NewCapability Tests
// ===========================================================================

// TestNewCapability_Valid verifies that NewCapability creates a Capability
// with all fields set correctly when given valid inputs.
func TestNewCapability_Valid(t *testing.T) {
	t.Parallel()
	meta := map[string]string{"provider": "anthropic", "max_tokens": "8192"}
	cap, err := NewCapability("model-execution", "1.0.0", "Execute LLM inference", meta)
	require.NoError(t, err)
	assert.Equal(t, "model-execution", cap.Name)
	assert.Equal(t, "1.0.0", cap.Version)
	assert.Equal(t, "Execute LLM inference", cap.Description)
	assert.Len(t, cap.Metadata, 2)
	assert.Equal(t, "anthropic", cap.Metadata["provider"])
}

// TestNewCapability_NilMetadata verifies that NewCapability handles nil
// metadata gracefully, resulting in a nil Metadata field.
func TestNewCapability_NilMetadata(t *testing.T) {
	t.Parallel()
	cap, err := NewCapability("search", "1.0.0", "Web search", nil)
	require.NoError(t, err)
	assert.Nil(t, cap.Metadata)
}

// TestNewCapability_EmptyMetadata verifies that NewCapability handles an
// empty metadata map, resulting in a nil Metadata field (not an empty map).
func TestNewCapability_EmptyMetadata(t *testing.T) {
	t.Parallel()
	cap, err := NewCapability("search", "1.0.0", "Web search", map[string]string{})
	require.NoError(t, err)
	assert.Nil(t, cap.Metadata)
}

// TestNewCapability_EmptyName verifies that NewCapability returns an error
// when the name is empty.
func TestNewCapability_EmptyName(t *testing.T) {
	t.Parallel()
	_, err := NewCapability("", "1.0.0", "desc", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name must not be empty")
	assert.True(t, sserr.IsValidation(err), "IsValidation() should be true for empty name")
}

// TestNewCapability_EmptyVersion verifies that NewCapability returns an
// error when the version is empty.
func TestNewCapability_EmptyVersion(t *testing.T) {
	t.Parallel()
	_, err := NewCapability("search", "", "desc", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version must not be empty")
	assert.True(t, sserr.IsValidation(err), "IsValidation() should be true for empty version")
}

// TestNewCapability_MetadataDefensivelyCopied verifies that mutating the
// input metadata map after construction does not affect the Capability's
// internal state.
func TestNewCapability_MetadataDefensivelyCopied(t *testing.T) {
	t.Parallel()
	meta := map[string]string{"key": "original"}
	cap, err := NewCapability("search", "1.0.0", "desc", meta)
	require.NoError(t, err)

	// Mutate the original map after construction.
	meta["key"] = "mutated"
	meta["new_key"] = "injected"

	// The capability's metadata should be unaffected.
	assert.Equal(t, "original", cap.Metadata["key"])
	_, exists := cap.Metadata["new_key"]
	assert.False(t, exists, "Metadata contains injected key, want defensive copy to prevent mutation")
}

// ===========================================================================
// Capability.Clone Tests
// ===========================================================================

// TestCapability_Clone verifies that Clone returns an independent copy
// with all fields preserved, and that mutating the clone does not affect
// the original.
func TestCapability_Clone(t *testing.T) {
	t.Parallel()
	original := Capability{
		Name:        "model-execution",
		Version:     "1.0.0",
		Description: "Execute LLM inference",
		Metadata:    map[string]string{"provider": "anthropic"},
	}

	cloned := original.Clone()

	// Verify fields are preserved.
	assert.Equal(t, original.Name, cloned.Name)
	assert.Equal(t, original.Version, cloned.Version)
	assert.Equal(t, original.Description, cloned.Description)
	assert.Equal(t, "anthropic", cloned.Metadata["provider"])

	// Mutate the clone's metadata and verify the original is unaffected.
	cloned.Metadata["provider"] = "openai"
	cloned.Metadata["new_key"] = "injected"

	assert.Equal(t, "anthropic", original.Metadata["provider"])
	_, exists := original.Metadata["new_key"]
	assert.False(t, exists, "original.Metadata contains key injected into clone")
}

// TestCapability_Clone_NilMetadata verifies that Clone handles a
// Capability with nil Metadata.
func TestCapability_Clone_NilMetadata(t *testing.T) {
	t.Parallel()
	original := Capability{Name: "test", Version: "1.0.0"}
	cloned := original.Clone()
	assert.Nil(t, cloned.Metadata)
}

// ===========================================================================
// JSON Serialization Tests
// ===========================================================================

// TestCapability_JSONRoundTrip verifies that a Capability can be marshaled
// to JSON and unmarshaled back with all fields preserved.
func TestCapability_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	original := Capability{
		Name:        "policy-evaluation",
		Version:     "2.1.0",
		Description: "Evaluate security policies",
		Metadata:    map[string]string{"engine": "opa", "version": "0.60"},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Capability
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.Version, restored.Version)
	assert.Equal(t, original.Description, restored.Description)
	assert.Equal(t, "opa", restored.Metadata["engine"])
}

// TestCapability_JSONOmitsEmptyMetadata verifies that the Metadata field
// is omitted from JSON output when it is nil (due to the omitempty tag).
func TestCapability_JSONOmitsEmptyMetadata(t *testing.T) {
	t.Parallel()
	cap := Capability{
		Name:    "test",
		Version: "1.0.0",
	}

	data, err := json.Marshal(cap)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "metadata")
}
