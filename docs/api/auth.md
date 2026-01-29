# Authentication & Identity Propagation

This document describes the authentication, authorization, and identity
propagation system provided by the `pkg/auth` package. It covers identity
types, context propagation, gRPC interceptors, HTTP middleware,
serialization, and security considerations for services running on the
StricklySoft Cloud Platform.

## Overview

When requests flow through multiple services on the platform, the
original caller's identity must be preserved for authorization and
audit. The `pkg/auth` package provides:

- **Identity abstraction** -- A uniform `Identity` interface with
  concrete implementations for users, services, agents, and system
  processes
- **Context propagation** -- Functions for storing and retrieving
  identity, caller service, and call chain information from
  `context.Context`
- **gRPC interceptors** -- Unary and stream interceptors for both
  server-side validation and client-side propagation
- **HTTP middleware** -- Handler middleware for inbound requests and a
  `RoundTripper` for outbound propagation
- **Serialization** -- Base64url-encoded JSON for safe transport of
  claims and call chains across service boundaries

The package does not implement JWT validation or RBAC directly.
Instead, it defines the `TokenValidator` interface so that each service
can plug in its own validation strategy. Placeholder files for JWT,
Kubernetes ServiceAccount, and RBAC integration are included for future
implementation.

## Identity Types

### IdentityType

`IdentityType` is a `string` enum representing the category of caller.

| Constant              | Value       | Description                                   |
|-----------------------|-------------|-----------------------------------------------|
| `IdentityTypeUser`    | `"user"`    | Human user authenticated via JWT/OAuth        |
| `IdentityTypeService` | `"service"` | Platform service via service account          |
| `IdentityTypeAgent`   | `"agent"`   | AI agent operating within the platform        |
| `IdentityTypeSystem`  | `"system"`  | Internal system process (background jobs, cron)|

#### Methods

| Method   | Signature          | Description                                    |
|----------|--------------------|------------------------------------------------|
| `String` | `String() string`  | Returns the string value of the identity type  |
| `Valid`  | `Valid() bool`     | Returns `true` if the value is one of the four defined constants |

```go
idType := auth.IdentityTypeAgent
fmt.Println(idType.String()) // "agent"
fmt.Println(idType.Valid())   // true

invalid := auth.IdentityType("unknown")
fmt.Println(invalid.Valid()) // false
```

## Identity Interface

The `Identity` interface defines the contract for all identity
representations in the platform:

```go
type Identity interface {
    ID() string
    Type() IdentityType
    Claims() map[string]any
    HasPermission(resource, action string) bool
}
```

| Method          | Description                                               |
|-----------------|-----------------------------------------------------------|
| `ID`            | Returns the unique identifier for the identity            |
| `Type`          | Returns the `IdentityType` enum value                     |
| `Claims`        | Returns a defensive copy of the claims map                |
| `HasPermission` | Checks whether the identity has the specified permission  |

All concrete implementations are safe for concurrent use by multiple
goroutines.

## TokenValidator Interface

The `TokenValidator` interface abstracts token validation so that each
service can provide its own implementation:

```go
type TokenValidator interface {
    Validate(ctx context.Context, token string) (Identity, error)
}
```

| Method     | Description                                                  |
|------------|--------------------------------------------------------------|
| `Validate` | Accepts a raw token string and returns the corresponding `Identity`, or an error if the token is invalid or expired |

Consumers implement this interface and pass it to the gRPC interceptors
and HTTP middleware. The package does not ship a production validator;
`jwt.go` is a placeholder for future JWT-based validation.

```go
type MyValidator struct {
    keySet jwk.Set
}

func (v *MyValidator) Validate(ctx context.Context, token string) (auth.Identity, error) {
    // Parse and verify JWT, extract claims, build Identity
    // ...
}
```

## BasicIdentity

`BasicIdentity` is the simplest concrete implementation of `Identity`.
It is primarily used at the transport level when deserializing identity
information from headers.

### Construction

```go
func NewBasicIdentity(id string, idType IdentityType, claims map[string]any) *BasicIdentity
```

| Parameter | Type                 | Description                        |
|-----------|----------------------|------------------------------------|
| `id`      | `string`             | Unique identifier for the identity |
| `idType`  | `IdentityType`       | Category of caller                 |
| `claims`  | `map[string]any`     | Arbitrary claims (defensively copied) |

### Behavior

- `Claims()` returns a defensive copy of the internal claims map.
- `HasPermission()` always returns `false`. `BasicIdentity` is a
  transport-level type and does not carry permission information.

```go
identity := auth.NewBasicIdentity("user-123", auth.IdentityTypeUser, map[string]any{
    "email": "dev@example.com",
    "role":  "admin",
})

fmt.Println(identity.ID())                          // "user-123"
fmt.Println(identity.Type())                         // "user"
fmt.Println(identity.Claims()["email"])              // "dev@example.com"
fmt.Println(identity.HasPermission("pods", "read"))  // false
```

## Permission

`Permission` represents a resource-action pair used for authorization
checks.

```go
type Permission struct {
    Resource string
    Action   string
}
```

| Field      | Type     | Description                                      |
|------------|----------|--------------------------------------------------|
| `Resource` | `string` | The resource being accessed; `"*"` matches any   |
| `Action`   | `string` | The action being performed; `"*"` matches any    |

Wildcard matching follows these rules:

- `Resource: "*"` matches any resource.
- `Action: "*"` matches any action.
- `Resource: "*", Action: "*"` grants unrestricted access.
- Exact string comparison is used for non-wildcard values.

## ServiceIdentity

`ServiceIdentity` represents a platform service and implements the
`Identity` interface with permission-based authorization.

### Construction

```go
func NewServiceIdentity(
    id, serviceName, namespace string,
    claims map[string]any,
    permissions []Permission,
) (*ServiceIdentity, error)
```

| Parameter     | Type             | Required | Description                          |
|---------------|------------------|----------|--------------------------------------|
| `id`          | `string`         | Yes      | Unique identifier                    |
| `serviceName` | `string`         | Yes      | Name of the service                  |
| `namespace`   | `string`         | No       | Kubernetes namespace (may be empty)  |
| `claims`      | `map[string]any` | No       | Arbitrary claims (defensively copied)|
| `permissions` | `[]Permission`   | No       | Granted permissions (defensively copied) |

Returns an error if `id` or `serviceName` is empty.

### Methods

| Method        | Signature                  | Description                              |
|---------------|----------------------------|------------------------------------------|
| `ID`          | `ID() string`              | Returns the unique identifier            |
| `Type`        | `Type() IdentityType`      | Returns `IdentityTypeService`            |
| `Claims`      | `Claims() map[string]any`  | Returns a defensive copy of claims       |
| `HasPermission` | `HasPermission(resource, action string) bool` | Checks permissions with wildcard support |
| `ServiceName` | `ServiceName() string`     | Returns the service name                 |
| `Namespace`   | `Namespace() string`       | Returns the Kubernetes namespace         |
| `Permissions` | `Permissions() []Permission` | Returns a defensive copy of permissions |

### Example

```go
svc, err := auth.NewServiceIdentity(
    "svc-agent-mgr-001",
    "agent-manager",
    "production",
    map[string]any{"tier": "platform"},
    []auth.Permission{
        {Resource: "agents", Action: "*"},
        {Resource: "executions", Action: "read"},
    },
)
if err != nil {
    return err
}

fmt.Println(svc.HasPermission("agents", "create"))     // true  (wildcard action)
fmt.Println(svc.HasPermission("executions", "read"))    // true  (exact match)
fmt.Println(svc.HasPermission("executions", "delete"))  // false
fmt.Println(svc.ServiceName())                          // "agent-manager"
fmt.Println(svc.Namespace())                            // "production"
```

## UserIdentity

`UserIdentity` represents a human user and implements the `Identity`
interface with permission-based authorization.

### Construction

```go
func NewUserIdentity(
    id, email, displayName string,
    claims map[string]any,
    permissions []Permission,
) (*UserIdentity, error)
```

| Parameter     | Type             | Required | Description                          |
|---------------|------------------|----------|--------------------------------------|
| `id`          | `string`         | Yes      | Unique identifier                    |
| `email`       | `string`         | Yes      | Email address                        |
| `displayName` | `string`         | No       | Human-readable display name          |
| `claims`      | `map[string]any` | No       | Arbitrary claims (defensively copied)|
| `permissions` | `[]Permission`   | No       | Granted permissions (defensively copied) |

Returns an error if `id` or `email` is empty.

### Methods

| Method          | Signature                    | Description                            |
|-----------------|------------------------------|----------------------------------------|
| `ID`            | `ID() string`                | Returns the unique identifier          |
| `Type`          | `Type() IdentityType`        | Returns `IdentityTypeUser`             |
| `Claims`        | `Claims() map[string]any`    | Returns a defensive copy of claims     |
| `HasPermission` | `HasPermission(resource, action string) bool` | Checks permissions with wildcard support |
| `Email`         | `Email() string`             | Returns the email address              |
| `DisplayName`   | `DisplayName() string`       | Returns the display name               |
| `Permissions`   | `Permissions() []Permission` | Returns a defensive copy of permissions|

### Example

```go
user, err := auth.NewUserIdentity(
    "usr-42",
    "ada@example.com",
    "Ada Lovelace",
    map[string]any{"org": "engineering"},
    []auth.Permission{
        {Resource: "executions", Action: "read"},
        {Resource: "executions", Action: "create"},
    },
)
if err != nil {
    return err
}

fmt.Println(user.Email())                                // "ada@example.com"
fmt.Println(user.HasPermission("executions", "create"))  // true
fmt.Println(user.HasPermission("executions", "delete"))  // false
```

## Call Chain

The call chain tracks the sequence of services a request has traversed,
enabling distributed audit trails and loop detection.

### CallerInfo

```go
type CallerInfo struct {
    ServiceName  string       `json:"service_name"`
    IdentityID   string       `json:"identity_id"`
    IdentityType IdentityType `json:"identity_type"`
}
```

| Field          | Type           | Description                            |
|----------------|----------------|----------------------------------------|
| `ServiceName`  | `string`       | Name of the calling service            |
| `IdentityID`   | `string`       | Identity ID at that hop                |
| `IdentityType` | `IdentityType` | Type of identity at that hop           |

### CallChain

```go
type CallChain struct {
    OriginalID   string       `json:"original_id"`
    OriginalType IdentityType `json:"original_type"`
    Callers      []CallerInfo `json:"callers"`
}
```

| Field          | Type           | Description                                |
|----------------|----------------|--------------------------------------------|
| `OriginalID`   | `string`       | Identity ID of the original caller         |
| `OriginalType` | `IdentityType` | Identity type of the original caller       |
| `Callers`      | `[]CallerInfo` | Ordered list of intermediate service hops  |

#### Methods

| Method         | Signature                              | Description                                               |
|----------------|----------------------------------------|-----------------------------------------------------------|
| `Depth`        | `Depth() int`                          | Returns `len(Callers)`                                    |
| `AppendCaller` | `AppendCaller(caller CallerInfo) *CallChain` | Returns a new chain with the caller appended; truncates if depth exceeds `MaxCallChainDepth` |

`AppendCaller` is non-mutating -- it returns a new `*CallChain`
instance, leaving the original unchanged.

### Constants

| Constant             | Value  | Description                                    |
|----------------------|--------|------------------------------------------------|
| `MaxCallChainDepth`  | `32`   | Maximum number of callers in a call chain      |
| `MaxHeaderValueSize` | `8192` | Maximum serialized header size in bytes (8 KB) |

### Example

```go
chain := &auth.CallChain{
    OriginalID:   "usr-42",
    OriginalType: auth.IdentityTypeUser,
}

chain = chain.AppendCaller(auth.CallerInfo{
    ServiceName:  "api-gateway",
    IdentityID:   "usr-42",
    IdentityType: auth.IdentityTypeUser,
})

chain = chain.AppendCaller(auth.CallerInfo{
    ServiceName:  "agent-manager",
    IdentityID:   "svc-agent-mgr-001",
    IdentityType: auth.IdentityTypeService,
})

fmt.Println(chain.Depth()) // 2
```

## Context Functions

The package provides functions for storing and retrieving auth-related
values in `context.Context`. These functions are the primary integration
point for application code.

### Identity Context

| Function                    | Signature                                                   | Description                                      |
|-----------------------------|-------------------------------------------------------------|--------------------------------------------------|
| `ContextWithIdentity`       | `ContextWithIdentity(ctx context.Context, identity Identity) context.Context` | Stores an identity in the context               |
| `IdentityFromContext`       | `IdentityFromContext(ctx context.Context) (Identity, bool)` | Retrieves the identity; returns `false` if absent |
| `MustIdentityFromContext`   | `MustIdentityFromContext(ctx context.Context) Identity`     | Retrieves the identity; **panics** if absent      |

### Caller Service Context

| Function                    | Signature                                                              | Description                                          |
|-----------------------------|------------------------------------------------------------------------|------------------------------------------------------|
| `ContextWithCallerService`  | `ContextWithCallerService(ctx context.Context, serviceName string) context.Context` | Stores the upstream caller service name    |
| `CallerServiceFromContext`  | `CallerServiceFromContext(ctx context.Context) (string, bool)`         | Retrieves the caller service; returns `false` if absent |

### Call Chain Context

| Function                    | Signature                                                                  | Description                                      |
|-----------------------------|----------------------------------------------------------------------------|--------------------------------------------------|
| `ContextWithCallChain`      | `ContextWithCallChain(ctx context.Context, chain *CallChain) context.Context` | Stores a call chain in the context            |
| `CallChainFromContext`      | `CallChainFromContext(ctx context.Context) (*CallChain, bool)`             | Retrieves the call chain; returns `false` if absent |

### Trace Context

| Function             | Signature                                                    | Description                                      |
|----------------------|--------------------------------------------------------------|--------------------------------------------------|
| `TraceIDFromContext` | `TraceIDFromContext(ctx context.Context) (string, bool)`     | Extracts the OpenTelemetry trace ID              |
| `SpanIDFromContext`  | `SpanIDFromContext(ctx context.Context) (string, bool)`      | Extracts the OpenTelemetry span ID               |

### Example

```go
import "github.com/StricklySoft/stricklysoft-core/pkg/auth"

// Store identity in context
ctx = auth.ContextWithIdentity(ctx, identity)

// Retrieve identity downstream
id, ok := auth.IdentityFromContext(ctx)
if !ok {
    return errors.New("no identity in context")
}

// In handlers where identity is guaranteed
id = auth.MustIdentityFromContext(ctx)

// Propagate caller service
ctx = auth.ContextWithCallerService(ctx, "api-gateway")
caller, ok := auth.CallerServiceFromContext(ctx)
```

## gRPC Integration

The package provides unary and stream interceptors for both server-side
validation and client-side identity propagation.

### Server Interceptors

Server interceptors extract the bearer token from gRPC metadata,
validate it using the provided `TokenValidator`, and store the
resulting `Identity` in the request context. They also extract caller
service and call chain information from metadata headers.

| Function                  | Signature                                                                                    |
|---------------------------|----------------------------------------------------------------------------------------------|
| `UnaryServerInterceptor`  | `UnaryServerInterceptor(validator TokenValidator, serviceName string) grpc.UnaryServerInterceptor`   |
| `StreamServerInterceptor` | `StreamServerInterceptor(validator TokenValidator, serviceName string) grpc.StreamServerInterceptor` |

#### Behavior

1. Extracts the `"authorization"` key from incoming gRPC metadata.
2. Strips the `"Bearer "` prefix to obtain the raw token.
3. Calls `validator.Validate(ctx, token)` to obtain an `Identity`.
4. On validation failure, returns a gRPC `Unauthenticated` status.
5. On success, stores the `Identity` in the context via
   `ContextWithIdentity`.
6. Extracts caller service and call chain from metadata headers and
   stores them in the context.

#### Example

```go
import (
    "google.golang.org/grpc"
    "github.com/StricklySoft/stricklysoft-core/pkg/auth"
)

server := grpc.NewServer(
    grpc.UnaryInterceptor(auth.UnaryServerInterceptor(validator, "my-service")),
    grpc.StreamInterceptor(auth.StreamServerInterceptor(validator, "my-service")),
)
```

### Client Interceptors

Client interceptors propagate the identity from the outgoing context
into gRPC metadata. They also build or extend the call chain to include
the current service.

| Function                  | Signature                                                                                    |
|---------------------------|----------------------------------------------------------------------------------------------|
| `UnaryClientInterceptor`  | `UnaryClientInterceptor(serviceName string) grpc.UnaryClientInterceptor`                     |
| `StreamClientInterceptor` | `StreamClientInterceptor(serviceName string) grpc.StreamClientInterceptor`                   |

#### Behavior

1. Reads the `Identity` from the context.
2. Serializes identity ID, type, and claims into outgoing metadata
   headers.
3. Reads or initializes a `CallChain` from the context.
4. Appends the current service as a caller and serializes the chain
   into outgoing metadata.

#### Example

```go
conn, err := grpc.Dial(target,
    grpc.WithUnaryInterceptor(auth.UnaryClientInterceptor("my-service")),
    grpc.WithStreamInterceptor(auth.StreamClientInterceptor("my-service")),
)
```

## HTTP Integration

### HTTPMiddleware

`HTTPMiddleware` extracts the `Authorization` header from inbound HTTP
requests, validates the bearer token, and stores the resulting
`Identity` in the request context.

```go
func HTTPMiddleware(validator TokenValidator, serviceName string) func(http.Handler) http.Handler
```

#### Behavior

1. Reads the `Authorization` header from the request.
2. Extracts the bearer token using `ExtractBearerToken`.
3. Calls `validator.Validate(ctx, token)` to obtain an `Identity`.
4. On validation failure, responds with HTTP `401 Unauthorized`.
5. On success, stores the `Identity` in the context and calls the
   next handler.

#### Example

```go
import (
    "net/http"
    "github.com/StricklySoft/stricklysoft-core/pkg/auth"
)

mux := http.NewServeMux()
mux.HandleFunc("/api/agents", handleListAgents)

handler := auth.HTTPMiddleware(validator, "api-gateway")(mux)
http.ListenAndServe(":8080", handler)
```

### PropagatingRoundTripper

`PropagatingRoundTripper` wraps an `http.RoundTripper` to propagate
identity information to outbound HTTP requests.

#### Construction

```go
func NewPropagatingRoundTripper(serviceName string, transport http.RoundTripper) *PropagatingRoundTripper
```

| Parameter     | Type                | Description                                  |
|---------------|---------------------|----------------------------------------------|
| `serviceName` | `string`            | Name of the current service                  |
| `transport`   | `http.RoundTripper` | Underlying transport (e.g., `http.DefaultTransport`) |

#### Methods

| Method      | Signature                                                     | Description                                          |
|-------------|---------------------------------------------------------------|------------------------------------------------------|
| `RoundTrip` | `RoundTrip(r *http.Request) (*http.Response, error)`          | Propagates identity headers and delegates to the underlying transport |

#### Example

```go
client := &http.Client{
    Transport: auth.NewPropagatingRoundTripper("agent-manager", http.DefaultTransport),
}

// Identity from the context is automatically propagated
req, _ := http.NewRequestWithContext(ctx, "GET", "http://execution-svc/api/runs", nil)
resp, err := client.Do(req)
```

## Serialization and Transport

### Header Constants

The package defines standard header names used for identity propagation
across service boundaries.

| Constant                | Value                  | Description                          |
|-------------------------|------------------------|--------------------------------------|
| `HeaderAuthorization`   | `"authorization"`      | Bearer token header                  |
| `HeaderIdentityID`      | `"x-identity-id"`      | Identity unique identifier           |
| `HeaderIdentityType`    | `"x-identity-type"`    | Identity type enum value             |
| `HeaderIdentityClaims`  | `"x-identity-claims"`  | Base64url-encoded JSON claims        |
| `HeaderCallerService`   | `"x-caller-service"`   | Upstream caller service name         |
| `HeaderCallChain`       | `"x-call-chain"`       | Base64url-encoded JSON call chain    |

### Functions

#### ExtractBearerToken

```go
func ExtractBearerToken(authHeader string) string
```

Extracts the token from an `Authorization` header value. The
`"Bearer "` prefix match is case-insensitive. Returns an empty string
if the prefix is not present.

#### SerializeClaims / DeserializeClaims

```go
func SerializeClaims(claims map[string]any) (string, error)
func DeserializeClaims(encoded string) (map[string]any, error)
```

`SerializeClaims` encodes a claims map as base64url-encoded JSON.
Returns an error if the serialized output exceeds `MaxHeaderValueSize`
(8192 bytes).

`DeserializeClaims` decodes a base64url-encoded JSON string back into
a claims map. Returns an error if the input is malformed.

#### SerializeCallChain / DeserializeCallChain

```go
func SerializeCallChain(chain *CallChain) (string, error)
func DeserializeCallChain(encoded string) (*CallChain, error)
```

`SerializeCallChain` encodes a `CallChain` as base64url-encoded JSON.
Returns an error if the serialized output exceeds `MaxHeaderValueSize`
(8192 bytes).

`DeserializeCallChain` decodes a base64url-encoded JSON string back
into a `*CallChain`. Returns an error if the input is malformed.

### Example

```go
claims := map[string]any{"role": "admin", "org": "eng"}
encoded, err := auth.SerializeClaims(claims)
if err != nil {
    return err
}

decoded, err := auth.DeserializeClaims(encoded)
if err != nil {
    return err
}
fmt.Println(decoded["role"]) // "admin"
```

## Security Considerations

1. **Headers never trusted blindly** -- Each service validates tokens
   independently via its `TokenValidator`. Identity headers from
   upstream services are informational; the token is the source of
   truth.
2. **Base64url encoding for transport safety, not confidentiality** --
   Serialized claims and call chains are encoded for safe transport
   over HTTP headers and gRPC metadata. This is not encryption.
3. **Claims must not contain sensitive data** -- Passwords, API keys,
   secrets, and other sensitive values must never be placed in claims.
   Claims are serialized into headers and may be logged.
4. **Defensive copying on all identity constructors** -- Claims maps
   and permission slices are defensively copied during construction.
   Callers cannot mutate internal state after creation.
5. **MaxHeaderValueSize prevents oversized headers** -- Serialized
   claims and call chains are rejected if they exceed 8192 bytes (8 KB),
   preventing denial-of-service via header inflation.
6. **Invalid identity types logged and defaulted** -- When an
   unrecognized `IdentityType` is deserialized from a header, the
   value is logged and defaults to `IdentityTypeService` to maintain
   the principle of least privilege.
7. **Malformed call chain headers do not fail requests** -- If the
   `x-call-chain` header cannot be deserialized, the error is logged
   but the request proceeds. This ensures that a corrupted header in
   one service does not cause cascading failures.
8. **Context propagation supports deadline enforcement** -- All
   validation and propagation functions accept `context.Context`,
   ensuring that upstream deadlines and cancellation signals are
   respected.

## Example: End-to-End Identity Propagation

```go
import (
    "context"
    "net/http"

    "google.golang.org/grpc"
    "github.com/StricklySoft/stricklysoft-core/pkg/auth"
)

// 1. HTTP gateway validates the user's token
gateway := http.NewServeMux()
gateway.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
    // Identity is in the context after middleware validation
    id := auth.MustIdentityFromContext(r.Context())
    fmt.Printf("Authenticated: %s (%s)\n", id.ID(), id.Type())

    // 2. Call downstream gRPC service -- identity propagates automatically
    resp, err := agentClient.ListAgents(r.Context(), &pb.ListAgentsRequest{})
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    // ...
})

handler := auth.HTTPMiddleware(validator, "api-gateway")(gateway)

// 3. gRPC client propagates identity to downstream services
conn, _ := grpc.Dial("agent-manager:443",
    grpc.WithUnaryInterceptor(auth.UnaryClientInterceptor("api-gateway")),
)

// 4. Downstream gRPC server validates and extracts identity
server := grpc.NewServer(
    grpc.UnaryInterceptor(auth.UnaryServerInterceptor(validator, "agent-manager")),
)
```

## File Structure

```
pkg/auth/
    identity.go        Identity interface, IdentityType, BasicIdentity, ServiceIdentity,
                       UserIdentity, Permission, CallerInfo, CallChain
    context.go         Context functions (ContextWithIdentity, IdentityFromContext,
                       MustIdentityFromContext, CallerService, CallChain, TraceID, SpanID)
    grpc.go            gRPC server and client interceptors (unary and stream)
    http.go            HTTP middleware and PropagatingRoundTripper
    propagation.go     Header constants, ExtractBearerToken, serialization/deserialization
    jwt.go             Placeholder for JWT token validation
    k8s.go             Placeholder for Kubernetes ServiceAccount integration
    rbac.go            Placeholder for RBAC authorization system
```
