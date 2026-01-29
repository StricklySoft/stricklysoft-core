package lifecycle

import (
	"log/slog"

	"go.opentelemetry.io/otel"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

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
// validated and deep-copied during [BaseAgentBuilder.Build] to prevent
// external mutation. Build returns an error if the capability has an empty
// Name or Version.
func (b *BaseAgentBuilder) WithCapability(cap Capability) *BaseAgentBuilder {
	b.capabilities = append(b.capabilities, cap)
	return b
}

// WithCapabilities adds multiple capabilities to the agent. Each capability
// is validated and deep-copied during [BaseAgentBuilder.Build].
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
// after the state is validated as [StateRunning] and before the agent
// transitions to [StatePaused]. Use this to suspend background workers
// or release non-essential resources while the agent is paused.
func (b *BaseAgentBuilder) WithOnPause(hook Hook) *BaseAgentBuilder {
	b.onPause = hook
	return b
}

// WithOnResume sets the lifecycle hook called during [BaseAgent.Resume],
// after the state is validated as [StatePaused] and before the agent
// transitions back to [StateRunning]. Use this to restart background
// workers or reacquire resources that were released during pause.
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
// is empty or any capability has an empty Name or Version.
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

	// Validate and defensively copy capabilities.
	caps := make([]Capability, len(b.capabilities))
	for i, c := range b.capabilities {
		if err := validateCapability(c); err != nil {
			return nil, err
		}
		caps[i] = c.Clone()
	}

	logger := b.logger
	if logger == nil {
		logger = slog.Default()
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
