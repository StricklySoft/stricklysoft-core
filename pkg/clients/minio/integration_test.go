//go:build integration

// Package minio_test contains integration tests for the MinIO client
// that require a running MinIO instance via testcontainers-go. These
// tests are gated behind the "integration" build tag and are executed in CI
// with Docker.
//
// Run locally with:
//
//	go test -v -race -tags=integration ./pkg/clients/minio/...
//
// Or via Makefile:
//
//	make test-integration
//
// # Architecture
//
// All tests run within a single [suite.Suite] that starts one MinIO
// container in [SetupSuite] and terminates it in [TearDownSuite]. Test
// isolation is achieved via unique bucket names per test method rather than
// per-test containers, which reduces total execution time significantly.
//
// # Bucket Naming Convention
//
// Each test method creates buckets with a unique prefix derived from its
// test category (e.g., test-putget-XXXX, test-list-XXXX). Buckets are
// cleaned up within each test when possible, but the entire container
// is destroyed at suite teardown.
package minio_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	miniogo "github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/StricklySoft/stricklysoft-core/internal/testutil/containers"
	"github.com/StricklySoft/stricklysoft-core/pkg/clients/minio"
	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// stripScheme removes the http:// or https:// scheme prefix from a URL
// if present, returning just the host:port. The minio-go client expects
// a bare endpoint (e.g., "localhost:9000") without scheme.
func stripScheme(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	// Also trim trailing slash if any.
	endpoint = strings.TrimRight(endpoint, "/")
	return endpoint
}

// ===========================================================================
// Suite Definition
// ===========================================================================

// MinIOIntegrationSuite runs all MinIO integration tests against a single
// shared container. The container is started once in SetupSuite and
// terminated in TearDownSuite. All test methods share the same client,
// using unique bucket names for isolation.
type MinIOIntegrationSuite struct {
	suite.Suite

	// ctx is the background context used for container and client
	// lifecycle operations.
	ctx context.Context

	// minioResult holds the started MinIO container and connection
	// details. It is set in SetupSuite and used to terminate the
	// container in TearDownSuite.
	minioResult *containers.MinIOResult

	// client is the SDK MinIO client connected to the test container.
	// All test methods use this client unless they need to test client
	// creation or close behavior.
	client *minio.Client
}

// uniqueBucket generates a unique bucket name for test isolation.
// Bucket names in S3/MinIO must be lowercase, 3-63 characters, and
// may contain hyphens.
func (s *MinIOIntegrationSuite) uniqueBucket(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%100000)
}

// SetupSuite starts a single MinIO container and creates a client
// shared across all tests in the suite. This runs once before any test
// method executes.
func (s *MinIOIntegrationSuite) SetupSuite() {
	s.ctx = context.Background()

	result, err := containers.StartMinIO(s.ctx)
	require.NoError(s.T(), err, "failed to start MinIO container")
	s.minioResult = result

	cfg := minio.Config{
		Endpoint:  stripScheme(result.Endpoint),
		AccessKey: result.AccessKey,
		SecretKey: minio.Secret(result.SecretKey),
		Region:    "us-east-1",
		UseSSL:    false,
	}
	require.NoError(s.T(), cfg.Validate(), "failed to validate config")

	client, err := minio.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err, "failed to create MinIO client")
	s.client = client
}

// TearDownSuite closes the client (no-op) and terminates the container.
// This runs once after all test methods have completed.
func (s *MinIOIntegrationSuite) TearDownSuite() {
	if s.client != nil {
		s.client.Close()
	}
	if s.minioResult != nil {
		if err := s.minioResult.Container.Terminate(s.ctx); err != nil {
			s.T().Logf("failed to terminate minio container: %v", err)
		}
	}
}

// TestMinIOIntegration is the top-level entry point that runs all
// suite tests. It is skipped in short mode (-short flag) to allow fast
// unit test runs without Docker.
func TestMinIOIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	suite.Run(t, new(MinIOIntegrationSuite))
}

// ===========================================================================
// Connection Tests
// ===========================================================================

// TestNewClient_ConnectsSuccessfully verifies that NewClient can
// establish a connection to a real MinIO instance and that the
// returned client is functional.
func (s *MinIOIntegrationSuite) TestNewClient_ConnectsSuccessfully() {
	require.NotNil(s.T(), s.client, "suite client should not be nil")
}

// TestHealth_ReturnsNil verifies that Health returns nil when the
// MinIO server is reachable and responding to API calls.
func (s *MinIOIntegrationSuite) TestHealth_ReturnsNil() {
	err := s.client.Health(s.ctx)
	require.NoError(s.T(), err, "Health() should succeed when MinIO is reachable")
}

// ===========================================================================
// Bucket Tests
// ===========================================================================

// TestMakeBucket_And_BucketExists verifies that MakeBucket creates a
// bucket and BucketExists correctly reports its existence.
func (s *MinIOIntegrationSuite) TestMakeBucket_And_BucketExists() {
	bucket := s.uniqueBucket("test-mkbucket")

	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err, "MakeBucket should succeed")

	exists, err := s.client.BucketExists(s.ctx, bucket)
	require.NoError(s.T(), err, "BucketExists should succeed")
	assert.True(s.T(), exists, "bucket should exist after creation")

	// Cleanup.
	_ = s.client.RemoveBucket(s.ctx, bucket)
}

// TestRemoveBucket verifies that RemoveBucket deletes an empty bucket.
func (s *MinIOIntegrationSuite) TestRemoveBucket() {
	bucket := s.uniqueBucket("test-rmbucket")

	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)

	err = s.client.RemoveBucket(s.ctx, bucket)
	require.NoError(s.T(), err, "RemoveBucket should succeed")

	exists, err := s.client.BucketExists(s.ctx, bucket)
	require.NoError(s.T(), err)
	assert.False(s.T(), exists, "bucket should not exist after removal")
}

// ===========================================================================
// Object Tests
// ===========================================================================

// TestPutObject_And_GetObject verifies that PutObject uploads content
// and GetObject retrieves it back with matching data.
func (s *MinIOIntegrationSuite) TestPutObject_And_GetObject() {
	bucket := s.uniqueBucket("test-putget")
	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)
	defer func() { _ = s.client.RemoveBucket(s.ctx, bucket) }()

	content := "hello, minio integration test!"
	reader := bytes.NewReader([]byte(content))

	_, err = s.client.PutObject(s.ctx, bucket, "test-key.txt", reader, int64(len(content)), miniogo.PutObjectOptions{
		ContentType: "text/plain",
	})
	require.NoError(s.T(), err, "PutObject should succeed")

	obj, err := s.client.GetObject(s.ctx, bucket, "test-key.txt", miniogo.GetObjectOptions{})
	require.NoError(s.T(), err, "GetObject should succeed")
	defer func() { _ = obj.Close() }()

	data, err := io.ReadAll(obj)
	require.NoError(s.T(), err, "reading object should succeed")
	assert.Equal(s.T(), content, string(data), "retrieved content should match uploaded content")

	// Cleanup.
	_ = s.client.RemoveObject(s.ctx, bucket, "test-key.txt", miniogo.RemoveObjectOptions{})
}

// TestStatObject verifies that StatObject returns correct metadata
// including size and content type.
func (s *MinIOIntegrationSuite) TestStatObject() {
	bucket := s.uniqueBucket("test-stat")
	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)
	defer func() {
		_ = s.client.RemoveObject(s.ctx, bucket, "stat-key.txt", miniogo.RemoveObjectOptions{})
		_ = s.client.RemoveBucket(s.ctx, bucket)
	}()

	content := "stat test content"
	reader := bytes.NewReader([]byte(content))
	_, err = s.client.PutObject(s.ctx, bucket, "stat-key.txt", reader, int64(len(content)), miniogo.PutObjectOptions{
		ContentType: "text/plain",
	})
	require.NoError(s.T(), err)

	info, err := s.client.StatObject(s.ctx, bucket, "stat-key.txt", miniogo.StatObjectOptions{})
	require.NoError(s.T(), err, "StatObject should succeed")
	assert.Equal(s.T(), int64(len(content)), info.Size, "size should match uploaded content length")
	assert.Equal(s.T(), "text/plain", info.ContentType, "content type should match")
}

// TestRemoveObject verifies that RemoveObject deletes an object
// from a bucket.
func (s *MinIOIntegrationSuite) TestRemoveObject() {
	bucket := s.uniqueBucket("test-rmobj")
	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)
	defer func() { _ = s.client.RemoveBucket(s.ctx, bucket) }()

	content := "remove me"
	reader := bytes.NewReader([]byte(content))
	_, err = s.client.PutObject(s.ctx, bucket, "remove-key.txt", reader, int64(len(content)), miniogo.PutObjectOptions{})
	require.NoError(s.T(), err)

	err = s.client.RemoveObject(s.ctx, bucket, "remove-key.txt", miniogo.RemoveObjectOptions{})
	require.NoError(s.T(), err, "RemoveObject should succeed")

	// Verify the object no longer exists by trying to stat it.
	_, statErr := s.client.StatObject(s.ctx, bucket, "remove-key.txt", miniogo.StatObjectOptions{})
	require.Error(s.T(), statErr, "StatObject should fail after removal")
}

// TestListObjects verifies that ListObjects returns the correct count
// of objects after putting multiple objects.
func (s *MinIOIntegrationSuite) TestListObjects() {
	bucket := s.uniqueBucket("test-list")
	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)
	defer func() {
		// Cleanup objects and bucket.
		for i := 0; i < 3; i++ {
			_ = s.client.RemoveObject(s.ctx, bucket, fmt.Sprintf("list-key-%d.txt", i), miniogo.RemoveObjectOptions{})
		}
		_ = s.client.RemoveBucket(s.ctx, bucket)
	}()

	// Put 3 objects.
	for i := 0; i < 3; i++ {
		content := fmt.Sprintf("content-%d", i)
		reader := bytes.NewReader([]byte(content))
		_, putErr := s.client.PutObject(s.ctx, bucket, fmt.Sprintf("list-key-%d.txt", i), reader, int64(len(content)), miniogo.PutObjectOptions{})
		require.NoError(s.T(), putErr)
	}

	// List objects and drain the channel.
	ch := s.client.ListObjects(s.ctx, bucket, miniogo.ListObjectsOptions{Recursive: true})
	var objects []miniogo.ObjectInfo
	for obj := range ch {
		require.Empty(s.T(), obj.Err, "ListObjects should not produce errors")
		objects = append(objects, obj)
	}

	assert.Len(s.T(), objects, 3, "ListObjects should return 3 objects")
}

// TestPutObject_LargePayload verifies that PutObject can handle a
// ~1MB payload without error.
func (s *MinIOIntegrationSuite) TestPutObject_LargePayload() {
	bucket := s.uniqueBucket("test-large")
	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)
	defer func() {
		_ = s.client.RemoveObject(s.ctx, bucket, "large-key.bin", miniogo.RemoveObjectOptions{})
		_ = s.client.RemoveBucket(s.ctx, bucket)
	}()

	// Create a ~1MB payload.
	payload := bytes.Repeat([]byte("x"), 1024*1024)
	reader := bytes.NewReader(payload)

	_, err = s.client.PutObject(s.ctx, bucket, "large-key.bin", reader, int64(len(payload)), miniogo.PutObjectOptions{})
	require.NoError(s.T(), err, "PutObject with 1MB payload should succeed")

	// Verify the size via StatObject.
	info, err := s.client.StatObject(s.ctx, bucket, "large-key.bin", miniogo.StatObjectOptions{})
	require.NoError(s.T(), err)
	assert.Equal(s.T(), int64(1024*1024), info.Size, "stat size should match 1MB")
}

// TestPutObject_ContentType verifies that the content type is preserved
// when uploading and retrieving object metadata.
func (s *MinIOIntegrationSuite) TestPutObject_ContentType() {
	bucket := s.uniqueBucket("test-ctype")
	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)
	defer func() {
		_ = s.client.RemoveObject(s.ctx, bucket, "typed.json", miniogo.RemoveObjectOptions{})
		_ = s.client.RemoveBucket(s.ctx, bucket)
	}()

	content := `{"key": "value"}`
	reader := bytes.NewReader([]byte(content))
	_, err = s.client.PutObject(s.ctx, bucket, "typed.json", reader, int64(len(content)), miniogo.PutObjectOptions{
		ContentType: "application/json",
	})
	require.NoError(s.T(), err)

	info, err := s.client.StatObject(s.ctx, bucket, "typed.json", miniogo.StatObjectOptions{})
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "application/json", info.ContentType,
		"content type should be preserved")
}

// ===========================================================================
// Presigned URL Tests
// ===========================================================================

// TestPresignedGetObject verifies that PresignedGetObject returns a
// non-nil URL.
func (s *MinIOIntegrationSuite) TestPresignedGetObject() {
	bucket := s.uniqueBucket("test-presignget")
	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)
	defer func() {
		_ = s.client.RemoveObject(s.ctx, bucket, "presign-key.txt", miniogo.RemoveObjectOptions{})
		_ = s.client.RemoveBucket(s.ctx, bucket)
	}()

	content := "presigned content"
	reader := bytes.NewReader([]byte(content))
	_, err = s.client.PutObject(s.ctx, bucket, "presign-key.txt", reader, int64(len(content)), miniogo.PutObjectOptions{})
	require.NoError(s.T(), err)

	u, err := s.client.PresignedGetObject(s.ctx, bucket, "presign-key.txt", 5*time.Minute, nil)
	require.NoError(s.T(), err, "PresignedGetObject should succeed")
	require.NotNil(s.T(), u, "presigned URL should not be nil")
	assert.Contains(s.T(), u.String(), "presign-key.txt",
		"presigned URL should contain the object key")
}

// TestPresignedPutObject verifies that PresignedPutObject returns a
// non-nil URL.
func (s *MinIOIntegrationSuite) TestPresignedPutObject() {
	bucket := s.uniqueBucket("test-presignput")
	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)
	defer func() { _ = s.client.RemoveBucket(s.ctx, bucket) }()

	u, err := s.client.PresignedPutObject(s.ctx, bucket, "upload-key.txt", 5*time.Minute)
	require.NoError(s.T(), err, "PresignedPutObject should succeed")
	require.NotNil(s.T(), u, "presigned URL should not be nil")
	assert.Contains(s.T(), u.String(), "upload-key.txt",
		"presigned URL should contain the object key")
}

// ===========================================================================
// Error Handling Tests
// ===========================================================================

// TestGetObject_NonExistentKey verifies that attempting to read a
// non-existent object returns an error when the object is accessed.
func (s *MinIOIntegrationSuite) TestGetObject_NonExistentKey() {
	bucket := s.uniqueBucket("test-nokey")
	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)
	defer func() { _ = s.client.RemoveBucket(s.ctx, bucket) }()

	// GetObject itself may succeed (lazy evaluation), but reading should fail.
	obj, err := s.client.GetObject(s.ctx, bucket, "nonexistent-key", miniogo.GetObjectOptions{})
	if err != nil {
		// Some MinIO versions fail immediately.
		return
	}
	defer func() { _ = obj.Close() }()

	// The error surfaces when we try to read from the object.
	_, readErr := io.ReadAll(obj)
	assert.Error(s.T(), readErr, "reading non-existent object should fail")
}

// TestBucketExists_NonExistent verifies that BucketExists returns false
// (without error) for a bucket that does not exist.
func (s *MinIOIntegrationSuite) TestBucketExists_NonExistent() {
	exists, err := s.client.BucketExists(s.ctx, "definitely-does-not-exist-bucket")
	require.NoError(s.T(), err, "BucketExists for non-existent bucket should not error")
	assert.False(s.T(), exists, "non-existent bucket should return false")
}

// ===========================================================================
// Context Timeout Tests
// ===========================================================================

// TestContextTimeout_ReturnsError verifies that operations fail with
// an appropriate error when the context deadline is exceeded.
func (s *MinIOIntegrationSuite) TestContextTimeout_ReturnsError() {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Nanosecond)
	defer cancel()
	// Allow the timeout to take effect.
	time.Sleep(1 * time.Millisecond)

	_, err := s.client.BucketExists(ctx, "any-bucket")
	require.Error(s.T(), err,
		"BucketExists with expired context should return an error")
}

// ===========================================================================
// Error Code Classification Tests
// ===========================================================================

// TestErrorCode_TimeoutClassification verifies that a real operation
// timeout produces the correct sserr error classification.
func (s *MinIOIntegrationSuite) TestErrorCode_TimeoutClassification() {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := s.client.BucketExists(ctx, "any-bucket")
	require.Error(s.T(), err)

	assert.True(s.T(), sserr.IsTimeout(err),
		"expected IsTimeout()=true for deadline exceeded error")
	assert.True(s.T(), sserr.IsRetryable(err),
		"expected IsRetryable()=true for timeout error")
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestClose_IsNoOp verifies that Close does not affect subsequent
// operations. Since MinIO uses stateless HTTP, Close is a no-op and
// the client should remain functional after Close.
func (s *MinIOIntegrationSuite) TestClose_IsNoOp() {
	// Create a dedicated client for this test.
	cfg := minio.Config{
		Endpoint:  stripScheme(s.minioResult.Endpoint),
		AccessKey: s.minioResult.AccessKey,
		SecretKey: minio.Secret(s.minioResult.SecretKey),
		Region:    "us-east-1",
		UseSSL:    false,
	}
	client, err := minio.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err)

	// Verify the client works before closing.
	require.NoError(s.T(), client.Health(s.ctx),
		"Health() should succeed before Close()")

	// Close (no-op).
	client.Close()

	// After Close, the client should still work because MinIO uses
	// stateless HTTP connections.
	require.NoError(s.T(), client.Health(s.ctx),
		"Health() should still succeed after Close() (no-op)")
}

// ===========================================================================
// Concurrency Tests
// ===========================================================================

// TestConcurrentOperations verifies that the client can handle
// concurrent operations from multiple goroutines, validating that the
// client is safe for concurrent use.
func (s *MinIOIntegrationSuite) TestConcurrentOperations() {
	bucket := s.uniqueBucket("test-concurrent")
	err := s.client.MakeBucket(s.ctx, bucket, miniogo.MakeBucketOptions{})
	require.NoError(s.T(), err)
	defer func() {
		// Cleanup objects and bucket.
		for i := 0; i < 10; i++ {
			_ = s.client.RemoveObject(s.ctx, bucket, fmt.Sprintf("concurrent-%d.txt", i), miniogo.RemoveObjectOptions{})
		}
		_ = s.client.RemoveBucket(s.ctx, bucket)
	}()

	const numWorkers = 10
	var wg sync.WaitGroup
	errs := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			content := fmt.Sprintf("concurrent-content-%d", n)
			reader := strings.NewReader(content)
			_, putErr := s.client.PutObject(s.ctx, bucket, fmt.Sprintf("concurrent-%d.txt", n), reader, int64(len(content)), miniogo.PutObjectOptions{})
			if putErr != nil {
				errs <- putErr
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(s.T(), err,
			"concurrent PutObject should not produce errors")
	}

	// Verify all objects were created by listing the bucket.
	ch := s.client.ListObjects(s.ctx, bucket, miniogo.ListObjectsOptions{Recursive: true})
	var count int
	for obj := range ch {
		require.Empty(s.T(), obj.Err, "ListObjects should not produce errors")
		count++
	}
	assert.Equal(s.T(), numWorkers, count,
		"all concurrent PutObjects should succeed")
}
