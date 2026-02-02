package qdrant

import (
	"context"
	"errors"
	"testing"

	pb "github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Mock VectorDB
// ===========================================================================

// mockVectorDB implements the VectorDB interface using testify/mock for
// unit testing. Each method delegates to the mock framework, allowing
// tests to set expectations and return values without a real Qdrant server.
type mockVectorDB struct {
	mock.Mock
}

func (m *mockVectorDB) CreateCollection(ctx context.Context, req *pb.CreateCollection) error {
	args := m.Called(ctx, req)
	return args.Error(0)
}

func (m *mockVectorDB) DeleteCollection(ctx context.Context, name string) error {
	args := m.Called(ctx, name)
	return args.Error(0)
}

func (m *mockVectorDB) ListCollections(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockVectorDB) GetCollectionInfo(ctx context.Context, name string) (*pb.CollectionInfo, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.CollectionInfo), args.Error(1)
}

func (m *mockVectorDB) Upsert(ctx context.Context, req *pb.UpsertPoints) (*pb.UpdateResult, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.UpdateResult), args.Error(1)
}

func (m *mockVectorDB) Query(ctx context.Context, req *pb.QueryPoints) ([]*pb.ScoredPoint, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*pb.ScoredPoint), args.Error(1)
}

func (m *mockVectorDB) Get(ctx context.Context, req *pb.GetPoints) ([]*pb.RetrievedPoint, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*pb.RetrievedPoint), args.Error(1)
}

func (m *mockVectorDB) Delete(ctx context.Context, req *pb.DeletePoints) (*pb.UpdateResult, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.UpdateResult), args.Error(1)
}

func (m *mockVectorDB) Scroll(ctx context.Context, req *pb.ScrollPoints) ([]*pb.RetrievedPoint, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*pb.RetrievedPoint), args.Error(1)
}

func (m *mockVectorDB) HealthCheck(ctx context.Context) (*pb.HealthCheckReply, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pb.HealthCheckReply), args.Error(1)
}

func (m *mockVectorDB) Close() error {
	args := m.Called()
	return args.Error(0)
}

// ===========================================================================
// NewFromVectorDB Tests
// ===========================================================================

// TestNewFromVectorDB_WithConfig verifies that NewFromVectorDB correctly
// initializes the client with the provided VectorDB and config.
func TestNewFromVectorDB_WithConfig(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	cfg := &Config{Host: "localhost", GRPCPort: 6334}
	client := NewFromVectorDB(m, cfg)

	assert.NotNil(t, client.vectorDB)
	assert.Equal(t, cfg, client.config)
	assert.NotNil(t, client.tracer)
}

// TestNewFromVectorDB_NilConfig verifies that NewFromVectorDB handles a nil
// config gracefully by initializing a zero-value Config.
func TestNewFromVectorDB_NilConfig(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	client := NewFromVectorDB(m, nil)

	require.NotNil(t, client.config)
	assert.Equal(t, "", client.config.Host)
}

// ===========================================================================
// CreateCollection Tests
// ===========================================================================

// TestClient_CreateCollection_Success verifies that CreateCollection
// delegates to the VectorDB and returns nil on success.
func TestClient_CreateCollection_Success(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	req := &pb.CreateCollection{CollectionName: "test"}
	m.On("CreateCollection", mock.Anything, req).Return(nil)

	client := NewFromVectorDB(m, nil)
	err := client.CreateCollection(context.Background(), req)
	require.NoError(t, err)

	m.AssertExpectations(t)
}

// TestClient_CreateCollection_Error verifies that CreateCollection returns
// a *sserr.Error with CodeInternalDatabase when the VectorDB returns an error.
func TestClient_CreateCollection_Error(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	req := &pb.CreateCollection{CollectionName: "test"}
	m.On("CreateCollection", mock.Anything, req).Return(errors.New("already exists"))

	client := NewFromVectorDB(m, nil)
	err := client.CreateCollection(context.Background(), req)
	require.Error(t, err)

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeInternalDatabase, ssErr.Code)

	m.AssertExpectations(t)
}

// ===========================================================================
// DeleteCollection Tests
// ===========================================================================

// TestClient_DeleteCollection_Success verifies that DeleteCollection
// delegates to the VectorDB and returns nil on success.
func TestClient_DeleteCollection_Success(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	m.On("DeleteCollection", mock.Anything, "test").Return(nil)

	client := NewFromVectorDB(m, nil)
	err := client.DeleteCollection(context.Background(), "test")
	require.NoError(t, err)

	m.AssertExpectations(t)
}

// ===========================================================================
// ListCollections Tests
// ===========================================================================

// TestClient_ListCollections_Success verifies that ListCollections returns
// the collections from the VectorDB.
func TestClient_ListCollections_Success(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	expected := []string{"col1", "col2"}
	m.On("ListCollections", mock.Anything).Return(expected, nil)

	client := NewFromVectorDB(m, nil)
	collections, err := client.ListCollections(context.Background())
	require.NoError(t, err)
	assert.Len(t, collections, 2)
	assert.Equal(t, "col1", collections[0])

	m.AssertExpectations(t)
}

// ===========================================================================
// CollectionInfo Tests
// ===========================================================================

// TestClient_CollectionInfo_Success verifies that CollectionInfo returns
// the collection info from the VectorDB.
func TestClient_CollectionInfo_Success(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	expected := &pb.CollectionInfo{}
	m.On("GetCollectionInfo", mock.Anything, "test").Return(expected, nil)

	client := NewFromVectorDB(m, nil)
	info, err := client.CollectionInfo(context.Background(), "test")
	require.NoError(t, err)
	assert.NotNil(t, info)

	m.AssertExpectations(t)
}

// ===========================================================================
// Upsert Tests
// ===========================================================================

// TestClient_Upsert_Success verifies that Upsert delegates to the VectorDB
// and returns the response on success.
func TestClient_Upsert_Success(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	req := &pb.UpsertPoints{CollectionName: "test"}
	expected := &pb.UpdateResult{}
	m.On("Upsert", mock.Anything, req).Return(expected, nil)

	client := NewFromVectorDB(m, nil)
	resp, err := client.Upsert(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)

	m.AssertExpectations(t)
}

// TestClient_Upsert_Error verifies that Upsert returns a *sserr.Error with
// CodeInternalDatabase when the VectorDB returns an error.
func TestClient_Upsert_Error(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	req := &pb.UpsertPoints{CollectionName: "test"}
	m.On("Upsert", mock.Anything, req).Return(nil, errors.New("collection not found"))

	client := NewFromVectorDB(m, nil)
	_, err := client.Upsert(context.Background(), req)
	require.Error(t, err)

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeInternalDatabase, ssErr.Code)

	m.AssertExpectations(t)
}

// ===========================================================================
// Search Tests
// ===========================================================================

// TestClient_Search_Success verifies that Search (which wraps Query on
// VectorDB) returns scored points on success.
func TestClient_Search_Success(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	req := &pb.QueryPoints{CollectionName: "test"}
	expected := []*pb.ScoredPoint{
		{Id: pb.NewIDNum(1)},
	}
	m.On("Query", mock.Anything, req).Return(expected, nil)

	client := NewFromVectorDB(m, nil)
	results, err := client.Search(context.Background(), req)
	require.NoError(t, err)
	assert.Len(t, results, 1)

	m.AssertExpectations(t)
}

// ===========================================================================
// Get Tests
// ===========================================================================

// TestClient_Get_Success verifies that Get delegates to the VectorDB's Get
// method and returns retrieved points.
func TestClient_Get_Success(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	req := &pb.GetPoints{CollectionName: "test"}
	expected := []*pb.RetrievedPoint{
		{Id: pb.NewIDNum(1)},
	}
	m.On("Get", mock.Anything, req).Return(expected, nil)

	client := NewFromVectorDB(m, nil)
	points, err := client.Get(context.Background(), req)
	require.NoError(t, err)
	assert.Len(t, points, 1)

	m.AssertExpectations(t)
}

// ===========================================================================
// Delete Tests
// ===========================================================================

// TestClient_Delete_Success verifies that Delete delegates to the VectorDB
// and returns the response on success.
func TestClient_Delete_Success(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	req := &pb.DeletePoints{CollectionName: "test"}
	expected := &pb.UpdateResult{}
	m.On("Delete", mock.Anything, req).Return(expected, nil)

	client := NewFromVectorDB(m, nil)
	resp, err := client.Delete(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, resp)

	m.AssertExpectations(t)
}

// ===========================================================================
// Scroll Tests
// ===========================================================================

// TestClient_Scroll_Success verifies that Scroll delegates to the VectorDB
// and returns retrieved points and the next page token.
func TestClient_Scroll_Success(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	req := &pb.ScrollPoints{CollectionName: "test"}
	expectedPoints := []*pb.RetrievedPoint{
		{Id: pb.NewIDNum(1)},
	}
	m.On("Scroll", mock.Anything, req).Return(expectedPoints, nil)

	client := NewFromVectorDB(m, nil)
	points, err := client.Scroll(context.Background(), req)
	require.NoError(t, err)
	assert.Len(t, points, 1)

	m.AssertExpectations(t)
}

// ===========================================================================
// Health Tests
// ===========================================================================

// TestClient_Health_Success verifies that Health returns nil when the
// VectorDB health check succeeds.
func TestClient_Health_Success(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	m.On("HealthCheck", mock.Anything).Return(&pb.HealthCheckReply{}, nil)

	client := NewFromVectorDB(m, nil)
	require.NoError(t, client.Health(context.Background()))

	m.AssertExpectations(t)
}

// TestClient_Health_Failure verifies that Health returns a *sserr.Error with
// CodeUnavailableDependency when the VectorDB health check fails.
func TestClient_Health_Failure(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	m.On("HealthCheck", mock.Anything).Return(nil, errors.New("connection refused"))

	client := NewFromVectorDB(m, nil)
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

// TestClient_Close verifies that Close delegates to the underlying VectorDB's
// Close method.
func TestClient_Close(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	m.On("Close").Return(nil)

	client := NewFromVectorDB(m, nil)
	err := client.Close()
	require.NoError(t, err)

	m.AssertExpectations(t)
}

// ===========================================================================
// VectorDB Accessor Tests
// ===========================================================================

// TestClient_VectorDBAccessor verifies that VectorDB() returns the same
// VectorDB instance that was injected via NewFromVectorDB.
func TestClient_VectorDBAccessor(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	client := NewFromVectorDB(m, nil)
	vdb := client.VectorDB()
	assert.NotNil(t, vdb)
	// Verify it is the same instance.
	assert.Equal(t, m, vdb)
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
	cause := errors.New("collection not found")
	result := wrapError(cause, "operation failed")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeInternalDatabase, result.Code)
	assert.ErrorIs(t, result, cause)
}

// ===========================================================================
// Error Classification Integration Tests
// ===========================================================================

// TestErrorClassification_SearchTimeout verifies the full error classification
// pipeline: a timeout error from Search is classified correctly by the
// platform error helpers (IsTimeout, IsRetryable).
func TestErrorClassification_SearchTimeout(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	req := &pb.QueryPoints{CollectionName: "test"}
	m.On("Query", mock.Anything, req).Return(nil, context.DeadlineExceeded)

	client := NewFromVectorDB(m, nil)
	_, searchErr := client.Search(context.Background(), req)
	require.Error(t, searchErr)

	assert.True(t, sserr.IsTimeout(searchErr), "IsTimeout() = false, want true for deadline exceeded error")
	assert.True(t, sserr.IsRetryable(searchErr), "IsRetryable() = false, want true for timeout error")
	assert.True(t, sserr.IsServerError(searchErr), "IsServerError() = false, want true for timeout error")

	m.AssertExpectations(t)
}

// TestErrorClassification_UpsertInternalDatabase verifies that a generic
// database error from Upsert is classified as an internal error.
func TestErrorClassification_UpsertInternalDatabase(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	req := &pb.UpsertPoints{CollectionName: "test"}
	m.On("Upsert", mock.Anything, req).Return(nil, errors.New("disk full"))

	client := NewFromVectorDB(m, nil)
	_, upsertErr := client.Upsert(context.Background(), req)
	require.Error(t, upsertErr)

	assert.True(t, sserr.IsInternal(upsertErr), "IsInternal() = false, want true for database error")
	assert.False(t, sserr.IsTimeout(upsertErr), "IsTimeout() = true, want false for non-timeout database error")
	assert.False(t, sserr.IsRetryable(upsertErr), "IsRetryable() = true, want false for internal database error")

	m.AssertExpectations(t)
}

// TestErrorClassification_HealthUnavailable verifies that a health check
// failure is classified as an unavailable dependency error.
func TestErrorClassification_HealthUnavailable(t *testing.T) {
	t.Parallel()
	m := &mockVectorDB{}
	m.On("HealthCheck", mock.Anything).Return(nil, errors.New("connection refused"))

	client := NewFromVectorDB(m, nil)
	healthErr := client.Health(context.Background())
	require.Error(t, healthErr)

	assert.True(t, sserr.IsUnavailable(healthErr), "IsUnavailable() = false, want true for health check failure")
	assert.True(t, sserr.IsRetryable(healthErr), "IsRetryable() = false, want true for unavailable dependency")

	m.AssertExpectations(t)
}
