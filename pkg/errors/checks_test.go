package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAsError_PlatformError(t *testing.T) {
	t.Parallel()
	platformErr := New(CodeValidation, "test")

	got, ok := AsError(platformErr)
	require.True(t, ok, "AsError should return true for platform error")
	assert.Equal(t, platformErr, got, "AsError should return the same platform error")
}

func TestAsError_WrappedPlatformError(t *testing.T) {
	t.Parallel()
	platformErr := New(CodeValidation, "test")
	wrapped := Wrap(platformErr, CodeInternal, "wrapper")

	got, ok := AsError(wrapped)
	require.True(t, ok, "AsError should return true for wrapped platform error")
	assert.Equal(t, CodeInternal, got.Code, "AsError should return outer error")
}

func TestAsError_StandardError(t *testing.T) {
	t.Parallel()
	stdErr := errors.New("standard error")

	got, ok := AsError(stdErr)
	assert.False(t, ok, "AsError should return false for standard error")
	assert.Nil(t, got, "AsError should return nil for standard error")
}

func TestAsError_Nil(t *testing.T) {
	t.Parallel()
	got, ok := AsError(nil)
	assert.False(t, ok, "AsError should return false for nil")
	assert.Nil(t, got, "AsError should return nil for nil input")
}

func TestAsError_DeepChain(t *testing.T) {
	t.Parallel()
	// Standard error wrapped in platform error wrapped in standard error wrapper
	platformErr := New(CodeTimeout, "timeout")
	doubleWrapped := errors.Join(errors.New("outer"), platformErr)

	got, ok := AsError(doubleWrapped)
	require.True(t, ok, "AsError should find platform error in deep chain")
	assert.Equal(t, CodeTimeout, got.Code, "AsError found wrong error")
}

func TestGetCode_PlatformError(t *testing.T) {
	t.Parallel()
	err := New(CodeValidation, "test")

	got := GetCode(err)
	assert.Equal(t, CodeValidation, got)
}

func TestGetCode_StandardError(t *testing.T) {
	t.Parallel()
	err := errors.New("standard error")

	got := GetCode(err)
	assert.Equal(t, Code(""), got, "GetCode() should return empty string for standard error")
}

func TestGetCode_Nil(t *testing.T) {
	t.Parallel()
	got := GetCode(nil)
	assert.Equal(t, Code(""), got, "GetCode(nil) should return empty string")
}

func TestHasCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		code Code
		want bool
	}{
		{
			name: "matching code",
			err:  New(CodeValidation, "test"),
			code: CodeValidation,
			want: true,
		},
		{
			name: "non-matching code",
			err:  New(CodeValidation, "test"),
			code: CodeNotFound,
			want: false,
		},
		{
			name: "standard error",
			err:  errors.New("standard"),
			code: CodeValidation,
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			code: CodeValidation,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, HasCode(tt.err, tt.code))
		})
	}
}

func TestIsValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"CodeValidation", New(CodeValidation, "test"), true},
		{"CodeValidationRequired", New(CodeValidationRequired, "test"), true},
		{"CodeValidationFormat", New(CodeValidationFormat, "test"), true},
		{"CodeValidationRange", New(CodeValidationRange, "test"), true},
		{"CodeAuthentication", New(CodeAuthentication, "test"), false},
		{"CodeNotFound", New(CodeNotFound, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsValidation(tt.err))
		})
	}
}

func TestIsAuthentication(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"CodeAuthentication", New(CodeAuthentication, "test"), true},
		{"CodeAuthenticationExpired", New(CodeAuthenticationExpired, "test"), true},
		{"CodeAuthenticationInvalid", New(CodeAuthenticationInvalid, "test"), true},
		{"CodeAuthorization", New(CodeAuthorization, "test"), false},
		{"CodeValidation", New(CodeValidation, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsAuthentication(tt.err))
		})
	}
}

func TestIsAuthorization(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"CodeAuthorization", New(CodeAuthorization, "test"), true},
		{"CodeAuthorizationDenied", New(CodeAuthorizationDenied, "test"), true},
		{"CodeAuthorizationInsufficientScope", New(CodeAuthorizationInsufficientScope, "test"), true},
		{"CodeAuthentication", New(CodeAuthentication, "test"), false},
		{"CodeValidation", New(CodeValidation, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsAuthorization(tt.err))
		})
	}
}

func TestIsNotFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"CodeNotFound", New(CodeNotFound, "test"), true},
		{"CodeNotFoundUser", New(CodeNotFoundUser, "test"), true},
		{"CodeNotFoundResource", New(CodeNotFoundResource, "test"), true},
		{"CodeValidation", New(CodeValidation, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsNotFound(tt.err))
		})
	}
}

func TestIsConflict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"CodeConflict", New(CodeConflict, "test"), true},
		{"CodeConflictAlreadyExists", New(CodeConflictAlreadyExists, "test"), true},
		{"CodeConflictVersionMismatch", New(CodeConflictVersionMismatch, "test"), true},
		{"CodeValidation", New(CodeValidation, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsConflict(tt.err))
		})
	}
}

func TestIsInternal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"CodeInternal", New(CodeInternal, "test"), true},
		{"CodeInternalDatabase", New(CodeInternalDatabase, "test"), true},
		{"CodeInternalConfiguration", New(CodeInternalConfiguration, "test"), true},
		{"CodeValidation", New(CodeValidation, "test"), false},
		{"CodeTimeout", New(CodeTimeout, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsInternal(tt.err))
		})
	}
}

func TestIsUnavailable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"CodeUnavailable", New(CodeUnavailable, "test"), true},
		{"CodeUnavailableDependency", New(CodeUnavailableDependency, "test"), true},
		{"CodeUnavailableOverloaded", New(CodeUnavailableOverloaded, "test"), true},
		{"CodeTimeout", New(CodeTimeout, "test"), false},
		{"CodeInternal", New(CodeInternal, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsUnavailable(tt.err))
		})
	}
}

func TestIsTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"CodeTimeout", New(CodeTimeout, "test"), true},
		{"CodeTimeoutDatabase", New(CodeTimeoutDatabase, "test"), true},
		{"CodeTimeoutDependency", New(CodeTimeoutDependency, "test"), true},
		{"CodeUnavailable", New(CodeUnavailable, "test"), false},
		{"CodeInternal", New(CodeInternal, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsTimeout(tt.err))
		})
	}
}

func TestIsRetryable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Retryable errors
		{"CodeTimeout", New(CodeTimeout, "test"), true},
		{"CodeTimeoutDatabase", New(CodeTimeoutDatabase, "test"), true},
		{"CodeTimeoutDependency", New(CodeTimeoutDependency, "test"), true},
		{"CodeUnavailable", New(CodeUnavailable, "test"), true},
		{"CodeUnavailableDependency", New(CodeUnavailableDependency, "test"), true},
		{"CodeUnavailableOverloaded", New(CodeUnavailableOverloaded, "test"), true},

		// Not retryable errors
		{"CodeValidation", New(CodeValidation, "test"), false},
		{"CodeAuthentication", New(CodeAuthentication, "test"), false},
		{"CodeAuthorization", New(CodeAuthorization, "test"), false},
		{"CodeNotFound", New(CodeNotFound, "test"), false},
		{"CodeConflict", New(CodeConflict, "test"), false},
		{"CodeInternal", New(CodeInternal, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsRetryable(tt.err))
		})
	}
}

func TestIsClientError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Client errors (4xx)
		{"CodeValidation", New(CodeValidation, "test"), true},
		{"CodeValidationRequired", New(CodeValidationRequired, "test"), true},
		{"CodeAuthentication", New(CodeAuthentication, "test"), true},
		{"CodeAuthenticationExpired", New(CodeAuthenticationExpired, "test"), true},
		{"CodeAuthorization", New(CodeAuthorization, "test"), true},
		{"CodeAuthorizationDenied", New(CodeAuthorizationDenied, "test"), true},
		{"CodeNotFound", New(CodeNotFound, "test"), true},
		{"CodeNotFoundUser", New(CodeNotFoundUser, "test"), true},
		{"CodeConflict", New(CodeConflict, "test"), true},
		{"CodeConflictAlreadyExists", New(CodeConflictAlreadyExists, "test"), true},

		// Server errors (5xx) - not client errors
		{"CodeInternal", New(CodeInternal, "test"), false},
		{"CodeUnavailable", New(CodeUnavailable, "test"), false},
		{"CodeTimeout", New(CodeTimeout, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsClientError(tt.err))
		})
	}
}

func TestIsServerError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Server errors (5xx)
		{"CodeInternal", New(CodeInternal, "test"), true},
		{"CodeInternalDatabase", New(CodeInternalDatabase, "test"), true},
		{"CodeInternalConfiguration", New(CodeInternalConfiguration, "test"), true},
		{"CodeUnavailable", New(CodeUnavailable, "test"), true},
		{"CodeUnavailableDependency", New(CodeUnavailableDependency, "test"), true},
		{"CodeTimeout", New(CodeTimeout, "test"), true},
		{"CodeTimeoutDatabase", New(CodeTimeoutDatabase, "test"), true},

		// Client errors (4xx) - not server errors
		{"CodeValidation", New(CodeValidation, "test"), false},
		{"CodeAuthentication", New(CodeAuthentication, "test"), false},
		{"CodeAuthorization", New(CodeAuthorization, "test"), false},
		{"CodeNotFound", New(CodeNotFound, "test"), false},
		{"CodeConflict", New(CodeConflict, "test"), false},
		{"standard error", errors.New("standard"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsServerError(tt.err))
		})
	}
}

func TestCheckFunctions_WithWrappedErrors(t *testing.T) {
	t.Parallel()
	// Ensure check functions work with wrapped platform errors
	inner := New(CodeNotFound, "not found")
	outer := Wrap(inner, CodeInternal, "operation failed")

	// The outer error is INT, not NF
	assert.False(t, IsNotFound(outer), "IsNotFound should check outer error code, not cause")
	assert.True(t, IsInternal(outer), "IsInternal should return true for outer error")
}

func TestCheckFunctions_Exhaustive(t *testing.T) {
	t.Parallel()
	// Test that every error category is covered by exactly one category check
	allCodes := []struct {
		code          Code
		isValidation  bool
		isAuth        bool
		isAuthz       bool
		isNotFound    bool
		isConflict    bool
		isInternal    bool
		isUnavailable bool
		isTimeout     bool
		isClientError bool
		isServerError bool
		isRetryable   bool
	}{
		{CodeValidation, true, false, false, false, false, false, false, false, true, false, false},
		{CodeAuthentication, false, true, false, false, false, false, false, false, true, false, false},
		{CodeAuthorization, false, false, true, false, false, false, false, false, true, false, false},
		{CodeNotFound, false, false, false, true, false, false, false, false, true, false, false},
		{CodeConflict, false, false, false, false, true, false, false, false, true, false, false},
		{CodeInternal, false, false, false, false, false, true, false, false, false, true, false},
		{CodeUnavailable, false, false, false, false, false, false, true, false, false, true, true},
		{CodeTimeout, false, false, false, false, false, false, false, true, false, true, true},
	}

	for _, tc := range allCodes {
		t.Run(string(tc.code), func(t *testing.T) {
			t.Parallel()
			err := New(tc.code, "test")

			assert.Equal(t, tc.isValidation, IsValidation(err), "IsValidation()")
			assert.Equal(t, tc.isAuth, IsAuthentication(err), "IsAuthentication()")
			assert.Equal(t, tc.isAuthz, IsAuthorization(err), "IsAuthorization()")
			assert.Equal(t, tc.isNotFound, IsNotFound(err), "IsNotFound()")
			assert.Equal(t, tc.isConflict, IsConflict(err), "IsConflict()")
			assert.Equal(t, tc.isInternal, IsInternal(err), "IsInternal()")
			assert.Equal(t, tc.isUnavailable, IsUnavailable(err), "IsUnavailable()")
			assert.Equal(t, tc.isTimeout, IsTimeout(err), "IsTimeout()")
			assert.Equal(t, tc.isClientError, IsClientError(err), "IsClientError()")
			assert.Equal(t, tc.isServerError, IsServerError(err), "IsServerError()")
			assert.Equal(t, tc.isRetryable, IsRetryable(err), "IsRetryable()")
		})
	}
}
