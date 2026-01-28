package errors

import (
	"errors"
	"fmt"
)

// New creates a new Error with the specified code and message.
// Use this for creating errors without an underlying cause.
//
// Example:
//
//	err := errors.New(errors.CodeValidation, "email address is required")
func New(code Code, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

// Newf creates a new Error with the specified code and formatted message.
// Use this for creating errors with dynamic content in the message.
//
// Example:
//
//	err := errors.Newf(errors.CodeNotFoundUser, "user %q not found", userID)
func Newf(code Code, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// Wrap wraps an existing error with additional context.
// The wrapped error becomes the Cause of the new error.
// If err is nil, Wrap returns nil.
//
// Example:
//
//	result, err := db.Query(ctx, sql)
//	if err != nil {
//	    return errors.Wrap(err, errors.CodeInternalDatabase, "failed to fetch user")
//	}
func Wrap(err error, code Code, message string) *Error {
	if err == nil {
		return nil
	}
	return &Error{
		Code:    code,
		Message: message,
		Cause:   err,
	}
}

// Wrapf wraps an existing error with a formatted message.
// The wrapped error becomes the Cause of the new error.
// If err is nil, Wrapf returns nil.
//
// Example:
//
//	err := errors.Wrapf(err, errors.CodeInternalDatabase, "failed to fetch user %q", userID)
func Wrapf(err error, code Code, format string, args ...any) *Error {
	if err == nil {
		return nil
	}
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Cause:   err,
	}
}

// Validation creates a new validation error.
// This is a convenience function equivalent to New(CodeValidation, message).
//
// Example:
//
//	err := errors.Validation("email address is invalid")
func Validation(message string) *Error {
	return New(CodeValidation, message)
}

// Validationf creates a new validation error with a formatted message.
//
// Example:
//
//	err := errors.Validationf("field %q must be at least %d characters", field, minLen)
func Validationf(format string, args ...any) *Error {
	return Newf(CodeValidation, format, args...)
}

// NotFound creates a new not found error.
// This is a convenience function equivalent to New(CodeNotFound, message).
//
// Example:
//
//	err := errors.NotFound("user not found")
func NotFound(message string) *Error {
	return New(CodeNotFound, message)
}

// NotFoundf creates a new not found error with a formatted message.
//
// Example:
//
//	err := errors.NotFoundf("user %q not found", userID)
func NotFoundf(format string, args ...any) *Error {
	return Newf(CodeNotFound, format, args...)
}

// Unauthorized creates a new authentication error.
// Use this when authentication fails (invalid or missing credentials).
//
// Example:
//
//	err := errors.Unauthorized("invalid authentication token")
func Unauthorized(message string) *Error {
	return New(CodeAuthentication, message)
}

// Forbidden creates a new authorization error.
// Use this when the authenticated user lacks permission for an action.
//
// Example:
//
//	err := errors.Forbidden("insufficient permissions to delete resource")
func Forbidden(message string) *Error {
	return New(CodeAuthorization, message)
}

// Conflict creates a new conflict error.
// Use this when an operation conflicts with the current state.
//
// Example:
//
//	err := errors.Conflict("user with email already exists")
func Conflict(message string) *Error {
	return New(CodeConflict, message)
}

// Internal creates a new internal error.
// Use this for unexpected system failures that should not expose details to users.
//
// Example:
//
//	err := errors.Internal("an unexpected error occurred")
func Internal(message string) *Error {
	return New(CodeInternal, message)
}

// Internalf creates a new internal error with a formatted message.
// Use this for logging detailed internal errors.
//
// Example:
//
//	err := errors.Internalf("failed to process request: %v", underlyingErr)
func Internalf(format string, args ...any) *Error {
	return Newf(CodeInternal, format, args...)
}

// Unavailable creates a new service unavailable error.
// Use this when a service or dependency is temporarily unavailable.
//
// Example:
//
//	err := errors.Unavailable("database is temporarily unavailable")
func Unavailable(message string) *Error {
	return New(CodeUnavailable, message)
}

// Timeout creates a new timeout error.
// Use this when an operation exceeds its time limit.
//
// Example:
//
//	err := errors.Timeout("request timed out after 30s")
func Timeout(message string) *Error {
	return New(CodeTimeout, message)
}

// FromError converts a standard error to an Error.
// If the error is already an *Error, it is returned as-is.
// Otherwise, it is wrapped as an internal error.
//
// Example:
//
//	platformErr := errors.FromError(err)
func FromError(err error) *Error {
	if err == nil {
		return nil
	}

	var e *Error
	if errors.As(err, &e) {
		return e
	}

	return Wrap(err, CodeInternal, "an unexpected error occurred")
}
