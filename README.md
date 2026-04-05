# raise-libvirt

A [raise](https://github.com/AmadlaOrg/raise) plugin that manages KVM/QEMU virtual machines via the libvirt API using `virsh` and `virt-install` command-line tools.

## Prerequisites

- libvirt daemon (`libvirtd`)
- QEMU/KVM (`qemu-kvm`)
- `virsh` CLI tool
- `virt-install` CLI tool
- `ssh` client

### Install on Debian/Ubuntu

```bash
sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients virtinst
sudo usermod -aG libvirt $USER
```

### Install on Fedora/RHEL

```bash
sudo dnf install qemu-kvm libvirt virt-install
sudo usermod -aG libvirt $USER
```

## Usage

```bash
# Show plugin info
raise-libvirt info

# Create and start a VM
raise-libvirt up myvm -f infrastructure.yaml

# Check VM status
raise-libvirt status myvm

# List all managed VMs
raise-libvirt status

# SSH into a VM
raise-libvirt ssh myvm

# Shut down a VM
raise-libvirt halt myvm

# Destroy a VM and remove storage
raise-libvirt destroy myvm
```

## Infrastructure Entity Example

```yaml
_type: amadla.org/entity/infrastructure/vm@v1.0.0
_body:
  provider: libvirt
  box: debian/bookworm64
  box_url: https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-generic-amd64.qcow2
  cpus: 2
  memory: 2048
  disk:
    size: 20
    type: qcow2
  network:
    type: nat
  forwarded_ports:
    - guest: 80
      host: 8080
      protocol: tcp
  synced_folders:
    - host: ./data
      guest: /srv/data
      type: 9p
  ssh:
    user: debian
    port: 22
```

## Build

```bash
make build
make test
```
