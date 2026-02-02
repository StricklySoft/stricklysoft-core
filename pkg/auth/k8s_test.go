package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// --------------------------------------------------------------------------
// Test helpers
// --------------------------------------------------------------------------

// mustGenerateRSAKeyPair generates a 2048-bit RSA key pair, failing the
// test if key generation fails.
func mustGenerateRSAKeyPair(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "failed to generate RSA key pair")
	return key
}

// mustGenerateRSAToken creates a signed RS256 JWT with the given claims
// and key ID. Fails the test if signing fails.
func mustGenerateRSAToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	require.NoError(t, err, "failed to sign JWT token")
	return signed
}

// serveJWKS starts an httptest server that serves a JWKS document at the
// /openid/v1/jwks path (matching Kubernetes OIDC discovery convention).
// Returns the server (caller must close via t.Cleanup).
func serveJWKS(t *testing.T, key *rsa.PrivateKey, kid string) *httptest.Server {
	t.Helper()

	nBytes := key.PublicKey.N.Bytes()
	eBytes := big.NewInt(int64(key.PublicKey.E)).Bytes()

	jwksDoc := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(nBytes),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/openid/v1/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwksDoc)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// newK8sTestValidator creates a JWTValidator configured for Kubernetes token
// validation against the given JWKS server. The server URL is used as the
// KubernetesIssuer since the validator constructs the JWKS URL as
// issuer + "/openid/v1/jwks".
func newK8sTestValidator(t *testing.T, serverURL string) *JWTValidator {
	t.Helper()
	cfg := ValidatorConfig{
		EnableKubernetes:   true,
		KubernetesIssuer:   serverURL,
		KubernetesAudience: "https://kubernetes.default.svc.cluster.local",
		TokenCacheTTL:      5 * time.Minute,
		TokenCacheMaxSize:  100,
		JWKSCacheTTL:       1 * time.Hour,
		ClockSkew:          30 * time.Second,
	}
	validator, err := NewJWTValidator(cfg)
	require.NoError(t, err, "NewJWTValidator() unexpected error")
	return validator
}

// makeK8sClaims returns a standard set of Kubernetes ServiceAccount JWT claims
// for testing. The issuer is set to the given server URL.
func makeK8sClaims(issuerURL string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss": issuerURL,
		"sub": "system:serviceaccount:my-namespace:my-service-account",
		"aud": []string{"https://kubernetes.default.svc.cluster.local"},
		"iat": jwt.NewNumericDate(now.Add(-1 * time.Minute)),
		"exp": jwt.NewNumericDate(now.Add(1 * time.Hour)),
		"nbf": jwt.NewNumericDate(now.Add(-1 * time.Minute)),
		"kubernetes.io": map[string]any{
			"namespace": "my-namespace",
			"serviceaccount": map[string]any{
				"name": "my-service-account",
				"uid":  "sa-uid-12345",
			},
		},
	}
}

// --------------------------------------------------------------------------
// TestValidateKubernetesToken_Valid
// --------------------------------------------------------------------------

func TestValidateKubernetesToken_Valid(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKeyPair(t)
	kid := "test-kid-valid"
	server := serveJWKS(t, key, kid)

	validator := newK8sTestValidator(t, server.URL)
	claims := makeK8sClaims(server.URL)
	tokenStr := mustGenerateRSAToken(t, key, kid, claims)

	identity, err := validator.Validate(context.Background(), tokenStr)
	require.NoError(t, err, "Validate() should succeed for valid K8s token")
	require.NotNil(t, identity)

	assert.Equal(t, IdentityTypeService, identity.Type(), "identity type should be service")
	assert.Equal(t, "system:serviceaccount:my-namespace:my-service-account", identity.ID())

	// Verify it is a ServiceIdentity.
	si, ok := identity.(*ServiceIdentity)
	require.True(t, ok, "identity should be *ServiceIdentity")
	assert.Equal(t, "my-service-account", si.ServiceName())
	assert.Equal(t, "my-namespace", si.Namespace())
}

// --------------------------------------------------------------------------
// TestValidateKubernetesToken_ExtractsNamespace
// --------------------------------------------------------------------------

func TestValidateKubernetesToken_ExtractsNamespace(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKeyPair(t)
	kid := "test-kid-ns"
	server := serveJWKS(t, key, kid)

	validator := newK8sTestValidator(t, server.URL)
	claims := makeK8sClaims(server.URL)
	// Override namespace to a custom value.
	k8sInfo := claims["kubernetes.io"].(map[string]any)
	k8sInfo["namespace"] = "production"
	saInfo := k8sInfo["serviceaccount"].(map[string]any)
	saInfo["name"] = "my-service-account"
	claims["sub"] = "system:serviceaccount:production:my-service-account"

	tokenStr := mustGenerateRSAToken(t, key, kid, claims)

	identity, err := validator.Validate(context.Background(), tokenStr)
	require.NoError(t, err)

	si, ok := identity.(*ServiceIdentity)
	require.True(t, ok)
	assert.Equal(t, "production", si.Namespace(), "namespace should be extracted from kubernetes.io claims")
}

// --------------------------------------------------------------------------
// TestValidateKubernetesToken_ExtractsServiceAccountName
// --------------------------------------------------------------------------

func TestValidateKubernetesToken_ExtractsServiceAccountName(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKeyPair(t)
	kid := "test-kid-sa"
	server := serveJWKS(t, key, kid)

	validator := newK8sTestValidator(t, server.URL)
	claims := makeK8sClaims(server.URL)
	// Override service account name.
	k8sInfo := claims["kubernetes.io"].(map[string]any)
	saInfo := k8sInfo["serviceaccount"].(map[string]any)
	saInfo["name"] = "nexus-gateway"
	claims["sub"] = "system:serviceaccount:my-namespace:nexus-gateway"

	tokenStr := mustGenerateRSAToken(t, key, kid, claims)

	identity, err := validator.Validate(context.Background(), tokenStr)
	require.NoError(t, err)

	si, ok := identity.(*ServiceIdentity)
	require.True(t, ok)
	assert.Equal(t, "nexus-gateway", si.ServiceName(), "service account name should be extracted from kubernetes.io claims")
}

// --------------------------------------------------------------------------
// TestValidateKubernetesToken_Expired
// --------------------------------------------------------------------------

func TestValidateKubernetesToken_Expired(t *testing.T) {
	t.Parallel()

	key := mustGenerateRSAKeyPair(t)
	kid := "test-kid-expired"
	server := serveJWKS(t, key, kid)

	validator := newK8sTestValidator(t, server.URL)
	claims := makeK8sClaims(server.URL)
	// Set expiry in the past (beyond clock skew).
	claims["exp"] = jwt.NewNumericDate(time.Now().Add(-10 * time.Minute))
	claims["iat"] = jwt.NewNumericDate(time.Now().Add(-20 * time.Minute))
	claims["nbf"] = jwt.NewNumericDate(time.Now().Add(-20 * time.Minute))

	tokenStr := mustGenerateRSAToken(t, key, kid, claims)

	_, err := validator.Validate(context.Background(), tokenStr)
	require.Error(t, err, "Validate() should fail for expired K8s token")

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "error should be *sserr.Error")
	assert.Equal(t, sserr.CodeAuthenticationExpired, ssErr.Code, "expired token should return AUTH_002")
}

// --------------------------------------------------------------------------
// TestValidateKubernetesToken_InvalidSignature
// --------------------------------------------------------------------------

func TestValidateKubernetesToken_InvalidSignature(t *testing.T) {
	t.Parallel()

	// Generate two different key pairs: one for signing, one for the JWKS.
	signingKey := mustGenerateRSAKeyPair(t)
	jwksKey := mustGenerateRSAKeyPair(t) // different key in JWKS
	kid := "test-kid-bad-sig"
	server := serveJWKS(t, jwksKey, kid)

	validator := newK8sTestValidator(t, server.URL)
	claims := makeK8sClaims(server.URL)

	// Sign with signingKey but JWKS has jwksKey.
	tokenStr := mustGenerateRSAToken(t, signingKey, kid, claims)

	_, err := validator.Validate(context.Background(), tokenStr)
	require.Error(t, err, "Validate() should fail for token with wrong signing key")

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "error should be *sserr.Error")
	assert.Equal(t, sserr.CodeAuthenticationInvalid, ssErr.Code, "invalid signature should return AUTH_003")
}

// --------------------------------------------------------------------------
// TestReadServiceAccountToken_ValidFile
// --------------------------------------------------------------------------

func TestReadServiceAccountToken_ValidFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	expectedToken := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.test-payload.signature"
	err := os.WriteFile(tokenPath, []byte(expectedToken+"\n"), 0o644)
	require.NoError(t, err)

	token, err := ReadServiceAccountToken(tokenPath)
	require.NoError(t, err, "ReadServiceAccountToken() should succeed for valid file")
	assert.Equal(t, expectedToken, token, "token should be trimmed of trailing newline")
}

// --------------------------------------------------------------------------
// TestReadServiceAccountToken_MissingFile
// --------------------------------------------------------------------------

func TestReadServiceAccountToken_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := ReadServiceAccountToken("/nonexistent/path/to/token")
	require.Error(t, err, "ReadServiceAccountToken() should fail for missing file")
	assert.Contains(t, err.Error(), "auth:", "error should have auth: prefix")
}

// --------------------------------------------------------------------------
// TestReadServiceAccountToken_EmptyFile
// --------------------------------------------------------------------------

func TestReadServiceAccountToken_EmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	err := os.WriteFile(tokenPath, []byte(""), 0o644)
	require.NoError(t, err)

	_, err = ReadServiceAccountToken(tokenPath)
	require.Error(t, err, "ReadServiceAccountToken() should fail for empty file")
	assert.Contains(t, err.Error(), "empty", "error should mention empty file")
}

// --------------------------------------------------------------------------
// TestReadServiceAccountToken_DefaultPath
// --------------------------------------------------------------------------

func TestReadServiceAccountToken_DefaultPath(t *testing.T) {
	t.Parallel()

	// Calling with empty string should use DefaultSATokenPath.
	// Behavior depends on whether the test is running inside a
	// Kubernetes pod (e.g., self-hosted CI runners) where the
	// default SA token path exists.
	token, err := ReadServiceAccountToken("")
	if _, statErr := os.Stat(DefaultSATokenPath); statErr == nil {
		// Running inside a K8s pod: the default token file exists.
		require.NoError(t, err, "ReadServiceAccountToken(\"\") should succeed when default path exists")
		assert.NotEmpty(t, token, "token should not be empty when default SA token file exists")
	} else {
		// Not in K8s: file doesn't exist, expect an error.
		require.Error(t, err, "ReadServiceAccountToken(\"\") should fail when default path doesn't exist")
		assert.Contains(t, err.Error(), DefaultSATokenPath, "error should reference the default SA token path")
	}
}

// --------------------------------------------------------------------------
// TestParseK8sServiceAccountClaims_NestedFormat
// --------------------------------------------------------------------------

func TestParseK8sServiceAccountClaims_NestedFormat(t *testing.T) {
	t.Parallel()

	claims := map[string]any{
		"sub": "system:serviceaccount:platform:agent-orchestrator",
		"kubernetes.io": map[string]any{
			"namespace": "platform",
			"serviceaccount": map[string]any{
				"name": "agent-orchestrator",
				"uid":  "uid-123",
			},
		},
	}

	identity, err := parseK8sServiceAccountClaims(claims, nil)
	require.NoError(t, err, "parseK8sServiceAccountClaims() should succeed with nested format")

	assert.Equal(t, "system:serviceaccount:platform:agent-orchestrator", identity.ID())
	assert.Equal(t, "agent-orchestrator", identity.ServiceName())
	assert.Equal(t, "platform", identity.Namespace())
	assert.Equal(t, IdentityTypeService, identity.Type())
}

// --------------------------------------------------------------------------
// TestParseK8sServiceAccountClaims_FlatFormat
// --------------------------------------------------------------------------

func TestParseK8sServiceAccountClaims_FlatFormat(t *testing.T) {
	t.Parallel()

	claims := map[string]any{
		"sub":                                    "system:serviceaccount:default:web-server",
		"kubernetes.io/serviceaccount/namespace": "default",
		"kubernetes.io/serviceaccount/service-account.name": "web-server",
	}

	identity, err := parseK8sServiceAccountClaims(claims, nil)
	require.NoError(t, err, "parseK8sServiceAccountClaims() should succeed with flat format")

	assert.Equal(t, "web-server", identity.ServiceName())
	assert.Equal(t, "default", identity.Namespace())
}

// --------------------------------------------------------------------------
// TestParseK8sServiceAccountClaims_SubjectParsing
// --------------------------------------------------------------------------

func TestParseK8sServiceAccountClaims_SubjectParsing(t *testing.T) {
	t.Parallel()

	claims := map[string]any{
		"sub": "system:serviceaccount:monitoring:prometheus",
	}

	identity, err := parseK8sServiceAccountClaims(claims, nil)
	require.NoError(t, err, "parseK8sServiceAccountClaims() should succeed by parsing sub claim")

	assert.Equal(t, "prometheus", identity.ServiceName(), "service account name should be parsed from sub")
	assert.Equal(t, "monitoring", identity.Namespace(), "namespace should be parsed from sub")
	assert.Equal(t, "system:serviceaccount:monitoring:prometheus", identity.ID())
}

// --------------------------------------------------------------------------
// TestParseK8sServiceAccountClaims_MissingNamespace
// --------------------------------------------------------------------------

func TestParseK8sServiceAccountClaims_MissingNamespace(t *testing.T) {
	t.Parallel()

	claims := map[string]any{
		"sub": "some-random-subject",
		"kubernetes.io": map[string]any{
			"serviceaccount": map[string]any{
				"name": "my-svc",
			},
		},
	}

	_, err := parseK8sServiceAccountClaims(claims, nil)
	require.Error(t, err, "parseK8sServiceAccountClaims() should fail when namespace is missing")
	assert.Contains(t, err.Error(), "namespace", "error should mention missing namespace")
}

// --------------------------------------------------------------------------
// TestParseK8sServiceAccountClaims_MissingServiceAccountName
// --------------------------------------------------------------------------

func TestParseK8sServiceAccountClaims_MissingServiceAccountName(t *testing.T) {
	t.Parallel()

	claims := map[string]any{
		"sub": "some-random-subject",
		"kubernetes.io": map[string]any{
			"namespace": "my-namespace",
		},
	}

	_, err := parseK8sServiceAccountClaims(claims, nil)
	require.Error(t, err, "parseK8sServiceAccountClaims() should fail when service account name is missing")
	assert.Contains(t, err.Error(), "service account name", "error should mention missing service account name")
}

// --------------------------------------------------------------------------
// TestParseK8sServiceAccountClaims_WithPermissions
// --------------------------------------------------------------------------

func TestParseK8sServiceAccountClaims_WithPermissions(t *testing.T) {
	t.Parallel()

	claims := map[string]any{
		"sub": "system:serviceaccount:platform:gateway",
		"kubernetes.io": map[string]any{
			"namespace": "platform",
			"serviceaccount": map[string]any{
				"name": "gateway",
			},
		},
	}
	perms := []Permission{
		{Resource: "agents", Action: "read"},
		{Resource: "agents", Action: "execute"},
	}

	identity, err := parseK8sServiceAccountClaims(claims, perms)
	require.NoError(t, err)

	assert.True(t, identity.HasPermission("agents", "read"), "should have agents:read permission")
	assert.True(t, identity.HasPermission("agents", "execute"), "should have agents:execute permission")
	assert.False(t, identity.HasPermission("agents", "delete"), "should not have agents:delete permission")
}

// --------------------------------------------------------------------------
// TestParseK8sServiceAccountClaims_FallbackSubjectID
// --------------------------------------------------------------------------

func TestParseK8sServiceAccountClaims_FallbackSubjectID(t *testing.T) {
	t.Parallel()

	// When sub is missing, the ID should be constructed from namespace:name.
	claims := map[string]any{
		"kubernetes.io": map[string]any{
			"namespace": "kube-system",
			"serviceaccount": map[string]any{
				"name": "coredns",
			},
		},
	}

	identity, err := parseK8sServiceAccountClaims(claims, nil)
	require.NoError(t, err)
	assert.Equal(t, "system:serviceaccount:kube-system:coredns", identity.ID(),
		"ID should be constructed when sub claim is absent")
}

// --------------------------------------------------------------------------
// TestReadServiceAccountToken_WhitespaceOnlyFile
// --------------------------------------------------------------------------

func TestReadServiceAccountToken_WhitespaceOnlyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	err := os.WriteFile(tokenPath, []byte("   \n  \t  \n"), 0o644)
	require.NoError(t, err)

	_, err = ReadServiceAccountToken(tokenPath)
	require.Error(t, err, "ReadServiceAccountToken() should fail for whitespace-only file")
	assert.Contains(t, err.Error(), "empty", "error should mention empty file")
}

// --------------------------------------------------------------------------
// TestReadServiceAccountToken_TrimsBothEnds
// --------------------------------------------------------------------------

func TestReadServiceAccountToken_TrimsBothEnds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	err := os.WriteFile(tokenPath, []byte("  my-token-value  \n"), 0o644)
	require.NoError(t, err)

	token, err := ReadServiceAccountToken(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "my-token-value", token, "token should have whitespace trimmed from both ends")
}

// --------------------------------------------------------------------------
// TestDefaultSATokenPath_Constant
// --------------------------------------------------------------------------

func TestDefaultSATokenPath_Constant(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/token", DefaultSATokenPath)
}

// --------------------------------------------------------------------------
// TestDefaultSACACertPath_Constant
// --------------------------------------------------------------------------

func TestDefaultSACACertPath_Constant(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt", DefaultSACACertPath)
}

// --------------------------------------------------------------------------
// TestDefaultK8sNamespacePath_Constant
// --------------------------------------------------------------------------

func TestDefaultK8sNamespacePath_Constant(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/namespace", DefaultK8sNamespacePath)
}
