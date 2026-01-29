# StricklySoft Core SDK

Shared SDK for the StricklySoft Distributed Intelligence Platform.

[![CI](https://github.com/StricklySoft/stricklysoft-core/actions/workflows/ci.yml/badge.svg)](https://github.com/StricklySoft/stricklysoft-core/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/StricklySoft/stricklysoft-core.svg)](https://pkg.go.dev/github.com/StricklySoft/stricklysoft-core)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## Overview

The StricklySoft Core SDK provides shared libraries and utilities for building AI agents on the StricklySoft platform. It includes:

- **Authentication** - Identity management, call-chain propagation, trace correlation
- **Data Models** - Execution records, audit events, policies
- **Database Clients** - PostgreSQL (with Redis, MongoDB, Neo4j, Qdrant, MinIO planned)
- **Observability** - OpenTelemetry tracing across lifecycle and database operations
- **Lifecycle Management** - Agent interface, health checks, graceful shutdown
- **Configuration** - Layered config loader with environment variables, config files, and struct tag defaults

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
    "log/slog"
    "os"
    "os/signal"

    "github.com/StricklySoft/stricklysoft-core/pkg/lifecycle"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()

    agent, err := lifecycle.NewBaseAgentBuilder("agent-001", "my-agent", "0.1.0").
        WithLogger(slog.Default()).
        WithOnStart(func(ctx context.Context) error {
            slog.Info("agent started")
            return nil
        }).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    if err := agent.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer agent.Stop(ctx)

    <-ctx.Done()
}
```

## Documentation

### API Reference

- [Configuration Loader](docs/api/config.md)
- [Lifecycle Management](docs/api/lifecycle.md)
- [Authentication](docs/api/auth.md)
- [Database Clients](docs/api/clients.md)
- [Error Handling](docs/api/errors.md)
- [Data Models](docs/api/models.md)
- [Observability](docs/api/observability.md)

### Guides

- [Best Practices](docs/best-practices.md)
- [Troubleshooting](docs/troubleshooting.md)

### Examples

- [Basic Agent](docs/examples/basic-agent.md)
- [Configuration](docs/examples/configuration.md)
- [Database Client](docs/examples/database-client.md)
- [Observability Setup](docs/examples/observability-setup.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0 - see [LICENSE](LICENSE) for details.
