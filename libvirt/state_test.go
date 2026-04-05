package libvirt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tempStatePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "state.json")
}

func TestStateManager_LoadAll_EmptyFile(t *testing.T) {
	sm := NewStateManagerWithPath(tempStatePath(t))

	states, err := sm.LoadAll()
	require.NoError(t, err)
	assert.Empty(t, states)
}

func TestStateManager_Add_Success(t *testing.T) {
	sm := NewStateManagerWithPath(tempStatePath(t))

	state := VMState{
		Name:      "test-vm",
		State:     "running",
		Provider:  "libvirt",
		CreatedAt: "2026-03-10T00:00:00Z",
	}

	err := sm.Add(state)
	require.NoError(t, err)

	states, err := sm.LoadAll()
	require.NoError(t, err)
	assert.Len(t, states, 1)
	assert.Equal(t, "test-vm", states[0].Name)
	assert.Equal(t, "running", states[0].State)
}

func TestStateManager_Add_Duplicate(t *testing.T) {
	sm := NewStateManagerWithPath(tempStatePath(t))

	state := VMState{
		Name:      "test-vm",
		State:     "running",
		Provider:  "libvirt",
		CreatedAt: "2026-03-10T00:00:00Z",
	}

	err := sm.Add(state)
	require.NoError(t, err)

	err = sm.Add(state)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestStateManager_Get_Success(t *testing.T) {
	sm := NewStateManagerWithPath(tempStatePath(t))

	state := VMState{
		Name:      "test-vm",
		State:     "running",
		Provider:  "libvirt",
		IP:        "192.168.122.10",
		CreatedAt: "2026-03-10T00:00:00Z",
	}

	err := sm.Add(state)
	require.NoError(t, err)

	got, err := sm.Get("test-vm")
	require.NoError(t, err)
	assert.Equal(t, "test-vm", got.Name)
	assert.Equal(t, "192.168.122.10", got.IP)
}

func TestStateManager_Get_NotFound(t *testing.T) {
	sm := NewStateManagerWithPath(tempStatePath(t))

	_, err := sm.Get("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStateManager_UpdateState_Success(t *testing.T) {
	sm := NewStateManagerWithPath(tempStatePath(t))

	state := VMState{
		Name:      "test-vm",
		State:     "running",
		Provider:  "libvirt",
		CreatedAt: "2026-03-10T00:00:00Z",
	}

	err := sm.Add(state)
	require.NoError(t, err)

	err = sm.UpdateState("test-vm", "shutoff")
	require.NoError(t, err)

	got, err := sm.Get("test-vm")
	require.NoError(t, err)
	assert.Equal(t, "shutoff", got.State)
}

func TestStateManager_UpdateState_NotFound(t *testing.T) {
	sm := NewStateManagerWithPath(tempStatePath(t))

	err := sm.UpdateState("nonexistent", "running")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStateManager_Remove_Success(t *testing.T) {
	sm := NewStateManagerWithPath(tempStatePath(t))

	state := VMState{
		Name:      "test-vm",
		State:     "running",
		Provider:  "libvirt",
		CreatedAt: "2026-03-10T00:00:00Z",
	}

	err := sm.Add(state)
	require.NoError(t, err)

	err = sm.Remove("test-vm")
	require.NoError(t, err)

	states, err := sm.LoadAll()
	require.NoError(t, err)
	assert.Empty(t, states)
}

func TestStateManager_Remove_NotFound(t *testing.T) {
	sm := NewStateManagerWithPath(tempStatePath(t))

	err := sm.Remove("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStateManager_MultipleVMs(t *testing.T) {
	sm := NewStateManagerWithPath(tempStatePath(t))

	vms := []VMState{
		{Name: "vm1", State: "running", Provider: "libvirt", CreatedAt: "2026-03-10T00:00:00Z"},
		{Name: "vm2", State: "shutoff", Provider: "libvirt", CreatedAt: "2026-03-10T01:00:00Z"},
		{Name: "vm3", State: "running", Provider: "libvirt", CreatedAt: "2026-03-10T02:00:00Z"},
	}

	for _, vm := range vms {
		err := sm.Add(vm)
		require.NoError(t, err)
	}

	states, err := sm.LoadAll()
	require.NoError(t, err)
	assert.Len(t, states, 3)

	// Remove middle one
	err = sm.Remove("vm2")
	require.NoError(t, err)

	states, err = sm.LoadAll()
	require.NoError(t, err)
	assert.Len(t, states, 2)
	assert.Equal(t, "vm1", states[0].Name)
	assert.Equal(t, "vm3", states[1].Name)
}

func TestStateManager_LoadCorruptFile(t *testing.T) {
	path := tempStatePath(t)
	dir := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))

	sm := NewStateManagerWithPath(path)
	_, err := sm.LoadAll()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse state file")
}

func TestStateManager_LoadEmptyJSON(t *testing.T) {
	path := tempStatePath(t)
	dir := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(path, []byte(""), 0o644))

	sm := NewStateManagerWithPath(path)
	states, err := sm.LoadAll()
	require.NoError(t, err)
	assert.Empty(t, states)
}
