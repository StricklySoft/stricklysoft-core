# StricklySoft Core SDK Documentation

## API Reference

Detailed reference for each SDK package, including types, functions,
configuration, error codes, and security considerations.

| Document | Package | Description |
|----------|---------|-------------|
| [Authentication](api/auth.md) | `pkg/auth` | Identity management, call-chain propagation, trace correlation |
| [Database Clients](api/clients.md) | `pkg/clients/*` | PostgreSQL client (released), Redis, MongoDB, Neo4j, Qdrant, MinIO (planned) |
| [Configuration](api/config.md) | `pkg/config` | Layered config loader with env vars, files, and struct tag defaults |
| [Error Handling](api/errors.md) | `pkg/errors` | 25 error codes across 8 categories, HTTP mapping, retryability |
| [Lifecycle](api/lifecycle.md) | `pkg/lifecycle` | Agent interface, state machine, builder pattern, hooks |
| [Data Models](api/models.md) | `pkg/models` | Execution records, status lifecycle, audit events |
| [Observability](api/observability.md) | `pkg/observability` | OpenTelemetry tracing, span attributes, tracer provider setup |

## Guides

| Document | Description |
|----------|-------------|
| [Getting Started](getting-started.md) | Installation, quick start, and first agent |
| [Best Practices](best-practices.md) | Error handling, resource cleanup, context propagation, testing |
| [Troubleshooting](troubleshooting.md) | Common errors, diagnostics, Kubernetes-specific issues |

## Examples

Runnable code examples demonstrating common use cases.

| Document | Description |
|----------|-------------|
| [Basic Agent](examples/basic-agent.md) | Agent with lifecycle hooks, capabilities, and health checks |
| [Configuration](examples/configuration.md) | Config loader with env vars, files, and Kubernetes integration |
| [Database Client](examples/database-client.md) | PostgreSQL CRUD, transactions, and unit testing with pgxmock |
| [Observability Setup](examples/observability-setup.md) | OTel tracer provider, OTLP export, and Jaeger integration |

## Runnable Code

The [examples/agent/](../examples/agent/) directory contains a
compilable Go application that demonstrates the SDK's core features.
See its [README](../examples/agent/README.md) for usage.
