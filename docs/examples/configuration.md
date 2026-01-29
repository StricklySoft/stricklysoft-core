# Example: Configuration

This example demonstrates the configuration loader's layered resolution
model, struct tags, file loading, custom validation, and Kubernetes
integration patterns.

## Basic Configuration

Define a configuration struct with `env`, `envDefault`, and `required`
struct tags:

```go
package main

import (
    "fmt"
    "time"

    "github.com/StricklySoft/stricklysoft-core/pkg/config"
)

type AppConfig struct {
    Host    string        `env:"HOST" envDefault:"localhost" yaml:"host"`
    Port    int           `env:"PORT" envDefault:"8080" yaml:"port" required:"true"`
    Debug   bool          `env:"DEBUG" envDefault:"false" yaml:"debug"`
    Timeout time.Duration `env:"TIMEOUT" envDefault:"30s" yaml:"timeout"`
}

func main() {
    cfg := config.MustLoad[AppConfig](
        config.New().WithEnvPrefix("APP"),
    )
    fmt.Printf("Listening on %s:%d (debug=%v, timeout=%v)\n",
        cfg.Host, cfg.Port, cfg.Debug, cfg.Timeout)
}
```

## Struct Tags

| Tag | Purpose | Example |
|-----|---------|---------|
| `env:"VAR_NAME"` | Maps field to environment variable | `env:"HOST"` |
| `envDefault:"value"` | Default when field is zero-valued | `envDefault:"localhost"` |
| `required:"true"` | Fails validation if field is zero | `required:"true"` |
| `yaml:"key"` | YAML file field mapping | `yaml:"host"` |
| `json:"key"` | JSON file field mapping | `json:"host"` |

## Loading with MustLoad

`MustLoad` is a generic convenience function for application startup.
It panics if loading or validation fails, which is the correct behavior
for missing configuration at startup:

```go
cfg := config.MustLoad[AppConfig](
    config.New().
        WithEnvPrefix("APP").
        WithFile("/etc/agent/config.yaml"),
)
```

## Loading with Load

For non-fatal configuration loading (where you want to handle errors),
use `Load`:

```go
var cfg AppConfig
if err := config.New().WithEnvPrefix("APP").Load(&cfg); err != nil {
    log.Printf("configuration error: %v", err)
    // Handle gracefully
}
```

## Environment Variable Prefix

`WithEnvPrefix` prepends a prefix to all environment variable lookups:

```go
config.New().WithEnvPrefix("MYAPP")
```

| Struct Tag | Without Prefix | With Prefix `MYAPP` |
|------------|----------------|---------------------|
| `env:"HOST"` | `HOST` | `MYAPP_HOST` |
| `env:"PORT"` | `PORT` | `MYAPP_PORT` |
| `env:"TIMEOUT"` | `TIMEOUT` | `MYAPP_TIMEOUT` |

The prefix is automatically uppercased.

## Nested Structs

Nested structs support automatic environment variable prefix chaining:

```go
type Config struct {
    App      string   `env:"APP"`
    Database DBConfig `env:"DB"`
}

type DBConfig struct {
    Host string `env:"HOST" envDefault:"localhost" yaml:"host" required:"true"`
    Port int    `env:"PORT" envDefault:"5432" yaml:"port" required:"true"`
    Name string `env:"NAME" yaml:"name" required:"true"`
}
```

With `WithEnvPrefix("MYAPP")`:

| Field | Environment Variable |
|-------|---------------------|
| `App` | `MYAPP_APP` |
| `Database.Host` | `MYAPP_DB_HOST` |
| `Database.Port` | `MYAPP_DB_PORT` |
| `Database.Name` | `MYAPP_DB_NAME` |

## File-Based Configuration

### YAML

```yaml
# config.yaml
host: staging.internal
port: 3000
timeout: 60s
```

```go
cfg := config.MustLoad[AppConfig](
    config.New().WithEnvPrefix("APP").WithFile("config.yaml"),
)
```

### JSON

```json
{
  "host": "staging.internal",
  "port": 3000,
  "timeout": "60s"
}
```

```go
cfg := config.MustLoad[AppConfig](
    config.New().WithEnvPrefix("APP").WithFile("config.json"),
)
```

Missing files are silently ignored. File-based configuration is optional.

## Priority Resolution

Values are resolved in priority order (highest wins):

```
Environment variables  (highest)
         |
   Config file values  (medium)
         |
  envDefault struct tags  (lowest)
```

| Field | envDefault | config.yaml | Env Var | Result |
|-------|-----------|-------------|---------|--------|
| Host | localhost | staging.internal | `APP_HOST=prod.example.com` | prod.example.com |
| Port | 8080 | 3000 | (not set) | 3000 |
| Timeout | 30s | (not set) | (not set) | 30s |

## Custom Validation

Implement the `Validator` interface for cross-field validation:

```go
import sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"

type DatabaseConfig struct {
    Host string `env:"HOST" required:"true"`
    Port int    `env:"PORT" required:"true"`
}

func (c *DatabaseConfig) Validate() error {
    if c.Port < 1 || c.Port > 65535 {
        return sserr.Newf(sserr.CodeValidation,
            "config: port %d is out of range [1, 65535]", c.Port)
    }
    return nil
}
```

The `Validator` interface is called only after all `required` tags pass.

## Supported Types

| Go Type | Parse Method | Example Value |
|---------|-------------|---------------|
| `string` | Direct assignment | `"localhost"` |
| `bool` | `strconv.ParseBool` | `"true"`, `"1"` |
| `int`, `int8`-`int64` | `strconv.ParseInt` | `"8080"` |
| `time.Duration` | `time.ParseDuration` | `"30s"`, `"1h30m"` |
| `[]string` | Comma-separated split | `"a, b, c"` |
| Named string types | `reflect.SetString` | Works for types like `Secret` |

## Kubernetes Integration

### ConfigMap for File-Based Config

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-config
data:
  config.yaml: |
    host: db.production.svc.cluster.local
    port: 5432
```

### Secret for Credentials

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: db-credentials
type: Opaque
data:
  password: cGFzc3dvcmQ=
```

### Pod Spec

```yaml
spec:
  containers:
    - name: agent
      env:
        - name: APP_DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: db-credentials
              key: password
      volumeMounts:
        - name: config
          mountPath: /etc/agent
  volumes:
    - name: config
      configMap:
        name: agent-config
```

### Application Code

```go
func main() {
    cfg := config.MustLoad[AppConfig](
        config.New().
            WithEnvPrefix("APP").
            WithFile("/etc/agent/config.yaml"),
    )
    // Priority: APP_* env vars > /etc/agent/config.yaml > envDefault tags
}
```

## Error Handling

| Error Code | When |
|------------|------|
| `CodeInternalConfiguration` | Invalid input, file parse error, unsupported extension |
| `CodeValidationRequired` | Required field has zero value |
| `CodeValidation` | Custom Validator.Validate() failure |

## Next Steps

- [Configuration API Reference](../api/config.md) -- Full API documentation
- [Basic Agent Example](basic-agent.md) -- Using config in an agent
