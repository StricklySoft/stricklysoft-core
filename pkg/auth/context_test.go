package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestContextWithIdentity_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	identity := NewBasicIdentity("user-42", IdentityTypeUser, map[string]any{"email": "test@example.com"})

	ctx = ContextWithIdentity(ctx, identity)

	got, ok := IdentityFromContext(ctx)
	require.True(t, ok, "IdentityFromContext returned false, want true")
	assert.Equal(t, "user-42", got.ID())
	assert.Equal(t, IdentityTypeUser, got.Type())
}

func TestIdentityFromContext_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	got, ok := IdentityFromContext(ctx)
	assert.False(t, ok, "IdentityFromContext returned true on empty context, want false")
	assert.Nil(t, got, "IdentityFromContext returned non-nil identity on empty context")
}

func TestMustIdentityFromContext_Panics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	defer func() {
		r := recover()
		assert.NotNil(t, r, "MustIdentityFromContext did not panic on empty context")
	}()

	MustIdentityFromContext(ctx)
}

func TestMustIdentityFromContext_Returns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	identity := NewBasicIdentity("user-1", IdentityTypeUser, nil)
	ctx = ContextWithIdentity(ctx, identity)

	got := MustIdentityFromContext(ctx)
	assert.Equal(t, "user-1", got.ID())
}

func TestContextWithCallerService_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ctx = ContextWithCallerService(ctx, "api-gateway")

	got, ok := CallerServiceFromContext(ctx)
	require.True(t, ok, "CallerServiceFromContext returned false, want true")
	assert.Equal(t, "api-gateway", got)
}

func TestCallerServiceFromContext_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	got, ok := CallerServiceFromContext(ctx)
	assert.False(t, ok, "CallerServiceFromContext returned true on empty context, want false")
	assert.Equal(t, "", got, "CallerServiceFromContext should return empty string on empty context")
}

func TestContextWithCallChain_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	chain := &CallChain{
		OriginalID:   "user-1",
		OriginalType: IdentityTypeUser,
		Callers: []CallerInfo{
			{ServiceName: "gateway", IdentityID: "svc-gw", IdentityType: IdentityTypeService},
		},
	}

	ctx = ContextWithCallChain(ctx, chain)

	got, ok := CallChainFromContext(ctx)
	require.True(t, ok, "CallChainFromContext returned false, want true")
	assert.Equal(t, "user-1", got.OriginalID)
	require.Len(t, got.Callers, 1)
	assert.Equal(t, "gateway", got.Callers[0].ServiceName)
}

func TestCallChainFromContext_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	got, ok := CallChainFromContext(ctx)
	assert.False(t, ok, "CallChainFromContext returned true on empty context, want false")
	assert.Nil(t, got, "CallChainFromContext returned non-nil on empty context")
}

func TestTraceIDFromContext_NoTrace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	traceID, ok := TraceIDFromContext(ctx)
	assert.False(t, ok, "TraceIDFromContext returned true with no trace, want false")
	assert.Equal(t, "", traceID, "TraceIDFromContext should return empty string")
}

func TestTraceIDFromContext_WithTrace(t *testing.T) {
	t.Parallel()
	// Create a span context with a known trace ID.
	traceIDBytes := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanIDBytes := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID(traceIDBytes),
		SpanID:     trace.SpanID(spanIDBytes),
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	traceID, ok := TraceIDFromContext(ctx)
	require.True(t, ok, "TraceIDFromContext returned false, want true")
	require.NotEmpty(t, traceID, "TraceIDFromContext returned empty string, want non-empty")
	// Verify it's a valid hex string of expected length (32 hex chars for 16 bytes).
	assert.Len(t, traceID, 32)
}

func TestSpanIDFromContext_NoTrace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	spanID, ok := SpanIDFromContext(ctx)
	assert.False(t, ok, "SpanIDFromContext returned true with no trace, want false")
	assert.Equal(t, "", spanID, "SpanIDFromContext should return empty string")
}

func TestSpanIDFromContext_WithTrace(t *testing.T) {
	t.Parallel()
	traceIDBytes := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanIDBytes := [8]byte{10, 20, 30, 40, 50, 60, 70, 80}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID(traceIDBytes),
		SpanID:     trace.SpanID(spanIDBytes),
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	spanID, ok := SpanIDFromContext(ctx)
	require.True(t, ok, "SpanIDFromContext returned false, want true")
	// 16 hex chars for 8 bytes.
	assert.Len(t, spanID, 16)
}

func TestSpanIDFromContext_TraceIDOnlyNoSpanID(t *testing.T) {
	t.Parallel()
	// A SpanContext with a valid TraceID but a zero SpanID should NOT
	// return a span ID. This verifies that SpanIDFromContext checks
	// HasSpanID() rather than HasTraceID().
	traceIDBytes := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID(traceIDBytes),
		SpanID:     trace.SpanID([8]byte{}), // zero span ID
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	spanID, ok := SpanIDFromContext(ctx)
	assert.False(t, ok, "SpanIDFromContext returned true with zero SpanID, want false")
	assert.Equal(t, "", spanID, "SpanIDFromContext should return empty string")
}

// TestContextKeys_Independent verifies that different context keys do not
// interfere with each other.
func TestContextKeys_Independent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	identity := NewBasicIdentity("user-1", IdentityTypeUser, nil)
	chain := &CallChain{OriginalID: "user-1", OriginalType: IdentityTypeUser}

	ctx = ContextWithIdentity(ctx, identity)
	ctx = ContextWithCallerService(ctx, "gateway")
	ctx = ContextWithCallChain(ctx, chain)

	// All three values should be independently retrievable.
	gotID, ok := IdentityFromContext(ctx)
	require.True(t, ok, "Identity not retrievable after setting all context values")
	assert.Equal(t, "user-1", gotID.ID())

	gotCaller, ok := CallerServiceFromContext(ctx)
	require.True(t, ok, "CallerService not retrievable after setting all context values")
	assert.Equal(t, "gateway", gotCaller)

	gotChain, ok := CallChainFromContext(ctx)
	require.True(t, ok, "CallChain not retrievable after setting all context values")
	assert.Equal(t, "user-1", gotChain.OriginalID)
}
