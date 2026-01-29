# Example Agent

This example demonstrates how to build an agent using the StricklySoft
Core SDK. It showcases configuration loading, lifecycle management,
identity context, execution model creation, error handling, and graceful
shutdown.

## Running the Example

```bash
go run examples/agent/main.go
```

Press `Ctrl+C` to trigger graceful shutdown via `SIGINT`.

## Configuration

Override defaults via environment variables with the `EXAMPLE_` prefix:

```bash
EXAMPLE_AGENT_ID=my-agent-001 \
EXAMPLE_AGENT_NAME=custom-agent \
EXAMPLE_VERSION=2.0.0 \
go run examples/agent/main.go
```

| Variable | Default | Description |
|----------|---------|-------------|
| `EXAMPLE_AGENT_ID` | `example-001` | Unique agent instance ID |
| `EXAMPLE_AGENT_NAME` | `example-agent` | Human-readable agent name |
| `EXAMPLE_VERSION` | `1.0.0` | Agent version |

## What It Demonstrates

- **Configuration** -- `config.MustLoad` with `WithEnvPrefix`
- **Lifecycle** -- `BaseAgentBuilder` with hooks, capabilities, state observers
- **Authentication** -- `BasicIdentity` creation and context propagation
- **Models** -- `NewExecution` with UUID generation and status lifecycle
- **Errors** -- `sserr.New`, `IsRetryable`, `HTTPStatus` mapping
- **Health** -- `agent.Health()` for readiness probes
- **Shutdown** -- Signal handling with `SIGINT`/`SIGTERM`

## Code Structure

- `main.go` -- Agent entry point and initialization

## Related Documentation

- [Basic Agent Example Guide](../../docs/examples/basic-agent.md)
- [Getting Started](../../docs/getting-started.md)
- [API Reference](../../README.md#documentation)
