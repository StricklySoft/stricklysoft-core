package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ExtractBearerToken
// ---------------------------------------------------------------------------

func TestExtractBearerToken_Valid(t *testing.T) {
	token := ExtractBearerToken("Bearer my-secret-token")
	if token != "my-secret-token" {
		t.Errorf("ExtractBearerToken = %q, want %q", token, "my-secret-token")
	}
}

func TestExtractBearerToken_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "lowercase", header: "bearer tok", want: "tok"},
		{name: "uppercase", header: "BEARER tok", want: "tok"},
		{name: "mixed case", header: "BeArEr tok", want: "tok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractBearerToken(tt.header)
			if got != tt.want {
				t.Errorf("ExtractBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestExtractBearerToken_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{name: "empty", header: ""},
		{name: "only prefix", header: "Bearer "},
		{name: "no prefix", header: "my-secret-token"},
		{name: "basic auth", header: "Basic dXNlcjpwYXNz"},
		{name: "too short", header: "Bear"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractBearerToken(tt.header)
			if got != "" {
				t.Errorf("ExtractBearerToken(%q) = %q, want empty string", tt.header, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SerializeClaims / DeserializeClaims
// ---------------------------------------------------------------------------

func TestSerializeClaims_RoundTrip(t *testing.T) {
	original := map[string]any{
		"email":  "user@example.com",
		"role":   "admin",
		"active": true,
	}

	encoded, err := SerializeClaims(original)
	if err != nil {
		t.Fatalf("SerializeClaims error: %v", err)
	}
	if encoded == "" {
		t.Fatal("SerializeClaims returned empty string for non-empty claims")
	}

	decoded, err := DeserializeClaims(encoded)
	if err != nil {
		t.Fatalf("DeserializeClaims error: %v", err)
	}
	if decoded["email"] != "user@example.com" {
		t.Errorf("email = %v, want %q", decoded["email"], "user@example.com")
	}
	if decoded["role"] != "admin" {
		t.Errorf("role = %v, want %q", decoded["role"], "admin")
	}
	// JSON unmarshals booleans as bool.
	if decoded["active"] != true {
		t.Errorf("active = %v, want true", decoded["active"])
	}
}

func TestSerializeClaims_EmptyMap(t *testing.T) {
	encoded, err := SerializeClaims(map[string]any{})
	if err != nil {
		t.Fatalf("SerializeClaims error: %v", err)
	}
	if encoded != "" {
		t.Errorf("SerializeClaims(empty) = %q, want empty string", encoded)
	}
}

func TestSerializeClaims_NilMap(t *testing.T) {
	encoded, err := SerializeClaims(nil)
	if err != nil {
		t.Fatalf("SerializeClaims error: %v", err)
	}
	if encoded != "" {
		t.Errorf("SerializeClaims(nil) = %q, want empty string", encoded)
	}
}

func TestSerializeClaims_Base64URLSafe(t *testing.T) {
	claims := map[string]any{"data": "value with special chars: +/= and more"}
	encoded, err := SerializeClaims(claims)
	if err != nil {
		t.Fatalf("SerializeClaims error: %v", err)
	}
	// base64url encoding should not contain '+', '/', or '='.
	if strings.ContainsAny(encoded, "+/=") {
		t.Errorf("encoded claims contain non-URL-safe characters: %q", encoded)
	}
}

func TestDeserializeClaims_EmptyString(t *testing.T) {
	decoded, err := DeserializeClaims("")
	if err != nil {
		t.Fatalf("DeserializeClaims error: %v", err)
	}
	if decoded == nil {
		t.Error("DeserializeClaims(\"\") returned nil, want empty map")
	}
	if len(decoded) != 0 {
		t.Errorf("DeserializeClaims(\"\") has %d entries, want 0", len(decoded))
	}
}

func TestDeserializeClaims_InvalidBase64(t *testing.T) {
	_, err := DeserializeClaims("!!! not valid base64 !!!")
	if err == nil {
		t.Error("DeserializeClaims did not return error for invalid base64")
	}
}

func TestDeserializeClaims_InvalidJSON(t *testing.T) {
	// Valid base64 but not valid JSON.
	encoded := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	_, err := DeserializeClaims(encoded)
	if err == nil {
		t.Error("DeserializeClaims did not return error for invalid JSON")
	}
}

func TestSerializeClaims_ExceedsMaxHeaderSize(t *testing.T) {
	// Create claims that will produce an encoded output larger than MaxHeaderValueSize.
	claims := make(map[string]any)
	for i := 0; i < 500; i++ {
		claims[fmt.Sprintf("claim-key-%04d", i)] = strings.Repeat("x", 20)
	}

	_, err := SerializeClaims(claims)
	if err == nil {
		t.Error("SerializeClaims should return error when encoded output exceeds MaxHeaderValueSize")
	}
	if err != nil && !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("error message should mention size limit, got: %v", err)
	}
}

func TestMaxHeaderValueSize_IsReasonable(t *testing.T) {
	if MaxHeaderValueSize < 4096 {
		t.Errorf("MaxHeaderValueSize = %d, too small for practical use", MaxHeaderValueSize)
	}
	if MaxHeaderValueSize > 16384 {
		t.Errorf("MaxHeaderValueSize = %d, exceeds HTTP/2 default limit", MaxHeaderValueSize)
	}
}

// ---------------------------------------------------------------------------
// SerializeCallChain / DeserializeCallChain
// ---------------------------------------------------------------------------

func TestSerializeCallChain_RoundTrip(t *testing.T) {
	original := &CallChain{
		OriginalID:   "user-42",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "gateway", IdentityID: "svc-gw", IdentityType: IdentityTypeService},
			{ServiceName: "orchestrator", IdentityID: "svc-orch", IdentityType: IdentityTypeService},
		},
	}

	encoded, err := SerializeCallChain(original)
	if err != nil {
		t.Fatalf("SerializeCallChain error: %v", err)
	}
	if encoded == "" {
		t.Fatal("SerializeCallChain returned empty string for non-nil chain")
	}

	decoded, err := DeserializeCallChain(encoded)
	if err != nil {
		t.Fatalf("DeserializeCallChain error: %v", err)
	}
	if decoded.OriginalID != "user-42" {
		t.Errorf("OriginalID = %q, want %q", decoded.OriginalID, "user-42")
	}
	if decoded.OriginalType != IdentityTypeUser {
		t.Errorf("OriginalType = %q, want %q", decoded.OriginalType, IdentityTypeUser)
	}
	if len(decoded.Callers) != 2 {
		t.Fatalf("Callers has %d entries, want 2", len(decoded.Callers))
	}
	if decoded.Callers[0].ServiceName != "gateway" {
		t.Errorf("Callers[0].ServiceName = %q, want %q", decoded.Callers[0].ServiceName, "gateway")
	}
	if decoded.Callers[1].ServiceName != "orchestrator" {
		t.Errorf("Callers[1].ServiceName = %q, want %q", decoded.Callers[1].ServiceName, "orchestrator")
	}
}

func TestSerializeCallChain_Nil(t *testing.T) {
	encoded, err := SerializeCallChain(nil)
	if err != nil {
		t.Fatalf("SerializeCallChain(nil) error: %v", err)
	}
	if encoded != "" {
		t.Errorf("SerializeCallChain(nil) = %q, want empty string", encoded)
	}
}

func TestDeserializeCallChain_EmptyString(t *testing.T) {
	chain, err := DeserializeCallChain("")
	if err != nil {
		t.Fatalf("DeserializeCallChain(\"\") error: %v", err)
	}
	if chain != nil {
		t.Error("DeserializeCallChain(\"\") returned non-nil, want nil")
	}
}

func TestDeserializeCallChain_InvalidBase64(t *testing.T) {
	_, err := DeserializeCallChain("!!! not valid base64 !!!")
	if err == nil {
		t.Error("DeserializeCallChain did not return error for invalid base64")
	}
}

func TestDeserializeCallChain_InvalidJSON(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("{not json"))
	_, err := DeserializeCallChain(encoded)
	if err == nil {
		t.Error("DeserializeCallChain did not return error for invalid JSON")
	}
}

func TestSerializeCallChain_ExceedsMaxHeaderSize(t *testing.T) {
	// Create a call chain with very long service names to exceed the limit.
	chain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
	}
	for i := 0; i < 100; i++ {
		chain.Callers = append(chain.Callers, CallerInfo{
			ServiceName:  strings.Repeat("a", 100),
			IdentityID:   strings.Repeat("b", 100),
			IdentityType: IdentityTypeService,
		})
	}

	_, err := SerializeCallChain(chain)
	if err == nil {
		t.Error("SerializeCallChain should return error when encoded output exceeds MaxHeaderValueSize")
	}
	if err != nil && !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("error message should mention size limit, got: %v", err)
	}
}

func TestSerializeCallChain_Base64URLSafe(t *testing.T) {
	chain := &CallChain{
		OriginalID:   "user+special/chars=",
		OriginalType: IdentityTypeUser,
	}
	encoded, err := SerializeCallChain(chain)
	if err != nil {
		t.Fatalf("SerializeCallChain error: %v", err)
	}
	// base64url encoding should not contain '+', '/', or '='.
	if strings.ContainsAny(encoded, "+/=") {
		t.Errorf("encoded call chain contains non-URL-safe characters: %q", encoded)
	}
}

// ---------------------------------------------------------------------------
// identityToHeaders
// ---------------------------------------------------------------------------

func TestIdentityToHeaders_NilIdentity(t *testing.T) {
	headers, err := identityToHeaders(nil, "svc", nil)
	if err != nil {
		t.Fatalf("identityToHeaders error: %v", err)
	}
	if headers != nil {
		t.Error("identityToHeaders(nil, ...) returned non-nil headers")
	}
}

func TestIdentityToHeaders_BasicIdentity(t *testing.T) {
	identity := NewBasicIdentity("user-1", IdentityTypeUser, map[string]any{"email": "u@x.com"})
	chain := &CallChain{OriginalID: "user-1", OriginalType: IdentityTypeUser}

	headers, err := identityToHeaders(identity, "my-svc", chain)
	if err != nil {
		t.Fatalf("identityToHeaders error: %v", err)
	}

	if headers[HeaderIdentityID] != "user-1" {
		t.Errorf("HeaderIdentityID = %q, want %q", headers[HeaderIdentityID], "user-1")
	}
	if headers[HeaderIdentityType] != "user" {
		t.Errorf("HeaderIdentityType = %q, want %q", headers[HeaderIdentityType], "user")
	}
	if headers[HeaderCallerService] != "my-svc" {
		t.Errorf("HeaderCallerService = %q, want %q", headers[HeaderCallerService], "my-svc")
	}
	if headers[HeaderIdentityClaims] == "" {
		t.Error("HeaderIdentityClaims is empty, expected encoded claims")
	}
	if headers[HeaderCallChain] == "" {
		t.Error("HeaderCallChain is empty, expected encoded chain")
	}
}

func TestIdentityToHeaders_NoClaims(t *testing.T) {
	identity := NewBasicIdentity("svc-1", IdentityTypeService, nil)

	headers, err := identityToHeaders(identity, "", nil)
	if err != nil {
		t.Fatalf("identityToHeaders error: %v", err)
	}

	if headers[HeaderIdentityID] != "svc-1" {
		t.Errorf("HeaderIdentityID = %q, want %q", headers[HeaderIdentityID], "svc-1")
	}
	// No claims means the claims header should not be set.
	if _, exists := headers[HeaderIdentityClaims]; exists {
		t.Error("HeaderIdentityClaims should not be set for empty claims")
	}
	// No caller service means the caller header should not be set.
	if _, exists := headers[HeaderCallerService]; exists {
		t.Error("HeaderCallerService should not be set for empty caller")
	}
	// No chain means the chain header should not be set.
	if _, exists := headers[HeaderCallChain]; exists {
		t.Error("HeaderCallChain should not be set for nil chain")
	}
}

// ---------------------------------------------------------------------------
// identityFromHeaders
// ---------------------------------------------------------------------------

func TestIdentityFromHeaders_RoundTrip(t *testing.T) {
	original := NewBasicIdentity("user-1", IdentityTypeUser, map[string]any{"role": "admin"})
	chain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "gateway", IdentityID: "svc-gw", IdentityType: IdentityTypeService},
		},
	}

	headers, err := identityToHeaders(original, "gateway", chain)
	if err != nil {
		t.Fatalf("identityToHeaders error: %v", err)
	}

	getter := func(key string) string {
		return headers[key]
	}

	identity, callerSvc, gotChain, err := identityFromHeaders(getter)
	if err != nil {
		t.Fatalf("identityFromHeaders error: %v", err)
	}
	if identity == nil {
		t.Fatal("identityFromHeaders returned nil identity")
	}
	if identity.ID() != "user-1" {
		t.Errorf("ID() = %q, want %q", identity.ID(), "user-1")
	}
	if identity.Type() != IdentityTypeUser {
		t.Errorf("Type() = %q, want %q", identity.Type(), IdentityTypeUser)
	}
	if identity.Claims()["role"] != "admin" {
		t.Errorf("Claims()[role] = %v, want %q", identity.Claims()["role"], "admin")
	}
	if callerSvc != "gateway" {
		t.Errorf("callerService = %q, want %q", callerSvc, "gateway")
	}
	if gotChain == nil {
		t.Fatal("identityFromHeaders returned nil chain")
	}
	if gotChain.OriginalID != "user-1" {
		t.Errorf("chain.OriginalID = %q, want %q", gotChain.OriginalID, "user-1")
	}
	if len(gotChain.Callers) != 1 {
		t.Fatalf("chain.Callers has %d entries, want 1", len(gotChain.Callers))
	}
}

func TestIdentityFromHeaders_NoHeaders(t *testing.T) {
	getter := func(key string) string { return "" }

	identity, callerSvc, chain, err := identityFromHeaders(getter)
	if err != nil {
		t.Fatalf("identityFromHeaders error: %v", err)
	}
	if identity != nil {
		t.Error("expected nil identity when no headers present")
	}
	if callerSvc != "" {
		t.Errorf("callerService = %q, want empty", callerSvc)
	}
	if chain != nil {
		t.Error("expected nil chain when no headers present")
	}
}

func TestIdentityFromHeaders_InvalidIdentityType(t *testing.T) {
	// When identity type is invalid, it should default to IdentityTypeService.
	getter := func(key string) string {
		switch key {
		case HeaderIdentityID:
			return "svc-1"
		case HeaderIdentityType:
			return "unknown_type"
		default:
			return ""
		}
	}

	identity, _, _, err := identityFromHeaders(getter)
	if err != nil {
		t.Fatalf("identityFromHeaders error: %v", err)
	}
	if identity == nil {
		t.Fatal("identityFromHeaders returned nil identity")
	}
	if identity.Type() != IdentityTypeService {
		t.Errorf("Type() = %q, want %q (default for invalid type)", identity.Type(), IdentityTypeService)
	}
}

func TestIdentityFromHeaders_EmptyIdentityType(t *testing.T) {
	// When identity type is empty (missing header), it should default to
	// IdentityTypeService without logging a warning.
	getter := func(key string) string {
		switch key {
		case HeaderIdentityID:
			return "svc-1"
		case HeaderIdentityType:
			return ""
		default:
			return ""
		}
	}

	identity, _, _, err := identityFromHeaders(getter)
	if err != nil {
		t.Fatalf("identityFromHeaders error: %v", err)
	}
	if identity == nil {
		t.Fatal("identityFromHeaders returned nil identity")
	}
	if identity.Type() != IdentityTypeService {
		t.Errorf("Type() = %q, want %q (default for empty type)", identity.Type(), IdentityTypeService)
	}
}

func TestIdentityFromHeaders_InvalidClaims(t *testing.T) {
	getter := func(key string) string {
		switch key {
		case HeaderIdentityID:
			return "user-1"
		case HeaderIdentityType:
			return "user"
		case HeaderIdentityClaims:
			return "!!! invalid base64 !!!"
		default:
			return ""
		}
	}

	_, _, _, err := identityFromHeaders(getter)
	if err == nil {
		t.Error("identityFromHeaders should return error for invalid claims encoding")
	}
}

func TestIdentityFromHeaders_InvalidCallChain(t *testing.T) {
	getter := func(key string) string {
		switch key {
		case HeaderIdentityID:
			return "user-1"
		case HeaderIdentityType:
			return "user"
		case HeaderCallChain:
			return "!!! invalid base64 !!!"
		default:
			return ""
		}
	}

	_, _, _, err := identityFromHeaders(getter)
	if err == nil {
		t.Error("identityFromHeaders should return error for invalid call chain encoding")
	}
}

// ---------------------------------------------------------------------------
// Header constants
// ---------------------------------------------------------------------------

func TestHeaderConstants_Values(t *testing.T) {
	// Verify that header constants have the expected values and use lowercase
	// (required for gRPC metadata compatibility).
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{name: "authorization", constant: HeaderAuthorization, expected: "authorization"},
		{name: "identity-id", constant: HeaderIdentityID, expected: "x-identity-id"},
		{name: "identity-type", constant: HeaderIdentityType, expected: "x-identity-type"},
		{name: "identity-claims", constant: HeaderIdentityClaims, expected: "x-identity-claims"},
		{name: "caller-service", constant: HeaderCallerService, expected: "x-caller-service"},
		{name: "call-chain", constant: HeaderCallChain, expected: "x-call-chain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("header constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// JSON serialization structure
// ---------------------------------------------------------------------------

func TestCallChain_JSONStructure(t *testing.T) {
	// Verify JSON field names match expected wire format.
	chain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "gw", IdentityID: "svc-gw", IdentityType: IdentityTypeService},
		},
	}

	data, err := json.Marshal(chain)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	// Check top-level keys match JSON tags.
	for _, key := range []string{"original_id", "original_type", "callers"} {
		if _, exists := raw[key]; !exists {
			t.Errorf("JSON output missing expected key %q", key)
		}
	}

	// Check caller info keys.
	callers, ok := raw["callers"].([]any)
	if !ok || len(callers) == 0 {
		t.Fatal("callers is not a non-empty array")
	}
	caller, ok := callers[0].(map[string]any)
	if !ok {
		t.Fatal("caller entry is not a map")
	}
	for _, key := range []string{"service_name", "identity_id", "identity_type"} {
		if _, exists := caller[key]; !exists {
			t.Errorf("caller JSON missing expected key %q", key)
		}
	}
}
