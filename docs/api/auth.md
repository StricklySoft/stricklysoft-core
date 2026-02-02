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

The package ships a production-ready `JWTValidator` that implements the
`TokenValidator` interface, supporting three token types: **Kubernetes
ServiceAccount** (RS256/ES256 via JWKS), **platform HMAC** (HS256), and
**external OIDC** (RS256/ES256 via JWKS). An RBAC subsystem maps JWT
claims (roles, scopes, direct permissions) to the `Permission` model
used by `ServiceIdentity` and `UserIdentity`.

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

The package ships `JWTValidator` as the production implementation.
See [JWT Validation](#jwt-validation) below for details.

```go
cfg := auth.DefaultValidatorConfig()
cfg.EnablePlatform = true
cfg.PlatformSigningKey = auth.Secret(os.Getenv("AUTH_PLATFORM_SIGNING_KEY"))

validator, err := auth.NewJWTValidator(cfg)
if err != nil {
    log.Fatal(err)
}

// Use with HTTP middleware
handler := auth.HTTPMiddleware(validator, "my-service")(mux)

// Use with gRPC interceptors
server := grpc.NewServer(
    grpc.UnaryInterceptor(auth.UnaryServerInterceptor(validator, "my-service")),
)
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

## JWT Validation

The `JWTValidator` is the production implementation of `TokenValidator`.
It validates three types of JWT tokens, each using a dedicated
verification path, and caches validated identities for performance.

### Secret Type

The `Secret` type wraps a `string` and overrides `String()`,
`GoString()`, and `MarshalText()` to return `"[REDACTED]"`. This
prevents signing keys and other sensitive configuration from leaking
into logs, JSON, or `fmt.Printf` output. The actual value is only
accessible via `Value()`.

```go
key := auth.Secret("my-signing-key-at-least-32-bytes!")
fmt.Println(key)           // [REDACTED]
fmt.Printf("%#v\n", key)  // [REDACTED]

// Only when the raw value is truly needed:
hmacKey := []byte(key.Value())
```

### Token Types

| Constant               | Value          | Algorithm      | Key Source                         |
|------------------------|----------------|----------------|------------------------------------|
| `TokenTypeKubernetes`  | `"kubernetes"` | RS256 / ES256  | Kubernetes OIDC discovery -> JWKS  |
| `TokenTypePlatform`    | `"platform"`   | HS256          | Shared HMAC key (`PlatformSigningKey`) |
| `TokenTypeOIDC`        | `"oidc"`       | RS256 / ES256  | OIDC discovery -> JWKS             |

### ValidatorConfig

`ValidatorConfig` holds all settings for `JWTValidator`. Fields with
`env:` tags can be populated from environment variables using the
`pkg/config` loader.

| Field                | Env Var                       | Default                                        | Description                                  |
|----------------------|-------------------------------|-------------------------------------------------|----------------------------------------------|
| `EnableKubernetes`   | `AUTH_ENABLE_KUBERNETES`      | `true`                                          | Accept K8s ServiceAccount tokens             |
| `EnablePlatform`     | `AUTH_ENABLE_PLATFORM`        | `false`                                         | Accept platform HMAC tokens                  |
| `EnableOIDC`         | `AUTH_ENABLE_OIDC`            | `false`                                         | Accept external OIDC tokens                  |
| `PlatformSigningKey` | `AUTH_PLATFORM_SIGNING_KEY`   | --                                              | HMAC key (Secret, >=32 bytes)                |
| `PlatformIssuer`     | `AUTH_PLATFORM_ISSUER`        | `stricklysoft-platform`                         | Expected `iss` for platform tokens           |
| `PlatformAudience`   | `AUTH_PLATFORM_AUDIENCE`      | --                                              | Expected `aud` for platform tokens (optional)|
| `OIDCIssuerURL`      | `AUTH_OIDC_ISSUER_URL`        | --                                              | OIDC issuer URL for .well-known discovery    |
| `OIDCAudience`       | `AUTH_OIDC_AUDIENCE`          | --                                              | Expected `aud` for OIDC tokens (optional)    |
| `KubernetesIssuer`   | `AUTH_KUBERNETES_ISSUER`      | `https://kubernetes.default.svc.cluster.local`  | Expected `iss` for K8s tokens                |
| `KubernetesAudience` | `AUTH_KUBERNETES_AUDIENCE`    | `https://kubernetes.default.svc.cluster.local`  | Expected `aud` for K8s tokens                |
| `TokenCacheTTL`      | `AUTH_TOKEN_CACHE_TTL`        | `5m`                                            | Max cache lifetime for validated tokens      |
| `TokenCacheMaxSize`  | `AUTH_TOKEN_CACHE_MAX_SIZE`   | `10000`                                         | Max entries in the token cache               |
| `JWKSCacheTTL`       | `AUTH_JWKS_CACHE_TTL`         | `1h`                                            | JWKS key set cache duration                  |
| `ClockSkew`          | `AUTH_CLOCK_SKEW`             | `30s`                                           | Tolerance for exp/nbf clock differences      |
| `PermissionMapper`   | --                            | `DefaultClaimsToPermissions`                    | Claims -> Permission mapping function        |
| `HTTPClient`         | --                            | `&http.Client{Timeout: 10s}`                    | HTTP client for JWKS/discovery fetches       |
| `SATokenPath`        | `AUTH_K8S_SA_TOKEN_PATH`      | `/var/run/secrets/.../token`                    | K8s SA token file path                       |

#### Validation Rules

`ValidatorConfig.Validate()` enforces:

- At least one token type must be enabled.
- If `EnablePlatform`: `PlatformSigningKey` >= 32 bytes.
- If `EnableOIDC`: `OIDCIssuerURL` must not be empty.
- `TokenCacheTTL`, `JWKSCacheTTL`, `ClockSkew` must be >= 0.
- `TokenCacheMaxSize` must be > 0.

### Construction

```go
func NewJWTValidator(cfg ValidatorConfig) (*JWTValidator, error)
func DefaultValidatorConfig() ValidatorConfig
```

`NewJWTValidator` validates the config and initializes the token cache,
JWKS cache, and OTel tracer. Returns an error if the configuration is
invalid.

`DefaultValidatorConfig` returns a config with only Kubernetes token
validation enabled -- suitable for services running in Kubernetes that
only need to authenticate ServiceAccount tokens.

### Validation Flow

```
Validate(ctx, token)
  |-- Check token cache (SHA-256 key) -> hit -> return cached Identity
  |-- Parse unverified header + payload -> extract iss, alg
  |-- Reject alg: none unconditionally
  |-- Detect token type by issuer match -> fallback by algorithm
  |-- Route to validation path:
  |   |-- validatePlatformToken()     -> HS256, verify iss/aud
  |   |-- validateOIDCToken()         -> RS256/ES256 via JWKS
  |   +-- validateKubernetesToken()   -> RS256/ES256 via K8s JWKS
  |-- Extract claims -> map[string]any
  |-- Map claims to permissions via PermissionMapper
  |-- Build Identity (UserIdentity or ServiceIdentity)
  |-- Cache result (TTL = min(config TTL, token exp - now))
  +-- Return Identity
```

### Token Type Detection

The validator determines the token type using the following rules:

1. Parse JWT header (without signature verification) to extract `alg`.
2. Parse unverified payload to extract `iss`.
3. Match `iss` against configured issuers:
   - `iss == KubernetesIssuer` -> Kubernetes path
   - `iss == PlatformIssuer` -> Platform path
   - `iss == OIDCIssuerURL` -> OIDC path
4. Fallback if `iss` does not match: HMAC algorithm -> Platform;
   asymmetric algorithm with OIDC enabled -> OIDC.
5. `alg: none` is rejected unconditionally, regardless of enabled types.

### Platform HMAC Validation

Platform tokens use HS256 signing with a shared secret key.

- Parsed with `jwt.WithValidMethods([]string{"HS256"})` to prevent
  algorithm confusion attacks.
- Key function returns `[]byte(config.PlatformSigningKey.Value())`.
- Validates `iss` matches `PlatformIssuer`, `aud` matches
  `PlatformAudience` (if configured).
- If claims contain `email` -> `UserIdentity`; if `service_name` ->
  `ServiceIdentity`; otherwise -> `BasicIdentity`.

### OIDC Validation

OIDC tokens use RS256 or ES256 signing with keys fetched via JWKS.

- Fetches `.well-known/openid-configuration` from `OIDCIssuerURL`.
- Extracts `jwks_uri` from the discovery document.
- Fetches and caches the JWKS key set.
- Parsed with `jwt.WithValidMethods([]string{"RS256", "ES256"})`.
- Key function looks up `kid` in the cached JWKS.
- Validates `iss` matches `OIDCIssuerURL`, `aud` matches `OIDCAudience`
  (if configured).
- Builds `UserIdentity` from standard OIDC claims (`sub`, `email`,
  `name`).

### Kubernetes Token Validation

Kubernetes ServiceAccount tokens use the same JWKS approach as OIDC,
fetching keys from the Kubernetes API server's OIDC discovery endpoint.

- Fetches `.well-known/openid-configuration` from `KubernetesIssuer`.
- Validates signature against the cluster's JWKS keys.
- Extracts Kubernetes-specific claims in three formats:
  1. **Nested**: `claims["kubernetes.io"]["namespace"]` and
     `claims["kubernetes.io"]["serviceaccount"]["name"]`
  2. **Flat**: `claims["kubernetes.io/serviceaccount/namespace"]`
  3. **Subject parsing**: `system:serviceaccount:<namespace>:<name>`
- Returns a `ServiceIdentity` with namespace and service account name.

### Token Cache

The token cache stores validated identities keyed by the SHA-256 hash
of the raw token string (never the token itself). This avoids
re-parsing and signature verification on repeated requests.

- **Lookup**: O(1) under `sync.RWMutex` read lock.
- **TTL**: `min(TokenCacheTTL, token expiry - now)` ensures tokens are
  not cached beyond their actual validity.
- **Eviction**: When at capacity, expired entries are evicted first.
- **Thread-safe**: All operations protected by `sync.RWMutex`.
- **Sub-millisecond**: Cache hits avoid all cryptographic operations.

### JWKS Cache

The JWKS cache stores fetched public keys keyed by JWKS URL.

- Supports both RSA and ECDSA (P-256, P-384) key types.
- Cache TTL is controlled by `JWKSCacheTTL` (default 1 hour).
- HTTPS-only enforcement for JWKS URLs.
- Response body limited to 1 MB to prevent resource exhaustion.
- Thread-safe via `sync.RWMutex`.

### OpenTelemetry Tracing

The validator creates spans for all validation paths:

| Span Name                       | Attributes                                          |
|---------------------------------|-----------------------------------------------------|
| `auth.Validate`                 | `auth.token_type`, `auth.identity_id`, `auth.cache_hit` |
| `auth.ValidatePlatformToken`    | Error status on failure                             |
| `auth.ValidateOIDCToken`        | Error status on failure                             |
| `auth.ValidateKubernetesToken`  | Error status on failure                             |
| `auth.FetchJWKS`                | Created on JWKS cache miss                          |

### Error Mapping

Validation errors are mapped to structured error codes from
`pkg/errors`:

| Condition                    | Error Code                     |
|------------------------------|--------------------------------|
| Empty or malformed token     | `AUTH_003` (AuthenticationInvalid) |
| Invalid signature            | `AUTH_003` (AuthenticationInvalid) |
| Expired token                | `AUTH_002` (AuthenticationExpired) |
| Issuer mismatch              | `AUTH_003` (AuthenticationInvalid) |
| Audience mismatch            | `AUTH_003` (AuthenticationInvalid) |
| Algorithm `none`             | `AUTH_003` (AuthenticationInvalid) |
| No validator matches token   | `AUTH_001` (Authentication)        |
| JWKS fetch failure           | `AUTH_001` (Authentication)        |
| Invalid configuration        | `VAL_001` (Validation)             |

### Example: Platform Token Validation

```go
cfg := auth.ValidatorConfig{
    EnableKubernetes:   false,
    EnablePlatform:     true,
    EnableOIDC:         false,
    PlatformSigningKey: auth.Secret(os.Getenv("AUTH_PLATFORM_SIGNING_KEY")),
    PlatformIssuer:     "stricklysoft-platform",
    TokenCacheTTL:      5 * time.Minute,
    TokenCacheMaxSize:  10000,
    JWKSCacheTTL:       1 * time.Hour,
    ClockSkew:          30 * time.Second,
}

validator, err := auth.NewJWTValidator(cfg)
if err != nil {
    log.Fatal(err)
}

// Use in HTTP middleware
handler := auth.HTTPMiddleware(validator, "api-gateway")(mux)
```

### Example: Multi-Provider Configuration

```go
cfg := auth.ValidatorConfig{
    EnableKubernetes:   true,
    EnablePlatform:     true,
    EnableOIDC:         true,
    PlatformSigningKey: auth.Secret(os.Getenv("AUTH_PLATFORM_SIGNING_KEY")),
    PlatformIssuer:     "stricklysoft-platform",
    OIDCIssuerURL:      "https://accounts.google.com",
    OIDCAudience:       "my-client-id.apps.googleusercontent.com",
    KubernetesIssuer:   "https://kubernetes.default.svc.cluster.local",
    KubernetesAudience: "https://kubernetes.default.svc.cluster.local",
    TokenCacheTTL:      5 * time.Minute,
    TokenCacheMaxSize:  10000,
    JWKSCacheTTL:       1 * time.Hour,
    ClockSkew:          30 * time.Second,
}

validator, err := auth.NewJWTValidator(cfg)
if err != nil {
    log.Fatal(err)
}
```

## Kubernetes ServiceAccount Integration

### Constants

| Constant                 | Value                                                          |
|--------------------------|----------------------------------------------------------------|
| `DefaultSATokenPath`    | `/var/run/secrets/kubernetes.io/serviceaccount/token`          |
| `DefaultSACACertPath`   | `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`         |
| `DefaultK8sNamespacePath` | `/var/run/secrets/kubernetes.io/serviceaccount/namespace`    |

### ReadServiceAccountToken

```go
func ReadServiceAccountToken(path string) (string, error)
```

Reads a Kubernetes ServiceAccount token from the given filesystem path.
If `path` is empty, `DefaultSATokenPath` is used. The token is trimmed
of whitespace. Returns an error if the file does not exist, cannot be
read, or is empty.

### ValidateServiceAccount

```go
func (v *JWTValidator) ValidateServiceAccount(ctx context.Context) (Identity, error)
```

Reads the pod's own ServiceAccount token from the configured path (or
`DefaultSATokenPath`) and validates it using the standard `Validate`
method. Returns a `*ServiceIdentity` with the Kubernetes namespace and
service account name extracted from the token claims.

```go
// In a Kubernetes pod, authenticate using the pod's own identity:
identity, err := validator.ValidateServiceAccount(ctx)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Running as %s in namespace %s\n",
    identity.(*auth.ServiceIdentity).ServiceName(),
    identity.(*auth.ServiceIdentity).Namespace(),
)
```

## RBAC Permission Mapping

The RBAC subsystem maps JWT claims to `Permission` values used by
`ServiceIdentity` and `UserIdentity` for authorization checks.

### RolePermissionMap

`RolePermissionMap` is a `map[string][]Permission` that defines which
permissions are granted to each role.

### DefaultRolePermissions

```go
func DefaultRolePermissions() RolePermissionMap
```

Returns the platform's default role mapping:

| Role        | Permissions                                                     |
|-------------|------------------------------------------------------------------|
| `admin`     | `*:*` (full access)                                             |
| `operator`  | `agents:*`, `deployments:*`, `logs:read`                        |
| `developer` | `agents:read`, `agents:execute`, `logs:read`, `deployments:read`|
| `viewer`    | `*:read` (read-only access to all resources)                    |

### ClaimsToPermissions

```go
func ClaimsToPermissions(claims map[string]any, roleMap RolePermissionMap) []Permission
```

Extracts permissions from JWT claims by inspecting three fields:

1. **`"permissions"`** -- Direct grants as `[]string` in
   `"resource:action"` format.
2. **`"roles"`** -- Role names as `[]string`, resolved via the provided
   `RolePermissionMap`.
3. **`"scope"`** -- OAuth2 scopes as a space-separated string in
   `"resource:action"` format.

All sources are merged and deduplicated. Malformed entries are silently
skipped.

### DefaultClaimsToPermissions

```go
func DefaultClaimsToPermissions(claims map[string]any) []Permission
```

Convenience function equivalent to
`ClaimsToPermissions(claims, DefaultRolePermissions())`.

### ParsePermissionString

```go
func ParsePermissionString(s string) (Permission, error)
```

Parses `"resource:action"` into a `Permission`. Returns an error if the
colon separator is missing or either part is empty.

### ParseScopePermissions

```go
func ParseScopePermissions(scope string) []Permission
```

Splits a space-separated OAuth2 scope string and parses each token as a
`"resource:action"` permission. Malformed tokens are skipped.

```go
perms := auth.ParseScopePermissions("agents:read logs:read deployments:create")
// []Permission{
//     {Resource: "agents", Action: "read"},
//     {Resource: "logs", Action: "read"},
//     {Resource: "deployments", Action: "create"},
// }
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
2. **Algorithm confusion prevention** -- Each validation path uses
   `jwt.WithValidMethods()` to explicitly restrict accepted algorithms.
   Platform tokens only accept HS256; OIDC and Kubernetes only accept
   RS256/ES256. This prevents an attacker from switching an RS256 token
   to HS256 and signing with the public key.
3. **`alg: none` rejected unconditionally** -- Tokens with algorithm
   `"none"` or `"None"` are rejected before any further processing,
   regardless of configuration.
4. **Secret type prevents credential logging** -- The `Secret` type
   overrides `String()`, `GoString()`, and `MarshalText()` to return
   `"[REDACTED]"`. Signing keys cannot accidentally appear in logs,
   JSON, or `fmt.Printf` output.
5. **HMAC key minimum 32 bytes** -- The validator requires platform
   signing keys to be at least 32 bytes (256 bits), ensuring adequate
   entropy for HS256.
6. **Token cache uses SHA-256 hashes** -- The token cache keys entries
   by the SHA-256 hash of the raw token, not the token itself. Raw
   tokens are never stored in the cache.
7. **Cache TTL respects token expiry** -- Cached tokens expire at
   `min(config TTL, token exp - now)`, ensuring tokens are never served
   from cache after their actual expiration.
8. **JWKS fetched over HTTPS only** -- JWKS URLs are validated to use
   HTTPS, preventing man-in-the-middle attacks on key distribution.
9. **JWKS response size limited** -- JWKS responses are capped at 1 MB
   to prevent memory exhaustion from malicious endpoints.
10. **Base64url encoding for transport safety, not confidentiality** --
    Serialized claims and call chains are encoded for safe transport
    over HTTP headers and gRPC metadata. This is not encryption.
11. **Claims must not contain sensitive data** -- Passwords, API keys,
    secrets, and other sensitive values must never be placed in claims.
    Claims are serialized into headers and may be logged.
12. **Defensive copying on all identity constructors** -- Claims maps
    and permission slices are defensively copied during construction.
    Callers cannot mutate internal state after creation.
13. **MaxHeaderValueSize prevents oversized headers** -- Serialized
    claims and call chains are rejected if they exceed 8192 bytes (8 KB),
    preventing denial-of-service via header inflation.
14. **Token size limit** -- Raw JWT tokens exceeding 8192 bytes are
    rejected before parsing, preventing resource exhaustion from
    oversized tokens.
15. **Thread-safe caches** -- Both the token cache and JWKS cache are
    protected by `sync.RWMutex`, safe for concurrent use.
16. **Malformed call chain headers do not fail requests** -- If the
    `x-call-chain` header cannot be deserialized, the error is logged
    but the request proceeds. This ensures that a corrupted header in
    one service does not cause cascading failures.
17. **Context propagation supports deadline enforcement** -- All
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
    jwt.go             JWTValidator, ValidatorConfig, Secret type, token/JWKS caches,
                       platform HMAC validation, OIDC validation, OTel tracing
    k8s.go             Kubernetes ServiceAccount validation, ReadServiceAccountToken,
                       ValidateServiceAccount, K8s-specific claim parsing
    rbac.go            RBAC permission mapping, RolePermissionMap, ClaimsToPermissions,
                       DefaultRolePermissions, ParsePermissionString, ParseScopePermissions
```
