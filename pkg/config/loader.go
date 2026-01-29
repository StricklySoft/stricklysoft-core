// Package config provides configuration loading from environment variables,
// files (YAML/JSON), and struct tag defaults for the StricklySoft Cloud
// Platform. It supports a layered configuration model where values are
// resolved in priority order:
//
//	envDefault struct tags  (lowest priority)
//	YAML/JSON config file  (medium priority)
//	Environment variables  (highest priority)
//
// This priority order mirrors how Kubernetes deployments typically work:
// sensible defaults are baked into the code, config files provide
// environment-specific overrides, and env vars (from ConfigMaps or Secrets)
// take final precedence.
//
// # Struct Tags
//
// The loader uses three struct tags to control behavior:
//
//   - `env:"VAR_NAME"` — maps the field to an environment variable
//   - `envDefault:"value"` — sets a default when the field is zero-valued
//   - `required:"true"` — fails validation if the field remains zero after loading
//
// Fields must also have `yaml` or `json` tags for file-based loading,
// since the YAML and JSON unmarshalers use those tags respectively.
//
// # Usage
//
//	type AppConfig struct {
//	    Host    string        `env:"HOST" envDefault:"localhost" yaml:"host"`
//	    Port    int           `env:"PORT" envDefault:"8080" yaml:"port" required:"true"`
//	    Debug   bool          `env:"DEBUG" envDefault:"false" yaml:"debug"`
//	    Timeout time.Duration `env:"TIMEOUT" envDefault:"30s" yaml:"timeout"`
//	}
//
//	cfg := config.MustLoad[AppConfig](
//	    config.New().WithEnvPrefix("APP").WithFile("config.yaml"),
//	)
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// durationType caches the reflect.Type for time.Duration to avoid repeated
// allocations during struct traversal. time.Duration has Kind() == Int64,
// so we need to distinguish it from plain int64 fields.
var durationType = reflect.TypeOf(time.Duration(0))

// Loader builds and executes configuration loading with a layered
// resolution strategy. Use [New] to create a Loader and configure it
// with [Loader.WithEnvPrefix] and [Loader.WithFile] before calling
// [Loader.Load].
//
// Loader is not safe for concurrent use. Create a new Loader for each
// Load call, or synchronize access externally.
type Loader struct {
	envPrefix string
	filePath  string
}

// New creates a new [Loader] with default settings. The returned Loader
// loads from environment variables only (no file, no prefix). Use
// [Loader.WithEnvPrefix] and [Loader.WithFile] to customize behavior
// before calling [Loader.Load].
func New() *Loader {
	return &Loader{}
}

// WithEnvPrefix sets a prefix that is prepended (with an underscore
// separator) to all environment variable names derived from the "env"
// struct tag. For example, WithEnvPrefix("APP") causes a field tagged
// `env:"HOST"` to read from the APP_HOST environment variable.
//
// The prefix is automatically uppercased. An empty prefix disables
// prefixing (the default behavior). WithEnvPrefix returns the Loader
// for fluent chaining.
func (l *Loader) WithEnvPrefix(prefix string) *Loader {
	l.envPrefix = strings.ToUpper(prefix)
	return l
}

// WithFile sets the path to a YAML or JSON configuration file. The
// file format is detected by extension:
//
//   - .yaml / .yml — parsed as YAML
//   - .json — parsed as JSON
//
// An unrecognized extension causes [Loader.Load] to return an error.
// If the file does not exist, loading proceeds without file-based
// values (file configuration is optional).
//
// The file path must not contain directory traversal sequences ("..").
// WithFile returns the Loader for fluent chaining.
func (l *Loader) WithFile(path string) *Loader {
	l.filePath = path
	return l
}

// Load populates the given struct pointer with configuration values
// resolved in priority order (highest wins):
//
//  1. envDefault struct tags (lowest priority)
//  2. YAML/JSON file values (if configured with [Loader.WithFile])
//  3. Environment variables from "env" struct tags (highest priority)
//
// After loading, the struct is validated:
//   - Fields tagged `required:"true"` must hold non-zero values
//   - If the struct implements [Validator], its Validate method is called
//
// The cfg parameter must be a non-nil pointer to a struct. Returns a
// [*sserr.Error] with code [sserr.CodeInternalConfiguration] for
// loading failures, or [sserr.CodeValidationRequired] /
// [sserr.CodeValidation] for validation failures.
func (l *Loader) Load(cfg any) error {
	rv := reflect.ValueOf(cfg)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return sserr.New(sserr.CodeInternalConfiguration,
			"config: Load requires a non-nil pointer to a struct")
	}

	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return sserr.New(sserr.CodeInternalConfiguration,
			"config: Load requires a pointer to a struct")
	}

	// Step 1: Apply envDefault tags to zero-valued fields.
	if err := applyDefaults(rv); err != nil {
		return err
	}

	// Step 2: Load from file (if configured).
	if l.filePath != "" {
		if err := l.loadFile(cfg); err != nil {
			return err
		}
	}

	// Step 3: Apply environment variables (highest priority).
	if err := applyEnv(rv, l.envPrefix); err != nil {
		return err
	}

	// Step 4: Validate required fields and custom Validator.
	return validate(cfg, rv)
}

// MustLoad is a generic convenience function that creates a zero-valued
// instance of T, loads configuration into it, and returns the populated
// value. It panics if loading or validation fails.
//
// Use MustLoad in application startup (e.g., func main) where a missing
// or invalid configuration should prevent the application from starting.
//
// T must be a struct type.
//
// Example:
//
//	cfg := config.MustLoad[AppConfig](config.New().WithEnvPrefix("APP"))
func MustLoad[T any](loader *Loader) T {
	var cfg T
	if err := loader.Load(&cfg); err != nil {
		panic(fmt.Sprintf("config: MustLoad failed: %v", err))
	}
	return cfg
}

// loadFile reads a YAML or JSON file and unmarshals it into the config
// struct. Missing files are silently ignored (file-based configuration
// is optional). The file path is validated to prevent directory
// traversal attacks.
func (l *Loader) loadFile(cfg any) error {
	// Security: reject directory traversal sequences.
	if strings.Contains(l.filePath, "..") {
		return sserr.New(sserr.CodeInternalConfiguration,
			"config: file path must not contain directory traversal (..) sequences")
	}

	data, err := os.ReadFile(l.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Missing file is not an error.
		}
		return sserr.Wrapf(err, sserr.CodeInternalConfiguration,
			"config: failed to read file %q", l.filePath)
	}

	ext := strings.ToLower(filepath.Ext(l.filePath))

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return sserr.Wrapf(err, sserr.CodeInternalConfiguration,
				"config: failed to parse YAML file %q", l.filePath)
		}
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return sserr.Wrapf(err, sserr.CodeInternalConfiguration,
				"config: failed to parse JSON file %q", l.filePath)
		}
	default:
		return sserr.Newf(sserr.CodeInternalConfiguration,
			"config: unsupported file extension %q (use .yaml, .yml, or .json)", ext)
	}

	return nil
}

// applyDefaults recursively traverses the struct and sets fields to
// their envDefault tag values when the field holds its zero value.
// Non-zero fields are left unchanged.
func applyDefaults(rv reflect.Value) error {
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rv.Field(i)
		sf := rt.Field(i)

		if !field.CanSet() {
			continue
		}

		// Recurse into nested structs.
		if field.Kind() == reflect.Struct && sf.Type != durationType {
			if err := applyDefaults(field); err != nil {
				return err
			}
			continue
		}

		tag := sf.Tag.Get("envDefault")
		if tag == "" {
			continue
		}

		// Only set if the field is currently zero-valued.
		if !field.IsZero() {
			continue
		}

		if err := setField(field, tag); err != nil {
			return sserr.Wrapf(err, sserr.CodeInternalConfiguration,
				"config: failed to apply default for field %q", sf.Name)
		}
	}

	return nil
}

// applyEnv recursively traverses the struct and sets fields from
// environment variables specified by the "env" struct tag. For nested
// structs, the parent's env tag value is prepended as a prefix
// (joined with "_") to the child's env tag.
//
// The prefix parameter includes both the global prefix (from
// [Loader.WithEnvPrefix]) and any accumulated nested struct prefixes.
func applyEnv(rv reflect.Value, prefix string) error {
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rv.Field(i)
		sf := rt.Field(i)

		if !field.CanSet() {
			continue
		}

		envTag := sf.Tag.Get("env")

		// Recurse into nested structs. The parent's env tag becomes
		// part of the prefix for child fields.
		if field.Kind() == reflect.Struct && sf.Type != durationType {
			nestedPrefix := prefix
			if envTag != "" {
				if nestedPrefix != "" {
					nestedPrefix = nestedPrefix + "_" + envTag
				} else {
					nestedPrefix = envTag
				}
			}
			if err := applyEnv(field, nestedPrefix); err != nil {
				return err
			}
			continue
		}

		if envTag == "" {
			continue
		}

		envKey := envTag
		if prefix != "" {
			envKey = prefix + "_" + envTag
		}

		val, ok := os.LookupEnv(envKey)
		if !ok {
			continue
		}

		if err := setField(field, val); err != nil {
			return sserr.Wrapf(err, sserr.CodeInternalConfiguration,
				"config: failed to set field %q from env var %q", sf.Name, envKey)
		}
	}

	return nil
}

// setField parses the string value and sets the reflect.Value according
// to its kind. Supported types:
//
//   - string (and named string types like postgres.Secret)
//   - bool
//   - int, int8, int16, int32, int64
//   - time.Duration (parsed with time.ParseDuration)
//   - []string (comma-separated, whitespace-trimmed)
//
// Returns an error for unsupported types or parse failures.
func setField(field reflect.Value, value string) error {
	// Handle time.Duration before the int64 case, since Duration's
	// underlying kind is int64 but requires time.ParseDuration.
	if field.Type() == durationType {
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("cannot parse duration %q: %w", value, err)
		}
		field.SetInt(int64(d))
		return nil
	}

	switch field.Kind() {
	case reflect.String:
		// Works for string and any named type with underlying kind
		// string (e.g., postgres.Secret, SSLMode, CloudProvider).
		field.SetString(value)

	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("cannot parse bool %q: %w", value, err)
		}
		field.SetBool(b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bitSize := field.Type().Bits()
		n, err := strconv.ParseInt(value, 10, bitSize)
		if err != nil {
			return fmt.Errorf("cannot parse integer %q: %w", value, err)
		}
		field.SetInt(n)

	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.String {
			parts := strings.Split(value, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			// Use reflect.MakeSlice with the field's actual type to
			// support named slice types (e.g., type Tags []string).
			// reflect.ValueOf(parts) would produce a []string value
			// that panics on Set if the field type differs.
			slice := reflect.MakeSlice(field.Type(), len(parts), len(parts))
			for i, p := range parts {
				slice.Index(i).SetString(p)
			}
			field.Set(slice)
		} else {
			return fmt.Errorf("unsupported slice element type %s", field.Type().Elem().Kind())
		}

	default:
		return fmt.Errorf("unsupported field type %s", field.Kind())
	}

	return nil
}
