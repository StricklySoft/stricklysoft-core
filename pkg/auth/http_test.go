package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// HTTPMiddleware
// ---------------------------------------------------------------------------

func TestHTTPMiddleware_ValidToken(t *testing.T) {
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

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	identity, ok := IdentityFromContext(capturedCtx)
	if !ok {
		t.Fatal("identity not found in context after middleware")
	}
	if identity.ID() != "user-42" {
		t.Errorf("identity.ID() = %q, want %q", identity.ID(), "user-42")
	}
}

func TestHTTPMiddleware_MissingAuthHeader(t *testing.T) {
	validator := &mockValidator{identity: newTestIdentity()}
	middleware := HTTPMiddleware(validator, "test-service")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called when auth header is missing")
	})

	handler := middleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestHTTPMiddleware_InvalidToken(t *testing.T) {
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

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestHTTPMiddleware_NonBearerAuth(t *testing.T) {
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

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestHTTPMiddleware_WithCallerServiceHeader(t *testing.T) {
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

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	caller, ok := CallerServiceFromContext(capturedCtx)
	if !ok {
		t.Fatal("caller service not found in context")
	}
	if caller != "upstream-gateway" {
		t.Errorf("caller = %q, want %q", caller, "upstream-gateway")
	}
}

func TestHTTPMiddleware_WithCallChainHeader(t *testing.T) {
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
	if err != nil {
		t.Fatalf("SerializeCallChain error: %v", err)
	}

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

	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	gotChain, ok := CallChainFromContext(capturedCtx)
	if !ok {
		t.Fatal("call chain not found in context")
	}
	if gotChain.OriginalID != "user-42" {
		t.Errorf("chain.OriginalID = %q, want %q", gotChain.OriginalID, "user-42")
	}
	if len(gotChain.Callers) != 1 {
		t.Fatalf("chain.Callers has %d entries, want 1", len(gotChain.Callers))
	}
}

func TestHTTPMiddleware_MalformedCallChain_DoesNotFail(t *testing.T) {
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
	if rr.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d (malformed chain should not block request)", rr.Code, http.StatusOK)
	}

	// Identity should still be present.
	identity, ok := IdentityFromContext(capturedCtx)
	if !ok {
		t.Fatal("identity not found in context")
	}
	if identity.ID() != "user-42" {
		t.Errorf("identity.ID() = %q, want %q", identity.ID(), "user-42")
	}

	// Call chain should not be present since deserialization failed.
	_, chainOk := CallChainFromContext(capturedCtx)
	if chainOk {
		t.Error("call chain should not be in context when deserialization fails")
	}
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
	mock := &mockRoundTripper{
		response: &http.Response{StatusCode: http.StatusOK},
	}
	rt := NewPropagatingRoundTripper("my-service", mock)

	identity := NewBasicIdentity("user-42", IdentityTypeUser, map[string]any{"role": "admin"})
	ctx := ContextWithIdentity(context.Background(), identity)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://downstream/api/data", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify identity headers were set on the outgoing request.
	captured := mock.capturedReq
	if captured.Header.Get(HeaderIdentityID) != "user-42" {
		t.Errorf("HeaderIdentityID = %q, want %q", captured.Header.Get(HeaderIdentityID), "user-42")
	}
	if captured.Header.Get(HeaderIdentityType) != "user" {
		t.Errorf("HeaderIdentityType = %q, want %q", captured.Header.Get(HeaderIdentityType), "user")
	}
	if captured.Header.Get(HeaderCallerService) != "my-service" {
		t.Errorf("HeaderCallerService = %q, want %q", captured.Header.Get(HeaderCallerService), "my-service")
	}

	// Verify claims were propagated.
	claimsHeader := captured.Header.Get(HeaderIdentityClaims)
	if claimsHeader == "" {
		t.Fatal("HeaderIdentityClaims is empty, expected encoded claims")
	}
	claims, err := DeserializeClaims(claimsHeader)
	if err != nil {
		t.Fatalf("DeserializeClaims error: %v", err)
	}
	if claims["role"] != "admin" {
		t.Errorf("claims[role] = %v, want %q", claims["role"], "admin")
	}

	// Verify call chain was created.
	chainHeader := captured.Header.Get(HeaderCallChain)
	if chainHeader == "" {
		t.Fatal("HeaderCallChain is empty")
	}
	chain, err := DeserializeCallChain(chainHeader)
	if err != nil {
		t.Fatalf("DeserializeCallChain error: %v", err)
	}
	if chain.OriginalID != "user-42" {
		t.Errorf("chain.OriginalID = %q, want %q", chain.OriginalID, "user-42")
	}
	if len(chain.Callers) != 1 {
		t.Fatalf("chain.Callers has %d entries, want 1", len(chain.Callers))
	}
	if chain.Callers[0].ServiceName != "my-service" {
		t.Errorf("chain.Callers[0].ServiceName = %q, want %q", chain.Callers[0].ServiceName, "my-service")
	}
}

func TestPropagatingRoundTripper_NoIdentity(t *testing.T) {
	mock := &mockRoundTripper{
		response: &http.Response{StatusCode: http.StatusOK},
	}
	rt := NewPropagatingRoundTripper("my-service", mock)

	// Context without identity — request should pass through unmodified.
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://downstream/api/data", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// No identity headers should have been added.
	captured := mock.capturedReq
	if captured.Header.Get(HeaderIdentityID) != "" {
		t.Errorf("HeaderIdentityID should be empty when no identity is present, got %q", captured.Header.Get(HeaderIdentityID))
	}
}

func TestPropagatingRoundTripper_ExistingCallChain(t *testing.T) {
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
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}

	captured := mock.capturedReq
	chainHeader := captured.Header.Get(HeaderCallChain)
	if chainHeader == "" {
		t.Fatal("HeaderCallChain is empty")
	}
	chain, err := DeserializeCallChain(chainHeader)
	if err != nil {
		t.Fatalf("DeserializeCallChain error: %v", err)
	}
	// Should have the existing caller plus the new one.
	if len(chain.Callers) != 2 {
		t.Fatalf("chain.Callers has %d entries, want 2", len(chain.Callers))
	}
	if chain.Callers[0].ServiceName != "gateway" {
		t.Errorf("chain.Callers[0].ServiceName = %q, want %q", chain.Callers[0].ServiceName, "gateway")
	}
	if chain.Callers[1].ServiceName != "downstream-service" {
		t.Errorf("chain.Callers[1].ServiceName = %q, want %q", chain.Callers[1].ServiceName, "downstream-service")
	}
}

func TestPropagatingRoundTripper_DoesNotMutateOriginalRequest(t *testing.T) {
	mock := &mockRoundTripper{
		response: &http.Response{StatusCode: http.StatusOK},
	}
	rt := NewPropagatingRoundTripper("my-service", mock)

	identity := NewBasicIdentity("user-1", IdentityTypeUser, nil)
	ctx := ContextWithIdentity(context.Background(), identity)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://downstream/api/data", nil)
	req.Header.Set("X-Original", "preserved")

	_, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}

	// Original request should not have identity headers.
	if req.Header.Get(HeaderIdentityID) != "" {
		t.Error("original request was mutated with identity headers")
	}
	// Original custom header should still be present.
	if req.Header.Get("X-Original") != "preserved" {
		t.Error("original request lost its custom headers")
	}
}

func TestPropagatingRoundTripper_NilTransport(t *testing.T) {
	// Passing nil transport should use http.DefaultTransport.
	rt := NewPropagatingRoundTripper("my-service", nil)
	if rt.wrapped == nil {
		t.Fatal("wrapped transport is nil, expected http.DefaultTransport")
	}
	if rt.wrapped != http.DefaultTransport {
		t.Error("wrapped transport is not http.DefaultTransport")
	}
}

func TestPropagatingRoundTripper_PreservesExistingHeaders(t *testing.T) {
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
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}

	captured := mock.capturedReq
	if captured.Header.Get("X-Request-Id") != "req-123" {
		t.Errorf("X-Request-Id = %q, want %q", captured.Header.Get("X-Request-Id"), "req-123")
	}
	if captured.Header.Get("Accept") != "application/json" {
		t.Errorf("Accept = %q, want %q", captured.Header.Get("Accept"), "application/json")
	}
	// Identity headers should also be present.
	if captured.Header.Get(HeaderIdentityID) != "user-1" {
		t.Errorf("HeaderIdentityID = %q, want %q", captured.Header.Get(HeaderIdentityID), "user-1")
	}
}
