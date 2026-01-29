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
