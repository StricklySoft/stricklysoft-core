# Example: Basic Agent

This example demonstrates building a concrete agent with lifecycle
management, health checks, capabilities, and configuration loading
using the StricklySoft Core SDK.

## Overview

Every agent on the StricklySoft Cloud Platform implements the `Agent`
interface from `pkg/lifecycle`. This example shows how to:

- Define a concrete agent struct that embeds `*lifecycle.BaseAgent`
- Load configuration using the `pkg/config` loader
- Build the agent with `BaseAgentBuilder` (fluent API)
- Register lifecycle hooks (OnStart, OnStop)
- Override `Health()` for deep health checks
- Handle OS signals for graceful shutdown

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "github.com/StricklySoft/stricklysoft-core/pkg/auth"
    "github.com/StricklySoft/stricklysoft-core/pkg/config"
    sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
    "github.com/StricklySoft/stricklysoft-core/pkg/lifecycle"
    "github.com/StricklySoft/stricklysoft-core/pkg/models"
)

// AgentConfig holds configuration loaded from environment variables.
type AgentConfig struct {
    AgentID   string `env:"AGENT_ID" envDefault:"example-001"`
    AgentName string `env:"AGENT_NAME" envDefault:"example-agent"`
    Version   string `env:"VERSION" envDefault:"1.0.0"`
}

func main() {
    // Step 1: Load configuration
    cfg := config.MustLoad[AgentConfig](
        config.New().WithEnvPrefix("EXAMPLE"),
    )

    // Step 2: Set up structured logging
    logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    }))

    // Step 3: Build the agent with lifecycle hooks
    agent, err := lifecycle.NewBaseAgentBuilder(
        cfg.AgentID, cfg.AgentName, cfg.Version,
    ).
        WithCapability(lifecycle.Capability{
            Name:        "example-processing",
            Version:     "1.0.0",
            Description: "Demonstrates SDK lifecycle management",
        }).
        WithLogger(logger).
        WithOnStart(func(ctx context.Context) error {
            logger.InfoContext(ctx, "agent startup hook: initializing resources")
            return nil
        }).
        WithOnStop(func(ctx context.Context) error {
            logger.InfoContext(ctx, "agent shutdown hook: releasing resources")
            return nil
        }).
        OnStateChange(func(old, new lifecycle.State) {
            logger.Info("state transition",
                "from", old.String(),
                "to", new.String(),
            )
        }).
        Build()
    if err != nil {
        logger.Error("failed to build agent", "error", err)
        os.Exit(1)
    }

    // Step 4: Start the agent
    ctx := context.Background()
    if err := agent.Start(ctx); err != nil {
        logger.Error("failed to start agent", "error", err)
        os.Exit(1)
    }

    // Step 5: Demonstrate identity context
    identity := auth.NewBasicIdentity("user-123", auth.IdentityTypeUser,
        map[string]any{"email": "user@example.com"},
    )
    ctx = auth.ContextWithIdentity(ctx, identity)

    // Step 6: Demonstrate execution model
    exec, err := models.NewExecution("user-123", "example task", "default")
    if err != nil {
        logger.Error("failed to create execution", "error", err)
    } else {
        logger.Info("execution created",
            "id", exec.ID,
            "status", exec.Status.String(),
            "terminal", exec.IsTerminal(),
        )
    }

    // Step 7: Demonstrate platform error handling
    dbErr := sserr.New(sserr.CodeUnavailableDependency,
        "database connection lost")
    if sserr.IsRetryable(dbErr) {
        logger.Warn("retryable error",
            "code", dbErr.Code,
            "message", dbErr.Message,
        )
    }

    // Step 8: Health check
    if err := agent.Health(ctx); err != nil {
        logger.Error("health check failed", "error", err)
    }

    // Step 9: Agent info snapshot
    info := agent.Info()
    logger.Info("agent info",
        "id", info.ID,
        "name", info.Name,
        "state", info.State.String(),
        "capabilities", fmt.Sprintf("%d", len(info.Capabilities)),
    )

    // Step 10: Wait for shutdown signal
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    sig := <-sigCh
    logger.Info("received signal", "signal", sig.String())

    // Step 11: Graceful shutdown
    if err := agent.Stop(ctx); err != nil {
        logger.Error("failed to stop agent", "error", err)
        os.Exit(1)
    }
    logger.Info("agent stopped successfully")
}
```

## Running the Example

```bash
cd examples/agent
go run main.go
```

To override configuration via environment variables:

```bash
EXAMPLE_AGENT_ID=my-agent-001 \
EXAMPLE_AGENT_NAME=my-custom-agent \
EXAMPLE_VERSION=2.0.0 \
go run main.go
```

Press `Ctrl+C` to trigger graceful shutdown.

## Key Concepts

### BaseAgentBuilder

The `BaseAgentBuilder` uses a fluent API pattern. Required fields
(`id`, `name`, `version`) are passed to the constructor. Optional
configuration is added via `With*` methods. `Build()` validates all
fields and returns the agent or an error.

### Lifecycle Hooks

Hooks run at specific points in the lifecycle:

| Hook | When | On Error |
|------|------|----------|
| OnStart | After `Starting`, before `Running` | Agent -> `Failed` |
| OnStop | After `Stopping`, before `Stopped` | Agent -> `Failed` |
| OnPause | While `Running`, before `Paused` | Agent -> `Failed` |
| OnResume | While `Paused`, before `Running` | Agent -> `Failed` |

Use hooks to initialize resources (database connections, caches) on
start and release them on stop.

### State Change Observers

State change observers are called synchronously on every state transition.
Use them for metrics, alerts, and registry updates. They must not block
or call lifecycle methods on the same agent.

### Capabilities

Capabilities declare what an agent can do. They are used by the platform
for service discovery and orchestration:

```go
lifecycle.Capability{
    Name:        "model-execution",
    Version:     "1.2.0",
    Description: "Execute LLM inference requests",
    Metadata:    map[string]string{"max_tokens": "8192"},
}
```

### Health Checks

The default `Health()` returns nil if the agent is running. Override it
for deeper checks:

```go
func (a *MyAgent) Health(ctx context.Context) error {
    if err := a.BaseAgent.Health(ctx); err != nil {
        return err
    }
    return a.db.Health(ctx) // Verify database connectivity
}
```

### Graceful Shutdown

The recommended pattern is:

1. Register signal handler for `SIGINT` and `SIGTERM`
2. Block until signal received
3. Call `agent.Stop(ctx)` to run the OnStop hook
4. Exit with code 0 on success

In Kubernetes, the kubelet sends `SIGTERM` on pod termination. The
`terminationGracePeriodSeconds` (default 30s) provides time for the
Stop hook to complete.

## Related Documentation

- [Lifecycle API Reference](../api/lifecycle.md) -- Full state machine
  and API documentation
- [Configuration API Reference](../api/config.md) -- Configuration
  loader details
- [Authentication API Reference](../api/auth.md) -- Identity propagation
- [Error Handling API Reference](../api/errors.md) -- Platform error codes
- [Data Models API Reference](../api/models.md) -- Execution model
- [Configuration Example](configuration.md) -- Config loading patterns
- [Database Client Example](database-client.md) -- PostgreSQL integration
