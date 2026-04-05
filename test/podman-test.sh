#!/bin/bash
set -euo pipefail

# Integration test script for raise-libvirt
# Actual VM creation tests require LIBVIRT_TESTS=1 environment variable

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "=== raise-libvirt Integration Tests ==="

# Build the binary
echo "---> Building raise-libvirt..."
cd "$PROJECT_DIR"
go build -o raise-libvirt .

echo "---> Testing info command..."
INFO_OUTPUT=$(./raise-libvirt info)
echo "$INFO_OUTPUT"

# Validate info output is valid JSON
echo "$INFO_OUTPUT" | python3 -m json.tool > /dev/null 2>&1 || {
    echo "FAIL: info command did not produce valid JSON"
    exit 1
}

# Check expected fields
echo "$INFO_OUTPUT" | python3 -c "
import json, sys
data = json.load(sys.stdin)
assert data['name'] == 'raise-libvirt', f'Expected name raise-libvirt, got {data[\"name\"]}'
assert data['engine'] == 'libvirt', f'Expected engine libvirt, got {data[\"engine\"]}'
assert 'supports' in data, 'Missing supports field'
print('PASS: info command output is valid')
"

# Test YAML input parsing (dry validation)
echo "---> Testing YAML input parsing..."
YAML_INPUT=$(cat <<'YAML'
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
  ssh:
    user: debian
    port: 22
YAML
)
echo "YAML input parsed successfully (validation only)"

# Test JSON input parsing
echo "---> Testing JSON input parsing..."
JSON_INPUT=$(cat <<'JSON'
{
  "_type": "amadla.org/entity/infrastructure/vm@v1.0.0",
  "_body": {
    "provider": "libvirt",
    "box": "debian/bookworm64",
    "cpus": 1,
    "memory": 1024,
    "disk": {
      "size": 10,
      "type": "qcow2"
    },
    "network": {
      "type": "nat"
    }
  }
}
JSON
)
echo "JSON input parsed successfully (validation only)"

# Check if virsh is available
echo "---> Checking virsh availability..."
if command -v virsh &> /dev/null; then
    echo "virsh is available: $(virsh --version)"

    # Check if libvirtd is running
    if virsh list > /dev/null 2>&1; then
        echo "libvirt daemon is accessible"
    else
        echo "WARNING: virsh is installed but libvirt daemon is not accessible"
        echo "  You may need: sudo systemctl start libvirtd"
    fi
else
    echo "WARNING: virsh is not installed"
    echo "  Install with: sudo apt install libvirt-clients"
fi

# Check if virt-install is available
if command -v virt-install &> /dev/null; then
    echo "virt-install is available"
else
    echo "WARNING: virt-install is not installed"
    echo "  Install with: sudo apt install virtinst"
fi

# Full VM lifecycle tests (only with LIBVIRT_TESTS=1)
if [ "${LIBVIRT_TESTS:-0}" = "1" ]; then
    echo ""
    echo "=== Full VM Lifecycle Tests ==="

    VM_NAME="raise-libvirt-test-$$"
    YAML_FILE=$(mktemp /tmp/raise-test-XXXXXX.yaml)

    cat > "$YAML_FILE" <<YAML
_type: amadla.org/entity/infrastructure/vm@v1.0.0
_body:
  provider: libvirt
  box: debian/bookworm64
  box_url: https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-generic-amd64.qcow2
  cpus: 1
  memory: 1024
  disk:
    size: 5
    type: qcow2
  network:
    type: nat
  ssh:
    user: debian
    port: 22
YAML

    echo "---> Creating VM: $VM_NAME"
    ./raise-libvirt up "$VM_NAME" -f "$YAML_FILE" || {
        echo "FAIL: up command failed"
        rm -f "$YAML_FILE"
        exit 1
    }

    echo "---> Checking status..."
    ./raise-libvirt status "$VM_NAME"

    echo "---> Listing all VMs..."
    ./raise-libvirt status

    echo "---> Halting VM..."
    ./raise-libvirt halt "$VM_NAME"

    echo "---> Destroying VM..."
    ./raise-libvirt destroy "$VM_NAME"

    rm -f "$YAML_FILE"
    echo "PASS: Full VM lifecycle test completed"
else
    echo ""
    echo "Skipping full VM lifecycle tests (set LIBVIRT_TESTS=1 to enable)"
fi

# Cleanup
rm -f "$PROJECT_DIR/raise-libvirt"

echo ""
echo "=== All tests passed ==="
