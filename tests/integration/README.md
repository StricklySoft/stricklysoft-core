# Integration Tests

Integration tests verify that SDK client packages work correctly against
real external services running in Docker containers via
[testcontainers-go](https://golang.testcontainers.org/).

## Prerequisites

- **Docker** installed and running (Docker Engine or Docker Desktop)
- **Go 1.22** or later
- Sufficient memory for container images (~1 GB total for all services)

> **Note:** The CI pipeline (GitHub Actions on `ubuntu-latest`) provides
> Docker automatically. Local Docker is only required when running
> integration tests on your development machine.

## Running Integration Tests

```bash
# Run all integration tests via Makefile
make test-integration

# Run all integration tests directly
go test -v -race -tags=integration ./pkg/... ./tests/...

# Run tests for a specific client
go test -v -race -tags=integration ./pkg/clients/postgres/...
go test -v -race -tags=integration ./pkg/clients/redis/...
go test -v -race -tags=integration ./pkg/clients/minio/...
go test -v -race -tags=integration ./pkg/clients/neo4j/...
go test -v -race -tags=integration ./pkg/clients/qdrant/...
```

The `-tags=integration` flag is required. Without it, the test files are
excluded from compilation because they carry the `//go:build integration`
build constraint.

### Skipping in Short Mode

Integration tests are also skipped when the `-short` flag is passed:

```bash
# This will NOT run integration tests
go test -v -race -short -tags=integration ./pkg/...
```

## Build Tags

All integration test files use the `integration` build tag to separate
them from unit tests:

```go
//go:build integration

package postgres_test
```

This ensures that:

1. Unit tests (`go test ./...`) run without Docker dependencies.
2. Docker-related imports (testcontainers-go) are not compiled into
   unit test binaries.
3. CI can run integration tests in a dedicated job with Docker available.

## Test Locations

Integration tests live alongside the client packages they test, not in
a separate `tests/integration/` tree. This follows Go convention of
colocating tests with source code.

| Client     | Test File                                      | Tests | Status      |
|------------|-------------------------------------------------|-------|-------------|
| PostgreSQL | `pkg/clients/postgres/integration_test.go`     | 21    | Implemented |
| Redis      | `pkg/clients/redis/integration_test.go`        | 21    | Implemented |
| MinIO      | `pkg/clients/minio/integration_test.go`        | 18    | Implemented |
| Neo4j      | `pkg/clients/neo4j/integration_test.go`        | 20    | Implemented |
| Qdrant     | `pkg/clients/qdrant/integration_test.go`       | 18    | Implemented |

## Container Helpers

Reusable container startup functions are provided in
`internal/testutil/containers/` to eliminate boilerplate across test
suites. These helpers are also gated behind `//go:build integration`.

### PostgreSQL

```go
import "github.com/StricklySoft/stricklysoft-core/internal/testutil/containers"

result, err := containers.StartPostgres(ctx)
if err != nil {
    t.Fatal(err)
}
defer result.Container.Terminate(ctx)

cfg := postgres.Config{
    URI:      result.ConnString,
    MaxConns: 10,
}
```

`StartPostgres` starts a `postgres:16-alpine` container with the
following defaults:

| Setting  | Value               |
|----------|---------------------|
| Image    | `postgres:16-alpine`|
| Database | `stricklysoft_test` |
| User     | `testuser`          |
| Password | `testpassword`      |
| SSL Mode | `disable`           |

The returned `PostgresResult` contains:
- `Container` — the testcontainer handle (for termination, log access)
- `ConnString` — a PostgreSQL URI ready for `postgres.Config.URI`

### Redis

```go
result, err := containers.StartRedis(ctx)
if err != nil {
    t.Fatal(err)
}
defer result.Container.Terminate(ctx)

cfg := redis.Config{URI: result.ConnString}
```

`StartRedis` starts a `redis:7-alpine` container:

| Setting  | Value             |
|----------|-------------------|
| Image    | `redis:7-alpine`  |
| Auth     | None (no password)|

The returned `RedisResult` contains:
- `Container` — the testcontainer handle
- `ConnString` — a Redis URI (e.g., `redis://localhost:55679/0`)

### MinIO

```go
result, err := containers.StartMinIO(ctx)
if err != nil {
    t.Fatal(err)
}
defer result.Container.Terminate(ctx)

cfg := minio.Config{
    Endpoint:  result.Endpoint,
    AccessKey: minio.Secret(result.AccessKey),
    SecretKey: minio.Secret(result.SecretKey),
    UseSSL:    false,
}
```

`StartMinIO` starts a `minio/minio:latest` container:

| Setting    | Value               |
|------------|---------------------|
| Image      | `minio/minio:latest`|
| Access Key | `minioadmin`        |
| Secret Key | `minioadmin`        |

The returned `MinIOResult` contains:
- `Container` — the testcontainer handle
- `Endpoint` — the S3-compatible API endpoint (e.g., `localhost:55680`)
- `AccessKey` — root access key
- `SecretKey` — root secret key

### Neo4j

```go
result, err := containers.StartNeo4j(ctx)
if err != nil {
    t.Fatal(err)
}
defer result.Container.Terminate(ctx)

cfg := neo4j.Config{
    URI:      result.BoltURL,
    Username: neo4j.Secret(result.Username),
    Password: neo4j.Secret(result.Password),
}
```

`StartNeo4j` starts a `neo4j:5-community` container:

| Setting  | Value              |
|----------|--------------------|
| Image    | `neo4j:5-community`|
| Username | `neo4j`            |
| Password | `testpassword`     |

The returned `Neo4jResult` contains:
- `Container` — the testcontainer handle
- `BoltURL` — Bolt protocol URL (e.g., `neo4j://localhost:55681`)
- `Username` — admin username
- `Password` — admin password

### Qdrant

```go
result, err := containers.StartQdrant(ctx)
if err != nil {
    t.Fatal(err)
}
defer result.Container.Terminate(ctx)

cfg := qdrant.Config{
    Host:     extractHost(result.GRPCEndpoint),
    GRPCPort: extractPort(result.GRPCEndpoint),
}
```

`StartQdrant` starts a `qdrant/qdrant:latest` container:

| Setting | Value                 |
|---------|-----------------------|
| Image   | `qdrant/qdrant:latest`|
| Auth    | None (no API key)     |

The returned `QdrantResult` contains:
- `Container` — the testcontainer handle
- `GRPCEndpoint` — gRPC API endpoint (e.g., `localhost:55682`)
- `RESTEndpoint` — REST API endpoint (e.g., `localhost:55683`)

## Test Suites

Each client uses a testify `suite.Suite` with a single shared container
for all test methods. This provides fast execution (one container startup
per suite) while isolating test data through unique key/collection/table
names per test.

### Architecture Pattern

```
TestXxxIntegration (entry point)
 └─ XxxIntegrationSuite
     ├── SetupSuite()    → starts 1 container, creates 1 client
     ├── Test methods     → N tests with isolated data
     └── TearDownSuite() → closes client, terminates container
```

### PostgreSQL Test Suite (21 tests)

| Category           | Test Method                          | Validates                                   |
|--------------------|--------------------------------------|---------------------------------------------|
| Connection         | `TestNewClient_ConnectsSuccessfully` | Client creation with real DB                |
| Connection         | `TestHealth_ReturnsNil`              | Health check against live DB                |
| Connection         | `TestNewClient_URIBasedConnection`   | URI-based config connection                 |
| DDL Execution      | `TestExec_CreateTable`               | CREATE TABLE via Exec                       |
| DML Execution      | `TestExec_InsertAndRowsAffected`     | INSERT with RowsAffected verification       |
| Query              | `TestQuery_SelectMultipleRows`       | Multi-row iteration and scanning            |
| Query              | `TestQuery_EmptyResultSet`           | Empty result without error                  |
| QueryRow           | `TestQueryRow_SingleRow`             | Single row scan                             |
| QueryRow           | `TestQueryRow_NoRows`               | pgx.ErrNoRows for missing rows             |
| Transactions       | `TestTransaction_CommitPersistsData` | Data visibility after commit                |
| Transactions       | `TestTransaction_RollbackDiscardsData` | Data discarded after rollback            |
| Transactions       | `TestTransaction_MultipleOperations` | INSERT + UPDATE + SELECT in single tx       |
| Context            | `TestContextTimeout_ReturnsError`    | Expired context fails operations            |
| Close              | `TestClose_ReleasesResources`        | Pool shutdown after Close()                 |
| Error Codes        | `TestErrorCode_TimeoutClassification`| sserr.IsTimeout + IsRetryable               |
| Error Codes        | `TestErrorCode_InvalidSQL`           | sserr.IsInternal for syntax errors          |
| Error Codes        | `TestErrorCode_ConstraintViolation`  | UNIQUE violation + pgconn.PgError unwrap    |
| Data Types         | `TestMultipleDataTypes`              | INTEGER, TEXT, TIMESTAMPTZ, BOOLEAN, JSONB  |
| Null Handling      | `TestNullHandling`                   | NULL columns with pointer scan targets      |
| Concurrency        | `TestConcurrentOperations`           | 10 goroutines concurrent INSERT (pool safe) |
| Pool               | `TestPoolAccessor`                   | Direct pool access via Pool() accessor      |

### Redis Test Suite (21 tests)

| Category           | Test Method                          | Validates                                   |
|--------------------|--------------------------------------|---------------------------------------------|
| Connection         | `TestNewClient_ConnectsSuccessfully` | Client creation with real Redis             |
| Connection         | `TestHealth_ReturnsNil`              | Health check (PING) against live server     |
| String Ops         | `TestSet_And_Get`                    | SET + GET round trip                        |
| String Ops         | `TestGet_NonExistentKey`             | GET returns redis.Nil for missing keys      |
| String Ops         | `TestDel_RemovesKey`                 | DEL removes a key                           |
| String Ops         | `TestExists_ReturnsCount`            | EXISTS returns correct key count            |
| Expiration         | `TestExpire_And_TTL`                 | EXPIRE + TTL round trip                     |
| Counters           | `TestIncr_And_Decr`                  | INCR + DECR atomic counters                 |
| Hash Ops           | `TestHSet_And_HGet`                  | HSET + HGET field access                    |
| Hash Ops           | `TestHGetAll`                        | HGETALL returns all fields                  |
| Hash Ops           | `TestHDel`                           | HDEL removes hash fields                    |
| List Ops           | `TestLPush_And_LRange`              | LPUSH + LRANGE ordered retrieval            |
| List Ops           | `TestRPush_And_LLen`                | RPUSH + LLEN length verification            |
| Set Ops            | `TestSAdd_And_SMembers`             | SADD + SMEMBERS set membership              |
| Set Ops            | `TestSIsMember`                      | SISMEMBER membership check                  |
| Set Ops            | `TestSRem`                           | SREM removes set members                    |
| Context            | `TestContextTimeout_ReturnsError`    | Expired context fails operations            |
| Error Codes        | `TestErrorCode_TimeoutClassification`| sserr.IsTimeout + IsRetryable               |
| Close              | `TestClose_ReleasesResources`        | Client shutdown after Close()               |
| Concurrency        | `TestConcurrentOperations`           | 10 goroutines concurrent SET (pool safe)    |
| Accessor           | `TestClientAccessor`                 | Direct client access via Client() accessor  |

### MinIO Test Suite (18 tests)

| Category           | Test Method                          | Validates                                   |
|--------------------|--------------------------------------|---------------------------------------------|
| Connection         | `TestNewClient_ConnectsSuccessfully` | Client creation with real MinIO             |
| Connection         | `TestHealth_ReturnsNil`              | Health check (BucketExists probe)           |
| Bucket Ops         | `TestMakeBucket_And_BucketExists`    | MakeBucket + BucketExists round trip        |
| Bucket Ops         | `TestRemoveBucket`                   | RemoveBucket cleanup                        |
| Object Ops         | `TestPutObject_And_GetObject`        | PutObject + GetObject round trip            |
| Object Ops         | `TestStatObject`                     | StatObject metadata retrieval               |
| Object Ops         | `TestRemoveObject`                   | RemoveObject deletion                       |
| Object Ops         | `TestListObjects`                    | ListObjects with prefix filtering           |
| Object Ops         | `TestPutObject_LargePayload`         | Large object upload (1 MB)                  |
| Object Ops         | `TestPutObject_ContentType`          | Content-Type preservation                   |
| Presigned URLs     | `TestPresignedGetObject`             | Presigned download URL generation           |
| Presigned URLs     | `TestPresignedPutObject`             | Presigned upload URL generation             |
| Error Handling     | `TestGetObject_NonExistentKey`       | Error for missing objects                   |
| Error Handling     | `TestBucketExists_NonExistent`       | False for missing buckets                   |
| Context            | `TestContextTimeout_ReturnsError`    | Expired context fails operations            |
| Error Codes        | `TestErrorCode_TimeoutClassification`| sserr.IsTimeout + IsRetryable               |
| Close              | `TestClose_IsNoOp`                   | Close is safe no-op (stateless HTTP)        |
| Concurrency        | `TestConcurrentOperations`           | 10 goroutines concurrent PutObject          |

### Neo4j Test Suite (20 tests)

| Category           | Test Method                          | Validates                                   |
|--------------------|--------------------------------------|---------------------------------------------|
| Connection         | `TestNewClient_ConnectsSuccessfully` | Client creation with real Neo4j             |
| Connection         | `TestHealth_ReturnsNil`              | Health check (VerifyConnectivity)           |
| Connection         | `TestNewClient_URIBasedConnection`   | URI-based config connection                 |
| Write Ops          | `TestExecuteWrite_CreateNode`        | CREATE node via managed transaction         |
| Write Ops          | `TestExecuteWrite_UpdateNode`        | SET properties via managed transaction      |
| Write Ops          | `TestExecuteWrite_DeleteNode`        | DELETE node via managed transaction         |
| Write Ops          | `TestExecuteWrite_CreateRelationship`| CREATE relationship between nodes           |
| Read Ops           | `TestExecuteRead_MatchNode`          | MATCH + RETURN single node                  |
| Read Ops           | `TestExecuteRead_TraverseRelationship`| MATCH traversal across relationships       |
| Read Ops           | `TestExecuteRead_EmptyResult`        | Empty result without error                  |
| Read Ops           | `TestExecuteRead_MultipleRecords`    | Multi-record result iteration               |
| Auto-Commit        | `TestRun_AutoCommitQuery`            | Auto-commit query execution                 |
| Data Types         | `TestExecuteWrite_MultipleDataTypes` | String, int, float, bool, list, map props   |
| Raw Access         | `TestSession_RawAccess`              | Direct session access via Session()         |
| Context            | `TestContextTimeout_ReturnsError`    | Expired context fails operations            |
| Error Codes        | `TestErrorCode_TimeoutClassification`| sserr.IsTimeout + IsRetryable               |
| Error Codes        | `TestErrorCode_InvalidCypher`        | sserr.IsInternal for syntax errors          |
| Close              | `TestClose_ReleasesResources`        | Driver shutdown after Close()               |
| Concurrency        | `TestConcurrentOperations`           | 10 goroutines concurrent CREATE (pool safe) |
| Accessor           | `TestDriverAccessor`                 | Direct driver access via Driver() accessor  |

### Qdrant Test Suite (18 tests)

| Category           | Test Method                          | Validates                                   |
|--------------------|--------------------------------------|---------------------------------------------|
| Connection         | `TestNewClient_ConnectsSuccessfully` | Client creation with real Qdrant            |
| Connection         | `TestHealth_ReturnsNil`              | Health check (gRPC health endpoint)         |
| Collection Ops     | `TestCreateCollection_And_CollectionInfo` | CreateCollection + CollectionInfo       |
| Collection Ops     | `TestDeleteCollection`               | DeleteCollection cleanup                    |
| Collection Ops     | `TestListCollections`                | ListCollections enumeration                 |
| Point Ops          | `TestUpsert_Points`                  | Upsert vectors with IDs                     |
| Point Ops          | `TestGet_ByID`                       | Get points by ID                            |
| Point Ops          | `TestDelete_Points`                  | Delete points by ID                         |
| Point Ops          | `TestUpsert_WithPayload`             | Upsert with JSON payload metadata           |
| Search             | `TestSearch_NearestNeighbors`        | Vector similarity search (cosine)           |
| Search             | `TestSearch_WithFilter`              | Filtered vector search with payload match   |
| Search             | `TestSearch_EmptyCollection`         | Search returns empty on no data             |
| Pagination         | `TestScroll_Pagination`              | Scroll through points with limit            |
| Context            | `TestContextTimeout_ReturnsError`    | Expired context fails operations            |
| Error Codes        | `TestErrorCode_TimeoutClassification`| sserr.IsTimeout + IsRetryable               |
| Close              | `TestClose_ReleasesResources`        | gRPC connection shutdown after Close()      |
| Concurrency        | `TestConcurrentOperations`           | 10 goroutines concurrent Upsert             |
| Accessor           | `TestVectorDBAccessor`               | Direct VectorDB access via accessor         |

## CI Configuration

The GitHub Actions CI workflow (`.github/workflows/ci.yaml`) runs
integration tests in a dedicated job with Docker available:

```yaml
go test -v -race -tags=integration ./...
```

No additional CI configuration is required when adding new integration
tests — they are automatically discovered via the build tag.

## Troubleshooting

### Docker not running

```
containers: failed to start postgres container: ...
```

Ensure Docker is running: `docker info` should succeed.

### Port conflicts

Testcontainers uses random ephemeral ports, so port conflicts are rare.
If you see connection errors, verify no firewall rules block outbound
connections to `localhost` on high-numbered ports.

### Slow first run

The first run downloads container images for all services (~1 GB total).
This is cached by Docker for subsequent runs. To pre-pull images:

```bash
docker pull postgres:16-alpine
docker pull redis:7-alpine
docker pull minio/minio:latest
docker pull neo4j:5-community
docker pull qdrant/qdrant:latest
```

### Container cleanup

Containers are terminated in `TearDownSuite`. If a test is interrupted
(e.g., `Ctrl+C`), orphaned containers may remain. Clean them up with:

```bash
docker ps -a --filter "label=org.testcontainers" -q | xargs -r docker rm -f
```
