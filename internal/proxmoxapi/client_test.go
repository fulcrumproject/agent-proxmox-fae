package proxmoxapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/vmagent"
	"github.com/fulcrumproject/utils/confbuilder"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func requireIntegrationTest(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("Set INTEGRATION_TESTS=1 to run against a real FAE test environment")
	}
}

func setupTestClient(t *testing.T) *Client {
	t.Helper()
	cfg, err := confbuilder.New(config.DefaultProxmoxAPIConfig()).
		EnvPrefix("PROXMOX_AGENT_PROXMOX_API_").
		EnvFiles(".env_proxmoxapi.test").Build()
	require.NoError(t, err)
	// Do not print credentials or fail over to an unrelated cluster during tests.
	require.Len(t, cfg.APIURLs, 1, "Configure one dedicated FAE test endpoint")
	cfg.Debug = false
	client := NewClient(cfg)
	t.Cleanup(func() { _ = client.restyClient.Close() })
	return client
}

func createTestUser(t *testing.T, client *Client) (string, string) {
	t.Helper()
	userID, poolID := vmagent.MakeUserIDs(uuid.NewString())
	// FAE 0.17 has no user/pool deletion endpoint. Record the IDs even if a
	// failed request partially creates them; never clean up via an unrelated API.
	t.Logf("Test user/pool (operator cleanup required): %s / %s", userID, poolID)
	require.NoError(t, client.CreateUser(&vmagent.PveUserCreation{UserID: userID, PoolID: poolID}))
	return userID, poolID
}

func TestClientPing(t *testing.T) {
	requireIntegrationTest(t)
	client := setupTestClient(t)
	ping, err := client.Ping()
	require.NoError(t, err)
	assert.True(t, ping.Fae, "FAE must be healthy")
	assert.True(t, ping.Proxmox, "Proxmox must be healthy")
}

func TestClientCreateAndGetUser(t *testing.T) {
	requireIntegrationTest(t)
	client := setupTestClient(t)
	userID, poolID := createTestUser(t, client)
	user, err := client.GetUser(userID)
	require.NoError(t, err)
	require.NotNil(t, user)
	resources, err := client.GetUserPools(userID)
	require.NoError(t, err)
	require.Contains(t, resources.Pools, poolID)
}

func TestClientGetNodes(t *testing.T) {
	requireIntegrationTest(t)
	client := setupTestClient(t)
	nodes, err := client.GetNodes()
	require.NoError(t, err)
	require.NotEmpty(t, nodes)
	t.Logf("Available nodes: %d", len(nodes))
}

func TestClientGetNodesMetrics(t *testing.T) {
	requireIntegrationTest(t)
	client := setupTestClient(t)
	metrics, err := client.GetNodesMetrics()
	require.NoError(t, err)
	require.NotEmpty(t, metrics.Metrics)
	t.Logf("Nodes with metrics: %d", len(metrics.Metrics))
}

func TestClientGetTemplates(t *testing.T) {
	requireIntegrationTest(t)
	client := setupTestClient(t)
	userID, _ := createTestUser(t, client)
	resources, err := client.GetUserPools(userID)
	require.NoError(t, err)
	require.NotEmpty(t, resources.Templates)
	for _, template := range resources.Templates {
		assert.NotZero(t, template.ID)
		assert.NotEmpty(t, template.Name)
		t.Logf("Template: %s (VMID %d)", template.Name, template.ID)
	}
}

func TestClientVMLifecycle(t *testing.T) {
	requireIntegrationTest(t)
	client := setupTestClient(t)

	// Validate approved fixtures before creating any resources. No hard-coded
	// address, implicit DHCP assumption or random OS/template selection.
	templateName := os.Getenv("PROXMOX_AGENT_TEST_TEMPLATE")
	ipAddress := os.Getenv("PROXMOX_AGENT_TEST_IPADDRESS")
	gateway := os.Getenv("PROXMOX_AGENT_TEST_GATEWAY")
	require.NotEmpty(t, templateName, "Set PROXMOX_AGENT_TEST_TEMPLATE to a Linux cloud-init template with base disk <= 20 GiB")
	require.NotNil(t, net.ParseIP(ipAddress).To4(), "Set PROXMOX_AGENT_TEST_IPADDRESS to a reserved test IPv4 address on vmbr")
	require.NotNil(t, net.ParseIP(gateway).To4(), "Set PROXMOX_AGENT_TEST_GATEWAY to the test network gateway")
	netmask, err := strconv.Atoi(os.Getenv("PROXMOX_AGENT_TEST_NETMASK"))
	require.NoError(t, err, "Set PROXMOX_AGENT_TEST_NETMASK to the IPv4 prefix length")
	require.GreaterOrEqual(t, netmask, 8)
	require.LessOrEqual(t, netmask, 32)
	var vlanTag *int
	if raw := os.Getenv("PROXMOX_AGENT_TEST_VLAN_TAG"); raw != "" {
		tag, err := strconv.Atoi(raw)
		require.NoError(t, err, "PROXMOX_AGENT_TEST_VLAN_TAG must be an integer")
		require.GreaterOrEqual(t, tag, 1)
		require.LessOrEqual(t, tag, 4094)
		vlanTag = &tag
	}
	readyTimeout := config.DefaultReadyTimeout
	if raw := os.Getenv("PROXMOX_AGENT_PROXMOX_API_READY_TIMEOUT"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		require.NoError(t, err)
		require.GreaterOrEqual(t, seconds, 0)
		if seconds > 0 {
			readyTimeout = time.Duration(seconds) * time.Second
		}
	}
	sshKey := createIntegrationSSHKey(t)

	userID, poolID := createTestUser(t, client)
	_, err = client.GetUser(userID)
	require.NoError(t, err)
	resources, err := client.GetUserPools(userID)
	require.NoError(t, err)
	require.NotEmpty(t, resources.Nodes)
	require.Contains(t, resources.Pools, poolID)
	require.GreaterOrEqual(t, resources.NextVMID, 100)

	var templateID int
	for _, candidate := range resources.Templates {
		if candidate.Name == templateName {
			templateID = candidate.ID
			break
		}
	}
	require.GreaterOrEqual(t, templateID, 100, "Requested template must be available")

	vmid := resources.NextVMID
	vmName := "test-vm-" + uuid.NewString()[:8]
	vmReq := &vmagent.VmCreateFromTemplate{
		Node: resources.Nodes[0], VMID: vmid, TemplateVMID: templateID,
		Name: vmName, Pool: poolID, CPUs: 4, Memory: 4096, DiskSize: 20,
		Bridge: "vmbr", VLANTag: vlanTag, IPAddress: ipAddress, Netmask: netmask, Gateway: gateway,
		Nameserver:   os.Getenv("PROXMOX_AGENT_TEST_NAMESERVER"),
		SearchDomain: os.Getenv("PROXMOX_AGENT_TEST_SEARCHDOMAIN"),
		LoginUser:    "fulcrum",
		LoginSSHKey:  sshKey,
	}
	// Preserve failed/ambiguous provisioning for operator inspection, including a
	// create request that timed out while FAE continues working in the background.
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("No automatic cleanup after failure: inspect VM %d (%s), user %s, pool %s", vmid, vmName, userID, poolID)
		}
	})
	t.Logf("Creating VM %d (%s), node %s, template %s, network %s/%d", vmid, vmName, vmReq.Node, templateName, ipAddress, netmask)
	require.NoError(t, client.CreateVMFromTemplate(userID, vmReq))
	readyVM, err := client.WaitForVMReady(context.Background(), userID, vmid, readyTimeout)
	require.NoError(t, err)
	require.Equal(t, vmid, readyVM.VMID)
	require.Equal(t, vmName, readyVM.Name)
	require.True(t, readyVM.QemuReady)
	require.False(t, readyVM.Lock)
	require.Contains(t, []string{"running", "stopped"}, readyVM.Status)
	t.Logf("Provisioning completed: VM %d status=%s qemuReady=%t", vmid, readyVM.Status, readyVM.QemuReady)
	if readyVM.Status == "running" {
		require.NoError(t, client.StopVM(userID, vmid))
		require.NoError(t, client.WaitForVMState(userID, vmid, "stopped", time.Minute))
	}
	t.Logf("Creation finalized: VM %d is stopped", vmid)
	vm, err := client.GetVM(userID, vmid)
	require.NoError(t, err)
	require.Equal(t, vmid, vm.VMID)
	require.Equal(t, vmName, vm.Name)
	require.False(t, vm.Lock)
	require.Equal(t, "stopped", vm.Status)

	// Partial updates must not send zeroes for unrelated fields.
	cpus, memory, diskSize := 2, 2048, 21
	cpuUpdateStarted := time.Now()
	require.NoError(t, client.PatchVM(userID, vmid, &vmagent.VmPatch{CPUs: &cpus}))
	// FAE can acknowledge a patch before subsequent reads reflect it.
	// Keep the same value/isolation assertions, with a bounded visibility wait.
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		updated, err := client.GetVM(userID, vmid)
		if !assert.NoError(check, err) {
			return
		}
		assert.Equal(check, float64(cpus), updated.MaxCPU)
		assert.Equal(check, vm.MaxMem, updated.MaxMem, "CPU-only patch must preserve memory")
		assert.Equal(check, vm.MaxDisk, updated.MaxDisk, "CPU-only patch must preserve disk")
		assert.Equal(check, "stopped", updated.Status)
		assert.False(check, updated.Lock)
	}, time.Minute, 2*time.Second, "CPU update must become visible without changing unrelated resources")
	t.Logf("CPU update verified after %s", time.Since(cpuUpdateStarted).Round(time.Second))
	require.NoError(t, client.PatchVM(userID, vmid, &vmagent.VmPatch{Memory: &memory}))
	require.NoError(t, client.PatchVM(userID, vmid, &vmagent.VmPatch{DiskSize: &diskSize}))
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		updated, err := client.GetVM(userID, vmid)
		if !assert.NoError(check, err) {
			return
		}
		assert.Equal(check, float64(cpus), updated.MaxCPU)
		assert.Equal(check, int64(memory)*1024*1024, updated.MaxMem)
		assert.Greater(check, updated.MaxDisk, vm.MaxDisk, "Disk patch must grow the disk")
		assert.Equal(check, "stopped", updated.Status)
		assert.False(check, updated.Lock)
	}, time.Minute, 2*time.Second, "CPU, memory and disk updates must be visible before continuing")
	t.Log("CPU, memory and disk updates verified")

	rules := []vmagent.FirewallRule{
		{Type: "in", Proto: "tcp", Action: "ACCEPT", Dport: "22", Comment: "Agent test SSH"},
		{Type: "in", Proto: "tcp", Action: "ACCEPT", Dport: "80", Comment: "Agent test HTTP"},
		{Type: "in", Proto: "tcp", Action: "ACCEPT", Dport: "443", Comment: "Agent test HTTPS"},
		{Type: "in", Proto: "udp", Action: "ACCEPT", Dport: "53", Comment: "Agent test DNS"},
	}
	for _, rule := range rules {
		require.NoError(t, client.AddVMFirewallRule(userID, vmid, &rule))
	}
	actualRules, err := client.GetVMFirewallRules(userID, vmid)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(actualRules), len(rules))
	var testRulePosition *int
	for _, expected := range rules {
		found := false
		for _, actual := range actualRules {
			if actual.Comment == expected.Comment && actual.Type == expected.Type && actual.Proto == expected.Proto && actual.Dport == expected.Dport && actual.Action == expected.Action {
				found = true
				testRulePosition = actual.Pos
				break
			}
		}
		require.True(t, found, "Firewall rule %q must be present", expected.Comment)
	}
	require.NotNil(t, testRulePosition)
	require.NoError(t, client.DeleteVMFirewallRule(userID, vmid, *testRulePosition))
	afterDelete, err := client.GetVMFirewallRules(userID, vmid)
	require.NoError(t, err)
	require.Len(t, afterDelete, len(actualRules)-1)

	require.NoError(t, client.StartVM(userID, vmid))
	require.NoError(t, client.WaitForVMState(userID, vmid, "running", 60*time.Second))
	metrics, err := client.GetVM(userID, vmid)
	require.NoError(t, err)
	require.Equal(t, "running", metrics.Status)
	require.Equal(t, float64(cpus), metrics.MaxCPU)
	require.Equal(t, int64(memory)*1024*1024, metrics.MaxMem)

	// Console access is not an agent job; require an approved source IP to test it.
	if !t.Run("VNC", func(t *testing.T) {
		sourceIP := os.Getenv("PROXMOX_AGENT_TEST_VNC_SOURCE_IP")
		if sourceIP == "" {
			t.Skip("Set PROXMOX_AGENT_TEST_VNC_SOURCE_IP to exercise optional console access")
		}
		vnc, err := client.OpenVNC(userID, vmid, &VncEnable{SourceIP: sourceIP})
		require.NoError(t, err)
		require.NotEmpty(t, vnc.Host)
	}) {
		return // Preserve the VM if the optional console check fails.
	}

	require.NoError(t, client.StopVM(userID, vmid))
	require.NoError(t, client.WaitForVMState(userID, vmid, "stopped", 60*time.Second))
	require.NoError(t, client.DeleteVM(userID, vmid))
	require.True(t, waitForVMDeleted(t, client, userID, vmid, 2*time.Minute))
	t.Logf("VM %d lifecycle passed and VM deletion confirmed", vmid)
}

func isNotFound(err error) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
}

// A timeout, 401, 403 or 500 must never be mistaken for a deleted VM.
func waitForVMDeleted(t *testing.T, client *Client, username string, vmid int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := client.GetVM(username, vmid)
		if isNotFound(err) {
			return true
		}
		if err != nil {
			t.Logf("Cannot verify VM deletion: %v", err)
			return false
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("VM %d still exists after deletion timeout", vmid)
	return false
}

// Never expose a public test VM using the shared private key in the repository.
func createIntegrationSSHKey(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "fae-integration-key-")
	require.NoError(t, err)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("SSH key retained for test-VM inspection: %s", filepath.Join(dir, "vm_key"))
			return
		}
		assert.NoError(t, os.RemoveAll(dir))
	})
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(private, "fae-integration")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vm_key"), pem.EncodeToMemory(block), 0600))
	key, err := ssh.NewPublicKey(public)
	require.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}
