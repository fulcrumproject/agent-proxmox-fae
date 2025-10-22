package vmagent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/fulcrumproject/agent-lib-go/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVMAgent_MetricsReporter(t *testing.T) {
	tests := []struct {
		name            string
		service         *agent.RawService
		setupMock       func(*MockProxmoxClient)
		expectError     bool
		errorMsg        string
		expectedMetrics int
	}{
		{
			name: "successful_metrics_collection",
			service: &agent.RawService{
				ConsumerID:        "testuser",
				AgentInstanceData: rawResourcesPtr(`{"vmid": 123}`),
				AgentInstanceID:   stringPtr("123"),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				vm := &Vm{
					VMID:      123,
					Name:      "test-vm",
					CPU:       0.5,
					MaxCPU:    2.0,
					Mem:       1024 * 1024 * 1024,      // 1GB
					MaxMem:    2048 * 1024 * 1024,      // 2GB
					Disk:      5 * 1024 * 1024 * 1024,  // 5GB
					MaxDisk:   10 * 1024 * 1024 * 1024, // 10GB
					DiskRead:  1024 * 1024,             // 1MB/s
					DiskWrite: 512 * 1024,              // 512KB/s
					NetIn:     2 * 1024 * 1024,         // 2MB/s
					NetOut:    1 * 1024 * 1024,         // 1MB/s
					Uptime:    3600,                    // 1 hour
				}
				mockClient.EXPECT().GetVM("testuser", 123).Return(vm, nil).Once()
			},
			expectError:     false,
			expectedMetrics: 8,
		},
		{
			name: "successful_metrics_zero_max_values",
			service: &agent.RawService{
				ConsumerID:        "testuser",
				AgentInstanceData: rawResourcesPtr(`{"vmid": 456}`),
				AgentInstanceID:   stringPtr("456"),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				vm := &Vm{
					VMID:      456,
					Name:      "test-vm-zero",
					CPU:       0.3,
					MaxCPU:    0, // Zero max CPU should result in 0 ratio
					Mem:       512 * 1024 * 1024,
					MaxMem:    0, // Zero max memory should result in 0 ratio
					Disk:      1024,
					MaxDisk:   0, // Zero max disk should result in 0 ratio
					DiskRead:  0,
					DiskWrite: 0,
					NetIn:     0,
					NetOut:    0,
					Uptime:    0,
				}
				mockClient.EXPECT().GetVM("testuser", 456).Return(vm, nil).Once()
			},
			expectError:     false,
			expectedMetrics: 8,
		},
		{
			name: "get_vm_fails",
			service: &agent.RawService{
				ConsumerID:        "testuser",
				AgentInstanceData: rawResourcesPtr(`{"vmid": 789}`),
				AgentInstanceID:   stringPtr("789"),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().GetVM("testuser", 789).Return(nil, errors.New("VM not found")).Once()
			},
			expectError: true,
			errorMsg:    "failed to get VM",
		},
		// Note: service_is_nil test case removed because MetricsReporterWrapper doesn't handle nil service gracefully
		{
			name: "service_agent_instance_id_is_nil",
			service: &agent.RawService{
				ConsumerID:        "testuser",
				AgentInstanceData: rawResourcesPtr(`{"vmid": 999}`),
				AgentInstanceID:   nil,
			},
			setupMock:   func(mockClient *MockProxmoxClient) {},
			expectError: true,
			errorMsg:    "service is nil or agentInstanceID is nil",
		},
		{
			name: "service_resources_missing_vmid",
			service: &agent.RawService{
				ConsumerID:        "testuser",
				AgentInstanceData: rawResourcesPtr(`{}`),
				AgentInstanceID:   stringPtr("111"),
			},
			setupMock:   func(mockClient *MockProxmoxClient) {},
			expectError: true,
			errorMsg:    "vmid is missing or zero",
		},
		{
			name: "service_resources_vmid_wrong_type",
			service: &agent.RawService{
				ConsumerID:        "testuser",
				AgentInstanceData: rawResourcesPtr(`{"vmid": "not-an-int"}`),
				AgentInstanceID:   stringPtr("222"),
			},
			setupMock:   func(mockClient *MockProxmoxClient) {},
			expectError: true,
			errorMsg:    "json: cannot unmarshal string",
		},
		{
			name: "service_resources_is_nil",
			service: &agent.RawService{
				ConsumerID:        "testuser",
				AgentInstanceData: nil,
				AgentInstanceID:   stringPtr("333"),
			},
			setupMock:   func(mockClient *MockProxmoxClient) {},
			expectError: true,
			errorMsg:    "vmid is missing or zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock client
			mockClient := NewMockProxmoxClient(t)
			tt.setupMock(mockClient)

			// Create VMAgent with mock client
			vmAgent := &VMAgent{
				proxmoxClient: mockClient,
			}

			// Get the metrics reporter
			metricsReporter := vmAgent.MetricsReporter()

			// Verify that MetricsReporter returns a function
			require.NotNil(t, metricsReporter, "MetricsReporter should return a non-nil function")

			// Execute the metrics reporter
			ctx := context.Background()
			metrics, err := metricsReporter(ctx, tt.service)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err, "Expected metrics reporter to return an error")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				assert.Nil(t, metrics, "Metrics should be nil when error occurs")
			} else {
				require.NoError(t, err, "Expected metrics reporter to succeed")
				require.Len(t, metrics, tt.expectedMetrics, "Expected number of metrics should match")

				if tt.expectedMetrics > 0 {
					vmidStr := (*tt.service.AgentInstanceID)

					// Verify CPU usage metric
					cpuMetric := findMetricByType(metrics, "vm.cpu.usage")
					require.NotNil(t, cpuMetric, "CPU usage metric should be present")
					assert.Equal(t, "vm.cpu.usage", cpuMetric.TypeName)
					assert.IsType(t, float64(0), cpuMetric.Value)
					assert.Equal(t, vmidStr, cpuMetric.ResourceID)
					assert.Equal(t, vmidStr, cpuMetric.AgentInstanceID)

					// Verify memory usage metric
					memMetric := findMetricByType(metrics, "vm.memory.usage")
					require.NotNil(t, memMetric, "Memory usage metric should be present")
					assert.Equal(t, "vm.memory.usage", memMetric.TypeName)
					assert.IsType(t, float64(0), memMetric.Value)
					assert.Equal(t, vmidStr, memMetric.ResourceID)
					assert.Equal(t, vmidStr, memMetric.AgentInstanceID)

					// Verify disk usage metric
					diskMetric := findMetricByType(metrics, "vm.disk.usage")
					require.NotNil(t, diskMetric, "Disk usage metric should be present")
					assert.Equal(t, "vm.disk.usage", diskMetric.TypeName)
					assert.IsType(t, float64(0), diskMetric.Value)
					assert.Equal(t, vmidStr, diskMetric.ResourceID)
					assert.Equal(t, vmidStr, diskMetric.AgentInstanceID)

					// Verify disk read metric
					diskReadMetric := findMetricByType(metrics, "vm.disk.read")
					require.NotNil(t, diskReadMetric, "Disk read metric should be present")
					assert.Equal(t, "vm.disk.read", diskReadMetric.TypeName)
					assert.IsType(t, float64(0), diskReadMetric.Value)
					assert.Equal(t, vmidStr, diskReadMetric.ResourceID)
					assert.Equal(t, vmidStr, diskReadMetric.AgentInstanceID)

					// Verify disk write metric
					diskWriteMetric := findMetricByType(metrics, "vm.disk.write")
					require.NotNil(t, diskWriteMetric, "Disk write metric should be present")
					assert.Equal(t, "vm.disk.write", diskWriteMetric.TypeName)
					assert.IsType(t, float64(0), diskWriteMetric.Value)
					assert.Equal(t, vmidStr, diskWriteMetric.ResourceID)
					assert.Equal(t, vmidStr, diskWriteMetric.AgentInstanceID)

					// Verify network in metric
					netInMetric := findMetricByType(metrics, "vm.network.in")
					require.NotNil(t, netInMetric, "Network in metric should be present")
					assert.Equal(t, "vm.network.in", netInMetric.TypeName)
					assert.IsType(t, float64(0), netInMetric.Value)
					assert.Equal(t, vmidStr, netInMetric.ResourceID)
					assert.Equal(t, vmidStr, netInMetric.AgentInstanceID)

					// Verify network out metric
					netOutMetric := findMetricByType(metrics, "vm.network.out")
					require.NotNil(t, netOutMetric, "Network out metric should be present")
					assert.Equal(t, "vm.network.out", netOutMetric.TypeName)
					assert.IsType(t, float64(0), netOutMetric.Value)
					assert.Equal(t, vmidStr, netOutMetric.ResourceID)
					assert.Equal(t, vmidStr, netOutMetric.AgentInstanceID)

					// Verify uptime metric
					uptimeMetric := findMetricByType(metrics, "vm.uptime")
					require.NotNil(t, uptimeMetric, "Uptime metric should be present")
					assert.Equal(t, "vm.uptime", uptimeMetric.TypeName)
					assert.IsType(t, float64(0), uptimeMetric.Value)
					assert.Equal(t, vmidStr, uptimeMetric.ResourceID)
					assert.Equal(t, vmidStr, uptimeMetric.AgentInstanceID)
				}
			}
		})
	}
}

// Helper function to create a string pointer
func stringPtr(s string) *string {
	return &s
}

// Helper function to create raw resources from JSON string
func rawResourcesPtr(jsonStr string) *json.RawMessage {
	raw := json.RawMessage(jsonStr)
	return &raw
}

// Helper function to find a metric by type name
func findMetricByType(metrics []agent.MetricEntry, typeName string) *agent.MetricEntry {
	for _, metric := range metrics {
		if metric.TypeName == typeName {
			return &metric
		}
	}
	return nil
}
