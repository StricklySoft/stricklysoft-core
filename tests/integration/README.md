# Integration Tests

This directory contains integration tests for the StricklySoft Core SDK.

## Overview

Integration tests verify that SDK components work correctly with real external services
(databases, message queues, etc.) running in containers via testcontainers-go.

## Prerequisites

- Docker installed and running
- Go 1.22 or later

## Running Integration Tests

```bash
# Run all integration tests
make test-integration

# Run integration tests with verbose output
go test -v -tags=integration ./tests/integration/...

# Run specific integration test
go test -v -tags=integration ./tests/integration/postgres/...
```

## Test Structure

```
tests/integration/
├── postgres/       # PostgreSQL client integration tests
├── redis/          # Redis client integration tests
├── qdrant/         # Qdrant vector DB integration tests
├── neo4j/          # Neo4j graph DB integration tests
├── minio/          # MinIO S3 integration tests
└── mongo/          # MongoDB integration tests
```

## Writing Integration Tests

1. Use the `//go:build integration` build tag
2. Use testcontainers-go for container management
3. Implement proper cleanup in test teardown
4. Use test suites for related tests

## Build Tags

Integration tests use the `integration` build tag to separate them from unit tests:

```go
//go:build integration

package integration_test
```

This allows running unit tests quickly without Docker dependencies.
