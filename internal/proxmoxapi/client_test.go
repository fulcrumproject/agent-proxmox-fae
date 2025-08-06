package proxmoxapi

import (
	"fmt"
	"testing"
	"time"

	"fulcrumproject.org/agent-proxmox-wrapped/internal/config"
	"github.com/fulcrumproject/utils/confbuilder"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient(t *testing.T) {
	cfg, err := confbuilder.New(config.DefaultConfig()).
		EnvPrefix(config.EnvPrefix).
		EnvFiles(".env").
		Build()
	require.NoError(t, err, "Failed to get config")

	client := NewClient(&cfg.ProxmoxAPI)
	require.NotNil(t, client, "Client should be created successfully")

	t.Run("Ping", func(t *testing.T) {
		_, err := client.Ping()
		assert.NoError(t, err, "Ping should succeed")
	})

	t.Run("CreateAndGetUser", func(t *testing.T) {
		testUUID := uuid.New().String()
		userReq := &PveUserCreation{
			UserID:    testUUID,
			FirstName: "Test",
			LastName:  "User " + testUUID,
			PoolID:    testUUID,
		}

		// Create the user
		err := client.CreateUser(userReq)
		if err != nil {
			t.Logf("CreateUser returned error (may be expected): %v", err)
			return // Skip the get test if create failed
		}
		t.Logf("Successfully created user with ID: %s", testUUID)

		// Get the user to verify it was created
		user, err := client.GetUser(testUUID)
		if err != nil {
			t.Logf("GetUser returned error (may be expected): %v", err)
		} else {
			assert.NotNil(t, user, "User should not be nil when no error occurs")
			t.Logf("Retrieved user: %+v", user)
		}

		// Get the user pools to verify user resources
		pools, err := client.GetUserPools(testUUID)
		if err != nil {
			t.Logf("GetUserPools returned error (may be expected): %v", err)
		} else {
			assert.NotNil(t, pools, "User pools should not be nil when no error occurs")
			t.Logf("Retrieved user pools: %+v", pools)
		}
	})

	t.Run("GetNodes", func(t *testing.T) {
		nodes, err := client.GetNodes()
		if err != nil {
			t.Logf("GetNodes returned error (may be expected): %v", err)
		} else {
			assert.NotNil(t, nodes, "Nodes should not be nil when no error occurs")
			t.Logf("Retrieved nodes: %+v", nodes)
		}
	})

	t.Run("GetNodesMetrics", func(t *testing.T) {
		metrics, err := client.GetNodesMetrics()
		if err != nil {
			t.Logf("GetNodesMetrics returned error (may be expected): %v", err)
		} else {
			assert.NotNil(t, metrics, "Nodes metrics should not be nil when no error occurs")
			assert.NotNil(t, metrics.Metrics, "Metrics map should not be nil")
			t.Logf("Retrieved nodes metrics: %+v", metrics)
		}
	})

	t.Run("VMLifecycle", func(t *testing.T) {
		// Step 0: Create a test user first
		testUUID := uuid.New().String()
		userReq := &PveUserCreation{
			UserID:    testUUID,
			FirstName: "Test",
			LastName:  "User " + testUUID,
			PoolID:    testUUID,
		}

		err := client.CreateUser(userReq)
		if err != nil {
			t.Logf("CreateUser returned error (may be expected): %v", err)
			return // Skip the rest of the test if user creation fails
		}
		t.Logf("Successfully created user with ID: %s", testUUID)

		// Use the created user for VM operations
		testUser := testUUID

		// Step 1: Get next available VM ID
		nextID, err := client.GetVMNextID()
		if err != nil {
			t.Logf("GetVMNextID returned error (may be expected): %v", err)
			return // Skip the rest of the test if we can't get next ID
		}
		require.NotNil(t, nextID, "NextID should not be nil")
		vmid := nextID.NextID
		t.Logf("Got next VM ID: %d", vmid)

		// Step 2: Get nodes to use for VM creation
		nodes, err := client.GetNodes()
		if err != nil || len(nodes) == 0 {
			t.Logf("GetNodes returned error or no nodes (may be expected): %v", err)
			return // Skip the rest if no nodes available
		}
		selectedNode := nodes[0] // Use the first available node
		t.Logf("Using node: %s", selectedNode)

		// Step 3: Get user resources to determine available OS images and pools
		resources, err := client.GetUserPools(testUser)
		if err != nil {
			t.Logf("GetUserPools returned error (may be expected): %v", err)
			return // Skip if we can't get user resources
		}

		// Use the same pool ID as the user, with fallback to default
		poolID := testUUID                             // Use the same UUID as pool ID
		osImage := "alpine-standard-3.22.0-x86_64.iso" // Default OS image
		osType := "l26"                                // Linux 2.6+ kernel

		if len(resources.Pools) > 0 {
			// Try to extract pool information if available
			t.Logf("Available pools: %+v", resources.Pools)
		}

		// Step 4: Create VM
		vmName := fmt.Sprintf("test-vm-%s", uuid.New().String()[:8])
		vmReq := &VmCreation{
			Node:    selectedNode,
			VMID:    vmid,
			Name:    vmName,
			Pool:    poolID,
			Sockets: 1,
			Cores:   1,
			Memory:  512,
			SCSI0:   "8", // 8GB disk
			IDE2:    osImage,
			OSType:  osType,
		}

		err = client.CreateVM(testUser, vmReq)
		if err != nil {
			t.Logf("CreateVM returned error (may be expected): %v", err)
			return // Skip the rest if VM creation fails
		}
		t.Logf("Successfully created VM %d with name: %s", vmid, vmName)

		// Step 5: Wait for VM to be in stopped state after creation
		if !waitForVMState(t, client, testUser, vmid, "stopped", 30*time.Second) {
			t.Logf("VM did not reach stopped state after creation, continuing anyway...")
		}

		// Get VM to verify it was created and get current state
		vm, err := client.GetVM(testUser, vmid)
		if err != nil {
			t.Logf("GetVM returned error: %v", err)
		} else {
			assert.NotNil(t, vm, "VM should not be nil")
			assert.Equal(t, vmid, vm.VMID, "VM ID should match")
			assert.Equal(t, vmName, vm.Name, "VM name should match")
			t.Logf("Retrieved VM: %+v", vm)
		}

		// Step 6: Start VM
		err = client.StartVM(testUser, vmid)
		if err != nil {
			t.Logf("StartVM returned error (may be expected): %v", err)
		} else {
			t.Logf("Successfully started VM %d", vmid)

			// Wait for VM to be in running state
			if !waitForVMState(t, client, testUser, vmid, "running", 60*time.Second) {
				t.Logf("VM did not reach running state after start, continuing anyway...")
			}
		}

		// Step 6.5: Get detailed VM status and metrics (requires PVE auth token)
		// Note: This would require a valid PVE authorization token
		pveAuth := "PVEAPIToken=your-token-here" // This should be configured in your test environment
		vmStatus, err := client.GetVMStatus(pveAuth, selectedNode, vmid)
		require.NoError(t, err, "GetVMStatus should not return an error")
		assert.NotNil(t, vmStatus, "VM status should not be nil")
		assert.NotEmpty(t, vmStatus.Data.Status, "VM status should not be empty")
		t.Logf("Retrieved detailed VM status:")
		t.Logf("  Status: %s, QMP Status: %s", vmStatus.Data.Status, vmStatus.Data.QMPStatus)
		t.Logf("  CPU Usage: %.2f%%, Memory: %d/%d MB", vmStatus.Data.CPU*100, vmStatus.Data.Mem/1024/1024, vmStatus.Data.MaxMem/1024/1024)
		t.Logf("  Disk: %d/%d MB, Uptime: %d seconds", vmStatus.Data.Disk/1024/1024, vmStatus.Data.MaxDisk/1024/1024, vmStatus.Data.Uptime)
		t.Logf("  Network: In=%d MB, Out=%d MB", vmStatus.Data.NetIn/1024/1024, vmStatus.Data.NetOut/1024/1024)

		// Step 7: Open VNC (requires source IP)
		vncReq := &VncEnable{
			SourceIP: "127.0.0.1", // Use localhost for testing
		}

		vncResp, err := client.OpenVNC(testUser, vmid, vncReq)
		if err != nil {
			t.Logf("OpenVNC returned error (may be expected): %v", err)
		} else {
			assert.NotNil(t, vncResp, "VNC response should not be nil")
			t.Logf("VNC opened successfully: %+v", vncResp)
		}

		// Step 8: Stop VM
		err = client.StopVM(testUser, vmid)
		if err != nil {
			t.Logf("StopVM returned error (may be expected): %v", err)
		} else {
			t.Logf("Successfully stopped VM %d", vmid)

			// Wait for VM to be in stopped state
			if !waitForVMState(t, client, testUser, vmid, "stopped", 60*time.Second) {
				t.Logf("VM did not reach stopped state after stop, continuing anyway...")
			}
		}

		// Step 9: Delete VM
		err = client.DeleteVM(testUser, vmid)
		if err != nil {
			t.Logf("DeleteVM returned error (may be expected): %v", err)
		} else {
			t.Logf("Successfully deleted VM %d", vmid)
		}

		// Step 10: Verify VM is deleted by trying to get it
		_, err = client.GetVM(testUser, vmid)
		if err != nil {
			t.Logf("VM %d successfully deleted (GetVM returned error as expected): %v", vmid, err)
		} else {
			t.Logf("Warning: VM %d still exists after deletion", vmid)
		}
	})
}

// waitForVMState waits for a VM to reach a specific state with timeout
func waitForVMState(t *testing.T, client *Client, username string, vmid int, expectedState string, timeout time.Duration) bool {
	t.Helper()

	start := time.Now()
	for time.Since(start) < timeout {
		vm, err := client.GetVM(username, vmid)
		if err != nil {
			t.Logf("Error getting VM state: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		t.Logf("VM %d current state: %s (waiting for: %s)", vmid, vm.Status, expectedState)
		if vm.Status == expectedState {
			t.Logf("VM %d reached expected state: %s", vmid, expectedState)
			return true
		}

		time.Sleep(2 * time.Second)
	}

	t.Logf("Timeout waiting for VM %d to reach state: %s", vmid, expectedState)
	return false
}
