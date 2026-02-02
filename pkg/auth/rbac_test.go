package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DefaultRolePermissions
// ---------------------------------------------------------------------------

func TestDefaultRolePermissions_ContainsAllRoles(t *testing.T) {
	t.Parallel()
	rpm := DefaultRolePermissions()

	expectedRoles := []string{"admin", "operator", "developer", "viewer"}
	for _, role := range expectedRoles {
		_, exists := rpm[role]
		assert.True(t, exists, "DefaultRolePermissions() missing role %q", role)
	}
	assert.Len(t, rpm, len(expectedRoles), "DefaultRolePermissions() should contain exactly %d roles", len(expectedRoles))
}

func TestDefaultRolePermissions_AdminHasFullAccess(t *testing.T) {
	t.Parallel()
	rpm := DefaultRolePermissions()

	adminPerms := rpm["admin"]
	require.Len(t, adminPerms, 1, "admin role should have exactly 1 permission (wildcard)")
	assert.Equal(t, Permission{Resource: "*", Action: "*"}, adminPerms[0], "admin should have full wildcard permission")
}

func TestDefaultRolePermissions_ViewerHasReadOnly(t *testing.T) {
	t.Parallel()
	rpm := DefaultRolePermissions()

	viewerPerms := rpm["viewer"]
	require.Len(t, viewerPerms, 1, "viewer role should have exactly 1 permission")
	assert.Equal(t, Permission{Resource: "*", Action: "read"}, viewerPerms[0], "viewer should have read-only wildcard permission")
}

// ---------------------------------------------------------------------------
// ClaimsToPermissions
// ---------------------------------------------------------------------------

func TestClaimsToPermissions_FromRoles(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"roles": []interface{}{"admin"},
	}
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(claims, rpm)

	require.Len(t, perms, 1)
	assert.Equal(t, Permission{Resource: "*", Action: "*"}, perms[0])
}

func TestClaimsToPermissions_FromDirectPermissions(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"permissions": []interface{}{"documents:read", "users:write"},
	}
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(claims, rpm)

	require.Len(t, perms, 2)
	assert.Contains(t, perms, Permission{Resource: "documents", Action: "read"})
	assert.Contains(t, perms, Permission{Resource: "users", Action: "write"})
}

func TestClaimsToPermissions_FromScope(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"scope": "agents:read logs:read",
	}
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(claims, rpm)

	require.Len(t, perms, 2)
	assert.Contains(t, perms, Permission{Resource: "agents", Action: "read"})
	assert.Contains(t, perms, Permission{Resource: "logs", Action: "read"})
}

func TestClaimsToPermissions_Combined(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"permissions": []interface{}{"documents:read"},
		"roles":       []interface{}{"viewer"},
		"scope":       "agents:execute",
	}
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(claims, rpm)

	// "documents:read" from permissions, "*:read" from viewer role, "agents:execute" from scope.
	assert.Contains(t, perms, Permission{Resource: "documents", Action: "read"})
	assert.Contains(t, perms, Permission{Resource: "*", Action: "read"})
	assert.Contains(t, perms, Permission{Resource: "agents", Action: "execute"})
	assert.Len(t, perms, 3)
}

func TestClaimsToPermissions_NilClaims(t *testing.T) {
	t.Parallel()
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(nil, rpm)

	assert.NotNil(t, perms, "ClaimsToPermissions(nil, ...) should return non-nil slice")
	assert.Empty(t, perms, "ClaimsToPermissions(nil, ...) should return empty slice")
}

func TestClaimsToPermissions_EmptyClaims(t *testing.T) {
	t.Parallel()
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(map[string]any{}, rpm)

	assert.NotNil(t, perms, "ClaimsToPermissions with empty claims should return non-nil slice")
	assert.Empty(t, perms, "ClaimsToPermissions with empty claims should return empty slice")
}

func TestClaimsToPermissions_UnknownRole_Ignored(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"roles": []interface{}{"nonexistent-role"},
	}
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(claims, rpm)

	assert.NotNil(t, perms, "unknown role should not cause nil result")
	assert.Empty(t, perms, "unknown role should produce no permissions")
}

func TestClaimsToPermissions_Deduplicated(t *testing.T) {
	t.Parallel()
	// Supply the same permission via direct permissions, roles, and scope.
	claims := map[string]any{
		"permissions": []interface{}{"agents:read"},
		"roles":       []interface{}{"developer"}, // developer includes agents:read
		"scope":       "agents:read",
	}
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(claims, rpm)

	// Count occurrences of agents:read — should appear exactly once.
	count := 0
	for _, p := range perms {
		if p.Resource == "agents" && p.Action == "read" {
			count++
		}
	}
	assert.Equal(t, 1, count, "agents:read should appear exactly once after deduplication")
}

func TestClaimsToPermissions_MalformedPermission_Skipped(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"permissions": []interface{}{"badformat", "documents:read"},
	}
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(claims, rpm)

	require.Len(t, perms, 1, "malformed permission should be skipped, valid one kept")
	assert.Equal(t, Permission{Resource: "documents", Action: "read"}, perms[0])
}

// ---------------------------------------------------------------------------
// DefaultClaimsToPermissions
// ---------------------------------------------------------------------------

func TestDefaultClaimsToPermissions_UsesDefaultRoles(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"roles": []interface{}{"admin"},
	}

	perms := DefaultClaimsToPermissions(claims)

	require.Len(t, perms, 1)
	assert.Equal(t, Permission{Resource: "*", Action: "*"}, perms[0],
		"DefaultClaimsToPermissions should delegate to ClaimsToPermissions with DefaultRolePermissions()")
}

// ---------------------------------------------------------------------------
// ParseScopePermissions
// ---------------------------------------------------------------------------

func TestParseScopePermissions_Single(t *testing.T) {
	t.Parallel()

	perms := ParseScopePermissions("agents:read")

	require.Len(t, perms, 1)
	assert.Equal(t, Permission{Resource: "agents", Action: "read"}, perms[0])
}

func TestParseScopePermissions_Multiple(t *testing.T) {
	t.Parallel()

	perms := ParseScopePermissions("agents:read logs:read deployments:write")

	require.Len(t, perms, 3)
	assert.Contains(t, perms, Permission{Resource: "agents", Action: "read"})
	assert.Contains(t, perms, Permission{Resource: "logs", Action: "read"})
	assert.Contains(t, perms, Permission{Resource: "deployments", Action: "write"})
}

func TestParseScopePermissions_Empty(t *testing.T) {
	t.Parallel()

	perms := ParseScopePermissions("")

	assert.NotNil(t, perms, "ParseScopePermissions(\"\") should return non-nil slice")
	assert.Empty(t, perms, "ParseScopePermissions(\"\") should return empty slice")
}

func TestParseScopePermissions_InvalidFormat_Skipped(t *testing.T) {
	t.Parallel()

	perms := ParseScopePermissions("valid:read nocolon also-bad valid:write")

	require.Len(t, perms, 2, "invalid scope entries should be skipped")
	assert.Contains(t, perms, Permission{Resource: "valid", Action: "read"})
	assert.Contains(t, perms, Permission{Resource: "valid", Action: "write"})
}

// ---------------------------------------------------------------------------
// ParsePermissionString
// ---------------------------------------------------------------------------

func TestParsePermissionString_Valid(t *testing.T) {
	t.Parallel()

	p, err := ParsePermissionString("documents:read")

	require.NoError(t, err)
	assert.Equal(t, "documents", p.Resource)
	assert.Equal(t, "read", p.Action)
}

func TestParsePermissionString_Wildcard(t *testing.T) {
	t.Parallel()

	p, err := ParsePermissionString("*:*")

	require.NoError(t, err)
	assert.Equal(t, "*", p.Resource)
	assert.Equal(t, "*", p.Action)
}

func TestParsePermissionString_Invalid_NoColon(t *testing.T) {
	t.Parallel()

	_, err := ParsePermissionString("nocolon")

	require.Error(t, err, "ParsePermissionString should return error for string without colon")
	assert.Contains(t, err.Error(), "missing colon separator")
}

func TestParsePermissionString_Invalid_EmptyResource(t *testing.T) {
	t.Parallel()

	_, err := ParsePermissionString(":read")

	require.Error(t, err, "ParsePermissionString should return error for empty resource")
	assert.Contains(t, err.Error(), "empty resource")
}

func TestParsePermissionString_Invalid_EmptyAction(t *testing.T) {
	t.Parallel()

	_, err := ParsePermissionString("docs:")

	require.Error(t, err, "ParsePermissionString should return error for empty action")
	assert.Contains(t, err.Error(), "empty action")
}

// ---------------------------------------------------------------------------
// ParsePermissionString — three-part format (resource:action:scope)
// ---------------------------------------------------------------------------

func TestParsePermissionString_ThreePart_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected Permission
	}{
		{
			name:     "specific scope",
			input:    "deployments:write:production",
			expected: Permission{Resource: "deployments", Action: "write", Scope: "production"},
		},
		{
			name:     "wildcard scope",
			input:    "*:*:*",
			expected: Permission{Resource: "*", Action: "*", Scope: "*"},
		},
		{
			name:     "staging scope",
			input:    "agents:execute:staging",
			expected: Permission{Resource: "agents", Action: "execute", Scope: "staging"},
		},
		{
			name:     "scope with hyphens",
			input:    "logs:read:us-east-1",
			expected: Permission{Resource: "logs", Action: "read", Scope: "us-east-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := ParsePermissionString(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, p)
		})
	}
}

func TestParsePermissionString_ThreePart_EmptyScope(t *testing.T) {
	t.Parallel()
	// "docs:read:" has an empty scope part — should be an error.
	_, err := ParsePermissionString("docs:read:")
	require.Error(t, err, "ParsePermissionString should return error for empty scope in three-part format")
	assert.Contains(t, err.Error(), "empty scope")
}

func TestParsePermissionString_TwoPart_HasEmptyScope(t *testing.T) {
	t.Parallel()
	// Two-part format should produce Permission with empty Scope.
	p, err := ParsePermissionString("documents:read")
	require.NoError(t, err)
	assert.Equal(t, "", p.Scope, "two-part format should produce empty Scope")
}

func TestParsePermissionString_ThreePart_RoundTrip(t *testing.T) {
	t.Parallel()
	// Parse → String → Parse should be idempotent.
	original := "deployments:write:production"
	p, err := ParsePermissionString(original)
	require.NoError(t, err)

	formatted := p.String()
	assert.Equal(t, original, formatted, "String() should produce the original input")

	reparsed, err := ParsePermissionString(formatted)
	require.NoError(t, err)
	assert.Equal(t, p, reparsed, "round-trip should produce equal Permission")
}

func TestParsePermissionString_TwoPart_RoundTrip(t *testing.T) {
	t.Parallel()
	original := "agents:execute"
	p, err := ParsePermissionString(original)
	require.NoError(t, err)

	formatted := p.String()
	assert.Equal(t, original, formatted)

	reparsed, err := ParsePermissionString(formatted)
	require.NoError(t, err)
	assert.Equal(t, p, reparsed)
}

// ---------------------------------------------------------------------------
// FormatPermission
// ---------------------------------------------------------------------------

func TestFormatPermission_EqualsString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		perm Permission
	}{
		{name: "global", perm: Permission{Resource: "docs", Action: "read"}},
		{name: "scoped", perm: Permission{Resource: "agents", Action: "execute", Scope: "production"}},
		{name: "wildcard", perm: Permission{Resource: "*", Action: "*", Scope: "*"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.perm.String(), FormatPermission(tt.perm),
				"FormatPermission should produce the same result as Permission.String()")
		})
	}
}

// ---------------------------------------------------------------------------
// Role
// ---------------------------------------------------------------------------

func TestRole_HasPermission_ExactMatch(t *testing.T) {
	t.Parallel()
	r := Role{
		Name: "developer",
		Permissions: []Permission{
			{Resource: "agents", Action: "read"},
			{Resource: "agents", Action: "execute"},
			{Resource: "logs", Action: "read"},
		},
	}

	assert.True(t, r.HasPermission("agents", "read"))
	assert.True(t, r.HasPermission("agents", "execute"))
	assert.True(t, r.HasPermission("logs", "read"))
	assert.False(t, r.HasPermission("agents", "delete"))
	assert.False(t, r.HasPermission("secrets", "read"))
}

func TestRole_HasPermission_Wildcard(t *testing.T) {
	t.Parallel()
	r := Role{
		Name:        "admin",
		Permissions: []Permission{{Resource: "*", Action: "*"}},
	}

	assert.True(t, r.HasPermission("anything", "anything"))
	assert.True(t, r.HasPermission("documents", "read"))
}

func TestRole_HasPermission_NoPermissions(t *testing.T) {
	t.Parallel()
	r := Role{Name: "empty"}

	assert.False(t, r.HasPermission("documents", "read"))
}

func TestRole_HasScopedPermission_ExactScope(t *testing.T) {
	t.Parallel()
	r := Role{
		Name: "scoped-operator",
		Permissions: []Permission{
			{Resource: "deployments", Action: "write", Scope: "production"},
			{Resource: "agents", Action: "read"},
		},
	}

	assert.True(t, r.HasScopedPermission("deployments", "write", "production"))
	assert.False(t, r.HasScopedPermission("deployments", "write", "staging"))
	assert.True(t, r.HasScopedPermission("deployments", "write", ""),
		"empty check scope should match scoped permission")
	assert.True(t, r.HasScopedPermission("agents", "read", "production"),
		"global perm should match any scope check")
	assert.True(t, r.HasScopedPermission("agents", "read", ""),
		"global perm should match empty scope check")
}

func TestRole_HasScopedPermission_WildcardScope(t *testing.T) {
	t.Parallel()
	r := Role{
		Name: "scoped-admin",
		Permissions: []Permission{
			{Resource: "*", Action: "*", Scope: "*"},
		},
	}

	assert.True(t, r.HasScopedPermission("anything", "anything", "any-scope"))
	assert.True(t, r.HasScopedPermission("docs", "read", ""))
}

func TestRole_HasScopedPermission_ScopeMismatch(t *testing.T) {
	t.Parallel()
	r := Role{
		Name: "prod-only",
		Permissions: []Permission{
			{Resource: "secrets", Action: "read", Scope: "production"},
		},
	}

	assert.False(t, r.HasScopedPermission("secrets", "read", "staging"),
		"production-scoped permission should not match staging")
}

func TestRole_HasScopedPermission_NoPermissions(t *testing.T) {
	t.Parallel()
	r := Role{Name: "empty"}

	assert.False(t, r.HasScopedPermission("docs", "read", ""))
}

// ---------------------------------------------------------------------------
// PermissionSet — Construction
// ---------------------------------------------------------------------------

func TestNewPermissionSet_NilInput(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet(nil)

	assert.Equal(t, 0, ps.Len())
	assert.NotNil(t, ps.Permissions())
	assert.Empty(t, ps.Permissions())
}

func TestNewPermissionSet_EmptyInput(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet([]Permission{})

	assert.Equal(t, 0, ps.Len())
}

func TestNewPermissionSet_Deduplication(t *testing.T) {
	t.Parallel()
	perms := []Permission{
		{Resource: "docs", Action: "read"},
		{Resource: "docs", Action: "read"}, // duplicate
		{Resource: "docs", Action: "write"},
		{Resource: "docs", Action: "read"}, // duplicate again
	}
	ps := NewPermissionSet(perms)

	assert.Equal(t, 2, ps.Len(), "duplicates should be removed")
}

func TestNewPermissionSet_ScopeAwareDedup(t *testing.T) {
	t.Parallel()
	// Same resource+action but different scopes are distinct.
	perms := []Permission{
		{Resource: "docs", Action: "read", Scope: "production"},
		{Resource: "docs", Action: "read", Scope: "staging"},
		{Resource: "docs", Action: "read"}, // global
	}
	ps := NewPermissionSet(perms)

	assert.Equal(t, 3, ps.Len(), "different scopes should produce distinct permissions")
}

// ---------------------------------------------------------------------------
// PermissionSet.Has — O(1) exact lookup
// ---------------------------------------------------------------------------

func TestPermissionSet_Has_ExactMatch(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet([]Permission{
		{Resource: "docs", Action: "read"},
		{Resource: "agents", Action: "execute", Scope: "production"},
	})

	assert.True(t, ps.Has("docs", "read", ""), "exact match should succeed")
	assert.True(t, ps.Has("agents", "execute", "production"), "scoped exact match should succeed")
	assert.False(t, ps.Has("docs", "read", "production"), "different scope should not exact-match")
	assert.False(t, ps.Has("agents", "execute", ""), "missing scope should not exact-match scoped perm")
	assert.False(t, ps.Has("users", "read", ""), "non-existent permission should not match")
}

func TestPermissionSet_Has_DoesNotMatchWildcards(t *testing.T) {
	t.Parallel()
	// Has() checks only the exact map. Wildcard permissions (any field is "*")
	// are stored in the wildcards slice, not the exact map, so Has() cannot
	// find them — even when searching for the literal "*" values.
	ps := NewPermissionSet([]Permission{
		{Resource: "*", Action: "*"},
	})

	assert.False(t, ps.Has("docs", "read", ""), "Has() should not use wildcard matching")
	assert.False(t, ps.Has("*", "*", ""),
		"Has() should not find wildcard permissions (they are in the wildcards slice, not exact map)")

	// Use Match() to find wildcard permissions.
	assert.True(t, ps.Match("docs", "read", ""), "Match() should find wildcard permissions")
	assert.True(t, ps.Match("*", "*", ""), "Match() should find the literal wildcard permission")
}

// ---------------------------------------------------------------------------
// PermissionSet.Match — exact + wildcard fallback
// ---------------------------------------------------------------------------

func TestPermissionSet_Match_ExactHit(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet([]Permission{
		{Resource: "docs", Action: "read"},
		{Resource: "agents", Action: "execute", Scope: "production"},
	})

	assert.True(t, ps.Match("docs", "read", ""), "exact match should succeed")
	assert.True(t, ps.Match("agents", "execute", "production"), "scoped exact match should succeed")
}

func TestPermissionSet_Match_WildcardFallback(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet([]Permission{
		{Resource: "*", Action: "*"},
	})

	assert.True(t, ps.Match("docs", "read", ""), "wildcard perm should match via fallback")
	assert.True(t, ps.Match("agents", "execute", "production"), "wildcard perm should match any scope")
}

func TestPermissionSet_Match_GlobalPermMatchesSpecificScope(t *testing.T) {
	t.Parallel()
	// A global permission (Scope="") in the exact map should match a check
	// with a specific scope, because global means "applies to all scopes".
	ps := NewPermissionSet([]Permission{
		{Resource: "docs", Action: "read"}, // Scope="" (global)
	})

	assert.True(t, ps.Match("docs", "read", ""), "global perm should match empty scope")
	assert.True(t, ps.Match("docs", "read", "production"),
		"global perm (exact map) should match specific scope check via Match()")
}

func TestPermissionSet_Match_ScopeMismatch(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet([]Permission{
		{Resource: "secrets", Action: "read", Scope: "production"},
	})

	assert.True(t, ps.Match("secrets", "read", "production"), "exact scope match")
	assert.False(t, ps.Match("secrets", "read", "staging"), "different scope should not match")
	assert.True(t, ps.Match("secrets", "read", ""),
		"empty check scope should match scoped perm (via Match global-check path)")
}

func TestPermissionSet_Match_MixedExactAndWildcard(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet([]Permission{
		{Resource: "docs", Action: "read"},
		{Resource: "*", Action: "read"},
		{Resource: "agents", Action: "*", Scope: "staging"},
	})

	assert.True(t, ps.Match("docs", "read", ""), "exact match")
	assert.True(t, ps.Match("users", "read", ""), "wildcard resource match")
	assert.True(t, ps.Match("agents", "execute", "staging"), "wildcard action + exact scope match")
	assert.False(t, ps.Match("agents", "execute", "production"),
		"wildcard action + wrong scope should not match")
	assert.True(t, ps.Match("agents", "execute", ""),
		"wildcard action + empty scope should match (empty scope = match any)")
}

func TestPermissionSet_Match_NoMatch(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet([]Permission{
		{Resource: "docs", Action: "read"},
	})

	assert.False(t, ps.Match("secrets", "delete", "production"), "completely different permission should not match")
}

// ---------------------------------------------------------------------------
// PermissionSet.Permissions — defensive copy
// ---------------------------------------------------------------------------

func TestPermissionSet_Permissions_DefensiveCopy(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet([]Permission{
		{Resource: "docs", Action: "read"},
		{Resource: "users", Action: "write"},
	})

	first := ps.Permissions()
	require.Len(t, first, 2)

	// Mutate the returned slice.
	first[0] = Permission{Resource: "HACKED", Action: "HACKED"}

	// Second call should return original values.
	second := ps.Permissions()
	assert.Equal(t, "docs", second[0].Resource, "Permissions() mutation leaked into PermissionSet")
}

func TestPermissionSet_Permissions_PreservesOrder(t *testing.T) {
	t.Parallel()
	perms := []Permission{
		{Resource: "aaa", Action: "first"},
		{Resource: "bbb", Action: "second"},
		{Resource: "ccc", Action: "third"},
	}
	ps := NewPermissionSet(perms)

	got := ps.Permissions()
	require.Len(t, got, 3)
	assert.Equal(t, "aaa", got[0].Resource)
	assert.Equal(t, "bbb", got[1].Resource)
	assert.Equal(t, "ccc", got[2].Resource)
}

// ---------------------------------------------------------------------------
// PermissionSet — Empty
// ---------------------------------------------------------------------------

func TestPermissionSet_Empty_Has(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet(nil)

	assert.False(t, ps.Has("docs", "read", ""))
}

func TestPermissionSet_Empty_Match(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet(nil)

	assert.False(t, ps.Match("docs", "read", ""))
}

func TestPermissionSet_Empty_Len(t *testing.T) {
	t.Parallel()
	ps := NewPermissionSet(nil)

	assert.Equal(t, 0, ps.Len())
}

// ---------------------------------------------------------------------------
// StandardRoles
// ---------------------------------------------------------------------------

func TestStandardRoles_ContainsThreeRoles(t *testing.T) {
	t.Parallel()
	roles := StandardRoles()

	assert.Len(t, roles, 3, "StandardRoles should return exactly 3 roles")
}

func TestStandardRoles_RoleNames(t *testing.T) {
	t.Parallel()
	roles := StandardRoles()
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = r.Name
	}

	assert.Contains(t, names, "admin")
	assert.Contains(t, names, "operator")
	assert.Contains(t, names, "viewer")
}

func TestStandardRoles_AdminHasFullAccess(t *testing.T) {
	t.Parallel()
	roles := StandardRoles()
	var admin Role
	for _, r := range roles {
		if r.Name == "admin" {
			admin = r
			break
		}
	}

	require.NotEmpty(t, admin.Name, "admin role should exist")
	assert.NotEmpty(t, admin.Description, "admin role should have a description")
	assert.True(t, admin.HasPermission("anything", "anything"), "admin should have full wildcard access")
	assert.True(t, admin.HasScopedPermission("docs", "delete", "production"), "admin should match any scope")
}

func TestStandardRoles_OperatorPermissions(t *testing.T) {
	t.Parallel()
	roles := StandardRoles()
	var operator Role
	for _, r := range roles {
		if r.Name == "operator" {
			operator = r
			break
		}
	}

	require.NotEmpty(t, operator.Name)
	assert.NotEmpty(t, operator.Description)

	// Operator should have agents:* and deployments:* and logs:read.
	assert.True(t, operator.HasPermission("agents", "read"))
	assert.True(t, operator.HasPermission("agents", "execute"))
	assert.True(t, operator.HasPermission("agents", "delete"))
	assert.True(t, operator.HasPermission("deployments", "write"))
	assert.True(t, operator.HasPermission("logs", "read"))
	assert.False(t, operator.HasPermission("logs", "write"), "operator should not have logs:write")
	assert.False(t, operator.HasPermission("secrets", "read"), "operator should not access secrets")
}

func TestStandardRoles_ViewerPermissions(t *testing.T) {
	t.Parallel()
	roles := StandardRoles()
	var viewer Role
	for _, r := range roles {
		if r.Name == "viewer" {
			viewer = r
			break
		}
	}

	require.NotEmpty(t, viewer.Name)
	assert.NotEmpty(t, viewer.Description)

	// Viewer should have *:read.
	assert.True(t, viewer.HasPermission("documents", "read"))
	assert.True(t, viewer.HasPermission("agents", "read"))
	assert.True(t, viewer.HasPermission("anything", "read"))
	assert.False(t, viewer.HasPermission("documents", "write"), "viewer should not have write access")
	assert.False(t, viewer.HasPermission("agents", "delete"), "viewer should not have delete access")
}

func TestStandardRoles_ReturnsNewSlice(t *testing.T) {
	t.Parallel()
	// Each call should return a new slice to prevent mutation.
	first := StandardRoles()
	second := StandardRoles()

	first[0].Name = "MUTATED"
	assert.NotEqual(t, "MUTATED", second[0].Name, "StandardRoles should return a new slice each time")
}

// ---------------------------------------------------------------------------
// StandardRoleMap
// ---------------------------------------------------------------------------

func TestStandardRoleMap_AllRolesPresent(t *testing.T) {
	t.Parallel()
	m := StandardRoleMap()

	assert.Len(t, m, 3)
	_, hasAdmin := m["admin"]
	_, hasOperator := m["operator"]
	_, hasViewer := m["viewer"]
	assert.True(t, hasAdmin, "admin should be in StandardRoleMap")
	assert.True(t, hasOperator, "operator should be in StandardRoleMap")
	assert.True(t, hasViewer, "viewer should be in StandardRoleMap")
}

func TestStandardRoleMap_LookupByName(t *testing.T) {
	t.Parallel()
	m := StandardRoleMap()

	admin := m["admin"]
	assert.Equal(t, "admin", admin.Name)
	assert.True(t, admin.HasPermission("anything", "anything"))

	viewer := m["viewer"]
	assert.Equal(t, "viewer", viewer.Name)
	assert.True(t, viewer.HasPermission("docs", "read"))
	assert.False(t, viewer.HasPermission("docs", "write"))
}

func TestStandardRoleMap_UnknownRole(t *testing.T) {
	t.Parallel()
	m := StandardRoleMap()

	_, exists := m["nonexistent"]
	assert.False(t, exists, "unknown role should not be in StandardRoleMap")
}

func TestStandardRoleMap_ReturnsNewMap(t *testing.T) {
	t.Parallel()
	first := StandardRoleMap()
	second := StandardRoleMap()

	delete(first, "admin")
	_, exists := second["admin"]
	assert.True(t, exists, "StandardRoleMap should return a new map each time")
}

// ---------------------------------------------------------------------------
// ClaimsToPermissions with three-part scope permissions
// ---------------------------------------------------------------------------

func TestClaimsToPermissions_ThreePartScopePermissions(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"scope": "deployments:write:production agents:read",
	}
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(claims, rpm)

	require.Len(t, perms, 2)
	assert.Contains(t, perms, Permission{Resource: "deployments", Action: "write", Scope: "production"})
	assert.Contains(t, perms, Permission{Resource: "agents", Action: "read"})
}

func TestClaimsToPermissions_ThreePartDirectPermissions(t *testing.T) {
	t.Parallel()
	claims := map[string]any{
		"permissions": []interface{}{"secrets:read:production", "logs:read"},
	}
	rpm := DefaultRolePermissions()

	perms := ClaimsToPermissions(claims, rpm)

	require.Len(t, perms, 2)
	assert.Contains(t, perms, Permission{Resource: "secrets", Action: "read", Scope: "production"})
	assert.Contains(t, perms, Permission{Resource: "logs", Action: "read"})
}
