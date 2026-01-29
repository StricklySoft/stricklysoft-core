package lifecycle

import (
	"testing"
)

// ===========================================================================
// State.String Tests
// ===========================================================================

// TestState_String verifies that every State constant returns the expected
// string representation via the String method.
func TestState_String(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateUnknown, "unknown"},
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StatePaused, "paused"},
		{StateStopping, "stopping"},
		{StateStopped, "stopped"},
		{StateFailed, "failed"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("State.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ===========================================================================
// State.Valid Tests
// ===========================================================================

// TestState_Valid verifies that all defined State constants are recognized
// as valid, and that invalid values (empty string, arbitrary strings) are
// rejected.
func TestState_Valid(t *testing.T) {
	validStates := []State{
		StateUnknown, StateStarting, StateRunning, StatePaused,
		StateStopping, StateStopped, StateFailed,
	}
	for _, s := range validStates {
		t.Run("valid_"+string(s), func(t *testing.T) {
			if !s.Valid() {
				t.Errorf("State(%q).Valid() = false, want true", s)
			}
		})
	}

	invalidStates := []State{"", "bogus", "RUNNING", "ready", "initializing"}
	for _, s := range invalidStates {
		name := string(s)
		if name == "" {
			name = "empty"
		}
		t.Run("invalid_"+name, func(t *testing.T) {
			if s.Valid() {
				t.Errorf("State(%q).Valid() = true, want false", s)
			}
		})
	}
}

// ===========================================================================
// State.IsTerminal Tests
// ===========================================================================

// TestState_IsTerminal verifies that only Stopped and Failed are recognized
// as terminal states, and all other states are non-terminal.
func TestState_IsTerminal(t *testing.T) {
	tests := []struct {
		state    State
		terminal bool
	}{
		{StateUnknown, false},
		{StateStarting, false},
		{StateRunning, false},
		{StatePaused, false},
		{StateStopping, false},
		{StateStopped, true},
		{StateFailed, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsTerminal(); got != tt.terminal {
				t.Errorf("State(%q).IsTerminal() = %v, want %v",
					tt.state, got, tt.terminal)
			}
		})
	}
}

// ===========================================================================
// ValidTransition Tests
// ===========================================================================

// TestValidTransition_AllValid verifies that every transition listed in the
// validTransitions matrix is accepted by ValidTransition.
func TestValidTransition_AllValid(t *testing.T) {
	tests := []struct {
		from State
		to   State
	}{
		// Unknown transitions
		{StateUnknown, StateStarting},
		{StateUnknown, StateFailed},
		// Starting transitions
		{StateStarting, StateRunning},
		{StateStarting, StateFailed},
		{StateStarting, StateStopping},
		// Running transitions
		{StateRunning, StatePaused},
		{StateRunning, StateStopping},
		{StateRunning, StateFailed},
		// Paused transitions
		{StatePaused, StateRunning},
		{StatePaused, StateStopping},
		{StatePaused, StateFailed},
		// Stopping transitions
		{StateStopping, StateStopped},
		{StateStopping, StateFailed},
		// Terminal restart transitions
		{StateStopped, StateStarting},
		{StateFailed, StateStarting},
	}
	for _, tt := range tests {
		name := string(tt.from) + "_to_" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			if !ValidTransition(tt.from, tt.to) {
				t.Errorf("ValidTransition(%q, %q) = false, want true",
					tt.from, tt.to)
			}
		})
	}
}

// TestValidTransition_Invalid verifies that transitions not in the matrix
// are rejected by ValidTransition.
func TestValidTransition_Invalid(t *testing.T) {
	tests := []struct {
		from State
		to   State
	}{
		// Cannot skip directly to Running from Unknown
		{StateUnknown, StateRunning},
		// Cannot go backwards from Running to Starting
		{StateRunning, StateStarting},
		// Cannot go from Stopped to Running (must go through Starting)
		{StateStopped, StateRunning},
		// Cannot pause from Stopped
		{StateStopped, StatePaused},
		// Cannot go from Stopping to Running (must complete shutdown)
		{StateStopping, StateRunning},
		// Cannot go from Failed directly to Running (must go through Starting)
		{StateFailed, StateRunning},
		// Cannot go from Starting to Paused (must be Running first)
		{StateStarting, StatePaused},
		// Cannot go from Unknown to Stopped
		{StateUnknown, StateStopped},
	}
	for _, tt := range tests {
		name := string(tt.from) + "_to_" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			if ValidTransition(tt.from, tt.to) {
				t.Errorf("ValidTransition(%q, %q) = true, want false",
					tt.from, tt.to)
			}
		})
	}
}

// TestValidTransition_SameState verifies that transitioning from a state
// to the same state is always rejected.
func TestValidTransition_SameState(t *testing.T) {
	states := []State{
		StateUnknown, StateStarting, StateRunning, StatePaused,
		StateStopping, StateStopped, StateFailed,
	}
	for _, s := range states {
		t.Run(string(s), func(t *testing.T) {
			if ValidTransition(s, s) {
				t.Errorf("ValidTransition(%q, %q) = true, want false (same-state)",
					s, s)
			}
		})
	}
}

// TestValidTransition_InvalidSourceState verifies that transitions from an
// unrecognized state are rejected.
func TestValidTransition_InvalidSourceState(t *testing.T) {
	if ValidTransition(State("nonexistent"), StateStarting) {
		t.Error("ValidTransition from unrecognized state = true, want false")
	}
}
