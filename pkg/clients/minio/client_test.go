package minio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Mock ObjectStore
// ===========================================================================

// mockObjectStore is a testify/mock implementation of ObjectStore for
// unit testing Client methods without a real MinIO server.
type mockObjectStore struct {
	mock.Mock
}

func (m *mockObjectStore) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	args := m.Called(ctx, bucketName, objectName, reader, objectSize, opts)
	return args.Get(0).(minio.UploadInfo), args.Error(1)
}

func (m *mockObjectStore) GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) (*minio.Object, error) {
	args := m.Called(ctx, bucketName, objectName, opts)
	obj, _ := args.Get(0).(*minio.Object)
	return obj, args.Error(1)
}

func (m *mockObjectStore) RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error {
	args := m.Called(ctx, bucketName, objectName, opts)
	return args.Error(0)
}

func (m *mockObjectStore) StatObject(ctx context.Context, bucketName, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error) {
	args := m.Called(ctx, bucketName, objectName, opts)
	return args.Get(0).(minio.ObjectInfo), args.Error(1)
}

func (m *mockObjectStore) ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	args := m.Called(ctx, bucketName, opts)
	return args.Get(0).(<-chan minio.ObjectInfo)
}

func (m *mockObjectStore) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	args := m.Called(ctx, bucketName)
	return args.Bool(0), args.Error(1)
}

func (m *mockObjectStore) MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error {
	args := m.Called(ctx, bucketName, opts)
	return args.Error(0)
}

func (m *mockObjectStore) RemoveBucket(ctx context.Context, bucketName string) error {
	args := m.Called(ctx, bucketName)
	return args.Error(0)
}

func (m *mockObjectStore) PresignedGetObject(ctx context.Context, bucketName, objectName string, expires time.Duration, reqParams url.Values) (*url.URL, error) {
	args := m.Called(ctx, bucketName, objectName, expires, reqParams)
	u, _ := args.Get(0).(*url.URL)
	return u, args.Error(1)
}

func (m *mockObjectStore) PresignedPutObject(ctx context.Context, bucketName, objectName string, expires time.Duration) (*url.URL, error) {
	args := m.Called(ctx, bucketName, objectName, expires)
	u, _ := args.Get(0).(*url.URL)
	return u, args.Error(1)
}

// ===========================================================================
// NewFromStore Tests
// ===========================================================================

// TestNewFromStore_WithConfig verifies that NewFromStore correctly initializes
// the client with the provided store and config.
func TestNewFromStore_WithConfig(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}
	cfg := &Config{Endpoint: "localhost:9000", AccessKey: "test"}
	client := NewFromStore(ms, cfg)

	assert.NotNil(t, client.store)
	assert.Equal(t, cfg, client.config)
	assert.NotNil(t, client.tracer)
}

// TestNewFromStore_NilConfig verifies that NewFromStore handles a nil config
// gracefully by initializing a zero-value Config.
func TestNewFromStore_NilConfig(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}
	client := NewFromStore(ms, nil)

	require.NotNil(t, client.config)
	assert.Equal(t, "", client.config.Endpoint)
}

// ===========================================================================
// PutObject Tests
// ===========================================================================

// TestClient_PutObject_Success verifies that PutObject returns upload info
// on a successful upload.
func TestClient_PutObject_Success(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	expectedInfo := minio.UploadInfo{
		Bucket: "test-bucket",
		Key:    "test-key",
		Size:   11,
	}
	reader := bytes.NewReader([]byte("hello world"))
	ms.On("PutObject", mock.Anything, "test-bucket", "test-key", reader, int64(11), minio.PutObjectOptions{}).
		Return(expectedInfo, nil)

	client := NewFromStore(ms, &Config{})
	info, err := client.PutObject(context.Background(), "test-bucket", "test-key", reader, 11, minio.PutObjectOptions{})
	require.NoError(t, err)
	assert.Equal(t, "test-bucket", info.Bucket)
	assert.Equal(t, "test-key", info.Key)

	ms.AssertExpectations(t)
}

// TestClient_PutObject_Error verifies that PutObject returns a *sserr.Error
// with CodeInternalDatabase when the store returns a non-timeout error.
func TestClient_PutObject_Error(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	reader := bytes.NewReader([]byte("data"))
	ms.On("PutObject", mock.Anything, "test-bucket", "test-key", reader, int64(4), minio.PutObjectOptions{}).
		Return(minio.UploadInfo{}, errors.New("access denied"))

	client := NewFromStore(ms, &Config{})
	_, err := client.PutObject(context.Background(), "test-bucket", "test-key", reader, 4, minio.PutObjectOptions{})
	require.Error(t, err)

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "PutObject() error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeInternalDatabase, ssErr.Code)

	ms.AssertExpectations(t)
}

// ===========================================================================
// GetObject Tests
// ===========================================================================

// TestClient_GetObject_Success verifies that GetObject returns an object
// on a successful retrieval.
func TestClient_GetObject_Success(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	// minio.Object is a concrete type that cannot be easily constructed in
	// tests. We return a nil *minio.Object to verify the call succeeds.
	ms.On("GetObject", mock.Anything, "test-bucket", "test-key", minio.GetObjectOptions{}).
		Return((*minio.Object)(nil), nil)

	client := NewFromStore(ms, &Config{})
	_, err := client.GetObject(context.Background(), "test-bucket", "test-key", minio.GetObjectOptions{})
	require.NoError(t, err)

	ms.AssertExpectations(t)
}

// TestClient_GetObject_Error verifies that GetObject returns a *sserr.Error
// with CodeInternalDatabase when the store returns an error.
func TestClient_GetObject_Error(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	ms.On("GetObject", mock.Anything, "test-bucket", "nonexistent", minio.GetObjectOptions{}).
		Return((*minio.Object)(nil), errors.New("key does not exist"))

	client := NewFromStore(ms, &Config{})
	_, err := client.GetObject(context.Background(), "test-bucket", "nonexistent", minio.GetObjectOptions{})
	require.Error(t, err)

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "GetObject() error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeInternalDatabase, ssErr.Code)

	ms.AssertExpectations(t)
}

// ===========================================================================
// RemoveObject Tests
// ===========================================================================

// TestClient_RemoveObject_Success verifies that RemoveObject returns nil
// on a successful deletion.
func TestClient_RemoveObject_Success(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	ms.On("RemoveObject", mock.Anything, "test-bucket", "test-key", minio.RemoveObjectOptions{}).
		Return(nil)

	client := NewFromStore(ms, &Config{})
	err := client.RemoveObject(context.Background(), "test-bucket", "test-key", minio.RemoveObjectOptions{})
	require.NoError(t, err)

	ms.AssertExpectations(t)
}

// ===========================================================================
// StatObject Tests
// ===========================================================================

// TestClient_StatObject_Success verifies that StatObject returns object info
// on a successful stat call.
func TestClient_StatObject_Success(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	expectedInfo := minio.ObjectInfo{
		Key:  "test-key",
		Size: 1024,
	}
	ms.On("StatObject", mock.Anything, "test-bucket", "test-key", minio.StatObjectOptions{}).
		Return(expectedInfo, nil)

	client := NewFromStore(ms, &Config{})
	info, err := client.StatObject(context.Background(), "test-bucket", "test-key", minio.StatObjectOptions{})
	require.NoError(t, err)
	assert.Equal(t, "test-key", info.Key)
	assert.Equal(t, int64(1024), info.Size)

	ms.AssertExpectations(t)
}

// ===========================================================================
// BucketExists Tests
// ===========================================================================

// TestClient_BucketExists_Success verifies that BucketExists returns the
// correct boolean result from the store.
func TestClient_BucketExists_Success(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	ms.On("BucketExists", mock.Anything, "test-bucket").
		Return(true, nil)

	client := NewFromStore(ms, &Config{})
	exists, err := client.BucketExists(context.Background(), "test-bucket")
	require.NoError(t, err)
	assert.True(t, exists)

	ms.AssertExpectations(t)
}

// ===========================================================================
// MakeBucket Tests
// ===========================================================================

// TestClient_MakeBucket_Success verifies that MakeBucket returns nil
// on a successful bucket creation.
func TestClient_MakeBucket_Success(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	ms.On("MakeBucket", mock.Anything, "new-bucket", minio.MakeBucketOptions{}).
		Return(nil)

	client := NewFromStore(ms, &Config{})
	err := client.MakeBucket(context.Background(), "new-bucket", minio.MakeBucketOptions{})
	require.NoError(t, err)

	ms.AssertExpectations(t)
}

// ===========================================================================
// RemoveBucket Tests
// ===========================================================================

// TestClient_RemoveBucket_Success verifies that RemoveBucket returns nil
// on a successful bucket deletion.
func TestClient_RemoveBucket_Success(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	ms.On("RemoveBucket", mock.Anything, "old-bucket").
		Return(nil)

	client := NewFromStore(ms, &Config{})
	err := client.RemoveBucket(context.Background(), "old-bucket")
	require.NoError(t, err)

	ms.AssertExpectations(t)
}

// ===========================================================================
// Health Tests
// ===========================================================================

// TestClient_Health_Success verifies that Health returns nil when the
// store's BucketExists call succeeds.
func TestClient_Health_Success(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	ms.On("BucketExists", mock.Anything, "health-check-probe").
		Return(false, nil)

	client := NewFromStore(ms, &Config{})
	require.NoError(t, client.Health(context.Background()))

	ms.AssertExpectations(t)
}

// TestClient_Health_Failure verifies that Health returns a *sserr.Error with
// CodeUnavailableDependency when the store's BucketExists call fails.
func TestClient_Health_Failure(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	ms.On("BucketExists", mock.Anything, "health-check-probe").
		Return(false, errors.New("connection refused"))

	client := NewFromStore(ms, &Config{})
	healthErr := client.Health(context.Background())
	require.Error(t, healthErr)

	var ssErr *sserr.Error
	require.True(t, errors.As(healthErr, &ssErr), "Health() error type = %T, want *sserr.Error", healthErr)
	assert.Equal(t, sserr.CodeUnavailableDependency, ssErr.Code)

	ms.AssertExpectations(t)
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestClient_Close_IsNoOp verifies that Close does not panic or error.
// The MinIO client uses stateless HTTP, so Close is a no-op.
func TestClient_Close_IsNoOp(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}
	client := NewFromStore(ms, nil)

	// Close should not panic.
	assert.NotPanics(t, func() {
		client.Close()
	})

	// Close can be called multiple times safely.
	assert.NotPanics(t, func() {
		client.Close()
	})
}

// ===========================================================================
// Store Accessor Tests
// ===========================================================================

// TestClient_Store_ReturnsUnderlyingStore verifies that Store() returns the
// same store instance that was injected via NewFromStore.
func TestClient_Store_ReturnsUnderlyingStore(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}
	client := NewFromStore(ms, nil)

	store := client.Store()
	assert.Equal(t, ms, store)
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
	result := wrapError(context.DeadlineExceeded, "operation timed out")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeTimeoutDatabase, result.Code)
	assert.ErrorIs(t, result, context.DeadlineExceeded)
}

// TestWrapError_ContextCanceled verifies that wrapError classifies
// context.Canceled as CodeInternalDatabase (not retryable), because
// cancellation means the caller abandoned the operation intentionally.
func TestWrapError_ContextCanceled(t *testing.T) {
	t.Parallel()
	result := wrapError(context.Canceled, "operation canceled")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeInternalDatabase, result.Code)
	assert.ErrorIs(t, result, context.Canceled)
}

// TestWrapError_GenericError verifies that wrapError classifies generic
// storage errors as CodeInternalDatabase.
func TestWrapError_GenericError(t *testing.T) {
	t.Parallel()
	cause := errors.New("access denied")
	result := wrapError(cause, "put object failed")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeInternalDatabase, result.Code)
	assert.ErrorIs(t, result, cause)
}

// ===========================================================================
// Error Classification Integration Tests
// ===========================================================================

// TestErrorClassification_PutObjectTimeout verifies the full error
// classification pipeline: a timeout error from PutObject is classified
// correctly by the platform error helpers (IsTimeout, IsRetryable).
func TestErrorClassification_PutObjectTimeout(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	reader := bytes.NewReader([]byte("data"))
	ms.On("PutObject", mock.Anything, "test-bucket", "test-key", reader, int64(4), minio.PutObjectOptions{}).
		Return(minio.UploadInfo{}, context.DeadlineExceeded)

	client := NewFromStore(ms, &Config{})
	_, err := client.PutObject(context.Background(), "test-bucket", "test-key", reader, 4, minio.PutObjectOptions{})
	require.Error(t, err)

	assert.True(t, sserr.IsTimeout(err), "IsTimeout() = false, want true for deadline exceeded error")
	assert.True(t, sserr.IsRetryable(err), "IsRetryable() = false, want true for timeout error")
	assert.True(t, sserr.IsServerError(err), "IsServerError() = false, want true for timeout error")
}

// TestErrorClassification_GetObjectInternalDatabase verifies that a generic
// storage error from GetObject is classified as an internal error.
func TestErrorClassification_GetObjectInternalDatabase(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	ms.On("GetObject", mock.Anything, "test-bucket", "test-key", minio.GetObjectOptions{}).
		Return((*minio.Object)(nil), errors.New("access denied"))

	client := NewFromStore(ms, &Config{})
	_, err := client.GetObject(context.Background(), "test-bucket", "test-key", minio.GetObjectOptions{})
	require.Error(t, err)

	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for storage error")
	assert.False(t, sserr.IsTimeout(err), "IsTimeout() = true, want false for non-timeout storage error")
	assert.False(t, sserr.IsRetryable(err), "IsRetryable() = true, want false for internal storage error")
}

// TestErrorClassification_HealthUnavailable verifies that a health check
// failure is classified as an unavailable dependency error.
func TestErrorClassification_HealthUnavailable(t *testing.T) {
	t.Parallel()
	ms := &mockObjectStore{}

	ms.On("BucketExists", mock.Anything, "health-check-probe").
		Return(false, errors.New("connection refused"))

	client := NewFromStore(ms, &Config{})
	healthErr := client.Health(context.Background())
	require.Error(t, healthErr)

	assert.True(t, sserr.IsUnavailable(healthErr), "IsUnavailable() = false, want true for health check failure")
	assert.True(t, sserr.IsRetryable(healthErr), "IsRetryable() = false, want true for unavailable dependency")
}
