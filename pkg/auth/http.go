package auth

import (
	"log/slog"
	"net/http"
)

// HTTPMiddleware returns an HTTP middleware that extracts and validates
// identity from incoming request headers.
//
// The middleware performs the following steps:
//  1. Extracts the "Authorization" header (bearer token)
//  2. Validates the token using the provided [TokenValidator]
//  3. Stores the resulting [Identity] in the request context
//  4. Extracts propagated caller service and call chain headers
//  5. Passes the enriched request to the next handler
//
// If no Authorization header is present or the token is invalid, the
// middleware responds with HTTP 401 Unauthorized.
//
// The serviceName parameter identifies the current service for call chain
// tracking.
//
// Example:
//
//	mux := http.NewServeMux()
//	mux.HandleFunc("/api/data", handleData)
//	handler := auth.HTTPMiddleware(validator, "my-service")(mux)
//	http.ListenAndServe(":8080", handler)
func HTTPMiddleware(validator TokenValidator, serviceName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract the bearer token from the Authorization header.
			authHeader := r.Header.Get(HeaderAuthorization)
			token := ExtractBearerToken(authHeader)
			if token == "" {
				http.Error(w, "missing or invalid authorization header", http.StatusUnauthorized)
				return
			}

			// Validate the token and extract the identity.
			ctx := r.Context()
			identity, err := validator.Validate(ctx, token)
			if err != nil {
				http.Error(w, "token validation failed", http.StatusUnauthorized)
				return
			}

			// Store the validated identity in the request context.
			ctx = ContextWithIdentity(ctx, identity)

			// Extract propagated caller service header.
			if caller := r.Header.Get(HeaderCallerService); caller != "" {
				ctx = ContextWithCallerService(ctx, caller)
			}

			// Extract and reconstruct the call chain.
			if chainHeader := r.Header.Get(HeaderCallChain); chainHeader != "" {
				chain, err := DeserializeCallChain(chainHeader)
				if err != nil {
					// Log but don't fail — the identity was already validated.
					slog.WarnContext(ctx, "auth: failed to deserialize call chain from HTTP header",
						"error", err,
						"service", serviceName,
					)
				} else if chain != nil {
					ctx = ContextWithCallChain(ctx, chain)
				}
			}

			// Continue with the enriched context.
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PropagatingRoundTripper wraps an [http.RoundTripper] to propagate identity
// context to outgoing HTTP requests. It reads the identity, caller service,
// and call chain from the request context and adds them as HTTP headers.
//
// This is used when a service needs to make outgoing HTTP calls to downstream
// services while preserving the identity context for authorization and audit.
//
// Example:
//
//	client := &http.Client{
//	    Transport: auth.NewPropagatingRoundTripper("my-service", http.DefaultTransport),
//	}
//	// Requests made with this client will automatically include identity headers.
//	resp, err := client.Do(req.WithContext(ctx))
type PropagatingRoundTripper struct {
	// serviceName identifies the current service in the call chain.
	serviceName string

	// wrapped is the underlying RoundTripper that performs the actual HTTP call.
	wrapped http.RoundTripper
}

// NewPropagatingRoundTripper creates a new PropagatingRoundTripper that wraps
// the given transport. If transport is nil, [http.DefaultTransport] is used.
//
// The serviceName parameter identifies the current service in the call chain.
func NewPropagatingRoundTripper(serviceName string, transport http.RoundTripper) *PropagatingRoundTripper {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &PropagatingRoundTripper{
		serviceName: serviceName,
		wrapped:     transport,
	}
}

// RoundTrip executes the HTTP request with identity headers injected from
// the request context. If no identity is present in the context, the request
// proceeds without modification.
//
// RoundTrip implements the [http.RoundTripper] interface.
func (t *PropagatingRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	identity, ok := IdentityFromContext(r.Context())
	if !ok {
		return t.wrapped.RoundTrip(r)
	}

	// Build the call chain, appending the current service.
	chain, _ := CallChainFromContext(r.Context())
	if chain == nil {
		chain = &CallChain{
			OriginalID:   identity.ID(),
			OriginalType: identity.Type(),
		}
	}
	chain = chain.AppendCaller(CallerInfo{
		ServiceName:  t.serviceName,
		IdentityID:   identity.ID(),
		IdentityType: identity.Type(),
	})

	headers, err := identityToHeaders(identity, t.serviceName, chain)
	if err != nil {
		// Log but don't fail — propagation failure should not prevent
		// the outgoing request.
		slog.WarnContext(r.Context(), "auth: failed to serialize identity for HTTP propagation",
			"error", err,
			"service", t.serviceName,
		)
		return t.wrapped.RoundTrip(r)
	}

	// Clone the request to avoid mutating the original.
	clone := r.Clone(r.Context())
	for k, v := range headers {
		clone.Header.Set(k, v)
	}

	return t.wrapped.RoundTrip(clone)
}
