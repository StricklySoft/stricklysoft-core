# Example: Observability Setup

This example demonstrates how to configure OpenTelemetry tracing for a
StricklySoft agent. The SDK's lifecycle and database client packages
automatically create spans when a tracer provider is registered with the
OTel global.

## Prerequisites

Add the OTel SDK dependencies to your module:

```bash
go get go.opentelemetry.io/otel/sdk/trace
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp
go get go.opentelemetry.io/otel/sdk/resource
go get go.opentelemetry.io/otel/semconv/v1.24.0
```

## Full Example

```go
package main

import (
    "context"
    "log"
    "log/slog"
    "os"
    "os/signal"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

    "github.com/StricklySoft/stricklysoft-core/pkg/auth"
    "github.com/StricklySoft/stricklysoft-core/pkg/clients/postgres"
    "github.com/StricklySoft/stricklysoft-core/pkg/config"
    "github.com/StricklySoft/stricklysoft-core/pkg/lifecycle"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()

    // 1. Set up the tracer provider
    tp, err := initTracer(ctx)
    if err != nil {
        log.Fatal(err)
    }
    defer tp.Shutdown(ctx)

    // 2. Create a database client (spans are created automatically)
    dbCfg := postgres.DefaultConfig()
    dbCfg.Password = postgres.Secret(os.Getenv("POSTGRES_PASSWORD"))

    db, err := postgres.NewClient(ctx, *dbCfg)
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // 3. Build an agent with lifecycle tracing
    agent, err := lifecycle.NewBaseAgentBuilder("agent-001", "research-agent", "0.1.0").
        WithLogger(slog.Default()).
        WithOnStart(func(ctx context.Context) error {
            // This database call creates a "postgres.Exec" span
            // nested under the "lifecycle.Start" span.
            _, err := db.Exec(ctx, "INSERT INTO agent_events (agent_id, event) VALUES ($1, $2)",
                "agent-001", "started")
            return err
        }).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    // 4. Start the agent (creates a "lifecycle.Start" span)
    if err := agent.Start(ctx); err != nil {
        log.Fatal(err)
    }

    <-ctx.Done()

    // 5. Stop the agent (creates a "lifecycle.Stop" span)
    agent.Stop(context.Background())
}

func initTracer(ctx context.Context) (*sdktrace.TracerProvider, error) {
    exporter, err := otlptracehttp.New(ctx)
    if err != nil {
        return nil, err
    }

    res, err := resource.Merge(
        resource.Default(),
        resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceNameKey.String("research-agent"),
            semconv.ServiceVersionKey.String("0.1.0"),
        ),
    )
    if err != nil {
        return nil, err
    }

    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithResource(res),
    )

    otel.SetTracerProvider(tp)
    return tp, nil
}
```

## What Gets Traced

With the tracer provider registered, the SDK automatically creates spans
for all instrumented operations:

```
research-agent (service)
└── lifecycle.Start [agent.id=agent-001, agent.name=research-agent]
    └── postgres.Exec [db.system=postgresql, db.name=stricklysoft,
                        db.statement="INSERT INTO agent_events (agent_id, event) VALUES ($1, $2)"]
```

No additional instrumentation code is needed in the application. The
lifecycle and database client packages create spans internally.

### Lifecycle Spans

| Span Name | Attributes |
|-----------|------------|
| `lifecycle.Start` | `agent.id`, `agent.name` |
| `lifecycle.Stop` | `agent.id`, `agent.name` |
| `lifecycle.Pause` | `agent.id`, `agent.name` |
| `lifecycle.Resume` | `agent.id`, `agent.name` |

### Database Spans

| Span Name | Attributes |
|-----------|------------|
| `postgres.Query` | `db.system`, `db.name`, `db.statement` |
| `postgres.QueryRow` | `db.system`, `db.name`, `db.statement` |
| `postgres.Exec` | `db.system`, `db.name`, `db.statement` |
| `postgres.Begin` | `db.system`, `db.name`, `db.statement` |
| `postgres.Health` | `db.system`, `db.name`, `db.statement` |

## Correlating Traces with Identity

Use the auth package to extract trace IDs for audit logging:

```go
func handleRequest(ctx context.Context) {
    identity, _ := auth.IdentityFromContext(ctx)
    traceID, _  := auth.TraceIDFromContext(ctx)

    slog.Info("processing request",
        "user_id", identity.Subject,
        "trace_id", traceID,
    )
}
```

This links application logs to distributed traces, making it possible
to find the trace for a specific user's request.

## OTLP Exporter Configuration

The `otlptracehttp` exporter reads standard OTel environment variables.
Configure these in your Kubernetes deployment:

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://tempo.observability.svc.cluster.local:4318"
  - name: OTEL_SERVICE_NAME
    value: "research-agent"
  - name: OTEL_RESOURCE_ATTRIBUTES
    value: "deployment.environment=production"
```

With these environment variables set, the `initTracer` function can be
simplified since `otlptracehttp.New(ctx)` picks up the endpoint
automatically.

## No Tracer Provider (Default Behavior)

If no tracer provider is configured, the OTel SDK uses a no-op
implementation. All span creation calls succeed but produce no output.
This means SDK packages work correctly without any observability setup
-- tracing is opt-in.

## Running the Example

```bash
# Start a local Jaeger instance for trace collection
docker run -d --name jaeger \
    -p 16686:16686 \
    -p 4318:4318 \
    jaegertracing/all-in-one:latest

# Set the exporter endpoint and run
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export POSTGRES_PASSWORD=secret
go run main.go

# View traces at http://localhost:16686
```

## Next Steps

- [Basic Agent Example](basic-agent.md) -- Agent lifecycle management
- [Database Client Example](database-client.md) -- PostgreSQL operations
- [Configuration Example](configuration.md) -- Configuration loading
- [Observability API Reference](../api/observability.md) -- Span details
  and security considerations
