package lifecycle

import (
	"encoding/json"
	"strings"
	"testing"
)

// ===========================================================================
// NewCapability Tests
// ===========================================================================

// TestNewCapability_Valid verifies that NewCapability creates a Capability
// with all fields set correctly when given valid inputs.
func TestNewCapability_Valid(t *testing.T) {
	meta := map[string]string{"provider": "anthropic", "max_tokens": "8192"}
	cap, err := NewCapability("model-execution", "1.0.0", "Execute LLM inference", meta)
	if err != nil {
		t.Fatalf("NewCapability() error: %v", err)
	}
	if cap.Name != "model-execution" {
		t.Errorf("Name = %q, want %q", cap.Name, "model-execution")
	}
	if cap.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", cap.Version, "1.0.0")
	}
	if cap.Description != "Execute LLM inference" {
		t.Errorf("Description = %q, want %q", cap.Description, "Execute LLM inference")
	}
	if len(cap.Metadata) != 2 {
		t.Errorf("Metadata length = %d, want 2", len(cap.Metadata))
	}
	if cap.Metadata["provider"] != "anthropic" {
		t.Errorf("Metadata[provider] = %q, want %q", cap.Metadata["provider"], "anthropic")
	}
}

// TestNewCapability_NilMetadata verifies that NewCapability handles nil
// metadata gracefully, resulting in a nil Metadata field.
func TestNewCapability_NilMetadata(t *testing.T) {
	cap, err := NewCapability("search", "1.0.0", "Web search", nil)
	if err != nil {
		t.Fatalf("NewCapability() error: %v", err)
	}
	if cap.Metadata != nil {
		t.Errorf("Metadata = %v, want nil for nil input", cap.Metadata)
	}
}

// TestNewCapability_EmptyMetadata verifies that NewCapability handles an
// empty metadata map, resulting in a nil Metadata field (not an empty map).
func TestNewCapability_EmptyMetadata(t *testing.T) {
	cap, err := NewCapability("search", "1.0.0", "Web search", map[string]string{})
	if err != nil {
		t.Fatalf("NewCapability() error: %v", err)
	}
	if cap.Metadata != nil {
		t.Errorf("Metadata = %v, want nil for empty input", cap.Metadata)
	}
}

// TestNewCapability_EmptyName verifies that NewCapability returns an error
// when the name is empty.
func TestNewCapability_EmptyName(t *testing.T) {
	_, err := NewCapability("", "1.0.0", "desc", nil)
	if err == nil {
		t.Fatal("NewCapability() expected error for empty name, got nil")
	}
	if !strings.Contains(err.Error(), "name must not be empty") {
		t.Errorf("error = %q, want message about empty name", err.Error())
	}
}

// TestNewCapability_EmptyVersion verifies that NewCapability returns an
// error when the version is empty.
func TestNewCapability_EmptyVersion(t *testing.T) {
	_, err := NewCapability("search", "", "desc", nil)
	if err == nil {
		t.Fatal("NewCapability() expected error for empty version, got nil")
	}
	if !strings.Contains(err.Error(), "version must not be empty") {
		t.Errorf("error = %q, want message about empty version", err.Error())
	}
}

// TestNewCapability_MetadataDefensivelyCopied verifies that mutating the
// input metadata map after construction does not affect the Capability's
// internal state.
func TestNewCapability_MetadataDefensivelyCopied(t *testing.T) {
	meta := map[string]string{"key": "original"}
	cap, err := NewCapability("search", "1.0.0", "desc", meta)
	if err != nil {
		t.Fatalf("NewCapability() error: %v", err)
	}

	// Mutate the original map after construction.
	meta["key"] = "mutated"
	meta["new_key"] = "injected"

	// The capability's metadata should be unaffected.
	if cap.Metadata["key"] != "original" {
		t.Errorf("Metadata[key] = %q, want %q (original should be preserved)",
			cap.Metadata["key"], "original")
	}
	if _, exists := cap.Metadata["new_key"]; exists {
		t.Error("Metadata contains injected key, want defensive copy to prevent mutation")
	}
}

// ===========================================================================
// Capability.Clone Tests
// ===========================================================================

// TestCapability_Clone verifies that Clone returns an independent copy
// with all fields preserved, and that mutating the clone does not affect
// the original.
func TestCapability_Clone(t *testing.T) {
	original := Capability{
		Name:        "model-execution",
		Version:     "1.0.0",
		Description: "Execute LLM inference",
		Metadata:    map[string]string{"provider": "anthropic"},
	}

	cloned := original.Clone()

	// Verify fields are preserved.
	if cloned.Name != original.Name {
		t.Errorf("Clone().Name = %q, want %q", cloned.Name, original.Name)
	}
	if cloned.Version != original.Version {
		t.Errorf("Clone().Version = %q, want %q", cloned.Version, original.Version)
	}
	if cloned.Description != original.Description {
		t.Errorf("Clone().Description = %q, want %q", cloned.Description, original.Description)
	}
	if cloned.Metadata["provider"] != "anthropic" {
		t.Errorf("Clone().Metadata[provider] = %q, want %q",
			cloned.Metadata["provider"], "anthropic")
	}

	// Mutate the clone's metadata and verify the original is unaffected.
	cloned.Metadata["provider"] = "openai"
	cloned.Metadata["new_key"] = "injected"

	if original.Metadata["provider"] != "anthropic" {
		t.Errorf("original.Metadata[provider] = %q after clone mutation, want %q",
			original.Metadata["provider"], "anthropic")
	}
	if _, exists := original.Metadata["new_key"]; exists {
		t.Error("original.Metadata contains key injected into clone")
	}
}

// TestCapability_Clone_NilMetadata verifies that Clone handles a
// Capability with nil Metadata.
func TestCapability_Clone_NilMetadata(t *testing.T) {
	original := Capability{Name: "test", Version: "1.0.0"}
	cloned := original.Clone()
	if cloned.Metadata != nil {
		t.Errorf("Clone().Metadata = %v, want nil", cloned.Metadata)
	}
}

// ===========================================================================
// JSON Serialization Tests
// ===========================================================================

// TestCapability_JSONRoundTrip verifies that a Capability can be marshaled
// to JSON and unmarshaled back with all fields preserved.
func TestCapability_JSONRoundTrip(t *testing.T) {
	original := Capability{
		Name:        "policy-evaluation",
		Version:     "2.1.0",
		Description: "Evaluate security policies",
		Metadata:    map[string]string{"engine": "opa", "version": "0.60"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var restored Capability
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if restored.Name != original.Name {
		t.Errorf("Name = %q, want %q", restored.Name, original.Name)
	}
	if restored.Version != original.Version {
		t.Errorf("Version = %q, want %q", restored.Version, original.Version)
	}
	if restored.Description != original.Description {
		t.Errorf("Description = %q, want %q", restored.Description, original.Description)
	}
	if restored.Metadata["engine"] != "opa" {
		t.Errorf("Metadata[engine] = %q, want %q", restored.Metadata["engine"], "opa")
	}
}

// TestCapability_JSONOmitsEmptyMetadata verifies that the Metadata field
// is omitted from JSON output when it is nil (due to the omitempty tag).
func TestCapability_JSONOmitsEmptyMetadata(t *testing.T) {
	cap := Capability{
		Name:    "test",
		Version: "1.0.0",
	}

	data, err := json.Marshal(cap)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	jsonStr := string(data)
	if strings.Contains(jsonStr, "metadata") {
		t.Errorf("JSON = %s, want metadata field omitted when nil", jsonStr)
	}
}
