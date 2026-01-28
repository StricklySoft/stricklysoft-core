package auth

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ---------------------------------------------------------------------------
// Mock TokenValidator for testing
// ---------------------------------------------------------------------------

// mockValidator implements TokenValidator for testing purposes.
type mockValidator struct {
	// identity is returned on successful validation.
	identity Identity

	// err is returned when validation should fail.
	err error
}

func (m *mockValidator) Validate(_ context.Context, token string) (Identity, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.identity, nil
}

// newTestIdentity creates a BasicIdentity for use in tests.
func newTestIdentity() Identity {
	return NewBasicIdentity("user-42", IdentityTypeUser, map[string]any{"email": "test@example.com"})
}

// ---------------------------------------------------------------------------
// UnaryServerInterceptor
// ---------------------------------------------------------------------------

func TestUnaryServerInterceptor_ValidToken(t *testing.T) {
	validator := &mockValidator{identity: newTestIdentity()}
	interceptor := UnaryServerInterceptor(validator, "test-service")

	// Simulate incoming gRPC request with authorization metadata.
	md := metadata.Pairs(HeaderAuthorization, "Bearer valid-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var capturedCtx context.Context
	handler := func(ctx context.Context, req any) (any, error) {
		capturedCtx = ctx
		return "response", nil
	}

	resp, err := interceptor(ctx, "request", &grpc.UnaryServerInfo{}, handler)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if resp != "response" {
		t.Errorf("response = %v, want %q", resp, "response")
	}

	// Verify identity was set in context.
	identity, ok := IdentityFromContext(capturedCtx)
	if !ok {
		t.Fatal("identity not found in context after interceptor")
	}
	if identity.ID() != "user-42" {
		t.Errorf("identity.ID() = %q, want %q", identity.ID(), "user-42")
	}
}

func TestUnaryServerInterceptor_MissingMetadata(t *testing.T) {
	validator := &mockValidator{identity: newTestIdentity()}
	interceptor := UnaryServerInterceptor(validator, "test-service")

	// No metadata in context.
	ctx := context.Background()
	handler := func(ctx context.Context, req any) (any, error) {
		t.Error("handler should not be called when metadata is missing")
		return nil, nil
	}

	_, err := interceptor(ctx, "request", &grpc.UnaryServerInfo{}, handler)
	if err == nil {
		t.Fatal("interceptor should return error when metadata is missing")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error is not a gRPC status: %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("status code = %v, want %v", st.Code(), codes.Unauthenticated)
	}
}

func TestUnaryServerInterceptor_MissingAuthorizationHeader(t *testing.T) {
	validator := &mockValidator{identity: newTestIdentity()}
	interceptor := UnaryServerInterceptor(validator, "test-service")

	// Metadata without authorization.
	md := metadata.Pairs("other-key", "other-value")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	handler := func(ctx context.Context, req any) (any, error) {
		t.Error("handler should not be called when authorization is missing")
		return nil, nil
	}

	_, err := interceptor(ctx, "request", &grpc.UnaryServerInfo{}, handler)
	if err == nil {
		t.Fatal("interceptor should return error when authorization is missing")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("status code = %v, want %v", st.Code(), codes.Unauthenticated)
	}
}

func TestUnaryServerInterceptor_InvalidToken(t *testing.T) {
	validator := &mockValidator{err: errors.New("token expired")}
	interceptor := UnaryServerInterceptor(validator, "test-service")

	md := metadata.Pairs(HeaderAuthorization, "Bearer expired-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	handler := func(ctx context.Context, req any) (any, error) {
		t.Error("handler should not be called when token is invalid")
		return nil, nil
	}

	_, err := interceptor(ctx, "request", &grpc.UnaryServerInfo{}, handler)
	if err == nil {
		t.Fatal("interceptor should return error when token is invalid")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("status code = %v, want %v", st.Code(), codes.Unauthenticated)
	}
}

func TestUnaryServerInterceptor_InvalidBearerFormat(t *testing.T) {
	validator := &mockValidator{identity: newTestIdentity()}
	interceptor := UnaryServerInterceptor(validator, "test-service")

	// Authorization header without "Bearer " prefix.
	md := metadata.Pairs(HeaderAuthorization, "Basic some-credentials")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	handler := func(ctx context.Context, req any) (any, error) {
		t.Error("handler should not be called for non-Bearer auth")
		return nil, nil
	}

	_, err := interceptor(ctx, "request", &grpc.UnaryServerInfo{}, handler)
	if err == nil {
		t.Fatal("interceptor should return error for non-Bearer auth")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("status code = %v, want %v", st.Code(), codes.Unauthenticated)
	}
}

func TestUnaryServerInterceptor_WithCallerService(t *testing.T) {
	validator := &mockValidator{identity: newTestIdentity()}
	interceptor := UnaryServerInterceptor(validator, "test-service")

	md := metadata.Pairs(
		HeaderAuthorization, "Bearer valid-token",
		HeaderCallerService, "upstream-gateway",
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var capturedCtx context.Context
	handler := func(ctx context.Context, req any) (any, error) {
		capturedCtx = ctx
		return "ok", nil
	}

	_, err := interceptor(ctx, "request", &grpc.UnaryServerInfo{}, handler)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	caller, ok := CallerServiceFromContext(capturedCtx)
	if !ok {
		t.Fatal("caller service not found in context")
	}
	if caller != "upstream-gateway" {
		t.Errorf("caller = %q, want %q", caller, "upstream-gateway")
	}
}

func TestUnaryServerInterceptor_WithCallChain(t *testing.T) {
	validator := &mockValidator{identity: newTestIdentity()}
	interceptor := UnaryServerInterceptor(validator, "test-service")

	// Create and serialize a call chain.
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

	md := metadata.Pairs(
		HeaderAuthorization, "Bearer valid-token",
		HeaderCallChain, encodedChain,
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var capturedCtx context.Context
	handler := func(ctx context.Context, req any) (any, error) {
		capturedCtx = ctx
		return "ok", nil
	}

	_, err = interceptor(ctx, "request", &grpc.UnaryServerInfo{}, handler)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
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

// ---------------------------------------------------------------------------
// StreamServerInterceptor
// ---------------------------------------------------------------------------

// mockServerStream implements grpc.ServerStream for testing.
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func TestStreamServerInterceptor_ValidToken(t *testing.T) {
	validator := &mockValidator{identity: newTestIdentity()}
	interceptor := StreamServerInterceptor(validator, "test-service")

	md := metadata.Pairs(HeaderAuthorization, "Bearer valid-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	stream := &mockServerStream{ctx: ctx}

	var capturedCtx context.Context
	handler := func(srv any, ss grpc.ServerStream) error {
		capturedCtx = ss.Context()
		return nil
	}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, handler)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	identity, ok := IdentityFromContext(capturedCtx)
	if !ok {
		t.Fatal("identity not found in stream context")
	}
	if identity.ID() != "user-42" {
		t.Errorf("identity.ID() = %q, want %q", identity.ID(), "user-42")
	}
}

func TestStreamServerInterceptor_MissingAuth(t *testing.T) {
	validator := &mockValidator{identity: newTestIdentity()}
	interceptor := StreamServerInterceptor(validator, "test-service")

	ctx := context.Background()
	stream := &mockServerStream{ctx: ctx}

	handler := func(srv any, ss grpc.ServerStream) error {
		t.Error("handler should not be called")
		return nil
	}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, handler)
	if err == nil {
		t.Fatal("interceptor should return error when auth is missing")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("status code = %v, want %v", st.Code(), codes.Unauthenticated)
	}
}

// ---------------------------------------------------------------------------
// UnaryClientInterceptor
// ---------------------------------------------------------------------------

func TestUnaryClientInterceptor_PropagatesIdentity(t *testing.T) {
	interceptor := UnaryClientInterceptor("client-service")

	// Set up context with identity.
	identity := NewBasicIdentity("user-42", IdentityTypeUser, map[string]any{"role": "admin"})
	ctx := ContextWithIdentity(context.Background(), identity)

	var capturedCtx context.Context
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		capturedCtx = ctx
		return nil
	}

	err := interceptor(ctx, "/test.Service/Method", "req", "reply", nil, invoker)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	// Verify outgoing metadata contains identity headers.
	md, ok := metadata.FromOutgoingContext(capturedCtx)
	if !ok {
		t.Fatal("no outgoing metadata in context")
	}

	idValues := md.Get(HeaderIdentityID)
	if len(idValues) == 0 || idValues[0] != "user-42" {
		t.Errorf("metadata identity-id = %v, want [\"user-42\"]", idValues)
	}

	typeValues := md.Get(HeaderIdentityType)
	if len(typeValues) == 0 || typeValues[0] != "user" {
		t.Errorf("metadata identity-type = %v, want [\"user\"]", typeValues)
	}

	callerValues := md.Get(HeaderCallerService)
	if len(callerValues) == 0 || callerValues[0] != "client-service" {
		t.Errorf("metadata caller-service = %v, want [\"client-service\"]", callerValues)
	}

	chainValues := md.Get(HeaderCallChain)
	if len(chainValues) == 0 {
		t.Fatal("metadata call-chain is missing")
	}
	chain, err := DeserializeCallChain(chainValues[0])
	if err != nil {
		t.Fatalf("DeserializeCallChain error: %v", err)
	}
	if chain.OriginalID != "user-42" {
		t.Errorf("chain.OriginalID = %q, want %q", chain.OriginalID, "user-42")
	}
	if len(chain.Callers) != 1 {
		t.Fatalf("chain.Callers has %d entries, want 1", len(chain.Callers))
	}
	if chain.Callers[0].ServiceName != "client-service" {
		t.Errorf("chain.Callers[0].ServiceName = %q, want %q", chain.Callers[0].ServiceName, "client-service")
	}
}

func TestUnaryClientInterceptor_NoIdentity(t *testing.T) {
	interceptor := UnaryClientInterceptor("client-service")

	// Context without identity — interceptor should pass through without modification.
	ctx := context.Background()

	var capturedCtx context.Context
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		capturedCtx = ctx
		return nil
	}

	err := interceptor(ctx, "/test.Service/Method", "req", "reply", nil, invoker)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	// No outgoing metadata should be added.
	_, ok := metadata.FromOutgoingContext(capturedCtx)
	if ok {
		t.Error("outgoing metadata should not be set when no identity is present")
	}
}

func TestUnaryClientInterceptor_ExistingCallChain(t *testing.T) {
	interceptor := UnaryClientInterceptor("downstream-service")

	// Set up context with identity and an existing call chain.
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

	var capturedCtx context.Context
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		capturedCtx = ctx
		return nil
	}

	err := interceptor(ctx, "/test.Service/Method", "req", "reply", nil, invoker)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	md, ok := metadata.FromOutgoingContext(capturedCtx)
	if !ok {
		t.Fatal("no outgoing metadata in context")
	}

	chainValues := md.Get(HeaderCallChain)
	if len(chainValues) == 0 {
		t.Fatal("call-chain metadata missing")
	}
	chain, err := DeserializeCallChain(chainValues[0])
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

func TestUnaryClientInterceptor_PreservesExistingMetadata(t *testing.T) {
	interceptor := UnaryClientInterceptor("client-service")

	identity := NewBasicIdentity("user-1", IdentityTypeUser, nil)
	ctx := ContextWithIdentity(context.Background(), identity)

	// Add pre-existing outgoing metadata.
	existingMD := metadata.Pairs("x-custom-header", "custom-value")
	ctx = metadata.NewOutgoingContext(ctx, existingMD)

	var capturedCtx context.Context
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		capturedCtx = ctx
		return nil
	}

	err := interceptor(ctx, "/test.Service/Method", "req", "reply", nil, invoker)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	md, ok := metadata.FromOutgoingContext(capturedCtx)
	if !ok {
		t.Fatal("no outgoing metadata in context")
	}

	// Custom header should still be present.
	custom := md.Get("x-custom-header")
	if len(custom) == 0 || custom[0] != "custom-value" {
		t.Errorf("custom header = %v, want [\"custom-value\"]", custom)
	}

	// Identity headers should also be present.
	idValues := md.Get(HeaderIdentityID)
	if len(idValues) == 0 || idValues[0] != "user-1" {
		t.Errorf("identity-id = %v, want [\"user-1\"]", idValues)
	}
}

// ---------------------------------------------------------------------------
// StreamClientInterceptor
// ---------------------------------------------------------------------------

// mockClientStream implements grpc.ClientStream for testing.
type mockClientStream struct {
	grpc.ClientStream
}

func TestStreamClientInterceptor_PropagatesIdentity(t *testing.T) {
	interceptor := StreamClientInterceptor("stream-service")

	identity := NewBasicIdentity("user-42", IdentityTypeUser, nil)
	ctx := ContextWithIdentity(context.Background(), identity)

	var capturedCtx context.Context
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		capturedCtx = ctx
		return &mockClientStream{}, nil
	}

	_, err := interceptor(ctx, &grpc.StreamDesc{}, nil, "/test.Service/Stream", streamer)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	md, ok := metadata.FromOutgoingContext(capturedCtx)
	if !ok {
		t.Fatal("no outgoing metadata in context")
	}

	idValues := md.Get(HeaderIdentityID)
	if len(idValues) == 0 || idValues[0] != "user-42" {
		t.Errorf("metadata identity-id = %v, want [\"user-42\"]", idValues)
	}
}

func TestStreamClientInterceptor_NoIdentity(t *testing.T) {
	interceptor := StreamClientInterceptor("stream-service")

	ctx := context.Background()

	var capturedCtx context.Context
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		capturedCtx = ctx
		return &mockClientStream{}, nil
	}

	_, err := interceptor(ctx, &grpc.StreamDesc{}, nil, "/test.Service/Stream", streamer)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}

	_, ok := metadata.FromOutgoingContext(capturedCtx)
	if ok {
		t.Error("outgoing metadata should not be set when no identity is present")
	}
}

// ---------------------------------------------------------------------------
// wrappedServerStream
// ---------------------------------------------------------------------------

func TestWrappedServerStream_OverridesContext(t *testing.T) {
	originalCtx := context.Background()
	enrichedCtx := context.WithValue(originalCtx, identityKey, newTestIdentity())

	stream := &mockServerStream{ctx: originalCtx}
	wrapped := &wrappedServerStream{ServerStream: stream, ctx: enrichedCtx}

	if wrapped.Context() != enrichedCtx {
		t.Error("wrappedServerStream.Context() did not return the enriched context")
	}
}
