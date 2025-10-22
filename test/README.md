# Test Directory

Contains test resources for the Proxmox VM Agent.

## SSH Keys

Test SSH key pair for VM authentication:

- `test_vm_key` - Private key (Ed25519)
- `test_vm_key.pub` - Public key

**Username**: `fulcrum`

### Connecting to VMs

```bash
ssh -i test/test_vm_key fulcrum@<VM_IP>
```


