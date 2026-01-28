package errors

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "error without cause",
			err: &Error{
				Code:    CodeValidation,
				Message: "invalid email address",
			},
			want: "VAL_001: invalid email address",
		},
		{
			name: "error with cause",
			err: &Error{
				Code:    CodeInternalDatabase,
				Message: "failed to fetch user",
				Cause:   errors.New("connection refused"),
			},
			want: "INT_002: failed to fetch user: connection refused",
		},
		{
			name: "error with empty message",
			err: &Error{
				Code:    CodeInternal,
				Message: "",
			},
			want: "INT_001: ",
		},
		{
			name: "error with nested platform error cause",
			err: &Error{
				Code:    CodeInternal,
				Message: "operation failed",
				Cause: &Error{
					Code:    CodeTimeout,
					Message: "database timeout",
				},
			},
			want: "INT_001: operation failed: TIMEOUT_001: database timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := &Error{
		Code:    CodeInternal,
		Message: "operation failed",
		Cause:   cause,
	}

	if got := err.Unwrap(); got != cause {
		t.Errorf("Error.Unwrap() = %v, want %v", got, cause)
	}

	// Test error without cause
	errNoCause := &Error{
		Code:    CodeValidation,
		Message: "invalid input",
	}

	if got := errNoCause.Unwrap(); got != nil {
		t.Errorf("Error.Unwrap() = %v, want nil", got)
	}
}

func TestError_Unwrap_ErrorsIs(t *testing.T) {
	// Test that errors.Is works with wrapped errors
	cause := errors.New("specific error")
	err := &Error{
		Code:    CodeInternal,
		Message: "wrapper",
		Cause:   cause,
	}

	if !errors.Is(err, cause) {
		t.Error("errors.Is should find the cause in the error chain")
	}
}

func TestError_Unwrap_ErrorsAs(t *testing.T) {
	// Test that errors.As works with nested platform errors
	innerErr := &Error{
		Code:    CodeTimeout,
		Message: "timeout",
	}
	outerErr := &Error{
		Code:    CodeInternal,
		Message: "wrapper",
		Cause:   innerErr,
	}

	var target *Error
	if !errors.As(outerErr, &target) {
		t.Error("errors.As should find *Error in chain")
	}
	if target.Code != CodeInternal {
		t.Errorf("errors.As found wrong error, got code %v, want %v", target.Code, CodeInternal)
	}
}

func TestError_HTTPStatus(t *testing.T) {
	tests := []struct {
		name string
		code Code
		want int
	}{
		// Validation errors -> 400
		{"validation", CodeValidation, http.StatusBadRequest},
		{"validation required", CodeValidationRequired, http.StatusBadRequest},
		{"validation format", CodeValidationFormat, http.StatusBadRequest},
		{"validation range", CodeValidationRange, http.StatusBadRequest},

		// Authentication errors -> 401
		{"authentication", CodeAuthentication, http.StatusUnauthorized},
		{"authentication expired", CodeAuthenticationExpired, http.StatusUnauthorized},
		{"authentication invalid", CodeAuthenticationInvalid, http.StatusUnauthorized},

		// Authorization errors -> 403
		{"authorization", CodeAuthorization, http.StatusForbidden},
		{"authorization denied", CodeAuthorizationDenied, http.StatusForbidden},
		{"authorization insufficient scope", CodeAuthorizationInsufficientScope, http.StatusForbidden},

		// Not found errors -> 404
		{"not found", CodeNotFound, http.StatusNotFound},
		{"not found user", CodeNotFoundUser, http.StatusNotFound},
		{"not found resource", CodeNotFoundResource, http.StatusNotFound},

		// Conflict errors -> 409
		{"conflict", CodeConflict, http.StatusConflict},
		{"conflict already exists", CodeConflictAlreadyExists, http.StatusConflict},
		{"conflict version mismatch", CodeConflictVersionMismatch, http.StatusConflict},

		// Internal errors -> 500
		{"internal", CodeInternal, http.StatusInternalServerError},
		{"internal database", CodeInternalDatabase, http.StatusInternalServerError},
		{"internal configuration", CodeInternalConfiguration, http.StatusInternalServerError},

		// Unavailable errors -> 503
		{"unavailable", CodeUnavailable, http.StatusServiceUnavailable},
		{"unavailable dependency", CodeUnavailableDependency, http.StatusServiceUnavailable},
		{"unavailable overloaded", CodeUnavailableOverloaded, http.StatusServiceUnavailable},

		// Timeout errors -> 504
		{"timeout", CodeTimeout, http.StatusGatewayTimeout},
		{"timeout database", CodeTimeoutDatabase, http.StatusGatewayTimeout},
		{"timeout dependency", CodeTimeoutDependency, http.StatusGatewayTimeout},

		// Unknown category -> 500
		{"unknown category", Code("UNKNOWN_001"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &Error{Code: tt.code, Message: "test"}
			if got := err.HTTPStatus(); got != tt.want {
				t.Errorf("Error.HTTPStatus() for %v = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestError_WithDetails(t *testing.T) {
	original := &Error{
		Code:    CodeValidation,
		Message: "validation failed",
		Details: map[string]any{"field": "email"},
	}

	newDetails := map[string]any{"reason": "invalid format"}
	modified := original.WithDetails(newDetails)

	// Original should be unchanged
	if _, ok := original.Details["reason"]; ok {
		t.Error("WithDetails modified the original error")
	}

	// Modified should have both fields
	if modified.Details["field"] != "email" {
		t.Error("WithDetails did not preserve existing details")
	}
	if modified.Details["reason"] != "invalid format" {
		t.Error("WithDetails did not add new details")
	}

	// Code and Message should be preserved
	if modified.Code != original.Code {
		t.Error("WithDetails did not preserve Code")
	}
	if modified.Message != original.Message {
		t.Error("WithDetails did not preserve Message")
	}
}

func TestError_WithDetails_Overwrite(t *testing.T) {
	original := &Error{
		Code:    CodeValidation,
		Message: "test",
		Details: map[string]any{"key": "original"},
	}

	modified := original.WithDetails(map[string]any{"key": "overwritten"})

	if original.Details["key"] != "original" {
		t.Error("WithDetails modified the original error")
	}
	if modified.Details["key"] != "overwritten" {
		t.Error("WithDetails did not overwrite existing key")
	}
}

func TestError_WithDetails_NilOriginal(t *testing.T) {
	original := &Error{
		Code:    CodeValidation,
		Message: "test",
		Details: nil,
	}

	modified := original.WithDetails(map[string]any{"key": "value"})

	if modified.Details["key"] != "value" {
		t.Error("WithDetails failed when original Details was nil")
	}
}

func TestError_WithDetail(t *testing.T) {
	original := &Error{
		Code:    CodeValidation,
		Message: "validation failed",
	}

	modified := original.WithDetail("field", "email")

	// Original should be unchanged
	if len(original.Details) > 0 {
		t.Error("WithDetail modified the original error")
	}

	// Modified should have the detail
	if modified.Details["field"] != "email" {
		t.Error("WithDetail did not add the detail")
	}
}

func TestError_WithDetail_Chaining(t *testing.T) {
	err := New(CodeValidation, "validation failed").
		WithDetail("field", "email").
		WithDetail("reason", "invalid format").
		WithDetail("value", "not-an-email")

	if err.Details["field"] != "email" {
		t.Error("Chained WithDetail failed for 'field'")
	}
	if err.Details["reason"] != "invalid format" {
		t.Error("Chained WithDetail failed for 'reason'")
	}
	if err.Details["value"] != "not-an-email" {
		t.Error("Chained WithDetail failed for 'value'")
	}
}

func TestError_Format(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		format   string
		contains []string
	}{
		{
			name: "standard format %v",
			err: &Error{
				Code:    CodeValidation,
				Message: "invalid input",
			},
			format:   "%v",
			contains: []string{"VAL_001", "invalid input"},
		},
		{
			name: "detailed format %+v without details",
			err: &Error{
				Code:    CodeValidation,
				Message: "invalid input",
			},
			format:   "%+v",
			contains: []string{"Error{", "Code:", "VAL_001", "Message:", "invalid input", "}"},
		},
		{
			name: "detailed format %+v with details",
			err: &Error{
				Code:    CodeValidation,
				Message: "invalid input",
				Details: map[string]any{"field": "email"},
			},
			format:   "%+v",
			contains: []string{"Error{", "Code:", "Message:", "Details:", "field", "email", "}"},
		},
		{
			name: "detailed format %+v with cause",
			err: &Error{
				Code:    CodeInternal,
				Message: "operation failed",
				Cause:   errors.New("underlying"),
			},
			format:   "%+v",
			contains: []string{"Error{", "Code:", "Message:", "Cause:", "underlying", "}"},
		},
		{
			name: "string format %s",
			err: &Error{
				Code:    CodeNotFound,
				Message: "user not found",
			},
			format:   "%s",
			contains: []string{"NF_001", "user not found"},
		},
		{
			name: "quoted format %q",
			err: &Error{
				Code:    CodeNotFound,
				Message: "user not found",
			},
			format:   "%q",
			contains: []string{"\"NF_001", "user not found\""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fmt.Sprintf(tt.format, tt.err)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("Format(%q) = %q, should contain %q", tt.format, got, want)
				}
			}
		})
	}
}

func TestError_Immutability(t *testing.T) {
	// Verify that Error methods don't mutate the original
	original := &Error{
		Code:    CodeValidation,
		Message: "original message",
		Details: map[string]any{"original": true},
	}

	// Store original values
	origCode := original.Code
	origMsg := original.Message
	origDetailsLen := len(original.Details)

	// Call all methods that could potentially mutate
	_ = original.Error()
	_ = original.Unwrap()
	_ = original.HTTPStatus()
	_ = original.WithDetails(map[string]any{"new": true})
	_ = original.WithDetail("another", "value")

	// Verify nothing changed
	if original.Code != origCode {
		t.Error("Code was mutated")
	}
	if original.Message != origMsg {
		t.Error("Message was mutated")
	}
	if len(original.Details) != origDetailsLen {
		t.Error("Details was mutated")
	}
}
