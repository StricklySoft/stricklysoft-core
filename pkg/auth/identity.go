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
// The identity model supports three types:
//   - User: A human user authenticated via JWT or other credential
//   - Service: A platform service authenticating via service account
//   - Agent: An AI agent operating on behalf of a user or system
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

import "context"

// IdentityType represents the type of authenticated identity.
// The platform distinguishes between human users, platform services,
// and AI agents, as each has different authorization semantics.
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
)

// String returns the string representation of the identity type.
func (t IdentityType) String() string {
	return string(t)
}

// Valid reports whether the identity type is one of the recognized values.
func (t IdentityType) Valid() bool {
	switch t {
	case IdentityTypeUser, IdentityTypeService, IdentityTypeAgent:
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

	// Type returns the category of identity (user, service, or agent).
	Type() IdentityType

	// Claims returns the identity's claims as a map. Claims include
	// attributes like email, roles, scopes, and other metadata from
	// the authentication token. The returned map must not be modified
	// by the caller.
	Claims() map[string]any
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
	id         string
	idType     IdentityType
	claims     map[string]any
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

// Type returns the identity type (user, service, or agent).
func (b *BasicIdentity) Type() IdentityType {
	return b.idType
}

// Claims returns the identity's claims. The returned map must not be
// modified by the caller.
func (b *BasicIdentity) Claims() map[string]any {
	return b.claims
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

// Depth returns the number of services in the call chain, including the
// current service. A direct call (no intermediaries) has depth 0.
func (c *CallChain) Depth() int {
	return len(c.Callers)
}

// AppendCaller adds a new caller to the end of the call chain and returns
// the updated chain. The original CallChain is not modified.
func (c *CallChain) AppendCaller(caller CallerInfo) *CallChain {
	callers := make([]CallerInfo, len(c.Callers), len(c.Callers)+1)
	copy(callers, c.Callers)
	callers = append(callers, caller)
	return &CallChain{
		OriginalID:   c.OriginalID,
		OriginalType: c.OriginalType,
		Callers:      callers,
	}
}
