package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fulcrumproject.org/agent-proxmox-wrapped/internal/config"
	"fulcrumproject.org/agent-proxmox-wrapped/internal/handlers"
	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
	"github.com/fulcrumproject/agent-lib-go/pkg/fulcrumcli"
	"github.com/fulcrumproject/agent-lib-go/pkg/stdagent"
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

	slog.Info("Starting test agent")

	client := fulcrumcli.NewHTTPClient[handlers.ServiceProperties](cfg.FulcrumAPIURL, cfg.AgentToken)

	// Create stdagent with configuration options
	testAgent := newAgent(client, cfg.JobPollInterval, cfg.MetricReportInterval)

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start the agent
	if err := testAgent.Run(ctx); err != nil {
		slog.Error("Failed to start agent", "error", err)
		os.Exit(1)
	}

	slog.Info("Test agent started successfully", "agent_id", testAgent.GetAgentID())

	// Wait for termination signal
	<-sigCh
	slog.Info("Received shutdown signal")

	// Create a context with timeout for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Shut down the agent
	if err := testAgent.Shutdown(shutdownCtx); err != nil {
		slog.Error("Error during shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("Agent uptime", "uptime", testAgent.GetUptime().Round(time.Second))
}

func newAgent(
	client *fulcrumcli.HTTPClient[handlers.ServiceProperties],
	jobPollInterval time.Duration,
	metricReportInterval time.Duration,
) *stdagent.Agent[handlers.ServiceProperties, handlers.JobResources] {
	testAgent, err := stdagent.New(client,
		stdagent.WithHeartbeatInterval[handlers.ServiceProperties, handlers.JobResources](60*time.Second),
		stdagent.WithJobPollInterval[handlers.ServiceProperties, handlers.JobResources](jobPollInterval),
		stdagent.WithMetricsReportInterval[handlers.ServiceProperties, handlers.JobResources](metricReportInterval),
	)
	if err != nil {
		slog.Error("Failed to create agent", "error", err)
		os.Exit(1)
	}

	// Register heartbeat handler (with VM resource updating)
	testAgent.OnHeartbeat(handlers.HeartbeatHandler())

	// Register job handlers
	testAgent.OnJob(agent.JobActionServiceCreate, handlers.CreateVMHandler())
	testAgent.OnJob(agent.JobActionServiceColdUpdate, handlers.UpdateVMHandler())
	testAgent.OnJob(agent.JobActionServiceHotUpdate, handlers.UpdateVMHandler())
	testAgent.OnJob(agent.JobActionServiceStart, handlers.StartVMHandler())
	testAgent.OnJob(agent.JobActionServiceStop, handlers.StopVMHandler())
	testAgent.OnJob(agent.JobActionServiceDelete, handlers.DeleteVMHandler())

	// Register metrics reporter
	testAgent.OnMetricsReport(handlers.MetricsReporter())
	return testAgent
}
