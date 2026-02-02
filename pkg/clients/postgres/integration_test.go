//go:build integration

// Package postgres_test contains integration tests for the PostgreSQL client
// that require a running PostgreSQL instance via testcontainers-go. These
// tests are gated behind the "integration" build tag and are executed in CI
// with Docker.
//
// Run locally with:
//
//	go test -v -race -tags=integration ./pkg/clients/postgres/...
//
// Or via Makefile:
//
//	make test-integration
//
// # Architecture
//
// All tests run within a single [suite.Suite] that starts one PostgreSQL
// container in [SetupSuite] and terminates it in [TearDownSuite]. Test
// isolation is achieved via unique table names per test method rather than
// per-test containers, which reduces total execution time from ~60s
// (10 container starts) to ~10s (1 container start).
//
// # Table Naming Convention
//
// Each test method creates tables with a unique prefix derived from its
// test category (e.g., test_exec_create, test_tx_commit, test_datatypes).
// Tables are not dropped between tests because the entire database is
// destroyed when the container terminates.
package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/StricklySoft/stricklysoft-core/internal/testutil/containers"
	"github.com/StricklySoft/stricklysoft-core/pkg/clients/postgres"
	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Suite Definition
// ===========================================================================

// PostgresIntegrationSuite runs all PostgreSQL integration tests against
// a single shared container. The container is started once in SetupSuite
// and terminated in TearDownSuite. All test methods share the same client
// and database, using unique table names for isolation.
type PostgresIntegrationSuite struct {
	suite.Suite

	// ctx is the background context used for container and client
	// lifecycle operations.
	ctx context.Context

	// pgResult holds the started PostgreSQL container and connection
	// string. It is set in SetupSuite and used to terminate the
	// container in TearDownSuite.
	pgResult *containers.PostgresResult

	// client is the SDK PostgreSQL client connected to the test
	// container. All test methods use this client unless they need
	// to test client creation or close behavior.
	client *postgres.Client

	// connString is the PostgreSQL connection URI for the test
	// container. Tests that need to create additional clients use
	// this to connect to the same database.
	connString string
}

// SetupSuite starts a single PostgreSQL container and creates a client
// shared across all tests in the suite. This runs once before any test
// method executes.
func (s *PostgresIntegrationSuite) SetupSuite() {
	s.ctx = context.Background()

	result, err := containers.StartPostgres(s.ctx)
	require.NoError(s.T(), err, "failed to start PostgreSQL container")
	s.pgResult = result
	s.connString = result.ConnString

	cfg := postgres.Config{
		URI:      result.ConnString,
		MaxConns: 10,
		MinConns: 2,
	}
	require.NoError(s.T(), cfg.Validate(), "failed to validate config")

	client, err := postgres.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err, "failed to create PostgreSQL client")
	s.client = client
}

// TearDownSuite closes the client and terminates the container. This
// runs once after all test methods have completed.
func (s *PostgresIntegrationSuite) TearDownSuite() {
	if s.client != nil {
		s.client.Close()
	}
	if s.pgResult != nil {
		if err := s.pgResult.Container.Terminate(s.ctx); err != nil {
			s.T().Logf("failed to terminate postgres container: %v", err)
		}
	}
}

// TestPostgresIntegration is the top-level entry point that runs all
// suite tests. It is skipped in short mode (-short flag) to allow fast
// unit test runs without Docker.
func TestPostgresIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	suite.Run(t, new(PostgresIntegrationSuite))
}

// ===========================================================================
// Connection Tests
// ===========================================================================

// TestNewClient_ConnectsSuccessfully verifies that NewClient can
// establish a connection to a real PostgreSQL instance and that the
// returned client is functional.
func (s *PostgresIntegrationSuite) TestNewClient_ConnectsSuccessfully() {
	require.NotNil(s.T(), s.client, "suite client should not be nil")
}

// TestHealth_ReturnsNil verifies that Health returns nil when the
// database is reachable and responding to pings.
func (s *PostgresIntegrationSuite) TestHealth_ReturnsNil() {
	err := s.client.Health(s.ctx)
	require.NoError(s.T(), err, "Health() should succeed when DB is reachable")
}

// ===========================================================================
// Exec Tests
// ===========================================================================

// TestExec_CreateTable verifies that Exec can execute DDL statements
// to create tables in the database.
func (s *PostgresIntegrationSuite) TestExec_CreateTable() {
	_, err := s.client.Exec(s.ctx, `
		CREATE TABLE IF NOT EXISTS test_exec_create (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT UNIQUE NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	require.NoError(s.T(), err, "Exec(CREATE TABLE) should succeed")
}

// TestExec_InsertAndRowsAffected verifies that Exec can insert rows
// and that the returned command tag reports the correct number of
// affected rows.
func (s *PostgresIntegrationSuite) TestExec_InsertAndRowsAffected() {
	_, err := s.client.Exec(s.ctx,
		`CREATE TABLE test_exec_insert (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(s.T(), err)

	tag, err := s.client.Exec(s.ctx,
		`INSERT INTO test_exec_insert (name) VALUES ($1)`, "Alice")
	require.NoError(s.T(), err)
	assert.Equal(s.T(), int64(1), tag.RowsAffected(),
		"INSERT should affect exactly 1 row")
}

// ===========================================================================
// Query Tests
// ===========================================================================

// TestQuery_SelectMultipleRows verifies that Query can retrieve
// multiple rows from a table and that the results can be iterated
// and scanned correctly.
func (s *PostgresIntegrationSuite) TestQuery_SelectMultipleRows() {
	_, err := s.client.Exec(s.ctx,
		`CREATE TABLE test_query_multi (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(s.T(), err)
	_, err = s.client.Exec(s.ctx,
		`INSERT INTO test_query_multi (name) VALUES ($1), ($2), ($3)`,
		"Alice", "Bob", "Charlie")
	require.NoError(s.T(), err)

	rows, err := s.client.Query(s.ctx,
		`SELECT id, name FROM test_query_multi ORDER BY id`)
	require.NoError(s.T(), err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var id int
		var name string
		require.NoError(s.T(), rows.Scan(&id, &name))
		names = append(names, name)
	}
	require.NoError(s.T(), rows.Err())
	assert.Equal(s.T(), []string{"Alice", "Bob", "Charlie"}, names)
}

// TestQuery_EmptyResultSet verifies that Query returns no rows without
// error when the queried table is empty. This differs from QueryRow's
// pgx.ErrNoRows behavior.
func (s *PostgresIntegrationSuite) TestQuery_EmptyResultSet() {
	_, err := s.client.Exec(s.ctx,
		`CREATE TABLE test_query_empty (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(s.T(), err)

	rows, err := s.client.Query(s.ctx,
		`SELECT id, name FROM test_query_empty`)
	require.NoError(s.T(), err)
	defer rows.Close()

	assert.False(s.T(), rows.Next(), "expected no rows from empty table")
	require.NoError(s.T(), rows.Err())
}

// ===========================================================================
// QueryRow Tests
// ===========================================================================

// TestQueryRow_SingleRow verifies that QueryRow returns a single row
// that can be scanned successfully.
func (s *PostgresIntegrationSuite) TestQueryRow_SingleRow() {
	_, err := s.client.Exec(s.ctx,
		`CREATE TABLE test_qr_single (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(s.T(), err)
	_, err = s.client.Exec(s.ctx,
		`INSERT INTO test_qr_single (name) VALUES ($1)`, "Alice")
	require.NoError(s.T(), err)

	var name string
	err = s.client.QueryRow(s.ctx,
		`SELECT name FROM test_qr_single WHERE id = $1`, 1).Scan(&name)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "Alice", name)
}

// TestQueryRow_NoRows verifies that QueryRow returns pgx.ErrNoRows
// when no matching row is found.
func (s *PostgresIntegrationSuite) TestQueryRow_NoRows() {
	_, err := s.client.Exec(s.ctx,
		`CREATE TABLE test_qr_norows (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(s.T(), err)

	var name string
	err = s.client.QueryRow(s.ctx,
		`SELECT name FROM test_qr_norows WHERE id = $1`, 999).Scan(&name)
	assert.ErrorIs(s.T(), err, pgx.ErrNoRows,
		"QueryRow on nonexistent row should return pgx.ErrNoRows")
}

// ===========================================================================
// Transaction Tests
// ===========================================================================

// TestTransaction_CommitPersistsData verifies that a committed
// transaction persists data that is visible after the transaction
// completes.
func (s *PostgresIntegrationSuite) TestTransaction_CommitPersistsData() {
	_, err := s.client.Exec(s.ctx,
		`CREATE TABLE test_tx_commit (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(s.T(), err)

	tx, err := s.client.Begin(s.ctx)
	require.NoError(s.T(), err)

	_, err = tx.Exec(s.ctx,
		`INSERT INTO test_tx_commit (name) VALUES ($1)`, "TxAlice")
	require.NoError(s.T(), err)
	require.NoError(s.T(), tx.Commit(s.ctx))

	var name string
	err = s.client.QueryRow(s.ctx,
		`SELECT name FROM test_tx_commit WHERE id = 1`).Scan(&name)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), "TxAlice", name)
}

// TestTransaction_RollbackDiscardsData verifies that a rolled-back
// transaction does not persist data — the inserted row is not visible
// after rollback.
func (s *PostgresIntegrationSuite) TestTransaction_RollbackDiscardsData() {
	_, err := s.client.Exec(s.ctx,
		`CREATE TABLE test_tx_rollback (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(s.T(), err)

	tx, err := s.client.Begin(s.ctx)
	require.NoError(s.T(), err)

	_, err = tx.Exec(s.ctx,
		`INSERT INTO test_tx_rollback (name) VALUES ($1)`, "TxGhost")
	require.NoError(s.T(), err)
	require.NoError(s.T(), tx.Rollback(s.ctx))

	var count int
	err = s.client.QueryRow(s.ctx,
		`SELECT COUNT(*) FROM test_tx_rollback`).Scan(&count)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 0, count, "table should be empty after rollback")
}

// TestTransaction_MultipleOperations verifies that a transaction can
// execute multiple INSERT, UPDATE, and SELECT operations, with all
// changes visible within the transaction and after commit.
func (s *PostgresIntegrationSuite) TestTransaction_MultipleOperations() {
	_, err := s.client.Exec(s.ctx, `CREATE TABLE test_tx_multi (
		id SERIAL PRIMARY KEY, name TEXT NOT NULL, active BOOLEAN DEFAULT true)`)
	require.NoError(s.T(), err)

	tx, err := s.client.Begin(s.ctx)
	require.NoError(s.T(), err)

	// Insert two rows.
	_, err = tx.Exec(s.ctx,
		`INSERT INTO test_tx_multi (name) VALUES ($1), ($2)`, "Alice", "Bob")
	require.NoError(s.T(), err)

	// Update one row within the transaction.
	_, err = tx.Exec(s.ctx,
		`UPDATE test_tx_multi SET active = false WHERE name = $1`, "Bob")
	require.NoError(s.T(), err)

	// Read within the transaction to verify visibility.
	var active bool
	err = tx.QueryRow(s.ctx,
		`SELECT active FROM test_tx_multi WHERE name = $1`, "Bob").Scan(&active)
	require.NoError(s.T(), err)
	assert.False(s.T(), active, "Bob should be inactive within the transaction")

	require.NoError(s.T(), tx.Commit(s.ctx))

	// Verify after commit: only Alice should be active.
	var activeCount int
	err = s.client.QueryRow(s.ctx,
		`SELECT COUNT(*) FROM test_tx_multi WHERE active = true`).Scan(&activeCount)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), 1, activeCount,
		"only 1 row should be active after commit")
}

// ===========================================================================
// Context Timeout Tests
// ===========================================================================

// TestContextTimeout_ReturnsError verifies that operations fail with
// an appropriate error when the context deadline is exceeded.
func (s *PostgresIntegrationSuite) TestContextTimeout_ReturnsError() {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Nanosecond)
	defer cancel()
	// Allow the timeout to take effect.
	time.Sleep(1 * time.Millisecond)

	_, err := s.client.Query(ctx, `SELECT pg_sleep(10)`)
	require.Error(s.T(), err,
		"Query with expired context should return an error")
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestClose_ReleasesResources verifies that after Close is called,
// the client's pool is shut down and further operations fail. This
// test creates its own client so it can close it without affecting
// other tests in the suite.
func (s *PostgresIntegrationSuite) TestClose_ReleasesResources() {
	cfg := postgres.Config{
		URI:      s.connString,
		MaxConns: 5,
		MinConns: 1,
	}
	require.NoError(s.T(), cfg.Validate())

	client, err := postgres.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err)

	// Verify the client works before closing.
	require.NoError(s.T(), client.Health(s.ctx),
		"Health() should succeed before Close()")

	client.Close()

	// After Close, Health should fail because the pool is shut down.
	assert.Error(s.T(), client.Health(s.ctx),
		"Health() should fail after Close()")
}

// ===========================================================================
// Error Code Classification Tests
// ===========================================================================

// TestErrorCode_TimeoutClassification verifies that a real query
// timeout produces the correct sserr error classification. A deadline-
// exceeded error should be classified as timeout and retryable.
func (s *PostgresIntegrationSuite) TestErrorCode_TimeoutClassification() {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := s.client.Query(ctx, `SELECT pg_sleep(10)`)
	require.Error(s.T(), err)

	assert.True(s.T(), sserr.IsTimeout(err),
		"expected IsTimeout()=true for deadline exceeded error")
	assert.True(s.T(), sserr.IsRetryable(err),
		"expected IsRetryable()=true for timeout error")
}

// TestErrorCode_InvalidSQL verifies that invalid SQL syntax produces
// an internal database error (not a timeout or validation error).
func (s *PostgresIntegrationSuite) TestErrorCode_InvalidSQL() {
	_, err := s.client.Exec(s.ctx, `INVALID SQL STATEMENT`)
	require.Error(s.T(), err)

	assert.True(s.T(), sserr.IsInternal(err),
		"expected IsInternal()=true for invalid SQL")
	assert.False(s.T(), sserr.IsTimeout(err),
		"expected IsTimeout()=false for invalid SQL")
}

// TestErrorCode_ConstraintViolation verifies that a UNIQUE constraint
// violation produces an internal database error and that the underlying
// PostgreSQL error (pgconn.PgError) is preserved in the error chain
// with the correct SQLSTATE code.
func (s *PostgresIntegrationSuite) TestErrorCode_ConstraintViolation() {
	_, err := s.client.Exec(s.ctx, `CREATE TABLE test_constraint (
		id SERIAL PRIMARY KEY, email TEXT UNIQUE NOT NULL)`)
	require.NoError(s.T(), err)

	_, err = s.client.Exec(s.ctx,
		`INSERT INTO test_constraint (email) VALUES ($1)`, "dup@example.com")
	require.NoError(s.T(), err)

	// Insert a duplicate — should violate the UNIQUE constraint.
	_, err = s.client.Exec(s.ctx,
		`INSERT INTO test_constraint (email) VALUES ($1)`, "dup@example.com")
	require.Error(s.T(), err)

	assert.True(s.T(), sserr.IsInternal(err),
		"constraint violation should be classified as internal error")

	// Verify the PostgreSQL-specific error is preserved in the chain.
	var pgErr *pgconn.PgError
	assert.True(s.T(), errors.As(err, &pgErr),
		"error chain should contain *pgconn.PgError")
	if pgErr != nil {
		assert.Equal(s.T(), "23505", pgErr.Code,
			"expected PostgreSQL SQLSTATE 23505 (unique_violation)")
	}
}

// ===========================================================================
// URI-Based Connection Tests
// ===========================================================================

// TestNewClient_URIBasedConnection verifies that creating a client
// using Config.URI (connection string) works end-to-end, connecting
// to the same container that the suite uses.
func (s *PostgresIntegrationSuite) TestNewClient_URIBasedConnection() {
	cfg := postgres.Config{
		URI:      s.connString,
		MaxConns: 3,
		MinConns: 1,
	}
	require.NoError(s.T(), cfg.Validate())

	client, err := postgres.NewClient(s.ctx, cfg)
	require.NoError(s.T(), err)
	defer client.Close()

	require.NoError(s.T(), client.Health(s.ctx),
		"URI-based client should pass health check")
}

// ===========================================================================
// Data Type Tests
// ===========================================================================

// TestMultipleDataTypes verifies that the client correctly handles
// inserting and querying multiple PostgreSQL data types: INTEGER, TEXT,
// TIMESTAMPTZ, BOOLEAN, and JSONB.
func (s *PostgresIntegrationSuite) TestMultipleDataTypes() {
	_, err := s.client.Exec(s.ctx, `CREATE TABLE test_datatypes (
		id         SERIAL PRIMARY KEY,
		int_val    INTEGER NOT NULL,
		text_val   TEXT NOT NULL,
		ts_val     TIMESTAMPTZ NOT NULL,
		bool_val   BOOLEAN NOT NULL,
		json_val   JSONB NOT NULL
	)`)
	require.NoError(s.T(), err)

	now := time.Now().UTC().Truncate(time.Microsecond)
	jsonData, err := json.Marshal(map[string]string{"key": "value"})
	require.NoError(s.T(), err)

	_, err = s.client.Exec(s.ctx,
		`INSERT INTO test_datatypes (int_val, text_val, ts_val, bool_val, json_val)
		 VALUES ($1, $2, $3, $4, $5)`,
		42, "hello", now, true, jsonData)
	require.NoError(s.T(), err)

	var intVal int
	var textVal string
	var tsVal time.Time
	var boolVal bool
	var jsonVal []byte

	err = s.client.QueryRow(s.ctx,
		`SELECT int_val, text_val, ts_val, bool_val, json_val
		 FROM test_datatypes WHERE id = 1`).
		Scan(&intVal, &textVal, &tsVal, &boolVal, &jsonVal)
	require.NoError(s.T(), err)

	assert.Equal(s.T(), 42, intVal)
	assert.Equal(s.T(), "hello", textVal)
	assert.WithinDuration(s.T(), now, tsVal, time.Millisecond,
		"timestamp should round-trip within 1ms precision")
	assert.True(s.T(), boolVal, "boolean should round-trip as true")

	var parsed map[string]string
	require.NoError(s.T(), json.Unmarshal(jsonVal, &parsed))
	assert.Equal(s.T(), "value", parsed["key"],
		"JSONB data should round-trip correctly")
}

// TestNullHandling verifies that the client correctly handles NULL
// values for nullable columns, using pointer scan targets.
func (s *PostgresIntegrationSuite) TestNullHandling() {
	_, err := s.client.Exec(s.ctx, `CREATE TABLE test_nulls (
		id   SERIAL PRIMARY KEY,
		name TEXT,
		age  INTEGER
	)`)
	require.NoError(s.T(), err)

	// Insert a row with all nullable columns set to NULL.
	_, err = s.client.Exec(s.ctx,
		`INSERT INTO test_nulls (name, age) VALUES ($1, $2)`, nil, nil)
	require.NoError(s.T(), err)

	var name *string
	var age *int
	err = s.client.QueryRow(s.ctx,
		`SELECT name, age FROM test_nulls WHERE id = 1`).Scan(&name, &age)
	require.NoError(s.T(), err)
	assert.Nil(s.T(), name, "name should scan as nil for NULL column")
	assert.Nil(s.T(), age, "age should scan as nil for NULL column")
}

// ===========================================================================
// Concurrency Tests
// ===========================================================================

// TestConcurrentOperations verifies that the client can handle
// concurrent operations from multiple goroutines, validating that the
// connection pool and client are safe for concurrent use.
func (s *PostgresIntegrationSuite) TestConcurrentOperations() {
	_, err := s.client.Exec(s.ctx, `CREATE TABLE test_concurrent (
		id SERIAL PRIMARY KEY, val INTEGER NOT NULL)`)
	require.NoError(s.T(), err)

	const numWorkers = 10
	var wg sync.WaitGroup
	errs := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, execErr := s.client.Exec(s.ctx,
				`INSERT INTO test_concurrent (val) VALUES ($1)`, n)
			if execErr != nil {
				errs <- execErr
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(s.T(), err,
			"concurrent INSERT should not produce errors")
	}

	var count int
	err = s.client.QueryRow(s.ctx,
		`SELECT COUNT(*) FROM test_concurrent`).Scan(&count)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), numWorkers, count,
		"all concurrent INSERTs should succeed")
}

// ===========================================================================
// Pool Accessor Tests
// ===========================================================================

// TestPoolAccessor verifies that client.Pool() returns a functional
// pool that can execute operations directly, bypassing the client's
// tracing and error wrapping layer.
func (s *PostgresIntegrationSuite) TestPoolAccessor() {
	pool := s.client.Pool()
	require.NotNil(s.T(), pool, "Pool() should return non-nil")

	// Use the pool directly to ping the database.
	err := pool.Ping(s.ctx)
	require.NoError(s.T(), err, "direct pool Ping should succeed")

	// Use the pool directly to execute a query.
	rows, err := pool.Query(s.ctx, `SELECT 1 AS val`)
	require.NoError(s.T(), err)
	defer rows.Close()

	require.True(s.T(), rows.Next(), "pool Query should return at least one row")
	var val int
	require.NoError(s.T(), rows.Scan(&val))
	assert.Equal(s.T(), 1, val)
}
