package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/AmadlaOrg/raise-libvirt/libvirt"
	"gopkg.in/yaml.v3"
)

// graphDoc is one document of a HERY entity stream. The body is kept as a
// raw node until the entity kind is known, so each kind can decode it into
// its own shape.
type graphDoc struct {
	Type string    `json:"_type" yaml:"_type"`
	Body yaml.Node `json:"_body" yaml:"_body"`
}

// entityBody is the infrastructure / infrastructure/vm body shape. It also
// covers the legacy single-document form where every VM field sat in one
// infrastructure _body.
type entityBody struct {
	Provider       string                 `json:"provider" yaml:"provider"`
	Region         string                 `json:"region" yaml:"region"`
	Image          string                 `json:"image" yaml:"image"`
	ImageURL       string                 `json:"image_url" yaml:"image_url"`
	CPUs           int                    `json:"cpus" yaml:"cpus"`
	Memory         int                    `json:"memory" yaml:"memory"`
	Disk           *entityDisk            `json:"disk" yaml:"disk"`
	Network        *entityNetwork         `json:"network" yaml:"network"`
	ForwardedPorts []libvirt.PortForward  `json:"forwarded_ports" yaml:"forwarded_ports"`
	SyncedFolders  []libvirt.SyncedFolder `json:"synced_folders" yaml:"synced_folders"`
	GUI            *bool                  `json:"gui" yaml:"gui"`
	SSH            *entitySSH             `json:"ssh" yaml:"ssh"`
}

type entityDisk struct {
	Size   int    `json:"size" yaml:"size"`
	Format string `json:"format" yaml:"format"`
}

type entityNetwork struct {
	Type   string `json:"type" yaml:"type"`
	Bridge string `json:"bridge" yaml:"bridge"`
	IP     string `json:"ip" yaml:"ip"`
}

type entitySSH struct {
	User       string `json:"user" yaml:"user"`
	Port       int    `json:"port" yaml:"port"`
	PrivateKey string `json:"private_key" yaml:"private_key"`
}

type systemBody struct {
	Hostname string `json:"hostname" yaml:"hostname"`
}

type cpuBody struct {
	CPUs  int `json:"cpus" yaml:"cpus"`
	Count int `json:"count" yaml:"count"`
}

type memoryBody struct {
	Size   int `json:"size" yaml:"size"`
	Memory int `json:"memory" yaml:"memory"`
}

// parseEntityToConfig builds a VMConfig from HERY entity input: either a
// single infrastructure entity (legacy form) or the multi-doc graph emitted
// by `hery compose --dir` (infrastructure, infrastructure/vm, system,
// system/cpu, system/memory — other entity kinds are ignored).
func parseEntityToConfig(data []byte) (*libvirt.VMConfig, error) {
	docs, err := decodeDocs(data)
	if err != nil {
		return nil, err
	}

	byKind := make(map[string]*yaml.Node, len(docs))
	for i := range docs {
		if docs[i].Body.Kind == 0 {
			continue
		}
		byKind[entityKind(docs[i].Type)] = &docs[i].Body
	}
	if len(byKind) == 0 {
		return nil, fmt.Errorf("input missing _body field")
	}

	config := &libvirt.VMConfig{}
	projected := false

	// Precedence: generic infrastructure first, VM specifics on top, then
	// System/* facts. A doc without _type is treated as legacy infrastructure.
	for _, k := range []string{"", "infrastructure", "infrastructure/vm"} {
		node, ok := byKind[k]
		if !ok {
			continue
		}
		var body entityBody
		if err := node.Decode(&body); err != nil {
			return nil, fmt.Errorf("%s: invalid _body: %w", kindLabel(k), err)
		}
		overlayBody(config, &body)
		projected = true
	}

	if node, ok := byKind["system"]; ok {
		var body systemBody
		if err := node.Decode(&body); err != nil {
			return nil, fmt.Errorf("system: invalid _body: %w", err)
		}
		if body.Hostname != "" {
			config.Name = body.Hostname
		}
		projected = true
	}
	if node, ok := byKind["system/cpu"]; ok {
		var body cpuBody
		if err := node.Decode(&body); err != nil {
			return nil, fmt.Errorf("system/cpu: invalid _body: %w", err)
		}
		if body.CPUs > 0 {
			config.CPUs = body.CPUs
		} else if body.Count > 0 {
			config.CPUs = body.Count
		}
		projected = true
	}
	if node, ok := byKind["system/memory"]; ok {
		var body memoryBody
		if err := node.Decode(&body); err != nil {
			return nil, fmt.Errorf("system/memory: invalid _body: %w", err)
		}
		if body.Size > 0 {
			config.MemoryMB = body.Size
		} else if body.Memory > 0 {
			config.MemoryMB = body.Memory
		}
		projected = true
	}

	if !projected {
		return nil, fmt.Errorf("no usable entity in input (expected infrastructure, infrastructure/vm, or system/* documents)")
	}
	return config, nil
}

// decodeDocs reads a YAML multi-doc stream (JSON is a YAML subset, so a
// single JSON entity decodes too).
func decodeDocs(data []byte) ([]graphDoc, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []graphDoc
	for {
		var d graphDoc
		if err := dec.Decode(&d); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("input is not valid JSON or YAML entity data: %w", err)
		}
		docs = append(docs, d)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no entity documents in input")
	}
	return docs, nil
}

// entityKind normalizes a _type URI to its kind path: version stripped,
// lowercased, and reduced to the segment after "/entity/" when present
// (e.g. "amadla.org/entity/system/cpu@v1.0.0" -> "system/cpu").
func entityKind(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if i := strings.IndexByte(t, '@'); i >= 0 {
		t = t[:i]
	}
	if i := strings.Index(t, "/entity/"); i >= 0 {
		t = t[i+len("/entity/"):]
	}
	return t
}

func kindLabel(k string) string {
	if k == "" {
		return "entity"
	}
	return k
}

// overlayBody copies non-zero fields of body onto config, so a later doc
// (infrastructure/vm) refines an earlier one (infrastructure) without wiping
// fields it does not set.
func overlayBody(config *libvirt.VMConfig, body *entityBody) {
	if body.Image != "" {
		config.Image = body.Image
	}
	if body.ImageURL != "" {
		config.ImageURL = body.ImageURL
	}
	if body.CPUs > 0 {
		config.CPUs = body.CPUs
	}
	if body.Memory > 0 {
		config.MemoryMB = body.Memory
	}
	if body.GUI != nil {
		config.GUI = *body.GUI
	}
	if len(body.ForwardedPorts) > 0 {
		config.ForwardedPorts = body.ForwardedPorts
	}
	if len(body.SyncedFolders) > 0 {
		config.SyncedFolders = body.SyncedFolders
	}
	if body.Disk != nil {
		if body.Disk.Size > 0 {
			config.DiskSizeGB = body.Disk.Size
		}
		if body.Disk.Format != "" {
			config.DiskFormat = body.Disk.Format
		}
	}
	if body.Network != nil {
		if body.Network.Type != "" {
			config.NetworkType = body.Network.Type
		}
		if body.Network.Bridge != "" {
			config.NetworkBridge = body.Network.Bridge
		}
		if body.Network.IP != "" {
			config.NetworkIP = body.Network.IP
		}
	}
	if body.SSH != nil {
		if body.SSH.User != "" {
			config.SSHUser = body.SSH.User
		}
		if body.SSH.Port > 0 {
			config.SSHPort = body.SSH.Port
		}
		if body.SSH.PrivateKey != "" {
			config.SSHPrivateKey = body.SSH.PrivateKey
		}
	}
}
