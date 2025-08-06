package proxmoxapi

import (
	"crypto/tls"
	"fmt"

	"resty.dev/v3"

	"fulcrumproject.org/agent-proxmox-wrapped/internal/config"
)

// Client represents a REST client for the Proxmox API
type Client struct {
	restyClient *resty.Client
}

// PingResponse represents the response from the ping endpoint
type PingResponse struct {
	Fae     bool `json:"fae"`
	Proxmox bool `json:"proxmox"`
}

// PveUser represents a PVE user response
type PveUser struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	HasAPI  bool   `json:"hasApi"`
}

// PveUserCreation represents the request for creating a new PVE user
type PveUserCreation struct {
	UserID    string `json:"userid"`
	FirstName string `json:"firstname"`
	LastName  string `json:"lastname"`
	PoolID    string `json:"poolid"`
}

// PveUserResources represents the resources available to a PVE user
type PveUserResources struct {
	NextID int               `json:"nextid"`
	Nodes  []interface{}     `json:"nodes"`
	Pools  []interface{}     `json:"pools"`
	IDE2   []interface{}     `json:"ide2"`
	OSType map[string]string `json:"ostype"`
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

// Vm represents a virtual machine
type Vm struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Node   string `json:"node"`
	Pool   string `json:"pool"`
}

// VmCreation represents the request for creating a new VM
type VmCreation struct {
	Node    string `json:"node"`
	VMID    int    `json:"vmid"`
	Name    string `json:"name"`
	Pool    string `json:"pool"`
	Sockets int    `json:"sockets"`
	Cores   int    `json:"cores"`
	Memory  int    `json:"memory"`
	SCSI0   string `json:"scsi0"`
	IDE2    string `json:"ide2"`
	OSType  string `json:"ostype"`
}

// VmClonation represents the request for cloning a VM
type VmClonation struct {
	NewID int    `json:"newid"`
	Name  string `json:"name"`
	Pool  string `json:"pool"`
}

// VmMigration represents the request for migrating a VM
type VmMigration struct {
	Target string `json:"target"`
}

// VncEnable represents the request for enabling VNC
type VncEnable struct {
	SourceIP string `json:"source_ip"`
}

// VncEnableResponse represents the response for VNC enabling
type VncEnableResponse struct {
	Host string `json:"host"`
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
		SetTimeout(cfg.Timeout).
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
func (c *Client) Ping() (*PingResponse, error) {
	var pingResp PingResponse

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

// CreateUser creates a new PVE user
func (c *Client) CreateUser(req *PveUserCreation) error {
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
func (c *Client) GetUserPools(username string) (*PveUserResources, error) {
	var resources PveUserResources

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
func (c *Client) GetUserVMs(username string) ([]Vm, error) {
	var vms []Vm

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
func (c *Client) CreateVM(username string, vmReq *VmCreation) error {
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
func (c *Client) GetVM(username string, vmid int) (*Vm, error) {
	var vm Vm

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
func (c *Client) CloneVM(username string, vmid int, cloneReq *VmClonation) error {
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
func (c *Client) MigrateVM(username string, vmid int, migrateReq *VmMigration) error {
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
