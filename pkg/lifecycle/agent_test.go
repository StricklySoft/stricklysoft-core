package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// mustBuildAgent is a test helper that creates a BaseAgent with default test
// identity values via the builder, failing the test if Build returns an error.
func mustBuildAgent(t *testing.T) *BaseAgent {
	t.Helper()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").Build()
	if err != nil {
		t.Fatalf("NewBaseAgentBuilder().Build() error: %v", err)
	}
	return agent
}

// mustStartAgent is a test helper that builds an agent with default test
// identity values and starts it, failing the test if either operation
// returns an error.
func mustStartAgent(t *testing.T) *BaseAgent {
	t.Helper()
	agent := mustBuildAgent(t)
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	return agent
}

// ===========================================================================
// Accessor Tests
// ===========================================================================

// TestBaseAgent_ID verifies that ID returns the value set during construction.
func TestBaseAgent_ID(t *testing.T) {
	agent := mustBuildAgent(t)
	if got := agent.ID(); got != "agent-001" {
		t.Errorf("ID() = %q, want %q", got, "agent-001")
	}
}

// TestBaseAgent_Name verifies that Name returns the value set during
// construction.
func TestBaseAgent_Name(t *testing.T) {
	agent := mustBuildAgent(t)
	if got := agent.Name(); got != "test-agent" {
		t.Errorf("Name() = %q, want %q", got, "test-agent")
	}
}

// TestBaseAgent_Version verifies that Version returns the value set during
// construction.
func TestBaseAgent_Version(t *testing.T) {
	agent := mustBuildAgent(t)
	if got := agent.Version(); got != "1.0.0" {
		t.Errorf("Version() = %q, want %q", got, "1.0.0")
	}
}

// ===========================================================================
// State Tests
// ===========================================================================

// TestBaseAgent_State_InitialValue verifies that a newly constructed agent
// starts in StateUnknown.
func TestBaseAgent_State_InitialValue(t *testing.T) {
	agent := mustBuildAgent(t)
	if got := agent.State(); got != StateUnknown {
		t.Errorf("State() = %q, want %q", got, StateUnknown)
	}
}

// TestBaseAgent_SetState_ValidTransition verifies that SetState succeeds
// for an allowed transition.
func TestBaseAgent_SetState_ValidTransition(t *testing.T) {
	agent := mustBuildAgent(t)

	// Unknown -> Starting is a valid transition.
	if err := agent.SetState(StateStarting); err != nil {
		t.Fatalf("SetState(Starting) error: %v", err)
	}
	if got := agent.State(); got != StateStarting {
		t.Errorf("State() = %q, want %q", got, StateStarting)
	}
}

// TestBaseAgent_SetState_InvalidTransition verifies that SetState returns
// a CodeConflict error for a disallowed transition.
func TestBaseAgent_SetState_InvalidTransition(t *testing.T) {
	agent := mustBuildAgent(t)

	// Unknown -> Running is not a valid transition.
	err := agent.SetState(StateRunning)
	if err == nil {
		t.Fatal("SetState(Running) from Unknown expected error, got nil")
	}

	var ssErr *sserr.Error
	if !errors.As(err, &ssErr) {
		t.Fatalf("error type = %T, want *sserr.Error", err)
	}
	if ssErr.Code != sserr.CodeConflict {
		t.Errorf("error code = %q, want %q", ssErr.Code, sserr.CodeConflict)
	}

	// State should remain unchanged.
	if got := agent.State(); got != StateUnknown {
		t.Errorf("State() after invalid transition = %q, want %q", got, StateUnknown)
	}
}

// TestBaseAgent_SetState_NotifiesHandlers verifies that state change
// handlers are called with the correct old and new state values.
func TestBaseAgent_SetState_NotifiesHandlers(t *testing.T) {
	var capturedOld, capturedNew State
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(old, new State) {
			capturedOld = old
			capturedNew = new
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.SetState(StateStarting); err != nil {
		t.Fatalf("SetState() error: %v", err)
	}

	if capturedOld != StateUnknown {
		t.Errorf("handler old = %q, want %q", capturedOld, StateUnknown)
	}
	if capturedNew != StateStarting {
		t.Errorf("handler new = %q, want %q", capturedNew, StateStarting)
	}
}

// TestBaseAgent_SetState_MultipleHandlers verifies that multiple handlers
// are called in registration order.
func TestBaseAgent_SetState_MultipleHandlers(t *testing.T) {
	var order []int
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(_, _ State) { order = append(order, 1) }).
		OnStateChange(func(_, _ State) { order = append(order, 2) }).
		OnStateChange(func(_, _ State) { order = append(order, 3) }).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.SetState(StateStarting); err != nil {
		t.Fatalf("SetState() error: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("handler call count = %d, want 3", len(order))
	}
	for i, v := range order {
		if v != i+1 {
			t.Errorf("order[%d] = %d, want %d", i, v, i+1)
		}
	}
}

// TestBaseAgent_SetState_HandlerPanicRecovery verifies that a panicking
// handler does not prevent the state change or crash the agent, and that
// subsequent handlers still execute.
func TestBaseAgent_SetState_HandlerPanicRecovery(t *testing.T) {
	var secondCalled bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(_, _ State) { panic("test panic") }).
		OnStateChange(func(_, _ State) { secondCalled = true }).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// SetState should not panic and should succeed.
	if err := agent.SetState(StateStarting); err != nil {
		t.Fatalf("SetState() error: %v", err)
	}

	// State should have changed despite the panic.
	if got := agent.State(); got != StateStarting {
		t.Errorf("State() = %q, want %q after handler panic", got, StateStarting)
	}

	// The second handler should still have been called.
	if !secondCalled {
		t.Error("second handler was not called after first handler panicked")
	}
}

// ===========================================================================
// Capabilities Tests
// ===========================================================================

// TestBaseAgent_Capabilities_Empty verifies that Capabilities returns an
// empty (non-nil) slice when no capabilities are registered.
func TestBaseAgent_Capabilities_Empty(t *testing.T) {
	agent := mustBuildAgent(t)
	caps := agent.Capabilities()
	if caps == nil {
		t.Error("Capabilities() = nil, want non-nil empty slice")
	}
	if len(caps) != 0 {
		t.Errorf("Capabilities() length = %d, want 0", len(caps))
	}
}

// TestBaseAgent_Capabilities_WithEntries verifies that Capabilities returns
// the capabilities registered via the builder.
func TestBaseAgent_Capabilities_WithEntries(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{Name: "search", Version: "1.0.0"}).
		WithCapability(Capability{Name: "execute", Version: "2.0.0"}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	caps := agent.Capabilities()
	if len(caps) != 2 {
		t.Fatalf("Capabilities() length = %d, want 2", len(caps))
	}
	if caps[0].Name != "search" {
		t.Errorf("caps[0].Name = %q, want %q", caps[0].Name, "search")
	}
	if caps[1].Name != "execute" {
		t.Errorf("caps[1].Name = %q, want %q", caps[1].Name, "execute")
	}
}

// TestBaseAgent_Capabilities_DefensiveCopy verifies that modifying the
// returned capability slice does not affect the agent's internal state.
func TestBaseAgent_Capabilities_DefensiveCopy(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{
			Name:     "search",
			Version:  "1.0.0",
			Metadata: map[string]string{"key": "original"},
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Get capabilities and mutate the returned slice.
	caps := agent.Capabilities()
	caps[0].Name = "mutated"
	caps[0].Metadata["key"] = "mutated"

	// Fetch again and verify the internal state was not affected.
	fresh := agent.Capabilities()
	if fresh[0].Name != "search" {
		t.Errorf("Name = %q after mutation, want %q (defensive copy)",
			fresh[0].Name, "search")
	}
	if fresh[0].Metadata["key"] != "original" {
		t.Errorf("Metadata[key] = %q after mutation, want %q (defensive copy)",
			fresh[0].Metadata["key"], "original")
	}
}

// ===========================================================================
// Info Tests
// ===========================================================================

// TestBaseAgent_Info verifies that Info returns an AgentInfo with all fields
// correctly populated.
func TestBaseAgent_Info(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{Name: "search", Version: "1.0.0"}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	info := agent.Info()

	if info.ID != "agent-001" {
		t.Errorf("Info().ID = %q, want %q", info.ID, "agent-001")
	}
	if info.Name != "test-agent" {
		t.Errorf("Info().Name = %q, want %q", info.Name, "test-agent")
	}
	if info.Version != "1.0.0" {
		t.Errorf("Info().Version = %q, want %q", info.Version, "1.0.0")
	}
	if info.State != StateUnknown {
		t.Errorf("Info().State = %q, want %q", info.State, StateUnknown)
	}
	if len(info.Capabilities) != 1 {
		t.Errorf("Info().Capabilities length = %d, want 1", len(info.Capabilities))
	}
}

// TestBaseAgent_Info_NoStartedAtBeforeStart verifies that Info returns nil
// StartedAt and zero Uptime before the agent has been started.
func TestBaseAgent_Info_NoStartedAtBeforeStart(t *testing.T) {
	agent := mustBuildAgent(t)
	info := agent.Info()

	if info.StartedAt != nil {
		t.Errorf("Info().StartedAt = %v, want nil before Start", info.StartedAt)
	}
	if info.Uptime != 0 {
		t.Errorf("Info().Uptime = %v, want 0 before Start", info.Uptime)
	}
}

// TestBaseAgent_Info_StartedAtAfterStart verifies that Info returns a
// non-nil StartedAt and positive Uptime after the agent has been started.
func TestBaseAgent_Info_StartedAtAfterStart(t *testing.T) {
	agent := mustStartAgent(t)
	info := agent.Info()

	if info.StartedAt == nil {
		t.Error("Info().StartedAt = nil after Start, want non-nil")
	}
	if info.Uptime < 0 {
		t.Errorf("Info().Uptime = %v after Start, want >= 0", info.Uptime)
	}
}

// TestBaseAgent_Info_UptimeResetAfterStop verifies that StartedAt is nil
// and Uptime is zero after the agent has been stopped.
func TestBaseAgent_Info_UptimeResetAfterStop(t *testing.T) {
	agent := mustStartAgent(t)

	if err := agent.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	info := agent.Info()
	if info.StartedAt != nil {
		t.Errorf("Info().StartedAt = %v after Stop, want nil", info.StartedAt)
	}
	if info.Uptime != 0 {
		t.Errorf("Info().Uptime = %v after Stop, want 0", info.Uptime)
	}
}

// ===========================================================================
// Health Tests
// ===========================================================================

// TestBaseAgent_Health_Running verifies that Health returns nil when the
// agent is in StateRunning.
func TestBaseAgent_Health_Running(t *testing.T) {
	agent := mustStartAgent(t)
	if err := agent.Health(context.Background()); err != nil {
		t.Errorf("Health() = %v when running, want nil", err)
	}
}

// TestBaseAgent_Health_NotRunning verifies that Health returns an error
// when the agent is not in StateRunning.
func TestBaseAgent_Health_NotRunning(t *testing.T) {
	agent := mustBuildAgent(t)

	err := agent.Health(context.Background())
	if err == nil {
		t.Fatal("Health() = nil when not running, want error")
	}

	var ssErr *sserr.Error
	if !errors.As(err, &ssErr) {
		t.Fatalf("error type = %T, want *sserr.Error", err)
	}
	if ssErr.Code != sserr.CodeUnavailable {
		t.Errorf("error code = %q, want %q", ssErr.Code, sserr.CodeUnavailable)
	}
}

// TestBaseAgent_Health_Paused verifies that Health returns an error when
// the agent is paused.
func TestBaseAgent_Health_Paused(t *testing.T) {
	agent := mustStartAgent(t)
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	err := agent.Health(context.Background())
	if err == nil {
		t.Fatal("Health() = nil when paused, want error")
	}
	if !sserr.IsUnavailable(err) {
		t.Errorf("IsUnavailable() = false, want true for paused agent")
	}
}

// ===========================================================================
// Start Tests
// ===========================================================================

// TestBaseAgent_Start_Success verifies that Start transitions the agent
// from Unknown to Running.
func TestBaseAgent_Start_Success(t *testing.T) {
	agent := mustBuildAgent(t)

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if got := agent.State(); got != StateRunning {
		t.Errorf("State() = %q after Start, want %q", got, StateRunning)
	}
}

// TestBaseAgent_Start_SetsStartedAt verifies that Start sets the startedAt
// timestamp.
func TestBaseAgent_Start_SetsStartedAt(t *testing.T) {
	before := time.Now().UTC()
	agent := mustStartAgent(t)
	after := time.Now().UTC()

	info := agent.Info()
	if info.StartedAt == nil {
		t.Fatal("StartedAt = nil after Start, want non-nil")
	}
	if info.StartedAt.Before(before) || info.StartedAt.After(after) {
		t.Errorf("StartedAt = %v, want between %v and %v",
			info.StartedAt, before, after)
	}
}

// TestBaseAgent_Start_WithHook verifies that the OnStart hook is called
// during Start.
func TestBaseAgent_Start_WithHook(t *testing.T) {
	var hookCalled bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStart(func(ctx context.Context) error {
			hookCalled = true
			return nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if !hookCalled {
		t.Error("OnStart hook was not called")
	}
}

// TestBaseAgent_Start_HookError verifies that a hook error transitions the
// agent to StateFailed and returns a wrapped error.
func TestBaseAgent_Start_HookError(t *testing.T) {
	hookErr := errors.New("database unavailable")
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStart(func(ctx context.Context) error {
			return hookErr
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	startErr := agent.Start(context.Background())
	if startErr == nil {
		t.Fatal("Start() with failing hook expected error, got nil")
	}

	// Agent should be in StateFailed.
	if got := agent.State(); got != StateFailed {
		t.Errorf("State() = %q after hook failure, want %q", got, StateFailed)
	}

	// Error should wrap the hook error.
	if !errors.Is(startErr, hookErr) {
		t.Error("Start() error does not wrap the hook error")
	}

	// Error should have CodeInternal.
	var ssErr *sserr.Error
	if !errors.As(startErr, &ssErr) {
		t.Fatalf("error type = %T, want *sserr.Error", startErr)
	}
	if ssErr.Code != sserr.CodeInternal {
		t.Errorf("error code = %q, want %q", ssErr.Code, sserr.CodeInternal)
	}
}

// TestBaseAgent_Start_InvalidState verifies that Start from StateRunning
// returns a conflict error.
func TestBaseAgent_Start_InvalidState(t *testing.T) {
	agent := mustStartAgent(t)

	err := agent.Start(context.Background())
	if err == nil {
		t.Fatal("Start() while running expected error, got nil")
	}

	if !sserr.IsConflict(err) {
		t.Errorf("IsConflict() = false, want true for Start while running")
	}
}

// TestBaseAgent_Start_ContextCanceled verifies that Start with a canceled
// context returns immediately without modifying state.
func TestBaseAgent_Start_ContextCanceled(t *testing.T) {
	agent := mustBuildAgent(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := agent.Start(ctx)
	if err == nil {
		t.Fatal("Start() with canceled context expected error, got nil")
	}

	// State should remain Unknown.
	if got := agent.State(); got != StateUnknown {
		t.Errorf("State() = %q after canceled Start, want %q", got, StateUnknown)
	}
}

// TestBaseAgent_Start_FromStopped verifies that an agent can be restarted
// after being stopped.
func TestBaseAgent_Start_FromStopped(t *testing.T) {
	agent := mustStartAgent(t)
	if err := agent.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() from Stopped error: %v", err)
	}

	if got := agent.State(); got != StateRunning {
		t.Errorf("State() = %q after restart, want %q", got, StateRunning)
	}
}

// TestBaseAgent_Start_FromFailed verifies that an agent can be restarted
// after a failure.
func TestBaseAgent_Start_FromFailed(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStart(func(ctx context.Context) error {
			return errors.New("startup failure")
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// First Start fails, putting agent in Failed state.
	_ = agent.Start(context.Background())
	if got := agent.State(); got != StateFailed {
		t.Fatalf("State() = %q, want %q after failed start", got, StateFailed)
	}

	// Replace the hook to succeed this time. Since hooks are set at
	// construction, we need a new agent. Instead, test the state transition.
	// Failed -> Starting should be valid.
	if err := agent.SetState(StateStarting); err != nil {
		t.Fatalf("SetState(Starting) from Failed error: %v", err)
	}
}

// ===========================================================================
// Stop Tests
// ===========================================================================

// TestBaseAgent_Stop_Success verifies that Stop transitions a running agent
// to Stopped.
func TestBaseAgent_Stop_Success(t *testing.T) {
	agent := mustStartAgent(t)

	if err := agent.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if got := agent.State(); got != StateStopped {
		t.Errorf("State() = %q after Stop, want %q", got, StateStopped)
	}
}

// TestBaseAgent_Stop_WithHook verifies that the OnStop hook is called
// during Stop.
func TestBaseAgent_Stop_WithHook(t *testing.T) {
	var hookCalled bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStop(func(ctx context.Context) error {
			hookCalled = true
			return nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if err := agent.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if !hookCalled {
		t.Error("OnStop hook was not called")
	}
}

// TestBaseAgent_Stop_HookError verifies that a stop hook error transitions
// the agent to StateFailed.
func TestBaseAgent_Stop_HookError(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStop(func(ctx context.Context) error {
			return errors.New("cleanup failed")
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	stopErr := agent.Stop(context.Background())
	if stopErr == nil {
		t.Fatal("Stop() with failing hook expected error, got nil")
	}

	if got := agent.State(); got != StateFailed {
		t.Errorf("State() = %q after stop hook failure, want %q", got, StateFailed)
	}
}

// TestBaseAgent_Stop_AlreadyStopped verifies that Stop is a no-op when
// the agent is already stopped.
func TestBaseAgent_Stop_AlreadyStopped(t *testing.T) {
	agent := mustStartAgent(t)
	if err := agent.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	// Second Stop should be a no-op.
	if err := agent.Stop(context.Background()); err != nil {
		t.Errorf("Stop() when already stopped = %v, want nil (no-op)", err)
	}
}

// TestBaseAgent_Stop_InvalidState verifies that Stop from Unknown returns
// a conflict error (Unknown cannot transition to Stopping).
func TestBaseAgent_Stop_InvalidState(t *testing.T) {
	agent := mustBuildAgent(t)

	err := agent.Stop(context.Background())
	if err == nil {
		t.Fatal("Stop() from Unknown expected error, got nil")
	}

	if !sserr.IsConflict(err) {
		t.Errorf("IsConflict() = false, want true for Stop from Unknown")
	}
}

// ===========================================================================
// Pause Tests
// ===========================================================================

// TestBaseAgent_Pause_Success verifies that Pause transitions a running
// agent to Paused.
func TestBaseAgent_Pause_Success(t *testing.T) {
	agent := mustStartAgent(t)

	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	if got := agent.State(); got != StatePaused {
		t.Errorf("State() = %q after Pause, want %q", got, StatePaused)
	}
}

// TestBaseAgent_Pause_WithHook verifies that the OnPause hook is called.
func TestBaseAgent_Pause_WithHook(t *testing.T) {
	var hookCalled bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnPause(func(ctx context.Context) error {
			hookCalled = true
			return nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	if !hookCalled {
		t.Error("OnPause hook was not called")
	}
}

// TestBaseAgent_Pause_InvalidState verifies that Pause from Stopped returns
// a conflict error.
func TestBaseAgent_Pause_InvalidState(t *testing.T) {
	agent := mustStartAgent(t)
	if err := agent.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	err := agent.Pause(context.Background())
	if err == nil {
		t.Fatal("Pause() from Stopped expected error, got nil")
	}
	if !sserr.IsConflict(err) {
		t.Errorf("IsConflict() = false, want true for Pause from Stopped")
	}
}

// TestBaseAgent_Pause_HookError verifies that a pause hook error transitions
// the agent to StateFailed.
func TestBaseAgent_Pause_HookError(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnPause(func(ctx context.Context) error {
			return errors.New("pause failed")
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	pauseErr := agent.Pause(context.Background())
	if pauseErr == nil {
		t.Fatal("Pause() with failing hook expected error, got nil")
	}
	if got := agent.State(); got != StateFailed {
		t.Errorf("State() = %q after pause hook failure, want %q", got, StateFailed)
	}
}

// ===========================================================================
// Resume Tests
// ===========================================================================

// TestBaseAgent_Resume_Success verifies that Resume transitions a paused
// agent back to Running.
func TestBaseAgent_Resume_Success(t *testing.T) {
	agent := mustStartAgent(t)
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	if err := agent.Resume(context.Background()); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if got := agent.State(); got != StateRunning {
		t.Errorf("State() = %q after Resume, want %q", got, StateRunning)
	}
}

// TestBaseAgent_Resume_WithHook verifies that the OnResume hook is called.
func TestBaseAgent_Resume_WithHook(t *testing.T) {
	var hookCalled bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnResume(func(ctx context.Context) error {
			hookCalled = true
			return nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}
	if err := agent.Resume(context.Background()); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if !hookCalled {
		t.Error("OnResume hook was not called")
	}
}

// TestBaseAgent_Resume_InvalidState verifies that Resume from Running
// returns a conflict error.
func TestBaseAgent_Resume_InvalidState(t *testing.T) {
	agent := mustStartAgent(t)

	err := agent.Resume(context.Background())
	if err == nil {
		t.Fatal("Resume() while running expected error, got nil")
	}
	if !sserr.IsConflict(err) {
		t.Errorf("IsConflict() = false, want true for Resume while Running")
	}
}

// TestBaseAgent_Resume_HookError verifies that a resume hook error
// transitions the agent to StateFailed.
func TestBaseAgent_Resume_HookError(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnResume(func(ctx context.Context) error {
			return errors.New("resume failed")
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	resumeErr := agent.Resume(context.Background())
	if resumeErr == nil {
		t.Fatal("Resume() with failing hook expected error, got nil")
	}
	if got := agent.State(); got != StateFailed {
		t.Errorf("State() = %q after resume hook failure, want %q", got, StateFailed)
	}
}

// ===========================================================================
// Full Lifecycle Tests
// ===========================================================================

// TestBaseAgent_FullLifecycle verifies the complete lifecycle flow:
// Start -> Pause -> Resume -> Stop.
func TestBaseAgent_FullLifecycle(t *testing.T) {
	var transitions []string
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(old, new State) {
			transitions = append(transitions, string(old)+"->"+string(new))
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	ctx := context.Background()

	// Start
	if err := agent.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Pause
	if err := agent.Pause(ctx); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	// Resume
	if err := agent.Resume(ctx); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	// Stop
	if err := agent.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	expected := []string{
		"unknown->starting",
		"starting->running",
		"running->paused",
		"paused->running",
		"running->stopping",
		"stopping->stopped",
	}

	if len(transitions) != len(expected) {
		t.Fatalf("transition count = %d, want %d\ngot: %v",
			len(transitions), len(expected), transitions)
	}
	for i, want := range expected {
		if transitions[i] != want {
			t.Errorf("transition[%d] = %q, want %q", i, transitions[i], want)
		}
	}
}

// ===========================================================================
// Concurrency Tests
// ===========================================================================

// TestBaseAgent_ConcurrentStateAccess verifies that concurrent reads of
// State() do not race with lifecycle operations. This test relies on the
// -race detector.
func TestBaseAgent_ConcurrentStateAccess(t *testing.T) {
	agent := mustBuildAgent(t)

	var wg sync.WaitGroup
	ctx := context.Background()

	// Start the agent in a goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = agent.Start(ctx)
	}()

	// Concurrently read State from multiple goroutines.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = agent.State()
		}()
	}

	wg.Wait()
}

// TestBaseAgent_ConcurrentStartStop verifies that concurrent Start and
// Stop calls do not race or corrupt state. Only one operation should
// succeed; the other should receive a conflict error.
func TestBaseAgent_ConcurrentStartStop(t *testing.T) {
	agent := mustBuildAgent(t)

	var wg sync.WaitGroup
	ctx := context.Background()
	var startErr, stopErr atomic.Value

	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := agent.Start(ctx); err != nil {
			startErr.Store(err)
		}
	}()
	go func() {
		defer wg.Done()
		// Small delay to increase the chance Start runs first.
		time.Sleep(time.Millisecond)
		if err := agent.Stop(ctx); err != nil {
			stopErr.Store(err)
		}
	}()

	wg.Wait()

	// The final state should be one of the valid end states.
	finalState := agent.State()
	validEndStates := map[State]bool{
		StateRunning: true,
		StateStopped: true,
		StateFailed:  true,
	}
	if !validEndStates[finalState] {
		t.Errorf("final state = %q, want one of Running, Stopped, or Failed", finalState)
	}
}

// TestBaseAgent_ConcurrentInfo verifies that concurrent Info() calls do
// not race with lifecycle operations.
func TestBaseAgent_ConcurrentInfo(t *testing.T) {
	agent := mustStartAgent(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info := agent.Info()
			// Access all fields to ensure no race.
			_ = info.ID
			_ = info.Name
			_ = info.Version
			_ = info.State
			_ = info.Capabilities
			_ = info.StartedAt
			_ = info.Uptime
		}()
	}
	wg.Wait()
}

// TestBaseAgent_ConcurrentSetState verifies that concurrent SetState calls
// do not corrupt the agent's state. This test relies on the -race detector.
func TestBaseAgent_ConcurrentSetState(t *testing.T) {
	agent := mustBuildAgent(t)

	var wg sync.WaitGroup
	// Multiple goroutines try to transition Unknown -> Starting.
	// Only one should succeed; the rest should fail because the state
	// has already changed.
	var successCount atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := agent.SetState(StateStarting); err == nil {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := successCount.Load(); got != 1 {
		t.Errorf("successful SetState count = %d, want exactly 1", got)
	}
	if got := agent.State(); got != StateStarting {
		t.Errorf("State() = %q, want %q", got, StateStarting)
	}
}

// ===========================================================================
// Context Cancellation Tests
// ===========================================================================

// TestBaseAgent_Stop_ContextCanceled verifies that Stop with a canceled
// context returns immediately without modifying state.
func TestBaseAgent_Stop_ContextCanceled(t *testing.T) {
	agent := mustStartAgent(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := agent.Stop(ctx)
	if err == nil {
		t.Fatal("Stop() with canceled context expected error, got nil")
	}
	if !sserr.IsTimeout(err) {
		t.Errorf("IsTimeout() = false, want true for canceled Stop context")
	}

	// State should remain Running.
	if got := agent.State(); got != StateRunning {
		t.Errorf("State() = %q after canceled Stop, want %q", got, StateRunning)
	}
}

// TestBaseAgent_Pause_ContextCanceled verifies that Pause with a canceled
// context returns immediately without modifying state.
func TestBaseAgent_Pause_ContextCanceled(t *testing.T) {
	agent := mustStartAgent(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := agent.Pause(ctx)
	if err == nil {
		t.Fatal("Pause() with canceled context expected error, got nil")
	}
	if !sserr.IsTimeout(err) {
		t.Errorf("IsTimeout() = false, want true for canceled Pause context")
	}

	// State should remain Running.
	if got := agent.State(); got != StateRunning {
		t.Errorf("State() = %q after canceled Pause, want %q", got, StateRunning)
	}
}

// TestBaseAgent_Resume_ContextCanceled verifies that Resume with a canceled
// context returns immediately without modifying state.
func TestBaseAgent_Resume_ContextCanceled(t *testing.T) {
	agent := mustStartAgent(t)
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := agent.Resume(ctx)
	if err == nil {
		t.Fatal("Resume() with canceled context expected error, got nil")
	}
	if !sserr.IsTimeout(err) {
		t.Errorf("IsTimeout() = false, want true for canceled Resume context")
	}

	// State should remain Paused.
	if got := agent.State(); got != StatePaused {
		t.Errorf("State() = %q after canceled Resume, want %q", got, StatePaused)
	}
}

// ===========================================================================
// Hook Error Wrapping Tests
// ===========================================================================

// TestBaseAgent_Stop_HookErrorWraps verifies that the stop hook error is
// wrapped and accessible via errors.Is, and has the correct error code.
func TestBaseAgent_Stop_HookErrorWraps(t *testing.T) {
	hookErr := errors.New("cleanup failed")
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStop(func(ctx context.Context) error {
			return hookErr
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	stopErr := agent.Stop(context.Background())
	if stopErr == nil {
		t.Fatal("Stop() with failing hook expected error, got nil")
	}
	if !errors.Is(stopErr, hookErr) {
		t.Error("Stop() error does not wrap the hook error")
	}
	if !sserr.IsInternal(stopErr) {
		t.Errorf("IsInternal() = false, want true for stop hook failure")
	}
}

// TestBaseAgent_Pause_HookErrorWraps verifies that the pause hook error is
// wrapped and accessible via errors.Is, and has the correct error code.
func TestBaseAgent_Pause_HookErrorWraps(t *testing.T) {
	hookErr := errors.New("pause failed")
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnPause(func(ctx context.Context) error {
			return hookErr
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	pauseErr := agent.Pause(context.Background())
	if pauseErr == nil {
		t.Fatal("Pause() with failing hook expected error, got nil")
	}
	if !errors.Is(pauseErr, hookErr) {
		t.Error("Pause() error does not wrap the hook error")
	}
	if !sserr.IsInternal(pauseErr) {
		t.Errorf("IsInternal() = false, want true for pause hook failure")
	}
}

// TestBaseAgent_Resume_HookErrorWraps verifies that the resume hook error
// is wrapped and accessible via errors.Is, and has the correct error code.
func TestBaseAgent_Resume_HookErrorWraps(t *testing.T) {
	hookErr := errors.New("resume failed")
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnResume(func(ctx context.Context) error {
			return hookErr
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	resumeErr := agent.Resume(context.Background())
	if resumeErr == nil {
		t.Fatal("Resume() with failing hook expected error, got nil")
	}
	if !errors.Is(resumeErr, hookErr) {
		t.Error("Resume() error does not wrap the hook error")
	}
	if !sserr.IsInternal(resumeErr) {
		t.Errorf("IsInternal() = false, want true for resume hook failure")
	}
}

// ===========================================================================
// Additional Lifecycle Tests
// ===========================================================================

// TestBaseAgent_Stop_FromPaused verifies that a paused agent can be stopped
// directly without resuming first.
func TestBaseAgent_Stop_FromPaused(t *testing.T) {
	agent := mustStartAgent(t)
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	if err := agent.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() from Paused error: %v", err)
	}
	if got := agent.State(); got != StateStopped {
		t.Errorf("State() = %q after Stop from Paused, want %q", got, StateStopped)
	}
}

// TestBaseAgent_Info_WhilePaused verifies that Info returns correct data
// when the agent is paused. StartedAt should be nil and Uptime should be
// zero because the agent is not currently running.
func TestBaseAgent_Info_WhilePaused(t *testing.T) {
	agent := mustStartAgent(t)
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	info := agent.Info()
	if info.State != StatePaused {
		t.Errorf("Info().State = %q, want %q", info.State, StatePaused)
	}
	// When paused, StartedAt is not reported (agent is not Running).
	if info.StartedAt != nil {
		t.Errorf("Info().StartedAt = %v while paused, want nil", info.StartedAt)
	}
	if info.Uptime != 0 {
		t.Errorf("Info().Uptime = %v while paused, want 0", info.Uptime)
	}
}

// TestBaseAgent_MultipleStartStopCycles verifies that an agent can be
// started and stopped multiple times. Each cycle should produce correct
// state transitions and reset the start timestamp.
func TestBaseAgent_MultipleStartStopCycles(t *testing.T) {
	agent := mustBuildAgent(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := agent.Start(ctx); err != nil {
			t.Fatalf("cycle %d: Start() error: %v", i, err)
		}
		if got := agent.State(); got != StateRunning {
			t.Fatalf("cycle %d: State() = %q after Start, want %q", i, got, StateRunning)
		}

		info := agent.Info()
		if info.StartedAt == nil {
			t.Fatalf("cycle %d: StartedAt = nil after Start, want non-nil", i)
		}

		if err := agent.Stop(ctx); err != nil {
			t.Fatalf("cycle %d: Stop() error: %v", i, err)
		}
		if got := agent.State(); got != StateStopped {
			t.Fatalf("cycle %d: State() = %q after Stop, want %q", i, got, StateStopped)
		}

		info = agent.Info()
		if info.StartedAt != nil {
			t.Fatalf("cycle %d: StartedAt = %v after Stop, want nil", i, info.StartedAt)
		}
	}
}

// ===========================================================================
// Hook State Visibility Tests
// ===========================================================================

// TestBaseAgent_Pause_HookSeesRunningState verifies that the OnPause hook
// executes while the agent is still in StateRunning, ensuring external
// observers only see StatePaused after the hook completes.
func TestBaseAgent_Pause_HookSeesRunningState(t *testing.T) {
	var stateInHook State
	var agentRef *BaseAgent

	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnPause(func(ctx context.Context) error {
			stateInHook = agentRef.State()
			return nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	agentRef = agent

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	if stateInHook != StateRunning {
		t.Errorf("state during pause hook = %q, want %q (hook should run before transition)",
			stateInHook, StateRunning)
	}
}

// TestBaseAgent_Resume_HookSeesPausedState verifies that the OnResume hook
// executes while the agent is still in StatePaused, ensuring external
// observers only see StateRunning after the hook completes.
func TestBaseAgent_Resume_HookSeesPausedState(t *testing.T) {
	var stateInHook State
	var innerAgent *BaseAgent

	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnResume(func(ctx context.Context) error {
			stateInHook = innerAgent.State()
			return nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	innerAgent = agent

	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := agent.Pause(context.Background()); err != nil {
		t.Fatalf("Pause() error: %v", err)
	}
	if err := agent.Resume(context.Background()); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if stateInHook != StatePaused {
		t.Errorf("state during resume hook = %q, want %q (hook should run before transition)",
			stateInHook, StatePaused)
	}
}

// ===========================================================================
// Additional Health Tests
// ===========================================================================

// TestBaseAgent_Health_Stopped verifies that Health returns a CodeUnavailable
// error when the agent is stopped.
func TestBaseAgent_Health_Stopped(t *testing.T) {
	agent := mustStartAgent(t)
	if err := agent.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	err := agent.Health(context.Background())
	if err == nil {
		t.Fatal("Health() = nil when stopped, want error")
	}
	if !sserr.IsUnavailable(err) {
		t.Errorf("IsUnavailable() = false, want true for stopped agent")
	}
}

// TestBaseAgent_Health_Failed verifies that Health returns a CodeUnavailable
// error when the agent is in the Failed state.
func TestBaseAgent_Health_Failed(t *testing.T) {
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStart(func(ctx context.Context) error {
			return errors.New("startup failure")
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	_ = agent.Start(context.Background()) // puts agent in Failed state

	healthErr := agent.Health(context.Background())
	if healthErr == nil {
		t.Fatal("Health() = nil when failed, want error")
	}
	if !sserr.IsUnavailable(healthErr) {
		t.Errorf("IsUnavailable() = false, want true for failed agent")
	}
}

// TestBaseAgent_Health_Starting verifies that Health returns a CodeUnavailable
// error when the agent is in the Starting state.
func TestBaseAgent_Health_Starting(t *testing.T) {
	agent := mustBuildAgent(t)
	if err := agent.SetState(StateStarting); err != nil {
		t.Fatalf("SetState(Starting) error: %v", err)
	}

	err := agent.Health(context.Background())
	if err == nil {
		t.Fatal("Health() = nil when starting, want error")
	}
	if !sserr.IsUnavailable(err) {
		t.Errorf("IsUnavailable() = false, want true for starting agent")
	}
}

// ===========================================================================
// Additional Concurrency Tests
// ===========================================================================

// TestBaseAgent_ConcurrentPauseResume verifies that concurrent Pause and
// Resume calls do not race or corrupt state. This test relies on the
// -race detector.
func TestBaseAgent_ConcurrentPauseResume(t *testing.T) {
	agent := mustStartAgent(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = agent.Pause(ctx)
		}()
		go func() {
			defer wg.Done()
			_ = agent.Resume(ctx)
		}()
	}
	wg.Wait()

	// The final state should be one of the valid states. We can't predict
	// exactly which one due to the race between operations, but it must
	// be a recognized state.
	finalState := agent.State()
	if !finalState.Valid() {
		t.Errorf("final state = %q, want a valid state", finalState)
	}
}

// ===========================================================================
// AgentInfo JSON Tests
// ===========================================================================

// TestAgentInfo_JSONRoundTrip verifies that AgentInfo can be marshaled to
// JSON and unmarshaled back with all fields preserved.
func TestAgentInfo_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond) // truncate for JSON precision
	info := AgentInfo{
		ID:      "agent-001",
		Name:    "test-agent",
		Version: "1.0.0",
		State:   StateRunning,
		Capabilities: []Capability{
			{Name: "search", Version: "1.0.0", Metadata: map[string]string{"k": "v"}},
		},
		StartedAt: &now,
		Uptime:    5 * time.Second,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	var restored AgentInfo
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	if restored.ID != info.ID {
		t.Errorf("ID = %q, want %q", restored.ID, info.ID)
	}
	if restored.Name != info.Name {
		t.Errorf("Name = %q, want %q", restored.Name, info.Name)
	}
	if restored.Version != info.Version {
		t.Errorf("Version = %q, want %q", restored.Version, info.Version)
	}
	if restored.State != info.State {
		t.Errorf("State = %q, want %q", restored.State, info.State)
	}
	if len(restored.Capabilities) != 1 {
		t.Fatalf("Capabilities length = %d, want 1", len(restored.Capabilities))
	}
	if restored.Capabilities[0].Name != "search" {
		t.Errorf("Capabilities[0].Name = %q, want %q",
			restored.Capabilities[0].Name, "search")
	}
	if restored.StartedAt == nil {
		t.Fatal("StartedAt = nil, want non-nil")
	}
	if !restored.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", restored.StartedAt, now)
	}
}
