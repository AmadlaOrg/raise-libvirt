package main

import (
	"testing"

	"github.com/AmadlaOrg/raise-libvirt/libvirt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rockyGraph mirrors the multi-doc stream `hery compose --dir` emits for the
// rocky-os quickstart (unrelated entity kinds included to prove they are
// ignored).
const rockyGraph = `---
_type: amadla.org/entity/infrastructure@v1.0.0
_requires:
  - ./vm.infrastructure.hery
_body:
  provider: libvirt
  ssh:
    user: root
    port: 22
---
_type: amadla.org/entity/infrastructure/vm@v1.0.0
_requires:
  - ./system.hery
  - ./os.hery
_body:
  image: ~/Projects/OS/Rocky-10.1-x86_64-minimal.iso
  disk:
    size: 20
    format: qcow2
  forwarded_ports:
    - guest: 22
      host: 2222
      protocol: tcp
  gui: true
---
_type: amadla.org/entity/os@v1.0.0
_body:
  distro: rocky
  version: "10.1"
---
_type: amadla.org/entity/security/mac@v1.0.0
_body:
  mode: enforcing
---
_type: amadla.org/entity/os/preference@v1.0.0
_extends: amadla.org/entity/os@v1.0.0
_body:
  firewall: firewalld
---
_type: amadla.org/entity/system@v1.0.0
_body:
  hostname: rocky-demo
  timezone: UTC
---
_type: amadla.org/entity/system/cpu@v1.0.0
_body:
  cpus: 2
---
_type: amadla.org/entity/system/memory@v1.0.0
_body:
  size: 2048
`

func TestParseEntityToConfig_MultiDocGraph(t *testing.T) {
	config, err := parseEntityToConfig([]byte(rockyGraph))
	require.NoError(t, err)

	assert.Equal(t, "rocky-demo", config.Name)
	assert.Equal(t, "~/Projects/OS/Rocky-10.1-x86_64-minimal.iso", config.Image)
	assert.Equal(t, 2, config.CPUs)
	assert.Equal(t, 2048, config.MemoryMB)
	assert.Equal(t, 20, config.DiskSizeGB)
	assert.Equal(t, "qcow2", config.DiskFormat)
	assert.True(t, config.GUI)
	assert.Equal(t, "root", config.SSHUser)
	assert.Equal(t, 22, config.SSHPort)
	require.Len(t, config.ForwardedPorts, 1)
	assert.Equal(t, libvirt.PortForward{Guest: 22, Host: 2222, Protocol: "tcp"}, config.ForwardedPorts[0])
}

func TestParseEntityToConfig_LegacySingleDoc(t *testing.T) {
	input := `_type: amadla.org/entity/infrastructure@v1.0.0
_body:
  image: /iso/rocky.iso
  cpus: 4
  memory: 4096
  disk:
    size: 40
    format: qcow2
  gui: false
  ssh:
    user: admin
    port: 2222
`
	config, err := parseEntityToConfig([]byte(input))
	require.NoError(t, err)

	assert.Equal(t, "/iso/rocky.iso", config.Image)
	assert.Equal(t, 4, config.CPUs)
	assert.Equal(t, 4096, config.MemoryMB)
	assert.Equal(t, 40, config.DiskSizeGB)
	assert.False(t, config.GUI)
	assert.Equal(t, "admin", config.SSHUser)
	assert.Equal(t, 2222, config.SSHPort)
}

func TestParseEntityToConfig_JSONSingleDoc(t *testing.T) {
	input := `{"_type": "amadla.org/entity/infrastructure/vm@v1.0.0", "_body": {"image": "/iso/x.iso", "cpus": 1, "memory": 512}}`
	config, err := parseEntityToConfig([]byte(input))
	require.NoError(t, err)

	assert.Equal(t, "/iso/x.iso", config.Image)
	assert.Equal(t, 1, config.CPUs)
	assert.Equal(t, 512, config.MemoryMB)
}

func TestParseEntityToConfig_UntypedDocIsLegacyInfrastructure(t *testing.T) {
	input := `_body:
  image: /iso/y.iso
  cpus: 2
`
	config, err := parseEntityToConfig([]byte(input))
	require.NoError(t, err)
	assert.Equal(t, "/iso/y.iso", config.Image)
	assert.Equal(t, 2, config.CPUs)
}

func TestParseEntityToConfig_VMOverridesInfrastructure(t *testing.T) {
	input := `---
_type: amadla.org/entity/infrastructure@v1.0.0
_body:
  image: /iso/base.iso
  cpus: 1
  ssh:
    user: root
---
_type: amadla.org/entity/infrastructure/vm@v1.0.0
_body:
  image: /iso/override.iso
`
	config, err := parseEntityToConfig([]byte(input))
	require.NoError(t, err)
	// VM doc wins where set; infrastructure values survive where it is silent.
	assert.Equal(t, "/iso/override.iso", config.Image)
	assert.Equal(t, 1, config.CPUs)
	assert.Equal(t, "root", config.SSHUser)
}

func TestParseEntityToConfig_SystemCPUCountFallback(t *testing.T) {
	input := `---
_type: amadla.org/entity/infrastructure/vm@v1.0.0
_body:
  image: /iso/z.iso
---
_type: amadla.org/entity/system/cpu@v1.0.0
_body:
  count: 8
`
	config, err := parseEntityToConfig([]byte(input))
	require.NoError(t, err)
	assert.Equal(t, 8, config.CPUs)
}

func TestParseEntityToConfig_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty input", ""},
		{"missing _body", "_type: amadla.org/entity/infrastructure@v1.0.0\n"},
		{"only unknown kinds", "_type: amadla.org/entity/os@v1.0.0\n_body:\n  distro: rocky\n"},
		{"invalid yaml", ":\n  - ]["},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseEntityToConfig([]byte(tt.input))
			assert.Error(t, err)
		})
	}
}
