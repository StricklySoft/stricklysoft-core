package errors

import (
	"errors"
	"testing"
)

func TestAsError_PlatformError(t *testing.T) {
	platformErr := New(CodeValidation, "test")

	got, ok := AsError(platformErr)
	if !ok {
		t.Error("AsError should return true for platform error")
	}
	if got != platformErr {
		t.Error("AsError should return the same platform error")
	}
}

func TestAsError_WrappedPlatformError(t *testing.T) {
	platformErr := New(CodeValidation, "test")
	wrapped := Wrap(platformErr, CodeInternal, "wrapper")

	got, ok := AsError(wrapped)
	if !ok {
		t.Error("AsError should return true for wrapped platform error")
	}
	if got.Code != CodeInternal {
		t.Errorf("AsError should return outer error, got code %v", got.Code)
	}
}

func TestAsError_StandardError(t *testing.T) {
	stdErr := errors.New("standard error")

	got, ok := AsError(stdErr)
	if ok {
		t.Error("AsError should return false for standard error")
	}
	if got != nil {
		t.Error("AsError should return nil for standard error")
	}
}

func TestAsError_Nil(t *testing.T) {
	got, ok := AsError(nil)
	if ok {
		t.Error("AsError should return false for nil")
	}
	if got != nil {
		t.Error("AsError should return nil for nil input")
	}
}

func TestAsError_DeepChain(t *testing.T) {
	// Standard error wrapped in platform error wrapped in standard error wrapper
	platformErr := New(CodeTimeout, "timeout")
	doubleWrapped := errors.Join(errors.New("outer"), platformErr)

	got, ok := AsError(doubleWrapped)
	if !ok {
		t.Error("AsError should find platform error in deep chain")
	}
	if got.Code != CodeTimeout {
		t.Errorf("AsError found wrong error, got code %v", got.Code)
	}
}

func TestGetCode_PlatformError(t *testing.T) {
	err := New(CodeValidation, "test")

	got := GetCode(err)
	if got != CodeValidation {
		t.Errorf("GetCode() = %v, want %v", got, CodeValidation)
	}
}

func TestGetCode_StandardError(t *testing.T) {
	err := errors.New("standard error")

	got := GetCode(err)
	if got != "" {
		t.Errorf("GetCode() = %v, want empty string", got)
	}
}

func TestGetCode_Nil(t *testing.T) {
	got := GetCode(nil)
	if got != "" {
		t.Errorf("GetCode(nil) = %v, want empty string", got)
	}
}

func TestHasCode(t *testing.T) {
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
			if got := HasCode(tt.err, tt.code); got != tt.want {
				t.Errorf("HasCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidation(t *testing.T) {
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
			if got := IsValidation(tt.err); got != tt.want {
				t.Errorf("IsValidation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAuthentication(t *testing.T) {
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
			if got := IsAuthentication(tt.err); got != tt.want {
				t.Errorf("IsAuthentication() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAuthorization(t *testing.T) {
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
			if got := IsAuthorization(tt.err); got != tt.want {
				t.Errorf("IsAuthorization() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
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
			if got := IsNotFound(tt.err); got != tt.want {
				t.Errorf("IsNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsConflict(t *testing.T) {
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
			if got := IsConflict(tt.err); got != tt.want {
				t.Errorf("IsConflict() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsInternal(t *testing.T) {
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
			if got := IsInternal(tt.err); got != tt.want {
				t.Errorf("IsInternal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUnavailable(t *testing.T) {
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
			if got := IsUnavailable(tt.err); got != tt.want {
				t.Errorf("IsUnavailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTimeout(t *testing.T) {
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
			if got := IsTimeout(tt.err); got != tt.want {
				t.Errorf("IsTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
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
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsClientError(t *testing.T) {
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
			if got := IsClientError(tt.err); got != tt.want {
				t.Errorf("IsClientError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsServerError(t *testing.T) {
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
			if got := IsServerError(tt.err); got != tt.want {
				t.Errorf("IsServerError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckFunctions_WithWrappedErrors(t *testing.T) {
	// Ensure check functions work with wrapped platform errors
	inner := New(CodeNotFound, "not found")
	outer := Wrap(inner, CodeInternal, "operation failed")

	// The outer error is INT, not NF
	if IsNotFound(outer) {
		t.Error("IsNotFound should check outer error code, not cause")
	}
	if !IsInternal(outer) {
		t.Error("IsInternal should return true for outer error")
	}
}

func TestCheckFunctions_Exhaustive(t *testing.T) {
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
			err := New(tc.code, "test")

			if got := IsValidation(err); got != tc.isValidation {
				t.Errorf("IsValidation() = %v, want %v", got, tc.isValidation)
			}
			if got := IsAuthentication(err); got != tc.isAuth {
				t.Errorf("IsAuthentication() = %v, want %v", got, tc.isAuth)
			}
			if got := IsAuthorization(err); got != tc.isAuthz {
				t.Errorf("IsAuthorization() = %v, want %v", got, tc.isAuthz)
			}
			if got := IsNotFound(err); got != tc.isNotFound {
				t.Errorf("IsNotFound() = %v, want %v", got, tc.isNotFound)
			}
			if got := IsConflict(err); got != tc.isConflict {
				t.Errorf("IsConflict() = %v, want %v", got, tc.isConflict)
			}
			if got := IsInternal(err); got != tc.isInternal {
				t.Errorf("IsInternal() = %v, want %v", got, tc.isInternal)
			}
			if got := IsUnavailable(err); got != tc.isUnavailable {
				t.Errorf("IsUnavailable() = %v, want %v", got, tc.isUnavailable)
			}
			if got := IsTimeout(err); got != tc.isTimeout {
				t.Errorf("IsTimeout() = %v, want %v", got, tc.isTimeout)
			}
			if got := IsClientError(err); got != tc.isClientError {
				t.Errorf("IsClientError() = %v, want %v", got, tc.isClientError)
			}
			if got := IsServerError(err); got != tc.isServerError {
				t.Errorf("IsServerError() = %v, want %v", got, tc.isServerError)
			}
			if got := IsRetryable(err); got != tc.isRetryable {
				t.Errorf("IsRetryable() = %v, want %v", got, tc.isRetryable)
			}
		})
	}
}
