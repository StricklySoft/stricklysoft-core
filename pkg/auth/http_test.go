package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// HTTPMiddleware
// ---------------------------------------------------------------------------

func TestHTTPMiddleware_ValidToken(t *testing.T) {
	t.Parallel()
	validator := &mockValidator{identity: newTestIdentity()}
	middleware := HTTPMiddleware(validator, "test-service")

	var capturedCtx context.Context
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	identity, ok := IdentityFromContext(capturedCtx)
	require.True(t, ok, "identity not found in context after middleware")
	assert.Equal(t, "user-42", identity.ID())
}

func TestHTTPMiddleware_MissingAuthHeader(t *testing.T) {
	t.Parallel()
	validator := &mockValidator{identity: newTestIdentity()}
	middleware := HTTPMiddleware(validator, "test-service")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called when auth header is missing")
	})

	handler := middleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestHTTPMiddleware_InvalidToken(t *testing.T) {
	t.Parallel()
	validator := &mockValidator{err: errors.New("token expired")}
	middleware := HTTPMiddleware(validator, "test-service")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called when token is invalid")
	})

	handler := middleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestHTTPMiddleware_NonBearerAuth(t *testing.T) {
	t.Parallel()
	validator := &mockValidator{identity: newTestIdentity()}
	middleware := HTTPMiddleware(validator, "test-service")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called for non-Bearer auth")
	})

	handler := middleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestHTTPMiddleware_WithCallerServiceHeader(t *testing.T) {
	t.Parallel()
	validator := &mockValidator{identity: newTestIdentity()}
	middleware := HTTPMiddleware(validator, "test-service")

	var capturedCtx context.Context
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set(HeaderCallerService, "upstream-gateway")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	caller, ok := CallerServiceFromContext(capturedCtx)
	require.True(t, ok, "caller service not found in context")
	assert.Equal(t, "upstream-gateway", caller)
}

func TestHTTPMiddleware_WithCallChainHeader(t *testing.T) {
	t.Parallel()
	validator := &mockValidator{identity: newTestIdentity()}
	middleware := HTTPMiddleware(validator, "test-service")

	chain := &CallChain{
		OriginalID:   "user-42",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "gateway", IdentityID: "svc-gw", IdentityType: IdentityTypeService},
		},
	}
	encodedChain, err := SerializeCallChain(chain)
	require.NoError(t, err, "SerializeCallChain error")

	var capturedCtx context.Context
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set(HeaderCallChain, encodedChain)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	gotChain, ok := CallChainFromContext(capturedCtx)
	require.True(t, ok, "call chain not found in context")
	assert.Equal(t, "user-42", gotChain.OriginalID)
	require.Len(t, gotChain.Callers, 1)
}

func TestHTTPMiddleware_MalformedCallChain_DoesNotFail(t *testing.T) {
	t.Parallel()
	validator := &mockValidator{identity: newTestIdentity()}
	middleware := HTTPMiddleware(validator, "test-service")

	var capturedCtx context.Context
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set(HeaderCallChain, "!!! not valid base64 !!!")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Malformed call chain should not cause a 401 — the identity was validated.
	assert.Equal(t, http.StatusOK, rr.Code, "malformed chain should not block request")

	// Identity should still be present.
	identity, ok := IdentityFromContext(capturedCtx)
	require.True(t, ok, "identity not found in context")
	assert.Equal(t, "user-42", identity.ID())

	// Call chain should not be present since deserialization failed.
	_, chainOk := CallChainFromContext(capturedCtx)
	assert.False(t, chainOk, "call chain should not be in context when deserialization fails")
}

// ---------------------------------------------------------------------------
// PropagatingRoundTripper
// ---------------------------------------------------------------------------

// mockRoundTripper captures the request for inspection and returns a canned response.
type mockRoundTripper struct {
	capturedReq *http.Request
	response    *http.Response
	err         error
}

func (m *mockRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	m.capturedReq = r
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func TestPropagatingRoundTripper_WithIdentity(t *testing.T) {
	t.Parallel()
	mock := &mockRoundTripper{
		response: &http.Response{StatusCode: http.StatusOK},
	}
	rt := NewPropagatingRoundTripper("my-service", mock)

	identity := NewBasicIdentity("user-42", IdentityTypeUser, map[string]any{"role": "admin"})
	ctx := ContextWithIdentity(context.Background(), identity)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://downstream/api/data", nil)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err, "RoundTrip error")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify identity headers were set on the outgoing request.
	captured := mock.capturedReq
	assert.Equal(t, "user-42", captured.Header.Get(HeaderIdentityID))
	assert.Equal(t, "user", captured.Header.Get(HeaderIdentityType))
	assert.Equal(t, "my-service", captured.Header.Get(HeaderCallerService))

	// Verify claims were propagated.
	claimsHeader := captured.Header.Get(HeaderIdentityClaims)
	require.NotEmpty(t, claimsHeader, "HeaderIdentityClaims is empty, expected encoded claims")
	claims, err := DeserializeClaims(claimsHeader)
	require.NoError(t, err, "DeserializeClaims error")
	assert.Equal(t, "admin", claims["role"])

	// Verify call chain was created.
	chainHeader := captured.Header.Get(HeaderCallChain)
	require.NotEmpty(t, chainHeader, "HeaderCallChain is empty")
	chain, err := DeserializeCallChain(chainHeader)
	require.NoError(t, err, "DeserializeCallChain error")
	assert.Equal(t, "user-42", chain.OriginalID)
	require.Len(t, chain.Callers, 1)
	assert.Equal(t, "my-service", chain.Callers[0].ServiceName)
}

func TestPropagatingRoundTripper_NoIdentity(t *testing.T) {
	t.Parallel()
	mock := &mockRoundTripper{
		response: &http.Response{StatusCode: http.StatusOK},
	}
	rt := NewPropagatingRoundTripper("my-service", mock)

	// Context without identity — request should pass through unmodified.
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://downstream/api/data", nil)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err, "RoundTrip error")
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// No identity headers should have been added.
	captured := mock.capturedReq
	assert.Empty(t, captured.Header.Get(HeaderIdentityID), "HeaderIdentityID should be empty when no identity is present")
}

func TestPropagatingRoundTripper_ExistingCallChain(t *testing.T) {
	t.Parallel()
	mock := &mockRoundTripper{
		response: &http.Response{StatusCode: http.StatusOK},
	}
	rt := NewPropagatingRoundTripper("downstream-service", mock)

	identity := NewBasicIdentity("user-1", IdentityTypeUser, nil)
	existingChain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "gateway", IdentityID: "svc-gw", IdentityType: IdentityTypeService},
		},
	}
	ctx := ContextWithIdentity(context.Background(), identity)
	ctx = ContextWithCallChain(ctx, existingChain)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://downstream/api/data", nil)

	_, err := rt.RoundTrip(req)
	require.NoError(t, err, "RoundTrip error")

	captured := mock.capturedReq
	chainHeader := captured.Header.Get(HeaderCallChain)
	require.NotEmpty(t, chainHeader, "HeaderCallChain is empty")
	chain, err := DeserializeCallChain(chainHeader)
	require.NoError(t, err, "DeserializeCallChain error")
	// Should have the existing caller plus the new one.
	require.Len(t, chain.Callers, 2)
	assert.Equal(t, "gateway", chain.Callers[0].ServiceName)
	assert.Equal(t, "downstream-service", chain.Callers[1].ServiceName)
}

func TestPropagatingRoundTripper_DoesNotMutateOriginalRequest(t *testing.T) {
	t.Parallel()
	mock := &mockRoundTripper{
		response: &http.Response{StatusCode: http.StatusOK},
	}
	rt := NewPropagatingRoundTripper("my-service", mock)

	identity := NewBasicIdentity("user-1", IdentityTypeUser, nil)
	ctx := ContextWithIdentity(context.Background(), identity)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://downstream/api/data", nil)
	req.Header.Set("X-Original", "preserved")

	_, err := rt.RoundTrip(req)
	require.NoError(t, err, "RoundTrip error")

	// Original request should not have identity headers.
	assert.Empty(t, req.Header.Get(HeaderIdentityID), "original request was mutated with identity headers")
	// Original custom header should still be present.
	assert.Equal(t, "preserved", req.Header.Get("X-Original"), "original request lost its custom headers")
}

func TestPropagatingRoundTripper_NilTransport(t *testing.T) {
	t.Parallel()
	// Passing nil transport should use http.DefaultTransport.
	rt := NewPropagatingRoundTripper("my-service", nil)
	require.NotNil(t, rt.wrapped, "wrapped transport is nil, expected http.DefaultTransport")
	assert.Equal(t, http.DefaultTransport, rt.wrapped, "wrapped transport is not http.DefaultTransport")
}

func TestPropagatingRoundTripper_PreservesExistingHeaders(t *testing.T) {
	t.Parallel()
	mock := &mockRoundTripper{
		response: &http.Response{StatusCode: http.StatusOK},
	}
	rt := NewPropagatingRoundTripper("my-service", mock)

	identity := NewBasicIdentity("user-1", IdentityTypeUser, nil)
	ctx := ContextWithIdentity(context.Background(), identity)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://downstream/api/data", nil)
	req.Header.Set("X-Request-Id", "req-123")
	req.Header.Set("Accept", "application/json")

	_, err := rt.RoundTrip(req)
	require.NoError(t, err, "RoundTrip error")

	captured := mock.capturedReq
	assert.Equal(t, "req-123", captured.Header.Get("X-Request-Id"))
	assert.Equal(t, "application/json", captured.Header.Get("Accept"))
	// Identity headers should also be present.
	assert.Equal(t, "user-1", captured.Header.Get(HeaderIdentityID))
}
