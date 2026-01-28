// Package models defines the core data models for the StricklySoft platform.
//
// The models in this package represent the central data structures shared
// across all platform agents. They are designed for serialization (JSON),
// database persistence (sqlx), and cross-service transport.
//
// Execution Model:
//
// The [Execution] type represents a single AI execution event — the core
// record that Vigil creates and all agents reference for tracking AI actions.
// Every AI execution in the platform is tracked with a unique Execution
// record that connects identity, intent, status, and outcomes.
//
// An Execution flows through a defined lifecycle:
//
//	pending → running → completed
//	                  → failed
//	                  → canceled
//	                  → timeout
//
// Once an execution reaches a terminal state (completed, failed, canceled,
// timeout), it cannot transition to another state. The [Execution.IsTerminal]
// method identifies terminal states.
package models

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ExecutionSchemaVersion identifies the current schema version of the
// Execution model. Increment this when making breaking changes to the
// struct fields or serialization format to support schema migration.
const ExecutionSchemaVersion = 1

// ExecutionStatus represents the lifecycle state of an AI execution.
// Executions begin in [ExecutionStatusPending] and progress through the
// lifecycle until reaching a terminal state.
type ExecutionStatus string

const (
	// ExecutionStatusPending indicates the execution has been created but
	// has not yet started processing. This is the initial state set by
	// [NewExecution].
	ExecutionStatusPending ExecutionStatus = "pending"

	// ExecutionStatusRunning indicates the execution is actively being
	// processed by an agent.
	ExecutionStatusRunning ExecutionStatus = "running"

	// ExecutionStatusCompleted indicates the execution finished
	// successfully. This is a terminal state.
	ExecutionStatusCompleted ExecutionStatus = "completed"

	// ExecutionStatusFailed indicates the execution encountered an error
	// and could not complete. This is a terminal state. The error details
	// are recorded in [Execution.ErrorMessage].
	ExecutionStatusFailed ExecutionStatus = "failed"

	// ExecutionStatusCanceled indicates the execution was canceled by a
	// user or system action before completion. This is a terminal state.
	ExecutionStatusCanceled ExecutionStatus = "canceled"

	// ExecutionStatusTimeout indicates the execution exceeded its allowed
	// time limit and was terminated. This is a terminal state.
	ExecutionStatusTimeout ExecutionStatus = "timeout"
)

// String returns the string representation of the execution status.
func (s ExecutionStatus) String() string {
	return string(s)
}

// Valid reports whether the execution status is one of the recognized values.
func (s ExecutionStatus) Valid() bool {
	switch s {
	case ExecutionStatusPending, ExecutionStatusRunning,
		ExecutionStatusCompleted, ExecutionStatusFailed,
		ExecutionStatusCanceled, ExecutionStatusTimeout:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether this status represents a final state from
// which no further transitions are possible.
func (s ExecutionStatus) IsTerminal() bool {
	switch s {
	case ExecutionStatusCompleted, ExecutionStatusFailed,
		ExecutionStatusCanceled, ExecutionStatusTimeout:
		return true
	default:
		return false
	}
}

// Execution represents a single AI execution event in the StricklySoft
// platform. It is the core record type that Vigil creates and all agents
// reference for tracking AI actions.
//
// Every field is annotated with both JSON tags (for API serialization) and
// db tags (for sqlx database mapping). Optional fields use omitempty to
// exclude zero values from serialized output.
//
// Execution records are created via [NewExecution] and are immutable after
// creation except for status-related updates (Status, EndTime, TokensUsed,
// ErrorMessage, Metadata, UpdatedAt). Status transition validation is the
// responsibility of the Vigil service, not this model.
type Execution struct {
	// ID is the unique identifier for this execution (UUID v4).
	ID string `json:"id" db:"id"`

	// IdentityID is the ID of the authenticated identity (user or service)
	// that initiated this execution. Links to the auth.Identity system.
	IdentityID string `json:"identity_id" db:"identity_id"`

	// Intent is the original prompt or action description that triggered
	// this execution. This is the human-readable description of what the
	// AI was asked to do.
	Intent string `json:"intent" db:"intent"`

	// Status is the current lifecycle state of the execution.
	// See [ExecutionStatus] for valid values.
	Status ExecutionStatus `json:"status" db:"status"`

	// StartTime is the UTC timestamp when the execution began processing.
	// Set to the creation time by [NewExecution].
	StartTime time.Time `json:"start_time" db:"start_time"`

	// EndTime is the UTC timestamp when the execution reached a terminal
	// state. Nil while the execution is still pending or running.
	EndTime *time.Time `json:"end_time,omitempty" db:"end_time"`

	// PodName is the Kubernetes pod name where the execution is running.
	// Empty if the execution has not been scheduled or is not running
	// in Kubernetes.
	PodName string `json:"pod_name,omitempty" db:"pod_name"`

	// Namespace is the Kubernetes namespace or deployment environment
	// where the execution runs.
	Namespace string `json:"namespace" db:"namespace"`

	// Model is the AI model identifier used for this execution
	// (e.g., "gpt-4", "claude-3-opus"). Empty if not yet determined.
	Model string `json:"model,omitempty" db:"model"`

	// TokensUsed is the total number of tokens consumed by this execution
	// (input + output). Zero until the execution completes or reports
	// partial usage.
	TokensUsed int `json:"tokens_used,omitempty" db:"tokens_used"`

	// ErrorMessage contains the error details when the execution has
	// failed. Empty for non-failed executions.
	ErrorMessage string `json:"error_message,omitempty" db:"error_message"`

	// Metadata is an extensible key-value store for agent-specific data.
	// Each agent can attach its own metadata without modifying the
	// Execution schema. Nil metadata is normalized to an empty map
	// by [NewExecution], so this field is always present in JSON output
	// for constructor-created executions (at minimum as an empty object).
	Metadata map[string]any `json:"metadata" db:"metadata"`

	// CreatedAt is the UTC timestamp when the execution record was created.
	CreatedAt time.Time `json:"created_at" db:"created_at"`

	// UpdatedAt is the UTC timestamp when the execution record was last
	// modified. Updated on every status change.
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// NewExecution creates a new Execution record with a generated UUID, pending
// status, and UTC timestamps. The metadata map is initialized to an empty map.
//
// Returns an error if any required field (identityID, intent, namespace) is
// empty. These fields are required because they are essential for audit
// trails and cannot be meaningfully defaulted.
func NewExecution(identityID, intent, namespace string) (*Execution, error) {
	if identityID == "" {
		return nil, errors.New("models: execution identityID must not be empty")
	}
	if intent == "" {
		return nil, errors.New("models: execution intent must not be empty")
	}
	if namespace == "" {
		return nil, errors.New("models: execution namespace must not be empty")
	}

	now := time.Now().UTC()
	return &Execution{
		ID:         uuid.New().String(),
		IdentityID: identityID,
		Intent:     intent,
		Status:     ExecutionStatusPending,
		StartTime:  now,
		Namespace:  namespace,
		Metadata:   make(map[string]any),
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// Validate checks that all required fields are present and that the status
// is a recognized value. Returns the first validation error encountered,
// or nil if the execution is valid.
//
// Required fields: ID, IdentityID, Intent, Namespace, Status (must be valid).
// Timestamps (StartTime, CreatedAt, UpdatedAt) must not be zero.
func (e *Execution) Validate() error {
	if e.ID == "" {
		return errors.New("models: execution ID is required")
	}
	if e.IdentityID == "" {
		return errors.New("models: execution identity ID is required")
	}
	if e.Intent == "" {
		return errors.New("models: execution intent is required")
	}
	if e.Namespace == "" {
		return errors.New("models: execution namespace is required")
	}
	if !e.Status.Valid() {
		return fmt.Errorf("models: invalid execution status %q", e.Status)
	}
	if e.StartTime.IsZero() {
		return errors.New("models: execution start time is required")
	}
	if e.CreatedAt.IsZero() {
		return errors.New("models: execution created_at is required")
	}
	if e.UpdatedAt.IsZero() {
		return errors.New("models: execution updated_at is required")
	}
	if e.TokensUsed < 0 {
		return fmt.Errorf("models: execution tokens_used must not be negative, got %d", e.TokensUsed)
	}
	return nil
}

// IsTerminal reports whether the execution has reached a final state from
// which no further transitions are possible (completed, failed, canceled,
// or timeout).
func (e *Execution) IsTerminal() bool {
	return e.Status.IsTerminal()
}

// Duration returns the wall-clock duration of the execution. If the
// execution has an EndTime, the duration is calculated from StartTime to
// EndTime. If the execution is still in progress (EndTime is nil), the
// duration is calculated from StartTime to the current time.
//
// Returns zero if StartTime is zero.
func (e *Execution) Duration() time.Duration {
	if e.StartTime.IsZero() {
		return 0
	}
	if e.EndTime != nil {
		return e.EndTime.Sub(e.StartTime)
	}
	return time.Since(e.StartTime)
}
