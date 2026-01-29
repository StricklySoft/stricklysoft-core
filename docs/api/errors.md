# Errors

This document describes the structured error system provided by the
`pkg/errors` package. It covers the error type, error codes, HTTP status
mapping, constructors, inspection functions, error wrapping, and security
considerations for services running on the StricklySoft Cloud Platform.

## Overview

Package `errors` provides structured error types with machine-readable
codes, HTTP status mapping, error wrapping, and inspection for services
on the StricklySoft Cloud Platform. Every error is an `*Error` value
carrying four fields:

- **Code** — a machine-readable `Code` (e.g., `"VAL_001"`) for
  programmatic handling, alerting, and documentation lookup
- **Message** — a human-readable description safe for end-user display
- **Cause** — an optional wrapped error for error chain inspection
- **Details** — an optional `map[string]any` for structured metadata
  such as field names, resource IDs, or constraint values

The package is imported as:

```go
import sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
```

Most platform code aliases the import to `sserr` to avoid collision with
the standard library `errors` package.

## Error Type

### Struct Definition

```go
type Error struct {
    Code    Code
    Message string
    Cause   error
    Details map[string]any
}
```

| Field     | Type              | Description                                         |
|-----------|-------------------|-----------------------------------------------------|
| `Code`    | `Code`            | Machine-readable error code (e.g., `"AUTH_001"`)    |
| `Message` | `string`          | Human-readable message; should not contain secrets  |
| `Cause`   | `error`           | Underlying error for chain inspection; may be nil   |
| `Details` | `map[string]any`  | Additional structured context; may be nil           |

### Methods

| Method                                          | Return     | Description                                                   |
|-------------------------------------------------|------------|---------------------------------------------------------------|
| `Error() string`                                | `string`   | Format: `"CODE: message: cause"` or `"CODE: message"`        |
| `Unwrap() error`                                | `error`    | Returns `Cause`; supports `errors.Is` / `errors.As` chains   |
| `HTTPStatus() int`                              | `int`      | Maps the code category to an HTTP status code                 |
| `WithDetails(details map[string]any) *Error`    | `*Error`   | Returns a new `Error` with merged details (immutable)         |
| `WithDetail(key string, value any) *Error`      | `*Error`   | Returns a new `Error` with one detail added (immutable)       |
| `Format(s fmt.State, verb rune)`                | —          | Implements `fmt.Formatter`; supports `%v`, `%+v`, `%q`       |

### Error() Output Format

The `Error()` method returns a string in one of two formats depending on
whether a `Cause` is present:

```
CODE: message            (no cause)
CODE: message: cause     (with cause)
```

Example:

```go
err := sserr.New(sserr.CodeValidation, "email address is invalid")
fmt.Println(err)
// Output: VAL_001: email address is invalid

wrapped := sserr.Wrap(dbErr, sserr.CodeInternalDatabase, "failed to fetch user")
fmt.Println(wrapped)
// Output: INT_002: failed to fetch user: connection refused
```

### Format() Verbs

The `Format` method implements `fmt.Formatter` for detailed output:

| Verb   | Output                                                                    |
|--------|---------------------------------------------------------------------------|
| `%v`   | Same as `Error()` — `"CODE: message"` or `"CODE: message: cause"`        |
| `%+v`  | Detailed: `Error{Code: "CODE", Message: "msg", Details: ..., Cause: ...}`|
| `%q`   | Quoted: `"\"CODE: message\""`                                             |

Example:

```go
err := sserr.New(sserr.CodeNotFound, "user not found").
    WithDetail("user_id", "usr-123")

fmt.Printf("%v\n", err)
// Output: NF_001: user not found

fmt.Printf("%+v\n", err)
// Output: Error{Code: "NF_001", Message: "user not found", Details: map[user_id:usr-123]}
```

### Immutable Details

`WithDetails` and `WithDetail` return a **new** `*Error` with the
additional details merged in. The original error is never modified.
This is safe for concurrent use and prevents accidental mutation when
errors are shared across goroutines.

```go
base := sserr.Validation("invalid input")
withField := base.WithDetail("field", "email")
withTwo := withField.WithDetail("reason", "format")

// base.Details is nil — unchanged
// withField.Details is map[field:email]
// withTwo.Details is map[field:email reason:format]
```

### HTTPStatus()

`HTTPStatus()` maps the error code's category to an HTTP status code.
See the [HTTP Status Mapping](#http-status-mapping) section for the
complete mapping table.

```go
err := sserr.NotFound("user not found")
status := err.HTTPStatus() // 404
```

## Code Type

`Code` is a named `string` type representing a machine-readable error
code. Codes follow the pattern `CATEGORY_XXX` where `CATEGORY` is a
short identifier and `XXX` is a three-digit numeric code.

```go
type Code string
```

### Methods

| Method              | Return   | Description                                                    |
|---------------------|----------|----------------------------------------------------------------|
| `String() string`   | `string` | Returns the string representation of the code                 |
| `Category() string` | `string` | Returns the prefix before the first underscore                 |

### Category Extraction

`Category()` returns the prefix before the first underscore character.
This groups related codes for HTTP status mapping and category checks.

```go
code := sserr.CodeValidationRequired // "VAL_002"
code.Category()                       // "VAL"
code.String()                         // "VAL_002"

code2 := sserr.CodeUnavailableDependency // "UNAVAIL_002"
code2.Category()                          // "UNAVAIL"
```

## Error Codes

The package defines 25 error codes across 8 categories. Error codes are
stable — once assigned, they do not change.

### Validation Errors (VAL_xxx) — HTTP 400

Used when request input fails validation rules.

| Constant                 | Value      | Description                        |
|--------------------------|------------|------------------------------------|
| `CodeValidation`         | `VAL_001`  | General validation failure         |
| `CodeValidationRequired` | `VAL_002`  | Required field is missing          |
| `CodeValidationFormat`   | `VAL_003`  | Field has an invalid format        |
| `CodeValidationRange`    | `VAL_004`  | Value is outside acceptable range  |

### Authentication Errors (AUTH_xxx) — HTTP 401

Used when authentication fails or credentials are invalid.

| Constant                    | Value       | Description                      |
|-----------------------------|-------------|----------------------------------|
| `CodeAuthentication`        | `AUTH_001`  | General authentication failure   |
| `CodeAuthenticationExpired` | `AUTH_002`  | Authentication token has expired |
| `CodeAuthenticationInvalid` | `AUTH_003`  | Authentication token is malformed|

### Authorization Errors (AUTHZ_xxx) — HTTP 403

Used when the authenticated identity lacks required permissions.

| Constant                              | Value        | Description                    |
|---------------------------------------|--------------|--------------------------------|
| `CodeAuthorization`                   | `AUTHZ_001`  | General authorization failure  |
| `CodeAuthorizationDenied`             | `AUTHZ_002`  | Access to a resource is denied |
| `CodeAuthorizationInsufficientScope`  | `AUTHZ_003`  | Token lacks required scopes    |

### Not Found Errors (NF_xxx) — HTTP 404

Used when a requested resource does not exist.

| Constant             | Value     | Description                         |
|----------------------|-----------|-------------------------------------|
| `CodeNotFound`       | `NF_001`  | General not found error             |
| `CodeNotFoundUser`   | `NF_002`  | Requested user was not found        |
| `CodeNotFoundResource`| `NF_003` | Requested resource was not found    |

### Conflict Errors (CONF_xxx) — HTTP 409

Used when an operation conflicts with the current state.

| Constant                       | Value       | Description                      |
|--------------------------------|-------------|----------------------------------|
| `CodeConflict`                 | `CONF_001`  | General conflict error           |
| `CodeConflictAlreadyExists`    | `CONF_002`  | Resource already exists          |
| `CodeConflictVersionMismatch`  | `CONF_003`  | Optimistic locking failure       |

### Internal Errors (INT_xxx) — HTTP 500

Used for unexpected internal failures.

| Constant                    | Value      | Description                      |
|-----------------------------|------------|----------------------------------|
| `CodeInternal`              | `INT_001`  | General internal error           |
| `CodeInternalDatabase`      | `INT_002`  | Database operation failed        |
| `CodeInternalConfiguration` | `INT_003`  | Configuration error              |

### Unavailable Errors (UNAVAIL_xxx) — HTTP 503

Used when a service is temporarily unavailable.

| Constant                     | Value          | Description                        |
|------------------------------|----------------|------------------------------------|
| `CodeUnavailable`            | `UNAVAIL_001`  | General service unavailable        |
| `CodeUnavailableDependency`  | `UNAVAIL_002`  | Dependent service is unavailable   |
| `CodeUnavailableOverloaded`  | `UNAVAIL_003`  | Service is overloaded              |

### Timeout Errors (TIMEOUT_xxx) — HTTP 504

Used when an operation exceeds its time limit.

| Constant               | Value          | Description                           |
|------------------------|----------------|---------------------------------------|
| `CodeTimeout`          | `TIMEOUT_001`  | General timeout error                 |
| `CodeTimeoutDatabase`  | `TIMEOUT_002`  | Database operation timed out          |
| `CodeTimeoutDependency`| `TIMEOUT_003`  | Call to a dependent service timed out |

## Constructors

### Core Constructors

| Function                                                      | Return   | Description                                                  |
|---------------------------------------------------------------|----------|--------------------------------------------------------------|
| `New(code Code, message string) *Error`                       | `*Error` | Creates an error with the given code and message             |
| `Newf(code Code, format string, args ...any) *Error`          | `*Error` | Creates an error with a `fmt.Sprintf`-formatted message      |
| `Wrap(err error, code Code, message string) *Error`           | `*Error` | Wraps `err` as the `Cause`; returns `nil` if `err` is `nil` |
| `Wrapf(err error, code Code, format string, args ...any) *Error`| `*Error`| Wraps `err` with a formatted message; returns `nil` if `err` is `nil` |

### Convenience Constructors

These functions create errors with a pre-assigned code, reducing
boilerplate for common error categories.

| Function                                        | Code                | Description                              |
|-------------------------------------------------|---------------------|------------------------------------------|
| `Validation(message string) *Error`             | `CodeValidation`    | General validation error                 |
| `Validationf(format string, args ...any) *Error`| `CodeValidation`   | Formatted validation error               |
| `NotFound(message string) *Error`               | `CodeNotFound`      | General not found error                  |
| `NotFoundf(format string, args ...any) *Error`  | `CodeNotFound`      | Formatted not found error                |
| `Unauthorized(message string) *Error`           | `CodeAuthentication`| Authentication failure                   |
| `Forbidden(message string) *Error`              | `CodeAuthorization` | Authorization failure                    |
| `Conflict(message string) *Error`               | `CodeConflict`      | Conflict error                           |
| `Internal(message string) *Error`               | `CodeInternal`      | Internal server error                    |
| `Internalf(format string, args ...any) *Error`  | `CodeInternal`      | Formatted internal error                 |
| `Unavailable(message string) *Error`            | `CodeUnavailable`   | Service unavailable error                |
| `Timeout(message string) *Error`                | `CodeTimeout`       | Timeout error                            |

### FromError

```go
func FromError(err error) *Error
```

`FromError` converts a standard `error` to an `*Error`. If the error is
already an `*Error` (anywhere in the error chain via `errors.As`), it is
returned as-is. Otherwise, the error is wrapped with `CodeInternal` and
the message `"an unexpected error occurred"`. Returns `nil` if `err` is
`nil`.

```go
// Standard error becomes an internal error
err := fmt.Errorf("disk full")
platformErr := sserr.FromError(err)
// platformErr.Code == CodeInternal
// platformErr.Cause == err

// Platform error passes through unchanged
existing := sserr.NotFound("user not found")
same := sserr.FromError(existing)
// same == existing
```

## Inspection Functions

### Error Extraction

| Function                            | Return            | Description                                                  |
|-------------------------------------|-------------------|--------------------------------------------------------------|
| `AsError(err error) (*Error, bool)` | `*Error`, `bool`  | Traverses the error chain with `errors.As`; returns the first `*Error` found |
| `GetCode(err error) Code`           | `Code`            | Returns the error code, or empty string `""` if not an `*Error` |
| `HasCode(err error, code Code) bool`| `bool`            | Returns `true` if the error has the specified code           |

### Category Checks

Each category check function extracts the `*Error` from the error chain
and compares its `Code.Category()` against the expected category string.
All return `false` for `nil` errors or errors that are not `*Error`.

| Function                        | Category   | True When                                       |
|---------------------------------|------------|-------------------------------------------------|
| `IsValidation(err error) bool`  | `"VAL"`    | Error is a validation error (VAL_xxx)           |
| `IsAuthentication(err error) bool`| `"AUTH"` | Error is an authentication error (AUTH_xxx)     |
| `IsAuthorization(err error) bool`| `"AUTHZ"` | Error is an authorization error (AUTHZ_xxx)     |
| `IsNotFound(err error) bool`    | `"NF"`     | Error is a not found error (NF_xxx)             |
| `IsConflict(err error) bool`    | `"CONF"`   | Error is a conflict error (CONF_xxx)            |
| `IsInternal(err error) bool`    | `"INT"`    | Error is an internal error (INT_xxx)            |
| `IsUnavailable(err error) bool` | `"UNAVAIL"`| Error is a service unavailable error (UNAVAIL_xxx)|
| `IsTimeout(err error) bool`     | `"TIMEOUT"`| Error is a timeout error (TIMEOUT_xxx)          |

### Aggregate Checks

These functions classify errors at a higher level than individual
categories.

| Function                          | True When                                           | Categories                       |
|-----------------------------------|-----------------------------------------------------|----------------------------------|
| `IsRetryable(err error) bool`     | Error is potentially retryable                      | `TIMEOUT`, `UNAVAIL`             |
| `IsClientError(err error) bool`   | Error is caused by the client request (4xx)         | `VAL`, `AUTH`, `AUTHZ`, `NF`, `CONF` |
| `IsServerError(err error) bool`   | Error is caused by a server-side issue (5xx)        | `INT`, `UNAVAIL`, `TIMEOUT`      |

## HTTP Status Mapping

The `HTTPStatus()` method maps the error code's category to an HTTP
status code. Unknown categories default to `500 Internal Server Error`.

| Category   | HTTP Status | Constant                           |
|------------|-------------|------------------------------------|
| `VAL`      | 400         | `http.StatusBadRequest`            |
| `AUTH`     | 401         | `http.StatusUnauthorized`          |
| `AUTHZ`    | 403         | `http.StatusForbidden`             |
| `NF`       | 404         | `http.StatusNotFound`              |
| `CONF`     | 409         | `http.StatusConflict`              |
| `INT`      | 500         | `http.StatusInternalServerError`   |
| `UNAVAIL`  | 503         | `http.StatusServiceUnavailable`    |
| `TIMEOUT`  | 504         | `http.StatusGatewayTimeout`        |

## Usage Examples

### Creating Errors

```go
import sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"

// Simple error with code and message
err := sserr.New(sserr.CodeValidation, "email address is required")

// Formatted message
err := sserr.Newf(sserr.CodeNotFoundUser, "user %q not found", userID)

// Convenience constructors
err := sserr.Validation("email address is invalid")
err := sserr.NotFoundf("user %q not found", userID)
err := sserr.Unauthorized("invalid authentication token")
err := sserr.Forbidden("insufficient permissions to delete resource")
err := sserr.Conflict("user with email already exists")
err := sserr.Internal("an unexpected error occurred")
err := sserr.Unavailable("database is temporarily unavailable")
err := sserr.Timeout("request timed out after 30s")
```

### Wrapping Errors

Use `Wrap` and `Wrapf` to add platform error context to errors from
external libraries, the standard library, or lower-level operations.
Both return `nil` when the input error is `nil`, which is safe for
one-line wrapping patterns.

```go
result, err := db.Query(ctx, sql)
if err != nil {
    return sserr.Wrap(err, sserr.CodeInternalDatabase, "failed to fetch user")
}

// With formatted message
row, err := db.QueryRow(ctx, sql, userID)
if err != nil {
    return sserr.Wrapf(err, sserr.CodeInternalDatabase,
        "failed to fetch user %q", userID)
}

// Safe nil handling — no nil check needed
return sserr.Wrap(maybeNilErr, sserr.CodeInternal, "processing failed")
// Returns nil if maybeNilErr is nil
```

### Adding Details

Details provide structured metadata for logging, debugging, or API
response bodies. Both `WithDetails` and `WithDetail` are immutable —
they return a new error, leaving the original unchanged.

```go
err := sserr.New(sserr.CodeValidationRequired, "required fields missing").
    WithDetails(map[string]any{
        "fields": []string{"email", "name"},
        "form":   "registration",
    })

// Or add details one at a time
err := sserr.Validation("value out of range").
    WithDetail("field", "age").
    WithDetail("min", 0).
    WithDetail("max", 150)
```

### Inspecting Errors

```go
// Extract the platform error from any error chain
if e, ok := sserr.AsError(err); ok {
    logger.Error("operation failed",
        "code", e.Code,
        "message", e.Message,
        "http_status", e.HTTPStatus(),
    )
}

// Check for a specific code
if sserr.HasCode(err, sserr.CodeConflictAlreadyExists) {
    // handle duplicate resource
}

// Get the code for a switch statement
switch sserr.GetCode(err) {
case sserr.CodeNotFoundUser:
    // handle missing user
case sserr.CodeNotFoundResource:
    // handle missing resource
default:
    // handle other cases
}
```

### Category-Based Error Handling

Category checks match all codes within a category. For example,
`IsValidation` matches `VAL_001`, `VAL_002`, `VAL_003`, and `VAL_004`.

```go
func handleError(w http.ResponseWriter, err error) {
    e, ok := sserr.AsError(err)
    if !ok {
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }

    // Use HTTPStatus() for the response code
    http.Error(w, e.Message, e.HTTPStatus())
}
```

### Retry Logic

```go
func executeWithRetry(ctx context.Context, fn func() error) error {
    var lastErr error
    for attempt := 0; attempt < 3; attempt++ {
        lastErr = fn()
        if lastErr == nil {
            return nil
        }
        if !sserr.IsRetryable(lastErr) {
            return lastErr // not retryable, fail immediately
        }
        // back off before retrying
        time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
    }
    return lastErr
}
```

### Client vs Server Error Classification

```go
func logError(err error) {
    if sserr.IsClientError(err) {
        // Client errors are expected — log at WARN level
        slog.Warn("client error", "error", err)
    } else if sserr.IsServerError(err) {
        // Server errors need attention — log at ERROR level and alert
        slog.Error("server error", "error", err)
        alerting.Notify(err)
    }
}
```

### Converting Standard Errors

```go
func processRequest(r *http.Request) error {
    result, err := externalLib.DoSomething(r.Body)
    if err != nil {
        // Convert any error into a platform error for uniform handling
        return sserr.FromError(err)
    }
    return nil
}
```

### Error Chain Traversal

The `*Error` type implements `Unwrap()`, which means standard library
functions `errors.Is` and `errors.As` traverse through platform errors.
The `AsError` function uses `errors.As` internally, so it finds `*Error`
values anywhere in a wrapped error chain.

```go
// Wrap a platform error in a standard fmt.Errorf chain
inner := sserr.NotFound("user not found")
outer := fmt.Errorf("service layer: %w", inner)

// AsError finds the platform error through the chain
if e, ok := sserr.AsError(outer); ok {
    fmt.Println(e.Code) // NF_001
}

// Category checks also traverse the chain
sserr.IsNotFound(outer) // true
```

### HTTP Handler Integration

```go
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
    user, err := h.service.FindUser(r.Context(), r.PathValue("id"))
    if err != nil {
        e := sserr.FromError(err)

        // Log server errors with full detail
        if sserr.IsServerError(e) {
            slog.ErrorContext(r.Context(), "request failed",
                "code", e.Code,
                "error", fmt.Sprintf("%+v", e),
            )
        }

        // Respond with appropriate HTTP status
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(e.HTTPStatus())
        json.NewEncoder(w).Encode(map[string]any{
            "error": map[string]any{
                "code":    e.Code,
                "message": e.Message,
            },
        })
        return
    }

    // success response...
}
```

## Security Considerations

1. **No sensitive data in messages** — Error messages may be returned to
   API clients. They should never contain passwords, file system paths,
   stack traces, internal IP addresses, or personally identifiable
   information (PII).

2. **Codes for programmatic handling** — Use error codes (not message
   string matching) for programmatic error handling. Codes are stable
   and machine-readable; messages are for human context only.

3. **Internal details not exposed** — Server errors (`INT_xxx`,
   `UNAVAIL_xxx`, `TIMEOUT_xxx`) should not expose implementation
   details to API clients. Log the full `%+v` representation
   server-side and return a generic message to the client.

4. **Immutable details** — `WithDetails` and `WithDetail` return new
   `*Error` instances. The original error is not modified, preventing
   accidental information leakage when errors are shared or cached.

5. **Error chain safety** — When wrapping errors with `Wrap`, ensure the
   cause error does not contain sensitive information that could be
   exposed through `Error()` string formatting.

## File Structure

```
pkg/errors/
    errors.go          Package documentation
    types.go           Error struct, methods (Error, Unwrap, HTTPStatus, WithDetails, Format)
    codes.go           Code type, String(), Category(), 25 error code constants
    constructors.go    New, Newf, Wrap, Wrapf, convenience constructors, FromError
    checks.go          AsError, GetCode, HasCode, Is* category checks, aggregate checks
```
