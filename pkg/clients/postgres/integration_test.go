//go:build integration

// Package postgres_test contains integration tests for the PostgreSQL client
// that require a running PostgreSQL instance. These tests are gated behind the
// "integration" build tag and are executed in CI with Docker via testcontainers.
//
// Run locally with:
//
//	go test -v -race -tags=integration ./pkg/clients/postgres/...
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/StricklySoft/stricklysoft-core/pkg/clients/postgres"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// testDBName is the database name used for integration tests.
const testDBName = "stricklysoft_test"

// testDBUser is the database user used for integration tests.
const testDBUser = "testuser"

// testDBPassword is the database password used for integration tests.
const testDBPassword = "testpassword"

// setupContainer starts a PostgreSQL 16 container and returns a connected
// Client. The container and client are cleaned up automatically when the
// test completes.
func setupContainer(t *testing.T) *postgres.Client {
	t.Helper()

	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"docker.io/postgres:16-alpine",
		tcpostgres.WithDatabase(testDBName),
		tcpostgres.WithUsername(testDBUser),
		tcpostgres.WithPassword(testDBPassword),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if termErr := container.Terminate(ctx); termErr != nil {
			t.Logf("failed to terminate postgres container: %v", termErr)
		}
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	cfg := postgres.Config{
		URI:      connStr,
		MaxConns: 5,
		MinConns: 1,
	}
	if valErr := cfg.Validate(); valErr != nil {
		t.Fatalf("failed to validate config: %v", valErr)
	}

	client, err := postgres.NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	t.Cleanup(func() {
		client.Close()
	})

	return client
}

// ===========================================================================
// Connection Tests
// ===========================================================================

// TestIntegration_NewClient_ConnectsSuccessfully verifies that NewClient
// can establish a connection to a real PostgreSQL instance and that the
// returned client is functional.
func TestIntegration_NewClient_ConnectsSuccessfully(t *testing.T) {
	client := setupContainer(t)
	if client == nil {
		t.Fatal("setupContainer returned nil client")
	}
}

// TestIntegration_Health_ReturnsNil verifies that Health returns nil when
// the database is reachable and responding to pings.
func TestIntegration_Health_ReturnsNil(t *testing.T) {
	client := setupContainer(t)
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health() error: %v", err)
	}
}

// ===========================================================================
// Exec Tests
// ===========================================================================

// TestIntegration_Exec_CreateTable verifies that Exec can execute DDL
// statements to create tables.
func TestIntegration_Exec_CreateTable(t *testing.T) {
	client := setupContainer(t)
	ctx := context.Background()

	_, err := client.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS test_users (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT UNIQUE NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		t.Fatalf("Exec(CREATE TABLE) error: %v", err)
	}
}

// TestIntegration_Exec_InsertAndRowsAffected verifies that Exec can insert
// rows and that the returned command tag reports the correct number of
// affected rows.
func TestIntegration_Exec_InsertAndRowsAffected(t *testing.T) {
	client := setupContainer(t)
	ctx := context.Background()

	// Create the table first.
	_, err := client.Exec(ctx, `CREATE TABLE test_insert (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("Exec(CREATE TABLE) error: %v", err)
	}

	// Insert a row and verify the command tag.
	tag, err := client.Exec(ctx, `INSERT INTO test_insert (name) VALUES ($1)`, "Alice")
	if err != nil {
		t.Fatalf("Exec(INSERT) error: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Errorf("RowsAffected() = %d, want 1", tag.RowsAffected())
	}
}

// ===========================================================================
// Query Tests
// ===========================================================================

// TestIntegration_Query_SelectMultipleRows verifies that Query can retrieve
// multiple rows from a table and that the results can be iterated and
// scanned correctly.
func TestIntegration_Query_SelectMultipleRows(t *testing.T) {
	client := setupContainer(t)
	ctx := context.Background()

	// Set up test data.
	_, err := client.Exec(ctx, `CREATE TABLE test_query (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("Exec(CREATE TABLE) error: %v", err)
	}
	_, err = client.Exec(ctx, `INSERT INTO test_query (name) VALUES ($1), ($2), ($3)`, "Alice", "Bob", "Charlie")
	if err != nil {
		t.Fatalf("Exec(INSERT) error: %v", err)
	}

	// Query the rows.
	rows, err := client.Query(ctx, `SELECT id, name FROM test_query ORDER BY id`)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var id int
		var name string
		if scanErr := rows.Scan(&id, &name); scanErr != nil {
			t.Fatalf("Scan() error: %v", scanErr)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}

	if len(names) != 3 {
		t.Fatalf("got %d rows, want 3", len(names))
	}
	if names[0] != "Alice" || names[1] != "Bob" || names[2] != "Charlie" {
		t.Errorf("names = %v, want [Alice, Bob, Charlie]", names)
	}
}

// ===========================================================================
// QueryRow Tests
// ===========================================================================

// TestIntegration_QueryRow_SingleRow verifies that QueryRow returns a single
// row that can be scanned successfully.
func TestIntegration_QueryRow_SingleRow(t *testing.T) {
	client := setupContainer(t)
	ctx := context.Background()

	_, err := client.Exec(ctx, `CREATE TABLE test_queryrow (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("Exec(CREATE TABLE) error: %v", err)
	}
	_, err = client.Exec(ctx, `INSERT INTO test_queryrow (name) VALUES ($1)`, "Alice")
	if err != nil {
		t.Fatalf("Exec(INSERT) error: %v", err)
	}

	var name string
	scanErr := client.QueryRow(ctx, `SELECT name FROM test_queryrow WHERE id = $1`, 1).Scan(&name)
	if scanErr != nil {
		t.Fatalf("QueryRow().Scan() error: %v", scanErr)
	}
	if name != "Alice" {
		t.Errorf("name = %q, want %q", name, "Alice")
	}
}

// TestIntegration_QueryRow_NoRows verifies that QueryRow returns
// pgx.ErrNoRows when no matching row is found.
func TestIntegration_QueryRow_NoRows(t *testing.T) {
	client := setupContainer(t)
	ctx := context.Background()

	_, err := client.Exec(ctx, `CREATE TABLE test_norows (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("Exec(CREATE TABLE) error: %v", err)
	}

	var name string
	scanErr := client.QueryRow(ctx, `SELECT name FROM test_norows WHERE id = $1`, 999).Scan(&name)
	if !errors.Is(scanErr, pgx.ErrNoRows) {
		t.Errorf("QueryRow().Scan() error = %v, want pgx.ErrNoRows", scanErr)
	}
}

// ===========================================================================
// Transaction Tests
// ===========================================================================

// TestIntegration_Transaction_CommitPersistsData verifies that a committed
// transaction persists data that is visible after the transaction completes.
func TestIntegration_Transaction_CommitPersistsData(t *testing.T) {
	client := setupContainer(t)
	ctx := context.Background()

	_, err := client.Exec(ctx, `CREATE TABLE test_tx_commit (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("Exec(CREATE TABLE) error: %v", err)
	}

	// Begin a transaction, insert data, and commit.
	tx, err := client.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	_, err = tx.Exec(ctx, `INSERT INTO test_tx_commit (name) VALUES ($1)`, "TxAlice")
	if err != nil {
		t.Fatalf("tx.Exec(INSERT) error: %v", err)
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("Commit() error: %v", commitErr)
	}

	// Verify the data is visible outside the transaction.
	var name string
	scanErr := client.QueryRow(ctx, `SELECT name FROM test_tx_commit WHERE id = 1`).Scan(&name)
	if scanErr != nil {
		t.Fatalf("QueryRow().Scan() after commit error: %v", scanErr)
	}
	if name != "TxAlice" {
		t.Errorf("name = %q, want %q", name, "TxAlice")
	}
}

// TestIntegration_Transaction_RollbackDiscardsData verifies that a rolled-back
// transaction does not persist data — the inserted row is not visible after
// rollback.
func TestIntegration_Transaction_RollbackDiscardsData(t *testing.T) {
	client := setupContainer(t)
	ctx := context.Background()

	_, err := client.Exec(ctx, `CREATE TABLE test_tx_rollback (id SERIAL PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("Exec(CREATE TABLE) error: %v", err)
	}

	// Begin a transaction, insert data, and roll back.
	tx, err := client.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	_, err = tx.Exec(ctx, `INSERT INTO test_tx_rollback (name) VALUES ($1)`, "TxGhost")
	if err != nil {
		t.Fatalf("tx.Exec(INSERT) error: %v", err)
	}

	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("Rollback() error: %v", rollbackErr)
	}

	// Verify the data is NOT visible — table should be empty.
	var count int
	scanErr := client.QueryRow(ctx, `SELECT COUNT(*) FROM test_tx_rollback`).Scan(&count)
	if scanErr != nil {
		t.Fatalf("QueryRow().Scan() after rollback error: %v", scanErr)
	}
	if count != 0 {
		t.Errorf("count = %d after rollback, want 0", count)
	}
}

// ===========================================================================
// Context Timeout Tests
// ===========================================================================

// TestIntegration_ContextTimeout_ReturnsError verifies that operations
// fail with an appropriate error when the context deadline is exceeded.
func TestIntegration_ContextTimeout_ReturnsError(t *testing.T) {
	client := setupContainer(t)

	// Create a context that is already expired.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	// Allow the timeout to take effect.
	time.Sleep(1 * time.Millisecond)

	_, err := client.Query(ctx, `SELECT pg_sleep(10)`)
	if err == nil {
		t.Fatal("Query() with expired context expected error, got nil")
	}
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestIntegration_Close_ReleasesResources verifies that after Close is
// called, the client's pool is shut down and further operations fail.
func TestIntegration_Close_ReleasesResources(t *testing.T) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"docker.io/postgres:16-alpine",
		tcpostgres.WithDatabase(testDBName),
		tcpostgres.WithUsername(testDBUser),
		tcpostgres.WithPassword(testDBPassword),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if termErr := container.Terminate(ctx); termErr != nil {
			t.Logf("failed to terminate postgres container: %v", termErr)
		}
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	cfg := postgres.Config{
		URI:      connStr,
		MaxConns: 5,
		MinConns: 1,
	}
	if valErr := cfg.Validate(); valErr != nil {
		t.Fatalf("failed to validate config: %v", valErr)
	}

	client, err := postgres.NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Verify the client works before closing.
	if healthErr := client.Health(ctx); healthErr != nil {
		t.Fatalf("Health() before close error: %v", healthErr)
	}

	// Close the client.
	client.Close()

	// After Close, Health should fail because the pool is shut down.
	healthErr := client.Health(ctx)
	if healthErr == nil {
		t.Error("Health() after Close() expected error, got nil")
	}
}
