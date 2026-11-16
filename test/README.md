# Test Directory

Contains test resources for the Proxmox VM Agent.

## SSH keys

`test_vm_key` and `test_vm_key.pub` are legacy shared fixtures. **Do not use
this committed private key to authenticate a public VM.**

The integration lifecycle test generates a fresh Ed25519 key. A failed run
leaves the VM and its private key (mode 600, in a protected temporary directory)
for investigation; the log prints the key's path, not its contents.

```bash
ssh -i /path/from/test/log/vm_key fulcrum@<TEST_VM_IP>
```

Successful runs delete the test VM and temporary key. Failed runs require
explicit cleanup after inspection. See the root README for network/VLAN
configuration, separate creation/readiness timeouts and live-test commands.
