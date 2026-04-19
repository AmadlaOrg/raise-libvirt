package libvirt

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExecCommand creates a mock exec.Command that calls TestHelperProcess.
func fakeExecCommand(exitCode int, stdout string) func(string, ...string) *exec.Cmd {
	return func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", command}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			fmt.Sprintf("GO_HELPER_EXIT_CODE=%d", exitCode),
			fmt.Sprintf("GO_HELPER_STDOUT=%s", stdout),
		)
		return cmd
	}
}

// TestHelperProcess is a helper function used by fakeExecCommand.
// It's not a real test — it's invoked as a subprocess.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	exitCode := 0
	if code := os.Getenv("GO_HELPER_EXIT_CODE"); code != "" {
		fmt.Sscanf(code, "%d", &exitCode)
	}

	stdout := os.Getenv("GO_HELPER_STDOUT")
	if stdout != "" {
		fmt.Fprint(os.Stdout, stdout)
	}

	os.Exit(exitCode)
}

func TestUp_Success(t *testing.T) {
	// Save and restore original
	origExecCommand := ExecCommand
	defer func() { ExecCommand = origExecCommand }()

	ExecCommand = fakeExecCommand(0, "")

	statePath := tempStatePath(t)
	sm := NewStateManagerWithPath(statePath)
	mgr := NewWithState(sm)

	config := &VMConfig{
		Name:     "test-vm",
		Image:    createTempBoxImage(t),
		CPUs:     2,
		MemoryMB: 2048,
	}

	state, err := mgr.Up(config)
	require.NoError(t, err)
	assert.Equal(t, "test-vm", state.Name)
	assert.Equal(t, "running", state.State)
	assert.Equal(t, "libvirt", state.Provider)
	assert.NotEmpty(t, state.CreatedAt)
}

func TestUp_MissingName(t *testing.T) {
	statePath := tempStatePath(t)
	sm := NewStateManagerWithPath(statePath)
	mgr := NewWithState(sm)

	config := &VMConfig{
		Image: "/some/image.qcow2",
	}

	_, err := mgr.Up(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "VM name is required")
}

func TestUp_MissingImage(t *testing.T) {
	origExecCommand := ExecCommand
	defer func() { ExecCommand = origExecCommand }()

	ExecCommand = fakeExecCommand(0, "")

	statePath := tempStatePath(t)
	sm := NewStateManagerWithPath(statePath)
	mgr := NewWithState(sm)

	config := &VMConfig{
		Name: "test-vm",
		// No image or image_url
	}

	_, err := mgr.Up(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "either image or image_url must be specified")
}

func TestHalt_Success(t *testing.T) {
	origExecCommand := ExecCommand
	defer func() { ExecCommand = origExecCommand }()

	ExecCommand = fakeExecCommand(0, "Domain 'test-vm' is being shutdown")

	statePath := tempStatePath(t)
	sm := NewStateManagerWithPath(statePath)
	// Pre-populate state
	err := sm.Add(VMState{Name: "test-vm", State: "running", Provider: "libvirt", CreatedAt: "2026-03-10T00:00:00Z"})
	require.NoError(t, err)

	mgr := NewWithState(sm)

	err = mgr.Halt("test-vm")
	require.NoError(t, err)

	// Verify state updated
	got, err := sm.Get("test-vm")
	require.NoError(t, err)
	assert.Equal(t, "shutoff", got.State)
}

func TestDestroy_Success(t *testing.T) {
	origExecCommand := ExecCommand
	defer func() { ExecCommand = origExecCommand }()

	ExecCommand = fakeExecCommand(0, "")

	statePath := tempStatePath(t)
	sm := NewStateManagerWithPath(statePath)
	err := sm.Add(VMState{Name: "test-vm", State: "running", Provider: "libvirt", CreatedAt: "2026-03-10T00:00:00Z"})
	require.NoError(t, err)

	mgr := NewWithState(sm)

	err = mgr.Destroy("test-vm")
	require.NoError(t, err)

	// Verify removed from state
	_, err = sm.Get("test-vm")
	assert.Error(t, err)
}

func TestStatus_Running(t *testing.T) {
	origExecCommand := ExecCommand
	defer func() { ExecCommand = origExecCommand }()

	ExecCommand = fakeExecCommand(0, "running\n")

	statePath := tempStatePath(t)
	sm := NewStateManagerWithPath(statePath)
	err := sm.Add(VMState{Name: "test-vm", State: "running", Provider: "libvirt", CreatedAt: "2026-03-10T00:00:00Z"})
	require.NoError(t, err)

	mgr := NewWithState(sm)

	state, err := mgr.Status("test-vm")
	require.NoError(t, err)
	assert.Equal(t, "test-vm", state.Name)
	assert.Equal(t, "running", state.State)
	assert.Equal(t, "2026-03-10T00:00:00Z", state.CreatedAt)
}

func TestStatus_Shutoff(t *testing.T) {
	origExecCommand := ExecCommand
	defer func() { ExecCommand = origExecCommand }()

	ExecCommand = fakeExecCommand(0, "shut off\n")

	statePath := tempStatePath(t)
	sm := NewStateManagerWithPath(statePath)
	err := sm.Add(VMState{Name: "test-vm", State: "running", Provider: "libvirt", CreatedAt: "2026-03-10T00:00:00Z"})
	require.NoError(t, err)

	mgr := NewWithState(sm)

	state, err := mgr.Status("test-vm")
	require.NoError(t, err)
	assert.Equal(t, "shut off", state.State)
}

func TestStatusAll_Empty(t *testing.T) {
	statePath := tempStatePath(t)
	sm := NewStateManagerWithPath(statePath)
	mgr := NewWithState(sm)

	states, err := mgr.StatusAll()
	require.NoError(t, err)
	assert.Empty(t, states)
}

func TestStatusAll_MultipleVMs(t *testing.T) {
	origExecCommand := ExecCommand
	defer func() { ExecCommand = origExecCommand }()

	virshListOutput := ` Id   Name      State
-----------------------------
 1    vm1       running
 -    vm2       shut off
`
	ExecCommand = fakeExecCommand(0, virshListOutput)

	statePath := tempStatePath(t)
	sm := NewStateManagerWithPath(statePath)
	err := sm.Add(VMState{Name: "vm1", State: "running", Provider: "libvirt", CreatedAt: "2026-03-10T00:00:00Z"})
	require.NoError(t, err)
	err = sm.Add(VMState{Name: "vm2", State: "running", Provider: "libvirt", CreatedAt: "2026-03-10T01:00:00Z"})
	require.NoError(t, err)

	mgr := NewWithState(sm)

	states, err := mgr.StatusAll()
	require.NoError(t, err)
	assert.Len(t, states, 2)
}

func TestParseVirshListOutput(t *testing.T) {
	output := ` Id   Name      State
-----------------------------
 1    webserver running
 2    dbserver  running
 -    testvm    shut off
`
	result := ParseVirshListOutput(output)
	assert.Equal(t, "running", result["webserver"])
	assert.Equal(t, "running", result["dbserver"])
	assert.Equal(t, "shut off", result["testvm"])
}

func TestParseVirshListOutput_Empty(t *testing.T) {
	output := ` Id   Name      State
-----------------------------

`
	result := ParseVirshListOutput(output)
	assert.Empty(t, result)
}

func TestParseIPFromDomifaddr_WithIP(t *testing.T) {
	output := ` Name       MAC address          Protocol     Address
-------------------------------------------------------------------------------
 vnet0      52:54:00:ab:cd:ef    ipv4         192.168.122.45/24
`
	ip := parseIPFromDomifaddr(output)
	assert.Equal(t, "192.168.122.45", ip)
}

func TestParseIPFromDomifaddr_NoIP(t *testing.T) {
	output := ` Name       MAC address          Protocol     Address
-------------------------------------------------------------------------------
`
	ip := parseIPFromDomifaddr(output)
	assert.Empty(t, ip)
}

func TestBuildVirtInstallArgs_Default(t *testing.T) {
	mgr := &manager{}
	config := &VMConfig{
		Name:        "test-vm",
		CPUs:        2,
		MemoryMB:    2048,
		DiskSizeGB:  20,
		DiskFormat:    "qcow2",
		NetworkType: "nat",
	}

	args := mgr.buildVirtInstallArgs(config, "/path/to/image.qcow2")

	assert.Contains(t, args, "--name")
	assert.Contains(t, args, "test-vm")
	assert.Contains(t, args, "--vcpus")
	assert.Contains(t, args, "2")
	assert.Contains(t, args, "--memory")
	assert.Contains(t, args, "2048")
	assert.Contains(t, args, "--import")
	assert.Contains(t, args, "--noautoconsole")
	assert.Contains(t, args, "--graphics")
	assert.Contains(t, args, "none")
}

func TestBuildVirtInstallArgs_Bridged(t *testing.T) {
	mgr := &manager{}
	config := &VMConfig{
		Name:          "test-vm",
		CPUs:          1,
		MemoryMB:      1024,
		DiskSizeGB:    10,
		DiskFormat:      "qcow2",
		NetworkType:   "bridged",
		NetworkBridge: "virbr1",
	}

	args := mgr.buildVirtInstallArgs(config, "/path/to/image.qcow2")

	found := false
	for _, arg := range args {
		if arg == "bridge=virbr1" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected bridge=virbr1 in args")
}

func TestBuildVirtInstallArgs_GUI(t *testing.T) {
	mgr := &manager{}
	config := &VMConfig{
		Name:        "test-vm",
		CPUs:        1,
		MemoryMB:    1024,
		DiskSizeGB:  10,
		DiskFormat:  "qcow2",
		NetworkType: "nat",
		GUI:         true,
	}

	args := mgr.buildVirtInstallArgs(config, "/path/to/image.qcow2")

	// Should contain --graphics vnc and --video vga when GUI is true
	assert.Contains(t, args, "--graphics")
	assert.Contains(t, args, "vnc,listen=127.0.0.1")
	assert.Contains(t, args, "--video")
	assert.Contains(t, args, "vga")
}

func TestBuildVirtInstallArgs_ISO(t *testing.T) {
	mgr := &manager{}
	config := &VMConfig{
		Name:        "test-vm",
		CPUs:        2,
		MemoryMB:    2048,
		DiskSizeGB:  20,
		DiskFormat:  "qcow2",
		NetworkType: "nat",
	}

	args := mgr.buildVirtInstallArgs(config, "/path/to/rocky.iso")

	assert.Contains(t, args, "--cdrom")
	assert.Contains(t, args, "/path/to/rocky.iso")
	assert.Contains(t, args, "--boot")
	assert.Contains(t, args, "cdrom,hd")
	// Should NOT contain --import for ISO boot
	assert.NotContains(t, args, "--import")
}

// createTempBoxImage creates a temporary file to act as a box image for testing.
func createTempBoxImage(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "test-box-*.qcow2")
	require.NoError(t, err)
	f.Close()
	return f.Name()
}
