package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustNewExecution creates an Execution, failing the test if construction
// returns an error.
func mustNewExecution(t *testing.T, identityID, intent, namespace string) *Execution {
	t.Helper()
	exec, err := NewExecution(identityID, intent, namespace)
	require.NoError(t, err, "NewExecution(%q, %q, %q) unexpected error", identityID, intent, namespace)
	return exec
}

// ---------------------------------------------------------------------------
// ExecutionStatus
// ---------------------------------------------------------------------------

func TestExecutionStatus_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		status   ExecutionStatus
		expected string
	}{
		{name: "pending", status: ExecutionStatusPending, expected: "pending"},
		{name: "running", status: ExecutionStatusRunning, expected: "running"},
		{name: "completed", status: ExecutionStatusCompleted, expected: "completed"},
		{name: "failed", status: ExecutionStatusFailed, expected: "failed"},
		{name: "canceled", status: ExecutionStatusCanceled, expected: "canceled"},
		{name: "timeout", status: ExecutionStatusTimeout, expected: "timeout"},
		{name: "custom", status: ExecutionStatus("custom"), expected: "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}

func TestExecutionStatus_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		status   ExecutionStatus
		expected bool
	}{
		{name: "pending is valid", status: ExecutionStatusPending, expected: true},
		{name: "running is valid", status: ExecutionStatusRunning, expected: true},
		{name: "completed is valid", status: ExecutionStatusCompleted, expected: true},
		{name: "failed is valid", status: ExecutionStatusFailed, expected: true},
		{name: "canceled is valid", status: ExecutionStatusCanceled, expected: true},
		{name: "timeout is valid", status: ExecutionStatusTimeout, expected: true},
		{name: "empty is invalid", status: ExecutionStatus(""), expected: false},
		{name: "unknown is invalid", status: ExecutionStatus("paused"), expected: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.status.Valid())
		})
	}
}

func TestExecutionStatus_IsTerminal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		status   ExecutionStatus
		expected bool
	}{
		{name: "pending is not terminal", status: ExecutionStatusPending, expected: false},
		{name: "running is not terminal", status: ExecutionStatusRunning, expected: false},
		{name: "completed is terminal", status: ExecutionStatusCompleted, expected: true},
		{name: "failed is terminal", status: ExecutionStatusFailed, expected: true},
		{name: "canceled is terminal", status: ExecutionStatusCanceled, expected: true},
		{name: "timeout is terminal", status: ExecutionStatusTimeout, expected: true},
		{name: "unknown is not terminal", status: ExecutionStatus("paused"), expected: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.status.IsTerminal())
		})
	}
}

// ---------------------------------------------------------------------------
// NewExecution
// ---------------------------------------------------------------------------

func TestNewExecution(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-123", "deploy application", "production")

	assert.NotEmpty(t, exec.ID, "ID should not be empty")
	assert.Equal(t, "user-123", exec.IdentityID)
	assert.Equal(t, "deploy application", exec.Intent)
	assert.Equal(t, ExecutionStatusPending, exec.Status)
	assert.Equal(t, "production", exec.Namespace)
	assert.NotNil(t, exec.Metadata, "Metadata should not be nil")
	assert.Len(t, exec.Metadata, 0)
	assert.Nil(t, exec.EndTime, "EndTime should be nil for a new execution")
	assert.Empty(t, exec.PodName, "PodName should be empty")
	assert.Empty(t, exec.Model, "Model should be empty")
	assert.Equal(t, 0, exec.TokensUsed)
	assert.Empty(t, exec.ErrorMessage, "ErrorMessage should be empty")
}

func TestNewExecution_GeneratesUniqueIDs(t *testing.T) {
	t.Parallel()
	exec1 := mustNewExecution(t, "user-1", "task-1", "ns")
	exec2 := mustNewExecution(t, "user-1", "task-1", "ns")

	assert.NotEqual(t, exec1.ID, exec2.ID, "two executions should have different IDs")
}

func TestNewExecution_UUIDFormat(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")

	// UUID v4 format: 8-4-4-4-12 hex characters.
	parts := strings.Split(exec.ID, "-")
	assert.Len(t, parts, 5, "ID %q does not have UUID format (expected 5 parts separated by hyphens)", exec.ID)
}

func TestNewExecution_TimestampsAreUTC(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")

	assert.Equal(t, time.UTC, exec.StartTime.Location())
	assert.Equal(t, time.UTC, exec.CreatedAt.Location())
	assert.Equal(t, time.UTC, exec.UpdatedAt.Location())
}

func TestNewExecution_TimestampsAreConsistent(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")

	assert.Equal(t, exec.CreatedAt, exec.StartTime, "StartTime and CreatedAt should be equal for a new execution")
	assert.Equal(t, exec.UpdatedAt, exec.StartTime, "StartTime and UpdatedAt should be equal for a new execution")
}

func TestNewExecution_EmptyIdentityID(t *testing.T) {
	t.Parallel()
	_, err := NewExecution("", "task", "ns")
	require.Error(t, err, "NewExecution with empty identityID should return an error")
	assert.Contains(t, err.Error(), "identityID")
}

func TestNewExecution_EmptyIntent(t *testing.T) {
	t.Parallel()
	_, err := NewExecution("user-1", "", "ns")
	require.Error(t, err, "NewExecution with empty intent should return an error")
	assert.Contains(t, err.Error(), "intent")
}

func TestNewExecution_EmptyNamespace(t *testing.T) {
	t.Parallel()
	_, err := NewExecution("user-1", "task", "")
	require.Error(t, err, "NewExecution with empty namespace should return an error")
	assert.Contains(t, err.Error(), "namespace")
}

func TestNewExecution_IsNotTerminal(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")

	assert.False(t, exec.IsTerminal(), "new execution should not be in a terminal state")
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func TestValidate_ValidExecution(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "deploy app", "production")
	assert.NoError(t, exec.Validate())
}

func TestValidate_EmptyID(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.ID = ""
	require.Error(t, exec.Validate(), "Validate() should return error for empty ID")
}

func TestValidate_EmptyIdentityID(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.IdentityID = ""
	require.Error(t, exec.Validate(), "Validate() should return error for empty IdentityID")
}

func TestValidate_EmptyIntent(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Intent = ""
	require.Error(t, exec.Validate(), "Validate() should return error for empty Intent")
}

func TestValidate_EmptyNamespace(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Namespace = ""
	require.Error(t, exec.Validate(), "Validate() should return error for empty Namespace")
}

func TestValidate_InvalidStatus(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatus("invalid")
	err := exec.Validate()
	require.Error(t, err, "Validate() should return error for invalid status")
	assert.Contains(t, err.Error(), "invalid")
}

func TestValidate_ZeroStartTime(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.StartTime = time.Time{}
	require.Error(t, exec.Validate(), "Validate() should return error for zero StartTime")
}

func TestValidate_ZeroCreatedAt(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.CreatedAt = time.Time{}
	require.Error(t, exec.Validate(), "Validate() should return error for zero CreatedAt")
}

func TestValidate_ZeroUpdatedAt(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.UpdatedAt = time.Time{}
	require.Error(t, exec.Validate(), "Validate() should return error for zero UpdatedAt")
}

func TestValidate_NegativeTokensUsed(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.TokensUsed = -1
	err := exec.Validate()
	require.Error(t, err, "Validate() should return error for negative TokensUsed")
	assert.Contains(t, err.Error(), "negative")
}

func TestValidate_ZeroTokensUsed(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.TokensUsed = 0
	assert.NoError(t, exec.Validate(), "Validate() should not return error for zero TokensUsed")
}

func TestValidate_AllStatuses(t *testing.T) {
	t.Parallel()
	statuses := []ExecutionStatus{
		ExecutionStatusPending,
		ExecutionStatusRunning,
		ExecutionStatusCompleted,
		ExecutionStatusFailed,
		ExecutionStatusCanceled,
		ExecutionStatusTimeout,
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			exec := mustNewExecution(t, "user-1", "task", "ns")
			exec.Status = status
			assert.NoError(t, exec.Validate(), "Validate() returned error for valid status %q", status)
		})
	}
}

func TestValidate_EmptyExecution(t *testing.T) {
	t.Parallel()
	exec := &Execution{}
	err := exec.Validate()
	require.Error(t, err, "Validate() should return error for empty execution")
}

// ---------------------------------------------------------------------------
// IsTerminal
// ---------------------------------------------------------------------------

func TestIsTerminal_PendingExecution(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusPending
	assert.False(t, exec.IsTerminal(), "pending execution should not be terminal")
}

func TestIsTerminal_RunningExecution(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusRunning
	assert.False(t, exec.IsTerminal(), "running execution should not be terminal")
}

func TestIsTerminal_CompletedExecution(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusCompleted
	assert.True(t, exec.IsTerminal(), "completed execution should be terminal")
}

func TestIsTerminal_FailedExecution(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusFailed
	assert.True(t, exec.IsTerminal(), "failed execution should be terminal")
}

func TestIsTerminal_CanceledExecution(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusCanceled
	assert.True(t, exec.IsTerminal(), "canceled execution should be terminal")
}

func TestIsTerminal_TimeoutExecution(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusTimeout
	assert.True(t, exec.IsTerminal(), "timeout execution should be terminal")
}

// ---------------------------------------------------------------------------
// Duration
// ---------------------------------------------------------------------------

func TestDuration_WithEndTime(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	endTime := exec.StartTime.Add(5 * time.Second)
	exec.EndTime = &endTime

	d := exec.Duration()
	assert.Equal(t, 5*time.Second, d)
}

func TestDuration_WithoutEndTime(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	// StartTime is set to now, so Duration() should be very small.
	d := exec.Duration()
	assert.GreaterOrEqual(t, d, time.Duration(0), "Duration() should not be negative")
	assert.LessOrEqual(t, d, time.Second, "Duration() too large for a just-created execution")
}

func TestDuration_ZeroStartTime(t *testing.T) {
	t.Parallel()
	exec := &Execution{}
	d := exec.Duration()
	assert.Equal(t, time.Duration(0), d, "Duration() should be 0 for zero StartTime")
}

func TestDuration_PreciseCalculation(t *testing.T) {
	t.Parallel()
	start := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 12, 5, 30, 0, time.UTC)
	exec := &Execution{
		StartTime: start,
		EndTime:   &end,
	}
	expected := 5*time.Minute + 30*time.Second
	assert.Equal(t, expected, exec.Duration())
}

// ---------------------------------------------------------------------------
// JSON Serialization
// ---------------------------------------------------------------------------

func TestExecution_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-123", "deploy app", "production")
	exec.PodName = "agent-pod-abc"
	exec.Model = "claude-3-opus"
	exec.TokensUsed = 1500
	exec.Metadata["agent"] = "vigil"
	exec.Metadata["version"] = "1.0"

	data, err := json.Marshal(exec)
	require.NoError(t, err, "json.Marshal error")

	var decoded Execution
	require.NoError(t, json.Unmarshal(data, &decoded), "json.Unmarshal error")

	assert.Equal(t, exec.ID, decoded.ID)
	assert.Equal(t, exec.IdentityID, decoded.IdentityID)
	assert.Equal(t, exec.Intent, decoded.Intent)
	assert.Equal(t, exec.Status, decoded.Status)
	assert.Equal(t, exec.PodName, decoded.PodName)
	assert.Equal(t, exec.Model, decoded.Model)
	assert.Equal(t, exec.TokensUsed, decoded.TokensUsed)
	assert.Equal(t, exec.Namespace, decoded.Namespace)
}

func TestExecution_JSONOmitsEmptyFields(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")

	data, err := json.Marshal(exec)
	require.NoError(t, err, "json.Marshal error")

	jsonStr := string(data)

	// These optional fields should be omitted when empty/zero.
	assert.NotContains(t, jsonStr, "end_time", "JSON should omit end_time when nil")
	assert.NotContains(t, jsonStr, "pod_name", "JSON should omit pod_name when empty")
	assert.NotContains(t, jsonStr, "model", "JSON should omit model when empty")
	assert.NotContains(t, jsonStr, "tokens_used", "JSON should omit tokens_used when zero")
	assert.NotContains(t, jsonStr, "error_message", "JSON should omit error_message when empty")

	// Required fields should always be present.
	assert.Contains(t, jsonStr, "\"id\"", "JSON should contain id")
	assert.Contains(t, jsonStr, "\"identity_id\"", "JSON should contain identity_id")
	assert.Contains(t, jsonStr, "\"status\"", "JSON should contain status")

	// Metadata is always present (initialized by NewExecution), even when empty.
	assert.Contains(t, jsonStr, "\"metadata\"", "JSON should contain metadata even when empty")
}

func TestExecution_JSONWithEndTime(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	endTime := exec.StartTime.Add(10 * time.Second)
	exec.EndTime = &endTime

	data, err := json.Marshal(exec)
	require.NoError(t, err, "json.Marshal error")

	assert.Contains(t, string(data), "end_time", "JSON should contain end_time when set")

	var decoded Execution
	require.NoError(t, json.Unmarshal(data, &decoded), "json.Unmarshal error")
	require.NotNil(t, decoded.EndTime, "decoded EndTime should not be nil")
	assert.True(t, decoded.EndTime.Equal(endTime), "decoded EndTime = %v, want %v", decoded.EndTime, endTime)
}

func TestExecution_JSONWithErrorMessage(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusFailed
	exec.ErrorMessage = "connection refused"

	data, err := json.Marshal(exec)
	require.NoError(t, err, "json.Marshal error")

	var decoded Execution
	require.NoError(t, json.Unmarshal(data, &decoded), "json.Unmarshal error")
	assert.Equal(t, "connection refused", decoded.ErrorMessage)
}

func TestExecution_JSONWithMetadata(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Metadata["key"] = "value"
	exec.Metadata["count"] = float64(42)

	data, err := json.Marshal(exec)
	require.NoError(t, err, "json.Marshal error")

	var decoded Execution
	require.NoError(t, json.Unmarshal(data, &decoded), "json.Unmarshal error")
	assert.Equal(t, "value", decoded.Metadata["key"])
	assert.Equal(t, float64(42), decoded.Metadata["count"])
}

func TestExecution_JSONMetadataEmptyMap(t *testing.T) {
	t.Parallel()
	exec := mustNewExecution(t, "user-1", "task", "ns")
	// NewExecution initializes Metadata to an empty map (non-nil).

	data, err := json.Marshal(exec)
	require.NoError(t, err, "json.Marshal error")

	jsonStr := string(data)

	// Empty-but-non-nil map should still appear in JSON output.
	assert.Contains(t, jsonStr, "\"metadata\":{}", "JSON should contain metadata as empty object")

	var decoded Execution
	require.NoError(t, json.Unmarshal(data, &decoded), "json.Unmarshal error")
	assert.NotNil(t, decoded.Metadata, "decoded Metadata should not be nil for empty map")
	assert.Len(t, decoded.Metadata, 0)
}

func TestExecution_JSONMetadataNilMap(t *testing.T) {
	t.Parallel()
	// Manually constructed execution with nil Metadata (not via NewExecution).
	exec := &Execution{
		ID:         "test-id",
		IdentityID: "user-1",
		Intent:     "task",
		Status:     ExecutionStatusPending,
		StartTime:  time.Now().UTC(),
		Namespace:  "ns",
		Metadata:   nil,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	data, err := json.Marshal(exec)
	require.NoError(t, err, "json.Marshal error")

	jsonStr := string(data)

	// Nil metadata should serialize as null (not omitted, since omitempty
	// was removed — metadata is always present in the JSON contract).
	assert.Contains(t, jsonStr, "\"metadata\":null", "JSON should contain metadata as null for nil map")
}

// ---------------------------------------------------------------------------
// Schema Version
// ---------------------------------------------------------------------------

func TestExecutionSchemaVersion(t *testing.T) {
	t.Parallel()
	assert.GreaterOrEqual(t, ExecutionSchemaVersion, 1, "ExecutionSchemaVersion should be >= 1")
}
