package config

import (
	"time"

	"github.com/fulcrumproject/utils/logging"
)

const (
	EnvPrefix = "PROXMOX_AGENT_"
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
