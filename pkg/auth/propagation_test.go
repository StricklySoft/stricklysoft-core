package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ExtractBearerToken
// ---------------------------------------------------------------------------

func TestExtractBearerToken_Valid(t *testing.T) {
	t.Parallel()
	token := ExtractBearerToken("Bearer my-secret-token")
	assert.Equal(t, "my-secret-token", token)
}

func TestExtractBearerToken_CaseInsensitive(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := ExtractBearerToken(tt.header)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractBearerToken_Invalid(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := ExtractBearerToken(tt.header)
			assert.Equal(t, "", got, "ExtractBearerToken(%q) should return empty string", tt.header)
		})
	}
}

// ---------------------------------------------------------------------------
// SerializeClaims / DeserializeClaims
// ---------------------------------------------------------------------------

func TestSerializeClaims_RoundTrip(t *testing.T) {
	t.Parallel()
	original := map[string]any{
		"email":  "user@example.com",
		"role":   "admin",
		"active": true,
	}

	encoded, err := SerializeClaims(original)
	require.NoError(t, err, "SerializeClaims error")
	require.NotEmpty(t, encoded, "SerializeClaims returned empty string for non-empty claims")

	decoded, err := DeserializeClaims(encoded)
	require.NoError(t, err, "DeserializeClaims error")
	assert.Equal(t, "user@example.com", decoded["email"])
	assert.Equal(t, "admin", decoded["role"])
	// JSON unmarshals booleans as bool.
	assert.Equal(t, true, decoded["active"])
}

func TestSerializeClaims_EmptyMap(t *testing.T) {
	t.Parallel()
	encoded, err := SerializeClaims(map[string]any{})
	require.NoError(t, err, "SerializeClaims error")
	assert.Equal(t, "", encoded, "SerializeClaims(empty) should return empty string")
}

func TestSerializeClaims_NilMap(t *testing.T) {
	t.Parallel()
	encoded, err := SerializeClaims(nil)
	require.NoError(t, err, "SerializeClaims error")
	assert.Equal(t, "", encoded, "SerializeClaims(nil) should return empty string")
}

func TestSerializeClaims_Base64URLSafe(t *testing.T) {
	t.Parallel()
	claims := map[string]any{"data": "value with special chars: +/= and more"}
	encoded, err := SerializeClaims(claims)
	require.NoError(t, err, "SerializeClaims error")
	// base64url encoding should not contain '+', '/', or '='.
	assert.False(t, strings.ContainsAny(encoded, "+/="), "encoded claims contain non-URL-safe characters: %q", encoded)
}

func TestDeserializeClaims_EmptyString(t *testing.T) {
	t.Parallel()
	decoded, err := DeserializeClaims("")
	require.NoError(t, err, "DeserializeClaims error")
	assert.NotNil(t, decoded, "DeserializeClaims(\"\") returned nil, want empty map")
	assert.Len(t, decoded, 0)
}

func TestDeserializeClaims_InvalidBase64(t *testing.T) {
	t.Parallel()
	_, err := DeserializeClaims("!!! not valid base64 !!!")
	require.Error(t, err, "DeserializeClaims did not return error for invalid base64")
}

func TestDeserializeClaims_InvalidJSON(t *testing.T) {
	t.Parallel()
	// Valid base64 but not valid JSON.
	encoded := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	_, err := DeserializeClaims(encoded)
	require.Error(t, err, "DeserializeClaims did not return error for invalid JSON")
}

func TestSerializeClaims_ExceedsMaxHeaderSize(t *testing.T) {
	t.Parallel()
	// Create claims that will produce an encoded output larger than MaxHeaderValueSize.
	claims := make(map[string]any)
	for i := 0; i < 500; i++ {
		claims[fmt.Sprintf("claim-key-%04d", i)] = strings.Repeat("x", 20)
	}

	_, err := SerializeClaims(claims)
	require.Error(t, err, "SerializeClaims should return error when encoded output exceeds MaxHeaderValueSize")
	assert.Contains(t, err.Error(), "exceeds maximum", "error message should mention size limit")
}

func TestMaxHeaderValueSize_IsReasonable(t *testing.T) {
	t.Parallel()
	assert.GreaterOrEqual(t, MaxHeaderValueSize, 4096, "MaxHeaderValueSize too small for practical use")
	assert.LessOrEqual(t, MaxHeaderValueSize, 16384, "MaxHeaderValueSize exceeds HTTP/2 default limit")
}

// ---------------------------------------------------------------------------
// SerializeCallChain / DeserializeCallChain
// ---------------------------------------------------------------------------

func TestSerializeCallChain_RoundTrip(t *testing.T) {
	t.Parallel()
	original := &CallChain{
		OriginalID:   "user-42",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "gateway", IdentityID: "svc-gw", IdentityType: IdentityTypeService},
			{ServiceName: "orchestrator", IdentityID: "svc-orch", IdentityType: IdentityTypeService},
		},
	}

	encoded, err := SerializeCallChain(original)
	require.NoError(t, err, "SerializeCallChain error")
	require.NotEmpty(t, encoded, "SerializeCallChain returned empty string for non-nil chain")

	decoded, err := DeserializeCallChain(encoded)
	require.NoError(t, err, "DeserializeCallChain error")
	assert.Equal(t, "user-42", decoded.OriginalID)
	assert.Equal(t, IdentityTypeUser, decoded.OriginalType)
	require.Len(t, decoded.Callers, 2)
	assert.Equal(t, "gateway", decoded.Callers[0].ServiceName)
	assert.Equal(t, "orchestrator", decoded.Callers[1].ServiceName)
}

func TestSerializeCallChain_Nil(t *testing.T) {
	t.Parallel()
	encoded, err := SerializeCallChain(nil)
	require.NoError(t, err, "SerializeCallChain(nil) error")
	assert.Equal(t, "", encoded, "SerializeCallChain(nil) should return empty string")
}

func TestDeserializeCallChain_EmptyString(t *testing.T) {
	t.Parallel()
	chain, err := DeserializeCallChain("")
	require.NoError(t, err, "DeserializeCallChain(\"\") error")
	assert.Nil(t, chain, "DeserializeCallChain(\"\") returned non-nil, want nil")
}

func TestDeserializeCallChain_InvalidBase64(t *testing.T) {
	t.Parallel()
	_, err := DeserializeCallChain("!!! not valid base64 !!!")
	require.Error(t, err, "DeserializeCallChain did not return error for invalid base64")
}

func TestDeserializeCallChain_InvalidJSON(t *testing.T) {
	t.Parallel()
	encoded := base64.RawURLEncoding.EncodeToString([]byte("{not json"))
	_, err := DeserializeCallChain(encoded)
	require.Error(t, err, "DeserializeCallChain did not return error for invalid JSON")
}

func TestSerializeCallChain_ExceedsMaxHeaderSize(t *testing.T) {
	t.Parallel()
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
	require.Error(t, err, "SerializeCallChain should return error when encoded output exceeds MaxHeaderValueSize")
	assert.Contains(t, err.Error(), "exceeds maximum", "error message should mention size limit")
}

func TestSerializeCallChain_Base64URLSafe(t *testing.T) {
	t.Parallel()
	chain := &CallChain{
		OriginalID:   "user+special/chars=",
		OriginalType: IdentityTypeUser,
	}
	encoded, err := SerializeCallChain(chain)
	require.NoError(t, err, "SerializeCallChain error")
	// base64url encoding should not contain '+', '/', or '='.
	assert.False(t, strings.ContainsAny(encoded, "+/="), "encoded call chain contains non-URL-safe characters: %q", encoded)
}

// ---------------------------------------------------------------------------
// identityToHeaders
// ---------------------------------------------------------------------------

func TestIdentityToHeaders_NilIdentity(t *testing.T) {
	t.Parallel()
	headers, err := identityToHeaders(nil, "svc", nil)
	require.NoError(t, err, "identityToHeaders error")
	assert.Nil(t, headers, "identityToHeaders(nil, ...) returned non-nil headers")
}

func TestIdentityToHeaders_BasicIdentity(t *testing.T) {
	t.Parallel()
	identity := NewBasicIdentity("user-1", IdentityTypeUser, map[string]any{"email": "u@x.com"})
	chain := &CallChain{OriginalID: "user-1", OriginalType: IdentityTypeUser}

	headers, err := identityToHeaders(identity, "my-svc", chain)
	require.NoError(t, err, "identityToHeaders error")

	assert.Equal(t, "user-1", headers[HeaderIdentityID])
	assert.Equal(t, "user", headers[HeaderIdentityType])
	assert.Equal(t, "my-svc", headers[HeaderCallerService])
	assert.NotEmpty(t, headers[HeaderIdentityClaims], "HeaderIdentityClaims is empty, expected encoded claims")
	assert.NotEmpty(t, headers[HeaderCallChain], "HeaderCallChain is empty, expected encoded chain")
}

func TestIdentityToHeaders_NoClaims(t *testing.T) {
	t.Parallel()
	identity := NewBasicIdentity("svc-1", IdentityTypeService, nil)

	headers, err := identityToHeaders(identity, "", nil)
	require.NoError(t, err, "identityToHeaders error")

	assert.Equal(t, "svc-1", headers[HeaderIdentityID])
	// No claims means the claims header should not be set.
	_, exists := headers[HeaderIdentityClaims]
	assert.False(t, exists, "HeaderIdentityClaims should not be set for empty claims")
	// No caller service means the caller header should not be set.
	_, exists = headers[HeaderCallerService]
	assert.False(t, exists, "HeaderCallerService should not be set for empty caller")
	// No chain means the chain header should not be set.
	_, exists = headers[HeaderCallChain]
	assert.False(t, exists, "HeaderCallChain should not be set for nil chain")
}

// ---------------------------------------------------------------------------
// identityFromHeaders
// ---------------------------------------------------------------------------

func TestIdentityFromHeaders_RoundTrip(t *testing.T) {
	t.Parallel()
	original := NewBasicIdentity("user-1", IdentityTypeUser, map[string]any{"role": "admin"})
	chain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "gateway", IdentityID: "svc-gw", IdentityType: IdentityTypeService},
		},
	}

	headers, err := identityToHeaders(original, "gateway", chain)
	require.NoError(t, err, "identityToHeaders error")

	getter := func(key string) string {
		return headers[key]
	}

	identity, callerSvc, gotChain, err := identityFromHeaders(getter)
	require.NoError(t, err, "identityFromHeaders error")
	require.NotNil(t, identity, "identityFromHeaders returned nil identity")
	assert.Equal(t, "user-1", identity.ID())
	assert.Equal(t, IdentityTypeUser, identity.Type())
	assert.Equal(t, "admin", identity.Claims()["role"])
	assert.Equal(t, "gateway", callerSvc)
	require.NotNil(t, gotChain, "identityFromHeaders returned nil chain")
	assert.Equal(t, "user-1", gotChain.OriginalID)
	require.Len(t, gotChain.Callers, 1)
}

func TestIdentityFromHeaders_NoHeaders(t *testing.T) {
	t.Parallel()
	getter := func(key string) string { return "" }

	identity, callerSvc, chain, err := identityFromHeaders(getter)
	require.NoError(t, err, "identityFromHeaders error")
	assert.Nil(t, identity, "expected nil identity when no headers present")
	assert.Equal(t, "", callerSvc)
	assert.Nil(t, chain, "expected nil chain when no headers present")
}

func TestIdentityFromHeaders_InvalidIdentityType(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "identityFromHeaders error")
	require.NotNil(t, identity, "identityFromHeaders returned nil identity")
	assert.Equal(t, IdentityTypeService, identity.Type(), "default for invalid type")
}

func TestIdentityFromHeaders_EmptyIdentityType(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "identityFromHeaders error")
	require.NotNil(t, identity, "identityFromHeaders returned nil identity")
	assert.Equal(t, IdentityTypeService, identity.Type(), "default for empty type")
}

func TestIdentityFromHeaders_InvalidClaims(t *testing.T) {
	t.Parallel()
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
	require.Error(t, err, "identityFromHeaders should return error for invalid claims encoding")
}

func TestIdentityFromHeaders_InvalidCallChain(t *testing.T) {
	t.Parallel()
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
	require.Error(t, err, "identityFromHeaders should return error for invalid call chain encoding")
}

// ---------------------------------------------------------------------------
// Header constants
// ---------------------------------------------------------------------------

func TestHeaderConstants_Values(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			assert.Equal(t, tt.expected, tt.constant)
		})
	}
}

// ---------------------------------------------------------------------------
// JSON serialization structure
// ---------------------------------------------------------------------------

func TestCallChain_JSONStructure(t *testing.T) {
	t.Parallel()
	// Verify JSON field names match expected wire format.
	chain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "gw", IdentityID: "svc-gw", IdentityType: IdentityTypeService},
		},
	}

	data, err := json.Marshal(chain)
	require.NoError(t, err, "json.Marshal error")

	var raw map[string]any
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err, "json.Unmarshal error")

	// Check top-level keys match JSON tags.
	for _, key := range []string{"original_id", "original_type", "callers"} {
		_, exists := raw[key]
		assert.True(t, exists, "JSON output missing expected key %q", key)
	}

	// Check caller info keys.
	callers, ok := raw["callers"].([]any)
	require.True(t, ok && len(callers) > 0, "callers is not a non-empty array")
	caller, ok := callers[0].(map[string]any)
	require.True(t, ok, "caller entry is not a map")
	for _, key := range []string{"service_name", "identity_id", "identity_type"} {
		_, exists := caller[key]
		assert.True(t, exists, "caller JSON missing expected key %q", key)
	}
}
