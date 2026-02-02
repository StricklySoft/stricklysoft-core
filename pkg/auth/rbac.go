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
//     or "resource:action:scope" format (e.g., "documents:read",
//     "deployments:write:production"). Malformed entries are silently
//     skipped.
//
//  2. "roles" — role names. Expected to be a []interface{} where each
//     element is a string that maps to permissions via the provided
//     RolePermissionMap. Unknown role names are silently ignored.
//
//  3. "scope" — OAuth2 scopes. Expected to be a single space-separated
//     string where each token is in "resource:action" or
//     "resource:action:scope" format (e.g., "agents:read logs:read",
//     "deployments:write:production agents:read"). Malformed tokens
//     are silently skipped.
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
// and parses each token as a permission using [ParsePermissionString].
// Both "resource:action" and "resource:action:scope" formats are
// supported. Tokens that do not conform to the expected format are
// silently skipped.
//
// Example:
//
//	ParseScopePermissions("agents:read deployments:write:production")
//	// returns []Permission{
//	//     {Resource: "agents", Action: "read"},
//	//     {Resource: "deployments", Action: "write", Scope: "production"},
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

// ParsePermissionString parses a permission string into a [Permission] value.
// Two formats are supported:
//
//   - "resource:action" — creates a Permission with an empty Scope (global).
//   - "resource:action:scope" — creates a Permission with the specified Scope.
//
// Both the resource and action parts may be the wildcard "*" to indicate
// unrestricted access. The scope part may also be "*" for explicit wildcard
// scope matching.
//
// Valid examples:
//
//	"documents:read"            -> Permission{Resource: "documents", Action: "read"}
//	"*:*"                       -> Permission{Resource: "*", Action: "*"}
//	"agents:execute"            -> Permission{Resource: "agents", Action: "execute"}
//	"deployments:write:prod"    -> Permission{Resource: "deployments", Action: "write", Scope: "prod"}
//	"*:*:*"                     -> Permission{Resource: "*", Action: "*", Scope: "*"}
//
// Returns an error if the string does not contain a colon separator, or
// if either the resource or action part is empty after splitting. An empty
// scope in three-part format (e.g., "docs:read:") is treated as an error.
func ParsePermissionString(s string) (Permission, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 {
		return Permission{}, fmt.Errorf("auth: invalid permission string %q: missing colon separator", s)
	}

	resource := parts[0]
	action := parts[1]

	if resource == "" {
		return Permission{}, errors.New("auth: invalid permission string: empty resource")
	}
	if action == "" {
		return Permission{}, errors.New("auth: invalid permission string: empty action")
	}

	var scope string
	if len(parts) == 3 {
		scope = parts[2]
		if scope == "" {
			return Permission{}, errors.New("auth: invalid permission string: empty scope (use two-part format for global permissions)")
		}
	}

	return Permission{Resource: resource, Action: action, Scope: scope}, nil
}

// FormatPermission returns the string representation of a [Permission] in
// colon-delimited format. This is equivalent to calling [Permission.String]
// and is provided as a standalone function for use in contexts where a
// function value is needed (e.g., mapping, serialization pipelines).
//
// If the permission's Scope is empty or "*", the format is "resource:action".
// Otherwise, the format is "resource:action:scope".
func FormatPermission(p Permission) string {
	return p.String()
}

// ---------------------------------------------------------------------------
// Role
// ---------------------------------------------------------------------------

// Role represents a named collection of permissions that can be assigned to
// identities. Roles provide a convenient abstraction for managing groups of
// related permissions, following the Role-Based Access Control (RBAC) pattern.
//
// Each role has a human-readable Name (used as a key for lookups), an optional
// Description for documentation purposes, and a slice of [Permission] values
// that define what the role authorizes.
//
// Role is a value type and is safe for concurrent read access. The Permissions
// slice should not be modified after the role is created; use the provided
// methods for authorization checks.
type Role struct {
	// Name is the unique identifier for the role (e.g., "admin", "viewer").
	// Role names are case-sensitive and should follow lowercase conventions.
	Name string

	// Description is a human-readable explanation of the role's purpose
	// and the level of access it grants. Used for documentation and
	// administrative interfaces.
	Description string

	// Permissions is the set of resource/action/scope grants that this
	// role provides. A role with no permissions grants no access.
	Permissions []Permission
}

// HasPermission checks whether this role grants access to the specified
// resource and action. This is the scope-unaware form that matches
// permissions regardless of their Scope value, consistent with
// [Identity.HasPermission].
//
// Supports wildcard matching: a permission with Resource="*" matches any
// resource, and Action="*" matches any action.
func (r Role) HasPermission(resource, action string) bool {
	return hasPermission(r.Permissions, resource, action)
}

// HasScopedPermission checks whether this role grants access to the
// specified resource, action, and scope combination. Unlike [HasPermission],
// this method evaluates the Scope dimension using the matching rules
// defined in [Permission.Match].
//
// Use this method when authorization decisions must account for environment,
// tenant, or organizational scope boundaries.
func (r Role) HasScopedPermission(resource, action, scope string) bool {
	for _, p := range r.Permissions {
		if p.Match(resource, action, scope) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// PermissionSet
// ---------------------------------------------------------------------------

// resourceActionKey is a map key for scope-agnostic permission lookups.
// It groups permissions by their Resource and Action fields, ignoring Scope.
type resourceActionKey struct {
	Resource string
	Action   string
}

// PermissionSet is an optimized, immutable collection of permissions that
// provides O(1) exact-match lookups for non-wildcard permissions and a
// linear fallback for wildcard matching.
//
// Internally, permissions are split into two groups at construction time:
//   - Exact permissions (no wildcards in Resource, Action, or Scope) are
//     stored in a map for O(1) lookup via [PermissionSet.Has].
//   - Wildcard permissions (any field is "*") are stored in a slice for
//     linear scanning when exact lookup misses.
//
// An additional scope-agnostic index (anyScope) enables O(1) lookups for
// the common case where the check scope is "" or "*", which per the
// [Permission.Match] semantics matches any permission regardless of scope.
// This is the path taken by [Identity.HasPermission] (2-arg, scope-unaware).
//
// This design is optimized for the common case where most permission checks
// are against specific resource/action/scope combinations, while still
// supporting wildcard grants like "*:*" (admin access).
//
// PermissionSet is safe for concurrent read access after construction.
type PermissionSet struct {
	// exact holds non-wildcard permissions for O(1) lookup.
	// The key is the full Permission struct (Resource, Action, Scope).
	exact map[Permission]struct{}

	// anyScope maps {Resource, Action} pairs to existence, regardless
	// of Scope. This enables O(1) lookups when the check scope is ""
	// or "*" (which match any permission scope). Only non-wildcard
	// (exact) permissions are indexed here; wildcard permissions are
	// handled by the wildcards slice.
	anyScope map[resourceActionKey]struct{}

	// wildcards holds permissions where at least one field is "*".
	// These require linear scanning via Permission.Match().
	wildcards []Permission

	// all holds the complete, ordered list of permissions for
	// introspection via Permissions(). This preserves insertion order.
	all []Permission
}

// NewPermissionSet creates a new [PermissionSet] from the given permissions.
// Permissions are deduplicated and split into exact-match and wildcard
// groups at construction time. The input slice is not modified.
//
// A nil or empty input produces a valid, empty PermissionSet.
func NewPermissionSet(perms []Permission) *PermissionSet {
	ps := &PermissionSet{
		exact:    make(map[Permission]struct{}, len(perms)),
		anyScope: make(map[resourceActionKey]struct{}, len(perms)),
	}

	seen := make(map[Permission]struct{}, len(perms))
	for _, p := range perms {
		// Deduplicate: skip permissions we've already added.
		if _, exists := seen[p]; exists {
			continue
		}
		seen[p] = struct{}{}

		ps.all = append(ps.all, p)

		if p.Resource == "*" || p.Action == "*" || p.Scope == "*" {
			ps.wildcards = append(ps.wildcards, p)
		} else {
			ps.exact[p] = struct{}{}
			ps.anyScope[resourceActionKey{Resource: p.Resource, Action: p.Action}] = struct{}{}
		}
	}

	return ps
}

// Has performs an O(1) exact-match lookup for the specified resource,
// action, and scope combination. It checks only the exact permission map
// and does NOT evaluate wildcard permissions.
//
// Use Has when you need fast, precise permission checks and wildcard
// grants are not applicable (e.g., checking if a specific scoped
// permission was explicitly granted).
//
// For authorization decisions that should respect wildcard permissions,
// use [PermissionSet.Match] instead.
func (ps *PermissionSet) Has(resource, action, scope string) bool {
	_, exists := ps.exact[Permission{Resource: resource, Action: action, Scope: scope}]
	return exists
}

// Match checks whether the permission set grants access to the specified
// resource, action, and scope combination. It uses a multi-level lookup
// strategy for optimal performance:
//
//  1. O(1) exact match: checks for the precise {resource, action, scope} tuple.
//  2. O(1) global permission check: if the check scope is specific, checks
//     for a global permission {resource, action, ""} that applies to all scopes.
//  3. O(1) scope-agnostic check: if the check scope is "" or "*", checks
//     whether any non-wildcard permission exists for {resource, action}
//     regardless of its scope (since empty/"*" check scope matches any
//     permission scope per [Permission.Match] semantics).
//  4. Linear wildcard scan: falls back to scanning wildcard permissions.
//
// This is the recommended method for authorization decisions, as it
// correctly handles both exact and wildcard permissions.
func (ps *PermissionSet) Match(resource, action, scope string) bool {
	// Fast path 1: O(1) exact match for the full {resource, action, scope} tuple.
	if ps.Has(resource, action, scope) {
		return true
	}

	if scope == "" || scope == "*" {
		// Fast path 2a: When check scope is "" or "*", it matches any
		// permission scope. Check the scope-agnostic index for O(1) lookup.
		if _, exists := ps.anyScope[resourceActionKey{Resource: resource, Action: action}]; exists {
			return true
		}
	} else {
		// Fast path 2b: Check for a global permission (Scope="") that
		// applies to all scopes. This handles the common case where a
		// global permission is in the exact map but the check has a
		// specific scope.
		if _, exists := ps.exact[Permission{Resource: resource, Action: action, Scope: ""}]; exists {
			return true
		}
	}

	// Slow path: linear scan of wildcard permissions.
	for _, p := range ps.wildcards {
		if p.Match(resource, action, scope) {
			return true
		}
	}

	return false
}

// Permissions returns a defensive copy of all permissions in the set,
// preserving the original insertion order (after deduplication). Callers
// may safely modify the returned slice without affecting the PermissionSet.
func (ps *PermissionSet) Permissions() []Permission {
	copied := make([]Permission, len(ps.all))
	copy(copied, ps.all)
	return copied
}

// Len returns the number of unique permissions in the set.
func (ps *PermissionSet) Len() int {
	return len(ps.all)
}

// ---------------------------------------------------------------------------
// Standard Roles
// ---------------------------------------------------------------------------

// StandardRoles returns the platform's standard role definitions as [Role]
// structs with names, descriptions, and associated permissions. These roles
// represent the baseline authorization model for the StricklySoft Cloud
// Platform:
//
//   - admin: Full, unrestricted access to all resources, actions, and scopes.
//     Intended for platform administrators and automated system processes
//     that require complete control.
//
//   - operator: Broad operational access to manage agents, deployments, and
//     infrastructure, with read access to logs and monitoring data. Intended
//     for SRE and operations teams.
//
//   - viewer: Read-only access to all resources across all scopes. Intended
//     for auditors, stakeholders, and read-only integrations.
//
// These roles are intentionally scope-unaware (Scope="" on all permissions),
// meaning they apply globally. For scope-restricted roles, create custom
// [Role] values with appropriately scoped permissions.
//
// Each call returns a new slice of roles; callers may safely modify it.
func StandardRoles() []Role {
	return []Role{
		{
			Name:        "admin",
			Description: "Full unrestricted access to all resources, actions, and scopes. Intended for platform administrators.",
			Permissions: []Permission{
				{Resource: "*", Action: "*"},
			},
		},
		{
			Name:        "operator",
			Description: "Operational access to manage agents, deployments, and infrastructure with read access to logs. Intended for SRE and operations teams.",
			Permissions: []Permission{
				{Resource: "agents", Action: "*"},
				{Resource: "deployments", Action: "*"},
				{Resource: "logs", Action: "read"},
			},
		},
		{
			Name:        "viewer",
			Description: "Read-only access to all resources. Intended for auditors, stakeholders, and read-only integrations.",
			Permissions: []Permission{
				{Resource: "*", Action: "read"},
			},
		},
	}
}

// StandardRoleMap returns the platform's standard roles indexed by name
// for O(1) lookup. This is a convenience wrapper around [StandardRoles]
// for contexts where role lookup by name is needed (e.g., mapping JWT
// role claims to Role structs).
//
// Each call returns a new map; callers may safely modify it.
func StandardRoleMap() map[string]Role {
	roles := StandardRoles()
	m := make(map[string]Role, len(roles))
	for _, r := range roles {
		m[r.Name] = r
	}
	return m
}
