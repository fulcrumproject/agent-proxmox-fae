package config

import (
	"time"

	"github.com/fulcrumproject/utils/logging"
)

const (
	EnvPrefix            = "PROXMOX_AGENT_"
	DefaultCreateTimeout = 10 * time.Minute
	DefaultReadyTimeout  = 30 * time.Minute
)

// Config holds the configuration for the test agent
type Config struct {
	// Fulcrum Core API connection
	AgentToken    string `json:"agentToken" validate:"required" env:"FULCRUM_API_TOKEN"` // Authentication token for the agent
	FulcrumAPIURL string `json:"fulcrumApiUrl" validate:"required" env:"FULCRUM_API_URL"`

	// Simulation parameters
	JobPollInterval      time.Duration `json:"jobPollInterval" env:"JOB_POLL_INTERVAL"`           // How often to poll for jobs
	MetricReportInterval time.Duration `json:"metricReportInterval" env:"METRIC_REPORT_INTERVAL"` // How often to report metrics

	// Logging
	LogConfig logging.Conf `json:"log" validate:"required"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		AgentToken:           "", // Must be provided
		FulcrumAPIURL:        "http://localhost:3000",
		JobPollInterval:      5 * time.Second,
		MetricReportInterval: 30 * time.Second,
	}
}

// ProxmoxAPIConfig holds the configuration for Proxmox API connection
type ProxmoxAPIConfig struct {
	APIToken        string   `json:"apiToken" validate:"required" env:"TOKEN"`            // Authentication token for Proxmox API
	APIURLs         []string `json:"apiUrls" validate:"required" env:"URLS"`              // Base URLs for Proxmox API (supports multiple endpoints)
	InsecureSkipTLS bool     `json:"insecureSkipTls" env:"INSECURE_SKIP_TLS"`             // Skip TLS certificate verification
	Timeout         int      `json:"timeout" env:"TIMEOUT"`                               // HTTP request timeout in seconds
	Debug           bool     `json:"debug" env:"DEBUG"`                                   // Enable debug logging for HTTP requests
	CreateTimeout   int      `json:"createTimeout" env:"CREATE_TIMEOUT" validate:"gte=0"` // Create request timeout in seconds (0 uses default)
	ReadyTimeout    int      `json:"readyTimeout" env:"READY_TIMEOUT" validate:"gte=0"`   // Provisioning wait in seconds (0 uses default)
}

// DefaultProxmoxAPIConfig returns the default configuration for Proxmox API
func DefaultProxmoxAPIConfig() *ProxmoxAPIConfig {
	return &ProxmoxAPIConfig{
		APIToken:        "",         // Must be provided
		APIURLs:         []string{}, // Must be provided
		InsecureSkipTLS: false,
		Timeout:         30,
		Debug:           false,
		CreateTimeout:   int(DefaultCreateTimeout / time.Second),
		ReadyTimeout:    int(DefaultReadyTimeout / time.Second),
	}
}

// CreationTimeout also supplies the default for remote configs created before this field existed.
func (c *ProxmoxAPIConfig) CreationTimeout() time.Duration {
	if c.CreateTimeout > 0 {
		return time.Duration(c.CreateTimeout) * time.Second
	}
	return DefaultCreateTimeout
}

func (c *ProxmoxAPIConfig) ReadinessTimeout() time.Duration {
	if c.ReadyTimeout > 0 {
		return time.Duration(c.ReadyTimeout) * time.Second
	}
	return DefaultReadyTimeout
}
