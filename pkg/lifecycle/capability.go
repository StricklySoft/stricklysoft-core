package lifecycle

import (
	"errors"
	"fmt"
)

// Capability describes a named capability that an agent supports. Capabilities
// are reported via [AgentInfo] and used by orchestration systems for feature
// discovery and routing decisions.
//
// A capability represents a discrete unit of functionality (e.g.,
// "model-execution", "policy-evaluation", "code-generation") with a semantic
// version indicating the implementation level. The [Metadata] field provides
// extensibility for capability-specific attributes.
//
// Capabilities are value types. Use [NewCapability] to construct validated
// instances and [Capability.Clone] to obtain deep copies when sharing between
// goroutines or returning from methods that expose internal state.
//
// Example:
//
//	cap, err := lifecycle.NewCapability(
//	    "model-execution",
//	    "1.2.0",
//	    "Execute LLM inference requests",
//	    map[string]string{"max_tokens": "8192", "provider": "anthropic"},
//	)
//	if err != nil {
//	    return err
//	}
type Capability struct {
	// Name is the identifier for the capability (e.g., "model-execution",
	// "policy-evaluation"). Must not be empty.
	Name string `json:"name"`

	// Version is the semantic version of the capability implementation
	// (e.g., "1.0.0"). Must not be empty.
	Version string `json:"version"`

	// Description is a human-readable summary of what this capability
	// provides. May be empty for internal-only capabilities.
	Description string `json:"description"`

	// Metadata contains additional key-value pairs with capability-specific
	// attributes (e.g., model provider, token limits, supported formats).
	// Omitted from JSON when empty.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// NewCapability creates a new [Capability] with validated fields. The metadata
// map is defensively copied to prevent external mutation. Returns an error if
// name or version is empty.
//
// Example:
//
//	cap, err := lifecycle.NewCapability("code-generation", "2.0.0", "Generate code from prompts", nil)
func NewCapability(name, version, description string, metadata map[string]string) (Capability, error) {
	if name == "" {
		return Capability{}, errors.New("lifecycle: capability name must not be empty")
	}
	if version == "" {
		return Capability{}, fmt.Errorf("lifecycle: capability %q version must not be empty", name)
	}

	// Defensive copy of metadata to prevent external mutation.
	var copied map[string]string
	if len(metadata) > 0 {
		copied = make(map[string]string, len(metadata))
		for k, v := range metadata {
			copied[k] = v
		}
	}

	return Capability{
		Name:        name,
		Version:     version,
		Description: description,
		Metadata:    copied,
	}, nil
}

// Clone returns a deep copy of the Capability, including a copy of the
// [Metadata] map. This is used internally by [BaseAgent] to return
// defensive copies from [BaseAgent.Capabilities] and [BaseAgent.Info].
func (c Capability) Clone() Capability {
	var copied map[string]string
	if len(c.Metadata) > 0 {
		copied = make(map[string]string, len(c.Metadata))
		for k, v := range c.Metadata {
			copied[k] = v
		}
	}
	return Capability{
		Name:        c.Name,
		Version:     c.Version,
		Description: c.Description,
		Metadata:    copied,
	}
}
