# Configuration Loader

This document describes the configuration loading system provided by the
`pkg/config` package. It covers the layered resolution model, struct tags,
builder API, supported types, validation, security considerations, and
integration patterns for services running on the StricklySoft Cloud Platform.

## Overview

The configuration loader provides a unified way to populate typed Go structs
from multiple configuration sources. It uses a layered resolution model that
mirrors how Kubernetes deployments typically work:

- **Struct tag defaults** provide sensible baseline values baked into the code
- **Config files** (YAML or JSON) supply environment-specific overrides
- **Environment variables** take final precedence, sourced from ConfigMaps
  or Secrets in Kubernetes

This layered approach means a service works with zero configuration (via
defaults), can be customized with a config file for specific environments,
and individual values can be overridden at deployment time through
environment variables.

## Layered Resolution Model

Values are resolved in priority order (highest wins):

```
Environment variables  (from env struct tag)     highest priority
         |
   Config file values  (YAML or JSON)            medium priority
         |
  envDefault struct tags                          lowest priority
```

Each layer only sets values it knows about. An unset environment variable
does not clear a value loaded from a file. A file that omits a field does
not clear its default.

### Resolution Example

Given this struct:

```go
type AppConfig struct {
    Host    string        `env:"HOST" envDefault:"localhost" yaml:"host"`
    Port    int           `env:"PORT" envDefault:"8080" yaml:"port"`
    Timeout time.Duration `env:"TIMEOUT" envDefault:"30s" yaml:"timeout"`
}
```

With `config.yaml`:

```yaml
host: staging.internal
port: 3000
```

And environment variable `HOST=prod.example.com`:

| Field   | Default     | File              | Env                 | Result              |
|---------|-------------|-------------------|---------------------|---------------------|
| Host    | localhost   | staging.internal  | prod.example.com    | prod.example.com    |
| Port    | 8080        | 3000              | (not set)           | 3000                |
| Timeout | 30s         | (not set)         | (not set)           | 30s                 |

## Struct Tags

The loader uses three struct tags to control behavior:

| Tag                    | Purpose                                            |
|------------------------|----------------------------------------------------|
| `env:"VAR_NAME"`       | Maps the field to an environment variable           |
| `envDefault:"value"`   | Sets a default when the field is zero-valued         |
| `required:"true"`      | Fails validation if the field remains zero           |

Fields must also have `yaml` or `json` tags for file-based loading, since
the YAML and JSON unmarshalers use those tags respectively.

### Example Struct

```go
type DatabaseConfig struct {
    Host     string        `env:"HOST" envDefault:"localhost" yaml:"host" json:"host" required:"true"`
    Port     int           `env:"PORT" envDefault:"5432" yaml:"port" json:"port" required:"true"`
    Name     string        `env:"NAME" yaml:"name" json:"name" required:"true"`
    SSLMode  string        `env:"SSL_MODE" envDefault:"require" yaml:"ssl_mode" json:"ssl_mode"`
    Timeout  time.Duration `env:"TIMEOUT" envDefault:"10s" yaml:"timeout" json:"timeout"`
    Password Secret        `env:"PASSWORD"`
}
```

## Loader API

### Construction

Use `New()` to create a Loader and configure it with builder methods:

```go
loader := config.New().
    WithEnvPrefix("APP").
    WithFile("config.yaml")
```

### Builder Methods

| Method                       | Description                                    |
|------------------------------|------------------------------------------------|
| `New() *Loader`              | Creates a Loader with default settings         |
| `WithEnvPrefix(prefix)`      | Sets an environment variable prefix            |
| `WithFile(path)`             | Sets the config file path (YAML or JSON)       |

Both `WithEnvPrefix` and `WithFile` return the `Loader` for fluent chaining.

### Load

```go
func (l *Loader) Load(cfg any) error
```

`Load` populates the given struct pointer with configuration values. The
`cfg` parameter must be a non-nil pointer to a struct.

Loading proceeds in four steps:

1. Apply `envDefault` tag values to zero-valued fields
2. Load and unmarshal the config file (if configured)
3. Apply environment variables (highest priority)
4. Validate required fields and custom `Validator` interface

### MustLoad

```go
func MustLoad[T any](loader *Loader) T
```

`MustLoad` is a generic convenience function for application startup. It
creates a zero-valued instance of `T`, loads configuration into it, and
returns the populated value. It panics if loading or validation fails.

Use `MustLoad` in `func main()` where a missing or invalid configuration
should prevent the application from starting:

```go
cfg := config.MustLoad[AppConfig](
    config.New().WithEnvPrefix("APP").WithFile("config.yaml"),
)
```

## Environment Variable Prefix

`WithEnvPrefix` sets a prefix that is prepended (with an underscore separator)
to all environment variable names derived from the `env` struct tag:

```go
config.New().WithEnvPrefix("APP")
```

| Struct Tag      | Without Prefix | With Prefix `APP` |
|-----------------|----------------|-------------------|
| `env:"HOST"`    | `HOST`         | `APP_HOST`        |
| `env:"PORT"`    | `PORT`         | `APP_PORT`        |
| `env:"TIMEOUT"` | `TIMEOUT`      | `APP_TIMEOUT`     |

The prefix is automatically uppercased. `WithEnvPrefix("myapp")` and
`WithEnvPrefix("MYAPP")` are equivalent.

An empty prefix disables prefixing (the default behavior).

## Supported Types

The loader supports the following field types:

| Go Type         | Parse Method                | Example Value       |
|-----------------|-----------------------------|---------------------|
| `string`        | Direct assignment           | `"localhost"`       |
| `bool`          | `strconv.ParseBool`         | `"true"`, `"1"`     |
| `int`           | `strconv.ParseInt`          | `"8080"`            |
| `int8`          | `strconv.ParseInt` (8-bit)  | `"127"`             |
| `int16`         | `strconv.ParseInt` (16-bit) | `"32000"`           |
| `int32`         | `strconv.ParseInt` (32-bit) | `"25"`              |
| `int64`         | `strconv.ParseInt` (64-bit) | `"1000000"`         |
| `time.Duration` | `time.ParseDuration`        | `"30s"`, `"1h30m"`  |
| `[]string`      | Comma-separated split       | `"a, b, c"`         |

### Named String Types

Named types with an underlying kind of `string` are supported without any
special handling. This includes types like `postgres.Secret`, `SSLMode`,
and `CloudProvider`. The loader uses `reflect.SetString()`, which works
for any type whose underlying kind is `string`.

```go
type Secret string
func (s Secret) String() string { return "[REDACTED]" }
func (s Secret) Value() string  { return string(s) }

type Config struct {
    Password Secret `env:"PASSWORD"`  // Works: Secret's underlying kind is string
}
```

### time.Duration

`time.Duration` has an underlying kind of `int64`, so the loader checks the
field's type explicitly before the integer switch case. Duration values are
parsed with `time.ParseDuration`, which accepts formats like `"30s"`,
`"5m"`, `"1h30m"`, `"500ms"`.

### String Slices

`[]string` fields are split on commas. Leading and trailing whitespace
around each element is trimmed:

- `"a,b,c"` produces `["a", "b", "c"]`
- `"x, y, z"` produces `["x", "y", "z"]`

## Nested Structs

Nested structs are supported with automatic environment variable prefix
chaining. When a struct field has an `env` tag, that tag value becomes
part of the prefix for all child fields.

### Example

```go
type Config struct {
    App      string   `env:"APP"`
    Database DBConfig `env:"DB"`
}

type DBConfig struct {
    Host string `env:"HOST" yaml:"host"`
    Port int    `env:"PORT" yaml:"port"`
}
```

| Field           | No Prefix | With Prefix `MYAPP` |
|-----------------|-----------|---------------------|
| `App`           | `APP`     | `MYAPP_APP`         |
| `Database.Host` | `DB_HOST` | `MYAPP_DB_HOST`     |
| `Database.Port` | `DB_PORT` | `MYAPP_DB_PORT`     |

For file-based loading, nested structs use the standard YAML/JSON structure
determined by their `yaml` or `json` tags:

```yaml
app: my-service
database:
  host: db.internal
  port: 5432
```

## File Loading

### Supported Formats

The file format is detected by extension:

| Extension      | Parser           |
|----------------|------------------|
| `.yaml`, `.yml`| `gopkg.in/yaml.v3` |
| `.json`        | `encoding/json`  |

Unrecognized extensions produce a `CodeInternalConfiguration` error.

### Missing Files

A missing config file is **not** an error. File-based configuration is
optional. The loader proceeds with defaults and environment variables.

### File Security

File paths containing directory traversal sequences (`..`) are rejected
with a `CodeInternalConfiguration` error. This prevents path traversal
attacks when file paths are derived from user input or external
configuration.

## Validation

Validation runs after all values are loaded. It consists of two phases:

### Phase 1: Required Tag Validation

Fields tagged with `required:"true"` are checked for zero values. If any
required field is zero-valued after loading, `Load` returns a
`CodeValidationRequired` error with the dotted field path:

```
config: required field "Database.Host" is empty
```

Required validation is recursive: it traverses nested structs and reports
the full path (e.g., `Database.Host`).

### Phase 2: Validator Interface

After required tag validation passes, the loader checks whether the config
struct implements the `Validator` interface:

```go
type Validator interface {
    Validate() error
}
```

If the struct implements `Validator`, its `Validate` method is called.
Errors that are already `*sserr.Error` instances are returned as-is; other
errors are wrapped with `sserr.CodeValidation`.

The `Validator` interface is **not** called if required tag validation
fails. This prevents custom validators from running against incomplete
configurations.

### Example Validator

```go
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

## Error Handling

All errors use the platform error package (`pkg/errors`):

| Code                         | When                                          |
|------------------------------|-----------------------------------------------|
| `CodeInternalConfiguration`  | Load input invalid (nil, non-pointer)         |
| `CodeInternalConfiguration`  | File read/parse error                         |
| `CodeInternalConfiguration`  | Unsupported file extension                    |
| `CodeInternalConfiguration`  | Directory traversal in file path              |
| `CodeInternalConfiguration`  | Type parse error (invalid int, bool, duration)|
| `CodeValidationRequired`     | Required field has zero value                 |
| `CodeValidation`             | Custom `Validator.Validate()` failure         |

All errors are `*sserr.Error` instances and can be checked with:

- `sserr.IsInternal(err)` for configuration loading errors
- `sserr.IsValidation(err)` for validation errors (covers both
  `CodeValidationRequired` and `CodeValidation`)
- `sserr.AsError(err)` to extract the `*sserr.Error` for code inspection

## Security Considerations

1. **Directory traversal prevention** — File paths containing `..` are
   rejected before any file system access occurs.

2. **Secret type support** — Named string types like `postgres.Secret`
   with redacted `String()` methods are supported. The loader sets the
   underlying string value without exposing it through logging or error
   messages.

3. **Environment variable isolation** — Each `Load` call reads environment
   variables at call time. The loader does not cache or persist environment
   variable values.

4. **File permissions** — Config files should be deployed with restrictive
   permissions (e.g., `0600` or `0640`). The loader reads files with
   `os.ReadFile`, which respects file system permissions.

5. **No remote sources** — The loader reads only from local environment
   variables and local files. Remote configuration sources (Consul, etcd)
   are out of scope.

6. **Input validation** — `Load` validates that its argument is a non-nil
   pointer to a struct before proceeding. Invalid inputs are rejected with
   descriptive error messages.

## Kubernetes Integration

The loader's layered model maps directly to Kubernetes configuration
patterns:

### Defaults (envDefault tags)

Sensible defaults are compiled into the binary. The service starts with
reasonable behavior even with zero external configuration.

### Config Files (YAML/JSON)

Environment-specific config files are mounted via ConfigMaps:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: agent-config
data:
  config.yaml: |
    host: db.production.svc.cluster.local
    port: 5432
    ssl_mode: verify-full
```

```yaml
volumeMounts:
  - name: config
    mountPath: /etc/agent
volumes:
  - name: config
    configMap:
      name: agent-config
```

### Environment Variables

Individual values are overridden from Secrets or ConfigMaps:

```yaml
env:
  - name: APP_DB_PASSWORD
    valueFrom:
      secretKeyRef:
        name: db-credentials
        key: password
  - name: APP_DB_HOST
    valueFrom:
      configMapKeyRef:
        name: agent-config
        key: db-host
```

### Full Example

```go
func main() {
    cfg := config.MustLoad[AppConfig](
        config.New().
            WithEnvPrefix("APP").
            WithFile("/etc/agent/config.yaml"),
    )

    // cfg is fully populated and validated.
    // Priority: APP_* env vars > /etc/agent/config.yaml > envDefault tags
}
```

## Thread Safety

`Loader` is **not** safe for concurrent use. Create a new `Loader` for
each `Load` call, or synchronize access externally. In practice, `Load`
is called once at startup, so concurrent access is not a concern.

## File Structure

```
pkg/config/
    loader.go          Loader builder, Load(), MustLoad[T](), file parsing, env/default traversal
    validation.go      Validator interface, required tag validation
    watcher.go         Placeholder (hot reload out of scope)
    loader_test.go     Comprehensive test suite (~45 tests)
```
