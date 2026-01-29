# Database & Storage Clients

This document describes the database and storage clients provided by the
`pkg/clients/` packages. It covers the PostgreSQL client in full detail
and documents the planned clients for Redis, MongoDB, Neo4j, Qdrant,
MinIO, and the shared retry package.

## Client Packages

| Package | Status | Description |
|---------|--------|-------------|
| [`pkg/clients/postgres`](#postgresql-client) | Released | PostgreSQL with pgxpool and OTel tracing |
| [`pkg/clients/redis`](#redis-client-planned) | Planned | Redis with cluster support and OTel tracing |
| [`pkg/clients/mongo`](#mongodb-client-planned) | Planned | MongoDB with connection pooling and OTel tracing |
| [`pkg/clients/neo4j`](#neo4j-client-planned) | Planned | Neo4j graph database with OTel tracing |
| [`pkg/clients/qdrant`](#qdrant-client-planned) | Planned | Qdrant vector database with OTel tracing |
| [`pkg/clients/minio`](#minio-client-planned) | Planned | MinIO S3-compatible object storage with OTel tracing |
| [`pkg/clients/retry`](#retry-package-planned) | Planned | Shared retry logic with exponential backoff |

## Design Principles

All client packages follow these conventions:

1. **Config struct with env tags** -- Each client has a `Config` struct
   with `env` struct tags for environment variable binding and sensible
   defaults via `envDefault` tags.
2. **Secret type for credentials** -- Sensitive fields use a `Secret`
   type (or equivalent) that redacts values in `String()`, `GoString()`,
   and `MarshalText()`.
3. **Pool/interface for testability** -- The main client wraps a pool or
   driver interface, enabling mock implementations for unit tests.
4. **OpenTelemetry tracing** -- All operations create spans with
   appropriate semantic conventions and span kinds.
5. **Platform error codes** -- Errors use `pkg/errors` codes for
   consistent classification and retryability.
6. **Health check method** -- Each client exposes `Health(ctx) error`
   for integration with lifecycle health checks.
7. **Close method** -- Each client exposes `Close()` for resource cleanup.

---

# PostgreSQL Client

The PostgreSQL client is provided by the `pkg/clients/postgres` package.
It provides connection pooling and OpenTelemetry tracing for services
running on the StricklySoft Cloud Platform.

## Overview

Package `postgres` provides a PostgreSQL client with connection pooling
and OpenTelemetry tracing. It is the standard way for platform services
to interact with PostgreSQL databases. The package uses `pgxpool` for
connection management, which handles connection retry for transient
failures internally.

Key design decisions:

- **Connection pooling** -- Built on `pgxpool` for efficient connection
  reuse across concurrent goroutines.
- **Secret safety** -- The `Secret` type prevents accidental logging of
  database passwords via `fmt`, `slog`, and JSON serialization.
- **OpenTelemetry tracing** -- All query operations emit spans with
  database attributes for distributed tracing.
- **Cloud-agnostic** -- Works with on-premise PostgreSQL, AWS RDS, Azure
  Database for PostgreSQL, and GCP Cloud SQL.
- **Kubernetes-native** -- Default configuration targets the in-cluster
  service address `postgres.databases.svc.cluster.local:5432`. Linkerd
  provides mTLS for service-to-service communication.

Import path:

```
"github.com/StricklySoft/stricklysoft-core/pkg/clients/postgres"
```

## Secret Type

`Secret` is a `string` type that redacts its value in all standard
output paths. It prevents database passwords from leaking into logs,
JSON responses, or debug output.

```go
type Secret string
```

### Methods

| Method                          | Returns        | Description                                    |
|---------------------------------|----------------|------------------------------------------------|
| `String() string`              | `"[REDACTED]"` | Satisfies `fmt.Stringer`; always redacted      |
| `GoString() string`            | `"[REDACTED]"` | Satisfies `fmt.GoStringer`; always redacted    |
| `Value() string`               | actual secret  | Returns the underlying password value          |
| `MarshalText() ([]byte, error)`| `"[REDACTED]"` | Satisfies `encoding.TextMarshaler`; redacted   |

### Usage

```go
password := postgres.Secret("s3cret!")

fmt.Println(password)        // [REDACTED]
fmt.Printf("%#v", password)  // [REDACTED]

b, _ := json.Marshal(password)
fmt.Println(string(b))       // "[REDACTED]"

// Only Value() returns the real password
connStr := fmt.Sprintf("postgres://user:%s@host/db", password.Value())
```

Use `Value()` only when constructing connection strings or passing the
password to the database driver. Never pass the return value of
`Value()` to a logger.

## SSLMode

`SSLMode` is a `string` type representing PostgreSQL SSL connection modes.

```go
type SSLMode string
```

### Constants

| Constant             | Value           | Description                                          |
|----------------------|-----------------|------------------------------------------------------|
| `SSLModeDisable`     | `"disable"`     | No SSL; use only with Linkerd mTLS                   |
| `SSLModeAllow`       | `"allow"`       | Attempts SSL, falls back to unencrypted              |
| `SSLModePrefer`      | `"prefer"`      | Attempts SSL first, falls back to unencrypted        |
| `SSLModeRequire`     | `"require"`     | Requires SSL, no certificate verification (default)  |
| `SSLModeVerifyCA`    | `"verify-ca"`   | Requires SSL, verifies certificate chain only        |
| `SSLModeVerifyFull`  | `"verify-full"` | Requires SSL, verifies certificate chain and hostname|

### Methods

| Method            | Returns  | Description                                        |
|-------------------|----------|----------------------------------------------------|
| `String() string` | mode     | Returns the string representation                  |
| `Valid() bool`    | bool     | Returns `true` if the mode is one of the constants |

### Choosing an SSL Mode

- **In-cluster with Linkerd**: `SSLModeDisable` is acceptable because
  Linkerd injects mTLS between all meshed pods.
- **Cloud databases**: `SSLModeVerifyFull` is recommended. It verifies
  both the certificate chain and the hostname, preventing
  man-in-the-middle attacks.
- **Default**: `SSLModeRequire` encrypts the connection but does not
  verify the server certificate. Suitable for environments where
  certificate distribution is not yet configured.

## CloudProvider

`CloudProvider` is a `string` type that identifies the cloud environment
hosting the PostgreSQL instance. It is informational and does not change
client behavior.

```go
type CloudProvider string
```

### Constants

| Constant              | Value      | Description                             |
|-----------------------|------------|-----------------------------------------|
| `CloudProviderNone`   | `""`       | On-premise or self-managed PostgreSQL   |
| `CloudProviderAWS`    | `"aws"`    | AWS RDS                                 |
| `CloudProviderAzure`  | `"azure"`  | Azure Database for PostgreSQL           |
| `CloudProviderGCP`    | `"gcp"`    | GCP Cloud SQL                           |

## Config

`Config` holds all parameters needed to establish a PostgreSQL connection.
It supports two modes: a pre-built connection URI, or structured fields
that are assembled into a URI by `ConnectionString()`.

```go
type Config struct {
    URI               string        `json:"uri,omitempty"       env:"POSTGRES_URI"`
    Host              string        `json:"host,omitempty"      env:"POSTGRES_HOST"`
    Port              int           `json:"port,omitempty"      env:"POSTGRES_PORT"`
    Database          string        `json:"database"            env:"POSTGRES_DATABASE"`
    User              string        `json:"user"                env:"POSTGRES_USER"`
    Password          Secret        `json:"-"                   env:"POSTGRES_PASSWORD"`
    SSLMode           SSLMode       `json:"ssl_mode,omitempty"  env:"POSTGRES_SSLMODE"`
    SSLRootCert       string        `json:"ssl_root_cert,omitempty" env:"POSTGRES_SSL_ROOT_CERT"`
    MaxConns          int32         `json:"max_conns,omitempty" env:"POSTGRES_MAX_CONNS"`
    MinConns          int32         `json:"min_conns,omitempty" env:"POSTGRES_MIN_CONNS"`
    MaxConnLifetime   time.Duration `json:"max_conn_lifetime,omitempty" env:"POSTGRES_MAX_CONN_LIFETIME"`
    MaxConnIdleTime   time.Duration `json:"max_conn_idle_time,omitempty" env:"POSTGRES_MAX_CONN_IDLE_TIME"`
    HealthCheckPeriod time.Duration `json:"health_check_period,omitempty" env:"POSTGRES_HEALTH_CHECK_PERIOD"`
    ConnectTimeout    time.Duration `json:"connect_timeout,omitempty" env:"POSTGRES_CONNECT_TIMEOUT"`
    CloudProvider     CloudProvider `json:"cloud_provider,omitempty" env:"POSTGRES_CLOUD_PROVIDER"`
}
```

### Config Fields

| Field               | Type            | Env Var                        | Description                                    |
|---------------------|-----------------|--------------------------------|------------------------------------------------|
| `URI`               | `string`        | `POSTGRES_URI`                 | Pre-built connection URI; overrides all structured fields |
| `Host`              | `string`        | `POSTGRES_HOST`                | PostgreSQL server hostname or IP address       |
| `Port`              | `int`           | `POSTGRES_PORT`                | PostgreSQL server port                         |
| `Database`          | `string`        | `POSTGRES_DATABASE`            | Target database name                           |
| `User`              | `string`        | `POSTGRES_USER`                | Authentication username                        |
| `Password`          | `Secret`        | `POSTGRES_PASSWORD`            | Authentication password (redacted in output)   |
| `SSLMode`           | `SSLMode`       | `POSTGRES_SSLMODE`             | TLS connection mode                            |
| `SSLRootCert`       | `string`        | `POSTGRES_SSL_ROOT_CERT`       | Path to CA certificate file for TLS verification |
| `MaxConns`          | `int32`         | `POSTGRES_MAX_CONNS`           | Maximum number of connections in the pool      |
| `MinConns`          | `int32`         | `POSTGRES_MIN_CONNS`           | Minimum idle connections maintained in the pool|
| `MaxConnLifetime`   | `time.Duration` | `POSTGRES_MAX_CONN_LIFETIME`   | Maximum lifetime of a connection before recycling |
| `MaxConnIdleTime`   | `time.Duration` | `POSTGRES_MAX_CONN_IDLE_TIME`  | Maximum time a connection may sit idle         |
| `HealthCheckPeriod` | `time.Duration` | `POSTGRES_HEALTH_CHECK_PERIOD` | Interval between pool health checks            |
| `ConnectTimeout`    | `time.Duration` | `POSTGRES_CONNECT_TIMEOUT`     | Timeout for establishing new connections       |
| `CloudProvider`     | `CloudProvider` | `POSTGRES_CLOUD_PROVIDER`      | Cloud provider hosting the database            |

### DefaultConfig()

Returns a `*Config` populated with platform defaults suitable for
in-cluster Kubernetes deployments:

```go
func DefaultConfig() *Config
```

| Field               | Default Value                                |
|---------------------|----------------------------------------------|
| `Host`              | `postgres.databases.svc.cluster.local`       |
| `Port`              | `5432`                                       |
| `Database`          | `stricklysoft`                               |
| `User`              | `postgres`                                   |
| `SSLMode`           | `SSLModeRequire`                             |
| `MaxConns`          | `25`                                         |
| `MinConns`          | `5`                                          |
| `MaxConnLifetime`   | `1h`                                         |
| `MaxConnIdleTime`   | `30m`                                        |
| `HealthCheckPeriod` | `1m`                                         |
| `ConnectTimeout`    | `10s`                                        |

Usage:

```go
cfg := postgres.DefaultConfig()
cfg.Password = postgres.Secret(os.Getenv("POSTGRES_PASSWORD"))
```

### Validate() error

Validates the configuration and applies pool defaults for any
zero-valued pool fields. When `URI` is set, structured fields
(`Host`, `Port`, `Database`, `User`, `SSLMode`) are skipped during
validation because the URI is used directly.

```go
func (c *Config) Validate() error
```

Validation rules:

| Rule                              | Error Condition                              |
|-----------------------------------|----------------------------------------------|
| `Database` is required            | Empty string                                 |
| `User` is required                | Empty string                                 |
| `Port` must be 1--65535           | Out of range                                 |
| `SSLMode` must be valid           | `Valid()` returns `false`                    |
| `SSLRootCert` must be readable    | File does not exist or is not accessible     |
| `MaxConns` must be >= 1           | Less than 1                                  |
| `MinConns` must be >= 0           | Negative value                               |
| `MaxConns` must be >= `MinConns`  | `MaxConns` < `MinConns`                      |
| Durations must not be negative    | Any duration field is negative               |

All validation errors use `CodeValidation`.

### ConnectionString() string

Builds a `postgres://` connection URI from the structured fields. If
`URI` is set, it is returned directly without modification.

```go
func (c *Config) ConnectionString() string
```

**Security note**: The returned string contains the cleartext password.
Do not log or persist the return value.

## Pool Interface

`Pool` defines the subset of `pgxpool.Pool` methods used by the client.
It enables testing with mock implementations (e.g., `pgxmock`).

```go
type Pool interface {
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
    Begin(ctx context.Context) (pgx.Tx, error)
    Ping(ctx context.Context) error
    Close()
}
```

This interface is satisfied by `*pgxpool.Pool` (production) and
`pgxmock.PgxPoolIface` (testing).

## Client

`Client` wraps a connection pool with OpenTelemetry tracing and
platform-standard error handling. All query methods create spans and
classify errors using the platform error codes.

```go
type Client struct {
    // unexported fields
}
```

### Construction

#### NewClient

Creates a new client by validating the config, creating a connection
pool, configuring TLS, and pinging the database to verify connectivity.

```go
func NewClient(ctx context.Context, cfg Config) (*Client, error)
```

Error codes returned:

| Code                          | Cause                                         |
|-------------------------------|-----------------------------------------------|
| `CodeValidation`              | Config validation failed                      |
| `CodeInternalConfiguration`   | Pool configuration parsing or TLS setup failed|
| `CodeUnavailableDependency`   | Database ping failed (server unreachable)     |
| `CodeInternalDatabase`        | Unexpected database error during setup        |

Usage:

```go
cfg := postgres.DefaultConfig()
cfg.Password = postgres.Secret(os.Getenv("POSTGRES_PASSWORD"))

client, err := postgres.NewClient(ctx, *cfg)
if err != nil {
    return fmt.Errorf("postgres client: %w", err)
}
defer client.Close()
```

#### NewFromPool

Creates a client from an existing `Pool` implementation. Intended for
unit testing with mock pools. The `cfg` parameter may be `nil`.

```go
func NewFromPool(pool Pool, cfg *Config) *Client
```

Usage:

```go
mock, err := pgxmock.NewPool()
if err != nil {
    t.Fatal(err)
}
defer mock.Close()

client := postgres.NewFromPool(mock, nil)
```

### Query Operations

All query operations create OpenTelemetry spans and classify errors
using platform error codes.

#### Query

Executes a query that returns rows. The caller must close the returned
`pgx.Rows` when done.

```go
func (c *Client) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
```

Error codes: `CodeTimeoutDatabase` (deadline exceeded) or
`CodeInternalDatabase` (all other errors).

Usage:

```go
rows, err := client.Query(ctx, "SELECT id, name FROM agents WHERE status = $1", "active")
if err != nil {
    return err
}
defer rows.Close()

for rows.Next() {
    var id, name string
    if err := rows.Scan(&id, &name); err != nil {
        return err
    }
    // process row
}
```

#### QueryRow

Executes a query that returns at most one row. Errors are deferred
until `Scan()` is called on the returned `pgx.Row`.

```go
func (c *Client) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
```

Usage:

```go
var count int
err := client.QueryRow(ctx, "SELECT COUNT(*) FROM executions").Scan(&count)
if err != nil {
    return err
}
```

#### Exec

Executes a query that does not return rows (INSERT, UPDATE, DELETE).
Returns a `pgconn.CommandTag` indicating the number of affected rows.

```go
func (c *Client) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
```

Error codes: `CodeTimeoutDatabase` (deadline exceeded) or
`CodeInternalDatabase` (all other errors).

Usage:

```go
tag, err := client.Exec(ctx, "DELETE FROM sessions WHERE expires_at < $1", time.Now())
if err != nil {
    return err
}
fmt.Printf("deleted %d rows\n", tag.RowsAffected())
```

#### Begin

Starts a database transaction. The caller should use the
`defer tx.Rollback(ctx)` pattern to ensure the transaction is rolled
back if an error occurs before `Commit()`.

```go
func (c *Client) Begin(ctx context.Context) (pgx.Tx, error)
```

Usage:

```go
tx, err := client.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

_, err = tx.Exec(ctx, "INSERT INTO events (type, payload) VALUES ($1, $2)", eventType, payload)
if err != nil {
    return err
}

return tx.Commit(ctx)
```

### Health

Pings the database to verify connectivity. If the provided context has
no deadline, a default timeout of 5 seconds (`DefaultHealthTimeout`)
is applied.

```go
func (c *Client) Health(ctx context.Context) error
```

Error code: `CodeUnavailableDependency` on failure.

Usage with a lifecycle agent:

```go
func (ra *ResearchAgent) Health(ctx context.Context) error {
    if err := ra.BaseAgent.Health(ctx); err != nil {
        return err
    }
    return ra.db.Health(ctx)
}
```

### Close

Releases all pool resources. Safe to call multiple times.

```go
func (c *Client) Close()
```

### Pool

Returns the underlying `Pool` for advanced operations such as
`CopyFrom` or `SendBatch` that are not exposed on `Client` directly.

```go
func (c *Client) Pool() Pool
```

Usage:

```go
pool := client.Pool()
// Use pool.(*pgxpool.Pool) for CopyFrom, SendBatch, etc.
```

### Client Methods Summary

| Method                                                         | Returns                         | Description                           |
|----------------------------------------------------------------|---------------------------------|---------------------------------------|
| `NewClient(ctx, cfg)`                                          | `(*Client, error)`              | Create client with validated config   |
| `NewFromPool(pool, cfg)`                                       | `*Client`                       | Create client from mock pool          |
| `Query(ctx, sql, args...)`                                     | `(pgx.Rows, error)`            | Execute query returning rows          |
| `QueryRow(ctx, sql, args...)`                                  | `pgx.Row`                       | Execute query returning single row    |
| `Exec(ctx, sql, args...)`                                      | `(pgconn.CommandTag, error)`    | Execute non-returning query           |
| `Begin(ctx)`                                                   | `(pgx.Tx, error)`               | Start a transaction                   |
| `Health(ctx)`                                                  | `error`                          | Ping database for health check        |
| `Close()`                                                      | --                               | Release pool resources                |
| `Pool()`                                                       | `Pool`                           | Access underlying connection pool     |

## OpenTelemetry Tracing

All query operations (`Query`, `QueryRow`, `Exec`, `Begin`) create
OpenTelemetry spans under the instrumentation scope:

```
github.com/StricklySoft/stricklysoft-core/pkg/clients/postgres
```

### Span Attributes

| Attribute        | Value                                             |
|------------------|---------------------------------------------------|
| `db.system`      | `"postgresql"`                                    |
| `db.name`        | Database name from config                         |
| `db.statement`   | SQL statement, truncated to 100 characters        |

### Span Configuration

- **Span kind**: `Client`
- **Error recording**: Errors are recorded on the span and the span
  status is set to `codes.Error`.
- **Statement truncation**: SQL statements longer than 100 characters
  are truncated in the `db.statement` attribute to prevent sensitive
  data from leaking into trace backends.

## Error Handling

All errors use the platform error package (`pkg/errors`). Error
classification follows a consistent pattern across all client methods.

### Error Classification

| Condition                       | Error Code                   | Retryable |
|---------------------------------|------------------------------|-----------|
| `context.DeadlineExceeded`      | `CodeTimeoutDatabase`        | Yes       |
| `context.Canceled`              | `CodeInternalDatabase`       | No        |
| Other query/exec errors         | `CodeInternalDatabase`       | No        |
| Health check failure            | `CodeUnavailableDependency`  | Yes       |
| Config validation failure       | `CodeValidation`             | No        |
| Pool configuration failure      | `CodeInternalConfiguration`  | No        |
| Database unreachable at startup | `CodeUnavailableDependency`  | Yes       |

### Retryability

Errors with `CodeTimeoutDatabase` and `CodeUnavailableDependency` are
marked as retryable via the platform error's `IsRetryable()` method.
Callers can use this to implement retry logic:

```go
rows, err := client.Query(ctx, "SELECT ...")
if err != nil {
    if sserr.IsRetryable(err) {
        // safe to retry with backoff
    }
    return err
}
```

## Cloud Provider Examples

### AWS RDS

```go
cfg := postgres.DefaultConfig()
cfg.Host = "mydb.xxx.us-east-1.rds.amazonaws.com"
cfg.Database = "myapp"
cfg.User = "myuser"
cfg.Password = postgres.Secret(os.Getenv("POSTGRES_PASSWORD"))
cfg.SSLMode = postgres.SSLModeVerifyFull
cfg.SSLRootCert = "/etc/ssl/certs/rds-ca-2019-root.pem"
cfg.CloudProvider = postgres.CloudProviderAWS

client, err := postgres.NewClient(ctx, *cfg)
```

### Azure Database for PostgreSQL

```go
cfg := postgres.DefaultConfig()
cfg.Host = "mydb.postgres.database.azure.com"
cfg.Database = "myapp"
cfg.User = "myuser@mydb"
cfg.Password = postgres.Secret(os.Getenv("POSTGRES_PASSWORD"))
cfg.SSLMode = postgres.SSLModeRequire
cfg.CloudProvider = postgres.CloudProviderAzure

client, err := postgres.NewClient(ctx, *cfg)
```

### GCP Cloud SQL (via Auth Proxy)

When using the Cloud SQL Auth Proxy, the proxy handles authentication
and encryption. Connect to localhost with SSL disabled:

```go
cfg := postgres.DefaultConfig()
cfg.Host = "127.0.0.1"
cfg.Database = "myapp"
cfg.User = "myuser"
cfg.Password = postgres.Secret(os.Getenv("POSTGRES_PASSWORD"))
cfg.SSLMode = postgres.SSLModeDisable
cfg.CloudProvider = postgres.CloudProviderGCP

client, err := postgres.NewClient(ctx, *cfg)
```

### In-Cluster Kubernetes (Default)

For services running in the same Kubernetes cluster as PostgreSQL,
the default configuration targets the cluster-internal service:

```go
cfg := postgres.DefaultConfig()
cfg.Password = postgres.Secret(os.Getenv("POSTGRES_PASSWORD"))

client, err := postgres.NewClient(ctx, *cfg)
```

Linkerd provides mTLS between the service pod and the PostgreSQL pod,
so `SSLModeRequire` (the default) provides defense in depth.

## Security Considerations

1. **Secret type prevents accidental password logging** --- The
   `Secret` type implements `fmt.Stringer`, `fmt.GoStringer`, and
   `encoding.TextMarshaler` to return `"[REDACTED]"` in all standard
   output paths. The `Password` field uses `json:"-"` to exclude it
   from JSON serialization entirely.

2. **SSLModeVerifyFull recommended for cloud databases** --- When
   connecting to managed PostgreSQL services over the public internet
   or across VPC boundaries, use `SSLModeVerifyFull` with the
   provider's CA certificate to prevent man-in-the-middle attacks.

3. **SQL truncation in OTel spans prevents data leakage** --- SQL
   statements are truncated to 100 characters in the `db.statement`
   span attribute. This prevents sensitive data embedded in queries
   (such as user data in INSERT statements) from being stored in
   trace backends.

4. **TLS 1.2 minimum version enforced** --- When TLS is enabled, the
   client enforces TLS 1.2 as the minimum protocol version, rejecting
   connections that attempt to negotiate older, less secure protocols.

5. **ConnectionString() returns cleartext password** --- The return
   value of `ConnectionString()` contains the password in cleartext.
   Never log, persist, or expose this value. Use it only for passing
   to the connection pool constructor.

6. **External Secrets Operator for Kubernetes credential injection** ---
   In production Kubernetes deployments, use the External Secrets
   Operator to inject database credentials as environment variables
   or mounted secrets. Avoid hardcoding passwords in configuration
   files or container images.

## PostgreSQL File Structure

```
pkg/clients/postgres/
    client.go             Client, Pool interface, NewClient, NewFromPool, operations
    config.go             Config, SSLMode, CloudProvider, Secret, DefaultConfig, Validate
    client_test.go        Unit tests with pgxmock
    config_test.go        Config validation and connection string tests
    integration_test.go   Integration tests with testcontainers
```

---

# Redis Client (Planned)

Package `redis` will provide a Redis client with cluster support and
OpenTelemetry tracing.

```
"github.com/StricklySoft/stricklysoft-core/pkg/clients/redis"
```

## Planned Features

- Connection to standalone Redis and Redis Cluster deployments
- OpenTelemetry tracing for all operations with `db.system=redis`
  semantic attributes
- `Config` struct with environment variable binding (`REDIS_HOST`,
  `REDIS_PORT`, `REDIS_PASSWORD`, etc.)
- `Secret` type for password redaction
- Connection pooling with configurable pool size
- `Health(ctx) error` for integration with lifecycle health checks
- TLS support for managed Redis services (AWS ElastiCache, Azure Cache
  for Redis, GCP Memorystore)
- Platform error code classification with retryable error detection

## Planned API Surface

```go
type Config struct {
    Host     string        `env:"REDIS_HOST" envDefault:"redis.databases.svc.cluster.local"`
    Port     int           `env:"REDIS_PORT" envDefault:"6379"`
    Password Secret        `env:"REDIS_PASSWORD"`
    DB       int           `env:"REDIS_DB" envDefault:"0"`
    // ... pool and TLS configuration
}

type Client struct { /* unexported */ }

func NewClient(ctx context.Context, cfg Config) (*Client, error)
func (c *Client) Health(ctx context.Context) error
func (c *Client) Close() error
```

## Current Status

The package contains placeholder files only:

```
pkg/clients/redis/
    client.go    Package declaration only
    config.go    Package declaration only
```

---

# MongoDB Client (Planned)

Package `mongo` will provide a MongoDB client with connection pooling
and OpenTelemetry tracing.

```
"github.com/StricklySoft/stricklysoft-core/pkg/clients/mongo"
```

## Planned Features

- MongoDB driver integration with the official `go.mongodb.org/mongo-driver`
- OpenTelemetry tracing for all operations with `db.system=mongodb`
  semantic attributes
- `Config` struct with environment variable binding (`MONGO_URI`,
  `MONGO_HOST`, `MONGO_DATABASE`, etc.)
- Connection pooling with configurable pool size and timeouts
- `Health(ctx) error` for integration with lifecycle health checks
- TLS support for managed MongoDB services (MongoDB Atlas, AWS
  DocumentDB, Azure Cosmos DB)
- Platform error code classification with retryable error detection

## Planned API Surface

```go
type Config struct {
    URI      string        `env:"MONGO_URI"`
    Host     string        `env:"MONGO_HOST" envDefault:"mongo.databases.svc.cluster.local"`
    Port     int           `env:"MONGO_PORT" envDefault:"27017"`
    Database string        `env:"MONGO_DATABASE"`
    User     string        `env:"MONGO_USER"`
    Password Secret        `env:"MONGO_PASSWORD"`
    // ... pool and TLS configuration
}

type Client struct { /* unexported */ }

func NewClient(ctx context.Context, cfg Config) (*Client, error)
func (c *Client) Database(name string) *mongo.Database
func (c *Client) Health(ctx context.Context) error
func (c *Client) Close(ctx context.Context) error
```

## Current Status

The package contains placeholder files only:

```
pkg/clients/mongo/
    client.go    Package declaration only
    config.go    Package declaration only
```

---

# Neo4j Client (Planned)

Package `neo4j` will provide a Neo4j graph database client with
OpenTelemetry tracing.

```
"github.com/StricklySoft/stricklysoft-core/pkg/clients/neo4j"
```

## Planned Features

- Neo4j driver integration with the official `github.com/neo4j/neo4j-go-driver`
- OpenTelemetry tracing for all operations with `db.system=neo4j`
  semantic attributes
- `Config` struct with environment variable binding (`NEO4J_URI`,
  `NEO4J_HOST`, `NEO4J_USER`, etc.)
- Session management with configurable access modes (read/write)
- `Health(ctx) error` for integration with lifecycle health checks
- TLS support for Neo4j Aura and self-managed deployments
- Cypher query execution with parameterized queries
- Platform error code classification with retryable error detection

## Planned API Surface

```go
type Config struct {
    URI      string `env:"NEO4J_URI" envDefault:"bolt://neo4j.databases.svc.cluster.local:7687"`
    User     string `env:"NEO4J_USER" envDefault:"neo4j"`
    Password Secret `env:"NEO4J_PASSWORD"`
    // ... TLS and session configuration
}

type Client struct { /* unexported */ }

func NewClient(ctx context.Context, cfg Config) (*Client, error)
func (c *Client) Session(ctx context.Context) neo4j.SessionWithContext
func (c *Client) Health(ctx context.Context) error
func (c *Client) Close(ctx context.Context) error
```

## Current Status

The package contains placeholder files only:

```
pkg/clients/neo4j/
    client.go    Package declaration only
    config.go    Package declaration only
```

---

# Qdrant Client (Planned)

Package `qdrant` will provide a Qdrant vector database client with
OpenTelemetry tracing.

```
"github.com/StricklySoft/stricklysoft-core/pkg/clients/qdrant"
```

## Planned Features

- Qdrant gRPC client integration for vector search operations
- OpenTelemetry tracing for all operations with `db.system=qdrant`
  semantic attributes
- `Config` struct with environment variable binding (`QDRANT_HOST`,
  `QDRANT_PORT`, `QDRANT_API_KEY`, etc.)
- Collection management (create, delete, list)
- Point operations (upsert, search, scroll, delete)
- Filtered vector search with payload conditions
- `Health(ctx) error` for integration with lifecycle health checks
- TLS support for Qdrant Cloud deployments
- Platform error code classification with retryable error detection

## Planned API Surface

```go
type Config struct {
    Host   string `env:"QDRANT_HOST" envDefault:"qdrant.databases.svc.cluster.local"`
    Port   int    `env:"QDRANT_PORT" envDefault:"6334"`
    APIKey Secret `env:"QDRANT_API_KEY"`
    UseTLS bool   `env:"QDRANT_USE_TLS" envDefault:"false"`
    // ... timeout configuration
}

type Client struct { /* unexported */ }

func NewClient(ctx context.Context, cfg Config) (*Client, error)
func (c *Client) Health(ctx context.Context) error
func (c *Client) Close() error
```

## Current Status

The package contains placeholder files only:

```
pkg/clients/qdrant/
    client.go    Package declaration only
    config.go    Package declaration only
```

---

# MinIO Client (Planned)

Package `minio` will provide a MinIO S3-compatible object storage client
with OpenTelemetry tracing.

```
"github.com/StricklySoft/stricklysoft-core/pkg/clients/minio"
```

## Planned Features

- MinIO client integration with the official `github.com/minio/minio-go/v7`
- OpenTelemetry tracing for all operations
- `Config` struct with environment variable binding (`MINIO_ENDPOINT`,
  `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, etc.)
- Bucket operations (create, list, exists)
- Object operations (put, get, remove, list, stat)
- Presigned URL generation for temporary access
- `Health(ctx) error` for integration with lifecycle health checks
- TLS support
- Platform error code classification with retryable error detection

## Planned API Surface

```go
type Config struct {
    Endpoint  string `env:"MINIO_ENDPOINT" envDefault:"minio.storage.svc.cluster.local:9000"`
    AccessKey Secret `env:"MINIO_ACCESS_KEY"`
    SecretKey Secret `env:"MINIO_SECRET_KEY"`
    UseTLS    bool   `env:"MINIO_USE_TLS" envDefault:"false"`
    Region    string `env:"MINIO_REGION" envDefault:"us-east-1"`
    // ... timeout configuration
}

type Client struct { /* unexported */ }

func NewClient(ctx context.Context, cfg Config) (*Client, error)
func (c *Client) Health(ctx context.Context) error
func (c *Client) Close()
```

## Current Status

The package contains placeholder files only:

```
pkg/clients/minio/
    client.go    Package declaration only
    config.go    Package declaration only
```

---

# Retry Package (Planned)

Package `retry` will provide shared retry logic with exponential backoff
for all client packages.

```
"github.com/StricklySoft/stricklysoft-core/pkg/clients/retry"
```

## Planned Features

- Exponential backoff with configurable base delay, max delay, and
  max attempts
- Jitter (full and decorrelated) to prevent thundering herd
- Context-aware cancellation (respects `ctx.Done()`)
- Integration with platform error codes (`IsRetryable()`) to
  automatically determine whether an operation should be retried
- OpenTelemetry span events for retry attempts
- Configurable retry predicate for custom retry decisions

## Planned API Surface

```go
type Config struct {
    MaxAttempts uint          `env:"RETRY_MAX_ATTEMPTS" envDefault:"3"`
    BaseDelay   time.Duration `env:"RETRY_BASE_DELAY" envDefault:"100ms"`
    MaxDelay    time.Duration `env:"RETRY_MAX_DELAY" envDefault:"10s"`
    Jitter      bool          `env:"RETRY_JITTER" envDefault:"true"`
}

func Do(ctx context.Context, cfg Config, fn func(ctx context.Context) error) error
func DoWithResult[T any](ctx context.Context, cfg Config, fn func(ctx context.Context) (T, error)) (T, error)
```

## Current Status

The package contains a placeholder file only:

```
pkg/clients/retry/
    backoff.go    Package declaration only
```

---

## File Structure

```
pkg/clients/
    postgres/
        client.go             Client, Pool interface, NewClient, NewFromPool, operations
        config.go             Config, SSLMode, CloudProvider, Secret, DefaultConfig, Validate
        client_test.go        Unit tests with pgxmock
        config_test.go        Config validation and connection string tests
        integration_test.go   Integration tests with testcontainers
    redis/
        client.go             Planned
        config.go             Planned
    mongo/
        client.go             Planned
        config.go             Planned
    neo4j/
        client.go             Planned
        config.go             Planned
    qdrant/
        client.go             Planned
        config.go             Planned
    minio/
        client.go             Planned
        config.go             Planned
    retry/
        backoff.go            Planned
```
