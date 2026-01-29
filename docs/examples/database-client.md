# Example: Database Client

This example demonstrates PostgreSQL client initialization, queries,
transactions, health checks, error handling, and testing patterns
using the StricklySoft Core SDK.

## Overview

The `pkg/clients/postgres` package provides a PostgreSQL client with
connection pooling (pgxpool) and OpenTelemetry tracing. This example
covers:

- Configuration with `DefaultConfig()` and environment variables
- Creating a client with `NewClient`
- Querying data with `Query` and `QueryRow`
- Executing statements with `Exec`
- Transaction patterns with `Begin`
- Health checks for readiness probes
- Error handling with platform error codes
- Testing with `pgxmock`

## Configuration

```go
import "github.com/StricklySoft/stricklysoft-core/pkg/clients/postgres"

// Start with sensible defaults for K8s deployment
cfg := postgres.DefaultConfig()

// Override with deployment-specific values
cfg.Host = "db.production.svc.cluster.local"
cfg.Database = "my-service"
cfg.Password = postgres.Secret(os.Getenv("POSTGRES_PASSWORD"))
cfg.SSLMode = postgres.SSLModeVerifyFull
cfg.SSLRootCert = "/etc/ssl/certs/ca.pem"
```

### Default Values

| Field | Default |
|-------|---------|
| Host | `postgres.databases.svc.cluster.local` |
| Port | `5432` |
| Database | `stricklysoft` |
| User | `postgres` |
| SSLMode | `require` |
| MaxConns | `25` |
| MinConns | `5` |
| MaxConnLifetime | `1h` |
| MaxConnIdleTime | `30m` |
| HealthCheckPeriod | `1m` |
| ConnectTimeout | `10s` |

### Environment Variables

All config fields can be set via environment variables:

| Field | Environment Variable |
|-------|---------------------|
| URI | `POSTGRES_URI` |
| Host | `POSTGRES_HOST` |
| Port | `POSTGRES_PORT` |
| Database | `POSTGRES_DATABASE` |
| User | `POSTGRES_USER` |
| Password | `POSTGRES_PASSWORD` |
| SSLMode | `POSTGRES_SSLMODE` |
| SSLRootCert | `POSTGRES_SSL_ROOT_CERT` |
| MaxConns | `POSTGRES_MAX_CONNS` |
| MinConns | `POSTGRES_MIN_CONNS` |
| CloudProvider | `POSTGRES_CLOUD_PROVIDER` |

## Creating a Client

```go
ctx := context.Background()

cfg := postgres.DefaultConfig()
cfg.Password = postgres.Secret(os.Getenv("POSTGRES_PASSWORD"))

client, err := postgres.NewClient(ctx, *cfg)
if err != nil {
    log.Fatalf("failed to connect: %v", err)
}
defer client.Close()
```

`NewClient` validates the configuration, creates the connection pool,
configures TLS if needed, and pings the database to verify connectivity.

## Querying Data

### Multiple Rows

```go
rows, err := client.Query(ctx,
    "SELECT id, name, email FROM users WHERE active = $1", true)
if err != nil {
    return err
}
defer rows.Close()

for rows.Next() {
    var id int
    var name, email string
    if err := rows.Scan(&id, &name, &email); err != nil {
        return err
    }
    fmt.Printf("User: %d %s %s\n", id, name, email)
}
```

### Single Row

```go
var name string
err := client.QueryRow(ctx,
    "SELECT name FROM users WHERE id = $1", 42).Scan(&name)
if errors.Is(err, pgx.ErrNoRows) {
    // Handle no results
    return sserr.NotFoundf("user %d not found", 42)
}
if err != nil {
    return err
}
```

## Executing Statements

```go
// INSERT
tag, err := client.Exec(ctx,
    "INSERT INTO users (name, email) VALUES ($1, $2)",
    "Alice", "alice@example.com")
if err != nil {
    return err
}
fmt.Printf("inserted %d rows\n", tag.RowsAffected())

// UPDATE
tag, err = client.Exec(ctx,
    "UPDATE users SET active = $1 WHERE last_login < $2",
    false, time.Now().Add(-90*24*time.Hour))

// DELETE
tag, err = client.Exec(ctx,
    "DELETE FROM sessions WHERE expired_at < $1", time.Now())
```

## Transactions

Use the `defer tx.Rollback(ctx)` pattern. Rollback on an already-committed
transaction is a no-op in pgx:

```go
tx, err := client.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

_, err = tx.Exec(ctx,
    "INSERT INTO orders (user_id, total) VALUES ($1, $2)", userID, total)
if err != nil {
    return err
}

_, err = tx.Exec(ctx,
    "UPDATE inventory SET quantity = quantity - $1 WHERE product_id = $2",
    quantity, productID)
if err != nil {
    return err
}

return tx.Commit(ctx)
```

## Health Checks

Use `Health()` for readiness and liveness probes:

```go
if err := client.Health(ctx); err != nil {
    // Database is not reachable
    log.Warn("database health check failed", "error", err)
}
```

`Health()` applies a default 5-second timeout if the context has no
deadline. It returns a `CodeUnavailableDependency` error on failure.

### In a Lifecycle Hook

```go
agent, err := lifecycle.NewBaseAgentBuilder(id, name, version).
    WithOnStart(func(ctx context.Context) error {
        return dbClient.Health(ctx) // Verify DB before accepting work
    }).
    Build()
```

### In a Custom Health Check

```go
func (a *MyAgent) Health(ctx context.Context) error {
    if err := a.BaseAgent.Health(ctx); err != nil {
        return err
    }
    return a.dbClient.Health(ctx)
}
```

## Error Handling

The client wraps all errors as platform `*sserr.Error` with appropriate
error codes:

```go
import sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"

rows, err := client.Query(ctx, "SELECT * FROM users")
if err != nil {
    if sserr.IsTimeout(err) {
        // Database query timed out — retryable
        log.Warn("query timed out, retrying...")
    }
    if sserr.IsRetryable(err) {
        // TIMEOUT and UNAVAIL errors are retryable
        return retryWithBackoff(func() error {
            _, err := client.Query(ctx, "SELECT * FROM users")
            return err
        })
    }
    return err
}
```

### Error Code Mapping

| Condition | Error Code | Retryable |
|-----------|-----------|-----------|
| `context.DeadlineExceeded` | `CodeTimeoutDatabase` | Yes |
| `context.Canceled` | `CodeInternalDatabase` | No |
| Other database errors | `CodeInternalDatabase` | No |
| Health check failure | `CodeUnavailableDependency` | Yes |
| Invalid config | `CodeValidation` | No |

## Testing with pgxmock

Use `NewFromPool` to inject a mock pool for unit testing:

```go
import (
    "testing"

    "github.com/pashagolub/pgxmock/v4"
    "github.com/StricklySoft/stricklysoft-core/pkg/clients/postgres"
)

func TestUserQuery(t *testing.T) {
    mock, err := pgxmock.NewPool()
    if err != nil {
        t.Fatal(err)
    }
    defer mock.Close()

    client := postgres.NewFromPool(mock, nil)

    mock.ExpectQuery("SELECT id, name FROM users").
        WithArgs(true).
        WillReturnRows(
            pgxmock.NewRows([]string{"id", "name"}).
                AddRow(1, "Alice").
                AddRow(2, "Bob"),
        )

    rows, err := client.Query(ctx, "SELECT id, name FROM users", true)
    if err != nil {
        t.Fatal(err)
    }
    defer rows.Close()

    // Assert rows...

    if err := mock.ExpectationsWereMet(); err != nil {
        t.Errorf("unmet expectations: %v", err)
    }
}
```

## Cloud Deployments

### AWS RDS

```go
cfg := postgres.DefaultConfig()
cfg.Host = "mydb.xxx.us-east-1.rds.amazonaws.com"
cfg.SSLMode = postgres.SSLModeVerifyFull
cfg.SSLRootCert = "/etc/ssl/certs/rds-ca-2019-root.pem"
cfg.CloudProvider = postgres.CloudProviderAWS
```

### Azure Database for PostgreSQL

```go
cfg := postgres.DefaultConfig()
cfg.Host = "mydb.postgres.database.azure.com"
cfg.User = "myuser@mydb"
cfg.SSLMode = postgres.SSLModeRequire
cfg.CloudProvider = postgres.CloudProviderAzure
```

### GCP Cloud SQL (via Auth Proxy)

```go
cfg := postgres.DefaultConfig()
cfg.Host = "127.0.0.1"
cfg.SSLMode = postgres.SSLModeDisable // Auth Proxy handles encryption
cfg.CloudProvider = postgres.CloudProviderGCP
```

## The Secret Type

The `Secret` type prevents accidental password logging:

```go
password := postgres.Secret("my-database-password")

fmt.Println(password)        // Output: [REDACTED]
fmt.Printf("%s", password)   // Output: [REDACTED]
fmt.Printf("%#v", password)  // Output: [REDACTED]

// Only Value() returns the actual secret
connStr := fmt.Sprintf("password=%s", password.Value())
```

## Related Documentation

- [Database Clients API Reference](../api/clients.md) -- Full API
  documentation
- [Configuration Example](configuration.md) -- Config loading patterns
- [Error Handling API Reference](../api/errors.md) -- Platform error codes
- [Basic Agent Example](basic-agent.md) -- Agent with database health check
