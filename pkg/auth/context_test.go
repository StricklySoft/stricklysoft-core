package auth

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestContextWithIdentity_RoundTrip(t *testing.T) {
	ctx := context.Background()
	identity := NewBasicIdentity("user-42", IdentityTypeUser, map[string]any{"email": "test@example.com"})

	ctx = ContextWithIdentity(ctx, identity)

	got, ok := IdentityFromContext(ctx)
	if !ok {
		t.Fatal("IdentityFromContext returned false, want true")
	}
	if got.ID() != "user-42" {
		t.Errorf("ID() = %q, want %q", got.ID(), "user-42")
	}
	if got.Type() != IdentityTypeUser {
		t.Errorf("Type() = %q, want %q", got.Type(), IdentityTypeUser)
	}
}

func TestIdentityFromContext_Empty(t *testing.T) {
	ctx := context.Background()

	got, ok := IdentityFromContext(ctx)
	if ok {
		t.Error("IdentityFromContext returned true on empty context, want false")
	}
	if got != nil {
		t.Error("IdentityFromContext returned non-nil identity on empty context")
	}
}

func TestMustIdentityFromContext_Panics(t *testing.T) {
	ctx := context.Background()

	defer func() {
		r := recover()
		if r == nil {
			t.Error("MustIdentityFromContext did not panic on empty context")
		}
	}()

	MustIdentityFromContext(ctx)
}

func TestMustIdentityFromContext_Returns(t *testing.T) {
	ctx := context.Background()
	identity := NewBasicIdentity("user-1", IdentityTypeUser, nil)
	ctx = ContextWithIdentity(ctx, identity)

	got := MustIdentityFromContext(ctx)
	if got.ID() != "user-1" {
		t.Errorf("ID() = %q, want %q", got.ID(), "user-1")
	}
}

func TestContextWithCallerService_RoundTrip(t *testing.T) {
	ctx := context.Background()
	ctx = ContextWithCallerService(ctx, "api-gateway")

	got, ok := CallerServiceFromContext(ctx)
	if !ok {
		t.Fatal("CallerServiceFromContext returned false, want true")
	}
	if got != "api-gateway" {
		t.Errorf("CallerServiceFromContext = %q, want %q", got, "api-gateway")
	}
}

func TestCallerServiceFromContext_Empty(t *testing.T) {
	ctx := context.Background()

	got, ok := CallerServiceFromContext(ctx)
	if ok {
		t.Error("CallerServiceFromContext returned true on empty context, want false")
	}
	if got != "" {
		t.Errorf("CallerServiceFromContext = %q on empty context, want empty string", got)
	}
}

func TestContextWithCallChain_RoundTrip(t *testing.T) {
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
	if !ok {
		t.Fatal("CallChainFromContext returned false, want true")
	}
	if got.OriginalID != "user-1" {
		t.Errorf("OriginalID = %q, want %q", got.OriginalID, "user-1")
	}
	if len(got.Callers) != 1 {
		t.Fatalf("Callers has %d entries, want 1", len(got.Callers))
	}
	if got.Callers[0].ServiceName != "gateway" {
		t.Errorf("Callers[0].ServiceName = %q, want %q", got.Callers[0].ServiceName, "gateway")
	}
}

func TestCallChainFromContext_Empty(t *testing.T) {
	ctx := context.Background()

	got, ok := CallChainFromContext(ctx)
	if ok {
		t.Error("CallChainFromContext returned true on empty context, want false")
	}
	if got != nil {
		t.Error("CallChainFromContext returned non-nil on empty context")
	}
}

func TestTraceIDFromContext_NoTrace(t *testing.T) {
	ctx := context.Background()

	traceID, ok := TraceIDFromContext(ctx)
	if ok {
		t.Error("TraceIDFromContext returned true with no trace, want false")
	}
	if traceID != "" {
		t.Errorf("TraceIDFromContext = %q, want empty string", traceID)
	}
}

func TestTraceIDFromContext_WithTrace(t *testing.T) {
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
	if !ok {
		t.Fatal("TraceIDFromContext returned false, want true")
	}
	if traceID == "" {
		t.Fatal("TraceIDFromContext returned empty string, want non-empty")
	}
	// Verify it's a valid hex string of expected length (32 hex chars for 16 bytes).
	if len(traceID) != 32 {
		t.Errorf("TraceID length = %d, want 32", len(traceID))
	}
}

func TestSpanIDFromContext_NoTrace(t *testing.T) {
	ctx := context.Background()

	spanID, ok := SpanIDFromContext(ctx)
	if ok {
		t.Error("SpanIDFromContext returned true with no trace, want false")
	}
	if spanID != "" {
		t.Errorf("SpanIDFromContext = %q, want empty string", spanID)
	}
}

func TestSpanIDFromContext_WithTrace(t *testing.T) {
	traceIDBytes := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanIDBytes := [8]byte{10, 20, 30, 40, 50, 60, 70, 80}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID(traceIDBytes),
		SpanID:     trace.SpanID(spanIDBytes),
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	spanID, ok := SpanIDFromContext(ctx)
	if !ok {
		t.Fatal("SpanIDFromContext returned false, want true")
	}
	// 16 hex chars for 8 bytes.
	if len(spanID) != 16 {
		t.Errorf("SpanID length = %d, want 16", len(spanID))
	}
}

// TestContextKeys_Independent verifies that different context keys do not
// interfere with each other.
func TestContextKeys_Independent(t *testing.T) {
	ctx := context.Background()

	identity := NewBasicIdentity("user-1", IdentityTypeUser, nil)
	chain := &CallChain{OriginalID: "user-1", OriginalType: IdentityTypeUser}

	ctx = ContextWithIdentity(ctx, identity)
	ctx = ContextWithCallerService(ctx, "gateway")
	ctx = ContextWithCallChain(ctx, chain)

	// All three values should be independently retrievable.
	gotID, ok := IdentityFromContext(ctx)
	if !ok || gotID.ID() != "user-1" {
		t.Error("Identity not retrievable after setting all context values")
	}

	gotCaller, ok := CallerServiceFromContext(ctx)
	if !ok || gotCaller != "gateway" {
		t.Error("CallerService not retrievable after setting all context values")
	}

	gotChain, ok := CallChainFromContext(ctx)
	if !ok || gotChain.OriginalID != "user-1" {
		t.Error("CallChain not retrievable after setting all context values")
	}
}
