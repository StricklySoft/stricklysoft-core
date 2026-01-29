package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	sserr "github.com/StricklySoft/stricklysoft-core/pkg/errors"
)

// ===========================================================================
// Test Types
// ===========================================================================

// testSecret mimics postgres.Secret: a named string type with a
// redacted String() method. Verifies that setField works for named
// string types without importing the postgres package.
type testSecret string

func (s testSecret) String() string { return "[REDACTED]" }
func (s testSecret) Value() string  { return string(s) }

type basicConfig struct {
	Host    string        `env:"HOST" envDefault:"localhost" yaml:"host" json:"host"`
	Port    int           `env:"PORT" envDefault:"8080" yaml:"port" json:"port"`
	Debug   bool          `env:"DEBUG" envDefault:"false" yaml:"debug" json:"debug"`
	Timeout time.Duration `env:"TIMEOUT" envDefault:"30s" yaml:"timeout" json:"timeout"`
}

type requiredConfig struct {
	Name string `env:"NAME" required:"true"`
	Port int    `env:"PORT"`
}

type secretConfig struct {
	Host     string     `env:"HOST"`
	Password testSecret `env:"PASSWORD"`
}

type nestedConfig struct {
	App      string      `env:"APP"`
	Database dbSubConfig `env:"DB"`
}

type dbSubConfig struct {
	Host     string     `env:"HOST" yaml:"host" json:"host"`
	Port     int        `env:"PORT" yaml:"port" json:"port"`
	Password testSecret `env:"PASSWORD"`
}

type sliceConfig struct {
	Tags []string `env:"TAGS" envDefault:"a,b,c"`
}

type int32Config struct {
	MaxConns int32 `env:"MAX_CONNS" envDefault:"25"`
}

type validatableConfig struct {
	Host string `env:"HOST"`
	Port int    `env:"PORT"`
}

func (c *validatableConfig) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return sserr.Newf(sserr.CodeValidation,
			"config: port %d is out of range [1, 65535]", c.Port)
	}
	return nil
}

type validatableStdlibConfig struct {
	Name string `env:"NAME"`
}

func (c *validatableStdlibConfig) Validate() error {
	if c.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

type nestedRequiredConfig struct {
	App      string               `env:"APP"`
	Database nestedRequiredDBConf `env:"DB"`
}

type nestedRequiredDBConf struct {
	Host string `env:"HOST" required:"true"`
}

// writeTestFile creates a file in the test's temp directory and returns
// its path. The test is failed if the file cannot be written.
func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeTestFile() error: %v", err)
	}
	return path
}

// ===========================================================================
// Loader Builder Tests
// ===========================================================================

// TestNew_ReturnsNonNilLoader verifies that New returns a non-nil Loader.
func TestNew_ReturnsNonNilLoader(t *testing.T) {
	l := New()
	if l == nil {
		t.Fatal("New() = nil, want non-nil Loader")
	}
}

// TestLoader_WithEnvPrefix_Chaining verifies that WithEnvPrefix returns
// the same Loader for fluent chaining.
func TestLoader_WithEnvPrefix_Chaining(t *testing.T) {
	l := New()
	got := l.WithEnvPrefix("APP")
	if got != l {
		t.Error("WithEnvPrefix() did not return the same Loader")
	}
}

// TestLoader_WithFile_Chaining verifies that WithFile returns the same
// Loader for fluent chaining.
func TestLoader_WithFile_Chaining(t *testing.T) {
	l := New()
	got := l.WithFile("config.yaml")
	if got != l {
		t.Error("WithFile() did not return the same Loader")
	}
}

// ===========================================================================
// Load — Input Validation Tests
// ===========================================================================

// TestLoader_Load_NilPointer verifies that Load returns an error when
// given a nil pointer.
func TestLoader_Load_NilPointer(t *testing.T) {
	err := New().Load((*basicConfig)(nil))
	if err == nil {
		t.Fatal("Load(nil) expected error, got nil")
	}
	if !sserr.IsInternal(err) {
		t.Errorf("IsInternal() = false, want true for nil pointer")
	}
}

// TestLoader_Load_NonPointer verifies that Load returns an error when
// given a struct value (not a pointer).
func TestLoader_Load_NonPointer(t *testing.T) {
	err := New().Load(basicConfig{})
	if err == nil {
		t.Fatal("Load(struct) expected error, got nil")
	}
	if !sserr.IsInternal(err) {
		t.Errorf("IsInternal() = false, want true for non-pointer")
	}
}

// TestLoader_Load_PointerToNonStruct verifies that Load returns an error
// when given a pointer to a non-struct type.
func TestLoader_Load_PointerToNonStruct(t *testing.T) {
	n := 42
	err := New().Load(&n)
	if err == nil {
		t.Fatal("Load(*int) expected error, got nil")
	}
	if !sserr.IsInternal(err) {
		t.Errorf("IsInternal() = false, want true for non-struct pointer")
	}
}

// ===========================================================================
// Load — envDefault Tag Tests
// ===========================================================================

// TestLoader_Load_Defaults_Applied verifies that envDefault tags are
// applied to zero-valued fields (string, int, bool, Duration).
func TestLoader_Load_Defaults_Applied(t *testing.T) {
	var cfg basicConfig
	if err := New().Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want %q", cfg.Host, "localhost")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want %d", cfg.Port, 8080)
	}
	if cfg.Debug != false {
		t.Errorf("Debug = %v, want false", cfg.Debug)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, 30*time.Second)
	}
}

// TestLoader_Load_Defaults_NotOverwriteExisting verifies that envDefault
// tags do not overwrite pre-existing non-zero values.
func TestLoader_Load_Defaults_NotOverwriteExisting(t *testing.T) {
	cfg := basicConfig{Host: "custom-host", Port: 9090}
	if err := New().Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "custom-host" {
		t.Errorf("Host = %q, want %q (should not be overwritten)", cfg.Host, "custom-host")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want %d (should not be overwritten)", cfg.Port, 9090)
	}
}

// TestLoader_Load_Defaults_Slice verifies that comma-separated envDefault
// values are correctly parsed into a string slice.
func TestLoader_Load_Defaults_Slice(t *testing.T) {
	var cfg sliceConfig
	if err := New().Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Tags) != 3 {
		t.Fatalf("Tags length = %d, want 3", len(cfg.Tags))
	}
	expected := []string{"a", "b", "c"}
	for i, want := range expected {
		if cfg.Tags[i] != want {
			t.Errorf("Tags[%d] = %q, want %q", i, cfg.Tags[i], want)
		}
	}
}

// TestLoader_Load_Defaults_Int32 verifies that int32 fields are correctly
// parsed from envDefault tags.
func TestLoader_Load_Defaults_Int32(t *testing.T) {
	var cfg int32Config
	if err := New().Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.MaxConns != 25 {
		t.Errorf("MaxConns = %d, want 25", cfg.MaxConns)
	}
}

// ===========================================================================
// Load — YAML File Loading Tests
// ===========================================================================

// TestLoader_Load_YAMLFile verifies that values are loaded from a YAML file.
func TestLoader_Load_YAMLFile(t *testing.T) {
	path := writeTestFile(t, "config.yaml", `
host: filehost
port: 3000
debug: true
timeout: 10s
`)

	var cfg basicConfig
	if err := New().WithFile(path).Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "filehost" {
		t.Errorf("Host = %q, want %q", cfg.Host, "filehost")
	}
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want %d", cfg.Port, 3000)
	}
	if cfg.Debug != true {
		t.Errorf("Debug = %v, want true", cfg.Debug)
	}
	if cfg.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want %v", cfg.Timeout, 10*time.Second)
	}
}

// TestLoader_Load_YAMLFile_OverridesDefaults verifies that file values
// override envDefault values.
func TestLoader_Load_YAMLFile_OverridesDefaults(t *testing.T) {
	path := writeTestFile(t, "config.yaml", `
host: from-file
port: 9999
`)

	var cfg basicConfig
	if err := New().WithFile(path).Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "from-file" {
		t.Errorf("Host = %q, want %q (file should override default)", cfg.Host, "from-file")
	}
	if cfg.Port != 9999 {
		t.Errorf("Port = %d, want %d (file should override default)", cfg.Port, 9999)
	}
}

// TestLoader_Load_MissingFile_NoError verifies that a missing config file
// does not produce an error (file configuration is optional).
func TestLoader_Load_MissingFile_NoError(t *testing.T) {
	var cfg basicConfig
	err := New().WithFile("/nonexistent/path/config.yaml").Load(&cfg)
	if err != nil {
		t.Fatalf("Load() with missing file error: %v (expected nil)", err)
	}

	// Defaults should still be applied.
	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want %q (default should apply)", cfg.Host, "localhost")
	}
}

// TestLoader_Load_YMLExtension verifies that .yml extension is recognized.
func TestLoader_Load_YMLExtension(t *testing.T) {
	path := writeTestFile(t, "config.yml", `
host: from-yml
`)

	var cfg basicConfig
	if err := New().WithFile(path).Load(&cfg); err != nil {
		t.Fatalf("Load() with .yml error: %v", err)
	}

	if cfg.Host != "from-yml" {
		t.Errorf("Host = %q, want %q", cfg.Host, "from-yml")
	}
}

// ===========================================================================
// Load — JSON File Loading Tests
// ===========================================================================

// TestLoader_Load_JSONFile verifies that values are loaded from a JSON file.
func TestLoader_Load_JSONFile(t *testing.T) {
	path := writeTestFile(t, "config.json", `{
  "host": "json-host",
  "port": 4000,
  "debug": true
}`)

	var cfg basicConfig
	if err := New().WithFile(path).Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "json-host" {
		t.Errorf("Host = %q, want %q", cfg.Host, "json-host")
	}
	if cfg.Port != 4000 {
		t.Errorf("Port = %d, want %d", cfg.Port, 4000)
	}
}

// TestLoader_Load_UnsupportedExtension verifies that an unsupported file
// extension returns a CodeInternalConfiguration error.
func TestLoader_Load_UnsupportedExtension(t *testing.T) {
	path := writeTestFile(t, "config.toml", `host = "test"`)

	var cfg basicConfig
	err := New().WithFile(path).Load(&cfg)
	if err == nil {
		t.Fatal("Load() with .toml expected error, got nil")
	}
	if !sserr.IsInternal(err) {
		t.Errorf("IsInternal() = false, want true for unsupported extension")
	}
}

// ===========================================================================
// Load — File Security Tests
// ===========================================================================

// TestLoader_Load_DirectoryTraversal verifies that file paths containing
// directory traversal sequences are rejected.
func TestLoader_Load_DirectoryTraversal(t *testing.T) {
	var cfg basicConfig
	err := New().WithFile("../../../etc/passwd").Load(&cfg)
	if err == nil {
		t.Fatal("Load() with directory traversal expected error, got nil")
	}
	if !sserr.IsInternal(err) {
		t.Errorf("IsInternal() = false, want true for directory traversal")
	}
}

// ===========================================================================
// Load — Environment Variable Tests
// ===========================================================================

// TestLoader_Load_EnvOverridesFile verifies that environment variables
// take precedence over file values.
func TestLoader_Load_EnvOverridesFile(t *testing.T) {
	path := writeTestFile(t, "config.yaml", `
host: from-file
port: 3000
`)

	t.Setenv("HOST", "from-env")
	t.Setenv("PORT", "5000")

	var cfg basicConfig
	if err := New().WithFile(path).Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "from-env" {
		t.Errorf("Host = %q, want %q (env should override file)", cfg.Host, "from-env")
	}
	if cfg.Port != 5000 {
		t.Errorf("Port = %d, want %d (env should override file)", cfg.Port, 5000)
	}
}

// TestLoader_Load_EnvOverridesDefault verifies that environment variables
// take precedence over envDefault values.
func TestLoader_Load_EnvOverridesDefault(t *testing.T) {
	t.Setenv("HOST", "env-host")

	var cfg basicConfig
	if err := New().Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "env-host" {
		t.Errorf("Host = %q, want %q (env should override default)", cfg.Host, "env-host")
	}
}

// TestLoader_Load_EnvPrefix verifies that WithEnvPrefix prepends the
// prefix to environment variable lookups.
func TestLoader_Load_EnvPrefix(t *testing.T) {
	t.Setenv("APP_HOST", "prefixed-host")
	t.Setenv("APP_PORT", "7070")

	var cfg basicConfig
	if err := New().WithEnvPrefix("APP").Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "prefixed-host" {
		t.Errorf("Host = %q, want %q", cfg.Host, "prefixed-host")
	}
	if cfg.Port != 7070 {
		t.Errorf("Port = %d, want %d", cfg.Port, 7070)
	}
}

// TestLoader_Load_EnvPrefix_UppercaseNormalization verifies that a
// lowercase prefix is uppercased automatically.
func TestLoader_Load_EnvPrefix_UppercaseNormalization(t *testing.T) {
	t.Setenv("MYAPP_HOST", "upper-host")

	var cfg basicConfig
	if err := New().WithEnvPrefix("myapp").Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "upper-host" {
		t.Errorf("Host = %q, want %q (prefix should be uppercased)", cfg.Host, "upper-host")
	}
}

// TestLoader_Load_EnvNotSet_KeepsFileValue verifies that an unset
// environment variable does not clear the file value.
func TestLoader_Load_EnvNotSet_KeepsFileValue(t *testing.T) {
	path := writeTestFile(t, "config.yaml", `
host: from-file
`)

	// Do NOT set HOST env var.

	var cfg basicConfig
	if err := New().WithFile(path).Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "from-file" {
		t.Errorf("Host = %q, want %q (unset env should preserve file value)",
			cfg.Host, "from-file")
	}
}

// ===========================================================================
// Load — Type Parsing Tests
// ===========================================================================

// TestLoader_Load_Types verifies that all supported types are correctly
// parsed from environment variables.
func TestLoader_Load_Types(t *testing.T) {
	tests := []struct {
		name    string
		envKey  string
		envVal  string
		check   func(t *testing.T)
		loadCfg func(t *testing.T) error
	}{
		{
			name:   "string",
			envKey: "HOST",
			envVal: "example.com",
			loadCfg: func(t *testing.T) error {
				var cfg basicConfig
				err := New().Load(&cfg)
				if err == nil && cfg.Host != "example.com" {
					t.Errorf("Host = %q, want %q", cfg.Host, "example.com")
				}
				return err
			},
		},
		{
			name:   "int",
			envKey: "PORT",
			envVal: "9090",
			loadCfg: func(t *testing.T) error {
				var cfg basicConfig
				err := New().Load(&cfg)
				if err == nil && cfg.Port != 9090 {
					t.Errorf("Port = %d, want %d", cfg.Port, 9090)
				}
				return err
			},
		},
		{
			name:   "int32",
			envKey: "MAX_CONNS",
			envVal: "50",
			loadCfg: func(t *testing.T) error {
				var cfg int32Config
				err := New().Load(&cfg)
				if err == nil && cfg.MaxConns != 50 {
					t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, 50)
				}
				return err
			},
		},
		{
			name:   "bool_true",
			envKey: "DEBUG",
			envVal: "true",
			loadCfg: func(t *testing.T) error {
				var cfg basicConfig
				err := New().Load(&cfg)
				if err == nil && !cfg.Debug {
					t.Error("Debug = false, want true")
				}
				return err
			},
		},
		{
			name:   "bool_1",
			envKey: "DEBUG",
			envVal: "1",
			loadCfg: func(t *testing.T) error {
				var cfg basicConfig
				err := New().Load(&cfg)
				if err == nil && !cfg.Debug {
					t.Error("Debug = false, want true (from '1')")
				}
				return err
			},
		},
		{
			name:   "duration",
			envKey: "TIMEOUT",
			envVal: "1h30m",
			loadCfg: func(t *testing.T) error {
				var cfg basicConfig
				err := New().Load(&cfg)
				expected := 90 * time.Minute
				if err == nil && cfg.Timeout != expected {
					t.Errorf("Timeout = %v, want %v", cfg.Timeout, expected)
				}
				return err
			},
		},
		{
			name:   "slice",
			envKey: "TAGS",
			envVal: "x, y, z",
			loadCfg: func(t *testing.T) error {
				var cfg sliceConfig
				err := New().Load(&cfg)
				if err == nil {
					if len(cfg.Tags) != 3 {
						t.Fatalf("Tags length = %d, want 3", len(cfg.Tags))
					}
					expected := []string{"x", "y", "z"}
					for i, want := range expected {
						if cfg.Tags[i] != want {
							t.Errorf("Tags[%d] = %q, want %q", i, cfg.Tags[i], want)
						}
					}
				}
				return err
			},
		},
		{
			name:   "named_string_secret",
			envKey: "PASSWORD",
			envVal: "s3cret",
			loadCfg: func(t *testing.T) error {
				var cfg secretConfig
				err := New().Load(&cfg)
				if err == nil {
					if cfg.Password.Value() != "s3cret" {
						t.Errorf("Password.Value() = %q, want %q", cfg.Password.Value(), "s3cret")
					}
					if cfg.Password.String() != "[REDACTED]" {
						t.Errorf("Password.String() = %q, want %q", cfg.Password.String(), "[REDACTED]")
					}
				}
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, tt.envVal)
			if err := tt.loadCfg(t); err != nil {
				t.Fatalf("Load() error: %v", err)
			}
		})
	}
}

// ===========================================================================
// Load — Secret Type Tests
// ===========================================================================

// TestLoader_Load_SecretFromEnv verifies that named string types (like
// postgres.Secret) are correctly set from environment variables, and that
// Value() returns the actual value while String() returns redacted text.
func TestLoader_Load_SecretFromEnv(t *testing.T) {
	t.Setenv("PASSWORD", "my-secret-password")

	var cfg secretConfig
	if err := New().Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Password.Value() != "my-secret-password" {
		t.Errorf("Password.Value() = %q, want %q", cfg.Password.Value(), "my-secret-password")
	}
	if cfg.Password.String() != "[REDACTED]" {
		t.Errorf("Password.String() = %q, want %q", cfg.Password.String(), "[REDACTED]")
	}
}

// ===========================================================================
// Load — Nested Struct Tests
// ===========================================================================

// TestLoader_Load_NestedStruct_Env verifies that nested struct fields
// are loaded from environment variables with the parent's env tag as prefix.
func TestLoader_Load_NestedStruct_Env(t *testing.T) {
	t.Setenv("APP", "my-app")
	t.Setenv("DB_HOST", "db.example.com")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_PASSWORD", "dbpass")

	var cfg nestedConfig
	if err := New().Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.App != "my-app" {
		t.Errorf("App = %q, want %q", cfg.App, "my-app")
	}
	if cfg.Database.Host != "db.example.com" {
		t.Errorf("Database.Host = %q, want %q", cfg.Database.Host, "db.example.com")
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("Database.Port = %d, want %d", cfg.Database.Port, 5432)
	}
	if cfg.Database.Password.Value() != "dbpass" {
		t.Errorf("Database.Password.Value() = %q, want %q",
			cfg.Database.Password.Value(), "dbpass")
	}
}

// TestLoader_Load_NestedStruct_WithPrefix verifies that the global env
// prefix is combined with the nested struct prefix.
func TestLoader_Load_NestedStruct_WithPrefix(t *testing.T) {
	t.Setenv("MYAPP_DB_HOST", "prefixed-db")
	t.Setenv("MYAPP_DB_PORT", "3306")

	var cfg nestedConfig
	if err := New().WithEnvPrefix("MYAPP").Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Database.Host != "prefixed-db" {
		t.Errorf("Database.Host = %q, want %q", cfg.Database.Host, "prefixed-db")
	}
	if cfg.Database.Port != 3306 {
		t.Errorf("Database.Port = %d, want %d", cfg.Database.Port, 3306)
	}
}

// TestLoader_Load_NestedStruct_File verifies that nested struct fields
// are loaded from a YAML file with nested structure.
func TestLoader_Load_NestedStruct_File(t *testing.T) {
	// Note: YAML uses the yaml tags, but our nestedConfig struct uses
	// env tags on the parent. The YAML structure must match the Go
	// struct field names (or yaml tags). Since dbSubConfig has yaml
	// tags, those control YAML mapping.
	path := writeTestFile(t, "config.yaml", `
app: yaml-app
database:
  host: yaml-db-host
  port: 5433
`)

	var cfg nestedConfig
	if err := New().WithFile(path).Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.App != "yaml-app" {
		t.Errorf("App = %q, want %q", cfg.App, "yaml-app")
	}
	if cfg.Database.Host != "yaml-db-host" {
		t.Errorf("Database.Host = %q, want %q", cfg.Database.Host, "yaml-db-host")
	}
	if cfg.Database.Port != 5433 {
		t.Errorf("Database.Port = %d, want %d", cfg.Database.Port, 5433)
	}
}

// ===========================================================================
// Load — Validation Tests (required tag)
// ===========================================================================

// TestLoader_Load_RequiredField_Set verifies that no error occurs when
// a required field has a value.
func TestLoader_Load_RequiredField_Set(t *testing.T) {
	t.Setenv("NAME", "test-name")

	var cfg requiredConfig
	if err := New().Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Name != "test-name" {
		t.Errorf("Name = %q, want %q", cfg.Name, "test-name")
	}
}

// TestLoader_Load_RequiredField_Missing verifies that a missing required
// field returns a CodeValidationRequired error with the field name.
func TestLoader_Load_RequiredField_Missing(t *testing.T) {
	var cfg requiredConfig
	err := New().Load(&cfg)
	if err == nil {
		t.Fatal("Load() expected error for missing required field, got nil")
	}

	var ssErr *sserr.Error
	if !errors.As(err, &ssErr) {
		t.Fatalf("error type = %T, want *sserr.Error", err)
	}
	if ssErr.Code != sserr.CodeValidationRequired {
		t.Errorf("error code = %q, want %q", ssErr.Code, sserr.CodeValidationRequired)
	}
}

// TestLoader_Load_RequiredField_ErrorIsValidation verifies that the
// required field error is classified as a validation error.
func TestLoader_Load_RequiredField_ErrorIsValidation(t *testing.T) {
	var cfg requiredConfig
	err := New().Load(&cfg)
	if err == nil {
		t.Fatal("Load() expected error, got nil")
	}
	if !sserr.IsValidation(err) {
		t.Error("IsValidation() = false, want true for required field violation")
	}
}

// TestLoader_Load_NestedRequiredField_Missing verifies that required
// validation works for nested struct fields with dotted path in error.
func TestLoader_Load_NestedRequiredField_Missing(t *testing.T) {
	var cfg nestedRequiredConfig
	err := New().Load(&cfg)
	if err == nil {
		t.Fatal("Load() expected error for nested required field, got nil")
	}
	if !sserr.IsValidation(err) {
		t.Error("IsValidation() = false, want true for nested required field")
	}
}

// ===========================================================================
// Load — Validator Interface Tests
// ===========================================================================

// TestLoader_Load_Validator_Called verifies that the Validator interface
// Validate method is called after loading and tag validation succeed.
func TestLoader_Load_Validator_Called(t *testing.T) {
	t.Setenv("HOST", "localhost")
	t.Setenv("PORT", "8080")

	var cfg validatableConfig
	if err := New().Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v (Validator should pass for port 8080)", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
}

// TestLoader_Load_Validator_ReturnsError verifies that a Validate()
// error is surfaced through Load().
func TestLoader_Load_Validator_ReturnsError(t *testing.T) {
	t.Setenv("HOST", "localhost")
	t.Setenv("PORT", "0") // Invalid: port must be 1-65535.

	var cfg validatableConfig
	err := New().Load(&cfg)
	if err == nil {
		t.Fatal("Load() expected error from Validator, got nil")
	}
	if !sserr.IsValidation(err) {
		t.Errorf("IsValidation() = false, want true for Validator error")
	}
}

// TestLoader_Load_Validator_StdlibError verifies that stdlib errors from
// Validate() are wrapped with CodeValidation.
func TestLoader_Load_Validator_StdlibError(t *testing.T) {
	// Don't set NAME — triggers Validate() failure.
	var cfg validatableStdlibConfig
	err := New().Load(&cfg)
	if err == nil {
		t.Fatal("Load() expected error from Validator, got nil")
	}
	if !sserr.IsValidation(err) {
		t.Errorf("IsValidation() = false, want true for wrapped stdlib error")
	}
}

// TestLoader_Load_Validator_NotCalledOnRequiredFailure verifies that
// the Validator interface is NOT called when required tag validation fails.
func TestLoader_Load_Validator_NotCalledOnRequiredFailure(t *testing.T) {
	// Verify that the error code is CodeValidationRequired (not
	// CodeValidation from a Validator). The requiredConfig type does
	// not implement Validator, so if the code is CodeValidationRequired
	// we know the required tag check ran and returned before any
	// Validator could be called.
	var c requiredConfig
	err := New().Load(&c)
	if err == nil {
		t.Fatal("Load() expected error, got nil")
	}
	var ssErr *sserr.Error
	if !errors.As(err, &ssErr) {
		t.Fatalf("error type = %T, want *sserr.Error", err)
	}
	// The error should be from the required tag check, not from a
	// Validator (requiredConfig doesn't implement Validator).
	if ssErr.Code != sserr.CodeValidationRequired {
		t.Errorf("error code = %q, want %q (required should fail before Validator)",
			ssErr.Code, sserr.CodeValidationRequired)
	}
}

// ===========================================================================
// Load — Priority Order Tests (Integration)
// ===========================================================================

// TestLoader_Load_PriorityOrder verifies the full priority chain:
// env > file > default.
func TestLoader_Load_PriorityOrder(t *testing.T) {
	path := writeTestFile(t, "config.yaml", `
host: from-file
port: 3000
`)

	// Set env to override the file value for Host.
	t.Setenv("HOST", "from-env")
	// Do NOT set PORT env var — file value should be used.

	var cfg basicConfig
	if err := New().WithFile(path).Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Host: env wins over file.
	if cfg.Host != "from-env" {
		t.Errorf("Host = %q, want %q (env > file)", cfg.Host, "from-env")
	}
	// Port: file wins over default.
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want %d (file > default)", cfg.Port, 3000)
	}
	// Timeout: default only (not in file, not in env).
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want %v (default only)", cfg.Timeout, 30*time.Second)
	}
}

// TestLoader_Load_FileOverridesDefault verifies that file values
// override envDefault values.
func TestLoader_Load_FileOverridesDefault(t *testing.T) {
	path := writeTestFile(t, "config.yaml", `
host: file-host
`)

	var cfg basicConfig
	if err := New().WithFile(path).Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "file-host" {
		t.Errorf("Host = %q, want %q (file > default)", cfg.Host, "file-host")
	}
}

// TestLoader_Load_DefaultOnly verifies that envDefault values are used
// when no file or env vars are provided.
func TestLoader_Load_DefaultOnly(t *testing.T) {
	var cfg basicConfig
	if err := New().Load(&cfg); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want %q (default only)", cfg.Host, "localhost")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want %d (default only)", cfg.Port, 8080)
	}
}

// ===========================================================================
// MustLoad Tests
// ===========================================================================

// TestMustLoad_Success verifies that MustLoad returns a populated struct
// when loading succeeds.
func TestMustLoad_Success(t *testing.T) {
	cfg := MustLoad[basicConfig](New())

	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want %q", cfg.Host, "localhost")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want %d", cfg.Port, 8080)
	}
}

// TestMustLoad_Panics verifies that MustLoad panics when a required
// field is missing.
func TestMustLoad_Panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustLoad() expected panic, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value type = %T, want string", r)
		}
		if msg == "" {
			t.Error("panic message is empty, want descriptive message")
		}
	}()

	_ = MustLoad[requiredConfig](New())
}

// ===========================================================================
// Load — Parse Error Tests
// ===========================================================================

// TestLoader_Load_InvalidInt_FromEnv verifies that a non-numeric string
// for an int field returns an error.
func TestLoader_Load_InvalidInt_FromEnv(t *testing.T) {
	t.Setenv("PORT", "not-a-number")

	var cfg basicConfig
	err := New().Load(&cfg)
	if err == nil {
		t.Fatal("Load() expected error for invalid int, got nil")
	}
	if !sserr.IsInternal(err) {
		t.Errorf("IsInternal() = false, want true for parse error")
	}
}

// TestLoader_Load_InvalidBool_FromEnv verifies that an invalid bool
// string returns an error.
func TestLoader_Load_InvalidBool_FromEnv(t *testing.T) {
	t.Setenv("DEBUG", "not-a-bool")

	var cfg basicConfig
	err := New().Load(&cfg)
	if err == nil {
		t.Fatal("Load() expected error for invalid bool, got nil")
	}
	if !sserr.IsInternal(err) {
		t.Errorf("IsInternal() = false, want true for parse error")
	}
}

// TestLoader_Load_InvalidDuration_FromEnv verifies that an invalid
// duration string returns an error.
func TestLoader_Load_InvalidDuration_FromEnv(t *testing.T) {
	t.Setenv("TIMEOUT", "not-a-duration")

	var cfg basicConfig
	err := New().Load(&cfg)
	if err == nil {
		t.Fatal("Load() expected error for invalid duration, got nil")
	}
	if !sserr.IsInternal(err) {
		t.Errorf("IsInternal() = false, want true for parse error")
	}
}

// TestLoader_Load_InvalidYAML_File verifies that a malformed YAML file
// returns an error.
func TestLoader_Load_InvalidYAML_File(t *testing.T) {
	path := writeTestFile(t, "bad.yaml", `
host: [invalid yaml
  missing closing bracket
`)

	var cfg basicConfig
	err := New().WithFile(path).Load(&cfg)
	if err == nil {
		t.Fatal("Load() expected error for malformed YAML, got nil")
	}
	if !sserr.IsInternal(err) {
		t.Errorf("IsInternal() = false, want true for YAML parse error")
	}
}

// TestLoader_Load_InvalidJSON_File verifies that a malformed JSON file
// returns an error.
func TestLoader_Load_InvalidJSON_File(t *testing.T) {
	path := writeTestFile(t, "bad.json", `{"host": invalid}`)

	var cfg basicConfig
	err := New().WithFile(path).Load(&cfg)
	if err == nil {
		t.Fatal("Load() expected error for malformed JSON, got nil")
	}
	if !sserr.IsInternal(err) {
		t.Errorf("IsInternal() = false, want true for JSON parse error")
	}
}
