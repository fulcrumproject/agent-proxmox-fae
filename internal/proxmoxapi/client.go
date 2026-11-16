package proxmoxapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"resty.dev/v3"

	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/vmagent"
)

// Client represents a REST client for the Proxmox API with multi-endpoint support
type Client struct {
	restyClient   *resty.Client
	createTimeout time.Duration
	endpoints     []string     // Available API endpoints
	mu            sync.RWMutex // Thread-safe access to endpoints
}

// HTTPError retains the response status, so callers can distinguish missing
// resources from authentication, validation and server failures.
type HTTPError struct {
	Operation  string
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s failed with status %d", e.Operation, e.StatusCode)
}

// PveUser represents a PVE user response
type PveUser struct {
	Enabled bool `json:"enabled"`
	HasAPI  bool `json:"hasApi"`
	Status  bool `json:"status"`
}

// NodesMetrics represents the metrics for all nodes
type NodesMetrics struct {
	Metrics map[string]NodesMetricsNode `json:"metrics"`
}

// NodesMetricsNode represents the metrics for a single node
type NodesMetricsNode struct {
	Mem  NodesMetricsNodeData `json:"mem"`
	CPU  NodesMetricsNodeData `json:"cpu"`
	Disk NodesMetricsNodeData `json:"disk"`
}

// NodesMetricsNodeData represents the data for a specific metric
type NodesMetricsNodeData struct {
	Cur float64 `json:"cur"`
	Max float64 `json:"max"`
	Pct float64 `json:"pct"`
}

// NextID represents the response for getting the next available VM ID
type NextID struct {
	NextID int `json:"nextvmid"`
}

// VmClone represents the request for cloning a VM
type VmClone struct {
	NewID int    `json:"newid"`
	Name  string `json:"name"`
	Pool  string `json:"pool"`
}

// VmMigrate represents the request for migrating a VM
type VmMigrate struct {
	Target string `json:"target"`
}

// VncEnable represents the request for enabling VNC
type VncEnable struct {
	SourceIP string `json:"source_ip"`
	Web      bool   `json:"web"`
}

// VncEnableResponse represents the response for VNC enabling
type VncEnableResponse struct {
	Host string `json:"host"`
}

// PveUserAddPool represents the request for adding a pool to a PVE user
type PveUserAddPool struct {
	PoolID         string `json:"poolid"`
	AssignIfExists bool   `json:"assign_if_exists"`
}

// FaeSecrets represents the response for generating AES keys
type FaeSecrets struct {
	AES string `json:"aes"`
	JWT string `json:"jwt"`
}

// FaeAuthTokenRequest represents the request for generating FAE auth tokens
type FaeAuthTokenRequest struct {
	App   string        `json:"app"`
	Roles []interface{} `json:"roles"`
}

// SSHKeyRequest represents the request for generating SSH keys
type SSHKeyRequest struct {
	Password string `json:"password"`
	Comment  string `json:"comment"`
}

// SSHKey represents the SSH key pair response
type SSHKey struct {
	Private string `json:"private"`
	Public  string `json:"public"`
}

// Snapshot represents a VM snapshot
type Snapshot struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parent      string `json:"parent,omitempty"`
	SnapTime    int64  `json:"snaptime,omitempty"`
	VMState     bool   `json:"vmstate,omitempty"`
	Running     bool   `json:"running,omitempty"`
}

// TakeSnapshot represents the request for taking a snapshot
type TakeSnapshot struct {
	SnapName    string `json:"snapname"`
	Description string `json:"description,omitempty"`
	VMState     bool   `json:"vmstate,omitempty"`
}

// SupportVm represents a Fulcrum support VM
type SupportVm struct {
	ID       int                    `json:"id"`
	Name     string                 `json:"name"`
	Hostname string                 `json:"hostname"`
	IPv4     string                 `json:"ipv4"`
	Config   map[string]interface{} `json:"config,omitempty"`
}

// FulcrumNetworkVmConfig represents configuration for Fulcrum Network VM
type FulcrumNetworkVmConfig struct {
	Daemons string `json:"daemons,omitempty"`
	FrrConf string `json:"frr_conf,omitempty"`
	RcLocal string `json:"rc_local,omitempty"`
	VlanTag int    `json:"vlan_tag,omitempty"`
}

// FulcrumAgentVmConfig represents configuration for Fulcrum Agent VM
type FulcrumAgentVmConfig struct {
	Env     string `json:"env,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

// FaeAuthToken represents the response for FAE auth token generation
type FaeAuthToken struct {
	Token string `json:"token"`
}

// VmStatus represents the detailed VM status from Proxmox API
type VmStatus struct {
	Data VmStatusData `json:"data"`
}

// VmStatusData represents the detailed VM status data from Proxmox API
type VmStatusData struct {
	Status    string  `json:"status"`    // VM status: "running", "stopped"
	QMPStatus string  `json:"qmpstatus"` // QEMU status: "running", "stopped", "paused", "prelaunch"
	VMID      int     `json:"vmid"`      // VM ID
	Name      string  `json:"name"`      // VM name
	Node      string  `json:"node"`      // Node name where VM is running
	CPU       float64 `json:"cpu"`       // Current CPU usage (0.0-1.0)
	CPUs      int     `json:"cpus"`      // Number of CPUs
	Disk      int64   `json:"disk"`      // Disk usage in bytes
	DiskRead  int64   `json:"diskread"`  // Disk read bytes
	DiskWrite int64   `json:"diskwrite"` // Disk write bytes
	MaxDisk   int64   `json:"maxdisk"`   // Maximum disk space in bytes
	MaxMem    int64   `json:"maxmem"`    // Maximum memory in bytes
	Mem       int64   `json:"mem"`       // Current memory usage in bytes
	NetIn     int64   `json:"netin"`     // Network bytes received
	NetOut    int64   `json:"netout"`    // Network bytes sent
	PID       int     `json:"pid"`       // Process ID of QEMU process
	Uptime    int64   `json:"uptime"`    // VM uptime in seconds
	Template  int     `json:"template"`  // Whether this is a template (0 or 1)
	Lock      string  `json:"lock"`      // VM lock status
	Tags      string  `json:"tags"`      // VM tags
}

// NewClient creates a new Proxmox API REST client with multi-endpoint support
func NewClient(cfg *config.ProxmoxAPIConfig) *Client {
	if len(cfg.APIURLs) == 0 {
		panic("at least one API URL is required in apiUrls")
	}

	// Store endpoints (will be shuffled per-request for load balancing)
	endpoints := make([]string, len(cfg.APIURLs))
	copy(endpoints, cfg.APIURLs)

	// Create a new resty client
	restyClient := resty.New()

	// Configure the client with settings from Proxmox config
	// Note: BaseURL will be set per-request in executeWithFailover
	restyClient.
		SetHeader("Authorization", cfg.APIToken).
		SetHeader("Content-Type", "application/json").
		SetTimeout(time.Duration(cfg.Timeout) * time.Second).
		SetDebug(cfg.Debug)

	// Configure TLS settings
	restyClient.SetTLSClientConfig(&tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipTLS,
	})

	slog.Info("Initialized Proxmox client", "endpoints", endpoints)

	return &Client{
		restyClient:   restyClient,
		createTimeout: cfg.CreationTimeout(),
		endpoints:     endpoints,
	}
}

// executeWithFailover executes a request with automatic failover across all endpoints
// Shuffles endpoints on each call for true random load balancing
func (c *Client) executeWithFailover(operation string, reqFunc func(baseURL string) (*resty.Response, error)) (*resty.Response, error) {
	// Copy and shuffle endpoints for this request
	c.mu.RLock()
	shuffled := make([]string, len(c.endpoints))
	copy(shuffled, c.endpoints)
	c.mu.RUnlock()

	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	var lastErr error

	// Try each endpoint in shuffled order
	for attempt, endpoint := range shuffled {
		resp, err := reqFunc(endpoint)

		// Success
		if err == nil && !resp.IsError() {
			if attempt > 0 {
				slog.Info("Request succeeded after failover",
					"operation", operation,
					"endpoint", endpoint,
					"attempt", attempt+1)
			}
			return resp, nil
		}

		// Log failure
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", operation, err)
			slog.Warn("Request failed",
				"operation", operation,
				"endpoint", endpoint,
				"error", err,
				"attempt", attempt+1,
				"total", len(shuffled))
		} else {
			lastErr = &HTTPError{Operation: operation, StatusCode: resp.StatusCode()}
			slog.Warn("Request returned error status",
				"operation", operation,
				"endpoint", endpoint,
				"status", resp.StatusCode(),
				"body", resp.String(),
				"attempt", attempt+1,
				"total", len(shuffled))
		}
	}

	return nil, fmt.Errorf("all %d Proxmox endpoints failed: %w", len(shuffled), lastErr)
}

// Ping calls the /ping endpoint to check API connectivity
func (c *Client) Ping() (*vmagent.PingResponse, error) {
	var pingResp vmagent.PingResponse

	_, err := c.executeWithFailover("ping", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetResult(&pingResp).
			Get(baseURL + "/ping")
	})

	if err != nil {
		return nil, err
	}

	return &pingResp, nil
}

// CreateUser creates a new PVE user (implements vmagent.ProxmoxClient interface)
func (c *Client) CreateUser(req *vmagent.PveUserCreation) error {
	return c.createPveUser(req)
}

// createPveUser is the internal implementation
func (c *Client) createPveUser(req *vmagent.PveUserCreation) error {
	_, err := c.executeWithFailover("createPveUser", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetBody(req).
			Post(baseURL + "/pve/pveuser")
	})

	return err
}

// GetUser retrieves a PVE user by username
func (c *Client) GetUser(username string) (*PveUser, error) {
	var user PveUser

	_, err := c.executeWithFailover("getUser", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetHeader("PVE-User", username).
			SetResult(&user).
			Get(baseURL + "/pve/pveuser")
	})

	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetUserPools retrieves the pools and resources available to a PVE user
func (c *Client) GetUserPools(username string) (*vmagent.PveUserResources, error) {
	var resources vmagent.PveUserResources

	_, err := c.executeWithFailover("getUserPools", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetHeader("PVE-User", username).
			SetResult(&resources).
			Get(baseURL + "/pve/pveuser/resources")
	})

	if err != nil {
		return nil, err
	}

	return &resources, nil
}

// GetNodes retrieves the list of available nodes
func (c *Client) GetNodes() ([]string, error) {
	var nodes []string

	_, err := c.executeWithFailover("getNodes", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetResult(&nodes).
			Get(baseURL + "/pve/nodes")
	})

	if err != nil {
		return nil, err
	}

	return nodes, nil
}

// GetNodesMetrics retrieves the metrics for all nodes
func (c *Client) GetNodesMetrics() (*NodesMetrics, error) {
	var metrics NodesMetrics

	_, err := c.executeWithFailover("getNodesMetrics", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetResult(&metrics).
			Get(baseURL + "/pve/nodes/metrics")
	})

	if err != nil {
		return nil, err
	}

	return &metrics, nil
}

// CreateVMFromTemplate creates a new virtual machine from a template
func (c *Client) CreateVMFromTemplate(username string, vmReq *vmagent.VmCreateFromTemplate) error {
	_, err := c.executeWithFailover("createVMFromTemplate", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetTimeout(c.createTimeout).
			SetHeader("PVE-User", username).
			SetBody(vmReq).
			Post(baseURL + "/pve/vms/create/from-template")
	})

	return err
}

// GetVM retrieves a specific VM by ID
func (c *Client) GetVM(username string, vmid int) (*vmagent.Vm, error) {
	return c.getVM(context.Background(), username, vmid)
}

func (c *Client) getVM(ctx context.Context, username string, vmid int) (*vmagent.Vm, error) {
	var vm vmagent.Vm

	_, err := c.executeWithFailover("getVM", func(baseURL string) (*resty.Response, error) {
		// A failed decode may have populated flags; do not carry them into failover.
		vm = vmagent.Vm{}
		return c.restyClient.R().
			SetContext(ctx).
			SetHeader("PVE-User", username).
			SetResult(&vm).
			Get(baseURL + fmt.Sprintf("/pve/vms/%d", vmid))
	})

	if err != nil {
		return nil, err
	}

	return &vm, nil
}

// PatchVM patches a virtual machine configuration
func (c *Client) PatchVM(username string, vmid int, patchReq *vmagent.VmPatch) error {
	_, err := c.executeWithFailover("patchVM", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetHeader("PVE-User", username).
			SetBody(patchReq).
			Patch(baseURL + fmt.Sprintf("/pve/vms/%d", vmid))
	})

	return err
}

// DeleteVM deletes a virtual machine
func (c *Client) DeleteVM(username string, vmid int) error {
	_, err := c.executeWithFailover("deleteVM", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetHeader("PVE-User", username).
			Delete(baseURL + fmt.Sprintf("/pve/vms/%d", vmid))
	})

	return err
}

// StartVM starts a virtual machine
func (c *Client) StartVM(username string, vmid int) error {
	_, err := c.executeWithFailover("startVM", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetHeader("PVE-User", username).
			Post(baseURL + fmt.Sprintf("/pve/vms/%d/start", vmid))
	})

	return err
}

// StopVM stops a virtual machine
func (c *Client) StopVM(username string, vmid int) error {
	_, err := c.executeWithFailover("stopVM", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetHeader("PVE-User", username).
			Post(baseURL + fmt.Sprintf("/pve/vms/%d/stop", vmid))
	})

	return err
}

// WaitForVMState waits for a VM to reach a specific state with timeout
// It checks both the status and that the VM is not locked (lock == false)
func (c *Client) WaitForVMState(username string, vmid int, expectedState string, timeout time.Duration) error {
	start := time.Now()
	pollInterval := 2 * time.Second

	for time.Since(start) < timeout {
		vm, err := c.GetVM(username, vmid)
		if err != nil {
			// If we can't get the VM, wait and retry
			time.Sleep(pollInterval)
			continue
		}

		// Check if VM is in expected state and not locked
		if vm.Status == expectedState && !vm.Lock {
			return nil
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timeout waiting for VM %d to reach state '%s' with lock=false after %v", vmid, expectedState, timeout)
}

// WaitForVMReady waits for completed provisioning, whether the VM is running
// or stopped. It returns the observed VM and never changes its power state.
func (c *Client) WaitForVMReady(ctx context.Context, username string, vmid int, timeout time.Duration) (*vmagent.Vm, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lastState := "not observed"
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("waiting for VM %d provisioning (%s): %w", vmid, lastState, err)
		}
		vm, err := c.getVM(ctx, username, vmid)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("waiting for VM %d provisioning (%s): %w", vmid, lastState, ctx.Err())
		}
		if err == nil {
			lastState = fmt.Sprintf("status=%s lock=%t qemuReady=%t", vm.Status, vm.Lock, vm.QemuReady)
			if (vm.Status == "running" || vm.Status == "stopped") && !vm.Lock && vm.QemuReady {
				return vm, nil
			}
		} else {
			var httpErr *HTTPError
			if errors.As(err, &httpErr) && httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 && httpErr.StatusCode != 404 && httpErr.StatusCode != 429 {
				return nil, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for VM %d provisioning (%s): %w", vmid, lastState, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

// OpenVNC opens VNC access for a virtual machine
func (c *Client) OpenVNC(username string, vmid int, vncReq *VncEnable) (*VncEnableResponse, error) {
	var vncResp VncEnableResponse

	_, err := c.executeWithFailover("openVNC", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetHeader("PVE-User", username).
			SetBody(vncReq).
			SetResult(&vncResp).
			Post(baseURL + fmt.Sprintf("/pve/vms/%d/vnc", vmid))
	})

	if err != nil {
		return nil, err
	}

	return &vncResp, nil
}

// GetVMFirewallRules retrieves the firewall rules for a VM
func (c *Client) GetVMFirewallRules(username string, vmid int) ([]vmagent.FirewallRule, error) {
	var rules []vmagent.FirewallRule

	_, err := c.executeWithFailover("getVMFirewallRules", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetHeader("PVE-User", username).
			SetResult(&rules).
			Get(baseURL + fmt.Sprintf("/pve/vms/%d/firewall", vmid))
	})

	if err != nil {
		return nil, err
	}

	return rules, nil
}

// AddVMFirewallRule adds a firewall rule to a VM
func (c *Client) AddVMFirewallRule(username string, vmid int, rule *vmagent.FirewallRule) error {
	// Position and digest belong to the read model, not the create request.
	request := *rule
	request.Pos = nil
	request.Digest = ""
	_, err := c.executeWithFailover("addVMFirewallRule", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetHeader("PVE-User", username).
			SetBody(&request).
			Post(baseURL + fmt.Sprintf("/pve/vms/%d/firewall", vmid))
	})

	return err
}

// DeleteVMFirewallRule deletes a firewall rule from a VM
func (c *Client) DeleteVMFirewallRule(username string, vmid int, pos int) error {
	_, err := c.executeWithFailover("deleteVMFirewallRule", func(baseURL string) (*resty.Response, error) {
		return c.restyClient.R().
			SetHeader("PVE-User", username).
			Delete(baseURL + fmt.Sprintf("/pve/vms/%d/firewall/%d", vmid, pos))
	})

	return err
}
