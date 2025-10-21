package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fulcrumproject/agent-lib-go/pkg/fulcrumcli"
	"github.com/fulcrumproject/agent-lib-go/pkg/stdagent"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/proxmoxapi"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/vmagent"
	"github.com/fulcrumproject/utils/confbuilder"
	"github.com/fulcrumproject/utils/logging"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := confbuilder.New(config.DefaultConfig()).
		EnvPrefix(config.EnvPrefix).
		EnvFiles(".env").
		File(configPath).
		Build()
	if err != nil {
		slog.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}

	// Setup structured logger
	logger := logging.NewLogger(&cfg.LogConfig)
	slog.SetDefault(logger)

	slog.Info("Starting proxmox agent")

	// Create stdagent with configuration options
	fulcrumClient := fulcrumcli.NewHTTPClient(cfg.FulcrumAPIURL, cfg.AgentToken)
	proxmoxClientFactory := func(config *config.ProxmoxAPIConfig) vmagent.ProxmoxClient {
		return proxmoxapi.NewClient(config)
	}
	vmAgent := vmagent.NewVMAgent(
		proxmoxClientFactory,
		fulcrumClient,
		stdagent.WithHealthInterval(60*time.Second),
		stdagent.WithJobPollInterval(cfg.JobPollInterval),
		stdagent.WithMetricsReportInterval(cfg.MetricReportInterval),
	)

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start the agent
	if err := vmAgent.Run(ctx); err != nil {
		slog.Error("Failed to start agent", "error", err)
		os.Exit(1)
	}

	slog.Info("Proxmox agent started successfully", "agent_id", vmAgent.GetAgentID())

	// Wait for termination signal
	<-sigCh
	slog.Info("Received shutdown signal")

	// Create a context with timeout for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Shut down the agent
	if err := vmAgent.Shutdown(shutdownCtx); err != nil {
		slog.Error("Error during shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("Agent uptime", "uptime", vmAgent.GetUptime().Round(time.Second))
}
