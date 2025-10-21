package vmagent

import (
	"context"
	"log"
	"log/slog"
	"time"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
	"github.com/fulcrumproject/agent-lib-go/pkg/stdagent"
	"github.com/fulcrumproject/agent-proxmox-fae/internal/config"
)

type RemoteConfig struct {
	ProxmoxAPI *config.ProxmoxAPIConfig `json:"proxmoxAPI"`
}

// VMProperties represents the properties of a VM service
type VMProperties struct {
	CPU    int    `json:"cpu"`
	Memory int    `json:"memory"`
	Disk   int    `json:"disk"`
	Image  string `json:"image"`
}

// VMResources represents the resources in a job response
type VMResources struct {
	VMID int `json:"vmid"`
}

type VMAgent struct {
	stdAgent             *stdagent.Agent
	proxmoxClient        ProxmoxClient
	proxmoxClientFactory func(config *config.ProxmoxAPIConfig) ProxmoxClient
}

func NewVMAgent(
	proxmoxClientFactory func(config *config.ProxmoxAPIConfig) ProxmoxClient,
	fulcrumClient stdagent.FulcrumClient,
	opts ...stdagent.AgentOption,
) *VMAgent {
	stdAgent, err := stdagent.New(fulcrumClient, opts...)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	vmAgent := &VMAgent{
		stdAgent:             stdAgent,
		proxmoxClientFactory: proxmoxClientFactory,
	}

	// Register connection handler
	stdAgent.OnConnect(agent.ConnectHandlerWrapper(vmAgent.ConnectHandler))

	// Register health handler (with VM resource updating)
	stdAgent.OnHealth(vmAgent.HealthHandler())

	// Register job handlers
	stdAgent.OnJob(agent.JobActionServiceCreate, agent.JobHandlerWrapper(vmAgent.CreateVMHandler()))
	stdAgent.OnJob(agent.JobActionServiceUpdate, agent.JobHandlerWrapper(vmAgent.UpdateVMHandler()))
	stdAgent.OnJob(agent.JobActionServiceStart, agent.JobHandlerWrapper(vmAgent.StartVMHandler()))
	stdAgent.OnJob(agent.JobActionServiceStop, agent.JobHandlerWrapper(vmAgent.StopVMHandler()))
	stdAgent.OnJob(agent.JobActionServiceDelete, agent.JobHandlerWrapper(vmAgent.DeleteVMHandler()))

	// Register metrics reporter
	stdAgent.OnMetrics(vmAgent.MetricsReporter())

	return vmAgent
}

func (v *VMAgent) ConnectHandler(ctx context.Context, info *agent.AgentInfo[RemoteConfig]) error {
	v.proxmoxClient = v.proxmoxClientFactory(info.Config.ProxmoxAPI)

	// Ping proxmox API
	ping, err := v.proxmoxClient.Ping()
	if err != nil {
		return err
	}
	slog.Info("Proxmox API ping", "ping", ping)

	return nil
}

func (v *VMAgent) GetAgentID() string {
	return v.stdAgent.GetAgentID()
}

func (v *VMAgent) Run(ctx context.Context) error {
	return v.stdAgent.Run(ctx)
}

func (v *VMAgent) Shutdown(ctx context.Context) error {
	return v.stdAgent.Shutdown(ctx)
}

func (v *VMAgent) GetUptime() time.Duration {
	return v.stdAgent.GetUptime()
}

func (v *VMAgent) GetJobStats() (int, int, int, int) {
	return v.stdAgent.GetJobStats()
}
