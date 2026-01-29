package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	t.Parallel()
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
	require.NoError(t, err, "interceptor returned error")
	assert.Equal(t, "response", resp)

	// Verify identity was set in context.
	identity, ok := IdentityFromContext(capturedCtx)
	require.True(t, ok, "identity not found in context after interceptor")
	assert.Equal(t, "user-42", identity.ID())
}

func TestUnaryServerInterceptor_MissingMetadata(t *testing.T) {
	t.Parallel()
	validator := &mockValidator{identity: newTestIdentity()}
	interceptor := UnaryServerInterceptor(validator, "test-service")

	// No metadata in context.
	ctx := context.Background()
	handler := func(ctx context.Context, req any) (any, error) {
		t.Error("handler should not be called when metadata is missing")
		return nil, nil
	}

	_, err := interceptor(ctx, "request", &grpc.UnaryServerInfo{}, handler)
	require.Error(t, err, "interceptor should return error when metadata is missing")
	st, ok := status.FromError(err)
	require.True(t, ok, "error is not a gRPC status: %v", err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestUnaryServerInterceptor_MissingAuthorizationHeader(t *testing.T) {
	t.Parallel()
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
	require.Error(t, err, "interceptor should return error when authorization is missing")
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestUnaryServerInterceptor_InvalidToken(t *testing.T) {
	t.Parallel()
	validator := &mockValidator{err: errors.New("token expired")}
	interceptor := UnaryServerInterceptor(validator, "test-service")

	md := metadata.Pairs(HeaderAuthorization, "Bearer expired-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	handler := func(ctx context.Context, req any) (any, error) {
		t.Error("handler should not be called when token is invalid")
		return nil, nil
	}

	_, err := interceptor(ctx, "request", &grpc.UnaryServerInfo{}, handler)
	require.Error(t, err, "interceptor should return error when token is invalid")
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestUnaryServerInterceptor_InvalidBearerFormat(t *testing.T) {
	t.Parallel()
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
	require.Error(t, err, "interceptor should return error for non-Bearer auth")
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestUnaryServerInterceptor_WithCallerService(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "interceptor returned error")

	caller, ok := CallerServiceFromContext(capturedCtx)
	require.True(t, ok, "caller service not found in context")
	assert.Equal(t, "upstream-gateway", caller)
}

func TestUnaryServerInterceptor_WithCallChain(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "SerializeCallChain error")

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
	require.NoError(t, err, "interceptor returned error")

	gotChain, ok := CallChainFromContext(capturedCtx)
	require.True(t, ok, "call chain not found in context")
	assert.Equal(t, "user-42", gotChain.OriginalID)
	require.Len(t, gotChain.Callers, 1)
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
	t.Parallel()
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
	require.NoError(t, err, "interceptor returned error")

	identity, ok := IdentityFromContext(capturedCtx)
	require.True(t, ok, "identity not found in stream context")
	assert.Equal(t, "user-42", identity.ID())
}

func TestStreamServerInterceptor_MissingAuth(t *testing.T) {
	t.Parallel()
	validator := &mockValidator{identity: newTestIdentity()}
	interceptor := StreamServerInterceptor(validator, "test-service")

	ctx := context.Background()
	stream := &mockServerStream{ctx: ctx}

	handler := func(srv any, ss grpc.ServerStream) error {
		t.Error("handler should not be called")
		return nil
	}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, handler)
	require.Error(t, err, "interceptor should return error when auth is missing")
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

// ---------------------------------------------------------------------------
// UnaryClientInterceptor
// ---------------------------------------------------------------------------

func TestUnaryClientInterceptor_PropagatesIdentity(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "interceptor returned error")

	// Verify outgoing metadata contains identity headers.
	md, ok := metadata.FromOutgoingContext(capturedCtx)
	require.True(t, ok, "no outgoing metadata in context")

	idValues := md.Get(HeaderIdentityID)
	require.NotEmpty(t, idValues)
	assert.Equal(t, "user-42", idValues[0])

	typeValues := md.Get(HeaderIdentityType)
	require.NotEmpty(t, typeValues)
	assert.Equal(t, "user", typeValues[0])

	callerValues := md.Get(HeaderCallerService)
	require.NotEmpty(t, callerValues)
	assert.Equal(t, "client-service", callerValues[0])

	chainValues := md.Get(HeaderCallChain)
	require.NotEmpty(t, chainValues, "metadata call-chain is missing")
	chain, err := DeserializeCallChain(chainValues[0])
	require.NoError(t, err, "DeserializeCallChain error")
	assert.Equal(t, "user-42", chain.OriginalID)
	require.Len(t, chain.Callers, 1)
	assert.Equal(t, "client-service", chain.Callers[0].ServiceName)
}

func TestUnaryClientInterceptor_NoIdentity(t *testing.T) {
	t.Parallel()
	interceptor := UnaryClientInterceptor("client-service")

	// Context without identity — interceptor should pass through without modification.
	ctx := context.Background()

	var capturedCtx context.Context
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		capturedCtx = ctx
		return nil
	}

	err := interceptor(ctx, "/test.Service/Method", "req", "reply", nil, invoker)
	require.NoError(t, err, "interceptor returned error")

	// No outgoing metadata should be added.
	_, ok := metadata.FromOutgoingContext(capturedCtx)
	assert.False(t, ok, "outgoing metadata should not be set when no identity is present")
}

func TestUnaryClientInterceptor_ExistingCallChain(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "interceptor returned error")

	md, ok := metadata.FromOutgoingContext(capturedCtx)
	require.True(t, ok, "no outgoing metadata in context")

	chainValues := md.Get(HeaderCallChain)
	require.NotEmpty(t, chainValues, "call-chain metadata missing")
	chain, err := DeserializeCallChain(chainValues[0])
	require.NoError(t, err, "DeserializeCallChain error")
	// Should have the existing caller plus the new one.
	require.Len(t, chain.Callers, 2)
	assert.Equal(t, "gateway", chain.Callers[0].ServiceName)
	assert.Equal(t, "downstream-service", chain.Callers[1].ServiceName)
}

func TestUnaryClientInterceptor_PreservesExistingMetadata(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, err, "interceptor returned error")

	md, ok := metadata.FromOutgoingContext(capturedCtx)
	require.True(t, ok, "no outgoing metadata in context")

	// Custom header should still be present.
	custom := md.Get("x-custom-header")
	require.NotEmpty(t, custom)
	assert.Equal(t, "custom-value", custom[0])

	// Identity headers should also be present.
	idValues := md.Get(HeaderIdentityID)
	require.NotEmpty(t, idValues)
	assert.Equal(t, "user-1", idValues[0])
}

// ---------------------------------------------------------------------------
// StreamClientInterceptor
// ---------------------------------------------------------------------------

// mockClientStream implements grpc.ClientStream for testing.
type mockClientStream struct {
	grpc.ClientStream
}

func TestStreamClientInterceptor_PropagatesIdentity(t *testing.T) {
	t.Parallel()
	interceptor := StreamClientInterceptor("stream-service")

	identity := NewBasicIdentity("user-42", IdentityTypeUser, nil)
	ctx := ContextWithIdentity(context.Background(), identity)

	var capturedCtx context.Context
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		capturedCtx = ctx
		return &mockClientStream{}, nil
	}

	_, err := interceptor(ctx, &grpc.StreamDesc{}, nil, "/test.Service/Stream", streamer)
	require.NoError(t, err, "interceptor returned error")

	md, ok := metadata.FromOutgoingContext(capturedCtx)
	require.True(t, ok, "no outgoing metadata in context")

	idValues := md.Get(HeaderIdentityID)
	require.NotEmpty(t, idValues)
	assert.Equal(t, "user-42", idValues[0])
}

func TestStreamClientInterceptor_NoIdentity(t *testing.T) {
	t.Parallel()
	interceptor := StreamClientInterceptor("stream-service")

	ctx := context.Background()

	var capturedCtx context.Context
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		capturedCtx = ctx
		return &mockClientStream{}, nil
	}

	_, err := interceptor(ctx, &grpc.StreamDesc{}, nil, "/test.Service/Stream", streamer)
	require.NoError(t, err, "interceptor returned error")

	_, ok := metadata.FromOutgoingContext(capturedCtx)
	assert.False(t, ok, "outgoing metadata should not be set when no identity is present")
}

// ---------------------------------------------------------------------------
// wrappedServerStream
// ---------------------------------------------------------------------------

func TestWrappedServerStream_OverridesContext(t *testing.T) {
	t.Parallel()
	originalCtx := context.Background()
	enrichedCtx := context.WithValue(originalCtx, identityKey, newTestIdentity())

	stream := &mockServerStream{ctx: originalCtx}
	wrapped := &wrappedServerStream{ServerStream: stream, ctx: enrichedCtx}

	assert.Equal(t, enrichedCtx, wrapped.Context(), "wrappedServerStream.Context() did not return the enriched context")
}
