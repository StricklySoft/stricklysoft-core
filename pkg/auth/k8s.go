package auth

import (
	"context"
	"fmt"
	"os"
	"strings"
)

const (
	// DefaultSACACertPath is the standard Kubernetes mount path for the
	// cluster CA certificate.
	DefaultSACACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// DefaultK8sNamespacePath is the standard Kubernetes mount path for the
	// pod's namespace.
	DefaultK8sNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

// ReadServiceAccountToken reads a Kubernetes ServiceAccount token from the
// filesystem at the given path. If path is empty, DefaultSATokenPath is used.
//
// The token is trimmed of leading/trailing whitespace and newlines. Returns
// an error if the file does not exist, cannot be read, or contains no
// content after trimming.
func ReadServiceAccountToken(path string) (string, error) {
	if path == "" {
		path = DefaultSATokenPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("auth: failed to read service account token from %s: %w", path, err)
	}

	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("auth: service account token file %s is empty", path)
	}

	return token, nil
}

// ValidateServiceAccount reads the projected ServiceAccount token from the
// configured path (or DefaultSATokenPath) and validates it using the
// standard Validate method. This is a convenience method for services that
// need to authenticate themselves using their own ServiceAccount token.
//
// The returned Identity will be a *ServiceIdentity with the Kubernetes
// namespace and service account name extracted from the token claims.
func (v *JWTValidator) ValidateServiceAccount(ctx context.Context) (Identity, error) {
	tokenPath := v.config.SATokenPath
	if tokenPath == "" {
		tokenPath = DefaultSATokenPath
	}

	tokenStr, err := ReadServiceAccountToken(tokenPath)
	if err != nil {
		return nil, err
	}

	return v.Validate(ctx, tokenStr)
}

// parseK8sServiceAccountClaims extracts Kubernetes ServiceAccount information
// from JWT claims and constructs a ServiceIdentity. It supports three claim
// formats in order of preference:
//
//  1. Nested format: claims["kubernetes.io"] containing "namespace" and
//     "serviceaccount" with "name" (modern projected tokens).
//
//  2. Flat format: claims["kubernetes.io/serviceaccount/namespace"] and
//     claims["kubernetes.io/serviceaccount/service-account.name"]
//     (legacy bound tokens).
//
//  3. Subject parsing: extracts namespace and name from the sub claim
//     in the format "system:serviceaccount:<namespace>:<name>".
//
// Returns an error if namespace or service account name cannot be determined.
func parseK8sServiceAccountClaims(claims map[string]any, permissions []Permission) (*ServiceIdentity, error) {
	var namespace, saName string

	// Try nested format first: claims["kubernetes.io"] -> map
	if k8sInfo, ok := claims["kubernetes.io"]; ok {
		if k8sMap, ok := k8sInfo.(map[string]any); ok {
			namespace, _ = k8sMap["namespace"].(string)

			if saInfo, ok := k8sMap["serviceaccount"]; ok {
				if saMap, ok := saInfo.(map[string]any); ok {
					saName, _ = saMap["name"].(string)
				}
			}
		}
	}

	// Fall back to flat format.
	if namespace == "" {
		if v, ok := claims["kubernetes.io/serviceaccount/namespace"]; ok {
			namespace, _ = v.(string)
		}
	}
	if saName == "" {
		if v, ok := claims["kubernetes.io/serviceaccount/service-account.name"]; ok {
			saName, _ = v.(string)
		}
	}

	// Fall back to parsing the sub claim.
	if namespace == "" || saName == "" {
		if sub, ok := claims["sub"].(string); ok {
			parts := strings.Split(sub, ":")
			// Expected format: system:serviceaccount:<namespace>:<name>
			if len(parts) == 4 && parts[0] == "system" && parts[1] == "serviceaccount" {
				if namespace == "" {
					namespace = parts[2]
				}
				if saName == "" {
					saName = parts[3]
				}
			}
		}
	}

	if namespace == "" {
		return nil, fmt.Errorf("auth: kubernetes token missing namespace claim")
	}
	if saName == "" {
		return nil, fmt.Errorf("auth: kubernetes token missing service account name claim")
	}

	// Use the sub claim as the identity ID, falling back to a constructed ID.
	id, _ := claims["sub"].(string)
	if id == "" {
		id = fmt.Sprintf("system:serviceaccount:%s:%s", namespace, saName)
	}

	return NewServiceIdentity(id, saName, namespace, claims, permissions)
}
