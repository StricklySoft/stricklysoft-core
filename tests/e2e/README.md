# End-to-End Tests

This directory contains end-to-end (E2E) tests for the StricklySoft Core SDK.

## Overview

E2E tests verify complete workflows and scenarios that span multiple SDK components,
testing the system as a whole rather than individual units.

## Prerequisites

- Docker and Docker Compose installed
- Go 1.22 or later
- Access to test environment (if testing against deployed services)

## Running E2E Tests

```bash
# Run all E2E tests
make test-e2e

# Run E2E tests with verbose output
go test -v -tags=e2e ./tests/e2e/...
```

## Test Scenarios

E2E tests cover complete workflows such as:

- Agent lifecycle (start → health check → graceful shutdown)
- Authentication flow (token validation → identity extraction → authorization)
- Data pipeline (write to DB → read → verify)
- Observability (traces propagated across service boundaries)

## Environment Configuration

E2E tests may require environment variables for connecting to test services:

```bash
export E2E_POSTGRES_HOST=localhost
export E2E_REDIS_HOST=localhost
export E2E_NEXUS_ENDPOINT=localhost:50051
```

## Build Tags

E2E tests use the `e2e` build tag:

```go
//go:build e2e

package e2e_test
```
