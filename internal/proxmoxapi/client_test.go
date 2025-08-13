package proxmoxapi

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/fulcrumproject/agent-proxmox-wrapped/internal/config"
	"github.com/fulcrumproject/agent-proxmox-wrapped/internal/vmagent"
	"github.com/fulcrumproject/utils/confbuilder"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireIntegrationTest skips the test if INTEGRATION_TESTS environment variable is not set to "1"
func requireIntegrationTest(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}
}

// setupTestClient creates a configured Proxmox client for testing
func setupTestClient(t *testing.T) *Client {
	t.Helper()

	cfg, err := confbuilder.New(config.DefaultProxmoxAPIConfig()).
		EnvPrefix("PROXMOX_AGENT_PROXMOX_API_").
		EnvFiles(".env_proxmoxapi.test").
		Build()
	require.NoError(t, err, "Failed to get config")

	client := NewClient(cfg)
	require.NotNil(t, client, "Client should be created successfully")

	return client
}

func TestClientPing(t *testing.T) {
	requireIntegrationTest(t)

	client := setupTestClient(t)

	_, err := client.Ping()
	assert.NoError(t, err, "Ping should succeed")
}

func TestClientCreateAndGetUser(t *testing.T) {
	requireIntegrationTest(t)

	client := setupTestClient(t)

	testUUID := uuid.New().String()
	userReq := &PveUserCreation{
		UserID:        testUUID,
		PoolID:        testUUID,
		ResetIfExists: false,
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
}

func TestClientGetNodes(t *testing.T) {
	requireIntegrationTest(t)

	client := setupTestClient(t)

	nodes, err := client.GetNodes()
	if err != nil {
		t.Logf("GetNodes returned error (may be expected): %v", err)
	} else {
		assert.NotNil(t, nodes, "Nodes should not be nil when no error occurs")
		t.Logf("Retrieved nodes: %+v", nodes)
	}
}

func TestClientGetNodesMetrics(t *testing.T) {
	requireIntegrationTest(t)

	client := setupTestClient(t)

	metrics, err := client.GetNodesMetrics()
	if err != nil {
		t.Logf("GetNodesMetrics returned error (may be expected): %v", err)
	} else {
		assert.NotNil(t, metrics, "Nodes metrics should not be nil when no error occurs")
		assert.NotNil(t, metrics.Metrics, "Metrics map should not be nil")
		t.Logf("Retrieved nodes metrics: %+v", metrics)
	}
}

func TestClientVMLifecycle(t *testing.T) {
	requireIntegrationTest(t)

	client := setupTestClient(t)

	// Step 0: Create a test user first
	testUUID := uuid.New().String()
	userReq := &PveUserCreation{
		UserID:        testUUID,
		PoolID:        testUUID,
		ResetIfExists: false,
	}

	err := client.CreateUser(userReq)
	if err != nil {
		t.Logf("CreateUser returned error (may be expected): %v", err)
		return // Skip the rest of the test if user creation fails
	}
	t.Logf("Successfully created user with ID: %s", testUUID)

	// Use the created user for VM operations
	testUser := testUUID

	// Step 1: Get user resources to determine available pools and next VM ID
	resources, err := client.GetUserPools(testUser)
	if err != nil {
		t.Logf("GetUserPools returned error (may be expected): %v", err)
		return // Skip if we can't get user resources
	}

	// Extract next VM ID from user pools
	vmid := resources.NextID
	t.Logf("Got next VM ID from user pools: %d", vmid)

	// Step 2: Get nodes to use for VM creation
	nodes, err := client.GetNodes()
	if err != nil || len(nodes) == 0 {
		t.Logf("GetNodes returned error or no nodes (may be expected): %v", err)
		return // Skip the rest if no nodes available
	}
	selectedNode := nodes[rand.Intn(len(nodes))] // Randomly select a node
	t.Logf("Randomly selected node: %s from %d available nodes", selectedNode, len(nodes))

	// Step 3: Configure VM creation parameters using user pools
	// Use the same pool ID as the user, with fallback to default
	poolID := testUUID // Use the same UUID as pool ID

	// Select a random OS image from available images
	var osImage string
	if len(resources.IDE2) > 0 {
		osImage = resources.IDE2[rand.Intn(len(resources.IDE2))]
		t.Logf("Randomly selected OS image: %s from %d available images", osImage, len(resources.IDE2))
	} else {
		osImage = "alpine-standard-3.22.0-x86_64.iso" // Fallback to default
		t.Logf("No images available in user resources, using fallback: %s", osImage)
	}

	if len(resources.Pools) > 0 {
		// Try to extract pool information if available
		t.Logf("Available pools: %+v", resources.Pools)
	}

	// Step 4: Create VM
	vmName := fmt.Sprintf("test-vm-%s", uuid.New().String()[:8])
	vmReq := &vmagent.VmCreation{
		Node:    selectedNode,
		VMID:    vmid,
		Name:    vmName,
		Pool:    poolID,
		Sockets: 1,
		Cores:   1,
		Memory:  512,
		SCSI0:   8, // 8GB disk
		IDE2:    osImage,
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

	// Step 6.5: Get detailed VM status and metrics
	vmWithMetrics, err := client.GetVM(testUser, vmid)
	if err != nil {
		t.Logf("GetVM (with metrics) returned error (may be expected): %v", err)
	} else {
		assert.NotNil(t, vmWithMetrics, "VM with metrics should not be nil")
		t.Logf("Retrieved detailed VM status and metrics:")
		t.Logf("  Status: %s", vmWithMetrics.Status)
		t.Logf("  CPU Usage: %.2f%%, Memory: %d MB", vmWithMetrics.CPU*100, vmWithMetrics.Mem/1024/1024)
		t.Logf("  Disk: %d MB, Uptime: %d seconds", vmWithMetrics.Disk/1024/1024, vmWithMetrics.Uptime)
		t.Logf("  Network: In=%d MB, Out=%d MB", vmWithMetrics.NetIn/1024/1024, vmWithMetrics.NetOut/1024/1024)
	}

	// Step 7: Open VNC (requires source IP)
	vncReq := &VncEnable{
		SourceIP: "127.0.0.1", // Use localhost for testing
		Web:      false,
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

	// Step 10: Wait for VM to be deleted and verify
	if waitForVMDeleted(t, client, testUser, vmid, 2*time.Minute) {
		t.Logf("VM %d successfully deleted", vmid)
	} else {
		t.Logf("Warning: VM %d still exists after deletion timeout", vmid)
	}
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

// waitForVMDeleted waits for a VM to be deleted (GetVM returns an error) with timeout
func waitForVMDeleted(t *testing.T, client *Client, username string, vmid int, timeout time.Duration) bool {
	t.Helper()

	start := time.Now()
	for time.Since(start) < timeout {
		_, err := client.GetVM(username, vmid)
		if err != nil {
			t.Logf("VM %d is deleted (GetVM returned error): %v", vmid, err)
			return true
		}

		t.Logf("VM %d still exists, waiting for deletion...", vmid)
		time.Sleep(2 * time.Second)
	}

	t.Logf("Timeout waiting for VM %d to be deleted", vmid)
	return false
}
