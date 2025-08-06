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
