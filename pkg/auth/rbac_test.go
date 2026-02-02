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
