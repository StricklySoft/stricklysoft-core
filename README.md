# StricklySoft Core SDK

Shared SDK for the StricklySoft Distributed Intelligence Platform.

[![CI](https://github.com/StricklySoft/stricklysoft-core/actions/workflows/ci.yml/badge.svg)](https://github.com/StricklySoft/stricklysoft-core/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/StricklySoft/stricklysoft-core.svg)](https://pkg.go.dev/github.com/StricklySoft/stricklysoft-core)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## Overview

The StricklySoft Core SDK provides shared libraries and utilities for building AI agents on the StricklySoft platform. It includes:

- **Authentication** - Identity management, JWT validation, RBAC
- **Data Models** - Execution records, audit events, policies
- **Database Clients** - PostgreSQL, Redis, MongoDB, Neo4j, Qdrant, MinIO
- **Observability** - OpenTelemetry tracing, Prometheus metrics, structured logging
- **Lifecycle Management** - Agent interface, health checks, graceful shutdown
- **Configuration** - Environment variables, config files, hot reload
- **Nexus Client** - API Gateway client with streaming support

## Installation

```bash
go get github.com/StricklySoft/stricklysoft-core
```

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/StricklySoft/stricklysoft-core/pkg/lifecycle"
    "github.com/StricklySoft/stricklysoft-core/pkg/observability"
)

func main() {
    ctx := context.Background()

    // Initialize tracing
    tp, err := observability.NewTracerProvider(ctx, &observability.TracerConfig{
        ServiceName: "my-agent",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer tp.Shutdown(ctx)

    // Create and start your agent
    // ...
}
```

## Documentation

- [Getting Started](docs/getting-started.md)
- [Authentication Patterns](docs/authentication.md)
- [Data Models](docs/models.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0 - see [LICENSE](LICENSE) for details.
