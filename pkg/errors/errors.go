// Package errors provides standardized error types and error handling utilities
// for the StricklySoft platform. It defines common error categories, error codes,
// and helper functions for creating, wrapping, and inspecting errors across all
// platform services.
//
// # Error Categories
//
// The package defines several error categories that map to common failure scenarios:
//
//   - Validation errors: Invalid input, missing required fields
//   - Authentication errors: Invalid credentials, expired tokens
//   - Authorization errors: Insufficient permissions, access denied
//   - NotFound errors: Resource does not exist
//   - Conflict errors: Resource already exists, version mismatch
//   - Internal errors: Unexpected system failures
//   - Unavailable errors: Service temporarily unavailable
//   - Timeout errors: Operation exceeded time limit
//
// # Error Codes
//
// Each error includes a machine-readable code (e.g., "AUTH_001") that can be used
// for error tracking, alerting, and client-side error handling. Error codes follow
// the pattern: CATEGORY_XXX where CATEGORY is a short identifier and XXX is a
// numeric code.
//
// # Usage
//
// Create a new error with context:
//
//	err := errors.New(errors.CodeValidation, "email address is invalid")
//
// Wrap an existing error:
//
//	err := errors.Wrap(err, errors.CodeInternal, "failed to process request")
//
// Check error category:
//
//	if errors.IsNotFound(err) {
//	    // handle not found
//	}
//
// Extract error details for logging:
//
//	if e, ok := errors.AsError(err); ok {
//	    logger.Error("operation failed",
//	        "code", e.Code,
//	        "message", e.Message,
//	    )
//	}
package errors
