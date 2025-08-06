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

// NewClient creates a new Proxmox API REST client
func NewClient(cfg *config.ProxmoxAPIConfig) *Client {
	// Create a new resty client
	restyClient := resty.New()

	// Configure the client with settings from Proxmox config
	restyClient.
		SetBaseURL(cfg.APIURL).
		SetHeader("Authorization", cfg.APIToken).
		SetHeader("Content-Type", "application/json").
		SetTimeout(cfg.Timeout)

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
