# Core Data Models

This document describes the core data models provided by the
`pkg/models` package. It covers the Execution model, its lifecycle
states, constructor, validation, methods, serialization tags, and
security considerations for data shared across all StricklySoft
platform agents.

## Overview

The `pkg/models` package defines the central data structures shared
across all platform agents. These models are designed for:

- **JSON serialization** -- API request/response payloads via `json` struct tags
- **Database persistence** -- PostgreSQL column mapping via `db` struct tags (sqlx)
- **Cross-service transport** -- Uniform representation across Vigil, agents, and internal APIs

The primary model today is `Execution`, which tracks every AI action on
the platform. Additional models (audit events, memory, policy) are
planned and have placeholder files reserved in the package.

## Schema Versioning

```go
const ExecutionSchemaVersion = 1
```

`ExecutionSchemaVersion` identifies the current schema version of the
Execution model. Increment this constant when making breaking changes
to the struct fields or serialization format. Consumers can use this
value to detect schema mismatches and trigger migrations.

## ExecutionStatus

`ExecutionStatus` is a `string` type that represents the lifecycle
state of an AI execution. Executions begin in `pending` and progress
through the lifecycle until reaching a terminal state.

### Status Values

| Constant                    | Value         | Description                                | Terminal |
|-----------------------------|---------------|--------------------------------------------|----------|
| `ExecutionStatusPending`    | `"pending"`   | Created but not yet started (initial state)| No       |
| `ExecutionStatusRunning`    | `"running"`   | Actively being processed by an agent       | No       |
| `ExecutionStatusCompleted`  | `"completed"` | Finished successfully                      | Yes      |
| `ExecutionStatusFailed`     | `"failed"`    | Encountered an error                       | Yes      |
| `ExecutionStatusCanceled`   | `"canceled"`  | Canceled by user or system action          | Yes      |
| `ExecutionStatusTimeout`    | `"timeout"`   | Exceeded allowed time limit                | Yes      |

### ExecutionStatus Methods

| Method         | Signature                       | Description                                                       |
|----------------|---------------------------------|-------------------------------------------------------------------|
| `String`       | `String() string`               | Returns the string representation of the status                   |
| `Valid`        | `Valid() bool`                   | Reports whether the status is one of the six recognized values    |
| `IsTerminal`   | `IsTerminal() bool`             | Reports whether the status is a final state (`completed`, `failed`, `canceled`, `timeout`) |

### Usage

```go
status := models.ExecutionStatusRunning

fmt.Println(status.String())     // "running"
fmt.Println(status.Valid())      // true
fmt.Println(status.IsTerminal()) // false

status = models.ExecutionStatusCompleted
fmt.Println(status.IsTerminal()) // true

status = ExecutionStatus("invalid")
fmt.Println(status.Valid())      // false
```

## Execution Lifecycle

Executions follow a linear lifecycle with one non-terminal progression
and four terminal outcomes:

```
pending --> running --> completed
                    --> failed
                    --> canceled
                    --> timeout
```

Once an execution reaches a terminal state (`completed`, `failed`,
`canceled`, `timeout`), it cannot transition to another state. The
`IsTerminal()` method on both `ExecutionStatus` and `Execution`
identifies terminal states.

**Note:** Status transition validation is the responsibility of the
Vigil service, not this model. The model defines the states and
identifies terminal states, but does not enforce the transition matrix.

## Execution Struct

The `Execution` struct represents a single AI execution event. It is
the core record type that Vigil creates and all agents reference for
tracking AI actions.

Every field is annotated with both `json` tags (for API serialization)
and `db` tags (for sqlx database mapping). Optional fields use
`omitempty` to exclude zero values from serialized output.

```go
type Execution struct {
    ID           string            `json:"id" db:"id"`
    IdentityID   string            `json:"identity_id" db:"identity_id"`
    Intent       string            `json:"intent" db:"intent"`
    Status       ExecutionStatus   `json:"status" db:"status"`
    StartTime    time.Time         `json:"start_time" db:"start_time"`
    EndTime      *time.Time        `json:"end_time,omitempty" db:"end_time"`
    PodName      string            `json:"pod_name,omitempty" db:"pod_name"`
    Namespace    string            `json:"namespace" db:"namespace"`
    Model        string            `json:"model,omitempty" db:"model"`
    TokensUsed   int               `json:"tokens_used,omitempty" db:"tokens_used"`
    ErrorMessage string            `json:"error_message,omitempty" db:"error_message"`
    Metadata     map[string]any    `json:"metadata" db:"metadata"`
    CreatedAt    time.Time         `json:"created_at" db:"created_at"`
    UpdatedAt    time.Time         `json:"updated_at" db:"updated_at"`
}
```

### Field Reference

| Field          | Type               | JSON Key          | DB Column       | Required | Omitempty | Description                                                        |
|----------------|--------------------|-------------------|-----------------|----------|-----------|--------------------------------------------------------------------|
| `ID`           | `string`           | `id`              | `id`            | Yes      | No        | Unique identifier (UUID v4), generated by `NewExecution`           |
| `IdentityID`   | `string`           | `identity_id`     | `identity_id`   | Yes      | No        | ID of the authenticated identity that initiated this execution     |
| `Intent`       | `string`           | `intent`          | `intent`        | Yes      | No        | Original prompt or action description that triggered the execution |
| `Status`       | `ExecutionStatus`  | `status`          | `status`        | Yes      | No        | Current lifecycle state (see ExecutionStatus)                      |
| `StartTime`    | `time.Time`        | `start_time`      | `start_time`    | Yes      | No        | UTC timestamp when the execution began processing                  |
| `EndTime`      | `*time.Time`       | `end_time`        | `end_time`      | No       | Yes       | UTC timestamp when the execution reached a terminal state          |
| `PodName`      | `string`           | `pod_name`        | `pod_name`      | No       | Yes       | Kubernetes pod name where the execution is running                 |
| `Namespace`    | `string`           | `namespace`       | `namespace`     | Yes      | No        | Kubernetes namespace or deployment environment                     |
| `Model`        | `string`           | `model`           | `model`         | No       | Yes       | AI model identifier (e.g., `"gpt-4"`, `"claude-3-opus"`)          |
| `TokensUsed`   | `int`              | `tokens_used`     | `tokens_used`   | No       | Yes       | Total tokens consumed (input + output); must be >= 0               |
| `ErrorMessage` | `string`           | `error_message`   | `error_message` | No       | Yes       | Error details when the execution has failed                        |
| `Metadata`     | `map[string]any`   | `metadata`        | `metadata`      | No       | No        | Extensible key-value store for agent-specific data                 |
| `CreatedAt`    | `time.Time`        | `created_at`      | `created_at`    | Yes      | No        | UTC timestamp when the record was created                          |
| `UpdatedAt`    | `time.Time`        | `updated_at`      | `updated_at`    | Yes      | No        | UTC timestamp when the record was last modified                    |

### Mutability

Execution records are created via `NewExecution` and are considered
immutable after creation except for status-related updates:

| Mutable Fields   | Updated When                         |
|------------------|--------------------------------------|
| `Status`         | Execution progresses through lifecycle |
| `EndTime`        | Execution reaches a terminal state   |
| `TokensUsed`     | Agent reports token consumption      |
| `ErrorMessage`   | Execution fails                      |
| `Metadata`       | Agent attaches additional data       |
| `UpdatedAt`      | Any status change occurs             |

## Constructor

### NewExecution

```go
func NewExecution(identityID, intent, namespace string) (*Execution, error)
```

Creates a new `Execution` record with sensible defaults. This is the
only recommended way to create Execution instances.

**Parameters:**

| Parameter    | Type     | Description                                      |
|--------------|----------|--------------------------------------------------|
| `identityID` | `string` | ID of the authenticated identity (must not be empty) |
| `intent`     | `string` | Prompt or action description (must not be empty)  |
| `namespace`  | `string` | Kubernetes namespace (must not be empty)           |

**Returns:** `(*Execution, error)`

**Behavior:**

1. Validates that `identityID`, `intent`, and `namespace` are all non-empty.
   Returns an error if any is empty.
2. Generates a UUID v4 for the `ID` field.
3. Sets `Status` to `ExecutionStatusPending`.
4. Sets `StartTime`, `CreatedAt`, and `UpdatedAt` to `time.Now().UTC()`.
5. Initializes `Metadata` to an empty map (`make(map[string]any)`).
6. All other fields are left at their zero values.

**Errors:**

| Condition              | Error Message                                      |
|------------------------|----------------------------------------------------|
| `identityID` is empty  | `"models: execution identityID must not be empty"` |
| `intent` is empty      | `"models: execution intent must not be empty"`     |
| `namespace` is empty   | `"models: execution namespace must not be empty"`  |

**Example:**

```go
exec, err := models.NewExecution(
    "user-abc-123",
    "Summarize the quarterly earnings report",
    "production",
)
if err != nil {
    return fmt.Errorf("failed to create execution: %w", err)
}

fmt.Println(exec.ID)        // e.g., "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
fmt.Println(exec.Status)    // "pending"
fmt.Println(exec.Metadata)  // map[]
```

## Methods

### Validate

```go
func (e *Execution) Validate() error
```

Checks that all required fields are present and that the execution is
in a consistent state. Returns the first validation error encountered,
or `nil` if the execution is valid.

**Validation Rules:**

| Check                         | Error Message                                                  |
|-------------------------------|----------------------------------------------------------------|
| `ID` is empty                 | `"models: execution ID is required"`                          |
| `IdentityID` is empty        | `"models: execution identity ID is required"`                 |
| `Intent` is empty            | `"models: execution intent is required"`                      |
| `Namespace` is empty         | `"models: execution namespace is required"`                   |
| `Status` is not valid        | `"models: invalid execution status \"<value>\""`              |
| `StartTime` is zero          | `"models: execution start time is required"`                  |
| `CreatedAt` is zero          | `"models: execution created_at is required"`                  |
| `UpdatedAt` is zero          | `"models: execution updated_at is required"`                  |
| `TokensUsed` is negative     | `"models: execution tokens_used must not be negative, got <n>"` |

**Example:**

```go
exec, _ := models.NewExecution("user-1", "search the web", "default")

// Valid immediately after construction
if err := exec.Validate(); err != nil {
    log.Fatal(err) // will not be reached
}

// Manually constructed with missing fields
bad := &models.Execution{Status: "invalid"}
if err := bad.Validate(); err != nil {
    fmt.Println(err) // "models: execution ID is required"
}
```

### IsTerminal

```go
func (e *Execution) IsTerminal() bool
```

Reports whether the execution has reached a final state from which no
further transitions are possible. Delegates to `e.Status.IsTerminal()`.

**Return values by status:**

| Status        | IsTerminal |
|---------------|------------|
| `pending`     | `false`    |
| `running`     | `false`    |
| `completed`   | `true`     |
| `failed`      | `true`     |
| `canceled`    | `true`     |
| `timeout`     | `true`     |

**Example:**

```go
exec, _ := models.NewExecution("user-1", "analyze data", "staging")

fmt.Println(exec.IsTerminal()) // false (status is "pending")

exec.Status = models.ExecutionStatusCompleted
fmt.Println(exec.IsTerminal()) // true
```

### Duration

```go
func (e *Execution) Duration() time.Duration
```

Returns the wall-clock duration of the execution.

**Behavior:**

| Condition                        | Return Value                        |
|----------------------------------|-------------------------------------|
| `StartTime` is zero              | `0`                                 |
| `EndTime` is set (non-nil)       | `EndTime.Sub(StartTime)`            |
| `EndTime` is nil (still running) | `time.Since(StartTime)`             |

**Example:**

```go
exec, _ := models.NewExecution("user-1", "run analysis", "production")

// While running, duration is computed from StartTime to now
fmt.Println(exec.Duration()) // e.g., 42ms (time since creation)

// After completion, duration is fixed
end := exec.StartTime.Add(5 * time.Second)
exec.EndTime = &end
fmt.Println(exec.Duration()) // 5s
```

## JSON Serialization

Executions serialize to JSON with the field names defined by struct
tags. Fields marked with `omitempty` are excluded when they hold their
zero value.

### Example JSON (newly created execution)

```json
{
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "identity_id": "user-abc-123",
    "intent": "Summarize the quarterly earnings report",
    "status": "pending",
    "start_time": "2025-03-15T14:30:00Z",
    "namespace": "production",
    "metadata": {},
    "created_at": "2025-03-15T14:30:00Z",
    "updated_at": "2025-03-15T14:30:00Z"
}
```

Note that `end_time`, `pod_name`, `model`, `tokens_used`, and
`error_message` are omitted because they are zero values and their
struct tags include `omitempty`.

### Example JSON (completed execution)

```json
{
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "identity_id": "user-abc-123",
    "intent": "Summarize the quarterly earnings report",
    "status": "completed",
    "start_time": "2025-03-15T14:30:00Z",
    "end_time": "2025-03-15T14:30:12Z",
    "pod_name": "vigil-worker-7b9f4",
    "namespace": "production",
    "model": "claude-3-opus",
    "tokens_used": 2048,
    "metadata": {
        "source": "api",
        "priority": "high"
    },
    "created_at": "2025-03-15T14:30:00Z",
    "updated_at": "2025-03-15T14:30:12Z"
}
```

## Database Mapping

Every field carries a `db` struct tag for use with `sqlx`. The tag
values correspond to PostgreSQL column names. The mapping is
straightforward -- each `db` tag matches the `json` tag with
underscores.

| Go Field       | DB Column        | PostgreSQL Type (recommended) |
|----------------|------------------|-------------------------------|
| `ID`           | `id`             | `UUID PRIMARY KEY`            |
| `IdentityID`   | `identity_id`    | `UUID NOT NULL`               |
| `Intent`       | `intent`         | `TEXT NOT NULL`               |
| `Status`       | `status`         | `TEXT NOT NULL`               |
| `StartTime`    | `start_time`     | `TIMESTAMPTZ NOT NULL`        |
| `EndTime`      | `end_time`       | `TIMESTAMPTZ`                 |
| `PodName`      | `pod_name`       | `TEXT`                        |
| `Namespace`    | `namespace`      | `TEXT NOT NULL`               |
| `Model`        | `model`          | `TEXT`                        |
| `TokensUsed`   | `tokens_used`    | `INTEGER DEFAULT 0`          |
| `ErrorMessage` | `error_message`  | `TEXT`                        |
| `Metadata`     | `metadata`       | `JSONB DEFAULT '{}'::jsonb`   |
| `CreatedAt`    | `created_at`     | `TIMESTAMPTZ NOT NULL`        |
| `UpdatedAt`    | `updated_at`     | `TIMESTAMPTZ NOT NULL`        |

## Complete Example

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "time"

    "github.com/StricklySoft/stricklysoft-core/pkg/models"
)

func main() {
    // Create a new execution
    exec, err := models.NewExecution(
        "identity-550e8400",
        "Search for recent AI safety papers and summarize findings",
        "production",
    )
    if err != nil {
        log.Fatalf("failed to create execution: %v", err)
    }

    // Validate the execution
    if err := exec.Validate(); err != nil {
        log.Fatalf("invalid execution: %v", err)
    }

    // Check initial state
    fmt.Println("Status:", exec.Status)          // "pending"
    fmt.Println("Terminal:", exec.IsTerminal())   // false
    fmt.Println("Duration:", exec.Duration())     // time since creation

    // Simulate lifecycle progression
    exec.Status = models.ExecutionStatusRunning
    exec.PodName = "vigil-worker-7b9f4"
    exec.Model = "claude-3-opus"
    exec.UpdatedAt = time.Now().UTC()

    // Simulate completion
    now := time.Now().UTC()
    exec.Status = models.ExecutionStatusCompleted
    exec.EndTime = &now
    exec.TokensUsed = 3500
    exec.Metadata["source"] = "research-agent"
    exec.Metadata["paper_count"] = 12
    exec.UpdatedAt = now

    fmt.Println("Terminal:", exec.IsTerminal())   // true
    fmt.Println("Duration:", exec.Duration())     // fixed duration

    // Serialize to JSON
    data, _ := json.MarshalIndent(exec, "", "    ")
    fmt.Println(string(data))
}
```

## Placeholder Models

The following files are reserved in the package for future model
definitions. They currently contain only the `package models`
declaration.

| File                | Planned Purpose                                     |
|---------------------|-----------------------------------------------------|
| `audit.go`          | Audit event model for tracking platform operations  |
| `memory.go`         | Memory model for agent context and conversation history |
| `policy.go`         | Policy model for access control and governance rules |
| `serialization.go`  | Shared serialization utilities for model encoding   |
| `validation.go`     | Shared validation utilities for model field checks  |

These files will be implemented in future sprints as the platform
evolves.

## Security Considerations

1. **Identity linkage** -- `IdentityID` links every execution to an
   `auth.Identity`, creating a complete audit trail of who initiated
   each AI action. This field is required and validated by both the
   constructor and `Validate()`.

2. **Namespace-scoped access** -- `Namespace` enables Kubernetes-scoped
   access control. Agents and services can filter executions by
   namespace to enforce tenant isolation. This field is required and
   cannot be empty.

3. **Extensible metadata** -- The `Metadata` map (`map[string]any`)
   allows agents to attach domain-specific data without modifying the
   Execution schema. This avoids schema churn while supporting diverse
   agent requirements. The constructor initializes metadata to an empty
   map to ensure consistent JSON serialization.

4. **Status transition enforcement** -- While the model defines valid
   states and identifies terminal states, status transition validation
   is the responsibility of the Vigil service. This separation keeps
   the model portable across services while centralizing business rules
   in the orchestrator.

5. **Input validation** -- Both `NewExecution()` and `Validate()` reject
   empty required fields. `Validate()` additionally checks for negative
   `TokensUsed` values and unrecognized status values, preventing
   invalid data from entering the system.

6. **UUID generation** -- Execution IDs are generated using UUID v4
   (`github.com/google/uuid`), providing 122 bits of randomness. IDs
   are not guessable and are safe for use in URLs and cross-service
   references.

## File Structure

```
pkg/models/
    execution.go       Execution struct, ExecutionStatus, NewExecution, Validate, Duration
    execution_test.go  Execution construction, validation, lifecycle, and serialization tests
    audit.go           Placeholder for audit event model
    memory.go          Placeholder for memory model
    policy.go          Placeholder for policy model
    serialization.go   Placeholder for serialization utilities
    validation.go      Placeholder for validation utilities
```
