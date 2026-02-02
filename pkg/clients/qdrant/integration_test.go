//go:build integration

// Package qdrant_test contains integration tests for the Qdrant client
// that require a running Qdrant instance via testcontainers-go. These
// tests are gated behind the "integration" build tag and are executed in CI
// with Docker.
//
// Run locally with:
//
//	go test -v -race -tags=integration ./pkg/clients/qdrant/...
//
// Or via Makefile:
//
//	make test-integration
//
// # Architecture
//
// All tests run within a single [suite.Suite] that starts one Qdrant
// container in [SetupSuite] and terminates it in [TearDownSuite]. Test
// isolation is achieved via unique collection names per test method rather
// than per-test containers, which reduces total execution time.
//
// # Collection Naming Convention
//
// Each test method creates collections with a unique prefix derived from
// its test category (e.g., test_create, test_upsert, test_search).
// Collections are not dropped between tests because the entire database
// is destroyed when the container terminates.
package qdrant_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	pb "github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/StricklySoft/stricklysoft-core/internal/testutil/containers"
	"github.com/StricklySoft/stricklysoft-core/pkg/clients/qdrant"
	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Suite Definition
// ===========================================================================

// QdrantIntegrationSuite runs all Qdrant integration tests against a single
// shared container. The container is started once in SetupSuite and
// terminated in TearDownSuite. All test methods share the same client,
// using unique collection names for isolation.
type QdrantIntegrationSuite struct {
	suite.Suite

	// ctx is the background context used for container and client
	// lifecycle operations.
	ctx context.Context

	// qdrantResult holds the started Qdrant container and endpoints.
	// It is set in SetupSuite and used to terminate the container in
	// TearDownSuite.
	qdrantResult *containers.QdrantResult

	// client is the SDK Qdrant client connected to the test container.
	// All test methods use this client unless they need to test client
	// creation or close behavior.
	client *qdrant.Client
}

// SetupSuite starts a single Qdrant container and creates a client shared
// across all tests in the suite. This runs once before any test method
// executes.
func (s *QdrantIntegrationSuite) SetupSuite() {
	s.ctx = context.Background()

	result, err := containers.StartQdrant(s.ctx)
	require.NoError(s.T(), err, "failed to start Qdrant container")
	s.qdrantResult = result

	cfg := qdrant.Config{
		Host:     result.GRPCEndpoint,
		GRPCPort: 0, // will be parsed from endpoint
	}

	// Parse host and port from the GRPCEndpoint (e.g., "localhost:55682").
	var host string
	var port int
	_, parseErr := fmt.Sscanf(result.GRPCEndpoint, "%s", &host)
	if parseErr != nil {
		// Fallback: try to split manually.
		host = result.GRPCEndpoint
	}
	// The GRPCEndpoint from testcontainers is "host:port" format.
	// We need to split it properly.
	for i := len(result.GRPCEndpoint) - 1; i >= 0; i-- {
		if result.GRPCEndpoint[i] == ':' {
			host = result.GRPCEndpoint[:i]
			_, _ = fmt.Sscanf(result.GRPCEndpoint[i+1:], "%d", &port)
			break
		}
	}

	cfg.Host = host
	cfg.GRPCPort = port

	client, err := qdrant.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err, "failed to create Qdrant client")
	s.client = client
}

// TearDownSuite closes the client and terminates the container. This
// runs once after all test methods have completed.
func (s *QdrantIntegrationSuite) TearDownSuite() {
	if s.client != nil {
		_ = s.client.Close()
	}
	if s.qdrantResult != nil {
		if err := s.qdrantResult.Container.Terminate(s.ctx); err != nil {
			s.T().Logf("failed to terminate qdrant container: %v", err)
		}
	}
}

// TestQdrantIntegration is the top-level entry point that runs all suite
// tests. It is skipped in short mode (-short flag) to allow fast unit
// test runs without Docker.
func TestQdrantIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	suite.Run(t, new(QdrantIntegrationSuite))
}

// ===========================================================================
// Connection Tests
// ===========================================================================

// TestNewClient_ConnectsSuccessfully verifies that NewClient can
// establish a connection to a real Qdrant instance and that the
// returned client is functional.
func (s *QdrantIntegrationSuite) TestNewClient_ConnectsSuccessfully() {
	require.NotNil(s.T(), s.client, "suite client should not be nil")
}

// TestHealth_ReturnsNil verifies that Health returns nil when the
// Qdrant server is reachable and responding to health checks.
func (s *QdrantIntegrationSuite) TestHealth_ReturnsNil() {
	err := s.client.Health(s.ctx)
	require.NoError(s.T(), err, "Health() should succeed when Qdrant is reachable")
}

// ===========================================================================
// Collection Tests
// ===========================================================================

// TestCreateCollection_And_CollectionInfo verifies that CreateCollection
// creates a collection and CollectionInfo retrieves its information.
func (s *QdrantIntegrationSuite) TestCreateCollection_And_CollectionInfo() {
	collectionName := "test_create_info"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err, "CreateCollection should succeed")

	info, err := s.client.CollectionInfo(s.ctx, collectionName)
	require.NoError(s.T(), err, "CollectionInfo should succeed")
	assert.NotNil(s.T(), info, "CollectionInfo should return non-nil info")
}

// TestDeleteCollection verifies that DeleteCollection removes a collection.
func (s *QdrantIntegrationSuite) TestDeleteCollection() {
	collectionName := "test_delete_col"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	err = s.client.DeleteCollection(s.ctx, collectionName)
	require.NoError(s.T(), err, "DeleteCollection should succeed")

	// Verify the collection no longer exists by checking CollectionInfo fails.
	_, err = s.client.CollectionInfo(s.ctx, collectionName)
	assert.Error(s.T(), err, "CollectionInfo should fail after DeleteCollection")
}

// TestListCollections verifies that ListCollections returns created
// collections.
func (s *QdrantIntegrationSuite) TestListCollections() {
	collectionName := "test_list_col"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	collections, err := s.client.ListCollections(s.ctx)
	require.NoError(s.T(), err)

	var found bool
	for _, col := range collections {
		if col == collectionName {
			found = true
			break
		}
	}
	assert.True(s.T(), found,
		"ListCollections should include %q", collectionName)
}

// ===========================================================================
// Point Operation Tests
// ===========================================================================

// TestUpsert_Points verifies that Upsert inserts vectors with payload
// into a collection.
func (s *QdrantIntegrationSuite) TestUpsert_Points() {
	collectionName := "test_upsert_points"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	resp, err := s.client.Upsert(s.ctx, &pb.UpsertPoints{
		CollectionName: collectionName,
		Points: []*pb.PointStruct{
			{
				Id:      pb.NewIDNum(1),
				Vectors: pb.NewVectors(0.1, 0.2, 0.3, 0.4),
				Payload: pb.NewValueMap(map[string]any{"name": "alpha"}),
			},
			{
				Id:      pb.NewIDNum(2),
				Vectors: pb.NewVectors(0.5, 0.6, 0.7, 0.8),
				Payload: pb.NewValueMap(map[string]any{"name": "beta"}),
			},
			{
				Id:      pb.NewIDNum(3),
				Vectors: pb.NewVectors(0.9, 0.8, 0.7, 0.6),
				Payload: pb.NewValueMap(map[string]any{"name": "gamma"}),
			},
		},
	})
	require.NoError(s.T(), err, "Upsert should succeed")
	assert.NotNil(s.T(), resp, "Upsert should return a response")
}

// TestSearch_NearestNeighbors verifies that Search returns the nearest
// neighbors for a query vector.
func (s *QdrantIntegrationSuite) TestSearch_NearestNeighbors() {
	collectionName := "test_search_nn"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	_, err = s.client.Upsert(s.ctx, &pb.UpsertPoints{
		CollectionName: collectionName,
		Points: []*pb.PointStruct{
			{
				Id:      pb.NewIDNum(1),
				Vectors: pb.NewVectors(1.0, 0.0, 0.0, 0.0),
				Payload: pb.NewValueMap(map[string]any{"name": "first"}),
			},
			{
				Id:      pb.NewIDNum(2),
				Vectors: pb.NewVectors(0.0, 1.0, 0.0, 0.0),
				Payload: pb.NewValueMap(map[string]any{"name": "second"}),
			},
			{
				Id:      pb.NewIDNum(3),
				Vectors: pb.NewVectors(0.0, 0.0, 1.0, 0.0),
				Payload: pb.NewValueMap(map[string]any{"name": "third"}),
			},
		},
	})
	require.NoError(s.T(), err)

	// Qdrant indexes vectors asynchronously after Upsert returns. A brief
	// wait is required before search operations to allow the HNSW index to
	// incorporate the newly upserted points. Without this, searches may
	// return stale or empty results.
	time.Sleep(500 * time.Millisecond)

	limit := uint64(3)
	results, err := s.client.Search(s.ctx, &pb.QueryPoints{
		CollectionName: collectionName,
		Query:          pb.NewQuery(0.9, 0.1, 0.0, 0.0),
		Limit:          &limit,
	})
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), results, "Search should return results")

	// The first result should be the vector most similar to (0.9, 0.1, 0, 0),
	// which is (1, 0, 0, 0) with ID 1.
	assert.Equal(s.T(), uint64(1), results[0].GetId().GetNum(),
		"top result should be point ID 1")
}

// TestGet_ByID verifies that Get retrieves a point by its ID.
func (s *QdrantIntegrationSuite) TestGet_ByID() {
	collectionName := "test_get_byid"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	_, err = s.client.Upsert(s.ctx, &pb.UpsertPoints{
		CollectionName: collectionName,
		Points: []*pb.PointStruct{
			{
				Id:      pb.NewIDNum(42),
				Vectors: pb.NewVectors(0.1, 0.2, 0.3, 0.4),
				Payload: pb.NewValueMap(map[string]any{"name": "answer"}),
			},
		},
	})
	require.NoError(s.T(), err)

	withPayload := true
	points, err := s.client.Get(s.ctx, &pb.GetPoints{
		CollectionName: collectionName,
		Ids:            []*pb.PointId{pb.NewIDNum(42)},
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: withPayload}},
	})
	require.NoError(s.T(), err)
	require.Len(s.T(), points, 1, "Get should return exactly 1 point")
	assert.Equal(s.T(), uint64(42), points[0].GetId().GetNum())
}

// TestDelete_Points verifies that Delete removes points from a collection.
func (s *QdrantIntegrationSuite) TestDelete_Points() {
	collectionName := "test_delete_pts"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	_, err = s.client.Upsert(s.ctx, &pb.UpsertPoints{
		CollectionName: collectionName,
		Points: []*pb.PointStruct{
			{
				Id:      pb.NewIDNum(1),
				Vectors: pb.NewVectors(0.1, 0.2, 0.3, 0.4),
			},
			{
				Id:      pb.NewIDNum(2),
				Vectors: pb.NewVectors(0.5, 0.6, 0.7, 0.8),
			},
		},
	})
	require.NoError(s.T(), err)

	// Delete point 1.
	_, err = s.client.Delete(s.ctx, &pb.DeletePoints{
		CollectionName: collectionName,
		Points: &pb.PointsSelector{
			PointsSelectorOneOf: &pb.PointsSelector_Points{
				Points: &pb.PointsIdsList{
					Ids: []*pb.PointId{pb.NewIDNum(1)},
				},
			},
		},
	})
	require.NoError(s.T(), err, "Delete should succeed")

	// Verify point 1 is gone.
	points, err := s.client.Get(s.ctx, &pb.GetPoints{
		CollectionName: collectionName,
		Ids:            []*pb.PointId{pb.NewIDNum(1)},
	})
	require.NoError(s.T(), err)
	assert.Empty(s.T(), points, "deleted point should not be returned by Get")
}

// TestScroll_Pagination verifies that Scroll supports pagination through
// points in a collection.
func (s *QdrantIntegrationSuite) TestScroll_Pagination() {
	collectionName := "test_scroll_page"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	// Upsert 10 points.
	var points []*pb.PointStruct
	for i := uint64(1); i <= 10; i++ {
		points = append(points, &pb.PointStruct{
			Id:      pb.NewIDNum(i),
			Vectors: pb.NewVectors(float32(i)*0.1, float32(i)*0.2, float32(i)*0.3, float32(i)*0.4),
		})
	}
	_, err = s.client.Upsert(s.ctx, &pb.UpsertPoints{
		CollectionName: collectionName,
		Points:         points,
	})
	require.NoError(s.T(), err)

	// Scroll with limit 5 - first page.
	limit := uint32(5)
	firstPage, err := s.client.Scroll(s.ctx, &pb.ScrollPoints{
		CollectionName: collectionName,
		Limit:          &limit,
	})
	require.NoError(s.T(), err)
	assert.Len(s.T(), firstPage, 5, "first page should have 5 points")

	// Scroll with limit 100 - all points.
	allLimit := uint32(100)
	allPoints, err := s.client.Scroll(s.ctx, &pb.ScrollPoints{
		CollectionName: collectionName,
		Limit:          &allLimit,
	})
	require.NoError(s.T(), err)
	assert.Len(s.T(), allPoints, 10, "scroll should return all 10 points")
}

// ===========================================================================
// Payload Tests
// ===========================================================================

// TestUpsert_WithPayload verifies that Upsert correctly stores payload
// fields of various types (string, int, float, bool).
func (s *QdrantIntegrationSuite) TestUpsert_WithPayload() {
	collectionName := "test_upsert_payload"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	_, err = s.client.Upsert(s.ctx, &pb.UpsertPoints{
		CollectionName: collectionName,
		Points: []*pb.PointStruct{
			{
				Id:      pb.NewIDNum(1),
				Vectors: pb.NewVectors(0.1, 0.2, 0.3, 0.4),
				Payload: pb.NewValueMap(map[string]any{
					"str_field":   "hello",
					"int_field":   42,
					"float_field": 3.14,
					"bool_field":  true,
				}),
			},
		},
	})
	require.NoError(s.T(), err)

	withPayload := true
	points, err := s.client.Get(s.ctx, &pb.GetPoints{
		CollectionName: collectionName,
		Ids:            []*pb.PointId{pb.NewIDNum(1)},
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: withPayload}},
	})
	require.NoError(s.T(), err)
	require.Len(s.T(), points, 1)

	payload := points[0].GetPayload()
	require.NotNil(s.T(), payload)

	// Verify string field.
	strVal := payload["str_field"]
	require.NotNil(s.T(), strVal)
	assert.Equal(s.T(), "hello", strVal.GetStringValue())

	// Verify integer field (stored as integer in Qdrant).
	intVal := payload["int_field"]
	require.NotNil(s.T(), intVal)
	assert.Equal(s.T(), int64(42), intVal.GetIntegerValue())

	// Verify float field.
	floatVal := payload["float_field"]
	require.NotNil(s.T(), floatVal)
	assert.InDelta(s.T(), 3.14, floatVal.GetDoubleValue(), 0.001)

	// Verify boolean field.
	boolVal := payload["bool_field"]
	require.NotNil(s.T(), boolVal)
	assert.True(s.T(), boolVal.GetBoolValue())
}

// TestSearch_WithFilter verifies that Search can filter results by payload
// field values.
func (s *QdrantIntegrationSuite) TestSearch_WithFilter() {
	collectionName := "test_search_filter"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	_, err = s.client.Upsert(s.ctx, &pb.UpsertPoints{
		CollectionName: collectionName,
		Points: []*pb.PointStruct{
			{
				Id:      pb.NewIDNum(1),
				Vectors: pb.NewVectors(1.0, 0.0, 0.0, 0.0),
				Payload: pb.NewValueMap(map[string]any{"category": "a"}),
			},
			{
				Id:      pb.NewIDNum(2),
				Vectors: pb.NewVectors(0.9, 0.1, 0.0, 0.0),
				Payload: pb.NewValueMap(map[string]any{"category": "b"}),
			},
			{
				Id:      pb.NewIDNum(3),
				Vectors: pb.NewVectors(0.8, 0.2, 0.0, 0.0),
				Payload: pb.NewValueMap(map[string]any{"category": "a"}),
			},
		},
	})
	require.NoError(s.T(), err)

	// Qdrant indexes vectors asynchronously after Upsert returns. A brief
	// wait is required before search operations to allow the HNSW index to
	// incorporate the newly upserted points. Without this, searches may
	// return stale or empty results.
	time.Sleep(500 * time.Millisecond)

	limit := uint64(10)
	results, err := s.client.Search(s.ctx, &pb.QueryPoints{
		CollectionName: collectionName,
		Query:          pb.NewQuery(1.0, 0.0, 0.0, 0.0),
		Limit:          &limit,
		Filter: &pb.Filter{
			Must: []*pb.Condition{
				{
					ConditionOneOf: &pb.Condition_Field{
						Field: &pb.FieldCondition{
							Key: "category",
							Match: &pb.Match{
								MatchValue: &pb.Match_Keyword{
									Keyword: "a",
								},
							},
						},
					},
				},
			},
		},
	})
	require.NoError(s.T(), err)

	// Only points with category "a" should be returned (IDs 1 and 3).
	assert.Len(s.T(), results, 2, "filtered search should return only matching points")
	for _, result := range results {
		id := result.GetId().GetNum()
		assert.True(s.T(), id == 1 || id == 3,
			"result ID %d should be 1 or 3 (category=a)", id)
	}
}

// TestSearch_EmptyCollection verifies that Search returns an empty result
// set when the collection has no points.
func (s *QdrantIntegrationSuite) TestSearch_EmptyCollection() {
	collectionName := "test_search_empty"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	limit := uint64(10)
	results, err := s.client.Search(s.ctx, &pb.QueryPoints{
		CollectionName: collectionName,
		Query:          pb.NewQuery(0.1, 0.2, 0.3, 0.4),
		Limit:          &limit,
	})
	require.NoError(s.T(), err)
	assert.Empty(s.T(), results, "search on empty collection should return empty results")
}

// ===========================================================================
// Context Timeout Tests
// ===========================================================================

// TestContextTimeout_ReturnsError verifies that operations fail with
// an appropriate error when the context deadline is exceeded.
func (s *QdrantIntegrationSuite) TestContextTimeout_ReturnsError() {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Nanosecond)
	defer cancel()
	// Allow the timeout to take effect.
	time.Sleep(1 * time.Millisecond)

	_, err := s.client.ListCollections(ctx)
	require.Error(s.T(), err,
		"ListCollections with expired context should return an error")
}

// ===========================================================================
// Error Code Classification Tests
// ===========================================================================

// TestErrorCode_TimeoutClassification verifies that a real operation
// timeout produces the correct sserr error classification.
func (s *QdrantIntegrationSuite) TestErrorCode_TimeoutClassification() {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := s.client.ListCollections(ctx)
	require.Error(s.T(), err)

	assert.True(s.T(), sserr.IsTimeout(err),
		"expected IsTimeout()=true for deadline exceeded error")
	assert.True(s.T(), sserr.IsRetryable(err),
		"expected IsRetryable()=true for timeout error")
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestClose_ReleasesResources verifies that after Close is called, the
// client's connection is shut down and further operations fail. This test
// creates its own client so it can close it without affecting other tests
// in the suite.
func (s *QdrantIntegrationSuite) TestClose_ReleasesResources() {
	// Parse host and port from the GRPCEndpoint.
	var host string
	var port int
	for i := len(s.qdrantResult.GRPCEndpoint) - 1; i >= 0; i-- {
		if s.qdrantResult.GRPCEndpoint[i] == ':' {
			host = s.qdrantResult.GRPCEndpoint[:i]
			_, _ = fmt.Sscanf(s.qdrantResult.GRPCEndpoint[i+1:], "%d", &port)
			break
		}
	}

	cfg := qdrant.Config{
		Host:     host,
		GRPCPort: port,
	}

	client, err := qdrant.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err)

	// Verify the client works before closing.
	require.NoError(s.T(), client.Health(s.ctx),
		"Health() should succeed before Close()")

	err = client.Close()
	require.NoError(s.T(), err, "Close() should succeed")

	// After Close, Health should fail because the connection is shut down.
	assert.Error(s.T(), client.Health(s.ctx),
		"Health() should fail after Close()")
}

// ===========================================================================
// Concurrency Tests
// ===========================================================================

// TestConcurrentOperations verifies that the client can handle concurrent
// operations from multiple goroutines, validating that the gRPC connection
// and client are safe for concurrent use.
func (s *QdrantIntegrationSuite) TestConcurrentOperations() {
	collectionName := "test_concurrent"

	err := s.client.CreateCollection(s.ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: pb.NewVectorsConfig(&pb.VectorParams{
			Size:     4,
			Distance: pb.Distance_Cosine,
		}),
	})
	require.NoError(s.T(), err)

	const numWorkers = 10
	var wg sync.WaitGroup
	errs := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, upsertErr := s.client.Upsert(s.ctx, &pb.UpsertPoints{
				CollectionName: collectionName,
				Points: []*pb.PointStruct{
					{
						Id:      pb.NewIDNum(uint64(n)),
						Vectors: pb.NewVectors(float32(n)*0.1, float32(n)*0.2, float32(n)*0.3, float32(n)*0.4),
						Payload: pb.NewValueMap(map[string]any{"worker": n}),
					},
				},
			})
			if upsertErr != nil {
				errs <- upsertErr
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(s.T(), err,
			"concurrent Upsert should not produce errors")
	}

	// Qdrant indexes vectors asynchronously after Upsert returns. A brief
	// wait is required before search operations to allow the HNSW index to
	// incorporate the newly upserted points. Without this, searches may
	// return stale or empty results.
	time.Sleep(500 * time.Millisecond)

	// Verify all points were inserted by scrolling.
	limit := uint32(100)
	points, err := s.client.Scroll(s.ctx, &pb.ScrollPoints{
		CollectionName: collectionName,
		Limit:          &limit,
	})
	require.NoError(s.T(), err)
	assert.Equal(s.T(), numWorkers, len(points),
		"all concurrent upserts should succeed")
}

// ===========================================================================
// VectorDB Accessor Tests
// ===========================================================================

// TestVectorDBAccessor verifies that client.VectorDB() returns a functional
// VectorDB that can execute operations directly, bypassing the client's
// tracing and error wrapping layer.
func (s *QdrantIntegrationSuite) TestVectorDBAccessor() {
	vdb := s.client.VectorDB()
	require.NotNil(s.T(), vdb, "VectorDB() should return non-nil")

	// Use the VectorDB directly to perform a health check.
	reply, err := vdb.HealthCheck(s.ctx)
	require.NoError(s.T(), err, "direct VectorDB HealthCheck should succeed")
	assert.NotNil(s.T(), reply, "HealthCheckReply should not be nil")
}
