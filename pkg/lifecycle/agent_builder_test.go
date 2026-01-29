package lifecycle

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Builder Validation Tests
// ===========================================================================

// TestBaseAgentBuilder_Build_Valid verifies that Build succeeds with all
// required fields set.
func TestBaseAgentBuilder_Build_Valid(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if agent == nil {
		t.Fatal("Build() returned nil agent")
	}
}

// TestBaseAgentBuilder_Build_EmptyID verifies that Build returns a
// CodeValidation error when the ID is empty.
func TestBaseAgentBuilder_Build_EmptyID(t *testing.T) {
	_, err := NewBaseAgentBuilder("", "test-agent", "1.0.0").Build()
	if err == nil {
		t.Fatal("Build() expected error for empty ID, got nil")
	}
	var ssErr *sserr.Error
	if !errors.As(err, &ssErr) {
		t.Fatalf("error type = %T, want *sserr.Error", err)
	}
	if ssErr.Code != sserr.CodeValidation {
		t.Errorf("error code = %q, want %q", ssErr.Code, sserr.CodeValidation)
	}
}

// TestBaseAgentBuilder_Build_EmptyName verifies that Build returns a
// CodeValidation error when the name is empty.
func TestBaseAgentBuilder_Build_EmptyName(t *testing.T) {
	_, err := NewBaseAgentBuilder("agent-001", "", "1.0.0").Build()
	if err == nil {
		t.Fatal("Build() expected error for empty name, got nil")
	}
	if !sserr.IsValidation(err) {
		t.Errorf("IsValidation() = false, want true for empty name")
	}
}

// TestBaseAgentBuilder_Build_EmptyVersion verifies that Build returns a
// CodeValidation error when the version is empty.
func TestBaseAgentBuilder_Build_EmptyVersion(t *testing.T) {
	_, err := NewBaseAgentBuilder("agent-001", "test-agent", "").Build()
	if err == nil {
		t.Fatal("Build() expected error for empty version, got nil")
	}
	if !sserr.IsValidation(err) {
		t.Errorf("IsValidation() = false, want true for empty version")
	}
}

// TestBaseAgentBuilder_Build_DefaultLogger verifies that Build uses
// slog.Default() when no custom logger is provided.
func TestBaseAgentBuilder_Build_DefaultLogger(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if agent.logger == nil {
		t.Error("logger = nil, want slog.Default()")
	}
}

// TestBaseAgentBuilder_Build_DefaultState verifies that Build initializes
// the agent in StateUnknown.
func TestBaseAgentBuilder_Build_DefaultState(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if got := agent.State(); got != StateUnknown {
		t.Errorf("initial State() = %q, want %q", got, StateUnknown)
	}
}

// ===========================================================================
// Builder Chaining Tests
// ===========================================================================

// TestBaseAgentBuilder_Chaining verifies that all builder methods return
// the builder for fluent chaining.
func TestBaseAgentBuilder_Chaining(t *testing.T) {
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
	if err != nil {
		t.Fatalf("Build() with full chaining error: %v", err)
	}
	if agent == nil {
		t.Fatal("Build() returned nil agent with full chaining")
	}
}

// ===========================================================================
// Builder Capability Tests
// ===========================================================================

// TestBaseAgentBuilder_WithCapability verifies that a capability added via
// the builder is present in the constructed agent.
func TestBaseAgentBuilder_WithCapability(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{Name: "search", Version: "1.0.0"}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	caps := agent.Capabilities()
	if len(caps) != 1 {
		t.Fatalf("Capabilities() length = %d, want 1", len(caps))
	}
	if caps[0].Name != "search" {
		t.Errorf("caps[0].Name = %q, want %q", caps[0].Name, "search")
	}
}

// TestBaseAgentBuilder_WithCapabilities verifies that multiple capabilities
// added via WithCapabilities are present.
func TestBaseAgentBuilder_WithCapabilities(t *testing.T) {
	caps := []Capability{
		{Name: "search", Version: "1.0.0"},
		{Name: "execute", Version: "2.0.0"},
	}
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapabilities(caps).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	result := agent.Capabilities()
	if len(result) != 2 {
		t.Fatalf("Capabilities() length = %d, want 2", len(result))
	}
}

// TestBaseAgentBuilder_CapabilitiesDefensivelyCopied verifies that
// modifying the input capabilities after Build does not affect the agent.
func TestBaseAgentBuilder_CapabilitiesDefensivelyCopied(t *testing.T) {
	cap := Capability{
		Name:     "search",
		Version:  "1.0.0",
		Metadata: map[string]string{"key": "original"},
	}
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(cap).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Mutate the original capability after Build.
	cap.Metadata["key"] = "mutated"

	// The agent's internal copy should be unaffected.
	caps := agent.Capabilities()
	if caps[0].Metadata["key"] != "original" {
		t.Errorf("Metadata[key] = %q after mutation, want %q (defensive copy)",
			caps[0].Metadata["key"], "original")
	}
}

// ===========================================================================
// Builder Hook Tests
// ===========================================================================

// TestBaseAgentBuilder_WithOnStart verifies that the OnStart hook is stored
// and called during Start.
func TestBaseAgentBuilder_WithOnStart(t *testing.T) {
	var called bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStart(func(ctx context.Context) error {
			called = true
			return nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if !called {
		t.Error("OnStart hook was not called")
	}
}

// TestBaseAgentBuilder_OnStateChange verifies that a state change handler
// registered via the builder is called on state transitions.
func TestBaseAgentBuilder_OnStateChange(t *testing.T) {
	var called bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(old, new State) {
			called = true
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.SetState(StateStarting); err != nil {
		t.Fatalf("SetState() error: %v", err)
	}
	if !called {
		t.Error("state change handler was not called")
	}
}

// TestBaseAgentBuilder_MultipleStateHandlers verifies that multiple state
// change handlers are stored and called in registration order.
func TestBaseAgentBuilder_MultipleStateHandlers(t *testing.T) {
	var order []int
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(_, _ State) { order = append(order, 1) }).
		OnStateChange(func(_, _ State) { order = append(order, 2) }).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.SetState(StateStarting); err != nil {
		t.Fatalf("SetState() error: %v", err)
	}

	if len(order) != 2 {
		t.Fatalf("handler call count = %d, want 2", len(order))
	}
	if order[0] != 1 || order[1] != 2 {
		t.Errorf("handler order = %v, want [1, 2]", order)
	}
}

// TestBaseAgentBuilder_WithLogger verifies that a custom logger is used
// by the agent.
func TestBaseAgentBuilder_WithLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithLogger(logger).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if agent.logger != logger {
		t.Error("agent.logger does not match the custom logger set via WithLogger")
	}
}
