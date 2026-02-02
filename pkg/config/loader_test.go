package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// requiredValidatableConfig implements both required:"true" tags AND the
// Validator interface. Used to prove that Validate() is NOT called when
// required tag validation fails first.
type requiredValidatableConfig struct {
	Name          string `env:"NAME" required:"true"`
	validateCalls *int   // tracks whether Validate() was invoked
}

func (c *requiredValidatableConfig) Validate() error {
	if c.validateCalls != nil {
		*c.validateCalls++
	}
	return nil
}

// namedSlice is a named slice type to verify that setField handles
// named slice types without panicking (uses reflect.MakeSlice instead
// of reflect.ValueOf).
type namedSlice []string

type namedSliceConfig struct {
	Tags namedSlice `env:"TAGS" envDefault:"a,b,c"`
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
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err, "writeTestFile() error")
	return path
}

// ===========================================================================
// Loader Builder Tests
// ===========================================================================

// TestNew_ReturnsNonNilLoader verifies that New returns a non-nil Loader.
func TestNew_ReturnsNonNilLoader(t *testing.T) {
	t.Parallel()
	l := New()
	require.NotNil(t, l, "New() = nil, want non-nil Loader")
}

// TestLoader_WithEnvPrefix_Chaining verifies that WithEnvPrefix returns
// the same Loader for fluent chaining.
func TestLoader_WithEnvPrefix_Chaining(t *testing.T) {
	t.Parallel()
	l := New()
	got := l.WithEnvPrefix("APP")
	assert.Equal(t, l, got, "WithEnvPrefix() did not return the same Loader")
}

// TestLoader_WithFile_Chaining verifies that WithFile returns the same
// Loader for fluent chaining.
func TestLoader_WithFile_Chaining(t *testing.T) {
	t.Parallel()
	l := New()
	got := l.WithFile("config.yaml")
	assert.Equal(t, l, got, "WithFile() did not return the same Loader")
}

// ===========================================================================
// Load — Input Validation Tests
// ===========================================================================

// TestLoader_Load_NilPointer verifies that Load returns an error when
// given a nil pointer.
func TestLoader_Load_NilPointer(t *testing.T) {
	t.Parallel()
	err := New().Load((*basicConfig)(nil))
	require.Error(t, err, "Load(nil) expected error, got nil")
	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for nil pointer")
}

// TestLoader_Load_NonPointer verifies that Load returns an error when
// given a struct value (not a pointer).
func TestLoader_Load_NonPointer(t *testing.T) {
	t.Parallel()
	err := New().Load(basicConfig{})
	require.Error(t, err, "Load(struct) expected error, got nil")
	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for non-pointer")
}

// TestLoader_Load_PointerToNonStruct verifies that Load returns an error
// when given a pointer to a non-struct type.
func TestLoader_Load_PointerToNonStruct(t *testing.T) {
	t.Parallel()
	n := 42
	err := New().Load(&n)
	require.Error(t, err, "Load(*int) expected error, got nil")
	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for non-struct pointer")
}

// ===========================================================================
// Load — envDefault Tag Tests
// ===========================================================================

// TestLoader_Load_Defaults_Applied verifies that envDefault tags are
// applied to zero-valued fields (string, int, bool, Duration).
func TestLoader_Load_Defaults_Applied(t *testing.T) {
	t.Parallel()
	var cfg basicConfig
	require.NoError(t, New().Load(&cfg))

	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, false, cfg.Debug)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
}

// TestLoader_Load_Defaults_NotOverwriteExisting verifies that envDefault
// tags do not overwrite pre-existing non-zero values.
func TestLoader_Load_Defaults_NotOverwriteExisting(t *testing.T) {
	t.Parallel()
	cfg := basicConfig{Host: "custom-host", Port: 9090}
	require.NoError(t, New().Load(&cfg))

	assert.Equal(t, "custom-host", cfg.Host, "should not be overwritten")
	assert.Equal(t, 9090, cfg.Port, "should not be overwritten")
}

// TestLoader_Load_Defaults_Slice verifies that comma-separated envDefault
// values are correctly parsed into a string slice.
func TestLoader_Load_Defaults_Slice(t *testing.T) {
	t.Parallel()
	var cfg sliceConfig
	require.NoError(t, New().Load(&cfg))

	require.Len(t, cfg.Tags, 3)
	expected := []string{"a", "b", "c"}
	for i, want := range expected {
		assert.Equal(t, want, cfg.Tags[i], "Tags[%d]", i)
	}
}

// TestLoader_Load_Defaults_Int32 verifies that int32 fields are correctly
// parsed from envDefault tags.
func TestLoader_Load_Defaults_Int32(t *testing.T) {
	t.Parallel()
	var cfg int32Config
	require.NoError(t, New().Load(&cfg))

	assert.Equal(t, int32(25), cfg.MaxConns)
}

// ===========================================================================
// Load — YAML File Loading Tests
// ===========================================================================

// TestLoader_Load_YAMLFile verifies that values are loaded from a YAML file.
func TestLoader_Load_YAMLFile(t *testing.T) {
	t.Parallel()
	path := writeTestFile(t, "config.yaml", `
host: filehost
port: 3000
debug: true
timeout: 10s
`)

	var cfg basicConfig
	require.NoError(t, New().WithFile(path).Load(&cfg))

	assert.Equal(t, "filehost", cfg.Host)
	assert.Equal(t, 3000, cfg.Port)
	assert.Equal(t, true, cfg.Debug)
	assert.Equal(t, 10*time.Second, cfg.Timeout)
}

// TestLoader_Load_YAMLFile_OverridesDefaults verifies that file values
// override envDefault values.
func TestLoader_Load_YAMLFile_OverridesDefaults(t *testing.T) {
	t.Parallel()
	path := writeTestFile(t, "config.yaml", `
host: from-file
port: 9999
`)

	var cfg basicConfig
	require.NoError(t, New().WithFile(path).Load(&cfg))

	assert.Equal(t, "from-file", cfg.Host, "file should override default")
	assert.Equal(t, 9999, cfg.Port, "file should override default")
}

// TestLoader_Load_MissingFile_NoError verifies that a missing config file
// does not produce an error (file configuration is optional).
func TestLoader_Load_MissingFile_NoError(t *testing.T) {
	t.Parallel()
	var cfg basicConfig
	err := New().WithFile("/nonexistent/path/config.yaml").Load(&cfg)
	require.NoError(t, err, "Load() with missing file should not error")

	// Defaults should still be applied.
	assert.Equal(t, "localhost", cfg.Host, "default should apply")
}

// TestLoader_Load_YMLExtension verifies that .yml extension is recognized.
func TestLoader_Load_YMLExtension(t *testing.T) {
	t.Parallel()
	path := writeTestFile(t, "config.yml", `
host: from-yml
`)

	var cfg basicConfig
	require.NoError(t, New().WithFile(path).Load(&cfg))

	assert.Equal(t, "from-yml", cfg.Host)
}

// ===========================================================================
// Load — JSON File Loading Tests
// ===========================================================================

// TestLoader_Load_JSONFile verifies that values are loaded from a JSON file.
func TestLoader_Load_JSONFile(t *testing.T) {
	t.Parallel()
	path := writeTestFile(t, "config.json", `{
  "host": "json-host",
  "port": 4000,
  "debug": true
}`)

	var cfg basicConfig
	require.NoError(t, New().WithFile(path).Load(&cfg))

	assert.Equal(t, "json-host", cfg.Host)
	assert.Equal(t, 4000, cfg.Port)
}

// TestLoader_Load_UnsupportedExtension verifies that an unsupported file
// extension returns a CodeInternalConfiguration error.
func TestLoader_Load_UnsupportedExtension(t *testing.T) {
	t.Parallel()
	path := writeTestFile(t, "config.toml", `host = "test"`)

	var cfg basicConfig
	err := New().WithFile(path).Load(&cfg)
	require.Error(t, err, "Load() with .toml expected error, got nil")
	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for unsupported extension")
}

// ===========================================================================
// Load — File Security Tests
// ===========================================================================

// TestLoader_Load_DirectoryTraversal verifies that file paths containing
// directory traversal sequences are rejected.
func TestLoader_Load_DirectoryTraversal(t *testing.T) {
	t.Parallel()
	var cfg basicConfig
	err := New().WithFile("../../../etc/passwd").Load(&cfg)
	require.Error(t, err, "Load() with directory traversal expected error, got nil")
	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for directory traversal")
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
	require.NoError(t, New().WithFile(path).Load(&cfg))

	assert.Equal(t, "from-env", cfg.Host, "env should override file")
	assert.Equal(t, 5000, cfg.Port, "env should override file")
}

// TestLoader_Load_EnvOverridesDefault verifies that environment variables
// take precedence over envDefault values.
func TestLoader_Load_EnvOverridesDefault(t *testing.T) {
	t.Setenv("HOST", "env-host")

	var cfg basicConfig
	require.NoError(t, New().Load(&cfg))

	assert.Equal(t, "env-host", cfg.Host, "env should override default")
}

// TestLoader_Load_EnvPrefix verifies that WithEnvPrefix prepends the
// prefix to environment variable lookups.
func TestLoader_Load_EnvPrefix(t *testing.T) {
	t.Setenv("APP_HOST", "prefixed-host")
	t.Setenv("APP_PORT", "7070")

	var cfg basicConfig
	require.NoError(t, New().WithEnvPrefix("APP").Load(&cfg))

	assert.Equal(t, "prefixed-host", cfg.Host)
	assert.Equal(t, 7070, cfg.Port)
}

// TestLoader_Load_EnvPrefix_UppercaseNormalization verifies that a
// lowercase prefix is uppercased automatically.
func TestLoader_Load_EnvPrefix_UppercaseNormalization(t *testing.T) {
	t.Setenv("MYAPP_HOST", "upper-host")

	var cfg basicConfig
	require.NoError(t, New().WithEnvPrefix("myapp").Load(&cfg))

	assert.Equal(t, "upper-host", cfg.Host, "prefix should be uppercased")
}

// TestLoader_Load_EnvNotSet_KeepsFileValue verifies that an unset
// environment variable does not clear the file value.
func TestLoader_Load_EnvNotSet_KeepsFileValue(t *testing.T) {
	t.Parallel()
	path := writeTestFile(t, "config.yaml", `
host: from-file
`)

	// Do NOT set HOST env var.

	var cfg basicConfig
	require.NoError(t, New().WithFile(path).Load(&cfg))

	assert.Equal(t, "from-file", cfg.Host, "unset env should preserve file value")
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
		loadCfg func(t *testing.T) error
	}{
		{
			name:   "string",
			envKey: "HOST",
			envVal: "example.com",
			loadCfg: func(t *testing.T) error {
				var cfg basicConfig
				err := New().Load(&cfg)
				if err == nil {
					assert.Equal(t, "example.com", cfg.Host)
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
				if err == nil {
					assert.Equal(t, 9090, cfg.Port)
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
				if err == nil {
					assert.Equal(t, int32(50), cfg.MaxConns)
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
				if err == nil {
					assert.True(t, cfg.Debug, "Debug = false, want true")
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
				if err == nil {
					assert.True(t, cfg.Debug, "Debug = false, want true (from '1')")
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
				if err == nil {
					assert.Equal(t, expected, cfg.Timeout)
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
					require.Len(t, cfg.Tags, 3)
					expected := []string{"x", "y", "z"}
					for i, want := range expected {
						assert.Equal(t, want, cfg.Tags[i], "Tags[%d]", i)
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
					assert.Equal(t, "s3cret", cfg.Password.Value())
					assert.Equal(t, "[REDACTED]", cfg.Password.String())
				}
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, tt.envVal)
			require.NoError(t, tt.loadCfg(t))
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
	require.NoError(t, New().Load(&cfg))

	assert.Equal(t, "my-secret-password", cfg.Password.Value())
	assert.Equal(t, "[REDACTED]", cfg.Password.String())
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
	require.NoError(t, New().Load(&cfg))

	assert.Equal(t, "my-app", cfg.App)
	assert.Equal(t, "db.example.com", cfg.Database.Host)
	assert.Equal(t, 5432, cfg.Database.Port)
	assert.Equal(t, "dbpass", cfg.Database.Password.Value())
}

// TestLoader_Load_NestedStruct_WithPrefix verifies that the global env
// prefix is combined with the nested struct prefix.
func TestLoader_Load_NestedStruct_WithPrefix(t *testing.T) {
	t.Setenv("MYAPP_DB_HOST", "prefixed-db")
	t.Setenv("MYAPP_DB_PORT", "3306")

	var cfg nestedConfig
	require.NoError(t, New().WithEnvPrefix("MYAPP").Load(&cfg))

	assert.Equal(t, "prefixed-db", cfg.Database.Host)
	assert.Equal(t, 3306, cfg.Database.Port)
}

// TestLoader_Load_NestedStruct_File verifies that nested struct fields
// are loaded from a YAML file with nested structure.
func TestLoader_Load_NestedStruct_File(t *testing.T) {
	t.Parallel()
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
	require.NoError(t, New().WithFile(path).Load(&cfg))

	assert.Equal(t, "yaml-app", cfg.App)
	assert.Equal(t, "yaml-db-host", cfg.Database.Host)
	assert.Equal(t, 5433, cfg.Database.Port)
}

// ===========================================================================
// Load — Validation Tests (required tag)
// ===========================================================================

// TestLoader_Load_RequiredField_Set verifies that no error occurs when
// a required field has a value.
func TestLoader_Load_RequiredField_Set(t *testing.T) {
	t.Setenv("NAME", "test-name")

	var cfg requiredConfig
	require.NoError(t, New().Load(&cfg))

	assert.Equal(t, "test-name", cfg.Name)
}

// TestLoader_Load_RequiredField_Missing verifies that a missing required
// field returns a CodeValidationRequired error with the field name.
func TestLoader_Load_RequiredField_Missing(t *testing.T) {
	t.Parallel()
	var cfg requiredConfig
	err := New().Load(&cfg)
	require.Error(t, err, "Load() expected error for missing required field, got nil")

	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeValidationRequired, ssErr.Code)
}

// TestLoader_Load_RequiredField_ErrorIsValidation verifies that the
// required field error is classified as a validation error.
func TestLoader_Load_RequiredField_ErrorIsValidation(t *testing.T) {
	t.Parallel()
	var cfg requiredConfig
	err := New().Load(&cfg)
	require.Error(t, err, "Load() expected error, got nil")
	assert.True(t, sserr.IsValidation(err), "IsValidation() = false, want true for required field violation")
}

// TestLoader_Load_NestedRequiredField_Missing verifies that required
// validation works for nested struct fields with dotted path in error.
func TestLoader_Load_NestedRequiredField_Missing(t *testing.T) {
	t.Parallel()
	var cfg nestedRequiredConfig
	err := New().Load(&cfg)
	require.Error(t, err, "Load() expected error for nested required field, got nil")
	assert.True(t, sserr.IsValidation(err), "IsValidation() = false, want true for nested required field")
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
	require.NoError(t, New().Load(&cfg), "Validator should pass for port 8080")

	assert.Equal(t, 8080, cfg.Port)
}

// TestLoader_Load_Validator_ReturnsError verifies that a Validate()
// error is surfaced through Load().
func TestLoader_Load_Validator_ReturnsError(t *testing.T) {
	t.Setenv("HOST", "localhost")
	t.Setenv("PORT", "0") // Invalid: port must be 1-65535.

	var cfg validatableConfig
	err := New().Load(&cfg)
	require.Error(t, err, "Load() expected error from Validator, got nil")
	assert.True(t, sserr.IsValidation(err), "IsValidation() = false, want true for Validator error")
}

// TestLoader_Load_Validator_StdlibError verifies that stdlib errors from
// Validate() are wrapped with CodeValidation.
func TestLoader_Load_Validator_StdlibError(t *testing.T) {
	t.Parallel()
	// Don't set NAME — triggers Validate() failure.
	var cfg validatableStdlibConfig
	err := New().Load(&cfg)
	require.Error(t, err, "Load() expected error from Validator, got nil")
	assert.True(t, sserr.IsValidation(err), "IsValidation() = false, want true for wrapped stdlib error")
}

// TestLoader_Load_Validator_NotCalledOnRequiredFailure verifies that
// the Validator interface is NOT called when required tag validation
// fails. This test uses a struct that implements BOTH required:"true"
// AND the Validator interface, then leaves the required field empty
// and asserts that Validate() was never invoked.
func TestLoader_Load_Validator_NotCalledOnRequiredFailure(t *testing.T) {
	t.Parallel()
	calls := 0
	cfg := requiredValidatableConfig{validateCalls: &calls}

	err := New().Load(&cfg)
	require.Error(t, err, "Load() expected error for missing required field, got nil")

	// The error must come from required tag validation, not from Validator.
	var ssErr *sserr.Error
	require.True(t, errors.As(err, &ssErr), "error type = %T, want *sserr.Error", err)
	assert.Equal(t, sserr.CodeValidationRequired, ssErr.Code, "required should fail before Validator")

	// The critical assertion: Validate() must not have been called.
	assert.Equal(t, 0, calls, "Validate() should not run when required fails")
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
	require.NoError(t, New().WithFile(path).Load(&cfg))

	// Host: env wins over file.
	assert.Equal(t, "from-env", cfg.Host, "env > file")
	// Port: file wins over default.
	assert.Equal(t, 3000, cfg.Port, "file > default")
	// Timeout: default only (not in file, not in env).
	assert.Equal(t, 30*time.Second, cfg.Timeout, "default only")
}

// TestLoader_Load_FileOverridesDefault verifies that file values
// override envDefault values.
func TestLoader_Load_FileOverridesDefault(t *testing.T) {
	t.Parallel()
	path := writeTestFile(t, "config.yaml", `
host: file-host
`)

	var cfg basicConfig
	require.NoError(t, New().WithFile(path).Load(&cfg))

	assert.Equal(t, "file-host", cfg.Host, "file > default")
}

// TestLoader_Load_DefaultOnly verifies that envDefault values are used
// when no file or env vars are provided.
func TestLoader_Load_DefaultOnly(t *testing.T) {
	t.Parallel()
	var cfg basicConfig
	require.NoError(t, New().Load(&cfg))

	assert.Equal(t, "localhost", cfg.Host, "default only")
	assert.Equal(t, 8080, cfg.Port, "default only")
}

// ===========================================================================
// MustLoad Tests
// ===========================================================================

// TestMustLoad_Success verifies that MustLoad returns a populated struct
// when loading succeeds.
func TestMustLoad_Success(t *testing.T) {
	t.Parallel()
	cfg := MustLoad[basicConfig](New())

	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
}

// TestMustLoad_Panics verifies that MustLoad panics when a required
// field is missing.
func TestMustLoad_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		require.NotNil(t, r, "MustLoad() expected panic, got none")
		msg, ok := r.(string)
		require.True(t, ok, "panic value type = %T, want string", r)
		assert.NotEmpty(t, msg, "panic message is empty, want descriptive message")
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
	require.Error(t, err, "Load() expected error for invalid int, got nil")
	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for parse error")
}

// TestLoader_Load_InvalidBool_FromEnv verifies that an invalid bool
// string returns an error.
func TestLoader_Load_InvalidBool_FromEnv(t *testing.T) {
	t.Setenv("DEBUG", "not-a-bool")

	var cfg basicConfig
	err := New().Load(&cfg)
	require.Error(t, err, "Load() expected error for invalid bool, got nil")
	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for parse error")
}

// TestLoader_Load_InvalidDuration_FromEnv verifies that an invalid
// duration string returns an error.
func TestLoader_Load_InvalidDuration_FromEnv(t *testing.T) {
	t.Setenv("TIMEOUT", "not-a-duration")

	var cfg basicConfig
	err := New().Load(&cfg)
	require.Error(t, err, "Load() expected error for invalid duration, got nil")
	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for parse error")
}

// TestLoader_Load_InvalidYAML_File verifies that a malformed YAML file
// returns an error.
func TestLoader_Load_InvalidYAML_File(t *testing.T) {
	t.Parallel()
	path := writeTestFile(t, "bad.yaml", `
host: [invalid yaml
  missing closing bracket
`)

	var cfg basicConfig
	err := New().WithFile(path).Load(&cfg)
	require.Error(t, err, "Load() expected error for malformed YAML, got nil")
	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for YAML parse error")
}

// TestLoader_Load_InvalidJSON_File verifies that a malformed JSON file
// returns an error.
func TestLoader_Load_InvalidJSON_File(t *testing.T) {
	t.Parallel()
	path := writeTestFile(t, "bad.json", `{"host": invalid}`)

	var cfg basicConfig
	err := New().WithFile(path).Load(&cfg)
	require.Error(t, err, "Load() expected error for malformed JSON, got nil")
	assert.True(t, sserr.IsInternal(err), "IsInternal() = false, want true for JSON parse error")
}

// ===========================================================================
// Load — Named Slice Type Tests
// ===========================================================================

// TestLoader_Load_NamedSlice_FromDefault verifies that named slice types
// (e.g., type Tags []string) are correctly populated from envDefault
// tags without panicking.
func TestLoader_Load_NamedSlice_FromDefault(t *testing.T) {
	t.Parallel()
	var cfg namedSliceConfig
	require.NoError(t, New().Load(&cfg))

	require.Len(t, cfg.Tags, 3)
	expected := []string{"a", "b", "c"}
	for i, want := range expected {
		assert.Equal(t, want, cfg.Tags[i], "Tags[%d]", i)
	}
}

// TestLoader_Load_NamedSlice_FromEnv verifies that named slice types
// are correctly populated from environment variables.
func TestLoader_Load_NamedSlice_FromEnv(t *testing.T) {
	t.Setenv("TAGS", "x, y, z")

	var cfg namedSliceConfig
	require.NoError(t, New().Load(&cfg))

	require.Len(t, cfg.Tags, 3)
	expected := []string{"x", "y", "z"}
	for i, want := range expected {
		assert.Equal(t, want, cfg.Tags[i], "Tags[%d]", i)
	}
}

// ===========================================================================
// Benchmark — NFR-1: Load time < 50ms
// ===========================================================================

// BenchmarkLoad measures the time to load a realistic configuration struct
// with defaults, a YAML file, and environment variable overrides. NFR-1
// requires Load time < 50ms; this benchmark verifies the constraint.
func BenchmarkLoad(b *testing.B) {
	// Create a temp YAML file once for the entire benchmark.
	dir := b.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := []byte("host: bench-host\nport: 3000\ndebug: true\ntimeout: 5s\n")
	if err := os.WriteFile(path, yamlContent, 0o600); err != nil {
		b.Fatalf("writeFile error: %v", err)
	}

	// Set env vars that override file values.
	b.Setenv("HOST", "env-host")
	b.Setenv("PORT", "9090")

	loader := New().WithFile(path)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var cfg basicConfig
		if err := loader.Load(&cfg); err != nil {
			b.Fatalf("Load() error: %v", err)
		}
	}
}

// TestBenchmarkLoad_Under50ms runs a single Load with file + env vars
// and asserts it completes under 50ms, satisfying NFR-1.
func TestBenchmarkLoad_Under50ms(t *testing.T) {
	path := writeTestFile(t, "config.yaml", `
host: bench-host
port: 3000
debug: true
timeout: 5s
`)

	t.Setenv("HOST", "env-host")
	t.Setenv("PORT", "9090")

	loader := New().WithFile(path)

	start := time.Now()
	var cfg basicConfig
	require.NoError(t, loader.Load(&cfg))
	elapsed := time.Since(start)

	// NFR-1 baseline is 50ms. The race detector adds significant overhead
	// (2-10x), so we use a relaxed threshold when -race is enabled.
	maxLoadTime := 50 * time.Millisecond
	if raceEnabled {
		maxLoadTime = 200 * time.Millisecond
	}
	assert.LessOrEqual(t, elapsed, maxLoadTime, "Load() took %v, want < %v (NFR-1)", elapsed, maxLoadTime)
}
