//go:build integration

// Package containers provides testcontainers-go helpers for integration
// testing against real database and service containers.
//
// All helpers in this package are gated behind the "integration" build
// tag so they do not pull Docker-related dependencies into unit test
// builds. Use them exclusively from test files that carry the same tag:
//
//	//go:build integration
//
// # PostgreSQL
//
// [StartPostgres] starts a PostgreSQL 16 container and returns a
// [PostgresResult] containing the container handle and a connection
// string ready for use with [postgres.Config.URI]:
//
//	result, err := containers.StartPostgres(ctx)
//	if err != nil { ... }
//	defer result.Container.Terminate(ctx)
//
//	cfg := postgres.Config{URI: result.ConnString, MaxConns: 5}
//
// # Redis
//
// [StartRedis] starts a Redis 7 container and returns a [RedisResult]
// containing the container handle and a connection string (redis://...):
//
//	result, err := containers.StartRedis(ctx)
//	if err != nil { ... }
//	defer result.Container.Terminate(ctx)
//
// # MinIO
//
// [StartMinIO] starts a MinIO container and returns a [MinIOResult]
// containing the container handle, API endpoint, and credentials:
//
//	result, err := containers.StartMinIO(ctx)
//	if err != nil { ... }
//	defer result.Container.Terminate(ctx)
//
// # Neo4j
//
// [StartNeo4j] starts a Neo4j 5 Community container and returns a
// [Neo4jResult] containing the container handle, Bolt URL, and
// credentials:
//
//	result, err := containers.StartNeo4j(ctx)
//	if err != nil { ... }
//	defer result.Container.Terminate(ctx)
//
// # Qdrant
//
// [StartQdrant] starts a Qdrant container and returns a [QdrantResult]
// containing the container handle, gRPC endpoint, and REST endpoint:
//
//	result, err := containers.StartQdrant(ctx)
//	if err != nil { ... }
//	defer result.Container.Terminate(ctx)
//
// # Adding New Helpers
//
// When a new client package is implemented, add a corresponding Start*
// function that returns a *Result struct with the container handle and
// any connection details the client needs.
package containers

import (
	"context"
	"fmt"

	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcqdrant "github.com/testcontainers/testcontainers-go/modules/qdrant"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// ===========================================================================
// PostgreSQL
// ===========================================================================

// DefaultPostgresImage is the container image used for PostgreSQL
// integration tests. Alpine variant is used for minimal image size and
// fast startup time.
const DefaultPostgresImage = "docker.io/postgres:16-alpine"

// DefaultPostgresDatabase is the database name created inside the
// PostgreSQL container for integration tests.
const DefaultPostgresDatabase = "stricklysoft_test"

// DefaultPostgresUser is the superuser name for the PostgreSQL
// container. This account has full DDL/DML privileges for test setup.
const DefaultPostgresUser = "testuser"

// DefaultPostgresPassword is the password for the test superuser.
// This is a deliberately weak credential suitable only for ephemeral
// test containers running on a trusted local network.
const DefaultPostgresPassword = "testpassword"

// PostgresResult holds a started PostgreSQL container and the
// connection string needed to connect to it. The caller is responsible
// for terminating the container when it is no longer needed:
//
//	defer result.Container.Terminate(ctx)
//
// ConnString includes sslmode=disable because testcontainers expose
// PostgreSQL on localhost without TLS.
type PostgresResult struct {
	// Container is the started PostgreSQL testcontainer. Use it to
	// retrieve mapped ports, inspect logs, or terminate the container.
	Container *tcpostgres.PostgresContainer

	// ConnString is a PostgreSQL connection string in URI format
	// (e.g., "postgres://testuser:testpassword@localhost:55432/stricklysoft_test?sslmode=disable").
	// Pass this directly to [postgres.Config.URI].
	ConnString string
}

// StartPostgres starts a PostgreSQL 16 container using testcontainers-go
// and returns a [PostgresResult] containing the container handle and a
// connection string with sslmode=disable.
//
// The container is configured with [DefaultPostgresImage],
// [DefaultPostgresDatabase], [DefaultPostgresUser], and
// [DefaultPostgresPassword]. It uses the postgres module's
// [tcpostgres.BasicWaitStrategies] to wait for the database to become
// ready before returning.
//
// The caller must terminate the container when done:
//
//	result, err := containers.StartPostgres(ctx)
//	if err != nil {
//	    return err
//	}
//	defer result.Container.Terminate(ctx)
//
// StartPostgres returns an error if the container fails to start or if
// the connection string cannot be retrieved. In the latter case, the
// container is terminated before returning.
func StartPostgres(ctx context.Context) (*PostgresResult, error) {
	container, err := tcpostgres.Run(ctx,
		DefaultPostgresImage,
		tcpostgres.WithDatabase(DefaultPostgresDatabase),
		tcpostgres.WithUsername(DefaultPostgresUser),
		tcpostgres.WithPassword(DefaultPostgresPassword),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("containers: failed to start postgres container: %w", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		// Clean up the started container before returning the error.
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("containers: failed to get connection string: %w", err)
	}

	return &PostgresResult{
		Container:  container,
		ConnString: connStr,
	}, nil
}

// ===========================================================================
// Redis
// ===========================================================================

// DefaultRedisImage is the container image used for Redis integration
// tests. Alpine variant is used for minimal image size (~30 MB) and
// fast startup.
const DefaultRedisImage = "docker.io/redis:7-alpine"

// RedisResult holds a started Redis container and the connection string
// needed to connect to it. The caller is responsible for terminating
// the container when it is no longer needed:
//
//	defer result.Container.Terminate(ctx)
//
// ConnString is in Redis URI format (e.g., "redis://localhost:55679/0").
type RedisResult struct {
	// Container is the started Redis testcontainer. Use it to
	// retrieve mapped ports, inspect logs, or terminate the container.
	Container *tcredis.RedisContainer

	// ConnString is a Redis connection string in URI format
	// (e.g., "redis://localhost:55679/0"). Pass this directly
	// to the Redis client configuration.
	ConnString string
}

// StartRedis starts a Redis 7 container using testcontainers-go and
// returns a [RedisResult] containing the container handle and a
// connection string.
//
// The container is configured with [DefaultRedisImage] and no
// authentication (suitable for ephemeral test containers on a trusted
// local network).
//
// The caller must terminate the container when done:
//
//	result, err := containers.StartRedis(ctx)
//	if err != nil {
//	    return err
//	}
//	defer result.Container.Terminate(ctx)
//
// StartRedis returns an error if the container fails to start or if
// the connection string cannot be retrieved. In the latter case, the
// container is terminated before returning.
func StartRedis(ctx context.Context) (*RedisResult, error) {
	container, err := tcredis.Run(ctx, DefaultRedisImage)
	if err != nil {
		return nil, fmt.Errorf("containers: failed to start redis container: %w", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("containers: failed to get redis connection string: %w", err)
	}

	return &RedisResult{
		Container:  container,
		ConnString: connStr,
	}, nil
}

// ===========================================================================
// MinIO
// ===========================================================================

// DefaultMinIOImage is the container image used for MinIO integration
// tests. Uses the official MinIO image for S3-compatible object storage.
const DefaultMinIOImage = "docker.io/minio/minio:latest"

// DefaultMinIOAccessKey is the root access key for the MinIO container.
// This is a deliberately simple credential suitable only for ephemeral
// test containers.
const DefaultMinIOAccessKey = "minioadmin"

// DefaultMinIOSecretKey is the root secret key for the MinIO container.
// This is a deliberately simple credential suitable only for ephemeral
// test containers.
const DefaultMinIOSecretKey = "minioadmin"

// MinIOResult holds a started MinIO container and the connection details
// needed to connect to it. The caller is responsible for terminating
// the container when it is no longer needed:
//
//	defer result.Container.Terminate(ctx)
type MinIOResult struct {
	// Container is the started MinIO testcontainer. Use it to
	// retrieve mapped ports, inspect logs, or terminate the container.
	Container *tcminio.MinioContainer

	// Endpoint is the MinIO API endpoint (e.g., "localhost:55680").
	// Use this with the MinIO client SDK to connect.
	Endpoint string

	// AccessKey is the root access key for the MinIO container.
	AccessKey string

	// SecretKey is the root secret key for the MinIO container.
	SecretKey string
}

// StartMinIO starts a MinIO container using testcontainers-go and
// returns a [MinIOResult] containing the container handle, API endpoint,
// and root credentials.
//
// The container is configured with [DefaultMinIOImage],
// [DefaultMinIOAccessKey], and [DefaultMinIOSecretKey].
//
// The caller must terminate the container when done:
//
//	result, err := containers.StartMinIO(ctx)
//	if err != nil {
//	    return err
//	}
//	defer result.Container.Terminate(ctx)
//
// StartMinIO returns an error if the container fails to start or if
// the connection string cannot be retrieved. In the latter case, the
// container is terminated before returning.
func StartMinIO(ctx context.Context) (*MinIOResult, error) {
	container, err := tcminio.Run(ctx,
		DefaultMinIOImage,
		tcminio.WithUsername(DefaultMinIOAccessKey),
		tcminio.WithPassword(DefaultMinIOSecretKey),
	)
	if err != nil {
		return nil, fmt.Errorf("containers: failed to start minio container: %w", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("containers: failed to get minio connection string: %w", err)
	}

	return &MinIOResult{
		Container: container,
		Endpoint:  connStr,
		AccessKey: DefaultMinIOAccessKey,
		SecretKey: DefaultMinIOSecretKey,
	}, nil
}

// ===========================================================================
// Neo4j
// ===========================================================================

// DefaultNeo4jImage is the container image used for Neo4j integration
// tests. Uses the Community Edition for license-free testing.
const DefaultNeo4jImage = "docker.io/neo4j:5-community"

// DefaultNeo4jPassword is the admin password for the Neo4j container.
// This is a deliberately simple credential suitable only for ephemeral
// test containers.
const DefaultNeo4jPassword = "testpassword"

// DefaultNeo4jUsername is the admin username for the Neo4j container.
// Neo4j Community Edition always uses "neo4j" as the initial username.
const DefaultNeo4jUsername = "neo4j"

// Neo4jResult holds a started Neo4j container and the connection details
// needed to connect to it. The caller is responsible for terminating
// the container when it is no longer needed:
//
//	defer result.Container.Terminate(ctx)
type Neo4jResult struct {
	// Container is the started Neo4j testcontainer. Use it to
	// retrieve mapped ports, inspect logs, or terminate the container.
	Container *tcneo4j.Neo4jContainer

	// BoltURL is the Bolt protocol URL (e.g., "neo4j://localhost:55681").
	// Use this with the Neo4j Go driver to connect.
	BoltURL string

	// Username is the admin username for the Neo4j container.
	Username string

	// Password is the admin password for the Neo4j container.
	Password string
}

// StartNeo4j starts a Neo4j 5 Community Edition container using
// testcontainers-go and returns a [Neo4jResult] containing the
// container handle, Bolt URL, and credentials.
//
// The container is configured with [DefaultNeo4jImage] and
// [DefaultNeo4jPassword]. Authentication is enabled to test
// credential-based connections.
//
// The caller must terminate the container when done:
//
//	result, err := containers.StartNeo4j(ctx)
//	if err != nil {
//	    return err
//	}
//	defer result.Container.Terminate(ctx)
//
// StartNeo4j returns an error if the container fails to start or if
// the Bolt URL cannot be retrieved. In the latter case, the container
// is terminated before returning.
func StartNeo4j(ctx context.Context) (*Neo4jResult, error) {
	container, err := tcneo4j.Run(ctx,
		DefaultNeo4jImage,
		tcneo4j.WithAdminPassword(DefaultNeo4jPassword),
	)
	if err != nil {
		return nil, fmt.Errorf("containers: failed to start neo4j container: %w", err)
	}

	boltURL, err := container.BoltUrl(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("containers: failed to get neo4j bolt URL: %w", err)
	}

	return &Neo4jResult{
		Container: container,
		BoltURL:   boltURL,
		Username:  DefaultNeo4jUsername,
		Password:  DefaultNeo4jPassword,
	}, nil
}

// ===========================================================================
// Qdrant
// ===========================================================================

// DefaultQdrantImage is the container image used for Qdrant integration
// tests. Pinned to v1.12 for compatibility with go-client v1.15.x; the
// client's version parser panics when the server major version exceeds
// the client's by more than one.
const DefaultQdrantImage = "docker.io/qdrant/qdrant:v1.12.6"

// QdrantResult holds a started Qdrant container and the connection
// details needed to connect to it. The caller is responsible for
// terminating the container when it is no longer needed:
//
//	defer result.Container.Terminate(ctx)
type QdrantResult struct {
	// Container is the started Qdrant testcontainer. Use it to
	// retrieve mapped ports, inspect logs, or terminate the container.
	Container *tcqdrant.QdrantContainer

	// GRPCEndpoint is the gRPC API endpoint (e.g., "localhost:55682").
	// Use this with the Qdrant Go client to connect.
	GRPCEndpoint string

	// RESTEndpoint is the REST API endpoint (e.g., "localhost:55683").
	// Use this for HTTP-based health checks or administrative operations.
	RESTEndpoint string
}

// StartQdrant starts a Qdrant container using testcontainers-go and
// returns a [QdrantResult] containing the container handle, gRPC
// endpoint, and REST endpoint.
//
// The container is configured with [DefaultQdrantImage] and no
// authentication (suitable for ephemeral test containers).
//
// The caller must terminate the container when done:
//
//	result, err := containers.StartQdrant(ctx)
//	if err != nil {
//	    return err
//	}
//	defer result.Container.Terminate(ctx)
//
// StartQdrant returns an error if the container fails to start or if
// the endpoints cannot be retrieved. In the latter case, the container
// is terminated before returning.
func StartQdrant(ctx context.Context) (*QdrantResult, error) {
	container, err := tcqdrant.Run(ctx, DefaultQdrantImage)
	if err != nil {
		return nil, fmt.Errorf("containers: failed to start qdrant container: %w", err)
	}

	grpcEndpoint, err := container.GRPCEndpoint(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("containers: failed to get qdrant gRPC endpoint: %w", err)
	}

	restEndpoint, err := container.RESTEndpoint(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("containers: failed to get qdrant REST endpoint: %w", err)
	}

	return &QdrantResult{
		Container:    container,
		GRPCEndpoint: grpcEndpoint,
		RESTEndpoint: restEndpoint,
	}, nil
}
