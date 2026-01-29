# Agent Lifecycle Management

This document describes the agent lifecycle system provided by the
`pkg/lifecycle` package. It covers the state machine, interfaces,
base implementation, hooks, capabilities, observability, and security
considerations for agents running on the StricklySoft Cloud Platform.

## Overview

Every agent on the platform implements the `Agent` interface from
`pkg/lifecycle`. This interface provides a uniform contract for:

- **Lifecycle management** — Start, Stop, Pause, Resume
- **Health reporting** — Health checks for readiness and liveness probes
- **Capability discovery** — Declaring what an agent can do
- **Identity** — Unique ID, name, and version for each agent instance

The package ships a ready-to-use `BaseAgent` implementation that handles
thread-safe state management, OpenTelemetry tracing, structured logging,
and lifecycle hook dispatch. Concrete agents embed `BaseAgent` and
register domain-specific hooks via the `BaseAgentBuilder`.

## State Machine

Agents follow a finite state machine with seven states and validated
transitions. Invalid transitions are rejected at runtime with a
`CodeConflict` error.

### States

| State      | Description                                         | Terminal |
|------------|-----------------------------------------------------|----------|
| `unknown`  | Initial state before any lifecycle method is called  | No       |
| `starting` | Agent is initializing (transient)                    | No       |
| `running`  | Agent is operational and processing work             | No       |
| `paused`   | Agent is suspended; resources retained               | No       |
| `stopping` | Agent is shutting down (transient)                   | No       |
| `stopped`  | Clean shutdown completed                             | Yes      |
| `failed`   | Unrecoverable error encountered                      | Yes      |

### Transition Matrix

```
Unknown  --> Starting, Failed
Starting --> Running, Failed, Stopping
Running  --> Paused, Stopping, Failed
Paused   --> Running, Stopping, Failed
Stopping --> Stopped, Failed
Stopped  --> Starting  (restart)
Failed   --> Starting  (recovery restart)
```

### Lifecycle Flow (happy path)

```
Unknown --> Starting --> Running --> Stopping --> Stopped
```

### Pause/Resume Flow

```
Running --> Paused --> Running
```

### Restart from Terminal State

```
Stopped --> Starting --> Running
Failed  --> Starting --> Running
```

### Same-State Transitions

Same-state transitions (e.g., `Running -> Running`) are always rejected.

### Programmatic State Changes

Use `SetState()` for programmatic transitions (e.g., marking an agent
as `Failed` when an internal error is detected). `SetState()` validates
the transition before applying it.

## Agent Interface

The `Agent` interface defines the full contract:

```go
type Agent interface {
    // Identity
    ID() string
    Name() string
    Version() string
    Info() AgentInfo

    // Lifecycle
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Pause(ctx context.Context) error
    Resume(ctx context.Context) error

    // State
    State() State
    Capabilities() []Capability

    // Health
    Health(ctx context.Context) error
}
```

All methods are safe for concurrent use by multiple goroutines.

## BaseAgent

`BaseAgent` is the platform's reference implementation of `Agent`. It
provides:

- **Thread-safe state management** via `sync.RWMutex`
- **State transition validation** against the transition matrix
- **Lifecycle hooks** (OnStart, OnStop, OnPause, OnResume)
- **State change observers** notified on every transition
- **OpenTelemetry spans** for all lifecycle operations
- **Structured logging** via `log/slog`
- **Defensive copying** of capabilities and metadata
- **Panic recovery** in state change handlers

### Construction

Use `BaseAgentBuilder` (fluent API) to construct a `BaseAgent`:

```go
agent, err := lifecycle.NewBaseAgentBuilder("agent-001", "research-agent", "1.0.0").
    WithCapability(lifecycle.Capability{
        Name:    "web-search",
        Version: "1.0.0",
    }).
    WithOnStart(func(ctx context.Context) error {
        return db.Health(ctx) // verify dependencies before accepting work
    }).
    WithOnStop(func(ctx context.Context) error {
        db.Close()
        return nil
    }).
    WithLogger(logger).
    OnStateChange(func(old, new lifecycle.State) {
        metrics.AgentStateTransition(old, new)
    }).
    Build()
if err != nil {
    return err
}
```

### Required Fields

| Field     | Description                           |
|-----------|---------------------------------------|
| `id`      | Unique instance identifier            |
| `name`    | Human-readable agent type name        |
| `version` | Semantic version of the implementation|

All three are validated during `Build()`. Empty values produce a
`CodeValidation` error.

### Optional Configuration

| Method             | Description                              |
|--------------------|------------------------------------------|
| `WithCapability`   | Add a single capability                  |
| `WithCapabilities` | Add multiple capabilities                |
| `WithLogger`       | Set a custom `*slog.Logger`              |
| `WithOnStart`      | Register start hook                      |
| `WithOnStop`       | Register stop hook                       |
| `WithOnPause`      | Register pause hook                      |
| `WithOnResume`     | Register resume hook                     |
| `OnStateChange`    | Register a state change observer         |

## Lifecycle Hooks

Hooks are functions with the signature:

```go
type Hook func(ctx context.Context) error
```

They execute at specific points in the lifecycle:

| Hook      | When                                       | On Error              |
|-----------|--------------------------------------------|-----------------------|
| OnStart   | After `Starting`, before `Running`         | Agent -> `Failed`     |
| OnStop    | After `Stopping`, before `Stopped`         | Agent -> `Failed`     |
| OnPause   | While still `Running`, before `Paused`     | Agent -> `Failed`     |
| OnResume  | While still `Paused`, before `Running`     | Agent -> `Failed`     |

### Hook Execution Rules

1. Hooks execute **outside** the state mutex to prevent deadlocks.
2. Hooks receive the caller's context, which may carry deadlines and
   cancellation signals.
3. If a hook returns an error, the agent transitions to `Failed` and
   the error is wrapped with `CodeInternal`.
4. Hooks may safely call read-only methods (`State()`, `Info()`) on the
   agent without causing deadlocks.

### Hook Execution Order

For `Start` and `Stop`, the agent transitions to a transient intermediate
state (`Starting`, `Stopping`) before the hook runs, then transitions to
the final state (`Running`, `Stopped`) after the hook succeeds.

For `Pause` and `Resume`, the agent's state is validated (must be
`Running` or `Paused` respectively) and the hook runs **before** the
state transition occurs. This means:

- The `OnPause` hook sees `StateRunning` when it executes
- The `OnResume` hook sees `StatePaused` when it executes
- External observers only see the final state (`Paused`, `Running`)
  after the hook completes successfully

This design ensures that hook failures do not leave the agent in an
inconsistent state visible to external consumers.

## State Change Observers

State change observers have the signature:

```go
type StateChangeHandler func(old, new State)
```

Observers are called synchronously under the state mutex on every
successful state transition. Multiple observers are called in
registration order.

### Observer Rules

1. Observers must not block for extended periods.
2. Observers must not call lifecycle methods on the same agent
   (deadlock).
3. Panicking observers are recovered and logged without preventing the
   state change.
4. Typical uses: emitting metrics, updating registries, triggering
   alerts.

## Capabilities

Capabilities declare what an agent can do:

```go
type Capability struct {
    Name        string            `json:"name"`
    Version     string            `json:"version"`
    Description string            `json:"description"`
    Metadata    map[string]string `json:"metadata,omitempty"`
}
```

### Construction

Use `NewCapability()` for validated construction with defensive metadata
copying. Returns a `*sserr.Error` with `CodeValidation` if `Name` or
`Version` is empty:

```go
cap, err := lifecycle.NewCapability(
    "model-execution",
    "1.2.0",
    "Execute LLM inference requests",
    map[string]string{"max_tokens": "8192", "provider": "anthropic"},
)
```

### Builder Validation

Capabilities registered via `WithCapability()` or `WithCapabilities()`
are validated during `Build()`. If any capability has an empty `Name` or
`Version`, `Build()` returns a `CodeValidation` error. This ensures
invalid capabilities are caught at construction time rather than at
runtime.

### Defensive Copying

Capabilities use defensive copying throughout:
- `NewCapability()` copies the metadata map on construction
- `Clone()` produces a deep copy with an independent metadata map
- `Capabilities()` returns cloned capabilities from `BaseAgent`
- `Info()` includes cloned capabilities in the snapshot
- `BaseAgentBuilder.Build()` clones all capabilities at construction

This prevents external mutation from affecting agent state.

## Health Checks

The default `Health()` implementation returns `nil` if the agent is in
`StateRunning`, or a `CodeUnavailable` error otherwise.

Concrete agents may override `Health()` to add deeper checks:

```go
func (ra *ResearchAgent) Health(ctx context.Context) error {
    if err := ra.BaseAgent.Health(ctx); err != nil {
        return err
    }
    return ra.db.Health(ctx) // verify database connectivity
}
```

## AgentInfo

`Info()` returns a point-in-time snapshot:

```go
type AgentInfo struct {
    ID           string        `json:"id"`
    Name         string        `json:"name"`
    Version      string        `json:"version"`
    State        State         `json:"state"`
    Capabilities []Capability  `json:"capabilities"`
    StartedAt    *time.Time    `json:"started_at,omitempty"`
    Uptime       time.Duration `json:"uptime,omitempty"`
}
```

- `StartedAt` is set when the agent enters `Running` and cleared on
  `Stop`. It is `nil` before the first start.
- `Uptime` is computed at call time from `StartedAt`. It is zero when
  the agent is not running.
- All mutable fields (capabilities, timestamps) are deep-copied.

## Observability

### OpenTelemetry Tracing

Every lifecycle operation (`Start`, `Stop`, `Pause`, `Resume`) creates
an OpenTelemetry span with the instrumentation scope:

```
github.com/StricklySoft/stricklysoft-core/pkg/lifecycle
```

Span attributes include `agent.id` and `agent.name`. On error, the span
records the error and sets the status to `codes.Error`.

### Structured Logging

All lifecycle events are logged via `log/slog` with structured fields:

| Level | Events                                                |
|-------|-------------------------------------------------------|
| INFO  | Agent starting, started, stopping, stopped, pausing,  |
|       | paused, resuming, resumed                             |
| ERROR | Hook failures, state change handler panics            |

Log messages follow the format `"lifecycle: <verb> <subject>"` with
fields `agent_id`, `agent_name`, `agent_version`, and `error` as
applicable.

## Thread Safety

- **Immutable fields** (`id`, `name`, `version`, hooks, tracer, logger)
  are set at construction and never modified. No mutex needed.
- **Mutable fields** (`state`, `capabilities`, `startedAt`) are
  protected by `sync.RWMutex`.
- All public methods are safe for concurrent use.
- Lifecycle methods (`Start`, `Stop`, `Pause`, `Resume`) use `SetState`
  for atomic state transitions.
- `Start` and `Stop` use `setStateLocked` to atomically update both
  the state and `startedAt` under the same lock acquisition, preventing
  a window where `Info()` could see `StateRunning` with nil `startedAt`.
- Race condition tests run with `-race` flag in CI.

## Error Handling

All errors use the platform error package (`pkg/errors`):

| Code                | When                                     |
|---------------------|------------------------------------------|
| `CodeValidation`    | Builder validation failure (empty fields)|
| `CodeConflict`      | Invalid state transition                 |
| `CodeTimeout`       | Context canceled before operation        |
| `CodeInternal`      | Lifecycle hook failure                   |
| `CodeUnavailable`   | Health check on non-running agent        |

## Security Considerations

1. **No credential storage** — Agents do not store credentials. Hook
   functions that need secrets should retrieve them from the platform's
   secret management system at startup.
2. **Defensive copying** — All public accessors return deep copies to
   prevent callers from mutating internal state.
3. **Panic recovery** — State change handlers that panic are recovered
   and logged. A panicking handler cannot crash the agent or corrupt
   state.
4. **Context propagation** — All lifecycle methods accept `context.Context`
   for deadline enforcement and identity propagation.
5. **Immutable identity** — Agent ID, name, and version are set at
   construction and cannot be changed, preventing impersonation.
6. **Input validation** — Builder validates all required fields. Invalid
   inputs are rejected before construction.

## Example: Concrete Agent

```go
type ResearchAgent struct {
    *lifecycle.BaseAgent
    db *postgres.Client
}

func NewResearchAgent(db *postgres.Client) (*ResearchAgent, error) {
    ra := &ResearchAgent{db: db}
    base, err := lifecycle.NewBaseAgentBuilder(
        "research-001", "research-agent", "1.0.0",
    ).
        WithCapability(lifecycle.Capability{
            Name: "web-search", Version: "1.0.0",
        }).
        WithOnStart(ra.onStart).
        WithOnStop(ra.onStop).
        Build()
    if err != nil {
        return nil, err
    }
    ra.BaseAgent = base
    return ra, nil
}

func (ra *ResearchAgent) onStart(ctx context.Context) error {
    return ra.db.Health(ctx)
}

func (ra *ResearchAgent) onStop(ctx context.Context) error {
    ra.db.Close()
    return nil
}

func (ra *ResearchAgent) Health(ctx context.Context) error {
    if err := ra.BaseAgent.Health(ctx); err != nil {
        return err
    }
    return ra.db.Health(ctx)
}
```

## File Structure

```
pkg/lifecycle/
    state.go              State enum, transition matrix, ValidTransition()
    capability.go         Capability struct, NewCapability(), Clone(), validateCapability()
    agent.go              Agent interface, AgentInfo, BaseAgent, lifecycle methods
    agent_builder.go      BaseAgentBuilder (fluent API for constructing BaseAgent)
    state_test.go         State and transition tests
    capability_test.go    Capability construction and serialization tests
    agent_test.go         Agent lifecycle, concurrency, and integration tests
    agent_builder_test.go Builder pattern, validation, and capability tests
```
