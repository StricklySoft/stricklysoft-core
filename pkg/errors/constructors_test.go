package errors

import (
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(CodeValidation, "invalid input")

	if err.Code != CodeValidation {
		t.Errorf("New().Code = %v, want %v", err.Code, CodeValidation)
	}
	if err.Message != "invalid input" {
		t.Errorf("New().Message = %v, want %v", err.Message, "invalid input")
	}
	if err.Cause != nil {
		t.Error("New().Cause should be nil")
	}
	if err.Details != nil {
		t.Error("New().Details should be nil")
	}
}

func TestNewf(t *testing.T) {
	err := Newf(CodeNotFoundUser, "user %q not found in namespace %s", "user-123", "default")

	if err.Code != CodeNotFoundUser {
		t.Errorf("Newf().Code = %v, want %v", err.Code, CodeNotFoundUser)
	}
	want := `user "user-123" not found in namespace default`
	if err.Message != want {
		t.Errorf("Newf().Message = %v, want %v", err.Message, want)
	}
}

func TestNewf_NoArgs(t *testing.T) {
	err := Newf(CodeInternal, "static message")

	if err.Message != "static message" {
		t.Errorf("Newf().Message = %v, want %v", err.Message, "static message")
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("connection refused")
	err := Wrap(cause, CodeInternalDatabase, "failed to connect to database")

	if err.Code != CodeInternalDatabase {
		t.Errorf("Wrap().Code = %v, want %v", err.Code, CodeInternalDatabase)
	}
	if err.Message != "failed to connect to database" {
		t.Errorf("Wrap().Message = %v, want %v", err.Message, "failed to connect to database")
	}
	if err.Cause != cause {
		t.Errorf("Wrap().Cause = %v, want %v", err.Cause, cause)
	}
}

func TestWrap_NilError(t *testing.T) {
	err := Wrap(nil, CodeInternal, "should not create error")

	if err != nil {
		t.Error("Wrap(nil, ...) should return nil")
	}
}

func TestWrap_PlatformError(t *testing.T) {
	inner := New(CodeTimeout, "timeout")
	outer := Wrap(inner, CodeInternal, "operation failed")

	if outer.Cause != inner {
		t.Error("Wrap should preserve platform error as cause")
	}

	// Should be able to unwrap to find inner error
	var target *Error
	if !errors.As(outer, &target) {
		t.Error("errors.As should find *Error")
	}
}

func TestWrapf(t *testing.T) {
	cause := errors.New("connection refused")
	err := Wrapf(cause, CodeInternalDatabase, "failed to connect to %s:%d", "localhost", 5432)

	if err.Code != CodeInternalDatabase {
		t.Errorf("Wrapf().Code = %v, want %v", err.Code, CodeInternalDatabase)
	}
	want := "failed to connect to localhost:5432"
	if err.Message != want {
		t.Errorf("Wrapf().Message = %v, want %v", err.Message, want)
	}
	if err.Cause != cause {
		t.Error("Wrapf should preserve cause")
	}
}

func TestWrapf_NilError(t *testing.T) {
	err := Wrapf(nil, CodeInternal, "should not create error: %v", "ignored")

	if err != nil {
		t.Error("Wrapf(nil, ...) should return nil")
	}
}

func TestValidation(t *testing.T) {
	err := Validation("email is required")

	if err.Code != CodeValidation {
		t.Errorf("Validation().Code = %v, want %v", err.Code, CodeValidation)
	}
	if err.Message != "email is required" {
		t.Errorf("Validation().Message = %v, want %v", err.Message, "email is required")
	}
}

func TestValidationf(t *testing.T) {
	err := Validationf("field %q must be at least %d characters", "password", 8)

	if err.Code != CodeValidation {
		t.Errorf("Validationf().Code = %v, want %v", err.Code, CodeValidation)
	}
	want := `field "password" must be at least 8 characters`
	if err.Message != want {
		t.Errorf("Validationf().Message = %v, want %v", err.Message, want)
	}
}

func TestNotFound(t *testing.T) {
	err := NotFound("resource not found")

	if err.Code != CodeNotFound {
		t.Errorf("NotFound().Code = %v, want %v", err.Code, CodeNotFound)
	}
	if err.Message != "resource not found" {
		t.Errorf("NotFound().Message = %v, want %v", err.Message, "resource not found")
	}
}

func TestNotFoundf(t *testing.T) {
	err := NotFoundf("user %q not found", "user-456")

	if err.Code != CodeNotFound {
		t.Errorf("NotFoundf().Code = %v, want %v", err.Code, CodeNotFound)
	}
	want := `user "user-456" not found`
	if err.Message != want {
		t.Errorf("NotFoundf().Message = %v, want %v", err.Message, want)
	}
}

func TestUnauthorized(t *testing.T) {
	err := Unauthorized("invalid token")

	if err.Code != CodeAuthentication {
		t.Errorf("Unauthorized().Code = %v, want %v", err.Code, CodeAuthentication)
	}
	if err.Message != "invalid token" {
		t.Errorf("Unauthorized().Message = %v, want %v", err.Message, "invalid token")
	}
}

func TestForbidden(t *testing.T) {
	err := Forbidden("access denied")

	if err.Code != CodeAuthorization {
		t.Errorf("Forbidden().Code = %v, want %v", err.Code, CodeAuthorization)
	}
	if err.Message != "access denied" {
		t.Errorf("Forbidden().Message = %v, want %v", err.Message, "access denied")
	}
}

func TestConflict(t *testing.T) {
	err := Conflict("resource already exists")

	if err.Code != CodeConflict {
		t.Errorf("Conflict().Code = %v, want %v", err.Code, CodeConflict)
	}
	if err.Message != "resource already exists" {
		t.Errorf("Conflict().Message = %v, want %v", err.Message, "resource already exists")
	}
}

func TestInternal(t *testing.T) {
	err := Internal("unexpected error")

	if err.Code != CodeInternal {
		t.Errorf("Internal().Code = %v, want %v", err.Code, CodeInternal)
	}
	if err.Message != "unexpected error" {
		t.Errorf("Internal().Message = %v, want %v", err.Message, "unexpected error")
	}
}

func TestInternalf(t *testing.T) {
	err := Internalf("failed to process request: %v", "disk full")

	if err.Code != CodeInternal {
		t.Errorf("Internalf().Code = %v, want %v", err.Code, CodeInternal)
	}
	want := "failed to process request: disk full"
	if err.Message != want {
		t.Errorf("Internalf().Message = %v, want %v", err.Message, want)
	}
}

func TestUnavailable(t *testing.T) {
	err := Unavailable("service temporarily unavailable")

	if err.Code != CodeUnavailable {
		t.Errorf("Unavailable().Code = %v, want %v", err.Code, CodeUnavailable)
	}
	if err.Message != "service temporarily unavailable" {
		t.Errorf("Unavailable().Message = %v, want %v", err.Message, "service temporarily unavailable")
	}
}

func TestTimeout(t *testing.T) {
	err := Timeout("operation timed out")

	if err.Code != CodeTimeout {
		t.Errorf("Timeout().Code = %v, want %v", err.Code, CodeTimeout)
	}
	if err.Message != "operation timed out" {
		t.Errorf("Timeout().Message = %v, want %v", err.Message, "operation timed out")
	}
}

func TestFromError_Nil(t *testing.T) {
	err := FromError(nil)

	if err != nil {
		t.Error("FromError(nil) should return nil")
	}
}

func TestFromError_PlatformError(t *testing.T) {
	original := New(CodeValidation, "original error")
	err := FromError(original)

	if err != original {
		t.Error("FromError should return platform error as-is")
	}
}

func TestFromError_StandardError(t *testing.T) {
	stdErr := errors.New("standard error")
	err := FromError(stdErr)

	if err.Code != CodeInternal {
		t.Errorf("FromError().Code = %v, want %v", err.Code, CodeInternal)
	}
	if err.Cause != stdErr {
		t.Error("FromError should wrap standard error as cause")
	}
}

func TestFromError_WrappedPlatformError(t *testing.T) {
	// Create a platform error wrapped in fmt.Errorf
	platformErr := New(CodeNotFound, "not found")
	wrappedErr := errors.Join(errors.New("context"), platformErr)

	err := FromError(wrappedErr)

	// Should extract the platform error from the chain
	if err.Code != CodeNotFound {
		t.Errorf("FromError should extract platform error from chain, got code %v", err.Code)
	}
}

func TestConstructorReturnTypes(t *testing.T) {
	// Verify all constructors return *Error (not error interface)
	// This enables method chaining like .WithDetail()

	var err *Error

	err = New(CodeValidation, "test")
	_ = err.WithDetail("key", "value") // Should compile

	err = Newf(CodeValidation, "test %s", "arg")
	_ = err.WithDetail("key", "value")

	err = Wrap(errors.New("cause"), CodeInternal, "test")
	if err != nil {
		_ = err.WithDetail("key", "value")
	}

	err = Wrapf(errors.New("cause"), CodeInternal, "test %s", "arg")
	if err != nil {
		_ = err.WithDetail("key", "value")
	}

	err = Validation("test")
	_ = err.WithDetail("key", "value")

	err = Validationf("test %s", "arg")
	_ = err.WithDetail("key", "value")

	err = NotFound("test")
	_ = err.WithDetail("key", "value")

	err = NotFoundf("test %s", "arg")
	_ = err.WithDetail("key", "value")

	err = Unauthorized("test")
	_ = err.WithDetail("key", "value")

	err = Forbidden("test")
	_ = err.WithDetail("key", "value")

	err = Conflict("test")
	_ = err.WithDetail("key", "value")

	err = Internal("test")
	_ = err.WithDetail("key", "value")

	err = Internalf("test %s", "arg")
	_ = err.WithDetail("key", "value")

	err = Unavailable("test")
	_ = err.WithDetail("key", "value")

	err = Timeout("test")
	_ = err.WithDetail("key", "value")
}
