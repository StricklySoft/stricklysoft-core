package auth

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// contextKey is an unexported type used for context keys in this package.
// Using a distinct type prevents collisions with keys from other packages.
type contextKey int

const (
	// identityKey stores the authenticated Identity in the context.
	identityKey contextKey = iota

	// callerServiceKey stores the name of the calling service in the context.
	callerServiceKey

	// callChainKey stores the CallChain tracking request provenance.
	callChainKey
)

// ContextWithIdentity returns a new context with the given Identity attached.
// The identity can later be retrieved with [IdentityFromContext].
//
// This is typically called by gRPC server interceptors and HTTP middleware
// after successfully validating an authentication token.
func ContextWithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityKey, identity)
}

// IdentityFromContext retrieves the Identity from the context.
// Returns the identity and true if present, or nil and false if no identity
// has been set. This function never returns a non-nil identity with false.
//
// Example:
//
//	identity, ok := auth.IdentityFromContext(ctx)
//	if !ok {
//	    return errors.Unauthorized("no identity in context")
//	}
//	log.Info("request from", "user", identity.ID(), "type", identity.Type())
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityKey).(Identity)
	return identity, ok
}

// MustIdentityFromContext retrieves the Identity from the context, panicking
// if no identity is present. This should only be used in code paths where
// an identity is guaranteed to exist (e.g., after authentication middleware).
func MustIdentityFromContext(ctx context.Context) Identity {
	identity, ok := IdentityFromContext(ctx)
	if !ok {
		panic("auth: no identity in context; ensure authentication middleware is configured")
	}
	return identity
}

// ContextWithCallerService returns a new context with the calling service name
// attached. This identifies which service forwarded the request.
//
// In a call chain User -> Gateway -> Orchestrator -> Agent, when the
// Orchestrator calls the Agent, it sets its own name ("orchestrator") as
// the caller service.
func ContextWithCallerService(ctx context.Context, serviceName string) context.Context {
	return context.WithValue(ctx, callerServiceKey, serviceName)
}

// CallerServiceFromContext retrieves the calling service name from the context.
// Returns the service name and true if present, or an empty string and false
// if no caller service has been set (indicating a direct client call).
func CallerServiceFromContext(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(callerServiceKey).(string)
	return name, ok
}

// ContextWithCallChain returns a new context with the given CallChain attached.
// The call chain tracks the full provenance of a request through the
// distributed system.
func ContextWithCallChain(ctx context.Context, chain *CallChain) context.Context {
	return context.WithValue(ctx, callChainKey, chain)
}

// CallChainFromContext retrieves the CallChain from the context.
// Returns the call chain and true if present, or nil and false if no
// call chain has been set (indicating this is the first service in the chain).
func CallChainFromContext(ctx context.Context) (*CallChain, bool) {
	chain, ok := ctx.Value(callChainKey).(*CallChain)
	return chain, ok
}

// TraceIDFromContext extracts the OpenTelemetry trace ID from the context.
// Returns the trace ID as a hex string and true if a valid trace is active,
// or an empty string and false if no trace is present.
//
// This allows correlating identity information with distributed traces,
// enabling operators to link authentication events to specific request flows.
func TraceIDFromContext(ctx context.Context) (string, bool) {
	spanCtx := trace.SpanFromContext(ctx).SpanContext()
	if !spanCtx.HasTraceID() {
		return "", false
	}
	return spanCtx.TraceID().String(), true
}

// SpanIDFromContext extracts the OpenTelemetry span ID from the context.
// Returns the span ID as a hex string and true if a valid span is active,
// or an empty string and false if no span is present.
func SpanIDFromContext(ctx context.Context) (string, bool) {
	spanCtx := trace.SpanFromContext(ctx).SpanContext()
	if !spanCtx.HasTraceID() {
		return "", false
	}
	return spanCtx.SpanID().String(), true
}
