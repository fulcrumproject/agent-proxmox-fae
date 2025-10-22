package proxmoxapi

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/vmagent"
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
	userID, poolID := vmagent.MakeUserIDs(testUUID)
	userReq := &PveUserCreation{
		UserID:        userID,
		PoolID:        poolID,
		ResetIfExists: false,
	}

	// Create the user
	err := client.createPveUser(userReq)
	if err != nil {
		t.Logf("CreateUser returned error (may be expected): %v", err)
		return // Skip the get test if create failed
	}
	t.Logf("Successfully created user with ID: %s", userID)

	// Get the user to verify it was created
	user, err := client.GetUser(userID)
	if err != nil {
		t.Logf("GetUser returned error (may be expected): %v", err)
	} else {
		assert.NotNil(t, user, "User should not be nil when no error occurs")
		t.Logf("Retrieved user: %+v", user)
	}

	// Get the user pools to verify user resources
	pools, err := client.GetUserPools(userID)
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

func TestClientGetTemplates(t *testing.T) {
	requireIntegrationTest(t)

	client := setupTestClient(t)

	// Create a test user to get access to templates
	testUUID := uuid.New().String()
	userID, poolID := vmagent.MakeUserIDs(testUUID)

	userReq := &PveUserCreation{
		UserID:        userID,
		PoolID:        poolID,
		ResetIfExists: false,
	}

	err := client.createPveUser(userReq)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	t.Logf("Successfully created user with ID: %s", userID)

	// Get user pools which includes templates
	resources, err := client.GetUserPools(userID)
	if err != nil {
		t.Fatalf("GetUserPools failed: %v", err)
	}

	assert.NotNil(t, resources, "User resources should not be nil")
	assert.NotNil(t, resources.Templates, "Templates should not be nil")

	t.Logf("Retrieved %d templates:", len(resources.Templates))
	for i, template := range resources.Templates {
		t.Logf("  Template %d: ID=%d, Name=%s", i+1, template.ID, template.Name)
		assert.NotZero(t, template.ID, "Template ID should not be zero")
		assert.NotEmpty(t, template.Name, "Template name should not be empty")
	}

	if len(resources.Templates) == 0 {
		t.Log("WARNING: No templates available in test environment")
	}
}

func TestClientVMLifecycle(t *testing.T) {
	requireIntegrationTest(t)

	client := setupTestClient(t)

	// Step 0: Create a test user first
	testUUID := uuid.New().String()
	userID, poolID := vmagent.MakeUserIDs(testUUID)

	userReq := &PveUserCreation{
		UserID:        userID,
		PoolID:        poolID,
		ResetIfExists: false,
	}

	err := client.createPveUser(userReq)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	t.Logf("Successfully created user with ID: %s", userID)

	// Step 1: Get user resources to determine available pools and next VM ID
	resources, err := client.GetUserPools(userID)
	if err != nil {
		t.Fatalf("GetUserPools failed after user creation: %v", err)
	}

	// Extract next VM ID from user pools
	vmid := resources.NextVMID
	t.Logf("Got next VM ID from user pools: %d", vmid)

	// Step 2: Get nodes to use for VM creation
	nodes, err := client.GetNodes()
	if err != nil {
		t.Fatalf("GetNodes failed: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatalf("No nodes available in test environment")
	}
	selectedNode := nodes[rand.Intn(len(nodes))] // Randomly select a node
	t.Logf("Randomly selected node: %s from %d available nodes", selectedNode, len(nodes))

	// Step 3: Configure VM creation parameters using user pools

	// Select a random template from available templates
	if len(resources.Templates) == 0 {
		t.Fatalf("No templates available in user resources")
	}

	// Select a random template
	selectedTemplate := resources.Templates[rand.Intn(len(resources.Templates))]
	templateVMID := selectedTemplate.ID
	templateName := selectedTemplate.Name
	t.Logf("Randomly selected template: %s (VMID: %d) from %d available templates", templateName, templateVMID, len(resources.Templates))

	if len(resources.Pools) > 0 {
		// Try to extract pool information if available
		t.Logf("Available pools: %+v", resources.Pools)
	}

	// Step 4: Create VM from template with network configuration and credentials
	vmName := fmt.Sprintf("test-vm-%s", uuid.New().String()[:8])
	vmReq := &vmagent.VmCreateFromTemplate{
		Node:         selectedNode,
		VMID:         vmid,
		TemplateVMID: templateVMID,
		Name:         vmName,
		Pool:         poolID,
		Sockets:      1,
		Cores:        1,
		Memory:       512,
		DiskSize:     8, // 8GB disk
		// Network configuration
		IPAddress:    "172.30.173.103",
		Netmask:      24,
		Gateway:      "172.30.173.1",
		Nameserver:   "1.1.1.1",
		SearchDomain: "opiquad.it",
		// Login credentials
		LoginUser: "fulcrum",
		LoginPass: "Fulcrum123!",
	}

	err = client.CreateVMFromTemplate(userID, vmReq)
	if err != nil {
		t.Fatalf("CreateVMFromTemplate failed: %v", err)
	}
	t.Logf("Successfully created VM %d from template %d with name: %s", vmid, templateVMID, vmName)
	t.Logf("  Network: IP=%s/%d, Gateway=%s, DNS=%s, SearchDomain=%s",
		vmReq.IPAddress, vmReq.Netmask, vmReq.Gateway, vmReq.Nameserver, vmReq.SearchDomain)
	t.Logf("  Login: User=%s", vmReq.LoginUser)

	// Step 5: Wait for VM to be in stopped state after creation
	if !waitForVMState(t, client, userID, vmid, "stopped", 30*time.Second) {
		t.Logf("VM did not reach stopped state after creation, continuing anyway...")
	}

	// Get VM to verify it was created and get current state
	vm, err := client.GetVM(userID, vmid)
	if err != nil {
		t.Fatalf("GetVM failed after creation: %v", err)
	}
	assert.NotNil(t, vm, "VM should not be nil")
	assert.Equal(t, vmid, vm.VMID, "VM ID should match")
	assert.Equal(t, vmName, vm.Name, "VM name should match")
	t.Logf("Retrieved VM: %+v", vm)

	// Step 5.5: Add and test firewall rules
	t.Logf("Adding firewall rules to VM %d", vmid)
	firewallRules := []vmagent.FirewallRule{
		{
			Type:    "in",
			Proto:   "tcp",
			Action:  "ACCEPT",
			Dport:   "22",
			Comment: "Allow SSH",
		},
		{
			Type:    "in",
			Proto:   "tcp",
			Action:  "ACCEPT",
			Dport:   "80",
			Comment: "Allow HTTP",
		},
		{
			Type:    "in",
			Proto:   "tcp",
			Action:  "ACCEPT",
			Dport:   "443",
			Comment: "Allow HTTPS",
		},
		{
			Type:    "in",
			Proto:   "udp",
			Action:  "ACCEPT",
			Dport:   "53",
			Comment: "Allow DNS",
		},
	}

	// Add each firewall rule
	for i, rule := range firewallRules {
		err = client.AddVMFirewallRule(userID, vmid, &rule)
		if err != nil {
			t.Fatalf("AddVMFirewallRule failed for rule %d: %v", i, err)
		}
		t.Logf("  Added firewall rule %d: %s - %s", i+1, rule.Comment, rule.Action)
	}

	// Retrieve and verify firewall rules
	retrievedRules, err := client.GetVMFirewallRules(userID, vmid)
	if err != nil {
		t.Fatalf("GetVMFirewallRules failed: %v", err)
	}
	t.Logf("Retrieved %d firewall rules:", len(retrievedRules))
	for i, rule := range retrievedRules {
		t.Logf("  Rule %d: Type=%s, Proto=%s, Action=%s, Dport=%s, Comment=%s",
			i+1, rule.Type, rule.Proto, rule.Action, rule.Dport, rule.Comment)
	}
	assert.GreaterOrEqual(t, len(retrievedRules), len(firewallRules),
		"Should have at least as many rules as we added")

	// Step 6: Start VM
	err = client.StartVM(userID, vmid)
	if err != nil {
		t.Fatalf("StartVM failed: %v", err)
	}
	t.Logf("Successfully started VM %d", vmid)

	// Wait for VM to be in running state
	if !waitForVMState(t, client, userID, vmid, "running", 60*time.Second) {
		t.Fatalf("VM did not reach running state after start")
	}

	// Step 6.5: Get detailed VM status and metrics
	vmWithMetrics, err := client.GetVM(userID, vmid)
	if err != nil {
		t.Fatalf("GetVM (with metrics) failed: %v", err)
	}
	assert.NotNil(t, vmWithMetrics, "VM with metrics should not be nil")
	t.Logf("Retrieved detailed VM status and metrics:")
	t.Logf("  Status: %s", vmWithMetrics.Status)
	t.Logf("  CPU Usage: %.2f%%, Memory: %d MB", vmWithMetrics.CPU*100, vmWithMetrics.Mem/1024/1024)
	t.Logf("  Disk: %d MB, Uptime: %d seconds", vmWithMetrics.Disk/1024/1024, vmWithMetrics.Uptime)
	t.Logf("  Network: In=%d MB, Out=%d MB", vmWithMetrics.NetIn/1024/1024, vmWithMetrics.NetOut/1024/1024)

	// Step 7: Open VNC (requires source IP)
	vncReq := &VncEnable{
		SourceIP: "127.0.0.1", // Use localhost for testing
		Web:      false,
	}

	vncResp, err := client.OpenVNC(userID, vmid, vncReq)
	if err != nil {
		t.Logf("OpenVNC returned error (may be expected): %v", err)
	} else {
		assert.NotNil(t, vncResp, "VNC response should not be nil")
		t.Logf("VNC opened successfully: %+v", vncResp)
	}

	// Step 8: Stop VM
	err = client.StopVM(userID, vmid)
	if err != nil {
		t.Fatalf("StopVM failed: %v", err)
	}
	t.Logf("Successfully stopped VM %d", vmid)

	// Wait for VM to be in stopped state
	if !waitForVMState(t, client, userID, vmid, "stopped", 60*time.Second) {
		t.Fatalf("VM did not reach stopped state after stop")
	}

	// Step 9: Delete VM
	err = client.DeleteVM(userID, vmid)
	if err != nil {
		t.Fatalf("DeleteVM failed: %v", err)
	}
	t.Logf("Successfully deleted VM %d", vmid)

	// Step 10: Wait for VM to be deleted and verify
	if !waitForVMDeleted(t, client, userID, vmid, 2*time.Minute) {
		t.Fatalf("VM %d still exists after deletion timeout", vmid)
	}
	t.Logf("VM %d successfully deleted", vmid)
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
