package auth

import (
	"fmt"
	"testing"
)

// mustNewServiceIdentity creates a ServiceIdentity, failing the test if
// construction returns an error. Use this in tests where valid inputs
// are expected.
func mustNewServiceIdentity(t *testing.T, id, serviceName, namespace string, claims map[string]any, permissions []Permission) *ServiceIdentity {
	t.Helper()
	identity, err := NewServiceIdentity(id, serviceName, namespace, claims, permissions)
	if err != nil {
		t.Fatalf("NewServiceIdentity(%q, %q, %q, ...) unexpected error: %v", id, serviceName, namespace, err)
	}
	return identity
}

// mustNewUserIdentity creates a UserIdentity, failing the test if
// construction returns an error. Use this in tests where valid inputs
// are expected.
func mustNewUserIdentity(t *testing.T, id, email, displayName string, claims map[string]any, permissions []Permission) *UserIdentity {
	t.Helper()
	identity, err := NewUserIdentity(id, email, displayName, claims, permissions)
	if err != nil {
		t.Fatalf("NewUserIdentity(%q, %q, %q, ...) unexpected error: %v", id, email, displayName, err)
	}
	return identity
}

func TestIdentityType_String(t *testing.T) {
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
			if got := tt.idType.String(); got != tt.expected {
				t.Errorf("IdentityType.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIdentityType_Valid(t *testing.T) {
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
			if got := tt.idType.Valid(); got != tt.expected {
				t.Errorf("IdentityType.Valid() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNewBasicIdentity(t *testing.T) {
	claims := map[string]any{"email": "user@example.com", "role": "admin"}
	identity := NewBasicIdentity("user-123", IdentityTypeUser, claims)

	if identity.ID() != "user-123" {
		t.Errorf("ID() = %q, want %q", identity.ID(), "user-123")
	}
	if identity.Type() != IdentityTypeUser {
		t.Errorf("Type() = %q, want %q", identity.Type(), IdentityTypeUser)
	}
	if len(identity.Claims()) != 2 {
		t.Errorf("Claims() has %d entries, want 2", len(identity.Claims()))
	}
	if identity.Claims()["email"] != "user@example.com" {
		t.Errorf("Claims()[email] = %q, want %q", identity.Claims()["email"], "user@example.com")
	}
}

func TestNewBasicIdentity_ClaimsAreCopied(t *testing.T) {
	claims := map[string]any{"key": "original"}
	identity := NewBasicIdentity("id", IdentityTypeUser, claims)

	// Mutating the original map should not affect the identity.
	claims["key"] = "mutated"
	claims["new_key"] = "new_value"

	if identity.Claims()["key"] != "original" {
		t.Error("claims mutation leaked into identity; expected defensive copy")
	}
	if _, exists := identity.Claims()["new_key"]; exists {
		t.Error("new claim key leaked into identity; expected defensive copy")
	}
}

func TestNewBasicIdentity_NilClaims(t *testing.T) {
	identity := NewBasicIdentity("id", IdentityTypeService, nil)

	if identity.Claims() == nil {
		t.Error("Claims() returned nil, expected empty map")
	}
	if len(identity.Claims()) != 0 {
		t.Errorf("Claims() has %d entries, want 0", len(identity.Claims()))
	}
}

// Verify concrete types implement the Identity interface at compile time.
var _ Identity = (*BasicIdentity)(nil)
var _ Identity = (*ServiceIdentity)(nil)
var _ Identity = (*UserIdentity)(nil)

func TestBasicIdentity_ClaimsReturnsDefensiveCopy(t *testing.T) {
	identity := NewBasicIdentity("user-1", IdentityTypeUser, map[string]any{"key": "original"})

	// Mutating the returned map should not affect subsequent calls.
	first := identity.Claims()
	first["key"] = "mutated"
	first["injected"] = "attack"

	second := identity.Claims()
	if second["key"] != "original" {
		t.Errorf("Claims() mutation leaked: key = %q, want %q", second["key"], "original")
	}
	if _, exists := second["injected"]; exists {
		t.Error("Claims() mutation leaked: injected key should not exist")
	}
}

func TestCallChain_Depth(t *testing.T) {
	chain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
	}
	if chain.Depth() != 0 {
		t.Errorf("Depth() = %d, want 0 for empty callers", chain.Depth())
	}

	chain = chain.AppendCaller(CallerInfo{ServiceName: "svc-a"})
	if chain.Depth() != 1 {
		t.Errorf("Depth() = %d, want 1", chain.Depth())
	}

	chain = chain.AppendCaller(CallerInfo{ServiceName: "svc-b"})
	if chain.Depth() != 2 {
		t.Errorf("Depth() = %d, want 2", chain.Depth())
	}
}

func TestCallChain_AppendCaller_TruncatesAtMaxDepth(t *testing.T) {
	chain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
	}

	// Fill the chain to MaxCallChainDepth.
	for i := 0; i < MaxCallChainDepth; i++ {
		chain = chain.AppendCaller(CallerInfo{ServiceName: fmt.Sprintf("svc-%d", i)})
	}
	if chain.Depth() != MaxCallChainDepth {
		t.Fatalf("Depth() = %d, want %d", chain.Depth(), MaxCallChainDepth)
	}

	// Adding one more should still result in MaxCallChainDepth entries.
	chain = chain.AppendCaller(CallerInfo{ServiceName: "svc-overflow"})
	if chain.Depth() != MaxCallChainDepth {
		t.Fatalf("Depth() = %d after overflow, want %d", chain.Depth(), MaxCallChainDepth)
	}

	// The newest caller should be the last entry.
	last := chain.Callers[MaxCallChainDepth-1]
	if last.ServiceName != "svc-overflow" {
		t.Errorf("last caller = %q, want %q", last.ServiceName, "svc-overflow")
	}

	// The oldest caller (svc-0) should have been dropped.
	first := chain.Callers[0]
	if first.ServiceName == "svc-0" {
		t.Error("oldest caller svc-0 should have been truncated")
	}
	// The first entry should now be svc-1 (second-oldest from previous chain).
	if first.ServiceName != "svc-1" {
		t.Errorf("first caller = %q, want %q", first.ServiceName, "svc-1")
	}
}

func TestCallChain_MaxCallChainDepth_IsReasonable(t *testing.T) {
	// Verify the constant is a sane value.
	if MaxCallChainDepth < 8 {
		t.Errorf("MaxCallChainDepth = %d, too small for realistic service meshes", MaxCallChainDepth)
	}
	if MaxCallChainDepth > 128 {
		t.Errorf("MaxCallChainDepth = %d, too large — risks header overflow", MaxCallChainDepth)
	}
}

func TestCallChain_AppendCaller_Immutable(t *testing.T) {
	original := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "svc-a"},
		},
	}

	extended := original.AppendCaller(CallerInfo{ServiceName: "svc-b"})

	// Original should not be modified.
	if len(original.Callers) != 1 {
		t.Errorf("original.Callers has %d entries, want 1 (immutability violated)", len(original.Callers))
	}

	// Extended should have both callers.
	if len(extended.Callers) != 2 {
		t.Errorf("extended.Callers has %d entries, want 2", len(extended.Callers))
	}
	if extended.Callers[0].ServiceName != "svc-a" {
		t.Errorf("extended.Callers[0].ServiceName = %q, want %q", extended.Callers[0].ServiceName, "svc-a")
	}
	if extended.Callers[1].ServiceName != "svc-b" {
		t.Errorf("extended.Callers[1].ServiceName = %q, want %q", extended.Callers[1].ServiceName, "svc-b")
	}

	// Original identity info should be preserved.
	if extended.OriginalID != "user-1" {
		t.Errorf("extended.OriginalID = %q, want %q", extended.OriginalID, "user-1")
	}
	if extended.OriginalType != IdentityTypeUser {
		t.Errorf("extended.OriginalType = %q, want %q", extended.OriginalType, IdentityTypeUser)
	}
}

// ---------------------------------------------------------------------------
// BasicIdentity.HasPermission
// ---------------------------------------------------------------------------

func TestBasicIdentity_HasPermission_AlwaysFalse(t *testing.T) {
	identity := NewBasicIdentity("user-1", IdentityTypeUser, map[string]any{"role": "admin"})

	// BasicIdentity is a transport-level type and should never grant permissions.
	if identity.HasPermission("documents", "read") {
		t.Error("BasicIdentity.HasPermission() should always return false")
	}
	if identity.HasPermission("*", "*") {
		t.Error("BasicIdentity.HasPermission() should return false even for wildcards")
	}
	if identity.HasPermission("", "") {
		t.Error("BasicIdentity.HasPermission() should return false for empty strings")
	}
}

// ---------------------------------------------------------------------------
// ServiceIdentity
// ---------------------------------------------------------------------------

func TestNewServiceIdentity(t *testing.T) {
	claims := map[string]any{"env": "production", "cluster": "us-east-1"}
	perms := []Permission{
		{Resource: "documents", Action: "read"},
		{Resource: "documents", Action: "write"},
	}
	identity := mustNewServiceIdentity(t, "svc-123", "nexus-gateway", "platform", claims, perms)

	if identity.ID() != "svc-123" {
		t.Errorf("ID() = %q, want %q", identity.ID(), "svc-123")
	}
	if identity.Type() != IdentityTypeService {
		t.Errorf("Type() = %q, want %q", identity.Type(), IdentityTypeService)
	}
	if identity.ServiceName() != "nexus-gateway" {
		t.Errorf("ServiceName() = %q, want %q", identity.ServiceName(), "nexus-gateway")
	}
	if identity.Namespace() != "platform" {
		t.Errorf("Namespace() = %q, want %q", identity.Namespace(), "platform")
	}
	if len(identity.Claims()) != 2 {
		t.Errorf("Claims() has %d entries, want 2", len(identity.Claims()))
	}
	if identity.Claims()["env"] != "production" {
		t.Errorf("Claims()[env] = %v, want %q", identity.Claims()["env"], "production")
	}
}

func TestNewServiceIdentity_EmptyID(t *testing.T) {
	_, err := NewServiceIdentity("", "svc", "ns", nil, nil)
	if err == nil {
		t.Fatal("NewServiceIdentity with empty ID should return an error")
	}
}

func TestNewServiceIdentity_EmptyServiceName(t *testing.T) {
	_, err := NewServiceIdentity("svc-1", "", "ns", nil, nil)
	if err == nil {
		t.Fatal("NewServiceIdentity with empty serviceName should return an error")
	}
}

func TestNewServiceIdentity_EmptyNamespaceIsAllowed(t *testing.T) {
	// Namespace can be empty (e.g., non-Kubernetes deployments).
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "", nil, nil)
	if identity.Namespace() != "" {
		t.Errorf("Namespace() = %q, want empty string", identity.Namespace())
	}
}

func TestNewServiceIdentity_ClaimsDefensivelyCopied(t *testing.T) {
	claims := map[string]any{"key": "original"}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", claims, nil)

	// Mutating the input map should not affect the identity.
	claims["key"] = "mutated"
	claims["injected"] = "value"

	if identity.Claims()["key"] != "original" {
		t.Error("input claims mutation leaked into ServiceIdentity")
	}
	if _, exists := identity.Claims()["injected"]; exists {
		t.Error("injected claim key leaked into ServiceIdentity")
	}
}

func TestServiceIdentity_ClaimsReturnsDefensiveCopy(t *testing.T) {
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", map[string]any{"key": "original"}, nil)

	first := identity.Claims()
	first["key"] = "mutated"
	first["injected"] = "attack"

	second := identity.Claims()
	if second["key"] != "original" {
		t.Errorf("Claims() mutation leaked: key = %q, want %q", second["key"], "original")
	}
	if _, exists := second["injected"]; exists {
		t.Error("Claims() mutation leaked: injected key should not exist")
	}
}

func TestNewServiceIdentity_PermissionsDefensivelyCopied(t *testing.T) {
	perms := []Permission{{Resource: "docs", Action: "read"}}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	// Mutating the input slice should not affect the identity.
	perms[0] = Permission{Resource: "HACKED", Action: "HACKED"}

	if identity.HasPermission("HACKED", "HACKED") {
		t.Error("input permissions mutation leaked into ServiceIdentity")
	}
	if !identity.HasPermission("docs", "read") {
		t.Error("original permission should still be present")
	}
}

func TestNewServiceIdentity_NilClaimsAndPermissions(t *testing.T) {
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, nil)

	if identity.Claims() == nil {
		t.Error("Claims() returned nil, expected empty map")
	}
	if len(identity.Claims()) != 0 {
		t.Errorf("Claims() has %d entries, want 0", len(identity.Claims()))
	}
	if identity.HasPermission("anything", "anything") {
		t.Error("HasPermission should return false with no permissions")
	}
}

func TestServiceIdentity_HasPermission_ExactMatch(t *testing.T) {
	perms := []Permission{
		{Resource: "documents", Action: "read"},
		{Resource: "users", Action: "delete"},
	}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	if !identity.HasPermission("documents", "read") {
		t.Error("expected permission for documents:read")
	}
	if !identity.HasPermission("users", "delete") {
		t.Error("expected permission for users:delete")
	}
	if identity.HasPermission("documents", "delete") {
		t.Error("should not have permission for documents:delete")
	}
	if identity.HasPermission("users", "read") {
		t.Error("should not have permission for users:read")
	}
}

func TestServiceIdentity_HasPermission_WildcardResource(t *testing.T) {
	perms := []Permission{{Resource: "*", Action: "read"}}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	if !identity.HasPermission("documents", "read") {
		t.Error("wildcard resource should match documents:read")
	}
	if !identity.HasPermission("users", "read") {
		t.Error("wildcard resource should match users:read")
	}
	if identity.HasPermission("documents", "write") {
		t.Error("wildcard resource should not match documents:write")
	}
}

func TestServiceIdentity_HasPermission_WildcardAction(t *testing.T) {
	perms := []Permission{{Resource: "documents", Action: "*"}}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	if !identity.HasPermission("documents", "read") {
		t.Error("wildcard action should match documents:read")
	}
	if !identity.HasPermission("documents", "write") {
		t.Error("wildcard action should match documents:write")
	}
	if !identity.HasPermission("documents", "delete") {
		t.Error("wildcard action should match documents:delete")
	}
	if identity.HasPermission("users", "read") {
		t.Error("wildcard action should not match users:read")
	}
}

func TestServiceIdentity_HasPermission_FullWildcard(t *testing.T) {
	perms := []Permission{{Resource: "*", Action: "*"}}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	if !identity.HasPermission("documents", "read") {
		t.Error("full wildcard should match documents:read")
	}
	if !identity.HasPermission("users", "delete") {
		t.Error("full wildcard should match users:delete")
	}
	if !identity.HasPermission("anything", "anything") {
		t.Error("full wildcard should match anything:anything")
	}
}

func TestServiceIdentity_HasPermission_NoMatch(t *testing.T) {
	perms := []Permission{{Resource: "documents", Action: "read"}}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	if identity.HasPermission("secrets", "read") {
		t.Error("should not have permission for secrets:read")
	}
	if identity.HasPermission("documents", "execute") {
		t.Error("should not have permission for documents:execute")
	}
}

func TestServiceIdentity_Permissions_ReturnsDefensiveCopy(t *testing.T) {
	perms := []Permission{
		{Resource: "documents", Action: "read"},
		{Resource: "users", Action: "write"},
	}
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, perms)

	got := identity.Permissions()
	if len(got) != 2 {
		t.Fatalf("Permissions() returned %d entries, want 2", len(got))
	}
	if got[0].Resource != "documents" || got[0].Action != "read" {
		t.Errorf("Permissions()[0] = %+v, want {documents read}", got[0])
	}
	if got[1].Resource != "users" || got[1].Action != "write" {
		t.Errorf("Permissions()[1] = %+v, want {users write}", got[1])
	}

	// Mutating the returned slice should not affect the identity.
	got[0] = Permission{Resource: "HACKED", Action: "HACKED"}
	second := identity.Permissions()
	if second[0].Resource != "documents" {
		t.Error("Permissions() mutation leaked into ServiceIdentity")
	}
}

func TestServiceIdentity_Permissions_NilPermissions(t *testing.T) {
	identity := mustNewServiceIdentity(t, "svc-1", "svc", "ns", nil, nil)

	got := identity.Permissions()
	if got == nil {
		t.Error("Permissions() returned nil, expected empty slice")
	}
	if len(got) != 0 {
		t.Errorf("Permissions() returned %d entries, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// UserIdentity
// ---------------------------------------------------------------------------

func TestNewUserIdentity(t *testing.T) {
	claims := map[string]any{"org": "stricklysoft", "tier": "enterprise"}
	perms := []Permission{
		{Resource: "projects", Action: "read"},
		{Resource: "projects", Action: "write"},
	}
	identity := mustNewUserIdentity(t, "usr-456", "admin@stricklysoft.io", "Admin User", claims, perms)

	if identity.ID() != "usr-456" {
		t.Errorf("ID() = %q, want %q", identity.ID(), "usr-456")
	}
	if identity.Type() != IdentityTypeUser {
		t.Errorf("Type() = %q, want %q", identity.Type(), IdentityTypeUser)
	}
	if identity.Email() != "admin@stricklysoft.io" {
		t.Errorf("Email() = %q, want %q", identity.Email(), "admin@stricklysoft.io")
	}
	if identity.DisplayName() != "Admin User" {
		t.Errorf("DisplayName() = %q, want %q", identity.DisplayName(), "Admin User")
	}
	if len(identity.Claims()) != 2 {
		t.Errorf("Claims() has %d entries, want 2", len(identity.Claims()))
	}
	if identity.Claims()["org"] != "stricklysoft" {
		t.Errorf("Claims()[org] = %v, want %q", identity.Claims()["org"], "stricklysoft")
	}
}

func TestNewUserIdentity_EmptyID(t *testing.T) {
	_, err := NewUserIdentity("", "a@b.com", "A", nil, nil)
	if err == nil {
		t.Fatal("NewUserIdentity with empty ID should return an error")
	}
}

func TestNewUserIdentity_EmptyEmail(t *testing.T) {
	_, err := NewUserIdentity("usr-1", "", "A", nil, nil)
	if err == nil {
		t.Fatal("NewUserIdentity with empty email should return an error")
	}
}

func TestNewUserIdentity_EmptyDisplayNameIsAllowed(t *testing.T) {
	// Display name can be empty (not all identity providers supply one).
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "", nil, nil)
	if identity.DisplayName() != "" {
		t.Errorf("DisplayName() = %q, want empty string", identity.DisplayName())
	}
}

func TestNewUserIdentity_ClaimsDefensivelyCopied(t *testing.T) {
	claims := map[string]any{"key": "original"}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", claims, nil)

	claims["key"] = "mutated"
	claims["injected"] = "value"

	if identity.Claims()["key"] != "original" {
		t.Error("input claims mutation leaked into UserIdentity")
	}
	if _, exists := identity.Claims()["injected"]; exists {
		t.Error("injected claim key leaked into UserIdentity")
	}
}

func TestUserIdentity_ClaimsReturnsDefensiveCopy(t *testing.T) {
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", map[string]any{"key": "original"}, nil)

	first := identity.Claims()
	first["key"] = "mutated"
	first["injected"] = "attack"

	second := identity.Claims()
	if second["key"] != "original" {
		t.Errorf("Claims() mutation leaked: key = %q, want %q", second["key"], "original")
	}
	if _, exists := second["injected"]; exists {
		t.Error("Claims() mutation leaked: injected key should not exist")
	}
}

func TestNewUserIdentity_PermissionsDefensivelyCopied(t *testing.T) {
	perms := []Permission{{Resource: "projects", Action: "read"}}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	perms[0] = Permission{Resource: "HACKED", Action: "HACKED"}

	if identity.HasPermission("HACKED", "HACKED") {
		t.Error("input permissions mutation leaked into UserIdentity")
	}
	if !identity.HasPermission("projects", "read") {
		t.Error("original permission should still be present")
	}
}

func TestNewUserIdentity_NilClaimsAndPermissions(t *testing.T) {
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, nil)

	if identity.Claims() == nil {
		t.Error("Claims() returned nil, expected empty map")
	}
	if len(identity.Claims()) != 0 {
		t.Errorf("Claims() has %d entries, want 0", len(identity.Claims()))
	}
	if identity.HasPermission("anything", "anything") {
		t.Error("HasPermission should return false with no permissions")
	}
}

func TestUserIdentity_HasPermission_ExactMatch(t *testing.T) {
	perms := []Permission{
		{Resource: "projects", Action: "read"},
		{Resource: "reports", Action: "generate"},
	}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	if !identity.HasPermission("projects", "read") {
		t.Error("expected permission for projects:read")
	}
	if !identity.HasPermission("reports", "generate") {
		t.Error("expected permission for reports:generate")
	}
	if identity.HasPermission("projects", "delete") {
		t.Error("should not have permission for projects:delete")
	}
}

func TestUserIdentity_HasPermission_WildcardResource(t *testing.T) {
	perms := []Permission{{Resource: "*", Action: "read"}}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	if !identity.HasPermission("projects", "read") {
		t.Error("wildcard resource should match projects:read")
	}
	if !identity.HasPermission("users", "read") {
		t.Error("wildcard resource should match users:read")
	}
	if identity.HasPermission("projects", "write") {
		t.Error("wildcard resource should not match projects:write")
	}
}

func TestUserIdentity_HasPermission_WildcardAction(t *testing.T) {
	perms := []Permission{{Resource: "projects", Action: "*"}}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	if !identity.HasPermission("projects", "read") {
		t.Error("wildcard action should match projects:read")
	}
	if !identity.HasPermission("projects", "delete") {
		t.Error("wildcard action should match projects:delete")
	}
	if identity.HasPermission("users", "read") {
		t.Error("wildcard action should not match users:read")
	}
}

func TestUserIdentity_HasPermission_FullWildcard(t *testing.T) {
	perms := []Permission{{Resource: "*", Action: "*"}}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	if !identity.HasPermission("anything", "anything") {
		t.Error("full wildcard should match anything:anything")
	}
}

func TestUserIdentity_HasPermission_NoMatch(t *testing.T) {
	perms := []Permission{{Resource: "projects", Action: "read"}}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	if identity.HasPermission("secrets", "read") {
		t.Error("should not have permission for secrets:read")
	}
}

func TestUserIdentity_HasPermission_MultiplePermissions(t *testing.T) {
	perms := []Permission{
		{Resource: "projects", Action: "read"},
		{Resource: "projects", Action: "write"},
		{Resource: "users", Action: "read"},
		{Resource: "agents", Action: "*"},
	}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	if !identity.HasPermission("projects", "read") {
		t.Error("expected projects:read")
	}
	if !identity.HasPermission("projects", "write") {
		t.Error("expected projects:write")
	}
	if !identity.HasPermission("users", "read") {
		t.Error("expected users:read")
	}
	if !identity.HasPermission("agents", "execute") {
		t.Error("expected agents:execute via wildcard action")
	}
	if identity.HasPermission("users", "delete") {
		t.Error("should not have users:delete")
	}
	if identity.HasPermission("secrets", "read") {
		t.Error("should not have secrets:read")
	}
}

func TestUserIdentity_Permissions_ReturnsDefensiveCopy(t *testing.T) {
	perms := []Permission{
		{Resource: "projects", Action: "read"},
		{Resource: "reports", Action: "generate"},
	}
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, perms)

	got := identity.Permissions()
	if len(got) != 2 {
		t.Fatalf("Permissions() returned %d entries, want 2", len(got))
	}
	if got[0].Resource != "projects" || got[0].Action != "read" {
		t.Errorf("Permissions()[0] = %+v, want {projects read}", got[0])
	}
	if got[1].Resource != "reports" || got[1].Action != "generate" {
		t.Errorf("Permissions()[1] = %+v, want {reports generate}", got[1])
	}

	// Mutating the returned slice should not affect the identity.
	got[0] = Permission{Resource: "HACKED", Action: "HACKED"}
	second := identity.Permissions()
	if second[0].Resource != "projects" {
		t.Error("Permissions() mutation leaked into UserIdentity")
	}
}

func TestUserIdentity_Permissions_NilPermissions(t *testing.T) {
	identity := mustNewUserIdentity(t, "usr-1", "a@b.com", "A", nil, nil)

	got := identity.Permissions()
	if got == nil {
		t.Error("Permissions() returned nil, expected empty slice")
	}
	if len(got) != 0 {
		t.Errorf("Permissions() returned %d entries, want 0", len(got))
	}
}
