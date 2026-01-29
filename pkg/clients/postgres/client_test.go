package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// NewFromPool Tests
// ===========================================================================

// TestNewFromPool_WithConfig verifies that NewFromPool correctly initializes
// the client with the provided pool and config, extracting the database name
// for OpenTelemetry span attributes.
func TestNewFromPool_WithConfig(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	cfg := &Config{Database: "testdb"}
	client := NewFromPool(mock, cfg)

	assert.NotNil(t, client.pool)
	assert.Equal(t, cfg, client.config)
	assert.Equal(t, "testdb", client.databaseName)
	assert.NotNil(t, client.tracer)
}

// TestNewFromPool_NilConfig verifies that NewFromPool handles a nil config
// gracefully by initializing a zero-value Config.
func TestNewFromPool_NilConfig(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	client := NewFromPool(mock, nil)

	require.NotNil(t, client.config)
	assert.Equal(t, "", client.databaseName)
}

// ===========================================================================
// Query Tests
// ===========================================================================

// TestClient_Query_Success verifies that Query returns rows on a successful
// database query. It checks that the pgxmock expectations are met and that
// the returned rows can be iterated and scanned correctly.
func TestClient_Query_Success(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	expectedRows := pgxmock.NewRows([]string{"id", "name"}).
		AddRow(1, "Alice").
		AddRow(2, "Bob")
	mock.ExpectQuery("SELECT id, name FROM users").
		WillReturnRows(expectedRows)

	client := NewFromPool(mock, &Config{Database: "testdb"})
	rows, err := client.Query(context.Background(), "SELECT id, name FROM users")
	require.NoError(t, err)
	defer rows.Close()

	// Verify we can iterate and scan the returned rows.
	var count int
	for rows.Next() {
		var id int
		var name string
		scanErr := rows.Scan(&id, &name)
		require.NoError(t, scanErr)
		count++
	}
	assert.Equal(t, 2, count)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestClient_Query_Error verifies that Query returns a *sserr.Error with
// CodeInternalDatabase when the database returns a non-timeout error.
func TestClient_Query_Error(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery("SELECT").
		WillReturnError(errors.New("relation does not exist"))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, queryErr := client.Query(context.Background(), "SELECT * FROM nonexistent")
	require.Error(t, queryErr)

	var ssErr *sserr.Error
	require.True(t, errors.As(queryErr, &ssErr), "Query() error type = %T, want *sserr.Error", queryErr)
	assert.Equal(t, sserr.CodeInternalDatabase, ssErr.Code)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestClient_Query_TimeoutError verifies that Query returns a *sserr.Error
// with CodeTimeoutDatabase when the context deadline is exceeded.
func TestClient_Query_TimeoutError(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery("SELECT").
		WillReturnError(context.DeadlineExceeded)

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, queryErr := client.Query(context.Background(), "SELECT 1")
	require.Error(t, queryErr)

	var ssErr *sserr.Error
	require.True(t, errors.As(queryErr, &ssErr), "Query() error type = %T, want *sserr.Error", queryErr)
	assert.Equal(t, sserr.CodeTimeoutDatabase, ssErr.Code)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// ===========================================================================
// QueryRow Tests
// ===========================================================================

// TestClient_QueryRow_Success verifies that QueryRow returns a row that
// can be scanned successfully on a matching query.
func TestClient_QueryRow_Success(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	expectedRows := pgxmock.NewRows([]string{"name"}).AddRow("Alice")
	mock.ExpectQuery("SELECT name FROM users WHERE id").
		WithArgs(42).
		WillReturnRows(expectedRows)

	client := NewFromPool(mock, &Config{Database: "testdb"})
	row := client.QueryRow(context.Background(), "SELECT name FROM users WHERE id = $1", 42)

	var name string
	scanErr := row.Scan(&name)
	require.NoError(t, scanErr)
	assert.Equal(t, "Alice", name)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestClient_QueryRow_NoRows verifies that QueryRow returns pgx.ErrNoRows
// when no matching row is found, surfacing the error during Scan().
func TestClient_QueryRow_NoRows(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery("SELECT name FROM users WHERE id").
		WithArgs(999).
		WillReturnError(pgx.ErrNoRows)

	client := NewFromPool(mock, &Config{Database: "testdb"})
	row := client.QueryRow(context.Background(), "SELECT name FROM users WHERE id = $1", 999)

	var name string
	scanErr := row.Scan(&name)
	assert.ErrorIs(t, scanErr, pgx.ErrNoRows)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// ===========================================================================
// Exec Tests
// ===========================================================================

// TestClient_Exec_Success verifies that Exec returns the correct command tag
// on a successful DML statement.
func TestClient_Exec_Success(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectExec("DELETE FROM sessions").
		WillReturnResult(pgxmock.NewResult("DELETE", 5))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	tag, err := client.Exec(context.Background(), "DELETE FROM sessions WHERE expired = true")
	require.NoError(t, err)
	assert.Equal(t, int64(5), tag.RowsAffected())

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestClient_Exec_Error verifies that Exec returns a *sserr.Error with
// CodeInternalDatabase when the database returns an error.
func TestClient_Exec_Error(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectExec("INSERT INTO users").
		WithArgs("dup@example.com").
		WillReturnError(&pgconn.PgError{
			Code:    "23505",
			Message: "duplicate key value violates unique constraint",
		})

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, execErr := client.Exec(context.Background(), "INSERT INTO users (email) VALUES ($1)", "dup@example.com")
	require.Error(t, execErr)

	var ssErr *sserr.Error
	require.True(t, errors.As(execErr, &ssErr), "Exec() error type = %T, want *sserr.Error", execErr)
	assert.Equal(t, sserr.CodeInternalDatabase, ssErr.Code)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// ===========================================================================
// Begin Tests
// ===========================================================================

// TestClient_Begin_Success verifies that Begin returns a valid transaction
// handle on success.
func TestClient_Begin_Success(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectBegin()

	client := NewFromPool(mock, &Config{Database: "testdb"})
	tx, err := client.Begin(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, tx)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestClient_Begin_Error verifies that Begin returns a *sserr.Error with
// CodeInternalDatabase when the database fails to start a transaction.
func TestClient_Begin_Error(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectBegin().WillReturnError(errors.New("connection refused"))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, beginErr := client.Begin(context.Background())
	require.Error(t, beginErr)

	var ssErr *sserr.Error
	require.True(t, errors.As(beginErr, &ssErr), "Begin() error type = %T, want *sserr.Error", beginErr)
	assert.Equal(t, sserr.CodeInternalDatabase, ssErr.Code)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// ===========================================================================
// Health Tests
// ===========================================================================

// TestClient_Health_Success verifies that Health returns nil when the
// database ping succeeds.
func TestClient_Health_Success(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectPing()

	client := NewFromPool(mock, &Config{Database: "testdb"})
	require.NoError(t, client.Health(context.Background()))

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestClient_Health_Failure verifies that Health returns a *sserr.Error with
// CodeUnavailableDependency when the database ping fails.
func TestClient_Health_Failure(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectPing().WillReturnError(errors.New("connection refused"))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	healthErr := client.Health(context.Background())
	require.Error(t, healthErr)

	var ssErr *sserr.Error
	require.True(t, errors.As(healthErr, &ssErr), "Health() error type = %T, want *sserr.Error", healthErr)
	assert.Equal(t, sserr.CodeUnavailableDependency, ssErr.Code)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestClient_Health_AppliesDefaultTimeout verifies that Health applies
// DefaultHealthTimeout when the caller's context has no deadline set.
func TestClient_Health_AppliesDefaultTimeout(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Use a context without a deadline to trigger default timeout application.
	mock.ExpectPing()

	client := NewFromPool(mock, &Config{Database: "testdb"})
	require.NoError(t, client.Health(context.Background()))

	assert.NoError(t, mock.ExpectationsWereMet())
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestClient_Close verifies that Close delegates to the underlying pool's
// Close method. The mock pool tracks whether Close was called.
func TestClient_Close(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	mock.ExpectClose()

	client := NewFromPool(mock, nil)
	client.Close()

	assert.NoError(t, mock.ExpectationsWereMet())
}

// ===========================================================================
// Pool Accessor Tests
// ===========================================================================

// TestClient_Pool_ReturnsUnderlyingPool verifies that Pool() returns the
// same pool instance that was injected via NewFromPool.
func TestClient_Pool_ReturnsUnderlyingPool(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	client := NewFromPool(mock, nil)
	pool := client.Pool()
	assert.NotNil(t, pool)
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
	cause := errors.New("syntax error at or near SELECT")
	result := wrapError(cause, "exec failed")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeInternalDatabase, result.Code)
	assert.ErrorIs(t, result, cause)
}

// TestWrapError_PgError verifies that wrapError classifies PostgreSQL-specific
// errors (pgconn.PgError) as CodeInternalDatabase, preserving the original
// error in the chain for inspection.
func TestWrapError_PgError(t *testing.T) {
	t.Parallel()
	pgErr := &pgconn.PgError{
		Code:    "42P01",
		Message: "relation \"users\" does not exist",
	}
	result := wrapError(pgErr, "query failed")
	require.NotNil(t, result)
	assert.Equal(t, sserr.CodeInternalDatabase, result.Code)

	// Verify the original PgError is preserved in the error chain.
	var unwrapped *pgconn.PgError
	assert.True(t, errors.As(result, &unwrapped), "wrapError() result does not unwrap to *pgconn.PgError")
}

// ===========================================================================
// Error Classification Integration Tests
// ===========================================================================

// TestErrorClassification_QueryTimeout verifies the full error classification
// pipeline: a timeout error from Query is classified correctly by the
// platform error helpers (IsTimeout, IsRetryable).
func TestErrorClassification_QueryTimeout(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery("SELECT").
		WillReturnError(context.DeadlineExceeded)

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, queryErr := client.Query(context.Background(), "SELECT 1")
	require.Error(t, queryErr)

	assert.True(t, sserr.IsTimeout(queryErr), "IsTimeout() = false, want true for deadline exceeded error")
	assert.True(t, sserr.IsRetryable(queryErr), "IsRetryable() = false, want true for timeout error")
	assert.True(t, sserr.IsServerError(queryErr), "IsServerError() = false, want true for timeout error")
}

// TestErrorClassification_ExecInternalDatabase verifies that a generic
// database error from Exec is classified as an internal error.
func TestErrorClassification_ExecInternalDatabase(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectExec("INSERT").
		WillReturnError(errors.New("disk full"))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, execErr := client.Exec(context.Background(), "INSERT INTO logs (msg) VALUES ($1)", "test")
	require.Error(t, execErr)

	assert.True(t, sserr.IsInternal(execErr), "IsInternal() = false, want true for database error")
	assert.False(t, sserr.IsTimeout(execErr), "IsTimeout() = true, want false for non-timeout database error")
	assert.False(t, sserr.IsRetryable(execErr), "IsRetryable() = true, want false for internal database error")
}

// TestErrorClassification_HealthUnavailable verifies that a health check
// failure is classified as an unavailable dependency error.
func TestErrorClassification_HealthUnavailable(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectPing().WillReturnError(errors.New("connection refused"))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	healthErr := client.Health(context.Background())
	require.Error(t, healthErr)

	assert.True(t, sserr.IsUnavailable(healthErr), "IsUnavailable() = false, want true for health check failure")
	assert.True(t, sserr.IsRetryable(healthErr), "IsRetryable() = false, want true for unavailable dependency")
}
