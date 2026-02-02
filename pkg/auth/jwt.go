package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ---------------------------------------------------------------------------
// Secret type — prevents accidental logging of sensitive values
// ---------------------------------------------------------------------------

// Secret is a string type that redacts its value in String(), GoString(), and
// MarshalText() to prevent accidental exposure in logs, JSON output, or
// fmt.Printf. The actual value is only accessible via the [Secret.Value]
// method, which should be called only where the raw value is truly needed
// (e.g., passing to a cryptographic function).
type Secret string

// secretRedacted is the placeholder text shown instead of the actual secret
// value when the secret is printed, formatted, or serialized.
const secretRedacted = "[REDACTED]"

// String returns the redacted placeholder, preventing the secret from being
// printed via fmt.Println, log.Printf, or similar functions.
func (s Secret) String() string { return secretRedacted }

// GoString returns the redacted placeholder, preventing the secret from being
// printed via fmt.Printf("%#v", secret).
func (s Secret) GoString() string { return secretRedacted }

// Value returns the actual secret string. This is the only way to access the
// underlying value and should be used only where the raw secret is required
// (e.g., passing to a cryptographic signing or verification function).
func (s Secret) Value() string { return string(s) }

// MarshalText implements [encoding.TextMarshaler], returning the redacted
// placeholder. This prevents the secret from leaking into JSON, YAML, or
// any other text-based serialization format.
func (s Secret) MarshalText() ([]byte, error) { return []byte(secretRedacted), nil }

// ---------------------------------------------------------------------------
// TokenType — identifies the type/source of a JWT
// ---------------------------------------------------------------------------

// TokenType identifies the authentication provider that issued a JWT.
// The validator uses the token type to route validation to the appropriate
// verification path (HMAC for platform, JWKS for OIDC/Kubernetes).
type TokenType string

const (
	// TokenTypeKubernetes identifies tokens issued by the Kubernetes API
	// server for ServiceAccount authentication. These tokens are verified
	// using the Kubernetes OIDC discovery endpoint.
	TokenTypeKubernetes TokenType = "kubernetes"

	// TokenTypePlatform identifies tokens issued by the StricklySoft platform
	// itself, signed with a shared HMAC secret (HS256). These are used for
	// service-to-service communication within the platform.
	TokenTypePlatform TokenType = "platform"

	// TokenTypeOIDC identifies tokens issued by an external OpenID Connect
	// provider (e.g., Auth0, Okta, Azure AD). These are verified using
	// JWKS fetched from the provider's discovery endpoint.
	TokenTypeOIDC TokenType = "oidc"
)

// ---------------------------------------------------------------------------
// HTTPClient interface
// ---------------------------------------------------------------------------

// HTTPClient abstracts the HTTP client used for fetching JWKS and OIDC
// discovery documents. This allows callers to provide custom HTTP clients
// with specific timeouts, transport settings, or middleware (e.g., for
// mTLS, proxy configuration, or request tracing).
//
// The standard [http.Client] satisfies this interface.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ---------------------------------------------------------------------------
// ValidatorConfig — configuration for the JWT validator
// ---------------------------------------------------------------------------

// ValidatorConfig holds the configuration for [JWTValidator]. It controls
// which token types are accepted, signing keys, OIDC discovery endpoints,
// caching behavior, and clock skew tolerance.
//
// At least one token type (Kubernetes, Platform, or OIDC) must be enabled.
// Each enabled type has its own required fields that are validated by the
// [ValidatorConfig.Validate] method.
type ValidatorConfig struct {
	// EnableKubernetes controls whether Kubernetes ServiceAccount tokens
	// are accepted. When enabled, the validator verifies tokens against
	// the Kubernetes OIDC discovery endpoint using RS256 or ES256.
	// Defaults to true.
	EnableKubernetes bool `json:"enable_kubernetes" env:"AUTH_ENABLE_KUBERNETES" envDefault:"true"`

	// EnablePlatform controls whether platform-issued HMAC tokens (HS256)
	// are accepted. When enabled, PlatformSigningKey must be set to a key
	// of at least 32 bytes. Defaults to false.
	EnablePlatform bool `json:"enable_platform" env:"AUTH_ENABLE_PLATFORM" envDefault:"false"`

	// EnableOIDC controls whether external OIDC provider tokens are
	// accepted. When enabled, OIDCIssuerURL must be set to the provider's
	// issuer URL for .well-known/openid-configuration discovery.
	// Defaults to false.
	EnableOIDC bool `json:"enable_oidc" env:"AUTH_ENABLE_OIDC" envDefault:"false"`

	// PlatformSigningKey is the HMAC signing key used to verify platform
	// tokens. Must be at least 32 bytes when EnablePlatform is true.
	// The Secret type prevents accidental logging of the key value.
	PlatformSigningKey Secret `json:"-" env:"AUTH_PLATFORM_SIGNING_KEY"`

	// PlatformIssuer is the expected "iss" claim in platform tokens.
	// Tokens with a different issuer are rejected. Defaults to
	// "stricklysoft-platform".
	PlatformIssuer string `json:"platform_issuer" env:"AUTH_PLATFORM_ISSUER" envDefault:"stricklysoft-platform"`

	// PlatformAudience is the expected "aud" claim in platform tokens.
	// If empty, the audience claim is not validated. This field is
	// optional.
	PlatformAudience string `json:"platform_audience,omitempty" env:"AUTH_PLATFORM_AUDIENCE"`

	// OIDCIssuerURL is the base URL of the external OIDC provider (e.g.,
	// "https://accounts.google.com"). The validator appends
	// "/.well-known/openid-configuration" to discover the JWKS endpoint.
	// Required when EnableOIDC is true.
	OIDCIssuerURL string `json:"oidc_issuer_url,omitempty" env:"AUTH_OIDC_ISSUER_URL"`

	// OIDCAudience is the expected "aud" claim in OIDC tokens. If empty,
	// the audience claim is not validated. This field is optional.
	OIDCAudience string `json:"oidc_audience,omitempty" env:"AUTH_OIDC_AUDIENCE"`

	// KubernetesIssuer is the expected "iss" claim in Kubernetes
	// ServiceAccount tokens. Defaults to
	// "https://kubernetes.default.svc.cluster.local".
	KubernetesIssuer string `json:"kubernetes_issuer" env:"AUTH_KUBERNETES_ISSUER" envDefault:"https://kubernetes.default.svc.cluster.local"`

	// KubernetesAudience is the expected "aud" claim in Kubernetes
	// ServiceAccount tokens. Defaults to
	// "https://kubernetes.default.svc.cluster.local".
	KubernetesAudience string `json:"kubernetes_audience" env:"AUTH_KUBERNETES_AUDIENCE" envDefault:"https://kubernetes.default.svc.cluster.local"`

	// TokenCacheTTL is the maximum time a validated token identity is
	// cached before re-validation is required. The actual cache TTL for
	// a token is the minimum of this value and the token's remaining
	// lifetime (exp - now). Must be non-negative. Defaults to 5 minutes.
	TokenCacheTTL time.Duration `json:"token_cache_ttl" env:"AUTH_TOKEN_CACHE_TTL" envDefault:"5m"`

	// TokenCacheMaxSize is the maximum number of entries in the token
	// cache. When the cache is full, expired entries are evicted first,
	// then the oldest entries are removed. Must be greater than zero.
	// Defaults to 10000.
	TokenCacheMaxSize int `json:"token_cache_max_size" env:"AUTH_TOKEN_CACHE_MAX_SIZE" envDefault:"10000"`

	// JWKSCacheTTL is the time a fetched JWKS response is cached before
	// being refreshed from the provider. Must be non-negative.
	// Defaults to 1 hour.
	JWKSCacheTTL time.Duration `json:"jwks_cache_ttl" env:"AUTH_JWKS_CACHE_TTL" envDefault:"1h"`

	// ClockSkew is the maximum allowed clock difference between the
	// validator and the token issuer. Tokens within this window of their
	// expiration or not-before times are still considered valid. Must be
	// non-negative. Defaults to 30 seconds.
	ClockSkew time.Duration `json:"clock_skew" env:"AUTH_CLOCK_SKEW" envDefault:"30s"`

	// PermissionMapper is a function that extracts permissions from JWT
	// claims. If nil, [DefaultClaimsToPermissions] is used. This allows
	// custom permission extraction logic for non-standard claim formats.
	PermissionMapper func(claims map[string]any) []Permission `json:"-"`

	// HTTPClient is the HTTP client used for fetching JWKS and OIDC
	// discovery documents. If nil, a default [http.Client] with a
	// 10-second timeout is used.
	HTTPClient HTTPClient `json:"-"`

	// SATokenPath is the filesystem path to the Kubernetes ServiceAccount
	// token file. If empty, defaults to [DefaultSATokenPath]
	// ("/var/run/secrets/kubernetes.io/serviceaccount/token").
	SATokenPath string `json:"sa_token_path,omitempty" env:"AUTH_K8S_SA_TOKEN_PATH"`
}

// DefaultSATokenPath is the default filesystem path to the Kubernetes
// ServiceAccount token, as mounted by the Kubernetes pod runtime.
const DefaultSATokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// maxTokenSize is the maximum accepted size for a JWT token string (8 KB).
// Tokens larger than this are rejected to prevent resource exhaustion.
const maxTokenSize = 8192

// Validate checks the configuration for logical correctness and returns
// a *[sserr.Error] with code [sserr.CodeValidation] if any field is invalid.
//
// Validation rules:
//   - At least one token type must be enabled
//   - If EnablePlatform: PlatformSigningKey must be at least 32 bytes
//   - If EnableOIDC: OIDCIssuerURL must not be empty
//   - TokenCacheTTL, JWKSCacheTTL, and ClockSkew must be non-negative
//   - TokenCacheMaxSize must be greater than zero
func (c *ValidatorConfig) Validate() *sserr.Error {
	if !c.EnableKubernetes && !c.EnablePlatform && !c.EnableOIDC {
		return sserr.New(sserr.CodeValidation, "auth: at least one token type must be enabled (kubernetes, platform, or oidc)")
	}

	if c.EnablePlatform {
		if len(c.PlatformSigningKey.Value()) < 32 {
			return sserr.New(sserr.CodeValidation, "auth: platform signing key must be at least 32 bytes")
		}
	}

	if c.EnableOIDC {
		if c.OIDCIssuerURL == "" {
			return sserr.New(sserr.CodeValidation, "auth: OIDC issuer URL must not be empty when OIDC is enabled")
		}
	}

	if c.TokenCacheTTL < 0 {
		return sserr.New(sserr.CodeValidation, "auth: token cache TTL must be non-negative")
	}

	if c.JWKSCacheTTL < 0 {
		return sserr.New(sserr.CodeValidation, "auth: JWKS cache TTL must be non-negative")
	}

	if c.ClockSkew < 0 {
		return sserr.New(sserr.CodeValidation, "auth: clock skew must be non-negative")
	}

	if c.TokenCacheMaxSize <= 0 {
		return sserr.New(sserr.CodeValidation, "auth: token cache max size must be greater than zero")
	}

	return nil
}

// DefaultValidatorConfig returns a ValidatorConfig with sensible defaults
// suitable for a Kubernetes-deployed service. Only Kubernetes token
// validation is enabled by default; platform and OIDC must be explicitly
// enabled and configured.
func DefaultValidatorConfig() ValidatorConfig {
	return ValidatorConfig{
		EnableKubernetes:   true,
		EnablePlatform:     false,
		EnableOIDC:         false,
		PlatformIssuer:     "stricklysoft-platform",
		KubernetesIssuer:   "https://kubernetes.default.svc.cluster.local",
		KubernetesAudience: "https://kubernetes.default.svc.cluster.local",
		TokenCacheTTL:      5 * time.Minute,
		TokenCacheMaxSize:  10000,
		JWKSCacheTTL:       1 * time.Hour,
		ClockSkew:          30 * time.Second,
		SATokenPath:        DefaultSATokenPath,
	}
}

// ---------------------------------------------------------------------------
// tokenCache — in-memory cache for validated token identities
// ---------------------------------------------------------------------------

// tokenCacheEntry stores a cached identity and its expiration time.
type tokenCacheEntry struct {
	identity  Identity
	expiresAt time.Time
}

// tokenCache provides an in-memory cache for validated token identities,
// keyed by the SHA-256 hash of the token string. This avoids re-parsing
// and re-validating tokens on every request.
type tokenCache struct {
	mu      sync.RWMutex
	entries map[string]*tokenCacheEntry
	maxSize int
	ttl     time.Duration
}

// newTokenCache creates a new token cache with the given TTL and maximum
// number of entries.
func newTokenCache(ttl time.Duration, maxSize int) *tokenCache {
	return &tokenCache{
		entries: make(map[string]*tokenCacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// get retrieves a cached identity by token hash. Returns the identity and
// true if the entry exists and has not expired, or nil and false otherwise.
func (c *tokenCache) get(tokenHash string) (Identity, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[tokenHash]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.identity, true
}

// put stores a validated identity in the cache. The effective cache TTL is
// the minimum of the configured TTL and the time remaining until the
// token's expiration (tokenExp). If the cache is at capacity, expired
// entries are evicted first; if still at capacity, the oldest entry is
// removed.
func (c *tokenCache) put(tokenHash string, identity Identity, tokenExp time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Calculate effective TTL: min(cache TTL, token remaining lifetime).
	ttl := c.ttl
	remaining := time.Until(tokenExp)
	if remaining > 0 && remaining < ttl {
		ttl = remaining
	}
	if ttl <= 0 {
		return // Token already expired; do not cache.
	}

	expiresAt := time.Now().Add(ttl)

	// Evict if at capacity.
	if len(c.entries) >= c.maxSize {
		c.evictExpiredLocked()
	}
	if len(c.entries) >= c.maxSize {
		// Evict the oldest entry (earliest expiresAt).
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, v := range c.entries {
			if first || v.expiresAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.expiresAt
				first = false
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}

	c.entries[tokenHash] = &tokenCacheEntry{
		identity:  identity,
		expiresAt: expiresAt,
	}
}

// evictExpired removes all expired entries from the cache. This method
// acquires the write lock and is safe for concurrent use.
func (c *tokenCache) evictExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpiredLocked()
}

// evictExpiredLocked removes all expired entries. Caller must hold the
// write lock.
func (c *tokenCache) evictExpiredLocked() {
	now := time.Now()
	for k, v := range c.entries {
		if now.After(v.expiresAt) {
			delete(c.entries, k)
		}
	}
}

// ---------------------------------------------------------------------------
// jwksCache — caches JWKS public keys for OIDC/Kubernetes validation
// ---------------------------------------------------------------------------

// jwksCacheEntry stores fetched JWKS keys and the time they were fetched.
type jwksCacheEntry struct {
	keys      map[string]any // kid -> *rsa.PublicKey or *ecdsa.PublicKey
	fetchedAt time.Time
}

// jwksCache caches JSON Web Key Sets (JWKS) fetched from OIDC providers
// and Kubernetes API servers. Keys are cached per JWKS URL and refreshed
// after the configured TTL expires.
type jwksCache struct {
	mu      sync.RWMutex
	entries map[string]*jwksCacheEntry
	ttl     time.Duration
	client  HTTPClient
}

// newJWKSCache creates a new JWKS cache with the given TTL and HTTP client.
func newJWKSCache(ttl time.Duration, client HTTPClient) *jwksCache {
	return &jwksCache{
		entries: make(map[string]*jwksCacheEntry),
		ttl:     ttl,
		client:  client,
	}
}

// getKey retrieves a public key by key ID (kid) from the JWKS at the given
// URL. If the JWKS is not cached or the cache has expired, the JWKS is
// fetched from the URL. If the kid is not found in a cached JWKS, the
// cache is refreshed (to handle key rotation). Returns the key on success,
// or an error if the key cannot be found or the JWKS cannot be fetched.
func (c *jwksCache) getKey(ctx context.Context, jwksURL, kid string) (any, error) {
	c.mu.RLock()
	entry, ok := c.entries[jwksURL]
	if ok && time.Since(entry.fetchedAt) < c.ttl {
		key, exists := entry.keys[kid]
		c.mu.RUnlock()
		if exists {
			return key, nil
		}
		// Kid not found in cached JWKS — may be a key rotation; refetch.
	} else {
		c.mu.RUnlock()
	}

	// Fetch or re-fetch JWKS.
	keys, err := c.fetchJWKS(ctx, jwksURL)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to fetch JWKS from %s: %w", jwksURL, err)
	}

	c.mu.Lock()
	c.entries[jwksURL] = &jwksCacheEntry{
		keys:      keys,
		fetchedAt: time.Now(),
	}
	c.mu.Unlock()

	key, exists := keys[kid]
	if !exists {
		return nil, fmt.Errorf("auth: key ID %q not found in JWKS from %s", kid, jwksURL)
	}
	return key, nil
}

// jwksResponse represents the JSON structure of a JWKS endpoint response.
type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

// jwkKey represents a single key in a JWKS response. Only the fields
// needed for RSA and EC key reconstruction are included.
type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	// RSA fields
	N string `json:"n"`
	E string `json:"e"`
	// EC fields
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// fetchJWKS makes an HTTP GET request to the JWKS URL, parses the response,
// and constructs a map of key ID to public key. Supports RSA and ECDSA
// (P-256, P-384, P-521) key types.
//
// The response body is limited to 1 MB to prevent resource exhaustion.
func (c *jwksCache) fetchJWKS(ctx context.Context, jwksURL string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to create JWKS request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: JWKS request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth: JWKS endpoint returned status %d", resp.StatusCode)
	}

	// Limit response body to 1 MB.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("auth: failed to read JWKS response: %w", err)
	}

	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("auth: failed to parse JWKS JSON: %w", err)
	}

	keys := make(map[string]any, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kid == "" {
			continue
		}
		switch k.Kty {
		case "RSA":
			pubKey, err := parseRSAPublicKey(k.N, k.E)
			if err != nil {
				continue // Skip malformed keys.
			}
			keys[k.Kid] = pubKey
		case "EC":
			pubKey, err := parseECPublicKey(k.Crv, k.X, k.Y)
			if err != nil {
				continue // Skip malformed keys.
			}
			keys[k.Kid] = pubKey
		}
	}
	return keys, nil
}

// parseRSAPublicKey constructs an *rsa.PublicKey from base64url-encoded
// modulus (n) and exponent (e) values.
func parseRSAPublicKey(nBase64, eBase64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nBase64)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to decode RSA modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eBase64)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to decode RSA exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

// parseECPublicKey constructs an *ecdsa.PublicKey from a curve name and
// base64url-encoded x and y coordinates.
func parseECPublicKey(crv, xBase64, yBase64 string) (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("auth: unsupported EC curve %q", crv)
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(xBase64)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to decode EC x coordinate: %w", err)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(yBase64)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to decode EC y coordinate: %w", err)
	}

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(xBytes),
		Y:     new(big.Int).SetBytes(yBytes),
	}, nil
}

// ---------------------------------------------------------------------------
// JWTValidator — multi-provider JWT validation with caching and OTel tracing
// ---------------------------------------------------------------------------

// tracerName is the OpenTelemetry instrumentation scope name for auth spans.
const tracerName = "github.com/StricklySoft/stricklysoft-core/pkg/auth"

// JWTValidator validates JWT tokens from multiple providers (Kubernetes,
// platform HMAC, OIDC) with built-in caching, JWKS management, and
// OpenTelemetry tracing. It implements the [TokenValidator] interface.
//
// JWTValidator is safe for concurrent use by multiple goroutines.
type JWTValidator struct {
	config     ValidatorConfig
	tracer     trace.Tracer
	tokenCache *tokenCache
	jwksCache  *jwksCache
	permMapper func(claims map[string]any) []Permission
	httpClient HTTPClient

	// oidcJWKSURL caches the JWKS URL discovered from the OIDC provider's
	// .well-known/openid-configuration endpoint.
	oidcJWKSURL    string
	oidcJWKSMu     sync.Mutex
	oidcDiscovered bool
}

// Compile-time assertion that JWTValidator implements TokenValidator.
var _ TokenValidator = (*JWTValidator)(nil)

// NewJWTValidator creates a new JWTValidator with the given configuration.
// The configuration is validated before use; an error is returned if the
// configuration is invalid.
//
// If cfg.PermissionMapper is nil, [DefaultClaimsToPermissions] is used.
// If cfg.HTTPClient is nil, a default [http.Client] with a 10-second
// timeout is used.
// If cfg.SATokenPath is empty, [DefaultSATokenPath] is used.
func NewJWTValidator(cfg ValidatorConfig) (*JWTValidator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	permMapper := cfg.PermissionMapper
	if permMapper == nil {
		permMapper = DefaultClaimsToPermissions
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	if cfg.SATokenPath == "" {
		cfg.SATokenPath = DefaultSATokenPath
	}

	return &JWTValidator{
		config:     cfg,
		tracer:     otel.Tracer(tracerName),
		tokenCache: newTokenCache(cfg.TokenCacheTTL, cfg.TokenCacheMaxSize),
		jwksCache:  newJWKSCache(cfg.JWKSCacheTTL, httpClient),
		permMapper: permMapper,
		httpClient: httpClient,
	}, nil
}

// ---------------------------------------------------------------------------
// Validate — main entry point implementing TokenValidator
// ---------------------------------------------------------------------------

// Validate verifies the given JWT token string and returns the Identity it
// represents. It supports platform HMAC tokens (HS256), OIDC tokens
// (RS256, ES256), and Kubernetes ServiceAccount tokens.
//
// The method performs the following steps:
//  1. Rejects empty or oversized tokens
//  2. Checks the in-memory token cache
//  3. Parses the token without verification to inspect claims
//  4. Detects the token type from the issuer claim
//  5. Routes to the appropriate verification path
//  6. Caches the validated identity
//  7. Records OpenTelemetry span attributes and errors
//
// Returns a *[sserr.Error] with the appropriate error code on failure.
func (v *JWTValidator) Validate(ctx context.Context, tokenStr string) (Identity, error) {
	ctx, span := startSpan(ctx, v.tracer, "auth.Validate")
	defer span.End()

	// Reject empty tokens.
	if tokenStr == "" {
		err := sserr.New(sserr.CodeAuthenticationInvalid, "auth: token must not be empty")
		finishSpan(span, err)
		return nil, err
	}

	// Reject oversized tokens.
	if len(tokenStr) > maxTokenSize {
		err := sserr.New(sserr.CodeAuthenticationInvalid, "auth: token exceeds maximum size")
		finishSpan(span, err)
		return nil, err
	}

	// Compute cache key (SHA-256 hash of token).
	hash := tokenHash(tokenStr)

	// Check token cache.
	if identity, ok := v.tokenCache.get(hash); ok {
		span.SetAttributes(attribute.Bool("auth.cache_hit", true))
		return identity, nil
	}
	span.SetAttributes(attribute.Bool("auth.cache_hit", false))

	// Parse token without verification to inspect header and claims.
	parser := jwt.NewParser()
	unverified, parts, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil || unverified == nil {
		parseErr := sserr.New(sserr.CodeAuthenticationInvalid, "auth: token is malformed")
		finishSpan(span, parseErr)
		return nil, parseErr
	}
	_ = parts // Unused but required by ParseUnverified signature.

	// Reject alg:none — critical security check.
	algStr, _ := unverified.Header["alg"].(string)
	if strings.EqualFold(algStr, "none") {
		err := sserr.New(sserr.CodeAuthenticationInvalid, "auth: algorithm 'none' is not permitted")
		finishSpan(span, err)
		return nil, err
	}

	// Extract issuer claim for routing.
	mc, ok := unverified.Claims.(jwt.MapClaims)
	if !ok {
		err := sserr.New(sserr.CodeAuthenticationInvalid, "auth: unable to extract claims")
		finishSpan(span, err)
		return nil, err
	}
	issuer, _ := mc["iss"].(string)

	// Detect token type from issuer and algorithm.
	tokenType := v.detectTokenType(issuer, algStr)
	span.SetAttributes(attribute.String("auth.token_type", string(tokenType)))

	// Route to the appropriate validation path.
	var identity Identity
	var validationErr error

	switch tokenType {
	case TokenTypePlatform:
		identity, validationErr = v.validatePlatformToken(ctx, tokenStr)
	case TokenTypeOIDC:
		identity, validationErr = v.validateOIDCToken(ctx, tokenStr)
	case TokenTypeKubernetes:
		identity, validationErr = v.validateKubernetesToken(ctx, tokenStr)
	default:
		validationErr = sserr.New(sserr.CodeAuthentication, "auth: no matching validator for token")
	}

	if validationErr != nil {
		classifiedErr := classifyError(validationErr)
		finishSpan(span, classifiedErr)
		return nil, classifiedErr
	}

	// Cache the validated identity using the token's exp claim.
	if exp, expErr := mc.GetExpirationTime(); expErr == nil && exp != nil {
		v.tokenCache.put(hash, identity, exp.Time)
	}

	// Set span attributes for successful validation.
	span.SetAttributes(
		attribute.String("auth.identity_id", identity.ID()),
		attribute.String("auth.identity_type", string(identity.Type())),
	)

	return identity, nil
}

// detectTokenType determines which validation path to use based on the
// token's issuer claim and signing algorithm.
func (v *JWTValidator) detectTokenType(issuer, alg string) TokenType {
	// Match by issuer first (most reliable).
	if v.config.EnableKubernetes && issuer == v.config.KubernetesIssuer {
		return TokenTypeKubernetes
	}
	if v.config.EnablePlatform && issuer == v.config.PlatformIssuer {
		return TokenTypePlatform
	}
	if v.config.EnableOIDC && issuer == v.config.OIDCIssuerURL {
		return TokenTypeOIDC
	}

	// Fallback: guess from algorithm.
	algUpper := strings.ToUpper(alg)
	if strings.HasPrefix(algUpper, "HS") && v.config.EnablePlatform {
		return TokenTypePlatform
	}
	if (strings.HasPrefix(algUpper, "RS") || strings.HasPrefix(algUpper, "ES")) && v.config.EnableOIDC {
		return TokenTypeOIDC
	}
	if (strings.HasPrefix(algUpper, "RS") || strings.HasPrefix(algUpper, "ES")) && v.config.EnableKubernetes {
		return TokenTypeKubernetes
	}

	// No match.
	return ""
}

// ---------------------------------------------------------------------------
// validatePlatformToken — HMAC (HS256) platform token validation
// ---------------------------------------------------------------------------

// validatePlatformToken verifies a platform-issued JWT signed with HS256.
// The token's signature is verified using the configured PlatformSigningKey.
//
// CRITICAL: jwt.WithValidMethods restricts accepted algorithms to HS256 only,
// preventing algorithm confusion attacks where an attacker could present an
// RSA-signed token and trick the validator into using the public key as an
// HMAC secret.
func (v *JWTValidator) validatePlatformToken(ctx context.Context, tokenStr string) (Identity, error) {
	_, span := startSpan(ctx, v.tracer, "auth.ValidatePlatformToken")
	defer span.End()

	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(v.config.PlatformIssuer),
		jwt.WithLeeway(v.config.ClockSkew),
	}
	if v.config.PlatformAudience != "" {
		parserOpts = append(parserOpts, jwt.WithAudience(v.config.PlatformAudience))
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		return []byte(v.config.PlatformSigningKey.Value()), nil
	}, parserOpts...)
	if err != nil {
		finishSpan(span, err)
		return nil, err
	}

	mc, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		err := sserr.New(sserr.CodeAuthenticationInvalid, "auth: invalid platform token claims")
		finishSpan(span, err)
		return nil, err
	}

	claims := mapClaimsToMap(mc)
	permissions := v.permMapper(claims)
	sub, _ := claims["sub"].(string)

	// Build identity based on available claims.
	if email, ok := claims["email"].(string); ok && email != "" {
		name, _ := claims["name"].(string)
		identity, err := NewUserIdentity(sub, email, name, claims, permissions)
		if err != nil {
			wrappedErr := sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: failed to create user identity from platform token")
			finishSpan(span, wrappedErr)
			return nil, wrappedErr
		}
		return identity, nil
	}

	if serviceName, ok := claims["service_name"].(string); ok && serviceName != "" {
		namespace, _ := claims["namespace"].(string)
		identity, err := NewServiceIdentity(sub, serviceName, namespace, claims, permissions)
		if err != nil {
			wrappedErr := sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: failed to create service identity from platform token")
			finishSpan(span, wrappedErr)
			return nil, wrappedErr
		}
		return identity, nil
	}

	// Fallback to basic identity for tokens without email or service_name.
	return NewBasicIdentity(sub, IdentityTypeService, claims), nil
}

// ---------------------------------------------------------------------------
// validateOIDCToken — RSA/ECDSA token validation via OIDC discovery
// ---------------------------------------------------------------------------

// validateOIDCToken verifies a JWT issued by an external OIDC provider.
// The token's signature is verified using public keys from the provider's
// JWKS endpoint, discovered via the .well-known/openid-configuration document.
func (v *JWTValidator) validateOIDCToken(ctx context.Context, tokenStr string) (Identity, error) {
	_, span := startSpan(ctx, v.tracer, "auth.ValidateOIDCToken")
	defer span.End()

	// Discover JWKS URL from OIDC provider.
	jwksURL, err := v.getOIDCJWKSURL(ctx)
	if err != nil {
		wrappedErr := sserr.Wrap(err, sserr.CodeAuthentication, "auth: OIDC discovery failed")
		finishSpan(span, wrappedErr)
		return nil, wrappedErr
	}

	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
		jwt.WithIssuer(v.config.OIDCIssuerURL),
		jwt.WithLeeway(v.config.ClockSkew),
	}
	if v.config.OIDCAudience != "" {
		parserOpts = append(parserOpts, jwt.WithAudience(v.config.OIDCAudience))
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("auth: token header missing kid")
		}
		return v.jwksCache.getKey(ctx, jwksURL, kid)
	}, parserOpts...)
	if err != nil {
		finishSpan(span, err)
		return nil, err
	}

	mc, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		err := sserr.New(sserr.CodeAuthenticationInvalid, "auth: invalid OIDC token claims")
		finishSpan(span, err)
		return nil, err
	}

	claims := mapClaimsToMap(mc)
	permissions := v.permMapper(claims)
	sub, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)
	name, _ := claims["name"].(string)

	if email != "" {
		identity, err := NewUserIdentity(sub, email, name, claims, permissions)
		if err != nil {
			wrappedErr := sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: failed to create user identity from OIDC token")
			finishSpan(span, wrappedErr)
			return nil, wrappedErr
		}
		return identity, nil
	}

	// Fallback for OIDC tokens without email.
	return NewBasicIdentity(sub, IdentityTypeUser, claims), nil
}

// getOIDCJWKSURL returns the cached OIDC JWKS URL or fetches it from the
// provider's .well-known/openid-configuration endpoint.
func (v *JWTValidator) getOIDCJWKSURL(ctx context.Context) (string, error) {
	v.oidcJWKSMu.Lock()
	defer v.oidcJWKSMu.Unlock()

	if v.oidcDiscovered && v.oidcJWKSURL != "" {
		return v.oidcJWKSURL, nil
	}

	discovery, err := fetchOIDCDiscovery(ctx, v.config.OIDCIssuerURL, v.httpClient)
	if err != nil {
		return "", err
	}

	v.oidcJWKSURL = discovery.JWKSURI
	v.oidcDiscovered = true
	return v.oidcJWKSURL, nil
}

// ---------------------------------------------------------------------------
// validateKubernetesToken — Kubernetes ServiceAccount token validation
// ---------------------------------------------------------------------------

// validateKubernetesToken verifies a Kubernetes ServiceAccount JWT token.
// Kubernetes tokens are validated similarly to OIDC tokens, using the
// Kubernetes OIDC discovery endpoint to fetch JWKS. Kubernetes-specific
// claims (namespace, service account name) are extracted using
// parseK8sServiceAccountClaims from k8s.go.
func (v *JWTValidator) validateKubernetesToken(ctx context.Context, tokenStr string) (Identity, error) {
	_, span := startSpan(ctx, v.tracer, "auth.ValidateKubernetesToken")
	var retErr error
	defer func() {
		finishSpan(span, retErr)
		span.End()
	}()

	// Kubernetes exposes OIDC discovery at the issuer URL.
	jwksURL := strings.TrimRight(v.config.KubernetesIssuer, "/") + "/openid/v1/jwks"

	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256", "ES256"}),
		jwt.WithIssuer(v.config.KubernetesIssuer),
		jwt.WithLeeway(v.config.ClockSkew),
	}
	if v.config.KubernetesAudience != "" {
		parserOpts = append(parserOpts, jwt.WithAudience(v.config.KubernetesAudience))
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, sserr.New(sserr.CodeAuthenticationInvalid, "auth: kubernetes token missing kid header")
		}
		return v.jwksCache.getKey(ctx, jwksURL, kid)
	}, parserOpts...)
	if err != nil {
		retErr = classifyJWTError(err)
		return nil, retErr
	}

	mc, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		retErr = sserr.New(sserr.CodeAuthenticationInvalid, "auth: invalid kubernetes token claims")
		return nil, retErr
	}

	claimsMap := mapClaimsToMap(mc)
	permissions := v.permMapper(claimsMap)

	// Parse Kubernetes-specific claims to build ServiceIdentity.
	identity, err := parseK8sServiceAccountClaims(claimsMap, permissions)
	if err != nil {
		retErr = sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: failed to parse kubernetes service account claims")
		return nil, retErr
	}

	return identity, nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// tokenHash computes the SHA-256 hash of a token string and returns it
// as a hex-encoded string. This is used as the cache key to avoid storing
// raw tokens in memory.
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// mapClaimsToMap converts jwt.MapClaims to a plain map[string]any.
// This allows the claims to be passed to functions that expect a plain map
// without carrying the jwt.MapClaims type.
func mapClaimsToMap(mc jwt.MapClaims) map[string]any {
	result := make(map[string]any, len(mc))
	for k, v := range mc {
		result[k] = v
	}
	return result
}

// classifyError converts a JWT library error or other error to an
// appropriate *sserr.Error with the correct error code. If the error
// is already an *sserr.Error, it is returned as-is.
func classifyError(err error) *sserr.Error {
	if err == nil {
		return nil
	}

	// If it is already our error type, return as-is.
	var ssError *sserr.Error
	if errors.As(err, &ssError) {
		return ssError
	}

	// Classify JWT library errors.
	if errors.Is(err, jwt.ErrTokenExpired) {
		return sserr.Wrap(err, sserr.CodeAuthenticationExpired, "auth: token has expired")
	}
	if errors.Is(err, jwt.ErrTokenMalformed) {
		return sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: token is malformed")
	}
	if errors.Is(err, jwt.ErrSignatureInvalid) {
		return sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: token signature is invalid")
	}
	if errors.Is(err, jwt.ErrTokenUnverifiable) {
		return sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: token is unverifiable")
	}
	if errors.Is(err, jwt.ErrTokenNotValidYet) {
		return sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: token is not yet valid")
	}
	if errors.Is(err, jwt.ErrTokenInvalidAudience) {
		return sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: token audience is invalid")
	}
	if errors.Is(err, jwt.ErrTokenInvalidIssuer) {
		return sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: token issuer is invalid")
	}
	if errors.Is(err, jwt.ErrTokenInvalidClaims) {
		return sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: token claims are invalid")
	}

	// Check for "no matching validator" pattern.
	if strings.Contains(err.Error(), "no matching validator") {
		return sserr.Wrap(err, sserr.CodeAuthentication, "auth: no matching validator for token")
	}

	// Default: general authentication invalid error.
	return sserr.Wrap(err, sserr.CodeAuthenticationInvalid, "auth: token validation failed")
}

// classifyJWTError is an alias for classifyError, retained for use by
// the Kubernetes token validation path in k8s.go.
func classifyJWTError(err error) *sserr.Error {
	return classifyError(err)
}

// oidcDiscoveryResponse represents the relevant fields from an OIDC
// provider's .well-known/openid-configuration document.
type oidcDiscoveryResponse struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

// fetchOIDCDiscovery fetches the OIDC discovery document from the provider's
// .well-known/openid-configuration endpoint and returns the parsed response
// containing the issuer and JWKS URI.
func fetchOIDCDiscovery(ctx context.Context, issuerURL string, client HTTPClient) (*oidcDiscoveryResponse, error) {
	discoveryURL := strings.TrimRight(issuerURL, "/") + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to create OIDC discovery request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: OIDC discovery request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth: OIDC discovery endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("auth: failed to read OIDC discovery response: %w", err)
	}

	var discovery oidcDiscoveryResponse
	if err := json.Unmarshal(body, &discovery); err != nil {
		return nil, fmt.Errorf("auth: failed to parse OIDC discovery JSON: %w", err)
	}

	if discovery.JWKSURI == "" {
		return nil, fmt.Errorf("auth: OIDC discovery document missing jwks_uri")
	}

	return &discovery, nil
}

// startSpan creates a new OpenTelemetry span with the given name. Returns
// the updated context and span.
func startSpan(ctx context.Context, tracer trace.Tracer, name string) (context.Context, trace.Span) {
	return tracer.Start(ctx, name)
}

// finishSpan records an error on the span if err is non-nil and sets the
// span status to Error. This is a helper for consistent error recording
// across validation paths.
func finishSpan(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
