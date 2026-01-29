# Observability

This document describes the observability support provided by the
StricklySoft Core SDK. It covers the OpenTelemetry tracing that is
integrated into the lifecycle and database client packages, the trace
context extraction utilities in the auth package, and the planned
unified observability package.

## Overview

The SDK provides distributed tracing through OpenTelemetry (OTel). Rather
than a centralized observability package, tracing is embedded directly in
the packages that perform traced operations. Each package creates its own
tracer using `otel.Tracer()` and follows OTel semantic conventions for
span naming and attributes.

The SDK uses OpenTelemetry v1.26.0:

```
go.opentelemetry.io/otel v1.26.0
go.opentelemetry.io/otel/trace v1.26.0
```

### Traced Packages

| Package | Instrumentation Scope | Span Kind | Operations |
|---------|----------------------|-----------|------------|
| `pkg/lifecycle` | `github.com/StricklySoft/stricklysoft-core/pkg/lifecycle` | Internal | Start, Stop, Pause, Resume |
| `pkg/clients/postgres` | `github.com/StricklySoft/stricklysoft-core/pkg/clients/postgres` | Client | Query, QueryRow, Exec, Begin, Health |

### Trace Context Utilities

| Package | Function | Description |
|---------|----------|-------------|
| `pkg/auth` | `TraceIDFromContext(ctx)` | Extracts OTel trace ID from context |
| `pkg/auth` | `SpanIDFromContext(ctx)` | Extracts OTel span ID from context |

## Tracer Provider Setup

The SDK does not create or configure a tracer provider. The host
application is responsible for setting up a `TracerProvider` and
registering it with the OTel global:

```go
import (
    "go.opentelemetry.io/otel"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
)

func initTracer(ctx context.Context) (*sdktrace.TracerProvider, error) {
    exporter, err := otlptracehttp.New(ctx,
        otlptracehttp.WithEndpoint("tempo.observability.svc.cluster.local:4318"),
        otlptracehttp.WithInsecure(),
    )
    if err != nil {
        return nil, err
    }

    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceNameKey.String("my-agent"),
            semconv.ServiceVersionKey.String("0.1.0"),
        )),
    )

    otel.SetTracerProvider(tp)
    return tp, nil
}
```

Once the global tracer provider is set, all SDK packages automatically
export spans through it. If no tracer provider is configured, the SDK
uses the OTel no-op tracer and spans are silently discarded.

## Lifecycle Tracing

The `pkg/lifecycle` package creates spans for all agent state transitions.
Each `BaseAgent` holds an immutable `trace.Tracer` instance created during
`Build()`:

```go
tracer: otel.Tracer("github.com/StricklySoft/stricklysoft-core/pkg/lifecycle")
```

### Spans

| Span Name | Operation | Description |
|-----------|-----------|-------------|
| `lifecycle.Start` | `agent.Start(ctx)` | Agent startup including onStart hook |
| `lifecycle.Stop` | `agent.Stop(ctx)` | Agent shutdown including onStop hook |
| `lifecycle.Pause` | `agent.Pause(ctx)` | Agent pause including onPause hook |
| `lifecycle.Resume` | `agent.Resume(ctx)` | Agent resume including onResume hook |

### Span Attributes

All lifecycle spans include:

| Attribute | Type | Value |
|-----------|------|-------|
| `agent.id` | string | Unique agent instance identifier |
| `agent.name` | string | Human-readable agent name |

### Span Configuration

- **Span kind**: `Internal` (operation within a single service)
- **Error recording**: Failed operations call `span.RecordError(err)` and
  set `span.SetStatus(codes.Error, err.Error())`
- **Success recording**: Successful operations set
  `span.SetStatus(codes.Ok, "")`

### Example Trace

```
lifecycle.Start [agent.id=agent-001, agent.name=research-agent]
├── duration: 45ms
├── status: OK
└── events: (none)
```

When a lifecycle operation fails:

```
lifecycle.Start [agent.id=agent-001, agent.name=research-agent]
├── duration: 12ms
├── status: ERROR "lifecycle: invalid state transition from running to starting"
└── events:
    └── exception [message="lifecycle: invalid state transition from running to starting"]
```

## Database Tracing

The `pkg/clients/postgres` package creates spans for all database
operations. Each `Client` holds an immutable `trace.Tracer` instance
created during construction:

```go
tracer: otel.Tracer("github.com/StricklySoft/stricklysoft-core/pkg/clients/postgres")
```

### Spans

| Span Name | Operation | Description |
|-----------|-----------|-------------|
| `postgres.Query` | `client.Query(ctx, sql, args...)` | SELECT queries returning rows |
| `postgres.QueryRow` | `client.QueryRow(ctx, sql, args...)` | SELECT queries returning a single row |
| `postgres.Exec` | `client.Exec(ctx, sql, args...)` | INSERT, UPDATE, DELETE operations |
| `postgres.Begin` | `client.Begin(ctx)` | Transaction start |
| `postgres.Health` | `client.Health(ctx)` | Health check ping |

### Span Attributes

All database spans follow the
[OTel database semantic conventions](https://opentelemetry.io/docs/specs/semconv/database/database-spans/):

| Attribute | Type | Value |
|-----------|------|-------|
| `db.system` | string | `"postgresql"` |
| `db.name` | string | Database name from config |
| `db.statement` | string | SQL statement (truncated to 100 characters) |

### Span Configuration

- **Span kind**: `Client` (calling an external service)
- **Error recording**: Failed operations call `span.RecordError(err)` and
  set `span.SetStatus(codes.Error, err.Error())`
- **Success recording**: Successful operations set
  `span.SetStatus(codes.Ok, "")`

### SQL Statement Truncation

SQL statements in the `db.statement` attribute are truncated to 100
characters to prevent sensitive data from leaking into trace backends.
The truncation is rune-aware (safe for multi-byte characters) and appends
`"..."` to truncated statements:

```go
// "SELECT id, name, email FROM users WHERE ..." (truncated)
```

### Example Trace

```
postgres.Query [db.system=postgresql, db.name=stricklysoft, db.statement="SELECT id, name FROM agents WHERE status = $1"]
├── duration: 3ms
├── status: OK
└── events: (none)
```

## Trace Context Extraction

The `pkg/auth` package provides functions to extract OTel trace and span
IDs from a Go context. These are useful for correlating identity
information with distributed traces in audit logging.

### TraceIDFromContext

```go
func TraceIDFromContext(ctx context.Context) (string, bool)
```

Extracts the OTel trace ID from the context. Returns the trace ID as a
32-character hex string and `true` if a valid trace is active, or an
empty string and `false` if no trace is present.

### SpanIDFromContext

```go
func SpanIDFromContext(ctx context.Context) (string, bool)
```

Extracts the OTel span ID from the context. Returns the span ID as a
16-character hex string and `true` if a valid span is active, or an
empty string and `false` if no span is present.

### Usage

```go
if traceID, ok := auth.TraceIDFromContext(ctx); ok {
    logger.Info("processing request",
        "trace_id", traceID,
        "user_id", identity.Subject,
    )
}
```

## Trace Context Flow

The following diagram shows how trace context flows through a typical
request:

```
HTTP/gRPC Request (with traceparent header)
    |
    v
[OTel SDK extracts trace context into ctx]
    |
    v
[auth.ContextWithIdentity] -- Adds identity to context
[auth.TraceIDFromContext]   -- Available for audit correlation
    |
    v
[lifecycle.Start/Stop/Pause/Resume]
    Creates "lifecycle.*" span (SpanKind: Internal)
    Attributes: agent.id, agent.name
    |
    v
[postgres.Query/Exec/Begin]
    Creates "postgres.*" span (SpanKind: Client)
    Attributes: db.system, db.name, db.statement
    |
    v
Response (with trace context propagated)
```

All spans share the same trace ID, creating a connected trace across
lifecycle operations and database calls.

## Error Handling in Traces

Both packages follow the same error handling pattern for spans:

1. **Record the error**: `span.RecordError(err)` adds an `exception`
   event to the span with the error message.
2. **Set error status**: `span.SetStatus(codes.Error, err.Error())` marks
   the span as failed.
3. **Set success status**: `span.SetStatus(codes.Ok, "")` marks the span
   as successful on the happy path.

The lifecycle package uses a `failSpan` helper:

```go
func failSpan(span trace.Span, err error) {
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
}
```

The postgres package uses a `finishSpan` helper that also calls
`span.End()`:

```go
func finishSpan(span trace.Span, err error) {
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    } else {
        span.SetStatus(codes.Ok, "")
    }
    span.End()
}
```

## Security Considerations

1. **SQL statement truncation** -- SQL statements are truncated to 100
   characters in span attributes to prevent sensitive data (user data in
   INSERT statements, query parameters) from being stored in trace
   backends.

2. **No credential leakage** -- The `Secret` type ensures database
   passwords are never included in span attributes or error messages.

3. **Trace ID is not a secret** -- Trace IDs and span IDs are
   correlation identifiers, not authentication tokens. Exposing them in
   logs is safe and expected.

4. **Tracer provider controls export** -- The host application decides
   where spans are exported. The SDK creates spans but has no control
   over the export destination or sampling policy.

## Kubernetes Deployment

### OTLP Exporter Configuration

Configure the OTel SDK exporter via standard environment variables:

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://tempo.observability.svc.cluster.local:4318"
  - name: OTEL_SERVICE_NAME
    value: "my-agent"
  - name: OTEL_RESOURCE_ATTRIBUTES
    value: "deployment.environment=production,service.version=0.1.0"
```

### Collector Sidecar

For production deployments, use an OpenTelemetry Collector sidecar or
DaemonSet to batch, filter, and export spans:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-agent
spec:
  containers:
    - name: agent
      image: my-agent:latest
      env:
        - name: OTEL_EXPORTER_OTLP_ENDPOINT
          value: "http://localhost:4318"
    - name: otel-collector
      image: otel/opentelemetry-collector:latest
      ports:
        - containerPort: 4318
```

## Planned: Unified Observability Package

The `pkg/observability/` package is planned to provide:

- **Tracer provider factory** -- Pre-configured `TracerProvider` with
  OTLP export, resource attributes, and sampling
- **Prometheus metrics** -- Standard platform metrics (request count,
  latency histograms, error rate counters)
- **Structured logging** -- JSON-formatted `slog.Handler` with automatic
  trace ID injection
- **Correlation** -- Automatic trace ID and request ID propagation across
  services via context and HTTP/gRPC headers

The package files exist as placeholders:

```
pkg/observability/
    tracer.go        Placeholder
    logger.go        Placeholder
    metrics.go       Placeholder
    correlation.go   Placeholder
```

Until the unified package is implemented, applications should configure
the OTel SDK directly as shown in the
[Tracer Provider Setup](#tracer-provider-setup) section above.

## File Structure

```
pkg/observability/
    tracer.go          Planned: Tracer provider factory
    logger.go          Planned: Structured logging with trace correlation
    metrics.go         Planned: Prometheus metrics registration
    correlation.go     Planned: Cross-service correlation utilities
```
