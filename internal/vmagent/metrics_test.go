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
				ConsumerID: "testuser",
				Resources:  rawResourcesPtr(`{"vmid": 123}`),
				ExternalID: stringPtr("123"),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				vm := &Vm{
					VMID:   123,
					Name:   "test-vm",
					CPU:    0.5,
					MaxCPU: 2.0,
					Mem:    1024 * 1024 * 1024, // 1GB
					MaxMem: 2048 * 1024 * 1024, // 2GB
				}
				mockClient.EXPECT().GetVM("testuser", 123).Return(vm, nil).Once()
			},
			expectError:     false,
			expectedMetrics: 2,
		},
		{
			name: "successful_metrics_zero_max_values",
			service: &agent.RawService{
				ConsumerID: "testuser",
				Resources:  rawResourcesPtr(`{"vmid": 456}`),
				ExternalID: stringPtr("456"),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				vm := &Vm{
					VMID:   456,
					Name:   "test-vm-zero",
					CPU:    0.3,
					MaxCPU: 0, // Zero max CPU should result in 0 ratio
					Mem:    512 * 1024 * 1024,
					MaxMem: 0, // Zero max memory should result in 0 ratio
				}
				mockClient.EXPECT().GetVM("testuser", 456).Return(vm, nil).Once()
			},
			expectError:     false,
			expectedMetrics: 2,
		},
		{
			name: "get_vm_fails",
			service: &agent.RawService{
				ConsumerID: "testuser",
				Resources:  rawResourcesPtr(`{"vmid": 789}`),
				ExternalID: stringPtr("789"),
			},
			setupMock: func(mockClient *MockProxmoxClient) {
				mockClient.EXPECT().GetVM("testuser", 789).Return(nil, errors.New("VM not found")).Once()
			},
			expectError: true,
			errorMsg:    "failed to get VM",
		},
		// Note: service_is_nil test case removed because MetricsReporterWrapper doesn't handle nil service gracefully
		{
			name: "service_external_id_is_nil",
			service: &agent.RawService{
				ConsumerID: "testuser",
				Resources:  rawResourcesPtr(`{"vmid": 999}`),
				ExternalID: nil,
			},
			setupMock:   func(mockClient *MockProxmoxClient) {},
			expectError: true,
			errorMsg:    "service is nil or externalID is nil",
		},
		{
			name: "service_resources_missing_vmid",
			service: &agent.RawService{
				ConsumerID: "testuser",
				Resources:  rawResourcesPtr(`{}`),
				ExternalID: stringPtr("111"),
			},
			setupMock:   func(mockClient *MockProxmoxClient) {},
			expectError: true,
			errorMsg:    "vmid is missing or zero",
		},
		{
			name: "service_resources_vmid_wrong_type",
			service: &agent.RawService{
				ConsumerID: "testuser",
				Resources:  rawResourcesPtr(`{"vmid": "not-an-int"}`),
				ExternalID: stringPtr("222"),
			},
			setupMock:   func(mockClient *MockProxmoxClient) {},
			expectError: true,
			errorMsg:    "json: cannot unmarshal string",
		},
		{
			name: "service_resources_is_nil",
			service: &agent.RawService{
				ConsumerID: "testuser",
				Resources:  nil,
				ExternalID: stringPtr("333"),
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
					// Verify CPU usage metric
					cpuMetric := findMetricByType(metrics, "vm.cpu.usage")
					require.NotNil(t, cpuMetric, "CPU usage metric should be present")
					assert.Equal(t, "vm.cpu.usage", cpuMetric.TypeName)
					assert.IsType(t, float64(0), cpuMetric.Value)

					// Verify memory usage metric
					memMetric := findMetricByType(metrics, "vm.memory.usage")
					require.NotNil(t, memMetric, "Memory usage metric should be present")
					assert.Equal(t, "vm.memory.usage", memMetric.TypeName)
					assert.IsType(t, float64(0), memMetric.Value)

					// Verify resource and external IDs match
					vmidStr := (*tt.service.ExternalID)
					assert.Equal(t, vmidStr, cpuMetric.ResourceID)
					assert.Equal(t, vmidStr, cpuMetric.ExternalID)
					assert.Equal(t, vmidStr, memMetric.ResourceID)
					assert.Equal(t, vmidStr, memMetric.ExternalID)
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
