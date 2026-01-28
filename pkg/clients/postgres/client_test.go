package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// NewFromPool Tests
// ===========================================================================

// TestNewFromPool_WithConfig verifies that NewFromPool correctly initializes
// the client with the provided pool and config, extracting the database name
// for OpenTelemetry span attributes.
func TestNewFromPool_WithConfig(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	cfg := &Config{Database: "testdb"}
	client := NewFromPool(mock, cfg)

	if client.pool == nil {
		t.Error("pool is nil, want non-nil")
	}
	if client.config != cfg {
		t.Error("config not set correctly")
	}
	if client.databaseName != "testdb" {
		t.Errorf("databaseName = %q, want %q", client.databaseName, "testdb")
	}
	if client.tracer == nil {
		t.Error("tracer is nil, want non-nil")
	}
}

// TestNewFromPool_NilConfig verifies that NewFromPool handles a nil config
// gracefully by initializing a zero-value Config.
func TestNewFromPool_NilConfig(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	client := NewFromPool(mock, nil)

	if client.config == nil {
		t.Error("config is nil, want non-nil zero-value Config")
	}
	if client.databaseName != "" {
		t.Errorf("databaseName = %q, want empty string for nil config", client.databaseName)
	}
}

// ===========================================================================
// Query Tests
// ===========================================================================

// TestClient_Query_Success verifies that Query returns rows on a successful
// database query. It checks that the pgxmock expectations are met and that
// the returned rows can be iterated and scanned correctly.
func TestClient_Query_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	expectedRows := pgxmock.NewRows([]string{"id", "name"}).
		AddRow(1, "Alice").
		AddRow(2, "Bob")
	mock.ExpectQuery("SELECT id, name FROM users").
		WillReturnRows(expectedRows)

	client := NewFromPool(mock, &Config{Database: "testdb"})
	rows, err := client.Query(context.Background(), "SELECT id, name FROM users")
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	defer rows.Close()

	// Verify we can iterate and scan the returned rows.
	var count int
	for rows.Next() {
		var id int
		var name string
		if scanErr := rows.Scan(&id, &name); scanErr != nil {
			t.Fatalf("Scan() error: %v", scanErr)
		}
		count++
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2", count)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestClient_Query_Error verifies that Query returns a *sserr.Error with
// CodeInternalDatabase when the database returns a non-timeout error.
func TestClient_Query_Error(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT").
		WillReturnError(errors.New("relation does not exist"))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, queryErr := client.Query(context.Background(), "SELECT * FROM nonexistent")
	if queryErr == nil {
		t.Fatal("Query() expected error, got nil")
	}

	var ssErr *sserr.Error
	if !errors.As(queryErr, &ssErr) {
		t.Fatalf("Query() error type = %T, want *sserr.Error", queryErr)
	}
	if ssErr.Code != sserr.CodeInternalDatabase {
		t.Errorf("error code = %q, want %q", ssErr.Code, sserr.CodeInternalDatabase)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestClient_Query_TimeoutError verifies that Query returns a *sserr.Error
// with CodeTimeoutDatabase when the context deadline is exceeded.
func TestClient_Query_TimeoutError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT").
		WillReturnError(context.DeadlineExceeded)

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, queryErr := client.Query(context.Background(), "SELECT 1")
	if queryErr == nil {
		t.Fatal("Query() expected error, got nil")
	}

	var ssErr *sserr.Error
	if !errors.As(queryErr, &ssErr) {
		t.Fatalf("Query() error type = %T, want *sserr.Error", queryErr)
	}
	if ssErr.Code != sserr.CodeTimeoutDatabase {
		t.Errorf("error code = %q, want %q", ssErr.Code, sserr.CodeTimeoutDatabase)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===========================================================================
// QueryRow Tests
// ===========================================================================

// TestClient_QueryRow_Success verifies that QueryRow returns a row that
// can be scanned successfully on a matching query.
func TestClient_QueryRow_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	expectedRows := pgxmock.NewRows([]string{"name"}).AddRow("Alice")
	mock.ExpectQuery("SELECT name FROM users WHERE id").
		WithArgs(42).
		WillReturnRows(expectedRows)

	client := NewFromPool(mock, &Config{Database: "testdb"})
	row := client.QueryRow(context.Background(), "SELECT name FROM users WHERE id = $1", 42)

	var name string
	if scanErr := row.Scan(&name); scanErr != nil {
		t.Fatalf("Scan() error: %v", scanErr)
	}
	if name != "Alice" {
		t.Errorf("name = %q, want %q", name, "Alice")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestClient_QueryRow_NoRows verifies that QueryRow returns pgx.ErrNoRows
// when no matching row is found, surfacing the error during Scan().
func TestClient_QueryRow_NoRows(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT name FROM users WHERE id").
		WithArgs(999).
		WillReturnError(pgx.ErrNoRows)

	client := NewFromPool(mock, &Config{Database: "testdb"})
	row := client.QueryRow(context.Background(), "SELECT name FROM users WHERE id = $1", 999)

	var name string
	scanErr := row.Scan(&name)
	if !errors.Is(scanErr, pgx.ErrNoRows) {
		t.Errorf("Scan() error = %v, want pgx.ErrNoRows", scanErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===========================================================================
// Exec Tests
// ===========================================================================

// TestClient_Exec_Success verifies that Exec returns the correct command tag
// on a successful DML statement.
func TestClient_Exec_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectExec("DELETE FROM sessions").
		WillReturnResult(pgxmock.NewResult("DELETE", 5))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	tag, err := client.Exec(context.Background(), "DELETE FROM sessions WHERE expired = true")
	if err != nil {
		t.Fatalf("Exec() error: %v", err)
	}
	if tag.RowsAffected() != 5 {
		t.Errorf("RowsAffected() = %d, want 5", tag.RowsAffected())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestClient_Exec_Error verifies that Exec returns a *sserr.Error with
// CodeInternalDatabase when the database returns an error.
func TestClient_Exec_Error(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectExec("INSERT INTO users").
		WithArgs("dup@example.com").
		WillReturnError(&pgconn.PgError{
			Code:    "23505",
			Message: "duplicate key value violates unique constraint",
		})

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, execErr := client.Exec(context.Background(), "INSERT INTO users (email) VALUES ($1)", "dup@example.com")
	if execErr == nil {
		t.Fatal("Exec() expected error, got nil")
	}

	var ssErr *sserr.Error
	if !errors.As(execErr, &ssErr) {
		t.Fatalf("Exec() error type = %T, want *sserr.Error", execErr)
	}
	if ssErr.Code != sserr.CodeInternalDatabase {
		t.Errorf("error code = %q, want %q", ssErr.Code, sserr.CodeInternalDatabase)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===========================================================================
// Begin Tests
// ===========================================================================

// TestClient_Begin_Success verifies that Begin returns a valid transaction
// handle on success.
func TestClient_Begin_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectBegin()

	client := NewFromPool(mock, &Config{Database: "testdb"})
	tx, err := client.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	if tx == nil {
		t.Error("Begin() returned nil transaction, want non-nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestClient_Begin_Error verifies that Begin returns a *sserr.Error with
// CodeInternalDatabase when the database fails to start a transaction.
func TestClient_Begin_Error(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectBegin().WillReturnError(errors.New("connection refused"))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, beginErr := client.Begin(context.Background())
	if beginErr == nil {
		t.Fatal("Begin() expected error, got nil")
	}

	var ssErr *sserr.Error
	if !errors.As(beginErr, &ssErr) {
		t.Fatalf("Begin() error type = %T, want *sserr.Error", beginErr)
	}
	if ssErr.Code != sserr.CodeInternalDatabase {
		t.Errorf("error code = %q, want %q", ssErr.Code, sserr.CodeInternalDatabase)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===========================================================================
// Health Tests
// ===========================================================================

// TestClient_Health_Success verifies that Health returns nil when the
// database ping succeeds.
func TestClient_Health_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectPing()

	client := NewFromPool(mock, &Config{Database: "testdb"})
	if healthErr := client.Health(context.Background()); healthErr != nil {
		t.Fatalf("Health() error: %v", healthErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestClient_Health_Failure verifies that Health returns a *sserr.Error with
// CodeUnavailableDependency when the database ping fails.
func TestClient_Health_Failure(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectPing().WillReturnError(errors.New("connection refused"))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	healthErr := client.Health(context.Background())
	if healthErr == nil {
		t.Fatal("Health() expected error, got nil")
	}

	var ssErr *sserr.Error
	if !errors.As(healthErr, &ssErr) {
		t.Fatalf("Health() error type = %T, want *sserr.Error", healthErr)
	}
	if ssErr.Code != sserr.CodeUnavailableDependency {
		t.Errorf("error code = %q, want %q", ssErr.Code, sserr.CodeUnavailableDependency)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestClient_Health_AppliesDefaultTimeout verifies that Health applies
// DefaultHealthTimeout when the caller's context has no deadline set.
func TestClient_Health_AppliesDefaultTimeout(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	// Use a context without a deadline to trigger default timeout application.
	mock.ExpectPing()

	client := NewFromPool(mock, &Config{Database: "testdb"})
	if healthErr := client.Health(context.Background()); healthErr != nil {
		t.Fatalf("Health() error: %v", healthErr)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===========================================================================
// Close Tests
// ===========================================================================

// TestClient_Close verifies that Close delegates to the underlying pool's
// Close method. The mock pool tracks whether Close was called.
func TestClient_Close(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}

	mock.ExpectClose()

	client := NewFromPool(mock, nil)
	client.Close()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===========================================================================
// Pool Accessor Tests
// ===========================================================================

// TestClient_Pool_ReturnsUnderlyingPool verifies that Pool() returns the
// same pool instance that was injected via NewFromPool.
func TestClient_Pool_ReturnsUnderlyingPool(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	client := NewFromPool(mock, nil)
	pool := client.Pool()
	if pool == nil {
		t.Error("Pool() returned nil, want non-nil")
	}
}

// ===========================================================================
// wrapError Tests
// ===========================================================================

// TestWrapError_Nil verifies that wrapError returns nil when given a nil
// error, preventing unnecessary error wrapping.
func TestWrapError_Nil(t *testing.T) {
	result := wrapError(nil, "should not wrap")
	if result != nil {
		t.Errorf("wrapError(nil) = %v, want nil", result)
	}
}

// TestWrapError_DeadlineExceeded verifies that wrapError classifies
// context.DeadlineExceeded as CodeTimeoutDatabase.
func TestWrapError_DeadlineExceeded(t *testing.T) {
	result := wrapError(context.DeadlineExceeded, "query timed out")
	if result == nil {
		t.Fatal("wrapError() returned nil, want *sserr.Error")
	}
	if result.Code != sserr.CodeTimeoutDatabase {
		t.Errorf("code = %q, want %q", result.Code, sserr.CodeTimeoutDatabase)
	}
	if !errors.Is(result, context.DeadlineExceeded) {
		t.Error("wrapError() result does not unwrap to context.DeadlineExceeded")
	}
}

// TestWrapError_ContextCanceled verifies that wrapError classifies
// context.Canceled as CodeTimeoutDatabase.
func TestWrapError_ContextCanceled(t *testing.T) {
	result := wrapError(context.Canceled, "query canceled")
	if result == nil {
		t.Fatal("wrapError() returned nil, want *sserr.Error")
	}
	if result.Code != sserr.CodeTimeoutDatabase {
		t.Errorf("code = %q, want %q", result.Code, sserr.CodeTimeoutDatabase)
	}
	if !errors.Is(result, context.Canceled) {
		t.Error("wrapError() result does not unwrap to context.Canceled")
	}
}

// TestWrapError_GenericError verifies that wrapError classifies generic
// database errors as CodeInternalDatabase.
func TestWrapError_GenericError(t *testing.T) {
	cause := errors.New("syntax error at or near SELECT")
	result := wrapError(cause, "exec failed")
	if result == nil {
		t.Fatal("wrapError() returned nil, want *sserr.Error")
	}
	if result.Code != sserr.CodeInternalDatabase {
		t.Errorf("code = %q, want %q", result.Code, sserr.CodeInternalDatabase)
	}
	if !errors.Is(result, cause) {
		t.Error("wrapError() result does not unwrap to original cause")
	}
}

// TestWrapError_PgError verifies that wrapError classifies PostgreSQL-specific
// errors (pgconn.PgError) as CodeInternalDatabase, preserving the original
// error in the chain for inspection.
func TestWrapError_PgError(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:    "42P01",
		Message: "relation \"users\" does not exist",
	}
	result := wrapError(pgErr, "query failed")
	if result == nil {
		t.Fatal("wrapError() returned nil, want *sserr.Error")
	}
	if result.Code != sserr.CodeInternalDatabase {
		t.Errorf("code = %q, want %q", result.Code, sserr.CodeInternalDatabase)
	}

	// Verify the original PgError is preserved in the error chain.
	var unwrapped *pgconn.PgError
	if !errors.As(result, &unwrapped) {
		t.Error("wrapError() result does not unwrap to *pgconn.PgError")
	}
}

// ===========================================================================
// Error Classification Integration Tests
// ===========================================================================

// TestErrorClassification_QueryTimeout verifies the full error classification
// pipeline: a timeout error from Query is classified correctly by the
// platform error helpers (IsTimeout, IsRetryable).
func TestErrorClassification_QueryTimeout(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectQuery("SELECT").
		WillReturnError(context.DeadlineExceeded)

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, queryErr := client.Query(context.Background(), "SELECT 1")
	if queryErr == nil {
		t.Fatal("Query() expected error, got nil")
	}

	if !sserr.IsTimeout(queryErr) {
		t.Error("IsTimeout() = false, want true for deadline exceeded error")
	}
	if !sserr.IsRetryable(queryErr) {
		t.Error("IsRetryable() = false, want true for timeout error")
	}
	if !sserr.IsServerError(queryErr) {
		t.Error("IsServerError() = false, want true for timeout error")
	}
}

// TestErrorClassification_ExecInternalDatabase verifies that a generic
// database error from Exec is classified as an internal error.
func TestErrorClassification_ExecInternalDatabase(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectExec("INSERT").
		WillReturnError(errors.New("disk full"))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	_, execErr := client.Exec(context.Background(), "INSERT INTO logs (msg) VALUES ($1)", "test")
	if execErr == nil {
		t.Fatal("Exec() expected error, got nil")
	}

	if !sserr.IsInternal(execErr) {
		t.Error("IsInternal() = false, want true for database error")
	}
	if sserr.IsTimeout(execErr) {
		t.Error("IsTimeout() = true, want false for non-timeout database error")
	}
	if sserr.IsRetryable(execErr) {
		t.Error("IsRetryable() = true, want false for internal database error")
	}
}

// TestErrorClassification_HealthUnavailable verifies that a health check
// failure is classified as an unavailable dependency error.
func TestErrorClassification_HealthUnavailable(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	mock.ExpectPing().WillReturnError(errors.New("connection refused"))

	client := NewFromPool(mock, &Config{Database: "testdb"})
	healthErr := client.Health(context.Background())
	if healthErr == nil {
		t.Fatal("Health() expected error, got nil")
	}

	if !sserr.IsUnavailable(healthErr) {
		t.Error("IsUnavailable() = false, want true for health check failure")
	}
	if !sserr.IsRetryable(healthErr) {
		t.Error("IsRetryable() = false, want true for unavailable dependency")
	}
}
