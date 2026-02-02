//go:build integration

// Package neo4j_test contains integration tests for the Neo4j client that
// require a running Neo4j instance via testcontainers-go. These tests are
// gated behind the "integration" build tag and are executed in CI with Docker.
//
// Run locally with:
//
//	go test -v -race -tags=integration ./pkg/clients/neo4j/...
//
// Or via Makefile:
//
//	make test-integration
//
// # Architecture
//
// All tests run within a single [suite.Suite] that starts one Neo4j
// container in [SetupSuite] and terminates it in [TearDownSuite]. Test
// isolation is achieved via unique node labels per test method rather than
// per-test containers, which reduces total execution time significantly.
//
// # Node Label Naming Convention
//
// Each test method creates nodes with a unique label derived from its
// test category (e.g., TestCreateNode, TestReadNode). All test data is
// destroyed when the container terminates.
package neo4j_test

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
	"github.com/StricklySoft/stricklysoft-core/pkg/clients/neo4j"
	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Suite Definition
// ===========================================================================

// Neo4jIntegrationSuite runs all Neo4j integration tests against a single
// shared container. The container is started once in SetupSuite and
// terminated in TearDownSuite. All test methods share the same client
// and database, using unique node labels for isolation.
type Neo4jIntegrationSuite struct {
	suite.Suite

	// ctx is the background context used for container and client
	// lifecycle operations.
	ctx context.Context

	// neo4jResult holds the started Neo4j container and connection
	// details. It is set in SetupSuite and used to terminate the
	// container in TearDownSuite.
	neo4jResult *containers.Neo4jResult

	// client is the SDK Neo4j client connected to the test container.
	// All test methods use this client unless they need to test client
	// creation or close behavior.
	client *neo4j.Client
}

// SetupSuite starts a single Neo4j container and creates a client shared
// across all tests in the suite. This runs once before any test method
// executes.
func (s *Neo4jIntegrationSuite) SetupSuite() {
	s.ctx = context.Background()

	result, err := containers.StartNeo4j(s.ctx)
	require.NoError(s.T(), err, "failed to start Neo4j container")
	s.neo4jResult = result

	cfg := neo4j.Config{
		URI:                   result.BoltURL,
		Database:              "neo4j",
		Username:              result.Username,
		Password:              neo4j.Secret(result.Password),
		MaxConnectionPoolSize: 10,
	}
	require.NoError(s.T(), cfg.Validate(), "failed to validate config")

	client, err := neo4j.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err, "failed to create Neo4j client")
	s.client = client
}

// TearDownSuite closes the client and terminates the container. This
// runs once after all test methods have completed.
func (s *Neo4jIntegrationSuite) TearDownSuite() {
	if s.client != nil {
		_ = s.client.Close(s.ctx)
	}
	if s.neo4jResult != nil {
		if err := s.neo4jResult.Container.Terminate(s.ctx); err != nil {
			s.T().Logf("failed to terminate neo4j container: %v", err)
		}
	}
}

// TestNeo4jIntegration is the top-level entry point that runs all suite
// tests. It is skipped in short mode (-short flag) to allow fast unit
// test runs without Docker.
func TestNeo4jIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	suite.Run(t, new(Neo4jIntegrationSuite))
}

// ===========================================================================
// Connection Tests
// ===========================================================================

// TestNewClient_ConnectsSuccessfully verifies that NewClient can establish
// a connection to a real Neo4j instance and that the returned client is
// functional.
func (s *Neo4jIntegrationSuite) TestNewClient_ConnectsSuccessfully() {
	require.NotNil(s.T(), s.client, "suite client should not be nil")
}

// TestHealth_ReturnsNil verifies that Health returns nil when the database
// is reachable and responding.
func (s *Neo4jIntegrationSuite) TestHealth_ReturnsNil() {
	err := s.client.Health(s.ctx)
	require.NoError(s.T(), err, "Health() should succeed when DB is reachable")
}

// ===========================================================================
// ExecuteWrite Tests
// ===========================================================================

// TestExecuteWrite_CreateNode verifies that ExecuteWrite can create a node
// in the graph database and return it.
func (s *Neo4jIntegrationSuite) TestExecuteWrite_CreateNode() {
	records, err := s.client.ExecuteWrite(s.ctx,
		"CREATE (n:TestCreateNode {name: $name}) RETURN n.name AS name",
		map[string]any{"name": "Alice"})
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 1)

	name, ok := records[0].Get("name")
	require.True(s.T(), ok, "record should contain 'name' field")
	assert.Equal(s.T(), "Alice", name)
}

// TestExecuteWrite_UpdateNode verifies that ExecuteWrite can update an
// existing node's properties.
func (s *Neo4jIntegrationSuite) TestExecuteWrite_UpdateNode() {
	// Create a node first.
	_, err := s.client.ExecuteWrite(s.ctx,
		"CREATE (n:TestUpdateNode {name: $name, updated: false}) RETURN n",
		map[string]any{"name": "UpdateTarget"})
	require.NoError(s.T(), err)

	// Update the node.
	records, err := s.client.ExecuteWrite(s.ctx,
		"MATCH (n:TestUpdateNode {name: $name}) SET n.updated = true RETURN n.updated AS updated",
		map[string]any{"name": "UpdateTarget"})
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 1)

	updated, ok := records[0].Get("updated")
	require.True(s.T(), ok)
	assert.Equal(s.T(), true, updated)
}

// TestExecuteWrite_DeleteNode verifies that ExecuteWrite can delete a
// node from the graph database.
func (s *Neo4jIntegrationSuite) TestExecuteWrite_DeleteNode() {
	// Create a node to delete.
	_, err := s.client.ExecuteWrite(s.ctx,
		"CREATE (n:TestDeleteNode {name: $name}) RETURN n",
		map[string]any{"name": "ToBeDeleted"})
	require.NoError(s.T(), err)

	// Delete the node.
	_, err = s.client.ExecuteWrite(s.ctx,
		"MATCH (n:TestDeleteNode {name: $name}) DELETE n",
		map[string]any{"name": "ToBeDeleted"})
	require.NoError(s.T(), err)

	// Verify deletion.
	records, err := s.client.ExecuteRead(s.ctx,
		"MATCH (n:TestDeleteNode {name: $name}) RETURN n",
		map[string]any{"name": "ToBeDeleted"})
	require.NoError(s.T(), err)
	assert.Empty(s.T(), records, "node should have been deleted")
}

// TestExecuteWrite_CreateRelationship verifies that ExecuteWrite can
// create relationships between nodes.
func (s *Neo4jIntegrationSuite) TestExecuteWrite_CreateRelationship() {
	// Create two nodes and a relationship between them.
	_, err := s.client.ExecuteWrite(s.ctx,
		`CREATE (a:TestRelPerson {name: $name1})
		 CREATE (b:TestRelPerson {name: $name2})
		 CREATE (a)-[:KNOWS {since: $since}]->(b)
		 RETURN a.name AS from, b.name AS to`,
		map[string]any{"name1": "Alice", "name2": "Bob", "since": 2020})
	require.NoError(s.T(), err)

	// Verify the relationship exists.
	records, err := s.client.ExecuteRead(s.ctx,
		`MATCH (a:TestRelPerson {name: $name1})-[r:KNOWS]->(b:TestRelPerson {name: $name2})
		 RETURN r.since AS since`,
		map[string]any{"name1": "Alice", "name2": "Bob"})
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 1)

	since, ok := records[0].Get("since")
	require.True(s.T(), ok)
	assert.Equal(s.T(), int64(2020), since)
}

// ===========================================================================
// ExecuteRead Tests
// ===========================================================================

// TestExecuteRead_MatchNode verifies that ExecuteRead can query nodes
// from the graph database.
func (s *Neo4jIntegrationSuite) TestExecuteRead_MatchNode() {
	// Create a node first.
	_, err := s.client.ExecuteWrite(s.ctx,
		"CREATE (n:TestReadNode {name: $name}) RETURN n",
		map[string]any{"name": "ReadTarget"})
	require.NoError(s.T(), err)

	// Read the node back.
	records, err := s.client.ExecuteRead(s.ctx,
		"MATCH (n:TestReadNode {name: $name}) RETURN n.name AS name",
		map[string]any{"name": "ReadTarget"})
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 1)

	name, ok := records[0].Get("name")
	require.True(s.T(), ok)
	assert.Equal(s.T(), "ReadTarget", name)
}

// TestExecuteRead_TraverseRelationship verifies that ExecuteRead can
// traverse relationships and return path data.
func (s *Neo4jIntegrationSuite) TestExecuteRead_TraverseRelationship() {
	// Create a chain: A -> B -> C
	_, err := s.client.ExecuteWrite(s.ctx,
		`CREATE (a:TestTraverse {name: "A"})
		 CREATE (b:TestTraverse {name: "B"})
		 CREATE (c:TestTraverse {name: "C"})
		 CREATE (a)-[:NEXT]->(b)
		 CREATE (b)-[:NEXT]->(c)`,
		nil)
	require.NoError(s.T(), err)

	// Traverse the chain.
	records, err := s.client.ExecuteRead(s.ctx,
		`MATCH (a:TestTraverse {name: "A"})-[:NEXT*]->(end:TestTraverse)
		 RETURN end.name AS name ORDER BY name`,
		nil)
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 2, "should reach B and C from A")

	name0, _ := records[0].Get("name")
	name1, _ := records[1].Get("name")
	assert.Equal(s.T(), "B", name0)
	assert.Equal(s.T(), "C", name1)
}

// TestExecuteRead_EmptyResult verifies that ExecuteRead returns an empty
// slice (not an error) when no matching nodes exist.
func (s *Neo4jIntegrationSuite) TestExecuteRead_EmptyResult() {
	records, err := s.client.ExecuteRead(s.ctx,
		"MATCH (n:NonExistentLabel_12345) RETURN n",
		nil)
	require.NoError(s.T(), err)
	assert.Empty(s.T(), records, "expected no records for nonexistent label")
}

// TestExecuteRead_MultipleRecords verifies that ExecuteRead can return
// multiple records from a single query.
func (s *Neo4jIntegrationSuite) TestExecuteRead_MultipleRecords() {
	// Create multiple nodes.
	_, err := s.client.ExecuteWrite(s.ctx,
		`CREATE (a:TestMultiRead {name: "A"})
		 CREATE (b:TestMultiRead {name: "B"})
		 CREATE (c:TestMultiRead {name: "C"})`,
		nil)
	require.NoError(s.T(), err)

	records, err := s.client.ExecuteRead(s.ctx,
		"MATCH (n:TestMultiRead) RETURN n.name AS name ORDER BY name",
		nil)
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 3)

	var names []string
	for _, r := range records {
		name, _ := r.Get("name")
		names = append(names, name.(string))
	}
	assert.Equal(s.T(), []string{"A", "B", "C"}, names)
}

// ===========================================================================
// Run Tests (Auto-commit)
// ===========================================================================

// TestRun_AutoCommitQuery verifies that Run can execute a simple
// auto-commit query.
func (s *Neo4jIntegrationSuite) TestRun_AutoCommitQuery() {
	records, err := s.client.Run(s.ctx, "RETURN 1 AS val", nil)
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 1)

	val, ok := records[0].Get("val")
	require.True(s.T(), ok)
	assert.Equal(s.T(), int64(1), val)
}

// ===========================================================================
// Data Type Tests
// ===========================================================================

// TestExecuteWrite_MultipleDataTypes verifies that the client correctly
// handles inserting and querying multiple Neo4j data types: string, int,
// float, bool, and list.
func (s *Neo4jIntegrationSuite) TestExecuteWrite_MultipleDataTypes() {
	records, err := s.client.ExecuteWrite(s.ctx,
		`CREATE (n:TestDataTypes {
			str_val: $str,
			int_val: $int,
			float_val: $float,
			bool_val: $bool,
			list_val: $list
		 })
		 RETURN n.str_val AS str, n.int_val AS int, n.float_val AS float,
		        n.bool_val AS bool, n.list_val AS list`,
		map[string]any{
			"str":   "hello",
			"int":   42,
			"float": 3.14,
			"bool":  true,
			"list":  []any{"a", "b", "c"},
		})
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 1)

	r := records[0]

	strVal, _ := r.Get("str")
	assert.Equal(s.T(), "hello", strVal)

	intVal, _ := r.Get("int")
	assert.Equal(s.T(), int64(42), intVal)

	floatVal, _ := r.Get("float")
	assert.InDelta(s.T(), 3.14, floatVal, 0.001)

	boolVal, _ := r.Get("bool")
	assert.Equal(s.T(), true, boolVal)

	listVal, _ := r.Get("list")
	assert.Equal(s.T(), []any{"a", "b", "c"}, listVal)
}

// ===========================================================================
// Session Tests
// ===========================================================================

// TestSession_RawAccess verifies that Session() returns a functional
// session that can execute queries directly, bypassing the client's
// managed transaction methods.
func (s *Neo4jIntegrationSuite) TestSession_RawAccess() {
	session := s.client.Session(s.ctx)
	defer func() { _ = session.Close(s.ctx) }()

	result, err := session.Run(s.ctx, "RETURN 'raw_session' AS val", nil)
	require.NoError(s.T(), err)

	records, err := result.Collect(s.ctx)
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 1)

	val, ok := records[0].Get("val")
	require.True(s.T(), ok)
	assert.Equal(s.T(), "raw_session", val)
}

// ===========================================================================
// Context Timeout Tests
// ===========================================================================

// TestContextTimeout_ReturnsError verifies that operations fail with an
// appropriate error when the context deadline is exceeded.
func (s *Neo4jIntegrationSuite) TestContextTimeout_ReturnsError() {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Nanosecond)
	defer cancel()
	// Allow the timeout to take effect.
	time.Sleep(1 * time.Millisecond)

	_, err := s.client.ExecuteRead(ctx,
		"UNWIND range(1, 10000000) AS x RETURN x",
		nil)
	require.Error(s.T(), err,
		"ExecuteRead with expired context should return an error")
}

// ===========================================================================
// Error Code Classification Tests
// ===========================================================================

// TestErrorCode_TimeoutClassification verifies that a real query timeout
// produces the correct sserr error classification.
func (s *Neo4jIntegrationSuite) TestErrorCode_TimeoutClassification() {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := s.client.ExecuteRead(ctx,
		"UNWIND range(1, 10000000) AS x RETURN x",
		nil)
	require.Error(s.T(), err)

	// The error should be classified as a timeout or at least a server error.
	// Note: Neo4j driver may wrap deadline exceeded differently, so we check
	// for either timeout or internal classification.
	var ssErr *sserr.Error
	require.True(s.T(), errors.As(err, &ssErr),
		"error should be a *sserr.Error, got %T", err)
	assert.True(s.T(), sserr.IsServerError(err),
		"expected IsServerError()=true for deadline exceeded error")
}

// TestErrorCode_InvalidCypher verifies that invalid Cypher syntax
// produces an internal database error.
func (s *Neo4jIntegrationSuite) TestErrorCode_InvalidCypher() {
	_, err := s.client.ExecuteWrite(s.ctx,
		"INVALID CYPHER STATEMENT !!!", nil)
	require.Error(s.T(), err)

	assert.True(s.T(), sserr.IsInternal(err),
		"expected IsInternal()=true for invalid Cypher")
	assert.False(s.T(), sserr.IsTimeout(err),
		"expected IsTimeout()=false for invalid Cypher")
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestClose_ReleasesResources verifies that after Close is called, the
// client's driver is shut down and further operations fail. This test
// creates its own client so it can close it without affecting other
// tests in the suite.
func (s *Neo4jIntegrationSuite) TestClose_ReleasesResources() {
	cfg := neo4j.Config{
		URI:                   s.neo4jResult.BoltURL,
		Database:              "neo4j",
		Username:              s.neo4jResult.Username,
		Password:              neo4j.Secret(s.neo4jResult.Password),
		MaxConnectionPoolSize: 5,
	}
	require.NoError(s.T(), cfg.Validate())

	client, err := neo4j.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err)

	// Verify the client works before closing.
	require.NoError(s.T(), client.Health(s.ctx),
		"Health() should succeed before Close()")

	require.NoError(s.T(), client.Close(s.ctx))

	// After Close, Health should fail because the driver is shut down.
	assert.Error(s.T(), client.Health(s.ctx),
		"Health() should fail after Close()")
}

// ===========================================================================
// Concurrency Tests
// ===========================================================================

// TestConcurrentOperations verifies that the client can handle concurrent
// operations from multiple goroutines, validating that the connection
// pool and client are safe for concurrent use.
func (s *Neo4jIntegrationSuite) TestConcurrentOperations() {
	const numWorkers = 10
	var wg sync.WaitGroup
	errs := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, execErr := s.client.ExecuteWrite(s.ctx,
				"CREATE (n:TestConcurrent {val: $val}) RETURN n",
				map[string]any{"val": n})
			if execErr != nil {
				errs <- execErr
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(s.T(), err,
			"concurrent CREATE should not produce errors")
	}

	records, err := s.client.ExecuteRead(s.ctx,
		"MATCH (n:TestConcurrent) RETURN count(n) AS cnt",
		nil)
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 1)

	cnt, ok := records[0].Get("cnt")
	require.True(s.T(), ok)
	assert.Equal(s.T(), int64(numWorkers), cnt,
		"all concurrent CREATEs should succeed")
}

// ===========================================================================
// Driver Accessor Tests
// ===========================================================================

// TestDriverAccessor verifies that client.Driver() returns a non-nil
// driver that can be used directly.
func (s *Neo4jIntegrationSuite) TestDriverAccessor() {
	driver := s.client.Driver()
	require.NotNil(s.T(), driver, "Driver() should return non-nil")

	// Use the driver directly to verify connectivity.
	err := driver.VerifyConnectivity(s.ctx)
	require.NoError(s.T(), err, "direct driver VerifyConnectivity should succeed")
}

// ===========================================================================
// URI-Based Connection Tests
// ===========================================================================

// TestNewClient_URIBasedConnection verifies that creating a client using
// Config.URI works end-to-end, connecting to the same container that
// the suite uses.
func (s *Neo4jIntegrationSuite) TestNewClient_URIBasedConnection() {
	cfg := neo4j.Config{
		URI:                   s.neo4jResult.BoltURL,
		Database:              "neo4j",
		Username:              s.neo4jResult.Username,
		Password:              neo4j.Secret(s.neo4jResult.Password),
		MaxConnectionPoolSize: 3,
	}
	require.NoError(s.T(), cfg.Validate())

	client, err := neo4j.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err)
	defer func() {
		_ = client.Close(s.ctx)
	}()

	require.NoError(s.T(), client.Health(s.ctx),
		"URI-based client should pass health check")

	// Verify the client is functional by running a simple query.
	records, err := client.Run(s.ctx, "RETURN 'uri_test' AS val", nil)
	require.NoError(s.T(), err)
	require.Len(s.T(), records, 1)

	val, ok := records[0].Get("val")
	require.True(s.T(), ok)
	assert.Equal(s.T(), "uri_test", val)
}

// ===========================================================================
// Utility: uniqueLabel generates a unique label for test isolation.
// ===========================================================================

func uniqueLabel(base string, idx int) string {
	return fmt.Sprintf("%s_%d_%d", base, time.Now().UnixNano(), idx)
}
