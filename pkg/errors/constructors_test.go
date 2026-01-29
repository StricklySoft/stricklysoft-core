package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()
	err := New(CodeValidation, "invalid input")

	assert.Equal(t, CodeValidation, err.Code)
	assert.Equal(t, "invalid input", err.Message)
	assert.Nil(t, err.Cause, "New().Cause should be nil")
	assert.Nil(t, err.Details, "New().Details should be nil")
}

func TestNewf(t *testing.T) {
	t.Parallel()
	err := Newf(CodeNotFoundUser, "user %q not found in namespace %s", "user-123", "default")

	assert.Equal(t, CodeNotFoundUser, err.Code)
	want := `user "user-123" not found in namespace default`
	assert.Equal(t, want, err.Message)
}

func TestNewf_NoArgs(t *testing.T) {
	t.Parallel()
	err := Newf(CodeInternal, "static message")

	assert.Equal(t, "static message", err.Message)
}

func TestWrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("connection refused")
	err := Wrap(cause, CodeInternalDatabase, "failed to connect to database")

	assert.Equal(t, CodeInternalDatabase, err.Code)
	assert.Equal(t, "failed to connect to database", err.Message)
	assert.Equal(t, cause, err.Cause)
}

func TestWrap_NilError(t *testing.T) {
	t.Parallel()
	err := Wrap(nil, CodeInternal, "should not create error")

	assert.Nil(t, err, "Wrap(nil, ...) should return nil")
}

func TestWrap_PlatformError(t *testing.T) {
	t.Parallel()
	inner := New(CodeTimeout, "timeout")
	outer := Wrap(inner, CodeInternal, "operation failed")

	assert.Equal(t, inner, outer.Cause, "Wrap should preserve platform error as cause")

	// Should be able to unwrap to find inner error
	var target *Error
	require.True(t, errors.As(outer, &target), "errors.As should find *Error")
}

func TestWrapf(t *testing.T) {
	t.Parallel()
	cause := errors.New("connection refused")
	err := Wrapf(cause, CodeInternalDatabase, "failed to connect to %s:%d", "localhost", 5432)

	assert.Equal(t, CodeInternalDatabase, err.Code)
	want := "failed to connect to localhost:5432"
	assert.Equal(t, want, err.Message)
	assert.Equal(t, cause, err.Cause, "Wrapf should preserve cause")
}

func TestWrapf_NilError(t *testing.T) {
	t.Parallel()
	err := Wrapf(nil, CodeInternal, "should not create error: %v", "ignored")

	assert.Nil(t, err, "Wrapf(nil, ...) should return nil")
}

func TestValidation(t *testing.T) {
	t.Parallel()
	err := Validation("email is required")

	assert.Equal(t, CodeValidation, err.Code)
	assert.Equal(t, "email is required", err.Message)
}

func TestValidationf(t *testing.T) {
	t.Parallel()
	err := Validationf("field %q must be at least %d characters", "password", 8)

	assert.Equal(t, CodeValidation, err.Code)
	want := `field "password" must be at least 8 characters`
	assert.Equal(t, want, err.Message)
}

func TestNotFound(t *testing.T) {
	t.Parallel()
	err := NotFound("resource not found")

	assert.Equal(t, CodeNotFound, err.Code)
	assert.Equal(t, "resource not found", err.Message)
}

func TestNotFoundf(t *testing.T) {
	t.Parallel()
	err := NotFoundf("user %q not found", "user-456")

	assert.Equal(t, CodeNotFound, err.Code)
	want := `user "user-456" not found`
	assert.Equal(t, want, err.Message)
}

func TestUnauthorized(t *testing.T) {
	t.Parallel()
	err := Unauthorized("invalid token")

	assert.Equal(t, CodeAuthentication, err.Code)
	assert.Equal(t, "invalid token", err.Message)
}

func TestForbidden(t *testing.T) {
	t.Parallel()
	err := Forbidden("access denied")

	assert.Equal(t, CodeAuthorization, err.Code)
	assert.Equal(t, "access denied", err.Message)
}

func TestConflict(t *testing.T) {
	t.Parallel()
	err := Conflict("resource already exists")

	assert.Equal(t, CodeConflict, err.Code)
	assert.Equal(t, "resource already exists", err.Message)
}

func TestInternal(t *testing.T) {
	t.Parallel()
	err := Internal("unexpected error")

	assert.Equal(t, CodeInternal, err.Code)
	assert.Equal(t, "unexpected error", err.Message)
}

func TestInternalf(t *testing.T) {
	t.Parallel()
	err := Internalf("failed to process request: %v", "disk full")

	assert.Equal(t, CodeInternal, err.Code)
	want := "failed to process request: disk full"
	assert.Equal(t, want, err.Message)
}

func TestUnavailable(t *testing.T) {
	t.Parallel()
	err := Unavailable("service temporarily unavailable")

	assert.Equal(t, CodeUnavailable, err.Code)
	assert.Equal(t, "service temporarily unavailable", err.Message)
}

func TestTimeout(t *testing.T) {
	t.Parallel()
	err := Timeout("operation timed out")

	assert.Equal(t, CodeTimeout, err.Code)
	assert.Equal(t, "operation timed out", err.Message)
}

func TestFromError_Nil(t *testing.T) {
	t.Parallel()
	err := FromError(nil)

	assert.Nil(t, err, "FromError(nil) should return nil")
}

func TestFromError_PlatformError(t *testing.T) {
	t.Parallel()
	original := New(CodeValidation, "original error")
	err := FromError(original)

	assert.Equal(t, original, err, "FromError should return platform error as-is")
}

func TestFromError_StandardError(t *testing.T) {
	t.Parallel()
	stdErr := errors.New("standard error")
	err := FromError(stdErr)

	assert.Equal(t, CodeInternal, err.Code)
	assert.Equal(t, stdErr, err.Cause, "FromError should wrap standard error as cause")
}

func TestFromError_WrappedPlatformError(t *testing.T) {
	t.Parallel()
	// Create a platform error wrapped in fmt.Errorf
	platformErr := New(CodeNotFound, "not found")
	wrappedErr := errors.Join(errors.New("context"), platformErr)

	err := FromError(wrappedErr)

	// Should extract the platform error from the chain
	assert.Equal(t, CodeNotFound, err.Code, "FromError should extract platform error from chain")
}

func TestConstructorReturnTypes(t *testing.T) {
	t.Parallel()
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
