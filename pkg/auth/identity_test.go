package auth

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustNewServiceIdentity creates a ServiceIdentity, failing the test if
// construction returns an error. Use this in tests where valid inputs
// are expected.
func mustNewServiceIdentity(t *testing.T, id, serviceName, namespace string, claims map[string]any, permissions []Permission) *ServiceIdentity {
	t.Helper()
	identity, err := NewServiceIdentity(id, serviceName, namespace, claims, permissions)
	require.NoError(t, err, "NewServiceIdentity(%q, %q, %q, ...) unexpected error", id, serviceName, namespace)
	return identity
}

// mustNewUserIdentity creates a UserIdentity, failing the test if
// construction returns an error. Use this in tests where valid inputs
// are expected.
func mustNewUserIdentity(t *testing.T, id, email, displayName string, claims map[string]any, permissions []Permission) *UserIdentity {
	t.Helper()
	identity, err := NewUserIdentity(id, email, displayName, claims, permissions)
	require.NoError(t, err, "NewUserIdentity(%q, %q, %q, ...) unexpected error", id, email, displayName)
	return identity
}

func TestIdentityType_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		idType   IdentityType
		expected string
	}{
		{name: "user type", idType: IdentityTypeUser, expected: "user"},
		{name: "service type", idType: IdentityTypeService, expected: "service"},
		{name: "agent type", idType: IdentityTypeAgent, expected: "agent"},
		{name: "system type", idType: IdentityTypeSystem, expected: "system"},
		{name: "custom type", idType: IdentityType("custom"), expected: "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.idType.String())
		})
	}
}

func TestIdentityType_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		idType   IdentityType
		expected bool
	}{
		{name: "user is valid", idType: IdentityTypeUser, expected: true},
		{name: "service is valid", idType: IdentityTypeService, expected: true},
		{name: "agent is valid", idType: IdentityTypeAgent, expected: true},
		{name: "system is valid", idType: IdentityTypeSystem, expected: true},
		{name: "empty is invalid", idType: IdentityType(""), expected: false},
		{name: "unknown is invalid", idType: IdentityType("robot"), expected: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.idType.Valid())
		})
	}
}

func TestNewBasicIdentity(t *testing.T) {
	t.Parallel()
	claims := map[string]any{"email": "user@example.com", "role": "admin"}
	identity := NewBasicIdentity("user-123", IdentityTypeUser, claims)

	assert.Equal(t, "user-123", identity.ID())
	assert.Equal(t, IdentityTypeUser, identity.Type())
	assert.Len(t, identity.Claims(), 2)
	assert.Equal(t, "user@example.com", identity.Claims()["email"])
}

func TestNewBasicIdentity_ClaimsAreCopied(t *testing.T) {
	t.Parallel()
	claims := map[string]any{"key": "original"}
	identity := NewBasicIdentity("id", IdentityTypeUser, claims)

	// Mutating the original map should not affect the identity.
	claims["key"] = "mutated"
	claims["new_key"] = "new_value"

	assert.Equal(t, "original", identity.Claims()["key"], "claims mutation leaked into identity; expected defensive copy")
	_, exists := identity.Claims()["new_key"]
	assert.False(t, exists, "new claim key leaked into identity; expected defensive copy")
}

func TestNewBasicIdentity_NilClaims(t *testing.T) {
	t.Parallel()
	identity := NewBasicIdentity("id", IdentityTypeService, nil)

	assert.NotNil(t, identity.Claims(), "Claims() returned nil, expected empty map")
	assert.Len(t, identity.Claims(), 0)
}

// Verify concrete types implement the Identity interface at compile time.
var _ Identity = (*BasicIdentity)(nil)
var _ Identity = (*ServiceIdentity)(nil)
var _ Identity = (*UserIdentity)(nil)

func TestBasicIdentity_ClaimsReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()
	identity := NewBasicIdentity("user-1", IdentityTypeUser, map[string]any{"key": "original"})

	// Mutating the returned map should not affect subsequent calls.
	first := identity.Claims()
	first["key"] = "mutated"
	first["injected"] = "attack"

	second := identity.Claims()
	assert.Equal(t, "original", second["key"], "Claims() mutation leaked")
	_, exists := second["injected"]
	assert.False(t, exists, "Claims() mutation leaked: injected key should not exist")
}

func TestCallChain_Depth(t *testing.T) {
	t.Parallel()
	chain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
	}
	assert.Equal(t, 0, chain.Depth(), "Depth() should be 0 for empty callers")

	chain = chain.AppendCaller(CallerInfo{ServiceName: "svc-a"})
	assert.Equal(t, 1, chain.Depth())

	chain = chain.AppendCaller(CallerInfo{ServiceName: "svc-b"})
	assert.Equal(t, 2, chain.Depth())
}

func TestCallChain_AppendCaller_TruncatesAtMaxDepth(t *testing.T) {
	t.Parallel()
	chain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
	}

	// Fill the chain to MaxCallChainDepth.
	for i := 0; i < MaxCallChainDepth; i++ {
		chain = chain.AppendCaller(CallerInfo{ServiceName: fmt.Sprintf("svc-%d", i)})
	}
	require.Equal(t, MaxCallChainDepth, chain.Depth())

	// Adding one more should still result in MaxCallChainDepth entries.
	chain = chain.AppendCaller(CallerInfo{ServiceName: "svc-overflow"})
	require.Equal(t, MaxCallChainDepth, chain.Depth(), "Depth() after overflow")

	// The newest caller should be the last entry.
	last := chain.Callers[MaxCallChainDepth-1]
	assert.Equal(t, "svc-overflow", last.ServiceName)

	// The oldest caller (svc-0) should have been dropped.
	first := chain.Callers[0]
	assert.NotEqual(t, "svc-0", first.ServiceName, "oldest caller svc-0 should have been truncated")
	// The first entry should now be svc-1 (second-oldest from previous chain).
	assert.Equal(t, "svc-1", first.ServiceName)
}

func TestCallChain_MaxCallChainDepth_IsReasonable(t *testing.T) {
	t.Parallel()
	// Verify the constant is a sane value.
	assert.GreaterOrEqual(t, MaxCallChainDepth, 8, "MaxCallChainDepth too small for realistic service meshes")
	assert.LessOrEqual(t, MaxCallChainDepth, 128, "MaxCallChainDepth too large — risks header overflow")
}

func TestCallChain_AppendCaller_Immutable(t *testing.T) {
	t.Parallel()
	original := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "svc-a"},
		},
	}

	extended := original.AppendCaller(CallerInfo{ServiceName: "svc-b"})

	// Original should not be modified.
	assert.Len(t, original.Callers, 1, "immutability violated")

	// Extended should have both callers.
	require.Len(t, extended.Callers, 2)
	assert.Equal(t, "svc-a", extended.Callers[0].ServiceName)
	assert.Equal(t, "svc-b", extended.Callers[1].ServiceName)

	// Original identity info should be preserved.
	assert.Equal(t, "user-1", extended.OriginalID)
	assert.Equal(t, IdentityTypeUser, extended.OriginalType)
}

// ---------------------------------------------------------------------------
// BasicIdentity.HasPermission
// ---------------------------------------------------------------------------

func TestBasicIdentity_HasPermission_AlwaysFalse(t *testing.T) {
	t.Parallel()
	identity := NewBasicIdentity("user-1", IdentityTypeUser, map[string]any{"role": "admin"})

	// BasicIdentity is a transport-level type and should never grant permissions.
	assert.False(t, identity.HasPermission("documents", "read"), "BasicIdentity.HasPermission() should always return false")
	assert.False(t, identity.HasPermission("*", "*"), "BasicIdentity.HasPermission() should return false even for wildcards")
	assert.False(t, identity.HasPermission("", ""), "BasicIdentity.HasPermission() should return false for empty strings")
}

// ---------------------------------------------------------------------------
// ServiceIdentity
// ---------------------------------------------------------------------------

func TestNewServiceIdentity(t *testing.T) {
	t.Parallel()
	claims := map[string]any{"env": "production", "cluster": "us-east-1"}
	perms := []Permission{
		{Resource: "documents", Action: "read"},
		{Resource: "documents", Action: "write"},
	}
	identity := mustNewServiceIdentity(t, "svc-123", "nexus-gateway", "platform", claims, perms)

	assert.Equal(t, "svc-123", identity.ID())
	assert.Equal(t, IdentityTypeService, identity.Type())
	assert.Equal(t, "nexus-gateway", identity.ServiceName())
	assert.Equal(t, "platform", identity.Namespace())
	assert.Len(t, identity.Claims(), 2)
	assert.Equal(t, "production", identity.Claims()["env"])
}

func TestNewServiceIdentity_EmptyID(t *testing.T) {
	t.Parallel()
	_, err := NewServiceIdentity("", "svc", "ns", nil, nil)
	require.Error(t, err, "NewServiceIdentity with empty ID should return an error")
}

func TestNewServiceIdentity_EmptyServiceName(t *testing.T) {
	t.Parallel()
	_, err := NewServiceIdentity("svc-1", "", "ns", nil, nil)
	require.Error(t, err, "NewServiceIdentity with empty serviceName should return an error")
}

func TestNewServiceIdentity_EmptyNamespaceIsAllowed(t *testing.T) {
	t.Parallel()
	// Namespace can be empty (e.g., non-Kubernetes deployments).
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "", nil, nil)
	assert.Equal(t, "", identity.Namespace())
}

func TestNewServiceIdentity_ClaimsDefensivelyCopied(t *testing.T) {
	t.Parallel()
	claims := map[string]any{"key": "original"}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", claims, nil)

	// Mutating the input map should not affect the identity.
	claims["key"] = "mutated"
	claims["injected"] = "value"

	assert.Equal(t, "original", identity.Claims()["key"], "input claims mutation leaked into ServiceIdentity")
	_, exists := identity.Claims()["injected"]
	assert.False(t, exists, "injected claim key leaked into ServiceIdentity")
}

func TestServiceIdentity_ClaimsReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", map[string]any{"key": "original"}, nil)

	first := identity.Claims()
	first["key"] = "mutated"
	first["injected"] = "attack"

	second := identity.Claims()
	assert.Equal(t, "original", second["key"], "Claims() mutation leaked")
	_, exists := second["injected"]
	assert.False(t, exists, "Claims() mutation leaked: injected key should not exist")
}

func TestNewServiceIdentity_PermissionsDefensivelyCopied(t *testing.T) {
	t.Parallel()
	perms := []Permission{{Resource: "docs", Action: "read"}}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	// Mutating the input slice should not affect the identity.
	perms[0] = Permission{Resource: "HACKED", Action: "HACKED"}

	assert.False(t, identity.HasPermission("HACKED", "HACKED"), "input permissions mutation leaked into ServiceIdentity")
	assert.True(t, identity.HasPermission("docs", "read"), "original permission should still be present")
}

func TestNewServiceIdentity_NilClaimsAndPermissions(t *testing.T) {
	t.Parallel()
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, nil)

	assert.NotNil(t, identity.Claims(), "Claims() returned nil, expected empty map")
	assert.Len(t, identity.Claims(), 0)
	assert.False(t, identity.HasPermission("anything", "anything"), "HasPermission should return false with no permissions")
}

func TestServiceIdentity_HasPermission_ExactMatch(t *testing.T) {
	t.Parallel()
	perms := []Permission{
		{Resource: "documents", Action: "read"},
		{Resource: "users", Action: "delete"},
	}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	assert.True(t, identity.HasPermission("documents", "read"), "expected permission for documents:read")
	assert.True(t, identity.HasPermission("users", "delete"), "expected permission for users:delete")
	assert.False(t, identity.HasPermission("documents", "delete"), "should not have permission for documents:delete")
	assert.False(t, identity.HasPermission("users", "read"), "should not have permission for users:read")
}

func TestServiceIdentity_HasPermission_WildcardResource(t *testing.T) {
	t.Parallel()
	perms := []Permission{{Resource: "*", Action: "read"}}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	assert.True(t, identity.HasPermission("documents", "read"), "wildcard resource should match documents:read")
	assert.True(t, identity.HasPermission("users", "read"), "wildcard resource should match users:read")
	assert.False(t, identity.HasPermission("documents", "write"), "wildcard resource should not match documents:write")
}

func TestServiceIdentity_HasPermission_WildcardAction(t *testing.T) {
	t.Parallel()
	perms := []Permission{{Resource: "documents", Action: "*"}}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	assert.True(t, identity.HasPermission("documents", "read"), "wildcard action should match documents:read")
	assert.True(t, identity.HasPermission("documents", "write"), "wildcard action should match documents:write")
	assert.True(t, identity.HasPermission("documents", "delete"), "wildcard action should match documents:delete")
	assert.False(t, identity.HasPermission("users", "read"), "wildcard action should not match users:read")
}

func TestServiceIdentity_HasPermission_FullWildcard(t *testing.T) {
	t.Parallel()
	perms := []Permission{{Resource: "*", Action: "*"}}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	assert.True(t, identity.HasPermission("documents", "read"), "full wildcard should match documents:read")
	assert.True(t, identity.HasPermission("users", "delete"), "full wildcard should match users:delete")
	assert.True(t, identity.HasPermission("anything", "anything"), "full wildcard should match anything:anything")
}

func TestServiceIdentity_HasPermission_NoMatch(t *testing.T) {
	t.Parallel()
	perms := []Permission{{Resource: "documents", Action: "read"}}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	assert.False(t, identity.HasPermission("secrets", "read"), "should not have permission for secrets:read")
	assert.False(t, identity.HasPermission("documents", "execute"), "should not have permission for documents:execute")
}

func TestServiceIdentity_Permissions_ReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()
	perms := []Permission{
		{Resource: "documents", Action: "read"},
		{Resource: "users", Action: "write"},
	}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	got := identity.Permissions()
	require.Len(t, got, 2)
	assert.Equal(t, "documents", got[0].Resource)
	assert.Equal(t, "read", got[0].Action)
	assert.Equal(t, "users", got[1].Resource)
	assert.Equal(t, "write", got[1].Action)

	// Mutating the returned slice should not affect the identity.
	got[0] = Permission{Resource: "HACKED", Action: "HACKED"}
	second := identity.Permissions()
	assert.Equal(t, "documents", second[0].Resource, "Permissions() mutation leaked into ServiceIdentity")
}

func TestServiceIdentity_Permissions_NilPermissions(t *testing.T) {
	t.Parallel()
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, nil)

	got := identity.Permissions()
	assert.NotNil(t, got, "Permissions() returned nil, expected empty slice")
	assert.Len(t, got, 0)
}

// ---------------------------------------------------------------------------
// UserIdentity
// ---------------------------------------------------------------------------

func TestNewUserIdentity(t *testing.T) {
	t.Parallel()
	claims := map[string]any{"org": "stricklysoft", "tier": "enterprise"}
	perms := []Permission{
		{Resource: "projects", Action: "read"},
		{Resource: "projects", Action: "write"},
	}
	identity := mustNewUserIdentity(t, "usr-456", "admin@stricklysoft.io", "Admin User", claims, perms)

	assert.Equal(t, "usr-456", identity.ID())
	assert.Equal(t, IdentityTypeUser, identity.Type())
	assert.Equal(t, "admin@stricklysoft.io", identity.Email())
	assert.Equal(t, "Admin User", identity.DisplayName())
	assert.Len(t, identity.Claims(), 2)
	assert.Equal(t, "stricklysoft", identity.Claims()["org"])
}

func TestNewUserIdentity_EmptyID(t *testing.T) {
	t.Parallel()
	_, err := NewUserIdentity("", "a@b.com", "A", nil, nil)
	require.Error(t, err, "NewUserIdentity with empty ID should return an error")
}

func TestNewUserIdentity_EmptyEmail(t *testing.T) {
	t.Parallel()
	_, err := NewUserIdentity("usr-1", "", "A", nil, nil)
	require.Error(t, err, "NewUserIdentity with empty email should return an error")
}

func TestNewUserIdentity_EmptyDisplayNameIsAllowed(t *testing.T) {
	t.Parallel()
	// Display name can be empty (not all identity providers supply one).
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "", nil, nil)
	assert.Equal(t, "", identity.DisplayName())
}

func TestNewUserIdentity_ClaimsDefensivelyCopied(t *testing.T) {
	t.Parallel()
	claims := map[string]any{"key": "original"}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", claims, nil)

	claims["key"] = "mutated"
	claims["injected"] = "value"

	assert.Equal(t, "original", identity.Claims()["key"], "input claims mutation leaked into UserIdentity")
	_, exists := identity.Claims()["injected"]
	assert.False(t, exists, "injected claim key leaked into UserIdentity")
}

func TestUserIdentity_ClaimsReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", map[string]any{"key": "original"}, nil)

	first := identity.Claims()
	first["key"] = "mutated"
	first["injected"] = "attack"

	second := identity.Claims()
	assert.Equal(t, "original", second["key"], "Claims() mutation leaked")
	_, exists := second["injected"]
	assert.False(t, exists, "Claims() mutation leaked: injected key should not exist")
}

func TestNewUserIdentity_PermissionsDefensivelyCopied(t *testing.T) {
	t.Parallel()
	perms := []Permission{{Resource: "projects", Action: "read"}}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	perms[0] = Permission{Resource: "HACKED", Action: "HACKED"}

	assert.False(t, identity.HasPermission("HACKED", "HACKED"), "input permissions mutation leaked into UserIdentity")
	assert.True(t, identity.HasPermission("projects", "read"), "original permission should still be present")
}

func TestNewUserIdentity_NilClaimsAndPermissions(t *testing.T) {
	t.Parallel()
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, nil)

	assert.NotNil(t, identity.Claims(), "Claims() returned nil, expected empty map")
	assert.Len(t, identity.Claims(), 0)
	assert.False(t, identity.HasPermission("anything", "anything"), "HasPermission should return false with no permissions")
}

func TestUserIdentity_HasPermission_ExactMatch(t *testing.T) {
	t.Parallel()
	perms := []Permission{
		{Resource: "projects", Action: "read"},
		{Resource: "reports", Action: "generate"},
	}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	assert.True(t, identity.HasPermission("projects", "read"), "expected permission for projects:read")
	assert.True(t, identity.HasPermission("reports", "generate"), "expected permission for reports:generate")
	assert.False(t, identity.HasPermission("projects", "delete"), "should not have permission for projects:delete")
}

func TestUserIdentity_HasPermission_WildcardResource(t *testing.T) {
	t.Parallel()
	perms := []Permission{{Resource: "*", Action: "read"}}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	assert.True(t, identity.HasPermission("projects", "read"), "wildcard resource should match projects:read")
	assert.True(t, identity.HasPermission("users", "read"), "wildcard resource should match users:read")
	assert.False(t, identity.HasPermission("projects", "write"), "wildcard resource should not match projects:write")
}

func TestUserIdentity_HasPermission_WildcardAction(t *testing.T) {
	t.Parallel()
	perms := []Permission{{Resource: "projects", Action: "*"}}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	assert.True(t, identity.HasPermission("projects", "read"), "wildcard action should match projects:read")
	assert.True(t, identity.HasPermission("projects", "delete"), "wildcard action should match projects:delete")
	assert.False(t, identity.HasPermission("users", "read"), "wildcard action should not match users:read")
}

func TestUserIdentity_HasPermission_FullWildcard(t *testing.T) {
	t.Parallel()
	perms := []Permission{{Resource: "*", Action: "*"}}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	assert.True(t, identity.HasPermission("anything", "anything"), "full wildcard should match anything:anything")
}

func TestUserIdentity_HasPermission_NoMatch(t *testing.T) {
	t.Parallel()
	perms := []Permission{{Resource: "projects", Action: "read"}}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	assert.False(t, identity.HasPermission("secrets", "read"), "should not have permission for secrets:read")
}

func TestUserIdentity_HasPermission_MultiplePermissions(t *testing.T) {
	t.Parallel()
	perms := []Permission{
		{Resource: "projects", Action: "read"},
		{Resource: "projects", Action: "write"},
		{Resource: "users", Action: "read"},
		{Resource: "agents", Action: "*"},
	}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	assert.True(t, identity.HasPermission("projects", "read"), "expected projects:read")
	assert.True(t, identity.HasPermission("projects", "write"), "expected projects:write")
	assert.True(t, identity.HasPermission("users", "read"), "expected users:read")
	assert.True(t, identity.HasPermission("agents", "execute"), "expected agents:execute via wildcard action")
	assert.False(t, identity.HasPermission("users", "delete"), "should not have users:delete")
	assert.False(t, identity.HasPermission("secrets", "read"), "should not have secrets:read")
}

func TestUserIdentity_Permissions_ReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()
	perms := []Permission{
		{Resource: "projects", Action: "read"},
		{Resource: "reports", Action: "generate"},
	}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	got := identity.Permissions()
	require.Len(t, got, 2)
	assert.Equal(t, "projects", got[0].Resource)
	assert.Equal(t, "read", got[0].Action)
	assert.Equal(t, "reports", got[1].Resource)
	assert.Equal(t, "generate", got[1].Action)

	// Mutating the returned slice should not affect the identity.
	got[0] = Permission{Resource: "HACKED", Action: "HACKED"}
	second := identity.Permissions()
	assert.Equal(t, "projects", second[0].Resource, "Permissions() mutation leaked into UserIdentity")
}

func TestUserIdentity_Permissions_NilPermissions(t *testing.T) {
	t.Parallel()
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, nil)

	got := identity.Permissions()
	assert.NotNil(t, got, "Permissions() returned nil, expected empty slice")
	assert.Len(t, got, 0)
}

// ---------------------------------------------------------------------------
// Permission.Match — comprehensive wildcard and scope combinations
// ---------------------------------------------------------------------------

func TestPermission_Match_ExactMatch(t *testing.T) {
	t.Parallel()
	p := Permission{Resource: "documents", Action: "read"}

	assert.True(t, p.Match("documents", "read", ""), "exact resource + action, empty scope should match")
	assert.True(t, p.Match("documents", "read", "production"), "exact resource + action, specific scope should match (perm scope is empty = global)")
	assert.False(t, p.Match("users", "read", ""), "wrong resource should not match")
	assert.False(t, p.Match("documents", "write", ""), "wrong action should not match")
}

func TestPermission_Match_WildcardResource(t *testing.T) {
	t.Parallel()
	p := Permission{Resource: "*", Action: "read"}

	assert.True(t, p.Match("documents", "read", ""), "wildcard resource should match any resource")
	assert.True(t, p.Match("users", "read", ""), "wildcard resource should match any resource")
	assert.True(t, p.Match("anything", "read", "production"), "wildcard resource + global scope should match with specific scope check")
	assert.False(t, p.Match("documents", "write", ""), "wildcard resource should not match wrong action")
}

func TestPermission_Match_WildcardAction(t *testing.T) {
	t.Parallel()
	p := Permission{Resource: "documents", Action: "*"}

	assert.True(t, p.Match("documents", "read", ""), "wildcard action should match any action")
	assert.True(t, p.Match("documents", "write", ""), "wildcard action should match any action")
	assert.True(t, p.Match("documents", "delete", "staging"), "wildcard action + global scope should match specific scope check")
	assert.False(t, p.Match("users", "read", ""), "wildcard action should not match wrong resource")
}

func TestPermission_Match_FullWildcard(t *testing.T) {
	t.Parallel()
	p := Permission{Resource: "*", Action: "*"}

	assert.True(t, p.Match("anything", "anything", ""), "full wildcard should match anything")
	assert.True(t, p.Match("documents", "read", "production"), "full wildcard should match specific scope")
	assert.True(t, p.Match("", "", ""), "full wildcard should match empty strings")
}

func TestPermission_Match_ScopedPermission_ExactScope(t *testing.T) {
	t.Parallel()
	p := Permission{Resource: "deployments", Action: "write", Scope: "production"}

	assert.True(t, p.Match("deployments", "write", "production"), "exact scope match should succeed")
	assert.False(t, p.Match("deployments", "write", "staging"), "different scope should not match")
	assert.True(t, p.Match("deployments", "write", ""), "empty check scope should match any perm scope")
	assert.True(t, p.Match("deployments", "write", "*"), "wildcard check scope should match any perm scope")
}

func TestPermission_Match_ScopedPermission_WildcardScope(t *testing.T) {
	t.Parallel()
	p := Permission{Resource: "agents", Action: "execute", Scope: "*"}

	assert.True(t, p.Match("agents", "execute", "production"), "wildcard scope perm should match any scope")
	assert.True(t, p.Match("agents", "execute", "staging"), "wildcard scope perm should match any scope")
	assert.True(t, p.Match("agents", "execute", ""), "wildcard scope perm should match empty scope")
	assert.False(t, p.Match("agents", "read", "production"), "wildcard scope should not bypass action check")
}

func TestPermission_Match_GlobalPermission_MatchesAnyScope(t *testing.T) {
	t.Parallel()
	// Global permission (Scope="") should match any check scope.
	p := Permission{Resource: "logs", Action: "read"}

	assert.True(t, p.Match("logs", "read", ""), "global perm + empty scope check")
	assert.True(t, p.Match("logs", "read", "production"), "global perm + specific scope check")
	assert.True(t, p.Match("logs", "read", "*"), "global perm + wildcard scope check")
}

func TestPermission_Match_EmptyCheckScope_MatchesAnyPermScope(t *testing.T) {
	t.Parallel()
	// Empty check scope ("") should match any permission scope.
	// This is the key backward-compatibility property.
	tests := []struct {
		name      string
		permScope string
	}{
		{name: "global perm", permScope: ""},
		{name: "wildcard perm", permScope: "*"},
		{name: "production perm", permScope: "production"},
		{name: "staging perm", permScope: "staging"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := Permission{Resource: "docs", Action: "read", Scope: tt.permScope}
			assert.True(t, p.Match("docs", "read", ""),
				"empty check scope should match permission with Scope=%q", tt.permScope)
		})
	}
}

func TestPermission_Match_WildcardCheckScope_MatchesAnyPermScope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		permScope string
	}{
		{name: "global perm", permScope: ""},
		{name: "wildcard perm", permScope: "*"},
		{name: "production perm", permScope: "production"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := Permission{Resource: "docs", Action: "read", Scope: tt.permScope}
			assert.True(t, p.Match("docs", "read", "*"),
				"wildcard check scope should match permission with Scope=%q", tt.permScope)
		})
	}
}

func TestPermission_Match_ScopeMismatch(t *testing.T) {
	t.Parallel()
	p := Permission{Resource: "secrets", Action: "read", Scope: "production"}

	assert.False(t, p.Match("secrets", "read", "staging"), "production scope should not match staging")
	assert.False(t, p.Match("secrets", "read", "development"), "production scope should not match development")
}

func TestPermission_Match_AllWildcards(t *testing.T) {
	t.Parallel()
	p := Permission{Resource: "*", Action: "*", Scope: "*"}

	assert.True(t, p.Match("any", "thing", "anywhere"), "all wildcards should match everything")
	assert.True(t, p.Match("", "", ""), "all wildcards should match empty strings")
}

func TestPermission_Match_MixedWildcards(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		perm     Permission
		resource string
		action   string
		scope    string
		want     bool
	}{
		{
			name:     "wildcard resource + exact action + exact scope",
			perm:     Permission{Resource: "*", Action: "read", Scope: "production"},
			resource: "docs", action: "read", scope: "production",
			want: true,
		},
		{
			name:     "wildcard resource + exact action + wrong scope",
			perm:     Permission{Resource: "*", Action: "read", Scope: "production"},
			resource: "docs", action: "read", scope: "staging",
			want: false,
		},
		{
			name:     "exact resource + wildcard action + wildcard scope",
			perm:     Permission{Resource: "agents", Action: "*", Scope: "*"},
			resource: "agents", action: "delete", scope: "any-env",
			want: true,
		},
		{
			name:     "exact resource + wildcard action + wrong resource",
			perm:     Permission{Resource: "agents", Action: "*", Scope: "*"},
			resource: "users", action: "delete", scope: "production",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.perm.Match(tt.resource, tt.action, tt.scope)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Permission.String
// ---------------------------------------------------------------------------

func TestPermission_String_TwoPart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		perm Permission
		want string
	}{
		{name: "simple", perm: Permission{Resource: "documents", Action: "read"}, want: "documents:read"},
		{name: "wildcards", perm: Permission{Resource: "*", Action: "*"}, want: "*:*"},
		{name: "empty scope", perm: Permission{Resource: "agents", Action: "execute", Scope: ""}, want: "agents:execute"},
		{name: "wildcard scope", perm: Permission{Resource: "logs", Action: "read", Scope: "*"}, want: "logs:read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.perm.String())
		})
	}
}

func TestPermission_String_ThreePart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		perm Permission
		want string
	}{
		{
			name: "specific scope",
			perm: Permission{Resource: "deployments", Action: "write", Scope: "production"},
			want: "deployments:write:production",
		},
		{
			name: "staging scope",
			perm: Permission{Resource: "agents", Action: "execute", Scope: "staging"},
			want: "agents:execute:staging",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.perm.String())
		})
	}
}

// ---------------------------------------------------------------------------
// HasPermission with scoped permissions — backward compatibility
// ---------------------------------------------------------------------------

func TestHasPermission_WithScopedPermissions_BackwardCompat(t *testing.T) {
	t.Parallel()
	// Scoped permissions should still match when HasPermission is called
	// (2-arg form passes scope="" internally, which matches any perm scope).
	perms := []Permission{
		{Resource: "documents", Action: "read", Scope: "production"},
		{Resource: "agents", Action: "execute", Scope: "staging"},
		{Resource: "logs", Action: "read"}, // global
	}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	// All permissions should match via 2-arg HasPermission (scope-unaware).
	assert.True(t, identity.HasPermission("documents", "read"),
		"scoped permission should match via 2-arg HasPermission")
	assert.True(t, identity.HasPermission("agents", "execute"),
		"scoped permission should match via 2-arg HasPermission")
	assert.True(t, identity.HasPermission("logs", "read"),
		"global permission should match via 2-arg HasPermission")

	// Non-existent permissions should still fail.
	assert.False(t, identity.HasPermission("secrets", "read"))
	assert.False(t, identity.HasPermission("documents", "delete"))
}

func TestHasPermission_WithScopedPermissions_UserIdentity(t *testing.T) {
	t.Parallel()
	perms := []Permission{
		{Resource: "projects", Action: "read", Scope: "tenant-a"},
		{Resource: "*", Action: "read", Scope: "tenant-b"},
	}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	// Both should match via scope-unaware HasPermission.
	assert.True(t, identity.HasPermission("projects", "read"))
	assert.True(t, identity.HasPermission("users", "read"),
		"wildcard resource with scoped perm should still match via 2-arg HasPermission")
	assert.False(t, identity.HasPermission("projects", "write"))
}
