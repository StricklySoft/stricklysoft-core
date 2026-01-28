package errors

// Code represents a machine-readable error code for categorizing errors.
// Error codes follow the pattern CATEGORY_XXX where CATEGORY is a short
// identifier (e.g., AUTH, VAL, INT) and XXX is a three-digit numeric code.
//
// Error codes are designed to be:
//   - Stable: Codes do not change once assigned
//   - Unique: Each error condition has a distinct code
//   - Searchable: Codes can be used to find documentation and solutions
//   - Machine-readable: Suitable for automated error handling
type Code string

// Error code categories and their ranges:
//
//	VAL_xxx  - Validation errors (400 Bad Request)
//	AUTH_xxx - Authentication errors (401 Unauthorized)
//	AUTHZ_xxx - Authorization errors (403 Forbidden)
//	NF_xxx   - Not found errors (404 Not Found)
//	CONF_xxx - Conflict errors (409 Conflict)
//	INT_xxx  - Internal errors (500 Internal Server Error)
//	UNAVAIL_xxx - Service unavailable (503 Service Unavailable)
//	TIMEOUT_xxx - Timeout errors (504 Gateway Timeout)
const (
	// Validation errors (VAL_xxx) - HTTP 400
	// Used when request input fails validation rules.

	// CodeValidation indicates a general validation failure.
	CodeValidation Code = "VAL_001"

	// CodeValidationRequired indicates a required field is missing.
	CodeValidationRequired Code = "VAL_002"

	// CodeValidationFormat indicates a field has an invalid format.
	CodeValidationFormat Code = "VAL_003"

	// CodeValidationRange indicates a value is outside acceptable range.
	CodeValidationRange Code = "VAL_004"

	// Authentication errors (AUTH_xxx) - HTTP 401
	// Used when authentication fails or credentials are invalid.

	// CodeAuthentication indicates a general authentication failure.
	CodeAuthentication Code = "AUTH_001"

	// CodeAuthenticationExpired indicates the authentication token has expired.
	CodeAuthenticationExpired Code = "AUTH_002"

	// CodeAuthenticationInvalid indicates the authentication token is malformed.
	CodeAuthenticationInvalid Code = "AUTH_003"

	// Authorization errors (AUTHZ_xxx) - HTTP 403
	// Used when the authenticated identity lacks required permissions.

	// CodeAuthorization indicates a general authorization failure.
	CodeAuthorization Code = "AUTHZ_001"

	// CodeAuthorizationDenied indicates access to a resource is denied.
	CodeAuthorizationDenied Code = "AUTHZ_002"

	// CodeAuthorizationInsufficientScope indicates the token lacks required scopes.
	CodeAuthorizationInsufficientScope Code = "AUTHZ_003"

	// Not found errors (NF_xxx) - HTTP 404
	// Used when a requested resource does not exist.

	// CodeNotFound indicates a general not found error.
	CodeNotFound Code = "NF_001"

	// CodeNotFoundUser indicates the requested user was not found.
	CodeNotFoundUser Code = "NF_002"

	// CodeNotFoundResource indicates the requested resource was not found.
	CodeNotFoundResource Code = "NF_003"

	// Conflict errors (CONF_xxx) - HTTP 409
	// Used when an operation conflicts with current state.

	// CodeConflict indicates a general conflict error.
	CodeConflict Code = "CONF_001"

	// CodeConflictAlreadyExists indicates the resource already exists.
	CodeConflictAlreadyExists Code = "CONF_002"

	// CodeConflictVersionMismatch indicates an optimistic locking failure.
	CodeConflictVersionMismatch Code = "CONF_003"

	// Internal errors (INT_xxx) - HTTP 500
	// Used for unexpected internal failures.

	// CodeInternal indicates a general internal error.
	CodeInternal Code = "INT_001"

	// CodeInternalDatabase indicates a database operation failed.
	CodeInternalDatabase Code = "INT_002"

	// CodeInternalConfiguration indicates a configuration error.
	CodeInternalConfiguration Code = "INT_003"

	// Unavailable errors (UNAVAIL_xxx) - HTTP 503
	// Used when a service is temporarily unavailable.

	// CodeUnavailable indicates a general service unavailable error.
	CodeUnavailable Code = "UNAVAIL_001"

	// CodeUnavailableDependency indicates a dependent service is unavailable.
	CodeUnavailableDependency Code = "UNAVAIL_002"

	// CodeUnavailableOverloaded indicates the service is overloaded.
	CodeUnavailableOverloaded Code = "UNAVAIL_003"

	// Timeout errors (TIMEOUT_xxx) - HTTP 504
	// Used when an operation exceeds its time limit.

	// CodeTimeout indicates a general timeout error.
	CodeTimeout Code = "TIMEOUT_001"

	// CodeTimeoutDatabase indicates a database operation timed out.
	CodeTimeoutDatabase Code = "TIMEOUT_002"

	// CodeTimeoutDependency indicates a call to a dependent service timed out.
	CodeTimeoutDependency Code = "TIMEOUT_003"
)

// String returns the string representation of the error code.
func (c Code) String() string {
	return string(c)
}

// Category returns the category prefix of the error code (e.g., "VAL", "AUTH").
func (c Code) Category() string {
	s := string(c)
	for i, r := range s {
		if r == '_' {
			return s[:i]
		}
	}
	return s
}
