//go:build integration

// Package redis_test contains integration tests for the Redis client that
// require a running Redis instance via testcontainers-go. These tests are
// gated behind the "integration" build tag and are executed in CI with Docker.
//
// Run locally with:
//
//	go test -v -race -tags=integration ./pkg/clients/redis/...
//
// Or via Makefile:
//
//	make test-integration
//
// # Architecture
//
// All tests run within a single [suite.Suite] that starts one Redis
// container in [SetupSuite] and terminates it in [TearDownSuite]. Test
// isolation is achieved via unique key prefixes per test method rather than
// per-test containers, which reduces total execution time.
package redis_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/StricklySoft/stricklysoft-core/internal/testutil/containers"
	"github.com/StricklySoft/stricklysoft-core/pkg/clients/redis"
	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Suite Definition
// ===========================================================================

// RedisIntegrationSuite runs all Redis integration tests against a single
// shared container. The container is started once in SetupSuite and
// terminated in TearDownSuite. All test methods share the same client,
// using unique key prefixes for isolation.
type RedisIntegrationSuite struct {
	suite.Suite

	// ctx is the background context used for container and client
	// lifecycle operations.
	ctx context.Context

	// redisResult holds the started Redis container and connection
	// string. It is set in SetupSuite and used to terminate the
	// container in TearDownSuite.
	redisResult *containers.RedisResult

	// client is the SDK Redis client connected to the test container.
	// All test methods use this client unless they need to test client
	// creation or close behavior.
	client *redis.Client

	// connString is the Redis connection URI for the test container.
	// Tests that need to create additional clients use this to connect
	// to the same instance.
	connString string
}

// SetupSuite starts a single Redis container and creates a client shared
// across all tests in the suite. This runs once before any test method
// executes.
func (s *RedisIntegrationSuite) SetupSuite() {
	s.ctx = context.Background()

	result, err := containers.StartRedis(s.ctx)
	require.NoError(s.T(), err, "failed to start Redis container")
	s.redisResult = result
	s.connString = result.ConnString

	cfg := redis.Config{
		URI:      result.ConnString,
		PoolSize: 10,
	}
	require.NoError(s.T(), cfg.Validate(), "failed to validate config")

	client, err := redis.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err, "failed to create Redis client")
	s.client = client
}

// TearDownSuite closes the client and terminates the container. This
// runs once after all test methods have completed.
func (s *RedisIntegrationSuite) TearDownSuite() {
	if s.client != nil {
		_ = s.client.Close()
	}
	if s.redisResult != nil {
		if err := s.redisResult.Container.Terminate(s.ctx); err != nil {
			s.T().Logf("failed to terminate redis container: %v", err)
		}
	}
}

// TestRedisIntegration is the top-level entry point that runs all suite
// tests. It is skipped in short mode (-short flag) to allow fast unit
// test runs without Docker.
func TestRedisIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	suite.Run(t, new(RedisIntegrationSuite))
}

// ===========================================================================
// Connection Tests
// ===========================================================================

// TestNewClient_ConnectsSuccessfully verifies that NewClient can
// establish a connection to a real Redis instance and that the returned
// client is functional.
func (s *RedisIntegrationSuite) TestNewClient_ConnectsSuccessfully() {
	require.NotNil(s.T(), s.client, "suite client should not be nil")
}

// TestHealth_ReturnsNil verifies that Health returns nil when Redis
// is reachable and responding to pings.
func (s *RedisIntegrationSuite) TestHealth_ReturnsNil() {
	err := s.client.Health(s.ctx)
	require.NoError(s.T(), err, "Health() should succeed when Redis is reachable")
}

// ===========================================================================
// String Operation Tests
// ===========================================================================

// TestSet_And_Get verifies that Set stores a value and Get retrieves it.
func (s *RedisIntegrationSuite) TestSet_And_Get() {
	key := "test:set_get:key1"
	err := s.client.Set(s.ctx, key, "hello", 10*time.Minute)
	require.NoError(s.T(), err, "Set should succeed")

	val, err := s.client.Get(s.ctx, key)
	require.NoError(s.T(), err, "Get should succeed")
	assert.Equal(s.T(), "hello", val)
}

// TestGet_NonExistentKey verifies that Get returns an error for a key
// that does not exist. The error should be wrapped as a platform error.
func (s *RedisIntegrationSuite) TestGet_NonExistentKey() {
	_, err := s.client.Get(s.ctx, "test:get_nonexistent:missing")
	require.Error(s.T(), err, "Get on nonexistent key should return an error")

	var ssErr *sserr.Error
	assert.True(s.T(), sserr.IsInternal(err),
		"nonexistent key error should be classified as internal")
	// Verify it wraps to our error type.
	require.True(s.T(), errors.As(err, &ssErr))
}

// TestDel_RemovesKey verifies that Del removes a key and returns the
// number of keys removed.
func (s *RedisIntegrationSuite) TestDel_RemovesKey() {
	key := "test:del:key1"
	err := s.client.Set(s.ctx, key, "temp", 10*time.Minute)
	require.NoError(s.T(), err)

	deleted, err := s.client.Del(s.ctx, key)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), int64(1), deleted)

	// Verify the key is gone.
	_, err = s.client.Get(s.ctx, key)
	require.Error(s.T(), err, "Get after Del should fail")
}

// TestExists_ReturnsCount verifies that Exists returns the correct
// count of existing keys.
func (s *RedisIntegrationSuite) TestExists_ReturnsCount() {
	key1 := "test:exists:key1"
	key2 := "test:exists:key2"
	err := s.client.Set(s.ctx, key1, "a", 10*time.Minute)
	require.NoError(s.T(), err)
	err = s.client.Set(s.ctx, key2, "b", 10*time.Minute)
	require.NoError(s.T(), err)

	count, err := s.client.Exists(s.ctx, key1, key2, "test:exists:nonexistent")
	require.NoError(s.T(), err)
	assert.Equal(s.T(), int64(2), count)
}

// TestExpire_And_TTL verifies that Expire sets a TTL and TTL retrieves
// a positive duration.
func (s *RedisIntegrationSuite) TestExpire_And_TTL() {
	key := "test:expire:key1"
	err := s.client.Set(s.ctx, key, "value", 0)
	require.NoError(s.T(), err)

	ok, err := s.client.Expire(s.ctx, key, 30*time.Second)
	require.NoError(s.T(), err)
	assert.True(s.T(), ok, "Expire should return true for existing key")

	ttl, err := s.client.TTL(s.ctx, key)
	require.NoError(s.T(), err)
	assert.True(s.T(), ttl > 0, "TTL should be positive, got %v", ttl)
	assert.True(s.T(), ttl <= 30*time.Second, "TTL should be <= 30s, got %v", ttl)
}

// TestIncr_And_Decr verifies that Incr and Decr correctly modify
// integer values.
func (s *RedisIntegrationSuite) TestIncr_And_Decr() {
	key := "test:incr_decr:counter"
	err := s.client.Set(s.ctx, key, "10", 10*time.Minute)
	require.NoError(s.T(), err)

	val, err := s.client.Incr(s.ctx, key)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), int64(11), val)

	val, err = s.client.Decr(s.ctx, key)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), int64(10), val)
}

// ===========================================================================
// Hash Operation Tests
// ===========================================================================

// TestHSet_And_HGet verifies that HSet stores hash fields and HGet
// retrieves them.
func (s *RedisIntegrationSuite) TestHSet_And_HGet() {
	key := "test:hash:user1"
	_, err := s.client.HSet(s.ctx, key, "name", "Alice", "age", "30")
	require.NoError(s.T(), err)

	name, err := s.client.HGet(s.ctx, key, "name")
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "Alice", name)

	age, err := s.client.HGet(s.ctx, key, "age")
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "30", age)
}

// TestHGetAll verifies that HGetAll returns all fields and values.
func (s *RedisIntegrationSuite) TestHGetAll() {
	key := "test:hash:user2"
	_, err := s.client.HSet(s.ctx, key, "name", "Bob", "role", "admin")
	require.NoError(s.T(), err)

	fields, err := s.client.HGetAll(s.ctx, key)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "Bob", fields["name"])
	assert.Equal(s.T(), "admin", fields["role"])
	assert.Len(s.T(), fields, 2)
}

// TestHDel verifies that HDel removes a field from a hash.
func (s *RedisIntegrationSuite) TestHDel() {
	key := "test:hash:user3"
	_, err := s.client.HSet(s.ctx, key, "name", "Charlie", "temp", "yes")
	require.NoError(s.T(), err)

	removed, err := s.client.HDel(s.ctx, key, "temp")
	require.NoError(s.T(), err)
	assert.Equal(s.T(), int64(1), removed)

	fields, err := s.client.HGetAll(s.ctx, key)
	require.NoError(s.T(), err)
	assert.Len(s.T(), fields, 1)
	assert.Equal(s.T(), "Charlie", fields["name"])
}

// ===========================================================================
// List Operation Tests
// ===========================================================================

// TestLPush_And_LRange verifies that LPush prepends values and LRange
// retrieves them.
func (s *RedisIntegrationSuite) TestLPush_And_LRange() {
	key := "test:list:queue1"
	_, err := s.client.LPush(s.ctx, key, "c", "b", "a")
	require.NoError(s.T(), err)

	items, err := s.client.LRange(s.ctx, key, 0, -1)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), []string{"a", "b", "c"}, items)
}

// TestRPush_And_LLen verifies that RPush appends values and LLen
// returns the correct length.
func (s *RedisIntegrationSuite) TestRPush_And_LLen() {
	key := "test:list:queue2"
	_, err := s.client.RPush(s.ctx, key, "x", "y", "z")
	require.NoError(s.T(), err)

	length, err := s.client.LLen(s.ctx, key)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), int64(3), length)
}

// ===========================================================================
// Set Operation Tests
// ===========================================================================

// TestSAdd_And_SMembers verifies that SAdd adds members and SMembers
// retrieves them.
func (s *RedisIntegrationSuite) TestSAdd_And_SMembers() {
	key := "test:set:tags1"
	_, err := s.client.SAdd(s.ctx, key, "go", "redis", "docker")
	require.NoError(s.T(), err)

	members, err := s.client.SMembers(s.ctx, key)
	require.NoError(s.T(), err)
	assert.Len(s.T(), members, 3)
	assert.ElementsMatch(s.T(), []string{"go", "redis", "docker"}, members)
}

// TestSIsMember verifies that SIsMember correctly identifies members
// and non-members.
func (s *RedisIntegrationSuite) TestSIsMember() {
	key := "test:set:tags2"
	_, err := s.client.SAdd(s.ctx, key, "go", "redis")
	require.NoError(s.T(), err)

	isMember, err := s.client.SIsMember(s.ctx, key, "go")
	require.NoError(s.T(), err)
	assert.True(s.T(), isMember, "go should be a member")

	isMember, err = s.client.SIsMember(s.ctx, key, "python")
	require.NoError(s.T(), err)
	assert.False(s.T(), isMember, "python should not be a member")
}

// TestSRem verifies that SRem removes members from a set.
func (s *RedisIntegrationSuite) TestSRem() {
	key := "test:set:tags3"
	_, err := s.client.SAdd(s.ctx, key, "a", "b", "c")
	require.NoError(s.T(), err)

	removed, err := s.client.SRem(s.ctx, key, "b")
	require.NoError(s.T(), err)
	assert.Equal(s.T(), int64(1), removed)

	members, err := s.client.SMembers(s.ctx, key)
	require.NoError(s.T(), err)
	assert.ElementsMatch(s.T(), []string{"a", "c"}, members)
}

// ===========================================================================
// Context Timeout Tests
// ===========================================================================

// TestContextTimeout_ReturnsError verifies that operations fail with
// an appropriate error when the context deadline is exceeded.
func (s *RedisIntegrationSuite) TestContextTimeout_ReturnsError() {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Nanosecond)
	defer cancel()
	// Allow the timeout to take effect.
	time.Sleep(1 * time.Millisecond)

	err := s.client.Set(ctx, "test:timeout:key1", "value", 0)
	require.Error(s.T(), err,
		"Set with expired context should return an error")
}

// ===========================================================================
// Error Code Classification Tests
// ===========================================================================

// TestErrorCode_TimeoutClassification verifies that a real command
// timeout produces the correct sserr error classification.
func (s *RedisIntegrationSuite) TestErrorCode_TimeoutClassification() {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	err := s.client.Set(ctx, "test:timeout_class:key1", "value", 0)
	require.Error(s.T(), err)

	assert.True(s.T(), sserr.IsTimeout(err),
		"expected IsTimeout()=true for deadline exceeded error")
	assert.True(s.T(), sserr.IsRetryable(err),
		"expected IsRetryable()=true for timeout error")
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestClose_ReleasesResources verifies that after Close is called,
// further operations fail. This test creates its own client so it can
// close it without affecting other tests in the suite.
func (s *RedisIntegrationSuite) TestClose_ReleasesResources() {
	cfg := redis.Config{
		URI:      s.connString,
		PoolSize: 5,
	}
	require.NoError(s.T(), cfg.Validate())

	client, err := redis.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err)

	// Verify the client works before closing.
	require.NoError(s.T(), client.Health(s.ctx),
		"Health() should succeed before Close()")

	err = client.Close()
	require.NoError(s.T(), err)

	// After Close, Health should fail because the connection is closed.
	assert.Error(s.T(), client.Health(s.ctx),
		"Health() should fail after Close()")
}

// ===========================================================================
// Concurrency Tests
// ===========================================================================

// TestConcurrentOperations verifies that the client can handle
// concurrent operations from multiple goroutines, validating that the
// connection pool and client are safe for concurrent use.
func (s *RedisIntegrationSuite) TestConcurrentOperations() {
	const numWorkers = 10
	var wg sync.WaitGroup
	errs := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("test:concurrent:key%d", n)
			if setErr := s.client.Set(s.ctx, key, fmt.Sprintf("val%d", n), 10*time.Minute); setErr != nil {
				errs <- setErr
				return
			}
			if _, getErr := s.client.Get(s.ctx, key); getErr != nil {
				errs <- getErr
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(s.T(), err,
			"concurrent operation should not produce errors")
	}
}

// ===========================================================================
// Client Accessor Tests
// ===========================================================================

// TestClientAccessor verifies that client.Client() returns a functional
// Cmdable that can execute operations directly, bypassing the client's
// tracing and error wrapping layer.
func (s *RedisIntegrationSuite) TestClientAccessor() {
	cmdable := s.client.Client()
	require.NotNil(s.T(), cmdable, "Client() should return non-nil")

	// Use the cmdable directly to ping Redis.
	err := cmdable.Ping(s.ctx).Err()
	require.NoError(s.T(), err, "direct cmdable Ping should succeed")
}
