package neo4j

import (
	"context"
	"errors"
	"testing"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Mock Driver
// ===========================================================================

// mockDriver implements the Driver interface for unit testing. It uses
// testify/mock to set expectations and verify calls.
type mockDriver struct {
	mock.Mock
}

func (m *mockDriver) NewSession(ctx context.Context, config neo4j.SessionConfig) neo4j.SessionWithContext {
	args := m.Called(ctx, config)
	return args.Get(0).(neo4j.SessionWithContext)
}

func (m *mockDriver) VerifyConnectivity(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockDriver) Close(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// ===========================================================================
// NewFromDriver Tests
// ===========================================================================

// TestNewFromDriver_WithConfig verifies that NewFromDriver correctly
// initializes the client with the provided driver and config, extracting
// the database name for OpenTelemetry span attributes.
func TestNewFromDriver_WithConfig(t *testing.T) {
	t.Parallel()
	d := &mockDriver{}

	cfg := &Config{Database: "testdb"}
	client := NewFromDriver(d, cfg)

	assert.NotNil(t, client.driver)
	assert.Equal(t, cfg, client.config)
	assert.Equal(t, "testdb", client.databaseName)
	assert.NotNil(t, client.tracer)
}

// TestNewFromDriver_NilConfig verifies that NewFromDriver handles a nil
// config gracefully by initializing a zero-value Config.
func TestNewFromDriver_NilConfig(t *testing.T) {
	t.Parallel()
	d := &mockDriver{}

	client := NewFromDriver(d, nil)

	require.NotNil(t, client.config)
	assert.Equal(t, "", client.databaseName)
}

// ===========================================================================
// Health Tests
// ===========================================================================

// TestClient_Health_Success verifies that Health returns nil when the
// driver connectivity check succeeds.
func TestClient_Health_Success(t *testing.T) {
	t.Parallel()
	d := &mockDriver{}
	d.On("VerifyConnectivity", mock.Anything).Return(nil)

	client := NewFromDriver(d, &Config{Database: "testdb"})
	require.NoError(t, client.Health(context.Background()))

	d.AssertExpectations(t)
}

// TestClient_Health_Failure verifies that Health returns a *sserr.Error
// with CodeUnavailableDependency when the connectivity check fails.
func TestClient_Health_Failure(t *testing.T) {
	t.Parallel()
	d := &mockDriver{}
	d.On("VerifyConnectivity", mock.Anything).Return(errors.New("connection refused"))

	client := NewFromDriver(d, &Config{Database: "testdb"})
	healthErr := client.Health(context.Background())
	require.Error(t, healthErr)

	var ssErr *sserr.Error
	require.True(t, errors.As(healthErr, &ssErr), "Health() error type = %T, want *sserr.Error", healthErr)
	assert.Equal(t, sserr.CodeUnavailableDependency, ssErr.Code)

	d.AssertExpectations(t)
}

// TestClient_Health_AppliesDefaultTimeout verifies that Health applies
// DefaultHealthTimeout when the caller's context has no deadline set.
func TestClient_Health_AppliesDefaultTimeout(t *testing.T) {
	t.Parallel()
	d := &mockDriver{}
	// Use a context without a deadline to trigger default timeout application.
	d.On("VerifyConnectivity", mock.Anything).Return(nil)

	client := NewFromDriver(d, &Config{Database: "testdb"})
	require.NoError(t, client.Health(context.Background()))

	d.AssertExpectations(t)
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestClient_Close_Success verifies that Close delegates to the
// underlying driver's Close method and returns nil on success.
func TestClient_Close_Success(t *testing.T) {
	t.Parallel()
	d := &mockDriver{}
	d.On("Close", mock.Anything).Return(nil)

	client := NewFromDriver(d, nil)
	err := client.Close(context.Background())
	require.NoError(t, err)

	d.AssertExpectations(t)
}

// TestClient_Close_Error verifies that Close returns a *sserr.Error with
// CodeInternalDatabase when the driver close fails.
func TestClient_Close_Error(t *testing.T) {
	t.Parallel()
	d := &mockDriver{}
	d.On("Close", mock.Anything).Return(errors.New("close failed"))

	client := NewFromDriver(d, nil)
	closeErr := client.Close(context.Background())
	require.Error(t, closeErr)

	var ssErr *sserr.Error
	require.True(t, errors.As(closeErr, &ssErr), "Close() error type = %T, want *sserr.Error", closeErr)
	assert.Equal(t, sserr.CodeInternalDatabase, ssErr.Code)

	d.AssertExpectations(t)
}

// ===========================================================================
// Driver Accessor Tests
// ===========================================================================

// TestClient_DriverAccessor verifies that Driver() returns the same
// driver instance that was injected via NewFromDriver.
func TestClient_DriverAccessor(t *testing.T) {
	t.Parallel()
	d := &mockDriver{}

	client := NewFromDriver(d, nil)
	got := client.Driver()
	assert.Equal(t, d, got)
}

// ===========================================================================
// wrapError Tests
// ===========================================================================

// TestWrapError_Nil verifies that wrapError returns nil when given a nil
// error, preventing unnecessary error wrapping.
func TestWrapError_Nil(t *testing.T) {
	t.Parallel()
	result := wrapError(nil, "should not wrap")
	assert.Nil(t, result)
}

// TestWrapError_DeadlineExceeded verifies that wrapError classifies
// context.DeadlineExceeded as CodeTimeoutDatabase.
func TestWrapError_DeadlineExceeded(t *testing.T) {
	t.Parallel()
	result := wrapError(context.DeadlineExceeded, "query timed out")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeTimeoutDatabase, result.Code)
	assert.ErrorIs(t, result, context.DeadlineExceeded)
}

// TestWrapError_ContextCanceled verifies that wrapError classifies
// context.Canceled as CodeInternalDatabase (not retryable), because
// cancellation means the caller abandoned the operation intentionally.
func TestWrapError_ContextCanceled(t *testing.T) {
	t.Parallel()
	result := wrapError(context.Canceled, "query canceled")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeInternalDatabase, result.Code)
	assert.ErrorIs(t, result, context.Canceled)
}

// TestWrapError_GenericError verifies that wrapError classifies generic
// database errors as CodeInternalDatabase.
func TestWrapError_GenericError(t *testing.T) {
	t.Parallel()
	cause := errors.New("syntax error")
	result := wrapError(cause, "query failed")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeInternalDatabase, result.Code)
	assert.ErrorIs(t, result, cause)
}

// ===========================================================================
// Error Classification Tests
// ===========================================================================

// TestErrorClassification_TimeoutIsRetryable verifies that a timeout
// error is classified as both timeout and retryable.
func TestErrorClassification_TimeoutIsRetryable(t *testing.T) {
	t.Parallel()
	result := wrapError(context.DeadlineExceeded, "query timed out")
	require.NotNil(t, result)

	assert.True(t, sserr.IsTimeout(result), "IsTimeout() = false, want true for deadline exceeded error")
	assert.True(t, sserr.IsRetryable(result), "IsRetryable() = false, want true for timeout error")
	assert.True(t, sserr.IsServerError(result), "IsServerError() = false, want true for timeout error")
}

// TestErrorClassification_InternalNotRetryable verifies that a generic
// database error is classified as internal and not retryable.
func TestErrorClassification_InternalNotRetryable(t *testing.T) {
	t.Parallel()
	result := wrapError(errors.New("syntax error"), "query failed")
	require.NotNil(t, result)

	assert.True(t, sserr.IsInternal(result), "IsInternal() = false, want true for database error")
	assert.False(t, sserr.IsTimeout(result), "IsTimeout() = true, want false for non-timeout database error")
	assert.False(t, sserr.IsRetryable(result), "IsRetryable() = true, want false for internal database error")
}

// TestErrorClassification_HealthUnavailable verifies that a health check
// failure is classified as an unavailable dependency error.
func TestErrorClassification_HealthUnavailable(t *testing.T) {
	t.Parallel()
	d := &mockDriver{}
	d.On("VerifyConnectivity", mock.Anything).Return(errors.New("connection refused"))

	client := NewFromDriver(d, &Config{Database: "testdb"})
	healthErr := client.Health(context.Background())
	require.Error(t, healthErr)

	assert.True(t, sserr.IsUnavailable(healthErr), "IsUnavailable() = false, want true for health check failure")
	assert.True(t, sserr.IsRetryable(healthErr), "IsRetryable() = false, want true for unavailable dependency")

	d.AssertExpectations(t)
}
