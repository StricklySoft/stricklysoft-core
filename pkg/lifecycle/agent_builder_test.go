package lifecycle

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Builder Validation Tests
// ===========================================================================

// TestBaseAgentBuilder_Build_Valid verifies that Build succeeds with all
// required fields set.
func TestBaseAgentBuilder_Build_Valid(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").Build()
	require.NoError(t, err)
	require.NotNil(t, agent)
}

// TestBaseAgentBuilder_Build_EmptyID verifies that Build returns a
// CodeValidation error when the ID is empty.
func TestBaseAgentBuilder_Build_EmptyID(t *testing.T) {
	t.Parallel()
	_, err := NewBaseAgentBuilder("", "test-agent", "1.0.0").Build()
	require.Error(t, err)
	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeValidation, ssErr.Code)
}

// TestBaseAgentBuilder_Build_EmptyName verifies that Build returns a
// CodeValidation error when the name is empty.
func TestBaseAgentBuilder_Build_EmptyName(t *testing.T) {
	t.Parallel()
	_, err := NewBaseAgentBuilder("agent-001", "", "1.0.0").Build()
	require.Error(t, err)
	assert.True(t, sserr.IsValidation(err), "IsValidation() should be true for empty name")
}

// TestBaseAgentBuilder_Build_EmptyVersion verifies that Build returns a
// CodeValidation error when the version is empty.
func TestBaseAgentBuilder_Build_EmptyVersion(t *testing.T) {
	t.Parallel()
	_, err := NewBaseAgentBuilder("agent-001", "test-agent", "").Build()
	require.Error(t, err)
	assert.True(t, sserr.IsValidation(err), "IsValidation() should be true for empty version")
}

// TestBaseAgentBuilder_Build_DefaultLogger verifies that Build uses
// slog.Default() when no custom logger is provided.
func TestBaseAgentBuilder_Build_DefaultLogger(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").Build()
	require.NoError(t, err)
	assert.NotNil(t, agent.logger)
}

// TestBaseAgentBuilder_Build_DefaultState verifies that Build initializes
// the agent in StateUnknown.
func TestBaseAgentBuilder_Build_DefaultState(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").Build()
	require.NoError(t, err)
	assert.Equal(t, StateUnknown, agent.State())
}

// ===========================================================================
// Builder Chaining Tests
// ===========================================================================

// TestBaseAgentBuilder_Chaining verifies that all builder methods return
// the builder for fluent chaining.
func TestBaseAgentBuilder_Chaining(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	hook := func(ctx context.Context) error { return nil }
	handler := func(old, new State) {}

	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{Name: "a", Version: "1.0.0"}).
		WithCapabilities([]Capability{{Name: "b", Version: "1.0.0"}}).
		WithLogger(logger).
		WithOnStart(hook).
		WithOnStop(hook).
		WithOnPause(hook).
		WithOnResume(hook).
		OnStateChange(handler).
		Build()
	require.NoError(t, err)
	require.NotNil(t, agent)
}

// ===========================================================================
// Builder Capability Tests
// ===========================================================================

// TestBaseAgentBuilder_WithCapability verifies that a capability added via
// the builder is present in the constructed agent.
func TestBaseAgentBuilder_WithCapability(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{Name: "search", Version: "1.0.0"}).
		Build()
	require.NoError(t, err)

	caps := agent.Capabilities()
	require.Len(t, caps, 1)
	assert.Equal(t, "search", caps[0].Name)
}

// TestBaseAgentBuilder_WithCapabilities verifies that multiple capabilities
// added via WithCapabilities are present.
func TestBaseAgentBuilder_WithCapabilities(t *testing.T) {
	t.Parallel()
	caps := []Capability{
		{Name: "search", Version: "1.0.0"},
		{Name: "execute", Version: "2.0.0"},
	}
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapabilities(caps).
		Build()
	require.NoError(t, err)

	result := agent.Capabilities()
	require.Len(t, result, 2)
}

// TestBaseAgentBuilder_CapabilitiesDefensivelyCopied verifies that
// modifying the input capabilities after Build does not affect the agent.
func TestBaseAgentBuilder_CapabilitiesDefensivelyCopied(t *testing.T) {
	t.Parallel()
	cap := Capability{
		Name:     "search",
		Version:  "1.0.0",
		Metadata: map[string]string{"key": "original"},
	}
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(cap).
		Build()
	require.NoError(t, err)

	// Mutate the original capability after Build.
	cap.Metadata["key"] = "mutated"

	// The agent's internal copy should be unaffected.
	caps := agent.Capabilities()
	assert.Equal(t, "original", caps[0].Metadata["key"])
}

// ===========================================================================
// Builder Hook Tests
// ===========================================================================

// TestBaseAgentBuilder_WithOnStart verifies that the OnStart hook is stored
// and called during Start.
func TestBaseAgentBuilder_WithOnStart(t *testing.T) {
	t.Parallel()
	var called bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStart(func(ctx context.Context) error {
			called = true
			return nil
		}).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.Start(context.Background()))
	assert.True(t, called, "OnStart hook was not called")
}

// TestBaseAgentBuilder_OnStateChange verifies that a state change handler
// registered via the builder is called on state transitions.
func TestBaseAgentBuilder_OnStateChange(t *testing.T) {
	t.Parallel()
	var called bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(old, new State) {
			called = true
		}).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.SetState(StateStarting))
	assert.True(t, called, "state change handler was not called")
}

// TestBaseAgentBuilder_MultipleStateHandlers verifies that multiple state
// change handlers are stored and called in registration order.
func TestBaseAgentBuilder_MultipleStateHandlers(t *testing.T) {
	t.Parallel()
	var order []int
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(_, _ State) { order = append(order, 1) }).
		OnStateChange(func(_, _ State) { order = append(order, 2) }).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.SetState(StateStarting))

	require.Len(t, order, 2)
	assert.Equal(t, []int{1, 2}, order)
}

// ===========================================================================
// Builder Capability Validation Tests
// ===========================================================================

// TestBaseAgentBuilder_Build_InvalidCapabilityEmptyName verifies that Build
// returns a CodeValidation error when a registered capability has an empty name.
func TestBaseAgentBuilder_Build_InvalidCapabilityEmptyName(t *testing.T) {
	t.Parallel()
	_, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{Name: "", Version: "1.0.0"}).
		Build()
	require.Error(t, err)
	assert.True(t, sserr.IsValidation(err), "IsValidation() should be true for empty capability name")
}

// TestBaseAgentBuilder_Build_InvalidCapabilityEmptyVersion verifies that Build
// returns a CodeValidation error when a registered capability has an empty version.
func TestBaseAgentBuilder_Build_InvalidCapabilityEmptyVersion(t *testing.T) {
	t.Parallel()
	_, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{Name: "search", Version: ""}).
		Build()
	require.Error(t, err)
	assert.True(t, sserr.IsValidation(err), "IsValidation() should be true for empty capability version")
}

// TestBaseAgentBuilder_Build_InvalidCapabilityViaWithCapabilities verifies
// that Build validates capabilities added via WithCapabilities.
func TestBaseAgentBuilder_Build_InvalidCapabilityViaWithCapabilities(t *testing.T) {
	t.Parallel()
	caps := []Capability{
		{Name: "valid", Version: "1.0.0"},
		{Name: "", Version: "1.0.0"}, // invalid
	}
	_, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapabilities(caps).
		Build()
	require.Error(t, err)
	assert.True(t, sserr.IsValidation(err), "IsValidation() should be true for invalid capability in batch")
}

// ===========================================================================
// Builder Logger Tests
// ===========================================================================

// TestBaseAgentBuilder_WithLogger verifies that a custom logger is used
// by the agent.
func TestBaseAgentBuilder_WithLogger(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithLogger(logger).
		Build()
	require.NoError(t, err)
	assert.Equal(t, logger, agent.logger)
}
