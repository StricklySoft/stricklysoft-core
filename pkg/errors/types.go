package errors

import (
	"fmt"
	"net/http"
)

// Error represents a structured error with a code, message, and optional cause.
// It implements the standard error interface and provides additional context
// for error handling, logging, and API responses.
//
// Error is designed to be:
//   - Immutable: Fields are not modified after creation
//   - Chainable: Supports error wrapping via the Cause field
//   - Structured: Provides machine-readable code and HTTP status
//   - Loggable: Implements fmt.Formatter for detailed output
type Error struct {
	// Code is the machine-readable error code (e.g., "AUTH_001").
	Code Code

	// Message is the human-readable error message.
	// This message may be shown to end users and should not contain
	// sensitive information such as internal paths or credentials.
	Message string

	// Cause is the underlying error that caused this error, if any.
	// Use Unwrap() to access the cause for error chain inspection.
	Cause error

	// Details contains additional structured data about the error.
	// This can include field-level validation errors, resource identifiers,
	// or other context useful for debugging.
	Details map[string]any
}

// Error implements the error interface, returning the error message.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause of this error, supporting
// errors.Unwrap() and errors.Is() from the standard library.
func (e *Error) Unwrap() error {
	return e.Cause
}

// HTTPStatus returns the appropriate HTTP status code for this error
// based on its error code category.
func (e *Error) HTTPStatus() int {
	switch e.Code.Category() {
	case "VAL":
		return http.StatusBadRequest
	case "AUTH":
		return http.StatusUnauthorized
	case "AUTHZ":
		return http.StatusForbidden
	case "NF":
		return http.StatusNotFound
	case "CONF":
		return http.StatusConflict
	case "INT":
		return http.StatusInternalServerError
	case "UNAVAIL":
		return http.StatusServiceUnavailable
	case "TIMEOUT":
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// WithDetails returns a new Error with the specified details added.
// The original error is not modified.
func (e *Error) WithDetails(details map[string]any) *Error {
	newDetails := make(map[string]any, len(e.Details)+len(details))
	for k, v := range e.Details {
		newDetails[k] = v
	}
	for k, v := range details {
		newDetails[k] = v
	}
	return &Error{
		Code:    e.Code,
		Message: e.Message,
		Cause:   e.Cause,
		Details: newDetails,
	}
}

// WithDetail returns a new Error with a single detail key-value pair added.
// The original error is not modified.
func (e *Error) WithDetail(key string, value any) *Error {
	newDetails := make(map[string]any, len(e.Details)+1)
	for k, v := range e.Details {
		newDetails[k] = v
	}
	newDetails[key] = value
	return &Error{
		Code:    e.Code,
		Message: e.Message,
		Cause:   e.Cause,
		Details: newDetails,
	}
}

// Format implements fmt.Formatter for detailed error output.
// Use %v for standard output, %+v for detailed output including the cause chain.
func (e *Error) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			fmt.Fprintf(s, "Error{Code: %q, Message: %q", e.Code, e.Message)
			if len(e.Details) > 0 {
				fmt.Fprintf(s, ", Details: %v", e.Details)
			}
			if e.Cause != nil {
				fmt.Fprintf(s, ", Cause: %+v", e.Cause)
			}
			fmt.Fprint(s, "}")
			return
		}
		fallthrough
	case 's':
		fmt.Fprint(s, e.Error())
	case 'q':
		fmt.Fprintf(s, "%q", e.Error())
	}
}
