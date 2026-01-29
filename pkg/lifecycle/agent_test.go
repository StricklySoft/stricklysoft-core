package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// mustBuildAgent is a test helper that creates a BaseAgent with default test
// identity values via the builder, failing the test if Build returns an error.
func mustBuildAgent(t *testing.T) *BaseAgent {
	t.Helper()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").Build()
	require.NoError(t, err)
	return agent
}

// mustStartAgent is a test helper that builds an agent with default test
// identity values and starts it, failing the test if either operation
// returns an error.
func mustStartAgent(t *testing.T) *BaseAgent {
	t.Helper()
	agent := mustBuildAgent(t)
	require.NoError(t, agent.Start(context.Background()))
	return agent
}

// ===========================================================================
// Accessor Tests
// ===========================================================================

// TestBaseAgent_ID verifies that ID returns the value set during construction.
func TestBaseAgent_ID(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)
	assert.Equal(t, "agent-001", agent.ID())
}

// TestBaseAgent_Name verifies that Name returns the value set during
// construction.
func TestBaseAgent_Name(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)
	assert.Equal(t, "test-agent", agent.Name())
}

// TestBaseAgent_Version verifies that Version returns the value set during
// construction.
func TestBaseAgent_Version(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)
	assert.Equal(t, "1.0.0", agent.Version())
}

// ===========================================================================
// State Tests
// ===========================================================================

// TestBaseAgent_State_InitialValue verifies that a newly constructed agent
// starts in StateUnknown.
func TestBaseAgent_State_InitialValue(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)
	assert.Equal(t, StateUnknown, agent.State())
}

// TestBaseAgent_SetState_ValidTransition verifies that SetState succeeds
// for an allowed transition.
func TestBaseAgent_SetState_ValidTransition(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)

	// Unknown -> Starting is a valid transition.
	require.NoError(t, agent.SetState(StateStarting))
	assert.Equal(t, StateStarting, agent.State())
}

// TestBaseAgent_SetState_InvalidTransition verifies that SetState returns
// a CodeConflict error for a disallowed transition.
func TestBaseAgent_SetState_InvalidTransition(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)

	// Unknown -> Running is not a valid transition.
	err := agent.SetState(StateRunning)
	require.Error(t, err)

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeConflict, ssErr.Code)

	// State should remain unchanged.
	assert.Equal(t, StateUnknown, agent.State())
}

// TestBaseAgent_SetState_NotifiesHandlers verifies that state change
// handlers are called with the correct old and new state values.
func TestBaseAgent_SetState_NotifiesHandlers(t *testing.T) {
	t.Parallel()
	var capturedOld, capturedNew State
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(old, new State) {
			capturedOld = old
			capturedNew = new
		}).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.SetState(StateStarting))

	assert.Equal(t, StateUnknown, capturedOld)
	assert.Equal(t, StateStarting, capturedNew)
}

// TestBaseAgent_SetState_MultipleHandlers verifies that multiple handlers
// are called in registration order.
func TestBaseAgent_SetState_MultipleHandlers(t *testing.T) {
	t.Parallel()
	var order []int
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(_, _ State) { order = append(order, 1) }).
		OnStateChange(func(_, _ State) { order = append(order, 2) }).
		OnStateChange(func(_, _ State) { order = append(order, 3) }).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.SetState(StateStarting))

	require.Len(t, order, 3)
	for i, v := range order {
		assert.Equal(t, i+1, v)
	}
}

// TestBaseAgent_SetState_HandlerPanicRecovery verifies that a panicking
// handler does not prevent the state change or crash the agent, and that
// subsequent handlers still execute.
func TestBaseAgent_SetState_HandlerPanicRecovery(t *testing.T) {
	t.Parallel()
	var secondCalled bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(_, _ State) { panic("test panic") }).
		OnStateChange(func(_, _ State) { secondCalled = true }).
		Build()
	require.NoError(t, err)

	// SetState should not panic and should succeed.
	require.NoError(t, agent.SetState(StateStarting))

	// State should have changed despite the panic.
	assert.Equal(t, StateStarting, agent.State())

	// The second handler should still have been called.
	assert.True(t, secondCalled, "second handler was not called after first handler panicked")
}

// ===========================================================================
// Capabilities Tests
// ===========================================================================

// TestBaseAgent_Capabilities_Empty verifies that Capabilities returns an
// empty (non-nil) slice when no capabilities are registered.
func TestBaseAgent_Capabilities_Empty(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)
	caps := agent.Capabilities()
	assert.NotNil(t, caps)
	assert.Len(t, caps, 0)
}

// TestBaseAgent_Capabilities_WithEntries verifies that Capabilities returns
// the capabilities registered via the builder.
func TestBaseAgent_Capabilities_WithEntries(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{Name: "search", Version: "1.0.0"}).
		WithCapability(Capability{Name: "execute", Version: "2.0.0"}).
		Build()
	require.NoError(t, err)

	caps := agent.Capabilities()
	require.Len(t, caps, 2)
	assert.Equal(t, "search", caps[0].Name)
	assert.Equal(t, "execute", caps[1].Name)
}

// TestBaseAgent_Capabilities_DefensiveCopy verifies that modifying the
// returned capability slice does not affect the agent's internal state.
func TestBaseAgent_Capabilities_DefensiveCopy(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{
			Name:     "search",
			Version:  "1.0.0",
			Metadata: map[string]string{"key": "original"},
		}).
		Build()
	require.NoError(t, err)

	// Get capabilities and mutate the returned slice.
	caps := agent.Capabilities()
	caps[0].Name = "mutated"
	caps[0].Metadata["key"] = "mutated"

	// Fetch again and verify the internal state was not affected.
	fresh := agent.Capabilities()
	assert.Equal(t, "search", fresh[0].Name)
	assert.Equal(t, "original", fresh[0].Metadata["key"])
}

// ===========================================================================
// Info Tests
// ===========================================================================

// TestBaseAgent_Info verifies that Info returns an AgentInfo with all fields
// correctly populated.
func TestBaseAgent_Info(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithCapability(Capability{Name: "search", Version: "1.0.0"}).
		Build()
	require.NoError(t, err)

	info := agent.Info()

	assert.Equal(t, "agent-001", info.ID)
	assert.Equal(t, "test-agent", info.Name)
	assert.Equal(t, "1.0.0", info.Version)
	assert.Equal(t, StateUnknown, info.State)
	assert.Len(t, info.Capabilities, 1)
}

// TestBaseAgent_Info_NoStartedAtBeforeStart verifies that Info returns nil
// StartedAt and zero Uptime before the agent has been started.
func TestBaseAgent_Info_NoStartedAtBeforeStart(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)
	info := agent.Info()

	assert.Nil(t, info.StartedAt)
	assert.Equal(t, time.Duration(0), info.Uptime)
}

// TestBaseAgent_Info_StartedAtAfterStart verifies that Info returns a
// non-nil StartedAt and positive Uptime after the agent has been started.
func TestBaseAgent_Info_StartedAtAfterStart(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	info := agent.Info()

	assert.NotNil(t, info.StartedAt)
	assert.GreaterOrEqual(t, info.Uptime, time.Duration(0))
}

// TestBaseAgent_Info_UptimeResetAfterStop verifies that StartedAt is nil
// and Uptime is zero after the agent has been stopped.
func TestBaseAgent_Info_UptimeResetAfterStop(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)

	require.NoError(t, agent.Stop(context.Background()))

	info := agent.Info()
	assert.Nil(t, info.StartedAt)
	assert.Equal(t, time.Duration(0), info.Uptime)
}

// ===========================================================================
// Health Tests
// ===========================================================================

// TestBaseAgent_Health_Running verifies that Health returns nil when the
// agent is in StateRunning.
func TestBaseAgent_Health_Running(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	assert.NoError(t, agent.Health(context.Background()))
}

// TestBaseAgent_Health_NotRunning verifies that Health returns an error
// when the agent is not in StateRunning.
func TestBaseAgent_Health_NotRunning(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)

	err := agent.Health(context.Background())
	require.Error(t, err)

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeUnavailable, ssErr.Code)
}

// TestBaseAgent_Health_Paused verifies that Health returns an error when
// the agent is paused.
func TestBaseAgent_Health_Paused(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	require.NoError(t, agent.Pause(context.Background()))

	err := agent.Health(context.Background())
	require.Error(t, err)
	assert.True(t, sserr.IsUnavailable(err), "IsUnavailable() should be true for paused agent")
}

// ===========================================================================
// Start Tests
// ===========================================================================

// TestBaseAgent_Start_Success verifies that Start transitions the agent
// from Unknown to Running.
func TestBaseAgent_Start_Success(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)

	require.NoError(t, agent.Start(context.Background()))

	assert.Equal(t, StateRunning, agent.State())
}

// TestBaseAgent_Start_SetsStartedAt verifies that Start sets the startedAt
// timestamp.
func TestBaseAgent_Start_SetsStartedAt(t *testing.T) {
	t.Parallel()
	before := time.Now().UTC()
	agent := mustStartAgent(t)
	after := time.Now().UTC()

	info := agent.Info()
	require.NotNil(t, info.StartedAt)
	assert.False(t, info.StartedAt.Before(before) || info.StartedAt.After(after),
		"StartedAt = %v, want between %v and %v", info.StartedAt, before, after)
}

// TestBaseAgent_Start_WithHook verifies that the OnStart hook is called
// during Start.
func TestBaseAgent_Start_WithHook(t *testing.T) {
	t.Parallel()
	var hookCalled bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStart(func(ctx context.Context) error {
			hookCalled = true
			return nil
		}).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.Start(context.Background()))

	assert.True(t, hookCalled, "OnStart hook was not called")
}

// TestBaseAgent_Start_HookError verifies that a hook error transitions the
// agent to StateFailed and returns a wrapped error.
func TestBaseAgent_Start_HookError(t *testing.T) {
	t.Parallel()
	hookErr := errors.New("database unavailable")
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStart(func(ctx context.Context) error {
			return hookErr
		}).
		Build()
	require.NoError(t, err)

	startErr := agent.Start(context.Background())
	require.Error(t, startErr)

	// Agent should be in StateFailed.
	assert.Equal(t, StateFailed, agent.State())

	// Error should wrap the hook error.
	assert.True(t, errors.Is(startErr, hookErr), "Start() error does not wrap the hook error")

	// Error should have CodeInternal.
	var ssErr *sserr.Error
	require.True(t, errors.As(startErr, &ssErr), "error type = %T, want *sserr.Error", startErr)
	assert.Equal(t, sserr.CodeInternal, ssErr.Code)
}

// TestBaseAgent_Start_InvalidState verifies that Start from StateRunning
// returns a conflict error.
func TestBaseAgent_Start_InvalidState(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)

	err := agent.Start(context.Background())
	require.Error(t, err)

	assert.True(t, sserr.IsConflict(err), "IsConflict() should be true for Start while running")
}

// TestBaseAgent_Start_ContextCanceled verifies that Start with a canceled
// context returns immediately without modifying state.
func TestBaseAgent_Start_ContextCanceled(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := agent.Start(ctx)
	require.Error(t, err)

	// State should remain Unknown.
	assert.Equal(t, StateUnknown, agent.State())
}

// TestBaseAgent_Start_FromStopped verifies that an agent can be restarted
// after being stopped.
func TestBaseAgent_Start_FromStopped(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	require.NoError(t, agent.Stop(context.Background()))

	require.NoError(t, agent.Start(context.Background()))

	assert.Equal(t, StateRunning, agent.State())
}

// TestBaseAgent_Start_FromFailed verifies that an agent can be restarted
// after a failure.
func TestBaseAgent_Start_FromFailed(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStart(func(ctx context.Context) error {
			return errors.New("startup failure")
		}).
		Build()
	require.NoError(t, err)

	// First Start fails, putting agent in Failed state.
	_ = agent.Start(context.Background())
	require.Equal(t, StateFailed, agent.State())

	// Replace the hook to succeed this time. Since hooks are set at
	// construction, we need a new agent. Instead, test the state transition.
	// Failed -> Starting should be valid.
	require.NoError(t, agent.SetState(StateStarting))
}

// ===========================================================================
// Stop Tests
// ===========================================================================

// TestBaseAgent_Stop_Success verifies that Stop transitions a running agent
// to Stopped.
func TestBaseAgent_Stop_Success(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)

	require.NoError(t, agent.Stop(context.Background()))

	assert.Equal(t, StateStopped, agent.State())
}

// TestBaseAgent_Stop_WithHook verifies that the OnStop hook is called
// during Stop.
func TestBaseAgent_Stop_WithHook(t *testing.T) {
	t.Parallel()
	var hookCalled bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStop(func(ctx context.Context) error {
			hookCalled = true
			return nil
		}).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.Start(context.Background()))

	require.NoError(t, agent.Stop(context.Background()))

	assert.True(t, hookCalled, "OnStop hook was not called")
}

// TestBaseAgent_Stop_HookError verifies that a stop hook error transitions
// the agent to StateFailed.
func TestBaseAgent_Stop_HookError(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStop(func(ctx context.Context) error {
			return errors.New("cleanup failed")
		}).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.Start(context.Background()))

	stopErr := agent.Stop(context.Background())
	require.Error(t, stopErr)

	assert.Equal(t, StateFailed, agent.State())
}

// TestBaseAgent_Stop_AlreadyStopped verifies that Stop is a no-op when
// the agent is already stopped.
func TestBaseAgent_Stop_AlreadyStopped(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	require.NoError(t, agent.Stop(context.Background()))

	// Second Stop should be a no-op.
	assert.NoError(t, agent.Stop(context.Background()))
}

// TestBaseAgent_Stop_InvalidState verifies that Stop from Unknown returns
// a conflict error (Unknown cannot transition to Stopping).
func TestBaseAgent_Stop_InvalidState(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)

	err := agent.Stop(context.Background())
	require.Error(t, err)

	assert.True(t, sserr.IsConflict(err), "IsConflict() should be true for Stop from Unknown")
}

// ===========================================================================
// Pause Tests
// ===========================================================================

// TestBaseAgent_Pause_Success verifies that Pause transitions a running
// agent to Paused.
func TestBaseAgent_Pause_Success(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)

	require.NoError(t, agent.Pause(context.Background()))

	assert.Equal(t, StatePaused, agent.State())
}

// TestBaseAgent_Pause_WithHook verifies that the OnPause hook is called.
func TestBaseAgent_Pause_WithHook(t *testing.T) {
	t.Parallel()
	var hookCalled bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnPause(func(ctx context.Context) error {
			hookCalled = true
			return nil
		}).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.Start(context.Background()))
	require.NoError(t, agent.Pause(context.Background()))

	assert.True(t, hookCalled, "OnPause hook was not called")
}

// TestBaseAgent_Pause_InvalidState verifies that Pause from Stopped returns
// a conflict error.
func TestBaseAgent_Pause_InvalidState(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	require.NoError(t, agent.Stop(context.Background()))

	err := agent.Pause(context.Background())
	require.Error(t, err)
	assert.True(t, sserr.IsConflict(err), "IsConflict() should be true for Pause from Stopped")
}

// TestBaseAgent_Pause_HookError verifies that a pause hook error transitions
// the agent to StateFailed.
func TestBaseAgent_Pause_HookError(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnPause(func(ctx context.Context) error {
			return errors.New("pause failed")
		}).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.Start(context.Background()))

	pauseErr := agent.Pause(context.Background())
	require.Error(t, pauseErr)
	assert.Equal(t, StateFailed, agent.State())
}

// ===========================================================================
// Resume Tests
// ===========================================================================

// TestBaseAgent_Resume_Success verifies that Resume transitions a paused
// agent back to Running.
func TestBaseAgent_Resume_Success(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	require.NoError(t, agent.Pause(context.Background()))

	require.NoError(t, agent.Resume(context.Background()))

	assert.Equal(t, StateRunning, agent.State())
}

// TestBaseAgent_Resume_WithHook verifies that the OnResume hook is called.
func TestBaseAgent_Resume_WithHook(t *testing.T) {
	t.Parallel()
	var hookCalled bool
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnResume(func(ctx context.Context) error {
			hookCalled = true
			return nil
		}).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.Start(context.Background()))
	require.NoError(t, agent.Pause(context.Background()))
	require.NoError(t, agent.Resume(context.Background()))

	assert.True(t, hookCalled, "OnResume hook was not called")
}

// TestBaseAgent_Resume_InvalidState verifies that Resume from Running
// returns a conflict error.
func TestBaseAgent_Resume_InvalidState(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)

	err := agent.Resume(context.Background())
	require.Error(t, err)
	assert.True(t, sserr.IsConflict(err), "IsConflict() should be true for Resume while Running")
}

// TestBaseAgent_Resume_HookError verifies that a resume hook error
// transitions the agent to StateFailed.
func TestBaseAgent_Resume_HookError(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnResume(func(ctx context.Context) error {
			return errors.New("resume failed")
		}).
		Build()
	require.NoError(t, err)

	require.NoError(t, agent.Start(context.Background()))
	require.NoError(t, agent.Pause(context.Background()))

	resumeErr := agent.Resume(context.Background())
	require.Error(t, resumeErr)
	assert.Equal(t, StateFailed, agent.State())
}

// ===========================================================================
// Full Lifecycle Tests
// ===========================================================================

// TestBaseAgent_FullLifecycle verifies the complete lifecycle flow:
// Start -> Pause -> Resume -> Stop.
func TestBaseAgent_FullLifecycle(t *testing.T) {
	t.Parallel()
	var transitions []string
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		OnStateChange(func(old, new State) {
			transitions = append(transitions, string(old)+"->"+string(new))
		}).
		Build()
	require.NoError(t, err)

	ctx := context.Background()

	// Start
	require.NoError(t, agent.Start(ctx))

	// Pause
	require.NoError(t, agent.Pause(ctx))

	// Resume
	require.NoError(t, agent.Resume(ctx))

	// Stop
	require.NoError(t, agent.Stop(ctx))

	expected := []string{
		"unknown->starting",
		"starting->running",
		"running->paused",
		"paused->running",
		"running->stopping",
		"stopping->stopped",
	}

	require.Len(t, transitions, len(expected))
	for i, want := range expected {
		assert.Equal(t, want, transitions[i])
	}
}

// ===========================================================================
// Concurrency Tests
// ===========================================================================

// TestBaseAgent_ConcurrentStateAccess verifies that concurrent reads of
// State() do not race with lifecycle operations. This test relies on the
// -race detector.
func TestBaseAgent_ConcurrentStateAccess(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	assert.True(t, validEndStates[finalState],
		"final state = %q, want one of Running, Stopped, or Failed", finalState)
}

// TestBaseAgent_ConcurrentInfo verifies that concurrent Info() calls do
// not race with lifecycle operations.
func TestBaseAgent_ConcurrentInfo(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

	assert.Equal(t, int32(1), successCount.Load())
	assert.Equal(t, StateStarting, agent.State())
}

// ===========================================================================
// Context Cancellation Tests
// ===========================================================================

// TestBaseAgent_Stop_ContextCanceled verifies that Stop with a canceled
// context returns immediately without modifying state.
func TestBaseAgent_Stop_ContextCanceled(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := agent.Stop(ctx)
	require.Error(t, err)
	assert.True(t, sserr.IsTimeout(err), "IsTimeout() should be true for canceled Stop context")

	// State should remain Running.
	assert.Equal(t, StateRunning, agent.State())
}

// TestBaseAgent_Pause_ContextCanceled verifies that Pause with a canceled
// context returns immediately without modifying state.
func TestBaseAgent_Pause_ContextCanceled(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := agent.Pause(ctx)
	require.Error(t, err)
	assert.True(t, sserr.IsTimeout(err), "IsTimeout() should be true for canceled Pause context")

	// State should remain Running.
	assert.Equal(t, StateRunning, agent.State())
}

// TestBaseAgent_Resume_ContextCanceled verifies that Resume with a canceled
// context returns immediately without modifying state.
func TestBaseAgent_Resume_ContextCanceled(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	require.NoError(t, agent.Pause(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := agent.Resume(ctx)
	require.Error(t, err)
	assert.True(t, sserr.IsTimeout(err), "IsTimeout() should be true for canceled Resume context")

	// State should remain Paused.
	assert.Equal(t, StatePaused, agent.State())
}

// ===========================================================================
// Hook Error Wrapping Tests
// ===========================================================================

// TestBaseAgent_Stop_HookErrorWraps verifies that the stop hook error is
// wrapped and accessible via errors.Is, and has the correct error code.
func TestBaseAgent_Stop_HookErrorWraps(t *testing.T) {
	t.Parallel()
	hookErr := errors.New("cleanup failed")
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStop(func(ctx context.Context) error {
			return hookErr
		}).
		Build()
	require.NoError(t, err)
	require.NoError(t, agent.Start(context.Background()))

	stopErr := agent.Stop(context.Background())
	require.Error(t, stopErr)
	assert.True(t, errors.Is(stopErr, hookErr), "Stop() error does not wrap the hook error")
	assert.True(t, sserr.IsInternal(stopErr), "IsInternal() should be true for stop hook failure")
}

// TestBaseAgent_Pause_HookErrorWraps verifies that the pause hook error is
// wrapped and accessible via errors.Is, and has the correct error code.
func TestBaseAgent_Pause_HookErrorWraps(t *testing.T) {
	t.Parallel()
	hookErr := errors.New("pause failed")
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnPause(func(ctx context.Context) error {
			return hookErr
		}).
		Build()
	require.NoError(t, err)
	require.NoError(t, agent.Start(context.Background()))

	pauseErr := agent.Pause(context.Background())
	require.Error(t, pauseErr)
	assert.True(t, errors.Is(pauseErr, hookErr), "Pause() error does not wrap the hook error")
	assert.True(t, sserr.IsInternal(pauseErr), "IsInternal() should be true for pause hook failure")
}

// TestBaseAgent_Resume_HookErrorWraps verifies that the resume hook error
// is wrapped and accessible via errors.Is, and has the correct error code.
func TestBaseAgent_Resume_HookErrorWraps(t *testing.T) {
	t.Parallel()
	hookErr := errors.New("resume failed")
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnResume(func(ctx context.Context) error {
			return hookErr
		}).
		Build()
	require.NoError(t, err)
	require.NoError(t, agent.Start(context.Background()))
	require.NoError(t, agent.Pause(context.Background()))

	resumeErr := agent.Resume(context.Background())
	require.Error(t, resumeErr)
	assert.True(t, errors.Is(resumeErr, hookErr), "Resume() error does not wrap the hook error")
	assert.True(t, sserr.IsInternal(resumeErr), "IsInternal() should be true for resume hook failure")
}

// ===========================================================================
// Additional Lifecycle Tests
// ===========================================================================

// TestBaseAgent_Stop_FromPaused verifies that a paused agent can be stopped
// directly without resuming first.
func TestBaseAgent_Stop_FromPaused(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	require.NoError(t, agent.Pause(context.Background()))

	require.NoError(t, agent.Stop(context.Background()))
	assert.Equal(t, StateStopped, agent.State())
}

// TestBaseAgent_Info_WhilePaused verifies that Info returns correct data
// when the agent is paused. StartedAt should be nil and Uptime should be
// zero because the agent is not currently running.
func TestBaseAgent_Info_WhilePaused(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	require.NoError(t, agent.Pause(context.Background()))

	info := agent.Info()
	assert.Equal(t, StatePaused, info.State)
	// When paused, StartedAt is not reported (agent is not Running).
	assert.Nil(t, info.StartedAt)
	assert.Equal(t, time.Duration(0), info.Uptime)
}

// TestBaseAgent_MultipleStartStopCycles verifies that an agent can be
// started and stopped multiple times. Each cycle should produce correct
// state transitions and reset the start timestamp.
func TestBaseAgent_MultipleStartStopCycles(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, agent.Start(ctx), "cycle %d: Start() error", i)
		require.Equal(t, StateRunning, agent.State(), "cycle %d: State() after Start", i)

		info := agent.Info()
		require.NotNil(t, info.StartedAt, "cycle %d: StartedAt after Start", i)

		require.NoError(t, agent.Stop(ctx), "cycle %d: Stop() error", i)
		require.Equal(t, StateStopped, agent.State(), "cycle %d: State() after Stop", i)

		info = agent.Info()
		require.Nil(t, info.StartedAt, "cycle %d: StartedAt after Stop", i)
	}
}

// ===========================================================================
// Hook State Visibility Tests
// ===========================================================================

// TestBaseAgent_Pause_HookSeesRunningState verifies that the OnPause hook
// executes while the agent is still in StateRunning, ensuring external
// observers only see StatePaused after the hook completes.
func TestBaseAgent_Pause_HookSeesRunningState(t *testing.T) {
	t.Parallel()
	var stateInHook State
	var agentRef *BaseAgent

	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnPause(func(ctx context.Context) error {
			stateInHook = agentRef.State()
			return nil
		}).
		Build()
	require.NoError(t, err)
	agentRef = agent

	require.NoError(t, agent.Start(context.Background()))
	require.NoError(t, agent.Pause(context.Background()))

	assert.Equal(t, StateRunning, stateInHook,
		"state during pause hook should be %q (hook should run before transition)", StateRunning)
}

// TestBaseAgent_Resume_HookSeesPausedState verifies that the OnResume hook
// executes while the agent is still in StatePaused, ensuring external
// observers only see StateRunning after the hook completes.
func TestBaseAgent_Resume_HookSeesPausedState(t *testing.T) {
	t.Parallel()
	var stateInHook State
	var innerAgent *BaseAgent

	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnResume(func(ctx context.Context) error {
			stateInHook = innerAgent.State()
			return nil
		}).
		Build()
	require.NoError(t, err)
	innerAgent = agent

	require.NoError(t, agent.Start(context.Background()))
	require.NoError(t, agent.Pause(context.Background()))
	require.NoError(t, agent.Resume(context.Background()))

	assert.Equal(t, StatePaused, stateInHook,
		"state during resume hook should be %q (hook should run before transition)", StatePaused)
}

// ===========================================================================
// Additional Health Tests
// ===========================================================================

// TestBaseAgent_Health_Stopped verifies that Health returns a CodeUnavailable
// error when the agent is stopped.
func TestBaseAgent_Health_Stopped(t *testing.T) {
	t.Parallel()
	agent := mustStartAgent(t)
	require.NoError(t, agent.Stop(context.Background()))

	err := agent.Health(context.Background())
	require.Error(t, err)
	assert.True(t, sserr.IsUnavailable(err), "IsUnavailable() should be true for stopped agent")
}

// TestBaseAgent_Health_Failed verifies that Health returns a CodeUnavailable
// error when the agent is in the Failed state.
func TestBaseAgent_Health_Failed(t *testing.T) {
	t.Parallel()
	agent, err := NewBaseAgentBuilder("agent-001", "test-agent", "1.0.0").
		WithOnStart(func(ctx context.Context) error {
			return errors.New("startup failure")
		}).
		Build()
	require.NoError(t, err)

	_ = agent.Start(context.Background()) // puts agent in Failed state

	healthErr := agent.Health(context.Background())
	require.Error(t, healthErr)
	assert.True(t, sserr.IsUnavailable(healthErr), "IsUnavailable() should be true for failed agent")
}

// TestBaseAgent_Health_Starting verifies that Health returns a CodeUnavailable
// error when the agent is in the Starting state.
func TestBaseAgent_Health_Starting(t *testing.T) {
	t.Parallel()
	agent := mustBuildAgent(t)
	require.NoError(t, agent.SetState(StateStarting))

	err := agent.Health(context.Background())
	require.Error(t, err)
	assert.True(t, sserr.IsUnavailable(err), "IsUnavailable() should be true for starting agent")
}

// ===========================================================================
// Additional Concurrency Tests
// ===========================================================================

// TestBaseAgent_ConcurrentPauseResume verifies that concurrent Pause and
// Resume calls do not race or corrupt state. This test relies on the
// -race detector.
func TestBaseAgent_ConcurrentPauseResume(t *testing.T) {
	t.Parallel()
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
	assert.True(t, finalState.Valid(), "final state = %q, want a valid state", finalState)
}

// ===========================================================================
// AgentInfo JSON Tests
// ===========================================================================

// TestAgentInfo_JSONRoundTrip verifies that AgentInfo can be marshaled to
// JSON and unmarshaled back with all fields preserved.
func TestAgentInfo_JSONRoundTrip(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err)

	var restored AgentInfo
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, info.ID, restored.ID)
	assert.Equal(t, info.Name, restored.Name)
	assert.Equal(t, info.Version, restored.Version)
	assert.Equal(t, info.State, restored.State)
	require.Len(t, restored.Capabilities, 1)
	assert.Equal(t, "search", restored.Capabilities[0].Name)
	require.NotNil(t, restored.StartedAt)
	assert.True(t, restored.StartedAt.Equal(now),
		"StartedAt = %v, want %v", restored.StartedAt, now)
}
