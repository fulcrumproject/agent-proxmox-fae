package proxmoxapi

import (
	"crypto/tls"
	"fmt"
	"time"

	"resty.dev/v3"

	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/vmagent"
)

// Client represents a REST client for the Proxmox API
type Client struct {
	restyClient *resty.Client
}

// PveUser represents a PVE user response
type PveUser struct {
	Enabled bool `json:"enabled"`
	HasAPI  bool `json:"hasApi"`
	Status  bool `json:"status"`
}

// PveUserCreation represents the request for creating a new PVE user
type PveUserCreation struct {
	UserID        string `json:"userid"`
	PoolID        string `json:"poolid"`
	ResetIfExists bool   `json:"reset_if_exists"`
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
	NextID int `json:"nextid"`
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

// NewClient creates a new Proxmox API REST client
func NewClient(cfg *config.ProxmoxAPIConfig) *Client {
	// Create a new resty client
	restyClient := resty.New()

	// Configure the client with settings from Proxmox config
	restyClient.
		SetBaseURL(cfg.APIURL).
		SetHeader("Authorization", cfg.APIToken).
		SetHeader("Content-Type", "application/json").
		SetTimeout(time.Duration(cfg.Timeout) * time.Second).
		SetDebug(cfg.Debug)

	// Configure TLS settings
	restyClient.SetTLSClientConfig(&tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipTLS,
	})

	return &Client{
		restyClient: restyClient,
	}
}

// Ping calls the /ping endpoint to check API connectivity
func (c *Client) Ping() (*vmagent.PingResponse, error) {
	var pingResp vmagent.PingResponse

	resp, err := c.restyClient.R().
		SetResult(&pingResp).
		Get("/ping")

	if err != nil {
		return nil, fmt.Errorf("failed to ping Proxmox API: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("ping request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return &pingResp, nil
}

// CreateUser creates a new PVE user (implements vmagent.ProxmoxClient interface)
func (c *Client) CreateUser(req *vmagent.PveUserCreation) error {
	// Convert vmagent type to proxmoxapi type
	proxmoxReq := &PveUserCreation{
		UserID:        req.UserID,
		PoolID:        req.PoolID,
		ResetIfExists: req.ResetIfExists,
	}

	return c.createPveUser(proxmoxReq)
}

// createPveUser is the internal implementation using proxmoxapi types
func (c *Client) createPveUser(req *PveUserCreation) error {
	resp, err := c.restyClient.R().
		SetBody(req).
		Post("/fae/v1/pveuser")

	if err != nil {
		return fmt.Errorf("failed to create PVE user: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("create user request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// GetUser retrieves a PVE user by username
func (c *Client) GetUser(username string) (*PveUser, error) {
	var user PveUser

	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		SetResult(&user).
		Get("/fae/v1/pveuser")

	if err != nil {
		return nil, fmt.Errorf("failed to get PVE user: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get user request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return &user, nil
}

// GetUserPools retrieves the pools and resources available to a PVE user
func (c *Client) GetUserPools(username string) (*vmagent.PveUserResources, error) {
	var resources vmagent.PveUserResources

	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		SetResult(&resources).
		Get("/fae/v1/pveuser/resources")

	if err != nil {
		return nil, fmt.Errorf("failed to get PVE user pools: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get user pools request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return &resources, nil
}

// AddPoolToUser adds a pool to a PVE user
func (c *Client) AddPoolToUser(username string, poolReq *PveUserAddPool) error {
	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		SetBody(poolReq).
		Post("/fae/v1/pveuser/pool")

	if err != nil {
		return fmt.Errorf("failed to add pool to user: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("add pool to user request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// GetNodes retrieves the list of available nodes
func (c *Client) GetNodes() ([]string, error) {
	var nodes []string

	resp, err := c.restyClient.R().
		SetResult(&nodes).
		Get("/fae/v1/nodes")

	if err != nil {
		return nil, fmt.Errorf("failed to get nodes: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get nodes request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nodes, nil
}

// GetNodesMetrics retrieves the metrics for all nodes
func (c *Client) GetNodesMetrics() (*NodesMetrics, error) {
	var metrics NodesMetrics

	resp, err := c.restyClient.R().
		SetResult(&metrics).
		Get("/fae/v1/nodes/metrics")

	if err != nil {
		return nil, fmt.Errorf("failed to get nodes metrics: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get nodes metrics request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return &metrics, nil
}

// GetVMNextID retrieves the next available VM ID
func (c *Client) GetVMNextID() (*NextID, error) {
	var nextID NextID

	resp, err := c.restyClient.R().
		SetResult(&nextID).
		Get("/fae/v1/vms/nextid")

	if err != nil {
		return nil, fmt.Errorf("failed to get VM next ID: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get VM next ID request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return &nextID, nil
}

// GetUserVMs retrieves VMs for a specific user
func (c *Client) GetUserVMs(username string) ([]vmagent.Vm, error) {
	var vms []vmagent.Vm

	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		SetResult(&vms).
		Get("/fae/v1/vms")

	if err != nil {
		return nil, fmt.Errorf("failed to get user VMs: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get user VMs request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return vms, nil
}

// CreateVM creates a new virtual machine
func (c *Client) CreateVM(username string, vmReq *vmagent.VmCreation) error {
	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		SetBody(vmReq).
		Post("/fae/v1/vms")

	if err != nil {
		return fmt.Errorf("failed to create VM: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("create VM request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// GetVM retrieves a specific VM by ID
func (c *Client) GetVM(username string, vmid int) (*vmagent.Vm, error) {
	var vm vmagent.Vm

	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		SetResult(&vm).
		Get(fmt.Sprintf("/fae/v1/vms/%d", vmid))

	if err != nil {
		return nil, fmt.Errorf("failed to get VM: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get VM request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return &vm, nil
}

// PatchVM patches a virtual machine configuration
func (c *Client) PatchVM(username string, vmid int, patchReq *vmagent.VmPatch) error {
	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		SetBody(patchReq).
		Patch(fmt.Sprintf("/fae/v1/vms/%d", vmid))

	if err != nil {
		return fmt.Errorf("failed to patch VM: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("patch VM request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// DeleteVM deletes a virtual machine
func (c *Client) DeleteVM(username string, vmid int) error {
	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		Delete(fmt.Sprintf("/fae/v1/vms/%d", vmid))

	if err != nil {
		return fmt.Errorf("failed to delete VM: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("delete VM request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// StartVM starts a virtual machine
func (c *Client) StartVM(username string, vmid int) error {
	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		Get(fmt.Sprintf("/fae/v1/vms/%d/start", vmid))

	if err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("start VM request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// StopVM stops a virtual machine
func (c *Client) StopVM(username string, vmid int) error {
	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		Get(fmt.Sprintf("/fae/v1/vms/%d/stop", vmid))

	if err != nil {
		return fmt.Errorf("failed to stop VM: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("stop VM request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// CloneVM clones a virtual machine
func (c *Client) CloneVM(username string, vmid int, cloneReq *VmClone) error {
	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		SetBody(cloneReq).
		Post(fmt.Sprintf("/fae/v1/vms/%d/clone", vmid))

	if err != nil {
		return fmt.Errorf("failed to clone VM: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("clone VM request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// MigrateVM migrates a virtual machine to another node
func (c *Client) MigrateVM(username string, vmid int, migrateReq *VmMigrate) error {
	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		SetBody(migrateReq).
		Post(fmt.Sprintf("/fae/v1/vms/%d/migrate", vmid))

	if err != nil {
		return fmt.Errorf("failed to migrate VM: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("migrate VM request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// OpenVNC opens VNC access for a virtual machine
func (c *Client) OpenVNC(username string, vmid int, vncReq *VncEnable) (*VncEnableResponse, error) {
	var vncResp VncEnableResponse

	resp, err := c.restyClient.R().
		SetHeader("PVE-User", username).
		SetBody(vncReq).
		SetResult(&vncResp).
		Post(fmt.Sprintf("/fae/v1/vms/%d/vnc", vmid))

	if err != nil {
		return nil, fmt.Errorf("failed to open VNC: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("open VNC request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return &vncResp, nil
}

// GetVMStatus retrieves detailed VM status and metrics using the Proxmox API wrapper
func (c *Client) GetVMStatus(pveAuth string, node string, vmid int) (*VmStatus, error) {
	var status VmStatus

	resp, err := c.restyClient.R().
		SetHeader("PVE-Authorization", pveAuth).
		SetResult(&status).
		Get(fmt.Sprintf("/api2/json/nodes/%s/qemu/%d/status/current", node, vmid))

	if err != nil {
		return nil, fmt.Errorf("failed to get VM status: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get VM status request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return &status, nil
}

// GenerateSecrets generates AES and JWT secrets
func (c *Client) GenerateSecrets() (*FaeSecrets, error) {
	var secrets FaeSecrets

	resp, err := c.restyClient.R().
		SetResult(&secrets).
		Get("/root/misc/generate-secrets")

	if err != nil {
		return nil, fmt.Errorf("failed to generate secrets: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("generate secrets request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return &secrets, nil
}

// GenerateFaeAuthToken generates a new FAE auth token
func (c *Client) GenerateFaeAuthToken(tokenReq *FaeAuthTokenRequest) (*FaeAuthToken, error) {
	var token FaeAuthToken

	resp, err := c.restyClient.R().
		SetBody(tokenReq).
		SetResult(&token).
		Post("/root/fae-auth-token/generate")

	if err != nil {
		return nil, fmt.Errorf("failed to generate FAE auth token: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("generate FAE auth token request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return &token, nil
}

// GetValidFaeAuthTokenApps gets the list of valid FAE auth token apps
func (c *Client) GetValidFaeAuthTokenApps() ([]string, error) {
	var apps []string

	resp, err := c.restyClient.R().
		SetResult(&apps).
		Get("/root/fae-auth-token/apps")

	if err != nil {
		return nil, fmt.Errorf("failed to get valid FAE auth token apps: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("get valid FAE auth token apps request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return apps, nil
}

// AddFaeAuthTokenApp adds a new FAE auth token app
func (c *Client) AddFaeAuthTokenApp(app string) error {
	resp, err := c.restyClient.R().
		Post(fmt.Sprintf("/root/fae-auth-token/apps/%s", app))

	if err != nil {
		return fmt.Errorf("failed to add FAE auth token app: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("add FAE auth token app request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// RemoveFaeAuthTokenApp removes a FAE auth token app
func (c *Client) RemoveFaeAuthTokenApp(app string) error {
	resp, err := c.restyClient.R().
		Delete(fmt.Sprintf("/root/fae-auth-token/apps/%s", app))

	if err != nil {
		return fmt.Errorf("failed to remove FAE auth token app: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("remove FAE auth token app request failed with status %d: %s", resp.StatusCode(), resp.String())
	}

	return nil
}
