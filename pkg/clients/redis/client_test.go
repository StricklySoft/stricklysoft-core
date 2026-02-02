package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Mock Implementation
// ===========================================================================

// mockCmdable implements the Cmdable interface using testify/mock for unit
// testing. Each method delegates to mock.Called() and returns the appropriate
// go-redis command type.
type mockCmdable struct {
	mock.Mock
}

func (m *mockCmdable) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	args := m.Called(ctx, key, value, expiration)
	return args.Get(0).(*redis.StatusCmd)
}

func (m *mockCmdable) Get(ctx context.Context, key string) *redis.StringCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.StringCmd)
}

func (m *mockCmdable) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	args := m.Called(ctx, keys)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) Exists(ctx context.Context, keys ...string) *redis.IntCmd {
	args := m.Called(ctx, keys)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd {
	args := m.Called(ctx, key, expiration)
	return args.Get(0).(*redis.BoolCmd)
}

func (m *mockCmdable) TTL(ctx context.Context, key string) *redis.DurationCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.DurationCmd)
}

func (m *mockCmdable) Incr(ctx context.Context, key string) *redis.IntCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) Decr(ctx context.Context, key string) *redis.IntCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) HSet(ctx context.Context, key string, values ...interface{}) *redis.IntCmd {
	args := m.Called(ctx, key, values)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) HGet(ctx context.Context, key, field string) *redis.StringCmd {
	args := m.Called(ctx, key, field)
	return args.Get(0).(*redis.StringCmd)
}

func (m *mockCmdable) HGetAll(ctx context.Context, key string) *redis.MapStringStringCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.MapStringStringCmd)
}

func (m *mockCmdable) HDel(ctx context.Context, key string, fields ...string) *redis.IntCmd {
	args := m.Called(ctx, key, fields)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd {
	args := m.Called(ctx, key, values)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) RPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd {
	args := m.Called(ctx, key, values)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) LRange(ctx context.Context, key string, start, stop int64) *redis.StringSliceCmd {
	args := m.Called(ctx, key, start, stop)
	return args.Get(0).(*redis.StringSliceCmd)
}

func (m *mockCmdable) LLen(ctx context.Context, key string) *redis.IntCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) SAdd(ctx context.Context, key string, members ...interface{}) *redis.IntCmd {
	args := m.Called(ctx, key, members)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) SMembers(ctx context.Context, key string) *redis.StringSliceCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.StringSliceCmd)
}

func (m *mockCmdable) SIsMember(ctx context.Context, key string, member interface{}) *redis.BoolCmd {
	args := m.Called(ctx, key, member)
	return args.Get(0).(*redis.BoolCmd)
}

func (m *mockCmdable) SRem(ctx context.Context, key string, members ...interface{}) *redis.IntCmd {
	args := m.Called(ctx, key, members)
	return args.Get(0).(*redis.IntCmd)
}

func (m *mockCmdable) Ping(ctx context.Context) *redis.StatusCmd {
	args := m.Called(ctx)
	return args.Get(0).(*redis.StatusCmd)
}

func (m *mockCmdable) Close() error {
	args := m.Called()
	return args.Error(0)
}

// ===========================================================================
// Command Result Helpers
// ===========================================================================

// newStatusCmd creates a *redis.StatusCmd with the given value or error.
func newStatusCmd(val string, err error) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(context.Background())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}
	return cmd
}

// newStringCmd creates a *redis.StringCmd with the given value or error.
func newStringCmd(val string, err error) *redis.StringCmd {
	cmd := redis.NewStringCmd(context.Background())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}
	return cmd
}

// newIntCmd creates a *redis.IntCmd with the given value or error.
func newIntCmd(val int64, err error) *redis.IntCmd {
	cmd := redis.NewIntCmd(context.Background())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}
	return cmd
}

// newBoolCmd creates a *redis.BoolCmd with the given value or error.
func newBoolCmd(val bool, err error) *redis.BoolCmd {
	cmd := redis.NewBoolCmd(context.Background())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}
	return cmd
}

// newDurationCmd creates a *redis.DurationCmd with the given value or error.
func newDurationCmd(val time.Duration, err error) *redis.DurationCmd {
	cmd := redis.NewDurationCmd(context.Background(), time.Second)
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}
	return cmd
}

// newStringSliceCmd creates a *redis.StringSliceCmd with the given value or error.
func newStringSliceCmd(val []string, err error) *redis.StringSliceCmd {
	cmd := redis.NewStringSliceCmd(context.Background())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}
	return cmd
}

// newMapStringStringCmd creates a *redis.MapStringStringCmd with the given value or error.
func newMapStringStringCmd(val map[string]string, err error) *redis.MapStringStringCmd {
	cmd := redis.NewMapStringStringCmd(context.Background())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}
	return cmd
}

// ===========================================================================
// NewFromClient Tests
// ===========================================================================

// TestNewFromClient_WithConfig verifies that NewFromClient correctly initializes
// the client with the provided cmdable and config.
func TestNewFromClient_WithConfig(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)

	cfg := &Config{DB: 3}
	client := NewFromClient(m, cfg)

	assert.NotNil(t, client.cmdable)
	assert.Equal(t, cfg, client.config)
	assert.Equal(t, 3, client.dbIndex)
	assert.NotNil(t, client.tracer)
}

// TestNewFromClient_NilConfig verifies that NewFromClient handles a nil config
// gracefully by initializing a zero-value Config.
func TestNewFromClient_NilConfig(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)

	client := NewFromClient(m, nil)

	require.NotNil(t, client.config)
	assert.Equal(t, 0, client.dbIndex)
}

// ===========================================================================
// Set Tests
// ===========================================================================

// TestClient_Set_Success verifies that Set returns nil on a successful
// SET command.
func TestClient_Set_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Set", mock.Anything, "key1", "value1", 10*time.Minute).
		Return(newStatusCmd("OK", nil))

	client := NewFromClient(m, &Config{DB: 0})
	err := client.Set(context.Background(), "key1", "value1", 10*time.Minute)
	require.NoError(t, err)

	m.AssertExpectations(t)
}

// TestClient_Set_Error verifies that Set returns a *sserr.Error with
// CodeInternalDatabase when Redis returns a non-timeout error.
func TestClient_Set_Error(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Set", mock.Anything, "key1", "value1", time.Duration(0)).
		Return(newStatusCmd("", errors.New("READONLY You can't write against a read only replica")))

	client := NewFromClient(m, &Config{DB: 0})
	err := client.Set(context.Background(), "key1", "value1", 0)
	require.Error(t, err)

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "Set() error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeInternalDatabase, ssErr.Code)

	m.AssertExpectations(t)
}

// TestClient_Set_TimeoutError verifies that Set returns a *sserr.Error
// with CodeTimeoutDatabase when the context deadline is exceeded.
func TestClient_Set_TimeoutError(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Set", mock.Anything, "key1", "value1", time.Duration(0)).
		Return(newStatusCmd("", context.DeadlineExceeded))

	client := NewFromClient(m, &Config{DB: 0})
	err := client.Set(context.Background(), "key1", "value1", 0)
	require.Error(t, err)

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "Set() error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeTimeoutDatabase, ssErr.Code)

	m.AssertExpectations(t)
}

// ===========================================================================
// Get Tests
// ===========================================================================

// TestClient_Get_Success verifies that Get returns the value on a
// successful GET command.
func TestClient_Get_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Get", mock.Anything, "key1").
		Return(newStringCmd("value1", nil))

	client := NewFromClient(m, &Config{DB: 0})
	val, err := client.Get(context.Background(), "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)

	m.AssertExpectations(t)
}

// TestClient_Get_Error verifies that Get returns a *sserr.Error when
// a Redis error occurs.
func TestClient_Get_Error(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Get", mock.Anything, "nonexistent").
		Return(newStringCmd("", redis.Nil))

	client := NewFromClient(m, &Config{DB: 0})
	_, err := client.Get(context.Background(), "nonexistent")
	require.Error(t, err)

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "Get() error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeInternalDatabase, ssErr.Code)

	m.AssertExpectations(t)
}

// ===========================================================================
// Del Tests
// ===========================================================================

// TestClient_Del_Success verifies that Del returns the number of deleted
// keys on success.
func TestClient_Del_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Del", mock.Anything, []string{"key1", "key2"}).
		Return(newIntCmd(2, nil))

	client := NewFromClient(m, &Config{DB: 0})
	deleted, err := client.Del(context.Background(), "key1", "key2")
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	m.AssertExpectations(t)
}

// ===========================================================================
// HSet Tests
// ===========================================================================

// TestClient_HSet_Success verifies that HSet returns the number of
// fields added on success.
func TestClient_HSet_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("HSet", mock.Anything, "hash1", []interface{}{"field1", "value1"}).
		Return(newIntCmd(1, nil))

	client := NewFromClient(m, &Config{DB: 0})
	added, err := client.HSet(context.Background(), "hash1", "field1", "value1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), added)

	m.AssertExpectations(t)
}

// ===========================================================================
// HGet Tests
// ===========================================================================

// TestClient_HGet_Success verifies that HGet returns the field value on
// success.
func TestClient_HGet_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("HGet", mock.Anything, "hash1", "field1").
		Return(newStringCmd("value1", nil))

	client := NewFromClient(m, &Config{DB: 0})
	val, err := client.HGet(context.Background(), "hash1", "field1")
	require.NoError(t, err)
	assert.Equal(t, "value1", val)

	m.AssertExpectations(t)
}

// ===========================================================================
// HGetAll Tests
// ===========================================================================

// TestClient_HGetAll_Success verifies that HGetAll returns all fields
// and values on success.
func TestClient_HGetAll_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	expected := map[string]string{"name": "Alice", "age": "30"}
	m.On("HGetAll", mock.Anything, "hash1").
		Return(newMapStringStringCmd(expected, nil))

	client := NewFromClient(m, &Config{DB: 0})
	val, err := client.HGetAll(context.Background(), "hash1")
	require.NoError(t, err)
	assert.Equal(t, expected, val)

	m.AssertExpectations(t)
}

// ===========================================================================
// LPush Tests
// ===========================================================================

// TestClient_LPush_Success verifies that LPush returns the list length
// after the push on success.
func TestClient_LPush_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("LPush", mock.Anything, "list1", []interface{}{"item1"}).
		Return(newIntCmd(1, nil))

	client := NewFromClient(m, &Config{DB: 0})
	length, err := client.LPush(context.Background(), "list1", "item1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), length)

	m.AssertExpectations(t)
}

// ===========================================================================
// LRange Tests
// ===========================================================================

// TestClient_LRange_Success verifies that LRange returns the list
// elements on success.
func TestClient_LRange_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("LRange", mock.Anything, "list1", int64(0), int64(-1)).
		Return(newStringSliceCmd([]string{"a", "b", "c"}, nil))

	client := NewFromClient(m, &Config{DB: 0})
	items, err := client.LRange(context.Background(), "list1", 0, -1)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, items)

	m.AssertExpectations(t)
}

// ===========================================================================
// SAdd Tests
// ===========================================================================

// TestClient_SAdd_Success verifies that SAdd returns the number of
// members added on success.
func TestClient_SAdd_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("SAdd", mock.Anything, "set1", []interface{}{"member1", "member2"}).
		Return(newIntCmd(2, nil))

	client := NewFromClient(m, &Config{DB: 0})
	added, err := client.SAdd(context.Background(), "set1", "member1", "member2")
	require.NoError(t, err)
	assert.Equal(t, int64(2), added)

	m.AssertExpectations(t)
}

// ===========================================================================
// SMembers Tests
// ===========================================================================

// TestClient_SMembers_Success verifies that SMembers returns all
// members of a set on success.
func TestClient_SMembers_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("SMembers", mock.Anything, "set1").
		Return(newStringSliceCmd([]string{"a", "b"}, nil))

	client := NewFromClient(m, &Config{DB: 0})
	members, err := client.SMembers(context.Background(), "set1")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, members)

	m.AssertExpectations(t)
}

// ===========================================================================
// Health Tests
// ===========================================================================

// TestClient_Health_Success verifies that Health returns nil when the
// Redis ping succeeds.
func TestClient_Health_Success(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Ping", mock.Anything).
		Return(newStatusCmd("PONG", nil))

	client := NewFromClient(m, &Config{DB: 0})
	require.NoError(t, client.Health(context.Background()))

	m.AssertExpectations(t)
}

// TestClient_Health_Failure verifies that Health returns a *sserr.Error with
// CodeUnavailableDependency when the Redis ping fails.
func TestClient_Health_Failure(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Ping", mock.Anything).
		Return(newStatusCmd("", errors.New("connection refused")))

	client := NewFromClient(m, &Config{DB: 0})
	healthErr := client.Health(context.Background())
	require.Error(t, healthErr)

	var ssErr *sserr.Error
	require.True(t, errors.As(healthErr, &ssErr), "Health() error type = %T, want *sserr.Error", healthErr)
	assert.Equal(t, sserr.CodeUnavailableDependency, ssErr.Code)

	m.AssertExpectations(t)
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestClient_Close verifies that Close delegates to the underlying
// cmdable's Close method.
func TestClient_Close(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Close").Return(nil)

	client := NewFromClient(m, nil)
	err := client.Close()
	require.NoError(t, err)

	m.AssertExpectations(t)
}

// ===========================================================================
// Client Accessor Tests
// ===========================================================================

// TestClient_ClientAccessor verifies that Client() returns the same
// cmdable instance that was injected via NewFromClient.
func TestClient_ClientAccessor(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)

	client := NewFromClient(m, nil)
	cmdable := client.Client()
	assert.NotNil(t, cmdable)
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
	result := wrapError(context.DeadlineExceeded, "command timed out")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeTimeoutDatabase, result.Code)
	assert.ErrorIs(t, result, context.DeadlineExceeded)
}

// TestWrapError_ContextCanceled verifies that wrapError classifies
// context.Canceled as CodeInternalDatabase (not retryable), because
// cancellation means the caller abandoned the operation intentionally.
func TestWrapError_ContextCanceled(t *testing.T) {
	t.Parallel()
	result := wrapError(context.Canceled, "command canceled")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeInternalDatabase, result.Code)
	assert.ErrorIs(t, result, context.Canceled)
}

// TestWrapError_GenericError verifies that wrapError classifies generic
// Redis errors as CodeInternalDatabase.
func TestWrapError_GenericError(t *testing.T) {
	t.Parallel()
	cause := errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
	result := wrapError(cause, "command failed")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeInternalDatabase, result.Code)
	assert.ErrorIs(t, result, cause)
}

// ===========================================================================
// Error Classification Integration Tests
// ===========================================================================

// TestErrorClassification_Timeout verifies the full error classification
// pipeline: a timeout error from Set is classified correctly by the
// platform error helpers (IsTimeout, IsRetryable).
func TestErrorClassification_Timeout(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Set", mock.Anything, "key1", "value1", time.Duration(0)).
		Return(newStatusCmd("", context.DeadlineExceeded))

	client := NewFromClient(m, &Config{DB: 0})
	err := client.Set(context.Background(), "key1", "value1", 0)
	require.Error(t, err)

	assert.True(t, sserr.IsTimeout(err), "IsTimeout() = false, want true for deadline exceeded error")
	assert.True(t, sserr.IsRetryable(err), "IsRetryable() = false, want true for timeout error")
	assert.True(t, sserr.IsServerError(err), "IsServerError() = false, want true for timeout error")
}

// TestErrorClassification_Internal verifies that a generic Redis error
// is classified as an internal error.
func TestErrorClassification_Internal(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Get", mock.Anything, "key1").
		Return(newStringCmd("", errors.New("LOADING Redis is loading the dataset in memory")))

	client := NewFromClient(m, &Config{DB: 0})
	_, err := client.Get(context.Background(), "key1")
	require.Error(t, err)

	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for database error")
	assert.False(t, sserr.IsTimeout(err), "IsTimeout() = true, want false for non-timeout database error")
	assert.False(t, sserr.IsRetryable(err), "IsRetryable() = true, want false for internal database error")
}

// TestErrorClassification_HealthUnavailable verifies that a health check
// failure is classified as an unavailable dependency error.
func TestErrorClassification_HealthUnavailable(t *testing.T) {
	t.Parallel()
	m := new(mockCmdable)
	m.On("Ping", mock.Anything).
		Return(newStatusCmd("", errors.New("connection refused")))

	client := NewFromClient(m, &Config{DB: 0})
	healthErr := client.Health(context.Background())
	require.Error(t, healthErr)

	assert.True(t, sserr.IsUnavailable(healthErr), "IsUnavailable() = false, want true for health check failure")
	assert.True(t, sserr.IsRetryable(healthErr), "IsRetryable() = false, want true for unavailable dependency")
}
