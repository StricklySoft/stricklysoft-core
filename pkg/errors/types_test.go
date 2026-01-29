package errors

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestError_Error(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			assert.Equal(t, tt.want, tt.err.Error())
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("underlying error")
	err := &Error{
		Code:    CodeInternal,
		Message: "operation failed",
		Cause:   cause,
	}

	assert.Equal(t, cause, err.Unwrap())

	// Test error without cause
	errNoCause := &Error{
		Code:    CodeValidation,
		Message: "invalid input",
	}

	assert.Nil(t, errNoCause.Unwrap())
}

func TestError_Unwrap_ErrorsIs(t *testing.T) {
	t.Parallel()
	// Test that errors.Is works with wrapped errors
	cause := errors.New("specific error")
	err := &Error{
		Code:    CodeInternal,
		Message: "wrapper",
		Cause:   cause,
	}

	assert.True(t, errors.Is(err, cause), "errors.Is should find the cause in the error chain")
}

func TestError_Unwrap_ErrorsAs(t *testing.T) {
	t.Parallel()
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
	require.True(t, errors.As(outerErr, &target), "errors.As should find *Error in chain")
	assert.Equal(t, CodeInternal, target.Code)
}

func TestError_HTTPStatus(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			err := &Error{Code: tt.code, Message: "test"}
			assert.Equal(t, tt.want, err.HTTPStatus(), "Error.HTTPStatus() for %v", tt.code)
		})
	}
}

func TestError_WithDetails(t *testing.T) {
	t.Parallel()
	original := &Error{
		Code:    CodeValidation,
		Message: "validation failed",
		Details: map[string]any{"field": "email"},
	}

	newDetails := map[string]any{"reason": "invalid format"}
	modified := original.WithDetails(newDetails)

	// Original should be unchanged
	assert.NotContains(t, original.Details, "reason", "WithDetails modified the original error")

	// Modified should have both fields
	assert.Equal(t, "email", modified.Details["field"], "WithDetails did not preserve existing details")
	assert.Equal(t, "invalid format", modified.Details["reason"], "WithDetails did not add new details")

	// Code and Message should be preserved
	assert.Equal(t, original.Code, modified.Code, "WithDetails did not preserve Code")
	assert.Equal(t, original.Message, modified.Message, "WithDetails did not preserve Message")
}

func TestError_WithDetails_Overwrite(t *testing.T) {
	t.Parallel()
	original := &Error{
		Code:    CodeValidation,
		Message: "test",
		Details: map[string]any{"key": "original"},
	}

	modified := original.WithDetails(map[string]any{"key": "overwritten"})

	assert.Equal(t, "original", original.Details["key"], "WithDetails modified the original error")
	assert.Equal(t, "overwritten", modified.Details["key"], "WithDetails did not overwrite existing key")
}

func TestError_WithDetails_NilOriginal(t *testing.T) {
	t.Parallel()
	original := &Error{
		Code:    CodeValidation,
		Message: "test",
		Details: nil,
	}

	modified := original.WithDetails(map[string]any{"key": "value"})

	assert.Equal(t, "value", modified.Details["key"], "WithDetails failed when original Details was nil")
}

func TestError_WithDetail(t *testing.T) {
	t.Parallel()
	original := &Error{
		Code:    CodeValidation,
		Message: "validation failed",
	}

	modified := original.WithDetail("field", "email")

	// Original should be unchanged
	assert.Empty(t, original.Details, "WithDetail modified the original error")

	// Modified should have the detail
	assert.Equal(t, "email", modified.Details["field"], "WithDetail did not add the detail")
}

func TestError_WithDetail_Chaining(t *testing.T) {
	t.Parallel()
	err := New(CodeValidation, "validation failed").
		WithDetail("field", "email").
		WithDetail("reason", "invalid format").
		WithDetail("value", "not-an-email")

	assert.Equal(t, "email", err.Details["field"], "Chained WithDetail failed for 'field'")
	assert.Equal(t, "invalid format", err.Details["reason"], "Chained WithDetail failed for 'reason'")
	assert.Equal(t, "not-an-email", err.Details["value"], "Chained WithDetail failed for 'value'")
}

func TestError_Format(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := fmt.Sprintf(tt.format, tt.err)
			for _, want := range tt.contains {
				assert.Contains(t, got, want, "Format(%q) = %q, should contain %q", tt.format, got, want)
			}
		})
	}
}

func TestError_Immutability(t *testing.T) {
	t.Parallel()
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
	assert.Equal(t, origCode, original.Code, "Code was mutated")
	assert.Equal(t, origMsg, original.Message, "Message was mutated")
	assert.Len(t, original.Details, origDetailsLen, "Details was mutated")
}
