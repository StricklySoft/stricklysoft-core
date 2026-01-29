# Troubleshooting

This guide covers common errors, their causes, and how to resolve them
when working with the StricklySoft Core SDK.

## Error Code Reference

Every error returned by the SDK is a `*sserr.Error` with a
machine-readable code. Use `sserr.AsError(err)` to extract the code:

```go
if e, ok := sserr.AsError(err); ok {
    fmt.Println(e.Code)       // e.g. "VAL_001"
    fmt.Println(e.HTTPStatus()) // e.g. 400
}
```

### Error Categories

| Category | Code Prefix | HTTP Status | Retryable | Description |
|----------|------------|-------------|-----------|-------------|
| Validation | `VAL_` | 400 | No | Invalid input or configuration |
| Authentication | `AUTH_` | 401 | No | Missing or invalid credentials |
| Authorization | `AUTHZ_` | 403 | No | Insufficient permissions |
| Not Found | `NF_` | 404 | No | Resource does not exist |
| Conflict | `CONF_` | 409 | No | State conflict or duplicate |
| Internal | `INT_` | 500 | No | Unexpected internal error |
| Unavailable | `UNAVAIL_` | 503 | Yes | Dependency not reachable |
| Timeout | `TIMEOUT_` | 504 | Yes | Operation exceeded deadline |

### Checking Error Properties

```go
sserr.IsClientError(err)   // 4xx — caller should fix the request
sserr.IsServerError(err)   // 5xx — server-side problem
sserr.IsRetryable(err)     // TIMEOUT or UNAVAIL — safe to retry
sserr.IsValidation(err)    // VAL — input validation failed
```

---

## Lifecycle Errors

### "lifecycle: invalid state transition from X to Y"

**Code**: `CodeConflict` (409)

**Cause**: You called a lifecycle method that is not valid for the
agent's current state.

**Common scenarios**:
- Calling `Start()` on an agent that is already running
- Calling `Pause()` on a stopped agent
- Calling `Stop()` on an agent that is already stopping

**Fix**: Check the agent's state before calling lifecycle methods:

```go
if agent.State() == lifecycle.StateRunning {
    agent.Stop(ctx)
}
```

**Valid transitions**:

| From | Allowed |
|------|---------|
| Unknown | Starting |
| Starting | Running, Failed |
| Running | Paused, Stopping, Failed |
| Paused | Running (Resume), Stopping, Failed |
| Stopping | Stopped, Failed |
| Stopped | (terminal) |
| Failed | (terminal) |

### "lifecycle: agent ID is required" / "name is required" / "version is required"

**Code**: `CodeValidation` (400)

**Cause**: `NewBaseAgentBuilder` was called with an empty string for
ID, name, or version.

**Fix**: Provide non-empty strings:

```go
lifecycle.NewBaseAgentBuilder("agent-001", "my-agent", "1.0.0")
```

### Agent stuck in Starting state

**Cause**: The `OnStart` hook is blocking — waiting for a dependency
that is not responding.

**Fix**: Pass a context with a deadline:

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

if err := agent.Start(ctx); err != nil {
    // CodeTimeout if deadline exceeded
}
```

### Agent transitions to Failed after Start

**Cause**: The `OnStart` hook returned an error. The agent moved from
`Starting` to `Failed` instead of `Running`.

**Fix**: Check the error returned by `Start()`. The hook error is
wrapped in the return value. Common causes:
- Database connection failed (`CodeUnavailableDependency`)
- Configuration validation failed (`CodeValidation`)
- Required dependency not ready

### Deadlock on state change

**Cause**: A state change handler called a lifecycle method
(`Start()`, `Stop()`, `Pause()`, `Resume()`) which tries to acquire
the mutex that the handler is already holding.

**Fix**: Never call lifecycle methods from state change handlers. Use
handlers only for logging, metrics, and notifications:

```go
// Safe: read-only operations
OnStateChange(func(old, new lifecycle.State) {
    slog.Info("state changed", "from", old, "to", new)
    metrics.Record(new)
})
```

---

## Database Client Errors

### "postgres: config validation failed"

**Code**: `CodeValidation` (400)

**Cause**: The `Config` struct has invalid values.

**Common issues**:

| Field | Error | Fix |
|-------|-------|-----|
| `Database` | Empty string | Set `Database` field or `POSTGRES_DATABASE` env var |
| `User` | Empty string | Set `User` field or `POSTGRES_USER` env var |
| `Port` | 0 or > 65535 | Set a valid port (default: 5432) |
| `SSLMode` | Invalid value | Use one of the `SSLMode*` constants |
| `SSLRootCert` | File not found | Verify the certificate file path exists |
| `MaxConns` | < 1 | Set to at least 1 (default: 25) |
| `MinConns` | > MaxConns | MinConns must be <= MaxConns |

### "postgres: failed to connect"

**Code**: `CodeUnavailableDependency` (503), retryable

**Cause**: The database server is not reachable. `NewClient` pings the
database after creating the pool, and the ping failed.

**Fix**:

1. Verify the database is running and accepting connections
2. Check the host and port in your config
3. If in-cluster, verify the PostgreSQL service exists:
   `kubectl get svc postgres -n databases`
4. Check network policies that might block the connection
5. If using TLS, verify the SSL mode and certificate path

### "postgres: query timed out"

**Code**: `CodeTimeoutDatabase` (504), retryable

**Cause**: The query exceeded the context deadline.

**Fix**:

1. Check if the query is slow (add database indexes)
2. Increase the context timeout if the query is legitimately slow
3. Check database load — high connection count can cause queuing

### "postgres: query failed"

**Code**: `CodeInternalDatabase` (500)

**Cause**: The database returned an error (syntax error, constraint
violation, etc.).

**Fix**: Check the wrapped error for the underlying PostgreSQL error
code. Common causes:
- SQL syntax error
- Unique constraint violation
- Foreign key violation
- Column type mismatch

### Connection pool exhaustion (queries hang)

**Cause**: All connections in the pool are in use. New queries block
waiting for a free connection.

**Common causes**:
- Unclosed `pgx.Rows` objects (each holds a connection)
- Long-running transactions that are never committed or rolled back
- `MaxConns` is too low for the workload

**Fix**:

1. Verify every `Query()` call has a `defer rows.Close()`
2. Verify every `Begin()` call has a `defer tx.Rollback(ctx)`
3. Increase `MaxConns` if the workload requires more concurrency
4. Add context deadlines to prevent individual queries from blocking
   forever

### TLS connection failures with Linkerd

**Cause**: Using `SSLModeVerifyFull` in a Linkerd-meshed cluster.
Linkerd terminates the application's TLS and re-encrypts with its
own mTLS certificates. The PostgreSQL server certificate does not
match what the client expects.

**Fix**: Use `SSLModeDisable` or `SSLModeRequire` when Linkerd
provides mTLS:

```go
cfg := postgres.DefaultConfig() // SSLMode defaults to Require
```

---

## Configuration Errors

### "config: required field \"X\" is empty"

**Code**: `CodeValidationRequired` (400)

**Cause**: A field tagged with `required:"true"` has a zero value
after all configuration layers (defaults, file, env vars) are
resolved.

**Fix**: Set the field in at least one layer:
- Add an `envDefault` tag for a sensible default
- Add the value to the config file
- Set the environment variable

### "config: unsupported file extension"

**Code**: `CodeInternalConfiguration` (500)

**Cause**: `WithFile()` was called with a file that has an
unsupported extension.

**Fix**: Use `.yaml`, `.yml`, or `.json`. Other formats are not
supported.

### "config: path contains directory traversal"

**Code**: `CodeInternalConfiguration` (500)

**Cause**: The file path passed to `WithFile()` contains `..`
segments.

**Fix**: Use absolute paths or paths relative to the working
directory without `..`:

```go
config.New().WithFile("/etc/agent/config.yaml")  // absolute
config.New().WithFile("config.yaml")              // relative
```

### "config: Load requires a non-nil pointer to a struct"

**Code**: `CodeInternalConfiguration` (500)

**Cause**: `Load()` was called with `nil`, a non-pointer, or a
pointer to a non-struct type.

**Fix**: Pass a pointer to a struct:

```go
var cfg AppConfig
err := loader.Load(&cfg)
```

### MustLoad panics at startup

**Cause**: Configuration loading or validation failed. `MustLoad`
intentionally panics because a service that cannot load its
configuration should not start.

**Fix**: Check the panic message for the specific error. Common
causes:
- Required field not set (set the environment variable)
- Config file has syntax errors (validate YAML/JSON)
- Environment variable has wrong type (e.g., `"abc"` for an int
  field)

---

## Authentication Errors

### "identity not found in context"

**Cause**: `MustIdentityFromContext()` was called on a context that
has no identity. This function panics.

**Fix**: Use `IdentityFromContext()` with the `ok` check, or ensure
authentication middleware runs before the handler:

```go
identity, ok := auth.IdentityFromContext(ctx)
if !ok {
    return sserr.New(sserr.CodeAuthentication, "not authenticated")
}
```

### Call chain exceeds maximum depth

**Cause**: The call chain has more than 32 entries
(`MaxCallChainDepth`). This suggests a circular service call.

**Fix**: Investigate the service call graph for cycles. Each service
should append itself to the chain exactly once.

### TraceIDFromContext returns empty string

**Cause**: No OpenTelemetry tracer provider is configured, so there
is no active span in the context.

**Fix**: Register a tracer provider at startup:

```go
otel.SetTracerProvider(tp)
```

If you intentionally run without tracing, check the `ok` return
value:

```go
if traceID, ok := auth.TraceIDFromContext(ctx); ok {
    // use traceID
}
```

---

## Observability Issues

### Spans not appearing in trace backend

**Possible causes**:

1. **No tracer provider registered** — Call
   `otel.SetTracerProvider(tp)` at startup
2. **Exporter endpoint wrong** — Verify
   `OTEL_EXPORTER_OTLP_ENDPOINT` points to a running collector
3. **Sampling rate too low** — Check the sampler configuration
4. **Exporter not flushed** — Call `tp.Shutdown(ctx)` before the
   process exits to flush buffered spans

**Diagnostic steps**:

```go
// Verify the tracer provider is not the no-op default
tp := otel.GetTracerProvider()
fmt.Printf("tracer provider type: %T\n", tp)
// Should NOT be "*trace.noopTracerProvider"
```

### SQL statements visible in trace backend

**Cause**: SQL statements longer than 100 characters are truncated,
but shorter statements appear in full. If a query contains sensitive
data in the first 100 characters, it will be visible.

**Fix**: Use parameterized queries. Parameters (`$1`, `$2`) are not
expanded in the span attribute:

```go
// Safe: parameters not included in span
client.Query(ctx, "SELECT * FROM users WHERE email = $1", email)

// Unsafe: literal value appears in span
client.Query(ctx, "SELECT * FROM users WHERE email = '"+email+"'")
```

---

## Kubernetes-Specific Issues

### Pod cannot connect to in-cluster PostgreSQL

1. Verify the service exists:
   ```bash
   kubectl get svc postgres -n databases
   ```

2. Verify the pod can resolve the DNS name:
   ```bash
   kubectl exec -it <pod> -- nslookup postgres.databases.svc.cluster.local
   ```

3. Check network policies:
   ```bash
   kubectl get networkpolicy -n databases
   ```

4. Check if Linkerd is injected on both sides:
   ```bash
   kubectl get pod <pod> -o jsonpath='{.spec.containers[*].name}'
   # Should include "linkerd-proxy"
   ```

### Configuration not updating after ConfigMap change

**Cause**: The config loader reads the file once at startup. It does
not watch for changes.

**Fix**: Restart the pod after updating the ConfigMap:

```bash
kubectl rollout restart deployment/my-agent
```

### Environment variable not taking effect

**Check order**:

1. Verify the env var is set in the pod:
   ```bash
   kubectl exec -it <pod> -- env | grep APP_
   ```

2. Verify the prefix matches:
   ```go
   config.New().WithEnvPrefix("APP") // looks for APP_HOST, not HOST
   ```

3. Remember precedence: env vars override file values, which
   override defaults. If the env var is set, it always wins.

---

## Diagnostic Checklist

When troubleshooting an issue, work through this checklist:

1. **Check the error code** — Extract the `sserr.Code` to identify
   the category (validation, auth, timeout, etc.)

2. **Check retryability** — If `sserr.IsRetryable(err)` returns
   true, the issue may be transient

3. **Check the wrapped error** — Use `errors.Unwrap(err)` to find
   the root cause (e.g., the underlying PostgreSQL error)

4. **Check the trace** — If tracing is configured, find the span in
   your trace backend. Error spans include the exception event with
   the full error message

5. **Check the agent state** — Call `agent.State()` and
   `agent.Info()` to see the current lifecycle state and uptime

6. **Check connectivity** — For `UNAVAIL` and `TIMEOUT` errors,
   verify network connectivity to the dependency

7. **Check configuration** — For `VAL` errors, print the resolved
   configuration (excluding secrets) to verify values loaded
   correctly
