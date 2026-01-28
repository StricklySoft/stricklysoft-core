package auth

import (
	"testing"
)

func TestIdentityType_String(t *testing.T) {
	tests := []struct {
		name     string
		idType   IdentityType
		expected string
	}{
		{name: "user type", idType: IdentityTypeUser, expected: "user"},
		{name: "service type", idType: IdentityTypeService, expected: "service"},
		{name: "agent type", idType: IdentityTypeAgent, expected: "agent"},
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

// Verify BasicIdentity implements the Identity interface at compile time.
var _ Identity = (*BasicIdentity)(nil)

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
