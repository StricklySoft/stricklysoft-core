// Package fixtures provides shared test data constants and factory
// functions for the StricklySoft Core SDK test suite.
//
// Using common constants for test agent identities prevents magic strings
// in tests and ensures consistency across packages.
package fixtures

// Standard agent identity values used across lifecycle and integration tests.
const (
	// AgentID is the default agent ID for unit tests.
	AgentID = "agent-001"

	// AgentName is the default agent name for unit tests.
	AgentName = "test-agent"

	// AgentVersion is the default agent version for unit tests.
	AgentVersion = "1.0.0"

	// AltAgentID is an alternative agent ID for tests requiring two agents.
	AltAgentID = "agent-002"

	// AltAgentName is an alternative agent name for tests requiring two agents.
	AltAgentName = "alt-agent"

	// AltAgentVersion is an alternative agent version string.
	AltAgentVersion = "2.0.0"
)

// Standard identity values used in auth tests.
const (
	// TestSubject is the default subject claim for test identities.
	TestSubject = "user-abc-123"

	// TestIssuer is the default issuer for test identities.
	TestIssuer = "https://auth.stricklysoft.test"

	// TestAudience is the default audience for test identities.
	TestAudience = "stricklysoft-core"

	// TestServiceName is the default service name for service identities.
	TestServiceName = "test-service"

	// TestServiceVersion is the default service version for service identities.
	TestServiceVersion = "1.0.0"
)

// Standard configuration values used in config loader tests.
const (
	// TestEnvPrefix is the default environment variable prefix for config tests.
	TestEnvPrefix = "TESTAPP"

	// TestConfigYAML is a minimal valid YAML configuration for tests.
	TestConfigYAML = `host: localhost
port: 8080
database: testdb
`

	// TestConfigJSON is a minimal valid JSON configuration for tests.
	TestConfigJSON = `{
  "host": "localhost",
  "port": 8080,
  "database": "testdb"
}`
)

// Standard database configuration values used in postgres client tests.
const (
	// TestDBHost is the default database host for test configurations.
	TestDBHost = "localhost"

	// TestDBPort is the default database port for test configurations.
	TestDBPort = 5432

	// TestDBName is the default database name for test configurations.
	TestDBName = "testdb"

	// TestDBUser is the default database user for test configurations.
	TestDBUser = "testuser"

	// TestDBPassword is the default database password for test configurations.
	// This is a deliberately weak value suitable only for unit tests.
	TestDBPassword = "testpass"
)
