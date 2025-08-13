package vmagent

import (
	"context"
	"fmt"
	"strconv"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
)

// MetricsReporter collects and returns metrics from VMs
func (v *VMAgent) MetricsReporter() agent.MetricsReporter {
	return func(ctx context.Context, service *agent.Service) ([]agent.MetricEntry, error) {
		var metrics []agent.MetricEntry

		if service == nil || service.ExternalID == nil {
			return nil, fmt.Errorf("service is nil or externalID is nil")
		}

		username, vmid, err := extractUserAndVMID(service)
		if err != nil {
			return nil, fmt.Errorf("failed to extract user and VMID from service: %w", err)
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
				TypeName:   "vm.cpu.usage",
				Value:      cpuUsageRatio,
				ResourceID: strconv.Itoa(vmid),
				ExternalID: strconv.Itoa(vmid),
			},

			agent.MetricEntry{
				TypeName:   "vm.memory.usage",
				Value:      memUsageRatio,
				ResourceID: strconv.Itoa(vmid),
				ExternalID: strconv.Itoa(vmid),
			},
		)

		return metrics, nil
	}
}
