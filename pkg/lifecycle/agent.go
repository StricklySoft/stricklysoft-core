package lifecycle

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// tracerName is the OpenTelemetry instrumentation scope name for this package.
// It follows the Go module path convention for OTel instrumentation libraries.
const tracerName = "github.com/StricklySoft/stricklysoft-core/pkg/lifecycle"

// StateChangeHandler is a callback invoked when an agent's lifecycle state
// changes. It receives the previous state and the new state.
//
// Handlers execute synchronously under the agent's state mutex during
// [BaseAgent.SetState]. Implementations must not block for extended periods
// or call lifecycle methods on the same agent, as this will cause a deadlock.
// Handlers that panic are recovered and logged without preventing the state
// change.
//
// Typical uses include emitting metrics, updating orchestration registries,
// and triggering alerts on failure transitions.
type StateChangeHandler func(old, new State)

// Hook is a function called during a lifecycle transition (start, stop,
// pause, resume). It receives the caller's context, which may carry
// deadlines, cancellation signals, and identity information.
//
// If a hook returns a non-nil error, the lifecycle transition is aborted
// and the agent transitions to [StateFailed]. Hooks should perform cleanup
// on error to avoid leaving resources in an inconsistent state.
//
// Hooks execute outside the agent's state mutex, so they may safely call
// read-only methods ([BaseAgent.State], [BaseAgent.Info]) on the agent
// without causing deadlocks.
type Hook func(ctx context.Context) error

// Agent defines the lifecycle contract for all agents on the StricklySoft
// Cloud Platform. Every agent — regardless of its specific functionality —
// implements this interface to provide uniform lifecycle management, health
// reporting, and capability discovery to the orchestration layer.
//
// All methods must be safe for concurrent use by multiple goroutines.
//
// The platform provides [BaseAgent] as a ready-to-use implementation
// with thread-safe state management, OpenTelemetry tracing, and hook
// support. Concrete agents embed or compose [BaseAgent] and register
// lifecycle hooks via [BaseAgentBuilder] to inject domain-specific
// startup and shutdown logic.
//
// Example (concrete agent using BaseAgent):
//
//	type ResearchAgent struct {
//	    *lifecycle.BaseAgent
//	    db *postgres.Client
//	}
//
//	func NewResearchAgent(db *postgres.Client) (*ResearchAgent, error) {
//	    ra := &ResearchAgent{db: db}
//	    base, err := lifecycle.NewBaseAgentBuilder("research-001", "research-agent", "1.0.0").
//	        WithCapability(lifecycle.Capability{Name: "web-search", Version: "1.0.0"}).
//	        WithOnStart(ra.onStart).
//	        WithOnStop(ra.onStop).
//	        Build()
//	    if err != nil {
//	        return nil, err
//	    }
//	    ra.BaseAgent = base
//	    return ra, nil
//	}
//
//	func (ra *ResearchAgent) onStart(ctx context.Context) error {
//	    return ra.db.Health(ctx) // verify DB before accepting work
//	}
//
//	func (ra *ResearchAgent) onStop(ctx context.Context) error {
//	    ra.db.Close()
//	    return nil
//	}
type Agent interface {
	// ID returns the unique identifier of the agent instance. IDs are
	// immutable after construction and typically follow the format
	// "<type>-<uuid>" (e.g., "research-agent-a1b2c3").
	ID() string

	// Name returns the human-readable name of the agent (e.g.,
	// "research-agent"). Names identify the agent type, not the instance.
	Name() string

	// Version returns the semantic version of the agent implementation
	// (e.g., "1.2.0").
	Version() string

	// Info returns a point-in-time snapshot of the agent's identity,
	// state, capabilities, and uptime. The returned [AgentInfo] is a
	// deep copy safe to serialize or store.
	Info() AgentInfo

	// Start begins the agent's operation. It transitions the agent through
	// [StateStarting] to [StateRunning], executing any registered OnStart
	// hook between the two transitions. If the hook fails, the agent
	// transitions to [StateFailed].
	//
	// Start may only be called from [StateUnknown], [StateStopped], or
	// [StateFailed]. Calling Start from any other state returns a
	// [sserr.CodeConflict] error.
	//
	// The context controls the deadline for startup; if the context is
	// canceled, Start returns immediately with a [sserr.CodeTimeout] error.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the agent. It transitions the agent
	// through [StateStopping] to [StateStopped], executing any registered
	// OnStop hook between the two transitions. If the hook fails, the
	// agent transitions to [StateFailed].
	//
	// Stop may be called from [StateRunning], [StatePaused], or
	// [StateStarting]. Calling Stop from a terminal state is a no-op
	// and returns nil. Calling Stop from any other state returns a
	// [sserr.CodeConflict] error.
	Stop(ctx context.Context) error

	// Pause temporarily suspends the agent's operation. The agent retains
	// its resources but stops processing new work. It transitions from
	// [StateRunning] to [StatePaused], executing any registered OnPause
	// hook. If the hook fails, the agent transitions to [StateFailed].
	//
	// Pause may only be called from [StateRunning]. Calling Pause from
	// any other state returns a [sserr.CodeConflict] error.
	Pause(ctx context.Context) error

	// Resume restores a paused agent to [StateRunning]. It transitions
	// from [StatePaused] to [StateRunning], executing any registered
	// OnResume hook. If the hook fails, the agent transitions to
	// [StateFailed].
	//
	// Resume may only be called from [StatePaused]. Calling Resume from
	// any other state returns a [sserr.CodeConflict] error.
	Resume(ctx context.Context) error

	// State returns the current lifecycle state of the agent.
	State() State

	// Capabilities returns the list of capabilities supported by this
	// agent. The returned slice is a defensive copy; modifying it does
	// not affect the agent's internal state.
	Capabilities() []Capability

	// Health performs a health check on the agent. Returns nil if the
	// agent is in [StateRunning], or a [sserr.CodeUnavailable] error
	// describing the current state otherwise. Concrete agents may
	// override this method to add deeper health checks (e.g., verifying
	// database connectivity).
	Health(ctx context.Context) error
}

// AgentInfo provides a point-in-time snapshot of an agent's identity,
// state, capabilities, and uptime. It is returned by [Agent.Info] and
// is safe to serialize to JSON for API responses, health endpoints,
// and orchestration registries.
//
// The Uptime field is computed at the time Info() is called and reflects
// the elapsed time since the agent entered [StateRunning]. It is zero
// if the agent has not yet started or has been stopped.
type AgentInfo struct {
	// ID is the unique identifier of the agent instance.
	ID string `json:"id"`

	// Name is the human-readable name of the agent type.
	Name string `json:"name"`

	// Version is the semantic version of the agent implementation.
	Version string `json:"version"`

	// State is the current lifecycle state of the agent.
	State State `json:"state"`

	// Capabilities is the list of capabilities the agent supports.
	Capabilities []Capability `json:"capabilities"`

	// StartedAt is the time the agent entered StateRunning. Nil if the
	// agent has not started or has been stopped.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// Uptime is the elapsed time since the agent entered StateRunning.
	// Zero if the agent is not currently running.
	Uptime time.Duration `json:"uptime,omitempty"`
}

// BaseAgent provides a thread-safe base implementation of the [Agent]
// interface with lifecycle state management, observer hooks, and
// OpenTelemetry tracing. It is the recommended foundation for all
// concrete agent implementations on the StricklySoft Cloud Platform.
//
// A BaseAgent is safe for concurrent use by multiple goroutines. Create
// one using [BaseAgentBuilder] and share it across the application.
//
// BaseAgent enforces a state machine that prevents invalid lifecycle
// transitions. All state changes are validated against the transition
// matrix defined in [validTransitions]. State change observers registered
// via [BaseAgentBuilder.OnStateChange] are notified synchronously on
// every transition.
//
// Lifecycle hooks (OnStart, OnStop, OnPause, OnResume) execute outside
// the state mutex to prevent deadlocks. If a hook fails, the agent
// transitions to [StateFailed] and the error is wrapped with a platform
// error code.
type BaseAgent struct {
	// Immutable fields — set at construction, never modified. These do
	// not require mutex protection.
	id      string
	name    string
	version string

	// Mutable fields — protected by mu.
	mu           sync.RWMutex
	state        State
	capabilities []Capability
	startedAt    *time.Time

	// Observability — set at construction, never modified.
	tracer trace.Tracer
	logger *slog.Logger

	// Lifecycle hooks — set at construction via builder, never modified.
	onStart  Hook
	onStop   Hook
	onPause  Hook
	onResume Hook

	// State change observers — set at construction via builder, never modified.
	stateHandlers []StateChangeHandler
}

// Compile-time interface compliance check. This ensures that *BaseAgent
// satisfies the Agent interface at compile time rather than at runtime.
var _ Agent = (*BaseAgent)(nil)

// ID returns the unique identifier of the agent. This value is immutable
// after construction.
func (a *BaseAgent) ID() string {
	return a.id
}

// Name returns the human-readable name of the agent. This value is
// immutable after construction.
func (a *BaseAgent) Name() string {
	return a.name
}

// Version returns the semantic version of the agent. This value is
// immutable after construction.
func (a *BaseAgent) Version() string {
	return a.version
}

// State returns the current lifecycle state of the agent. This method
// is safe for concurrent use.
func (a *BaseAgent) State() State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// Capabilities returns a defensive copy of the agent's registered
// capabilities. Modifying the returned slice or its elements does not
// affect the agent's internal state. This method is safe for concurrent
// use.
func (a *BaseAgent) Capabilities() []Capability {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return cloneCapabilities(a.capabilities)
}

// Info returns a point-in-time snapshot of the agent's identity, state,
// capabilities, and uptime. The returned [AgentInfo] contains deep copies
// of all mutable fields and is safe to serialize to JSON. This method is
// safe for concurrent use.
func (a *BaseAgent) Info() AgentInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	info := AgentInfo{
		ID:           a.id,
		Name:         a.name,
		Version:      a.version,
		State:        a.state,
		Capabilities: cloneCapabilities(a.capabilities),
	}

	if a.startedAt != nil && a.state == StateRunning {
		t := *a.startedAt
		info.StartedAt = &t
		info.Uptime = time.Since(t)
	}

	return info
}

// Health performs a health check on the agent. Returns nil if the agent is
// in [StateRunning], or a [*sserr.Error] with code
// [sserr.CodeUnavailable] if the agent is in any other state.
//
// Concrete agents may embed BaseAgent and override this method to add
// deeper health checks (e.g., verifying database connectivity, checking
// model availability).
//
// Example:
//
//	func (ra *ResearchAgent) Health(ctx context.Context) error {
//	    if err := ra.BaseAgent.Health(ctx); err != nil {
//	        return err
//	    }
//	    return ra.db.Health(ctx)
//	}
func (a *BaseAgent) Health(ctx context.Context) error {
	state := a.State()
	if state != StateRunning {
		return sserr.Newf(sserr.CodeUnavailable,
			"lifecycle: agent is not running, current state is %q", state)
	}
	return nil
}

// SetState transitions the agent to the given state after validating the
// transition against the lifecycle state machine. Returns a
// [*sserr.Error] with code [sserr.CodeConflict] if the transition is
// not allowed.
//
// On a successful transition, all registered [StateChangeHandler]
// functions are called synchronously with the old and new state values.
// Handlers execute under the state mutex; they must not call lifecycle
// methods on the same agent or block for extended periods.
//
// SetState is exported for use by concrete agent implementations that
// need to set state programmatically (e.g., transitioning to
// [StateFailed] when an internal error is detected).
//
// Example:
//
//	if err := criticalOperation(); err != nil {
//	    slog.ErrorContext(ctx, "lifecycle: critical operation failed", "error", err)
//	    _ = agent.SetState(lifecycle.StateFailed)
//	}
func (a *BaseAgent) SetState(new State) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	old := a.state
	if !ValidTransition(old, new) {
		return sserr.Newf(sserr.CodeConflict,
			"lifecycle: invalid state transition from %q to %q", old, new)
	}

	a.state = new

	// Notify state change handlers under the lock to guarantee ordering.
	// Each handler is called in a deferred-recover wrapper to prevent a
	// panicking handler from crashing the agent or corrupting state.
	for _, h := range a.stateHandlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					a.logger.Error("lifecycle: state change handler panicked",
						"panic", r,
						"agent_id", a.id,
						"old_state", string(old),
						"new_state", string(new),
					)
				}
			}()
			h(old, new)
		}()
	}

	return nil
}

// Start begins the agent's operation. It transitions the agent through
// [StateStarting] to [StateRunning], executing any registered OnStart
// hook between the two transitions.
//
// The context controls the deadline for startup. If the context is
// already canceled, Start returns immediately without modifying state.
//
// If the OnStart hook returns an error, the agent transitions to
// [StateFailed] and the error is returned wrapped with
// [sserr.CodeInternal].
func (a *BaseAgent) Start(ctx context.Context) error {
	ctx, span := a.tracer.Start(ctx, "lifecycle.Start",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("agent.id", a.id),
			attribute.String("agent.name", a.name),
		),
	)
	defer span.End()

	// Check context before acquiring the lock.
	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return sserr.Wrap(err, sserr.CodeTimeout,
			"lifecycle: start canceled before execution")
	}

	// Transition to Starting.
	if err := a.SetState(StateStarting); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	a.logger.InfoContext(ctx, "lifecycle: starting agent",
		"agent_id", a.id,
		"agent_name", a.name,
		"agent_version", a.version,
	)

	// Execute the OnStart hook outside the lock.
	if a.onStart != nil {
		if err := a.onStart(ctx); err != nil {
			a.logger.ErrorContext(ctx, "lifecycle: start hook failed",
				"agent_id", a.id,
				"error", err,
			)
			_ = a.SetState(StateFailed)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return sserr.Wrap(err, sserr.CodeInternal,
				"lifecycle: start hook failed")
		}
	}

	// Transition to Running and record the start timestamp.
	if err := a.SetState(StateRunning); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	now := time.Now().UTC()
	a.mu.Lock()
	a.startedAt = &now
	a.mu.Unlock()

	a.logger.InfoContext(ctx, "lifecycle: agent started",
		"agent_id", a.id,
		"agent_name", a.name,
	)
	span.SetStatus(codes.Ok, "")

	return nil
}

// Stop gracefully shuts down the agent. It transitions the agent through
// [StateStopping] to [StateStopped], executing any registered OnStop hook
// between the two transitions.
//
// If the agent is already in a terminal state ([StateStopped] or
// [StateFailed]), Stop is a no-op and returns nil. This makes it safe
// to call Stop multiple times or in a deferred cleanup.
//
// If the OnStop hook returns an error, the agent transitions to
// [StateFailed] and the error is returned wrapped with
// [sserr.CodeInternal].
func (a *BaseAgent) Stop(ctx context.Context) error {
	ctx, span := a.tracer.Start(ctx, "lifecycle.Stop",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("agent.id", a.id),
			attribute.String("agent.name", a.name),
		),
	)
	defer span.End()

	// Terminal states: Stop is a no-op.
	if a.State().IsTerminal() {
		span.SetStatus(codes.Ok, "")
		return nil
	}

	// Check context before proceeding.
	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return sserr.Wrap(err, sserr.CodeTimeout,
			"lifecycle: stop canceled before execution")
	}

	// Transition to Stopping.
	if err := a.SetState(StateStopping); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	a.logger.InfoContext(ctx, "lifecycle: stopping agent",
		"agent_id", a.id,
		"agent_name", a.name,
	)

	// Execute the OnStop hook outside the lock.
	if a.onStop != nil {
		if err := a.onStop(ctx); err != nil {
			a.logger.ErrorContext(ctx, "lifecycle: stop hook failed",
				"agent_id", a.id,
				"error", err,
			)
			_ = a.SetState(StateFailed)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return sserr.Wrap(err, sserr.CodeInternal,
				"lifecycle: stop hook failed")
		}
	}

	// Transition to Stopped and clear the start timestamp.
	if err := a.SetState(StateStopped); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	a.mu.Lock()
	a.startedAt = nil
	a.mu.Unlock()

	a.logger.InfoContext(ctx, "lifecycle: agent stopped",
		"agent_id", a.id,
		"agent_name", a.name,
	)
	span.SetStatus(codes.Ok, "")

	return nil
}

// Pause temporarily suspends the agent's operation. It transitions from
// [StateRunning] to [StatePaused], executing any registered OnPause hook.
//
// If the OnPause hook returns an error, the agent transitions to
// [StateFailed] and the error is returned wrapped with
// [sserr.CodeInternal].
func (a *BaseAgent) Pause(ctx context.Context) error {
	ctx, span := a.tracer.Start(ctx, "lifecycle.Pause",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("agent.id", a.id),
			attribute.String("agent.name", a.name),
		),
	)
	defer span.End()

	// Check context before proceeding.
	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return sserr.Wrap(err, sserr.CodeTimeout,
			"lifecycle: pause canceled before execution")
	}

	// Validate that we're in a state that can be paused (Running).
	// The state machine enforces this: only Running -> Paused is valid.
	if err := a.SetState(StatePaused); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	a.logger.InfoContext(ctx, "lifecycle: pausing agent",
		"agent_id", a.id,
		"agent_name", a.name,
	)

	// Execute the OnPause hook outside the lock.
	if a.onPause != nil {
		if err := a.onPause(ctx); err != nil {
			a.logger.ErrorContext(ctx, "lifecycle: pause hook failed",
				"agent_id", a.id,
				"error", err,
			)
			_ = a.SetState(StateFailed)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return sserr.Wrap(err, sserr.CodeInternal,
				"lifecycle: pause hook failed")
		}
	}

	a.logger.InfoContext(ctx, "lifecycle: agent paused",
		"agent_id", a.id,
		"agent_name", a.name,
	)
	span.SetStatus(codes.Ok, "")

	return nil
}

// Resume restores a paused agent to [StateRunning]. It transitions from
// [StatePaused] to [StateRunning], executing any registered OnResume hook.
//
// If the OnResume hook returns an error, the agent transitions to
// [StateFailed] and the error is returned wrapped with
// [sserr.CodeInternal].
func (a *BaseAgent) Resume(ctx context.Context) error {
	ctx, span := a.tracer.Start(ctx, "lifecycle.Resume",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("agent.id", a.id),
			attribute.String("agent.name", a.name),
		),
	)
	defer span.End()

	// Check context before proceeding.
	if err := ctx.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return sserr.Wrap(err, sserr.CodeTimeout,
			"lifecycle: resume canceled before execution")
	}

	// Validate that we're paused. The state machine enforces this:
	// only Paused -> Running is valid for Resume.
	if err := a.SetState(StateRunning); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	a.logger.InfoContext(ctx, "lifecycle: resuming agent",
		"agent_id", a.id,
		"agent_name", a.name,
	)

	// Execute the OnResume hook outside the lock.
	if a.onResume != nil {
		if err := a.onResume(ctx); err != nil {
			a.logger.ErrorContext(ctx, "lifecycle: resume hook failed",
				"agent_id", a.id,
				"error", err,
			)
			_ = a.SetState(StateFailed)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return sserr.Wrap(err, sserr.CodeInternal,
				"lifecycle: resume hook failed")
		}
	}

	a.logger.InfoContext(ctx, "lifecycle: agent resumed",
		"agent_id", a.id,
		"agent_name", a.name,
	)
	span.SetStatus(codes.Ok, "")

	return nil
}

// cloneCapabilities returns a deep copy of a capability slice, including
// independent copies of each capability's metadata map.
func cloneCapabilities(caps []Capability) []Capability {
	if caps == nil {
		return []Capability{}
	}
	cloned := make([]Capability, len(caps))
	for i, c := range caps {
		cloned[i] = c.Clone()
	}
	return cloned
}

// =========================================================================
// BaseAgentBuilder
// =========================================================================

// BaseAgentBuilder constructs a [BaseAgent] with validated configuration
// and optional lifecycle hooks. Use [NewBaseAgentBuilder] to start building.
//
// The builder follows the fluent API pattern: all configuration methods
// return the builder for chaining. Call [BaseAgentBuilder.Build] to
// validate the configuration and produce the agent.
//
// Example:
//
//	agent, err := lifecycle.NewBaseAgentBuilder("agent-001", "research-agent", "1.0.0").
//	    WithCapability(lifecycle.Capability{Name: "web-search", Version: "1.0.0"}).
//	    WithOnStart(func(ctx context.Context) error {
//	        return db.Health(ctx)
//	    }).
//	    WithOnStop(func(ctx context.Context) error {
//	        db.Close()
//	        return nil
//	    }).
//	    OnStateChange(func(old, new lifecycle.State) {
//	        metrics.AgentStateTransition(old, new)
//	    }).
//	    Build()
type BaseAgentBuilder struct {
	id            string
	name          string
	version       string
	capabilities  []Capability
	logger        *slog.Logger
	onStart       Hook
	onStop        Hook
	onPause       Hook
	onResume      Hook
	stateHandlers []StateChangeHandler
}

// NewBaseAgentBuilder creates a new builder with the required identity fields.
// The id, name, and version are validated during [BaseAgentBuilder.Build].
//
// Parameters:
//   - id: unique identifier for the agent instance (e.g., "research-agent-a1b2c3")
//   - name: human-readable agent type name (e.g., "research-agent")
//   - version: semantic version of the agent implementation (e.g., "1.0.0")
func NewBaseAgentBuilder(id, name, version string) *BaseAgentBuilder {
	return &BaseAgentBuilder{
		id:      id,
		name:    name,
		version: version,
	}
}

// WithCapability adds a single capability to the agent. The capability is
// deep-copied during [BaseAgentBuilder.Build] to prevent external mutation.
func (b *BaseAgentBuilder) WithCapability(cap Capability) *BaseAgentBuilder {
	b.capabilities = append(b.capabilities, cap)
	return b
}

// WithCapabilities adds multiple capabilities to the agent. Each capability
// is deep-copied during [BaseAgentBuilder.Build].
func (b *BaseAgentBuilder) WithCapabilities(caps []Capability) *BaseAgentBuilder {
	b.capabilities = append(b.capabilities, caps...)
	return b
}

// WithLogger sets a custom [*slog.Logger] for the agent. If not called,
// [slog.Default] is used. The logger is used for lifecycle event logging
// and panic recovery messages.
func (b *BaseAgentBuilder) WithLogger(logger *slog.Logger) *BaseAgentBuilder {
	b.logger = logger
	return b
}

// WithOnStart sets the lifecycle hook called during [BaseAgent.Start],
// after the agent transitions to [StateStarting] and before it transitions
// to [StateRunning]. Use this to perform agent-specific initialization
// (e.g., verifying database connectivity, loading models, subscribing to
// message queues).
func (b *BaseAgentBuilder) WithOnStart(hook Hook) *BaseAgentBuilder {
	b.onStart = hook
	return b
}

// WithOnStop sets the lifecycle hook called during [BaseAgent.Stop],
// after the agent transitions to [StateStopping] and before it transitions
// to [StateStopped]. Use this to perform agent-specific cleanup (e.g.,
// closing database connections, flushing buffers, unsubscribing from
// message queues).
func (b *BaseAgentBuilder) WithOnStop(hook Hook) *BaseAgentBuilder {
	b.onStop = hook
	return b
}

// WithOnPause sets the lifecycle hook called during [BaseAgent.Pause],
// after the agent transitions to [StatePaused]. Use this to suspend
// background workers or release non-essential resources while the agent
// is paused.
func (b *BaseAgentBuilder) WithOnPause(hook Hook) *BaseAgentBuilder {
	b.onPause = hook
	return b
}

// WithOnResume sets the lifecycle hook called during [BaseAgent.Resume],
// after the agent transitions back to [StateRunning]. Use this to restart
// background workers or reacquire resources that were released during
// pause.
func (b *BaseAgentBuilder) WithOnResume(hook Hook) *BaseAgentBuilder {
	b.onResume = hook
	return b
}

// OnStateChange registers a [StateChangeHandler] that is called on every
// state transition. Multiple handlers may be registered and are called in
// registration order. Handlers execute synchronously under the state mutex
// during [BaseAgent.SetState].
//
// Handlers are defensively copied during [BaseAgentBuilder.Build] to
// prevent external modification of the handler list after construction.
func (b *BaseAgentBuilder) OnStateChange(handler StateChangeHandler) *BaseAgentBuilder {
	b.stateHandlers = append(b.stateHandlers, handler)
	return b
}

// Build validates the configuration and constructs a [*BaseAgent]. Returns
// a [*sserr.Error] with code [sserr.CodeValidation] if any required field
// is empty.
//
// Build performs defensive copies of all mutable inputs (capabilities,
// state handlers) to prevent external mutation after construction. The
// initial state is [StateUnknown].
func (b *BaseAgentBuilder) Build() (*BaseAgent, error) {
	if b.id == "" {
		return nil, sserr.New(sserr.CodeValidation,
			"lifecycle: agent id must not be empty")
	}
	if b.name == "" {
		return nil, sserr.New(sserr.CodeValidation,
			"lifecycle: agent name must not be empty")
	}
	if b.version == "" {
		return nil, sserr.New(sserr.CodeValidation,
			"lifecycle: agent version must not be empty")
	}

	logger := b.logger
	if logger == nil {
		logger = slog.Default()
	}

	// Defensive copy of capabilities.
	caps := make([]Capability, len(b.capabilities))
	for i, c := range b.capabilities {
		caps[i] = c.Clone()
	}

	// Defensive copy of state handlers.
	handlers := make([]StateChangeHandler, len(b.stateHandlers))
	copy(handlers, b.stateHandlers)

	return &BaseAgent{
		id:            b.id,
		name:          b.name,
		version:       b.version,
		state:         StateUnknown,
		capabilities:  caps,
		tracer:        otel.Tracer(tracerName),
		logger:        logger,
		onStart:       b.onStart,
		onStop:        b.onStop,
		onPause:       b.onPause,
		onResume:      b.onResume,
		stateHandlers: handlers,
	}, nil
}
