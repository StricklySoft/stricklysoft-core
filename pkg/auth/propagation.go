package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// Header and metadata key constants for identity propagation.
// These keys are used in both HTTP headers and gRPC metadata to carry
// identity information across service boundaries.
//
// All custom headers use the "x-" prefix to distinguish them from standard
// HTTP headers. Values that contain structured data (claims, call chain)
// are base64url-encoded JSON to ensure safe transport.
const (
	// HeaderAuthorization is the standard authorization header carrying the
	// bearer token. This is the primary authentication credential used by
	// server interceptors and middleware to validate the caller.
	HeaderAuthorization = "authorization"

	// HeaderIdentityID carries the unique identifier of the authenticated
	// identity. This is set by server-side interceptors after token validation
	// and propagated by client-side interceptors to downstream services.
	HeaderIdentityID = "x-identity-id"

	// HeaderIdentityType carries the type of the authenticated identity
	// (user, service, or agent). See [IdentityType] for valid values.
	HeaderIdentityType = "x-identity-type"

	// HeaderIdentityClaims carries the identity's claims as a base64url-encoded
	// JSON object. Claims contain attributes like email, roles, scopes, and
	// other metadata from the authentication token.
	//
	// Security: Claims are encoded for transport safety, not for confidentiality.
	// Do not include sensitive data (passwords, secrets) in claims.
	HeaderIdentityClaims = "x-identity-claims"

	// HeaderCallerService carries the name of the service that forwarded the
	// request. This allows the receiving service to identify its immediate
	// upstream caller for audit and authorization purposes.
	HeaderCallerService = "x-caller-service"

	// HeaderCallChain carries the full call chain as a base64url-encoded JSON
	// array. This tracks every service that has handled the request, enabling
	// complete audit trails through the distributed system.
	HeaderCallChain = "x-call-chain"
)

// MaxHeaderValueSize is the maximum allowed size in bytes for a single
// serialized header value (claims or call chain). This limit prevents
// oversized headers that would be rejected by HTTP/2 (default
// SETTINGS_MAX_HEADER_LIST_SIZE is 16 KB) or HTTP/1.1 servers (commonly
// limited to 8 KB per header).
//
// The value 8192 (8 KB) is a conservative limit that works with all
// standard HTTP implementations. Individual values are checked
// independently; total header budget is left to the transport layer.
const MaxHeaderValueSize = 8192

// bearerPrefix is the standard "Bearer " prefix for authorization tokens.
const bearerPrefix = "Bearer "

// ExtractBearerToken extracts the token from an authorization header value.
// It handles the "Bearer " prefix case-insensitively.
// Returns an empty string if the header is empty or does not have a bearer prefix.
func ExtractBearerToken(authHeader string) string {
	if len(authHeader) <= len(bearerPrefix) {
		return ""
	}
	// Case-insensitive comparison for "Bearer " prefix.
	prefix := authHeader[:len(bearerPrefix)]
	if !strings.EqualFold(prefix, bearerPrefix) {
		return ""
	}
	return authHeader[len(bearerPrefix):]
}

// SerializeClaims encodes a claims map as a base64url-encoded JSON string.
// This format is safe for use in HTTP headers and gRPC metadata values.
//
// Returns an empty string if claims is nil or empty.
// Returns an error if the claims cannot be marshaled to JSON or if the
// encoded output exceeds [MaxHeaderValueSize].
func SerializeClaims(claims map[string]any) (string, error) {
	if len(claims) == 0 {
		return "", nil
	}
	data, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: failed to marshal claims: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	if len(encoded) > MaxHeaderValueSize {
		return "", fmt.Errorf("auth: serialized claims size %d exceeds maximum %d bytes", len(encoded), MaxHeaderValueSize)
	}
	return encoded, nil
}

// DeserializeClaims decodes a base64url-encoded JSON string into a claims map.
// Returns an empty map (not nil) if the encoded string is empty.
// Returns an error if the string cannot be decoded or parsed.
func DeserializeClaims(encoded string) (map[string]any, error) {
	if encoded == "" {
		return make(map[string]any), nil
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to decode claims: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, fmt.Errorf("auth: failed to unmarshal claims: %w", err)
	}
	if claims == nil {
		claims = make(map[string]any)
	}
	return claims, nil
}

// SerializeCallChain encodes a CallChain as a base64url-encoded JSON string.
// This format is safe for use in HTTP headers and gRPC metadata values.
//
// Returns an empty string if chain is nil.
// Returns an error if the chain cannot be marshaled to JSON or if the
// encoded output exceeds [MaxHeaderValueSize].
func SerializeCallChain(chain *CallChain) (string, error) {
	if chain == nil {
		return "", nil
	}
	data, err := json.Marshal(chain)
	if err != nil {
		return "", fmt.Errorf("auth: failed to marshal call chain: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	if len(encoded) > MaxHeaderValueSize {
		return "", fmt.Errorf("auth: serialized call chain size %d exceeds maximum %d bytes", len(encoded), MaxHeaderValueSize)
	}
	return encoded, nil
}

// DeserializeCallChain decodes a base64url-encoded JSON string into a CallChain.
// Returns nil if the encoded string is empty.
// Returns an error if the string cannot be decoded or parsed.
func DeserializeCallChain(encoded string) (*CallChain, error) {
	if encoded == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to decode call chain: %w", err)
	}
	var chain CallChain
	if err := json.Unmarshal(data, &chain); err != nil {
		return nil, fmt.Errorf("auth: failed to unmarshal call chain: %w", err)
	}
	return &chain, nil
}

// identityToHeaders extracts identity information into a set of key-value
// pairs suitable for use as HTTP headers or gRPC metadata.
// Returns nil if identity is nil.
func identityToHeaders(identity Identity, callerService string, chain *CallChain) (map[string]string, error) {
	if identity == nil {
		return nil, nil
	}

	headers := map[string]string{
		HeaderIdentityID:   identity.ID(),
		HeaderIdentityType: string(identity.Type()),
	}

	// Serialize claims if present.
	if claims := identity.Claims(); len(claims) > 0 {
		encoded, err := SerializeClaims(claims)
		if err != nil {
			return nil, err
		}
		headers[HeaderIdentityClaims] = encoded
	}

	// Include caller service if set.
	if callerService != "" {
		headers[HeaderCallerService] = callerService
	}

	// Serialize call chain if present.
	if chain != nil {
		encoded, err := SerializeCallChain(chain)
		if err != nil {
			return nil, err
		}
		headers[HeaderCallChain] = encoded
	}

	return headers, nil
}

// identityFromHeaders reconstructs an Identity and call chain metadata
// from a set of key-value pairs (HTTP headers or gRPC metadata).
//
// The getValue function retrieves a single value for a given key.
// Returns nil identity if no identity ID is found in the headers.
type headerGetter func(key string) string

func identityFromHeaders(getValue headerGetter) (Identity, string, *CallChain, error) {
	id := getValue(HeaderIdentityID)
	if id == "" {
		return nil, "", nil, nil
	}

	idType := IdentityType(getValue(HeaderIdentityType))
	if !idType.Valid() {
		// Default to service identity if type is missing or invalid,
		// since service-to-service propagation is the most common case.
		// Log a warning for non-empty invalid values to surface tampered
		// or malformed headers in security audits.
		if raw := string(idType); raw != "" {
			slog.Warn("auth: invalid identity type in propagated header, defaulting to service",
				"invalid_type", raw,
				"identity_id", id,
			)
		}
		idType = IdentityTypeService
	}

	// Deserialize claims.
	var claims map[string]any
	if encoded := getValue(HeaderIdentityClaims); encoded != "" {
		var err error
		claims, err = DeserializeClaims(encoded)
		if err != nil {
			return nil, "", nil, fmt.Errorf("auth: invalid propagated claims: %w", err)
		}
	}

	identity := NewBasicIdentity(id, idType, claims)

	// Extract caller service.
	callerService := getValue(HeaderCallerService)

	// Deserialize call chain.
	var chain *CallChain
	if encoded := getValue(HeaderCallChain); encoded != "" {
		var err error
		chain, err = DeserializeCallChain(encoded)
		if err != nil {
			return nil, "", nil, fmt.Errorf("auth: invalid propagated call chain: %w", err)
		}
	}

	return identity, callerService, chain, nil
}
