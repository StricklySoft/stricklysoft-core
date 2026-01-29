// Package testutil provides shared test helpers for the StricklySoft Core SDK.
//
// All helpers accept [testing.TB] for compatibility with both tests and
// benchmarks. Functions that halt the test on failure use [require] from
// testify; functions that record failures without stopping use [assert].
//
// Every helper calls t.Helper() so that test failure messages report the
// caller's file and line number rather than this package's.
package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// RequireNoError halts the test immediately if err is non-nil.
// Use this for preconditions whose failure makes continuing meaningless.
func RequireNoError(t testing.TB, err error, msgAndArgs ...any) {
	t.Helper()
	require.NoError(t, err, msgAndArgs...)
}

// RequireError halts the test immediately if err is nil.
// Use this when an error is expected and subsequent assertions depend on it.
func RequireError(t testing.TB, err error, msgAndArgs ...any) {
	t.Helper()
	require.Error(t, err, msgAndArgs...)
}

// RequireErrorCode halts the test if err is nil, is not an *sserr.Error,
// or does not carry the expected error code. This is the primary helper
// for validating platform error responses.
//
// Example:
//
//	err := loader.Load(nil)
//	testutil.RequireErrorCode(t, err, sserr.CodeInternalConfiguration)
func RequireErrorCode(t testing.TB, err error, code sserr.Code, msgAndArgs ...any) {
	t.Helper()
	require.Error(t, err, msgAndArgs...)
	ssErr, ok := sserr.AsError(err)
	require.True(t, ok, "expected *sserr.Error, got %T: %v", err, err)
	require.Equal(t, code, ssErr.Code,
		"error code mismatch: got %q, want %q (message: %s)",
		ssErr.Code, code, ssErr.Message)
}

// AssertErrorCode records a test failure (without halting) if err is nil,
// is not an *sserr.Error, or does not carry the expected error code.
// Use this in table-driven tests where you want to check all rows.
func AssertErrorCode(t testing.TB, err error, code sserr.Code, msgAndArgs ...any) bool {
	t.Helper()
	if !assert.Error(t, err, msgAndArgs...) {
		return false
	}
	ssErr, ok := sserr.AsError(err)
	if !assert.True(t, ok, "expected *sserr.Error, got %T: %v", err, err) {
		return false
	}
	return assert.Equal(t, code, ssErr.Code,
		"error code mismatch: got %q, want %q (message: %s)",
		ssErr.Code, code, ssErr.Message)
}

// AssertNoSSError records a test failure if err is non-nil and is an
// *sserr.Error, printing the code and message for diagnostics.
func AssertNoSSError(t testing.TB, err error) bool {
	t.Helper()
	if err == nil {
		return true
	}
	if ssErr, ok := sserr.AsError(err); ok {
		return assert.Fail(t,
			"unexpected sserr.Error",
			"code=%s message=%s", ssErr.Code, ssErr.Message)
	}
	return assert.NoError(t, err)
}

// TempConfigFile creates a temporary file with the given content and
// extension (e.g., ".yaml", ".json") inside t.TempDir(). The file is
// automatically cleaned up when the test finishes.
//
// The file is created with mode 0600 (owner read/write only) following
// the principle of least privilege for configuration files.
func TempConfigFile(t testing.TB, content, ext string) string {
	t.Helper()
	dir := t.TempDir()
	name := "config" + ext
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err, "failed to write temp config file %s", path)
	return path
}

// TempFile creates a temporary file with the given name and content
// inside t.TempDir(). The file is automatically cleaned up when the
// test finishes.
func TempFile(t testing.TB, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err, "failed to write temp file %s", path)
	return path
}

// SetEnv sets an environment variable and registers a cleanup function
// that restores the original value (or unsets it if it was not set)
// when the test completes.
//
// This is safe for use in parallel tests only if each test sets a
// unique environment variable. For shared variables, do not use
// t.Parallel().
func SetEnv(t testing.TB, key, value string) {
	t.Helper()
	prev, existed := os.LookupEnv(key)
	err := os.Setenv(key, value)
	require.NoError(t, err, "failed to set env var %s", key)
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// UnsetEnv unsets an environment variable and registers a cleanup
// function that restores the original value when the test completes.
func UnsetEnv(t testing.TB, key string) {
	t.Helper()
	prev, existed := os.LookupEnv(key)
	err := os.Unsetenv(key)
	require.NoError(t, err, "failed to unset env var %s", key)
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, prev)
		}
	})
}

// AssertJSONRoundTrip marshals v to JSON and unmarshals it back into a
// new value of the same type, asserting that the result equals the
// original. This validates that JSON serialization/deserialization is
// lossless for the given value.
//
// The function uses [assert.Equal] which performs deep comparison,
// handling nested structs, slices, and maps correctly.
func AssertJSONRoundTrip[T any](t testing.TB, v T) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err, "json.Marshal failed")

	var decoded T
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "json.Unmarshal failed")

	assert.Equal(t, v, decoded, "JSON round-trip produced different value")
}

// AssertJSONContains marshals v to JSON and asserts that the resulting
// JSON string contains the expected substring. Useful for verifying
// that specific fields appear in serialized output.
func AssertJSONContains(t testing.TB, v any, expected string) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err, "json.Marshal failed")
	assert.Contains(t, string(data), expected,
		"expected JSON to contain %q, got: %s", expected, string(data))
}

// AssertJSONNotContains marshals v to JSON and asserts that the
// resulting JSON string does not contain the unexpected substring.
// Useful for verifying that sensitive fields are redacted.
func AssertJSONNotContains(t testing.TB, v any, unexpected string) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err, "json.Marshal failed")
	assert.NotContains(t, string(data), unexpected,
		"expected JSON to NOT contain %q, got: %s", unexpected, string(data))
}
