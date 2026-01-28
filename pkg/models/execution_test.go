package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// mustNewExecution creates an Execution, failing the test if construction
// returns an error.
func mustNewExecution(t *testing.T, identityID, intent, namespace string) *Execution {
	t.Helper()
	exec, err := NewExecution(identityID, intent, namespace)
	if err != nil {
		t.Fatalf("NewExecution(%q, %q, %q) unexpected error: %v", identityID, intent, namespace, err)
	}
	return exec
}

// ---------------------------------------------------------------------------
// ExecutionStatus
// ---------------------------------------------------------------------------

func TestExecutionStatus_String(t *testing.T) {
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
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("ExecutionStatus.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExecutionStatus_Valid(t *testing.T) {
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
			if got := tt.status.Valid(); got != tt.expected {
				t.Errorf("ExecutionStatus.Valid() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExecutionStatus_IsTerminal(t *testing.T) {
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
			if got := tt.status.IsTerminal(); got != tt.expected {
				t.Errorf("ExecutionStatus.IsTerminal() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NewExecution
// ---------------------------------------------------------------------------

func TestNewExecution(t *testing.T) {
	exec := mustNewExecution(t, "user-123", "deploy application", "production")

	if exec.ID == "" {
		t.Error("ID should not be empty")
	}
	if exec.IdentityID != "user-123" {
		t.Errorf("IdentityID = %q, want %q", exec.IdentityID, "user-123")
	}
	if exec.Intent != "deploy application" {
		t.Errorf("Intent = %q, want %q", exec.Intent, "deploy application")
	}
	if exec.Status != ExecutionStatusPending {
		t.Errorf("Status = %q, want %q", exec.Status, ExecutionStatusPending)
	}
	if exec.Namespace != "production" {
		t.Errorf("Namespace = %q, want %q", exec.Namespace, "production")
	}
	if exec.Metadata == nil {
		t.Error("Metadata should not be nil")
	}
	if len(exec.Metadata) != 0 {
		t.Errorf("Metadata should be empty, got %d entries", len(exec.Metadata))
	}
	if exec.EndTime != nil {
		t.Error("EndTime should be nil for a new execution")
	}
	if exec.PodName != "" {
		t.Errorf("PodName should be empty, got %q", exec.PodName)
	}
	if exec.Model != "" {
		t.Errorf("Model should be empty, got %q", exec.Model)
	}
	if exec.TokensUsed != 0 {
		t.Errorf("TokensUsed should be 0, got %d", exec.TokensUsed)
	}
	if exec.ErrorMessage != "" {
		t.Errorf("ErrorMessage should be empty, got %q", exec.ErrorMessage)
	}
}

func TestNewExecution_GeneratesUniqueIDs(t *testing.T) {
	exec1 := mustNewExecution(t, "user-1", "task-1", "ns")
	exec2 := mustNewExecution(t, "user-1", "task-1", "ns")

	if exec1.ID == exec2.ID {
		t.Errorf("two executions should have different IDs, both got %q", exec1.ID)
	}
}

func TestNewExecution_UUIDFormat(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")

	// UUID v4 format: 8-4-4-4-12 hex characters.
	parts := strings.Split(exec.ID, "-")
	if len(parts) != 5 {
		t.Errorf("ID %q does not have UUID format (expected 5 parts separated by hyphens)", exec.ID)
	}
}

func TestNewExecution_TimestampsAreUTC(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")

	if exec.StartTime.Location() != time.UTC {
		t.Errorf("StartTime location = %v, want UTC", exec.StartTime.Location())
	}
	if exec.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", exec.CreatedAt.Location())
	}
	if exec.UpdatedAt.Location() != time.UTC {
		t.Errorf("UpdatedAt location = %v, want UTC", exec.UpdatedAt.Location())
	}
}

func TestNewExecution_TimestampsAreConsistent(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")

	if exec.StartTime != exec.CreatedAt {
		t.Error("StartTime and CreatedAt should be equal for a new execution")
	}
	if exec.StartTime != exec.UpdatedAt {
		t.Error("StartTime and UpdatedAt should be equal for a new execution")
	}
}

func TestNewExecution_EmptyIdentityID(t *testing.T) {
	_, err := NewExecution("", "task", "ns")
	if err == nil {
		t.Fatal("NewExecution with empty identityID should return an error")
	}
	if !strings.Contains(err.Error(), "identityID") {
		t.Errorf("error should mention identityID, got: %v", err)
	}
}

func TestNewExecution_EmptyIntent(t *testing.T) {
	_, err := NewExecution("user-1", "", "ns")
	if err == nil {
		t.Fatal("NewExecution with empty intent should return an error")
	}
	if !strings.Contains(err.Error(), "intent") {
		t.Errorf("error should mention intent, got: %v", err)
	}
}

func TestNewExecution_EmptyNamespace(t *testing.T) {
	_, err := NewExecution("user-1", "task", "")
	if err == nil {
		t.Fatal("NewExecution with empty namespace should return an error")
	}
	if !strings.Contains(err.Error(), "namespace") {
		t.Errorf("error should mention namespace, got: %v", err)
	}
}

func TestNewExecution_IsNotTerminal(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")

	if exec.IsTerminal() {
		t.Error("new execution should not be in a terminal state")
	}
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func TestValidate_ValidExecution(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "deploy app", "production")
	if err := exec.Validate(); err != nil {
		t.Errorf("Validate() returned error for valid execution: %v", err)
	}
}

func TestValidate_EmptyID(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.ID = ""
	if err := exec.Validate(); err == nil {
		t.Error("Validate() should return error for empty ID")
	}
}

func TestValidate_EmptyIdentityID(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.IdentityID = ""
	if err := exec.Validate(); err == nil {
		t.Error("Validate() should return error for empty IdentityID")
	}
}

func TestValidate_EmptyIntent(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Intent = ""
	if err := exec.Validate(); err == nil {
		t.Error("Validate() should return error for empty Intent")
	}
}

func TestValidate_EmptyNamespace(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Namespace = ""
	if err := exec.Validate(); err == nil {
		t.Error("Validate() should return error for empty Namespace")
	}
}

func TestValidate_InvalidStatus(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatus("invalid")
	err := exec.Validate()
	if err == nil {
		t.Error("Validate() should return error for invalid status")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error should mention the invalid status, got: %v", err)
	}
}

func TestValidate_ZeroStartTime(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.StartTime = time.Time{}
	if err := exec.Validate(); err == nil {
		t.Error("Validate() should return error for zero StartTime")
	}
}

func TestValidate_ZeroCreatedAt(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.CreatedAt = time.Time{}
	if err := exec.Validate(); err == nil {
		t.Error("Validate() should return error for zero CreatedAt")
	}
}

func TestValidate_ZeroUpdatedAt(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.UpdatedAt = time.Time{}
	if err := exec.Validate(); err == nil {
		t.Error("Validate() should return error for zero UpdatedAt")
	}
}

func TestValidate_NegativeTokensUsed(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.TokensUsed = -1
	err := exec.Validate()
	if err == nil {
		t.Error("Validate() should return error for negative TokensUsed")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Errorf("error should mention negative, got: %v", err)
	}
}

func TestValidate_ZeroTokensUsed(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.TokensUsed = 0
	if err := exec.Validate(); err != nil {
		t.Errorf("Validate() should not return error for zero TokensUsed: %v", err)
	}
}

func TestValidate_AllStatuses(t *testing.T) {
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
			exec := mustNewExecution(t, "user-1", "task", "ns")
			exec.Status = status
			if err := exec.Validate(); err != nil {
				t.Errorf("Validate() returned error for valid status %q: %v", status, err)
			}
		})
	}
}

func TestValidate_EmptyExecution(t *testing.T) {
	exec := &Execution{}
	err := exec.Validate()
	if err == nil {
		t.Error("Validate() should return error for empty execution")
	}
}

// ---------------------------------------------------------------------------
// IsTerminal
// ---------------------------------------------------------------------------

func TestIsTerminal_PendingExecution(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusPending
	if exec.IsTerminal() {
		t.Error("pending execution should not be terminal")
	}
}

func TestIsTerminal_RunningExecution(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusRunning
	if exec.IsTerminal() {
		t.Error("running execution should not be terminal")
	}
}

func TestIsTerminal_CompletedExecution(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusCompleted
	if !exec.IsTerminal() {
		t.Error("completed execution should be terminal")
	}
}

func TestIsTerminal_FailedExecution(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusFailed
	if !exec.IsTerminal() {
		t.Error("failed execution should be terminal")
	}
}

func TestIsTerminal_CanceledExecution(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusCanceled
	if !exec.IsTerminal() {
		t.Error("canceled execution should be terminal")
	}
}

func TestIsTerminal_TimeoutExecution(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusTimeout
	if !exec.IsTerminal() {
		t.Error("timeout execution should be terminal")
	}
}

// ---------------------------------------------------------------------------
// Duration
// ---------------------------------------------------------------------------

func TestDuration_WithEndTime(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	endTime := exec.StartTime.Add(5 * time.Second)
	exec.EndTime = &endTime

	d := exec.Duration()
	if d != 5*time.Second {
		t.Errorf("Duration() = %v, want %v", d, 5*time.Second)
	}
}

func TestDuration_WithoutEndTime(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	// StartTime is set to now, so Duration() should be very small.
	d := exec.Duration()
	if d < 0 {
		t.Errorf("Duration() = %v, should not be negative", d)
	}
	if d > time.Second {
		t.Errorf("Duration() = %v, too large for a just-created execution", d)
	}
}

func TestDuration_ZeroStartTime(t *testing.T) {
	exec := &Execution{}
	d := exec.Duration()
	if d != 0 {
		t.Errorf("Duration() = %v, want 0 for zero StartTime", d)
	}
}

func TestDuration_PreciseCalculation(t *testing.T) {
	start := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 1, 12, 5, 30, 0, time.UTC)
	exec := &Execution{
		StartTime: start,
		EndTime:   &end,
	}
	expected := 5*time.Minute + 30*time.Second
	if got := exec.Duration(); got != expected {
		t.Errorf("Duration() = %v, want %v", got, expected)
	}
}

// ---------------------------------------------------------------------------
// JSON Serialization
// ---------------------------------------------------------------------------

func TestExecution_JSONRoundTrip(t *testing.T) {
	exec := mustNewExecution(t, "user-123", "deploy app", "production")
	exec.PodName = "agent-pod-abc"
	exec.Model = "claude-3-opus"
	exec.TokensUsed = 1500
	exec.Metadata["agent"] = "vigil"
	exec.Metadata["version"] = "1.0"

	data, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var decoded Execution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if decoded.ID != exec.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, exec.ID)
	}
	if decoded.IdentityID != exec.IdentityID {
		t.Errorf("IdentityID = %q, want %q", decoded.IdentityID, exec.IdentityID)
	}
	if decoded.Intent != exec.Intent {
		t.Errorf("Intent = %q, want %q", decoded.Intent, exec.Intent)
	}
	if decoded.Status != exec.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, exec.Status)
	}
	if decoded.PodName != exec.PodName {
		t.Errorf("PodName = %q, want %q", decoded.PodName, exec.PodName)
	}
	if decoded.Model != exec.Model {
		t.Errorf("Model = %q, want %q", decoded.Model, exec.Model)
	}
	if decoded.TokensUsed != exec.TokensUsed {
		t.Errorf("TokensUsed = %d, want %d", decoded.TokensUsed, exec.TokensUsed)
	}
	if decoded.Namespace != exec.Namespace {
		t.Errorf("Namespace = %q, want %q", decoded.Namespace, exec.Namespace)
	}
}

func TestExecution_JSONOmitsEmptyFields(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")

	data, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	jsonStr := string(data)

	// These optional fields should be omitted when empty/zero.
	if strings.Contains(jsonStr, "end_time") {
		t.Error("JSON should omit end_time when nil")
	}
	if strings.Contains(jsonStr, "pod_name") {
		t.Error("JSON should omit pod_name when empty")
	}
	if strings.Contains(jsonStr, "model") {
		t.Error("JSON should omit model when empty")
	}
	if strings.Contains(jsonStr, "tokens_used") {
		t.Error("JSON should omit tokens_used when zero")
	}
	if strings.Contains(jsonStr, "error_message") {
		t.Error("JSON should omit error_message when empty")
	}

	// Required fields should always be present.
	if !strings.Contains(jsonStr, "\"id\"") {
		t.Error("JSON should contain id")
	}
	if !strings.Contains(jsonStr, "\"identity_id\"") {
		t.Error("JSON should contain identity_id")
	}
	if !strings.Contains(jsonStr, "\"status\"") {
		t.Error("JSON should contain status")
	}

	// Metadata is always present (initialized by NewExecution), even when empty.
	if !strings.Contains(jsonStr, "\"metadata\"") {
		t.Error("JSON should contain metadata even when empty")
	}
}

func TestExecution_JSONWithEndTime(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	endTime := exec.StartTime.Add(10 * time.Second)
	exec.EndTime = &endTime

	data, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	if !strings.Contains(string(data), "end_time") {
		t.Error("JSON should contain end_time when set")
	}

	var decoded Execution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if decoded.EndTime == nil {
		t.Fatal("decoded EndTime should not be nil")
	}
	if !decoded.EndTime.Equal(endTime) {
		t.Errorf("decoded EndTime = %v, want %v", decoded.EndTime, endTime)
	}
}

func TestExecution_JSONWithErrorMessage(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Status = ExecutionStatusFailed
	exec.ErrorMessage = "connection refused"

	data, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var decoded Execution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if decoded.ErrorMessage != "connection refused" {
		t.Errorf("ErrorMessage = %q, want %q", decoded.ErrorMessage, "connection refused")
	}
}

func TestExecution_JSONWithMetadata(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	exec.Metadata["key"] = "value"
	exec.Metadata["count"] = float64(42)

	data, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var decoded Execution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if decoded.Metadata["key"] != "value" {
		t.Errorf("Metadata[key] = %v, want %q", decoded.Metadata["key"], "value")
	}
	if decoded.Metadata["count"] != float64(42) {
		t.Errorf("Metadata[count] = %v, want 42", decoded.Metadata["count"])
	}
}

func TestExecution_JSONMetadataEmptyMap(t *testing.T) {
	exec := mustNewExecution(t, "user-1", "task", "ns")
	// NewExecution initializes Metadata to an empty map (non-nil).

	data, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	jsonStr := string(data)

	// Empty-but-non-nil map should still appear in JSON output.
	if !strings.Contains(jsonStr, "\"metadata\":{}") {
		t.Errorf("JSON should contain metadata as empty object, got: %s", jsonStr)
	}

	var decoded Execution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if decoded.Metadata == nil {
		t.Error("decoded Metadata should not be nil for empty map")
	}
	if len(decoded.Metadata) != 0 {
		t.Errorf("decoded Metadata should be empty, got %d entries", len(decoded.Metadata))
	}
}

func TestExecution_JSONMetadataNilMap(t *testing.T) {
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
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	jsonStr := string(data)

	// Nil metadata should serialize as null (not omitted, since omitempty
	// was removed — metadata is always present in the JSON contract).
	if !strings.Contains(jsonStr, "\"metadata\":null") {
		t.Errorf("JSON should contain metadata as null for nil map, got: %s", jsonStr)
	}
}

// ---------------------------------------------------------------------------
// Schema Version
// ---------------------------------------------------------------------------

func TestExecutionSchemaVersion(t *testing.T) {
	if ExecutionSchemaVersion < 1 {
		t.Errorf("ExecutionSchemaVersion = %d, should be >= 1", ExecutionSchemaVersion)
	}
}
