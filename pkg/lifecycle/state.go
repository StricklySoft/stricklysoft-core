// Package lifecycle provides agent lifecycle management for the StricklySoft
// Cloud Platform, including state machine transitions, health checks, and
// graceful shutdown.
//
// # Agent Lifecycle
//
// Every agent on the platform follows a defined lifecycle managed by a
// finite state machine. The [State] type represents the agent's current
// position in this lifecycle, and all transitions are validated against the
// [validTransitions] matrix to prevent illegal state changes.
//
// The lifecycle flow for a healthy agent is:
//
//	Unknown → Starting → Running → Stopping → Stopped
//
// Agents may also be paused and resumed:
//
//	Running → Paused → Running
//
// Any non-terminal state may transition to Failed on error, and both
// terminal states (Stopped, Failed) may transition back to Starting
// for restart.
//
// # Thread Safety
//
// State management in [BaseAgent] is protected by a [sync.RWMutex].
// All state reads and writes are safe for concurrent use by multiple
// goroutines, including lifecycle methods ([BaseAgent.Start],
// [BaseAgent.Stop], [BaseAgent.Pause], [BaseAgent.Resume]) and
// state queries ([BaseAgent.State], [BaseAgent.Info]).
//
// # OpenTelemetry Integration
//
// Lifecycle operations create OpenTelemetry spans with semantic attributes
// for observability. The tracer scope is
// "github.com/StricklySoft/stricklysoft-core/pkg/lifecycle".
package lifecycle

// State represents the lifecycle state of an agent in the StricklySoft
// Cloud Platform. States form a finite state machine with validated
// transitions defined by [ValidTransition].
//
// The zero value ("") is not a valid state; agents are initialized with
// [StateUnknown] at construction time.
type State string

const (
	// StateUnknown is the initial state of a newly constructed agent before
	// any lifecycle method has been called. An agent in this state has not
	// yet been started.
	StateUnknown State = "unknown"

	// StateStarting indicates the agent is in the process of starting. This
	// is a transient state set at the beginning of [BaseAgent.Start] before
	// the OnStart hook executes. External observers may see this state
	// during startup.
	StateStarting State = "starting"

	// StateRunning indicates the agent has started successfully and is
	// processing work. This is the only state in which [BaseAgent.Health]
	// reports healthy. Agents remain in this state until stopped, paused,
	// or a failure occurs.
	StateRunning State = "running"

	// StatePaused indicates the agent has been temporarily suspended via
	// [BaseAgent.Pause]. A paused agent retains its resources but does not
	// process new work. Call [BaseAgent.Resume] to return to
	// [StateRunning].
	StatePaused State = "paused"

	// StateStopping indicates the agent is in the process of shutting down.
	// This is a transient state set at the beginning of [BaseAgent.Stop]
	// before the OnStop hook executes, giving the agent time to drain
	// in-flight work.
	StateStopping State = "stopping"

	// StateStopped indicates the agent has completed a clean shutdown. This
	// is a terminal state. A stopped agent may be restarted by calling
	// [BaseAgent.Start], which transitions it back to [StateStarting].
	StateStopped State = "stopped"

	// StateFailed indicates the agent encountered an unrecoverable error.
	// This is a terminal state. A failed agent may be restarted by calling
	// [BaseAgent.Start], which transitions it back to [StateStarting].
	// The error that caused the failure should be logged before the
	// transition.
	StateFailed State = "failed"
)

// String returns the string representation of the state.
func (s State) String() string {
	return string(s)
}

// Valid reports whether the state is one of the recognized lifecycle states.
// The zero value ("") is not valid.
func (s State) Valid() bool {
	switch s {
	case StateUnknown, StateStarting, StateRunning, StatePaused,
		StateStopping, StateStopped, StateFailed:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether the state is a terminal lifecycle state.
// Terminal states are [StateStopped] and [StateFailed]. An agent in a
// terminal state is not processing work and must be restarted to resume
// operation.
func (s State) IsTerminal() bool {
	switch s {
	case StateStopped, StateFailed:
		return true
	default:
		return false
	}
}

// validTransitions defines the allowed state transitions for the agent
// lifecycle state machine. Each key is a source state, and the value is the
// set of states it may transition to. Transitions not present in this map
// are rejected by [ValidTransition].
//
// Transition matrix:
//
//	Unknown  → Starting, Failed
//	Starting → Running, Failed, Stopping
//	Running  → Paused, Stopping, Failed
//	Paused   → Running, Stopping, Failed
//	Stopping → Stopped, Failed
//	Stopped  → Starting              (restart)
//	Failed   → Starting              (recovery restart)
var validTransitions = map[State][]State{
	StateUnknown:  {StateStarting, StateFailed},
	StateStarting: {StateRunning, StateFailed, StateStopping},
	StateRunning:  {StatePaused, StateStopping, StateFailed},
	StatePaused:   {StateRunning, StateStopping, StateFailed},
	StateStopping: {StateStopped, StateFailed},
	StateStopped:  {StateStarting},
	StateFailed:   {StateStarting},
}

// ValidTransition reports whether transitioning from state from to state to
// is allowed by the lifecycle state machine. Both from and to must be valid
// states, and the transition must be present in the [validTransitions]
// matrix. Same-state transitions (from == to) are always rejected.
func ValidTransition(from, to State) bool {
	if from == to {
		return false
	}
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}
