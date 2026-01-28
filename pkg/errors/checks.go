package errors

import (
	"errors"
)

// AsError attempts to convert an error to an *Error.
// Returns the Error and true if successful, nil and false otherwise.
// This function traverses the error chain using errors.As.
//
// Example:
//
//	if e, ok := errors.AsError(err); ok {
//	    log.Printf("error code: %s, message: %s", e.Code, e.Message)
//	}
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// GetCode returns the error code from an error.
// If the error is not an *Error or is nil, returns an empty string.
//
// Example:
//
//	code := errors.GetCode(err)
//	if code == errors.CodeNotFound {
//	    // handle not found
//	}
func GetCode(err error) Code {
	if e, ok := AsError(err); ok {
		return e.Code
	}
	return ""
}

// HasCode checks if an error has the specified error code.
// Returns false if the error is nil or not an *Error.
//
// Example:
//
//	if errors.HasCode(err, errors.CodeValidation) {
//	    // handle validation error
//	}
func HasCode(err error, code Code) bool {
	return GetCode(err) == code
}

// IsValidation checks if the error is a validation error (VAL_xxx).
// Returns true if the error code starts with "VAL".
//
// Example:
//
//	if errors.IsValidation(err) {
//	    // return 400 Bad Request
//	}
func IsValidation(err error) bool {
	e, ok := AsError(err)
	return ok && e.Code.Category() == "VAL"
}

// IsAuthentication checks if the error is an authentication error (AUTH_xxx).
// Returns true if the error code starts with "AUTH".
//
// Example:
//
//	if errors.IsAuthentication(err) {
//	    // return 401 Unauthorized
//	}
func IsAuthentication(err error) bool {
	e, ok := AsError(err)
	return ok && e.Code.Category() == "AUTH"
}

// IsAuthorization checks if the error is an authorization error (AUTHZ_xxx).
// Returns true if the error code starts with "AUTHZ".
//
// Example:
//
//	if errors.IsAuthorization(err) {
//	    // return 403 Forbidden
//	}
func IsAuthorization(err error) bool {
	e, ok := AsError(err)
	return ok && e.Code.Category() == "AUTHZ"
}

// IsNotFound checks if the error is a not found error (NF_xxx).
// Returns true if the error code starts with "NF".
//
// Example:
//
//	if errors.IsNotFound(err) {
//	    // return 404 Not Found
//	}
func IsNotFound(err error) bool {
	e, ok := AsError(err)
	return ok && e.Code.Category() == "NF"
}

// IsConflict checks if the error is a conflict error (CONF_xxx).
// Returns true if the error code starts with "CONF".
//
// Example:
//
//	if errors.IsConflict(err) {
//	    // return 409 Conflict
//	}
func IsConflict(err error) bool {
	e, ok := AsError(err)
	return ok && e.Code.Category() == "CONF"
}

// IsInternal checks if the error is an internal error (INT_xxx).
// Returns true if the error code starts with "INT".
//
// Example:
//
//	if errors.IsInternal(err) {
//	    // log error details, return generic message to client
//	}
func IsInternal(err error) bool {
	e, ok := AsError(err)
	return ok && e.Code.Category() == "INT"
}

// IsUnavailable checks if the error is a service unavailable error (UNAVAIL_xxx).
// Returns true if the error code starts with "UNAVAIL".
//
// Example:
//
//	if errors.IsUnavailable(err) {
//	    // return 503 Service Unavailable, maybe with Retry-After header
//	}
func IsUnavailable(err error) bool {
	e, ok := AsError(err)
	return ok && e.Code.Category() == "UNAVAIL"
}

// IsTimeout checks if the error is a timeout error (TIMEOUT_xxx).
// Returns true if the error code starts with "TIMEOUT".
//
// Example:
//
//	if errors.IsTimeout(err) {
//	    // return 504 Gateway Timeout
//	}
func IsTimeout(err error) bool {
	e, ok := AsError(err)
	return ok && e.Code.Category() == "TIMEOUT"
}

// IsRetryable checks if the error is potentially retryable.
// Timeout and unavailable errors are considered retryable.
// Internal errors may or may not be retryable depending on the cause.
//
// Example:
//
//	if errors.IsRetryable(err) {
//	    // implement retry with backoff
//	}
func IsRetryable(err error) bool {
	e, ok := AsError(err)
	if !ok {
		return false
	}
	switch e.Code.Category() {
	case "TIMEOUT", "UNAVAIL":
		return true
	default:
		return false
	}
}

// IsClientError checks if the error is a client error (4xx HTTP status).
// Client errors include validation, authentication, authorization, not found, and conflict.
//
// Example:
//
//	if errors.IsClientError(err) {
//	    // error is due to client request, not server issue
//	}
func IsClientError(err error) bool {
	e, ok := AsError(err)
	if !ok {
		return false
	}
	switch e.Code.Category() {
	case "VAL", "AUTH", "AUTHZ", "NF", "CONF":
		return true
	default:
		return false
	}
}

// IsServerError checks if the error is a server error (5xx HTTP status).
// Server errors include internal, unavailable, and timeout errors.
//
// Example:
//
//	if errors.IsServerError(err) {
//	    // error is due to server issue, may need alerting
//	}
func IsServerError(err error) bool {
	e, ok := AsError(err)
	if !ok {
		return false
	}
	switch e.Code.Category() {
	case "INT", "UNAVAIL", "TIMEOUT":
		return true
	default:
		return false
	}
}
