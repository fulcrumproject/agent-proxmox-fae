package vmagent

import (
	"context"
	"fmt"
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
// VMCreateProperties represents the parameters for creating a new VM (service.create)
// All fields marked as required must be provided
type VMCreateProperties struct {
	// Required fields
	CPU      int    `json:"cpu"`
	Memory   int    `json:"memory"`
	Disk     int    `json:"disk"`
	Template string `json:"template"` // Template name (validated via serviceOption)

	// Network configuration (required - from pool allocation)
	NetConfig *NetworkConfig `json:"netconfig"`

	// DNS configuration (optional)
	Nameserver   string `json:"nameserver,omitempty"`
	SearchDomain string `json:"searchdomain,omitempty"`

	// Login configuration (required: loginuser + at least one of loginpass/loginsshkey)
	LoginUser   string `json:"loginuser"`             // Username for VM access (required)
	LoginPass   string `json:"loginpass,omitempty"`   // Password for VM access
	LoginSSHKey string `json:"loginsshkey,omitempty"` // SSH public key

	// Security configuration (optional)
	FirewallRules []FirewallRule `json:"firewallrules,omitempty"` // Firewall rules to apply
}

// VMUpdateProperties represents the parameters for updating an existing VM (service.update)
// All fields are optional - only provided fields will be updated (patch semantics)
// Uses pointers to distinguish between "not provided" (nil) vs "set to zero value"
type VMUpdateProperties struct {
	// Optional - only update if provided (pointer distinguishes nil vs 0)
	CPU    *int `json:"cpu,omitempty"`
	Memory *int `json:"memory,omitempty"`
	Disk   *int `json:"disk,omitempty"`

	// Firewall rules reconciliation (optional)
	// - nil: don't touch existing rules
	// - empty array: remove all rules
	// - populated array: reconcile with existing rules (add/remove as needed)
	FirewallRules *[]FirewallRule `json:"firewallrules,omitempty"`
}

// VMProperties is an alias for VMCreateProperties for backward compatibility
// Deprecated: Use VMCreateProperties or VMUpdateProperties explicitly
type VMProperties = VMCreateProperties

// NetworkConfig represents network pool allocation
type NetworkConfig struct {
	IPAddress string `json:"ipaddress"` // Static IP from pool
	Netmask   int    `json:"netmask"`   // CIDR notation (e.g., 24)
	Gateway   string `json:"gateway"`   // Gateway IP from pool
}

// FirewallRule represents a VM firewall rule
// Pos and Digest are read-only fields from the API (omitted when creating/updating rules)
type FirewallRule struct {
	Type    string `json:"type"`              // in or out
	Proto   string `json:"proto"`             // tcp, udp, icmp
	Action  string `json:"action"`            // ACCEPT, DROP, REJECT
	Source  string `json:"source,omitempty"`  // Source IP/CIDR
	Sport   string `json:"sport,omitempty"`   // Source port(s)
	Dest    string `json:"dest,omitempty"`    // Destination IP/CIDR
	Dport   string `json:"dport,omitempty"`   // Destination port(s)
	Comment string `json:"comment,omitempty"` // Rule description
	Pos     *int   `json:"pos,omitempty"`     // Position (read-only from API, omitted when creating)
	Digest  string `json:"digest,omitempty"`  // Digest (read-only from API, omitted when creating)
}

// VMResources represents the resources in a job response
type VMResources struct {
	VMID int `json:"vmid"`
}

// MakeUserIDs creates prefixed user and pool IDs from a consumer ID
// to ensure they start with a letter (required by Proxmox)
func MakeUserIDs(consumerID string) (userID, poolID string) {
	return fmt.Sprintf("user-%s", consumerID), fmt.Sprintf("pool-%s", consumerID)
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
	stdAgent.OnJob("create", agent.JobHandlerWrapper(vmAgent.CreateVMHandler()))
	stdAgent.OnJob("update", agent.JobHandlerWrapper(vmAgent.UpdateVMHandler()))
	stdAgent.OnJob("start", agent.JobHandlerWrapper(vmAgent.StartVMHandler()))
	stdAgent.OnJob("stop", agent.JobHandlerWrapper(vmAgent.StopVMHandler()))
	stdAgent.OnJob("delete", agent.JobHandlerWrapper(vmAgent.DeleteVMHandler()))

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

func (v *VMAgent) GetJobStats() (int, int, int) {
	return v.stdAgent.GetJobStats()
}
