package libvirt

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ExecCommand is a variable for exec.Command to allow mocking in tests.
var ExecCommand = exec.Command


// HttpGet is a variable for http.Get to allow mocking in tests.
var HttpGet = http.Get

// OsCreate is a variable for os.Create to allow mocking in tests.
var OsCreate = os.Create

// IoCopy is a variable for io.Copy to allow mocking in tests.
var IoCopy = io.Copy

// VMConfig holds the configuration for a virtual machine.
type VMConfig struct {
	Name           string         `json:"name" yaml:"name"`
	Box            string         `json:"box" yaml:"box"`
	BoxURL         string         `json:"box_url" yaml:"box_url"`
	CPUs           int            `json:"cpus" yaml:"cpus"`
	MemoryMB       int            `json:"memory" yaml:"memory"`
	DiskSizeGB     int            `json:"disk_size" yaml:"disk_size"`
	DiskType       string         `json:"disk_type" yaml:"disk_type"`
	NetworkType    string         `json:"network_type" yaml:"network_type"`
	NetworkBridge  string         `json:"network_bridge" yaml:"network_bridge"`
	NetworkIP      string         `json:"network_ip" yaml:"network_ip"`
	ForwardedPorts []PortForward  `json:"forwarded_ports" yaml:"forwarded_ports"`
	SyncedFolders  []SyncedFolder `json:"synced_folders" yaml:"synced_folders"`
	GUI            bool           `json:"gui" yaml:"gui"`
	SSHUser        string         `json:"ssh_user" yaml:"ssh_user"`
	SSHPort        int            `json:"ssh_port" yaml:"ssh_port"`
	SSHPrivateKey  string         `json:"ssh_private_key" yaml:"ssh_private_key"`
}

// PortForward defines a port forwarding rule between host and guest.
type PortForward struct {
	Guest    int    `json:"guest" yaml:"guest"`
	Host     int    `json:"host" yaml:"host"`
	Protocol string `json:"protocol" yaml:"protocol"`
}

// SyncedFolder defines a folder shared between host and guest.
type SyncedFolder struct {
	Host  string `json:"host" yaml:"host"`
	Guest string `json:"guest" yaml:"guest"`
	Type  string `json:"type" yaml:"type"`
}

// VMState represents the current state of a managed VM.
type VMState struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Provider  string `json:"provider"`
	IP        string `json:"ip,omitempty"`
	CreatedAt string `json:"created_at"`
}

// Manager defines the interface for managing libvirt VMs.
type Manager interface {
	Up(config *VMConfig) (*VMState, error)
	Halt(name string) error
	Destroy(name string) error
	SSH(name string, user string, port int, keyFile string) error
	Status(name string) (*VMState, error)
	StatusAll() ([]VMState, error)
}

// manager implements Manager using virsh and virt-install commands.
type manager struct {
	stateManager StateManager
}

// New creates a new libvirt manager.
func New() Manager {
	return &manager{
		stateManager: NewStateManager(),
	}
}

// NewWithState creates a new libvirt manager with a custom state manager (for testing).
func NewWithState(sm StateManager) Manager {
	return &manager{
		stateManager: sm,
	}
}

// Up creates and starts a VM based on the provided configuration.
func (m *manager) Up(config *VMConfig) (*VMState, error) {
	if config.Name == "" {
		return nil, fmt.Errorf("VM name is required")
	}

	// Set defaults
	if config.CPUs <= 0 {
		config.CPUs = 1
	}
	if config.MemoryMB <= 0 {
		config.MemoryMB = 1024
	}
	if config.DiskSizeGB <= 0 {
		config.DiskSizeGB = 10
	}
	if config.DiskType == "" {
		config.DiskType = "qcow2"
	}
	if config.NetworkType == "" {
		config.NetworkType = "nat"
	}
	if config.SSHUser == "" {
		config.SSHUser = "root"
	}
	if config.SSHPort <= 0 {
		config.SSHPort = 22
	}

	// Resolve box image
	imagePath, err := m.resolveBoxImage(config)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve box image: %w", err)
	}

	// Build virt-install arguments
	args := m.buildVirtInstallArgs(config, imagePath)

	// Run virt-install
	cmd := ExecCommand("virt-install", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("virt-install failed: %s: %w", string(output), err)
	}

	// Create state entry
	state := &VMState{
		Name:      config.Name,
		State:     "running",
		Provider:  "libvirt",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Try to get IP
	ip, _ := m.getVMIP(config.Name)
	state.IP = ip

	// Save state
	if err := m.stateManager.Add(*state); err != nil {
		return nil, fmt.Errorf("VM created but failed to save state: %w", err)
	}

	return state, nil
}

// Halt shuts down a VM gracefully.
func (m *manager) Halt(name string) error {
	cmd := ExecCommand("virsh", "shutdown", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("virsh shutdown failed: %s: %w", string(output), err)
	}

	if err := m.stateManager.UpdateState(name, "shutoff"); err != nil {
		return fmt.Errorf("VM shut down but failed to update state: %w", err)
	}

	return nil
}

// Destroy removes a VM and its storage.
func (m *manager) Destroy(name string) error {
	// Try to destroy (force stop) the VM - ignore errors if it's already stopped
	cmd := ExecCommand("virsh", "destroy", name)
	_ = cmd.Run()

	// Undefine the VM and remove storage
	cmd = ExecCommand("virsh", "undefine", name, "--remove-all-storage")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("virsh undefine failed: %s: %w", string(output), err)
	}

	if err := m.stateManager.Remove(name); err != nil {
		return fmt.Errorf("VM destroyed but failed to update state: %w", err)
	}

	return nil
}

// SSH connects to a VM via SSH, replacing the current process.
func (m *manager) SSH(name string, user string, port int, keyFile string) error {
	ip, err := m.getVMIP(name)
	if err != nil {
		return fmt.Errorf("failed to get VM IP: %w", err)
	}

	if ip == "" {
		return fmt.Errorf("VM %q has no IP address assigned", name)
	}

	if user == "" {
		user = "root"
	}
	if port <= 0 {
		port = 22
	}

	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-p", fmt.Sprintf("%d", port),
	}

	if keyFile != "" {
		sshArgs = append(sshArgs, "-i", keyFile)
	}

	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", user, ip))

	cmd := ExecCommand("ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Status returns the current state of a specific VM.
func (m *manager) Status(name string) (*VMState, error) {
	cmd := ExecCommand("virsh", "domstate", name)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("virsh domstate failed for %q: %w", name, err)
	}

	state := strings.TrimSpace(string(output))

	ip, _ := m.getVMIP(name)

	vmState := &VMState{
		Name:     name,
		State:    state,
		Provider: "libvirt",
		IP:       ip,
	}

	// Enrich with stored state info
	if stored, err := m.stateManager.Get(name); err == nil {
		vmState.CreatedAt = stored.CreatedAt
	}

	// Update stored state
	_ = m.stateManager.UpdateState(name, state)

	return vmState, nil
}

// StatusAll returns the status of all managed VMs.
func (m *manager) StatusAll() ([]VMState, error) {
	states, err := m.stateManager.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	if len(states) == 0 {
		return []VMState{}, nil
	}

	// Refresh states from virsh
	virshStates, err := m.parseVirshList()
	if err != nil {
		// Return stored states if virsh fails
		return states, nil
	}

	for i := range states {
		if virshState, ok := virshStates[states[i].Name]; ok {
			states[i].State = virshState
		} else {
			states[i].State = "not found"
		}
		ip, _ := m.getVMIP(states[i].Name)
		states[i].IP = ip
	}

	return states, nil
}

// resolveBoxImage finds or downloads the box image and returns its path.
func (m *manager) resolveBoxImage(config *VMConfig) (string, error) {
	if config.Box == "" && config.BoxURL == "" {
		return "", fmt.Errorf("either box or box_url must be specified")
	}

	boxDir := boxesDir()
	if err := os.MkdirAll(boxDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create boxes directory: %w", err)
	}

	// If box is an absolute path to an existing file, use it directly
	if config.Box != "" && filepath.IsAbs(config.Box) {
		if _, err := os.Stat(config.Box); err == nil {
			return config.Box, nil
		}
	}

	// Normalize box name for filesystem (e.g., "debian/bookworm64" -> "debian-bookworm64")
	boxFileName := strings.ReplaceAll(config.Box, "/", "-") + ".qcow2"
	boxPath := filepath.Join(boxDir, boxFileName)

	// Check if already downloaded
	if _, err := os.Stat(boxPath); err == nil {
		return boxPath, nil
	}

	// Download if URL provided
	if config.BoxURL != "" {
		fmt.Fprintf(os.Stderr, "Downloading box image from %s...\n", config.BoxURL)
		if err := m.downloadImage(config.BoxURL, boxPath); err != nil {
			return "", fmt.Errorf("failed to download box image: %w", err)
		}
		return boxPath, nil
	}

	return "", fmt.Errorf("box image %q not found and no box_url provided", config.Box)
}

// downloadImage downloads a file from a URL.
func (m *manager) downloadImage(url, destPath string) error {
	resp, err := HttpGet(url)
	if err != nil {
		return fmt.Errorf("HTTP GET failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	out, err := OsCreate(destPath)
	if err != nil {
		return fmt.Errorf("cannot create file: %w", err)
	}
	defer out.Close()

	_, err = IoCopy(out, resp.Body)
	if err != nil {
		os.Remove(destPath)
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}

// buildVirtInstallArgs constructs the virt-install command arguments.
func (m *manager) buildVirtInstallArgs(config *VMConfig, imagePath string) []string {
	args := []string{
		"--name", config.Name,
		"--vcpus", fmt.Sprintf("%d", config.CPUs),
		"--memory", fmt.Sprintf("%d", config.MemoryMB),
		"--import",
		"--disk", fmt.Sprintf("path=%s,size=%d,format=%s", imagePath, config.DiskSizeGB, config.DiskType),
		"--os-variant", "generic",
		"--noautoconsole",
	}

	// Network configuration
	switch config.NetworkType {
	case "bridged":
		bridge := config.NetworkBridge
		if bridge == "" {
			bridge = "br0"
		}
		args = append(args, "--network", fmt.Sprintf("bridge=%s", bridge))
	case "private":
		args = append(args, "--network", "network=default,model=virtio")
	default: // nat
		args = append(args, "--network", "network=default,model=virtio")
	}

	// Headless by default
	if !config.GUI {
		args = append(args, "--graphics", "none")
	}

	return args
}

// getVMIP retrieves the IP address of a VM using virsh domifaddr.
func (m *manager) getVMIP(name string) (string, error) {
	cmd := ExecCommand("virsh", "domifaddr", name)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return parseIPFromDomifaddr(string(output)), nil
}

// parseIPFromDomifaddr extracts the IP address from virsh domifaddr output.
func parseIPFromDomifaddr(output string) string {
	// virsh domifaddr output format:
	//  Name       MAC address          Protocol     Address
	// -------------------------------------------------------------------------------
	//  vnet0      52:54:00:xx:xx:xx    ipv4         192.168.122.x/24
	re := regexp.MustCompile(`(\d+\.\d+\.\d+\.\d+)`)
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			return matches[1]
		}
	}
	return ""
}

// parseVirshList parses the output of virsh list --all into a map of name -> state.
func (m *manager) parseVirshList() (map[string]string, error) {
	cmd := ExecCommand("virsh", "list", "--all")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return ParseVirshListOutput(string(output)), nil
}

// ParseVirshListOutput parses virsh list --all output into a map of name -> state.
func ParseVirshListOutput(output string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip header lines, separator lines, and empty lines
		if line == "" || strings.HasPrefix(line, "Id") || strings.HasPrefix(line, "---") {
			continue
		}

		// Parse: " Id   Name    State"
		// Format: " 1    myvm    running" or " -    myvm    shut off"
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			name := fields[1]
			// State can be two words (e.g., "shut off")
			state := strings.Join(fields[2:], " ")
			result[name] = state
		}
	}

	return result
}

// boxesDir returns the path to the boxes directory.
func boxesDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "raise", "boxes")
}

// OutputJSON writes a value as indented JSON to stdout.
func OutputJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
