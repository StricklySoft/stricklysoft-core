# Best Practices

This guide covers recommended patterns for building services with the
StricklySoft Core SDK. Each section addresses a specific area of the
SDK with concrete do/don't examples drawn from the actual codebase.

## Error Handling

### Use Platform Error Codes

All errors returned from SDK packages use `*sserr.Error` with
machine-readable codes. Preserve this convention in your own code:

```go
// Create errors with a code and message
err := sserr.New(sserr.CodeValidation, "email address is invalid")

// Wrap errors from external libraries to add a platform code
result, err := externalService.Call(ctx)
if err != nil {
    return sserr.Wrap(err, sserr.CodeUnavailableDependency,
        "external service unavailable")
}
```

Do not return bare `error` values from public APIs. Callers depend on
error codes for retry decisions and HTTP status mapping.

### Check Retryability Before Retrying

```go
rows, err := client.Query(ctx, "SELECT ...")
if err != nil {
    if sserr.IsRetryable(err) {
        // Safe to retry — timeout or unavailable dependency
    }
    return err
}
```

Only `TIMEOUT` and `UNAVAIL` errors are retryable. Retrying a
`CodeValidation` or `CodeConflict` error will always fail again.

### Map Errors to HTTP Status Codes

```go
if e, ok := sserr.AsError(err); ok {
    w.WriteHeader(e.HTTPStatus())
    json.NewEncoder(w).Encode(map[string]string{
        "code":    string(e.Code),
        "message": e.Message,
    })
    return
}
```

The `HTTPStatus()` method maps each error category to the correct
HTTP status (400, 401, 403, 404, 409, 500, 503, 504). Use it instead
of hardcoding status codes.

## Lifecycle Management

### Use the Builder Pattern

Build agents with `NewBaseAgentBuilder` and configure hooks before
calling `Build()`:

```go
agent, err := lifecycle.NewBaseAgentBuilder("agent-001", "research-agent", "1.0.0").
    WithCapability(lifecycle.Capability{
        Name:    "web-search",
        Version: "1.0.0",
    }).
    WithLogger(slog.Default()).
    WithOnStart(func(ctx context.Context) error {
        return db.Health(ctx)
    }).
    WithOnStop(func(ctx context.Context) error {
        db.Close()
        return nil
    }).
    OnStateChange(func(old, new lifecycle.State) {
        slog.Info("state changed", "from", old, "to", new)
    }).
    Build()
```

### Clean Up Resources on Hook Failure

If an `OnStart` hook allocates resources before failing, it must
clean them up. The agent transitions to `StateFailed` and `OnStop`
is not called.

```go
// Bad: db stays open when Health() fails
WithOnStart(func(ctx context.Context) error {
    ra.db, _ = postgres.NewClient(ctx, cfg)
    return ra.db.Health(ctx) // fails → db leaked
})

// Good: close db before returning error
WithOnStart(func(ctx context.Context) error {
    db, err := postgres.NewClient(ctx, cfg)
    if err != nil {
        return err
    }
    if err := db.Health(ctx); err != nil {
        db.Close()
        return err
    }
    ra.db = db
    return nil
})
```

### Respect the State Machine

Agents follow a strict finite state machine. Invalid transitions
return `CodeConflict`:

```
Unknown → Starting → Running → Stopping → Stopped
                       ↕
                     Paused
```

- Do not call `Start()` on a running agent.
- Do not call `Pause()` on a stopped agent.
- Any state can transition to `Failed` on error.
- `Stopped` and `Failed` are terminal — the agent must be rebuilt.

### Never Call Lifecycle Methods from State Change Handlers

State change handlers execute under the agent's mutex lock. Calling
`Start()`, `Stop()`, `Pause()`, or `Resume()` from a handler will
deadlock:

```go
// DEADLOCK: handler acquires lock that is already held
OnStateChange(func(old, new lifecycle.State) {
    agent.Stop(ctx) // blocks forever
})
```

Read-only methods (`State()`, `Info()`, `Health()`) are safe to call
from handlers.

## Database Client

### Always Defer Close

```go
client, err := postgres.NewClient(ctx, cfg)
if err != nil {
    return err
}
defer client.Close()
```

Forgetting `Close()` leaks the connection pool. Connections are held
until the process exits, which can exhaust the database's connection
limit.

### Always Close Rows

```go
rows, err := client.Query(ctx, "SELECT id, name FROM agents")
if err != nil {
    return err
}
defer rows.Close()

for rows.Next() {
    // ...
}
```

An unclosed `pgx.Rows` holds a connection from the pool. If enough
rows objects accumulate, new queries block waiting for a free
connection.

### Use Transactions with Deferred Rollback

```go
tx, err := client.Begin(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx) // no-op if committed

_, err = tx.Exec(ctx, "INSERT INTO events ...")
if err != nil {
    return err
}

return tx.Commit(ctx)
```

The deferred `Rollback()` ensures the transaction is cleaned up if
an error occurs before `Commit()`. Calling `Rollback()` after
`Commit()` is a safe no-op.

### Compose Health Checks

When your agent wraps a database client, compose the health checks:

```go
func (ra *ResearchAgent) Health(ctx context.Context) error {
    if err := ra.BaseAgent.Health(ctx); err != nil {
        return err
    }
    return ra.db.Health(ctx)
}
```

The database `Health()` applies a default 5-second timeout if the
context has no deadline.

### Use NewFromPool for Unit Tests

```go
mock, err := pgxmock.NewPool()
if err != nil {
    t.Fatal(err)
}
defer mock.Close()

mock.ExpectQuery("SELECT").WillReturnRows(
    pgxmock.NewRows([]string{"id"}).AddRow(1),
)

client := postgres.NewFromPool(mock, nil)

// ... exercise client ...

if err := mock.ExpectationsWereMet(); err != nil {
    t.Errorf("unfulfilled expectations: %v", err)
}
```

Always call `ExpectationsWereMet()` at the end of each test to
verify all expected queries were executed.

## Context Propagation

### Always Propagate Context

Pass the incoming context through all layers. Do not create a new
`context.Background()` — this drops identity, trace, and deadline
information:

```go
// Bad: loses identity and trace context
func (s *Service) Handle(ctx context.Context) error {
    return s.db.Query(context.Background(), "SELECT ...")
}

// Good: propagates everything
func (s *Service) Handle(ctx context.Context) error {
    return s.db.Query(ctx, "SELECT ...")
}
```

### Check Context Values Defensively

Context values may be absent. Always check the `ok` return:

```go
identity, ok := auth.IdentityFromContext(ctx)
if !ok {
    return sserr.New(sserr.CodeAuthentication, "no identity in context")
}
```

Use `MustIdentityFromContext()` only in code paths where identity is
guaranteed (e.g., after authentication middleware). It panics if no
identity is present.

### Set Deadlines on User-Facing Operations

```go
ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()

if err := agent.Start(ctx); err != nil {
    // returns CodeTimeout if deadline exceeded
}
```

Without a deadline, a hanging dependency blocks the operation
indefinitely.

## Configuration

### Use MustLoad in main()

`MustLoad` panics on failure, which is appropriate for startup
configuration. A service that cannot load its configuration should
not start:

```go
func main() {
    cfg := config.MustLoad[AppConfig](
        config.New().
            WithEnvPrefix("APP").
            WithFile("/etc/agent/config.yaml"),
    )
    // cfg is fully validated
}
```

Use `Load()` (non-panicking) when loading configuration in tests or
when partial failure is acceptable.

### Use required Tags for Mandatory Fields

```go
type Config struct {
    Database string `env:"DATABASE" required:"true"`
    Host     string `env:"HOST" envDefault:"localhost"`
}
```

Required validation runs after all layers are resolved. If `DATABASE`
is not set in any layer (env var, file, or default), `Load()` returns
a `CodeValidationRequired` error with the field path.

### Use the Secret Type for Credentials

```go
type Config struct {
    Password postgres.Secret `env:"PASSWORD"`
}
```

The `Secret` type redacts its value in `fmt.Println`, `slog`, JSON
marshaling, and `%#v` formatting. Only `Value()` returns the actual
string.

## Observability

### Register a Tracer Provider at Startup

The SDK creates spans automatically, but they go nowhere without a
registered tracer provider:

```go
import "go.opentelemetry.io/otel"

tp := initTracerProvider() // your setup
otel.SetTracerProvider(tp)
defer tp.Shutdown(ctx)
```

Once registered, all lifecycle and database operations export spans.
Without a provider, the no-op tracer discards everything silently.

### Correlate Logs with Traces

Extract the trace ID from context and include it in structured logs:

```go
traceID, _ := auth.TraceIDFromContext(ctx)
slog.Info("processing request",
    "trace_id", traceID,
    "user_id", identity.Subject,
)
```

This makes it possible to find the distributed trace for any log
entry in your trace backend (Jaeger, Tempo, etc.).

### Do Not Log SQL Statements

The database client truncates SQL to 100 characters in span
attributes to prevent sensitive data leakage. Apply the same
discipline in application logs:

```go
// Bad: may contain user data
slog.Info("query", "sql", sql)

// Good: log the operation, not the statement
slog.Info("queried agents", "count", len(results))
```

## Kubernetes Deployment

### Use Environment Variables for Secrets

Inject credentials via Kubernetes Secrets or the External Secrets
Operator. Do not put credentials in config files or container images:

```yaml
env:
  - name: APP_DB_PASSWORD
    valueFrom:
      secretKeyRef:
        name: db-credentials
        key: password
```

### Use ConfigMaps for Non-Secret Configuration

```yaml
volumes:
  - name: config
    configMap:
      name: agent-config
volumeMounts:
  - name: config
    mountPath: /etc/agent
```

The config loader's layered model (`env > file > default`) maps
directly to this pattern.

### Match SSL Mode to Network Topology

| Environment | SSL Mode | Reason |
|-------------|----------|--------|
| In-cluster with Linkerd | `disable` or `require` | Linkerd provides mTLS |
| Cloud database (VPC) | `verify-ca` | Verify certificate chain |
| Cloud database (public) | `verify-full` | Verify chain + hostname |

Using `verify-full` with Linkerd causes connection failures because
Linkerd terminates TLS and re-encrypts with its own certificate.

## Testing

### Test State Transitions Explicitly

```go
agent, _ := lifecycle.NewBaseAgentBuilder("t", "t", "1.0").Build()

// Verify initial state
if agent.State() != lifecycle.StateUnknown {
    t.Fatalf("expected Unknown, got %s", agent.State())
}

// Start and verify
agent.Start(ctx)
if agent.State() != lifecycle.StateRunning {
    t.Fatalf("expected Running, got %s", agent.State())
}

// Verify invalid transition returns error
err := agent.Start(ctx) // already running
if err == nil {
    t.Fatal("expected error for Start() from Running state")
}
```

### Use Table-Driven Tests for Error Codes

```go
tests := []struct {
    name string
    code sserr.Code
    want int
}{
    {"validation", sserr.CodeValidation, 400},
    {"not found", sserr.CodeNotFound, 404},
    {"timeout", sserr.CodeTimeoutDatabase, 504},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        err := sserr.New(tt.code, "test")
        if got := err.HTTPStatus(); got != tt.want {
            t.Errorf("HTTPStatus() = %d, want %d", got, tt.want)
        }
    })
}
```

### Use Integration Tests with Testcontainers

The postgres package uses testcontainers for integration testing.
Follow the same pattern for end-to-end database tests:

```go
//go:build integration

func TestIntegration(t *testing.T) {
    ctx := context.Background()
    container, err := postgres.RunContainer(ctx)
    if err != nil {
        t.Skip("docker not available")
    }
    defer container.Terminate(ctx)

    // Connect and test against real database
}
```

Tag integration tests with `//go:build integration` and run them
separately: `go test -tags=integration ./...`
