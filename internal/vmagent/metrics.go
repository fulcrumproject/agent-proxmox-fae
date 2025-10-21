package vmagent

import (
	"context"
	"fmt"
	"strconv"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
)

// MetricsReporter returns a raw metrics reporter using the new generified wrapper
func (v *VMAgent) MetricsReporter() agent.RawMetricsReporter {
	return agent.MetricsReporterWrapper(v.typedMetricsReporter())
}

// typedMetricsReporter returns the typed metrics reporter that will be wrapped
func (v *VMAgent) typedMetricsReporter() agent.MetricsReporter[VMProperties, VMResources] {
	return func(ctx context.Context, service *agent.Service[VMProperties, VMResources]) ([]agent.MetricEntry, error) {
		var metrics []agent.MetricEntry

		if service == nil || service.AgentInstanceID == nil {
			return nil, fmt.Errorf("service is nil or agentInstanceID is nil")
		}

		if service.AgentInstanceData == nil {
			return nil, fmt.Errorf("job service agent instance data are nil")
		}

		username := service.ConsumerID
		vmid := service.AgentInstanceData.VMID

		// Validate VMID is provided
		if vmid == 0 {
			return nil, fmt.Errorf("vmid is missing or zero")
		}

		vm, err := v.proxmoxClient.GetVM(username, vmid)
		if err != nil {
			return nil, fmt.Errorf("failed to get VM: %w", err)
		}

		// Calculate CPU usage ratio (current/max)
		var cpuUsageRatio float64
		if vm.MaxCPU > 0 {
			cpuUsageRatio = vm.CPU / vm.MaxCPU
		}

		// Calculate memory usage ratio (current/max)
		var memUsageRatio float64
		if vm.MaxMem > 0 {
			memUsageRatio = float64(vm.Mem) / float64(vm.MaxMem)
		}

	metrics = append(metrics,
		agent.MetricEntry{
			TypeName:        "vm.cpu.usage",
			Value:           cpuUsageRatio,
			ResourceID:      strconv.Itoa(vmid),
			AgentInstanceID: *service.AgentInstanceID,
		},

		agent.MetricEntry{
			TypeName:        "vm.memory.usage",
			Value:           memUsageRatio,
			ResourceID:      strconv.Itoa(vmid),
			AgentInstanceID: *service.AgentInstanceID,
		},
	)

		return metrics, nil
	}
}
