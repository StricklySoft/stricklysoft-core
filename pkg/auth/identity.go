// Package auth provides authentication, authorization, and identity propagation
// primitives for services running on the StricklySoft Cloud Platform.
//
// Identity Propagation:
//
// When a request flows through multiple services (agents), the original caller's
// identity must be preserved for authorization and audit purposes. This package
// provides gRPC interceptors and HTTP middleware that transparently propagate
// identity context across service boundaries.
//
// The identity model supports four types:
//   - User: A human user authenticated via JWT or other credential
//   - Service: A platform service authenticating via service account
//   - Agent: An AI agent operating on behalf of a user or system
//   - System: An internal system process (background jobs, cron, migrations)
//
// Each request carries both the original identity (who initiated the request)
// and a call chain (which services have handled it), enabling full audit trails.
//
// Security:
//
// Forwarded identity headers are never trusted blindly. Every service in the
// chain must validate the token independently using a [TokenValidator]. Claims
// are serialized as base64url-encoded JSON for safe transport in headers and
// gRPC metadata.
package auth

import (
	"context"
	"errors"
)

// IdentityType represents the type of authenticated identity.
// The platform distinguishes between human users, platform services,
// AI agents, and system processes, as each has different authorization
// semantics.
type IdentityType string

const (
	// IdentityTypeUser represents a human user authenticated via credentials.
	// User identities originate from external authentication providers (OAuth2,
	// OIDC, SAML) and carry user-specific claims such as email and roles.
	IdentityTypeUser IdentityType = "user"

	// IdentityTypeService represents a platform service authenticated via
	// service account credentials (e.g., Kubernetes ServiceAccount tokens,
	// mTLS certificates). Service identities are used for service-to-service
	// communication where no human user is involved.
	IdentityTypeService IdentityType = "service"

	// IdentityTypeAgent represents an AI agent operating within the platform.
	// Agent identities track which agent is performing an action, enabling
	// fine-grained authorization and audit logging for autonomous operations.
	IdentityTypeAgent IdentityType = "agent"

	// IdentityTypeSystem represents an internal system process such as a
	// background job, scheduled task, or migration. System identities are
	// not tied to a human user or external service and typically have
	// elevated privileges scoped to their specific function.
	IdentityTypeSystem IdentityType = "system"
)

// String returns the string representation of the identity type.
func (t IdentityType) String() string {
	return string(t)
}

// Valid reports whether the identity type is one of the recognized values.
func (t IdentityType) Valid() bool {
	switch t {
	case IdentityTypeUser, IdentityTypeService, IdentityTypeAgent, IdentityTypeSystem:
		return true
	default:
		return false
	}
}

// Identity represents an authenticated entity in the StricklySoft platform.
// Every request processed by the platform is associated with an Identity that
// identifies who (or what) is making the request.
//
// Implementations must be safe for concurrent use by multiple goroutines.
type Identity interface {
	// ID returns the unique identifier of the identity.
	// For users, this is typically a UUID from the identity provider.
	// For services, this is the service account name.
	// For agents, this is the agent instance identifier.
	ID() string

	// Type returns the category of identity (user, service, agent, or system).
	Type() IdentityType

	// Claims returns the identity's claims as a map. Claims include
	// attributes like email, roles, scopes, and other metadata from
	// the authentication token. Implementations should return a copy
	// of the underlying claims to ensure immutability.
	Claims() map[string]any

	// HasPermission checks whether this identity is authorized to perform
	// the given action on the specified resource. The resource and action
	// strings follow the format defined by the authorization policy
	// (e.g., resource="documents", action="read").
	//
	// Implementations may check against an in-memory permission list,
	// consult an external policy engine, or delegate to an RBAC system.
	HasPermission(resource, action string) bool
}

// TokenValidator validates authentication tokens and extracts the identity
// they represent. Implementations are responsible for verifying token
// signatures, expiration, audience, and any other security requirements.
//
// This interface is used by gRPC interceptors and HTTP middleware to
// authenticate incoming requests. Consumers provide their own implementation
// that matches their authentication infrastructure (JWT, Kubernetes SA, etc.).
//
// Implementations must be safe for concurrent use by multiple goroutines.
type TokenValidator interface {
	// Validate verifies the given token string and returns the Identity
	// it represents. Returns an error if the token is invalid, expired,
	// or cannot be verified.
	//
	// The context may carry deadlines, cancellation signals, and tracing
	// information that validators should respect.
	Validate(ctx context.Context, token string) (Identity, error)
}

// BasicIdentity is a simple, immutable implementation of the Identity interface.
// It is used for carrying identity information across service boundaries after
// deserialization from gRPC metadata or HTTP headers.
//
// For token-based authentication, the TokenValidator implementation should
// return its own Identity type with richer functionality. BasicIdentity is
// primarily used for reconstructing identity from propagated headers.
type BasicIdentity struct {
	id     string
	idType IdentityType
	claims map[string]any
}

// NewBasicIdentity creates a new BasicIdentity with the given parameters.
// The claims map is copied to prevent external mutation.
func NewBasicIdentity(id string, idType IdentityType, claims map[string]any) *BasicIdentity {
	// Deep-copy claims to ensure immutability.
	copied := make(map[string]any, len(claims))
	for k, v := range claims {
		copied[k] = v
	}
	return &BasicIdentity{
		id:     id,
		idType: idType,
		claims: copied,
	}
}

// ID returns the unique identifier of the identity.
func (b *BasicIdentity) ID() string {
	return b.id
}

// Type returns the identity type (user, service, agent, or system).
func (b *BasicIdentity) Type() IdentityType {
	return b.idType
}

// Claims returns a shallow copy of the identity's claims. Each call returns
// a new map, so callers may safely modify the result without affecting the
// identity or other callers.
func (b *BasicIdentity) Claims() map[string]any {
	copied := make(map[string]any, len(b.claims))
	for k, v := range b.claims {
		copied[k] = v
	}
	return copied
}

// HasPermission always returns false for BasicIdentity. BasicIdentity is a
// transport-level type used for deserializing identity from propagated
// headers; it does not carry permission information. Use [ServiceIdentity]
// or [UserIdentity] for authorization decisions.
func (b *BasicIdentity) HasPermission(resource, action string) bool {
	return false
}

// Permission represents an authorization grant for a specific resource and
// action. Permissions are attached to identities and checked via
// [Identity.HasPermission] to make authorization decisions.
//
// Example permissions:
//
//	Permission{Resource: "documents", Action: "read"}
//	Permission{Resource: "users", Action: "delete"}
//	Permission{Resource: "*", Action: "*"}  // wildcard — full access
type Permission struct {
	// Resource is the resource being accessed (e.g., "documents", "users",
	// "agents"). The wildcard "*" matches all resources.
	Resource string

	// Action is the operation being performed (e.g., "read", "write",
	// "delete", "execute"). The wildcard "*" matches all actions.
	Action string
}

// ServiceIdentity represents a platform service or agent authenticated via
// service account credentials. It carries service-specific metadata
// (service name, Kubernetes namespace) and an explicit list of permissions
// that define what the service is authorized to do.
//
// ServiceIdentity is immutable after creation.
type ServiceIdentity struct {
	id          string
	serviceName string
	namespace   string
	claims      map[string]any
	permissions []Permission
}

// NewServiceIdentity creates a new ServiceIdentity for a platform service.
// The serviceName identifies the service (e.g., "nexus-gateway"), and
// namespace is the Kubernetes namespace or deployment environment.
// Claims and permissions are defensively copied to ensure immutability.
//
// Returns an error if id or serviceName is empty, as these are required
// for authorization and audit purposes.
func NewServiceIdentity(id, serviceName, namespace string, claims map[string]any, permissions []Permission) (*ServiceIdentity, error) {
	if id == "" {
		return nil, errors.New("auth: service identity id must not be empty")
	}
	if serviceName == "" {
		return nil, errors.New("auth: service identity serviceName must not be empty")
	}
	copiedClaims := make(map[string]any, len(claims))
	for k, v := range claims {
		copiedClaims[k] = v
	}
	copiedPerms := make([]Permission, len(permissions))
	copy(copiedPerms, permissions)
	return &ServiceIdentity{
		id:          id,
		serviceName: serviceName,
		namespace:   namespace,
		claims:      copiedClaims,
		permissions: copiedPerms,
	}, nil
}

// ID returns the unique identifier of the service identity.
func (s *ServiceIdentity) ID() string { return s.id }

// Type returns IdentityTypeService.
func (s *ServiceIdentity) Type() IdentityType { return IdentityTypeService }

// Claims returns a shallow copy of the service identity's claims.
func (s *ServiceIdentity) Claims() map[string]any {
	copied := make(map[string]any, len(s.claims))
	for k, v := range s.claims {
		copied[k] = v
	}
	return copied
}

// HasPermission checks whether this service identity has been granted the
// specified permission. Supports wildcard matching: a permission with
// Resource="*" matches any resource, and Action="*" matches any action.
func (s *ServiceIdentity) HasPermission(resource, action string) bool {
	return hasPermission(s.permissions, resource, action)
}

// ServiceName returns the name of the service (e.g., "nexus-gateway").
func (s *ServiceIdentity) ServiceName() string { return s.serviceName }

// Namespace returns the Kubernetes namespace or deployment environment
// of the service.
func (s *ServiceIdentity) Namespace() string { return s.namespace }

// Permissions returns a copy of the service identity's permission list.
// The returned slice is a defensive copy; callers may safely modify it
// without affecting the identity. This is useful for audit logging and
// policy introspection.
func (s *ServiceIdentity) Permissions() []Permission {
	copied := make([]Permission, len(s.permissions))
	copy(copied, s.permissions)
	return copied
}

// UserIdentity represents a human user authenticated via external
// credentials (OAuth2, OIDC, SAML, JWT). It carries user-specific
// metadata (email, display name) and permissions derived from the
// user's roles.
//
// UserIdentity is immutable after creation.
type UserIdentity struct {
	id          string
	email       string
	displayName string
	claims      map[string]any
	permissions []Permission
}

// NewUserIdentity creates a new UserIdentity for a human user.
// Claims and permissions are defensively copied to ensure immutability.
//
// Returns an error if id or email is empty, as these are required
// for authorization and audit purposes.
func NewUserIdentity(id, email, displayName string, claims map[string]any, permissions []Permission) (*UserIdentity, error) {
	if id == "" {
		return nil, errors.New("auth: user identity id must not be empty")
	}
	if email == "" {
		return nil, errors.New("auth: user identity email must not be empty")
	}
	copiedClaims := make(map[string]any, len(claims))
	for k, v := range claims {
		copiedClaims[k] = v
	}
	copiedPerms := make([]Permission, len(permissions))
	copy(copiedPerms, permissions)
	return &UserIdentity{
		id:          id,
		email:       email,
		displayName: displayName,
		claims:      copiedClaims,
		permissions: copiedPerms,
	}, nil
}

// ID returns the unique identifier of the user (typically a UUID from the
// identity provider).
func (u *UserIdentity) ID() string { return u.id }

// Type returns IdentityTypeUser.
func (u *UserIdentity) Type() IdentityType { return IdentityTypeUser }

// Claims returns a shallow copy of the user identity's claims.
func (u *UserIdentity) Claims() map[string]any {
	copied := make(map[string]any, len(u.claims))
	for k, v := range u.claims {
		copied[k] = v
	}
	return copied
}

// HasPermission checks whether this user identity has been granted the
// specified permission. Supports wildcard matching: a permission with
// Resource="*" matches any resource, and Action="*" matches any action.
func (u *UserIdentity) HasPermission(resource, action string) bool {
	return hasPermission(u.permissions, resource, action)
}

// Email returns the user's email address.
func (u *UserIdentity) Email() string { return u.email }

// DisplayName returns the user's display name.
func (u *UserIdentity) DisplayName() string { return u.displayName }

// Permissions returns a copy of the user identity's permission list.
// The returned slice is a defensive copy; callers may safely modify it
// without affecting the identity. This is useful for audit logging and
// policy introspection.
func (u *UserIdentity) Permissions() []Permission {
	copied := make([]Permission, len(u.permissions))
	copy(copied, u.permissions)
	return copied
}

// hasPermission is a shared helper that checks whether a permission list
// grants access to the given resource and action. Supports wildcard "*"
// for both resource and action fields.
func hasPermission(permissions []Permission, resource, action string) bool {
	for _, p := range permissions {
		resourceMatch := p.Resource == "*" || p.Resource == resource
		actionMatch := p.Action == "*" || p.Action == action
		if resourceMatch && actionMatch {
			return true
		}
	}
	return false
}

// CallerInfo records the identity of a service in the call chain.
// When service A calls service B on behalf of a user, CallerInfo captures
// service A's identity so the full request path can be reconstructed for
// audit and debugging purposes.
type CallerInfo struct {
	// ServiceName is the name of the calling service (e.g., "nexus-gateway",
	// "agent-orchestrator").
	ServiceName string `json:"service_name"`

	// IdentityID is the authenticated identity ID of the calling service.
	// This is the service's own identity, not the original requester's.
	IdentityID string `json:"identity_id"`

	// IdentityType is the type of the calling service's identity
	// (typically IdentityTypeService).
	IdentityType IdentityType `json:"identity_type"`
}

// CallChain tracks the full chain of services that have handled a request.
// This enables audit logging, debugging, and understanding the complete
// request path through the distributed system.
//
// Example chain: User -> API Gateway -> Agent Orchestrator -> Agent
//
//	CallChain{
//	    OriginalID:   "user-uuid-123",
//	    OriginalType: IdentityTypeUser,
//	    Callers: []CallerInfo{
//	        {ServiceName: "api-gateway", IdentityID: "svc-gw", IdentityType: IdentityTypeService},
//	        {ServiceName: "agent-orchestrator", IdentityID: "svc-orch", IdentityType: IdentityTypeService},
//	    },
//	}
type CallChain struct {
	// OriginalID is the identity ID of the request originator (typically a user).
	OriginalID string `json:"original_id"`

	// OriginalType is the identity type of the request originator.
	OriginalType IdentityType `json:"original_type"`

	// Callers is an ordered list of services that forwarded the request.
	// The first entry is the first service that received the request from
	// the originator, and the last entry is the service that made the
	// current call.
	Callers []CallerInfo `json:"callers"`
}

// MaxCallChainDepth is the maximum number of callers tracked in a CallChain.
// When a chain exceeds this depth, the oldest intermediate callers are
// truncated to prevent unbounded growth that could exceed HTTP header size
// limits or cause excessive memory usage.
//
// The value 32 supports realistic deep call chains while keeping the
// serialized header well within HTTP/2's default SETTINGS_MAX_HEADER_LIST_SIZE
// (16 KB) and HTTP/1.1's practical limits (~8 KB).
const MaxCallChainDepth = 32

// Depth returns the number of services in the call chain, including the
// current service. A direct call (no intermediaries) has depth 0.
func (c *CallChain) Depth() int {
	return len(c.Callers)
}

// AppendCaller adds a new caller to the end of the call chain and returns
// the updated chain. The original CallChain is not modified.
//
// If appending the caller would exceed [MaxCallChainDepth], the oldest
// intermediate callers are dropped to make room while preserving the most
// recent callers (which are most useful for debugging).
func (c *CallChain) AppendCaller(caller CallerInfo) *CallChain {
	callers := make([]CallerInfo, len(c.Callers), len(c.Callers)+1)
	copy(callers, c.Callers)
	callers = append(callers, caller)

	// Truncate oldest callers if the chain exceeds the maximum depth.
	if len(callers) > MaxCallChainDepth {
		callers = callers[len(callers)-MaxCallChainDepth:]
	}

	return &CallChain{
		OriginalID:   c.OriginalID,
		OriginalType: c.OriginalType,
		Callers:      callers,
	}
}
