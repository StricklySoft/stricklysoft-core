# Getting Started with StricklySoft Core SDK

This guide covers installation and basic usage of the StricklySoft
Core SDK for building AI agents on the platform.

## Installation

```bash
go get github.com/StricklySoft/stricklysoft-core
```

## Quick Start

Create a minimal agent with lifecycle management:

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
        WithOnStop(func(ctx context.Context) error {
            slog.Info("agent stopped")
            return nil
        }).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    if err := agent.Start(ctx); err != nil {
        log.Fatal(err)
    }

    <-ctx.Done()
    agent.Stop(context.Background())
}
```

## Next Steps

### API Reference

- [Configuration Loader](api/config.md) -- Layered config from env vars,
  files, and defaults
- [Lifecycle Management](api/lifecycle.md) -- Agent interface, state
  machine, health checks
- [Authentication](api/auth.md) -- Identity propagation and trace
  correlation
- [Database Clients](api/clients.md) -- PostgreSQL client with OTel
  tracing
- [Error Handling](api/errors.md) -- Platform error codes and
  classification
- [Data Models](api/models.md) -- Execution records and platform types
- [Observability](api/observability.md) -- OpenTelemetry tracing across
  the SDK

### Examples

- [Basic Agent](examples/basic-agent.md) -- Full agent with lifecycle,
  health checks, and capabilities
- [Configuration](examples/configuration.md) -- Config loader patterns
  with Kubernetes integration
- [Database Client](examples/database-client.md) -- PostgreSQL usage,
  transactions, and testing
- [Observability Setup](examples/observability-setup.md) -- OTel tracer
  provider and span export
