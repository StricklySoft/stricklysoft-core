package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"go.opentelemetry.io/otel"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testSigningKey is a 32-byte HMAC key used across platform token tests.
const testSigningKey = "this-is-a-32-byte-test-signing-k"

// jwtTestGenerateHMACToken creates an HS256-signed JWT with the given claims.
// Fails the test immediately if token creation fails.
func jwtTestGenerateHMACToken(t *testing.T, key []byte, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(key)
	require.NoError(t, err, "failed to sign HMAC token")
	return tokenStr
}

// jwtTestGenerateRSAKeyPair generates a 2048-bit RSA key pair for testing.
func jwtTestGenerateRSAKeyPair(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "failed to generate RSA key pair")
	return privKey, &privKey.PublicKey
}

// jwtTestGenerateRSAToken creates an RS256-signed JWT with the given claims and kid.
func jwtTestGenerateRSAToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	tokenStr, err := token.SignedString(key)
	require.NoError(t, err, "failed to sign RSA token")
	return tokenStr
}

// jwtTestGenerateECDSAKeyPair generates a P-256 ECDSA key pair for testing.
func jwtTestGenerateECDSAKeyPair(t *testing.T) (*ecdsa.PrivateKey, *ecdsa.PublicKey) {
	t.Helper()
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "failed to generate ECDSA key pair")
	return privKey, &privKey.PublicKey
}

// jwtTestGenerateECDSAToken creates an ES256-signed JWT with the given claims and kid.
func jwtTestGenerateECDSAToken(t *testing.T, key *ecdsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = kid
	tokenStr, err := token.SignedString(key)
	require.NoError(t, err, "failed to sign ECDSA token")
	return tokenStr
}

// jwtTestServeJWKS starts an httptest.Server that serves a JWKS document containing
// the given RSA and ECDSA public keys. Each key is keyed by its kid.
func jwtTestServeJWKS(t *testing.T, rsaKeys map[string]*rsa.PublicKey, ecKeys map[string]*ecdsa.PublicKey) *httptest.Server {
	t.Helper()

	type jwkEntry struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Alg string `json:"alg,omitempty"`
		Use string `json:"use,omitempty"`
		N   string `json:"n,omitempty"`
		E   string `json:"e,omitempty"`
		Crv string `json:"crv,omitempty"`
		X   string `json:"x,omitempty"`
		Y   string `json:"y,omitempty"`
	}

	var keys []jwkEntry

	for kid, pub := range rsaKeys {
		keys = append(keys, jwkEntry{
			Kty: "RSA",
			Kid: kid,
			Alg: "RS256",
			Use: "sig",
			N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}

	for kid, pub := range ecKeys {
		keys = append(keys, jwkEntry{
			Kty: "EC",
			Kid: kid,
			Crv: "P-256",
			Use: "sig",
			X:   base64.RawURLEncoding.EncodeToString(pub.X.Bytes()),
			Y:   base64.RawURLEncoding.EncodeToString(pub.Y.Bytes()),
		})
	}

	jwksDoc, err := json.Marshal(map[string]any{"keys": keys})
	require.NoError(t, err, "failed to marshal JWKS")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jwksDoc)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newPlatformConfig returns a ValidatorConfig with only platform tokens
// enabled and a 32-byte test signing key.
func newPlatformConfig() ValidatorConfig {
	return ValidatorConfig{
		EnableKubernetes:   false,
		EnablePlatform:     true,
		EnableOIDC:         false,
		PlatformSigningKey: Secret(testSigningKey),
		PlatformIssuer:     "stricklysoft-platform",
		TokenCacheTTL:      5 * time.Minute,
		TokenCacheMaxSize:  100,
		JWKSCacheTTL:       1 * time.Hour,
		ClockSkew:          30 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// Secret type tests
// ---------------------------------------------------------------------------

func TestSecret_String_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-key-value")
	assert.Equal(t, "[REDACTED]", s.String())
	assert.Equal(t, "[REDACTED]", s.String())
}

func TestSecret_GoString_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-key-value")
	assert.Equal(t, "[REDACTED]", s.GoString())
	assert.Equal(t, "[REDACTED]", fmt.Sprintf("%#v", s))
}

func TestSecret_Value_ReturnsActualValue(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-key-value")
	assert.Equal(t, "super-secret-key-value", s.Value())
}

func TestSecret_MarshalText_ReturnsRedacted(t *testing.T) {
	t.Parallel()
	s := Secret("super-secret-key-value")
	text, err := s.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "[REDACTED]", string(text))
}

// ---------------------------------------------------------------------------
// ValidatorConfig validation tests
// ---------------------------------------------------------------------------

func TestValidatorConfig_Validate_MinimalKubernetes(t *testing.T) {
	t.Parallel()
	cfg := ValidatorConfig{
		EnableKubernetes:  true,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         30 * time.Second,
	}
	assert.Nil(t, cfg.Validate(), "kubernetes-only config should be valid")
}

func TestValidatorConfig_Validate_PlatformRequiresKey(t *testing.T) {
	t.Parallel()
	cfg := ValidatorConfig{
		EnableKubernetes:  false,
		EnablePlatform:    true,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         30 * time.Second,
		// PlatformSigningKey intentionally not set.
	}
	err := cfg.Validate()
	require.NotNil(t, err, "platform without signing key should fail validation")
	assert.Equal(t, sserr.CodeValidation, err.Code)
	assert.Contains(t, err.Message, "signing key")
}

func TestValidatorConfig_Validate_PlatformKeyTooShort(t *testing.T) {
	t.Parallel()
	cfg := ValidatorConfig{
		EnableKubernetes:   false,
		EnablePlatform:     true,
		PlatformSigningKey: Secret("short-key"),
		TokenCacheTTL:      5 * time.Minute,
		TokenCacheMaxSize:  100,
		JWKSCacheTTL:       1 * time.Hour,
		ClockSkew:          30 * time.Second,
	}
	err := cfg.Validate()
	require.NotNil(t, err, "platform with short signing key should fail validation")
	assert.Equal(t, sserr.CodeValidation, err.Code)
	assert.Contains(t, err.Message, "32 bytes")
}

func TestValidatorConfig_Validate_OIDCRequiresIssuer(t *testing.T) {
	t.Parallel()
	cfg := ValidatorConfig{
		EnableKubernetes:  false,
		EnableOIDC:        true,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         30 * time.Second,
		// OIDCIssuerURL intentionally empty.
	}
	err := cfg.Validate()
	require.NotNil(t, err, "OIDC without issuer URL should fail validation")
	assert.Equal(t, sserr.CodeValidation, err.Code)
	assert.Contains(t, err.Message, "OIDC issuer URL")
}

func TestValidatorConfig_Validate_NoEnabledValidators(t *testing.T) {
	t.Parallel()
	cfg := ValidatorConfig{
		EnableKubernetes:  false,
		EnablePlatform:    false,
		EnableOIDC:        false,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         30 * time.Second,
	}
	err := cfg.Validate()
	require.NotNil(t, err, "config with no enabled validators should fail")
	assert.Equal(t, sserr.CodeValidation, err.Code)
	assert.Contains(t, err.Message, "at least one token type")
}

func TestValidatorConfig_Validate_NegativeTTL(t *testing.T) {
	t.Parallel()
	cfg := ValidatorConfig{
		EnableKubernetes:  true,
		TokenCacheTTL:     -1 * time.Second,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         30 * time.Second,
	}
	err := cfg.Validate()
	require.NotNil(t, err, "negative TTL should fail validation")
	assert.Equal(t, sserr.CodeValidation, err.Code)
	assert.Contains(t, err.Message, "non-negative")
}

func TestDefaultValidatorConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultValidatorConfig()

	assert.True(t, cfg.EnableKubernetes)
	assert.False(t, cfg.EnablePlatform)
	assert.False(t, cfg.EnableOIDC)
	assert.Equal(t, "stricklysoft-platform", cfg.PlatformIssuer)
	assert.Equal(t, "https://kubernetes.default.svc.cluster.local", cfg.KubernetesIssuer)
	assert.Equal(t, "https://kubernetes.default.svc.cluster.local", cfg.KubernetesAudience)
	assert.Equal(t, 5*time.Minute, cfg.TokenCacheTTL)
	assert.Equal(t, 10000, cfg.TokenCacheMaxSize)
	assert.Equal(t, 1*time.Hour, cfg.JWKSCacheTTL)
	assert.Equal(t, 30*time.Second, cfg.ClockSkew)
	assert.Equal(t, DefaultSATokenPath, cfg.SATokenPath)

	// Defaults should be valid.
	assert.Nil(t, cfg.Validate())
}

// ---------------------------------------------------------------------------
// NewJWTValidator tests
// ---------------------------------------------------------------------------

func TestNewJWTValidator_ValidConfig(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)
	require.NotNil(t, v)
}

func TestNewJWTValidator_InvalidConfig(t *testing.T) {
	t.Parallel()
	cfg := ValidatorConfig{
		EnableKubernetes:  false,
		EnablePlatform:    false,
		EnableOIDC:        false,
		TokenCacheMaxSize: 100,
		TokenCacheTTL:     5 * time.Minute,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         30 * time.Second,
	}
	v, err := NewJWTValidator(cfg)
	require.Error(t, err)
	assert.Nil(t, v)
}

// ---------------------------------------------------------------------------
// Platform HMAC validation tests
// ---------------------------------------------------------------------------

func TestValidate_PlatformToken_Valid(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateHMACToken(t, []byte(testSigningKey), jwt.MapClaims{
		"iss": "stricklysoft-platform",
		"sub": "svc-test-123",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat": jwt.NewNumericDate(time.Now()),
	})

	identity, err := v.Validate(context.Background(), tokenStr)
	require.NoError(t, err)
	require.NotNil(t, identity)
	assert.Equal(t, "svc-test-123", identity.ID())
}

func TestValidate_PlatformToken_Expired(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	cfg.ClockSkew = 0 // No leeway for this test.
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateHMACToken(t, []byte(testSigningKey), jwt.MapClaims{
		"iss": "stricklysoft-platform",
		"sub": "svc-test-123",
		"exp": jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)), // Expired an hour ago.
		"iat": jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
	})

	_, err = v.Validate(context.Background(), tokenStr)
	require.Error(t, err)
	var ssErr *sserr.Error
	require.ErrorAs(t, err, &ssErr)
	assert.Equal(t, sserr.CodeAuthenticationExpired, ssErr.Code, "expired token should produce AUTH_002")
}

func TestValidate_PlatformToken_InvalidSignature(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	// Sign with a different key.
	wrongKey := []byte("this-is-a-different-32byte-keyXX")
	tokenStr := jwtTestGenerateHMACToken(t, wrongKey, jwt.MapClaims{
		"iss": "stricklysoft-platform",
		"sub": "svc-test-123",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat": jwt.NewNumericDate(time.Now()),
	})

	_, err = v.Validate(context.Background(), tokenStr)
	require.Error(t, err)
	var ssErr *sserr.Error
	require.ErrorAs(t, err, &ssErr)
	assert.Equal(t, sserr.CodeAuthenticationInvalid, ssErr.Code, "wrong signature should produce AUTH_003")
}

func TestValidate_PlatformToken_WrongIssuer(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	// Enable kubernetes to make sure the validator detects it as platform via alg fallback.
	cfg.EnableKubernetes = false
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateHMACToken(t, []byte(testSigningKey), jwt.MapClaims{
		"iss": "wrong-issuer",
		"sub": "svc-test-123",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat": jwt.NewNumericDate(time.Now()),
	})

	_, err = v.Validate(context.Background(), tokenStr)
	require.Error(t, err)
	var ssErr *sserr.Error
	require.ErrorAs(t, err, &ssErr)
	assert.Equal(t, sserr.CodeAuthenticationInvalid, ssErr.Code, "wrong issuer should produce AUTH_003")
}

func TestValidate_PlatformToken_AlgNone(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	// Craft a token with alg:none manually. This is a three-part base64 string.
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"stricklysoft-platform","sub":"evil","exp":9999999999}`))
	algNoneToken := header + "." + payload + "."

	_, err = v.Validate(context.Background(), algNoneToken)
	require.Error(t, err)
	var ssErr *sserr.Error
	require.ErrorAs(t, err, &ssErr)
	assert.Equal(t, sserr.CodeAuthenticationInvalid, ssErr.Code, "alg:none should produce AUTH_003")
}

func TestValidate_PlatformToken_UserClaims_ReturnsUserIdentity(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateHMACToken(t, []byte(testSigningKey), jwt.MapClaims{
		"iss":   "stricklysoft-platform",
		"sub":   "user-456",
		"email": "user@example.com",
		"name":  "Test User",
		"exp":   jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat":   jwt.NewNumericDate(time.Now()),
	})

	identity, err := v.Validate(context.Background(), tokenStr)
	require.NoError(t, err)
	require.NotNil(t, identity)

	assert.Equal(t, IdentityTypeUser, identity.Type(), "token with email should produce UserIdentity")
	assert.Equal(t, "user-456", identity.ID())

	userID, ok := identity.(*UserIdentity)
	require.True(t, ok, "identity should be *UserIdentity")
	assert.Equal(t, "user@example.com", userID.Email())
	assert.Equal(t, "Test User", userID.DisplayName())
}

func TestValidate_PlatformToken_ServiceClaims_ReturnsServiceIdentity(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateHMACToken(t, []byte(testSigningKey), jwt.MapClaims{
		"iss":          "stricklysoft-platform",
		"sub":          "svc-789",
		"service_name": "nexus-gateway",
		"namespace":    "platform",
		"exp":          jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat":          jwt.NewNumericDate(time.Now()),
	})

	identity, err := v.Validate(context.Background(), tokenStr)
	require.NoError(t, err)
	require.NotNil(t, identity)

	assert.Equal(t, IdentityTypeService, identity.Type(), "token with service_name should produce ServiceIdentity")
	assert.Equal(t, "svc-789", identity.ID())

	svcID, ok := identity.(*ServiceIdentity)
	require.True(t, ok, "identity should be *ServiceIdentity")
	assert.Equal(t, "nexus-gateway", svcID.ServiceName())
	assert.Equal(t, "platform", svcID.Namespace())
}

// ---------------------------------------------------------------------------
// OIDC validation tests (using httptest servers)
// ---------------------------------------------------------------------------

func TestValidate_OIDCToken_Valid_RSA(t *testing.T) {
	t.Parallel()
	rsaPriv, rsaPub := jwtTestGenerateRSAKeyPair(t)

	// Set up JWKS server.
	jwksSrv := jwtTestServeJWKS(t, map[string]*rsa.PublicKey{"rsa-key-1": rsaPub}, nil)

	// Set up OIDC discovery server (issuer URL is the discovery server itself).
	discoveryURL := "" // will be set below
	discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			resp, _ := json.Marshal(map[string]string{
				"issuer":   discoveryURL,
				"jwks_uri": jwksSrv.URL,
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(resp)
			return
		}
		http.NotFound(w, r)
	}))
	discoveryURL = discoverySrv.URL
	t.Cleanup(discoverySrv.Close)

	cfg := ValidatorConfig{
		EnableKubernetes:  false,
		EnablePlatform:    false,
		EnableOIDC:        true,
		OIDCIssuerURL:     discoverySrv.URL,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         30 * time.Second,
		HTTPClient:        discoverySrv.Client(),
	}
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateRSAToken(t, rsaPriv, "rsa-key-1", jwt.MapClaims{
		"iss":   discoverySrv.URL,
		"sub":   "user-oidc-1",
		"email": "oidc@example.com",
		"name":  "OIDC User",
		"exp":   jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat":   jwt.NewNumericDate(time.Now()),
	})

	identity, err := v.Validate(context.Background(), tokenStr)
	require.NoError(t, err)
	require.NotNil(t, identity)
	assert.Equal(t, "user-oidc-1", identity.ID())
	assert.Equal(t, IdentityTypeUser, identity.Type())

	userID, ok := identity.(*UserIdentity)
	require.True(t, ok)
	assert.Equal(t, "oidc@example.com", userID.Email())
}

func TestValidate_OIDCToken_Valid_ECDSA(t *testing.T) {
	t.Parallel()
	ecPriv, ecPub := jwtTestGenerateECDSAKeyPair(t)

	jwksSrv := jwtTestServeJWKS(t, nil, map[string]*ecdsa.PublicKey{"ec-key-1": ecPub})

	discoveryURL := ""
	discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			resp, _ := json.Marshal(map[string]string{
				"issuer":   discoveryURL,
				"jwks_uri": jwksSrv.URL,
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(resp)
			return
		}
		http.NotFound(w, r)
	}))
	discoveryURL = discoverySrv.URL
	t.Cleanup(discoverySrv.Close)

	cfg := ValidatorConfig{
		EnableKubernetes:  false,
		EnablePlatform:    false,
		EnableOIDC:        true,
		OIDCIssuerURL:     discoverySrv.URL,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         30 * time.Second,
		HTTPClient:        discoverySrv.Client(),
	}
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateECDSAToken(t, ecPriv, "ec-key-1", jwt.MapClaims{
		"iss":   discoverySrv.URL,
		"sub":   "user-ec-1",
		"email": "ec@example.com",
		"exp":   jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat":   jwt.NewNumericDate(time.Now()),
	})

	identity, err := v.Validate(context.Background(), tokenStr)
	require.NoError(t, err)
	require.NotNil(t, identity)
	assert.Equal(t, "user-ec-1", identity.ID())
	assert.Equal(t, IdentityTypeUser, identity.Type())
}

func TestValidate_OIDCToken_Expired(t *testing.T) {
	t.Parallel()
	rsaPriv, rsaPub := jwtTestGenerateRSAKeyPair(t)

	jwksSrv := jwtTestServeJWKS(t, map[string]*rsa.PublicKey{"rsa-key-1": rsaPub}, nil)

	discoveryURL := ""
	discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			resp, _ := json.Marshal(map[string]string{
				"issuer":   discoveryURL,
				"jwks_uri": jwksSrv.URL,
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(resp)
			return
		}
		http.NotFound(w, r)
	}))
	discoveryURL = discoverySrv.URL
	t.Cleanup(discoverySrv.Close)

	cfg := ValidatorConfig{
		EnableKubernetes:  false,
		EnablePlatform:    false,
		EnableOIDC:        true,
		OIDCIssuerURL:     discoverySrv.URL,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         0, // No leeway.
		HTTPClient:        discoverySrv.Client(),
	}
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateRSAToken(t, rsaPriv, "rsa-key-1", jwt.MapClaims{
		"iss": discoverySrv.URL,
		"sub": "user-expired",
		"exp": jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		"iat": jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
	})

	_, err = v.Validate(context.Background(), tokenStr)
	require.Error(t, err)
	var ssErr *sserr.Error
	require.ErrorAs(t, err, &ssErr)
	assert.Equal(t, sserr.CodeAuthenticationExpired, ssErr.Code, "expired OIDC token should produce AUTH_002")
}

func TestValidate_OIDCToken_UnknownKid(t *testing.T) {
	t.Parallel()
	rsaPriv, rsaPub := jwtTestGenerateRSAKeyPair(t)

	// Serve JWKS with key "known-kid", but sign token with "unknown-kid".
	jwksSrv := jwtTestServeJWKS(t, map[string]*rsa.PublicKey{"known-kid": rsaPub}, nil)

	discoveryURL := ""
	discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			resp, _ := json.Marshal(map[string]string{
				"issuer":   discoveryURL,
				"jwks_uri": jwksSrv.URL,
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(resp)
			return
		}
		http.NotFound(w, r)
	}))
	discoveryURL = discoverySrv.URL
	t.Cleanup(discoverySrv.Close)

	cfg := ValidatorConfig{
		EnableKubernetes:  false,
		EnablePlatform:    false,
		EnableOIDC:        true,
		OIDCIssuerURL:     discoverySrv.URL,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         30 * time.Second,
		HTTPClient:        discoverySrv.Client(),
	}
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateRSAToken(t, rsaPriv, "unknown-kid", jwt.MapClaims{
		"iss": discoverySrv.URL,
		"sub": "user-unknown-kid",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat": jwt.NewNumericDate(time.Now()),
	})

	_, err = v.Validate(context.Background(), tokenStr)
	require.Error(t, err)
	var ssErr *sserr.Error
	require.ErrorAs(t, err, &ssErr)
	assert.Equal(t, sserr.CodeAuthenticationInvalid, ssErr.Code, "unknown kid should produce AUTH_003")
}

// ---------------------------------------------------------------------------
// Token cache tests
// ---------------------------------------------------------------------------

func TestTokenCache_PutAndGet(t *testing.T) {
	t.Parallel()
	cache := newTokenCache(5*time.Minute, 100)

	identity := NewBasicIdentity("test-id", IdentityTypeUser, nil)
	cache.put("hash-1", identity, time.Now().Add(1*time.Hour))

	got, ok := cache.get("hash-1")
	assert.True(t, ok, "expected cache hit")
	assert.Equal(t, "test-id", got.ID())
}

func TestTokenCache_Expiry(t *testing.T) {
	t.Parallel()
	// Very short TTL.
	cache := newTokenCache(1*time.Millisecond, 100)

	identity := NewBasicIdentity("test-id", IdentityTypeUser, nil)
	cache.put("hash-1", identity, time.Now().Add(1*time.Hour))

	// Wait for TTL to expire.
	time.Sleep(10 * time.Millisecond)

	_, ok := cache.get("hash-1")
	assert.False(t, ok, "expected cache miss after TTL expiry")
}

func TestTokenCache_MaxSize_Eviction(t *testing.T) {
	t.Parallel()
	cache := newTokenCache(5*time.Minute, 2)

	id1 := NewBasicIdentity("id-1", IdentityTypeUser, nil)
	id2 := NewBasicIdentity("id-2", IdentityTypeUser, nil)
	id3 := NewBasicIdentity("id-3", IdentityTypeUser, nil)

	cache.put("hash-1", id1, time.Now().Add(1*time.Hour))
	cache.put("hash-2", id2, time.Now().Add(1*time.Hour))

	// Cache is now full. Adding a third entry should evict one.
	cache.put("hash-3", id3, time.Now().Add(1*time.Hour))

	got, ok := cache.get("hash-3")
	assert.True(t, ok, "newest entry should be present")
	assert.Equal(t, "id-3", got.ID())

	// At least one of the earlier entries should have been evicted.
	_, ok1 := cache.get("hash-1")
	_, ok2 := cache.get("hash-2")
	// We know cache size is 2, so at most 2 entries exist.
	cache.mu.RLock()
	size := len(cache.entries)
	cache.mu.RUnlock()
	assert.LessOrEqual(t, size, 2, "cache should not exceed max size")
	// At least one of the old entries should be gone.
	assert.False(t, ok1 && ok2, "at least one old entry should have been evicted")
}

func TestValidate_CacheHit(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateHMACToken(t, []byte(testSigningKey), jwt.MapClaims{
		"iss": "stricklysoft-platform",
		"sub": "cache-test-user",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat": jwt.NewNumericDate(time.Now()),
	})

	// First call — cache miss.
	identity1, err := v.Validate(context.Background(), tokenStr)
	require.NoError(t, err)
	assert.Equal(t, "cache-test-user", identity1.ID())

	// Second call — should hit cache and return the same identity.
	identity2, err := v.Validate(context.Background(), tokenStr)
	require.NoError(t, err)
	assert.Equal(t, identity1.ID(), identity2.ID())
}

// ---------------------------------------------------------------------------
// Error code tests
// ---------------------------------------------------------------------------

func TestValidate_EmptyToken(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	_, err = v.Validate(context.Background(), "")
	require.Error(t, err)
	var ssErr *sserr.Error
	require.ErrorAs(t, err, &ssErr)
	assert.Equal(t, sserr.CodeAuthenticationInvalid, ssErr.Code, "empty token should produce AUTH_003")
}

func TestValidate_MalformedJWT(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	_, err = v.Validate(context.Background(), "not.a.jwt")
	require.Error(t, err)
	var ssErr *sserr.Error
	require.ErrorAs(t, err, &ssErr)
	assert.Equal(t, sserr.CodeAuthenticationInvalid, ssErr.Code, "malformed JWT should produce AUTH_003")
}

func TestValidate_NoMatchingValidator(t *testing.T) {
	t.Parallel()
	// Only platform is enabled, but the token has an unknown issuer and uses
	// RS256 algorithm which does not match platform HMAC.
	rsaPriv, _ := jwtTestGenerateRSAKeyPair(t)

	cfg := ValidatorConfig{
		EnableKubernetes:   false,
		EnablePlatform:     true,
		EnableOIDC:         false,
		PlatformSigningKey: Secret(testSigningKey),
		PlatformIssuer:     "stricklysoft-platform",
		TokenCacheTTL:      5 * time.Minute,
		TokenCacheMaxSize:  100,
		JWKSCacheTTL:       1 * time.Hour,
		ClockSkew:          30 * time.Second,
	}
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	// Generate an RS256 token (not HMAC) with a non-platform issuer.
	tokenStr := jwtTestGenerateRSAToken(t, rsaPriv, "some-kid", jwt.MapClaims{
		"iss": "https://unknown-provider.example.com",
		"sub": "user-unknown",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat": jwt.NewNumericDate(time.Now()),
	})

	_, err = v.Validate(context.Background(), tokenStr)
	require.Error(t, err)
	var ssErr *sserr.Error
	require.ErrorAs(t, err, &ssErr)
	assert.Equal(t, sserr.CodeAuthentication, ssErr.Code, "no matching validator should produce AUTH_001")
}

// ---------------------------------------------------------------------------
// JWKS cache tests
// ---------------------------------------------------------------------------

func TestJWKSCache_FetchAndCache(t *testing.T) {
	t.Parallel()
	rsaPriv, rsaPub := jwtTestGenerateRSAKeyPair(t)
	_ = rsaPriv

	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		jwksDoc, _ := json.Marshal(map[string]any{
			"keys": []map[string]string{
				{
					"kty": "RSA",
					"kid": "test-kid",
					"alg": "RS256",
					"use": "sig",
					"n":   base64.RawURLEncoding.EncodeToString(rsaPub.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaPub.E)).Bytes()),
				},
			},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jwksDoc)
	}))
	t.Cleanup(srv.Close)

	cache := newJWKSCache(1*time.Hour, srv.Client())

	// First fetch.
	key1, err := cache.getKey(context.Background(), srv.URL, "test-kid")
	require.NoError(t, err)
	require.NotNil(t, key1)

	// Second fetch should use cache.
	key2, err := cache.getKey(context.Background(), srv.URL, "test-kid")
	require.NoError(t, err)
	require.NotNil(t, key2)

	// Should have only fetched once.
	assert.Equal(t, 1, fetchCount, "JWKS should have been fetched only once (cached)")
}

func TestJWKSCache_TTLExpiry(t *testing.T) {
	t.Parallel()
	_, rsaPub := jwtTestGenerateRSAKeyPair(t)

	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		jwksDoc, _ := json.Marshal(map[string]any{
			"keys": []map[string]string{
				{
					"kty": "RSA",
					"kid": "test-kid",
					"alg": "RS256",
					"use": "sig",
					"n":   base64.RawURLEncoding.EncodeToString(rsaPub.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaPub.E)).Bytes()),
				},
			},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(jwksDoc)
	}))
	t.Cleanup(srv.Close)

	// Very short TTL.
	cache := newJWKSCache(1*time.Millisecond, srv.Client())

	// First fetch.
	_, err := cache.getKey(context.Background(), srv.URL, "test-kid")
	require.NoError(t, err)

	// Wait for TTL to expire.
	time.Sleep(10 * time.Millisecond)

	// Second fetch should refetch.
	_, err = cache.getKey(context.Background(), srv.URL, "test-kid")
	require.NoError(t, err)

	assert.Equal(t, 2, fetchCount, "JWKS should have been re-fetched after TTL expiry")
}

// ---------------------------------------------------------------------------
// tokenHash tests
// ---------------------------------------------------------------------------

func TestTokenHash_SHA256(t *testing.T) {
	t.Parallel()

	token := "example.jwt.token"
	hash := tokenHash(token)

	// Verify it matches Go's standard SHA-256.
	expected := sha256.Sum256([]byte(token))
	assert.Equal(t, hex.EncodeToString(expected[:]), hash)
}

func TestTokenHash_DifferentTokens_DifferentHashes(t *testing.T) {
	t.Parallel()

	hash1 := tokenHash("token-a")
	hash2 := tokenHash("token-b")
	assert.NotEqual(t, hash1, hash2, "different tokens should produce different hashes")
}

// ---------------------------------------------------------------------------
// classifyError tests
// ---------------------------------------------------------------------------

func TestClassifyError_ExpiredToken(t *testing.T) {
	t.Parallel()
	// jwt.ErrTokenExpired is wrapped in jwt.ValidationError by the library.
	err := fmt.Errorf("token validation: %w", jwt.ErrTokenExpired)
	result := classifyError(err)
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeAuthenticationExpired, result.Code)
}

func TestClassifyError_MalformedToken(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("parse error: %w", jwt.ErrTokenMalformed)
	result := classifyError(err)
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeAuthenticationInvalid, result.Code)
}

func TestClassifyError_InvalidSignature(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("verify error: %w", jwt.ErrSignatureInvalid)
	result := classifyError(err)
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeAuthenticationInvalid, result.Code)
}

func TestClassifyError_AlreadySSError(t *testing.T) {
	t.Parallel()
	original := sserr.New(sserr.CodeAuthentication, "custom auth error")
	result := classifyError(original)
	assert.Equal(t, original, result, "should return the existing *sserr.Error as-is")
}

func TestClassifyError_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, classifyError(nil))
}

// ---------------------------------------------------------------------------
// OTel tests (basic)
// ---------------------------------------------------------------------------

func TestValidate_CreatesSpan(t *testing.T) {
	t.Parallel()

	// Set up a test trace provider with a span recorder.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	// Set the global tracer provider for this test.
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	tokenStr := jwtTestGenerateHMACToken(t, []byte(testSigningKey), jwt.MapClaims{
		"iss": "stricklysoft-platform",
		"sub": "span-test-user",
		"exp": jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		"iat": jwt.NewNumericDate(time.Now()),
	})

	_, err = v.Validate(context.Background(), tokenStr)
	require.NoError(t, err)

	// Flush and check spans.
	_ = tp.ForceFlush(context.Background())

	spans := exporter.GetSpans()
	require.NotEmpty(t, spans, "at least one span should have been created")

	// Look for the auth.Validate span.
	var found bool
	for _, s := range spans {
		if s.Name == "auth.Validate" {
			found = true
			break
		}
	}
	assert.True(t, found, "auth.Validate span should exist in recorded spans")
}

// ---------------------------------------------------------------------------
// Token size limit test
// ---------------------------------------------------------------------------

func TestValidate_OversizedToken(t *testing.T) {
	t.Parallel()
	cfg := newPlatformConfig()
	v, err := NewJWTValidator(cfg)
	require.NoError(t, err)

	// Create a token that exceeds 8192 bytes.
	oversized := strings.Repeat("a", maxTokenSize+1)

	_, err = v.Validate(context.Background(), oversized)
	require.Error(t, err)
	var ssErr *sserr.Error
	require.ErrorAs(t, err, &ssErr)
	assert.Equal(t, sserr.CodeAuthenticationInvalid, ssErr.Code, "oversized token should produce AUTH_003")
	assert.Contains(t, ssErr.Message, "maximum size")
}

// ---------------------------------------------------------------------------
// detectTokenType tests
// ---------------------------------------------------------------------------

func TestDetectTokenType_ByIssuer(t *testing.T) {
	t.Parallel()
	v := &JWTValidator{
		config: ValidatorConfig{
			EnableKubernetes: true,
			EnablePlatform:   true,
			EnableOIDC:       true,
			KubernetesIssuer: "https://kubernetes.default.svc.cluster.local",
			PlatformIssuer:   "stricklysoft-platform",
			OIDCIssuerURL:    "https://auth.example.com",
		},
	}

	assert.Equal(t, TokenTypeKubernetes, v.detectTokenType("https://kubernetes.default.svc.cluster.local", "RS256"))
	assert.Equal(t, TokenTypePlatform, v.detectTokenType("stricklysoft-platform", "HS256"))
	assert.Equal(t, TokenTypeOIDC, v.detectTokenType("https://auth.example.com", "RS256"))
}

func TestDetectTokenType_ByAlgorithmFallback(t *testing.T) {
	t.Parallel()
	v := &JWTValidator{
		config: ValidatorConfig{
			EnableKubernetes: false,
			EnablePlatform:   true,
			EnableOIDC:       true,
			PlatformIssuer:   "stricklysoft-platform",
			OIDCIssuerURL:    "https://auth.example.com",
		},
	}

	// Unknown issuer, but HS256 alg -> platform.
	assert.Equal(t, TokenTypePlatform, v.detectTokenType("unknown-issuer", "HS256"))

	// Unknown issuer, but RS256 alg and OIDC enabled -> oidc.
	assert.Equal(t, TokenTypeOIDC, v.detectTokenType("unknown-issuer", "RS256"))
}

func TestDetectTokenType_NoMatch(t *testing.T) {
	t.Parallel()
	v := &JWTValidator{
		config: ValidatorConfig{
			EnableKubernetes: false,
			EnablePlatform:   false,
			EnableOIDC:       false,
		},
	}

	assert.Equal(t, TokenType(""), v.detectTokenType("unknown", "RS256"))
}

// ---------------------------------------------------------------------------
// ValidatorConfig edge cases
// ---------------------------------------------------------------------------

func TestValidatorConfig_Validate_NegativeJWKSCacheTTL(t *testing.T) {
	t.Parallel()
	cfg := ValidatorConfig{
		EnableKubernetes:  true,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      -1 * time.Second,
		ClockSkew:         30 * time.Second,
	}
	err := cfg.Validate()
	require.NotNil(t, err)
	assert.Contains(t, err.Message, "JWKS cache TTL")
}

func TestValidatorConfig_Validate_NegativeClockSkew(t *testing.T) {
	t.Parallel()
	cfg := ValidatorConfig{
		EnableKubernetes:  true,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 100,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         -1 * time.Second,
	}
	err := cfg.Validate()
	require.NotNil(t, err)
	assert.Contains(t, err.Message, "clock skew")
}

func TestValidatorConfig_Validate_ZeroMaxSize(t *testing.T) {
	t.Parallel()
	cfg := ValidatorConfig{
		EnableKubernetes:  true,
		TokenCacheTTL:     5 * time.Minute,
		TokenCacheMaxSize: 0,
		JWKSCacheTTL:      1 * time.Hour,
		ClockSkew:         30 * time.Second,
	}
	err := cfg.Validate()
	require.NotNil(t, err)
	assert.Contains(t, err.Message, "max size")
}

// ---------------------------------------------------------------------------
// mapClaimsToMap tests
// ---------------------------------------------------------------------------

func TestMapClaimsToMap(t *testing.T) {
	t.Parallel()
	mc := jwt.MapClaims{
		"iss":   "test-issuer",
		"sub":   "test-subject",
		"email": "test@example.com",
	}

	result := mapClaimsToMap(mc)
	assert.Equal(t, "test-issuer", result["iss"])
	assert.Equal(t, "test-subject", result["sub"])
	assert.Equal(t, "test@example.com", result["email"])
	assert.Len(t, result, 3)
}

// ---------------------------------------------------------------------------
// Token cache evictExpired tests
// ---------------------------------------------------------------------------

func TestTokenCache_EvictExpired(t *testing.T) {
	t.Parallel()
	cache := newTokenCache(1*time.Millisecond, 100)

	id1 := NewBasicIdentity("id-1", IdentityTypeUser, nil)
	cache.put("hash-1", id1, time.Now().Add(1*time.Hour))

	// Wait for TTL.
	time.Sleep(10 * time.Millisecond)

	cache.evictExpired()

	cache.mu.RLock()
	size := len(cache.entries)
	cache.mu.RUnlock()
	assert.Equal(t, 0, size, "expired entries should be evicted")
}
