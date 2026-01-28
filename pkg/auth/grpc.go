package auth

import (
	"context"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor returns a gRPC unary server interceptor that extracts
// and validates identity from incoming request metadata.
//
// The interceptor performs the following steps:
//  1. Extracts the "authorization" metadata value (bearer token)
//  2. Validates the token using the provided [TokenValidator]
//  3. Stores the resulting [Identity] in the request context
//  4. Extracts propagated caller service and call chain metadata
//  5. Passes the enriched context to the handler
//
// If no authorization metadata is present or the token is invalid, the
// interceptor returns a gRPC Unauthenticated error.
//
// The serviceName parameter identifies the current service for call chain
// tracking. It is recorded as the receiving service in audit logs.
func UnaryServerInterceptor(validator TokenValidator, serviceName string) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		ctx, err := extractIdentityFromGRPC(ctx, validator, serviceName)
		if err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor returns a gRPC stream server interceptor that extracts
// and validates identity from incoming stream metadata.
//
// This interceptor performs the same authentication steps as
// [UnaryServerInterceptor] but wraps the stream to carry the enriched context.
func StreamServerInterceptor(validator TokenValidator, serviceName string) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		ctx, err := extractIdentityFromGRPC(ss.Context(), validator, serviceName)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedServerStream{ServerStream: ss, ctx: ctx})
	}
}

// UnaryClientInterceptor returns a gRPC unary client interceptor that propagates
// identity from the context to outgoing request metadata.
//
// The interceptor performs the following steps:
//  1. Retrieves the [Identity] from the context (if present)
//  2. Serializes identity ID, type, and claims into gRPC metadata
//  3. Includes the caller service name and call chain for audit
//  4. Merges the identity metadata with any existing outgoing metadata
//
// If no identity is in the context, the request proceeds without identity
// metadata (allowing unauthenticated service-to-service calls where appropriate).
//
// The serviceName parameter identifies the current service in the call chain.
func UnaryClientInterceptor(serviceName string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = propagateIdentityToGRPC(ctx, serviceName)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// StreamClientInterceptor returns a gRPC stream client interceptor that
// propagates identity from the context to outgoing stream metadata.
//
// This interceptor performs the same propagation steps as
// [UnaryClientInterceptor].
func StreamClientInterceptor(serviceName string) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx = propagateIdentityToGRPC(ctx, serviceName)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

// extractIdentityFromGRPC extracts identity from incoming gRPC metadata,
// validates the bearer token, and enriches the context with identity
// information and call chain data.
func extractIdentityFromGRPC(ctx context.Context, validator TokenValidator, serviceName string) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx, status.Error(codes.Unauthenticated, "missing metadata")
	}

	// Extract and validate the bearer token.
	tokens := md.Get(HeaderAuthorization)
	if len(tokens) == 0 {
		return ctx, status.Error(codes.Unauthenticated, "missing authorization metadata")
	}
	token := ExtractBearerToken(tokens[0])
	if token == "" {
		return ctx, status.Error(codes.Unauthenticated, "invalid authorization format")
	}

	identity, err := validator.Validate(ctx, token)
	if err != nil {
		return ctx, status.Error(codes.Unauthenticated, "token validation failed")
	}

	// Store the validated identity in the context.
	ctx = ContextWithIdentity(ctx, identity)

	// Extract propagated caller service from metadata.
	if callers := md.Get(HeaderCallerService); len(callers) > 0 && callers[0] != "" {
		ctx = ContextWithCallerService(ctx, callers[0])
	}

	// Extract and reconstruct the call chain from metadata.
	if chains := md.Get(HeaderCallChain); len(chains) > 0 && chains[0] != "" {
		chain, err := DeserializeCallChain(chains[0])
		if err != nil {
			// Log the error but don't fail the request — a malformed call
			// chain header should not prevent processing. The identity itself
			// was already validated.
			slog.WarnContext(ctx, "auth: failed to deserialize call chain from gRPC metadata",
				"error", err,
				"service", serviceName,
			)
		} else if chain != nil {
			ctx = ContextWithCallChain(ctx, chain)
		}
	}

	return ctx, nil
}

// propagateIdentityToGRPC adds identity information from the context to
// outgoing gRPC metadata for downstream services.
func propagateIdentityToGRPC(ctx context.Context, serviceName string) context.Context {
	identity, ok := IdentityFromContext(ctx)
	if !ok {
		return ctx
	}

	// Build the call chain. If a chain already exists in the context,
	// append the current service. Otherwise, start a new chain.
	chain, _ := CallChainFromContext(ctx)
	if chain == nil {
		chain = &CallChain{
			OriginalID:   identity.ID(),
			OriginalType: identity.Type(),
		}
	}
	chain = chain.AppendCaller(CallerInfo{
		ServiceName:  serviceName,
		IdentityID:   identity.ID(),
		IdentityType: identity.Type(),
	})

	headers, err := identityToHeaders(identity, serviceName, chain)
	if err != nil {
		// Log but don't fail — identity propagation failure should not
		// prevent the outgoing call. The downstream service will simply
		// not receive identity context and will require its own authentication.
		slog.WarnContext(ctx, "auth: failed to serialize identity for gRPC propagation",
			"error", err,
			"service", serviceName,
		)
		return ctx
	}

	// Convert headers to metadata pairs.
	pairs := make([]string, 0, len(headers)*2)
	for k, v := range headers {
		pairs = append(pairs, strings.ToLower(k), v)
	}
	md := metadata.Pairs(pairs...)

	// Merge with any existing outgoing metadata.
	existingMD, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		md = metadata.Join(existingMD, md)
	}

	return metadata.NewOutgoingContext(ctx, md)
}

// wrappedServerStream wraps a grpc.ServerStream to override its Context method.
// This is necessary because ServerStream.Context() returns the original stream
// context, which does not contain the identity added by the interceptor.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the wrapped context containing identity information.
func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}
