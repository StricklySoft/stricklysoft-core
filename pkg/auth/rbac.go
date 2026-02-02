package auth

import (
	"errors"
	"fmt"
	"strings"
)

// RolePermissionMap maps role names to slices of Permission. It is the
// central data structure for role-based access control (RBAC), defining
// which permissions are granted to each role in the platform.
//
// Roles are identified by lowercase strings (e.g., "admin", "viewer").
// Each role maps to one or more Permission values that define what
// resources and actions the role is authorized to access.
//
// Example:
//
//	rpm := RolePermissionMap{
//	    "admin":  {{Resource: "*", Action: "*"}},
//	    "viewer": {{Resource: "*", Action: "read"}},
//	}
type RolePermissionMap map[string][]Permission

// DefaultRolePermissions returns the default platform role-to-permission
// mapping used by the StricklySoft Cloud Platform. This mapping defines
// four standard roles with progressively narrower access:
//
//   - admin: Full access to all resources and actions (wildcard).
//   - operator: Full access to agents and deployments, plus read-only
//     access to logs.
//   - developer: Read and execute access to agents, read-only access
//     to logs and deployments.
//   - viewer: Read-only access to all resources.
//
// Callers may use this as a starting point and extend or override the
// mapping for tenant-specific or service-specific customization.
func DefaultRolePermissions() RolePermissionMap {
	return RolePermissionMap{
		"admin": {
			{Resource: "*", Action: "*"},
		},
		"operator": {
			{Resource: "agents", Action: "*"},
			{Resource: "deployments", Action: "*"},
			{Resource: "logs", Action: "read"},
		},
		"developer": {
			{Resource: "agents", Action: "read"},
			{Resource: "agents", Action: "execute"},
			{Resource: "logs", Action: "read"},
			{Resource: "deployments", Action: "read"},
		},
		"viewer": {
			{Resource: "*", Action: "read"},
		},
	}
}

// ClaimsToPermissions extracts a deduplicated slice of Permission from
// JWT claims by inspecting three well-known claim fields, in order:
//
//  1. "permissions" — direct permission grants. Expected to be a
//     []interface{} where each element is a string in "resource:action"
//     format (e.g., "documents:read"). Malformed entries are silently
//     skipped.
//
//  2. "roles" — role names. Expected to be a []interface{} where each
//     element is a string that maps to permissions via the provided
//     RolePermissionMap. Unknown role names are silently ignored.
//
//  3. "scope" — OAuth2 scopes. Expected to be a single space-separated
//     string where each token is in "resource:action" format (e.g.,
//     "agents:read logs:read"). Malformed tokens are silently skipped.
//
// Permissions from all three sources are merged and deduplicated before
// being returned. The function never returns an error; malformed or
// missing claim values are silently ignored to support diverse token
// formats without requiring callers to handle parse failures.
//
// If claims is nil or empty, an empty slice is returned.
func ClaimsToPermissions(claims map[string]any, roleMap RolePermissionMap) []Permission {
	if len(claims) == 0 {
		return []Permission{}
	}

	seen := make(map[Permission]struct{})
	var result []Permission

	addPerm := func(p Permission) {
		if _, exists := seen[p]; !exists {
			seen[p] = struct{}{}
			result = append(result, p)
		}
	}

	// 1. Direct permission grants from "permissions" claim.
	if raw, ok := claims["permissions"]; ok {
		if perms, ok := raw.([]interface{}); ok {
			for _, entry := range perms {
				s, ok := entry.(string)
				if !ok {
					continue
				}
				p, err := ParsePermissionString(s)
				if err != nil {
					continue
				}
				addPerm(p)
			}
		}
	}

	// 2. Role-based permissions from "roles" claim.
	if raw, ok := claims["roles"]; ok {
		if roles, ok := raw.([]interface{}); ok {
			for _, entry := range roles {
				roleName, ok := entry.(string)
				if !ok {
					continue
				}
				if perms, exists := roleMap[roleName]; exists {
					for _, p := range perms {
						addPerm(p)
					}
				}
			}
		}
	}

	// 3. OAuth2 scope permissions from "scope" claim.
	if raw, ok := claims["scope"]; ok {
		if scopeStr, ok := raw.(string); ok {
			for _, p := range ParseScopePermissions(scopeStr) {
				addPerm(p)
			}
		}
	}

	if result == nil {
		return []Permission{}
	}
	return result
}

// DefaultClaimsToPermissions is a convenience function that extracts
// permissions from JWT claims using the platform's default role mapping
// returned by [DefaultRolePermissions]. It is equivalent to calling:
//
//	ClaimsToPermissions(claims, DefaultRolePermissions())
//
// This is the recommended entry point for services that use the
// standard platform roles without customization.
func DefaultClaimsToPermissions(claims map[string]any) []Permission {
	return ClaimsToPermissions(claims, DefaultRolePermissions())
}

// ParseScopePermissions splits a space-separated OAuth2 scope string
// and parses each token as a "resource:action" permission. Tokens that
// do not conform to the expected format are silently skipped.
//
// Example:
//
//	ParseScopePermissions("agents:read logs:read")
//	// returns []Permission{
//	//     {Resource: "agents", Action: "read"},
//	//     {Resource: "logs", Action: "read"},
//	// }
//
// An empty string returns an empty slice.
func ParseScopePermissions(scope string) []Permission {
	if scope == "" {
		return []Permission{}
	}

	tokens := strings.Fields(scope)
	var result []Permission
	for _, token := range tokens {
		p, err := ParsePermissionString(token)
		if err != nil {
			continue
		}
		result = append(result, p)
	}

	if result == nil {
		return []Permission{}
	}
	return result
}

// ParsePermissionString parses a string in "resource:action" format into
// a Permission value. Both the resource and action parts may be the
// wildcard "*" to indicate unrestricted access.
//
// Valid examples:
//
//	"documents:read"   -> Permission{Resource: "documents", Action: "read"}
//	"*:*"              -> Permission{Resource: "*", Action: "*"}
//	"agents:execute"   -> Permission{Resource: "agents", Action: "execute"}
//
// Returns an error if the string does not contain a colon separator, or
// if either the resource or action part is empty after splitting.
func ParsePermissionString(s string) (Permission, error) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return Permission{}, fmt.Errorf("auth: invalid permission string %q: missing colon separator", s)
	}

	resource := s[:idx]
	action := s[idx+1:]

	if resource == "" {
		return Permission{}, errors.New("auth: invalid permission string: empty resource")
	}
	if action == "" {
		return Permission{}, errors.New("auth: invalid permission string: empty action")
	}

	return Permission{Resource: resource, Action: action}, nil
}
