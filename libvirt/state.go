package libvirt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// StateManager defines the interface for managing VM state persistence.
type StateManager interface {
	LoadAll() ([]VMState, error)
	Get(name string) (*VMState, error)
	Add(state VMState) error
	UpdateState(name string, state string) error
	Remove(name string) error
}

// stateManager implements StateManager using a JSON file.
type stateManager struct {
	statePath string
	mu        sync.Mutex
}

// NewStateManager creates a new state manager with the default state file path.
func NewStateManager() StateManager {
	return &stateManager{
		statePath: defaultStatePath(),
	}
}

// NewStateManagerWithPath creates a new state manager with a custom state file path.
func NewStateManagerWithPath(path string) StateManager {
	return &stateManager{
		statePath: path,
	}
}

// defaultStatePath returns the default path to the state file.
func defaultStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "raise", "libvirt", "state.json")
}

// LoadAll reads all VM states from the state file.
func (s *stateManager) LoadAll() ([]VMState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.load()
}

// Get retrieves a single VM state by name.
func (s *stateManager) Get(name string) (*VMState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	states, err := s.load()
	if err != nil {
		return nil, err
	}

	for i := range states {
		if states[i].Name == name {
			return &states[i], nil
		}
	}

	return nil, fmt.Errorf("VM %q not found in state", name)
}

// Add appends a new VM state entry.
func (s *stateManager) Add(state VMState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	states, err := s.load()
	if err != nil {
		return err
	}

	// Check for duplicate
	for _, existing := range states {
		if existing.Name == state.Name {
			return fmt.Errorf("VM %q already exists in state", state.Name)
		}
	}

	states = append(states, state)
	return s.save(states)
}

// UpdateState updates the state field of a VM.
func (s *stateManager) UpdateState(name string, newState string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	states, err := s.load()
	if err != nil {
		return err
	}

	found := false
	for i := range states {
		if states[i].Name == name {
			states[i].State = newState
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("VM %q not found in state", name)
	}

	return s.save(states)
}

// Remove deletes a VM state entry by name.
func (s *stateManager) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	states, err := s.load()
	if err != nil {
		return err
	}

	filtered := make([]VMState, 0, len(states))
	found := false
	for _, state := range states {
		if state.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, state)
	}

	if !found {
		return fmt.Errorf("VM %q not found in state", name)
	}

	return s.save(filtered)
}

// load reads the state file. Returns an empty slice if the file doesn't exist.
func (s *stateManager) load() ([]VMState, error) {
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []VMState{}, nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	if len(data) == 0 {
		return []VMState{}, nil
	}

	var states []VMState
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return states, nil
}

// save writes the state to the state file.
func (s *stateManager) save(states []VMState) error {
	dir := filepath.Dir(s.statePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(s.statePath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}
